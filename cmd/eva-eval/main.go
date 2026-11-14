// Command eva-eval is eva-md's evaluation harness: it judges sets of
// result rows, which the instruments write, against the accepted baseline.
// It is a tool for eva-md's developers, kept apart from eva-md: no release
// or install builds it, and eva-md never imports it.
//
//	eva-eval gate [--repo dir] [--base rev] [--catalogue file] [--gates file] [--baseline file] rows
//	eva-eval diff [--catalogue file] first second
//	eva-eval baseline accept [--repo dir] [--catalogue file] [--out file] rows
//
// A set of rows is a results folder or its rows file. The catalogue, the
// gates and the baseline default to spec/metrics.json, eval/gates.json and
// eval/baselines/baseline.json in the repository.
//
// An instrument, which measures something and writes rows, adds its own
// command from its own file with register, as "eva-eval <name> ...", and
// the usage lists it after these three.
//
// gate prints a line that names the rows' commit, whether its tree was
// dirty, the baseline's commit and the base, then one line for each gate,
// and exits 0 when every gate holds and 1 when one fails; every command
// exits 2 for a usage error or data it cannot read.
//
// The base is the commit the change being gated starts from. The baseline
// is stale, and fails its gates, when a path its metric covers changed
// between the baseline's commit and the base: the main line moved on, and
// a new baseline must be accepted there. A change on the rows' own branch
// is what the gates judge, so it never makes the baseline stale. By
// default the base is the merge base of the rows' commit and main (or
// origin/main), so the repository must hold the rows' commit; --base
// names it outright.
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"

	"github.com/heyNag/eva-md/internal/eval/results"
)

func main() {
	// A hang-up ends a run as an interrupt does: a driver that a dropped
	// connection killed outright would leave the chat it started, which
	// runs in a process group of its own, with no limit and no record.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM, syscall.SIGHUP)
	code := run(ctx, os.Args[1:], os.Stdout, os.Stderr)
	stop()
	os.Exit(code)
}

const usageText = `usage:
  eva-eval gate [--repo dir] [--base rev] [--catalogue file] [--gates file] [--baseline file] rows
  eva-eval diff [--catalogue file] first second
  eva-eval baseline accept [--repo dir] [--catalogue file] [--out file] rows`

// An instrument is a command that an instrument's own file adds.
type instrument struct {
	name     string // the command's name, the first argument
	synopsis string // its usage line, after "eva-eval "
	run      func(ctx context.Context, args []string, out, errOut io.Writer) int
}

// instruments holds the commands the instruments added, by name.
var instruments = map[string]instrument{}

// register adds an instrument's command. Its file calls it from init, so
// adding an instrument changes no other file. It panics on a name that is
// taken, that is not lower-case letters and dashes, or on a command with
// no usage line or nothing to run: each is a mistake in the program.
func register(i instrument) {
	_, taken := instruments[i.name]
	switch {
	case i.name == "gate" || i.name == "diff" || i.name == "baseline" || taken:
		panic("eva-eval: the command " + i.name + " is registered twice")
	case i.name == "" || strings.Trim(i.name, "abcdefghijklmnopqrstuvwxyz-") != "":
		panic("eva-eval: a command's name is lower-case letters and dashes, not " + strconv.Quote(i.name))
	case !strings.HasPrefix(i.synopsis, i.name) || strings.Contains(i.synopsis, "\n") || i.run == nil:
		panic("eva-eval: the command " + i.name + " needs a usage line that starts with its name, and something to run")
	}
	instruments[i.name] = i
}

// usageLines is the usage: the three commands, then the instruments' by
// name.
func usageLines() string {
	names := make([]string, 0, len(instruments))
	for name := range instruments {
		names = append(names, name)
	}
	sort.Strings(names)
	text := usageText
	for _, name := range names {
		text += "\n  eva-eval " + instruments[name].synopsis
	}
	return text
}

// run runs the command and returns its exit code.
func run(ctx context.Context, args []string, out, errOut io.Writer) int {
	if len(args) == 0 {
		return usage(errOut, "it needs a command")
	}
	switch {
	case args[0] == "gate":
		return gate(ctx, args[1:], out, errOut)
	case args[0] == "diff":
		return diff(args[1:], out, errOut)
	case args[0] == "baseline" && len(args) > 1 && args[1] == "accept":
		return accept(ctx, args[2:], out, errOut)
	}
	if i, ok := instruments[args[0]]; ok {
		return i.run(ctx, args[1:], out, errOut)
	}
	return usage(errOut, "there is no command "+args[0])
}

func usage(w io.Writer, msg string) int {
	fmt.Fprintln(w, "eva-eval: "+msg+"\n"+usageLines())
	return 2
}

