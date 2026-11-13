package spike

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
	self   = "internal/eval/spike"
)

// allowed are the eva-md packages a non-test file of the spike may import.
var allowed = []string{module + "/internal/eval/scenario", module + "/internal/ledger/event", module + "/internal/host/adapter"}

// importers lists the Go files of the tree that import the spike, test
// files included, outside the spike's own folders and cmd/eva-eval, and
// how many files it read. Every file counts, whatever its build tags;
// folders Go itself skips are skipped.
func importers(fsys fs.FS) ([]string, int, error) {
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
		dir := path.Dir(p)
		if !strings.HasSuffix(name, ".go") || dir == self || strings.HasPrefix(dir, self+"/") || dir == "cmd/eva-eval" {
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
			if ip, _ := strconv.Unquote(spec.Path.Value); ip == module+"/"+self {
				out = append(out, p)
			}
		}
		return nil
	})
	return out, read, err
}

// outsideImports lists the eva-md packages, beyond allowed, that the
// non-test Go files of dir in fsys import, as file: package.
func outsideImports(fsys fs.FS, dir string) ([]string, int, error) {
	files, err := fs.Glob(fsys, path.Join(dir, "*.go"))
	if err != nil {
		return nil, 0, err
	}
	var out []string
	read := 0
	for _, p := range files {
		if strings.HasSuffix(p, "_test.go") {
			continue
		}
		src, err := fs.ReadFile(fsys, p)
		if err != nil {
			return nil, 0, err
		}
		f, err := parser.ParseFile(token.NewFileSet(), p, src, parser.ImportsOnly)
		if err != nil {
			return nil, 0, err
		}
		read++
		for _, spec := range f.Imports {
			ip, _ := strconv.Unquote(spec.Path.Value)
			if (ip == module || strings.HasPrefix(ip, module+"/")) && !slices.Contains(allowed, ip) {
				out = append(out, path.Base(p)+": "+ip)
			}
		}
	}
	return out, read, nil
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

// TestNothingElseImportsTheSpike: no package outside the spike's folders
// and cmd/eva-eval imports it, so no release can carry it.
func TestNothingElseImportsTheSpike(t *testing.T) {
	got, read, err := importers(os.DirFS(repoRoot(t)))
	if err != nil {
		t.Fatal(err)
	}
	if read < 100 {
		t.Fatalf("the walk read only %d files", read)
	}
	if len(got) > 0 {
		t.Errorf("%v import %s", got, self)
	}
}

// TestTheSpikeImportsOnlyItsThree: the spike's non-test files import from
// eva-md only the scenario model, the event model and the host adapter.
func TestTheSpikeImportsOnlyItsThree(t *testing.T) {
	got, read, err := outsideImports(os.DirFS(repoRoot(t)), self)
	if err != nil {
		t.Fatal(err)
	}
	if read < 3 {
		t.Fatalf("read only %d files", read)
	}
	if len(got) > 0 {
		t.Errorf("the spike imports %v", got)
	}
}

// TestTheImportChecksCanFail: each check finds what it looks for wherever
// it is, and lets pass what it allows.
func TestTheImportChecksCanFail(t *testing.T) {
	imp := func(p string) []byte { return []byte("package x\n\nimport _ \"" + module + "/" + p + "\"\n") }
	fsys := fstest.MapFS{
		"cmd/eva-md/main.go":                     {Data: imp(self)},
		"internal/a/a.go":                        {Data: imp(self)},
		"internal/b/b_test.go":                   {Data: imp(self)},
		"internal/c/c.go":                        {Data: []byte("//go:build never\n\n" + string(imp(self)))},
		"internal/eval/spike/arm/arm.go":         {Data: imp(self)},
		"internal/eval/spikes/s.go":              {Data: imp(self)},
		"cmd/eva-eval/spike.go":                  {Data: imp(self)},
		"internal/d/testdata/d.go":               {Data: imp(self)},
		".claude/worktrees/w/x/x.go":             {Data: imp(self)},
		"internal/e/e.go":                        {Data: imp(self + "/arm")},
		self + "/ok.go":                          {Data: []byte("package spike\n\nimport (\n\t\"strings\"\n\t_ \"" + allowed[0] + "\"\n\t_ \"" + allowed[2] + "\"\n)\n")},
		self + "/bad.go":                         {Data: []byte("package spike\n\nimport (\n\t\"fmt\"\n\t_ \"" + module + "/internal/redact\"\n)\n")},
		self + "/root.go":                        {Data: []byte("package spike\n\nimport _ \"" + module + "\"\n")},
		self + "/bad_test.go":                    {Data: imp("internal/update")},
		self + "/arm/deeper.go":                  {Data: imp("internal/update")},
		"internal/eval/scenario/scenario.go":     {Data: imp("internal/inbox")},
		"internal/eval/spike/testdata/x/y.go":    {Data: imp(self)},
		"cmd/eva-eval/nested/not-the-command.go": {Data: imp(self)},
	}
	got, _, err := importers(fsys)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"cmd/eva-eval/nested/not-the-command.go", "cmd/eva-md/main.go", "internal/a/a.go", "internal/b/b_test.go",
		"internal/c/c.go", "internal/eval/spikes/s.go"}; !slices.Equal(got, want) {
		t.Errorf("the importers check found %v, want %v", got, want)
	}
	out, _, err := outsideImports(fsys, self)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"bad.go: " + module + "/internal/redact", "root.go: " + module}; !slices.Equal(out, want) {
		t.Errorf("the own-imports check found %v, want %v", out, want)
	}
}
