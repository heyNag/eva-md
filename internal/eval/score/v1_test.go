package score_test

import (
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/heyNag/eva-md/internal/eval/scenario"
	"github.com/heyNag/eva-md/internal/eval/score"
	"github.com/heyNag/eva-md/internal/eval/v1page"
	"github.com/heyNag/eva-md/internal/ledger/event"
	"github.com/heyNag/eva-md/internal/threadpage"
	"github.com/heyNag/eva-md/internal/update/accuracy"
)

// ref is v1's reference to line seq of session i of sc.
func ref(sc scenario.Scenario, i, seq int) string {
	return "s:" + event.Prefix(sc.Sessions[i].ID) + "#" + strconv.Itoa(seq)
}

func item(text, tag string, refs ...string) threadpage.Item {
	return threadpage.Item{Text: text, Tag: tag, Refs: refs}
}

// line is a Timeline line as v1 writes it.
func line(word, tag, text string, refs ...string) string {
	return strings.Join(append([]string{"- " + word + " [" + tag + "]: " + text}, refs...), " ")
}

// idealV1 is the page a perfect v1 update writes for each ported
// scenario, in v1's own words: the page v1's canned correct outputs give,
// as internal/update's accuracy test applies them, but where a comment says
// otherwise. Each holds every value the port asks for.
func idealV1(sc scenario.Scenario) threadpage.Page {
	r := func(i, seq int) string { return ref(sc, i, seq) }
	switch sc.ID {
	case "v1.01":
		return threadpage.Page{
			Now: threadpage.Now{Summary: "The lock uses flock; a pid file is proposed.",
				Open:      []threadpage.Item{item("Switch the lock to a pid file instead, PIDGEON-7?", "suggested", r(0, 2))},
				Decisions: []threadpage.Item{item("Use flock for the per-session lock, KESTREL-41", "stated", r(0, 1))}},
			Timeline: []threadpage.Group{{Date: "2026-01-13", Lines: []string{line("decision", "stated", "Use flock for the per-session lock, KESTREL-41", r(0, 1))}}},
		}
	case "v1.02":
		// v1's canned line drops "yesterday", which the port asks for.
		return threadpage.Page{
			Now:      threadpage.Now{Decisions: []threadpage.Item{item("Freeze releases on 2026-03-03, FROST-9", "stated", r(0, 1))}},
			Timeline: []threadpage.Group{{Date: "2026-01-13", Lines: []string{line("change", "stated", "Cut release candidate HERON-3 yesterday", r(0, 1))}}},
		}
	case "v1.03":
		return threadpage.Page{
			Now: threadpage.Now{Decisions: []threadpage.Item{item("Store pages as JSONL, OSPREY-J", "stated", r(1, 1))}},
			Timeline: []threadpage.Group{{Date: "2026-01-14", Lines: []string{
				line("decision", "stated", "~~Store pages as YAML, OSPREY-Y~~ → Store pages as JSONL, OSPREY-J (superseded 2026-01-14)", r(0, 1), r(1, 1), "<!-- i:n1 -->")}}},
		}
	case "v1.04":
		return threadpage.Page{
			Now: threadpage.Now{Decisions: []threadpage.Item{item("The token variable is EVA_MD_TOKEN, WREN-5", "stated", r(1, 1))}},
			Timeline: []threadpage.Group{{Date: "2026-01-13", Lines: []string{
				line("decision", "stated", "~~The token variable is EVA_TOKEN, WREN-2~~ → The token variable is EVA_MD_TOKEN, WREN-5 (superseded 2026-01-13)", r(0, 1), r(1, 1), "<!-- i:n1 -->")}}},
		}
	case "v1.05":
		// v1's canned item drops BenchmarkBrief from the condition.
		return threadpage.Page{Now: threadpage.Now{Decisions: []threadpage.Item{
			item("If BenchmarkBrief in scripts/benchmark.py passes 300 ms, switch to the streaming parser, CONDOR-8", "stated", r(0, 1))}}}
	case "v1.06":
		return threadpage.Page{Now: threadpage.Now{Decisions: []threadpage.Item{item("Keep SQLite for now, AVOCET-1", "stated", r(0, 2))}}}
	case "v1.07":
		return threadpage.Page{Timeline: []threadpage.Group{{Date: "2026-01-13", Lines: []string{
			line("change", "observed", "Rotated the signing keys as scheduled, LORIKEET-4", r(0, 4), r(0, 5))}}}}
	case "v1.08":
		// v1's canned page answers MARTIN-3 only on the Timeline, and logs
		// the tool's and the assistant's word that the parser is finished
		// only in MARTIN-1's done line, under the user's tag; this one adds
		// the answer as a decision and a finding for each, in their voice.
		return threadpage.Page{
			Now: threadpage.Now{
				Done:      []threadpage.Item{item("Finished the parser", "observed", r(1, 1))},
				Next:      []threadpage.Item{item("Write the renderer, MARTIN-2", "stated", r(0, 1))},
				Decisions: []threadpage.Item{item("Use Go 1.26, MARTIN-4", "stated", r(0, 1)), item("Pages hold hidden ids", "stated", r(1, 3))},
			},
			Timeline: []threadpage.Group{{Date: "2026-01-14", Lines: []string{
				line("done", "stated", "Write the parser, MARTIN-1", r(0, 1), r(1, 1), r(1, 2), "<!-- i:n1 -->"),
				line("answered", "stated", "Should pages hold hidden ids, MARTIN-3? → Pages hold hidden ids", r(0, 1), r(1, 3), "<!-- i:n3 -->"),
				line("finding", "observed", "The parser's tests pass", r(1, 1)),
				line("finding", "observed", "The parser is finished", r(1, 2)),
			}}},
		}
	case "v1.09":
		return threadpage.Page{
			Now: threadpage.Now{Decisions: []threadpage.Item{item("Rotate logs weekly, DOTTEREL-W", "accepted", r(1, 1))}},
			Timeline: []threadpage.Group{{Date: "2026-01-14", Lines: []string{
				line("question", "suggested", "~~Rotate logs daily, DOTTEREL-D?~~ (dropped: the user chose weekly rotation)", r(0, 2), r(1, 1), "<!-- i:n1 -->"),
				line("answered", "suggested", "Rotate logs weekly, DOTTEREL-W? → Rotate logs weekly, DOTTEREL-W [accepted]", r(0, 2), r(1, 1), "<!-- i:n2 -->"),
			}}},
		}
	case "v1.10":
		return threadpage.Page{
			Now: threadpage.Now{Decisions: []threadpage.Item{item("Nix flakes handle everything, TERN-7", "accepted", r(2, 1), r(2, 2))}},
			Timeline: []threadpage.Group{{Date: "2026-01-15", Lines: []string{
				line("decision", "stated", "~~Ansible playbooks own provisioning, GANNET-2~~ → Nix flakes handle everything, TERN-7 [accepted] (superseded 2026-01-15)", r(0, 1), r(2, 1), r(2, 2), "<!-- i:n1 -->"),
				line("answered", "stated", `eva-md: replace question: does "Nix flakes handle everything, TERN-7" replace "Ansible playbooks own provisioning, GANNET-2"? → yes [inferred]`, r(1, 1), r(2, 1), r(2, 2), "<!-- i:n3 -->"),
				line("decision", "stated", "~~Nix flakes handle everything, TERN-7~~ (dropped: it now replaces the Ansible decision)", r(1, 1), r(2, 2), "<!-- i:n2 -->"),
			}}},
		}
	case "v1.11":
		// v1's canned page loses KITE-WARM, which the out-of-order guard
		// keeps from the Now; this one logs it under its day.
		return threadpage.Page{
			Now: threadpage.Now{
				Next:      []threadpage.Item{item("Log cache misses, KITE-LOG", "stated", r(0, 2))},
				Decisions: []threadpage.Item{item("Cache entries for 5 minutes, KITE-5", "stated", r(1, 1))},
			},
			Timeline: []threadpage.Group{{Date: "2026-01-12", Lines: []string{
				line("finding", "observed", "A cold cache costs 3 s at startup, KITE-COLD", r(2, 2), r(2, 3)),
				line("next", "stated", "Warm the cache at startup, KITE-WARM", r(2, 1)),
			}}},
		}
	case "v1.12":
		return threadpage.Page{Now: threadpage.Now{
			Open:      []threadpage.Item{item("Add a watchdog that restarts stuck update jobs, EGRET-5?", "suggested", r(1, 2))},
			Decisions: []threadpage.Item{item("Use flock for the session lock in internal/session/lock.go, SKUA-0", "stated", r(0, 1))},
		}}
	}
	panic("no ideal page for " + sc.ID)
}

