package hostfacts

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"maps"
	"os"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/heyNag/eva-md/internal/host/adapter"
)

var errTrailing = errors.New("hostfacts: more than one JSON value")

// Field is one value of a host's record row as the recording keeps it:
// never its text. A boolean is kept; a string is kept only when it is on
// the enum allowlist or shaped like a host's id, and otherwise only as its
// length, its keyed hash and the name of a leading wrapper tag; a number
// only as its sign and its count of digits. An object or an array keeps its
// count of members, so the nesting and every array's length are known.
type Field struct {
	Path   string `json:"path"`
	Type   string `json:"type"`
	Value  any    `json:"value,omitempty"`
	Len    int    `json:"len,omitempty"`
	Hash   string `json:"hash,omitempty"`
	Tag    string `json:"tag,omitempty"`
	Sign   string `json:"sign,omitempty"`
	Digits int    `json:"digits,omitempty"`
	Frac   bool   `json:"frac,omitempty"`
	Items  *int   `json:"items,omitempty"`
	// Voice is the skeleton of a string in the form of a voice hand-over.
	Voice *Voice `json:"voice,omitempty"`
}

// Row is one row of a record file, described.
type Row struct {
	Offset  int64           `json:"offset"`
	Size    int             `json:"size"`
	Index   int             `json:"index"`
	RowType string          `json:"row_type,omitempty"`
	Invalid bool            `json:"invalid,omitempty"`
	Cut     bool            `json:"cut,omitempty"`
	Fields  []Field         `json:"fields,omitempty"`
	Derived map[string]bool `json:"derived,omitempty"`
}

// Presence says, at the moment of a hook, whether the turn's reply row, its
// prompt row and its turn-start row are in the record. Nil is not known.
type Presence struct {
	Reply     *bool `json:"reply_row,omitempty"`
	Prompt    *bool `json:"prompt_row,omitempty"`
	TurnStart *bool `json:"turn_start_row,omitempty"`
}

// Snapshot is the record file a payload names, at one hook: its size and
// modified time, and each row added since the last snapshot of that file.
type Snapshot struct {
	Size        int64     `json:"size"`
	Mtime       string    `json:"mtime,omitempty"`
	Missing     bool      `json:"missing,omitempty"`
	Rows        []Row     `json:"rows,omitempty"`
	PartialTail int       `json:"partial_tail,omitempty"`
	Cut         bool      `json:"cut,omitempty"`
	Presence    *Presence `json:"presence,omitempty"`
}

// SnapshotInput is what TakeSnapshot needs.
type SnapshotInput struct {
	Host string // a host's id
	Path string
	// From is the offset of the first row not yet described, and Index
	// that row's number in the file.
	From  int64
	Index int
	Key   []byte
	// PayloadHashes holds the hash of every string any payload of the
	// recording carried, so a user row that matches none is told apart.
	PayloadHashes map[string]bool
	// Reply, Prompt and Turn are the hashes of the turn's reply, prompt
	// and turn id, or "" when not known.
	Reply, Prompt, Turn string
	// MaxBytes bounds what is read.
	MaxBytes int64
	// MaxFields bounds each row's fields.
	MaxFields int
	// Seen holds the hash of every string of the file's rows before From,
	// and Starts the hash of every string of its turn-start rows before
	// From, as earlier snapshots gave them: a snapshot reads only the rows
	// added since the last, and still knows whether a turn's rows are in
	// the file.
	Seen, Starts map[string]bool
}

// SnapshotResult is a snapshot, where the next one starts, and the hashes
// its rows add to the file's.
type SnapshotResult struct {
	Snapshot Snapshot
	// Next is the offset of the first row not yet described, and Index its
	// row number.
	Next  int64
	Index int
	// Restart says the file was replaced by a shorter one and read from
	// its start: the hashes of the earlier rows no longer hold.
	Restart bool
	// Seen and Starts are the hashes the new rows add to in.Seen and
	// in.Starts.
	Seen, Starts []string
}

