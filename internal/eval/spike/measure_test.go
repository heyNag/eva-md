package spike

import (
	"fmt"
	"maps"
	"slices"
	"strings"
	"testing"
)

// runPass runs pass n of s as an arm would: renders it with what st shows,
// checks answer, asks for repair with repair when anything is repairable
// and repair is not nil, applies the final verdict to st, and returns the
// outcome. A repair of "invalid" stands for a repair answer that does not
// parse.
func runPass(t *testing.T, s Script, n int, st *Store, f Form, answer, repair []byte) Outcome {
	t.Helper()
	shown := st.Shown()
	in := renderPass(t, s, n, shown, f)
	o := Outcome{Script: s, Input: in}
	a, err := ParseAnswer(answer, f)
	if err != nil {
		o.Invalid = true
		return o
	}
	o.First = Check(in, shown, a, f)
	o.Final = o.First
	if len(o.First.ToRepair()) > 0 && repair != nil {
		first := append([]byte(Prompt(f)), in.Bytes...)
		input := RepairInput(first, answer, o.First)
		o.RepairCalled, o.RepairInputBytes, o.RepairPrefixBytes = true, len(input), len(first)
		r, err := ParseRepair(repair, f)
		if err != nil {
			o.RepairInvalid, r = true, Repair{}
		}
		o.Final = ApplyRepair(in, shown, o.First, r, f)
	}
	if err := st.Apply(passSession(s, n), o.Final); err != nil {
		t.Fatal(err)
	}
	return o
}

// counts is a Tally's counts by id.
func counts(t Tally) map[string]int {
	out := map[string]int{}
	for _, m := range t.Counts() {
		out[m.ID] = int(m.Value)
	}
	return out
}

// TestTallyOfAHandBuiltRun: three passes, the canned faults of assent.01
// with their repair, an invalid answer to v1.08 and an answer of nothing
// to v1.03, give exact figures for every measure.
func TestTallyOfAHandBuiltRun(t *testing.T) {
	f := FormQuotes
	var tally Tally
	a1 := scriptByID(t, "assent.01")
	o := runPass(t, a1, 1, &Store{}, f, canned(t, f, "assent.01.1.faults"), canned(t, f, "assent.01.1.faults.repair"))
	tally.Add(o)
	tally.Add(runPass(t, scriptByID(t, "v1.08"), 1, &Store{}, f, []byte(`{"ops":[],"nothing":null,"ops":[]}`), nil))
	tally.Add(runPass(t, scriptByID(t, "v1.03"), 1, &Store{}, f, []byte(`{"ops":[],"nothing":"none_new"}`), nil))

	prefix := len(Prompt(f)) + len(o.Input.Bytes)
	answer := strings.TrimRight(string(canned(t, f, "assent.01.1.faults")), "\n")
	tail := "#2 bad_shape kind\n#3 unknown_target id\n#4 bad_entry ev.1.e\n#5 quote_not_found ev.1.q\n#6 ungrounded text\n"
	want := map[string]int{
		"spans_cited_first": 10, "spans_missed_first": 2, "spans_cited_final": 11, "spans_missed_final": 1,
		"ops_proposed": 8, "ops_accepted_first": 2, "ops_accepted_final": 4,
		"refused_bad_shape": 0, "refused_unknown_target": 1, "refused_bad_entry": 1, "refused_quote_not_found": 0,
		"refused_bad_segment": 0, "refused_ungrounded": 1, "refused_tag_too_low": 1,
		"answers_invalid": 1, "schema_breaks": 1, "nothing": 1, "role_fixed": 1,
		"repair_calls": 1, "repair_sent": 5, "repair_recovered": 2,
		"repair_input_bytes":  prefix + len(RepairBoundary) + 1 + len(answer) + 1 + len(RepairHead) + 1 + len(tail),
		"repair_prefix_bytes": prefix,
		"plants_refusal":      4, "plants_hedge": 4, "plants_assent": 2,
		"assent_cited_refusal": 1, "assent_cited_hedge": 0, "accepted_from_refusal": 0, "accepted_from_hedge": 0,
		"assents_captured": 2,
	}
	if got := counts(tally); !maps.Equal(got, want) {
		for _, id := range CountIDs {
			if got[id] != want[id] {
				t.Errorf("%s = %d, want %d", id, got[id], want[id])
			}
		}
		if len(got) != len(want) {
			t.Errorf("%d counts, want %d", len(got), len(want))
		}
	}
	wantRatios := []Measure{{"quote_miss_first", 2.0 / 10}, {"quote_miss_final", 1.0 / 11}, {"repair_yield", 2.0 / 5}}
	if got := tally.Ratios(); !slices.Equal(got, wantRatios) {
		t.Errorf("ratios %v, want %v", got, wantRatios)
	}
}

