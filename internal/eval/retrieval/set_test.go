package retrieval

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"unicode"
	"unicode/utf8"
)

// loadCommitted loads the committed set, once per test.
func loadCommitted(t *testing.T) *Set {
	t.Helper()
	s, err := Load(committedSet)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// tokens returns s's runs of letters and digits of 3 characters or more,
// as written.
func tokens(s string) []string {
	var out []string
	for _, f := range strings.FieldsFunc(s, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) }) {
		if utf8.RuneCountInString(f) >= 3 {
			out = append(out, f)
		}
	}
	return out
}

// wordsOf returns the lowercased tokens of the texts, as a set.
func wordsOf(texts ...string) map[string]bool {
	out := map[string]bool{}
	for _, s := range texts {
		for _, w := range tokens(s) {
			out[strings.ToLower(w)] = true
		}
	}
	return out
}

// shared returns the words of 3 characters or more that text shares with
// the item's title or statement, but for the set's common words, sorted.
func shared(s *Set, text string, it *Item) []string {
	mine := wordsOf(it.Title, it.Statement)
	var out []string
	for w := range wordsOf(text) {
		if mine[w] && !slices.Contains(s.Common, w) {
			out = append(out, w)
		}
	}
	slices.Sort(out)
	return out
}

// nameOf returns the item's name, a cue of one capitalized word, or "".
func nameOf(it *Item) string {
	for _, c := range it.Cues {
		if IsName(c) {
			return c
		}
	}
	return ""
}

// siblings reports whether two projects share a system page.
func siblings(s *Set, a, b string) bool {
	if a == b {
		return false
	}
	for _, g := range s.Groups {
		if slices.Contains(g.Projects, a) && slices.Contains(g.Projects, b) {
			return true
		}
	}
	return false
}

func inScope(it *Item, project string) bool { return it.Scope == ScopeAll || it.Project == project }

// checkLabels checks that each event's labels say what its text does. It
// returns the first fault, naming the event or item by id.
func checkLabels(s *Set) error {
	items := map[string]*Item{}
	names := map[string]string{} // name: the item it is a cue of
	for i := range s.Items {
		it := &s.Items[i]
		items[it.ID] = it
		for _, c := range it.Cues {
			if !IsName(c) {
				continue
			}
			if other, ok := names[c]; ok {
				return fmt.Errorf("the name %s is a cue of %s and %s", c, other, it.ID)
			}
			if nameOf(it) != c {
				return fmt.Errorf("item %s has two names", it.ID)
			}
			names[c] = it.ID
		}
	}
	for name, id := range names {
		word := strings.ToLower(name)
		for _, it := range s.Items {
			if it.ID != id && wordsOf(append([]string{it.Title, it.Statement, it.Words}, it.Cues...)...)[word] {
				return fmt.Errorf("the name %s's word is also in item %s", name, it.ID)
			}
		}
		if it := items[id]; (it.Type == "rule" || it.Type == "constraint") && !wordsOf(it.Words)[word] {
			return fmt.Errorf("the name %s is not in its %s's words", name, it.Type)
		}
	}
	for i := range s.Events {
		e := &s.Events[i]
		if err := checkEvent(s, e, items); err != nil {
			return fmt.Errorf("event %s (%s, %s): %w", e.ID, e.Split, e.Class, err)
		}
	}
	return nil
}