// measure is v1's Measure of page p on script s, after every chat.
func measure(s *accuracy.Script, p threadpage.Page) accuracy.Metrics {
	seen := map[int]bool{}
	for i := range s.Turns {
		seen[i] = true
	}
	return s.Measure(p, 0, seen, time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC))
}

// read renders p and reads it back as v1page reads a saved page.
func read(t *testing.T, p threadpage.Page) score.Export {
	t.Helper()
	ex, err := v1page.ReadThread("---\ntype: thread\n---\n" + threadpage.Render(p))
	if err != nil {
		t.Fatal(err)
	}
	return ex
}

// clean reports what keeps a result from a perfect one: a fact that must
// be stored that does not hold, or any count above 0.
func clean(sc scenario.Scenario, r score.Result) []string {
	var out []string
	for i, fr := range r.Facts {
		if fr.Outcome != score.Holds {
			out = append(out, fmt.Sprintf("%s (%s) is %s %v", fr.Fact, sc.Facts[i].Slots[0].Value, fr.Outcome, fr.Problems))
		}
	}
	m := r.Metrics
	if m.FactHolds != 1 || m.FactRecall != 1 || m.WrongWrite+m.Noise+m.Misattributed+m.StaleInForce+m.Dupes+m.OTRStored+m.Violations != 0 {
		out = append(out, fmt.Sprintf("metrics %+v", m))
	}
	for i, ir := range r.Items {
		if ir.Outcome != score.Supports && ir.Outcome != score.Unscored {
			out = append(out, fmt.Sprintf("item %d is %s", i, ir.Outcome))
		}
	}
	return out
}

