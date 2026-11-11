package retrieval

import (
	"bytes"
	"context"
	"errors"
	"maps"
	"math"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/heyNag/eva-md/internal/eval/results"
)

// answers is a fake system: the items it adds for each event, by id.
type answers map[string][]string

func (a answers) Add(_ context.Context, e Event) ([]string, error) { return a[e.ID], nil }

var catalogueFile = filepath.Join("..", "..", "..", "eval", "retrieval", "metrics.json")

// smallSet is a set of three projects, two of them sharing a system page,
// four items and five events, every one of them held out.
func smallSet() *Set {
	return &Set{
		Version: 1,
		Projects: []Project{
			{ID: "p_aaaaaaaaaaaa", Key: "github.com__acme__alpha", Name: "alpha", Pages: []string{"shared"}},
			{ID: "p_bbbbbbbbbbbb", Key: "github.com__acme__bravo", Name: "bravo", Pages: []string{"shared"}},
			{ID: "p_cccccccccccc", Key: "local-charlie-0a1b2c", Name: "charlie"},
		},
		Groups: []Group{{Page: "shared", Projects: []string{"p_aaaaaaaaaaaa", "p_bbbbbbbbbbbb"}}},
		Items: []Item{
			{ID: "iaaaaaaaaaaaaa", Type: "fact", Project: "p_aaaaaaaaaaaa", Scope: ScopeProject, Title: "Ledger export format", Statement: "The ledger export writes one CSV row per invoice line."},
			{ID: "ibbbbbbbbbbbbb", Type: "fact", Project: "p_aaaaaaaaaaaa", Scope: ScopeProject, Title: "Ledger import format", Statement: "The ledger import reads one JSON object per payment."},
			{ID: "iccccccccccccc", Type: "fact", Project: "p_bbbbbbbbbbbb", Scope: ScopeProject, Title: "Courier export format", Statement: "The courier export writes one XML node per parcel."},
			{ID: "iddddddddddddd", Type: "fact", Project: "p_cccccccccccc", Scope: ScopeProject, Title: "Courier export format", Statement: "The courier export writes one XML node per parcel."},
			{ID: "iggggggggggggg", Type: "rule", Scope: ScopeAll, Title: "Plain commits", Statement: "Write commit messages in plain words.", Words: "plain commit messages"},
		},
		Events: []Event{
			{ID: "e1", Project: "p_aaaaaaaaaaaa", Text: "t1", Split: "lexical", Class: Positive, Relevant: []string{"iaaaaaaaaaaaaa"}, Decoys: []string{}},
			{ID: "e5", Project: "p_aaaaaaaaaaaa", Text: "t5", Split: "paraphrase", Class: Positive, Relevant: []string{"iaaaaaaaaaaaaa", "ibbbbbbbbbbbbb"}, Decoys: []string{}},
			{ID: "e6", Project: "p_aaaaaaaaaaaa", Text: "t6", Split: "name", Class: Positive, Relevant: []string{"iggggggggggggg"}, Decoys: []string{}},
			{ID: "e7", Project: "p_aaaaaaaaaaaa", Text: "t7", Split: "lexical", Class: "near_miss", Relevant: []string{}, Decoys: []string{"ibbbbbbbbbbbbb"}},
			{ID: "e8", Project: "p_aaaaaaaaaaaa", Text: "t8", Split: "lexical", Class: "sibling_only", Relevant: []string{}, Decoys: []string{"iccccccccccccc"}},
		},
	}
}

func figures(t *testing.T, s *Set, sys System) map[string][2]int {
	t.Helper()
	res, err := Score(context.Background(), s, "fake", sys)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string][2]int{}
	for _, f := range res.Figures {
		if _, twice := out[f.Metric]; twice {
			t.Fatalf("%s is given twice", f.Metric)
		}
		out[f.Metric] = [2]int{f.Num, f.Den}
	}
	return out
}

