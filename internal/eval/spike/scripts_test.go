package spike

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"unicode"

	"github.com/heyNag/eva-md/internal/eval/scenario"
	"github.com/heyNag/eva-md/internal/eval/score"
	"github.com/heyNag/eva-md/internal/host/adapter"
	"github.com/heyNag/eva-md/internal/ledger/event"
	"github.com/heyNag/eva-md/internal/redact"
)

var regen = flag.Bool("regen", false, "rewrite the scripts in testdata/scripts")

// family is the scripts of one family, in Scripts' order.
func family(name string) []Script {
	var out []Script
	for _, s := range Scripts() {
		if s.Scenario.Family == name {
			out = append(out, s)
		}
	}
	return out
}

// TestScriptsOrderAndValidate: the scripts are v1's 12 as scenario.V1
// gives them, at least six of the family assent, then bounds.01, every id
// once, and each passes Validate.
func TestScriptsOrderAndValidate(t *testing.T) {
	scripts := Scripts()
	v1 := scenario.V1()
	if len(scripts) < len(v1)+7 {
		t.Fatalf("%d scripts", len(scripts))
	}
	var ids []string
	for i, s := range scripts {
		if err := s.Validate(); err != nil {
			t.Error(err)
		}
		if slices.Contains(ids, s.Scenario.ID) {
			t.Errorf("script %s is given twice", s.Scenario.ID)
		}
		ids = append(ids, s.Scenario.ID)
		switch {
		case i < len(v1):
			want, _ := json.Marshal(v1[i])
			got, _ := json.Marshal(s.Scenario)
			if !bytes.Equal(got, want) || len(s.Plants) != 0 {
				t.Errorf("script %d is not v1's %s as scenario.V1 gives it", i+1, v1[i].ID)
			}
		case i == len(scripts)-1:
			if s.Scenario.ID != "bounds.01" || s.Scenario.Family != "bounds" {
				t.Errorf("the last script is %s", s.Scenario.ID)
			}
		default:
			if want := fmt.Sprintf("assent.%02d", i-len(v1)+1); s.Scenario.ID != want || s.Scenario.Family != "assent" {
				t.Errorf("script %d is %s of %s, want %s of assent", i+1, s.Scenario.ID, s.Scenario.Family, want)
			}
		}
	}
	if n := len(family("assent")); n < 6 {
		t.Errorf("the family assent has %d scripts, want at least 6", n)
	}
}

// assentWords are the words a refusal may open with that read as a yes.
var assentWords = []string{"yes", "yeah", "yep", "ok", "okay", "sure"}

func words(text string) []string {
	return strings.FieldsFunc(strings.ToLower(text), func(r rune) bool { return !unicode.IsLetter(r) && r != '\'' })
}

// planted is a plant with the turns it names and its fact.
type planted struct {
	Plant
	script            string
	answer, proposal  scenario.Turn
	fact              scenario.Fact
	proposalPlantsTwo bool
}

func plantsOf(t *testing.T, scripts []Script) []planted {
	t.Helper()
	var out []planted
	for _, s := range scripts {
		for _, p := range s.Plants {
			sess := s.Scenario.Sessions[slices.IndexFunc(s.Scenario.Sessions, func(x scenario.Session) bool { return x.ID == p.Session })]
			fi := slices.IndexFunc(s.Scenario.Facts, func(f scenario.Fact) bool { return f.ID == p.Fact })
			if fi < 0 {
				t.Fatalf("%s: no fact %s", s.Scenario.ID, p.Fact)
			}
			prop := sess.Turns[p.Proposal-1]
			out = append(out, planted{Plant: p, script: s.Scenario.ID, answer: sess.Turns[p.Seq-1], proposal: prop,
				fact: s.Scenario.Facts[fi], proposalPlantsTwo: len(prop.Plants) == 2})
		}
	}
	return out
}

