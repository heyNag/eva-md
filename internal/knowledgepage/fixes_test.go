package knowledgepage

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/heyNag/eva-md/internal/okfadapter"
)

// A section a person added that okf's save would refuse, a --- rule then a
// "Status:" line, makes the page damaged: its operations are passed over
// with the reason, the save goes on, and the page is still listed and
// recalled, read-only.
func TestSaveSkipsAPageOKFWouldRefuse(t *testing.T) {
	w := fixture(t)
	save(t, w, meta(), addInbox)
	edit(t, w, 1, "\nPart of the [knowledge map](map.md).\n", "\n## Notes\nAgreed in the design review.\n\n---\n\nStatus: confirmed with the team.\n\nPart of the [knowledge map](map.md).\n")
	before := readFile(t, w, "knowledge/k1.md")
	r, err := Save(w, []Op{supersedeInbox, {Action: Drop, ID: 1, Tag: "stated", Reason: "gone", At: at2}}, meta())
	if err != nil || r.Pages != 0 || !reflect.DeepEqual(r.Skipped, []Skip{{0, 1, ReasonDamaged}, {1, 1, ReasonDamaged}}) {
		t.Fatalf("Save = %+v, %v", r, err)
	}
	if readFile(t, w, "knowledge/k1.md") != before {
		t.Fatal("the page was written")
	}
	rows, err := ReadIndexAt(w.Root())
	if err != nil || len(rows) != 1 || !rows[0].ReadOnly || rows[0].Statement != addInbox.Text {
		t.Fatalf("rows %+v, %v", rows, err)
	}
	validate(t, w)
}

// A title or statement a person edited to hold the --- or the line break
// okf's save refuses makes the page damaged: its operation is passed over,
// the rest of the save goes on, and the page is listed read-only.
func TestSaveSkipsATitleOrStatementOKFWouldRefuse(t *testing.T) {
	for _, tc := range []struct{ name, from, to string }{
		{"title dashes", "title: Inbox format\n", "title: Inbox --- format\n"},
		{"title line break", "title: Inbox format\n", "title: \"Inbox\\nformat\"\n"},
		{"statement dashes", "\nThe inbox stores entries as JSONL, one file per session [stated]", "\nThe inbox --- stores entries as JSONL, one file per session [stated]"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := fixture(t)
			save(t, w, meta(), addInbox)
			edit(t, w, 1, tc.from, tc.to)
			before := readFile(t, w, "knowledge/k1.md")
			r, err := Save(w, []Op{{Action: Drop, ID: 1, Tag: "stated", Reason: "gone", At: at2}, addOffline}, meta())
			if err != nil || r.Pages != 1 || !reflect.DeepEqual(r.Skipped, []Skip{{0, 1, ReasonDamaged}}) {
				t.Fatalf("Save = %+v, %v", r, err)
			}
			if readFile(t, w, "knowledge/k1.md") != before {
				t.Fatal("the page was written")
			}
			if readFile(t, w, "knowledge/k2.md") == "" {
				t.Fatal("the add in the same save was lost")
			}
			rows, err := ReadIndexAt(w.Root())
			i := slices.IndexFunc(rows, func(r Row) bool { return r.ID == "k1" })
			if err != nil || len(rows) != 2 || i < 0 || !rows[i].ReadOnly {
				t.Fatalf("rows %+v, %v", rows, err)
			}
		})
	}
}

// When an operation fails after Save wrote a page, the map and the indexes
// are still built for the page, and the error is returned.
func TestSaveRebuildsAfterAFailedWrite(t *testing.T) {
	w := fixture(t)
	save(t, w, meta(), addInbox)
	failed := errors.New("the disk is full")
	update = func(*okfadapter.Wiki, string, string, func(*okfadapter.Concept) error, string) (bool, error) {
		return false, failed
	}
	t.Cleanup(func() { update = (*okfadapter.Wiki).Update })
	r, err := Save(w, []Op{addOffline, supersedeInbox}, meta())
	if !errors.Is(err, failed) || !slices.Equal(r.Created, []int{2}) {
		t.Fatalf("Save = %+v, %v", r, err)
	}
	rows, err := ReadIndexAt(w.Root())
	if err != nil || len(rows) != 2 {
		t.Fatalf("the recall index: %+v, %v", rows, err)
	}
	if m := readFile(t, w, "knowledge/map.md"); !strings.Contains(m, "(k2.md)") {
		t.Fatalf("the map:\n%s", m)
	}
	if !slices.Contains(r.Files, IndexFile) || !slices.Contains(r.Files, "knowledge/map.md") {
		t.Fatalf("files %v", r.Files)
	}
}

