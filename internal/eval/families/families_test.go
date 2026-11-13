package families

import (
	"fmt"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/heyNag/eva-md/internal/eval/results"
	"github.com/heyNag/eva-md/internal/eval/scenario"
	"github.com/heyNag/eva-md/internal/redact"
)

// testSeed is the seed the tests draw held-out instances from. No commit
// gives it as a gate would, and no test writes an instance down.
const testSeed = 0x7e57_5eed_0000_0001

// instances are every family's instances the tests judge: a tuning
// family's from the pack, a held-out family's 20 drawn from testSeed.
func instances(t *testing.T) map[string][]scenario.Scenario {
	t.Helper()
	tuning, err := Tuning()
	if err != nil {
		t.Fatal(err)
	}
	out := map[string][]scenario.Scenario{}
	for _, sc := range tuning {
		out[sc.Family] = append(out[sc.Family], sc)
	}
	for _, f := range All() {
		if f.Split != scenario.SplitHeldout {
			continue
		}
		drawn, err := Draw(f.Name, testSeed, 20)
		if err != nil {
			t.Fatal(err)
		}
		out[f.Name] = drawn
	}
	for _, f := range All() {
		if len(out[f.Name]) < 20 {
			t.Fatalf("%d instances of %s, want at least 20", len(out[f.Name]), f.Name)
		}
	}
	return out
}

// wordRun reports whether text holds lit as a run of whole words, in any
// case.
func wordRun(text, lit string) bool {
	split := func(s string) []string {
		return strings.FieldsFunc(strings.ToLower(s), func(r rune) bool {
			return !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-' || r == '_')
		})
	}
	have, want := split(text), split(lit)
	for i := 0; i+len(want) <= len(have) && len(want) > 0; i++ {
		if slices.Equal(have[i:i+len(want)], want) {
			return true
		}
	}
	return false
}

func literals(s scenario.Slot) []string { return append([]string{s.Value}, s.Forms...) }

// TestFamiliesAreListedOnce: six families, three of each split, each by a
// name that is an id, and ByName finds each.
func TestFamiliesAreListedOnce(t *testing.T) {
	split := map[scenario.Split]int{}
	var seen []string
	for _, f := range All() {
		if slices.Contains(seen, f.Name) || !regexp.MustCompile(`^[a-z][a-z-]*$`).MatchString(f.Name) || f.Generate == nil {
			t.Errorf("family %q is listed twice, or is not an id, or has no generator", f.Name)
		}
		seen = append(seen, f.Name)
		split[f.Split]++
		if g, ok := ByName(f.Name); !ok || g.Name != f.Name {
			t.Errorf("ByName(%q) finds %q", f.Name, g.Name)
		}
	}
	if len(seen) != 6 || split[scenario.SplitTuning] != 3 || split[scenario.SplitHeldout] != 3 {
		t.Errorf("families %v, splits %v", seen, split)
	}
	if _, ok := ByName("v1"); ok {
		t.Error("ByName finds a family that is not listed")
	}
}

// TestInstancesKeepTheModel: every instance is a valid scenario of its
// family and split, with the id <name>.<seed in hex>, made again alike from
// its seed. Every planted value and form holds a letter from g to z and is
// in Forbid; a turn holds a planted value only if it plants a fact with
// it; every template's placeholders are filled; each instance has turns
// that plant nothing; and where the planting turns fall varies with the
// seed.
func TestInstancesKeepTheModel(t *testing.T) {
	for name, scs := range instances(t) {
		f, _ := ByName(name)
		firsts, lengths := map[int]bool{}, map[int]bool{}
		for _, sc := range scs {
			if err := sc.Validate(); err != nil {
				t.Fatal(err)
			}
			if sc.ID != fmt.Sprintf("%s.%x", name, sc.Seed) || sc.Family != name || sc.Split != f.Split || sc.Name == "" {
				t.Errorf("%s: family %s, split %s, seed %x", sc.ID, sc.Family, sc.Split, sc.Seed)
			}
			if again := f.Generate(sc.Seed); !reflect.DeepEqual(again, sc) {
				t.Errorf("%s is not made alike from its seed", sc.ID)
			}
			forbid := scenario.Forbid(sc)
			for _, fact := range sc.Facts {
				for _, sl := range fact.Slots {
					for _, lit := range literals(sl) {
						if !strings.ContainsFunc(strings.ToLower(lit), func(r rune) bool { return r >= 'g' && r <= 'z' }) || !slices.Contains(forbid, lit) {
							t.Errorf("%s: %q has no letter from g to z, or is not forbidden", sc.ID, lit)
						}
					}
				}
			}
			if len(sc.Sessions) != 1 {
				t.Fatalf("%s has %d sessions", sc.ID, len(sc.Sessions))
			}
			s := sc.Sessions[0]
			for n, tr := range s.Turns {
				if strings.ContainsAny(tr.Text, "{}") {
					t.Errorf("%s#%d leaves a placeholder: %q", sc.ID, n+1, tr.Text)
				}
				for _, fact := range sc.Facts {
					for _, sl := range fact.Slots {
						holds := slices.ContainsFunc(literals(sl), func(lit string) bool { return wordRun(tr.Text, lit) })
						if holds && !plantsSlot(sc, tr, sl) {
							t.Errorf("%s#%d holds %q of %s, and plants no fact with it", sc.ID, n+1, sl.Value, fact.ID)
						}
						if !holds && slices.Contains(tr.Plants, fact.ID) {
							t.Errorf("%s#%d plants %s, and does not hold %q", sc.ID, n+1, fact.ID, sl.Value)
						}
					}
				}
			}
			first := slices.IndexFunc(s.Turns, func(tr scenario.Turn) bool { return len(tr.Plants) > 0 })
			firsts[first], lengths[len(s.Turns)] = true, true
			if !slices.ContainsFunc(s.Turns, func(tr scenario.Turn) bool { return len(tr.Plants) == 0 }) {
				t.Errorf("%s: every turn plants a fact", sc.ID)
			}
		}
		if len(firsts) < 2 || len(lengths) < 2 {
			t.Errorf("%s: the first planting turn falls at %v and the chats are %v turns long, over %d seeds", name, firsts, lengths, len(scs))
		}
	}
}

