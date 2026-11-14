package codec

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// kind is what a line of the frontmatter is.
type kind byte

const (
	opaque    kind = iota // belongs to no key
	keyLine               // starts an entry
	contLine              // continues the entry above it: indented, or "-"
	valueLine             // ": value" under an explicit "? key"
)

// entry is one key of the frontmatter, with the lines that belong to it.
type entry struct {
	key       string
	lines     []int    // the indexes of its lines, the key's first
	rest      string   // what follows the key's colon
	cont      []string // its continuation lines, without their ends
	canonical bool     // the bare key at the line's start, the colon right after it
	explicit  bool     // an explicit "? key"
	valued    bool     // an explicit key that has its ": value" line
}

var (
	// reKeyLine is a bare key: its colon is followed by a space, a tab or
	// the line's end, as YAML requires of a key's colon.
	reKeyLine = regexp.MustCompile(`^([A-Za-z_][A-Za-z0-9_-]*):((?:[ \t].*)?)$`)
	// reTightKey is a line that would be a bare key but for the space after
	// its colon, "locked:false": no key to YAML, so it protects as one.
	reTightKey = regexp.MustCompile(`^([A-Za-z_][A-Za-z0-9_-]*):[^ \t]`)
	// reAnchor and reAlias find an anchor or an alias at the start of a
	// node; either may be in a quoted string, which only refuses more.
	reAnchor  = regexp.MustCompile("(^|[ \t\\[{,])&[^ \t,\\[\\]{}]")
	reAlias   = regexp.MustCompile("(^|[ \t\\[{,])\\*[^ \t,\\[\\]{}]")
	rePlain   = regexp.MustCompile(`^[a-z][a-z0-9_.-]*$`)
	reKey     = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)
	yamlWords = map[string]bool{"y": true, "n": true, "yes": true, "no": true, "on": true, "off": true, "true": true, "false": true, "null": true}
)

// readFront reads the frontmatter's lines into entries and opaque runs.
func (d *Doc) readFront() {
	d.kinds = make([]kind, d.close)
	// The top level is the indentation of the block's first line that is
	// neither blank nor a comment: YAML allows a whole block indented.
	for i := 1; i < d.close; i++ {
		l := text(d.lines[i])
		if t := strings.TrimLeft(l, " "); t != "" && !strings.HasPrefix(t, "#") {
			d.base = len(l) - len(t)
			break
		}
	}
	var last *entry
	add := func(e *entry, i int) {
		e.lines = []int{i}
		d.entries = append(d.entries, e)
		d.kinds[i] = keyLine
		last = e
	}
	for i := 1; i < d.close; i++ {
		l := text(d.lines[i])
		ind := len(l) - len(strings.TrimLeft(l, " "))
		if ind == d.base {
			s := l[d.base:]
			if m := reKeyLine.FindStringSubmatch(s); m != nil {
				add(&entry{key: m[1], rest: m[2], canonical: d.base == 0}, i)
				continue
			}
			if key, rest, explicit, ok := yamlKey(s); ok {
				add(&entry{key: key, rest: rest, explicit: explicit}, i)
				continue
			}
			// An explicit key's value line follows its key, and any lines
			// that continue the key: "? a" / "  b" / ": v" is the key "a b".
			if last != nil && last.explicit && strings.HasPrefix(s, ":") && !last.valued {
				last.rest, last.valued = s[1:], true
				last.lines = append(last.lines, i)
				d.kinds[i] = valueLine
				continue
			}
		}
		if last != nil && (ind > d.base || strings.HasPrefix(l[ind:], "-")) {
			last.cont = append(last.cont, l)
			last.lines = append(last.lines, i)
			d.kinds[i] = contLine
		}
	}
}

// find returns the entries for key.
func (d *Doc) find(key string) []*entry {
	var out []*entry
	for _, e := range d.entries {
		if e.key == key {
			out = append(out, e)
		}
	}
	return out
}

