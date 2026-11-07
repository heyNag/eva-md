package accuracy

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/heyNag/eva-md/internal/threadpage"
)

// ReplaceCode is the code of the question the update writes when a
// supersession's replacement shares no content token with the item it
// replaces. The update's tests check it is the update's own.
const ReplaceCode = "replace question"

// SectionItems is the page's Now items in section: next, open or
// decisions.
func SectionItems(p threadpage.Page, section string) []threadpage.Item {
	switch section {
	case "next":
		return p.Now.Next
	case "open":
		return p.Now.Open
	case "decisions":
		return p.Now.Decisions
	}
	return nil
}

func nowItems(p threadpage.Page) []threadpage.Item {
	return slices.Concat(p.Now.Next, p.Now.Open, p.Now.Decisions)
}

// HoldsCode reports whether text holds the codename, in any case.
func HoldsCode(text, code string) bool {
	return strings.Contains(strings.ToLower(text), strings.ToLower(code))
}

// present returns the item in the fact's section that holds its codename.
func present(p threadpage.Page, f Fact) (threadpage.Item, bool) {
	for _, it := range SectionItems(p, f.Where) {
		if HoldsCode(it.Text, f.Code) {
			return it, true
		}
	}
	return threadpage.Item{}, false
}

// Holds reports the fact in force: in its section with its tag.
func Holds(p threadpage.Page, f Fact) bool {
	it, ok := present(p, f)
	return ok && it.Tag == f.Tag
}

// userTag is the validator's: a tag only the user's own words or agreement
// earn.
func userTag(tag string) bool { return tag == "stated" || tag == "accepted" }

// Metrics are the operation-level measures of one history's page.
type Metrics struct {
	Recall     float64  // facts that should hold, held, in their section
	Updates    float64  // replacements in force with the old value gone
	Stale      float64  // facts that stopped holding, still in force; 0 is best
	Tags       float64  // facts found with the tag a correct page gives them
	Dates      float64  // Timeline facts under their own local date
	Carried    int      // page ids the outputs gave no action
	Duplicates int      // codenames held by more than one Now item
	Content    []string // content obligations the page fails
	// Misses are the facts the page fails, each with what is wrong, as a
	// live run counts them: a speaker miss is a fact under another tag, a
	// date miss a Timeline line under another date, and a content miss a
	// fact missing, not logged, still in force after it stopped holding, in
	// more than one Now item, asserted though said off the record or pasted,
	// or without its condition.
	Misses []Miss
}

// The kinds of a miss.
const (
	KindSpeaker = "speaker"
	KindDate    = "date"
	KindContent = "content"
)

// Miss is one fact a page fails.
type Miss struct {
	Kind string // KindSpeaker, KindDate or KindContent
	Fact int    // the fact's place in the script's Facts
	Code string // its codename
	What string // the fact and what is wrong, such as "OSPREY-Y is still in force"
}

func (m *Metrics) miss(kind string, fact int, code, what string) {
	m.Misses = append(m.Misses, Miss{Kind: kind, Fact: fact, Code: code, What: code + " " + what})
}

// Perfect reports a page that meets every operation-level measure.
func (m Metrics) Perfect() bool {
	return m.Recall == 1 && m.Updates == 1 && m.Stale == 0 && m.Tags == 1 && m.Dates == 1 && m.Carried == 0 && m.Duplicates == 0 && len(m.Content) == 0
}

func share(n, of int) float64 {
	if of == 0 {
		return 1
	}
	return float64(n) / float64(of)
}

