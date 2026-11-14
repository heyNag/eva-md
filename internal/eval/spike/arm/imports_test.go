package arm

import (
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path"
	"slices"
	"strconv"
	"strings"
	"testing"
	"testing/fstest"
)

const module = "github.com/heyNag/eva-md"

// allowed are the eva-md packages a non-test file of the arm may import:
// never internal/update, so it runs on canned answers alone.
var allowed = []string{module + "/internal/eval/spike", module + "/internal/eval/scenario", module + "/internal/eval/score",
	module + "/internal/eval/results"}

// outsideImports lists the eva-md packages beyond allowed that the
// non-test Go files of fsys's root import, as file: package, and how many
// files it read.
func outsideImports(fsys fs.FS) ([]string, int, error) {
	files, err := fs.Glob(fsys, "*.go")
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

// TestTheArmImportsOnlyItsFour: the arm's non-test files import from
// eva-md only the spike, the scenario model, the scorer and the results
// store.
func TestTheArmImportsOnlyItsFour(t *testing.T) {
	got, read, err := outsideImports(os.DirFS("."))
	if err != nil {
		t.Fatal(err)
	}
	if read < 4 {
		t.Fatalf("read only %d files", read)
	}
	if len(got) > 0 {
		t.Errorf("the arm imports %v", got)
	}
}

// TestTheImportCheckCanFail: the check finds an import beyond the four in
// any non-test file, in a group or alone, renamed or blank, and passes the
// four, the standard library and a test file.
func TestTheImportCheckCanFail(t *testing.T) {
	fsys := fstest.MapFS{
		"ok.go":     {Data: []byte("package arm\n\nimport (\n\t\"strings\"\n\t_ \"" + allowed[0] + "\"\n\tx \"" + allowed[3] + "\"\n)\n")},
		"update.go": {Data: []byte("package arm\n\nimport u \"" + module + "/internal/update\"\n")},
		"event.go":  {Data: []byte("package arm\n\nimport (\n\t\"fmt\"\n\t_ \"" + module + "/internal/ledger/event\"\n)\n")},
		"root.go":   {Data: []byte("package arm\n\nimport _ \"" + module + "\"\n")},
		"x_test.go": {Data: []byte("package arm\n\nimport _ \"" + module + "/internal/update\"\n")},
		"sub/s.go":  {Data: []byte("package sub\n\nimport _ \"" + module + "/internal/update\"\n")},
	}
	got, read, err := outsideImports(fsys)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"event.go: " + module + "/internal/ledger/event", "root.go: " + module, "update.go: " + module + "/internal/update"}
	if read != 4 || !slices.Equal(got, want) {
		t.Errorf("read %d files, found %v, want %v", read, got, want)
	}
}
