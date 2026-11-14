package drive

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"math"
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

	"github.com/heyNag/eva-md/internal/eval/continuation/drive/fakehost"
	"github.com/heyNag/eva-md/internal/eval/continuation/task"
	"github.com/heyNag/eva-md/internal/eval/results"
	"github.com/heyNag/eva-md/internal/host/adapter"
)

// wantRows gives the rows a judged trial of the fixture must give, with
// the outcome o, measured on the rig's commit, untouched tree, host and
// version 1.2.3, and the model the fake names. Times are left out.
func (r *rig) wantRows(n int, o task.Outcome, model string) []results.Row {
	r.t.Helper()
	tk, err := task.Load(r.script.Task)
	if err != nil {
		r.t.Fatal(err)
	}
	rows, err := task.Rows(tk, r.opts.Condition, n, o, results.Row{Commit: r.commit(), Host: r.opts.Host, HostVersion: "1.2.3", Model: model})
	if err != nil {
		r.t.Fatal(err)
	}
	for i := range rows {
		rows[i].V = results.Version
	}
	return rows
}

// timeless gives rows without their time.
func timeless(rows []results.Row) []results.Row {
	rows = slices.Clone(rows)
	for i := range rows {
		rows[i].Time = time.Time{}
	}
	return rows
}

// trialDir is the folder of the trial n of the run.
func (r *rig) trialDir(run string, n int) string {
	return filepath.Join(r.opts.Work, run, fakehost.FixtureID, string(r.opts.Condition), "t"+strconv.Itoa(n))
}

// For each host, on the fake, a hidden-decision task under the ideal
// record is solved at once, and under memory off after one correction
// that supplies the hidden fact; the rows are exact but for their time.
// The fake's record shows each call's arguments, its environment of
// exactly the driver's four variables and the one passed, its folder, and
// no hidden file in the workspace at any prompt.
func TestHiddenTaskOnEachHost(t *testing.T) {
	for _, h := range adapter.All() {
		for _, c := range []task.Condition{task.Oracle, task.Off} {
			t.Run(string(h.ID)+"/"+string(c), func(t *testing.T) {
				r := newRig(t, 1)
				r.opts.Host, r.opts.Condition, r.opts.Model = string(h.ID), c, "made-up-model-7"
				// The trial's folder is read after the run.
				r.opts.KeepTrials = true
				s, out, err := r.run()
				if err != nil {
					t.Fatalf("run: %v\n%s", err, out)
				}
				want := task.Outcome{Solved: true, FirstTry: true, Rounds: 1, AgentEvents: 2, PromptTokens: 1000, CachedTokens: 600, OutputTokens: 50, WallMs: 1000}
				if c == task.Off {
					want = task.Outcome{Solved: true, Corrections: 1, Rounds: 2, AgentEvents: 4, PromptTokens: 2000, CachedTokens: 1200, OutputTokens: 100, WallMs: 2000}
				}
				if got := timeless(r.rows()); !reflect.DeepEqual(got, r.wantRows(1, want, "made-up-model-7")) {
					t.Fatalf("rows:\n%+v\nwant\n%+v", got, r.wantRows(1, want, "made-up-model-7"))
				}
				if s.Judged != 1 || s.Planned != 1 || s.Rows != 9 || s.NoModel != 0 || s.Tokens != want.PromptTokens+want.OutputTokens {
					t.Fatalf("summary %+v", s)
				}
				calls := r.calls()
				var chats []fakehost.Call
				for _, call := range calls {
					if call.Mode == "start" || call.Mode == "continue" {
						chats = append(chats, call)
					}
				}
				// Codex lists its features once a trial, before its web tools
				// are turned off; Claude Code needs no list.
				lists := map[adapter.ID]int{adapter.Codex: 1}[h.ID]
				if len(chats) != want.Rounds || len(calls) != want.Rounds+1+lists || calls[0].Mode != "version" || countMode(calls, "tools") != lists {
					t.Fatalf("calls: %+v", calls)
				}
				tk, _ := task.Load(r.script.Task)
				first, _ := task.FirstPrompt(tk, c)
				if chats[0].Mode != "start" || chats[0].Prompt != first || chats[0].Model != "made-up-model-7" {
					t.Fatalf("the first call: %+v", chats[0])
				}
				if c == task.Off {
					follow := "These hidden tests still fail: banner-colour.\n\nWhat they check:\n- " + tk.Facts[0].Statement + "\n\nPlease make them pass."
					if chats[1].Mode != "continue" || chats[1].Session != chats[0].Session || chats[1].Prompt != follow {
						t.Fatalf("the follow-up: %+v", chats[1])
					}
				}
				dir := r.trialDir(strings.TrimPrefix(strings.SplitN(out, "\n", 3)[1], "run "), 1)
				// Memory off and no plugin, by the facts the host table
				// names (F-062 and F-169).
				off := map[adapter.ID][]string{
					adapter.ClaudeCode: {"--settings", `{"autoMemoryEnabled":false}`},
					adapter.Codex:      {"--disable", "hooks", "--disable", "plugins"},
				}[h.ID]
				// And Claude Code's by a variable too (evaluate.md 4.5); Codex
				// has none (F-170).
				env := []string{fakehost.ScriptVar, "GIT_CONFIG_NOSYSTEM", "HOME", "PATH", "TMPDIR"}
				if h.ID == adapter.ClaudeCode {
					env = []string{fakehost.OffVar, fakehost.ScriptVar, "GIT_CONFIG_NOSYSTEM", "HOME", "PATH", "TMPDIR"}
				}
				for _, call := range calls {
					if !slices.Equal(call.Env, env) {
						t.Fatalf("the environment: %v, want %v", call.Env, env)
					}
					if value, set := call.Fixed[fakehost.OffVar]; set != (h.ID == adapter.ClaudeCode) || set && value != "1" {
						t.Fatalf("the variable that turns memory off: %q %v", value, set)
					}
					if call.Fixed["HOME"] != filepath.Join(dir, "home") || call.Fixed["TMPDIR"] != filepath.Join(dir, "tmp") ||
						call.Fixed["GIT_CONFIG_NOSYSTEM"] != "1" || call.Fixed["PATH"] != filepath.Join(dir, "bin")+string(filepath.ListSeparator)+os.Getenv("PATH") {
						t.Fatalf("the built variables: %v", call.Fixed)
					}
					if call.Mode == "version" || call.Mode == "tools" {
						continue
					}
					if call.Dir != filepath.Join(dir, "workspace") {
						t.Fatalf("the chat ran in %s", call.Dir)
					}
					if len(off) == 0 || !holdsRun(call.Args, off) {
						t.Fatalf("the call %v lacks the flags that turn memory and plugins off", call.Args)
					}
					checkWebOff(t, h.ID, call.Args)
					for _, f := range call.Files {
						if strings.HasPrefix(f, "checks/") || f == "answer.txt" && call.Mode == "start" {
							t.Fatalf("the workspace held %s at a prompt", f)
						}
					}
					// Nor can the chat find one in its trial's folder.
					for _, f := range call.Around {
						if strings.Contains(f, "checks/") {
							t.Fatalf("the trial's folder held %s at a prompt", f)
						}
					}
					// Its home has Go's telemetry off at each prompt.
					if !slices.Contains(call.Around, "home/"+telemetryMode) {
						t.Fatalf("the trial's home lacked Go's telemetry mode at a prompt: %v", call.Around)
					}
				}
				if want := slices.Sorted(slices.Values([]string{".gitconfig", telemetryMode})); !slices.Equal(files(t, filepath.Join(dir, "home")), want) {
					t.Fatalf("the home holds %v", list(t, filepath.Join(dir, "home")))
				}
				if mode, err := os.ReadFile(filepath.Join(dir, "home", filepath.FromSlash(telemetryMode))); err != nil || strings.TrimSpace(string(mode)) != "off" {
					t.Fatalf("Go's telemetry mode: %q %v", mode, err)
				}
				gc, _ := os.ReadFile(filepath.Join(dir, "home", ".gitconfig"))
				if string(gc) != gitconfig || !strings.Contains(gitconfig, "gpgsign = false") || !strings.Contains(gitconfig, "auto = false") {
					t.Fatalf("the home's git configuration: %q", gc)
				}
			})
		}
	}
}

