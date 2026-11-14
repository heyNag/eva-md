package families

import (
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/heyNag/eva-md/internal/eval/scenario"
	"github.com/heyNag/eva-md/internal/eval/score"
	"github.com/heyNag/eva-md/internal/ledger/event"
)

func scoreIdeal(sc scenario.Scenario) score.Result { return score.Score(sc, score.Ideal(sc)) }

// perfect is the metrics of a store that holds every fact and errs in
// nothing.
var perfect = score.Metrics{FactRecall: 1, FactHolds: 1}

// plantedAt is where the turn that plants fact id stands.
func plantedAt(t *testing.T, sc scenario.Scenario, id string) (score.Source, scenario.Turn) {
	t.Helper()
	for _, s := range sc.Sessions {
		for n, tr := range s.Turns {
			if slices.Contains(tr.Plants, id) {
				return score.Source{Session: s.ID, Seq: int64(n + 1)}, tr
			}
		}
	}
	t.Fatalf("%s: no turn plants %s", sc.ID, id)
	return score.Source{}, scenario.Turn{}
}

// itemOf is an active item of kind under tag that holds text and cites
// the turn that plants fact id.
func itemOf(t *testing.T, sc scenario.Scenario, kind scenario.Kind, tag, text, id string) score.Item {
	src, _ := plantedAt(t, sc, id)
	return score.Item{Kind: kind, State: score.StateActive, Authority: tag, Text: text, Sources: []score.Source{src}}
}

// with is the ideal store of sc with items added.
func with(sc scenario.Scenario, items ...score.Item) score.Export {
	ex := score.Ideal(sc)
	ex.Items = append(ex.Items, items...)
	return ex
}

// factResult is the result of fact id.
func factResult(r score.Result, id string) score.FactResult {
	for _, f := range r.Facts {
		if f.Fact == id {
			return f
		}
	}
	return score.FactResult{}
}

// facts are the facts of sc that must be as must is.
func facts(sc scenario.Scenario, must scenario.Must) []scenario.Fact {
	return slices.DeleteFunc(slices.Clone(sc.Facts), func(f scenario.Fact) bool { return f.Must != must })
}

// TestIdealAndMovedHold: on every instance of every family, the store a
// perfect organizer writes holds every fact with every count 0, and so
// does the same store with a fact moved to another kind it permits.
func TestIdealAndMovedHold(t *testing.T) {
	for _, scs := range instances(t) {
		for _, sc := range scs {
			ideal := score.Ideal(sc)
			r := score.Score(sc, ideal)
			if r.Metrics != perfect {
				t.Errorf("%s: the ideal store scores %+v", sc.ID, r.Metrics)
			}
			for _, f := range r.Facts {
				if f.Outcome != score.Holds {
					t.Errorf("%s: the ideal store leaves %s %s", sc.ID, f.Fact, f.Outcome)
				}
			}
			moved, err := score.Distort(ideal, sc, score.DistortMoved)
			if err != nil {
				t.Fatalf("%s: %v", sc.ID, err)
			}
			changed := 0
			for i := range moved.Items {
				if !reflect.DeepEqual(moved.Items[i], ideal.Items[i]) {
					changed++
					if moved.Items[i].Kind == ideal.Items[i].Kind {
						t.Errorf("%s: moved changed item %d and left its kind", sc.ID, i)
					}
				}
			}
			if r := score.Score(sc, moved); changed != 1 || r.Metrics != perfect {
				t.Errorf("%s: %d items moved, and the store scores %+v", sc.ID, changed, r.Metrics)
			}
		}
	}
}

