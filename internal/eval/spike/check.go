package spike

import (
	"fmt"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/heyNag/eva-md/internal/ledger/event"
)

// The tags an operation takes from the record, highest first.
const (
	TagStated    = "stated"
	TagAccepted  = "accepted"
	TagObserved  = "observed"
	TagSuggested = "suggested"
	TagExternal  = "external"
)

// tags are the tags, lowest first, so that a tag's index is its rank.
var tags = []string{TagExternal, TagSuggested, TagObserved, TagAccepted, TagStated}

// rank is a tag's rank, -1 for no tag.
func rank(tag string) int { return slices.Index(tags, tag) }

// The kinds a shown next step and a shown open question have; a shown item
// has one of the plan's five.
const (
	KindNext     = "next"
	KindQuestion = "question"
)

// Verdict is the checker's judgement of one answer: one Judged for each of
// its operations, in the answer's order.
type Verdict struct {
	Ops []Judged
	// Breaks: the answer broke a bound or an enum its schema states (F-064):
	// more operations than MaxOps, an unknown nothing, or an operation
	// whose Breaks is set.
	Breaks bool
	// RepairBreaks: set by ApplyRepair when the repair answer broke its
	// schema in the same way.
	RepairBreaks bool
}

// Judged is one operation as judged: accepted when Code is empty, else
// refused with one code of Codes, at Place.
type Judged struct {
	Op    Op
	Code  string
	Place string
	// Tag is the tag the operation's spans give it, once the checker came
	// that far; "" before, or when no span gives one.
	Tag string
	// Spans are its spans as resolved, once its shape and target passed.
	Spans []Cited
	// Breaks: the operation breaks a bound or an enum of the schema.
	Breaks bool
	// Sent: ApplyRepair sent it to the repair round. Repaired: the repair
	// answer replaced it, and Op is the replacement.
	Sent, Repaired bool
}

// Accepted reports an operation the checker accepted.
func (j Judged) Accepted() bool { return j.Code == "" }

// Cited is one span as resolved: the entry it names, and the words it
// cites, a quote with its white space collapsed or the segment's text.
// Code is "" for a span found, else bad_entry, quote_not_found or
// bad_segment. RoleFixed: its as does not fit its entry's author.
type Cited struct {
	E         int64
	Session   string
	Author    event.Author
	As        string
	Words     string
	Code      string
	RoleFixed bool
}

// checker holds what Check reads for every operation of one answer.
type checker struct {
	f       Form
	entries map[int64]Entry
	flat    map[int64]string // each entry's text with its white space collapsed
	// firstTyped is, for each agent entry, the first typed prompt shown
	// after it, 0 when there is none.
	firstTyped map[int64]int64
	shown      map[string]Shown
}

func newChecker(in Input, shown []Shown, f Form) *checker {
	c := &checker{f: f, entries: map[int64]Entry{}, flat: map[int64]string{}, firstTyped: map[int64]int64{}, shown: map[string]Shown{}}
	var ns []int64
	for _, e := range in.Entries {
		if _, ok := c.entries[e.N]; ok {
			continue
		}
		c.entries[e.N] = e
		c.flat[e.N] = collapse(e.Text)
		ns = append(ns, e.N)
	}
	slices.Sort(ns)
	next := int64(0)
	for _, n := range slices.Backward(ns) {
		e := c.entries[n]
		if e.Author == event.AuthorAgent {
			c.firstTyped[n] = next
		}
		if e.Author == event.AuthorTyped && e.Kind == event.KindPrompt {
			next = n
		}
	}
	for _, s := range shown {
		if _, ok := c.shown[s.Handle]; !ok {
			c.shown[s.Handle] = s
		}
	}
	return c
}

