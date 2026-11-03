// Package knowledgepage models the wiki's knowledge pages: one lasting fact,
// decision, procedure or constraint each, which the background update
// promotes from the chats it reads. It renders and reads a page, saves the
// update's operations on the pages, and builds what lists them: the
// knowledge map, the folder's index, the root index's line and the recall
// index the prompt hook and the brief read. Pages reach the disk only
// through internal/okfadapter, and their free text goes through the thread
// page escaper (docs/design.md "Knowledge page").
//
// A saved page reads:
//
//	---
//	type: knowledge
//	title: Inbox format
//	description: The inbox stores entries as JSONL, one file per session
//	generated: { by: claude-code/claude-opus-5-5, at: "2026-11-02T09:30:00Z" }
//	status: stable
//	sources:
//	  - resource: "s:3f2a91c0#14"
//	  - resource: "github.com__acme__eva@4e2bf4c:internal/inbox"
//	as_of: "2026-10-28"
//	kind: "decision"
//	project: "github.com__acme__eva"
//	scope: "project"
//	state: "active"
//	---
//
//	## Statement
//	The inbox stores entries as JSONL, one file per session [stated] s:3f2a91c0#14 <!-- t:2026-10-28T10:00:00+01:00 -->
//
//	## History
//	- ~~The inbox stores entries as JSON~~ → The inbox stores entries as JSONL, one file per session (superseded 2026-10-28) [stated] s:3f2a91c0#14
//
//	Part of the [knowledge map](map.md).
//
// The statement is in the description too, so an index line and okf's
// drift check see it. The History section is left out while it is empty.
package knowledgepage

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/heyNag/eva-md/internal/coderef"
	"github.com/heyNag/eva-md/internal/okfadapter"
	"github.com/heyNag/eva-md/internal/threadpage"
)

// Folder is the wiki folder of the knowledge pages, created by the first
// write.
const Folder = "knowledge"

// The kinds of knowledge a page holds. A constraint is a standing limit on
// the work or the system: what must or must not be done, or must hold.
const (
	Fact       = "fact"
	Decision   = "decision"
	Procedure  = "procedure"
	Constraint = "constraint"
)

// The scopes of a page: the project it was learned in, or every project.
const (
	ScopeProject = "project"
	ScopeAll     = "all"
)

// The states of a page. A retired page stays, listed on the map, so its id
// is never used again.
const (
	Active  = "active"
	Retired = "retired"
)

// Bounds, in characters for text.
const (
	MaxTitle     = 80
	MaxStatement = 300
	MaxReason    = 300
	MaxSources   = 20 // the inbox refs a page keeps, the newest last
	MaxPaths     = 3  // the code paths an operation may cite
	// maxPage bounds the read of a page; one past it is unreadable.
	maxPage = 256 << 10
)

// MapLink ends every page, so okf never counts one as orphaned.
const MapLink = "Part of the [knowledge map](map.md)."

const (
	pageType         = "knowledge"
	statementHeading = "## Statement"
	historyHeading   = "## History"
)

var (
	kinds  = map[string]bool{Fact: true, Decision: true, Procedure: true, Constraint: true}
	scopes = map[string]bool{ScopeProject: true, ScopeAll: true}
	states = map[string]bool{Active: true, Retired: true}
	tags   = map[string]bool{"stated": true, "accepted": true, "suggested": true, "observed": true, "inferred": true}
	// refPattern is an inbox ref as a source names it, s:<session>#<n>.
	refPattern = regexp.MustCompile(`^s:[^\s#<>\[\]()` + "`" + `]{1,80}#[0-9]{1,9}$`)
	// idPattern is a page's name: k and a decimal number of at most 9
	// digits without leading zeros.
	idPattern = regexp.MustCompile(`^k[1-9][0-9]{0,8}$`)
)

// Errors Read returns for a page it won't model.
var (
	ErrOtherType = errors.New("knowledgepage: the page is not a knowledge page")
	ErrDamaged   = errors.New("knowledgepage: the page can't be read back safely")
)