// Protection reads locked and verified fail-closed, in any form a person
// may have written them. An unreadable document is Unreadable and nothing
// else.
func (d *Doc) Protection() Protection {
	if d.why() != "" {
		return Protection{Unreadable: true}
	}
	var p Protection
	if d.flowRoot() || d.hiddenBreak() {
		// The block is one flow mapping, or has a line YAML breaks where
		// the codec does not: it may hold either key on lines the codec
		// does not read as entries.
		p.Locked, p.Verified = true, true
	}
	// Only the bare key at a line's start is read by its value: any other
	// line that names a mark, as a lenient reader may read it, protects.
	canonical := map[int]bool{}
	for _, e := range d.entries {
		canonical[e.lines[0]] = e.canonical
	}
	// A line that belongs to no key but reads as a protection key, such as
	// one indented less than the block or with a tab or U+FEFF before it,
	// is a mark a person wrote; so is a key line with no space after its
	// colon.
	for i := 1; i < d.close; i++ {
		if d.kinds[i] != opaque && d.kinds[i] != keyLine {
			continue
		}
		if !canonical[i] {
			switch looseKey(text(d.lines[i])) {
			case "locked":
				p.Locked = true
			case "verified":
				p.Verified = true
			case unknownKey:
				p.Locked, p.Verified = true, true
			}
		}
		s := unlead(text(d.lines[i]))
		key, ok := "", false
		switch m := reTightKey.FindStringSubmatch(s); {
		case m != nil:
			key, ok = m[1], true
		case d.kinds[i] != opaque:
		case strings.HasPrefix(s, "*"):
			key, ok = unknownKey, true
		default:
			key, _, _, ok = yamlKey(s)
		}
		switch {
		case !ok:
		case key == "locked":
			p.Locked = true
		case key == "verified":
			p.Verified = true
		case key == unknownKey || key == mergeKey:
			p.Locked, p.Verified = true, true
		}
	}
	n := 0
	for _, e := range d.entries {
		// Only the bare key, its value after a space or a tab, is read by
		// its value; any other form a person wrote protects.
		bare := e.canonical && len(e.cont) == 0 && (e.rest == "" || e.rest[0] == ' ' || e.rest[0] == '\t')
		key := e.key
		if w := strings.TrimFunc(key, loose); w == "locked" || w == "verified" {
			// "'locked '", or "locked" after a no-break space: a reader
			// that trims more than YAML does reads the mark.
			key = w
		}
		switch key {
		case unknownKey, mergeKey:
			// Either may stand for locked or verified.
			p.Locked, p.Verified = true, true
		case "locked":
			n++
			raw := lenient(e.rest)
			unlocked := bare && !quoted(e.rest) && (raw == "false" || raw == "False" || raw == "FALSE")
			p.Locked = p.Locked || !unlocked
		case "verified":
			empty := bare && map[string]bool{"": true, "[]": true, "{}": true, `""`: true, "null": true, "~": true}[strings.Trim(stripComment(e.rest), " \t")]
			if empty && d.tabUnder(e.lines[0]) {
				// "verified:" then "\tby: river": YAML refuses the tab, and
				// a reader that takes it for indentation, as v1 did, reads a
				// line under the key.
				empty = false
			}
			p.Verified = p.Verified || !empty
		}
	}
	if n > 1 {
		p.Locked = true
	}
	return p
}

// tabUnder reports whether the first line after line i of the frontmatter
// that is not blank, as Go trims white space, starts with a tab.
func (d *Doc) tabUnder(i int) bool {
	for j := i + 1; j < d.close; j++ {
		if l := text(d.lines[j]); strings.TrimSpace(l) != "" {
			return strings.HasPrefix(l, "\t")
		}
	}
	return false
}

// Value decodes a key in a written form: a plain scalar as a string, JSON
// with numbers as json.Number, a block list as a list of objects. It
// returns ErrNoKey, ErrRepeated or ErrNotWritten, and never guesses; an
// unreadable document has no value it can decode.
func (d *Doc) Value(key string) (any, error) {
	if d.why() != "" {
		return nil, ErrNotWritten
	}
	es := d.find(key)
	switch {
	case len(es) == 0:
		return nil, ErrNoKey
	case len(es) > 1:
		return nil, ErrRepeated
	}
	v, ok := writtenValue(es[0])
	if !ok {
		return nil, ErrNotWritten
	}
	return v, nil
}

