package work

import (
	"context"
	"fmt"
	"io"
	"math/rand/v2"
	"slices"
	"sync"
	"testing"
	"time"
)

func TestNoWakeIsLostWhileAJobIsHeld(t *testing.T) {
	dirs := testDirs(t)
	release := make(chan struct{})
	held := make(chan struct{})
	var once sync.Once
	var mu sync.Mutex
	ran := map[string]int{}
	withJobs(t, Job{Kind: "sync", Run: func(_ context.Context, marks []Mark, _ io.Writer) Outcome {
		mu.Lock()
		for _, m := range marks {
			ran[m.Key]++
		}
		mu.Unlock()
		once.Do(func() { close(held) })
		<-release
		return Done
	}})
	first := make(chan error, 1)
	go func() {
		first <- Wake(context.Background(), dirs, []Mark{{Kind: "sync", Key: "held", Reason: "stop"}}, Options{})
	}()
	select {
	case <-held:
	case <-time.After(hangGuard):
		t.Fatal("the first job did not start")
	}
	const wakes = 50
	returned := make(chan error, wakes)
	waiters := make(chan struct{}, wakes)
	for i := range wakes {
		go func() {
			o := Options{hook: func(p string) {
				if p == "waiter" {
					waiters <- struct{}{}
				}
			}}
			returned <- Wake(context.Background(), dirs, []Mark{{Kind: "sync", Key: fmt.Sprintf("s%02d", i), Reason: "stop"}}, o)
		}()
	}
	// While the job is held, every wake but one returns, and that one waits.
	for range wakes - 1 {
		select {
		case err := <-returned:
			if err != nil {
				t.Fatal(err)
			}
		case <-time.After(hangGuard):
			t.Fatal("a wake did not return while the job was held")
		}
	}
	select {
	case <-waiters:
	case <-time.After(hangGuard):
		t.Fatal("no wake became the waiter")
	}
	if len(waiters) != 0 {
		t.Fatal("two wakes became the waiter")
	}
	close(release)
	for _, c := range []chan error{first, returned} {
		select {
		case err := <-c:
			if err != nil {
				t.Fatal(err)
			}
		case <-time.After(hangGuard):
			t.Fatal("a wake did not return after the job was released")
		}
	}
	for i := range wakes {
		if key := fmt.Sprintf("s%02d", i); ran[key] != 1 {
			t.Errorf("%s ran %d times", key, ran[key])
		}
	}
	if ran["held"] != 1 {
		t.Errorf("the held mark ran %d times", ran["held"])
	}
	if p := peak.Load(); p > 2 || p < 2 {
		t.Errorf("%d callers were inside the worker or the waiter at once, want 2", p)
	}
	if n := inside.Load(); n != 0 {
		t.Errorf("%d callers still inside", n)
	}
	if got := names(t, dirs, wakeDir); len(got) != 0 {
		t.Errorf("marks left: %q", got)
	}
}

// The points of a wake where a schedule holds a caller until it lets it
// go: just after taking the worker lock, after taking marks, in the job's
// Run, and before the last listing.
var holdPoints = []string{"worker", "taken", "run", "last"}

// A schedEvent is a caller reaching a point.
type schedEvent struct {
	caller int
	point  string // "marked", "waiter", a hold point, or "exit"
	marks  []Mark // for "run"
	err    error  // for "exit"
}

type callerKey struct{}

// A caller is one wake of a schedule, and the channel that lets it go.
type caller struct {
	id      int
	release chan Outcome
}

func TestGeneratedSchedulesLoseNoWake(t *testing.T) {
	const schedules = 200
	lost := 0
	for seed := uint64(1); seed <= schedules; seed++ {
		lost += runSchedule(t, seed)
		if t.Failed() {
			t.Fatalf("schedule %d failed", seed)
		}
	}
	t.Logf("lost_wakes %d over %d schedules", lost, schedules)
	if lost != 0 {
		t.Fatalf("lost_wakes %d", lost)
	}
}