// TestPlantCounts: the canned answers that cite refusals and hedges as
// assent give exact plant counts, a split answer counts only its refused
// half as a refusal cited, and the faithful answers cite none.
func TestPlantCounts(t *testing.T) {
	for _, f := range Forms {
		var cited, faithfully Tally
		for _, id := range []string{"assent.01", "assent.02"} {
			s := scriptByID(t, id)
			cited.Add(runPass(t, s, 1, &Store{}, f, canned(t, f, id+".1.assent-cited"), nil))
			faithfully.Add(runPass(t, s, 1, &Store{}, f, canned(t, f, id+".1.faithful"), nil))
		}
		for _, c := range []struct {
			name string
			t    Tally
			want map[string]int
		}{
			{"cited as assent", cited, map[string]int{"plants_refusal": 8, "plants_hedge": 8, "plants_assent": 4,
				"assent_cited_refusal": 2, "accepted_from_refusal": 2, "assent_cited_hedge": 3, "accepted_from_hedge": 2, "assents_captured": 2}},
			{"faithful", faithfully, map[string]int{"plants_refusal": 8, "plants_hedge": 8, "plants_assent": 4,
				"assent_cited_refusal": 0, "accepted_from_refusal": 0, "assent_cited_hedge": 0, "accepted_from_hedge": 0, "assents_captured": 4}},
		} {
			for id, n := range c.want {
				if got := c.t.Count(id); got != n {
					t.Errorf("%s: %s: %s = %d, want %d", f, c.name, id, got, n)
				}
			}
		}
		// The split answer alone: its assent captured, its refusal cited
		// once, by the operation that holds the refused value only.
		var split Tally
		s := scriptByID(t, "assent.02")
		o := runPass(t, s, 1, &Store{}, f, canned(t, f, "assent.02.1.assent-cited"), nil)
		o.First.Ops = o.First.Ops[:1]
		split.Add(o)
		if split.Count("assent_cited_refusal") != 0 || split.Count("assents_captured") != 1 {
			t.Errorf("%s: the assented half alone counts %d refusals cited, %d captured", f, split.Count("assent_cited_refusal"), split.Count("assents_captured"))
		}
	}
}

// TestPlantSlotMatchesInAnyCase: the planted fact's first slot is found in
// an operation's text in any letter case, whether the text or the slot is
// the one in another case.
func TestPlantSlotMatchesInAnyCase(t *testing.T) {
	for _, f := range Forms {
		s := scriptByID(t, "assent.01")
		o := runPass(t, s, 1, &Store{}, f, canned(t, f, "assent.01.1.assent-cited"), nil)
		var base Tally
		base.Add(o)
		ids := []string{"assent_cited_refusal", "assent_cited_hedge", "accepted_from_refusal", "accepted_from_hedge", "assents_captured"}
		if base.Count("assent_cited_refusal") == 0 || base.Count("assents_captured") == 0 {
			t.Fatalf("%s: the canned answer counts nothing: %v", f, counts(base))
		}
		// Every text in upper case.
		upper := o
		upper.First.Ops = slices.Clone(o.First.Ops)
		for i, j := range upper.First.Ops {
			if j.Op.Text != nil {
				text := strings.ToUpper(*j.Op.Text)
				upper.First.Ops[i].Op.Text = &text
			}
		}
		// Every slot in upper case, the texts as they were.
		slots := o
		slots.Script.Scenario.Facts = slices.Clone(o.Script.Scenario.Facts)
		for i, fact := range slots.Script.Scenario.Facts {
			fact.Slots = slices.Clone(fact.Slots)
			for k := range fact.Slots {
				fact.Slots[k].Value = strings.ToUpper(fact.Slots[k].Value)
			}
			slots.Script.Scenario.Facts[i] = fact
		}
		for name, other := range map[string]Outcome{"texts": upper, "slots": slots} {
			var tally Tally
			tally.Add(other)
			for _, id := range ids {
				if tally.Count(id) != base.Count(id) {
					t.Errorf("%s: %s in upper case: %s = %d, want %d", f, name, id, tally.Count(id), base.Count(id))
				}
			}
		}
	}
}

// TestPlantsOfAnotherSessionAreNotCounted: a plant counts only in a pass
// that shows its answer, in its own session.
func TestPlantsOfAnotherSessionAreNotCounted(t *testing.T) {
	s := scriptByID(t, "assent.01")
	o := runPass(t, s, 1, &Store{}, FormQuotes, canned(t, FormQuotes, "assent.01.1.assent-cited"), nil)
	for i := range o.Script.Plants {
		o.Script.Plants[i].Session = "another-session"
	}
	var tally Tally
	tally.Add(o)
	for _, id := range []string{"plants_refusal", "plants_hedge", "plants_assent", "assent_cited_refusal", "assents_captured"} {
		if tally.Count(id) != 0 {
			t.Errorf("%s = %d", id, tally.Count(id))
		}
	}
}