// TestAssentFamilyCounts: across the family, at least 24 refusals in 8
// wordings, 24 hedges in 8 and 12 assents in 5, the wordings counted among
// the answers to a single proposal; at least 4 one-word
// refusals and 4 that open with a word of assent; at least 4 replies that
// propose two things, one taken and one refused. Every fact the agent's
// replies plant is answered once, no refusal or hedge repeats its value,
// and no value is part of another.
func TestAssentFamilyCounts(t *testing.T) {
	ps := plantsOf(t, family("assent"))
	count := map[string]int{}
	wordings := map[string]map[string]bool{}
	oneWord, opensYes := 0, 0
	split := map[string]map[int64][]string{} // script, proposal: the modes that answer it
	for _, p := range ps {
		count[p.Mode]++
		if wordings[p.Mode] == nil {
			wordings[p.Mode] = map[string]bool{}
		}
		// A wording counts only where it answers one proposal alone, so the
		// answers to two proposals cannot make up for a list of wordings
		// that has shrunk.
		if !p.proposalPlantsTwo {
			wordings[p.Mode][p.answer.Text] = true
		}
		w := words(p.answer.Text)
		if p.Mode == ModeRefusal && len(w) == 1 {
			oneWord++
		}
		if p.Mode == ModeRefusal && len(w) > 1 && slices.Contains(assentWords, w[0]) {
			opensYes++
		}
		if p.proposalPlantsTwo {
			if split[p.script] == nil {
				split[p.script] = map[int64][]string{}
			}
			split[p.script][p.Proposal] = append(split[p.script][p.Proposal], p.Mode)
		}
		if p.Mode != ModeAssent && strings.Contains(letters(p.answer.Text), letters(p.fact.Slots[0].Value)) {
			t.Errorf("%s: the %s %q repeats %q", p.script, p.Mode, p.answer.Text, p.fact.Slots[0].Value)
		}
	}
	for _, want := range []struct {
		mode        string
		n, wordings int
	}{{ModeRefusal, 24, 8}, {ModeHedge, 24, 8}, {ModeAssent, 12, 5}} {
		if count[want.mode] < want.n || len(wordings[want.mode]) < want.wordings {
			t.Errorf("%d %ss in %d wordings, want at least %d in %d", count[want.mode], want.mode, len(wordings[want.mode]), want.n, want.wordings)
		}
	}
	if oneWord < 4 || opensYes < 4 {
		t.Errorf("%d one-word refusals and %d that open with assent, want at least 4 of each", oneWord, opensYes)
	}
	twos := 0
	for _, byProposal := range split {
		for _, ms := range byProposal {
			slices.Sort(ms)
			if slices.Equal(ms, []string{ModeAssent, ModeRefusal}) {
				twos++
			}
		}
	}
	if twos < 4 {
		t.Errorf("%d replies propose two things, one taken and one refused, want at least 4", twos)
	}
	var values []string
	for _, s := range family("assent") {
		answered := map[string]int{}
		for _, p := range s.Plants {
			answered[p.Fact]++
		}
		for _, sess := range s.Scenario.Sessions {
			for _, tr := range sess.Turns {
				for _, id := range tr.Plants {
					if tr.Author != event.AuthorAgent || answered[id] != 1 {
						t.Errorf("%s: fact %s is planted by %s and answered %d times", s.Scenario.ID, id, tr.Author, answered[id])
					}
				}
			}
		}
		for _, f := range s.Scenario.Facts {
			values = append(values, letters(f.Slots[0].Value))
		}
	}
	for i, v := range values {
		for j, w := range values {
			if i != j && strings.Contains(w, v) {
				t.Errorf("value %q is part of %q", v, w)
			}
		}
	}
}

