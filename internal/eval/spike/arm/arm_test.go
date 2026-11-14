package arm

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/heyNag/eva-md/internal/eval/results"
	"github.com/heyNag/eva-md/internal/eval/scenario"
	"github.com/heyNag/eva-md/internal/eval/score"
	"github.com/heyNag/eva-md/internal/eval/spike"
	"github.com/heyNag/eva-md/internal/ledger/event"
)

// faithfulIDs are the scripts DV-03b answered faithfully in both forms
// that the fidelity scorer finds whole: three of v1's and two of assent.
var faithfulIDs = []string{"v1.03", "v1.05", "v1.06", "assent.01", "assent.02"}

// scripts are the spike's scripts with the given ids, in that order.
func scripts(t *testing.T, ids ...string) []spike.Script {
	t.Helper()
	var out []spike.Script
	for _, id := range ids {
		i := slices.IndexFunc(spike.Scripts(), func(s spike.Script) bool { return s.Scenario.ID == id })
		if i < 0 {
			t.Fatalf("no script %s", id)
		}
		out = append(out, spike.Scripts()[i])
	}
	return out
}

// arm is an arm of the form f, with made-up words.
func arm(f spike.Form) Arm {
	return Arm{Host: "codex", HostVersion: "0.99.1", Tier: TierSmall, Effort: "low", Form: f}
}

// answer reads the canned answer <name>.json of form f: this package's
// test data first, then DV-03b's.
func answer(t *testing.T, f spike.Form, name string) []byte {
	t.Helper()
	for _, dir := range []string{"testdata", filepath.Join("..", "testdata")} {
		data, err := os.ReadFile(filepath.Join(dir, "answers", string(f), name+".json"))
		if err == nil {
			return data
		}
		if !errors.Is(err, os.ErrNotExist) {
			t.Fatal(err)
		}
	}
	t.Fatalf("no canned answer %s/%s", f, name)
	return nil
}

// canned is an answering function over canned answers. The first call of
// pass n of script s is answered with the answer names gives for "s.n",
// "s.n.faithful" when it gives none, and its repair call with that name
// and ".repair". A name of "" answers nothing ({"ops":[]}). fail gives a
// reason for a call by "s.n.kind", and raw its output as given; cached
// gives each repair reply cached input. Every call is kept in calls; once
// shut, a call fails the test.
type canned struct {
	t      *testing.T
	f      spike.Form
	names  map[string]string
	fail   map[string]string
	raw    map[string][]byte
	cached bool
	calls  []Call
	shut   bool
}

func (c *canned) ask(_ context.Context, call Call) (Reply, error) {
	c.t.Helper()
	if c.shut {
		c.t.Errorf("the live answering function was called for %s pass %d", call.Script, call.Pass)
		return Reply{}, errors.New("shut")
	}
	c.calls = append(c.calls, call)
	at := fmt.Sprintf("%s.%d", call.Script, call.Pass)
	r := Reply{TokensIn: (len(call.System) + len(call.Input)) / 4, Attempts: 1, Seconds: 0.25 * float64(call.Pass), Effort: "low"}
	if reason, ok := c.fail[at+"."+call.Kind]; ok {
		r.Reason = reason
		return r, nil
	}
	if c.cached && call.Kind == KindRepair {
		n := (len(call.System) + len(call.Input)) / 8
		r.CachedIn = &n
	}
	name, ok := c.names[at]
	if !ok {
		name = at + ".faithful"
	}
	out, ok := c.raw[at+"."+call.Kind]
	switch {
	case ok:
		r.Output = out
	case name == "":
		r.Output = []byte(`{"ops": [], "nothing": "none_new"}`)
	case call.Kind == KindRepair:
		r.Output = answer(c.t, c.f, name+".repair")
	default:
		r.Output = answer(c.t, c.f, name)
	}
	r.TokensOut = len(r.Output) / 4
	return r, nil
}

// run runs a over ids with c, and fails the test on an error.
func run(t *testing.T, a Arm, c *canned, ids ...string) Result {
	t.Helper()
	res, err := Run(context.Background(), a, scripts(t, ids...), c.ask, nil)
	if err != nil {
		t.Fatal(err)
	}
	return res
}

