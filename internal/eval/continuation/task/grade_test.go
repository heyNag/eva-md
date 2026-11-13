package task

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// prepared builds the fixture task's workspace with the overlay o laid over
// it after the start state ("" for none), and returns the task and the
// workspace.
func prepared(t *testing.T, name, o string) (Task, string) {
	t.Helper()
	x := load(t, name)
	ws := filepath.Join(tempDir(t), "ws")
	if err := Prepare(context.Background(), x, repos, ws); err != nil {
		t.Fatal(err)
	}
	if o != "" {
		for _, r := range x.Repos {
			if err := layOverlay(x, o, r, repoRoot(x, ws, r)); err != nil {
				t.Fatal(err)
			}
		}
	}
	return x, ws
}

func TestGradeFixtures(t *testing.T) {
	pass := func(ids ...string) []string { return ids }
	cases := []struct {
		task, overlay string
		want          Grading
	}{
		{"handoff-split", "", Grading{Fail: []Failure{{"split-spreads", nil}, {"split-refuses-zero", []string{"f-zero"}}}}},
		{"handoff-split", OverlaySolution, Grading{Pass: pass("split-spreads", "split-refuses-zero")}},
		{"hidden-parse", "", Grading{Fail: []Failure{{"parse-reads", nil}, {"parse-errors", []string{"f-prefix"}}}}},
		{"hidden-parse", OverlaySolution, Grading{Pass: pass("parse-reads", "parse-errors")}},
		{"hidden-parse", OverlayDecoy, Grading{Pass: pass("parse-reads"), Fail: []Failure{{"parse-errors", []string{"f-prefix"}}}}},
		{"control-max", "", Grading{Fail: []Failure{{"max-largest", nil}, {"max-empty", nil}}}},
		{"control-max", OverlaySolution, Grading{Pass: pass("max-largest", "max-empty")}},
	}
	for _, c := range cases {
		t.Run(c.task+"/"+c.overlay, func(t *testing.T) {
			x, ws := prepared(t, c.task, c.overlay)
			before := snapshot(t, ws)
			g, err := Grade(context.Background(), x, x.Dir, ws, filepath.Join(tempDir(t), "grade"))
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(g, c.want) {
				t.Fatalf("Grade = %+v, want %+v", g, c.want)
			}
			if g.Solved() != (len(c.want.Fail) == 0) {
				t.Fatal("Solved disagrees with the failures")
			}
			if after := snapshot(t, ws); !slices.Equal(before, after) {
				t.Fatalf("Grade changed the workspace:\n%v\n%v", before, after)
			}
			if got := tree(t, ws); slices.ContainsFunc(got, func(p string) bool { return strings.Contains(p, "hidden_test") }) {
				t.Fatalf("a hidden file is in the workspace: %v", got)
			}
		})
	}
}

func TestCheckFixtures(t *testing.T) {
	tasks, err := LoadAll(fixtures)
	if err != nil {
		t.Fatal(err)
	}
	for _, x := range tasks {
		if err := Check(context.Background(), x, repos, filepath.Join(tempDir(t), "check")); err != nil {
			t.Errorf("Check: %v", err)
		}
	}
}

func TestCheckFindsABrokenTask(t *testing.T) {
	cases := []struct {
		name, fixture string
		change        func(t *testing.T, dir string)
		want          string
	}{
		{"the start state already passes", "control-max", func(t *testing.T, dir string) {
			if err := copyTree(filepath.Join(dir, "solution"), mkdir(t, filepath.Join(dir, "start"))); err != nil {
				t.Fatal(err)
			}
		}, "every test passes on the start state"},
		{"the solution fails a test", "control-max", func(t *testing.T, dir string) {
			write(t, dir, "solution/ledgerkit/max.go", "package ledgerkit\n\nfunc Max(amounts []int) (int, bool) { return 0, len(amounts) > 0 }\n")
		}, "with the solution, test max-largest fails"},
		{"no solution", "control-max", func(t *testing.T, dir string) {
			if err := os.RemoveAll(filepath.Join(dir, "solution")); err != nil {
				t.Fatal(err)
			}
		}, "no solution overlay"},
		{"the decoy passes a test that encodes a fact", "hidden-parse", func(t *testing.T, dir string) {
			if err := os.RemoveAll(filepath.Join(dir, "decoy")); err != nil {
				t.Fatal(err)
			}
			if err := copyTree(filepath.Join(dir, "solution"), mkdir(t, filepath.Join(dir, "decoy"))); err != nil {
				t.Fatal(err)
			}
		}, "test parse-errors passes, though it encodes a fact"},
		{"the decoy fails a test that encodes none", "hidden-parse", func(t *testing.T, dir string) {
			write(t, dir, "decoy/ledgerkit/parse.go", "package ledgerkit\n\nimport \"errors\"\n\nfunc Parse(s string) (int, error) { return 0, errors.New(\"no\") }\n")
		}, "test parse-reads fails, though it encodes no fact"},
		{"no decoy", "hidden-parse", func(t *testing.T, dir string) {
			if err := os.RemoveAll(filepath.Join(dir, "decoy")); err != nil {
				t.Fatal(err)
			}
		}, "no decoy overlay"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir := copyFixture(t, c.fixture)
			c.change(t, dir)
			x, err := Load(dir)
			if err != nil {
				t.Fatal(err)
			}
			err = Check(context.Background(), x, repos, filepath.Join(tempDir(t), "check"))
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("Check = %v, want an error saying %q", err, c.want)
			}
		})
	}
}

