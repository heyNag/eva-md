package main

import (
	"bytes"
	"context"
	"errors"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/heyNag/eva-md/internal/testlimits"
	"github.com/heyNag/eva-md/internal/threadpage"
	"github.com/heyNag/eva-md/internal/update"
	"github.com/heyNag/eva-md/internal/update/accuracy"
	"github.com/heyNag/eva-md/internal/update/accuracy/live"
)

// The time limits that protect a host CLI's run, such as the wait for its
// output once it exits, are raised for every test, before any test runs, so
// a slow machine can't fail one.
func TestMain(m *testing.M) {
	testlimits.Raise()
	os.Exit(m.Run())
}

// isolate gives a test its own home, git config and temp folder, and a
// PATH of an empty folder and the system folders, so no real claude or
// codex can run. It returns the temp folder.
func isolate(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(home, "gitconfig"))
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	tmp := t.TempDir()
	t.Setenv("TMPDIR", tmp)
	t.Setenv("PATH", t.TempDir()+":/usr/bin:/bin")
	return tmp
}

// empty is an answer the validator accepts that changes nothing.
const empty = `{"summary":"","last_chat":"","done":[],"items":[],"timeline":[],"unknowns":[],"rules":[],"knowledge":[]}`

// envelope is what a fake claude prints for the empty answer.
const envelope = `{"type":"result","subtype":"success","is_error":false,"usage":{"input_tokens":120,"output_tokens":40},"permission_denials":[],"structured_output":` + empty + `}`

// fakeRunner records each request and answers it with answer, or with the
// empty answer at the effort asked for, after checking its run folder: an
// empty folder by absolute path, in a temporary home of the run's own in
// the temp folder.
type fakeRunner struct {
	t        *testing.T
	tmp      string
	requests []update.Request
	answer   func(n int) (update.Result, error)
}

func (f *fakeRunner) Run(ctx context.Context, r update.Request) (update.Result, error) {
	f.requests = append(f.requests, r)
	names, err := os.ReadDir(r.RunDir)
	home := filepath.Dir(r.RunDir)
	if err != nil || len(names) != 0 || !filepath.IsAbs(r.RunDir) || filepath.Dir(home) != f.tmp || !strings.HasPrefix(filepath.Base(home), "eva-md-live-accuracy-") {
		f.t.Errorf("request %d: run folder %s: %v, %d names", len(f.requests), r.RunDir, err, len(names))
	}
	if f.answer == nil {
		return update.Result{Output: []byte(empty), Effort: r.Effort}, nil
	}
	return f.answer(len(f.requests))
}

// noRunner fails a test that reaches a runner.
func noRunner(t *testing.T) func(string) update.Runner {
	return func(host string) update.Runner {
		t.Errorf("a runner for %s was made", host)
		return &fakeRunner{t: t}
	}
}

