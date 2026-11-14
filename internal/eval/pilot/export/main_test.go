package export

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/heyNag/eva-md/internal/testgitenv"
)

// TestMain gives the package's tests a home of their own, so nothing the
// pilot's check runs reads or writes the real one, with Go's telemetry off
// in it: a go command otherwise leaves a process behind that writes into
// the home after the command ends. Go's build cache is the one exception
// (AGENTS.md): it is found first, with go env as go test itself finds it,
// and kept through GOCACHE, and GOENV=off keeps go from reading a settings
// file of the person running the tests.
func TestMain(m *testing.M) {
	testgitenv.Clear()
	os.Exit(run(m))
}

func run(m *testing.M) int {
	out, err := exec.Command("go", "env", "GOCACHE").Output()
	cache := strings.TrimSpace(string(out))
	if err != nil || cache == "" || cache == "off" {
		fmt.Fprintln(os.Stderr, "go env GOCACHE gives no build cache:", err)
		return 1
	}
	home, err := os.MkdirTemp("", "pilot-export-home-")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer os.RemoveAll(home)
	config := filepath.Join(home, ".config")
	if runtime.GOOS == "darwin" {
		config = filepath.Join(home, "Library", "Application Support")
	}
	if err := os.MkdirAll(filepath.Join(config, "go", "telemetry"), 0o700); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if err := os.WriteFile(filepath.Join(config, "go", "telemetry", "mode"), []byte("off\n"), 0o600); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	for name, value := range map[string]string{
		"HOME": home, "XDG_CONFIG_HOME": filepath.Join(home, ".config"), "XDG_CACHE_HOME": filepath.Join(home, ".cache"),
		"GOENV": "off", "GOCACHE": cache, "GIT_CONFIG_GLOBAL": filepath.Join(home, "gitconfig"), "GIT_CONFIG_NOSYSTEM": "1",
	} {
		if err := os.Setenv(name, value); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
	}
	return m.Run()
}

// The tests run with a home of their own, Go's telemetry off in it, and go
// names no settings file and keeps the build cache it was given.
func TestTheTestsHaveAHomeOfTheirOwn(t *testing.T) {
	home := os.Getenv("HOME")
	if !strings.HasPrefix(home, os.TempDir()) {
		t.Errorf("HOME is %q, outside the temporary folder", home)
	}
	out, err := exec.Command("go", "env", "GOENV", "GOCACHE", "GOTELEMETRY").Output()
	if want := "\n" + os.Getenv("GOCACHE") + "\noff\n"; err != nil || string(out) != want {
		t.Errorf("go env GOENV GOCACHE GOTELEMETRY gives %q, %v, want %q", out, err, want)
	}
}
