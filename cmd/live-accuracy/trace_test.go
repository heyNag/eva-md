package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"runtime/debug"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/heyNag/eva-md/internal/testlimits"
	"github.com/heyNag/eva-md/internal/threadpage"
	"github.com/heyNag/eva-md/internal/update"
	"github.com/heyNag/eva-md/internal/update/accuracy"
)

// The read of a CLI's version for run.json gets the long limits the other
// tests' host commands get, before any test runs: TestMain is main_test.go's,
// which raises the update's.
func init() {
	versionTimeout, versionWaitDelay = testlimits.Long, testlimits.Long
}

// costed is answer with what a runner reports beside an answer: each call
// n made 1 to 3 attempts, used 100n tokens in and n out, and names the
// model that answered, as a runner's Result does.
func costed(answer func(n int) (update.Result, error)) func(n int) (update.Result, error) {
	return func(n int) (update.Result, error) {
		res, err := answer(n)
		if err == nil {
			res.Attempts, res.TokensIn, res.TokensOut, res.ModelReported = 1+n%3, 100*n, n, "gpt-test-reported"
		}
		return res, err
	}
}

// tapRunner is fakeRunner, which also hands its tap made-up output for
// each attempt its Result reports, as the real runner does. At the call
// interrupt, it gives the tap one attempt, ends the run's context and
// fails as the real runner does when its context ends.
// Before its taps, call slow lasts at least hold, so its line's ms is at
// least hold whatever the machine's speed, and beforeTap, when set, is
// called with the call's number.
type tapRunner struct {
	*fakeRunner
	interrupt int
	cancel    func()
	slow      int
	hold      time.Duration
	beforeTap func(n int)
}

func (f tapRunner) Run(ctx context.Context, r update.Request) (update.Result, error) {
	res, err := f.fakeRunner.Run(ctx, r)
	n := len(f.requests)
	if n == f.slow {
		time.Sleep(f.hold)
	}
	if f.beforeTap != nil {
		f.beforeTap(n)
	}
	if n == f.interrupt {
		r.Tap(rawOut(n, 1), rawErr(n, 1))
		f.cancel()
		return update.Result{}, fmt.Errorf("update: run canceled: %w", ctx.Err())
	}
	for a := 1; a <= res.Attempts && r.Tap != nil; a++ {
		r.Tap(rawOut(n, a), rawErr(n, a))
	}
	return res, err
}

// rawOut and rawErr are what a fake CLI printed on attempt a of call n.
func rawOut(n, a int) []byte {
	return []byte(fmt.Sprintf(`{"type":"made-up","call":%d,"attempt":%d}`+"\n", n, a))
}

func rawErr(n, a int) []byte {
	return []byte(fmt.Sprintf("made-up notice %d.%d\x00 with no newline", n, a))
}

// passKeys and the keys of run.json and end.json, in the format's order.
var (
	runKeys   = []string{"v", "host", "effort", "model", "cli_version", "eva_md", "timeout_refused", "day"}
	passKeys  = []string{"script", "pass", "ms", "tokens_in", "tokens_out", "attempts", "effort_used", "model_reported", "model_note", "degraded", "reason", "refused"}
	endKeys   = []string{"outcome", "passes", "refused", "timeouts", "stopped"}
	buildKeys = []string{"commit", "dirty"}
)

// decodeLine decodes one JSON line of a trace into v, which must hold
// exactly keys and write back to the same bytes, so in the format's order.
func decodeLine(t *testing.T, line []byte, keys []string, v any) {
	t.Helper()
	got, err := objectKeys(bytes.TrimSuffix(line, []byte("\n")))
	if err != nil {
		t.Fatalf("%q: %v", line, err)
	}
	if !sameKeys(got, keys) {
		t.Fatalf("%q has keys other than %q", line, keys)
	}
	d := json.NewDecoder(bytes.NewReader(line))
	d.DisallowUnknownFields()
	if err := d.Decode(v); err != nil {
		t.Fatalf("%q: %v", line, err)
	}
	again, err := encodeLine(v)
	if err != nil || !bytes.Equal(again, line) {
		t.Fatalf("%q is not as the format writes it: %q", line, again)
	}
}

func sameKeys(got map[string]json.RawMessage, keys []string) bool {
	if len(got) != len(keys) {
		return false
	}
	for _, k := range keys {
		if _, ok := got[k]; !ok {
			return false
		}
	}
	return true
}

