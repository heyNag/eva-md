package shapes

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/heyNag/eva-md/internal/catchup"
	"github.com/heyNag/eva-md/internal/host/adapter"
	"github.com/heyNag/eva-md/internal/ledger/inboxv1"
	"github.com/heyNag/eva-md/internal/session"
)

// maxNames bounds the distinct names a file's tally keeps of one kind, and
// the turn ids it keeps: past it, a count of distinct names is a floor.
const maxNames = 4096

// tally is what one file, or one inbox session, adds to the report:
// counts by path (see tree), the paths it shows at least once, which count
// its chat once, the distinct names counted under other by path, as
// hashes, and the turn ids its rows carry, as hashes. It holds nothing of
// a row's text, no name off the lists and no value.
type tally struct {
	n       map[string]int
	present map[string]bool
	others  map[string]map[uint64]bool
	turns   map[uint64]bool
}

func newTally() *tally {
	return &tally{n: map[string]int{}, present: map[string]bool{}, others: map[string]map[uint64]bool{}, turns: map[uint64]bool{}}
}

// add counts a path's rows.
func (t *tally) add(path ...string) { t.n[strings.Join(path, "/")]++ }

// chat marks a path as shown in the tally's chat.
func (t *tally) chat(path ...string) { t.present[strings.Join(path, "/")] = true }

// name counts name under path: under its own name when list holds it, and
// otherwise under other, with its hash among the distinct other names. It
// counts the row and marks the name as shown in the chat.
func (t *tally) name(list []string, name string, path ...string) {
	base := strings.Join(path, "/")
	n := listed(list, name)
	t.n[base+"/rows/"+n]++
	t.present[base+"/chats/"+n] = true
	if n == other {
		set := t.others[base]
		if set == nil {
			set = map[uint64]bool{}
			t.others[base] = set
		}
		if len(set) < maxNames {
			set[hash(name)] = true
		}
	}
}

// value counts a one-word value under path by name: a string on list by
// its own name, any other string and any value that is not a string under
// other.
func (t *tally) value(list []string, raw json.RawMessage, path ...string) {
	var s string
	if json.Unmarshal(raw, &s) != nil {
		s = "\x00" + kind(raw)
	}
	t.name(list, s, path...)
}

// version counts a version under path, by chat: as itself when it is
// three numbers of ASCII digits, and otherwise as other.
func (t *tally) version(raw json.RawMessage, path ...string) {
	var s string
	if json.Unmarshal(raw, &s) != nil || !versionForm.MatchString(s) {
		s = other
	}
	t.chat(append(path, "chats", s)...)
}

// turn keeps a turn id's hash, to tell whether the inbox's turn ids are on
// the host's rows.
func (t *tally) turn(id string) {
	if id != "" && len(t.turns) < maxNames {
		t.turns[hash(id)] = true
	}
}

// hash is a name's or an id's hash, kept only while the report is made.
func hash(s string) uint64 {
	sum := sha256.Sum256([]byte(s))
	return binary.BigEndian.Uint64(sum[:8])
}

// kind is the JSON kind of a value that is not a string.
func kind(raw json.RawMessage) string {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 {
		return "none"
	}
	switch raw[0] {
	case '{':
		return "object"
	case '[':
		return "array"
	case 't', 'f':
		return "bool"
	case 'n':
		return "null"
	}
	return "number"
}

// sizeBucket is the bucket of a size in bytes.
func sizeBucket(n int) string {
	for _, b := range sizeBuckets {
		if b.below < 0 || n < b.below {
			return b.name
		}
	}
	return ""
}

// gapBucket is the bucket of a gap in time.
func gapBucket(d time.Duration) string {
	for _, b := range gapBuckets {
		if b.below < 0 || d.Seconds() < b.below {
			return b.name
		}
	}
	return ""
}

// fileRead is what reading one host file found: its tally, the session
// ids its rows name, whether it was said off the record, ran in a folder
// the hooks leave out or held a row whether that was so could not be
// told, and the first and last time a row gives.
type fileRead struct {
	t           *tally
	ids         []string // while the file is read
	nodes       []string // the nodes of those ids, once it is read
	off, away   bool
	unknown     bool
	first, last time.Time
}

