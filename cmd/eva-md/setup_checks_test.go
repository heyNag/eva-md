package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/heyNag/eva-md/internal/install"
)

// Setup's checks run in order before any question, and the first that fails
// stops setup (exit 1) with what is wrong, its exact fix and "Then run setup
// again.", after a line for each check that passed; nothing is asked or
// changed after it.
func TestSetupChecklistStopsAtTheFirstProblem(t *testing.T) {
	without := func(missing ...string) func(string) (string, error) {
		return func(name string) (string, error) {
			for _, m := range missing {
				if name == m {
					return "", errors.New("not found")
				}
			}
			return "/usr/bin/" + name, nil
		}
	}
	for _, tc := range []struct {
		name      string
		goos      string
		missing   []string
		args      []string
		file      string
		passed    []string // the lines before the failure
		fail, fix string
	}{
		{name: "macOS", goos: "linux",
			fail: "This isn't macOS: eva-md runs on macOS only", fix: "run eva-md setup on a Mac"},
		{name: "git", missing: []string{"git"}, passed: []string{"macOS"},
			fail: "git isn't installed", fix: "xcode-select --install"},
		{name: "no host app", missing: []string{"claude", "codex"}, passed: []string{"macOS", "git"},
			fail: "Neither Claude Code nor Codex is installed (no claude or codex command)", fix: "install Claude Code or Codex"},
		{name: "the --host app", missing: []string{"codex"}, args: []string{"--host", "codex"}, passed: []string{"macOS", "git"},
			fail: "Codex isn't installed (no codex command)", fix: "install Codex, or leave out --host"},
		{name: "the EVA_MD_HOSTS app", missing: []string{"claude"}, file: "EVA_MD_HOSTS=claude\n", passed: []string{"macOS", "git"},
			fail: "Claude Code isn't installed (no claude command)", fix: "install Claude Code, or change EVA_MD_HOSTS in <file>"},
		{name: "config.env", file: "EVA_MD_GIT_HOST_URL=git@gitlab.example.com:me/eva-wiki.git\n", passed: []string{"macOS", "git", "Claude Code and Codex"},
			fail: "EVA_MD_GIT_HOST_URL in <file> is a repo's address; give the git host's web address, such as https://gitlab.example.com, and a repo's address in EVA_MD_WIKI_REMOTE or --remote", fix: "fix <file>, or move it aside"},
		{name: "the git host's CLI", file: "EVA_MD_GIT_HOST_URL=https://gitlab.example.com/your-username\n", missing: []string{"codex"}, passed: []string{"macOS", "git", "Claude Code; Codex not found", "Read <file>"},
			fail: "glab isn't installed", fix: "install glab (brew install glab), then run glab auth login --hostname gitlab.example.com"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFakeSetup(t, true)
			file := f.env.dirs.AnswersFile()
			if tc.goos != "" {
				f.env.goos = tc.goos
			}
			f.env.lookPath = without(tc.missing...)
			if tc.file != "" {
				f.answersFile(t, tc.file)
			}
			code, out, errOut := f.run("~/projects/wiki\ngit@github.com:me/eva-wiki.git\ny\n", tc.args...)
			if code != 1 || errOut != "" || len(f.inits) != 0 || len(f.installs) != 0 {
				t.Fatalf("code %d inits %d installs %d\n%s\n%s", code, len(f.inits), len(f.installs), out, errOut)
			}
			want := "Checking this Mac\n"
			for _, p := range tc.passed {
				want += "  ✓ " + p + "\n"
			}
			want += "  ✗ " + tc.fail + "\n    Fix: " + tc.fix + "\n    Then run setup again.\n"
			if want = strings.ReplaceAll(want, "<file>", file); !strings.HasSuffix(out, want) {
				t.Fatalf("stdout doesn't end with\n%s\ngot\n%s", want, out)
			}
			wantNotIn(t, "stdout", out, "Folder:", "Summary", "[y/N]", "\x1b[")
		})
	}
}

