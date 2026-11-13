package main

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/heyNag/eva-md/internal/eval/guard"
)

// eva-eval paths checks that each commit in a range stays inside the
// paths its cards own, and eva-eval size that the Go built into eva-md,
// net of v1 lines scheduled for deletion, is within v1's size. Both exit
// 0 when the check holds, 1 when it fails, and 2 for a usage error or a
// repository they cannot read. The blocks a card carries and the Card: line
// a commit carries are in the guard package's doc comment.
//
// eva-eval size --ledger FILE, with FILE read under --repo, checks the size
// ledger (guard.LedgerFile) too: after size's report it prints the
// repository's HEAD, for the ledger's counted-at, each of the ledger's rows
// and figures as they count today, each figure that is not today's with
// the line to write, and the ledger's faults. It exits 1 on a difference
// or a fault, whatever size's own check says, 0 otherwise, and 2 for a
// usage error, a ledger it cannot read or a repository without v1's
// history.
func init() {
	register(instrument{
		name:     "paths",
		synopsis: "paths [--repo dir] FROM TO",
		run:      pathsCommand,
	})
	register(instrument{
		name:     "size",
		synopsis: "size [--repo dir] [--ledger file]",
		run:      sizeCommand,
	})
}

func pathsCommand(ctx context.Context, args []string, out, errOut io.Writer) int {
	f := flag.NewFlagSet("eva-eval paths", flag.ContinueOnError)
	f.SetOutput(errOut)
	repo := f.String("repo", ".", "the repository whose commits are judged")
	if err := f.Parse(args); err != nil {
		return 2
	}
	if f.NArg() != 2 {
		return usage(errOut, "paths takes the two commits of a range, FROM and TO")
	}
	judged, err := guard.Ownership(ctx, *repo, f.Arg(0), f.Arg(1))
	if err != nil {
		return fail(errOut, err)
	}
	lead, faults := 0, 0
	for _, j := range judged {
		id := j.Commit[:12]
		switch {
		case j.Lead():
			lead++
			fmt.Fprintf(out, "%s has no Card: line: the lead's, not judged (%d paths)\n", id, j.Paths)
		case len(j.Faults) == 0:
			fmt.Fprintf(out, "%s %s: its %d paths are inside its card's paths\n", id, strings.Join(j.Cards, ", "), j.Paths)
		}
		for _, fault := range j.Faults {
			faults++
			fmt.Fprintf(out, "%s %s\n", id, fault)
		}
	}
	fmt.Fprintf(out, "%d commits: %d judged, %d the lead's; %d faults.\n", len(judged), len(judged)-lead, lead, faults)
	if faults > 0 {
		return 1
	}
	return 0
}

func sizeCommand(ctx context.Context, args []string, out, errOut io.Writer) int {
	f := flag.NewFlagSet("eva-eval size", flag.ContinueOnError)
	f.SetOutput(errOut)
	repo := f.String("repo", ".", "the root of the repository whose binary is counted")
	ledgerFile := f.String("ledger", "", "the size ledger to check, from the repository's root")
	if err := f.Parse(args); err != nil {
		return 2
	}
	if f.NArg() != 0 {
		return usage(errOut, "size takes no argument but its flags")
	}
	if *ledgerFile != "" {
		return ledgerCommand(ctx, *repo, *ledgerFile, out, errOut)
	}
	built, err := guard.Built(ctx, *repo)
	if err != nil {
		return fail(errOut, err)
	}
	size, err := guard.Count(*repo, built, guard.Ceiling, func() (map[string]bool, error) {
		return guard.V1Files(ctx, *repo)
	})
	if err != nil {
		return fail(errOut, err)
	}
	fmt.Fprint(out, size.Report())
	if !size.Holds() {
		return 1
	}
	return 0
}

// ledgerCommand is eva-eval size --ledger file.
func ledgerCommand(ctx context.Context, repo, file string, out, errOut io.Writer) int {
	text, err := os.ReadFile(filepath.Join(repo, filepath.FromSlash(file)))
	if err != nil {
		return fail(errOut, err)
	}
	v1, err := guard.V1Files(ctx, repo)
	if err != nil {
		return fail(errOut, err)
	}
	had := func() (map[string]bool, error) { return v1, nil }
	head, err := exec.CommandContext(ctx, "git", "-C", repo, "rev-parse", "--verify", "HEAD^{commit}").Output()
	if err != nil || len(bytes.TrimSpace(head)) < 12 {
		return fail(errOut, fmt.Errorf("cannot read the repository's HEAD: %v", err))
	}
	built, err := guard.Built(ctx, repo)
	if err != nil {
		return fail(errOut, err)
	}
	size, err := guard.Count(repo, built, guard.Ceiling, had)
	if err != nil {
		return fail(errOut, err)
	}
	ledger, err := guard.CheckLedger(repo, string(text), built, guard.Ceiling, had)
	if err != nil {
		return fail(errOut, err)
	}
	fmt.Fprint(out, size.Report())
	fmt.Fprintf(out, "Ledger %s, counted at %s; HEAD is %s.\n", file, ledger.CountedAt, head[:12])
	fmt.Fprint(out, ledger.Report())
	if !ledger.Holds() {
		return 1
	}
	return 0
}
