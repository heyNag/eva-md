// Command live-accuracy runs the update's accuracy suite live on one host:
// each pass of its 12 fact scripts through the host's real headless update
// command, the update's validator and threadpage.Apply, as the update job
// runs a pass, then prints the suite's metrics against the live target in
// docs/design.md. It is a tool for eva-md's developers, kept apart from
// eva-md: no release or install builds it, and eva-md never imports the
// suite.
//
// Every pass is a model call on the host's subscription, 21 in all, so it
// needs --host and refuses to run without --spend-model-quota. It works in
// a temporary home of its own, which holds each call's empty run folder and
// is removed at the end; it reads and writes nothing of eva-md's own. The
// host CLI runs with your usual login, as the update job's calls do.
//
// --record writes each pass's answer to a recording as the run goes (see
// record.go), --replay answers each pass from one, calling nothing, and
// --pages writes each script's page after its last pass, which the
// evaluation's fidelity command scores.
//
//	go run ./cmd/live-accuracy --host claude-code --spend-model-quota
package main

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
	"os/signal"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"time"

	"github.com/heyNag/eva-md/internal/config"
	"github.com/heyNag/eva-md/internal/threadpage"
	"github.com/heyNag/eva-md/internal/update"
	"github.com/heyNag/eva-md/internal/update/accuracy"
	"github.com/heyNag/eva-md/internal/update/accuracy/live"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	code := run(ctx, os.Args[1:], os.Stdout, os.Stderr, update.NewRunner)
	stop()
	os.Exit(code)
}

// clis are the commands the hosts' update runs start.
var clis = map[string]string{update.HostClaude: "claude", update.HostCodex: "codex"}

// run runs the command with the runner newRunner gives for the host, and
// returns its exit code: 0 when the run meets the target, 1 when it misses
// it or stops, 2 for a usage error or a run refused.
func run(ctx context.Context, args []string, out, errOut io.Writer, newRunner func(host string) update.Runner) (code int) {
	f := flag.NewFlagSet("live-accuracy", flag.ContinueOnError)
	f.SetOutput(errOut)
	host := f.String("host", "", "the host to run on, claude-code or codex; required")
	spend := f.Bool("spend-model-quota", false, "make the run's model calls, on the host's subscription; without it, nothing is called")
	effort := f.String("effort", config.EffortHighest, "the effort each call asks for: one of the host's levels, or highest")
	model := f.String("model", "", "the model each call asks for; the host's default when empty")
	cli := f.String("cli", "", "the host CLI's absolute path; looked up on PATH when empty")
	ignore := f.Bool("codex-ignore-user-config", false, "run codex without your Codex config, as codex_ignore_user_config = true runs the update")
	record := f.String("record", "", "write each pass's answer to this file, which must not exist yet, as the run goes")
	replay := f.String("replay", "", "answer each pass from this recording, made with the same --host and --model; calls nothing and needs no --spend-model-quota")
	pages := f.String("pages", "", "write each script's page after its last pass into this folder, new or empty, as v1.NN.md")
	if err := f.Parse(args); err != nil {
		return 2
	}
	name, ok := clis[*host]
	switch {
	case f.NArg() != 0:
		return usage(errOut, "it takes no arguments, only flags")
	case !ok:
		return usage(errOut, "--host must be claude-code or codex")
	case *cli != "" && (!filepath.IsAbs(*cli) || filepath.Base(*cli) != name):
		return usage(errOut, "--cli must be the absolute path of a command named "+name)
	case *ignore && *host != update.HostCodex:
		return usage(errOut, "--codex-ignore-user-config is for codex only")
	case *record != "" && *replay != "":
		return usage(errOut, "--record and --replay can't be used together")
	case *replay != "" && (*cli != "" || *ignore):
		return usage(errOut, "--replay runs no CLI, so it takes no --cli or --codex-ignore-user-config")
	}
	// The effort, the model and the CLI as the runner would refuse them, or
	// pass over a CLI for the one on PATH, before anything is made or called.
	if err := update.CheckFlags(*host, *effort, *model, *cli); err != nil {
		return usage(errOut, strings.TrimPrefix(err.Error(), "update: "))
	}
	scripts, calls := accuracy.Scripts(), 0
	index, counts := map[*accuracy.Script]int{}, make([]int, len(scripts))
	for i, s := range scripts {
		calls += len(s.Passes)
		index[s], counts[i] = i+1, len(s.Passes)
	}
	var answers map[passKey]passRecord
	if *replay != "" {
		var err error
		if answers, err = readRecording(*replay, *host, *model, counts); err != nil {
			fmt.Fprintf(errOut, "live-accuracy: the recording %s is refused: %v. Nothing was called.\n", *replay, err)
			return 2
		}
	} else if !*spend {
		fmt.Fprintf(errOut, "live-accuracy: the run makes %d model calls on %s's subscription, one for each pass of the suite's %d fact scripts, each allowed up to 10 minutes. Nothing was called; run it again with --spend-model-quota to make them.\n", calls, *host, len(scripts))
		return 2
	}
	if *record != "" {
		if _, err := os.Lstat(*record); !errors.Is(err, fs.ErrNotExist) {
			return usage(errOut, "--record must name a file that does not exist yet")
		}
	}
	if *pages != "" {
		if err := checkPages(*pages); err != nil {
			return usage(errOut, err.Error())
		}
	}
	// The recording first, so a folder it cannot be made in leaves nothing
	// made and nothing called.
	var rec *recorder
	if *record != "" {
		var err error
		if rec, err = createRecording(*record, *host, *model); err != nil {
			fmt.Fprintf(errOut, "live-accuracy: could not make the recording: %v\n", err)
			return 1
		}
		defer func() {
			if err := rec.close(); err != nil {
				fmt.Fprintf(errOut, "live-accuracy: could not close the recording: %v\n", err)
				code = 1
			}
		}()
	}
	if *pages != "" {
		if err := os.MkdirAll(*pages, 0o755); err != nil {
			fmt.Fprintf(errOut, "live-accuracy: could not make the pages folder: %v\n", err)
			return 1
		}
	}
	efforts := map[string]int{}
	var ask live.Ask
	if answers != nil {
		ask = replayer{answers: answers, index: index, efforts: efforts}.ask
		fmt.Fprintf(errOut, "live-accuracy: replaying the %d answers of the recording %s; nothing is called\n", calls, *replay)
	} else {
		home, err := os.MkdirTemp("", "eva-md-live-accuracy-")
		if err != nil {
			fmt.Fprintf(errOut, "live-accuracy: could not make its temporary home: %v\n", err)
			return 1
		}
		defer func() {
			if err := os.RemoveAll(home); err != nil {
				fmt.Fprintf(errOut, "live-accuracy: could not remove its temporary home %s: %v\n", home, err)
				code = 1
			}
		}()
		c := caller{host: *host, effort: *effort, model: *model, cli: *cli, ignore: *ignore, home: home, runner: newRunner(*host), log: errOut, efforts: efforts}
		ask = c.ask
	}
	if rec != nil {
		ask = recorded(ask, rec, index)
	}
	who := *model
	if who == "" {
		who = "the host's default model"
	}
	fmt.Fprintf(out, "The accuracy suite's %d fact scripts on %s, %s, effort %s:\n", len(scripts), *host, who, *effort)
	var pageErr error
	res, err := live.Run(ctx, *host, scripts, ask, func(h live.History) {
		history(out, h)
		if *pages != "" && pageErr == nil {
			pageErr = writePage(*pages, index[h.Script], threadpage.Render(h.Page))
		}
	})
	if pageErr != nil {
		fmt.Fprintf(errOut, "live-accuracy: could not write a page: %v\n", pageErr)
	}
	if err != nil {
		var stopped *live.Stopped
		if !errors.As(err, &stopped) {
			err = fmt.Errorf("the run stopped: %w", err)
		}
		fmt.Fprintf(errOut, "live-accuracy: %v\n", err)
		return 1
	}
	if code = summary(out, *host, res, efforts); pageErr != nil {
		code = 1
	}
	return code
}