// TestBoundsPlantsOverTheBound: bounds.01 plants more facts to store, in
// one session, than an answer may hold operations, each in a typed prompt
// of its own: no shown entry, and so no quote of an entry and no segment,
// holds the values of two facts, so an answer that keeps each fact in an
// operation of its own needs more than MaxOps of them, in either form.
func TestBoundsPlantsOverTheBound(t *testing.T) {
	s := family("bounds")
	if len(s) != 1 || len(s[0].Scenario.Sessions) != 1 {
		t.Fatalf("bounds: %d scripts", len(s))
	}
	sc := s[0].Scenario
	n := 0
	for _, f := range sc.Facts {
		if f.Must == scenario.MustStore {
			n++
		}
	}
	if n <= MaxOps {
		t.Errorf("bounds.01 plants %d facts to keep, want over %d", n, MaxOps)
	}
	if got := entriesHoldingTwo(t, sc); len(got) > 0 {
		t.Errorf("bounds.01: %v", got)
	}
	// The check finds a turn that states two settings.
	two := sc
	two.Sessions = slices.Clone(sc.Sessions)
	two.Sessions[0].Turns = slices.Clone(sc.Sessions[0].Turns)
	tr := &two.Sessions[0].Turns[2]
	tr.Text += " Also " + sc.Facts[1].Slots[0].Value + " is " + sc.Facts[1].Slots[1].Value + "."
	if got := entriesHoldingTwo(t, two); len(got) != 2 {
		t.Errorf("the check finds %v in a turn that states two settings", got)
	}
}

// entriesHoldingTwo lists, in each form, the shown entries of sc's first
// session that plant more than one fact or hold the key of more than one.
func entriesHoldingTwo(t *testing.T, sc scenario.Scenario) []string {
	t.Helper()
	var out []string
	sess := sc.Sessions[0]
	for _, f := range Forms {
		in, err := Render(sess, nil, f)
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range in.Entries {
			keys := 0
			for _, fact := range sc.Facts {
				if strings.Contains(e.Text, fact.Slots[0].Value) {
					keys++
				}
			}
			planted := 0
			if e.N <= int64(len(sess.Turns)) {
				planted = len(sess.Turns[e.N-1].Plants)
			}
			if keys > 1 || planted > 1 {
				out = append(out, fmt.Sprintf("%s: entry %d plants %d facts and holds %d keys", f, e.N, planted, keys))
			}
		}
	}
	return out
}

// TestEverySlotIsSaid: each slot of the new families' facts is said, as
// written, in the turn that plants it.
func TestEverySlotIsSaid(t *testing.T) {
	for _, s := range Scripts()[len(scenario.V1()):] {
		for _, sess := range s.Scenario.Sessions {
			for _, tr := range sess.Turns {
				for _, id := range tr.Plants {
					f := s.Scenario.Facts[slices.IndexFunc(s.Scenario.Facts, func(f scenario.Fact) bool { return f.ID == id })]
					for _, sl := range f.Slots {
						if !strings.Contains(tr.Text, sl.Value) {
							t.Errorf("%s: fact %s's %q is not said where it is planted", s.Scenario.ID, id, sl.Value)
						}
					}
				}
			}
		}
	}
}

// TestScriptsTextIsASCIIAndRedactionKeepsIt: every text of every script,
// its turns, names and slots, is ASCII, and redaction changes no turn.
func TestScriptsTextIsASCIIAndRedactionKeepsIt(t *testing.T) {
	ascii := func(s string) bool {
		return !strings.ContainsFunc(s, func(r rune) bool { return r > unicode.MaxASCII })
	}
	for _, s := range Scripts() {
		texts := []string{s.Scenario.Name}
		for _, f := range s.Scenario.Facts {
			for _, sl := range f.Slots {
				texts = append(texts, sl.Value)
				texts = append(texts, sl.Forms...)
			}
		}
		for _, sess := range s.Scenario.Sessions {
			for _, tr := range sess.Turns {
				texts = append(texts, tr.Text)
				if out, n := redact.Redact(tr.Text); out != tr.Text || n != 0 {
					t.Errorf("%s: redaction changes %q to %q", s.Scenario.ID, tr.Text, out)
				}
			}
		}
		for _, text := range texts {
			if !ascii(text) {
				t.Errorf("%s: %q is not ASCII", s.Scenario.ID, text)
			}
		}
	}
}

