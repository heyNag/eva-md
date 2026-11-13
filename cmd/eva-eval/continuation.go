package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/heyNag/eva-md/internal/eval/continuation/drive"
	"github.com/heyNag/eva-md/internal/eval/continuation/task"
)

// eva-eval continuation runs the continuation instrument (EV-17): run
// drives a condition's trials on a host's CLI and writes their rows, and
// check checks tasks offline. Their output holds ids and figures only.
func init() {
	register(instrument{
		name:     "continuation",
		synopsis: "continuation run --tasks DIR --repos DIR --host ID --cli PATH --condition C --trials N --work DIR --out DIR --max-sessions N --max-tokens N [--model ID] [--session-timeout D] [--pass-env NAME]... [--eva-md PATH] [--repo DIR] [--spend-model-quota] | check --tasks DIR --repos DIR",
		run:      continuation,
	})
}

const continuationUsage = `usage:
  eva-eval continuation run --tasks DIR --repos DIR --host ID --cli PATH --condition C --trials N --work DIR --out DIR --max-sessions N --max-tokens N [--model ID] [--session-timeout D] [--pass-env NAME]... [--eva-md PATH] [--repo DIR] [--spend-model-quota]
  eva-eval continuation check --tasks DIR --repos DIR`

func continuationFail(w io.Writer, msg string) int {
	fmt.Fprintln(w, "eva-eval: "+msg+"\n"+continuationUsage)
	return 2
}

func continuation(ctx context.Context, args []string, out, errOut io.Writer) int {
	if len(args) == 0 {
		return continuationFail(errOut, "continuation needs run or check")
	}
	switch args[0] {
	case "run":
		return continuationRun(ctx, args[1:], out, errOut)
	case "check":
		return continuationCheck(ctx, args[1:], out, errOut)
	}
	return continuationFail(errOut, "there is no continuation command "+args[0])
}

// names is a flag that may be given more than once.
type names []string

func (n *names) String() string     { return strings.Join(*n, ",") }
func (n *names) Set(v string) error { *n = append(*n, v); return nil }

// continuationRun exits 0 when every planned trial was judged, 1 when one
// was left unjudged or not run, and 2 when nothing started or the run
// stopped.
func continuationRun(ctx context.Context, args []string, out, errOut io.Writer) int {
	f := flag.NewFlagSet("eva-eval continuation run", flag.ContinueOnError)
	f.SetOutput(io.Discard)
	var o drive.Options
	var condition string
	var pass names
	f.StringVar(&o.Tasks, "tasks", "", "")
	f.StringVar(&o.Repos, "repos", "", "")
	f.StringVar(&o.Host, "host", "", "")
	f.StringVar(&o.CLI, "cli", "", "")
	f.StringVar(&condition, "condition", "", "")
	f.IntVar(&o.Trials, "trials", 0, "")
	f.StringVar(&o.Work, "work", "", "")
	f.StringVar(&o.Out, "out", "", "")
	f.Int64Var(&o.MaxSessions, "max-sessions", 0, "")
	f.Int64Var(&o.MaxTokens, "max-tokens", 0, "")
	f.StringVar(&o.Model, "model", "", "")
	f.DurationVar(&o.SessionTimeout, "session-timeout", drive.DefaultSessionTimeout, "")
	f.Var(&pass, "pass-env", "")
	f.StringVar(&o.EvaMD, "eva-md", "", "")
	f.StringVar(&o.Repo, "repo", ".", "")
	f.BoolVar(&o.Spend, "spend-model-quota", false, "")
	if err := f.Parse(args); err != nil {
		// The flag package's message can quote a value, so it is not shown.
		return continuationFail(errOut, "a flag of continuation run is unknown or has no valid value")
	}
	if f.NArg() != 0 {
		return continuationFail(errOut, "continuation run takes no arguments besides its flags")
	}
	if o.SessionTimeout <= 0 {
		return continuationFail(errOut, "--session-timeout is not a positive duration")
	}
	o.Condition = task.Condition(condition)
	o.PassEnv = pass
	s, err := drive.Run(ctx, o, out)
	var usage *drive.UsageError
	switch {
	case errors.As(err, &usage):
		return continuationFail(errOut, usage.Error())
	case errors.Is(err, drive.ErrNoQuota):
		fmt.Fprintln(errOut, "eva-eval: "+err.Error())
		return 2
	case err != nil:
		return fail(errOut, err)
	case s.Judged != s.Planned:
		return 1
	}
	return 0
}

// continuationCheck checks each task offline, a line a task, and exits 1
// when one fails.
func continuationCheck(ctx context.Context, args []string, out, errOut io.Writer) int {
	f := flag.NewFlagSet("eva-eval continuation check", flag.ContinueOnError)
	f.SetOutput(io.Discard)
	tasks := f.String("tasks", "", "")
	repos := f.String("repos", "", "")
	if err := f.Parse(args); err != nil || f.NArg() != 0 || *tasks == "" || *repos == "" {
		return continuationFail(errOut, "continuation check takes --tasks and --repos")
	}
	all, err := task.LoadAll(*tasks)
	if err != nil {
		return fail(errOut, err)
	}
	scratch, err := os.MkdirTemp("", "eva-eval-check-")
	if err != nil {
		return fail(errOut, errors.New("cannot make a scratch folder"))
	}
	defer os.RemoveAll(scratch)
	code := 0
	for i, t := range all {
		dir := fmt.Sprintf("%s%c%d", scratch, os.PathSeparator, i+1)
		if err := task.Check(ctx, t, *repos, dir); err != nil {
			fmt.Fprintf(out, "%s fail: %v\n", t.ID, err)
			code = 1
			continue
		}
		fmt.Fprintf(out, "%s ok\n", t.ID)
	}
	return code
}
