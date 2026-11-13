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
// bad_segment. RoleFixed: its as does not fit its entry's author. Quoted:
// the span lies wholly in lines the renderer marks as quoted, so a user's
// entry shows it but does not say it.
type Cited struct {
	E         int64
	Session   string
	Author    event.Author
	As        string
	Words     string
	Code      string
	RoleFixed bool
	Quoted    bool
}

// checker holds what Check reads for every operation of one answer.
type checker struct {
	f       Form
	entries map[int64]Entry
	flat    map[int64]string // each entry's text with its white space collapsed
	// quotedAt marks, for each byte of an entry's flat text, whether it
	// lies in a line the renderer marks as quoted; nil for an entry with
	// no such line. quotedSeg marks each segment of an entry in the
	// segment form the same way.
	quotedAt  map[int64][]bool
	quotedSeg map[int64][]bool
	// firstTyped is, for each agent entry, the first typed prompt shown
	// after it, 0 when there is none.
	firstTyped map[int64]int64
	shown      map[string]Shown
}

func newChecker(in Input, shown []Shown, f Form) *checker {
	c := &checker{f: f, entries: map[int64]Entry{}, flat: map[int64]string{}, quotedAt: map[int64][]bool{},
		quotedSeg: map[int64][]bool{}, firstTyped: map[int64]int64{}, shown: map[string]Shown{}}
	var ns []int64
	for _, e := range in.Entries {
		if _, ok := c.entries[e.N]; ok {
			continue
		}
		c.entries[e.N] = e
		if f == FormSegments {
			c.quotedSeg[e.N] = quotedSegments(e)
		} else {
			c.flat[e.N], c.quotedAt[e.N] = flatten(e.Text)
		}
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
// starting and ending at a word boundary, and 8 to 240 characters unless it
// is the whole text; in the segment form its segment must exist, and its
// text becomes the quote. A span is quoted when it lies wholly in lines the
// renderer marks as quoted: in the quotes form when every place it is
// found does, in the segment form when its segment's line does.
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
		ct.Quoted = c.quotedSeg[s.E][s.S-1]
		return ct
	}
	q, text := collapse(s.Q), c.flat[s.E]
	n := utf8.RuneCountInString(q)
	if q == "" || (q != text && (n < MinQuote || n > MaxQuote)) {
		ct.Code = "quote_not_found"
		return ct
	}
	found, quoted := false, true
	for from := 0; from <= len(text) && quoted; {
		i := strings.Index(text[from:], q)
		if i < 0 {
			break
		}
		at, end := from+i, from+i+len(q)
		if bounded(text, at, end) {
			found = true
			quoted = within(c.quotedAt[s.E], at, end)
		}
		_, size := utf8.DecodeRuneInString(text[at:])
		from = at + size
	}
	if !found {
		ct.Code = "quote_not_found"
		return ct
	}
	ct.Words, ct.Quoted = q, quoted
	return ct
}

// wordRune reports a rune that is part of a word: a letter, a digit, or a
// mark that combines with the letter before it.
func wordRune(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r) || unicode.Is(unicode.M, r)
}

// bounded reports whether text[at:end] starts and ends at a word boundary:
// where its first rune is part of a word, no part of a word stands right
// before it, and where its last is, none stands right after it.
func bounded(text string, at, end int) bool {
	first, _ := utf8.DecodeRuneInString(text[at:])
	before, nb := utf8.DecodeLastRuneInString(text[:at])
	last, _ := utf8.DecodeLastRuneInString(text[:end])
	after, na := utf8.DecodeRuneInString(text[end:])
	return !(nb > 0 && wordRune(first) && wordRune(before)) && !(na > 0 && wordRune(last) && wordRune(after))
}

// within reports whether every byte of text[at:end] lies in a quoted line,
// by quotedAt, which is nil for an entry with none.
func within(quotedAt []bool, at, end int) bool {
	return quotedAt != nil && !slices.Contains(quotedAt[at:end], false)
}

// collapse turns each run of white space in s into one space and drops it
// at either end: the one change the quote match allows.
func collapse(s string) string { return strings.Join(strings.Fields(s), " ") }

// quotedLine reports a line of an entry's text that the renderer marks as
// quoted: one that starts with Mark.
func quotedLine(l string) bool { return strings.HasPrefix(l, Mark) }

// flatten is text collapsed as collapse does it, and for each of its bytes
// whether it lies in a quoted line: a word's by its line's, and a space
// between two words only when both words lie in quoted lines. The marks
// are nil when no line is quoted.
func flatten(text string) (string, []bool) {
	ls := strings.Split(text, "\n")
	if !slices.ContainsFunc(ls, quotedLine) {
		return collapse(text), nil
	}
	var b strings.Builder
	var at []bool
	prev := false
	for _, l := range ls {
		q := quotedLine(l)
		for _, w := range strings.Fields(l) {
			if b.Len() > 0 {
				b.WriteByte(' ')
				at = append(at, prev && q)
			}
			b.WriteString(w)
			for range len(w) {
				at = append(at, q)
			}
			prev = q
		}
	}
	return b.String(), at
}

