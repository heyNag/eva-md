package okfadapter

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A head read gives the frontmatter and the body up to the end of the first
// line that holds the marker, of a page whose whole is over the limit,
// which a bounded read of the whole refuses. A head over the limit is
// refused, a page without the marker is read whole, a marker in the
// frontmatter ends nothing, and a page behind a link or without frontmatter
// is refused.
func TestReadHeadStopsAtTheMarker(t *testing.T) {
	w := testWiki(t)
	const end = "<!-- eva:now:end -->"
	head := "## Now\n<!-- eva:now:start -->\nThe export runs nightly.\n" + end + "\n"
	rest := "## Timeline\n" + strings.Repeat("- a line of the Timeline that grew for years\n", 2000)
	c := concept("Long thread", head+rest)
	c.Description = "Mentions " + end + " in its frontmatter"
	c.Extra = map[string]any{"as_of": "2026-10-28T09:00:00Z", "head": "4e2bf4c"}
	p := mustCreate(t, w, "general/threads/default", c)
	limit := int64(len(p.Raw)) - int64(len(rest)) + 1024
	if _, err := w.ReadMax(p.ID, limit); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("the whole page is within the limit, so this test proves nothing: %v", err)
	}
	got, err := w.ReadHead(p.ID, end, limit)
	if err != nil {
		t.Fatal(err)
	}
	if got.Body != "\n"+head || got.Title != "Long thread" || got.Extra["as_of"] != "2026-10-28T09:00:00Z" || got.Extra["head"] != "4e2bf4c" || got.Description != c.Description {
		t.Fatalf("head %+v\nbody %q", got, got.Body)
	}
	if _, err := w.ReadHead(p.ID, end, int64(len(p.Raw))-int64(len(rest))-1); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("a head over the limit = %v", err)
	}
	if got, err := w.ReadHead(p.ID, "<!-- no such marker -->", 0); err != nil || got.Body != "\n"+head+rest {
		t.Fatalf("a page without the marker: %v", err)
	}
	if _, err := w.ReadHead(p.ID, "<!-- no such marker -->", limit); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("a page without the marker over the limit = %v", err)
	}
	if err := os.Symlink(filepath.Join(w.Root(), "general", "threads", "default.md"), filepath.Join(w.Root(), "general", "threads", "linked.md")); err != nil {
		t.Fatal(err)
	}
	if _, err := w.ReadHead("general/threads/linked", end, limit); err == nil {
		t.Fatal("a linked page was read")
	}
	if err := os.WriteFile(filepath.Join(w.Root(), "general", "plain.md"), []byte("# Plain\n"+end+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := w.ReadHead("general/plain", end, limit); err == nil {
		t.Fatal("a page without frontmatter was read")
	}
}
