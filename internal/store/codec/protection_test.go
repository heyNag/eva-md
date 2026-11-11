package codec

import (
	"math/rand/v2"
	"strings"
	"testing"

	"github.com/heyNag/eva-md/internal/store/profile"
)

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

// Lines that protect both ways where the profile checker reads less today
// (SPEC.md section 10): an alias with its colon right after its name, an
// explicit key whose node is a compact mapping, a flow collection as a key.
var bothBeyondProfile = []string{
	"*k: true", "*k:\ttrue", "*k:", "*k.x: true", "? note: x\n  locked: true\n: v", "? &k note: x\n  verified: [x]\n: v",
	"? \"a\": b\n: v", "? locked : false", "? verified: []", "[locked]: false", "{locked: true}: x", "[a, \"b]\"]: x",
}

// genFront makes a frontmatter of protection keys in generated forms and
// values, and the protection the format's section 6 gives it. beyond says
// it holds a form the profile checker does not read as the codec does.
func genFront(r *rand.Rand) (front string, want Protection, beyond bool) {
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
				lines = append(lines, pick(bothBeyondProfile))
				want.Locked, want.Verified, beyond = true, true, true
			case n == 2:
				// A tab before a key: YAML refuses the line, but a person
				// wrote a mark. U+FEFF before one: a reader may drop it.
				lead := pick([]string{"\t", "\xef\xbb\xbf", "\t\xef\xbb\xbf"})
				if r.IntN(2) == 0 {
					lines, want.Locked = append(lines, lead+"locked:"+pick(unlockValues)), true
				} else {
					lines, want.Verified = append(lines, lead+"verified:"+pick(unverifiedValues)), true
				}
				beyond = true
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
	return front, want, beyond
}

func formKey(form, key string) string {
	return strings.Replace(form, "%s", key, 1)
}

// A property test over generated values and key forms finds no misread:
// each document protects as the format says, and as the profile checker
// reads it in a store.
func TestProtectionProperty(t *testing.T) {
	r := rand.New(rand.NewPCG(3, 11))
	root := copyFixture(t)
	counts := map[Protection]int{}
	for n := range 4000 {
		front, want, beyond := genFront(r)
		b := []byte("---\n" + front + "---\nbody\n")
		d := Read(b)
		if d.Unreadable() != "" {
			t.Fatalf("generated an unreadable document: %s\n%s", d.Unreadable(), b)
		}
		got := d.Protection()
		counts[got]++
		if got != want {
			t.Errorf("protection %+v, want %+v, for\n%s", got, want, front)
		}
		if n%8 == 0 {
			put(t, root, hand, b)
			rep, err := profile.Check(root)
			if err != nil {
				t.Fatal(err)
			}
			if pp := rep.Protected[hand]; !beyond {
				agree(t, front, got, pp)
			} else if pp.Locked && !got.Locked || pp.Verified && !got.Verified {
				// Where the checker reads less, it never reads more.
				t.Errorf("%s: codec %+v, profile %+v", front, got, pp)
			}
		}
	}
	// Each verdict is reached often enough for the test to mean something.
	for _, p := range []Protection{{}, {Locked: true}, {Verified: true}, {Locked: true, Verified: true}} {
		if counts[p] < 100 {
			t.Errorf("%+v reached %d times", p, counts[p])
		}
	}
}

