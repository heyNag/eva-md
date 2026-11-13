package retrievaltest_test

import (
	"context"
	"errors"
	"maps"
	"slices"
	"sync"
	"testing"

	"github.com/heyNag/eva-md/internal/eval/retrieval"
	"github.com/heyNag/eva-md/internal/eval/retrieval/retrievaltest"
)

// smallSet is a set of the test's own, every value made up: positives of
// each split, some with two items, and an event of each negative class.
func smallSet() *retrieval.Set {
	items := []retrieval.Item{
		{ID: "k1", Type: "fact", Project: "p1", Scope: retrieval.ScopeProject, Title: "the cache", Statement: "the cache lives under bin"},
		{ID: "k2", Type: "rule", Project: "p1", Scope: retrieval.ScopeProject, Title: "the hook", Statement: "the hook stays fast"},
		{ID: "k3", Type: "decision", Project: "p2", Scope: retrieval.ScopeProject, Title: "the parser", Statement: "the parser reads one line"},
		{ID: "k4", Type: "constraint", Scope: retrieval.ScopeAll, Title: "the remote", Statement: "the remote is pushed daily"},
	}
	events := []retrieval.Event{
		{ID: "ev01", Project: "p1", First: true, Text: "where is the cache", Split: "lexical", Class: retrieval.Positive, Relevant: []string{"k1"}},
		{ID: "ev02", Project: "p1", Text: "how quick is the hook and where is the cache", Split: "paraphrase", Class: retrieval.Positive, Relevant: []string{"k2", "k1"}},
		{ID: "ev03", Project: "p2", Text: "what does the parser read", Split: "lexical", Class: retrieval.Positive, Relevant: []string{"k3"}},
		{ID: "ev04", Project: "p2", Text: "when is the remote pushed", Split: "name", Class: retrieval.Positive, Relevant: []string{"k4", "k3"}},
		{ID: "ev05", Project: "p1", Text: "what is for lunch", Split: "lexical", Class: "no_match", Decoys: []string{"k1"}},
		{ID: "ev06", Project: "p2", Text: "the parser of another tool", Split: "paraphrase", Class: "near_miss", Decoys: []string{"k3"}},
		{ID: "ev07", Project: "p1", Text: "the sibling's cache", Split: "name", Class: "sibling_only", Decoys: []string{"k1"}},
		{ID: "ev08", Project: "p1", First: true, Text: "the hook again", Split: "lexical", Class: retrieval.Positive, Relevant: []string{"k2"}},
		{ID: "ev09", Project: "p2", Text: "nothing here", Split: "name", Class: "no_match"},
		{ID: "ev10", Project: "p1", Text: "the remote and the cache", Split: "paraphrase", Class: retrieval.Positive, Relevant: []string{"k4", "k1"}},
	}
	return &retrieval.Set{
		Version:  1,
		Projects: []retrieval.Project{{ID: "p1", Key: "key-one", Name: "one"}, {ID: "p2", Key: "key-two", Name: "two"}},
		Items:    items,
		Events:   events,
	}
}