// TestMomentIsNeverARule: an instruction meant for the moment stored as a
// rule or a constraint is a violation, of that item and that fact, and
// counts once; kept as a next step it is not. The standing instruction
// is stored as a rule.
func TestMomentIsNeverARule(t *testing.T) {
	for _, sc := range instances(t)["moment"] {
		standing := facts(sc, scenario.MustStore)
		if len(standing) != 1 || standing[0].Kinds[0] != scenario.KindRule || score.Ideal(sc).Items[0].Kind != scenario.KindRule {
			t.Errorf("%s: the standing instruction is %+v", sc.ID, standing)
		}
		momentary := facts(sc, scenario.MustNotAsRule)
		if len(momentary) != 2 {
			t.Fatalf("%s plants %d instructions for the moment", sc.ID, len(momentary))
		}
		for i, f := range momentary {
			kind := []scenario.Kind{scenario.KindRule, scenario.KindConstraint}[i]
			r := score.Score(sc, with(sc, itemOf(t, sc, kind, score.TagStated, f.Slots[0].Value, f.ID)))
			want := perfect
			want.Violations = 1
			if r.Metrics != want || r.Items[len(r.Items)-1].Outcome != score.Violation || factResult(r, f.ID).Outcome != score.Violation {
				t.Errorf("%s: %s stored as a %s scores %+v, item %s", sc.ID, f.ID, kind, r.Metrics, r.Items[len(r.Items)-1].Outcome)
			}
			r = score.Score(sc, with(sc, itemOf(t, sc, scenario.KindNext, score.TagStated, f.Slots[0].Value, f.ID)))
			if r.Metrics != perfect || factResult(r, f.ID).Outcome != score.Holds {
				t.Errorf("%s: %s kept as a next step scores %+v", sc.ID, f.ID, r.Metrics)
			}
		}
	}
}

