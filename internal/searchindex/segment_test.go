package searchindex

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/heyNag/eva-md/internal/inbox"
)

// corpus is a block of synthetic documents, one source holding them all,
// document i dated base plus i minutes, its kind cycling through the four,
// its line 100 bytes long at offset 101·i.
func corpus(texts []string) *builder {
	b := newBuilder()
	src := b.source("codex-seg.jsonl", 3, 0)
	for i, text := range texts {
		kind := []string{"prompt", "reply", "tool", "plan"}[i%4]
		b.add(inbox.Line{Entry: inbox.Entry{Text: text, Kind: kind, TS: base.Add(time.Duration(i) * time.Minute).Format(time.RFC3339)}, Offset: int64(i) * 101, Length: 100, Line: i + 1}, src)
	}
	b.sources[src].To = int64(len(texts)) * 101
	return b
}

// writeCorpus writes the corpus as a segment in a fresh folder and returns
// the folder, opened, and the segment's name.
func writeCorpus(t *testing.T, texts []string) (*os.Root, string) {
	t.Helper()
	root, err := os.OpenRoot(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { root.Close() })
	info, err := writeSegment(context.Background(), root, corpus(texts).input())
	if err != nil {
		t.Fatal(err)
	}
	return root, info.File
}

// model is what a segment of texts must hold: each token's documents and
// counts, and each document's length.
func model(texts []string) (map[string][]posting, []uint16) {
	terms := map[string][]posting{}
	var dl []uint16
	for i, text := range texts {
		tf := map[string]uint32{}
		dl = append(dl, uint16(Tokenize(text, func(s string) { tf[s]++ })))
		for tok, n := range tf {
			terms[tok] = append(terms[tok], posting{doc: uint32(i), tf: n})
		}
	}
	return terms, dl
}

func texts300() []string {
	var out []string
	for i := 0; i < 300; i++ {
		out = append(out, fmt.Sprintf("word%03d common %s ReadMax%d gpt%d word%03d", i%160, strings.Repeat("echo ", i%3), i%5, i%9, (i*7)%160))
	}
	return out
}

// A segment reads back exactly what was written: every term in byte order
// with its documents and counts, across dictionary blocks, every document's
// date, length, kind and location, across location blocks, and its source
// with its run of documents.
func TestSegmentRoundTrip(t *testing.T) {
	texts := texts300()
	root, name := writeCorpus(t, texts)
	seg, err := OpenSegment(root, name)
	if err != nil {
		t.Fatal(err)
	}
	defer seg.Close()
	if err := seg.Verify(); err != nil {
		t.Fatal(err)
	}
	terms, dl := model(texts)
	if seg.Docs() != len(texts) || len(seg.Sources()) != 1 || !slices.Equal(seg.Sources()[0].Runs, [][2]uint32{{0, 300}}) || seg.Sources()[0].Gen != 3 {
		t.Fatalf("documents %d, sources %+v", seg.Docs(), seg.Sources())
	}
	for i := range texts {
		ts, l, kind := seg.Meta(uint32(i))
		loc, err := seg.Loc(uint32(i))
		if err != nil || ts != uint32(base.Add(time.Duration(i)*time.Minute).Unix()) || l != dl[i] || kind != uint8(i%4+1) || loc != (DocLoc{Source: 0, Offset: int64(i) * 101, Length: 100, Line: i + 1}) {
			t.Fatalf("document %d: ts %d, length %d, kind %d, location %+v, %v", i, ts, l, kind, loc, err)
		}
	}
	var want []string
	for tok := range terms {
		want = append(want, tok)
	}
	sort.Strings(want)
	if len(want) <= dictBlock {
		t.Fatalf("only %d terms, so no second dictionary block", len(want))
	}
	c := &cursor{s: seg}
	for _, tok := range want {
		got, ok, err := c.next()
		if err != nil || !ok || got.Text != tok || got.DF != len(terms[tok]) {
			t.Fatalf("term %q: %+v %v %v", tok, got, ok, err)
		}
		var ps []posting
		if err := seg.Postings(got, func(doc, tf uint32) { ps = append(ps, posting{doc, tf}) }); err != nil {
			t.Fatal(err)
		}
		if !slices.Equal(ps, terms[tok]) {
			t.Fatalf("%q's postings: %v, want %v", tok, ps, terms[tok])
		}
		var exact []string
		if err := seg.Lookup(tok, false, func(t Term) bool { exact = append(exact, t.Text); return true }); err != nil || !slices.Equal(exact, []string{tok}) {
			t.Fatalf("lookup %q: %q %v", tok, exact, err)
		}
	}
	if _, ok, err := c.next(); ok || err != nil {
		t.Fatalf("a term past the last: %v %v", ok, err)
	}
	var prefixed, wantPrefixed []string
	for _, tok := range want {
		if strings.HasPrefix(tok, "word1") {
			wantPrefixed = append(wantPrefixed, tok)
		}
	}
	if err := seg.Lookup("word1", true, func(t Term) bool { prefixed = append(prefixed, t.Text); return true }); err != nil || !slices.Equal(prefixed, wantPrefixed) {
		t.Fatalf("prefix word1: %q, want %q, %v", prefixed, wantPrefixed, err)
	}
}