func checkEvent(s *Set, e *Event, items map[string]*Item) error {
	text := wordsOf(e.Text)
	for _, p := range s.Projects {
		if text[strings.ToLower(p.Name)] {
			return fmt.Errorf("it names the project %s", p.ID)
		}
	}
	if e.Split == "name" && e.Class != Positive && e.Class != "near_miss" {
		return errors.New("a name event is a positive or a near miss")
	}
	strongest := func(ids []string) int { // the fewest words the text shares with any of them
		n := -1
		for _, id := range ids {
			if k := len(shared(s, e.Text, items[id])); n < 0 || k < n {
				n = k
			}
		}
		return n
	}
	// below reports an item in the event's scope that shares as many words
	// as the weakest of the lures, or more than limit when there is none.
	below := func(lures []string, limit int) error {
		if len(lures) > 0 {
			limit = strongest(lures) - 1
		}
		for _, it := range s.Items {
			if inScope(&it, e.Project) && len(shared(s, e.Text, &it)) > limit {
				return fmt.Errorf("item %s of its scope shares %v", it.ID, shared(s, e.Text, &it))
			}
		}
		return nil
	}
	switch e.Class {
	case Positive:
		for _, id := range e.Relevant {
			it := items[id]
			if !inScope(it, e.Project) {
				return fmt.Errorf("relevant item %s is another project's", id)
			}
			switch w := shared(s, e.Text, it); {
			case e.Split == "lexical" && len(w) < 2:
				return fmt.Errorf("it shares %v with relevant item %s, not two words", w, id)
			case e.Split == "paraphrase" && len(w) > 0:
				return fmt.Errorf("it shares %v with relevant item %s", w, id)
			}
		}
		if e.Split == "name" {
			if len(e.Relevant) != 1 || nameOf(items[e.Relevant[0]]) == "" {
				return errors.New("a name event has one relevant item, and it has a name")
			}
			if name := nameOf(items[e.Relevant[0]]); !slices.Contains(tokens(e.Text), name) {
				return fmt.Errorf("it does not hold the name of %s", e.Relevant[0])
			}
		}
	case "near_miss":
		if len(e.Decoys) == 0 {
			return errors.New("a near miss has no decoy")
		}
		for _, id := range e.Decoys {
			if w := shared(s, e.Text, items[id]); len(w) < 2 {
				return fmt.Errorf("it shares %v with decoy %s, not two words", w, id)
			}
		}
		if e.Split == "name" {
			name := ""
			if len(e.Decoys) == 1 {
				name = nameOf(items[e.Decoys[0]])
			}
			switch {
			case name == "":
				return errors.New("a name near miss has one decoy, and it has a name")
			case slices.Contains(tokens(e.Text), name) || !slices.Contains(tokens(e.Text), strings.ToLower(name)):
				return fmt.Errorf("it does not hold %s's word as an ordinary word", e.Decoys[0])
			}
		}
	case "sibling_only":
		if len(e.Decoys) == 0 {
			return errors.New("a sibling-only event has no decoy")
		}
		for _, id := range e.Decoys {
			if !siblings(s, e.Project, items[id].Project) {
				return fmt.Errorf("decoy %s is not of a project that shares a system page with it", id)
			}
		}
		if err := below(e.Decoys, 0); err != nil {
			return err
		}
	case "no_match":
		for _, id := range e.Decoys {
			if it := items[id]; it.Scope == ScopeAll || it.Project == e.Project || siblings(s, e.Project, it.Project) {
				return fmt.Errorf("decoy %s is of its scope or a sibling's", id)
			}
		}
		if err := below(e.Decoys, 1); err != nil {
			return err
		}
	}
	return nil
}

