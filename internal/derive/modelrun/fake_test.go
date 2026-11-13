package modelrun

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
	"testing"

	"github.com/heyNag/eva-md/internal/host/adapter"
)

// The fake records a copy of each call, answers with Outcome and Err, or
// with Respond when that is set, gives Caps to Probe, and gives a done
// context's error, as a real runner does.
func TestFake(t *testing.T) {
	f := &Fake{
		Caps:    Caps{Host: adapter.Codex, CLI: "/fake/cli", Version: "1.2.3", Efforts: []string{"low"}, Features: []string{"alpha"}},
		Outcome: Outcome{Class: ClassOK, Answer: []byte(answer), ModelsSeen: []string{"m"}, Signals: []Signal{SignalModelUnreported}},
	}
	caps, err := f.Probe(context.Background())
	if err != nil || !sameCaps(caps, f.Caps) {
		t.Fatalf("probe %+v %v", caps, err)
	}
	caps.Efforts[0], caps.Features[0] = "changed", "changed"
	if f.Caps.Efforts[0] != "low" || f.Caps.Features[0] != "alpha" {
		t.Fatal("the probe gave the fake's own slices")
	}
	c := Call{Task: TaskExtract, Input: []byte("input"), Schema: []byte("{}")}
	out, err := f.Run(context.Background(), c)
	if err != nil || out.Class != ClassOK || string(out.Answer) != answer {
		t.Fatalf("run %+v %v", out, err)
	}
	out.Answer[0], out.ModelsSeen[0], out.Signals[0] = 'X', "changed", "changed"
	c.Input[0], c.Schema[0] = 'X', 'X'
	if again, _ := f.Run(context.Background(), Call{Task: TaskRepair}); string(again.Answer) != answer || again.ModelsSeen[0] != "m" || again.Signals[0] != SignalModelUnreported {
		t.Fatalf("the run gave the fake's own slices: %+v", again)
	}
	calls := f.Calls()
	if len(calls) != 2 || string(calls[0].Input) != "input" || string(calls[0].Schema) != "{}" || calls[1].Task != TaskRepair {
		t.Fatalf("calls %+v", calls)
	}
	calls[0].Task = "changed"
	if f.Calls()[0].Task != TaskExtract {
		t.Fatal("Calls gave the fake's own list")
	}

	f.Err = errors.New("made-up failure")
	if _, err := f.Run(context.Background(), Call{}); err != f.Err {
		t.Fatalf("err %v", err)
	}
	f.Respond = func(c Call) (Outcome, error) {
		c.Input[0] = 'X'
		return Outcome{Class: ClassHost, ModelAsked: c.Model}, nil
	}
	if out, err := f.Run(context.Background(), Call{Model: "m1", Input: []byte("kept")}); err != nil || out.Class != ClassHost || out.ModelAsked != "m1" {
		t.Fatalf("respond %+v %v", out, err)
	}
	if got := f.Calls(); string(got[len(got)-1].Input) != "kept" {
		t.Fatalf("Respond changed the recorded call: %q", got[len(got)-1].Input)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := f.Probe(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("probe with a done context: %v", err)
	}
	n := len(f.Calls())
	if _, err := f.Run(ctx, Call{Task: TaskReview}); !errors.Is(err, context.Canceled) || len(f.Calls()) != n+1 {
		t.Fatalf("run with a done context: %v", err)
	}
}

// The fake may be called from several goroutines at once.
func TestFakeIsSafeForConcurrentUse(t *testing.T) {
	f := &Fake{Outcome: Outcome{Class: ClassOK, Answer: []byte(answer)}}
	var wg sync.WaitGroup
	for i := range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := f.Probe(context.Background()); err != nil {
				t.Error(err)
			}
			if _, err := f.Run(context.Background(), Call{Model: fmt.Sprint(i)}); err != nil {
				t.Error(err)
			}
			_ = f.Calls()
		}()
	}
	wg.Wait()
	var models []string
	for _, c := range f.Calls() {
		models = append(models, c.Model)
	}
	slices.Sort(models)
	if len(slices.Compact(slices.Clone(models))) != 20 {
		t.Fatalf("calls %q", models)
	}
}