// TestCorrectionKeepsTheNewValue: the corrected value is the one in force;
// an active item that holds the old value is stale_in_force whether it
// cites the user's first words or the agent's reply that repeats them,
// unless it holds the correction in full and names the old value as its
// history; and a store that keeps only the old value in force holds no
// fact.
func TestCorrectionKeepsTheNewValue(t *testing.T) {
	for _, sc := range instances(t)["correction"] {
		if len(sc.Facts) != 3 {
			t.Fatalf("%s plants %d facts", sc.ID, len(sc.Facts))
		}
		old, fixed, echo := sc.Facts[0], sc.Facts[1], sc.Facts[2]
		echoAt, echoTurn := plantedAt(t, sc, echo.ID)
		fixedAt, _ := plantedAt(t, sc, fixed.ID)
		if echoAt.Seq <= fixedAt.Seq || fixed.Supersedes != old.ID || old.Until == nil || echo.Until == nil || echo.Slots[0].Value != old.Slots[0].Value ||
			echo.Authority != scenario.AuthorityOther || echoTurn.Author != event.AuthorAgent || fixed.Slots[0].Value == old.Slots[0].Value {
			t.Fatalf("%s: the facts are %+v", sc.ID, sc.Facts)
		}
		ideal := score.Ideal(sc)
		if r := score.Score(sc, ideal); factResult(r, fixed.ID).Outcome != score.Holds || ideal.Items[1].State != score.StateActive ||
			ideal.Items[0].State != score.StateRetired || ideal.Items[2].State != score.StateRetired {
			t.Errorf("%s: the ideal store is %+v", sc.ID, ideal)
		}
		for at, tag := range map[string]string{old.ID: score.TagStated, echo.ID: score.TagSuggested} {
			r := score.Score(sc, with(sc, itemOf(t, sc, scenario.KindFact, tag, old.Slots[0].Value, at)))
			want := perfect
			want.StaleInForce = 1
			if r.Metrics != want || r.Items[len(r.Items)-1].Outcome != score.StaleInForce || factResult(r, at).Outcome != score.StaleInForce {
				t.Errorf("%s: the old value in force, citing %s, scores %+v, item %s", sc.ID, at, r.Metrics, r.Items[len(r.Items)-1].Outcome)
			}
		}
		onlyOld := score.Export{Items: []score.Item{itemOf(t, sc, scenario.KindDecision, score.TagStated, old.Slots[0].Value, old.ID)}}
		if r := score.Score(sc, onlyOld); r.Metrics.FactHolds != 0 || r.Metrics.StaleInForce != 1 || factResult(r, fixed.ID).Outcome != score.Missing {
			t.Errorf("%s: only the old value scores %+v", sc.ID, r.Metrics)
		}
		// A store that keeps the corrected value in force and names the
		// old one as its history holds every fact, whether its item cites
		// the old line and the correction, the correction alone, or either
		// with the agent's echo of the old value.
		oldAt, _ := plantedAt(t, sc, old.ID)
		kept := fixed.Slots[0].Value + ", " + fixed.Slots[1].Value
		history := func(text string, cites ...score.Source) score.Result {
			ex := score.Ideal(sc)
			ex.Items[1] = score.Item{Kind: scenario.KindDecision, State: score.StateActive, Authority: score.TagStated, Text: text, Sources: cites}
			return score.Score(sc, ex)
		}
		for _, tc := range []struct {
			text  string
			cites []score.Source
		}{
			{kept + ", corrected from " + old.Slots[0].Value, []score.Source{oldAt, fixedAt}},
			{kept + ", corrected from " + old.Slots[0].Value, []score.Source{fixedAt}},
			{kept + ", corrected from " + old.Slots[0].Value, []score.Source{fixedAt, echoAt}},
			{kept + ", corrected from " + old.Slots[0].Value, []score.Source{oldAt, fixedAt, echoAt}},
			{kept + ", replacing " + old.Slots[1].Value, []score.Source{oldAt, fixedAt}},
			{"Not " + old.Slots[0].Value + " after all: " + kept, []score.Source{oldAt, fixedAt}},
		} {
			r := history(tc.text, tc.cites...)
			if r.Metrics != perfect || r.Items[1].Outcome != score.Supports {
				t.Errorf("%s: %q, citing %v, scores %+v, item %s", sc.ID, tc.text, tc.cites, r.Metrics, r.Items[1].Outcome)
			}
			for _, f := range r.Facts {
				if f.Outcome != score.Holds {
					t.Errorf("%s: %q, citing %v, leaves %s %s", sc.ID, tc.text, tc.cites, f.Fact, f.Outcome)
				}
			}
		}
		// The cost of the references, recorded: a store that keeps the
		// corrected value in force but drops its reference has not kept the
		// correction in full, as a dropped codename in v1's scenarios. Alone,
		// the value scores value_dropped; beside the old value named as its
		// history it scores stale_in_force, since only an item that holds
		// the correction in full may name the old value while active. Read
		// this family's numbers with both in mind.
		if r := history(fixed.Slots[0].Value, fixedAt); r.Metrics.FactHolds == 1 || r.Metrics.WrongWrite != 1 || r.Metrics.StaleInForce != 0 ||
			factResult(r, fixed.ID).Outcome != score.ValueDropped || r.Items[1].Outcome != score.ValueDropped {
			t.Errorf("%s: the corrected value alone, without its reference, scores %+v, fact %s, item %s; the rule recorded is value_dropped",
				sc.ID, r.Metrics, factResult(r, fixed.ID).Outcome, r.Items[1].Outcome)
		}
		if r := history(fixed.Slots[0].Value+", corrected from "+old.Slots[0].Value, oldAt, fixedAt); r.Metrics.FactHolds == 1 || r.Metrics.StaleInForce != 1 ||
			r.Items[1].Outcome != score.StaleInForce {
			t.Errorf("%s: the corrected value without its reference, naming the old one, scores %+v, item %s; the rule recorded is stale_in_force",
				sc.ID, r.Metrics, r.Items[1].Outcome)
		}
		// The echo is spared only as the history of the correction held in
		// full: held in full itself, beside the correction, it is kept in
		// force.
		echoed := kept + "; the agent said " + echo.Slots[0].Value + ", " + echo.Slots[1].Value
		if r := history(echoed, fixedAt, echoAt); r.Metrics.StaleInForce != 1 || r.Items[1].Outcome != score.StaleInForce ||
			factResult(r, echo.ID).Outcome != score.StaleInForce || factResult(r, fixed.ID).Outcome != score.Holds {
			t.Errorf("%s: the echo in full beside the correction scores %+v, item %s, echo %s", sc.ID, r.Metrics, r.Items[1].Outcome, factResult(r, echo.ID).Outcome)
		}
	}
}

