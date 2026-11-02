package main

import (
	"bytes"
	"context"
	"errors"
	"io/fs"
	"os"
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
