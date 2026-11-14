package score_test

import (
	"errors"
	"fmt"
	"math"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/heyNag/eva-md/internal/eval/scenario"
	"github.com/heyNag/eva-md/internal/eval/score"
	"github.com/heyNag/eva-md/internal/host/adapter"
	"github.com/heyNag/eva-md/internal/ledger/event"
)

// required counts the facts of sc that must be stored and are in force.
func required(sc scenario.Scenario) int {
	ended := map[string]bool{}
	for _, f := range sc.Facts {
		if f.Until != nil {
			ended[f.ID] = true
		}
		if f.Supersedes != "" {
			ended[f.Supersedes] = true
		}
	}
	n := 0
	for _, f := range sc.Facts {
		if f.Must == scenario.MustStore && !ended[f.ID] {
			n++
		}
	}
	return n
}

// mine is the outcome the item a distortion spoils, or adds, must get.
var mine = map[score.Distortion]score.Outcome{
	score.DistortValueWrong: score.ValueWrong, score.DistortDateDropped: score.DateDropped, score.DistortCondDropped: score.CondDropped,
	score.DistortInvented: score.Unsupported, score.DistortStaleKept: score.StaleInForce,
}

// seededRight reports how the scorer's result on spoiled, base spoiled by
// d, differs from what d must give, against its result on base: exactly
// the one fact d spoils changes, to the outcome d names, the one item d
// changes or adds gets it, and the metrics move by that and nothing else.
// moved changes nothing.
func seededRight(sc scenario.Scenario, base, spoiled score.Export, d score.Distortion) error {
	b, s := score.Score(sc, base), score.Score(sc, spoiled)
	var changedFacts []string
	for i := range b.Facts {
		if b.Facts[i].Outcome != s.Facts[i].Outcome || !slices.Equal(b.Facts[i].Problems, s.Facts[i].Problems) {
			changedFacts = append(changedFacts, fmt.Sprintf("%s %s %v", s.Facts[i].Fact, s.Facts[i].Outcome, s.Facts[i].Problems))
		}
		if b.Facts[i].Outcome != score.Holds {
			return fmt.Errorf("the base scores %s %s", b.Facts[i].Fact, b.Facts[i].Outcome)
		}
	}
	// The item d changes or adds.
	at := -1
	for i, it := range spoiled.Items {
		if i >= len(base.Items) || !reflect.DeepEqual(it, base.Items[i]) {
			if at >= 0 {
				return errors.New("more than one item changed")
			}
			at = i
		}
	}
	if at < 0 || len(spoiled.Items) < len(base.Items) {
		return errors.New("no item changed")
	}
	for i := range s.Items {
		if i != at && !reflect.DeepEqual(s.Items[i], b.Items[i]) {
			return fmt.Errorf("item %d went from %s to %s", i, b.Items[i].Outcome, s.Items[i].Outcome)
		}
	}
	want := b.Metrics
	share := 1 / float64(required(sc))
	var wantFact string
	switch d {
	case score.DistortValueWrong, score.DistortDateDropped, score.DistortCondDropped:
		want.WrongWrite++
		want.FactHolds -= share
		want.FactRecall -= share
		wantFact = string(mine[d]) + " []"
	case score.DistortInvented:
		want.WrongWrite++
	case score.DistortStaleKept:
		want.StaleInForce++
		wantFact = string(score.StaleInForce) + " []"
	case score.DistortWrongVoice:
		want.FactHolds -= share
		wantFact = "found [authority]"
		if it := spoiled.Items[at]; it.Authority == score.TagStated && othersOnly(sc, it) {
			want.Misattributed++
		}
	case score.DistortMoved:
		if spoiled.Items[at].Kind == base.Items[at].Kind {
			return errors.New("moved left the kind")
		}
	}
	switch {
	case wantFact == "" && len(changedFacts) != 0:
		return fmt.Errorf("facts changed: %v", changedFacts)
	case wantFact != "" && (len(changedFacts) != 1 || !endsWith(changedFacts[0], wantFact)):
		return fmt.Errorf("facts changed: %v, want one to %s", changedFacts, wantFact)
	}
	if o, ok := mine[d]; ok && s.Items[at].Outcome != o {
		return fmt.Errorf("the item is %s, want %s", s.Items[at].Outcome, o)
	}
	if !closeMetrics(s.Metrics, want) {
		return fmt.Errorf("metrics %+v, want %+v", s.Metrics, want)
	}
	return nil
}

