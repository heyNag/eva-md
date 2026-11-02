package main

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/heyNag/eva-md/internal/config"
)

// answersFile writes the answers file, config.env, with body.
func (f *fakeSetup) answersFile(t *testing.T, body string) string {
	t.Helper()
	file := f.env.dirs.AnswersFile()
	if err := os.MkdirAll(filepath.Dir(file), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return file
}

// With the folder and the repo in the answers file, setup asks neither
// question, says in its summary where the answers came from, and still asks
// to confirm.
func TestSetupUsesTheAnswersFile(t *testing.T) {
	glab := fakeCLI{name: "glab", signedIn: "gitlab.example.com", host: "gitlab.com"}
	f := newFakeSetup(t, true).fakes(t, glab)
	file := f.answersFile(t, "EVA_MD_WIKI_DIR=~/projects/wiki\nexport EVA_MD_WIKI_REMOTE=\"git@gitlab.example.com:me/eva-wiki.git\"\nEVA_MD_HOSTS=codex\n")
	code, out, errOut := f.run("y\n")
	f.wantRemote(t, code, out, errOut, "git@gitlab.example.com:me/eva-wiki.git")
	if want := filepath.Join(f.env.home, "projects", "wiki"); f.inits[0].Dir != want || f.installs[0].Host != "codex" {
		t.Fatalf("init %+v install %+v", f.inits[0], f.installs[0])
	}
	wantIn(t, "stdout", out,
		"Summary\n  Answers from  "+file+"\n  Wiki folder   "+f.inits[0].Dir+"\n  Pushed to     git@gitlab.example.com:me/eva-wiki.git\n  Plugins for   Codex\n",
		"Set up eva.md with these choices? [y/N] ", "  ✓ Read "+file+"\n  ✓ glab signed in to gitlab.example.com\n")
	wantNotIn(t, "stdout", out, "1. Where should", "2. Which private", "Wrote ")
	// A no changes nothing, as without the file.
	f = newFakeSetup(t, true).fakes(t, glab)
	f.answersFile(t, "EVA_MD_WIKI_DIR=~/projects/wiki\nEVA_MD_WIKI_REMOTE=git@gitlab.example.com:me/eva-wiki.git\n")
	if code, out, _ := f.run("n\n"); code != 1 || len(f.inits) != 0 || !strings.Contains(out, "Nothing was changed.") {
		t.Fatalf("code %d inits %d\n%s", code, len(f.inits), out)
	}
}

// Only the folder in the file: setup asks only for the repo, on the git host
// the file names.
func TestSetupAsksWhatTheAnswersFileLeavesOut(t *testing.T) {
	f := newFakeSetup(t, true).fakes(t, fakeCLI{name: "glab", signedIn: "gitlab.example.com", host: "gitlab.com", after: glabJSON("eva-wiki", "private", true)})
	f.answersFile(t, "EVA_MD_WIKI_DIR=~/projects/wiki\nEVA_MD_GIT_HOST_URL=https://gitlab.example.com/users/your-username/projects\n")
	code, out, errOut := f.run("\ny\n")
	f.wantRemote(t, code, out, errOut, "git@gitlab.example.com:me/eva-wiki.git")
	wantIn(t, "stdout", out, "  ✓ glab signed in to gitlab.example.com\n", "2. Which private git repository should it be pushed to?\n   Setup can create it for you.",
		"   Create a private repo named eva-wiki with glab on gitlab.example.com? [Y/n] ", "  Answers from  ")
	wantNotIn(t, "stdout", out, "1. Where should")
}

// A flag wins over the file, for the folder, the repo and the hosts; the git
// host's flag is in TestSetupPicksTheCLIForTheGitHost.
func TestSetupFlagsBeatTheAnswersFile(t *testing.T) {
	f := newFakeSetup(t, false).ghOnGitHub(t)
	f.answersFile(t, "EVA_MD_WIKI_DIR=~/file-wiki\nEVA_MD_WIKI_REMOTE=git@github.com:me/file-wiki.git\nEVA_MD_HOSTS=codex\n")
	dir := filepath.Join(f.env.home, "flag-wiki")
	code, out, errOut := f.run("", "--dir", dir, "--remote", "git@github.com:me/flag-wiki.git", "--host", "claude")
	f.wantRemote(t, code, out, errOut, "git@github.com:me/flag-wiki.git")
	if f.inits[0].Dir != dir || f.installs[0].Host != "claude" {
		t.Fatalf("init %+v install %+v", f.inits[0], f.installs[0])
	}
	wantNotIn(t, "stdout", out, "Answers from")
	// One flag leaves the other answer to the file.
	f = newFakeSetup(t, false).ghOnGitHub(t)
	file := f.answersFile(t, "EVA_MD_WIKI_DIR=~/file-wiki\nEVA_MD_WIKI_REMOTE=git@github.com:me/file-wiki.git\n")
	code, out, errOut = f.run("", "--remote", "git@github.com:me/flag-wiki.git")
	f.wantRemote(t, code, out, errOut, "git@github.com:me/flag-wiki.git")
	if want := filepath.Join(f.env.home, "file-wiki"); f.inits[0].Dir != want {
		t.Fatalf("dir %s, want %s", f.inits[0].Dir, want)
	}
	wantIn(t, "stdout", out, "  Answers from  "+file+"\n")
}

// Without a terminal, the file's folder and repo are used as --dir and
// --remote are; a file that lacks them stops setup, naming its keys.
func TestSetupWithoutTerminalUsesTheAnswersFile(t *testing.T) {
	f := newFakeSetup(t, false).ghOnGitHub(t)
	file := f.answersFile(t, "EVA_MD_WIKI_DIR="+filepath.Join(f.env.home, "wiki")+"\nEVA_MD_WIKI_REMOTE=git@github.com:me/eva-wiki.git\n")
	code, out, errOut := f.run("")
	f.wantRemote(t, code, out, errOut, "git@github.com:me/eva-wiki.git")
	wantNotIn(t, "stdout", out, "[y/N]", "[Y/n]")
	wantIn(t, "stdout", out, "  Answers from  "+file+"\n")

	f = newFakeSetup(t, false)
	file = f.answersFile(t, "EVA_MD_WIKI_DIR=~/wiki\n")
	code, _, errOut = f.run("")
	if code != 2 || errOut != "eva-md: setup can't ask questions here (no terminal); fill in EVA_MD_WIKI_DIR and EVA_MD_WIKI_REMOTE in "+file+", or pass --dir and --remote\n" {
		t.Fatalf("code %d stderr %q", code, errOut)
	}
}

// A missing file gets the template, never over an existing one. Setup says
// so in a line of its checks, and in a terminal asks its questions as
// before; without one and without --dir and --remote it stops, saying it
// wrote it.
func TestSetupWritesTheAnswersTemplate(t *testing.T) {
	f := newFakeSetup(t, true)
	file := f.env.dirs.AnswersFile()
	code, out, errOut := f.run("~/projects/wiki\ngit@github.com:me/eva-wiki.git\ny\n")
	f.wantRemote(t, code, out, errOut, "git@github.com:me/eva-wiki.git")
	wantIn(t, "stdout", out, "  ✓ Wrote "+file+", a template for setup's answers\n", "\nFill in "+file+" and run setup again, or answer here.\n\n1. Where should the wiki live on this Mac?\n")
	wantNotIn(t, "stdout", out, "Answers from")
	info, err := os.Stat(file)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("the template: %v, %v", info, err)
	}
	if b, _ := os.ReadFile(file); string(b) != config.AnswersTemplate {
		t.Fatalf("the template is %q", b)
	}

	f = newFakeSetup(t, false)
	file = f.env.dirs.AnswersFile()
	code, out, errOut = f.run("")
	if code != 2 || errOut != "eva-md: setup can't ask questions here (no terminal); it wrote "+file+": fill it in and run setup again, or pass --dir and --remote\n" || len(f.inits) != 0 {
		t.Fatalf("code %d inits %d\n%s\n%s", code, len(f.inits), out, errOut)
	}
	if b, _ := os.ReadFile(file); string(b) != config.AnswersTemplate {
		t.Fatalf("the template is %q", b)
	}
	// Filled in, it isn't written again.
	f.ghOnGitHub(t)
	f.answersFile(t, "EVA_MD_WIKI_DIR=~/wiki\nEVA_MD_WIKI_REMOTE=git@github.com:me/eva-wiki.git\n")
	code, out, errOut = f.run("")
	f.wantRemote(t, code, out, errOut, "git@github.com:me/eva-wiki.git")
	if b, _ := os.ReadFile(file); !strings.HasPrefix(string(b), "EVA_MD_WIKI_DIR=~/wiki\n") {
		t.Fatalf("the file was replaced: %q", b)
	}
}

