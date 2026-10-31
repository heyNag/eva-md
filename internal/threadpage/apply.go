package threadpage

import (
	"slices"
	"strconv"
	"strings"
	"time"
)

// Op is one validated operation on the Now.
type Op struct {
	ID          string // existing item; empty for add
	Kind        string // next, question or decision
	Action      string // keep, edit, add, done, answered, superseded or dropped
	Text, Tag   string
	Refs        []string
	At          time.Time
	Reason      string // for dropped
	Replacement string // for superseded and answered
	Code        string // set by the validator for a line eva-md writes itself
}

// Event is one Timeline line to append.
type Event struct {
	Kind      string // decision, done, finding, change, problem or question
	Text, Tag string
	Refs      []string
}

// Update is one validated pass, ready to apply.
type Update struct {
	Date              string // YYYY-MM-DD of the newest cited entry: the Timeline group; see Apply for any other form
	Summary, LastChat string
	Done              []Item
	Ops               []Op
	Events            []Event
	KeepNow           bool     // the out-of-order guard: append Timeline lines, leave the Now alone
	Rules             []RuleOp // changes to the rules pages, which ApplyRules makes; Apply leaves them
}

// Stats counts what Apply changed, for the run log.
type Stats struct {
	Added     int // Now items added, and Timeline lines appended for events
	Edited    int // Now items an edit changed
	Retired   int // items done, answered, superseded or dropped
	Moved     int // items moved to the Timeline past the Next or Open cap
	Duplicate int // adds and events already on the page, left out
	Numbered  int // Now items without an id that got one
	Undated   int // Timeline lines of an update without a YYYY-MM-DD date
}

// Apply places a validated update on p and returns the new page; p itself is
// not changed. Unless KeepNow is set, a non-empty summary or last chat
// replaces the old one, Done is replaced (at most DoneCap items, without
// ids), the operations run, and the Next and Open caps are applied. Then the
// events are appended. Lines retired or moved this pass, then the events, go
// to the Timeline under the update's date: in the last group when it has
// that date, otherwise in a new group at the end, even when the date is
// older.
//
// An update whose date isn't YYYY-MM-DD files its lines under the last
// group's date. On a page without a group it has nowhere to file them, and
// retired lines carry ids that must stay on the page, so an update with lines
// to write changes nothing there. Stats.Undated counts those lines either way.
//
// Operations on existing items run first, in order, and only the first one
// for an id counts. Then each item in Next, Open questions and Decisions in
// force without an id gets one, in page order, so the next pass can name it,
// and the adds follow. A new id is n<k>, k one more than the largest on the
// page, retired lines included, so an id is never used twice. An operation
// on an id that isn't in the Now changes nothing; so do a superseded
// operation whose replacement is empty or the old text, an answered or
// superseded one without a tag, which would show the answer under the
// item's tag (S1), an add whose text and refs are already on an item, or on
// the line moving it at a cap would write, and an event whose text and refs
// match a Timeline line. Applying the same update twice therefore changes
// nothing, as long as its operations name only ids that were on the page it
// was made for.
//
// Past the Next or Open cap, inferred and suggested items move first, then
// the oldest by their time (an item without one counts as oldest, then the
// first on the page). A decision added past DecisionCap is kept under Open
// questions as an OverCapCode line, which never moves and doesn't count
// toward the Open cap.
func Apply(p Page, u Update) (Page, Stats) {
	a := applier{page: clonePage(p), date: groupDate(u.Date), seen: map[string]bool{}}
	undated := a.date == ""
	if n := len(p.Timeline); undated && n > 0 {
		a.date = p.Timeline[n-1].Date
	}
	a.next = maxID(a.page) + 1
	for _, g := range a.page.Timeline {
		for _, l := range g.Lines {
			if t := lineTail(l); t != "" {
				a.seen[t] = true
			}
		}
	}
	if !u.KeepNow {
		a.replace(u)
		a.apply(u.Ops)
		a.capItems(&a.page.Now.Next, NextCap, "next")
		a.capItems(&a.page.Now.Open, OpenCap, "question")
	}
	for _, e := range u.Events {
		a.event(e)
	}
	if undated {
		if len(p.Timeline) == 0 && len(a.lines) > 0 {
			return clonePage(p), Stats{Undated: len(a.lines)}
		}
		a.stats.Undated = len(a.lines)
	}
	a.flush()
	return a.page, a.stats
}

