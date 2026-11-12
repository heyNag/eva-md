// The tests that run the profile checker are outside the package, as the
// checker reads through the codec. They hold that the two give the same
// protection: the checker has no reader of its own.
package codec_test

import (
	"errors"
	"math/rand/v2"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/heyNag/eva-md/internal/store/codec"
	"github.com/heyNag/eva-md/internal/store/profile"
)

// Protection is the codec's, which the tests compare with the checker's.
type Protection = codec.Protection

// Values YAML reads as false, which a bare locked key may hold unlocked.
var unlockValues = []string{" false", " False", " FALSE", "  false", " false  ", " false # c", " False  # not now", "\tfalse"}

// Values that lock: anything but a bare false, as YAML might read it or
// as the codec cannot.
var lockValues = []string{
	" true", " True", " yes", " no", " off", " on", " 0", " 1", ` "false"`, " 'false'", "", " ", " # only a comment",
	" false#c", " false\t# c", " [false]", " {}", " ~", " null", " !!bool false", " &a false", " *a", " fals",
	" false false", " FALSe", " false,", ` "\x66alse"`, "\n  false", " false\n  - x", "\n  - false",
}

// Values a bare verified key may hold unverified.
var unverifiedValues = []string{"", " ", " []", " {}", ` ""`, " null", " ~", " [] # none yet", " ~  # c", " null # c"}

// Values that verify.
var verifiedValues = []string{
	" true", " false", " no", " x", ` "x"`, ` "" # c`, " [x]", " {a: b}", ` [{"by": "human:user", "at": "2026-11-06T09:00:00Z"}]`,
	" [ ]", " { }", " Null", " NULL", " 'null'", "\n  - by: human:user", " !!null", " &a []",
}

// Forms of a key other than the bare one at the line's start; %s is the
// key. Each is read as the key, so it protects whatever its value.
var keyForms = []string{
	`"%s":`, `'%s':`, `%s :`, "%s\t:", "? %s\n:", "!!str %s:", "&a %s:", "&a !!str '%s' :", `"%s" :`, "? !!str %s\n:",
}

// Keys that look like a protection key but are not one.
var otherLines = []string{
	`title: "x"`, "lockedness: true", "Locked: true", "VERIFIED: [1]", "verified_by: x", "# locked: true",
	"note:\n  locked: true", "note:\n  verified: [x]", "",
}

// Lines that protect both ways, which the profile checker's own reader,
// before it read through the codec, read less (SPEC.md section 10 as it
// was): an alias with its colon right after its name, an explicit key
// whose node is a compact mapping, a flow collection as a key.
var bothKeys = []string{
	"*k: true", "*k:\ttrue", "*k:", "*k.x: true", "? note: x\n  locked: true\n: v", "? &k note: x\n  verified: [x]\n: v",
	"? \"a\": b\n: v", "? locked : false", "? verified: []", "[locked]: false", "{locked: true}: x", "[a, \"b]\"]: x",
}

// genFront makes a frontmatter of protection keys in generated forms and
// values, and the protection the format's section 6 gives it.
func genFront(r *rand.Rand) (front string, want Protection) {
	var lines []string
	nLocked := 0
	slots := r.IntN(6) + 1
	indent := r.IntN(10) == 0
	pick := func(s []string) string { return s[r.IntN(len(s))] }
	for range slots {
		switch r.IntN(5) {
		case 0, 1:
			nLocked++
			form := r.IntN(3) == 0
			if form || indent {
				want.Locked = true
			}
			var v string
			if r.IntN(2) == 0 {
				v = pick(unlockValues)
			} else {
				v, want.Locked = pick(lockValues), true
			}
			if form {
				lines = append(lines, formKey(pick(keyForms), "locked")+v)
			} else {
				lines = append(lines, "locked:"+v)
			}
		case 2:
			form := r.IntN(3) == 0
			if form || indent {
				want.Verified = true
			}
			var v string
			if r.IntN(2) == 0 {
				v = pick(unverifiedValues)
			} else {
				v, want.Verified = pick(verifiedValues), true
			}
			if form {
				lines = append(lines, formKey(pick(keyForms), "verified")+v)
			} else {
				lines = append(lines, "verified:"+v)
			}
		case 3:
			lines = append(lines, pick(otherLines))
		case 4:
			switch n := r.IntN(16); {
			case n == 0:
				lines = append(lines, pick([]string{"*k : true", "<<: *defaults", `<<: {"locked": true}`, "? [locked]\n: false", "?\n  locked\n: false"}))
				want.Locked, want.Verified = true, true
			case n == 1:
				lines = append(lines, pick(bothKeys))
				want.Locked, want.Verified = true, true
			case n == 2:
				// A tab before a key: YAML refuses the line, but a person
				// wrote a mark. U+FEFF before one: a reader may drop it.
				lead := pick([]string{"\t", "\xef\xbb\xbf", "\t\xef\xbb\xbf"})
				if r.IntN(2) == 0 {
					lines, want.Locked = append(lines, lead+"locked:"+pick(unlockValues)), true
				} else {
					lines, want.Verified = append(lines, lead+"verified:"+pick(unverifiedValues)), true
				}
			default:
				lines = append(lines, "status: stable")
			}
		}
	}
	if nLocked > 1 {
		want.Locked = true
	}
	front = "type: fact\n" + strings.Join(lines, "\n") + "\n"
	if indent {
		front = "  " + strings.ReplaceAll(strings.TrimSuffix(front, "\n"), "\n", "\n  ") + "\n"
	}
	return front, want
}

