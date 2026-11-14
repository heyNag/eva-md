package main

// eva-eval spike is the contract spike's instrument (plan section 8, Stage
// 0, row DV-03; card DV-03d). It has three commands:
//
//	eva-eval spike plan [--repeat n] [--efforts low,medium] [--output-allowance n] [--overhead host=n,...] --out file
//	eva-eval spike run --host h --tier t --effort e --form f --out dir [--model name] [--repeat n]
//	    [--record file | --replay file] [--spend-model-quota] [--cli path] [--codex-ignore-user-config] [--repo dir]
//	eva-eval spike frontier [--catalogue file] rows...
//
// plan writes, for the lead, the calls and tokens of every arm of the live
// run; eval/spike/run-plan.md is its file at the defaults. run runs one arm
// (internal/eval/spike/arm) over the spike's scripts and appends its rows
// to a results folder: live, each call goes to the host's CLI through v1's
// update runner, and only with --spend-model-quota, as the live accuracy
// run asks; or from a recording, calling nothing. frontier reads the arms'
// rows and prints the table the lead writes the decision record from: the
// default row, the evidence form, whether a typed refusal was taken for
// assent, and what repair yields and costs.
//
// The model's name comes only from --model: it is written to no file and
// printed nowhere. Rows carry the tier, and the run's Forbid holds the
// name, so a row that held it would not be written; the run checks every
// row it could write for it before it calls anything.
//
// Like the spike it measures, this file is a throwaway: it is deleted with
// internal/eval/spike when DV-21 lands. No release or install builds it.

import (
	"cmp"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/heyNag/eva-md/internal/eval/results"
	"github.com/heyNag/eva-md/internal/eval/scenario"
	"github.com/heyNag/eva-md/internal/eval/score"
	"github.com/heyNag/eva-md/internal/eval/spike"
	"github.com/heyNag/eva-md/internal/eval/spike/arm"
	"github.com/heyNag/eva-md/internal/host/adapter"
	"github.com/heyNag/eva-md/internal/update"
)

func init() {
	register(instrument{
		name: "spike",
		synopsis: "spike plan [--repeat n] [--efforts list] [--output-allowance n] [--overhead host=n,...] --out file" +
			" | spike run --host h --tier t --effort e --form f --out dir [--model name] [--repeat n] [--record file | --replay file]" +
			" [--spend-model-quota] [--cli path] [--codex-ignore-user-config] [--repo dir] | spike frontier [--catalogue file] rows...",
		run: spikeCommand,
	})
}

// What a live run reaches the machine through. Tests replace them: the
// runner with a fake, and the probe's run of <cli> --version with one that
// starts nothing. A nil probeRun runs the CLI, as the adapter does.
var (
	spikeRunner   = update.NewRunner
	spikeProbeRun func(ctx context.Context, cli string, args ...string) ([]byte, error)
)

// The plan's defaults, and the row's allowance (plan section 8, row DV-03).
const (
	planAllowance      = 2000
	rowAllowanceCalls  = 1900
	rowAllowanceTokens = 25_000_000
	maxRepeat          = 100
)

// planEfforts are the efforts the plan takes by default: the two of the row.
var planEfforts = []string{"low", "medium"}

// The bar of design 8.3, and the evidence form's switch of design 5.4.
const (
	barRecall     = 0.90
	barWrongWrite = 0.02
	barQuoteMiss  = 0.02
	epsilon       = 1e-9
)

func spikeCommand(ctx context.Context, args []string, out, errOut io.Writer) int {
	if len(args) > 0 {
		switch args[0] {
		case "plan":
			return spikePlanCommand(args[1:], out, errOut)
		case "run":
			return spikeRunCommand(ctx, args[1:], out, errOut)
		case "frontier":
			return spikeFrontierCommand(args[1:], out, errOut)
		}
	}
	return usage(errOut, "spike takes plan, run or frontier")
}

// hostIDs are the ids of every host eva-md knows, in its order.
func hostIDs() []string {
	var ids []string
	for _, h := range adapter.All() {
		ids = append(ids, string(h.ID))
	}
	return ids
}

// passes is the number of passes of every script.
func passes(scripts []spike.Script) int {
	n := 0
	for _, s := range scripts {
		n += len(s.Scenario.Passes)
	}
	return n
}

// ---- plan ----

// planOptions are what a plan is made of.
type planOptions struct {
	repeat    int
	efforts   []string
	allowance int
	overhead  map[string]int
}

// planScript is one script's first calls and their input tokens, in each
// form, overhead left out.
type planScript struct {
	id     string
	passes int
	in     map[spike.Form]int
}

// planArm is one arm's calls and tokens, repair counted in full.
type planArm struct {
	host, tier, effort string
	form               spike.Form
	first, repair      int
	in, out            int
}

// plan is a run's plan.
type plan struct {
	opts    planOptions
	scripts []planScript
	arms    []planArm
}

// tokens is n bytes as tokens: a quarter, rounded up.
func tokens(n int) int { return (n + 3) / 4 }

// makePlan works out every arm's calls and tokens. A first call's input is
// the bytes of the prompt, the pass rendered with nothing shown and the
// schema, as tokens, plus the host's overhead; a repair call's adds the
// output allowance; each call's output is the allowance.
func makePlan(opts planOptions) (plan, error) {
	p := plan{opts: opts}
	for _, s := range spike.Scripts() {
		ps := planScript{id: s.Scenario.ID, passes: len(s.Scenario.Passes), in: map[spike.Form]int{}}
		for _, f := range spike.Forms {
			fixed := len(spike.Prompt(f)) + len(spike.Schema(f))
			for _, pass := range s.Scenario.Passes {
				in, err := spike.Render(s.Scenario.Sessions[pass.Session], nil, f)
				if err != nil {
					return plan{}, fmt.Errorf("script %s: %w", s.Scenario.ID, err)
				}
				ps.in[f] += tokens(fixed + len(in.Bytes))
			}
		}
		p.scripts = append(p.scripts, ps)
	}
	for _, host := range hostIDs() {
		for _, tier := range arm.Tiers {
			for _, effort := range opts.efforts {
				for _, f := range spike.Forms {
					a := planArm{host: host, tier: tier, effort: effort, form: f}
					for _, s := range p.scripts {
						n := s.passes * opts.repeat
						a.first += n
						a.repair += n
						first := s.in[f]*opts.repeat + n*opts.overhead[host]
						a.in += first + first + n*opts.allowance
						a.out += 2 * n * opts.allowance
					}
					p.arms = append(p.arms, a)
				}
			}
		}
	}
	return p, nil
}

