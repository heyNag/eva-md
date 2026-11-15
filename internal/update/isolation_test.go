package update

import (
	"os"
	"path/filepath"
	"testing"
)

// gitQuiet turns git's background work off in a test's git config: after
// a commit, a clone, a fetch or a push git may otherwise start its
// maintenance in the background, which outlives the command and races the
// removal of the test's folders and the next command's locks.
const gitQuiet = "[gc]\n\tauto = 0\n[maintenance]\n\tauto = false\n"

// isolate gives a test its own home, git config and temp folder, and a PATH
// of only the fake CLIs and the system folders, so no real claude or codex
// can run.
func isolate(t *testing.T) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(home, "gitconfig"))
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	if err := os.WriteFile(filepath.Join(home, "gitconfig"), []byte(gitQuiet), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TMPDIR", t.TempDir())
	t.Setenv("PATH", fakeBin+":/usr/bin:/bin")
}
