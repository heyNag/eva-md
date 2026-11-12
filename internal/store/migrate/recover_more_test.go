package migrate

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// TestRecoveryKeepsAnEditOfATouchedPath kills a run at each point a
// recording run lists, then has a person write into general/now.md, a
// path the step removes and carries. Before the step's commit, the rerun
// keeps the person's text, refuses the tree as dirty and keeps the
// journal; once the person undoes the edit, it gives the clean run's tree.
// After the commit, the rerun finishes and the person's file stays beside
// it.
func TestRecoveryKeepsAnEditOfATouchedPath(t *testing.T) {
	isolate(t)
	points, want := record(t, synthetic)
	journalAt := slices.Index(points, "journal write") + 1
	committedAt := slices.Index(points, "git commit") + 1
	const edit = "# Now\n\nNothing yet.\nA person's line.\n"
	for i := 1; i <= len(points); i++ {
		t.Run(fmt.Sprintf("%02d %s", i, points[i-1]), func(t *testing.T) {
			dir := v1Wiki(t, v1Files)
			before := gitT(t, dir, "rev-parse", "HEAD")
			o := machine(t, writerB, "rerun", passB)
			crashAt(t, dir, o, "synthetic", i)
			now := filepath.Join(dir, "general", "now.md")
			writeT(t, now, edit)

			r, err := Run(bg(), dir, synthetic, o)
			if b, rerr := os.ReadFile(now); rerr != nil || string(b) != edit {
				t.Fatalf("the person's edit is gone: %q, %v (Run = %+v, %v)", b, rerr, r, err)
			}
			if i >= committedAt {
				if err != nil || r.Committed != (i < len(points)) {
					t.Fatalf("Run = %+v, %v; want the step finished", r, err)
				}
				if got := treeID(t, dir); got != want {
					t.Fatalf("tree %s, want the clean run's %s", got, want)
				}
				return
			}
			if !errors.Is(err, ErrDirty) || r.Committed {
				t.Fatalf("Run = %+v, %v; want ErrDirty", r, err)
			}
			if got := gitT(t, dir, "rev-parse", "HEAD"); got != before {
				t.Fatal("HEAD moved")
			}
			_, jerr := os.Stat(filepath.Join(o.StateDir, journalFile))
			if (jerr == nil) != (i >= journalAt) {
				t.Fatalf("journal: %v; want one only once the cut run wrote it", jerr)
			}

			gitT(t, dir, "checkout", "HEAD", "--", "general/now.md")
			r, err = Run(bg(), dir, synthetic, o)
			if err != nil || !r.Committed {
				t.Fatalf("Run = %+v, %v; want a commit", r, err)
			}
			if got := treeID(t, dir); got != want {
				t.Fatalf("tree %s, want the clean run's %s", got, want)
			}
			if got := gitT(t, dir, "status", "--porcelain", "--untracked-files=all", "--ignored"); got != "" {
				t.Fatalf("the tree is not clean:\n%s", got)
			}
			pass := passB
			if i >= journalAt {
				pass = passA
			}
			if r.Pass != pass {
				t.Fatalf("pass %s, want %s", r.Pass, pass)
			}
		})
	}
}

// TestRecoveryFindsTheCommitBelowALaterOne cuts a run after its commit,
// before it removes its journal, then lands another commit on main: the
// rerun finds the step's commit below it and finishes, and a run after
// that makes no commit.
func TestRecoveryFindsTheCommitBelowALaterOne(t *testing.T) {
	isolate(t)
	points, want := record(t, synthetic)
	dir := v1Wiki(t, v1Files)
	before := gitT(t, dir, "rev-parse", "HEAD")
	o := machine(t, writerB, "rerun", passB)
	crashAt(t, dir, o, "synthetic", slices.Index(points, "git commit")+1)
	step := gitT(t, dir, "rev-parse", "HEAD")
	gitT(t, dir, "commit", "-q", "--allow-empty", "-m", "a later pass")
	later := gitT(t, dir, "rev-parse", "HEAD")

	r, err := Run(bg(), dir, synthetic, o)
	if err != nil || !r.Committed || r.Commit != step || r.Before != before || r.Pass != passA {
		t.Fatalf("Run = %+v, %v; want the step's commit %s finished", r, err, step)
	}
	if _, err := os.Stat(filepath.Join(o.StateDir, journalFile)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("the journal is still there: %v", err)
	}
	if got := gitT(t, dir, "rev-parse", "HEAD"); got != later {
		t.Fatal("HEAD moved")
	}
	if got := gitT(t, dir, "rev-parse", step+"^{tree}"); got != want {
		t.Fatalf("the step's tree %s, want the clean run's %s", got, want)
	}
	if r, err := Run(bg(), dir, synthetic, o); err != nil || r.Committed {
		t.Fatalf("second Run = %+v, %v; want no commit", r, err)
	}
}