// A bad value, a key setup doesn't know or a file it won't read: a bad one
// stops setup naming the key and the file, an unknown key is named and
// ignored, and no value from the file is printed.
func TestSetupReportsTheAnswersFilesProblems(t *testing.T) {
	f := newFakeSetup(t, false).ghOnGitHub(t)
	file := f.answersFile(t, "EVA_MD_HOSTS=fake-value-7f3a\n")
	stops := func(out, what string) bool {
		return strings.HasSuffix(out, "  ✗ "+what+"\n    Fix: fix "+file+", or move it aside\n    Then run setup again.\n") && !strings.Contains(out, "7f3a")
	}
	if code, out, errOut := f.run(""); code != 1 || !stops(out, "EVA_MD_HOSTS in "+file+" must be claude, codex or all") || errOut != "" || len(f.installs) != 0 {
		t.Fatalf("code %d\n%s\n%s", code, out, errOut)
	}
	f.answersFile(t, "EVA_MD_WIKI_REMOTE=fake-value-7f3a\n")
	if code, out, errOut := f.run(""); code != 1 || !stops(out, "EVA_MD_WIKI_REMOTE in "+file+" isn't an address setup can use; give an SSH or HTTPS address, such as git@github.com:you/eva-wiki.git") {
		t.Fatalf("code %d\n%s\n%s", code, out, errOut)
	}
	f.answersFile(t, "EVA_MD_WIKI_DIR=~/Documents/wiki\nEVA_MD_WIKI_REMOTE=git@github.com:me/eva-wiki.git\n")
	if code, _, errOut := f.run(""); code != 1 || !strings.HasPrefix(errOut, "eva-md: EVA_MD_WIKI_DIR in "+file+": that folder is in Documents") {
		t.Fatalf("code %d stderr %q", code, errOut)
	}
	// A key that looks like setup's is named; any other, such as a secret
	// pasted by mistake whose = padding makes it look like a key, only by
	// its line.
	f.answersFile(t, "EVA_MD_TOKEN=fake-value-7f3a\nZmFrZVNlY3JldFRva2VuN2YzYTEyMzQ1Njc4OTBhYg==\nEVA_MD_WIKI_DIR=~/wiki\nEVA_MD_WIKI_REMOTE=git@github.com:me/eva-wiki.git\n")
	code, out, errOut := f.run("")
	f.wantRemote(t, code, out, errOut, "git@github.com:me/eva-wiki.git")
	if errOut != "eva-md: EVA_MD_TOKEN in "+file+" isn't a key setup knows; it is ignored\neva-md: line 2 of "+file+" has a key setup doesn't know; it is ignored\n" {
		t.Fatalf("stderr %q", errOut)
	}
	wantNotIn(t, "output", out+errOut, "ZmFr", "7f3a")
	if err := os.Remove(file); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(f.env.home, "elsewhere.env"), file); err != nil {
		t.Fatal(err)
	}
	f.inits = nil
	if code, out, _ := f.run("", "--dir", filepath.Join(f.env.home, "wiki2"), "--remote", "git@github.com:me/eva-wiki.git"); code != 1 || !stops(out, file+" is a link; setup reads its answers only from a regular file") || len(f.inits) != 0 {
		t.Fatalf("a link: code %d\n%s", code, out)
	}
	if _, err := os.Lstat(filepath.Join(f.env.home, "elsewhere.env")); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("written through the link: %v", err)
	}
	// --git-host refuses what EVA_MD_GIT_HOST_URL refuses, naming the flag,
	// never the value.
	for _, value := range []string{"git@github.com:me/fake-value-7f3a.git", "https://me:fake-value-7f3a@github.com"} {
		if code, _, errOut := f.run("", "--git-host", value); code != 2 || !strings.Contains(errOut, "setup --git-host ") || strings.Contains(errOut, "7f3a") {
			t.Fatalf("a bad --git-host: code %d stderr %q", code, errOut)
		}
	}
}