// reader reads one host file's rows.
type reader struct {
	host    string
	src     catchup.Source
	outside func(string) bool
	f       fileRead
	rows    int
	prev    time.Time // the time of the last row with one
	// Claude Code: the last folder and branch a row gave.
	cwd, branch string
	// Codex: a turn is running, and has had its prompt.
	running, prompted bool
	ordinal           float64
	ordinals          bool
	turned            bool // a turn was begun or a prompt given
	lastYear          int  // the last year a row's time is taken in
}

// firstYear is the first year a row's time is taken in.
const firstYear = 2000

// readRows reads a host's file, the session file s names, row by row, each
// at most limit bytes: a longer row is scanned for a record control, as
// recovery scans one, and counted as too long. A last row with no newline
// is left unread, as recovery leaves it. outside says which folders' chats
// the hooks leave out. A row dated before firstYear, or after the year
// after now's, is counted as out of range and its time is not used.
func readRows(ctx context.Context, s catchup.Source, outside func(string) bool, r io.Reader, limit int, written, now time.Time) (fileRead, error) {
	lastYear := 9999
	if !now.IsZero() {
		lastYear = min(now.UTC().Year()+1, lastYear)
	}
	h := &reader{host: s.Host, src: s, outside: outside, f: fileRead{t: newTally()}, lastYear: lastYear}
	br := bufio.NewReaderSize(r, 64<<10)
	var row []byte
	var long catchup.LongRow
	over := false
	size := 0
	for n := 0; ; n++ {
		if n%1024 == 0 {
			if err := ctx.Err(); err != nil {
				return fileRead{}, err
			}
		}
		chunk, err := br.ReadSlice('\n')
		size += len(chunk)
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
			h.end(written)
			return h.f, nil
		case err != nil:
			return fileRead{}, err
		}
		if over {
			h.long(&long, size)
		} else {
			h.row(bytes.TrimSuffix(row, []byte{'\n'}), size)
		}
		row, over, size = row[:0], false, 0
	}
}

// end counts what the whole file shows.
func (h *reader) end(written time.Time) {
	t := h.f.t
	if h.turned {
		t.chat(facts138, h.host, "with_a_turn")
	}
	if !h.prev.IsZero() && !written.IsZero() {
		d := written.Sub(h.prev)
		if d < 0 {
			t.add(facts020, h.host, "file_written_after_last_row", "earlier")
		} else {
			t.add(facts020, h.host, "file_written_after_last_row", gapBucket(d))
		}
	}
}

// Fact numbers, as the report keys them.
const (
	facts001 = "F-001"
	facts004 = "F-004"
	facts005 = "F-005"
	facts010 = "F-010"
	facts013 = "F-013"
	facts018 = "F-018"
	facts020 = "F-020"
	facts021 = "F-021"
	facts022 = "F-022"
	facts024 = "F-024"
	facts027 = "F-027"
	facts034 = "F-034"
	facts079 = "F-079"
	facts084 = "F-084"
	facts085 = "F-085"
	facts132 = "F-132"
	facts133 = "F-133"
	facts134 = "F-134"
	facts136 = "F-136"
	facts138 = "F-138"
	facts141 = "F-141"
)

// long counts a row too long to read whole. A record control in it, as
// recovery's scan finds one for a row of the file's own session or of a
// session an earlier row named, puts the file off the record; a row the
// scan cannot follow leaves that not known.
func (h *reader) long(l *catchup.LongRow, size int) {
	h.rows++
	t := h.f.t
	t.add(h.host, "rows", "too_long")
	if h.host == string(adapter.Codex) {
		if h.rows == 1 {
			t.add(facts134, h.host, "first_row_bytes", sizeBucket(size))
		}
	} else {
		t.add(facts085, h.host, "bytes", "too_long", sizeBucket(size))
	}
	var at time.Time
	for _, id := range append([]string{h.src.Session}, h.f.ids...) {
		s := h.src
		s.Session = id
		control, ts, ok := l.Control(s)
		if !ok {
			h.f.unknown = true
			return
		}
		if control != "" {
			h.f.off = true
		}
		at, _ = time.Parse(time.RFC3339Nano, ts)
	}
	h.when(at)
}