func endsWith(s, suffix string) bool {
	return len(s) >= len(suffix) && s[len(s)-len(suffix):] == suffix
}

// othersOnly reports an item every line of which is someone else's words.
func othersOnly(sc scenario.Scenario, it score.Item) bool {
	n := 0
	for _, src := range it.Sources {
		t, _, err := sc.Locate(src.Session, src.Seq)
		if err != nil {
			continue
		}
		n++
		switch t.Author {
		case event.AuthorTyped, event.AuthorChosen, event.AuthorSpoken, event.AuthorV1User:
			return false
		}
	}
	return n > 0
}

func closeMetrics(a, b score.Metrics) bool {
	near := func(x, y float64) bool { return math.Abs(x-y) < 1e-9 }
	fa, fb := a, b
	fa.FactRecall, fa.FactHolds, fb.FactRecall, fb.FactHolds = 0, 0, 0, 0
	return near(a.FactRecall, b.FactRecall) && near(a.FactHolds, b.FactHolds) && fa == fb
}

// TestIdealScoresPerfectly: the store a perfect organizer writes holds
// every fact of every ported scenario, with every count at 0.
func TestIdealScoresPerfectly(t *testing.T) {
	for _, sc := range append(scenario.V1(), twoKinds()) {
		if bad := clean(sc, score.Score(sc, score.Ideal(sc))); len(bad) != 0 {
			t.Errorf("%s: %v", sc.ID, bad)
		}
	}
}

// TestSeededSet: every way of the seeded set is scored right on every
// ported scenario it applies to, and each applies to at least one.
func TestSeededSet(t *testing.T) {
	for _, d := range score.Distortions {
		applied := 0
		for _, sc := range scenario.V1() {
			ideal := score.Ideal(sc)
			spoiled, err := score.Distort(ideal, sc, d)
			if errors.Is(err, score.ErrNotApplicable) {
				if !reflect.DeepEqual(spoiled, ideal) {
					t.Errorf("%s, %s: an export it does not apply to changed", sc.ID, d)
				}
				continue
			}
			if err != nil {
				t.Fatal(err)
			}
			applied++
			if err := seededRight(sc, ideal, spoiled, d); err != nil {
				t.Errorf("%s, %s: %v", sc.ID, d, err)
			}
			if again := score.Ideal(sc); !reflect.DeepEqual(ideal, again) {
				t.Errorf("%s, %s: Distort changed the export it was given", sc.ID, d)
			}
		}
		if applied == 0 {
			t.Errorf("%s applies to no ported scenario", d)
		}
	}
	if _, err := score.Distort(score.Ideal(scenario.V1()[0]), scenario.V1()[0], "blurred"); err == nil || errors.Is(err, score.ErrNotApplicable) {
		t.Errorf("an unknown distortion: %v", err)
	}
}

// twoKinds is a made-up scenario with one fact a store may file as a
// decision or as a constraint.
func twoKinds() scenario.Scenario {
	at := time.Date(2026, 3, 2, 9, 0, 0, 0, time.UTC)
	return scenario.Scenario{ID: "test.two-kinds", Family: "test", Name: "a fact of two kinds", Split: scenario.SplitTuning,
		Sessions: []scenario.Session{{Host: string(adapter.ClaudeCode), ID: "5eed0001-aaaa-4000-8000-000000000001", Branch: "main", Turns: []scenario.Turn{
			{At: at, Author: event.AuthorTyped, Kind: event.KindPrompt, Text: "Keep the queue under 200 items, QUOKKA-3.", Plants: []string{"q1"}},
		}}},
		Facts: []scenario.Fact{{ID: "q1", Kinds: []scenario.Kind{scenario.KindDecision, scenario.KindConstraint}, Authority: scenario.AuthorityUser,
			Must: scenario.MustStore, Slots: []scenario.Slot{{Class: scenario.SlotIdent, Value: "QUOKKA-3"}, {Class: scenario.SlotInt, Value: "200"}}}},
		Passes: []scenario.Pass{{}},
	}
}

