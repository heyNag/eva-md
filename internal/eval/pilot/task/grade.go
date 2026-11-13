package task

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"time"
)

// runners are the commands a test may run, by language.
var runners = map[string]string{LangGo: "go", LangPython: "python3"}

// waitDelay bounds how long a run whose limit has passed may take to
// exit once its process group is killed.
var waitDelay = 10 * time.Second

// Grade copies the repositories as workdir lays them out in workspace
// (workspace itself for repo, workspace/<name> for each for outside) into
// scratch, which must be empty or not exist, leaving out .git folders and
// anything that is not a folder or a plain file; it lays hidden/ over the
// copy, runs each test in its repository within limit, and returns the
// ids of the tests that did not exit 0, in the task's order. A test out
// of time fails. It never writes workspace.
//
// Tests run in an environment built from nothing: PATH, HOME and TMPDIR
// under scratch, PATH holding only links to the runners; GOCACHE as
// "go env GOCACHE" gives it; GOPROXY=off and GOTOOLCHAIN=local, so go
// reaches no network; and GOWORK=off, so no go.work above scratch joins
// in. A runner not installed is an error, and so is ctx ending.
func Grade(ctx context.Context, t Task, workspace, scratch string, limit time.Duration) (failed []string, err error) {
	var needs []string
	for _, test := range t.Tests {
		if !slices.Contains(t.Repos, test.Repo) || len(test.Run) == 0 {
			return nil, t.errorf("test %s has no repository or nothing to run", test.ID)
		}
		needs = append(needs, test.Run[0])
	}
	s, err := newSandbox(ctx, t, scratch, needs, workspace, t.Dir)
	if err != nil {
		return nil, err
	}
	for i, r := range t.Repos {
		src := workspace
		if t.Workdir == WorkdirOutside {
			src = filepath.Join(workspace, r)
		}
		src, err := resolved(src)
		if info, statErr := os.Stat(src); err != nil || statErr != nil || !info.IsDir() {
			return nil, t.errorf("the workspace has no folder for repository %d of %d", i+1, len(t.Repos))
		}
		if err := copyTree(src, s.repo(r)); err != nil {
			return nil, t.errorf("cannot copy repository %d of the workspace: %s", i+1, bare(err))
		}
		if err := lay(filepath.Join(t.Dir, treeHidden, r), s.repo(r)); err != nil {
			return nil, t.errorf("cannot lay the hidden tests over repository %d: %s", i+1, bare(err))
		}
	}
	for _, test := range t.Tests {
		ok, err := s.run(ctx, s.repo(test.Repo), test.Run, limit)
		if err != nil && ctx.Err() != nil {
			return nil, fmt.Errorf("task %s: test %s: %w", t.ID, test.ID, ctx.Err())
		}
		if err != nil {
			return nil, t.errorf("test %s: %s", test.ID, bare(err))
		}
		if !ok {
			failed = append(failed, test.ID)
		}
	}
	return failed, nil
}

// errorf returns an error that names the task and says what went wrong
// running it.
func (t Task) errorf(format string, args ...any) error {
	return fmt.Errorf("task %s: %s", t.ID, fmt.Sprintf(format, args...))
}

// sandbox is a scratch folder set up to run tests in.
type sandbox struct {
	dir string
	bin string
	env []string
}

func (s *sandbox) repo(name string) string { return filepath.Join(s.dir, "repos", name) }

