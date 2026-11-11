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
		item    score.Outcome     // of the last item
		facts   map[string]string // outcome and problems of facts that change
		metrics func(score.Metrics) bool
	}{
		{name: "the value in another form", spoil: func(e *score.Export) { e.Items[0] = k5("Cache entries for 300 s, KITE-5") },
			item: "", metrics: func(m score.Metrics) bool { return m.FactHolds == 1 }},
		{name: "a wrong value", spoil: func(e *score.Export) { e.Items[0] = k5("Cache entries for 7 minutes, KITE-5") },
			facts: map[string]string{"k5": "value_wrong []"}, metrics: func(m score.Metrics) bool { return m.WrongWrite == 1 && m.FactHolds == fiveSixths && m.FactRecall == fiveSixths }},
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
			facts: map[string]string{"p2": "found [authority]"}, metrics: func(m score.Metrics) bool { return m.Misattributed == 1 && m.FactRecall == 1 && m.FactHolds == fiveSixths }},
		{name: "an unknown tag", spoil: func(e *score.Export) { e.Items[1].Authority = "bogus" },
			facts: map[string]string{"p2": "found [authority]"}, metrics: func(m score.Metrics) bool { return m.Misattributed == 0 }},
		{name: "an unknown scope", spoil: func(e *score.Export) { e.Items[0].Scope = "everywhere" },
			facts: map[string]string{"k5": "found [scope]"}},
		{name: "a known scope", spoil: func(e *score.Export) { e.Items[0].Scope = "project" }},
		{name: "a dated line under another day", spoil: func(e *score.Export) { e.Items[5].Date = "2026-03-01" },
			facts: map[string]string{"t1": "found [date]"}},
		{name: "a fact in force retired", spoil: func(e *score.Export) { e.Items[0].State = score.StateRetired },
			facts: map[string]string{"k5": "found [validity]"}},
		{name: "a fact in force only as a dated line", spoil: func(e *score.Export) {
			e.Items[0] = score.Item{Kind: scenario.KindLog, State: score.StateLog, Authority: score.TagStated, Date: "2026-03-02", Text: "Cache entries for 5 minutes, KITE-5", Sources: []score.Source{src(1)}}
		}, facts: map[string]string{"k5": "found [kind]"}, metrics: func(m score.Metrics) bool { return m.FactRecall == 1 && m.FactHolds == fiveSixths }},
		{name: "a fact in force nowhere", spoil: func(e *score.Export) { e.Items = e.Items[1:] },
			facts: map[string]string{"k5": "missing []"}, metrics: func(m score.Metrics) bool { return m.FactRecall == fiveSixths && m.FactHolds == fiveSixths }},
		{name: "a fact that ended kept in force", spoil: func(e *score.Export) {
			e.Items = append(e.Items, active(scenario.KindDecision, score.TagStated, "Use Go 1.26, GOV-1", src(9)))
		}, item: score.StaleInForce, facts: map[string]string{"g1": "stale_in_force []"}, metrics: func(m score.Metrics) bool { return m.StaleInForce == 1 && m.Dupes == 0 }},
		{name: "a fact that ended on a dated line", spoil: func(e *score.Export) {
			e.Items = append(e.Items, score.Item{Kind: scenario.KindLog, State: score.StateLog, Authority: score.TagStated, Date: "2026-03-02", Text: "Used Go 1.26, GOV-1", Sources: []score.Source{src(9)}})
		}, item: score.Supports, metrics: func(m score.Metrics) bool { return m.StaleInForce == 0 }},
		{name: "a proposal carried on by its acceptance", spoil: func(e *score.Export) {
			i := slices.IndexFunc(e.Items, func(it score.Item) bool { return it.State == score.StateActive && strings.HasPrefix(it.Text, "DOTTEREL-W") })
			e.Items[i].Sources = []score.Source{src(12), src(13)}
		}, metrics: func(m score.Metrics) bool { return m.StaleInForce == 0 }},
		{name: "a proposal kept open after its acceptance", spoil: func(e *score.Export) {
			e.Items = append(e.Items, active(scenario.KindOpen, score.TagSuggested, "Rotate logs weekly, DOTTEREL-W?", src(12)))
		}, item: score.StaleInForce, facts: map[string]string{"w1": "stale_in_force []"}, metrics: func(m score.Metrics) bool { return m.StaleInForce == 1 }},
		{name: "a fact stored twice", spoil: func(e *score.Export) { e.Items = append(e.Items, k5("KITE-5: cache entries for 5 min")) },
			item: score.Supports, metrics: func(m score.Metrics) bool { return m.Dupes == 1 }},
		{name: "a dated line logged twice", spoil: func(e *score.Export) { e.Items = append(e.Items, e.Items[5]) },
			item: score.Supports, metrics: func(m score.Metrics) bool { return m.Dupes == 0 }},
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
		Host: "codex", HostVersion: "0.1.0", Time: time.Date(2026, 11, 11, 0, 0, 0, 0, time.UTC)}
	for _, sc := range append(scenario.V1(), cases()) {
		ex, _ := score.Distort(score.Ideal(sc), sc, score.DistortValueWrong)
		r := score.Score(sc, ex)
		rows := r.Rows(template)
		if len(rows) != len(want) {
			t.Fatalf("%s: %d rows", sc.ID, len(rows))
		}
		seen := map[string]bool{}
		for _, row := range rows {
			if want[row.Metric] != row.Unit || seen[row.Metric] || row.Scenario != sc.ID || row.Commit != template.Commit || row.Seed != 7 || row.Host != "codex" {
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
