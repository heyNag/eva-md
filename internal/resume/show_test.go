package resume

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/heyNag/eva-md/internal/okfadapter"
	"github.com/heyNag/eva-md/internal/threadpage"
)

// bigPage is a thread page of about 3 MB: a Now, ten years of daily
// Timeline groups of 3 lines each, and a person's notes after them.
func bigPage() threadpage.Page {
	filler := strings.Repeat("words of a long decision ", 9)
	var groups []threadpage.Group
	for d := time.Date(2016, 1, 1, 0, 0, 0, 0, time.UTC); d.Year() < 2026; d = d.AddDate(0, 0, 1) {
		day := d.Format(time.DateOnly)
		g := threadpage.Group{Date: day}
		for i := 1; i <= 3; i++ {
			g.Lines = append(g.Lines, fmt.Sprintf("- decision [stated]: %s item %d %ss:big%05d#%d", day, i, filler, d.YearDay(), i))
		}
		groups = append(groups, g)
	}
	return threadpage.Page{
		Title:    "resume · main",
		Now:      threadpage.Now{Summary: "BIG-NOW summary.", Next: []threadpage.Item{{ID: "n1", Text: "BIG-NOW next step", Tag: "stated", Refs: []string{"s:big00001#1"}}}},
		Timeline: groups,
		Other:    []string{"## Notes\nBIG-NOTES kept by a person."},
	}
}

// countingReader counts the bytes read through it.
type countingReader struct {
	r io.ReaderAt
	n int64
}

func (c *countingReader) ReadAt(p []byte, off int64) (int, error) {
	n, err := c.r.ReadAt(p, off)
	c.n += int64(n)
	return n, err
}

// eva-md show streams a page for a section and returns exactly that
// section: the Now of a 3 MB page comes from its first 64 KiB. The whole page
// is still refused past 2 MiB.
func TestShowStreamsOneSectionOfALargePage(t *testing.T) {
	o := fixture(t)
	w := pageWiki(t, o)
	body := savePage(t, w, "projects/resume/threads/main", "main", o.Now.Add(-time.Hour), bigPage())
	raw, err := os.ReadFile(filepath.Join(o.Config.WikiPath, "projects", "resume", "threads", "main.md"))
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) < 3_000_000 {
		t.Fatalf("the page has only %d bytes", len(raw))
	}
	now := body[strings.Index(body, "## Now\n") : strings.Index(body, threadpage.NowEnd)+len(threadpage.NowEnd)]
	r := &countingReader{r: bytes.NewReader(raw)}
	got, err := readSection(context.Background(), r, "Now", ShowOptions{})
	if err != nil || got != now {
		t.Fatalf("readSection = %q, %v; want %q", got, err, now)
	}
	if r.n > 256<<10 {
		t.Fatalf("read %d of %d bytes for the Now", r.n, len(raw))
	}
	start := time.Now()
	text, err := Show(context.Background(), o.Dirs, o.Config, "projects/resume/threads/main#now")
	if err != nil || text != clean(now) {
		t.Fatalf("show #now = %q, %v", text, err)
	}
	t.Logf("#Now of a %.1f MB page in %s, %d bytes read", float64(len(raw))/1e6, time.Since(start), r.n)
	if _, err := Show(context.Background(), o.Dirs, o.Config, "projects/resume/threads/main"); err == nil || !strings.Contains(err.Error(), "exceeds 2 MiB") {
		t.Fatalf("whole page: %v", err)
	}
	// A section after the Timeline is reached by reading to it.
	if notes, err := Show(context.Background(), o.Dirs, o.Config, "projects/resume/threads/main#Notes"); err != nil || notes != "## Notes\nBIG-NOTES kept by a person." {
		t.Fatalf("notes = %q, %v", notes, err)
	}
	// #Timeline and a day read the page once, and the groups shown only up
	// to their ends.
	for _, c := range []struct {
		name string
		so   ShowOptions
	}{{"Timeline", ShowOptions{}}, {"Timeline", ShowOptions{Last: 200}}, {"2020-02-29", ShowOptions{}}} {
		r := &countingReader{r: bytes.NewReader(raw)}
		if _, err := readSection(context.Background(), r, c.name, c.so); err != nil {
			t.Fatalf("#%s %+v: %v", c.name, c.so, err)
		}
		if r.n > int64(len(raw))+2*sectionCap {
			t.Errorf("#%s %+v read %d bytes of a %d-byte page", c.name, c.so, r.n, len(raw))
		}
	}
}

