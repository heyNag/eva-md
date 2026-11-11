package main

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// workplaceWording is how a workplace slips into prose, comments and
// fixtures: a machine, proxy, identity or folder that belongs to work, or a
// company. The ordinary verb, as in a chat that worked on a branch, is fine.
var workplaceWording = regexp.MustCompile(`(?i)\bwork[ -](machines?|macs?|laptops?|proxy|proxies|accounts?|identity|email|person|ca|instance)\b|~/work\b|\bwork\.gitconfig|\bwork@|\bcompan(y|y's|ies)\b|\bemployers?\b|\bcorporate\b|\bat work\b|\bpersonal and work\b`)

// The repo is public, so its docs, code, tests, scripts and workflows speak
// of a self-hosted GitLab, GitHub, a GitHub Enterprise server or a managed
// machine, never of work machines or companies. This reads only the
// checkout's own files: README.md, AGENTS.md, every .md under docs/ at any
// depth (the v2 build's plan, cards, specs and working papers among them),
// the Go files under cmd/ and internal/, and every file under scripts/ and
// .github/.
func TestThePublicRepoUsesNeutralWording(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	self, err := filepath.Abs("wording_test.go")
	if err != nil {
		t.Fatal(err)
	}
	files := []string{filepath.Join(root, "README.md"), filepath.Join(root, "AGENTS.md")}
	// Each folder, and which of its files are read: "" for every file.
	for _, walk := range []struct{ dir, suffix string }{
		{"docs", ".md"},
		{"cmd", ".go"},
		{"internal", ".go"},
		{"scripts", ""},
		{".github", ""},
	} {
		before := len(files)
		err := filepath.WalkDir(filepath.Join(root, walk.dir), func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() && d.Name() == "__pycache__" {
				return filepath.SkipDir
			}
			if d.Type().IsRegular() && strings.HasSuffix(path, walk.suffix) && path != self {
				files = append(files, path)
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		if len(files) == before {
			t.Fatalf("found no file to read in %s", walk.dir)
		}
	}
	for _, path := range files {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		for i, line := range strings.Split(string(data), "\n") {
			// Every word the pattern bans holds one of these, so a line
			// without them is passed over: the pattern alone, on every line
			// of every file, takes minutes under the race detector.
			lower := strings.ToLower(line)
			if !strings.Contains(lower, "work") && !strings.Contains(lower, "compan") &&
				!strings.Contains(lower, "employer") && !strings.Contains(lower, "corporate") {
				continue
			}
			if m := workplaceWording.FindString(line); m != "" {
				rel, _ := filepath.Rel(root, path)
				t.Errorf("%s:%d says %q", rel, i+1, m)
			}
		}
	}
}