// writtenValue decodes an entry in a written form of the format's
// section 4, and reports whether it is in one.
func writtenValue(e *entry) (any, bool) {
	if !e.canonical {
		return nil, false
	}
	if e.rest == "" {
		if len(e.cont) == 0 {
			return nil, false
		}
		var list []any
		for _, l := range e.cont {
			s, ok := strings.CutPrefix(l, "  - ")
			v, isJSON := canonicalJSON(s)
			if _, isObj := v.(map[string]any); !ok || !isJSON || !isObj {
				return nil, false
			}
			list = append(list, v)
		}
		return list, true
	}
	if len(e.cont) > 0 || !strings.HasPrefix(e.rest, " ") {
		return nil, false
	}
	s := e.rest[1:]
	if rePlain.MatchString(s) && !yamlWords[s] {
		return s, true
	}
	if strings.HasPrefix(s, `"`) || strings.HasPrefix(s, "[") || strings.HasPrefix(s, "{") {
		return canonicalJSON(s)
	}
	return nil, false
}

// canonicalJSON decodes s when it is JSON with one space after each colon
// and comma outside strings, and no other space outside strings.
func canonicalJSON(s string) (any, bool) {
	var buf bytes.Buffer
	if json.Compact(&buf, []byte(s)) != nil || spaced(buf.Bytes()) != s {
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

// spaced puts one space after each colon and comma outside strings of
// compact JSON.
func spaced(b []byte) string {
	var out strings.Builder
	in, esc := false, false
	for _, c := range b {
		out.WriteByte(c)
		switch {
		case esc:
			esc = false
		case in && c == '\\':
			esc = true
		case c == '"':
			in = !in
		case !in && (c == ':' || c == ','):
			out.WriteByte(' ')
		}
	}
	return out.String()
}

// marshal is v as compact JSON, with HTML escaping off, and each character
// YAML does not allow in a document escaped. A string that is not valid
// UTF-8 is refused: encoding/json would write U+FFFD in its place. The
// error says nothing of v, which may be text a caller must not log.
func marshal(v any) ([]byte, error) {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, fmt.Errorf("%w: the value is not one JSON can hold", ErrNotWritten)
	}
	j := bytes.TrimSuffix(b.Bytes(), []byte("\n"))
	if !utf8.Valid(j) || replaced(j) {
		return nil, fmt.Errorf("%w: a string in the value is not valid UTF-8", ErrNotWritten)
	}
	return yamlSafe(j), nil
}

// replaced reports a \ufffd escape in JSON: what encoding/json writes for
// a byte that is not UTF-8. It writes U+FFFD itself as the character, so
// only a json.RawMessage holding the escape is refused with it.
func replaced(j []byte) bool {
	in := false
	for i := 0; i < len(j); i++ {
		switch {
		case j[i] == '"':
			in = !in
		case in && j[i] == '\\':
			if bytes.HasPrefix(j[i+1:], []byte("ufffd")) || bytes.HasPrefix(j[i+1:], []byte("uFFFD")) {
				return true
			}
			i++
		}
	}
	return false
}

// yamlSafe escapes each character of JSON that YAML does not allow in a
// document, which encoding/json writes as it is: DEL, the C1 controls
// (NEL among them, at which a YAML reader may break a line), U+FFFE and
// U+FFFF. Outside strings JSON holds none of them, and inside one the
// escape is the same value.
func yamlSafe(j []byte) []byte {
	if !bytes.ContainsFunc(j, notYAML) {
		return j
	}
	var out []byte
	for _, r := range string(j) {
		if notYAML(r) {
			out = fmt.Appendf(out, `\u%04x`, r)
		} else {
			out = utf8.AppendRune(out, r)
		}
	}
	return out
}

// notYAML reports a character, valid in JSON text, that is not printable
// to YAML.
func notYAML(r rune) bool {
	return r == 0x7f || r >= 0x80 && r <= 0x9f || r == 0xfffe || r == 0xffff
}

// form is the written form of v: plain for a string in the plain alphabet
// that is not a YAML word, otherwise spaced JSON. A value that is not a
// string, list or object is refused.
func form(v any) (string, error) {
	j, err := marshal(v)
	if err != nil {
		return "", err
	}
	switch j[0] {
	case '"':
		var s string
		if json.Unmarshal(j, &s) == nil && rePlain.MatchString(s) && !yamlWords[s] {
			return s, nil
		}
	case '[', '{':
	default:
		return "", fmt.Errorf("%w: the value is not a string, list or object", ErrNotWritten)
	}
	return spaced(j), nil
}

// Set writes key as "key: value" in the written form of v, in place of the
// entry for key in whatever form it was written, or after the last entry.
// It refuses a repeated key.
func (d *Doc) Set(key string, v any) error {
	if err := d.editable(key); err != nil {
		return err
	}
	f, err := form(v)
	if err != nil {
		return err
	}
	return d.put(key, []string{key + ": " + f + "\n"})
}

// SetBlock writes key as "key:" and then one "  - " line for each object
// of list, which must marshal to a non-empty JSON list of objects.
func (d *Doc) SetBlock(key string, list any) error {
	if err := d.editable(key); err != nil {
		return err
	}
	j, err := marshal(list)
	if err != nil {
		return err
	}
	var items []json.RawMessage
	if j[0] != '[' || json.Unmarshal(j, &items) != nil || len(items) == 0 {
		return fmt.Errorf("%w: a block is a list of one or more objects", ErrNotWritten)
	}
	lines := []string{key + ":\n"}
	for _, it := range items {
		if it[0] != '{' {
			return fmt.Errorf("%w: a block is a list of one or more objects", ErrNotWritten)
		}
		lines = append(lines, "  - "+spaced(it)+"\n")
	}
	return d.put(key, lines)
}

// Delete removes the entry for key and every line under it.
func (d *Doc) Delete(key string) error {
	if err := d.editable(key); err != nil {
		return err
	}
	if len(d.find(key)) == 0 {
		return ErrNoKey
	}
	return d.put(key, nil)
}

// editable refuses an edit of key the codec may not make.
func (d *Doc) editable(key string) error {
	switch {
	case d.why() != "" || d.Protection().Locked || key == "locked":
		return ErrProtected
	case !reKey.MatchString(key) || yamlWords[key]:
		return fmt.Errorf("%w: the key is not one the codec writes", ErrNotWritten)
	case d.base != 0:
		return fmt.Errorf("%w: the frontmatter is indented as a whole", ErrNotWritten)
	}
	return nil
}

// put replaces the lines of key's entry with lines, where its first line
// was, or adds them after the last entry's last line; no lines removes
// it. The result must read back with every other line as it was and the
// entry as written, or nothing changes.
func (d *Doc) put(key string, lines []string) error {
	es := d.find(key)
	if len(es) > 1 {
		return ErrRepeated
	}
	for _, e := range d.entries {
		if opens(e) {
			// A YAML reader continues the value onto lines the codec reads
			// on their own, so an edit next to it, or of it, may change
			// what another line holds.
			return fmt.Errorf("%w: a value does not close on its lines", ErrNotWritten)
		}
	}
	var out []string
	if len(es) == 1 {
		own := d.owned(key)
		if d.removesAnchor(own) {
			return fmt.Errorf("%w: the entry defines an anchor that another line may name", ErrNotWritten)
		}
		for i, l := range d.lines {
			if i == es[0].lines[0] {
				out = append(out, lines...)
			}
			if !own[i] {
				out = append(out, l)
			}
		}
	} else {
		at := d.close
		if n := len(d.entries); n > 0 {
			last := d.entries[n-1]
			at = last.lines[len(last.lines)-1] + 1
		}
		out = slices.Concat(d.lines[:at], lines, d.lines[at:])
	}
	nd := Read([]byte(strings.Join(out, "")))
	if why := nd.why(); why != "" {
		return fmt.Errorf("%w: the edit would leave %s", ErrNotWritten, why)
	}
	if !slices.Equal(d.others(key), nd.others(key)) {
		return fmt.Errorf("%w: the edit would change how another line reads", ErrNotWritten)
	}
	// Only an edit of verified may change how the document protects.
	was, is := d.Protection(), nd.Protection()
	if key == "verified" {
		was.Verified, is.Verified = false, false
	}
	if was != is {
		return fmt.Errorf("%w: the edit would change how the document protects", ErrNotWritten)
	}
	got := nd.find(key)
	if len(lines) == 0 && len(got) != 0 {
		return fmt.Errorf("%w: the key would still be read", ErrNotWritten)
	}
	if len(lines) > 0 {
		if len(got) != 1 || len(got[0].lines) != len(lines) {
			return fmt.Errorf("%w: the entry would not read back as written", ErrNotWritten)
		}
		if _, ok := writtenValue(got[0]); !ok {
			return fmt.Errorf("%w: the entry would not read back as written", ErrNotWritten)
		}
	}
	*d = *nd
	return nil
}

// others lists every line of the frontmatter, but key's, with what it is.
func (d *Doc) others(key string) []string {
	own := d.owned(key)
	var out []string
	for i := 0; i <= d.close; i++ {
		if !own[i] {
			k := byte(opaque)
			if i < d.close {
				k = byte(d.kinds[i])
			}
			out = append(out, string(rune('0'+k))+d.lines[i])
		}
	}
	return out
}

// removesAnchor reports lines to be removed that define an anchor while
// another line of the frontmatter holds an alias: a YAML reader refuses an
// alias whose anchor is gone.
func (d *Doc) removesAnchor(own map[int]bool) bool {
	anchor, alias := false, false
	for i := 1; i < d.close; i++ {
		l := text(d.lines[i])
		if own[i] {
			anchor = anchor || reAnchor.MatchString(l)
		} else {
			alias = alias || reAlias.MatchString(l)
		}
	}
	return anchor && alias
}

// opens reports an entry whose value opens a quoted scalar or a flow
// collection that does not close on the entry's lines.
func opens(e *entry) bool {
	s := strings.TrimLeft(e.rest, " \t")
	for strings.HasPrefix(s, "!") || strings.HasPrefix(s, "&") {
		i := strings.IndexAny(s, " \t")
		if i < 0 {
			return false
		}
		s = strings.TrimLeft(s[i:], " \t")
	}
	if s == "" || strings.IndexByte(`"'[{`, s[0]) < 0 {
		return false
	}
	// The value's lines as one, as YAML folds them.
	for _, c := range e.cont {
		s += " " + strings.TrimLeft(c, " \t")
	}
	if s[0] == '"' || s[0] == '\'' {
		return quoteEnd(s, 0) < 0
	}
	return flowEnd(s) < 0
}

// owned marks the lines of key's entries.
func (d *Doc) owned(key string) map[int]bool {
	own := map[int]bool{}
	for _, e := range d.find(key) {
		for _, i := range e.lines {
			own[i] = true
		}
	}
	return own
}

// flowRoot reports a block whose first line that is neither blank nor a
// comment opens a flow mapping, after any tag or anchor: the whole block
// is then one mapping written on its lines.
func (d *Doc) flowRoot() bool {
	for i := 1; i < d.close; i++ {
		s := strings.TrimRight(unlead(text(d.lines[i])), " \t")
		if s == "" || strings.HasPrefix(s, "#") {
			continue
		}
		for strings.HasPrefix(s, "!") || strings.HasPrefix(s, "&") {
			j := strings.IndexAny(s, " \t")
			if j < 0 {
				// A tag or anchor alone on its line: the node is on the next.
				s = ""
				break
			}
			s = strings.TrimLeft(s[j:], " \t")
		}
		if s != "" {
			return strings.HasPrefix(s, "{")
		}
	}
	return false
}

// hiddenBreak reports a frontmatter line that holds a character a YAML
// reader may take as a line break: a CR not at the line's end, or NEL, LS
// or PS.
func (d *Doc) hiddenBreak() bool {
	for i := 1; i < d.close; i++ {
		if strings.ContainsAny(text(d.lines[i]), "\r\u0085  ") {
			return true
		}
	}
	return false
}

// lenient reads a scalar as YAML would, for a line a person wrote: a
// quoted string is unquoted, and a comment after a plain value is dropped.
// Only a space or a tab is white space to YAML.
func lenient(rest string) string {
	s := strings.Trim(rest, " \t")
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
		s = strings.TrimRight(s[:i], " \t")
	}
	if strings.HasPrefix(s, "#") {
		return ""
	}
	return s
}

