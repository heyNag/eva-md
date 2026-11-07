package update

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/heyNag/eva-md/internal/paths"
)

// Jobs and hooks that start the same new run log, or the same new day's
// job log, at once each get it, never an error: on macOS, a file created
// through os.Root without O_EXCL can fail with ENOENT for the caller that
// loses the race, which would lose a run's record or a spawn.
func TestNewLogsTakeConcurrentCreators(t *testing.T) {
	isolate(t)
	base := t.TempDir()
	now := time.Date(2026, 10, 31, 9, 30, 0, 0, time.Local)
	for i := 0; i < 50; i++ {
		state := filepath.Join(base, fmt.Sprint("state-", i))
		dirs := paths.Dirs{State: state}
		start := make(chan struct{})
		var wg sync.WaitGroup
		for worker := 0; worker < 8; worker++ {
			wg.Go(func() {
				<-start
				f, err := OpenLog(dirs, now)
				if err != nil {
					t.Errorf("job log %d: %v", i, err)
					return
				}
				f.Close()
				if err := appendRecord(state, Record{At: now.Format(time.RFC3339), Host: HostCodex, Outcome: OutcomeSkipped}); err != nil {
					t.Errorf("run log %d: %v", i, err)
				}
			})
		}
		close(start)
		wg.Wait()
		if t.Failed() {
			return
		}
		f, err := os.Open(filepath.Join(state, "updates", "2026-10-31.jsonl"))
		if err != nil {
			t.Fatal(err)
		}
		lines := 0
		for s := bufio.NewScanner(f); s.Scan(); {
			lines++
		}
		f.Close()
		if lines != 8 {
			t.Fatalf("run log %d: %d lines, want 8", i, lines)
		}
	}
}