// TestOtherAuthorHasNoAuthority: the subagent's decision-shaped line
// stored under the user's authority is a violation; under another's it is
// not. The line is the subagent's, by a turn that names it.
func TestOtherAuthorHasNoAuthority(t *testing.T) {
	for _, sc := range instances(t)["other-author"] {
		lines := facts(sc, scenario.MustNotWithAuthority)
		if len(lines) != 1 {
			t.Fatalf("%s plants %d lines of another's", sc.ID, len(lines))
		}
		f := lines[0]
		if _, tr := plantedAt(t, sc, f.ID); tr.Author != event.AuthorSubagent || tr.Agent == "" {
			t.Errorf("%s: the line is by %s, agent %q", sc.ID, tr.Author, tr.Agent)
		}
		for _, tag := range []string{score.TagStated, score.TagAccepted} {
			r := score.Score(sc, with(sc, itemOf(t, sc, scenario.KindDecision, tag, f.Slots[0].Value, f.ID)))
			if r.Metrics.Violations != 1 || r.Items[len(r.Items)-1].Outcome != score.Violation || factResult(r, f.ID).Outcome != score.Violation {
				t.Errorf("%s: the line under %s scores %+v", sc.ID, tag, r.Metrics)
			}
		}
		if r := score.Score(sc, with(sc, itemOf(t, sc, scenario.KindOpen, score.TagSuggested, f.Slots[0].Value, f.ID))); r.Metrics != perfect {
			t.Errorf("%s: the line kept as another's scores %+v", sc.ID, r.Metrics)
		}
	}
}

// TestUnagreedHasNoAuthority: the agent's proposal under the user's
// authority is a violation, whether the item cites the agent's line or
// the user's question about it; under another's it is not.
func TestUnagreedHasNoAuthority(t *testing.T) {
	for _, sc := range instances(t)["unagreed"] {
		lines := facts(sc, scenario.MustNotWithAuthority)
		if len(lines) != 2 || lines[0].Slots[0].Value != lines[1].Slots[0].Value {
			t.Fatalf("%s: the proposal is planted as %+v", sc.ID, lines)
		}
		for _, f := range lines {
			r := score.Score(sc, with(sc, itemOf(t, sc, scenario.KindDecision, score.TagStated, f.Slots[0].Value, f.ID)))
			if r.Metrics.Violations != 1 || r.Items[len(r.Items)-1].Outcome != score.Violation || factResult(r, f.ID).Outcome != score.Violation {
				t.Errorf("%s: the proposal under the user's authority, citing %s, scores %+v", sc.ID, f.ID, r.Metrics)
			}
			if r := score.Score(sc, with(sc, itemOf(t, sc, scenario.KindOpen, score.TagSuggested, f.Slots[0].Value, f.ID))); r.Metrics != perfect {
				t.Errorf("%s: the proposal kept as the agent's, citing %s, scores %+v", sc.ID, f.ID, r.Metrics)
			}
		}
	}
}