// TestV1IdealPagesScorePerfectly: for each of v1's 12 ported scripts, the
// page a perfect v1 update writes, rendered and read back with v1page,
// holds every fact with every count at 0, and v1's Measure finds the same
// page perfect.
func TestV1IdealPagesScorePerfectly(t *testing.T) {
	ported, scripts := scenario.V1(), accuracy.Scripts()
	if len(ported) != 12 || len(scripts) != 12 {
		t.Fatalf("%d ported scenarios and %d scripts", len(ported), len(scripts))
	}
	for i, sc := range ported {
		p := idealV1(sc)
		if m := measure(scripts[i], p); !m.Perfect() || len(m.Misses) != 0 {
			t.Errorf("%s: v1's Measure: %+v", sc.ID, m)
		}
		r := score.Score(sc, read(t, p))
		if bad := clean(sc, r); len(bad) != 0 {
			t.Errorf("%s: %s", sc.ID, strings.Join(bad, "; "))
		}
	}
}

// The canned page v1's accuracy test gives for item accounting across two
// passes, as threadpage.Render writes it: it answers MARTIN-3 only on the
// Timeline, and its one dated line that holds the parser's two results is
// MARTIN-1's done line, under the user's tag.
func TestV1CannedAccountingPage(t *testing.T) {
	sc := scenario.V1()[7]
	p := idealV1(sc)
	p.Now.Decisions = p.Now.Decisions[:1]
	p.Timeline[0].Lines = p.Timeline[0].Lines[:2]
	if m := measure(accuracy.Scripts()[7], p); !m.Perfect() {
		t.Fatalf("v1's Measure: %+v", m)
	}
	res := score.Score(sc, read(t, p))
	want := map[string]string{"f5": "found [authority]", "f6": "found [authority]", "f7": "found [kind]"}
	for _, fr := range res.Facts {
		got := fmt.Sprintf("%s %v", fr.Outcome, fr.Problems)
		if w, ok := want[fr.Fact]; ok && got != w || !ok && fr.Outcome != score.Holds {
			t.Errorf("%s is %s", fr.Fact, got)
		}
	}
	if m := res.Metrics; m.FactRecall != 1 || m.FactHolds != 0.4 || m.WrongWrite+m.Noise+m.Misattributed+m.Dupes+m.StaleInForce != 0 {
		t.Errorf("metrics %+v", m)
	}
}

