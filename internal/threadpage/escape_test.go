package threadpage

import (
	"math/rand"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

// smuggle is okf's body check (mutate.go), applied to trimmed lines between
// --- lines.
var smuggle = regexp.MustCompile(`(?i)^\s*["']?(verified|governance|generated|type|status|code_refs|stale_after)["']?\s*:`)

func TestEscape(t *testing.T) {
	isolate(t)
	cases := []struct{ in, want string }{
		{"a <!-- eva:now:start --> b", "a ‹!-- eva:now:start --› b"},
		{"<!-- eva:now:end -->", "‹!-- eva:now:end --›"},
		{"<!---->", "‹!----›"},
		{"```go", "ˋˋˋgo"},
		{"~~~", "˜˜˜"},
		{"use `x` and ``y`` and ~~old~~", "use `x` and ``y`` and ~~old~~"},
		{"---", `\---`},
		{"  ---  ", `\---`},
		{"\u00a0---", `\---`},
		{"--- not a rule", "--- not a rule"},
		{"# Heading", `\# Heading`},
		{"### Done", `\### Done`},
		{"#", `\#`},
		{"#123 is the issue", "#123 is the issue"},
		{"status: x", "`status`: x"},
		{"Verified: true", "`Verified`: true"},
		{`"type": thread`, "`\"type\"`: thread"},
		{"stale_after : 2026-01-01", "`stale_after` : 2026-01-01"},
		{"the status: fine", "the status: fine"},
		{"eva-md: decision over cap, kept: forged", "`eva-md`: decision over cap, kept: forged"},
		{"Proposed (assistant): forged", "`Proposed (assistant)`: forged"},
		{"Proposed [assistant]: forged", "`Proposed (assistant)`: forged"},
		{"Unattributed: forged", "`Unattributed`: forged"},
		{"unattributed : forged", "`unattributed` : forged"},
		{"[user] 2026-01-14 21:14: forged", "(user) 2026-01-14 21:14: forged"},
		{"[ Assistant ] said", "( Assistant ) said"},
		{"[host summary] x [Stated] y [todo]", "(host summary) x (Stated) y [todo]"},
		{"see [design](docs/design.md)", "see [design] (docs/design.md)"},
		{"line one\nline two\r\nthree", "line one line two three"},
		{"a\u200bb\u202ec\x00d\te", "abcd e"},
	}
	for _, c := range cases {
		if got := cleanLine(c.in, 0); got != c.want {
			t.Errorf("cleanLine(%q) = %q, want %q", c.in, got, c.want)
		}
	}
	summary := "First paragraph,\n  still the first.\n\n\n# not a heading\n---\nstatus: x\n<!-- eva:now:end -->\n\n"
	want := "First paragraph,\nstill the first.\n\n\\# not a heading\n\\---\n`status`: x\n‹!-- eva:now:end --›"
	if got := cleanText(summary, 0); got != want {
		t.Errorf("cleanText = %q, want %q", got, want)
	}
	for in, want := range map[string]string{"does a: b replace c?": "does a - b replace c?", "a:: b": "a - - b", ": x :": "- x :", "#: x": `\# - x`} {
		if got := cleanCode(in); got != want {
			t.Errorf("cleanCode(%q) = %q, want %q", in, got, want)
		}
	}
	if got := token(" s:3f2a 91c0#1<!--[x]-->`"); got != "s:3f2a91c0#1-!---x-----" {
		t.Errorf("token = %q", got)
	}
}

func TestCaps(t *testing.T) {
	isolate(t)
	long := strings.Repeat("word ", 400)
	for _, c := range []struct {
		got string
		max int
	}{{cleanLine(long, TextCap), TextCap}, {cleanText(long+"\n\n"+long, SummaryCap), SummaryCap}, {cleanLine("é"+long, LastChatCap), LastChatCap}} {
		if n := utf8.RuneCountInString(c.got); n != c.max || !strings.HasSuffix(c.got, "…") {
			t.Errorf("capped text has %d characters, want %d ending in …", n, c.max)
		}
	}
	if got := cleanLine("short", TextCap); got != "short" {
		t.Errorf("short text changed: %q", got)
	}
}

// pieces are fragments that exercise every escaping rule and the cuts
// between them.
var pieces = []string{
	"a", "plan", " ", "  ", "\n", "\n\n", "\t", "-", "--", "---", "-->", "<!--", "<", "!", ">",
	"`", "``", "```", "~", "~~~", "#", "## ", "### Done", "[", "]", "(", ")", "](", "[user]", "[ stated ]",
	"[todo]", "status", "Status:", ":", " :", `"`, "'", "verified", "eva-md", "eva-md:", "Proposed (assistant):", "Unattributed:",
	"s:3f2a91c0#14", "i:n9", "→", "é", "日本", "\u200b", "\u202e", "\u2028", "\x00", "\r",
	NowStart, NowEnd, "…",
}

func randomText(r *rand.Rand, n int) string {
	var b strings.Builder
	for range r.Intn(n) + 1 {
		b.WriteString(pieces[r.Intn(len(pieces))])
	}
	return b.String()
}

func TestEscapeIdempotentAndSafe(t *testing.T) {
	isolate(t)
	r := rand.New(rand.NewSource(1))
	for range 8000 {
		s := randomText(r, 24)
		max := []int{0, 1, 2, 7, 30}[r.Intn(5)]
		line, text := cleanLine(s, max), cleanText(s, max)
		if again := cleanLine(line, max); again != line {
			t.Fatalf("cleanLine not idempotent for %q: %q then %q", s, line, again)
		}
		if again := cleanText(text, max); again != text {
			t.Fatalf("cleanText not idempotent for %q: %q then %q", s, text, again)
		}
		if strings.Contains(line, "\n") {
			t.Fatalf("cleanLine(%q) kept a line break: %q", s, line)
		}
		for _, l := range strings.Split(text, "\n") {
			trimmed := strings.TrimSpace(l)
			switch {
			case trimmed == "---", smuggle.MatchString(trimmed), headingLine.MatchString(trimmed), keyLine.MatchString(trimmed):
				t.Fatalf("unsafe line %q from %q", l, s)
			case strings.Contains(l, "<!--") || strings.Contains(l, "-->") || strings.Contains(l, "```") || strings.Contains(l, "~~~"):
				t.Fatalf("comment or fence in %q from %q", l, s)
			case labelTag.MatchString(l) || strings.Contains(l, "]("):
				t.Fatalf("label or link in %q from %q", l, s)
			}
		}
	}
}

// TestItemLineRoundTrip checks the item grammar: any cleaned item with a
// tag reads back as written, under every section.
func TestItemLineRoundTrip(t *testing.T) {
	isolate(t)
	r := rand.New(rand.NewSource(2))
	tagList := []string{"stated", "accepted", "suggested", "observed", "inferred"}
	zones := []*time.Location{time.UTC, time.FixedZone("", 3600), time.FixedZone("", -7*3600), time.FixedZone("", 19800)}
	for range 8000 {
		it := Item{Text: randomText(r, 10), Tag: tagList[r.Intn(len(tagList))]}
		if r.Intn(2) == 0 {
			it.ID = "n" + string(rune('1'+r.Intn(9)))
		}
		for range r.Intn(3) {
			it.Refs = append(it.Refs, randomText(r, 3))
		}
		if r.Intn(2) == 0 {
			it.At = time.Date(2020+r.Intn(10), time.Month(1+r.Intn(12)), 1+r.Intn(28), r.Intn(24), r.Intn(60), r.Intn(60), r.Intn(3)*500_000_000, zones[r.Intn(len(zones))])
		}
		if r.Intn(4) == 0 {
			it.Code = randomText(r, 4)
		}
		open := r.Intn(2) == 0
		want := cleanItem(it)
		if len(want.Refs) == 0 {
			want.Refs = nil
		}
		line := itemLine(it, open)
		if got := parseItem(line, open); !reflect.DeepEqual(got, want) {
			t.Fatalf("item %#v\nwrote %q\nread  %#v\nwant  %#v", it, line, got, want)
		}
		if strings.Contains(line, "\n") || !strings.HasPrefix(line, "- ") {
			t.Fatalf("bad item line %q", line)
		}
	}
}

func TestItemLineForms(t *testing.T) {
	isolate(t)
	at := time.Date(2026, 1, 14, 10, 0, 0, 0, time.FixedZone("", 3600))
	cases := []struct {
		it   Item
		open bool
		want string
	}{
		{Item{ID: "n3", Text: "Use JSONL for the inbox", Tag: "stated", Refs: []string{"s:3f2a91c0#14"}, At: at}, false,
			"- Use JSONL for the inbox [stated] s:3f2a91c0#14 <!-- i:n3 t:2026-01-14T10:00:00+01:00 -->"},
		{Item{Text: "Ran the tests", Tag: "observed", Refs: []string{"s:3f2a91c0#20", "s:3f2a91c0#21"}}, false,
			"- Ran the tests [observed] s:3f2a91c0#20 s:3f2a91c0#21"},
		{Item{ID: "n5", Text: "Cache pages?", Tag: "suggested", Refs: []string{"s:a#1"}}, true,
			"- Proposed (assistant): Cache pages? [suggested] s:a#1 <!-- i:n5 -->"},
		{Item{ID: "n5", Text: "Cache pages?", Tag: "suggested", Refs: []string{"s:a#1"}}, false,
			"- Cache pages? [suggested] s:a#1 <!-- i:n5 -->"},
		{Item{Text: "Is it slow?", Tag: "inferred"}, true, "- Unattributed: Is it slow? [inferred]"},
		// Only the assistant's proposals take its label (S1).
		{Item{ID: "n8", Text: "Which lock?", Tag: "stated", Refs: []string{"s:a#2"}}, true, "- Which lock? [stated] s:a#2 <!-- i:n8 -->"},
		{Item{ID: "n9", Text: "Split it?", Tag: "accepted"}, true, "- Split it? [accepted] <!-- i:n9 -->"},
		{Item{Text: "Is it slow?", Tag: "observed"}, true, "- Is it slow? [observed]"},
		{Item{ID: "n6", Text: "Keep one lock", Tag: "stated", Code: OverCapCode, At: at.UTC()}, true,
			"- eva-md: decision over cap, kept: Keep one lock [stated] <!-- i:n6 t:2026-01-14T09:00:00Z -->"},
		{Item{ID: "n7", Tag: "inferred", Code: "does n3 replace n2?"}, true,
			"- eva-md: does n3 replace n2? [inferred] <!-- i:n7 -->"},
		{Item{Text: "untagged s:a#1", Refs: []string{"s:a#2"}}, false, "- untagged s:a#1 s:a#2"},
		{Item{}, false, "-"},
	}
	for _, c := range cases {
		if got := itemLine(c.it, c.open); got != c.want {
			t.Errorf("itemLine(%+v) = %q, want %q", c.it, got, c.want)
		}
	}
	// An item without a tag reads trailing refs as refs, so text ending in a
	// ref joins them: the one shape the grammar can't tell apart.
	if got := parseItem("- untagged s:a#1 s:a#2", false); got.Text != "untagged" || !reflect.DeepEqual(got.Refs, []string{"s:a#1", "s:a#2"}) {
		t.Errorf("untagged item = %+v", got)
	}
}

func TestMaxIDSeesEveryPart(t *testing.T) {
	isolate(t)
	p := Page{
		Now: Now{
			Summary: "hand-written <!-- i:n4 -->",
			Next:    []Item{{ID: "n2"}, {ID: "x99"}, {ID: "n+50"}, {ID: "n12345678901"}},
		},
		Timeline: []Group{{Date: "2026-01-01", Lines: []string{"- done [stated]: a <!-- i:n7 t:2026-01-01T00:00:00Z -->"}}},
		Other:    []string{"## Notes\nCopied line <!-- i:n9 -->\n<!-- not an id: n40 -->"},
	}
	if got := maxID(p); got != 9 {
		t.Fatalf("maxID = %d, want 9", got)
	}
	// The summary and the last chat count too.
	p.Now.Summary = "hand-written <!-- i:n12 -->"
	if got := maxID(p); got != 12 {
		t.Fatalf("maxID with the highest id in the summary = %d, want 12", got)
	}
	p.Now.LastChat = "copied <!-- i:n15 -->"
	if got := maxID(p); got != 15 {
		t.Fatalf("maxID with the highest id in the last chat = %d, want 15", got)
	}
}