// num writes n with a comma between each three digits.
func num(n int) string {
	s := strconv.Itoa(n)
	var b strings.Builder
	for i, c := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(c)
	}
	return b.String()
}

// side says whether n is under, at or over the allowance.
func side(n, allowance int) string {
	switch {
	case n < allowance:
		return "under it"
	case n > allowance:
		return "over it"
	}
	return "at it"
}

// render writes the plan as Markdown.
func (p plan) render() string {
	var b strings.Builder
	w := func(format string, args ...any) { fmt.Fprintf(&b, format, args...) }
	o := p.opts
	hosts := hostIDs()
	var forms []string
	for _, f := range spike.Forms {
		forms = append(forms, string(f))
	}
	var overhead []string
	for _, h := range hosts {
		overhead = append(overhead, fmt.Sprintf("%s %s", h, num(o.overhead[h])))
	}
	w("# The contract spike's run plan\n\n")
	w("The calls and tokens of every arm of the contract spike's live run (plan section 8, Stage 0, row DV-03), as `eva-eval spike plan` works them out before anything is called. `eval/spike/run-plan.md` is this plan at the defaults. Each arm is run by `eva-eval spike run`, which calls nothing without `--spend-model-quota`.\n\n")
	w("## Rules and defaults\n\n")
	w("- **Arms.** Every host eva-md knows (%s), every tier (%s), every effort asked for (%s; `--efforts`, default %s) and both forms of the contract (%s): %d arms. The tier is all a row says of its model.\n",
		strings.Join(hosts, ", "), strings.Join(arm.Tiers, ", "), strings.Join(o.efforts, ", "), strings.Join(planEfforts, ","), strings.Join(forms, ", "), len(p.arms))
	w("- **Calls.** An arm makes one first call for each pass of the spike's %d scripts, %d passes, in each round, and runs %d round%s (`--repeat`, default 1). It makes at most one repair call for each first call, and this plan counts every one.\n",
		len(p.scripts), passes(spike.Scripts()), o.repeat, plural(o.repeat, "", "s"))
	w("- **Tokens.** A first call's input is the bytes of the prompt, of the pass rendered with nothing shown and of the answer's schema, divided by 4 and rounded up, plus the host's overhead per call. A repair call's input adds the output allowance to its first call's. Each call's output is the output allowance, %s tokens (`--output-allowance`, default %s). Tokens are input and output together.\n",
		num(o.allowance), num(planAllowance))
	w("- **Overhead.** The host's own input per call: %s (`--overhead host=n,...`, default 0). It is unknown until DV-02's recordings show what each host adds to a call.\n", strings.Join(overhead, ", "))
	w("- **Spoken scripts.** They are not counted: they wait on the wrapper HF-00 records, and the row allows about a tenth more for them.\n")
	w("- **The row's allowance** is about %s calls and %s tokens over both hosts (plan section 8, row DV-03). Where the run happens, and its price, are the lead's to state; the run is the owner's to allow.\n\n",
		num(rowAllowanceCalls), num(rowAllowanceTokens))
	w("## Scripts\n\n")
	w("| Script | Passes | First calls' input tokens, %s | First calls' input tokens, %s |\n| --- | --- | --- | --- |\n", forms[0], forms[1])
	for _, s := range p.scripts {
		w("| %s | %d | %s | %s |\n", s.id, s.passes, num(s.in[spike.Forms[0]]), num(s.in[spike.Forms[1]]))
	}
	w("\nOverhead is left out here; the arms add it.\n\n")
	w("## Arms\n\n")
	w("| Host | Tier | Effort | Form | First calls | Repair calls, at most | Tokens in | Tokens out | Tokens |\n| --- | --- | --- | --- | --- | --- | --- | --- | --- |\n")
	for _, a := range p.arms {
		w("| %s | %s | %s | %s | %s | %s | %s | %s | %s |\n", a.host, a.tier, a.effort, a.form, num(a.first), num(a.repair), num(a.in), num(a.out), num(a.in+a.out))
	}
	w("\n## Totals\n\n")
	w("| Host | Arms | Calls, at most | Tokens in | Tokens out | Tokens |\n| --- | --- | --- | --- | --- | --- |\n")
	var all planArm
	arms := 0
	for _, h := range hosts {
		var t planArm
		n := 0
		for _, a := range p.arms {
			if a.host == h {
				n++
				t.first, t.repair, t.in, t.out = t.first+a.first, t.repair+a.repair, t.in+a.in, t.out+a.out
			}
		}
		w("| %s | %d | %s | %s | %s | %s |\n", h, n, num(t.first+t.repair), num(t.in), num(t.out), num(t.in+t.out))
		arms += n
		all.first, all.repair, all.in, all.out = all.first+t.first, all.repair+t.repair, all.in+t.in, all.out+t.out
	}
	calls, toks := all.first+all.repair, all.in+all.out
	w("| all | %d | %s | %s | %s | %s |\n\n", arms, num(calls), num(all.in), num(all.out), num(toks))
	w("Against the row's allowance: %s calls at most, %s, the row's being about %s; %s tokens at most, %s, the row's being about %s.\n",
		num(calls), side(calls, rowAllowanceCalls), num(rowAllowanceCalls), num(toks), side(toks, rowAllowanceTokens), num(rowAllowanceTokens))
	return b.String()
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

// parseOverhead reads host=n,... for hosts eva-md knows, each once.
func parseOverhead(s string) (map[string]int, error) {
	m := map[string]int{}
	if s == "" {
		return m, nil
	}
	for _, part := range strings.Split(s, ",") {
		host, n, ok := strings.Cut(part, "=")
		v, err := strconv.Atoi(n)
		switch {
		case !ok || err != nil || v < 0 || v > 1_000_000:
			return nil, errors.New("--overhead takes host=n,..., n a whole number from 0 to 1000000")
		case !slices.Contains(hostIDs(), host):
			return nil, fmt.Errorf("--overhead names a host that is not one of %s", strings.Join(hostIDs(), ", "))
		}
		if _, dup := m[host]; dup {
			return nil, errors.New("--overhead names a host twice")
		}
		m[host] = v
	}
	return m, nil
}

// parseEfforts reads a list of efforts that every host's runner takes, each
// once.
func parseEfforts(s string) ([]string, error) {
	var efforts []string
	for _, e := range strings.Split(s, ",") {
		if !slices.Contains(arm.Efforts, e) || slices.Contains(efforts, e) {
			return nil, fmt.Errorf("--efforts takes efforts of %s, each once", strings.Join(arm.Efforts, ", "))
		}
		for _, h := range hostIDs() {
			if update.CheckFlags(h, e, "", "") != nil {
				return nil, fmt.Errorf("--efforts: %s does not take the effort %s", h, e)
			}
		}
		efforts = append(efforts, e)
	}
	return efforts, nil
}

func spikePlanCommand(args []string, out, errOut io.Writer) int {
	f := flag.NewFlagSet("eva-eval spike plan", flag.ContinueOnError)
	f.SetOutput(errOut)
	repeat := f.Int("repeat", 1, "the rounds each arm runs")
	efforts := f.String("efforts", strings.Join(planEfforts, ","), "the efforts, each one every host takes")
	allowance := f.Int("output-allowance", planAllowance, "each call's output tokens, which a repair call's input adds")
	overhead := f.String("overhead", "", "each host's own input tokens per call, as host=n,...; 0 when not given")
	outFile := f.String("out", "", "the plan file to write")
	if err := f.Parse(args); err != nil {
		return 2
	}
	switch {
	case f.NArg() != 0:
		return usage(errOut, "spike plan takes no argument but its flags")
	case *outFile == "":
		return usage(errOut, "spike plan needs --out")
	case *repeat < 1 || *repeat > maxRepeat:
		return usage(errOut, fmt.Sprintf("--repeat must be from 1 to %d", maxRepeat))
	case *allowance < 1 || *allowance > 1_000_000:
		return usage(errOut, "--output-allowance must be from 1 to 1000000")
	}
	eff, err := parseEfforts(*efforts)
	if err != nil {
		return usage(errOut, err.Error())
	}
	oh, err := parseOverhead(*overhead)
	if err != nil {
		return usage(errOut, err.Error())
	}
	p, err := makePlan(planOptions{repeat: *repeat, efforts: eff, allowance: *allowance, overhead: oh})
	if err != nil {
		return fail(errOut, err)
	}
	if err := os.WriteFile(*outFile, []byte(p.render()), 0o644); err != nil {
		return fail(errOut, err)
	}
	fmt.Fprintf(out, "Wrote the plan of %d arms to %s.\n", len(p.arms), *outFile)
	return 0
}

// ---- run ----

// spikeMetric is a metric an arm writes, and its unit.
type spikeMetric struct{ name, unit string }

// spikeMetrics are every metric an arm can write (DV-03c's rows), in the
// order Rows writes them; eval/spike/metrics.json catalogues the same.
func spikeMetrics() []spikeMetric {
	var ms []spikeMetric
	for _, id := range spike.CountIDs {
		ms = append(ms, spikeMetric{"spike." + id, "count"})
	}
	for _, id := range spike.RatioIDs {
		ms = append(ms, spikeMetric{"spike." + id, "ratio"})
	}
	ms = append(ms,
		spikeMetric{"spike.calls_first", "count"}, spikeMetric{"spike.calls_repair", "count"}, spikeMetric{"spike.calls_failed", "count"},
		spikeMetric{"spike.tokens_in", "tokens"}, spikeMetric{"spike.tokens_out", "tokens"}, spikeMetric{"spike.repair_tokens_in", "tokens"},
		spikeMetric{"spike.repair_tokens_cached_in", "tokens"}, spikeMetric{"spike.seconds", "seconds"}, spikeMetric{"spike.honest_refused", "ratio"})
	for _, r := range (score.Result{}).Rows(results.Row{}) {
		ms = append(ms, spikeMetric{r.Metric, r.Unit})
	}
	return ms
}

// everyRow is every row a run of a could write from template, every
// optional one included, so a word the run forbids can be looked for in
// each before anything is called.
func everyRow(a arm.Arm, scripts []spike.Script, template results.Row) []results.Row {
	base := template
	base.Instrument, base.Model, base.Host, base.HostVersion = arm.Instrument, a.Tier, a.Host, a.HostVersion
	var rows []results.Row
	for _, sideName := range []string{"on", "off"} {
		ids := []string{}
		for _, s := range scripts {
			ids = append(ids, s.Scenario.ID)
		}
		for _, id := range append(ids, "all") {
			for _, m := range spikeMetrics() {
				r := base
				r.Metric, r.Unit, r.Scenario = m.name, m.unit, string(a.Form)+"."+a.Effort+"."+sideName+"."+id
				rows = append(rows, r)
			}
		}
	}
	return rows
}

// hide puts a word in place of the model's name wherever s holds it, so no
// error printed can carry it.
func hide(s, model string) string {
	if model == "" {
		return s
	}
	return strings.ReplaceAll(s, model, "(the model asked for)")
}

// errStopped is the stop of a run whose host answered from another model
// than the one asked for.
var errStopped = errors.New("the host answered from another model than the one asked for, so the run stopped: its rows would be judged as the model's")

// spikeCaller asks the host's CLI through v1's update runner, each call in
// an empty run folder of the run's temporary home.
type spikeCaller struct {
	host, effort, model, cli string
	ignore                   bool
	home                     string
	runner                   update.Runner
	reported                 string // the model the host's output first said answered, if it said
}

func (c *spikeCaller) ask(ctx context.Context, call arm.Call) (arm.Reply, error) {
	dir, err := os.MkdirTemp(c.home, "run-")
	if err != nil {
		return arm.Reply{}, errors.New("could not make a run folder")
	}
	defer os.RemoveAll(dir)
	req := update.Request{Host: c.host, Model: c.model, Effort: c.effort, Rules: call.System, Input: call.Input,
		Schema: call.Schema, RunDir: dir, CLI: c.cli, IgnoreUserConfig: c.ignore}
	start := time.Now()
	res, err := c.runner.Run(ctx, req)
	if err != nil {
		return arm.Reply{}, err
	}
	elapsed := time.Since(start).Seconds()
	// A fallback to the host's default model, or an answer the host says
	// came from another model than the run's earlier answers, stops the
	// run, whether the call succeeded or failed.
	if res.Model != c.model || strings.HasPrefix(res.ModelNote, update.NoteFallback) {
		return arm.Reply{}, errStopped
	}
	if res.ModelReported != "" {
		if c.reported == "" {
			c.reported = res.ModelReported
		} else if res.ModelReported != c.reported {
			return arm.Reply{}, errStopped
		}
	}
	reply := arm.Reply{Reason: res.Reason, TokensIn: res.TokensIn, TokensOut: res.TokensOut, Attempts: res.Attempts,
		Seconds: elapsed, Effort: res.Effort}
	if res.Reason == "" {
		reply.Output = res.Output
	}
	return reply, nil
}

// capped is ask, which refuses a first call past first and a repair call
// past as many: the most the run's plan line said it would make.
func capped(ask arm.Ask, first int) arm.Ask {
	made := map[string]int{}
	return func(ctx context.Context, c arm.Call) (arm.Reply, error) {
		made[c.Kind]++
		if made[c.Kind] > first {
			return arm.Reply{}, fmt.Errorf("the run asked for more %s calls than the %d it said it would make", c.Kind, first)
		}
		return ask(ctx, c)
	}
}

// counting is ask, counting the answers at each effort the host used.
func counting(ask arm.Ask, efforts map[string]int) arm.Ask {
	return func(ctx context.Context, c arm.Call) (arm.Reply, error) {
		r, err := ask(ctx, c)
		if err == nil && r.Reason == "" {
			efforts[r.Effort]++
		}
		return r, err
	}
}

// checkResults refuses a results folder whose rows file the store could
// not append to, so a live run cannot end with rows it cannot keep.
func checkResults(dir string) error {
	info, err := os.Stat(dir)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return nil
	case err != nil:
		return err
	case !info.IsDir():
		return errors.New("--out must name a results folder")
	}
	if _, err := os.Stat(filepath.Join(dir, results.FileName)); errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	_, err = results.Load(dir)
	return err
}

