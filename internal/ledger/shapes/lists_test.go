package shapes

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

// Every name on a list says where it comes from: lists.go builds each list
// of a host's names from calls of from only (an empty list may be written
// as one), each from names one of the sources lists.go declares, and each
// source is either a file of this repository that holds every name taken
// from it as a word, or a host's published source or documents; and the
// names the calls give are the list's, in order. A list lists.go declares
// that this test does not know fails it, so a list added later is held to
// the same rule.
func TestEveryListedNameSaysWhereItComesFrom(t *testing.T) {
	lists := map[string][]string{
		"claudeRowTypes": claudeRowTypes, "claudeRowFields": claudeRowFields, "claudeMessageFields": claudeMessageFields,
		"claudeItemTypes": claudeItemTypes, "claudeItemFields": claudeItemFields, "claudeSubtypes": claudeSubtypes,
		"claudeEntrypoints": claudeEntrypoints, "turnOrigins": turnOrigins, "codexRowTypes": codexRowTypes,
		"codexRowFields": codexRowFields, "codexPayloadTypes": codexPayloadTypes, "codexPayloadFields": codexPayloadFields,
		"codexSources": codexSources, "codexOriginators": codexOriginators,
	}
	// The report's own words and buckets, which are eva-md's, not a host's.
	own := []string{"settledInFull", "settledInPart", "notByShapes", "figureWords", "sizeBuckets", "gapBuckets", "fileBuckets"}
	// The constants a list may give a name by.
	named := map[string]string{"branchField": branchField}

	file, err := parser.ParseFile(token.NewFileSet(), "lists.go", nil, parser.SkipObjectResolution)
	if err != nil {
		t.Fatal(err)
	}
	sources := map[string]string{}
	var vars []*ast.ValueSpec
	for _, decl := range file.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok {
			continue
		}
		for _, spec := range gen.Specs {
			vs, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			if gen.Tok == token.VAR {
				vars = append(vars, vs)
				continue
			}
			if typ, ok := vs.Type.(*ast.Ident); gen.Tok == token.CONST && ok && typ.Name == "source" {
				for i, name := range vs.Names {
					value := ""
					if lit, ok := vs.Values[i].(*ast.BasicLit); ok {
						value, _ = strconv.Unquote(lit.Value)
					}
					if strings.TrimSpace(value) == "" {
						t.Errorf("the source %s is not a string that says where", name.Name)
						continue
					}
					sources[name.Name] = value
				}
			}
		}
	}
	type taken struct{ name, source string }
	found := map[string]bool{}
	for _, vs := range vars {
		for i, ident := range vs.Names {
			name := ident.Name
			if slices.Contains(own, name) {
				continue
			}
			want, ok := lists[name]
			if !ok {
				t.Errorf("lists.go declares %s, which this test does not know: a list of a host's names is added to the test's", name)
				continue
			}
			found[name] = true
			var got []taken
			var walk func(e ast.Expr)
			walk = func(e ast.Expr) {
				switch e := e.(type) {
				case *ast.CompositeLit:
					if len(e.Elts) > 0 {
						t.Errorf("%s holds names that no from gives a source for", name)
					}
					return
				case *ast.CallExpr:
					if fn, ok := e.Fun.(*ast.Ident); ok && fn.Name == "from" && len(e.Args) > 0 {
						src, ok := e.Args[0].(*ast.Ident)
						if !ok || sources[src.Name] == "" {
							t.Errorf("%s: a from names no source lists.go declares", name)
							return
						}
						for _, arg := range e.Args[1:] {
							switch arg := arg.(type) {
							case *ast.BasicLit:
								s, err := strconv.Unquote(arg.Value)
								if err != nil {
									t.Errorf("%s: %s is not a string", name, arg.Value)
								}
								got = append(got, taken{s, sources[src.Name]})
							case *ast.Ident:
								s, ok := named[arg.Name]
								if !ok {
									t.Errorf("%s: %s is not a constant this test knows", name, arg.Name)
								}
								got = append(got, taken{s, sources[src.Name]})
							default:
								t.Errorf("%s: a from gives a name that is not a string", name)
							}
						}
						return
					}
					if sel, ok := e.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == "Concat" {
						for _, arg := range e.Args {
							walk(arg)
						}
						return
					}
				}
				t.Errorf("%s is not built of from calls only", name)
			}
			if i < len(vs.Values) {
				walk(vs.Values[i])
			}
			var names []string
			for _, g := range got {
				names = append(names, g.name)
			}
			if !slices.Equal(names, want) {
				t.Errorf("%s's from calls give %q, the list holds %q", name, names, want)
			}
			for _, g := range got {
				if rest, ok := strings.CutPrefix(g.source, "published: "); ok {
					if strings.TrimSpace(rest) == "" {
						t.Errorf("%s: %q's published source says nothing", name, g.name)
					}
					continue
				}
				data, err := os.ReadFile(filepath.Join("..", "..", "..", filepath.FromSlash(g.source)))
				if err != nil {
					t.Errorf("%s: %q's source: %v", name, g.name, err)
					continue
				}
				word := regexp.MustCompile(`(^|[^A-Za-z0-9_-])` + regexp.QuoteMeta(g.name) + `($|[^A-Za-z0-9_-])`)
				if !word.Match(data) {
					t.Errorf("%s: %q is not in its source, %s", name, g.name, g.source)
				}
			}
		}
	}
	for name := range lists {
		if !found[name] {
			t.Errorf("lists.go no longer declares %s", name)
		}
	}
}

