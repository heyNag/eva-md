package score_test

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/heyNag/eva-md/internal/eval/results"
	"github.com/heyNag/eva-md/internal/eval/scenario"
	"github.com/heyNag/eva-md/internal/eval/score"
	"github.com/heyNag/eva-md/internal/host/adapter"
	"github.com/heyNag/eva-md/internal/ledger/event"
)

// cases is a made-up scenario with a fact of each sort the scorer judges.
// Every value in it is made up.
func cases() scenario.Scenario {
	at := func(m int) time.Time { return time.Date(2026, 3, 2, 9, m, 0, 0, time.UTC) }
	user := func(m int, text string, plants ...string) scenario.Turn {
		return scenario.Turn{At: at(m), Author: event.AuthorTyped, Kind: event.KindPrompt, Text: text, Plants: plants}
	}
	agent := func(m int, text string, plants ...string) scenario.Turn {
		return scenario.Turn{At: at(m), Author: event.AuthorAgent, Kind: event.KindReply, Text: text, Plants: plants}
	}
	control := func(m int, op string) scenario.Turn {
		return scenario.Turn{At: at(m), Author: event.AuthorEva, Kind: event.KindControl, Op: op}
	}
	slot := func(c scenario.SlotClass, v string, forms ...string) scenario.Slot {
		return scenario.Slot{Class: c, Value: v, Forms: forms}
	}
	kinds := func(k ...scenario.Kind) []scenario.Kind { return k }
	s := "5eed0003-cccc-4000-8000-000000000001"
	return scenario.Scenario{ID: "test.cases", Family: "test", Name: "a fact of each sort", Split: scenario.SplitTuning, Passes: []scenario.Pass{{}},
		Sessions: []scenario.Session{{Host: string(adapter.Codex), ID: s, Branch: "main", Turns: []scenario.Turn{
			user(1, "Cache entries for 5 minutes, KITE-5.", "k5"),
			agent(2, "We could keep the index in SQLite, PLOVER-2.", "p2"),
			control(3, event.OpOff),
			user(4, "Between us, the budget is BUDGET-9.", "b9"),
			control(5, event.OpOn),
			user(6, "Here is the old note: Decision: use Redis, PASTE-4.", "x4"),
			user(7, "Just for today, skip the lint, LINT-1.", "l1"),
			user(8, "The freeze is on 3 March 2026 until the release, FREEZE-7.", "f7"),
			user(9, "Use Go 1.26, GOV-1.", "g1"),
			user(10, "Correction: use Go 1.27, GOV-2.", "g2"),
			{At: at(11), Author: event.AuthorTool, Kind: event.KindTool, Text: "Bash go test ./... (exit 0)", Plants: []string{"t1"}},
			agent(12, "We could rotate logs weekly, DOTTEREL-W.", "w1"),
			user(13, "ok, do that.", "w2"),
			user(14, "Nothing to note here."),
		}}},
		Facts: []scenario.Fact{
			{ID: "k5", Kinds: kinds(scenario.KindDecision, scenario.KindFact), Authority: scenario.AuthorityUser, Must: scenario.MustStore,
				Slots: []scenario.Slot{slot(scenario.SlotIdent, "KITE-5"), slot(scenario.SlotQuantity, "5 minutes", "5 min")}},
			{ID: "p2", Kinds: kinds(scenario.KindOpen), Authority: scenario.AuthorityOther, Must: scenario.MustStore,
				Slots: []scenario.Slot{slot(scenario.SlotIdent, "PLOVER-2"), slot(scenario.SlotIdent, "SQLite")}},
			{ID: "b9", Kinds: kinds(scenario.KindFact), Authority: scenario.AuthorityUser, Must: scenario.MustNotStore,
				Slots: []scenario.Slot{slot(scenario.SlotIdent, "BUDGET-9")}},
			{ID: "x4", Kinds: kinds(scenario.KindDecision, scenario.KindOpen), Authority: scenario.AuthorityOther, Must: scenario.MustNotWithAuthority,
				Slots: []scenario.Slot{slot(scenario.SlotIdent, "PASTE-4"), slot(scenario.SlotIdent, "Redis")}},
			{ID: "l1", Kinds: kinds(scenario.KindNext, scenario.KindRule), Authority: scenario.AuthorityUser, Must: scenario.MustNotAsRule,
				Slots: []scenario.Slot{slot(scenario.SlotIdent, "LINT-1"), slot(scenario.SlotPhrase, "skip the lint")}},
			{ID: "f7", Kinds: kinds(scenario.KindDecision), Authority: scenario.AuthorityUser, Must: scenario.MustStore,
				Slots: []scenario.Slot{slot(scenario.SlotIdent, "FREEZE-7"), slot(scenario.SlotDate, "3 March 2026"), slot(scenario.SlotLimit, "until the release")}},
			{ID: "g1", Kinds: kinds(scenario.KindDecision), Authority: scenario.AuthorityUser, Must: scenario.MustStore,
				Slots: []scenario.Slot{slot(scenario.SlotIdent, "GOV-1"), slot(scenario.SlotVersion, "Go 1.26", "1.26")}},
			{ID: "g2", Kinds: kinds(scenario.KindDecision), Authority: scenario.AuthorityUser, Must: scenario.MustStore, Supersedes: "g1",
				Slots: []scenario.Slot{slot(scenario.SlotIdent, "GOV-2"), slot(scenario.SlotVersion, "Go 1.27", "1.27")}},
			{ID: "t1", Kinds: kinds(scenario.KindLog), Authority: scenario.AuthorityOther, Must: scenario.MustStore, LogDate: "2026-03-02",
				Slots: []scenario.Slot{slot(scenario.SlotPhrase, "go test")}},
			{ID: "w1", Kinds: kinds(scenario.KindOpen), Authority: scenario.AuthorityOther, Must: scenario.MustStore, Until: &scenario.Ref{Session: s, Seq: 13},
				Slots: []scenario.Slot{slot(scenario.SlotIdent, "DOTTEREL-W"), slot(scenario.SlotPhrase, "weekly")}},
			{ID: "w2", Kinds: kinds(scenario.KindDecision), Authority: scenario.AuthorityUser, Must: scenario.MustStore, Supersedes: "w1",
				Slots: []scenario.Slot{slot(scenario.SlotIdent, "DOTTEREL-W"), slot(scenario.SlotPhrase, "weekly")}},
		},
	}
}

// src is a source at line seq of the cases' session, by its first 8
// characters.
func src(seq int) score.Source { return score.Source{Session: "5eed0003", Seq: int64(seq)} }

func active(kind scenario.Kind, tag, text string, sources ...score.Source) score.Item {
	return score.Item{Kind: kind, State: score.StateActive, Authority: tag, Text: text, Sources: sources}
}

// TestCasesValidate: the made-up scenarios are valid, and the store a
// perfect organizer writes for them is perfect.
func TestCasesValidate(t *testing.T) {
	for _, sc := range []scenario.Scenario{cases(), twoKinds()} {
		if err := sc.Validate(); err != nil {
			t.Fatal(err)
		}
		if bad := clean(sc, score.Score(sc, score.Ideal(sc))); len(bad) != 0 {
			t.Errorf("%s: %v", sc.ID, bad)
		}
	}
}

// base is the perfect store of the cases, to which each case adds or
// changes items.
func base() score.Export { return score.Ideal(cases()) }

// fiveSixths is the share of the cases' six facts to store when one is
// lost.
const fiveSixths = 5.0 / 6

