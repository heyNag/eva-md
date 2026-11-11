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

// genFront makes a frontmatter of protection keys in generated forms and
// values, and the protection the format's section 6 gives it.
func genFront(r *rand.Rand) (string, Protection) {
	var lines []string
	var want Protection
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
			if r.IntN(8) == 0 {
				lines = append(lines, pick([]string{"*k : true", "<<: *defaults", `<<: {"locked": true}`, "? [locked]\n: false", "?\n  locked\n: false"}))
				want.Locked, want.Verified = true, true
			} else {
				lines = append(lines, "status: stable")
			}
		}
	}
	if nLocked > 1 {
		want.Locked = true
	}
	front := "type: fact\n" + strings.Join(lines, "\n") + "\n"
	if indent {
		front = "  " + strings.ReplaceAll(strings.TrimSuffix(front, "\n"), "\n", "\n  ") + "\n"
	}
	return front, want
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
		front, want := genFront(r)
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
			agree(t, front, got, rep.Protected[hand])
		}
	}
	// Each verdict is reached often enough for the test to mean something.
	for _, p := range []Protection{{}, {Locked: true}, {Verified: true}, {Locked: true, Verified: true}} {
		if counts[p] < 100 {
			t.Errorf("%+v reached %d times", p, counts[p])
		}
	}
}