func formKey(form, key string) string {
	return strings.Replace(form, "%s", key, 1)
}

// checked is the protection the profile checker reports for b placed at
// rel in the store at root.
func checked(t *testing.T, root, rel string, b []byte) profile.Protection {
	t.Helper()
	codec.Put(t, root, rel, b)
	rep, err := profile.Check(root)
	if err != nil {
		t.Fatal(err)
	}
	return rep.Protected[rel]
}

// place is a path a document is checked at, in a copy of the fixture.
type place struct {
	root, rel string
	listed    bool // the hand-edit record lists it
}

// places are the paths a document is checked at: an item the hand-edit
// record lists and one it does not, and a session, a project record and a
// disagreement, which may carry no protection, each both listed and not.
// The checker reports protection wherever it is, so it must equal the
// codec's at each.
func places(t *testing.T) []place {
	t.Helper()
	plain, listed := codec.CopyFixture(t), codec.CopyFixture(t)
	others := []string{codec.Session, codec.Project, codec.Disagreement}
	rec, err := os.ReadFile(filepath.Join(listed, codec.HandEdits))
	if err != nil {
		t.Fatal(err)
	}
	for _, rel := range others {
		rec = append(rec, rel+"\t2026-11-10T10:00:00+01:00\tacknowledged\n"...)
	}
	codec.Put(t, listed, codec.HandEdits, rec)
	ps := []place{{plain, codec.Hand, true}, {plain, codec.Fact, false}}
	for _, rel := range others {
		ps = append(ps, place{plain, rel, false}, place{listed, rel, true})
	}
	return ps
}

func (p place) String() string {
	if p.listed {
		return p.rel + ", hand-edited"
	}
	return p.rel
}

// A property test over generated values and key forms finds no misread:
// each document protects as the format says, and the profile checker
// reports the same protection for it in a store.
func TestProtectionProperty(t *testing.T) {
	r := rand.New(rand.NewPCG(3, 11))
	ps := places(t)
	counts := map[Protection]int{}
	for n := range 4000 {
		front, want := genFront(r)
		b := []byte("---\n" + front + "---\nbody\n")
		d := codec.Read(b)
		if d.Unreadable() != "" {
			t.Fatalf("generated an unreadable document: %s\n%s", d.Unreadable(), b)
		}
		got := d.Protection()
		counts[got]++
		if got != want {
			t.Errorf("protection %+v, want %+v, for\n%s", got, want, front)
		}
		if n%8 == 0 {
			p := ps[n/8%len(ps)]
			agree(t, front+" at "+p.String(), got, checked(t, p.root, p.rel, b))
		}
	}
	// Each verdict is reached often enough for the test to mean something.
	for _, p := range []Protection{{}, {Locked: true}, {Verified: true}, {Locked: true, Verified: true}} {
		if counts[p] < 100 {
			t.Errorf("%+v reached %d times", p, counts[p])
		}
	}
}

