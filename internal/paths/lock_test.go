package paths

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

// TryLock creates its folder and a private lock file, reports a held lock
// as busy without waiting, and refuses a symlink in the lock's place.
func TestTryLock(t *testing.T) {
	isolate(t)
	dir := filepath.Join(t.TempDir(), "locks")
	unlock, busy, err := TryLock(dir, "job.lock")
	if err != nil || busy {
		t.Fatalf("first lock: busy %v, %v", busy, err)
	}
	if info, err := os.Stat(filepath.Join(dir, "job.lock")); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("the lock file: %v %v", info, err)
	}
	if _, busy, err := TryLock(dir, "job.lock"); err != nil || !busy {
		t.Fatalf("held lock: busy %v, %v", busy, err)
	}
	unlock()
	again, busy, err := TryLock(dir, "job.lock")
	if err != nil || busy {
		t.Fatalf("after unlock: busy %v, %v", busy, err)
	}
	again()
	if err := os.Symlink(filepath.Join(dir, "job.lock"), filepath.Join(dir, "linked.lock")); err != nil {
		t.Fatal(err)
	}
	if _, _, err := TryLock(dir, "linked.lock"); err == nil {
		t.Fatal("a symlinked lock was taken")
	}
}

func TestSearchIndexIsUnderTheCache(t *testing.T) {
	d := Dirs{State: "/state"}
	if got := d.SearchIndex(); got != filepath.Join(d.Cache(), "search") || got != "/state/cache/search" {
		t.Fatalf("SearchIndex() = %q", got)
	}
}

// Callers that create the same new lock at once each get the lock or busy,
// never an error: on macOS, creating a file through os.Root without O_EXCL
// can fail with ENOENT for the one that loses the race.
func TestTryLockOnANewLockIsTakenOrBusy(t *testing.T) {
	isolate(t)
	dir := filepath.Join(t.TempDir(), "locks")
	for i := 0; i < 500; i++ {
		name := fmt.Sprintf("new-%d.lock", i)
		start := make(chan struct{})
		var wg sync.WaitGroup
		for worker := 0; worker < 8; worker++ {
			wg.Go(func() {
				<-start
				unlock, _, err := TryLock(dir, name)
				if err != nil {
					t.Errorf("%s: %v", name, err)
					return
				}
				if unlock != nil {
					unlock()
				}
			})
		}
		close(start)
		wg.Wait()
		if t.Failed() {
			return
		}
	}
}