func spikeRunCommand(ctx context.Context, args []string, out, errOut io.Writer) int {
	f := flag.NewFlagSet("eva-eval spike run", flag.ContinueOnError)
	f.SetOutput(errOut)
	host := f.String("host", "", "the host: "+strings.Join(hostIDs(), " or "))
	tier := f.String("tier", "", "the model's tier: "+strings.Join(arm.Tiers, ", "))
	effort := f.String("effort", "", "the effort each call asks for")
	form := f.String("form", "", "the contract's form: quotes or segments")
	outDir := f.String("out", "", "the results folder the rows are appended to")
	model := f.String("model", "", "the model each live call asks for; written nowhere")
	repeat := f.Int("repeat", 1, "the rounds the arm runs")
	record := f.String("record", "", "write each call's answer to this file, which must not exist yet, as the run goes")
	replay := f.String("replay", "", "answer each call from this recording; calls nothing")
	spend := f.Bool("spend-model-quota", false, "make the arm's model calls, on the host's subscription; without it, nothing is called")
	cli := f.String("cli", "", "the host CLI's absolute path; looked up on PATH when empty")
	ignore := f.Bool("codex-ignore-user-config", false, "run codex without your Codex config")
	repo := f.String("repo", ".", "the repository measured, whose commit the rows name")
	if err := f.Parse(args); err != nil {
		return 2
	}
	live := *replay == ""
	// Every message from here may be read beside the model's name: hide it.
	refuse := func(msg string) int { return usage(errOut, hide(msg, *model)) }
	switch {
	case f.NArg() != 0:
		return refuse("spike run takes no argument but its flags")
	case !slices.Contains(hostIDs(), *host):
		return refuse("--host must be " + strings.Join(hostIDs(), " or "))
	case !slices.Contains(arm.Tiers, *tier):
		return refuse("--tier must be " + strings.Join(arm.Tiers, ", "))
	case !slices.Contains(arm.Efforts, *effort):
		return refuse("--effort must be one of " + strings.Join(arm.Efforts, ", "))
	case !spike.Form(*form).Valid():
		return refuse("--form must be quotes or segments")
	case *outDir == "":
		return refuse("spike run needs --out")
	case *repeat < 1 || *repeat > maxRepeat:
		return refuse(fmt.Sprintf("--repeat must be from 1 to %d", maxRepeat))
	case *record != "" && !live:
		return refuse("--record and --replay can't be used together")
	case !live && (*model != "" || *spend || *cli != "" || *ignore):
		return refuse("--replay calls nothing, so it takes no --model, --spend-model-quota, --cli or --codex-ignore-user-config")
	case *ignore && *host != string(adapter.Codex):
		return refuse("--codex-ignore-user-config is for " + string(adapter.Codex) + " only")
	case live && *model == "":
		return refuse("a live run needs --model; the rows name only the tier")
	}
	if live {
		if err := update.CheckFlags(*host, *effort, *model, *cli); err != nil {
			return refuse(strings.TrimPrefix(err.Error(), "update: "))
		}
	}
	scripts := spike.Scripts()
	a := arm.Arm{Host: *host, Tier: *tier, Effort: *effort, Form: spike.Form(*form), Repeat: *repeat}
	first := passes(scripts) * *repeat
	commit, dirty, err := results.Source(ctx, *repo)
	if err != nil {
		return fail(errOut, err)
	}
	if err := checkResults(*outDir); err != nil {
		return refuse("--out: " + err.Error())
	}
	var forbid []string
	for _, s := range scripts {
		forbid = append(forbid, scenario.Forbid(s.Scenario)...)
	}
	var ask arm.Ask
	if !live {
		file, err := os.Open(*replay)
		if err != nil {
			return refuse("--replay: " + err.Error())
		}
		var version string
		ask, version, err = arm.Replay(file, a)
		file.Close()
		if err != nil {
			fmt.Fprintf(errOut, "eva-eval: the recording is refused: %v. Nothing was called.\n", err)
			return 2
		}
		a.HostVersion = version
	} else {
		lookPath := exec.LookPath
		if *cli != "" {
			lookPath = func(string) (string, error) { return *cli, nil }
		}
		probe := adapter.Probe(ctx, adapter.ID(*host), adapter.Env{LookPath: lookPath, Run: spikeProbeRun})
		if probe.Version == "" {
			fmt.Fprintf(errOut, "eva-eval: could not read %s's version, which every row names, so the run is refused. Nothing was called.\n", *host)
			return 2
		}
		a.HostVersion = probe.Version
		forbid = append(forbid, *model)
	}
	template := results.Row{Commit: commit, Dirty: dirty, Seed: 0, Time: time.Now()}
	if err := results.Forbidden(everyRow(a, scripts, template), forbid); err != nil {
		fmt.Fprintf(errOut, "eva-eval: a row the run would write holds the model's name, or a word a script plants, so the run is refused (%s). Nothing was called.\n", err)
		return 2
	}
	fmt.Fprintf(out, "The arm %s %s, tier %s, effort %s, form %s makes %d first calls, one for each of the %d passes of the spike's %d scripts in each of %d round%s, and at most %d repair calls: %d calls at most.\n",
		a.Host, a.HostVersion, a.Tier, a.Effort, a.Form, first, passes(scripts), len(scripts), *repeat, plural(*repeat, "", "s"), first, 2*first)
	if live && !*spend {
		fmt.Fprintf(errOut, "eva-eval: the run's calls are made on %s's subscription. Nothing was called; run it again with --spend-model-quota to make them.\n", a.Host)
		return 2
	}
	var rec *os.File
	if *record != "" {
		if rec, err = os.OpenFile(*record, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644); err != nil {
			return refuse("--record must name a file that does not exist yet, in a folder that does")
		}
	}
	code := func() int {
		if live {
			home, err := os.MkdirTemp("", "eva-eval-spike-")
			if err != nil {
				fmt.Fprintln(errOut, "eva-eval: could not make the run's temporary home")
				return 1
			}
			defer os.RemoveAll(home)
			c := &spikeCaller{host: *host, effort: *effort, model: *model, cli: *cli, ignore: *ignore, home: home, runner: spikeRunner(*host)}
			ask = c.ask
			fmt.Fprintf(errOut, "eva-eval: asking %s; nothing is kept of the run but its rows and the recording\n", a.Host)
		} else {
			fmt.Fprintln(errOut, "eva-eval: replaying the recording; nothing is called")
		}
		efforts := map[string]int{}
		ask = counting(capped(ask, first), efforts)
		var w io.Writer
		if rec != nil {
			w = rec
		}
		res, err := arm.Run(ctx, a, scripts, ask, w)
		if err != nil {
			fmt.Fprintf(errOut, "eva-eval: %s; no row was written\n", hide(err.Error(), *model))
			return 1
		}
		rows := arm.Rows(res, template)
		if err := (results.Store{Dir: *outDir, Forbid: forbid}).Append(rows...); err != nil {
			fmt.Fprintf(errOut, "eva-eval: %s\n", hide(err.Error(), *model))
			return 1
		}
		summarizeRun(out, rows, efforts)
		fmt.Fprintf(out, "Wrote %d rows of commit %s to the results folder %s.\n", len(rows), commit[:12], *outDir)
		return 0
	}()
	if rec != nil {
		info, statErr := rec.Stat()
		if err := rec.Close(); err != nil {
			fmt.Fprintln(errOut, "eva-eval: could not close the recording")
			code = 1
		}
		// A run that stopped before its first call leaves no empty
		// recording behind.
		if code != 0 && statErr == nil && info.Size() == 0 {
			os.Remove(*record)
		}
	}
	return code
}

