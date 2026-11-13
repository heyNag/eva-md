package task

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// hangGuard is how long a test waits for a call that should end on its
// own before it calls the call hung.
const hangGuard = 2 * time.Minute

// loaded loads the one task in the tasks folder dir.
func loaded(t *testing.T, dir string) Task {
	t.Helper()
	tasks, err := Load(dir)
	if err != nil || len(tasks) != 1 {
		t.Fatalf("Load: %d tasks, %v", len(tasks), err)
	}
	return tasks[0]
}

// workspace lays out the task's repositories, with the tree layer over
// them when it is not "", as workdir lays them out, in a new folder.
func workspace(t *testing.T, task Task, layer string) string {
	t.Helper()
	ws := filepath.Join(t.TempDir(), "workspace")
	for _, r := range task.Repos {
		to := ws
		if task.Workdir == WorkdirOutside {
			if err := os.MkdirAll(ws, 0o755); err != nil {
				t.Fatal(err)
			}
			to = filepath.Join(ws, r)
		}
		if err := copyTree(filepath.Join(task.Dir, treeRepos, r), to); err != nil {
			t.Fatal(err)
		}
		if layer != "" {
			if err := lay(filepath.Join(task.Dir, layer, r), to); err != nil {
				t.Fatal(err)
			}
		}
	}
	return ws
}

