package main

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/heyNag/eva-md/internal/config"
	"github.com/heyNag/eva-md/internal/health"
	"github.com/heyNag/eva-md/internal/install"
	"github.com/heyNag/eva-md/internal/paths"
	"github.com/heyNag/eva-md/internal/wikigit"
	"github.com/heyNag/eva-md/internal/wikiinit"
)

// fakeSetup records what the guided setup asked the real steps to do.
type fakeSetup struct {
	env      setupEnv
	inits    []wikiinit.Options
	installs []install.Options
	peeks    []string        // the remotes setup read before using them
	public   map[string]bool // the repos, by identity, anyone can read
	reads    []string        // the repos setup tried to read with no sign-in
	initErr  error
	bin      string // PATH, where a test puts fake gh and glab scripts
	tmp      string // the temporary folder realGit gives git and setup
}

// realGitPath is the real git, found before any test replaces PATH.
var realGitPath, _ = exec.LookPath("git")

func newFakeSetup(t *testing.T, interactive bool) *fakeSetup {
	t.Helper()
	d := isolated(t)
	home, err := filepath.EvalSymlinks(os.Getenv("HOME"))
	if err != nil {
		t.Fatal(err)
	}
	// Setup finds gh and glab on PATH, so tests only ever see the fakes
	// they write into bin, never the real CLIs. The host and token
	// variables the fakes report start empty.
	f := &fakeSetup{bin: t.TempDir(), public: map[string]bool{}}
	t.Setenv("PATH", f.bin)
	// No test reads a repo over the network.
	saved := anyoneCanRead
	anyoneCanRead = func(_ context.Context, id string) bool { f.reads = append(f.reads, id); return f.public[id] }
	t.Cleanup(func() { anyoneCanRead = saved })
	for _, key := range []string{"GH_HOST", "GITLAB_HOST", "GITLAB_TOKEN", "GITLAB_ACCESS_TOKEN", "OAUTH_TOKEN"} {
		t.Setenv(key, "")
	}
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
		// Every repo reads as empty, unless a test uses realGit.
		peek: func(_ context.Context, remote string) (string, error) {
			f.peeks = append(f.peeks, remote)
			return "", nil
		},
		machineLabel: func() (string, error) { return "this-mac", nil },
	}
	return f
}

func (f *fakeSetup) run(input string, args ...string) (int, string, string) {
	var out, errOut bytes.Buffer
	code := runSetup(context.Background(), args, strings.NewReader(input), &out, &errOut, f.env)
	return code, out.String(), errOut.String()
}

// realGit lets setup run the real git, the real init and peekRemote. The
// made-up hosts' SSH addresses reach bare repos in <home>/remotes through a
// fake ssh: git@github.com:me/eva-wiki.git is <home>/remotes/me/eva-wiki.git.
// git may use no other transport than that one and local paths, so nothing
// reaches the network. Commits are made by a made-up name without signing.
func (f *fakeSetup) realGit(t *testing.T) {
	t.Helper()
	if realGitPath == "" {
		t.Fatal("git is not installed")
	}
	gitconfig := "[user]\n\tname = Eva Fixture\n\temail = fixture@example.invalid\n[commit]\n\tgpgsign = false\n[init]\n\tdefaultBranch = main\n[ssh]\n\tvariant = simple\n"
	if err := os.WriteFile(os.Getenv("GIT_CONFIG_GLOBAL"), []byte(gitconfig), 0o600); err != nil {
		t.Fatal(err)
	}
	// A link, not a script: macOS checks a new script on its first run.
	if err := os.Symlink(realGitPath, filepath.Join(f.bin, "git")); err != nil {
		t.Fatal(err)
	}
	// As the simple variant, ssh gets the host and the command, such as
	// git-upload-pack 'me/eva-wiki.git', which it runs on the bare repo.
	t.Setenv("GIT_SSH_COMMAND", `/bin/sh -c 'eval "set -- $2"; exec git "${1#git-}" "$HOME/remotes/$2"' fake-ssh`)
	t.Setenv("GIT_ALLOW_PROTOCOL", "file:ssh")
	f.tmp = t.TempDir()
	t.Setenv("TMPDIR", f.tmp)
	f.env.initWiki = func(ctx context.Context, o wikiinit.Options) (wikiinit.Result, error) {
		f.inits = append(f.inits, o)
		return wikiinit.Run(ctx, o)
	}
	f.env.peek = func(ctx context.Context, remote string) (string, error) {
		f.peeks = append(f.peeks, remote)
		return peekRemote(ctx, remote)
	}
}

// gitIn runs git in dir as setup runs it.
func gitIn(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := wikigit.Run(context.Background(), time.Minute, dir, args...)
	if err != nil {
		t.Fatalf("git %s: %v", strings.Join(args, " "), err)
	}
	return out
}

// bareRepo makes the empty bare repo behind git@github.com:me/<name>.git.
func (f *fakeSetup) bareRepo(t *testing.T, name string) string {
	t.Helper()
	bare := filepath.Join(f.env.home, "remotes", "me", name+".git")
	if err := os.MkdirAll(filepath.Dir(bare), 0o700); err != nil {
		t.Fatal(err)
	}
	gitIn(t, filepath.Dir(bare), "init", "--quiet", "--bare", bare)
	return bare
}