// TestV1Paraphrases: on v1's port, what was said off the record, pasted or
// replaced, stored in other words without its codename, is caught as what
// it is, not as an ordinary wrong write.
func TestV1Paraphrases(t *testing.T) {
	ported := scenario.V1()
	for _, tc := range []struct {
		scenario int
		spoil    func(sc scenario.Scenario, p *threadpage.Page)
		check    func(score.Metrics) bool
	}{
		{0, func(sc scenario.Scenario, p *threadpage.Page) {
			p.Now.Decisions = append(p.Now.Decisions, item("The budget is ninety-nine thousand", "stated", ref(sc, 0, 4)))
		}, func(m score.Metrics) bool { return m.OTRStored == 1 && m.Violations == 1 && m.WrongWrite == 0 }},
		{5, func(sc scenario.Scenario, p *threadpage.Page) {
			p.Now.Decisions = append(p.Now.Decisions, item("We will use Postgres", "stated", ref(sc, 0, 1)))
		}, func(m score.Metrics) bool { return m.Violations == 1 && m.WrongWrite == 0 }},
		{3, func(sc scenario.Scenario, p *threadpage.Page) {
			p.Now.Decisions = append(p.Now.Decisions, item("The token variable is EVA_TOKEN", "stated", ref(sc, 0, 1)))
		}, func(m score.Metrics) bool { return m.StaleInForce == 1 && m.WrongWrite == 0 }},
	} {
		sc := ported[tc.scenario]
		p := idealV1(sc)
		tc.spoil(sc, &p)
		if m := score.Score(sc, read(t, p)).Metrics; !tc.check(m) {
			t.Errorf("%s: metrics %+v", sc.ID, m)
		}
	}
}

// TestV1History: on v1's port, an item in force that names the value its
// fact replaced, or the pasted claim it turns down, and cites that line
// too, supports what it states and keeps nothing in force; an item in
// force that holds a value of a fact that ended, and no fact that ended it,
// keeps it in force, even when a fact its lines do not plant holds the
// same value, or the item holds another fact of the same line in full.
func TestV1History(t *testing.T) {
	ported := scenario.V1()
	byID := func(id string) scenario.Scenario {
		return ported[slices.IndexFunc(ported, func(sc scenario.Scenario) bool { return sc.ID == id })]
	}
	decision := func(text, tag string, refs ...[2]int) func(scenario.Scenario, *threadpage.Page) {
		return func(sc scenario.Scenario, p *threadpage.Page) {
			var rs []string
			for _, r := range refs {
				rs = append(rs, ref(sc, r[0], r[1]))
			}
			p.Now.Decisions[0] = item(text, tag, rs...)
		}
	}
	for _, tc := range []struct {
		name, scenario string
		spoil          func(scenario.Scenario, *threadpage.Page)
		stale          string // the fact kept in force, or "" for a clean page
	}{
		{"a correction that names the old value", "v1.04", decision("The token variable is EVA_MD_TOKEN, WREN-5, corrected from EVA_TOKEN", "stated", [2]int{0, 1}, [2]int{1, 1}), ""},
		{"a correction that names the old codename", "v1.04", decision("The token variable is EVA_MD_TOKEN, WREN-5, replacing WREN-2", "stated", [2]int{0, 1}, [2]int{1, 1}), ""},
		{"a reversal that names the old value", "v1.03", decision("Store pages as JSONL after all, OSPREY-J, not YAML", "stated", [2]int{0, 1}, [2]int{1, 1}), ""},
		{"a new quantity that names the old one", "v1.11", decision("Cache entries for 5 minutes, KITE-5, up from 1 minute", "stated", [2]int{0, 1}, [2]int{1, 1}), ""},
		{"a replacement that names what it replaces", "v1.10", decision("Nix flakes replace the Ansible playbooks, TERN-7", "accepted", [2]int{0, 1}, [2]int{2, 2}), ""},
		{"an acceptance that names the choice turned down", "v1.09", decision("Rotate logs weekly, DOTTEREL-W, not daily", "accepted", [2]int{0, 2}, [2]int{1, 1}), ""},
		{"a decision that names the pasted claim it turns down", "v1.06", decision("Keep SQLite for now, AVOCET-1, not Postgres", "stated", [2]int{0, 1}, [2]int{0, 2}), ""},
		{"a next step kept after it was done", "v1.08", func(sc scenario.Scenario, p *threadpage.Page) {
			p.Now.Next = append(p.Now.Next, item("Write the parser", "stated", ref(sc, 0, 1)))
		}, "MARTIN-1"},
		{"a question kept open after its answer", "v1.08", func(sc scenario.Scenario, p *threadpage.Page) {
			p.Now.Open = append(p.Now.Open, item("Should pages hold hidden ids?", "stated", ref(sc, 0, 1)))
		}, "MARTIN-3"},
		{"a next step kept beside a fact of its line that did not end it", "v1.08", func(sc scenario.Scenario, p *threadpage.Page) {
			p.Now.Next = append(p.Now.Next, item("Write the parser; we use Go 1.26, MARTIN-4", "stated", ref(sc, 0, 1)))
		}, "MARTIN-1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sc := byID(tc.scenario)
			p := idealV1(sc)
			tc.spoil(sc, &p)
			r := score.Score(sc, read(t, p))
			if tc.stale == "" {
				if bad := clean(sc, r); len(bad) != 0 {
					t.Errorf("%s", strings.Join(bad, "; "))
				}
				return
			}
			var outcomes []score.Outcome
			for _, ir := range r.Items {
				if ir.Outcome != score.Supports && ir.Outcome != score.Unscored {
					outcomes = append(outcomes, ir.Outcome)
				}
			}
			if !slices.Equal(outcomes, []score.Outcome{score.StaleInForce}) {
				t.Errorf("item outcomes %v, want the one item kept in force", outcomes)
			}
			for i, fr := range r.Facts {
				if want := sc.Facts[i].Slots[0].Value == tc.stale; (fr.Outcome == score.StaleInForce) != want {
					t.Errorf("%s is %s", sc.Facts[i].Slots[0].Value, fr.Outcome)
				}
			}
			if m := r.Metrics; m.StaleInForce != 1 || m.WrongWrite != 0 || m.Violations != 0 || m.FactHolds != 1 {
				t.Errorf("metrics %+v", m)
			}
		})
	}
}

