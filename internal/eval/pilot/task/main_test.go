package task

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestMain gives the package's tests a home of their own, so nothing they
// run reads or writes the real one. Go's build cache is the one exception
// (AGENTS.md): it is found first, with go env as go test itself finds it,
// and kept through GOCACHE, and GOENV=off keeps go from reading a settings
// file of the person running the tests, so Grade's own "go env GOCACHE"
// gives that cache back and reads nothing in any home.
func TestMain(m *testing.M) {
	os.Exit(run(m))
}

func run(m *testing.M) int {
	out, err := exec.Command("go", "env", "GOCACHE").Output()
	cache := strings.TrimSpace(string(out))
	if err != nil || cache == "" || cache == "off" {
		fmt.Fprintln(os.Stderr, "go env GOCACHE gives no build cache:", err)
		return 1
	}
	home, err := os.MkdirTemp("", "pilot-task-home-")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer os.RemoveAll(home)
	for name, value := range map[string]string{
		"HOME": home, "XDG_CONFIG_HOME": filepath.Join(home, ".config"), "XDG_CACHE_HOME": filepath.Join(home, ".cache"),
		"GOENV": "off", "GOCACHE": cache,
	} {
		if err := os.Setenv(name, value); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
	}
	return m.Run()
}

// The tests run with a home of their own, and go names no settings file
// and keeps the build cache it was given.
func TestTheTestsHaveAHomeOfTheirOwn(t *testing.T) {
	if home := os.Getenv("HOME"); !strings.HasPrefix(home, os.TempDir()) {
		t.Errorf("HOME is %q, outside the temporary folder", home)
	}
	out, err := exec.Command("go", "env", "GOENV", "GOCACHE").Output()
	if want := "\n" + os.Getenv("GOCACHE") + "\n"; err != nil || string(out) != want {
		t.Errorf("go env GOENV GOCACHE gives %q, %v, want %q", out, err, want)
	}
}
