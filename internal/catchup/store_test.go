package catchup

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// InStore judges a transcript by where its folder and the host's folder
// lead: a path a host writes through a link to the home is in the home's
// folder, a host folder linked elsewhere, as a dotfiles setup links
// ~/.claude, is still the host's, the path written through it or through
// the folder it leads to, and a link from inside the folder to elsewhere
// leads out of it. A folder not made yet is judged by its path as written,
// against the host's folder as written and where it leads. The home is
// HOME as the hook gets it, a real folder: a home that is itself a symlink
// isn't supported.
func TestInStoreFollowsSymlinks(t *testing.T) {
	base := t.TempDir()
	home := filepath.Join(base, "home")
	linked := filepath.Join(base, "home-link")
	dotfiles := filepath.Join(base, "dotfiles")
	other := filepath.Join(base, "other")
	for _, dir := range []string{
		filepath.Join(home, ".claude", "projects", "project"),
		filepath.Join(home, ".codex", "sessions", "2026", "10", "29"),
		filepath.Join(base, "outside", "project"),
		filepath.Join(dotfiles, "claude", "projects", "project"),
		other,
	} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	for target, name := range map[string]string{
		home: linked,
		filepath.Join(base, "outside", "project"): filepath.Join(home, ".claude", "projects", "out"),
		filepath.Join(dotfiles, "claude"):         filepath.Join(other, ".claude"),
	} {
		if err := os.Symlink(target, name); err != nil {
			t.Fatal(err)
		}
	}
	claude := func(home, folder string) string { return filepath.Join(home, ".claude", "projects", folder, "s.jsonl") }
	codex := func(home string) string {
		return filepath.Join(home, ".codex", "sessions", "2026", "10", "29", "rollout-2026-10-29T15-04-05-s.jsonl")
	}
	for _, tc := range []struct {
		name, host, home, path string
		in                     bool
	}{
		{"plain", "claude-code", home, claude(home, "project"), true},
		{"the path through a link to the home", "claude-code", home, claude(linked, "project"), true},
		{"a linked host folder", "claude-code", other, claude(other, "project"), true},
		{"a linked host folder, the path through the folder it leads to", "claude-code", other, filepath.Join(dotfiles, "claude", "projects", "project", "s.jsonl"), true},
		{"a link out of the folder", "claude-code", home, claude(home, "out"), false},
		{"a link out, the path through a link to the home", "claude-code", home, claude(linked, "out"), false},
		{"a folder not made yet", "claude-code", home, claude(home, "new"), true},
		{"a folder not made yet, the path through the folder a linked host folder leads to", "claude-code", other, filepath.Join(dotfiles, "claude", "projects", "new", "s.jsonl"), true},
		{"a folder not made yet, outside", "claude-code", home, filepath.Join(home, ".claude-moved", "projects", "new", "s.jsonl"), false},
		{"the host's folder itself", "claude-code", home, filepath.Join(home, ".claude", "projects"), false},
		{"outside", "claude-code", home, filepath.Join(home, "s.jsonl"), false},
		{"codex", "codex", home, codex(home), true},
		{"codex, the path through a link to the home", "codex", home, codex(linked), true},
		{"codex, Claude Code's folder", "codex", home, claude(home, "project"), false},
		{"a relative path", "claude-code", home, filepath.Join(".claude", "projects", "project", "s.jsonl"), false},
		{"a relative home", "claude-code", "home", claude(home, "project"), false},
		{"another host", "other", home, claude(home, "project"), false},
	} {
		if got := InStore(tc.host, tc.home, tc.path); got != tc.in {
			t.Errorf("%s: InStore(%s, %s, %s) = %v", tc.name, tc.host, tc.home, tc.path, got)
		}
	}
}

// Recovery reads a transcript whose path a host writes through a link to
// the home, as capture registers it, with the home resolved and the path as
// the host wrote it, and refuses one a link inside the host's folder leads
// out of it. InStore, judging against HOME as the hook gets it, agrees.
func TestRecoveryReadsATranscriptWrittenThroughALinkToTheHome(t *testing.T) {
	for _, tc := range []struct {
		name   string
		folder string
		read   bool
	}{{"the path through a link to the home", "project", true}, {"a link out of the folder", "out", false}} {
		t.Run(tc.name, func(t *testing.T) {
			dirs, _, e := fixture(t, "claude-code")
			home := os.Getenv("HOME")
			linked := filepath.Join(t.TempDir(), "home-link")
			if err := os.Symlink(home, linked); err != nil {
				t.Fatal(err)
			}
			outside := filepath.Join(home, "outside")
			if err := os.MkdirAll(outside, 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(outside, filepath.Join(home, ".claude", "projects", "out")); err != nil {
				t.Fatal(err)
			}
			resolved, err := filepath.EvalSymlinks(home)
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(linked, ".claude", "projects", tc.folder, e.Session+".jsonl")
			if err := os.WriteFile(path, nil, 0o600); err != nil {
				t.Fatal(err)
			}
			e.Branch = "linked"
			ctx := context.Background()
			if err := Register(ctx, dirs, resolved, path, filepath.Join(home, "project"), e, false, false, time.Date(2026, 10, 29, 10, 1, 0, 0, time.UTC)); err != nil {
				t.Fatal(err)
			}
			appendJSON(t, path, claudeRow(e, "assistant", "A reply the hooks missed.", "turn1"))
			n, _ := Recover(ctx, dirs, e.Host, e.Session)
			if read := n == 1; read != tc.read || InStore("claude-code", home, path) != tc.read {
				t.Fatalf("recovered %d rows of %s, in the store %v", n, path, InStore("claude-code", home, path))
			}
		})
	}
}
