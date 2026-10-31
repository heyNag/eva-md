package wikiinit

import (
	"context"
	"time"

	"github.com/heyNag/eva-md/internal/wikigit"
)

// git runs a Git command in dir through wikigit's hardened runner, with a
// two-minute limit.
func git(ctx context.Context, dir string, args ...string) (string, error) {
	return wikigit.Run(ctx, 120*time.Second, dir, args...)
}

// gitEnvironment is the environment the runner gives git.
func gitEnvironment() []string { return wikigit.Environment() }

// remoteIdentity checks a remote as wikigit.RemoteIdentity does; it never
// echoes an untrusted URL in an error.
func remoteIdentity(raw string) (string, string, error) { return wikigit.RemoteIdentity(raw) }
