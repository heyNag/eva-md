package threadpage_test

import (
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	tp "github.com/heyNag/eva-md/internal/threadpage"
)

var (
	t0 = time.Date(2026, 1, 14, 10, 0, 0, 0, plus1)
	t1 = time.Date(2026, 1, 15, 9, 30, 0, 0, plus1)
)

func day(d int) time.Time { return time.Date(2026, 1, d, 12, 0, 0, 0, time.UTC) }

func ref(n int) []string { return []string{fmt.Sprintf("s:cccc0003#%d", n)} }

// actionsPage and actionsUpdate exercise every action at once.
func actionsPage() tp.Page {
	return tp.Page{
		Title: "eva-md · main",
		Now: tp.Now{
			Summary:  "old summary",
			LastChat: "old last chat",
			Done:     []tp.Item{{Text: "old done", Tag: "observed", Refs: []string{"s:aaaa0001#8"}}},
			Next: []tp.Item{
				{ID: "n1", Text: "Write the parser", Tag: "stated", Refs: []string{"s:aaaa0001#3"}, At: t0},
				{ID: "n2", Text: "Write the renderer", Tag: "stated", Refs: []string{"s:aaaa0001#4"}, At: t0},
				{ID: "n7", Text: "Try a cache", Tag: "suggested", Refs: []string{"s:aaaa0001#9"}},
			},
			Open: []tp.Item{
				{ID: "n3", Text: "Keep a cache?", Tag: "suggested", Refs: []string{"s:aaaa0001#5"}},
				{ID: "n8", Text: "Which lock?", Tag: "stated", Refs: []string{"s:aaaa0001#10"}, At: t0},
			},
			Decisions: []tp.Item{
				{ID: "n4", Text: "Use JSON", Tag: "stated", Refs: []string{"s:aaaa0001#1"}, At: t0},
				{ID: "n5", Text: "One lock per wiki", Tag: "stated", Refs: []string{"s:aaaa0001#2"}, At: t0},
			},
		},
		Timeline: []tp.Group{{Date: "2026-01-14", Lines: []string{"- decision [stated]: Use JSON s:aaaa0001#1"}}},
		Other:    []string{"## Notes\nmine"},
	}
}

func actionsUpdate() tp.Update {
	return tp.Update{
		Date:     "2026-01-15",
		Summary:  "new summary",
		LastChat: "2026-01-15 · codex/gpt-5.5-codex · new",
		Done:     []tp.Item{{ID: "n99", Text: "Wrote the parser", Tag: "observed", Refs: []string{"s:bbbb0002#7"}}},
		Ops: []tp.Op{
			{ID: "n2", Kind: "next", Action: "keep"},
			{ID: "n8", Kind: "question", Action: "edit", Text: "Which lock, flock or a pid file?", Refs: []string{"s:bbbb0002#2"}, At: t1},
			{ID: "n1", Kind: "next", Action: "done", Refs: []string{"s:bbbb0002#7"}},
			{ID: "n3", Kind: "question", Action: "answered", Replacement: "No, read pages by path", Tag: "stated", Refs: []string{"s:bbbb0002#3"}},
			{ID: "n4", Kind: "decision", Action: "superseded", Replacement: "Use JSONL", Tag: "stated", Refs: []string{"s:bbbb0002#4"}, At: t1},
			{ID: "n7", Kind: "next", Action: "dropped", Reason: "pages are read by path", Refs: []string{"s:bbbb0002#3"}},
			{Kind: "next", Action: "add", Text: "Write the validator", Tag: "stated", Refs: []string{"s:bbbb0002#5"}, At: t1},
			{Kind: "question", Action: "add", Text: "Cap the summary at 600?", Tag: "inferred", Refs: []string{"s:bbbb0002#6"}},
			{Kind: "decision", Action: "add", Text: "Escape free text once", Tag: "accepted", Refs: []string{"s:bbbb0002#6", "s:bbbb0002#8"}, At: t1},
			{ID: "n42", Kind: "next", Action: "done"},
		},
		Events: []tp.Event{
			{Kind: "decision", Text: "Use JSONL", Tag: "stated", Refs: []string{"s:bbbb0002#4"}},
			{Kind: "decision", Text: "Use JSON", Tag: "stated", Refs: []string{"s:aaaa0001#1"}},
			{Kind: "finding", Text: "a 2 MB page\nparses in 0.5 ms", Tag: "observed", Refs: []string{"s:bbbb0002#9"}},
		},
	}
}