// readTrace reads a trace folder: run.json, the lines of passes.jsonl,
// end.json, and the raw files by name, failing on anything else there.
func readTrace(t *testing.T, dir string) (traceRun, []tracePass, traceEnd, map[string][]byte) {
	t.Helper()
	var run traceRun
	var passes []tracePass
	var end traceEnd
	raw := map[string][]byte{}
	for _, name := range leftIn(t, dir) {
		if name == "raw" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		switch {
		case name == "run.json":
			decodeLine(t, data, runKeys, &run)
			var b map[string]json.RawMessage
			_ = json.Unmarshal(data, &b)
			if keys, err := objectKeys(b["eva_md"]); err != nil || !sameKeys(keys, buildKeys) {
				t.Fatalf("eva_md is %s", b["eva_md"])
			}
		case name == "end.json":
			decodeLine(t, data, endKeys, &end)
		case name == "passes.jsonl":
			if len(data) == 0 {
				break
			}
			for _, l := range bytes.SplitAfter(bytes.TrimSuffix(data, []byte("\n")), []byte("\n")) {
				if !bytes.HasSuffix(l, []byte("\n")) {
					l = append(l, '\n')
				}
				var p tracePass
				decodeLine(t, l, passKeys, &p)
				passes = append(passes, p)
			}
			if !bytes.HasSuffix(data, []byte("\n")) {
				t.Fatalf("passes.jsonl ends part way through a line")
			}
		case strings.HasPrefix(name, "raw"+string(filepath.Separator)):
			raw[filepath.Base(name)] = data
		default:
			t.Fatalf("the trace holds %s", name)
		}
	}
	return run, passes, end, raw
}

// timeOfDay is a clock time, as a timestamp would hold one.
var timeOfDay = regexp.MustCompile(`\d\d:\d\d`)

// noMachine fails the test when a JSON file of the trace holds a path, a
// folder the test made, a time of day or the machine's system or
// architecture.
func noMachine(t *testing.T, dir string, made ...string) {
	t.Helper()
	for _, name := range []string{"run.json", "passes.jsonl", "end.json"} {
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		for _, leak := range append(made, "/", runtime.GOOS, runtime.GOARCH) {
			if bytes.Contains(data, []byte(leak)) {
				t.Errorf("%s holds %q:\n%s", name, leak, data)
			}
		}
		if timeOfDay.Match(data) {
			t.Errorf("%s holds a time of day:\n%s", name, data)
		}
	}
}

// refusedIn reads, from the report, each answer the run counts refused,
// by its script's place in the suite and its pass, with why.
func refusedIn(t *testing.T, report string) map[passKey]string {
	t.Helper()
	scripts := accuracy.Scripts()
	refused, script := map[passKey]string{}, 0
	for _, l := range strings.Split(report, "\n") {
		if script < len(scripts) && strings.HasPrefix(l, "- "+scripts[script].Name+", ") {
			script++
			continue
		}
		var pass int
		var why string
		if rest, ok := strings.CutPrefix(l, "  content, pass "); ok && strings.Contains(rest, ": the answer was refused: ") {
			n, after, _ := strings.Cut(rest, ": the answer was refused: ")
			if _, err := fmt.Sscan(n, &pass); err != nil {
				t.Fatalf("%q: %v", l, err)
			}
			why = after
			refused[passKey{script, pass}] = why
		}
	}
	return refused
}

// nth is the suite's nth pass, from 1.
func nth(n int) passKey {
	for i, s := range accuracy.Scripts() {
		for p := range s.Passes {
			if n--; n == 0 {
				return passKey{i + 1, p + 1}
			}
		}
	}
	return passKey{}
}

// sinceDays are the UTC dates from before to now, which a run's day falls
// in.
func sinceDays(before time.Time) []string {
	return []string{before.UTC().Format(time.DateOnly), time.Now().UTC().Format(time.DateOnly)}
}

// clockFree is stderr with each time a call took written as N.
var clockFree = regexp.MustCompile(`\d+\.\d s\b`)