// fakeHostCLI writes a fake host command into f's PATH that answers each
// argument list in answers with its output, exits 1 for an answer of
// "!fail", and exits 2 for any other command. Shell built-ins only: PATH
// holds nothing else.
func (f *fakeSetup) fakeHostCLI(t *testing.T, name string, answers map[string]string) {
	t.Helper()
	var b strings.Builder
	b.WriteString("#!/bin/sh\ncase \"$*\" in\n")
	for args, answer := range answers {
		if answer == "!fail" {
			fmt.Fprintf(&b, "%q) exit 1 ;;\n", args)
		} else {
			fmt.Fprintf(&b, "%q) printf '%%s\\n' '%s' ;;\n", args, answer)
		}
	}
	b.WriteString("*) exit 2 ;;\nesac\n")
	if err := os.WriteFile(filepath.Join(f.bin, name), []byte(b.String()), 0o700); err != nil {
		t.Fatal(err)
	}
}

// Last among setup's checks of this Mac come install's own, as a dry run
// that needs no config, here the real ones against fake claude and codex
// commands: whatever install would refuse stops setup there, exit 1, before
// it asks anything or makes a wiki, with the fix. Codex with its hooks
// feature off names both ways out: turning it on, or installing for Claude
// Code only, in the answers file, or with --host when the flag chose the
// hosts, and on a Mac with no claude command, after installing Claude Code;
// --host codex, which leaves Claude Code out of the hosts, doesn't hide the
// claude command that is there. A failed host command says to run it; a
// refusal is said as install words it, with its fix. Passing, the line
// names the apps the plugins are for, and setup goes on.
func TestSetupStopsWhereInstallWouldRefuse(t *testing.T) {
	type answers = map[string]string
	for _, tc := range []struct {
		name          string
		args          []string
		claude, codex answers
		noClaude      bool // no claude command, for setup's lookup and install's
		unmanaged     bool // a binary eva-md didn't install sits where install puts its own
		passed        string
		fail, fix     string
	}{
		{name: "hooks off",
			fail: "Codex's hooks feature is off, so eva-md can't capture Codex chats",
			fix:  "run codex features enable hooks, or set EVA_MD_HOSTS=claude in <file> to install for Claude Code only"},
		{name: "hooks off with --host", args: []string{"--host", "all"},
			fail: "Codex's hooks feature is off, so eva-md can't capture Codex chats",
			fix:  "run codex features enable hooks, or run setup with --host claude to install for Claude Code only"},
		{name: "hooks off with --host codex", args: []string{"--host", "codex"},
			fail: "Codex's hooks feature is off, so eva-md can't capture Codex chats",
			fix:  "run codex features enable hooks, or run setup with --host claude to install for Claude Code only"},
		{name: "hooks off without Claude Code", noClaude: true,
			fail: "Codex's hooks feature is off, so eva-md can't capture Codex chats",
			fix:  "run codex features enable hooks, or install Claude Code, then set EVA_MD_HOSTS=claude in <file> to install for Claude Code only"},
		{name: "hooks off with --host codex without Claude Code", args: []string{"--host", "codex"}, noClaude: true,
			fail: "Codex's hooks feature is off, so eva-md can't capture Codex chats",
			fix:  "run codex features enable hooks, or install Claude Code, then run setup with --host claude to install for Claude Code only"},
		{name: "for Claude Code only", args: []string{"--host", "claude"}, passed: "The plugins can be installed for Claude Code"},
		{name: "hooks on", codex: answers{"features list": "hooks experimental true"}, passed: "The plugins can be installed for Claude Code and Codex"},
		{name: "a host command that fails", claude: answers{"plugin marketplace list --json": "!fail"},
			fail: "The plugins can't be installed: claude plugin marketplace list --json failed", fix: "run claude plugin marketplace list --json to see why"},
		{name: "a registration that needs attention",
			claude: answers{"plugin marketplace list --json": `[{"name":"eva-md","root":"<share>/marketplace/claude"}]`,
				"plugin list --json": `[{"pluginId":"eva-md@eva-md","name":"eva-md","enabled":false,"readFromFolder":"<share>/marketplace/claude/plugins/eva-md","folderVersion":"1","errors":[]}]`},
			fail: "The plugins can't be installed: existing claude eva-md registration needs attention: eva-md plugin is disabled",
			fix:  "run eva-md uninstall --apply, or the uninstall script with --apply, then install eva-md again"},
		{name: "another eva-md copy",
			claude: answers{"plugin list --json": `[{"pluginId":"eva-md@eva-md","name":"eva-md","enabled":true},{"pluginId":"eva-md@eva-md","name":"eva-md","enabled":true}]`},
			fail:   "The plugins can't be installed: multiple eva-md plugin copies are registered",
			fix:    "remove the other eva-md registration with that app's plugin commands"},
		{name: "an unmanaged binary", args: []string{"--host", "claude"}, unmanaged: true,
			fail: "The plugins can't be installed: refusing to replace an unmanaged binary, <share>/bin/eva-md",
			fix:  "move it aside"},
		{name: "a list install can't read", claude: answers{"plugin list --json": "not a list"},
			fail: "The plugins can't be installed: invalid host inventory JSON",
			fix:  "fix that first; setup makes no wiki until install's checks pass"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFakeSetup(t, true)
			file := f.env.dirs.AnswersFile()
			binary := filepath.Join(t.TempDir(), "eva-md")
			if err := os.WriteFile(binary, []byte("eva-md binary"), 0o700); err != nil {
				t.Fatal(err)
			}
			f.env.executable = func() (string, error) { return binary, nil }
			var checks []install.Options
			f.env.preflight = func(ctx context.Context, o install.Options) (install.Result, error) {
				checks = append(checks, o)
				o.Preflight = true
				return install.Run(ctx, o)
			}
			claude := answers{"plugin marketplace list --json": "[]", "plugin list --json": "[]"}
			codex := answers{"plugin marketplace list --json": `{"marketplaces":[]}`, "features list": "hooks experimental false"}
			for host, given := range map[string]answers{"claude": tc.claude, "codex": tc.codex} {
				for args, answer := range given {
					map[string]answers{"claude": claude, "codex": codex}[host][args] = strings.ReplaceAll(answer, "<share>", f.env.dirs.Share)
				}
			}
			f.fakeHostCLI(t, "codex", codex)
			if tc.noClaude {
				f.env.lookPath = func(name string) (string, error) {
					if name == "claude" {
						return "", errors.New("not found")
					}
					return "/usr/bin/" + name, nil
				}
			} else {
				f.fakeHostCLI(t, "claude", claude)
			}
			if tc.unmanaged {
				if err := os.MkdirAll(filepath.Join(f.env.dirs.Share, "bin"), 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(f.env.dirs.Share, "bin", "eva-md"), []byte("not eva-md's"), 0o700); err != nil {
					t.Fatal(err)
				}
			}
			code, out, errOut := f.run("~/projects/wiki\ngit@github.com:me/eva-wiki.git\ny\n", tc.args...)
			host := "all"
			if len(tc.args) > 0 {
				host = tc.args[len(tc.args)-1]
			}
			if len(checks) != 1 || checks[0].Binary != binary || checks[0].Host != host {
				t.Fatalf("install's checks ran as %+v", checks)
			}
			if tc.passed != "" {
				if code != 0 || len(f.inits) != 1 || len(f.installs) != 1 {
					t.Fatalf("code %d inits %d installs %d\n%s\n%s", code, len(f.inits), len(f.installs), out, errOut)
				}
				checked := "  ✓ Commit signing on (ssh)\n  ✓ " + tc.passed + "\n"
				wantIn(t, "stdout", out, checked)
				if strings.Index(out, checked) > strings.Index(out, "1. Where should the wiki live on this Mac?") {
					t.Fatalf("install's checks came after the first question:\n%s", out)
				}
				return
			}
			if code != 1 || errOut != "" || len(f.inits) != 0 || len(f.installs) != 0 {
				t.Fatalf("code %d inits %d installs %d\n%s\n%s", code, len(f.inits), len(f.installs), out, errOut)
			}
			want := "  ✓ Commit signing on (ssh)\n  ✗ " + strings.ReplaceAll(tc.fail, "<share>", f.env.dirs.Share) + "\n    Fix: " + strings.ReplaceAll(tc.fix, "<file>", file) + "\n    Then run setup again.\n"
			if !strings.HasSuffix(out, want) {
				t.Fatalf("stdout doesn't end with\n%s\ngot\n%s", want, out)
			}
			wantNotIn(t, "stdout", out, "Folder:", "Summary", "[y/N]")
		})
	}
}

