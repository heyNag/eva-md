package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/heyNag/eva-md/internal/coderef"
	"github.com/heyNag/eva-md/internal/identity"
	"github.com/heyNag/eva-md/internal/paths"
	"github.com/heyNag/eva-md/internal/wikiinit"
)

// codeWiki is a wiki set up by init in the test's home, and a clone of
// github.com/acme/orders beside it whose first commit holds
// internal/orders/a.go. The test runs in the clone. It returns the folders,
// the wiki and the clone with its first commit.
func codeWiki(t *testing.T) (paths.Dirs, string, string, string) {
	t.Helper()
	d, home := gitHomeFixture(t, "")
	remote, wiki := filepath.Join(home, "remote.git"), filepath.Join(home, "wiki")
	gitRun(t, home, "init", "--bare", "--quiet", remote)
	if _, err := wikiinit.Run(context.Background(), wikiinit.Options{Remote: remote, Dir: wiki, Dirs: d, Version: "test", OKFModule: "v0.7.2"}); err != nil {
		t.Fatal(err)
	}
	clone := filepath.Join(home, "orders")
	if err := os.MkdirAll(filepath.Join(clone, "internal", "orders"), 0o700); err != nil {
		t.Fatal(err)
	}
	gitRun(t, clone, "init", "--quiet")
	gitRun(t, clone, "remote", "add", "origin", "git@github.com:acme/orders.git")
	first := commitIn(t, clone, "internal/orders/a.go", "package orders\n", "First")
	// A chat in the clone leaves an entry in the identity cache, after which
	// eva-md finds the clone with no git.
	if _, err := (identity.Cache{Dir: filepath.Join(d.Cache(), "identity")}).Resolve(context.Background(), clone); err != nil {
		t.Fatal(err)
	}
	t.Chdir(clone)
	return d, wiki, clone, first
}

// dayOf is the day git dates commit by, as eva-md says a last change's day,
// so a test that runs across midnight still agrees with it.
func dayOf(t *testing.T, clone, commit string) string {
	t.Helper()
	return gitRun(t, clone, "show", "-s", "--format=%cs", commit)
}

