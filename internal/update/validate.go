package update

import (
	"cmp"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/heyNag/eva-md/internal/inbox"
	"github.com/heyNag/eva-md/internal/redact"
	"github.com/heyNag/eva-md/internal/threadpage"
)

// Report counts what Validate changed or found, for the run log. It holds
// counts only, never entry or output text.
type Report struct {
	BadRefs      int        `json:"bad_refs"`     // refs outside the batch, or past a line's fifth, removed
	Downgraded   int        `json:"downgraded"`   // tags the citations didn't support, changed
	Unattributed int        `json:"unattributed"` // of those, stated or accepted claims that cite no user prompt, made inferred
	Pasted       int        `json:"pasted"`       // of those, stated or accepted claims whose user text is pasted, made inferred
	Proposals    int        `json:"proposals"`    // decisions without the user's tag, filed as questions
	Dates        int        `json:"dates"`        // lines left out for a date no cited entry holds
	Carried      int        `json:"carried"`      // the page's ids the output gave no action, kept as they were
	Ignored      int        `json:"ignored"`      // operations on ids not on the page, or second ones on an id
	Refused      int        `json:"refused"`      // operations a rule refused; their items stay as they were
	Stale        int        `json:"stale"`        // supersessions without a user prompt as new as the item, refused
	Restatements int        `json:"restatements"` // supersessions that restate the item, refused
	Replace      int        `json:"replace"`      // supersessions turned into an add and a "does X replace Y?" line
	Duplicates   int        `json:"duplicates"`   // adds and Timeline lines already on the page, left out
	Misses       Misses     `json:"misses"`       // code-like tokens nothing in the batch holds
	Unknowns     int        `json:"unknowns"`     // what the model listed as unclear
	KeptNow      bool       `json:"kept_now"`     // the out-of-order guard kept the Now
	Rules        RuleCounts `json:"rules"`        // what became of the output's rule changes
}

// Misses counts, by class, the code-like tokens and the numbers with units
// of the text an update writes that no entry of its batch, item on its page
// or commit subject holds.
type Misses struct {
	Paths       int `json:"paths"`
	URLs        int `json:"urls"`
	SHAs        int `json:"shas"`
	Identifiers int `json:"identifiers"`
	Numbers     int `json:"numbers"`
}

// RuleCounts counts what Validate did with the output's rule changes.
type RuleCounts struct {
	Added      int `json:"added"`
	Edited     int `json:"edited"`
	Dropped    int `json:"dropped"`
	General    int `json:"general"`    // of the adds, rules for all projects
	Refused    int `json:"refused"`    // changes not on the user's own stated words, or to an all-projects rule without them
	Stale      int `json:"stale"`      // edits and drops citing no user prompt as new as the rule
	Duplicates int `json:"duplicates"` // adds and edits whose text the page holds
	Ignored    int `json:"ignored"`    // ids not on file, second changes to an id, and every change while the out-of-order guard holds
}

const (
	maxDateDrops = 3   // lines an output may lose to uncited dates before it is rejected
	doneCap      = 200 // one Done line
	lastChatCap  = 300 // the model's last chat; the job adds the date and host/model within threadpage.LastChatCap
	replaceShare = 130 // each text of a "does X replace Y?" line, so the line fits threadpage.TextCap
	overlapShare = 0.6 // the share of an assistant's add an existing item may hold before it is a duplicate
)

// replaceCode is the code of the line eva-md writes when a supersession's
// replacement, X, shares no content token with the item, Y: does X replace Y?
const replaceCode = "replace question"

// Validate checks the model's output against the batch it answered and the
// page the batch was built from, and returns the update for threadpage.Apply.
// It is pure and deterministic. It rejects, with an error wrapping
// ErrRejected, output that doesn't parse strictly against the schema for the
// page (see Schema), that loses more than 3 lines to dates no cited entry
// holds, or that gives more than half the page's ids no action when it may
// change the Now. Anything else it mends line by line, counting each change
// in the Report; docs/design.md "Validation" lists the rules. Every operation
// names an id on the page, once; only stated and accepted items are
// decisions; every string is redacted again and capped, and Apply's renderer
// escapes its structure.
func Validate(data []byte, b Batch, page threadpage.Page) (threadpage.Update, Report, error) {
	v := newValidator(b, page)
	out, err := parseOutput(data, len(v.order)+maxAdds)
	if err != nil {
		return threadpage.Update{}, Report{}, err
	}
	// The out-of-order guard: a session older than the page appends its
	// Timeline lines and leaves the Now alone.
	v.u.KeepNow = !b.AsOf.IsZero() && !b.Last.IsZero() && b.Last.Before(b.AsOf)
	v.report.KeptNow = v.u.KeepNow
	v.items(out.Items)
	v.events(out.Timeline)
	v.done(out.Done)
	v.rules(out.Rules)
	v.u.Summary = v.free(scrubText(out.Summary, threadpage.SummaryCap))
	v.u.LastChat = v.free(scrub(out.LastChat, lastChatCap))
	v.report.Unknowns = len(out.Unknowns)
	if err := v.verdict(); err != nil {
		return threadpage.Update{}, v.report, err
	}
	v.u.Date = v.date()
	return v.u, v.report, nil
}

