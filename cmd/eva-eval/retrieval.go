package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/heyNag/eva-md/internal/eval/results"
	"github.com/heyNag/eva-md/internal/eval/retrieval"
)

// eva-eval retrieval scores a retrieval system on the labelled set: it
// asks the system for each of the set's prompts, appends a row for each
// figure to the results folder, and prints the figures and the ids of the
// prompts given an item that is not relevant, never a prompt's text.
func init() {
	register(instrument{
		name:     "retrieval",
		synopsis: "retrieval [--repo dir] [--set dir] --system name --out dir",
		run:      retrievalCommand,
	})
}

func retrievalCommand(ctx context.Context, args []string, out, errOut io.Writer) int {
	f := flag.NewFlagSet("eva-eval retrieval", flag.ContinueOnError)
	f.SetOutput(errOut)
	repo := f.String("repo", ".", "the repository measured, whose commit the rows name")
	setDir := f.String("set", "", "the labelled set; eval/retrieval/set in the repository when empty")
	system := f.String("system", "", "the system to score: one of "+strings.Join(retrieval.Systems(), ", "))
	outDir := f.String("out", "", "the results folder the rows are appended to")
	if err := f.Parse(args); err != nil {
		return 2
	}
	switch {
	case f.NArg() != 0:
		return usage(errOut, "retrieval takes no argument but its flags")
	case *system == "" || *outDir == "":
		return usage(errOut, "retrieval needs --system and --out")
	case !slices.Contains(retrieval.Systems(), *system):
		return fail(errOut, fmt.Errorf("there is no system %s; the systems are %s", *system, strings.Join(retrieval.Systems(), ", ")))
	}
	dir := *setDir
	if dir == "" {
		dir = filepath.Join(*repo, "eval", "retrieval", "set")
	}
	set, err := retrieval.Load(dir)
	if err != nil {
		return fail(errOut, err)
	}
	commit, dirty, err := results.Source(ctx, *repo)
	if err != nil {
		return fail(errOut, err)
	}
	sys, err := retrieval.Build(*system, set)
	if err != nil {
		return fail(errOut, err)
	}
	res, err := retrieval.Score(ctx, set, *system, sys)
	if err != nil {
		return fail(errOut, err)
	}
	store := results.Store{Dir: *outDir, Forbid: set.Forbid()}
	if err := store.Append(res.Rows(commit, dirty, time.Now())...); err != nil {
		return fail(errOut, err)
	}
	res.Table(out)
	fmt.Fprintf(out, "Wrote %d rows of commit %s to %s.\n", len(res.Figures), commit[:12], filepath.Join(*outDir, results.FileName))
	return 0
}