// leftIn lists what a folder holds.
func leftIn(t *testing.T, dir string) []string {
	t.Helper()
	var names []string
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path != dir {
			rel, _ := filepath.Rel(dir, path)
			names = append(names, rel)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return names
}

// The run is impossible to start by accident: it needs a host named by
// flag, and without --spend-model-quota it says what it would spend and
// stops, with no runner made, no home made and nothing called; so does any
// other usage error, an effort or a model the runner would refuse among
// them.
func TestRefusals(t *testing.T) {
	for _, tc := range []struct {
		args []string
		want string
	}{
		{nil, "live-accuracy: --host must be claude-code or codex; usage: "},
		{[]string{"--spend-model-quota"}, "live-accuracy: --host must be claude-code or codex; usage: "},
		{[]string{"--host", "claude", "--spend-model-quota"}, "live-accuracy: --host must be claude-code or codex; usage: "},
		{[]string{"--host", "codex", "--spend-model-quota", "now"}, "live-accuracy: it takes no arguments, only flags; usage: "},
		{[]string{"--host", "codex", "--spend-model-quota", "--cli", "bin/codex"}, "live-accuracy: --cli must be the absolute path of a command named codex; usage: "},
		{[]string{"--host", "codex", "--spend-model-quota", "--cli", "/opt/bin/claude"}, "live-accuracy: --cli must be the absolute path of a command named codex; usage: "},
		{[]string{"--host", "claude-code", "--spend-model-quota", "--codex-ignore-user-config"}, "live-accuracy: --codex-ignore-user-config is for codex only; usage: "},
		{[]string{"--host", "claude-code", "--spend-model-quota", "--effort", "ultra"}, `live-accuracy: effort "ultra" is not one of low, medium, high, xhigh, max or highest; usage: `},
		{[]string{"--host", "codex", "--effort", "turbo"}, `live-accuracy: effort "turbo" is not one of none, minimal, low, medium, high, xhigh, max, ultra or highest; usage: `},
		{[]string{"--host", "claude-code", "--spend-model-quota", "--model", "--dangerously-skip-permissions"}, "live-accuracy: the model name has characters a CLI flag can't take; usage: "},
		{[]string{"--host", "codex", "--spend-model-quota", "--model", "gpt test"}, "live-accuracy: the model name has characters a CLI flag can't take; usage: "},
		{[]string{"--host", "claude-code", "--spend-model-quota=false"}, "live-accuracy: the run makes 21 model calls on claude-code's subscription, one for each pass of the suite's 12 fact scripts, each allowed up to 10 minutes. Nothing was called; run it again with --spend-model-quota to make them.\n"},
		{[]string{"--host", "codex", "--effort", "high"}, "live-accuracy: the run makes 21 model calls on codex's subscription, one for each pass of the suite's 12 fact scripts, each allowed up to 10 minutes. Nothing was called; run it again with --spend-model-quota to make them.\n"},
		{[]string{"--host", "codex", "--spend-model-quota", "--quota"}, "flag provided but not defined: -quota\n"},
	} {
		t.Run(strings.Join(tc.args, " "), func(t *testing.T) {
			tmp := isolate(t)
			var out, errOut bytes.Buffer
			if code := run(context.Background(), tc.args, &out, &errOut, noRunner(t)); code != 2 || out.Len() != 0 || !strings.HasPrefix(errOut.String(), tc.want) {
				t.Fatalf("exit %d, out %q, err %q", code, out.String(), errOut.String())
			}
			if left := leftIn(t, tmp); len(left) != 0 {
				t.Fatalf("the refused run left %q", left)
			}
		})
	}
}

// fakeCLI writes an executable file named name in a folder of its own,
// outside the temp folder, and returns its path.
func fakeCLI(t *testing.T, name, script string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "bin", name)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	return path
}

