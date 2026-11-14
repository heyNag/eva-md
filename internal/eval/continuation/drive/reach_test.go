package drive

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/heyNag/eva-md/internal/eval/continuation/drive/fakehost"
	"github.com/heyNag/eva-md/internal/eval/continuation/task"
	"github.com/heyNag/eva-md/internal/host/adapter"
)

// emptyWork fails unless the rig's work folder holds nothing: every
// trial's folder, and the run's, is gone.
func emptyWork(t *testing.T, r *rig) {
	t.Helper()
	if got := list(t, r.opts.Work); len(got) != 0 {
		t.Fatalf("the work folder holds %v", got)
	}
}

// stoppedBy fails unless the run stopped with the code.
func stoppedBy(t *testing.T, err error, code string) {
	t.Helper()
	var stopped *Stopped
	if !errors.As(err, &stopped) || stopped.Code != code {
		t.Fatalf("run: %v", err)
	}
}

// A hidden file the chat puts within its reach, at its path in the
// workspace (its bytes or others), in the home under another name, in the
// temporary folder, or between two megabytes of other bytes, gives
// hidden_found and stops the run: no later prompt, no later trial, no row,
// and no trial's folder left. Found after the first call, or after the
// second, which a graded round's prompt started.
func TestAHiddenFileWithinReachStopsTheRun(t *testing.T) {
	for _, h := range adapter.All() {
		for _, how := range []string{"path", "at-path", "home", "tmp", "padded"} {
			for _, from := range []int{0, 2} {
				t.Run(string(h.ID)+"/"+how+"/round "+string(rune('0'+max(from, 1))), func(t *testing.T) {
					r := newRig(t, 1)
					r.opts.Host, r.opts.Trials = string(h.ID), 2
					r.script.Steal, r.script.From = how, from
					s, out, err := r.run()
					stoppedBy(t, err, CodeHiddenFound)
					if s.Judged != 0 || s.Unjudged[CodeHiddenFound] != 1 || len(s.Unjudged) != 1 || s.NotRun != 1 ||
						!strings.Contains(out, "trial hidden-banner.t1: unjudged, hidden_found\n") {
						t.Fatalf("run: %+v\n%s", s, out)
					}
					calls := r.calls()
					continues := 0
					if from == 2 {
						continues = 1
					}
					if countMode(calls, "start") != 1 || countMode(calls, "continue") != continues {
						t.Fatalf("calls: %+v", calls)
					}
					if rows := r.rows(); rows != nil {
						t.Fatalf("rows: %+v", rows)
					}
					emptyWork(t, r)
					records, err := LoadRuns(r.opts.Out)
					if err != nil || len(records) != 1 || records[0].Ended != CodeHiddenFound {
						t.Fatalf("records %+v: %v", records, err)
					}
				})
			}
		}
	}
}

