package capture

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// packageFiles parses the package's Go files, its tests or not.
func packageFiles(t *testing.T, tests bool) map[string]*ast.File {
	t.Helper()
	names, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]*ast.File{}
	for _, name := range names {
		if strings.HasSuffix(name, "_test.go") != tests {
			continue
		}
		f, err := parser.ParseFile(token.NewFileSet(), name, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		out[name] = f
	}
	if len(out) == 0 {
		t.Fatal("no file read")
	}
	return out
}

// ruleWords gives the value of every constant named Rule… in the
// package's code, so a rule added there is held by the tests that use it.
func ruleWords(t *testing.T) []string {
	t.Helper()
	var words []string
	for _, f := range packageFiles(t, false) {
		for _, decl := range f.Decls {
			g, ok := decl.(*ast.GenDecl)
			if !ok || g.Tok != token.CONST {
				continue
			}
			for _, spec := range g.Specs {
				v := spec.(*ast.ValueSpec)
				for i, name := range v.Names {
					if !strings.HasPrefix(name.Name, "Rule") || i >= len(v.Values) {
						continue
					}
					lit, ok := v.Values[i].(*ast.BasicLit)
					if !ok || lit.Kind != token.STRING {
						t.Fatalf("%s is not a string literal", name.Name)
					}
					word, _ := strconv.Unquote(lit.Value)
					if slices.Contains(words, word) {
						t.Fatalf("the rule word %q is there twice", word)
					}
					words = append(words, word)
				}
			}
		}
	}
	if len(words) < 15 {
		t.Fatalf("read %d rule words: %v", len(words), words)
	}
	return words
}

// SPEC.md names every rule word and every fact the one list cites.
func TestSpecNamesEveryRuleAndFact(t *testing.T) {
	data, err := os.ReadFile("SPEC.md")
	if err != nil {
		t.Fatal(err)
	}
	spec := string(data)
	for _, w := range ruleWords(t) {
		if !strings.Contains(spec, "`"+w+"`") {
			t.Errorf("SPEC.md does not name the rule `%s`", w)
		}
	}
	fact := regexp.MustCompile(`^F-\d{3}$`)
	for _, list := range [][]Entry{sources, prefixes, entrypoints} {
		for _, e := range list {
			if !fact.MatchString(e.Fact) {
				t.Errorf("%q cites %q, which is no fact's number", e.Value, e.Fact)
			}
			if !regexp.MustCompile(`\b` + e.Fact + `\b`).MatchString(spec) {
				t.Errorf("SPEC.md does not name %s, which %q cites", e.Fact, e.Value)
			}
			if !strings.Contains(spec, string(e.State)) {
				t.Errorf("SPEC.md does not name the state %q, which %q has", e.State, e.Value)
			}
			if !strings.Contains(spec, "`"+string(e.Author)+"`") {
				t.Errorf("SPEC.md does not name the author %s, which %q gives", e.Author, e.Value)
			}
		}
	}
}

// The package's code reads no file, clock, environment or network and
// writes no log: it imports none of the packages that would.
func TestCodeImportsNoInputOrOutput(t *testing.T) {
	banned := []string{"os", "io/fs", "net", "os/exec", "log", "time", "syscall", "unsafe", "math/rand", "math/rand/v2", "crypto/rand", "runtime"}
	for name, f := range packageFiles(t, false) {
		for _, spec := range f.Imports {
			path, _ := strconv.Unquote(spec.Path.Value)
			if slices.Contains(banned, path) || strings.HasPrefix(path, "net/") || strings.HasPrefix(path, "log/") || strings.HasPrefix(path, "os/") {
				t.Errorf("%s imports %s", name, path)
			}
			if strings.HasPrefix(path, "github.com/heyNag/eva-md/") && path != "github.com/heyNag/eva-md/internal/host/adapter" && path != "github.com/heyNag/eva-md/internal/ledger/event" {
				t.Errorf("%s imports %s; the package imports from eva-md only the adapter and the event model", name, path)
			}
			if !strings.HasPrefix(path, "github.com/heyNag/eva-md/") && strings.Contains(strings.Split(path, "/")[0], ".") {
				t.Errorf("%s imports %s, outside the standard library", name, path)
			}
		}
	}
}