// TestAcceptedIsADecisionNeverARule: the acceptance is stored under the
// accepted authority as a decision; stored as a rule it is a violation;
// stored under the agent's authority, suggested or observed, it is found
// with the wrong authority. Stored under stated it is
// TestAcceptedUnderStatedIsFoundWithTheWrongAuthority's.
func TestAcceptedIsADecisionNeverARule(t *testing.T) {
	for _, sc := range instances(t)["accepted"] {
		stored, nevers := facts(sc, scenario.MustStore), facts(sc, scenario.MustNotAsRule)
		if len(stored) != 2 || len(nevers) != 1 {
			t.Fatalf("%s plants %d facts to store and %d never to be a rule", sc.ID, len(stored), len(nevers))
		}
		choice, never := stored[1], nevers[0]
		ideal := score.Ideal(sc)
		if ideal.Items[1].Authority != score.TagAccepted || ideal.Items[1].Kind != scenario.KindDecision {
			t.Errorf("%s: the acceptance is stored as %+v", sc.ID, ideal.Items[1])
		}
		r := score.Score(sc, with(sc, itemOf(t, sc, scenario.KindRule, score.TagAccepted, choice.Slots[0].Value, choice.ID)))
		if r.Metrics.Violations != 1 || r.Items[len(r.Items)-1].Outcome != score.Violation || factResult(r, never.ID).Outcome != score.Violation {
			t.Errorf("%s: the acceptance stored as a rule scores %+v", sc.ID, r.Metrics)
		}
		for _, tag := range []string{score.TagSuggested, score.TagObserved} {
			ex := score.Ideal(sc)
			ex.Items[1].Authority = tag
			fr := factResult(score.Score(sc, ex), choice.ID)
			if fr.Outcome != score.Found || !slices.Equal(fr.Problems, []score.Problem{score.ProblemAuthority}) {
				t.Errorf("%s: the acceptance under %s is %s %v, want found with the wrong authority", sc.ID, tag, fr.Outcome, fr.Problems)
			}
		}
	}
}

// TestAcceptedUnderStatedIsFoundWithTheWrongAuthority: the acceptance
// stored under stated, as though the user had said it unasked, is found
// with the wrong authority, as a decision or a procedure. It costs
// fact_holds the acceptance's share, the only fact to store still in
// force, and nothing else: it is still recalled, and the user's choice is
// the user's words, so it is not misattributed.
func TestAcceptedUnderStatedIsFoundWithTheWrongAuthority(t *testing.T) {
	for _, sc := range instances(t)["accepted"] {
		stored := facts(sc, scenario.MustStore)
		if len(stored) != 2 {
			t.Fatalf("%s plants %d facts to store", sc.ID, len(stored))
		}
		choice := stored[1]
		for _, kind := range choice.Kinds {
			ex := score.Ideal(sc)
			ex.Items[1].Authority, ex.Items[1].Kind = score.TagStated, kind
			r := score.Score(sc, ex)
			want := perfect
			want.FactHolds = 0
			if fr := factResult(r, choice.ID); fr.Outcome != score.Found || !slices.Equal(fr.Problems, []score.Problem{score.ProblemAuthority}) ||
				r.Metrics != want {
				t.Errorf("%s: the acceptance under stated, as a %s, is %s %v, metrics %+v; want found [authority] and %+v",
					sc.ID, kind, fr.Outcome, fr.Problems, r.Metrics, want)
			}
			ex.Items[1].Authority = score.TagAccepted
			if r := score.Score(sc, ex); r.Metrics != perfect {
				t.Errorf("%s: the acceptance under accepted, as a %s, scores %+v", sc.ID, kind, r.Metrics)
			}
		}
	}
}

// keptLimits are, for each form of limit, ways a store may write the
// finding with its limit kept, in words of its own; {v} is the finding's
// name, {n} a sample's size and {w} a hedge. Each must hold.
var keptLimits = map[string][]string{
	"sample": {
		"No {v} errors across {n} runs", "No {v} errors in {n} runs checked", "{v}: no errors, sample of {n}",
		"{v} clean in {n} sampled runs",
	},
	"deployed": {
		"The {v} fix isn't deployed", "The {v} fix isn’t deployed", "{v} fix: deploy pending", "The {v} patch is not in production",
		"{v} change, pending deployment", "The {v} fix hasn't been deployed", "{v}: tested, awaiting deploy", "The {v} fix is not yet live",
	},
	"refused": {
		"Reading {v} was blocked: access forbidden", "{v}: permission denied", "Access to {v} was rejected", "No access to {v}",
		"Querying {v} is not allowed", "{v} returned unauthorized", "{v}: the read was blocked",
	},
	"hypothesis": {"The {v} cache causes the slow start ({w})"},
}