// holdsRun reports whether args hold want as a run of adjacent arguments.
func holdsRun(args, want []string) bool {
	for i := 0; i+len(want) <= len(args); i++ {
		if slices.Equal(args[i:i+len(want)], want) {
			return true
		}
	}
	return false
}

// telemetryMode is the slash path, in a home, of the mode file of Go's
// telemetry, as os.UserConfigDir names it on this system.
var telemetryMode = map[bool]string{false: ".config/go/telemetry/mode", true: "Library/Application Support/go/telemetry/mode"}[runtime.GOOS == "darwin"]

// files lists the files under dir, as slash paths, folders left out.
func files(t *testing.T, dir string) []string {
	t.Helper()
	var names []string
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, _ := filepath.Rel(dir, p)
		names = append(names, filepath.ToSlash(rel))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return names
}

// list lists the names under dir, as slash paths.
func list(t *testing.T, dir string) []string {
	t.Helper()
	var names []string
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || p == dir {
			return err
		}
		rel, _ := filepath.Rel(dir, p)
		names = append(names, filepath.ToSlash(rel))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return names
}

// A round that supplies two facts counts two corrections, not one.
func TestOneRoundSuppliesTwoFacts(t *testing.T) {
	r := newRig(t, 2)
	s, out, err := r.run()
	if err != nil {
		t.Fatalf("run: %v\n%s", err, out)
	}
	want := task.Outcome{Solved: true, Corrections: 2, Rounds: 2, AgentEvents: 4, PromptTokens: 2000, CachedTokens: 1200, OutputTokens: 100, WallMs: 2000}
	if got := timeless(r.rows()); !reflect.DeepEqual(got, r.wantRows(1, want, "fake-model-1")) || s.Judged != 1 {
		t.Fatalf("rows:\n%+v\nwant\n%+v", got, r.wantRows(1, want, "fake-model-1"))
	}
	calls := r.calls()
	tk, _ := task.Load(r.script.Task)
	if p := calls[len(calls)-1].Prompt; !strings.Contains(p, "banner-colour, banner-close.") || !strings.Contains(p, tk.Facts[0].Statement) || !strings.Contains(p, tk.Facts[1].Statement) {
		t.Fatalf("the follow-up: %q", p)
	}
}

// A chat that never gets the fact fails after the task's rounds, with one
// correction: a fact is supplied once, and a round that supplies none is a
// round only.
func TestUnsolvedAfterEveryRound(t *testing.T) {
	r := newRig(t, 1)
	r.script.Facts = []string{"a word no prompt holds"}
	s, out, err := r.run()
	if err != nil {
		t.Fatalf("run: %v\n%s", err, out)
	}
	want := task.Outcome{Corrections: 1, Rounds: 3, AgentEvents: 6, PromptTokens: 3000, CachedTokens: 1800, OutputTokens: 150, WallMs: 3000}
	if got := timeless(r.rows()); !reflect.DeepEqual(got, r.wantRows(1, want, "fake-model-1")) || s.Judged != 1 {
		t.Fatalf("rows:\n%+v\nwant\n%+v", got, r.wantRows(1, want, "fake-model-1"))
	}
	calls := r.calls()
	if p := calls[len(calls)-1].Prompt; p != "These hidden tests still fail: banner-colour.\n\nPlease make them pass." {
		t.Fatalf("the last round: %q", p)
	}
}

// A variable passed by --pass-env reaches the chat, and its value is in no
// row, no output, no file the driver wrote and not in the fake's record.
func TestAPassedValueGoesNowhereElse(t *testing.T) {
	r := newRig(t, 1)
	const secret = "made-up-key-QX7Z31-not-real"
	t.Setenv("EVAL_MADE_UP_KEY", secret)
	r.opts.PassEnv = append(r.opts.PassEnv, "EVAL_MADE_UP_KEY")
	_, out, err := r.run()
	if err != nil {
		t.Fatalf("run: %v\n%s", err, out)
	}
	calls := r.calls()
	if !slices.Contains(calls[1].Env, "EVAL_MADE_UP_KEY") {
		t.Fatalf("the variable did not reach the chat: %v", calls[1].Env)
	}
	if strings.Contains(out, secret) {
		t.Fatal("the output holds the passed value")
	}
	for _, root := range []string{r.opts.Work, r.opts.Out, r.script.Dir} {
		err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || d.Type()&fs.ModeSymlink != 0 {
				return err
			}
			data, err := os.ReadFile(p)
			if err == nil && strings.Contains(string(data), secret) {
				t.Errorf("%s holds the passed value", p)
			}
			return err
		})
		if err != nil {
			t.Fatal(err)
		}
	}
}

// defaultTempFolders is TempFolders as the package sets it, before any
// test changes it.
var defaultTempFolders = slices.Clone(TempFolders)

// The temporary folders --work may not overlap are, but in tests, those
// eva-md's hooks skip.
func TestTempFoldersAreTheHooks(t *testing.T) {
	if want := []string{"/tmp", "/var/tmp"}; !slices.Equal(defaultTempFolders, want) {
		t.Fatalf("%v, want %v", defaultTempFolders, want)
	}
}

// Each --work the card refuses is refused before anything starts, for its
// own reason: nothing runs and the folder is not made. /tmp, /var/tmp and
// a link into /tmp are refused by the folders the command uses, whatever
// TMPDIR is, and nothing is written under /tmp; a root the test makes, a
// link into it, and a folder holding it, by a root of the test's own.
func TestWorkFoldersRefused(t *testing.T) {
	r := newRig(t, 1)
	home := os.Getenv("HOME")
	tmpName := "evamd-drive-test-" + strconv.Itoa(os.Getpid())
	toTmp := filepath.Join(r.base, "to-tmp")
	if err := os.Symlink("/tmp", toTmp); err != nil {
		t.Fatal(err)
	}
	toHome := filepath.Join(r.base, "to-home")
	if err := os.Symlink(home, toHome); err != nil {
		t.Fatal(err)
	}
	mkdir(t, r.tmp)
	toMade := filepath.Join(r.base, "to-made-up-tmp")
	if err := os.Symlink(r.tmp, toMade); err != nil {
		t.Fatal(err)
	}
	holder := filepath.Join(r.base, "holder")
	const temp = "overlaps a temporary folder"
	mine := []string{r.tmp}
	for name, c := range map[string]struct {
		work   string
		roots  []string
		reason string
	}{
		"relative":           {"work", mine, "not an absolute path"},
		"the home":           {home, mine, "overlaps the invoking home"},
		"in the home":        {filepath.Join(home, "work"), mine, "overlaps the invoking home"},
		"a link to home":     {filepath.Join(toHome, "work"), mine, "overlaps the invoking home"},
		"holding the home":   {r.base, mine, "overlaps the invoking home"},
		"in /tmp":            {filepath.Join("/tmp", tmpName), defaultTempFolders, temp},
		"/var/tmp":           {filepath.Join("/var/tmp", tmpName), defaultTempFolders, temp},
		"a link into /tmp":   {filepath.Join(toTmp, tmpName), defaultTempFolders, temp},
		"in a made root":     {filepath.Join(r.tmp, "work"), mine, temp},
		"a link into it":     {filepath.Join(toMade, "work"), mine, temp},
		"holding a made one": {holder, []string{filepath.Join(holder, "made-up-tmp")}, temp},
		"in the tasks":       {filepath.Join(r.opts.Tasks, "work"), mine, "overlaps --tasks"},
		"in the repos":       {filepath.Join(r.opts.Repos, "work"), mine, "overlaps --repos"},
		"in the repository":  {filepath.Join(r.opts.Repo, "work"), mine, "overlaps --repo"},
		"holding the tasks":  {filepath.Dir(r.opts.Tasks), mine, "overlaps --tasks"},
	} {
		t.Run(name, func(t *testing.T) {
			useTempFolders(t, c.roots...)
			r.opts.Work = c.work
			_, out, err := r.run()
			var usage *UsageError
			if !errors.As(err, &usage) || out != "" || !strings.Contains(err.Error(), c.reason) {
				t.Fatalf("run: %v, output %q", err, out)
			}
			if calls := r.calls(); len(calls) != 0 {
				t.Fatalf("the fake ran: %+v", calls)
			}
			for _, p := range []string{filepath.Join("/tmp", tmpName), filepath.Join("/var/tmp", tmpName), filepath.Join(home, "work"), filepath.Join(r.tmp, "work"), holder, r.opts.Out} {
				if _, err := os.Lstat(p); !errors.Is(err, fs.ErrNotExist) {
					t.Fatalf("%s was made", p)
				}
			}
		})
	}
}