// On a Mac already set up, the file's folder and repo must name the wiki and
// its repo, as --dir and --remote must; a mismatch names the file and key.
func TestSetupChecksTheAnswersFileOnASetUpMac(t *testing.T) {
	f := newFakeSetup(t, false)
	f.realGit(t)
	f.bareRepo(t, "eva-wiki")
	c := f.setUp(t, "git@github.com:me/eva-wiki.git")
	file := f.answersFile(t, "EVA_MD_WIKI_DIR=~/elsewhere\n")
	if code, _, errOut := f.run(""); code != 1 || errOut != "eva-md: EVA_MD_WIKI_DIR in "+file+" names another folder than this Mac's wiki, "+c.WikiPath+"; setup won't move an existing wiki, so make it name that folder or leave it empty\n" {
		t.Fatalf("code %d stderr %q", code, errOut)
	}
	f.answersFile(t, "EVA_MD_WIKI_REMOTE=git@github.com:me/other-wiki.git\n")
	if code, _, errOut := f.run(""); code != 1 || errOut != "eva-md: EVA_MD_WIKI_REMOTE in "+file+" names another repo than the one this Mac's wiki is pushed to, github.com/me/eva-wiki; make it name that repo or leave it empty\n" {
		t.Fatalf("code %d stderr %q", code, errOut)
	}
	// The flags still say so in their own words.
	if code, _, errOut := f.run("", "--remote", "git@github.com:me/other-wiki.git"); code != 1 || !strings.Contains(errOut, "leave out --remote to keep it") {
		t.Fatalf("code %d stderr %q", code, errOut)
	}
	f.answersFile(t, "EVA_MD_WIKI_DIR=~/projects/wiki\nEVA_MD_WIKI_REMOTE=git@github.com:me/eva-wiki.git\nEVA_MD_HOSTS=claude\n")
	code, out, errOut := f.run("")
	if code != 0 || len(f.installs) != 1 || f.installs[0].Host != "claude" || !strings.Contains(out, "eva-md is set up here") {
		t.Fatalf("code %d installs %+v\n%s\n%s", code, f.installs, out, errOut)
	}
}