// plantsSlot reports whether turn tr plants a fact with slot sl.
func plantsSlot(sc scenario.Scenario, tr scenario.Turn, sl scenario.Slot) bool {
	for _, f := range sc.Facts {
		if slices.Contains(tr.Plants, f.ID) && slices.ContainsFunc(f.Slots, func(g scenario.Slot) bool { return g.Class == sl.Class && g.Value == sl.Value }) {
			return true
		}
	}
	return false
}

// TestTemplatesAndBanks: each role of a planting turn has at least three
// templates, and every turn of the fillers at least three; the bank holds
// at least 20 names, no two alike, each with a letter from g to z and no
// word another holds; each hypothesis template holds a hedge; and no
// template of a family is another's.
func TestTemplatesAndBanks(t *testing.T) {
	var all []string
	for family, roles := range texts {
		for role, list := range roles {
			if len(list) < 3 {
				t.Errorf("%s %s has %d templates, want at least 3", family, role, len(list))
			}
			for _, tmpl := range list {
				if slices.Contains(all, tmpl) {
					t.Errorf("%q is given twice", tmpl)
				}
				all = append(all, tmpl)
			}
		}
	}
	for _, from := range []string{"filler", "heldout-filler"} {
		if len(texts[from]["ask"]) != len(texts[from]["answer"]) {
			t.Errorf("%s: each question has an answer that goes with it", from)
		}
	}
	for _, tmpl := range limitTexts["hypothesis"] {
		if !slices.ContainsFunc(hedges, func(h string) bool { return wordRun(tmpl, h) }) {
			t.Errorf("%q holds no hedge", tmpl)
		}
	}
	for _, form := range limitForms {
		if len(limitTexts[form]) < 3 {
			t.Errorf("the limit form %s has %d templates", form, len(limitTexts[form]))
		}
	}
	if len(names) < 20 {
		t.Errorf("the bank holds %d names, want at least 20", len(names))
	}
	for i, n := range names {
		if !strings.ContainsFunc(n, func(r rune) bool { return r >= 'g' && r <= 'z' }) {
			t.Errorf("%q has no letter from g to z", n)
		}
		for _, m := range names[:i] {
			if strings.Contains(n, m) || strings.Contains(m, n) {
				t.Errorf("%q and %q overlap", n, m)
			}
		}
		for _, tmpl := range all {
			if wordRun(tmpl, n) {
				t.Errorf("the template %q holds the name %q", tmpl, n)
			}
		}
	}
}

// TestDraws: a draw makes n distinct instances, the same for the same seed;
// two draws with different seeds share no instance; a draw of no family
// or of no instance is an error; and SeedOf reads the first 16 hex digits
// of a commit and refuses anything else.
func TestDraws(t *testing.T) {
	for _, f := range All() {
		a, err := Draw(f.Name, testSeed, 25)
		if err != nil {
			t.Fatal(err)
		}
		b, _ := Draw(f.Name, testSeed+1, 25)
		c, _ := Draw(f.Name, ^uint64(testSeed), 25)
		again, _ := Draw(f.Name, testSeed, 25)
		if !reflect.DeepEqual(a, again) {
			t.Errorf("%s: two draws from one seed differ", f.Name)
		}
		ids, hashes := map[string]int{}, map[string]int{}
		for i, draw := range [][]scenario.Scenario{a, b, c} {
			if len(draw) != 25 {
				t.Fatalf("%s: a draw of 25 made %d", f.Name, len(draw))
			}
			for _, sc := range draw {
				h, err := Hash(sc)
				if err != nil {
					t.Fatal(err)
				}
				if _, ok := ids[sc.ID]; ok {
					t.Errorf("%s: draw %d makes an instance another made", f.Name, i)
				}
				if _, ok := hashes[h]; ok {
					t.Errorf("%s: two instances are alike (draw %d)", f.Name, i)
				}
				ids[sc.ID], hashes[h] = i, i
			}
		}
	}
	if _, err := Draw("wish", 1, 1); err == nil {
		t.Error("a draw of no family")
	}
	if _, err := Draw("limit", 1, 0); err == nil {
		t.Error("a draw of no instance")
	}
	commit := "0123456789abcdef0123456789abcdef01234567"
	if s, err := SeedOf(commit); err != nil || s != 0x0123456789abcdef {
		t.Errorf("SeedOf(%s) = %x, %v", commit, s, err)
	}
	if s, err := SeedOf("fedcba9876543210"); err != nil || s != 0xfedcba9876543210 {
		t.Errorf("SeedOf of 16 digits = %x, %v", s, err)
	}
	for _, bad := range []string{"", "0123456789abcde", "0123456789ABCDEF0123", "0123456789abcdeg0123", " 0123456789abcdef", "+123456789abcdef0"} {
		if _, err := SeedOf(bad); err == nil {
			t.Errorf("SeedOf(%q) takes it", bad)
		}
	}
}

