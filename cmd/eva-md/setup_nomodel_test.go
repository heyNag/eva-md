package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/heyNag/eva-md/internal/health"
	"github.com/heyNag/eva-md/internal/host/hostfake"
	"github.com/heyNag/eva-md/internal/inbox"
	"github.com/heyNag/eva-md/internal/install"
	"github.com/heyNag/eva-md/internal/paths"
)

// Setup makes no model call (SET-K19): a whole guided setup, with the real
// install, health check and git against the fake hosts and a fake GitHub,
// starts the host CLIs only for what is no headless run of a prompt, and
// leaves the chats captured before it set aside, so no update falls due
// for them.

// The fake host CLIs run this test binary again: as one, it answers and
// exits here, before any test runs.
func init() { hostfake.Main() }

// headless says why a host CLI's start is a headless run of a prompt, the
// form the update and doctor --live call a model in, or "" for any other:
// claude with -p or --print, and codex exec or its alias e.
func headless(c hostfake.Call) string {
	switch c.CLI {
	case hostfake.Claude:
		if i := slices.IndexFunc(c.Args, func(a string) bool { return a == "-p" || a == "--print" }); i >= 0 {
			return "claude " + c.Args[i]
		}
	case hostfake.Codex:
		for _, a := range c.Args {
			if a == "--" {
				break
			}
			if a == "exec" || a == "e" {
				return "codex " + a
			}
		}
	}
	return ""
}

// The forms a model is called in are told from what setup and install
// run.
func TestHeadlessRunsAreTold(t *testing.T) {
	for _, c := range []struct {
		call hostfake.Call
		want string
	}{
		{hostfake.Call{CLI: "claude", Args: []string{"-p", "--safe-mode", "--tools", ""}}, "claude -p"},
		{hostfake.Call{CLI: "claude", Args: []string{"--model", "x", "--print"}}, "claude --print"},
		{hostfake.Call{CLI: "codex", Args: []string{"--no-daemon", "exec", "--ephemeral", "--json"}}, "codex exec"},
		{hostfake.Call{CLI: "codex", Args: []string{"e", "hello"}}, "codex e"},
		{hostfake.Call{CLI: "claude", Args: []string{"plugin", "list", "--json"}}, ""},
		{hostfake.Call{CLI: "claude", Args: []string{"--version"}}, ""},
		{hostfake.Call{CLI: "codex", Args: []string{"features", "list"}}, ""},
		{hostfake.Call{CLI: "codex", Args: []string{"--no-daemon", "app-server", "--stdio"}}, ""},
		{hostfake.Call{CLI: "codex", Args: []string{"plugin", "add", "--", "exec"}}, ""},
	} {
		if got := headless(c.call); got != c.want {
			t.Errorf("%s read as %q, want %q", c.call, got, c.want)
		}
	}
}

