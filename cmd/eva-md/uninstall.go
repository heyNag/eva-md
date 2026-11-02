package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/heyNag/eva-md/internal/install"
	"github.com/heyNag/eva-md/internal/paths"
)

// runUninstall previews or removes eva-md: with --apply the program (the
// host plugins and marketplaces, the stable binary and the link on PATH),
// and with --all as well the config and the local data, once the person has
// typed delete in a terminal, or passed --yes where there is none. It never
// touches the wiki folder or its remote, and its last lines say so.
func runUninstall(ctx context.Context, args []string, in io.Reader, out, errOut io.Writer, interactive bool) int {
	f := flag.NewFlagSet("uninstall", flag.ContinueOnError)
	f.SetOutput(io.Discard)
	apply := f.Bool("apply", false, "")
	all := f.Bool("all", false, "")
	yes := f.Bool("yes", false, "")
	if err := f.Parse(args); err != nil || f.NArg() != 0 {
		return usage(errOut, "invalid arguments for uninstall")
	}
	d, err := paths.Default()
	if err != nil {
		fmt.Fprintln(errOut, "eva-md: home folder is unavailable")
		return 1
	}
	opts := install.UninstallOptions{Dirs: d, All: *all}
	pctx, cancel := context.WithTimeout(ctx, installTimeout)
	r, err := install.Uninstall(pctx, opts)
	cancel()
	if err != nil {
		fmt.Fprintln(errOut, "eva-md:", err)
		return 1
	}
	if !*apply {
		fmt.Fprintln(out, "Preview; nothing was changed.")
		listUninstall(out, r, "Would remove:", "Would keep:")
		// The same words fit eva-md uninstall and the uninstall script.
		next := "Add --apply to remove the program, keeping the config and the local data, or --apply --all to remove those too."
		if *all {
			next = "Add --apply to remove all of this."
		}
		fmt.Fprintln(out, next)
		closingLines(out, r)
		return 0
	}
	// On a terminal the person types delete, --yes or not; --yes is the
	// confirmation only where there is no terminal to type it in.
	if *all && (interactive || !*yes) {
		if !interactive {
			fmt.Fprintln(errOut, "eva-md: --all deletes the config and the local data, so it asks you to type delete in a terminal; without one, add --yes. Nothing was removed.")
			return 2
		}
		listUninstall(out, r, "This removes:", "and keeps:")
		fmt.Fprint(out, "Type delete to remove all of this: ")
		answer, _ := bufio.NewReader(in).ReadString('\n')
		if strings.TrimSpace(answer) != "delete" {
			fmt.Fprintln(out, "Nothing was removed.")
			return 1
		}
	}
	opts.Apply = true
	actx, cancel := context.WithTimeout(ctx, installTimeout)
	r, err = install.Uninstall(actx, opts)
	cancel()
	switch {
	case errors.Is(err, install.ErrBusy):
		fmt.Fprintln(errOut, "eva-md:", err)
		return 1
	case err != nil:
		if len(r.Actions) > 0 {
			fmt.Fprintln(out, "Failed. Planned:")
			for _, action := range r.Actions {
				fmt.Fprintln(out, "  "+action)
			}
		}
		fmt.Fprintln(errOut, "eva-md:", err)
		return 1
	}
	listUninstall(out, r, "Removed:", "Kept:")
	closingLines(out, r)
	return 0
}

// listUninstall prints what uninstall removes and what it keeps, the wiki
// last among what it keeps.
func listUninstall(out io.Writer, r install.UninstallResult, removes, keeps string) {
	if len(r.Actions) == 0 {
		fmt.Fprintln(out, "Nothing to remove: eva-md is not installed.")
	} else {
		fmt.Fprintln(out, removes)
		for _, action := range r.Actions {
			fmt.Fprintln(out, "  "+action)
		}
	}
	kept := r.Kept
	switch {
	case r.Wiki != "" && r.Remote != "":
		kept = append(kept, "the wiki folder, "+r.Wiki+", and its remote, "+r.Remote)
	case r.Wiki != "":
		kept = append(kept, "the wiki folder, "+r.Wiki)
	}
	if len(kept) > 0 {
		fmt.Fprintln(out, keeps)
		for _, k := range kept {
			fmt.Fprintln(out, "  "+k)
		}
	}
}

// closingLines says what eva-md leaves to the person: Codex's approval of its
// hooks, and the wiki.
func closingLines(out io.Writer, r install.UninstallResult) {
	fmt.Fprintln(out)
	fmt.Fprintln(out, "Codex keeps its approval of eva-md's hooks in its own config, which eva-md never edits.")
	fmt.Fprintln(out, "To clear it, open Codex, type /hooks and remove eva-md's hooks there.")
	switch {
	case r.Wiki != "" && r.Remote != "":
		fmt.Fprintf(out, "Your wiki is kept in %s, pushed to %s.\n", r.Wiki, r.Remote)
	case r.Wiki != "":
		fmt.Fprintf(out, "Your wiki is kept in %s.\n", r.Wiki)
	default:
		fmt.Fprintln(out, "eva-md never deletes a wiki folder or its remote.")
		return
	}
	fmt.Fprintln(out, "eva-md never deletes the wiki or its remote; deleting them is your own step.")
}