// digest sums every path, mode and byte under dir, links included.
func digest(t *testing.T, dir string) string {
	t.Helper()
	h := sha256.New()
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(dir, path)
		fmt.Fprintf(h, "%s %v %d\n", rel, info.Mode(), info.ModTime().UnixNano())
		switch {
		case info.Mode()&fs.ModeSymlink != 0:
			target, err := os.Readlink(path)
			if err != nil {
				return err
			}
			fmt.Fprintln(h, target)
		case info.Mode().IsRegular():
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			h.Write(data)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return hex.EncodeToString(h.Sum(nil))
}

// grade runs Grade under a hang guard.
func grade(t *testing.T, task Task, ws, scratch string, limit time.Duration) ([]string, error) {
	t.Helper()
	type result struct {
		failed []string
		err    error
	}
	done := make(chan result, 1)
	go func() {
		failed, err := Grade(context.Background(), task, ws, scratch, limit)
		done <- result{failed, err}
	}()
	select {
	case r := <-done:
		return r.failed, r.err
	case <-time.After(hangGuard):
		t.Fatalf("Grade did not return within %v", hangGuard)
		return nil, nil
	}
}

// Grade copies the workspace and runs the tests on the copy: the
// workspace's bytes, modes and times are as they were, though Python
// writes its byte code beside what it imports, and a hidden test of the
// workspace's own, which would pass, is replaced by the task's. A .git
// folder and a link in the workspace stay out of the copy.
func TestGradeLeavesTheWorkspaceAlone(t *testing.T) {
	task := loaded(t, copyTasks(t, "d04"))
	ws := workspace(t, task, treeSolution)
	writeFile(t, filepath.Join(ws, ".git", "HEAD"), "ref: refs/heads/main\n")
	if err := os.Symlink(filepath.Join(ws, "counters.py"), filepath.Join(ws, "linked.py")); err != nil {
		t.Fatal(err)
	}
	before := digest(t, ws)
	failed, err := grade(t, task, ws, filepath.Join(t.TempDir(), "scratch"), time.Hour)
	if err != nil || len(failed) != 0 {
		t.Fatalf("Grade on the solution: %v, %v", failed, err)
	}
	if after := digest(t, ws); after != before {
		t.Error("Grade changed the workspace")
	}

	start := workspace(t, task, "")
	writeFile(t, filepath.Join(start, "test_hidden.py"), "import unittest\n\n\nclass HiddenTests(unittest.TestCase):\n"+
		"    def test_round_trip(self):\n        pass\n\n    def test_state_file_format(self):\n        pass\n")
	failed, err = grade(t, task, start, filepath.Join(t.TempDir(), "scratch"), time.Hour)
	if err != nil || !slices.Equal(failed, []string{"t1", "t2"}) {
		t.Errorf("Grade on the start with tests of its own that pass: %v, %v", failed, err)
	}

	// The copy a test runs in holds no .git folder and no link, so no test
	// reaches the workspace through one; the workspace holds both still.
	synth := synthetic(t, LangPython, nil, map[string]string{
		"test_copy.py": "import os\nimport unittest\n\n\nclass T(unittest.TestCase):\n" +
			"    def test_copy(self):\n" +
			"        self.assertTrue(os.path.isfile(\"x.py\"))\n" +
			"        self.assertFalse(os.path.lexists(\".git\"))\n" +
			"        self.assertFalse(os.path.lexists(\"linked.py\"))\n" +
			"        self.assertFalse(os.path.lexists(\"linked\"))\n",
	}, []string{"python3", "-m", "unittest", "test_copy.T.test_copy"})
	ws = filepath.Join(t.TempDir(), "workspace")
	writeFile(t, filepath.Join(ws, "x.py"), "\n")
	writeFile(t, filepath.Join(ws, "sub", "y.py"), "\n")
	writeFile(t, filepath.Join(ws, ".git", "HEAD"), "ref: refs/heads/main\n")
	if err := os.Symlink(filepath.Join(ws, "x.py"), filepath.Join(ws, "linked.py")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(ws, "sub"), filepath.Join(ws, "linked")); err != nil {
		t.Fatal(err)
	}
	before = digest(t, ws)
	failed, err = grade(t, synth, ws, filepath.Join(t.TempDir(), "scratch"), time.Hour)
	if err != nil || len(failed) != 0 {
		t.Errorf("a workspace with a .git folder and links: %v, %v", failed, err)
	}
	if digest(t, ws) != before {
		t.Error("Grade changed a workspace with a .git folder and links")
	}
}

// Grade copies each repository from under its name when the chats run
// outside them, and says so, naming no repository, when one is missing.
func TestGradeOutside(t *testing.T) {
	task := loaded(t, copyTasks(t, "h05"))
	ws := workspace(t, task, treeSolution)
	failed, err := grade(t, task, ws, filepath.Join(t.TempDir(), "scratch"), time.Hour)
	if err != nil || len(failed) != 0 {
		t.Fatalf("the solution: %v, %v", failed, err)
	}
	failed, err = grade(t, task, workspace(t, task, ""), filepath.Join(t.TempDir(), "scratch"), time.Hour)
	if err != nil || !slices.Equal(failed, []string{"t1", "t2"}) {
		t.Fatalf("the start: %v, %v", failed, err)
	}
	if err := os.RemoveAll(filepath.Join(ws, "tallyport")); err != nil {
		t.Fatal(err)
	}
	_, err = grade(t, task, ws, filepath.Join(t.TempDir(), "scratch"), time.Hour)
	if err == nil || !strings.Contains(err.Error(), "task h05: the workspace has no folder for repository 2 of 2") || holds(err.Error(), Forbid(task)) {
		t.Errorf("a repository missing: %v", err)
	}
}

// synthetic writes a task of one repository, made for these tests alone,
// with one hidden test per file of hidden, and returns it loaded: it runs
// no rule, so its tests may be anything.
func synthetic(t *testing.T, lang string, repo, hidden map[string]string, runs ...[]string) Task {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "x01")
	for rel, content := range repo {
		writeFile(t, filepath.Join(dir, treeRepos, "synthrepo", rel), content)
	}
	for rel, content := range hidden {
		writeFile(t, filepath.Join(dir, treeHidden, "synthrepo", rel), content)
	}
	task := Task{V: Version, ID: "x01", Family: FamilyHidden, Lang: lang, Repos: []string{"synthrepo"}, Workdir: WorkdirRepo, Dir: dir}
	for i, run := range runs {
		task.Tests = append(task.Tests, Test{ID: fmt.Sprintf("t%d", i+1), Repo: "synthrepo", Run: run})
	}
	return task
}

