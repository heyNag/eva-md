package score

import (
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/heyNag/eva-md/internal/eval/scenario"
	"github.com/heyNag/eva-md/internal/ledger/event"
)

// The seeded set: the store a perfect organizer writes, and the ways to
// spoil it one at a time, each of which the scorer must catch but moved,
// which it must not count against the store.

// Distortion is one way to spoil a store.
type Distortion string

const (
	// DistortValueWrong gives a stored fact another value of the same class.
	DistortValueWrong Distortion = "value_wrong"
	// DistortDateDropped drops a stored fact's date.
	DistortDateDropped Distortion = "date_dropped"
	// DistortCondDropped drops a stored fact's condition.
	DistortCondDropped Distortion = "cond_dropped"
	// DistortInvented adds an active item that cites nothing.
	DistortInvented Distortion = "invented"
	// DistortWrongVoice files a stored fact under the other voice: the
	// user's tag for another's words, another's for the user's.
	DistortWrongVoice Distortion = "wrong_voice"
	// DistortStaleKept keeps a fact that ended in force.
	DistortStaleKept Distortion = "stale_kept"
	// DistortMoved files a stored fact in another kind it permits, which
	// is still right.
	DistortMoved Distortion = "moved"
)

// Distortions are the seeded set, in order.
var Distortions = []Distortion{DistortValueWrong, DistortDateDropped, DistortCondDropped, DistortInvented, DistortWrongVoice, DistortStaleKept, DistortMoved}

// ErrNotApplicable is Distort's error for an export it cannot spoil that
// way, as one with no stored date for date_dropped.
var ErrNotApplicable = errors.New("score: the distortion applies to nothing in the export")

// Ideal is the store a perfect organizer writes for sc: for each fact that
// must be stored, one item in its first permitted kind, holding each of
// its values as said, citing the line that plants it, under the tag its
// voice earns. A fact that ended is retired; one filed as a dated line
// stands under its day. Facts that must not be stored, or may be kept but
// need not be, are left out.
func Ideal(sc scenario.Scenario) Export {
	s := newScorer(sc)
	var ex Export
	for f, fact := range sc.Facts {
		if fact.Must != scenario.MustStore {
			continue
		}
		state := StateActive
		if s.ended[f] {
			state = StateRetired
		}
		it := idealItem(sc, fact, fact.Kinds[0])
		if it.Kind == scenario.KindLog {
			state, it.Date = StateLog, fact.LogDate
		}
		it.State = state
		ex.Items = append(ex.Items, it)
	}
	return ex
}

// idealItem is fact as a perfect organizer writes it in kind, but its state.
func idealItem(sc scenario.Scenario, fact scenario.Fact, kind scenario.Kind) Item {
	var vals []string
	for _, sl := range fact.Slots {
		vals = append(vals, sl.Value)
	}
	it := Item{Kind: kind, Text: strings.Join(vals, "; ")}
	src, turn, ok := plantedAt(sc, fact.ID)
	if ok {
		it.Sources = []Source{src}
	}
	switch {
	case fact.Authority == scenario.AuthorityUser:
		it.Authority = TagStated
		for _, g := range sc.Facts {
			if g.ID == fact.Supersedes && g.Authority != scenario.AuthorityUser {
				it.Authority = TagAccepted
			}
		}
	case turn.Author == event.AuthorTool:
		it.Authority = TagObserved
	case turn.Author == event.AuthorAgent || turn.Author == event.AuthorSubagent:
		it.Authority = TagSuggested
	default:
		it.Authority = TagExternal
	}
	return it
}

// plantedAt is the line that plants fact id, and its turn.
func plantedAt(sc scenario.Scenario, id string) (Source, scenario.Turn, bool) {
	for _, s := range sc.Sessions {
		for n, t := range s.Turns {
			if slices.Contains(t.Plants, id) {
				return Source{Session: s.ID, Seq: int64(n + 1)}, t, true
			}
		}
	}
	return Source{}, scenario.Turn{}, false
}