// Check judges each operation of answer a to the pass rendered as in, with
// the items shown, in form f: accepted with its tag, or refused with one
// code of Codes. Each operation is judged alone, with one exception: a
// handle that an operation accepted before it in the answer already
// changes is no longer a target. A refused operation never takes another
// with it. Check is pure: it reads no clock and changes nothing.
func Check(in Input, shown []Shown, a Answer, f Form) Verdict {
	c := newChecker(in, shown, f)
	v := Verdict{Ops: make([]Judged, len(a.Ops))}
	if len(a.Ops) > MaxOps || (a.Nothing != nil && !slices.Contains(nothings, *a.Nothing)) {
		v.Breaks = true
	}
	claimed := map[string]bool{}
	for i, op := range a.Ops {
		v.Ops[i] = c.judge(i, op, claimed)
		v.Breaks = v.Breaks || v.Ops[i].Breaks
	}
	return v
}

// judge judges op at index i of its answer; claimed holds the handles that
// operations accepted before it change, and gains its own when it is
// accepted.
func (c *checker) judge(i int, op Op, claimed map[string]bool) Judged {
	j := Judged{Op: op, Breaks: breaks(op)}
	if !c.f.Valid() {
		j.Code, j.Place = "bad_shape", "form"
		return j
	}
	if place := shape(i, op); place != "" {
		j.Code, j.Place = "bad_shape", place
		return j
	}
	target, ok := c.target(op, claimed)
	if !ok {
		j.Code, j.Place = "unknown_target", "id"
		return j
	}
	j.Spans = make([]Cited, len(op.Ev))
	for k, s := range op.Ev {
		j.Spans[k] = c.cite(s)
		if j.Spans[k].Code != "" && j.Code == "" {
			j.Code, j.Place = j.Spans[k].Code, spanPlace(k, j.Spans[k].Code)
		}
	}
	if j.Code != "" {
		return j
	}
	j.Tag = c.tag(j.Spans)
	switch {
	case !enough(op, target, j.Tag):
		j.Code, j.Place = "tag_too_low", "ev"
	case (j.Tag == TagStated || j.Tag == TagAccepted) && op.Text != nil && !grounded(*op.Text, j.Spans):
		j.Code, j.Place = "ungrounded", "text"
	}
	if j.Accepted() && op.ID != nil {
		claimed[*op.ID] = true
	}
	return j
}

// needsID reports the operations that name a shown handle in id.
func needsID(op string) bool {
	return op == "item.supersede" || op == "item.retire" || op == "next.done" || op == "ask.answer"
}

// shape is where op, at index i of its answer, breaks its shape, or "".
func shape(i int, op Op) string {
	in := func(p *string, set []string) bool { return p != nil && slices.Contains(set, *p) }
	switch {
	case i >= MaxOps:
		return "ops"
	case !slices.Contains(opNames, op.Op):
		return "op"
	case (op.ID != nil) != needsID(op.Op):
		return "id"
	}
	switch op.Op {
	case "item.add", "item.supersede":
		if !in(op.Kind, itemKinds) {
			return "kind"
		}
	case "note":
		if !in(op.Kind, noteKinds) {
			return "kind"
		}
	default:
		if op.Kind != nil {
			return "kind"
		}
	}
	if (op.Op == "item.add") != in(op.Admit, admits) || (op.Op != "item.add" && op.Admit != nil) {
		return "admit"
	}
	if op.Op != "next.done" && (op.Text == nil || strings.TrimSpace(*op.Text) == "") {
		return "text"
	}
	if len(op.Ev) < MinSpans || len(op.Ev) > MaxSpans {
		return "ev"
	}
	for k, s := range op.Ev {
		if !slices.Contains(roles, s.As) {
			return fmt.Sprintf("ev.%d.as", k+1)
		}
	}
	return ""
}

// breaks reports an operation that breaks a bound or an enum the schema
// states, which a strict mode would not have let through: an unknown op,
// kind, admit or role, or a number of spans out of bounds.
func breaks(op Op) bool {
	unknown := func(p *string, set ...[]string) bool {
		return p != nil && !slices.ContainsFunc(set, func(s []string) bool { return slices.Contains(s, *p) })
	}
	return !slices.Contains(opNames, op.Op) || unknown(op.Kind, itemKinds, noteKinds) || unknown(op.Admit, admits) ||
		len(op.Ev) < MinSpans || len(op.Ev) > MaxSpans ||
		slices.ContainsFunc(op.Ev, func(s Span) bool { return !slices.Contains(roles, s.As) })
}