// A test that blocks until it is killed fails at the limit given to that
// one call of Grade, and the call returns, for a Python test and for go
// test, whose test binary is a child of go's in the same process group.
// The limit holds for each test of one call: a test after one that ran
// out of time has the whole limit again, and so does the next call.
func TestGradeFailsATestOutOfTime(t *testing.T) {
	blocks := map[string]string{
		"test_block.py": "import threading\nimport unittest\n\n\nclass T(unittest.TestCase):\n" +
			"    def test_block(self):\n        threading.Event().wait()\n\n" +
			"    def test_pass(self):\n        pass\n",
	}
	task := synthetic(t, LangPython, map[string]string{"x.py": "\n"}, blocks, []string{"python3", "-m", "unittest", "test_block.T.test_block"})
	failed, err := grade(t, task, filepath.Join(task.Dir, treeRepos, "synthrepo"), filepath.Join(t.TempDir(), "scratch"), 300*time.Millisecond)
	if err != nil || !slices.Equal(failed, []string{"t1"}) {
		t.Errorf("a blocked Python test: %v, %v", failed, err)
	}

	// The limit is long enough for a test that passes, whatever the
	// machine's speed, and the hang guard stays above it.
	task = synthetic(t, LangPython, map[string]string{"x.py": "\n"}, blocks,
		[]string{"python3", "-m", "unittest", "test_block.T.test_block"}, []string{"python3", "-m", "unittest", "test_block.T.test_pass"})
	failed, err = grade(t, task, filepath.Join(task.Dir, treeRepos, "synthrepo"), filepath.Join(t.TempDir(), "scratch"), time.Minute)
	if err != nil || !slices.Equal(failed, []string{"t1"}) {
		t.Errorf("a blocked Python test, then one that passes: %v, %v", failed, err)
	}

	task = synthetic(t, LangGo, map[string]string{"go.mod": "module synthrepo\n\ngo 1.22\n", "x.go": "package synthrepo\n"},
		map[string]string{"block_test.go": "package synthrepo\n\nimport \"testing\"\n\nfunc TestBlock(t *testing.T) { select {} }\n"},
		[]string{"go", "test", "-count=1", "-run", "^TestBlock$", "."})
	failed, err = grade(t, task, filepath.Join(task.Dir, treeRepos, "synthrepo"), filepath.Join(t.TempDir(), "scratch"), 3*time.Second)
	if err != nil || !slices.Equal(failed, []string{"t1"}) {
		t.Errorf("a blocked Go test: %v, %v", failed, err)
	}
}

// When a run is stopped, its whole process group is killed: go test's
// test binary, a child of go's that has written its process id and then
// blocks, does not outlive go. The run is stopped by ending Grade's
// context once the id is written, which kills as the limit does, so no
// time decides when it is stopped.
func TestGradeKillsTheWholeProcessGroup(t *testing.T) {
	pidFile := filepath.Join(t.TempDir(), "pid")
	src := fmt.Sprintf(`package synthrepo

import (
	"os"
	"strconv"
	"testing"
)

func TestBlock(t *testing.T) {
	if err := os.WriteFile(%[1]q+".new", []byte(strconv.Itoa(os.Getpid())), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(%[1]q+".new", %[1]q); err != nil {
		t.Fatal(err)
	}
	select {}
}
`, pidFile)
	task := synthetic(t, LangGo, map[string]string{"go.mod": "module synthrepo\n\ngo 1.22\n", "x.go": "package synthrepo\n"},
		map[string]string{"block_test.go": src}, []string{"go", "test", "-count=1", "-run", "^TestBlock$", "."})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := Grade(ctx, task, filepath.Join(task.Dir, treeRepos, "synthrepo"), filepath.Join(t.TempDir(), "scratch"), time.Hour)
		done <- err
	}()

	guard := time.After(hangGuard)
	pid := 0
	for pid == 0 {
		if data, err := os.ReadFile(pidFile); err == nil {
			if pid, err = strconv.Atoi(string(data)); err != nil {
				t.Fatalf("the test binary wrote %q", data)
			}
			break
		}
		select {
		case err := <-done:
			t.Fatalf("Grade returned before the test binary started: %v", err)
		case <-guard:
			t.Fatalf("the test binary did not start within %v", hangGuard)
		case <-time.After(20 * time.Millisecond):
		}
	}
	// Should the binary outlive Grade, the test does not leave it behind.
	t.Cleanup(func() { _ = syscall.Kill(pid, syscall.SIGKILL) })
	if pid == os.Getpid() {
		t.Fatal("the test binary wrote this process's id")
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Errorf("Grade with its context ended: %v", err)
		}
	case <-guard:
		t.Fatalf("Grade did not return within %v", hangGuard)
	}
	for alive(pid) {
		select {
		case <-guard:
			t.Fatalf("the test binary, a child of go's, was not killed with it")
		case <-time.After(20 * time.Millisecond):
		}
	}
}

