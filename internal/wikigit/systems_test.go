package wikigit_test

import (
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/heyNag/eva-md/internal/wikigit"
)

// SecretLines names the lines the commit's scan would leave a page out for,
// and passes identifiers the scan reads as identifiers.
func TestSecretLines(t *testing.T) {
	secret := "ghp_" + strings.Repeat("ab12", 10)
	text := "## Interfaces\n" +
		"- Branch `local-token-refresh-worker-c9a8c7f1e2d3b4a5`, tracked in ENG-1234\n" +
		"- The CI token is " + secret + "\n" +
		"\n" +
		"api_key = " + secret
	if got := wikigit.SecretLines(text); !reflect.DeepEqual(got, []int{3, 5}) {
		t.Fatalf("lines %v", got)
	}
	if got := wikigit.SecretLines("## Summary\nPlain text.\n"); got != nil {
		t.Fatalf("clean text: %v", got)
	}
}

// The daily commit stages the system pages with the day's other pages.
func TestCommitsSystemPages(t *testing.T) {
	f := newFixture(t)
	f.write(t, "systems/orders.md", "---\ntype: system\ntitle: Orders\ndescription: Takes orders\nstatus: stable\n---\n\n## Summary\nTakes orders.\n\nPart of the [system map](map.md).\n")
	f.write(t, "systems/index.md", "# Systems\n\n* [Orders](orders.md) - Takes orders\n")
	if r := f.commit(t); r.Outcome != wikigit.Committed || len(r.Unstaged) != 0 {
		t.Fatalf("result %+v", r)
	}
	files := f.committedFiles(t)
	if !slices.Contains(files, "systems/orders.md") || !slices.Contains(files, "systems/index.md") {
		t.Fatalf("committed %v", files)
	}
}

// The daily commit stages the knowledge pages, their map and index and the
// recall index with the day's other pages, and counts the pages.
func TestCommitsKnowledgePages(t *testing.T) {
	f := newFixture(t)
	f.write(t, "knowledge/k1.md", "---\ntype: knowledge\ntitle: Hooks stay offline\ndescription: Hooks never make a network call\nstatus: stable\n"+
		"sources:\n  - resource: \"s:3f2a91c0#15\"\nas_of: \"2026-10-28\"\nkind: \"constraint\"\nproject: \"github.com__acme__eva\"\nscope: \"all\"\nstate: \"active\"\n---\n\n"+
		"## Statement\nHooks never make a network call [stated] s:3f2a91c0#15 <!-- t:2026-10-28T10:00:00+01:00 -->\n\nPart of the [knowledge map](map.md).\n")
	f.write(t, "knowledge/map.md", "---\ntype: map\ntitle: Knowledge map\ndescription: Every knowledge page, one line each\nstatus: stable\n---\n\n# Knowledge map\n\n"+
		"- [Hooks stay offline](k1.md): constraint · all projects · github.com__acme__eva · as of 2026-10-28\n")
	f.write(t, "knowledge/index.md", "# Knowledge\n\n* [Hooks stay offline](k1.md) - Hooks never make a network call\n* [Knowledge map](map.md) - Every knowledge page, one line each\n")
	f.write(t, ".eva-md/knowledge.tsv", "# eva-md knowledge index 1\nk1\tconstraint\tstated\tall\tgithub.com__acme__eva\t2026-10-28\t2026-10-28T10:00:00+01:00\t\tHooks stay offline\tHooks never make a network call\t\n")
	r := f.commit(t)
	if r.Outcome != wikigit.Committed || len(r.Unstaged) != 0 || r.Pages != 2 {
		t.Fatalf("result %+v", r)
	}
	files := f.committedFiles(t)
	for _, want := range []string{"knowledge/k1.md", "knowledge/map.md", "knowledge/index.md", ".eva-md/knowledge.tsv"} {
		if !slices.Contains(files, want) {
			t.Fatalf("%s not committed: %v", want, files)
		}
	}
}

// A knowledge folder removed whole is committed as removed with the next
// day's changes, so no tracked page is left behind as deleted.
func TestCommitsARemovedKnowledgeFolder(t *testing.T) {
	f := newFixture(t)
	f.write(t, "knowledge/k1.md", "---\ntype: knowledge\ntitle: Hooks stay offline\ndescription: Hooks never make a network call\nstatus: stable\n"+
		"sources:\n  - resource: \"s:3f2a91c0#15\"\nas_of: \"2026-10-28\"\nkind: \"constraint\"\nproject: \"github.com__acme__eva\"\nscope: \"all\"\nstate: \"active\"\n---\n\n"+
		"## Statement\nHooks never make a network call [stated] s:3f2a91c0#15 <!-- t:2026-10-28T10:00:00+01:00 -->\n\nPart of the [knowledge map](map.md).\n")
	f.write(t, "knowledge/map.md", "---\ntype: map\ntitle: Knowledge map\ndescription: Every knowledge page, one line each\nstatus: stable\n---\n\n# Knowledge map\n\n"+
		"- [Hooks stay offline](k1.md): constraint · all projects · github.com__acme__eva · as of 2026-10-28\n")
	if r := f.commit(t); r.Outcome != wikigit.Committed {
		t.Fatalf("first commit %+v", r)
	}
	if err := os.RemoveAll(filepath.Join(f.wiki, "knowledge")); err != nil {
		t.Fatal(err)
	}
	f.edit(t, "decision: the knowledge folder was cleared [stated] s:3f2a91c0#16")
	f.o.Now = func() time.Time { return day.AddDate(0, 0, 1) }
	if r := f.commit(t); r.Outcome != wikigit.Committed {
		t.Fatalf("second commit %+v", r)
	}
	files := f.committedFiles(t)
	for _, want := range []string{"general/threads/default.md", "knowledge/k1.md", "knowledge/map.md"} {
		if !slices.Contains(files, want) {
			t.Fatalf("%s not committed: %v", want, files)
		}
	}
	if left := git(t, f.wiki, "status", "--porcelain"); left != "" {
		t.Fatalf("left uncommitted: %q", left)
	}
}
