package mcp

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/heyNag/eva-md/internal/coderef"
	"github.com/heyNag/eva-md/internal/config"
	"github.com/heyNag/eva-md/internal/identity"
	"github.com/heyNag/eva-md/internal/inbox"
	"github.com/heyNag/eva-md/internal/okfadapter"
	"github.com/heyNag/eva-md/internal/searchindex"
	"github.com/heyNag/eva-md/internal/systempage"
	"github.com/heyNag/eva-md/internal/threadpage"
	"github.com/heyNag/eva-md/internal/wikiinit"
)

// fixture is a home with a config naming a wiki, and a git repo outside the
// home, the project of the folder the server started in.
type fixture struct {
	s       Server
	repo    string
	project identity.Project
	c       config.Config
	now     time.Time // when the fixture was made, which its planted dates count from
}

func newFixture(t *testing.T) fixture {
	t.Helper()
	d := isolated(t)
	writeFile(t, os.Getenv("GIT_CONFIG_GLOBAL"), "[user]\n\tname = Test\n\temail = test@example.com\n[init]\n\tdefaultBranch = main\n[commit]\n\tgpgsign = false\n")
	repo := t.TempDir()
	for _, args := range [][]string{{"init", "-q"}, {"remote", "add", "origin", "git@github.com:team/orders.git"}, {"commit", "-q", "--allow-empty", "-m", "start"}} {
		if out, err := exec.Command("git", append([]string{"-C", repo}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	p, err := identity.Resolve(context.Background(), repo)
	if err != nil || p.Slug != "orders" || p.Branch != "main" {
		t.Fatalf("project %+v %v", p, err)
	}
	c := config.Default()
	c.WikiPath = filepath.Join(os.Getenv("HOME"), "wiki")
	f := fixture{s: Server{Host: "codex", Dirs: d, Version: "test", Getwd: repo, beatWait: time.Millisecond}, repo: repo, project: p, c: c, now: time.Now()}
	f.save(t)
	return f
}

// save writes the fixture's config.
func (f fixture) save(t *testing.T) {
	t.Helper()
	if err := config.Save(f.s.Dirs.ConfigFile(), f.c); err != nil {
		t.Fatal(err)
	}
}

// say appends one entry of the session sid to the project's inbox.
func (f fixture) say(t *testing.T, project, sid string, at time.Time, role, kind, text string) {
	t.Helper()
	e := inbox.Entry{V: 1, TS: at.Format(time.RFC3339), Host: "codex", Session: sid, Role: role, Kind: kind, Text: text, Project: project, Branch: "main", Origin: "hook"}
	if err := (inbox.Store{Root: f.s.Dirs.Inbox()}).Append(e); err != nil {
		t.Fatal(err)
	}
}

// day is a day before the fixture's day, in the form a Timeline group has.
// It counts from when the fixture was made, so a test that runs across
// midnight plants and asks for the same days.
func (f fixture) day(daysAgo int) string { return f.now.AddDate(0, 0, -daysAgo).Format(time.DateOnly) }

// asOf is the as_of of the thread page's Now that wiki plants: two hours
// before the fixture was made.
func (f fixture) asOf() time.Time { return f.now.Add(-2 * time.Hour) }

// wiki makes the wiki: the project's now page and thread page, with a Now
// and a Timeline, both rules pages, a system page of the project and one
// of another, and the map.
func (f fixture) wiki(t *testing.T) {
	t.Helper()
	if err := okfadapter.Init(f.c.WikiPath); err != nil {
		t.Fatal(err)
	}
	w, err := okfadapter.Open(f.c.WikiPath, f.s.Dirs.Locks())
	if err != nil {
		t.Fatal(err)
	}
	now := &okfadapter.Concept{Type: "now", Title: "orders", Status: "stable", Extra: map[string]any{"remote": f.project.Remote}, Body: "[main](threads/main.md)"}
	at := f.asOf()
	page := threadpage.Page{Title: "orders · main", Now: threadpage.Now{Summary: threadpage.Placeholder}}
	page, _ = threadpage.Apply(page, threadpage.Update{Date: f.day(1), Summary: "The export runs nightly.", Ops: []threadpage.Op{
		{Kind: "next", Action: "add", Text: "Wire the retry queue", Tag: "stated", Refs: []string{"s:mcpcall-#1"}, At: at},
		{Kind: "question", Action: "add", Text: "Keep the old exporter?", Tag: "suggested", Refs: []string{"s:mcpcall-#2"}},
		{Kind: "decision", Action: "add", Text: "Store the quartz ledger as append-only JSONL", Tag: "stated", Refs: []string{"s:mcpcall-#1"}, At: at},
	}, Events: []threadpage.Event{{Kind: "decision", Text: "Store the quartz ledger as append-only JSONL", Tag: "stated", Refs: []string{"s:mcpcall-#1"}}}})
	thread := &okfadapter.Concept{Type: "thread", Title: page.Title, Status: "stable", Extra: map[string]any{"branch": "main", "as_of": at.Format(time.RFC3339)}, Body: threadpage.Render(page)}
	for id, c := range map[string]*okfadapter.Concept{"projects/orders/now": now, "projects/orders/threads/main": thread} {
		if err := w.Create(id, c, "eva-md/test"); err != nil {
			t.Fatal(err)
		}
	}
	for id, general := range map[string]bool{"general/rules": true, "projects/orders/rules": false} {
		rules, _ := threadpage.ApplyRules(threadpage.RulesPage{Title: "rules"}, []threadpage.RuleOp{{General: general, Action: "add", Text: "Never push on Fridays", Tag: "stated", Refs: []string{"s:0000aaaa#1"}, At: at}}, general)
		if err := w.Create(id, &okfadapter.Concept{Type: "rules", Title: "rules", Status: "stable", Body: threadpage.RenderRules(rules)}, "eva-md/test"); err != nil {
			t.Fatal(err)
		}
	}
	writeFile(t, filepath.Join(f.c.WikiPath, "systems", "orders-api.md"), "---\ntype: system\ntitle: Orders API\ndescription: Takes orders and hands them to billing\nas_of: \""+f.day(2)+"\"\n---\n\n## Summary\nThe orders API takes the cobalt queue's orders.\n\n## Depends on\n- [Billing](billing.md)\n\n## Unverified\nPeak load is about 40 orders a second.\n")
	writeFile(t, filepath.Join(f.c.WikiPath, "systems", "billing.md"), "---\ntype: system\ntitle: Billing\ndescription: Invoices\nas_of: \""+f.day(2)+"\"\n---\n\n## Summary\nBilling sends invoices.\n")
	writeFile(t, filepath.Join(f.c.WikiPath, "systems", "map.md"), "---\ntype: map\ntitle: System map\n---\n\n# System map\n\n"+
		"- [Billing](billing.md): Invoices · repos: github.com__team__billing · as of "+f.day(2)+"\n"+
		"- [Orders API](orders-api.md): Takes orders and hands them to billing · repos: "+f.project.Key+" · as of "+f.day(2)+"\n")
}

// call runs one tool call, as the worker does, without the protocol.
func (f fixture) call(t *testing.T, name string, args map[string]any) result {
	t.Helper()
	raw, _ := json.Marshal(args)
	var a arguments
	if err := json.Unmarshal(raw, &a); err != nil {
		t.Fatal(err)
	}
	s := f.s
	s.Now, s.Getenv = time.Now, os.Getenv
	srv := &server{Server: s, base: context.Background(), start: time.Now()}
	ctx, cancel := context.WithTimeout(context.Background(), srv.deadline(name))
	defer cancel()
	return srv.handler(name)(ctx, srv, a)
}

// answer runs a call that must succeed and returns its text.
func (f fixture) answer(t *testing.T, name string, args map[string]any) string {
	t.Helper()
	r := f.call(t, name, args)
	if r.IsError || len(r.Content) != 1 {
		t.Fatalf("%s %v: %+v", name, args, r)
	}
	return r.Content[0].Text
}

// A planted word is found in a chat and on a page, each with a ref that
// eva_show opens under its label.
func TestSearchFindsWhatShowOpens(t *testing.T) {
	f := newFixture(t)
	f.wiki(t)
	f.say(t, f.project.Key, "mcpcall-session", time.Now().Add(-time.Hour), "user", "prompt", "Remember the tangerine protocol.")
	out := f.answer(t, "eva_search", map[string]any{"terms": []string{"tangerine"}})
	ref := regexp.MustCompile(`s:mcpcall-#\d+`).FindString(out)
	if !strings.HasPrefix(out, "eva.md search: ") || ref == "" || !strings.Contains(out, "\nScanned 1 file (") {
		t.Fatalf("search:\n%s", out)
	}
	if got := f.answer(t, "eva_show", map[string]any{"ref": ref}); !strings.HasPrefix(got, showLabel+"\n[user] ") || !strings.Contains(got, "Remember the tangerine protocol.") {
		t.Fatalf("show %s:\n%s", ref, got)
	}
	out = f.answer(t, "eva_search", map[string]any{"terms": []string{"quartz", "ledger"}})
	ref = "projects/orders/threads/main#" + f.day(1)
	if !strings.Contains(out, " · "+ref+" · decision: Store the quartz ledger as append-only JSONL") {
		t.Fatalf("page hit:\n%s", out)
	}
	if got := f.answer(t, "eva_show", map[string]any{"ref": ref}); !strings.HasPrefix(got, showLabel+"\n### "+f.day(1)+"\n") || !strings.Contains(got, "quartz ledger") {
		t.Fatalf("show %s:\n%s", ref, got)
	}
	if out := f.answer(t, "eva_search", map[string]any{"terms": []string{"cobalt"}}); !strings.Contains(out, "[system] ") || !strings.Contains(out, "systems/orders-api#Summary") {
		t.Fatalf("system hit:\n%s", out)
	}
}

// all, days, limit and cwd are honoured, and a folder outside git, or an
// unknown one, says it searched the general area.
func TestSearchArguments(t *testing.T) {
	f := newFixture(t)
	now := time.Now()
	f.say(t, f.project.Key, "mcpcall-session", now.Add(-time.Hour), "user", "prompt", "The amber switch, first.")
	f.say(t, f.project.Key, "mcpcall-session", now.Add(-50*time.Minute), "user", "prompt", "The amber switch, second.")
	f.say(t, f.project.Key, "mcpcall-session", now.AddDate(0, 0, -200), "user", "prompt", "The violet switch, long ago.")
	f.say(t, "github.com__team__other", "other-session", now.Add(-time.Hour), "user", "prompt", "The amber switch, elsewhere.")
	if out := f.answer(t, "eva_search", map[string]any{"terms": []string{"amber"}, "limit": 1}); !strings.Contains(out, "\n1 more; refine with other terms\n") || strings.Contains(out, "elsewhere") {
		t.Fatalf("limit:\n%s", out)
	}
	if out := f.answer(t, "eva_search", map[string]any{"terms": []string{"amber"}, "all": true}); !strings.Contains(out, "elsewhere") || !strings.Contains(out, "github.com__team__other") {
		t.Fatalf("all:\n%s", out)
	}
	if out := f.answer(t, "eva_search", map[string]any{"terms": []string{"violet"}}); !strings.HasPrefix(out, "No matches.") {
		t.Fatalf("90 days:\n%s", out)
	}
	if out := f.answer(t, "eva_search", map[string]any{"terms": []string{"violet"}, "days": 0}); !strings.Contains(out, "long ago") || !strings.Contains(out, ", all dates, ") {
		t.Fatalf("days 0:\n%s", out)
	}
	outside := t.TempDir()
	out := f.answer(t, "eva_search", map[string]any{"terms": []string{"amber"}, "cwd": outside})
	if !strings.HasPrefix(out, "scope: general area (no git project at "+outside+"); pass cwd=<project folder>, or all=true\nNo matches.") {
		t.Fatalf("cwd outside git:\n%s", out)
	}
	if out := f.answer(t, "eva_search", map[string]any{"terms": []string{"amber"}, "cwd": f.repo}); !strings.Contains(out, "amber switch, second") || strings.HasPrefix(out, "scope:") {
		t.Fatalf("cwd:\n%s", out)
	}
	f.s.Getwd = ""
	if out := f.answer(t, "eva_search", map[string]any{"terms": []string{"amber"}}); !strings.HasPrefix(out, "scope: general area (the folder the host started eva.md in is unknown); pass cwd=<project folder>, or all=true\n") {
		t.Fatalf("no folder:\n%s", out)
	}
}

// Each bound is refused as an error that names it, so the model can
// correct the call.
func TestArgumentBounds(t *testing.T) {
	f := newFixture(t)
	for _, tc := range []struct {
		tool string
		args map[string]any
		want string
	}{
		{"eva_search", map[string]any{}, "terms must be"},
		{"eva_search", map[string]any{"terms": "amber"}, "terms must be"},
		{"eva_search", map[string]any{"terms": []string{}}, "terms must be"},
		{"eva_search", map[string]any{"terms": []string{" "}}, "terms must be"},
		{"eva_search", map[string]any{"terms": strings.Split("a b c d e f g h i j k l m n o p q", " ")}, "terms must be"},
		{"eva_search", map[string]any{"terms": []string{strings.Repeat("x", 257)}}, "terms must be"},
		{"eva_search", map[string]any{"terms": []string{"x"}, "limit": 0}, "limit must be"},
		{"eva_search", map[string]any{"terms": []string{"x"}, "limit": 51}, "limit must be"},
		{"eva_search", map[string]any{"terms": []string{"x"}, "limit": 1.5}, "limit must be"},
		{"eva_search", map[string]any{"terms": []string{"x"}, "days": -1}, "days must be"},
		{"eva_search", map[string]any{"terms": []string{"x"}, "all": "yes"}, "all must be true or false"},
		{"eva_search", map[string]any{"terms": []string{"x"}, "cwd": "relative/folder"}, "cwd must be"},
		{"eva_search", map[string]any{"terms": []string{"x"}, "cwd": filepath.Join(f.repo, "missing")}, "cwd must be"},
		{"eva_status", map[string]any{"cwd": 7}, "cwd must be"},
		{"eva_show", map[string]any{}, "ref must be"},
		{"eva_show", map[string]any{"ref": "systems/map\nsystems/x"}, "ref must be"},
		{"eva_show", map[string]any{"ref": strings.Repeat("x", 1025)}, "ref must be"},
		{"eva_show", map[string]any{"ref": "projects/orders/threads/main#Timeline", "last": 0}, "last must be"},
		{"eva_show", map[string]any{"ref": "projects/orders/threads/main#Timeline", "since": "yesterday"}, "since must be"},
		{"eva_show", map[string]any{"ref": "projects/orders/threads/main#Now", "last": 2}, "last and since apply only to a page's #Timeline"},
		{"eva_show", map[string]any{"ref": "s:abcdefgh#1", "since": f.day(3)}, "last and since apply only to a page's #Timeline"},
	} {
		r := f.call(t, tc.tool, tc.args)
		if !r.IsError || !strings.HasPrefix(r.Content[0].Text, tc.want) {
			t.Errorf("%s %v: %+v", tc.tool, tc.args, r)
		}
	}
	// Absent and null are the defaults; 10.0 is a whole number.
	if r := f.call(t, "eva_search", map[string]any{"terms": []string{"x"}, "limit": 10.0, "days": nil, "all": nil, "cwd": nil}); r.IsError {
		t.Fatal(r)
	}
}

// An excluded folder gets one sentence, which is no error: there is
// nothing to fix.
func TestExcludedFolder(t *testing.T) {
	f := newFixture(t)
	f.say(t, f.project.Key, "mcpcall-session", time.Now().Add(-time.Hour), "user", "prompt", "Remember the tangerine protocol.")
	f.c.Exclude = []string{f.repo}
	f.save(t)
	for _, tc := range []struct {
		tool string
		args map[string]any
		want string
	}{
		{"eva_search", map[string]any{"terms": []string{"tangerine"}}, excluded},
		{"eva_search", map[string]any{"terms": []string{"tangerine"}, "all": true}, excluded},
		{"eva_status", nil, "This folder is excluded in eva-md's config; eva.md keeps nothing for it."},
	} {
		if r := f.call(t, tc.tool, tc.args); r.IsError || r.Content[0].Text != tc.want {
			t.Fatalf("%s: %+v", tc.tool, r)
		}
	}
}

// Without a config the tools are on and search the inbox, saying no wiki
// is set up; an unreadable config makes every call an error that names
// the fix.
func TestMissingAndUnreadableConfig(t *testing.T) {
	f := newFixture(t)
	f.say(t, f.project.Key, "mcpcall-session", time.Now().Add(-time.Hour), "user", "prompt", "Remember the tangerine protocol.")
	if err := os.Remove(f.s.Dirs.ConfigFile()); err != nil {
		t.Fatal(err)
	}
	ss := serve(t, f.s)
	ss.send(request(1, "tools/list", nil))
	if r := ss.next(); string(r.Result) != string(toolsJSON()) {
		t.Fatalf("tools without a config: %s", r.line)
	}
	if out := f.answer(t, "eva_search", map[string]any{"terms": []string{"tangerine"}}); !strings.Contains(out, "tangerine protocol") || strings.Contains(out, "thread page") {
		t.Fatalf("search without a config:\n%s", out)
	}
	if out := f.answer(t, "eva_status", nil); !strings.Contains(out, "\nthread page: no wiki is set up\npending update: no wiki is set up, so no update runs\n") {
		t.Fatalf("status without a config:\n%s", out)
	}
	if r := f.call(t, "eva_show", map[string]any{"ref": "systems/map"}); !r.IsError || r.Content[0].Text != "eva-md: no wiki is set up" {
		t.Fatalf("show without a wiki: %+v", r)
	}
	writeFile(t, f.s.Dirs.ConfigFile(), "exclude = [")
	for _, name := range []string{"eva_search", "eva_show", "eva_status"} {
		if r := f.call(t, name, map[string]any{"terms": []string{"x"}, "ref": "systems/map"}); !r.IsError || r.Content[0].Text != "eva-md's configuration can't be read; run eva-md doctor" {
			t.Fatalf("%s with an unreadable config: %+v", name, r)
		}
	}
}

// eva_show prints a whole system page, the Now, the Timeline with last and
// since, a day and a section by its anchor, each under the label, and after
// a system page the system pages that link to it.
func TestShowPagesAndSections(t *testing.T) {
	f := newFixture(t)
	f.wiki(t)
	page, err := os.ReadFile(filepath.Join(f.c.WikiPath, "systems", "orders-api.md"))
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		args       map[string]any
		want, miss string
	}{
		{map[string]any{"ref": "systems/orders-api"}, showLabel + "\n" + strings.TrimSuffix(string(page), "\n"), "linked from"},
		{map[string]any{"ref": "systems/billing"}, "Billing sends invoices.\n\neva.md: systems/billing: linked from systems/orders-api", ""},
		{map[string]any{"ref": "systems/orders-api#Depends-on"}, showLabel + "\n## Depends on\n- [Billing](billing.md)", "## Unverified"},
		{map[string]any{"ref": "projects/orders/threads/main#Now"}, "Wire the retry queue", "### " + f.day(1)},
		{map[string]any{"ref": "projects/orders/threads/main#Timeline", "last": 1}, "### " + f.day(1), ""},
		{map[string]any{"ref": "projects/orders/threads/main#Timeline", "since": f.day(0)}, showLabel + "\n## Timeline", "quartz"},
		{map[string]any{"ref": "projects/orders/threads/main#" + f.day(1)}, "Store the quartz ledger", ""},
	} {
		got := f.answer(t, "eva_show", tc.args)
		if !strings.HasPrefix(got, showLabel+"\n") || !strings.Contains(got, tc.want) || tc.miss != "" && strings.Contains(got, tc.miss) {
			t.Fatalf("%v:\n%s", tc.args, got)
		}
	}
	if r := f.call(t, "eva_show", map[string]any{"ref": "systems/nothing-here"}); !r.IsError || r.Content[0].Text != "eva-md: no such page or file; eva_search finds refs, systems/index lists the system pages and knowledge/map the knowledge pages" {
		t.Fatalf("a missing page: %+v", r)
	}
	// A knowledge page, its Statement and its History, with no line of the
	// pages that link to it.
	writeFile(t, filepath.Join(f.c.WikiPath, "knowledge", "k1.md"), "---\ntype: knowledge\ntitle: Inbox format\ndescription: The inbox stores JSONL per session and day\nstatus: stable\n"+
		"as_of: \"2026-10-28\"\nkind: \"decision\"\nproject: \""+f.project.Key+"\"\nscope: \"project\"\nstate: \"active\"\n---\n\n"+
		"## Statement\nThe inbox stores JSONL per session and day [stated] s:4b5c6d7e#3\n\n"+
		"## History\n- ~~The inbox stores JSONL per session~~ → The inbox stores JSONL per session and day (superseded 2026-10-28) [stated] s:4b5c6d7e#3\n\n"+
		"Part of the [knowledge map](map.md).\n")
	for ref, want := range map[string]string{
		"knowledge/k1":           "\n## Statement\nThe inbox stores JSONL per session and day [stated] s:4b5c6d7e#3\n\n## History\n",
		"knowledge/k1#Statement": showLabel + "\n## Statement\nThe inbox stores JSONL per session and day [stated] s:4b5c6d7e#3",
		"knowledge/k1#History":   showLabel + "\n## History\n- ~~The inbox stores JSONL per session~~ → The inbox stores JSONL per session and day (superseded 2026-10-28) [stated] s:4b5c6d7e#3\n\nPart of the [knowledge map](map.md).",
	} {
		if got := f.answer(t, "eva_show", map[string]any{"ref": ref}); !strings.Contains(got, want) || strings.Contains(got, "linked from") || strings.HasSuffix(ref, "#Statement") && got != want {
			t.Fatalf("%s:\n%s", ref, got)
		}
	}
}

// A page over 24 KB is cut at a line end, with a note that names the
// sections to ask for instead.
func TestShowCutsALongPage(t *testing.T) {
	f := newFixture(t)
	f.wiki(t)
	var b strings.Builder
	b.WriteString("---\ntype: thread\ntitle: orders · long\n---\n\n## Now\n<!-- eva:now:start -->\nThe long thread.\n<!-- eva:now:end -->\n\n## Timeline\n")
	for i := 0; b.Len() < 60<<10; i++ {
		fmt.Fprintf(&b, "\n### %s\n- finding: the nightly export took %d minutes on run %d [observed] s:longpage#%d\n", time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC).AddDate(0, 0, i).Format(time.DateOnly), i%60, i, i)
	}
	b.WriteString("\n## Notes\nKept by hand.\n\n## Depends on\nNothing.\n")
	writeFile(t, filepath.Join(f.c.WikiPath, "projects", "orders", "threads", "long.md"), b.String())
	got := f.answer(t, "eva_show", map[string]any{"ref": "projects/orders/threads/long"})
	note := regexp.MustCompile(`\n\[eva\.md: output cut at (\d+) KB, (\d+) KB left; ask for one section of projects/orders/threads/long: #Now, #Timeline, #<YYYY-MM-DD>, #Notes or #Depends-on\]$`).FindStringSubmatch(got)
	if len(got) > showCap+len(showLabel)+1 || note == nil || note[1] != "23" || note[2] != "37" {
		t.Fatalf("%d bytes, note %q:\n%s", len(got), note, got[max(len(got)-400, 0):])
	}
	kept := strings.TrimPrefix(got[:len(got)-len(note[0])], showLabel+"\n")
	if !strings.HasPrefix(b.String(), kept+"\n") {
		t.Fatal("the page was not cut at a line end")
	}
}

// The knowledge map past 24 KB, one list with no section to ask for, is cut
// with a note that says how to get the rest: the command that prints it
// whole, and search.
func TestShowCutsALongKnowledgeMap(t *testing.T) {
	f := newFixture(t)
	f.wiki(t)
	var b strings.Builder
	b.WriteString("---\ntype: map\ntitle: Knowledge map\ndescription: Every knowledge page, one line each\n---\n\n# Knowledge map\n\n")
	for i := 1; b.Len() < 40<<10; i++ {
		fmt.Fprintf(&b, "- [Nightly export fact %d](k%d.md): fact · this project · %s · as of 2026-10-28\n", i, i, f.project.Key)
	}
	writeFile(t, filepath.Join(f.c.WikiPath, "knowledge", "map.md"), b.String())
	hint := "the newest pages are in the rest; " + filepath.Join(f.s.Dirs.Share, "bin", "eva-md") + " show 'knowledge/map' prints it whole, and eva_search finds a knowledge page by its words]"
	for _, ref := range []string{"knowledge/map", "knowledge/map.md"} {
		got := f.answer(t, "eva_show", map[string]any{"ref": ref})
		if len(got) > showCap+len(showLabel)+1 || !strings.Contains(got, "\n- [Nightly export fact 1](k1.md): ") || !strings.HasSuffix(got, " KB left; "+strings.Replace(hint, "'knowledge/map'", "'"+ref+"'", 1)) {
			t.Fatalf("%s: %d bytes:\n%s", ref, len(got), got[max(len(got)-400, 0):])
		}
	}
}

// A system page system write saved at its own 24 KB cap comes back whole,
// with the lines the write added, though it is over the show cap.
func TestShowGivesASystemPageAtItsCapWhole(t *testing.T) {
	f := newFixture(t)
	home := os.Getenv("HOME")
	if out, err := exec.Command("git", "init", "--bare", "--quiet", filepath.Join(home, "remote.git")).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
	if err := os.Remove(f.s.Dirs.ConfigFile()); err != nil {
		t.Fatal(err)
	}
	if _, err := wikiinit.Run(context.Background(), wikiinit.Options{Remote: filepath.Join(home, "remote.git"), Dir: f.c.WikiPath, Dirs: f.s.Dirs, Version: "test", NoPush: true}); err != nil {
		t.Fatal(err)
	}
	c, err := config.Load(f.s.Dirs.ConfigFile())
	if err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	b.WriteString("---\ntype: system\ntitle: Ledger\ndescription: Keeps the ledger\nstatus: stable\ngenerated:\n  by: codex/gpt-5.5-codex\n" +
		"sources:\n- resource: https://docs.example.com/eng/ledger v3\nrepos:\n  - " + f.project.Key + "\nas_of: \"" + f.day(1) + "\"\nstate: active\n---\n\n" +
		"## Summary\nThe ledger keeps every order.\n\n## Responsibilities\n")
	for i := 0; b.Len() < 24<<10-100; i++ {
		fmt.Fprintf(&b, "- The ledger keeps entry %d of the nightly export.\n", i)
	}
	b.WriteString("- The ledger keeps the rest" + strings.Repeat(".", 24<<10-b.Len()-len("- The ledger keeps the rest")-1) + "\n")
	if b.Len() != 24<<10 {
		t.Fatalf("the page is %d bytes", b.Len())
	}
	if _, err := systempage.Write(context.Background(), systempage.Options{Config: c, Dirs: f.s.Dirs, Version: "test"}, "ledger", false, strings.NewReader(b.String())); err != nil {
		t.Fatal(err)
	}
	saved, err := os.ReadFile(filepath.Join(c.WikiPath, "systems", "ledger.md"))
	if err != nil {
		t.Fatal(err)
	}
	if len(saved) <= showCap {
		t.Fatalf("the saved page is %d bytes, within the show cap", len(saved))
	}
	if got := f.answer(t, "eva_show", map[string]any{"ref": "systems/ledger"}); got != showLabel+"\n"+string(saved) {
		t.Fatalf("%d bytes:\n%s", len(got), got[max(len(got)-400, 0):])
	}
}

// A search of 50 long hits is cut to 24 KB at a line end, keeping its
// trailer as the last line.
func TestSearchIsCutAtItsCap(t *testing.T) {
	f := newFixture(t)
	now := time.Now()
	for i := 0; i < 50; i++ {
		f.say(t, f.project.Key, "mcpcall-session", now.Add(time.Duration(i-60)*time.Minute), "user", "prompt", "tangerine "+strings.Repeat("蜜柑の記録を残す", 30))
	}
	out := f.answer(t, "eva_search", map[string]any{"terms": []string{"tangerine"}, "limit": 50})
	lines := strings.Split(out, "\n")
	trailer, note := lines[len(lines)-1], lines[len(lines)-2]
	if len(out) > searchCap || !strings.HasPrefix(trailer, "Scanned 1 file (") || !regexp.MustCompile(`^\[eva\.md: output cut at \d+ KB, \d+ KB left; a lower limit or more terms show fewer hits\]$`).MatchString(note) {
		t.Fatalf("%d bytes:\n%s", len(out), out[max(len(out)-600, 0):])
	}
	for _, l := range lines[1 : len(lines)-2] {
		if !strings.HasPrefix(l, "[user] ") || !strings.HasSuffix(l, "…") && !strings.HasSuffix(l, "残す") {
			t.Fatalf("a hit line was cut: %q", l)
		}
	}
}

// eva_status says, in under 4 KB, what eva.md holds for the project: the
// thread page with its Now and rules, the pending update, the project's
// systems, the hooks, the updates, the notice and what to read next.
func TestStatus(t *testing.T) {
	f := newFixture(t)
	if out := f.answer(t, "eva_status", nil); !strings.HasPrefix(out, "scope: "+f.project.Key+" · branch main · thread projects/orders/threads/main\nthread page: the wiki is unreadable\n") || !strings.Contains(out, "\nsystems: no system pages\nknowledge: the wiki is unreadable\n") {
		t.Fatalf("no wiki yet:\n%s", out)
	}
	f.wiki(t)
	now := time.Now()
	file := (inbox.Store{Root: f.s.Dirs.Inbox()}).Path(f.project.Key, "codex", "mcpcall-session")
	f.say(t, f.project.Key, "mcpcall-session", now.Add(-time.Hour), "user", "prompt", strings.Repeat("Twelve kilobytes of chat. ", 470))
	info, err := os.Stat(file)
	if err != nil || info.Size() < 12<<10 || info.Size() > 13<<10 {
		t.Fatalf("the session file is %d bytes: %v", info.Size(), err)
	}
	installedFor(t, f.s.Dirs, "codex")
	beatAt(t, f.s.Dirs, "codex", "SessionStart", now.Add(-3*time.Hour))
	writeFile(t, filepath.Join(f.s.Dirs.State, "needs-trust.json"), `{"host":"codex","needs_trust":true}`)
	out := f.answer(t, "eva_status", nil)
	asOf := f.asOf().Format("2006-01-02 15:04")
	want := []string{
		"scope: " + f.project.Key + " · branch main · thread projects/orders/threads/main",
		"thread page: projects/orders/threads/main, Now as of " + asOf + " (1 decision in force, 1 next step, 1 open question); standing rules: 1 for this project, 1 for all projects",
		"pending update: 1 session, about 12 KB unprocessed",
		"systems for this project: orders-api (the map lists 2)",
		"knowledge: none yet",
		"hooks: claude-code not installed, codex ran 3 hours ago; Codex hooks await approval in /hooks",
		"updates: claude-code not installed, codex none today or yesterday; no daily commit yet",
		"notice: review its hooks in Codex /hooks; run eva-md doctor to verify.",
		"read: eva_show projects/orders/threads/main#Now · eva_show projects/orders/threads/main#Timeline · eva_show systems/index",
	}
	if out != strings.Join(want, "\n") || len(out) > statusCap {
		t.Fatalf("status:\n%s\nwant:\n%s", out, strings.Join(want, "\n"))
	}
	f.c.Updates = false
	f.save(t)
	if out := f.answer(t, "eva_status", nil); !strings.Contains(out, "\npending update: 1 session, about 12 KB unprocessed; updates are off (updates = false)\n") || !strings.Contains(out, "\nupdates: off (updates = false); no daily commit yet\n") {
		t.Fatalf("updates off:\n%s", out)
	}
	outside := t.TempDir()
	if out := f.answer(t, "eva_status", map[string]any{"cwd": outside}); !strings.HasPrefix(out, "scope: general area (no git project at "+outside+"); pass cwd=<project folder>\nthread page: none yet for this branch; standing rules: 0 for this project, 1 for all projects\n") {
		t.Fatalf("general area:\n%s", out)
	}
}

// eva_status says how far the code each of the project's systems cites
// moved, as the prompt pointers say it, from the code status; a status taken
// at other commits than the map's line cites says nothing of it.
// eva_status counts the knowledge pages the recall index lists and the
// constraints that hold for the project, and names the knowledge map among
// what to read once it exists.
func TestStatusCountsKnowledge(t *testing.T) {
	f := newFixture(t)
	f.wiki(t)
	row := func(id, kind, scope, project string) string {
		return strings.Join([]string{id, kind, "stated", scope, project, "2026-10-28", "", "", "A title", "A statement", ""}, "\t") + "\n"
	}
	writeFile(t, filepath.Join(f.c.WikiPath, ".eva-md", "knowledge.tsv"), "# eva-md knowledge index 1\n"+
		row("k1", "constraint", "project", f.project.Key)+row("k2", "constraint", "all", "github.com__team__billing")+
		row("k3", "constraint", "project", "github.com__team__billing")+row("k4", "fact", "project", f.project.Key))
	out := f.answer(t, "eva_status", nil)
	if !strings.Contains(out, "\nknowledge: 4 pages, 2 constraints for this project\nhooks: ") || strings.Contains(out, "knowledge/map") {
		t.Fatalf("knowledge:\n%s", out)
	}
	writeFile(t, filepath.Join(f.c.WikiPath, "knowledge", "map.md"), "---\ntype: map\ntitle: Knowledge map\n---\n\n# Knowledge map\n")
	if out := f.answer(t, "eva_status", nil); !strings.HasSuffix(out, " · eva_show systems/index · eva_show knowledge/map") {
		t.Fatalf("the map:\n%s", out)
	}
	writeFile(t, filepath.Join(f.c.WikiPath, ".eva-md", "knowledge.tsv"), "# eva-md knowledge index 1\n"+row("k4", "fact", "project", f.project.Key))
	if out := f.answer(t, "eva_status", nil); !strings.Contains(out, "\nknowledge: 1 page, 0 constraints for this project\n") {
		t.Fatalf("one page:\n%s", out)
	}
}

func TestStatusSaysHowFarTheCodeMoved(t *testing.T) {
	f := newFixture(t)
	f.wiki(t)
	writeFile(t, filepath.Join(f.c.WikiPath, "systems", "map.md"), "---\ntype: map\ntitle: System map\n---\n\n# System map\n\n"+
		"- [Billing](billing.md): Invoices · repos: github.com__team__billing · as of "+f.day(2)+"\n"+
		"- [Orders API](orders-api.md): Takes orders and hands them to billing · repos: "+f.project.Key+" · code: "+f.project.Key+"@4e2bf4c:internal/orders · as of "+f.day(2)+"\n")
	plant := func(commit string) {
		t.Helper()
		r := coderef.Report{V: 1, At: f.now.Format(time.RFC3339), Pages: []coderef.PageStatus{{Slug: "orders-api", AsOf: f.day(2),
			Repos: []coderef.RepoStatus{{Key: f.project.Key, Commit: commit, State: coderef.Moved, Moved: 3}}}}}
		if err := coderef.Save(f.s.Dirs.State, r); err != nil {
			t.Fatal(err)
		}
	}
	plant("4e2bf4c")
	if out := f.answer(t, "eva_status", nil); !strings.Contains(out, "\nsystems for this project: orders-api (code moved 3 commits since "+f.day(2)+") (the map lists 2)\n") {
		t.Fatalf("moved:\n%s", out)
	}
	plant("9c1d2e3")
	if out := f.answer(t, "eva_status", nil); !strings.Contains(out, "\nsystems for this project: orders-api (the map lists 2)\n") {
		t.Fatalf("other commits:\n%s", out)
	}
}

// tree hashes every file and folder under dir: its path, mode, size, time
// and bytes.
func tree(t *testing.T, dir string) string {
	t.Helper()
	h := sha256.New()
	err := filepath.WalkDir(dir, func(path string, e fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := e.Info()
		if err != nil {
			return err
		}
		fmt.Fprintf(h, "%s %v %d %d\n", path, info.Mode(), info.Size(), info.ModTime().UnixNano())
		if info.Mode().IsRegular() {
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			h.Write(data)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return hex.EncodeToString(h.Sum(nil))
}

// The server writes nothing: 30 calls leave the home folder, the inbox,
// the wiki and the state exactly as they were, with over 4 MiB of the inbox
// in scope unindexed (searchindex.TopUpMin), enough that eva-md search would
// index it for the next search.
func TestServerWritesNothing(t *testing.T) {
	f := newFixture(t)
	f.wiki(t)
	f.say(t, f.project.Key, "mcpcall-session", time.Now().Add(-time.Hour), "user", "prompt", "Remember the tangerine protocol.")
	filler := strings.Repeat("filler words that pad the inbox out to its size; ", 20)
	var backlog []byte
	for len(backlog) < searchindex.TopUpMin+1<<20 {
		e := inbox.Entry{V: 1, Host: "codex", Session: "backlog", Project: f.project.Key, Branch: "main", TS: time.Now().Add(-2 * time.Hour).Format(time.RFC3339), Role: "user", Kind: "prompt", Text: filler, Origin: "hook"}
		line, err := json.Marshal(e)
		if err != nil {
			t.Fatal(err)
		}
		backlog = append(append(backlog, line...), '\n')
	}
	if err := os.WriteFile((inbox.Store{Root: f.s.Dirs.Inbox()}).Path(f.project.Key, "codex", "backlog"), backlog, 0o600); err != nil {
		t.Fatal(err)
	}
	installedFor(t, f.s.Dirs, "codex")
	beatAt(t, f.s.Dirs, "codex", "SessionStart", time.Now().Add(-time.Hour))
	home := os.Getenv("HOME")
	before := tree(t, home)
	ss := serve(t, f.s)
	ss.send(request(0, "initialize", map[string]any{"protocolVersion": "2025-06-18"}))
	ss.next()
	for i := 1; i <= 10; i++ {
		ss.send(callTool(3*i, "eva_search", map[string]any{"terms": []string{"tangerine"}}),
			callTool(3*i+1, "eva_show", map[string]any{"ref": "projects/orders/threads/main#Now"}),
			callTool(3*i+2, "eva_status", nil))
		for j := 0; j < 3; j++ {
			if res := ss.next().tool(t); res.IsError {
				t.Fatal(res)
			}
		}
	}
	ss.in.Close()
	if code := ss.ended(); code != 0 {
		t.Fatal(code)
	}
	if after := tree(t, home); after != before {
		t.Fatal("the calls changed the home folder")
	}
}
