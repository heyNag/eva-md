package searchindex

import (
	"bytes"
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"testing"
	"time"

	"github.com/heyNag/eva-md/internal/paths"
)

// contents is what an index folder holds, whatever its segments: every
// live document by file and line with its date, length and kind, and every
// term with the live documents holding it and how often.
func contents(t *testing.T, d paths.Dirs) (docs, postings []string) {
	t.Helper()
	root, err := os.OpenRoot(filepath.Join(d.SearchIndex(), "general"))
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	m, err := ReadManifest(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range m.Segments {
		seg, err := OpenSegment(root, s.File)
		if err != nil {
			t.Fatal(err)
		}
		where := make([]string, seg.Docs())
		for doc := range where {
			l, err := seg.Loc(uint32(doc))
			if err != nil {
				t.Fatal(err)
			}
			src := seg.sources[l.Source]
			if !live(m, src) {
				continue
			}
			where[doc] = fmt.Sprintf("%s#%d", src.Name, l.Line)
			ts, dl, kind := seg.Meta(uint32(doc))
			docs = append(docs, fmt.Sprintf("%s ts=%d dl=%d kind=%d", where[doc], ts, dl, kind))
		}
		c := &cursor{s: seg}
		for {
			term, ok, err := c.next()
			if err != nil {
				t.Fatal(err)
			}
			if !ok {
				break
			}
			if err := seg.Postings(term, func(doc, tf uint32) {
				if where[doc] != "" {
					postings = append(postings, fmt.Sprintf("%s %s tf=%d", term.Text, where[doc], tf))
				}
			}); err != nil {
				t.Fatal(err)
			}
		}
		seg.Close()
	}
	sort.Strings(docs)
	sort.Strings(postings)
	return docs, postings
}

// Merging an index built in many refreshes gives what one build of the same
// inbox gives: the same documents, terms and postings.
func TestMergeEqualsRebuild(t *testing.T) {
	d := fixture(t)
	for round := 0; round < 9; round++ {
		for s := 0; s < 3; s++ {
			prompt(t, d, fmt.Sprintf("merge-%d", s), round*10+s, fmt.Sprintf("round%d session%d MergeWord common %d", round, s, round*s))
			say(t, d, fmt.Sprintf("merge-%d", s), round*10+s, "tool", "tool", fmt.Sprintf("Bash: go test ./pkg%d (exit 0)", round))
		}
		refreshOK(t, d, Options{})
	}
	if m := readManifest(t, d, "general"); len(m.Segments) != 9 {
		t.Fatalf("%d segments before the merge", len(m.Segments))
	}
	catchupOK(t, d)
	m := readManifest(t, d, "general")
	if len(m.Segments) > maxSegments || len(m.Retired) == 0 {
		t.Fatalf("after the catch-up: %d segments, %d retired", len(m.Segments), len(m.Retired))
	}
	mergedDocs, mergedPostings := contents(t, d)
	if _, err := Rebuild(context.Background(), d, "index"); err != nil {
		t.Fatal(err)
	}
	builtDocs, builtPostings := contents(t, d)
	if !slices.Equal(mergedDocs, builtDocs) || !slices.Equal(mergedPostings, builtPostings) || len(builtDocs) != 54 {
		t.Fatalf("merged and built differ: %d and %d documents, %d and %d postings", len(mergedDocs), len(builtDocs), len(mergedPostings), len(builtPostings))
	}
}

// A merge drops the documents of a file's earlier generations, and their
// terms with them, and the manifest forgets a file gone once no segment
// names it.
func TestMergeDropsDeadGenerations(t *testing.T) {
	d := fixture(t)
	prompt(t, d, "dead", 1, "oldtext tangerine, a longer line")
	prompt(t, d, "gone", 2, "gonetext lime")
	refreshOK(t, d, Options{})
	if err := os.Truncate(sessionPath(d, "dead"), 0); err != nil {
		t.Fatal(err)
	}
	prompt(t, d, "dead", 3, "newtext tangerine") // shorter: the file shrank
	if err := os.Remove(sessionPath(d, "gone")); err != nil {
		t.Fatal(err)
	}
	refreshOK(t, d, Options{})
	// Two segments, the first now all dead: the policy merges it away.
	catchupOK(t, d)
	m := readManifest(t, d, "general")
	terms := dictionary(t, d)
	if slices.Contains(terms, "oldtext") || slices.Contains(terms, "gonetext") || !slices.Contains(terms, "newtext") {
		t.Fatalf("terms after the merge: %q", terms)
	}
	if _, ok := m.Files["codex-gone.jsonl"]; ok || m.Files["codex-dead.jsonl"].Gen != 2 {
		t.Fatalf("files after the merge: %+v", m.Files)
	}
	docs, _ := contents(t, d)
	if len(docs) != 1 {
		t.Fatalf("documents after the merge: %q", docs)
	}
}

// The policy merges every small segment once there are more than 4, the
// smallest once there are more than 8, and any holding a dead document; a
// lone segment with nothing dead stays.
func TestMergePolicy(t *testing.T) {
	seg := func(i int, mb int64, docs int) SegmentInfo {
		return SegmentInfo{File: fmt.Sprintf("seg-%016x.idx", i), Bytes: mb << 20, Docs: docs}
	}
	m := Manifest{}
	for i := 0; i < 4; i++ {
		m.Segments = append(m.Segments, seg(i, 1, 10))
	}
	if got := mergeSet(m, nil); got != nil {
		t.Fatalf("4 small segments: %q", got)
	}
	m.Segments = append(m.Segments, seg(4, 2, 10))
	if got := mergeSet(m, nil); len(got) != 5 {
		t.Fatalf("5 small segments: %q", got)
	}
	m.Segments = nil
	for i := 0; i < 9; i++ {
		m.Segments = append(m.Segments, seg(i, int64(10+i), 10))
	}
	if got := mergeSet(m, nil); !slices.Equal(got, []string{m.Segments[0].File, m.Segments[1].File}) {
		t.Fatalf("9 large segments: %q", got)
	}
	m.Segments = []SegmentInfo{seg(0, 20, 100), seg(1, 20, 100)}
	if got := mergeSet(m, map[string]int{m.Segments[1].File: 1}); !slices.Equal(got, []string{m.Segments[1].File}) {
		t.Fatalf("one dead: %q", got)
	}
	if got := mergeSet(m, map[string]int{m.Segments[1].File: 0}); got != nil {
		t.Fatalf("none dead: %q", got)
	}
}

// A session file deleted, here the one line of 41 that held a word, takes
// its words out of the segments the manifest names at the next catch-up,
// and out of the folder once the segment it retired is pruned.
func TestDeletedSessionLeavesNoWords(t *testing.T) {
	d := fixture(t)
	for i := 0; i < 40; i++ {
		prompt(t, d, "keep", i, fmt.Sprintf("Ordinary chat line %d about the export", i))
	}
	prompt(t, d, "leak", 50, "the wifi passphrase is zanzibarquokka77")
	catchupOK(t, d)
	if err := os.Remove(sessionPath(d, "leak")); err != nil {
		t.Fatal(err)
	}
	catchupOK(t, d)
	root, err := os.OpenRoot(filepath.Join(d.SearchIndex(), "general"))
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	holds := func(names []string) string {
		t.Helper()
		for _, name := range names {
			data, err := root.ReadFile(name)
			if err != nil {
				t.Fatal(err)
			}
			if bytes.Contains(data, []byte("zanzibarquokka")) {
				return name
			}
		}
		return ""
	}
	m, err := ReadManifest(root)
	if err != nil || len(m.Segments) == 0 {
		t.Fatal(m, err)
	}
	var named []string
	for _, s := range m.Segments {
		named = append(named, s.File)
	}
	if seg := holds(named); seg != "" {
		t.Fatalf("%s still holds a word of the deleted session", seg)
	}
	if _, err := prune(root, &m, time.Now().Add(PruneAge+time.Minute)); err != nil {
		t.Fatal(err)
	}
	all, err := fs.Glob(root.FS(), "seg-*.idx")
	if err != nil {
		t.Fatal(err)
	}
	if seg := holds(all); seg != "" {
		t.Fatalf("%s still holds a word of the deleted session once pruned", seg)
	}
}

// Pruning keeps what a reader may still open: the segments the manifest
// names, one retired less than 10 minutes ago, and a file not named that
// changed less than 10 minutes ago; it deletes the rest of the segments
// and temporary files.
func TestPruneKeepsRecentSegments(t *testing.T) {
	dir := t.TempDir()
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	now := time.Now()
	old := now.Add(-PruneAge - time.Minute)
	name := func(i int) string { return fmt.Sprintf("seg-%016x.idx", i) }
	files := map[string]time.Time{
		name(1):                  old, // named
		name(2):                  old, // retired a minute ago
		name(3):                  old, // retired long ago
		name(4):                  now, // named nowhere, new
		name(5):                  old, // named nowhere, old
		paths.TempPrefix + "new": now,
		paths.TempPrefix + "old": old,
		"notes.txt":              old, // not ours
	}
	for f, mod := range files {
		if err := os.WriteFile(filepath.Join(dir, f), nil, 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(filepath.Join(dir, f), mod, mod); err != nil {
			t.Fatal(err)
		}
	}
	m := Manifest{Segments: []SegmentInfo{{File: name(1)}}, Retired: []Retired{{File: name(2), At: now.Add(-time.Minute)}, {File: name(3), At: old}, {File: name(9), At: old}}}
	changed, err := prune(root, &m, now)
	if err != nil || !changed {
		t.Fatal(changed, err)
	}
	var left []string
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		left = append(left, e.Name())
	}
	want := []string{paths.TempPrefix + "new", "notes.txt", name(1), name(2), name(4)}
	sort.Strings(want)
	if !slices.Equal(left, want) || len(m.Retired) != 1 || m.Retired[0].File != name(2) {
		t.Fatalf("left %q, retired %+v", left, m.Retired)
	}
}
