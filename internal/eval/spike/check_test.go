package spike

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/heyNag/eva-md/internal/eval/scenario"
	"github.com/heyNag/eva-md/internal/ledger/event"
)

// scriptByID is the script id names.
func scriptByID(t *testing.T, id string) Script {
	t.Helper()
	for _, s := range Scripts() {
		if s.Scenario.ID == id {
			return s
		}
	}
	t.Fatalf("no script %s", id)
	return Script{}
}

// canned reads the canned answer testdata/answers/<form>/<name>.json.
func canned(t *testing.T, f Form, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "answers", string(f), name+".json"))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// passSession is the session pass n, from 1, of script s takes.
func passSession(s Script, n int) scenario.Session {
	return s.Scenario.Sessions[s.Scenario.Passes[n-1].Session]
}

// renderPass renders pass n of s with the items shown, in form f.
func renderPass(t *testing.T, s Script, n int, shown []Shown, f Form) Input {
	t.Helper()
	in, err := Render(passSession(s, n), shown, f)
	if err != nil {
		t.Fatal(err)
	}
	return in
}

// parse reads an answer that must be valid.
func parse(t *testing.T, data []byte, f Form) Answer {
	t.Helper()
	a, err := ParseAnswer(data, f)
	if err != nil {
		t.Fatalf("%s: %v", f, err)
	}
	return a
}

// codes are the codes of v's operations, "" for one accepted.
func codes(v Verdict) []string {
	out := make([]string, len(v.Ops))
	for i, j := range v.Ops {
		out[i] = j.Code
	}
	return out
}

// faithful are the scripts that have a faithful canned answer for each of
// their passes.
var faithful = []string{"v1.03", "v1.05", "v1.08", "assent.01", "assent.02"}

// TestFaithfulAnswersAreAcceptedWhole: every faithful canned answer, in
// both forms, is accepted whole, each pass judged against what the store
// shows after the passes before it, and every faithful file is read.
func TestFaithfulAnswersAreAcceptedWhole(t *testing.T) {
	for _, f := range Forms {
		read := map[string]bool{}
		for _, id := range faithful {
			s := scriptByID(t, id)
			var st Store
			for n := 1; n <= len(s.Scenario.Passes); n++ {
				name := fmt.Sprintf("%s.%d.faithful", id, n)
				read[name] = true
				in := renderPass(t, s, n, st.Shown(), f)
				v := Check(in, st.Shown(), parse(t, canned(t, f, name), f), f)
				if len(v.Ops) == 0 || v.Breaks {
					t.Errorf("%s %s: %d operations, breaks %v", f, name, len(v.Ops), v.Breaks)
				}
				for i, j := range v.Ops {
					if !j.Accepted() {
						t.Errorf("%s %s: operation %d refused %s at %s", f, name, i+1, j.Code, j.Place)
					}
					for _, sp := range j.Spans {
						if sp.RoleFixed {
							t.Errorf("%s %s: operation %d: span %d's role is fixed", f, name, i+1, sp.E)
						}
					}
				}
				if err := st.Apply(passSession(s, n), v); err != nil {
					t.Fatalf("%s %s: %v", f, name, err)
				}
			}
		}
		files, err := filepath.Glob(filepath.Join("testdata", "answers", string(f), "*.faithful.json"))
		if err != nil {
			t.Fatal(err)
		}
		for _, p := range files {
			if name := strings.TrimSuffix(filepath.Base(p), ".json"); !read[name] {
				t.Errorf("%s: %s is not checked", f, name)
			}
		}
		if len(files) != len(read) {
			t.Errorf("%s: %d faithful files, %d read", f, len(files), len(read))
		}
	}
}

// faultCodes and faultPlaces are what assent.01's canned faults are
// refused with, by form: one operation for each code the form can reach,
// between two good ones.
var (
	faultCodes = map[Form][]string{
		FormQuotes:   {"", "bad_shape", "unknown_target", "bad_entry", "quote_not_found", "ungrounded", "tag_too_low", ""},
		FormSegments: {"", "bad_shape", "unknown_target", "bad_entry", "bad_segment", "ungrounded", "tag_too_low", ""},
	}
	faultPlaces = map[Form][]string{
		FormQuotes:   {"", "kind", "id", "ev.1.e", "ev.1.q", "text", "ev", ""},
		FormSegments: {"", "kind", "id", "ev.1.e", "ev.1.s", "text", "ev", ""},
	}
)

// checkFaults checks assent.01's canned faults in form f.
func checkFaults(t *testing.T, f Form) (Input, Verdict) {
	t.Helper()
	in := renderPass(t, scriptByID(t, "assent.01"), 1, nil, f)
	return in, Check(in, nil, parse(t, canned(t, f, "assent.01.1.faults"), f), f)
}