// A run writes nothing to the invoking temporary folder: every child gets
// the trial's.
func TestNothingInTheInvokingTemporaryFolder(t *testing.T) {
	r := newRig(t, 1)
	tmp := filepath.Join(r.base, "invoking-tmp")
	mkdir(t, tmp)
	t.Setenv("TMPDIR", tmp)
	if _, out, err := r.run(); err != nil {
		t.Fatalf("run: %v\n%s", err, out)
	}
	if got := list(t, tmp); len(got) != 0 {
		t.Fatalf("the invoking temporary folder holds %v", got)
	}
}

// A stalled session, which blocks until it is killed, is stopped at the
// run's --session-timeout: the trial is unjudged, timeout, and gives no
// row. The fake blocks whatever the limit, so no time decides the outcome;
// the guard only bounds a hang.
func TestStalledSessionTimesOut(t *testing.T) {
	r := newRig(t, 1)
	r.script.Stall = true
	r.opts.SessionTimeout = 200 * time.Millisecond
	type result struct {
		s   Summary
		out string
		err error
	}
	done := make(chan result, 1)
	go func() {
		s, out, err := r.run()
		done <- result{s, out, err}
	}()
	var res result
	select {
	case res = <-done:
	case <-time.After(5 * time.Minute):
		t.Fatal("the stalled session was not stopped")
	}
	if res.err != nil || res.s.Unjudged[CodeTimeout] != 1 || res.s.Judged != 0 || !strings.Contains(res.out, "unjudged, timeout") {
		t.Fatalf("run: %+v %v\n%s", res.s, res.err, res.out)
	}
	if rows := r.rows(); rows != nil {
		t.Fatalf("an unjudged trial wrote rows: %+v", rows)
	}
	// The limit may end the session before the fake has recorded its start.
	if calls := r.calls(); countMode(calls, "start") > 1 || countMode(calls, "continue") != 0 {
		t.Fatalf("calls: %+v", calls)
	}
}

// Failures that say nothing of the answer leave the trial unjudged with
// their code and write no row.
func TestHostFailuresLeaveTheTrialUnjudged(t *testing.T) {
	for _, h := range adapter.All() {
		for code, set := range map[string]func(r *rig){
			CodeSignIn:      func(r *rig) { r.script.SignIn = true },
			CodeRateLimited: func(r *rig) { r.script.RateLimited = true },
			CodeOtherModel:  func(r *rig) { r.opts.Model = "made-up-model-7"; r.script.Model = "made-up-model-8" },
		} {
			t.Run(string(h.ID)+"/"+code, func(t *testing.T) {
				r := newRig(t, 1)
				r.opts.Host = string(h.ID)
				set(r)
				s, out, err := r.run()
				if err != nil || s.Unjudged[code] != 1 || s.Judged != 0 || len(s.Unjudged) != 1 {
					t.Fatalf("run: %+v %v\n%s", s, err, out)
				}
				if !strings.Contains(out, "unjudged, "+code) || !strings.Contains(out, code+" 1") {
					t.Fatalf("output: %s", out)
				}
				if rows := r.rows(); rows != nil {
					t.Fatalf("rows: %+v", rows)
				}
			})
		}
	}
}

// The plan prints first; a plan over --max-sessions is refused and nothing
// starts, while one at it runs.
func TestMaxSessions(t *testing.T) {
	r := newRig(t, 1)
	r.opts.Trials, r.opts.MaxSessions = 3, 2
	_, out, err := r.run()
	var usage *UsageError
	if !errors.As(err, &usage) || !strings.HasPrefix(out, "plan: condition off, host claude-code, tasks 1, trials 3 each, sessions 3; ceilings: sessions 2,") {
		t.Fatalf("run: %v\n%s", err, out)
	}
	if calls := r.calls(); len(calls) != 0 {
		t.Fatalf("the fake ran: %+v", calls)
	}
	if _, err := os.Lstat(r.opts.Work); !errors.Is(err, fs.ErrNotExist) {
		t.Fatal("the work folder was made")
	}
	r.opts.MaxSessions = 3
	s, out, err := r.run()
	if err != nil || s.Judged != 3 {
		t.Fatalf("run: %+v %v\n%s", s, err, out)
	}
}

// No trial starts once the tokens spent reach --max-tokens: the rest are
// counted not run. One token fewer spent lets the next trial start.
func TestMaxTokens(t *testing.T) {
	r := newRig(t, 1)
	r.opts.Condition, r.opts.Trials = task.Oracle, 3
	perTrial := r.script.Prompt + r.script.Output
	r.opts.MaxTokens = perTrial
	s, out, err := r.run()
	if err != nil || s.Judged != 1 || s.NotRun != 2 || s.Tokens != perTrial || !strings.Contains(out, "trial hidden-banner.t2 and the 1 after it: not run, --max-tokens reached\n") {
		t.Fatalf("run: %+v %v\n%s", s, err, out)
	}
	if starts := countMode(r.calls(), "start"); starts != 1 {
		t.Fatalf("%d sessions started", starts)
	}
	r.opts.MaxTokens = perTrial + 1
	r.fresh("two")
	if s, out, err = r.run(); err != nil || s.Judged != 2 || s.NotRun != 1 || !strings.Contains(out, "trial hidden-banner.t3 and the 0 after it: not run") {
		t.Fatalf("run: %+v %v\n%s", s, err, out)
	}
}

