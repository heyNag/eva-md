package wikigit

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/heyNag/eva-md/internal/identity"
)

// fakeGit puts a git script first on PATH for the rest of the test.
func fakeGit(t *testing.T, script string) {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "git"), []byte("#!/bin/sh\n"+script+"\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

// zeros is a reader of n zero bytes that hides any WriteTo, so io.Copy
// writes them as os/exec copies a command's output: through the buffer's
// ReadFrom when it has one, else its Write.
func zeros(n int) io.Reader { return io.LimitReader(zeroReader{}, int64(n)) }

type zeroReader struct{}

func (zeroReader) Read(p []byte) (int, error) {
	clear(p)
	return len(p), nil
}

// A git command that prints past the output limit fails, and one that
// prints up to it doesn't: os/exec copies its output through the buffer's
// Write, never a ReadFrom that would keep all of it.
func TestRunFailsPastTheOutputLimit(t *testing.T) {
	isolate(t)
	dir := t.TempDir()
	fakeGit(t, `exec /usr/bin/head -c "$EVA_MD_TEST_BYTES" /dev/zero`)
	ctx := context.Background()
	t.Setenv("EVA_MD_TEST_BYTES", fmt.Sprint(outputLimit))
	if out, err := Run(ctx, time.Minute, dir, "status"); err != nil || len(out) != outputLimit {
		t.Fatalf("Run at the limit: %d bytes, %v", len(out), err)
	}
	t.Setenv("EVA_MD_TEST_BYTES", fmt.Sprint(outputLimit+1))
	if out, err := Run(ctx, time.Minute, dir, "status"); err == nil {
		t.Fatalf("Run past the limit kept %d bytes", len(out))
	}
}

// Dirty tells a work tree with changes from one without, however long git's
// list of them: it keeps none of the list, so no output limit applies, and a
// git that fails is an error, never a clean tree.
func TestDirtyReadsAListOfAnyLength(t *testing.T) {
	isolate(t)
	dir := t.TempDir()
	ctx := context.Background()
	fakeGit(t, `[ "$EVA_MD_TEST_BYTES" -eq 0 ] || exec /usr/bin/head -c "$EVA_MD_TEST_BYTES" /dev/zero`)
	for _, c := range []struct {
		bytes int
		want  bool
	}{{0, false}, {1, true}, {2 * outputLimit, true}} {
		t.Setenv("EVA_MD_TEST_BYTES", fmt.Sprint(c.bytes))
		if got, err := Dirty(ctx, time.Minute, dir); err != nil || got != c.want {
			t.Fatalf("a status of %d bytes: dirty %v, %v", c.bytes, got, err)
		}
	}
	fakeGit(t, "exit 1")
	if _, err := Dirty(ctx, time.Minute, dir); err == nil {
		t.Fatal("a git that failed read as a clean tree")
	}
}

// Uninstall counts every changed file of a wiki whose list of them passes
// one command's output limit, as the daily commit lists its staged files.
func TestUnsavedPastOneCommandsLimit(t *testing.T) {
	isolate(t)
	const files = 9000
	fakeGit(t, fmt.Sprintf(`case "$*" in
*rev-parse*) exit 1 ;;
*status*)
	i=0
	while [ $i -lt %d ]; do
		printf '?? projects/p/threads/%%05d-%s.md\0' $i
		i=$((i+1))
	done ;;
esac`, files, strings.Repeat("x", 100)))
	commits, n, err := Unsaved(context.Background(), t.TempDir())
	if err != nil || commits != 0 || n != files {
		t.Fatalf("Unsaved: %d commits, %d files, %v", commits, n, err)
	}
}

// git's diagnostics are only read to sort a failure, so they are cut at
// the output limit and never fail the command.
func TestDiagnosticsAreCutAtTheLimit(t *testing.T) {
	diagnostics := limitedBuffer{truncate: true}
	if n, err := io.Copy(&diagnostics, zeros(2*outputLimit)); err != nil || n != 2*outputLimit {
		t.Fatalf("copied %d bytes: %v", n, err)
	}
	if got := len(diagnostics.String()); got != outputLimit {
		t.Fatalf("kept %d bytes of git's diagnostics, want %d", got, outputLimit)
	}
	out := limitedBuffer{}
	if _, err := io.Copy(&out, zeros(outputLimit+1)); err == nil {
		t.Fatalf("an output past the limit was kept whole: %d bytes", len(out.String()))
	}
}

// The daily commit's list of staged files takes far more than one command's
// output limit, about 100 bytes and a path each, so a commit of more files
// than 1 MiB holds still lists them all.
func TestStagedFilesPastOneCommandsLimit(t *testing.T) {
	isolate(t)
	const files = 12000
	fakeGit(t, fmt.Sprintf(`i=0
while [ $i -lt %d ]; do
	printf ':100644 100644 %s %s M\0projects/p/threads/page-%%05d.md\0' $i
	i=$((i+1))
done`, files, strings.Repeat("0", 40), strings.Repeat("1", 40)))
	got, err := stagedFiles(context.Background(), t.TempDir())
	if err != nil || len(got) != files {
		t.Fatalf("stagedFiles: %d files, %v", len(got), err)
	}
	if got[files-1].path != fmt.Sprintf("projects/p/threads/page-%05d.md", files-1) || got[0].mode != "100644" {
		t.Fatalf("stagedFiles: last %+v, first %+v", got[files-1], got[0])
	}
}

// planted holds one variable of each kind every git eva-md runs leaves out,
// and kept some it passes on: git's own config files, what signs and
// authenticates, and the locale's other variables, which LC_ALL=C overrides.
var (
	planted = map[string]string{
		"GIT_DIR": "/elsewhere/.git", "GIT_WORK_TREE": "/elsewhere", "GIT_IMPLICIT_WORK_TREE": "0", "GIT_INDEX_FILE": "/elsewhere/index",
		"GIT_COMMON_DIR": "/elsewhere/.git", "GIT_OBJECT_DIRECTORY": "/elsewhere/objects", "GIT_ALTERNATE_OBJECT_DIRECTORIES": "/elsewhere/alt",
		"GIT_GRAFT_FILE": "/elsewhere/grafts", "GIT_SHALLOW_FILE": "/elsewhere/shallow", "GIT_REPLACE_REF_BASE": "refs/elsewhere/",
		"GIT_NO_REPLACE_OBJECTS": "1", "GIT_PREFIX": "sub/", "GIT_NAMESPACE": "elsewhere",
		"GIT_CONFIG": "/elsewhere/config", "GIT_CONFIG_PARAMETERS": "'core.hookspath'='/elsewhere/hooks'",
		"GIT_CONFIG_COUNT": "1", "GIT_CONFIG_KEY_0": "remote.origin.url", "GIT_CONFIG_VALUE_0": "https://example.com/decoy/repo.git",
		"GIT_LITERAL_PATHSPECS": "1", "GIT_GLOB_PATHSPECS": "1", "GIT_NOGLOB_PATHSPECS": "1", "GIT_ICASE_PATHSPECS": "1",
		"GIT_TRACE": "1", "GIT_TRACE2_EVENT": "/elsewhere/trace", "GIT_TRACE_PACKET": "1", "GIT_CURL_VERBOSE": "1",
		"GIT_TERMINAL_PROMPT": "1", "LC_ALL": "de_DE.UTF-8",
	}
	kept = map[string]string{
		"GIT_CONFIG_SYSTEM": "/etc/gitconfig-test", "GIT_SSH_COMMAND": "ssh -o BatchMode=yes", "GIT_ASKPASS": "/usr/bin/false",
		"GIT_EXEC_PATH": "/opt/git-test/libexec", "LANG": "de_DE.UTF-8", "LC_MESSAGES": "de_DE.UTF-8",
	}
)

// set plants every variable of planted and kept for the rest of the test.
func set(t *testing.T) {
	t.Helper()
	for _, vars := range []map[string]string{planted, kept} {
		for k, v := range vars {
			t.Setenv(k, v)
		}
	}
}

// Every git eva-md runs leaves out the same variables, from one list: the
// wiki's runner and the read-only calls agree on each planted one, and the
// read-only calls only add their own two settings, optional locks and lazy
// fetches off.
func TestEveryGitDropsTheSameVariables(t *testing.T) {
	isolate(t)
	set(t)
	check := func(name string, env []string, extra ...string) {
		t.Helper()
		got := map[string]string{}
		for _, kv := range env {
			k, v, _ := strings.Cut(kv, "=")
			if _, twice := got[k]; twice {
				t.Errorf("%s: %s set twice", name, k)
			}
			got[k] = v
		}
		want := map[string]string{"GIT_TERMINAL_PROMPT": "0", "LC_ALL": "C"}
		for _, kv := range extra {
			k, v, _ := strings.Cut(kv, "=")
			want[k] = v
		}
		for k := range planted {
			if v, ok := got[k]; ok && v != want[k] {
				t.Errorf("%s keeps %s=%s", name, k, v)
			}
		}
		for k, v := range kept {
			want[k] = v
		}
		for k, v := range want {
			if got[k] != v {
				t.Errorf("%s: %s=%q, want %q", name, k, got[k], v)
			}
		}
	}
	check("wikigit.Environment", Environment())
	check("identity.GitEnv", identity.GitEnv(), "GIT_OPTIONAL_LOCKS=0", "GIT_NO_LAZY_FETCH=1")
	runner := append(Environment(), "GIT_OPTIONAL_LOCKS=0", "GIT_NO_LAZY_FETCH=1")
	if names := differing(runner, identity.GitEnv()); len(names) > 0 {
		t.Errorf("the wiki's runner and the read-only calls differ in %q", names)
	}
}

// differing names the variables that one environment holds and the other
// doesn't, or holds with another value, never their values, as the
// process's own can hold secrets.
func differing(a, b []string) []string {
	count := map[string]int{}
	for _, kv := range a {
		count[kv]++
	}
	for _, kv := range b {
		count[kv]--
	}
	var names []string
	for kv, n := range count {
		if n != 0 {
			name, _, _ := strings.Cut(kv, "=")
			names = append(names, name)
		}
	}
	slices.Sort(names)
	return slices.Compact(names)
}

// git's messages reach classify and readFailure in English whatever the
// locale, since a git built with translations, such as Homebrew's, words
// them in the locale's language. The fake git words its refusal in German
// unless LC_ALL is C.
func TestReadFailureInAnyLocale(t *testing.T) {
	isolate(t)
	t.Setenv("LC_ALL", "de_DE.UTF-8")
	t.Setenv("LANG", "de_DE.UTF-8")
	fakeGit(t, `if [ "$LC_ALL" = C ]; then
	echo "fatal: repository 'https://gitlab.example.com/me/eva-wiki.git/' not found" >&2
else
	echo "fatal: Repository 'https://gitlab.example.com/me/eva-wiki.git/' nicht gefunden" >&2
fi
exit 128`)
	_, err := Run(context.Background(), time.Minute, t.TempDir(), "ls-remote", "origin")
	var e *Error
	if !errors.As(err, &e) || e.Read != ReadNotFound {
		t.Fatalf("Run: %v, want a read reason of %s", err, ReadNotFound)
	}
}

// Commits and checks that start the same new lock file, or the same new
// month's record file, at once each get it, never an error: on macOS, a
// file created through os.Root without O_EXCL can fail with ENOENT for the
// caller that loses the race.
func TestNewLockAndRecordFilesTakeConcurrentCreators(t *testing.T) {
	isolate(t)
	base := t.TempDir()
	at := time.Date(2026, 10, 31, 9, 30, 0, 0, time.Local)
	for i := 0; i < 50; i++ {
		locks, state := filepath.Join(base, fmt.Sprint("locks-", i)), filepath.Join(base, fmt.Sprint("state-", i))
		start := make(chan struct{})
		var wg sync.WaitGroup
		for worker := 0; worker < 8; worker++ {
			wg.Go(func() {
				<-start
				f, err := lockFile(locks, "commit.lock")
				if err != nil {
					t.Errorf("lock file %d: %v", i, err)
					return
				}
				f.Close()
				if err := appendRecord(state, at, Result{Outcome: Nothing}); err != nil {
					t.Errorf("record %d: %v", i, err)
				}
			})
		}
		close(start)
		wg.Wait()
		if t.Failed() {
			return
		}
		f, err := os.Open(filepath.Join(state, "commits", "2026-10.jsonl"))
		if err != nil {
			t.Fatal(err)
		}
		lines := 0
		for s := bufio.NewScanner(f); s.Scan(); {
			lines++
		}
		f.Close()
		if lines != 8 {
			t.Fatalf("records %d: %d lines, want 8", i, lines)
		}
	}
}