// validator holds one Validate's state.
type validator struct {
	b        Batch
	page     threadpage.Page
	u        threadpage.Update
	report   Report
	held     map[string]held // the page's Now items by id
	order    []string        // their ids, in page order
	acted    map[string]bool // ids that had an operation, kept or refused
	retired  map[string]bool // ids done, answered or dropped this pass
	accepted map[string]bool // normalized texts of stored proposals this output accepts
	onPage   map[string]bool // normalized texts on the page that stay there
	newItems map[string]bool // normalized texts this pass puts in the Now
	newLines map[string]bool // normalized texts of this pass's Timeline lines
	asks     []ask           // supersessions turned into a replace question
	at       time.Time       // the newest entry a kept operation or Timeline line cites
	linesAt  time.Time       // the newest entry a kept Timeline line cites
	newest   time.Time       // the batch's newest entry that isn't an event
	hay      string          // what grounds a code-like token, lowercased
	rulesOn  map[string]heldRule
	ruleKeys map[bool]map[string]bool // normalized rule texts by page, general or not, for duplicates
	ruleN    map[bool]int             // rules by page, for the cap
}

// heldRule is a standing rule on file, with its page.
type heldRule struct {
	item    threadpage.Item
	general bool
}

// held is a Now item on the page with its kind; a decision kept past the
// cap under Open questions is a decision.
type held struct {
	item threadpage.Item
	kind string
}

// heldItems returns the page's Now items that have an id, each id once, in
// page order, with their kinds: the items an output gives an action.
func heldItems(page threadpage.Page) []held {
	var out []held
	seen := map[string]bool{}
	n := page.Now
	for _, s := range []struct {
		list []threadpage.Item
		kind string
	}{{n.Next, "next"}, {n.Open, "question"}, {n.Decisions, "decision"}} {
		for _, it := range s.list {
			if it.ID == "" || seen[it.ID] {
				continue
			}
			seen[it.ID] = true
			kind := s.kind
			if it.Code == threadpage.OverCapCode {
				kind = "decision"
			}
			out = append(out, held{it, kind})
		}
	}
	return out
}

func newValidator(b Batch, page threadpage.Page) *validator {
	v := &validator{b: b, page: page, held: map[string]held{}, acted: map[string]bool{}, retired: map[string]bool{},
		accepted: map[string]bool{}, onPage: map[string]bool{}, newItems: map[string]bool{}, newLines: map[string]bool{}}
	for _, h := range heldItems(page) {
		v.held[h.item.ID] = h
		v.order = append(v.order, h.item.ID)
	}
	hay := slices.Clone(b.Commits)
	for _, e := range b.Entries {
		hay = append(hay, e.Text)
		if e.Role != "event" {
			v.newest = later(v.newest, e.At)
		}
	}
	for _, it := range slices.Concat(page.Now.Next, page.Now.Open, page.Now.Decisions) {
		hay = append(hay, it.Text)
	}
	v.hay = strings.ToLower(strings.Join(hay, "\n"))
	v.rulesOn, v.ruleKeys, v.ruleN = map[string]heldRule{}, map[bool]map[string]bool{true: {}, false: {}}, map[bool]int{}
	for _, general := range []bool{true, false} {
		p := b.Rules.page(general)
		v.ruleN[general] = len(p.Rules)
		for _, it := range p.Rules {
			v.ruleKeys[general][normalize(it.Text)] = true
		}
		for _, it := range threadpage.HeldRules(p, general) {
			v.rulesOn[it.ID] = heldRule{it, general}
		}
	}
	return v
}