// noModelSetup is a fake setup with the real install, its checks and the
// health check, against fake host CLIs that record every command they are
// given, git against a bare repo behind a fake GitHub address, and a chat
// captured before setup. It returns the fake hosts and the chat's file.
func noModelSetup(t *testing.T) (*fakeSetup, *hostfake.Fake, string) {
	t.Helper()
	f := newFakeSetup(t, true).ghOnGitHub(t)
	f.realGit(t)
	f.bareRepo(t, "eva-wiki")
	hosts := hostfake.New(filepath.Join(t.TempDir(), "hosts"))
	if err := hosts.WriteCLIs(f.bin); err != nil {
		t.Fatal(err)
	}
	f.env.lookPath = exec.LookPath
	binary := filepath.Join(t.TempDir(), "eva-md")
	if err := os.WriteFile(binary, []byte("#!/bin/sh\nexit 0\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	f.env.executable = func() (string, error) { return binary, nil }
	f.env.install = install.Run
	f.env.preflight = func(ctx context.Context, o install.Options) (install.Result, error) {
		o.Preflight = true
		return install.Run(ctx, o)
	}
	f.env.inspect = func(ctx context.Context, d paths.Dirs) health.Report { return health.Inspect(ctx, d, true) }
	store := inbox.Store{Root: f.env.dirs.Inbox()}
	for i, text := range []string{"Plan the ledger export.", "Export it as CSV, one file a month."} {
		role, kind := "user", "prompt"
		if i == 1 {
			role, kind = "assistant", "reply"
		}
		e := inbox.Entry{V: 1, TS: time.Now().Add(-time.Hour).Format(time.RFC3339), Host: "codex", Session: "before-setup", Turn: "a",
			Role: role, Kind: kind, Text: text, Project: "general", Branch: "default", Cwd: "project", Model: "fixture-model", Origin: "hook"}
		if err := store.Append(e); err != nil {
			t.Fatal(err)
		}
	}
	file := store.Path("general", "codex", "before-setup")
	if pending(t, store, file) == 0 {
		t.Fatal("the chat captured before setup has nothing pending before setup runs")
	}
	return f, hosts, file
}

// pending is how many bytes of a captured chat the update has yet to
// process.
func pending(t *testing.T, store inbox.Store, file string) int64 {
	t.Helper()
	info, err := os.Stat(file)
	if err != nil {
		t.Fatal(err)
	}
	m, err := store.Mark(file)
	if err != nil {
		t.Fatal(err)
	}
	return m.Pending(info.Size())
}

// headlessRuns are the starts of a host CLI that ran a prompt, of all
// those the fake hosts were given.
func headlessRuns(t *testing.T, hosts *hostfake.Fake) (calls, runs []string) {
	t.Helper()
	all, err := hosts.Calls()
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range all {
		calls = append(calls, c.String())
		if why := headless(c); why != "" {
			runs = append(runs, c.String())
		}
	}
	return calls, runs
}

// A whole guided setup, which installs the plugins for both hosts and runs
// the health check, starts the host CLIs, but never to run a prompt; and
// the chat captured before it is set aside, so no update falls due for it.
func TestSetupMakesNoModelCall(t *testing.T) {
	f, hosts, chat := noModelSetup(t)
	code, out, errOut := f.run("~/projects/wiki\ngit@github.com:me/eva-wiki.git\ny\n")
	if code != 0 || !strings.Contains(out, "Setup will create a new wiki in this folder and push it, then install the plugins.") {
		t.Fatalf("setup: %d\n%s\n%s", code, out, errOut)
	}
	calls, runs := headlessRuns(t, hosts)
	if len(runs) > 0 {
		t.Errorf("setup ran a prompt on a host: %q", runs)
	}
	// Setup did start each host: install registered the plugins, and the
	// health check asked about them.
	for _, want := range []string{"claude plugin install", "codex plugin add", "codex features list"} {
		if !slices.ContainsFunc(calls, func(c string) bool { return strings.HasPrefix(c, want) }) {
			t.Errorf("no host CLI was started as %q, so setup did not run whole: %q", want, calls)
		}
	}
	if n := pending(t, inbox.Store{Root: f.env.dirs.Inbox()}, chat); n != 0 {
		t.Errorf("setup left %d bytes of a chat captured before it for an update", n)
	}
	if _, err := os.Stat(filepath.Join(f.env.dirs.Inbox(), ".baseline")); err != nil {
		t.Errorf("setup recorded no baseline: %v", err)
	}
}

// The check fails a setup that runs a prompt on a host: here the health
// check it runs at the end starts claude -p through the fake host, as a
// health check that made a model call would.
func TestASetupThatRunsAPromptIsFound(t *testing.T) {
	f, hosts, _ := noModelSetup(t)
	inspect := f.env.inspect
	f.env.inspect = func(ctx context.Context, d paths.Dirs) health.Report {
		_ = exec.CommandContext(ctx, "claude", "-p", "Say hello").Run()
		return inspect(ctx, d)
	}
	if code, out, errOut := f.run("~/projects/wiki\ngit@github.com:me/eva-wiki.git\ny\n"); code != 0 {
		t.Fatalf("setup: %d\n%s\n%s", code, out, errOut)
	}
	if _, runs := headlessRuns(t, hosts); !slices.Equal(runs, []string{"claude -p Say hello"}) {
		t.Fatalf("the check found the runs %q, want the seeded one", runs)
	}
}
