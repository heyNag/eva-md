package audit

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/heyNag/eva-md/internal/config"
	"github.com/heyNag/eva-md/internal/inbox"
)

// The hosts' sessions with no eva-md file are counted by file name: those
// written before the inbox's oldest line apart, and on Claude Code those in
// a folder left out by design, by the folder's name or where a symlink to
// it leads; Codex's file names give no folder, so that is not known. A
// file that is not a session's, a symlink and a folder too deep are not
// counted, and no session file is opened.
func TestHostSessionsWithNoEvaFile(t *testing.T) {
	f := newFixture(t)
	f.store("claude-code", marker+"-kept", inbox.Entry{TS: at(0, 0), Role: "event", Kind: "event", Text: "session_start"})
	f.store("codex", codexID, inbox.Entry{TS: at(0, 0), Role: "event", Kind: "event", Text: "session_start"})
	projects := filepath.Join(f.home, ".claude", "projects")
	sessions := filepath.Join(f.home, ".codex", "sessions", "2026", "11", "02")
	recent := time.Date(2026, 11, 3, 0, 0, 0, 0, time.UTC)
	old := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	files := []struct {
		path string
		mod  time.Time
	}{
		{filepath.Join(projects, "-"+marker+"-cwd", marker+"-kept.jsonl"), recent},             // has an eva-md file
		{filepath.Join(projects, "-"+marker+"-cwd", marker+"-lost.jsonl"), recent},             // none
		{filepath.Join(projects, "-"+marker+"-cwd", marker+"-old.jsonl"), old},                 // none, older than the inbox
		{filepath.Join(projects, claudeFolder(f.home, false)+"-tmp-x", "a.jsonl"), recent},     // the temporary folder
		{filepath.Join(projects, claudeFolder(f.home, false)+"-realtmp-y", "f.jsonl"), recent}, // where its symlink leads
		{filepath.Join(projects, "-private-var-folders-ab-T", "b.jsonl"), recent},              // macOS's temporary folders
		{filepath.Join(projects, claudeFolder(f.home, false)+"--claude-spares", "c.jsonl"), recent},
		{filepath.Join(projects, claudeFolder(f.home, false)+"-wiki", "d.jsonl"), recent},     // the wiki
		{filepath.Join(projects, claudeFolder(f.home, false)+"-secret-1", "e.jsonl"), recent}, // excluded by a pattern
		{filepath.Join(projects, "-"+marker+"-cwd", "notes.txt"), recent},
		{filepath.Join(projects, "-"+marker+"-cwd", marker+"-lost", "subagents", "agent-1.jsonl"), recent},
		{filepath.Join(sessions, "rollout-2026-11-02T10-00-00-"+codexID+".jsonl"), recent},
		{filepath.Join(sessions, "rollout-2026-11-02T11-00-00-1a1b2c3d-4e5f-4a6b-8c7d-9e0f1a2b3c4d.jsonl"), recent},
		{filepath.Join(sessions, "rollout-2026-11-02T12-00-00-not-a-uuid.jsonl"), recent},
		{filepath.Join(f.home, ".codex", "sessions", "2026", "rollout-2026-11-02T12-00-00-2a1b2c3d-4e5f-4a6b-8c7d-9e0f1a2b3c4d.jsonl"), recent},
	}
	for _, file := range files {
		if err := os.MkdirAll(filepath.Dir(file.path), 0o700); err != nil {
			t.Fatal(err)
		}
		// Unreadable: the count needs the names alone.
		if err := os.WriteFile(file.path, []byte("{}\n"), 0o000); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(file.path, file.mod, file.mod); err != nil {
			t.Fatal(err)
		}
	}
	elsewhere := filepath.Join(f.home, "elsewhere.jsonl")
	if err := os.WriteFile(elsewhere, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(elsewhere, filepath.Join(projects, "-"+marker+"-cwd", "linked.jsonl")); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(f.home, "realtmp"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(f.home, "realtmp"), filepath.Join(f.home, "tmp")); err != nil {
		t.Fatal(err)
	}
	o := Options{Dirs: f.dirs, Home: f.home, TempDir: filepath.Join(f.home, "tmp"),
		Config: config.Config{WikiPath: filepath.Join(f.home, "wiki"), Exclude: []string{"~/secret-[0-9]", "remote:github.com/x/*"}}}
	r, err := Run(t.Context(), o)
	if err != nil {
		t.Fatal(err)
	}
	want := []Store{
		{Host: "claude-code", Files: 9, NoEvaFile: 7, Older: 1, ByDesign: 6, Folders: true},
		{Host: "codex", Files: 2, NoEvaFile: 1},
	}
	if len(r.Stores) != 2 || r.Stores[0] != want[0] || r.Stores[1] != want[1] {
		t.Errorf("stores %+v, want %+v", r.Stores, want)
	}
}

// A Claude Code project folder's name is its folder's path with every
// character but an ASCII letter or digit made a dash, and a pattern keeps
// its wildcards.
func TestClaudeFolder(t *testing.T) {
	for _, c := range []struct {
		dir  string
		glob bool
		want string
	}{
		{"/Users/a/my.project", false, "-users-a-my-project"},
		{"/Users/a/x*y", false, "-users-a-x-y"},
		{"/Users/a/x*y?[ab]", true, "-users-a-x*y??"},
		{"/Users/a/é", false, "-users-a--"},
	} {
		if got := claudeFolder(c.dir, c.glob); got != c.want {
			t.Errorf("claudeFolder(%q, %v) = %q, want %q", c.dir, c.glob, got, c.want)
		}
	}
}