// runSchedule plays one seeded schedule: wakes, each with one or no mark,
// started at points where every caller is held or waiting, and holds let
// go in a random order. It returns the wakes no run took up.
func runSchedule(t *testing.T, seed uint64) int {
	t.Helper()
	rng := rand.New(rand.NewPCG(seed, 0x5eed))
	dirs := testDirs(t)
	events := make(chan schedEvent, 64)
	release := map[int]chan Outcome{}
	run := func(ctx context.Context, marks []Mark, _ io.Writer) Outcome {
		c := ctx.Value(callerKey{}).(caller)
		events <- schedEvent{caller: c.id, point: "run", marks: marks}
		return <-c.release
	}
	withJobs(t, Job{Kind: "sync", Priority: 1, Run: run}, Job{Kind: "tidy", Priority: 2, Run: run})

	type wake struct {
		key, reason string
	}
	var wakes []wake  // in the order their marks were written
	var runs [][]Mark // every Run's marks
	state := map[int]string{}
	wakesLeft := 6 + rng.IntN(7)
	keys := []string{"sync-a", "sync-b", "tidy-a", "sync-c"}

	start := func(c int, marks []Mark) {
		rel := make(chan Outcome, 1)
		release[c] = rel
		state[c] = "active"
		ctx := context.WithValue(context.Background(), callerKey{}, caller{c, rel})
		o := Options{hook: func(p string) {
			switch p {
			case "marked", "waiter":
				events <- schedEvent{caller: c, point: p}
			case "worker", "taken", "last":
				events <- schedEvent{caller: c, point: p}
				<-rel
			}
		}}
		go func() {
			err := Wake(ctx, dirs, marks, o)
			events <- schedEvent{caller: c, point: "exit", err: err}
		}()
	}
	// stable reports whether every caller is held, waiting behind a held
	// worker, or gone.
	stable := func() bool {
		lockHeld, waiting := false, false
		for _, s := range state {
			switch s {
			case "active":
				return false
			case "waiter":
				waiting = true
			case "worker", "taken", "run":
				lockHeld = true
			}
		}
		return !waiting || lockHeld
	}
	settle := func() {
		for !stable() {
			select {
			case ev := <-events:
				switch ev.point {
				case "marked":
					// Still active.
				case "exit":
					if ev.err != nil {
						t.Errorf("seed %d: caller %d: %v", seed, ev.caller, ev.err)
					}
					delete(state, ev.caller)
				case "run":
					runs = append(runs, ev.marks)
					state[ev.caller] = ev.point
				default:
					state[ev.caller] = ev.point
				}
			case <-time.After(hangGuard):
				t.Fatalf("seed %d: stuck in state %v", seed, state)
			}
		}
	}

	next := 0
	for wakesLeft > 0 || len(state) > 0 {
		var held []int
		for c, s := range state {
			if slices.Contains(holdPoints, s) {
				held = append(held, c)
			}
		}
		slices.Sort(held)
		if wakesLeft > 0 && (len(held) == 0 || rng.IntN(3) == 0) {
			wakesLeft--
			var marks []Mark
			if rng.IntN(6) > 0 { // one wake in six is a tick
				kind, key, _ := cutKey(keys[rng.IntN(len(keys))])
				reason := fmt.Sprintf("w%d", len(wakes))
				marks = []Mark{{Kind: kind, Key: key, Reason: reason}}
				wakes = append(wakes, wake{kind + "-" + key, reason})
			}
			start(next, marks)
			next++
			settle()
			continue
		}
		if len(held) == 0 {
			t.Fatalf("seed %d: callers %v, none held", seed, state)
		}
		c := held[rng.IntN(len(held))]
		out := Done
		if rng.IntN(5) == 0 {
			out = Failed
		}
		state[c] = "active"
		release[c] <- out
		settle()
	}

	// Every wake reached a run that took its mark, or a mark written over
	// it later, after it was written: the marks are written in order, so
	// the reasons tell.
	order := map[string]int{}
	for i, w := range wakes {
		order[w.reason] = i
	}
	took := map[string]bool{}
	for _, marks := range runs {
		for _, m := range marks {
			r := m.Kind + "-" + m.Key + "/" + m.Reason
			if took[r] {
				t.Errorf("seed %d: mark %s ran twice", seed, r)
			}
			took[r] = true
		}
	}
	lost := 0
	for i, w := range wakes {
		reached := false
		for j := i; j < len(wakes) && !reached; j++ {
			reached = wakes[j].key == w.key && took[w.key+"/"+wakes[j].reason]
		}
		if !reached {
			lost++
			t.Errorf("seed %d: wake %d for %s was lost", seed, i, w.key)
		}
	}
	if got := names(t, dirs, wakeDir); len(got) != 0 {
		t.Errorf("seed %d: marks left: %q", seed, got)
	}
	if got := names(t, dirs, runDir); len(got) != 0 {
		t.Errorf("seed %d: runs left: %q", seed, got)
	}
	if n := inside.Load(); n != 0 {
		t.Errorf("seed %d: %d callers still inside", seed, n)
	}
	if p := peak.Load(); p > 2 {
		t.Errorf("seed %d: %d callers were inside at once", seed, p)
	}
	return lost
}

func cutKey(s string) (kind, key string, ok bool) {
	for i := range len(s) {
		if s[i] == '-' {
			return s[:i], s[i+1:], true
		}
	}
	return "", "", false
}
