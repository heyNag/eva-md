package threadpage_test

import (
	"math/rand"
	"reflect"
	"strings"
	"testing"
	"time"

	tp "github.com/heyNag/eva-md/internal/threadpage"
)

// scanBody gives a Scanner every line of body.
func scanBody(body string) []tp.Unit {
	var s tp.Scanner
	var out []tp.Unit
	for _, l := range strings.Split(body, "\n") {
		if u, ok := s.Line(l); ok {
			out = append(out, u)
		}
	}
	return out
}

// checkScan reports where a Scanner reads body differently from p, what
// Parse made of it: every Now item under its heading, and every Timeline
// line under its date, in order.
func checkScan(t *testing.T, name, body string, p tp.Page) {
	t.Helper()
	want := map[string][]tp.Item{"done": p.Now.Done, "next": p.Now.Next, "question": p.Now.Open, "decision": p.Now.Decisions}
	got := map[string][]tp.Item{}
	var lines, wantLines []string
	for _, u := range scanBody(body) {
		if u.Now {
			got[u.Kind] = append(got[u.Kind], u.Item)
			continue
		}
		lines = append(lines, u.Date+"\x00"+u.Line)
	}
	for _, g := range p.Timeline {
		for _, l := range g.Lines {
			wantLines = append(wantLines, g.Date+"\x00"+l)
		}
	}
	for kind, items := range want {
		if len(items) == 0 && len(got[kind]) == 0 {
			continue
		}
		if !reflect.DeepEqual(got[kind], items) {
			t.Fatalf("%s: %s items read as\n%#v\nwant\n%#v", name, kind, got[kind], items)
		}
	}
	if !reflect.DeepEqual(lines, wantLines) {
		t.Fatalf("%s: Timeline lines read as\n%q\nwant\n%q", name, lines, wantLines)
	}
}

// On every body Parse accepts, a Scanner reads the same Now items and
// Timeline lines; on a damaged one it reads what it can without failing.
func TestScannerReadsWhatParseReads(t *testing.T) {
	isolate(t)
	years := realisticPage()
	years.Timeline = append(yearsOfTimeline(), years.Timeline...)
	now := "## Now\n" + tp.NowStart + "\n"
	bodies := map[string]string{
		"sample":      sampleBody,
		"placeholder": placeholderBody,
		"realistic":   tp.Render(realisticPage()),
		"years":       tp.Render(years),
		// A fence left open in a group, a "## " line inside it, and fenced
		// headings in a person's section.
		"fences":     "# t\n\n" + now + "### Next\n- one [stated]\n" + tp.NowEnd + "\n\n## Timeline\n### 2026-01-01\n- a\n```\n## not a section\n### 2026-01-02\n- b\n\n## Notes\n~~~\n## Timeline\n### 2026-01-03\n~~~\n",
		"older form": "# Eva main\n\n## Notes\nFirst, before the Now.\n\n## Now\n" + tp.NowStart + "\nCurrent state. <!-- i:n3 --> s:3f2a91c0#14\n\n### Next\n* starred item\nplain line\n- Proposed (assistant): user words [stated] s:a#2\n\n### Open questions\n- Proposed (assistant): a proposal [suggested] s:a#3\n" + tp.NowEnd + "\n\n## Timeline\n### 2026-10-28\n- decision [stated]: use JSONL.  s:3f2a91c0#14  \n\n\n### not a date\n  indented line\n```md\n## Not a section\n```\n\n## Ideas\n```\n## inside a fence\n```\n",
	}
	r := rand.New(rand.NewSource(8))
	p := tp.Page{Title: "random"}
	for i := range 300 {
		p, _ = tp.Apply(p, randomUpdate(r, p))
		if i%50 == 49 {
			bodies["random "+string(rune('a'+i/50))] = tp.Render(p)
		}
	}
	for name, body := range bodies {
		p, err := tp.Parse(body)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		checkScan(t, name, body, p)
	}
	// Damaged bodies: a missing end marker, a repeated Now, text outside the
	// markers. The Timeline after a Now without its end is still read.
	for _, body := range []string{
		now + "### Next\n- open [stated]\n## Timeline\n### 2026-01-01\n- decision [stated]: kept s:a#1\n",
		now + tp.NowEnd + "\n## Now\n" + tp.NowStart + "\n### Decisions in force\n- twice [stated]\n" + tp.NowEnd + "\n",
		"stray\n" + now + "### Next\n- x [stated]\n### Mystery\n- not an item\n" + tp.NowEnd + "\n## Timeline\nbefore a date\n### 2026-01-02\n- y\n",
	} {
		if _, err := tp.Parse(body); err == nil {
			t.Fatalf("Parse accepted a damaged body:\n%s", body)
		}
		units := scanBody(body)
		if len(units) == 0 {
			t.Fatalf("nothing read from:\n%s", body)
		}
		for _, u := range units {
			if strings.Contains(u.Item.Text, "not an item") || strings.Contains(u.Item.Text, "before a date") {
				t.Fatalf("read %q from a place that holds no items:\n%s", u.Line, body)
			}
		}
		if !strings.Contains(body, "## Timeline\n### 2026-01-01") {
			continue
		}
		if last := units[len(units)-1]; last.Now || last.Kind != "decision" || last.Item.Text != "kept" {
			t.Fatalf("the Timeline after a Now without its end marker: %+v", units)
		}
	}
}