// summarizeRun prints the run's figures over all scripts, each side's mean
// over its rounds, and the efforts the host answered at.
func summarizeRun(out io.Writer, rows []results.Row, efforts map[string]int) {
	arms, err := gather(rows)
	if err != nil || len(arms) != 1 {
		return
	}
	for _, sideName := range []string{"on", "off"} {
		fs := arms[0].sides[sideName]
		fmt.Fprintf(out, "%s, over every script: fact_recall %s, wrong_write %s, misattributed %s, quote_miss_final %s, calls %s first, %s repair, %s failed\n",
			sideName, fs.show("fidelity.fact_recall", "%.3f"), fs.show("fidelity.wrong_write", "%.2f"), fs.show("fidelity.misattributed", "%.2f"),
			fs.show("spike.quote_miss_final", "%.3f"), fs.show("spike.calls_first", "%.0f"), fs.show("spike.calls_repair", "%.0f"), fs.show("spike.calls_failed", "%.0f"))
	}
	used := slices.Sorted(maps.Keys(efforts))
	for _, e := range used {
		name := "effort " + e
		if e == "" {
			name = "no effort flag"
		}
		fmt.Fprintf(out, "answers at %s: %d\n", name, efforts[e])
	}
}

// ---- frontier ----

// armFigures are one side of an arm's all-scripts rows: each metric's values,
// one a trial.
type armFigures map[string][]float64

