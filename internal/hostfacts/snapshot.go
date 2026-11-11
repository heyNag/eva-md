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
}

// TakeSnapshot describes the rows of in.Path from in.From, and gives the
// offset and row number the next snapshot starts from. A last line without
// its newline is left for the next snapshot.
func TakeSnapshot(in SnapshotInput) (snap Snapshot, next int64, index int, err error) {
	next, index = in.From, in.Index
	f, err := os.Open(in.Path)
	if errors.Is(err, os.ErrNotExist) {
		return Snapshot{Missing: true}, next, index, nil
	}
	if err != nil {
		return snap, next, index, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return snap, next, index, err
	}
	snap.Size, snap.Mtime = info.Size(), info.ModTime().UTC().Format(time.RFC3339Nano)
	if in.From > info.Size() {
		// The file was replaced by a shorter one: start again.
		in.From, in.Index = 0, 0
		next, index = 0, 0
	}
	whole := info.Size() <= in.MaxBytes
	start := in.From
	if whole {
		start = 0
	}
	limit := in.MaxBytes
	if _, err := f.Seek(start, io.SeekStart); err != nil {
		return snap, next, index, err
	}
	data, err := io.ReadAll(io.LimitReader(f, limit))
	if err != nil {
		return snap, next, index, err
	}
	if int64(len(data)) < info.Size()-start {
		snap.Cut = true
	}
	seen := map[string]bool{}
	var turnStarts []map[string]bool
	offset, row := start, 0
	if !whole {
		row = in.Index
	}
	for len(data) > 0 {
		nl := bytes.IndexByte(data, '\n')
		if nl < 0 {
			snap.PartialTail = len(data)
			break
		}
		line := data[:nl]
		data = data[nl+1:]
		r := describeRow(in, line, row)
		r.Offset, r.Size = offset, len(line)
		hashes := rowHashes(in.Key, line)
		for h := range hashes {
			seen[h] = true
		}
		if turnStartRow(in.Host, line) {
			turnStarts = append(turnStarts, hashes)
		}
		if offset >= in.From {
			snap.Rows = append(snap.Rows, r)
			next, index = offset+int64(nl)+1, row+1
		}
		offset += int64(nl) + 1
		row++
	}
	if whole {
		p := &Presence{}
		if in.Reply != "" {
			p.Reply = ptr(seen[in.Reply])
		}
		if in.Prompt != "" {
			p.Prompt = ptr(seen[in.Prompt])
		}
		if in.Turn != "" && in.Host == string(adapter.Codex) {
			found := false
			for _, hs := range turnStarts {
				found = found || hs[in.Turn]
			}
			p.TurnStart = ptr(found)
		}
		if p.Reply != nil || p.Prompt != nil || p.TurnStart != nil {
			snap.Presence = p
		}
	}
	return snap, next, index, nil
}

func ptr(b bool) *bool { return &b }

// PayloadHashes gives the keyed hash of each string of a decoded payload,
// by its field path as PathString writes it.
func PayloadHashes(key []byte, v any) map[string]string {
	out := map[string]string{}
	walk(v, nil, func(path []string, x any) {
		if s, ok := x.(string); ok {
			out[PathString(key, path)] = Hash(key, s)
		}
	})
	return out
}

// rowHashes gives the hash of every string of a row.
func rowHashes(key, line []byte) map[string]bool {
	out := map[string]bool{}
	v, err := Decode(line)
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

// turnStartRow reports a Codex row that starts a turn.
func turnStartRow(host string, line []byte) bool {
	if host != string(adapter.Codex) {
		return false
	}
	v, err := Decode(line)
	if err != nil {
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
// tier, or a version.
var hashedWords = []string{"model", "effort", "tier", "plan", "version"}

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
	if last == "reason" || last == "source" {
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

func describeRow(in SnapshotInput, line []byte, index int) Row {
	r := Row{Index: index}
	v, err := Decode(line)
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
		case enumShape.MatchString(t) && !hashedPath(path) && (listed(k, path) || k.core):
			f.Value = t
		case IDShaped(t):
			f.Value = t
		default:
			f.Len = len(t)
			f.Hash = Hash(in.Key, t)
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
