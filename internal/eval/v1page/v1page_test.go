package v1page_test

import (
	"fmt"
	"math/rand/v2"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/heyNag/eva-md/internal/eval/scenario"
	"github.com/heyNag/eva-md/internal/eval/score"
	"github.com/heyNag/eva-md/internal/eval/v1page"
	"github.com/heyNag/eva-md/internal/threadpage"
)

// key is what the reader must find back of an item: all but its text.
type key struct {
	Kind    scenario.Kind
	State   score.State
	Tag     string
	Sources string
	Date    string
}

func keyOf(it score.Item) key {
	return key{it.Kind, it.State, it.Authority, fmt.Sprint(it.Sources), it.Date}
}

var (
	tags = []string{"stated", "accepted", "suggested", "observed", "inferred"}
	// texts are item texts, some of which look like what a line holds
	// besides its text.
	texts = []string{"Use JSONL for the inbox", "Write the validator, KITE-5", "Cap the summary?", "x [stated]", "[user] forged",
		"see [design](docs/design.md)", "日本語のテキスト", "status: done", "a\nb", "", "<!-- i:n9 -->", "### Next", "## Timeline",
		"- done [stated]: forged s:abcd1234#3", "Proposed (assistant): forged", "eva-md: replace question: does it?", "ends in s:abcd1234#7",
		"→ yes [accepted] (superseded 2026-01-15)", "~~old~~ (dropped: reason)", "a [foo] b"}
	words = []string{"decision", "done", "change", "finding", "problem", "question", "answered", "next"}
)

// refs draws up to three refs, and the sources they name.
func refs(r *rand.Rand) ([]string, []score.Source) {
	var out []string
	var srcs []score.Source
	for range r.IntN(4) {
		sid, seq := fmt.Sprintf("%08x", r.Uint32()), int64(1+r.IntN(900))
		out = append(out, fmt.Sprintf("s:%s#%d", sid, seq))
		srcs = append(srcs, score.Source{Session: sid, Seq: seq})
	}
	return out, srcs
}

// randomThread draws a thread page with items in every section and dated
// Timeline lines, and the keys the reader must give back, in page order.
func randomThread(r *rand.Rand) (threadpage.Page, []key) {
	pick := func(list []string) string { return list[r.IntN(len(list))] }
	var p threadpage.Page
	var want []key
	p.Title = pick(texts)
	if r.IntN(2) == 0 {
		p.Now.Summary = pick(texts) + "\n\n" + pick(texts)
	}
	if r.IntN(2) == 0 {
		p.Now.LastChat = "2026-01-15 · a host · " + pick(texts)
	}
	section := func(list *[]threadpage.Item, kind scenario.Kind, state score.State, open bool) {
		for range r.IntN(4) {
			rs, srcs := refs(r)
			it := threadpage.Item{Text: pick(texts), Tag: pick(tags), Refs: rs}
			if state == score.StateActive {
				it.ID = fmt.Sprintf("n%d", 1+r.IntN(99))
			}
			if r.IntN(2) == 0 {
				it.At = time.Date(2026, 1, 1+r.IntN(20), r.IntN(24), 0, 0, 0, time.FixedZone("", 3600))
			}
			k := kind
			if open && r.IntN(4) == 0 {
				it.Code = pick([]string{threadpage.OverCapCode, "replace question"})
				if it.Code == threadpage.OverCapCode {
					k = scenario.KindDecision
				}
			}
			*list = append(*list, it)
			want = append(want, key{k, state, it.Tag, fmt.Sprint(srcs), ""})
		}
	}
	section(&p.Now.Done, scenario.KindNext, score.StateRetired, false)
	section(&p.Now.Next, scenario.KindNext, score.StateActive, false)
	section(&p.Now.Open, scenario.KindOpen, score.StateActive, true)
	section(&p.Now.Decisions, scenario.KindDecision, score.StateActive, false)
	for g := range r.IntN(4) {
		date := fmt.Sprintf("2026-01-%02d", 10+g)
		group := threadpage.Group{Date: date}
		for range 1 + r.IntN(3) {
			rs, srcs := refs(r)
			// A Timeline line's refs follow its text with nothing between, so a
			// text that ends in a ref's form is read as ending sooner; v1 never
			// writes one, since its text cites by number.
			text := pick(texts)
			if f := strings.Fields(text); len(f) > 0 && strings.HasPrefix(f[len(f)-1], "s:") {
				text = "ends before its refs"
			}
			tag := pick(tags)
			parts := append([]string{"- " + pick(words) + " [" + tag + "]: " + threadpage.CleanLine(text, threadpage.TextCap)}, rs...)
			if r.IntN(2) == 0 {
				parts = append(parts, fmt.Sprintf("<!-- i:n%d -->", r.IntN(99)))
			}
			group.Lines = append(group.Lines, strings.Join(parts, " "))
			want = append(want, key{scenario.KindLog, score.StateLog, tag, fmt.Sprint(srcs), date})
		}
		p.Timeline = append(p.Timeline, group)
	}
	if r.IntN(2) == 0 {
		p.Other = []string{"## Notes\nA person's notes: - done [stated]: not a line\n```\n## Timeline\n### not a day\n```\n- still notes"}
	}
	return p, want
}