// cites is what one line's refs point at.
type cites struct {
	nums          []int     // the entry numbers in the batch, each once
	refs          []string  // their refs
	users         []int     // the cited user prompts with words of the user's own; an event, or a prompt all pasted, never counts
	owns          []string  // their own text, one for each
	own, pasted   string    // the cited user prompts' own text and pasted text
	newestUser    time.Time // the newest of the users
	newest        time.Time // the newest cited entry
	assistant     bool      // an assistant entry is cited
	result        bool      // a tool entry, or an assistant's report of a result, is cited
	onlyAssistant bool      // entries are cited, and all of them are the assistant's
}

// cite resolves a line's refs to entries of the batch, keeping the first 5
// distinct ones, which the tags are checked against and the line shows. A
// ref outside the batch, or past the fifth, is left out and counted.
func (v *validator) cite(nums []int) cites {
	c := cites{onlyAssistant: true}
	for _, n := range nums {
		if n < 1 || n > len(v.b.Entries) {
			v.report.BadRefs++
			continue
		}
		if slices.Contains(c.nums, n) {
			continue
		}
		if len(c.nums) == maxRefs {
			v.report.BadRefs++
			continue
		}
		e := v.b.Entries[n-1]
		c.nums = append(c.nums, n)
		c.refs = append(c.refs, v.ref(e))
		c.newest = later(c.newest, e.At)
		switch {
		case e.Role == "user" && e.Kind == "prompt":
			own, pasted := e.Text, ""
			if pastes(v.b.Host) {
				own, pasted = ownAndPasted(e.Text)
			}
			c.own, c.pasted = c.own+own+"\n", c.pasted+pasted+"\n"
			if strings.TrimSpace(own) != "" {
				c.users = append(c.users, n)
				c.owns = append(c.owns, own)
				c.newestUser = later(c.newestUser, e.At)
			}
		case e.Role == "assistant":
			c.assistant = true
			c.result = c.result || e.report
		case e.Role == "tool":
			c.result = true
		}
		c.onlyAssistant = c.onlyAssistant && e.Role == "assistant"
	}
	c.onlyAssistant = c.onlyAssistant && len(c.nums) > 0
	return c
}

// ref is an entry's source ref, s:<id8>#<line>, or with the full session id
// when another session file shares its first 8 characters, as eva-md search
// writes refs.
func (v *validator) ref(e Numbered) string {
	if v.b.FullRef {
		return "s:" + e.Session + "#" + strconv.Itoa(e.Line)
	}
	return inbox.Ref(e.Session, e.Line)
}

// tag returns the tag a line's citations support: want when they support it,
// otherwise inferred, counting the change, and apart the claims of the
// user's words that cite no user prompt. stated needs a cited user prompt.
// accepted needs one and an earlier proposal (see proposed); a claim of
// accepted that has its user prompt but no proposal is stated, so the user's
// words never go under the assistant's label (S1), except when it accepts a
// stored proposal its prompt doesn't come after. suggested needs an
// assistant entry, and observed a tool entry or an assistant's report of a
// result. on is the stored item an edit, answer or supersession acts on.
func (v *validator) tag(want string, c cites, on *threadpage.Item, action, text string) string {
	got := "inferred"
	// A claim of the user's words that rests on text the user pasted is not
	// the user's own decision. Nor is one that shares no word with what the
	// user said, or with the proposal accepted: citing a bare "go ahead"
	// can't make an assistant's text, or text injected into it, the user's.
	pasted := userTag(want) && len(c.users) > 0 && restsOnPaste(c, text)
	rests := restsOn(text, c.own)
	switch want {
	case "stated":
		if len(c.users) > 0 && !pasted && rests {
			got = "stated"
		}
	case "accepted":
		proposal := v.restsOnProposal(c, on, text)
		switch {
		case len(c.users) == 0 || pasted:
		case v.proposed(c, on, action, text) && proposal:
			got = "accepted"
		case !accepting(on, action) && rests:
			got = "stated"
		}
		rests = proposal
	case "suggested":
		if c.assistant {
			got = "suggested"
		}
	case "observed":
		if c.result {
			got = "observed"
		}
	}
	if got != want {
		v.report.Downgraded++
	}
	switch {
	case !userTag(want):
	case pasted || len(c.users) == 0 && strings.TrimSpace(c.pasted) != "":
		v.report.Pasted++
	case len(c.users) == 0, got == "inferred" && !rests:
		v.report.Unattributed++
	}
	return got
}

// restsOnPaste reports whether text rests on what the user pasted: one of
// its content tokens is in the pasted text of its cited prompts and not in
// their own words.
func restsOnPaste(c cites, text string) bool {
	if strings.TrimSpace(c.pasted) == "" {
		return false
	}
	mine, theirs := contentTokens(c.own), contentTokens(c.pasted)
	for t := range contentTokens(text) {
		if theirs[t] && !mine[t] {
			return true
		}
	}
	return false
}

