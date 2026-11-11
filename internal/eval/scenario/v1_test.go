package scenario

import (
	"bytes"
	"encoding/json"
	"flag"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/heyNag/eva-md/internal/inbox"
	"github.com/heyNag/eva-md/internal/update/accuracy"
)

var regen = flag.Bool("regen", false, "rewrite the golden scenarios in testdata/v1")

// jsonl is entries as a session file, as accuracy.Chat.JSONL writes it.
func jsonl(t *testing.T, entries []inbox.Entry) []byte {
	t.Helper()
	var b bytes.Buffer
	for _, e := range entries {
		data, err := json.Marshal(e)
		if err != nil {
			t.Fatal(err)
		}
		b.Write(data)
		b.WriteByte('\n')
	}
	return b.Bytes()
}

// TestV1EntriesMatchTheSuite: every chat of every ported script renders,
// for both hosts, to the session file the suite renders, byte for byte.
func TestV1EntriesMatchTheSuite(t *testing.T) {
	scripts, ported := accuracy.Scripts(), V1()
	if len(ported) != len(scripts) || len(ported) != 12 {
		t.Fatalf("%d ported scenarios, %d scripts, want 12 of each", len(ported), len(scripts))
	}
	for i, s := range scripts {
		sc := ported[i]
		if len(sc.Sessions) != len(s.Turns) {
			t.Fatalf("%s: %d sessions, the script has %d chats", sc.ID, len(sc.Sessions), len(s.Turns))
		}
		for chat := range s.Turns {
			for _, host := range []string{"claude-code", "codex"} {
				want, err := s.Chat(chat, host).JSONL()
				if err != nil {
					t.Fatal(err)
				}
				entries, err := V1Entries(sc.Sessions[chat], host)
				if err != nil {
					t.Fatalf("%s chat %d on %s: %v", sc.ID, chat, host, err)
				}
				if got := jsonl(t, entries); !bytes.Equal(got, want) {
					t.Errorf("%s chat %d on %s renders\n%s\nwant\n%s", sc.ID, chat, host, got, want)
				}
			}
		}
	}
}

// added are the facts the port plants where v1 plants none, by scenario,
// each named by its first slot's value; v1.go says why at each.
var added = map[string][]string{"v1.08": {"parser", "parser", "hidden ids"}, "v1.11": {"KITE-WARM"}}

// TestV1PortsTheSuite: each ported scenario has its script's id, name,
// passes and facts, and judges each fact as v1 does, but where a comment in
// v1.go says otherwise.
func TestV1PortsTheSuite(t *testing.T) {
	where := map[string]Kind{"decisions": KindDecision, "next": KindNext, "open": KindOpen, "timeline": KindLog}
	for i, s := range accuracy.Scripts() {
		sc := V1()[i]
		if want := "v1." + []string{"01", "02", "03", "04", "05", "06", "07", "08", "09", "10", "11", "12"}[i]; sc.ID != want || sc.Family != "v1" || sc.Split != SplitTuning || sc.Name != s.Name {
			t.Errorf("scenario %d is %s (%s, %s, %q), want %s (v1, tuning, %q)", i+1, sc.ID, sc.Family, sc.Split, sc.Name, want, s.Name)
		}
		var passes []Pass
		for _, p := range s.Passes {
			passes = append(passes, Pass{Session: p.Chat, Kept: p.Kept})
		}
		if !slices.Equal(sc.Passes, passes) {
			t.Errorf("%s: passes %v, want %v", sc.ID, sc.Passes, passes)
		}
		mapped := map[string]bool{}
		for _, f := range s.Facts {
			got, ok := portOf(sc, f)
			if !ok {
				t.Errorf("%s: no ported fact %s at its turn", sc.ID, f.Code)
				continue
			}
			mapped[got.ID] = true
			wantAuthority, wantMust := AuthorityOther, MustStore
			switch {
			case f.Off:
				wantAuthority, wantMust = AuthorityUser, MustNotStore
			case f.Pasted:
				wantMust = MustNotWithAuthority
			case f.Tag == "stated" || f.Tag == "accepted":
				wantAuthority = AuthorityUser
			}
			if got.Authority != wantAuthority || got.Must != wantMust {
				t.Errorf("%s: %s is %s with %s authority, want %s with %s", sc.ID, f.Code, got.Must, got.Authority, wantMust, wantAuthority)
			}
			if f.Where != "" && !slices.Contains(got.Kinds, where[f.Where]) {
				t.Errorf("%s: %s may be filed as %v, which leaves out v1's %s", sc.ID, f.Code, got.Kinds, f.Where)
			}
			if logged := f.Where == "timeline"; logged != (got.LogDate != "") || logged && got.LogDate != f.At.Format(time.DateOnly) {
				t.Errorf("%s: %s has log date %q", sc.ID, f.Code, got.LogDate)
			}
			if f.Until.IsZero() != (got.Until == nil) {
				t.Errorf("%s: %s ends at %v, v1 at %v", sc.ID, f.Code, got.Until, f.Until)
			} else if got.Until != nil {
				if end, err := sc.turn(*got.Until); err != nil || !end.At.Equal(f.Until) {
					t.Errorf("%s: %s ends at a turn of %v, want %v (%v)", sc.ID, f.Code, end.At, f.Until, err)
				}
			}
			if old := factByID(sc, got.Supersedes); f.Replaces != "" && (old == nil || old.Slots[0].Value != f.Replaces) || f.Replaces == "" && got.Supersedes != "" {
				t.Errorf("%s: %s supersedes %q, v1 replaces %q", sc.ID, f.Code, got.Supersedes, f.Replaces)
			}
			if f.Cond && !slices.ContainsFunc(got.Slots, func(s Slot) bool { return s.Class == SlotCond && strings.Contains(f.Says, s.Value) }) {
				t.Errorf("%s: conditional %s has no condition slot", sc.ID, f.Code)
			}
		}
		var extra []string
		for _, f := range sc.Facts {
			if !mapped[f.ID] {
				extra = append(extra, f.Slots[0].Value)
			}
		}
		if !slices.Equal(extra, added[sc.ID]) {
			t.Errorf("%s: the port adds facts %q, want %q", sc.ID, extra, added[sc.ID])
		}
	}
}