// target is the shown item, next step or question op's id names, and
// whether it may be op's target: shown, of the sort op changes, and not
// changed by an operation accepted before it.
func (c *checker) target(op Op, claimed map[string]bool) (Shown, bool) {
	if op.ID == nil {
		return Shown{}, true
	}
	s, ok := c.shown[*op.ID]
	if !ok || claimed[*op.ID] {
		return Shown{}, false
	}
	switch op.Op {
	case "next.done":
		return s, s.Kind == KindNext
	case "ask.answer":
		return s, s.Kind == KindQuestion
	}
	return s, slices.Contains(itemKinds, s.Kind)
}

// spanPlace is where a span's fault lies, its number counted from 1.
func spanPlace(k int, code string) string {
	switch code {
	case "bad_entry":
		return fmt.Sprintf("ev.%d.e", k+1)
	case "bad_segment":
		return fmt.Sprintf("ev.%d.s", k+1)
	}
	return fmt.Sprintf("ev.%d.q", k+1)
}

// cite resolves one span: its entry must be shown; in the quotes form its
// quote must be in the entry's text once both collapse their white space,
// and 8 to 240 characters unless it is the whole text; in the segment form
// its segment must exist, and its text becomes the quote.
func (c *checker) cite(s Span) Cited {
	ct := Cited{E: s.E, As: s.As}
	e, ok := c.entries[s.E]
	if !ok {
		ct.Code = "bad_entry"
		return ct
	}
	ct.Session, ct.Author, ct.RoleFixed = e.Session, e.Author, !roleFits(s.As, e.Author)
	if c.f == FormSegments {
		if s.S < 1 || s.S > int64(len(e.Segments)) {
			ct.Code = "bad_segment"
			return ct
		}
		ct.Words = collapse(e.Segments[s.S-1])
		return ct
	}
	q, text := collapse(s.Q), c.flat[s.E]
	n := utf8.RuneCountInString(q)
	if q == "" || !strings.Contains(text, q) || (q != text && (n < MinQuote || n > MaxQuote)) {
		ct.Code = "quote_not_found"
		return ct
	}
	ct.Words = q
	return ct
}

// collapse turns each run of white space in s into one space and drops it
// at either end: the one change the quote match allows.
func collapse(s string) string { return strings.Join(strings.Fields(s), " ") }

// roleFits reports whether a span's as fits its entry's author: said and
// assent are the user's, typed or chosen; proposal and report the agent's
// or a subagent's; result a tool's. Any other author fits no role.
func roleFits(as string, a event.Author) bool {
	switch as {
	case "said", "assent":
		return a == event.AuthorTyped || a == event.AuthorChosen
	case "proposal", "report":
		return a == event.AuthorAgent || a == event.AuthorSubagent
	case "result":
		return a == event.AuthorTool
	}
	return false
}

// tag is the highest tag the spans give, "" when none gives one.
func (c *checker) tag(spans []Cited) string {
	var proposals []int64
	for _, s := range spans {
		if s.As == "proposal" && s.Author == event.AuthorAgent {
			proposals = append(proposals, s.E)
		}
	}
	best := ""
	for _, s := range spans {
		if t := c.spanTag(s, proposals); rank(t) > rank(best) {
			best = t
		}
	}
	return best
}

// spanTag is the tag one span gives, from its entry's author alone: a typed
// entry is stated, except when cited as an assent, which is accepted when
// the entry is the first typed prompt shown after the agent entry of one
// of the operation's proposal spans, and gives nothing otherwise.
func (c *checker) spanTag(s Cited, proposals []int64) string {
	switch s.Author {
	case event.AuthorTyped:
		if s.As != "assent" {
			return TagStated
		}
		for _, p := range proposals {
			if c.firstTyped[p] == s.E {
				return TagAccepted
			}
		}
		return ""
	case event.AuthorChosen:
		return TagAccepted
	case event.AuthorTool:
		return TagObserved
	case event.AuthorAgent, event.AuthorSubagent:
		return TagSuggested
	}
	return TagExternal
}