// sum is the sum of a metric's values, and whether there is one.
func (f armFigures) sum(metric string) (float64, bool) {
	v := f[metric]
	s := 0.0
	for _, x := range v {
		s += x
	}
	return s, len(v) > 0
}

// mean is a metric's mean over its trials, and whether there is one.
func (f armFigures) mean(metric string) (float64, bool) {
	s, ok := f.sum(metric)
	if !ok {
		return 0, false
	}
	return s / float64(len(f[metric])), true
}

// show is a metric's mean in format, or - when there is none.
func (f armFigures) show(metric, format string) string {
	if m, ok := f.mean(metric); ok {
		return fmt.Sprintf(format, m)
	}
	return "-"
}

// ratio is the sum of num over the sum of den, and whether both are there
// and den is not 0.
func (f armFigures) ratio(num, den string) (float64, bool) {
	n, ok1 := f.sum(num)
	d, ok2 := f.sum(den)
	if !ok1 || !ok2 || d == 0 {
		return 0, false
	}
	return n / d, true
}

// trials is the number of trials of the side: its rows of calls_first.
func (f armFigures) trials() int { return len(f["spike.calls_first"]) }

// tokensPerTrial is the mean of all tokens, in and out, a trial.
func (f armFigures) tokensPerTrial() (float64, bool) {
	in, ok1 := f.sum("spike.tokens_in")
	o, ok2 := f.sum("spike.tokens_out")
	if !ok1 || !ok2 || len(f["spike.tokens_in"]) == 0 {
		return 0, false
	}
	return (in + o) / float64(len(f["spike.tokens_in"])), true
}