// quotedSegments marks each of e's segments that lies in a quoted line. A
// segment lies within one line, so its line is found by splitting each
// line of e's text as the renderer does. For an entry whose segments that
// does not give, such as one built by hand, a segment is marked when it
// starts with Mark.
func quotedSegments(e Entry) []bool {
	var segs []string
	var out []bool
	for _, l := range strings.Split(e.Text, "\n") {
		for _, s := range segments([]string{l}) {
			segs = append(segs, s)
			out = append(out, quotedLine(l))
		}
	}
	if slices.Equal(segs, e.Segments) {
		return out
	}
	out = make([]bool, len(e.Segments))
	for i, s := range e.Segments {
		out[i] = quotedLine(s)
	}
	return out
}

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
// of the operation's proposal spans, and gives nothing otherwise. A span of
// a user's entry, typed or chosen, that lies wholly in quoted lines is what
// the user showed, not what the user said: external, as an entry pasted
// whole is.
func (c *checker) spanTag(s Cited, proposals []int64) string {
	if s.Quoted && (s.Author == event.AuthorTyped || s.Author == event.AuthorChosen) {
		return TagExternal
	}
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
			tok := trimToken(f)
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

// trimToken trims the marks edge reports from either end of a word, but
// for a point that opens a number, as in ".5", which is part of its value.
func trimToken(w string) string {
	w = strings.TrimRightFunc(w, edge)
	for w != "" {
		r, n := utf8.DecodeRuneInString(w)
		if !edge(r) {
			break
		}
		if next, _ := utf8.DecodeRuneInString(w[n:]); r == '.' && unicode.IsDigit(next) {
			break
		}
		w = w[n:]
	}
	return w
}

// edge reports a mark trimmed from either end of a token: punctuation that
// ends a sentence or a clause, brackets and quotes.
func edge(r rune) bool { return strings.ContainsRune(".,;:!?()[]{}<>\"'`“”‘’", r) }

// mustGround reports a token that holds a digit, a slash, an underscore or
// a dot between two letters, but for a dot of an abbreviation such as
// "e.g." or "a.m.", whose letters on both sides stand alone: a letter that
// is the whole of its run of letters.
func mustGround(tok string) bool {
	rs := []rune(tok)
	letter := func(i int) bool { return i >= 0 && i < len(rs) && unicode.IsLetter(rs[i]) }
	for i, r := range rs {
		switch {
		case unicode.IsDigit(r), r == '/', r == '_':
			return true
		case r == '.' && letter(i-1) && letter(i+1) && (letter(i-2) || letter(i+2)):
			return true
		}
	}
	return false
}

// joiners are the marks that join two parts of one value: a decimal point
// or a version's dot, a thousands mark, a path's slash, an identifier's
// underscore, a range's dash and a time's colon.
const joiners = ".,/_-:"

// numeric reports a token made of digits alone, with a sign before them or
// points or thousands marks among them: a number, not a path or a name.
func numeric(tok string) bool {
	digits := false
	for i, r := range tok {
		switch {
		case unicode.IsDigit(r):
			digits = true
		case r == '.' || r == ',':
		case i == 0 && (r == '-' || r == '+' || r == '−'):
		default:
			return false
		}
	}
	return digits
}

// holds reports whether words hold tok whole: at a place where the value
// goes on neither before nor after it. It goes on where a letter or a
// digit stands right next to it, or a joiner with a letter or a digit
// beyond, but for a number, which a hyphen or a slash and then a letter
// end, as in 30-second or 3/day; and before a token that starts with a
// digit, where a minus sign or a point stands right before it. So 5 is not
// held by 15, 5.5, 1,500, 4-5, 5-6, -5 or .5, 1.2 not by 1.2.3, 6 not by
// PELICAN-6, and scripts/bench not by scripts/bench/out; 5 is held by
// "5.", "5, then" and "5-minute".
func holds(words, tok string) bool {
	alnum := func(r rune) bool { return unicode.IsLetter(r) || unicode.IsDigit(r) }
	joiner := func(r rune) bool { return strings.ContainsRune(joiners, r) }
	digit := tok != "" && unicode.IsDigit([]rune(tok)[0])
	number := numeric(tok)
	for from := 0; from <= len(words); {
		i := strings.Index(words[from:], tok)
		if i < 0 {
			return false
		}
		at, end := from+i, from+i+len(tok)
		b1, n1 := utf8.DecodeLastRuneInString(words[:at])
		b2, _ := utf8.DecodeLastRuneInString(words[:at-n1])
		a1, m1 := utf8.DecodeRuneInString(words[end:])
		a2, _ := utf8.DecodeRuneInString(words[end+m1:])
		goesOnBefore := n1 > 0 && (alnum(b1) || joiner(b1) && at-n1 > 0 && alnum(b2) || digit && (b1 == '-' || b1 == '.'))
		ends := number && (a1 == '-' || a1 == '/') && unicode.IsLetter(a2)
		goesOnAfter := m1 > 0 && (alnum(a1) || joiner(a1) && end+m1 < len(words) && alnum(a2) && !ends)
		if !goesOnBefore && !goesOnAfter {
			return true
		}
		_, size := utf8.DecodeRuneInString(words[at:])
		from = at + size
	}
	return false
}
