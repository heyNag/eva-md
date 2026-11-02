package knowledgepage

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

// TestIndexBoundsAndOrder checks the recall index's order, newest as_of
// first, and its bound: past MaxIndex the oldest active pages are left out,
// retired pages are never listed, and what is written parses back.
func TestIndexBoundsAndOrder(t *testing.T) {
	isolate(t)
	var entries []Entry
	long := strings.Repeat("word ", 59) + "end"
	for i := 1; i <= 3000; i++ {
		e := Entry{ID: i, Kind: Fact, Title: fmt.Sprintf("Title %d", i), Statement: fmt.Sprintf("Statement %d %s", i, long), Tag: "observed",
			Scope: ScopeProject, Project: project, State: Active, AsOf: time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC).AddDate(0, 0, i).Format(time.DateOnly)}
		if i%10 == 0 {
			e.State = Retired
		}
		entries = append(entries, e)
	}
	text, listed, leftOut := indexText(entries)
	if len(text) > MaxIndex || leftOut == 0 || listed+leftOut != 2700 {
		t.Fatalf("index of %d bytes lists %d and leaves out %d", len(text), listed, leftOut)
	}
	rows := ParseIndex([]byte(text))
	if len(rows) != listed {
		t.Fatalf("parsed %d rows of %d", len(rows), listed)
	}
	for i, r := range rows {
		if i > 0 && r.AsOf > rows[i-1].AsOf {
			t.Fatalf("row %d (%s) is newer than the row before it", i, r.AsOf)
		}
		if id, _ := ParseID(r.ID); id%10 == 0 {
			t.Fatalf("a retired page is listed: %s", r.ID)
		}
	}
	if rows[0].ID != "k2999" {
		t.Errorf("the first row is %s, want the newest page, k2999", rows[0].ID)
	}
	// The left-out pages are the oldest: every listed page is newer than any
	// left out.
	oldest := rows[len(rows)-1].AsOf
	n := 0
	for _, e := range entries {
		if e.State == Active && e.AsOf < oldest {
			n++
		}
	}
	if n != leftOut {
		t.Errorf("%d active pages are older than the oldest listed, want the %d left out", n, leftOut)
	}

	// Equal as_of: the newer time, then the larger id, first; a field's tabs
	// and line breaks become spaces; a path with a comma is left out.
	same := []Entry{
		{ID: 1, Kind: Fact, Title: "A", Statement: "a\tb\nc", Tag: "stated", Scope: ScopeAll, Project: project, State: Active, AsOf: "2026-10-28", Paths: []string{"x,y", "z"}},
		{ID: 2, Kind: Fact, Title: "B", Statement: "b", Tag: "stated", Scope: ScopeAll, Project: project, State: Active, AsOf: "2026-10-28", At: at1},
		{ID: 3, Kind: Fact, Title: "C", Statement: "c", Tag: "stated", Scope: ScopeAll, Project: project, State: Active, AsOf: "2026-10-28"},
	}
	text, _, _ = indexText(same)
	want := "# eva-md knowledge index 1\n" +
		"k2\tfact\tstated\tall\t" + project + "\t2026-10-28\t2026-10-28T10:00:00+01:00\t\tB\tb\t\n" +
		"k3\tfact\tstated\tall\t" + project + "\t2026-10-28\t\t\tC\tc\t\n" +
		"k1\tfact\tstated\tall\t" + project + "\t2026-10-28\t\t\tA\ta b c\tz\n"
	if text != want {
		t.Errorf("index =\n%q\nwant\n%q", text, want)
	}
}

