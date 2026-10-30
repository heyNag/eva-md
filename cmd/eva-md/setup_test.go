package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/heyNag/eva-md/internal/config"
	"github.com/heyNag/eva-md/internal/health"
	"github.com/heyNag/eva-md/internal/install"
	"github.com/heyNag/eva-md/internal/paths"
	"github.com/heyNag/eva-md/internal/wikiinit"
)

// fakeSetup records what the guided setup asked the real steps to do.
type fakeSetup struct {
	env      setupEnv
	inits    []wikiinit.Options
	installs []install.Options
	initErr  error
}

func newFakeSetup(t *testing.T, interactive bool) *fakeSetup {
	t.Helper()
	d := isolated(t)
	home, err := filepath.EvalSymlinks(os.Getenv("HOME"))
	if err != nil {
		t.Fatal(err)
	}
	f := &fakeSetup{}
	f.env = setupEnv{
		dirs:        d,
		interactive: interactive,
		home:        home,
		cwd:         home,
		lookPath:    func(name string) (string, error) { return "/usr/bin/" + name, nil },
		signing:     func(context.Context) string { return "ssh" },
		initWiki: func(_ context.Context, o wikiinit.Options) (wikiinit.Result, error) {
			f.inits = append(f.inits, o)
			return wikiinit.Result{WikiPath: o.Dir, Instance: "test"}, f.initErr
		},
		install: func(_ context.Context, o install.Options) (install.Result, error) {
			f.installs = append(f.installs, o)
			return install.Result{Actions: []string{"replace stable binary"}, NeedsTrust: true}, nil
		},
		inspect: func(context.Context, paths.Dirs) health.Report {
			return health.Report{Checks: []health.Check{{Name: "wiki", Status: "PASS"}, {Name: "updates", Status: "UNKNOWN"}}}
		},
		executable: func() (string, error) { return "/tmp/eva-md", nil },
	}
	return f
}

func (f *fakeSetup) run(input string, args ...string) (int, string, string) {
	var out, errOut bytes.Buffer
	code := runSetup(context.Background(), args, strings.NewReader(input), &out, &errOut, f.env)
	return code, out.String(), errOut.String()
}

func TestSetupAsksForFolderAndRemote(t *testing.T) {
	f := newFakeSetup(t, true)
	input := strings.Join([]string{
		"~/Documents/wiki",  // refused: macOS protects Documents
		"'~/projects/wiki'", // quotes from a pasted path are fine
		"not a remote",      // refused
		"git@github.com:me/eva-wiki.git",
		"y",
	}, "\n") + "\n"
	code, out, errOut := f.run(input)
	if code != 0 {
		t.Fatalf("code %d\nstdout:\n%s\nstderr:\n%s", code, out, errOut)
	}
	if len(f.inits) != 1 || len(f.installs) != 1 {
		t.Fatalf("inits=%d installs=%d", len(f.inits), len(f.installs))
	}
	if want := filepath.Join(f.env.home, "projects", "wiki"); f.inits[0].Dir != want || f.inits[0].Remote != "git@github.com:me/eva-wiki.git" {
		t.Fatalf("init options %+v, want dir %s", f.inits[0], want)
	}
	if f.installs[0].Host != "all" || f.installs[0].Binary != "/tmp/eva-md" {
		t.Fatalf("install options %+v", f.installs[0])
	}
	for _, want := range []string{"protects from background updates", "Paste an address like", "Summary", "type /hooks", "1 passed, 0 failed, 1 unknown", "eva.md is ready.", "where did we stop?"} {
		if !strings.Contains(out, want) {
			t.Errorf("stdout lacks %q:\n%s", want, out)
		}
	}
}

func TestSetupHasNoDefaultFolder(t *testing.T) {
	f := newFakeSetup(t, true)
	// Empty answers never pick a folder; after three tries setup stops.
	code, out, errOut := f.run("\n\n\n")
	if code != 1 || len(f.inits) != 0 || !strings.Contains(errOut, "nothing was changed") || !strings.Contains(out, "Please type an answer") {
		t.Fatalf("code %d inits %d\nstdout:\n%s\nstderr:\n%s", code, len(f.inits), out, errOut)
	}
}

func TestSetupDeclinedChangesNothing(t *testing.T) {
	f := newFakeSetup(t, true)
	code, out, _ := f.run("~/projects/wiki\ngit@github.com:me/eva-wiki.git\nn\n")
	if code != 1 || len(f.inits) != 0 || len(f.installs) != 0 || !strings.Contains(out, "Nothing was changed.") {
		t.Fatalf("code %d inits %d installs %d\n%s", code, len(f.inits), len(f.installs), out)
	}
}

