package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/heyNag/eva-md/internal/paths"
	"github.com/heyNag/eva-md/internal/searchindex"
)

// runIndex runs eva-md index [--rebuild] [--json]: it brings the search
// index up to date with the inbox now, as the daily maintain does, or with
// --rebuild builds it again from nothing. Search works without it, scanning
// what it doesn't cover. It prints a line for each project folder it
// indexed, merged or built again, then a total, counts only; with --json,
// the index's status instead. A catch-up already running prints
// "index: busy" and exits 0; a failure exits 1.
func runIndex(ctx context.Context, args []string, out, errOut io.Writer) int {
	f := flag.NewFlagSet("index", flag.ContinueOnError)
	f.SetOutput(io.Discard)
	rebuild := f.Bool("rebuild", false, "")
	asJSON := f.Bool("json", false, "")
	if err := f.Parse(args[1:]); err != nil || f.NArg() != 0 {
		return usage(errOut, "invalid arguments for index")
	}
	dirs, err := paths.Default()
	if err != nil {
		fmt.Fprintln(errOut, "eva-md: home folder is unavailable")
		return 1
	}
	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()
	catchup := searchindex.Catchup
	if *rebuild {
		catchup = searchindex.Rebuild
	}
	r, err := catchup(ctx, dirs, "index")
	if r.Busy {
		fmt.Fprintln(out, "index: busy")
		return 0
	}
	if !*asJSON {
		for _, p := range r.Projects {
			if p.Files > 0 || p.Merged > 0 || p.Rebuilt || p.Unreadable > 0 || p.Busy {
				fmt.Fprintf(out, "index: %s: %s\n", p.Name, indexSummary(p.Report, false))
			}
		}
		fmt.Fprintln(out, "index: "+indexSummary(r, true))
	}
	if err != nil {
		fmt.Fprintln(errOut, "eva-md: index:", err)
		return 1
	}
	if *asJSON {
		if err := json.NewEncoder(out).Encode(searchindex.Inspect(dirs)); err != nil {
			return 1
		}
	}
	return 0
}

// indexStep brings the search index up to date, the step maintain runs
// with or without a wiki, and returns the step's exit code.
func indexStep(ctx context.Context, dirs paths.Dirs, out, errOut io.Writer) int {
	r, err := searchindex.Catchup(ctx, dirs, "maintain")
	switch {
	case r.Busy:
		fmt.Fprintln(out, "maintain: index: busy")
		return 0
	case err != nil:
		fmt.Fprintln(errOut, "eva-md: index:", err)
		return 1
	}
	fmt.Fprintln(out, "maintain: index: "+indexSummary(r, true))
	return 0
}

// indexSummary words a catch-up's report, such as "2 files (3.1 MB)
// indexed in 1 project, 4 segments merged, 12.0 MB in all", or "up to date
// (12.0 MB)" when it found nothing to do. total counts the projects, as a
// catch-up's total does.
func indexSummary(r searchindex.Report, total bool) string {
	var parts []string
	if r.Files > 0 {
		part := fmt.Sprintf("%d %s (%.1f MB) indexed", r.Files, plural(r.Files, "file", "files"), float64(r.Bytes)/1e6)
		if total {
			n := 0
			for _, p := range r.Projects {
				if p.Files > 0 {
					n++
				}
			}
			part += fmt.Sprintf(" in %d %s", n, plural(n, "project", "projects"))
		}
		parts = append(parts, part)
	}
	if r.Merged > 0 {
		parts = append(parts, fmt.Sprintf("%d %s merged", r.Merged, plural(r.Merged, "segment", "segments")))
	}
	rebuilt, busy := 0, 0
	if r.Rebuilt {
		rebuilt = 1
	}
	for _, p := range r.Projects {
		if p.Rebuilt {
			rebuilt++
		}
		if p.Busy {
			busy++
		}
	}
	if rebuilt > 0 && total {
		parts = append(parts, fmt.Sprintf("%d %s built again", rebuilt, plural(rebuilt, "project folder", "project folders")))
	} else if rebuilt > 0 {
		parts = append(parts, "built again")
	}
	if r.Unreadable > 0 {
		parts = append(parts, fmt.Sprintf("%d %s unreadable", r.Unreadable, plural(r.Unreadable, "file", "files")))
	}
	switch {
	case r.Busy:
		parts = append(parts, "busy")
	case busy > 0:
		parts = append(parts, fmt.Sprintf("%d %s busy", busy, plural(busy, "project folder", "project folders")))
	}
	if len(parts) == 0 {
		return fmt.Sprintf("up to date (%.1f MB)", float64(r.Size)/1e6)
	}
	return strings.Join(parts, ", ") + fmt.Sprintf(", %.1f MB in all", float64(r.Size)/1e6)
}
