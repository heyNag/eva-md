package migrate

import (
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

const self = "github.com/heyNag/eva-md/internal/store/migrate"

// TestNothingElseImportsIt holds that only the package's own tests import
// it, and that it imports nothing but the standard library and eva-md's
// own packages.
func TestNothingElseImportsIt(t *testing.T) {
	here, err := filepath.Abs(".")
	if err != nil {
		t.Fatal(err)
	}
	module := filepath.Join(here, "..", "..", "..")
	seen := 0
	err = filepath.WalkDir(module, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && (d.Name() == ".git" || d.Name() == "testdata" || strings.HasPrefix(d.Name(), ".") && p != module) {
			return filepath.SkipDir
		}
		if d.IsDir() || !strings.HasSuffix(p, ".go") {
			return nil
		}
		f, err := parser.ParseFile(token.NewFileSet(), p, nil, parser.ImportsOnly)
		if err != nil {
			return err
		}
		seen++
		mine := filepath.Dir(p) == here
		for _, imp := range f.Imports {
			path, _ := strconv.Unquote(imp.Path.Value)
			if path == self && !mine {
				t.Errorf("%s imports the package", p)
			}
			if mine && !strings.HasSuffix(p, "_test.go") && strings.Contains(strings.Split(path, "/")[0], ".") && !strings.HasPrefix(path, "github.com/heyNag/eva-md/") {
				t.Errorf("%s imports a module outside eva-md: %s", p, path)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if seen < 100 {
		t.Fatalf("only %d Go files were read; the walk missed the module", seen)
	}
}