// TestLimitSurvives: a finding stored without its limit, or a hypothesis
// stored as a fact without its hedge, is a wrong write, limit_dropped, of
// the item and the fact; with it, in the words it was said in or in one
// of the common ways of writing it, it holds, as an open question or as a
// fact. A finding of the first three forms holds as a dated line too; a
// hypothesis may be filed only as an open question or a fact, so as a
// dated line it does not hold. The draw holds every form of limit.
func TestLimitSurvives(t *testing.T) {
	forms := map[string]bool{}
	for _, sc := range instances(t)["limit"] {
		if len(sc.Facts) != 1 || len(sc.Facts[0].Slots) != 2 {
			t.Fatalf("%s: the facts are %+v", sc.ID, sc.Facts)
		}
		f := sc.Facts[0]
		bound := f.Slots[1]
		form, n := "", ""
		switch {
		case slices.Contains(hedges, bound.Value):
			form = "hypothesis"
		case wordRun(bound.Value, "sample"):
			form = "sample"
			n = strings.Fields(bound.Value)[2]
		case bound.Value == "not deployed":
			form = "deployed"
		case bound.Value == "refused":
			form = "refused"
		default:
			t.Errorf("%s: a limit of no form, %q", sc.ID, bound.Value)
		}
		forms[form] = true
		var phrasings []string
		for _, p := range keptLimits[form] {
			if form != "hypothesis" {
				phrasings = append(phrasings, fillIn(p, f.Slots[0].Value, "", n))
				continue
			}
			for _, h := range hedges {
				phrasings = append(phrasings, fillIn(p, f.Slots[0].Value, h, ""))
			}
		}
		for _, text := range phrasings {
			for _, kind := range []scenario.Kind{scenario.KindOpen, scenario.KindFact} {
				kept := score.Export{Items: []score.Item{itemOf(t, sc, kind, score.TagSuggested, text, f.ID)}}
				if r := score.Score(sc, kept); r.Metrics != perfect {
					t.Errorf("%s: %q, as %s, scores %+v, item %s", sc.ID, text, kind, r.Metrics, r.Items[0].Outcome)
				}
			}
		}
		_, tr := plantedAt(t, sc, f.ID)
		logged := itemOf(t, sc, scenario.KindLog, score.TagSuggested, f.Slots[0].Value+", "+bound.Value, f.ID)
		logged.State, logged.Date = score.StateLog, tr.At.Format(time.DateOnly)
		r := score.Score(sc, score.Export{Items: []score.Item{logged}})
		if form == "hypothesis" {
			if !slices.Equal(f.Kinds, []scenario.Kind{scenario.KindOpen, scenario.KindFact}) || f.LogDate != "" || r.Facts[0].Outcome == score.Holds {
				t.Errorf("%s: a hypothesis may be filed as %v, and as a dated line it is %s", sc.ID, f.Kinds, r.Facts[0].Outcome)
			}
		} else if r.Metrics != perfect {
			t.Errorf("%s: the finding with %q, as a dated line, scores %+v", sc.ID, bound.Value, r.Metrics)
		}
		dropped := score.Export{Items: []score.Item{itemOf(t, sc, scenario.KindFact, score.TagSuggested, f.Slots[0].Value, f.ID)}}
		r = score.Score(sc, dropped)
		want := score.Metrics{WrongWrite: 1}
		if r.Metrics != want || r.Items[0].Outcome != score.LimitDropped || r.Facts[0].Outcome != score.LimitDropped {
			t.Errorf("%s: the finding without %q scores %+v, item %s, fact %s", sc.ID, bound.Value, r.Metrics, r.Items[0].Outcome, r.Facts[0].Outcome)
		}
		for _, kind := range []scenario.Kind{scenario.KindOpen, scenario.KindFact} {
			kept := score.Export{Items: []score.Item{itemOf(t, sc, kind, score.TagSuggested, f.Slots[0].Value+", "+bound.Value, f.ID)}}
			if r := score.Score(sc, kept); r.Metrics != perfect {
				t.Errorf("%s: the finding with %q, as %s, scores %+v", sc.ID, bound.Value, kind, r.Metrics)
			}
		}
	}
	if len(forms) != len(limitForms) {
		t.Errorf("the draw holds the forms %v, want all of %v", forms, limitForms)
	}
}

