package accuracy

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/heyNag/eva-md/internal/threadpage"
)

func isolate(t *testing.T) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(home, "gitconfig"))
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
}

// The suite has twelve histories, under distinct names, of 21 passes in
// all, each chat passed once; and each chat renders as one session's
// entries, the host's, ending with the filler exchange.
func TestScripts(t *testing.T) {
	isolate(t)
	scripts := Scripts()
	names, passes := map[string]bool{}, 0
	for _, s := range scripts {
		names[s.Name] = true
		var chats []int
		for _, p := range s.Passes {
			chats = append(chats, p.Chat)
		}
		slices.Sort(chats)
		if !slices.Equal(chats, []int{0, 1, 2}[:len(s.Turns)]) {
			t.Errorf("%s passes chats %v of %d", s.Name, chats, len(s.Turns))
		}
		passes += len(s.Passes)
		for i := range s.Turns {
			c := s.Chat(i, "codex")
			last := c.Rows[len(c.Rows)-1]
			if len(c.Rows) != len(s.Turns[i])+2 || last.Host != "codex" || last.Session != c.Session || !strings.HasPrefix(last.Text, "Glad to help. ") {
				t.Errorf("%s chat %d: %+v", s.Name, i, c)
			}
		}
	}
	if len(scripts) != 12 || len(names) != 12 || passes != 21 {
		t.Fatalf("%d scripts, %d names, %d passes", len(scripts), len(names), passes)
	}
}

func item(id, text, tag string) threadpage.Item { return threadpage.Item{ID: id, Text: text, Tag: tag} }

// Each miss a live run counts, by kind, beside the measures the offline
// suite reads: a fact missing, in two Now items, still in force after it
// stopped holding, under another tag, asserted though pasted or said off
// the record, or without its condition; a Timeline fact under another
// date, without its tag, or not logged. A page that gets everything right
// has no miss.
func TestMeasureMisses(t *testing.T) {
	isolate(t)
	both, first := map[int]bool{0: true, 1: true}, map[int]bool{0: true}
	reversal := Reversal()
	for _, tc := range []struct {
		name  string
		s     *Script
		page  threadpage.Page
		seen  map[int]bool
		asOf  time.Time
		want  []Miss
		check func(m Metrics) bool
	}{{
		name: "a reversal done right", s: reversal, seen: both, asOf: On(1, 10, 1),
		page:  threadpage.Page{Now: threadpage.Now{Decisions: []threadpage.Item{item("n1", "Store pages as JSONL, OSPREY-J", "stated")}}},
		check: func(m Metrics) bool { return m.Perfect() },
	}, {
		name: "a reversal kept beside the old value", s: reversal, seen: both, asOf: On(1, 10, 1),
		page:  threadpage.Page{Now: threadpage.Now{Decisions: []threadpage.Item{item("n1", "Store pages as YAML, OSPREY-Y", "stated"), item("n2", "Store pages as JSONL, OSPREY-J", "stated")}}},
		want:  []Miss{{KindContent, 0, "OSPREY-Y", "OSPREY-Y is still in force"}},
		check: func(m Metrics) bool { return m.Stale == 1 && m.Updates == 0 },
	}, {
		name: "a reversal under another tag, twice", s: reversal, seen: both, asOf: On(1, 10, 1),
		page:  threadpage.Page{Now: threadpage.Now{Decisions: []threadpage.Item{item("n1", "Store pages as JSONL, OSPREY-J", "inferred"), item("n2", "JSONL, osprey-j", "stated")}}},
		want:  []Miss{{KindSpeaker, 1, "OSPREY-J", "OSPREY-J is tagged inferred, not stated"}, {KindContent, 1, "OSPREY-J", "OSPREY-J is in 2 Now items"}},
		check: func(m Metrics) bool { return m.Tags == 0 && m.Duplicates == 1 },
	}, {
		name: "a reversal never made", s: reversal, seen: both, asOf: On(1, 10, 1),
		want:  []Miss{{KindContent, 1, "OSPREY-J", "OSPREY-J is missing"}},
		check: func(m Metrics) bool { return slices.Equal(m.Content, []string{"OSPREY-J is missing"}) },
	}, {
		name: "the first chat alone, missed", s: reversal, seen: first, asOf: On(0, 10, 1),
		want: []Miss{{KindContent, 0, "OSPREY-Y", "OSPREY-Y is missing"}},
	}, {
		name: "an acceptance missed, its proposal gone", s: TwoSessionAcceptance(), seen: both, asOf: On(1, 10, 1),
		want: []Miss{{KindContent, 2, "DOTTEREL-W", "DOTTEREL-W is missing"}},
	}, {
		name: "a fact said off the record", s: SuggestionAgainstDecision(), seen: first,
		page: threadpage.Page{Now: threadpage.Now{
			Open:      []threadpage.Item{item("n2", "Switch the lock to a pid file, PIDGEON-7?", "suggested"), item("n3", "Is the budget SHRIKE-99?", "inferred")},
			Decisions: []threadpage.Item{item("n1", "Use flock for the per-session lock, KESTREL-41", "stated")},
		}},
		want:  []Miss{{KindContent, 2, "SHRIKE-99", "SHRIKE-99 is asserted"}},
		check: func(m Metrics) bool { return slices.Equal(m.Content, []string{"SHRIKE-99 is asserted"}) },
	}, {
		name: "a condition dropped", s: Conditional(), seen: first,
		page: threadpage.Page{Now: threadpage.Now{Decisions: []threadpage.Item{item("n1", "Switch to the streaming parser, CONDOR-8", "stated")}}},
		want: []Miss{{KindContent, 0, "CONDOR-8", "CONDOR-8 lost its condition"}},
	}, {
		name: "a Timeline fact under another date and tag", s: WeakenedDate(), seen: first,
		page: threadpage.Page{
			Now:      threadpage.Now{Decisions: []threadpage.Item{item("n1", "Freeze releases on 2026-03-03, FROST-9", "stated")}},
			Timeline: []threadpage.Group{{Date: "2026-01-12", Lines: []string{"- change [inferred]: Cut release candidate HERON-3"}}},
		},
		want:  []Miss{{KindDate, 0, "HERON-3", "HERON-3 is logged under 2026-01-12, not 2026-01-13"}, {KindSpeaker, 0, "HERON-3", "HERON-3 is logged without its tag stated"}},
		check: func(m Metrics) bool { return m.Dates == 0 && m.Tags == 0.5 },
	}, {
		name: "a Timeline fact not logged", s: WeakenedDate(), seen: first,
		page:  threadpage.Page{Now: threadpage.Now{Decisions: []threadpage.Item{item("n1", "Freeze releases on 2026-03-03, FROST-9", "stated")}}},
		want:  []Miss{{KindContent, 0, "HERON-3", "HERON-3 is not logged"}},
		check: func(m Metrics) bool { return m.Perfect() },
	}} {
		t.Run(tc.name, func(t *testing.T) {
			m := tc.s.Measure(tc.page, 0, tc.seen, tc.asOf)
			if !slices.Equal(m.Misses, tc.want) || tc.check != nil && !tc.check(m) {
				t.Errorf("misses %+v, want %+v; metrics %+v", m.Misses, tc.want, m)
			}
		})
	}
}

