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

// faultsOf checks a copy of the fixture after brk and returns its faults
// as "path rule" strings, sorted.
func faultsOf(t *testing.T, brk func(t *testing.T, root string)) []string {
	t.Helper()
	root := copyFixture(t)
	brk(t, root)
	rep, err := Check(root)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, f := range rep.Faults {
		out = append(out, f.Path+" "+f.Rule)
	}
	slices.Sort(out)
	return out
}

// A mark carried where none may be is faulted once for each key it
// protects: by its entry's fault, or, where the codec reads it on no
// entry of that key, by a fault of its own; never twice, and never left
// unnamed because the other key's entry was faulted.
func TestCarriedMarksEachFaulted(t *testing.T) {
	handSes := ses + "\t2026-11-10T10:00:00+01:00\tunacknowledged\n"
	handProj := projA + "\t2026-11-10T10:00:00+01:00\tacknowledged\n"
	cases := []struct {
		name  string
		rel   string // the file broken
		hand  string // its line of the hand-edit record, or ""
		lines string // added after its type line
		want  []string
	}{
		{"an empty verified entry and a lock with a tab before it", ses, handSes, "verified: []\n\tlocked: true\n", []string{"locked", "verified"}},
		{"an empty verified entry and locked:false", ses, handSes, "verified: []\nlocked:false\n", []string{"locked", "verified"}},
		{"a lock and a mark, each with a tab before it", ses, handSes, "\tlocked: true\n\tverified: [x]\n", []string{"locked", "verified"}},
		{"an unlocked entry and verified:[x]", projA, handProj, "locked: false\nverified:[x]\n", []string{"locked", "verified"}},
		// The engine never writes locked, so a file the record does not
		// list holds it in no written form too.
		{"locked: true", ses, "", "locked: true\n", []string{"locked", "written-form"}},
		{"locked: true, hand-edited", ses, handSes, "locked: true\n", []string{"locked"}},
		{"verified with a mark", projA, "", "verified: [{\"by\": \"human:user\", \"at\": \"2026-11-06T09:00:00Z\"}]\n", []string{"verified"}},
		{"an alias, which protects both ways", ses, handSes, "*k : true\n", []string{"locked"}},
		{"a merge, which protects both ways", ses, handSes, "<<: {\"locked\": true}\n", []string{"locked"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := faultsOf(t, func(t *testing.T, r string) {
				if c.hand != "" {
					appendTo(t, r, edits, c.hand)
				}
				typ := "type: session\n"
				if c.rel == projA {
					typ = "type: project\n"
				}
				edit(t, r, c.rel, typ, typ+c.lines)
			})
			var want []string
			for _, rule := range c.want {
				want = append(want, c.rel+" "+rule)
			}
			if !slices.Equal(got, want) {
				t.Errorf("faults %q, want %q", got, want)
			}
		})
	}
}

// The checker reads what the codec gives it as the codec says: a value a
// person wrote as its scalar, the whole of a key's lines, the body with its
// headings, every section of a repeated heading, and lines with no CR.
func TestReadsTheCodecsFields(t *testing.T) {
	dec2Hand := dec2 + "\t2026-11-10T10:00:00+01:00\tacknowledged\n"
	consHand := cons + "\t2026-11-10T10:00:00+01:00\tacknowledged\n"
	cases := []struct {
		name string
		brk  func(t *testing.T, r string)
		want []string
	}{
		// The body's first line is its heading, which is no statement.
		{"a body that opens with a heading", func(t *testing.T, r string) {
			edit(t, r, fact, "---\nThe orders service", "---\n## Detail\nThe orders service")
		}, []string{fact + " statement"}},
		// A hand-edited decision's type and state are read as scalars: a
		// quoted type and a commented state are still a decision in force.
		{"a quoted type and a commented state", func(t *testing.T, r string) {
			appendTo(t, r, edits, dec2Hand)
			edit(t, r, dec2, "type: decision\n", "type: 'decision'\n")
			edit(t, r, dec2, "state: active\n", "state: active # still\n")
		}, nil},
		// A hand-edited item's supersedes in a block list is read whole.
		{"supersedes as a block list", func(t *testing.T, r string) {
			appendTo(t, r, edits, consHand)
			edit(t, r, cons, "locked: true", "supersedes:\n  - i01qtk73d4e5f6\nlocked: true")
		}, []string{ws1 + " decisions-in-force"}},
		// A bad line in a second Log section is a log line.
		{"a second Log section", func(t *testing.T, r string) {
			appendTo(t, r, ses, "\n## Log\n- not a log line\n")
		}, []string{ses + " log"}},
		// A hand-edited workstream with CR LF ends: its working items and
		// decisions in force are read without the CR.
		{"a workstream with CR LF ends", func(t *testing.T, r string) {
			appendTo(t, r, edits, ws1+"\t2026-11-10T10:00:00+01:00\tacknowledged\n")
			put(t, r, ws1, strings.ReplaceAll(readFile(t, r, ws1), "\n", "\r\n"))
		}, nil},
		// A hand-edited item with CR LF ends: its definitions are read
		// without the CR.
		{"an item with CR LF ends", func(t *testing.T, r string) {
			put(t, r, hand, strings.ReplaceAll(readFile(t, r, hand), "\n", "\r\n"))
		}, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := faultsOf(t, c.brk); !slices.Equal(got, c.want) {
				t.Errorf("faults %q, want %q", got, c.want)
			}
		})
	}
}
