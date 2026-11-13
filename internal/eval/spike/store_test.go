package spike

import (
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/heyNag/eva-md/internal/eval/scenario"
	"github.com/heyNag/eva-md/internal/ledger/event"
)

// runFaithful runs the faithful canned answers of script id in form f
// through a store, and returns it with what each pass was shown.
func runFaithful(t *testing.T, id string, f Form) (*Store, [][]Shown) {
	t.Helper()
	s := scriptByID(t, id)
	st := &Store{}
	var shown [][]Shown
	for n := 1; n <= len(s.Scenario.Passes); n++ {
		sh := st.Shown()
		shown = append(shown, sh)
		in := renderPass(t, s, n, sh, f)
		v := Check(in, sh, parse(t, canned(t, f, fmt.Sprintf("%s.%d.faithful", id, n)), f), f)
		if err := st.Apply(passSession(s, n), v); err != nil {
			t.Fatal(err)
		}
	}
	return st, shown
}

// TestStoreAcrossPasses: v1.08's two passes add next steps, a question
// and a fact, then mark a step done, answer the question and add a
// decision and a note, each with its tag, day and sources; the second
// pass is shown what the first left in force.
func TestStoreAcrossPasses(t *testing.T) {
	s := scriptByID(t, "v1.08")
	s1, s2 := s.Scenario.Sessions[0].ID, s.Scenario.Sessions[1].ID
	src := func(session string, n ...int64) []Source {
		var out []Source
		for _, x := range n {
			out = append(out, Source{Session: session, N: x})
		}
		return out
	}
	want := []Item{
		{Handle: "h1", Kind: KindNext, State: StateRetired, Tag: TagStated, Text: "Write the parser, MARTIN-1.", Day: "2026-01-13", Sources: src(s1, 1),
			Ended: "The parser is written and its tests pass.", EndedBy: src(s2, 1, 2)},
		{Handle: "h2", Kind: KindNext, State: StateActive, Tag: TagStated, Text: "Write the renderer after the parser, MARTIN-2.", Day: "2026-01-13", Sources: src(s1, 1)},
		{Handle: "h3", Kind: KindQuestion, State: StateRetired, Tag: TagStated, Text: "Should pages hold hidden ids, MARTIN-3?", Day: "2026-01-13", Sources: src(s1, 1),
			Ended: "Pages hold hidden ids.", EndedBy: src(s2, 3)},
		{Handle: "h4", Kind: "fact", State: StateActive, Tag: TagStated, Text: "The project uses Go 1.26, MARTIN-4.", Day: "2026-01-13", Sources: src(s1, 1)},
		{Handle: "h5", Kind: "decision", State: StateActive, Tag: TagStated, Text: "Pages hold hidden ids.", Day: "2026-01-14", Sources: src(s2, 3)},
		{Handle: "h6", Kind: "did", State: StateLog, Tag: TagObserved, Text: "The parser tests pass.", Day: "2026-01-14", Sources: src(s2, 1)},
	}
	for _, f := range Forms {
		st, shown := runFaithful(t, "v1.08", f)
		if got := st.Items(); !reflect.DeepEqual(got, want) {
			t.Errorf("%s: items\n%+v\nwant\n%+v", f, got, want)
		}
		wantShown := []Shown{
			{Handle: "h1", Kind: KindNext, Tag: TagStated, Text: "Write the parser, MARTIN-1."},
			{Handle: "h2", Kind: KindNext, Tag: TagStated, Text: "Write the renderer after the parser, MARTIN-2."},
			{Handle: "h3", Kind: KindQuestion, Tag: TagStated, Text: "Should pages hold hidden ids, MARTIN-3?"},
			{Handle: "h4", Kind: "fact", Tag: TagStated, Text: "The project uses Go 1.26, MARTIN-4."},
		}
		if len(shown[0]) != 0 || !reflect.DeepEqual(shown[1], wantShown) {
			t.Errorf("%s: shown %+v", f, shown)
		}
		if got := st.Shown(); len(got) != 3 || got[0].Handle != "h2" || got[1].Handle != "h4" || got[2].Handle != "h5" {
			t.Errorf("%s: shown after the run %+v", f, got)
		}
	}
}

// TestStoreSupersedes: v1.03's reversal retires the first decision and
// mints a new one that replaces it.
func TestStoreSupersedes(t *testing.T) {
	s := scriptByID(t, "v1.03")
	for _, f := range Forms {
		st, _ := runFaithful(t, "v1.03", f)
		want := []Item{
			{Handle: "h1", Kind: "decision", State: StateRetired, Tag: TagStated, Text: "Pages are stored as YAML, OSPREY-Y.", Day: "2026-01-13",
				Sources: []Source{{s.Scenario.Sessions[0].ID, 1}}, EndedBy: []Source{{s.Scenario.Sessions[1].ID, 1}}},
			{Handle: "h2", Kind: "decision", State: StateActive, Tag: TagStated, Text: "Pages are stored as JSONL, OSPREY-J.", Day: "2026-01-14",
				Sources: []Source{{s.Scenario.Sessions[1].ID, 1}}, Replaces: "h1"},
		}
		if got := st.Items(); !reflect.DeepEqual(got, want) {
			t.Errorf("%s: items %+v", f, got)
		}
	}
}

