package profile

import (
	"bytes"
	"encoding/json"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"
)

// entry is one key of a frontmatter block, with the lines that belong to it.
type entry struct {
	key       string
	line      int      // 1-based line of the key
	rest      string   // what follows "key:" on its line
	cont      []string // continuation lines: indented or "- " lines after it
	canonical bool     // the key is bare, at the line's start, the colon right after it
	explicit  bool     // an explicit "? key", its value on a ": value" line
	written   bool     // in a written form of the spec's section 4
	val       any      // the decoded value when written
	raw       string   // the scalar read leniently, for a line in no written form
}

// doc is a document read for checking.
type doc struct {
	path       string
	loc        loc
	unreadable string // why it cannot be read; "" when it can
	entries    []*entry
	opaque     []int    // lines of the block that belong to no key
	body       []string // the lines after the closing ---
	hand       bool     // the hand-edit record lists it
}

var (
	reKeyLine  = regexp.MustCompile(`^([A-Za-z_][A-Za-z0-9_-]*):(.*)$`)
	rePlain    = regexp.MustCompile(`^[a-z][a-z0-9_.-]*$`)
	yamlWords  = map[string]bool{"y": true, "n": true, "yes": true, "no": true, "on": true, "off": true, "true": true, "false": true, "null": true}
	reConflict = regexp.MustCompile(`^(<<<<<<<|=======|>>>>>>>|\|\|\|\|\|\|\|)( |$)`)
)

// unreadableReason says why b cannot be read as text, or "".
func unreadableReason(b []byte) string {
	switch {
	case bytes.HasPrefix(b, []byte("\xef\xbb\xbf")):
		return "a byte-order mark"
	case !utf8.Valid(b):
		return "invalid UTF-8"
	case bytes.IndexByte(b, 0) >= 0:
		return "a NUL byte"
	}
	for i, line := range strings.Split(string(b), "\n") {
		if len(line) > 64<<10 {
			return "line " + itoa(i+1) + " is over 64 KiB"
		}
		if reConflict.MatchString(strings.TrimSuffix(line, "\r")) {
			return "a conflict marker on line " + itoa(i+1)
		}
	}
	return ""
}

// parseDoc reads a document: its frontmatter block and its body.
func parseDoc(path string, b []byte) *doc {
	d := &doc{path: path}
	if d.unreadable = unreadableReason(b); d.unreadable != "" {
		return d
	}
	lines := strings.Split(strings.TrimSuffix(string(b), "\n"), "\n")
	for i := range lines {
		lines[i] = strings.TrimSuffix(lines[i], "\r")
	}
	if lines[0] != "---" {
		d.unreadable = "no frontmatter on line 1"
		return d
	}
	end := -1
	for i := 1; i < len(lines); i++ {
		if lines[i] == "---" {
			end = i
			break
		}
	}
	if end < 0 {
		d.unreadable = "a frontmatter that does not close"
		return d
	}
	// The block's top level is the indentation of its first line that is
	// neither blank nor a comment: YAML allows a whole block indented.
	base := 0
	for i := 1; i < end; i++ {
		if t := strings.TrimLeft(lines[i], " "); t != "" && !strings.HasPrefix(t, "#") {
			base = len(lines[i]) - len(t)
			break
		}
	}
	var last *entry
	for i := 1; i < end; i++ {
		l := lines[i]
		ind := len(l) - len(strings.TrimLeft(l, " "))
		if ind == base {
			s := l[base:]
			if m := reKeyLine.FindStringSubmatch(s); m != nil {
				last = &entry{key: m[1], line: i + 1, rest: m[2], canonical: base == 0}
				d.entries = append(d.entries, last)
				continue
			}
			if key, rest, explicit, ok := yamlKey(s); ok {
				last = &entry{key: key, line: i + 1, rest: rest, explicit: explicit}
				d.entries = append(d.entries, last)
				continue
			}
			if last != nil && last.explicit && strings.HasPrefix(s, ":") && last.rest == "" && len(last.cont) == 0 {
				last.rest = s[1:]
				continue
			}
		}
		if last != nil && (ind > base || strings.HasPrefix(l[ind:], "-")) {
			last.cont = append(last.cont, l)
			continue
		}
		d.opaque = append(d.opaque, i+1)
	}
	for _, e := range d.entries {
		e.written, e.val = writtenValue(e)
		e.raw = lenient(e.rest)
	}
	d.body = lines[end+1:]
	return d
}

