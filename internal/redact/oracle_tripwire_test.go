package redact

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"go/ast"
	"go/parser"
	"go/scanner"
	"go/token"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// The tripwire, TestOracleBoundsWidenRedactGo, guards what TestOracle cannot
// (see oracleBounds). It holds, in exceptionSource, a fingerprint of the
// source of every declaration the exceptions for code are made of:
// floorValue and plainCode, what the floor's span must pass in apply (inRef,
// opRefRe, containedBy, cuts, the markers), floorMatch and the floor's
// pattern, every declaration a widening of oracleBounds edits, as the value
// rules' reading of code, and every declaration of the package any of these
// names, in turn. A change to any of them, in redact.go, passwords.go or
// another file of the package, fails it until a person writes the new
// fingerprint into exceptionSource, and its message says what that person
// must add first (tripwireAdvice); comments and layout do not count.
// TestOracleTripwireSeesEveryWidening checks that every widening oracleBounds
// lists, named ones too, changes the fingerprint.

// exceptionRoots are the declarations the exceptions for code start from,
// besides those the widenings of oracleBounds edit.
var exceptionRoots = []string{"floorValue", "plainCode", "floorMatch", "floorValueRe", "inRef", "opRefRe", "containedBy", "cuts", "markerSpans"}

// exceptionSource is the fingerprint of the declarations of the exceptions
// for code, as exceptionListing prints it: a line each, of its file, its name
// and a hash of its tokens without its comments. TestOracleBoundsWidenRedactGo
// fails when it is not the package's.
const exceptionSource = `
redact.go aliasName 5a55f02e7680ffa9
redact.go ansiRe 108e738ad1bc15f8
redact.go argSpace c93de39fea188d9a
redact.go assigned ea91f8d5d781e991
redact.go clean 742aabf77dff3706
redact.go closeBracket 68c058ea699f893a
redact.go closers e84189e21532f8b5
redact.go closingBracket ba708d42e8b259ff
redact.go codeAfter 78d961caf04515ee
redact.go codeEnd b20d543495bdad87
redact.go codeHead 62554bac8c3473cc
redact.go codeName 38e41dc2f63941f3
redact.go containedBy 4d6e5c927dca0e07
redact.go cuts 135a8fd86e312a75
redact.go enclosing 8faad9645612b902
redact.go exprLike 570bcfd9bd82f197
redact.go floorMatch 9a5f64a32e7077a9
redact.go floorValue cdc233fe33929485
redact.go floorValueRe f95d3428fb05eac3
redact.go functionWords f1321b8d0f2aeca3
redact.go inRef 39c37509f257785f
redact.go inlineContext a61b2f9027497d58
redact.go inlineContextAt 795fea3b26afd6fc
redact.go inlineEnd 3aa950da71f4b9dc
redact.go instanceVariable f208332d4990a594
redact.go isAlnum 557b2ddb040d81de
redact.go isCode 47f479ce6dda951a
redact.go keySeparator 1db44ed7dc661676
redact.go keyTail dcd6343fe53eeb40
redact.go lineValue 14902b818a8d4093
redact.go lineValueEnd d2e18a169dd8eda5
redact.go literal eccabf198cccb458
redact.go looksLikePlaceholder 46d923eb0bdc6dc2
redact.go looksSecret 16683c992db7d705
redact.go marker 82a67563942a1a54
redact.go markerAt 061f1c4af0546924
redact.go markerOver 4b423a883e3bff92
redact.go markerRe daca5940a97e45cc
redact.go markerSpans 301507c0af83bec8
redact.go markerStart 576c4d5c279ca06a
redact.go maxCodeScan 315a26d582d781e6
redact.go maxFloorScan f253a0ead238ee93
redact.go memberPath a34c984a24f43e90
redact.go memberSegment 407cc4479ea5cc14
redact.go minValueLen 0f0f5c340930231f
redact.go nextPair 36c89a50bdfa0834
redact.go nextSecretKey 2adc8236fd41b044
redact.go opRef 84901c1f8bca5cfa
redact.go opRefRe 093df1f79ef672a0
redact.go passphrase 1c982a4bcf451b4c
redact.go passwordWords 84e079dd25a074a0
redact.go percentEnd 6042053458320b4d
redact.go percentHead 6f4bf0c1f0975301
redact.go placeholder 954e9654048406ff
redact.go plainCode c3a17e81c8ed9547
redact.go quotedName 8652d6fe9e50b508
redact.go refHead 9cad60beacd249dc
redact.go regexFlags a02edcdcea91f805
redact.go regexHead bfe5a5c03b8d819a
redact.go regexLiteral f7e2ed2607d3224a
redact.go secretKeyAt 30954b9bbd91ecb0
redact.go shortName b5be2aaa1f1cff02
redact.go span 9293d2c1cbb245e7
redact.go tokenWords d364529ae1794fef
redact.go typeHead 4c50a5df12d86b95
redact.go typeName 823ba145a75b7699
redact.go valueStops 74ee32d61fd71a7a
redact.go variableRef 71cebf89f2ecf1d5
redact.go wordLike 5a6f7e7dcdcc08a5
redact.go wordyName 4a627017c7b0aab2
redact.go yamlProperties bd7a77f608a0777d
`