// stubTask is a task whose tests run the test binary as a stub runner, one
// test for each stub named, in a workspace of one made-up file. The stub
// that blocks has a limit of 1 second; the others the default, so that
// only their exit decides them, however slowly the machine starts them.
func stubTask(t *testing.T, stubs ...string) (Task, string) {
	t.Helper()
	x := base(t)
	x.Tests = nil
	for _, s := range stubs {
		limit := 0
		if s == stubBlock {
			limit = 1
		}
		x.Tests = append(x.Tests, Test{ID: s, Repo: "ledgerkit", Run: []string{os.Args[0], s}, LimitS: limit})
	}
	ws := tempDir(t)
	write(t, ws, "a.txt", "x\n")
	return x, ws
}

func TestGradeTimeLimit(t *testing.T) {
	x, ws := stubTask(t, stubBlock, stubPass, stubFail)
	g, err := Grade(context.Background(), x, tempDir(t), ws, filepath.Join(tempDir(t), "grade"))
	if err != nil {
		t.Fatal(err)
	}
	want := Grading{Pass: []string{stubPass}, Fail: []Failure{{stubBlock, nil}, {stubFail, nil}}}
	if !reflect.DeepEqual(g, want) {
		t.Fatalf("Grade = %+v, want %+v", g, want)
	}
}

func TestGradeEndedContextIsAnError(t *testing.T) {
	x, ws := stubTask(t, stubPass)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Grade(ctx, x, tempDir(t), ws, filepath.Join(tempDir(t), "grade")); err == nil {
		t.Fatal("a grading whose context had ended gave a result")
	}
}

func TestGradeRunnerNotInstalled(t *testing.T) {
	x, ws := stubTask(t, stubPass)
	x.Tests[0].Run = []string{"eva-md-no-such-runner", "test"}
	if _, err := Grade(context.Background(), x, tempDir(t), ws, filepath.Join(tempDir(t), "grade")); err == nil || !strings.Contains(err.Error(), "not installed") {
		t.Fatalf("Grade = %v, want an error saying the runner is not installed", err)
	}
}

func TestGradeRunsInAnEnvironmentOfItsOwn(t *testing.T) {
	x, ws := stubTask(t, stubPass)
	write(t, ws, "go.mod", "module example.com/ledgerkit\n\ngo 1.22\n")
	write(t, ws, "env_test.go", `package ledgerkit

import (
	"os"
	"path/filepath"
	"testing"
)

func TestEnv(t *testing.T) {
	for k, want := range map[string]string{"GOPROXY": "off", "GOTOOLCHAIN": "local", "GOWORK": "off", "GOFLAGS": "", "EVA_TASK_SECRET": ""} {
		if got := os.Getenv(k); got != want {
			t.Errorf("%s = %q, want %q", k, got, want)
		}
	}
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	scratch := filepath.Dir(wd)
	if filepath.Base(wd) != "workspace" || os.Getenv("HOME") != filepath.Join(scratch, "home") || os.Getenv("TMPDIR") != filepath.Join(scratch, "tmp") {
		t.Errorf("ran in %s with HOME %s and TMPDIR %s", wd, os.Getenv("HOME"), os.Getenv("TMPDIR"))
	}
}
`)
	x.Tests[0].Run = []string{"go", "test", "-count=1", "-run", "^TestEnv$", "."}
	t.Setenv("GOPROXY", "https://proxy.example.invalid")
	t.Setenv("GOFLAGS", "-mod=vendor")
	t.Setenv("GOTOOLCHAIN", "go1.99.0")
	t.Setenv("EVA_TASK_SECRET", "made-up-secret")
	g, err := Grade(context.Background(), x, tempDir(t), ws, filepath.Join(tempDir(t), "grade"))
	if err != nil {
		t.Fatal(err)
	}
	if !g.Solved() {
		t.Fatalf("the test saw the caller's environment: %+v", g)
	}

	var names []string
	for _, kv := range testEnv("/h", "/t") {
		name, _, _ := strings.Cut(kv, "=")
		names = append(names, name)
	}
	slices.Sort(names)
	want := []string{"GOCACHE", "GOFLAGS", "GOPROXY", "GOSUMDB", "GOTOOLCHAIN", "GOWORK", "HOME", "LC_ALL", "PATH", "PIP_NO_INDEX", "PYTHONDONTWRITEBYTECODE", "TMPDIR"}
	if !slices.Equal(names, want) {
		t.Fatalf("a test's environment holds %v, want %v", names, want)
	}
}

