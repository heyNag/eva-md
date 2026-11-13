package task

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
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

// Grade copies the workspace into scratch/workspace/, where scratch must not
// exist or be an empty folder that neither holds the workspace nor is in
// it, lays the hidden/ overlay of the task in taskDir over the copy, and
// runs each test in its repository's copy within its time limit. A test
// passes when it exits 0; one that fails or runs out of time fails, and so
// does one that what the chat left cannot run: its repository's folder
// missing, or its runner, given as a path in the repository, missing or
// not runnable (see runTest). A runner that is not installed on the
// machine, or a ctx that ends, is an error. A workspace given by a link is
// graded from where the link leads. It never
// writes the workspace. Tests run in an environment built from nothing
// (see testEnv), with a home and a temporary folder of their own in
// scratch, so no secret of the caller's reaches the code under test and
// nothing is fetched.
func Grade(ctx context.Context, t Task, taskDir, workspace, scratch string) (Grading, error) {
	// The workspace is copied from where its links lead, so that one given
	// by a link is copied whole, not as the link alone.
	workspace, err := apart(workspace, scratch)
	if err != nil {
		return Grading{}, fmt.Errorf("task %s: %w", t.ID, err)
	}
	if err := emptyDir(scratch); err != nil {
		return Grading{}, fmt.Errorf("task %s: the grading folder: %w", t.ID, err)
	}
	cp, home, tmp := filepath.Join(scratch, "workspace"), filepath.Join(scratch, "home"), filepath.Join(scratch, "tmp")
	for _, dir := range []string{cp, home, tmp} {
		if err := os.Mkdir(dir, 0o755); err != nil {
			return Grading{}, fmt.Errorf("task %s: %w", t.ID, err)
		}
	}
	if err := GoTelemetryOff(home); err != nil {
		return Grading{}, fmt.Errorf("task %s: %w", t.ID, pathless(err))
	}
	if err := copyTree(workspace, cp); err != nil {
		return Grading{}, fmt.Errorf("task %s: copying the workspace: %w", t.ID, pathless(err))
	}
	hidden := t
	hidden.Dir = taskDir
	for _, name := range t.Repos {
		if err := layOverlay(hidden, OverlayHidden, name, repoRoot(t, cp, name)); err != nil {
			return Grading{}, fmt.Errorf("task %s: %w", t.ID, err)
		}
	}
	env := testEnv(home, tmp)
	var g Grading
	for _, x := range t.Tests {
		pass, err := runTest(ctx, x, repoRoot(t, cp, x.Repo), env)
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

// GoTelemetryOff turns Go's telemetry off for a go command run with the
// home folder home: it writes off to the mode file Go reads in the
// configuration folder there (os.UserConfigDir's: .config, or on macOS
// Library/Application Support), making its folders. Otherwise a go command
// keeps counters in that folder and may leave a process behind, outside
// its process group, that writes into it after the command has ended.
func GoTelemetryOff(home string) error {
	config := filepath.Join(home, ".config")
	if runtime.GOOS == "darwin" {
		config = filepath.Join(home, "Library", "Application Support")
	}
	dir := filepath.Join(config, "go", "telemetry")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "mode"), []byte("off\n"), 0o644)
}

// apart returns the workspace ws made absolute with its links resolved, or
// an error when it and the grading folder scratch overlap: when one is, or
// is inside, the other, links resolved.
func apart(ws, scratch string) (string, error) {
	ws, errW := resolve(ws)
	scratch, errS := resolve(scratch)
	if err := errors.Join(errW, errS); err != nil {
		return "", pathless(err)
	}
	if within(ws, scratch) || within(scratch, ws) {
		return "", errors.New("the grading folder and the workspace overlap")
	}
	return ws, nil
}