// packageSources reads the package's files but its tests, by name.
func packageSources(t *testing.T) map[string]string {
	t.Helper()
	names, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	files := map[string]string{}
	for _, n := range names {
		if strings.HasSuffix(n, "_test.go") {
			continue
		}
		b, err := os.ReadFile(n)
		if err != nil {
			t.Fatal(err)
		}
		files[n] = string(b)
	}
	if files["redact.go"] == "" || files["passwords.go"] == "" {
		t.Fatalf("the package's files: %v", slices.Sorted(maps.Keys(files)))
	}
	return files
}

// pkgDecl is a top-level declaration of the package: a function, a method,
// or one name of a const, var or type.
type pkgDecl struct {
	file     string
	node     ast.Node
	pos, end int    // its bytes in file, its doc comment left out
	tokens   string // its tokens, without comments (sourceTokens)
}

// sourceTokens returns the tokens of Go source src, one a line, without its
// comments and the semicolons a line's end makes: its comments and its
// layout do not change them.
func sourceTokens(src string) string {
	fset := token.NewFileSet()
	var sc scanner.Scanner
	sc.Init(fset.AddFile("", -1, len(src)), []byte(src), nil, 0)
	var b strings.Builder
	for {
		_, tok, lit := sc.Scan()
		if tok == token.EOF {
			return b.String()
		}
		if tok == token.SEMICOLON && lit == "\n" {
			continue
		}
		fmt.Fprintf(&b, "%s %s\n", tok, lit)
	}
}

// packageDecls parses files and returns their top-level declarations by
// name; a method is named by its receiver's type, a dot and its name.
func packageDecls(files map[string]string) (map[string]*pkgDecl, error) {
	fset := token.NewFileSet()
	decls := map[string]*pkgDecl{}
	add := func(name, file string, node ast.Node, pos, end token.Pos) error {
		if decls[name] != nil {
			return fmt.Errorf("%s is declared twice", name)
		}
		d := &pkgDecl{file: file, node: node, pos: fset.Position(pos).Offset, end: fset.Position(end).Offset}
		d.tokens = sourceTokens(files[file][d.pos:d.end])
		decls[name] = d
		return nil
	}
	for _, name := range slices.Sorted(maps.Keys(files)) {
		f, err := parser.ParseFile(fset, name, files[name], parser.SkipObjectResolution)
		if err != nil {
			return nil, err
		}
		for _, d := range f.Decls {
			switch d := d.(type) {
			case *ast.FuncDecl:
				id := d.Name.Name
				if d.Recv != nil && len(d.Recv.List) == 1 {
					rt := d.Recv.List[0].Type
					if s, ok := rt.(*ast.StarExpr); ok {
						rt = s.X
					}
					if i, ok := rt.(*ast.Ident); ok {
						id = i.Name + "." + id
					}
				}
				if err := add(id, name, d, d.Pos(), d.End()); err != nil {
					return nil, err
				}
			case *ast.GenDecl:
				for _, s := range d.Specs {
					// A declaration of one spec with no brackets starts at its
					// keyword.
					pos, end := s.Pos(), s.End()
					if !d.Lparen.IsValid() {
						pos, end = d.Pos(), d.End()
					}
					switch s := s.(type) {
					case *ast.ValueSpec:
						for _, id := range s.Names {
							if err := add(id.Name, name, s, pos, end); err != nil {
								return nil, err
							}
						}
					case *ast.TypeSpec:
						if err := add(s.Name.Name, name, s, pos, end); err != nil {
							return nil, err
						}
					}
				}
			}
		}
	}
	return decls, nil
}

// exceptionRootsOf returns exceptionRoots and the declarations of redact.go
// that hold the old text of each edit of each widening of oracleBounds,
// named ones too.
func exceptionRootsOf(decls map[string]*pkgDecl, redactGo string) ([]string, error) {
	roots := slices.Clone(exceptionRoots)
	for _, b := range oracleBounds {
		for _, w := range append(b.widenings(), b.named...) {
			for _, e := range w {
				i := strings.Index(redactGo, e.old)
				if i < 0 {
					// TestOracleBoundsWidenRedactGo reports it.
					continue
				}
				found := false
				for name, d := range decls {
					if d.file == "redact.go" && d.pos <= i && i+len(e.old) <= d.end {
						roots = append(roots, name)
						found = true
					}
				}
				if !found {
					return nil, fmt.Errorf("bound %s: %q is in no declaration of redact.go", b.id, e.old)
				}
			}
		}
	}
	slices.Sort(roots)
	return slices.Compact(roots), nil
}

