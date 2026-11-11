package scenario

import (
	"context"
	"errors"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/heyNag/eva-md/internal/eval/results"
	"github.com/heyNag/eva-md/internal/inbox"
	"github.com/heyNag/eva-md/internal/ledger/event"
	"github.com/heyNag/eva-md/internal/ledger/inboxv1"
	"github.com/heyNag/eva-md/internal/redact"
)

// writeInbox stores every session of sc for host in a fresh v1 inbox, in a
// fresh home, and returns the inbox's root and each session's entries.
// Nothing here reads or writes the real one.
func writeInbox(t *testing.T, sc Scenario, host string) (string, [][]inbox.Entry) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(home, "gitconfig"))
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	root := filepath.Join(home, "inbox")
	var all [][]inbox.Entry
	for _, s := range sc.Sessions {
		entries, err := V1Entries(s, host)
		if err != nil {
			t.Fatal(err)
		}
		path := inbox.Store{Root: root}.Path(project, host, s.ID)
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, jsonl(t, entries), 0o600); err != nil {
			t.Fatal(err)
		}
		all = append(all, entries)
	}
	return root, all
}

// TestLocateFindsRenderedTurns: written to an inbox and read back through
// the v1 backend, each line holds the text of the turn Locate gives for its
// seq, by the session's full id or its prefix; the reader withholds only
// what the record keeps off, which plants only facts that must not be
// stored; the filler's lines find no turn; and every planted fact is found
// at exactly one line.
func TestLocateFindsRenderedTurns(t *testing.T) {
	withheld := 0
	for _, sc := range V1() {
		for _, host := range []string{"claude-code", "codex"} {
			root, entries := writeInbox(t, sc, host)
			b := inboxv1.Backend{Root: root}
			keys, err := b.Sessions(context.Background())
			if err != nil || len(keys) != len(sc.Sessions) {
				t.Fatalf("%s: %d sessions read, want %d (%v)", sc.ID, len(keys), len(sc.Sessions), err)
			}
			found := map[string]int{}
			for _, k := range keys {
				j := slices.IndexFunc(sc.Sessions, func(s Session) bool { return s.ID == k.Session })
				if j < 0 {
					t.Fatalf("%s: the inbox holds a session the scenario does not", sc.ID)
				}
				s := sc.Sessions[j]
				_, evs, err := b.Read(context.Background(), k)
				if err != nil {
					t.Fatal(err)
				}
				rendered, err := Events(s)
				if err != nil {
					t.Fatal(err)
				}
				if len(evs) != len(s.Turns)+2 || len(rendered) != len(evs) {
					t.Fatalf("%s %s: %d lines read, %d events rendered, want %d", sc.ID, s.ID, len(evs), len(rendered), len(s.Turns)+2)
				}
				for i, ev := range evs {
					ref := event.Prefix(s.ID)
					if i%2 == 1 {
						ref = s.ID
					}
					turn, facts, err := sc.Locate(ref, ev.Seq)
					if ev.Seq > int64(len(s.Turns)) {
						if !errors.Is(err, ErrNoTurn) || !strings.HasSuffix(ev.Text, filler) {
							t.Errorf("%s %s#%d: a filler line locates %v (%v)", sc.ID, ref, ev.Seq, turn, err)
						}
						continue
					}
					if err != nil {
						t.Fatalf("%s %s#%d: %v", sc.ID, ref, ev.Seq, err)
					}
					want := s.Turns[ev.Seq-1]
					if !turn.At.Equal(want.At) || turn.Text != want.Text || !slices.Equal(turn.Plants, want.Plants) {
						t.Errorf("%s %s#%d locates %+v, want %+v", sc.ID, ref, ev.Seq, turn, want)
					}
					var ids []string
					for _, f := range facts {
						ids = append(ids, f.ID)
						found[f.ID]++
					}
					if !slices.Equal(ids, want.Plants) {
						t.Errorf("%s %s#%d: facts %v, want %v", sc.ID, ref, ev.Seq, ids, want.Plants)
					}
					_, _, text, err := v1Row(turn)
					if err != nil || entries[j][ev.Seq-1].Text != text {
						t.Fatalf("%s %s#%d: the turn renders %q, the entry holds %q (%v)", sc.ID, ref, ev.Seq, text, entries[j][ev.Seq-1].Text, err)
					}
					if ev.Has(event.MarkWithheld) {
						withheld++
						if ev.Why != event.WhyOffRecord || len(facts) == 0 || slices.ContainsFunc(facts, func(f Fact) bool { return f.Must != MustNotStore }) {
							t.Errorf("%s %s#%d is withheld (%s), yet plants %v", sc.ID, ref, ev.Seq, ev.Why, facts)
						}
						continue
					}
					if ev.Text != text || ev.Kind != rendered[ev.Seq-1].Kind {
						t.Errorf("%s %s#%d reads %s %q, want %s %q", sc.ID, ref, ev.Seq, ev.Kind, ev.Text, rendered[ev.Seq-1].Kind, text)
					}
				}
			}
			for _, f := range sc.Facts {
				if found[f.ID] != 1 {
					t.Errorf("%s: fact %s is found at %d lines", sc.ID, f.ID, found[f.ID])
				}
			}
		}
	}
	if withheld != 2 {
		t.Errorf("%d lines withheld across the port, want 2: the off-the-record line of v1.01 on each host", withheld)
	}
}