// copyingTest makes the fixture's hidden test of the colour copy itself,
// as code a grading runs may, to the path to, read from the folder the test
// runs in: <trial>/grade/r<n>/workspace.
func copyingTest(t *testing.T, r *rig, to string) {
	t.Helper()
	colour := filepath.Join(r.script.Task, "hidden", fakehost.FixtureRepo, "checks", "colour")
	body := "#!/bin/sh\ncp checks/colour " + to + "\ngrep -q " + fakehost.FixtureColor + " answer.txt\n"
	if err := os.WriteFile(colour, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
}

// Code a hidden test runs, which copies the hidden file out of the grading
// copy, into the trial's home, into the trial's grading folder beside the
// copy, or beside the trial's folder, is found before the prompt that would
// follow the grading: one call, and the run stops, leaving nothing.
func TestAHiddenTestThatCopiesItselfIsFound(t *testing.T) {
	for name, to := range map[string]string{
		"the home":             "../../../home/made-up-copy",
		"the grading folder":   "../../made-up-copy",
		"beside the trial":     "../../../../made-up-copy",
		"the run's own folder": "../../../../../../made-up-copy",
	} {
		t.Run(name, func(t *testing.T) {
			r := newRig(t, 1)
			r.opts.Trials = 2
			copyingTest(t, r, to)
			s, out, err := r.run()
			stoppedBy(t, err, CodeHiddenFound)
			if s.NotRun != 1 || s.Unjudged[CodeHiddenFound] != 1 || s.Judged != 0 {
				t.Fatalf("run: %+v\n%s", s, out)
			}
			if calls := r.calls(); countMode(calls, "start") != 1 || countMode(calls, "continue") != 0 {
				t.Fatalf("calls: %+v", calls)
			}
			emptyWork(t, r)
		})
	}
}

// The trial's grading folder is the driver's user's alone, and a grading
// whose folder a chat run as that user redirected, by a link at its round's
// folder or at the grading folder, or by a folder of its own there, writes
// no copy: the trial is hidden_found before any copy is made, so the
// hidden tests are written nowhere, no later prompt goes, and the run
// stops.
func TestARedirectedGradingFolderStopsTheRun(t *testing.T) {
	for name, plant := range map[string]func(t *testing.T, grade, outside string){
		"a link at the round's folder": func(t *testing.T, grade, outside string) {
			if err := os.Symlink(outside, filepath.Join(grade, "r1")); err != nil {
				t.Fatal(err)
			}
		},
		"a link at the grading folder": func(t *testing.T, grade, outside string) {
			if err := os.Remove(grade); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(outside, grade); err != nil {
				t.Fatal(err)
			}
		},
		"a folder of the chat's": func(t *testing.T, grade, _ string) {
			mkdir(t, filepath.Join(grade, "r1"))
		},
	} {
		t.Run(name, func(t *testing.T) {
			r := newRig(t, 1)
			r.opts.Condition, r.opts.Trials = task.Eva, 2
			outside := filepath.Join(r.base, "made-up-outside")
			mkdir(t, outside)
			var mode os.FileMode
			useArm(t, Arm{Prepare: func(_ context.Context, tr Trial) (Launch, error) {
				info, err := os.Lstat(tr.Dirs.Grade)
				if err != nil {
					return Launch{}, err
				}
				mode = info.Mode().Perm()
				plant(t, tr.Dirs.Grade, outside)
				return Launch{}, nil
			}})
			s, out, err := r.run()
			stoppedBy(t, err, CodeHiddenFound)
			if mode != 0o700 {
				t.Fatalf("the grading folder's mode is %v", mode)
			}
			if s.Judged != 0 || s.Unjudged[CodeHiddenFound] != 1 || s.NotRun != 1 {
				t.Fatalf("run: %+v\n%s", s, out)
			}
			if calls := r.calls(); countMode(calls, "start") != 1 || countMode(calls, "continue") != 0 {
				t.Fatalf("calls: %+v", calls)
			}
			if got := list(t, outside); len(got) != 0 {
				t.Fatalf("a grading wrote %v where the link points", got)
			}
			if rows := r.rows(); rows != nil {
				t.Fatalf("rows: %+v", rows)
			}
			emptyWork(t, r)
		})
	}
}

// A copy the last grading of a trial leaves where a later trial's chat can
// reach it, beside the trial's folder or in a trial's folder --keep-trials
// keeps, is found before the next trial's chat: the first trial is judged,
// the second starts no call, and the run stops.
func TestACopyALastGradingLeavesStopsTheNextTrial(t *testing.T) {
	for name, c := range map[string]struct {
		to   string
		keep bool
	}{
		"beside the trial": {"../../../../made-up-copy", false},
		"in a kept trial":  {"../../../home/made-up-copy", true},
	} {
		t.Run(name, func(t *testing.T) {
			r := newRig(t, 1)
			// The ideal record solves the trial in its first round, so its
			// grading is its last and no prompt follows it.
			r.opts.Condition, r.opts.Trials, r.opts.KeepTrials = task.Oracle, 3, c.keep
			copyingTest(t, r, c.to)
			s, out, err := r.run()
			stoppedBy(t, err, CodeHiddenFound)
			if s.Judged != 1 || s.Unjudged[CodeHiddenFound] != 1 || s.NotRun != 1 ||
				!strings.Contains(out, "trial hidden-banner.t2: unjudged, hidden_found\n") {
				t.Fatalf("run: %+v\n%s", s, out)
			}
			if calls := r.calls(); countMode(calls, "start") != 1 || countMode(calls, "continue") != 0 {
				t.Fatalf("calls: %+v", calls)
			}
			if !c.keep {
				emptyWork(t, r)
			}
		})
	}
}

// A hidden file's bytes in a folder its owner may not read, or may not
// enter, are found: the folder is given its owner's rights first.
func TestFoundInAFolderTheOwnerCannotRead(t *testing.T) {
	data := []byte("#!/bin/sh\nmade-up hidden test\n")
	for name, mode := range map[string]os.FileMode{"not read": 0o300, "not entered": 0o600} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			dir := filepath.Join(root, "locked")
			write(t, filepath.Join(dir, "copy.txt"), "made-up before\n"+string(data))
			if err := os.Chmod(dir, mode); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })
			h := &hidden{content: [][]byte{data}, longest: len(data)}
			if in, err := h.found(root); err != nil || !in {
				t.Fatalf("found: %v %v", in, err)
			}
		})
	}
}