// TestRecoveryRefusesAnotherCommit: under a journal, a commit on main above
// the commit before that is not this pass's own, with one parent, the
// commit before, is no finished step: main moved.
func TestRecoveryRefusesAnotherCommit(t *testing.T) {
	isolate(t)
	trailers := func(pass string) string {
		return "Eva-Pass: " + pass + "\nEva-Kind: migrate\nEva-Writer: " + writerA + "\nEva-Ops: create=0 revise=0 refused=0\nEva-Schema: 2"
	}
	land := func(t *testing.T, dir string, args ...string) {
		c := gitT(t, dir, append([]string{"commit-tree", "HEAD^{tree}"}, args...)...)
		gitT(t, dir, "update-ref", "refs/heads/main", c)
	}
	for name, make := range map[string]func(t *testing.T, dir, before string){
		"another pass's": func(t *testing.T, dir, before string) {
			land(t, dir, "-p", before, "-m", "migrate: - (+0 ~0 -0)", "-m", trailers(passB))
		},
		"with two parents": func(t *testing.T, dir, before string) {
			side := gitT(t, dir, "commit-tree", "HEAD^{tree}", "-p", before, "-m", "side")
			land(t, dir, "-p", before, "-p", side, "-m", "migrate: - (+0 ~0 -0)", "-m", trailers(passA))
		},
		"over another parent": func(t *testing.T, dir, before string) {
			land(t, dir, "-p", before, "-m", "a person's")
			land(t, dir, "-p", gitT(t, dir, "rev-parse", "HEAD"), "-m", "migrate: - (+0 ~0 -0)", "-m", trailers(passA))
		},
	} {
		t.Run(name, func(t *testing.T) {
			dir := v1Wiki(t, v1Files)
			before := gitT(t, dir, "rev-parse", "HEAD")
			o := machine(t, writerA, "laptop", passA)
			if err := writeJournal(o.StateDir, journal{Pass: passA, From: 1, To: 2, Before: before}); err != nil {
				t.Fatal(err)
			}
			make(t, dir, before)
			refused(t, dir, synthetic, o, ErrMoved)
		})
	}
}

// TestRecoveryKeepsTheJournalsStoreID: a recovery writes the store id the
// cut run gave the store, whatever id the machine offers now.
func TestRecoveryKeepsTheJournalsStoreID(t *testing.T) {
	isolate(t)
	points, want := record(t, synthetic)
	dir := v1Wiki(t, v1Files)
	o := machine(t, writerB, "rerun", passB)
	o.StoreID = "s01mj780q8r2t7"
	crashAt(t, dir, o, "synthetic", slices.Index(points, "write")+1)
	if r, err := Run(bg(), dir, synthetic, o); err != nil || !r.Committed {
		t.Fatalf("Run = %+v, %v; want a commit", r, err)
	}
	if got := treeID(t, dir); got != want {
		t.Fatalf("tree %s, want the clean run's %s", got, want)
	}
}

// dropLink is a made-up step 1 to 2 that removes the symlink v1WithLink
// commits, which it cannot read, and writes index.md.
var dropLink = Step{From: 1, To: 2, MinReader: 2, Classes: []string{"index"}, Plan: func(t Tree) (Change, error) {
	if _, err := t.Read("general/link.md"); err == nil {
		return Change{}, errors.New("the link reads as a file")
	}
	return Change{Write: map[string][]byte{"index.md": []byte(newIndex)}, Remove: []string{"general/link.md"},
		Counts: Counts{Written: map[string]int{"index": 1}, Removed: 1, Unreadable: 1}}, nil
}}

// TestRecoveryPutsALinkBackAsAFile cuts short a run that removed a
// committed symlink: the recovery puts it back as a file of its target,
// never as a link, and git, with symlinks off, takes that file for the
// link, so the rerun finds the tree clean and gives the clean run's tree.
func TestRecoveryPutsALinkBackAsAFile(t *testing.T) {
	isolate(t)
	clean := v1WithLink(t)
	var points []string
	hook = func(p string) { points = append(points, p) }
	_, err := Run(bg(), clean, dropLink, machine(t, writerA, "first", passA))
	hook = func(string) {}
	if err != nil {
		t.Fatal(err)
	}
	want := treeID(t, clean)

	dir := v1WithLink(t)
	link := filepath.Join(dir, "general", "link.md")
	o := machine(t, writerB, "rerun", passB)
	crashAt(t, dir, o, "drop-link", slices.Index(points, "remove")+1)
	if _, err := os.Lstat(link); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("the cut run did not remove the link: %v", err)
	}
	var back os.FileMode
	backErr := errors.New("not seen")
	hook = func(p string) {
		if p == "git status" && backErr != nil {
			var info os.FileInfo
			if info, backErr = os.Lstat(link); backErr == nil {
				back = info.Mode()
			}
		}
	}
	r, err := Run(bg(), dir, dropLink, o)
	hook = func(string) {}
	if backErr != nil || !back.IsRegular() {
		t.Fatalf("the link came back as %v, %v; want a file", back, backErr)
	}
	if err != nil || !r.Committed || r.Pass != passA {
		t.Fatalf("Run = %+v, %v; want a commit under the first pass", r, err)
	}
	if got := treeID(t, dir); got != want {
		t.Fatalf("tree %s, want the clean run's %s", got, want)
	}
}
