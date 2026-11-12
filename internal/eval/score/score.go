// Package score is the fidelity instrument's scorer. It reads what a store
// holds, as an Export of typed items, and scores it against a scenario:
// each planted fact by its values, and each item by what supports it. It
// sees only the export, never the code that wrote it, so v1 and v2 are
// judged by one rule. No release contains it, and no non-test file of it
// imports the code that organizes memory.
//
// An item is tied to facts only through the lines it cites, which
// scenario.Locate turns into the turns behind them and the facts they
// plant; a codename is a value like any other. Of the facts its cited
// turns plant, an item supports those whose every value it holds. A fact
// that must be stored and is still in force holds when an item in a
// permitted kind supports it under the right authority and scope, and as a
// dated line under its day; it is found when an item supports it with
// something else wrong, which the result names. An item that holds only
// some of a fact's values gives the fact and itself the worst of what it
// lacks: a wrong value, then a dropped date, condition, limit or other
// value. Three ties need less than every value, and give the fact's own
// verdict instead: an item that cites a line said off the record breaks
// that fact's Must in any words; one that holds any value of a fact that
// may be kept, only never under the user's tag or never as a rule, breaks
// it when filed so; and an active item that holds a value of a fact that
// ended, which no fact still in force holds, keeps it in force.
package score

import (
	"errors"
	"slices"

	"github.com/heyNag/eva-md/internal/eval/scenario"
	"github.com/heyNag/eva-md/internal/ledger/event"
)

// KindSummary is an item that summarizes, which is not scored here.
const KindSummary scenario.Kind = "summary"

// State is whether an item is in force.
type State string

const (
	StateActive  State = "active"
	StateRetired State = "retired" // done, answered, superseded or dropped
	StateLog     State = "log"     // a dated line
)

// The tags an item may carry. Only stated and accepted are the user's.
const (
	TagStated    = "stated"
	TagAccepted  = "accepted"
	TagObserved  = "observed"
	TagSuggested = "suggested"
	TagExternal  = "external"
	TagEcho      = "echo"
	TagInferred  = "inferred" // v1's
)

var otherTags = []string{TagObserved, TagSuggested, TagExternal, TagEcho, TagInferred}

func userTag(tag string) bool { return tag == TagStated || tag == TagAccepted }

// scopes are the scopes an item may name; empty is a store that has none.
var scopes = []string{"", "here", "project", "all"}

// Source is a line an item cites: its session, in full or by its first 8
// characters, and its seq, which is v1's line.
type Source struct {
	Session string `json:"session"`
	Seq     int64  `json:"seq"`
}

// Item is one record a store holds.
type Item struct {
	Kind      scenario.Kind `json:"kind"`
	State     State         `json:"state"`
	Authority string        `json:"authority"` // the tag
	Scope     string        `json:"scope,omitempty"`
	Text      string        `json:"text"`
	Date      string        `json:"date,omitempty"` // a dated line's day, YYYY-MM-DD
	Sources   []Source      `json:"sources,omitempty"`
}

// Export is what a store holds, as the scorer reads it.
type Export struct {
	Items []Item `json:"items"`
}

// Outcome is what the scorer finds of a fact or an item.
type Outcome string

const (
	// Of a fact only.
	Holds   Outcome = "holds"
	Missing Outcome = "missing"
	// Of a fact, or of the item that caused it.
	Found        Outcome = "found"
	ValueWrong   Outcome = "value_wrong"
	ValueDropped Outcome = "value_dropped"
	DateDropped  Outcome = "date_dropped"
	CondDropped  Outcome = "cond_dropped"
	LimitDropped Outcome = "limit_dropped"
	StaleInForce Outcome = "stale_in_force"
	Violation    Outcome = "violation"
	// Of an item only.
	Supports    Outcome = "supports"
	Noise       Outcome = "noise"       // it cites only lines that plant nothing
	Unsupported Outcome = "unsupported" // it cites no line of the scenario
	Unscored    Outcome = "unscored"    // a summary
)

// slotOutcomes are the outcomes of a value lacking, worst first.
var slotOutcomes = []Outcome{ValueWrong, DateDropped, CondDropped, LimitDropped, ValueDropped}

// Problem is what is wrong with a fact that is found but does not hold.
type Problem string

const (
	ProblemKind      Problem = "kind"      // not in a permitted kind
	ProblemAuthority Problem = "authority" // under another's tag
	ProblemScope     Problem = "scope"     // under no known scope
	ProblemDate      Problem = "date"      // a dated line under another day
	ProblemValidity  Problem = "validity"  // retired while it holds
)