// TestParseIndex checks that the reader skips every line that is not a row
// and reads nothing of a file in another format.
func TestParseIndex(t *testing.T) {
	isolate(t)
	good := "k12\tdecision\tstated\tproject\t" + project + "\t2026-10-28\t2026-10-28T10:00:00+01:00\tread-only\tTitle\tStatement\ta/b.go,c"
	data := strings.Join([]string{
		"# eva-md knowledge index 1",
		good,
		"",
		"k01\tfact\tstated\tproject\tp\t2026-10-28\t\t\tT\tS\t",
		"k1\trumour\tstated\tproject\tp\t2026-10-28\t\t\tT\tS\t",
		"k1\tfact\tpasted\tproject\tp\t2026-10-28\t\t\tT\tS\t",
		"k1\tfact\tstated\tteam\tp\t2026-10-28\t\t\tT\tS\t",
		"k1\tfact\tstated\tproject\t\t2026-10-28\t\t\tT\tS\t",
		"k1\tfact\tstated\tproject\tp\t28-10-2026\t\t\tT\tS\t",
		"k1\tfact\tstated\tproject\tp\t2026-10-28\tyesterday\t\tT\tS\t",
		"k1\tfact\tstated\tproject\tp\t2026-10-28\t\twritable\tT\tS\t",
		"k1\tfact\tstated\tproject\tp\t2026-10-28\t\t\t\tS\t",
		"k1\tfact\tstated\tproject\tp\t2026-10-28\t\t\tT\t\t",
		"k1\tfact\tstated\tproject\tp\t2026-10-28\t\t\tT\tS",
		"k1\tfact\tstated\tproject\tp\t2026-10-28\t\t\tT\tS\t\textra",
		"k2\tfact\tstated\tall\tp\t2026-10-28\t\t\tT\tS\t",
	}, "\n") + "\n"
	rows := ParseIndex([]byte(data))
	want := []Row{
		{ID: "k12", Kind: Decision, Tag: "stated", Scope: ScopeProject, Project: project, AsOf: "2026-10-28", At: at1, ReadOnly: true, Title: "Title", Statement: "Statement", Paths: []string{"a/b.go", "c"}},
		{ID: "k2", Kind: Fact, Tag: "stated", Scope: ScopeAll, Project: "p", AsOf: "2026-10-28", Title: "T", Statement: "S"},
	}
	if !reflect.DeepEqual(rows, want) {
		t.Errorf("rows =\n%+v\nwant\n%+v", rows, want)
	}
	if rows := ParseIndex([]byte("# eva-md knowledge index 2\n" + good + "\n")); rows != nil {
		t.Errorf("another format read as %+v", rows)
	}
	if rows := ParseIndex([]byte(good + "\n")); rows != nil {
		t.Errorf("a file without the header read as %+v", rows)
	}
	if rows[0].Ref() != "knowledge/k12" {
		t.Errorf("ref = %q", rows[0].Ref())
	}
}

