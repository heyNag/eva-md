package lint

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/heyNag/eva-md/internal/inbox"
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

// The lint flags a page over 512 KB and a ref past a session's end, counts
// a ref to a session this inbox never held apart, as another Mac's, passes
// valid short and full refs, and saves its result.
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
	if r.Pages != 3 || r.BadRefs != 1 || r.ElsewhereRefs != 1 || r.RemovedRefs != 0 || !reflect.DeepEqual(r.Oversize, []string{"projects/acme/threads/big"}) || !reflect.DeepEqual(r.BadRefPages, []string{"projects/acme/threads/main"}) {
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

// Without an inbox, as on a Mac that adopted another Mac's wiki or set one
// up again after its local data was removed, every ref names a chat this
// Mac never captured, which is no fault; without projects, only the general
// area is read.
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
	if r.Pages != 1 || r.BadRefs != 0 || r.ElsewhereRefs != 2 || r.RemovedRefs != 0 || len(r.Oversize) != 0 || len(r.BadRefPages) != 0 {
		t.Fatalf("result %+v", r)
	}
}

// The lint sorts each ref that doesn't resolve by what this Mac holds of
// its session: a session eva-md inbox clear removed is removed, whatever
// the line, also by its short id and when a chat still open made its file
// again; a session whose file is gone while its watermark is here was
// captured here and lost, as a file deleted by hand leaves it, so its refs
// are bad, as is a line past a file's end; and a session this inbox holds
// nothing of is another Mac's.
func TestLintSortsRefsToRemovedLostAndOtherMacsChats(t *testing.T) {
	home := isolate(t)
	dirs, err := paths.Default()
	if err != nil {
		t.Fatal(err)
	}
	const (
		kept    = "3f2a91c0-1111-2222-3333-444444444444"
		lost    = "51e0aa21-1111-2222-3333-444444444444"
		removed = "7c4d02b9-1111-2222-3333-444444444444"
		reborn  = "9a8b7c6d-1111-2222-3333-444444444444"
		other   = "c0ffee00-1111-2222-3333-444444444444"
	)
	folder := filepath.Join(dirs.Inbox(), "acme")
	write(t, filepath.Join(folder, "claude-code-"+kept+".jsonl"), "{}\n{}\n{}\n")
	// The lost chat's file was deleted by hand; its watermark stays.
	write(t, filepath.Join(folder, ".state", "codex-"+lost+".jsonl.wm"), "120\n")
	// A chat still open when it was removed made its file again.
	write(t, filepath.Join(folder, "claude-code-"+reborn+".jsonl"), "{}\n")
	store := inbox.Store{Root: dirs.Inbox()}
	removals := []inbox.Capture{{Project: "acme", Host: "codex", ID: removed}, {Project: "acme", Host: "claude-code", ID: reborn}}
	if err := store.RecordRemoved(context.Background(), time.Now(), removals); err != nil {
		t.Fatal(err)
	}
	wiki := filepath.Join(home, "wiki")
	write(t, filepath.Join(wiki, "index.md"), "# Wiki\n")
	write(t, filepath.Join(wiki, "projects", "acme", "threads", "main.md"), page(
		"s:3f2a91c0#1",
		"s:3f2a91c0#9",
		"s:51e0aa21#2",
		"s:"+removed+"#4",
		"s:7c4d02b9#4",
		"s:"+reborn+"#1",
		"s:9a8b7c6d#5",
		"s:c0ffee00#1",
	))
	write(t, filepath.Join(wiki, "general", "threads", "default.md"), page("s:"+kept+"#2", "s:"+other+"#3"))
	r, err := Run(context.Background(), wiki, dirs)
	if err != nil {
		t.Fatal(err)
	}
	if r.Pages != 2 || r.BadRefs != 2 || r.RemovedRefs != 3 || r.ElsewhereRefs != 2 || !reflect.DeepEqual(r.BadRefPages, []string{"projects/acme/threads/main"}) {
		t.Fatalf("result %+v", r)
	}
}