// proposed reports whether an accepted claim has its earlier proposal: a
// cited assistant entry before its newest cited user prompt; for an edit or
// answer on a stored proposal, that proposal, when a cited user prompt is at
// or after its time (change 4); or, for an add or Timeline line, a stored
// proposal with its text that this output accepts so.
func (v *validator) proposed(c cites, on *threadpage.Item, action, text string) bool {
	for _, n := range c.nums {
		if n < slices.Max(c.users) && v.b.Entries[n-1].Role == "assistant" {
			return true
		}
	}
	switch {
	case accepting(on, action):
		for _, u := range c.users {
			if !v.b.Entries[u-1].At.IsZero() && !v.b.Entries[u-1].At.Before(on.At) {
				return true
			}
		}
		return false
	case on == nil:
		return v.accepted[normalize(text)]
	}
	return false
}

// restsOn reports whether text shares a word with one of said, in some
// form (see sameWord), as "Postgres" and "PostgreSQL" do.
func restsOn(text string, said ...string) bool {
	claim := contentTokens(text)
	for _, s := range said {
		for w := range contentTokens(s) {
			for k := range claim {
				if sameWord(k, w) {
					return true
				}
			}
		}
	}
	return false
}

// restsOnProposal reports whether an accepted claim rests on what was
// accepted: the cited assistant entries before the user's answer, the
// stored proposal, a proposal this output accepted, or the user's words.
func (v *validator) restsOnProposal(c cites, on *threadpage.Item, text string) bool {
	if on == nil && v.accepted[normalize(text)] {
		return true
	}
	said := []string{c.own}
	if on != nil {
		said = append(said, on.Text)
	}
	for _, n := range c.nums {
		if e := v.b.Entries[n-1]; len(c.users) > 0 && n < slices.Max(c.users) && e.Role == "assistant" {
			said = append(said, e.Text)
		}
	}
	return restsOn(text, said...)
}

// accepting reports an edit or answer on a stored proposal, an item tagged
// suggested or inferred, which may carry accepted (change 4).
func accepting(on *threadpage.Item, action string) bool {
	return on != nil && (action == "edit" || action == "answered") && assistantTag(on.Tag)
}

// accept records a stored proposal this output accepts, so an add of its
// text, or of the answer, may carry accepted too.
func (v *validator) accept(texts ...string) {
	for _, t := range texts {
		if t = normalize(t); t != "" {
			v.accepted[t] = true
		}
	}
}

func userTag(tag string) bool      { return tag == "stated" || tag == "accepted" }
func assistantTag(tag string) bool { return tag == "suggested" || tag == "inferred" }

// owned reports whether an item on the page is the user's: tagged so, or
// written by hand, as an item without a tag is, since the update tags every
// item it writes.
func owned(tag string) bool { return userTag(tag) || tag == "" }

// file returns the kind a line is filed under: a decision without the
// user's tag is a question, a proposal.
func (v *validator) file(kind, tag string) string {
	if kind == "decision" && !userTag(tag) {
		v.report.Proposals++
		return "question"
	}
	return kind
}

// items checks the operations as Apply runs them: those on existing items
// first, in order, then the adds.
func (v *validator) items(ops []itemOp) {
	var adds []itemOp
	for _, op := range ops {
		if op.Action == "add" {
			adds = append(adds, op)
		} else {
			v.act(op)
		}
	}
	v.learn()
	for _, a := range v.asks {
		v.place(a)
	}
	for _, op := range adds {
		v.add(op)
	}
}

// act checks one operation on an existing item. Only the first for an id
// counts, and only for an id on the page. A replace question is eva-md's own
// line, whose text its exit (S2) reads, so it is only kept, answered or
// dropped.
func (v *validator) act(op itemOp) {
	id := ""
	if op.ID != nil {
		id = strings.TrimSpace(*op.ID)
	}
	h, ok := v.held[id]
	if !ok || v.acted[id] {
		v.report.Ignored++
		return
	}
	v.acted[id] = true
	c := v.cite(op.Refs)
	if h.item.Code == replaceCode && op.Action != "keep" && op.Action != "answered" && op.Action != "dropped" {
		v.report.Refused++
		return
	}
	switch op.Action {
	case "edit":
		v.edit(id, h, op, c)
	case "done":
		v.finish(id, h, c)
	case "answered":
		v.answer(id, h, op, c)
	case "superseded":
		v.supersede(id, h, op, c)
	case "dropped":
		v.drop(id, h, op, c)
	}
}