// TestCountsAddUp: over every canned answer, each operation proposed is
// accepted or refused once, finally; nothing recovered was not sent; no
// more spans are missed than cited.
func TestCountsAddUp(t *testing.T) {
	type run struct{ id, name, repair string }
	runs := []run{
		{"assent.01", "faults", "faults.repair"}, {"assent.01", "assent-cited", ""}, {"assent.02", "assent-cited", ""},
		{"v1.05", "ungrounded", "ungrounded.repair"},
	}
	for _, id := range faithful {
		runs = append(runs, run{id, "faithful", ""})
	}
	for _, f := range Forms {
		var all Tally
		for _, r := range runs {
			s := scriptByID(t, r.id)
			st := &Store{}
			for n := 1; n <= len(s.Scenario.Passes); n++ {
				name := fmt.Sprintf("%s.%d.%s", r.id, n, r.name)
				var repair []byte
				if r.repair != "" {
					repair = canned(t, f, fmt.Sprintf("%s.%d.%s", r.id, n, r.repair))
				}
				var one Tally
				o := runPass(t, s, n, st, f, canned(t, f, name), repair)
				one.Add(o)
				all.Add(o)
				checkSums(t, string(f)+" "+name, one)
			}
		}
		checkSums(t, string(f)+" all", all)
	}
}

// checkSums checks what a tally's counts must hold among themselves.
func checkSums(t *testing.T, name string, tally Tally) {
	t.Helper()
	c := counts(tally)
	refused := 0
	for _, code := range Codes {
		refused += c["refused_"+code.ID]
	}
	switch {
	case c["ops_proposed"] != c["ops_accepted_final"]+refused:
		t.Errorf("%s: %d proposed, %d accepted and %d refused", name, c["ops_proposed"], c["ops_accepted_final"], refused)
	case c["ops_accepted_first"] > c["ops_proposed"] || c["repair_recovered"] > c["repair_sent"]:
		t.Errorf("%s: %v", name, c)
	case c["ops_accepted_final"] != c["ops_accepted_first"]+c["repair_recovered"]:
		t.Errorf("%s: %d accepted at last, %d first and %d recovered", name, c["ops_accepted_final"], c["ops_accepted_first"], c["repair_recovered"])
	case c["spans_missed_first"] > c["spans_cited_first"] || c["spans_missed_final"] > c["spans_cited_final"]:
		t.Errorf("%s: spans %v", name, c)
	case c["repair_prefix_bytes"] > c["repair_input_bytes"]:
		t.Errorf("%s: a prefix longer than its input", name)
	}
}

// TestTallyIDs: the counts are the card's, refused_<code> for every code,
// each once, and each is among the ids the spike writes, so the run's
// Forbid is checked against it; an empty tally gives every count as zero
// and no ratio.
func TestTallyIDs(t *testing.T) {
	written := writtenIDs()
	for _, id := range append(slices.Clone(CountIDs), RatioIDs...) {
		if !slices.Contains(written, "spike."+id) {
			t.Errorf("%s is not among the ids the spike writes", id)
		}
	}
	for _, c := range Codes {
		if !slices.Contains(CountIDs, "refused_"+c.ID) {
			t.Errorf("no count for %s", c.ID)
		}
	}
	sorted := slices.Sorted(slices.Values(CountIDs))
	if len(slices.Compact(sorted)) != len(CountIDs) || len(CountIDs) != 24+len(Codes) {
		t.Errorf("%d counts: %v", len(CountIDs), CountIDs)
	}
	var empty Tally
	if got := empty.Counts(); len(got) != len(CountIDs) || slices.ContainsFunc(got, func(m Measure) bool { return m.Value != 0 }) {
		t.Errorf("an empty tally counts %v", got)
	}
	if got := empty.Ratios(); len(got) != 0 {
		t.Errorf("an empty tally has ratios %v", got)
	}
	// A ratio with no denominator is left out, the others kept.
	part := Tally{n: map[string]int{"spans_cited_first": 4, "spans_missed_first": 1}}
	if got := part.Ratios(); !slices.Equal(got, []Measure{{"quote_miss_first", 0.25}}) {
		t.Errorf("ratios %v", got)
	}
}

// TestTallyReadsFirstWithoutARepairCall: without a repair call the first
// verdict is final, whatever Final holds; an invalid answer counts as
// that alone, and an invalid repair answer as one more.
func TestTallyReadsFirstWithoutARepairCall(t *testing.T) {
	f := FormQuotes
	o := runPass(t, scriptByID(t, "assent.01"), 1, &Store{}, f, canned(t, f, "assent.01.1.faults"), nil)
	o.Final = Verdict{Ops: []Judged{{}, {}}}
	var tally Tally
	tally.Add(o)
	if tally.Count("ops_accepted_final") != 2 || tally.Count("refused_tag_too_low") != 1 || tally.Count("repair_calls") != 0 || tally.Count("repair_sent") != 0 {
		t.Errorf("counts %v", counts(tally))
	}
	var bad Tally
	bad.Add(runPass(t, scriptByID(t, "assent.01"), 1, &Store{}, f, canned(t, f, "assent.01.1.faults"), []byte("not json")))
	bad.Add(Outcome{Script: scriptByID(t, "v1.03"), Invalid: true, First: o.First})
	c := counts(bad)
	if c["answers_invalid"] != 2 || c["repair_calls"] != 1 || c["repair_sent"] != 5 || c["repair_recovered"] != 0 || c["ops_proposed"] != 8 || c["schema_breaks"] != 0 {
		t.Errorf("counts %v", c)
	}
}