// TestEachCodeIsReachedByACannedAnswer: the canned faults are refused with
// the codes and places expected, and between both forms every code of
// Codes is reached.
func TestEachCodeIsReachedByACannedAnswer(t *testing.T) {
	reached := map[string]bool{}
	for _, f := range Forms {
		_, v := checkFaults(t, f)
		if got := codes(v); !slices.Equal(got, faultCodes[f]) {
			t.Errorf("%s: codes %q, want %q", f, got, faultCodes[f])
		}
		for i, j := range v.Ops {
			if j.Place != faultPlaces[f][i] {
				t.Errorf("%s: operation %d is refused at %q, want %q", f, i+1, j.Place, faultPlaces[f][i])
			}
			reached[j.Code] = true
		}
		if v.Breaks {
			t.Errorf("%s: the faults break no bound of the schema", f)
		}
	}
	for _, c := range Codes {
		if !reached[c.ID] {
			t.Errorf("no canned answer reaches %s", c.ID)
		}
	}
}

// judgeOne checks an answer of the one operation op against in and shown,
// in form f.
func judgeOne(in Input, shown []Shown, op Op, f Form) Judged {
	return Check(in, shown, Answer{Ops: []Op{op}}, f).Ops[0]
}

func sq(e int64, q, as string) Span { return Span{E: e, Q: q, As: as} }

func add(kind, text string, ev ...Span) Op {
	admit := "decision"
	return Op{Op: "item.add", Kind: &kind, Text: &text, Admit: &admit, Ev: ev}
}

// TestQuoteMatch: a quote is found only word for word in its entry, white
// space aside, and 8 to 240 characters unless it is the whole text.
func TestQuoteMatch(t *testing.T) {
	in := renderPass(t, scriptByID(t, "assent.01"), 1, nil, FormQuotes)
	strict := "Shall I add a dry run, importer verify --strict-rows, before every import?"
	filler := in.Entries[len(in.Entries)-2].Text
	for _, c := range []struct {
		name  string
		e     int64
		q     string
		found bool
	}{
		{"exact", 6, strict, true},
		{"a part of eight characters", 6, "a dry ru", true},
		{"doubled space", 6, strings.Replace(strict, "dry run", "dry  run", 1), true},
		{"a tab and a line break", 6, strings.Replace(strict, "a dry run", "a\tdry\n  run", 1), true},
		{"space at either end", 6, "  " + strict + " ", true},
		{"a no-break space", 6, strings.Replace(strict, "dry run", "dry run", 1), true},
		{"off by a word", 6, strings.Replace(strict, "every", "each", 1), false},
		{"a word dropped", 6, strings.Replace(strict, "dry ", "", 1), false},
		{"curly quotes", 21, "“Yes, I see why, but no”", false},
		{"curly apostrophe", 8, "rename the importer’s queue", false},
		{"case folded", 6, strings.ToLower(strict), false},
		{"a Cyrillic look-alike", 6, strings.Replace(strict, "dry", "drу", 1), false},
		{"a zero-width space", 6, strings.Replace(strict, "dry", "d​ry", 1), false},
		{"no space where one was", 6, strings.Replace(strict, "dry run", "dryrun", 1), false},
		{"across two entries", 6, "before every import? yes", false},
		{"the next entry's words", 6, "I suggest we rename", false},
		{"the head of an entry", 7, "[7] user typed 14:06: yes", false},
		{"seven characters", 6, "a dry r", false},
		{"a whole one-word entry", 7, "yes", true},
		{"the whole entry with spaces", 7, " yes ", true},
		{"a yes inside a longer entry", 21, "Yes", false},
		{"empty", 7, "", false},
		{"white space alone", 7, "   ", false},
		{"the whole of a long entry", 22, filler, true},
		{"241 characters of a long entry", 22, string([]rune(filler)[:241]), false},
		{"240 characters of a long entry", 22, string([]rune(filler)[:240]), true},
	} {
		j := judgeOne(in, nil, Op{Op: "note", Kind: ptr("found"), Text: ptr("Noted."), Ev: []Span{sq(c.e, c.q, "said")}}, FormQuotes)
		if found := j.Code != "quote_not_found"; found != c.found {
			t.Errorf("%s: %q in entry %d: code %q", c.name, c.q, c.e, j.Code)
		}
		if c.found && !j.Accepted() {
			t.Errorf("%s: refused %s", c.name, j.Code)
		}
	}
	// The words a found quote cites are the quote with its white space
	// collapsed, as the entry holds them.
	j := judgeOne(in, nil, Op{Op: "note", Kind: ptr("found"), Text: ptr("Noted."), Ev: []Span{sq(6, " a  dry\nrun ", "report")}}, FormQuotes)
	if j.Spans[0].Words != "a dry run" {
		t.Errorf("the words cited are %q", j.Spans[0].Words)
	}
}