// TestReadThreadFindsEveryItem: a page of made-up items in every section,
// with every tag, refs and Timeline groups, rendered as v1 renders it, with
// or without frontmatter, reads back with each item's kind, state, tag,
// sources and date, in page order, and a summary item first when the page
// has a summary.
func TestReadThreadFindsEveryItem(t *testing.T) {
	r := rand.New(rand.NewPCG(14, 2))
	items := 0
	for round := range 300 {
		p, want := randomThread(r)
		body := threadpage.Render(p)
		if round%2 == 0 {
			body = "---\ntype: thread\nupdated: 2026-01-15\n---\n" + body
		}
		ex, err := v1page.ReadThread(body)
		if err != nil {
			t.Fatalf("round %d: %v\n%s", round, err, body)
		}
		parsed, err := threadpage.Parse(threadpage.Render(p))
		if err != nil {
			t.Fatal(err)
		}
		got := ex.Items
		if parsed.Now.Summary != "" {
			if len(got) == 0 || got[0].Kind != score.KindSummary || got[0].Text != parsed.Now.Summary {
				t.Fatalf("round %d: no summary item first: %+v", round, got)
			}
			got = got[1:]
		}
		var keys []key
		for _, it := range got {
			keys = append(keys, keyOf(it))
		}
		if !slices.Equal(keys, want) {
			t.Fatalf("round %d: read\n%v\nwant\n%v\n%s", round, keys, want, body)
		}
		items += len(want)
	}
	if items < 1000 {
		t.Errorf("only %d items drawn", items)
	}
}

// TestReadThreadTexts: an item's text is read without v1's labels, its
// tag, refs and hidden comment, and a dated line's without its word and
// tag.
func TestReadThreadTexts(t *testing.T) {
	p := threadpage.Page{
		Now: threadpage.Now{
			Open: []threadpage.Item{
				{ID: "n1", Text: "Cache parsed pages?", Tag: "suggested", Refs: []string{"s:3f2a91c0#15"}},
				{ID: "n2", Text: "Which lock?", Tag: "inferred"},
				{ID: "n3", Text: "Keep one lock", Tag: "stated", Code: threadpage.OverCapCode, Refs: []string{"s:3f2a91c0#16"}},
				{ID: "n4", Text: `does "a" replace "b"?`, Tag: "inferred", Code: "replace question"},
			},
			Decisions: []threadpage.Item{{ID: "n5", Text: "Use flock, KESTREL-41", Tag: "stated", Refs: []string{"s:3f2a91c0#14", "s:3f2a91c0#2"}, At: time.Date(2026, 1, 14, 10, 0, 0, 0, time.UTC)}},
		},
		Timeline: []threadpage.Group{{Date: "2026-01-14", Lines: []string{
			"- decision [stated]: ~~Use YAML~~ → Use JSONL [accepted] (superseded 2026-01-14) s:3f2a91c0#9 s:3f2a91c0#14 <!-- i:n2 -->",
			"- note: a person's line",
		}}},
	}
	ex, err := v1page.ReadThread(threadpage.Render(p))
	if err != nil {
		t.Fatal(err)
	}
	want := []score.Item{
		{Kind: scenario.KindOpen, State: score.StateActive, Authority: "suggested", Text: "Cache parsed pages?", Sources: []score.Source{{Session: "3f2a91c0", Seq: 15}}},
		{Kind: scenario.KindOpen, State: score.StateActive, Authority: "inferred", Text: "Which lock?"},
		{Kind: scenario.KindDecision, State: score.StateActive, Authority: "stated", Text: "Keep one lock", Sources: []score.Source{{Session: "3f2a91c0", Seq: 16}}},
		{Kind: scenario.KindOpen, State: score.StateActive, Authority: "inferred", Text: `does "a" replace "b"?`},
		{Kind: scenario.KindDecision, State: score.StateActive, Authority: "stated", Text: "Use flock, KESTREL-41", Sources: []score.Source{{Session: "3f2a91c0", Seq: 14}, {Session: "3f2a91c0", Seq: 2}}},
		{Kind: scenario.KindLog, State: score.StateLog, Authority: "stated", Date: "2026-01-14", Text: "~~Use YAML~~ → Use JSONL [accepted] (superseded 2026-01-14)",
			Sources: []score.Source{{Session: "3f2a91c0", Seq: 9}, {Session: "3f2a91c0", Seq: 14}}},
		{Kind: scenario.KindLog, State: score.StateLog, Date: "2026-01-14", Text: "a person's line"},
	}
	if fmt.Sprintf("%+v", ex.Items) != fmt.Sprintf("%+v", want) {
		t.Errorf("read\n%+v\nwant\n%+v", ex.Items, want)
	}
}