// A run asks the runner for every pass of the suite in order, each with the
// update's rules, the batch on stdin, the schema for the page and the flags'
// effort, model and CLI, in an empty run folder of its own in a temporary
// home; it prints each history's metrics, what it missed and its page,
// then the counts against the target and the effort the answers came at,
// and leaves nothing behind. Answers that change nothing miss every fact,
// so the run misses the target.
func TestRun(t *testing.T) {
	tmp := isolate(t)
	codex := fakeCLI(t, "codex", "#!/bin/sh\nexit 1\n")
	runner := &fakeRunner{t: t, tmp: tmp}
	var out, errOut bytes.Buffer
	args := []string{"--host", "codex", "--spend-model-quota", "--effort", "high", "--model", "gpt-test", "--cli", codex, "--codex-ignore-user-config"}
	code := run(context.Background(), args, &out, &errOut, func(host string) update.Runner {
		if host != update.HostCodex {
			t.Errorf("a runner for %s", host)
		}
		return runner
	})
	if code != 1 {
		t.Fatalf("exit %d\n%s\n%s", code, out.String(), errOut.String())
	}
	var dirs, inputs []string
	for i, r := range runner.requests {
		if r.Host != update.HostCodex || r.Effort != "high" || r.Model != "gpt-test" || r.CLI != codex || r.Rules != update.Rules ||
			!bytes.Equal(r.Schema, update.Schema(threadpage.Page{})) || r.Timeout != 0 || !r.IgnoreUserConfig {
			t.Errorf("request %d: %+v", i+1, r)
		}
		dirs, inputs = append(dirs, r.RunDir), append(inputs, string(r.Input))
	}
	if len(runner.requests) != 21 || len(slices.Compact(slices.Sorted(slices.Values(dirs)))) != 21 {
		t.Fatalf("%d requests in %d run folders", len(runner.requests), len(slices.Compact(slices.Sorted(slices.Values(dirs)))))
	}
	if !strings.Contains(inputs[0], "New entries, oldest first:\n[1] user 2026-01-13 10:01: Use flock for the per-session lock, KESTREL-41.\n") {
		t.Errorf("the first batch:\n%s", inputs[0])
	}
	if left := leftIn(t, tmp); len(left) != 0 {
		t.Errorf("the run left %q", left)
	}
	if left := leftIn(t, os.Getenv("HOME")); len(left) != 0 {
		t.Errorf("the run wrote %q in the home folder", left)
	}
	text := out.String()
	var ended []string
	for _, l := range strings.Split(text, "\n") {
		if name, _, ok := strings.Cut(strings.TrimPrefix(l, "- "), ", "); ok && strings.HasPrefix(l, "- ") {
			ended = append(ended, name)
		}
	}
	var names []string
	for _, s := range accuracy.Scripts() {
		names = append(names, strings.SplitN(s.Name, ", ", 2)[0])
	}
	if !slices.Equal(ended, names) {
		t.Errorf("histories %q, want %q", ended, names)
	}
	for _, want := range []string{
		"The accuracy suite's 12 fact scripts on codex, gpt-test, effort high:\n- an assistant's suggestion against the user's decision, 1 pass: recall 0.00, updates 1.00, stale 0.00, tags 1.00, dates 1.00, carried 0, duplicates 0\n" +
			"  content, pass 1: KESTREL-41 is missing\n  content, pass 1: PIDGEON-7 is missing\n  The page after its last pass:\n",
		"- a reversal, 2 passes: recall 0.00, updates 0.00, stale 0.00, tags 1.00, dates 1.00, carried 0, duplicates 0\n" +
			"  content, pass 1: OSPREY-Y is missing\n  content, pass 2: OSPREY-J is missing\n",
		"\nOn codex, against the target in docs/design.md: every enforceable obligation met, no speaker or date error, and at most 2 content obligations missed.\n" +
			"  enforceable obligations missed: 0\n  speaker errors: 0\n  date errors: 0\n  content obligations missed: 26\n  answers refused: 0 of 21\n  answers at effort high: 21 of 21\nThe run misses the target.\n",
		"- the two-session acceptance, 2 passes: recall 0.00, updates 1.00, stale 0.00, tags 1.00, dates 1.00, carried 0, duplicates 0\n" +
			"  content, pass 1: DOTTEREL-D is missing\n  content, pass 1: DOTTEREL-W is missing\n  content, pass 2: DOTTEREL-W is missing\n",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("the report lacks %q:\n%s", want, text)
		}
	}
	if n := strings.Count(errOut.String(), ": asking codex\n"); n != 21 || strings.Count(errOut.String(), "live-accuracy: answered in ") != 21 ||
		!strings.Contains(errOut.String(), "live-accuracy: a reversal, pass 2 of 2: asking codex\n") {
		t.Errorf("progress:\n%s", errOut.String())
	}
}