// template is a row template with a made-up commit and a fixed time.
func template() results.Row {
	return results.Row{Commit: strings.Repeat("0123456789abcdef", 3)[:40], Seed: 7, Time: time.Date(2026, 3, 3, 12, 0, 0, 0, time.UTC)}
}

// figures are rows' values by "scenario metric".
func figures(rows []results.Row) map[string]float64 {
	out := map[string]float64{}
	for _, r := range rows {
		k := r.Scenario + " " + r.Metric
		if _, ok := out[k]; ok {
			panic("a row given twice: " + k)
		}
		out[k] = r.Value
	}
	return out
}

// TestFaithfulAnswers: an arm over three of v1's scripts and two of
// assent on faithful answers keeps every fact that must hold, refuses
// nothing and makes no repair call, in both forms, on both sides.
func TestFaithfulAnswers(t *testing.T) {
	for _, f := range spike.Forms {
		c := &canned{t: t, f: f}
		res := run(t, arm(f), c, faithfulIDs...)
		got := figures(Rows(res, template()))
		for _, side := range []string{"on", "off"} {
			for _, id := range append(slices.Clone(faithfulIDs), "all") {
				sc := scenarioID(arm(f), side, id)
				want := map[string]float64{score.MetricFactHolds: 1, score.MetricFactRecall: 1, score.MetricWrongWrite: 0,
					score.MetricMisattributed: 0, score.MetricViolations: 0, "spike.calls_repair": 0, "spike.repair_calls": 0,
					"spike.calls_failed": 0, "spike.answers_invalid_first": 0, "spike.honest_refused": 0}
				for _, code := range spike.Codes {
					want["spike.refused_"+code.ID] = 0
				}
				for m, v := range want {
					if g, ok := got[sc+" "+m]; !ok || g != v {
						t.Errorf("%s %s = %v (given %v), want %v", sc, m, g, ok, v)
					}
				}
				if got[sc+" spike.ops_accepted_final"] == 0 || got[sc+" spike.ops_accepted_final"] != got[sc+" spike.ops_proposed"] {
					t.Errorf("%s: %v of %v operations accepted", sc, got[sc+" spike.ops_accepted_final"], got[sc+" spike.ops_proposed"])
				}
			}
		}
		passes := 0
		for _, s := range scripts(t, faithfulIDs...) {
			passes += len(s.Scenario.Passes)
		}
		if len(c.calls) != passes || got[scenarioID(arm(f), "on", "all")+" spike.calls_first"] != float64(passes) {
			t.Errorf("%s: %d calls, want one for each of %d passes", f, len(c.calls), passes)
		}
		for _, sr := range res.Rounds[0].Scripts {
			if !reflect.DeepEqual(sr.On.Items, sr.Off.Items) || len(sr.On.Items) == 0 {
				t.Errorf("%s %s: the stores differ, or are empty, with nothing to repair", f, sr.Script)
			}
		}
	}
}