// TakeSnapshot describes the rows of in.Path from in.From, at most
// in.MaxBytes of them, and gives the offset and row number the next
// snapshot starts from. A last line without its newline is left for the
// next snapshot. Only the new rows are read: the file's earlier rows are
// known by their hashes, in in.Seen and in.Starts.
func TakeSnapshot(in SnapshotInput) (SnapshotResult, error) {
	res := SnapshotResult{Next: in.From, Index: in.Index}
	f, err := os.Open(in.Path)
	if errors.Is(err, os.ErrNotExist) {
		res.Snapshot.Missing = true
		return res, nil
	}
	if err != nil {
		return res, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return res, err
	}
	snap := &res.Snapshot
	snap.Size, snap.Mtime = info.Size(), info.ModTime().UTC().Format(time.RFC3339Nano)
	if in.From > info.Size() {
		// The file was replaced by a shorter one: start again.
		in.From, in.Index, in.Seen, in.Starts = 0, 0, nil, nil
		res.Next, res.Index, res.Restart = 0, 0, true
	}
	if _, err := f.Seek(in.From, io.SeekStart); err != nil {
		return res, err
	}
	data, err := io.ReadAll(io.LimitReader(f, in.MaxBytes))
	if err != nil {
		return res, err
	}
	if int64(len(data)) < info.Size()-in.From {
		snap.Cut = true
	}
	seen, starts := map[string]bool{}, map[string]bool{}
	offset, row := in.From, in.Index
	for len(data) > 0 {
		nl := bytes.IndexByte(data, '\n')
		if nl < 0 {
			snap.PartialTail = len(data)
			break
		}
		line := data[:nl]
		data = data[nl+1:]
		v, derr := Decode(line)
		r := describeRow(in, v, derr, row)
		r.Offset, r.Size = offset, len(line)
		snap.Rows = append(snap.Rows, r)
		hashes := rowHashes(in.Key, v, derr)
		start := derr == nil && turnStartRow(in.Host, v)
		for h := range hashes {
			if !in.Seen[h] && !seen[h] {
				seen[h] = true
				res.Seen = append(res.Seen, h)
			}
			if start && !in.Starts[h] && !starts[h] {
				starts[h] = true
				res.Starts = append(res.Starts, h)
			}
		}
		offset += int64(nl) + 1
		row++
		res.Next, res.Index = offset, row
	}
	slices.Sort(res.Seen)
	slices.Sort(res.Starts)
	// Presence is known only when every row written so far was read.
	if !snap.Cut {
		p := &Presence{}
		if in.Reply != "" {
			p.Reply = ptr(in.Seen[in.Reply] || seen[in.Reply])
		}
		if in.Prompt != "" {
			p.Prompt = ptr(in.Seen[in.Prompt] || seen[in.Prompt])
		}
		if in.Turn != "" && in.Host == string(adapter.Codex) {
			p.TurnStart = ptr(in.Starts[in.Turn] || starts[in.Turn])
		}
		if p.Reply != nil || p.Prompt != nil || p.TurnStart != nil {
			snap.Presence = p
		}
	}
	return res, nil
}

func ptr(b bool) *bool { return &b }

// PayloadHashes gives the keyed hash of each string of a decoded payload,
// by its field path as PathString writes it, but for the keys inside a
// value HideAccountValues hides, which it writes as HideAccountValues
// does: the path names, in the payload the recording keeps, the value
// whose hash it gives.
func PayloadHashes(key []byte, v any) map[string]string {
	out := map[string]string{}
	walk(v, nil, func(path []string, x any) {
		if s, ok := x.(string); ok {
			out[payloadPath(key, path)] = Hash(key, s)
		}
	})
	return out
}

// payloadPath writes a payload's path as PathString does, but for each key
// inside a value HideAccountValues hides, which it writes as that does. An
// index stays.
func payloadPath(key []byte, path []string) string {
	out := make([]string, len(path))
	hidden := false
	for i, seg := range path {
		if hidden && !isIndex(seg) {
			out[i] = hiddenKey(key, seg)
		} else {
			out[i] = PathString(key, path[i:i+1])
		}
		hidden = hidden || accountKey(seg)
	}
	return strings.Join(out, ".")
}

// hiddenKey is how a key inside a hidden value is written: "#" and its
// keyed hash, as PathString writes a key that is not key-shaped.
func hiddenKey(key []byte, k string) string { return "#" + Hash(key, k) }

// accountWords are the words of a key under which a payload's strings are
// written as their keyed hash: a model's name, a plan or a tier, which no
// recording handed over may name. A version stays, since fixtures are
// filed by the CLI's version.
var accountWords = []string{"model", "plan", "tier"}

// HideAccountValues gives a copy of a decoded payload in which every string
// under a key whose name holds a word of accountWords, in any letter case,
// at any depth, the key's own value or one nested in it, is replaced by its
// keyed hash, and every key nested in such a value, as a model's name a
// map is keyed by, by "#" and its keyed hash, but for a key of digits
// only, which a path reads as an index. PayloadHashes, taken from
// the payload as the host sent it, gives the same hash at the same path,
// so the payload still joins the record's rows, where such a value is a
// hash too.
func HideAccountValues(key []byte, v any) any {
	return hideUnder(key, v, false)
}

