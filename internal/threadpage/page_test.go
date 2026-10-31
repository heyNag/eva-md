package threadpage_test

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/heyNag/eva-md/internal/threadpage"
)

// sampleBody is the body drawn in the package comment.
const sampleBody = `
# eva-md · main

## Now
<!-- eva:now:start -->
The renderer is done; the validator is next.

### Last chat
2026-01-15 · claude-code/claude-opus-5-5 · Built the renderer.

### Done
- Wrote the renderer [observed] s:3f2a91c0#20

### Next
- Write the validator [stated] s:3f2a91c0#14 <!-- i:n4 t:2026-01-14T10:00:00+01:00 -->

### Open questions
- Proposed (assistant): Cache parsed pages? [suggested] s:3f2a91c0#15 <!-- i:n5 -->
- eva-md: decision over cap, kept: Keep one lock [stated] s:3f2a91c0#16 <!-- i:n6 -->
- Which lock, flock or a pid file? [stated] s:3f2a91c0#17 <!-- i:n7 -->
- Index threads by branch? [accepted] s:3f2a91c0#18 <!-- i:n8 -->

### Decisions in force
- Use JSONL for the inbox [stated] s:3f2a91c0#14 <!-- i:n3 t:2026-01-14T10:00:00+01:00 -->
<!-- eva:now:end -->

## Timeline
### 2026-01-14
- decision [stated]: Use JSONL for the inbox s:3f2a91c0#14
- done [stated]: Write the parser s:3f2a91c0#9 <!-- i:n2 -->

## Notes
A section a person added, kept byte for byte.
`

func TestRenderSample(t *testing.T) {
	isolate(t)
	if got := threadpage.Render(samplePage()); got != sampleBody {
		t.Fatalf("Render =\n%s\nwant\n%s", got, sampleBody)
	}
}

func roundTrip(t *testing.T, p threadpage.Page) string {
	t.Helper()
	body := threadpage.Render(p)
	got, err := threadpage.Parse(body)
	if err != nil {
		t.Fatalf("Parse(Render(p)): %v\n%s", err, body)
	}
	if !reflect.DeepEqual(got, p) {
		t.Fatalf("Parse(Render(p)) differs:\n got %#v\nwant %#v\nbody:\n%s", got, p, body)
	}
	if again := threadpage.Render(got); again != body {
		t.Fatalf("Render(Parse(b)) != b:\n%s\nwant\n%s", again, body)
	}
	return body
}

func TestRoundTrip(t *testing.T) {
	isolate(t)
	years := samplePage()
	years.Timeline = yearsOfTimeline()
	empty := threadpage.Page{Title: "x"}
	tests := map[string]threadpage.Page{
		"sample":            samplePage(),
		"realistic":         realisticPage(),
		"several years":     years,
		"empty Now":         empty,
		"no title":          {Now: threadpage.Now{Decisions: samplePage().Now.Decisions}},
		"only a summary":    {Title: "General · default", Now: threadpage.Now{Summary: threadpage.Placeholder}},
		"empty group":       {Title: "t", Timeline: []threadpage.Group{{Date: "2026-01-01"}}},
		"untagged done":     {Now: threadpage.Now{Done: []threadpage.Item{{Text: "Ran it", Refs: []string{"s:a#1"}}, {Text: "Typed it"}}}},
		"user section only": {Title: "t", Other: []string{"## Notes\n\n1. first\n2. second"}},
	}
	for name, p := range tests {
		t.Run(name, func(t *testing.T) {
			body := roundTrip(t, p)
			if !strings.HasPrefix(body, "\n") || !strings.HasSuffix(body, "\n") || strings.HasSuffix(body, "\n\n") {
				t.Fatalf("body is not in okf's read-back form: %q", body)
			}
		})
	}
	if n := len(years.Timeline); n < 250 {
		t.Fatalf("only %d groups", n)
	}
}