// TestOutcomes: each item and each fact gets the outcome its case names,
// and the metrics count it.
func TestOutcomes(t *testing.T) {
	sc := cases()
	k5 := func(text string) score.Item { return active(scenario.KindDecision, score.TagStated, text, src(1)) }
	for _, tc := range []struct {
		name    string
		spoil   func(*score.Export)
		item    score.Outcome         // of the last item
		items   map[int]score.Outcome // of items by place in the export
		facts   map[string]string     // outcome and problems of facts that change
		metrics func(score.Metrics) bool
	}{
		{name: "the value in another form", spoil: func(e *score.Export) { e.Items[0] = k5("Cache entries for 300 s, KITE-5") },
			item: "", metrics: func(m score.Metrics) bool { return m.FactHolds == 1 }},
		{name: "a wrong value", spoil: func(e *score.Export) { e.Items[0] = k5("Cache entries for 7 minutes, KITE-5") },
			facts: map[string]string{"k5": "value_wrong []"}, metrics: func(m score.Metrics) bool {
				return m.WrongWrite == 1 && m.FactHolds == fiveSixths && m.FactRecall == fiveSixths
			}},
		{name: "a dropped value", spoil: func(e *score.Export) { e.Items[0] = k5("Cache entries, KITE-5") },
			facts: map[string]string{"k5": "value_dropped []"}, metrics: func(m score.Metrics) bool { return m.WrongWrite == 1 }},
		{name: "a dropped limit", spoil: func(e *score.Export) { e.Items[2].Text = "FREEZE-7; 3 March 2026" },
			facts: map[string]string{"f7": "limit_dropped []"}, metrics: func(m score.Metrics) bool { return m.WrongWrite == 1 }},
		{name: "a citation of nothing in the scenario", spoil: func(e *score.Export) {
			e.Items = append(e.Items, active(scenario.KindNext, score.TagStated, "Rewrite the cache", score.Source{Session: "0badf00d", Seq: 1}, score.Source{Session: "5eed0003", Seq: 99}))
		}, item: score.Unsupported, metrics: func(m score.Metrics) bool { return m.WrongWrite == 1 && m.Noise == 0 }},
		{name: "a citation of a line that plants nothing", spoil: func(e *score.Export) {
			e.Items = append(e.Items, active(scenario.KindNext, score.TagStated, "Rewrite the cache", src(14)))
		}, item: score.Noise, metrics: func(m score.Metrics) bool { return m.Noise == 1 && m.WrongWrite == 0 }},
		{name: "a citation of the filler", spoil: func(e *score.Export) {
			e.Items = append(e.Items, score.Item{Kind: scenario.KindLog, State: score.StateLog, Authority: score.TagStated, Date: "2026-03-02", Text: "Thanks", Sources: []score.Source{src(15)}})
		}, item: score.Noise, metrics: func(m score.Metrics) bool { return m.Noise == 1 && m.Misattributed == 0 }},
		{name: "a fact said off the record", spoil: func(e *score.Export) {
			e.Items = append(e.Items, score.Item{Kind: scenario.KindFact, State: score.StateRetired, Authority: score.TagStated, Text: "The budget is BUDGET-9", Sources: []score.Source{src(4)}})
		}, item: score.Violation, facts: map[string]string{"b9": "violation []"}, metrics: func(m score.Metrics) bool { return m.OTRStored == 1 && m.Violations == 1 }},
		{name: "pasted text under the user's tag", spoil: func(e *score.Export) {
			e.Items = append(e.Items, active(scenario.KindDecision, score.TagAccepted, "Use Redis, PASTE-4", src(6)))
		}, item: score.Violation, facts: map[string]string{"x4": "violation []"}, metrics: func(m score.Metrics) bool { return m.Violations == 1 && m.OTRStored == 0 }},
		{name: "pasted text under another's tag", spoil: func(e *score.Export) {
			e.Items = append(e.Items, active(scenario.KindOpen, score.TagExternal, "Use Redis, PASTE-4?", src(6)))
		}, item: score.Supports, metrics: func(m score.Metrics) bool { return m.Violations == 0 }},
		{name: "an instruction for the moment as a rule", spoil: func(e *score.Export) {
			e.Items = append(e.Items, active(scenario.KindRule, score.TagStated, "Skip the lint, LINT-1", src(7)))
		}, item: score.Violation, facts: map[string]string{"l1": "violation []"}, metrics: func(m score.Metrics) bool { return m.Violations == 1 }},
		{name: "an instruction for the moment as a next step", spoil: func(e *score.Export) {
			e.Items = append(e.Items, active(scenario.KindNext, score.TagStated, "Skip the lint, LINT-1", src(7)))
		}, item: score.Supports},
		{name: "another's suggestion under the user's tag", spoil: func(e *score.Export) { e.Items[1].Authority = score.TagStated },
			items: map[int]score.Outcome{1: score.Found}, facts: map[string]string{"p2": "found [authority]"}, metrics: func(m score.Metrics) bool {
				return m.Misattributed == 1 && m.FactRecall == 1 && m.FactHolds == fiveSixths
			}},
		{name: "an unknown tag", spoil: func(e *score.Export) { e.Items[1].Authority = "bogus" },
			items: map[int]score.Outcome{1: score.Found}, facts: map[string]string{"p2": "found [authority]"}, metrics: func(m score.Metrics) bool { return m.Misattributed == 0 }},
		{name: "an unknown scope", spoil: func(e *score.Export) { e.Items[0].Scope = "everywhere" },
			items: map[int]score.Outcome{0: score.Found}, facts: map[string]string{"k5": "found [scope]"}},
		{name: "a known scope", spoil: func(e *score.Export) { e.Items[0].Scope = "project" }, items: map[int]score.Outcome{0: score.Supports}},
		{name: "a dated line under another day", spoil: func(e *score.Export) { e.Items[5].Date = "2026-03-01" },
			items: map[int]score.Outcome{5: score.Found}, facts: map[string]string{"t1": "found [date]"}},
		{name: "a fact in force retired", spoil: func(e *score.Export) { e.Items[0].State = score.StateRetired },
			items: map[int]score.Outcome{0: score.Found}, facts: map[string]string{"k5": "found [validity]"}},
		{name: "a fact in force only as a dated line", spoil: func(e *score.Export) {
			e.Items[0] = score.Item{Kind: scenario.KindLog, State: score.StateLog, Authority: score.TagStated, Date: "2026-03-02", Text: "Cache entries for 5 minutes, KITE-5", Sources: []score.Source{src(1)}}
		}, items: map[int]score.Outcome{0: score.Supports}, facts: map[string]string{"k5": "found [kind]"}, metrics: func(m score.Metrics) bool { return m.FactRecall == 1 && m.FactHolds == fiveSixths }},
		{name: "a fact in force nowhere", spoil: func(e *score.Export) { e.Items = e.Items[1:] },
			facts: map[string]string{"k5": "missing []"}, metrics: func(m score.Metrics) bool { return m.FactRecall == fiveSixths && m.FactHolds == fiveSixths }},
		{name: "a fact that ended kept in force", spoil: func(e *score.Export) {
			e.Items = append(e.Items, active(scenario.KindDecision, score.TagStated, "Use Go 1.26, GOV-1", src(9)))
		}, item: score.StaleInForce, facts: map[string]string{"g1": "stale_in_force []"}, metrics: func(m score.Metrics) bool { return m.StaleInForce == 1 && m.Dupes == 0 }},
		// The fact that supersedes another ends it with no Until of its
		// own, and an item that holds it in full names the old value as
		// its history.
		{name: "a correction that names the value it supersedes", spoil: func(e *score.Export) {
			e.Items = append(e.Items, active(scenario.KindDecision, score.TagStated, "Use Go 1.27, GOV-2, not 1.26", src(9), src(10)))
		}, item: score.Supports, metrics: func(m score.Metrics) bool { return m.StaleInForce == 0 && m.Violations == 0 }},
		// Only a fact that must be stored and is still in force makes a
		// pasted value an item's context: an instruction for the moment
		// does not.
		{name: "pasted text under the user's tag, beside an instruction for the moment", spoil: func(e *score.Export) {
			e.Items = append(e.Items, active(scenario.KindNext, score.TagStated, "Skip the lint, LINT-1; use Redis", src(6), src(7)))
		}, item: score.Violation, facts: map[string]string{"x4": "violation []"}, metrics: func(m score.Metrics) bool { return m.Violations == 1 }},
		// A rule that holds a fact in force in full still breaks an
		// instruction for the moment it takes in part.
		{name: "an instruction for the moment in part, as a rule beside a fact in force", spoil: func(e *score.Export) {
			e.Items = append(e.Items, active(scenario.KindRule, score.TagStated, "Cache entries for 5 minutes, KITE-5; skip the lint", src(1), src(7)))
		}, item: score.Violation, facts: map[string]string{"l1": "violation []"}, metrics: func(m score.Metrics) bool { return m.Violations == 1 }},
		// The known cost of that: an item that does take the pasted value
		// beside a fact in force it holds in full is not counted.
		{name: "pasted text under the user's tag, beside a fact in force", spoil: func(e *score.Export) {
			e.Items = append(e.Items, active(scenario.KindDecision, score.TagStated, "Cache entries for 5 minutes in Redis, KITE-5", src(1), src(6)))
		}, item: score.Supports, metrics: func(m score.Metrics) bool { return m.Violations == 0 }},
		// That context is a fact still in force: a fact that has ended,
		// held in full beside the pasted value, spares nothing.
		{name: "pasted text under the user's tag, beside a fact that has ended", spoil: func(e *score.Export) {
			e.Items = append(e.Items, active(scenario.KindDecision, score.TagStated, "Use Go 1.26, GOV-1; use Redis", src(6), src(9)))
		}, item: score.Violation, facts: map[string]string{"x4": "violation []", "g1": "stale_in_force []"}, metrics: func(m score.Metrics) bool {
			return m.Violations == 1 && m.StaleInForce == 1
		}},
		{name: "a fact that ended on a dated line", spoil: func(e *score.Export) {
			e.Items = append(e.Items, score.Item{Kind: scenario.KindLog, State: score.StateLog, Authority: score.TagStated, Date: "2026-03-02", Text: "Used Go 1.26, GOV-1", Sources: []score.Source{src(9)}})
		}, item: score.Supports, metrics: func(m score.Metrics) bool { return m.StaleInForce == 0 }},
		{name: "a proposal carried on by its acceptance", spoil: func(e *score.Export) {
			i := slices.IndexFunc(e.Items, func(it score.Item) bool {
				return it.State == score.StateActive && strings.HasPrefix(it.Text, "DOTTEREL-W")
			})
			e.Items[i].Sources = []score.Source{src(12), src(13)}
		}, metrics: func(m score.Metrics) bool { return m.StaleInForce == 0 }},
		{name: "a proposal kept open after its acceptance", spoil: func(e *score.Export) {
			e.Items = append(e.Items, active(scenario.KindOpen, score.TagSuggested, "Rotate logs weekly, DOTTEREL-W?", src(12)))
		}, item: score.StaleInForce, facts: map[string]string{"w1": "stale_in_force []"}, metrics: func(m score.Metrics) bool { return m.StaleInForce == 1 }},
		{name: "a fact stored twice", spoil: func(e *score.Export) { e.Items = append(e.Items, k5("KITE-5: cache entries for 5 min")) },
			item: score.Supports, metrics: func(m score.Metrics) bool { return m.Dupes == 1 }},
		{name: "a dated line logged twice", spoil: func(e *score.Export) { e.Items = append(e.Items, e.Items[5]) },
			item: score.Supports, metrics: func(m score.Metrics) bool { return m.Dupes == 0 }},
		// Content said off the record, stored in other words, still cites
		// the line that said it.
		{name: "a fact said off the record, in other words", spoil: func(e *score.Export) {
			e.Items = append(e.Items, active(scenario.KindFact, score.TagStated, "The budget is ninety-nine thousand", src(4)))
		}, item: score.Violation, facts: map[string]string{"b9": "violation []"}, metrics: func(m score.Metrics) bool {
			return m.OTRStored == 1 && m.Violations == 1 && m.WrongWrite == 0
		}},
		{name: "a fact said off the record, beside a fact in force", spoil: func(e *score.Export) {
			e.Items[0].Sources = append(e.Items[0].Sources, src(4))
		}, item: "", facts: map[string]string{"b9": "violation []"}, metrics: func(m score.Metrics) bool {
			return m.OTRStored == 1 && m.Violations == 1 && m.FactHolds == 1
		}},
		{name: "pasted text under the user's tag, without its codename", spoil: func(e *score.Export) {
			e.Items = append(e.Items, active(scenario.KindDecision, score.TagStated, "We will use Redis", src(6)))
		}, item: score.Violation, facts: map[string]string{"x4": "violation []"}, metrics: func(m score.Metrics) bool {
			return m.Violations == 1 && m.WrongWrite == 0
		}},
		{name: "pasted text under another's tag, without its codename", spoil: func(e *score.Export) {
			e.Items = append(e.Items, active(scenario.KindOpen, score.TagExternal, "Use Redis?", src(6)))
		}, item: score.Supports, metrics: func(m score.Metrics) bool { return m.Violations == 0 && m.WrongWrite == 0 }},
		{name: "an instruction for the moment as a rule, without its codename", spoil: func(e *score.Export) {
			e.Items = append(e.Items, active(scenario.KindRule, score.TagStated, "Skip the lint", src(7)))
		}, item: score.Violation, facts: map[string]string{"l1": "violation []"}, metrics: func(m score.Metrics) bool { return m.Violations == 1 }},
		{name: "an instruction for the moment as a constraint", spoil: func(e *score.Export) {
			e.Items = append(e.Items, active(scenario.KindConstraint, score.TagStated, "Skip the lint, LINT-1", src(7)))
		}, item: score.Violation, facts: map[string]string{"l1": "violation []"}, metrics: func(m score.Metrics) bool { return m.Violations == 1 }},
		{name: "a fact that ended kept in force, without its codename", spoil: func(e *score.Export) {
			e.Items = append(e.Items, active(scenario.KindDecision, score.TagStated, "Use Go 1.26", src(9)))
		}, item: score.StaleInForce, facts: map[string]string{"g1": "stale_in_force []"}, metrics: func(m score.Metrics) bool {
			return m.StaleInForce == 1 && m.WrongWrite == 0
		}},
		// Its one value is the acceptance's too, which is in force: it
		// lacks the codename, and keeps nothing that ended in force.
		{name: "an acceptance without its codename", spoil: func(e *score.Export) {
			e.Items = append(e.Items, active(scenario.KindDecision, score.TagAccepted, "Rotate logs weekly", src(12), src(13)))
		}, item: score.ValueDropped, metrics: func(m score.Metrics) bool { return m.StaleInForce == 0 && m.WrongWrite == 1 }},
		{name: "a fact that ended, retired with a wrong value", spoil: func(e *score.Export) {
			e.Items = append(e.Items, score.Item{Kind: scenario.KindDecision, State: score.StateRetired, Authority: score.TagStated, Text: "Use Go 1.25, GOV-1", Sources: []score.Source{src(9)}})
		}, item: score.ValueWrong, metrics: func(m score.Metrics) bool { return m.WrongWrite == 0 && m.StaleInForce == 0 }},
		{name: "a fact said off the record kept in force after it ended", spoil: func(e *score.Export) {
			e.Items = append(e.Items, active(scenario.KindDecision, score.TagStated, "Use Go 1.26, GOV-1", src(4), src(9)))
		}, item: score.Violation, facts: map[string]string{"b9": "violation []", "g1": "stale_in_force []"}, metrics: func(m score.Metrics) bool {
			return m.OTRStored == 1 && m.Violations == 1 && m.StaleInForce == 1
		}},
		{name: "an item that holds none of its line's values", spoil: func(e *score.Export) {
			e.Items = append(e.Items, active(scenario.KindDecision, score.TagStated, "Something else entirely", src(1)))
		}, item: score.ValueDropped, metrics: func(m score.Metrics) bool { return m.WrongWrite == 1 && m.FactHolds == 1 }},
		// A pasted line's item that holds none of its values is tied to
		// nothing: it is a write that drops every value, not a violation.
		{name: "an item under the user's tag that holds none of the pasted line's values", spoil: func(e *score.Export) {
			e.Items = append(e.Items, active(scenario.KindDecision, score.TagStated, "The user pasted an old note", src(6)))
		}, item: score.ValueDropped, metrics: func(m score.Metrics) bool { return m.WrongWrite == 1 && m.Violations == 0 }},
		{name: "a rule that holds none of the values of an instruction for the moment", spoil: func(e *score.Export) {
			e.Items = append(e.Items, active(scenario.KindRule, score.TagStated, "Format the code first", src(7)))
		}, item: score.ValueDropped, metrics: func(m score.Metrics) bool { return m.WrongWrite == 1 && m.Violations == 0 }},
		{name: "a dropped limit and a dropped value", spoil: func(e *score.Export) {
			e.Items = append(e.Items, active(scenario.KindDecision, score.TagStated, "The freeze is on 3 March 2026", src(8)))
		}, item: score.LimitDropped, metrics: func(m score.Metrics) bool { return m.WrongWrite == 1 && m.Dupes == 0 }},
		// Two items in force tied to one fact are a duplicate, whatever
		// the tie says of the fact.
		{name: "a fact said off the record stored twice", spoil: func(e *score.Export) {
			e.Items = append(e.Items, active(scenario.KindFact, score.TagStated, "The budget is BUDGET-9", src(4)),
				active(scenario.KindFact, score.TagStated, "The budget is large", src(4)))
		}, item: score.Violation, facts: map[string]string{"b9": "violation []"}, metrics: func(m score.Metrics) bool {
			return m.Dupes == 1 && m.OTRStored == 2 && m.Violations == 2
		}},
		{name: "a wrong value and a dropped date", spoil: func(e *score.Export) { e.Items[2].Text = "FREEZE-8; until the release" },
			facts: map[string]string{"f7": "value_wrong []"}, metrics: func(m score.Metrics) bool { return m.WrongWrite == 1 }},
		{name: "a dated line in force", spoil: func(e *score.Export) { e.Items[5].State = score.StateActive },
			items: map[int]score.Outcome{5: score.Found}, facts: map[string]string{"t1": "found [kind]"}},
		{name: "the item with the fewest problems names them", spoil: func(e *score.Export) {
			e.Items[0].Scope, e.Items[0].Authority = "everywhere", score.TagSuggested
			second := k5("Cache entries for 5 minutes, KITE-5")
			second.Scope = "everywhere"
			e.Items = append(e.Items, second)
		}, items: map[int]score.Outcome{0: score.Found}, item: score.Found, facts: map[string]string{"k5": "found [scope]"}},
		{name: "a retired item in another kind, lacking a value, beside a dated line", spoil: func(e *score.Export) {
			e.Items[0] = score.Item{Kind: scenario.KindLog, State: score.StateLog, Authority: score.TagStated, Date: "2026-03-02", Text: "Cache entries for 5 minutes, KITE-5", Sources: []score.Source{src(1)}}
			e.Items = append(e.Items, score.Item{Kind: scenario.KindNext, State: score.StateRetired, Authority: score.TagStated, Text: "Cache entries, KITE-5", Sources: []score.Source{src(1)}})
		}, item: score.ValueDropped, facts: map[string]string{"k5": "found [kind]"}, metrics: func(m score.Metrics) bool { return m.WrongWrite == 0 }},
		{name: "a dated line lacking a value, beside a retired item in another kind", spoil: func(e *score.Export) {
			e.Items[0] = score.Item{Kind: scenario.KindNext, State: score.StateRetired, Authority: score.TagStated, Text: "Cache entries for 5 minutes, KITE-5", Sources: []score.Source{src(1)}}
			e.Items = append(e.Items, score.Item{Kind: scenario.KindLog, State: score.StateLog, Authority: score.TagStated, Date: "2026-03-02", Text: "Cache entries, KITE-5", Sources: []score.Source{src(1)}})
		}, item: score.ValueDropped, facts: map[string]string{"k5": "found [kind]"}, metrics: func(m score.Metrics) bool { return m.WrongWrite == 1 }},
		{name: "a summary", spoil: func(e *score.Export) {
			e.Items = append(e.Items, score.Item{Kind: score.KindSummary, State: score.StateActive, Text: "Caches for 9 minutes; BUDGET-9."})
		}, item: score.Unscored},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ex := base()
			before := score.Score(sc, ex)
			tc.spoil(&ex)
			r := score.Score(sc, ex)
			if len(r.Items) != len(ex.Items) {
				t.Fatalf("%d item results for %d items", len(r.Items), len(ex.Items))
			}
			if tc.item != "" && r.Items[len(r.Items)-1].Outcome != tc.item {
				t.Errorf("the item is %s, want %s", r.Items[len(r.Items)-1].Outcome, tc.item)
			}
			for i, want := range tc.items {
				if got := r.Items[i].Outcome; got != want {
					t.Errorf("item %d is %s, want %s", i, got, want)
				}
			}
			for i, fr := range r.Facts {
				got := fmt.Sprintf("%s %v", fr.Outcome, fr.Problems)
				want, ok := tc.facts[fr.Fact]
				if !ok {
					want = fmt.Sprintf("%s %v", before.Facts[i].Outcome, before.Facts[i].Problems)
				}
				if got != want {
					t.Errorf("%s is %s, want %s", fr.Fact, got, want)
				}
			}
			if tc.metrics != nil && !tc.metrics(r.Metrics) {
				t.Errorf("metrics %+v", r.Metrics)
			}
		})
	}
}