func score(t *testing.T, s *retrieval.Set, name string, sys retrieval.System) *retrieval.Result {
	t.Helper()
	r, err := retrieval.Score(context.Background(), s, name, sys)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func built(t *testing.T, name string, s *retrieval.Set) retrieval.System {
	t.Helper()
	sys, err := retrieval.Build(name, s)
	if err != nil {
		t.Fatal(err)
	}
	return sys
}

func sameAdded(a, b map[string][]string) bool {
	return maps.EqualFunc(a, b, func(x, y []string) bool { return slices.Equal(x, y) })
}

// Given the oracle's answers, the fake scores as the oracle does; given
// none, as silence does.
func TestTheFakeScoresAsTheOracleAndAsSilence(t *testing.T) {
	s := smallSet()
	halves := map[string]bool{}
	for _, e := range s.Events {
		halves[retrieval.Half(e.ID)] = true
	}
	if len(halves) != 2 {
		t.Fatalf("the set's events fall in %d halves, want both", len(halves))
	}
	answers := map[string][]string{}
	for _, e := range s.Events {
		if len(e.Relevant) > 0 {
			answers[e.ID] = e.Relevant
		}
	}
	oracle := score(t, s, "oracle", built(t, "oracle", s))
	silent := score(t, s, "silent", built(t, "silent", s))
	if slices.Equal(oracle.Figures, silent.Figures) {
		t.Fatal("the oracle and silence give the same figures, so the test could not fail")
	}
	for _, c := range []struct {
		name    string
		answers map[string][]string
		want    *retrieval.Result
	}{
		{"the oracle's answers", answers, oracle},
		{"no answers", nil, silent},
	} {
		t.Run(c.name, func(t *testing.T) {
			fake := retrievaltest.New(c.answers)
			got := score(t, s, "fake", fake)
			if !slices.Equal(got.Figures, c.want.Figures) {
				t.Errorf("the fake's figures are %v, want %v", got.Figures, c.want.Figures)
			}
			if !sameAdded(got.Added, c.want.Added) {
				t.Errorf("the fake added %v, want %v", got.Added, c.want.Added)
			}
			var ids []string
			for _, e := range s.Events {
				ids = append(ids, e.ID)
			}
			if !slices.Equal(fake.Asked(), ids) {
				t.Errorf("the fake was asked %v, want %v", fake.Asked(), ids)
			}
		})
	}
}

func TestAddAnswersInOrderAndIsSilentOtherwise(t *testing.T) {
	f := retrievaltest.New(map[string][]string{"a": {"k2", "k1", "k3"}, "b": {}})
	ctx := context.Background()
	if got, err := f.Add(ctx, retrieval.Event{ID: "a"}); err != nil || !slices.Equal(got, []string{"k2", "k1", "k3"}) {
		t.Errorf("a gave %v, %v", got, err)
	}
	for _, id := range []string{"b", "c", ""} {
		if got, err := f.Add(ctx, retrieval.Event{ID: id, Relevant: []string{"k1"}}); err != nil || len(got) != 0 {
			t.Errorf("%q gave %v, %v; want silence", id, got, err)
		}
	}
}

func TestFailOnFailsOnlyItsEvent(t *testing.T) {
	f := retrievaltest.New(map[string][]string{"a": {"k1"}, "b": {"k2"}})
	ctx := context.Background()
	boom := errors.New("made-up failure")
	f.FailOn("a", boom)
	if got, err := f.Add(ctx, retrieval.Event{ID: "a"}); !errors.Is(err, boom) || got != nil {
		t.Errorf("a gave %v, %v; want the failure", got, err)
	}
	if got, err := f.Add(ctx, retrieval.Event{ID: "b"}); err != nil || !slices.Equal(got, []string{"k2"}) {
		t.Errorf("b gave %v, %v", got, err)
	}
	if got, err := f.Add(ctx, retrieval.Event{ID: "c"}); err != nil || got != nil {
		t.Errorf("c gave %v, %v", got, err)
	}
	// It fails again each time it is asked, until it is taken back.
	if _, err := f.Add(ctx, retrieval.Event{ID: "a"}); !errors.Is(err, boom) {
		t.Errorf("a asked again gave %v", err)
	}
	f.FailOn("a", nil)
	if got, err := f.Add(ctx, retrieval.Event{ID: "a"}); err != nil || !slices.Equal(got, []string{"k1"}) {
		t.Errorf("a after FailOn(nil) gave %v, %v", got, err)
	}
	// Failed asks are asked too, in order.
	if want := []string{"a", "b", "c", "a", "a"}; !slices.Equal(f.Asked(), want) {
		t.Errorf("asked %v, want %v", f.Asked(), want)
	}

	// The scorer stops at the failing event and names it.
	s := smallSet()
	fake := retrievaltest.New(nil)
	fake.FailOn("ev03", boom)
	if _, err := retrieval.Score(ctx, s, "fake", fake); !errors.Is(err, boom) {
		t.Errorf("the scorer gave %v", err)
	}
	if want := []string{"ev01", "ev02", "ev03"}; !slices.Equal(fake.Asked(), want) {
		t.Errorf("the scorer asked %v, want %v", fake.Asked(), want)
	}
}

func TestChangingWhatTheFakeGaveOrWasGivenChangesNothing(t *testing.T) {
	answers := map[string][]string{"a": {"k1", "k2"}}
	f := retrievaltest.New(answers)
	answers["a"][0] = "changed"
	answers["b"] = []string{"k3"}
	ctx := context.Background()
	got, err := f.Add(ctx, retrieval.Event{ID: "a"})
	if err != nil || !slices.Equal(got, []string{"k1", "k2"}) {
		t.Fatalf("after its map changed, a gave %v, %v", got, err)
	}
	got[0] = "changed"
	_ = append(got[:1], "appended")
	if again, _ := f.Add(ctx, retrieval.Event{ID: "a"}); !slices.Equal(again, []string{"k1", "k2"}) {
		t.Errorf("after its answer changed, a gave %v", again)
	}
	if b, _ := f.Add(ctx, retrieval.Event{ID: "b"}); b != nil {
		t.Errorf("an answer added to the map after New gave %v", b)
	}
	asked := f.Asked()
	asked[0] = "changed"
	if f.Asked()[0] != "a" {
		t.Error("changing what Asked returned changed the fake")
	}
}

func TestAskedKeepsTheOrderUnderConcurrentAsks(t *testing.T) {
	f := retrievaltest.New(nil)
	ctx := context.Background()
	var wg sync.WaitGroup
	for g := range 4 {
		wg.Go(func() {
			for i := range 50 {
				_, _ = f.Add(ctx, retrieval.Event{ID: string(rune('a'+g)) + string(rune('0'+i%10))})
			}
		})
	}
	wg.Wait()
	asked := f.Asked()
	if len(asked) != 200 {
		t.Fatalf("asked %d times, want 200", len(asked))
	}
	// Each goroutine's asks keep their own order among the others.
	for g := range 4 {
		var mine []string
		for _, id := range asked {
			if id[0] == byte('a'+g) {
				mine = append(mine, id)
			}
		}
		for i, id := range mine {
			if id[1] != byte('0'+i%10) {
				t.Fatalf("goroutine %d's asks are out of order: %v", g, mine)
			}
		}
	}
}