// A Timeline line reads back as Apply wrote it: its kind, tag, text, refs,
// and the id and time of a retired item. A line in another form keeps its
// text.
func TestScannerReadsTimelineLines(t *testing.T) {
	isolate(t)
	at := time.Date(2026, 1, 13, 9, 0, 0, 0, plus1)
	cases := map[string]tp.Unit{
		"- answered [suggested]: Keep a cache? → no, read pages by path [stated] s:3f2a91c0#15 s:91bd0e44#2 <!-- i:n16 -->":            {Kind: "answered", Item: tp.Item{ID: "n16", Text: "Keep a cache? → no, read pages by path [stated]", Tag: "suggested", Refs: []string{"s:3f2a91c0#15", "s:91bd0e44#2"}}},
		"- decision [stated]: ~~Use JSON~~ → Use JSONL (superseded 2026-01-15) s:3f2a91c0#1 <!-- i:n1 t:2026-01-13T09:00:00+01:00 -->": {Kind: "decision", Item: tp.Item{ID: "n1", Text: "~~Use JSON~~ → Use JSONL (superseded 2026-01-15)", Tag: "stated", Refs: []string{"s:3f2a91c0#1"}, At: at}},
		"- finding [observed]: a 2 MB page parses in 0.5 ms s:91bd0e44#8":                                                              {Kind: "finding", Item: tp.Item{Text: "a 2 MB page parses in 0.5 ms", Tag: "observed", Refs: []string{"s:91bd0e44#8"}}},
		"- question [inferred]: eva-md: decision over cap, kept: `status`: x (moved: cap) s:a#3 <!-- i:n7 -->":                         {Kind: "question", Item: tp.Item{ID: "n7", Text: "eva-md: decision over cap, kept: `status`: x (moved: cap)", Tag: "inferred", Refs: []string{"s:a#3"}}},
		"- next: ~~Typed it~~ (dropped) s:a#1 <!-- i:n2 -->":                                                                           {Kind: "next", Item: tp.Item{ID: "n2", Text: "~~Typed it~~ (dropped)", Refs: []string{"s:a#1"}}},
		"A line a person wrote, kept as it is.":                                                                                        {Item: tp.Item{Text: "A line a person wrote, kept as it is."}},
		"- Note: a person's bullet s:a#4":                                                                                              {Item: tp.Item{Text: "Note: a person's bullet", Refs: []string{"s:a#4"}}},
		"- decision [made-up]: not a tag eva-md writes":                                                                                {Item: tp.Item{Text: "decision [made-up]: not a tag eva-md writes"}},
	}
	for line, want := range cases {
		var s tp.Scanner
		s.Line("## Timeline")
		s.Line("### 2026-01-15")
		got, ok := s.Line(line)
		want.Date, want.Line = "2026-01-15", line
		if !ok || !reflect.DeepEqual(got, want) {
			t.Errorf("%q read as %#v, want %#v", line, got, want)
		}
	}
	// Every line Apply writes rebuilds from what the Scanner read.
	r := rand.New(rand.NewSource(5))
	p := tp.Page{Title: "random"}
	lines := 0
	for range 400 {
		p, _ = tp.Apply(p, randomUpdate(r, p))
	}
	for _, u := range scanBody(tp.Render(p)) {
		if u.Now {
			continue
		}
		lines++
		if got := rebuild(u); got != u.Line {
			t.Fatalf("%q rebuilds as %q from %#v", u.Line, got, u)
		}
	}
	if lines < 200 {
		t.Fatalf("only %d Timeline lines", lines)
	}
}

// rebuild writes a Timeline line from its parts, as Apply does.
func rebuild(u tp.Unit) string {
	head := u.Kind
	if u.Item.Tag != "" {
		head += " [" + u.Item.Tag + "]"
	}
	parts := append([]string{head + ":", u.Item.Text}, u.Item.Refs...)
	var meta []string
	if u.Item.ID != "" {
		meta = append(meta, "i:"+u.Item.ID)
	}
	if !u.Item.At.IsZero() {
		meta = append(meta, "t:"+u.Item.At.Format(time.RFC3339Nano))
	}
	if len(meta) > 0 {
		parts = append(parts, "<!-- "+strings.Join(meta, " ")+" -->")
	}
	return "- " + strings.Join(parts, " ")
}

// FuzzScanner checks that a Scanner never panics and, on every body Parse
// accepts, reads what Parse reads.
func FuzzScanner(f *testing.F) {
	isolate(f)
	now := "## Now\n" + tp.NowStart + "\n"
	for _, seed := range []string{
		sampleBody, placeholderBody, tp.Render(realisticPage()),
		now + "- x [stated] <!-->\n- y <!-- i:n1 t:bad -->\n" + tp.NowEnd + "\n",
		now + "### Next\n- a <!-- <!-- i:n2 --> -->\n* b\n-\n" + tp.NowEnd + "\n## Timeline\n###\n- l\n## X\n```\n## Now\n",
		"# t\n\n" + now + tp.NowEnd + "\n\n## Timeline\n### 2026-01-01\n- done [stated]: x s:a#1 <!-- i:n9 -->\n\n## Notes\n~~~\n## Timeline\n",
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, body string) {
		p, err := tp.Parse(body)
		if err != nil {
			scanBody(body)
			return
		}
		checkScan(t, "fuzz", body, p)
	})
}
