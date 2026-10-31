package okfadapter

import (
	"errors"
	"testing"
	"time"
)

// Writes inside Locked take no lock of their own, and a write from outside
// waits until Locked returns.
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
		select {
		case err := <-done:
			t.Errorf("a write outside Locked ran while it held the lock: %v", err)
		case <-time.After(200 * time.Millisecond):
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"inside", "outside"} {
		if _, err := w.Read(id); err != nil {
			t.Fatal(err)
		}
	}
}

// A write that waits longer than lockTimeout for the wiki lock fails with
// ErrLockBusy, so a caller can tell a busy wiki from a page okf refused.
func TestBusyLockReportsErrLockBusy(t *testing.T) {
	w := testWiki(t)
	saved := lockTimeout
	lockTimeout = 50 * time.Millisecond
	t.Cleanup(func() { lockTimeout = saved })
	err := w.Locked(func(*Wiki) error {
		return w.Create("blocked", concept("Blocked", "Body"), "codex/test")
	})
	if !errors.Is(err, ErrLockBusy) {
		t.Fatalf("a write past the lock timeout: %v", err)
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
