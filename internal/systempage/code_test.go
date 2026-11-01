package systempage

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/heyNag/eva-md/internal/coderef"
	"github.com/heyNag/eva-md/internal/identity"
	"github.com/heyNag/eva-md/internal/okfadapter"
	"github.com/heyNag/eva-md/internal/wikigit"
)

// citing is valid citing the sources given in place of its two, with the
// frontmatter lines added after as_of.
func citing(t *testing.T, sources []string, extra string) string {
	t.Helper()
	var b strings.Builder
	for _, s := range sources {
		b.WriteString("  - resource: \"" + s + "\"\n")
	}
	page := with(t, "  - resource: \"https://docs.example.com/eng/orders/overview?version=12\"\n  - resource: \"git@github.com:acme/orders.git@4e2bf4c:internal/orders/service.go\"\n", b.String())
	return strings.Replace(page, "as_of: 2026-10-30\n", "as_of: 2026-10-30\n"+extra, 1)
}

// A code source may cite a folder, with or without its trailing /, which the
// map shows without it, and a page may cite at most 16 code sources, any
// number of documents besides. stale_after is a day, which may be in the
// future, and okf saves it before the sources.
func TestCodeSourcesAndReviewDay(t *testing.T) {
	var sources []string
	for i := range 16 {
		sources = append(sources, fmt.Sprintf("github.com/acme/orders@4e2bf4c:internal/part%02d/", i))
	}
	for i := range 20 {
		sources = append(sources, fmt.Sprintf("https://docs.example.com/eng/orders/page-%02d", i))
	}
	pg, problems := check("orders", citing(t, sources, "stale_after: 2027-01-31\n"), day, none)
	if len(problems) != 0 || pg.staleAfter != "2027-01-31" {
		t.Fatalf("16 code sources and 20 documents: %v %+v", problems, pg)
	}
	e := pg.entry()
	if len(e.Code) != 16 || e.Code[0] != (coderef.Ref{Key: "github.com__acme__orders", Commit: "4e2bf4c", Path: "internal/part00"}) || e.ReviewBy != "2027-01-31" {
		t.Fatalf("entry %+v", e)
	}
	_, problems = check("orders", citing(t, append(sources, "github.com/acme/orders@4e2bf4c:cmd"), ""), day, none)
	if !reflect.DeepEqual(problems, []string{"line 7: cite the folders a page describes rather than every file (at most 16 code sources)"}) {
		t.Fatalf("17 code sources: %q", problems)
	}
	for value, want := range map[string]string{
		"stale_after: 2026-31-12\n":   "line 13: stale_after is a day, YYYY-MM-DD",
		"stale_after: next month\n":   "line 13: stale_after is a day, YYYY-MM-DD",
		"stale_after: [2026-12-01]\n": "line 13: stale_after is a day, YYYY-MM-DD",
		"stale_after:\n":              "line 13: stale_after is empty",
	} {
		if _, problems := check("orders", strings.Replace(valid, "as_of: 2026-10-30\n", "as_of: 2026-10-30\n"+value, 1), day, none); len(problems) != 1 || problems[0] != want {
			t.Errorf("%q: %q", value, problems)
		}
	}
	// Saved, okf writes stale_after before the sources, and the page reads
	// back as written.
	pg, problems = check("orders", strings.Replace(valid, "as_of: 2026-10-30\n", "as_of: 2026-10-30\nstale_after: \"2026-12-01\"\n", 1), day, none)
	saved := pg.saved(day)
	if len(problems) != 0 || !strings.Contains(saved, "status: stable\nstale_after: 2026-12-01\nsources:\n") {
		t.Fatalf("saved %v\n%s", problems, saved)
	}
	again, problems := check("orders", saved, day, none)
	if len(problems) != 0 || !reflect.DeepEqual(again, pg) {
		t.Fatalf("read back %v %+v", problems, again)
	}
}