// TestV1Condition: CONDOR-8's condition holds with its quantity in any
// form, and is dropped when its words stand outside the clause a marker
// opens.
func TestV1Condition(t *testing.T) {
	sc := scenario.V1()[4]
	for text, want := range map[string]score.Outcome{
		"If BenchmarkBrief in scripts/benchmark.py passes 300ms, switch to the streaming parser, CONDOR-8":                   score.Holds,
		"If BenchmarkBrief in scripts/benchmark.py passes 0.3 s, switch to the streaming parser, CONDOR-8":                   score.Holds,
		"If the build fails, switch to the streaming parser; BenchmarkBrief in scripts/benchmark.py passes 300 ms, CONDOR-8": score.CondDropped,
		"If BenchmarkBrief, in scripts/benchmark.py, passes 300 ms, switch to the streaming parser, CONDOR-8":                score.Holds,
		"If BenchmarkBrief in scripts/benchmark.py passes a 300-ms limit, switch to the streaming parser, CONDOR-8":          score.Holds,
		"Unless BenchmarkBrief in scripts/benchmark.py passes 300 ms, switch to the streaming parser, CONDOR-8":              score.CondDropped,
		"If BenchmarkBrief in scripts/benchmark.py passes, switch to the streaming parser within 300 ms, CONDOR-8":           score.CondDropped,
		"If BenchmarkBrief passes 300 ms, switch to the streaming parser in scripts/benchmark.py, CONDOR-8":                  score.CondDropped,
	} {
		p := threadpage.Page{Now: threadpage.Now{Decisions: []threadpage.Item{item(text, "stated", ref(sc, 0, 1))}}}
		r := score.Score(sc, read(t, p))
		if r.Facts[0].Outcome != want {
			t.Errorf("%q: CONDOR-8 is %s, want %s", text, r.Facts[0].Outcome, want)
		}
		if bad := clean(sc, r); want == score.Holds && len(bad) != 0 || want != score.Holds && r.Metrics.WrongWrite != 1 {
			t.Errorf("%q: %v, metrics %+v", text, bad, r.Metrics)
		}
	}
}