// The facts the report settles in full or in part, and those it says
// shapes cannot settle, are together exactly the facts the host fact sheet
// marks "by the field report", each once.
func TestTheFactsAreTheSheets(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "..", "docs", "v2", "host-facts.md"))
	if err != nil {
		t.Fatal(err)
	}
	header := regexp.MustCompile(`^#### (F-[0-9]{3})\.`)
	var marked []string
	fact := ""
	for _, line := range strings.Split(string(data), "\n") {
		if m := header.FindStringSubmatch(line); m != nil {
			fact = m[1]
			continue
		}
		if strings.HasPrefix(line, "- **Settled by.**") && strings.Contains(line, "**Mark: by the field report") && fact != "" {
			marked = append(marked, fact)
		}
	}
	if len(marked) != 51 {
		t.Fatalf("the sheet marks %d facts by the field report: %v", len(marked), marked)
	}
	var ours []string
	for _, l := range [][]string{settledInFull, settledInPart, notByShapes} {
		ours = append(ours, l...)
	}
	sorted := slices.Sorted(slices.Values(ours))
	if len(slices.Compact(slices.Clone(sorted))) != len(ours) {
		t.Errorf("a fact is listed twice: %v", sorted)
	}
	slices.Sort(marked)
	if !slices.Equal(sorted, marked) {
		t.Errorf("the report lists\n%v\nthe sheet marks\n%v", sorted, marked)
	}
}

// No list holds a name twice, other, or anything but printable ASCII; and
// Words holds every list's names.
func TestTheLists(t *testing.T) {
	words := Words()
	for _, l := range [][]string{claudeRowTypes, claudeRowFields, claudeMessageFields, claudeItemTypes, claudeItemFields,
		claudeSubtypes, claudeEntrypoints, turnOrigins, codexRowTypes, codexRowFields, codexPayloadTypes, codexPayloadFields,
		codexSources, codexOriginators} {
		if len(slices.Compact(slices.Sorted(slices.Values(l)))) != len(l) || slices.Contains(l, other) {
			t.Errorf("%v holds a name twice, or other", l)
		}
		for _, n := range l {
			if _, ok := slices.BinarySearch(words, n); !ok {
				t.Errorf("Words lacks %q", n)
			}
			for _, b := range []byte(n) {
				if b < 0x21 || b > 0x7e {
					t.Errorf("%q is not printable ASCII", n)
				}
			}
		}
	}
}