// groupDate returns s, without surrounding spaces, when it is a date in
// YYYY-MM-DD form, otherwise "".
func groupDate(s string) string {
	s = strings.TrimSpace(s)
	if _, err := time.Parse(time.DateOnly, s); err != nil {
		return ""
	}
	return s
}

// applier holds one Apply's state.
type applier struct {
	page  Page
	stats Stats
	date  string
	next  int             // the next id number
	seen  map[string]bool // the tails of the Timeline's lines
	lines []string        // the Timeline lines of this pass
}

// replace swaps in the summary, the last chat and Done.
func (a *applier) replace(u Update) {
	if s := cleanText(u.Summary, SummaryCap); s != "" {
		a.page.Now.Summary = s
	}
	if l := cleanLine(u.LastChat, LastChatCap); l != "" {
		a.page.Now.LastChat = l
	}
	var done []Item
	for _, it := range u.Done {
		if it = cleanItem(it); it.Text != "" && len(done) < DoneCap {
			it.ID = ""
			done = append(done, it)
		}
	}
	a.page.Now.Done = done
}

// apply runs the operations on existing items in order, the first one per
// id only, numbers the items without an id, then runs the adds. This keeps a
// second pass from changing anything: a later operation on an id would act
// on what the first one left, and an add that matched an item retired after
// it would come back.
func (a *applier) apply(ops []Op) {
	done := map[string]bool{}
	for _, op := range ops {
		if done[op.ID] {
			continue
		}
		switch op.Action {
		case "edit":
			a.edit(op)
		case "done", "answered", "superseded", "dropped":
			a.retire(op)
		default:
			continue
		}
		done[op.ID] = true
	}
	a.number()
	for _, op := range ops {
		if op.Action == "add" {
			a.add(op)
		}
	}
}

// number gives each item without an id, such as one a person wrote, the
// next id, in page order.
func (a *applier) number() {
	n := &a.page.Now
	for _, list := range []*[]Item{&n.Next, &n.Open, &n.Decisions} {
		for i := range *list {
			if (*list)[i].ID == "" {
				(*list)[i].ID = "n" + strconv.Itoa(a.next)
				a.next++
				a.stats.Numbered++
			}
		}
	}
}

func (a *applier) add(op Op) {
	it := cleanItem(Item{Text: op.Text, Tag: op.Tag, Refs: op.Refs, At: op.At, Code: op.Code})
	if it.Text == "" {
		return
	}
	n := &a.page.Now
	var list *[]Item
	switch op.Kind {
	case "next":
		list = &n.Next
	case "question":
		list = &n.Open
	case "decision":
		list = &n.Decisions
		if len(n.Decisions) >= DecisionCap {
			list, it.Code = &n.Open, OverCapCode
		}
	default:
		return
	}
	if a.onPage(it) {
		a.stats.Duplicate++
		return
	}
	it.ID = "n" + strconv.Itoa(a.next)
	a.next++
	*list = append(*list, it)
	a.stats.Added++
}

// onPage reports whether the Now holds an item with it.Text and it.Refs, or
// the Timeline holds the line that moving it at a cap writes.
func (a *applier) onPage(it Item) bool {
	n := a.page.Now
	for _, list := range [][]Item{n.Next, n.Open, n.Decisions} {
		for _, have := range list {
			if have.Text == it.Text && slices.Equal(have.Refs, it.Refs) {
				return true
			}
		}
	}
	return a.seen[lineTail(timelineLine("", "", movedText(it), it.Refs, ""))]
}

func (a *applier) edit(op Op) {
	list, i, _ := a.find(op.ID)
	if list == nil {
		return
	}
	old := (*list)[i]
	it := old
	if t := cleanLine(op.Text, TextCap); t != "" {
		it.Text = t
	}
	if op.Tag != "" {
		it.Tag = op.Tag
	}
	if refs := cleanRefs(op.Refs); refs != nil {
		it.Refs = refs
	}
	if !op.At.IsZero() {
		it.At = canonical(op.At)
	}
	if c := cleanCode(op.Code); c != "" {
		it.Code = c
	}
	if !sameItem(it, old) {
		(*list)[i] = it
		a.stats.Edited++
	}
}