// row counts one row read whole.
func (h *reader) row(line []byte, size int) {
	h.rows++
	t := h.f.t
	var row map[string]json.RawMessage
	if json.Unmarshal(line, &row) != nil || row == nil {
		// Not a JSON object: whether it was a record control is not known.
		t.add(h.host, "rows", "not_json")
		h.f.unknown = true
		return
	}
	t.add(h.host, "rows", "read")
	var at time.Time
	var ts string
	if json.Unmarshal(row["timestamp"], &ts) == nil {
		at, _ = time.Parse(time.RFC3339Nano, ts)
	}
	h.when(at)
	if h.host == string(adapter.Codex) {
		h.codex(row, size)
	} else {
		h.claude(row, size)
	}
}

// when counts a row's time: none, one out of range, or one taken.
func (h *reader) when(at time.Time) {
	switch y := at.UTC().Year(); {
	case at.IsZero():
		h.f.t.add(h.host, "rows", "no_time")
	case y < firstYear || y > h.lastYear:
		h.f.t.add(h.host, "rows", "out_of_range")
	default:
		h.at(at)
	}
}

// at counts a row's time against the last row's that had one.
func (h *reader) at(at time.Time) {
	t := h.f.t
	if !h.prev.IsZero() {
		switch d := at.Sub(h.prev); {
		case d < 0:
			t.add(facts020, h.host, "order", "earlier")
		case d == 0:
			t.add(facts020, h.host, "order", "same_time")
		default:
			t.add(facts020, h.host, "order", "later")
			t.add(facts020, h.host, "gaps", gapBucket(d))
		}
	}
	if h.f.first.IsZero() || at.Before(h.f.first) {
		h.f.first = at
	}
	if at.After(h.f.last) {
		h.f.last = at
	}
	h.prev = at
}

// str is raw's string, or "" when it is not one.
func str(raw json.RawMessage) string {
	var s string
	_ = json.Unmarshal(raw, &s)
	return s
}

// truth is whether raw is the JSON true.
func truth(raw json.RawMessage) bool {
	var b bool
	return json.Unmarshal(raw, &b) == nil && b
}

// keys are an object's keys, in no order that matters.
func keys(m map[string]json.RawMessage) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}

// folder judges a row's folder: one the hooks leave out puts the file out.
func (h *reader) folder(cwd string) {
	if filepath.IsAbs(cwd) && h.outside(cwd) {
		h.f.away = true
	}
}

// control puts the file off the record when text is a record control.
func (h *reader) control(text string) {
	if session.RecordControl(text) != "" {
		h.f.off = true
	}
}

// id keeps a session id a row names, to count the file with that chat.
func (h *reader) id(id string) {
	if id != "" && id != h.src.Session && !slices.Contains(h.f.ids, id) && len(h.f.ids) < maxNames {
		h.f.ids = append(h.f.ids, id)
	}
}