// Entry is a knowledge page as eva-md reads it.
type Entry struct {
	ID int // the N of knowledge/k<N>
	// Kind, Scope and State are one of their constants; Project is the
	// project key the page was learned in; AsOf is the day of the newest
	// entry the statement cites, YYYY-MM-DD; Tag is the statement's speaker
	// tag, such as stated.
	Kind, Title, Statement, Tag, Scope, Project, State, AsOf string
	// At is the time of the newest entry the statement cites, zero when the
	// page has none.
	At time.Time
	// Refs are the inbox refs the statement cites; Sources those of every
	// statement the page has held, the newest last.
	Refs, Sources []string
	// Code are the code sources, <project key>@<commit>:<path>, as written,
	// and Paths the paths they cite, read with coderef.Parse.
	Code, Paths []string
	// History are the History section's lines, as written.
	History []string
	// Locked and Verified report a page a person locked or verified, which
	// eva-md never writes.
	Locked, Verified bool

	other   []string            // sections a person added, each verbatim
	keep    []okfadapter.Source // sources that are neither inbox refs nor code
	damaged bool                // read leniently from a page Read calls damaged, which is never written
}

// Name is a page's name, k<N>, as the model and the reader see it.
func Name(id int) string { return "k" + strconv.Itoa(id) }

// Ref is a page's ref, knowledge/k<N>, as eva-md show takes it.
func Ref(id int) string { return Folder + "/" + Name(id) }

// Name is the entry's name, k<N>.
func (e Entry) Name() string { return Name(e.ID) }

// Ref is the entry's ref, knowledge/k<N>.
func (e Entry) Ref() string { return Ref(e.ID) }

// ParseID reads a page's number from its name, k<N>, its file name, its ref
// or its path in the wiki, such as k12, k12.md, knowledge/k12 or
// knowledge/k12.md.
func ParseID(s string) (int, bool) {
	s = strings.TrimSuffix(strings.TrimPrefix(s, Folder+"/"), ".md")
	if !idPattern.MatchString(s) {
		return 0, false
	}
	n, err := strconv.Atoi(s[1:])
	return n, err == nil
}

// Read models a page okf parsed. A page of another type is ErrOtherType; a
// page whose frontmatter values or body eva-md can't read back safely is
// ErrDamaged, and is never written. So is one whose body, as eva-md would
// write it back, okf's save refuses (see okfadapter.SmugglesFrontmatter). A
// locked or verified page reads as usual, with Locked or Verified set.
func Read(c *okfadapter.Concept) (Entry, error) {
	e, err := front(c)
	if err != nil {
		return Entry{}, err
	}
	b, ok := parseBody(c.Body, true)
	if !ok {
		return Entry{}, fmt.Errorf("%w: the body is not in the form eva-md writes", ErrDamaged)
	}
	e.Statement, e.Tag, e.Refs, e.At, e.History, e.other = b.statement, b.tag, b.refs, b.at, b.history, b.other
	if okfadapter.SmugglesFrontmatter(e.body()) {
		return Entry{}, fmt.Errorf("%w: a block in the body holds a frontmatter key the wiki refuses", ErrDamaged)
	}
	// okf's save refuses a title or description with a line break or ---,
	// and the statement is written back as the description.
	if refusedMeta(e.Title) || refusedMeta(e.Statement) {
		return Entry{}, fmt.Errorf("%w: the title or statement holds a line break or the --- the wiki refuses", ErrDamaged)
	}
	return e, nil
}

// refusedMeta reports a title or description okf's save refuses.
func refusedMeta(s string) bool {
	return strings.ContainsAny(s, "\r\n") || strings.Contains(s, "---")
}

// readDamaged models a page Read calls damaged whose frontmatter it reads,
// leniently, as search reads a body: text eva-md doesn't know is passed
// over, and one statement line without its tag is the statement, or else
// the description. Such a page is still listed, read-only, and never
// written. It reports false when it can't model the page.
func readDamaged(c *okfadapter.Concept) (Entry, bool) {
	e, err := front(c)
	if err != nil {
		return Entry{}, false
	}
	b, _ := parseBody(c.Body, false)
	e.Statement, e.Tag, e.Refs, e.At, e.History = b.statement, b.tag, b.refs, b.at, b.history
	if e.Statement == "" {
		e.Statement = cleanText(c.Description, MaxStatement)
	}
	e.damaged = true
	return e, e.Statement != ""
}

