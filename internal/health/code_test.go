package health

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/heyNag/eva-md/internal/coderef"
	"github.com/heyNag/eva-md/internal/config"
)

// The system code row reads the code status: none without system pages,
// UNKNOWN before the first status, for one that can't be read, while a
// page's code couldn't be checked, a page can't be read or the map lacks a
// page's code, and PASS otherwise, with what moved, what is gone at HEAD,
// what cites a path its commit lacks or a commit off its clone's branch with
// nothing counted, and what is past its review day. It is never FAIL.
func TestCodeRow(t *testing.T) {
	d := isolated(t)
	c := wikiConfig(t)
	row := func() (Check, bool) {
		var r Report
		inspectCode(&r, d, c, clock)
		for _, check := range r.Checks {
			if check.Name == "system code" {
				return check, true
			}
		}
		return Check{}, false
	}
	if got, ok := row(); ok {
		t.Fatalf("no system pages: %+v", got)
	}
	put(t, filepath.Join(c.WikiPath, "systems", "orders.md"), []byte("not read"))
	if got, _ := row(); got.Status != "UNKNOWN" || got.Detail != "no code status yet" || !strings.Contains(got.Fix, "eva-md system status") {
		t.Fatalf("no status: %+v", got)
	}
	put(t, filepath.Join(d.State, coderef.File), []byte("{not json"))
	if got, _ := row(); got.Status != "UNKNOWN" || got.Detail != "the code status is unreadable" {
		t.Fatalf("unreadable: %+v", got)
	}
	moved := func(slug string, n int, gone ...string) coderef.PageStatus {
		return coderef.PageStatus{Slug: slug, AsOf: "2026-10-28", Repos: []coderef.RepoStatus{{Key: "github.com__acme__" + slug, Commit: "4e2bf4c", State: coderef.Moved, Moved: n, Gone: gone}}}
	}
	unchanged := coderef.PageStatus{Slug: "ledger", Repos: []coderef.RepoStatus{{Key: "github.com__acme__ledger", Commit: "4e2bf4c", State: coderef.Unchanged}}}
	capped := coderef.PageStatus{Slug: "payments", Repos: []coderef.RepoStatus{{Key: "github.com__acme__payments", Commit: "4e2bf4c", State: coderef.Moved, Moved: coderef.MaxCount, Capped: true}}}
	docs := coderef.PageStatus{Slug: "manual", AsOf: "2026-09-01", ReviewBy: "2026-10-15"}
	later := coderef.PageStatus{Slug: "guide", AsOf: "2026-10-01", ReviewBy: "2026-12-01"}
	unchecked := func(slug, state string) coderef.PageStatus {
		return coderef.PageStatus{Slug: slug, Repos: []coderef.RepoStatus{{Key: "github.com__acme__" + slug, Commit: "4e2bf4c", State: state}}}
	}
	for _, tc := range []struct {
		name   string
		report coderef.Report
		status string
		detail string
		fix    string
	}{
		{"all checked", coderef.Report{Pages: []coderef.PageStatus{moved("orders", 14, "api/orders.proto"), capped, unchanged, docs, later}},
			"PASS", "3 pages tied to code (status of 2026-10-31 03:10): 1 unchanged, 2 moved (orders: 14 commits, payments: 1000+ commits); 1 cites a path gone at HEAD (orders); 1 past its review date (manual)",
			"in a chat, ask to digest those systems again; the skill cites the new commit"},
		{"nothing moved", coderef.Report{Pages: []coderef.PageStatus{unchanged, later}},
			"PASS", "1 page tied to code (status of 2026-10-31 03:10): 1 unchanged, 0 moved", ""},
		{"no code", coderef.Report{Pages: []coderef.PageStatus{later, docs}},
			"PASS", "none of the 2 system pages cites code (status of 2026-10-31 03:10); 1 past its review date (manual)", "in a chat, ask to digest those systems again; the skill cites the new commit"},
		{"unchecked", coderef.Report{Pages: []coderef.PageStatus{moved("orders", 2), unchecked("billing", coderef.NoClone), unchecked("search", coderef.NoClone), unchecked("ldap", coderef.UnknownCommit)}, MapWithout: 2, Unreadable: []string{"broken"}},
			"UNKNOWN", "4 pages tied to code (status of 2026-10-31 03:10): 0 unchanged, 1 moved (orders: 2 commits); 3 of 4 could not be checked (no clone on this Mac: billing, search; commit not in the clone: ldap); 1 page is unreadable: broken; the map lacks the code of 2 pages written before it carried code",
			"chat once in each repo's clone, then run eva-md system status; fetch the cited commits into the clones, then run eva-md system status; fix or remove the unreadable pages by hand; write any system page again with eva-md system write <slug> --replace, which rebuilds the map"},
		{"lacks and off branch", coderef.Report{Pages: []coderef.PageStatus{unchanged,
			{Slug: "orders", Repos: []coderef.RepoStatus{{Key: "github.com__acme__orders", Commit: "4e2bf4c", Paths: []string{"internal/order"}, State: coderef.Unchanged, Gone: []string{"internal/order"}}}},
			{Slug: "billing", Repos: []coderef.RepoStatus{{Key: "github.com__acme__billing", Commit: "4e2bf4c", State: coderef.NotOnBranch, Branch: "main"}}},
			{Slug: "side", Repos: []coderef.RepoStatus{{Key: "github.com__acme__side", Commit: "4e2bf4c", State: coderef.NotOnBranch, Moved: 3, Branch: "main"}}}}},
			"PASS", "4 pages tied to code (status of 2026-10-31 03:10): 1 unchanged, 1 moved (side: 3 commits); 1 cites a path its commit lacks (orders); 1 cites a commit off its clone's branch (billing)",
			"in a chat, ask to digest those systems again; the skill cites the new commit"},
		{"timed out", coderef.Report{Pages: []coderef.PageStatus{unchecked("orders", coderef.TimedOut)}},
			"UNKNOWN", "1 page tied to code (status of 2026-10-31 03:10): 0 unchanged, 0 moved; 1 of 1 could not be checked (git ran out of time: orders)", "run eva-md system status"},
	} {
		r := tc.report
		r.V, r.At = 1, "2026-10-31T03:10:00+01:00"
		if err := coderef.Save(d.State, r); err != nil {
			t.Fatal(err)
		}
		got, _ := row()
		if got.Status != tc.status || got.Detail != tc.detail || got.Fix != tc.fix {
			t.Errorf("%s:\n got %+v\nwant %s %q %q", tc.name, got, tc.status, tc.detail, tc.fix)
		}
	}
}

// eva-md status reads the code status and never writes it, nor makes one.
func TestStatusLeavesTheCodeStatus(t *testing.T) {
	d := isolated(t)
	c := wikiConfig(t)
	if err := config.Save(d.ConfigFile(), c); err != nil {
		t.Fatal(err)
	}
	put(t, filepath.Join(c.WikiPath, "systems", "orders.md"), []byte("not read"))
	file := filepath.Join(d.State, coderef.File)
	Status(t.Context(), d, "v0.7.2")
	if _, err := os.Stat(file); !os.IsNotExist(err) {
		t.Fatalf("status made a code status: %v", err)
	}
	if err := coderef.Save(d.State, coderef.Report{V: 1, At: "2026-10-31T03:10:00Z", Pages: []coderef.PageStatus{{Slug: "orders"}}}); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	info, _ := os.Stat(file)
	row := findRow(t, Status(t.Context(), d, "v0.7.2"), "system code")
	after, err := os.ReadFile(file)
	again, _ := os.Stat(file)
	if err != nil || string(after) != string(before) || !again.ModTime().Equal(info.ModTime()) || row.Status != "PASS" {
		t.Fatalf("status changed the code status: %+v", row)
	}
}
