package codec

import (
	"reflect"
	"slices"
	"strings"
	"testing"
)

// Entries gives every entry in file order, a repeated key twice, with its
// line, whether it is written, its value, its scalar and its text.
func TestEntries(t *testing.T) {
	in := "---\n# a comment\ntype: fact\ntitle: 'it''s'  # note\nsources:\n  - {\"id\": \"e1\"}\nstate: active\nstate: \"retired\"\n" +
		"\"locked\": true\n*k : x\n? a\n  b\n: v\ncues: [\"a\", \"b\"]\nnote: |\n  one\n\n  two\n---\nbody\n"
	got := Read([]byte(in)).Entries()
	want := []Entry{
		{Key: "type", Line: 3, Written: true, Value: "fact", Scalar: "fact", Text: " fact"},
		{Key: "title", Line: 4, Scalar: "it's", Text: " 'it''s'  # note"},
		{Key: "sources", Line: 5, Written: true, Value: []any{map[string]any{"id": "e1"}}, Text: "\n  - {\"id\": \"e1\"}"},
		{Key: "state", Line: 7, Written: true, Value: "active", Scalar: "active", Text: " active"},
		{Key: "state", Line: 8, Written: true, Value: "retired", Scalar: "retired", Text: ` "retired"`},
		{Key: "locked", Line: 9, Scalar: "true", Text: " true"},
		{Key: unknownKey, Line: 10, Scalar: "x", Text: " x"},
		{Key: "a", Line: 11, Scalar: "v", Text: " v\n  b"},
		{Key: "cues", Line: 14, Written: true, Value: []any{"a", "b"}, Scalar: `["a", "b"]`, Text: ` ["a", "b"]`},
		// A blank line is opaque, in a block scalar too.
		{Key: "note", Line: 15, Scalar: "|", Text: " |\n  one\n  two"},
	}
	if o := Read([]byte(in)).Opaque(); !slices.Equal(o, []int{2, 17}) {
		t.Errorf("Opaque: %v", o)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Entries:\n%#v\nwant\n%#v", got, want)
	}
	// Each call decodes afresh: the caller owns what it gets.
	got[2].Value.([]any)[0].(map[string]any)["id"] = "changed"
	if again := Read([]byte(in)).Entries(); again[2].Value.([]any)[0].(map[string]any)["id"] != "e1" {
		t.Error("a change to one call's value shows in the next")
	}
	d := Read([]byte(in))
	d.Entries()[0].Value = "changed"
	if v, _ := d.Value("type"); v != "fact" {
		t.Errorf("a change to an entry shows in Value: %v", v)
	}
	for _, b := range []string{"", "---\n", "---\ntype: \x00\n---\n", "\xef\xbb\xbf---\ntype: fact\n---\n"} {
		if e := Read([]byte(b)).Entries(); e != nil {
			t.Errorf("%q is unreadable and has entries %v", b, e)
		}
	}
	if (&Doc{}).Entries() != nil {
		t.Error("the zero Doc has entries")
	}
}

// For every key there once, in every readable document of the corpus and
// the fixture store, Entries says it is written exactly when Value decodes
// it, and gives the same value; a key there more than once is so in
// Entries too.
func TestEntriesAgreeWithValue(t *testing.T) {
	docs := corpus(t)
	for name, b := range editable(t) {
		docs[name] = b
	}
	n := 0
	for name, b := range docs {
		d := Read(b)
		count := map[string]int{}
		for _, e := range d.Entries() {
			count[e.Key]++
		}
		if len(count) != 0 {
			n++
		}
		for _, e := range d.Entries() {
			v, err := d.Value(e.Key)
			switch {
			case count[e.Key] > 1:
				if err != ErrRepeated {
					t.Errorf("%s: %s is there %d times, Value says %v", name, e.Key, count[e.Key], err)
				}
			case e.Written != (err == nil) || e.Written && !reflect.DeepEqual(v, e.Value):
				t.Errorf("%s: %s: entry %v %#v, Value %#v %v", name, e.Key, e.Written, e.Value, v, err)
			case !e.Written && e.Value != nil:
				t.Errorf("%s: %s is not written and has a value", name, e.Key)
			}
		}
	}
	if n < 20 {
		t.Fatalf("only %d documents with entries", n)
	}
}