// writtenValue reports whether an entry is in a written form, and its
// value when it is.
func writtenValue(e *entry) (bool, any) {
	if !e.canonical {
		return false, nil
	}
	if e.rest == "" {
		if len(e.cont) == 0 {
			return false, nil
		}
		var list []any
		for _, l := range e.cont {
			v, ok := canonicalJSON(strings.TrimPrefix(l, "  - "))
			if _, isObj := v.(map[string]any); !ok || !isObj || !strings.HasPrefix(l, "  - ") {
				return false, nil
			}
			list = append(list, v)
		}
		return true, list
	}
	if len(e.cont) > 0 || !strings.HasPrefix(e.rest, " ") {
		return false, nil
	}
	s := e.rest[1:]
	if rePlain.MatchString(s) && !yamlWords[s] {
		return true, s
	}
	switch {
	case strings.HasPrefix(s, `"`), strings.HasPrefix(s, "["), strings.HasPrefix(s, "{"):
		if v, ok := canonicalJSON(s); ok {
			return true, v
		}
	}
	return false, nil
}

// canonicalJSON decodes s when it is JSON with one space after each colon
// and comma outside strings, and no other space outside strings.
func canonicalJSON(s string) (any, bool) {
	var buf bytes.Buffer
	if json.Compact(&buf, []byte(s)) != nil {
		return nil, false
	}
	var spaced strings.Builder
	in, esc := false, false
	for _, c := range buf.String() {
		spaced.WriteRune(c)
		switch {
		case esc:
			esc = false
		case in && c == '\\':
			esc = true
		case c == '"':
			in = !in
		case !in && (c == ':' || c == ','):
			spaced.WriteByte(' ')
		}
	}
	if spaced.String() != s {
		return nil, false
	}
	dec := json.NewDecoder(strings.NewReader(s))
	dec.UseNumber()
	var v any
	if dec.Decode(&v) != nil {
		return nil, false
	}
	return v, true
}

// lenient reads a scalar as YAML would, for a line a person wrote: a
// quoted string is unquoted, and a comment after a plain value is dropped.
func lenient(rest string) string {
	s := strings.TrimSpace(rest)
	switch {
	case strings.HasPrefix(s, `"`):
		var v string
		dec := json.NewDecoder(strings.NewReader(s))
		if dec.Decode(&v) == nil {
			return v
		}
		return s
	case strings.HasPrefix(s, "'"):
		var b strings.Builder
		for i := 1; i < len(s); i++ {
			if s[i] != '\'' {
				b.WriteByte(s[i])
			} else if i+1 < len(s) && s[i+1] == '\'' {
				b.WriteByte('\'')
				i++
			} else {
				return b.String()
			}
		}
		return s
	}
	if i := strings.Index(s, " #"); i >= 0 {
		s = strings.TrimSpace(s[:i])
	}
	if strings.HasPrefix(s, "#") {
		return ""
	}
	return s
}

// quoted reports a value written in quotes.
func quoted(rest string) bool {
	s := strings.TrimSpace(rest)
	return strings.HasPrefix(s, `"`) || strings.HasPrefix(s, "'")
}

// Protection says how a file refuses the operations aimed at it.
type Protection struct {
	Locked, Verified, Unreadable bool
}

// protection reads locked and verified fail-closed: in any form a person
// may have written them.
func (d *doc) protection() Protection {
	if d.unreadable != "" {
		return Protection{Unreadable: true}
	}
	var p Protection
	n := 0
	for _, e := range d.entries {
		switch e.key {
		case unknownKey, mergeKey:
			// Either may stand for locked or verified.
			p.Locked, p.Verified = true, true
		case "locked":
			n++
			// Only the bare key is read by its value; any other form a
			// person wrote locks.
			unlocked := e.canonical && !quoted(e.rest) && len(e.cont) == 0 && (e.raw == "false" || e.raw == "False" || e.raw == "FALSE")
			p.Locked = p.Locked || !unlocked
		case "verified":
			empty := e.canonical && len(e.cont) == 0 && map[string]bool{"": true, "[]": true, "{}": true, `""`: true, "null": true, "~": true}[strings.TrimSpace(stripComment(e.rest))]
			p.Verified = p.Verified || !empty
		}
	}
	if n > 1 {
		p.Locked = true
	}
	return p
}

// unknownKey and mergeKey are keys protection reads as both locked and
// verified. An unknown key cannot be read from its line: an alias, which
// names a node elsewhere, or an explicit key whose node is not a plain or
// quoted scalar on the line. A merge copies another mapping's keys in.
const (
	unknownKey = "*"
	mergeKey   = "<<"
)

