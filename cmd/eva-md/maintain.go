package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/heyNag/eva-md/internal/config"
	"github.com/heyNag/eva-md/internal/health"
	"github.com/heyNag/eva-md/internal/lint"
	"github.com/heyNag/eva-md/internal/paths"
	"github.com/heyNag/eva-md/internal/update"
	"github.com/heyNag/eva-md/internal/wikigit"
)

// attemptsKept is how many days of the daily trigger's attempt markers
// maintain keeps, as the update keeps its logs.
const attemptsKept = 14

// runMaintain runs eva-md maintain [--push-only], the daily job the first
// hook event of a day spawns detached: the day's commit and push, unless
// commits = false in the config, then the lint, which writes
// <state>/lint.json, and the removal of attempt markers older than 14 days.
// With --push-only, which a chat start spawns while a push is pending, it
// only pushes, and only while the newest commit record's push is still
// pending. It runs under <locks>/maintain.lock; a run that finds it held
// exits 0 at once. It prints a line a step, counts and reasons only, and
// exits 1 when a step was refused or failed.
func runMaintain(ctx context.Context, args []string, out, errOut io.Writer) int {
	f := flag.NewFlagSet("maintain", flag.ContinueOnError)
	f.SetOutput(io.Discard)
	pushOnly := f.Bool("push-only", false, "")
	if err := f.Parse(args[1:]); err != nil || f.NArg() != 0 {
		return usage(errOut, "invalid arguments for maintain")
	}
	dirs, err := paths.Default()
	if err != nil {
		fmt.Fprintln(errOut, "eva-md: home folder is unavailable")
		return 1
	}
	c, err := config.Load(dirs.ConfigFile())
	if err != nil || c.WikiPath == "" {
		fmt.Fprintln(errOut, "eva-md: no wiki is set up; run eva-md setup first")
		return 1
	}
	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()
	unlock, busy, err := update.TryLock(dirs.Locks(), "maintain.lock")
	if busy {
		fmt.Fprintln(out, "maintain: busy")
		return 0
	}
	if err != nil {
		fmt.Fprintln(errOut, "eva-md: maintain:", err)
		return 1
	}
	defer unlock()
	code := commitStep(ctx, c, dirs, *pushOnly, out, errOut)
	if *pushOnly {
		return code
	}
	r, err := lint.Run(ctx, c.WikiPath, dirs)
	if err != nil {
		fmt.Fprintln(errOut, "eva-md: lint:", err)
		code = 1
	} else {
		fmt.Fprintf(out, "maintain: lint: %d thread pages, %d over 512 KB, %d bad refs\n", r.Pages, len(r.Oversize), r.BadRefs)
	}
	pruneAttempts(dirs.State, time.Now())
	return code
}

// commitStep runs the day's commit and push, or with pushOnly the push of a
// pending one, and returns the step's exit code.
func commitStep(ctx context.Context, c config.Config, dirs paths.Dirs, pushOnly bool, out, errOut io.Writer) int {
	o := wikigit.Options{Config: c, Dirs: dirs}
	var r wikigit.Result
	var err error
	switch {
	case !c.Commits:
		fmt.Fprintln(out, "maintain: commits are off (commits = false in the config)")
		return 0
	case pushOnly && !health.RetryPush(dirs, time.Now()):
		fmt.Fprintln(out, "maintain: no push is pending")
		return 0
	case pushOnly:
		r, err = wikigit.Push(ctx, o)
	default:
		r, err = wikigit.Commit(ctx, o)
	}
	fmt.Fprintln(out, "maintain: "+commitLine(r))
	if err != nil {
		fmt.Fprintln(errOut, "eva-md:", err)
		return 1
	}
	if r.Outcome == wikigit.Refused || r.Outcome == wikigit.Failed {
		return 1
	}
	return 0
}

// pruneAttempts removes the daily trigger's markers, <state>/commit/attempt-
// <YYYY-MM-DD>, dated more than 14 days before now.
func pruneAttempts(state string, now time.Time) {
	dir := filepath.Join(state, "commit")
	entries, _ := os.ReadDir(dir)
	cutoff := now.AddDate(0, 0, -attemptsKept).Format(time.DateOnly)
	for _, e := range entries {
		date, ok := strings.CutPrefix(e.Name(), "attempt-")
		if _, err := time.Parse(time.DateOnly, date); ok && err == nil && e.Type().IsRegular() && date < cutoff {
			_ = os.Remove(filepath.Join(dir, e.Name()))
		}
	}
}