// TestReadIndexStrict checks that the hook's and the brief's read takes the
// index by its path only, as a regular file with no link on its path, one
// hard link, within its bound.
func TestReadIndexStrict(t *testing.T) {
	home := isolate(t)
	wiki := filepath.Join(home, "wiki")
	hidden := filepath.Join(wiki, ".eva-md")
	if err := os.MkdirAll(hidden, 0o700); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(wiki, filepath.FromSlash(IndexFile))
	if _, err := ReadIndexAt(wiki); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("a missing index: %v", err)
	}
	text := "# eva-md knowledge index 1\nk1\tfact\tstated\tall\tp\t2026-10-28\t\t\tT\tS\t\n"
	if err := os.WriteFile(file, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	if rows, err := ReadIndexAt(wiki); err != nil || len(rows) != 1 {
		t.Fatalf("ReadIndexAt = %+v, %v", rows, err)
	}
	if err := os.Link(file, filepath.Join(home, "second")); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadIndexAt(wiki); err == nil {
		t.Error("read a file with two hard links")
	}
	if err := os.Remove(filepath.Join(home, "second")); err != nil {
		t.Fatal(err)
	}
	elsewhere := filepath.Join(home, "elsewhere.tsv")
	if err := os.WriteFile(elsewhere, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(file); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(elsewhere, file); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadIndexAt(wiki); err == nil {
		t.Error("read through a symlinked file")
	}
	if err := os.RemoveAll(hidden); err != nil {
		t.Fatal(err)
	}
	real := filepath.Join(home, "real")
	if err := os.MkdirAll(real, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(real, "knowledge.tsv"), []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(real, hidden); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadIndexAt(wiki); err == nil {
		t.Error("read through a symlinked folder")
	}
	if err := os.Remove(hidden); err != nil {
		t.Fatal(err)
	}
	// A relative link that stays inside the wiki gets past the pinned root,
	// so only the index's own check on its path refuses it.
	inside := filepath.Join(wiki, ".eva-md.real")
	if err := os.MkdirAll(inside, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(inside, "knowledge.tsv"), []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(".eva-md.real", hidden); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadIndexAt(wiki); err == nil || !strings.Contains(err.Error(), "linked parent") {
		t.Errorf("read through a folder linked inside the wiki: %v", err)
	}
	if err := os.Remove(hidden); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(hidden, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, []byte(text+strings.Repeat("x", MaxIndex)), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadIndexAt(wiki); err == nil {
		t.Error("read an index over its bound")
	}
	// An index of exactly its bound is read, and one byte more is refused.
	full := text + strings.Repeat("x", MaxIndex-len(text)-1) + "\n"
	if err := os.WriteFile(file, []byte(full), 0o600); err != nil {
		t.Fatal(err)
	}
	if rows, err := ReadIndexAt(wiki); err != nil || len(rows) != 1 {
		t.Errorf("an index of %d bytes: %+v, %v", len(full), rows, err)
	}
	if err := os.WriteFile(file, []byte(full+"x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadIndexAt(wiki); err == nil {
		t.Error("read an index one byte over its bound")
	}
	if err := os.Remove(file); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(file, 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadIndexAt(wiki); err == nil {
		t.Error("read a folder")
	}
}

// TestParseID checks the forms a page's number is read from.
func TestParseID(t *testing.T) {
	isolate(t)
	for s, want := range map[string]int{"k1": 1, "k12.md": 12, "knowledge/k12": 12, "knowledge/k12.md": 12, "k999999999": 999999999} {
		if got, ok := ParseID(s); !ok || got != want {
			t.Errorf("ParseID(%q) = %d, %v, want %d", s, got, ok, want)
		}
	}
	for _, s := range []string{"", "k", "k0", "k01", "K1", "k1a", "k-1", "k1000000000", "systems/k1", "knowledge/map", "k1.txt"} {
		if _, ok := ParseID(s); ok {
			t.Errorf("ParseID(%q) read a number", s)
		}
	}
	if Name(7) != "k7" || Ref(7) != "knowledge/k7" {
		t.Errorf("Name and Ref = %q, %q", Name(7), Ref(7))
	}
}

// TestLenientHandEdits checks the search reader on pages a person edited:
// it passes over what it doesn't know rather than refusing the page.
func TestLenientHandEdits(t *testing.T) {
	isolate(t)
	page := "---\ntype: knowledge\ntitle: 'It''s a title'\ndescription: The statement from the description\nkind: rumour\nscope: \"all\"\nstate: retired\nproject: p\nas_of: soon\nlocked: ON\nsources:\n  - resource: \"s:3f2a91c0#1\"\n  - resource: https://example.com/doc\n  - resource: \"" + project + "@" + commit + ":internal/inbox/\"\n---\n\n# A heading a person added\n\n## Statement\n\nStatement text [inferred] s:3f2a91c0#1\nA second line a person added.\n\n## History\nnot a list line\n- ~~old~~ (retired 2026-10-01) [stated]\n\n## Notes\nKept.\n"
	e, ok := Lenient([]byte(page))
	want := Entry{Title: "It's a title", Statement: "Statement text", Tag: "inferred", Scope: ScopeAll, State: Retired, Project: "p", Locked: true,
		Refs: []string{"s:3f2a91c0#1"}, Sources: []string{"s:3f2a91c0#1"}, Code: []string{project + "@" + commit + ":internal/inbox/"}, Paths: []string{"internal/inbox"},
		History: []string{"- ~~old~~ (retired 2026-10-01) [stated]"}}
	if !ok || !reflect.DeepEqual(e, want) {
		t.Errorf("Lenient =\n%+v\nwant\n%+v", e, want)
	}
	noBody := strings.Replace(page, "## Statement", "## Elsewhere", 1)
	if e, ok := Lenient([]byte(noBody)); !ok || e.Statement != "The statement from the description" {
		t.Errorf("without a statement line: %+v, %v", e, ok)
	}
	for _, bad := range []string{"", "no frontmatter", "---\ntype: thread\ntitle: T\ndescription: D\n---\n", "---\ntype: knowledge\ndescription: D\n---\n", "---\ntype: knowledge\ntitle: T\n"} {
		if _, ok := Lenient([]byte(bad)); ok {
			t.Errorf("Lenient read %q", bad)
		}
	}
}
