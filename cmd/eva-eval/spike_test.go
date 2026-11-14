package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/heyNag/eva-md/internal/eval/results"
	"github.com/heyNag/eva-md/internal/eval/spike"
	"github.com/heyNag/eva-md/internal/eval/spike/arm"
	"github.com/heyNag/eva-md/internal/host/adapter"
	"github.com/heyNag/eva-md/internal/update"
)

// madeUpModel is the model name every live run of these tests asks for.
// No row, recording or output may hold it.
const madeUpModel = "zq-fixture-model-7"

// spikeRig is a run's world: a repository measured, a temp folder, a
// PATH that holds only a stand-in for each host's CLI, which leaves a
// mark if it is ever started, and the probe's run of --version replaced.
type spikeRig struct {
	repo, tmp, bin string
	probes         int
}

func newSpikeRig(t *testing.T) *spikeRig {
	t.Helper()
	repo, _ := newRepo(t)
	r := &spikeRig{repo: repo, tmp: t.TempDir(), bin: t.TempDir()}
	t.Setenv("TMPDIR", r.tmp)
	t.Setenv("PATH", r.bin+":/usr/bin:/bin")
	for _, h := range adapter.All() {
		write(t, filepath.Join(r.bin, h.Names.CLI), "#!/bin/sh\ntouch \"$0.ran\"\nexit 1\n")
		if err := os.Chmod(filepath.Join(r.bin, h.Names.CLI), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	savedRunner, savedProbe := spikeRunner, spikeProbeRun
	t.Cleanup(func() {
		spikeRunner, spikeProbeRun = savedRunner, savedProbe
		for _, h := range adapter.All() {
			if _, err := os.Stat(filepath.Join(r.bin, h.Names.CLI+".ran")); err == nil {
				t.Errorf("the stand-in for %s was started", h.Names.CLI)
			}
		}
	})
	spikeRunner = func(host string) update.Runner {
		t.Errorf("a runner for %s was made", host)
		return &spikeFake{t: t}
	}
	spikeProbeRun = func(ctx context.Context, cli string, args ...string) ([]byte, error) {
		r.probes++
		if !slices.Equal(args, []string{"--version"}) {
			t.Errorf("the probe ran %v", args)
		}
		return []byte("9.8." + fmt.Sprint(700+r.probes) + " (made up)\n"), nil
	}
	return r
}

// spikeFake is a host runner that answers every call in the schema's
// shape: each first call with one operation that cites words no entry
// holds, so the checker asks for a repair, and each repair with no fix.
// Each call it is asked is kept; fail, when set, may change a call's
// result.
type spikeFake struct {
	t        *testing.T
	tmp      string
	requests []update.Request
	homes    map[string]bool
	fail     func(n int, res *update.Result) error
}

func (f *spikeFake) Run(ctx context.Context, r update.Request) (update.Result, error) {
	f.requests = append(f.requests, r)
	n := len(f.requests)
	names, err := os.ReadDir(r.RunDir)
	home := filepath.Dir(r.RunDir)
	tmp, _ := filepath.EvalSymlinks(f.tmp)
	parent, _ := filepath.EvalSymlinks(filepath.Dir(home))
	if err != nil || len(names) != 0 || !filepath.IsAbs(r.RunDir) || parent != tmp || !strings.HasPrefix(filepath.Base(home), "eva-eval-spike-") {
		f.t.Errorf("request %d: run folder %s: %v, %d names", n, r.RunDir, err, len(names))
	}
	if f.homes == nil {
		f.homes = map[string]bool{}
	}
	f.homes[home] = true
	res := update.Result{Model: r.Model, TokensIn: 1000 + n, TokensOut: 50, Attempts: 1, Effort: r.Effort}
	form := spike.FormQuotes
	if bytes.Equal(r.Schema, spike.Schema(spike.FormSegments)) || bytes.Equal(r.Schema, spike.RepairSchema(spike.FormSegments)) {
		form = spike.FormSegments
	}
	switch {
	case bytes.Equal(r.Schema, spike.RepairSchema(form)):
		res.Output = []byte(`{"fixes":[]}`)
	case form == spike.FormQuotes:
		res.Output = []byte(`{"ops":[{"op":"note","id":null,"kind":"did","text":"The user opened the chat.","admit":null,"ev":[{"e":1,"q":"words that no entry holds at all","as":"said"}]}],"nothing":null}`)
	default:
		res.Output = []byte(`{"ops":[{"op":"note","id":null,"kind":"did","text":"The user opened the chat.","admit":null,"ev":[{"e":1,"s":99,"as":"said"}]}],"nothing":null}`)
	}
	if n%7 == 0 {
		res.Output, res.Reason, res.Effort = nil, update.ReasonRateLimit, ""
	}
	if f.fail != nil {
		if err := f.fail(n, &res); err != nil {
			return update.Result{}, err
		}
	}
	return res, nil
}

// spikeCmd runs eva-eval spike with args.
func spikeCmd(args ...string) (int, string, string) {
	return cmd(append([]string{"spike"}, args...)...)
}

// liveArgs are a live run's flags on host, writing to out.
func liveArgs(r *spikeRig, host, out string, more ...string) []string {
	return append([]string{"run", "--host", host, "--tier", "small", "--effort", "low", "--form", "quotes", "--out", out,
		"--model", madeUpModel, "--repo", r.repo}, more...)
}

// noModel fails the test when any of texts holds the made-up model's name,
// in any case.
func noModel(t *testing.T, what string, texts ...string) {
	t.Helper()
	for _, text := range texts {
		if strings.Contains(strings.ToLower(text), strings.ToLower(madeUpModel)) {
			t.Errorf("%s holds the model's name", what)
		}
	}
}

// totalPasses counts the spike's passes by hand.
func totalPasses() int {
	n := 0
	for _, s := range spike.Scripts() {
		n += len(s.Scenario.Passes)
	}
	return n
}

func TestSpikeIsRegistered(t *testing.T) {
	if _, ok := instruments["spike"]; !ok {
		t.Fatal("spike is not registered")
	}
	code, _, errOut := spikeCmd()
	if code != 2 || !strings.Contains(errOut, "eva-eval spike plan") || !strings.Contains(errOut, "spike frontier") {
		t.Errorf("spike alone: %d %q", code, errOut)
	}
	if code, _, _ := spikeCmd("walk"); code != 2 {
		t.Errorf("spike walk: %d", code)
	}
}

// The committed plan is a fresh plan at the defaults, byte for byte, and
// each arm's calls are the scripts' passes.
func TestRunPlanIsAFreshPlan(t *testing.T) {
	root := moduleRoot(t)
	committed, err := os.ReadFile(filepath.Join(root, "eval", "spike", "run-plan.md"))
	if err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(t.TempDir(), "plan.md")
	code, out, errOut := spikeCmd("plan", "--out", file)
	if code != 0 || errOut != "" || !strings.HasPrefix(out, "Wrote the plan of 24 arms to ") {
		t.Fatalf("plan: %d %q %q", code, out, errOut)
	}
	fresh, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(fresh, committed) {
		t.Fatalf("eval/spike/run-plan.md is not a fresh plan at the defaults; write it again with eva-eval spike plan --out eval/spike/run-plan.md")
	}
	p, err := makePlan(planOptions{repeat: 1, efforts: planEfforts, allowance: planAllowance, overhead: map[string]int{}})
	if err != nil {
		t.Fatal(err)
	}
	if string(fresh) != p.render() {
		t.Fatal("the command wrote another plan than makePlan's")
	}
	want := totalPasses()
	if want < 20 {
		t.Fatalf("the spike has %d passes", want)
	}
	if len(p.arms) != len(adapter.All())*len(arm.Tiers)*len(planEfforts)*len(spike.Forms) {
		t.Fatalf("%d arms", len(p.arms))
	}
	calls := 0
	for _, a := range p.arms {
		if a.first != want || a.repair != want {
			t.Errorf("%s %s %s %s: %d first and %d repair calls, not %d", a.host, a.tier, a.effort, a.form, a.first, a.repair, want)
		}
		if !strings.Contains(string(fresh), fmt.Sprintf("| %s | %s | %s | %s | %s | %s |", a.host, a.tier, a.effort, a.form, num(a.first), num(a.repair))) {
			t.Errorf("the plan has no line for %s %s %s %s", a.host, a.tier, a.effort, a.form)
		}
		calls += a.first + a.repair
	}
	if !strings.Contains(string(fresh), "Against the row's allowance: "+num(calls)+" calls at most, under it") {
		t.Errorf("the plan does not say its %d calls are under the row's allowance", calls)
	}
	for _, words := range []string{"divided by 4 and rounded up", "unknown until DV-02's recordings", "Spoken scripts", "about a tenth more", "lead's to state"} {
		if !strings.Contains(string(fresh), words) {
			t.Errorf("the plan's head does not say %q", words)
		}
	}
}

// A first call's input tokens are worked out by hand for one script; a
// repair call adds the allowance; the overhead and the repeat count as
// the rules say.
func TestPlanArithmetic(t *testing.T) {
	base, err := makePlan(planOptions{repeat: 1, efforts: []string{"low"}, allowance: 1000, overhead: map[string]int{}})
	if err != nil {
		t.Fatal(err)
	}
	s := spike.Scripts()[0]
	f := spike.FormSegments
	in, err := spike.Render(s.Scenario.Sessions[s.Scenario.Passes[0].Session], nil, f)
	if err != nil {
		t.Fatal(err)
	}
	bytesIn := len(spike.Prompt(f)) + len(in.Bytes) + len(spike.Schema(f))
	if want := (bytesIn + 3) / 4; len(s.Scenario.Passes) != 1 || base.scripts[0].in[f] != want {
		t.Fatalf("script %s: %d tokens, want %d over %d passes", s.Scenario.ID, base.scripts[0].in[f], want, len(s.Scenario.Passes))
	}
	if tokens(8) != 2 || tokens(9) != 3 || tokens(0) != 0 {
		t.Error("tokens does not round up")
	}
	firstIn := map[spike.Form]int{}
	for _, ps := range base.scripts {
		for _, f := range spike.Forms {
			firstIn[f] += ps.in[f]
		}
	}
	n := totalPasses()
	for _, a := range base.arms {
		if a.in != 2*firstIn[a.form]+n*1000 || a.out != 2*n*1000 {
			t.Errorf("%s %s %s: %d in and %d out, first calls' input %d", a.host, a.tier, a.form, a.in, a.out, firstIn[a.form])
		}
	}
	more, err := makePlan(planOptions{repeat: 3, efforts: []string{"low", "medium"}, allowance: 1000, overhead: map[string]int{string(adapter.Codex): 40}})
	if err != nil {
		t.Fatal(err)
	}
	for _, b := range more.arms {
		wantIn := 3 * (2*firstIn[b.form] + n*1000)
		if b.host == string(adapter.Codex) {
			wantIn += 2 * 3 * n * 40
		}
		if b.first != 3*n || b.repair != 3*n || b.in != wantIn || b.out != 3*2*n*1000 {
			t.Errorf("%s %s %s: %d first, %d in (want %d), %d out", b.host, b.effort, b.form, b.first, b.in, wantIn, b.out)
		}
	}
	if len(more.arms) != 2*len(base.arms) {
		t.Errorf("two efforts make %d arms", len(more.arms))
	}
}

func TestPlanRefusals(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "plan.md")
	for _, args := range [][]string{
		{"plan"},
		{"plan", "--out", file, "extra"},
		{"plan", "--out", file, "--repeat", "0"},
		{"plan", "--out", file, "--repeat", "101"},
		{"plan", "--out", file, "--output-allowance", "0"},
		{"plan", "--out", file, "--efforts", "minimal"},
		{"plan", "--out", file, "--efforts", "low,low"},
		{"plan", "--out", file, "--efforts", "highest"},
		{"plan", "--out", file, "--efforts", ""},
		{"plan", "--out", file, "--overhead", "elsewhere=5"},
		{"plan", "--out", file, "--overhead", string(adapter.Codex) + "=5," + string(adapter.Codex) + "=6"},
		{"plan", "--out", file, "--overhead", string(adapter.Codex) + "=-1"},
		{"plan", "--out", file, "--overhead", string(adapter.Codex)},
	} {
		if code, _, _ := spikeCmd(args...); code != 2 {
			t.Errorf("%q: exit %d", args, code)
		}
	}
	if _, err := os.Stat(file); !errors.Is(err, fs.ErrNotExist) {
		t.Error("a refused plan wrote its file")
	}
	if code, _, _ := spikeCmd("plan", "--out", file, "--overhead", string(adapter.ClaudeCode)+"=7,"+string(adapter.Codex)+"=0"); code != 0 {
		t.Errorf("a good overhead: exit %d", code)
	}
	data, _ := os.ReadFile(file)
	if !strings.Contains(string(data), string(adapter.ClaudeCode)+" 7, "+string(adapter.Codex)+" 0") {
		t.Error("the plan does not state the overhead it was given")
	}
}

// Every refusal of run calls nothing, makes no runner, writes no row and
// no recording, and prints no model's name.
func TestRunRefusals(t *testing.T) {
	claude, codex := string(adapter.ClaudeCode), string(adapter.Codex)
	for _, tc := range []struct {
		name string
		args func(r *spikeRig, out, rec string) []string
		want string
	}{
		{"no quota", func(r *spikeRig, out, rec string) []string { return liveArgs(r, codex, out, "--record", rec) },
			"Nothing was called; run it again with --spend-model-quota"},
		{"quota false", func(r *spikeRig, out, rec string) []string {
			return liveArgs(r, claude, out, "--spend-model-quota=false", "--record", rec)
		}, "Nothing was called; run it again with --spend-model-quota"},
		{"no model", func(r *spikeRig, out, rec string) []string {
			return []string{"run", "--host", codex, "--tier", "small", "--effort", "low", "--form", "quotes", "--out", out, "--repo", r.repo, "--spend-model-quota"}
		}, "a live run needs --model"},
		{"replay with quota", func(r *spikeRig, out, rec string) []string {
			return []string{"run", "--host", codex, "--tier", "small", "--effort", "low", "--form", "quotes", "--out", out, "--repo", r.repo, "--replay", rec, "--spend-model-quota"}
		}, "--replay calls nothing"},
		{"replay with model", func(r *spikeRig, out, rec string) []string { return liveArgs(r, codex, out, "--replay", rec) }, "--replay calls nothing"},
		{"replay with cli", func(r *spikeRig, out, rec string) []string {
			return []string{"run", "--host", codex, "--tier", "small", "--effort", "low", "--form", "quotes", "--out", out, "--repo", r.repo, "--replay", rec, "--cli", "/x/codex"}
		}, "--replay calls nothing"},
		{"record and replay", func(r *spikeRig, out, rec string) []string {
			return []string{"run", "--host", codex, "--tier", "small", "--effort", "low", "--form", "quotes", "--out", out, "--repo", r.repo, "--replay", rec, "--record", rec + "2"}
		}, "can't be used together"},
		{"ignore on claude", func(r *spikeRig, out, rec string) []string {
			return liveArgs(r, claude, out, "--spend-model-quota", "--codex-ignore-user-config")
		}, "is for codex only"},
		{"an effort the host lacks", func(r *spikeRig, out, rec string) []string {
			a := liveArgs(r, claude, out, "--spend-model-quota")
			a[6] = "minimal"
			return a
		}, `effort "minimal" is not one of`},
		{"no host", func(r *spikeRig, out, rec string) []string {
			a := liveArgs(r, claude, out, "--spend-model-quota")
			a[2] = "claude"
			return a
		}, "--host must be"},
		{"a tier that is a name", func(r *spikeRig, out, rec string) []string {
			a := liveArgs(r, claude, out, "--spend-model-quota")
			a[4] = "haiku"
			return a
		}, "--tier must be"},
		{"a bad cli", func(r *spikeRig, out, rec string) []string {
			return liveArgs(r, codex, out, "--spend-model-quota", "--cli", "bin/codex")
		}, "is not the absolute path"},
		{"repeat", func(r *spikeRig, out, rec string) []string {
			return liveArgs(r, codex, out, "--spend-model-quota", "--repeat", "0")
		}, "--repeat must be"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newSpikeRig(t)
			out := filepath.Join(t.TempDir(), "rows")
			rec := filepath.Join(t.TempDir(), "rec.jsonl")
			if strings.HasPrefix(tc.name, "replay") || tc.name == "record and replay" {
				write(t, rec, "")
			}
			code, stdout, errOut := spikeCmd(tc.args(r, out, rec)...)
			if code != 2 || !strings.Contains(errOut, tc.want) {
				t.Fatalf("exit %d, out %q, err %q", code, stdout, errOut)
			}
			noModel(t, "the output", stdout, errOut)
			if _, err := os.Stat(out); !errors.Is(err, fs.ErrNotExist) {
				t.Error("a refused run made its results folder")
			}
			if !strings.HasPrefix(tc.name, "replay") && tc.name != "record and replay" {
				if _, err := os.Stat(rec); !errors.Is(err, fs.ErrNotExist) {
					t.Error("a refused run made its recording")
				}
			}
			if left := leftIn(t, r.tmp); len(left) != 0 {
				t.Errorf("the refused run left %q", left)
			}
		})
	}
}