// writtenIDs are the ids the spike writes: its forms and codes, the tiers
// and efforts of an arm, the parts of a row's scenario, and the metric ids
// of the spike's cards and of the fidelity scorer. Script ids are added
// from Scripts.
func writtenIDs() []string {
	ids := []string{"spike", "small", "middle", "large", "minimal", "low", "medium", "high", "xhigh", "max", "on", "off", "all",
		"first", "repair", "refusal", "hedge", "assent"}
	for _, f := range Forms {
		ids = append(ids, string(f))
	}
	for _, c := range Codes {
		ids = append(ids, c.ID, "spike.refused_"+c.ID)
	}
	for _, h := range adapter.All() {
		ids = append(ids, string(h.ID))
	}
	for _, m := range []string{"spans_cited_first", "spans_cited_final", "spans_missed_first", "spans_missed_final", "ops_proposed",
		"ops_accepted_first", "ops_accepted_final", "answers_invalid", "schema_breaks", "nothing", "role_fixed", "repair_calls",
		"repair_sent", "repair_recovered", "repair_input_bytes", "repair_prefix_bytes", "plants_refusal", "plants_hedge",
		"plants_assent", "assent_cited_refusal", "assent_cited_hedge", "accepted_from_refusal", "accepted_from_hedge",
		"assents_captured", "quote_miss_first", "quote_miss_final", "repair_yield", "calls_first", "calls_repair",
		"calls_failed", "tokens_in", "tokens_out", "repair_tokens_in", "repair_tokens_cached_in", "seconds", "honest_refused"} {
		ids = append(ids, "spike."+m)
	}
	ids = append(ids, score.MetricFactRecall, score.MetricFactHolds, score.MetricWrongWrite, score.MetricNoise, score.MetricMisattributed,
		score.MetricStaleInForce, score.MetricDupes, score.MetricOTRStored, score.MetricViolations)
	for _, s := range Scripts() {
		ids = append(ids, s.Scenario.ID)
	}
	return ids
}

// inID reports the first id whose letters and digits hold a word's, as
// results.Forbidden would find it, or "".
func inID(word string, ids []string) string {
	for _, id := range ids {
		if k := letters(word); k != "" && strings.Contains(letters(id), k) {
			return id
		}
	}
	return ""
}

// TestNoPlantedValueIsInAnID: no word scenario.Forbid lists for a script
// occurs in an id the spike writes, so a row can always pass the run's
// Forbid.
func TestNoPlantedValueIsInAnID(t *testing.T) {
	ids := writtenIDs()
	for _, s := range Scripts() {
		for _, w := range scenario.Forbid(s.Scenario) {
			if id := inID(w, ids); id != "" {
				t.Errorf("%s: the planted %q is in the id %s", s.Scenario.ID, w, id)
			}
		}
	}
	if inID("Refusal", ids) == "" || inID("quote-s", ids) == "" {
		t.Error("the check misses a word in an id")
	}
}

func encodeScript(s Script) ([]byte, error) {
	data, err := json.MarshalIndent(s, "", "  ")
	return append(data, '\n'), err
}

// TestScriptsGolden: each script is committed as testdata/scripts/<id>.json,
// which regenerating gives byte for byte, and which decodes strictly to a
// valid script that encodes back to it. No other file is there.
func TestScriptsGolden(t *testing.T) {
	dir := filepath.Join("testdata", "scripts")
	scripts := Scripts()
	for _, s := range scripts {
		data, err := encodeScript(s)
		if err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(dir, s.Scenario.ID+".json")
		if *regen {
			if err := os.WriteFile(path, data, 0o644); err != nil {
				t.Fatal(err)
			}
		}
		golden, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(data, golden) {
			t.Errorf("%s differs from %s; run the test with -regen after checking the change", s.Scenario.ID, path)
		}
		var back Script
		d := json.NewDecoder(bytes.NewReader(golden))
		d.DisallowUnknownFields()
		if err := d.Decode(&back); err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		if _, err := d.Token(); err != io.EOF {
			t.Errorf("%s: data after the script", path)
		}
		if err := back.Validate(); err != nil {
			t.Errorf("%s: %v", path, err)
		}
		if again, err := encodeScript(back); err != nil || !bytes.Equal(again, golden) {
			t.Errorf("%s does not encode back to itself (%v)", path, err)
		}
	}
	files, err := filepath.Glob(filepath.Join(dir, "*"))
	if err != nil || len(files) != len(scripts) {
		t.Errorf("%d files in %s, want %d (%v)", len(files), dir, len(scripts), err)
	}
}

