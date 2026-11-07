package wikiinit

import (
	"context"
	"time"

	"github.com/heyNag/eva-md/internal/wikigit"
)

// GitTimeout bounds each Git command init runs. Tests raise it before any
// test runs.
var GitTimeout = 120 * time.Second

// git runs a Git command in dir through wikigit's hardened runner, within
// GitTimeout.
func git(ctx context.Context, dir string, args ...string) (string, error) {
	return wikigit.Run(ctx, GitTimeout, dir, args...)
}

// dirty reports whether git status finds a change in dir, however many,
// within GitTimeout (see wikigit.Dirty).
func dirty(ctx context.Context, dir string) (bool, error) {
	return wikigit.Dirty(ctx, GitTimeout, dir)
}

// gitEnvironment is the environment the runner gives git.
func gitEnvironment() []string { return wikigit.Environment() }

// remoteIdentity checks a remote as wikigit.RemoteIdentity does; it never
// echoes an untrusted URL in an error.
func remoteIdentity(raw string) (string, string, error) { return wikigit.RemoteIdentity(raw) }