// retire writes an item's retired line and takes it out of the Now; a
// superseded item stays, under its id, with the replacement. An answer or
// replacement without a tag would show under the item's tag, so it changes
// nothing.
func (a *applier) retire(op Op) {
	list, i, kind := a.find(op.ID)
	if list == nil {
		return
	}
	if (op.Action == "answered" || op.Action == "superseded") && tagToken(op.Tag) == "" {
		return
	}
	it := (*list)[i]
	if it.Code == OverCapCode {
		kind = "decision" // kept under Open questions, but a decision
	}
	text := label(it, false)
	refs := mergeRefs(it.Refs, cleanRefs(op.Refs))
	replacement := cleanLine(op.Replacement, TextCap)
	switch op.Action {
	case "done":
		a.retired("done", it, text, refs)
	case "answered":
		if replacement != "" {
			text += " → " + replacement + ownTag(op.Tag, it.Tag)
		}
		a.retired("answered", it, text, refs)
	case "dropped":
		why := "(dropped)"
		if r := cleanLine(op.Reason, TextCap); r != "" {
			why = "(dropped: " + r + ")"
		}
		a.retired(kind, it, "~~"+text+"~~ "+why, refs)
	case "superseded":
		if replacement == "" || replacement == it.Text {
			return
		}
		a.retired(kind, it, "~~"+text+"~~ → "+replacement+ownTag(op.Tag, it.Tag)+" "+joinNonEmpty(" ", "(superseded", a.date)+")", refs)
		it.Text, it.Tag = replacement, op.Tag
		if r := cleanRefs(op.Refs); r != nil {
			it.Refs = r
		}
		if !op.At.IsZero() {
			it.At = canonical(op.At)
		}
		(*list)[i] = it
		a.stats.Retired++
		return
	}
	*list = remove(*list, i)
	a.stats.Retired++
}

// capItems moves items past limit to the Timeline, never an OverCapCode line.
func (a *applier) capItems(list *[]Item, limit int, kind string) {
	for {
		best, count := -1, 0
		for i, it := range *list {
			if it.Code == OverCapCode {
				continue
			}
			count++
			if best < 0 || movesFirst(it, (*list)[best]) {
				best = i
			}
		}
		if count <= limit {
			return
		}
		it := (*list)[best]
		a.retired(kind, it, movedText(it), it.Refs)
		*list = remove(*list, best)
		a.stats.Moved++
	}
}

// movesFirst reports whether x moves before y at a cap: an assistant's item
// before any other, then the older one, an item without a time first.
func movesFirst(x, y Item) bool {
	if ax, ay := assistant(x.Tag), assistant(y.Tag); ax != ay {
		return ax
	}
	if x.At.IsZero() != y.At.IsZero() {
		return x.At.IsZero()
	}
	return x.At.Before(y.At)
}

func assistant(tag string) bool { return tag == "inferred" || tag == "suggested" }

func movedText(it Item) string { return label(it, false) + " (moved: cap)" }

func (a *applier) event(e Event) {
	text := cleanLine(e.Text, TextCap)
	if text == "" {
		return
	}
	line := timelineLine(word(e.Kind), e.Tag, text, cleanRefs(e.Refs), "")
	if a.seen[lineTail(line)] {
		a.stats.Duplicate++
		return
	}
	a.add1(line)
	a.stats.Added++
}

// retired adds the Timeline line of an item leaving the Now, with its tag
// and id comment.
func (a *applier) retired(kind string, it Item, text string, refs []string) {
	a.add1(timelineLine(kind, it.Tag, text, refs, comment(it.ID, it.At)))
}

func (a *applier) add1(line string) {
	a.lines = append(a.lines, line)
	if t := lineTail(line); t != "" {
		a.seen[t] = true
	}
}

// flush adds this pass's lines to the Timeline.
func (a *applier) flush() {
	if len(a.lines) == 0 {
		return
	}
	tl := a.page.Timeline
	if n := len(tl); n > 0 && tl[n-1].Date == a.date {
		tl[n-1].Lines = append(tl[n-1].Lines, a.lines...)
		return
	}
	a.page.Timeline = append(tl, Group{Date: a.date, Lines: a.lines})
}

// find returns the list holding the Now item with id, its index and its
// kind, or a nil list.
func (a *applier) find(id string) (*[]Item, int, string) {
	if id == "" {
		return nil, -1, ""
	}
	n := &a.page.Now
	for _, s := range []struct {
		list *[]Item
		kind string
	}{{&n.Next, "next"}, {&n.Open, "question"}, {&n.Decisions, "decision"}} {
		for i := range *s.list {
			if (*s.list)[i].ID == id {
				return s.list, i, s.kind
			}
		}
	}
	return nil, -1, ""
}