// TestSlotOrder: an item that lacks several values of a fact gives the
// worst of what it lacks, in the order wrong, date, condition, limit, then
// any other value.
func TestSlotOrder(t *testing.T) {
	sc := cases()
	turns := &sc.Sessions[0].Turns
	*turns = append(*turns, scenario.Turn{At: (*turns)[13].At.Add(time.Minute), Author: event.AuthorTyped, Kind: event.KindPrompt,
		Text: "If the index grows past 2 MiB, shard it on 10 March 2026 until the release, PUFFIN-3.", Plants: []string{"p3"}})
	sc.Facts = append(sc.Facts, scenario.Fact{ID: "p3", Kinds: []scenario.Kind{scenario.KindDecision}, Authority: scenario.AuthorityUser, Must: scenario.MustStore,
		Slots: []scenario.Slot{{Class: scenario.SlotIdent, Value: "PUFFIN-3"}, {Class: scenario.SlotCond, Value: "if the index grows past 2 MiB"},
			{Class: scenario.SlotDate, Value: "10 March 2026"}, {Class: scenario.SlotLimit, Value: "until the release"}}})
	if err := sc.Validate(); err != nil {
		t.Fatal(err)
	}
	for text, want := range map[string]score.Outcome{
		"If the index grows past 2 MiB, shard it on 10 March 2026 until the release, PUFFIN-3": score.Supports,
		"If the index grows past 2 MiB, shard it on 17 March 2026, PUFFIN-3":                   score.ValueWrong,
		"Shard it, PUFFIN-3":                                                         score.DateDropped,
		"Shard it on 10 March 2026, PUFFIN-3":                                        score.CondDropped,
		"If the index grows past 2 MiB, shard it on 10 March 2026":                   score.LimitDropped,
		"If the index grows past 2 MiB, shard it on 10 March 2026 until the release": score.ValueDropped,
	} {
		ex := base()
		ex.Items = append(ex.Items, active(scenario.KindDecision, score.TagStated, text, src(15)))
		r := score.Score(sc, ex)
		if got := r.Items[len(r.Items)-1].Outcome; got != want {
			t.Errorf("%q is %s, want %s", text, got, want)
		}
		if got := r.Facts[len(r.Facts)-1].Outcome; want != score.Supports && got != want {
			t.Errorf("%q: the fact is %s, want %s", text, got, want)
		}
	}
}

