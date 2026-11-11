package audit

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/heyNag/eva-md/internal/catchup"
	"github.com/heyNag/eva-md/internal/ledger/inboxv1"
	"github.com/heyNag/eva-md/internal/session"
)

// The tools whose calls v1's hooks capture: the matchers the plugins
// declare for PostToolUse, which a test holds equal to what the installer
// writes. Calls of other tools are left out by design and not counted.
const (
	claudeMatcher = "Bash|Edit|Write|NotebookEdit|TaskCreate|TaskUpdate|TodoWrite|Read|Grep|Glob|WebFetch|WebSearch|mcp__.*"
	codexMatcher  = "^(Bash|apply_patch|update_plan|mcp__.*)$"
)

var (
	claudeTools = regexp.MustCompile("^(?:" + claudeMatcher + ")$")
	codexTools  = regexp.MustCompile(codexMatcher)
)

// codexShell holds the names Codex's record gives a shell call, whose hook
// names it Bash. Which names a version uses is a host fact the host sitting
// records; until then a call by another name counts only if it matches the
// matcher itself.
var codexShell = map[string]bool{"shell": true, "container.exec": true, "exec_command": true, "shell_command": true, "local_shell": true}

// maxRow is the longest row of a host's record the audit decodes. A longer
// row, as a pasted image makes, is scanned for a record control, as
// recovery scans one, and otherwise counted as too long to read.
const maxRow = 32 << 20

// The kinds of a host's item that are not compared.
const (
	interrupt = -1 // its turn was interrupted
	longRow   = -2 // a row too long to read
	awayItem  = -3 // a prompt, reply or tool call in a folder the hooks leave out
)

// hostItem is one prompt, final reply or tool call of the host's record,
// an interrupt, or a row too long to read, with its turn id and its time
// (zero when the row has none). It holds nothing of the row's text.
type hostItem struct {
	kind  int
	turn  string
	at    time.Time
	voice bool
}

// hostRecord is what the audit counts of one host transcript: its items,
// outside every range said off the record, and how many such ranges it
// holds and the stretches of time they cover.
type hostRecord struct {
	items  []hostItem
	paused int
	pauses []interval
}

// hostReader reads one transcript's rows in order.
type hostReader struct {
	src     catchup.Source
	outside func(string) bool // a folder whose chats the hooks leave out
	rec     hostRecord
	turn    string    // the current turn's id
	off     bool      // in a range said off the record
	offAt   time.Time // when it began, zero when not known
	last    time.Time // the time of the last row that had one
	// unknown marks a row since the last record control that could not be
	// read, so whether it was one is not known; unknownAt is its time, or
	// the time of the last row before it.
	unknown   bool
	unknownAt time.Time
	away      bool // on Codex, the turn runs in a folder the hooks leave out
	tools     map[string]bool
	replied   map[string]bool // Codex turns whose final reply is counted
	ordinal   int             // Codex turns begun, for those with no turn id
	answered  bool            // the reply of turn ordinal is counted
}

// readHost reads the host's transcript r of the session s names. A row
// longer than limit is scanned for a record control and otherwise counted
// as too long to read. A last row with no newline is left unread, as
// recovery leaves it. outside, when not nil, says which folders' chats the
// hooks leave out by design.
func readHost(ctx context.Context, s catchup.Source, outside func(string) bool, r io.Reader, limit int) (hostRecord, error) {
	if outside == nil {
		outside = func(string) bool { return false }
	}
	h := &hostReader{src: s, outside: outside, tools: map[string]bool{}, replied: map[string]bool{}}
	br := bufio.NewReaderSize(r, 64<<10)
	var row []byte
	var long catchup.LongRow
	over := false
	for n := 0; ; n++ {
		if n%1024 == 0 {
			if err := ctx.Err(); err != nil {
				return hostRecord{}, err
			}
		}
		chunk, err := br.ReadSlice('\n')
		if !over && len(row)+len(chunk) > limit {
			over = true
			long = catchup.LongRow{}
			long.Feed(row)
			row = row[:0]
		}
		if over {
			long.Feed(bytes.TrimSuffix(chunk, []byte{'\n'}))
		} else {
			row = append(row, chunk...)
		}
		switch {
		case errors.Is(err, bufio.ErrBufferFull):
			continue
		case errors.Is(err, io.EOF):
			if h.off {
				h.rec.pauses = append(h.rec.pauses, interval{start: h.offAt})
			}
			return h.rec, nil
		case err != nil:
			return hostRecord{}, err
		}
		switch {
		case over:
			h.long(long.Control(s))
		case s.Host == "codex":
			h.codex(row)
		default:
			h.claude(row)
		}
		row, over = row[:0], false
	}
}