// perCall is a sum over every call, first and repair.
func (f armFigures) perCall(metrics ...string) (float64, bool) {
	calls := 0.0
	for _, m := range []string{"spike.calls_first", "spike.calls_repair"} {
		s, ok := f.sum(m)
		if !ok {
			return 0, false
		}
		calls += s
	}
	total := 0.0
	for _, m := range metrics {
		s, ok := f.sum(m)
		if !ok {
			return 0, false
		}
		total += s
	}
	if calls == 0 {
		return 0, false
	}
	return total / calls, true
}

// wrongShare is wrong_write as a share: the wrong writes over the
// operations accepted after repair, the nearest count of stored statements
// the rows hold. With nothing accepted, a wrong write makes it 1.
func (f armFigures) wrongShare() (float64, bool) {
	w, ok1 := f.sum("fidelity.wrong_write")
	a, ok2 := f.sum("spike.ops_accepted_final")
	switch {
	case !ok1 || !ok2:
		return 0, false
	case a == 0 && w > 0:
		return 1, true
	case a == 0:
		return 0, true
	}
	return w / a, true
}

// frontierArm is one line of the frontier: an arm's all-scripts rows on
// each side.
type frontierArm struct {
	host, version, tier, effort string
	form                        spike.Form
	sides                       map[string]armFigures
}

func (a frontierArm) on() armFigures { return a.sides["on"] }

// passes says whether the arm meets design 8.3's bar.
func (a frontierArm) passes() bool {
	recall, ok1 := a.on().mean("fidelity.fact_recall")
	share, ok2 := a.on().wrongShare()
	mis, ok3 := a.on().sum("fidelity.misattributed")
	return ok1 && ok2 && ok3 && recall >= barRecall-epsilon && share <= barWrongWrite+epsilon && mis == 0
}

// label names the arm.
func (a frontierArm) label() string {
	return fmt.Sprintf("%s %s, tier %s, effort %s, form %s", a.host, a.version, a.tier, a.effort, a.form)
}

// gather groups the spike's all-scripts rows by arm. Rows of another
// instrument, and a script's own rows, are passed over. It refuses a
// row that names no tier as its model, and an arm whose rows give two
// host versions.
func gather(rows []results.Row) ([]frontierArm, error) {
	type key struct{ host, tier, effort, form string }
	byKey := map[key]*frontierArm{}
	for i, r := range rows {
		if r.Instrument != arm.Instrument {
			continue
		}
		parts := strings.Split(r.Scenario, ".")
		if len(parts) != 4 || parts[3] != "all" || (parts[2] != "on" && parts[2] != "off") ||
			!spike.Form(parts[0]).Valid() || !slices.Contains(arm.Efforts, parts[1]) {
			continue
		}
		switch {
		case !slices.Contains(arm.Tiers, r.Model):
			return nil, fmt.Errorf("row %d: its model is not a tier", i+1)
		case r.Host == "":
			return nil, fmt.Errorf("row %d: it names no host", i+1)
		}
		k := key{r.Host, r.Model, parts[1], parts[0]}
		a := byKey[k]
		if a == nil {
			a = &frontierArm{host: r.Host, version: r.HostVersion, tier: r.Model, effort: parts[1], form: spike.Form(parts[0]),
				sides: map[string]armFigures{"on": {}, "off": {}}}
			byKey[k] = a
		}
		if a.version != r.HostVersion {
			return nil, fmt.Errorf("row %d: the arm %s, tier %s, effort %s, form %s has rows of two host versions", i+1, r.Host, r.Model, parts[1], parts[0])
		}
		a.sides[parts[2]][r.Metric] = append(a.sides[parts[2]][r.Metric], r.Value)
	}
	var arms []frontierArm
	for _, a := range byKey {
		arms = append(arms, *a)
	}
	slices.SortFunc(arms, func(x, y frontierArm) int {
		return cmp.Or(cmp.Compare(hostOrder(x.host), hostOrder(y.host)), strings.Compare(x.host, y.host),
			cmp.Compare(slices.Index(arm.Tiers, x.tier), slices.Index(arm.Tiers, y.tier)),
			cmp.Compare(slices.Index(arm.Efforts, x.effort), slices.Index(arm.Efforts, y.effort)),
			cmp.Compare(slices.Index(spike.Forms, x.form), slices.Index(spike.Forms, y.form)))
	})
	return arms, nil
}

// hostOrder is a host's place in eva-md's order; one it does not know
// comes after.
func hostOrder(h string) int {
	if i := slices.Index(hostIDs(), h); i >= 0 {
		return i
	}
	return len(hostIDs())
}

// cheaper orders arms by tier, then by measured tokens a trial, then by
// effort and form; an arm with no tokens measured comes last of its tier.
func cheaper(x, y frontierArm) int {
	tx, okx := x.on().tokensPerTrial()
	ty, oky := y.on().tokensPerTrial()
	byTokens := 0
	switch {
	case okx && oky:
		byTokens = cmp.Compare(tx, ty)
	case okx:
		byTokens = -1
	case oky:
		byTokens = 1
	}
	return cmp.Or(cmp.Compare(slices.Index(arm.Tiers, x.tier), slices.Index(arm.Tiers, y.tier)), byTokens,
		cmp.Compare(slices.Index(arm.Efforts, x.effort), slices.Index(arm.Efforts, y.effort)),
		cmp.Compare(slices.Index(spike.Forms, x.form), slices.Index(spike.Forms, y.form)))
}

// pick is the default among arms, by design 8.3: the cheapest small arm
// that meets the bar; else the cheapest middle arm that meets it; else the
// cheapest middle arm, which misses it. A large arm is never the default.
// ok is false when there is no small or middle arm to name.
func pick(arms []frontierArm) (best frontierArm, meets, ok bool) {
	sorted := slices.Clone(arms)
	slices.SortFunc(sorted, cheaper)
	for _, tier := range []string{arm.TierSmall, arm.TierMiddle} {
		for _, a := range sorted {
			if a.tier == tier && a.passes() {
				return a, true, true
			}
		}
	}
	for _, a := range sorted {
		if a.tier == arm.TierMiddle {
			return a, false, true
		}
	}
	return frontierArm{}, false, false
}

// verdict is what the frontier reads by rule from the arms.
type verdict struct {
	form     spike.Form // the evidence form, "" when it cannot be read
	formWhy  string
	defaults map[string]frontierArm // by host
	meets    map[string]bool
	why      map[string]string
}