// TestV1Forms: a value written in another form the normalizers read is
// held; a day written with slashes is not read, by choice.
func TestV1Forms(t *testing.T) {
	ported := scenario.V1()
	for _, tc := range []struct {
		scenario int
		fact     string
		text     string
		want     score.Outcome
	}{
		{10, "KITE-5", "Cache entries with a 5-minute TTL, KITE-5", score.Holds},
		{10, "KITE-5", "Cache entries for 300 seconds, KITE-5", score.Holds},
		{1, "FROST-9", "Freeze releases on March 3, 2026, FROST-9", score.Holds},
		{1, "FROST-9", "Freeze releases on 3/3/2026, FROST-9", score.DateDropped},
	} {
		sc := ported[tc.scenario]
		p := idealV1(sc)
		p.Now.Decisions[0].Text = tc.text
		r := score.Score(sc, read(t, p))
		i := slices.IndexFunc(sc.Facts, func(f scenario.Fact) bool { return f.Slots[0].Value == tc.fact })
		if r.Facts[i].Outcome != tc.want {
			t.Errorf("%s %q: %s is %s, want %s", sc.ID, tc.text, tc.fact, r.Facts[i].Outcome, tc.want)
		}
		if bad := clean(sc, r); tc.want == score.Holds && len(bad) != 0 || tc.want != score.Holds && r.Metrics.WrongWrite != 1 {
			t.Errorf("%s %q: %v, metrics %+v", sc.ID, tc.text, bad, r.Metrics)
		}
	}
}

// TestPlanGate is the plan's gate for EV-14, on v1's port: a page with a
// wrong value, the correction's or the cache's, one with FROST-9's date
// dropped, and one with an item added, citing nothing and again citing the
// filler, each still perfect to v1's Measure, and each counted once by the
// scorer as a wrong write or noise, with its outcome named.
func TestPlanGate(t *testing.T) {
	ported, scripts := scenario.V1(), accuracy.Scripts()
	byID := func(id string) (scenario.Scenario, *accuracy.Script) {
		i := slices.IndexFunc(ported, func(sc scenario.Scenario) bool { return sc.ID == id })
		return ported[i], scripts[i]
	}
	added := func(sc scenario.Scenario, refs ...string) func(*threadpage.Page) {
		return func(p *threadpage.Page) {
			p.Now.Next = append(p.Now.Next, item("Move the build cache to a shared remote store", "stated", refs...))
		}
	}
	sc12, _ := byID("v1.12")
	filler := ref(sc12, 1, len(sc12.Sessions[1].Turns)+1)
	for _, tc := range []struct {
		name, scenario string
		spoil          func(*threadpage.Page)
		count          func(score.Metrics) int
		item           score.Outcome
		fact           string // the fact the spoiling hits, if any
	}{
		{"the correction's value wrong", "v1.04", func(p *threadpage.Page) { p.Now.Decisions[0].Text = "The token variable is EVA_TOKEN, WREN-5" },
			func(m score.Metrics) int { return m.WrongWrite }, score.ValueWrong, "WREN-5"},
		{"the cache's value wrong", "v1.11", func(p *threadpage.Page) { p.Now.Decisions[0].Text = "Cache entries for 7 minutes, KITE-5" },
			func(m score.Metrics) int { return m.WrongWrite }, score.ValueWrong, "KITE-5"},
		{"FROST-9's date dropped", "v1.02", func(p *threadpage.Page) { p.Now.Decisions[0].Text = "Freeze releases, FROST-9" },
			func(m score.Metrics) int { return m.WrongWrite }, score.DateDropped, "FROST-9"},
		{"an item citing nothing", "v1.12", added(sc12),
			func(m score.Metrics) int { return m.WrongWrite }, score.Unsupported, ""},
		{"an item citing the filler", "v1.12", added(sc12, filler),
			func(m score.Metrics) int { return m.Noise }, score.Noise, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sc, s := byID(tc.scenario)
			p := idealV1(sc)
			tc.spoil(&p)
			if m := measure(s, p); !m.Perfect() {
				t.Errorf("v1's Measure catches it: %+v", m)
			}
			res := score.Score(sc, read(t, p))
			m := res.Metrics
			if total := m.WrongWrite + m.Noise + m.Misattributed + m.StaleInForce + m.Dupes + m.OTRStored + m.Violations; tc.count(m) != 1 || total != 1 {
				t.Errorf("metrics %+v, want one count", m)
			}
			var outcomes []score.Outcome
			for _, ir := range res.Items {
				if ir.Outcome != score.Supports && ir.Outcome != score.Unscored {
					outcomes = append(outcomes, ir.Outcome)
				}
			}
			if !slices.Equal(outcomes, []score.Outcome{tc.item}) {
				t.Errorf("item outcomes %v, want %v", outcomes, tc.item)
			}
			for i, fr := range res.Facts {
				want := score.Holds
				if tc.fact != "" && sc.Facts[i].Slots[0].Value == tc.fact {
					want = tc.item
				}
				if fr.Outcome != want {
					t.Errorf("%s is %s, want %s", sc.Facts[i].Slots[0].Value, fr.Outcome, want)
				}
			}
		})
	}
}