// commitIn writes a file of the clone and commits it.
func commitIn(t *testing.T, clone, file, text, message string) string {
	t.Helper()
	if err := os.WriteFile(filepath.Join(clone, filepath.FromSlash(file)), []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	gitRun(t, clone, "add", "-A")
	gitRun(t, clone, "commit", "--quiet", "-m", message)
	return gitRun(t, clone, "rev-parse", "HEAD")
}

// ordersPage is a system page citing the sources given.
func ordersPage(sources ...string) string {
	page := "---\ntype: system\ntitle: Orders\ndescription: Takes orders\nstatus: stable\n" +
		"generated: { by: claude-code/claude-opus-5-5 }\nsources:\n"
	for _, s := range sources {
		page += "  - resource: \"" + s + "\"\n"
	}
	return page + "aliases: [OrdersSync]\nas_of: 2026-10-30\nstate: active\n---\n\n## Summary\nOrders in short.\n"
}

// evaIn runs eva-md with page on stdin.
func evaIn(t *testing.T, page string, args ...string) (int, string, string) {
	t.Helper()
	var out, errOut strings.Builder
	code := run(context.Background(), args, strings.NewReader(page), &out, &errOut)
	return code, out.String(), errOut.String()
}

// eva-md show names, on stderr after any code lines, the system pages that
// link to the page shown, while stdout stays the page as saved.
func TestShowNamesTheLinkingPages(t *testing.T) {
	_, wiki, _, _ := codeWiki(t)
	if code, _, errOut := evaIn(t, ordersPage("https://docs.example.com/eng/orders v2"), "system", "write", "orders"); code != 0 || errOut != "" {
		t.Fatalf("write orders: %d %q", code, errOut)
	}
	billing := strings.NewReplacer("title: Orders", "title: Billing", "aliases: [OrdersSync]\n", "", "Orders in short.", "Billing bills [orders](orders.md).").Replace(ordersPage("https://docs.example.com/eng/billing v2"))
	if code, _, errOut := evaIn(t, billing, "system", "write", "billing"); code != 0 || errOut != "" {
		t.Fatalf("write billing: %d %q", code, errOut)
	}
	saved, err := os.ReadFile(filepath.Join(wiki, "systems", "orders.md"))
	if err != nil {
		t.Fatal(err)
	}
	if code, out, errOut := evaMD(t, "show", "systems/orders"); code != 0 || out != string(saved)+"\n" || errOut != "eva.md: systems/orders: linked from systems/billing\n" {
		t.Fatalf("show: %d %q\nstderr %q", code, out, errOut)
	}
	if code, _, errOut := evaMD(t, "show", "systems/billing"); code != 0 || errOut != "" {
		t.Fatalf("no page links to billing: %d %q", code, errOut)
	}
}

// One chain, end to end: a page written in a clone has its code checked
// there; once the clone moves two commits, the brief's pointer, system
// status, eva-md show's stderr and the doctor's row all say so, while show's
// stdout stays the page as saved. A check or a count that runs out of time
// says so instead, as their packages' tests show: here the time limits are
// raised, so each says what it found.
func TestCodeEndToEnd(t *testing.T) {
	d, wiki, clone, first := codeWiki(t)
	short := first[:7]
	code, out, errOut := evaIn(t, ordersPage("git@github.com:acme/orders.git@"+short+":internal/orders/"), "system", "write", "orders")
	if code != 0 || errOut != "" || !strings.HasSuffix(out, "; the map has 1 system\nchecked 1 code source of github.com/acme/orders at "+short+" in "+clone+"\n") {
		t.Fatalf("write: %d %q %q", code, out, errOut)
	}
	commitIn(t, clone, "internal/orders/a.go", "package orders // two\n", "Split pricing")
	retry := commitIn(t, clone, "internal/orders/a.go", "package orders // three\n", "Retry payments")
	commitIn(t, clone, "README.md", "Orders\n", "Docs")
	note := "code moved 2 commits since 2026-10-30"

	// The pointer keeps the description only when the test home's path
	// leaves room for it within a pointer's 200 characters.
	pointer := "System: Orders: Takes orders (" + note + "): " + filepath.Join(d.Share, "bin", "eva-md") + " show systems/orders"
	if utf8.RuneCountInString(pointer) > 200 {
		pointer = "System: Orders (" + note + "): " + filepath.Join(d.Share, "bin", "eva-md") + " show systems/orders"
	}
	code, out, _ = evaMD(t, "brief", "--cwd", clone, "--host", "codex")
	if code != 0 || !strings.Contains(out, "\n"+pointer+"\n") {
		t.Fatalf("brief: %d\n%s", code, out)
	}

	code, out, errOut = evaMD(t, "system", "status")
	line := "orders: " + note + " (github.com/acme/orders at " + short + ": internal/orders; measured in " + clone + " on main; last change " + dayOf(t, clone, retry) + ": Retry payments)\n"
	if code != 0 || out != line+"1 of 1 system page tied to code: 1 moved, 0 unchanged, 0 not checked\n" || errOut != "" {
		t.Fatalf("system status: %d %q %q", code, out, errOut)
	}
	s, ok, err := coderef.Load(d.State, coderef.Limit)
	if err != nil || !ok || len(s.Pages) != 1 || s.Pages[0].Repos[0].Moved != 2 || s.Pages[0].Repos[0].Clone != clone {
		t.Fatalf("status file %+v %v %v", s, ok, err)
	}
	if info, err := os.Stat(filepath.Join(d.State, coderef.File)); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("status file mode: %v", err)
	}

	saved, err := os.ReadFile(filepath.Join(wiki, "systems", "orders.md"))
	if err != nil {
		t.Fatal(err)
	}
	code, out, errOut = evaMD(t, "show", "systems/orders")
	if code != 0 || out != string(saved)+"\n" || errOut != "eva.md: systems/orders: "+strings.TrimPrefix(line, "orders: ") {
		t.Fatalf("show: %d %q\nstderr %q", code, out, errOut)
	}
	code, out, errOut = evaMD(t, "show", "systems/orders#Summary")
	if code != 0 || out != "## Summary\nOrders in short.\n\nPart of the [system map](map.md).\n" || !strings.HasPrefix(errOut, "eva.md: systems/orders: "+note) {
		t.Fatalf("show a section: %d %q %q", code, out, errOut)
	}
	if code, _, errOut := evaMD(t, "show", "systems/map"); code != 0 || errOut != "" {
		t.Fatalf("show the map: %d %q", code, errOut)
	}

	_, out, _ = evaMD(t, "status", "--json")
	var r struct {
		Checks []struct{ Name, Status, Detail, Fix string }
	}
	if err := json.Unmarshal([]byte(out), &r); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, c := range r.Checks {
		if c.Name == "system code" {
			found = true
			if c.Status != "PASS" || !strings.Contains(c.Detail, "1 moved (orders: 2 commits)") || !strings.Contains(c.Fix, "digest those systems again") {
				t.Fatalf("doctor row %+v", c)
			}
		}
	}
	if !found {
		t.Fatal("no system code row")
	}
}

