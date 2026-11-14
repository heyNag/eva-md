package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/heyNag/eva-md/internal/eval/continuation/drive"
	"github.com/heyNag/eva-md/internal/eval/continuation/drive/fakehost"
	"github.com/heyNag/eva-md/internal/eval/results"
)

// The fake host runs this test binary again under a host CLI's name. The
// driver's limits are raised once, so that the fake's version and a killed
// call's output are waited for long enough on a slow machine.
func init() {
	fakehost.Main()
	drive.VersionTimeout = 10 * time.Minute
	drive.WaitDelay = 10 * time.Minute
}

// contRig is a made-up world for eva-eval continuation: a home, the fake
// host's fixture task, a repository the rows' commit is read from, and
// the fake's script and record.
type contRig struct {
	base, tasks, repos, repo, record, script, cli string
}

func newContRig(t *testing.T) contRig {
	t.Helper()
	isolate(t)
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	r := contRig{base: base, repo: filepath.Join(base, "repo"), record: filepath.Join(base, "record"), script: filepath.Join(base, "script.json")}
	if r.tasks, r.repos, err = fakehost.Fixture(filepath.Join(base, "data"), 1); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(r.repo, "README.md"), "measured\n")
	gitIn(t, r.repo, "init", "--quiet")
	gitIn(t, r.repo, "add", ".")
	gitIn(t, r.repo, "commit", "--quiet", "-m", "first")
	if err := os.Mkdir(r.record, 0o755); err != nil {
		t.Fatal(err)
	}
	if r.cli, err = os.Executable(); err != nil {
		t.Fatal(err)
	}
	if r.cli, err = filepath.EvalSymlinks(r.cli); err != nil {
		t.Fatal(err)
	}
	r.setScript(t, fakehost.Script{})
	t.Setenv(fakehost.ScriptVar, r.script)
	// The test's folders may be under /tmp, as they are when TMPDIR is: the
	// temporary root --work may not overlap is one of the rig's own.
	was := drive.TempFolders
	drive.TempFolders = []string{r.tmpRoot()}
	t.Cleanup(func() { drive.TempFolders = was })
	return r
}

// tmpRoot is the temporary root the rig gives the driver.
func (r contRig) tmpRoot() string { return filepath.Join(r.base, "made-up-tmp") }

// setScript writes the fake's script, with the rig's record and task.
func (r contRig) setScript(t *testing.T, s fakehost.Script) {
	t.Helper()
	s.Events, s.Prompt, s.Cached, s.Output = 3, 900, 400, 30
	r.writeScript(t, s)
}

// writeScript writes the fake's script with the rig's record and task, and
// the tokens s gives.
func (r contRig) writeScript(t *testing.T, s fakehost.Script) {
	t.Helper()
	s.Dir, s.Task, s.Facts = r.record, filepath.Join(r.tasks, fakehost.FixtureID), []string{fakehost.FixtureColor}
	data, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	write(t, r.script, string(data))
}

func (r contRig) args(more ...string) []string {
	return append([]string{"continuation", "run", "--tasks", r.tasks, "--repos", r.repos, "--host", "codex", "--cli", r.cli,
		"--condition", "off", "--trials", "2", "--work", filepath.Join(r.base, "work"), "--out", filepath.Join(r.base, "out"),
		"--max-sessions", "2", "--max-tokens", "100000", "--pass-env", fakehost.ScriptVar, "--repo", r.repo}, more...)
}

func (r contRig) calls(t *testing.T) []fakehost.Call {
	t.Helper()
	calls, err := fakehost.Read(r.record)
	if err != nil {
		t.Fatal(err)
	}
	return calls
}

// The command lists its two forms in eva-eval's usage.
func TestContinuationIsRegistered(t *testing.T) {
	code, _, errOut := cmd("continuation")
	if code != 2 || !strings.Contains(errOut, "eva-eval continuation run --tasks DIR") || !strings.Contains(errOut, "eva-eval continuation check --tasks DIR --repos DIR") {
		t.Fatalf("exit %d\n%s", code, errOut)
	}
	if code, _, errOut := cmd("nothing"); code != 2 || !strings.Contains(errOut, "eva-eval continuation <command> ...") {
		t.Fatalf("eva-eval's usage: %d\n%s", code, errOut)
	}
}