// On five events a fake system's additions give exact figures for every
// measure, leave out each ratio with nothing to count, and one sibling
// item more raises false additions by exactly one addition.
func TestFiveEventsGiveExactFigures(t *testing.T) {
	s := smallSet()
	for _, e := range s.Events {
		if Half(e.ID) != Heldout {
			t.Fatalf("event %s is not held out; the figures below assume it is", e.ID)
		}
	}
	fake := answers{
		"e1": {"iaaaaaaaaaaaaa", "ibbbbbbbbbbbbb"}, // one relevant, one not
		"e5": {"ibbbbbbbbbbbbb"},                   // one of two relevant
		"e7": {"ibbbbbbbbbbbbb"},                   // a near miss given its decoy
	}
	h := func(group, measure string) string { return Metric(Heldout, group, measure) }
	want := map[string][2]int{
		h("lexical", Precision): {1, 3}, h("lexical", Recall): {1, 1}, h("lexical", FalseAdditions): {2, 3},
		h("lexical", SilencePos): {0, 1}, h("lexical", SilenceNeg): {1, 2},
		h("paraphrase", Precision): {1, 1}, h("paraphrase", Recall): {1, 2}, h("paraphrase", FalseAdditions): {0, 1},
		h("paraphrase", SilencePos): {0, 1},
		h("name", Recall):           {0, 1}, h("name", SilencePos): {1, 1},
		h("all", Precision): {2, 4}, h("all", Recall): {2, 4}, h("all", FalseAdditions): {2, 4},
		h("all", SilencePos): {1, 3}, h("all", SilenceNeg): {1, 2},
		h("near_miss", FalseEvents): {1, 1}, h("sibling_only", FalseEvents): {0, 1},
	}
	got := figures(t, s, fake)
	if !maps.Equal(got, want) {
		t.Errorf("figures:\n got %v\nwant %v", got, want)
	}

	fake["e8"] = []string{"iccccccccccccc"} // the sibling's item, added to the sibling-only event
	after := figures(t, s, fake)
	for _, g := range []string{"lexical", "all"} {
		m := h(g, FalseAdditions)
		if after[m] != [2]int{got[m][0] + 1, got[m][1] + 1} {
			t.Errorf("%s: %v, then %v with one sibling item more", m, got[m], after[m])
		}
	}
	for m, v := range got {
		if !strings.Contains(m, ".lexical.") && !strings.Contains(m, ".all.") && !strings.Contains(m, ".sibling_only.") && after[m] != v {
			t.Errorf("%s changed from %v to %v", m, v, after[m])
		}
	}
	if after[h("sibling_only", FalseEvents)] != [2]int{1, 1} || after[h("lexical", SilenceNeg)] != [2]int{0, 2} {
		t.Errorf("the sibling-only event's figures: %v %v", after[h("sibling_only", FalseEvents)], after[h("lexical", SilenceNeg)])
	}

	// Every figure's metric is one the scorer may give.
	for m := range after {
		if !slices.Contains(Metrics(), m) {
			t.Errorf("%s is not among the metrics", m)
		}
	}
}

// Zero in 160 prints a bound under 1.9%, and the table prints it with the
// count.
func TestTheBoundOfAZeroCount(t *testing.T) {
	if b := Bound(160); b >= 0.019 || math.Abs(b-(1-math.Pow(0.05, 1.0/160))) > 1e-15 || b < 0.0185 {
		t.Errorf("Bound(160) is %v", b)
	}
	if b := Bound(1); math.Abs(b-0.95) > 1e-12 {
		t.Errorf("Bound(1) is %v", b)
	}
	r := &Result{System: "fake", Version: 1, Figures: []Figure{
		{Metric(Heldout, "no_match", FalseEvents), 0, 160},
		{Metric(Heldout, "near_miss", FalseEvents), 3, 160},
	}}
	var b bytes.Buffer
	r.Table(&b)
	lines := strings.Split(b.String(), "\n")
	if !strings.Contains(lines[1], "0 of 160, under 1.86% at 95%") || strings.Contains(lines[2], "under") || !strings.Contains(lines[2], "3 of 160") {
		t.Errorf("table:\n%s", b.String())
	}
}

// A system that adds an item the set lacks, adds one twice or fails stops
// the run, naming the event; and changing the event it is given changes
// no label.
func TestScoreRefusesABadAnswer(t *testing.T) {
	for name, sys := range map[string]System{
		"an unknown item": answers{"e7": {"izzzzzzzzzzzzz"}},
		"an item twice":   answers{"e7": {"iaaaaaaaaaaaaa", "iaaaaaaaaaaaaa"}},
		"an error":        failing("e7"),
	} {
		if _, err := Score(context.Background(), smallSet(), "fake", sys); err == nil || !strings.Contains(err.Error(), "e7") {
			t.Errorf("%s: %v", name, err)
		}
	}
	s := smallSet()
	if _, err := Score(context.Background(), s, "fake", meddler{}); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(s.Events[0].Relevant, []string{"iaaaaaaaaaaaaa"}) || len(s.Events[3].Decoys) != 1 {
		t.Errorf("a system changed the labels: %v %v", s.Events[0].Relevant, s.Events[3].Decoys)
	}
}

