package adapter

import (
	"bufio"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// hostLiterals are the values a string literal holding a host's name has.
var hostLiterals = map[string]bool{"claude": true, "claude-code": true, "codex": true, "Claude Code": true, "Codex": true}

// countHostLiterals counts, per package under cmd/ and internal/ of root
// outside internal/host/, the lines of its non-test Go files that hold a
// string literal whose whole value is a host's name. A package is its folder,
// relative to root with slashes; one with no such line is left out.
func countHostLiterals(root string) (map[string]int, error) {
	counts := map[string]int{}
	skip := filepath.Join(root, "internal", "host")
	for _, top := range []string{"cmd", "internal"} {
		err := filepath.WalkDir(filepath.Join(root, top), func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			name := d.Name()
			if d.IsDir() {
				if path == skip || name == "testdata" || strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_") {
					return filepath.SkipDir
				}
				return nil
			}
			if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
				return nil
			}
			n, err := hostLiteralLines(path)
			if err != nil || n == 0 {
				return err
			}
			pkg, err := filepath.Rel(root, filepath.Dir(path))
			if err != nil {
				return err
			}
			counts[filepath.ToSlash(pkg)] += n
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	return counts, nil
}

// hostLiteralLines counts the lines of one Go file that hold a string
// literal whose whole value is a host's name. Comments are not literals.
func hostLiteralLines(path string) (int, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
	if err != nil {
		return 0, err
	}
	lines := map[int]bool{}
	ast.Inspect(file, func(n ast.Node) bool {
		lit, ok := n.(*ast.BasicLit)
		if !ok || lit.Kind != token.STRING {
			return true
		}
		if value, err := strconv.Unquote(lit.Value); err == nil && hostLiterals[value] {
			lines[fset.Position(lit.Pos()).Line] = true
		}
		return true
	})
	return len(lines), nil
}

// readBaseline reads lines of a package's path and its count.
func readBaseline(path string) (map[string]int, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	counts := map[string]int{}
	s := bufio.NewScanner(f)
	for s.Scan() {
		line := strings.TrimSpace(s.Text())
		if line == "" {
			continue
		}
		pkg, count, ok := strings.Cut(line, " ")
		n, err := strconv.Atoi(strings.TrimSpace(count))
		if !ok || err != nil || n <= 0 {
			return nil, fmt.Errorf("%s: bad line %q", path, line)
		}
		if _, seen := counts[pkg]; seen {
			return nil, fmt.Errorf("%s: %s is listed twice", path, pkg)
		}
		counts[pkg] = n
	}
	return counts, s.Err()
}

// compareCounts says, for each package whose count differs from the
// baseline, its package and both numbers.
func compareCounts(baseline, counts map[string]int) []string {
	var pkgs []string
	for pkg := range baseline {
		pkgs = append(pkgs, pkg)
	}
	for pkg := range counts {
		if _, ok := baseline[pkg]; !ok {
			pkgs = append(pkgs, pkg)
		}
	}
	slices.Sort(pkgs)
	var diffs []string
	for _, pkg := range pkgs {
		if got, want := counts[pkg], baseline[pkg]; got != want {
			diffs = append(diffs, fmt.Sprintf("%s has %d lines with a host literal, the baseline %d", pkg, got, want))
		}
	}
	return diffs
}

// Host names written outside internal/host may only go down. A change that
// lowers a package's count lowers its line in testdata/host-literals.txt; a
// package that reaches 0 loses its line.
func TestHostLiteralCount(t *testing.T) {
	root := filepath.Join("..", "..", "..")
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Fatalf("the repository's root: %v", err)
	}
	counts, err := countHostLiterals(root)
	if err != nil {
		t.Fatal(err)
	}
	total := 0
	for _, pkg := range slices.Sorted(maps.Keys(counts)) {
		t.Logf("host_literal_lines %s %d", pkg, counts[pkg])
		total += counts[pkg]
	}
	t.Logf("host_literal_lines total %d", total)
	baseline, err := readBaseline(filepath.Join("testdata", "host-literals.txt"))
	if err != nil {
		t.Fatal(err)
	}
	for _, diff := range compareCounts(baseline, counts) {
		t.Error(diff)
	}
}

