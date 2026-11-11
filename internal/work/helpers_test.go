package work

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/heyNag/eva-md/internal/paths"
)

// hangGuard bounds a wait that only a bug makes long.
const hangGuard = 60 * time.Second

// withJobs replaces the registered jobs with js for one test, and resets
// the count of callers inside.
func withJobs(t *testing.T, js ...Job) {
	t.Helper()
	jobsMu.Lock()
	saved := jobs
	jobs = map[string]Job{}
	jobsMu.Unlock()
	for _, j := range js {
		Register(j)
	}
	inside.Store(0)
	peak.Store(0)
	t.Cleanup(func() {
		jobsMu.Lock()
		jobs = saved
		jobsMu.Unlock()
	})
}

// testDirs is a fresh state folder, through any alias of the temporary
// folder.
func testDirs(t *testing.T) paths.Dirs {
	t.Helper()
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return paths.Dirs{Config: filepath.Join(base, "config"), State: filepath.Join(base, "state"), Share: filepath.Join(base, "share")}
}

// holdLocks takes worker.lock and waiter.lock, so a wake writes its marks
// and returns without running anything, until the returned func is called.
func holdLocks(t *testing.T, dirs paths.Dirs) func() {
	t.Helper()
	var unlocks []func()
	for _, name := range []string{workerLock, waiterLock} {
		unlock, busy, err := paths.TryLock(dirs.Locks(), name)
		if err != nil || busy {
			t.Fatalf("take %s: busy %v, %v", name, busy, err)
		}
		unlocks = append(unlocks, unlock)
	}
	done := false
	release := func() {
		if !done {
			done = true
			for _, u := range unlocks {
				u()
			}
		}
	}
	t.Cleanup(release)
	return release
}

// leaveMarks writes marks with both locks held, so nothing runs them.
func leaveMarks(t *testing.T, dirs paths.Dirs, marks ...Mark) {
	t.Helper()
	release := holdLocks(t, dirs)
	if err := Wake(context.Background(), dirs, marks, Options{}); err != nil {
		t.Fatal(err)
	}
	release()
}

// names lists the files in <state>/work/dir, temporary files apart.
func names(t *testing.T, dirs paths.Dirs, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(dirs.State, "work", dir))
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, e := range entries {
		if !strings.HasPrefix(e.Name(), paths.TempPrefix) {
			out = append(out, e.Name())
		}
	}
	return out
}

// journalText is every journal file's bytes, oldest month first.
func journalText(t *testing.T, dirs paths.Dirs) string {
	t.Helper()
	files, err := filepath.Glob(filepath.Join(dirs.State, "work", journalDir, "*"))
	if err != nil {
		t.Fatal(err)
	}
	slices.Sort(files)
	var b strings.Builder
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		b.Write(data)
	}
	return b.String()
}

// journal is every journal line, decoded strictly.
func journal(t *testing.T, dirs paths.Dirs) []entry {
	t.Helper()
	var out []entry
	for _, line := range strings.Split(strings.TrimSpace(journalText(t, dirs)), "\n") {
		if line == "" {
			continue
		}
		d := json.NewDecoder(strings.NewReader(line))
		d.DisallowUnknownFields()
		var e entry
		if err := d.Decode(&e); err != nil {
			t.Fatalf("journal line %q: %v", line, err)
		}
		out = append(out, e)
	}
	return out
}

// keysOf lists the marks' keys.
func keysOf(marks []Mark) []string {
	out := make([]string, len(marks))
	for i, m := range marks {
		out[i] = m.Key
	}
	return out
}

// recorder is a job's Run that records the marks of each call and ends
// done.
type recorder struct {
	calls chan []Mark
}

func newRecorder() *recorder { return &recorder{calls: make(chan []Mark, 1000)} }

func (r *recorder) run(_ context.Context, marks []Mark, _ io.Writer) Outcome {
	r.calls <- marks
	return Done
}

func (r *recorder) all() [][]Mark {
	var out [][]Mark
	for {
		select {
		case c := <-r.calls:
			out = append(out, c)
		default:
			return out
		}
	}
}

// fileHas reports whether the file holds s.
func fileHas(path, s string) bool {
	data, err := os.ReadFile(path)
	return err == nil && bytes.Contains(data, []byte(s))
}