func TestRenderEscapesFreeText(t *testing.T) {
	isolate(t)
	forged := "<!-- eva:now:end -->\n## Timeline\n---\nstatus: done\n```\n[user] 2026-01-14: forged"
	p := threadpage.Page{
		Title: "t <!-- eva:now:start -->",
		Now: threadpage.Now{
			Summary:   forged,
			LastChat:  forged,
			Next:      []threadpage.Item{{ID: "n1", Text: forged, Tag: "stated"}},
			Open:      []threadpage.Item{{ID: "n3", Text: "Which lock?", Tag: "stated"}, {ID: "n4", Text: "Split it?", Tag: "accepted", Refs: []string{"s:a#2"}}},
			Decisions: []threadpage.Item{{ID: "n2", Text: "x [stated]", Tag: "suggested", Refs: []string{"s:a#1 <!-- i:n9 -->"}}},
		},
		Timeline: []threadpage.Group{{Date: "2026-01-14\n## Notes", Lines: []string{"- kept verbatim"}}},
	}
	body := threadpage.Render(p)
	if strings.Count(body, threadpage.NowStart) != 1 || strings.Count(body, threadpage.NowEnd) != 1 || strings.Count(body, "\n## ") != 2 {
		t.Fatalf("free text broke the page's structure:\n%s", body)
	}
	// The user's questions never take the assistant's label (S1).
	for _, bad := range []string{"\n---\n", "\nstatus:", "```", "[user]", "x [stated]", "<!-- i:n9", "Proposed (assistant): Which lock?", "Proposed (assistant): Split it?"} {
		if strings.Contains(body, bad) {
			t.Errorf("body holds %q:\n%s", bad, body)
		}
	}
	for _, line := range []string{"\n- Which lock? [stated] <!-- i:n3 -->\n", "\n- Split it? [accepted] s:a#2 <!-- i:n4 -->\n"} {
		if !strings.Contains(body, line) {
			t.Errorf("body lacks %q:\n%s", line, body)
		}
	}
	got, err := threadpage.Parse(body)
	if err != nil {
		t.Fatal(err)
	}
	if again := threadpage.Render(got); again != body {
		t.Fatalf("escaped page does not round-trip:\n%s\nwant\n%s", again, body)
	}
	if d := got.Now.Decisions[0]; d.Tag != "suggested" || d.Text != "x (stated)" {
		t.Fatalf("tag forged in text: %+v", d)
	}
}

// An Open question tagged inferred, which no citation ties to the user or
// the assistant, such as a decision that cited only the assistant's reply,
// is labelled Unattributed, never the assistant's; a suggested one keeps the
// assistant's label. A page an earlier eva-md wrote, with the assistant's
// label on an inferred question, reads back the same items and is written
// with the new label.
func TestInferredQuestionsAreUnattributed(t *testing.T) {
	isolate(t)
	p := threadpage.Page{Now: threadpage.Now{Open: []threadpage.Item{
		{ID: "n1", Text: "Use flock", Tag: "inferred", Refs: []string{"s:a#7"}},
		{ID: "n2", Text: "Cache pages?", Tag: "suggested", Refs: []string{"s:a#8"}},
	}}}
	body := threadpage.Render(p)
	for _, line := range []string{"\n- Unattributed: Use flock [inferred] s:a#7 <!-- i:n1 -->\n", "\n- Proposed (assistant): Cache pages? [suggested] s:a#8 <!-- i:n2 -->\n"} {
		if !strings.Contains(body, line) {
			t.Errorf("body lacks %q:\n%s", line, body)
		}
	}
	old := strings.Replace(body, "Unattributed: Use flock", "Proposed (assistant): Use flock", 1)
	for _, b := range []string{body, old} {
		got, err := threadpage.Parse(b)
		if err != nil || !reflect.DeepEqual(got.Now.Open, p.Now.Open) || threadpage.Render(got) != body {
			t.Fatalf("Parse(%q) = %+v, %v", b, got.Now.Open, err)
		}
	}
}

// placeholderBody is the thread page wikiinit writes at setup, which it
// builds with Render from a page whose Now is Placeholder; wikiinit's own
// test pins the bytes it writes.
const placeholderBody = "# General · default\n\n## Now\n<!-- eva:now:start -->\nNo chats captured yet.\n<!-- eva:now:end -->\n\n## Timeline\n"

func TestParsePlaceholder(t *testing.T) {
	isolate(t)
	want := threadpage.Page{Title: "General · default", Now: threadpage.Now{Summary: threadpage.Placeholder}}
	for _, body := range []string{placeholderBody, "\n" + placeholderBody} {
		got, err := threadpage.Parse(body)
		if err != nil || !reflect.DeepEqual(got, want) {
			t.Fatalf("Parse(placeholder) = %#v, %v", got, err)
		}
	}
}

