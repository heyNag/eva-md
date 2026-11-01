package coderef

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// partialClone clones a repo, whose main holds two commits changing
// internal/orders/a.go and whose side branch a third, into a partial clone
// of main alone, with filter blob:none or tree:0, from a file:// origin that
// would serve any object the clone lacks. It returns the clone, main's first
// commit, which the clone has, and side's, which it lacks.
func partialClone(t *testing.T, home, filter string) (clone, first, side string) {
	t.Helper()
	origin := newRepo(t, filepath.Join(home, "origin"), "")
	run(t, origin, "config", "uploadpack.allowFilter", "true")
	run(t, origin, "config", "uploadpack.allowAnySHA1InWant", "true")
	first = change(t, origin, "one", "internal/orders/a.go", "v1")
	change(t, origin, "two", "internal/orders/a.go", "v2")
	run(t, origin, "checkout", "-q", "-b", "side")
	side = change(t, origin, "side", "internal/orders/a.go", "side")
	run(t, origin, "checkout", "-q", "main")
	clone = filepath.Join(home, "clone")
	if out, err := exec.Command("git", "clone", "-q", "--filter="+filter, "--single-branch", "--branch", "main", "file://"+origin, clone).CombinedOutput(); err != nil {
		t.Fatalf("clone: %v %s", err, out)
	}
	return clone, first, side
}

// objects is what git counts of a clone's objects, read without lazy
// fetches, so the count itself fetches nothing.
func objects(t *testing.T, clone string) string {
	t.Helper()
	cmd := exec.Command("git", "-C", clone, "count-objects", "-v")
	cmd.Env = append(os.Environ(), "GIT_NO_LAZY_FETCH=1")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("count-objects: %v %s", err, out)
	}
	return string(out)
}

// A measurement in a partial clone never fetches what the clone lacks from
// its remote, with a git that honours GIT_NO_LAZY_FETCH and with one that,
// as git before 2.44 does, ignores it: the clone gains no object, and a
// commit it lacks is not in the clone, while a count that needs trees it
// lacks fails. An inherited GIT_NO_LAZY_FETCH=0 changes nothing.
func TestNoLazyFetchInAPartialClone(t *testing.T) {
	real, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	for _, filter := range []string{"blob:none", "tree:0"} {
		for _, old := range []bool{false, true} {
			t.Run(filter+" old="+strconv.FormatBool(old), func(t *testing.T) {
				home := isolate(t)
				t.Setenv("GIT_NO_LAZY_FETCH", "0")
				clone, first, side := partialClone(t, home, filter)
				if !Partial(clone) {
					t.Fatal("a partial clone is not seen as one")
				}
				if old {
					fakeGit(t, "unset GIT_NO_LAZY_FETCH\nexec '"+real+"' \"$@\"")
				}
				before := objects(t, clone)
				ctx := context.Background()
				if has, err := HasCommit(ctx, clone, side); has || err != nil {
					t.Errorf("HasCommit of the commit the clone lacks = %v, %v", has, err)
				}
				if _, err := Commits(ctx, clone, side, []string{"internal/orders"}); !errors.Is(err, ErrNoCommit) {
					t.Errorf("Commits since the commit the clone lacks: %v", err)
				}
				_, err := Commits(ctx, clone, first, []string{"internal/orders"})
				if filter == "tree:0" && err == nil {
					t.Error("a count that needs trees the clone lacks counted")
				}
				Missing(ctx, clone, first, []string{"internal/orders"})
				Last(ctx, clone, first, []string{"internal/orders"})
				if after := objects(t, clone); after != before {
					t.Fatalf("git fetched into the partial clone:\nbefore %s\nafter %s", before, after)
				}
			})
		}
	}
}

// Partial reads a clone's config: a clone, or a worktree of one, made with
// a filter is partial, one made without is not, even when its remote's URL
// and its branches' names hold the words, one with an includeIf may be, and
// so may a folder without a git folder. The config's keys and sections count
// by their names, and a line it can't read counts too.
func TestPartial(t *testing.T) {
	home := isolate(t)
	plain := newRepo(t, filepath.Join(home, "plain"), "git@github.com:acme/include-promisor-partialclone.git")
	change(t, plain, "one", "a.go", "a")
	run(t, plain, "config", "branch.fix-includes.remote", "origin")
	run(t, plain, "config", "branch.promisor-partialclone.remote", "origin")
	run(t, plain, "worktree", "add", "-q", filepath.Join(home, "plain-tree"))
	included := newRepo(t, filepath.Join(home, "included"), "")
	run(t, included, "config", "includeIf.gitdir:/elsewhere/.path", "/elsewhere/config")
	clone, _, _ := partialClone(t, home, "blob:none")
	run(t, clone, "worktree", "add", "-q", filepath.Join(home, "clone-tree"))
	for dir, want := range map[string]bool{plain: false, filepath.Join(home, "plain-tree"): false, included: true, clone: true, filepath.Join(home, "clone-tree"): true, filepath.Join(home, "none"): true} {
		if got := Partial(dir); got != want {
			t.Errorf("Partial(%s) = %v", filepath.Base(dir), got)
		}
	}
	for config, want := range map[string]bool{
		"[remote \"origin\"]\n\tPromisor\n":                             true,
		"[remote \"origin\"] partialCloneFilter = blob:none\n":          true,
		"[Extensions]\n\tpartialClone = origin\n":                       true,
		"[core]\n\tpartialclone = origin # promisor\n":                  false,
		"[Include]\n\tpath = other\n":                                   true,
		"\ufeff[include]\n\tpath = extra\n":                             true,
		"\ufeff[extensions]\n\tpartialclone = origin\n":                 true,
		"[remote \"a]b\"]\n\turl = x\n":                                 true,
		"[core]\n\tsshCommand = ssh \\\n\t-o promisor=yes\n":            true,
		"; promisor\n# [include]\n[branch \"include\"]\n\tremote = x\n": false,
	} {
		if got := partialConfig(config); got != want {
			t.Errorf("partialConfig(%q) = %v", config, got)
		}
	}
}