// A heading in a fenced block is no heading to show, as to Parse and the
// Scanner: a shell comment in a person's notes doesn't cut them short, a
// "## " line fenced in a Timeline group doesn't end the Timeline, a "### "
// line there always starts a group, and a stray fence in the Now hides
// nothing. So show returns each section and day as Parse reads it, which
// are the lines search finds under a day.
func TestShowSectionsSkipFencedHeadings(t *testing.T) {
	body := "\n# resume · main\n\n## Now\n" + threadpage.NowStart + "\nThe fence work.\n```\n" + threadpage.NowEnd + "\n\n## Timeline\n" +
		"### 2026-10-20\n- finding [observed]: a fenced block follows\n```\n## not a section\n" +
		"### 2026-10-21\n- decision [stated]: the group a fence was left open in s:fence001#2\n```\n" +
		"### 2026-10-22\n- done [observed]: after the fence\n\n" +
		"## Notes\nInstall:\n```sh\n# build first\ngo build ./...\n```\nThen run it.\n"
	p, err := threadpage.Parse(body)
	if err != nil || len(p.Timeline) != 3 || len(p.Other) != 1 {
		t.Fatalf("Parse: %v, %d groups, %d other sections", err, len(p.Timeline), len(p.Other))
	}
	section := func(name string) string {
		t.Helper()
		text, err := readSection(context.Background(), strings.NewReader(body), name, ShowOptions{})
		if err != nil {
			t.Fatalf("#%s: %v", name, err)
		}
		return text
	}
	if got := section("Notes"); got != p.Other[0] {
		t.Errorf("#Notes = %q, want %q", got, p.Other[0])
	}
	if got, want := section("Now"), "## Now\n"+threadpage.NowStart+"\nThe fence work.\n```\n"+threadpage.NowEnd; got != want {
		t.Errorf("#Now = %q, want %q", got, want)
	}
	timeline := "## Timeline"
	for _, g := range p.Timeline {
		day := "### " + g.Date + "\n" + strings.Join(g.Lines, "\n")
		if got := section(g.Date); got != day {
			t.Errorf("#%s = %q, want %q", g.Date, got, day)
		}
		timeline += "\n" + day
	}
	if got := section("Timeline"); got != timeline {
		t.Errorf("#Timeline = %q, want %q", got, timeline)
	}
	if _, err := readSection(context.Background(), strings.NewReader(body), "build first", ShowOptions{}); err == nil {
		t.Error("a fenced line was found as a section")
	}
}