// FactResult is the outcome of one fact.
type FactResult struct {
	Fact     string    `json:"fact"`
	Outcome  Outcome   `json:"outcome"`
	Problems []Problem `json:"problems,omitempty"` // for found
	Items    []int     `json:"items,omitempty"`    // the items tied to it, by place in the export
}

// ItemResult is the outcome of one item.
type ItemResult struct {
	Outcome Outcome  `json:"outcome"`
	Facts   []string `json:"facts,omitempty"` // the facts it is tied to
}

// Result is a store's score on one scenario.
type Result struct {
	Scenario string       `json:"scenario"`
	Facts    []FactResult `json:"facts"` // in the scenario's order
	Items    []ItemResult `json:"items"` // in the export's order
	Metrics  Metrics      `json:"metrics"`
}

// verdict is what one item that supports a fact says of it.
type verdict int

const (
	history   verdict = iota // a record of it, not its place
	holds                    // it stores it rightly
	found                    // it stores it with problems
	stale                    // it keeps it in force after it ended
	violation                // it breaks the fact's Must
)

// support is a fact an item is tied to, and what the item says of it.
type support struct {
	fact     int
	verdict  verdict
	problems []Problem
}

// read is what the scorer makes of one item.
type read struct {
	resolved bool            // it cites a line of the scenario
	turns    []scenario.Turn // the turns behind the lines it cites
	text     text
	supports []support // the facts it holds every value of, and its ties
	partial  int       // when it supports none: the fact it holds most of, or -1
	lack     Outcome   // and the worst of what it lacks
	cands    []int     // the facts its turns plant
	outcome  Outcome
}

// Score scores ex against sc.
func Score(sc scenario.Scenario, ex Export) Result {
	_, _, res := evaluate(sc, ex)
	return res
}

// evaluate is Score, with what it read of each item.
func evaluate(sc scenario.Scenario, ex Export) (*scorer, []read, Result) {
	s := newScorer(sc)
	reads := make([]read, len(ex.Items))
	for i, it := range ex.Items {
		reads[i] = s.read(it)
	}
	for i, it := range ex.Items {
		s.judge(it, &reads[i])
	}
	res := Result{Scenario: sc.ID, Items: make([]ItemResult, len(ex.Items))}
	for i := range reads {
		res.Items[i] = ItemResult{Outcome: reads[i].outcome, Facts: s.tiedFacts(reads[i])}
	}
	for f := range sc.Facts {
		res.Facts = append(res.Facts, s.fact(f, ex, reads))
	}
	res.Metrics = s.metrics(ex, reads, res)
	return s, reads, res
}

type scorer struct {
	sc    scenario.Scenario
	ended []bool
	index map[string]int
}

func newScorer(sc scenario.Scenario) *scorer {
	s := &scorer{sc: sc, ended: make([]bool, len(sc.Facts)), index: map[string]int{}}
	for i, f := range sc.Facts {
		s.index[f.ID] = i
	}
	for i, f := range sc.Facts {
		if f.Until != nil {
			s.ended[i] = true
		}
		if j, ok := s.index[f.Supersedes]; ok && f.Supersedes != "" {
			s.ended[j] = true
		}
	}
	return s
}

// required reports a fact that must be stored and is still in force.
func (s *scorer) required(f int) bool { return s.sc.Facts[f].Must == scenario.MustStore && !s.ended[f] }

// read resolves an item's lines and finds the facts it supports or holds
// part of.
func (s *scorer) read(it Item) read {
	r := read{partial: -1, text: readText(it.Text)}
	if it.Kind == KindSummary {
		r.outcome = Unscored
		return r
	}
	for _, src := range it.Sources {
		t, facts, err := s.sc.Locate(src.Session, src.Seq)
		if errors.Is(err, scenario.ErrNoTurn) {
			r.resolved = true
			continue
		}
		if err != nil {
			continue
		}
		r.resolved = true
		r.turns = append(r.turns, t)
		for _, f := range facts {
			if i := s.index[f.ID]; !slices.Contains(r.cands, i) {
				r.cands = append(r.cands, i)
			}
		}
	}
	best := 0
	for _, f := range r.cands {
		held, lack := s.match(r.text, s.sc.Facts[f])
		switch {
		case lack == "":
			r.supports = append(r.supports, support{fact: f})
		case s.tied(it, r.text, f, held):
			r.supports = append(r.supports, support{fact: f})
		case r.partial < 0 || held > best:
			r.partial, r.lack, best = f, lack, held
		}
	}
	if len(r.supports) > 0 {
		r.partial, r.lack = -1, ""
	}
	return r
}

