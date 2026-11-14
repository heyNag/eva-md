package spike

import (
	"slices"
	"strings"

	"github.com/heyNag/eva-md/internal/eval/scenario"
)

// Outcome is what one pass of a script came to, for a Tally.
type Outcome struct {
	Script Script
	// Input is the pass as rendered.
	Input Input
	// Invalid: the first answer was not the schema's JSON, so First holds
	// no operation and no repair call was made. It counts in
	// answers_invalid_first, RepairInvalid in answers_invalid_repair.
	Invalid bool
	// First is Check's verdict on the first answer.
	First Verdict
	// RepairCalled: a repair call was made. RepairInvalid: its answer was
	// not the schema's JSON. RepairInputBytes is the repair input's
	// length, RepairPrefixBytes the length of the first call's input it
	// starts with.
	RepairCalled, RepairInvalid         bool
	RepairInputBytes, RepairPrefixBytes int
	// Final is ApplyRepair's verdict when a repair call was made, with an
	// empty Repair when it failed or its answer was invalid; Tally reads
	// First in its place when none was made.
	Final Verdict
}

// CountIDs are a Tally's counts, by their fixed ids, in order.
var CountIDs = func() []string {
	ids := []string{"spans_cited_first", "spans_missed_first", "spans_cited_final", "spans_missed_final",
		"ops_proposed", "ops_accepted_first", "ops_accepted_final"}
	for _, c := range Codes {
		ids = append(ids, "refused_"+c.ID)
	}
	return append(ids, "answers_invalid_first", "answers_invalid_repair", "schema_breaks_first", "schema_breaks_repair", "nothing", "role_fixed",
		"repair_calls", "repair_sent", "repair_recovered", "repair_input_bytes", "repair_prefix_bytes",
		"plants_refusal", "plants_hedge", "plants_assent", "assent_cited_refusal", "assent_cited_hedge",
		"accepted_from_refusal", "accepted_from_hedge", "assents_captured")
}()

// RatioIDs are a Tally's ratios, by their fixed ids, in order.
var RatioIDs = []string{"quote_miss_first", "quote_miss_final", "repair_yield"}

// Measure is one count or ratio of a Tally.
type Measure struct {
	ID    string
	Value float64
}

// Tally counts what passes came to, over passes and scripts. Its zero
// value is empty and ready.
type Tally struct {
	n map[string]int
}

// Count is the count id holds, 0 for an id it does not know.
func (t Tally) Count(id string) int { return t.n[id] }

// Counts is every count, by CountIDs' order, zeros included.
func (t Tally) Counts() []Measure {
	out := make([]Measure, len(CountIDs))
	for i, id := range CountIDs {
		out[i] = Measure{ID: id, Value: float64(t.n[id])}
	}
	return out
}

// Ratios are quote_miss_first and quote_miss_final, the share of the spans
// cited that were not found, first and after repair, and repair_yield,
// the share of the operations sent to repair that it recovered, by
// RatioIDs' order, leaving out a ratio with no denominator.
func (t Tally) Ratios() []Measure {
	var out []Measure
	for _, r := range []struct{ id, num, den string }{
		{"quote_miss_first", "spans_missed_first", "spans_cited_first"},
		{"quote_miss_final", "spans_missed_final", "spans_cited_final"},
		{"repair_yield", "repair_recovered", "repair_sent"},
	} {
		if d := t.n[r.den]; d > 0 {
			out = append(out, Measure{ID: r.id, Value: float64(t.n[r.num]) / float64(d)})
		}
	}
	return out
}

// Add counts one pass.
func (t *Tally) Add(o Outcome) {
	if t.n == nil {
		t.n = map[string]int{}
	}
	add := func(id string, n int) { t.n[id] += n }
	one := func(b bool) int {
		if b {
			return 1
		}
		return 0
	}
	add("answers_invalid_first", one(o.Invalid))
	add("answers_invalid_repair", one(o.RepairCalled && o.RepairInvalid))
	add("repair_calls", one(o.RepairCalled))
	if o.RepairCalled {
		add("repair_input_bytes", o.RepairInputBytes)
		add("repair_prefix_bytes", o.RepairPrefixBytes)
	}
	t.plants(o)
	if o.Invalid {
		return
	}
	final := o.First
	if o.RepairCalled {
		final = o.Final
	}
	add("schema_breaks_first", one(o.First.Breaks))
	add("schema_breaks_repair", one(o.RepairCalled && final.RepairBreaks))
	add("ops_proposed", len(o.First.Ops))
	add("nothing", one(len(o.First.Ops) == 0))
	for _, j := range o.First.Ops {
		add("ops_accepted_first", one(j.Accepted()))
		for _, s := range j.Spans {
			add("spans_cited_first", 1)
			add("spans_missed_first", one(s.Code != ""))
		}
	}
	for _, j := range final.Ops {
		if j.Accepted() {
			add("ops_accepted_final", 1)
		} else {
			add("refused_"+j.Code, 1)
		}
		add("repair_sent", one(j.Sent))
		add("repair_recovered", one(j.Sent && j.Accepted()))
		for _, s := range j.Spans {
			add("spans_cited_final", 1)
			add("spans_missed_final", one(s.Code != ""))
			add("role_fixed", one(s.RoleFixed))
		}
	}
	t.assents(o, final)
}

// shownPlants are the plants of o's script whose answer the pass shows.
func shownPlants(o Outcome) []Plant {
	var out []Plant
	for _, p := range o.Script.Plants {
		if slices.ContainsFunc(o.Input.Entries, func(e Entry) bool { return e.Session == p.Session && e.N == p.Seq }) {
			out = append(out, p)
		}
	}
	return out
}

// plants counts the plants the pass shows, by mode.
func (t *Tally) plants(o Outcome) {
	for _, p := range shownPlants(o) {
		t.n["plants_"+p.Mode]++
	}
}

// assents counts, for each plant the pass shows, the item operations of
// the final verdict that keep its fact as a decision or a procedure: of
// kind decision or procedure, with the fact's first slot in their text in
// any case, citing the plant's answer whatever their spans' as. For a
// refusal or a hedge each is counted in assent_cited_<mode>, and in
// accepted_from_<mode> when accepted as stated or accepted; an assent is
// counted once in assents_captured when one of them is accepted.
func (t *Tally) assents(o Outcome, final Verdict) {
	for _, p := range shownPlants(o) {
		i := slices.IndexFunc(o.Script.Scenario.Facts, func(f scenario.Fact) bool { return f.ID == p.Fact })
		if i < 0 || len(o.Script.Scenario.Facts[i].Slots) == 0 {
			continue
		}
		value := strings.ToLower(o.Script.Scenario.Facts[i].Slots[0].Value)
		captured := false
		for _, j := range final.Ops {
			op := j.Op
			if !strings.HasPrefix(op.Op, "item.") || op.Kind == nil || (*op.Kind != "decision" && *op.Kind != "procedure") ||
				op.Text == nil || !strings.Contains(strings.ToLower(*op.Text), value) ||
				!slices.ContainsFunc(op.Ev, func(s Span) bool { return s.E == p.Seq }) {
				continue
			}
			kept := j.Accepted() && (j.Tag == TagStated || j.Tag == TagAccepted)
			switch p.Mode {
			case ModeRefusal, ModeHedge:
				t.n["assent_cited_"+p.Mode]++
				if kept {
					t.n["accepted_from_"+p.Mode]++
				}
			case ModeAssent:
				captured = captured || kept
			}
		}
		if captured {
			t.n["assents_captured"]++
		}
	}
}
