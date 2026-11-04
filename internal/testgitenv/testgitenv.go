// Package testgitenv clears, for tests, the git configuration a process's
// environment can carry. Git reads configuration from variables as well as
// from files: GIT_CONFIG_COUNT with GIT_CONFIG_KEY_<n> and
// GIT_CONFIG_VALUE_<n>, GIT_CONFIG_PARAMETERS (what `git -c` passes on to
// the git commands it starts), and GIT_CONFIG, which `git config` reads as
// its file. A test's own home, GIT_CONFIG_GLOBAL and GIT_CONFIG_NOSYSTEM
// shut out the user's and the system's files but not these, so a workspace
// that sets them, such as one that rewrites git@github.com: addresses to
// https://github.com/, would reach every git a test starts, directly or
// through the code it tests, and turn a fixture's made-up address into a
// real one.
//
// Only tests use it, from TestMain, before any test runs; a release never
// calls it. It imports nothing from eva-md, so any package's tests can use
// it without an import cycle.
package testgitenv

import (
	"os"
	"strings"
)

// Clear unsets every variable that carries git configuration, so the tests
// in this process, and every process they start, see only the configuration
// the tests write. A test that checks how eva-md treats one of these
// variables sets it itself, with t.Setenv.
func Clear() {
	for _, kv := range os.Environ() {
		name, _, _ := strings.Cut(kv, "=")
		if name == "GIT_CONFIG" || name == "GIT_CONFIG_PARAMETERS" || name == "GIT_CONFIG_COUNT" ||
			strings.HasPrefix(name, "GIT_CONFIG_KEY_") || strings.HasPrefix(name, "GIT_CONFIG_VALUE_") {
			os.Unsetenv(name)
		}
	}
}
