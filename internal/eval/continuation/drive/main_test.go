package drive

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
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
	VersionTimeout = 10 * time.Minute
	WaitDelay = 10 * time.Minute
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
	// ended says how each chat call of the rig's runs ended, as the driver
	// saw it, for diagnose.
	ended []string
}

// traced makes the rig's next run keep a trace, in a folder of its own
// named for name, which diagnose shows.
func (r *rig) traced(name string) {
	r.opts.Trace = filepath.Join(r.base, "trace-"+name)
}

// diagnose says what the rig's runs did, for a test that fails: each of
// the fake's calls, by its process, with how it ended by its own record
// (or that it has none: something ended it from outside, or its runtime
// did, or it killed itself as its script asked); how each chat call ended
// as the driver saw it and what it was read as; and, when the last run
// kept a trace, what each call printed, its first 2 KB. Only made-up data
// passes through a rig, so nothing a real run holds is shown.
func (r *rig) diagnose() string {
	r.t.Helper()
	var b strings.Builder
	calls, err := fakehost.Read(r.script.Dir)
	ends, eerr := fakehost.ReadEnds(r.script.Dir)
	fmt.Fprintf(&b, "the fake's calls (%v, ends %v):\n", err, eerr)
	used := make([]bool, len(ends))
	for i, c := range calls {
		end := "no end recorded"
		for j, e := range ends {
			if !used[j] && e.Pid == c.Pid {
				used[j] = true
				end = fmt.Sprintf("exit %d", e.Exit)
				if e.Fault != "" {
					end += ", " + e.Fault
				}
				break
			}
		}
		fmt.Fprintf(&b, "  %d: pid %d, %s %s, session %q: %s\n", i+1, c.Pid, c.Host, c.Mode, c.Session, end)
	}
	b.WriteString("the chat calls as the driver saw them:\n")
	for i, e := range r.ended {
		fmt.Fprintf(&b, "  %d: %s\n", i+1, e)
	}
	if r.opts.Trace == "" {
		b.WriteString("no trace kept\n")
		return b.String()
	}
	b.WriteString("the trace:\n")
	err = filepath.WalkDir(r.opts.Trace, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(r.opts.Trace, p)
		fmt.Fprintf(&b, "  %s (%d bytes): %q\n", filepath.ToSlash(rel), len(data), data[:min(len(data), 2<<10)])
		return nil
	})
	if err != nil {
		fmt.Fprintf(&b, "  reading it: %v\n", err)
	}
	return b.String()
}

// endedAs is how a chat call ended, by the error its wait gave, and what
// it was read as.
func endedAs(err error, rep report) string {
	how := "exit status 0"
	if err != nil {
		how = err.Error()
	}
	return fmt.Sprintf("%s; read as answered %v, fail %q, session %q, usage %v", how, rep.Answered, rep.Fail, rep.Session, rep.Usage)
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
	write(t, filepath.Join(home, "gitconfig"), "[user]\nname = Rig Fixture\nemail = rig@example.invalid\n[commit]\ngpgsign = false\n[init]\ndefaultBranch = main\n"+
		// No background work of git's in the rig's repositories: a commit
		// otherwise starts maintenance that makes and removes files in .git
		// while a test walks the rig (CI's macOS job failed on that once).
		"[maintenance]\nauto = false\n[gc]\nauto = 0\n")
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
		ended: func(err error, rep report) { r.ended = append(r.ended, endedAs(err, rep)) },
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

// fresh gives the rig's next run a results folder and a work folder of
// their own, named for name, as a second attempt at the same trials takes.
func (r *rig) fresh(name string) {
	r.opts.Out = filepath.Join(r.base, "out-"+name)
	r.opts.Work = filepath.Join(r.base, "work-"+name)
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