// TestLocateRefusesWhatItCannotName: an unknown session, a line before the
// first or past the filler, and a prefix two sessions share are errors,
// none of them ErrNoTurn.
func TestLocateRefusesWhatItCannotName(t *testing.T) {
	sc := reversal()
	s := sc.Sessions[0]
	for _, c := range []struct {
		session string
		seq     int64
	}{{"ffffffff", 1}, {s.ID[:7], 1}, {s.ID + "0", 1}, {s.ID, 0}, {s.ID, -1}, {s.ID, int64(len(s.Turns)) + 3}} {
		if _, _, err := sc.Locate(c.session, c.seq); err == nil || errors.Is(err, ErrNoTurn) {
			t.Errorf("Locate(%q, %d): %v", c.session, c.seq, err)
		}
	}
	shared := sc
	shared.Sessions = slices.Clone(sc.Sessions)
	shared.Sessions[1].ID = event.Prefix(s.ID) + "-another"
	if _, _, err := shared.Locate(event.Prefix(s.ID), 1); err == nil {
		t.Error("Locate takes a prefix two sessions share")
	}
	if _, _, err := shared.Locate(s.ID, 1); err != nil {
		t.Errorf("Locate refuses a full id whose prefix another shares: %v", err)
	}
	if err := shared.Validate(); err == nil {
		t.Error("two sessions that share their first 8 characters are valid")
	}
}

// TestEventsFollowTheTurns: each session renders to valid events with seq
// from 1, one for each turn and two for the filler, each with its turn's
// time, author, kind, text and operation, a turn ordinal that each prompt
// opens, and a sameness key no other event of the session has.
func TestEventsFollowTheTurns(t *testing.T) {
	for _, sc := range V1() {
		for _, s := range sc.Sessions {
			evs, err := Events(s)
			if err != nil {
				t.Fatal(err)
			}
			turns, _ := rendered(s)
			if len(evs) != len(s.Turns)+2 || len(turns) != len(evs) {
				t.Fatalf("%s %s: %d events, want %d", sc.ID, s.ID, len(evs), len(s.Turns)+2)
			}
			keys := map[string]bool{}
			ordinal := 0
			for i, ev := range evs {
				tr := turns[i]
				if tr.Kind == event.KindPrompt {
					ordinal++
				}
				if ev.Seq != int64(i+1) || ev.T != tr.At.Format(time.RFC3339Nano) || ev.Author != tr.Author || ev.Kind != tr.Kind ||
					ev.Text != tr.Text || ev.Op != tr.Op || ev.Turn != ordinal || ev.Validate() != nil {
					t.Errorf("%s %s: event %d is %+v for turn %+v", sc.ID, s.ID, i+1, ev, tr)
				}
				if keys[ev.Src.Key] {
					t.Errorf("%s %s: key %s is given twice", sc.ID, s.ID, ev.Src.Key)
				}
				keys[ev.Src.Key] = true
			}
		}
	}
}

