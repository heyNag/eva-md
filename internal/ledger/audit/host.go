package audit

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"regexp"
	"strings"
	"time"

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
// row, as a pasted image makes, is passed over and counted.
const maxRow = 32 << 20

// interrupt is the kind of a host item that says its turn was interrupted.
const interrupt = -1

// hostItem is one prompt, final reply or tool call of the host's record,
// or an interrupt, with its turn id and its time (zero when the row has
// none). It holds nothing of the row's text.
type hostItem struct {
	kind  int
	turn  string
	at    time.Time
	voice bool
}

// hostRecord is what the audit counts of one host transcript: its items,
// outside every range said off the record, how many such ranges it holds,
// and how many rows were too long to read.
type hostRecord struct {
	items   []hostItem
	paused  int
	tooLong int
}

// hostReader reads one transcript's rows in order.
type hostReader struct {
	host, session string
	rec           hostRecord
	turn          string // the current turn's id
	off           bool   // in a range said off the record
	tools         map[string]bool
	replied       map[string]bool // Codex turns whose final reply is counted
}

// readHost reads the host's transcript r of session id, passing over rows
// longer than limit. A last row with no newline is left unread, as
// recovery leaves it.
func readHost(ctx context.Context, host, id string, r io.Reader, limit int) (hostRecord, error) {
	h := &hostReader{host: host, session: id, tools: map[string]bool{}, replied: map[string]bool{}}
	br := bufio.NewReaderSize(r, 64<<10)
	var row []byte
	over := false
	for n := 0; ; n++ {
		if n%1024 == 0 {
			if err := ctx.Err(); err != nil {
				return hostRecord{}, err
			}
		}
		chunk, err := br.ReadSlice('\n')
		if !over && len(row)+len(chunk) > limit {
			over, row = true, row[:0]
		}
		if !over {
			row = append(row, chunk...)
		}
		switch {
		case errors.Is(err, bufio.ErrBufferFull):
			continue
		case errors.Is(err, io.EOF):
			return h.rec, nil
		case err != nil:
			return hostRecord{}, err
		}
		if over {
			h.rec.tooLong++
		} else if host == "codex" {
			h.codex(row)
		} else {
			h.claude(row)
		}
		row, over = row[:0], false
	}
}

// add counts an item unless the record is off the record there.
func (h *hostReader) add(kind int, turn, ts string, voice bool) {
	if h.off {
		return
	}
	at, _ := time.Parse(time.RFC3339Nano, ts)
	h.rec.items = append(h.rec.items, hostItem{kind: kind, turn: turn, at: at, voice: voice})
}

// prompt counts a prompt that reached the agent, unless it is a record
// control, which opens or closes a range said off the record and is
// counted as neither, or a text v1's hooks never take for a prompt.
func (h *hostReader) prompt(text, turn, ts string) {
	switch session.RecordControl(text) {
	case "off_record":
		if !h.off {
			h.off = true
			h.rec.paused++
		}
		return
	case "on_record":
		h.off = false
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
	if json.Unmarshal(line, &row) != nil || row.SessionID != h.session || row.IsSidechain || row.AgentID != "" || row.Message.Role != row.Type {
		return
	}
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
	} `json:"payload"`
}

// codex reads one row of Codex's record: a user message is a prompt, a
// final agent message the turn's reply, counted once a turn, and a call of
// a tool v1's hook takes a tool call.
func (h *hostReader) codex(line []byte) {
	var row codexRow
	if json.Unmarshal(line, &row) != nil {
		return
	}
	p := row.Payload
	turn := p.TurnID
	if turn == "" {
		turn = h.turn
	}
	switch row.Type + "/" + p.Type {
	case "turn_context/", "event_msg/task_started":
		if p.TurnID != "" {
			h.turn = p.TurnID
		}
		return
	case "event_msg/user_message":
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

// reply counts a turn's final reply once: Codex writes it in more than one
// row.
func (h *hostReader) reply(turn, ts string) {
	if h.off || h.replied[turn] {
		return
	}
	h.replied[turn] = true
	h.add(Reply, turn, ts, false)
}

// str is raw's string, or "" when it is not one.
func str(raw json.RawMessage) string {
	var s string
	_ = json.Unmarshal(raw, &s)
	return s
}