// tied reports whether an item that cites the line planting fact f, and
// holds held of its values but not all, is still tied to it, so that it
// says of the fact what an item holding all of them would: any item that
// cites a fact said off the record; one that holds a value of a fact that
// may be kept but only so; and an active item that holds a value of a fact
// that ended which no fact still in force holds.
func (s *scorer) tied(it Item, t text, f, held int) bool {
	fact := s.sc.Facts[f]
	switch fact.Must {
	case scenario.MustNotStore:
		return true
	case scenario.MustStore:
		return s.ended[f] && it.State == StateActive && slices.ContainsFunc(fact.Slots, func(sl scenario.Slot) bool {
			return t.holds(sl) && !slices.ContainsFunc(s.sc.Facts, func(g scenario.Fact) bool {
				return s.required(s.index[g.ID]) && hasSlot(g, sl)
			})
		})
	}
	return held > 0
}

// match counts the slots of f the text holds and gives the worst outcome of
// those it lacks, or "" when it lacks none.
func (s *scorer) match(t text, f scenario.Fact) (int, Outcome) {
	held, worst := 0, -1
	for _, sl := range f.Slots {
		if t.holds(sl) {
			held++
			continue
		}
		o := ValueDropped
		switch {
		case t.wrong(sl, f):
			o = ValueWrong
		case sl.Class == scenario.SlotDate:
			o = DateDropped
		case sl.Class == scenario.SlotCond:
			o = CondDropped
		case sl.Class == scenario.SlotLimit:
			o = LimitDropped
		}
		if k := slices.Index(slotOutcomes, o); worst < 0 || k < worst {
			worst = k
		}
	}
	if worst < 0 {
		return held, ""
	}
	return held, slotOutcomes[worst]
}

// judge gives each fact an item supports its verdict, and the item its
// outcome.
func (s *scorer) judge(it Item, r *read) {
	if r.outcome == Unscored {
		return
	}
	for i, sp := range r.supports {
		r.supports[i].verdict, r.supports[i].problems = s.verdict(it, r, sp)
	}
	switch {
	case !r.resolved:
		r.outcome = Unsupported
	case len(r.cands) == 0:
		r.outcome = Noise
	case r.has(violation):
		r.outcome = Violation
	case r.has(stale):
		r.outcome = StaleInForce
	case len(r.supports) == 0:
		r.outcome = r.lack
	case r.has(holds) || r.has(history):
		r.outcome = Supports
	default:
		r.outcome = Found
	}
}

func (r read) has(v verdict) bool {
	return slices.ContainsFunc(r.supports, func(sp support) bool { return sp.verdict == v })
}

// verdict is what item it, which supports or is tied to a fact, says of
// it.
func (s *scorer) verdict(it Item, r *read, sp support) (verdict, []Problem) {
	f := sp.fact
	fact := s.sc.Facts[f]
	switch fact.Must {
	case scenario.MustNotStore:
		return violation, nil
	case scenario.MustNotWithAuthority:
		if userTag(it.Authority) {
			return violation, nil
		}
		return history, nil
	case scenario.MustNotAsRule:
		if it.Kind == scenario.KindRule || it.Kind == scenario.KindConstraint {
			return violation, nil
		}
		return history, nil
	case scenario.MustStore:
	default:
		return history, nil
	}
	if s.ended[f] {
		if it.State == StateActive && !s.carriedOn(r, f) {
			return stale, nil
		}
		return history, nil
	}
	permitted := slices.Contains(fact.Kinds, it.Kind)
	var problems []Problem
	switch {
	case it.State == StateActive && it.Kind != scenario.KindLog && permitted:
	case it.State == StateActive:
		problems = append(problems, ProblemKind)
	case it.State == StateLog && slices.Contains(fact.Kinds, scenario.KindLog):
		if it.Date != fact.LogDate {
			problems = append(problems, ProblemDate)
		}
	case it.State == StateRetired && permitted:
		problems = append(problems, ProblemValidity)
	default:
		return history, nil
	}
	if userTag(it.Authority) != (fact.Authority == scenario.AuthorityUser) || !userTag(it.Authority) && !slices.Contains(otherTags, it.Authority) {
		problems = append(problems, ProblemAuthority)
	}
	if !slices.Contains(scopes, it.Scope) {
		problems = append(problems, ProblemScope)
	}
	if len(problems) > 0 {
		return found, problems
	}
	return holds, nil
}