// edit checks an edit. One that changes neither the text nor the tag adds
// its refs and, when it cites a user prompt newer than the item's time,
// refreshes that time; no edit moves it back (S4). A new text or tag must be
// one the citations support, a decision or an item the user owns takes no
// edit that leaves it without the user's tag, and a line eva-md wrote keeps
// its stored tag (S1).
func (v *validator) edit(id string, h held, op itemOp, c cites) {
	old := h.item
	text := scrub(deref(op.Text), threadpage.TextCap)
	if normalize(text) == normalize(old.Text) {
		text = ""
	}
	at := c.newestUser
	if !at.After(old.At) {
		at = time.Time{}
	}
	if text == "" && op.Tag == old.Tag {
		if len(c.refs) > 0 {
			v.keep(threadpage.Op{ID: id, Kind: h.kind, Action: "edit", Refs: mergeRefs(old.Refs, c.refs), At: at}, c)
		}
		return
	}
	if !v.dated(text, c.nums) {
		v.report.Dates++
		return
	}
	// The tag stands on the text the item keeps.
	tag := v.tag(op.Tag, c, &old, "edit", cmp.Or(text, old.Text))
	if (h.kind == "decision" || owned(old.Tag)) && !userTag(tag) {
		v.report.Refused++
		return
	}
	if tag == "accepted" && accepting(&old, "edit") {
		v.accept(old.Text, text)
	}
	o := threadpage.Op{ID: id, Kind: h.kind, Action: "edit", Text: text, Refs: mergeRefs(old.Refs, c.refs), At: at}
	if tag != old.Tag && old.Code == "" {
		o.Tag = tag
	}
	if text != "" {
		v.newItems[normalize(text)] = true
	}
	v.keep(o, c)
	v.ground(text)
}

// finish checks done, which needs a tool entry or an assistant's report of
// a result among its citations. A decision is not a task: it leaves the Now
// only superseded or dropped, through the user.
func (v *validator) finish(id string, h held, c cites) {
	if !c.result || h.kind == "decision" {
		v.report.Refused++
		return
	}
	v.retire(id, threadpage.Op{ID: id, Kind: h.kind, Action: "done", Refs: c.refs}, c)
}

// answer checks answered, which needs an answer and a cited user prompt, and
// an item that isn't a decision. The answer carries its own tag, which Apply
// shows beside it.
func (v *validator) answer(id string, h held, op itemOp, c cites) {
	answer := scrub(deref(op.Replacement), threadpage.TextCap)
	switch {
	case answer == "" || len(c.users) == 0 || h.kind == "decision":
		v.report.Refused++
		return
	case !v.dated(answer, c.nums):
		v.report.Dates++
		return
	}
	tag := v.tag(op.Tag, c, &h.item, "answered", answer)
	if tag == "accepted" && accepting(&h.item, "answered") {
		v.accept(h.item.Text, answer)
	}
	v.retire(id, threadpage.Op{ID: id, Kind: h.kind, Action: "answered", Replacement: answer, Tag: tag, Refs: c.refs}, c)
	v.ground(answer)
}

// supersede checks a supersession. Its replacement must not restate the
// item, must carry the user's tag when the item is a decision or the user's
// (an assistant's item never supersedes the user's), and must cite a user
// prompt at or after the item's time (change 5, S4). One that shares no
// content token with the item becomes an add and a "does X replace Y?" line,
// unless the page holds that line and this supersession answers it (S2).
func (v *validator) supersede(id string, h held, op itemOp, c cites) {
	old := h.item
	repl := scrub(deref(op.Replacement), threadpage.TextCap)
	switch {
	case repl == "":
		v.report.Refused++
		return
	case normalize(repl) == normalize(old.Text):
		v.report.Restatements++
		return
	case !v.dated(repl, c.nums):
		v.report.Dates++
		return
	}
	tag := v.tag(op.Tag, c, &old, "superseded", repl)
	switch {
	case (h.kind == "decision" || owned(old.Tag)) && !userTag(tag):
		v.report.Refused++
		return
	case !old.At.IsZero() && (c.newestUser.IsZero() || c.newestUser.Before(old.At)):
		v.report.Stale++
		return
	case !shareToken(repl, old.Text) && !v.settled(old, repl, c):
		v.asks = append(v.asks, ask{held: h, text: repl, tag: tag, c: c})
		v.report.Replace++
		return
	}
	v.newItems[normalize(repl)] = true
	v.keep(threadpage.Op{ID: id, Kind: h.kind, Action: "superseded", Replacement: repl, Tag: tag, Refs: c.refs, At: c.newestUser}, c)
	v.ground(repl)
}