// leftIn lists what a folder holds.
func leftIn(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names
}

// Without --spend-model-quota the run says how many calls it would make,
// which is the scripts' passes, after its checks, and calls nothing.
func TestRunWithoutQuotaSaysWhatItWouldCall(t *testing.T) {
	r := newSpikeRig(t)
	out := filepath.Join(t.TempDir(), "rows")
	code, stdout, errOut := spikeCmd(liveArgs(r, string(adapter.Codex), out, "--repeat", "2")...)
	n := 2 * totalPasses()
	want := fmt.Sprintf("makes %d first calls, one for each of the %d passes of the spike's %d scripts in each of 2 rounds, and at most %d repair calls: %d calls at most.\n", n, n/2, len(spike.Scripts()), n, 2*n)
	if code != 2 || !strings.Contains(stdout, want) || !strings.Contains(errOut, "Nothing was called") {
		t.Fatalf("exit %d, out %q, err %q", code, stdout, errOut)
	}
	if r.probes != 1 || !strings.Contains(stdout, "9.8.701") {
		t.Errorf("the version was read %d times: %q", r.probes, stdout)
	}
	noModel(t, "the output", stdout, errOut)
}

// A model whose name a row the run would write holds is refused before
// anything is called, in any case or spelling of its letters.
func TestRunRefusesAModelARowHolds(t *testing.T) {
	for _, model := range []string{"small", "Quotes", "calls-first", "SPIKE", "fact.recall", "repair.yield"} {
		t.Run(model, func(t *testing.T) {
			r := newSpikeRig(t)
			out := filepath.Join(t.TempDir(), "rows")
			args := liveArgs(r, string(adapter.ClaudeCode), out, "--spend-model-quota")
			args[slices.Index(args, madeUpModel)] = model
			code, stdout, errOut := spikeCmd(args...)
			if code != 2 || !strings.Contains(errOut, "holds the model's name") || !strings.Contains(errOut, "Nothing was called") {
				t.Fatalf("exit %d, out %q, err %q", code, stdout, errOut)
			}
			if strings.Contains(stdout+errOut, model) && model != "small" {
				t.Errorf("the output names the model %s", model)
			}
			if _, err := os.Stat(out); !errors.Is(err, fs.ErrNotExist) {
				t.Error("a refused run made its results folder")
			}
		})
	}
}