// Each call is the update job's: the batch's input, the update's rules and
// the schema for the page the batch was built for, in a new empty run
// folder in the run's home that is gone once the call returns. The effort
// its answer came at is counted.
func TestAsk(t *testing.T) {
	tmp := isolate(t)
	home, err := os.MkdirTemp(tmp, "eva-md-live-accuracy-")
	if err != nil {
		t.Fatal(err)
	}
	runner := &fakeRunner{t: t, tmp: tmp}
	c := caller{host: update.HostClaude, effort: "max", model: "claude-test", cli: "/opt/claude/bin/claude", home: home, runner: runner, log: &bytes.Buffer{}, efforts: map[string]int{}}
	page := threadpage.Page{Now: threadpage.Now{Next: []threadpage.Item{{ID: "n1", Text: "Write the parser", Tag: "stated"}}}}
	b := update.Batch{Input: []byte("New entries, oldest first:\n[1] user 2026-01-14 10:01: Write the renderer next.\n")}
	if _, err := c.ask(context.Background(), accuracy.Reversal(), 2, b, page); err != nil {
		t.Fatal(err)
	}
	r := runner.requests[0]
	if len(runner.requests) != 1 || r.Host != update.HostClaude || r.Effort != "max" || r.Model != "claude-test" || r.CLI != "/opt/claude/bin/claude" || r.IgnoreUserConfig ||
		r.Rules != update.Rules || !bytes.Equal(r.Input, b.Input) || !bytes.Equal(r.Schema, update.Schema(page)) || bytes.Equal(r.Schema, update.Schema(threadpage.Page{})) {
		t.Fatalf("requests %+v", runner.requests)
	}
	if len(c.efforts) != 1 || c.efforts["max"] != 1 {
		t.Fatalf("efforts %v", c.efforts)
	}
	if left := leftIn(t, home); len(left) != 0 {
		t.Fatalf("the call left %q", left)
	}
}

