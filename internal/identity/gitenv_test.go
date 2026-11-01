package identity

import (
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