// TestSegmentMatch: in the segment form a span names a segment its entry
// has, counted from 1, and its text becomes the words cited.
func TestSegmentMatch(t *testing.T) {
	in := renderPass(t, scriptByID(t, "assent.01"), 1, nil, FormSegments)
	for _, c := range []struct {
		e, s  int64
		code  string
		words string
	}{
		{19, 1, "", "Hmm, possibly."},
		{19, 2, "", "Park it for now."},
		{19, 3, "bad_segment", ""},
		{19, 0, "bad_segment", ""},
		{19, -1, "bad_segment", ""},
		{0, 1, "bad_entry", ""},
		{24, 1, "bad_entry", ""},
	} {
		j := judgeOne(in, nil, Op{Op: "note", Kind: ptr("found"), Text: ptr("Noted."), Ev: []Span{{E: c.e, S: c.s, As: "said"}}}, FormSegments)
		if j.Code != c.code || (c.code == "" && j.Spans[0].Words != c.words) {
			t.Errorf("segment %d.%d: code %q, spans %+v", c.e, c.s, j.Code, j.Spans)
		}
	}
	// A quote is not read in the segment form, nor a segment in the quotes
	// form.
	if j := judgeOne(in, nil, Op{Op: "note", Kind: ptr("found"), Text: ptr("Noted."), Ev: []Span{{E: 19, Q: "Hmm, possibly.", As: "said"}}}, FormSegments); j.Code != "bad_segment" {
		t.Errorf("a quote in the segment form: %q", j.Code)
	}
	qin := renderPass(t, scriptByID(t, "assent.01"), 1, nil, FormQuotes)
	if j := judgeOne(qin, nil, Op{Op: "note", Kind: ptr("found"), Text: ptr("Noted."), Ev: []Span{{E: 19, S: 1, As: "said"}}}, FormQuotes); j.Code != "quote_not_found" {
		t.Errorf("a segment in the quotes form: %q", j.Code)
	}
}

// TestAssentRule: a whole-entry "yes" that is the first typed prompt after
// the proposal it is cited with makes a decision accepted; the same yes
// after a later proposal, as the second prompt after the one cited, gives
// nothing; a "yes" quoted inside a longer entry is not found.
func TestAssentRule(t *testing.T) {
	s := scriptByID(t, "assent.01")
	strict := "Shall I add a dry run, importer verify --strict-rows, before every import?"
	marrow := "Should the importer write its checkpoints to the marrow_ckpt table instead of files?"
	for _, f := range Forms {
		in := renderPass(t, s, 1, nil, f)
		span := func(e int64, q string, k int64, as string) Span {
			if f == FormSegments {
				return Span{E: e, S: k, As: as}
			}
			return sq(e, q, as)
		}
		for _, c := range []struct {
			name string
			ev   []Span
			code string
			tag  string
		}{
			{"the first prompt after the proposal", []Span{span(6, strict, 1, "proposal"), span(7, "yes", 1, "assent")}, "", TagAccepted},
			{"the yes cited first", []Span{span(7, "yes", 1, "assent"), span(6, strict, 1, "proposal")}, "", TagAccepted},
			{"the second prompt after the proposal", []Span{span(4, marrow, 1, "proposal"), span(7, "yes", 1, "assent")}, "tag_too_low", TagSuggested},
			{"a prompt before the proposal", []Span{span(8, "I suggest we rename the importer's queue to harvest-intake.", 1, "proposal"), span(7, "yes", 1, "assent")}, "tag_too_low", TagSuggested},
			{"no proposal cited", []Span{span(7, "yes", 1, "assent")}, "tag_too_low", ""},
			{"the proposal cited as a report", []Span{span(6, strict, 1, "report"), span(7, "yes", 1, "assent")}, "tag_too_low", TagSuggested},
			{"a typed entry cited as the proposal", []Span{span(5, "maybe", 1, "proposal"), span(7, "yes", 1, "assent")}, "", TagStated},
			{"the proposal alone", []Span{span(6, strict, 1, "proposal")}, "tag_too_low", TagSuggested},
		} {
			j := judgeOne(in, nil, add("procedure", "Run importer verify --strict-rows as a dry run before every import.", c.ev...), f)
			if j.Code != c.code || j.Tag != c.tag {
				t.Errorf("%s: %s: code %q tag %q, want %q %q", f, c.name, j.Code, j.Tag, c.code, c.tag)
			}
		}
	}
	// A yes inside a longer entry cannot be cited alone.
	in := renderPass(t, s, 1, nil, FormQuotes)
	if j := judgeOne(in, nil, add("decision", "The old csvmux reader is dropped.", sq(20, "Should we drop the old csvmux reader from the importer?", "proposal"), sq(21, "Yes", "assent")), FormQuotes); j.Code != "quote_not_found" {
		t.Errorf("a yes inside a longer entry: %q", j.Code)
	}
	// "a typed entry cited as the proposal" above is stated only because a
	// typed entry is stated whatever its as, not because it was taken for
	// a proposal: a constraint, which needs stated, is accepted on it, and
	// its role is fixed.
	j := judgeOne(in, nil, add("constraint", "The importer stays as it is.", sq(15, "no thanks, leave it as it is", "proposal")), FormQuotes)
	if !j.Accepted() || j.Tag != TagStated || !j.Spans[0].RoleFixed {
		t.Errorf("a typed entry cited as a proposal: %+v", j)
	}
}