// beyond holds, for each of the nine cases SPEC.md section 10 listed where
// the profile checker's own reader saw no protection and the codec did,
// documents of that case: each value is one YAML reads as a lock or a
// mark, or as a value that is not a bare false or an empty mark.
var beyond = []struct {
	n     int // the case, in section 10's order
	front string
	want  Protection
}{
	// 1. Only a space or a tab is white space to YAML: these values are
	// strings holding a no-break space, an em space or a vertical tab.
	{1, "locked: \u00a0false", Protection{Locked: true}},
	{1, "locked: false\u00a0", Protection{Locked: true}},
	{1, "locked: false\u00a0 # c", Protection{Locked: true}},
	{1, "locked: false\v", Protection{Locked: true}},
	{1, "locked: \u2003false", Protection{Locked: true}},
	{1, "locked:\u00a0false", Protection{Locked: true}},
	{1, "verified: \u00a0[]", Protection{Verified: true}},
	{1, "verified: ~\u00a0", Protection{Verified: true}},
	// 2. With no space after the colon the line is no key to YAML: the
	// codec cannot read it, so it protects.
	{2, "locked:false", Protection{Locked: true}},
	{2, "verified:[]", Protection{Verified: true}},
	{2, "verified:{locked: true}", Protection{Verified: true}},
	{2, "locked:{x: y}", Protection{Locked: true}},
	// 3. A block that is one flow mapping holds its keys on its lines.
	{3, "{type: fact, locked: false}", Protection{Locked: true, Verified: true}},
	{3, "# a note\n!!map {locked: true}", Protection{Locked: true, Verified: true}},
	{3, "&a\n{\n  locked: true\n}", Protection{Locked: true, Verified: true}},
	{3, "  { type: fact }", Protection{Locked: true, Verified: true}},
	// 4. A YAML reader may break a line where the codec does not: a CR
	// alone, NEL, LS or PS.
	{4, "title: x\rlocked: true", Protection{Locked: true, Verified: true}},
	{4, "title: x\u0085locked: true", Protection{Locked: true, Verified: true}},
	{4, "title: x\u2028locked: true", Protection{Locked: true, Verified: true}},
	{4, "title: \"x\u2029\"", Protection{Locked: true, Verified: true}},
	{4, "title: x\r\r", Protection{Locked: true, Verified: true}},
	// 5. A line that belongs to no key but reads as a protection key: one
	// indented less than a block indented whole.
	{5, "  type: fact\nlocked: false", Protection{Locked: true}},
	{5, "  type: fact\n verified: []", Protection{Verified: true}},
	{5, "    type: fact\n  \"locked\": false", Protection{Locked: true}},
	{5, "  type: fact\n<<: *x", Protection{Locked: true, Verified: true}},
	// 6. An alias with its colon right after its name: libyaml reads the
	// alias k, then the colon, so k's scalar is the key. A name past
	// libyaml's alphabet: no key to it, but another reader may take the
	// name to the space.
	{6, "type: fact\ntitle: &k locked\n*k: true", Protection{Locked: true, Verified: true}},
	{6, "locked: false\nnote: &k locked\n*k: true", Protection{Locked: true, Verified: true}},
	{6, "title: &k verified\n*k: [x]", Protection{Locked: true, Verified: true}},
	{6, "*k:\ttrue", Protection{Locked: true, Verified: true}},
	{6, "*k:", Protection{Locked: true, Verified: true}},
	{6, "type: fact\n*k.x: true", Protection{Locked: true, Verified: true}},
	// 7. An explicit key whose node is a compact mapping is no scalar.
	{7, "type: fact\n? note: x\n  locked: true\n: v", Protection{Locked: true, Verified: true}},
	{7, "type: fact\n? &k note: x\n  verified: [x]\n: v", Protection{Locked: true, Verified: true}},
	{7, "type: fact\n? \"a\": b\n: v", Protection{Locked: true, Verified: true}},
	{7, "type: fact\n? locked : false", Protection{Locked: true, Verified: true}},
	{7, "type: fact\n? verified: []", Protection{Locked: true, Verified: true}},
	// 8. A flow collection as an implicit key is no scalar either.
	{8, "type: fact\n[locked]: false", Protection{Locked: true, Verified: true}},
	{8, "type: fact\n{locked: true}: x", Protection{Locked: true, Verified: true}},
	{8, "type: fact\n[a, \"b]\"]: x", Protection{Locked: true, Verified: true}},
	// 9. A tab before a key: YAML refuses the line, and a person wrote a
	// mark. U+FEFF before a key: a reader handed the frontmatter alone
	// drops it where the stream starts and reads the key.
	{9, "title: x\n\tlocked: true", Protection{Locked: true}},
	{9, "\tlocked: true", Protection{Locked: true}},
	{9, "type: fact\n\tlocked:\tfalse", Protection{Locked: true}},
	{9, "type: fact\n\t\"verified\": []", Protection{Verified: true}},
	{9, "\xef\xbb\xbflocked: false", Protection{Locked: true}},
	{9, "\xef\xbb\xbf\xef\xbb\xbf  verified: []\ntype: fact", Protection{Verified: true}},
	{9, "type: fact\n\xef\xbb\xbf\tlocked: false", Protection{Locked: true}},
	{9, "\xef\xbb\xbf{type: fact, locked: false}", Protection{Locked: true, Verified: true}},
	{9, "\xef\xbb\xbf\n\xef\xbb\xbf# c\n\t\xef\xbb\xbf!!map {type: fact}", Protection{Locked: true, Verified: true}},
}

