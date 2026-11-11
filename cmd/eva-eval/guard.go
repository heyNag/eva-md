package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/heyNag/eva-md/internal/eval/guard"
)

// eva-eval paths checks that each commit in a range stays inside the
// paths its cards own, and eva-eval size that the Go built into eva-md,
// net of v1 lines scheduled for deletion, is within v1's size. Both exit
// 0 when the check holds, 1 when it fails, and 2 for a usage error or a
// repository they cannot read. The blocks a card carries and the trailer a
// commit carries are in the guard package's doc comment.
func init() {
	register(instrument{
		name:     "paths",
		synopsis: "paths [--repo dir] FROM TO",
		run:      pathsCommand,
	})
	register(instrument{
		name:     "size",
		synopsis: "size [--repo dir]",
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
			fmt.Fprintf(out, "%s has no Card: trailer: the lead's, not judged (%d paths)\n", id, j.Paths)
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
	if err := f.Parse(args); err != nil {
		return 2
	}
	if f.NArg() != 0 {
		return usage(errOut, "size takes no argument but its flag")
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