// A page a person verified in a form okf reads no entry from, a plain
// value or a block mapping, is never written, and keeps its marker.
func TestVerifiedForms(t *testing.T) {
	w := fixture(t)
	save(t, w, meta(), addInbox, addOffline)
	edit(t, w, 1, "status: stable\n", "verified: true\nstatus: stable\n")
	edit(t, w, 2, "status: stable\n", "verified:\n  by: human/reviewer\n  at: \"2026-10-29T10:00:00Z\"\nstatus: stable\n")
	before := snapshot(t, w)
	r := save(t, w, meta(), supersedeInbox, dropOffline)
	if r.Pages != 0 || !reflect.DeepEqual(r.Skipped, []Skip{{0, 1, ReasonVerified}, {1, 2, ReasonVerified}}) {
		t.Fatalf("Save = %+v", r)
	}
	after := snapshot(t, w)
	for _, f := range []string{"knowledge/k1.md", "knowledge/k2.md"} {
		if after[f] != before[f] {
			t.Errorf("%s was written", f)
		}
	}
	rows, _ := ReadIndexAt(w.Root())
	if len(rows) != 2 || !rows[0].ReadOnly || !rows[1].ReadOnly {
		t.Fatalf("rows %+v", rows)
	}
}

// A person removes the knowledge folder: the recall index and the root
// index's line, which still list its pages, are out of date, and the
// repair leaves the index its header alone and removes the line, without
// making the folder again.
func TestRefreshWithoutTheFolder(t *testing.T) {
	w := fixture(t)
	save(t, w, meta(), addInbox, addOffline)
	if err := os.RemoveAll(filepath.Join(w.Root(), Folder)); err != nil {
		t.Fatal(err)
	}
	r, err := Refresh(w, "test", false)
	if err != nil || !r.Exists || r.Pages != 0 || !slices.Equal(r.Stale, []string{IndexFile, "index.md"}) || r.Rebuilt {
		t.Fatalf("Refresh without repair = %+v, %v", r, err)
	}
	if rows, _ := ReadIndexAt(w.Root()); len(rows) != 2 {
		t.Fatalf("Refresh without repair wrote: %+v", rows)
	}
	if r, err = Refresh(w, "test", true); err != nil || !r.Rebuilt || !slices.Equal(r.Stale, []string{IndexFile, "index.md"}) {
		t.Fatalf("Refresh with repair = %+v, %v", r, err)
	}
	if rows, err := ReadIndexAt(w.Root()); err != nil || len(rows) != 0 || readFile(t, w, IndexFile) != indexHeader+"\n" {
		t.Fatalf("the recall index: %+v, %v", rows, err)
	}
	if strings.Contains(readFile(t, w, "index.md"), "knowledge/index.md") {
		t.Fatalf("the root index:\n%s", readFile(t, w, "index.md"))
	}
	if _, err := os.Stat(filepath.Join(w.Root(), Folder)); !os.IsNotExist(err) {
		t.Fatal("the repair made the folder")
	}
	if r, err = Refresh(w, "test", true); err != nil || !r.Exists || len(r.Stale) != 0 || r.Rebuilt {
		t.Fatalf("Refresh once repaired = %+v, %v", r, err)
	}
	validate(t, w)
}

// The map is eva-md's own: one past a page's read bound is out of date and
// built again, never damaged.
func TestRefreshRebuildsALargeMap(t *testing.T) {
	w := fixture(t)
	save(t, w, meta(), addInbox)
	editFile(t, w, "knowledge/map.md", "# Knowledge map\n", "# Knowledge map\n\n"+strings.Repeat("A line of an old map.\n", (maxPage>>4)+100))
	if info, err := os.Stat(filepath.Join(w.Root(), "knowledge", "map.md")); err != nil || info.Size() <= maxPage {
		t.Fatalf("the map is not past the bound: %v", err)
	}
	r, err := Refresh(w, "test", true)
	if err != nil || r.MapSkip != "" || !slices.Equal(r.Stale, []string{"knowledge/map.md"}) || !r.Rebuilt {
		t.Fatalf("Refresh = %+v, %v", r, err)
	}
	if m := readFile(t, w, "knowledge/map.md"); strings.Contains(m, "A line of an old map.") || !strings.Contains(m, "(k1.md)") {
		t.Fatalf("the map:\n%s", m)
	}
	validate(t, w)
}