// front models a page's frontmatter: its id, type, title, the values eva-md
// writes, and its sources.
func front(c *okfadapter.Concept) (Entry, error) {
	id, ok := ParseID(c.ID)
	if !ok {
		return Entry{}, fmt.Errorf("%w: its name is not k<N>", ErrDamaged)
	}
	if strings.TrimSpace(c.Type) != pageType {
		return Entry{}, ErrOtherType
	}
	e := Entry{ID: id, Title: strings.TrimSpace(c.Title), Locked: okfadapter.IsLocked(c),
		Verified: len(c.Verified) > 0 || okfadapter.VerifiedIn([]byte(c.RawContent))}
	str := func(key string) string { s, _ := c.Extra[key].(string); return strings.TrimSpace(s) }
	e.Kind, e.Scope, e.Project, e.State, e.AsOf = str("kind"), str("scope"), str("project"), str("state"), str("as_of")
	if !kinds[e.Kind] || !scopes[e.Scope] || !states[e.State] || e.Project == "" || e.Title == "" || !day(e.AsOf) {
		return Entry{}, fmt.Errorf("%w: a frontmatter value is missing or not one eva-md writes", ErrDamaged)
	}
	var sources []string
	for _, s := range c.Sources {
		if s.ID != "" || s.Title != "" || s.Author != "" || s.LastModified != "" || s.UsageCount != 0 || len(s.Extra) > 0 {
			e.keep = append(e.keep, s)
			continue
		}
		sources = append(sources, s.Resource)
	}
	var others []string
	e.Sources, e.Code, e.Paths, others = splitSources(sources)
	for _, s := range others {
		e.keep = append(e.keep, okfadapter.Source{Resource: s})
	}
	return e, nil
}

// splitSources sorts a page's sources into inbox refs, code sources (with
// the paths they cite) and the rest, keeping each one's order.
func splitSources(sources []string) (refs, code, paths, other []string) {
	for _, s := range sources {
		s = strings.TrimSpace(s)
		switch r, isCode := coderef.Parse(s); {
		case refPattern.MatchString(s):
			refs = append(refs, s)
		case isCode:
			code = append(code, s)
			if r.Path != "" && !slices.Contains(paths, r.Path) {
				paths = append(paths, r.Path)
			}
		case s != "":
			other = append(other, s)
		}
	}
	return refs, code, paths, other
}

// set puts the entry into c, keeping any other frontmatter a person added.
func (e Entry) set(c *okfadapter.Concept) {
	c.Type, c.Title, c.Description, c.Status = pageType, e.Title, e.Statement, "stable"
	c.Sources = nil
	sources := e.Sources[max(len(e.Sources)-MaxSources, 0):]
	for _, s := range slices.Concat(sources, e.Code) {
		c.Sources = append(c.Sources, okfadapter.Source{Resource: s})
	}
	c.Sources = append(c.Sources, e.keep...)
	if c.Extra == nil {
		c.Extra = map[string]any{}
	}
	c.Extra["as_of"], c.Extra["kind"], c.Extra["project"] = e.AsOf, e.Kind, e.Project
	c.Extra["scope"], c.Extra["state"] = e.Scope, e.State
	c.Body = e.body()
}

// Render returns the page as okf saves it, with no generated line, for a
// check of the saved text (such as the daily commit's secret scan) before it
// is written.
func (e Entry) Render() string {
	c := &okfadapter.Concept{ID: e.Ref(), Path: e.Ref() + ".md"}
	e.set(c)
	return okfadapter.Serialize(c)
}

// body is the page's body: the Statement, the History when it has lines,
// any section a person added, and the map link. It starts with a blank line
// and ends with one newline, as okf reads a body back.
func (e Entry) body() string {
	var b strings.Builder
	b.WriteString("\n" + statementHeading + "\n" + e.statementLine() + "\n")
	if len(e.History) > 0 {
		b.WriteString("\n" + historyHeading + "\n")
		for _, l := range e.History {
			b.WriteString(l + "\n")
		}
	}
	for _, s := range e.other {
		b.WriteString("\n" + s + "\n")
	}
	b.WriteString("\n" + MapLink + "\n")
	return b.String()
}

// statementLine is "<statement> [<tag>] <refs> <!-- t:<time> -->", each
// part after the statement only when it has a value.
func (e Entry) statementLine() string {
	parts := []string{e.Statement}
	if e.Tag != "" {
		parts = append(parts, "["+e.Tag+"]")
	}
	parts = append(parts, e.Refs...)
	if !e.At.IsZero() {
		parts = append(parts, "<!-- t:"+canonical(e.At).Format(time.RFC3339Nano)+" -->")
	}
	return strings.Join(parts, " ")
}

