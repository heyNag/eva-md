package codec

import (
	"bytes"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/iotest"
)

const (
	corpusDir = "testdata/corpus"
	fixture   = "../testdata/fixture-store"
	// fact is a fixture item the record does not list; hand is one it
	// lists as hand-edited.
	fact = "projects/p_00gb3367a19r-acme-orders/items/2026/11/i01qa78gk3m9pa-orders-port.md"
	hand = "projects/p_6pbe1q7fxb5h-scratch-notes/items/2026/11/i01qqqp0w9x0y1-sandbox-account.md"
)

// The tests that run the profile checker, in protection_test.go, are in
// package codec_test, as the checker imports the codec. These give them
// the fixture store and the corpus.
const (
	Fixture = fixture
	Fact    = fact
	Hand    = hand
)

var (
	Corpus      = corpus
	CopyFixture = copyFixture
	Put         = put
	Restore     = restore
	IsDocument  = isDocument
)

// corpusWant is how each hand edit of the corpus protects. Every file of
// the corpus is listed, so a new one says what it expects.
var corpusWant = map[string]Protection{
	"block-scalar.md":     {},
	"comments.md":         {},
	"crlf.md":             {},
	"fenced.md":           {},
	"flow-mapping.md":     {},
	"no-body.md":          {},
	"no-final-newline.md": {},
	"open-quote.md":       {},
	"quoted-key.md":       {},
	"repeated-key.md":     {},
	"written.md":          {},

	"protect-alias-key.md":      {Locked: true, Verified: true},
	"protect-block-lock.md":     {Locked: true},
	"protect-empty-lock.md":     {Locked: true},
	"protect-explicit-key.md":   {Locked: true},
	"protect-indented-block.md": {Locked: true},
	"protect-key-forms.md":      {Locked: true},
	"protect-locked-comment.md": {Locked: true},
	"protect-merge-key.md":      {Locked: true, Verified: true},
	"protect-quoted-false.md":   {Locked: true},
	"protect-repeated-lock.md":  {Locked: true},
	"protect-unlocked.md":       {},
	"protect-verified-false.md": {Verified: true},
	"protect-verified.md":       {Verified: true},
	"bom.md":                    {Unreadable: true},
	"conflict.md":               {Unreadable: true},
	"empty.md":                  {Unreadable: true},
	"invalid-utf8.md":           {Unreadable: true},
	"long-line.md":              {Unreadable: true},
	"no-frontmatter.md":         {Unreadable: true},
	"nul.md":                    {Unreadable: true},
	"unclosed.md":               {Unreadable: true},
}

// corpus returns each file of the corpus by name.
func corpus(t testing.TB) map[string][]byte {
	t.Helper()
	ents, err := os.ReadDir(corpusDir)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string][]byte{}
	for _, e := range ents {
		b, err := os.ReadFile(filepath.Join(corpusDir, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		out[e.Name()] = b
	}
	return out
}

func TestCorpusIsListed(t *testing.T) {
	files := corpus(t)
	for name := range files {
		if _, ok := corpusWant[name]; !ok {
			t.Errorf("%s: no expected protection", name)
		}
	}
	for name := range corpusWant {
		if _, ok := files[name]; !ok {
			t.Errorf("%s: listed but not in the corpus", name)
		}
	}
	// The corpus holds the hand edits the card names.
	has := func(name, sub string) {
		if !bytes.Contains(files[name], []byte(sub)) {
			t.Errorf("%s does not hold %q", name, sub)
		}
	}
	has("comments.md", "  # ")
	has("comments.md", " # checked")
	has("block-scalar.md", "description: |\n")
	has("flow-mapping.md", "{ from: ")
	has("repeated-key.md", "state: retired")
	has("quoted-key.md", `"title": `)
	has("crlf.md", "\r\n")
	has("fenced.md", "```yaml\n---\n")
	has("fenced.md", "\n## not a section")
	has("bom.md", "\xef\xbb\xbf")
	has("conflict.md", "\n=======\n")
	has("long-line.md", strings.Repeat("x", 100<<10))
	has("open-quote.md", "note: \"and a note that runs on\nstatus: draft\"\n")
	// A key edit next to a value that runs on is refused; the body is not.
	d := Read(files["open-quote.md"])
	if err := d.Set("status", "stable"); !errors.Is(err, ErrNotWritten) {
		t.Errorf("open-quote.md: Set %v, want ErrNotWritten", err)
	}
	if err := d.SetSection("Detail", "x\n"); err != nil {
		t.Errorf("open-quote.md: SetSection %v", err)
	}
	if b := files["no-final-newline.md"]; bytes.HasSuffix(b, []byte("\n")) {
		t.Error("no-final-newline.md ends in a newline")
	}
}

// Write(Read(b)) == b for every hand edit of the corpus, readable or not,
// and ReadFrom reads what Read does.
func TestRoundTripCorpus(t *testing.T) {
	for name, b := range corpus(t) {
		got, err := Write(Read(b))
		if err != nil || !bytes.Equal(got, b) {
			t.Errorf("%s: does not write back its input (err %v)", name, err)
		}
		d, err := ReadFrom(iotest.OneByteReader(bytes.NewReader(b)), int64(len(b)))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if got, _ := Write(d); !bytes.Equal(got, b) {
			t.Errorf("%s: ReadFrom does not write back its input", name)
		}
	}
}

func TestCorpusProtection(t *testing.T) {
	for name, b := range corpus(t) {
		d := Read(b)
		if got, want := d.Protection(), corpusWant[name]; got != want {
			t.Errorf("%s: protection %+v, want %+v", name, got, want)
		}
		if (d.Unreadable() != "") != corpusWant[name].Unreadable {
			t.Errorf("%s: unreadable %q", name, d.Unreadable())
		}
	}
}

// The plan's case: a comment after the value does not undo a lock.
func TestLockedWithAComment(t *testing.T) {
	d := Read([]byte("---\ntype: fact\nlocked: true  # x\n---\nbody\n"))
	if !d.Protection().Locked {
		t.Error("locked: true  # x is not locked")
	}
}

// isDocument reports a path of the store's document classes.
func isDocument(rel string) bool {
	switch {
	case strings.HasPrefix(rel, "legacy/"), strings.Contains(rel, "/notes/"), !strings.HasSuffix(rel, ".md"):
		return false
	case strings.HasPrefix(rel, "systems/"), strings.HasSuffix(rel, "/project.md"):
		return true
	}
	for _, s := range []string{"/items/", "/workstreams/", "/disagreements/", "/sessions/"} {
		if strings.Contains(rel, s) {
			return true
		}
	}
	return false
}

func copyFixture(t testing.TB) string {
	t.Helper()
	root := t.TempDir()
	err := filepath.WalkDir(fixture, func(p string, de fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(fixture, p)
		if de.IsDir() {
			return os.MkdirAll(filepath.Join(root, rel), 0o755)
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(root, rel), b, 0o644)
	})
	if err != nil {
		t.Fatal(err)
	}
	return root
}

func put(t testing.TB, root, rel string, b []byte) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(rel)), b, 0o644); err != nil {
		t.Fatal(err)
	}
}

// restore puts the fixture's own bytes back at each path.
func restore(t testing.TB, root string, rels ...string) {
	t.Helper()
	for _, rel := range rels {
		b, err := os.ReadFile(filepath.Join(fixture, filepath.FromSlash(rel)))
		if err != nil {
			t.Fatal(err)
		}
		put(t, root, rel, b)
	}
}