// Commit signing is information: off, it is a ! line and setup goes on.
func TestSetupNotesCommitSigningOff(t *testing.T) {
	f := newFakeSetup(t, false).ghOnGitHub(t)
	f.env.signing = func(context.Context) string { return "" }
	code, out, errOut := f.run("", "--dir", filepath.Join(f.env.home, "wiki"), "--remote", "git@github.com:me/eva-wiki.git")
	f.wantRemote(t, code, out, errOut, "git@github.com:me/eva-wiki.git")
	wantIn(t, "stdout", out, "  ✓ gh signed in to github.com\n  ! Commit signing off: the wiki's commits won't be signed\n")
}

// The marks are coloured, green, red and yellow, only when stdout is a
// terminal and NO_COLOR is unset; otherwise the same text has no escape
// codes.
func TestSetupColoursItsMarksOnlyForATerminal(t *testing.T) {
	// A terminal: the controlling side of a new pseudo-terminal is one.
	pty, err := os.OpenFile("/dev/ptmx", os.O_RDWR, 0)
	if err != nil {
		t.Fatalf("no pseudo-terminal: %v", err)
	}
	defer pty.Close()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	defer w.Close()
	null, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatal(err)
	}
	defer null.Close()
	for _, tc := range []struct {
		name    string
		stdout  *os.File
		noColor string
		want    bool
	}{
		{"a terminal", pty, "", true},
		{"a terminal with NO_COLOR", pty, "1", false},
		{"a pipe", w, "", false},
		{"the null device", null, "", false},
	} {
		if got := useColor(tc.stdout, tc.noColor); got != tc.want {
			t.Errorf("%s: colour %t, want %t", tc.name, got, tc.want)
		}
	}

	f := newFakeSetup(t, true)
	f.env.color = true
	f.env.signing = func(context.Context) string { return "" }
	f.env.lookPath = func(name string) (string, error) {
		if name == "codex" {
			return "", errors.New("not found")
		}
		return "/usr/bin/" + name, nil
	}
	code, out, _ := f.run("", "--host", "codex")
	if code != 1 {
		t.Fatalf("code %d\n%s", code, out)
	}
	wantIn(t, "stdout", out, "  \x1b[32m✓\x1b[0m macOS\n", "  \x1b[31m✗\x1b[0m Codex isn't installed (no codex command)\n    Fix: install Codex, or leave out --host\n")
	f.env.lookPath = func(name string) (string, error) { return "/usr/bin/" + name, nil }
	_, out, _ = f.run("")
	wantIn(t, "stdout", out, "  \x1b[33m!\x1b[0m Commit signing off: the wiki's commits won't be signed\n")
	f.env.color = false
	_, out, _ = f.run("")
	wantIn(t, "stdout", out, "  ✓ macOS\n", "  ! Commit signing off")
	wantNotIn(t, "stdout", out, "\x1b[")
}