// quoted reports a value written in quotes.
func quoted(rest string) bool {
	s := strings.Trim(rest, " \t")
	return strings.HasPrefix(s, `"`) || strings.HasPrefix(s, "'")
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
// key, an alias as the key, or a flow collection as the key. It returns
// the key, what follows its colon, and whether the key is explicit; ok is
// false for a line it cannot read as a key. An explicit key's value is on
// its own ": value" line, so its rest is empty.
func yamlKey(s string) (key, rest string, explicit, ok bool) {
	if s == "?" || strings.HasPrefix(s, "? ") || strings.HasPrefix(s, "?\t") {
		key, after, ok := readKey(strings.TrimLeft(s[1:], " \t"), true)
		if !ok || strings.HasPrefix(after, ":") {
			// No scalar on the line, or a compact mapping as the key's
			// node, "? note: x": either may hold locked or verified.
			return unknownKey, "", true, true
		}
		return key, "", true, true
	}
	key, after, ok := readKey(s, false)
	if !ok || !strings.HasPrefix(after, ":") {
		return "", "", false, false
	}
	return key, after[1:], false, true
}

// readKey reads the key at the start of s, after any tag or anchor, and
// returns it and what follows it, spaces dropped. Unless explicit, a plain
// key must end in a colon. No key starts with white space. An alias, or a
// flow collection closed on the line, is read as the unknown key.
func readKey(s string, explicit bool) (key, after string, ok bool) {
	for strings.HasPrefix(s, "!") || strings.HasPrefix(s, "&") {
		i := strings.IndexAny(s, " \t")
		if i < 0 {
			return "", "", false
		}
		s = strings.TrimLeft(s[i:], " \t")
	}
	if s == "" || s[0] == ' ' || s[0] == '\t' || strings.HasPrefix(s, bom) {
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
		// An alias's name is libyaml's anchor alphabet, so "*k: true" is
		// the alias k and then a colon. A reader that takes more as the
		// name finds no key on the line, and Protection reads every line
		// that starts with "*" as the unknown key.
		i := 1
		for i < len(s) && isAnchorByte(s[i]) {
			i++
		}
		key, after = unknownKey, s[i:]
	case c == '[' || c == '{':
		// A flow collection as the key, "[locked]: false": no scalar.
		end := flowEnd(s)
		if end < 0 {
			return "", "", false
		}
		key, after = unknownKey, s[end:]
	case strings.IndexByte("-#,]}|>%@`:", c) >= 0:
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

// bom is U+FEFF, which a YAML reader drops where a stream starts: a
// frontmatter line that starts with it reads as the line without it to a
// reader handed the frontmatter alone.
const bom = "\xef\xbb\xbf"

// unlead drops the spaces, tabs and U+FEFF a line starts with.
func unlead(s string) string {
	for t := ""; t != s; {
		t, s = s, strings.TrimPrefix(strings.TrimLeft(s, " \t"), bom)
	}
	return s
}

// blank reports a rune a lenient reader may trim from a key: white space
// as Go's unicode package knows it, and U+FEFF.
func blank(r rune) bool {
	return unicode.IsSpace(r) || r == '\ufeff'
}

// loose reports a rune a lenient reader may trim from a key: blank, or a
// quote.
func loose(r rune) bool {
	return blank(r) || r == '"' || r == '\''
}

// looseKey reads the mark a line names as a reader more lenient than YAML
// may, as v1 did: "locked" or "verified" when the text before its first
// colon, inside quotes too, trims by loose to that word, or is one
// double-quoted string whose escapes give such a text; unknownKey when the
// line starts with a quote not closed on it, which another reader may
// close anywhere, or its key is a double-quoted string with an escape YAML
// does not have, which another reader may read as anything; else "".
func looseKey(l string) string {
	s := strings.TrimLeftFunc(l, blank)
	if strings.HasPrefix(s, `"`) || strings.HasPrefix(s, "'") {
		if quoteEnd(s, 0) < 0 {
			return unknownKey
		}
	}
	if q := quotedKey(s); q != "" && !escapesValid(q[1:len(q)-1]) {
		return unknownKey
	}
	before, _, ok := strings.Cut(l, ":")
	if !ok {
		return ""
	}
	w := strings.TrimFunc(before, loose)
	if t := strings.TrimFunc(before, blank); w != "locked" && w != "verified" && len(t) > 1 && t[0] == '"' && quoteEnd(t, 0) == len(t)-1 {
		w = strings.TrimFunc(yamlUnescape(t[1:len(t)-1]), loose)
	}
	if w == "locked" || w == "verified" {
		return w
	}
	return ""
}

// quotedKey is the double-quoted scalar, quotes and all, that s starts
// with after an explicit "? " and any tag or anchor, or "" when there is
// none or it does not close on the line.
func quotedKey(s string) string {
	if strings.HasPrefix(s, "? ") || strings.HasPrefix(s, "?\t") {
		s = strings.TrimLeft(s[1:], " \t")
	}
	for strings.HasPrefix(s, "!") || strings.HasPrefix(s, "&") {
		i := strings.IndexAny(s, " \t")
		if i < 0 {
			return ""
		}
		s = strings.TrimLeft(s[i:], " \t")
	}
	if !strings.HasPrefix(s, `"`) {
		return ""
	}
	end := quoteEnd(s, 0)
	if end < 0 {
		return ""
	}
	return s[:end+1]
}

// escapesValid reports whether every escape in the inside of a
// double-quoted scalar is one of YAML 1.2's, and each \x, \u or \U is a
// character.
func escapesValid(in string) bool {
	width := map[byte]int{'x': 2, 'u': 4, 'U': 8}
	for i := 0; i < len(in); i++ {
		if in[i] != '\\' {
			continue
		}
		if i+1 == len(in) {
			return false
		}
		c := in[i+1]
		if n := width[c]; n > 0 {
			if i+2+n > len(in) {
				return false
			}
			v, err := strconv.ParseUint(in[i+2:i+2+n], 16, 32)
			if err != nil || !utf8.ValidRune(rune(v)) {
				return false
			}
			i += 1 + n
			continue
		}
		if strings.IndexByte("0abt\tnvfre \"/\\N_LP", c) < 0 {
			return false
		}
		i++
	}
	return true
}

// isAnchorByte reports a byte of libyaml's anchor and alias names.
func isAnchorByte(c byte) bool {
	return c == '_' || c == '-' || c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z'
}

// flowEnd is the index just after the flow collection that starts s, or
// -1 when it does not close on the line. A quote opens a string only where
// a node may start; a comment runs to the line's end.
func flowEnd(s string) int {
	depth := 0
	prev := byte(' ')
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case (c == '"' || c == '\'') && strings.IndexByte(" \t[{,:", prev) >= 0:
			j := quoteEnd(s, i)
			if j < 0 {
				return -1
			}
			i = j
		case c == '#' && (prev == ' ' || prev == '\t'):
			return -1
		case c == '[' || c == '{':
			depth++
		case c == ']' || c == '}':
			if depth--; depth == 0 {
				return i + 1
			}
		}
		prev = s[i]
	}
	return -1
}

// quoteEnd is the index of the quote that closes the string opened at
// s[i], or -1 when it does not close on the line.
func quoteEnd(s string, i int) int {
	q := s[i]
	for j := i + 1; j < len(s); j++ {
		switch {
		case q == '"' && s[j] == '\\':
			j++
		case s[j] != q:
		case q == '\'' && j+1 < len(s) && s[j+1] == '\'':
			j++
		default:
			return j
		}
	}
	return -1
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

// yamlUnescape reads the escapes of a double-quoted YAML scalar as YAML 1.2
// does: "\_", "\L" and "\P" are U+00A0, U+2028 and U+2029, not a space. An
// escape YAML does not know is kept as written.
func yamlUnescape(s string) string {
	simple := map[byte]string{'0': "\x00", 'a': "\a", 'b': "\b", 't': "\t", '\t': "\t", 'n': "\n", 'v': "\v", 'f': "\f",
		'r': "\r", 'e': "\x1b", ' ': " ", '"': `"`, '/': "/", '\\': `\`, 'N': "\u0085", '_': "\u00a0", 'L': "\u2028", 'P': "\u2029"}
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
