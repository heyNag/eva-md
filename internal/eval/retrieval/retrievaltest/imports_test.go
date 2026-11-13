package retrievaltest_test

import (
	"go/ast"
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
	self   = "internal/eval/retrieval/retrievaltest"
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
		"cmd/eva-md/main.go":         {Data: []byte("package main\n")},
		"internal/a/a.go":            {Data: imp},
		"internal/b/b_test.go":       {Data: imp},
		"internal/c/c.go":            {Data: []byte("//go:build never\n\n" + string(imp))},
		self + "/more.go":            {Data: imp},
		"internal/d/testdata/d.go":   {Data: imp},
		".claude/worktrees/w/x/x.go": {Data: imp},
		"internal/e/e.go":            {Data: []byte("package e\n\nimport _ \"" + module + "/" + self + "/deeper\"\n")},
		self + "x/y.go":              {Data: []byte("package y\n\nimport _ \"" + module + "/" + self + "x\"\n")},
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
	if len(files) < 3 {
		t.Fatalf("only %d files: %v", len(files), files)
	}
	for _, name := range files {
		src, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		uses, err := bannedUses(name, src)
		if err != nil {
			t.Fatal(err)
		}
		for _, u := range uses {
			t.Errorf("%s uses %s", name, u)
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
		}
	}
}

// banned holds, by package, what a file of the fake may not name: what
// sleeps or reads the wall clock, and what writes a file. A name counts
// whether it is called or passed as a value, as New(time.Now) passes it.
var banned = map[string][]string{
	"time": {"Now", "Since", "Until", "Sleep", "After", "AfterFunc", "Tick", "NewTicker", "NewTimer"},
	"os": {"WriteFile", "Create", "CreateTemp", "OpenFile", "Mkdir", "MkdirAll", "MkdirTemp",
		"Remove", "RemoveAll", "Rename", "Symlink", "Link", "Chmod", "Chtimes", "Truncate"},
}

// bannedUses lists what of banned a Go file names, under whatever name it
// imports the package, and the imports that run or write outside the
// fake: os/exec, syscall, and a dot import of a package banned watches,
// whose names the check could then not see.
func bannedUses(name string, src []byte) ([]string, error) {
	f, err := parser.ParseFile(token.NewFileSet(), name, src, parser.SkipObjectResolution)
	if err != nil {
		return nil, err
	}
	var out []string
	local := map[string]string{} // the name a file uses for a watched package
	for _, spec := range f.Imports {
		ip, _ := strconv.Unquote(spec.Path.Value)
		if ip == "os/exec" || ip == "syscall" {
			out = append(out, "the import "+ip)
			continue
		}
		if _, ok := banned[ip]; !ok {
			continue
		}
		as := ip
		if spec.Name != nil {
			as = spec.Name.Name
		}
		switch as {
		case "_":
		case ".":
			out = append(out, "a dot import of "+ip)
		default:
			local[as] = ip
		}
	}
	ast.Inspect(f, func(n ast.Node) bool {
		sel, ok := n.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		if id, ok := sel.X.(*ast.Ident); ok {
			if ip, ok := local[id.Name]; ok && slices.Contains(banned[ip], sel.Sel.Name) {
				out = append(out, ip+"."+sel.Sel.Name)
			}
		}
		return true
	})
	return out, nil
}

// The check of the fake's own files finds the wall clock and a write
// however a file names them, called or passed as a value, and passes what
// reads neither.
func TestTheClockCheckCanFail(t *testing.T) {
	for _, c := range []struct {
		src  string
		want []string
	}{
		{"import \"time\"\n\nvar _ = New(time.Now)", []string{"time.Now"}},
		{"import \"time\"\n\nfunc f() { time.NewTicker(time.Second); time.AfterFunc(0, nil); _ = time.Tick(1) }",
			[]string{"time.NewTicker", "time.AfterFunc", "time.Tick"}},
		{"import \"time\"\n\nfunc f() { time.Sleep(1); <-time.After(1); _ = time.Since(time.Time{}) }",
			[]string{"time.Sleep", "time.After", "time.Since"}},
		{"import clock \"time\"\n\nvar _ = clock.Now()", []string{"time.Now"}},
		{"import . \"time\"\n\nvar _ = Now()", []string{"a dot import of time"}},
		{"import \"os/exec\"\n\nvar _ = exec.Command", []string{"the import os/exec"}},
		{"import \"os\"\n\nfunc f() { os.WriteFile(\"x\", nil, 0); os.MkdirAll(\"x\", 0); os.RemoveAll(\"x\") }",
			[]string{"os.WriteFile", "os.MkdirAll", "os.RemoveAll"}},
		{"import (\n\t\"os\"\n\t\"time\"\n)\n\n// time.Now() in a comment\nvar _ = \"time.Now() in a string\"\n\n" +
			"func f(t interface{ TempDir() string }) {\n\t_ = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC).Add(time.Minute)\n\t_ = t.TempDir()\n\tos.ReadFile(\"x\")\n}", nil},
	} {
		got, err := bannedUses("x.go", []byte("package x\n\n"+c.src+"\n"))
		if err != nil {
			t.Fatalf("%q: %v", c.src, err)
		}
		if !slices.Equal(got, c.want) {
			t.Errorf("%q: the check found %v, want %v", c.src, got, c.want)
		}
	}
}