// portOf is the ported fact v1's fact f becomes: planted at the turn that
// says it, with its codename as its first slot.
func portOf(sc Scenario, f accuracy.Fact) (Fact, bool) {
	if f.Session >= len(sc.Sessions) {
		return Fact{}, false
	}
	for _, tr := range sc.Sessions[f.Session].Turns {
		if !tr.At.Equal(f.At) || tr.Text != f.Says {
			continue
		}
		for _, id := range tr.Plants {
			if g := factByID(sc, id); g != nil && g.Slots[0].Class == SlotIdent && g.Slots[0].Value == f.Code {
				return *g, true
			}
		}
	}
	return Fact{}, false
}

func factByID(sc Scenario, id string) *Fact {
	for i := range sc.Facts {
		if sc.Facts[i].ID == id {
			return &sc.Facts[i]
		}
	}
	return nil
}

// TestEveryPortedSlotIsSaid: each slot's value is said, as written, in the
// turn that plants it or one before it in the scenario: an acceptance
// carries on a value said before.
func TestEveryPortedSlotIsSaid(t *testing.T) {
	for _, sc := range V1() {
		for _, f := range sc.Facts {
			var said []string
			var at time.Time
			for _, s := range sc.Sessions {
				for _, tr := range s.Turns {
					if slices.Contains(tr.Plants, f.ID) {
						at = tr.At
					}
				}
			}
			for _, s := range sc.Sessions {
				for _, tr := range s.Turns {
					if !tr.At.After(at) {
						said = append(said, tr.Text)
					}
				}
			}
			for _, s := range f.Slots {
				if !slices.ContainsFunc(said, func(text string) bool { return strings.Contains(text, s.Value) }) {
					t.Errorf("%s: fact %s's slot %q is said nowhere before it", sc.ID, f.ID, s.Value)
				}
			}
		}
	}
}

// TestV1Golden: the 12 ported scenarios are committed as JSON, and each
// file decodes, strictly, to a valid scenario that encodes back to it.
func TestV1Golden(t *testing.T) {
	for _, sc := range V1() {
		if err := sc.Validate(); err != nil {
			t.Fatal(err)
		}
		data, err := encode(sc)
		if err != nil {
			t.Fatal(err)
		}
		path := filepath.Join("testdata", "v1", sc.ID+".json")
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
			t.Errorf("%s differs from %s; run the test with -regen after checking the change", sc.ID, path)
		}
		var back Scenario
		d := json.NewDecoder(bytes.NewReader(golden))
		d.DisallowUnknownFields()
		if err := d.Decode(&back); err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		if _, err := d.Token(); err != io.EOF {
			t.Errorf("%s: data after the scenario", path)
		}
		if err := back.Validate(); err != nil {
			t.Errorf("%s: %v", path, err)
		}
		if again, err := encode(back); err != nil || !bytes.Equal(again, golden) {
			t.Errorf("%s does not encode back to itself (%v)", path, err)
		}
	}
	files, err := filepath.Glob(filepath.Join("testdata", "v1", "*.json"))
	if err != nil || len(files) != 12 {
		t.Errorf("%d golden files, want 12 (%v)", len(files), err)
	}
}

func encode(sc Scenario) ([]byte, error) {
	data, err := json.MarshalIndent(sc, "", "  ")
	return append(data, '\n'), err
}

// codename is how the suite writes a codename.
var codename = regexp.MustCompile(`\b[A-Z]+-[A-Z0-9]+\b`)

