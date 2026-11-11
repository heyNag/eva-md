package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/heyNag/eva-md/internal/eval/guard"
)

// commitAll stages everything in repo and commits it with message.
func commitAll(t *testing.T, repo, message string) string {
	t.Helper()
	gitIn(t, repo, "add", "--all")
	gitIn(t, repo, "commit", "--quiet", "-m", message)
	return gitIn(t, repo, "rev-parse", "HEAD")
}

func TestPathsCommand(t *testing.T) {
	isolate(t)
	repo := filepath.Join(t.TempDir(), "repo")
	write(t, filepath.Join(repo, "docs", "v2", "cards", "AA-1.md"), "# AA-1\n\n```paths\na/**\n```\n")
	write(t, filepath.Join(repo, "a", "x.go"), "package a\n")
	gitIn(t, repo, "init", "--quiet")
	base := commitAll(t, repo, "the lead's first commit")
	write(t, filepath.Join(repo, "a", "y.go"), "package a\n")
	inside := commitAll(t, repo, "inside\n\nCard: AA-1")
	write(t, filepath.Join(repo, "notes.md"), "made up\n")
	lead := commitAll(t, repo, "the lead's")

	code, out, errOut := cmd("paths", "--repo", repo, base, lead)
	want := inside[:12] + " AA-1: its 1 paths are inside its card's paths\n" +
		lead[:12] + " has no Card: trailer: the lead's, not judged (1 paths)\n" +
		"2 commits: 1 judged, 1 the lead's; 0 faults.\n"
	if code != 0 || out != want || errOut != "" {
		t.Fatalf("%d\n%s\nwant\n%s%s", code, out, want, errOut)
	}

	write(t, filepath.Join(repo, "b", "z.go"), "package b\n")
	outside := commitAll(t, repo, "outside\n\nCard: AA-1")
	code, out, _ = cmd("paths", "--repo", repo, lead, outside)
	want = outside[:12] + " AA-1: b/z.go is outside the card's paths\n1 commits: 1 judged, 0 the lead's; 1 faults.\n"
	if code != 1 || out != want {
		t.Fatalf("%d\n%s\nwant\n%s", code, out, want)
	}

	for name, args := range map[string][]string{
		"one commit":      {"paths", "--repo", repo, base},
		"three commits":   {"paths", "--repo", repo, base, lead, outside},
		"an unknown ref":  {"paths", "--repo", repo, "no-such-ref", lead},
		"no repository":   {"paths", "--repo", t.TempDir(), "HEAD~1", "HEAD"},
		"an unknown flag": {"paths", "--fast", base, lead},
	} {
		if code, out, errOut := cmd(args...); code != 2 || out != "" || errOut == "" {
			t.Errorf("%s: %d %q %q", name, code, out, errOut)
		}
	}
}

// sizeEnv points the go command at the test's home, offline, on the
// installed toolchain and Go's own caches, which go test itself uses.
func sizeEnv(t *testing.T) {
	t.Helper()
	out, err := exec.Command("go", "env", "GOCACHE", "GOMODCACHE").Output()
	if err != nil {
		t.Fatal(err)
	}
	caches := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(caches) != 2 {
		t.Fatalf("go env gave %q", out)
	}
	isolate(t)
	home := os.Getenv("HOME")
	config := filepath.Join(home, ".config")
	if runtime.GOOS == "darwin" {
		config = filepath.Join(home, "Library", "Application Support")
	}
	write(t, filepath.Join(config, "go", "telemetry", "mode"), "off\n")
	for k, v := range map[string]string{
		"XDG_CONFIG_HOME": filepath.Join(home, ".config"), "GOENV": "off", "GOCACHE": caches[0], "GOMODCACHE": caches[1],
		"GOTOOLCHAIN": "local", "GOPROXY": "off",
	} {
		t.Setenv(k, v)
	}
}

// The size command fails one line over the ceiling, end to end: a module
// whose eva-md is one file of exactly the ceiling's lines holds, and one
// line more fails.
func TestSizeCommand(t *testing.T) {
	sizeEnv(t)
	root := filepath.Join(t.TempDir(), "module")
	write(t, filepath.Join(root, "go.mod"), "module example.com/app\n\ngo 1.22\n")
	main := "package main\n\nfunc main() {}\n"
	write(t, filepath.Join(root, "cmd", "eva-md", "main.go"), main+strings.Repeat("\n", guard.Ceiling-3))
	code, out, errOut := cmd("size", "--repo", root)
	want := "Built lines: 53694\nScheduled lines: 0, in 0 files of the merged cards' deletes blocks\nNet lines: 53694\nCeiling: 53694\nHeadroom: 0\n"
	if code != 0 || out != want || errOut != "" {
		t.Fatalf("%d\n%s\nwant\n%s%s", code, out, want, errOut)
	}
	write(t, filepath.Join(root, "cmd", "eva-md", "main.go"), main+strings.Repeat("\n", guard.Ceiling-2))
	code, out, _ = cmd("size", "--repo", root)
	if code != 1 || !strings.Contains(out, "Headroom: -1\nThe net lines exceed the ceiling by 1.\n") {
		t.Fatalf("%d\n%s", code, out)
	}
	for name, args := range map[string][]string{
		"an argument":  {"size", "--repo", root, "extra"},
		"no module":    {"size", "--repo", t.TempDir()},
		"unknown flag": {"size", "--fast"},
	} {
		if code, out, errOut := cmd(args...); code != 2 || out != "" || errOut == "" {
			t.Errorf("%s: %d %q %q", name, code, out, errOut)
		}
	}
}