// TestHypothesisAsAnOpenQuestionNeedsNoHedge: a hypothesis asked as an
// open question with no hedge word holds, since the question is itself
// unsure; stated as a fact with no hedge word, or filed in a kind it does
// not permit with none, it is limit_dropped. Only the hypothesis leaves
// its limit unasked, and only of an open question: an open question about
// a finding of another form that drops its limit is limit_dropped.
func TestHypothesisAsAnOpenQuestionNeedsNoHedge(t *testing.T) {
	seen := 0
	for _, sc := range instances(t)["limit"] {
		f := sc.Facts[0]
		hypothesis := slices.Contains(hedges, f.Slots[1].Value)
		if want := []scenario.Kind(nil); hypothesis {
			want = []scenario.Kind{scenario.KindOpen}
			if !slices.Equal(f.Slots[1].Unasked, want) || f.Slots[0].Unasked != nil {
				t.Errorf("%s: the hypothesis leaves its slots unasked of %v and %v", sc.ID, f.Slots[0].Unasked, f.Slots[1].Unasked)
			}
		} else if f.Slots[0].Unasked != nil || f.Slots[1].Unasked != nil {
			t.Errorf("%s: a finding with %q leaves a slot unasked", sc.ID, f.Slots[1].Value)
		}
		asked := "Does the " + f.Slots[0].Value + " cache cause the slow start?"
		r := score.Score(sc, score.Export{Items: []score.Item{itemOf(t, sc, scenario.KindOpen, score.TagSuggested, asked, f.ID)}})
		if !hypothesis {
			if r.Metrics != (score.Metrics{WrongWrite: 1}) || r.Facts[0].Outcome != score.LimitDropped {
				t.Errorf("%s: an open question that drops %q scores %+v, fact %s", sc.ID, f.Slots[1].Value, r.Metrics, r.Facts[0].Outcome)
			}
			continue
		}
		seen++
		if r.Metrics != perfect || r.Items[0].Outcome != score.Supports || r.Facts[0].Outcome != score.Holds {
			t.Errorf("%s: the hypothesis asked as an open question with no hedge word scores %+v, item %s, fact %s", sc.ID, r.Metrics, r.Items[0].Outcome, r.Facts[0].Outcome)
		}
		for _, kind := range []scenario.Kind{scenario.KindFact, scenario.KindDecision, scenario.KindNext} {
			stated := "The " + f.Slots[0].Value + " cache causes the slow start"
			r := score.Score(sc, score.Export{Items: []score.Item{itemOf(t, sc, kind, score.TagSuggested, stated, f.ID)}})
			if r.Metrics != (score.Metrics{WrongWrite: 1}) || r.Items[0].Outcome != score.LimitDropped || r.Facts[0].Outcome != score.LimitDropped {
				t.Errorf("%s: the hypothesis as a %s with no hedge word scores %+v, item %s, fact %s", sc.ID, kind, r.Metrics, r.Items[0].Outcome, r.Facts[0].Outcome)
			}
		}
	}
	if seen == 0 {
		t.Error("the draw holds no hypothesis")
	}
}