// The trials not run are counted in one line, however many there are, and
// a plan too large to count is refused: neither runs on past a ceiling.
func TestCeilingsOnAHugePlan(t *testing.T) {
	r := newRig(t, 1)
	r.opts.Condition = task.Oracle
	r.opts.Trials, r.opts.MaxSessions = 1<<40, 1<<41
	r.opts.MaxTokens = r.script.Prompt + r.script.Output
	s, out, err := r.run()
	if err != nil || s.Judged != 1 || s.NotRun != 1<<40-1 || strings.Count(out, "\n") != 5 {
		t.Fatalf("run: %+v %v\n%s", s, err, out)
	}
	if starts := countMode(r.calls(), "start"); starts != 1 {
		t.Fatalf("%d sessions started", starts)
	}
	// Two tasks of math.MaxInt trials each: the count would wrap.
	second(t, r)
	r.fresh("two")
	r.opts.Trials, r.opts.MaxSessions = math.MaxInt, 1
	s, out, err = r.run()
	var usage *UsageError
	if !errors.As(err, &usage) || !strings.Contains(out, "tasks 2, trials "+strconv.Itoa(math.MaxInt)+" each, sessions over ") {
		t.Fatalf("run: %+v %v\n%s", s, err, out)
	}
	if starts := countMode(r.calls(), "start"); starts != 1 {
		t.Fatalf("%d sessions started", starts)
	}
}

// second adds a second task to the rig's tasks, the fixture's under
// another id.
func second(t *testing.T, r *rig) {
	t.Helper()
	tk, err := task.Load(r.script.Task)
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(r.opts.Tasks, "hidden-banner-two")
	if err := os.CopyFS(dir, os.DirFS(r.script.Task)); err != nil {
		t.Fatal(err)
	}
	tk.ID = "hidden-banner-two"
	data, err := json.MarshalIndent(tk, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(dir, task.FileName), string(data)+"\n")
	if all, err := task.LoadAll(r.opts.Tasks); err != nil || len(all) != 2 {
		t.Fatalf("%d tasks: %v", len(all), err)
	}
}

func countMode(calls []fakehost.Call, mode string) int {
	n := 0
	for _, c := range calls {
		if c.Mode == mode {
			n++
		}
	}
	return n
}

// Without --spend-model-quota the plan prints and nothing starts: the fake
// records nothing, and no folder is made.
func TestNothingStartsWithoutLeave(t *testing.T) {
	r := newRig(t, 1)
	r.opts.Spend = false
	_, out, err := r.run()
	if !errors.Is(err, ErrNoQuota) || !strings.HasPrefix(out, "plan: ") || strings.Count(out, "\n") != 1 {
		t.Fatalf("run: %v\n%s", err, out)
	}
	if calls := r.calls(); len(calls) != 0 {
		t.Fatalf("the fake ran: %+v", calls)
	}
	for _, p := range []string{r.opts.Work, r.opts.Out} {
		if _, err := os.Lstat(p); !errors.Is(err, fs.ErrNotExist) {
			t.Fatalf("%s was made", p)
		}
	}
}

// useArm registers an Arm under the eva condition for the test.
func useArm(t *testing.T, a Arm) {
	t.Helper()
	Register(task.Eva, a)
	t.Cleanup(func() { delete(arms, task.Eva) })
}

// An unregistered condition is a usage error.
func TestUnregisteredCondition(t *testing.T) {
	r := newRig(t, 1)
	r.opts.Condition = task.Eva
	_, _, err := r.run()
	var usage *UsageError
	if !errors.As(err, &usage) || !strings.Contains(err.Error(), "not registered") {
		t.Fatalf("run: %v", err)
	}
}

// An Arm whose Check fails starts no trial.
func TestArmCheckFails(t *testing.T) {
	r := newRig(t, 1)
	var seen Options
	useArm(t, Arm{Check: func(o Options) error { seen = o; return errors.New("made-up refusal") }})
	r.opts.Condition = task.Eva
	_, _, err := r.run()
	if err == nil || !strings.Contains(err.Error(), "made-up refusal") || seen.Work != r.opts.Work {
		t.Fatalf("run: %v", err)
	}
	if calls := r.calls(); len(calls) != 0 {
		t.Fatalf("the fake ran: %+v", calls)
	}
	if _, err := os.Lstat(r.opts.Work); !errors.Is(err, fs.ErrNotExist) {
		t.Fatal("the work folder was made")
	}
}

// An Arm's Prepare sees the trial, and what it launches reaches the chat:
// its flags and variables. One whose Prepare or After returns *Unjudged
// writes no row; what After spends is added to the outcome and to the
// tokens the ceiling counts.
func TestArmPrepareAndAfter(t *testing.T) {
	t.Run("launch", func(t *testing.T) {
		r := newRig(t, 1)
		r.opts.Condition = task.Eva
		r.opts.EvaMD = filepath.Join(r.base, "made-up-eva-md")
		r.opts.KeepTrials = true // its bin is read after the run
		write(t, r.opts.EvaMD, "#!/bin/sh\nexit 0\n")
		if err := os.Chmod(r.opts.EvaMD, 0o755); err != nil {
			t.Fatal(err)
		}
		var trial Trial
		useArm(t, Arm{
			Prepare: func(_ context.Context, tr Trial) (Launch, error) {
				trial = tr
				return Launch{Args: []string{"--made-up-flag"}, Env: []string{"EVAL_MADE_UP_ACTOR=person"}}, nil
			},
			After: func(_ context.Context, tr Trial) (Cost, error) { return Cost{Tokens: 70, Ms: 900}, nil },
		})
		s, out, err := r.run()
		if err != nil || s.Judged != 1 || s.Tokens != 2*1050+70 {
			t.Fatalf("run: %+v %v\n%s", s, err, out)
		}
		if trial.Number != 1 || trial.Condition != task.Eva || trial.Task.ID != fakehost.FixtureID || trial.Host.ID != adapter.ClaudeCode ||
			trial.CLI != filepath.Join(trial.Dirs.Bin, "claude") || !slices.Contains(trial.Env, "TMPDIR="+trial.Dirs.Tmp) {
			t.Fatalf("the trial: %+v", trial)
		}
		for _, d := range []string{trial.Dirs.Home, trial.Dirs.Tmp, trial.Dirs.Bin, trial.Dirs.Workspace, trial.Dirs.Grade} {
			if filepath.Dir(d) != trial.Dirs.Root {
				t.Fatalf("%s is not in the trial's folder", d)
			}
		}
		if to, err := os.Readlink(filepath.Join(trial.Dirs.Bin, "eva-md")); err != nil || to != r.opts.EvaMD {
			t.Fatalf("--eva-md is not linked into the trial's bin: %q %v", to, err)
		}
		calls := r.calls()
		// A condition's own flags come before those that turn the web tools
		// off, which every chat still has.
		if chats := checkChatsWebOff(t, adapter.ClaudeCode, calls); chats != 2 {
			t.Fatalf("%d chats, want 2", chats)
		}
		if !slices.Contains(calls[1].Args, "--made-up-flag") || !slices.Contains(calls[1].Env, "EVAL_MADE_UP_ACTOR") || slices.Contains(calls[0].Env, "EVAL_MADE_UP_ACTOR") {
			t.Fatalf("calls: %+v", calls)
		}
		want := task.Outcome{Solved: true, Corrections: 1, Rounds: 2, AgentEvents: 4, PromptTokens: 2000, CachedTokens: 1200, OutputTokens: 100, WallMs: 2000,
			Memory: &task.Spend{Tokens: 70, Ms: 900}}
		rows := timeless(r.rows())
		if !reflect.DeepEqual(rows, r.wantRows(1, want, "fake-model-1")) || len(rows) != 11 || rows[9].Metric != "continuation.hidden.eva.memory_tokens" {
			t.Fatalf("rows:\n%+v\nwant\n%+v", rows, r.wantRows(1, want, "fake-model-1"))
		}
	})
	for name, arm := range map[string]Arm{
		"prepare": {Prepare: func(context.Context, Trial) (Launch, error) { return Launch{}, &Unjudged{Code: CodeSetupFailed} }},
		"after":   {After: func(context.Context, Trial) (Cost, error) { return Cost{Tokens: 5}, &Unjudged{Code: CodeUpdateFailed} }},
	} {
		t.Run(name, func(t *testing.T) {
			r := newRig(t, 1)
			r.opts.Condition = task.Eva
			useArm(t, arm)
			s, out, err := r.run()
			if err != nil || s.Judged != 0 || len(s.Unjudged) != 1 {
				t.Fatalf("run: %+v %v\n%s", s, err, out)
			}
			if rows := r.rows(); rows != nil {
				t.Fatalf("rows: %+v", rows)
			}
			if name == "prepare" && countMode(r.calls(), "start") != 0 {
				t.Fatal("a chat started after Prepare left the trial unjudged")
			}
		})
	}
	t.Run("a code off the list", func(t *testing.T) {
		r := newRig(t, 1)
		r.opts.Condition = task.Eva
		useArm(t, Arm{Prepare: func(context.Context, Trial) (Launch, error) { return Launch{}, &Unjudged{Code: "made_up"} }})
		if _, _, err := r.run(); err == nil {
			t.Fatal("an unjudged code off the list was taken")
		}
		if rows := r.rows(); rows != nil {
			t.Fatalf("rows: %+v", rows)
		}
	})
	t.Run("a variable the chat has", func(t *testing.T) {
		r := newRig(t, 1)
		r.opts.Condition = task.Eva
		useArm(t, Arm{Prepare: func(context.Context, Trial) (Launch, error) { return Launch{Env: []string{"HOME=/elsewhere"}}, nil }})
		if _, _, err := r.run(); err == nil || countMode(r.calls(), "start") != 0 {
			t.Fatalf("a launch that replaces HOME ran: %v", err)
		}
	})
}

