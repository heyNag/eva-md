package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
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