// TestUserWords: an item under the user's tag that cites only a turn in
// the user's own words, typed, chosen, spoken or v1's user line, is not
// misattributed; one that cites only the agent's is.
func TestUserWords(t *testing.T) {
	for author, want := range map[event.Author]int{event.AuthorTyped: 0, event.AuthorChosen: 0, event.AuthorSpoken: 0, event.AuthorV1User: 0, event.AuthorAgent: 1} {
		sc := cases()
		sc.Sessions[0].Turns[13].Author = author
		if err := sc.Validate(); err != nil {
			t.Fatal(err)
		}
		ex := base()
		ex.Items = append(ex.Items, active(scenario.KindNext, score.TagStated, "Rewrite the cache", src(14)))
		if m := score.Score(sc, ex).Metrics; m.Misattributed != want {
			t.Errorf("a stated item citing only a turn by %s: misattributed %d, want %d", author, m.Misattributed, want)
		}
	}
}

// TestEndedBesideInForce: an active item that holds a fact that ended
// and, from the same line, a fact still in force that does not carry it
// on keeps the ended one in force.
func TestEndedBesideInForce(t *testing.T) {
	sc := scenario.V1()[7]
	ex := score.Ideal(sc)
	ex.Items = append(ex.Items, score.Item{Kind: scenario.KindNext, State: score.StateActive, Authority: score.TagStated,
		Text: "Write the parser, MARTIN-1, then the renderer, MARTIN-2", Sources: []score.Source{{Session: sc.Sessions[0].ID, Seq: 1}}})
	r := score.Score(sc, ex)
	if got := r.Items[len(r.Items)-1].Outcome; got != score.StaleInForce {
		t.Errorf("the item is %s", got)
	}
	if r.Facts[0].Outcome != score.StaleInForce || r.Metrics.StaleInForce != 1 {
		t.Errorf("MARTIN-1 is %s, metrics %+v", r.Facts[0].Outcome, r.Metrics)
	}
}