// TestRenderingsRefuseWhatTheyCannotRender: a spoken or front-model turn
// fails both renderings, saying it waits on HF-00's recorded wrapper; a
// dialog or plan decision fails V1Entries, which keeps no such entry, and
// renders as an event.
func TestRenderingsRefuseWhatTheyCannotRender(t *testing.T) {
	base := Session{Host: "claude-code", ID: "0badcafe-2222-4000-8000-000000000001", Branch: "main", Turns: []Turn{
		{At: on(0, 9, 0), Author: event.AuthorTyped, Kind: event.KindPrompt, Text: "Which queue should the worker use?"},
		{At: on(0, 9, 1), Author: event.AuthorAgent, Kind: event.KindReply, Text: "Either the disk queue or the memory queue."},
	}}
	with := func(t Turn) Session {
		s := base
		s.Turns = append(slices.Clone(base.Turns), t)
		return s
	}
	for _, author := range []event.Author{event.AuthorSpoken, event.AuthorFront} {
		s := with(Turn{At: on(0, 9, 2), Author: author, Kind: event.KindPrompt, Text: "Use the disk queue."})
		if _, err := V1Entries(s, "claude-code"); err == nil || !strings.Contains(err.Error(), "HF-00's recorded wrapper") {
			t.Errorf("V1Entries of a %s turn: %v", author, err)
		}
		if _, err := Events(s); err == nil || !strings.Contains(err.Error(), "HF-00's recorded wrapper") {
			t.Errorf("Events of a %s turn: %v", author, err)
		}
	}
	for _, kind := range []event.Kind{event.KindDialog, event.KindPlanDecision} {
		s := with(Turn{At: on(0, 9, 2), Author: event.AuthorChosen, Kind: kind, Text: "Which queue? The disk queue."})
		if _, err := V1Entries(s, "codex"); err == nil || !strings.Contains(err.Error(), "v1 keeps no "+string(kind)+" entry") {
			t.Errorf("V1Entries of a %s turn: %v", kind, err)
		}
		evs, err := Events(s)
		if err != nil || len(evs) != 5 || evs[2].Kind != kind || evs[2].Author != event.AuthorChosen || evs[2].Text != "Which queue? The disk queue." {
			t.Errorf("Events of a %s turn: %v, %+v", kind, err, evs)
		}
	}
	if _, err := V1Entries(base, "cursor"); err == nil {
		t.Error("V1Entries renders for a host v1 does not capture")
	}
	if _, err := V1Entries(with(Turn{At: on(0, 9, 2), Author: event.AuthorProgram, Kind: event.KindPrompt, Text: "Run the nightly job."}), "codex"); err == nil {
		t.Error("V1Entries drops a program prompt's text without a word")
	}
	if _, err := V1Entries(Session{Host: "codex", ID: base.ID, Branch: "main"}, "codex"); err == nil {
		t.Error("V1Entries renders a session with no turn")
	}
}

// TestValidateRefusesBrokenScenarios: each way a scenario can break the
// model is an error.
func TestValidateRefusesBrokenScenarios(t *testing.T) {
	for _, c := range []struct {
		name   string
		mutate func(*Scenario)
	}{
		{"an id that is not a results id", func(sc *Scenario) { sc.ID = "V1 01" }},
		{"an unknown split", func(sc *Scenario) { sc.Split = "train" }},
		{"no pass", func(sc *Scenario) { sc.Passes = nil }},
		{"a pass of no session", func(sc *Scenario) { sc.Passes = []Pass{{Session: 2}} }},
		{"an unknown host", func(sc *Scenario) { sc.Sessions[0].Host = "cursor" }},
		{"a fact with no slot", func(sc *Scenario) { sc.Facts[0].Slots = nil }},
		{"a fact with no kind", func(sc *Scenario) { sc.Facts[0].Kinds = nil }},
		{"an unknown kind", func(sc *Scenario) { sc.Facts[0].Kinds = []Kind{"wish"} }},
		{"a repeated kind", func(sc *Scenario) { sc.Facts[0].Kinds = []Kind{KindDecision, KindDecision} }},
		{"no must", func(sc *Scenario) { sc.Facts[0].Must = "" }},
		{"no authority", func(sc *Scenario) { sc.Facts[0].Authority = "" }},
		{"an unknown slot class", func(sc *Scenario) { sc.Facts[0].Slots[1].Class = "colour" }},
		{"an empty slot value", func(sc *Scenario) { sc.Facts[0].Slots[1].Value = " " }},
		{"a log kind without a date", func(sc *Scenario) { sc.Facts[0].Kinds = append(sc.Facts[0].Kinds, KindLog) }},
		{"a date without a log kind", func(sc *Scenario) { sc.Facts[0].LogDate = "2026-01-13" }},
		{"an end before the fact", func(sc *Scenario) { sc.Facts[1].Until = &Ref{Session: sc.Sessions[1].ID, Seq: 1} }},
		{"an end at no turn", func(sc *Scenario) { sc.Facts[0].Until = &Ref{Session: sc.Sessions[1].ID, Seq: 2} }},
		{"a fact that supersedes itself", func(sc *Scenario) { sc.Facts[1].Supersedes = sc.Facts[1].ID }},
		{"a fact that supersedes none", func(sc *Scenario) { sc.Facts[1].Supersedes = "f9" }},
		{"a fact planted twice", func(sc *Scenario) { sc.Sessions[1].Turns[0].Plants = []string{"f1", "f2"} }},
		{"a fact planted by no turn", func(sc *Scenario) { sc.Sessions[1].Turns[0].Plants = nil }},
		{"a turn that plants no fact", func(sc *Scenario) { sc.Sessions[1].Turns[0].Plants = []string{"f2", "f3"} }},
		{"a fact given twice", func(sc *Scenario) { sc.Facts[1].ID = "f1" }},
		{"a turn by no known author", func(sc *Scenario) { sc.Sessions[0].Turns[0].Author = event.AuthorUnknown }},
		{"a control that does not go off or on", func(sc *Scenario) { sc.Sessions[0].Turns[0].Kind = event.KindControl }},
		{"an operation on a prompt", func(sc *Scenario) { sc.Sessions[0].Turns[0].Op = event.OpOff }},
	} {
		sc := reversal()
		if err := sc.Validate(); err != nil {
			t.Fatal(err)
		}
		c.mutate(&sc)
		if err := sc.Validate(); err == nil {
			t.Errorf("%s is valid", c.name)
		}
	}
}