// A host whose version cannot be read is refused: every row names it.
func TestRunRefusesAHostWithNoVersion(t *testing.T) {
	r := newSpikeRig(t)
	spikeProbeRun = func(context.Context, string, ...string) ([]byte, error) { return []byte("no version here\n"), nil }
	code, stdout, errOut := spikeCmd(liveArgs(r, string(adapter.Codex), filepath.Join(t.TempDir(), "rows"), "--spend-model-quota")...)
	if code != 2 || !strings.Contains(errOut, "could not read codex's version") {
		t.Fatalf("exit %d, out %q, err %q", code, stdout, errOut)
	}
	if err := os.Remove(filepath.Join(r.bin, "codex")); err != nil {
		t.Fatal(err)
	}
	spikeProbeRun = func(context.Context, string, ...string) ([]byte, error) {
		t.Error("a CLI that is not on PATH was probed")
		return nil, nil
	}
	if code, _, errOut := spikeCmd(liveArgs(r, string(adapter.Codex), filepath.Join(t.TempDir(), "rows"), "--spend-model-quota")...); code != 2 || !strings.Contains(errOut, "version") {
		t.Fatalf("no CLI: exit %d, err %q", code, errOut)
	}
}

// liveFake makes the runner a fake on the rig's temp folder, and returns
// it.
func liveFake(t *testing.T, r *spikeRig) *spikeFake {
	fake := &spikeFake{t: t, tmp: r.tmp}
	spikeRunner = func(host string) update.Runner {
		if host != string(adapter.ClaudeCode) && host != string(adapter.Codex) {
			t.Errorf("a runner for %s", host)
		}
		return fake
	}
	return fake
}

