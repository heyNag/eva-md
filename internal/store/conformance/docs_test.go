package conformance

import (
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// The table's rows: a property, its source, its check, the variants that
// kill it.
var reRow = regexp.MustCompile("^\\| (\\w+) \\| [^|]+ \\| `(Check\\w+)` \\| ([^|]*) \\|$")

func TestThePropertiesTable(t *testing.T) {
	b, err := os.ReadFile("PROPERTIES.md")
	if err != nil {
		t.Fatal(err)
	}
	if len(b) > 12*1024 {
		t.Errorf("PROPERTIES.md is %d bytes; at most 12 KB", len(b))
	}
	killed := map[string][]string{}
	for _, v := range variants {
		killed[v.property] = append(killed[v.property], v.name)
	}
	var rows []string
	for line := range strings.SplitSeq(string(b), "\n") {
		m := reRow.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		prop := m[1]
		rows = append(rows, prop)
		if m[2] != "Check"+prop {
			t.Errorf("the row of %s names the check %s", prop, m[2])
		}
		var listed []string
		for _, v := range regexp.MustCompile("`(\\w+)`").FindAllStringSubmatch(m[3], -1) {
			listed = append(listed, v[1])
		}
		if len(listed) == 0 {
			t.Errorf("no variant kills %s", prop)
		}
		slices.Sort(listed)
		shown := slices.Clone(killed[prop])
		slices.Sort(shown)
		if !slices.Equal(listed, shown) {
			t.Errorf("PROPERTIES.md says %v kill %s; the tests show %v", listed, prop, shown)
		}
		if !strings.Contains(string(b), "\n### "+prop+"\n") {
			t.Errorf("%s has no section", prop)
		}
	}
	if want := []string{PropCrashSafety, PropExactlyOnce, PropProtection, PropRestore, PropReplay, PropSessionKept}; !slices.Equal(rows, want) {
		t.Errorf("the table's properties are %v, want %v", rows, want)
	}
}

// The files this package adds, and the store's interface and ids.
func ownFiles(t *testing.T) []string {
	t.Helper()
	files, err := filepath.Glob("*")
	if err != nil {
		t.Fatal(err)
	}
	files = append(files, filepath.Join("..", "api.go"), filepath.Join("..", "ids.go"), filepath.Join("..", "ids_test.go"))
	if len(files) < 12 {
		t.Fatalf("only %d files: %v", len(files), files)
	}
	return files
}

func TestOnlyEvasNameShows(t *testing.T) {
	re := regexp.MustCompile(`(?i)` + "o" + "kf")
	for _, f := range ownFiles(t) {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		if re.Match(b) {
			t.Errorf("%s names the library v1 was built on", f)
		}
	}
}

// No test sleeps, reads the wall clock, runs git or writes to disk: no
// file here calls what would.
func TestNoCheckTouchesTheMachine(t *testing.T) {
	banned := []string{"time." + "Now(", "time." + "Since(", "time." + "Sleep(", "time." + "After(", "time." + "Tick(", "os/" + "exec", "os." + "WriteFile(", "os." + "Create(", "os." + "Mkdir", "os." + "Remove", "TempDir" + "("}
	for _, f := range ownFiles(t) {
		if !strings.HasSuffix(f, ".go") {
			continue
		}
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for _, w := range banned {
			if strings.Contains(string(b), w) {
				t.Errorf("%s calls %s", f, w)
			}
		}
	}
}

const module = "github.com/heyNag/eva-md/"

// reaches follows the imports of the non-test files of the package in dir,
// and of every package of the module they import, and reports whether it
// reaches target. Every file counts, whatever its build tags.
func reaches(t *testing.T, root, dir, target string, seen map[string]bool) bool {
	t.Helper()
	if seen[dir] {
		return false
	}
	seen[dir] = true
	files, err := filepath.Glob(filepath.Join(root, dir, "*.go"))
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		parsed, err := parser.ParseFile(token.NewFileSet(), f, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatal(err)
		}
		for _, spec := range parsed.Imports {
			p, _ := strconv.Unquote(spec.Path.Value)
			rel, ok := strings.CutPrefix(p, module)
			if !ok {
				continue
			}
			if rel == target || reaches(t, root, rel, target, seen) {
				return true
			}
		}
	}
	return false
}

func TestNothingUnderCmdReachesTheSuite(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	const suite = "internal/store/conformance"
	var dirs []string
	err = filepath.WalkDir(filepath.Join(root, "cmd"), func(path string, d fs.DirEntry, err error) error {
		if err == nil && d.IsDir() {
			rel, _ := filepath.Rel(root, path)
			dirs = append(dirs, filepath.ToSlash(rel))
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(dirs, "cmd/eva-md") {
		t.Fatalf("the walk found no cmd/eva-md in %v", dirs)
	}
	for _, dir := range dirs {
		if reaches(t, root, dir, suite, map[string]bool{}) {
			t.Errorf("%s reaches %s", dir, suite)
		}
	}
	// The walk follows imports through packages: the CLI reaches the
	// frozen redaction rules through the redaction package, and the suite
	// reaches the codec.
	if !reaches(t, root, "cmd/eva-md", "internal/redact/baseline", map[string]bool{}) {
		t.Error("the walk does not follow the CLI's imports to the redaction baseline")
	}
	if !reaches(t, root, suite, "internal/store/codec", map[string]bool{}) {
		t.Error("the walk does not find that the suite imports the codec")
	}
}
