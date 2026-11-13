package modelrun

import (
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

const (
	module   = "github.com/heyNag/eva-md"
	thisPath = module + "/internal/derive/modelrun"
)

// imports gives the import paths of a Go file.
func imports(t *testing.T, path string) []string {
	t.Helper()
	f, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
	if err != nil {
		t.Fatal(err)
	}
	var paths []string
	for _, spec := range f.Imports {
		p, err := strconv.Unquote(spec.Path.Value)
		if err != nil {
			t.Fatal(err)
		}
		paths = append(paths, p)
	}
	return paths
}

// The package's code imports, beyond the standard library, only the host
// adapter: no module, and nothing else of eva-md.
func TestThePackageImportsOnlyTheAdapter(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	checked := 0
	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		checked++
		for _, p := range imports(t, name) {
			if strings.Contains(strings.Split(p, "/")[0], ".") && p != module+"/internal/host/adapter" {
				t.Errorf("%s imports %s", name, p)
			}
		}
	}
	if checked < 4 {
		t.Fatalf("only %d files of the package were checked", checked)
	}
}

// Nothing calls the package yet: no code outside it imports it, so it is
// built into no binary.
func TestNoCodeOutsideThePackageImportsIt(t *testing.T) {
	root := filepath.Join("..", "..", "..")
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Fatalf("the repository's root: %v", err)
	}
	here, err := filepath.Abs(".")
	if err != nil {
		t.Fatal(err)
	}
	checked := 0
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		name := d.Name()
		if d.IsDir() {
			if path != root && (name == "testdata" || strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_")) {
				return filepath.SkipDir
			}
			if abs, err := filepath.Abs(path); err == nil && abs == here {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			return nil
		}
		checked++
		for _, p := range imports(t, path) {
			if p == thisPath {
				t.Errorf("%s imports the package", path)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if checked < 100 {
		t.Fatalf("only %d files were checked", checked)
	}
}