// withoutTime is rows with every time cleared.
func withoutTime(rows []results.Row) []results.Row {
	out := slices.Clone(rows)
	for i := range out {
		out[i].Time = time.Time{}
	}
	return out
}

// A live run on a fake runner asks each call through the update runner as
// cmd/live-accuracy does, makes no more calls than it said, keeps nothing
// but its rows and its recording, and its recording replayed, with no
// runner, gives the same rows apart from time. The model's name is in no
// byte of the rows, the recording or the output.
func TestRunRecordThenReplay(t *testing.T) {
	r := newSpikeRig(t)
	fake := liveFake(t, r)
	dir := t.TempDir()
	rec := filepath.Join(dir, "rec.jsonl")
	liveOut := filepath.Join(dir, "live")
	code, stdout, errOut := spikeCmd(liveArgs(r, string(adapter.Codex), liveOut, "--spend-model-quota", "--record", rec, "--codex-ignore-user-config")...)
	if code != 0 {
		t.Fatalf("live: exit %d, out %q, err %q", code, stdout, errOut)
	}
	n := totalPasses()
	first, repair := 0, 0
	for _, q := range fake.requests {
		switch {
		case bytes.Equal(q.Schema, spike.Schema(spike.FormQuotes)):
			first++
		case bytes.Equal(q.Schema, spike.RepairSchema(spike.FormQuotes)):
			repair++
		default:
			t.Errorf("a request with another schema")
		}
		if q.Host != string(adapter.Codex) || q.Model != madeUpModel || q.Effort != "low" || q.Rules != spike.Prompt(spike.FormQuotes) ||
			!q.IgnoreUserConfig || q.CLI != "" || len(q.Input) == 0 || q.Tap != nil {
			t.Errorf("a request: host %s, effort %s, ignore %v", q.Host, q.Effort, q.IgnoreUserConfig)
		}
	}
	if first != n || repair == 0 || repair > n {
		t.Errorf("%d first and %d repair calls, of %d passes", first, repair, n)
	}
	if len(fake.homes) != 1 {
		t.Errorf("the calls ran in %d homes", len(fake.homes))
	}
	if left := leftIn(t, r.tmp); len(left) != 0 {
		t.Errorf("the run left %q in the temp folder", left)
	}
	if !strings.Contains(stdout, "on, over every script: fact_recall ") || !strings.Contains(stdout, "answers at effort low: ") {
		t.Errorf("the run's summary: %q", stdout)
	}
	live, err := results.Load(liveOut)
	if err != nil {
		t.Fatal(err)
	}
	cat, err := results.LoadCatalogue(filepath.Join(moduleRoot(t), "eval", "spike", "metrics.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := cat.Check(live); err != nil {
		t.Error(err)
	}
	commit := gitIn(t, r.repo, "rev-parse", "HEAD")
	for _, row := range live {
		if row.Model != "small" || row.Host != string(adapter.Codex) || row.HostVersion != "9.8.701" || row.Instrument != "spike" || row.Commit != commit {
			t.Fatalf("a row: %+v", row)
		}
	}

	spikeRunner = func(host string) update.Runner {
		t.Errorf("the replay made a runner for %s", host)
		return fake
	}
	before := len(fake.requests)
	replayOut := filepath.Join(dir, "replay")
	code, stdout2, errOut2 := spikeCmd("run", "--host", string(adapter.Codex), "--tier", "small", "--effort", "low", "--form", "quotes",
		"--out", replayOut, "--repo", r.repo, "--replay", rec)
	if code != 0 || len(fake.requests) != before {
		t.Fatalf("replay: exit %d, out %q, err %q", code, stdout2, errOut2)
	}
	replayed, err := results.Load(replayOut)
	if err != nil {
		t.Fatal(err)
	}
	if len(replayed) != len(live) || !slicesEqualRows(withoutTime(replayed), withoutTime(live)) {
		t.Error("the replay's rows differ from the run's apart from time")
	}
	recBytes, _ := os.ReadFile(rec)
	liveBytes, _ := os.ReadFile(filepath.Join(liveOut, results.FileName))
	replayBytes, _ := os.ReadFile(filepath.Join(replayOut, results.FileName))
	noModel(t, "a byte of the rows, the recording or the output", string(recBytes), string(liveBytes), string(replayBytes), stdout, errOut, stdout2, errOut2)
	if r.probes != 1 {
		t.Errorf("the version was read %d times; a replay reads it from the recording", r.probes)
	}
}

func slicesEqualRows(a, b []results.Row) bool {
	return slices.EqualFunc(a, b, func(x, y results.Row) bool { return x == y })
}

// A recording that lacks a line, or holds one too many, stops the replay
// with no row written and nothing called.
func TestReplayOfAWrongRecording(t *testing.T) {
	r := newSpikeRig(t)
	liveFake(t, r)
	dir := t.TempDir()
	rec := filepath.Join(dir, "rec.jsonl")
	if code, _, errOut := spikeCmd(liveArgs(r, string(adapter.ClaudeCode), filepath.Join(dir, "live"), "--spend-model-quota", "--record", rec)...); code != 0 {
		t.Fatalf("live: %d %q", code, errOut)
	}
	spikeRunner = func(host string) update.Runner {
		t.Errorf("the replay made a runner for %s", host)
		return nil
	}
	data, err := os.ReadFile(rec)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.SplitAfter(string(data), "\n")
	lines = lines[:len(lines)-1]
	for name, text := range map[string]string{
		"a line short":  strings.Join(lines[:len(lines)-1], ""),
		"one more line": strings.Join(lines, "") + strings.Replace(lines[0], `"round":1`, `"round":9`, 1),
		"another arm":   strings.ReplaceAll(strings.Join(lines, ""), `"tier":"small"`, `"tier":"large"`),
	} {
		t.Run(name, func(t *testing.T) {
			file := filepath.Join(t.TempDir(), "rec.jsonl")
			write(t, file, text)
			out := filepath.Join(t.TempDir(), "rows")
			code, stdout, errOut := spikeCmd("run", "--host", string(adapter.ClaudeCode), "--tier", "small", "--effort", "low", "--form", "quotes",
				"--out", out, "--repo", r.repo, "--replay", file)
			if code == 0 {
				t.Fatalf("exit 0, out %q, err %q", stdout, errOut)
			}
			if _, err := os.Stat(out); !errors.Is(err, fs.ErrNotExist) {
				t.Error("the replay wrote rows")
			}
		})
	}
}

// A fallback from the model asked for stops the run, whether the call
// succeeded or failed, and so does an answer the host says came from
// another model than the run's earlier ones: no row is written and the
// model's name is not printed.
func TestRunStopsOnAnotherModel(t *testing.T) {
	for name, fail := range map[string]func(n int, res *update.Result) error{
		"fallback": func(n int, res *update.Result) error {
			if n == 3 {
				res.Model, res.ModelNote = "", update.NoteFallback+madeUpModel
			}
			return nil
		},
		"fallback that failed": func(n int, res *update.Result) error {
			if n == 3 {
				res.Model, res.ModelNote, res.Reason, res.Output = "", update.NoteFallback+madeUpModel, update.ReasonTimeout, nil
			}
			return nil
		},
		"another reported": func(n int, res *update.Result) error {
			res.ModelReported = madeUpModel + "-20260101"
			if n == 4 {
				res.ModelReported = "zq-other-model"
			}
			return nil
		},
		"a runner error naming the model": func(n int, res *update.Result) error {
			if n == 2 {
				return errors.New("update: something about " + madeUpModel)
			}
			return nil
		},
	} {
		t.Run(name, func(t *testing.T) {
			r := newSpikeRig(t)
			fake := liveFake(t, r)
			fake.fail = fail
			dir := t.TempDir()
			rec := filepath.Join(dir, "rec.jsonl")
			out := filepath.Join(dir, "rows")
			code, stdout, errOut := spikeCmd(liveArgs(r, string(adapter.Codex), out, "--spend-model-quota", "--record", rec)...)
			if code != 1 || !strings.Contains(errOut, "no row was written") {
				t.Fatalf("exit %d, out %q, err %q", code, stdout, errOut)
			}
			if name != "a runner error naming the model" && !strings.Contains(errOut, "answered from another model") {
				t.Errorf("err %q", errOut)
			}
			if _, err := os.Stat(out); !errors.Is(err, fs.ErrNotExist) {
				t.Error("the stopped run wrote rows")
			}
			recBytes, _ := os.ReadFile(rec)
			noModel(t, "the output or the recording", stdout, errOut, string(recBytes))
			if left := leftIn(t, r.tmp); len(left) != 0 {
				t.Errorf("the run left %q", left)
			}
		})
	}
}

// capped refuses a call past the count the plan line said.
func TestCappedAsk(t *testing.T) {
	asked := 0
	ask := capped(func(context.Context, arm.Call) (arm.Reply, error) { asked++; return arm.Reply{}, nil }, 2)
	for i, kind := range []string{arm.KindFirst, arm.KindRepair, arm.KindFirst, arm.KindRepair} {
		if _, err := ask(context.Background(), arm.Call{Kind: kind}); err != nil {
			t.Fatalf("call %d: %v", i, err)
		}
	}
	if _, err := ask(context.Background(), arm.Call{Kind: arm.KindFirst}); err == nil || asked != 4 {
		t.Errorf("a third first call: %v, %d asked", err, asked)
	}
	if _, err := ask(context.Background(), arm.Call{Kind: arm.KindRepair}); err == nil || asked != 4 {
		t.Errorf("a third repair call: %v, %d asked", err, asked)
	}
}

// The rows a run checks before it calls cover every row an arm writes,
// and the catalogue lists exactly the metrics an arm writes, in their
// units, with the spike's two paths.
func TestEveryRowAndTheCatalogue(t *testing.T) {
	cat, err := results.LoadCatalogue(filepath.Join(moduleRoot(t), "eval", "spike", "metrics.json"))
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{}
	for _, m := range spikeMetrics() {
		want[m.name] = m.unit
	}
	got := map[string]string{}
	for _, m := range cat.Metrics {
		got[m.Name] = m.Unit
		if !slices.Equal(m.Paths, []string{"internal/eval/spike", "internal/eval/score"}) {
			t.Errorf("%s: paths %v", m.Name, m.Paths)
		}
	}
	if !maps.Equal(got, want) || len(cat.Metrics) != len(spikeMetrics()) {
		t.Errorf("the catalogue lists %d metrics, the arm writes %d", len(cat.Metrics), len(spikeMetrics()))
	}
	// A run whose replies report cached input and give repairs, so every
	// optional row is written.
	a := arm.Arm{Host: string(adapter.Codex), HostVersion: "1.2.3", Tier: "middle", Effort: "medium", Form: spike.FormQuotes}
	scripts := spike.Scripts()
	cached := 5
	ask := func(_ context.Context, c arm.Call) (arm.Reply, error) {
		if c.Kind == arm.KindRepair {
			return arm.Reply{Output: []byte(`{"fixes":[]}`), TokensIn: 10, CachedIn: &cached}, nil
		}
		return arm.Reply{Output: []byte(`{"ops":[{"op":"note","id":null,"kind":"did","text":"The user opened the chat.","admit":null,"ev":[{"e":1,"q":"words that no entry holds at all","as":"said"}]}],"nothing":null}`), TokensIn: 10, CachedIn: &cached}, nil
	}
	res, err := arm.Run(context.Background(), a, scripts, ask, nil)
	if err != nil {
		t.Fatal(err)
	}
	template := results.Row{Commit: strings.Repeat("ab", 20), Time: time0}
	rows := arm.Rows(res, template)
	if err := cat.Check(rows); err != nil {
		t.Error(err)
	}
	type id struct{ metric, unit, scenario string }
	all := map[id]results.Row{}
	for _, r := range everyRow(a, scripts, template) {
		all[id{r.Metric, r.Unit, r.Scenario}] = r
	}
	written := map[string]bool{}
	for _, r := range rows {
		k := id{r.Metric, r.Unit, r.Scenario}
		e, ok := all[k]
		e.Value = r.Value
		if !ok || e != r {
			t.Fatalf("a row written is not among those checked: %s %s", r.Scenario, r.Metric)
		}
		written[r.Metric] = true
	}
	for _, m := range []string{"spike.repair_tokens_cached_in", "spike.quote_miss_final", "spike.repair_yield"} {
		if !written[m] {
			t.Errorf("the run wrote no %s row", m)
		}
	}
}

// ---- the frontier ----

// trial is one trial of an arm's on.all rows, metric by metric.
type trial map[string]float64

// good is a trial that meets the bar, with tokens.
func good(tok float64) trial {
	return trial{"fidelity.fact_recall": 0.95, "fidelity.wrong_write": 0, "fidelity.misattributed": 0, "fidelity.stale_in_force": 1,
		"spike.ops_accepted_final": 50, "spike.quote_miss_final": 0.01, "spike.tokens_in": tok, "spike.tokens_out": 100,
		"spike.calls_first": 28, "spike.calls_repair": 2, "spike.seconds": 60, "spike.plants_refusal": 24, "spike.plants_hedge": 24,
		"spike.assent_cited_refusal": 0, "spike.accepted_from_refusal": 0, "spike.assent_cited_hedge": 0, "spike.accepted_from_hedge": 0,
		"spike.repair_yield": 0.5, "spike.repair_tokens_in": 300, "spike.schema_breaks_first": 0, "spike.schema_breaks_repair": 0,
		"spike.answers_invalid_first": 0, "spike.answers_invalid_repair": 0, "spike.calls_failed": 0}
}

// with is t with some metrics changed.
func (t trial) with(kv ...any) trial {
	out := maps.Clone(t)
	for i := 0; i < len(kv); i += 2 {
		out[kv[i].(string)] = kv[i+1].(float64)
	}
	return out
}

// armRows are the on.all rows of an arm's trials, and the off.all rows of
// the same figures.
func armRows(host, tier, effort string, form spike.Form, trials ...trial) []results.Row {
	units := map[string]string{}
	for _, m := range spikeMetrics() {
		units[m.name] = m.unit
	}
	var rows []results.Row
	for i, tr := range trials {
		for _, side := range []string{"on", "off"} {
			for _, m := range slices.Sorted(maps.Keys(tr)) {
				rows = append(rows, results.Row{Metric: m, Value: tr[m], Unit: units[m], Commit: strings.Repeat("c", 40), Instrument: "spike",
					Scenario: string(form) + "." + effort + "." + side + ".all", Host: host, HostVersion: "1.0.0", Model: tier, Seed: uint64(i), Time: time0})
			}
		}
	}
	return rows
}

// frontierOf runs the frontier on rows and returns what it printed.
func frontierOf(t *testing.T, rows ...[]results.Row) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "rows")
	if err := (results.Store{Dir: dir}).Append(slices.Concat(rows...)...); err != nil {
		t.Fatal(err)
	}
	code, out, errOut := spikeCmd("frontier", "--catalogue", filepath.Join(moduleRoot(t), "eval", "spike", "metrics.json"), dir)
	if code != 0 || errOut != "" {
		t.Fatalf("frontier: %d %q", code, errOut)
	}
	return out
}

