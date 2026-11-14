package codec

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"
)

// ErrUnreadable says ReadV1 was given a document the codec cannot read;
// Unreadable says why.
var ErrUnreadable = errors.New("codec: the document cannot be read")

// V1Page is a page of a v1 wiki, read as v1 read it: its fields are named
// as v1's page model named them. Extra holds every other top-level key but
// resource, tags, governance and code_refs, which v1 read into fields
// eva-md never used. Doc is the document as the codec reads it; its
// Protection is the page's, never a reading of Extra["locked"].
type V1Page struct {
	Doc         *Doc           `json:"-"`
	Type        string         `json:"type"`
	Title       string         `json:"title"`
	Description string         `json:"description"`
	Status      string         `json:"status"`
	StaleAfter  string         `json:"stale_after"`
	Generated   *V1Stamp       `json:"generated"`
	Verified    []V1Stamp      `json:"verified"`
	Sources     []V1Source     `json:"sources"`
	Extra       map[string]any `json:"extra"`
	Body        string         `json:"body"`
}

// V1Stamp is a generated or verified record: who, and when.
type V1Stamp struct {
	By string `json:"by"`
	At string `json:"at"`
}

// V1Source is one of a page's sources.
type V1Source struct {
	ID           string            `json:"id"`
	Resource     string            `json:"resource"`
	Title        string            `json:"title"`
	Author       string            `json:"author"`
	LastModified string            `json:"last_modified"`
	UsageCount   int               `json:"usage_count"`
	Extra        map[string]string `json:"extra"`
}

// v1Keys are the keys v1 read into fields of its page model, and those
// eva-md wrote or read among the rest, its Extra.
var v1Keys = strings.Fields("type title description resource status governance code_refs stale_after tags generated verified sources " +
	"aliases as_of branch head kind locked model model_note parent project remote repos scope state writer")

// ReadV1 reads a v1 page through Read. It reads the forms v1 wrote, as v1
// read them, and where v1 misread a hand edit it reads as YAML does: a
// comment after a value is dropped, a single-quoted value is unquoted.
// It refuses what it would have to guess at, returning a page that holds
// the Doc and nothing else: an unreadable document (ErrUnreadable), a
// repeated key (ErrRepeated), and a value it does not decode
// (ErrNotWritten): a block scalar, a tag, anchor or alias, a value that
// runs onto the next line, or a key it cannot read. An error names a key
// only when it is one v1 wrote.
func ReadV1(b []byte) (V1Page, error) {
	d := Read(b)
	p, err := readV1(d)
	if err != nil {
		return V1Page{Doc: d}, err
	}
	return p, nil
}

func readV1(d *Doc) (V1Page, error) {
	p := V1Page{Doc: d, Extra: map[string]any{}}
	if why := d.why(); why != "" {
		return p, fmt.Errorf("%w: %s", ErrUnreadable, why)
	}
	if d.flowRoot() {
		return p, fmt.Errorf("%w: the frontmatter is one flow mapping", ErrNotWritten)
	}
	// A line that belongs to no key and is not blank or a comment is one
	// YAML reads otherwise or refuses: "title:x", a line indented with a
	// tab, one less indented than the block. Its words would be dropped.
	for i := 1; i < d.close; i++ {
		if t := strings.TrimLeft(text(d.lines[i]), " \t"); d.kinds[i] == opaque && t != "" && t[0] != '#' {
			return p, fmt.Errorf("%w: line %d belongs to no key", ErrNotWritten, i+1)
		}
	}
	seen := map[string]bool{}
	for _, e := range d.entries {
		if e.key == unknownKey || e.key == mergeKey {
			return p, fmt.Errorf("%w: a key that is an alias, a merge or a collection", ErrNotWritten)
		}
		if !keyAsV1(text(d.lines[e.lines[0]]), e) {
			return p, fmt.Errorf("%w: a key v1 read otherwise", ErrNotWritten)
		}
		if seen[e.key] {
			return p, fmt.Errorf("%w: %s", ErrRepeated, named(e.key))
		}
		seen[e.key] = true
		if err := p.set(e); err != nil {
			return p, fmt.Errorf("%w: %s, under %s", ErrNotWritten, err.Error(), named(e.key))
		}
	}
	p.Body = strings.Join(d.lines[d.close+1:], "")
	return p, nil
}