// yamlKey reads a line that YAML reads as a mapping key in a form other
// than the bare "key:" of the written forms: a quoted key, spaces or tabs
// before the colon, an explicit "? key", a tag or an anchor before the
// key, or an alias as the key. It returns the key, what follows its colon,
// and whether the key is explicit; ok is false for a line it cannot read
// as a key.
func yamlKey(s string) (key, rest string, explicit, ok bool) {
	if s == "?" || strings.HasPrefix(s, "? ") || strings.HasPrefix(s, "?\t") {
		key, after, ok := readKey(strings.TrimLeft(s[1:], " \t"), true)
		if !ok {
			return unknownKey, "", true, true
		}
		return key, strings.TrimPrefix(after, ":"), true, true
	}
	key, after, ok := readKey(s, false)
	if !ok || !strings.HasPrefix(after, ":") {
		return "", "", false, false
	}
	return key, after[1:], false, true
}

// readKey reads the key at the start of s, after any tag or anchor, and
// returns it and what follows it, spaces dropped. Unless explicit, a plain
// key must end in a colon.
func readKey(s string, explicit bool) (key, after string, ok bool) {
	for strings.HasPrefix(s, "!") || strings.HasPrefix(s, "&") {
		i := strings.IndexAny(s, " \t")
		if i < 0 {
			return "", "", false
		}
		s = strings.TrimLeft(s[i:], " \t")
	}
	if s == "" {
		return "", "", false
	}
	switch c := s[0]; {
	case c == '"':
		end := -1
		for i := 1; i < len(s) && end < 0; i++ {
			if s[i] == '\\' {
				i++
			} else if s[i] == '"' {
				end = i
			}
		}
		if end < 0 {
			return "", "", false
		}
		key, after = yamlUnescape(s[1:end]), s[end+1:]
	case c == '\'':
		var b strings.Builder
		end := -1
		for i := 1; i < len(s) && end < 0; i++ {
			switch {
			case s[i] != '\'':
				b.WriteByte(s[i])
			case i+1 < len(s) && s[i+1] == '\'':
				b.WriteByte('\'')
				i++
			default:
				end = i
			}
		}
		if end < 0 {
			return "", "", false
		}
		key, after = b.String(), s[end+1:]
	case c == '*':
		i := strings.IndexAny(s, " \t")
		if i < 0 {
			i = len(s)
		}
		key, after = unknownKey, s[i:]
	case strings.IndexByte("-#,[]{}|>%@`:", c) >= 0:
		return "", "", false
	default:
		i := plainKeyEnd(s)
		if i < 0 {
			if !explicit {
				return "", "", false
			}
			i = len(s)
			if j := strings.Index(s, " #"); j >= 0 {
				i = j
			}
		}
		key, after = strings.TrimRight(s[:i], " \t"), s[i:]
	}
	return key, strings.TrimLeft(after, " \t"), true
}

// plainKeyEnd is the index of the colon that ends a plain key: the first
// one followed by a space, a tab or the line's end, before any comment.
func plainKeyEnd(s string) int {
	for i := 0; i < len(s); i++ {
		switch {
		case s[i] == '#' && i > 0 && (s[i-1] == ' ' || s[i-1] == '\t'):
			return -1
		case s[i] == ':' && (i+1 == len(s) || s[i+1] == ' ' || s[i+1] == '\t'):
			return i
		}
	}
	return -1
}

// yamlUnescape reads the escapes of a double-quoted YAML scalar. An escape
// YAML does not know is kept as written.
func yamlUnescape(s string) string {
	simple := map[byte]string{'0': "\x00", 'a': "\a", 'b': "\b", 't': "\t", '\t': "\t", 'n': "\n", 'v': "\v", 'f': "\f",
		'r': "\r", 'e': "\x1b", ' ': " ", '"': `"`, '/': "/", '\\': `\`, 'N': "\u0085", '_': " ", 'L': " ", 'P': " "}
	width := map[byte]int{'x': 2, 'u': 4, 'U': 8}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] != '\\' || i+1 == len(s) {
			b.WriteByte(s[i])
			continue
		}
		c := s[i+1]
		if r, ok := simple[c]; ok {
			b.WriteString(r)
			i++
			continue
		}
		if n := width[c]; n > 0 && i+2+n <= len(s) {
			if v, err := strconv.ParseUint(s[i+2:i+2+n], 16, 32); err == nil && utf8.ValidRune(rune(v)) {
				b.WriteRune(rune(v))
				i += 1 + n
				continue
			}
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

// stripComment drops a comment after an unquoted value.
func stripComment(s string) string {
	if quoted(s) {
		return s
	}
	if i := strings.Index(s, " #"); i >= 0 {
		return s[:i]
	}
	return s
}

// get returns the entry for key, or nil.
func (d *doc) get(key string) *entry {
	for _, e := range d.entries {
		if e.key == key {
			return e
		}
	}
	return nil
}