// Where the codec reads protection more strictly than the profile checker
// does today: each value is one YAML reads as a lock or a mark, or as a
// value that is not a bare false or an empty mark, so each protects.
// SPEC.md section 10 names these cases.
func TestProtectionBeyondProfile(t *testing.T) {
	cases := []struct {
		front string
		want  Protection
	}{
		// Only a space or a tab is white space to YAML: these values are
		// strings holding a no-break space, an em space or a vertical tab.
		{"locked:  false", Protection{Locked: true}},
		{"locked: false ", Protection{Locked: true}},
		{"locked: false  # c", Protection{Locked: true}},
		{"locked: false\v", Protection{Locked: true}},
		{"locked:  false", Protection{Locked: true}},
		{"verified:  []", Protection{Verified: true}},
		{"verified: ~ ", Protection{Verified: true}},
		// With no space after the colon the line is no key to YAML: the
		// codec cannot read it, so it protects.
		{"locked:false", Protection{Locked: true}},
		{"verified:[]", Protection{Verified: true}},
		// A block that is one flow mapping holds its keys on its lines.
		{"{type: fact, locked: false}", Protection{Locked: true, Verified: true}},
		{"# a note\n!!map {locked: true}", Protection{Locked: true, Verified: true}},
		{"&a\n{\n  locked: true\n}", Protection{Locked: true, Verified: true}},
		{"  { type: fact }", Protection{Locked: true, Verified: true}},
		// A YAML reader may break a line where the codec does not: a CR
		// alone, NEL, LS or PS.
		{"title: x\rlocked: true", Protection{Locked: true, Verified: true}},
		{"title: x\u0085locked: true", Protection{Locked: true, Verified: true}},
		{"title: x locked: true", Protection{Locked: true, Verified: true}},
		{"title: \"x \"", Protection{Locked: true, Verified: true}},
		{"title: x\r\r", Protection{Locked: true, Verified: true}},
		// A line that belongs to no key but reads as a protection key: one
		// indented less than a block indented whole.
		{"  type: fact\nlocked: false", Protection{Locked: true}},
		{"  type: fact\n verified: []", Protection{Verified: true}},
		{"    type: fact\n  \"locked\": false", Protection{Locked: true}},
		{"  type: fact\n<<: *x", Protection{Locked: true, Verified: true}},
		// An alias with its colon right after its name: libyaml reads the
		// alias k, then the colon, so k's scalar is the key.
		{"type: fact\ntitle: &k locked\n*k: true", Protection{Locked: true, Verified: true}},
		{"locked: false\nnote: &k locked\n*k: true", Protection{Locked: true, Verified: true}},
		{"title: &k verified\n*k: [x]", Protection{Locked: true, Verified: true}},
		{"*k:\ttrue", Protection{Locked: true, Verified: true}},
		{"*k:", Protection{Locked: true, Verified: true}},
		// A name past libyaml's alphabet: no key to it, but another reader
		// may take the name to the space.
		{"type: fact\n*k.x: true", Protection{Locked: true, Verified: true}},
		// An explicit key whose node is a compact mapping is no scalar.
		{"type: fact\n? note: x\n  locked: true\n: v", Protection{Locked: true, Verified: true}},
		{"type: fact\n? &k note: x\n  verified: [x]\n: v", Protection{Locked: true, Verified: true}},
		{"type: fact\n? \"a\": b\n: v", Protection{Locked: true, Verified: true}},
		{"type: fact\n? locked : false", Protection{Locked: true, Verified: true}},
		{"type: fact\n? verified: []", Protection{Locked: true, Verified: true}},
		// A flow collection as an implicit key is no scalar either.
		{"type: fact\n[locked]: false", Protection{Locked: true, Verified: true}},
		{"type: fact\n{locked: true}: x", Protection{Locked: true, Verified: true}},
		{"type: fact\n[a, \"b]\"]: x", Protection{Locked: true, Verified: true}},
		// A tab before a key: YAML refuses the line, and a person wrote a
		// mark.
		{"title: x\n\tlocked: true", Protection{Locked: true}},
		{"\tlocked: true", Protection{Locked: true}},
		{"type: fact\n\tlocked:\tfalse", Protection{Locked: true}},
		{"type: fact\n\t\"verified\": []", Protection{Verified: true}},
		// U+FEFF before a key: a reader handed the frontmatter alone drops
		// it where the stream starts and reads the key.
		{"\xef\xbb\xbflocked: false", Protection{Locked: true}},
		{"\xef\xbb\xbf\xef\xbb\xbf  verified: []\ntype: fact", Protection{Verified: true}},
		{"type: fact\n\xef\xbb\xbf\tlocked: false", Protection{Locked: true}},
		{"\xef\xbb\xbf{type: fact, locked: false}", Protection{Locked: true, Verified: true}},
		{"\xef\xbb\xbf\n\xef\xbb\xbf# c\n\t\xef\xbb\xbf!!map {type: fact}", Protection{Locked: true, Verified: true}},
		// A key line with no space after its colon is a different key to
		// YAML, "verified:{locked"; it still protects as the key it shows.
		{"verified:{locked: true}", Protection{Verified: true}},
		{"locked:{x: y}", Protection{Locked: true}},
	}
	for _, c := range cases {
		if got := Read([]byte("---\n" + c.front + "\n---\nbody\n")).Protection(); got != c.want {
			t.Errorf("%q: %+v, want %+v", c.front, got, c.want)
		}
	}
	// What stays unprotected: a bare false and an empty mark with spaces
	// or tabs around them, a flow mapping under a key, a comment.
	for _, front := range []string{"locked:\tfalse\t", "locked: false  # c", "verified:\t[]\t", "title: {a: b}\nlocked: false", "# {locked: true}\ntype: fact", "title: x\r\nlocked: false\r",
		"  # locked: true\n  type: fact", "  type: fact\n# locked: true", "  type: fact\nlockedness: true",
		// A comment before the first key does not set the top level.
		"  # note\ntype: fact\nlocked: false",
		// A tab-only line is blank; a flow collection with no colon after
		// it, or as a value, is no key.
		"locked: false\n\t", "title: [locked, verified]\n[a]", "title: {locked: true}"} {
		if got := Read([]byte("---\n" + front + "\n---\n")).Protection(); got != (Protection{}) {
			t.Errorf("%q: %+v, want none", front, got)
		}
	}
}