// TestRowsTakeThePort: a results store whose Forbid is a ported
// scenario's takes a row naming the scenario, a full commit id included,
// and refuses one that holds a planted word in any spelling.
func TestRowsTakeThePort(t *testing.T) {
	for _, sc := range V1() {
		store := results.Store{Dir: t.TempDir(), Forbid: Forbid(sc)}
		row := results.Row{Metric: "fidelity.fact_recall", Value: 1, Unit: "ratio", Commit: strings.Repeat("0123456789abcdef", 4)[:40],
			Instrument: "fidelity", Scenario: sc.ID, Time: time.Date(2026, 11, 11, 0, 0, 0, 0, time.UTC)}
		if err := store.Append(row); err != nil {
			t.Errorf("%s: %v", sc.ID, err)
		}
		for _, w := range Forbid(sc) {
			bad := row
			bad.Model = strings.Trim(strings.ToLower(strings.NewReplacer(" ", "-", "/", "-", "_", "-").Replace(w)), "-.")
			if err := store.Append(bad); err == nil || err.Error() != "results: row 1: model holds a word the run forbids" {
				t.Errorf("%s: a row whose model is %q: %v", sc.ID, bad.Model, err)
			}
		}
	}
}

// redacted lists the rendered texts the redactor changes, by scenario,
// session prefix and seq, with why. It is empty: a script the redactor
// changes is reported, not edited.
var redacted = map[string]string{}

// TestRedactLeavesRenderedTextsAlone: no text either rendering gives is
// changed by the redactor, but those listed in redacted.
func TestRedactLeavesRenderedTextsAlone(t *testing.T) {
	changed := map[string]bool{}
	for _, sc := range V1() {
		for _, s := range sc.Sessions {
			entries, err := V1Entries(s, "claude-code")
			if err != nil {
				t.Fatal(err)
			}
			evs, err := Events(s)
			if err != nil {
				t.Fatal(err)
			}
			for i := range entries {
				for _, text := range []string{entries[i].Text, evs[i].Text} {
					if out, _ := redact.Redact(text); out != text {
						changed[sc.ID+"/"+event.Prefix(s.ID)+"#"+strconv.Itoa(i+1)] = true
					}
				}
			}
		}
	}
	for at := range changed {
		if _, ok := redacted[at]; !ok {
			t.Errorf("the redactor changes the text at %s", at)
		}
	}
	for at := range redacted {
		if !changed[at] {
			t.Errorf("%s is listed as changed by the redactor, and is not", at)
		}
	}
}

// TestImportsNoOrganizer: no non-test file of the package, nor any package
// of the module it reaches through them, imports the update or the thread
// page model, so the scorer that reads scenarios stays apart from what it
// judges.
func TestImportsNoOrganizer(t *testing.T) {
	const module = "github.com/heyNag/eva-md/"
	root, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	var visit func(pkg, from string)
	visit = func(pkg, from string) {
		if seen[pkg] {
			return
		}
		seen[pkg] = true
		for _, banned := range []string{"internal/update", "internal/threadpage"} {
			if pkg == banned || strings.HasPrefix(pkg, banned+"/") {
				t.Errorf("%s imports %s", from, pkg)
			}
		}
		files, err := filepath.Glob(filepath.Join(root, pkg, "*.go"))
		if err != nil || len(files) == 0 {
			t.Fatalf("no Go files in %s (%v)", pkg, err)
		}
		for _, file := range files {
			if strings.HasSuffix(file, "_test.go") {
				continue
			}
			f, err := parser.ParseFile(token.NewFileSet(), file, nil, parser.ImportsOnly)
			if err != nil {
				t.Fatal(err)
			}
			for _, imp := range f.Imports {
				if p, _ := strconv.Unquote(imp.Path.Value); strings.HasPrefix(p, module) {
					visit(strings.TrimPrefix(p, module), pkg)
				}
			}
		}
	}
	visit(filepath.ToSlash(filepath.Join("internal", "eval", "scenario")), "")
	if !seen["internal/ledger/event"] || !seen["internal/inbox"] {
		t.Error("the walk missed the package's own imports")
	}
}

// TestOnlyEvasName: no file of the package, its test data included, names
// the library eva-md builds on.
func TestOnlyEvasName(t *testing.T) {
	other := "o" + "kf"
	err := filepath.WalkDir(".", func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if strings.Contains(strings.ToLower(string(data)), other) {
			t.Errorf("%s names another library", path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