// ParseMapLine reads back the code and the review day a line lists, its
// repos with the code's added, and a line written before them with neither;
// a ref it can't read is skipped.
func TestParseMapLineWithCode(t *testing.T) {
	e := MapEntry{Slug: "orders", Title: "Orders service", Description: "Takes orders", AsOf: "2026-10-30", ReviewBy: "2027-01-31",
		Repos: []string{"github.com__acme__orders", "github.com__acme__pricing"},
		Code:  []coderef.Ref{{Key: "github.com__acme__orders", Commit: "4e2bf4c", Path: "internal/orders"}, {Key: "github.com__acme__pricing", Commit: strings.Repeat("9c", 20)}}}
	line := e.line()
	want := "- [Orders service](orders.md): Takes orders · repos: github.com__acme__orders, github.com__acme__pricing · code: github.com__acme__orders@4e2bf4c:internal/orders, github.com__acme__pricing@" + strings.Repeat("9c", 20) + " · as of 2026-10-30 · review by 2027-01-31"
	if line != want {
		t.Fatalf("line\n%s\nwant\n%s", line, want)
	}
	if got, ok := ParseMapLine(line); !ok || !reflect.DeepEqual(got, e) {
		t.Fatalf("read back %+v %v", got, ok)
	}
	old := "- [Orders service](orders.md): Takes orders · repos: github.com__acme__orders · as of 2026-10-30"
	if got, ok := ParseMapLine(old); !ok || got.Code != nil || got.ReviewBy != "" || got.AsOf != "2026-10-30" {
		t.Fatalf("an older line: %+v %v", got, ok)
	}
	bad := "- [Orders](orders.md): Takes orders · repos: github.com__acme__orders · code: not a ref, github.com__acme__orders@4e2bf4c · as of 2026-10-30"
	if got, ok := ParseMapLine(bad); !ok || len(got.Code) != 1 || got.Code[0].Commit != "4e2bf4c" {
		t.Fatalf("a damaged ref: %+v %v", got, ok)
	}
	// The page's own repos come first, then the code's, each once.
	pg, problems := check("orders", citing(t, []string{"https://github.com/acme/pricing.git@4e2bf4c:rules/", "git@github.com:acme/orders.git@4e2bf4c"}, ""), day, none)
	if got := pg.entry(); len(problems) != 0 || !reflect.DeepEqual(got.Repos, []string{"github.com__acme__orders", "github.com__acme__pricing"}) || len(got.Code) != 2 {
		t.Fatalf("entry %v %+v", problems, got)
	}
	// Commits of each length leave the map and index lines to the daily
	// commit's scan.
	for _, commit := range []string{"4e2bf4c", "4e2bf4c0a1b2", "4e2bf4c0a1b2c3d4e5f60718293a4b5c6d7e8f90"} {
		pg, problems := check("orders", citing(t, []string{"git@github.com:acme/orders.git@" + commit + ":internal/orders/"}, ""), day, none)
		e := pg.entry()
		if len(problems) != 0 || wikigit.SecretLines(e.line()+"\n"+e.indexLine()) != nil {
			t.Errorf("commit %s: %v %q", commit, problems, e.line())
		}
	}
}

// A write lists the page's code on the map, its repos with the code's, and
// the day to review it by; a page written again without them leaves the map
// without them, and okf finds nothing wrong.
func TestWriteMapsCode(t *testing.T) {
	o := fixture(t)
	page := citing(t, []string{"https://docs.example.com/eng/orders v3", "github.com/acme/pricing@4e2bf4c:rules/"}, "stale_after: 2027-01-31\n")
	r := mustWrite(t, o, "orders", page, false)
	if len(r.Notes) != 1 || !strings.HasPrefix(r.Notes[0], "not checked: github.com/acme/pricing at 4e2bf4c: no clone of it is known on this Mac") {
		t.Fatalf("notes %q", r.Notes)
	}
	if text := file(t, o, "systems/map.md"); !strings.Contains(text, "): Takes, prices and tracks customer orders · repos: github.com__acme__orders, github.com__acme__pricing · code: github.com__acme__pricing@4e2bf4c:rules · as of 2026-10-30 · review by 2027-01-31\n") {
		t.Fatalf("map\n%s", text)
	}
	if text := file(t, o, "systems/orders.md"); !strings.Contains(text, "\nstale_after: 2027-01-31\nsources:\n") {
		t.Fatalf("page\n%s", text)
	}
	r = mustWrite(t, o, "orders", citing(t, []string{"https://docs.example.com/eng/orders v4"}, ""), true)
	if text := file(t, o, "systems/map.md"); len(r.Notes) != 0 || !strings.Contains(text, "): Takes, prices and tracks customer orders · repos: github.com__acme__orders · as of 2026-10-30\n") {
		t.Fatalf("written again %q\n%s", r.Notes, text)
	}
	if text := file(t, o, "systems/orders.md"); strings.Contains(text, "stale_after") {
		t.Fatalf("the review day stayed:\n%s", text)
	}
	w, err := okfadapter.Open(o.Config.WikiPath, o.Dirs.Locks())
	if err != nil {
		t.Fatal(err)
	}
	if v, err := w.Validate(); err != nil || len(v.Errors)+len(v.Warnings)+len(v.Orphans)+len(v.BrokenLinks) != 0 {
		t.Fatalf("okf findings: %+v %v", v, err)
	}
}