// enough reports whether tag is enough for op: an item by its kind, the
// kind of the target for a retire; a next step or a question any tag but
// external; a note any tag. Superseding or retiring an item that is stated
// or accepted needs stated. No tag is enough for nothing.
func enough(op Op, target Shown, tag string) bool {
	if tag == "" {
		return false
	}
	byKind := func(kind string) bool {
		switch kind {
		case "decision", "procedure":
			return tag == TagStated || tag == TagAccepted
		case "constraint", "rule":
			return tag == TagStated
		case "fact":
			return tag == TagStated || tag == TagObserved
		}
		return false
	}
	lattice := target.Tag != TagStated && target.Tag != TagAccepted || tag == TagStated
	switch op.Op {
	case "item.add":
		return byKind(*op.Kind)
	case "item.supersede":
		return byKind(*op.Kind) && lattice
	case "item.retire":
		return byKind(target.Kind) && lattice
	case "note":
		return true
	}
	return tag != TagExternal
}

// grounded reports whether every token of text that must be grounded is in
// one of the spans' words: a token holding a digit, a slash, an
// underscore or a dot between letters, and every token in double quotes or
// backticks. A token is found only whole, where no letter or digit stands
// before or after it.
func grounded(text string, spans []Cited) bool {
	seen := map[string]bool{}
	for _, tok := range groundTokens(text) {
		if seen[tok] {
			continue
		}
		seen[tok] = true
		if !slices.ContainsFunc(spans, func(s Cited) bool { return holds(s.Words, tok) }) {
			return false
		}
	}
	return true
}

// quotePairs are the marks that open and close a quoted name.
var quotePairs = map[rune]rune{'"': '"', '`': '`', '“': '”', '‘': '’'}

// groundTokens lists text's tokens that must be grounded, in order.
func groundTokens(text string) []string {
	var out []string
	add := func(part string, quoted bool) {
		for _, f := range strings.Fields(part) {
			tok := strings.TrimFunc(f, edge)
			if tok != "" && (quoted || mustGround(tok)) {
				out = append(out, tok)
			}
		}
	}
	rest := text
	for rest != "" {
		i := strings.IndexFunc(rest, func(r rune) bool { _, ok := quotePairs[r]; return ok })
		if i < 0 {
			break
		}
		open, size := utf8.DecodeRuneInString(rest[i:])
		end := strings.IndexRune(rest[i+size:], quotePairs[open])
		if end < 0 {
			break
		}
		add(rest[:i], false)
		add(rest[i+size:i+size+end], true)
		rest = rest[i+size+end+utf8.RuneLen(quotePairs[open]):]
	}
	add(rest, false)
	return out
}

// edge reports a mark trimmed from either end of a token: punctuation that
// ends a sentence or a clause, brackets and quotes.
func edge(r rune) bool { return strings.ContainsRune(".,;:!?()[]{}<>\"'`“”‘’", r) }

// mustGround reports a token that holds a digit, a slash, an underscore or
// a dot between two letters.
func mustGround(tok string) bool {
	rs := []rune(tok)
	for i, r := range rs {
		switch {
		case unicode.IsDigit(r), r == '/', r == '_':
			return true
		case r == '.' && i > 0 && i+1 < len(rs) && unicode.IsLetter(rs[i-1]) && unicode.IsLetter(rs[i+1]):
			return true
		}
	}
	return false
}

// holds reports whether words hold tok whole: at a place where no letter
// or digit stands right before or right after it.
func holds(words, tok string) bool {
	alnum := func(r rune) bool { return unicode.IsLetter(r) || unicode.IsDigit(r) }
	for from := 0; from <= len(words); {
		i := strings.Index(words[from:], tok)
		if i < 0 {
			return false
		}
		at := from + i
		before, _ := utf8.DecodeLastRuneInString(words[:at])
		after, _ := utf8.DecodeRuneInString(words[at+len(tok):])
		if (at == 0 || !alnum(before)) && (at+len(tok) == len(words) || !alnum(after)) {
			return true
		}
		_, size := utf8.DecodeRuneInString(words[at:])
		from = at + size
	}
	return false
}
