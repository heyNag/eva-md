package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"path/filepath"

	"github.com/heyNag/eva-md/internal/eval/continuation/report"
	"github.com/heyNag/eva-md/internal/eval/continuation/task"
	"github.com/heyNag/eva-md/internal/eval/results"
)

// eva-eval continuation report reads the rows and run records of results
// folders and prints the pilot's figures (EV-03c): what the runs did, what
// the judged trials measured, a session's cost, the variance between
// conditions and the tasks' screens, by host, task and condition. It
// starts nothing and reads no host's output.
func init() {
	addContinuation("report", continuationCommand{
		synopsis: "report --tasks DIR [--catalogue FILE] [--rates IN,CACHED,OUT] OUT...",
		run:      continuationReport,
	})
}

// continuationReport prints the report and exits 0, or exits 2 with no
// figure for a usage error or data it refuses. Its messages name a results
// folder by its place among the arguments, never by its path, and do not
// pass on the loaders' errors, which can hold one.
func continuationReport(_ context.Context, args []string, out, errOut io.Writer) int {
	f := flag.NewFlagSet("eva-eval continuation report", flag.ContinueOnError)
	f.SetOutput(io.Discard)
	tasksDir := f.String("tasks", "", "")
	catalogue := f.String("catalogue", filepath.Join("eval", "continuation", "metrics.json"), "")
	ratesFlag := f.String("rates", "", "")
	if err := f.Parse(args); err != nil {
		// The flag package's message can quote a value, so it is not shown.
		return continuationFail(errOut, "a flag of continuation report is unknown or has no valid value")
	}
	if *tasksDir == "" || f.NArg() == 0 {
		return continuationFail(errOut, "continuation report takes --tasks and one results folder or more")
	}
	var rates *report.Rates
	if *ratesFlag != "" {
		r, err := report.ParseRates(*ratesFlag)
		if err != nil {
			return continuationFail(errOut, err.Error())
		}
		rates = &r
	}
	refuse := func(msg string) int {
		fmt.Fprintln(errOut, "eva-eval: continuation report: "+msg)
		return 2
	}
	if err := report.SizeOK(*catalogue); err != nil {
		return refuse("the catalogue " + err.Error())
	}
	cat, err := results.LoadCatalogue(*catalogue)
	if err != nil {
		return refuse("the catalogue cannot be read")
	}
	tasks, err := task.LoadAll(*tasksDir)
	if err != nil {
		return refuse("the tasks cannot be read")
	}
	var folders []report.Folder
	for i, dir := range f.Args() {
		folder, err := report.ReadFolder(dir, cat)
		if err != nil {
			return refuse(fmt.Sprintf("folder %d: %v", i+1, err))
		}
		folders = append(folders, folder)
	}
	r, err := report.Build(tasks, folders, rates)
	if err != nil {
		return refuse(err.Error())
	}
	text, err := r.Text()
	if err != nil {
		return refuse(err.Error())
	}
	fmt.Fprint(out, text)
	return 0
}
