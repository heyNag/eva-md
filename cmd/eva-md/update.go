package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/heyNag/eva-md/internal/config"
	"github.com/heyNag/eva-md/internal/paths"
	"github.com/heyNag/eva-md/internal/update"
)

// newRunner starts the host CLIs for an update; tests replace it with a
// fake runner, so no test can start a real claude or codex.
var newRunner = update.NewRunner

// updateClock is the update job's clock, which tests pin, so the day of a
// job can't change between two runs of a test.
var updateClock = time.Now

// sweepDay is update's --sweep: on its own, the day's sweep on the day of
// the job's clock; as a chat start gives it, --sweep=YYYY-MM-DD, on the day
// the hook found it due on (see update.Job.SweepDay).
type sweepDay struct {
	on  bool
	day string
}

func (s *sweepDay) String() string   { return s.day }
func (s *sweepDay) IsBoolFlag() bool { return true }

func (s *sweepDay) Set(value string) error {
	if on, err := strconv.ParseBool(value); err == nil {
		s.on, s.day = on, ""
		return nil
	}
	if at, err := time.Parse(time.DateOnly, value); err != nil || at.Format(time.DateOnly) != value {
		return errors.New("--sweep takes a day, YYYY-MM-DD")
	}
	s.on, s.day = true, value
	return nil
}

func init() {
	register(command{name: "update", file: "update.go", place: 140,
		synopsis: []string{"eva-md update [--session host:id ... | --pending] [--sweep[=YYYY-MM-DD]] [--dry-run] [--foreground]"},
		run: func(ctx context.Context, args []string, _ io.Reader, out, errOut io.Writer) int {
			return runUpdate(ctx, args, out, errOut)
		}})
}

// runUpdate runs eva-md update --session <host>:<id>, given once or more,
// or --pending, and with --sweep, which a person's first chat start of the
// day adds to its catch-up, the day's sweep too (see update.Job.CatchUp).
// With --dry-run it prints the next pass's exact command, the names of the
// child's environment variables, the rules, the input and the schema, and
// makes no call. With --foreground it runs attached, as a person does, with
// progress on stderr; without it, it is the job the triggers spawn detached,
// and its lines go to the day's log. It prints one summary line once the
// job ran. A busy lock, updates = false, the budget, a hold, a backoff or a
// quarantine stops it without an error; a failed pass exits 1.
func runUpdate(ctx context.Context, args []string, out, errOut io.Writer) int {
	f := flag.NewFlagSet("update", flag.ContinueOnError)
	f.SetOutput(io.Discard)
	var sessionArgs []string
	f.Func("session", "", func(s string) error { sessionArgs = append(sessionArgs, s); return nil })
	pending := f.Bool("pending", false, "")
	var sweep sweepDay
	f.Var(&sweep, "sweep", "")
	dry := f.Bool("dry-run", false, "")
	foreground := f.Bool("foreground", false, "")
	if err := f.Parse(args[1:]); err != nil || f.NArg() != 0 {
		return usage(errOut, "invalid arguments for update")
	}
	switch {
	case len(sessionArgs) == 0 && !*pending && !sweep.on:
		return usage(errOut, "update needs --session host:id or --pending, or --sweep")
	case len(sessionArgs) > 0 && *pending:
		return usage(errOut, "update takes --session or --pending, not both")
	case *pending && sweep.on:
		return usage(errOut, "update takes --pending or --sweep, not both")
	case *dry && *pending:
		return usage(errOut, "update --dry-run needs --session")
	case *dry && sweep.on:
		return usage(errOut, "update --dry-run takes no --sweep")
	case *dry && len(sessionArgs) > 1:
		return usage(errOut, "update --dry-run takes one --session")
	}
	var refs []update.Ref
	for _, s := range sessionArgs {
		host, id, _ := strings.Cut(s, ":")
		if id == "" || host != update.HostClaude && host != update.HostCodex {
			return usage(errOut, "update --session takes host:id, with host claude-code or codex")
		}
		refs = append(refs, update.Ref{Host: host, ID: id})
	}
	dirs, err := paths.Default()
	if err != nil {
		fmt.Fprintln(errOut, "eva-md: home folder is unavailable")
		return 1
	}
	c, err := config.Load(dirs.ConfigFile())
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			fmt.Fprintln(errOut, "eva-md: no wiki is set up; run eva-md setup first")
		} else {
			fmt.Fprintln(errOut, "eva-md: configuration is invalid")
		}
		return 1
	}
	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()
	job := update.Job{Dirs: dirs, Config: c, Version: version, Manual: *foreground, Runner: newRunner, Now: updateClock, SweepDay: sweep.day}
	if *dry {
		if err := job.DryRun(ctx, refs[0].Host, refs[0].ID, out); err != nil {
			fmt.Fprintln(errOut, "eva-md:", updateError(err, sessionArgs))
			return 1
		}
		return 0
	}
	job.Log = errOut
	if !*foreground {
		if log, err := update.OpenLog(dirs, time.Now()); err == nil {
			defer log.Close()
			job.Log = log
		}
	}
	var s update.Summary
	if *pending {
		s, err = job.Pending(ctx)
	} else {
		s, err = job.CatchUp(ctx, refs, sweep.on)
	}
	// A job that could not start, such as one whose wiki is missing, did
	// nothing to sum up; one cut short by a signal sums up what it did.
	if err == nil || errors.Is(err, context.Canceled) {
		fmt.Fprintln(out, "update: "+s.Line())
	}
	if err != nil {
		fmt.Fprintln(errOut, "eva-md:", updateError(err, sessionArgs))
		return 1
	}
	if s.Failed() || s.Errors > 0 {
		return 1
	}
	return 0
}

// updateError words a job's error for the terminal.
func updateError(err error, sessionArgs []string) string {
	if errors.Is(err, update.ErrNoSession) {
		return "no inbox file for " + strings.Join(sessionArgs, " or ")
	}
	return err.Error()
}
