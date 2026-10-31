package update

import (
	"path/filepath"
	"testing"
)

// isolate gives a test its own home, git config and temp folder, and a PATH
// of only the fake CLIs and the system folders, so no real claude or codex
// can run.
func isolate(t *testing.T) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(home, "gitconfig"))
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("TMPDIR", t.TempDir())
	t.Setenv("PATH", fakeBin+":/usr/bin:/bin")
}