// TestStoreAssents: an assent is kept with the tag accepted and both its
// spans as sources; the hedges are open questions, suggested.
func TestStoreAssents(t *testing.T) {
	s := scriptByID(t, "assent.02")
	sid := s.Scenario.Sessions[0].ID
	for _, f := range Forms {
		st, _ := runFaithful(t, "assent.02", f)
		items := st.Items()
		if len(items) != 6 {
			t.Fatalf("%s: %d items", f, len(items))
		}
		got := items[1]
		if got.Kind != "decision" || got.Tag != TagAccepted || got.State != StateActive || !reflect.DeepEqual(got.Sources, []Source{{sid, 6}, {sid, 7}}) || got.Day != "2026-03-03" {
			t.Errorf("%s: the assent is kept as %+v", f, got)
		}
		if q := items[0]; q.Kind != KindQuestion || q.Tag != TagSuggested {
			t.Errorf("%s: the hedge is kept as %+v", f, q)
		}
	}
}

// TestStoreDayIsTheNewestEntrys: an item's day is that of its newest cited
// entry, in that entry's own offset, and an entry cited twice is one
// source.
func TestStoreDayIsTheNewestEntrys(t *testing.T) {
	east, west := time.FixedZone("", 9*3600), time.FixedZone("", -5*3600)
	s := scenario.Session{ID: "days-session", Branch: "main", Turns: []scenario.Turn{
		{At: time.Date(2026, 5, 1, 23, 50, 0, 0, west), Author: event.AuthorTyped, Kind: event.KindPrompt, Text: "Keep the relay small."},
		{At: time.Date(2026, 5, 2, 13, 10, 0, 0, east), Author: event.AuthorTyped, Kind: event.KindPrompt, Text: "And keep it quiet."},
	}}
	in, err := Render(s, nil, FormQuotes)
	if err != nil {
		t.Fatal(err)
	}
	// 23:50 at -05:00 (04:50 UTC on the 2nd) is later than 13:10 at
	// +09:00 (04:10 UTC on the 2nd): the first entry is the newest, and
	// its day, in its own offset, is the 1st.
	v := Check(in, nil, Answer{Ops: []Op{add("decision", "The relay is small and quiet.",
		sq(2, "And keep it quiet.", "said"), sq(1, "Keep the relay small.", "said"), sq(1, "the relay small.", "said"))}}, FormQuotes)
	var st Store
	if err := st.Apply(s, v); err != nil {
		t.Fatal(err)
	}
	if it := st.Items()[0]; it.Day != "2026-05-01" || !reflect.DeepEqual(it.Sources, []Source{{s.ID, 2}, {s.ID, 1}}) {
		t.Errorf("the item is %+v", it)
	}
}

// TestStoreApplyRefusesWhatDoesNotFit: a verdict that names a handle no
// longer active, or of another sort, or cites an entry the session does
// not have, changes nothing; refused operations are not applied; Items
// gives copies.
func TestStoreApplyRefusesWhatDoesNotFit(t *testing.T) {
	s := scriptByID(t, "v1.08")
	st, _ := runFaithful(t, "v1.08", FormQuotes)
	before := st.Items()
	sess := passSession(s, 2)
	span := Cited{E: 3, Session: sess.ID, Author: event.AuthorTyped, As: "said", Words: "Yes, pages hold hidden ids."}
	for name, j := range map[string]Judged{
		"a retired handle":   {Op: Op{Op: "next.done", ID: ptr("h1"), Ev: []Span{{E: 3}}}, Tag: TagStated, Spans: []Cited{span}},
		"a handle not there": {Op: Op{Op: "item.retire", ID: ptr("h9"), Text: ptr("Gone."), Ev: []Span{{E: 3}}}, Tag: TagStated, Spans: []Cited{span}},
		"another sort":       {Op: Op{Op: "item.retire", ID: ptr("h2"), Text: ptr("Gone."), Ev: []Span{{E: 3}}}, Tag: TagStated, Spans: []Cited{span}},
		"no handle":          {Op: Op{Op: "next.done", Ev: []Span{{E: 3}}}, Tag: TagStated, Spans: []Cited{span}},
		"no kind":            {Op: Op{Op: "item.add", Text: ptr("Gone."), Ev: []Span{{E: 3}}}, Tag: TagStated, Spans: []Cited{span}},
		"another session": {Op: Op{Op: "note", Kind: ptr("did"), Text: ptr("Done."), Ev: []Span{{E: 3}}}, Tag: TagStated,
			Spans: []Cited{{E: 3, Session: "elsewhere", As: "said"}}},
		"an entry not there": {Op: Op{Op: "note", Kind: ptr("did"), Text: ptr("Done."), Ev: []Span{{E: 40}}}, Tag: TagStated,
			Spans: []Cited{{E: 40, Session: sess.ID, As: "said"}}},
		"no span": {Op: Op{Op: "note", Kind: ptr("did"), Text: ptr("Done.")}, Tag: TagStated},
	} {
		good := Judged{Op: Op{Op: "note", Kind: ptr("did"), Text: ptr("Done."), Ev: []Span{{E: 3}}}, Tag: TagStated, Spans: []Cited{span}}
		if err := st.Apply(sess, Verdict{Ops: []Judged{good, j}}); err == nil {
			t.Errorf("%s: applied", name)
		}
		if !reflect.DeepEqual(st.Items(), before) {
			t.Errorf("%s: a refused apply changed the store", name)
		}
	}
	refused := Judged{Op: Op{Op: "item.retire", ID: ptr("h9"), Text: ptr("Gone.")}, Code: "unknown_target"}
	if err := st.Apply(sess, Verdict{Ops: []Judged{refused}}); err != nil || !reflect.DeepEqual(st.Items(), before) {
		t.Errorf("a refused operation was applied: %v", err)
	}
	items := st.Items()
	items[0].Sources[0].N, items[0].Text = 99, "changed"
	if !reflect.DeepEqual(st.Items(), before) {
		t.Error("Items gives the store's own records")
	}
}