// madeUp is a made-up session with an entry by each author the tag rule
// names, every text free of tokens that must be grounded.
func madeUp() scenario.Session {
	at := func(m int) time.Time { return time.Date(2026, 4, 7, 9, m, 0, 0, time.FixedZone("", 7200)) }
	turn := func(m int, a event.Author, k event.Kind, text string) scenario.Turn {
		return scenario.Turn{At: at(m), Author: a, Kind: k, Text: text}
	}
	sub := turn(5, event.AuthorSubagent, event.KindSubReport, "The relay is small and quiet, the subagent says.")
	sub.Agent = "helper"
	return scenario.Session{ID: "made-up-session", Branch: "main", Turns: []scenario.Turn{
		turn(1, event.AuthorTyped, event.KindPrompt, "Keep the relay small and quiet."),
		turn(2, event.AuthorAgent, event.KindReply, "I will keep the relay small and quiet."),
		turn(3, event.AuthorTool, event.KindTool, "relay check passed, small and quiet"),
		turn(4, event.AuthorPasted, event.KindPrompt, "Decision: the relay is small and quiet by design."),
		sub,
		turn(6, event.AuthorChosen, event.KindDialog, "Keep the relay small and quiet"),
		turn(7, event.AuthorProgram, event.KindPrompt, "The relay is small and quiet, a program says."),
		turn(8, event.AuthorV1User, event.KindPrompt, "The relay is small and quiet, v1 says."),
	}}
}

// TestTagComesFromTheAuthor: each author gives its tag whatever as the
// answer names, an as that does not fit its author counts as fixed, and
// each product takes the tags it allows.
func TestTagComesFromTheAuthor(t *testing.T) {
	in, err := Render(madeUp(), nil, FormQuotes)
	if err != nil {
		t.Fatal(err)
	}
	authors := []struct {
		e   int64
		tag string
		fit string // the as that fits the author, "" for none
	}{
		{1, TagStated, "said"}, {2, TagSuggested, "report"}, {3, TagObserved, "result"}, {4, TagExternal, ""},
		{5, TagSuggested, "report"}, {6, TagAccepted, "said"}, {7, TagExternal, ""}, {8, TagExternal, ""},
	}
	allows := map[string][]string{
		"decision":   {TagStated, TagAccepted},
		"procedure":  {TagStated, TagAccepted},
		"constraint": {TagStated},
		"rule":       {TagStated},
		"fact":       {TagStated, TagObserved},
		"next.add":   {TagStated, TagAccepted, TagObserved, TagSuggested},
		"ask.add":    {TagStated, TagAccepted, TagObserved, TagSuggested},
		"note":       {TagStated, TagAccepted, TagObserved, TagSuggested, TagExternal},
	}
	for _, a := range authors {
		e := in.Entries[a.e-1]
		for _, as := range roles {
			for what, ok := range allows {
				var op Op
				switch what {
				case "next.add", "ask.add":
					op = Op{Op: what, Text: ptr("The relay is small."), Ev: []Span{sq(a.e, e.Text, as)}}
				case "note":
					op = Op{Op: what, Kind: ptr("found"), Text: ptr("The relay is small."), Ev: []Span{sq(a.e, e.Text, as)}}
				default:
					op = add(what, "The relay is small.", sq(a.e, e.Text, as))
				}
				j := judgeOne(in, nil, op, FormQuotes)
				want := a.tag
				if as == "assent" && a.e == 1 {
					want = "" // a typed assent with no proposal gives nothing
				}
				if j.Tag != want {
					t.Errorf("entry %d cited as %s for %s: tag %q, want %q", a.e, as, what, j.Tag, want)
				}
				if accepted := slices.Contains(ok, want); j.Accepted() != accepted {
					t.Errorf("entry %d cited as %s for %s: code %q", a.e, as, what, j.Code)
				}
				if fixed := as != a.fit && !(a.fit == "said" && as == "assent") && !(a.fit == "report" && as == "proposal"); j.Spans[0].RoleFixed != fixed {
					t.Errorf("entry %d cited as %s: role fixed %v", a.e, as, j.Spans[0].RoleFixed)
				}
			}
		}
	}
	// An operation takes its spans' highest tag: an agent's words and a
	// tool's record make a fact observed.
	j := judgeOne(in, nil, add("fact", "The relay is small.", sq(2, in.Entries[1].Text, "report"), sq(3, in.Entries[2].Text, "result")), FormQuotes)
	if !j.Accepted() || j.Tag != TagObserved {
		t.Errorf("highest tag: %+v", j)
	}
	// A decision citing a pasted entry is too low, whatever its as.
	for _, as := range roles {
		if j := judgeOne(in, nil, add("decision", "The relay is small.", sq(4, in.Entries[3].Text, as)), FormQuotes); j.Code != "tag_too_low" {
			t.Errorf("a decision citing a pasted entry as %s: %q", as, j.Code)
		}
	}
}