// A run with --trace writes run.json, a line for each of the 21 passes,
// in order, with what the runner reported, the raw output of every
// attempt byte for byte and end.json, and nothing of the machine. An
// answer the runner refuses has its reason in refused; one the validator
// refuses has the validator's error there and an empty reason, as the
// report counts it. Given with --record and --pages, each writes what it
// writes alone, and the report and the progress are the run's without the
// trace.
func TestTraceOfARun(t *testing.T) {
	tmp := isolate(t)
	codex := fakeCLI(t, "codex", "#!/bin/sh\nif [ \"$1\" = --version ]; then echo 'codex-cli 0.166.0'; exit 0; fi\nexit 1\n")
	type ran struct {
		out, err string
		code     int
	}
	const slow, hold = 7, 30 * time.Millisecond
	runWith := func(t *testing.T, extra ...string) ran {
		t.Helper()
		runner := tapRunner{fakeRunner: &fakeRunner{t: t, tmp: tmp, answer: costed(mixed)}, slow: slow, hold: hold}
		var out, errOut bytes.Buffer
		args := append([]string{"--host", "codex", "--effort", "high", "--model", "gpt-test", "--spend-model-quota", "--cli", codex}, extra...)
		code := run(context.Background(), args, &out, &errOut, func(string) update.Runner { return runner })
		if len(runner.requests) != 21 {
			t.Fatalf("%d calls\n%s\n%s", len(runner.requests), out.String(), errOut.String())
		}
		return ran{out.String(), clockFree.ReplaceAllString(errOut.String(), "N s"), code}
	}
	dir := t.TempDir()
	trace, alone := filepath.Join(dir, "trace"), filepath.Join(dir, "alone")
	// A folder that is there and empty, open to others, is made private.
	must(t, os.Mkdir(alone, 0o755))
	must(t, os.Chmod(alone, 0o755))
	before := time.Now()
	with := runWith(t, "--trace", trace, "--record", filepath.Join(dir, "answers.jsonl"), "--pages", filepath.Join(dir, "pages"))
	wall := time.Since(before)
	plain := runWith(t)
	if with != plain || with.code != 1 {
		t.Fatalf("with the trace:\n%+v\nwithout:\n%+v", with, plain)
	}

	run, passes, end, raw := readTrace(t, trace)
	if run.V != 1 || run.Host != "codex" || run.Effort != "high" || run.Model != "gpt-test" || run.CLIVersion != "0.166.0" || run.EvaMD != buildOf() ||
		run.TimeoutRefused || !slices.Contains(sinceDays(before), run.Day) {
		t.Errorf("run.json %+v", run)
	}
	if len(passes) != 21 {
		t.Fatalf("%d lines", len(passes))
	}
	refused := refusedIn(t, with.out)
	if len(refused) != 9 {
		t.Fatalf("the report counts %d refused: %v\n%s", len(refused), refused, with.out)
	}
	n, attempts, validator := 0, 0, 0
	for i, s := range accuracy.Scripts() {
		for p := range s.Passes {
			l := passes[n]
			n++
			want, _ := costed(mixed)(n)
			// A call's ms lies within the run's wall time, and the call that
			// lasts at least hold says so.
			if l.MS < 0 || l.MS > wall.Milliseconds() || n == slow && l.MS < hold.Milliseconds() {
				t.Errorf("line %d: ms %d, the run took %d ms", n, l.MS, wall.Milliseconds())
			}
			if l.Script != i+1 || l.Pass != p+1 || l.TokensIn != 100*n || l.TokensOut != n || l.Attempts != want.Attempts || l.EffortUsed != want.Effort ||
				l.ModelReported != "gpt-test-reported" || l.ModelNote != "" || l.Degraded != want.Degraded || l.Reason != want.Reason {
				t.Errorf("line %d: %+v", n, l)
			}
			if why := refused[passKey{i + 1, p + 1}]; l.Refused != why {
				t.Errorf("line %d: refused %q, the report's %q", n, l.Refused, why)
			}
			if l.Reason == "" && l.Refused != "" {
				validator++
				if !strings.HasPrefix(l.Refused, "invalid_output: update: output rejected: ") {
					t.Errorf("line %d: refused %q", n, l.Refused)
				}
			}
			for a := 1; a <= l.Attempts; a++ {
				name := fmt.Sprintf("%02d-%02d-%02d", i+1, p+1, a)
				if !bytes.Equal(raw[name+".out"], rawOut(n, a)) || !bytes.Equal(raw[name+".err"], rawErr(n, a)) {
					t.Errorf("%s holds %q and %q", name, raw[name+".out"], raw[name+".err"])
				}
				attempts++
			}
		}
	}
	if validator == 0 || len(raw) != 2*attempts {
		t.Errorf("%d answers the validator refused, %d raw files for %d attempts", validator, len(raw), attempts)
	}
	if end != (traceEnd{Outcome: "missed", Passes: 21, Refused: 9}) {
		t.Errorf("end.json %+v", end)
	}
	noMachine(t, trace, tmp, os.Getenv("HOME"), dir, filepath.Dir(codex))

	// Each of the three writes alone what it writes beside the others: the
	// trace's lines but for each call's time, the recording and the pages
	// byte for byte.
	alone1 := runWith(t, "--trace", alone)
	alone2 := runWith(t, "--record", filepath.Join(dir, "answers-alone.jsonl"), "--pages", filepath.Join(dir, "pages-alone"))
	if alone1 != plain || alone2 != plain {
		t.Errorf("a run with one of the flags printed another report or progress")
	}
	private(t, trace)
	private(t, alone)
	run2, passes2, end2, raw2 := readTrace(t, alone)
	for i := range passes2 {
		passes2[i].MS = passes[i].MS
	}
	if run2 != run || !slices.Equal(passes2, passes) || end2 != end || len(raw2) != len(raw) {
		t.Errorf("the trace written alone differs")
	}
	for name, data := range raw {
		if !bytes.Equal(raw2[name], data) {
			t.Errorf("%s differs written alone", name)
		}
	}
	rec, rec2 := readFile(t, filepath.Join(dir, "answers.jsonl")), readFile(t, filepath.Join(dir, "answers-alone.jsonl"))
	if rec != rec2 || !mapsEqual(pagesIn(t, filepath.Join(dir, "pages")), pagesIn(t, filepath.Join(dir, "pages-alone"))) {
		t.Errorf("the recording or the pages differ written beside the trace")
	}
	if left := leftIn(t, tmp); len(left) != 0 {
		t.Errorf("the runs left %q", left)
	}
}