// line is the line of out that starts with prefix, or "".
func line(out, prefix string) string {
	for _, l := range strings.Split(out, "\n") {
		if strings.HasPrefix(l, prefix) {
			return l
		}
	}
	return ""
}

var claudeID, codexID = string(adapter.ClaudeCode), string(adapter.Codex)

func TestFrontierASmallRowPasses(t *testing.T) {
	q, s := spike.FormQuotes, spike.FormSegments
	out := frontierOf(t,
		// On claude-code two small rows pass; the cheaper by tokens is the
		// medium one. A large row is cheaper still and is never the default.
		armRows(claudeID, "small", "low", q, good(9000), good(9000)),
		armRows(claudeID, "small", "medium", q, good(7000), good(8000)),
		armRows(claudeID, "large", "low", q, good(100)),
		armRows(claudeID, "middle", "low", q, good(5000)),
		armRows(claudeID, "small", "low", s, good(1000)),
		// On codex the cheapest small row misses fact_recall in its mean.
		armRows(codexID, "small", "low", q, good(1000).with("fidelity.fact_recall", 0.95), good(1000).with("fidelity.fact_recall", 0.80)),
		armRows(codexID, "small", "medium", q, good(4000)),
		armRows(codexID, "middle", "low", q, good(500)),
	)
	if l := line(out, "- **The evidence form:**"); !strings.HasPrefix(l, "- **The evidence form:** quotes, since quote_miss_final is at most 2%") {
		t.Errorf("form: %q", l)
	}
	if l := line(out, "- **The default row on claude-code:**"); !strings.Contains(l, "tier small, effort medium, form quotes, the cheapest small row that meets the bar") ||
		!strings.HasSuffix(l, "it meets the bar.") {
		t.Errorf("claude-code: %q", l)
	}
	if l := line(out, "- **The default row on codex:**"); !strings.Contains(l, "tier small, effort medium, form quotes") || !strings.HasSuffix(l, "it meets the bar.") {
		t.Errorf("codex: %q", l)
	}
	if l := line(out, "- **Assent on codex:**"); !strings.Contains(l, "no typed refusal or hedge was accepted as assent, of 24 and 24 shown, over 1 trials") {
		t.Errorf("assent: %q", l)
	}
	if l := line(out, "| claude-code | 1.0.0 | small | medium | quotes | 2 | 0.950 |"); l == "" {
		t.Errorf("no line for the two trials of claude-code small medium quotes:\n%s", out)
	}
	if !strings.Contains(out, "stale_in_force` is shown and is no bar") || !strings.Contains(out, "Repair on codex") {
		t.Error("the head or the repair lines are missing")
	}
	if l := line(out, "- **Repair on codex**"); !strings.Contains(l, "cached share (F-065) not measured") {
		t.Errorf("repair: %q", l)
	}
}

