package cindervale

import (
	"path/filepath"
	"testing"
)

func TestDirIsUnderHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir, err := Dir()
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(home, ".cindervale", "cache"); dir != want {
		t.Errorf("Dir = %q, want %q", dir, want)
	}
}
