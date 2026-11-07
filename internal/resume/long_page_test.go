package resume

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// A thread page whose Timeline grew past 2 MiB still gives its Now, since
// the brief reads a page only up to the end of its Now: in a startup brief,
// in a prompt's refresh, which then repeats nothing, and to eva_status. A
// page whose Now ends past 2 MiB gives none.
func TestLongPageKeepsItsNow(t *testing.T) {
	o := fixture(t)
	capture(t, o, "codex", "source-session", "main", o.Now.Add(-time.Hour), "Keep the export running")
	wiki(t, o, o.Now.Add(-time.Minute), "### Next\nLONG_PAGE_NOW · s:source-s#1")
	file := filepath.Join(o.Config.WikiPath, "projects", "resume", "threads", "main.md")
	f, err := os.OpenFile(file, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	for day := 0; b.Len() <= pageCap+(1<<20); day++ {
		fmt.Fprintf(&b, "\n### %s\n", time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC).AddDate(0, 0, day).Format(time.DateOnly))
		for i := 0; i < 20; i++ {
			fmt.Fprintf(&b, "- done [stated]: exported batch %d of day %d without a retry s:source-s#1\n", i, day)
		}
	}
	if _, err := f.WriteString(b.String()); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(file); err != nil || info.Size() <= pageCap+(1<<20) {
		t.Fatalf("the page is not over 3 MiB: %v %v", info, err)
	}
	o.Variant = "startup"
	r, err := Build(context.Background(), o)
	if err != nil || !strings.Contains(r.Text, "LONG_PAGE_NOW") {
		t.Fatalf("startup %v:\n%s", err, r.Text)
	}
	o.Variant = "refresh"
	refreshed, err := Build(context.Background(), o)
	if err != nil || !strings.Contains(refreshed.Text, "LONG_PAGE_NOW") {
		t.Fatalf("refresh %+v %v", refreshed, err)
	}
	o.PreviousHash = refreshed.NowHash
	if again, err := Build(context.Background(), o); err != nil || again.Text != "" {
		t.Fatalf("an unchanged Now refreshed again: %v %q", err, again.Text)
	}
	if info := ProjectPage(context.Background(), o); !info.Found || info.Problem != "" {
		t.Fatalf("eva_status's page %+v", info)
	}
	// The same lines before the Now push its end past 2 MiB.
	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	moved := strings.Replace(string(data), "## Now\n", "## Notes\n"+b.String()+"\n## Now\n", 1)
	if err := os.WriteFile(file, []byte(moved), 0o644); err != nil {
		t.Fatal(err)
	}
	o.Variant, o.PreviousHash = "startup", ""
	if r, err := Build(context.Background(), o); err != nil || strings.Contains(r.Text, "LONG_PAGE_NOW") {
		t.Fatalf("a Now that ends past 2 MiB was read: %v", err)
	}
}

// Markers after the Now's end are never looked at: a page with a second
// Now block below its Timeline, as a person's paste might leave, still
// gives its first Now, while a second start marker before the first end
// still gives none.
func TestNowMarkersAfterTheNowAreNotLookedAt(t *testing.T) {
	o := fixture(t)
	capture(t, o, "codex", "source-session", "main", o.Now.Add(-time.Hour), "Keep the export running")
	wiki(t, o, o.Now.Add(-time.Minute), "### Next\nFIRST_NOW · s:source-s#1")
	file := filepath.Join(o.Config.WikiPath, "projects", "resume", "threads", "main.md")
	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	pasted := string(data) + "\n## Notes\n<!-- eva:now:start -->\nPASTED_NOW\n<!-- eva:now:end -->\n"
	if err := os.WriteFile(file, []byte(pasted), 0o644); err != nil {
		t.Fatal(err)
	}
	o.Variant = "startup"
	if r, err := Build(context.Background(), o); err != nil || !strings.Contains(r.Text, "FIRST_NOW") || strings.Contains(r.Text, "PASTED_NOW") {
		t.Fatalf("a second Now block below the Timeline: %v\n%s", err, r.Text)
	}
	twice := strings.Replace(pasted, "<!-- eva:now:start -->\n### Next", "<!-- eva:now:start -->\n<!-- eva:now:start -->\n### Next", 1)
	if err := os.WriteFile(file, []byte(twice), 0o644); err != nil {
		t.Fatal(err)
	}
	if r, err := Build(context.Background(), o); err != nil || strings.Contains(r.Text, "FIRST_NOW") {
		t.Fatalf("two start markers before the end: %v\n%s", err, r.Text)
	}
}