// TestSupersedeAndRetire: changing what is stated or accepted needs the
// user's own words; a retire takes its target's kind's rule.
func TestSupersedeAndRetire(t *testing.T) {
	in, err := Render(madeUp(), nil, FormQuotes)
	if err != nil {
		t.Fatal(err)
	}
	shown := []Shown{
		{Handle: "h1", Kind: "decision", Tag: TagStated, Text: "The relay is small."},
		{Handle: "h2", Kind: "fact", Tag: TagObserved, Text: "The relay is quiet."},
		{Handle: "h3", Kind: "decision", Tag: TagAccepted, Text: "The relay is quiet."},
		{Handle: "h4", Kind: KindNext, Tag: TagSuggested, Text: "Check the relay."},
		{Handle: "h5", Kind: KindQuestion, Tag: TagSuggested, Text: "Is the relay small?"},
	}
	span := func(e int64) Span { return sq(e, in.Entries[e-1].Text, "said") }
	sup := func(id, kind string, e int64) Op {
		return Op{Op: "item.supersede", ID: &id, Kind: &kind, Text: ptr("The relay is smaller."), Ev: []Span{span(e)}}
	}
	ret := func(op, id string, e int64) Op { return Op{Op: op, ID: &id, Text: ptr("It is done."), Ev: []Span{span(e)}} }
	for _, c := range []struct {
		name string
		op   Op
		code string
	}{
		{"supersede a stated decision by typed words", sup("h1", "decision", 1), ""},
		{"supersede a stated decision by a choice", sup("h1", "decision", 6), "tag_too_low"},
		{"supersede an accepted decision by a choice", sup("h3", "decision", 6), "tag_too_low"},
		{"supersede an observed fact by a tool", sup("h2", "fact", 3), ""},
		{"supersede an observed fact by the agent", sup("h2", "fact", 2), "tag_too_low"},
		{"retire a stated decision by typed words", ret("item.retire", "h1", 1), ""},
		{"retire a stated decision by a choice", ret("item.retire", "h1", 6), "tag_too_low"},
		{"retire an observed fact by a tool", ret("item.retire", "h2", 3), ""},
		{"retire an observed fact by the agent", ret("item.retire", "h2", 2), "tag_too_low"},
		{"a next step done by the agent", ret("next.done", "h4", 2), ""},
		{"a next step done by a paste", ret("next.done", "h4", 4), "tag_too_low"},
		{"a question answered by a tool", ret("ask.answer", "h5", 3), ""},
		{"a question answered by a program", ret("ask.answer", "h5", 7), "tag_too_low"},
		{"supersede a next step", sup("h4", "decision", 1), "unknown_target"},
		{"retire a question", ret("item.retire", "h5", 1), "unknown_target"},
		{"mark an item done", ret("next.done", "h1", 1), "unknown_target"},
		{"answer a next step", ret("ask.answer", "h4", 1), "unknown_target"},
		{"retire a handle not shown", ret("item.retire", "h9", 1), "unknown_target"},
		{"retire a handle in another case", ret("item.retire", "H1", 1), "unknown_target"},
	} {
		if j := judgeOne(in, shown, c.op, FormQuotes); j.Code != c.code {
			t.Errorf("%s: %q, want %q", c.name, j.Code, c.code)
		}
	}
	// A handle that an operation accepted earlier in the answer changes is
	// no longer a target; one refused earlier leaves it to the next.
	twice := Check(in, shown, Answer{Ops: []Op{ret("item.retire", "h1", 1), sup("h1", "decision", 1)}}, FormQuotes)
	if got := codes(twice); !slices.Equal(got, []string{"", "unknown_target"}) {
		t.Errorf("a handle changed twice: %q", got)
	}
	after := Check(in, shown, Answer{Ops: []Op{ret("item.retire", "h1", 6), sup("h1", "decision", 1)}}, FormQuotes)
	if got := codes(after); !slices.Equal(got, []string{"tag_too_low", ""}) {
		t.Errorf("a handle a refused operation named: %q", got)
	}
}

