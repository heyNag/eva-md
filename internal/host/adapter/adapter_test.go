package adapter

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// Tests check what the probe reads, not how fast the machine starts a
// command: its time limits are raised for every test, before any test runs.
func TestMain(m *testing.M) {
	probeTimeout = 10 * time.Minute
	probeWaitDelay = 10 * time.Minute
	os.Exit(m.Run())
}

// All gives v1's two hosts in v1's order, with the names v1 spells, and a
// change to what it gives changes nothing else.
func TestAllIsInV1sOrder(t *testing.T) {
	want := []Host{
		{ID: "claude-code", Names: Names{App: "Claude Code", CLI: "claude", InstallName: "claude"}},
		{ID: "codex", Names: Names{App: "Codex", CLI: "codex", InstallName: "codex"}},
	}
	got := All()
	if !slices.Equal(got, want) {
		t.Fatalf("All() = %+v, want %+v", got, want)
	}
	got[0].ID = "changed"
	if All()[0].ID != ClaudeCode {
		t.Fatal("changing what All gave changed the registry")
	}
}

// ByID takes exactly the spellings the hook's --host takes, and ByInstallName
// exactly those install's --host takes; neither takes the other's.
func TestLookupsTakeExactlyV1sSpellings(t *testing.T) {
	for _, c := range []struct {
		id   string
		want ID
	}{{"claude-code", ClaudeCode}, {"codex", Codex}} {
		h, ok := ByID(c.id)
		if !ok || h.ID != c.want {
			t.Errorf("ByID(%q) = %+v, %v", c.id, h, ok)
		}
	}
	for _, c := range []struct {
		name string
		want ID
	}{{"claude", ClaudeCode}, {"codex", Codex}} {
		h, ok := ByInstallName(c.name)
		if !ok || h.ID != c.want {
			t.Errorf("ByInstallName(%q) = %+v, %v", c.name, h, ok)
		}
	}
	for _, id := range []string{"claude", "Claude Code", "Claude-Code", "CODEX", "codex ", "", "claude_code", "generic"} {
		if h, ok := ByID(id); ok {
			t.Errorf("ByID(%q) found %+v", id, h)
		}
	}
	for _, name := range []string{"claude-code", "Claude Code", "Claude", "Codex", " claude", "", "all"} {
		if h, ok := ByInstallName(name); ok {
			t.Errorf("ByInstallName(%q) found %+v", name, h)
		}
	}
}

// The package imports only the standard library, and its text, tests and
// fixtures too, names no library.
func TestOnlyTheStandardLibraryAndEvasName(t *testing.T) {
	library := "o" + "kf"
	err := filepath.WalkDir(".", func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if strings.Contains(strings.ToLower(string(data)), library) {
			t.Errorf("%s names a library", path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range goFiles(t, ".") {
		for _, imp := range imports(t, path) {
			if first, _, _ := strings.Cut(imp, "/"); strings.Contains(first, ".") {
				t.Errorf("%s imports %s, which is not the standard library", path, imp)
			}
		}
	}
}