// TestOneQuoteOffByAWord: one span off by a word makes one repair call,
// and a repair that fixes it leaves on and off differing by exactly that
// item.
func TestOneQuoteOffByAWord(t *testing.T) {
	for _, f := range spike.Forms {
		c := &canned{t: t, f: f, names: map[string]string{"assent.01.1": "assent.01.1.off-by-a-word"}}
		res := run(t, arm(f), c, "assent.01")
		sr := res.Rounds[0].Scripts[0]
		if sr.On.Calls.Repair != 1 || sr.Off.Calls.Repair != 0 || len(c.calls) != 2 || c.calls[1].Kind != KindRepair {
			t.Fatalf("%s: %d repair calls on, %d off, %d calls", f, sr.On.Calls.Repair, sr.Off.Calls.Repair, len(c.calls))
		}
		code := "quote_not_found"
		if f == spike.FormSegments {
			code = "bad_segment"
		}
		if !bytes.Contains(c.calls[1].Input, []byte("\n#5 "+code+" ev.1.")) || !bytes.HasPrefix(c.calls[1].Input, c.calls[0].Input) ||
			!bytes.Equal(c.calls[1].Schema, spike.RepairSchema(f)) || c.calls[1].System != spike.Prompt(f) {
			t.Errorf("%s: the repair call is not the fifth operation's repair input", f)
		}
		on, off := sr.On.Items, sr.Off.Items
		if len(on) != len(off)+1 {
			t.Fatalf("%s: %d items on, %d off", f, len(on), len(off))
		}
		var only []spike.Item
		for _, it := range on {
			i := slices.IndexFunc(off, func(o spike.Item) bool { return o.Text == it.Text && o.Kind == it.Kind && o.Tag == it.Tag })
			if i < 0 {
				only = append(only, it)
			}
		}
		if len(only) != 1 || only[0].Kind != "decision" || !strings.Contains(only[0].Text, "rejects.jsonl") || only[0].Tag != spike.TagAccepted {
			t.Errorf("%s: on alone holds %+v", f, only)
		}
		on1, off1 := sr.On.Tally, sr.Off.Tally
		if on1.Count("repair_recovered") != 1 || on1.Count("ops_accepted_final") != 6 || off1.Count("ops_accepted_final") != 5 ||
			off1.Count("refused_"+code) != 1 || on1.Count("refused_"+code) != 0 || off1.Count("repair_calls") != 0 {
			t.Errorf("%s: on and off tallies do not tell the repair apart", f)
		}
		// What the repair call reads is the prompt and its input, and what
		// it shares with the first call is the prompt and the first input.
		prompt := len(spike.Prompt(f))
		if on1.Count("repair_input_bytes") != prompt+len(c.calls[1].Input) || on1.Count("repair_prefix_bytes") != prompt+len(c.calls[0].Input) ||
			off1.Count("repair_input_bytes") != 0 {
			t.Errorf("%s: repair input %d bytes, prefix %d", f, on1.Count("repair_input_bytes"), on1.Count("repair_prefix_bytes"))
		}
		if want := (prompt + len(c.calls[1].Input)) / 4; sr.On.Calls.RepairTokensIn != want || sr.Off.Calls.RepairTokensIn != 0 ||
			sr.On.Calls.TokensIn != want+(prompt+len(c.calls[0].Input))/4 || sr.Off.Calls.TokensIn != (prompt+len(c.calls[0].Input))/4 {
			t.Errorf("%s: tokens %+v on, %+v off", f, sr.On.Calls, sr.Off.Calls)
		}
		if sr.On.Fidelity.Metrics.FactHolds != 1 || sr.Off.Fidelity.Metrics.FactHolds >= 1 {
			t.Errorf("%s: fact_holds %v on, %v off", f, sr.On.Fidelity.Metrics.FactHolds, sr.Off.Fidelity.Metrics.FactHolds)
		}
	}
}

// TestOffFollowsOnsHandles: when a repair adds a record on keeps and off
// does not, a later pass's handles name on's records, and off applies
// each to its own record of the same operation, or skips one whose target
// it never held.
func TestOffFollowsOnsHandles(t *testing.T) {
	f := spike.FormQuotes
	c := &canned{t: t, f: f, names: map[string]string{"v1.08.1": "v1.08.1.off-by-a-word"}}
	res := run(t, arm(f), c, "v1.08")
	sr := res.Rounds[0].Scripts[0]
	type rec struct{ kind, state, text string }
	view := func(items []spike.Item) []rec {
		var out []rec
		for _, it := range items {
			out = append(out, rec{it.Kind, it.State, it.Text})
		}
		return out
	}
	wantOn := []rec{
		{"next", "retired", "Write the parser, MARTIN-1."},
		{"next", "active", "Write the renderer after the parser, MARTIN-2."},
		{"question", "retired", "Should pages hold hidden ids, MARTIN-3?"},
		{"fact", "active", "The project uses Go 1.26, MARTIN-4."},
		{"decision", "active", "Pages hold hidden ids."},
		{"did", "log", "The parser tests pass."},
	}
	if got := view(sr.On.Items); !reflect.DeepEqual(got, wantOn) {
		t.Errorf("on holds %+v, want %+v", got, wantOn)
	}
	if got := view(sr.Off.Items); !reflect.DeepEqual(got, wantOn[1:]) {
		t.Errorf("off holds %+v, want %+v", got, wantOn[1:])
	}
	if sr.On.Tally.Count("ops_accepted_final") != 8 || sr.Off.Tally.Count("ops_accepted_final") != 7 {
		t.Errorf("accepted %d on, %d off", sr.On.Tally.Count("ops_accepted_final"), sr.Off.Tally.Count("ops_accepted_final"))
	}
}

