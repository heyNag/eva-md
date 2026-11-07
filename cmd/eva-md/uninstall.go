package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"strings"
	"time"

	"github.com/heyNag/eva-md/internal/install"
	"github.com/heyNag/eva-md/internal/paths"
	"github.com/heyNag/eva-md/internal/wikigit"
)

// unsavedTimeout bounds uninstall's read of the wiki with git. Tests raise
// it before any test runs.
var unsavedTimeout = 10 * time.Second

// unsaved is what the wiki holds that its remote lacks, as uninstall reads
// it with local git, before it removes anything and again after (see
// wikigit.Unsaved).
type unsaved struct {
	commits, files int
	unread         bool // git couldn't read the wiki
}

// readUnsaved reads that of the wiki folder, when there is one. It never
// fetches, commits or pushes, and a failed read is only reported.
func readUnsaved(ctx context.Context, wiki string) unsaved {
	if wiki == "" {
		return unsaved{}
	}
	if _, err := os.Lstat(wiki); errors.Is(err, fs.ErrNotExist) {
		return unsaved{}
	}
	ctx, cancel := context.WithTimeout(ctx, unsavedTimeout)
	defer cancel()
	commits, files, err := wikigit.Unsaved(ctx, wiki)
	if err != nil {
		return unsaved{unread: true}
	}
	return unsaved{commits: commits, files: files}
}

// line says what of the wiki isn't committed or pushed, and how to save it,
// naming eva-md as command, as setup's closing lines do (install.Command):
// with its commit before uninstalling, while eva-md is there to run; once it
// is gone (removed), with git in the wiki folder, or by installing eva-md
// again and running that commit, which --all (all) leaves only for commits:
// setup takes a wiki with changes no commit holds only with the config that
// names it, which --all removes. A wiki the config didn't name before the
// removal (named is false), as when the config was moved aside or can't be
// read, gets git alone, before and after: eva-md's commit works only with
// the config that names the wiki, and a new install's setup refuses a config
// it can't read. It is "" when there is nothing to say.
func (u unsaved) line(command string, removed, all, named bool) string {
	if u.unread {
		return "Git couldn't read the wiki folder, so whether all of it is committed and pushed is unknown; git status there shows what isn't."
	}
	var parts []string
	if u.files > 0 {
		parts = append(parts, fmt.Sprintf("%d %s", u.files, plural(u.files, "changed file isn't committed", "changed files aren't committed")))
	}
	if u.commits > 0 {
		parts = append(parts, fmt.Sprintf("%d %s", u.commits, plural(u.commits, "commit isn't pushed", "commits aren't pushed")))
	}
	if len(parts) == 0 {
		return ""
	}
	they, them := "they are", "them"
	if u.files+u.commits == 1 {
		they, them = "it is", "it"
	}
	run, save, byGit := command+" commit", "commit and push "+them, "commit and push "+them+" with git in the wiki folder"
	if u.files == 0 {
		run, save, byGit = command+" commit --push-only", "push "+them, "push "+them+" with git push in the wiki folder"
	}
	line := "In the wiki, " + strings.Join(parts, " and ") + ", so " + they + " only on this Mac: "
	switch {
	case !named:
		return line + byGit + "."
	case !removed:
		return line + "run " + run + " before you uninstall to " + save + "."
	case all && u.files > 0:
		return line + byGit + "."
	}
	return line + byGit + ", or install eva-md again and run " + run + "."
}

// runUninstall previews or removes eva-md: with --apply the program (the
// host plugins and marketplaces, the stable binary and the link on PATH),
// and with --all as well the config and the local data, once the person has
// typed delete in a terminal, or passed --yes where there is none. It never
// touches the wiki folder or its remote, and its last lines say so, and what
// of the wiki isn't committed or pushed, which it reads with git: before it
// removes anything for the preview and --all's question, and after the
// removal again for the last lines.
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
	// The lines name eva-md as this Mac runs it before anything is removed.
	// Without the stable binary, as after an earlier --apply, when the
	// uninstall script runs the latest release's, there is no eva-md to save
	// the wiki with before uninstalling. With --all, they hold for the Mac
	// once the config is gone too.
	command := install.Command(d)
	info, err := os.Stat(r.Binary)
	gone := err != nil || !info.Mode().IsRegular()
	if !*apply {
		fmt.Fprintln(out, "Preview; nothing was changed.")
		listUninstall(out, r, "Would remove:", "Would keep:")
		// The same words fit eva-md uninstall and the uninstall script.
		next := "Add --apply to remove the program, keeping the config and the local data, or --apply --all to remove those too."
		if *all {
			next = "Add --apply to remove all of this."
		}
		fmt.Fprintln(out, next)
		closingLines(out, r, readUnsaved(ctx, r.Wiki).line(command, gone, *all, r.WikiFromConfig))
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
		if line := readUnsaved(ctx, r.Wiki).line(command, gone, *all, r.WikiFromConfig); line != "" {
			fmt.Fprintln(out, line)
		}
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
	// eva-md is gone now, and with --all the config too, so whether the
	// config named the wiki is as this removal read it, before it removed
	// anything. The wiki is read again, on every path: it may have changed
	// while the question waited or the hosts removed the plugins, as when the
	// person ran eva-md commit, or a page was written.
	closingLines(out, r, readUnsaved(ctx, r.Wiki).line(command, true, *all, r.WikiFromConfig))
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
// hooks, and the wiki, with line, which says what of it isn't committed or
// pushed and how to save it (see unsaved.line).
func closingLines(out io.Writer, r install.UninstallResult, line string) {
	fmt.Fprintln(out)
	fmt.Fprintln(out, "Codex keeps its approval of eva-md's hooks in its own config, which eva-md never edits.")
	fmt.Fprintln(out, "To clear it, open Codex, type /hooks and remove eva-md's hooks there.")
	switch {
	case r.Wiki != "" && r.Remote != "" && line == "":
		fmt.Fprintf(out, "Your wiki is kept in %s, pushed to %s.\n", r.Wiki, r.Remote)
	case r.Wiki != "" && r.Remote != "":
		fmt.Fprintf(out, "Your wiki is kept in %s; its remote is %s.\n", r.Wiki, r.Remote)
		fmt.Fprintln(out, line)
	case r.Wiki != "":
		fmt.Fprintf(out, "Your wiki is kept in %s.\n", r.Wiki)
		if line != "" {
			fmt.Fprintln(out, line)
		}
	default:
		fmt.Fprintln(out, "eva-md never deletes a wiki folder or its remote.")
		return
	}
	fmt.Fprintln(out, "eva-md never deletes the wiki or its remote; deleting them is your own step.")
}