// long counts a row too long to read, unless it is a record control. A row
// whose control can't be told may have been one.
func (h *hostReader) long(control, ts string, ok bool) {
	at, _ := time.Parse(time.RFC3339Nano, ts)
	if control != "" {
		h.control(control, at)
	} else {
		if !ok {
			h.unread(at)
		}
		h.add(longRow, "", ts, false)
	}
	h.seen(at)
}

// seen keeps the time of a row that has one.
func (h *hostReader) seen(at time.Time) {
	if !at.IsZero() {
		h.last = at
	}
}

// unread marks a row that could not be read, at its time, or the time of
// the last row before it when it has none.
func (h *hostReader) unread(at time.Time) {
	if h.unknown {
		return
	}
	if at.IsZero() {
		at = h.last
	}
	h.unknown, h.unknownAt = true, at
}

// add counts an item unless the record is off the record there. In a
// folder the hooks leave out a prompt, reply or tool call is counted
// apart and an interrupt not at all.
func (h *hostReader) add(kind int, turn, ts string, voice bool) {
	if h.off {
		return
	}
	if h.away && kind == interrupt {
		return
	}
	if h.away && kind >= 0 {
		kind, turn, voice = awayItem, "", false
	}
	at, _ := time.Parse(time.RFC3339Nano, ts)
	h.rec.items = append(h.rec.items, hostItem{kind: kind, turn: turn, at: at, voice: voice})
}

// control opens or closes a range said off the record. A range's stretch
// of time runs from its control's row to a second after the row that
// closed it, and is open where a row has no time. A control that brings
// the chat back on the record with no range open closes one opened at a
// row since the last control that could not be read, if there was one,
// since that row may have been the control that opened it.
func (h *hostReader) control(control string, at time.Time) {
	switch control {
	case "off_record":
		if !h.off {
			h.off, h.offAt = true, at
			h.rec.paused++
		}
	case "on_record":
		end := at
		if !end.IsZero() {
			end = end.Add(time.Second)
		}
		switch {
		case h.off:
			h.rec.pauses = append(h.rec.pauses, interval{start: h.offAt, end: end})
		case h.unknown:
			h.rec.pauses = append(h.rec.pauses, interval{start: h.unknownAt, end: end})
			h.rec.paused++
		}
		h.off = false
	}
	h.unknown = false
}

// prompt counts a prompt that reached the agent, unless it is a record
// control, which opens or closes a range said off the record and is
// counted as neither, or a text v1's hooks never take for a prompt.
func (h *hostReader) prompt(text, turn, ts string) {
	if control := session.RecordControl(text); control != "" {
		at, _ := time.Parse(time.RFC3339Nano, ts)
		h.control(control, at)
		return
	}
	head := strings.ToLower(strings.TrimSpace(text))
	switch {
	case strings.HasPrefix(head, "[request interrupted by user"):
		h.add(interrupt, turn, ts, false)
	case head == "" || hostWrapper(head) || builtinCommand(head):
	default:
		h.add(Prompt, turn, ts, strings.HasPrefix(strings.TrimLeft(text, " \t\r\n"), inboxv1.Wrapper))
	}
}