// TestAFailedCallIsCountedAndTheRunGoesOn: a first call that fails leaves
// its pass with no answer, a repair call that fails leaves the first
// verdict, each is counted, and every later pass is still asked.
func TestAFailedCallIsCountedAndTheRunGoesOn(t *testing.T) {
	f := spike.FormQuotes
	c := &canned{t: t, f: f, names: map[string]string{"assent.01.1": "assent.01.1.off-by-a-word"},
		fail: map[string]string{"v1.05.1.first": "timeout", "assent.01.1.repair": "rate_limit"}}
	res := run(t, arm(f), c, "v1.05", "v1.03", "assent.01")
	if len(c.calls) != 5 {
		t.Fatalf("%d calls, want 5: the run stopped", len(c.calls))
	}
	v105, v103, a01 := res.Rounds[0].Scripts[0], res.Rounds[0].Scripts[1], res.Rounds[0].Scripts[2]
	if v105.On.Calls.Failed != 1 || v105.Off.Calls.Failed != 1 || v105.On.Calls.First != 1 || v103.On.Calls.Failed != 0 ||
		a01.On.Calls.Failed != 1 || a01.Off.Calls.Failed != 0 || res.Rounds[0].On.Calls.Failed != 2 || res.Rounds[0].Off.Calls.Failed != 1 {
		t.Errorf("failed calls: %+v, %+v", v105.On.Calls, a01.On.Calls)
	}
	// v1.05's pass is no answer, not a pass of the tally: neither invalid
	// nor nothing, and its plants unshown.
	if counts := v105.On.Tally.Counts(); slices.ContainsFunc(counts, func(m spike.Measure) bool { return m.Value != 0 }) ||
		len(v105.On.Items) != 0 || v105.On.Fidelity.Metrics.FactHolds != 0 {
		t.Errorf("v1.05 after a failed first call: %v", counts)
	}
	if v103.On.Fidelity.Metrics.FactHolds != 1 || len(v103.On.Items) != 2 {
		t.Error("the script after a failed call was not run whole")
	}
	if a01.On.Tally.Count("repair_calls") != 1 || a01.On.Tally.Count("repair_sent") != 1 || a01.On.Tally.Count("repair_recovered") != 0 ||
		a01.On.Tally.Count("answers_invalid_repair") != 0 || !reflect.DeepEqual(a01.On.Items, a01.Off.Items) {
		t.Errorf("assent.01 after a failed repair call: %v", a01.On.Tally.Counts())
	}
	rows := figures(Rows(res, template()))
	if rows[scenarioID(arm(f), "on", "all")+" spike.calls_failed"] != 2 || rows[scenarioID(arm(f), "off", "all")+" spike.calls_failed"] != 1 ||
		rows[scenarioID(arm(f), "on", "all")+" spike.calls_first"] != 4 || rows[scenarioID(arm(f), "on", "all")+" spike.calls_repair"] != 1 {
		t.Error("the rows do not count the failed calls")
	}
}

// TestAFailedCallIgnoresItsOutput: a reply with a reason is not read,
// though it gives an answer.
func TestAFailedCallIgnoresItsOutput(t *testing.T) {
	f := spike.FormQuotes
	ask := func(_ context.Context, c Call) (Reply, error) {
		return Reply{Reason: "host", Output: answer(t, f, fmt.Sprintf("%s.%d.faithful", c.Script, c.Pass))}, nil
	}
	var rec bytes.Buffer
	res, err := Run(context.Background(), arm(f), scripts(t, "v1.05"), ask, &rec)
	if err != nil {
		t.Fatal(err)
	}
	if sr := res.Rounds[0].Scripts[0]; len(sr.On.Items) != 0 || sr.On.Calls.Failed != 1 {
		t.Errorf("a failed call's answer was kept: %+v", sr.On.Items)
	}
	if !strings.Contains(rec.String(), `"output":null,"reason":"host"`) {
		t.Errorf("the recording keeps a failed call's answer: %s", rec.String())
	}
}

