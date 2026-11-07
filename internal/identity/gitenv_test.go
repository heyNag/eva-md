package identity

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// eva-md's read-only git calls run with lazy fetches off, whatever the
// process's environment says, so a partial clone never fetches from its
// remote.
func TestGitEnvTurnsLazyFetchesOff(t *testing.T) {
	t.Setenv("GIT_NO_LAZY_FETCH", "0")
	var set []string
	for _, kv := range GitEnv() {
		if strings.HasPrefix(kv, "GIT_NO_LAZY_FETCH=") {
			set = append(set, kv)
		}
	}
	if !slices.Equal(set, []string{"GIT_NO_LAZY_FETCH=1"}) {
		t.Fatalf("GIT_NO_LAZY_FETCH in git's environment: %q", set)
	}
}

// A git trace in the environment changes nothing. git writes it to its
// stderr ahead of the "not a git repository" line that tells a folder
// outside any repo from a git that failed, so with it a chat outside a repo
// would fail identity and the hook would store nothing; written to a file,
// it would log every read.
func TestGitTraceChangesNothing(t *testing.T) {
	home := gitEnvForTest(t)
	repo := newRepo(t, filepath.Join(home, "code", "traced"), true)
	outside := filepath.Join(home, "notes")
	if err := os.MkdirAll(outside, 0o700); err != nil {
		t.Fatal(err)
	}
	trace := filepath.Join(home, "trace.log")
	t.Setenv("GIT_TRACE", "1")
	t.Setenv("GIT_TRACE_SETUP", "1")
	t.Setenv("GIT_TRACE2_EVENT", trace)
	ctx := context.Background()
	if got, err := Resolve(ctx, outside); err != nil || got != General() {
		t.Fatalf("outside any repo: %+v, %v", got, err)
	}
	if got, err := Resolve(ctx, repo); err != nil || got.Root != repo || got.Kind != KindLocal || got.Branch != "main" {
		t.Fatalf("in a repo: %+v, %v", got, err)
	}
	if _, err := os.Stat(trace); !os.IsNotExist(err) {
		t.Fatalf("git wrote a trace: %v", err)
	}
}