func hideUnder(key []byte, v any, hidden bool) any {
	switch t := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, x := range t {
			nk := k
			if hidden && !isIndex(k) {
				nk = hiddenKey(key, k)
			}
			out[nk] = hideUnder(key, x, hidden || accountKey(k))
		}
		return out
	case []any:
		out := make([]any, len(t))
		for i, x := range t {
			out[i] = hideUnder(key, x, hidden)
		}
		return out
	case string:
		if hidden {
			return Hash(key, t)
		}
	}
	return v
}

func accountKey(k string) bool {
	lower := strings.ToLower(k)
	for _, w := range accountWords {
		if strings.Contains(lower, w) {
			return true
		}
	}
	return false
}

// rowHashes gives the hash of every string of a decoded row.
func rowHashes(key []byte, v any, err error) map[string]bool {
	out := map[string]bool{}
	if err != nil {
		return out
	}
	walk(v, nil, func(_ []string, x any) {
		if s, ok := x.(string); ok {
			out[Hash(key, s)] = true
		}
	})
	return out
}

// walk calls fn for every value under v, the root's members first, with
// its path of keys and indexes; object keys are visited in sorted order.
func walk(v any, path []string, fn func(path []string, v any)) {
	switch t := v.(type) {
	case map[string]any:
		for _, k := range slices.Sorted(maps.Keys(t)) {
			p := append(slices.Clone(path), k)
			fn(p, t[k])
			walk(t[k], p, fn)
		}
	case []any:
		for i, x := range t {
			p := append(slices.Clone(path), strconv.Itoa(i))
			fn(p, x)
			walk(x, p, fn)
		}
	}
}

// field returns the string at a path of keys, or "".
func field(v any, keys ...string) string {
	for _, k := range keys {
		m, ok := v.(map[string]any)
		if !ok {
			return ""
		}
		v = m[k]
	}
	s, _ := v.(string)
	return s
}

func has(v any, keys ...string) bool {
	for _, k := range keys {
		m, ok := v.(map[string]any)
		if !ok {
			return false
		}
		if v, ok = m[k]; !ok {
			return false
		}
	}
	return v != nil
}

// turnStartRow reports a decoded Codex row that starts a turn.
func turnStartRow(host string, v any) bool {
	if host != string(adapter.Codex) {
		return false
	}
	pt := field(v, "payload", "type")
	return field(v, "type") == "event_msg" && (pt == "task_started" || pt == "turn_started")
}