// A --cli that names no executable file is refused as a usage error before
// anything is made: the runner would quietly start the CLI on PATH in its
// place, which here would answer every pass.
func TestABadCLIIsRefused(t *testing.T) {
	for _, kind := range []string{"gone", "a folder", "not executable"} {
		t.Run(kind, func(t *testing.T) {
			tmp := isolate(t)
			onPath := fakeCLI(t, "claude", "#!/bin/sh\ncat > /dev/null\necho ran >> \"$HOME/ran\"\ncat <<'EOF'\n"+envelope+"\nEOF\n")
			t.Setenv("PATH", filepath.Dir(onPath)+":/usr/bin:/bin")
			cli := filepath.Join(t.TempDir(), "claude")
			switch kind {
			case "a folder":
				if err := os.Mkdir(cli, 0o700); err != nil {
					t.Fatal(err)
				}
			case "not executable":
				if err := os.WriteFile(cli, []byte("#!/bin/sh\n"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			made := 0
			var out, errOut bytes.Buffer
			code := run(context.Background(), []string{"--host", "claude-code", "--spend-model-quota", "--cli", cli}, &out, &errOut, func(host string) update.Runner {
				made++
				return update.NewRunner(host)
			})
			want := "live-accuracy: the CLI path " + cli + " is not the absolute path of an executable file named claude; usage: "
			if code != 2 || made != 0 || out.Len() != 0 || !strings.HasPrefix(errOut.String(), want) {
				t.Fatalf("exit %d, %d runners made, out %q, err %q", code, made, out.String(), errOut.String())
			}
			if left := leftIn(t, tmp); len(left) != 0 {
				t.Fatalf("the refused run left %q", left)
			}
			if left := leftIn(t, os.Getenv("HOME")); len(left) != 0 {
				t.Fatalf("the claude on PATH ran: the home folder holds %q", left)
			}
		})
	}
}

// A model the host rejects stops the run at its first answer: the runner
// falls back to the host's default model, whose answers the report would
// credit to the model asked for. Here a fake claude rejects any --model, so
// the first pass runs it three times, the last with no model flag, and the
// run stops there, judging nothing, whether the default model's answer is
// one the update takes or one the runner refuses, whose reason the stop
// names.
func TestARejectedModelStopsTheRun(t *testing.T) {
	refused := `{"type":"result","subtype":"error_max_structured_output_retries","is_error":false,"usage":{"input_tokens":120,"output_tokens":40},"permission_denials":[]}`
	for _, tc := range []struct{ name, answer, stop string }{
		{"an answer", envelope, "claude-code rejected the model claude-test-x and answered with its default model"},
		{"a refused answer", refused, "claude-code rejected the model claude-test-x, and its default model's run then failed: invalid_output"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tmp := isolate(t)
			home := os.Getenv("HOME")
			rejected := `{"type":"result","subtype":"error_during_execution","is_error":true,"result":"model not found","api_error_status":404,"permission_denials":[]}`
			claude := fakeCLI(t, "claude", "#!/bin/sh\ncat > /dev/null\nfor a in \"$@\"; do\n  if [ \"$a\" = --model ]; then\n    echo model >> \"$HOME/runs\"\n    cat <<'EOF'\n"+rejected+
				"\nEOF\n    exit 1\n  fi\ndone\necho default >> \"$HOME/runs\"\ncat <<'EOF'\n"+tc.answer+"\nEOF\n")
			var out, errOut bytes.Buffer
			code := run(context.Background(), []string{"--host", "claude-code", "--spend-model-quota", "--model", "claude-test-x", "--cli", claude}, &out, &errOut, update.NewRunner)
			runs, err := os.ReadFile(filepath.Join(home, "runs"))
			if err != nil {
				t.Fatal(err)
			}
			if code != 1 || string(runs) != "model\nmodel\ndefault\n" || strings.Contains(out.String(), "target") ||
				!strings.HasSuffix(errOut.String(), "live-accuracy: an assistant's suggestion against the user's decision, pass 1: "+tc.stop+"; the run stopped before it could be judged\n") {
				t.Fatalf("exit %d after runs %q\n%s\n%s", code, runs, out.String(), errOut.String())
			}
			if left := leftIn(t, tmp); len(left) != 0 {
				t.Fatalf("the run left %q", left)
			}
		})
	}
}

// A host failure that says nothing of the answers stops the run where it
// happens, judging nothing; so does a request the runner can't run. Either
// way the run's home is removed.
func TestAFailureStopsTheRun(t *testing.T) {
	for _, tc := range []struct {
		name   string
		answer func(n int) (update.Result, error)
		calls  int
		want   string
	}{
		{"auth", func(n int) (update.Result, error) {
			if n == 3 {
				return update.Result{Reason: update.ReasonAuth}, nil
			}
			return update.Result{Output: []byte(empty)}, nil
		}, 3, "live-accuracy: a reversal, pass 1: auth; the run stopped before it could be judged\n"},
		{"an effort the host lacks", func(int) (update.Result, error) {
			return update.Result{}, errors.New(`update: effort "turbo" is not one of low, high or highest`)
		}, 1, "live-accuracy: the run stopped: update: effort \"turbo\" is not one of low, high or highest\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tmp := isolate(t)
			runner := &fakeRunner{t: t, tmp: tmp, answer: tc.answer}
			var out, errOut bytes.Buffer
			code := run(context.Background(), []string{"--host", "claude-code", "--spend-model-quota"}, &out, &errOut, func(string) update.Runner { return runner })
			if code != 1 || len(runner.requests) != tc.calls || !strings.HasSuffix(errOut.String(), tc.want) || strings.Contains(out.String(), "target") {
				t.Fatalf("exit %d after %d calls\n%s\n%s", code, len(runner.requests), out.String(), errOut.String())
			}
			if left := leftIn(t, tmp); len(left) != 0 {
				t.Fatalf("the run left %q", left)
			}
		})
	}
}

// A history that missed nothing prints its metrics alone; one with a
// refused answer prints it once, as the tally counts it, with what it
// missed and its page. A run whose tally meets the target says so and
// exits 0, after the efforts its answers came at, most first.
func TestReport(t *testing.T) {
	isolate(t)
	s := accuracy.Reversal()
	var out bytes.Buffer
	history(&out, live.History{Script: s, Metrics: accuracy.Metrics{Recall: 1, Updates: 1, Tags: 1, Dates: 1}})
	if out.String() != "- a reversal, 2 passes: recall 1.00, updates 1.00, stale 0.00, tags 1.00, dates 1.00, carried 0, duplicates 0\n" {
		t.Fatalf("%q", out.String())
	}
	out.Reset()
	page := threadpage.Page{Now: threadpage.Now{Decisions: []threadpage.Item{{ID: "n1", Text: "Store pages as YAML, OSPREY-Y", Tag: "stated"}}}}
	history(&out, live.History{Script: s, Metrics: accuracy.Metrics{Updates: 1, Tags: 1, Dates: 1}, Page: page, Refused: []string{"pass 2: tool_used"},
		Missed: []string{"content, pass 2: the answer was refused: tool_used", "content, pass 2: OSPREY-Y is still in force", "content, pass 2: OSPREY-J is missing"}})
	if text := out.String(); !strings.HasPrefix(text, "- a reversal, 2 passes: recall 0.00, updates 1.00, stale 0.00, tags 1.00, dates 1.00, carried 0, duplicates 0\n"+
		"  content, pass 2: the answer was refused: tool_used\n  content, pass 2: OSPREY-Y is still in force\n  content, pass 2: OSPREY-J is missing\n  The page after its last pass:\n") ||
		strings.Count(text, "tool_used") != 1 || !strings.Contains(text, "\n    - Store pages as YAML, OSPREY-Y [stated]") {
		t.Fatalf("%q", text)
	}
	out.Reset()
	var res live.Result
	res.Passes = 21
	res.Tally.Measure(s.Name, 2, accuracy.Metrics{Misses: []accuracy.Miss{{Kind: accuracy.KindContent, Code: "OSPREY-Y", What: "OSPREY-Y is still in force"}}})
	if code := summary(&out, update.HostClaude, res, map[string]int{"high": 1, "max": 18, "": 1, "xhigh": 1}); code != 0 || !strings.HasSuffix(out.String(),
		"  content obligations missed: 1\n  answers refused: 0 of 21\n  answers at effort max: 18 of 21\n  answers with no effort flag: 1 of 21\n"+
			"  answers at effort high: 1 of 21\n  answers at effort xhigh: 1 of 21\nThe run meets the target.\n") {
		t.Fatalf("exit %d: %q", code, out.String())
	}
}

// The real runner runs the host CLI the flags name, in each call's run
// folder, with the batch on stdin; here a fake claude records where it ran
// and answers with nothing to change. The run's home, run folders and all,
// is gone at the end, and nothing is written in the home folder but the
// fake's own record.
func TestTheRealRunner(t *testing.T) {
	tmp := isolate(t)
	home := os.Getenv("HOME")
	dump := filepath.Join(home, "fake")
	bin := filepath.Join(t.TempDir(), "bin")
	claude := filepath.Join(bin, "claude")
	for path, data := range map[string]string{
		filepath.Join(dump, "stdout"): envelope,
		claude:                        "#!/bin/sh\npwd -P >> \"$HOME/fake/runs\"\ncat > /dev/null\ncat \"$HOME/fake/stdout\"\n",
	} {
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(data), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	var out, errOut bytes.Buffer
	if code := run(context.Background(), []string{"--host", "claude-code", "--spend-model-quota", "--cli", claude}, &out, &errOut, update.NewRunner); code != 1 ||
		!strings.Contains(out.String(), "  answers refused: 0 of 21\n  answers at effort max: 21 of 21\nThe run misses the target.\n") {
		t.Fatalf("exit %d\n%s\n%s", code, out.String(), errOut.String())
	}
	data, err := os.ReadFile(filepath.Join(dump, "runs"))
	if err != nil {
		t.Fatal(err)
	}
	real, err := filepath.EvalSymlinks(tmp)
	if err != nil {
		t.Fatal(err)
	}
	runs := strings.Fields(string(data))
	for _, dir := range runs {
		if base := filepath.Base(filepath.Dir(dir)); filepath.Dir(filepath.Dir(dir)) != real || !strings.HasPrefix(base, "eva-md-live-accuracy-") || !strings.HasPrefix(filepath.Base(dir), "run-") {
			t.Errorf("a run in %s", dir)
		}
	}
	if len(runs) != 21 || strings.Count(errOut.String(), "live-accuracy: answered in ") != 21 {
		t.Fatalf("%d runs\n%s", len(runs), errOut.String())
	}
	if left := leftIn(t, tmp); len(left) != 0 {
		t.Errorf("the run left %q", left)
	}
	if left := leftIn(t, home); !slices.Equal(left, []string{"fake", "fake/runs", "fake/stdout"}) {
		t.Errorf("the home folder holds %q", left)
	}
}

// No release or install step builds the command: every go build, install
// or run in the workflows and scripts builds ./cmd/eva-md alone, and none
// names this command.
func TestNoReleaseBuildsIt(t *testing.T) {
	root := filepath.Join("..", "..")
	files, err := filepath.Glob(filepath.Join(root, ".github", "workflows", "*.y*ml"))
	if err != nil {
		t.Fatal(err)
	}
	scripts, err := filepath.Glob(filepath.Join(root, "scripts", "*"))
	if err != nil {
		t.Fatal(err)
	}
	if len(files) < 3 || len(scripts) < 3 {
		t.Fatalf("found %v and %v; the check read the wrong folders", files, scripts)
	}
	// A command runs to the end of its line, or to a ;, & or |; a line a
	// backslash continues is one line.
	gocmd := regexp.MustCompile(`\bgo +(build|install|run)\b([^\n;&|]*)`)
	builds := 0
	for _, path := range append(files, scripts...) {
		if info, err := os.Stat(path); err != nil || info.IsDir() {
			continue
		}
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		text := strings.ReplaceAll(string(data), "\\\n", " ")
		if strings.Contains(text, "live-accuracy") {
			t.Errorf("%s names live-accuracy", path)
		}
		for _, m := range gocmd.FindAllStringSubmatch(text, -1) {
			var pkgs []string
			for _, f := range strings.Fields(m[2]) {
				if f = strings.Trim(f, `"'`); strings.HasPrefix(f, "./") || strings.Contains(f, "...") {
					pkgs = append(pkgs, f)
				}
			}
			if !slices.Equal(pkgs, []string{"./cmd/eva-md"}) {
				t.Errorf("%s: go %s%s builds %q", path, m[1], m[2], pkgs)
			}
			builds++
		}
	}
	if builds < 3 {
		t.Fatalf("found %d builds; the check read the wrong files", builds)
	}
}

// Only this command, and the live run it calls, import the suite, so the
// scripts never ship in eva-md: no other Go file but a test imports it.
func TestOnlyItImportsTheSuite(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	suite := map[string]bool{"github.com/heyNag/eva-md/internal/update/accuracy": true, "github.com/heyNag/eva-md/internal/update/accuracy/live": true}
	allowed := map[string]bool{"cmd/live-accuracy/main.go": true, "internal/update/accuracy/live/live.go": true}
	var importers []string
	for _, dir := range []string{"cmd", "internal"} {
		err := filepath.WalkDir(filepath.Join(root, dir), func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return err
			}
			f, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
			if err != nil {
				return err
			}
			for _, imp := range f.Imports {
				if p, _ := strconv.Unquote(imp.Path.Value); suite[p] {
					rel, _ := filepath.Rel(root, path)
					importers = append(importers, filepath.ToSlash(rel))
				}
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	slices.Sort(importers)
	importers = slices.Compact(importers)
	if !slices.Equal(importers, []string{"cmd/live-accuracy/main.go", "internal/update/accuracy/live/live.go"}) {
		t.Fatalf("the suite is imported by %q; only %v may", importers, allowed)
	}
}