// evaWiki makes an eva wiki in folder and pushes it to remote, an empty
// repo, as setup on the Mac with this label leaves it, without touching f's
// config.
func evaWiki(t *testing.T, folder, remote, label string) {
	t.Helper()
	tmp := t.TempDir()
	dirs := paths.Dirs{Config: filepath.Join(tmp, "config"), State: filepath.Join(tmp, "state"), Share: filepath.Join(tmp, "share")}
	if _, err := wikiinit.Run(context.Background(), wikiinit.Options{Remote: remote, Dir: folder, Dirs: dirs, Version: "test", OKFModule: "v0.7.2", NoPush: true}); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(folder, ".eva-md", "schema.toml")
	schema, err := config.LoadSchema(file)
	if err != nil {
		t.Fatal(err)
	}
	schema.Instance = label + "-1a2b3c4d"
	if err := config.SaveSchema(file, schema); err != nil {
		t.Fatal(err)
	}
	gitIn(t, folder, "commit", "--quiet", "--all", "--message", "Fixture instance")
	gitIn(t, folder, "push", "--quiet", "origin", "HEAD:refs/heads/main")
}

// plainRepo pushes a README, which isn't an eva wiki, to the bare repo
// behind git@github.com:me/<name>.git.
func (f *fakeSetup) plainRepo(t *testing.T, name string) {
	t.Helper()
	bare := f.bareRepo(t, name)
	src := t.TempDir()
	gitIn(t, src, "init", "--quiet")
	if err := os.WriteFile(filepath.Join(src, "README.md"), []byte("# Notes\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	gitIn(t, src, "add", "README.md")
	gitIn(t, src, "commit", "--quiet", "--message", "README")
	gitIn(t, src, "push", "--quiet", bare, "HEAD:refs/heads/main")
}

// setUp sets f's Mac up as setup leaves it: a wiki in <home>/projects/wiki,
// pushed to remote, an empty repo. It returns the config.
func (f *fakeSetup) setUp(t *testing.T, remote string) config.Config {
	t.Helper()
	o := wikiinit.Options{Remote: remote, Dir: filepath.Join(f.env.home, "projects", "wiki"), Dirs: f.env.dirs, Version: "test", OKFModule: "v0.7.2"}
	if _, err := wikiinit.Run(context.Background(), o); err != nil {
		t.Fatal(err)
	}
	c, err := config.Load(f.env.dirs.ConfigFile())
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// noTempLeft fails when a folder peekRemote made is left behind.
func (f *fakeSetup) noTempLeft(t *testing.T) {
	t.Helper()
	if left, _ := filepath.Glob(filepath.Join(f.tmp, "eva-md-setup-*")); len(left) != 0 {
		t.Fatalf("temporary folders left: %v", left)
	}
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
	if len(f.peeks) != 1 || f.peeks[0] != "git@github.com:me/eva-wiki.git" {
		t.Fatalf("read the repos %q", f.peeks)
	}
	for _, want := range []string{"protects from background updates", "Paste an address like",
		"   Checking the repo with git; your git setup may ask you to approve.\n   The repo is empty: setup will create the wiki in it.\n",
		"Summary", "Setup will create a new wiki in this folder and push it, then install the plugins.\nSet up eva.md with these choices? [y/N] ",
		"type /hooks", "1 passed, 0 failed, 1 unknown", "eva.md is ready.", "where did we stop?"} {
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
	if strings.Contains(out, "[y/N]") || strings.Contains(out, "[Y/n]") || len(f.peeks) != 0 {
		t.Fatalf("asked, or read the repo %q, without a terminal:\n%s", f.peeks, out)
	}
	wantIn(t, "stdout", out, "Setup will create the wiki in this folder, or use the one the repo already holds, then install the plugins.\n")
}

func TestSetupRefusesProtectedFolderFlag(t *testing.T) {
	f := newFakeSetup(t, false)
	code, _, errOut := f.run("", "--dir", filepath.Join(f.env.home, "Desktop", "wiki"), "--remote", "git@github.com:me/eva-wiki.git")
	if code != 1 || len(f.inits) != 0 || !strings.Contains(errOut, "on the Desktop") {
		t.Fatalf("code %d inits %d stderr %q", code, len(f.inits), errOut)
	}
}

func TestSetupKeepsAHealthyWiki(t *testing.T) {
	f := newFakeSetup(t, true)
	f.realGit(t)
	f.bareRepo(t, "eva-wiki")
	c := f.setUp(t, "git@github.com:me/eva-wiki.git")
	code, out, errOut := f.run("y\n")
	if code != 0 || len(f.inits) != 0 || len(f.installs) != 1 {
		t.Fatalf("code %d inits %d installs %d\n%s\n%s", code, len(f.inits), len(f.installs), out, errOut)
	}
	// One line, then the question, without a summary that repeats it.
	wantIn(t, "stdout", out, "\neva-md is set up here: the wiki is in "+c.WikiPath+", pushed to github.com/me/eva-wiki.\nInstall or update the plugins? [y/N] ")
	wantNotIn(t, "stdout", out, "1. Where should", "2. Which private", "Summary")

	// Without a terminal it asks nothing.
	f.env.interactive = false
	if code, out, _ := f.run(""); code != 0 || len(f.installs) != 2 || strings.Contains(out, "[y/N]") {
		t.Fatalf("code %d installs %d\n%s", code, len(f.installs), out)
	}
	if code, _, errOut := f.run("", "--dir", filepath.Join(f.env.home, "elsewhere")); code != 1 || !strings.Contains(errOut, "won't move an existing wiki") {
		t.Fatalf("code %d stderr %q", code, errOut)
	}
	if code, _, errOut := f.run("", "--remote", "git@github.com:me/other-wiki.git"); code != 1 || !strings.Contains(errOut, "pushed to a different remote") {
		t.Fatalf("code %d stderr %q", code, errOut)
	}
}

func TestSetupClonesAMissingWikiAgain(t *testing.T) {
	// missing sets f's Mac up with a wiki pushed to remote, then removes its
	// folder.
	missing := func(t *testing.T, f *fakeSetup, remote string) config.Config {
		t.Helper()
		f.realGit(t)
		f.bareRepo(t, "eva-wiki")
		c := f.setUp(t, remote)
		// Settings the config holds stay as they are.
		c.Exclude = []string{"~/private"}
		if err := config.Save(f.env.dirs.ConfigFile(), c); err != nil {
			t.Fatal(err)
		}
		if err := os.RemoveAll(c.WikiPath); err != nil {
			t.Fatal(err)
		}
		return c
	}
	// again presses Enter at the question and checks that setup cloned the
	// wiki again from this address into its folder, which was missing or
	// empty as state says, as it was, asking nothing more and pushing nothing.
	again := func(t *testing.T, f *fakeSetup, c config.Config, state, from string, args ...string) {
		t.Helper()
		code, out, errOut := f.run("\n", args...)
		if code != 0 || len(f.inits) != 1 || f.inits[0].Remote != from || f.inits[0].Dir != c.WikiPath || !f.inits[0].NoPush || len(f.installs) != 1 {
			t.Fatalf("code %d inits %+v installs %d\n%s\n%s", code, f.inits, len(f.installs), out, errOut)
		}
		wantIn(t, "stdout", out,
			"\nYour wiki folder "+c.WikiPath+" is "+state+". Clone it again from "+from+" into "+c.WikiPath+"? [Y/n] \n",
			"Setup will clone the wiki again into its folder, then install or update the plugins.\n\nCloning the wiki\n",
			"  Using the existing eva wiki in "+c.WikiPath+"\n")
		after, err := config.Load(f.env.dirs.ConfigFile())
		if err != nil || after.Instance != c.Instance || after.WikiPath != c.WikiPath || after.ExpectedRemote != c.ExpectedRemote || len(after.Exclude) != 1 {
			t.Fatalf("config %+v, was %+v (%v)", after, c, err)
		}
		if code, out, errOut := f.run("y\n"); code != 0 || !strings.Contains(out, "eva-md is set up here") {
			t.Fatalf("after cloning again: code %d\n%s\n%s", code, out, errOut)
		}
	}

	t.Run("by its SSH address", func(t *testing.T) {
		f := newFakeSetup(t, true)
		c := missing(t, f, "git@github.com:me/eva-wiki.git")
		// Declined, or without a terminal: nothing changes.
		want := "eva-md: the config points to a wiki at " + c.WikiPath + ", which doesn't exist; restore it, or move " + f.env.dirs.ConfigFile() + " aside to start over\n"
		code, out, errOut := f.run("n\n")
		if code != 1 || errOut != want || !strings.Contains(out, "? [Y/n] ") {
			t.Fatalf("declined: code %d\n%s\n%s", code, out, errOut)
		}
		f.env.interactive = false
		code, out, errOut = f.run("")
		if code != 1 || errOut != want || strings.Contains(out, "[Y/n]") {
			t.Fatalf("without a terminal: code %d\n%s\n%s", code, out, errOut)
		}
		if _, err := os.Stat(c.WikiPath); !errors.Is(err, os.ErrNotExist) || len(f.inits) != 0 || len(f.installs) != 0 {
			t.Fatalf("something changed: %v, inits %d, installs %d", err, len(f.inits), len(f.installs))
		}
		// gh set to use HTTPS gives the HTTPS address, as git pushed the
		// wiki over HTTPS before. The tests' git reaches no HTTPS repo, so
		// the repo's check answers for it.
		f.env.interactive = true
		realPeek := f.env.peek
		f.env.peek = func(context.Context, string) (string, error) { return c.Instance, nil }
		f.fakes(t, fakeCLI{name: "gh", protocol: "https", exists: "me/eva-wiki", before: ghJSON("eva-wiki", "PRIVATE", false)})
		if code, out, _ := f.run("n\n"); code != 1 || !strings.Contains(out, " Clone it again from https://github.com/me/eva-wiki.git into "+c.WikiPath+"? [Y/n] ") {
			t.Fatalf("code %d\n%s", code, out)
		}
		// A repo renamed since answers with its new address, which setup
		// doesn't use.
		f.fakes(t, fakeCLI{name: "gh", exists: "me/eva-wiki", before: ghJSON("eva-memory", "PRIVATE", false)})
		if code, out, _ := f.run("n\n"); code != 1 || !strings.Contains(out, " Clone it again from git@github.com:me/eva-wiki.git into "+c.WikiPath+"? [Y/n] ") {
			t.Fatalf("code %d\n%s", code, out)
		}
		// Without gh, the SSH address. When git can't read the repo, setup
		// says what to check before it asks.
		f.env.peek = realPeek
		if err := os.Remove(filepath.Join(f.bin, "gh")); err != nil {
			t.Fatal(err)
		}
		bare := filepath.Join(f.env.home, "remotes", "me", "eva-wiki.git")
		if err := os.Rename(bare, bare+".aside"); err != nil {
			t.Fatal(err)
		}
		code, out, errOut = f.run("\n")
		if code != 1 || len(f.inits) != 0 || len(f.installs) != 0 || strings.Contains(out, "[Y/n]") || errOut != "eva-md: git@github.com:me/eva-wiki.git: git couldn't read this repo; check its address and that git can sign in to it; or run eva-md setup --remote with the right address\n" {
			t.Fatalf("an unreadable repo: code %d\n%s\n%s", code, out, errOut)
		}
		if err := os.Rename(bare+".aside", bare); err != nil {
			t.Fatal(err)
		}
		f.inits = nil
		again(t, f, c, "missing", "git@github.com:me/eva-wiki.git")
	})
	t.Run("from a local remote", func(t *testing.T) {
		f := newFakeSetup(t, true)
		remote := filepath.Join(f.env.home, "remotes", "me", "eva-wiki.git")
		again(t, f, missing(t, f, remote), "missing", remote)
	})
	t.Run("from --remote, with --yes", func(t *testing.T) {
		f := newFakeSetup(t, true)
		c := missing(t, f, "git@github.com:me/eva-wiki.git")
		const remote = "ssh://git@github.com/me/eva-wiki.git"
		again(t, f, c, "missing", remote, "--remote", remote, "--yes")
	})
	t.Run("into its empty folder", func(t *testing.T) {
		f := newFakeSetup(t, true)
		c := missing(t, f, "git@github.com:me/eva-wiki.git")
		if err := os.Mkdir(c.WikiPath, 0o700); err != nil {
			t.Fatal(err)
		}
		again(t, f, c, "empty", "git@github.com:me/eva-wiki.git")
	})
	t.Run("not from an empty repo or another wiki", func(t *testing.T) {
		// Cloned again, either would become a new wiki in place of the
		// configured one, so setup stops before it asks.
		f := newFakeSetup(t, true)
		c := missing(t, f, "git@github.com:me/eva-wiki.git")
		bare := filepath.Join(f.env.home, "remotes", "me", "eva-wiki.git")
		if err := os.RemoveAll(bare); err != nil {
			t.Fatal(err)
		}
		f.bareRepo(t, "eva-wiki")
		code, out, errOut := f.run("\n")
		want := "eva-md: the repo at git@github.com:me/eva-wiki.git is empty, so your wiki can't be cloned again from it; push your wiki there, or move " + f.env.dirs.ConfigFile() + " aside to start over\n"
		if code != 1 || len(f.inits) != 0 || len(f.installs) != 0 || strings.Contains(out, "[Y/n]") || errOut != want {
			t.Fatalf("an empty repo: code %d\n%s\n%s", code, out, errOut)
		}
		f.env.peek = func(context.Context, string) (string, error) { return "another-mac-1a2b3c4d", nil }
		code, out, errOut = f.run("\n")
		if code != 1 || len(f.inits) != 0 || strings.Contains(out, "[Y/n]") || !strings.Contains(errOut, " holds another eva wiki than the one the config names; ") {
			t.Fatalf("another wiki: code %d\n%s\n%s", code, out, errOut)
		}
		if after, err := config.Load(f.env.dirs.ConfigFile()); err != nil || after.Instance != c.Instance {
			t.Fatalf("the config changed: %+v %v", after, err)
		}
	})
	t.Run("whose parent folder is missing too", func(t *testing.T) {
		// As on a volume that isn't mounted: no is the default, and nothing
		// is made.
		f := newFakeSetup(t, true)
		c := missing(t, f, "git@github.com:me/eva-wiki.git")
		parent := filepath.Dir(c.WikiPath)
		if err := os.Remove(parent); err != nil {
			t.Fatal(err)
		}
		// The repo is read only after a yes, which most don't give here.
		peeks := len(f.peeks)
		code, out, errOut := f.run("\n")
		if code != 1 || len(f.inits) != 0 || len(f.peeks) != peeks || !strings.HasSuffix(out, "\nYour wiki folder "+c.WikiPath+" is missing, and its parent folder is missing too. Clone it again from git@github.com:me/eva-wiki.git into "+c.WikiPath+"? [y/N] ") || !strings.Contains(errOut, ", which doesn't exist; restore it") {
			t.Fatalf("code %d inits %d\n%s\n%s", code, len(f.inits), out, errOut)
		}
		if _, err := os.Stat(parent); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("the parent folder was made: %v", err)
		}
		// A yes reads the repo first, then clones.
		code, out, errOut = f.run("y\n")
		if code != 0 || len(f.peeks) != peeks+1 || len(f.inits) != 1 || !strings.Contains(out, "? [y/N] Checking the repo with git") {
			t.Fatalf("yes: code %d peeks %d inits %d\n%s\n%s", code, len(f.peeks)-peeks, len(f.inits), out, errOut)
		}
	})
}

func TestSetupStopsWhenTheConfiguredWikiDoesNotMatch(t *testing.T) {
	f := newFakeSetup(t, true)
	f.realGit(t)
	f.bareRepo(t, "eva-wiki")
	c := f.setUp(t, "git@github.com:me/eva-wiki.git")
	aside := filepath.Join(f.env.home, "aside")
	// move renames from to to; each case below undoes its change after.
	move := func(t *testing.T, from, to string) {
		t.Helper()
		if err := os.Rename(from, to); err != nil {
			t.Error(err)
		}
	}
	setInstance := func(t *testing.T, instance string) {
		t.Helper()
		changed := c
		changed.Instance = instance
		if err := config.Save(f.env.dirs.ConfigFile(), changed); err != nil {
			t.Error(err)
		}
	}
	schema := filepath.Join(c.WikiPath, ".eva-md", "schema.toml")
	for _, tc := range []struct {
		name, problem string
		change, undo  func(t *testing.T)
	}{
		{"not a clone", "the wiki folder %s isn't a git clone",
			func(t *testing.T) { move(t, filepath.Join(c.WikiPath, ".git"), aside) },
			func(t *testing.T) { move(t, aside, filepath.Join(c.WikiPath, ".git")) }},
		{"another origin", "the wiki folder %s isn't a clone of github.com/me/eva-wiki",
			func(t *testing.T) {
				gitIn(t, c.WikiPath, "remote", "set-url", "origin", "git@github.com:me/other-wiki.git")
			},
			func(t *testing.T) {
				gitIn(t, c.WikiPath, "remote", "set-url", "origin", "git@github.com:me/eva-wiki.git")
			}},
		{"no schema", "the wiki folder %s has no readable .eva-md/schema.toml",
			func(t *testing.T) { move(t, schema, aside) },
			func(t *testing.T) { move(t, aside, schema) }},
		{"another instance", "the wiki in %s is another eva wiki than the one the config names",
			func(t *testing.T) { setInstance(t, "old-laptop-1a2b3c4d") },
			func(t *testing.T) { setInstance(t, c.Instance) }},
		{"a file", "the config points to a wiki at %s, which isn't a folder",
			func(t *testing.T) {
				move(t, c.WikiPath, aside)
				if err := os.WriteFile(c.WikiPath, []byte("notes"), 0o600); err != nil {
					t.Error(err)
				}
			},
			func(t *testing.T) {
				if err := os.Remove(c.WikiPath); err != nil {
					t.Error(err)
				}
				move(t, aside, c.WikiPath)
			}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tc.change(t)
			defer tc.undo(t)
			before, _ := config.Load(f.env.dirs.ConfigFile())
			code, out, errOut := f.run("y\ny\n")
			want := "eva-md: " + strings.Replace(tc.problem, "%s", c.WikiPath, 1) + "; restore your wiki there, or move " + f.env.dirs.ConfigFile() + " aside to start over\n"
			if code != 1 || errOut != want || len(f.inits) != 0 || len(f.installs) != 0 || strings.Contains(out, "[") {
				t.Fatalf("code %d inits %d installs %d\nstdout:\n%s\nstderr:\n%s\nwant %q", code, len(f.inits), len(f.installs), out, errOut, want)
			}
			if after, _ := config.Load(f.env.dirs.ConfigFile()); after.Instance != before.Instance || after.WikiPath != before.WikiPath {
				t.Fatal("the config changed")
			}
		})
	}
	// Undone, the wiki is as setup left it.
	if code, out, errOut := f.run("y\n"); code != 0 || !strings.Contains(out, "eva-md is set up here") {
		t.Fatalf("code %d\n%s\n%s", code, out, errOut)
	}
	t.Run("an unreadable config", func(t *testing.T) {
		if err := os.WriteFile(f.env.dirs.ConfigFile(), []byte("wiki_path = ["), 0o600); err != nil {
			t.Fatal(err)
		}
		code, _, errOut := f.run("y\n")
		if code != 1 || errOut != "eva-md: the eva-md config at "+f.env.dirs.ConfigFile()+" can't be read; fix or move it aside and run setup again\n" {
			t.Fatalf("code %d stderr %q", code, errOut)
		}
	})
}

func TestSetupUsesTheEvaWikiInTheFolder(t *testing.T) {
	// The folder holds a wiki made on old-laptop, whose config went.
	f := newFakeSetup(t, true)
	f.realGit(t)
	f.bareRepo(t, "eva-wiki")
	folder := filepath.Join(f.env.home, "projects", "wiki")
	evaWiki(t, folder, "git@github.com:me/eva-wiki.git", "old-laptop")
	const found = "   This folder is already an eva wiki, pushed to git@github.com:me/eva-wiki.git."
	// stopped checks that a run stopped before changing anything.
	stopped := func(t *testing.T, code int, out, errOut string) {
		t.Helper()
		if code != 1 || len(f.inits) != 0 || errOut == "" {
			t.Fatalf("code %d inits %d\n%s\n%s", code, len(f.inits), out, errOut)
		}
	}

	t.Run("pushed to a public repo", func(t *testing.T) {
		f.fakes(t, fakeCLI{name: "gh", exists: "me/eva-wiki", before: ghJSON("eva-wiki", "PUBLIC", false)})
		code, out, errOut := f.answers(t)
		stopped(t, code, out, errOut)
		wantIn(t, "stdout", out, found+"\n   This repo is public; the wiki holds your chats' summaries, so use a private repo\n   Folder: ")
	})
	f.fakes(t, fakeCLI{name: "gh", exists: "me/eva-wiki", before: ghJSON("eva-wiki", "PRIVATE", false)})
	t.Run("another Mac's", func(t *testing.T) {
		// This Mac is this-mac, and no is the default.
		code, out, errOut := f.answers(t, "")
		stopped(t, code, out, errOut)
		wantIn(t, "stdout", out, "   Folder: "+found+"\n"+otherMac+"   Choose a new or empty folder instead\n   Folder: ")
	})
	t.Run("another --remote", func(t *testing.T) {
		code, out, errOut := f.run("", "--dir", folder, "--remote", "git@github.com:me/other-wiki.git")
		stopped(t, code, out, errOut)
		if errOut != "eva-md: --remote names another repo; leave it out to use this wiki, or choose another folder\n" {
			t.Fatalf("stderr %q", errOut)
		}
	})
	f.env.machineLabel = func() (string, error) { return "old-laptop", nil }
	t.Run("this Mac's, declined", func(t *testing.T) {
		code, out, errOut := f.answers(t, "n")
		stopped(t, code, out, errOut)
		wantIn(t, "stdout", out, "   Folder: "+found+" Use it? [Y/n]    Choose a new or empty folder instead\n   Folder: ")
		// By --dir, it says why it stopped.
		code, out, errOut = f.run("n\n", "--dir", folder)
		stopped(t, code, out, errOut)
		if errOut != "eva-md: you chose not to use the eva wiki in "+folder+"; choose a new or empty folder instead\n" {
			t.Fatalf("stderr %q", errOut)
		}
	})
	t.Run("this Mac's", func(t *testing.T) {
		code, out, errOut := f.answers(t, "", "y")
		f.wantRemote(t, code, out, errOut, "git@github.com:me/eva-wiki.git")
		wantIn(t, "stdout", out,
			"   Folder: "+found+" Use it? [Y/n] \n",
			"  Pushed to     git@github.com:me/eva-wiki.git\n",
			"Setup will use the wiki already in this folder, then install the plugins.\n",
			"Using the wiki in this folder\n", "  Using the existing eva wiki in "+folder+"\n")
		wantNotIn(t, "stdout", out, "2. Which private", "Checking the repo")
		if c, err := config.Load(f.env.dirs.ConfigFile()); err != nil || c.Instance != "old-laptop-1a2b3c4d" || c.WikiPath != folder {
			t.Fatalf("config %+v %v", c, err)
		}
		// gh was asked whether the repo is private.
		wantIn(t, "gh log", f.log(t, "gh"), "repo view me/eva-wiki --json")
	})
	t.Run("without a terminal", func(t *testing.T) {
		// As before: init adopts it and nothing is asked, though another Mac
		// made it. The Mac isn't set up, whichever subtests ran before.
		if err := os.Remove(f.env.dirs.ConfigFile()); err != nil && !errors.Is(err, os.ErrNotExist) {
			t.Fatal(err)
		}
		f.env.interactive, f.inits = false, nil
		f.env.machineLabel = func() (string, error) { return "this-mac", nil }
		code, out, errOut := f.run("", "--dir", folder, "--remote", "git@github.com:me/eva-wiki.git")
		f.wantRemote(t, code, out, errOut, "git@github.com:me/eva-wiki.git")
		wantNotIn(t, "stdout", out, "[Y/n]", "[y/N]", "set up on old-laptop")
	})
}

func TestSetupRefusesAFolderThatIsAnotherClone(t *testing.T) {
	// The folder is a clone of notes, which isn't an eva wiki: refused at the
	// folder question, before gh is asked anything.
	f := newFakeSetup(t, true).fakes(t, fakeCLI{name: "gh", after: ghJSON("eva-wiki", "PRIVATE", true)})
	f.realGit(t)
	f.plainRepo(t, "notes")
	folder := filepath.Join(f.env.home, "projects", "wiki")
	if err := os.MkdirAll(filepath.Dir(folder), 0o700); err != nil {
		t.Fatal(err)
	}
	gitIn(t, filepath.Dir(folder), "clone", "--quiet", "git@github.com:me/notes.git", folder)
	code, out, errOut := f.answers(t)
	if code != 1 || len(f.inits) != 0 || !strings.Contains(out, "   Folder:    That folder is a clone of another repository; choose a new or empty folder\n   Folder: ") {
		t.Fatalf("code %d inits %d\n%s\n%s", code, len(f.inits), out, errOut)
	}
	if log := f.log(t, "gh"); log != "" {
		t.Fatalf("gh was asked:\n%s", log)
	}
	// An empty folder inside a repository is a new folder, not a clone.
	inner := filepath.Join(folder, "inner")
	if err := os.Mkdir(inner, 0o700); err != nil {
		t.Fatal(err)
	}
	code, out, errOut = f.run(inner + "\n")
	if code != 1 || !strings.Contains(out, "   Folder: \n2. Which private") {
		t.Fatalf("code %d\n%s\n%s", code, out, errOut)
	}
	wantNotIn(t, "gh log", f.log(t, "gh"), "repo create")
}

// A folder that is a git repository with no commits and no remote can't
// hold the wiki, and an eva wiki with uncommitted changes can't be adopted:
// both are refused at the folder question, saying what to do, not after the
// summary.
func TestSetupRefusesARepoWithoutRemoteAndADirtyWiki(t *testing.T) {
	f := newFakeSetup(t, true)
	f.realGit(t)
	folder := filepath.Join(f.env.home, "projects", "wiki")
	if err := os.MkdirAll(folder, 0o700); err != nil {
		t.Fatal(err)
	}
	gitIn(t, folder, "init", "--quiet")
	code, out, errOut := f.answers(t)
	if code != 1 || len(f.inits) != 0 || !strings.Contains(out, "   Folder:    That folder is already a git repository, with no remote; choose a new or empty folder\n   Folder: ") {
		t.Fatalf("a repository without a remote: code %d\n%s\n%s", code, out, errOut)
	}
	// With commits, it is a repository of its own, not a clone.
	if err := os.WriteFile(filepath.Join(folder, "notes.md"), []byte("# Notes\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	gitIn(t, folder, "add", "notes.md")
	gitIn(t, folder, "commit", "--quiet", "--message", "Notes")
	code, out, errOut = f.answers(t)
	if code != 1 || len(f.inits) != 0 || !strings.Contains(out, "   Folder:    That folder is already a git repository with commits; choose a new or empty folder\n   Folder: ") {
		t.Fatalf("a repository with commits: code %d\n%s\n%s", code, out, errOut)
	}
	if err := os.RemoveAll(folder); err != nil {
		t.Fatal(err)
	}
	f.bareRepo(t, "eva-wiki")
	evaWiki(t, folder, "git@github.com:me/eva-wiki.git", "this-mac")
	if err := os.WriteFile(filepath.Join(folder, "general", "notes.md"), []byte("# Notes\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	code, out, errOut = f.answers(t)
	if code != 1 || len(f.inits) != 0 || strings.Contains(out, "[Y/n]") || !strings.Contains(out, "   Folder:    This eva wiki has uncommitted changes; commit them in "+folder+" (git add -A && git commit) and run setup again\n") {
		t.Fatalf("a dirty wiki: code %d\n%s\n%s", code, out, errOut)
	}
	// An eva wiki whose remote was removed can neither be used nor made new.
	if err := os.Remove(filepath.Join(folder, "general", "notes.md")); err != nil {
		t.Fatal(err)
	}
	gitIn(t, folder, "remote", "remove", "origin")
	code, out, errOut = f.answers(t)
	if code != 1 || len(f.inits) != 0 || !strings.Contains(out, "   Folder:    That folder holds an eva wiki with no remote; add its remote back (git remote add origin <address>) or choose a new or empty folder\n") {
		t.Fatalf("an eva wiki with no remote: code %d\n%s\n%s", code, out, errOut)
	}
	// Its local remote on a volume that isn't mounted is right, only absent.
	missing := filepath.Join(f.env.home, "Volumes", "backup", "eva-wiki.git")
	gitIn(t, folder, "remote", "add", "origin", missing)
	code, out, errOut = f.answers(t)
	if code != 1 || len(f.inits) != 0 || !strings.Contains(out, "   Folder:    That folder holds an eva wiki whose local remote "+missing+" isn't there now; mount or restore it and run setup again, or choose a new or empty folder\n") {
		t.Fatalf("an eva wiki whose local remote is absent: code %d\n%s\n%s", code, out, errOut)
	}
	// A file:// URL, there or not, is one setup can't use.
	bare := f.bareRepo(t, "file-wiki")
	gitIn(t, folder, "remote", "set-url", "origin", "file://"+bare)
	code, out, errOut = f.answers(t)
	if code != 1 || len(f.inits) != 0 || !strings.Contains(out, "   Folder:    That folder holds an eva wiki whose remote setup can't use; point it at the wiki's repo (git remote set-url origin <address>) or choose a new or empty folder\n") {
		t.Fatalf("an eva wiki with a file:// remote: code %d\n%s\n%s", code, out, errOut)
	}
}

func TestSetupWithoutTerminalAdoptsAsBefore(t *testing.T) {
	// Another Mac's wiki, by its address: adopted without a question, and
	// without reading the repo first.
	f := newFakeSetup(t, false)
	f.realGit(t)
	f.bareRepo(t, "eva-wiki")
	evaWiki(t, filepath.Join(t.TempDir(), "wiki"), "git@github.com:me/eva-wiki.git", "old-laptop")
	folder := filepath.Join(f.env.home, "projects", "wiki")
	code, out, errOut := f.run("", "--dir", folder, "--remote", "git@github.com:me/eva-wiki.git")
	f.wantRemote(t, code, out, errOut, "git@github.com:me/eva-wiki.git")
	if len(f.peeks) != 0 || strings.Contains(out, "[y/N]") {
		t.Fatalf("read the repo %q, or asked:\n%s", f.peeks, out)
	}
	if c, err := config.Load(f.env.dirs.ConfigFile()); err != nil || c.Instance != "old-laptop-1a2b3c4d" {
		t.Fatalf("config %+v %v", c, err)
	}
}

func TestSetupStopsWhenTheWikiFails(t *testing.T) {
	f := newFakeSetup(t, true)
	f.initErr = errors.New("wikiinit: origin does not match requested remote")
	code, _, errOut := f.run("~/projects/wiki\ngit@github.com:me/eva-wiki.git\ny\n")
	if code != 1 || len(f.installs) != 0 || !strings.Contains(errOut, "before installing anything") || !strings.Contains(errOut, "Origin does not match") {
		t.Fatalf("code %d installs %d stderr %q", code, len(f.installs), errOut)
	}
	// After setup made the repo, it says the repo stays.
	f = newFakeSetup(t, true).fakes(t, fakeCLI{name: "gh", after: ghJSON("eva-wiki", "PRIVATE", true)})
	f.initErr = errors.New("wikigit: git push failed; files are preserved, and signing/authentication settings were not changed")
	code, _, errOut = f.run("~/projects/wiki\n\ny\n")
	if code != 1 || len(f.installs) != 0 || !strings.HasSuffix(errOut, "\neva-md: the new repo stays for the next setup\neva-md: setup stopped before installing anything; fix this and run setup again\n") {
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
	if code, out, errOut := f.run(""); code != 1 || !strings.Contains(errOut, "needs the claude or codex command") || !strings.Contains(out, "  Claude Code    not found (no claude command)\n") {
		t.Fatalf("code %d\n%s\n%s", code, out, errOut)
	}
	if code, _, errOut := f.run("", "--host", "codex"); code != 1 || errOut != "eva-md: the codex command isn't on PATH; install it, or leave out --host\n" {
		t.Fatalf("code %d stderr %q", code, errOut)
	}
	f.env.lookPath = func(string) (string, error) { return "", errors.New("not found") }
	if code, _, errOut := f.run(""); code != 1 || !strings.Contains(errOut, "needs git") {
		t.Fatalf("code %d stderr %q", code, errOut)
	}
}

func TestPeekRemote(t *testing.T) {
	f := newFakeSetup(t, true)
	f.realGit(t)
	f.bareRepo(t, "empty")
	f.bareRepo(t, "eva-wiki")
	evaWiki(t, filepath.Join(t.TempDir(), "wiki"), "git@github.com:me/eva-wiki.git", "old-laptop")
	f.plainRepo(t, "notes")
	// future holds a wiki whose schema a later eva-md wrote.
	f.bareRepo(t, "future")
	future := filepath.Join(t.TempDir(), "wiki")
	evaWiki(t, future, "git@github.com:me/future.git", "old-laptop")
	file := filepath.Join(future, ".eva-md", "schema.toml")
	schema, err := config.LoadSchema(file)
	if err != nil {
		t.Fatal(err)
	}
	schema.SchemaVersion = 2
	if err := config.SaveSchema(file, schema); err != nil {
		t.Fatal(err)
	}
	gitIn(t, future, "commit", "--quiet", "--all", "--message", "Schema 2")
	gitIn(t, future, "push", "--quiet", "origin", "HEAD:refs/heads/main")
	for _, tc := range []struct{ remote, instance, err string }{
		{"git@github.com:me/empty.git", "", ""},
		{"git@github.com:me/eva-wiki.git", "old-laptop-1a2b3c4d", ""},
		{filepath.Join(f.env.home, "remotes", "me", "eva-wiki.git"), "old-laptop-1a2b3c4d", ""},
		{"git@github.com:me/notes.git", "", "this repo has content but isn't an eva wiki; use an empty private repo, or the repo of your eva wiki"},
		{"git@github.com:me/future.git", "", "this repo holds an eva wiki this eva-md can't use; update eva-md"},
		{"git@github.com:me/missing.git", "", "git couldn't read this repo; check its address and that git can sign in to it"},
	} {
		instance, err := peekRemote(context.Background(), tc.remote)
		if instance != tc.instance || (err == nil) != (tc.err == "") || err != nil && err.Error() != tc.err {
			t.Errorf("peekRemote(%s) = %q, %v; want %q, %q", tc.remote, instance, err, tc.instance, tc.err)
		}
	}
	f.noTempLeft(t)
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

func TestYesNo(t *testing.T) {
	for _, tc := range []struct {
		input   string
		def     bool
		yes, ok bool
		retries int
	}{
		{"\n", true, true, true, 0},
		{"\n", false, false, true, 0},
		{"YES\n", false, true, true, 0},
		{"n\n", true, false, true, 0},
		{"sure\nmaybe\ny\n", false, true, true, 2},
		{"a\nb\nc\ny\n", true, false, true, 3},
		{"", true, false, false, 0},
	} {
		var out bytes.Buffer
		answers := bufio.NewReader(strings.NewReader(tc.input))
		ask := func(string) (string, bool) {
			line, err := answers.ReadString('\n')
			return strings.TrimSpace(line), err == nil || line != ""
		}
		yes, ok := yesNo(ask, &out, "Use it? ", tc.def)
		if yes != tc.yes || ok != tc.ok || strings.Count(out.String(), "Please type y or n.") != tc.retries {
			t.Errorf("yesNo(%q, %t) = %t, %t; printed %q", tc.input, tc.def, yes, ok, out.String())
		}
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
