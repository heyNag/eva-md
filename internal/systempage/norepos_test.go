package systempage

import (
	"reflect"
	"strings"
	"testing"
)

// A page that lists no repo and cites no code is written, with a note that
// no project's brief or eva_status points at it, and its line on the map
// names no repo, with an empty list or none. A page whose code sources name
// a repo gets no such note, and neither does the page once written again
// with its repos.
func TestWriteNotesAPageNoRepoLists(t *testing.T) {
	o := fixture(t)
	docs := citing(t, []string{"https://docs.example.com/eng/orders v3"}, "")
	for i, repos := range []string{"", "repos: []\n"} {
		page := strings.Replace(docs, "repos: [git@github.com:acme/orders.git]\n", repos, 1)
		r := mustWrite(t, o, "orders", page, i > 0)
		if !reflect.DeepEqual(r.Notes, []string{noRepos}) {
			t.Fatalf("%q: notes %q", repos, r.Notes)
		}
		if text := file(t, o, "systems/map.md"); !strings.Contains(text, "\n- [Orders service](orders.md): Takes, prices and tracks customer orders · as of 2026-10-30\n") {
			t.Fatalf("%q: map\n%s", repos, text)
		}
	}
	coded := citing(t, []string{"github.com/acme/pricing@4e2bf4c:rules/"}, "")
	coded = strings.NewReplacer("repos: [git@github.com:acme/orders.git]\n", "", "title: Orders service", "title: Pricing rules", "aliases: [order service]", "aliases: [price rules]").Replace(coded)
	r := mustWrite(t, o, "pricing", coded, false)
	if len(r.Notes) != 1 || !strings.HasPrefix(r.Notes[0], "not checked: github.com/acme/pricing at 4e2bf4c: ") {
		t.Fatalf("a page whose code names a repo: notes %q", r.Notes)
	}
	if r := mustWrite(t, o, "orders", citing(t, []string{"https://docs.example.com/eng/orders v4"}, ""), true); len(r.Notes) != 0 {
		t.Fatalf("written again with its repos: notes %q", r.Notes)
	}
}