// settled reports whether the page holds "does X replace Y?" for y and this
// supersession answers it: its replacement normalizes to X, or it cites a
// user prompt after the line's time (S2).
func (v *validator) settled(y threadpage.Item, repl string, c cites) bool {
	head, tail := `does "`, `" replace "`+cutRunes(y.Text, replaceShare)+`"?`
	for _, id := range v.order {
		q := v.held[id].item
		if q.Code != replaceCode || len(q.Text) < len(head)+len(tail) || !strings.HasPrefix(q.Text, head) || !strings.HasSuffix(q.Text, tail) {
			continue
		}
		x := q.Text[len(head) : len(q.Text)-len(tail)]
		if normalize(x) == normalize(cutRunes(repl, replaceShare)) || c.newestUser.After(q.At) {
			return true
		}
	}
	return false
}

// ask is a supersession turned into an add of its replacement, X, and the
// line eva-md writes, "does X replace Y?". The item, Y, stays as it is.
type ask struct {
	held      held
	text, tag string
	c         cites
}

// place adds a replace question: the replacement, unless the Now holds it
// already, and the line, which keeps the item's own tag (S1) and takes the
// time of the newest user prompt it cites.
func (v *validator) place(a ask) {
	y := a.held.item
	if key := normalize(a.text); !v.onPage[key] && !v.newItems[key] {
		v.newItems[key] = true
		v.keep(threadpage.Op{Kind: v.file(a.held.kind, a.tag), Action: "add", Text: a.text, Tag: a.tag, Refs: a.c.refs, At: a.c.newestUser}, a.c)
		v.ground(a.text)
	}
	at := a.c.newestUser
	for _, t := range []time.Time{a.c.newest, v.newest} {
		if at.IsZero() {
			at = t
		}
	}
	v.keep(threadpage.Op{Kind: "question", Action: "add", Text: replaceText(a.text, y.Text), Tag: y.Tag, Refs: a.c.refs, At: at, Code: replaceCode}, a.c)
}

// replaceText is the text of a replace question.
func replaceText(x, y string) string {
	return `does "` + cutRunes(x, replaceShare) + `" replace "` + cutRunes(y, replaceShare) + `"?`
}

// drop checks dropped, which needs a reason; an item the user owns, or
// wrote by hand, also needs a cited user prompt.
func (v *validator) drop(id string, h held, op itemOp, c cites) {
	reason := scrub(deref(op.Reason), threadpage.TextCap)
	switch {
	case reason == "" || owned(h.item.Tag) && len(c.users) == 0:
		v.report.Refused++
		return
	case !v.dated(reason, c.nums):
		v.report.Dates++
		return
	}
	v.retire(id, threadpage.Op{ID: id, Kind: h.kind, Action: "dropped", Reason: reason, Refs: c.refs}, c)
}

func (v *validator) retire(id string, op threadpage.Op, c cites) {
	v.retired[id] = true
	v.keep(op, c)
}

// keep adds a checked operation to the update.
func (v *validator) keep(op threadpage.Op, c cites) {
	v.u.Ops = append(v.u.Ops, op)
	v.at = later(v.at, c.newest)
}

// learn collects the normalized texts an add or Timeline line may not
// repeat: the page's Now items that stay this pass, and its Timeline.
func (v *validator) learn() {
	for _, it := range v.current() {
		v.onPage[normalize(it.Text)] = true
	}
	for _, g := range v.page.Timeline {
		for _, l := range g.Lines {
			for _, t := range timelineTexts(l) {
				v.onPage[normalize(t)] = true
			}
		}
	}
	delete(v.onPage, "")
}

// current returns the page's Next, Open questions and Decisions in force
// items that aren't retired this pass, in page order.
func (v *validator) current() []threadpage.Item {
	var out []threadpage.Item
	n := v.page.Now
	for _, list := range [][]threadpage.Item{n.Next, n.Open, n.Decisions} {
		for _, it := range list {
			if it.ID == "" || !v.retired[it.ID] {
				out = append(out, it)
			}
		}
	}
	return out
}

