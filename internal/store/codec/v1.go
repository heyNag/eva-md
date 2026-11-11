package codec

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
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
	seen := map[string]bool{}
	for _, e := range d.entries {
		if e.key == unknownKey || e.key == mergeKey {
			return p, fmt.Errorf("%w: a key that is an alias, a merge or a collection", ErrNotWritten)
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

// named is a key for an error's text: the key when v1 wrote it, as a key a
// person added may be text that must not reach a log.
func named(key string) string {
	if slices.Contains(v1Keys, key) {
		return key
	}
	return "a key"
}

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
	case s != "" && strings.IndexByte("|>", s[0]) >= 0:
		return errors.New("a block scalar")
	case s != "" && strings.IndexByte("!&*", s[0]) >= 0:
		return errors.New("a tag, an anchor or an alias")
	case s != "" && len(lines) > 0, runsOn(s):
		return errors.New("a value that runs onto the next line")
	}
	switch e.key {
	case "type", "title", "description", "status", "stale_after":
		if len(lines) > 0 {
			return errors.New("a scalar on the lines below its key")
		}
		switch v := unquoteV1(s); e.key {
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
		var m map[string]string
		if strings.HasPrefix(s, "{") {
			m = flowMap(s)
		} else if m = blockMap(lines); len(m) == 0 {
			return nil
		}
		p.Generated = &V1Stamp{By: m["by"], At: m["at"]}
	case "verified":
		switch {
		case strings.HasPrefix(s, "{"):
			m := flowMap(s)
			p.Verified = append(p.Verified, V1Stamp{By: m["by"], At: m["at"]})
		case strings.HasPrefix(s, "["):
			for _, it := range flowItems(s[1 : len(s)-1]) {
				if m := flowMap(strings.TrimSpace(it)); m["by"] != "" || m["at"] != "" {
					p.Verified = append(p.Verified, V1Stamp{By: m["by"], At: m["at"]})
				}
			}
		default:
			for _, m := range listOfMaps(lines) {
				p.Verified = append(p.Verified, V1Stamp{By: m["by"], At: m["at"]})
			}
		}
	case "sources":
		for _, m := range listOfMaps(lines) {
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
		if len(lines) == 0 {
			p.Extra[e.key] = extraValue(s)
		} else if items, ok := blockList(lines); ok {
			p.Extra[e.key] = items
		} else {
			p.Extra[e.key] = outdent(lines)
		}
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

// runsOn reports a value, its comment dropped, that opens a quoted string
// or a flow collection it does not close: YAML continues it onto the next
// lines.
func runsOn(s string) bool {
	switch {
	case s == "":
		return false
	case s[0] == '"' || s[0] == '\'':
		return quoteEnd(s, 0) != len(s)-1
	case s[0] == '[' || s[0] == '{':
		return flowEnd(s) != len(s)
	}
	return false
}

// unquoteV1 reads a scalar, the white space around it dropped as v1
// dropped it: a double-quoted one as JSON reads it, or with YAML's escapes
// when JSON cannot; a single-quoted one as YAML does; any other as it is.
func unquoteV1(s string) string {
	s = strings.TrimSpace(s)
	if len(s) < 2 || s[0] != '"' && s[0] != '\'' || quoteEnd(s, 0) != len(s)-1 {
		return s
	}
	if s[0] == '"' {
		var v string
		if json.Unmarshal([]byte(s), &v) == nil {
			return v
		}
		return yamlUnescape(s[1 : len(s)-1])
	}
	return strings.ReplaceAll(s[1:len(s)-1], "''", "'")
}

// extraValue reads a value on its key's line as v1 read one it did not
// know: JSON, then a flow list of such values, then a scalar.
func extraValue(s string) any {
	if s = strings.TrimSpace(s); s == "" {
		return ""
	}
	var v any
	if json.Unmarshal([]byte(s), &v) == nil {
		return v
	}
	if strings.HasPrefix(s, "[") && strings.HasSuffix(s, "]") {
		items := []any{}
		for _, it := range flowItems(s[1 : len(s)-1]) {
			if it = strings.TrimSpace(it); it != "" {
				items = append(items, extraValue(it))
			}
		}
		return items
	}
	return unquoteV1(s)
}

// flowItems splits the inside of a flow collection at its commas, outside
// quotes and nested collections.
func flowItems(s string) []string {
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
// did, and unquotes both sides.
func cutPair(s string) (key, value string, ok bool) {
	prev := byte(' ')
	for i := 0; i < len(s); i++ {
		switch c := s[i]; {
		case (c == '"' || c == '\'') && strings.IndexByte(" \t[{,", prev) >= 0:
			if j := quoteEnd(s, i); j >= 0 {
				i = j
			}
		case c == ':':
			return unquoteV1(s[:i]), unquoteV1(s[i+1:]), true
		}
		prev = s[i]
	}
	return "", "", false
}

// flowMap reads a flow mapping, "{ by: x, at: y }", into its pairs.
func flowMap(s string) map[string]string {
	out := map[string]string{}
	if strings.HasPrefix(s, "{") && strings.HasSuffix(s, "}") {
		s = s[1 : len(s)-1]
	}
	for _, it := range flowItems(s) {
		if k, v, ok := cutPair(it); ok && k != "" {
			out[k] = v
		}
	}
	return out
}

// blockMap reads the lines under a key as one mapping, a "- " before a
// pair dropped.
func blockMap(lines []string) map[string]string {
	out := map[string]string{}
	for _, l := range lines {
		t := strings.TrimPrefix(strings.Trim(uncomment(l), " \t"), "-")
		if k, v, ok := cutPair(t); ok {
			out[k] = v
		}
	}
	return out
}

// listOfMaps reads the lines under a key as a block list of mappings, each
// item a flow mapping on its "- " line or pairs on it and the lines under.
func listOfMaps(lines []string) []map[string]string {
	var out []map[string]string
	var cur map[string]string
	for _, l := range lines {
		t := uncomment(l)
		if rest, ok := strings.CutPrefix(t, "-"); ok {
			cur = map[string]string{}
			out = append(out, cur)
			if t = strings.Trim(rest, " \t"); strings.HasPrefix(t, "{") {
				for k, v := range flowMap(t) {
					cur[k] = v
				}
				continue
			}
		}
		if k, v, ok := cutPair(t); ok && cur != nil {
			cur[k] = v
		}
	}
	return out
}

// blockList reads the lines under a key as a block list of scalars, all at
// one indentation. A list of mappings or of lists is not one.
func blockList(lines []string) ([]any, bool) {
	items := []any{}
	indent := -1
	for _, l := range lines {
		t := strings.TrimLeft(l, " \t")
		rest, ok := strings.CutPrefix(t, "-")
		if !ok || indent >= 0 && len(l)-len(t) != indent {
			return nil, false
		}
		indent = len(l) - len(t)
		it := uncomment(rest)
		if it == "" || it == "-" || strings.HasPrefix(it, "- ") || mapping(it) {
			return nil, false
		}
		items = append(items, extraValue(it))
	}
	return items, len(items) > 0
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