// private fails the test unless the trace folder, its raw folder and
// every file in them are private to their owner: 0700 and 0600.
func private(t *testing.T, dir string) {
	t.Helper()
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		want := fs.FileMode(0o600)
		if d.IsDir() {
			want = 0o700
		}
		if got := info.Mode().Perm(); got != want {
			t.Errorf("%s has mode %v, want %v", path, got, want)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// A run that stops keeps the trace of every pass up to and with the one
// that stopped it, raw output included, and ends it with end.json, which
// says why: a host failure's reason, model_fallback for an answer from the
// host's default model, whose line is written, an interrupt, or an error
// the terminal names, such as a raw file that cannot be written. The
// terminal says what it said without a trace.
func TestTraceOfAStoppedRun(t *testing.T) {
	answered := update.Result{Output: []byte(empty), Effort: "max"}
	for _, tc := range []struct {
		name      string
		answer    func(n int) (update.Result, error)
		interrupt int
		breakRaw  int // the call before whose taps the raw folder is replaced by a file
		lines     int
		stopped   string
		stderr    string // how the terminal's last line ends
	}{
		{"a rate limit at pass 5", func(n int) (update.Result, error) {
			if n == 5 {
				return update.Result{Reason: update.ReasonRateLimit}, nil
			}
			return answered, nil
		}, 0, 0, 5, update.ReasonRateLimit, fmt.Sprintf(", pass %d: rate_limit; the run stopped before it could be judged\n", nth(5).pass)},
		{"an answer from the default model at pass 3", func(n int) (update.Result, error) {
			if n == 3 {
				res := answered
				res.ModelNote = update.NoteFallback + "claude-test"
				return res, nil
			}
			return answered, nil
		}, 0, 0, 3, "model_fallback", fmt.Sprintf(", pass %d: claude-code rejected the model claude-test and answered with its default model; the run stopped before it could be judged\n", nth(3).pass)},
		{"an interrupt at pass 3", func(n int) (update.Result, error) {
			return answered, nil
		}, 3, 0, 2, "interrupted", ": context canceled\n"},
		{"a request that cannot run at pass 4", func(n int) (update.Result, error) {
			if n == 4 {
				return update.Result{}, errors.New("update: the batch is empty")
			}
			return answered, nil
		}, 0, 0, 3, "error", ": update: the batch is empty\n"},
		{"a raw file that cannot be written at pass 4", func(n int) (update.Result, error) {
			return answered, nil
		}, 0, 4, 3, "error", ": not a directory\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tmp := isolate(t)
			// The fake runner runs nothing, and no CLI of the machine's runs
			// for the version.
			t.Setenv("PATH", t.TempDir())
			trace := filepath.Join(t.TempDir(), "trace")
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			runner := tapRunner{fakeRunner: &fakeRunner{t: t, tmp: tmp, answer: costed(tc.answer)}, interrupt: tc.interrupt, cancel: cancel}
			if tc.breakRaw != 0 {
				runner.beforeTap = func(n int) {
					if n == tc.breakRaw {
						must(t, os.RemoveAll(filepath.Join(trace, "raw")))
						must(t, os.WriteFile(filepath.Join(trace, "raw"), nil, 0o600))
					}
				}
			}
			var out, errOut bytes.Buffer
			code := run(ctx, []string{"--host", "claude-code", "--spend-model-quota", "--trace", trace}, &out, &errOut, func(string) update.Runner { return runner })
			if code != 1 || strings.Contains(out.String(), "target") || !strings.HasSuffix(errOut.String(), tc.stderr) {
				t.Fatalf("exit %d\n%s\n%s", code, out.String(), errOut.String())
			}
			if tc.breakRaw != 0 && !strings.Contains(errOut.String(), "live-accuracy: the run stopped: could not write the trace: ") {
				t.Errorf("stderr %q", errOut.String())
			}
			_, passes, end, raw := readTrace(t, trace)
			if len(passes) != tc.lines || end != (traceEnd{Outcome: "stopped", Passes: tc.lines, Stopped: tc.stopped}) {
				t.Fatalf("lines %+v, end %+v", passes, end)
			}
			last := passes[len(passes)-1]
			if k := nth(5); tc.stopped == update.ReasonRateLimit && (last.Reason != update.ReasonRateLimit || last.Refused != "" || last.Script != k.script || last.Pass != k.pass) {
				t.Errorf("the fifth line %+v", last)
			}
			if tc.stopped == "model_fallback" && (last.ModelNote != update.NoteFallback+"claude-test" || last.Reason != "" || last.Refused != "") {
				t.Errorf("the third line %+v", last)
			}
			want := 0
			for _, p := range passes {
				want += 2 * p.Attempts
			}
			switch {
			case tc.interrupt != 0:
				want += 2 // the interrupted call's one attempt
			case tc.breakRaw != 0:
				want = 0 // the raw folder is a file now
			}
			if len(raw) != want {
				t.Errorf("%d raw files, want %d: %v", len(raw), want, slices.Sorted(func(yield func(string) bool) {
					for k := range raw {
						if !yield(k) {
							return
						}
					}
				}))
			}
			noMachine(t, trace, tmp, os.Getenv("HOME"))
		})
	}
}

// A run whose fifth pass times out: with --timeout-refused it counts that
// pass refused and goes on to the 21st, saying how many answers timed
// out, and the trace and the recording keep the timeout; a replay of that
// recording with the flag prints the same report and writes the same
// pages. Without the flag, the run, and a replay of the recording, stop at
// the fifth pass as before.
func TestTimeoutRefused(t *testing.T) {
	tmp := isolate(t)
	// The fake runner runs nothing, and no CLI of the machine's runs for the
	// version.
	t.Setenv("PATH", t.TempDir())
	timedOut := costed(func(n int) (update.Result, error) {
		if n == 5 {
			return update.Result{Reason: update.ReasonTimeout}, nil
		}
		return update.Result{Output: []byte(empty), Effort: "max"}, nil
	})
	dir := t.TempDir()
	trace, recording, pages := filepath.Join(dir, "trace"), filepath.Join(dir, "answers.jsonl"), filepath.Join(dir, "pages")
	runner := tapRunner{fakeRunner: &fakeRunner{t: t, tmp: tmp, answer: timedOut}}
	var out, errOut bytes.Buffer
	args := []string{"--host", "claude-code", "--spend-model-quota", "--timeout-refused", "--trace", trace, "--record", recording, "--pages", pages}
	code := run(context.Background(), args, &out, &errOut, func(string) update.Runner { return runner })
	report := out.String()
	if code != 1 || len(runner.requests) != 21 || !strings.Contains(report, "  answers refused: 1 of 21\n  answers timed out: 1\n  answers at effort max: 20 of 21\nThe run misses the target.\n") ||
		!strings.Contains(report, fmt.Sprintf("content, pass %d: the answer was refused: invalid_output\n", nth(5).pass)) || strings.Count(errOut.String(), "live-accuracy: timeout after ") != 1 {
		t.Fatalf("exit %d after %d calls\n%s\n%s", code, len(runner.requests), report, errOut.String())
	}
	run1, passes, end, _ := readTrace(t, trace)
	if !run1.TimeoutRefused || len(passes) != 21 || passes[4].Reason != update.ReasonTimeout || passes[4].Refused != update.ReasonTimeout ||
		end != (traceEnd{Outcome: "missed", Passes: 21, Refused: 1, Timeouts: 1}) {
		t.Fatalf("run %+v, fifth line %+v, end %+v", run1, passes[4], end)
	}
	if lines := readLines(t, recording); len(lines) != 21 || lines[4].Reason != update.ReasonTimeout {
		t.Fatalf("the recording's fifth line %+v", lines[4])
	}

	replayPages := filepath.Join(dir, "replay")
	var again, againErr bytes.Buffer
	if code := run(context.Background(), []string{"--host", "claude-code", "--effort", "highest", "--replay", recording, "--timeout-refused", "--pages", replayPages}, &again, &againErr, noRunner(t)); code != 1 ||
		again.String() != report || !mapsEqual(pagesIn(t, replayPages), pagesIn(t, pages)) {
		t.Fatalf("replay exit %d\n%s\nwant\n%s\n%s", code, again.String(), report, againErr.String())
	}
	again.Reset()
	againErr.Reset()
	if code := run(context.Background(), []string{"--host", "claude-code", "--replay", recording}, &again, &againErr, noRunner(t)); code != 1 ||
		strings.Contains(again.String(), "target") || !strings.HasSuffix(againErr.String(), fmt.Sprintf(", pass %d: timeout; the run stopped before it could be judged\n", nth(5).pass)) {
		t.Fatalf("replay without the flag: exit %d\n%s\n%s", code, again.String(), againErr.String())
	}

	// Without the flag the run stops at the timeout, and its trace says so.
	stopTrace := filepath.Join(dir, "stopped")
	runner = tapRunner{fakeRunner: &fakeRunner{t: t, tmp: tmp, answer: timedOut}}
	out.Reset()
	errOut.Reset()
	code = run(context.Background(), []string{"--host", "claude-code", "--spend-model-quota", "--trace", stopTrace}, &out, &errOut, func(string) update.Runner { return runner })
	if code != 1 || len(runner.requests) != 5 || strings.Contains(out.String(), "target") ||
		!strings.HasSuffix(errOut.String(), fmt.Sprintf(", pass %d: timeout; the run stopped before it could be judged\n", nth(5).pass)) {
		t.Fatalf("without the flag: exit %d after %d calls\n%s\n%s", code, len(runner.requests), out.String(), errOut.String())
	}
	run2, passes, end, _ := readTrace(t, stopTrace)
	if run2.TimeoutRefused || len(passes) != 5 || passes[4].Refused != "" || end != (traceEnd{Outcome: "stopped", Passes: 5, Timeouts: 1, Stopped: update.ReasonTimeout}) {
		t.Fatalf("run %+v, lines %+v, end %+v", run2, passes, end)
	}
}

// A --trace that names a folder holding a file, a file, or a link to
// nothing is refused before anything is made or called, and so is one
// with --replay, which calls nothing, or one that is, holds or lies in the
// pages folder, or holds the recording.
func TestTraceRefusals(t *testing.T) {
	for _, tc := range []struct {
		name string
		args func(dir string) []string
		want string
	}{
		{"a folder that holds a file", func(dir string) []string {
			must(t, os.Mkdir(filepath.Join(dir, "trace"), 0o700))
			must(t, os.WriteFile(filepath.Join(dir, "trace", "run.json"), []byte("{}\n"), 0o600))
			return []string{"--trace", filepath.Join(dir, "trace")}
		}, "--trace must name a folder that is new or empty"},
		{"a file", func(dir string) []string {
			must(t, os.WriteFile(filepath.Join(dir, "trace"), nil, 0o600))
			return []string{"--trace", filepath.Join(dir, "trace")}
		}, "--trace must name a folder that is new or empty"},
		{"a link to nothing", func(dir string) []string {
			must(t, os.Symlink(filepath.Join(dir, "gone"), filepath.Join(dir, "trace")))
			return []string{"--trace", filepath.Join(dir, "trace")}
		}, "--trace names a link to nothing"},
		{"the pages folder", func(dir string) []string {
			return []string{"--trace", filepath.Join(dir, "out"), "--pages", filepath.Join(dir, "out")}
		}, "--trace must name a folder of its own, apart from --pages and --record"},
		{"a folder holding the recording", func(dir string) []string {
			return []string{"--trace", filepath.Join(dir, "out"), "--record", filepath.Join(dir, "out", "answers.jsonl")}
		}, "--trace must name a folder of its own, apart from --pages and --record"},
		{"a folder in the pages folder", func(dir string) []string {
			return []string{"--trace", filepath.Join(dir, "out", "trace"), "--pages", filepath.Join(dir, "out")}
		}, "--trace must name a folder of its own, apart from --pages and --record"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tmp := isolate(t)
			dir := t.TempDir()
			args := append([]string{"--host", "claude-code", "--spend-model-quota"}, tc.args(dir)...)
			before := leftIn(t, dir)
			var out, errOut bytes.Buffer
			if code := run(context.Background(), args, &out, &errOut, noRunner(t)); code != 2 || out.Len() != 0 || !strings.HasPrefix(errOut.String(), "live-accuracy: "+tc.want+"; usage: ") {
				t.Fatalf("exit %d, out %q, err %q", code, out.String(), errOut.String())
			}
			if left := leftIn(t, dir); !slices.Equal(left, before) {
				t.Fatalf("the refused run changed %q to %q", before, left)
			}
			if left := leftIn(t, tmp); len(left) != 0 {
				t.Fatalf("the refused run left %q", left)
			}
		})
	}
	t.Run("with --replay", func(t *testing.T) {
		isolate(t)
		dir := t.TempDir()
		recording := filepath.Join(dir, "answers.jsonl")
		writeRecording(t, recording, nil)
		var out, errOut bytes.Buffer
		code := run(context.Background(), []string{"--host", "claude-code", "--replay", recording, "--trace", filepath.Join(dir, "trace")}, &out, &errOut, noRunner(t))
		if code != 2 || out.Len() != 0 || !strings.HasPrefix(errOut.String(), "live-accuracy: --replay runs no CLI, so it has no trace to write; usage: ") {
			t.Fatalf("exit %d, out %q, err %q", code, out.String(), errOut.String())
		}
		if left := leftIn(t, dir); !slices.Equal(left, []string{"answers.jsonl"}) {
			t.Fatalf("the refused replay left %q", left)
		}
	})
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

// A --trace in the git work tree the command is run from, as go run runs
// it from the checkout, is refused before anything is made or called,
// named directly or through a link, from the top of the tree or a folder
// in it; one beside the tree is not.
func TestTraceInsideTheWorkTree(t *testing.T) {
	for _, tc := range []struct {
		name, cwd, trace string
		refused          bool
	}{
		{"in the tree", "repo/cmd", "repo/trace", true},
		{"the tree's own folder, from its top", "repo", "repo/cmd", true},
		{"through a link to the tree", "repo/cmd", "alias/trace", true},
		{"beside the tree", "repo/cmd", "beside/trace", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tmp := isolate(t)
			t.Setenv("PATH", t.TempDir())
			dir := t.TempDir()
			must(t, os.MkdirAll(filepath.Join(dir, "repo", ".git"), 0o700))
			must(t, os.MkdirAll(filepath.Join(dir, "repo", "cmd"), 0o700))
			must(t, os.Symlink(filepath.Join(dir, "repo"), filepath.Join(dir, "alias")))
			t.Chdir(filepath.Join(dir, tc.cwd))
			trace := filepath.Join(dir, tc.trace)
			before := leftIn(t, dir)
			var out, errOut bytes.Buffer
			if !tc.refused {
				runner := tapRunner{fakeRunner: &fakeRunner{t: t, tmp: tmp}}
				if code := run(context.Background(), []string{"--host", "claude-code", "--spend-model-quota", "--trace", trace}, &out, &errOut, func(string) update.Runner { return runner }); code != 1 || len(runner.requests) != 21 {
					t.Fatalf("exit %d after %d calls\n%s", code, len(runner.requests), errOut.String())
				}
				if _, passes, _, _ := readTrace(t, trace); len(passes) != 21 {
					t.Fatalf("%d lines", len(passes))
				}
				return
			}
			code := run(context.Background(), []string{"--host", "claude-code", "--spend-model-quota", "--trace", trace}, &out, &errOut, noRunner(t))
			if code != 2 || out.Len() != 0 || !strings.HasPrefix(errOut.String(), "live-accuracy: --trace must name a folder outside the git work tree it is run from, ") {
				t.Fatalf("exit %d, out %q, err %q", code, out.String(), errOut.String())
			}
			if left := leftIn(t, dir); !slices.Equal(left, before) {
				t.Fatalf("the refused run changed %q to %q", before, left)
			}
		})
	}
}

