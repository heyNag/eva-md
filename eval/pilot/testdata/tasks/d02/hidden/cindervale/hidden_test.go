package cindervale

import (
	"os"
	"path/filepath"
	"testing"
)

func TestHiddenDirIsMade(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir, err := Dir()
	if err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(dir); err != nil || !info.IsDir() || dir != filepath.Join(home, ".cindervale", "cache") {
		t.Errorf("Dir = %q, %v: want the folder under the home folder, made", dir, err)
	}
}

func TestHiddenDirOverride(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	want := filepath.Join(t.TempDir(), "moved", "cache")
	t.Setenv("TOVRANE_CACHE_DIR", want)
	dir, err := Dir()
	if err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(dir); err != nil || !info.IsDir() || dir != want {
		t.Errorf("Dir = %q, %v: want the folder the variable names, made", dir, err)
	}
}