func TestGradeRunnerByPath(t *testing.T) {
	x, ws := stubTask(t, stubPass)
	if err := os.MkdirAll(filepath.Join(ws, "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(os.Args[0], filepath.Join(ws, "bin", "stub")); err != nil {
		t.Fatal(err)
	}
	x.Tests[0].Run = []string{"bin/stub", stubPass}
	g, err := Grade(context.Background(), x, tempDir(t), ws, filepath.Join(tempDir(t), "grade"))
	if err != nil || !g.Solved() {
		t.Fatalf("a runner relative to the repository: %+v, %v", g, err)
	}
	x.Tests[0].Run = []string{filepath.Join(tempDir(t), "none"), stubPass}
	if _, err := Grade(context.Background(), x, tempDir(t), ws, filepath.Join(tempDir(t), "grade")); err == nil || !strings.Contains(err.Error(), "not installed") {
		t.Fatalf("a missing runner by an absolute path: %v", err)
	}
}

// TestGradeFailsWhatTheChatBroke holds Grade to failing, never erring on,
// a test that what the chat left cannot run, so that a driver never drops
// or repeats a trial for it.
func TestGradeFailsWhatTheChatBroke(t *testing.T) {
	cases := []struct {
		name   string
		change func(t *testing.T, x *Task, ws string)
	}{
		{"a repository folder the workspace lacks", func(t *testing.T, x *Task, ws string) {
			x.Workdir = WorkdirOutside
		}},
		{"a repository that is a file", func(t *testing.T, x *Task, ws string) {
			x.Workdir = WorkdirOutside
			write(t, ws, "ledgerkit", "x\n")
		}},
		{"a repository that is a link out of the workspace", func(t *testing.T, x *Task, ws string) {
			x.Workdir = WorkdirOutside
			if err := os.Symlink(tempDir(t), filepath.Join(ws, "ledgerkit")); err != nil {
				t.Fatal(err)
			}
		}},
		{"a runner in the repository that is missing", func(t *testing.T, x *Task, ws string) {
			x.Tests[0].Run = []string{"bin/none", stubPass}
		}},
		{"a runner in the repository that is a folder", func(t *testing.T, x *Task, ws string) {
			mkdir(t, filepath.Join(ws, "bin", "run"))
			x.Tests[0].Run = []string{"bin/run", stubPass}
		}},
		{"a runner in the repository that cannot be started", func(t *testing.T, x *Task, ws string) {
			write(t, ws, "bin/run", "not a program\n")
			x.Tests[0].Run = []string{"bin/run", stubPass}
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			x, ws := stubTask(t, stubPass)
			c.change(t, &x, ws)
			g, err := Grade(context.Background(), x, tempDir(t), ws, filepath.Join(tempDir(t), "grade"))
			want := Grading{Fail: []Failure{{stubPass, nil}}}
			if err != nil || !reflect.DeepEqual(g, want) {
				t.Fatalf("Grade = %+v, %v, want %+v and no error", g, err, want)
			}
		})
	}
}

func TestGradeThroughALink(t *testing.T) {
	x, ws := prepared(t, "control-max", OverlaySolution)
	link := filepath.Join(tempDir(t), "link")
	if err := os.Symlink(ws, link); err != nil {
		t.Fatal(err)
	}
	before := snapshot(t, ws)
	g, err := Grade(context.Background(), x, x.Dir, link, filepath.Join(tempDir(t), "grade"))
	if err != nil {
		t.Fatal(err)
	}
	if want := (Grading{Pass: []string{"max-largest", "max-empty"}}); !reflect.DeepEqual(g, want) {
		t.Fatalf("Grade through a link = %+v, want %+v", g, want)
	}
	if after := snapshot(t, ws); !slices.Equal(before, after) {
		t.Fatal("Grade through a link changed the workspace")
	}
}

// hangGuard bounds how long a test waits for what should come at once.
const hangGuard = 60 * time.Second

// TestGradeKillsWhatATestLeaves holds Grade to killing every process a
// test started, in the test's process group, whether the test ends by
// itself or is killed: go test, for one, runs its test binary as a child.
func TestGradeKillsWhatATestLeaves(t *testing.T) {
	t.Run("a test that ends by itself", func(t *testing.T) {
		x, ws := stubTask(t, stubPass)
		pidFile := filepath.Join(tempDir(t), "pid")
		x.Tests[0].Run = []string{os.Args[0], stubGroup, pidFile, "exit"}
		g, err := Grade(context.Background(), x, tempDir(t), ws, filepath.Join(tempDir(t), "grade"))
		if err != nil || !g.Solved() {
			t.Fatalf("Grade = %+v, %v", g, err)
		}
		pid, ok := readPid(t, pidFile)
		if !ok {
			t.Fatal("the stub passed without writing its child's pid")
		}
		waitGone(t, pid)
	})
	t.Run("a test that is killed", func(t *testing.T) {
		x, ws := stubTask(t, stubPass)
		pidFile := filepath.Join(tempDir(t), "pid")
		x.Tests[0].Run = []string{os.Args[0], stubGroup, pidFile, "block"}
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		taskDir, scratch := tempDir(t), filepath.Join(tempDir(t), "grade")
		done := make(chan error, 1)
		go func() {
			_, err := Grade(ctx, x, taskDir, ws, scratch)
			done <- err
		}()
		guard := time.After(hangGuard)
		tick := time.NewTicker(10 * time.Millisecond)
		defer tick.Stop()
		var pid int
		for {
			p, ok := readPid(t, pidFile)
			if ok {
				pid = p
				break
			}
			select {
			case err := <-done:
				t.Fatalf("Grade ended before the stub's child ran: %v", err)
			case <-guard:
				t.Fatal("the stub's child never ran")
			case <-tick.C:
			}
		}
		cancel()
		select {
		case err := <-done:
			if err == nil {
				t.Fatal("a grading whose context ended gave a result")
			}
		case <-time.After(hangGuard):
			t.Fatal("Grade did not return once its context ended")
		}
		waitGone(t, pid)
	})
}

// readPid reads the pid the stub-group runner wrote, if it has, and kills
// that process when the test ends, should Grade have left it.
func readPid(t *testing.T, pidFile string) (int, bool) {
	t.Helper()
	data, err := os.ReadFile(pidFile)
	if errors.Is(err, fs.ErrNotExist) {
		return 0, false
	}
	if err != nil {
		t.Fatal(err)
	}
	pid, err := strconv.Atoi(string(data))
	if err != nil || pid <= 1 {
		t.Fatalf("the stub wrote %q as its child's pid", data)
	}
	t.Cleanup(func() {
		if !gone(pid) {
			_ = syscall.Kill(pid, syscall.SIGKILL)
		}
	})
	return pid, true
}

// waitGone waits, within the hang guard, until the process pid is gone.
func waitGone(t *testing.T, pid int) {
	t.Helper()
	guard := time.After(hangGuard)
	tick := time.NewTicker(10 * time.Millisecond)
	defer tick.Stop()
	for !gone(pid) {
		select {
		case <-guard:
			t.Fatalf("process %d, which a test started, outlived the grading", pid)
		case <-tick.C:
		}
	}
}

// gone reports whether the process pid has ended: it no longer exists, or
// on Linux it is a zombie no one has reaped yet.
func gone(pid int) bool {
	if errors.Is(syscall.Kill(pid, 0), syscall.ESRCH) {
		return true
	}
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return false
	}
	i := bytes.LastIndexByte(data, ')')
	return i >= 0 && i+2 < len(data) && data[i+2] == 'Z'
}