var (
	enumShape = regexp.MustCompile(`^[a-z0-9_.-]{1,32}$`)
	idShape   = regexp.MustCompile(`^(?:[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}|(?:toolu|call|msg|resp)_[A-Za-z0-9]+)$`)
	keyShape  = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_-]{0,63}$`)
)

// IDShaped reports a string on the closed list of id shapes: a UUID, or
// toolu_, call_, msg_ or resp_ followed by letters and digits.
func IDShaped(s string) bool { return idShape.MatchString(s) }

// hashedWords are the words that keep a field's string a hash even where
// every enum-shaped string is kept: a model, an effort level, a plan or
// tier, a version, or a git branch, whose name is the account's.
var hashedWords = []string{"model", "effort", "tier", "plan", "version", "branch"}

// freeTextKeys are the keys under which a host writes text: what was said,
// a tool's output or input, a command. On a row type whose every
// enum-shaped string is kept, a string at or under one of these keys stays
// a hash, so a tool's one-word output or a lowercase token is never kept.
var freeTextKeys = map[string]bool{
	"content": true, "text": true, "stdout": true, "stderr": true, "output": true, "message": true,
	"prompt": true, "transcript": true, "tooluseresult": true, "result": true, "summary": true,
	"input": true, "command": true, "arguments": true, "args": true, "instructions": true,
}

// freeTextPath reports a path at or under a free-text key.
func freeTextPath(path []string) bool {
	for _, seg := range path {
		if freeTextKeys[strings.ToLower(seg)] {
			return true
		}
	}
	return false
}

// rowKind sorts a row by what the allowlist keeps of it.
type rowKind struct {
	core     bool // every enum-shaped string is kept
	codex    bool
	first    bool
	start    bool // a Codex turn-start row
	realtime bool
	response bool
	userRole bool
}

func kindOf(host string, v any, index int) rowKind {
	k := rowKind{codex: host == string(adapter.Codex), first: index == 0}
	t := field(v, "type")
	if k.codex {
		pt := field(v, "payload", "type")
		k.start = t == "event_msg" && (pt == "task_started" || pt == "turn_started")
		k.realtime = t == "realtime_item"
		k.response = t == "response_item"
		k.core = k.first || k.start || k.realtime
		k.userRole = field(v, "payload", "role") == "user" || pt == "user_message"
	} else {
		k.core = t == "user" || t == "system"
		k.userRole = t == "user" || field(v, "message", "role") == "user"
	}
	return k
}

// listed reports whether the enum allowlist names a path on a row of kind k.
// path holds the row's keys, an array's index as its number.
func listed(k rowKind, path []string) bool {
	p := strings.Join(path, ".")
	last := path[len(path)-1]
	if (last == "reason" || last == "source") && !freeTextPath(path[:len(path)-1]) {
		return true
	}
	if k.codex {
		switch {
		case p == "type" || p == "payload.type":
			return true
		case k.first && (p == "payload.originator" || strings.HasPrefix(p, "payload.source.")):
			return true
		case k.start && p == "payload.turn_attribution.turn_trigger":
			return true
		case k.realtime && (p == "payload.role" || p == "payload.outcome" || p == "payload.presentation.type"):
			return true
		case k.response && (p == "payload.role" || len(path) == 4 && path[1] == "content" && last == "type"):
			return true
		}
		return false
	}
	switch p {
	case "type", "subtype", "userType", "entrypoint", "level", "message.role":
		return true
	}
	return len(path) == 4 && path[0] == "message" && path[1] == "content" && last == "type"
}

func hashedPath(path []string) bool {
	for _, seg := range path {
		lower := strings.ToLower(seg)
		for _, w := range hashedWords {
			if strings.Contains(lower, w) {
				return true
			}
		}
	}
	return false
}

// PathString writes a path as the recording does: keys and indexes joined
// by dots, a key that is not shaped like a name replaced by # and its hash.
func PathString(key []byte, path []string) string {
	out := make([]string, len(path))
	for i, seg := range path {
		if keyShape.MatchString(seg) || isIndex(seg) {
			out[i] = seg
		} else {
			out[i] = "#" + Hash(key, seg)
		}
	}
	return strings.Join(out, ".")
}

func isIndex(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

func describeRow(in SnapshotInput, v any, err error, index int) Row {
	r := Row{Index: index}
	if err != nil {
		r.Invalid = true
		return r
	}
	if t := field(v, "type"); enumShape.MatchString(t) {
		r.RowType = t
	}
	k := kindOf(in.Host, v, index)
	walk(v, nil, func(path []string, x any) {
		if in.MaxFields > 0 && len(r.Fields) >= in.MaxFields {
			r.Cut = true
			return
		}
		r.Fields = append(r.Fields, describe(in, k, path, x))
	})
	if k.start {
		r.Derived = map[string]bool{
			"parent_turn_id_present": has(v, "payload", "turn_attribution", "parent_turn_id"),
		}
		root := field(v, "payload", "turn_attribution", "root_turn_id")
		turn := field(v, "payload", "turn_id")
		if turn == "" {
			turn = field(v, "payload", "turn_attribution", "turn_id")
		}
		if root != "" && turn != "" {
			r.Derived["root_turn_id_is_turn_id"] = root == turn
		}
	}
	return r
}

func describe(in SnapshotInput, k rowKind, path []string, x any) Field {
	f := Field{Path: PathString(in.Key, path)}
	switch t := x.(type) {
	case nil:
		f.Type = "null"
	case bool:
		f.Type, f.Value = "bool", t
	case map[string]any:
		n := len(t)
		f.Type, f.Items = "object", &n
	case []any:
		n := len(t)
		f.Type, f.Items = "array", &n
	case json.Number:
		f.Type = "number"
		f.Sign, f.Digits, f.Frac = numberShape(string(t))
	case string:
		f.Type = "string"
		switch {
		case enumShape.MatchString(t) && !hashedPath(path) && (listed(k, path) || k.core && !freeTextPath(path)):
			f.Value = t
		case IDShaped(t):
			f.Value = t
		default:
			f.Len = len(t)
			f.Hash = Hash(in.Key, t)
			f.Voice, _ = VoiceOf(in.Key, t)
			if tag, ok := Vocab().LeadingTag(t); ok {
				f.Tag = tag
			} else if k.userRole && !in.PayloadHashes[f.Hash] {
				f.Tag, _ = UnlistedTag(t)
			}
		}
	}
	return f
}

// numberShape gives a number's sign, its count of digits before any point,
// and whether it has a fraction.
func numberShape(s string) (sign string, digits int, frac bool) {
	sign = "+"
	if strings.HasPrefix(s, "-") {
		sign, s = "-", s[1:]
	}
	mant, _, _ := strings.Cut(strings.ToLower(s), "e")
	whole, fraction, ok := strings.Cut(mant, ".")
	whole = strings.TrimLeft(whole, "0")
	if whole == "" && (!ok || strings.Trim(fraction, "0") == "") {
		return "0", 0, false
	}
	return sign, len(whole), ok && strings.Trim(fraction, "0") != ""
}
