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

// The repo is public, so its docs, code and tests speak of a self-hosted
// GitLab, GitHub, a GitHub Enterprise server or a managed machine, never of
// work machines or companies. This reads only the checkout's own files.
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
	docs, err := filepath.Glob(filepath.Join(root, "docs", "*.md"))
	if err != nil || len(docs) == 0 {
		t.Fatalf("docs: %v, %v", docs, err)
	}
	files = append(files, docs...)
	for _, dir := range []string{"cmd", "internal"} {
		err := filepath.WalkDir(filepath.Join(root, dir), func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if !d.IsDir() && strings.HasSuffix(path, ".go") && path != self {
				files = append(files, path)
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	for _, path := range files {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		for i, line := range strings.Split(string(data), "\n") {
			if m := workplaceWording.FindString(line); m != "" {
				rel, _ := filepath.Rel(root, path)
				t.Errorf("%s:%d says %q", rel, i+1, m)
			}
		}
	}
}