type failing string

func (f failing) Add(_ context.Context, e Event) ([]string, error) {
	if e.ID == string(f) {
		return nil, errors.New("broken")
	}
	return nil, nil
}

// meddler changes the labels it is given and adds the first relevant item.
type meddler struct{}

func (meddler) Add(_ context.Context, e Event) ([]string, error) {
	if len(e.Relevant) > 0 {
		e.Relevant[0] = "izzzzzzzzzzzzz"
	}
	if len(e.Decoys) > 0 {
		e.Decoys[0] = "izzzzzzzzzzzzz"
	}
	return nil, nil
}

// v1's recall adds a sibling project's item only when the two projects
// share a system page, and never an item of a project that shares none.
func TestV1AddsASiblingOnlyWithASharedPage(t *testing.T) {
	ask := func(s *Set, project, text string) []string {
		t.Helper()
		sys, err := Build("v1-recall", s)
		if err != nil {
			t.Fatal(err)
		}
		got, err := sys.Add(context.Background(), Event{ID: "q", Project: project, Text: text})
		if err != nil {
			t.Fatal(err)
		}
		return got
	}
	const text = "What is the courier export format, one XML node per parcel?"
	s := smallSet()
	if got := ask(s, "p_aaaaaaaaaaaa", text); !slices.Equal(got, []string{"iccccccccccccc"}) {
		t.Errorf("with a shared page, alpha gets %v", got)
	}
	if got := ask(s, "p_cccccccccccc", text); !slices.Equal(got, []string{"iddddddddddddd"}) {
		t.Errorf("charlie gets %v", got)
	}
	s.Projects[0].Pages, s.Projects[1].Pages = nil, nil
	if got := ask(s, "p_aaaaaaaaaaaa", text); len(got) != 0 {
		t.Errorf("with no shared page, alpha gets %v", got)
	}
	if got := ask(s, "p_aaaaaaaaaaaa", "Does the ledger export write one CSV row per invoice line?"); !slices.Equal(got, []string{"iaaaaaaaaaaaaa"}) {
		t.Errorf("alpha's own item: %v", got)
	}
	if got := ask(s, "p_bbbbbbbbbbbb", "Should commit messages be in plain words?"); !slices.Equal(got, []string{"iggggggggggggg"}) {
		t.Errorf("the item of every project, a rule: %v", got)
	}

	// On the committed set, v1 adds sibling items with the page, and none
	// without it; it never adds an item of a project outside the chat's
	// and its siblings'.
	set := loadCommitted(t)
	siblingAdds := func(set *Set) int {
		t.Helper()
		sys, err := Build("v1-recall", set)
		if err != nil {
			t.Fatal(err)
		}
		res, err := Score(context.Background(), set, "v1-recall", sys)
		if err != nil {
			t.Fatal(err)
		}
		n := 0
		for _, e := range set.Events {
			for _, id := range res.Added[e.ID] {
				it := set.Item(id)
				switch {
				case inScope(it, e.Project):
				case siblings(set, e.Project, it.Project):
					n++
				default:
					t.Errorf("event %s was given %s of a project that is not a sibling", e.ID, id)
				}
			}
		}
		return n
	}
	if n := siblingAdds(set); n == 0 {
		t.Error("v1 added no sibling item on the set")
	}
	for i := range set.Projects {
		set.Projects[i].Pages = nil
	}
	if n := siblingAdds(set); n != 0 {
		t.Errorf("with no shared page, v1 added %d sibling items", n)
	}
}

// The test arms give the figures they stand for on the committed set.
func TestOracleAndSilent(t *testing.T) {
	s := loadCommitted(t)
	for _, name := range []string{"oracle", "silent"} {
		sys, err := Build(name, s)
		if err != nil {
			t.Fatal(err)
		}
		res, err := Score(context.Background(), s, name, sys)
		if err != nil {
			t.Fatal(err)
		}
		seen := map[string]bool{}
		for _, f := range res.Figures {
			measure := f.Metric[strings.LastIndex(f.Metric, ".")+1:]
			seen[measure] = true
			want := map[string]map[string]float64{
				"oracle": {Precision: 1, Recall: 1, FalseAdditions: 0, SilencePos: 0, SilenceNeg: 1, FalseEvents: 0},
				"silent": {Recall: 0, SilencePos: 1, SilenceNeg: 1, FalseEvents: 0},
			}[name]
			if v, ok := want[measure]; !ok || f.Value() != v {
				t.Errorf("%s: %s is %d of %d", name, f.Metric, f.Num, f.Den)
			}
		}
		if name == "oracle" && len(seen) != 6 || name == "silent" && len(seen) != 4 {
			t.Errorf("%s gives the measures %v", name, seen)
		}
	}
}

