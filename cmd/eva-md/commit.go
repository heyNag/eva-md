package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/heyNag/eva-md/internal/config"
	"github.com/heyNag/eva-md/internal/paths"
	"github.com/heyNag/eva-md/internal/wikigit"
)

// runCommit runs eva-md commit [--push-only]: the daily commit and push now,
// with the guards maintain uses, or with --push-only a push of commits made
// earlier. It prints one line: the outcome, the reason when there is one,
// and the push result.
func runCommit(ctx context.Context, args []string, out, errOut io.Writer) int {
	f := flag.NewFlagSet("commit", flag.ContinueOnError)
	f.SetOutput(io.Discard)
	pushOnly := f.Bool("push-only", false, "")
	if err := f.Parse(args[1:]); err != nil || f.NArg() != 0 {
		return usage(errOut, "invalid arguments for commit")
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
	commit := wikigit.Commit
	if *pushOnly {
		commit = wikigit.Push
	}
	r, err := commit(ctx, wikigit.Options{Config: c, Dirs: dirs})
	fmt.Fprintln(out, commitLine(r))
	if err != nil {
		fmt.Fprintln(errOut, "eva-md:", err)
		return 1
	}
	switch {
	case r.Outcome == wikigit.Refused || r.Outcome == wikigit.Failed:
		return 1
	case r.Reason == wikigit.ReasonPushOnly && r.Outcome == wikigit.Nothing && r.Push != wikigit.PushOK && r.Push != wikigit.PushUpToDate:
		return 1
	}
	return 0
}

// commitLine describes a run in one line, such as
// "committed (3 pages, 1 left out), push ok" or "refused (origin), push skipped".
func commitLine(r wikigit.Result) string {
	if r.Reason == wikigit.ReasonPushOnly {
		if r.Outcome == wikigit.Busy {
			return "busy, push skipped"
		}
		return "push " + r.Push
	}
	var details []string
	switch {
	case r.Outcome == wikigit.Committed && r.Pages == 1:
		details = append(details, "1 page")
	case r.Outcome == wikigit.Committed:
		details = append(details, fmt.Sprintf("%d pages", r.Pages))
	case r.Reason != "":
		details = append(details, r.Reason)
	}
	if r.LockAge > 0 {
		details = append(details, fmt.Sprintf("lock %d s old", r.LockAge))
	}
	if n := len(r.Unstaged); n > 0 {
		details = append(details, fmt.Sprintf("%d left out", n))
	}
	s := r.Outcome
	if len(details) > 0 {
		s += " (" + strings.Join(details, ", ") + ")"
	}
	return s + ", push " + r.Push
}
