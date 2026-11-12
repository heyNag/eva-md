package migrate

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/heyNag/eva-md/internal/paths"
)

// indexOnly is a made-up step 1 to 2 that writes index.md and nothing
// else, so the wiki keeps files no run of it touches.
var indexOnly = Step{From: 1, To: 2, MinReader: 2, Classes: []string{"index"}, Plan: func(Tree) (Change, error) {
	return Change{Write: map[string][]byte{"index.md": []byte(newIndex)}, Counts: Counts{Written: map[string]int{"index": 1}}}, nil
}}

// fileToFolder is a made-up step 1 to 2 that turns the file log.md into a
// folder of that name.
var fileToFolder = Step{From: 1, To: 2, MinReader: 2, Classes: []string{"log"}, Plan: func(t Tree) (Change, error) {
	b, err := t.Read("log.md")
	return Change{Write: map[string][]byte{"log.md/entries.md": b}, Remove: []string{"log.md"}, Counts: Counts{Read: map[string]int{"log": 1}, Written: map[string]int{"log": 1}, Removed: 1}}, err
}}

// TestRecoveryTurnsAFolderBackIntoAFile cuts short a step that made a
// folder where a file was: the rerun removes the folder, puts the file
// back, and gives the clean run's tree.
func TestRecoveryTurnsAFolderBackIntoAFile(t *testing.T) {
	isolate(t)
	points, want := record(t, fileToFolder)
	dir := v1Wiki(t, v1Files)
	o := machine(t, writerB, "rerun", passB)
	crashAt(t, dir, o, "file-to-folder", slices.Index(points, "git add"))
	if info, err := os.Stat(filepath.Join(dir, "log.md")); err != nil || !info.IsDir() {
		t.Fatalf("the cut run did not make the folder: %v", err)
	}
	r, err := Run(bg(), dir, fileToFolder, o)
	if err != nil || !r.Committed || r.Pass != passA {
		t.Fatalf("Run = %+v, %v; want a commit under the first pass", r, err)
	}
	if got := treeID(t, dir); got != want {
		t.Fatalf("tree %s, want the clean run's %s", got, want)
	}
}

// record runs step on a fresh copy of v1Files and returns the crash points
// it passed, in order, and the clean run's tree id.
func record(t *testing.T, step Step) ([]string, string) {
	t.Helper()
	dir := v1Wiki(t, v1Files)
	var points []string
	hook = func(p string) { points = append(points, p) }
	_, err := Run(bg(), dir, step, machine(t, writerA, "first", passA))
	hook = func(string) {}
	if err != nil {
		t.Fatal(err)
	}
	return points, treeID(t, dir)
}

// TestRecoveryKeepsAPersonsEdit cuts a run short once it has written its
// files, then has a person change a file the step never touches and add
// one: the rerun puts back only the step's own paths and the runner's,
// keeps the person's work, and refuses the tree as dirty. Once the
// person's changes are gone, it finishes under the first pass.
func TestRecoveryKeepsAPersonsEdit(t *testing.T) {
	isolate(t)
	points, want := record(t, indexOnly)
	dir := v1Wiki(t, v1Files)
	before := gitT(t, dir, "rev-parse", "HEAD")
	o := machine(t, writerB, "rerun", passB)
	crashAt(t, dir, o, "index-only", slices.Index(points, "git add"))
	if b, err := os.ReadFile(filepath.Join(dir, "index.md")); err != nil || string(b) != newIndex {
		t.Fatalf("the cut run did not write index.md: %q, %v", b, err)
	}

	const edit = "## [2026-11-02] a person's own line\n"
	writeT(t, filepath.Join(dir, "log.md"), edit)
	writeT(t, filepath.Join(dir, "general", "draft.md"), "a draft\n")
	if r, err := Run(bg(), dir, indexOnly, o); !errors.Is(err, ErrDirty) || r.Committed {
		t.Fatalf("Run = %+v, %v; want ErrDirty", r, err)
	}
	if b, err := os.ReadFile(filepath.Join(dir, "log.md")); err != nil || string(b) != edit {
		t.Fatalf("the person's edit is gone: %q, %v", b, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "general", "draft.md")); err != nil {
		t.Fatalf("the person's new file is gone: %v", err)
	}
	for _, p := range []string{"index.md", ".gitattributes", ".gitignore", ".eva-md/schema.toml"} {
		if b, err := os.ReadFile(filepath.Join(dir, p)); err != nil || string(b) != v1Files[p] {
			t.Errorf("%s is not as it was: %q, %v", p, b, err)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, ".eva-md", "store.toml")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("the cut run's store.toml is still there: %v", err)
	}
	if got := gitT(t, dir, "status", "--porcelain", "--untracked-files=all"); got != "M log.md\n?? general/draft.md" {
		t.Errorf("status =\n%s\nwant only the person's changes", got)
	}
	if got := gitT(t, dir, "rev-parse", "HEAD"); got != before {
		t.Fatal("HEAD moved")
	}
	if _, err := os.Stat(filepath.Join(o.StateDir, journalFile)); err != nil {
		t.Fatalf("the journal went with the refusal: %v", err)
	}

	gitT(t, dir, "checkout", "--", "log.md")
	if err := os.Remove(filepath.Join(dir, "general", "draft.md")); err != nil {
		t.Fatal(err)
	}
	r, err := Run(bg(), dir, indexOnly, o)
	if err != nil || !r.Committed || r.Pass != passA {
		t.Fatalf("Run = %+v, %v; want a commit under the first pass", r, err)
	}
	if got := treeID(t, dir); got != want {
		t.Fatalf("tree %s, want the clean run's %s", got, want)
	}
}

