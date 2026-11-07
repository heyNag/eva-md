package session

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// TryLock takes the lock Lock takes, by the same name, without waiting: a
// session Lock holds, as a hook holds it, is busy, also one whose id a
// file name can't hold, and once it is let go TryLock takes it and holds
// it against another try. Another host's session is another lock. A link
// in the lock's place is refused.
func TestTryLockTakesTheSessionsLockWithoutWaiting(t *testing.T) {
	isolate(t)
	dir := filepath.Join(t.TempDir(), "locks")
	for _, id := range []string{"3f2a91c0-1111-2222-3333-444444444444", "odd:id"} {
		held, err := Lock(context.Background(), dir, "codex", id)
		if err != nil {
			t.Fatal(err)
		}
		release, busy, err := TryLock(dir, "codex", id)
		if err != nil || !busy || release != nil {
			t.Fatalf("%s: a held session: busy %v, %v", id, busy, err)
		}
		held()
		release, busy, err = TryLock(dir, "codex", id)
		if err != nil || busy || release == nil {
			t.Fatalf("%s: a free session: busy %v, %v", id, busy, err)
		}
		if other, busy, err := TryLock(dir, "codex", id); err != nil || !busy || other != nil {
			t.Fatalf("%s: TryLock took a session TryLock holds: busy %v, %v", id, busy, err)
		}
		release()
		if other, busy, err := TryLock(dir, "claude-code", id); err != nil || busy {
			t.Fatalf("%s: another host's session: busy %v, %v", id, busy, err)
		} else {
			other()
		}
	}
	target := filepath.Join(t.TempDir(), "elsewhere")
	if err := os.WriteFile(target, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(dir, "codex-linked.lock")); err != nil {
		t.Fatal(err)
	}
	if release, busy, err := TryLock(dir, "codex", "linked"); err == nil {
		if release != nil {
			release()
		}
		t.Fatalf("TryLock took a link: busy %v", busy)
	}
}