// alive reports whether the process pid runs: it exists and, where /proc
// tells, is not a zombie, which a process killed after its parent stays
// until something reaps it.
func alive(pid int) bool {
	if err := syscall.Kill(pid, 0); errors.Is(err, syscall.ESRCH) {
		return false
	}
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return true
	}
	if i := strings.LastIndexByte(string(data), ')'); i >= 0 && i+2 < len(data) {
		return data[i+2] != 'Z' && data[i+2] != 'X'
	}
	return true
}

// alive tells a process that runs from one that has ended.
func TestAlive(t *testing.T) {
	cmd := exec.Command("go", "version")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	pid := cmd.Process.Pid
	if !alive(os.Getpid()) {
		t.Error("this process is not alive")
	}
	if err := cmd.Wait(); err != nil {
		t.Fatal(err)
	}
	if alive(pid) {
		t.Error("a process that has ended and been reaped is alive")
	}
}

// A test runs in an environment built from nothing: PATH holds only the
// runners' links, HOME and TMPDIR are beside it under scratch, go takes
// GOCACHE from go env and reaches no network (GOPROXY=off,
// GOTOOLCHAIN=local) and no go.work, and no variable of the caller's
// reaches it.
func TestGradeBuildsTheEnvironmentFromNothing(t *testing.T) {
	t.Setenv("EVA_PILOT_MADE_UP", "kestrel-made-up")
	t.Setenv("GOFLAGS", "-mod=mod")
	out, err := exec.Command("go", "env", "GOCACHE").Output()
	if err != nil {
		t.Fatal(err)
	}
	cache := strings.TrimSpace(string(out))

	py := synthetic(t, LangPython, map[string]string{"x.py": "\n"}, map[string]string{
		"test_env.py": `import os
import unittest


class T(unittest.TestCase):
    def test_env(self):
        e = os.environ
        bins = e["PATH"].split(os.pathsep)
        self.assertEqual(len(bins), 1)
        self.assertEqual(os.path.basename(bins[0]), "bin")
        self.assertEqual(sorted(os.listdir(bins[0])), ["python3"])
        scratch = os.path.dirname(bins[0])
        self.assertEqual(e["HOME"], os.path.join(scratch, "home"))
        self.assertEqual(e["TMPDIR"], os.path.join(scratch, "tmp"))
        self.assertEqual(os.getcwd(), os.path.join(scratch, "repos", "synthrepo"))
        self.assertEqual(e.get("GOPROXY"), "off")
        self.assertEqual(e.get("GOTOOLCHAIN"), "local")
        self.assertEqual(e.get("GOWORK"), "off")
        self.assertNotIn("EVA_PILOT_MADE_UP", e)
        self.assertNotIn("GOFLAGS", e)
`,
	}, []string{"python3", "-m", "unittest", "test_env.T.test_env"})
	failed, err := grade(t, py, filepath.Join(py.Dir, treeRepos, "synthrepo"), filepath.Join(t.TempDir(), "scratch"), time.Hour)
	if err != nil || len(failed) != 0 {
		t.Errorf("Python's environment: %v, %v", failed, err)
	}

	gotest := fmt.Sprintf(`package synthrepo

import (
	"os"
	"path/filepath"
	"testing"
)

func TestEnv(t *testing.T) {
	// go test puts its own GOROOT/bin before the PATH it was given.
	path := filepath.SplitList(os.Getenv("PATH"))
	bin := path[len(path)-1]
	scratch := filepath.Dir(bin)
	for name, want := range map[string]string{
		"GOPROXY": "off", "GOTOOLCHAIN": "local", "GOWORK": "off", "GOCACHE": %q, "GOFLAGS": "",
		"EVA_PILOT_MADE_UP": "", "HOME": filepath.Join(scratch, "home"), "TMPDIR": filepath.Join(scratch, "tmp"),
	} {
		if got := os.Getenv(name); got != want {
			t.Errorf("%%s is %%q, want %%q", name, got, want)
		}
	}
	if filepath.Base(bin) != "bin" || len(path) > 2 {
		t.Errorf("PATH is %%q", os.Getenv("PATH"))
	}
}
`, cache)
	gt := synthetic(t, LangGo, map[string]string{"go.mod": "module synthrepo\n\ngo 1.22\n", "x.go": "package synthrepo\n"},
		map[string]string{"env_test.go": gotest}, []string{"go", "test", "-count=1", "-run", "^TestEnv$", "."})
	failed, err = grade(t, gt, filepath.Join(gt.Dir, treeRepos, "synthrepo"), filepath.Join(t.TempDir(), "scratch"), time.Hour)
	if err != nil || len(failed) != 0 {
		t.Errorf("Go's environment: %v, %v", failed, err)
	}
}