// A launch with the plugin loads it as the host table says, from the
// trial's home, and is unjudged when setup left no plugin there.
func TestPluginLaunch(t *testing.T) {
	for _, h := range adapter.All() {
		t.Run(string(h.ID), func(t *testing.T) {
			r := newRig(t, 1)
			r.opts.Host, r.opts.Condition = string(h.ID), task.Eva
			// The trial's home is read after the run.
			r.opts.KeepTrials = true
			install := false
			useArm(t, Arm{Prepare: func(_ context.Context, tr Trial) (Launch, error) {
				if install {
					write(t, filepath.Join(pluginDir(tr.Dirs.Home, h), "hooks", "hooks.json"),
						`{"hooks":{"Stop":[{"hooks":[{"type":"command","command":"'eva-md' hook --host x","timeout":5}]}]}}`)
				}
				return Launch{Plugin: true}, nil
			}})
			s, out, err := r.run()
			if err != nil || s.Unjudged[CodeSetupFailed] != 1 || countMode(r.calls(), "start") != 0 {
				t.Fatalf("no plugin: %+v %v\n%s", s, err, out)
			}
			install = true
			r.fresh("two")
			if s, out, err = r.run(); err != nil || s.Judged != 1 {
				t.Fatalf("run: %+v %v\n%s", s, err, out)
			}
			calls := r.calls()
			// Each chat that loads the plugin has the host's web tools off.
			if chats := checkChatsWebOff(t, h.ID, calls); chats < 1 {
				t.Fatalf("no chat ran: %+v", calls)
			}
			args := calls[len(calls)-1].Args
			home := calls[len(calls)-1].Fixed["HOME"]
			// The plugin's chat has the host's own memory off by its variable
			// too.
			if value, set := calls[len(calls)-1].Fixed[fakehost.OffVar]; set != (h.ID == adapter.ClaudeCode) || set && value != "1" {
				t.Fatalf("the variable that turns memory off: %q %v", value, set)
			}
			switch h.ID {
			case adapter.ClaudeCode:
				if !holdsRun(args, []string{"--plugin-dir", pluginDir(home, h)}) || !holdsRun(args, []string{"--settings", `{"autoMemoryEnabled":false}`}) {
					t.Fatalf("args: %v", args)
				}
			case adapter.Codex:
				if holdsRun(args, []string{"--disable", "hooks"}) || !holdsRun(args, []string{"--disable", "plugins"}) {
					t.Fatalf("args: %v", args)
				}
				data, err := os.ReadFile(filepath.Join(home, ".codex", "requirements.toml"))
				if err != nil || string(data) != "[[hooks.Stop]]\n[[hooks.Stop.hooks]]\ncommand = \"'eva-md' hook --host x\"\ntimeout = 5\ntype = \"command\"\n" {
					t.Fatalf("the managed hooks: %q %v", data, err)
				}
			}
		})
	}
}

// Delivered nil gives no asked_again row; a stub gives it exactly, asked
// once for each fact a round supplies, before that round.
func TestAskedAgain(t *testing.T) {
	r := newRig(t, 2)
	type ask struct {
		n    int
		fact string
	}
	var asks []ask
	r.opts.Delivered = func(tr Trial, fact string) (bool, error) {
		if countMode(r.calls(), "continue") != 0 {
			t.Error("Delivered was asked after the round that supplied the fact")
		}
		asks = append(asks, ask{tr.Number, fact})
		return fact == "f-limit", nil
	}
	if _, out, err := r.run(); err != nil {
		t.Fatalf("run: %v\n%s", err, out)
	}
	if want := []ask{{1, "f-color"}, {1, "f-limit"}}; !slices.Equal(asks, want) {
		t.Fatalf("asks %v, want %v", asks, want)
	}
	one := 1
	want := task.Outcome{Solved: true, Corrections: 2, Rounds: 2, AgentEvents: 4, PromptTokens: 2000, CachedTokens: 1200, OutputTokens: 100, WallMs: 2000, AskedAgain: &one}
	if got := timeless(r.rows()); !reflect.DeepEqual(got, r.wantRows(1, want, "fake-model-1")) {
		t.Fatalf("rows:\n%+v\nwant\n%+v", got, r.wantRows(1, want, "fake-model-1"))
	}
	for _, row := range r.rows() {
		if row.Metric == "continuation.hidden.off.asked_again" && row.Value != 1 {
			t.Fatalf("asked_again %v", row.Value)
		}
	}
}

// Without Delivered no row says asked_again.
func TestNoAskedAgainWhenNotMeasured(t *testing.T) {
	r := newRig(t, 2)
	if _, out, err := r.run(); err != nil {
		t.Fatalf("run: %v\n%s", err, out)
	}
	for _, row := range r.rows() {
		if strings.HasSuffix(row.Metric, ".asked_again") {
			t.Fatalf("row %+v", row)
		}
	}
	if len(r.rows()) != 9 {
		t.Fatalf("%d rows", len(r.rows()))
	}
}