// While an inbox project folder can't be read in full, the folder itself
// or a session file in it, a ref to a chat the rest of the inbox holds
// nothing of may name a chat in that folder, so the lint counts it as
// unknown, never as another Mac's, and names the folder; refs it can judge
// are judged as before.
func TestLintCountsRefsUnknownWhileAFolderCantBeRead(t *testing.T) {
	home := isolate(t)
	dirs, err := paths.Default()
	if err != nil {
		t.Fatal(err)
	}
	const (
		kept    = "3f2a91c0-1111-2222-3333-444444444444"
		general = "51e0aa21-1111-2222-3333-444444444444"
		removed = "7c4d02b9-1111-2222-3333-444444444444"
	)
	folder := filepath.Join(dirs.Inbox(), "acme")
	file := filepath.Join(folder, "claude-code-"+kept+".jsonl")
	write(t, file, "{}\n{}\n")
	write(t, filepath.Join(folder, ".state", "claude-code-"+kept+".jsonl.wm"), "4\n")
	write(t, filepath.Join(dirs.Inbox(), "general", "codex-"+general+".jsonl"), "{}\n")
	if err := (inbox.Store{Root: dirs.Inbox()}).RecordRemoved(context.Background(), time.Now(), []inbox.Capture{{Project: "general", Host: "codex", ID: removed}}); err != nil {
		t.Fatal(err)
	}
	wiki := filepath.Join(home, "wiki")
	write(t, filepath.Join(wiki, "index.md"), "# Wiki\n")
	write(t, filepath.Join(wiki, "projects", "acme", "threads", "main.md"), page("s:3f2a91c0#1", "s:3f2a91c0#2", "s:c0ffee00#1", "s:51e0aa21#1", "s:7c4d02b9#3"))
	r, err := Run(context.Background(), wiki, dirs)
	if err != nil || r.BadRefs != 0 || r.ElsewhereRefs != 1 || r.RemovedRefs != 1 || r.UnknownRefs != 0 || !reflect.DeepEqual(r.UnreadFolders, []string{}) {
		t.Fatalf("every folder read: %+v %v", r, err)
	}
	t.Cleanup(func() { os.Chmod(file, 0o600); os.Chmod(folder, 0o700) })
	for _, tc := range []struct {
		what string
		path string
		mode os.FileMode // its mode when it can be read
	}{{"a session file", file, 0o600}, {"a project folder", folder, 0o700}} {
		if err := os.Chmod(tc.path, 0); err != nil {
			t.Fatal(err)
		}
		r, err := Run(context.Background(), wiki, dirs)
		if err != nil || r.BadRefs != 0 || r.ElsewhereRefs != 0 || r.RemovedRefs != 1 || r.UnknownRefs != 3 || !reflect.DeepEqual(r.UnreadFolders, []string{"acme"}) {
			t.Fatalf("%s that can't be read: %+v %v", tc.what, r, err)
		}
		b, err := os.ReadFile(filepath.Join(dirs.State, "lint.json"))
		if err != nil || !strings.Contains(string(b), `"unknown_refs":3,"unreadable_inbox_folders":["acme"]`) {
			t.Fatalf("%s that can't be read: lint.json %s %v", tc.what, b, err)
		}
		if err := os.Chmod(tc.path, tc.mode); err != nil {
			t.Fatal(err)
		}
	}
}

// Citing counts the refs of the thread pages that name the sessions given,
// by their full ids and by their short ones, and lists the pages holding
// them, as eva-md inbox clear says before it removes them.
func TestCiting(t *testing.T) {
	home := isolate(t)
	session := "3f2a91c0-1111-2222-3333-444444444444"
	wiki := filepath.Join(home, "wiki")
	write(t, filepath.Join(wiki, "index.md"), "# Wiki\n")
	write(t, filepath.Join(wiki, "general", "threads", "default.md"), page("s:deadbeef#1", "s:deadbeef#2"))
	write(t, filepath.Join(wiki, "projects", "acme", "threads", "main.md"), page("s:3f2a91c0#1", "s:"+session+"#2", "s:deadbeef#1", "s:3f2a91c0#40"))
	write(t, filepath.Join(wiki, "projects", "beta", "threads", "main.md"), page("s:3f2a91c0#7"))
	refs, pages, err := Citing(context.Background(), wiki, []string{session})
	if err != nil || refs != 4 || !reflect.DeepEqual(pages, []string{"projects/acme/threads/main", "projects/beta/threads/main"}) {
		t.Fatalf("%d refs on %q, %v", refs, pages, err)
	}
	if refs, pages, err := Citing(context.Background(), wiki, []string{"0badc0de"}); err != nil || refs != 0 || len(pages) != 0 {
		t.Fatalf("%d refs on %q, %v", refs, pages, err)
	}
}