// A local remote, as a bare repo on an external volume, is never written to
// the answers file, so a setup while the volume is away, which a Mac set up
// with it passes, isn't stopped by the file. One a person wrote there that
// isn't there now is matched by its path on a Mac set up with it, and on a
// new Mac stops setup saying the path isn't there now.
func TestSetupKeepsALocalRemoteWhoseVolumeIsAway(t *testing.T) {
	f := newFakeSetup(t, false)
	f.realGit(t)
	volume := filepath.Join(f.env.home, "volume")
	bare := filepath.Join(volume, "eva-wiki.git")
	if err := os.MkdirAll(volume, 0o700); err != nil {
		t.Fatal(err)
	}
	gitIn(t, volume, "init", "--quiet", "--bare", bare)
	c := f.setUp(t, bare)
	file := f.env.dirs.AnswersFile()
	if code, out, errOut := f.run(""); code != 0 || !strings.Contains(out, "  Wrote "+file+" with this Mac's answers") {
		t.Fatalf("code %d\n%s\n%s", code, out, errOut)
	}
	b, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	wantIn(t, "the file", string(b), "\nEVA_MD_WIKI_DIR=~/projects/wiki\n", "\nEVA_MD_WIKI_REMOTE=\n", "\nEVA_MD_GIT_HOST_URL=\n")
	wantNotIn(t, "the file", string(b), bare)
	if err := os.Rename(volume, volume+"-away"); err != nil {
		t.Fatal(err)
	}
	setUpHere := "eva-md is set up here: the wiki is in " + c.WikiPath + ", pushed to " + c.ExpectedRemote + ".\n"
	if code, out, errOut := f.run(""); code != 0 || !strings.Contains(out, setUpHere) {
		t.Fatalf("the file setup wrote, the volume away: code %d\n%s\n%s", code, out, errOut)
	}
	// The path a person wrote there.
	f.answersFile(t, "EVA_MD_WIKI_REMOTE="+bare+"\n")
	if code, out, errOut := f.run(""); code != 0 || !strings.Contains(out, setUpHere) {
		t.Fatalf("the path in the file, the volume away: code %d\n%s\n%s", code, out, errOut)
	}
	f.answersFile(t, "EVA_MD_WIKI_REMOTE="+filepath.Join(volume, "other-wiki.git")+"\n")
	if code, _, errOut := f.run(""); code != 1 || !strings.Contains(errOut, "EVA_MD_WIKI_REMOTE in "+file+" names another repo than the one this Mac's wiki is pushed to") {
		t.Fatalf("another path: code %d stderr %q", code, errOut)
	}

	// On a new Mac, the message says the path isn't there now.
	f = newFakeSetup(t, false)
	file = f.answersFile(t, "EVA_MD_WIKI_DIR=~/wiki\nEVA_MD_WIKI_REMOTE="+bare+"\n")
	code, out, errOut := f.run("")
	if want := "  ✗ EVA_MD_WIKI_REMOTE in " + file + " names " + bare + ", which isn't there now; mount or restore it, or give an SSH or HTTPS address\n    Fix: fix " + file + ", or move it aside\n"; code != 1 || !strings.Contains(out, want) || len(f.inits) != 0 {
		t.Fatalf("code %d, want %q\n%s\n%s", code, want, out, errOut)
	}
}