// exceptionListing returns, for the declarations roots name and every
// declaration of the package they name in turn, a line each, sorted: the
// file, the name and the first 16 hex digits of the SHA-256 of its tokens
// (sourceTokens).
func exceptionListing(decls map[string]*pkgDecl, roots []string) (string, error) {
	seen := map[string]bool{}
	todo := slices.Clone(roots)
	for len(todo) > 0 {
		name := todo[len(todo)-1]
		todo = todo[:len(todo)-1]
		if seen[name] {
			continue
		}
		d := decls[name]
		if d == nil {
			return "", fmt.Errorf("no declaration %s in the package", name)
		}
		seen[name] = true
		var walk func(n ast.Node) bool
		walk = func(n ast.Node) bool {
			switch n := n.(type) {
			case *ast.SelectorExpr:
				// A field or a method, or a name of another package.
				ast.Inspect(n.X, walk)
				return false
			case *ast.Ident:
				if decls[n.Name] != nil && !seen[n.Name] {
					todo = append(todo, n.Name)
				}
			}
			return true
		}
		ast.Inspect(d.node, walk)
	}
	var lines []string
	for name := range seen {
		d := decls[name]
		sum := sha256.Sum256([]byte(d.tokens))
		lines = append(lines, fmt.Sprintf("%s %s %s", d.file, name, hex.EncodeToString(sum[:8])))
	}
	slices.Sort(lines)
	return strings.Join(lines, "\n"), nil
}

// The tripwire sees every widening: each widening of each bound that
// oracleBounds lists, named ones too, applied to redact.go alone, changes
// the fingerprint TestOracleBoundsWidenRedactGo compares, from the same
// roots; and so does a change of each declaration it holds, but not a
// comment.
func TestOracleTripwireSeesEveryWidening(t *testing.T) {
	isolate(t)
	files := packageSources(t)
	decls, err := packageDecls(files)
	if err != nil {
		t.Fatal(err)
	}
	roots, err := exceptionRootsOf(decls, files["redact.go"])
	if err != nil {
		t.Fatal(err)
	}
	listing := func(files map[string]string) string {
		t.Helper()
		d, err := packageDecls(files)
		if err != nil {
			t.Fatal(err)
		}
		l, err := exceptionListing(d, roots)
		if err != nil {
			t.Fatal(err)
		}
		return l
	}
	before := listing(files)
	if before != strings.TrimSpace(exceptionSource) {
		t.Fatalf("exceptionSource is not the package's: see TestOracleBoundsWidenRedactGo")
	}
	n := 0
	for _, b := range oracleBounds {
		for k, w := range append(b.widenings(), b.named...) {
			widened, err := applyWidening(files["redact.go"], w)
			if err != nil {
				t.Fatalf("bound %s, widening %d: %v", b.id, k+1, err)
			}
			changed := maps.Clone(files)
			changed["redact.go"] = widened
			if listing(changed) == before {
				t.Errorf("bound %s, widening %d: the tripwire does not see it", b.id, k+1)
			}
			n++
		}
	}
	// Every declaration it holds: a change of its last name or string, as a
	// widening could make, is seen; a comment added after it is not.
	held := strings.Split(before, "\n")
	for _, line := range held {
		name := strings.Fields(line)[1]
		d := decls[name]
		src := files[d.file]
		changed := maps.Clone(files)
		changed[d.file] = src[:d.end] + " // a comment" + src[d.end:]
		if listing(changed) != before {
			t.Errorf("%s: a comment after it changes the fingerprint", name)
		}
		at := -1
		fset := token.NewFileSet()
		var sc scanner.Scanner
		sc.Init(fset.AddFile("", -1, d.end-d.pos), []byte(src[d.pos:d.end]), nil, 0)
		for {
			pos, tok, _ := sc.Scan()
			if tok == token.EOF {
				break
			}
			if tok == token.IDENT || tok == token.STRING {
				at = d.pos + fset.Position(pos).Offset + 1
			}
		}
		changed[d.file] = src[:at] + "x" + src[at:]
		if listing(changed) == before {
			t.Errorf("%s: a change at byte %d of %s is not seen", name, at, d.file)
		}
	}
	t.Logf("%d widenings seen; %d declarations held", n, len(held))
}
