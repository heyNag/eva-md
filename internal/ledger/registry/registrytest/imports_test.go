package registrytest_test

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
	self   = "internal/ledger/registry/registrytest"
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
	root, err := filepath.Abs(filepath.Join("..", "..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Fatalf("no go.mod at %s: %v", root, err)
	}
	return root
}

// No file of eva-md but a test imports the fake, so no build carries it.
func TestOnlyTestsImportIt(t *testing.T) {
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

// The check finds a planted importer wherever it is, and lets a test, the
// fake's own files and folders Go skips import it.
func TestTheImportCheckCanFail(t *testing.T) {
	imp := []byte("package x\n\nimport _ \"" + module + "/" + self + "\"\n")
	fsys := fstest.MapFS{
		"cmd/eva-md/main.go":                  {Data: []byte("package main\n")},
		"internal/a/a.go":                     {Data: imp},
		"internal/ledger/registry/planted.go": {Data: imp},
		"internal/b/b_test.go":                {Data: imp},
		"internal/c/c.go":                     {Data: []byte("//go:build never\n\n" + string(imp))},
		self + "/more.go":                     {Data: imp},
		"internal/d/testdata/d.go":            {Data: imp},
		".claude/worktrees/w/x/x.go":          {Data: imp},
		"internal/e/e.go":                     {Data: []byte("package e\n\nimport _ \"" + module + "/" + self + "/deeper\"\n")},
		"internal/f/f.go":                     {Data: []byte("package f\n\nimport _ \"" + module + "/" + self + "x\"\n")},
	}
	got, _, err := importers(fsys, self)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"internal/a/a.go", "internal/c/c.go", "internal/ledger/registry/planted.go"}
	if !slices.Equal(got, want) {
		t.Errorf("the check found %v, want %v", got, want)
	}
}

// The registry's non-test files import the standard library only, and the
// fake's the standard library and the registry.
func TestOwnImports(t *testing.T) {
	for dir, allowed := range map[string][]string{
		"..": nil,
		".":  {module + "/internal/ledger/registry"},
	} {
		files, err := filepath.Glob(filepath.Join(dir, "*.go"))
		if err != nil {
			t.Fatal(err)
		}
		read := 0
		for _, name := range files {
			if strings.HasSuffix(name, "_test.go") {
				continue
			}
			read++
			f, err := parser.ParseFile(token.NewFileSet(), name, nil, parser.ImportsOnly)
			if err != nil {
				t.Fatal(err)
			}
			for _, spec := range f.Imports {
				ip, _ := strconv.Unquote(spec.Path.Value)
				first, _, _ := strings.Cut(ip, "/")
				if strings.Contains(first, ".") && !slices.Contains(allowed, ip) {
					t.Errorf("%s imports %s", name, ip)
				}
			}
		}
		if read == 0 {
			t.Errorf("no non-test file in %s", dir)
		}
	}
}