// A runner not installed is an error that names it, and a test that runs
// something other than a runner is refused.
func TestGradeNeedsItsRunners(t *testing.T) {
	goPath, err := exec.LookPath("go")
	if err != nil {
		t.Fatal(err)
	}
	bin := t.TempDir()
	if err := os.Symlink(goPath, filepath.Join(bin, "go")); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)
	task := loaded(t, copyTasks(t, "d04"))
	_, err = grade(t, task, workspace(t, task, ""), filepath.Join(t.TempDir(), "scratch"), time.Hour)
	if err == nil || !strings.Contains(err.Error(), "task d04: the runner python3 is not installed") || holds(err.Error(), Forbid(task)) {
		t.Errorf("no python3: %v", err)
	}

	task.Tests[0].Run = []string{"sh", "-c", "true"}
	_, err = grade(t, task, workspace(t, task, ""), filepath.Join(t.TempDir(), "scratch"), time.Hour)
	if err == nil || !strings.Contains(err.Error(), "which is no runner") {
		t.Errorf("a shell: %v", err)
	}
}

// The scratch folder must be empty, and apart from the workspace and the
// task.
func TestGradeNeedsAScratchOfItsOwn(t *testing.T) {
	task := loaded(t, copyTasks(t, "d04"))
	ws := workspace(t, task, "")
	full := t.TempDir()
	writeFile(t, filepath.Join(full, "left.txt"), "x\n")
	for name, scratch := range map[string]string{
		"not empty":            full,
		"in the workspace":     filepath.Join(ws, "scratch"),
		"around the workspace": filepath.Dir(ws),
		"in the task":          filepath.Join(task.Dir, "scratch"),
	} {
		before := digest(t, ws)
		if _, err := grade(t, task, ws, scratch, time.Hour); err == nil {
			t.Errorf("%s: Grade ran", name)
		}
		if digest(t, ws) != before {
			t.Errorf("%s: the workspace changed", name)
		}
	}
}

// Grade stops when its context ends, and says so.
func TestGradeStopsWithItsContext(t *testing.T) {
	task := loaded(t, copyTasks(t, "d04"))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := Grade(ctx, task, workspace(t, task, ""), filepath.Join(t.TempDir(), "scratch"), time.Hour)
	if !errors.Is(err, context.Canceled) || !strings.HasPrefix(err.Error(), "task d04: test t1: ") {
		t.Errorf("Grade with an ended context: %v", err)
	}
}
