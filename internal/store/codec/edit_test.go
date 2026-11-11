package codec

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"math/rand/v2"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/heyNag/eva-md/internal/store/profile"
)

// genString makes a string from runes that matter to YAML, JSON and
// Markdown, or one in the plain alphabet.
func genString(r *rand.Rand) string {
	if r.IntN(3) == 0 {
		const first, rest = "abcxyz", "abz09_.-"
		s := string(first[r.IntN(len(first))])
		for range r.IntN(8) {
			s += string(rest[r.IntN(len(rest))])
		}
		if r.IntN(6) == 0 {
			s = []string{"yes", "no", "on", "off", "true", "false", "null", "y", "n"}[r.IntN(9)]
		}
		return s
	}
	parts := []string{"a", "Z", " ", ":", ", ", "#", " # ", `"`, "'", `\`, "\n", "\t", "<", ">", "&", "é", " ", "---", "## ", "[", "]", "{", "}", "-", "~", "*", "!", "%", "@", "`", "0", "1.5"}
	var b strings.Builder
	for range r.IntN(10) {
		b.WriteString(parts[r.IntN(len(parts))])
	}
	return b.String()
}

type ordered struct {
	Zeta  string `json:"zeta"`
	Alpha int    `json:"alpha"`
	Mid   []bool `json:"mid,omitempty"`
}

// genValue makes a string, a list or an object.
func genValue(r *rand.Rand, depth int) any {
	if depth > 2 {
		return genString(r)
	}
	switch r.IntN(6) {
	case 0, 1:
		return genString(r)
	case 2:
		var l []any
		for range r.IntN(4) {
			l = append(l, genLeaf(r, depth))
		}
		if l == nil {
			l = []any{}
		}
		return l
	case 3:
		m := map[string]any{}
		for range r.IntN(4) {
			m[genString(r)] = genLeaf(r, depth)
		}
		return m
	case 4:
		return ordered{Zeta: genString(r), Alpha: r.IntN(100) - 50, Mid: []bool{r.IntN(2) == 0}}
	default:
		return []string{genString(r), genString(r)}
	}
}

func genLeaf(r *rand.Rand, depth int) any {
	switch r.IntN(5) {
	case 0:
		return r.IntN(1000)
	case 1:
		return r.Float64()
	case 2:
		return nil
	case 3:
		return r.IntN(2) == 0
	default:
		return genValue(r, depth+1)
	}
}

// genBlock makes a list of one or more objects.
func genBlock(r *rand.Rand) any {
	if r.IntN(2) == 0 {
		return []ordered{{Zeta: genString(r), Alpha: 1}, {Zeta: genString(r)}}[:1+r.IntN(2)]
	}
	var l []map[string]any
	for range 1 + r.IntN(3) {
		l = append(l, map[string]any{"id": genString(r), "n": r.IntN(9), "x": genLeaf(r, 1)})
	}
	return l
}

// normal is what Value gives back for v: v through JSON, numbers kept as
// json.Number.
func normal(t *testing.T, v any) any {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.UseNumber()
	var out any
	if err := dec.Decode(&out); err != nil {
		t.Fatal(err)
	}
	return out
}

func genText(r *rand.Rand) string {
	if r.IntN(5) == 0 {
		return ""
	}
	lines := []string{"A line of text.[^e1]", "", "- a list item", "### a smaller heading", "[^e1]: claude-code:x#1", "`code`", "#tag", "--- a rule", "  indented"}
	var b strings.Builder
	for range 1 + r.IntN(4) {
		b.WriteString(lines[r.IntN(len(lines))] + "\n")
	}
	return b.String()
}

// editable returns the documents a property test edits: the readable,
// unlocked documents of the corpus and the fixture store.
func editable(t *testing.T) map[string][]byte {
	docs := map[string][]byte{}
	for name, b := range corpus(t) {
		if p := Read(b).Protection(); !p.Locked && !p.Unreadable {
			docs[name] = b
		}
	}
	filepath.WalkDir(fixture, func(p string, de fs.DirEntry, err error) error {
		if err != nil {
			t.Fatal(err)
		}
		rel, _ := filepath.Rel(fixture, p)
		if de.IsDir() || !isDocument(filepath.ToSlash(rel)) {
			return nil
		}
		b, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		if p := Read(b).Protection(); !p.Locked && !p.Unreadable {
			docs[filepath.ToSlash(rel)] = b
		}
		return nil
	})
	if len(docs) < 20 {
		t.Fatalf("only %d documents to edit", len(docs))
	}
	return docs
}

// keyLines are the frontmatter lines of d with what they are, but those of
// key's entries.
func keyLines(d *Doc, key string) []string {
	return d.others(key)
}

// After one edit to a readable, unlocked document, every byte outside the
// edited entry or section is unchanged and Read gives back what was set.
func TestEditProperty(t *testing.T) {
	r := rand.New(rand.NewPCG(5, 8))
	docs := editable(t)
	names := slices.Sorted(func(yield func(string) bool) {
		for n := range docs {
			if !yield(n) {
				return
			}
		}
	})
	done := map[string]int{}
	for range 6000 {
		name := names[r.IntN(len(names))]
		old := docs[name]
		d := Read(old)
		var keys []string
		for _, e := range d.entries {
			keys = append(keys, e.key)
		}
		var heads []string
		for _, s := range d.sections {
			heads = append(heads, s.heading)
		}
		pickKey := func() string {
			if len(keys) > 0 && r.IntN(2) == 0 {
				return keys[r.IntN(len(keys))]
			}
			return []string{"title", "cues", "sources", "verified", "description", "new_key", "anchors"}[r.IntN(7)]
		}
		pickHead := func() string {
			if len(heads) > 0 && r.IntN(2) == 0 {
				return heads[r.IntN(len(heads))]
			}
			return []string{"Detail", "Evidence", "Log", "Next steps"}[r.IntN(4)]
		}
		op := r.IntN(6)
		var err error
		var key, head, txt string
		var val any
		switch op {
		case 0:
			key, val = pickKey(), genValue(r, 0)
			err = d.Set(key, val)
		case 1:
			key, val = pickKey(), genBlock(r)
			err = d.SetBlock(key, val)
		case 2:
			key = pickKey()
			err = d.Delete(key)
		case 3:
			head, txt = pickHead(), genText(r)
			err = d.SetSection(head, txt)
		case 4:
			txt = genText(r)
			err = d.SetPreamble(txt)
		case 5:
			head = pickHead()
			err = d.RemoveSection(head)
		}
		what := fmt.Sprintf("%s: op %d key %q heading %q", name, op, key, head)
		if err != nil {
			if got, _ := Write(d); !bytes.Equal(got, old) {
				t.Fatalf("%s: refused with %v, and changed the document", what, err)
			}
			old := Read(old)
			switch {
			case op <= 2 && key == "locked" && errors.Is(err, ErrProtected):
			case op <= 2 && len(old.find(key)) > 1 && errors.Is(err, ErrRepeated):
			case op <= 2 && old.base != 0 && errors.Is(err, ErrNotWritten):
			case op == 2 && len(old.find(key)) == 0 && errors.Is(err, ErrNoKey):
			case (op == 3 || op == 5) && countHeads(heads, head) > 1 && errors.Is(err, ErrRepeated):
			case op == 5 && countHeads(heads, head) == 0 && errors.Is(err, ErrNoKey):
			default:
				t.Fatalf("%s: unexpected refusal %v", what, err)
			}
			done["refused"]++
			continue
		}
		done[fmt.Sprint(op)]++
		if op <= 2 {
			checkKeyEdit(t, what, old, d, key, val, op)
		} else {
			checkBodyEdit(t, what, old, d, op, head, txt)
		}
	}
	for op := range 6 {
		if done[fmt.Sprint(op)] < 300 {
			t.Errorf("op %d succeeded only %d times: %v", op, done[fmt.Sprint(op)], done)
		}
	}
}

func countHeads(heads []string, h string) int {
	n := 0
	for _, x := range heads {
		if x == h {
			n++
		}
	}
	return n
}

func checkKeyEdit(t *testing.T, what string, old []byte, d *Doc, key string, val any, op int) {
	t.Helper()
	b, err := Write(d)
	if err != nil {
		t.Fatal(err)
	}
	o, n := Read(old), Read(b)
	// Every other line of the frontmatter, and the whole body, is as it was.
	if !slices.Equal(keyLines(o, key), keyLines(n, key)) {
		t.Fatalf("%s: a line outside the entry changed:\n%s\n----\n%s", what, old, b)
	}
	if strings.Join(o.lines[o.close:], "") != strings.Join(n.lines[n.close:], "") {
		t.Fatalf("%s: the body changed", what)
	}
	for _, e := range o.entries {
		if e.key == key {
			continue
		}
		v1, err1 := o.Value(e.key)
		v2, err2 := n.Value(e.key)
		if !reflect.DeepEqual(v1, v2) || err1 != err2 {
			t.Fatalf("%s: the value of %s changed", what, e.key)
		}
	}
	po, pn := o.Protection(), n.Protection()
	if key == "verified" {
		po.Verified, pn.Verified = false, false
	}
	if po != pn {
		t.Fatalf("%s: protection changed from %+v to %+v", what, o.Protection(), n.Protection())
	}
	// Read gives back the value set.
	got, err := n.Value(key)
	switch op {
	case 2:
		if err != ErrNoKey {
			t.Fatalf("%s: deleted, then Value says %v", what, err)
		}
	default:
		want := normal(t, val)
		if l, ok := want.([]any); ok && op == 1 {
			want = l
		}
		if err != nil || !reflect.DeepEqual(got, want) {
			t.Fatalf("%s: Value gives %#v (%v), want %#v", what, got, err, want)
		}
	}
	// A new key goes after the last entry.
	if op != 2 && len(o.find(key)) == 0 && n.entries[len(n.entries)-1].key != key {
		t.Fatalf("%s: a new key is not after the last entry", what)
	}
}

func checkBodyEdit(t *testing.T, what string, old []byte, d *Doc, op int, head, txt string) {
	t.Helper()
	b, err := Write(d)
	if err != nil {
		t.Fatal(err)
	}
	o := Read(old)
	// The range of bytes the edit names, and what it holds after.
	offset := func(line int) int { return len(strings.Join(o.lines[:line], "")) }
	next := func(i int) int {
		if i+1 < len(o.sections) {
			return offset(o.sections[i+1].at)
		}
		return len(old)
	}
	var s, e int
	var mid string
	switch op {
	case 3:
		s, e, mid = len(old), len(old), "## "+head+"\n"+txt
		for i, sec := range o.sections {
			if sec.heading == head {
				s, e, mid = offset(sec.at+1), next(i), txt
			}
		}
	case 4:
		s, e, mid = offset(o.close+1), len(old), txt
		if len(o.sections) > 0 {
			e = offset(o.sections[0].at)
		}
	case 5:
		for i, sec := range o.sections {
			if sec.heading == head {
				s, e = offset(sec.at), next(i)
			}
		}
	}
	prefix := string(old[:s])
	if s == len(old) && !strings.HasSuffix(prefix, "\n") && mid != "" {
		prefix += "\n"
	}
	if want := prefix + mid + string(old[e:]); string(b) != want {
		t.Fatalf("%s: wrote\n%q\nwant\n%q", what, b, want)
	}
	n := Read(b)
	switch op {
	case 3:
		if got, err := n.Section(head); err != nil || got != txt {
			t.Fatalf("%s: Section gives %q (%v), want %q", what, got, err, txt)
		}
	case 4:
		if got := n.Preamble(); got != txt {
			t.Fatalf("%s: Preamble gives %q, want %q", what, got, txt)
		}
	case 5:
		if _, err := n.Section(head); err != ErrNoKey {
			t.Fatalf("%s: removed, then Section says %v", what, err)
		}
	}
	if n.Protection() != o.Protection() {
		t.Fatalf("%s: protection changed", what)
	}
}

// Setting each key an item may hold, with generated values, on a copy of
// a fixture item leaves the profile checker with no written-form or
// frontmatter fault for it.
func TestSetEachItemKey(t *testing.T) {
	keys := []string{"type", "id", "title", "description", "status", "state", "state_at", "state_reason", "scope", "project", "workstream",
		"tag", "words", "generated", "learned", "asserted_at", "confirmed", "supersedes", "branch", "anchors", "cues", "sources",
		"migrated_from", "verified"}
	r := rand.New(rand.NewPCG(7, 1))
	root := copyFixture(t)
	orig, err := os.ReadFile(filepath.Join(fixture, fact))
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range keys {
		for i := range 12 {
			d := Read(orig)
			var err error
			if i%3 == 2 {
				err = d.SetBlock(key, genBlock(r))
			} else {
				err = d.Set(key, genValue(r, 0))
			}
			if err != nil {
				t.Fatalf("%s: %v", key, err)
			}
			b, _ := Write(d)
			put(t, root, fact, b)
			rep, err := profile.Check(root)
			if err != nil {
				t.Fatal(err)
			}
			for _, f := range rep.Faults {
				if f.Path == fact && (f.Rule == "written-form" || f.Rule == "frontmatter") {
					t.Errorf("%s: %v\n%s", key, f, b)
				}
			}
		}
	}
	if err := Read(orig).Set("locked", "x"); !errors.Is(err, ErrProtected) {
		t.Errorf("Set of locked: %v", err)
	}
}