// add checks a new item: its dates, its tag and its kind, and, for a
// decision or question, that the page or this output doesn't hold it already
// (change 6, S5). next items are never deduplicated.
func (v *validator) add(op itemOp) {
	text := scrub(deref(op.Text), threadpage.TextCap)
	if text == "" {
		v.report.Refused++
		return
	}
	c := v.cite(op.Refs)
	if !v.dated(text, c.nums) {
		v.report.Dates++
		return
	}
	tag := v.tag(op.Tag, c, nil, "add", text)
	kind := v.file(op.Kind, tag)
	key := normalize(text)
	if kind != "next" && (v.onPage[key] || v.newItems[key] || v.overlaps(text, c)) {
		v.report.Duplicates++
		return
	}
	v.newItems[key] = true
	v.keep(threadpage.Op{Kind: kind, Action: "add", Text: text, Tag: tag, Refs: c.refs, At: c.newestUser}, c)
	v.ground(text)
}

// overlaps reports an add cited only by assistant entries that an existing
// Now item already covers: the item holds about 0.6 of the add's content
// tokens, or every code-like token of the add when it has one (S5).
func (v *validator) overlaps(text string, c cites) bool {
	if !c.onlyAssistant {
		return false
	}
	codes := codeTokens(text)
	for _, it := range v.current() {
		if overlap(text, it.Text) >= overlapShare || len(codes) > 0 && allGrounded(codes, strings.ToLower(it.Text)) {
			return true
		}
	}
	return false
}

func allGrounded(tokens []codeToken, hay string) bool {
	for _, t := range tokens {
		if !grounded(t, hay) {
			return false
		}
	}
	return true
}

// events checks the Timeline lines: dates, tag and kind as for an add. A
// decision, finding or question line that repeats one on the page or in this
// output is left out (change 6); done, change and problem lines recur.
func (v *validator) events(lines []eventLine) {
	for _, l := range lines {
		text := scrub(l.Text, threadpage.TextCap)
		c := v.cite(l.Refs)
		if text == "" {
			v.report.Refused++
			continue
		}
		if !v.dated(text, c.nums) {
			v.report.Dates++
			continue
		}
		tag := v.tag(l.Tag, c, nil, "event", text)
		kind := v.file(l.Kind, tag)
		key := normalize(text)
		if (kind == "decision" || kind == "finding" || kind == "question") && (v.onPage[key] || v.newLines[key]) {
			v.report.Duplicates++
			continue
		}
		v.newLines[key] = true
		v.u.Events = append(v.u.Events, threadpage.Event{Kind: kind, Text: text, Tag: tag, Refs: c.refs})
		v.at, v.linesAt = later(v.at, c.newest), later(v.linesAt, c.newest)
		v.ground(text)
	}
}

// done checks the Done lines, each with the tag its citations give it.
func (v *validator) done(lines []doneLine) {
	for _, l := range lines {
		text := scrub(l.Text, doneCap)
		c := v.cite(l.Refs)
		switch {
		case text == "":
			v.report.Refused++
		case !v.dated(text, c.nums):
			v.report.Dates++
		default:
			v.u.Done = append(v.u.Done, threadpage.Item{Text: text, Tag: doneTag(c, text), Refs: c.refs})
			v.ground(text)
		}
	}
}

// doneTag is a Done line's tag: observed for a tool entry or an assistant's
// report of a result, stated for a user prompt whose own words it rests on,
// inferred otherwise. Every Done line carries one.
func doneTag(c cites, text string) string {
	switch {
	case c.result:
		return "observed"
	case len(c.users) > 0 && !restsOnPaste(c, text):
		return "stated"
	}
	return "inferred"
}

// free checks the summary or the last chat, which cite nothing: a date in it
// must appear in some entry of the batch, or it is left out and the page
// keeps its own.
func (v *validator) free(s string) string {
	all := make([]int, len(v.b.Entries))
	for i := range all {
		all[i] = i + 1
	}
	if !v.dated(s, all) {
		v.report.Dates++
		return ""
	}
	v.ground(s)
	return s
}

// dated reports whether every date written in s appears in the text of one
// of the entries numbered nums. A date in another form counts when it names
// the same day. An entry's own date, in its header, is not in its text: an
// event said "yesterday" didn't happen on it.
func (v *validator) dated(s string, nums []int) bool {
	want := dates(s)
	if len(want) == 0 {
		return true
	}
	var have []date
	for _, n := range nums {
		have = append(have, entryDates(v.b.Entries[n-1].Text)...)
	}
	for _, d := range want {
		if !slices.ContainsFunc(have, d.same) {
			return false
		}
	}
	return true
}

