package spec

import (
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// The module's Go source, as the checks on what the source holds read it:
// each file of cmd/ and internal/, outside testdata, parsed with its
// comments. Reading the source, not a command's listing, keeps these
// checks to files: they pass as root and take seconds.

// module is the module's path, as go.mod names it.
const module = "github.com/heyNag/eva-md"

// goFile is one Go file of the module.
type goFile struct {
	path string // from the module's root, with slashes
	pkg  string // its folder, as path's
	test bool
	file *ast.File
	fset *token.FileSet
}

// sources parses every Go file under cmd/ and internal/ at root, but those
// in a testdata or hidden folder, and fails the test on one that does not
// parse.
func sources(t *testing.T, root string) []goFile {
	t.Helper()
	fset := token.NewFileSet()
	var files []goFile
	for _, top := range []string{"cmd", "internal"} {
		if _, err := os.Stat(filepath.Join(root, top)); errors.Is(err, fs.ErrNotExist) {
			continue
		}
		err := filepath.WalkDir(filepath.Join(root, top), func(path string, d fs.DirEntry, err error) error {
			switch {
			case err != nil:
				return err
			case d.IsDir() && (d.Name() == "testdata" || strings.HasPrefix(d.Name(), ".")):
				return filepath.SkipDir
			case d.IsDir() || !strings.HasSuffix(d.Name(), ".go"):
				return nil
			}
			f, err := parser.ParseFile(fset, path, nil, parser.ParseComments|parser.SkipObjectResolution)
			if err != nil {
				return err
			}
			rel, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}
			rel = filepath.ToSlash(rel)
			files = append(files, goFile{path: rel, pkg: filepath.ToSlash(filepath.Dir(rel)), test: strings.HasSuffix(rel, "_test.go"), file: f, fset: fset})
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	return files
}

// imports lists the paths a file imports, and the name each is known by in
// the file: its own name if it gives one, else the last element of its
// path.
func (g goFile) imports() map[string]string {
	out := map[string]string{}
	for _, spec := range g.file.Imports {
		path, err := strconv.Unquote(spec.Path.Value)
		if err != nil {
			continue
		}
		name := path[strings.LastIndex(path, "/")+1:]
		if spec.Name != nil {
			name = spec.Name.Name
		}
		out[path] = name
	}
	return out
}

// line is where n starts in the file, as path:line.
func (g goFile) line(n ast.Node) string {
	return g.path + ":" + strconv.Itoa(g.fset.Position(n.Pos()).Line)
}

// packageOf is the folder of a module import path, or "" for another
// module's.
func packageOf(path string) string {
	if rest, ok := strings.CutPrefix(path, module+"/"); ok {
		return rest
	}
	return ""
}

// tempHome gives a test a home folder of its own, so nothing it runs can
// read or write the real one.
func tempHome(t *testing.T) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
}

// writeTree writes files, by their paths from dir, into dir, for a seeded
// copy of the module's source.
func writeTree(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	for _, name := range slices.Sorted(func(yield func(string) bool) {
		for k := range files {
			if !yield(k) {
				return
			}
		}
	}) {
		file := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(file, []byte(files[name]), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}