// The set has the shape the card sets: enough items, projects, events and
// negatives, every class and split in each half, and enough name events
// that resolve and name near misses in each half.
func TestTheSetsShape(t *testing.T) {
	s := loadCommitted(t)
	if len(s.Items) < 500 || len(s.Events) < 800 {
		t.Errorf("%d items and %d events; the set needs 500 and 800", len(s.Items), len(s.Events))
	}
	byProject := map[string]int{}
	kinds := map[string]bool{}
	for _, it := range s.Items {
		byProject[it.Project]++
		kinds[it.Type] = true
	}
	delete(byProject, "")
	if len(byProject) < 3 || len(s.Projects) != len(byProject) {
		t.Errorf("items in %d projects of %d; the set needs 3", len(byProject), len(s.Projects))
	}
	if len(kinds) != len(Kinds) {
		t.Errorf("the items are of %d kinds, not %d", len(kinds), len(Kinds))
	}
	if len(s.Groups) == 0 {
		t.Error("no projects share a system page, so no event is a sibling's")
	}
	for _, half := range Halves {
		count := map[string]int{}
		negatives := 0
		for _, e := range s.Events {
			if Half(e.ID) != half {
				continue
			}
			count[e.Class]++
			count[e.Split]++
			count[e.Split+"."+e.Class]++
			if e.Class != Positive {
				negatives++
			}
		}
		if negatives < 160 {
			t.Errorf("%s: %d negatives, not 160", half, negatives)
		}
		for _, k := range append(slices.Clone(Classes), Splits...) {
			if count[k] == 0 {
				t.Errorf("%s: no event is %s", half, k)
			}
		}
		if count["name.positive"] < 30 || count["name.near_miss"] < 30 {
			t.Errorf("%s: %d name events that resolve and %d name near misses, not 30 of each", half, count["name.positive"], count["name.near_miss"])
		}
		t.Logf("%s: %v", half, count)
	}
	for _, e := range s.Events {
		for _, id := range e.Relevant {
			it := s.Item(id)
			if it == nil || it.Scope != ScopeAll && it.Project != e.Project || siblings(s, e.Project, it.Project) {
				t.Errorf("event %s: relevant item %s is missing or another project's", e.ID, id)
			}
		}
	}
}

// The labels say what each text does: a name is a cue of one item, a
// paraphrase shares no word with its item but the set's common ones, a
// near miss shares two with each decoy, and a negative's lures are the
// strongest words in its scope.
func TestTheSetsLabels(t *testing.T) {
	s := loadCommitted(t)
	if len(s.Common) == 0 || len(s.Common) > 40 {
		t.Errorf("the set declares %d common words; the list is short and not empty", len(s.Common))
	}
	if err := checkLabels(s); err != nil {
		t.Fatal(err)
	}
	if len(s.Forbid()) < len(s.Projects)*2+30 {
		t.Errorf("the set forbids %d words; its names are missing", len(s.Forbid()))
	}
}

// Each check of the labels can fail: an event changed to break each rule
// is caught.
func TestTheLabelChecksCanFail(t *testing.T) {
	first := func(s *Set, split, class string) *Event {
		for i := range s.Events {
			if e := &s.Events[i]; e.Split == split && e.Class == class && (class != "no_match" || len(e.Decoys) == 0) {
				return e
			}
		}
		t.Fatalf("no %s %s event", split, class)
		return nil
	}
	for name, breakIt := range map[string]func(s *Set){
		"a paraphrase that uses its item's word": func(s *Set) {
			e := first(s, "paraphrase", Positive)
			e.Text += " " + tokens(s.Item(e.Relevant[0]).Title)[0]
		},
		"a near miss that shares one word": func(s *Set) {
			e := first(s, "lexical", "near_miss")
			e.Text = "Rename the variable job to task, it reads better."
		},
		"a name that is a cue of two items": func(s *Set) {
			e := first(s, "name", Positive)
			name := nameOf(s.Item(e.Relevant[0]))
			for i := range s.Items {
				if nameOf(&s.Items[i]) == "" {
					s.Items[i].Cues = append(s.Items[i].Cues, name)
					return
				}
			}
		},
		"a name event without its name": func(s *Set) {
			e := first(s, "name", Positive)
			e.Text = "What should I know before touching it?"
		},
		"a name near miss with the name capitalized": func(s *Set) {
			e := first(s, "name", "near_miss")
			e.Text += " " + nameOf(s.Item(e.Decoys[0]))
		},
		"a sibling-only event whose decoy is its own project's": func(s *Set) {
			e := first(s, "lexical", "sibling_only")
			for _, it := range s.Items {
				if it.Project == e.Project {
					e.Decoys = []string{it.ID}
					return
				}
			}
		},
		"a no-match event that matches its own project": func(s *Set) {
			e := first(s, "lexical", "no_match")
			for _, it := range s.Items {
				if it.Project == e.Project {
					e.Text += " " + it.Title
					return
				}
			}
		},
		"a prompt that names a project": func(s *Set) {
			e := first(s, "lexical", Positive)
			e.Text += " in " + s.Projects[0].Name
		},
	} {
		s := loadCommitted(t)
		breakIt(s)
		if err := checkLabels(s); err == nil {
			t.Errorf("%s: the labels pass", name)
		}
	}
}