// parsedBody is a page's body as parseBody reads it.
type parsedBody struct {
	statement, tag string
	refs           []string
	at             time.Time
	history        []string
	other          []string
}

// parseBody reads a body in the form body writes. Strictly, as before a
// write, any other text in the Statement or History section, or before the
// first section, makes it unreadable, so a write never loses a person's
// text; leniently, as search reads it, such text is passed over: the first
// statement line with its tag is the statement, or else the first without
// one, as it stands.
func parseBody(text string, strict bool) (parsedBody, bool) {
	var b parsedBody
	var section string
	var current []string // the lines of a section a person added
	seen := map[string]bool{}
	statements := 0
	tagless := "" // the first statement line without a tag, read leniently
	fence := ""
	flush := func() {
		if current != nil {
			for len(current) > 0 && strings.TrimSpace(current[len(current)-1]) == "" {
				current = current[:len(current)-1]
			}
			b.other = append(b.other, strings.Join(current, "\n"))
			current = nil
		}
	}
	for _, l := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		if fence == "" && strings.HasPrefix(l, "## ") {
			flush()
			section = strings.TrimRight(l, " \t")
			if section == statementHeading || section == historyHeading {
				if seen[section] && strict {
					return parsedBody{}, false
				}
				seen[section] = true
			} else {
				current = []string{l}
			}
			continue
		}
		if fence == "" && strings.TrimSpace(l) == MapLink {
			continue
		}
		switch {
		case section == statementHeading:
			if strings.TrimSpace(l) == "" {
				continue
			}
			if statements++; statements > 1 && strict {
				return parsedBody{}, false
			}
			text, tag, refs, at, ok := parseStatement(l)
			switch {
			case !ok && strict:
				return parsedBody{}, false
			case ok && b.tag == "":
				b.statement, b.tag, b.refs, b.at = text, tag, refs, at
			case !ok && tagless == "":
				// A person rewrote the line in their own words, without its
				// tag: unless another line has one, it is the statement.
				tagless = cleanText(l, MaxStatement)
			}
		case section == historyHeading:
			if t := strings.TrimSpace(l); t == "" {
				continue
			} else if !strings.HasPrefix(t, "- ") {
				if strict {
					return parsedBody{}, false
				}
				continue
			}
			b.history = append(b.history, strings.TrimSpace(l))
		case current != nil:
			current = append(current, l)
			fence = threadpage.NextFence(fence, l)
		case strings.TrimSpace(l) != "" && strict:
			return parsedBody{}, false
		}
	}
	flush()
	if b.statement == "" {
		b.statement = tagless
	}
	return b, b.statement != ""
}

// parseStatement reads a Statement line: the statement, then its tag, its
// refs and the hidden comment with the time of the newest entry it cites.
// The escaper turns a tag in free text into parentheses, so the last tag on
// the line ends the statement.
func parseStatement(l string) (text, tag string, refs []string, at time.Time, ok bool) {
	l = strings.TrimSpace(l)
	if strings.HasSuffix(l, "-->") {
		i := strings.LastIndex(l, "<!--")
		if i < 0 {
			return "", "", nil, time.Time{}, false
		}
		for _, f := range strings.Fields(l[i+4 : len(l)-3]) {
			if v, found := strings.CutPrefix(f, "t:"); found {
				if t, err := time.Parse(time.RFC3339Nano, v); err == nil {
					at = canonical(t)
				}
			}
		}
		l = strings.TrimSpace(l[:i])
	}
	words := strings.Fields(l)
	n := len(words)
	for n > 0 && refPattern.MatchString(words[n-1]) {
		n--
	}
	refs = slices.Clone(words[n:])
	if n == 0 || !strings.HasPrefix(words[n-1], "[") || !strings.HasSuffix(words[n-1], "]") || !tags[strings.Trim(words[n-1], "[]")] {
		return "", "", nil, time.Time{}, false
	}
	tag = strings.Trim(words[n-1], "[]")
	text = strings.Join(words[:n-1], " ")
	return text, tag, refs, at, text != ""
}