// TestHeldOutShapes: one instance of each held-out family, from a seed set
// here, has the facts, slots, Musts and planting authors its family
// plants. No instance is written down.
func TestHeldOutShapes(t *testing.T) {
	const seed = 0x5eed_0f_1e1d
	planter := func(sc scenario.Scenario, id string) scenario.Turn {
		_, tr := plantedAt(t, sc, id)
		return tr
	}
	classes := func(f scenario.Fact) []scenario.SlotClass {
		var out []scenario.SlotClass
		for _, s := range f.Slots {
			out = append(out, s.Class)
		}
		return out
	}
	limitF, _ := ByName("limit")
	sc := limitF.Generate(seed)
	if len(sc.Facts) != 1 {
		t.Fatalf("limit plants %d facts", len(sc.Facts))
	}
	f := sc.Facts[0]
	tr := planter(sc, f.ID)
	if len(sc.Facts) != 1 || f.Must != scenario.MustStore || f.Authority != scenario.AuthorityOther ||
		!slices.Equal(classes(f), []scenario.SlotClass{scenario.SlotIdent, scenario.SlotLimit}) ||
		!slices.Contains(f.Kinds, scenario.KindOpen) || !slices.Contains(f.Kinds, scenario.KindFact) ||
		tr.Author != event.AuthorAgent || tr.Kind != event.KindReply {
		t.Errorf("limit: %+v, planted by %s %s", sc.Facts, tr.Author, tr.Kind)
	}

	unagreedF, _ := ByName("unagreed")
	sc = unagreedF.Generate(seed)
	var musts []scenario.Must
	var authors []event.Author
	for _, f := range sc.Facts {
		musts = append(musts, f.Must)
		authors = append(authors, planter(sc, f.ID).Author)
		if !slices.Equal(classes(f), []scenario.SlotClass{scenario.SlotIdent}) {
			t.Errorf("unagreed: %s has slots %v", f.ID, classes(f))
		}
	}
	lines := facts(sc, scenario.MustNotWithAuthority)
	own := facts(sc, scenario.MustStore)
	if len(lines) != 2 || len(own) != 1 || lines[0].Slots[0].Value != lines[1].Slots[0].Value || own[0].Slots[0].Value == lines[0].Slots[0].Value ||
		planter(sc, lines[0].ID).Author != event.AuthorAgent || planter(sc, lines[1].ID).Author != event.AuthorTyped ||
		planter(sc, own[0].ID).Author != event.AuthorTyped || own[0].Authority != scenario.AuthorityUser {
		t.Errorf("unagreed: Musts %v, by %v", musts, authors)
	}

	acceptedF, _ := ByName("accepted")
	sc = acceptedF.Generate(seed)
	if len(sc.Facts) != 3 {
		t.Fatalf("accepted plants %d facts", len(sc.Facts))
	}
	proposal, choice, never := sc.Facts[0], sc.Facts[1], sc.Facts[2]
	at := planter(sc, choice.ID)
	src, _ := plantedAt(t, sc, choice.ID)
	if proposal.Must != scenario.MustStore || proposal.Authority != scenario.AuthorityOther || planter(sc, proposal.ID).Author != event.AuthorAgent ||
		proposal.Until == nil || proposal.Until.Seq != src.Seq ||
		choice.Must != scenario.MustStore || choice.Authority != scenario.AuthorityUser || choice.Supersedes != proposal.ID ||
		!slices.Equal(choice.Kinds, []scenario.Kind{scenario.KindDecision, scenario.KindProcedure}) ||
		never.Must != scenario.MustNotAsRule || !slices.Contains(at.Plants, never.ID) || !reflect.DeepEqual(never.Slots, choice.Slots) ||
		at.Author != event.AuthorChosen || at.Kind != event.KindDialog && at.Kind != event.KindPlanDecision {
		t.Errorf("accepted: %+v, chosen by %s in a %s", sc.Facts, at.Author, at.Kind)
	}
	if _, err := scenario.V1Entries(sc.Sessions[0], sc.Sessions[0].Host); err == nil {
		t.Error("accepted renders as v1's entries")
	}
	if _, err := scenario.Events(sc.Sessions[0]); err != nil {
		t.Errorf("accepted does not render as events: %v", err)
	}
	// Both ways of accepting come up over a draw.
	drawn, _ := Draw("accepted", seed, 20)
	ways := map[event.Kind]bool{}
	for _, sc := range drawn {
		ways[planter(sc, sc.Facts[1].ID).Kind] = true
	}
	if !ways[event.KindDialog] || !ways[event.KindPlanDecision] {
		t.Errorf("a draw accepts by %v", ways)
	}
}
