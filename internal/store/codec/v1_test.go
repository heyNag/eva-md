package codec

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// v1Golden is a page as testdata/v1's goldens hold it, written as the
// tests in v1oracle write them from v1's own reading.
func v1Golden(t *testing.T, p V1Page) []byte {
	t.Helper()
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(p); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func readV1File(t *testing.T, path string) (V1Page, error) {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return ReadV1(b)
}

// Each fixture page reads through ReadV1 as its golden, v1's own reading,
// holds it; every page has its golden and every golden its page.
func TestV1Goldens(t *testing.T) {
	pages, _ := filepath.Glob("testdata/v1/*.md")
	goldens, _ := filepath.Glob("testdata/v1/*.json")
	if len(pages) < 9 || len(pages) != len(goldens) {
		t.Fatalf("%d fixture pages, %d goldens", len(pages), len(goldens))
	}
	for _, path := range pages {
		want, err := os.ReadFile(strings.TrimSuffix(path, ".md") + ".json")
		if err != nil {
			t.Error(err)
			continue
		}
		p, err := readV1File(t, path)
		if err != nil {
			t.Errorf("%s: %v", path, err)
			continue
		}
		if p.Doc == nil || p.Doc.Unreadable() != "" {
			t.Errorf("%s: no readable Doc", path)
		}
		if got := v1Golden(t, p); !bytes.Equal(got, want) {
			t.Errorf("%s reads\n%s\nits golden holds\n%s", path, got, want)
		}
	}
}

// Where v1 misread a hand edit, the codec keeps its own reading. Each row
// names a file of testdata/v1/differences, what v1 read, and the codec's
// answer. The tests in v1oracle check that v1 reads each otherwise.
func TestV1Differences(t *testing.T) {
	type check func(p V1Page, err error) bool
	locked := func(p V1Page, err error) bool { return err == nil && p.Doc.Protection().Locked }
	refused := func(sentinel error, prot Protection) check {
		return func(p V1Page, err error) bool {
			return errors.Is(err, sentinel) && p.Doc != nil && p.Doc.Protection() == prot && p.Extra == nil && p.Title == ""
		}
	}
	rows := []struct {
		file, v1 string
		codec    check
	}{
		{"comment-after-value", `the comment in the value: "Tide feed  # draft"`,
			func(p V1Page, err error) bool { return err == nil && p.Title == "Tide feed" && p.Status == "stable" }},
		{"comment-after-lock", `the string "true  # leave alone", so not locked`,
			func(p V1Page, err error) bool { return locked(p, err) && p.Extra["locked"] == true }},
		{"comment-after-flow", "a generated record with no by",
			func(p V1Page, err error) bool {
				return err == nil && reflect.DeepEqual(p.Generated, &V1Stamp{By: "codex/model-b", At: "2026-11-04T06:12:09Z"})
			}},
		{"comment-as-key", `a key "# note" in Extra`,
			func(p V1Page, err error) bool { return err == nil && len(p.Extra) == 0 && p.Title == "Tide feed" }},
		{"repeated-key", "the last value", refused(ErrRepeated, Protection{})},
		{"block-scalar", `the description "|"`, refused(ErrNotWritten, Protection{})},
		{"continued-value", "an empty description", refused(ErrNotWritten, Protection{})},
		{"byte-order-mark", "the page without the mark", refused(ErrUnreadable, Protection{Unreadable: true})},
		{"long-line", "every key after the long line dropped, the lock among them", refused(ErrUnreadable, Protection{Unreadable: true})},
		{"quoted-false-lock", `the string "false", so not locked`,
			func(p V1Page, err error) bool { return locked(p, err) && p.Extra["locked"] == "false" }},
		{"quoted-key-lock", "the key locked, false, so not locked",
			func(p V1Page, err error) bool { return locked(p, err) && p.Extra["locked"] == false }},
		{"single-quoted", `"it''s the feed", quotes doubled and spaces trimmed`,
			func(p V1Page, err error) bool { return err == nil && p.Title == "it's the feed  " }},
		// The one row where v1 protected and the codec does not: an empty
		// mapping is no verified mark (the format's section 6), though v1
		// read it as a record with no one and no time.
		{"empty-verified", "one verified record, empty, so verified",
			func(p V1Page, err error) bool {
				return err == nil && reflect.DeepEqual(p.Verified, []V1Stamp{{}}) && !p.Doc.Protection().Verified
			}},
	}
	var named []string
	for _, r := range rows {
		named = append(named, r.file+".md")
		p, err := readV1File(t, filepath.Join("testdata/v1/differences", r.file+".md"))
		if !r.codec(p, err) {
			t.Errorf("%s (v1 read %s): the codec reads %+v, %v", r.file, r.v1, p, err)
		}
	}
	files, _ := filepath.Glob("testdata/v1/differences/*.md")
	for _, f := range files {
		if !slices.Contains(named, filepath.Base(f)) {
			t.Errorf("%s is in no row", f)
		}
	}
	if len(files) != len(rows) {
		t.Errorf("%d files, %d rows", len(files), len(rows))
	}
}

// Forms v1 read, which the codec reads alike, beyond those v1 wrote.
func TestReadV1Forms(t *testing.T) {
	page := func(front string) string { return "---\n" + front + "---\nbody\n" }
	for _, c := range []struct {
		name, in string
		want     V1Page
	}{
		{"generated as a block mapping", page("generated:\n  by: codex/model-b\n  at: \"2026-11-04T06:12:09Z\"\n"),
			V1Page{Generated: &V1Stamp{"codex/model-b", "2026-11-04T06:12:09Z"}}},
		{"generated with no pairs", page("generated: x\n"), V1Page{}},
		{"verified as a flow list, empty items dropped", page("verified: [{ by: a, at: b }, {}, { by: c }]\n"),
			V1Page{Verified: []V1Stamp{{"a", "b"}, {"c", ""}}}},
		{"verified as a block list of pairs", page("verified:\n  - by: a\n    at: b\n  - { by: c, at: d }\n"),
			V1Page{Verified: []V1Stamp{{"a", "b"}, {"c", "d"}}}},
		{"sources with a count and a comment line", page("sources:\n  # kept\n  - resource: \"s:1#2\"\n    usage_count: 3\n    seen: \"x, y\"\n"),
			V1Page{Sources: []V1Source{{Resource: "s:1#2", UsageCount: 3, Extra: map[string]string{"seen": "x, y"}}}}},
		{"a block list of scalars", page("repos:\n  - a\n  - \"b c\"\n  - 3\n"),
			V1Page{Extra: map[string]any{"repos": []any{"a", "b c", float64(3)}}}},
		{"a block v1 kept as lines", page("note:\n  a: 1\n    b: 2\n"),
			V1Page{Extra: map[string]any{"note": []string{"a: 1", "  b: 2"}}}},
		{"a flow list of plain values", page("aliases: [tide, 'harbour feed', 2, true]\n"),
			V1Page{Extra: map[string]any{"aliases": []any{"tide", "harbour feed", float64(2), true}}}},
		{"JSON and plain scalars", page("head: 4e2bf4c\nparent: \"\"\nlocked: null\nmodel_note: \"a\\u00e9\"\nempty:\n"),
			V1Page{Extra: map[string]any{"head": "4e2bf4c", "parent": "", "locked": nil, "model_note": "aé", "empty": ""}}},
		{"keys v1 kept in fields eva-md never used", page("tags: [a]\nresource: x\ngovernance: hold\ncode_refs: [y]\n"), V1Page{}},
		{"an explicit key and a key with a space before its colon", page("? title\n: Tide\nstatus : stable\n"),
			V1Page{Title: "Tide", Status: "stable"}},
		{"CR LF lines", "---\r\ntype: thread\r\ntitle: \"A\"\r\n---\r\nbody\r\n", V1Page{Type: "thread", Title: "A", Body: "body\r\n"}},
	} {
		got, err := ReadV1([]byte(c.in))
		if err != nil {
			t.Errorf("%s: %v", c.name, err)
			continue
		}
		got.Doc = nil
		if c.want.Extra == nil {
			c.want.Extra = map[string]any{}
		}
		if c.want.Body == "" {
			c.want.Body = "body\n"
		}
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: got %+v, want %+v", c.name, got, c.want)
		}
	}
}

