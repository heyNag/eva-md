// Package testgitenv clears, for tests, the git configuration and the commit
// identity a process's environment can carry. Git reads configuration from
// variables as well as from files: GIT_CONFIG_COUNT with GIT_CONFIG_KEY_<n>
// and GIT_CONFIG_VALUE_<n>, GIT_CONFIG_PARAMETERS (what `git -c` passes on
// to the git commands it starts), and GIT_CONFIG, which `git config` reads
// as its file. A test's own home, GIT_CONFIG_GLOBAL and GIT_CONFIG_NOSYSTEM
// shut out the user's and the system's files but not these, so a workspace
// that sets them, such as one that rewrites git@github.com: addresses to
// https://github.com/, would reach every git a test starts, directly or
// through the code it tests, and turn a fixture's made-up address into a
// real one.
//
// Git also takes a commit's author and committer from GIT_AUTHOR_NAME,
// GIT_AUTHOR_EMAIL, GIT_COMMITTER_NAME and GIT_COMMITTER_EMAIL, ahead of
// any name and email configured, with `git -c` too. eva-md's git runner
// passes them on, as do the gits tests start themselves, so a workspace
// that sets them, to give its commits its owner's identity, would put that
// identity on every commit a test makes, in place of the fixture's.
//
// Only tests use it, from TestMain, before any test runs; a release never
// calls it. It imports nothing from eva-md, so any package's tests can use
// it without an import cycle.
package testgitenv

import (
	"os"
	"strings"
)

// Clear unsets every variable that carries git configuration (GIT_CONFIG,
// GIT_CONFIG_PARAMETERS, GIT_CONFIG_COUNT, and each GIT_CONFIG_KEY_<n> and
// GIT_CONFIG_VALUE_<n>) and the four that carry a commit's identity
// (GIT_AUTHOR_NAME, GIT_AUTHOR_EMAIL, GIT_COMMITTER_NAME and
// GIT_COMMITTER_EMAIL), so the tests in this process, and every process they
// start, see only the configuration and the identity the tests write. It
// keeps every other variable: GIT_CONFIG_GLOBAL and GIT_CONFIG_NOSYSTEM,
// which tests set to isolate their git, and GIT_AUTHOR_DATE and
// GIT_COMMITTER_DATE, which carry no identity and which some tests set to
// date their commits. A test that checks how eva-md treats one of the
// variables Clear unsets sets it itself, with t.Setenv.
func Clear() {
	for _, kv := range os.Environ() {
		name, _, _ := strings.Cut(kv, "=")
		if name == "GIT_CONFIG" || name == "GIT_CONFIG_PARAMETERS" || name == "GIT_CONFIG_COUNT" ||
			strings.HasPrefix(name, "GIT_CONFIG_KEY_") || strings.HasPrefix(name, "GIT_CONFIG_VALUE_") ||
			name == "GIT_AUTHOR_NAME" || name == "GIT_AUTHOR_EMAIL" || name == "GIT_COMMITTER_NAME" || name == "GIT_COMMITTER_EMAIL" {
			os.Unsetenv(name)
		}
	}
}