// TestCachedInput: repair_tokens_cached_in is written only when every
// repair call reported cached input, as their sum.
func TestCachedInput(t *testing.T) {
	f := spike.FormQuotes
	names := map[string]string{"assent.01.1": "assent.01.1.off-by-a-word", "v1.05.1": "v1.05.1.ungrounded"}
	metric := " spike.repair_tokens_cached_in"
	for _, cached := range []bool{false, true} {
		c := &canned{t: t, f: f, names: names, cached: cached}
		got := figures(Rows(run(t, arm(f), c, "assent.01", "v1.05", "v1.03"), template()))
		want := 0
		for _, call := range c.calls {
			if call.Kind == KindRepair {
				want += (len(call.System) + len(call.Input)) / 8
			}
		}
		for _, id := range []string{"assent.01", "v1.05", "all"} {
			v, ok := got[scenarioID(arm(f), "on", id)+metric]
			if ok != cached {
				t.Errorf("cached %v: %s has a cached row: %v", cached, id, ok)
			}
			if id == "all" && cached && v != float64(want) {
				t.Errorf("cached input %v, want %d", v, want)
			}
		}
		for _, id := range []string{"v1.03", "all"} {
			if _, ok := got[scenarioID(arm(f), "off", id)+metric]; ok {
				t.Errorf("cached %v: off %s, with no repair call, has a cached row", cached, id)
			}
		}
		if _, ok := got[scenarioID(arm(f), "on", "v1.03")+metric]; ok {
			t.Errorf("v1.03, with no repair call, has a cached row")
		}
	}
	// One repair call that reports none, beside one that does: no row.
	c := &canned{t: t, f: f, names: names, cached: true}
	ask := func(ctx context.Context, call Call) (Reply, error) {
		r, err := c.ask(ctx, call)
		if call.Script == "v1.05" {
			r.CachedIn = nil
		}
		return r, err
	}
	res, err := Run(context.Background(), arm(f), scripts(t, "assent.01", "v1.05"), ask, nil)
	if err != nil {
		t.Fatal(err)
	}
	got := figures(Rows(res, template()))
	if _, ok := got[scenarioID(arm(f), "on", "all")+metric]; ok {
		t.Error("a cached row though one repair call reported none")
	}
	if _, ok := got[scenarioID(arm(f), "on", "assent.01")+metric]; !ok {
		t.Error("no cached row for the script whose repair call reported it")
	}
}

