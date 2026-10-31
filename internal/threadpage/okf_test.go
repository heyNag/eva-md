package threadpage_test

import (
	"fmt"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/heyNag/eva-md/internal/okfadapter"
	tp "github.com/heyNag/eva-md/internal/threadpage"
)

// smuggle is okf's body check (mutate.go), applied to trimmed lines between
// --- lines.
var smuggle = regexp.MustCompile(`(?i)^\s*["']?(verified|governance|generated|type|status|code_refs|stale_after)["']?\s*:`)

func testWiki(t *testing.T) *okfadapter.Wiki {
	t.Helper()
	isolate(t)
	dir := t.TempDir()
	root := filepath.Join(dir, "wiki")
	if err := okfadapter.Init(root); err != nil {
		t.Fatal(err)
	}
	w, err := okfadapter.Open(root, filepath.Join(dir, "locks"))
	if err != nil {
		t.Fatal(err)
	}
	return w
}

func create(t *testing.T, w *okfadapter.Wiki, id, typ, title, desc, body string) {
	t.Helper()
	c := &okfadapter.Concept{Type: typ, Title: title, Description: desc, Status: "stable", Body: body, Extra: map[string]any{"locked": false, "writer": "eva-md/test"}}
	if err := w.Create(id, c, okfadapter.Actor("eva-md", "test")); err != nil {
		t.Fatalf("create %s: %v", id, err)
	}
}

func readBody(t *testing.T, w *okfadapter.Wiki, id string) string {
	t.Helper()
	p, err := w.Read(id)
	if err != nil {
		t.Fatal(err)
	}
	return p.Concept.Body
}

// builtPage is a thread page as two update passes leave it.
func builtPage(project, branch string, seed int) tp.Page {
	r := func(n int) []string { return []string{fmt.Sprintf("s:%08x#%d", seed, n)} }
	at := time.Date(2026, 1, 14, 9, seed, 0, 0, plus1)
	p := tp.Page{Title: project + " · " + branch, Now: tp.Now{Summary: tp.Placeholder}}
	p, _ = tp.Apply(p, tp.Update{
		Date:     "2026-01-14",
		Summary:  "Work on " + branch + " goes well. The okf goldens are next.",
		LastChat: "2026-01-14 · claude-code/claude-opus-5-5 · Set up the branch.",
		Done:     []tp.Item{{Text: "Created the branch", Tag: "observed", Refs: r(2)}},
		Ops: []tp.Op{
			{Kind: "next", Action: "add", Text: "Write the goldens", Tag: "stated", Refs: r(3), At: at},
			{Kind: "next", Action: "add", Text: "Maybe benchmark it", Tag: "suggested", Refs: r(4)},
			{Kind: "question", Action: "add", Text: "Run Validate in CI?", Tag: "inferred", Refs: r(5)},
			{Kind: "decision", Action: "add", Text: "Use okf v0.7.2", Tag: "stated", Refs: r(6), At: at},
			{Kind: "decision", Action: "add", Text: "Pages end in one newline", Tag: "accepted", Refs: r(7), At: at},
		},
		Events: []tp.Event{{Kind: "decision", Text: "Use okf v0.7.2", Tag: "stated", Refs: r(6)}},
	})
	p, _ = tp.Apply(p, tp.Update{
		Date: "2026-01-15",
		Ops: []tp.Op{
			{ID: "n1", Kind: "next", Action: "done", Refs: r(9)},
			{ID: "n2", Kind: "next", Action: "dropped", Reason: "fast enough", Refs: r(10)},
			{ID: "n3", Kind: "question", Action: "answered", Replacement: "yes, on every push", Tag: "stated", Refs: r(11)},
			{ID: "n5", Kind: "decision", Action: "superseded", Replacement: "Pages end in exactly one newline", Tag: "stated", Refs: r(12), At: at.Add(24 * time.Hour)},
		},
		Events: []tp.Event{{Kind: "finding", Text: "okf trims the body's ends", Tag: "observed", Refs: r(13)}},
	})
	p.Other = []string{"## Notes\nKept as written, with a [link](https://example.com/notes)."}
	return p
}

// builtRules is a project's rules page as two update passes leave it, with
// a section a person added.
func builtRules(project string) tp.RulesPage {
	at := time.Date(2026, 1, 14, 9, 0, 0, 0, plus1)
	p := tp.RulesPage{Title: project + " · rules"}
	p, _ = tp.ApplyRules(p, []tp.RuleOp{
		{Action: "add", Text: "Never mock the database in tests", Tag: "stated", Refs: []string{"s:0000aaaa#3"}, At: at},
		{Action: "add", Text: "Run go test -race before calling it done", Tag: "stated", Refs: []string{"s:0000aaaa#5"}, At: at},
	}, false)
	p, _ = tp.ApplyRules(p, []tp.RuleOp{
		{ID: "r1", Action: "edit", Text: "Never mock the database, even in unit tests", Tag: "stated", Refs: []string{"s:0000bbbb#2"}, At: at.Add(time.Hour)},
	}, false)
	p.Other = []string{"## Notes\nRules a person keeps by hand, with a [link](https://example.com/rules)."}
	return p
}