// TestForbidHoldsEveryCodename: Forbid lists every codename of the suite,
// those of its facts and any its texts say, and keeps a word of every fact,
// each holding a letter from g to z. Beyond the codenames, it lists every
// planted word: each value and accepted form of every slot that holds such
// a letter, and nothing else.
func TestForbidHoldsEveryCodename(t *testing.T) {
	for i, s := range accuracy.Scripts() {
		sc := V1()[i]
		words := Forbid(sc)
		want := map[string]bool{}
		for _, f := range sc.Facts {
			for _, sl := range f.Slots {
				for _, w := range append([]string{sl.Value}, sl.Forms...) {
					if strings.ContainsFunc(strings.ToLower(w), func(r rune) bool { return r >= 'g' && r <= 'z' }) {
						want[w] = true
						if !slices.Contains(words, w) {
							t.Errorf("%s: Forbid leaves out the planted word %q of fact %s", sc.ID, w, f.ID)
						}
					}
				}
			}
		}
		for j, w := range words {
			if !want[w] || slices.Contains(words[:j], w) {
				t.Errorf("%s: Forbid holds %q, which is no planted word or is listed twice", sc.ID, w)
			}
		}
		var codes []string
		for _, f := range s.Facts {
			codes = append(codes, f.Code)
		}
		for _, chat := range s.Turns {
			for _, tr := range chat {
				codes = append(codes, codename.FindAllString(tr.Text, -1)...)
			}
		}
		for _, c := range codes {
			if !slices.Contains(words, c) {
				t.Errorf("%s: Forbid leaves out %s", sc.ID, c)
			}
		}
		if err := checkForbid(sc); err != nil {
			t.Error(err)
		}
	}
}

// checkForbid reports a word of Forbid(sc) with no letter from g to z, a
// word that is not a value or form of one of sc's slots, or a fact none of
// whose slots' values is in the list.
func checkForbid(sc Scenario) error {
	words := Forbid(sc)
	var values []string
	for _, f := range sc.Facts {
		var kept bool
		for _, s := range f.Slots {
			values = append(values, s.Value)
			values = append(values, s.Forms...)
			kept = kept || slices.Contains(words, s.Value)
		}
		if !kept {
			return &forbidError{sc.ID + ": fact " + f.ID + " has no word in Forbid"}
		}
	}
	for _, w := range words {
		g := false
		for _, r := range strings.ToLower(w) {
			g = g || strings.ContainsRune("ghijklmnopqrstuvwxyz", r)
		}
		if !g || !slices.Contains(values, w) {
			return &forbidError{sc.ID + ": Forbid holds a word with no letter from g to z, or no slot's"}
		}
	}
	return nil
}

type forbidError struct{ s string }

func (e *forbidError) Error() string { return e.s }

// TestForbidCheckFailsADoctoredScenario: a fact whose only value is a
// bare number or a date in digits has no forbidden word, which the check
// above and Validate both refuse.
func TestForbidCheckFailsADoctoredScenario(t *testing.T) {
	for _, value := range []string{"300", "2026-03-03", "DEADBEEF", "3.5e10"} {
		sc := conditional()
		sc.Facts[0].Slots = []Slot{slot(SlotInt, value)}
		if err := checkForbid(sc); err == nil {
			t.Errorf("a fact whose only slot is %q passes the check", value)
		}
		if err := sc.Validate(); err == nil || !strings.Contains(err.Error(), "no forbidden word") {
			t.Errorf("a fact whose only slot is %q is valid: %v", value, err)
		}
		if words := Forbid(sc); len(words) != 0 {
			t.Errorf("Forbid of a fact whose only slot is %q is %q", value, words)
		}
	}
}

// TestV1CitedLinesPlant: each line v1's correct output for v1.08's second
// chat cites plants a fact in the port, so the scorer counts none of v1's
// correct items as noise: the test run and the word that the parser is
// finished (MARTIN-1's Done, and its dated line), and the user's answer to
// MARTIN-3. MARTIN-1 still ends at the word that the parser is finished.
func TestV1CitedLinesPlant(t *testing.T) {
	sc := V1()[7]
	s := sc.Sessions[1]
	for _, said := range []string{"go test", "parser is finished", "hidden ids"} {
		n := slices.IndexFunc(s.Turns, func(tr Turn) bool { return strings.Contains(tr.Text, said) })
		if n < 0 {
			t.Fatalf("%s: no line says %q", sc.ID, said)
		}
		if _, facts, err := sc.Locate(s.ID[:8], int64(n+1)); err != nil || len(facts) == 0 {
			t.Errorf("%s: the line that says %q plants %d facts (%v)", sc.ID, said, len(facts), err)
		}
	}
	martin1 := sc.Facts[0]
	if martin1.Slots[0].Value != "MARTIN-1" || martin1.Until == nil || martin1.Until.Session != s.ID || martin1.Until.Seq != 2 {
		t.Errorf("%s: MARTIN-1 is %+v, want it to end at the word that the parser is finished", sc.ID, martin1)
	}
}