// A start that holds a hidden file's bytes, from the repository or the
// task's start overlay, is the task's fault: the run stops before any call,
// the version's included.
func TestAStartHoldingAHiddenFileStopsTheRun(t *testing.T) {
	for name, where := range map[string]func(r *rig) string{
		"the repository":    func(r *rig) string { return filepath.Join(r.opts.Repos, fakehost.FixtureRepo, "notes", "copy.txt") },
		"the start overlay": func(r *rig) string { return filepath.Join(r.script.Task, "start", fakehost.FixtureRepo, "copy.txt") },
	} {
		t.Run(name, func(t *testing.T) {
			r := newRig(t, 1)
			r.opts.Trials = 3
			data, err := os.ReadFile(filepath.Join(r.script.Task, "hidden", fakehost.FixtureRepo, "checks", "answer"))
			if err != nil {
				t.Fatal(err)
			}
			write(t, where(r), "made-up notes before\n"+string(data)+"and after\n")
			s, out, err := r.run()
			stoppedBy(t, err, CodeHiddenFound)
			if s.NotRun != 2 || s.Unjudged[CodeHiddenFound] != 1 || s.Tokens != 0 {
				t.Fatalf("run: %+v\n%s", s, out)
			}
			if calls := r.calls(); len(calls) != 0 {
				t.Fatalf("calls: %+v", calls)
			}
			emptyWork(t, r)
		})
	}
}

// A condition that writes a hidden file's bytes into the trial, as a
// condition's Prepare or the plugin's files may, stops the run before the
// chat starts: the version is read, and no session starts.
func TestAConditionThatLaysAHiddenFileStopsTheRun(t *testing.T) {
	for name, where := range map[string]func(tr Trial) string{
		"in the workspace": func(tr Trial) string { return filepath.Join(tr.Dirs.Workspace, "answer.txt") },
		"in the home":      func(tr Trial) string { return filepath.Join(tr.Dirs.Home, ".made-up-plugin", "notes.md") },
	} {
		t.Run(name, func(t *testing.T) {
			r := newRig(t, 1)
			r.opts.Condition, r.opts.Trials = task.Eva, 2
			data, err := os.ReadFile(filepath.Join(r.script.Task, "hidden", fakehost.FixtureRepo, "checks", "colour"))
			if err != nil {
				t.Fatal(err)
			}
			useArm(t, Arm{Prepare: func(_ context.Context, tr Trial) (Launch, error) {
				p := where(tr)
				if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
					return Launch{}, err
				}
				return Launch{}, os.WriteFile(p, append([]byte("made-up notes\n"), data...), 0o644)
			}})
			s, out, err := r.run()
			stoppedBy(t, err, CodeHiddenFound)
			if s.Judged != 0 || s.Unjudged[CodeHiddenFound] != 1 || s.NotRun != 1 || s.Tokens != 0 {
				t.Fatalf("run: %+v\n%s", s, out)
			}
			if calls := r.calls(); countMode(calls, "version") != 1 || countMode(calls, "start") != 0 {
				t.Fatalf("calls: %+v", calls)
			}
			emptyWork(t, r)
		})
	}
}