func TestFrontierOnlyAMiddleRowPasses(t *testing.T) {
	q := spike.FormQuotes
	out := frontierOf(t,
		armRows(codexID, "small", "low", q, good(1000).with("fidelity.misattributed", 1.0)),
		armRows(codexID, "small", "medium", q, good(1000).with("fidelity.wrong_write", 2.0)),
		armRows(codexID, "middle", "low", q, good(3000).with("fidelity.fact_recall", 0.85)),
		armRows(codexID, "middle", "medium", q, good(6000).with("spike.accepted_from_hedge", 1.0, "spike.assent_cited_hedge", 2.0)),
		armRows(codexID, "large", "low", q, good(10)),
	)
	l := line(out, "- **The default row on codex:**")
	if !strings.Contains(l, "tier middle, effort medium, form quotes, no small row meets the bar, so the middle tier's cheapest row that meets it") ||
		!strings.HasSuffix(l, "it meets the bar.") {
		t.Errorf("codex: %q", l)
	}
	if l := line(out, "- **Assent on codex:**"); !strings.Contains(l, "0 typed refusals and 1 typed hedges were accepted as assent") ||
		!strings.Contains(l, "a short typed yes does not count") {
		t.Errorf("assent: %q", l)
	}
	if l := line(out, "- **The evidence form:**"); !strings.HasPrefix(l, "- **The evidence form:** not read: ") || !strings.HasSuffix(l, "on claude-code.") {
		t.Errorf("a host with no rows: %q", l)
	}
	if l := line(out, "- **The default row on claude-code:**"); l != "- **The default row on claude-code:** none: no small or middle row to name." {
		t.Errorf("claude-code, with no rows: %q", l)
	}
	// The wrong_write share of 2 over 50 is 0.04, over the bar; one wrong
	// write over 50 is 0.02, at it.
	out = frontierOf(t, armRows(codexID, "small", "low", q, good(1000).with("fidelity.wrong_write", 1.0)))
	if l := line(out, "- **The default row on codex:**"); !strings.Contains(l, "tier small") || !strings.HasSuffix(l, "it meets the bar.") {
		t.Errorf("a share at the bar: %q", l)
	}
}