// TestHonestRefused: an operation refused for its evidence that holds a
// fact the user said counts as honest; one that holds a changed value
// does not; the ratio is left out with no denominator.
func TestHonestRefused(t *testing.T) {
	f := spike.FormQuotes
	skip := func(_ context.Context, c Call) (Reply, error) {
		if c.Kind == KindRepair {
			return Reply{Output: []byte(`{"fixes":[{"index":5,"op":null},{"index":1,"op":null}]}`)}, nil
		}
		name := map[string]string{"assent.01": "assent.01.1.off-by-a-word", "v1.05": "v1.05.1.ungrounded"}[c.Script]
		return Reply{Output: answer(t, f, name)}, nil
	}
	res, err := Run(context.Background(), arm(f), scripts(t, "assent.01", "v1.05"), skip, nil)
	if err != nil {
		t.Fatal(err)
	}
	a01, v105 := res.Rounds[0].Scripts[0], res.Rounds[0].Scripts[1]
	// assent.01: the decision refused for a quote off by a word holds its
	// fact; beside it the procedure and nothing else of the user's.
	for _, sd := range []Side{a01.On, a01.Off} {
		if sd.Honest != 1 || sd.HonestOf != 2 {
			t.Errorf("assent.01: honest %d of %d, want 1 of 2", sd.Honest, sd.HonestOf)
		}
	}
	// v1.05: the decision with a changed number is refused rightly.
	if v105.On.Honest != 0 || v105.On.HonestOf != 0 {
		t.Errorf("v1.05: honest %d of %d, want 0 of 0", v105.On.Honest, v105.On.HonestOf)
	}
	got := figures(Rows(res, template()))
	if v := got[scenarioID(arm(f), "on", "assent.01")+" spike.honest_refused"]; v != 0.5 {
		t.Errorf("honest_refused %v, want 0.5", v)
	}
	if _, ok := got[scenarioID(arm(f), "on", "v1.05")+" spike.honest_refused"]; ok {
		t.Error("honest_refused written with no denominator")
	}
	if v := got[scenarioID(arm(f), "on", "all")+" spike.honest_refused"]; v != 0.5 {
		t.Errorf("honest_refused over all %v, want 0.5", v)
	}
	// v1.08, faithful: five items of the user's, one of which, a next step
	// that names an ended step's value, the scorer finds stale; the note is
	// the tool's.
	sd := run(t, arm(f), &canned{t: t, f: f}, "v1.08").Rounds[0].Scripts[0].On
	kept := 0
	for _, it := range sd.Items {
		if it.Tag == spike.TagStated || it.Tag == spike.TagAccepted {
			kept++
		}
	}
	if kept != 5 || sd.Honest != 0 || sd.HonestOf != 4 {
		t.Errorf("v1.08: %d items of the user's, honest %d of %d, want 0 of 4", kept, sd.Honest, sd.HonestOf)
	}
}

// TestSoloNeedsAUsersEntry: a refused operation that cites no user's
// shown entry, or adds no record, is not exported; a note is a dated line
// under its entry's day.
func TestSoloNeedsAUsersEntry(t *testing.T) {
	if string(event.AuthorTyped) != userAuthors[0] || string(event.AuthorChosen) != userAuthors[1] || len(userAuthors) != 2 {
		t.Fatalf("userAuthors %v are not the record's typed and chosen", userAuthors)
	}
	s := scripts(t, "assent.01")[0]
	sess := s.Scenario.Sessions[0]
	in, err := spike.Render(sess, nil, spike.FormQuotes)
	if err != nil {
		t.Fatal(err)
	}
	at, err := times(sess)
	if err != nil {
		t.Fatal(err)
	}
	str := func(s string) *string { return &s }
	agent := spike.Op{Op: "item.add", Kind: str("decision"), Text: str("x"), Ev: []spike.Span{{E: 2, As: "proposal"}}}
	if _, ok := solo(agent, in, at); ok {
		t.Error("an operation citing the agent alone was exported")
	}
	unshown := spike.Op{Op: "item.add", Kind: str("decision"), Text: str("x"), Ev: []spike.Span{{E: 999, As: "said"}}}
	if _, ok := solo(unshown, in, at); ok {
		t.Error("an operation citing no shown entry was exported")
	}
	retire := spike.Op{Op: "item.retire", ID: str("h1"), Text: str("x"), Ev: []spike.Span{{E: 3, As: "said"}}}
	if _, ok := solo(retire, in, at); ok {
		t.Error("an operation that adds no record was exported")
	}
	note := spike.Op{Op: "note", Kind: str("found"), Text: str("n"), Ev: []spike.Span{{E: 2, As: "proposal"}, {E: 3, As: "said"}, {E: 3, As: "said"}}}
	it, ok := solo(note, in, at)
	want := score.Item{Kind: scenario.KindLog, State: score.StateLog, Authority: score.TagStated, Text: "n",
		Date: at[3].Format(time.DateOnly), Sources: []score.Source{{Session: sess.ID, Seq: 2}, {Session: sess.ID, Seq: 3}}}
	if !ok || !reflect.DeepEqual(it, want) {
		t.Errorf("note exported as %+v, want %+v", it, want)
	}
	ask := spike.Op{Op: "ask.add", Text: str("q"), Ev: []spike.Span{{E: 3, As: "said"}}}
	if it, _ := solo(ask, in, at); it.Kind != scenario.KindOpen || it.State != score.StateActive {
		t.Errorf("a question exported as %+v", it)
	}
}