// Lenient reads a saved page as search does, without okf: its frontmatter
// line by line and its Statement and History sections, where a statement
// line without its tag counts as it stands; without a statement line, the
// description is the statement. It reports false for a page that is not a
// knowledge page or that has no title or statement. A
// value that isn't one eva-md writes is left empty. The ID is the caller's:
// search knows it from the file name (see ParseID).
func Lenient(data []byte) (Entry, bool) {
	text := strings.ReplaceAll(string(data), "\r\n", "\n")
	rest, ok := strings.CutPrefix(text, "---\n")
	if !ok {
		return Entry{}, false
	}
	end := strings.Index(rest, "\n---\n")
	if end < 0 {
		if !strings.HasSuffix(rest, "\n---") {
			return Entry{}, false
		}
		end = len(rest) - 4
	}
	front, after := rest[:end], ""
	if end+5 <= len(rest) {
		after = rest[end+5:]
	}
	var e Entry
	values := map[string]string{}
	var sources []string
	block := ""
	for _, l := range strings.Split(front, "\n") {
		if strings.HasPrefix(l, " ") || strings.HasPrefix(l, "\t") {
			if block == "sources" {
				item, ok := strings.CutPrefix(strings.TrimSpace(l), "- resource:")
				if ok && len(sources) < 4*MaxSources {
					sources = append(sources, scalar(item))
				}
			}
			continue
		}
		key, value, ok := strings.Cut(l, ":")
		if !ok {
			block = ""
			continue
		}
		key = strings.TrimSpace(key)
		block = key
		if _, dup := values[key]; !dup {
			values[key] = scalar(value)
		}
	}
	if values["type"] != pageType {
		return Entry{}, false
	}
	pick := func(key string, allowed map[string]bool) string {
		if v := values[key]; allowed[v] {
			return v
		}
		return ""
	}
	e.Title, e.Project = values["title"], values["project"]
	e.Kind, e.Scope, e.State = pick("kind", kinds), pick("scope", scopes), pick("state", states)
	if day(values["as_of"]) {
		e.AsOf = values["as_of"]
	}
	switch strings.ToLower(values["locked"]) {
	case "true", "yes", "on":
		e.Locked = true
	}
	_, e.Verified = values["verified"]
	e.Sources, e.Code, e.Paths, _ = splitSources(sources)
	b, _ := parseBody(after, false)
	e.Statement, e.Tag, e.Refs, e.At, e.History = b.statement, b.tag, b.refs, b.at, b.history
	if e.Statement == "" {
		e.Statement = values["description"]
	}
	return e, e.Title != "" && e.Statement != ""
}

// scalar reads a frontmatter value as okf writes one: JSON-quoted, single
// quoted or plain.
func scalar(v string) string {
	v = strings.TrimSpace(v)
	switch {
	case strings.HasPrefix(v, `"`):
		var s string
		if json.Unmarshal([]byte(v), &s) == nil {
			return strings.TrimSpace(s)
		}
		return ""
	case len(v) >= 2 && strings.HasPrefix(v, "'") && strings.HasSuffix(v, "'"):
		return strings.TrimSpace(strings.ReplaceAll(v[1:len(v)-1], "''", "'"))
	}
	return v
}

// day reports a date as as_of holds one, YYYY-MM-DD.
func day(s string) bool {
	_, err := time.Parse(time.DateOnly, s)
	return err == nil
}

// canonical is t with its monotonic reading dropped, in its own offset, or
// in UTC when it has none, as thread pages write times.
func canonical(t time.Time) time.Time {
	if t.IsZero() {
		return time.Time{}
	}
	t = t.Round(0)
	if _, off := t.Zone(); off != 0 {
		return t.In(time.FixedZone("", off))
	}
	return t.UTC()
}

// cleanText is free text as a page writes it: one line escaped by the
// thread page escaper, of at most n characters, without the "---" okf
// refuses in a title or description.
func cleanText(s string, n int) string {
	s = threadpage.CleanLine(s, 0)
	for strings.Contains(s, "---") {
		s = strings.ReplaceAll(s, "---", "--")
	}
	return threadpage.CleanLine(s, n)
}

// normalize is text as two statements are compared: lowercased letters and
// digits, one space between words.
func normalize(s string) string {
	return strings.Join(words(s, 1), " ")
}