// v1's run on the committed set, as rows: what the baseline must hold.
func v1Rows(t *testing.T, commit string, at time.Time) []results.Row {
	t.Helper()
	s := loadCommitted(t)
	sys, err := Build("v1-recall", s)
	if err != nil {
		t.Fatal(err)
	}
	res, err := Score(context.Background(), s, "v1-recall", sys)
	if err != nil {
		t.Fatal(err)
	}
	return res.Rows(commit, false, at)
}

// Two runs give the same rows apart from the time; the rows are of the
// catalogue, pass the Store with the set's forbidden words, and hold none
// of them; a planted name in a scenario id makes the Store refuse a row.
func TestRowsAndTheirForbiddenWords(t *testing.T) {
	commit := strings.Repeat("ab", 20)
	first := v1Rows(t, commit, time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC))
	second := v1Rows(t, commit, time.Date(2026, 11, 2, 0, 0, 0, 0, time.UTC))
	for i := range second {
		second[i].Time = first[i].Time
	}
	if !slices.Equal(first, second) || len(first) == 0 {
		t.Errorf("two runs differ: %d and %d rows", len(first), len(second))
	}
	cat, err := results.LoadCatalogue(catalogueFile)
	if err != nil {
		t.Fatal(err)
	}
	if err := cat.Check(first); err != nil {
		t.Error(err)
	}
	s := loadCommitted(t)
	forbid := s.Forbid()
	store := results.Store{Dir: t.TempDir(), Forbid: forbid}
	if err := store.Append(first...); err != nil {
		t.Fatal(err)
	}
	if first[0].Scenario != "v1-recall.set1" || first[0].Instrument != "retrieval" || first[0].Unit != "ratio" || first[0].Seed != s.Seed {
		t.Errorf("a row: %+v", first[0])
	}

	// No id the scorer can write holds a planted word: no metric, and the
	// scenario of no registered system.
	var every []results.Row
	for _, name := range Systems() {
		for _, m := range Metrics() {
			r := first[0]
			r.Metric, r.Scenario = m, (&Result{System: name, Version: s.Version}).Scenario()
			every = append(every, r)
		}
	}
	if err := results.Forbidden(every, forbid); err != nil {
		t.Error(err)
	}
	for _, word := range forbid {
		r := first[0]
		r.Scenario = "v1-recall.set1-" + strings.ToLower(strings.NewReplacer("_", "-", ".", "-").Replace(word))
		if err := store.Append(r); err == nil {
			t.Errorf("a scenario id holding %q is stored", word)
		}
	}
	if len(forbid) < 40 {
		t.Errorf("the set forbids %d words", len(forbid))
	}
}

// The catalogue lists every metric the scorer can give, in its order, as
// ratios in the right direction, covering the set, this package and v1's
// recall, and never the baseline; the gates are not-worse with no margin
// on the held-out precision, recall and false additions of all events and
// of name events.
func TestTheGateFiles(t *testing.T) {
	cat, err := results.LoadCatalogue(catalogueFile)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, m := range cat.Metrics {
		names = append(names, m.Name)
		want := "higher"
		if strings.HasSuffix(m.Name, FalseAdditions) || strings.HasSuffix(m.Name, SilencePos) || strings.HasSuffix(m.Name, FalseEvents) {
			want = "lower"
		}
		if m.Unit != Unit || m.Direction != want {
			t.Errorf("%s is in %s, %s is better", m.Name, m.Unit, m.Direction)
		}
		if !slices.Equal(m.Paths, []string{"eval/retrieval/set", "internal/eval/retrieval", "internal/knowledgepage"}) {
			t.Errorf("%s covers %v", m.Name, m.Paths)
		}
	}
	if !slices.Equal(names, Metrics()) {
		t.Errorf("the catalogue lists %d metrics, the scorer gives %d", len(names), len(Metrics()))
	}
	gates, err := results.LoadGates(filepath.Join(filepath.Dir(catalogueFile), "gates.json"), cat)
	if err != nil {
		t.Fatal(err)
	}
	var want []results.Gate
	for _, g := range []string{"all", "name"} {
		for _, m := range []string{Precision, Recall, FalseAdditions} {
			want = append(want, results.Gate{Metric: Metric(Heldout, g, m), Stat: "mean", Rule: "not_worse"})
		}
	}
	if !slices.Equal(gates, want) {
		t.Errorf("gates: %+v", gates)
	}
}

