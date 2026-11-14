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

// Each fixture page reads through ReadV1 as its golden, v1's own reading;
// every page has its golden and every golden its page.
func TestV1Goldens(t *testing.T) {
	checkGoldens(t, "testdata/v1", 9)
}

// Forms v1 read, beyond the pages it wrote, which the codec reads alike:
// each page of testdata/v1/forms reads as its golden, which the tests in
// v1oracle hold to v1's own reading.
func TestReadV1Forms(t *testing.T) {
	checkGoldens(t, "testdata/v1/forms", 10)
}

// checkGoldens reads each page in dir through ReadV1 and compares it with
// its golden. dir holds at least least pages, and a golden for each.
func checkGoldens(t *testing.T, dir string, least int) {
	t.Helper()
	pages, _ := filepath.Glob(filepath.Join(dir, "*.md"))
	goldens, _ := filepath.Glob(filepath.Join(dir, "*.json"))
	if len(pages) < least || len(pages) != len(goldens) {
		t.Fatalf("%s: %d pages, %d goldens", dir, len(pages), len(goldens))
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
		{"hash-after-tab", `the comment in the value: "Tide\t# draft"`,
			func(p V1Page, err error) bool { return err == nil && p.Title == "Tide" && p.Status == "stable" }},
		// A quote inside a plain value opens no string, so the "#" after it
		// starts a comment.
		{"quote-inside-before-hash", `the comment in the value: "it's the one from #42"`,
			func(p V1Page, err error) bool { return err == nil && p.Extra["model_note"] == "it's the one from" }},
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
		{"yaml-escape", `"Tide\x20feed", the escape kept`,
			func(p V1Page, err error) bool { return err == nil && p.Title == "Tide feed" }},
		{"tab-in-quotes", `"Tide feed", the tab trimmed as JSON refused it`,
			func(p V1Page, err error) bool { return err == nil && p.Title == "\tTide feed" }},
		{"explicit-key", `no title, and "Tide feed" under the key ""`,
			func(p V1Page, err error) bool { return err == nil && p.Title == "Tide feed" && len(p.Extra) == 0 }},
		{"indented-block", "no key at all",
			func(p V1Page, err error) bool {
				return err == nil && p.Type == "thread" && p.Title == "Tide feed" && p.Status == "stable"
			}},
		// YAML reads no key on these lines, or refuses them; v1 read one.
		{"no-space-after-colon", `the title "Tide feed"`, refused(ErrNotWritten, Protection{})},
		{"tab-indent", `the list ["tide-tables"]`, refused(ErrNotWritten, Protection{})},
		{"text-after-quote", `the title "\"Tide\" feed"`, refused(ErrNotWritten, Protection{})},
		{"hash-in-source-key", `a source with the key "note #1"`, refused(ErrNotWritten, Protection{})},
		// YAML reads these keys otherwise; v1 cut the line at its first
		// colon and trimmed quotes from the key, or dropped the pair.
		{"key-with-colon", `the title "{a: b}"`, refused(ErrNotWritten, Protection{})},
		{"key-with-quote", "no such key: the pair dropped", refused(ErrNotWritten, Protection{})},
		{"anchor-before-key", `the key "&a title"`, refused(ErrNotWritten, Protection{})},
		{"quote-in-source-key", "a source without the pair", refused(ErrNotWritten, Protection{})},
		{"tight-pair", `a generated record by "codex/model-b"`, refused(ErrNotWritten, Protection{})},
		{"open-quote-in-source-key", "a source without the pair", refused(ErrNotWritten, Protection{})},
		// v1 opened a string at a quote anywhere, YAML only where a node
		// may start; v1 split a flow collection or told a pair otherwise.
		{"quote-inside-flow-item", `the aliases ["tide", "harbour's feed, bay"]`, refused(ErrNotWritten, Protection{})},
		{"quote-inside-list-item", `the repos ["tide-tables", "harbour's: feed"]`, refused(ErrNotWritten, Protection{})},
		{"quote-inside-verified", `one verified record, by "human:river's, at: ..."`, refused(ErrNotWritten, Protection{Verified: true})},
		{"text-after-quoted-item", `the aliases ["tide", "harbour\" \"feed"]`, refused(ErrNotWritten, Protection{})},
		{"other-bracket", `a second verified record by "eva-md/2026.11.5 }"`, refused(ErrNotWritten, Protection{Verified: true})},
		// YAML starts an item only at a dash with a space after it, and a
		// comment, even one right after a dash, is no item's text.
		{"tight-dash", `the list ["tide-tables", "harbour"]`, refused(ErrNotWritten, Protection{})},
		{"comment-only-item", `the list ["tide-tables", "# harbour later"]`, refused(ErrNotWritten, Protection{})},
		{"text-after-quoted-list-item", `the item "\"harbour\" feed", its quotes kept`, refused(ErrNotWritten, Protection{})},
		{"flow-item-comment", `one verified record, by "human:river"`, refused(ErrNotWritten, Protection{Verified: true})},
		{"hash-after-quoted-key", `the kind "#decision"`, refused(ErrNotWritten, Protection{})},
		{"unknown-escape", `the title "Tide\qfeed"`, refused(ErrNotWritten, Protection{})},
		// A comment line under a key is no item and no line of a value.
		{"comment-line-under-key", `the kept lines ["- tide-tables", "# harbour later", "- harbour"] and ["# by hand"]`,
			func(p V1Page, err error) bool {
				return err == nil && reflect.DeepEqual(p.Extra["repos"], []any{"tide-tables", "harbour"}) && p.Extra["model"] == "model-b"
			}},
		{"nul-byte", "the title with the byte", refused(ErrUnreadable, Protection{Unreadable: true})},
		{"invalid-utf8", "the title with the byte", refused(ErrUnreadable, Protection{Unreadable: true})},
		{"conflict-marker", "the body with the markers", refused(ErrUnreadable, Protection{Unreadable: true})},
		// The rows where v1 protected and the codec does not. An empty
		// mapping is no verified mark (the format's section 6), though v1
		// read it as a record with no one and no time.
		{"empty-verified", "one verified record, empty, so verified",
			func(p V1Page, err error) bool {
				return err == nil && reflect.DeepEqual(p.Verified, []V1Stamp{{}}) && !p.Doc.Protection().Verified
			}},
		// A comment is no value: the key is empty, so no verified mark.
		{"verified-comment-only", "no record, but verified, as the key holds text",
			func(p V1Page, err error) bool { return err == nil && p.Verified == nil && !p.Doc.Protection().Verified }},
		// An empty value with a comment after it is empty too; v1 compared
		// the whole text after the colon, comment and all, with its empty
		// values.
		{"verified-empty-list-comment", `"[] # none yet", no empty value, so verified`,
			func(p V1Page, err error) bool { return err == nil && p.Verified == nil && !p.Doc.Protection().Verified }},
		{"verified-null-comment", `"~ # by hand later", so verified`,
			func(p V1Page, err error) bool { return err == nil && p.Verified == nil && !p.Doc.Protection().Verified }},
		{"verified-empty-map-comment", `one verified record, empty, and "{} # none yet", so verified`,
			func(p V1Page, err error) bool {
				return err == nil && reflect.DeepEqual(p.Verified, []V1Stamp{{}}) && !p.Doc.Protection().Verified
			}},
		// A space before the colon and none after it, or a quote v1 trimmed
		// from the key: YAML reads another key or none. ReadV1 refuses
		// them, and Protection reads the mark v1 read.
		{"locked-space-before-colon", "locked: true", refused(ErrNotWritten, Protection{Locked: true})},
		{"verified-space-before-colon", `verified by "a"`, refused(ErrNotWritten, Protection{Verified: true})},
		{"verified-quote-in-key", `the key "verified", so verified`, refused(ErrNotWritten, Protection{Verified: true})},
		{"locked-spaces-in-quotes", `the key "locked", spaces trimmed, so locked`, refused(ErrNotWritten, Protection{Locked: true})},
		// v1's check of a verified mark cut the line at its first colon,
		// quotes and all; the page itself it read as YAML does.
		{"verified-colon-in-quoted-key", `the key "verified", the value "by river': yes", so verified`, refused(ErrNotWritten, Protection{Verified: true})},
		// v1 trimmed white space from a key as Go does, a no-break space
		// too; YAML keeps it in the key, and Protection trims it as v1 did.
		{"nbsp-before-locked", `the key "locked", the space trimmed, so locked`, refused(ErrNotWritten, Protection{Locked: true})},
		{"verified-nbsp-before-colon", `the key "verified", the space trimmed, so verified`, refused(ErrNotWritten, Protection{Verified: true})},
		{"locked-nbsp-in-quotes", `the key "locked", the space trimmed, so locked`, refused(ErrNotWritten, Protection{Locked: true})},
		{"status-nbsp-before-colon", `the status "stable"`, refused(ErrNotWritten, Protection{})},
		// A quote that never closes: YAML reads no key; v1's check of a
		// verified mark cut the line at its colon and trimmed the quote.
		// Another reader may close it anywhere, so it protects both ways.
		{"verified-unclosed-quote", `the key "verified", so verified`, refused(ErrNotWritten, Protection{Locked: true, Verified: true})},
		// YAML refuses a plain value that holds ": "; v1 kept all of it,
		// and the codec would drop the comment from it.
		{"colon-value-comment", `the writer "Tide: feed #3 (draft)"`, refused(ErrNotWritten, Protection{})},
		{"colon-pair-comment", `a generated record by "codex: model-b # by hand"`, refused(ErrNotWritten, Protection{})},
		{"colon-source-comment", `a source titled "Tide: feed # draft"`, refused(ErrNotWritten, Protection{})},
		{"colon-item-comment", `the repos ["tide-tables", "harbour:\tbay # later"]`, refused(ErrNotWritten, Protection{})},
		// A plain value that ends in a colon, then a comment: YAML refuses
		// it, and the codec would drop the comment.
		{"colon-end-comment", `the writer "Tide: # x"`, refused(ErrNotWritten, Protection{})},
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
		{"a line that is no key", page("title: x\nsecret_word_zq:y\n"), ErrNotWritten},
		{"a line indented with a tab", page("title: x\n\tsecret_word_zq: y\n"), ErrNotWritten},
		{"a line less indented than the block", page("  title: x\nsecret_word_zq: y\n"), ErrNotWritten},
		{"a line before the first key", page("secret_word_zq\ntitle: x\n"), ErrNotWritten},
		{"text after a quoted value", page("secret_word_zq: \"a\" b\n"), ErrNotWritten},
		{"text after a flow list", page("repos: [a] b\n"), ErrNotWritten},
		{"a source line that is no pair", page("sources:\n  - resource: x\n    secret_word_zq\n"), ErrNotWritten},
		{"a source pair before any item", page("sources:\n  secret_word_zq: x\n"), ErrNotWritten},
		{"a verified line that is no pair", page("verified:\n  - by: a\n    secret_word_zq # x\n"), ErrNotWritten},
		{"a generated line that is no pair", page("generated:\n  by: a\n  secret_word_zq\n"), ErrNotWritten},
		{"text after a quoted source value", page("sources:\n  - resource: 'a\\' secret_word_zq'\n"), ErrNotWritten},
		{"a source value that runs on", page("sources:\n  - resource: \"secret_word_zq\n"), ErrNotWritten},
		{"text after a quoted value in a flow mapping", page("generated: { by: \"x\" secret_word_zq, at: y }\n"), ErrNotWritten},
		{"a flow mapping item that is no pair", page("verified: { secret_word_zq }\n"), ErrNotWritten},
		{"a verified item that is no mapping", page("verified: [secret_word_zq]\n"), ErrNotWritten},
		{"a source item's flow mapping item that is no pair", page("sources:\n  - { resource: x, secret_word_zq }\n"), ErrNotWritten},
		{"a block scalar with nothing under it", page("description: |\n"), ErrNotWritten},
		{"a key that holds a colon", page("secret_word_zq :{a: b}\n"), ErrNotWritten},
		{"a key v1 wrote, then a colon", page("generated :{by: a, at: b}\n"), ErrNotWritten},
		{"a key v1 wrote, then two colons", page("title:: x\n"), ErrNotWritten},
		{"a key that holds a quote", page("secret\"word_zq: x\n"), ErrNotWritten},
		{"a single-quoted key with an escape", page("'secret''word_zq': x\n"), ErrNotWritten},
		{"a double-quoted key with an escape", page("\"secret\\x20word_zq\": x\n"), ErrNotWritten},
		{"an anchor before the key", page("&a secret_word_zq: x\n"), ErrNotWritten},
		{"a tag before the key", page("!!str secret_word_zq: x\n"), ErrNotWritten},
		{"an explicit key after a tag", page("? !!str secret_word_zq\n: x\n"), ErrNotWritten},
		{"a source key that holds a quote", page("sources:\n  - resource: x\n    secret'word_zq: y\n"), ErrNotWritten},
		{"a source pair with no space after its colon", page("sources:\n  - resource: x\n    secret_word_zq:y\n"), ErrNotWritten},
		{"a generated pair with two colons", page("generated:\n  by:: a\n"), ErrNotWritten},
		{"a flow pair with no space after its colon", page("generated: {by:a, at: b}\n"), ErrNotWritten},
		{"a quote inside a plain item of a flow list", page("secret_word_zq: [secret'word_zq, b]\n"), ErrNotWritten},
		{"a quote inside a plain item, a quoted item after it", page("x: [secret'word_zq, 'b']\n"), ErrNotWritten},
		{"a quote inside a nested flow list", page("secret_word_zq: [[secret'word_zq, b]]\n"), ErrNotWritten},
		{"a quote inside a plain value of a flow mapping", page("generated: { by: secret\"word_zq, at: b }\n"), ErrNotWritten},
		{"a quote inside a verified list", page("verified: [{ by: secret\"word_zq }, { by: c }]\n"), ErrNotWritten},
		{"a quote inside a source's flow mapping", page("sources:\n  - { resource: secret'word_zq, title: x }\n"), ErrNotWritten},
		{"a list item v1 read as a scalar", page("secret_word_zq:\n  - secret'word_zq: x\n"), ErrNotWritten},
		{"a list item with a quote in its flow list", page("secret_word_zq:\n  - [secret'word_zq, x]\n"), ErrNotWritten},
		{"a flow list closed by a brace", page("verified: [{ by: a }, { by: secret_word_zq }}\n"), ErrNotWritten},
		{"a flow mapping closed by a bracket", page("generated: { by: secret_word_zq ]\n"), ErrNotWritten},
		{"text after a quoted item of a flow list", page("secret_word_zq: [a, \"b,\"secret_word_zq\"]\n"), ErrNotWritten},
		{"a source key whose quote does not close", page("sources:\n  - \"secret_word_zq: s:1\n"), ErrNotWritten},
		{"a source key with text after its quote", page("sources:\n  - resource: s:1\n    \"a\"secret_word_zq\": x\n"), ErrNotWritten},
		{"list items with no space after the dash", page("secret_word_zq:\n  -secret_word_zq\n  -b\n"), ErrNotWritten},
		{"a list item that is only a comment", page("secret_word_zq:\n  - a\n  - # secret_word_zq\n"), ErrNotWritten},
		{"a list item that is a comment after a pair", page("secret_word_zq:\n  - a # secret_word_zq: b\n"), ErrNotWritten},
		{"text after a quoted list item", page("secret_word_zq:\n  - a\n  - \"b\" secret_word_zq\n"), ErrNotWritten},
		{"a source with no space after the dash", page("sources:\n  -resource: secret_word_zq\n"), ErrNotWritten},
		{"a generated pair with no space after the dash", page("generated:\n  -by: secret_word_zq\n"), ErrNotWritten},
		{"a flow mapping cut by a comment in a list", page("verified:\n  - { by: a, # secret_word_zq }\n"), ErrNotWritten},
		{"a # right after a quoted key's colon", page("'secret_word_zq':#secret_word_zq\n"), ErrNotWritten},
		{"a space inside a quoted key's quotes", page("' secret_word_zq': x\n"), ErrNotWritten},
		{"a space inside a double-quoted key's quotes, with a tab", page("\"\tsecret_word_zq \": x\n"), ErrNotWritten},
		{"a space inside a quoted source key", page("sources:\n  - ' secret_word_zq': x\n"), ErrNotWritten},
		{"an escape in a quoted source key", page("sources:\n  - 'secret''word_zq': x\n"), ErrNotWritten},
		{"an escape YAML does not know", page("title: \"secret\\qword_zq\"\n"), ErrNotWritten},
		{"a short unicode escape", page("secret_word_zq: \"\\u00e9t\\u0e9\"\n"), ErrNotWritten},
		{"an escape past the last code point", page("title: \"\\U00110000\"\n"), ErrNotWritten},
		{"a quoted key with a colon after verified", page("\"verified:secret_word_zq\": x\n"), ErrNotWritten},
		{"a single-quoted key with a colon after verified, a list under it", page("'verified:':\n  - by: secret_word_zq\n"), ErrNotWritten},
		// YAML reads these as U+00A0, U+2028 and U+2029; v1 kept them as
		// written. ReadV1 does not guess between the two.
		{"an escape that is a no-break space", page("title: \"secret\\_word_zq\"\n"), ErrNotWritten},
		{"an escape that is a line separator", page("title: \"secret\\Lword_zq\"\n"), ErrNotWritten},
		{"an escape that is a paragraph separator", page("title: \"secret\\Pword_zq\"\n"), ErrNotWritten},
		{"an escape YAML does not know, in a flow mapping", page("generated: { by: \"secret\\qword_zq\" }\n"), ErrNotWritten},
		{"an escape YAML does not know, in a source", page("sources:\n  - resource: \"secret\\qword_zq\"\n"), ErrNotWritten},
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
	// A frontmatter that is one flow mapping is refused as that, not as the
	// key it holds.
	if _, err := ReadV1(page("{type: thread, title: x}\n")); err == nil || !strings.Contains(err.Error(), "one flow mapping") {
		t.Errorf("%v does not say the frontmatter is one flow mapping", err)
	}
}

// ReadV1 never panics, and a refusal returns the Doc and nothing else.
func FuzzReadV1(f *testing.F) {
	for _, glob := range []string{"testdata/v1/*.md", "testdata/v1/forms/*.md", "testdata/v1/differences/*.md", "testdata/corpus/*.md"} {
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