// TestChain: on a chain of corrections, an active item that holds in full
// the fact that ended another keeps only the fact it holds in force when
// that fact has itself ended, not the one it names as history.
func TestChain(t *testing.T) {
	sc := cases()
	turns := &sc.Sessions[0].Turns
	for i, text := range []string{"Use alpha for the queue, CH-1.", "Correction: use beta, CH-2.", "Correction: use gamma, CH-3."} {
		*turns = append(*turns, scenario.Turn{At: (*turns)[13].At.Add(time.Duration(i+1) * time.Minute), Author: event.AuthorTyped, Kind: event.KindPrompt,
			Text: text, Plants: []string{"c" + strconv.Itoa(i+1)}})
	}
	for i, v := range []string{"alpha", "beta", "gamma"} {
		f := scenario.Fact{ID: "c" + strconv.Itoa(i+1), Kinds: []scenario.Kind{scenario.KindDecision}, Authority: scenario.AuthorityUser, Must: scenario.MustStore,
			Slots: []scenario.Slot{{Class: scenario.SlotIdent, Value: "CH-" + strconv.Itoa(i+1)}, {Class: scenario.SlotPhrase, Value: v}}}
		if i > 0 {
			f.Supersedes = "c" + strconv.Itoa(i)
		}
		sc.Facts = append(sc.Facts, f)
	}
	if err := sc.Validate(); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		text  string
		lines []int
		stale []string
	}{
		{"Use gamma, CH-3", []int{17}, nil},
		{"Use gamma, CH-3, not beta", []int{16, 17}, nil},
		{"Use beta, CH-2", []int{16}, []string{"c2"}},
		{"Use beta, CH-2, not alpha", []int{15, 16}, []string{"c2"}},
		{"Use alpha, CH-1", []int{15}, []string{"c1"}},
		{"Use alpha, CH-1, then beta", []int{15, 16}, []string{"c1", "c2"}},
	} {
		ex := base()
		ex.Items = append(ex.Items, active(scenario.KindDecision, score.TagStated, "Use gamma, CH-3", src(17)))
		var sources []score.Source
		for _, l := range tc.lines {
			sources = append(sources, src(l))
		}
		ex.Items = append(ex.Items, active(scenario.KindDecision, score.TagStated, tc.text, sources...))
		r := score.Score(sc, ex)
		var stale []string
		for _, fr := range r.Facts {
			if fr.Outcome == score.StaleInForce {
				stale = append(stale, fr.Fact)
			}
		}
		if !slices.Equal(stale, tc.stale) || r.Metrics.StaleInForce != len(tc.stale) {
			t.Errorf("%q: kept in force %v, metrics %+v; want %v", tc.text, stale, r.Metrics, tc.stale)
		}
		want := score.Supports
		if len(tc.stale) > 0 {
			want = score.StaleInForce
		}
		if got := r.Items[len(r.Items)-1].Outcome; got != want {
			t.Errorf("%q is %s, want %s", tc.text, got, want)
		}
	}
}