// A page a person puts in the folder keeps its line in the folder's index
// through every save and repair, so okf finds it listed.
func TestFolderIndexKeepsOtherPages(t *testing.T) {
	w := fixture(t)
	save(t, w, meta(), addInbox)
	if err := w.Create("knowledge/notes", &okfadapter.Concept{Type: "note", Title: "Notes", Description: "A person's notes", Status: "stable",
		Body: "\nSee the [knowledge map](map.md).\n"}, okfadapter.Actor("human", "reviewer")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(w.Root(), "knowledge", "draft.md"), []byte("No frontmatter.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	save(t, w, meta(), addOffline)
	index := readFile(t, w, "knowledge/index.md")
	for _, l := range []string{"* [Notes](notes.md) - A person's notes\n", "* [draft](draft.md)\n"} {
		if !strings.Contains(index, l) {
			t.Errorf("the folder's index has no %q:\n%s", l, index)
		}
	}
	if r, err := Refresh(w, "test", true); err != nil || len(r.Stale) != 0 {
		t.Fatalf("Refresh = %+v, %v", r, err)
	}
	if err := os.Remove(filepath.Join(w.Root(), "knowledge", "draft.md")); err != nil {
		t.Fatal(err)
	}
	if _, err := Refresh(w, "test", true); err != nil {
		t.Fatal(err)
	}
	validate(t, w)
}

// A recall index the readers refuse, here one with a second hard link, is
// out of date however its bytes read, and the repair writes it again.
func TestRefreshRewritesAnIndexTheReadersRefuse(t *testing.T) {
	w := fixture(t)
	save(t, w, meta(), addInbox)
	file := filepath.Join(w.Root(), filepath.FromSlash(IndexFile))
	if err := os.Link(file, filepath.Join(filepath.Dir(w.Root()), "backup.tsv")); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadIndexAt(w.Root()); err == nil {
		t.Fatal("the readers read a hard-linked index")
	}
	if r, err := Refresh(w, "test", false); err != nil || !slices.Equal(r.Stale, []string{IndexFile}) {
		t.Fatalf("Refresh without repair = %+v, %v", r, err)
	}
	if r, err := Refresh(w, "test", true); err != nil || !r.Rebuilt {
		t.Fatalf("Refresh with repair = %+v, %v", r, err)
	}
	if rows, err := ReadIndexAt(w.Root()); err != nil || len(rows) != 1 {
		t.Fatalf("the repaired index: %+v, %v", rows, err)
	}
}

// A supersede that narrows a page for all projects to the chat's project
// moves the page there, so the brief of that project still lists it.
func TestSupersedeMovesThePage(t *testing.T) {
	w := fixture(t)
	save(t, w, meta(), addOffline)
	narrow := Op{Action: Supersede, ID: 1, Kind: Constraint, Text: "Hooks never make a network call in this repo", Tag: "stated", Scope: ScopeProject,
		Project: other, Refs: []string{"s:4b5c6d7e#4"}, At: at2}
	save(t, w, meta(), narrow)
	if e := entry(t, w, 1); e.Scope != ScopeProject || e.Project != other {
		t.Fatalf("entry %+v", e)
	}
	rows, _ := ReadIndexAt(w.Root())
	if c := Constraints(rows, other); len(c) != 1 || c[0].ID != "k1" {
		t.Fatalf("the constraints of %s: %+v", other, c)
	}
}

// Written gives every line Save writes for an operation, each of which is
// then in the files the save keeps current, so a check of them checks what
// is saved.
func TestWrittenIsWhatSaveWrites(t *testing.T) {
	w := fixture(t)
	for _, op := range []Op{addInbox, addOffline, supersedeInbox, dropOffline} {
		var on Entry
		if op.Action != Add {
			on = entry(t, w, op.ID)
		} else {
			ids, _, _ := listIDs(w)
			on.ID = len(ids) + 1
		}
		lines := Written(op, on, meta())
		save(t, w, meta(), op)
		var saved []string
		for _, f := range []string{"knowledge/" + on.Name() + ".md", "knowledge/index.md", "knowledge/map.md", IndexFile, "log.md"} {
			saved = append(saved, strings.Split(readFile(t, w, f), "\n")...)
		}
		for _, l := range lines {
			if !slices.Contains(saved, l) {
				t.Errorf("%s: %q is in no file the save wrote", op.Action, l)
			}
		}
	}
	if lines := Written(Op{Action: Supersede, Text: "x", Tag: "rumour"}, Entry{ID: 1}, meta()); lines != nil {
		t.Errorf("an invalid operation: %q", lines)
	}
}

// An add is a duplicate only of a page that applies to the save's project:
// one for all projects, or the project's own. Another project's own page
// with the same statement doesn't hold it here.
func TestDuplicatesApplyToTheProject(t *testing.T) {
	w := fixture(t)
	save(t, w, meta(), addInbox, addOffline)
	elsewhere := meta()
	elsewhere.Project = other
	r := save(t, w, elsewhere, addInbox, addOffline)
	if !slices.Equal(r.Created, []int{3}) || !reflect.DeepEqual(r.Skipped, []Skip{{1, 0, ReasonDuplicate}}) {
		t.Fatalf("Save in another project = %+v", r)
	}
	if e := entry(t, w, 3); e.Project != other || e.Statement != addInbox.Text {
		t.Fatalf("k3 = %+v", e)
	}
}