// keyAsV1 reports whether v1 read an entry's key, on the key's line l, as
// YAML and the codec do. v1 cut a line at its first colon and trimmed the
// quotes around the key, so it read otherwise a tag or an anchor before
// the key ("&a title: x"), a plain key that holds a colon or a quote
// ("title :{a: b}", "title:: x", "verified': x"), a quoted key with an
// escape in it ("\"a\x20b\": x") or a space at either end inside its
// quotes ("'locked ': true"), one that holds a colon after the word
// verified ("'verified: a': x"), and a plain key with white space at an
// end that YAML keeps and v1 trimmed, as a no-break space (U+00A0) before
// "locked".
func keyAsV1(l string, e *entry) bool {
	if e.canonical {
		return true
	}
	s := unlead(l)
	if e.explicit {
		s = strings.TrimLeft(s[1:], " \t")
	}
	switch {
	case s == "":
		return true
	case s[0] == '!' || s[0] == '&':
		return false
	case s[0] == '"' || s[0] == '\'':
		end := quoteEnd(s, 0)
		return end >= 0 && quotedKeyAsV1(s[:end+1]) && (e.explicit || !v1Verified(s[:end+1]))
	}
	return !strings.ContainsAny(e.key, `:"'`) && e.key == strings.TrimSpace(e.key)
}

// v1Verified reports a quoted key q that is no verified key but that v1's
// check of a verified mark took for one: it cut the line at its first
// colon, inside quotes too, and trimmed the quotes ("'verified: a': x").
func v1Verified(q string) bool {
	k, _, ok := strings.Cut(q, ":")
	return ok && strings.Trim(strings.TrimSpace(k), `"'`) == "verified"
}