// TestShape: each way an operation breaks its shape is bad_shape at its
// place, and only a break of the schema's own bounds and enums counts as
// one.
func TestShape(t *testing.T) {
	in := renderPass(t, scriptByID(t, "assent.01"), 1, nil, FormQuotes)
	shown := []Shown{{Handle: "h1", Kind: "decision", Tag: TagStated, Text: "Something holds."}}
	good := func() Op {
		return Op{Op: "note", Kind: ptr("found"), Text: ptr("Noted."), Ev: []Span{sq(7, "yes", "said")}}
	}
	with := func(change func(*Op)) Op { op := good(); change(&op); return op }
	for _, c := range []struct {
		name   string
		op     Op
		place  string
		breaks bool
	}{
		{"good", good(), "", false},
		{"unknown op", with(func(o *Op) { o.Op = "item.confirm" }), "op", true},
		{"op in another case", with(func(o *Op) { o.Op = "Note" }), "op", true},
		{"an id on a note", with(func(o *Op) { o.ID = ptr("h1") }), "id", false},
		{"no id on a retire", Op{Op: "item.retire", Text: ptr("Gone."), Ev: []Span{sq(7, "yes", "said")}}, "id", false},
		{"no kind on a note", with(func(o *Op) { o.Kind = nil }), "kind", false},
		{"an item's kind on a note", with(func(o *Op) { o.Kind = ptr("decision") }), "kind", false},
		{"a kind outside the enum", with(func(o *Op) { o.Kind = ptr("lesson") }), "kind", true},
		{"a note's kind on an add", add("did", "Noted.", sq(7, "yes", "said")), "kind", false},
		{"a kind on next.add", Op{Op: "next.add", Kind: ptr("fact"), Text: ptr("Do it."), Ev: []Span{sq(7, "yes", "said")}}, "kind", false},
		{"an admit on a note", with(func(o *Op) { o.Admit = ptr("decision") }), "admit", false},
		{"an admit outside the enum", with(func(o *Op) { o.Admit = ptr("whim") }), "admit", true},
		{"no admit on an add", Op{Op: "item.add", Kind: ptr("fact"), Text: ptr("Noted."), Ev: []Span{sq(7, "yes", "said")}}, "admit", false},
		{"no text", with(func(o *Op) { o.Text = nil }), "text", false},
		{"blank text", with(func(o *Op) { o.Text = ptr(" \t\n") }), "text", false},
		{"no span", with(func(o *Op) { o.Ev = nil }), "ev", true},
		{"four spans", with(func(o *Op) { o.Ev = slices.Repeat(o.Ev, 4) }), "ev", true},
		{"an unknown as", with(func(o *Op) { o.Ev = append(o.Ev, sq(7, "yes", "agreed")) }), "ev.2.as", true},
	} {
		j := judgeOne(in, shown, c.op, FormQuotes)
		if c.place == "" {
			if !j.Accepted() {
				t.Errorf("%s: refused %s", c.name, j.Code)
			}
		} else if j.Code != "bad_shape" || j.Place != c.place {
			t.Errorf("%s: %q at %q, want bad_shape at %q", c.name, j.Code, j.Place, c.place)
		}
		if j.Breaks != c.breaks {
			t.Errorf("%s: breaks %v", c.name, j.Breaks)
		}
	}
	// A next step done needs no text.
	done := Op{Op: "next.done", ID: ptr("h1"), Ev: []Span{sq(7, "yes", "said")}}
	if j := judgeOne(in, []Shown{{Handle: "h1", Kind: KindNext, Tag: TagStated, Text: "Do it."}}, done, FormQuotes); !j.Accepted() {
		t.Errorf("a next step done without text: %q", j.Code)
	}
	// An answer over the bound breaks the schema, and each operation past
	// it is refused; so does an unknown nothing.
	many := slices.Repeat([]Op{good()}, MaxOps+2)
	v := Check(in, nil, Answer{Ops: many}, FormQuotes)
	if !v.Breaks || codes(v)[MaxOps-1] != "" || codes(v)[MaxOps] != "bad_shape" || v.Ops[MaxOps+1].Place != "ops" {
		t.Errorf("an answer over the bound: breaks %v, codes %q", v.Breaks, codes(v))
	}
	if v := Check(in, nil, Answer{Ops: many[:MaxOps]}, FormQuotes); v.Breaks {
		t.Error("an answer at the bound breaks the schema")
	}
	for nothing, breaks := range map[string]bool{"none_new": false, "only_progress": false, "only_derivable": false, "maybe": true, "": true} {
		if v := Check(in, nil, Answer{Nothing: &nothing}, FormQuotes); v.Breaks != breaks || len(v.Ops) != 0 {
			t.Errorf("nothing %q: breaks %v", nothing, v.Breaks)
		}
	}
	// An unknown form refuses every operation.
	if v := Check(in, nil, Answer{Ops: []Op{good()}}, "pages"); codes(v)[0] != "bad_shape" {
		t.Errorf("an unknown form: %q", codes(v))
	}
}