// system status prints a line for each repo and commit a page cites, most
// moved first, the pages that couldn't be checked and those past their
// review day, then the counts, and saves the status; with --json it prints
// the status; with slugs it measures and updates only those pages. A slug
// that can't name a page is a usage error, and one with no page fails.
func TestSystemStatusCommand(t *testing.T) {
	d, _, clone, first := codeWiki(t)
	short := first[:7]
	for slug, page := range map[string]string{
		"orders":  ordersPage("git@github.com:acme/orders.git@" + short + ":internal/orders/"),
		"current": strings.Replace(ordersPage("git@github.com:acme/orders.git@"+short), "aliases: [OrdersSync]\n", "", 1),
		"billing": strings.Replace(ordersPage("https://github.com/acme/billing.git@abcdef1:cmd/"), "aliases: [OrdersSync]\n", "", 1),
		"manual":  strings.Replace(ordersPage("https://docs.example.com/eng/manual v2"), "as_of: 2026-10-30\n", "as_of: 2026-10-30\nstale_after: 2026-10-31\n", 1),
	} {
		if code, out, errOut := evaIn(t, strings.Replace(page, "aliases: [OrdersSync]\n", "aliases: ["+slug+"-sync]\n", 1), "system", "write", slug); code != 0 {
			t.Fatalf("write %s: %d %q %q", slug, code, out, errOut)
		}
	}
	split := commitIn(t, clone, "internal/orders/a.go", "package orders // two\n", "Split pricing")
	code, out, errOut := evaMD(t, "system", "status")
	lines := strings.Split(strings.TrimSuffix(out, "\n"), "\n")
	want := []string{
		"current: code moved 1 commit since 2026-10-30 (github.com/acme/orders at " + short + "; measured in " + clone + " on main; last change " + dayOf(t, clone, split) + ": Split pricing)",
		"orders: code moved 1 commit since 2026-10-30 (github.com/acme/orders at " + short + ": internal/orders; measured in " + clone + " on main; last change " + dayOf(t, clone, split) + ": Split pricing)",
		"billing: not checked: no clone of github.com/acme/billing is known on this Mac; eva-md learns clones from chats in them",
		"manual: review due since 2026-10-31",
		"3 of 4 system pages tied to code: 2 moved, 0 unchanged, 1 not checked; 1 past its review date",
	}
	if code != 0 || errOut != "" || strings.Join(lines, "\n") != strings.Join(want, "\n") {
		t.Fatalf("system status: %d %q\n%s", code, errOut, out)
	}
	code, out, _ = evaMD(t, "system", "status", "--json", "orders")
	var r coderef.Report
	if err := json.Unmarshal([]byte(out), &r); code != 0 || err != nil || len(r.Pages) != 1 || r.Pages[0].Slug != "orders" || r.Pages[0].Repos[0].State != coderef.Moved || strings.Contains(out, "Split pricing") {
		t.Fatalf("--json: %d %v %s", code, err, out)
	}
	if s, ok, err := coderef.Load(d.State, coderef.Limit); err != nil || !ok || len(s.Pages) != 4 {
		t.Fatalf("a run over one page kept the others: %+v %v %v", s, ok, err)
	}
	if code, _, errOut := evaMD(t, "system", "status", "Orders"); code != 2 || !strings.HasPrefix(errOut, "eva-md: ") {
		t.Fatalf("a bad slug: %d %q", code, errOut)
	}
	if code, _, errOut := evaMD(t, "system", "status", "ledger"); code != 1 || !strings.Contains(errOut, "no system page systems/ledger.md") {
		t.Fatalf("a slug with no page: %d %q", code, errOut)
	}
}

// maintain takes the day's code status once the wiki has system pages, after
// the lint and before its last step, the search index, and says what it
// found; a wiki whose systems folder can't be read fails the step, and code
// that moved or couldn't be checked never does.
func TestMaintainTakesTheCodeStatus(t *testing.T) {
	d, wiki, clone, first := codeWiki(t)
	if code, out, errOut := evaIn(t, ordersPage("git@github.com:acme/orders.git@"+first[:7]+":internal/orders/", "https://github.com/acme/billing.git@abcdef1"), "system", "write", "orders"); code != 0 {
		t.Fatalf("write: %d %q %q", code, out, errOut)
	}
	commitIn(t, clone, "internal/orders/a.go", "package orders // two\n", "Split pricing")
	code, out, errOut := evaMD(t, "maintain")
	if code != 0 || !strings.HasSuffix(out, "\nmaintain: lint: 1 thread pages, 0 over 512 KB, 0 bad refs\nmaintain: code status: 1 of 1 system page tied to code: 1 moved, 0 unchanged, 1 not checked\nmaintain: index: up to date (0.0 MB)\n") || errOut != "" {
		t.Fatalf("maintain: %d %q %q", code, out, errOut)
	}
	s, ok, err := coderef.Load(d.State, coderef.Limit)
	if err != nil || !ok || len(s.Pages) != 1 || len(s.Pages[0].Repos) != 2 || s.Pages[0].Repos[0].Moved != 1 || s.Pages[0].Repos[1].State != coderef.NoClone {
		t.Fatalf("status %+v %v %v", s, ok, err)
	}
	if os.Geteuid() == 0 {
		return
	}
	systems := filepath.Join(wiki, "systems")
	if err := os.Chmod(systems, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(systems, 0o700) })
	code, out, errOut = evaMD(t, "maintain")
	if code != 1 || strings.Contains(out, "code status") || !strings.Contains(errOut, "eva-md: code status: ") {
		t.Fatalf("an unreadable systems folder: %d %q %q", code, out, errOut)
	}
}

