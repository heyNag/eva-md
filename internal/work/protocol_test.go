package work

import (
	"context"
	"io"
	"reflect"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/heyNag/eva-md/internal/paths"
)

// waitFor fails the test when c is not closed within the hang guard.
func waitFor(t *testing.T, c <-chan struct{}, what string) {
	t.Helper()
	select {
	case <-c:
	case <-time.After(hangGuard):
		t.Fatal(what)
	}
}

// writeMarkNow writes m into wake/ as a caller would, whatever lock is
// held.
func writeMarkNow(t *testing.T, dirs paths.Dirs, m Mark, now time.Time) {
	t.Helper()
	root, err := openWork(dirs)
	if err != nil {
		t.Error(err)
		return
	}
	defer root.Close()
	if err := writeMark(context.Background(), root, m, now); err != nil {
		t.Error(err)
	}
}

func TestTheWaiterDropsItsLockBeforeItWorks(t *testing.T) {
	dirs := testDirs(t)
	releaseB := make(chan struct{})
	running := make(chan string, 4)
	withJobs(t, Job{Kind: "sync", Run: func(_ context.Context, marks []Mark, _ io.Writer) Outcome {
		for _, m := range marks {
			running <- m.Key
			if m.Key == "b" {
				<-releaseB
			}
		}
		return Done
	}})
	// A is the worker: it runs its mark and is held once it is out of
	// marks, the lock still held, and again before its last listing.
	aIdle, aGoIdle, aLast, aGoLast := make(chan struct{}), make(chan struct{}), make(chan struct{}), make(chan struct{})
	a := make(chan error, 1)
	go func() {
		// A may pass these points again, after the test is done with it.
		var idles, lasts int
		a <- Wake(context.Background(), dirs, []Mark{{Kind: "sync", Key: "a", Reason: "stop"}}, Options{hook: func(p string) {
			switch {
			case p == "idle" && idles == 0:
				idles++
				close(aIdle)
				<-aGoIdle
			case p == "last" && lasts == 0:
				lasts++
				close(aLast)
				<-aGoLast
			}
		}})
	}()
	waitFor(t, aIdle, "the first wake did not become the worker")
	// A mark A does not see: it lists nothing more while it holds the lock.
	writeMarkNow(t, dirs, Mark{Kind: "sync", Key: "b", Reason: "stop"}, time.Now())
	// B, a tick, becomes the waiter, then the worker once A lets go.
	bWaiter := make(chan struct{})
	freeAtWorker := make(chan bool, 1)
	b := make(chan error, 1)
	go func() {
		var waiters, workers int
		b <- Wake(context.Background(), dirs, nil, Options{hook: func(p string) {
			switch {
			case p == "waiter" && waiters == 0:
				waiters++
				close(bWaiter)
			case p == "worker" && workers == 0:
				workers++
				unlock, busy, err := paths.TryLock(dirs.Locks(), waiterLock)
				if err == nil && !busy {
					unlock()
				}
				freeAtWorker <- err == nil && !busy
			}
		}})
	}()
	waitFor(t, bWaiter, "the second wake did not become the waiter")
	close(aGoIdle)
	waitFor(t, aLast, "the first wake did not reach its last listing")
	if k := <-running; k != "a" {
		t.Fatalf("ran %s first, want a", k)
	}
	select {
	case k := <-running:
		if k != "b" {
			t.Fatalf("ran %s, want b", k)
		}
	case <-time.After(hangGuard):
		t.Fatal("the waiter did not run b as the worker")
	}
	// Derive 3.1 step 5: it drops waiter.lock before it lists any mark.
	if !<-freeAtWorker {
		t.Fatal("the waiter still held waiter.lock when it became the worker")
	}
	// While B works, a third wake must become the waiter, not return.
	cWaiter := make(chan struct{})
	c := make(chan error, 1)
	go func() {
		var once sync.Once
		c <- Wake(context.Background(), dirs, []Mark{{Kind: "sync", Key: "c", Reason: "stop"}}, Options{hook: func(p string) {
			if p == "waiter" {
				once.Do(func() { close(cWaiter) })
			}
		}})
	}()
	select {
	case <-cWaiter:
	case err := <-c:
		t.Fatalf("the third wake returned %v without becoming the waiter", err)
	case <-time.After(hangGuard):
		t.Fatal("the third wake did not become the waiter")
	}
	close(releaseB)
	close(aGoLast)
	for _, ch := range []chan error{a, b, c} {
		select {
		case err := <-ch:
			if err != nil {
				t.Fatal(err)
			}
		case <-time.After(hangGuard):
			t.Fatal("a wake did not return")
		}
	}
	select {
	case k := <-running:
		if k != "c" {
			t.Fatalf("ran %s, want c", k)
		}
	default:
		t.Fatal("c did not run")
	}
	if got := names(t, dirs, wakeDir); len(got) != 0 {
		t.Fatalf("marks left: %q", got)
	}
}

func TestARunGetsTheWordsOfTheMarkItMoved(t *testing.T) {
	dirs := testDirs(t)
	r := newRecorder()
	withJobs(t, Job{Kind: "sync", Run: r.run})
	rewrote := false
	o := Options{hook: func(p string) {
		if p == "listed" && !rewrote {
			rewrote = true
			// The same mark, written again between the listing and the take.
			writeMarkNow(t, dirs, Mark{Kind: "sync", Key: "k1", Reason: "end", Host: "codex"}, time.Now())
		}
	}}
	if err := Wake(context.Background(), dirs, []Mark{{Kind: "sync", Key: "k1", Reason: "stop"}}, o); err != nil {
		t.Fatal(err)
	}
	if !rewrote {
		t.Fatal("the wake never reached the listed point")
	}
	want := [][]Mark{{{Kind: "sync", Key: "k1", Reason: "end", Host: "codex"}}}
	if got := r.all(); !reflect.DeepEqual(got, want) {
		t.Fatalf("ran %+v, want %+v", got, want)
	}
	lines := journal(t, dirs)
	if len(lines) != 2 || !slices.Equal(lines[0].Reasons, []string{"end"}) || !slices.Equal(lines[0].Hosts, []string{"codex"}) {
		t.Fatalf("journal %+v", lines)
	}
	if got := names(t, dirs, wakeDir); len(got) != 0 {
		t.Fatalf("marks left: %q", got)
	}
}