// Usage errors start nothing.
func TestUsageErrors(t *testing.T) {
	for name, c := range map[string]struct {
		set  func(t *testing.T, o *Options)
		want string // what the error says
	}{
		"no host":               {func(_ *testing.T, o *Options) { o.Host = "" }, "--host is not a host id"},
		"an unknown host":       {func(_ *testing.T, o *Options) { o.Host = "made-up-host" }, "--host is not a host id"},
		"a relative CLI":        {func(_ *testing.T, o *Options) { o.CLI = "claude" }, "--cli: not an absolute path"},
		"a CLI that is no file": {func(_ *testing.T, o *Options) { o.CLI = filepath.Join(filepath.Dir(o.CLI), "made-up-cli") }, "--cli: no such file"},
		"no trials":             {func(_ *testing.T, o *Options) { o.Trials = 0 }, "--trials is under 1"},
		"no session ceiling":    {func(_ *testing.T, o *Options) { o.MaxSessions = 0 }, "--max-sessions is missing or under 1"},
		"no token ceiling":      {func(_ *testing.T, o *Options) { o.MaxTokens = 0 }, "--max-tokens is missing or under 1"},
		"a pass-env with =":     {func(_ *testing.T, o *Options) { o.PassEnv = append(o.PassEnv, "A=B") }, "is not a variable's name"},
		"a pass-env the driver": {func(_ *testing.T, o *Options) { o.PassEnv = append(o.PassEnv, "HOME") }, "--pass-env HOME is one the driver sets itself"},
		// Set, so that only the driver's setting it can refuse it.
		"a pass-env a host's off": {func(t *testing.T, o *Options) {
			t.Setenv(fakehost.OffVar, "0")
			o.PassEnv = append(o.PassEnv, fakehost.OffVar)
		}, "--pass-env " + fakehost.OffVar + " is one the driver sets itself"},
		"a pass-env not set":     {func(_ *testing.T, o *Options) { o.PassEnv = append(o.PassEnv, "EVAL_MADE_UP_UNSET") }, "--pass-env EVAL_MADE_UP_UNSET is not set"},
		"a pass-env given twice": {func(_ *testing.T, o *Options) { o.PassEnv = append(o.PassEnv, o.PassEnv[0]) }, "is given twice"},
		"a model not an id":      {func(_ *testing.T, o *Options) { o.Model = "Made Up" }, "--model is not a model id"},
		"no results folder":      {func(_ *testing.T, o *Options) { o.Out = "" }, "--out is missing"},
		"no tasks":               {func(_ *testing.T, o *Options) { o.Tasks = filepath.Join(filepath.Dir(o.Tasks), "none") }, "task: "},
	} {
		t.Run(name, func(t *testing.T) {
			r := newRig(t, 1)
			c.set(t, &r.opts)
			_, _, err := r.run()
			var usage *UsageError
			if !errors.As(err, &usage) || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("run: %v, want an error saying %q", err, c.want)
			}
			if calls := r.calls(); len(calls) != 0 {
				t.Fatalf("the fake ran: %+v", calls)
			}
		})
	}
}

// --pass-env passes no variable that points a host, git or the XDG
// folders at a configuration outside the trial, or reaches the invoking
// user's keys, and none whose value reaches the invoking home: each is
// refused before anything starts.
func TestPassEnvThatReachesOutIsRefused(t *testing.T) {
	for name, c := range map[string]struct{ name, value string }{
		"a host's configuration":  {"CODEX_HOME", "/made-up/codex"},
		"another host's":          {"CLAUDE_CONFIG_DIR", "/made-up/claude"},
		"git's":                   {"GIT_CONFIG_GLOBAL", "/made-up/gitconfig"},
		"git's folder":            {"GIT_DIR", "/made-up/repo/.git"},
		"the XDG folders":         {"XDG_CONFIG_HOME", "/made-up/config"},
		"the keys":                {"SSH_AUTH_SOCK", "/made-up/agent.sock"},
		"the home":                {"EVAL_MADE_UP_DIR", "{home}"},
		"in the home":             {"EVAL_MADE_UP_DIR", "{home}/.config/made-up"},
		"in the home, unresolved": {"EVAL_MADE_UP_DIR", "{home}/../home/.made-up"},
		"holding the home":        {"EVAL_MADE_UP_DIR", "/"},
		"in a list":               {"EVAL_MADE_UP_PATH", "/made-up/bin:{home}/bin"},
		"after an =":              {"EVAL_MADE_UP_OPTS", "--config={home}/x.toml"},
		"by a tilde":              {"EVAL_MADE_UP_DIR", "~/.made-up"},
		// The chat runs in <work>/<run>/<task>/<condition>/t<n>/workspace,
		// and the rig's work folder is beside its home.
		"climbing to the home": {"EVAL_MADE_UP_DIR", "../../../../../../home/.made-up"},
		"climbing past it":     {"EVAL_MADE_UP_PATH", "/usr/bin:../../../../../../.."},
	} {
		t.Run(name, func(t *testing.T) {
			r := newRig(t, 1)
			t.Setenv(c.name, strings.ReplaceAll(c.value, "{home}", os.Getenv("HOME")))
			r.opts.PassEnv = append(r.opts.PassEnv, c.name)
			_, out, err := r.run()
			var usage *UsageError
			if !errors.As(err, &usage) || out != "" {
				t.Fatalf("run: %v\n%s", err, out)
			}
			if calls := r.calls(); len(calls) != 0 {
				t.Fatalf("the fake ran: %+v", calls)
			}
		})
	}
	// A path outside the home, a relative one that stays in the trial, and
	// a short switch, pass.
	r := newRig(t, 1)
	t.Setenv("EVAL_MADE_UP_DIR", filepath.Join(r.base, "elsewhere")+string(filepath.ListSeparator)+"/usr/bin")
	t.Setenv("EVAL_MADE_UP_REL", "../home/.config:../../../../../../elsewhere")
	t.Setenv("EVAL_MADE_UP_SWITCH", "1")
	r.opts.PassEnv = append(r.opts.PassEnv, "EVAL_MADE_UP_DIR", "EVAL_MADE_UP_REL", "EVAL_MADE_UP_SWITCH")
	if s, out, err := r.run(); err != nil || s.Judged != 1 {
		t.Fatalf("run: %+v %v\n%s", s, err, out)
	}
}

// A relative path in a passed value is read from where the chat really
// runs: under --work with its links resolved. Through a link, a path that
// climbs from the unresolved folder would end outside the home, but from
// the resolved one it reaches the home, and is refused.
func TestPassEnvRelativeToTheResolvedWork(t *testing.T) {
	r := newRig(t, 1)
	real := filepath.Join(r.base, "deep", "er", "work")
	mkdir(t, real)
	r.opts.Work = filepath.Join(r.base, "to-work")
	if err := os.Symlink(real, r.opts.Work); err != nil {
		t.Fatal(err)
	}
	// Eight folders up from the resolved workspace is the rig's folder.
	t.Setenv("EVAL_MADE_UP_DIR", "../../../../../../../../home")
	r.opts.PassEnv = append(r.opts.PassEnv, "EVAL_MADE_UP_DIR")
	_, out, err := r.run()
	var usage *UsageError
	if !errors.As(err, &usage) || out != "" || !strings.Contains(err.Error(), "reaches the invoking home") {
		t.Fatalf("run: %v\n%s", err, out)
	}
	if calls := r.calls(); len(calls) != 0 {
		t.Fatalf("the fake ran: %+v", calls)
	}
}

// A value reaches the home wherever the home's path is in it whole, even a
// home whose name holds a space, a comma or an "=", which the parts a
// value splits into would cut; a sibling of the home whose name starts as
// the home's does not.
func TestReachesHome(t *testing.T) {
	for _, home := range []string{"/made-up/the home", "/made-up/a,b=c", "/made-up/plain"} {
		for value, want := range map[string]bool{
			home:                           true,
			home + "/.config":              true,
			"--config=" + home + "/x.toml": true,
			"/usr/bin:" + home + "/bin":    true,
			"'" + home + "'":               true,
			home + "-other/x":              false,
			home + "s":                     false,
			"/made-up/elsewhere":           false,
			"/made-up":                     true,
			"plain words":                  false,
		} {
			if got := reachesHome(value, []string{home}, "/made-up/work/run/task/off/t1/workspace"); got != want {
				t.Errorf("home %q, value %q: %v", home, value, got)
			}
		}
	}
	// A relative path is read from the folder the chat runs in: one that
	// stays in the trial passes, and one that climbs to the home, or past
	// it, does not.
	for value, want := range map[string]bool{
		"../home/.config":                        false,
		"sub/dir":                                false,
		"../../../../../../elsewhere":            false,
		"../../../../../../home":                 true,
		"../../../../../../home/.config":         true,
		"../../../../../..":                      true,
		"--config=../../../../../../home/x.toml": true,
		"/usr/bin:../../../../../../home/bin":    true,
	} {
		if got := reachesHome(value, []string{"/made-up/home"}, "/made-up/work/run/task/off/t1/workspace"); got != want {
			t.Errorf("value %q: %v", value, got)
		}
	}
}

