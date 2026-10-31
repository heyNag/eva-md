package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"time"

	"github.com/heyNag/eva-md/internal/config"
	"github.com/heyNag/eva-md/internal/paths"
	"github.com/heyNag/eva-md/internal/systempage"
)

const systemUsage = "usage: eva-md system write <slug> [--replace] < page.md"

// runSystem runs eva-md system write <slug> [--replace]: it reads one system
// page from stdin and writes it to the wiki with the system map, from any
// folder. It prints one line, "wrote systems/<slug>.md (<n> bytes); the map
// has <m> systems", or the problems it refused the page for, each naming its
// lines and never their text. It exits 0 on a write, 1 on a refusal and 2
// on a usage error.
func runSystem(ctx context.Context, args []string, in io.Reader, out, errOut io.Writer) int {
	if len(args) < 2 || args[1] != "write" {
		return usage(errOut, systemUsage)
	}
	f := flag.NewFlagSet("system write", flag.ContinueOnError)
	f.SetOutput(io.Discard)
	replace := f.Bool("replace", false, "")
	rest, err := interleaved(f, args[2:])
	if err != nil || len(rest) != 1 {
		return usage(errOut, systemUsage)
	}
	slug := rest[0]
	if err := systempage.CheckSlug(slug); err != nil {
		return usage(errOut, err.Error())
	}
	if terminal(in) {
		return usage(errOut, "system write reads the page from stdin; "+systemUsage)
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
	r, err := systempage.Write(ctx, systempage.Options{Config: c, Dirs: dirs, Version: version, Now: time.Now}, slug, *replace, in)
	var refused *systempage.Refusal
	switch {
	case errors.As(err, &refused):
		fmt.Fprintf(errOut, "eva-md: systems/%s.md was not written:\n", slug)
		for _, p := range refused.Problems {
			fmt.Fprintln(errOut, "  "+p)
		}
		return 1
	case err != nil:
		fmt.Fprintln(errOut, "eva-md:", err)
		return 1
	}
	systems := "systems"
	if r.Systems == 1 {
		systems = "system"
	}
	fmt.Fprintf(out, "wrote %s (%d bytes); the map has %d %s\n", r.Page, r.Bytes, r.Systems, systems)
	return 0
}

// terminal reports a stdin that is a terminal, with no page piped in.
func terminal(in io.Reader) bool {
	f, ok := in.(*os.File)
	if !ok {
		return false
	}
	info, err := f.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}
