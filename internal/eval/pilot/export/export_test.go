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
// any test starts, refuses the tasks, and nothing is written. Its minutes
// are the largest of the tasks exported: d04's 35, which comes before
// h05's 20, so neither the last task's nor the smallest would give it.
func TestExportTakesItsLimit(t *testing.T) {
	src := t.TempDir()
	for _, id := range []string{"d04", "h05"} {
		if err := os.CopyFS(filepath.Join(src, id), os.DirFS(filepath.Join(committed(t), id))); err != nil {
			t.Fatal(err)
		}
	}
	file := filepath.Join(src, "d04", "task.json")
	data, err := os.ReadFile(file)
	if err != nil || strings.Count(string(data), `"minutes": 20`) != 1 {
		t.Fatalf("d04's minutes: %v", err)
	}
	if err := os.WriteFile(file, []byte(strings.Replace(string(data), `"minutes": 20`, `"minutes": 35`, 1)), 0o644); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(t.TempDir(), "out")
	r, err := Export(context.Background(), Options{Tasks: src, Out: out, Limit: 0})
	if err != nil || r.Written || len(r.Tasks) != 2 || r.Tasks[0].Rule == nil || r.Tasks[1].Rule == nil {
		t.Fatalf("a limit no test meets: %+v, %v", r, err)
	}
	if _, err := os.Lstat(out); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("a refused task was written: %v", err)
	}
	r, err = Export(context.Background(), Options{Tasks: src, Out: out, Limit: time.Hour})
	if err != nil || !r.Written || r.Minutes != 35 || len(r.Tasks) != 2 || r.Tasks[0].Refused() || r.Tasks[1].Refused() {
		t.Fatalf("a limit of an hour: %+v, %v", r, err)
	}
	info, err := os.Stat(out)
	if err != nil || info.Mode().Perm() != 0o700 {
		t.Errorf("the folder written: %v, %v", info.Mode().Perm(), err)
	}
}

// A path lies inside the tasks folder when it reaches that folder, or a
// folder below it, by any name. The walk by the folder itself is held here
// to names through a link, the one second name a test can make without
// privilege on every system; a spelling in another letter case is held to
// what the file system says of it, and on macOS, whose file system ignores
// case and whose links do not explain that name, it is the walk by the
// folder itself that refuses it.
func TestInsideByTheFolderItself(t *testing.T) {
	base := t.TempDir()
	tasks := filepath.Join(base, "tasks")
	if err := os.MkdirAll(filepath.Join(tasks, "sub"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(base, "tasksx"), 0o700); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(base, "alias")
	if err := os.Symlink(tasks, alias); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{
		tasks, filepath.Join(tasks, "out"), filepath.Join(tasks, "sub", "new", "out"),
		alias, filepath.Join(alias, "out"), filepath.Join(alias, "sub", "new", "out"),
	} {
		if !insideByIdentity(p, tasks) || !inside(p, tasks) {
			t.Errorf("%s is not inside the tasks", p)
		}
	}
	for _, p := range []string{base, filepath.Join(base, "out"), filepath.Join(base, "tasksx", "out"), filepath.Join(base, "none", "out")} {
		if insideByIdentity(p, tasks) || inside(p, tasks) {
			t.Errorf("%s is inside the tasks", p)
		}
	}
	if insideByIdentity(filepath.Join(base, "out"), filepath.Join(base, "none")) {
		t.Error("a path is inside a folder that does not exist")
	}

	other := filepath.Join(base, "TASKS")
	a, errA := os.Stat(other)
	b, errB := os.Stat(tasks)
	oneFolder := errA == nil && errB == nil && os.SameFile(a, b)
	if got := inside(filepath.Join(other, "out"), tasks); got != oneFolder {
		t.Errorf("a path through the tasks spelt in another case is inside them %v, and they are one folder %v", got, oneFolder)
	}
}

// Only folder-<n> is the label Load gives a folder it does not name.
func TestIsLabel(t *testing.T) {
	for s, want := range map[string]bool{
		"folder-1": true, "folder-12": true, "folder-": false, "folder-1a": false,
		"d04": false, "afolder-1": false, "folder1": false,
	} {
		if isLabel(s) != want {
			t.Errorf("isLabel(%q) is %v", s, !want)
		}
	}
}
