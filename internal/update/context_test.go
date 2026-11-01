package update

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// With log.showSignature on in the user's git config, commitsOf still gives
// the branch's subjects and its newest commit, and checks no signature: the
// signing program, which logs each run, never runs. A git that prints a
// line before the first commit's still gives that commit as the head.
func TestCommitsOfWithShowSignature(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(home, "gitconfig"))
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("SSH_AUTH_SOCK", "")
	key := filepath.Join(home, "key")
	if out, err := exec.Command("ssh-keygen", "-q", "-t", "ed25519", "-N", "", "-f", key, "-C", "fixture").CombinedOutput(); err != nil {
		t.Fatalf("ssh-keygen: %v %s", err, out)
	}
	pub, err := os.ReadFile(key + ".pub")
	if err != nil {
		t.Fatal(err)
	}
	real, err := exec.LookPath("ssh-keygen")
	if err != nil {
		t.Fatal(err)
	}
	program, ran := filepath.Join(home, "sign"), filepath.Join(home, "sign.log")
	if err := os.WriteFile(program, []byte("#!/bin/sh\necho run >> '"+ran+"'\nexec '"+real+"' \"$@\"\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	allowed := filepath.Join(home, "allowed")
	if err := os.WriteFile(allowed, []byte("fixture@example.invalid "+string(pub)), 0o600); err != nil {
		t.Fatal(err)
	}
	config := "[user]\nname = Fixture\nemail = fixture@example.invalid\nsigningkey = " + key + ".pub\n[gpg]\nformat = ssh\n[gpg \"ssh\"]\nprogram = " + program + "\nallowedSignersFile = " + allowed + "\n[commit]\ngpgsign = true\n[log]\nshowSignature = true\n"
	if err := os.WriteFile(filepath.Join(home, "gitconfig"), []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	repo := filepath.Join(home, "repo")
	git := func(args ...string) string {
		t.Helper()
		out, err := exec.Command("git", append([]string{"-C", repo}, args...)...).CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	if err := os.Mkdir(repo, 0o700); err != nil {
		t.Fatal(err)
	}
	git("init", "-q", "-b", "main")
	git("commit", "-q", "--allow-empty", "-m", "first")
	git("commit", "-q", "--allow-empty", "-m", "second")
	if out := git("log", "-1", "--format=%s"); !strings.Contains(out, "Good") {
		t.Fatalf("the fixture shows no signature: %q", out)
	}
	if err := os.Remove(ran); err != nil {
		t.Fatalf("the fixture ran no signing program: %v", err)
	}
	subjects, head := commitsOf(context.Background(), repo, "main")
	want := git("rev-parse", "HEAD")
	if !reflect.DeepEqual(subjects, []string{"second", "first"}) || head != want {
		t.Fatalf("commitsOf = %q %q, want head %s", subjects, head, want)
	}
	if _, err := os.Stat(ran); err == nil {
		t.Fatal("commitsOf checked a signature")
	}
	fakeGit(t, "printf 'Good \"git\" signature\\n"+want+"\\tsecond\\n'")
	if subjects, head := commitsOf(context.Background(), repo, "main"); !reflect.DeepEqual(subjects, []string{"second"}) || head != want {
		t.Fatalf("commitsOf after a line with no commit = %q %q", subjects, head)
	}
}

// commitsOf's git fetches nothing: in any clone it runs with
// GIT_NO_LAZY_FETCH=1, whatever it inherits, and never with
// --no-lazy-fetch, which a git before 2.44 refuses, since it reads only
// commits, which a partial clone always has.
func TestCommitsOfFetchesNothing(t *testing.T) {
	isolate(t)
	t.Setenv("GIT_NO_LAZY_FETCH", "0")
	logged := filepath.Join(t.TempDir(), "git.log")
	fakeGit(t, "echo \"$* GIT_NO_LAZY_FETCH=$GIT_NO_LAZY_FETCH\" >> '"+logged+"'")
	for _, config := range []string{"[remote \"origin\"]\n\tpromisor = true\n", "[core]\n\tbare = false\n"} {
		clone := t.TempDir()
		if err := os.Mkdir(filepath.Join(clone, ".git"), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(clone, ".git", "config"), []byte(config), 0o600); err != nil {
			t.Fatal(err)
		}
		commitsOf(context.Background(), clone, "main")
	}
	data, err := os.ReadFile(logged)
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if err != nil || len(lines) != 2 || strings.Contains(string(data), "--no-lazy-fetch") || !strings.HasSuffix(lines[0], " GIT_NO_LAZY_FETCH=1") || !strings.HasSuffix(lines[1], " GIT_NO_LAZY_FETCH=1") {
		t.Fatalf("git ran: %v\n%s", err, data)
	}
}

// fakeGit puts a git first on PATH that runs script.
func fakeGit(t *testing.T, script string) {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "git"), []byte("#!/bin/sh\n"+script+"\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}