func TestApplyActions(t *testing.T) {
	isolate(t)
	p := actionsPage()
	got, st := tp.Apply(p, actionsUpdate())
	want := actionsPage()
	want.Now = tp.Now{
		Summary:  "new summary",
		LastChat: "2026-01-15 · codex/gpt-5.5-codex · new",
		Done:     []tp.Item{{Text: "Wrote the parser", Tag: "observed", Refs: []string{"s:bbbb0002#7"}}},
		Next: []tp.Item{
			{ID: "n2", Text: "Write the renderer", Tag: "stated", Refs: []string{"s:aaaa0001#4"}, At: t0},
			{ID: "n9", Text: "Write the validator", Tag: "stated", Refs: []string{"s:bbbb0002#5"}, At: t1},
		},
		Open: []tp.Item{
			{ID: "n8", Text: "Which lock, flock or a pid file?", Tag: "stated", Refs: []string{"s:bbbb0002#2"}, At: t1},
			{ID: "n10", Text: "Cap the summary at 600?", Tag: "inferred", Refs: []string{"s:bbbb0002#6"}},
		},
		Decisions: []tp.Item{
			{ID: "n4", Text: "Use JSONL", Tag: "stated", Refs: []string{"s:bbbb0002#4"}, At: t1},
			{ID: "n5", Text: "One lock per wiki", Tag: "stated", Refs: []string{"s:aaaa0001#2"}, At: t0},
			{ID: "n11", Text: "Escape free text once", Tag: "accepted", Refs: []string{"s:bbbb0002#6", "s:bbbb0002#8"}, At: t1},
		},
	}
	want.Timeline = append(want.Timeline, tp.Group{Date: "2026-01-15", Lines: []string{
		"- done [stated]: Write the parser s:aaaa0001#3 s:bbbb0002#7 <!-- i:n1 t:2026-01-14T10:00:00+01:00 -->",
		"- answered [suggested]: Keep a cache? → No, read pages by path [stated] s:aaaa0001#5 s:bbbb0002#3 <!-- i:n3 -->",
		"- decision [stated]: ~~Use JSON~~ → Use JSONL (superseded 2026-01-15) s:aaaa0001#1 s:bbbb0002#4 <!-- i:n4 t:2026-01-14T10:00:00+01:00 -->",
		"- next [suggested]: ~~Try a cache~~ (dropped: pages are read by path) s:aaaa0001#9 s:bbbb0002#3 <!-- i:n7 -->",
		"- decision [stated]: Use JSONL s:bbbb0002#4",
		"- finding [observed]: a 2 MB page parses in 0.5 ms s:bbbb0002#9",
	}})
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Apply =\n%s\nwant\n%s", tp.Render(got), tp.Render(want))
	}
	if wantStats := (tp.Stats{Added: 5, Edited: 1, Retired: 4, Duplicate: 1}); st != wantStats {
		t.Fatalf("stats = %+v, want %+v", st, wantStats)
	}
	if !reflect.DeepEqual(p, actionsPage()) {
		t.Fatal("Apply changed its input page")
	}
	roundTrip(t, got)
}