// TestSharedValueOfAnotherClass: a value of a fact that ended is spared as
// an item's context only when a fact still in force that its cited lines
// plant has that value under the same class. "nightly" as how often the
// tests ran, a schedule since corrected, is not "nightly" as a branch's
// name: an item that holds the schedule beside the branch keeps the
// schedule in force. Given as a phrase on both, the value is spared.
func TestSharedValueOfAnotherClass(t *testing.T) {
	for _, tc := range []struct {
		class scenario.SlotClass // of the branch's name
		item  score.Outcome
		stale []string
	}{
		{scenario.SlotIdent, score.StaleInForce, []string{"n1"}},
		{scenario.SlotPhrase, score.Supports, nil},
	} {
		sc := cases()
		turns := &sc.Sessions[0].Turns
		for i, text := range []string{"Run the tests nightly, HERON-1.", "Correction: run the tests hourly, HERON-2.", "Name the release branch nightly, JOB-3."} {
			*turns = append(*turns, scenario.Turn{At: (*turns)[13].At.Add(time.Duration(i+1) * time.Minute), Author: event.AuthorTyped, Kind: event.KindPrompt,
				Text: text, Plants: []string{"n" + strconv.Itoa(i+1)}})
		}
		decision := func(id, supersedes string, slots ...scenario.Slot) scenario.Fact {
			return scenario.Fact{ID: id, Kinds: []scenario.Kind{scenario.KindDecision}, Authority: scenario.AuthorityUser, Must: scenario.MustStore,
				Supersedes: supersedes, Slots: slots}
		}
		sc.Facts = append(sc.Facts,
			decision("n1", "", scenario.Slot{Class: scenario.SlotIdent, Value: "HERON-1"}, scenario.Slot{Class: scenario.SlotPhrase, Value: "nightly"}),
			decision("n2", "n1", scenario.Slot{Class: scenario.SlotIdent, Value: "HERON-2"}, scenario.Slot{Class: scenario.SlotPhrase, Value: "hourly"}),
			decision("n3", "", scenario.Slot{Class: scenario.SlotIdent, Value: "JOB-3"}, scenario.Slot{Class: tc.class, Value: "nightly"}))
		if err := sc.Validate(); err != nil {
			t.Fatal(err)
		}
		ex := score.Ideal(sc)
		if bad := clean(sc, score.Score(sc, ex)); len(bad) != 0 {
			t.Fatalf("the ideal store, the branch's name a %s: %v", tc.class, bad)
		}
		ex.Items = append(ex.Items, active(scenario.KindDecision, score.TagStated, "Run the tests nightly on the branch nightly, JOB-3", src(15), src(17)))
		r := score.Score(sc, ex)
		var stale []string
		for _, fr := range r.Facts {
			if fr.Outcome == score.StaleInForce {
				stale = append(stale, fr.Fact)
			}
		}
		if got := r.Items[len(r.Items)-1].Outcome; got != tc.item || !slices.Equal(stale, tc.stale) || r.Metrics.StaleInForce != len(tc.stale) {
			t.Errorf("the branch's name a %s: the item is %s, kept in force %v, metrics %+v; want %s and %v", tc.class, got, stale, r.Metrics, tc.item, tc.stale)
		}
	}
}

// TestAcceptanceCarriesAccepted: an acceptance, a fact of the user's that
// supersedes another's, filed under stated is found with the wrong
// authority, which costs fact_holds its share and moves nothing else;
// under accepted it holds. Any other fact of the user's holds under
// either tag: the cases' k5, and v1.10's TERN-7, which supersedes a fact
// of the user's own.
func TestAcceptanceCarriesAccepted(t *testing.T) {
	sc := cases()
	// at is the place in the ideal store of the item in force that cites
	// line seq: 1 plants k5, 13 the acceptance w2.
	at := func(seq int64) int {
		i := slices.IndexFunc(base().Items, func(it score.Item) bool { return it.State == score.StateActive && it.Sources[0].Seq == seq })
		if i < 0 {
			t.Fatalf("no item in force cites line %d", seq)
		}
		return i
	}
	tagged := func(seq int64, tag string) score.Result {
		ex := base()
		ex.Items[at(seq)].Authority = tag
		return score.Score(sc, ex)
	}
	if it := base().Items[at(13)]; it.Authority != score.TagAccepted || it.Text != "DOTTEREL-W; weekly" {
		t.Fatalf("the ideal store files w2 as %+v", it)
	}
	r := tagged(13, score.TagStated)
	want := score.Score(sc, base()).Metrics
	want.FactHolds = fiveSixths
	if fr := r.Facts[len(r.Facts)-1]; fr.Fact != "w2" || fr.Outcome != score.Found || !slices.Equal(fr.Problems, []score.Problem{score.ProblemAuthority}) ||
		!closeMetrics(r.Metrics, want) || r.Items[at(13)].Outcome != score.Found {
		t.Errorf("w2 under stated is %s %v, item %s, metrics %+v; want found [authority] and %+v", fr.Outcome, fr.Problems, r.Items[at(13)].Outcome, r.Metrics, want)
	}
	for _, tc := range []struct {
		seq int64
		tag string
	}{{13, score.TagAccepted}, {1, score.TagStated}, {1, score.TagAccepted}} {
		if bad := clean(sc, tagged(tc.seq, tc.tag)); len(bad) != 0 {
			t.Errorf("line %d under %s: %v", tc.seq, tc.tag, bad)
		}
	}

	// v1's recorded pages: v1.09 tags the acceptance accepted; tagged
	// stated, it is found with the wrong authority. v1.10's TERN-7 holds
	// under either tag.
	v109, v110 := scenario.V1()[8], scenario.V1()[9]
	p := idealV1(v109)
	before := score.Score(v109, read(t, p))
	p.Now.Decisions[0].Tag = score.TagStated
	after := score.Score(v109, read(t, p))
	want = before.Metrics
	want.FactHolds -= 1.0 / float64(required(v109))
	if required(v109) != 1 || len(after.Facts) != 3 || v109.Facts[2].Supersedes != v109.Facts[1].ID {
		t.Fatalf("v1.09 is %+v", v109.Facts)
	}
	if fr := after.Facts[2]; fr.Outcome != score.Found || !slices.Equal(fr.Problems, []score.Problem{score.ProblemAuthority}) || !closeMetrics(after.Metrics, want) {
		t.Errorf("v1.09's acceptance under stated is %s %v, metrics %+v; want found [authority] and %+v", fr.Outcome, fr.Problems, after.Metrics, want)
	}
	for _, tag := range []string{score.TagAccepted, score.TagStated} {
		p := idealV1(v110)
		if p.Now.Decisions[0].Text != "Nix flakes handle everything, TERN-7" || v110.Facts[2].Supersedes != v110.Facts[0].ID || v110.Facts[0].Authority != scenario.AuthorityUser {
			t.Fatalf("v1.10's page or facts moved: %+v", v110.Facts)
		}
		p.Now.Decisions[0].Tag = tag
		if bad := clean(v110, score.Score(v110, read(t, p))); len(bad) != 0 {
			t.Errorf("v1.10's TERN-7 under %s: %v", tag, bad)
		}
	}
}

// hedged is a made-up scenario with one finding whose hedge is a slot
// left unasked of an open question. unasked names the kinds it is left
// unasked of; given a kind the finding does not permit, the scenario is
// not valid, which the scorer does not check.
func hedged(unasked ...scenario.Kind) scenario.Scenario {
	at := time.Date(2026, 3, 2, 9, 0, 0, 0, time.UTC)
	return scenario.Scenario{ID: "test.hedged", Family: "test", Name: "a hypothesis", Split: scenario.SplitHeldout, Passes: []scenario.Pass{{}},
		Sessions: []scenario.Session{{Host: string(adapter.Codex), ID: "5eed0004-dddd-4000-8000-000000000001", Branch: "main", Turns: []scenario.Turn{
			{At: at, Author: event.AuthorAgent, Kind: event.KindReply, Text: "My guess is that the WAXWING-4 cache slows the start.", Plants: []string{"h1"}},
		}}},
		Facts: []scenario.Fact{{ID: "h1", Kinds: []scenario.Kind{scenario.KindOpen, scenario.KindFact}, Authority: scenario.AuthorityOther, Must: scenario.MustStore,
			Slots: []scenario.Slot{{Class: scenario.SlotIdent, Value: "WAXWING-4"}, {Class: scenario.SlotLimit, Value: "guess", Forms: []string{"suspect"}, Unasked: unasked}}}},
	}
}