// toPage files an export back on a v1 page, as v1's Measure reads one:
// each kind where v1 keeps it, a dated line under its day, and a kind v1
// has no place for left out.
func toPage(ex score.Export) threadpage.Page {
	var p threadpage.Page
	for _, it := range ex.Items {
		var refs []string
		for _, s := range it.Sources {
			refs = append(refs, "s:"+s.Session+"#"+strconv.FormatInt(s.Seq, 10))
		}
		ti := item(it.Text, it.Authority, refs...)
		switch {
		case it.Kind == score.KindSummary:
			p.Now.Summary = it.Text
		case it.State == score.StateLog:
			l := line("change", it.Authority, it.Text, refs...)
			if n := len(p.Timeline); n > 0 && p.Timeline[n-1].Date == it.Date {
				p.Timeline[n-1].Lines = append(p.Timeline[n-1].Lines, l)
			} else {
				p.Timeline = append(p.Timeline, threadpage.Group{Date: it.Date, Lines: []string{l}})
			}
		case it.State == score.StateRetired && it.Kind == scenario.KindNext:
			p.Now.Done = append(p.Now.Done, ti)
		case it.State != score.StateActive:
		case it.Kind == scenario.KindDecision:
			p.Now.Decisions = append(p.Now.Decisions, ti)
		case it.Kind == scenario.KindNext:
			p.Now.Next = append(p.Now.Next, ti)
		case it.Kind == scenario.KindOpen:
			p.Now.Open = append(p.Now.Open, ti)
		}
	}
	return p
}

// TestSeededSetAgainstV1 spoils each ported scenario's ideal v1 page each
// way of the seeded set, and asks v1's Measure and the scorer whether they
// score it right: a spoiled page as not perfect, and a moved item, still
// right, as perfect. A way is scored right when it is on every scenario
// it applies to. v1's Measure scores three of the seven right; the scorer
// all seven.
func TestSeededSetAgainstV1(t *testing.T) {
	ported, scripts := scenario.V1(), accuracy.Scripts()
	v1Right, scorerRight := 0, 0
	for _, d := range score.Distortions {
		applied, v1OK, scorerOK := 0, true, true
		for i, sc := range ported {
			ex := read(t, idealV1(sc))
			spoiled, err := score.Distort(ex, sc, d)
			if errors.Is(err, score.ErrNotApplicable) {
				continue
			}
			if err != nil {
				t.Fatal(err)
			}
			applied++
			if measure(scripts[i], toPage(spoiled)).Perfect() != (d == score.DistortMoved) {
				v1OK = false
			}
			if err := seededRight(sc, ex, spoiled, d); err != nil {
				scorerOK = false
				t.Errorf("%s, %s: %v", sc.ID, d, err)
			}
		}
		if applied == 0 {
			t.Errorf("%s applies to no ported scenario", d)
		}
		if v1OK {
			v1Right++
		}
		if scorerOK {
			scorerRight++
		}
		t.Logf("%s: applies to %d ported scenarios; v1's Measure right on all: %v; the scorer: %v", d, applied, v1OK, scorerOK)
	}
	t.Logf("of the %d seeded distortions, v1's Measure scores %d right and the scorer %d", len(score.Distortions), v1Right, scorerRight)
	if v1Right != 3 || scorerRight != len(score.Distortions) {
		t.Errorf("v1's Measure scores %d right, want 3; the scorer %d, want %d", v1Right, scorerRight, len(score.Distortions))
	}
}