func usage(w io.Writer, msg string) int {
	fmt.Fprintln(w, "live-accuracy: "+msg+"; usage: live-accuracy --host claude-code|codex --spend-model-quota [--effort level] [--model name] [--cli path] [--codex-ignore-user-config] [--record file] [--pages dir], "+
		"or live-accuracy --host claude-code|codex --replay file [--effort level] [--model name] [--pages dir]")
	return 2
}

// recorded is ask, writing each answer it gives to rec, script by its
// place in the suite. A pass ask fails, as on a model the host rejected,
// is not written: its answer came from another model, or none came.
func recorded(ask live.Ask, rec *recorder, index map[*accuracy.Script]int) live.Ask {
	return func(ctx context.Context, s *accuracy.Script, n int, b update.Batch, page threadpage.Page) (update.Result, error) {
		res, err := ask(ctx, s, n, b, page)
		if err != nil {
			return res, err
		}
		if err := rec.write(index[s], n, res); err != nil {
			return res, fmt.Errorf("could not write the recording: %w", err)
		}
		return res, nil
	}
}

// replayer answers each pass from a recording, which readRecording checked
// holds every pass of the suite, and counts the efforts of its answers as
// caller does.
type replayer struct {
	answers map[passKey]passRecord
	index   map[*accuracy.Script]int
	efforts map[string]int
}

func (r replayer) ask(ctx context.Context, s *accuracy.Script, n int, _ update.Batch, _ threadpage.Page) (update.Result, error) {
	if err := ctx.Err(); err != nil {
		return update.Result{}, err
	}
	a, ok := r.answers[passKey{r.index[s], n}]
	if !ok {
		return update.Result{}, fmt.Errorf("the recording has no answer for %s, pass %d", s.Name, n)
	}
	res := a.result()
	if res.Reason == "" {
		r.efforts[res.Effort]++
	}
	return res, nil
}