// heldoutID is the shape of a held-out instance's id.
var heldoutID = func() *regexp.Regexp {
	var held []string
	for _, f := range All() {
		if f.Split == scenario.SplitHeldout {
			held = append(held, regexp.QuoteMeta(f.Name))
		}
	}
	return regexp.MustCompile(`(?i)\b(?:` + strings.Join(held, "|") + `)\.[0-9a-f]+\b`)
}()

// TestNoFileNamesAHeldOutInstance: no file under the package's folder, its
// tests and test data included, holds the id of a held-out instance. The
// pattern matches every id a draw makes, and one written in a text.
func TestNoFileNamesAHeldOutInstance(t *testing.T) {
	for _, f := range All() {
		drawn, _ := Draw(f.Name, testSeed, 3)
		for _, sc := range drawn {
			if heldoutID.MatchString(sc.ID) != (f.Split == scenario.SplitHeldout) {
				t.Errorf("the pattern says %v of %s", heldoutID.MatchString(sc.ID), sc.ID)
			}
		}
	}
	if !heldoutID.MatchString("see " + "unagreed" + ".9" + "f2c, line 4") {
		t.Error("the pattern misses an id written in a text")
	}
	files := 0
	err := filepath.WalkDir(".", func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		files++
		if m := heldoutID.Find(data); m != nil {
			t.Errorf("%s names a held-out instance", path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if files < 8 {
		t.Errorf("%d files read; the walk missed the folder", files)
	}
}

// TestRedactLeavesTextsAlone: no text of any instance, rendered as events
// or, where v1 keeps it, as inbox entries, is changed by the redactor.
func TestRedactLeavesTextsAlone(t *testing.T) {
	texts := 0
	for _, scs := range instances(t) {
		for _, sc := range scs {
			for _, s := range sc.Sessions {
				evs, err := scenario.Events(s)
				if err != nil {
					t.Fatalf("%s: %v", sc.ID, err)
				}
				var all []string
				for _, ev := range evs {
					all = append(all, ev.Text)
				}
				entries, err := scenario.V1Entries(s, s.Host)
				switch {
				case sc.Family == "accepted":
					if err == nil || !strings.Contains(err.Error(), "v1 keeps no") {
						t.Errorf("%s renders for v1: %v", sc.ID, err)
					}
				case err != nil:
					t.Fatalf("%s: %v", sc.ID, err)
				}
				for _, e := range entries {
					all = append(all, e.Text)
				}
				for i, text := range all {
					texts++
					if out, _ := redact.Redact(text); out != text {
						t.Errorf("%s: the redactor changes text %d", sc.ID, i+1)
					}
				}
			}
		}
	}
	if texts < 1000 {
		t.Errorf("%d texts read", texts)
	}
}

// TestRowsTakeEveryInstance: a results store whose Forbid is an instance's
// takes every row its score gives, a full commit and a host named, and
// refuses one whose model is a planted value.
func TestRowsTakeEveryInstance(t *testing.T) {
	for _, scs := range instances(t) {
		for _, sc := range scs {
			store := results.Store{Dir: t.TempDir(), Forbid: scenario.Forbid(sc)}
			rows := scoreIdeal(sc).Rows(results.Row{Commit: strings.Repeat("0123456789abcdef", 4)[:40], Instrument: "fidelity",
				Host: sc.Sessions[0].Host, HostVersion: "1.0.0", Model: "test-model", Seed: sc.Seed, Time: time.Date(2026, 11, 13, 0, 0, 0, 0, time.UTC)})
			if err := store.Append(rows...); err != nil {
				t.Errorf("%s: %v", sc.ID, err)
			}
			bad := rows[0]
			bad.Model = strings.ToLower(strings.ReplaceAll(sc.Facts[0].Slots[0].Value, " ", "-"))
			if err := store.Append(bad); err == nil {
				t.Errorf("%s: a row whose model is a planted value is taken", sc.ID)
			}
		}
	}
}

// TestImportsNoOrganizer: no non-test file of the package, nor any package
// of the module it reaches through them, imports the update or the thread
// page model.
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
	visit(filepath.ToSlash(filepath.Join("internal", "eval", "families")), "")
	if !seen["internal/eval/scenario"] || !seen["internal/ledger/event"] {
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