// TestApplyTagChanges checks the S1 path: an assistant's item edited,
// answered or superseded with the user's words comes out under the user's
// tag, on the item and on its Timeline line.
func TestApplyTagChanges(t *testing.T) {
	isolate(t)
	p := tp.Page{Now: tp.Now{
		Next:      []tp.Item{{ID: "n1", Text: "Try a cache", Tag: "suggested", Refs: ref(1)}},
		Open:      []tp.Item{{ID: "n2", Text: "Cache pages?", Tag: "inferred", Refs: ref(2)}},
		Decisions: []tp.Item{{ID: "n3", Text: "old", Tag: "suggested", Refs: ref(3)}},
	}}
	got, st := tp.Apply(p, tp.Update{Date: "2026-01-20", Ops: []tp.Op{
		{ID: "n1", Kind: "next", Action: "edit", Text: "Try a cache", Tag: "accepted"},
		{ID: "n2", Kind: "question", Action: "answered", Replacement: "answer", Tag: "stated", Refs: ref(4)},
		{ID: "n3", Kind: "decision", Action: "superseded", Replacement: "new", Tag: "stated", Refs: ref(5)},
	}})
	want := tp.Page{
		Now: tp.Now{
			Next:      []tp.Item{{ID: "n1", Text: "Try a cache", Tag: "accepted", Refs: ref(1)}},
			Decisions: []tp.Item{{ID: "n3", Text: "new", Tag: "stated", Refs: ref(5)}},
		},
		Timeline: []tp.Group{{Date: "2026-01-20", Lines: []string{
			"- answered [inferred]: Cache pages? → answer [stated] s:cccc0003#2 s:cccc0003#4 <!-- i:n2 -->",
			"- decision [suggested]: ~~old~~ → new [stated] (superseded 2026-01-20) s:cccc0003#3 s:cccc0003#5 <!-- i:n3 -->",
		}}},
	}
	if !reflect.DeepEqual(got, want) || st != (tp.Stats{Edited: 1, Retired: 2}) {
		t.Fatalf("Apply = (stats %+v)\n%s\nwant\n%s", st, tp.Render(got), tp.Render(want))
	}
	// Without a tag, the answer or replacement would show under the item's
	// tag, so the operation changes nothing.
	for _, op := range []tp.Op{
		{ID: "n2", Kind: "question", Action: "answered", Replacement: "answer", Refs: ref(4)},
		{ID: "n3", Kind: "decision", Action: "superseded", Replacement: "new", Refs: ref(5)},
		{ID: "n3", Kind: "decision", Action: "superseded", Replacement: "new", Tag: "<>"},
	} {
		if got, st := tp.Apply(p, tp.Update{Date: "2026-01-20", Ops: []tp.Op{op}}); !reflect.DeepEqual(got, p) || st != (tp.Stats{}) {
			t.Errorf("%s with tag %q changed the page (stats %+v):\n%s", op.Action, op.Tag, st, tp.Render(got))
		}
	}
}

func TestApplyIDsNeverReused(t *testing.T) {
	isolate(t)
	p := tp.Page{
		Now: tp.Now{
			Next:      []tp.Item{{ID: "n1", Text: "a", Tag: "stated"}, {ID: "n5", Text: "highest", Tag: "stated"}},
			Decisions: []tp.Item{{ID: "n3", Text: "b", Tag: "stated"}},
		},
		Timeline: []tp.Group{{Date: "2026-01-14", Lines: []string{"- done [stated]: c <!-- i:n4 -->"}}},
	}
	add := func(text string) tp.Op { return tp.Op{Kind: "next", Action: "add", Text: text, Tag: "stated"} }
	p, _ = tp.Apply(p, tp.Update{Date: "2026-01-15", Ops: []tp.Op{{ID: "n5", Kind: "next", Action: "done"}, add("first new")}})
	if last := p.Now.Next[len(p.Now.Next)-1]; last.ID != "n6" || last.Text != "first new" {
		t.Fatalf("after retiring n5 the add got %+v, want n6", last)
	}
	if !strings.Contains(strings.Join(p.Timeline[1].Lines, "\n"), "highest <!-- i:n5 -->") {
		t.Fatalf("the retired line lost its id: %q", p.Timeline[1].Lines)
	}
	p, _ = tp.Apply(p, tp.Update{Date: "2026-01-15", Ops: []tp.Op{{ID: "n6", Kind: "next", Action: "dropped", Reason: "r"}, add("second new")}})
	if last := p.Now.Next[len(p.Now.Next)-1]; last.ID != "n7" {
		t.Fatalf("the next add got %+v, want n7", last)
	}
	// The highest id may live only in the Timeline.
	old := tp.Page{Timeline: []tp.Group{{Date: "2025-12-01", Lines: []string{"- next [stated]: ~~x~~ (dropped: y) <!-- i:n41 t:2025-12-01T10:00:00Z -->"}}}}
	old, _ = tp.Apply(old, tp.Update{Date: "2026-01-15", Ops: []tp.Op{add("x")}})
	if got := old.Now.Next[0].ID; got != "n42" {
		t.Fatalf("id after a Timeline-only n41 = %s, want n42", got)
	}
}

