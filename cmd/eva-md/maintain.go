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
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/heyNag/eva-md/internal/catchup"
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

func init() {
	register(command{name: "maintain", file: "maintain.go", place: 160,
		synopsis: []string{"eva-md maintain [--push-only]"},
		run: func(ctx context.Context, args []string, _ io.Reader, out, errOut io.Writer) int {
			return runMaintain(ctx, args, out, errOut)
		}})
}

// runMaintain runs eva-md maintain [--push-only], the daily job the first
// hook event of a day spawns detached: the files built from the knowledge
// pages, when they are out of date (see knowledgeStep), the day's commit and
// push, unless commits = false in the config, then the lint, which writes
// <state>/lint.json, the code status of the system pages (see codeStep),
// the removal of attempt markers older than 14 days, and that of the
// transcript-recovery records not saved for 30 days or past the newest
// 2,048 of a host (see pruneRecords).
// With --push-only, which a chat start spawns while a push is pending, it
// only pushes, and only while the newest commit record's push is still
// pending. It runs under <locks>/maintain.lock; a run that finds it held
// exits 0 at once. It prints a line a step, counts and reasons only, and
// exits 1 when a step was refused or failed. Last it brings the search
// index up to date (see indexStep). Without a wiki, as in a capture-only
// install, it runs only that step and then the two removals.
func runMaintain(ctx context.Context, args []string, out, errOut io.Writer) int {
	code, _ := maintain(ctx, args, out, errOut)
	return code
}

// maintain is runMaintain, and also reports whether maintain.lock was held,
// so that the worker's maintain job can keep its marks for the next run.
func maintain(ctx context.Context, args []string, out, errOut io.Writer) (code int, busy bool) {
	f := flag.NewFlagSet("maintain", flag.ContinueOnError)
	f.SetOutput(io.Discard)
	pushOnly := f.Bool("push-only", false, "")
	if err := f.Parse(args[1:]); err != nil || f.NArg() != 0 {
		return usage(errOut, "invalid arguments for maintain"), false
	}
	dirs, err := paths.Default()
	if err != nil {
		fmt.Fprintln(errOut, "eva-md: home folder is unavailable")
		return 1, false
	}
	c, err := config.Load(dirs.ConfigFile())
	wiki := err == nil && c.WikiPath != ""
	if !wiki && (*pushOnly || err != nil && !errors.Is(err, fs.ErrNotExist)) {
		fmt.Fprintln(errOut, "eva-md: no wiki is set up; run eva-md setup first")
		return 1, false
	}
	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()
	unlock, busy, err := update.TryLock(dirs.Locks(), "maintain.lock")
	if busy {
		fmt.Fprintln(out, "maintain: busy")
		return 0, true
	}
	if err != nil {
		fmt.Fprintln(errOut, "eva-md: maintain:", err)
		return 1, false
	}
	defer unlock()
	if !wiki {
		// A capture-only install: maintain keeps the search index, and
		// prunes the markers and records the hooks leave.
		code = indexStep(ctx, dirs, out, errOut)
		pruneAttempts(dirs.State, time.Now())
		pruneRecords(ctx, dirs, time.Now(), out)
		return code, false
	}
	code = 0
	if !*pushOnly {
		code = knowledgeStep(c, dirs, out, errOut)
	}
	if commitStep(ctx, c, dirs, *pushOnly, out, errOut) != 0 {
		code = 1
	}
	if *pushOnly {
		return code, false
	}
	r, err := lint.Run(ctx, c.WikiPath, dirs)
	if err != nil {
		fmt.Fprintln(errOut, "eva-md: lint:", err)
		code = 1
	} else {
		fmt.Fprintf(out, "maintain: lint: %d thread pages, %d over 512 KB, %d bad refs\n", r.Pages, len(r.Oversize), r.BadRefs)
	}
	if codeStep(ctx, c, dirs, out, errOut) != 0 {
		code = 1
	}
	pruneAttempts(dirs.State, time.Now())
	pruneRecords(ctx, dirs, time.Now(), out)
	if indexStep(ctx, dirs, out, errOut) != 0 {
		code = 1
	}
	return code, false
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

// pruneRecords removes the transcript-recovery records, in
// <state>/catchup/<host>/, not saved for 30 days or past the newest 2,048
// of a host, so the chat-start catch-up, which lists at most 4,096, still
// finds the newest; it never removes the drift records beside them. It
// prints a line only when it removed any. A record it can't remove waits
// for the next run, as an attempt marker does.
func pruneRecords(ctx context.Context, dirs paths.Dirs, now time.Time, out io.Writer) {
	n, _ := catchup.Prune(ctx, dirs, now)
	switch {
	case n == 1:
		fmt.Fprintln(out, "maintain: transcript recovery: removed 1 record")
	case n > 1:
		fmt.Fprintf(out, "maintain: transcript recovery: removed %d records\n", n)
	}
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
