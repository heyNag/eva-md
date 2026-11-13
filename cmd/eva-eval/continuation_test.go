package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/heyNag/eva-md/internal/eval/continuation/drive/fakehost"
	"github.com/heyNag/eva-md/internal/eval/results"
)

// The fake host runs this test binary again under a host CLI's name.
func init() { fakehost.Main() }

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
	return r
}

// setScript writes the fake's script, with the rig's record and task.
func (r contRig) setScript(t *testing.T, s fakehost.Script) {
	t.Helper()
	s.Dir, s.Task, s.Facts = r.record, filepath.Join(r.tasks, fakehost.FixtureID), []string{fakehost.FixtureColor}
	s.Events, s.Prompt, s.Cached, s.Output = 3, 900, 400, 30
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
	if code, _, errOut := cmd("nothing"); code != 2 || !strings.Contains(errOut, "eva-eval continuation run") {
		t.Fatalf("eva-eval's usage: %d\n%s", code, errOut)
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
	if len(lines) != 5 || lines[0] != "plan: condition off, host codex, tasks 1, trials 2 each, sessions 2; ceilings: sessions 2, tokens 100000" ||
		lines[2] != "trial hidden-banner.t1: solved 1, first try 0, corrections 1, rounds 2" ||
		!strings.HasSuffix(lines[4], ": planned 2, judged 2, not run 0, unjudged sign_in 0, rate_limited 0, timeout 0, other_model 0, setup_failed 0, update_failed 0; tokens 3720, rows 18") {
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
		r.args("--spend-model-quota", "--work", "/tmp/evamd-continuation-"+strconv.Itoa(os.Getpid())),
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
	for _, p := range []string{filepath.Join(r.base, "work"), filepath.Join(r.base, "out"), "/tmp/evamd-continuation-" + strconv.Itoa(os.Getpid())} {
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
	if _, err := os.Stat(filepath.Join(r.base, "out")); !os.IsNotExist(err) {
		t.Fatal("an unjudged run wrote rows")
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
