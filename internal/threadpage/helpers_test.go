package threadpage_test

import (
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/heyNag/eva-md/internal/threadpage"
)

func isolate(t testing.TB) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(home, "gitconfig"))
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
}

var (
	plus1  = time.FixedZone("", 3600)
	india  = time.FixedZone("", 19800)
	minus8 = time.FixedZone("", -8*3600)
)

// samplePage is the page drawn in the package comment.
func samplePage() threadpage.Page {
	at := time.Date(2026, 1, 14, 10, 0, 0, 0, plus1)
	return threadpage.Page{
		Title: "eva-md · main",
		Now: threadpage.Now{
			Summary:  "The renderer is done; the validator is next.",
			LastChat: "2026-01-15 · claude-code/claude-opus-5-5 · Built the renderer.",
			Done:     []threadpage.Item{{Text: "Wrote the renderer", Tag: "observed", Refs: []string{"s:3f2a91c0#20"}}},
			Next:     []threadpage.Item{{ID: "n4", Text: "Write the validator", Tag: "stated", Refs: []string{"s:3f2a91c0#14"}, At: at}},
			Open: []threadpage.Item{
				{ID: "n5", Text: "Cache parsed pages?", Tag: "suggested", Refs: []string{"s:3f2a91c0#15"}},
				{ID: "n6", Text: "Keep one lock", Tag: "stated", Refs: []string{"s:3f2a91c0#16"}, Code: threadpage.OverCapCode},
				{ID: "n7", Text: "Which lock, flock or a pid file?", Tag: "stated", Refs: []string{"s:3f2a91c0#17"}},
				{ID: "n8", Text: "Index threads by branch?", Tag: "accepted", Refs: []string{"s:3f2a91c0#18"}},
			},
			Decisions: []threadpage.Item{{ID: "n3", Text: "Use JSONL for the inbox", Tag: "stated", Refs: []string{"s:3f2a91c0#14"}, At: at}},
		},
		Timeline: []threadpage.Group{{Date: "2026-01-14", Lines: []string{
			"- decision [stated]: Use JSONL for the inbox s:3f2a91c0#14",
			"- done [stated]: Write the parser s:3f2a91c0#9 <!-- i:n2 -->",
		}}},
		Other: []string{"## Notes\nA section a person added, kept byte for byte."},
	}
}

// realisticPage has every kind of line: a summary of two paragraphs, code
// lines, proposals, retired lines, several offsets and people's sections,
// one with a fenced block that holds a heading.
func realisticPage() threadpage.Page {
	p := samplePage()
	p.Now.Summary = "Thread pages render and parse; Apply places validated operations.\n\nNext comes the validator, then the update job."
	p.Now.Done = append(p.Now.Done, threadpage.Item{Text: "Fixed the cap order", Tag: "stated", Refs: []string{"s:91bd0e44#3", "s:91bd0e44#5"}, At: time.Date(2026, 1, 15, 8, 30, 0, 0, india)})
	p.Now.Next = append(p.Now.Next,
		threadpage.Item{ID: "n17", Text: "Measure a 2 MB page", Tag: "inferred", Refs: []string{"s:91bd0e44#7"}},
		threadpage.Item{ID: "n9", Text: "Ask about rotation · later", Tag: "accepted", Refs: []string{"s:91bd0e44#9"}, At: time.Date(2026, 1, 15, 9, 0, 0, 123000000, minus8)},
	)
	p.Now.Open = append(p.Now.Open,
		threadpage.Item{ID: "n10", Text: "Should search cover pages?", Tag: "stated", Refs: []string{"s:91bd0e44#11"}, At: time.Date(2026, 1, 15, 9, 5, 0, 0, time.UTC)},
		threadpage.Item{ID: "n11", Text: "Does n3 replace n2?", Tag: "inferred", Code: "replace question"},
		threadpage.Item{ID: "n12", Text: "Split the page at 512 KB?", Tag: "inferred", Refs: []string{"s:91bd0e44#12"}},
	)
	p.Now.Decisions = append(p.Now.Decisions,
		threadpage.Item{ID: "n13", Text: "日本語 and émoji 🙂 stay as written", Tag: "accepted", Refs: []string{"s:91bd0e44#13"}, At: time.Date(2026, 1, 15, 9, 6, 0, 0, india)},
	)
	p.Timeline = append(p.Timeline, threadpage.Group{Date: "2026-01-15", Lines: []string{
		"- answered [suggested]: Keep a cache? → no, read pages by path [stated] s:3f2a91c0#15 s:91bd0e44#2 <!-- i:n16 -->",
		"- decision [stated]: ~~Use JSON~~ → Use JSONL (superseded 2026-01-15) s:3f2a91c0#1 s:91bd0e44#4 <!-- i:n1 t:2026-01-13T09:00:00+01:00 -->",
		"- next [inferred]: ~~Add a daemon~~ (dropped: nothing stays running) s:3f2a91c0#2 <!-- i:n14 -->",
		"- question [suggested]: Use BM25? (moved: cap) s:3f2a91c0#3 <!-- i:n15 -->",
		"- finding [observed]: a 2 MB page parses in 0.5 ms s:91bd0e44#8",
		"A line a person wrote, kept as it is.",
	}})
	p.Other = append(p.Other,
		"## Links\n\n- [design](https://example.com/design)\n\n```md\n## Not a section\n<!-- a comment of theirs -->\n```\n\nAfter the block.",
		"## Ideas\nshort",
	)
	return p
}

// yearsOfTimeline returns groups every four days from 2023 to 2026.
func yearsOfTimeline() []threadpage.Group {
	var groups []threadpage.Group
	for d := time.Date(2023, 1, 2, 0, 0, 0, 0, time.UTC); d.Year() < 2026; d = d.AddDate(0, 0, 4) {
		date := d.Format("2006-01-02")
		groups = append(groups, threadpage.Group{Date: date, Lines: []string{
			fmt.Sprintf("- change [observed]: edited page.go on %s s:%08x#%d", date, d.YearDay(), d.Day()),
			fmt.Sprintf("- decision [stated]: decision of %s s:%08x#%d", date, d.YearDay(), d.Day()+1),
			fmt.Sprintf("- done [stated]: task of %s s:%08x#4 <!-- i:n%d -->", date, d.YearDay(), d.YearDay()),
		}})
	}
	return groups
}