// A trace that cannot be made, under a link to nothing, stops the run
// before any runner is made, and leaves nothing made: neither the
// recording nor the pages folder. A pages folder that cannot be made,
// once the trace is, takes the trace away again, and an empty folder the
// trace was to fill gets its mode back.
func TestATraceThatCannotBeMade(t *testing.T) {
	t.Run("the trace", func(t *testing.T) {
		tmp := isolate(t)
		t.Setenv("PATH", t.TempDir())
		dir := t.TempDir()
		must(t, os.Symlink(filepath.Join(dir, "gone"), filepath.Join(dir, "link")))
		var out, errOut bytes.Buffer
		args := []string{"--host", "claude-code", "--spend-model-quota", "--trace", filepath.Join(dir, "link", "trace"),
			"--record", filepath.Join(dir, "answers.jsonl"), "--pages", filepath.Join(dir, "pages")}
		if code := run(context.Background(), args, &out, &errOut, noRunner(t)); code != 1 || out.Len() != 0 || !strings.HasPrefix(errOut.String(), "live-accuracy: could not make the trace: ") {
			t.Fatalf("exit %d, out %q, err %q", code, out.String(), errOut.String())
		}
		if left := leftIn(t, dir); !slices.Equal(left, []string{"link"}) {
			t.Errorf("the run left %q", left)
		}
		if left := leftIn(t, tmp); len(left) != 0 {
			t.Errorf("the run left %q", left)
		}
		// The recording can be made again.
		if _, err := os.Lstat(filepath.Join(dir, "answers.jsonl")); err == nil {
			t.Errorf("the recording is still there")
		}
	})
	for _, made := range []bool{true, false} {
		t.Run(fmt.Sprintf("the pages folder, the trace's folder made %v", made), func(t *testing.T) {
			tmp := isolate(t)
			t.Setenv("PATH", t.TempDir())
			dir := t.TempDir()
			must(t, os.Symlink(filepath.Join(dir, "gone"), filepath.Join(dir, "link")))
			trace := filepath.Join(dir, "new", "trace")
			want := []string{"link"}
			if !made {
				trace = filepath.Join(dir, "trace")
				must(t, os.Mkdir(trace, 0o755))
				must(t, os.Chmod(trace, 0o755))
				want = append(want, "trace")
			}
			var out, errOut bytes.Buffer
			args := []string{"--host", "claude-code", "--spend-model-quota", "--trace", trace, "--pages", filepath.Join(dir, "link", "pages")}
			if code := run(context.Background(), args, &out, &errOut, noRunner(t)); code != 1 || out.Len() != 0 || !strings.HasPrefix(errOut.String(), "live-accuracy: could not make the pages folder: ") {
				t.Fatalf("exit %d, out %q, err %q", code, out.String(), errOut.String())
			}
			if left := leftIn(t, dir); !slices.Equal(left, want) {
				t.Errorf("the run left %q", left)
			}
			if !made {
				if info, err := os.Stat(trace); err != nil || info.Mode().Perm() != 0o755 {
					t.Errorf("the trace's folder: %v, %v", info.Mode(), err)
				}
			}
			if left := leftIn(t, tmp); len(left) != 0 {
				t.Errorf("the run left %q", left)
			}
		})
	}
}

