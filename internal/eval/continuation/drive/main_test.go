package drive

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
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
	t      testing.TB
	base   string // the test's folder, links resolved
	opts   Options
	script fakehost.Script
	file   string // the script's file
	clock  time.Time
	tmp    string // the temporary root the rig gives the driver
	// ended are the calls of the CLI the rig's runs made, in order, as the
	// driver saw each end, for diagnose, which may read them while a run a
	// test's hang guard gave up on still adds to them.
	mu    sync.Mutex
	ended []callEnd
}

// traced makes the rig's next run keep a trace, in a folder of its own
// named for name, which diagnose shows.
func (r *rig) traced(name string) {
	r.opts.Trace = filepath.Join(r.base, "trace-"+name)
}

// diagnose says what the rig's runs did, for a test that fails: each of
// the fake's calls, by its process, with how it ended by its own record
// (or that it has none: something ended it from outside, or its runtime
// did, or it killed itself as its script asked), and each end the fake
// recorded of a process that recorded no call, a fault before its record;
// each call of the CLI the driver made, the version's and the web tools'
// list among them, by its process, with how its wait ended, what a chat
// call was read as, and whether the fake recorded that process at all,
// or that it made none; and, when the last run kept a trace, what each
// call printed, its first 2 KB. Only made-up data passes through a rig,
// so nothing a real run holds is shown.
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
				end = fakeEnd(e)
				break
			}
		}
		fmt.Fprintf(&b, "  %d: pid %d, %s %s, session %q: %s\n", i+1, c.Pid, c.Host, c.Mode, c.Session, end)
	}
	for j, e := range ends {
		if !used[j] {
			fmt.Fprintf(&b, "  pid %d, no call recorded: %s\n", e.Pid, fakeEnd(e))
		}
	}
	b.WriteString("the calls of the CLI as the driver saw them:\n")
	r.mu.Lock()
	ended := slices.Clone(r.ended)
	r.mu.Unlock()
	for i, e := range ended {
		fmt.Fprintf(&b, "  %d: %s; the fake's record: %s\n", i+1, endedAs(e), recordOf(e.Pid, calls, ends))
	}
	if len(ended) == 0 {
		// As when the probe found no CLI to run, and so gave no version.
		b.WriteString("  none\n")
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

// fakeEnd is an end the fake recorded: its exit and its fault.
func fakeEnd(e fakehost.End) string {
	end := fmt.Sprintf("exit %d", e.Exit)
	if e.Fault != "" {
		end += ", " + e.Fault
	}
	return end
}

// recordOf says what the fake recorded of the process pid: a call, an
// end, both or neither. A process with neither never reached its record:
// it died first, as one its runtime ended does, or failed to read its
// script, or was not the fake at all.
func recordOf(pid int, calls []fakehost.Call, ends []fakehost.End) string {
	if pid == 0 {
		return "none, the call did not start"
	}
	var parts []string
	for _, c := range calls {
		if c.Pid == pid {
			parts = append(parts, "a "+c.Mode+" call")
		}
	}
	for _, e := range ends {
		if e.Pid == pid {
			parts = append(parts, "an end, "+fakeEnd(e))
		}
	}
	if len(parts) == 0 {
		return "nothing, it never reached its record"
	}
	return strings.Join(parts, " and ")
}

// endedAs is how a call of the CLI ended as the driver saw it: which call,
// its process, how its wait ended by the error's kind, and what a chat
// call was read as.
func endedAs(e callEnd) string {
	s := fmt.Sprintf("%s, pid %d, %s", e.Call, e.Pid, waitKind(e.Err))
	if e.Over {
		s += ", its output past its limit"
	}
	if e.Call == traceChat {
		rep := e.Report
		s += fmt.Sprintf("; read as answered %v, fail %q, session %q, usage %v", rep.Answered, rep.Fail, rep.Session, rep.Usage)
	}
	return s
}

// waitKind is how a call's wait ended, by its error's kind, with no path:
// "exit status N" or "signal: NAME" for an exit or a signal, and "no exit:"
// then the error for any other, a program that could not be started or an
// output held past the wait.
func waitKind(err error) string {
	var exit *exec.ExitError
	switch {
	case err == nil:
		return "exit status 0"
	case errors.As(err, &exit):
		return exit.Error()
	}
	return "no exit: " + pathless(err).Error()
}

// useTempFolders makes roots the driver's temporary folders for the test.
func useTempFolders(t testing.TB, roots ...string) {
	t.Helper()
	was := TempFolders
	TempFolders = roots
	t.Cleanup(func() { TempFolders = was })
}

// newRig makes a rig for the test t, whose output, should it fail, ends
// with what diagnose says.
func newRig(t testing.TB, facts int) *rig {
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
		ended: func(e callEnd) {
			r.mu.Lock()
			defer r.mu.Unlock()
			r.ended = append(r.ended, e)
		},
	}
	// Registered after the test's folder, so it runs before that folder is
	// removed.
	t.Cleanup(func() {
		if t.Failed() {
			t.Log("what the rig's runs did:\n" + r.diagnose())
		}
	})
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

func mkdir(t testing.TB, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
}

func write(t testing.TB, path, body string) {
	t.Helper()
	mkdir(t, filepath.Dir(path))
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func git(t testing.TB, dir string, args ...string) string {
	t.Helper()
	out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v %s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}