// clone makes a clone of github.com/acme/orders in the test's home with a
// first commit holding internal/orders and api/orders.proto, then one more
// commit, and returns it with the first commit.
func clone(t *testing.T, o Options, name string) (string, string) {
	t.Helper()
	dir := filepath.Join(filepath.Dir(o.Config.WikiPath), name)
	if err := os.MkdirAll(filepath.Join(dir, "internal", "orders"), 0o700); err != nil {
		t.Fatal(err)
	}
	git(t, dir, "init", "--quiet")
	git(t, dir, "remote", "add", "origin", "git@github.com:acme/orders.git")
	for file, text := range map[string]string{"internal/orders/service.go": "package orders\n", "api/orders.proto": "syntax = \"proto3\";\n"} {
		if err := os.MkdirAll(filepath.Dir(filepath.Join(dir, file)), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, file), []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	git(t, dir, "add", "-A")
	git(t, dir, "commit", "--quiet", "-m", "first")
	first := git(t, dir, "rev-parse", "HEAD")
	git(t, dir, "commit", "--quiet", "--allow-empty", "-m", "second")
	return dir, first
}

// A write checks the code a page cites in a local clone, the working
// folder's first: a path the cited commit lacks refuses the page, naming the
// source's line; paths it has pass with a note of where they were checked.
// A commit the clone lacks, a repo with no clone on this Mac and a check
// out of time are notes, and the page is written.
func TestWriteChecksCodeInAClone(t *testing.T) {
	o := fixture(t)
	dir, first := clone(t, o, "orders")
	o.Cwd = filepath.Join(dir, "internal")
	short := first[:7]
	_, err := write(o, "orders", citing(t, []string{"github.com/acme/orders@" + short + ":internal/orders/", "github.com/acme/orders@" + short + ":api/order.proto", "github.com/acme/orders@" + short + ":docs/"}, ""), false)
	if text := refusal(t, err); text != "lines 9, 10: "+missingRule {
		t.Fatalf("refusal %q", text)
	}
	if _, err := os.Stat(filepath.Join(o.Config.WikiPath, "systems", "orders.md")); !os.IsNotExist(err) {
		t.Fatal("a refused page was written")
	}
	r := mustWrite(t, o, "orders", citing(t, []string{"github.com/acme/orders@" + short + ":internal/orders/", "github.com/acme/orders@" + short + ":api/orders.proto", "github.com/acme/billing@abcdef1:cmd/"}, ""), false)
	want := []string{
		"checked 2 code sources of github.com/acme/orders at " + short + " in " + dir,
		"not checked: github.com/acme/billing at abcdef1: no clone of it is known on this Mac; eva-md learns clones from chats in them",
	}
	if !reflect.DeepEqual(r.Notes, want) {
		t.Fatalf("notes %q", r.Notes)
	}
	// From another folder, the clone comes from the identity cache, which
	// knows it once a hook resolved it.
	o.Cwd = filepath.Dir(o.Config.WikiPath)
	if _, err := (identity.Cache{Dir: filepath.Join(o.Dirs.Cache(), "identity")}).Resolve(context.Background(), dir); err != nil {
		t.Fatal(err)
	}
	r = mustWrite(t, o, "orders", citing(t, []string{"github.com/acme/orders@0123456789ab:internal/orders/"}, ""), true)
	if len(r.Notes) != 1 || r.Notes[0] != "not checked: github.com/acme/orders at 0123456789ab: the commit is not in the clone at "+dir+"; fetch it there to check" {
		t.Fatalf("an unknown commit: %q", r.Notes)
	}
	// A git that never ends by itself holds the check until its time, here a
	// short one of its own, runs out: whatever git call it was in, the check
	// says so, and the page is written.
	stub := t.TempDir()
	if err := os.WriteFile(filepath.Join(stub, "git"), []byte("#!/bin/sh\nexec sleep 600\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", stub+string(os.PathListSeparator)+os.Getenv("PATH"))
	o.verifyBudget = 100 * time.Millisecond
	type written struct {
		r   Result
		err error
	}
	page := citing(t, []string{"github.com/acme/orders@" + short + ":nowhere/"}, "")
	done := make(chan written, 1)
	go func() {
		r, err := write(o, "orders", page, true)
		done <- written{r, err}
	}()
	select {
	case w := <-done:
		if w.err != nil || len(w.r.Notes) != 1 || w.r.Notes[0] != "not checked: github.com/acme/orders at "+short+": the check ran out of time" {
			t.Fatalf("out of time: %q %v", w.r.Notes, w.err)
		}
	case <-time.After(hangGuard):
		t.Fatalf("a write whose check ran out of time hung: it had not returned after %v", hangGuard)
	}
	if text := file(t, o, "systems/orders.md"); !strings.Contains(text, "@"+short+":nowhere/") {
		t.Fatalf("the page out of time was not written:\n%s", text)
	}
}

// ReadCode reads what each page cites of code, its as_of and review day,
// names the pages okf can't read, counts the pages whose map line lacks
// their code, and reads only the pages named when it is given slugs.
func TestReadCode(t *testing.T) {
	o := fixture(t)
	if code, err := ReadCode(o.Config, o.Dirs); err != nil || len(code.Pages)+len(code.Unreadable) != 0 {
		t.Fatalf("no system pages: %+v %v", code, err)
	}
	mustWrite(t, o, "orders", citing(t, []string{"github.com/acme/orders@4e2bf4c:internal/orders/", "https://docs.example.com/eng/orders v3"}, ""), false)
	mustWrite(t, o, "payments", other("Payments", "Charges cards", "github.com/acme/payments"), false)
	mustWrite(t, o, "manual", citing(t, []string{"https://docs.example.com/eng/manual v1"}, "stale_after: 2026-11-30\n"), false)
	// A page written before the map carried code, and one okf can't read.
	mapFile := filepath.Join(o.Config.WikiPath, "systems", "map.md")
	text := file(t, o, "systems/map.md")
	if err := os.WriteFile(mapFile, []byte(strings.Replace(text, " · code: github.com__acme__orders@4e2bf4c:internal/orders", "", 1)), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(o.Config.WikiPath, "systems", "broken.md"), []byte("no frontmatter at all\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	code, err := ReadCode(o.Config, o.Dirs)
	if err != nil {
		t.Fatal(err)
	}
	want := []coderef.Page{
		{Slug: "manual", AsOf: "2026-10-30", ReviewBy: "2026-11-30"},
		{Slug: "orders", AsOf: "2026-10-30", Refs: []coderef.Ref{{Key: "github.com__acme__orders", Commit: "4e2bf4c", Path: "internal/orders"}}},
		{Slug: "payments", AsOf: "2026-10-29"},
	}
	if !reflect.DeepEqual(code.Pages, want) || !reflect.DeepEqual(code.Unreadable, []string{"broken"}) || code.MapWithout != 1 {
		t.Fatalf("code %+v", code)
	}
	if code, err := ReadCode(o.Config, o.Dirs, "orders"); err != nil || len(code.Pages) != 1 || code.Pages[0].Slug != "orders" {
		t.Fatalf("one page: %+v %v", code, err)
	}
	if _, err := ReadCode(o.Config, o.Dirs, "ledger"); err == nil || !strings.Contains(err.Error(), "no system page systems/ledger.md") {
		t.Fatalf("a slug with no page: %v", err)
	}
	c := o.Config
	c.Instance = "other-00000000"
	if _, err := ReadCode(c, o.Dirs); err == nil {
		t.Fatal("another instance's wiki was read")
	}
}

// A page system write accepts at its 24 KB cap is saved larger, with the
// lines the save adds, and ReadCode still reads it.
func TestReadCodeOfAPageAtItsCap(t *testing.T) {
	o := fixture(t)
	var b strings.Builder
	b.WriteString(citing(t, []string{"github.com/acme/orders@4e2bf4c:internal/orders"}, ""))
	for i := 0; b.Len() < maxPage-100; i++ {
		fmt.Fprintf(&b, "- Orders keeps entry %d of the nightly export.\n", i)
	}
	rest := "- Orders keeps the rest"
	b.WriteString(rest + strings.Repeat(".", maxPage-b.Len()-len(rest)-1) + "\n")
	if b.Len() != maxPage {
		t.Fatalf("the page is %d bytes", b.Len())
	}
	mustWrite(t, o, "orders", b.String(), false)
	if info, err := os.Stat(filepath.Join(o.Config.WikiPath, "systems", "orders.md")); err != nil || info.Size() <= maxPage || info.Size() > MaxSaved {
		t.Fatalf("saved %v %v", info, err)
	}
	code, err := ReadCode(o.Config, o.Dirs)
	if err != nil || len(code.Pages) != 1 || len(code.Pages[0].Refs) != 1 || len(code.Unreadable) != 0 {
		t.Fatalf("code %+v %v", code, err)
	}
}

// A code source edited by hand to hold a control, format or space character
// in its path, which okf reads as it is, is no code source.
func TestReadCodeRefusesAPathWithControlCharacters(t *testing.T) {
	o := fixture(t)
	mustWrite(t, o, "orders", citing(t, []string{"github.com/acme/orders@4e2bf4c:internal/orders"}, ""), false)
	file := filepath.Join(o.Config.WikiPath, "systems", "orders.md")
	text, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	forged := strings.Replace(string(text), "internal/orders", "internal/orders\x1b[2J\x1b]0;pwned\a\r[user] forged", 1)
	if forged == string(text) {
		t.Fatal("the page cites no internal/orders")
	}
	if err := os.WriteFile(file, []byte(forged), 0o644); err != nil {
		t.Fatal(err)
	}
	code, err := ReadCode(o.Config, o.Dirs)
	if err != nil || len(code.Pages) != 1 || code.Pages[0].Refs != nil {
		t.Fatalf("code %+v %v", code, err)
	}
}