// run.json's eva_md is the build's vcs.revision and vcs.modified: the
// commit, or "" when the build has none, and dirty only when modified is
// "true".
func TestBuildFrom(t *testing.T) {
	settings := func(kv ...string) *debug.BuildInfo {
		info := &debug.BuildInfo{Settings: []debug.BuildSetting{{Key: "-buildmode", Value: "exe"}, {Key: "GOOS", Value: "made-up"}}}
		for i := 0; i < len(kv); i += 2 {
			info.Settings = append(info.Settings, debug.BuildSetting{Key: kv[i], Value: kv[i+1]})
		}
		return info
	}
	const commit = "0123456789abcdef0123456789abcdef01234567"
	for _, tc := range []struct {
		info *debug.BuildInfo
		ok   bool
		want traceBuild
	}{
		{settings("vcs", "git", "vcs.revision", commit, "vcs.time", "2026-11-13T00:00:00Z", "vcs.modified", "true"), true, traceBuild{Commit: commit, Dirty: true}},
		{settings("vcs.revision", commit, "vcs.modified", "false"), true, traceBuild{Commit: commit}},
		{settings("vcs.revision", commit), true, traceBuild{Commit: commit}},
		{settings("vcs.modified", "true"), true, traceBuild{Dirty: true}},
		{settings(), true, traceBuild{}},
		{settings("vcs.revision", commit), false, traceBuild{}},
		{nil, false, traceBuild{}},
	} {
		if got := buildFrom(tc.info, tc.ok); got != tc.want {
			t.Errorf("%+v, %v: %+v, want %+v", tc.info, tc.ok, got, tc.want)
		}
	}
	if got := buildOf(); got != buildFrom(debug.ReadBuildInfo()) {
		t.Errorf("buildOf %+v", got)
	}
}

