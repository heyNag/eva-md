package profile

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// The checker reads every document through the codec and has no reader of
// its own: its package imports the codec, and declares none of the
// functions its own reader had, nor anything else that reads a frontmatter
// line, a value's written form or a section.
func TestReadsThroughTheCodec(t *testing.T) {
	banned := []string{
		"unreadableReason", "parseDoc", "lenient", "quoted", "protection", "yamlKey", "readKey", "plainKeyEnd",
		"yamlUnescape", "stripComment", "section", "writtenValue", "canonicalJSON", "reKeyLine", "rePlain", "yamlWords", "reConflict",
	}
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	imported := false
	n := 0
	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		n++
		f, err := parser.ParseFile(token.NewFileSet(), name, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, imp := range f.Imports {
			p, _ := strconv.Unquote(imp.Path.Value)
			imported = imported || p == "github.com/heyNag/eva-md/internal/store/codec"
		}
		ast.Inspect(f, func(node ast.Node) bool {
			var names []*ast.Ident
			switch x := node.(type) {
			case *ast.FuncDecl:
				names = append(names, x.Name)
			case *ast.ValueSpec:
				names = append(names, x.Names...)
			case *ast.TypeSpec:
				names = append(names, x.Name)
			}
			for _, id := range names {
				if slices.Contains(banned, id.Name) {
					t.Errorf("%s declares %s: the checker reads documents through the codec", name, id.Name)
				}
			}
			return true
		})
		b, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		// A frontmatter's delimiter, or a comment after a value, looked for
		// in the checker's code would be a reading of its own.
		for _, s := range []string{`"---"`, `" #"`} {
			if strings.Contains(string(b), s) {
				t.Errorf("%s holds %s: the codec reads that", name, s)
			}
		}
	}
	if n < 5 {
		t.Fatalf("read %d files of the package", n)
	}
	if !imported {
		t.Error("the checker does not import the codec")
	}
}

// The checker reads sections as the codec does: a "## " line inside fenced
// code is not a section, and a "# " line does not end one.
func TestSectionsAsTheCodecReadsThem(t *testing.T) {
	cases := []struct {
		name string
		old  string
		new  string
		rule string // the one rule broken, or "" for none
	}{
		// A log heading inside a fence before the real one is the body's:
		// its fence lines are not log lines.
		{"a fenced heading before the log", "## Log\n", "```\n## Log\n```\n\n## Log\n", ""},
		// The only log heading is inside a fence: there is no log.
		{"the log only in a fence", "## Log\n", "```\n## Log\n", "log"},
		// A "# " line under the log does not end it: it and the line after
		// it are log lines that break the rule.
		{"a top heading under the log", "- did: chose a machine key for store commits [^e1]\n",
			"- did: chose a machine key for store commits [^e1]\n# Aside\n- thought: x [^e1]\n", "log"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			root := copyFixture(t)
			edit(t, root, ses, c.old, c.new)
			rep, err := Check(root)
			if err != nil {
				t.Fatal(err)
			}
			if c.rule == "" {
				for _, f := range rep.Faults {
					t.Error(f)
				}
				return
			}
			checkBreak(t, c.rule, func(t *testing.T, r string) { edit(t, r, ses, c.old, c.new) })
		})
	}
	// In a workstream, a fenced "## Next steps" holds no working items, and
	// lines after a "# " line under the real one are still its lines.
	root := copyFixture(t)
	b := readFile(t, root, ws1)
	edit(t, root, ws1, "## Next steps\n", "```\n## Next steps\n- not a working item\n```\n\n## Next steps\n")
	if rep, _ := Check(root); len(rep.Faults) != 0 {
		t.Errorf("a fenced heading is read as a section: %v", rep.Faults)
	}
	put(t, root, ws1, strings.Replace(b, "## Next steps\n", "## Next steps\n# Aside\n", 1))
	rep, _ := Check(root)
	if len(rep.Faults) != 1 || rep.Faults[0].Rule != "working-items" {
		t.Errorf("a # line ends a section: %v", rep.Faults)
	}
}