// A command another file adds to the continuation's map runs as a
// subcommand, with the arguments after its name, and its usage line is
// listed; a name taken, or not of lower-case letters and dashes, or a
// command with no usage line, is refused.
func TestContinuationCommandsAreAMap(t *testing.T) {
	var got []string
	addContinuation("made-up", continuationCommand{synopsis: "made-up --flag N", run: func(_ context.Context, args []string, out, _ io.Writer) int {
		got = args
		fmt.Fprintln(out, "made-up ran")
		return 7
	}})
	t.Cleanup(func() { delete(continuationCommands, "made-up") })
	code, out, _ := cmd("continuation", "made-up", "--flag", "3")
	if code != 7 || out != "made-up ran\n" || !slices.Equal(got, []string{"--flag", "3"}) {
		t.Fatalf("exit %d, args %v\n%s", code, got, out)
	}
	if code, _, errOut := cmd("continuation"); code != 2 || !strings.Contains(errOut, "\n  eva-eval continuation made-up --flag N\n") ||
		!strings.Contains(errOut, "\n  eva-eval continuation check --tasks DIR") {
		t.Fatalf("exit %d\n%s", code, errOut)
	}
	for name, c := range map[string]struct {
		name string
		cmd  continuationCommand
	}{
		"taken":           {"run", continuationCommand{synopsis: "run", run: continuationRun}},
		"not a name":      {"Made_Up", continuationCommand{synopsis: "Made_Up", run: continuationRun}},
		"no usage line":   {"other", continuationCommand{run: continuationRun}},
		"nothing to run":  {"other", continuationCommand{synopsis: "other"}},
		"two usage lines": {"other", continuationCommand{synopsis: "other\nmore", run: continuationRun}},
	} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("%s: added", name)
				}
			}()
			addContinuation(c.name, c.cmd)
		}()
	}
	if _, ok := continuationCommands["other"]; ok {
		t.Fatal("a refused command was added")
	}
}

