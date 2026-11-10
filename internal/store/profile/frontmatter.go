package profile

import (
	"bytes"
	"encoding/json"
	"regexp"
	"strings"
	"unicode/utf8"
)

// entry is one key of a frontmatter block, with the lines that belong to it.
type entry struct {
	key     string
	line    int      // 1-based line of the key
	rest    string   // what follows "key:" on its line
	cont    []string // continuation lines: indented or "- " lines after it
	written bool     // in a written form of the spec's section 4
	val     any      // the decoded value when written
	raw     string   // the scalar read leniently, for a line in no written form
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
	var last *entry
	for i := 1; i < end; i++ {
		l := lines[i]
		if m := reKeyLine.FindStringSubmatch(l); m != nil {
			last = &entry{key: m[1], line: i + 1, rest: m[2]}
			d.entries = append(d.entries, last)
			continue
		}
		if last != nil && (strings.HasPrefix(l, " ") || strings.HasPrefix(l, "-")) {
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
		if i := strings.Index(s[1:], "'"); i >= 0 {
			return strings.ReplaceAll(s[1:1+i], "''", "'")
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
		case "locked":
			n++
			unlocked := !quoted(e.rest) && len(e.cont) == 0 && (e.raw == "false" || e.raw == "False" || e.raw == "FALSE")
			p.Locked = p.Locked || !unlocked
		case "verified":
			empty := len(e.cont) == 0 && map[string]bool{"": true, "[]": true, "{}": true, `""`: true, "null": true, "~": true}[strings.TrimSpace(stripComment(e.rest))]
			p.Verified = p.Verified || !empty
		}
	}
	if n > 1 {
		p.Locked = true
	}
	return p
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