// Distort spoils ex one way and returns the spoiled copy; ex itself is not
// changed. It spoils the first stored fact, in sc's order, it applies to:
// the first item that stores it, a fact that must be stored and is still
// in force. value_wrong replaces the last of the fact's values a wrong one
// can be read for, date_dropped and cond_dropped remove its date or
// condition, each where the item's text holds it as said or in a form;
// wrong_voice changes the item's tag; moved files an active item in the
// fact's next permitted kind other than a dated line. stale_kept adds an
// active item for the first fact that ended and has a kind other than a
// dated line, as Ideal would write it in force, and invented adds an
// active item that cites nothing. It returns ErrNotApplicable when no
// fact or item fits.
func Distort(ex Export, sc scenario.Scenario, d Distortion) (Export, error) {
	if !slices.Contains(Distortions, d) {
		return ex, fmt.Errorf("score: unknown distortion %q", d)
	}
	s, reads, _ := evaluate(sc, ex)
	out := Export{Items: slices.Clone(ex.Items)}
	for i := range out.Items {
		out.Items[i].Sources = slices.Clone(out.Items[i].Sources)
	}
	switch d {
	case DistortInvented:
		out.Items = append(out.Items, Item{Kind: scenario.KindDecision, State: StateActive, Authority: TagStated,
			Text: "Move the build cache to a shared remote store"})
		return out, nil
	case DistortStaleKept:
		for f, fact := range sc.Facts {
			if fact.Must != scenario.MustStore || !s.ended[f] {
				continue
			}
			for _, k := range fact.Kinds {
				if k != scenario.KindLog {
					it := idealItem(sc, fact, k)
					it.State = StateActive
					out.Items = append(out.Items, it)
					return out, nil
				}
			}
		}
		return ex, ErrNotApplicable
	}
	for f, fact := range sc.Facts {
		if !s.required(f) {
			continue
		}
		i := slices.IndexFunc(reads, func(r read) bool {
			return slices.ContainsFunc(r.supports, func(sp support) bool { return sp.fact == f && sp.verdict == holds })
		})
		if i < 0 {
			continue
		}
		it := &out.Items[i]
		switch d {
		case DistortValueWrong:
			for _, sl := range slices.Backward(fact.Slots) {
				if w, ok := wrongValue(sl); ok && holdsLiteral(it.Text, sl) {
					it.Text = replaceLiterals(it.Text, sl, w)
					return out, nil
				}
			}
		case DistortDateDropped, DistortCondDropped:
			class := scenario.SlotDate
			if d == DistortCondDropped {
				class = scenario.SlotCond
			}
			for _, sl := range fact.Slots {
				if sl.Class == class && holdsLiteral(it.Text, sl) {
					it.Text = replaceLiterals(it.Text, sl, "")
					return out, nil
				}
			}
		case DistortWrongVoice:
			if userTag(it.Authority) {
				it.Authority = TagSuggested
			} else {
				it.Authority = TagStated
			}
			return out, nil
		case DistortMoved:
			var kinds []scenario.Kind
			for _, k := range fact.Kinds {
				if k != scenario.KindLog {
					kinds = append(kinds, k)
				}
			}
			if it.State == StateActive && len(kinds) >= 2 {
				it.Kind = kinds[(slices.Index(kinds, it.Kind)+1)%len(kinds)]
				return out, nil
			}
		}
	}
	return ex, ErrNotApplicable
}

// literalPattern finds a slot's literal as a whole, in any case.
func literalPattern(lit string) *regexp.Regexp {
	return regexp.MustCompile(`(?i)(^|[^\pL\pN_])` + regexp.QuoteMeta(lit) + `($|[^\pL\pN_])`)
}

// holdsLiteral reports whether s holds one of the slot's literals as said.
func holdsLiteral(s string, sl scenario.Slot) bool {
	return slices.ContainsFunc(literals(sl), func(lit string) bool { return literalPattern(lit).MatchString(s) })
}

// replaceLiterals replaces each of the slot's literals in s, the longest
// first, with with.
func replaceLiterals(s string, sl scenario.Slot, with string) string {
	lits := literals(sl)
	slices.SortStableFunc(lits, func(a, b string) int { return len(b) - len(a) })
	for _, lit := range lits {
		s = literalPattern(lit).ReplaceAllString(s, "${1}"+strings.ReplaceAll(with, "$", "$$")+"${2}")
	}
	return s
}

var firstNumber = regexp.MustCompile(`[0-9]+`)

// wrongValue is a value of the slot's class the scorer reads as another,
// for the classes where it can tell a wrong value from a dropped one.
func wrongValue(sl scenario.Slot) (string, bool) {
	v := sl.Value
	switch sl.Class {
	case scenario.SlotInt:
		n, err := strconv.Atoi(strings.TrimSpace(v))
		return strconv.Itoa(n + 1), err == nil
	case scenario.SlotQuantity:
		loc := firstNumber.FindStringIndex(v)
		if loc == nil {
			return "", false
		}
		n, _ := strconv.Atoi(v[loc[0]:loc[1]])
		return v[:loc[0]] + strconv.Itoa(n+1) + v[loc[1]:], true
	case scenario.SlotDate:
		ds := dates(v)
		if len(ds) == 0 {
			return "", false
		}
		switch ds[0] {
		case "yesterday":
			return "tomorrow", true
		case "today":
			return "yesterday", true
		case "tomorrow":
			return "today", true
		}
		t, err := time.Parse(time.DateOnly, ds[0])
		return t.AddDate(0, 0, 7).Format(time.DateOnly), err == nil
	case scenario.SlotPath:
		if i := strings.LastIndexByte(v, '/'); i >= 0 {
			return v[:i+1] + "old/" + v[i+1:], true
		}
		return "old/" + v, true
	case scenario.SlotVersion:
		locs := firstNumber.FindAllStringIndex(v, -1)
		if len(locs) == 0 {
			return "", false
		}
		loc := locs[len(locs)-1]
		n, _ := strconv.Atoi(v[loc[0]:loc[1]])
		return v[:loc[0]] + strconv.Itoa(n+1) + v[loc[1]:], true
	case scenario.SlotIdent:
		return v + "_2", true
	}
	return "", false
}