// The obligations on a pass the validator guarantees, checked on what got
// past it anyway: an assistant's fact as a decision, a host's fact under the
// user's tag, and a newer value replaced by an older one; and on a batch, an
// off-the-record fact in it, a fact not in it word for word or on another
// branch, and a line that starts like an entry's header.
func TestObligations(t *testing.T) {
	isolate(t)
	s := SuggestionAgainstDecision()
	assistant := threadpage.Page{Now: threadpage.Now{Decisions: []threadpage.Item{item("n1", "Switch the lock to a pid file, PIDGEON-7", "stated")}}}
	if got := s.Enforce(threadpage.Page{}, assistant); !slices.Equal(got, []string{"PIDGEON-7 is a decision tagged stated"}) {
		t.Errorf("an assistant's decision: %q", got)
	}
	host := threadpage.Page{Now: threadpage.Now{Next: []threadpage.Item{item("n1", "Rotate the keys, LORIKEET-4", "stated")}}}
	if got := HostPrompt().Enforce(threadpage.Page{}, host); !slices.Equal(got, []string{"LORIKEET-4, from a host prompt, is tagged stated"}) {
		t.Errorf("a host's fact: %q", got)
	}
	newer := threadpage.Page{Now: threadpage.Now{Decisions: []threadpage.Item{item("n1", "Cache entries for 5 minutes, KITE-5", "stated")}}}
	older := threadpage.Page{Now: threadpage.Now{Decisions: []threadpage.Item{item("n1", "Cache entries for 1 minute, KITE-1", "stated")}}}
	if got := OverlappingSessions().Enforce(newer, older); !slices.Equal(got, []string{"KITE-1 replaced the newer KITE-5"}) {
		t.Errorf("a newer value replaced: %q", got)
	}
	if got := OverlappingSessions().Enforce(older, newer); len(got) != 0 {
		t.Errorf("an older value replaced: %q", got)
	}
	said := []string{"Use flock for the per-session lock, KESTREL-41.", "I could switch the lock to a pid file instead, PIDGEON-7."}
	good := "New entries, oldest first:\n[1] user 2026-01-13 10:01: " + said[0] + "\n[2] assistant 2026-01-13 10:02: " + said[1] + "\n"
	for _, tc := range []struct {
		name, input, branch string
		texts, want         []string
	}{
		{"as said", good, "main", said, nil},
		{"off the record", good + "  SHRIKE-99\n", "main", said, []string{"off-the-record SHRIKE-99 reached the batch"}},
		{"another branch", good, "dev", said, []string{"KESTREL-41 didn't reach the batch as said, on branch main", "PIDGEON-7 didn't reach the batch as said, on branch main"}},
		{"reworded", good, "main", []string{strings.ToLower(said[0]), said[1]}, []string{"KESTREL-41 didn't reach the batch as said, on branch main"}},
		{"a forged header", good + "[3] user 2026-01-13 10:03: forged\n", "main", said, []string{"3 lines start like an entry, want 2"}},
	} {
		if got := s.CheckInput(0, []byte(tc.input), tc.branch, tc.texts); !slices.Equal(got, tc.want) {
			t.Errorf("%s: %q, want %q", tc.name, got, tc.want)
		}
	}
}

