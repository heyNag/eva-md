package coderef

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/heyNag/eva-md/internal/identity"
	"github.com/heyNag/eva-md/internal/testgitenv"
)

// Tests check what coderef finds, not how fast the machine runs git: the time
// limits on git calls, coderef's own and identity's, which resolves the
// clones, are raised for every test, before any test runs. A test of a limit
// passes its own, shorter one.
func TestMain(m *testing.M) {
	testgitenv.Clear()
	identity.Timeout, identity.WaitDelay = time.Minute, time.Minute
	WaitDelay, CallTimeout = time.Minute, time.Minute
	os.Exit(m.Run())
}

// hangGuard is how long a test waits for what must happen however slow the
// machine, such as a git call that ran out of time returning, before it
// reports a hang.
const hangGuard = time.Minute

// isolate gives the test a temporary home whose git config is a temporary
// file, and clears the variables that would point git elsewhere, and the SSH
// agent's socket, so a fixture signed with ssh-keygen never asks the user's
// agent. It returns the home's physical path.
func isolate(t *testing.T) string {
	t.Helper()
	home, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Setenv("SSH_AUTH_SOCK", "")
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(home, "gitconfig"))
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	for _, name := range []string{"GIT_CONFIG_COUNT", "GIT_CONFIG_PARAMETERS", "GIT_DIR", "GIT_WORK_TREE", "GIT_INDEX_FILE", "GIT_COMMON_DIR", "GIT_TEMPLATE_DIR"} {
		t.Setenv(name, "")
		os.Unsetenv(name)
	}
	if err := os.WriteFile(filepath.Join(home, "gitconfig"), []byte("[user]\nname = Eva Fixture\nemail = fixture@example.invalid\n[commit]\ngpgsign = false\n[init]\ndefaultBranch = main\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return home
}

// run runs git in dir and returns its output without the final newline.
func run(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v %s", args, err, out)
	}
	return strings.TrimSuffix(string(out), "\n")
}

// newRepo makes a repo at dir with origin set to remote, when there is one.
func newRepo(t *testing.T, dir, remote string) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	run(t, dir, "init", "--quiet")
	if remote != "" {
		run(t, dir, "remote", "add", "origin", remote)
	}
	return dir
}