func TestFrontierNoRowPasses(t *testing.T) {
	q := spike.FormQuotes
	out := frontierOf(t,
		armRows(claudeID, "small", "low", q, good(1000).with("fidelity.fact_recall", 0.5)),
		armRows(claudeID, "middle", "low", q, good(4000).with("fidelity.fact_recall", 0.89)),
		armRows(claudeID, "middle", "medium", q, good(3000).with("fidelity.misattributed", 1.0, "spike.accepted_from_refusal", 2.0)),
		armRows(claudeID, "large", "low", q, good(2000)),
	)
	l := line(out, "- **The default row on claude-code:**")
	if !strings.Contains(l, "tier middle, effort medium, form quotes, no small row meets the bar, and no middle row does: the middle tier's cheapest row, which misses the bar") ||
		!strings.HasSuffix(l, "it misses the bar.") || strings.Contains(l, "meets the bar.") {
		t.Errorf("claude-code: %q", l)
	}
	if l := line(out, "- **Assent on claude-code:**"); !strings.Contains(l, "2 typed refusals and 0 typed hedges were accepted") {
		t.Errorf("assent: %q", l)
	}
	// With no middle row at all there is no default to name.
	out = frontierOf(t, armRows(claudeID, "small", "low", q, good(1000).with("fidelity.fact_recall", 0.5)), armRows(claudeID, "large", "low", q, good(1000)))
	if l := line(out, "- **The default row on claude-code:**"); l != "- **The default row on claude-code:** none: no small or middle row to name." {
		t.Errorf("no middle row: %q", l)
	}
}