// Opaque gives the lines of the frontmatter that belong to no key, counted
// as Entry.Line is, and no line of an entry.
func TestOpaque(t *testing.T) {
	cases := map[string][]int{
		"---\n# a comment\n\ntype: fact\n---\n":                                {2, 3},
		"---\ntype: fact\ntitle:x\n\tlocked: true\nnote: y\n---\nbody\n":       {3, 4},
		"---\n  type: fact\n  title: x\nlocked: false\n  state: active\n---\n": {4},
		"---\ntype: fact\nsources:\n  - {}\n\n  - {}\nstatus: stable\n---\n":   {5},
		"---\n- x\ntype: fact\n---\n":                                          {2},
		"---\n---\n":                                                           nil,
		"---\ntype: fact\n---\n":                                               nil,
	}
	for in, want := range cases {
		if got := Read([]byte(in)).Opaque(); !slices.Equal(got, want) {
			t.Errorf("%q: %v, want %v", in, got, want)
		}
	}
	// Over the corpus: an opaque line is no entry's key line, the lines
	// ascend, and each is inside the frontmatter.
	for name, b := range corpus(t) {
		d := Read(b)
		lines := d.Opaque()
		if !slices.IsSorted(lines) {
			t.Errorf("%s: %v", name, lines)
		}
		for _, e := range d.Entries() {
			if slices.Contains(lines, e.Line) {
				t.Errorf("%s: line %d is an entry's and opaque", name, e.Line)
			}
		}
		for _, n := range lines {
			if n < 2 || n > d.close {
				t.Errorf("%s: line %d is outside the frontmatter", name, n)
			}
		}
	}
	if Read([]byte("---\n# x\n")).Opaque() != nil {
		t.Error("an unreadable document has opaque lines")
	}
}

// Sections gives every section in order, a repeated heading twice; a
// "## " line in fenced code starts none, and a "# " line ends none.
func TestSectionsInOrder(t *testing.T) {
	in := "---\ntype: fact\n---\nThe statement.\n```\n## not a section\n```\n## Log\n- did: a\n# Top\n- did: b\n" +
		"## Detail\r\nwhy\r\n## Log\n~~~\n## in a fence\n~~~\n## Last"
	got := Read([]byte(in)).Sections()
	want := []Section{
		{Heading: "Log", Text: "- did: a\n# Top\n- did: b\n"},
		{Heading: "Detail", Text: "why\r\n"},
		{Heading: "Log", Text: "~~~\n## in a fence\n~~~\n"},
		{Heading: "Last", Text: ""},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Sections:\n%#v\nwant\n%#v", got, want)
	}
	// Each section's text is what Section gives for a heading there once.
	d := Read([]byte(in))
	for _, s := range got {
		text, err := d.Section(s.Heading)
		if s.Heading == "Log" {
			if err != ErrRepeated {
				t.Errorf("Log: %v", err)
			}
		} else if err != nil || text != s.Text {
			t.Errorf("%s: Section %q %v, Sections %q", s.Heading, text, err, s.Text)
		}
	}
	if s := Read([]byte("---\n---\nonly a preamble\n")).Sections(); len(s) != 0 {
		t.Errorf("a body with no section: %v", s)
	}
	if s := Read([]byte("---\n---\n\x00\n## Log\n")).Sections(); s != nil {
		t.Errorf("an unreadable document has sections: %v", s)
	}
}

// UnreadableText gives the reasons of Unreadable but the frontmatter's,
// so a static file with no frontmatter reads.
func TestUnreadableText(t *testing.T) {
	cases := map[string]string{
		"\xef\xbb\xbf* -text\n":               "a byte-order mark",
		"label = \"\xff\"\n":                  "invalid UTF-8",
		"a\x00\n":                             "a NUL byte",
		"x\n" + strings.Repeat("y", 64<<10+1): "line 2 is over 64 KiB",
		"path\tat\n=======\n":                 "a conflict marker on line 2",
		"* -text -filter -ident\n":            "",
		"schema = 2\r\n":                      "",
		"":                                    "",
		"---\ntype: fact\n":                   "",
		strings.Repeat("y", 64<<10) + "\n":    "",
	}
	for in, want := range cases {
		if got := UnreadableText([]byte(in)); got != want {
			t.Errorf("%.40q: %q, want %q", in, got, want)
		}
	}
	// On a document it says what Unreadable says, but for the reasons of
	// the frontmatter.
	for name, b := range corpus(t) {
		why := Read(b).Unreadable()
		if why == "no frontmatter on line 1" || why == "a frontmatter that does not close" {
			why = ""
		}
		if got := UnreadableText(b); got != why {
			t.Errorf("%s: %q, Unreadable %q", name, got, why)
		}
	}
}
