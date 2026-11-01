package okfadapter

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// Writes inside Locked take no lock of their own, and a write from outside
// waits until Locked returns. Once the write from outside is seen sleeping
// between tries of the busy lock, it can't go on until Locked lets the lock
// go, so its page is not there yet; a write that skips the lock, or takes
// another, returns without ever sleeping there.
func TestLockedHoldsTheLockAcrossWrites(t *testing.T) {
	w := testWiki(t)
	done := make(chan error, 1)
	err := w.Locked(func(lw *Wiki) error {
		go func() { done <- w.Create("outside", concept("Outside", "Body"), "codex/test") }()
		if err := lw.Create("inside", concept("Inside", "Body"), "codex/test"); err != nil {
			return err
		}
		if err := lw.WriteFile("index.md", []byte("# Fixture\n")); err != nil {
			return err
		}
		// The lock every write from outside takes is held: a try that
		// doesn't wait finds it busy.
		if unlock, err := w.lockWithin(0); !errors.Is(err, ErrLockBusy) {
			if err == nil {
				unlock()
			}
			t.Errorf("a write from outside could take the lock while Locked held it: %v", err)
		}
		waitingForLock(t, done, "/okfadapter.(*Wiki).Create(")
		if _, err := w.Read("outside"); !errors.Is(err, fs.ErrNotExist) {
			t.Errorf("the write from outside saved its page while Locked held the lock: %v", err)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(hangGuard):
		t.Fatalf("the write from outside still waits %s after Locked returned", hangGuard)
	}
	for _, id := range []string{"inside", "outside"} {
		if _, err := w.Read(id); err != nil {
			t.Fatal(err)
		}
	}
}

// hangGuard bounds a wait that only a hang outlasts.
const hangGuard = time.Minute

// waitingForLock returns once a goroutine that the test's own goroutine
// started, running the write named by caller, sleeps between tries of the
// busy wiki lock, as the goroutines' stacks show. It fails if the write
// returns first. lockWithin sleeps there only after a try found the lock
// busy. A write that an earlier run of the test left waiting was started by
// another goroutine, so it never matches. The pause between looks only
// paces them.
func waitingForLock(t *testing.T, done <-chan error, caller string) {
	t.Helper()
	buf := make([]byte, 64<<10)
	self, _, _ := strings.Cut(string(buf[:runtime.Stack(buf, false)]), " [")
	guard := time.After(hangGuard)
	for {
		n := runtime.Stack(buf, true)
		if n == len(buf) {
			buf = make([]byte, 2*len(buf))
			continue
		}
		for _, g := range strings.Split(string(buf[:n]), "\n\n") {
			if strings.Contains(g, "\ntime.Sleep(") && strings.Contains(g, "/okfadapter.(*Wiki).lockWithin(") &&
				strings.Contains(g, caller) && strings.Contains(g, " in "+self+"\n") {
				return
			}
		}
		select {
		case err := <-done:
			t.Fatalf("the write returned while the wiki lock was held, never seen waiting for it: %v", err)
		case <-guard:
			t.Fatalf("the write was not seen waiting for the wiki lock in %s: it hangs before the lock", hangGuard)
		case <-time.After(time.Millisecond):
		}
	}
}

// A write that waits longer than its time limit for the wiki lock fails with
// ErrLockBusy, which every write returns as the lock gives it, so a caller
// can tell a busy wiki from a page okf refused. The lock gives up at the
// limit, not before, while Locked still holds it. Locked holds it until
// every write has returned, so the writes always find it busy; they are made
// through a second handle on the wiki whose own wait is 50 ms.
func TestBusyLockReportsErrLockBusy(t *testing.T) {
	w := testWiki(t)
	const limit = 50 * time.Millisecond
	other := *w
	other.lockWait = limit
	writes := []struct {
		name  string
		write func() error
	}{
		{"Create", func() error { return other.Create("blocked", concept("Blocked", "Body"), "codex/test") }},
		{"Update", func() error {
			_, err := other.Update("blocked", "", func(*Concept) error { return nil }, "codex/test")
			return err
		}},
		{"WriteFile", func() error { return other.WriteFile("blocked/index.md", []byte("# Blocked\n")) }},
		{"Locked", func() error {
			return other.Locked(func(*Wiki) error {
				t.Error("Locked ran its function while the lock was busy")
				return nil
			})
		}},
	}
	err := w.Locked(func(*Wiki) error {
		started := time.Now()
		unlock, err := w.lockWithin(limit)
		if err == nil {
			unlock()
		}
		if elapsed := time.Since(started); err != nil && elapsed < limit {
			t.Errorf("gave up after %s, before the limit", elapsed)
		}
		for _, c := range writes {
			if err := busyWrite(t, c.write); !errors.Is(err, ErrLockBusy) {
				t.Errorf("%s while Locked held the lock: %v, want ErrLockBusy", c.name, err)
			}
		}
		return err
	})
	if !errors.Is(err, ErrLockBusy) {
		t.Fatalf("a write past the lock timeout: %v", err)
	}
	if _, err := w.Read("blocked"); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("a write that found the lock busy saved its page: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(w.Root(), "blocked")); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("a write that found the lock busy saved its file: %v", err)
	}
}

// busyWrite runs a write that must give up on the busy wiki lock at its
// 50 ms limit, and fails if it still waits a minute later.
func busyWrite(t *testing.T, write func() error) error {
	t.Helper()
	done := make(chan error, 1)
	go func() { done <- write() }()
	select {
	case err := <-done:
		return err
	case <-time.After(hangGuard):
		t.Fatalf("a write still waits for the busy wiki lock %s past its 50 ms limit", hangGuard)
		return nil
	}
}

// A person locks a page with locked set to true, yes or on, in any case,
// written plain or quoted.
func TestIsLocked(t *testing.T) {
	for _, tc := range []struct {
		value any
		want  bool
	}{
		{true, true}, {"true", true}, {"True", true}, {"yes", true}, {" YES ", true}, {"on", true},
		{false, false}, {"false", false}, {"no", false}, {"off", false}, {"", false}, {1.0, false}, {nil, false},
	} {
		c := &Concept{Extra: map[string]any{}}
		if tc.value != nil {
			c.Extra["locked"] = tc.value
		}
		if got := IsLocked(c); got != tc.want {
			t.Errorf("locked: %#v reads as %v", tc.value, got)
		}
	}
	if IsLocked(&Concept{}) {
		t.Error("a page without frontmatter extras reads as locked")
	}
}

// Serialize gives the text Create saves, the trimmed title and the extra
// keys included, once it holds the generated line Create added.
func TestSerializeIsWhatCreateSaves(t *testing.T) {
	w := testWiki(t)
	c := concept(" Orders: core ", "Body")
	c.Extra = map[string]any{"repos": []string{"github.com__acme__orders"}, "as_of": "2026-10-30"}
	if err := w.Create("orders", c, "codex/test"); err != nil {
		t.Fatal(err)
	}
	pg, err := w.Read("orders")
	if err != nil {
		t.Fatal(err)
	}
	c.Generated = pg.Concept.Generated
	if got := Serialize(c); got != string(pg.Raw) {
		t.Fatalf("serialized\n%s\nsaved\n%s", got, pg.Raw)
	}
}