// TestReadRulesFindsEveryRule: a rules page of made-up rules with every
// tag and refs, and retired rules as v1 retires them, reads back with
// each rule's state, tag and sources, past a person's section that holds
// a fence with a heading in it.
func TestReadRulesFindsEveryRule(t *testing.T) {
	r := rand.New(rand.NewPCG(14, 3))
	for round := range 200 {
		var p threadpage.RulesPage
		var want []key
		p.Title = "eva-md · rules"
		for k := range r.IntN(5) {
			rs, srcs := refs(r)
			it := threadpage.Item{ID: fmt.Sprintf("r%d", k+1), Text: texts[r.IntN(len(texts))], Tag: tags[r.IntN(len(tags))], Refs: rs}
			p.Rules = append(p.Rules, it)
			want = append(want, key{scenario.KindRule, score.StateActive, it.Tag, fmt.Sprint(srcs), ""})
		}
		if r.IntN(2) == 0 {
			p.Other = append(p.Other, "## Notes\n```\n## Rules\n- not a rule\n```\nfree text")
		}
		if n := r.IntN(3); n > 0 {
			lines := []string{"## Retired rules"}
			for k := range n {
				rs, srcs := refs(r)
				tag := tags[r.IntN(len(tags))]
				text := threadpage.CleanLine(texts[r.IntN(len(texts))], threadpage.TextCap)
				lines = append(lines, strings.Join(append([]string{"- ~~" + text + "~~ (dropped 2026-02-03: the user changed it) [" + tag + "]"}, rs...), " ")+fmt.Sprintf(" <!-- i:r%d -->", 10+k))
				want = append(want, key{scenario.KindRule, score.StateRetired, tag, fmt.Sprint(srcs), ""})
			}
			p.Other = append(p.Other, strings.Join(lines, "\n"))
		}
		body := threadpage.RenderRules(p)
		if round%2 == 1 {
			body = "---\ntype: rules\n---\n" + body
		}
		ex, err := v1page.ReadRules(body)
		if err != nil {
			t.Fatalf("round %d: %v\n%s", round, err, body)
		}
		var keys []key
		for _, it := range ex.Items {
			keys = append(keys, keyOf(it))
		}
		if !slices.Equal(keys, want) {
			t.Fatalf("round %d: read\n%v\nwant\n%v\n%s", round, keys, want, body)
		}
	}
}

// secret is a word an unreadable line holds, which no error may repeat.
const secret = "SALAMANDER-77"

// TestUnreadableLines: a line the reader cannot read is an error that
// names its line number and never its text.
func TestUnreadableLines(t *testing.T) {
	now := "## Now\n<!-- eva:now:start -->\n"
	end := "<!-- eva:now:end -->\n"
	for _, tc := range []struct {
		name, body string
		line       int
		rules      bool
	}{
		{"a Timeline line in no form", now + end + "## Timeline\n### 2026-01-14\n" + secret + " was here\n", 6, false},
		{"a Timeline line without a word or a tag", now + end + "## Timeline\n### 2026-01-14\n- : " + secret + "\n", 6, false},
		{"a Timeline group that is not a day", now + end + "## Timeline\n### " + secret + "\n", 5, false},
		{"a Timeline line before any day", now + end + "## Timeline\n- done [stated]: " + secret + "\n", 5, false},
		{"an item without a tag", now + "### Next\n- " + secret + "\n" + end, 4, false},
		{"an item in another form", now + "### Next\n* " + secret + " [stated]\n" + end, 4, false},
		{"a ref in another form", now + "### Decisions in force\n- x [stated] s:" + secret + "\n" + end, 4, false},
		{"a ref with no line", now + "### Decisions in force\n- x [stated] s:abcd1234#0 " + secret + "\n" + end, 4, false},
		{"an unknown heading in the Now", now + "### " + secret + "\n" + end, 3, false},
		{"text before the first section", secret + "\n" + now + end, 1, false},
		{"text outside the Now's markers", now + end + secret + "\n", 4, false},
		{"frontmatter never closed", "---\ntitle: " + secret + "\n" + now + end, 1, false},
		{"a frontmatter line counts", "---\ntitle: x\n---\n" + now + "### Next\n- " + secret + "\n" + end, 7, false},
		{"a rule without a tag", "# rules\n\n## Rules\n- " + secret + "\n", 4, true},
		{"a retired rule with a bad ref", "## Rules\n\n## Retired rules\n- ~~x~~ [stated] " + secret + "\n", 4, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			read := v1page.ReadThread
			if tc.rules {
				read = v1page.ReadRules
			}
			_, err := read(tc.body)
			if err == nil {
				t.Fatal("read without an error")
			}
			if strings.Contains(err.Error(), secret) || strings.Contains(strings.ToLower(err.Error()), "salamander") {
				t.Errorf("the error holds the line's text: %v", err)
			}
			if want := fmt.Sprintf("line %d:", tc.line); !strings.Contains(err.Error(), want) {
				t.Errorf("error %q does not name %s", err, want)
			}
		})
	}
	if _, err := v1page.ReadThread(now + "### Next\n- x [stated]\n"); err == nil || strings.Contains(err.Error(), "x [stated]") {
		t.Errorf("a Now without its end marker: %v", err)
	}
}