// decide reads the evidence form, then each host's default row, by rule.
func decide(arms []frontierArm, hosts []string) verdict {
	v := verdict{defaults: map[string]frontierArm{}, meets: map[string]bool{}, why: map[string]string{}}
	of := func(host string, form spike.Form) []frontierArm {
		var out []frontierArm
		for _, a := range arms {
			if a.host == host && (form == "" || a.form == form) {
				out = append(out, a)
			}
		}
		return out
	}
	// The evidence form, design 5.4: segments when quote_miss_final is over
	// 2% at either host's default row of the quotes form.
	over, unread := []string{}, []string{}
	var notes []string
	for _, h := range hosts {
		q, _, ok := pick(of(h, spike.FormQuotes))
		miss, measured := q.on().mean("spike.quote_miss_final")
		switch {
		case !ok || !measured:
			unread = append(unread, h)
		case miss > barQuoteMiss+epsilon:
			over = append(over, h)
			notes = append(notes, fmt.Sprintf("%s %.3f at tier %s, effort %s", h, miss, q.tier, q.effort))
		default:
			notes = append(notes, fmt.Sprintf("%s %.3f at tier %s, effort %s", h, miss, q.tier, q.effort))
		}
	}
	switch {
	case len(over) > 0:
		v.form = spike.FormSegments
		v.formWhy = fmt.Sprintf("quote_miss_final is over 2%% at the quotes form's default row on %s (%s)", strings.Join(over, " and "), strings.Join(notes, "; "))
	case len(unread) > 0:
		v.formWhy = fmt.Sprintf("it cannot be read: no quotes row with a quote_miss_final to read at the quotes form's default row on %s", strings.Join(unread, " and "))
	default:
		v.form = spike.FormQuotes
		v.formWhy = fmt.Sprintf("quote_miss_final is at most 2%% at the quotes form's default row on every host (%s)", strings.Join(notes, "; "))
	}
	for _, h := range hosts {
		a, meets, ok := pick(of(h, v.form))
		if !ok {
			v.why[h] = "no small or middle row to name"
			continue
		}
		v.defaults[h], v.meets[h] = a, meets
		hasSmall := slices.ContainsFunc(of(h, v.form), func(x frontierArm) bool { return x.tier == arm.TierSmall })
		switch {
		case a.tier == arm.TierSmall:
			v.why[h] = "the cheapest small row that meets the bar"
		case meets && hasSmall:
			v.why[h] = "no small row meets the bar, so the middle tier's cheapest row that meets it"
		case meets:
			v.why[h] = "no small row was measured, so the middle tier's cheapest row that meets it"
		case hasSmall:
			v.why[h] = "no small row meets the bar, and no middle row does: the middle tier's cheapest row, which misses the bar"
		default:
			v.why[h] = "no small row was measured, and no middle row meets the bar: the middle tier's cheapest row, which misses the bar"
		}
	}
	return v
}

// cell is a value in format, or - when there is none.
func cell(v float64, ok bool, format string) string {
	if !ok {
		return "-"
	}
	return fmt.Sprintf(format, v)
}

func spikeFrontierCommand(args []string, out, errOut io.Writer) int {
	f, fl := newFlags("spike frontier", errOut)
	if err := f.Parse(args); err != nil {
		return 2
	}
	if f.NArg() == 0 {
		return usage(errOut, "spike frontier takes one or more sets of rows")
	}
	cat, err := results.LoadCatalogue(inRepo(fl, *fl.catalogue, "eval/spike/metrics.json"))
	if err != nil {
		return fail(errOut, err)
	}
	var rows []results.Row
	for _, path := range f.Args() {
		set, err := results.Load(path)
		if err == nil {
			err = cat.Check(set)
		}
		if err != nil {
			return fail(errOut, err)
		}
		rows = append(rows, set...)
	}
	arms, err := gather(rows)
	if err != nil {
		return fail(errOut, err)
	}
	if len(arms) == 0 {
		return fail(errOut, errors.New("the rows hold no arm of the spike: no row of a scenario <form>.<effort>.on.all"))
	}
	fmt.Fprint(out, frontier(rows, arms))
	return 0
}