// ground counts the code-like tokens and the numbers with units of s that
// nothing in the batch, on the page or in the commit subjects holds (change
// 1, S8). It never changes a tag or drops a line.
func (v *validator) ground(s string) {
	for _, n := range numberTokens(s) {
		if !numberGrounded(n, v.hay) {
			v.report.Misses.Numbers++
		}
	}
	for _, t := range codeTokens(s) {
		if grounded(t, v.hay) {
			continue
		}
		switch t.class {
		case classPath:
			v.report.Misses.Paths++
		case classURL:
			v.report.Misses.URLs++
		case classSHA:
			v.report.Misses.SHAs++
		default:
			v.report.Misses.Identifiers++
		}
	}
}

// verdict counts the page's ids the output gave no action, which stay as
// they were, and rejects the output past the limits. When the out-of-order
// guard keeps the Now, the ids need no action.
func (v *validator) verdict() error {
	for _, id := range v.order {
		if !v.acted[id] {
			v.report.Carried++
		}
	}
	switch {
	case v.report.Dates > maxDateDrops:
		return reject("%d lines wrote a date no cited entry holds", v.report.Dates)
	case !v.u.KeepNow && 2*v.report.Carried > len(v.order):
		return reject("%d of the page's %d item ids have no action", v.report.Carried, len(v.order))
	}
	return nil
}

// date is the Timeline date: the local date of the newest entry the update
// cites (its Timeline lines only, when the Now is kept), or of the batch's
// newest entry.
func (v *validator) date() string {
	at := v.at
	if v.u.KeepNow {
		at = v.linesAt
	}
	if at.IsZero() {
		at = v.newest
	}
	if at.IsZero() {
		return ""
	}
	return at.Format(time.DateOnly)
}

var (
	// struck is struck-through text: a superseded or dropped item's old text.
	struck = regexp.MustCompile(`~~.*?~~`)
	// note is the note at the end of a retired Timeline line.
	note = regexp.MustCompile(`\s*\((?:superseded|dropped|moved)[^)]*\)$`)
	// codeLabel starts the text of a line eva-md wrote.
	codeLabel = regexp.MustCompile(`^eva-md: [^:]*: `)
	// tagMark ends an answer or replacement that shows its own tag.
	tagMark = regexp.MustCompile(`\s*\[(?:stated|accepted|suggested|observed|inferred)\]$`)
	// refField is a source ref.
	refField = regexp.MustCompile(`^s:\S+#[0-9]+$`)
)

// timelineTexts returns the texts a Timeline line holds in force, to compare
// a new line with: what follows its "<word> [<tag>]: ", without the refs,
// hidden comment, note, eva-md's label or an answer's tag, with each side of
// an arrow on its own. Struck-through text is left out, so a return to a
// superseded or dropped value is not a duplicate.
func timelineTexts(line string) []string {
	rest := hidden.ReplaceAllString(strings.TrimPrefix(strings.TrimSpace(line), "- "), "")
	if _, after, ok := strings.Cut(rest, ": "); ok {
		rest = after
	}
	fields := strings.Fields(rest)
	for len(fields) > 0 && refField.MatchString(fields[len(fields)-1]) {
		fields = fields[:len(fields)-1]
	}
	rest = note.ReplaceAllString(struck.ReplaceAllString(strings.Join(fields, " "), ""), "")
	var out []string
	for _, part := range strings.Split(rest, " → ") {
		out = append(out, strings.TrimSpace(tagMark.ReplaceAllString(codeLabel.ReplaceAllString(strings.TrimSpace(part), ""), "")))
	}
	return out
}

// mergeRefs returns an edited item's refs: its own, then the edit's, each
// once, keeping the first and the newest when there are more than maxRefs.
func mergeRefs(old, add []string) []string {
	var out []string
	for _, r := range append(slices.Clone(old), add...) {
		if !slices.Contains(out, r) {
			out = append(out, r)
		}
	}
	if len(out) > maxRefs {
		out = append(out[:1], out[len(out)-maxRefs+1:]...)
	}
	return out
}

// scrub redacts a line of output again, collapses it to one line and keeps
// at most n characters of it; Apply's renderer escapes its structure.
func scrub(s string, n int) string {
	s, _ = redact.Redact(s)
	return cutRunes(strings.Join(strings.Fields(s), " "), n)
}

// scrubText is scrub for the summary, which keeps its line breaks.
func scrubText(s string, n int) string {
	s, _ = redact.Redact(s)
	return cutRunes(strings.TrimSpace(s), n)
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func later(a, b time.Time) time.Time {
	if b.After(a) {
		return b
	}
	return a
}