// Through the real runner, a fake claude that refuses the effort max and
// answers at xhigh: run.json has the version the fake prints, each pass's
// line the runner's tokens, attempts, effort, model and note, and the raw
// files what the fake printed on each attempt, byte for byte. Nothing of
// the run's temporary home reaches the JSON files.
func TestTraceThroughTheRealRunner(t *testing.T) {
	tmp := isolate(t)
	home := os.Getenv("HOME")
	refusal := `{"type":"result","subtype":"success","is_error":true,"api_error_status":400,"result":"API Error: 400 effort max is not supported by this model","usage":{"input_tokens":0,"output_tokens":0}}`
	answer := `{"type":"result","subtype":"success","is_error":false,"usage":{"input_tokens":120,"cache_read_input_tokens":30,"output_tokens":40},` +
		`"modelUsage":{"claude-test-reported":{"inputTokens":120,"cacheReadInputTokens":30,"outputTokens":40}},"permission_denials":[],"structured_output":` + empty + `}`
	claude := fakeCLI(t, "claude", "#!/bin/sh\nif [ \"$1\" = --version ]; then echo '2.1.400 (Claude Code)'; exit 0; fi\ncat > /dev/null\n"+
		"for a in \"$@\"; do\n  if [ \"$a\" = max ]; then\n    printf 'made-up refusal notice' >&2\n    cat <<'EOF'\n"+refusal+"\nEOF\n    exit 1\n  fi\ndone\n"+
		"printf 'made-up answer notice\\n' >&2\ncat <<'EOF'\n"+answer+"\nEOF\n")
	trace := filepath.Join(t.TempDir(), "trace")
	var out, errOut bytes.Buffer
	if code := run(context.Background(), []string{"--host", "claude-code", "--spend-model-quota", "--cli", claude, "--trace", trace}, &out, &errOut, update.NewRunner); code != 1 ||
		!strings.Contains(out.String(), "  answers refused: 0 of 21\n  answers at effort xhigh: 21 of 21\nThe run misses the target.\n") {
		t.Fatalf("exit %d\n%s\n%s", code, out.String(), errOut.String())
	}
	run, passes, end, raw := readTrace(t, trace)
	if run.CLIVersion != "2.1.400" || run.Effort != "highest" || run.Model != "" || len(passes) != 21 || end != (traceEnd{Outcome: "missed", Passes: 21}) {
		t.Fatalf("run %+v, %d lines, end %+v", run, len(passes), end)
	}
	for _, p := range passes {
		if p.TokensIn != 150 || p.TokensOut != 40 || p.Attempts != 2 || p.EffortUsed != "xhigh" || p.ModelReported != "claude-test-reported" ||
			p.ModelNote != update.NoteDefaultModel || p.Degraded != "effort max was rejected; the run used xhigh" || p.Reason != "" || p.Refused != "" {
			t.Errorf("line %+v", p)
		}
		name := fmt.Sprintf("%02d-%02d-", p.Script, p.Pass)
		if string(raw[name+"01.out"]) != refusal+"\n" || string(raw[name+"01.err"]) != "made-up refusal notice" ||
			string(raw[name+"02.out"]) != answer+"\n" || string(raw[name+"02.err"]) != "made-up answer notice\n" {
			t.Errorf("%s: %q", name, raw)
		}
	}
	if len(raw) != 84 {
		t.Errorf("%d raw files", len(raw))
	}
	noMachine(t, trace, tmp, home, filepath.Dir(claude))
	if left := leftIn(t, tmp); len(left) != 0 {
		t.Errorf("the run left %q", left)
	}
}

