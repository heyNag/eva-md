package threadpage_test

import (
	"reflect"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	tp "github.com/heyNag/eva-md/internal/threadpage"
)

// okfLink is the link pattern okf counts for orphans and broken links
// (bundle.go).
var okfLink = regexp.MustCompile(`\]\(([^)\s]+\.md)(?:#[^)]*)?\)`)

func TestRenderOverview(t *testing.T) {
	isolate(t)
	odd := "feature/x](y.md)"
	threads := []tp.Thread{
		{Branch: "old", File: "old", Summary: "Old work. Done long ago.", Active: day(2)},
		{Branch: "main", File: "main", Summary: "Parser done! Validator next.", Active: day(9)},
		{Branch: odd, File: tp.ThreadName(odd), Summary: "\\# not a heading\nsecond line [user]", Active: day(9)},
		{Branch: "idle", File: "idle"},
		{Branch: "feature/y", Summary: "No file given.", Active: day(3)},
		{File: "default", Summary: tp.Placeholder},
		{Branch: "日本", File: tp.ThreadName("日本"), Summary: "日本語の要約。二文目。", Active: day(5).In(time.FixedZone("", 9*3600))},
		{Branch: "long", File: "long", Summary: strings.Repeat("word ", 100), Active: day(1)},
	}
	input := slices.Clone(threads)
	got := tp.RenderOverview("eva-md", threads, false)
	want := "\n# eva-md\n\n## Threads\n\n" +
		"* [feature/x\\] (y.md)](threads/" + tp.ThreadName(odd) + ".md) — \\# not a heading second line (user) · last active 2026-01-09\n" +
		"* [main](threads/main.md) — Parser done! · last active 2026-01-09\n" +
		"* [日本](threads/日本.md) — 日本語の要約。 · last active 2026-01-05\n" +
		"* [feature/y](threads/" + tp.ThreadName("feature/y") + ".md) — No file given. · last active 2026-01-03\n" +
		"* [old](threads/old.md) — Old work. · last active 2026-01-02\n" +
		"* [long](threads/long.md) — " + strings.Repeat("word ", 39) + "word… · last active 2026-01-01\n" +
		"* [default](threads/default.md) — No chats captured yet.\n" +
		"* [idle](threads/idle.md)\n"
	if got != want {
		t.Fatalf("RenderOverview =\n%s\nwant\n%s", got, want)
	}
	if !reflect.DeepEqual(threads, input) {
		t.Fatal("RenderOverview reordered its input")
	}
	// okf sees exactly one link per thread, to that thread's file.
	var targets []string
	for _, m := range okfLink.FindAllStringSubmatch(got, -1) {
		targets = append(targets, m[1])
	}
	var files []string
	for _, th := range threads {
		file := th.File
		if file == "" {
			file = tp.ThreadName(th.Branch)
		}
		files = append(files, "threads/"+file+".md")
	}
	slices.Sort(targets)
	slices.Sort(files)
	if !slices.Equal(targets, files) {
		t.Fatalf("okf links %q, want %q", targets, files)
	}
	if got := tp.RenderOverview("p", nil, false); got != "\n# p\n\n## Threads\n" {
		t.Fatalf("no threads = %q", got)
	}
	// With a rules page, the overview links it once, before the threads.
	got = tp.RenderOverview("p", []tp.Thread{{Branch: "main", File: "main"}}, true)
	if want := "\n# p\n\n## Rules\n\n* [Standing rules](rules.md)\n\n## Threads\n\n* [main](threads/main.md)\n"; got != want {
		t.Fatalf("with rules = %q, want %q", got, want)
	}
	if links := okfLink.FindAllStringSubmatch(got, -1); len(links) != 2 || links[0][1] != "rules.md" {
		t.Fatalf("okf links %q", links)
	}
}

func TestOverviewSentence(t *testing.T) {
	isolate(t)
	for summary, want := range map[string]string{
		"Use e.g. a lock. Then more.":  "Use e.g. a lock.",
		"Step 1. Read it. Then write.": "Step 1. Read it.",
		"Ship v1. soon. Then v2.":      "Ship v1. soon.",
		"Ends in a number 42.":         "Ends in a number 42.",
		"Plain. Second.":               "Plain.",
		"Version 2? Maybe.":            "Version 2?",
	} {
		got := tp.RenderOverview("p", []tp.Thread{{Branch: "b", File: "b", Summary: summary}}, false)
		if want := "\n# p\n\n## Threads\n\n* [b](threads/b.md) — " + want + "\n"; got != want {
			t.Errorf("summary %q gives %q, want %q", summary, got, want)
		}
	}
}
