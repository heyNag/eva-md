package migrate

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"testing"
)

// The child's orders, by environment: the crash point, counted from 1, and
// its folders.
const (
	crashEnv  = "EVA_MD_MIGRATE_CRASH_AT"
	rootEnv   = "EVA_MD_MIGRATE_ROOT"
	stateEnv  = "EVA_MD_MIGRATE_STATE"
	locksEnv  = "EVA_MD_MIGRATE_LOCKS"
	stepEnv   = "EVA_MD_MIGRATE_STEP"
	childExit = 7 // the child ran to its end without reaching the point
)

// crashSteps are the steps a child can be told to run, by name.
var crashSteps = map[string]Step{"synthetic": synthetic, "index-only": indexOnly, "file-to-folder": fileToFolder}

// crashChild runs the step it was given, under the first pass, and kills
// itself with SIGKILL at the crash point it was given.
func crashChild() int {
	at, err := strconv.Atoi(os.Getenv(crashEnv))
	step, ok := crashSteps[os.Getenv(stepEnv)]
	if err != nil || !ok {
		return 2
	}
	n := 0
	hook = func(string) {
		if n++; n == at {
			_ = syscall.Kill(os.Getpid(), syscall.SIGKILL)
			select {}
		}
	}
	o := Options{StateDir: os.Getenv(stateEnv), LocksDir: os.Getenv(locksEnv), StoreID: storeID, Writer: writerA, Label: "first", Pass: passA}
	_, _ = Run(bg(), os.Getenv(rootEnv), step, o)
	return childExit
}

// crashAt runs the named step in a child on dir, with o's folders, and
// checks that it killed itself at the point given, counted from 1.
func crashAt(t *testing.T, dir string, o Options, step string, at int) {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=^$")
	cmd.Env = append(os.Environ(), crashEnv+"="+strconv.Itoa(at), stepEnv+"="+step, rootEnv+"="+dir, stateEnv+"="+o.StateDir, locksEnv+"="+o.LocksDir)
	err := cmd.Run()
	var exit *exec.ExitError
	if !errors.As(err, &exit) {
		t.Fatalf("the child did not crash: %v", err)
	}
	if ws, ok := exit.Sys().(syscall.WaitStatus); !ok || !ws.Signaled() || ws.Signal() != syscall.SIGKILL {
		t.Fatalf("the child did not crash at point %d: %v", at, err)
	}
}

// TestCrashAtEveryPoint kills a run at each point a recording run lists,
// every file write, git call and journal write or removal, and reruns it
// with another pass: the rerun gives the clean run's tree, one migrate
// commit over the commit before, a clean tree and no journal, and the
// commit carries the first pass once the journal was written.
func TestCrashAtEveryPoint(t *testing.T) {
	isolate(t)
	points, want := record(t, synthetic)
	journalAt := slices.Index(points, "journal write") + 1
	for _, p := range []string{"journal write", "journal remove", "write", "remove", "git add", "git commit", "git update-ref"} {
		if !slices.Contains(points, p) {
			t.Fatalf("the recording has no %q point: %v", p, points)
		}
	}
	if journalAt < 2 || points[len(points)-1] != "journal remove" {
		t.Fatalf("the recording is out of order: %v", points)
	}

	for i := 1; i <= len(points); i++ {
		t.Run(fmt.Sprintf("%02d %s", i, points[i-1]), func(t *testing.T) {
			dir := v1Wiki(t, v1Files)
			before := gitT(t, dir, "rev-parse", "HEAD")
			o := machine(t, writerB, "rerun", passB)
			crashAt(t, dir, o, "synthetic", i)

			r, err := Run(bg(), dir, synthetic, o)
			if err != nil {
				t.Fatal(err)
			}
			if got := treeID(t, dir); got != want {
				t.Errorf("tree %s, want the clean run's %s", got, want)
			}
			if got := gitT(t, dir, "rev-list", "--count", before+"..HEAD"); got != "1" {
				t.Errorf("%s commits over the commit before, want 1", got)
			}
			if got := gitT(t, dir, "rev-parse", "HEAD^"); got != before {
				t.Errorf("the commit's parent is %s, want %s", got, before)
			}
			if got := gitT(t, dir, "rev-parse", "refs/eva-md/before-schema-2"); got != before {
				t.Errorf("the before ref is %s, want %s", got, before)
			}
			if got := gitT(t, dir, "status", "--porcelain", "--untracked-files=all", "--ignored"); got != "" {
				t.Errorf("the tree is not clean:\n%s", got)
			}
			if _, err := os.Stat(filepath.Join(o.StateDir, journalFile)); !errors.Is(err, os.ErrNotExist) {
				t.Errorf("the journal is still there: %v", err)
			}
			pass := passB
			if i >= journalAt {
				pass = passA
			}
			msg := gitT(t, dir, "log", "-1", "--format=%B")
			if !strings.Contains(msg, "\nEva-Pass: "+pass+"\n") || !strings.HasPrefix(msg, "migrate: - (+10 ~5 -7)\n") {
				t.Errorf("message =\n%s\nwant pass %s", msg, pass)
			}
			// The rerun commits unless the child got past its commit;
			// then it finishes the child's, or, past the journal's
			// removal, finds the step done.
			last := i == len(points)
			if r.Committed == last || (r.Committed && (r.Pass != pass || r.Commit != gitT(t, dir, "rev-parse", "HEAD"))) {
				t.Errorf("rerun = %+v", r)
			}
		})
	}
}