func items(prefix string, from, to int, tag string) []tp.Item {
	var out []tp.Item
	for k := from; k <= to; k++ {
		out = append(out, tp.Item{ID: fmt.Sprintf("n%d", k), Text: fmt.Sprintf("%s %d", prefix, k), Tag: tag, Refs: ref(k), At: day(1).Add(time.Duration(k) * time.Hour)})
	}
	return out
}

func TestApplyCapOrder(t *testing.T) {
	isolate(t)
	next := []tp.Item{
		{ID: "n1", Text: "oldest stated", Tag: "stated", Refs: ref(1), At: day(1)},
		{ID: "n2", Text: "a suggestion", Tag: "suggested", Refs: ref(2)},
		{ID: "n3", Text: "stated two", Tag: "stated", Refs: ref(3), At: day(2)},
		{ID: "n4", Text: "an inference", Tag: "inferred", Refs: ref(4), At: day(9)},
		{ID: "n5", Text: "accepted", Tag: "accepted", Refs: ref(5), At: day(3)},
		{ID: "n6", Text: "observed, no time", Tag: "observed", Refs: ref(6)},
	}
	next = append(next, items("stated", 7, 10, "stated")...)
	p := tp.Page{Now: tp.Now{Next: next}}
	var ops []tp.Op
	for k := 11; k <= 14; k++ {
		ops = append(ops, tp.Op{Kind: "next", Action: "add", Text: fmt.Sprintf("new %d", k), Tag: "stated", Refs: ref(k), At: day(20)})
	}
	got, st := tp.Apply(p, tp.Update{Date: "2026-01-20", Ops: ops})
	want := []string{
		"- next [suggested]: a suggestion (moved: cap) s:cccc0003#2 <!-- i:n2 -->",
		"- next [inferred]: an inference (moved: cap) s:cccc0003#4 <!-- i:n4 t:2026-01-09T12:00:00Z -->",
		"- next [observed]: observed, no time (moved: cap) s:cccc0003#6 <!-- i:n6 -->",
		"- next [stated]: oldest stated (moved: cap) s:cccc0003#1 <!-- i:n1 t:2026-01-01T12:00:00Z -->",
	}
	if len(got.Timeline) != 1 || !reflect.DeepEqual(got.Timeline[0].Lines, want) {
		t.Fatalf("moved lines =\n%s\nwant\n%s", strings.Join(got.Timeline[0].Lines, "\n"), strings.Join(want, "\n"))
	}
	if len(got.Now.Next) != tp.NextCap || st.Moved != 4 || st.Added != 4 {
		t.Fatalf("Next has %d items, stats %+v", len(got.Now.Next), st)
	}
	// The same rule holds under Open questions, where proposals render with
	// the assistant's label: a new suggestion moves before any stated
	// question, however old.
	open := items("question", 1, 10, "stated")
	got, _ = tp.Apply(tp.Page{Now: tp.Now{Open: open}}, tp.Update{Date: "2026-01-20", Ops: []tp.Op{{Kind: "question", Action: "add", Text: "Try BM25?", Tag: "suggested", Refs: ref(11)}}})
	if !reflect.DeepEqual(got.Now.Open, open) || got.Timeline[0].Lines[0] != "- question [suggested]: Try BM25? (moved: cap) s:cccc0003#11 <!-- i:n11 -->" {
		t.Fatalf("Open cap moved a stated question:\n%s", tp.Render(got))
	}
}