// timelineLine writes "- <kind> [<tag>]: <text> <refs> <meta>".
func timelineLine(kind, tag, text string, refs []string, meta string) string {
	parts := append([]string{joinNonEmpty(" ", kind, tagToken(tag)) + ":", text}, refs...)
	return "- " + joinNonEmpty(" ", append(parts, meta)...)
}

// lineTail returns what follows a Timeline line's "<word> [<tag>]: ",
// without its hidden comment: the text and refs that make two lines the
// same. A line in another form has none.
func lineTail(line string) string {
	rest, ok := strings.CutPrefix(line, "- ")
	if !ok {
		return ""
	}
	_, rest, ok = strings.Cut(rest, ":")
	if !ok {
		return ""
	}
	if a := strings.LastIndex(rest, "<!--"); a >= 0 && strings.HasSuffix(strings.TrimSpace(rest), "-->") {
		rest = rest[:a]
	}
	return strings.TrimSpace(rest)
}

// ownTag shows an answer's or replacement's tag when it differs from the
// item's.
func ownTag(tag, itemTag string) string {
	if t := tagToken(tag); t != "" && tag != itemTag {
		return " " + t
	}
	return ""
}

// maxID returns the largest k of an id n<k> anywhere on the page.
func maxID(p Page) int {
	top := 0
	note := func(id string) {
		if len(id) < 2 || len(id) > 10 || id[0] != 'n' || strings.Trim(id[1:], "0123456789") != "" {
			return
		}
		if k, _ := strconv.Atoi(id[1:]); k > top {
			top = k
		}
	}
	n := p.Now
	for _, list := range [][]Item{n.Done, n.Next, n.Open, n.Decisions} {
		for _, it := range list {
			note(it.ID)
		}
	}
	texts := append([]string{n.Summary, n.LastChat}, p.Other...)
	for _, g := range p.Timeline {
		texts = append(texts, g.Lines...)
	}
	for _, s := range texts {
		for _, id := range commentIDs(s) {
			note(id)
		}
	}
	return top
}

// commentIDs returns the ids (i:<id>) in the HTML comments of s.
func commentIDs(s string) []string {
	var ids []string
	for {
		a := strings.Index(s, "<!--")
		if a < 0 {
			return ids
		}
		s = s[a+4:]
		b := strings.Index(s, "-->")
		if b < 0 {
			return ids
		}
		for _, f := range strings.Fields(s[:b]) {
			if id, ok := strings.CutPrefix(f, "i:"); ok {
				ids = append(ids, id)
			}
		}
		s = s[b+3:]
	}
}

func cleanItem(it Item) Item {
	it.Text = cleanLine(it.Text, TextCap)
	it.Refs = cleanRefs(it.Refs)
	it.At = canonical(it.At)
	it.Code = cleanCode(it.Code)
	return it
}

func sameItem(x, y Item) bool {
	_, ox := x.At.Zone()
	_, oy := y.At.Zone()
	return x.ID == y.ID && x.Text == y.Text && x.Tag == y.Tag && x.Code == y.Code &&
		slices.Equal(x.Refs, y.Refs) && x.At.Equal(y.At) && ox == oy
}

// mergeRefs returns the refs of both lists, each once, in order.
func mergeRefs(a, b []string) []string {
	var out []string
	for _, r := range append(slices.Clone(a), b...) {
		if !slices.Contains(out, r) {
			out = append(out, r)
		}
	}
	return out
}

// remove drops item i, leaving nil rather than an empty list, as Parse does.
func remove(items []Item, i int) []Item {
	items = append(items[:i], items[i+1:]...)
	if len(items) == 0 {
		return nil
	}
	return items
}

func clonePage(p Page) Page {
	c := p
	c.Now.Done = cloneItems(p.Now.Done)
	c.Now.Next = cloneItems(p.Now.Next)
	c.Now.Open = cloneItems(p.Now.Open)
	c.Now.Decisions = cloneItems(p.Now.Decisions)
	c.Timeline = slices.Clone(p.Timeline)
	for i := range c.Timeline {
		c.Timeline[i].Lines = slices.Clone(c.Timeline[i].Lines)
	}
	c.Other = slices.Clone(p.Other)
	return c
}

func cloneItems(items []Item) []Item {
	out := slices.Clone(items)
	for i := range out {
		out[i].Refs = slices.Clone(out[i].Refs)
	}
	return out
}