// newSandbox makes scratch, which must be empty or not exist and must not
// overlap any of avoid, into a sandbox holding links to the runners
// needed, each found on the invoking PATH.
func newSandbox(ctx context.Context, t Task, scratch string, needs []string, avoid ...string) (*sandbox, error) {
	dir, err := resolved(scratch)
	if err != nil {
		return nil, t.errorf("cannot resolve the scratch folder: %s", bare(err))
	}
	for _, a := range avoid {
		if a == "" {
			continue
		}
		a, err := resolved(a)
		if err != nil {
			return nil, t.errorf("cannot resolve a folder: %s", bare(err))
		}
		if within(dir, a) || within(a, dir) {
			return nil, t.errorf("the scratch folder overlaps the workspace or the task")
		}
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, t.errorf("cannot make the scratch folder: %s", bare(err))
	}
	if entries, err := os.ReadDir(dir); err != nil || len(entries) > 0 {
		return nil, t.errorf("the scratch folder is not empty")
	}
	s := &sandbox{dir: dir, bin: filepath.Join(dir, "bin")}
	for _, sub := range []string{"bin", "home", "tmp", "repos"} {
		if err := os.Mkdir(filepath.Join(dir, sub), 0o700); err != nil {
			return nil, t.errorf("cannot make the scratch folder: %s", bare(err))
		}
	}
	s.env = []string{"PATH=" + s.bin, "HOME=" + filepath.Join(dir, "home"), "TMPDIR=" + filepath.Join(dir, "tmp"),
		"GOPROXY=off", "GOTOOLCHAIN=local", "GOWORK=off"}
	linked := map[string]bool{}
	for _, name := range needs {
		if linked[name] {
			continue
		}
		if !slices.Contains([]string{runners[LangGo], runners[LangPython]}, name) {
			return nil, t.errorf("a test runs %q, which is no runner", name)
		}
		path, err := exec.LookPath(name)
		if err == nil {
			path, err = filepath.Abs(path)
		}
		if err != nil {
			return nil, t.errorf("the runner %s is not installed", name)
		}
		if err := os.Symlink(path, filepath.Join(s.bin, name)); err != nil {
			return nil, t.errorf("cannot link the runner %s: %s", name, bare(err))
		}
		linked[name] = true
		if name == runners[LangGo] {
			out, err := exec.CommandContext(ctx, path, "env", "GOCACHE").Output()
			cache := strings.TrimSpace(string(out))
			if err != nil || cache == "" {
				return nil, t.errorf("go env GOCACHE gives no build cache")
			}
			s.env = append(s.env, "GOCACHE="+cache)
		}
	}
	return s, nil
}

// resolved returns path made absolute with its links resolved; of a path
// that does not exist yet, the part that does is resolved.
func resolved(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	real, err := filepath.EvalSymlinks(abs)
	if errors.Is(err, fs.ErrNotExist) && filepath.Dir(abs) != abs {
		parent, err := resolved(filepath.Dir(abs))
		if err != nil {
			return "", err
		}
		return filepath.Join(parent, filepath.Base(abs)), nil
	}
	return real, err
}

// within reports whether path is dir or under it.
func within(path, dir string) bool {
	rel, err := filepath.Rel(dir, path)
	return err == nil && (rel == "." || filepath.IsLocal(rel))
}

// run runs argv, whose first word is a runner linked in the sandbox, in
// dir, within limit, and reports whether it exited 0. The run is its own
// process group, all of which is killed when the limit passes. An error
// means it could not start, or ctx ended.
func (s *sandbox) run(ctx context.Context, dir string, argv []string, limit time.Duration) (bool, error) {
	rctx, cancel := context.WithTimeout(ctx, limit)
	defer cancel()
	cmd := exec.CommandContext(rctx, filepath.Join(s.bin, argv[0]), argv[1:]...)
	cmd.Dir = dir
	cmd.Env = s.env
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	cmd.WaitDelay = waitDelay
	err := cmd.Run()
	var exit *exec.ExitError
	switch {
	case ctx.Err() != nil:
		return false, ctx.Err()
	case err == nil:
		return true, nil
	case errors.As(err, &exit), rctx.Err() != nil:
		return false, nil
	}
	return false, err
}

// copyTree copies the folders and plain files under src to dst, leaving
// out .git folders and anything else. dst must not exist.
func copyTree(src, dst string) error {
	return filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		to := filepath.Join(dst, rel)
		switch {
		case d.IsDir() && d.Name() == ".git" && rel != ".":
			return filepath.SkipDir
		case d.IsDir():
			return os.Mkdir(to, 0o755)
		case d.Type().IsRegular():
			return copyFile(path, to)
		}
		return nil
	})
}

// lay copies the plain files under src over dst, making the folders they
// need; a missing src lays nothing. A link or other file under src, or a
// file of src where dst has a folder, is an error.
func lay(src, dst string) error {
	if _, err := os.Lstat(src); errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	return filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		to := filepath.Join(dst, rel)
		switch {
		case d.IsDir():
			return os.MkdirAll(to, 0o755)
		case !d.Type().IsRegular():
			return errors.New("an overlay holds a link or a file that is not plain")
		}
		if info, err := os.Lstat(to); err == nil && !info.Mode().IsRegular() {
			return errors.New("an overlay's file stands where the repository has something else")
		}
		return copyFile(path, to)
	})
}

// copyFile copies the plain file src to dst, with its permission bits,
// replacing dst.
func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	info, err := in.Stat()
	if err != nil {
		return err
	}
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, info.Mode().Perm()|0o200)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}