// fail reports an error that stops a command.
func fail(w io.Writer, err error) int {
	fmt.Fprintln(w, "eva-eval: "+err.Error())
	return 2
}

// files are the flags that name the repository and its evaluation files.
type files struct {
	repo, catalogue *string
}

func newFlags(name string, errOut io.Writer) (*flag.FlagSet, files) {
	f := flag.NewFlagSet("eva-eval "+name, flag.ContinueOnError)
	f.SetOutput(errOut)
	return f, files{
		repo:      f.String("repo", ".", "the repository the rows were measured on"),
		catalogue: f.String("catalogue", "", "the metric catalogue; spec/metrics.json in the repository when empty"),
	}
}

// inRepo returns the flag's value, or the default file in the repository.
func inRepo(fs files, value, def string) string {
	if value != "" {
		return value
	}
	return filepath.Join(*fs.repo, filepath.FromSlash(def))
}

func gate(ctx context.Context, args []string, out, errOut io.Writer) int {
	f, fs := newFlags("gate", errOut)
	base := f.String("base", "", "the commit the change starts from, which the baseline must still describe; the merge base of the rows' commit and main when empty")
	gatesFile := f.String("gates", "", "the gates; eval/gates.json in the repository when empty")
	baseFile := f.String("baseline", "", "the accepted baseline; eval/baselines/baseline.json in the repository when empty")
	if err := f.Parse(args); err != nil {
		return 2
	}
	if f.NArg() != 1 {
		return usage(errOut, "gate takes one set of rows")
	}
	cat, err := results.LoadCatalogue(inRepo(fs, *fs.catalogue, "spec/metrics.json"))
	if err != nil {
		return fail(errOut, err)
	}
	gates, err := results.LoadGates(inRepo(fs, *gatesFile, "eval/gates.json"), cat)
	if err != nil {
		return fail(errOut, err)
	}
	b, err := results.LoadBaseline(inRepo(fs, *baseFile, "eval/baselines/baseline.json"), cat)
	if err != nil {
		return fail(errOut, err)
	}
	rows, err := results.Load(f.Arg(0))
	if err == nil {
		err = cat.Check(rows)
	}
	if err != nil {
		return fail(errOut, err)
	}
	if *base == "" {
		if *base, err = results.StartOf(ctx, *fs.repo, rows); err != nil {
			return fail(errOut, fmt.Errorf("cannot find the commit the change starts from: %w; name it with --base", err))
		}
	}
	verdicts := results.Evaluate(ctx, *fs.repo, *base, cat, gates, rows, b)
	fmt.Fprintln(out, results.Header(rows, b, *base))
	for _, v := range verdicts {
		fmt.Fprintln(out, v.Line)
	}
	if !results.Hold(verdicts) {
		return 1
	}
	return 0
}

func diff(args []string, out, errOut io.Writer) int {
	f, fs := newFlags("diff", errOut)
	if err := f.Parse(args); err != nil {
		return 2
	}
	if f.NArg() != 2 {
		return usage(errOut, "diff takes two sets of rows")
	}
	cat, err := results.LoadCatalogue(inRepo(fs, *fs.catalogue, "spec/metrics.json"))
	if err != nil {
		return fail(errOut, err)
	}
	var sets [2][]results.Row
	for i := range sets {
		if sets[i], err = results.Load(f.Arg(i)); err != nil {
			return fail(errOut, err)
		}
	}
	if err := results.Diff(out, cat, sets[0], sets[1]); err != nil {
		return fail(errOut, err)
	}
	return 0
}

func accept(ctx context.Context, args []string, out, errOut io.Writer) int {
	f, fs := newFlags("baseline accept", errOut)
	outFile := f.String("out", "", "the baseline to write; eval/baselines/baseline.json in the repository when empty")
	if err := f.Parse(args); err != nil {
		return 2
	}
	if f.NArg() != 1 {
		return usage(errOut, "baseline accept takes one set of rows")
	}
	cat, err := results.LoadCatalogue(inRepo(fs, *fs.catalogue, "spec/metrics.json"))
	if err != nil {
		return fail(errOut, err)
	}
	rows, err := results.Load(f.Arg(0))
	if err != nil {
		return fail(errOut, err)
	}
	file := inRepo(fs, *outFile, "eval/baselines/baseline.json")
	b, err := results.Accept(ctx, *fs.repo, cat, rows, file)
	if err != nil {
		return fail(errOut, err)
	}
	fmt.Fprintf(out, "Accepted %d rows of commit %s as the baseline in %s.\n", len(b.Rows), b.Commit, file)
	return 0
}
