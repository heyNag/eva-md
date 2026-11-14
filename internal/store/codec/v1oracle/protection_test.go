package v1oracle_test

import (
	"fmt"
	"math/rand/v2"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/heyNag/eva-md/internal/okfadapter"
	"github.com/heyNag/eva-md/internal/store/codec"
)

// coversSeed is TestProtectionCoversV1's seed when -seed is not given.
const coversSeed = 20261114

// reBare is a bare locked or verified line, the one form the codec reads
// by its value.
var reBare = regexp.MustCompile(`^(locked|verified):( .*)?$`)

// markLine is one generated line, with the lines under it.
type markLine struct {
	text, under string
}

// spaces are white space a key's line may hold: YAML's own, Unicode's,
// and U+FEFF.
var spaces = []string{" ", "\t", "\U000000a0", "\U00002003", "\U00003000", "\U0000202f", "\U0000feff", "\U00000085"}

// someSpace is white space one time in n, else none.
func someSpace(r *rand.Rand, n int) string {
	if r.IntN(n) != 0 {
		return ""
	}
	s := spaces[r.IntN(len(spaces))]
	if r.IntN(4) == 0 {
		s += spaces[r.IntN(len(spaces))]
	}
	return s
}

// word is locked, verified or a near miss, as a person may have typed it
// inside double quotes when quoted: a letter spelled by an escape, or an
// escape of white space around it, at times.
func word(r *rand.Rand, quoted bool) string {
	w := []string{"locked", "locked", "locked", "locked", "locked", "verified", "verified", "verified", "verified", "verified", "lockd", "Locked", "verifie", "lockedness"}[r.IntN(14)]
	if quoted && r.IntN(3) == 0 {
		i := r.IntN(len(w))
		esc := fmt.Sprintf("\\x%02x", w[i])
		if r.IntN(3) != 0 {
			esc = fmt.Sprintf("\\u%04x", w[i])
		}
		w = w[:i] + esc + w[i+1:]
	}
	if quoted && r.IntN(5) == 0 {
		esc := []string{"\\_", "\\t", "\\L", "\\P", "\\N", "\\u00a0", "\\ ", "\\q", "\\ud800", "\\x4"}[r.IntN(10)]
		if r.IntN(2) == 0 {
			w = esc + w
		} else {
			w += esc
		}
	}
	return w
}

// values are what may follow a key's colon, v1 locking on some.
var values = []string{" true", " true", " true", " true", " yes", ` "true"`, " On", " 'yes'", " TRUE", " false", " {by: river}", " [x]", "", " ~", " # c", " null", ` ""`, " x"}

// unders are lines a key may have under it.
var unders = []string{"", "", "", "\n  - x", "\n  by: river", "\n- x", "\n\tby: river", "\n\t- by: river", "\n  - by: river"}

// betweens are lines that may fall between a key and the lines under it,
// none with a colon outside quotes: v1 kept the key's block open past them.
var betweens = []string{"\n# by hand", "\nsee below", "\n? note", "\ngenrat\"d: x", "\n'a:b'", "\n&a x", "\n note", "\n"}

// keyLine is a line that names a mark, or nearly, in a form a person may
// have typed: white space before it, quotes matched, mismatched, unclosed
// or none, white space and escapes inside them, white space and a quote
// before the colon, zero to two colons, and a value.
func keyLine(r *rand.Rand) markLine {
	open, close := "", ""
	switch r.IntN(8) {
	case 0, 1:
		open, close = `"`, `"`
	case 2, 3:
		open, close = "'", "'"
	case 4:
		open, close = []string{`"`, "'"}[r.IntN(2)], []string{"'", `"`}[r.IntN(2)]
	case 5:
		open = []string{`"`, "'"}[r.IntN(2)]
	}
	inside := someSpace(r, 4) + word(r, open == `"`) + someSpace(r, 4)
	if r.IntN(10) == 0 {
		inside += ": by river"
	}
	l := someSpace(r, 3) + open + inside + close + someSpace(r, 3)
	if r.IntN(10) == 0 {
		l += []string{`"`, "'"}[r.IntN(2)]
	}
	switch n := r.IntN(12); {
	case n == 0:
		l += " " + strings.TrimLeft(values[r.IntN(len(values))], " ")
	case n == 1:
		l += ":" + []string{": x", " a: b", ":"}[r.IntN(3)]
	case n == 2:
		l += ":" + strings.TrimLeft(values[r.IntN(len(values))], " ")
	default:
		l += ":" + values[r.IntN(len(values))]
	}
	under := unders[r.IntN(len(unders))]
	if under != "" && r.IntN(3) == 0 {
		under = betweens[r.IntN(len(betweens))] + under
	}
	return markLine{l, under}
}

// otherLines are the keys around the marks, the bare marks among them.
var otherLines = []string{"type: thread", "title: Tide feed", "status: stable", "description: a feed", "stale_after: 2027-02-01",
	"locked: false", "locked: true", "verified: []", "verified: {}", "verified: {} # none yet", "verified: [] # none yet",
	"verified: ~ # later", "verified: null # later", "verified: # by river", "verified: ~", "verified: \"\"", "# a note", "", "see below", "? note"}