// caller asks the host's update command for each pass's answer.
type caller struct {
	host, effort, model, cli string
	ignore                   bool   // codex_ignore_user_config
	home                     string // the run's temporary home
	runner                   update.Runner
	log                      io.Writer
	efforts                  map[string]int // the answers at each effort the host used, "" for none
}

// ask runs the host's update command on the pass's batch, with the
// update's rules and the schema for page, in an empty run folder in the
// run's home, as the update job's runModel does. A run on the host's
// default model in place of the one asked for, which the host rejected,
// stops the run, whether its answer came, was refused or failed: its
// answers would be judged as the other model's.
func (c caller) ask(ctx context.Context, s *accuracy.Script, n int, b update.Batch, page threadpage.Page) (update.Result, error) {
	dir, err := os.MkdirTemp(c.home, "run-")
	if err != nil {
		return update.Result{}, fmt.Errorf("could not make a run folder: %w", err)
	}
	defer os.RemoveAll(dir)
	fmt.Fprintf(c.log, "live-accuracy: %s, pass %d of %d: asking %s\n", s.Name, n, len(s.Passes), c.host)
	start := time.Now()
	res, err := c.runner.Run(ctx, update.Request{Host: c.host, Model: c.model, Effort: c.effort, Rules: update.Rules, Input: b.Input,
		Schema: update.Schema(page), RunDir: dir, CLI: c.cli, IgnoreUserConfig: c.ignore})
	if err != nil {
		return res, err
	}
	took := time.Since(start).Seconds()
	model, fellBack := strings.CutPrefix(res.ModelNote, update.NoteFallback)
	if res.Reason != "" {
		fmt.Fprintf(c.log, "live-accuracy: %s after %.1f s\n", res.Reason, took)
		if fellBack {
			return res, &live.Stopped{History: s.Name, Pass: n, Reason: c.host + " rejected the model " + model + ", and its default model's run then failed: " + res.Reason}
		}
		return res, nil
	}
	effort := "effort " + res.Effort
	if res.Effort == "" {
		effort = "no effort flag"
	}
	if res.Degraded != "" {
		effort += "; " + res.Degraded
	}
	fmt.Fprintf(c.log, "live-accuracy: answered in %.1f s, %s\n", took, effort)
	if fellBack {
		return res, &live.Stopped{History: s.Name, Pass: n, Reason: c.host + " rejected the model " + model + " and answered with its default model"}
	}
	c.efforts[res.Effort]++
	return res, nil
}

// history prints one script's metrics after its last pass, then what the
// run missed in it, each refused answer among them, and the page it ended
// with when it missed anything.
func history(out io.Writer, h live.History) {
	m := h.Metrics
	fmt.Fprintf(out, "- %s, %d %s: recall %.2f, updates %.2f, stale %.2f, tags %.2f, dates %.2f, carried %d, duplicates %d\n",
		h.Script.Name, len(h.Script.Passes), plural(len(h.Script.Passes), "pass", "passes"), m.Recall, m.Updates, m.Stale, m.Tags, m.Dates, m.Carried, m.Duplicates)
	for _, l := range h.Missed {
		fmt.Fprintln(out, "  "+l)
	}
	if len(h.Missed) == 0 {
		return
	}
	fmt.Fprintln(out, "  The page after its last pass:")
	for _, l := range strings.Split(strings.TrimSpace(threadpage.Render(h.Page)), "\n") {
		fmt.Fprintln(out, strings.TrimRight("    "+l, " "))
	}
}

// summary prints the run's counts against the target, then how many of
// its answers came at each effort the host used, most first, and returns
// the exit code: 0 when the run meets the target.
func summary(out io.Writer, host string, res live.Result, efforts map[string]int) int {
	t := res.Tally
	fmt.Fprintf(out, "\nOn %s, against the target in docs/design.md: every enforceable obligation met, no speaker or date error, and at most %d content obligations missed.\n", host, accuracy.MaxContentMissed)
	fmt.Fprintf(out, "  enforceable obligations missed: %d\n", len(t.Enforceable))
	fmt.Fprintf(out, "  speaker errors: %d\n", len(t.Speaker))
	fmt.Fprintf(out, "  date errors: %d\n", len(t.Date))
	fmt.Fprintf(out, "  content obligations missed: %d\n", len(t.Content))
	fmt.Fprintf(out, "  answers refused: %d of %d\n", res.Refused, res.Passes)
	used := slices.Collect(maps.Keys(efforts))
	slices.SortFunc(used, func(a, b string) int { return cmp.Or(efforts[b]-efforts[a], strings.Compare(a, b)) })
	for _, e := range used {
		if e == "" {
			fmt.Fprintf(out, "  answers with no effort flag: %d of %d\n", efforts[e], res.Passes)
		} else {
			fmt.Fprintf(out, "  answers at effort %s: %d of %d\n", e, efforts[e], res.Passes)
		}
	}
	if t.Meets() {
		fmt.Fprintln(out, "The run meets the target.")
		return 0
	}
	fmt.Fprintln(out, "The run misses the target.")
	return 1
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}