// copySet copies the committed set into a temporary folder.
func copySet(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	entries, err := os.ReadDir(committedSet)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		data, err := os.ReadFile(filepath.Join(committedSet, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, e.Name()), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// changeEvent changes the text of the first event of the half in the set
// at dir.
func changeEvent(t *testing.T, dir, half string) {
	t.Helper()
	file := filepath.Join(dir, EventsFile)
	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	s := loadCommitted(t)
	for _, e := range s.Events {
		if Half(e.ID) == half {
			old := fmt.Sprintf(`"id":%q,`, e.ID)
			if !bytes.Contains(data, []byte(old)) {
				t.Fatalf("event %s is not written as %s", e.ID, old)
			}
			i := bytes.Index(data, []byte(old))
			j := bytes.Index(data[i:], []byte(`"text":"`)) + i + len(`"text":"`)
			data = slices.Concat(data[:j], []byte("Changed. "), data[j:])
			break
		}
	}
	if err := os.WriteFile(file, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

// A changed event fails the halves' check, naming its half, until
// halves.sha256 changes with it.
func TestTheHalvesAreFrozen(t *testing.T) {
	s := loadCommitted(t)
	for _, half := range Halves {
		n := 0
		for _, e := range s.Events {
			if Half(e.ID) == half {
				n++
			}
		}
		if n < len(s.Events)/3 {
			t.Errorf("the %s half holds %d of %d events", half, n, len(s.Events))
		}
	}
	for _, half := range Halves {
		dir := copySet(t)
		changeEvent(t, dir, half)
		_, err := Load(dir)
		if err == nil || !strings.Contains(err.Error(), "the "+half+" half") {
			t.Fatalf("a changed %s event: %v", half, err)
		}
		changed, err := readLines[Event](filepath.Join(dir, EventsFile))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, HalvesFile), HalvesText(changed), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := Load(dir); err != nil {
			t.Fatalf("a changed %s event with its new hash: %v", half, err)
		}
	}
	if got, want := Half("ev-0001"), Half("ev-0001"); got != want || (got != Tuning && got != Heldout) {
		t.Errorf("Half is %q then %q", got, want)
	}
}

// writeSetFiles writes a set to dir as the generator does, with the
// halves' hashes of its events.
func writeSetFiles(t *testing.T, dir string, s *Set) {
	t.Helper()
	files := map[string][]byte{
		SetFile:      indented(t, meta{V: 1, Version: s.Version, Seed: s.Seed, Common: s.Common}),
		ProjectsFile: indented(t, projectsFile{Projects: s.Projects, Groups: s.Groups}),
		ItemsFile:    lines(t, s.Items),
		EventsFile:   lines(t, s.Events),
		HalvesFile:   HalvesText(s.Events),
	}
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// Load refuses a set that breaks a rule of its format, naming the file and
// line or the field, but never what a text held. Written back unchanged,
// the set loads.
func TestLoadRefusesABrokenSet(t *testing.T) {
	positive := func(s *Set) *Event {
		for i := range s.Events {
			if e := &s.Events[i]; e.Class == Positive && s.Item(e.Relevant[0]).Scope != ScopeAll {
				return e
			}
		}
		t.Fatal("no positive event")
		return nil
	}
	negative := func(s *Set) *Event {
		for i := range s.Events {
			if e := &s.Events[i]; e.Class != Positive {
				return e
			}
		}
		t.Fatal("no negative event")
		return nil
	}
	of := func(s *Set, kind string) *Item {
		return &s.Items[slices.IndexFunc(s.Items, func(it Item) bool { return it.Type == kind })]
	}
	cases := map[string]func(s *Set){
		"nothing changed":                 nil,
		"an item of a kind not the five":  func(s *Set) { s.Items[0].Type = "lesson" },
		"an item id outside the grammar":  func(s *Set) { s.Items[0].ID = "k12" },
		"an item listed twice":            func(s *Set) { s.Items = append(s.Items, s.Items[0]) },
		"a statement over 150 characters": func(s *Set) { s.Items[0].Statement = strings.Repeat("secretword ", 14) + "x" },
		"words on a fact":                 func(s *Set) { of(s, "fact").Words = "secretword" },
		"a rule without words":            func(s *Set) { of(s, "rule").Words = "" },
		"nine cues":                       func(s *Set) { s.Items[0].Cues = slices.Repeat([]string{"secretword"}, 9) },
		"an item of a missing project":    func(s *Set) { s.Items[0].Project = "p_000000000000" },
		"a relevant item that is missing": func(s *Set) { positive(s).Relevant = []string{"izzzzzzzzzzzzz"} },
		"a relevant item of another project": func(s *Set) {
			e := positive(s)
			for _, it := range s.Items {
				if it.Project != "" && it.Project != e.Project {
					e.Relevant = []string{it.ID}
					return
				}
			}
		},
		"a relevant item twice":             func(s *Set) { e := positive(s); e.Relevant = append(e.Relevant, e.Relevant[0]) },
		"a decoy that is relevant":          func(s *Set) { e := positive(s); e.Decoys = append(e.Decoys, e.Relevant[0]) },
		"a negative with a relevant item":   func(s *Set) { negative(s).Relevant = []string{s.Items[0].ID} },
		"a positive with no relevant item":  func(s *Set) { positive(s).Relevant = []string{} },
		"an event of an unknown split":      func(s *Set) { s.Events[0].Split = "fuzzy" },
		"an event of an unknown class":      func(s *Set) { s.Events[0].Class = "maybe" },
		"an event listed twice":             func(s *Set) { s.Events = append(s.Events, s.Events[0]) },
		"a group of one project":            func(s *Set) { s.Groups[0].Projects = s.Groups[0].Projects[:1] },
		"a project id outside the grammar":  func(s *Set) { s.Projects[0].ID = "p_X" },
		"a key that does not hold its name": func(s *Set) { s.Projects[0].Name = "secretword" },
		"a set of version 0":                func(s *Set) { s.Version = 0 },
	}
	for name, change := range cases {
		s := loadCommitted(t)
		if change != nil {
			change(s)
		}
		dir := t.TempDir()
		writeSetFiles(t, dir, s)
		_, err := Load(dir)
		switch {
		case change == nil && err != nil:
			t.Errorf("%s: %v", name, err)
		case change != nil && err == nil:
			t.Errorf("%s: the set loads", name)
		case err != nil && strings.Contains(err.Error(), "secretword"):
			t.Errorf("%s: the error shows a text: %v", name, err)
		}
	}
	for name, change := range map[string]func([]byte) []byte{
		"an unknown field":         func(b []byte) []byte { return bytes.Replace(b, []byte(`"type":`), []byte(`"colour":"x","type":`), 1) },
		"a part of a line at last": func(b []byte) []byte { return b[:len(b)-1] },
		"two values on a line":     func(b []byte) []byte { return bytes.Replace(b, []byte("}\n"), []byte("} {}\n"), 1) },
	} {
		dir := copySet(t)
		file := filepath.Join(dir, ItemsFile)
		data, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(file, change(data), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := Load(dir); err == nil || !strings.Contains(err.Error(), ItemsFile) {
			t.Errorf("%s: %v", name, err)
		}
	}
}