// TestRecoveryClearsWhatACutGitLeft leaves what a run cut short inside git
// or inside a write would: lock files, a temporary file holding part of a
// write (a write is atomic, so no file it names is ever half written), and
// files written, staged and removed. The rerun clears them and gives the
// clean run's tree under the journal's pass.
func TestRecoveryClearsWhatACutGitLeft(t *testing.T) {
	isolate(t)
	_, want := record(t, synthetic)
	dir := v1Wiki(t, v1Files)
	before := gitT(t, dir, "rev-parse", "HEAD")
	o := machine(t, writerB, "rerun", passB)
	if err := writeJournal(o.StateDir, journal{Pass: passA, From: 1, To: 2, Before: before}); err != nil {
		t.Fatal(err)
	}
	writeT(t, filepath.Join(dir, "legacy", "v1", "log.md"), v1Files["log.md"])
	writeT(t, filepath.Join(dir, ".eva-md", "store.toml"), "schema = 2\nmin_reader = 2\nstore_id = \""+storeID+"\"\nepoch = 1\n")
	writeT(t, filepath.Join(dir, ".gitattributes"), "* -text -filter -ident\n")
	gitT(t, dir, "add", "-A")
	if err := os.Remove(filepath.Join(dir, "log.md")); err != nil {
		t.Fatal(err)
	}
	writeT(t, filepath.Join(dir, "general", paths.TempPrefix+"0123abcd"), "partial")
	for _, lock := range []string{"index.lock", "HEAD.lock", "refs/heads/main.lock", "refs/eva-md/before-schema-2.lock"} {
		writeT(t, filepath.Join(dir, ".git", filepath.FromSlash(lock)), "")
	}

	r, err := Run(bg(), dir, synthetic, o)
	if err != nil || !r.Committed || r.Pass != passA {
		t.Fatalf("Run = %+v, %v; want a commit under the journal's pass", r, err)
	}
	if got := treeID(t, dir); got != want {
		t.Fatalf("tree %s, want the clean run's %s", got, want)
	}
	if got := gitT(t, dir, "status", "--porcelain", "--untracked-files=all", "--ignored"); got != "" {
		t.Fatalf("the tree is not clean:\n%s", got)
	}
	if got := gitT(t, dir, "rev-list", "--count", before+"..HEAD"); got != "1" {
		t.Fatalf("%s commits over the commit before, want 1", got)
	}
}

// TestRecoveryCutShortToo cuts a run short once it has staged its files,
// then cuts the run that recovers it at each of that run's own points: a
// third run still gives the clean run's tree, one commit and the first
// pass.
func TestRecoveryCutShortToo(t *testing.T) {
	isolate(t)
	points, want := record(t, synthetic)
	first := slices.Index(points, "git add") + 1

	// The points of a recovery: a run over what the first cut left.
	dir := v1Wiki(t, v1Files)
	o := machine(t, writerB, "rerun", passB)
	crashAt(t, dir, o, "synthetic", first)
	var again []string
	hook = func(p string) { again = append(again, p) }
	_, err := Run(bg(), dir, synthetic, o)
	hook = func(string) {}
	if err != nil {
		t.Fatal(err)
	}
	reset := slices.Index(again, "git reset")
	if reset < 0 || slices.Index(again[reset:], "write") < 0 || !slices.Contains(again, "git commit") {
		t.Fatalf("the recovery did not go back and commit: %v", again)
	}

	for j := 1; j <= len(again); j++ {
		t.Run(strings.ReplaceAll(again[j-1], " ", "-"), func(t *testing.T) {
			dir := v1Wiki(t, v1Files)
			before := gitT(t, dir, "rev-parse", "HEAD")
			o := machine(t, writerB, "rerun", passB)
			crashAt(t, dir, o, "synthetic", first)
			crashAt(t, dir, o, "synthetic", j)
			if _, err := Run(bg(), dir, synthetic, o); err != nil {
				t.Fatal(err)
			}
			if got := treeID(t, dir); got != want {
				t.Errorf("tree %s, want the clean run's %s", got, want)
			}
			if got := gitT(t, dir, "rev-list", "--count", before+"..HEAD"); got != "1" {
				t.Errorf("%s commits over the commit before, want 1", got)
			}
			if got := gitT(t, dir, "status", "--porcelain", "--untracked-files=all", "--ignored"); got != "" {
				t.Errorf("the tree is not clean:\n%s", got)
			}
			if msg := gitT(t, dir, "log", "-1", "--format=%B"); !strings.Contains(msg, "\nEva-Pass: "+passA+"\n") {
				t.Errorf("message =\n%s\nwant the first pass", msg)
			}
			if _, err := os.Stat(filepath.Join(o.StateDir, journalFile)); !errors.Is(err, os.ErrNotExist) {
				t.Errorf("the journal is still there: %v", err)
			}
		})
	}
}
