package resume

import (
	"context"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/heyNag/eva-md/internal/inbox"
)

// A startup or clear brief says an update for the previous chat is pending,
// right below that chat, while its session file holds a backlog a pass
// could take and no update has found too small, with updates on and a wiki
// set up: the hash-triggered refresh then delivers the Now. A pass takes 600
// bytes or more, or any rest of a group a pass stopped in. It says nothing
// once the update caught up, for a smaller backlog, for a rest the update
// left waiting, with updates off or no wiki, in a compact brief, whose chat
// is its own, and for a chat on another branch, whose update never
// refreshes this branch's Now.
func TestBriefSaysWhenAnUpdateIsPending(t *testing.T) {
	after := regexp.MustCompile(`Previous chat: [^\n]*\n` + regexp.QuoteMeta(pendingLine))
	for _, tc := range []struct {
		name string
		edit func(o *Options, store inbox.Store, file string, size int64)
		want bool
	}{
		{"backlog", func(*Options, inbox.Store, string, int64) {}, true},
		{"clear", func(o *Options, _ inbox.Store, _ string, _ int64) { o.Variant = "clear" }, true},
		{"600 bytes past the watermark", func(_ *Options, store inbox.Store, file string, size int64) {
			setMark(t, store, file, inbox.Mark{Offset: size - 600})
		}, true},
		{"599 bytes past the watermark", func(_ *Options, store inbox.Store, file string, size int64) {
			setMark(t, store, file, inbox.Mark{Offset: size - 599})
		}, false},
		{"rest of a group under 600 bytes", func(_ *Options, store inbox.Store, file string, size int64) {
			setMark(t, store, file, inbox.Mark{Offset: size - 200, More: true})
		}, true},
		{"caught up", func(_ *Options, store inbox.Store, file string, size int64) {
			setMark(t, store, file, inbox.Mark{Offset: size})
		}, false},
		{"rest waiting", func(_ *Options, store inbox.Store, file string, size int64) {
			setMark(t, store, file, inbox.Mark{Offset: size / 2, Wait: size})
		}, false},
		{"updates off", func(o *Options, _ inbox.Store, _ string, _ int64) { o.Config.Updates = false }, false},
		{"no wiki", func(o *Options, _ inbox.Store, _ string, _ int64) { o.Config.WikiPath = "" }, false},
		{"compact", func(o *Options, _ inbox.Store, _ string, _ int64) {
			o.Variant, o.Host, o.Session = "compact", "codex", "source-123"
		}, false},
		{"lineage", func(o *Options, _ inbox.Store, _ string, _ int64) { o.Project.Branch = "feature/other" }, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			o := fixture(t)
			o.Variant, o.Config.Updates = "startup", true
			capture(t, o, "codex", "source-123", "main", o.Now.Add(-time.Hour), "Codename Saffron; retain the pending migration.")
			wiki(t, o, o.Now.Add(-2*time.Hour), "### Next\nFinish the migration")
			store := inbox.Store{Root: o.Dirs.Inbox()}
			file := store.Path(o.Project.Key, "codex", "source-123")
			info, err := os.Stat(file)
			if err != nil {
				t.Fatal(err)
			}
			tc.edit(&o, store, file, info.Size())
			r, err := Build(context.Background(), o)
			if err != nil {
				t.Fatal(err)
			}
			if tc.want && !after.MatchString(r.Text) || !tc.want && strings.Contains(r.Text, pendingLine) {
				t.Fatalf("pending line shown: %v, want %v\n%s", strings.Contains(r.Text, pendingLine), tc.want, r.Text)
			}
			if tc.name == "lineage" && !regexp.MustCompile(`Continuing from main`).MatchString(r.Text) {
				t.Fatalf("not a lineage brief:\n%s", r.Text)
			}
		})
	}
}

func setMark(t *testing.T, store inbox.Store, file string, m inbox.Mark) {
	t.Helper()
	if err := store.SetMark(file, m); err != nil {
		t.Fatal(err)
	}
}

// The brief lists the project's session files it read, newest first, for
// the hook's catch-up: name, path, host, id, time and size.
func TestBriefListsTheFilesItRead(t *testing.T) {
	o := fixture(t)
	o.Variant = "startup"
	capture(t, o, "codex", "older-chat", "main", o.Now.Add(-2*time.Hour), "First.")
	capture(t, o, "claude-code", "newer-chat", "main", o.Now.Add(-time.Hour), "Second.")
	store := inbox.Store{Root: o.Dirs.Inbox()}
	older, newer := store.Path(o.Project.Key, "codex", "older-chat"), store.Path(o.Project.Key, "claude-code", "newer-chat")
	if err := os.Chtimes(older, o.Now.Add(-2*time.Hour), o.Now.Add(-2*time.Hour)); err != nil {
		t.Fatal(err)
	}
	r, err := Build(context.Background(), o)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Files) != 2 {
		t.Fatalf("files %+v", r.Files)
	}
	for i, want := range []struct{ path, host, id string }{{newer, "claude-code", "newer-chat"}, {older, "codex", "older-chat"}} {
		f := r.Files[i]
		info, _ := os.Stat(want.path)
		if f.Path != want.path || f.Host != want.host || f.ID != want.id || f.Size != info.Size() || !f.Mod.Equal(info.ModTime()) {
			t.Errorf("file %d: %+v, want %+v", i, f, want)
		}
	}
}