// TestUnaskedSlot: an item in a kind that leaves a slot unasked holds its
// fact without that slot; in another kind the fact permits, or one it does
// not, lacking the slot is limit_dropped. A slot left unasked of a kind
// the fact does not permit, which Validate refuses, is still asked of it.
func TestUnaskedSlot(t *testing.T) {
	sc := hedged(scenario.KindOpen)
	if err := sc.Validate(); err != nil {
		t.Fatal(err)
	}
	if bad := clean(sc, score.Score(sc, score.Ideal(sc))); len(bad) != 0 {
		t.Fatalf("the ideal store: %v", bad)
	}
	src := score.Source{Session: "5eed0004", Seq: 1}
	for _, tc := range []struct {
		sc         scenario.Scenario
		kind       scenario.Kind
		text       string
		item, fact string
	}{
		{sc, scenario.KindOpen, "Does the WAXWING-4 cache slow the start?", "supports", "holds []"},
		{sc, scenario.KindOpen, "Does the WAXWING-4 cache slow the start? A guess.", "supports", "holds []"},
		{sc, scenario.KindFact, "The WAXWING-4 cache slows the start", "limit_dropped", "limit_dropped []"},
		{sc, scenario.KindFact, "I suspect the WAXWING-4 cache slows the start", "supports", "holds []"},
		{sc, scenario.KindNext, "Check the WAXWING-4 cache", "limit_dropped", "limit_dropped []"},
		{sc, scenario.KindNext, "Check the WAXWING-4 cache, a guess", "found", "found [kind]"},
		{sc, scenario.KindOpen, "Does the cache slow the start?", "value_dropped", "value_dropped []"},
		{hedged(scenario.KindNext), scenario.KindNext, "Check the WAXWING-4 cache", "limit_dropped", "limit_dropped []"},
		{hedged(scenario.KindNext), scenario.KindOpen, "Does the WAXWING-4 cache slow the start?", "limit_dropped", "limit_dropped []"},
	} {
		r := score.Score(tc.sc, score.Export{Items: []score.Item{active(tc.kind, score.TagSuggested, tc.text, src)}})
		if got := fmt.Sprintf("%s %v", r.Facts[0].Outcome, r.Facts[0].Problems); string(r.Items[0].Outcome) != tc.item || got != tc.fact {
			t.Errorf("%q as %s, unasked of %v: the item is %s and the fact %s; want %s and %s", tc.text, tc.kind, tc.sc.Facts[0].Slots[1].Unasked, r.Items[0].Outcome, got, tc.item, tc.fact)
		}
	}
	if err := hedged(scenario.KindNext).Validate(); err == nil {
		t.Error("a slot unasked of a kind the fact does not permit is valid")
	}
}

// echoes is a made-up scenario with two corrections and the agent's
// reply that echoes both old values, which ends at the next line.
func echoes() scenario.Scenario {
	at := func(m int) time.Time { return time.Date(2026, 3, 2, 9, m, 0, 0, time.UTC) }
	user := func(m int, text string, plants ...string) scenario.Turn {
		return scenario.Turn{At: at(m), Author: event.AuthorTyped, Kind: event.KindPrompt, Text: text, Plants: plants}
	}
	s := "5eed0005-eeee-4000-8000-000000000001"
	fact := func(id string, authority scenario.Authority, supersedes, ref, value string) scenario.Fact {
		f := scenario.Fact{ID: id, Kinds: []scenario.Kind{scenario.KindDecision, scenario.KindFact}, Authority: authority, Must: scenario.MustStore, Supersedes: supersedes,
			Slots: []scenario.Slot{{Class: scenario.SlotIdent, Value: ref}, {Class: scenario.SlotPhrase, Value: value}}}
		if authority == scenario.AuthorityOther {
			f.Kinds, f.Until = []scenario.Kind{scenario.KindFact}, &scenario.Ref{Session: s, Seq: 6}
		}
		return f
	}
	return scenario.Scenario{ID: "test.echoes", Family: "test", Name: "two corrections and their echo", Split: scenario.SplitTuning, Passes: []scenario.Pass{{}},
		Sessions: []scenario.Session{{Host: string(adapter.Codex), ID: s, Branch: "main", Turns: []scenario.Turn{
			user(1, "Run the queue on alpha, QUEUE-1.", "a1"),
			user(2, "Keep the logs in bravo, LOGS-1.", "b1"),
			user(3, "Correction: run the queue on gamma, QUEUE-2.", "a2"),
			user(4, "Correction: keep the logs in delta, LOGS-2.", "b2"),
			{At: at(5), Author: event.AuthorAgent, Kind: event.KindReply, Text: "Done: the queue runs on alpha, QUEUE-8, and the logs stay in bravo, LOGS-8.",
				Plants: []string{"ea", "eb"}},
			user(6, "Nothing to note here."),
		}}},
		Facts: []scenario.Fact{
			fact("a1", scenario.AuthorityUser, "", "QUEUE-1", "alpha"), fact("b1", scenario.AuthorityUser, "", "LOGS-1", "bravo"),
			fact("a2", scenario.AuthorityUser, "a1", "QUEUE-2", "gamma"), fact("b2", scenario.AuthorityUser, "b1", "LOGS-2", "delta"),
			fact("ea", scenario.AuthorityOther, "", "QUEUE-8", "alpha"), fact("eb", scenario.AuthorityOther, "", "LOGS-8", "bravo"),
		},
	}
}

// TestCorrectionsEcho: an active item that holds a correction in full and
// names the value it replaced is not stale when it also cites the agent's
// echo of that value, with or without the old line. The echo is still
// kept in force by an item that holds the old value alone, one that holds
// the echo in full beside the correction, and one that holds the echo of
// one correction beside the other correction, whose replaced value it is
// not. The cost, recorded: value matching cannot read a negation, so an
// item that takes the echoed value beside the correction is spared too.
func TestCorrectionsEcho(t *testing.T) {
	sc := echoes()
	if err := sc.Validate(); err != nil {
		t.Fatal(err)
	}
	if bad := clean(sc, score.Score(sc, score.Ideal(sc))); len(bad) != 0 {
		t.Fatalf("the ideal store: %v", bad)
	}
	line := func(seq int) score.Source { return score.Source{Session: "5eed0005", Seq: int64(seq)} }
	for _, tc := range []struct {
		text  string
		lines []int
		stale []string
	}{
		{"Run the queue on gamma, QUEUE-2, corrected from alpha", []int{3, 5}, nil},
		{"Run the queue on gamma, QUEUE-2, corrected from alpha", []int{1, 3, 5}, nil},
		{"Keep the logs in delta, LOGS-2, not bravo", []int{2, 4, 5}, nil},
		{"Run the queue on gamma, QUEUE-2; not alpha, keep the logs in delta, LOGS-2, not bravo", []int{3, 4, 5}, nil},
		{"Run the queue on alpha, not gamma, QUEUE-2", []int{3, 5}, nil}, // the cost
		{"Run the queue on alpha", []int{5}, []string{"ea"}},
		{"Run the queue on alpha", []int{1, 5}, []string{"a1", "ea"}},
		{"Run the queue on gamma, QUEUE-2; the agent said alpha, QUEUE-8", []int{3, 5}, []string{"ea"}},
		{"Keep the logs in delta, LOGS-2; the queue on alpha", []int{4, 5}, []string{"ea"}},
		{"Keep the logs in delta, LOGS-2; the queue on alpha", []int{1, 4, 5}, []string{"a1", "ea"}},
	} {
		ex := score.Ideal(sc)
		var sources []score.Source
		for _, l := range tc.lines {
			sources = append(sources, line(l))
		}
		ex.Items = append(ex.Items, active(scenario.KindDecision, score.TagStated, tc.text, sources...))
		r := score.Score(sc, ex)
		var stale []string
		for _, fr := range r.Facts {
			if fr.Outcome == score.StaleInForce {
				stale = append(stale, fr.Fact)
			} else if fr.Outcome != score.Holds {
				t.Errorf("%q, citing %v: %s is %s", tc.text, tc.lines, fr.Fact, fr.Outcome)
			}
		}
		want := score.Supports
		if len(tc.stale) > 0 {
			want = score.StaleInForce
		}
		if got := r.Items[len(r.Items)-1].Outcome; got != want || !slices.Equal(stale, tc.stale) || r.Metrics.StaleInForce != len(tc.stale) {
			t.Errorf("%q, citing %v: the item is %s, kept in force %v, metrics %+v; want %s and %v", tc.text, tc.lines, got, stale, r.Metrics, want, tc.stale)
		}
	}
}