func TestApplyDecisionOverCap(t *testing.T) {
	isolate(t)
	p := tp.Page{Now: tp.Now{Decisions: items("decision", 1, 40, "stated"), Open: items("question", 41, 50, "stated")}}
	got, st := tp.Apply(p, tp.Update{Date: "2026-01-20", Ops: []tp.Op{{Kind: "decision", Action: "add", Text: "Keep one lock", Tag: "accepted", Refs: ref(51), At: day(20)}}})
	kept := tp.Item{ID: "n51", Text: "Keep one lock", Tag: "accepted", Refs: ref(51), At: day(20), Code: tp.OverCapCode}
	if !reflect.DeepEqual(got.Now.Decisions, p.Now.Decisions) || len(got.Now.Open) != 11 || !reflect.DeepEqual(got.Now.Open[10], kept) || st.Moved != 0 || got.Timeline != nil {
		t.Fatalf("decision over cap: stats %+v\n%s", st, tp.Render(got))
	}
	line := "- eva-md: decision over cap, kept: Keep one lock [accepted] s:cccc0003#51 <!-- i:n51 t:2026-01-20T12:00:00Z -->"
	if body := tp.Render(got); !strings.Contains(body, "### Open questions\n") || !strings.Contains(body, "\n"+line+"\n") {
		t.Fatalf("missing %q in\n%s", line, body)
	}
	// Questions past the cap move; the kept decision never does.
	got, st = tp.Apply(got, tp.Update{Date: "2026-01-21", Ops: []tp.Op{
		{Kind: "question", Action: "add", Text: "One more?", Tag: "stated", Refs: ref(52), At: day(21)},
		{Kind: "decision", Action: "add", Text: "And another", Tag: "stated", Refs: ref(53), At: day(21)},
	}})
	if len(got.Now.Open) != 12 || st.Moved != 1 || !strings.Contains(got.Timeline[0].Lines[0], "question 41 (moved: cap)") {
		t.Fatalf("second pass: stats %+v\n%s", st, tp.Render(got))
	}
	for _, it := range got.Now.Open[len(got.Now.Open)-3:] {
		if it.Text != "One more?" && it.Code != tp.OverCapCode {
			t.Fatalf("decision lost its code: %+v", it)
		}
	}
	// A kept decision that is superseded is logged as a decision and keeps
	// its place under Open questions.
	got, _ = tp.Apply(got, tp.Update{Date: "2026-01-22", Ops: []tp.Op{{ID: "n51", Kind: "decision", Action: "superseded", Replacement: "Keep two locks", Tag: "accepted", Refs: ref(54)}}})
	line = "- decision [accepted]: ~~eva-md: decision over cap, kept: Keep one lock~~ → Keep two locks (superseded 2026-01-22) s:cccc0003#51 s:cccc0003#54 <!-- i:n51 t:2026-01-20T12:00:00Z -->"
	if lines := got.Timeline[len(got.Timeline)-1].Lines; !reflect.DeepEqual(lines, []string{line}) {
		t.Fatalf("superseded kept decision = %q, want %q", lines, line)
	}
	i := slices.IndexFunc(got.Now.Open, func(it tp.Item) bool { return it.ID == "n51" })
	if i < 0 || got.Now.Open[i].Text != "Keep two locks" || got.Now.Open[i].Code != tp.OverCapCode {
		t.Fatalf("superseded kept decision left Open = %+v", got.Now.Open)
	}
	roundTrip(t, got)
}

func TestApplyKeepNow(t *testing.T) {
	isolate(t)
	p := actionsPage()
	u := actionsUpdate()
	u.KeepNow = true
	got, st := tp.Apply(p, u)
	if !reflect.DeepEqual(got.Now, p.Now) {
		t.Fatalf("KeepNow changed the Now:\n%s", tp.Render(got))
	}
	want := []string{"- decision [stated]: Use JSONL s:bbbb0002#4", "- finding [observed]: a 2 MB page parses in 0.5 ms s:bbbb0002#9"}
	if len(got.Timeline) != 2 || !reflect.DeepEqual(got.Timeline[1], tp.Group{Date: "2026-01-15", Lines: want}) {
		t.Fatalf("KeepNow Timeline = %#v", got.Timeline)
	}
	if want := (tp.Stats{Added: 2, Duplicate: 1}); st != want {
		t.Fatalf("stats = %+v, want %+v", st, want)
	}
}

