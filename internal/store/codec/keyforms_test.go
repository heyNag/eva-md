package codec

import (
	"errors"
	"strings"
	"testing"
)

// keyFormRow is a frontmatter line and the protection it gives in a
// document of other keys.
type keyFormRow struct {
	name, line string
	want       Protection
}

// newKeyForms are lines that name a mark as v1 or another reader more
// lenient than YAML read it, behind Unicode white space, quotes and
// escapes. Each protects as its mark, and none did before ST-03c.
func newKeyForms() []keyFormRow {
	lk, vf, both := Protection{Locked: true}, Protection{Verified: true}, Protection{Locked: true, Verified: true}
	rows := []keyFormRow{
		// The nine forms of SPEC.md section 12 that ReadV1 refuses.
		{"a space before the colon and none after", "locked :true", lk},
		{"a space before the colon of verified", "verified :{by: a}", vf},
		{"a quote after the key", "verified': x", vf},
		{"spaces inside the quotes", "'locked ': true", lk},
		{"a colon inside the quotes", "'verified: by river': yes", vf},
		{"a no-break space before the key", "\U000000a0locked: true", lk},
		{"a narrow no-break space before the colon", "verified\U0000202f: me", vf},
		{"a no-break space inside the quotes", "'locked\U000000a0 ': true", lk},
		{"a quote that never closes", `"verified: me`, both},
		// A letter spelled by an escape, after white space YAML does not
		// trim: the line is a plain key to the codec, and a quoted one to
		// a reader that trims first.
		{"an escaped letter after a no-break space", "\U000000a0\"lo\\u0063ked\": x", lk},
		{"an escaped letter after an ideographic space", "\U00003000\"verifi\\x65d\": [x]", vf},
		{"an escaped letter after an em space, a space after the quote", "\U00002003\"lock\\x65d\" : false", lk},
		// Escapes YAML reads as white space.
		{`"\_"`, `"locked\_": x`, lk},
		{`"\L"`, `"locked\L": x`, lk},
		{`"\P"`, `"verified\P": x`, vf},
		{`"\N"`, `"\Nverified": x`, vf},
		{`"\t"`, `"locked\t": false`, lk},
		{`"\u00a0"`, "\"\\u00a0locked\": false", lk},
		// An explicit key has no colon on its line: the key as read trims.
		{"an escaped no-break space in an explicit key", "? \"locked\\_\"\n: false", lk},
		{"spaces inside the quotes of an explicit key", "? ' verified '\n: []", vf},
		// Run-on and unclosed quotes.
		{"a double quote that runs past a colon", `"locked: no": x`, lk},
		{"a single quote that never closes", "'locked: false", both},
		{"a quote that never closes, after an em space", "\U00002003\"title: x", both},
		{"a quote that never closes, after U+FEFF", "\U0000feff'note: x", both},
		{"a doubled single quote that never closes", "'it''s: x", both},
		// Escapes YAML does not have, and escapes that are no character.
		{"an unknown escape in a key", `"lo\qcked": x`, both},
		{"an unknown escape in any key", `"title\q": x`, both},
		{"an unknown escape in an explicit key", "? \"lo\\qcked\"\n: x", both},
		{"an unknown escape in a tagged key", `!!str "lo\qcked": x`, both},
		{"a surrogate", `"locked\ud800": x`, both},
		{"a surrogate in any key", `"\uDFFF": x`, both},
		{"a short hex escape", `"lo\x6": x`, both},
		{"a short unicode escape", `"lo\u06": x`, both},
		{"an escape past the last code point", `"\U00110000": x`, both},
		{"an unknown escape after a tab", "\t\"lo\\qcked\": x", both},
		// A line under an empty bare verified that the codec does not give
		// it, as v1 read a key's block: open past blank lines and lines
		// with no colon outside quotes, a tab taken for indentation, and
		// each "-" item in it a verified entry.
		{"a tab-led line under an empty verified", "verified:\n\tby: river", vf},
		{"a tab-led line under a verified comment", "verified: # by river\n\tby: river", vf},
		{"a tab-led item after a comment", "verified:\n# c\n\t- by: river", vf},
		{"a tab-led item after a comment, under ~", "verified: ~\n# by hand\n\t- by: river", vf},
		{"a tab-led item after a comment, under null", "verified: null\n# by river\n\t- by: river", vf},
		{`a tab-led item after a comment, under ""`, "verified: \"\"\n# c\n\t- by: river", vf},
		{"a tab-led dash alone after a comment", "verified:\n# c\n\t-", vf},
		{"a tab-led flow item after a comment", "verified:\n# c\n\t- {by: river}", vf},
		{"a tab-led item after plain text", "verified:\nnocolon\n\t- by: river", vf},
		{"a tab-led item after plain text, under null", "verified: null\nsee below\n\t- by: river", vf},
		{"a tab-led item after text led by a space", "verified: ~\n note\n\t- by: river", vf},
		{"a tab-led item after a quoted colon", "verified:\n'a:b'\n\t- by: river", vf},
		{"a tab-led item after an anchor", "verified:\n&a x\n\t- by: river", vf},
		{"an item after an explicit key", "verified:\n? note\n  - by: river", vf},
		{"a dash item after an explicit key", "verified:\n? x\n- by: river", vf},
		{"an item after a key with an unclosed quote", "verified:\ngenrat\"d: x\n  - by: river", vf},
		{"a dash item after a key with an unclosed quote", "verified:\ngenrat\"d: x\n- by: river", vf},
		// v1 read no entry from a tab-led line that is no item, nor from a
		// line under [] or {}: a lenient reader may, so they protect.
		{"a tab-led pair after a comment", "verified:\n# c\n\tby: river", vf},
		{"a tab-led line after a blank one under an empty list", "verified: []\n\n\tby: river", vf},
		{"a tab-led item after a comment, under {}", "verified: {}\n# c\n\t- by: river", vf},
	}
	// Each of three Unicode spaces before a key, before its colon, inside
	// its quotes and around them.
	for _, sp := range []struct{ name, r string }{{"U+00A0", "\U000000a0"}, {"U+2003", "\U00002003"}, {"U+3000", "\U00003000"}} {
		for _, k := range []struct {
			key, value string
			want       Protection
		}{{"locked", " false", lk}, {"verified", " []", vf}} {
			rows = append(rows,
				keyFormRow{sp.name + " before " + k.key, sp.r + k.key + ":" + k.value, k.want},
				keyFormRow{sp.name + " before the colon of " + k.key, k.key + sp.r + ":" + k.value, k.want},
				keyFormRow{sp.name + " inside the quotes of " + k.key, `"` + sp.r + k.key + `":` + k.value, k.want},
				keyFormRow{sp.name + " inside single quotes of " + k.key, "'" + k.key + sp.r + "':" + k.value, k.want},
				keyFormRow{sp.name + " around the quotes of " + k.key, sp.r + `"` + k.key + `"` + sp.r + ":" + k.value, k.want},
			)
		}
	}
	return rows
}