// TestValidateRefusesABadPlant: each way a plant can be wrong is refused.
func TestValidateRefusesABadPlant(t *testing.T) {
	base := family("assent")[1]
	if err := base.Validate(); err != nil {
		t.Fatal(err)
	}
	find := func(s Script, mode string) int {
		return slices.IndexFunc(s.Plants, func(p Plant) bool { return p.Mode == mode })
	}
	clone := func() Script {
		data, err := json.Marshal(base)
		if err != nil {
			t.Fatal(err)
		}
		var s Script
		if err := json.Unmarshal(data, &s); err != nil {
			t.Fatal(err)
		}
		return s
	}
	for _, c := range []struct {
		name   string
		mutate func(s *Script)
		want   string
	}{
		{"unknown mode", func(s *Script) { s.Plants[0].Mode = "silence" }, "unknown mode"},
		{"unknown session", func(s *Script) { s.Plants[0].Session = "nobody" }, "no such session"},
		{"answer before proposal", func(s *Script) { s.Plants[0].Seq = s.Plants[0].Proposal }, "after the proposal"},
		{"answer past the session", func(s *Script) { s.Plants[0].Seq = 99 }, "after the proposal"},
		{"proposal not the agent's", func(s *Script) { s.Plants[0].Proposal--; s.Plants[0].Seq-- }, "not the agent's"},
		{"answer not typed", func(s *Script) { s.Scenario.Sessions[0].Turns[s.Plants[0].Seq-1].Author = event.AuthorPasted }, "not a typed prompt"},
		{"a typed prompt between", func(s *Script) { s.Plants[0].Seq += 2 }, "not the first typed prompt"},
		{"fact not proposed there", func(s *Script) { s.Plants[0].Fact = s.Plants[1].Fact }, "plants no fact"},
		{"fact answered twice", func(s *Script) { s.Plants = append(s.Plants, s.Plants[0]) }, "answered twice"},
		{"assent with another's authority", func(s *Script) {
			p := s.Plants[find(*s, ModeAssent)]
			for i := range s.Scenario.Facts {
				if s.Scenario.Facts[i].ID == p.Fact {
					s.Scenario.Facts[i].Authority, s.Scenario.Facts[i].Must = scenario.AuthorityOther, scenario.MustNotWithAuthority
				}
			}
		}, "stored under the user's authority"},
		{"assent as a rule", func(s *Script) {
			p := s.Plants[find(*s, ModeAssent)]
			for i := range s.Scenario.Facts {
				if s.Scenario.Facts[i].ID == p.Fact {
					s.Scenario.Facts[i].Kinds = append(s.Scenario.Facts[i].Kinds, scenario.KindRule)
				}
			}
		}, "stored under the user's authority"},
		{"refused fact to store", func(s *Script) {
			p := s.Plants[find(*s, ModeRefusal)]
			for i := range s.Scenario.Facts {
				if s.Scenario.Facts[i].ID == p.Fact {
					s.Scenario.Facts[i].Authority, s.Scenario.Facts[i].Must = scenario.AuthorityUser, scenario.MustStore
				}
			}
		}, "must not carry"},
		{"refusal repeats its value", func(s *Script) {
			p := s.Plants[find(*s, ModeHedge)]
			f := s.Scenario.Facts[slices.IndexFunc(s.Scenario.Facts, func(f scenario.Fact) bool { return f.ID == p.Fact })]
			tr := &s.Scenario.Sessions[0].Turns[p.Seq-1]
			tr.Text = "maybe " + strings.ToUpper(f.Slots[0].Value)
		}, "repeats the value"},
	} {
		s := clone()
		c.mutate(&s)
		if err := s.Validate(); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: Validate gives %v, want %q", c.name, err, c.want)
		}
	}
}