func TestApplyTimelineGroups(t *testing.T) {
	isolate(t)
	p := tp.Page{Timeline: []tp.Group{
		{Date: "2026-01-14", Lines: []string{"- done [stated]: a s:a#1"}},
		{Date: "2026-01-15", Lines: []string{"- done [stated]: b s:a#2"}},
	}}
	event := func(text string) []tp.Event {
		return []tp.Event{{Kind: "change", Text: text, Tag: "observed", Refs: []string{"s:a#9"}}}
	}
	// An older date starts a new group at the end.
	got, _ := tp.Apply(p, tp.Update{Date: "2026-01-14", Events: event("late line")})
	want := append(p.Timeline[:2:2], tp.Group{Date: "2026-01-14", Lines: []string{"- change [observed]: late line s:a#9"}})
	if !reflect.DeepEqual(got.Timeline, want) {
		t.Fatalf("out-of-order date: %#v", got.Timeline)
	}
	// The last group's date takes the lines; existing lines never change.
	got, _ = tp.Apply(got, tp.Update{Date: "2026-01-14", Events: event("same day")})
	if len(got.Timeline) != 3 || len(got.Timeline[2].Lines) != 2 || !reflect.DeepEqual(got.Timeline[:2], p.Timeline) {
		t.Fatalf("same date: %#v", got.Timeline)
	}
	// Nothing to append adds no group.
	if got, _ := tp.Apply(p, tp.Update{Date: "2026-01-16", Events: event("")}); !reflect.DeepEqual(got, p) {
		t.Fatalf("an empty event changed the page: %#v", got)
	}
	// An update without a YYYY-MM-DD date files its lines under the last
	// group's date, and counts them.
	for i, date := range []string{"", " ", "Jan 16", "2026-1-16", "2026-01-16\n## Notes"} {
		text := fmt.Sprintf("undated %d", i)
		got, st := tp.Apply(p, tp.Update{Date: date, Events: event(text)})
		want := append(p.Timeline[:1:1], tp.Group{Date: "2026-01-15", Lines: []string{"- done [stated]: b s:a#2", "- change [observed]: " + text + " s:a#9"}})
		if !reflect.DeepEqual(got.Timeline, want) || st != (tp.Stats{Added: 1, Undated: 1}) {
			t.Errorf("date %q: stats %+v, Timeline %#v", date, st, got.Timeline)
		}
	}
	// On a page without a group it has nowhere to file them, so it changes
	// nothing; without lines to write it still applies.
	bare := tp.Page{Now: tp.Now{Next: []tp.Item{{ID: "n1", Text: "a", Tag: "stated"}}}}
	if got, st := tp.Apply(bare, tp.Update{Summary: "s", Ops: []tp.Op{{ID: "n1", Kind: "next", Action: "done"}}, Events: event("x")}); !reflect.DeepEqual(got, bare) || st != (tp.Stats{Undated: 2}) {
		t.Errorf("undated update on a page without a group: stats %+v\n%s", st, tp.Render(got))
	}
	if got, st := tp.Apply(bare, tp.Update{Summary: "s"}); got.Now.Summary != "s" || got.Timeline != nil || st != (tp.Stats{}) {
		t.Errorf("undated update without lines: stats %+v\n%s", st, tp.Render(got))
	}
}

// TestApplyNumbersHandWrittenItems checks that an item a person wrote
// without an id gets one, so a later pass can name it.
func TestApplyNumbersHandWrittenItems(t *testing.T) {
	isolate(t)
	body := "## Now\n" + tp.NowStart + "\n### Next\n- Write the docs\n- Ship it [stated] s:a#1 <!-- i:n2 -->\n\n### Open questions\n- Which host?\n\n### Decisions in force\n- Use Go [stated]\n" + tp.NowEnd + "\n\n## Timeline\n### 2026-01-14\n- done [stated]: x <!-- i:n4 -->\n"
	p, err := tp.Parse(body)
	if err != nil {
		t.Fatal(err)
	}
	u := tp.Update{Date: "2026-01-15", Ops: []tp.Op{{Kind: "next", Action: "add", Text: "Add tests", Tag: "stated"}}}
	got, st := tp.Apply(p, u)
	want := tp.Now{
		Next:      []tp.Item{{ID: "n5", Text: "Write the docs"}, {ID: "n2", Text: "Ship it", Tag: "stated", Refs: []string{"s:a#1"}}, {ID: "n8", Text: "Add tests", Tag: "stated"}},
		Open:      []tp.Item{{ID: "n6", Text: "Which host?"}},
		Decisions: []tp.Item{{ID: "n7", Text: "Use Go", Tag: "stated"}},
	}
	if !reflect.DeepEqual(got.Now, want) || st != (tp.Stats{Added: 1, Numbered: 3}) {
		t.Fatalf("Apply = (stats %+v)\n%s", st, tp.Render(got))
	}
	if again, st := tp.Apply(got, u); !reflect.DeepEqual(again, got) || st != (tp.Stats{Duplicate: 1}) {
		t.Fatalf("second Apply changed the page (stats %+v):\n%s", st, tp.Render(again))
	}
	roundTrip(t, got)
	later, st := tp.Apply(got, tp.Update{Date: "2026-01-16", Ops: []tp.Op{{ID: "n5", Kind: "next", Action: "done"}}})
	if st.Retired != 1 || later.Timeline[1].Lines[0] != "- done: Write the docs <!-- i:n5 -->" {
		t.Fatalf("done on a numbered item: stats %+v\n%s", st, tp.Render(later))
	}
	if kept, _ := tp.Apply(p, tp.Update{Date: "2026-01-15", KeepNow: true}); !reflect.DeepEqual(kept.Now, p.Now) {
		t.Fatalf("KeepNow numbered items:\n%s", tp.Render(kept))
	}
}