// TestGrounding: under the user's authority, a number, a path, an
// identifier with an underscore or a dot, and a quoted name must each be
// in a cited span, whole.
func TestGrounding(t *testing.T) {
	s := scriptByID(t, "v1.05")
	cond := "If BenchmarkBrief in scripts/benchmark.py passes 300 ms, switch to the streaming parser, CONDOR-8."
	in := renderPass(t, s, 1, nil, FormQuotes)
	for _, c := range []struct {
		text, quote string
		ok          bool
	}{
		{cond, cond, true},
		{"Switch to the streaming parser, CONDOR-8, if BenchmarkBrief in scripts/benchmark.py passes 300 ms.", cond, true},
		{strings.Replace(cond, "300", "250", 1), cond, false},
		{strings.Replace(cond, "300 ms", "300ms", 1), cond, false},
		{strings.Replace(cond, "300", "30", 1), cond, false},
		{strings.Replace(cond, "300", "3000", 1), cond, false},
		{strings.Replace(cond, "CONDOR-8", "CONDOR-9", 1), cond, false},
		{strings.Replace(cond, "CONDOR-8", "condor-8", 1), cond, false},
		{strings.Replace(cond, "scripts/benchmark.py", "scripts/bench.py", 1), cond, false},
		{strings.Replace(cond, "scripts/benchmark.py", "(scripts/benchmark.py)", 1), cond, true},
		{"Switch to the streaming parser when BenchmarkBrief passes 300 ms.", "BenchmarkBrief in scripts/benchmark.py passes 300 ms", true},
		{"Switch at 300 ms, CONDOR-8.", "BenchmarkBrief in scripts/benchmark.py passes 300 ms", false},
		{"Switch to the `streaming` parser.", cond, true},
		{"Switch to the `fast` parser.", cond, false},
		{"Switch to the \"streaming parser\".", cond, true},
		{"Switch to the “quick parser”.", cond, false},
		{"Switch to the ‘quick’ parser.", cond, false},
		{"Switch to the \"streaming parser", cond, true},
		{"Switch the parser in run_bench.", cond, false},
		{"Switch the parser in bench.yaml.", cond, false},
		{"Switch the parser, e.g. now.", cond, false},
		{"Switch the parser soon.", cond, true},
	} {
		j := judgeOne(in, nil, add("decision", c.text, sq(1, c.quote, "said")), FormQuotes)
		if j.Accepted() != c.ok || (!c.ok && j.Code != "ungrounded") {
			t.Errorf("%q citing %q: code %q", c.text, c.quote, j.Code)
		}
	}
	// Only what is stored under the user's authority is grounded: a note
	// citing the agent may hold any number.
	a1 := renderPass(t, scriptByID(t, "assent.01"), 1, nil, FormQuotes)
	if j := judgeOne(a1, nil, Op{Op: "note", Kind: ptr("found"), Text: ptr("The agent proposed 3 things in 42 ways."), Ev: []Span{sq(2, "I could move the importer to the quillstream parser.", "proposal")}}, FormQuotes); !j.Accepted() {
		t.Errorf("a suggested note is grounded: %q", j.Code)
	}
	if j := judgeOne(a1, nil, Op{Op: "note", Kind: ptr("found"), Text: ptr("The user said no 3 times."), Ev: []Span{sq(3, "no", "said")}}, FormQuotes); j.Code != "ungrounded" {
		t.Errorf("a stated note is not grounded: %q", j.Code)
	}
}

// TestGroundTokens: the tokens that must be grounded, and where a token is
// found whole.
func TestGroundTokens(t *testing.T) {
	got := groundTokens("Run `make vendor-snap` at 5 pm on api.v2, see docs/run.md, \"Blue Fox\" and “Red Owl”, not e.g. this or that, nor 'single'.")
	want := []string{"make", "vendor-snap", "5", "api.v2", "docs/run.md", "Blue", "Fox", "Red", "Owl", "e.g"}
	if !slices.Equal(got, want) {
		t.Errorf("tokens %q, want %q", got, want)
	}
	for _, c := range []struct {
		words, tok string
		ok         bool
	}{
		{"cache for 5 minutes", "5", true},
		{"cache for 15 minutes", "5", false},
		{"cache for 51 minutes", "5", false},
		{"cache for 5.", "5", true},
		{"cache for 5.5 s", "5", true},
		{"cache_5 here", "5", true},
		{"a5 then 5", "5", true},
		{"tool ./scripts/x.sh ran", "./scripts/x.sh", true},
		{"tool /scripts/x.sh ran", "/scripts/x.sh", true},
		{"tool a/scripts/x.sh ran", "/scripts/x.sh", false},
		{"tool scripts/x.sh ran", "/scripts/x.sh", false},
		{"", "5", false},
		{"Gö 1.26", "1.26", true},
		{"Gö1.26", "1.26", false},
	} {
		if holds(c.words, c.tok) != c.ok {
			t.Errorf("holds(%q, %q) = %v", c.words, c.tok, !c.ok)
		}
	}
}

// TestARefusedOperationTakesNoOtherWithIt: each operation of the canned
// faults is judged as it is when it stands alone, in either order of the
// answer.
func TestARefusedOperationTakesNoOtherWithIt(t *testing.T) {
	for _, f := range Forms {
		in, v := checkFaults(t, f)
		a := parse(t, canned(t, f, "assent.01.1.faults"), f)
		for i, op := range a.Ops {
			alone := Check(in, nil, Answer{Ops: []Op{op}}, f).Ops[0]
			if !reflect.DeepEqual(alone, v.Ops[i]) {
				t.Errorf("%s: operation %d alone is %+v, in the answer %+v", f, i+1, alone, v.Ops[i])
			}
		}
		rev := slices.Clone(a.Ops)
		slices.Reverse(rev)
		got := codes(Check(in, nil, Answer{Ops: rev}, f))
		slices.Reverse(got)
		if !slices.Equal(got, faultCodes[f]) {
			t.Errorf("%s: reversed, the codes are %q", f, got)
		}
	}
}