// claude counts one Claude Code row: its type and fields, its message's
// fields, its content items' types and fields, and the values the lists
// name. It never looks inside a content item's input, content or text,
// nor inside toolUseResult, but for a user row's text, which it checks for
// a record control and keeps nothing of.
func (h *reader) claude(row map[string]json.RawMessage, size int) {
	t := h.f.t
	host := h.host
	typ := listed(claudeRowTypes, str(row["type"]))
	t.value(claudeRowTypes, row["type"], facts085, host, "row_types")
	t.add(facts085, host, "bytes", typ, sizeBucket(size))
	for _, k := range keys(row) {
		t.name(claudeRowFields, k, facts084, host, "fields", typ)
	}
	for _, flag := range []string{"isMeta", "isSidechain", "isCompactSummary", "repeatsCompactSummary", "isApiErrorMessage", "isVisibleInTranscriptOnly"} {
		if truth(row[flag]) {
			t.add(facts084, host, "flags_true", flag)
		}
	}
	if raw, ok := row["entrypoint"]; ok {
		t.value(claudeEntrypoints, raw, facts079, host, "entrypoint")
		t.value(claudeEntrypoints, raw, facts018, host, "entrypoint")
	}
	if raw, ok := row["turnOrigin"]; ok {
		t.value(turnOrigins, raw, facts018, host, "turn_origin")
	}
	if raw, ok := row["subtype"]; ok {
		t.value(claudeSubtypes, raw, facts085, host, "subtypes")
	}
	if raw, ok := row["version"]; ok {
		t.version(raw, facts084, host, "versions")
	}
	h.id(str(row["sessionId"]))
	// A row's own content, as a queued prompt's, is checked for a record
	// control too.
	if text, ok := row["content"]; ok {
		h.control(str(text))
	}
	cwd, branch := str(row["cwd"]), str(row[branchField])
	h.folder(cwd)
	if str(row["timestamp"]) != "" {
		t.add(facts021, host, typ, "time")
	}
	if cwd != "" {
		t.add(facts021, host, typ, "folder")
		if h.cwd != "" && cwd != h.cwd {
			t.chat(facts021, host, "folder_changed")
		}
		h.cwd = cwd
	}
	if branch != "" {
		t.add(facts021, host, typ, "branch")
		if h.branch != "" && branch != h.branch {
			t.chat(facts021, host, "branch_changed")
		}
		h.branch = branch
	}
	if truth(row["isSidechain"]) {
		t.add(facts027, host, "sidechain")
	}
	if str(row["agentId"]) != "" || str(row["agent_id"]) != "" {
		t.add(facts027, host, "agent_id")
	}
	if truth(row["isCompactSummary"]) {
		t.add(facts034, host, "compact_summary")
	}
	if typ == "user" || typ == "assistant" {
		t.add(facts001, host, "rows")
		turn := str(row["promptId"])
		if turn == "" {
			turn = str(row["prompt_id"])
		}
		if turn != "" {
			t.add(facts001, host, "with_turn_id")
			t.turn(turn)
		}
	}
	var message map[string]json.RawMessage
	if json.Unmarshal(row["message"], &message) != nil || message == nil {
		return
	}
	for _, k := range keys(message) {
		t.name(claudeMessageFields, k, facts084, host, "message_fields")
	}
	var text string
	var items []map[string]json.RawMessage
	if json.Unmarshal(message["content"], &text) == nil {
		if typ == "user" {
			h.control(text)
			h.turned = true
		}
		if typ == "assistant" {
			t.add(facts022, host, "assistant_rows", "text_only")
		}
		return
	}
	if json.Unmarshal(message["content"], &items) != nil {
		return
	}
	var texts []string
	hasText, hasTool, hasResult := false, false, false
	for _, it := range items {
		itemType := listed(claudeItemTypes, str(it["type"]))
		t.value(claudeItemTypes, it["type"], facts084, host, "item_types")
		for _, k := range keys(it) {
			t.name(claudeItemFields, k, facts084, host, "item_fields", itemType)
		}
		switch itemType {
		case "text":
			hasText = true
			texts = append(texts, str(it["text"]))
		case "tool_use":
			hasTool = true
		case "tool_result":
			hasResult = true
			t.add(facts022, host, "tool_results")
		}
	}
	switch typ {
	case "user":
		if hasResult {
			if _, ok := row["toolUseResult"]; ok {
				t.add(facts022, host, "with_tool_use_result")
			}
			return
		}
		h.control(strings.Join(texts, "\n"))
		h.turned = true
	case "assistant":
		switch {
		case hasText && hasTool:
			t.add(facts022, host, "assistant_rows", "text_and_tools")
		case hasText:
			t.add(facts022, host, "assistant_rows", "text_only")
		case hasTool:
			t.add(facts022, host, "assistant_rows", "tools_only")
		default:
			t.add(facts022, host, "assistant_rows", "neither")
		}
	}
}

