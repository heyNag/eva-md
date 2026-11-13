package task

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"time"
)

// Grading is which of a task's hidden tests passed on a workspace.
type Grading struct {
	Pass []string  // the ids of the tests that passed, in the task's order
	Fail []Failure // the tests that failed, in the task's order
}

// Failure is one failed test and the facts it encodes.
type Failure struct {
	Test  string
	Facts []string
}

// Solved reports whether every test passed.
func (g Grading) Solved() bool { return len(g.Fail) == 0 }

// waitDelay bounds how long a test's run is waited for once it is killed.
const waitDelay = 10 * time.Second

// Grade copies the workspace into scratch, which must not exist or be an
// empty folder outside the workspace, lays the hidden/ overlay of the task
// in taskDir over the copy, and runs each test in its repository's copy
// within its time limit. A test passes when it exits 0; one that fails or
// runs out of time fails. A runner that is not installed, or a ctx that
// ends, is an error. It never writes the workspace, and Go runs with no
// proxy, no other toolchain and no workspace file, so nothing is fetched.
func Grade(ctx context.Context, t Task, taskDir, workspace, scratch string) (Grading, error) {
	if err := apart(workspace, scratch); err != nil {
		return Grading{}, fmt.Errorf("task %s: %w", t.ID, err)
	}
	if err := emptyDir(scratch); err != nil {
		return Grading{}, fmt.Errorf("task %s: the grading folder: %w", t.ID, err)
	}
	if err := copyTree(workspace, scratch); err != nil {
		return Grading{}, fmt.Errorf("task %s: copying the workspace: %w", t.ID, err)
	}
	hidden := t
	hidden.Dir = taskDir
	for _, name := range t.Repos {
		if err := layOverlay(hidden, OverlayHidden, name, repoRoot(t, scratch, name)); err != nil {
			return Grading{}, fmt.Errorf("task %s: %w", t.ID, err)
		}
	}
	var g Grading
	for _, x := range t.Tests {
		pass, err := runTest(ctx, x, repoRoot(t, scratch, x.Repo))
		if err != nil {
			return Grading{}, fmt.Errorf("task %s: test %s: %w", t.ID, x.ID, err)
		}
		if pass {
			g.Pass = append(g.Pass, x.ID)
		} else {
			g.Fail = append(g.Fail, Failure{Test: x.ID, Facts: slices.Clone(x.Facts)})
		}
	}
	return g, nil
}

// apart returns an error when one of the folders a and b is, or is inside,
// the other.
func apart(a, b string) error {
	a, errA := filepath.Abs(a)
	b, errB := filepath.Abs(b)
	if err := errors.Join(errA, errB); err != nil {
		return err
	}
	if within(a, b) || within(b, a) {
		return errors.New("the grading folder and the workspace overlap")
	}
	return nil
}

// within reports whether p is dir or inside it; both are absolute.
func within(p, dir string) bool {
	rel, err := filepath.Rel(dir, p)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// copyTree copies the folder src into the folder dst: folders, regular
// files with whether they are executable, and links as links, never
// followed. Anything else, such as a named pipe, is left out.
func copyTree(src, dst string) error {
	return filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, p)
		if err != nil || rel == "." {
			return err
		}
		to := filepath.Join(dst, rel)
		switch {
		case d.IsDir():
			return os.Mkdir(to, 0o755)
		case d.Type()&fs.ModeSymlink != 0:
			target, err := os.Readlink(p)
			if err != nil {
				return err
			}
			return os.Symlink(target, to)
		case d.Type().IsRegular():
			info, err := d.Info()
			if err != nil {
				return err
			}
			data, err := os.ReadFile(p)
			if err != nil {
				return err
			}
			return os.WriteFile(to, data, fileMode(info.Mode()))
		}
		return nil
	})
}

