package hostfake

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

const importPath = "github.com/heyNag/eva-md/internal/host/hostfake"

// moduleRoot is the repository's top folder, three up from this package.
func moduleRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Fatalf("no go.mod three folders up: %v", err)
	}
	return root
}

// imports gives the import paths of a Go file.
func imports(t *testing.T, path string) []string {
	t.Helper()
	f, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
	if err != nil {
		t.Fatal(err)
	}
	var paths []string
	for _, spec := range f.Imports {
		p, _ := strconv.Unquote(spec.Path.Value)
		paths = append(paths, p)
	}
	return paths
}

// The package, its tests included, imports only the standard library, and
// no net package.
func TestTheFakeImportsOnlyTheStandardLibrary(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil || len(files) == 0 {
		t.Fatalf("no file of the package: %v", err)
	}
	for _, name := range files {
		for _, path := range imports(t, name) {
			if bad := unwanted(path); bad != "" {
				t.Errorf("%s imports %s: %s", name, path, bad)
			}
		}
	}
	for path, want := range map[string]bool{"github.com/x/y": true, "net": true, "net/http": true, "os": false, "golang.org/x/sys/unix": true, "network": false} {
		if got := unwanted(path) != ""; got != want {
			t.Errorf("unwanted(%q) = %t", path, got)
		}
	}
}

// unwanted says why the fake may not import path, or "".
func unwanted(path string) string {
	if path == "net" || strings.HasPrefix(path, "net/") {
		return "a net package"
	}
	if first, _, _ := strings.Cut(path, "/"); strings.Contains(first, ".") {
		return "outside the standard library"
	}
	return ""
}

// Only tests import the package: no file of the module but a _test.go file
// outside the package names it.
func TestOnlyTestsImportTheFake(t *testing.T) {
	root := moduleRoot(t)
	checked := 0
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			// Hidden folders, such as worktrees under .claude, and
			// testdata hold no package of this module.
			if path != root && (strings.HasPrefix(d.Name(), ".") || d.Name() == "testdata") {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		checked++
		for _, p := range imports(t, path) {
			if p == importPath && filepath.Dir(path) != filepath.Join(root, "internal", "host", "hostfake") {
				rel, _ := filepath.Rel(root, path)
				t.Errorf("%s imports the fake, which only tests may", rel)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if checked < 50 {
		t.Fatalf("only %d files were checked", checked)
	}
	if !importsFake(t, filepath.Join(root, "internal", "install", "hostfake_test.go")) {
		t.Error("install's tests no longer import the fake, so this test checks nothing that uses it")
	}
}

func importsFake(t *testing.T, path string) bool {
	for _, p := range imports(t, path) {
		if p == importPath {
			return true
		}
	}
	return false
}

// Nothing in the fake or the tests that use it sleeps or waits on a timer.
func TestNothingSleeps(t *testing.T) {
	root := moduleRoot(t)
	files, _ := filepath.Glob("*.go")
	files = append(files, filepath.Join(root, "internal", "install", "hostfake_test.go"))
	for _, name := range files {
		data, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		for _, call := range []string{"time." + "Sleep", "time." + "After(", "time." + "Tick", "time." + "NewTimer"} {
			if strings.Contains(string(data), call) {
				t.Errorf("%s calls %s", name, call)
			}
		}
	}
}