// #Timeline shows the newest 10 days by default, in page order, never a day
// filed out of order at the end; --last and --since choose others; past
// about 16 KB it stops and says how many lines are left. #<day> returns that
// day's group, or both when the day was filed twice. Options that don't fit
// the ref are refused.
func TestShowTimelineChoosesDateGroups(t *testing.T) {
	o := fixture(t)
	w := pageWiki(t, o)
	p := bigPage()
	p.Timeline = append(p.Timeline, threadpage.Group{Date: "2016-01-05", Lines: []string{"- finding [observed]: LATE-FILED for an old day s:late0001#1"}})
	savePage(t, w, "projects/resume/threads/main", "main", o.Now.Add(-time.Hour), p)
	show := func(ref string, so ShowOptions) string {
		t.Helper()
		text, err := ShowWith(context.Background(), o.Dirs, o.Config, "projects/resume/threads/main"+ref, so)
		if err != nil {
			t.Fatalf("%s %+v: %v", ref, so, err)
		}
		return text
	}
	days := func(text string) []string {
		var out []string
		for _, l := range strings.Split(text, "\n") {
			if d, ok := strings.CutPrefix(l, "### "); ok {
				out = append(out, d)
			}
		}
		return out
	}
	newest := func(n int) []string {
		var out []string
		for i := n - 1; i >= 0; i-- {
			out = append(out, time.Date(2025, 12, 31-i, 0, 0, 0, 0, time.UTC).Format(time.DateOnly))
		}
		return out
	}
	text := show("#Timeline", ShowOptions{})
	if !strings.HasPrefix(text, "## Timeline\nShowing 10 of 3653 date groups; use --since YYYY-MM-DD or --last N for others.\n### 2025-12-22\n") || !reflect.DeepEqual(days(text), newest(10)) ||
		strings.Count(text, "\n- decision [stated]: ") != 30 || strings.Contains(text, "LATE-FILED") || strings.Contains(text, "more line") {
		t.Fatalf("default:\n%s", text)
	}
	for _, c := range []struct {
		so   ShowOptions
		want []string
	}{
		{ShowOptions{Last: 3}, newest(3)},
		{ShowOptions{Since: "2025-12-27"}, newest(5)},
		{ShowOptions{Since: "2025-12-01", Last: 2}, newest(2)},
		{ShowOptions{Since: "2026-01-01"}, nil},
	} {
		if got := days(show("#Timeline", c.so)); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%+v: days %q, want %q", c.so, got, c.want)
		}
	}
	// 200 days are over 16 KB: the lines shown and the lines left add up.
	long := show("#Timeline", ShowOptions{Last: 200})
	m := regexp.MustCompile(`\n(\d+) more lines; use --since or --last$`).FindStringSubmatch(long)
	if m == nil || len(long) > sectionCap+100 {
		t.Fatalf("%d bytes, ending %q", len(long), long[max(len(long)-200, 0):])
	}
	left, _ := strconv.Atoi(m[1])
	shown := 0
	for _, l := range strings.Split(long, "\n")[2:] {
		if strings.HasPrefix(l, "### ") || strings.HasPrefix(l, "- ") {
			shown++
		}
	}
	if shown == 0 || shown+left != 200*4 {
		t.Fatalf("%d lines shown and %d left of %d", shown, left, 200*4)
	}
	one := show("#2020-02-29", ShowOptions{})
	for _, g := range p.Timeline {
		if g.Date == "2020-02-29" && one != "### 2020-02-29\n"+strings.Join(g.Lines, "\n") {
			t.Fatalf("one day:\n%s", one)
		}
	}
	twice := show("#2016-01-05", ShowOptions{})
	if !reflect.DeepEqual(days(twice), []string{"2016-01-05", "2016-01-05"}) || strings.Count(twice, "\n- ") != 4 || !strings.HasSuffix(twice, "LATE-FILED for an old day s:late0001#1") {
		t.Fatalf("a day filed twice:\n%s", twice)
	}
	for _, c := range []struct {
		ref string
		so  ShowOptions
	}{
		{"projects/resume/threads/main#Now", ShowOptions{Last: 3}},
		{"projects/resume/threads/main", ShowOptions{Since: "2025-12-01"}},
		{"s:big00001#1", ShowOptions{Last: 3}},
		{"projects/resume/threads/main#Timeline", ShowOptions{Since: "2025-1-5"}},
		{"projects/resume/threads/main#Timeline", ShowOptions{Last: -1}},
		{"projects/resume/threads/main#Nowhere", ShowOptions{}},
	} {
		if _, err := ShowWith(context.Background(), o.Dirs, o.Config, c.ref, c.so); err == nil {
			t.Errorf("%s %+v accepted", c.ref, c.so)
		}
	}
	// Lines between the Timeline's heading and its first day are shown with
	// it.
	old := &okfadapter.Concept{Type: "thread", Title: "old", Status: "stable", Body: "## Now\n" + threadpage.NowStart + "\nx\n" + threadpage.NowEnd + "\n\n## Timeline\nA line before any day.\n### 2026-10-01\n- first day\n"}
	if err := w.Create("projects/resume/threads/old", old, "eva-md/test"); err != nil {
		t.Fatal(err)
	}
	if got, err := Show(context.Background(), o.Dirs, o.Config, "projects/resume/threads/old#Timeline"); err != nil || got != "## Timeline\nA line before any day.\n### 2026-10-01\n- first day" {
		t.Fatalf("intro = %q, %v", got, err)
	}
}