// runTest runs the test x in dir and reports whether it passed.
func runTest(ctx context.Context, x Test, dir string) (bool, error) {
	runner, err := exec.LookPath(x.Run[0])
	if err != nil {
		return false, errors.New("its runner is not installed")
	}
	tctx, cancel := context.WithTimeout(ctx, time.Duration(x.Limit())*time.Second)
	defer cancel()
	cmd := exec.CommandContext(tctx, runner, x.Run[1:]...)
	cmd.Dir = dir
	cmd.Env = testEnv()
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return killGroup(cmd.Process.Pid) }
	cmd.WaitDelay = waitDelay
	err = cmd.Run()
	if cmd.Process != nil {
		// Whatever the test left running in its group goes with it.
		_ = killGroup(cmd.Process.Pid)
	}
	var exit *exec.ExitError
	switch {
	case ctx.Err() != nil:
		return false, ctx.Err()
	case err == nil:
		return true, nil
	case errors.As(err, &exit), tctx.Err() != nil:
		return false, nil
	}
	return false, fmt.Errorf("running it: %w", err)
}

// killGroup kills the process group pid leads.
func killGroup(pid int) error {
	err := syscall.Kill(-pid, syscall.SIGKILL)
	if errors.Is(err, syscall.ESRCH) {
		return os.ErrProcessDone
	}
	return err
}

// testEnv is the environment a test runs in: the caller's, with Go kept
// off the network and to the toolchain installed, and pip off its index.
func testEnv() []string {
	drop := []string{"GOFLAGS", "GOPROXY", "GOTOOLCHAIN", "GOSUMDB", "GOWORK", "GONOPROXY", "GONOSUMDB", "GOPRIVATE", "GOINSECURE"}
	var env []string
	for _, kv := range os.Environ() {
		name, _, _ := strings.Cut(kv, "=")
		if !slices.Contains(drop, name) {
			env = append(env, kv)
		}
	}
	return append(env, "GOPROXY=off", "GOTOOLCHAIN=local", "GOSUMDB=off", "GOWORK=off", "GOFLAGS=", "PIP_NO_INDEX=1", "PYTHONDONTWRITEBYTECODE=1")
}

// Check checks the task offline in scratch, which must not exist or be an
// empty folder: on the start state at least one test fails; with the
// solution/ overlay every test passes; and in a hidden-decision task, with
// the decoy/ overlay, exactly the tests that encode a fact fail.
func Check(ctx context.Context, t Task, reposDir, scratch string) error {
	if err := emptyDir(scratch); err != nil {
		return fmt.Errorf("task %s: the check's folder: %w", t.ID, err)
	}
	start, err := gradeWith(ctx, t, reposDir, scratch, OverlayStart)
	if err != nil {
		return err
	}
	if start.Solved() {
		return fmt.Errorf("task %s: every test passes on the start state", t.ID)
	}
	solution, err := gradeWith(ctx, t, reposDir, scratch, OverlaySolution)
	if err != nil {
		return err
	}
	if !solution.Solved() {
		return fmt.Errorf("task %s: with the solution, test %s fails", t.ID, solution.Fail[0].Test)
	}
	if t.Family != FamilyHidden {
		return nil
	}
	decoy, err := gradeWith(ctx, t, reposDir, scratch, OverlayDecoy)
	if err != nil {
		return err
	}
	for _, x := range t.Tests {
		failed := slices.ContainsFunc(decoy.Fail, func(f Failure) bool { return f.Test == x.ID })
		switch {
		case failed && len(x.Facts) == 0:
			return fmt.Errorf("task %s: with the decoy, test %s fails, though it encodes no fact", t.ID, x.ID)
		case !failed && len(x.Facts) > 0:
			return fmt.Errorf("task %s: with the decoy, test %s passes, though it encodes a fact", t.ID, x.ID)
		}
	}
	return nil
}

// gradeWith prepares a workspace in scratch/<o>/, lays the overlay o over
// it unless it is the start state, and grades it.
func gradeWith(ctx context.Context, t Task, reposDir, scratch, o string) (Grading, error) {
	if o != OverlayStart {
		if _, err := os.Lstat(filepath.Join(t.Dir, o)); err != nil {
			return Grading{}, fmt.Errorf("task %s: it has no %s overlay", t.ID, o)
		}
	}
	ws := filepath.Join(scratch, o, "workspace")
	if err := Prepare(ctx, t, reposDir, ws); err != nil {
		return Grading{}, err
	}
	if o != OverlayStart {
		for _, name := range t.Repos {
			if err := layOverlay(t, o, name, repoRoot(t, ws, name)); err != nil {
				return Grading{}, fmt.Errorf("task %s: %w", t.ID, err)
			}
		}
	}
	return Grade(ctx, t, t.Dir, ws, filepath.Join(scratch, o, "grade"))
}
