package export

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/heyNag/eva-md/internal/eval/pilot/task"
	"github.com/heyNag/eva-md/internal/eval/results"
)

// committed is the folder of the pilot's committed tasks.
func committed(t *testing.T) string {
	t.Helper()
	dir, err := filepath.Abs(filepath.Join("..", "..", "..", "..", "eval", "pilot", "testdata", "tasks"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "h01", "task.json")); err != nil {
		t.Fatal(err)
	}
	return dir
}

// loadSome loads the committed tasks with the ids, by id.
func loadSome(t *testing.T, ids ...string) []task.Task {
	t.Helper()
	all, err := task.Load(committed(t))
	if err != nil {
		t.Fatal(err)
	}
	return slices.DeleteFunc(all, func(x task.Task) bool { return !slices.Contains(ids, x.ID) })
}

// forbidden reports whether s holds a word of the tasks' Forbid.
func forbidden(s string, tasks []task.Task) bool {
	var words []string
	for _, t := range tasks {
		words = append(words, task.Forbid(t)...)
	}
	return results.Forbidden([]results.Row{{Scenario: s}}, words) != nil
}

// tree lists every path under dir, with "/" after a folder's.
func tree(t *testing.T, dir string) []string {
	t.Helper()
	var paths []string
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(dir, path)
		if d.IsDir() {
			rel += "/"
		}
		paths = append(paths, filepath.ToSlash(rel))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return paths
}

// failingAt is a copier that copies as the export does until its n-th
// call, which fails, having written half a file.
func failingAt(n int, calls *int) func(src, dst string) error {
	return func(src, dst string) error {
		*calls++
		if *calls == n {
			if err := os.WriteFile(dst, []byte("half"), 0o600); err != nil {
				return err
			}
			return errors.New("a made-up failure")
		}
		return copyFile(src, dst)
	}
}

// A write that fails at any file removes what it wrote: a folder it made
// is gone, and one that existed is empty again with its mode back. The
// error names no value, repository or path.
func TestAWriteThatFailsLeavesNothing(t *testing.T) {
	tasks := loadSome(t, "d04", "h05")
	total := 0
	probe := filepath.Join(t.TempDir(), "out")
	if err := write(probe, false, tasks, failingAt(-1, &total)); err != nil {
		t.Fatal(err)
	}
	if total < 4 {
		t.Fatalf("the export copies %d files", total)
	}
	for n := 1; n <= total; n++ {
		calls := 0
		out := filepath.Join(t.TempDir(), "out")
		err := write(out, false, tasks, failingAt(n, &calls))
		if err == nil || forbidden(err.Error(), tasks) || strings.Contains(err.Error(), string(filepath.Separator)) {
			t.Errorf("failing at file %d: %v", n, err)
		}
		if _, err := os.Lstat(out); !errors.Is(err, fs.ErrNotExist) {
			t.Errorf("failing at file %d: the folder it made is left: %v", n, err)
		}

		calls = 0
		out = t.TempDir()
		if err := os.Chmod(out, 0o751); err != nil {
			t.Fatal(err)
		}
		if err := write(out, true, tasks, failingAt(n, &calls)); err == nil {
			t.Errorf("failing at file %d of a folder that existed: no error", n)
		}
		info, err := os.Stat(out)
		if err != nil || info.Mode().Perm() != 0o751 {
			t.Errorf("failing at file %d: the folder that existed has mode %v, %v", n, info.Mode().Perm(), err)
		}
		if got := tree(t, out); !slices.Equal(got, []string{"./"}) {
			t.Errorf("failing at file %d: the folder that existed holds %v", n, got)
		}
	}
}

