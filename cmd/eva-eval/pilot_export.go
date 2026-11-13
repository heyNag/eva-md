package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/heyNag/eva-md/internal/eval/pilot/export"
)

// eva-eval pilot export writes the pilot's tasks in the continuation's
// format (EV-03b), for its driver: a check of each task as pilot check
// makes it, then the folder, written only when every task passes. Its
// output holds ids and figures only.
func init() {
	addPilot("export", pilotCommand{synopsis: "export --out dir [--tasks dir] [--task id]...", run: pilotExport})
}

// taskIDs is the --task flag, which may be given more than once.
type taskIDs []string

func (n *taskIDs) String() string     { return strings.Join(*n, ",") }
func (n *taskIDs) Set(v string) error { *n = append(*n, v); return nil }

// pilotExport prints a line a task, "<id> <family> exported", then
// "session-timeout <n>m", the largest minutes exported, and exits 0. When
// a task is refused it prints, a line a task, what pilot check prints, or
// the continuation format's refusal, writes nothing, and exits 1, or 2
// when a task could not be checked at all. A usage error, or a write that
// fails, exits 2 and leaves nothing written.
func pilotExport(ctx context.Context, args []string, out, errOut io.Writer) int {
	f := flag.NewFlagSet("eva-eval pilot export", flag.ContinueOnError)
	// The flag package's message can quote a value, so it is not shown.
	f.SetOutput(io.Discard)
	o := export.Options{Limit: pilotCheckLimit}
	var ids taskIDs
	f.StringVar(&o.Tasks, "tasks", filepath.Join("eval", "pilot", "testdata", "tasks"), "")
	f.StringVar(&o.Out, "out", "", "")
	f.Var(&ids, "task", "")
	if err := f.Parse(args); err != nil {
		return pilotUsage(errOut, "a flag of pilot export is unknown or has no value")
	}
	if f.NArg() != 0 {
		return pilotUsage(errOut, "pilot export takes no argument but its flags")
	}
	if o.Out == "" {
		return pilotUsage(errOut, "pilot export needs --out")
	}
	o.IDs = ids
	r, err := export.Export(ctx, o)
	var usage *export.UsageError
	switch {
	case errors.As(err, &usage):
		return pilotUsage(errOut, "pilot export: "+usage.Error())
	case err != nil:
		return fail(errOut, err)
	}
	code := 0
	for _, t := range r.Tasks {
		switch {
		case t.Rule != nil:
			fmt.Fprintln(out, broke(t.Rule))
			code = max(code, 1)
		case t.Unchecked != nil:
			fmt.Fprintln(out, t.ID+" "+t.Family+" cannot be checked: "+t.Unchecked.Error())
			code = 2
		case t.Format != "":
			fmt.Fprintln(out, t.ID+" "+t.Family+" driver format: "+t.Format)
			code = max(code, 1)
		case r.Written:
			fmt.Fprintln(out, t.ID+" "+t.Family+" exported")
		default:
			fmt.Fprintln(out, t.ID+" "+t.Family+" ok")
		}
	}
	if code == 0 && r.Written {
		fmt.Fprintf(out, "session-timeout %dm\n", r.Minutes)
	}
	return code
}