// Lines that name no mark, or the bare key read by its value.
var keyFormsUnprotected = []keyFormRow{
	{"a no-break space before the colon of another key", "status\U000000a0: stable", Protection{}},
	{"a quote inside a key", `genrat"d: x`, Protection{}},
	{"a capital", "Locked: true", Protection{}},
	{"a longer word", "lockedness: x", Protection{}},
	{"a comment", "# locked: true", Protection{}},
	{"the word as a value", "title: locked: x", Protection{}},
	{"the bare key with false", "locked: false", Protection{}},
	{"the bare verified key, empty", "verified: []", Protection{}},
	// A null with a comment after it, one of SPEC.md section 12's empty
	// forms, as ~ or as null.
	{"a comment after ~", "verified: ~ # by river", Protection{}},
	{"a comment after null", "verified: null # by river", Protection{}},
	{"a comment and a key between an empty verified and a tab-led item", "verified:\n# c\nnote: x\n\t- by: river", Protection{}},
	{"a key with a quoted colon between an empty verified and an item", "verified:\n'a': b\n- by: river", Protection{}},
	{"an escape YAML has, in another key", "\"ti\\u0074le\\_\": x", Protection{}},
	{"an unknown escape in a value", `title: "lo\qcked"`, Protection{}},
	{"a quote that never closes, in a value", "title: 'locked: x", Protection{}},
	{"another key behind a no-break space", "\U000000a0title: x", Protection{}},
	{"a quoted key behind white space, decoded to another word", "\U000000a0\"lo\\u0063k\": x", Protection{}},
}

// Each line protects as its row says, between other keys; a document
// newly read as locked refuses an edit.
func TestProtectionKeyForms(t *testing.T) {
	rows := append(newKeyForms(), keyFormsUnprotected...)
	for _, r := range rows {
		b := []byte("---\ntype: fact\n" + r.line + "\nstatus: stable\n---\nbody\n")
		d := Read(b)
		if got := d.Protection(); got != r.want {
			t.Errorf("%s, %q: %+v, want %+v", r.name, r.line, got, r.want)
		}
		if r.want.Locked {
			if err := d.Set("title", "x"); !errors.Is(err, ErrProtected) {
				t.Errorf("%s, %q: Set gives %v, want ErrProtected", r.name, r.line, err)
			}
		}
		// The line, as the first of the block, protects alike.
		if got := Read([]byte("---\n" + r.line + "\ntype: fact\n---\n")).Protection(); got != r.want {
			t.Errorf("%s, %q first: %+v, want %+v", r.name, r.line, got, r.want)
		}
	}
}

// YAML 1.2's escapes, each read as its character; an escape YAML does
// not have is kept as written and is not valid.
func TestYAMLEscapes(t *testing.T) {
	for _, c := range []struct{ in, want string }{
		{`\0`, "\x00"}, {`\a`, "\a"}, {`\b`, "\b"}, {`\t`, "\t"}, {"\\\t", "\t"}, {`\n`, "\n"}, {`\v`, "\v"},
		{`\f`, "\f"}, {`\r`, "\r"}, {`\e`, "\x1b"}, {`\ `, " "}, {`\"`, `"`}, {`\/`, "/"}, {`\\`, `\`},
		{`\N`, "\u0085"}, {`\_`, "\U000000a0"}, {`\L`, "\U00002028"}, {`\P`, "\U00002029"},
		{`\x41`, "A"}, {"\\u00e9", "\U000000e9"}, {`\U0001F600`, "\U0001F600"},
	} {
		if got := yamlUnescape("a" + c.in + "b"); got != "a"+c.want+"b" {
			t.Errorf("%q reads %q, want %q", c.in, got, c.want)
		}
		if !escapesValid("a" + c.in + "b") {
			t.Errorf("%q is not valid", c.in)
		}
	}
	for _, in := range []string{`\q`, `\ud800`, `\uDFFF`, `\x4`, `\xZZ`, `\u00e`, `\U00110000`, `\U0000004`, `\`, `\'`, `\c`} {
		if escapesValid("a" + in) {
			t.Errorf("%q is valid", in)
		}
		if in != `\` && !strings.HasPrefix(in, `\x`) && !strings.HasPrefix(in, `\u`) && !strings.HasPrefix(in, `\U`) {
			if got := yamlUnescape("a" + in); got != "a"+in {
				t.Errorf("%q reads %q, want it kept", in, got)
			}
		}
	}
}