// A passed value the host names as its model reaches no row: the rows are
// refused and the run stops.
func TestAPassedValueAsTheModelWritesNoRow(t *testing.T) {
	r := newRig(t, 1)
	const secret = "made-up-key-qx7z31-not-real"
	t.Setenv("EVAL_MADE_UP_KEY", secret)
	r.opts.PassEnv = append(r.opts.PassEnv, "EVAL_MADE_UP_KEY")
	r.script.Model = secret
	s, out, err := r.run()
	if err == nil || s.Judged != 0 || strings.Contains(out, secret) || strings.Contains(err.Error(), secret) {
		t.Fatalf("run: %+v %v\n%s", s, err, out)
	}
	if rows := r.rows(); rows != nil {
		t.Fatalf("rows: %+v", rows)
	}
}

// A call that gives no answer is never graded: a failure of the host's
// service (an overload or another 5xx), an exit having printed nothing, a
// CLI killed by a signal, before an answer or after a whole one, and an
// answer naming no session when the trial needs a follow-up. Each leaves
// its trial unjudged, host_failed, with no row, in the first round or a
// later one; the run goes on, and the next trial, whose session the fake
// answers as usual, writes its rows exactly.
func TestCallsWithNoAnswerAreHostFailed(t *testing.T) {
	for _, h := range adapter.All() {
		for name, set := range map[string]func(s *fakehost.Script){
			"a 529":                  func(s *fakehost.Script) { s.Status = 529 },
			"a 500":                  func(s *fakehost.Script) { s.Status = 500 },
			"nothing printed":        func(s *fakehost.Script) { s.Empty = true },
			"killed":                 func(s *fakehost.Script) { s.Killed = true },
			"killed after answering": func(s *fakehost.Script) { s.KilledAfter = true },
			"no session":             func(s *fakehost.Script) { s.NoSession = true },
			"a 529 in round two":     func(s *fakehost.Script) { s.Status, s.From = 529, 2 },
			"killed in round two":    func(s *fakehost.Script) { s.Killed, s.From = true, 2 },
			"killed after answering in round two": func(s *fakehost.Script) {
				s.KilledAfter, s.From = true, 2
			},
		} {
			t.Run(string(h.ID)+"/"+name, func(t *testing.T) {
				r := newRig(t, 1)
				r.opts.Host, r.opts.Trials = string(h.ID), 2
				set(&r.script)
				r.script.Session = 1
				s, out, err := r.run()
				if err != nil || s.Judged != 1 || s.Unjudged[CodeHostFailed] != 1 || len(s.Unjudged) != 1 || s.Rows != 9 {
					t.Fatalf("run: %+v %v\n%s", s, err, out)
				}
				if !strings.Contains(out, "trial hidden-banner.t1: unjudged, host_failed\n") ||
					!strings.Contains(out, "trial hidden-banner.t2: solved 1, first try 0, corrections 1, rounds 2\n") ||
					!strings.Contains(out, ", host_failed 1, ") {
					t.Fatalf("output:\n%s", out)
				}
				want := task.Outcome{Solved: true, Corrections: 1, Rounds: 2, AgentEvents: 4, PromptTokens: 2000, CachedTokens: 1200, OutputTokens: 100, WallMs: 2000}
				if got := timeless(r.rows()); !reflect.DeepEqual(got, r.wantRows(2, want, "fake-model-1")) {
					t.Fatalf("rows:\n%+v\nwant\n%+v", got, r.wantRows(2, want, "fake-model-1"))
				}
				// The first trial's chats: one, or two when the second round
				// failed; then the second trial's two.
				first := 1
				if r.script.From == 2 {
					first = 2
				}
				if chats := countMode(r.calls(), "start") + countMode(r.calls(), "continue"); chats != first+2 {
					t.Fatalf("%d chats, want %d", chats, first+2)
				}
			})
		}
	}
}

// A CLI that a signal ends after it printed a whole answer is not graded,
// though the answer would be solved: under the ideal record the fake lays
// the solution and answers in its first call, then is killed.
func TestAnAnswerThenASignalIsNotGraded(t *testing.T) {
	for _, h := range adapter.All() {
		t.Run(string(h.ID), func(t *testing.T) {
			r := newRig(t, 1)
			r.opts.Host, r.opts.Condition = string(h.ID), task.Oracle
			r.script.KilledAfter = true
			s, out, err := r.run()
			if err != nil || s.Judged != 0 || s.Unjudged[CodeHostFailed] != 1 || strings.Contains(out, "solved") {
				t.Fatalf("run: %+v %v\n%s", s, err, out)
			}
			if rows := r.rows(); rows != nil {
				t.Fatalf("rows: %+v", rows)
			}
			// What the killed call printed is counted against the ceiling.
			if s.Tokens != r.script.Prompt+r.script.Output || countMode(r.calls(), "start") != 1 {
				t.Fatalf("tokens %d, calls %+v", s.Tokens, r.calls())
			}
			// Without the signal the same answer is solved.
			r = newRig(t, 1)
			r.opts.Host, r.opts.Condition = string(h.ID), task.Oracle
			if s, out, err := r.run(); err != nil || s.Judged != 1 || !strings.Contains(out, "solved 1, first try 1") {
				t.Fatalf("run: %+v %v\n%s", s, err, out)
			}
		})
	}
}

// A session that names no model is judged, whether or not --model was
// given: its rows carry no model, and the run counts it. One that names
// another model than the one asked for is other_model; one that names the
// one asked for, or any when none was asked for, is judged with it.
func TestASessionThatNamesNoModel(t *testing.T) {
	for _, h := range adapter.All() {
		for name, c := range map[string]struct {
			asked   string
			noModel bool
			named   string // the model the fake names, when it names one
			model   string // the rows' model; "" for none
			code    string // the unjudged code, or "" for judged
		}{
			"none named, one asked":   {"made-up-model-7", true, "", "", ""},
			"none named, none asked":  {"", true, "", "", ""},
			"another named":           {"made-up-model-7", false, "made-up-model-8", "", CodeOtherModel},
			"the one asked named":     {"made-up-model-7", false, "", "made-up-model-7", ""},
			"one named, none asked":   {"", false, "made-up-model-8", "made-up-model-8", ""},
			"the default, none asked": {"", false, "", "fake-model-1", ""},
		} {
			t.Run(string(h.ID)+"/"+name, func(t *testing.T) {
				r := newRig(t, 1)
				r.opts.Host, r.opts.Model = string(h.ID), c.asked
				r.script.NoModel, r.script.Model = c.noModel, c.named
				s, out, err := r.run()
				if err != nil {
					t.Fatalf("run: %v\n%s", err, out)
				}
				if c.code != "" {
					if s.Judged != 0 || s.Unjudged[c.code] != 1 || s.NoModel != 0 || r.rows() != nil {
						t.Fatalf("run: %+v\n%s", s, out)
					}
					return
				}
				noModel := 0
				if c.model == "" {
					noModel = 1
				}
				if s.Judged != 1 || len(s.Unjudged) != 0 || s.NoModel != noModel || !strings.Contains(out, "judged 1, no model "+strconv.Itoa(noModel)+",") {
					t.Fatalf("run: %+v\n%s", s, out)
				}
				want := task.Outcome{Solved: true, Corrections: 1, Rounds: 2, AgentEvents: 4, PromptTokens: 2000, CachedTokens: 1200, OutputTokens: 100, WallMs: 2000}
				if got := timeless(r.rows()); !reflect.DeepEqual(got, r.wantRows(1, want, c.model)) {
					t.Fatalf("rows:\n%+v\nwant\n%+v", got, r.wantRows(1, want, c.model))
				}
			})
		}
	}
}

