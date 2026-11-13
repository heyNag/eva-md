package drive

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/heyNag/eva-md/internal/eval/continuation/drive/fakehost"
	"github.com/heyNag/eva-md/internal/eval/continuation/task"
	"github.com/heyNag/eva-md/internal/eval/results"
	"github.com/heyNag/eva-md/internal/testgitenv"
)

func TestMain(m *testing.M) {
	fakehost.Main()
	testgitenv.Clear()
	// The production limits, raised once: the fake's --version and a
	// killed call's output are waited for long enough on a slow machine.
	versionTimeout = 10 * time.Minute
	waitDelay = 10 * time.Minute
	os.Exit(m.Run())
}

// rig is one test's made-up world: a home, a repository the rows' commit
// is read from, the fixture's tasks and repositories, a work folder, a
// results folder, and the fake host's script and record.
type rig struct {
	t      *testing.T
	base   string // the test's folder, links resolved
	opts   Options
	script fakehost.Script
	file   string // the script's file
	clock  time.Time
	tmp    string // the temporary root the rig gives the driver
}

// useTempFolders makes roots the driver's temporary folders for the test.
func useTempFolders(t *testing.T, roots ...string) {
	t.Helper()
	was := TempFolders
	TempFolders = roots
	t.Cleanup(func() { TempFolders = was })
}

func newRig(t *testing.T, facts int) *rig {
	t.Helper()
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	home := filepath.Join(base, "home")
	mkdir(t, home)
	t.Setenv("HOME", home)
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(home, "gitconfig"))
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	write(t, filepath.Join(home, "gitconfig"), "[user]\nname = Rig Fixture\nemail = rig@example.invalid\n[commit]\ngpgsign = false\n[init]\ndefaultBranch = main\n")
	repo := filepath.Join(base, "repo")
	write(t, filepath.Join(repo, "README.md"), "measured\n")
	git(t, repo, "init", "-q")
	git(t, repo, "add", ".")
	git(t, repo, "commit", "-q", "-m", "first")
	tasks, repos, err := fakehost.Fixture(filepath.Join(base, "data"), facts)
	if err != nil {
		t.Fatal(err)
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	if exe, err = filepath.EvalSymlinks(exe); err != nil {
		t.Fatal(err)
	}
	// The test's own folders may be under /tmp, as they are when TMPDIR is:
	// the temporary roots --work may not overlap are the rig's own.
	r := &rig{t: t, base: base, clock: time.Date(2026, 11, 13, 9, 0, 0, 0, time.UTC), tmp: filepath.Join(base, "made-up-tmp")}
	useTempFolders(t, r.tmp)
	record := filepath.Join(base, "record")
	mkdir(t, record)
	r.script = fakehost.Script{Dir: record, Task: filepath.Join(tasks, fakehost.FixtureID), Facts: []string{fakehost.FixtureColor},
		Events: 2, Prompt: 1000, Cached: 600, Output: 50}
	if facts > 1 {
		r.script.Facts = append(r.script.Facts, fakehost.FixtureLimit)
	}
	r.file = filepath.Join(base, "script.json")
	t.Setenv(fakehost.ScriptVar, r.file)
	r.opts = Options{
		Tasks: tasks, Repos: repos, Host: "claude-code", CLI: exe, Condition: task.Off, Trials: 1,
		Work: filepath.Join(base, "work"), Out: filepath.Join(base, "out"),
		MaxSessions: 100, MaxTokens: 1 << 40, SessionTimeout: time.Hour,
		PassEnv: []string{fakehost.ScriptVar}, Repo: repo, Spend: true,
		Clock: func() time.Time { r.clock = r.clock.Add(time.Second); return r.clock },
	}
	return r
}

// run writes the script and runs the driver.
func (r *rig) run() (Summary, string, error) {
	r.t.Helper()
	data, err := json.Marshal(r.script)
	if err != nil {
		r.t.Fatal(err)
	}
	write(r.t, r.file, string(data))
	var out bytes.Buffer
	s, err := Run(context.Background(), r.opts, &out)
	return s, out.String(), err
}

// calls reads the fake's record.
func (r *rig) calls() []fakehost.Call {
	r.t.Helper()
	calls, err := fakehost.Read(r.script.Dir)
	if err != nil {
		r.t.Fatal(err)
	}
	return calls
}

// rows reads the results folder; none is no row.
func (r *rig) rows() []results.Row {
	r.t.Helper()
	if _, err := os.Stat(filepath.Join(r.opts.Out, results.FileName)); os.IsNotExist(err) {
		return nil
	}
	rows, err := results.Load(r.opts.Out)
	if err != nil {
		r.t.Fatal(err)
	}
	return rows
}

func (r *rig) commit() string {
	return git(r.t, r.opts.Repo, "rev-parse", "HEAD")
}

func mkdir(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
}

func write(t *testing.T, path, body string) {
	t.Helper()
	mkdir(t, filepath.Dir(path))
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v %s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}