// A run on the fake writes the rows of two trials, which the catalogue
// accepts, prints ids and figures only, and exits 0. A passed variable's
// value is in neither output nor a row.
func TestContinuationRun(t *testing.T) {
	r := newContRig(t)
	const secret = "made-up-key-PL4N9-not-real"
	t.Setenv("EVAL_MADE_UP_KEY", secret)
	code, out, errOut := cmd(r.args("--pass-env", "EVAL_MADE_UP_KEY", "--model", "made-up-model-3", "--spend-model-quota")...)
	if code != 0 || errOut != "" {
		t.Fatalf("exit %d\n%s\n%s", code, out, errOut)
	}
	lines := strings.Split(strings.TrimSuffix(out, "\n"), "\n")
	if len(lines) != 5 || lines[0] != "plan: condition off, host codex, tasks 1, trials 2 each, sessions 2; ceilings: sessions 2, tokens 100000; trial folders removed" ||
		lines[2] != "trial hidden-banner.t1: solved 1, first try 0, corrections 1, rounds 2" ||
		!strings.HasSuffix(lines[4], ": planned 2, judged 2, no model 0, not run 0, unjudged sign_in 0, rate_limited 0, timeout 0, other_model 0, setup_failed 0, update_failed 0, host_failed 0, no_usage 0, memory_off 0, hidden_found 0; tokens 3720, rows 18") {
		t.Fatalf("output:\n%s", out)
	}
	if strings.Contains(out, secret) || strings.Contains(out, r.base) || strings.Contains(out, fakehost.FixtureColor) || strings.Contains(out, fakehost.FixtureRepo) {
		t.Fatalf("the output holds a value or a path:\n%s", out)
	}
	rows, err := results.Load(filepath.Join(r.base, "out"))
	if err != nil {
		t.Fatal(err)
	}
	cat, err := results.LoadCatalogue(filepath.Join("..", "..", "eval", "continuation", "metrics.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := cat.Check(rows); err != nil || len(rows) != 18 {
		t.Fatalf("%d rows: %v", len(rows), err)
	}
	commit := gitIn(t, r.repo, "rev-parse", "HEAD")
	for _, row := range rows {
		if row.Commit != commit || row.Dirty || row.Host != "codex" || row.HostVersion != "1.2.3" || row.Model != "made-up-model-3" || row.Instrument != "continuation" {
			t.Fatalf("row %+v", row)
		}
	}
	data, err := os.ReadFile(filepath.Join(r.base, "out", results.FileName))
	if err != nil || strings.Contains(string(data), secret) {
		t.Fatalf("the rows file: %v", err)
	}
}

// Without --spend-model-quota the plan prints, nothing starts and it
// exits 2; so does a usage error, which says what is wrong.
func TestContinuationRunRefuses(t *testing.T) {
	r := newContRig(t)
	code, out, errOut := cmd(r.args()...)
	if code != 2 || !strings.HasPrefix(out, "plan: ") || !strings.Contains(errOut, "--spend-model-quota") {
		t.Fatalf("exit %d\n%s\n%s", code, out, errOut)
	}
	for _, args := range [][]string{
		r.args("--spend-model-quota", "--max-sessions", "1"),
		r.args("--spend-model-quota", "--condition", "eva"),
		r.args("--spend-model-quota", "--work", filepath.Join(r.tmpRoot(), "work")),
		r.args("--spend-model-quota", "--pass-env", "A=B"),
		r.args("--spend-model-quota", "--session-timeout", "0s"),
		r.args("--spend-model-quota", "--made-up-flag"),
		r.args("--spend-model-quota", "stray"),
		{"continuation", "run", "--spend-model-quota"},
		{"continuation", "made-up"},
	} {
		if code, _, errOut := cmd(args...); code != 2 || !strings.Contains(errOut, "usage:") {
			t.Fatalf("%v: exit %d\n%s", args, code, errOut)
		}
	}
	if calls := r.calls(t); len(calls) != 0 {
		t.Fatalf("the fake ran: %+v", calls)
	}
	for _, p := range []string{filepath.Join(r.base, "work"), filepath.Join(r.base, "out"), r.tmpRoot()} {
		if _, err := os.Lstat(p); !os.IsNotExist(err) {
			t.Fatalf("%s was made", p)
		}
	}
}

// A trial left unjudged makes the run exit 1, with no row.
func TestContinuationRunUnjudged(t *testing.T) {
	r := newContRig(t)
	r.setScript(t, fakehost.Script{SignIn: true})
	code, out, errOut := cmd(r.args("--spend-model-quota")...)
	if code != 1 || errOut != "" || !strings.Contains(out, "unjudged sign_in 2,") {
		t.Fatalf("exit %d\n%s\n%s", code, out, errOut)
	}
	if _, err := os.Stat(filepath.Join(r.base, "out", results.FileName)); !os.IsNotExist(err) {
		t.Fatal("an unjudged run wrote rows")
	}
	// It wrote its record, which counts both.
	records, err := drive.LoadRuns(filepath.Join(r.base, "out"))
	if err != nil || len(records) != 1 || records[0].Unjudged[drive.CodeSignIn] != 2 || records[0].Ended != drive.EndedDone {
		t.Fatalf("records %+v: %v", records, err)
	}
}

// A call with no answer leaves its trial unjudged, host_failed, and the run
// goes on: the next trial writes its rows, and the run exits 1. A session
// that names no model is judged, and counted.
func TestContinuationRunGoesOnPastAHostFailure(t *testing.T) {
	r := newContRig(t)
	r.setScript(t, fakehost.Script{Status: 529, Session: 1, NoModel: true})
	code, out, errOut := cmd(r.args("--spend-model-quota", "--model", "made-up-model-3")...)
	if code != 1 || errOut != "" || !strings.Contains(out, "trial hidden-banner.t1: unjudged, host_failed\n") ||
		!strings.Contains(out, ": planned 2, judged 1, no model 1, not run 0, ") || !strings.Contains(out, ", host_failed 1, ") {
		t.Fatalf("exit %d\n%s\n%s", code, out, errOut)
	}
	rows, err := results.Load(filepath.Join(r.base, "out"))
	if err != nil || len(rows) != 9 {
		t.Fatalf("%d rows: %v", len(rows), err)
	}
	for _, row := range rows {
		if row.Scenario != "hidden-banner.t2" || row.Model != "" {
			t.Fatalf("row %+v", row)
		}
	}
}

// On each host, a call that answers without its usage, or with 0 tokens,
// stops the run: no row, the rest not run, the summary printed, the
// record's ended no_usage, and exit 2.
func TestContinuationRunStopsOnNoUsage(t *testing.T) {
	for _, host := range []string{"claude-code", "codex"} {
		for name, script := range map[string]fakehost.Script{
			"no usage": {NoUsage: true, Events: 3, Prompt: 900, Cached: 400, Output: 30},
			"0 tokens": {Events: 3},
		} {
			t.Run(host+"/"+name, func(t *testing.T) {
				r := newContRig(t)
				r.writeScript(t, script)
				code, out, errOut := cmd(r.args("--spend-model-quota", "--host", host)...)
				if code != 2 || !strings.Contains(errOut, "the run stopped") || !strings.Contains(out, "trial hidden-banner.t1: unjudged, no_usage\n") ||
					!strings.Contains(out, ": planned 2, judged 0, no model 0, not run 1, ") || !strings.Contains(out, "host "+host+",") {
					t.Fatalf("exit %d\n%s\n%s", code, out, errOut)
				}
				if _, err := os.Stat(filepath.Join(r.base, "out", results.FileName)); !os.IsNotExist(err) {
					t.Fatal("a run with no usage wrote rows")
				}
				if starts := len(r.calls(t)); starts != 2 {
					t.Fatalf("%d calls, want the version and one chat", starts)
				}
				records, err := drive.LoadRuns(filepath.Join(r.base, "out"))
				if err != nil || len(records) != 1 || records[0].Ended != drive.CodeNoUsage || records[0].Host != host {
					t.Fatalf("records %+v: %v", records, err)
				}
			})
		}
	}
}

// --keep-trials keeps the trials' folders; without it the work folder is
// left empty. A second run of the same trials in the results folder is
// refused before its plan and starts nothing.
func TestContinuationRunKeepsTrialsAndRefusesASecondAttempt(t *testing.T) {
	r := newContRig(t)
	work := filepath.Join(r.base, "work")
	code, out, errOut := cmd(r.args("--spend-model-quota")...)
	if code != 0 || errOut != "" {
		t.Fatalf("exit %d\n%s\n%s", code, out, errOut)
	}
	if entries, err := os.ReadDir(work); err != nil || len(entries) != 0 {
		t.Fatalf("the work folder holds %v: %v", entries, err)
	}
	calls := len(r.calls(t))
	code, out, errOut = cmd(r.args("--spend-model-quota")...)
	if code != 2 || out != "" || !strings.Contains(errOut, "--out holds an earlier attempt") || len(r.calls(t)) != calls {
		t.Fatalf("exit %d\n%s\n%s", code, out, errOut)
	}
	args := r.args("--spend-model-quota", "--keep-trials", "--out", filepath.Join(r.base, "out-two"))
	code, out, errOut = cmd(args...)
	if code != 0 || !strings.Contains(out, "; trial folders kept, each closed to others once it ends\n") {
		t.Fatalf("exit %d\n%s\n%s", code, out, errOut)
	}
	entries, err := os.ReadDir(work)
	if err != nil || len(entries) != 1 {
		t.Fatalf("the work folder holds %v: %v", entries, err)
	}
	for _, n := range []string{"t1", "t2"} {
		if _, err := os.Stat(filepath.Join(work, entries[0].Name(), fakehost.FixtureID, "off", n, "workspace")); err != nil {
			t.Fatalf("trial %s: %v", n, err)
		}
	}
}

// --trace keeps each call's stdout and stderr in the folder it names, and
// the plan says so; one inside --out is refused before anything starts.
func TestContinuationRunTrace(t *testing.T) {
	r := newContRig(t)
	trace := filepath.Join(r.base, "trace")
	code, out, errOut := cmd(r.args("--spend-model-quota", "--trace", filepath.Join(r.base, "out", "trace"))...)
	if code != 2 || !strings.Contains(errOut, "--trace overlaps --out") || len(r.calls(t)) != 0 {
		t.Fatalf("exit %d\n%s\n%s", code, out, errOut)
	}
	code, out, errOut = cmd(r.args("--spend-model-quota", "--trace", trace)...)
	if code != 0 || errOut != "" || !strings.Contains(out, "; trial folders removed; each call's output kept in the trace\n") || strings.Contains(out, r.base) {
		t.Fatalf("exit %d\n%s\n%s", code, out, errOut)
	}
	var kept []string
	err := filepath.WalkDir(trace, func(p string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			kept = append(kept, filepath.Base(p))
		}
		return err
	})
	slices.Sort(kept)
	want := []string{"1-version.stderr", "1-version.stderr", "1-version.stdout", "1-version.stdout", "2-chat.stderr", "2-chat.stderr",
		"2-chat.stdout", "2-chat.stdout", "3-chat.stderr", "3-chat.stderr", "3-chat.stdout", "3-chat.stdout"}
	if err != nil || !slices.Equal(kept, want) {
		t.Fatalf("the trace holds %v: %v", kept, err)
	}
}

// check prints a line a task and exits 1 when one fails its check.
func TestContinuationCheck(t *testing.T) {
	r := newContRig(t)
	code, out, errOut := cmd("continuation", "check", "--tasks", r.tasks, "--repos", r.repos)
	if code != 0 || out != "hidden-banner ok\n" || errOut != "" {
		t.Fatalf("exit %d\n%s\n%s", code, out, errOut)
	}
	if err := os.Remove(filepath.Join(r.tasks, fakehost.FixtureID, "solution", fakehost.FixtureRepo, "answer.txt")); err != nil {
		t.Fatal(err)
	}
	code, out, _ = cmd("continuation", "check", "--tasks", r.tasks, "--repos", r.repos)
	if code != 1 || !strings.HasPrefix(out, "hidden-banner fail: ") {
		t.Fatalf("exit %d\n%s", code, out)
	}
	if code, _, _ := cmd("continuation", "check", "--tasks", r.tasks); code != 2 {
		t.Fatalf("check without --repos: exit %d", code)
	}
}