// What stays unprotected: a bare false and an empty mark with spaces or
// tabs around them, a flow mapping under a key, a comment.
var unprotected = []string{
	"locked:\tfalse\t", "locked: false  # c", "verified:\t[]\t", "title: {a: b}\nlocked: false", "# {locked: true}\ntype: fact", "title: x\r\nlocked: false\r",
	"  # locked: true\n  type: fact", "  type: fact\n# locked: true", "  type: fact\nlockedness: true",
	// A comment before the first key does not set the top level.
	"  # note\ntype: fact\nlocked: false",
	// A tab-only line is blank; a flow collection with no colon after it,
	// or as a value, is no key.
	"locked: false\n\t", "title: [locked, verified]\n[a]", "title: {locked: true}",
}

// Each case SPEC.md section 10 listed, where the checker's own reader saw
// no protection, protects in the codec, and the profile checker reports
// the same protection for it in a store, both in a file the hand-edit
// record lists and in one it does not, and at a session, a project record
// and a disagreement, listed and not, where none may be carried; so do the
// documents that protect nothing.
func TestProtectionBeyondProfile(t *testing.T) {
	ps := places(t)
	seen := map[int]bool{}
	for _, c := range beyond {
		seen[c.n] = true
		b := []byte("---\n" + c.front + "\n---\nbody\n")
		got := codec.Read(b).Protection()
		if got != c.want {
			t.Errorf("case %d, %q: %+v, want %+v", c.n, c.front, got, c.want)
		}
		for _, p := range ps {
			agree(t, c.front+" at "+p.String(), got, checked(t, p.root, p.rel, b))
		}
	}
	for n := 1; n <= 9; n++ {
		if !seen[n] {
			t.Errorf("no document of case %d", n)
		}
	}
	for _, front := range unprotected {
		b := []byte("---\n" + front + "\n---\n")
		got := codec.Read(b).Protection()
		if got != (Protection{}) {
			t.Errorf("%q: %+v, want none", front, got)
		}
		for _, p := range ps {
			agree(t, front+" at "+p.String(), got, checked(t, p.root, p.rel, b))
		}
	}
}

// The codec and the profile checker read protection alike, on every
// document of the fixture store and on each corpus case placed in a copy
// of it.
func TestProtectionAgreesWithProfile(t *testing.T) {
	rep, err := profile.Check(codec.Fixture)
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	err = filepath.WalkDir(codec.Fixture, func(p string, de os.DirEntry, err error) error {
		if err != nil || de.IsDir() {
			return err
		}
		rel, _ := filepath.Rel(codec.Fixture, p)
		rel = filepath.ToSlash(rel)
		if !codec.IsDocument(rel) {
			return nil
		}
		n++
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		agree(t, rel, codec.Read(b).Protection(), rep.Protected[rel])
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if n < 15 {
		t.Fatalf("read %d documents of the fixture", n)
	}
	root := codec.CopyFixture(t)
	for name, b := range codec.Corpus(t) {
		for _, p := range []string{codec.Fact, codec.Hand, "systems/orders.md"} {
			agree(t, name+" at "+p, codec.Read(b).Protection(), checked(t, root, p, b))
		}
		codec.Restore(t, root, codec.Fact, codec.Hand, "systems/orders.md")
	}
}

func agree(t *testing.T, what string, got Protection, want profile.Protection) {
	t.Helper()
	if got.Locked != want.Locked || got.Verified != want.Verified || got.Unreadable != want.Unreadable {
		t.Errorf("%s: codec %+v, profile %+v", what, got, want)
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
	root := codec.CopyFixture(t)
	orig, err := os.ReadFile(filepath.Join(codec.Fixture, codec.Fact))
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range keys {
		for i := range 12 {
			d := codec.Read(orig)
			var err error
			if i%3 == 2 {
				err = d.SetBlock(key, codec.GenBlock(r))
			} else {
				err = d.Set(key, codec.GenValue(r, 0))
			}
			if err != nil {
				t.Fatalf("%s: %v", key, err)
			}
			b, _ := codec.Write(d)
			codec.Put(t, root, codec.Fact, b)
			rep, err := profile.Check(root)
			if err != nil {
				t.Fatal(err)
			}
			for _, f := range rep.Faults {
				if f.Path == codec.Fact && (f.Rule == "written-form" || f.Rule == "frontmatter") {
					t.Errorf("%s: %v\n%s", key, f, b)
				}
			}
		}
	}
	if err := codec.Read(orig).Set("locked", "x"); !errors.Is(err, codec.ErrProtected) {
		t.Errorf("Set of locked: %v", err)
	}
}