// coversDoc is a document of other keys with key lines placed among them,
// at times in a block indented whole, where a key line may be indented
// with it or not.
func coversDoc(r *rand.Rand) []markLine {
	var lines []markLine
	for range 1 + r.IntN(4) {
		lines = append(lines, markLine{text: otherLines[r.IntN(len(otherLines))]})
	}
	for range 1 + r.IntN(3) {
		at := r.IntN(len(lines) + 1)
		lines = slices.Insert(lines, at, keyLine(r))
	}
	if r.IntN(10) == 0 {
		for i := range lines {
			if r.IntN(4) != 0 {
				lines[i].text = "  " + lines[i].text
				lines[i].under = strings.ReplaceAll(lines[i].under, "\n", "\n  ")
			}
		}
	}
	return lines
}

// render is the document's bytes.
func render(lines []markLine) []byte {
	var b strings.Builder
	b.WriteString("---\n")
	for _, l := range lines {
		b.WriteString(l.text + l.under + "\n")
	}
	b.WriteString("---\n\n# Body\n")
	return []byte(b.String())
}

// replaced is lines with each that matches replaced by with(it).
func replaced(lines []markLine, match func(markLine) bool, with func(markLine) markLine) []markLine {
	out := slices.Clone(lines)
	for i, l := range out {
		if match(l) {
			out[i] = with(l)
		}
	}
	return out
}

// noMark is a key that names no mark, with nothing under it.
func noMark(markLine) markLine { return markLine{text: "status: stable"} }

// bareVerified is a bare verified with no value, the lines under the line
// kept: v1 read them under it still.
func bareVerified(l markLine) markLine { return markLine{"verified:", l.under} }

// bare reports a bare locked or verified line at the line's start.
func bare(l markLine) bool { return reBare.MatchString(l.text) }

// emptyVerified reports a bare verified line in one of the five empty
// forms SPEC.md section 12 names, which v1 read as verified and the format
// as no mark: {}, {} # x, [] # x, a null with a comment (~ # x or null # x),
// or only a comment.
func emptyVerified(l markLine) bool {
	v, ok := strings.CutPrefix(l.text, "verified:")
	if !ok || v != "" && v[0] != ' ' {
		return false
	}
	v = strings.TrimSpace(v)
	if v == "{}" || strings.HasPrefix(v, "#") {
		return true
	}
	val, _, ok := strings.Cut(v, " #")
	return ok && slices.Contains([]string{"{}", "[]", "~", "null"}, strings.TrimSpace(val))
}

// v1Protects reads b as v1 did: locked and verified.
func v1Protects(t *testing.T, w *okfadapter.Wiki, b []byte) (locked, verified bool) {
	t.Helper()
	pg := libraryRead(t, w, "p", b)
	return okfadapter.IsLocked(pg.Concept), okfadapter.IsVerified(pg)
}

// No document v1 read as locked or verified is read by the codec as
// neither that nor unreadable, but for a bare verified line in one of the
// empty forms SPEC.md section 12 names. 10,000 documents of key lines in
// the forms a person may type, among other keys, from a fixed seed or
// -seed; at least 2,000 of them v1 protects by a line other than the bare
// one.
func TestProtectionCoversV1(t *testing.T) {
	s := *seed
	if s == 0 {
		s = coversSeed
	}
	t.Logf("seed %d (go test -run %s -seed %d repeats it)", s, t.Name(), s)
	r := rand.New(rand.NewPCG(s, s))
	w := wiki(t)
	byOther, failed := 0, 0
	for i := range 10000 {
		lines := coversDoc(r)
		b := render(lines)
		locked, verified := v1Protects(t, w, b)
		p := codec.Read(b).Protection()
		var miss []string
		if locked && !p.Locked && !p.Unreadable {
			miss = append(miss, "locked")
		}
		if verified && !p.Verified && !p.Unreadable {
			// v1 read it as verified by an empty form's value alone.
			if _, still := v1Protects(t, w, render(replaced(lines, emptyVerified, bareVerified))); still {
				miss = append(miss, "verified")
			}
		}
		if len(miss) > 0 {
			t.Errorf("document %d: v1 reads it as %s, the codec as %+v:\n%s", i, strings.Join(miss, " and "), p, b)
			if failed++; failed == 5 {
				t.FailNow()
			}
		}
		if locked || verified {
			if !slices.ContainsFunc(lines, bare) {
				byOther++
			} else if l, v := v1Protects(t, w, render(replaced(lines, bare, noMark))); l || v {
				byOther++
			}
		}
	}
	t.Logf("v1 protects %d documents by a line other than the bare one", byOther)
	if byOther < 2000 {
		t.Errorf("v1 protects %d documents by a line other than the bare one, under 2,000; the generator is to make more", byOther)
	}
}