// No trial's folder is left, even one the chat made read-only in its
// workspace and its home, whether the trial is judged, unjudged, or the
// one the run stopped in; and the work folder is left empty, so another
// run may use it.
func TestNoTrialFolderIsLeft(t *testing.T) {
	for _, h := range adapter.All() {
		t.Run(string(h.ID), func(t *testing.T) {
			r := newRig(t, 1)
			r.opts.Host, r.opts.Trials = string(h.ID), 2
			r.script.ReadOnly = true
			if s, out, err := r.run(); err != nil || s.Judged != 2 {
				t.Fatalf("run: %+v %v\n%s", s, err, out)
			}
			// The fake did leave what it was asked to.
			if calls := r.calls(); !strings.Contains(strings.Join(calls[len(calls)-1].Around, " "), "home/made-up-locked/inner/file") {
				t.Fatalf("the fake left no read-only folder: %v", calls[len(calls)-1].Around)
			}
			emptyWork(t, r)
			r.opts.Out = filepath.Join(r.base, "out-two")
			r.script.SignIn = true
			if s, out, err := r.run(); err != nil || s.Unjudged[CodeSignIn] != 2 {
				t.Fatalf("run: %+v %v\n%s", s, err, out)
			}
			emptyWork(t, r)
			r.opts.Out = filepath.Join(r.base, "out-three")
			r.script.SignIn, r.script.NoUsage = false, true
			_, _, err := r.run()
			stoppedBy(t, err, CodeNoUsage)
			emptyWork(t, r)
		})
	}
}

// Each trial's folder goes before the next trial starts, whether it was
// judged or unjudged: when a trial is prepared, its folder is the only one
// beside it.
func TestATrialFolderGoesBeforeTheNext(t *testing.T) {
	r := newRig(t, 1)
	r.opts.Condition, r.opts.Trials = task.Eva, 3
	r.script.SignIn, r.script.Session = true, 2
	var beside [][]string
	useArm(t, Arm{Prepare: func(_ context.Context, tr Trial) (Launch, error) {
		entries, err := os.ReadDir(filepath.Dir(tr.Dirs.Root))
		var names []string
		for _, e := range entries {
			names = append(names, e.Name())
		}
		beside = append(beside, names)
		return Launch{}, err
	}})
	s, out, err := r.run()
	if err != nil || s.Judged != 2 || s.Unjudged[CodeSignIn] != 1 {
		t.Fatalf("run: %+v %v\n%s", s, err, out)
	}
	if want := [][]string{{"t1"}, {"t2"}, {"t3"}}; !reflect.DeepEqual(beside, want) {
		t.Fatalf("beside each trial's folder: %v, want %v", beside, want)
	}
	emptyWork(t, r)
}