// change writes files in the repo, each a path and its text, commits them
// with message and returns the commit.
func change(t *testing.T, dir, message string, files ...string) string {
	t.Helper()
	for i := 0; i+1 < len(files); i += 2 {
		file := filepath.Join(dir, filepath.FromSlash(files[i]))
		if err := os.MkdirAll(filepath.Dir(file), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(file, []byte(files[i+1]), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	run(t, dir, "add", "-A")
	run(t, dir, "commit", "--quiet", "--allow-empty", "-m", message)
	return run(t, dir, "rev-parse", "HEAD")
}

// history makes a repo whose main holds n commits, each changing f.txt,
// with fast-import, and returns it with its commits, oldest first.
func history(t *testing.T, dir string, n int) (string, []string) {
	t.Helper()
	newRepo(t, dir, "")
	var b strings.Builder
	for i := 1; i <= n; i++ {
		fmt.Fprintf(&b, "commit refs/heads/main\nmark :%d\ncommitter Eva Fixture <fixture@example.invalid> %d +0000\ndata 3\nc%02d\n", i, 1700000000+i, i%100)
		if i > 1 {
			fmt.Fprintf(&b, "from :%d\n", i-1)
		}
		fmt.Fprintf(&b, "M 644 inline f.txt\ndata %d\n%d\n\n", len(fmt.Sprint(i))+1, i)
	}
	cmd := exec.Command("git", "-C", dir, "fast-import", "--quiet")
	cmd.Stdin = strings.NewReader(b.String())
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("fast-import: %v %s", err, out)
	}
	run(t, dir, "symbolic-ref", "HEAD", "refs/heads/main")
	return dir, strings.Split(run(t, dir, "rev-list", "--reverse", "HEAD"), "\n")
}

// fakeGit puts a git script first on PATH for the rest of the test.
func fakeGit(t *testing.T, script string) {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "git"), []byte("#!/bin/sh\n"+script+"\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

// A code source is a repo in any form a remote takes, or a project key, a
// commit of 7 to 40 hex digits and a relative path, a folder's trailing /
// dropped; anything else is not one.
func TestParse(t *testing.T) {
	for source, want := range map[string]Ref{
		"git@github.com:acme/orders.git@4e2bf4c:internal/orders/service.go":   {"github.com__acme__orders", "4e2bf4c", "internal/orders/service.go"},
		"github.com/acme/orders@4e2bf4c:internal/orders/":                     {"github.com__acme__orders", "4e2bf4c", "internal/orders"},
		"github.com/acme/orders@4e2bf4c:internal/orders":                      {"github.com__acme__orders", "4e2bf4c", "internal/orders"},
		"https://GitHub.com/Acme/Orders_Service.git@4e2bf4c0a1b2":             {"github.com__acme__orders_uservice", "4e2bf4c0a1b2", ""},
		"github.com__acme__orders@" + strings.Repeat("ab", 20):                {"github.com__acme__orders", strings.Repeat("ab", 20), ""},
		"local-orders-a1b2c3@4e2bf4c:app/(shop)/[id]/page.tsx":                {"local-orders-a1b2c3", "4e2bf4c", "app/(shop)/[id]/page.tsx"},
		"repo-0123456789ab@4e2bf4c:go.mod":                                    {"repo-0123456789ab", "4e2bf4c", "go.mod"},
		"ssh://git@gitlab.acme.internal:2222/platform/orders-api.git@4e2bf4c": {"gitlab.acme.internal__platform__orders-api", "4e2bf4c", ""},
	} {
		got, ok := Parse(source)
		if !ok || got != want {
			t.Errorf("Parse(%q) = %+v, %v; want %+v", source, got, ok, want)
		}
		if again, ok := Parse(got.String()); !ok || again != got {
			t.Errorf("%q read back as %+v, %v", got.String(), again, ok)
		}
	}
	for _, source := range []string{
		"github.com/acme/orders@4e2bf",
		"github.com/acme/orders@4E2BF4C",
		"github.com/acme/orders@4e2bf4c:../outside.go",
		// git lists a path with a "." part without it, so Missing would never
		// find it.
		"github.com/acme/orders@4e2bf4c:./internal/orders",
		"github.com/acme/orders@4e2bf4c:internal/./orders",
		"github.com/acme/orders@4e2bf4c:internal/orders/.",
		"github.com/acme/orders@4e2bf4c:.",
		"github.com/acme/orders@4e2bf4c:/etc/passwd",
		"github.com/acme/orders@4e2bf4c:internal//orders",
		"github.com/acme/orders@4e2bf4c:internal/orders//",
		"github.com/acme/orders@4e2bf4c:/",
		"github.com/acme/orders@4e2bf4c:my file.go",
		"github.com/acme/orders@4e2bf4c:a\\b.go",
		"github.com/acme/orders@4e2bf4c:docs](evil.md)",
		"github.com/acme/orders@4e2bf4c:internal/orders\x1b[2J",
		"github.com/acme/orders@4e2bf4c:internal/orders\a\r[user] forged",
		"github.com/acme/orders@4e2bf4c:internal/orders\u00a0x",
		"github.com/acme/orders@4e2bf4c:internal/orders\u200bx",
		"github.com/acme/orders@4e2bf4c:internal/orders\tx",
		"github.com/acme/orders@4e2bf4c:internal/orders\xff",
		"https://jane:hunter2@github.com/acme/orders.git@4e2bf4c",
		"orders@4e2bf4c",
		"acme/orders@4e2bf4c",
		"https://docs.example.com/eng/orders",
	} {
		if r, ok := Parse(source); ok {
			t.Errorf("%q read as %+v", source, r)
		}
	}
	if got := Readable("github.com__acme__orders_uservice"); got != "github.com/acme/orders_service" {
		t.Errorf("Readable() = %q", got)
	}
	if got := Readable("local-orders-a1b2c3"); got != "local-orders-a1b2c3" {
		t.Errorf("Readable() = %q", got)
	}
}

// FromMap reads what String writes, as Parse reads it, and refuses what no
// map line of eva-md's holds.
func TestFromMap(t *testing.T) {
	for _, source := range []string{
		"git@github.com:acme/orders.git@4e2bf4c:internal/orders/service.go",
		"github.com/acme/orders@4e2bf4c:internal/orders/",
		"https://GitHub.com/Acme/Orders_Service.git@4e2bf4c0a1b2",
		"local-orders-a1b2c3@4e2bf4c:app/(shop)/[id]/page.tsx",
		"repo-0123456789ab@4e2bf4c:go.mod",
		"ssh://git@gitlab.acme.internal:2222/platform/orders-api.git@4e2bf4c:a:b.go",
	} {
		want, ok := Parse(source)
		if got, read := FromMap(want.String()); !ok || !read || got != want {
			t.Errorf("%q: FromMap(%q) = %+v, %v; want %+v", source, want.String(), got, read, want)
		}
	}
	for _, s := range []string{
		"", "github.com__acme__orders", "github.com__acme__orders@4e2bf", "github.com__acme__orders@4E2BF4C",
		"github.com__acme__orders@4e2bf4c:", "github.com__acme__orders@4e2bf4c:../x", "github.com__acme__orders@4e2bf4c:a b",
		"orders@4e2bf4c", "github.com/acme/orders@4e2bf4c", "github.com__acme__orders@4e2bf4c:dir/a@deadbee",
		"github.com__acme__orders, github.com__acme__x@4e2bf4c", "github.com__acme__orders@4e2bf4c:docs](evil.md)",
		"github.com__acme__orders@4e2bf4c:internal/\x1b[2Jx", "github.com__acme__orders@4e2bf4c:a\u2028b",
	} {
		if r, ok := FromMap(s); ok {
			t.Errorf("%q read as %+v", s, r)
		}
	}
}

// Movements read as the pointers say them, and a page's refs combine into
// the largest movement, unchanged when every one was measured unchanged,
// and none otherwise.
func TestNoteAndTally(t *testing.T) {
	for _, tc := range []struct {
		m    Movement
		want string
	}{
		{Movement{}, "code unchanged since 2026-10-30"},
		{Movement{Count: 1}, "code moved 1 commit since 2026-10-30"},
		{Movement{Count: 14}, "code moved 14 commits since 2026-10-30"},
		{Movement{Count: MaxCount, Capped: true}, "code moved 1000+ commits since 2026-10-30"},
	} {
		if got := Note("2026-10-30", tc.m); got != tc.want {
			t.Errorf("Note(%+v) = %q", tc.m, got)
		}
	}
	if got := Note("", Movement{Count: 3}); got != "" {
		t.Errorf("a note without a day: %q", got)
	}
	tally := func(adds ...any) (Movement, bool) {
		var tl Tally
		for i := 0; i < len(adds); i += 2 {
			tl.Add(adds[i].(Movement), adds[i+1].(bool))
		}
		return tl.Result()
	}
	for _, tc := range []struct {
		adds []any
		want Movement
		ok   bool
	}{
		{nil, Movement{}, false},
		{[]any{Movement{}, true, Movement{}, true}, Movement{}, true},
		{[]any{Movement{}, true, Movement{}, false}, Movement{}, false},
		{[]any{Movement{Count: 2}, true, Movement{}, false, Movement{Count: 9}, true}, Movement{Count: 9}, true},
		{[]any{Movement{Count: MaxCount, Capped: true}, true, Movement{Count: 9}, true}, Movement{Count: MaxCount, Capped: true}, true},
	} {
		if got, ok := tally(tc.adds...); got != tc.want || ok != tc.ok {
			t.Errorf("tally %v = %+v, %v", tc.adds, got, ok)
		}
	}
}

// A page's movement is said by its line on the map: none for a status taken
// at other commits than the line cites, a line without code, an as_of that
// is not a day, or code that couldn't be measured.
func TestMovementSince(t *testing.T) {
	ref, ok := Parse("github.com/acme/ledger@4e2bf4c:internal/books/")
	other, ok2 := Parse("github.com/acme/ledger@9c1d2e3:internal/books/")
	if !ok || !ok2 {
		t.Fatal("Parse")
	}
	moved := PageStatus{Slug: "ledger", Repos: []RepoStatus{{Key: "github.com__acme__ledger", Commit: "4e2bf4c", State: Moved, Moved: 14}}}
	if m, ok := moved.MovementSince("2026-10-30", []Ref{ref}); !ok || m != (Movement{Count: 14}) {
		t.Errorf("moved: %+v %v", m, ok)
	}
	unmeasured := PageStatus{Slug: "ledger", Repos: []RepoStatus{{Key: "github.com__acme__ledger", Commit: "4e2bf4c", State: NoClone}}}
	for name, c := range map[string]struct {
		p    PageStatus
		asOf string
		refs []Ref
	}{
		"other commits": {moved, "2026-10-30", []Ref{other}},
		"no code":       {moved, "2026-10-30", nil},
		"no day":        {moved, "", []Ref{ref}},
		"not measured":  {unmeasured, "2026-10-30", []Ref{ref}},
	} {
		if m, ok := c.p.MovementSince(c.asOf, c.refs); ok {
			t.Errorf("%s: %+v", name, m)
		}
	}
}

// Commits counts the commits on HEAD since a commit that touch the paths
// given, together, or any path; Missing names the paths a tree lacks, a
// folder found by itself or by a file in it; a commit the clone lacks is
// ErrNoCommit; a commit off HEAD's history is counted since the commit the
// two have in common; and a path renamed away stops counting at the rename
// and is missing at HEAD.
func TestCommitsAndMissing(t *testing.T) {
	home := isolate(t)
	ctx := context.Background()
	dir := newRepo(t, filepath.Join(home, "orders"), "")
	first := change(t, dir, "first", "internal/orders/service.go", "a", "api/orders.proto", "p", "README.md", "r")
	change(t, dir, "orders", "internal/orders/service.go", "b")
	change(t, dir, "docs", "README.md", "s")
	change(t, dir, "api", "api/orders.proto", "q")
	run(t, dir, "mv", "api/orders.proto", "api/v2.proto")
	renamed := change(t, dir, "rename the proto")
	change(t, dir, "orders again", "internal/orders/service.go", "c")
	for _, tc := range []struct {
		paths []string
		want  int
	}{
		{nil, 5},
		{[]string{"internal/orders"}, 2},
		{[]string{"internal/orders/service.go"}, 2},
		{[]string{"internal/orders", "README.md"}, 3},
		{[]string{"api/orders.proto"}, 2},
		{[]string{"cmd"}, 0},
	} {
		if m, err := Commits(ctx, dir, first[:7], tc.paths); err != nil || m != (Movement{Count: tc.want}) {
			t.Errorf("Commits %v = %+v, %v; want %d", tc.paths, m, err, tc.want)
		}
	}
	if m, err := Commits(ctx, dir, renamed, []string{"api/orders.proto"}); err != nil || m.Count != 0 {
		t.Errorf("after the rename: %+v %v", m, err)
	}
	if _, err := Commits(ctx, dir, "0123456789abcdef", nil); !errors.Is(err, ErrNoCommit) {
		t.Errorf("an unknown commit: %v", err)
	}
	if has, err := HasCommit(ctx, dir, "0123456"); has || err != nil {
		t.Errorf("HasCommit of an unknown commit: %v %v", has, err)
	}
	if has, err := HasCommit(ctx, dir, first[:12]); !has || err != nil {
		t.Errorf("HasCommit: %v %v", has, err)
	}
	missing, err := Missing(ctx, dir, first, []string{"internal/orders", "internal/orders/service.go", "api/orders.proto", "internal/ord", "cmd/eva-md"})
	if err != nil || !reflect.DeepEqual(missing, []string{"internal/ord", "cmd/eva-md"}) {
		t.Errorf("Missing at the first commit = %q, %v", missing, err)
	}
	if missing, err := Missing(ctx, dir, "HEAD", []string{"internal", "api/orders.proto", "api/v2.proto"}); err != nil || !reflect.DeepEqual(missing, []string{"api/orders.proto"}) {
		t.Errorf("Missing at HEAD = %q, %v", missing, err)
	}
	// A commit on another branch: not on HEAD's history, counted since the
	// commit the two have in common.
	run(t, dir, "checkout", "--quiet", "-b", "side", first)
	side := change(t, dir, "side", "internal/orders/side.go", "s")
	run(t, dir, "checkout", "--quiet", "main")
	if on, err := Ancestor(ctx, dir, side); on || err != nil {
		t.Errorf("Ancestor of a side commit: %v %v", on, err)
	}
	if on, err := Ancestor(ctx, dir, first); !on || err != nil {
		t.Errorf("Ancestor of the first commit: %v %v", on, err)
	}
	if m, err := Commits(ctx, dir, side, []string{"internal/orders"}); err != nil || m.Count != 2 {
		t.Errorf("Commits since a side commit: %+v %v", m, err)
	}
	// Head names the branch, or none for a detached HEAD.
	head := run(t, dir, "rev-parse", "HEAD")
	if commit, branch, err := Head(ctx, dir); commit != head || branch != "main" || err != nil {
		t.Errorf("Head = %q %q %v", commit, branch, err)
	}
	run(t, dir, "checkout", "--quiet", "--detach")
	if commit, branch, err := Head(ctx, dir); commit != head || branch != "" || err != nil {
		t.Errorf("detached Head = %q %q %v", commit, branch, err)
	}
}

// A count stops at 1000, and says it did.
func TestCommitsCapped(t *testing.T) {
	home := isolate(t)
	dir, commits := history(t, filepath.Join(home, "big"), MaxCount+3)
	ctx := context.Background()
	if m, err := Commits(ctx, dir, commits[0], nil); err != nil || m != (Movement{Count: MaxCount, Capped: true}) {
		t.Fatalf("over the cap: %+v %v", m, err)
	}
	if m, err := Commits(ctx, dir, commits[2], []string{"f.txt"}); err != nil || m != (Movement{Count: MaxCount}) {
		t.Fatalf("at the cap: %+v %v", m, err)
	}
}

// Last gives the newest change's day and subject, redacted, on one line and
// cut to 80 characters. The commits are dated, so the day never depends on
// when the test runs.
func TestLast(t *testing.T) {
	home := isolate(t)
	t.Setenv("GIT_COMMITTER_DATE", "2026-10-29T10:00:00+00:00")
	dir := newRepo(t, filepath.Join(home, "orders"), "")
	first := change(t, dir, "first", "a.go", "a")
	secret := "ghp_" + strings.Repeat("ab12", 10)
	change(t, dir, "Rotate the token "+secret+" and "+strings.Repeat("tidy up ", 12), "a.go", "b")
	change(t, dir, "Unrelated", "b.go", "b")
	day, subject, err := Last(context.Background(), dir, first, []string{"a.go"})
	if err != nil || day != "2026-10-29" || strings.Contains(subject, secret) || !strings.HasPrefix(subject, "Rotate the token [REDACTED:") || len([]rune(subject)) != 80 || !strings.HasSuffix(subject, "…") {
		t.Fatalf("Last = %q %q %v", day, subject, err)
	}
	if day, subject, err := Last(context.Background(), dir, first, []string{"c.go"}); day != "" || subject != "" || err != nil {
		t.Fatalf("Last of untouched paths = %q %q %v", day, subject, err)
	}
}

// A git call still running when its time is up is killed, and comes back as
// ErrTimeout. Its git never ends by itself, so only the call's time ends it.
func TestTimeout(t *testing.T) {
	isolate(t)
	fakeGit(t, "exec sleep 600")
	dir := t.TempDir()
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := Commits(ctx, dir, "4e2bf4c", nil)
		done <- err
	}()
	select {
	case err := <-done:
		if !errors.Is(err, ErrTimeout) {
			t.Fatalf("a call out of time: %v", err)
		}
	case <-time.After(hangGuard):
		t.Fatalf("a git call out of time hung: it had not returned after %v", hangGuard)
	}
}