// A git call killed at its deadline takes the processes it started with it:
// here a child that holds git's output open. The test ends the call's time
// once git has started the child, so the kill always meets it.
func TestTimeoutKillsTheGroup(t *testing.T) {
	isolate(t)
	fifo := filepath.Join(t.TempDir(), "pid")
	if err := syscall.Mkfifo(fifo, 0o600); err != nil {
		t.Fatal(err)
	}
	fakeGit(t, "sleep 600 &\necho $! > '"+fifo+"'\nwait")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := Commits(ctx, t.TempDir(), "4e2bf4c", nil)
		done <- err
	}()
	pid := make(chan int, 1)
	go func() {
		data, _ := os.ReadFile(fifo)
		n, _ := strconv.Atoi(strings.TrimSpace(string(data)))
		pid <- n
	}()
	var child int
	select {
	case child = <-pid:
	case <-time.After(hangGuard):
		t.Fatalf("the fake git started no child within %v", hangGuard)
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, ErrTimeout) {
			t.Fatalf("a call out of time: %v", err)
		}
	case <-time.After(hangGuard):
		t.Fatalf("a git call out of time hung: it had not returned after %v", hangGuard)
	}
	// The child closed git's output, so it was killed: ps lists it no
	// longer once reaped, as a zombie before, or, as the system closes a
	// process's files before it makes it a zombie, as exiting (E). A child
	// the kill missed would hold the output open and sleep.
	out, _ := exec.Command("ps", "-o", "stat=", "-p", strconv.Itoa(child)).Output()
	if state := strings.TrimSpace(string(out)); state != "" && !strings.HasPrefix(state, "Z") && !strings.Contains(state, "E") {
		syscall.Kill(child, syscall.SIGKILL)
		t.Fatalf("the child git started outlived the call (state %s)", state)
	}
}

// With log.showSignature on in the user's git config, Last still gives the
// newest change's day and subject: no signature is checked, and only a day
// is taken for one.
func TestLastWithShowSignature(t *testing.T) {
	home := isolate(t)
	t.Setenv("GIT_COMMITTER_DATE", "2026-10-29T10:00:00+00:00")
	key := filepath.Join(home, "key")
	if out, err := exec.Command("ssh-keygen", "-q", "-t", "ed25519", "-N", "", "-f", key, "-C", "fixture").CombinedOutput(); err != nil {
		t.Fatalf("ssh-keygen: %v %s", err, out)
	}
	pub, err := os.ReadFile(key + ".pub")
	if err != nil {
		t.Fatal(err)
	}
	allowed := filepath.Join(home, "allowed")
	if err := os.WriteFile(allowed, []byte("fixture@example.invalid "+string(pub)), 0o600); err != nil {
		t.Fatal(err)
	}
	config := "[user]\nname = Eva Fixture\nemail = fixture@example.invalid\nsigningkey = " + key + ".pub\n[gpg]\nformat = ssh\n[gpg \"ssh\"]\nallowedSignersFile = " + allowed + "\n[commit]\ngpgsign = true\n[log]\nshowSignature = true\n[init]\ndefaultBranch = main\n"
	if err := os.WriteFile(filepath.Join(home, "gitconfig"), []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	dir := newRepo(t, filepath.Join(home, "orders"), "")
	first := change(t, dir, "first", "a.go", "a")
	change(t, dir, "Second change", "a.go", "b")
	if day, subject, err := Last(context.Background(), dir, first, []string{"a.go"}); day != "2026-10-29" || subject != "Second change" || err != nil {
		t.Fatalf("Last = %q %q %v", day, subject, err)
	}
	fakeGit(t, "printf 'Good signature\\n\\t\\n'")
	if day, subject, err := Last(context.Background(), dir, first, []string{"a.go"}); day != "" || subject != "" || err != nil {
		t.Fatalf("Last of a line without a day = %q %q %v", day, subject, err)
	}
}
