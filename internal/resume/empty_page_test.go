package resume

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/heyNag/eva-md/internal/identity"
	"github.com/heyNag/eva-md/internal/okfadapter"
)

func TestInitialGeneralScaffoldIsNotMemory(t *testing.T) {
	o := fixture(t)
	o.Project = identity.General()
	if err := okfadapter.Init(o.Config.WikiPath); err != nil {
		t.Fatal(err)
	}
	w, err := okfadapter.Open(o.Config.WikiPath, o.Dirs.Locks())
	if err != nil {
		t.Fatal(err)
	}
	// Match the Now region emitted by wikiinit, including its absent as_of.
	placeholder := &okfadapter.Concept{
		Type: "thread", Title: "General · default", Status: "stable",
		Extra: map[string]any{"branch": "default", "state": "active", "project": "general"},
		Body:  "# General · default\n\n## Now\n<!-- eva:now:start -->\nNo chats captured yet.\n<!-- eva:now:end -->\n\n## Timeline\n",
	}
	if err := w.Create("general/threads/default", placeholder, "eva-md/test"); err != nil {
		t.Fatal(err)
	}
	for _, variant := range []string{"startup", "clear", "compact", "resume", "fork", "refresh"} {
		o.Variant = variant
		r, err := Build(context.Background(), o)
		if err != nil || r.Found || r.Text != "" || r.NowHash != "" {
			t.Fatalf("%s treated scaffold as memory: %+v, %v", variant, r, err)
		}
	}
	// Once actual work exists it must still resume from raw captures, without
	// presenting the untouched scaffold as an old summary of that work.
	capture(t, o, "codex", "actual-work", "default", o.Now.Add(-time.Hour), "Continue the public replay check")
	o.Variant = "startup"
	r, err := Build(context.Background(), o)
	if err != nil || !r.Found || !strings.Contains(r.Text, "public replay check") {
		t.Fatalf("raw history absent: %+v, %v", r, err)
	}
	if strings.Contains(r.Text, "No chats captured yet") || strings.Contains(r.Text, "0001-") {
		t.Fatal(r.Text)
	}
}

func TestUndatedRealNowIsPreservedWithUnknownFreshness(t *testing.T) {
	for _, date := range []string{"", "not-a-date", "0001-01-01T00:00:00Z"} {
		t.Run(date, func(t *testing.T) {
			o := fixture(t)
			wiki(t, o, time.Time{}, "### Next\nInspect the pending replay result.")
			w, err := okfadapter.Open(o.Config.WikiPath, o.Dirs.Locks())
			if err != nil {
				t.Fatal(err)
			}
			pg, err := w.Read("projects/resume/threads/main")
			if err != nil {
				t.Fatal(err)
			}
			_, err = w.Update(pg.ID, pg.Hash, func(c *okfadapter.Concept) error {
				if date == "" {
					delete(c.Extra, "as_of")
				} else {
					c.Extra["as_of"] = date
				}
				return nil
			}, "eva-md/test")
			if err != nil {
				t.Fatal(err)
			}
			for _, withHistory := range []bool{false, true} {
				if withHistory {
					capture(t, o, "codex", "recent-work", "main", o.Now.Add(-time.Minute), "Most recent raw checkpoint")
				}
				r, err := Build(context.Background(), o)
				if err != nil || !r.Found || !strings.Contains(r.Text, "Inspect the pending replay result") || !strings.Contains(r.Text, "Now (date unknown)") {
					t.Fatalf("undated memory lost: %+v, %v", r, err)
				}
				if strings.Contains(r.Text, "Now (as of 0001-") || strings.Contains(r.Text, "predates") {
					t.Fatalf("invented page chronology: %s", r.Text)
				}
				if withHistory && (!strings.Contains(r.Text, "freshness unknown") || !strings.Contains(r.Text, "Most recent raw checkpoint")) {
					t.Fatalf("missing uncertainty or raw fallback: %s", r.Text)
				}
			}
		})
	}
}