// EVA_MD_CODEX_IGNORE_USER_CONFIG sets codex_ignore_user_config at the end
// of setup, keeping the config's other settings; empty, it leaves it alone.
func TestSetupSetsTheCodexSwitchFromTheAnswersFile(t *testing.T) {
	f := newFakeSetup(t, false)
	f.realGit(t)
	f.bareRepo(t, "eva-wiki")
	c := f.setUp(t, "git@github.com:me/eva-wiki.git")
	c.Exclude = []string{"~/private"}
	if err := config.Save(f.env.dirs.ConfigFile(), c); err != nil {
		t.Fatal(err)
	}
	f.answersFile(t, "EVA_MD_CODEX_IGNORE_USER_CONFIG=true\n")
	code, out, errOut := f.run("")
	if code != 0 {
		t.Fatalf("code %d\n%s\n%s", code, out, errOut)
	}
	wantIn(t, "stdout", out, "  Set codex_ignore_user_config = true in "+f.env.dirs.ConfigFile()+"\n\nChecking health\n")
	after, err := config.Load(f.env.dirs.ConfigFile())
	if err != nil || !after.CodexIgnoreUserConfig || len(after.Exclude) != 1 || after.Instance != c.Instance {
		t.Fatalf("config %+v %v", after, err)
	}
	// Already set, nothing is written or said: the config keeps its bytes
	// and its file, which any save would replace.
	unchanged := func(run func()) {
		t.Helper()
		before, err := os.Stat(f.env.dirs.ConfigFile())
		if err != nil {
			t.Fatal(err)
		}
		bytes, _ := os.ReadFile(f.env.dirs.ConfigFile())
		run()
		after, err := os.Stat(f.env.dirs.ConfigFile())
		if err != nil {
			t.Fatal(err)
		}
		if b, _ := os.ReadFile(f.env.dirs.ConfigFile()); !os.SameFile(before, after) || string(b) != string(bytes) {
			t.Fatalf("the config was written again")
		}
	}
	unchanged(func() {
		if code, out, _ := f.run(""); code != 0 || strings.Contains(out, "Set codex_ignore_user_config") {
			t.Fatalf("code %d\n%s", code, out)
		}
	})
	// Empty leaves it as it is.
	f.answersFile(t, config.AnswersTemplate)
	unchanged(func() {
		if code, out, _ := f.run(""); code != 0 || strings.Contains(out, "codex_ignore_user_config") {
			t.Fatalf("code %d\n%s", code, out)
		}
	})
	if after, err := config.Load(f.env.dirs.ConfigFile()); err != nil || !after.CodexIgnoreUserConfig {
		t.Fatalf("config %+v %v", after, err)
	}
	f.answersFile(t, "EVA_MD_CODEX_IGNORE_USER_CONFIG=false\n")
	if code, out, _ := f.run(""); code != 0 || !strings.Contains(out, "  Set codex_ignore_user_config = false in ") {
		t.Fatalf("code %d\n%s", code, out)
	}
	if after, err := config.Load(f.env.dirs.ConfigFile()); err != nil || after.CodexIgnoreUserConfig {
		t.Fatalf("config %+v %v", after, err)
	}
}

// With the folder from the file holding an eva wiki pushed to another repo
// than the remote names, setup stops naming the remote's own source, as it
// is, never after the folder's key.
func TestSetupNamesTheRemotesSourceWhenTheFolderIsAnotherWiki(t *testing.T) {
	f := newFakeSetup(t, true)
	f.realGit(t)
	f.bareRepo(t, "eva-wiki")
	evaWiki(t, filepath.Join(f.env.home, "projects", "wiki"), "git@code.example.com:me/eva-wiki.git", "this-mac")
	file := f.answersFile(t, "EVA_MD_WIKI_DIR=~/projects/wiki\nEVA_MD_WIKI_REMOTE=git@code.example.com:me/other-wiki.git\n")
	code, _, errOut := f.run("y\n")
	if want := "eva-md: EVA_MD_WIKI_REMOTE in " + file + " names another repo; leave it empty to use this wiki, or choose another folder\n"; code != 1 || errOut != want || len(f.inits) != 0 {
		t.Fatalf("code %d stderr %q, want %q", code, errOut, want)
	}
	code, _, errOut = f.run("y\n", "--remote", "git@code.example.com:me/other-wiki.git")
	if want := "eva-md: --remote names another repo; leave it out to use this wiki, or choose another folder\n"; code != 1 || errOut != want || len(f.inits) != 0 {
		t.Fatalf("code %d stderr %q, want %q", code, errOut, want)
	}
}