func TestSetupWithoutTerminalNeedsFlags(t *testing.T) {
	f := newFakeSetup(t, false)
	if code, _, errOut := f.run(""); code != 2 || !strings.Contains(errOut, "pass --dir and --remote") {
		t.Fatalf("code %d stderr %q", code, errOut)
	}
	dir := filepath.Join(f.env.home, "wiki-here")
	code, out, errOut := f.run("", "--dir", dir, "--remote", "git@github.com:me/eva-wiki.git", "--host", "codex")
	if code != 0 || len(f.inits) != 1 || f.inits[0].Dir != dir || f.installs[0].Host != "codex" {
		t.Fatalf("code %d inits %+v installs %+v\n%s\n%s", code, f.inits, f.installs, out, errOut)
	}
	if strings.Contains(out, "[y/N]") {
		t.Fatal("asked for confirmation without a terminal")
	}
}

func TestSetupRefusesProtectedFolderFlag(t *testing.T) {
	f := newFakeSetup(t, false)
	code, _, errOut := f.run("", "--dir", filepath.Join(f.env.home, "Desktop", "wiki"), "--remote", "git@github.com:me/eva-wiki.git")
	if code != 1 || len(f.inits) != 0 || !strings.Contains(errOut, "on the Desktop") {
		t.Fatalf("code %d inits %d stderr %q", code, len(f.inits), errOut)
	}
}

func TestSetupKeepsAnExistingWiki(t *testing.T) {
	f := newFakeSetup(t, true)
	wiki := filepath.Join(f.env.home, "my-wiki")
	if err := os.MkdirAll(wiki, 0o700); err != nil {
		t.Fatal(err)
	}
	c := config.Default()
	c.WikiPath, c.ExpectedRemote = wiki, "github.com/me/eva-wiki"
	if err := config.Save(f.env.dirs.ConfigFile(), c); err != nil {
		t.Fatal(err)
	}
	code, out, errOut := f.run("y\n")
	if code != 0 || len(f.inits) != 0 || len(f.installs) != 1 || !strings.Contains(out, "already set up") || !strings.Contains(out, "Install or update the plugins?") {
		t.Fatalf("code %d inits %d installs %d\n%s\n%s", code, len(f.inits), len(f.installs), out, errOut)
	}
	if code, _, errOut := f.run("", "--dir", filepath.Join(f.env.home, "elsewhere")); code != 1 || !strings.Contains(errOut, "won't move an existing wiki") {
		t.Fatalf("code %d stderr %q", code, errOut)
	}
	if err := os.Remove(wiki); err != nil {
		t.Fatal(err)
	}
	if code, _, errOut := f.run("y\n"); code != 1 || !strings.Contains(errOut, "doesn't exist") {
		t.Fatalf("missing wiki: code %d stderr %q", code, errOut)
	}
}

func TestSetupStopsWhenTheWikiFails(t *testing.T) {
	f := newFakeSetup(t, true)
	f.initErr = errors.New("wikiinit: origin does not match requested remote")
	code, _, errOut := f.run("~/projects/wiki\ngit@github.com:me/eva-wiki.git\ny\n")
	if code != 1 || len(f.installs) != 0 || !strings.Contains(errOut, "before installing anything") || !strings.Contains(errOut, "Origin does not match") {
		t.Fatalf("code %d installs %d stderr %q", code, len(f.installs), errOut)
	}
}

func TestSetupNeedsGitAndAHost(t *testing.T) {
	f := newFakeSetup(t, true)
	f.env.lookPath = func(name string) (string, error) {
		if name == "git" {
			return "/usr/bin/git", nil
		}
		return "", errors.New("not found")
	}
	if code, _, errOut := f.run(""); code != 1 || !strings.Contains(errOut, "needs Claude Code or Codex") {
		t.Fatalf("code %d stderr %q", code, errOut)
	}
	f.env.lookPath = func(string) (string, error) { return "", errors.New("not found") }
	if code, _, errOut := f.run(""); code != 1 || !strings.Contains(errOut, "needs git") {
		t.Fatalf("code %d stderr %q", code, errOut)
	}
}

func TestAnswerPath(t *testing.T) {
	env := setupEnv{home: "/Users/me", cwd: "/Users/me/projects"}
	for in, want := range map[string]string{
		"~/projects/wiki":   "/Users/me/projects/wiki",
		"  \"/opt/wiki\"  ": "/opt/wiki",
		"wiki":              "/Users/me/projects/wiki",
		"../notes/wiki/":    "/Users/me/notes/wiki",
		"~":                 "/Users/me",
	} {
		if got, err := answerPath(in, env); err != nil || got != want {
			t.Errorf("answerPath(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	if _, err := answerPath("~other/wiki", env); err == nil {
		t.Error("~user paths should be refused")
	}
}

func TestDevNullIsNotATerminal(t *testing.T) {
	null, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatal(err)
	}
	defer null.Close()
	if isTerminal(null) {
		t.Fatal("/dev/null taken for a terminal")
	}
	if isTerminal(strings.NewReader("")) {
		t.Fatal("a reader taken for a terminal")
	}
}