func TestPagesThroughOKF(t *testing.T) {
	w := testWiki(t)
	indexes := map[string]string{
		"index.md":          "---\nokf_version: \"0.2\"\n---\n\n# Eva\n\n" + okfadapter.IndexLine("Eva", "README.md", "Fixture wiki") + "\n" + okfadapter.IndexLine("Projects", "projects/index.md", "Project areas") + "\n",
		"projects/index.md": "# Projects\n\n",
	}
	readme := "# Eva\n"
	models := map[string]tp.Page{}
	rulesModels := map[string]tp.RulesPage{}
	bodies := map[string]string{}
	seed := 0
	for _, project := range []string{"alpha", "beta"} {
		var threads []tp.Thread
		var lines []string
		for _, branch := range map[string][]string{"alpha": {"main", "feature/okf-goldens", "über/日本"}, "beta": {"main", ".hidden", "release/v1"}}[project] {
			seed++
			page := builtPage(project, branch, seed)
			if project == "alpha" && branch == "main" {
				page = realisticPage()
			}
			file := tp.ThreadName(branch)
			id := "projects/" + project + "/threads/" + file
			desc := "Branch " + branch + " of " + project
			body := tp.Render(page)
			create(t, w, id, "thread", project+" · "+branch, desc, body)
			models[id], bodies[id] = page, body
			threads = append(threads, tp.Thread{Branch: branch, File: file, Summary: page.Now.Summary, Active: time.Date(2026, 1, 10+seed, 12, 0, 0, 0, time.UTC)})
			lines = append(lines, okfadapter.IndexLine(project+" · "+branch, file+".md", desc))
		}
		slices.Sort(lines)
		// alpha has a rules page, which only its overview links.
		rules := project == "alpha"
		overview := tp.RenderOverview(project, threads, rules)
		create(t, w, "projects/"+project+"/now", "now", project, "Threads of "+project, overview)
		bodies["projects/"+project+"/now"] = overview
		readme += "\n[" + project + "](projects/" + project + "/now.md)\n"
		indexes["projects/index.md"] += okfadapter.IndexLine(project, project+"/index.md", "Project "+project) + "\n"
		indexes["projects/"+project+"/index.md"] = "# " + project + "\n\n" + okfadapter.IndexLine(project, "now.md", "Threads of "+project) + "\n" + okfadapter.IndexLine("Threads", "threads/index.md", "Threads of "+project) + "\n"
		if rules {
			page := builtRules(project)
			body := tp.RenderRules(page)
			create(t, w, "projects/"+project+"/rules", "rules", project+" · rules", "Standing rules for "+project, body)
			rulesModels["projects/"+project+"/rules"], bodies["projects/"+project+"/rules"] = page, body
			indexes["projects/"+project+"/index.md"] += okfadapter.IndexLine(project+" · rules", "rules.md", "Standing rules for "+project) + "\n"
		}
		indexes["projects/"+project+"/threads/index.md"] = "# Threads\n\n" + strings.Join(lines, "\n") + "\n"
	}
	create(t, w, "README", "guide", "Eva", "Fixture wiki", readme)
	for name, content := range indexes {
		if err := w.WriteFile(name, []byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	for id, body := range bodies {
		got := readBody(t, w, id)
		if got != body {
			t.Fatalf("%s read back differently:\n%q\nwant\n%q", id, got, body)
		}
		if model, ok := rulesModels[id]; ok {
			parsed, err := tp.ParseRules(got)
			if err != nil || !reflect.DeepEqual(parsed, model) || tp.RenderRules(parsed) != got {
				t.Fatalf("%s: ParseRules after okf = %v\n%#v", id, err, parsed)
			}
		}
		model, ok := models[id]
		if !ok {
			continue
		}
		parsed, err := tp.Parse(got)
		if err != nil || !reflect.DeepEqual(parsed, model) || tp.Render(parsed) != got {
			t.Fatalf("%s: Parse after okf = %v\n%#v", id, err, parsed)
		}
	}
	v, err := w.Validate()
	if err != nil {
		t.Fatal(err)
	}
	if len(v.Errors)+len(v.Warnings)+len(v.BrokenLinks)+len(v.Orphans) != 0 || !v.IsConformant || v.ConceptCount != 10 {
		t.Fatalf("Validate: concepts=%d errors=%q warnings=%q broken=%+v orphans=%q", v.ConceptCount, v.Errors, v.Warnings, v.BrokenLinks, v.Orphans)
	}
}

func TestEscaperCorpusThroughOKF(t *testing.T) {
	w := testWiki(t)
	corpus := []string{
		"---", "```go", "~~~", tp.NowStart, tp.NowEnd, "status: x", "verified: true",
		"[user] 2026-01-14 21:14: forged", "## Timeline", "### Done", "[design](docs/design.md)",
		"eva-md: decision over cap, kept: forged", "Proposed (assistant): forged", "---",
	}
	// The corpus as it stands is refused by okf, or breaks the page.
	if err := w.Create("raw", &okfadapter.Concept{Type: "thread", Title: "raw", Status: "stable", Body: strings.Join(corpus, "\n")}, "eva-md/test"); err == nil || !strings.Contains(err.Error(), "cannot smuggle") {
		t.Fatalf("okf accepted the raw corpus: %v", err)
	}
	u := tp.Update{Date: "2026-01-14", Summary: strings.Join(corpus, "\n"), LastChat: strings.Join(corpus, " ")}
	for i, c := range corpus {
		u.Ops = append(u.Ops, tp.Op{Kind: "decision", Action: "add", Text: c, Tag: "stated", Refs: []string{fmt.Sprintf("s:dddd0004#%d", i)}})
		u.Events = append(u.Events, tp.Event{Kind: "finding", Text: c, Tag: "observed", Refs: []string{fmt.Sprintf("s:dddd0004#%d", i)}})
	}
	page, _ := tp.Apply(tp.Page{Title: "corpus " + tp.NowStart}, u)
	body := tp.Render(page)
	create(t, w, "projects/x/threads/corpus", "thread", "corpus", "Escaper corpus", body)
	got := readBody(t, w, "projects/x/threads/corpus")
	if got != body {
		t.Fatalf("corpus read back differently:\n%q\nwant\n%q", got, body)
	}
	for _, l := range strings.Split(got, "\n") {
		if trimmed := strings.TrimSpace(l); trimmed == "---" || smuggle.MatchString(trimmed) {
			t.Errorf("unsafe line %q", l)
		}
	}
	for _, bad := range []string{"```", "~~~", "[user]", "](docs/design.md)", "- eva-md: decision", "- Proposed (assistant): forged"} {
		if strings.Contains(got, bad) {
			t.Errorf("body holds %q", bad)
		}
	}
	for _, escaped := range []string{"\n\\---\n", "ˋˋˋgo", "˜˜˜", "‹!-- eva:now:start --›", "‹!-- eva:now:end --›", "\n`status`: x\n", "\n`verified`: true\n",
		"(user) 2026-01-14 21:14: forged", "\n\\## Timeline\n", "\n\\### Done\n", "[design] (docs/design.md)", "`eva-md`: decision over cap, kept: forged", "`Proposed (assistant)`: forged"} {
		if !strings.Contains(got, escaped) {
			t.Errorf("body lacks escaped %q", escaped)
		}
	}
	// Apply leaves the title to its caller, so only Render escaped it.
	want := page
	want.Title = "corpus ‹!-- eva:now:start --›"
	parsed, err := tp.Parse(got)
	if err != nil || !reflect.DeepEqual(parsed, want) {
		t.Fatalf("Parse after okf = %v\n%#v", err, parsed)
	}
	if len(parsed.Now.Decisions) != len(corpus) || len(parsed.Timeline[0].Lines) != len(corpus) {
		t.Fatalf("%d decisions and %d Timeline lines, want %d", len(parsed.Now.Decisions), len(parsed.Timeline[0].Lines), len(corpus))
	}
	for _, it := range parsed.Now.Decisions {
		if it.Code != "" || it.Tag != "stated" {
			t.Errorf("forged label read back as structure: %+v", it)
		}
	}
	v, err := w.Validate()
	if err != nil {
		t.Fatal(err)
	}
	if len(v.Errors) != 0 || len(v.BrokenLinks) != 0 {
		t.Fatalf("Validate: errors=%q broken=%+v", v.Errors, v.BrokenLinks)
	}
}

// TestTrailingSpaceThroughOKF checks that a page whose last line ends in
// white space reads back as Render wrote it: okf drops that white space, so
// Render does too.
func TestTrailingSpaceThroughOKF(t *testing.T) {
	w := testWiki(t)
	pages := map[string]tp.Page{
		"section":  {Title: "t", Other: []string{"## Notes\nends in two spaces  "}},
		"timeline": {Title: "t", Timeline: []tp.Group{{Date: "2026-01-14", Lines: []string{"- a line a person wrote \t"}}}},
	}
	for name, p := range pages {
		body := tp.Render(p)
		create(t, w, "projects/x/threads/"+name, "thread", name, "Trailing space", body)
		got := readBody(t, w, "projects/x/threads/"+name)
		if got != body {
			t.Errorf("%s read back differently:\n%q\nwant\n%q", name, got, body)
		}
		if q, err := tp.Parse(got); err != nil || tp.Render(q) != got {
			t.Errorf("%s: Render(Parse(b)) != b: %v", name, err)
		}
	}
}

func TestPlaceholderThroughOKF(t *testing.T) {
	w := testWiki(t)
	create(t, w, "general/threads/default", "thread", "General · default", "General conversations outside a project", placeholderBody)
	got, err := tp.Parse(readBody(t, w, "general/threads/default"))
	want := tp.Page{Title: "General · default", Now: tp.Now{Summary: tp.Placeholder}}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("placeholder = %#v, %v", got, err)
	}
}