// The template is written once setup's first checks pass, and said in the
// next line, so a check that stops setup after it never leaves it unsaid;
// one that stops setup before it writes nothing.
func TestSetupSaysItWroteTheTemplateWhenItStops(t *testing.T) {
	f := newFakeSetup(t, true)
	file := f.env.dirs.AnswersFile()
	code, out, errOut := f.run("~/projects/wiki\n", "--git-host", "gitlab.com")
	if code != 1 || len(f.inits) != 0 {
		t.Fatalf("code %d inits %d\n%s\n%s", code, len(f.inits), out, errOut)
	}
	wantIn(t, "stdout", out, "  ✓ Wrote "+file+", a template for setup's answers\n  ✗ glab isn't installed\n")
	if b, err := os.ReadFile(file); err != nil || string(b) != config.AnswersTemplate {
		t.Fatalf("the template: %q, %v", b, err)
	}

	f = newFakeSetup(t, true)
	f.env.lookPath = func(name string) (string, error) {
		if name == "git" {
			return "/usr/bin/git", nil
		}
		return "", errors.New("not found")
	}
	if code, out, _ := f.run("~/projects/wiki\n"); code != 1 || strings.Contains(out, "Wrote") {
		t.Fatalf("code %d\n%s", code, out)
	}
	if _, err := os.Lstat(f.env.dirs.AnswersFile()); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("the template was written: %v", err)
	}
}

// After a successful setup on a Mac that has no answers file, as one set up
// before there was one, setup writes it filled in with what this Mac now
// uses, mode 0600, and says so; it never writes over one that is there. A
// --host flag is for its run only: the file says all, and the next plain
// setup installs for all.
func TestSetupWritesTheAnswersFileOnASetUpMac(t *testing.T) {
	f := newFakeSetup(t, false)
	f.realGit(t)
	f.bareRepo(t, "eva-wiki")
	c := f.setUp(t, "git@code.example.com:me/eva-wiki.git")
	c.CodexIgnoreUserConfig = true
	if err := config.Save(f.env.dirs.ConfigFile(), c); err != nil {
		t.Fatal(err)
	}
	file := f.env.dirs.AnswersFile()
	code, out, errOut := f.run("", "--host", "codex")
	if code != 0 {
		t.Fatalf("code %d\n%s\n%s", code, out, errOut)
	}
	wantIn(t, "stdout", out, "  Wrote "+file+" with this Mac's answers, for the next setup\n")
	on := true
	want, err := config.FilledAnswers(config.Answers{WikiDir: "~/projects/wiki", WikiRemote: "git@code.example.com:me/eva-wiki.git", GitHost: "code.example.com", Hosts: "all", CodexIgnoreUserConfig: &on})
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(file)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("the file: %v, %v", info, err)
	}
	if b, _ := os.ReadFile(file); string(b) != want {
		t.Fatalf("the file is\n%s\nwant\n%s", b, want)
	}
	wantIn(t, "the file", want, "\nEVA_MD_GIT_HOST_URL=https://code.example.com\n", "\nEVA_MD_HOSTS=all\n")
	if code, out, errOut := f.run(""); code != 0 || f.installs[len(f.installs)-1].Host != "all" {
		t.Fatalf("code %d installs %+v\n%s\n%s", code, f.installs, out, errOut)
	}
	// Run again, setup reads it, and writes nothing over it.
	if err := os.WriteFile(file, []byte("EVA_MD_HOSTS=claude\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	code, out, errOut = f.run("")
	if code != 0 || strings.Contains(out, "Wrote") || f.installs[len(f.installs)-1].Host != "claude" {
		t.Fatalf("code %d\n%s\n%s", code, out, errOut)
	}
	if b, _ := os.ReadFile(file); string(b) != "EVA_MD_HOSTS=claude\n" {
		t.Fatalf("the file was replaced: %q", b)
	}
}