// carriedOn reports whether the item also supports a fact still in force
// that holds every value of ended fact f, as an acceptance carries on the
// proposal it accepts.
func (s *scorer) carriedOn(r *read, f int) bool {
	for _, sp := range r.supports {
		if g := sp.fact; g != f && s.required(g) && covers(s.sc.Facts[g], s.sc.Facts[f]) {
			return true
		}
	}
	return false
}

// covers reports whether every slot of f is a slot of g.
func covers(g, f scenario.Fact) bool {
	return !slices.ContainsFunc(f.Slots, func(fs scenario.Slot) bool { return !hasSlot(g, fs) })
}

// hasSlot reports whether slot sl is a slot of g, by class and by a value
// or form in common.
func hasSlot(g scenario.Fact, sl scenario.Slot) bool {
	return slices.ContainsFunc(g.Slots, func(gs scenario.Slot) bool {
		return gs.Class == sl.Class && slices.ContainsFunc(literals(gs), func(a string) bool {
			return slices.ContainsFunc(literals(sl), func(b string) bool { return slices.Equal(words(a), words(b)) })
		})
	})
}

// tiedFacts are the ids of the facts an item is tied to.
func (s *scorer) tiedFacts(r read) []string {
	var ids []string
	for _, sp := range r.supports {
		ids = append(ids, s.sc.Facts[sp.fact].ID)
	}
	if r.partial >= 0 {
		ids = append(ids, s.sc.Facts[r.partial].ID)
	}
	return ids
}

// fact gives fact f its outcome from the items tied to it.
func (s *scorer) fact(f int, ex Export, reads []read) FactResult {
	fact := s.sc.Facts[f]
	res := FactResult{Fact: fact.ID}
	var verdicts []support
	var placedLack, historyLack []Outcome
	for i, r := range reads {
		if j := slices.IndexFunc(r.supports, func(sp support) bool { return sp.fact == f }); j >= 0 {
			res.Items = append(res.Items, i)
			verdicts = append(verdicts, r.supports[j])
		}
		if r.partial == f {
			res.Items = append(res.Items, i)
			if s.placed(ex.Items[i], fact) {
				placedLack = append(placedLack, r.lack)
			} else {
				historyLack = append(historyLack, r.lack)
			}
		}
	}
	some := func(v verdict) bool {
		return slices.ContainsFunc(verdicts, func(sp support) bool { return sp.verdict == v })
	}
	switch {
	case fact.Must != scenario.MustStore:
		res.Outcome = Holds
		if some(violation) {
			res.Outcome = Violation
		}
	case s.ended[f]:
		res.Outcome = Holds
		if some(stale) {
			res.Outcome = StaleInForce
		}
	case some(holds):
		res.Outcome = Holds
	case some(found):
		res.Outcome = Found
		for _, sp := range verdicts {
			if sp.verdict == found && (res.Problems == nil || len(sp.problems) < len(res.Problems)) {
				res.Problems = sp.problems
			}
		}
	case len(placedLack) > 0:
		res.Outcome = worst(placedLack)
	case len(verdicts) > 0:
		res.Outcome, res.Problems = Found, []Problem{ProblemKind}
	case len(historyLack) > 0:
		res.Outcome = worst(historyLack)
	default:
		res.Outcome = Missing
	}
	return res
}

// placed reports whether an item stands where fact f would be stored: in
// force, as a dated line of a fact that may be one, or retired in one of
// its kinds.
func (s *scorer) placed(it Item, f scenario.Fact) bool {
	switch it.State {
	case StateActive:
		return true
	case StateLog:
		return slices.Contains(f.Kinds, scenario.KindLog)
	case StateRetired:
		return slices.Contains(f.Kinds, it.Kind)
	}
	return false
}

func worst(os []Outcome) Outcome {
	return slices.MinFunc(os, func(a, b Outcome) int { return slices.Index(slotOutcomes, a) - slices.Index(slotOutcomes, b) })
}

// userWords reports whether a turn is the user's own words.
func userWords(t scenario.Turn) bool {
	switch t.Author {
	case event.AuthorTyped, event.AuthorChosen, event.AuthorSpoken, event.AuthorV1User:
		return true
	}
	return false
}