// --keep-trials keeps every trial's folder, as the plan line and the record
// say, but no grading's copy, which holds the hidden tests; and each is
// closed to all but the driver's user once its trial ends, before the next
// trial is prepared, so that a chat run as another user cannot reach an
// earlier trial's solved work or its host's record of the facts.
func TestKeepTrials(t *testing.T) {
	r := newRig(t, 1)
	r.opts.Condition, r.opts.Trials, r.opts.KeepTrials = task.Eva, 3, true
	var before []os.FileMode // the modes of the earlier trials' folders, at each trial's Prepare
	useArm(t, Arm{Prepare: func(_ context.Context, tr Trial) (Launch, error) {
		for n := 1; n < tr.Number && tr.Options.KeepTrials; n++ {
			info, err := os.Lstat(filepath.Join(filepath.Dir(tr.Dirs.Root), "t"+string(rune('0'+n))))
			if err != nil {
				return Launch{}, err
			}
			before = append(before, info.Mode().Perm())
		}
		return Launch{}, nil
	}})
	s, out, err := r.run()
	if err != nil || s.Judged != 3 || !strings.Contains(strings.SplitN(out, "\n", 2)[0], "; trial folders kept, each closed to others once it ends") {
		t.Fatalf("run: %+v %v\n%s", s, err, out)
	}
	if want := []os.FileMode{0o700, 0o700, 0o700}; !reflect.DeepEqual(before, want) {
		t.Fatalf("the earlier trials' folders' modes: %v, want %v", before, want)
	}
	for n := 1; n <= 3; n++ {
		dir := r.trialDir(s.Run, n)
		if info, err := os.Lstat(dir); err != nil || info.Mode().Perm() != 0o700 {
			t.Fatalf("trial %d's folder: %v %v", n, info, err)
		}
		for _, sub := range []string{"home", "tmp", "bin", "workspace"} {
			if _, err := os.Stat(filepath.Join(dir, sub)); err != nil {
				t.Fatalf("trial %d: %v", n, err)
			}
		}
		if got := list(t, filepath.Join(dir, "grade")); len(got) != 0 {
			t.Fatalf("trial %d's grading folder holds %v", n, got)
		}
	}
	records, err := LoadRuns(r.opts.Out)
	if err != nil || len(records) != 1 || !records[0].Kept {
		t.Fatalf("records %+v: %v", records, err)
	}
	// Without it, the plan line says the folders are removed.
	r.fresh("two")
	r.opts.KeepTrials = false
	if _, out, err := r.run(); err != nil || !strings.Contains(strings.SplitN(out, "\n", 2)[0], "; trial folders removed") {
		t.Fatalf("run: %v\n%s", err, out)
	}
}

// removals counts the tries at removing each trial's folder, and fails the
// first fails of them.
type removals struct {
	mu    sync.Mutex
	tries map[string]int
	fails int
	// end, when set, is called at the try that is the last that fails.
	end func()
}

func (m *removals) removeAll(p string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !strings.HasPrefix(filepath.Base(p), "t") {
		return os.RemoveAll(p) // a grading's copy
	}
	m.tries[p]++
	if m.tries[p] <= m.fails {
		if m.tries[p] == m.fails && m.end != nil {
			m.end()
		}
		return errors.New("made-up: a file is held")
	}
	return os.RemoveAll(p)
}

