// Package release tests the release flow: scripts/release.sh, run against a
// temporary origin with a fake gh, and the release workflow.
package release

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// Every action the release workflow uses is pinned to a full commit, with
// its version in a comment, so a moved tag can't change what a release runs.
func TestReleaseWorkflowPinsEveryAction(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", ".github", "workflows", "release.yml"))
	if err != nil {
		t.Fatal(err)
	}
	uses := regexp.MustCompile(`^\s*(?:-\s+)?uses:\s*(.*)$`)
	pinned := regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_./-]+@[0-9a-f]{40} # v[0-9]+(\.[0-9]+)*$`)
	n := 0
	for i, line := range strings.Split(string(data), "\n") {
		m := uses.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		n++
		if !pinned.MatchString(strings.TrimSpace(m[1])) {
			t.Errorf("line %d isn't pinned to a commit with its version: %s", i+1, strings.TrimSpace(line))
		}
	}
	if n == 0 {
		t.Fatal("the workflow uses no actions; the check read the wrong file")
	}
}