// Lookups at the edges of the 128-term dictionary blocks: the last term of
// a block, the first of the next, a prefix across them, and terms before
// the first, between two and after the last.
func TestDictionaryBlockEdges(t *testing.T) {
	var texts []string
	for i := 0; i < 130; i++ {
		texts = append(texts, fmt.Sprintf("t%03d", i))
	}
	root, name := writeCorpus(t, texts)
	seg, err := OpenSegment(root, name)
	if err != nil {
		t.Fatal(err)
	}
	defer seg.Close()
	look := func(tok string, prefix bool) []string {
		var out []string
		if err := seg.Lookup(tok, prefix, func(t Term) bool { out = append(out, t.Text); return true }); err != nil {
			t.Fatal(err)
		}
		return out
	}
	for _, tok := range []string{"t000", "t127", "t128", "t129"} {
		if got := look(tok, false); !slices.Equal(got, []string{tok}) {
			t.Fatalf("%s: %q", tok, got)
		}
	}
	for _, tok := range []string{"a", "t0005", "t130", "zzz"} {
		if got := look(tok, false); got != nil {
			t.Fatalf("%s found %q", tok, got)
		}
	}
	if got := look("t12", true); !slices.Equal(got, []string{"t120", "t121", "t122", "t123", "t124", "t125", "t126", "t127", "t128", "t129"}) {
		t.Fatalf("prefix t12: %q", got)
	}
	// t000 is a run of a letter and digits, so t and 000 are terms too.
	if got := look("t", true); len(got) != 131 || got[0] != "t" {
		t.Fatalf("prefix t: %d terms", len(got))
	}
}

// exercise reads all of a segment that can be read, as a search and a
// merge would, and returns the first error.
func exercise(seg *Segment) error {
	var first error
	keep := func(err error) {
		if first == nil {
			first = err
		}
	}
	c := &cursor{s: seg}
	for {
		t, ok, err := c.next()
		if err != nil {
			keep(err)
			break
		}
		if !ok {
			break
		}
		keep(seg.Postings(t, func(uint32, uint32) {}))
		keep(seg.Lookup(t.Text, true, func(Term) bool { return true }))
	}
	for d := 0; d < seg.Docs(); d++ {
		_, err := seg.Loc(uint32(d))
		keep(err)
	}
	return first
}

// Every single byte changed in a segment is caught: OpenSegment refuses it,
// or a full Verify does, always as ErrDamaged, and no read of a changed
// segment panics.
func TestSegmentByteFlips(t *testing.T) {
	var texts []string
	for i := 0; i < 40; i++ {
		texts = append(texts, fmt.Sprintf("flip%d SecretLines %d", i%11, i))
	}
	root, name := writeCorpus(t, texts)
	data, err := root.ReadFile(name)
	if err != nil {
		t.Fatal(err)
	}
	const changed = "seg-00000000000000ff.idx"
	if err := root.WriteFile(changed, data, 0o600); err != nil {
		t.Fatal(err)
	}
	f, err := root.OpenFile(changed, os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	step := 1
	if raceOn {
		step = 7
	}
	for i := 0; i < len(data); i += step {
		if _, err := f.WriteAt([]byte{data[i] ^ 0x5a}, int64(i)); err != nil {
			t.Fatal(err)
		}
		seg, err := OpenSegment(root, changed)
		if err == nil {
			readErr := exercise(seg)
			verifyErr := seg.Verify()
			seg.Close()
			if !errors.Is(verifyErr, ErrDamaged) || readErr != nil && !errors.Is(readErr, ErrDamaged) {
				t.Fatalf("byte %d of %d changed: verify %v, reads %v", i, len(data), verifyErr, readErr)
			}
		} else if !errors.Is(err, ErrDamaged) {
			t.Fatalf("byte %d: %v", i, err)
		}
		if _, err := f.WriteAt(data[i:i+1], int64(i)); err != nil {
			t.Fatal(err)
		}
	}
}

// FuzzSegmentReader opens arbitrary bytes as a segment and reads what it
// can: it may refuse them, never panic.
func FuzzSegmentReader(f *testing.F) {
	root, err := os.OpenRoot(f.TempDir())
	if err != nil {
		f.Fatal(err)
	}
	defer root.Close()
	info, err := writeSegment(context.Background(), root, corpus([]string{"alpha beta", "gamma DeltaEcho 42", "alpha"}).input())
	if err != nil {
		f.Fatal(err)
	}
	valid, err := root.ReadFile(info.File)
	if err != nil {
		f.Fatal(err)
	}
	f.Add(valid)
	f.Add(valid[:len(valid)-1])
	f.Add([]byte(magic))
	f.Fuzz(func(t *testing.T, data []byte) {
		dir := t.TempDir()
		name := "seg-00000000000000aa.idx"
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
		r, err := os.OpenRoot(dir)
		if err != nil {
			t.Fatal(err)
		}
		defer r.Close()
		seg, err := OpenSegment(r, name)
		if err != nil {
			return
		}
		defer seg.Close()
		_ = exercise(seg)
		_ = seg.Verify()
	})
}