// A passed value that every row of the run would hold is refused before
// any trial starts, so that no quota is spent on rows the store would
// refuse: the model asked for, the host's id, a task's id, the commit and
// the rows' own words. The fake records nothing.
func TestAPassedValueTheRowsWouldHoldIsRefused(t *testing.T) {
	for name, value := range map[string]func(r *rig) string{
		"the model asked for": func(r *rig) string { return r.opts.Model },
		"the host's id":       func(r *rig) string { return "Claude_Code" },
		"a task's id":         func(r *rig) string { return fakehost.FixtureID },
		"the commit":          func(r *rig) string { return r.commit()[4:16] },
		"the instrument":      func(r *rig) string { return task.Instrument },
	} {
		t.Run(name, func(t *testing.T) {
			r := newRig(t, 1)
			r.opts.Model = "made-up-model-7"
			t.Setenv("EVAL_MADE_UP_VALUE", value(r))
			r.opts.PassEnv = append(r.opts.PassEnv, "EVAL_MADE_UP_VALUE")
			s, out, err := r.run()
			var usage *UsageError
			if !errors.As(err, &usage) || !strings.Contains(err.Error(), "--pass-env EVAL_MADE_UP_VALUE holds a value the run's rows would hold") ||
				strings.Contains(err.Error(), value(r)) || s.Run != "" {
				t.Fatalf("run: %+v %v\n%s", s, err, out)
			}
			if calls := r.calls(); len(calls) != 0 {
				t.Fatalf("the fake ran: %+v", calls)
			}
			if _, err := os.Lstat(r.opts.Work); !errors.Is(err, fs.ErrNotExist) {
				t.Fatal("the work folder was made")
			}
		})
	}
}

// An answer that names no session is graded when no follow-up is needed,
// and one that ends at the turn bound, exiting 1, is graded as it stands.
func TestAnswersThatAreGraded(t *testing.T) {
	r := newRig(t, 1)
	r.opts.Condition = task.Oracle
	r.script.NoSession = true
	if s, out, err := r.run(); err != nil || s.Judged != 1 {
		t.Fatalf("no session: %+v %v\n%s", s, err, out)
	}
	r = newRig(t, 1)
	r.script.TurnBound = true
	s, out, err := r.run()
	if err != nil || s.Judged != 1 || !strings.Contains(out, "solved 1, first try 0, corrections 1, rounds 2") {
		t.Fatalf("the turn bound: %+v %v\n%s", s, err, out)
	}
}

// A stalled call's whole process group is killed at the session's limit:
// the fake's child, which holds the call's output, goes with it, so the
// trial ends at once as timeout, with no row. The limit ends at an exact
// point, once the child runs; were the child left alive, the wait for the
// output would last the raised WaitDelay, past the guard.
func TestStalledSessionsGroupIsKilled(t *testing.T) {
	for _, h := range adapter.All() {
		t.Run(string(h.ID), func(t *testing.T) {
			r := newRig(t, 1)
			r.opts.Host = string(h.ID)
			ready := filepath.Join(r.base, "ready")
			if err := syscall.Mkfifo(ready, 0o600); err != nil {
				t.Fatal(err)
			}
			r.script.Stall, r.script.Ready = true, ready
			var limits []time.Duration
			r.opts.sessionLimit = func(ctx context.Context, d time.Duration) (context.Context, context.CancelFunc) {
				limits = append(limits, d)
				c, cancel := context.WithCancel(ctx)
				go func() {
					// Opening blocks until the child opens the pipe to write.
					if f, err := os.Open(ready); err == nil {
						_, _ = io.ReadAll(f)
						_ = f.Close()
					}
					cancel()
				}()
				return c, cancel
			}
			res := guarded(t, r)
			if res.err != nil || res.s.Unjudged[CodeTimeout] != 1 || res.s.Judged != 0 || !strings.Contains(res.out, "unjudged, timeout") {
				t.Fatalf("run: %+v %v\n%s", res.s, res.err, res.out)
			}
			if rows := r.rows(); rows != nil {
				t.Fatalf("an unjudged trial wrote rows: %+v", rows)
			}
			if !slices.Equal(limits, []time.Duration{time.Hour}) || countMode(r.calls(), "start") != 1 {
				t.Fatalf("limits %v, calls %+v", limits, r.calls())
			}
		})
	}
}

// A call that answers and exits leaving a child that holds its output is
// graded at once: the child is killed with the call's group as soon as the
// call has exited, so it can neither hold the trial nor edit the workspace
// while it is graded.
func TestALeftChildIsKilled(t *testing.T) {
	for _, h := range adapter.All() {
		t.Run(string(h.ID), func(t *testing.T) {
			r := newRig(t, 1)
			r.opts.Host = string(h.ID)
			r.script.Leave = true
			res := guarded(t, r)
			want := task.Outcome{Solved: true, Corrections: 1, Rounds: 2, AgentEvents: 4, PromptTokens: 2000, CachedTokens: 1200, OutputTokens: 100, WallMs: 2000}
			if res.err != nil || res.s.Judged != 1 {
				t.Fatalf("run: %+v %v\n%s", res.s, res.err, res.out)
			}
			if got := timeless(r.rows()); !reflect.DeepEqual(got, r.wantRows(1, want, "fake-model-1")) {
				t.Fatalf("rows:\n%+v\nwant\n%+v", got, r.wantRows(1, want, "fake-model-1"))
			}
		})
	}
}

// The session's time limit bounds its calls, not their grading: each call
// is given what is left of it, and a call past it leaves the trial
// unjudged, timeout, with no row, though an earlier round was graded.
func TestSessionLimitIsForCalls(t *testing.T) {
	r := newRig(t, 1)
	var limits []time.Duration
	r.opts.sessionLimit = func(ctx context.Context, d time.Duration) (context.Context, context.CancelFunc) {
		limits = append(limits, d)
		c, cancel := context.WithCancel(ctx)
		if len(limits) == 2 {
			cancel()
		}
		return c, cancel
	}
	s, out, err := r.run()
	if err != nil || s.Unjudged[CodeTimeout] != 1 || s.Judged != 0 {
		t.Fatalf("run: %+v %v\n%s", s, err, out)
	}
	if rows := r.rows(); rows != nil {
		t.Fatalf("rows: %+v", rows)
	}
	if len(limits) != 2 || limits[0] != r.opts.SessionTimeout || limits[1] >= limits[0] || limits[1] <= 0 {
		t.Fatalf("limits %v", limits)
	}
}

type guardedResult struct {
	s   Summary
	out string
	err error
}

// guarded runs the rig, failing when the run does not end within a guard
// far above any run here.
func guarded(t *testing.T, r *rig) guardedResult {
	t.Helper()
	done := make(chan guardedResult, 1)
	go func() {
		s, out, err := r.run()
		done <- guardedResult{s, out, err}
	}()
	select {
	case res := <-done:
		return res
	case <-time.After(5 * time.Minute):
		t.Fatal("the run did not end: a process the call left holds its output")
	}
	return guardedResult{}
}