// On a tree of its own, the count takes literals in code, one per line, and
// leaves out comments, test files, testdata and internal/host; a literal
// added to a package fails the comparison naming that package.
func TestHostLiteralCountOnATemporaryTree(t *testing.T) {
	root := t.TempDir()
	write := func(path, body string) {
		t.Helper()
		path = filepath.Join(root, filepath.FromSlash(path))
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("internal/alpha/a.go", "package alpha\n\n// \"claude\" in a comment.\nvar host = \"codex\"\n\nconst note = \"claude-code is a host\"\n\nvar raw = `Claude Code`\n")
	write("internal/alpha/a_test.go", "package alpha\n\nvar inTest = \"claude\"\n")
	write("internal/alpha/testdata/fixture.go", "package fixture\n\nvar inTestdata = \"codex\"\n")
	write("internal/host/adapter/names.go", "package adapter\n\nvar own = \"codex\"\n")
	write("internal/host/capture/c.go", "package capture\n\nvar own = \"claude\"\n")
	write("internal/hostess/h.go", "package hostess\n\nvar outside = \"Codex\"\n")
	write("cmd/tool/main.go", "package main\n\nfunc main() { f(\"claude\", \"codex\") }\n\nfunc f(a, b string) { _ = map[string]string{\"Claude Code\": a} }\n")
	write("notes/n.go", "package notes\n\nvar elsewhere = \"codex\"\n")
	counts, err := countHostLiterals(root)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]int{"internal/alpha": 2, "internal/hostess": 1, "cmd/tool": 2}
	if fmt.Sprint(counts) != fmt.Sprint(want) {
		t.Fatalf("counts %v, want %v", counts, want)
	}
	write("baseline.txt", "cmd/tool 2\ninternal/alpha 2\n\ninternal/hostess 1\n")
	baseline, err := readBaseline(filepath.Join(root, "baseline.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if diffs := compareCounts(baseline, counts); len(diffs) != 0 {
		t.Fatalf("an unchanged tree: %v", diffs)
	}

	// One literal more in a package, in a file of its own.
	write("internal/alpha/b.go", "package alpha\n\nvar another = \"Codex\"\n")
	// A comment and a test literal more change nothing.
	write("cmd/tool/more.go", "package main\n\n// codex, \"codex\"\n")
	write("cmd/tool/main_test.go", "package main\n\nvar x = \"codex\"\n")
	counts, err = countHostLiterals(root)
	if err != nil {
		t.Fatal(err)
	}
	diffs := compareCounts(baseline, counts)
	if len(diffs) != 1 || diffs[0] != "internal/alpha has 3 lines with a host literal, the baseline 2" {
		t.Fatalf("one literal added: %q", diffs)
	}

	// A new package with a literal, and a package whose literals are gone.
	write("internal/beta/b.go", "package beta\n\nvar h = \"claude\"\n")
	write("internal/hostess/h.go", "package hostess\n")
	counts, err = countHostLiterals(root)
	if err != nil {
		t.Fatal(err)
	}
	diffs = compareCounts(baseline, counts)
	if want := []string{
		"internal/alpha has 3 lines with a host literal, the baseline 2",
		"internal/beta has 1 lines with a host literal, the baseline 0",
		"internal/hostess has 0 lines with a host literal, the baseline 1",
	}; !slices.Equal(diffs, want) {
		t.Fatalf("diffs %q, want %q", diffs, want)
	}
}

// The baseline file's lines are a path and a positive count, each package
// once.
func TestReadBaselineRefusesBadLines(t *testing.T) {
	for _, body := range []string{"internal/x\n", "internal/x 0\n", "internal/x two\n", "internal/x 1\ninternal/x 2\n", "internal/x -1\n"} {
		path := filepath.Join(t.TempDir(), "baseline.txt")
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := readBaseline(path); err == nil {
			t.Errorf("baseline %q was read", body)
		}
	}
}

// goFiles lists the Go files of a folder.
func goFiles(t *testing.T, dir string) []string {
	t.Helper()
	files, err := filepath.Glob(filepath.Join(dir, "*.go"))
	if err != nil || len(files) == 0 {
		t.Fatalf("Go files in %s: %v, %v", dir, files, err)
	}
	return files
}

// imports gives the import paths of a Go file.
func imports(t *testing.T, path string) []string {
	t.Helper()
	file, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
	if err != nil {
		t.Fatal(err)
	}
	var paths []string
	for _, imp := range file.Imports {
		p, err := strconv.Unquote(imp.Path.Value)
		if err != nil {
			t.Fatal(err)
		}
		paths = append(paths, p)
	}
	return paths
}