// The accepted baseline holds v1's figures on the committed set, every
// one of them and only those, with a figure for every gate.
func TestTheBaselineIsV1sRun(t *testing.T) {
	cat, err := results.LoadCatalogue(catalogueFile)
	if err != nil {
		t.Fatal(err)
	}
	b, err := results.LoadBaseline(filepath.Join(filepath.Dir(catalogueFile), "baseline.json"), cat)
	if err != nil {
		t.Fatal(err)
	}
	fresh := v1Rows(t, b.Commit, time.Time{})
	if len(b.Rows) != len(fresh) {
		t.Fatalf("the baseline holds %d rows, a run gives %d", len(b.Rows), len(fresh))
	}
	for i, r := range b.Rows {
		f := fresh[i]
		if r.Metric != f.Metric || r.Value != f.Value || r.Scenario != f.Scenario || r.Instrument != f.Instrument || r.Seed != f.Seed || r.Unit != f.Unit {
			t.Errorf("baseline row %d: %s %v %s, a run gives %s %v %s", i+1, r.Metric, r.Value, r.Scenario, f.Metric, f.Value, f.Scenario)
		}
	}
	gates, err := results.LoadGates(filepath.Join(filepath.Dir(catalogueFile), "gates.json"), cat)
	if err != nil {
		t.Fatal(err)
	}
	for _, g := range gates {
		if !slices.ContainsFunc(b.Rows, func(r results.Row) bool { return r.Metric == g.Metric }) {
			t.Errorf("the baseline holds no figure for the gate on %s", g.Metric)
		}
	}
}

// The table prints figures and ids, and no event's text and no item's.
func TestTheTableShowsNoText(t *testing.T) {
	s := loadCommitted(t)
	sys, err := Build("v1-recall", s)
	if err != nil {
		t.Fatal(err)
	}
	res, err := Score(context.Background(), s, "v1-recall", sys)
	if err != nil {
		t.Fatal(err)
	}
	var b bytes.Buffer
	res.Table(&b)
	out := b.String()
	if !strings.Contains(out, Metric(Heldout, "all", Precision)) || !strings.Contains(out, "given an item that is not relevant") {
		t.Fatalf("table:\n%s", out)
	}
	for _, e := range s.Events {
		if strings.Contains(out, e.Text) {
			t.Fatalf("the table shows event %s's text", e.ID)
		}
	}
	for _, it := range s.Items {
		if strings.Contains(out, it.Title) || strings.Contains(out, it.Statement) {
			t.Fatalf("the table shows item %s's text", it.ID)
		}
	}
	for _, w := range s.Forbid() {
		if strings.Contains(strings.ToLower(out), strings.ToLower(w)) {
			t.Fatalf("the table shows a planted word")
		}
	}
	t.Log("\n" + out[:strings.Index(out, "Events given")])
}

// Register refuses a name that is taken or malformed, and a system with no
// builder; the registered systems are the test arms and v1's recall.
func TestRegister(t *testing.T) {
	if got := Systems(); !slices.Equal(got, []string{"oracle", "silent", "v1-recall"}) {
		t.Errorf("systems: %v", got)
	}
	build := func([]Item, []Project) (System, error) { return answers{}, nil }
	for name, call := range map[string]func(){
		"a name taken":    func() { Register("oracle", build) },
		"a capital":       func() { Register("Fake", build) },
		"a dot":           func() { Register("fake.one", build) },
		"no builder":      func() { Register("fake", nil) },
		"an empty name":   func() { Register("", build) },
		"a name too long": func() { Register(strings.Repeat("a", 33), build) },
	} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("%s: Register did not stop the program", name)
				}
			}()
			call()
		}()
	}
	if _, err := Build("missing", smallSet()); err == nil {
		t.Error("an unknown system is built")
	}
	if got := Systems(); len(got) != 3 {
		t.Errorf("a refused system was kept: %v", got)
	}
}
