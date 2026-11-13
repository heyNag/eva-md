package storetest_test

import (
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"testing/fstest"
)

const (
	module = "github.com/heyNag/eva-md"
	self   = "internal/store/storetest"
)

// importers lists the Go files of the tree, outside dir, that import the
// module's package in dir, and how many files it read. A test file counts
// only inside dir: tests may import the fake, and nothing else may.
// Every file counts, whatever its build tags; folders Go itself skips are
// skipped.
func importers(fsys fs.FS, dir string) ([]string, int, error) {
	var out []string
	read := 0
	err := fs.WalkDir(fsys, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		name := d.Name()
		if d.IsDir() {
			if p != "." && (strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_") || name == "testdata") {
				return fs.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(name, ".go") || path.Dir(p) == dir || strings.HasSuffix(name, "_test.go") {
			return nil
		}
		src, err := fs.ReadFile(fsys, p)
		if err != nil {
			return err
		}
		f, err := parser.ParseFile(token.NewFileSet(), p, src, parser.ImportsOnly)
		if err != nil {
			return err
		}
		read++
		for _, spec := range f.Imports {
			if ip, _ := strconv.Unquote(spec.Path.Value); ip == module+"/"+dir {
				out = append(out, p)
			}
		}
		return nil
	})
	return out, read, err
}

func repoRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Fatalf("no go.mod at %s: %v", root, err)
	}
	return root
}

// No file of eva-md but a test imports the fake, so no build carries it.
func TestOnlyTestsImportTheFake(t *testing.T) {
	got, read, err := importers(os.DirFS(repoRoot(t)), self)
	if err != nil {
		t.Fatal(err)
	}
	if read < 100 {
		t.Fatalf("the walk read only %d files", read)
	}
	if len(got) > 0 {
		t.Errorf("non-test files import %s: %v", self, got)
	}
}

// The check finds an importer wherever it is, and lets a test, the fake's
// own files and folders Go skips import it.
func TestTheImportCheckCanFail(t *testing.T) {
	imp := []byte("package x\n\nimport _ \"" + module + "/" + self + "\"\n")
	fsys := fstest.MapFS{
		"cmd/eva-md/main.go":             {Data: []byte("package main\n")},
		"internal/a/a.go":                {Data: imp},
		"internal/b/b_test.go":           {Data: imp},
		"internal/c/c.go":                {Data: []byte("//go:build never\n\n" + string(imp))},
		self + "/more.go":                {Data: imp},
		"internal/d/testdata/d.go":       {Data: imp},
		".claude/worktrees/w/x/x.go":     {Data: imp},
		"internal/e/e.go":                {Data: []byte("package e\n\nimport _ \"" + module + "/" + self + "/deeper\"\n")},
		"internal/store/storetestx/y.go": {Data: []byte("package y\n\nimport _ \"" + module + "/" + self + "x\"\n")},
	}
	got, _, err := importers(fsys, self)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"internal/a/a.go", "internal/c/c.go"}; !slices.Equal(got, want) {
		t.Errorf("the check found %v, want %v", got, want)
	}
}

// The fake imports only the standard library and eva-md's packages, and
// no file of it sleeps, reads the wall clock or writes a file.
func TestTheFakesOwnFiles(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	if len(files) < 4 {
		t.Fatalf("only %d files: %v", len(files), files)
	}
	banned := []string{"time." + "Now(", "time." + "Since(", "time." + "Until(", "time." + "Sleep(", "time." + "After(", "time." + "Tick(", "time." + "NewTimer(",
		"os/" + "exec", "os." + "WriteFile(", "os." + "Create(", "os." + "OpenFile(", "os." + "Mkdir", "os." + "Remove", "os." + "Rename("}
	for _, name := range files {
		src, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		for _, w := range banned {
			if strings.Contains(string(src), w) {
				t.Errorf("%s calls %s", name, w)
			}
		}
		f, err := parser.ParseFile(token.NewFileSet(), name, src, parser.ImportsOnly)
		if err != nil {
			t.Fatal(err)
		}
		for _, spec := range f.Imports {
			ip, _ := strconv.Unquote(spec.Path.Value)
			first, _, _ := strings.Cut(ip, "/")
			if strings.Contains(first, ".") && ip != module && !strings.HasPrefix(ip, module+"/") {
				t.Errorf("%s imports %s, from outside the standard library and eva-md", name, ip)
			}
			// The consumer's test uses the fake as another package would:
			// through the store's interface and the fake alone.
			if name == "consumer_test.go" && strings.Contains(first, ".") && ip != module+"/internal/store" && ip != module+"/"+self {
				t.Errorf("%s imports %s; a consumer needs only the store and the fake", name, ip)
			}
		}
	}
}