// TestExportByHand: a store's records reach the scorer as the card maps
// them, and the per-script fidelity rows are the scorer's own on that
// export.
func TestExportByHand(t *testing.T) {
	f := spike.FormQuotes
	res := run(t, arm(f), &canned{t: t, f: f}, "v1.08")
	sr := res.Rounds[0].Scripts[0]
	sc := scripts(t, "v1.08")[0].Scenario
	s1, s2 := sc.Sessions[0].ID, sc.Sessions[1].ID
	src := func(s string, n ...int64) []score.Source {
		var out []score.Source
		for _, x := range n {
			out = append(out, score.Source{Session: s, Seq: x})
		}
		return out
	}
	want := []score.Item{
		{Kind: scenario.KindNext, State: score.StateRetired, Authority: "stated", Text: "Write the parser, MARTIN-1.", Sources: src(s1, 1)},
		{Kind: scenario.KindNext, State: score.StateActive, Authority: "stated", Text: "Write the renderer after the parser, MARTIN-2.", Sources: src(s1, 1)},
		{Kind: scenario.KindOpen, State: score.StateRetired, Authority: "stated", Text: "Should pages hold hidden ids, MARTIN-3?", Sources: src(s1, 1)},
		{Kind: scenario.KindFact, State: score.StateActive, Authority: "stated", Text: "The project uses Go 1.26, MARTIN-4.", Sources: src(s1, 1)},
		{Kind: scenario.KindDecision, State: score.StateActive, Authority: "stated", Text: "Pages hold hidden ids.", Sources: src(s2, 3)},
		{Kind: scenario.KindLog, State: score.StateLog, Authority: "observed", Text: "The parser tests pass.", Date: sr.On.Items[5].Day, Sources: src(s2, 1)},
	}
	ex := export(sr.On.Items)
	if !reflect.DeepEqual(ex.Items, want) {
		t.Fatalf("export\n%+v\nwant\n%+v", ex.Items, want)
	}
	if want[5].Date == "" {
		t.Fatal("the note has no day")
	}
	byHand := score.Score(sc, score.Export{Items: want})
	if !reflect.DeepEqual(sr.On.Fidelity, byHand) {
		t.Errorf("fidelity %+v, the scorer's %+v", sr.On.Fidelity.Metrics, byHand.Metrics)
	}
	got := figures(Rows(res, template()))
	for _, r := range byHand.Rows(template()) {
		if v, ok := got[scenarioID(arm(f), "on", "v1.08")+" "+r.Metric]; !ok || v != r.Value {
			t.Errorf("%s = %v, the scorer's %v", r.Metric, v, r.Value)
		}
	}
}

// TestPooledFidelity: over all scripts, the shares are of every script's
// facts that must hold together, and the counts are summed.
func TestPooledFidelity(t *testing.T) {
	f := spike.FormQuotes
	c := &canned{t: t, f: f, names: map[string]string{"v1.05.1": ""}}
	res := run(t, arm(f), c, "v1.03", "v1.05", "v1.08")
	rd := res.Rounds[0]
	required, held := 0, 0
	for _, sr := range rd.Scripts {
		empty := score.Score(scripts(t, sr.Script)[0].Scenario, score.Export{})
		n := 0
		for i, fr := range empty.Facts {
			if fr.Outcome == score.Missing {
				n++
				if sr.On.Fidelity.Facts[i].Outcome == score.Holds {
					held++
				}
			}
		}
		if sr.On.Required != n {
			t.Errorf("%s: %d facts must hold, want %d", sr.Script, sr.On.Required, n)
		}
		required += n
	}
	m := rd.On.Fidelity.Metrics
	if required == 0 || required != rd.On.Required || m.FactHolds != float64(held)/float64(required) || m.FactHolds == 1 || m.StaleInForce != 1 {
		t.Errorf("pooled %+v over %d facts, %d holding", m, required, held)
	}
	var mean float64
	for _, sr := range rd.Scripts {
		mean += sr.On.Fidelity.Metrics.FactHolds / 3
	}
	if m.FactHolds == mean {
		t.Error("the pooled share is the mean of the scripts' shares, not the share of their facts")
	}
}
