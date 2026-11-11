package work

import (
	"go/parser"
	"go/token"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// The hook may import this package at no cost: its code imports, from
// eva-md, only config and paths, never the update, the hook, health or a
// command.
func TestThePackageImportsNothingTheHookWouldPayFor(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	allowed := map[string]bool{
		"github.com/heyNag/eva-md/internal/config": true,
		"github.com/heyNag/eva-md/internal/paths":  true,
	}
	checked := 0
	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(token.NewFileSet(), name, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatal(err)
		}
		checked++
		for _, spec := range f.Imports {
			path, _ := strconv.Unquote(spec.Path.Value)
			if strings.Contains(path, ".") && !allowed[path] {
				t.Errorf("%s imports %s", name, path)
			}
		}
	}
	if checked == 0 {
		t.Fatal("no file of the package was checked")
	}
}
