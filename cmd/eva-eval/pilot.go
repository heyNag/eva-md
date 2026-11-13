package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/heyNag/eva-md/internal/eval/pilot/task"
)

// eva-eval pilot holds the pilot's commands (EV-03), which are throwaway:
// they are deleted when the continuation harness (EV-17) lands. Each is
// "eva-eval pilot <name> ...". This file adds check; another file adds
// its own command to pilotCommands with addPilot, from its init, so no
// command's file edits this one.
func init() {
	register(instrument{
		name:     "pilot",
		synopsis: "pilot <command> ...: the pilot's commands, which eva-eval pilot lists",
		run:      pilotMain,
	})
	addPilot("check", pilotCommand{synopsis: "check [--tasks dir]", run: pilotCheck})
}

// pilotCommand is a command of eva-eval pilot.
type pilotCommand struct {
	synopsis string // its usage line, after "eva-eval pilot "
	run      func(ctx context.Context, args []string, out, errOut io.Writer) int
}

// pilotCommands holds the pilot's commands, by name.
var pilotCommands = map[string]pilotCommand{}

// addPilot adds a command of eva-eval pilot. It panics on a name that is
// taken or not lower-case letters and dashes, or on a command with no
// usage line or nothing to run: each is a mistake in the program.
func addPilot(name string, c pilotCommand) {
	_, taken := pilotCommands[name]
	switch {
	case taken:
		panic("eva-eval: the pilot command " + name + " is added twice")
	case name == "" || strings.Trim(name, "abcdefghijklmnopqrstuvwxyz-") != "":
		panic("eva-eval: a pilot command's name is lower-case letters and dashes, not " + fmt.Sprintf("%q", name))
	case !strings.HasPrefix(c.synopsis, name) || strings.Contains(c.synopsis, "\n") || c.run == nil:
		panic("eva-eval: the pilot command " + name + " needs a usage line that starts with its name, and something to run")
	}
	pilotCommands[name] = c
}

func pilotMain(ctx context.Context, args []string, out, errOut io.Writer) int {
	if len(args) == 0 {
		return pilotUsage(errOut, "pilot needs a command")
	}
	c, ok := pilotCommands[args[0]]
	if !ok {
		return pilotUsage(errOut, "there is no pilot command "+args[0])
	}
	return c.run(ctx, args[1:], out, errOut)
}

// pilotUsage reports a usage error with the pilot's commands, by name,
// and returns 2.
func pilotUsage(w io.Writer, msg string) int {
	names := make([]string, 0, len(pilotCommands))
	for name := range pilotCommands {
		names = append(names, name)
	}
	sort.Strings(names)
	text := "eva-eval: " + msg + "\nusage:"
	for _, name := range names {
		text += "\n  eva-eval pilot " + pilotCommands[name].synopsis
	}
	fmt.Fprintln(w, text)
	return 2
}

// pilotCheckLimit bounds each test run of eva-eval pilot check.
var pilotCheckLimit = 5 * time.Minute

// eva-eval pilot check applies every rule to the pilot's tasks: it
// prints one line a task, its id, its family, and ok or the rule it
// breaks, and exits 0 when every task keeps them, 1 when one does not,
// and 2 for a usage error or when it cannot read the tasks or run their
// tests.
func pilotCheck(ctx context.Context, args []string, out, errOut io.Writer) int {
	f := flag.NewFlagSet("eva-eval pilot check", flag.ContinueOnError)
	f.SetOutput(errOut)
	dir := f.String("tasks", filepath.Join("eval", "pilot", "testdata", "tasks"), "the folder of tasks")
	if err := f.Parse(args); err != nil {
		return 2
	}
	if f.NArg() != 0 {
		return pilotUsage(errOut, "pilot check takes no argument but its flag")
	}
	tasks, err := task.Load(*dir)
	refused := map[string]*task.Error{}
	if err != nil {
		var joined interface{ Unwrap() []error }
		if !errors.As(err, &joined) {
			return fail(errOut, err)
		}
		for _, e := range joined.Unwrap() {
			var te *task.Error
			if !errors.As(e, &te) {
				return fail(errOut, e)
			}
			refused[te.Task] = te
		}
	}
	scratch, err := os.MkdirTemp("", "eva-eval-pilot-")
	if err != nil {
		return fail(errOut, err)
	}
	defer os.RemoveAll(scratch)

	lines := map[string]string{}
	code := 0
	for id, te := range refused {
		lines[id] = broke(te)
		code = 1
	}
	for _, t := range tasks {
		err := task.Check(ctx, t, filepath.Join(scratch, t.ID), pilotCheckLimit)
		var te *task.Error
		switch {
		case err == nil:
			lines[t.ID] = t.ID + " " + t.Family + " ok"
		case errors.As(err, &te):
			lines[t.ID] = broke(te)
			code = max(code, 1)
		default:
			lines[t.ID] = t.ID + " " + t.Family + " cannot be checked: " + err.Error()
			code = 2
		}
	}
	ids := make([]string, 0, len(lines))
	for id := range lines {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		fmt.Fprintln(out, lines[id])
	}
	return code
}

// broke is a task's line for a rule it breaks.
func broke(e *task.Error) string {
	family := e.Family
	if family == "" {
		family = "-"
	}
	return e.Task + " " + family + " " + e.Rule + ": " + e.Why
}