func TestGradeNeverWritesThroughALink(t *testing.T) {
	t.Run("a file", func(t *testing.T) {
		x, ws := prepared(t, "hidden-parse", OverlayDecoy)
		victim := filepath.Join(ws, "victim.go")
		write(t, ws, "victim.go", "package ledgerkit\n")
		if err := os.Symlink(victim, filepath.Join(ws, "parse_hidden_test.go")); err != nil {
			t.Fatal(err)
		}
		before := snapshot(t, ws)
		g, err := Grade(context.Background(), x, x.Dir, ws, filepath.Join(tempDir(t), "grade"))
		if err != nil {
			t.Fatal(err)
		}
		if after := snapshot(t, ws); !slices.Equal(before, after) {
			t.Fatalf("Grade wrote the workspace through a link:\n%v\n%v", before, after)
		}
		if want := []string{"parse-reads"}; !slices.Equal(g.Pass, want) {
			t.Fatalf("passed %v, want %v: the hidden file did not replace the link", g.Pass, want)
		}
	})
	t.Run("a repository", func(t *testing.T) {
		x, ws := prepared(t, "handoff-split", OverlaySolution)
		elsewhere := filepath.Join(ws, "elsewhere")
		if err := os.Rename(filepath.Join(ws, "ledgerkit"), elsewhere); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(elsewhere, filepath.Join(ws, "ledgerkit")); err != nil {
			t.Fatal(err)
		}
		before := snapshot(t, ws)
		g, err := Grade(context.Background(), x, x.Dir, ws, filepath.Join(tempDir(t), "grade"))
		if err != nil {
			t.Fatal(err)
		}
		if after := snapshot(t, ws); !slices.Equal(before, after) {
			t.Fatalf("Grade wrote the workspace through a link:\n%v\n%v", before, after)
		}
		if len(g.Pass) != 0 {
			t.Fatalf("passed %v in a repository that is only a link", g.Pass)
		}
	})
}