// frontier writes the frontier's Markdown.
func frontier(rows []results.Row, arms []frontierArm) string {
	var b strings.Builder
	w := func(format string, args ...any) { fmt.Fprintf(&b, format, args...) }
	commits, dirty := map[string]bool{}, false
	for _, r := range rows {
		if r.Instrument == arm.Instrument {
			commits[r.Commit[:12]] = true
			dirty = dirty || r.Dirty
		}
	}
	tree := "clean trees"
	if dirty {
		tree = "a dirty tree among them"
	}
	// Every host eva-md knows, measured or not, so a rule that reads
	// either host cannot pass over one that has no rows.
	hosts := hostIDs()
	for _, a := range arms {
		if !slices.Contains(hosts, a.host) {
			hosts = append(hosts, a.host)
		}
	}
	w("# The contract spike's frontier\n\n")
	w("From %d arms, of commit %s (%s).\n\n", len(arms), strings.Join(slices.Sorted(maps.Keys(commits)), ", "), tree)
	w("How to read it:\n\n")
	w("- One line for each host, tier, effort and form, from the arm's `<form>.<effort>.on.all` rows, with repair: the store that applies each pass's final verdicts. A row of each metric is one trial (a round of a run); each figure is the mean over the trials, a share over those that have one. The tier is all a row says of its model.\n")
	w("- `wrong_write` is the scorer's count a trial; its share is the wrong writes over the operations accepted after repair, the nearest count of stored statements the rows hold, and the bar of design 8.3 reads the share.\n")
	w("- `stale_in_force` is shown and is no bar: the scorer counts a faithful answer to v1.08 as stale, since a next step that names a word of an ended step reads as keeping that step in force (EV-14e may change that reading), so a count of about 1 a trial can be no fault of the model.\n")
	w("- Refusals and hedges: the typed refusals and hedges the scripts plant that an operation kept as a decision or a procedure citing them (cited), and of those the ones accepted under the user's authority (accepted).\n")
	w("- Tokens a call are every call's input and output over the calls, first and repair; seconds a call likewise.\n")
	w("- The default row (design 8.3): the cheapest, by tier then measured tokens a trial, with `fact_recall` at least %.2f, `wrong_write` at most %.2f as a share and `misattributed` 0; if no small row meets that, the middle tier's cheapest that does, or else its cheapest, which is said to miss the bar. A large row is never the default. It is read for each host among the rows of the evidence form.\n", barRecall, barWrongWrite)
	w("- The evidence form (design 5.4): segments when `quote_miss_final` is over 2%% on either host at the quotes form's default row, read by the same rule among that form's rows; else quotes.\n")
	w("- Assent (plan section 8): if a typed refusal or hedge was accepted as assent at a default row, a short typed yes does not count, and decision 10 stays at dialog choices and plan approvals.\n\n")
	w("| Host | Version | Tier | Effort | Form | Trials | fact_recall | wrong_write | wrong_write share | misattributed | stale_in_force | honest_refused | quote_miss_final | Refusals cited, accepted | Hedges cited, accepted | repair_yield | Tokens a call | Seconds a call |\n")
	w("| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |\n")
	for _, a := range arms {
		on := a.on()
		share, okShare := on.wrongShare()
		tok, okTok := on.perCall("spike.tokens_in", "spike.tokens_out")
		sec, okSec := on.perCall("spike.seconds")
		w("| %s | %s | %s | %s | %s | %d | %s | %s | %s | %s | %s | %s | %s | %s, %s | %s, %s | %s | %s | %s |\n",
			a.host, a.version, a.tier, a.effort, a.form, on.trials(),
			on.show("fidelity.fact_recall", "%.3f"), on.show("fidelity.wrong_write", "%.2f"), cell(share, okShare, "%.3f"),
			on.show("fidelity.misattributed", "%.2f"), on.show("fidelity.stale_in_force", "%.2f"), on.show("spike.honest_refused", "%.3f"),
			on.show("spike.quote_miss_final", "%.3f"),
			on.show("spike.assent_cited_refusal", "%.2f"), on.show("spike.accepted_from_refusal", "%.2f"),
			on.show("spike.assent_cited_hedge", "%.2f"), on.show("spike.accepted_from_hedge", "%.2f"),
			on.show("spike.repair_yield", "%.3f"), cell(tok, okTok, "%.0f"), cell(sec, okSec, "%.1f"))
	}
	w("\nWhat the answers broke (host fact F-064), a trial: schema breaks and invalid answers, of the first answers and of the repair answers apart, and the calls that failed.\n\n")
	w("| Host | Tier | Effort | Form | Schema breaks, first | Schema breaks, repair | Invalid, first | Invalid, repair | Calls failed |\n| --- | --- | --- | --- | --- | --- | --- | --- | --- |\n")
	for _, a := range arms {
		on := a.on()
		w("| %s | %s | %s | %s | %s | %s | %s | %s | %s |\n", a.host, a.tier, a.effort, a.form,
			on.show("spike.schema_breaks_first", "%.2f"), on.show("spike.schema_breaks_repair", "%.2f"),
			on.show("spike.answers_invalid_first", "%.2f"), on.show("spike.answers_invalid_repair", "%.2f"), on.show("spike.calls_failed", "%.2f"))
	}
	v := decide(arms, hosts)
	w("\n## By rule\n\n")
	if v.form == "" {
		w("- **The evidence form:** not read: %s.\n", v.formWhy)
	} else {
		w("- **The evidence form:** %s, since %s.\n", v.form, v.formWhy)
	}
	for _, h := range hosts {
		d, ok := v.defaults[h]
		if !ok {
			w("- **The default row on %s:** none: %s.\n", h, v.why[h])
			continue
		}
		on := d.on()
		share, _ := on.wrongShare()
		met := "it meets the bar"
		if !v.meets[h] {
			met = "it misses the bar"
		}
		w("- **The default row on %s:** %s, %s: fact_recall %s, wrong_write share %.3f, misattributed %s; %s.\n",
			h, d.label(), v.why[h], on.show("fidelity.fact_recall", "%.3f"), share, on.show("fidelity.misattributed", "%.2f"), met)
	}
	for _, h := range hosts {
		d, ok := v.defaults[h]
		if !ok {
			continue
		}
		on := d.on()
		refused, _ := on.sum("spike.accepted_from_refusal")
		hedged, _ := on.sum("spike.accepted_from_hedge")
		pr, _ := on.sum("spike.plants_refusal")
		ph, _ := on.sum("spike.plants_hedge")
		switch {
		case refused+hedged > 0:
			w("- **Assent on %s:** at the default row %.0f typed refusals and %.0f typed hedges were accepted as assent, of %.0f and %.0f shown, over %d trials: a short typed yes does not count, and decision 10 stays at dialog choices and plan approvals.\n",
				h, refused, hedged, pr, ph, on.trials())
		case pr+ph == 0:
			w("- **Assent on %s:** not measured: the default row's trials showed no planted refusal or hedge.\n", h)
		default:
			w("- **Assent on %s:** at the default row no typed refusal or hedge was accepted as assent, of %.0f and %.0f shown, over %d trials.\n", h, pr, ph, on.trials())
		}
	}
	for _, h := range hosts {
		d, ok := v.defaults[h]
		if !ok {
			continue
		}
		on, off := d.on(), d.sides["off"]
		yield := on.show("spike.repair_yield", "%.3f")
		tokShare, okTok := on.ratio("spike.repair_tokens_in", "spike.tokens_in")
		cached := "not measured: no repair call reported its cached input"
		if c, ok := on.sum("spike.repair_tokens_cached_in"); ok {
			// A trial writes its cached row only when it made repair calls
			// and every one reported it: read the share only when every
			// trial with repair input did.
			repairIn, withRepair := 0.0, 0
			for _, x := range on["spike.repair_tokens_in"] {
				repairIn += x
				if x > 0 {
					withRepair++
				}
			}
			if n := len(on["spike.repair_tokens_cached_in"]); n == withRepair && repairIn > 0 {
				cached = fmt.Sprintf("%.3f of the repair calls' input", c/repairIn)
			} else {
				cached = fmt.Sprintf("not read: reported in %d of the %d trials with repair input", n, withRepair)
			}
		}
		w("- **Repair on %s**, at the default row: yield %s; the repair calls' input is %s of all input (the rows keep no repair output apart); cached share (F-065) %s; fact_recall %s with repair, %s without.\n",
			h, yield, cell(tokShare, okTok, "%.3f"), cached, on.show("fidelity.fact_recall", "%.3f"), off.show("fidelity.fact_recall", "%.3f"))
	}
	return b.String()
}