// TestCheckChangesNothing: Check gives the same verdict twice and leaves
// its input, the items shown and the answer as they were.
func TestCheckChangesNothing(t *testing.T) {
	for _, f := range Forms {
		in := renderPass(t, scriptByID(t, "assent.01"), 1, nil, f)
		data := canned(t, f, "assent.01.1.faults")
		a := parse(t, data, f)
		shown := []Shown{{Handle: "h1", Kind: "decision", Tag: TagStated, Text: "Something holds."}}
		before := fmt.Sprintf("%#v %#v %#v", in, shown, a)
		v1, v2 := Check(in, shown, a, f), Check(in, shown, a, f)
		if !reflect.DeepEqual(v1, v2) {
			t.Errorf("%s: two verdicts differ", f)
		}
		if after := fmt.Sprintf("%#v %#v %#v", in, shown, a); after != before {
			t.Errorf("%s: Check changed what it was given", f)
		}
	}
}

// TestHugeAndDeepAnswers: an answer is never trusted to be small, shallow
// or well formed. A deep one, and one that gives a key twice, are not
// read; a huge one is judged op by op, past the bound refused at once.
func TestHugeAndDeepAnswers(t *testing.T) {
	for _, data := range []string{
		strings.Repeat("[", 1<<20) + strings.Repeat("]", 1<<20),
		`{"ops":` + strings.Repeat("[", 1<<16) + strings.Repeat("]", 1<<16) + `,"nothing":null}`,
		`{"ops":[{"op":"note","id":null,"kind":"found","text":"x","admit":null,"ev":` + strings.Repeat(`[`, 1<<16) + strings.Repeat(`]`, 1<<16) + `}],"nothing":null}`,
		`{"ops":[],"nothing":null,"ops":[]}`,
		`{"ops":[{"op":"note","op":"item.add","id":null,"kind":"found","text":"x","admit":null,"ev":[{"e":7,"q":"yes","as":"said"}]}],"nothing":null}`,
		`{"ops":[{"op":"note","id":null,"kind":"found","text":"x","admit":null,"ev":[{"e":7,"e":3,"q":"yes","as":"said"}]}],"nothing":null}`,
	} {
		if _, err := ParseAnswer([]byte(data), FormQuotes); err == nil {
			t.Errorf("an answer of %d bytes starting %.40q is read", len(data), data)
		}
	}
	in := renderPass(t, scriptByID(t, "assent.01"), 1, nil, FormQuotes)
	op := `{"op":"note","id":null,"kind":"found","text":"Noted.","admit":null,"ev":[{"e":7,"q":"yes","as":"said"}]}`
	const n = 50000
	huge := `{"ops":[` + strings.TrimSuffix(strings.Repeat(op+",", n), ",") + `],"nothing":null}`
	v := Check(in, nil, parse(t, []byte(huge), FormQuotes), FormQuotes)
	if len(v.Ops) != n || !v.Breaks || !v.Ops[MaxOps-1].Accepted() || v.Ops[n-1].Code != "bad_shape" {
		t.Errorf("a huge answer: %d operations, breaks %v", len(v.Ops), v.Breaks)
	}
	// A huge text, a huge quote and many spans are judged.
	var b strings.Builder
	for i := range 300000 {
		fmt.Fprintf(&b, "%d ", i)
	}
	text := b.String()
	if j := judgeOne(in, nil, Op{Op: "note", Kind: ptr("found"), Text: &text, Ev: []Span{sq(7, "yes", "said")}}, FormQuotes); j.Code != "ungrounded" {
		t.Errorf("a huge text: %q", j.Code)
	}
	v5 := renderPass(t, scriptByID(t, "v1.05"), 1, nil, FormQuotes)
	cond := v5.Entries[0].Text
	same := strings.Repeat("CONDOR-8, 300 ms and scripts/benchmark.py ", 300000)
	if j := judgeOne(v5, nil, add("decision", same, sq(1, cond, "said")), FormQuotes); !j.Accepted() {
		t.Errorf("a huge grounded text: %q", j.Code)
	}
	if j := judgeOne(in, nil, Op{Op: "note", Kind: ptr("found"), Text: ptr("Noted."), Ev: []Span{sq(7, text, "said")}}, FormQuotes); j.Code != "quote_not_found" {
		t.Errorf("a huge quote: %q", j.Code)
	}
	if j := judgeOne(in, nil, Op{Op: "note", Kind: ptr("found"), Text: ptr("Noted."), Ev: slices.Repeat([]Span{sq(7, "yes", "said")}, 100000)}, FormQuotes); j.Code != "bad_shape" || !j.Breaks {
		t.Errorf("many spans: %q", j.Code)
	}
}
