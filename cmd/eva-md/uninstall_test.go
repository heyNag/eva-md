package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/heyNag/eva-md/internal/install"
	"github.com/heyNag/eva-md/internal/paths"
)

// uninstallAs runs eva-md uninstall with args, with input on a terminal when
// terminal is set, as a person typing it would.
func uninstallAs(terminal bool, input string, args ...string) (int, string, string) {
	var out, errOut bytes.Buffer
	code := runUninstall(context.Background(), args, strings.NewReader(input), &out, &errOut, terminal)
	return code, out.String(), errOut.String()
}

// installedWithData is a Mac installed by release 2026.11.3, with a chat in
// the inbox, so the local data has something to keep.
func installedWithData(t *testing.T) *fakeMac {
	t.Helper()
	m := newFakeMac(t)
	m.installed(t, "2026.11.3")
	if err := os.MkdirAll(m.dirs.Inbox(), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(m.dirs.Inbox(), "chat.jsonl"), []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return m
}

// kept fails when any of eva-md's config or local data is gone.
func (m *fakeMac) kept(t *testing.T) {
	t.Helper()
	for _, file := range []string{m.dirs.ConfigFile(), filepath.Join(m.dirs.Inbox(), "chat.jsonl")} {
		if _, err := os.Stat(file); err != nil {
			t.Fatalf("%s removed: %v", file, err)
		}
	}
}

// The fake Mac's git starts no maintenance in the background, which git
// otherwise starts after a commit, in the wiki, and after a push, in its
// remote: its lock stays in the objects folder after the command returns,
// so a walk that shows the wiki and its remote unchanged, as the tests take
// right after their commits, could list a file that is gone when it reads
// it, or tell two walks of an unchanged wiki apart. git runs as gitRun runs
// it, but for the trace, which gitRun's environment leaves out.
func TestFakeMacStartsNoMaintenance(t *testing.T) {
	m := newFakeMac(t)
	m.setUpWiki(t)
	addTimelineLine(t, m.wiki, "done [stated]: Committed and pushed")
	trace := filepath.Join(t.TempDir(), "trace")
	for _, args := range [][]string{{"commit", "--quiet", "--all", "-m", "A commit"}, {"push", "--quiet", "origin", "HEAD"}} {
		ctx, cancel := context.WithTimeout(context.Background(), hangGuard)
		cmd := exec.CommandContext(ctx, "git", append([]string{"-c", "core.fsmonitor=false", "-c", "core.hooksPath=/dev/null", "--no-optional-locks", "-C", m.wiki}, args...)...)
		cmd.Env = append(os.Environ(), "GIT_TRACE="+trace)
		out, err := cmd.CombinedOutput()
		cancel()
		if err != nil {
			t.Fatalf("git %s: %v\n%s", args[0], err, out)
		}
	}
	b, err := os.ReadFile(trace)
	if err != nil {
		t.Fatal(err)
	}
	if s := string(b); !strings.Contains(s, "built-in: git commit") || !strings.Contains(s, "built-in: git receive-pack") || strings.Contains(s, "maintenance run") {
		t.Fatalf("git's trace of a commit and a push:\n%s", s)
	}
}

// The preview removes nothing, and lists what --apply would remove and what
// it keeps, with the config's and the data's sizes; the wiki comes last.
func TestUninstallPreviewRemovesNothing(t *testing.T) {
	m := installedWithData(t)
	before := tree(t, m.dirs.Share, m.dirs.Config, m.dirs.State, m.hosts, m.link(), m.wiki, m.remote)
	code, out, errOut := uninstallAs(false, "")
	if code != 0 {
		t.Fatalf("code %d\n%s\n%s", code, out, errOut)
	}
	says(t, "stdout", out, "Preview; nothing was changed.\nWould remove:\n  claude: the eva-md plugin and its marketplace registration\n  codex: the eva-md plugin and its marketplace registration\n",
		"  the link "+m.link()+"\nWould keep:\n  the config, "+m.dirs.Config+" (config.toml and config.env), ",
		"  the local data, "+m.dirs.State+" (inbox, search index, logs and caches) and the install record, ",
		"Add --apply to remove the program, keeping the config and the local data, or --apply --all to remove those too.\n",
		"Codex keeps its approval of eva-md's hooks in its own config, which eva-md never edits.\nTo clear it, open Codex, type /hooks",
		"eva-md never deletes the wiki or its remote; deleting them is your own step.\n")
	if !strings.HasSuffix(out, "Your wiki is kept in "+m.wiki+", pushed to file://"+m.remote+".\neva-md never deletes the wiki or its remote; deleting them is your own step.\n") {
		t.Fatalf("the last lines:\n%s", out)
	}
	if after := tree(t, m.dirs.Share, m.dirs.Config, m.dirs.State, m.hosts, m.link(), m.wiki, m.remote); after != before {
		t.Fatalf("the preview changed:\n%s\nwas:\n%s", after, before)
	}
	// --all lists the config and the data as removed.
	_, out, _ = uninstallAs(false, "", "--all")
	says(t, "stdout", out, "Would remove:", "  the config, "+m.dirs.Config, "Add --apply to remove all of this.")
}

// --apply removes the plugins through the hosts, the stable binary and the
// link, and keeps the config, the data, the record of what is left, and the
// wiki with its remote.
func TestUninstallApplyKeepsConfigDataAndWiki(t *testing.T) {
	m := installedWithData(t)
	wiki := tree(t, m.wiki, m.remote)
	code, out, errOut := uninstallAs(false, "", "--apply")
	if code != 0 {
		t.Fatalf("code %d\n%s\n%s", code, out, errOut)
	}
	says(t, "stdout", out, "Removed:\n", "Kept:\n  the config, ")
	m.wantRegistered(t, false)
	for _, gone := range []string{m.stable(), m.link(), filepath.Join(m.dirs.Share, "marketplace")} {
		if _, err := os.Lstat(gone); !errors.Is(err, fs.ErrNotExist) {
			t.Fatalf("%s kept: %v", gone, err)
		}
	}
	m.kept(t)
	r, ok := install.ReadRecord(m.dirs)
	if !ok {
		t.Fatal("the record is gone")
	}
	if _, has := r.Find(install.KindBinary, ""); has {
		t.Fatalf("the record still lists the binary: %+v", r)
	}
	if w, has := r.Find(install.KindWiki, ""); !has || !w.Kept || w.Path != m.wiki {
		t.Fatalf("the record's wiki: %+v", r)
	}
	if tree(t, m.wiki, m.remote) != wiki {
		t.Fatal("the wiki or its remote changed")
	}
}

// --all asks for delete on the terminal, --yes or not: delete removes the
// config and the local data too, any other answer removes nothing at all,
// and without a terminal it needs --yes. The wiki and its remote stay in
// every case.
func TestUninstallAllAsksForDelete(t *testing.T) {
	m := installedWithData(t)
	wiki := tree(t, m.wiki, m.remote)
	before := tree(t, m.dirs.Share, m.dirs.Config, m.dirs.State, m.hosts, m.link())
	for _, c := range []struct {
		terminal bool
		input    string
		yes      bool
		code     int
		want     string
	}{
		{true, "yes\n", false, 1, "Type delete to remove all of this: Nothing was removed.\n"},
		{true, "", false, 1, "Nothing was removed."},
		{true, "no\n", true, 1, "Type delete to remove all of this: Nothing was removed.\n"},
		{false, "delete\n", false, 2, "add --yes. Nothing was removed."},
	} {
		args := []string{"--apply", "--all"}
		if c.yes {
			args = append(args, "--yes")
		}
		code, out, errOut := uninstallAs(c.terminal, c.input, args...)
		if code != c.code || !strings.Contains(out+errOut, c.want) {
			t.Fatalf("%q %v: code %d\n%s\n%s", c.input, args, code, out, errOut)
		}
		if after := tree(t, m.dirs.Share, m.dirs.Config, m.dirs.State, m.hosts, m.link()); after != before {
			t.Fatalf("%q changed something", c.input)
		}
	}
	code, out, errOut := uninstallAs(true, "delete\n", "--apply", "--all")
	if code != 0 {
		t.Fatalf("code %d\n%s\n%s", code, out, errOut)
	}
	says(t, "stdout", out, "This removes:\n", "and keeps:\n  the wiki folder, "+m.wiki)
	m.wantRegistered(t, false)
	for _, gone := range []string{m.dirs.Config, m.dirs.State, m.dirs.Share, m.link()} {
		if _, err := os.Lstat(gone); !errors.Is(err, fs.ErrNotExist) {
			t.Fatalf("%s kept: %v", gone, err)
		}
	}
	if tree(t, m.wiki, m.remote) != wiki {
		t.Fatal("the wiki or its remote changed")
	}
	// Without a terminal, --yes is the confirmation.
	m = installedWithData(t)
	wiki = tree(t, m.wiki, m.remote)
	if code, out, errOut := uninstallAs(false, "", "--apply", "--all", "--yes"); code != 0 {
		t.Fatalf("--yes: code %d\n%s\n%s", code, out, errOut)
	}
	for _, gone := range []string{m.dirs.Config, m.dirs.State, m.dirs.Share} {
		if _, err := os.Lstat(gone); !errors.Is(err, fs.ErrNotExist) {
			t.Fatalf("%s kept: %v", gone, err)
		}
	}
	if tree(t, m.wiki, m.remote) != wiki {
		t.Fatal("the wiki or its remote changed")
	}
}

// Without the record, uninstall uses the known layout: it still removes the
// plugins, the binary and the link, and finds the wiki in the config.
func TestUninstallWithoutTheRecord(t *testing.T) {
	m := installedWithData(t)
	wiki := tree(t, m.wiki, m.remote)
	if err := os.WriteFile(filepath.Join(m.dirs.Share, install.RecordFile), []byte("not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	code, out, errOut := uninstallAs(false, "", "--apply")
	if code != 0 {
		t.Fatalf("code %d\n%s\n%s", code, out, errOut)
	}
	says(t, "stdout", out, "  the link "+m.link()+"\n", "Your wiki is kept in "+m.wiki)
	m.wantRegistered(t, false)
	for _, gone := range []string{m.stable(), m.link()} {
		if _, err := os.Lstat(gone); !errors.Is(err, fs.ErrNotExist) {
			t.Fatalf("%s kept: %v", gone, err)
		}
	}
	m.kept(t)
	if err := os.Remove(filepath.Join(m.dirs.Share, install.RecordFile)); err != nil {
		t.Fatal(err)
	}
	if code, out, _ := uninstallAs(false, "", "--apply"); code != 0 || !strings.Contains(out, "Nothing to remove: eva-md is not installed.") {
		t.Fatalf("again: code %d\n%s", code, out)
	}
	if tree(t, m.wiki, m.remote) != wiki {
		t.Fatal("the wiki or its remote changed")
	}
}

// A ~/.local/bin/eva-md that points somewhere else is never removed, and
// install leaves it alone too.
func TestUninstallKeepsALinkToSomethingElse(t *testing.T) {
	m := newFakeMac(t)
	other := filepath.Join(m.home, "other-eva-md")
	if err := os.WriteFile(other, []byte("#!/bin/sh\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(other, m.link()); err != nil {
		t.Fatal(err)
	}
	m.installed(t, "2026.11.3")
	wiki := tree(t, m.wiki, m.remote)
	if to, _ := os.Readlink(m.link()); to != other {
		t.Fatalf("install changed the link to %q", to)
	}
	if r, _ := install.ReadRecord(m.dirs); func() bool { _, has := r.Find(install.KindPathLink, ""); return has }() {
		t.Fatalf("the record lists a link: %+v", r)
	}
	code, out, errOut := uninstallAs(false, "", "--apply")
	if code != 0 {
		t.Fatalf("code %d\n%s\n%s", code, out, errOut)
	}
	says(t, "stdout", out, "  "+m.link()+", which isn't eva-md's own link\n")
	if to, _ := os.Readlink(m.link()); to != other {
		t.Fatalf("the link is now %q", to)
	}
	if tree(t, m.wiki, m.remote) != wiki {
		t.Fatal("the wiki or its remote changed")
	}
}

// Uninstall reads the wiki with local git before it removes anything. With
// changed pages no commit holds and a commit no push took, the preview's
// last lines say so, give the remote without "pushed to" and name eva-md
// commit, and --all's question says it too; with commits alone they name
// eva-md commit --push-only; and after --apply they say how to save them
// with eva-md gone. A wiki git can't read is said so and never stops
// uninstall. The wiki and its remote never change.
func TestUninstallSaysWhatTheWikiHasNotSaved(t *testing.T) {
	m := installedWithData(t)
	addTimelineLine(t, m.wiki, "done [stated]: Committed but not pushed")
	gitRun(t, m.wiki, "commit", "--quiet", "--all", "-m", "A commit not pushed")
	addTimelineLine(t, m.wiki, "done [stated]: Changed but not committed")
	if err := os.MkdirAll(filepath.Join(m.wiki, "projects", "app", "threads"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(m.wiki, "projects", "app", "threads", "main.md"), []byte("# Main\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	wiki := tree(t, m.wiki, m.remote)
	kept := "Your wiki is kept in " + m.wiki + "; its remote is file://" + m.remote + ".\n"
	both := "In the wiki, 2 changed files aren't committed and 1 commit isn't pushed, so they are only on this Mac: "
	code, out, errOut := uninstallAs(false, "")
	if code != 0 || !strings.HasSuffix(out, kept+both+"run eva-md commit before you uninstall to commit and push them.\neva-md never deletes the wiki or its remote; deleting them is your own step.\n") || strings.Contains(out, "pushed to") {
		t.Fatalf("the preview: code %d\n%s\n%s", code, out, errOut)
	}
	code, out, _ = uninstallAs(true, "no\n", "--apply", "--all")
	if code != 1 || !strings.Contains(out, "\n"+both+"run eva-md commit before you uninstall to commit and push them.\nType delete to remove all of this: Nothing was removed.\n") {
		t.Fatalf("--all's question: code %d\n%s", code, out)
	}
	gitRun(t, m.wiki, "add", "--all")
	gitRun(t, m.wiki, "commit", "--quiet", "-m", "Another commit not pushed")
	wiki = tree(t, m.wiki, m.remote)
	commits := "In the wiki, 2 commits aren't pushed, so they are only on this Mac: "
	if _, out, _ = uninstallAs(false, ""); !strings.Contains(out, kept+commits+"run eva-md commit --push-only before you uninstall to push them.\n") {
		t.Fatalf("commits alone:\n%s", out)
	}
	code, out, errOut = uninstallAs(false, "", "--apply")
	if code != 0 || !strings.Contains(out, kept+commits+"push them with git push in the wiki folder, or install eva-md again and run eva-md commit --push-only.\n") {
		t.Fatalf("--apply: code %d\n%s\n%s", code, out, errOut)
	}
	m.wantRegistered(t, false)
	if tree(t, m.wiki, m.remote) != wiki {
		t.Fatal("the wiki or its remote changed")
	}

	// A wiki git can't read, as one whose HEAD is gone, is said so, and
	// uninstall goes on.
	m = installedWithData(t)
	if err := os.Rename(filepath.Join(m.wiki, ".git", "HEAD"), filepath.Join(m.wiki, ".git", "HEAD.aside")); err != nil {
		t.Fatal(err)
	}
	wiki = tree(t, m.wiki, m.remote)
	code, out, errOut = uninstallAs(false, "", "--apply")
	if code != 0 || !strings.Contains(out, "Your wiki is kept in "+m.wiki+"; its remote is file://"+m.remote+".\nGit couldn't read the wiki folder, so whether all of it is committed and pushed is unknown; git status there shows what isn't.\n") {
		t.Fatalf("an unreadable wiki: code %d\n%s\n%s", code, out, errOut)
	}
	m.wantRegistered(t, false)
	if tree(t, m.wiki, m.remote) != wiki {
		t.Fatal("the wiki or its remote changed")
	}
}

// The lines that say how to save the wiki name only what can run. Without
// ~/.local/bin on PATH, where install makes no link, they name the stable
// binary by its full path, as setup's closing lines do. With eva-md gone,
// after --apply and in a later uninstall, as the uninstall script runs with
// the latest release's binary, they say to use git in the wiki folder or to
// install eva-md again, never to commit before uninstalling; for --all,
// which removes the config, without which setup takes a wiki with changed
// files only once they are committed, git alone, in its preview and its
// question too. Commits alone, which setup pushes when it takes the wiki
// again, keep both ways. The wiki and its remote never change.
func TestUninstallNamesOnlyWhatCanSaveTheWiki(t *testing.T) {
	m := newFakeMac(t)
	var path []string
	for _, dir := range filepath.SplitList(os.Getenv("PATH")) {
		if dir != filepath.Dir(m.link()) && dir != filepath.Join(m.home, ".local", "bin") {
			path = append(path, dir)
		}
	}
	t.Setenv("PATH", strings.Join(path, string(filepath.ListSeparator)))
	m.installed(t, "2026.11.3")
	if _, err := os.Lstat(m.link()); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("install linked %s off PATH: %v", m.link(), err)
	}
	addTimelineLine(t, m.wiki, "done [stated]: Changed but not committed")
	wiki := tree(t, m.wiki, m.remote)
	changed := "In the wiki, 1 changed file isn't committed, so it is only on this Mac: "
	byGit := changed + "commit and push it with git in the wiki folder"
	again := byGit + ", or install eva-md again and run " + m.stable() + " commit.\n"
	if code, out, errOut := uninstallAs(false, ""); code != 0 || !strings.Contains(out, changed+"run "+m.stable()+" commit before you uninstall to commit and push it.\n") {
		t.Fatalf("the preview: code %d\n%s\n%s", code, out, errOut)
	}
	if code, out, errOut := uninstallAs(false, "", "--apply"); code != 0 || !strings.Contains(out, again) {
		t.Fatalf("--apply: code %d\n%s\n%s", code, out, errOut)
	}
	if code, out, errOut := uninstallAs(false, ""); code != 0 || !strings.Contains(out, "Nothing to remove: eva-md is not installed.") || !strings.Contains(out, again) {
		t.Fatalf("the preview without eva-md: code %d\n%s\n%s", code, out, errOut)
	}
	if code, out, errOut := uninstallAs(false, "", "--all"); code != 0 || !strings.Contains(out, "\n"+byGit+".\n") {
		t.Fatalf("--all's preview without eva-md: code %d\n%s\n%s", code, out, errOut)
	}
	if code, out, _ := uninstallAs(true, "no\n", "--apply", "--all"); code != 1 || !strings.Contains(out, "\n"+byGit+".\nType delete to remove all of this: Nothing was removed.\n") {
		t.Fatalf("--all's question without eva-md: code %d\n%s", code, out)
	}
	if code, out, errOut := uninstallAs(false, "", "--apply", "--all", "--yes"); code != 0 || !strings.Contains(out, "\n"+byGit+".\n") {
		t.Fatalf("--all: code %d\n%s\n%s", code, out, errOut)
	}
	if tree(t, m.wiki, m.remote) != wiki {
		t.Fatal("the wiki or its remote changed")
	}

	// A commit alone, on a Mac with the link on PATH: eva-md commit
	// --push-only, before and after --all.
	m = installedWithData(t)
	addTimelineLine(t, m.wiki, "done [stated]: Committed but not pushed")
	gitRun(t, m.wiki, "commit", "--quiet", "--all", "-m", "A commit not pushed")
	wiki = tree(t, m.wiki, m.remote)
	commit := "In the wiki, 1 commit isn't pushed, so it is only on this Mac: "
	if code, out, _ := uninstallAs(true, "no\n", "--apply", "--all"); code != 1 || !strings.Contains(out, "\n"+commit+"run eva-md commit --push-only before you uninstall to push it.\nType delete") {
		t.Fatalf("--all's question: code %d\n%s", code, out)
	}
	if code, out, errOut := uninstallAs(false, "", "--apply", "--all", "--yes"); code != 0 || !strings.Contains(out, "\n"+commit+"push it with git push in the wiki folder, or install eva-md again and run eva-md commit --push-only.\n") {
		t.Fatalf("--all: code %d\n%s\n%s", code, out, errOut)
	}
	if tree(t, m.wiki, m.remote) != wiki {
		t.Fatal("the wiki or its remote changed")
	}
}

// When the config doesn't name the wiki, as after it was moved aside, which
// setup says to do to start over, or when it can't be read, uninstall takes
// the wiki from the record, and its lines give git alone, in the preview,
// in --all's question and after --apply, and in a preview once eva-md is
// gone: eva-md commit needs the config that names the wiki, and a new
// install's setup refuses a config it can't read, and takes a wiki whose
// changes no commit holds only with the config that names it. The wiki and
// its remote never change.
func TestUninstallNamesGitAloneWithoutTheConfig(t *testing.T) {
	gitAlone := func(what, out string) {
		t.Helper()
		if strings.Contains(out, "eva-md commit") || strings.Contains(out, "install eva-md again") {
			t.Fatalf("%s names more than git:\n%s", what, out)
		}
	}
	m := installedWithData(t)
	addTimelineLine(t, m.wiki, "done [stated]: Changed but not committed")
	if err := os.Rename(m.dirs.ConfigFile(), m.dirs.ConfigFile()+".aside"); err != nil {
		t.Fatal(err)
	}
	wiki := tree(t, m.wiki, m.remote)
	kept := "Your wiki is kept in " + m.wiki + "; its remote is file://" + m.remote + ".\n"
	byGit := "In the wiki, 1 changed file isn't committed, so it is only on this Mac: commit and push it with git in the wiki folder.\n"
	for _, args := range [][]string{nil, {"--all"}} {
		code, out, errOut := uninstallAs(false, "", args...)
		if code != 0 || !strings.Contains(out, kept+byGit) {
			t.Fatalf("the preview %v: code %d\n%s\n%s", args, code, out, errOut)
		}
		gitAlone(fmt.Sprintf("the preview %v", args), out)
	}
	code, out, errOut := uninstallAs(true, "no\n", "--apply", "--all")
	if code != 1 || !strings.Contains(out, "\n"+byGit+"Type delete to remove all of this: Nothing was removed.\n") {
		t.Fatalf("--all's question: code %d\n%s\n%s", code, out, errOut)
	}
	gitAlone("--all's question", out)
	code, out, errOut = uninstallAs(false, "", "--apply")
	if code != 0 || !strings.Contains(out, "Removed:\n") || !strings.Contains(out, kept+byGit) {
		t.Fatalf("--apply: code %d\n%s\n%s", code, out, errOut)
	}
	gitAlone("--apply", out)
	m.wantRegistered(t, false)
	code, out, errOut = uninstallAs(false, "")
	if code != 0 || !strings.Contains(out, "Nothing to remove: eva-md is not installed.") || !strings.Contains(out, kept+byGit) {
		t.Fatalf("the preview without eva-md: code %d\n%s\n%s", code, out, errOut)
	}
	gitAlone("the preview without eva-md", out)
	if tree(t, m.wiki, m.remote) != wiki {
		t.Fatal("the wiki or its remote changed")
	}

	// A config that can't be read, with a commit alone: git push, before
	// and after --apply, which keeps that config.
	m = installedWithData(t)
	addTimelineLine(t, m.wiki, "done [stated]: Committed but not pushed")
	gitRun(t, m.wiki, "commit", "--quiet", "--all", "-m", "A commit not pushed")
	if err := os.WriteFile(m.dirs.ConfigFile(), []byte("wiki_path = [broken\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	wiki = tree(t, m.wiki, m.remote)
	kept = "Your wiki is kept in " + m.wiki + "; its remote is file://" + m.remote + ".\n"
	push := "In the wiki, 1 commit isn't pushed, so it is only on this Mac: push it with git push in the wiki folder.\n"
	code, out, errOut = uninstallAs(false, "")
	if code != 0 || !strings.Contains(out, kept+push) {
		t.Fatalf("the preview with a config that can't be read: code %d\n%s\n%s", code, out, errOut)
	}
	gitAlone("the preview with a config that can't be read", out)
	code, out, errOut = uninstallAs(false, "", "--apply")
	if code != 0 || !strings.Contains(out, "Removed:\n") || !strings.Contains(out, kept+push) {
		t.Fatalf("--apply with a config that can't be read: code %d\n%s\n%s", code, out, errOut)
	}
	gitAlone("--apply with a config that can't be read", out)
	m.wantRegistered(t, false)
	if tree(t, m.wiki, m.remote) != wiki {
		t.Fatal("the wiki or its remote changed")
	}
}

// atThePrompt is a terminal on which do runs while --all's question waits,
// as the person or a job of eva-md's would act meanwhile, before delete is
// typed.
type atThePrompt struct {
	do   func()
	done bool
}

func (p *atThePrompt) Read(b []byte) (int, error) {
	if !p.done {
		p.done = true
		p.do()
	}
	return copy(b, "delete\n"), nil
}

// Uninstall reads the wiki again after the removal for its last lines, on
// every path, so they say what the wiki holds then: after eva-md commit,
// run as --all's question said while it waited, that the wiki is pushed;
// after a page was written while it waited, or while the hosts removed the
// plugins, as on the paths with no question, that it isn't committed, with
// how to save it once eva-md is gone.
func TestUninstallReadsTheWikiAgainForItsLastLines(t *testing.T) {
	const question = "Type delete to remove all of this: "
	m := installedWithData(t)
	addTimelineLine(t, m.wiki, "done [stated]: Changed but not committed")
	var out bytes.Buffer
	commit := &atThePrompt{do: func() {
		if code, stdout, errOut := evaMD(t, "commit"); code != 0 {
			t.Fatalf("eva-md commit at the prompt: code %d\n%s\n%s", code, stdout, errOut)
		}
	}}
	if code := runUninstall(context.Background(), []string{"--apply", "--all"}, commit, &out, &out, true); code != 0 || !commit.done {
		t.Fatalf("code %d, asked %t\n%s", code, commit.done, out.String())
	}
	asked, closing, _ := strings.Cut(out.String(), question)
	if !strings.Contains(asked, "\nIn the wiki, 1 changed file isn't committed, so it is only on this Mac: run eva-md commit before you uninstall to commit and push it.\n") {
		t.Fatalf("the question:\n%s", out.String())
	}
	if !strings.Contains(closing, "Your wiki is kept in "+m.wiki+", pushed to file://"+m.remote+".\n") || strings.Contains(closing, "In the wiki") {
		t.Fatalf("the last lines after eva-md commit at the prompt:\n%s", out.String())
	}

	notCommitted := func(m *fakeMac) string {
		return "Your wiki is kept in " + m.wiki + "; its remote is file://" + m.remote + ".\nIn the wiki, 1 changed file isn't committed, so it is only on this Mac: commit and push it with git in the wiki folder"
	}
	m = installedWithData(t)
	out.Reset()
	write := &atThePrompt{do: func() { addTimelineLine(t, m.wiki, "done [stated]: Written while the question waited") }}
	if code := runUninstall(context.Background(), []string{"--apply", "--all"}, write, &out, &out, true); code != 0 || !write.done {
		t.Fatalf("code %d, asked %t\n%s", code, write.done, out.String())
	}
	asked, closing, _ = strings.Cut(out.String(), question)
	if strings.Contains(asked, "In the wiki") || !strings.Contains(closing, notCommitted(m)+".\n") {
		t.Fatalf("the last lines after a page written at the prompt:\n%s", out.String())
	}

	// With no question, the fake Claude Code writes a page while it removes
	// eva-md's plugin.
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"--apply"}, ", or install eva-md again and run eva-md commit.\n"},
		{[]string{"--apply", "--all", "--yes"}, ".\n"},
	} {
		m = installedWithData(t)
		claude := filepath.Join(m.bin, "claude")
		host := filepath.Join(t.TempDir(), "claude")
		if err := os.Rename(claude, host); err != nil {
			t.Fatal(err)
		}
		page := filepath.Join(m.wiki, "general", "threads", "default.md")
		q := func(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }
		script := "#!/bin/sh\ncase \"$*\" in\n\"plugin uninstall \"*) printf '### 2026-10-31\\n- done [stated]: Written while uninstall removed\\n' >> " + q(page) + " ;;\nesac\nexec " + q(host) + " \"$@\"\n"
		if err := os.WriteFile(claude, []byte(script), 0o700); err != nil {
			t.Fatal(err)
		}
		code, out, errOut := uninstallAs(false, "", tc.args...)
		if code != 0 || !strings.Contains(out, "Removed:\n") || !strings.Contains(out, notCommitted(m)+tc.want) {
			t.Fatalf("%v: code %d\n%s\n%s", tc.args, code, out, errOut)
		}
		m.wantRegistered(t, false)
	}
}

// What the wiki hasn't saved is said in counts, one or many, before and
// after the removal, naming eva-md as it is given; with --all, changed
// files are saved with git alone once eva-md is gone. A wiki the config
// doesn't name is saved with git alone, before and after, with --all or
// not, for commits alone too. Nothing is said when everything is saved.
func TestUnsavedLine(t *testing.T) {
	const full = "/Users/you/.local/share/eva-md/bin/eva-md"
	for _, tc := range []struct {
		u                   unsaved
		command             string
		removed, all, named bool
		want                string
	}{
		{unsaved{}, "eva-md", false, false, true, ""},
		{unsaved{}, "eva-md", true, true, true, ""},
		{unsaved{}, "eva-md", false, false, false, ""},
		{unsaved{files: 1}, "eva-md", false, false, true, "In the wiki, 1 changed file isn't committed, so it is only on this Mac: run eva-md commit before you uninstall to commit and push it."},
		{unsaved{files: 1}, full, false, true, true, "In the wiki, 1 changed file isn't committed, so it is only on this Mac: run " + full + " commit before you uninstall to commit and push it."},
		{unsaved{files: 1}, "eva-md", true, false, true, "In the wiki, 1 changed file isn't committed, so it is only on this Mac: commit and push it with git in the wiki folder, or install eva-md again and run eva-md commit."},
		{unsaved{files: 1}, "eva-md", true, true, true, "In the wiki, 1 changed file isn't committed, so it is only on this Mac: commit and push it with git in the wiki folder."},
		{unsaved{commits: 1}, full, false, false, true, "In the wiki, 1 commit isn't pushed, so it is only on this Mac: run " + full + " commit --push-only before you uninstall to push it."},
		{unsaved{commits: 2}, full, true, true, true, "In the wiki, 2 commits aren't pushed, so they are only on this Mac: push them with git push in the wiki folder, or install eva-md again and run " + full + " commit --push-only."},
		{unsaved{files: 3, commits: 2}, "eva-md", true, false, true, "In the wiki, 3 changed files aren't committed and 2 commits aren't pushed, so they are only on this Mac: commit and push them with git in the wiki folder, or install eva-md again and run eva-md commit."},
		{unsaved{files: 3, commits: 2}, "eva-md", true, true, true, "In the wiki, 3 changed files aren't committed and 2 commits aren't pushed, so they are only on this Mac: commit and push them with git in the wiki folder."},
		{unsaved{unread: true}, "eva-md", false, false, true, "Git couldn't read the wiki folder, so whether all of it is committed and pushed is unknown; git status there shows what isn't."},
		// The config doesn't name the wiki.
		{unsaved{files: 1}, "eva-md", false, false, false, "In the wiki, 1 changed file isn't committed, so it is only on this Mac: commit and push it with git in the wiki folder."},
		{unsaved{files: 1}, full, false, true, false, "In the wiki, 1 changed file isn't committed, so it is only on this Mac: commit and push it with git in the wiki folder."},
		{unsaved{commits: 1}, "eva-md", false, false, false, "In the wiki, 1 commit isn't pushed, so it is only on this Mac: push it with git push in the wiki folder."},
		{unsaved{commits: 2}, full, true, false, false, "In the wiki, 2 commits aren't pushed, so they are only on this Mac: push them with git push in the wiki folder."},
		{unsaved{commits: 2}, "eva-md", true, true, false, "In the wiki, 2 commits aren't pushed, so they are only on this Mac: push them with git push in the wiki folder."},
		{unsaved{files: 3, commits: 2}, "eva-md", true, false, false, "In the wiki, 3 changed files aren't committed and 2 commits aren't pushed, so they are only on this Mac: commit and push them with git in the wiki folder."},
		{unsaved{unread: true}, "eva-md", true, false, false, "Git couldn't read the wiki folder, so whether all of it is committed and pushed is unknown; git status there shows what isn't."},
	} {
		if got := tc.u.line(tc.command, tc.removed, tc.all, tc.named); got != tc.want {
			t.Errorf("%+v, %s, removed %t, all %t, named %t:\n got %q\nwant %q", tc.u, tc.command, tc.removed, tc.all, tc.named, got, tc.want)
		}
	}
}

// While a background update or the daily job holds its lock, uninstall
// removes nothing and says to try again shortly.
func TestUninstallWaitsForTheUpdateAndDailyJob(t *testing.T) {
	m := installedWithData(t)
	before := tree(t, m.dirs.Share, m.dirs.Config, m.hosts, m.link(), m.wiki, m.remote)
	for _, name := range []string{"update.lock", "maintain.lock"} {
		unlock, busy, err := paths.TryLock(m.dirs.Locks(), name)
		if err != nil || busy {
			t.Fatal(err, busy)
		}
		code, _, errOut := uninstallAs(true, "delete\n", "--apply", "--all")
		unlock()
		if code != 1 || !strings.Contains(errOut, "a background update or the daily job is running, so nothing was removed; try again shortly") {
			t.Fatalf("%s: code %d %q", name, code, errOut)
		}
		if tree(t, m.dirs.Share, m.dirs.Config, m.hosts, m.link(), m.wiki, m.remote) != before {
			t.Fatalf("%s: something was removed", name)
		}
	}
}