// quotedKeyAsV1 reports whether v1 read the quoted key q, its quotes
// included, as YAML does. v1 kept a double-quoted key as JSON reads it;
// else it took the text inside the quotes, with no escape read and the
// white space at its ends trimmed.
func quotedKeyAsV1(q string) bool {
	in := q[1 : len(q)-1]
	if q[0] == '"' {
		var v string
		if strings.Contains(in, `\`) {
			return false
		} else if json.Unmarshal([]byte(q), &v) == nil {
			return true
		}
	} else if strings.Contains(in, "'") {
		return false
	}
	return in == strings.TrimSpace(in)
}

// named is a key for an error's text: the key when v1 wrote it, as a key a
// person added may be text that must not reach a log.
func named(key string) string {
	if slices.Contains(v1Keys, key) {
		return key
	}
	return "a key"
}

// errNoPair is text under generated, verified or sources that holds no
// pair of the mapping or list it is in.
var errNoPair = errors.New("text that is no pair")

// errQuoteInside is a quote inside a plain value, where v1 opened a
// string and YAML does not: v1 read "[it's, b]" as one item and
// "- it's: x" as a scalar.
var errQuoteInside = errors.New("a quote inside a plain value")

// errTightDash is a dash with no space after it, which v1 read as an item
// and YAML reads as text: "-resource: x" is the key "-resource".
var errTightDash = errors.New("a dash with no space after it")

// tight reports what follows a dash as text, not an item: YAML starts an
// item only at a dash followed by a space, a tab or the line's end.
func tight(rest string) bool {
	return rest != "" && rest[0] != ' ' && rest[0] != '\t'
}

// errListOtherwise is lines under a key that v1 read as a list and YAML
// does not, or the other way round.
var errListOtherwise = errors.New("lines v1 read as a list otherwise")

// set reads one entry into p.
func (p *V1Page) set(e *entry) error {
	s := uncomment(e.rest)
	var lines []string // the lines under the key, but blank and comment lines
	for _, l := range e.cont {
		if t := strings.TrimLeft(l, " \t"); t != "" && !strings.HasPrefix(t, "#") {
			lines = append(lines, l)
		}
	}
	switch {
	case colonComment(e.rest, s):
		return errColonComment
	case strings.HasPrefix(e.rest, "#"):
		// After a quoted key, "'kind':#x": v1 read the value "#x", the
		// codec a comment, and YAML starts no plain value with "#".
		return errors.New("a # right after the colon")
	case s != "" && strings.IndexByte("|>", s[0]) >= 0:
		return errors.New("a block scalar")
	case s != "" && strings.IndexByte("!&*", s[0]) >= 0:
		return errors.New("a tag, an anchor or an alias")
	case s != "" && len(lines) > 0, closeOf(s) < 0:
		return errors.New("a value that runs onto the next line")
	case closeOf(s) != len(s):
		return errors.New("a quoted value or a collection with text after it")
	case strings.HasPrefix(s, "[") && !strings.HasSuffix(s, "]"), strings.HasPrefix(s, "{") && !strings.HasSuffix(s, "}"):
		// v1 took "[a, b}" for no list, or kept the "}" in its last item.
		return errors.New("a collection closed by the other bracket")
	}
	switch e.key {
	case "type", "title", "description", "status", "stale_after":
		if len(lines) > 0 {
			return errors.New("a scalar on the lines below its key")
		}
		v, err := unquoteV1(s)
		if err != nil {
			return err
		}
		switch e.key {
		case "type":
			p.Type = v
		case "title":
			p.Title = v
		case "description":
			p.Description = v
		case "status":
			p.Status = v
		default:
			p.StaleAfter = v
		}
	case "generated":
		m, err := blockMap(lines)
		if strings.HasPrefix(s, "{") {
			m, err = flowMap(s)
		}
		switch {
		case err != nil:
			return err
		case len(m) == 0 && !strings.HasPrefix(s, "{"):
			return nil
		}
		p.Generated = &V1Stamp{By: m["by"], At: m["at"]}
	case "verified":
		switch {
		case strings.HasPrefix(s, "{"):
			m, err := flowMap(s)
			if err != nil {
				return err
			}
			p.Verified = append(p.Verified, V1Stamp{By: m["by"], At: m["at"]})
		case strings.HasPrefix(s, "["):
			items, err := flowItems(s[1 : len(s)-1])
			if err != nil {
				return err
			}
			for _, it := range items {
				m, err := flowMap(strings.TrimSpace(it))
				if err != nil {
					return err
				}
				if m["by"] != "" || m["at"] != "" {
					p.Verified = append(p.Verified, V1Stamp{By: m["by"], At: m["at"]})
				}
			}
		default:
			items, err := listOfMaps(lines)
			if err != nil {
				return err
			}
			for _, m := range items {
				p.Verified = append(p.Verified, V1Stamp{By: m["by"], At: m["at"]})
			}
		}
	case "sources":
		items, err := listOfMaps(lines)
		if err != nil {
			return err
		}
		for _, m := range items {
			src := V1Source{ID: m["id"], Resource: m["resource"], Title: m["title"], Author: m["author"], LastModified: m["last_modified"]}
			if n, err := strconv.Atoi(m["usage_count"]); err == nil {
				src.UsageCount = n
			}
			for k, v := range m {
				switch k {
				case "id", "resource", "title", "author", "last_modified", "usage_count":
					continue
				}
				if src.Extra == nil {
					src.Extra = map[string]string{}
				}
				src.Extra[k] = v
			}
			p.Sources = append(p.Sources, src)
		}
	case "resource", "tags", "governance", "code_refs":
	default:
		var err error
		if len(lines) == 0 {
			p.Extra[e.key], err = extraValue(s)
		} else if items, ok, lerr := blockList(lines); lerr != nil {
			err = lerr
		} else if ok {
			p.Extra[e.key] = items
		} else {
			p.Extra[e.key] = outdent(lines)
		}
		return err
	}
	return nil
}

// uncomment trims a value and drops a comment after it: a "#" at its start
// or after a space or a tab, outside quotes. A quote opens a string only
// where a node may start.
func uncomment(s string) string {
	s = strings.TrimSpace(s)
	prev := byte(' ')
	for i := 0; i < len(s); i++ {
		switch c := s[i]; {
		case (c == '"' || c == '\'') && strings.IndexByte(" \t[{,:", prev) >= 0:
			j := quoteEnd(s, i)
			if j < 0 {
				return s
			}
			i = j
		case c == '#' && (prev == ' ' || prev == '\t'):
			return strings.TrimRight(s[:i], " \t")
		}
		prev = s[i]
	}
	return s
}

// errColonComment is a comment after a plain value that holds ": ".
var errColonComment = errors.New("a comment after a plain value that holds a colon")

// colonComment reports a value v, a value or pair's value in raw as
// uncomment left it, when uncomment dropped a comment from raw and v is a
// plain scalar holding ": " or ending in ":". YAML refuses such a value
// and v1 kept the comment in it ("Tide: feed #3"), so neither reads it as
// the codec would.
func colonComment(raw, v string) bool {
	v = strings.TrimSpace(v)
	return uncomment(raw) != strings.TrimSpace(raw) && v != "" && strings.IndexByte(`"'[{`, v[0]) < 0 &&
		(strings.Contains(v, ": ") || strings.Contains(v, ":\t") || strings.HasSuffix(v, ":"))
}

// pairValue is the text after the first colon of a pair outside quotes,
// as cutPair finds it, or "" when s holds none.
func pairValue(s string) string {
	prev := byte(' ')
	for i := 0; i < len(s); i++ {
		switch c := s[i]; {
		case (c == '"' || c == '\'') && strings.IndexByte(" \t[{,", prev) >= 0:
			if j := quoteEnd(s, i); j >= 0 {
				i = j
			}
		case c == ':':
			return s[i+1:]
		}
		prev = s[i]
	}
	return ""
}

// closeOf is the index just after the quoted string or flow collection a
// value, its comment dropped, opens, -1 when it does not close on the line
// (YAML continues it onto the next lines), and len(s) for any other value.
func closeOf(s string) int {
	switch {
	case s == "":
	case s[0] == '"' || s[0] == '\'':
		if j := quoteEnd(s, 0); j >= 0 {
			return j + 1
		}
		return -1
	case s[0] == '[' || s[0] == '{':
		return flowEnd(s)
	}
	return len(s)
}

// unquoteV1 reads a scalar, the white space around it dropped as v1
// dropped it: a double-quoted one as JSON reads it, or with YAML's escapes
// when JSON cannot; a single-quoted one as YAML does; any other as it is.
// It refuses an escape YAML does not know, which YAML refuses and v1 kept,
// and "\_", "\L" and "\P", which YAML reads as U+00A0, U+2028 and U+2029
// and v1 kept as written.
func unquoteV1(s string) (string, error) {
	s = strings.TrimSpace(s)
	if len(s) < 2 || s[0] != '"' && s[0] != '\'' || quoteEnd(s, 0) != len(s)-1 {
		return s, nil
	}
	if s[0] == '"' {
		var v string
		if json.Unmarshal([]byte(s), &v) == nil {
			return v, nil
		}
		if !escapesKnown(s[1 : len(s)-1]) {
			return "", errEscape
		}
		return yamlUnescape(s[1 : len(s)-1]), nil
	}
	return strings.ReplaceAll(s[1:len(s)-1], "''", "'"), nil
}

// errEscape is an escape in a double-quoted value that ReadV1 does not
// read as YAML does.
var errEscape = errors.New("an escape YAML does not read so")

// escapesKnown reports whether every escape in the inside of a
// double-quoted scalar is one YAML knows, but "\_", "\L" and "\P", which
// unquoteV1 refuses.
func escapesKnown(in string) bool {
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
		if strings.IndexByte("0abt\tnvfre \"/\\N", c) < 0 {
			return false
		}
		i++
	}
	return true
}

// extraValue reads a value on its key's line as v1 read one it did not
// know: JSON, then a flow list of such values, then a scalar.
func extraValue(s string) (any, error) {
	if s = strings.TrimSpace(s); s == "" {
		return "", nil
	}
	var v any
	if json.Unmarshal([]byte(s), &v) == nil {
		return v, nil
	}
	if strings.HasPrefix(s, "[") && strings.HasSuffix(s, "]") {
		parts, err := flowItems(s[1 : len(s)-1])
		if err != nil {
			return nil, err
		}
		items := []any{}
		for _, it := range parts {
			if it = strings.TrimSpace(it); it != "" {
				if closeOf(it) != len(it) {
					// v1 unquoted "\"a,\"b\"" to "a,\"b"; YAML refuses it.
					return nil, errors.New("an item with text after its quoted value or collection")
				}
				v, err := extraValue(it)
				if err != nil {
					return nil, err
				}
				items = append(items, v)
			}
		}
		return items, nil
	}
	return unquoteV1(s)
}

// flowItems splits the inside of a flow collection at its commas, outside
// quotes and nested collections. It refuses one v1 split otherwise.
func flowItems(s string) ([]string, error) {
	out := yamlItems(s)
	if !slices.Equal(out, v1Items(s)) {
		return nil, errQuoteInside
	}
	return out, nil
}

// v1Items splits the inside of a flow collection as v1 did: a quote
// anywhere opens a string, not only where a node may start.
func v1Items(s string) []string {
	var out []string
	depth, start := 0, 0
	for i := 0; i < len(s); i++ {
		switch c := s[i]; c {
		case '"', '\'':
			i = v1QuoteEnd(s, i)
		case '[', '{':
			depth++
		case ']', '}':
			if depth > 0 {
				depth--
			}
		case ',':
			if depth == 0 {
				out = append(out, s[start:i])
				start = i + 1
			}
		}
	}
	return append(out, s[start:])
}

// v1QuoteEnd is the index of the quote that closes the one at i as v1
// read it, a backslash escaping the next byte in a double-quoted string,
// or len(s) when none does.
func v1QuoteEnd(s string, i int) int {
	for j := i + 1; j < len(s); j++ {
		switch {
		case s[i] == '"' && s[j] == '\\':
			j++
		case s[j] == s[i]:
			return j
		}
	}
	return len(s)
}

// yamlItems splits the inside of a flow collection as YAML does: a quote
// opens a string only where a node may start.
func yamlItems(s string) []string {
	var out []string
	depth, start := 0, 0
	prev := byte(' ')
	for i := 0; i < len(s); i++ {
		switch c := s[i]; {
		case (c == '"' || c == '\'') && strings.IndexByte(" \t[{,:", prev) >= 0:
			if j := quoteEnd(s, i); j >= 0 {
				i = j
			}
		case c == '[' || c == '{':
			depth++
		case (c == ']' || c == '}') && depth > 0:
			depth--
		case c == ',' && depth == 0:
			out = append(out, s[start:i])
			start = i + 1
		}
		prev = s[i]
	}
	return append(out, s[start:])
}

// cutPair splits "key: value" at the first colon outside quotes, as v1
// did, and unquotes both sides. A value that opens a quoted string or a
// collection it does not end with is no pair: YAML reads it otherwise.
// The error is an escape unquoteV1 refuses.
func cutPair(s string) (key, value string, ok bool, err error) {
	prev := byte(' ')
	for i := 0; i < len(s); i++ {
		switch c := s[i]; {
		case (c == '"' || c == '\'') && strings.IndexByte(" \t[{,", prev) >= 0:
			if j := quoteEnd(s, i); j >= 0 {
				i = j
			}
		case c == ':':
			v := strings.TrimSpace(s[i+1:])
			if closeOf(v) != len(v) {
				return "", "", false, nil
			}
			// A plain key that holds a quote, or a colon with no space
			// after it, is a key YAML reads otherwise: "note's: y" was
			// dropped by v1, "by:a" is one scalar and "by :x: y" the key
			// "by :x".
			if k := strings.TrimSpace(s[:i]); k != "" && k[0] != '"' && k[0] != '\'' &&
				(strings.ContainsAny(k, `"'`) || i+1 < len(s) && s[i+1] != ' ' && s[i+1] != '\t') {
				return "", "", false, nil
			}
			// A quoted key that does not close, or has text after its
			// quote, v1 dropped: "\"resource: x", "\"a\"b\": x". One with
			// an escape, or a space at an end inside its quotes, v1 read
			// as another key: "'it''s': x", "' resource': x".
			if k := strings.TrimSpace(s[:i]); k != "" && (k[0] == '"' || k[0] == '\'') && (closeOf(k) != len(k) || !quotedKeyAsV1(k)) {
				return "", "", false, nil
			}
			if key, err = unquoteV1(s[:i]); err == nil {
				value, err = unquoteV1(v)
			}
			return key, value, err == nil, err
		}
		prev = s[i]
	}
	return "", "", false, nil
}

// flowMap reads a flow mapping, "{ by: x, at: y }", into its pairs. It
// refuses an item that holds text but no pair.
func flowMap(s string) (map[string]string, error) {
	out := map[string]string{}
	if strings.HasPrefix(s, "{") && strings.HasSuffix(s, "}") {
		s = s[1 : len(s)-1]
	}
	items, err := flowItems(s)
	if err != nil {
		return nil, err
	}
	for _, it := range items {
		k, v, ok, err := cutPair(it)
		if err != nil {
			return nil, err
		}
		if !ok && strings.Trim(it, " \t") != "" {
			return nil, errNoPair
		}
		if ok && k != "" {
			out[k] = v
		}
	}
	return out, nil
}

// blockMap reads the lines under a key as one mapping, a "- " before a
// pair dropped. It refuses a line that holds text but no pair, as
// "note #1: x", which YAML reads as "note" and a comment, and a dash with
// no space after it, as "-by: x", the key "-by" to YAML.
func blockMap(lines []string) (map[string]string, error) {
	out := map[string]string{}
	for _, l := range lines {
		t := strings.Trim(uncomment(l), " \t")
		if rest, ok := strings.CutPrefix(t, "-"); ok {
			if tight(rest) {
				return nil, errTightDash
			}
			t = rest
		}
		if colonComment(l, pairValue(t)) {
			return nil, errColonComment
		}
		k, v, ok, err := cutPair(t)
		switch {
		case err != nil:
			return nil, err
		case !ok && strings.Trim(t, " \t") != "":
			return nil, errNoPair
		case ok:
			out[k] = v
		}
	}
	return out, nil
}

// listOfMaps reads the lines under a key as a block list of mappings, each
// item a flow mapping on its "- " line or pairs on it and the lines under.
// It refuses a line that holds text but no pair of an item, words v1 kept
// or dropped and YAML reads otherwise, and a dash with no space after it.
func listOfMaps(lines []string) ([]map[string]string, error) {
	var out []map[string]string
	var cur map[string]string
	for _, l := range lines {
		t := uncomment(l)
		if rest, ok := strings.CutPrefix(t, "-"); ok {
			if tight(rest) {
				return nil, errTightDash
			}
			cur = map[string]string{}
			out = append(out, cur)
			if t = strings.Trim(rest, " \t"); strings.HasPrefix(t, "{") {
				if closeOf(t) != len(t) || !strings.HasSuffix(t, "}") {
					// "- { by: a, # at: b }": YAML runs the mapping on
					// past the comment, v1 read its pairs to the line's end.
					return nil, errors.New("a flow mapping that does not close at the line's end")
				}
				m, err := flowMap(t)
				if err != nil {
					return nil, err
				}
				for k, v := range m {
					cur[k] = v
				}
				continue
			}
		}
		if colonComment(l, pairValue(t)) {
			return nil, errColonComment
		}
		k, v, ok, err := cutPair(t)
		switch {
		case err != nil:
			return nil, err
		case ok && cur != nil:
			cur[k] = v
		case t != "":
			return nil, errNoPair
		}
	}
	return out, nil
}

// blockList reads the lines under a key as a block list of scalars, all at
// one indentation. A list of mappings or of lists is not one. It refuses
// lines v1 read as a list and YAML does not, or the other way round: an
// item with no space after its dash ("-x"), one that is only a comment
// ("- # c"), or one v1 told apart from a pair otherwise ("- it's: x").
func blockList(lines []string) ([]any, bool, error) {
	items := []any{}
	list, v1List := true, true
	indent := -1
	for _, l := range lines {
		t := strings.TrimLeft(l, " \t")
		rest, ok := strings.CutPrefix(t, "-")
		if !ok || indent >= 0 && len(l)-len(t) != indent {
			return nil, false, nil
		}
		indent = len(l) - len(t)
		v1It, it := strings.TrimSpace(rest), uncomment(rest)
		v1List = v1List && !(v1It == "" || v1It == "-" || strings.HasPrefix(v1It, "- ") || v1Mapping(v1It))
		list = list && !(tight(rest) || it == "" || it == "-" || strings.HasPrefix(it, "- ") || mapping(it))
		if !list {
			continue
		}
		if colonComment(rest, it) {
			return nil, false, errColonComment
		}
		if closeOf(it) != len(it) {
			// v1 unquoted "\"a\" b" to "a\" b"; YAML refuses it.
			return nil, false, errors.New("an item with text after its quoted value or collection")
		}
		v, err := extraValue(it)
		if err != nil {
			return nil, false, err
		}
		items = append(items, v)
	}
	if list != v1List {
		return nil, false, errListOtherwise
	}
	return items, list && len(items) > 0, nil
}

// v1Mapping reports a "key: value" pair as v1 did: at the first colon
// outside quotes, where a quote anywhere opens a string.
func v1Mapping(s string) bool {
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '"', '\'':
			i = v1QuoteEnd(s, i)
		case ':':
			return i+1 == len(s) || s[i+1] == ' '
		}
	}
	return false
}

// mapping reports a "key: value" pair, not a scalar such as a link.
func mapping(s string) bool {
	prev := byte(' ')
	for i := 0; i < len(s); i++ {
		switch c := s[i]; {
		case (c == '"' || c == '\'') && strings.IndexByte(" \t[{,", prev) >= 0:
			if j := quoteEnd(s, i); j >= 0 {
				i = j
			}
		case c == ':':
			return i+1 == len(s) || s[i+1] == ' '
		}
		prev = s[i]
	}
	return false
}

// outdent is the lines under a key that are no list, with the indentation
// they share removed, as v1 kept a block it could not read.
func outdent(lines []string) []string {
	least := -1
	for _, l := range lines {
		if n := len(l) - len(strings.TrimLeft(l, " \t")); least < 0 || n < least {
			least = n
		}
	}
	out := make([]string, len(lines))
	for i, l := range lines {
		out[i] = l[least:]
	}
	return out
}