// The command itself colours the marks as stdout and NO_COLOR say: not
// through a pipe, on a terminal, and not on a terminal with NO_COLOR set.
// Setup stops at its git check, with no git on PATH, so it touches nothing.
func TestSetupCommandColoursForAStdoutTerminalOnly(t *testing.T) {
	isolated(t)
	t.Setenv("PATH", t.TempDir())
	pty, err := os.OpenFile("/dev/ptmx", os.O_RDWR, 0)
	if err != nil {
		t.Fatalf("no pseudo-terminal: %v", err)
	}
	defer pty.Close()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	defer w.Close()
	saved := os.Stdout
	t.Cleanup(func() { os.Stdout = saved })
	for _, tc := range []struct {
		name    string
		stdout  *os.File
		noColor string
		want    bool
	}{
		{"a pipe", w, "", false},
		{"a terminal", pty, "", true},
		{"a terminal with NO_COLOR", pty, "1", false},
	} {
		t.Setenv("NO_COLOR", tc.noColor)
		os.Stdout = tc.stdout
		var out, errOut bytes.Buffer
		code := runSetupCommand(context.Background(), nil, strings.NewReader(""), &out, &errOut)
		os.Stdout = saved
		if code != 1 || !strings.Contains(out.String(), "git isn't installed\n    Fix: xcode-select --install\n") {
			t.Fatalf("%s: code %d\n%s\n%s", tc.name, code, out.String(), errOut.String())
		}
		if got := strings.Contains(out.String(), "\x1b[31m✗\x1b[0m git isn't installed"); got != tc.want {
			t.Errorf("%s: coloured %t, want %t:\n%q", tc.name, got, tc.want, out.String())
		}
		if !tc.want {
			wantNotIn(t, tc.name, out.String(), "\x1b[")
		}
	}
}