// resolve returns p made absolute with its links resolved, as far as it
// exists.
func resolve(p string) (string, error) {
	p, err := filepath.Abs(p)
	if err != nil {
		return "", err
	}
	real, err := filepath.EvalSymlinks(p)
	switch {
	case err == nil:
		return real, nil
	case !errors.Is(err, fs.ErrNotExist) || filepath.Dir(p) == p:
		return "", err
	}
	parent, err := resolve(filepath.Dir(p))
	if err != nil {
		return "", err
	}
	return filepath.Join(parent, filepath.Base(p)), nil
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

// runTest runs the test x in dir, its repository's folder in the copy,
// with the environment env and reports whether it passed.
//
// What the chat may have changed fails the test and is never an error, so
// that a driver never drops or repeats a trial for what the chat did: a
// repository folder the copy lacks, or holds as a link or a file, and a
// runner given as a path relative to the repository (a file of the copy)
// that is missing or cannot be started. A runner named with no slash is
// looked up on env's PATH, and one given as an absolute path is used as it
// is; either, when missing, is an error, as the machine lacks it.
func runTest(ctx context.Context, x Test, dir string, env []string) (bool, error) {
	if info, err := os.Lstat(dir); err != nil || !info.IsDir() {
		return false, nil
	}
	runner := x.Run[0]
	inRepo := false
	switch {
	case !strings.Contains(runner, "/"):
		found, err := lookPath(runner, env)
		if err != nil {
			return false, errors.New("its runner is not installed")
		}
		runner = found
	case filepath.IsAbs(runner):
		if info, err := os.Stat(runner); err != nil || info.IsDir() {
			return false, errors.New("its runner is not installed")
		}
	default:
		inRepo = true
		runner = filepath.Join(dir, runner)
		if info, err := os.Stat(runner); err != nil || info.IsDir() {
			return false, nil
		}
	}
	tctx, cancel := context.WithTimeout(ctx, time.Duration(x.Limit())*time.Second)
	defer cancel()
	cmd := exec.CommandContext(tctx, runner, x.Run[1:]...)
	cmd.Dir = dir
	cmd.Env = env
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return killGroup(cmd.Process.Pid) }
	cmd.WaitDelay = waitDelay
	err := cmd.Run()
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
	case errors.As(err, &exit), tctx.Err() != nil, inRepo:
		return false, nil
	}
	return false, fmt.Errorf("running it: %w", pathless(err))
}

// lookPath finds the executable name on the PATH env gives.
func lookPath(name string, env []string) (string, error) {
	for _, kv := range env {
		path, ok := strings.CutPrefix(kv, "PATH=")
		if !ok {
			continue
		}
		for _, dir := range filepath.SplitList(path) {
			if !filepath.IsAbs(dir) {
				continue
			}
			p := filepath.Join(dir, name)
			if info, err := os.Stat(p); err == nil && !info.IsDir() && info.Mode().Perm()&0o111 != 0 {
				return p, nil
			}
		}
	}
	return "", exec.ErrNotFound
}

// killGroup kills the process group pid leads.
func killGroup(pid int) error {
	err := syscall.Kill(-pid, syscall.SIGKILL)
	if errors.Is(err, syscall.ESRCH) {
		return os.ErrProcessDone
	}
	return err
}

// testEnv is the whole environment a test runs in, built from nothing:
// the caller's PATH, a home and a temporary folder of the grading's own,
// Go's build cache where the caller's is, Go kept off the network, to the
// toolchain installed and out of any workspace file, and pip off its
// index. No other variable of the caller's reaches it.
func testEnv(home, tmp string) []string {
	env := []string{
		"PATH=" + os.Getenv("PATH"),
		"HOME=" + home,
		"TMPDIR=" + tmp,
		"LC_ALL=C",
		"GOPROXY=off",
		"GOSUMDB=off",
		"GOTOOLCHAIN=local",
		"GOWORK=off",
		"GOFLAGS=",
		"PIP_NO_INDEX=1",
		"PYTHONDONTWRITEBYTECODE=1",
	}
	if cache := goCache(); cache != "" {
		env = append(env, "GOCACHE="+cache)
	}
	return env
}

// goCache is the caller's Go build cache: GOCACHE when set, else what Go
// says it is, else "" when there is no Go.
var goCache = sync.OnceValue(func() string {
	if c := os.Getenv("GOCACHE"); c != "" {
		return c
	}
	out, err := exec.Command("go", "env", "GOCACHE").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
})

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