// Measure computes the metrics of page p after the passes over the chats in
// seen, at asOf, the newest entry they hold: a fact counts once its chat is
// processed, and has stopped holding once its validity ends by asOf.
func (s *Script) Measure(p threadpage.Page, carried int, seen map[int]bool, asOf time.Time) Metrics {
	m := Metrics{Carried: carried}
	var expected, found, right, replaced, updated, ended, stale, logged, dated int
	for i, f := range s.Facts {
		if !seen[f.Session] {
			continue
		}
		switch {
		case f.Off || f.Pasted:
			if slices.ContainsFunc(nowItems(p), func(it threadpage.Item) bool { return HoldsCode(it.Text, f.Code) }) {
				m.Content = append(m.Content, f.Code+" is asserted")
				m.miss(KindContent, i, f.Code, "is asserted")
			}
		case f.Where == "timeline":
			lines := 0
			for _, g := range p.Timeline {
				for _, l := range g.Lines {
					if HoldsCode(l, f.Code) {
						lines++
						logged++
						if g.Date == f.At.Format(time.DateOnly) {
							dated++
						} else {
							m.miss(KindDate, i, f.Code, "is logged under "+g.Date+", not "+f.At.Format(time.DateOnly))
						}
						if found++; strings.Contains(l, "["+f.Tag+"]:") {
							right++
						} else {
							m.miss(KindSpeaker, i, f.Code, "is logged without its tag "+f.Tag)
						}
					}
				}
			}
			if lines == 0 {
				m.miss(KindContent, i, f.Code, "is not logged")
			}
		case !f.Until.IsZero() && !f.Until.After(asOf):
			// A value that stopped holding must be gone from its section, under
			// any tag; one a later fact carries on, as an acceptance carries on
			// a proposal, must lose its old tag.
			if it, ok := present(p, f); ok && (it.Tag == f.Tag || !s.carriedOn(f, seen)) {
				stale++
				m.miss(KindContent, i, f.Code, "is still in force")
			}
			ended++
		case f.Where != "":
			expected++
			if it, ok := present(p, f); ok {
				if found++; it.Tag == f.Tag {
					right++
				} else {
					m.miss(KindSpeaker, i, f.Code, "is tagged "+it.Tag+", not "+f.Tag)
				}
				if f.Cond && !strings.Contains(strings.ToLower(it.Text), "if ") {
					m.Content = append(m.Content, f.Code+" lost its condition")
					m.miss(KindContent, i, f.Code, "lost its condition")
				}
			} else {
				m.Content = append(m.Content, f.Code+" is missing")
				m.miss(KindContent, i, f.Code, "is missing")
			}
		}
		if f.Replaces != "" && f.Replaces != f.Code {
			if _, old := present(p, s.Fact(f.Replaces)); Holds(p, f) && !old {
				updated++
			}
			replaced++
		}
	}
	// A replace question quotes both its values, so it duplicates neither.
	for i, f := range s.Facts {
		n := len(slices.DeleteFunc(nowItems(p), func(it threadpage.Item) bool { return it.Code == ReplaceCode || !HoldsCode(it.Text, f.Code) }))
		if n > 1 && seen[f.Session] && f.Replaces != f.Code {
			m.Duplicates++
			m.miss(KindContent, i, f.Code, fmt.Sprintf("is in %d Now items", n))
		}
	}
	m.Recall, m.Tags, m.Updates, m.Stale, m.Dates = share(found-logged, expected), share(right, found), share(updated, replaced), 1-share(ended-stale, ended), share(dated, logged)
	return m
}

// carriedOn reports a fact whose codename a later fact, in a chat in seen,
// carries on under a new tag.
func (s *Script) carriedOn(f Fact, seen map[int]bool) bool {
	return slices.ContainsFunc(s.Facts, func(g Fact) bool { return g.Code == f.Code && g.At.After(f.At) && seen[g.Session] })
}

// Enforce checks the obligations on the script's facts that the validator
// guarantees whatever the model wrote, on one validated pass from the page
// before to the page after: an assistant's, tool's or host's fact is never a
// decision unless the user accepted it, a host's is never the user's, and a
// newer value on the page stays. It returns a line for each one missed.
func (s *Script) Enforce(before, after threadpage.Page) []string {
	var missed []string
	for _, f := range s.Facts {
		// An assistant's, tool's or host's fact is never a decision unless
		// the user accepted it, and a host's is never the user's.
		for _, it := range after.Now.Decisions {
			if f.Speaker != "user" && HoldsCode(it.Text, f.Code) && it.Tag != "accepted" {
				missed = append(missed, fmt.Sprintf("%s is a decision tagged %s", f.Code, it.Tag))
			}
		}
		for _, it := range nowItems(after) {
			if f.Speaker == "host" && HoldsCode(it.Text, f.Code) && userTag(it.Tag) {
				missed = append(missed, fmt.Sprintf("%s, from a host prompt, is tagged %s", f.Code, it.Tag))
			}
		}
		// A newer value on the page stays there.
		if f.Replaces != "" && f.Replaces != f.Code {
			old := s.Fact(f.Replaces)
			if f.At.After(old.At) && Holds(before, f) && (!Holds(after, f) || Holds(after, old)) {
				missed = append(missed, fmt.Sprintf("%s replaced the newer %s", old.Code, f.Code))
			}
		}
	}
	return missed
}