// Quotes that miss more than 2% at the quotes form's default row on
// either host make segments the form, and the default rows are then read
// among the segment rows.
func TestFrontierSegments(t *testing.T) {
	q, s := spike.FormQuotes, spike.FormSegments
	rows := [][]results.Row{
		armRows(claudeID, "small", "low", q, good(1000)),
		armRows(claudeID, "small", "low", s, good(2000)),
		armRows(codexID, "small", "low", q, good(1000).with("spike.quote_miss_final", 0.03)),
		armRows(codexID, "small", "low", s, good(1500).with("spike.accepted_from_refusal", 1.0)),
		armRows(codexID, "middle", "low", s, good(900)),
	}
	out := frontierOf(t, rows...)
	if l := line(out, "- **The evidence form:**"); !strings.HasPrefix(l, "- **The evidence form:** segments, since quote_miss_final is over 2% at the quotes form's default row on codex (") {
		t.Errorf("form: %q", l)
	}
	if l := line(out, "- **The default row on claude-code:**"); !strings.Contains(l, "tier small, effort low, form segments") {
		t.Errorf("claude-code: %q", l)
	}
	if l := line(out, "- **The default row on codex:**"); !strings.Contains(l, "tier small, effort low, form segments") {
		t.Errorf("codex: %q", l)
	}
	if l := line(out, "- **Assent on codex:**"); !strings.Contains(l, "a short typed yes does not count") {
		t.Errorf("assent at the segment row: %q", l)
	}
	// At 2% exactly the quotes stay.
	rows[2] = armRows(codexID, "small", "low", q, good(1000).with("spike.quote_miss_final", 0.02))
	if l := line(frontierOf(t, rows...), "- **The evidence form:**"); !strings.HasPrefix(l, "- **The evidence form:** quotes") {
		t.Errorf("at 2%%: %q", l)
	}
	// With no quotes row on a host the form is not read.
	out = frontierOf(t, rows[0], rows[3])
	if l := line(out, "- **The evidence form:**"); !strings.HasPrefix(l, "- **The evidence form:** not read: ") || !strings.Contains(l, "on codex") {
		t.Errorf("unread: %q", l)
	}
}

// Trials are averaged: two trials of 0.85 and 0.95 give 0.90, which meets
// the bar; and the repair figures read cached input only when every trial
// with repair input reported it.
func TestFrontierAveragesTrials(t *testing.T) {
	q := spike.FormQuotes
	out := frontierOf(t,
		armRows(codexID, "small", "low", q, good(1000).with("fidelity.fact_recall", 0.85, "spike.repair_tokens_cached_in", 240.0),
			good(1000).with("fidelity.fact_recall", 0.95, "spike.repair_tokens_cached_in", 210.0)))
	l := line(out, "- **The default row on codex:**")
	if !strings.Contains(l, "fact_recall 0.900") || !strings.HasSuffix(l, "it meets the bar.") {
		t.Errorf("default: %q", l)
	}
	if l := line(out, "- **Repair on codex**"); !strings.Contains(l, "cached share (F-065) 0.750 of the repair calls' input") || !strings.Contains(l, "yield 0.500") {
		t.Errorf("repair: %q", l)
	}
	out = frontierOf(t,
		armRows(codexID, "small", "low", q, good(1000).with("spike.repair_tokens_cached_in", 240.0), good(1000)))
	if l := line(out, "- **Repair on codex**"); !strings.Contains(l, "not read: reported in 1 of the 2 trials with repair input") {
		t.Errorf("repair, cached in one trial: %q", l)
	}
}

func TestFrontierRefusals(t *testing.T) {
	cat := filepath.Join(moduleRoot(t), "eval", "spike", "metrics.json")
	store := func(rows ...results.Row) string {
		dir := filepath.Join(t.TempDir(), "rows")
		if err := (results.Store{Dir: dir}).Append(rows...); err != nil {
			t.Fatal(err)
		}
		return dir
	}
	named := armRows(codexID, "small", "low", spike.FormQuotes, good(1))
	named[0].Model = "zq-made-up-name"
	twoVersions := slices.Concat(armRows(codexID, "small", "low", spike.FormQuotes, good(1)), armRows(codexID, "small", "low", spike.FormQuotes, good(1)))
	twoVersions[0].HostVersion = "2.0.0"
	other := armRows(codexID, "small", "low", spike.FormQuotes, good(1))
	other[0].Metric = "hook.capture_p95_ms"
	scriptOnly := armRows(codexID, "small", "low", spike.FormQuotes, good(1))
	for i := range scriptOnly {
		scriptOnly[i].Scenario = strings.TrimSuffix(scriptOnly[i].Scenario, "all") + "v1.01"
	}
	noHost := armRows(codexID, "small", "low", spike.FormQuotes, good(1))
	for i := range noHost {
		noHost[i].Host, noHost[i].HostVersion = "", ""
	}
	for name, args := range map[string][]string{
		"no host":            {"frontier", "--catalogue", cat, store(noHost...)},
		"no rows":            {"frontier", "--catalogue", cat},
		"a model's name":     {"frontier", "--catalogue", cat, store(named...)},
		"two host versions":  {"frontier", "--catalogue", cat, store(twoVersions...)},
		"not in the catalog": {"frontier", "--catalogue", cat, store(other...)},
		"no all rows":        {"frontier", "--catalogue", cat, store(scriptOnly...)},
		"no catalogue":       {"frontier", "--catalogue", filepath.Join(t.TempDir(), "none.json"), store(armRows(codexID, "small", "low", spike.FormQuotes, good(1))...)},
	} {
		code, out, errOut := spikeCmd(args...)
		if code != 2 || out != "" {
			t.Errorf("%s: exit %d, out %q, err %q", name, code, out, errOut)
		}
		if strings.Contains(errOut, "zq-made-up-name") {
			t.Errorf("%s: the error names the model", name)
		}
	}
}