func TestApplyReplacesDone(t *testing.T) {
	isolate(t)
	p := actionsPage()
	var done []tp.Item
	for k := 1; k <= 10; k++ {
		done = append(done, tp.Item{ID: "n9", Text: fmt.Sprintf("done %d", k), Tag: "observed", Refs: ref(k)})
	}
	done[1].Text = " \n "
	got, _ := tp.Apply(p, tp.Update{Date: "2026-01-15", Done: done})
	if len(got.Now.Done) != tp.DoneCap || got.Now.Done[1].Text != "done 3" || got.Now.Done[0].ID != "" {
		t.Fatalf("Done = %#v", got.Now.Done)
	}
	if got.Now.Summary != p.Now.Summary || got.Now.LastChat != p.Now.LastChat {
		t.Fatal("an empty summary or last chat replaced the old one")
	}
	if got, _ := tp.Apply(p, tp.Update{Date: "2026-01-15"}); got.Now.Done != nil {
		t.Fatalf("Done not replaced each pass: %#v", got.Now.Done)
	}
}

func TestApplyTwiceChangesNothing(t *testing.T) {
	isolate(t)
	p := actionsPage()
	p.Now.Next = append(p.Now.Next, items("stated", 20, 27, "stated")...)
	p.Now.Decisions = append(p.Now.Decisions, items("decision", 30, 67, "stated")...)
	u := actionsUpdate()
	u.Ops = append(u.Ops,
		// Next reaches its cap, so this suggestion moves at once.
		tp.Op{Kind: "next", Action: "add", Text: "Maybe add a daemon", Tag: "suggested", Refs: ref(90)},
		tp.Op{Kind: "decision", Action: "add", Text: "Past the decision cap", Tag: "stated", Refs: ref(91), At: t1},
		tp.Op{Kind: "decision", Action: "add", Text: "Past the decision cap", Tag: "stated", Refs: ref(91), At: t1},
		tp.Op{ID: "n5", Kind: "decision", Action: "edit", Text: "One lock per wiki, held briefly", Tag: "accepted"},
	)
	before := tp.Render(p)
	once, first := tp.Apply(p, u)
	twice, second := tp.Apply(once, u)
	if !reflect.DeepEqual(once, twice) || tp.Render(once) != tp.Render(twice) {
		t.Fatalf("second Apply changed the page:\n%s\nthen\n%s", tp.Render(once), tp.Render(twice))
	}
	if first.Moved == 0 || first.Duplicate == 0 || second.Added+second.Edited+second.Retired+second.Moved+second.Numbered != 0 || second.Duplicate == 0 {
		t.Fatalf("stats: first %+v, second %+v", first, second)
	}
	if i := slices.IndexFunc(once.Now.Decisions, func(it tp.Item) bool { return it.ID == "n5" }); i < 0 || once.Now.Decisions[i].Tag != "accepted" {
		t.Fatalf("the edit of n5 to accepted was lost: %+v", once.Now.Decisions)
	}
	if tp.Render(p) != before {
		t.Fatal("Apply changed its input page")
	}
	roundTrip(t, once)
}