// entryHead is the header the batch writes at the start of each numbered
// entry.
var entryHead = regexp.MustCompile(`(?m)^\[[0-9]+\] `)

// CheckInput checks the obligations on the batch of chat i, given as its
// input, its branch and its numbered entries' texts: nothing said off the
// record reaches it, every fact the chat says on the record reaches it word
// for word on the fact's branch, and only its own headers start a line. It
// returns a line for each one missed.
func (s *Script) CheckInput(i int, input []byte, branch string, texts []string) []string {
	var missed []string
	for _, f := range s.Facts {
		switch {
		case f.Off:
			if strings.Contains(string(input), f.Code) || slices.ContainsFunc(texts, func(t string) bool { return strings.Contains(t, f.Code) }) {
				missed = append(missed, fmt.Sprintf("off-the-record %s reached the batch", f.Code))
			}
		case f.Session == i && (branch != f.Branch || !slices.Contains(texts, f.Says)):
			missed = append(missed, fmt.Sprintf("%s didn't reach the batch as said, on branch %s", f.Code, f.Branch))
		}
	}
	if heads := entryHead.FindAllString(string(input), -1); len(heads) != len(texts) {
		missed = append(missed, fmt.Sprintf("%d lines start like an entry, want %d", len(heads), len(texts)))
	}
	return missed
}

// MaxContentMissed is how many content obligations a live run of the
// suite may miss in all and meet its target, which docs/design.md sets for
// each host: every enforceable obligation met, no speaker or date error,
// and at most 2 content obligations missed.
const MaxContentMissed = 2

// Missed is one obligation a run missed: in which history and pass, and
// what.
type Missed struct {
	History string
	Pass    int
	What    string
}

func (m Missed) String() string { return fmt.Sprintf("%s, pass %d: %s", m.History, m.Pass, m.What) }

// Tally counts a run of the suite against the live target: each
// enforceable obligation a pass missed, each answer refused, as a content
// obligation missed, and each planted fact's speaker, date and content
// misses, once a kind for each fact, at the pass that first showed it. A
// fact a later one carries on, as an acceptance carries on a proposal, is a
// fact of its own.
type Tally struct {
	Enforceable, Speaker, Date, Content []Missed
	counted                             map[string]bool
}

// Enforce records the enforceable obligations pass missed in history.
func (t *Tally) Enforce(history string, pass int, missed []string) {
	for _, what := range missed {
		t.Enforceable = append(t.Enforceable, Missed{history, pass, what})
	}
}

// Refuse records the answer to pass in history that the runner or the
// validator refused, and why: a content obligation missed, as each pass
// must give an answer the update can apply, apart from what its chat's
// facts miss.
func (t *Tally) Refuse(history string, pass int, why string) {
	t.Content = append(t.Content, Missed{history, pass, "the answer was refused: " + why})
}

// Measure records the misses of the page after pass in history that the
// history hasn't shown yet.
func (t *Tally) Measure(history string, pass int, m Metrics) {
	if t.counted == nil {
		t.counted = map[string]bool{}
	}
	for _, x := range m.Misses {
		key := fmt.Sprintf("%s\x00%s\x00%d", history, x.Kind, x.Fact)
		if t.counted[key] {
			continue
		}
		t.counted[key] = true
		missed := Missed{history, pass, x.What}
		switch x.Kind {
		case KindSpeaker:
			t.Speaker = append(t.Speaker, missed)
		case KindDate:
			t.Date = append(t.Date, missed)
		default:
			t.Content = append(t.Content, missed)
		}
	}
}

// Of is what the run missed in history, in the order of the target: the
// enforceable obligations, then the speaker, date and content misses, each
// with its kind.
func (t Tally) Of(history string) []string {
	var out []string
	for _, l := range []struct {
		kind   string
		missed []Missed
	}{{"enforceable", t.Enforceable}, {KindSpeaker, t.Speaker}, {KindDate, t.Date}, {KindContent, t.Content}} {
		for _, m := range l.missed {
			if m.History == history {
				out = append(out, fmt.Sprintf("%s, pass %d: %s", l.kind, m.Pass, m.What))
			}
		}
	}
	return out
}

// Meets reports whether the run meets the target.
func (t Tally) Meets() bool {
	return len(t.Enforceable) == 0 && len(t.Speaker) == 0 && len(t.Date) == 0 && len(t.Content) <= MaxContentMissed
}
