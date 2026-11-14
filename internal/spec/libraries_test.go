package spec

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// libraries are the modules eva-md requires, each with the one package of
// the program that may import it (OKF-K01, and AGENTS.md's rule on
// dependencies): the page library through the adapter, and the TOML
// library through the config.
var libraries = map[string]string{
	"github.com/okf-memory/okf-agent-memory": "internal/okfadapter",
	"github.com/BurntSushi/toml":             "internal/config",
}

// required lists the modules go.mod at root requires.
func required(t *testing.T, root string) []string {
	t.Helper()
	f, err := os.Open(filepath.Join(root, "go.mod"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var out []string
	block := false
	s := bufio.NewScanner(f)
	for s.Scan() {
		fields := strings.Fields(strings.TrimSpace(strings.SplitN(s.Text(), "//", 2)[0]))
		switch {
		case len(fields) == 2 && fields[0] == "require" && fields[1] == "(":
			block = true
		case block && len(fields) == 1 && fields[0] == ")":
			block = false
		case block && len(fields) == 2:
			out = append(out, fields[0])
		case len(fields) == 3 && fields[0] == "require":
			out = append(out, fields[1])
		}
	}
	if err := s.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

// checkLibraries holds the source at root to libraries: go.mod requires no
// module the table lacks, and among the Go files that are not tests, only
// a library's own package imports it, or any package under its path, and
// that package does.
func checkLibraries(t *testing.T, root string) []string {
	t.Helper()
	var problems []string
	for _, m := range required(t, root) {
		if _, ok := libraries[m]; !ok {
			problems = append(problems, fmt.Sprintf("go.mod requires %s, which no package is named to import", m))
		}
	}
	used := map[string]bool{}
	for _, f := range sources(t, root) {
		if f.test {
			continue
		}
		for _, spec := range f.file.Imports {
			path := strings.Trim(spec.Path.Value, "\"")
			for lib, owner := range libraries {
				if path != lib && !strings.HasPrefix(path, lib+"/") {
					continue
				}
				if f.pkg != owner {
					problems = append(problems, fmt.Sprintf("%s imports %s, which only %s may import", f.line(spec), path, owner))
				} else {
					used[lib] = true
				}
			}
		}
	}
	for _, lib := range slices.Sorted(func(yield func(string) bool) {
		for k := range libraries {
			if !yield(k) {
				return
			}
		}
	}) {
		if !used[lib] {
			problems = append(problems, fmt.Sprintf("%s never imports %s, so the check read the wrong source", libraries[lib], lib))
		}
	}
	return problems
}

// Only the adapter imports the page library and only the config the TOML
// library, among the program's Go files, and go.mod requires nothing else.
// Tests may import either.
func TestEachLibraryHasOneImporter(t *testing.T) {
	for _, p := range checkLibraries(t, repoRoot(t)) {
		t.Error(p)
	}
}

// The check fails a second importer of the page library, one of a package
// under its path, a second of the TOML library, a third module, and an
// owner that no longer imports its library; a test that imports either
// passes.
func TestASecondImporterIsFound(t *testing.T) {
	const okf, toml = "github.com/okf-memory/okf-agent-memory", "github.com/BurntSushi/toml"
	base := map[string]string{
		"go.mod":                        "module " + module + "\n\ngo 1.26.0\n\nrequire (\n\t" + toml + " v1.6.0\n\t" + okf + " v0.8.0 // the page library\n)\n",
		"internal/okfadapter/a.go":      "package okfadapter\n\nimport _ \"" + okf + "/pkg/okf\"\n",
		"internal/config/c.go":          "package config\n\nimport toml \"" + toml + "\"\n\nvar _ = toml.Unmarshal\n",
		"internal/threadpage/p_test.go": "package threadpage\n\nimport _ \"" + okf + "/pkg/okf\"\n",
		"cmd/eva-md/main.go":            "package main\n\nimport _ \"" + module + "/internal/okfadapter\"\n\nfunc main() {}\n",
	}
	for _, c := range []struct {
		name  string
		files map[string]string
		want  []string
	}{
		{"none", nil, nil},
		{"a second importer of the page library", map[string]string{
			"internal/update/u.go": "package update\n\nimport (\n\t\"fmt\"\n\n\t\"" + okf + "/pkg/okf\"\n)\n",
		}, []string{`^internal/update/u\.go:6 imports github\.com/okf-memory/okf-agent-memory/pkg/okf, which only internal/okfadapter may import$`}},
		{"the library's root, from a command", map[string]string{
			"cmd/eva-md/x.go": "package main\n\nimport _ \"" + okf + "\"\n",
		}, []string{`^cmd/eva-md/x\.go:3 imports github\.com/okf-memory/okf-agent-memory, which only internal/okfadapter may import$`}},
		{"a second importer of the TOML library", map[string]string{
			"internal/health/h.go": "package health\n\nimport t \"" + toml + "\"\n\nvar _ = t.Unmarshal\n",
		}, []string{`^internal/health/h\.go:3 imports github\.com/BurntSushi/toml, which only internal/config may import$`}},
		{"a third module", map[string]string{
			"go.mod": base["go.mod"] + "\nrequire example.com/more v1.0.0\n",
		}, []string{`^go\.mod requires example\.com/more, which no package is named to import$`}},
		{"an owner that no longer imports it", map[string]string{
			"internal/config/c.go": "package config\n",
		}, []string{`^internal/config never imports github\.com/BurntSushi/toml, so the check read the wrong source$`}},
	} {
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			files := map[string]string{}
			for k, v := range base {
				files[k] = v
			}
			for k, v := range c.files {
				files[k] = v
			}
			writeTree(t, dir, files)
			problems := checkLibraries(t, dir)
			if len(problems) != len(c.want) {
				t.Fatalf("problems %q, want %d", problems, len(c.want))
			}
			for i, w := range c.want {
				if !regexp.MustCompile(w).MatchString(problems[i]) {
					t.Errorf("problem %q does not say %q", problems[i], w)
				}
			}
		})
	}
}