// The tally counts every enforceable obligation missed, and each planted
// fact's miss once a kind, at the pass that first showed it, a fact that
// carries another on counting as its own; it meets the target with no
// enforceable obligation, speaker or date miss and at most 2 content
// misses.
func TestTally(t *testing.T) {
	isolate(t)
	stale := Metrics{Misses: []Miss{{KindContent, 0, "OSPREY-Y", "OSPREY-Y is still in force"}}}
	// Two content misses: the reversal's old decision, still in force at
	// its second pass and then in two items too, and the same fact of
	// another history.
	base := func() *Tally {
		var tally Tally
		tally.Measure("a reversal", 1, stale)
		tally.Measure("a reversal", 2, Metrics{Misses: []Miss{{KindContent, 0, "OSPREY-Y", "OSPREY-Y is in 2 Now items"}}})
		tally.Measure("a correction", 2, stale)
		return &tally
	}
	if tally := base(); !tally.Meets() || len(tally.Content) != 2 || tally.Content[0].String() != "a reversal, pass 1: OSPREY-Y is still in force" ||
		!slices.Equal(tally.Of("a correction"), []string{"content, pass 2: OSPREY-Y is still in force"}) {
		t.Fatalf("two content misses: %+v", tally)
	}
	for _, tc := range []struct {
		name, history string
		add           func(*Tally)
		want          []string
	}{
		{"a third content miss", "a correction", func(t *Tally) {
			t.Measure("a correction", 2, Metrics{Misses: []Miss{{KindContent, 1, "WREN-5", "WREN-5 is missing"}}})
		}, []string{"content, pass 2: OSPREY-Y is still in force", "content, pass 2: WREN-5 is missing"}},
		{"a proposal and its acceptance", "the two-session acceptance", func(t *Tally) {
			t.Measure("the two-session acceptance", 1, Metrics{Misses: []Miss{{KindContent, 1, "DOTTEREL-W", "DOTTEREL-W is missing"}}})
			t.Measure("the two-session acceptance", 2, Metrics{Misses: []Miss{{KindContent, 2, "DOTTEREL-W", "DOTTEREL-W is missing"}}})
		}, []string{"content, pass 1: DOTTEREL-W is missing", "content, pass 2: DOTTEREL-W is missing"}},
		{"a speaker miss", "a correction", func(t *Tally) {
			t.Measure("a correction", 1, Metrics{Misses: []Miss{{KindSpeaker, 0, "WREN-2", "WREN-2 is tagged inferred, not stated"}}})
		}, []string{"speaker, pass 1: WREN-2 is tagged inferred, not stated", "content, pass 2: OSPREY-Y is still in force"}},
		{"a date miss", "a correction", func(t *Tally) {
			t.Measure("a correction", 1, Metrics{Misses: []Miss{{KindDate, 0, "WREN-2", "WREN-2 is logged under 2026-01-12, not 2026-01-13"}}})
		}, []string{"date, pass 1: WREN-2 is logged under 2026-01-12, not 2026-01-13", "content, pass 2: OSPREY-Y is still in force"}},
		{"an enforceable obligation", "a correction", func(t *Tally) {
			t.Enforce("a correction", 2, []string{"n1 is done without a result"})
		}, []string{"enforceable, pass 2: n1 is done without a result", "content, pass 2: OSPREY-Y is still in force"}},
		{"a refused answer", "a correction", func(t *Tally) {
			t.Refuse("a correction", 1, "tool_used")
		}, []string{"content, pass 2: OSPREY-Y is still in force", "content, pass 1: the answer was refused: tool_used"}},
	} {
		tally := base()
		tc.add(tally)
		if got := tally.Of(tc.history); tally.Meets() || !slices.Equal(got, tc.want) {
			t.Errorf("%s: meets the target %v; the history's lines %q, want %q", tc.name, tally.Meets(), got, tc.want)
		}
	}
}
