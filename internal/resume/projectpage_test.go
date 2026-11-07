package resume

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/heyNag/eva-md/internal/config"
	"github.com/heyNag/eva-md/internal/identity"
	"github.com/heyNag/eva-md/internal/okfadapter"
	"github.com/heyNag/eva-md/internal/threadpage"
)

// ProjectPage finds the thread page as the brief does and counts what its
// Now and the rules pages hold: a fresh page and an older one by their
// as_of, a branch without a page, the project's folder under another name,
// another instance's wiki and no wiki.
func TestProjectPage(t *testing.T) {
	o := fixture(t)
	if got := ProjectPage(context.Background(), o); got.Problem != "the wiki is unreadable" || got.Found || got.Page != "projects/resume/threads/main" {
		t.Fatalf("before the wiki exists: %+v", got)
	}
	none := o
	none.Config = config.Config{}
	if got := ProjectPage(context.Background(), none); got.Problem != "no wiki is set up" || got.Page != "projects/resume/threads/main" {
		t.Fatalf("no wiki: %+v", got)
	}
	for _, asOf := range []time.Time{o.Now.Add(-time.Hour), o.Now.Add(-30 * 24 * time.Hour)} {
		o := fixture(t)
		w := pageWiki(t, o)
		savePage(t, w, "projects/resume/threads/main", "main", asOf, contractPage())
		writeRules(t, o, generalRules, true, "Always run go vet before committing")
		writeRules(t, o, "projects/resume/rules", false, "Never mock the database in tests", "Never push on Fridays")
		got := ProjectPage(context.Background(), o)
		want := PageInfo{Page: "projects/resume/threads/main", Found: true, AsOf: asOf, Counted: true, Decisions: 2, Next: 1, Open: 1, ProjectRules: 2, GeneralRules: 1}
		if !got.AsOf.Equal(asOf) {
			t.Fatalf("as_of %v, want %v", got.AsOf, asOf)
		}
		got.AsOf = asOf
		if got != want {
			t.Fatalf("got %+v\nwant %+v", got, want)
		}
		other := o
		other.Project.Branch = "feature/x"
		if got := ProjectPage(context.Background(), other); got.Found || got.Page != "projects/resume/threads/"+threadpage.ThreadName("feature/x") || got.ProjectRules != 2 {
			t.Fatalf("a branch without a page: %+v", got)
		}
	}
	// A folder under another name, proved by its now page.
	o = fixture(t)
	o.Project.Slug = "resume-0a1b2c"
	w := pageWiki(t, o)
	savePage(t, w, "projects/resume/threads/main", "main", o.Now.Add(-time.Hour), contractPage())
	if got := ProjectPage(context.Background(), o); !got.Found || got.Page != "projects/resume/threads/main" {
		t.Fatalf("another folder name: %+v", got)
	}
	// Another instance's wiki is not read.
	o.Config.Instance = "other-12345678"
	if err := config.SaveSchema(filepath.Join(o.Config.WikiPath, ".eva-md", "schema.toml"), config.WikiSchema{SchemaVersion: 1, Instance: "home-87654321"}); err != nil {
		t.Fatal(err)
	}
	if got := ProjectPage(context.Background(), o); got.Problem != "the wiki is not the instance the config names" || got.Found {
		t.Fatalf("another instance: %+v", got)
	}
	// A Now in another format is found, but not counted.
	o = fixture(t)
	wiki(t, o, o.Now.Add(-time.Hour), "A Now written by hand.")
	if got := ProjectPage(context.Background(), o); !got.Found || got.Counted || got.Decisions+got.Next+got.Open != 0 {
		t.Fatalf("a Now by hand: %+v", got)
	}
	// The general area's page.
	o = fixture(t)
	o.Project = identity.General()
	if got := ProjectPage(context.Background(), o); got.Page != "general/threads/default" {
		t.Fatalf("general: %+v", got)
	}
}

// SystemsFor returns the map's systems whose repos include the project, in
// the map's order when none cites the project's code (for the order when
// some do, see TestBriefRanksSystemsCitingTheProjectsCode), and how many the
// map lists.
func TestSystemsFor(t *testing.T) {
	o := fixture(t)
	if got, total := SystemsFor(context.Background(), o); got != nil || total != 0 {
		t.Fatalf("no map: %v %d", got, total)
	}
	if err := okfadapter.Init(o.Config.WikiPath); err != nil {
		t.Fatal(err)
	}
	writeMap(t, o,
		mapLine("orders", "Orders", "Takes orders", o.Project.Key),
		mapLine("billing", "Billing", "Invoices", "github.com__team__billing"),
		mapLine("search", "Search", "Finds things", "github.com__team__billing", o.Project.Key))
	got, total := SystemsFor(context.Background(), o)
	var slugs []string
	for _, e := range got {
		slugs = append(slugs, e.Slug)
	}
	if strings.Join(slugs, " ") != "orders search" || total != 3 {
		t.Fatalf("%v of %d", slugs, total)
	}
	other := o
	other.Project.Key = "github.com__team__elsewhere"
	if got, total := SystemsFor(context.Background(), other); len(got) != 0 || total != 3 {
		t.Fatalf("another project: %v %d", got, total)
	}
}