func TestGradeRefusesOverlap(t *testing.T) {
	x, ws := prepared(t, "control-max", "")
	before := snapshot(t, ws)
	link := filepath.Join(tempDir(t), "link")
	if err := os.Symlink(ws, link); err != nil {
		t.Fatal(err)
	}
	for _, scratch := range []string{ws, filepath.Join(ws, "grade"), filepath.Dir(ws), link, filepath.Join(link, "grade")} {
		if _, err := Grade(context.Background(), x, x.Dir, ws, scratch); err == nil || !strings.Contains(err.Error(), "overlap") {
			t.Fatalf("Grade in %s = %v, want an error saying the folders overlap", scratch, err)
		}
	}
	if after := snapshot(t, ws); !slices.Equal(before, after) {
		t.Fatal("a refused grading changed the workspace")
	}
}

// telemetryDir is Go's telemetry folder in the home folder home, as
// os.UserConfigDir names it on this system with no XDG_CONFIG_HOME.
func telemetryDir(home string) string {
	if runtime.GOOS == "darwin" {
		return filepath.Join(home, "Library", "Application Support", "go", "telemetry")
	}
	return filepath.Join(home, ".config", "go", "telemetry")
}

// GoTelemetryOff writes off to the mode file Go reads in a home, making
// its folders.
func TestGoTelemetryOff(t *testing.T) {
	home := tempDir(t)
	if err := GoTelemetryOff(home); err != nil {
		t.Fatal(err)
	}
	mode, err := os.ReadFile(filepath.Join(telemetryDir(home), "mode"))
	if err != nil || strings.TrimSpace(string(mode)) != "off" {
		t.Fatalf("the mode file: %q %v", mode, err)
	}
}

// A test runs with Go's telemetry off in its home: a stub that fails
// unless the mode file there says off passes.
func TestGradeTurnsGoTelemetryOff(t *testing.T) {
	x, ws := stubTask(t, stubTelemetry)
	g, err := Grade(context.Background(), x, tempDir(t), ws, filepath.Join(tempDir(t), "grade"))
	if err != nil || !reflect.DeepEqual(g, Grading{Pass: []string{stubTelemetry}}) {
		t.Fatalf("Grade = %+v, %v", g, err)
	}
}

// After a grading whose tests run go, the telemetry folder of the
// grading's home holds the mode file alone: go kept no counters there,
// which is what leaves a process writing into it.
func TestGradeLeavesNoTelemetry(t *testing.T) {
	x, ws := prepared(t, "control-max", OverlaySolution)
	scratch := filepath.Join(tempDir(t), "grade")
	g, err := Grade(context.Background(), x, x.Dir, ws, scratch)
	if err != nil || !g.Solved() {
		t.Fatalf("Grade = %+v, %v", g, err)
	}
	entries, err := os.ReadDir(telemetryDir(filepath.Join(scratch, "home")))
	if err != nil || len(entries) != 1 || entries[0].Name() != "mode" {
		t.Fatalf("the telemetry folder holds %v: %v", entries, err)
	}
}
