package lint

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/heyNag/eva-md/internal/paths"
)

func isolate(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(home, "gitconfig"))
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	return home
}

func write(t *testing.T, file, text string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(file), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

// page is a thread page with frontmatter, Now items and Timeline lines
// citing refs.
func page(refs ...string) string {
	var b strings.Builder
	b.WriteString("---\ntype: thread\ntitle: \"acme · main\"\nsources:\n  - resource: \"s:00000000\"\nbranch: \"main\"\n---\n\n# acme · main\n\n## Now\n<!-- eva:now:start -->\nLinting.\n\n### Next\n")
	b.WriteString("- Write the lint [stated] " + refs[0] + " <!-- i:n1 -->\n<!-- eva:now:end -->\n\n## Timeline\n### 2026-10-30\n")
	for _, r := range refs[1:] {
		b.WriteString("- done [stated]: Checked a ref " + r + "\n")
	}
	return b.String()
}

// The lint flags a page over 512 KB and refs past a session's end or to an
// unknown session, passes valid short and full refs, and saves its result.
func TestLint(t *testing.T) {
	home := isolate(t)
	dirs, err := paths.Default()
	if err != nil {
		t.Fatal(err)
	}
	session := "3f2a91c0-1111-2222-3333-444444444444"
	// Three lines, the last unfinished, as a capture cut short leaves it.
	write(t, filepath.Join(dirs.Inbox(), "acme", "claude-code-"+session+".jsonl"), "{}\n{}\n{}")
	wiki := filepath.Join(home, "wiki")
	write(t, filepath.Join(wiki, "index.md"), "# Wiki\n")
	write(t, filepath.Join(wiki, "general", "threads", "index.md"), "# Threads\n\n* [x](missing.md) s:deadbeef#1\n")
	write(t, filepath.Join(wiki, "general", "threads", "default.md"), page("s:3f2a91c0#1", "s:3f2a91c0#3", "s:"+session+"#2"))
	write(t, filepath.Join(wiki, "projects", "acme", "threads", "main.md"), page("s:3f2a91c0#2", "s:3f2a91c0#4", "s:deadbeef#1"))
	big := page("s:3f2a91c0#1") + strings.Repeat("- done [stated]: A long Timeline line that fills the page s:3f2a91c0#2\n", 8000)
	if len(big) <= OversizeBytes {
		t.Fatalf("the big page has only %d bytes", len(big))
	}
	write(t, filepath.Join(wiki, "projects", "acme", "threads", "big.md"), big)
	r, err := Run(context.Background(), wiki, dirs)
	if err != nil {
		t.Fatal(err)
	}
	if r.Pages != 3 || r.BadRefs != 2 || !reflect.DeepEqual(r.Oversize, []string{"projects/acme/threads/big"}) || !reflect.DeepEqual(r.BadRefPages, []string{"projects/acme/threads/main"}) {
		t.Fatalf("result %+v", r)
	}
	file := filepath.Join(dirs.State, "lint.json")
	info, err := os.Stat(file)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("lint.json: %v %v", info, err)
	}
	b, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	var saved Result
	if err := json.Unmarshal(b, &saved); err != nil || !reflect.DeepEqual(saved, r) {
		t.Fatalf("saved %+v, %v", saved, err)
	}
}

// Without an inbox, every ref is unknown; without projects, only the
// general area is read.
func TestLintWithoutInbox(t *testing.T) {
	home := isolate(t)
	dirs, err := paths.Default()
	if err != nil {
		t.Fatal(err)
	}
	wiki := filepath.Join(home, "wiki")
	write(t, filepath.Join(wiki, "index.md"), "# Wiki\n")
	write(t, filepath.Join(wiki, "general", "threads", "default.md"), page("s:3f2a91c0#1", "s:3f2a91c0#2"))
	r, err := Run(context.Background(), wiki, dirs)
	if err != nil {
		t.Fatal(err)
	}
	if r.Pages != 1 || r.BadRefs != 2 || len(r.Oversize) != 0 || !reflect.DeepEqual(r.BadRefPages, []string{"general/threads/default"}) {
		t.Fatalf("result %+v", r)
	}
}