// A folder that was empty when the export began and is not when it writes
// is left as it is.
func TestAWriteLeavesAFolderThatChanged(t *testing.T) {
	tasks := loadSome(t, "d04")
	out := t.TempDir()
	if err := os.WriteFile(filepath.Join(out, "left.txt"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := write(out, true, tasks, copyFile); err == nil {
		t.Error("the export wrote into a folder that is no longer empty")
	}
	if got := tree(t, out); !slices.Equal(got, []string{"./", "left.txt"}) {
		t.Errorf("the folder holds %v", got)
	}
}

// An id or a repository's name that is not one name, which the pilot's Load
// never gives, writes nothing anywhere.
func TestAWriteRefusesANameThatClimbsOut(t *testing.T) {
	for name, edit := range map[string]func(*task.Task){
		"an id":                 func(x *task.Task) { x.ID = filepath.Join("..", "..", "escaped") },
		"a repository":          func(x *task.Task) { x.Repos = []string{filepath.Join("..", "escaped")} },
		"an absolute":           func(x *task.Task) { x.Repos = []string{string(filepath.Separator) + "escaped"} },
		"a repository of a dot": func(x *task.Task) { x.Repos = []string{".."} },
	} {
		x := loadSome(t, "d04")[0]
		edit(&x)
		base := t.TempDir()
		out := filepath.Join(base, "a", "out")
		if err := os.Mkdir(filepath.Dir(out), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := write(out, false, []task.Task{x}, copyFile); err == nil {
			t.Errorf("%s: the export wrote it", name)
		}
		if got := tree(t, base); !slices.Equal(got, []string{"./", "a/"}) {
			t.Errorf("%s: the export left %v", name, got)
		}
	}
}

// The copy never reads through a link, and its file is executable for its
// owner alone, exactly when the source is executable by anyone.
func TestCopyFile(t *testing.T) {
	dir := t.TempDir()
	for _, mode := range []fs.FileMode{0o644, 0o600, 0o755, 0o711, 0o640 | 0o001, 0o444} {
		src := filepath.Join(dir, "src-"+mode.String())
		if err := os.WriteFile(src, []byte("made-up bytes\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(src, mode); err != nil {
			t.Fatal(err)
		}
		dst := filepath.Join(dir, "dst-"+mode.String())
		if err := copyFile(src, dst); err != nil {
			t.Fatal(err)
		}
		want := fs.FileMode(0o600)
		if mode&0o111 != 0 {
			want = 0o700
		}
		info, err := os.Stat(dst)
		if data, _ := os.ReadFile(dst); err != nil || info.Mode().Perm() != want || string(data) != "made-up bytes\n" {
			t.Errorf("a copy of mode %v: %v %q, %v", mode, info.Mode().Perm(), data, err)
		}
		if err := copyFile(src, dst); err == nil {
			t.Errorf("a copy of mode %v replaced a file", mode)
		}
	}
	link := filepath.Join(dir, "link")
	if err := os.Symlink(filepath.Join(dir, "src--rw-r--r--"), link); err != nil {
		t.Fatal(err)
	}
	if err := copyFile(link, filepath.Join(dir, "through")); err == nil {
		t.Error("the copy read through a link")
	}
	if err := copyTree(dir, filepath.Join(t.TempDir(), "copy"), copyFile); err == nil {
		t.Error("a folder holding a link was copied")
	}
}

// Export writes what it checked, with each test of the pilot's check
// bounded by the limit it is given: a limit of 0, which has passed before
// any test starts, refuses the task, and nothing is written.
func TestExportTakesItsLimit(t *testing.T) {
	src := t.TempDir()
	if err := os.CopyFS(filepath.Join(src, "d04"), os.DirFS(filepath.Join(committed(t), "d04"))); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(t.TempDir(), "out")
	r, err := Export(context.Background(), Options{Tasks: src, Out: out, Limit: 0})
	if err != nil || r.Written || len(r.Tasks) != 1 || r.Tasks[0].Rule == nil {
		t.Fatalf("a limit no test meets: %+v, %v", r, err)
	}
	if _, err := os.Lstat(out); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("a refused task was written: %v", err)
	}
	r, err = Export(context.Background(), Options{Tasks: src, Out: out, Limit: time.Hour})
	if err != nil || !r.Written || r.Minutes != 20 || len(r.Tasks) != 1 || r.Tasks[0].Refused() {
		t.Fatalf("a limit of an hour: %+v, %v", r, err)
	}
	info, err := os.Stat(out)
	if err != nil || info.Mode().Perm() != 0o700 {
		t.Errorf("the folder written: %v, %v", info.Mode().Perm(), err)
	}
}