// codex counts one Codex row: its type and fields, its payload's type and
// fields, the session's first record's source, originator and version,
// and the order of turns and prompts. It never looks inside a payload's
// message, content, arguments, output, source or turn_attribution, but for
// a prompt's text, which it checks for a record control and keeps nothing
// of.
func (h *reader) codex(row map[string]json.RawMessage, size int) {
	t := h.f.t
	host := h.host
	typ := listed(codexRowTypes, str(row["type"]))
	t.value(codexRowTypes, row["type"], facts132, host, "row_types")
	for _, k := range keys(row) {
		t.name(codexRowFields, k, facts132, host, "fields")
	}
	if h.rows == 1 {
		t.value(codexRowTypes, row["type"], facts134, host, "first_row")
		t.add(facts134, host, "first_row_bytes", sizeBucket(size))
	}
	if raw, ok := row["ordinal"]; ok {
		t.add(facts133, host, "with_ordinal")
		var o float64
		if json.Unmarshal(raw, &o) == nil {
			if h.ordinals && o <= h.ordinal {
				t.add(facts133, host, "ordinal_not_rising")
			} else if h.ordinals {
				t.add(facts133, host, "ordinal_rising")
			}
			h.ordinal, h.ordinals = o, true
		}
	}
	var p map[string]json.RawMessage
	if json.Unmarshal(row["payload"], &p) != nil || p == nil {
		return
	}
	ptype := listed(codexPayloadTypes, str(p["type"]))
	if _, ok := p["type"]; ok {
		t.value(codexPayloadTypes, p["type"], facts132, host, "payload_types")
	}
	for _, k := range keys(p) {
		t.name(codexPayloadFields, k, facts132, host, "payload_fields", typ, ptype)
	}
	if h.rows == 1 {
		for _, k := range keys(p) {
			t.name(codexPayloadFields, k, facts134, host, "payload_fields")
		}
	}
	turn := str(p["turn_id"])
	t.turn(turn)
	if typ == "event_msg" {
		t.add(facts001, host, "rows")
		if turn != "" {
			t.add(facts001, host, "with_turn_id")
		}
	}
	if str(row["timestamp"]) != "" {
		t.add(facts021, host, typ, "time")
	}
	if cwd := str(p["cwd"]); cwd != "" && (typ == "session_meta" || typ == "turn_context") {
		h.folder(cwd)
		t.add(facts021, host, typ, "folder")
		if h.cwd != "" && cwd != h.cwd {
			t.chat(facts021, host, "folder_changed")
		}
		h.cwd = cwd
	}
	if typ == "session_meta" {
		h.id(str(p["id"]))
		if raw, ok := p["source"]; ok {
			t.value(codexSources, raw, facts018, host, "source")
		}
		if raw, ok := p["originator"]; ok {
			t.value(codexOriginators, raw, facts018, host, "originator")
		}
		if raw, ok := p["cli_version"]; ok {
			t.version(raw, facts132, host, "versions")
		}
	}
	switch typ + "/" + ptype {
	case "event_msg/task_started", "event_msg/turn_started":
		h.running, h.prompted, h.turned = true, false, true
	case "event_msg/task_complete", "event_msg/turn_aborted":
		h.running, h.prompted = false, false
	case "event_msg/user_message":
		text := str(p["message"])
		h.control(text)
		h.turned = true
		t.add(facts010, host, "prompts")
		if h.running && h.prompted {
			t.add(facts010, host, "while_working")
			t.chat(facts010, host, "chats", "while_working")
		} else {
			t.add(facts010, host, "began_turn")
		}
		h.prompted = true
		if strings.HasPrefix(strings.TrimLeft(text, " \t\r\n"), inboxv1.Wrapper) {
			t.add(facts141, host, "wrapper_prompts")
		}
	case "response_item/message":
		if str(p["role"]) == "user" {
			var items []map[string]json.RawMessage
			if json.Unmarshal(p["content"], &items) == nil {
				var texts []string
				for _, it := range items {
					texts = append(texts, str(it["text"]))
				}
				h.control(strings.Join(texts, "\n"))
			} else {
				h.control(str(p["content"]))
			}
		}
	}
	if typ == "compacted" {
		t.add(facts034, host, "compacted")
		t.add(facts136, host, "summary_bytes", sizeBucket(len(p["message"])))
	}
}