// What ReadV1 refuses: an error it can be told by, the Doc set, and a text
// that names neither the library v1 was built on nor a key a person added.
func TestReadV1Refusals(t *testing.T) {
	page := func(front string) []byte { return []byte("---\n" + front + "---\nbody\n") }
	for _, c := range []struct {
		name string
		in   []byte
		want error
	}{
		{"no frontmatter", []byte("body\n"), ErrUnreadable},
		{"a conflict marker", []byte("---\ntitle: x\n<<<<<<< ours\n---\n"), ErrUnreadable},
		{"a block scalar in a key a person added", page("secret_word_zq: >-\n  text\n"), ErrNotWritten},
		{"a tag", page("secret_word_zq: !!str x\n"), ErrNotWritten},
		{"an anchor", page("title: &a x\n"), ErrNotWritten},
		{"an alias as the key", page("*secret_word_zq: x\n"), ErrNotWritten},
		{"a merge", page("<<: {a: b}\n"), ErrNotWritten},
		{"one flow mapping", page("{type: thread, secret_word_zq: x}\n"), ErrNotWritten},
		{"a quoted value that runs on", page("secret_word_zq: \"a\n  b\"\n"), ErrNotWritten},
		{"a flow list that runs on", page("repos: [a,\n  b]\n"), ErrNotWritten},
		{"a plain value with lines under it", page("secret_word_zq: a\n  b\n"), ErrNotWritten},
		{"a list under a value", page("sources: x\n  - resource: y\n"), ErrNotWritten},
		{"a repeated key a person added", page("secret_word_zq: a\n\"secret_word_zq\": b\n"), ErrRepeated},
	} {
		p, err := ReadV1(c.in)
		if !errors.Is(err, c.want) {
			t.Errorf("%s: %v, want %v", c.name, err, c.want)
			continue
		}
		if p.Doc == nil {
			t.Errorf("%s: no Doc", c.name)
		}
		if regexp.MustCompile(`(?i)okf|secret_word`).MatchString(err.Error()) {
			t.Errorf("%s: %q names the library or the key", c.name, err)
		}
	}
	// A key v1 wrote is named, so a refusal says where to look.
	if _, err := ReadV1(page("description: |\n  x\n")); err == nil || !strings.Contains(err.Error(), "description") {
		t.Errorf("%v does not name description", err)
	}
}

// ReadV1 never panics, and a refusal returns the Doc and nothing else.
func FuzzReadV1(f *testing.F) {
	for _, glob := range []string{"testdata/v1/*.md", "testdata/v1/differences/*.md", "testdata/corpus/*.md"} {
		files, _ := filepath.Glob(glob)
		for _, path := range files {
			if b, err := os.ReadFile(path); err == nil {
				f.Add(b)
			}
		}
	}
	f.Fuzz(func(t *testing.T, b []byte) {
		p, err := ReadV1(b)
		if p.Doc == nil {
			t.Fatal("no Doc")
		}
		if err != nil && !reflect.DeepEqual(p, V1Page{Doc: p.Doc}) {
			t.Fatalf("a refusal returns %+v", p)
		}
		if err == nil && p.Extra == nil {
			t.Fatal("no Extra")
		}
	})
}