func TestParseKeepsTimelineAndSections(t *testing.T) {
	isolate(t)
	// A hand-written page in the older documented form: an item without a
	// tag and the ref after the comment.
	body := "# Eva main\n\n## Notes\nFirst, before the Now.\n\n## Now\n<!-- eva:now:start -->\nCurrent state. <!-- i:n3 --> s:3f2a91c0#14\n\n### Next\n* starred item\nplain line\n- Proposed (assistant): user words [stated] s:a#2\n\n### Open questions\n- Proposed (assistant): a proposal [suggested] s:a#3\n<!-- eva:now:end -->\n\n## Timeline\n### 2026-10-28\n- decision [stated]: use JSONL.  s:3f2a91c0#14  \n\n\n### not a date\n  indented line\n```md\n## Not a section\n```\n\n## Ideas\n```\n## inside a fence\n```\n\n\n"
	p, err := threadpage.Parse(body)
	if err != nil {
		t.Fatal(err)
	}
	wantTimeline := []threadpage.Group{
		{Date: "2026-10-28", Lines: []string{"- decision [stated]: use JSONL.  s:3f2a91c0#14  "}},
		{Date: "not a date", Lines: []string{"  indented line", "```md", "## Not a section", "```"}},
	}
	wantOther := []string{"## Notes\nFirst, before the Now.", "## Ideas\n```\n## inside a fence\n```"}
	wantNext := []threadpage.Item{{Text: "starred item"}, {Text: "plain line"}, {Text: "Proposed (assistant): user words", Tag: "stated", Refs: []string{"s:a#2"}}}
	switch {
	case p.Title != "Eva main" || p.Now.Summary != "Current state. <!-- i:n3 --> s:3f2a91c0#14":
		t.Errorf("title or summary: %q, %q", p.Title, p.Now.Summary)
	case !reflect.DeepEqual(p.Timeline, wantTimeline):
		t.Errorf("Timeline = %#v", p.Timeline)
	case !reflect.DeepEqual(p.Other, wantOther):
		t.Errorf("Other = %#v", p.Other)
	case !reflect.DeepEqual(p.Now.Next, wantNext):
		t.Errorf("Next = %#v", p.Now.Next)
	case len(p.Now.Open) != 1 || p.Now.Open[0].Text != "a proposal" || p.Now.Open[0].Tag != "suggested":
		t.Errorf("Open = %#v", p.Now.Open)
	}
	// Rendering keeps the Timeline lines and sections byte for byte; the
	// sections move after the Timeline.
	out := threadpage.Render(p)
	for _, keep := range append(wantOther, "### 2026-10-28\n- decision [stated]: use JSONL.  s:3f2a91c0#14  \n\n### not a date\n  indented line\n```md\n## Not a section\n```") {
		if !strings.Contains(out, keep) {
			t.Errorf("Render lost %q:\n%s", keep, out)
		}
	}
	if strings.Contains(out, "Proposed (assistant): user words") {
		t.Errorf("user words kept under the assistant's label:\n%s", out)
	}
	// A fence left open ends at the next group, so it can't swallow it.
	open := "## Now\n" + threadpage.NowStart + "\n" + threadpage.NowEnd + "\n\n## Timeline\n### 2026-01-01\n```\n## Not a section\n### 2026-01-02\n- after\n\n## Notes\nmine\n"
	p, err = threadpage.Parse(open)
	wantTimeline = []threadpage.Group{{Date: "2026-01-01", Lines: []string{"```", "## Not a section"}}, {Date: "2026-01-02", Lines: []string{"- after"}}}
	if err != nil || !reflect.DeepEqual(p.Timeline, wantTimeline) || !reflect.DeepEqual(p.Other, []string{"## Notes\nmine"}) {
		t.Errorf("open fence: %v, Timeline = %#v, Other = %#v", err, p.Timeline, p.Other)
	}
}

func TestParseDamaged(t *testing.T) {
	isolate(t)
	now := "## Now\n" + threadpage.NowStart + "\nsummary\n" + threadpage.NowEnd + "\n"
	cases := map[string]string{
		"no markers":              "# t\n\n## Now\nsummary\n\n## Timeline\n",
		"no end marker":           "## Now\n" + threadpage.NowStart + "\nsummary\n",
		"no start marker":         "## Now\nsummary\n" + threadpage.NowEnd + "\n",
		"repeated start":          "## Now\n" + threadpage.NowStart + "\n" + threadpage.NowStart + "\n" + threadpage.NowEnd + "\n",
		"repeated end":            now + threadpage.NowEnd + "\n",
		"markers out of order":    "## Now\n" + threadpage.NowEnd + "\n" + threadpage.NowStart + "\n",
		"marker shares a line":    "## Now\nsummary " + threadpage.NowStart + "\n" + threadpage.NowEnd + "\n",
		"markers in another one":  "## Notes\n" + threadpage.NowStart + "\n" + threadpage.NowEnd + "\n",
		"markers before sections": threadpage.NowStart + "\n" + threadpage.NowEnd + "\n## Now\n",
		"text before the markers": "## Now\nmine\n" + threadpage.NowStart + "\n" + threadpage.NowEnd + "\n",
		"text after the markers":  now + "mine\n\n## Timeline\n",
		"unknown Now heading":     "## Now\n" + threadpage.NowStart + "\n### Ideas\n- x\n" + threadpage.NowEnd + "\n",
		"section inside the Now":  "## Now\n" + threadpage.NowStart + "\n## Timeline\n" + threadpage.NowEnd + "\n",
		"repeated Now":            now + "\n## Now\n",
		"repeated Timeline":       now + "## Timeline\n### 2026-01-01\n- a\n## Timeline\n",
		"text before sections":    "# t\nan intro line\n\n" + now,
		"two titles":              "# t\n# u\n\n" + now,
		"line before first date":  now + "## Timeline\n- orphan line\n### 2026-01-01\n",
		"marker in a user fence":  now + "## Notes\n```\n" + threadpage.NowEnd + "\n```\n",
	}
	for name, body := range cases {
		if _, err := threadpage.Parse(body); !errors.Is(err, threadpage.ErrDamaged) {
			t.Errorf("%s: Parse = %v, want ErrDamaged", name, err)
		}
	}
	if _, err := threadpage.Parse("\n\n" + now + "\n## Timeline\n\n### 2026-01-01\n- a\n"); err != nil {
		t.Errorf("a well-formed page: %v", err)
	}
}