// A path its commit lacks, and a commit off its clone's branch with nothing
// counted since the two forked, are not counted, each with its own words and
// count, and sort with the pages that couldn't be checked; a commit off the
// branch with commits counted since moved. Paths, clones and branches
// from a status file are printed with control, format and space characters
// but a plain space as "?".
func TestCodeLinesForPathsAndBranches(t *testing.T) {
	lacks := coderef.PageStatus{Slug: "orders", AsOf: "2026-10-30", Repos: []coderef.RepoStatus{{Key: "github.com__acme__orders", Commit: "4e2bf4c", Paths: []string{"internal/order"}, State: coderef.Unchanged, Gone: []string{"internal/order"}, Clone: "/src/orders", Branch: "main"}}}
	off := coderef.PageStatus{Slug: "billing", AsOf: "2026-10-30", Repos: []coderef.RepoStatus{{Key: "github.com__acme__billing", Commit: "abcdef1", State: coderef.NotOnBranch, Clone: "/src/billing", Branch: "main"}}}
	unchanged := coderef.PageStatus{Slug: "alpha", AsOf: "2026-10-30", Repos: []coderef.RepoStatus{{Key: "github.com__acme__alpha", Commit: "abcdef1", State: coderef.Unchanged, Clone: "/src/alpha", Branch: "main"}}}
	forged := coderef.RepoStatus{Key: "github.com__acme__orders", Commit: "4e2bf4c", Paths: []string{"internal/orders\x1b[2J\r[user] x", "\xff"}, State: coderef.Moved, Moved: 2, Gone: []string{"a​b"}, Clone: "/src/My Orders\n", Branch: "main\a"}

	if got, want := repoLine(lacks.AsOf, lacks.Repos[0]), "not counted (github.com/acme/orders at 4e2bf4c: internal/order; measured in /src/orders on main; cites a path its commit lacks: internal/order)"; got != want {
		t.Errorf("lacks:\n got %q\nwant %q", got, want)
	}
	if got, want := repoLine(off.AsOf, off.Repos[0]), "not counted (github.com/acme/billing at abcdef1; measured in /src/billing on main; commit abcdef1 is not on this branch, with nothing counted since the commit the two have in common)"; got != want {
		t.Errorf("off:\n got %q\nwant %q", got, want)
	}
	moved := off.Repos[0]
	moved.Moved = 3
	if got, want := repoLine(off.AsOf, moved), "code moved 3 commits since 2026-10-30 (github.com/acme/billing at abcdef1; measured in /src/billing on main; commit abcdef1 is not on this branch, so counted since the commit the two have in common)"; got != want {
		t.Errorf("off and moved:\n got %q\nwant %q", got, want)
	}
	if got, want := repoLine("2026-10-30", forged), "code moved 2 commits since 2026-10-30 (github.com/acme/orders at 4e2bf4c: internal/orders?[2J?[user]?x, ?; measured in /src/My Orders? on main?; gone at HEAD: a?b)"; got != want {
		t.Errorf("forged:\n got %q\nwant %q", got, want)
	}
	r := coderef.Report{Pages: []coderef.PageStatus{unchanged, lacks, off}}
	if got, want := codeSummary(r, "2026-10-31"), "3 of 3 system pages tied to code: 0 moved, 1 unchanged, 0 not checked; 1 cites a path its commit lacks; 1 cites a commit off its clone's branch"; got != want {
		t.Errorf("summary:\n got %q\nwant %q", got, want)
	}
	var order []string
	for _, p := range byMovement(r.Pages) {
		order = append(order, p.Slug)
	}
	if want := []string{"billing", "orders", "alpha"}; !slices.Equal(order, want) {
		t.Errorf("order %q, want %q", order, want)
	}
}