// TestDistortAsksWhatTheScorerAsks: a slot left unasked of the item's
// kind is never the one the seeded set spoils, since the scorer rightly
// does not miss it there; date_dropped, whose only date is such a slot,
// applies to nothing, and every other way is scored right.
func TestDistortAsksWhatTheScorerAsks(t *testing.T) {
	sc := twoKinds()
	sc.Facts[0].Kinds = []scenario.Kind{scenario.KindOpen, scenario.KindFact}
	sc.Facts[0].Slots = []scenario.Slot{{Class: scenario.SlotIdent, Value: "QUOKKA-3"}, {Class: scenario.SlotCond, Value: "if the queue is cold"},
		{Class: scenario.SlotDate, Value: "3 March 2026", Unasked: []scenario.Kind{scenario.KindOpen}}}
	if err := sc.Validate(); err != nil {
		t.Fatal(err)
	}
	ideal := score.Ideal(sc)
	if ideal.Items[0].Kind != scenario.KindOpen || ideal.Items[0].Text != "QUOKKA-3; if the queue is cold; 3 March 2026" {
		t.Fatalf("the ideal store is %+v", ideal)
	}
	for _, d := range score.Distortions {
		spoiled, err := score.Distort(ideal, sc, d)
		switch {
		case d == score.DistortDateDropped || d == score.DistortStaleKept:
			if !errors.Is(err, score.ErrNotApplicable) {
				t.Errorf("%s applies: %v, %+v", d, err, spoiled)
			}
		case err != nil:
			t.Errorf("%s: %v", d, err)
		default:
			if err := seededRight(sc, ideal, spoiled, d); err != nil {
				t.Errorf("%s: %v", d, err)
			}
			if d == score.DistortValueWrong && !strings.Contains(spoiled.Items[0].Text, "3 March 2026") {
				t.Errorf("value_wrong spoils the unasked date: %q", spoiled.Items[0].Text)
			}
		}
	}
}

// TestMovedKeepsHolds: a fact of two kinds, moved from one to the other,
// still holds.
func TestMovedKeepsHolds(t *testing.T) {
	sc := twoKinds()
	if err := sc.Validate(); err != nil {
		t.Fatal(err)
	}
	ideal := score.Ideal(sc)
	moved, err := score.Distort(ideal, sc, score.DistortMoved)
	if err != nil {
		t.Fatal(err)
	}
	if moved.Items[0].Kind != scenario.KindConstraint || ideal.Items[0].Kind != scenario.KindDecision {
		t.Fatalf("moved %+v from %+v", moved.Items[0], ideal.Items[0])
	}
	if r := score.Score(sc, moved); r.Metrics.FactHolds != 1 || r.Facts[0].Outcome != score.Holds {
		t.Errorf("a moved fact: %+v", r)
	}
	if err := seededRight(sc, ideal, moved, score.DistortMoved); err != nil {
		t.Error(err)
	}
	// Filed in a kind it does not permit, it is found, not held.
	wrong := score.Export{Items: slices.Clone(ideal.Items)}
	wrong.Items[0].Kind = scenario.KindNext
	if r := score.Score(sc, wrong); r.Facts[0].Outcome != score.Found || !slices.Equal(r.Facts[0].Problems, []score.Problem{score.ProblemKind}) || r.Metrics.FactHolds != 0 || r.Metrics.FactRecall != 1 {
		t.Errorf("a fact in a kind it does not permit: %+v", r)
	}
}