// A trial's folder that cannot be removed is tried again: one that goes
// at the third try lets the run go on; one that never goes stops the run
// once the limit ends, which the test ends at the second failed try, with
// the record's ended error. No time decides either.
func TestATrialFolderIsTriedAgain(t *testing.T) {
	never := func(ctx context.Context, _ time.Duration) (context.Context, context.CancelFunc) {
		return context.WithCancel(ctx)
	}
	t.Run("goes", func(t *testing.T) {
		r := newRig(t, 1)
		m := &removals{tries: map[string]int{}, fails: 2}
		r.opts.Trials, r.opts.removeAll, r.opts.removeLimit = 2, m.removeAll, never
		s, out, err := r.run()
		if err != nil || s.Judged != 2 {
			t.Fatalf("run: %+v %v\n%s", s, err, out)
		}
		if len(m.tries) != 2 {
			t.Fatalf("tries %v", m.tries)
		}
		for p, n := range m.tries {
			if n != 3 {
				t.Fatalf("%s: %d tries", p, n)
			}
		}
		emptyWork(t, r)
	})
	t.Run("stays", func(t *testing.T) {
		r := newRig(t, 1)
		var cancel context.CancelFunc
		m := &removals{tries: map[string]int{}, fails: 2}
		m.end = func() { cancel() }
		r.opts.Trials, r.opts.removeAll = 3, m.removeAll
		r.opts.removeLimit = func(ctx context.Context, _ time.Duration) (context.Context, context.CancelFunc) {
			c, f := context.WithCancel(ctx)
			cancel = f
			return c, f
		}
		s, out, err := r.run()
		var stopped *Stopped
		if err == nil || errors.As(err, &stopped) || !strings.Contains(err.Error(), "removing its folder") || s.Judged != 1 || s.NotRun != 2 {
			t.Fatalf("run: %+v %v\n%s", s, err, out)
		}
		if len(m.tries) != 1 {
			t.Fatalf("tries %v", m.tries)
		}
		for _, n := range m.tries {
			if n != 2 {
				t.Fatalf("%d tries", n)
			}
		}
		records, lerr := LoadRuns(r.opts.Out)
		if lerr != nil || len(records) != 1 || records[0].Ended != EndedError || records[0].Judged != 1 {
			t.Fatalf("records %+v: %v", records, lerr)
		}
	})
}

// A hidden file's bytes across two of the pieces a file is read in are
// found, wherever the boundary falls in them, in the first piece's end and
// a later one's; bytes that differ in their last byte are not, nor a file
// that is a link to one that holds them.
func TestHoldsAcrossPieces(t *testing.T) {
	pattern := []byte("#!/bin/sh\nmade-up hidden test, 40 bytes\n")
	other := []byte("#!/bin/sh\nmade-up hidden check\n")
	h := &hidden{content: [][]byte{other, pattern}, longest: len(pattern)}
	window := make([]byte, chunk+h.longest)
	l := len(pattern)
	first := chunk + l          // what the first read fills
	second := first + chunk + 1 // and the second, after the l-1 kept
	dir := t.TempDir()
	for _, end := range []int{first + 1, first + l/2, first + l - 1, second + 1, second + l - 1} {
		for _, near := range []bool{false, true} {
			body := bytes.Repeat([]byte("x"), end-l)
			if near {
				body = append(body, pattern[:l-1]...)
				body = append(body, '!')
			} else {
				body = append(body, pattern...)
			}
			body = append(body, "after"...)
			p := filepath.Join(dir, "file")
			if err := os.WriteFile(p, body, 0o644); err != nil {
				t.Fatal(err)
			}
			if in, err := h.holds(p, window); err != nil || in == near {
				t.Fatalf("ending at %d, near %v: %v %v", end, near, in, err)
			}
		}
	}
	link := filepath.Join(dir, "link")
	if err := os.Symlink(filepath.Join(dir, "file"), link); err != nil {
		t.Fatal(err)
	}
	if in, err := h.holds(link, window); in {
		t.Fatalf("a link was followed: %v", err)
	}
}

// A work folder that holds anything, or is no folder, is refused before
// the plan, and nothing runs.
func TestAWorkFolderNotEmptyIsRefused(t *testing.T) {
	r := newRig(t, 1)
	write(t, filepath.Join(r.opts.Work, "left", "behind.txt"), "made-up\n")
	refusedBeforeThePlan(t, r, "--work is not empty")
	r.opts.Work = filepath.Join(r.base, "a-file")
	write(t, r.opts.Work, "made-up\n")
	refusedBeforeThePlan(t, r, "--work is not a folder")
	// An empty one is taken.
	r.opts.Work = filepath.Join(r.base, "empty")
	mkdir(t, r.opts.Work)
	if s, out, err := r.run(); err != nil || s.Judged != 1 {
		t.Fatalf("run: %+v %v\n%s", s, err, out)
	}
}