// hostWrapper reports a Claude Code user row that wraps text the user did
// not type as a prompt: bash-mode input and output and command banners,
// as recovery's list has them.
func hostWrapper(head string) bool {
	for _, p := range []string{"<bash-input>", "<bash-stdout>", "<bash-stderr>", "<command-message>", "<command-name>", "<command-args>", "<local-command-stdout>", "<local-command-stderr>"} {
		if strings.HasPrefix(head, p) {
			return true
		}
	}
	return false
}

// builtinCommand reports a host's own command, which v1's prompt hook
// passes over, as its list has them.
func builtinCommand(head string) bool {
	word, _, _ := strings.Cut(head, " ")
	switch strings.TrimSpace(word) {
	case "/clear", "/compact", "/resume", "/fork", "/model", "/help", "/exit", "/quit", "/memory", "/memories", "/hooks", "/status", "/config", "/permissions", "/cost", "/context", "/login", "/logout":
		return true
	}
	return false
}

// item is one item of a message's content.
type item struct {
	Type string `json:"type"`
	Text string `json:"text"`
	ID   string `json:"id"`
	Name string `json:"name"`
}

// claudeRow is what the audit reads of a row of Claude Code's record.
type claudeRow struct {
	Type        string `json:"type"`
	SessionID   string `json:"sessionId"`
	Timestamp   string `json:"timestamp"`
	Cwd         string `json:"cwd"`
	PromptID    string `json:"promptId"`
	IsMeta      bool   `json:"isMeta"`
	IsSidechain bool   `json:"isSidechain"`
	AgentID     string `json:"agentId"`
	Summary     bool   `json:"isCompactSummary"`
	Repeats     bool   `json:"repeatsCompactSummary"`
	APIError    bool   `json:"isApiErrorMessage"`
	Message     struct {
		Role    string          `json:"role"`
		Model   string          `json:"model"`
		Stop    string          `json:"stop_reason"`
		Content json.RawMessage `json:"content"`
	} `json:"message"`
}

// claude reads one row of Claude Code's record: a user row is a prompt,
// unless it is a tool's result, the host's own or a subagent's; an
// assistant row holds tool calls and, when it ends the turn, a final reply.
func (h *hostReader) claude(line []byte) {
	var row claudeRow
	if json.Unmarshal(line, &row) != nil {
		h.unread(time.Time{})
		return
	}
	at, _ := time.Parse(time.RFC3339Nano, row.Timestamp)
	defer h.seen(at)
	if row.SessionID != h.src.Session || row.IsSidechain || row.AgentID != "" || row.Message.Role != row.Type {
		return
	}
	h.away = row.Cwd != "" && h.outside(row.Cwd)
	if row.PromptID != "" {
		h.turn = row.PromptID
	}
	var text string
	var items []item
	if json.Unmarshal(row.Message.Content, &text) != nil && json.Unmarshal(row.Message.Content, &items) != nil {
		return
	}
	switch row.Type {
	case "user":
		if row.IsMeta || row.Summary || row.Repeats {
			return
		}
		var parts []string
		for _, it := range items {
			if it.Type == "tool_result" {
				return
			}
			if it.Type == "text" {
				parts = append(parts, it.Text)
			}
		}
		if items != nil {
			text = strings.Join(parts, "\n")
		}
		h.prompt(text, h.turn, row.Timestamp)
	case "assistant":
		if row.Message.Model == "<synthetic>" || row.APIError {
			return
		}
		reply := true
		var parts []string
		for _, it := range items {
			switch it.Type {
			case "text":
				parts = append(parts, it.Text)
			case "tool_use":
				reply = false
				if claudeTools.MatchString(it.Name) && (it.ID == "" || !h.tools[it.ID]) {
					h.tools[it.ID] = true
					h.add(Tool, h.turn, row.Timestamp, false)
				}
			default:
				reply = false
			}
		}
		if items != nil {
			text = strings.Join(parts, "\n")
		}
		if reply && (row.Message.Stop == "end_turn" || row.Message.Stop == "stop_sequence") && strings.TrimSpace(text) != "" {
			h.add(Reply, h.turn, row.Timestamp, false)
		}
	}
}