// TestNoRequiredFact: a scenario whose facts need not be stored has both
// shares at 1, whatever the store holds.
func TestNoRequiredFact(t *testing.T) {
	sc := cases()
	sc.Facts = slices.DeleteFunc(sc.Facts, func(f scenario.Fact) bool { return f.Must == scenario.MustStore })
	if m := score.Score(sc, score.Export{}).Metrics; m.FactRecall != 1 || m.FactHolds != 1 {
		t.Errorf("metrics %+v", m)
	}
}

// TestRows: a result gives one row for each metric, from the template,
// that a results store accepts with the scenario's planted words
// forbidden, and no field of which holds one.
func TestRows(t *testing.T) {
	want := map[string]string{score.MetricFactRecall: "ratio", score.MetricFactHolds: "ratio", score.MetricWrongWrite: "count", score.MetricNoise: "count",
		score.MetricMisattributed: "count", score.MetricStaleInForce: "count", score.MetricDupes: "count", score.MetricOTRStored: "count", score.MetricViolations: "count"}
	template := results.Row{Commit: strings.Repeat("0123456789abcdef", 4)[:40], Instrument: "fidelity", Scenario: "not-this-one", Seed: 7,
		Host: string(adapter.Codex), HostVersion: "0.1.0", Time: time.Date(2026, 11, 11, 0, 0, 0, 0, time.UTC)}
	// Each row holds its own metric's figure: every figure differs, so no
	// two fields swapped give the same rows.
	m := score.Metrics{FactRecall: 0.25, FactHolds: 0.125, WrongWrite: 3, Noise: 4, Misattributed: 5, StaleInForce: 6, Dupes: 7, OTRStored: 8, Violations: 9}
	values := map[string]float64{score.MetricFactRecall: 0.25, score.MetricFactHolds: 0.125, score.MetricWrongWrite: 3, score.MetricNoise: 4,
		score.MetricMisattributed: 5, score.MetricStaleInForce: 6, score.MetricDupes: 7, score.MetricOTRStored: 8, score.MetricViolations: 9}
	rows := score.Result{Scenario: "test.rows", Metrics: m}.Rows(template)
	if len(rows) != len(values) {
		t.Fatalf("%d rows", len(rows))
	}
	for _, row := range rows {
		if v, ok := values[row.Metric]; !ok || row.Value != v {
			t.Errorf("row %s holds %v, want %v", row.Metric, row.Value, v)
		}
		delete(values, row.Metric)
	}
	for _, sc := range append(scenario.V1(), cases()) {
		ex, _ := score.Distort(score.Ideal(sc), sc, score.DistortValueWrong)
		r := score.Score(sc, ex)
		rows := r.Rows(template)
		if len(rows) != len(want) {
			t.Fatalf("%s: %d rows", sc.ID, len(rows))
		}
		seen := map[string]bool{}
		for _, row := range rows {
			if want[row.Metric] != row.Unit || seen[row.Metric] || row.Scenario != sc.ID || row.Commit != template.Commit || row.Seed != 7 || row.Host != template.Host {
				t.Errorf("%s: row %+v", sc.ID, row)
			}
			seen[row.Metric] = true
		}
		if rows[0].Value != r.Metrics.FactRecall || rows[2].Value != float64(r.Metrics.WrongWrite) || r.Metrics.WrongWrite != 1 {
			t.Errorf("%s: values %v for %+v", sc.ID, rows, r.Metrics)
		}
		dir := t.TempDir()
		store := results.Store{Dir: dir, Forbid: scenario.Forbid(sc)}
		if err := store.Append(rows...); err != nil {
			t.Errorf("%s: %v", sc.ID, err)
		}
		if err := results.Forbidden(rows, scenario.Forbid(sc)); err != nil {
			t.Errorf("%s: %v", sc.ID, err)
		}
		if got, err := results.Load(dir); err != nil || len(got) != len(want) {
			t.Errorf("%s: read back %d rows, %v", sc.ID, len(got), err)
		}
		// The check can fail: a row that names a planted word is refused.
		bad := rows[0]
		bad.Model = strings.ToLower(sc.Facts[0].Slots[0].Value)
		if err := results.Forbidden([]results.Row{bad}, scenario.Forbid(sc)); err == nil {
			t.Errorf("%s: a row naming %q passes", sc.ID, bad.Model)
		}
	}
}

// TestImportsNoOrganizer: no non-test file of the scorer or of the v1 page
// reader, nor any package of the module they reach through them, imports
// the code that organizes memory or writes pages.
func TestImportsNoOrganizer(t *testing.T) {
	const module = "github.com/heyNag/eva-md/"
	banned := []string{"internal/update", "internal/threadpage", "internal/okfadapter", "internal/systempage", "internal/knowledgepage",
		"internal/wikiinit", "internal/wikigit", "internal/lint", "internal/organize", "internal/store"}
	root, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	var visit func(pkg, from string)
	visit = func(pkg, from string) {
		if seen[pkg] {
			return
		}
		seen[pkg] = true
		for _, b := range banned {
			if pkg == b || strings.HasPrefix(pkg, b+"/") {
				t.Errorf("%s imports %s", from, pkg)
			}
		}
		files, err := filepath.Glob(filepath.Join(root, filepath.FromSlash(pkg), "*.go"))
		if err != nil || len(files) == 0 {
			t.Fatalf("no Go files in %s (%v)", pkg, err)
		}
		for _, file := range files {
			if strings.HasSuffix(file, "_test.go") {
				continue
			}
			f, err := parser.ParseFile(token.NewFileSet(), file, nil, parser.ImportsOnly)
			if err != nil {
				t.Fatal(err)
			}
			for _, imp := range f.Imports {
				if p, _ := strconv.Unquote(imp.Path.Value); strings.HasPrefix(p, module) {
					visit(strings.TrimPrefix(p, module), pkg)
				}
			}
		}
	}
	visit("internal/eval/score", "")
	visit("internal/eval/v1page", "")
	for _, pkg := range []string{"internal/eval/scenario", "internal/eval/results", "internal/ledger/event"} {
		if !seen[pkg] {
			t.Errorf("the walk missed %s", pkg)
		}
	}
	// The check can fail: the scorer's own tests import the thread page
	// model, and the walk finds it there.
	f, err := parser.ParseFile(token.NewFileSet(), "v1_test.go", nil, parser.ImportsOnly)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.ContainsFunc(f.Imports, func(imp *ast.ImportSpec) bool { return strings.Contains(imp.Path.Value, "internal/threadpage") }) {
		t.Error("the tests' own import of the thread page model is not seen")
	}
}
