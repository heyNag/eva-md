package resume

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/heyNag/eva-md/internal/identity"
)

// gitVars is the part of an environment that decides how git runs: its own
// variables and LC_ALL, sorted.
func gitVars(env []string) []string {
	var out []string
	for _, kv := range env {
		if strings.HasPrefix(kv, "GIT_") || strings.HasPrefix(kv, "LC_ALL=") {
			out = append(out, kv)
		}
	}
	slices.Sort(out)
	return out
}

// Live git runs with the environment of every read-only git eva-md runs
// (identity.GitEnv), which leaves out the same variables as the wiki's
// runner: a git trace, another repository, config carried in the
// environment and the locale's language never reach it, and git's own
// config files and its signing and sign-in settings do.
func TestLiveGitRunsWithTheReadEnvironment(t *testing.T) {
	fixture(t)
	for k, v := range map[string]string{
		"GIT_DIR": "/elsewhere/.git", "GIT_TRACE": "1", "GIT_CONFIG_COUNT": "1", "GIT_CONFIG_KEY_0": "core.hooksPath", "GIT_CONFIG_VALUE_0": "/elsewhere/hooks",
		"GIT_LITERAL_PATHSPECS": "1", "LC_ALL": "de_DE.UTF-8", "GIT_CONFIG_SYSTEM": "/etc/gitconfig-test", "GIT_SSH_COMMAND": "ssh -o BatchMode=yes",
	} {
		t.Setenv(k, v)
	}
	seen := filepath.Join(t.TempDir(), "env")
	fakeGit(t, "env > '"+seen+"'")
	if _, err := git(context.Background(), t.TempDir(), "status"); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(seen)
	if err != nil {
		t.Fatal(err)
	}
	got, want := gitVars(strings.Split(strings.TrimSpace(string(data)), "\n")), gitVars(identity.GitEnv())
	if !slices.Equal(got, want) {
		t.Fatalf("live git's environment differs from the read-only calls' in %q", differingNames(got, want))
	}
}

// differingNames names the variables, never their values, that one sorted
// list holds and the other doesn't, as the environment can hold secrets.
func differingNames(a, b []string) []string {
	var names []string
	for _, kv := range append(slices.Clone(a), b...) {
		if !slices.Contains(a, kv) || !slices.Contains(b, kv) {
			name, _, _ := strings.Cut(kv, "=")
			names = append(names, name)
		}
	}
	slices.Sort(names)
	return slices.Compact(names)
}

// The brief's stash count is git's own, so the 64 KiB a live git call keeps
// never cuts it: none is said as such, real stashes are counted, also
// beside a file named refs/stash, and so are 2,000, whose list, at 41 bytes
// a stash, would pass 64 KiB at the 1,599th.
func TestBriefCountsEveryStash(t *testing.T) {
	fixture(t)
	repo, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	run := func(args ...string) string {
		t.Helper()
		out, err := exec.Command("git", append([]string{"-C", repo, "-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid", "-c", "commit.gpgsign=false"}, args...)...).CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	stashes := func() string {
		t.Helper()
		for _, line := range strings.Split(gitSummary(context.Background(), identity.Project{Root: repo}, "").text, "\n") {
			if strings.HasPrefix(line, "stashes:") {
				return line
			}
		}
		t.Fatal("the brief has no stash line")
		return ""
	}
	run("init", "--quiet", "-b", "main")
	if err := os.WriteFile(filepath.Join(repo, "notes.txt"), []byte("first\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	run("add", "notes.txt")
	run("commit", "--quiet", "-m", "first")
	if got := stashes(); got != "stashes: 0 or unavailable" {
		t.Fatalf("no stash: %q", got)
	}
	for _, text := range []string{"second\n", "third\n"} {
		if err := os.WriteFile(filepath.Join(repo, "notes.txt"), []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
		run("stash", "--quiet")
	}
	if got := stashes(); got != "stashes: 2" {
		t.Fatalf("two stashes: %q", got)
	}
	if err := os.MkdirAll(filepath.Join(repo, "refs"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "refs", "stash"), []byte("a file\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := stashes(); got != "stashes: 2" {
		t.Fatalf("two stashes beside a file named refs/stash: %q", got)
	}
	head := run("rev-parse", "HEAD")
	var log strings.Builder
	for i := range 2000 {
		fmt.Fprintf(&log, "%s %s Fixture <fixture@example.invalid> %d +0000\tWIP on main: %d\n", strings.Repeat("0", 40), head, 1700000000+i, i)
	}
	if err := os.WriteFile(filepath.Join(repo, ".git", "logs", "refs", "stash"), []byte(log.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	if listed := strings.Count(run("stash", "list"), "\n") + 1; listed != 2000 {
		t.Fatalf("the fixture holds %d stashes", listed)
	}
	if got := stashes(); got != "stashes: 2000" {
		t.Fatalf("2,000 stashes: %q", got)
	}
}

// Live git keeps the first 64 KiB of what a call prints, however much git
// prints: os/exec copies it through the buffer's Write, never a ReadFrom
// that would keep all of it.
func TestLiveGitOutputIsBounded(t *testing.T) {
	fixture(t)
	fakeGit(t, "exec /usr/bin/head -c 131072 /dev/zero")
	out, err := git(context.Background(), t.TempDir(), "status")
	if err != nil || len(out) != gitOutputLimit {
		t.Fatalf("live git kept %d bytes, %v; want %d", len(out), err, gitOutputLimit)
	}
}