// codexRow is what the audit reads of a row of Codex's record.
type codexRow struct {
	Timestamp string `json:"timestamp"`
	Type      string `json:"type"`
	Payload   struct {
		Type    string          `json:"type"`
		TurnID  string          `json:"turn_id"`
		Message json.RawMessage `json:"message"`
		Last    json.RawMessage `json:"last_agent_message"`
		Phase   string          `json:"phase"`
		Role    string          `json:"role"`
		Name    string          `json:"name"`
		CallID  string          `json:"call_id"`
		Cwd     string          `json:"cwd"`
	} `json:"payload"`
}

// codex reads one row of Codex's record: a user message is a prompt, a
// final agent message the turn's reply, counted once a turn, and a call of
// a tool v1's hook takes a tool call. The session's first record and each
// turn's context say which folder its turns run in.
func (h *hostReader) codex(line []byte) {
	var row codexRow
	if json.Unmarshal(line, &row) != nil {
		h.unread(time.Time{})
		return
	}
	at, _ := time.Parse(time.RFC3339Nano, row.Timestamp)
	defer h.seen(at)
	p := row.Payload
	turn := p.TurnID
	if turn == "" {
		turn = h.turn
	}
	switch row.Type + "/" + p.Type {
	case "session_meta/":
		if p.Cwd != "" {
			h.away = h.outside(p.Cwd)
		}
	case "turn_context/", "event_msg/task_started":
		if p.TurnID != "" {
			h.turn = p.TurnID
		}
		if p.Cwd != "" {
			h.away = h.outside(p.Cwd)
		}
		h.begin()
		return
	case "event_msg/user_message":
		h.begin()
		h.prompt(str(p.Message), turn, row.Timestamp)
	case "event_msg/agent_message":
		if p.Phase == "final" && strings.TrimSpace(str(p.Message)) != "" {
			h.reply(turn, row.Timestamp)
		}
	case "event_msg/task_complete":
		if strings.TrimSpace(str(p.Last)) != "" {
			h.reply(turn, row.Timestamp)
		}
	case "event_msg/turn_aborted":
		h.add(interrupt, turn, row.Timestamp, false)
	case "response_item/message":
		if p.Role == "assistant" && p.Phase == "final" {
			h.reply(h.turn, row.Timestamp)
		}
	case "response_item/function_call", "response_item/custom_tool_call", "response_item/local_shell_call":
		name := p.Name
		if codexShell[name] || p.Type == "local_shell_call" {
			name = "Bash"
		}
		if codexTools.MatchString(name) && (p.CallID == "" || !h.tools[p.CallID]) {
			h.tools[p.CallID] = true
			h.add(Tool, h.turn, row.Timestamp, false)
		}
	}
}

// begin marks a row that begins a Codex turn: a turn begun once its reply
// is counted is the next one.
func (h *hostReader) begin() {
	if h.answered {
		h.ordinal, h.answered = h.ordinal+1, false
	}
}

// reply counts a turn's final reply once: Codex writes it in more than one
// row. A turn with no id is told by the rows that begin it.
func (h *hostReader) reply(turn, ts string) {
	key := turn
	if key == "" {
		key = "#" + strconv.Itoa(h.ordinal)
	}
	if h.off || h.replied[key] {
		return
	}
	h.replied[key], h.answered = true, true
	h.add(Reply, turn, ts, false)
}

// str is raw's string, or "" when it is not one.
func str(raw json.RawMessage) string {
	var s string
	_ = json.Unmarshal(raw, &s)
	return s
}