// What the run counts refused, as the trace names it: the runner's reason
// when it refused the answer, a timeout only under --timeout-refused, the
// validator's error for an answer it refuses, and nothing for an answer
// it takes or a failure that stops the run.
func TestRefusedWhy(t *testing.T) {
	for _, tc := range []struct {
		res            update.Result
		timeoutRefused bool
		want           string
	}{
		{update.Result{Reason: update.ReasonToolUsed}, false, "tool_used"},
		{update.Result{Reason: update.ReasonInvalidOutput}, false, "invalid_output"},
		{update.Result{Reason: update.ReasonTimeout}, true, "timeout"},
		{update.Result{Reason: update.ReasonTimeout}, false, ""},
		{update.Result{Reason: update.ReasonRateLimit}, true, ""},
		{update.Result{Output: []byte(empty)}, false, ""},
		{update.Result{Output: []byte(`{"summary":1}`)}, false, "invalid_output: update: output rejected: "},
	} {
		got := refusedWhy(tc.res, update.Batch{}, threadpage.Page{}, tc.timeoutRefused)
		if got != tc.want && !(strings.HasSuffix(tc.want, ": ") && strings.HasPrefix(got, tc.want)) {
			t.Errorf("%+v, %v: %q, want %q", tc.res, tc.timeoutRefused, got, tc.want)
		}
	}
}
