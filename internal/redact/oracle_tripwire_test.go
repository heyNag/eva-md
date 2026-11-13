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
// (see oracleBounds). It holds, in exceptionSource, every top-level
// declaration of every Go file of this folder that is not a test file: each
// function, each method, named with its receiver as rule.runs, each var,
// const and type, and each init, with its file and a hash of its tokens, and
// for each file a line of its package clause, its imports and its //go:
// directives. No list of names and no walk from one decide what it holds. A
// declaration changed in any token, added, removed or moved to another file
// fails it, and so does a new file, until a person writes the new
// fingerprint into exceptionSource; its message says what that person must
// add first (tripwireAdvice). Comments and layout do not count. It does not
// hold internal/redact/baseline, another package, which is frozen by rule,
// nor the code outside this package that reads the switch
// redaction_current_rules_only. It reads the files on disk, so a file changed
// with go test -overlay does not reach it.
// TestOracleTripwireHoldsEveryDeclaration checks that it reads every
// non-test file of the folder and has a line for every declaration the
// parser finds in them; TestOracleTripwireSeesEveryWidening checks that every
// widening oracleBounds lists, named ones too, changes the fingerprint, and
// that a change to each declaration it holds does but a comment does not.

// exceptionSource is the fingerprint, as exceptionListing prints it: a line
// for each top-level declaration of the package's files outside its tests,
// of its file, its name and the first 16 hex digits of the SHA-256 of its
// tokens without their comments, and a line for each file, named (file), of
// its package clause, its imports and its //go: directives. A method is
// named with its receiver's type; a name declared more than once, as init or
// _, has '#' and its count from the second on, so each has a line of its
// own. TestOracleBoundsWidenRedactGo fails when it is not the package's.
// Before a person writes a new one here, they do what tripwireAdvice says.
const exceptionSource = `
alone.go (file) 859495d462914b58
alone.go BaselineAtRunTime 5123543bd922d8a8
alone.go SetBaselineAtRunTime 854027a61933d866
alone.go baselineOff 67abb47a127b4f92
alone.go currentRules 9ebb739eb8ba46d5
alone.go driver.runAlone b3048e889a3e71cb
alone.go maxAloneRounds 5c4b62bb02bce762
alone.go trace.alone 638529401f5994d5
passwords.go (file) e0f0da360b3e681d
passwords.go afterColon abd30b6da6d73258
passwords.go afterLogin 7cbe4599bd709f9d
passwords.go blanks 1ebd98fb5902964f
passwords.go caseChange 49a94e40cb046380
passwords.go closingQuote af483321f591284c
passwords.go command 0b023a9eec525058
passwords.go commandAt 1b3713ca29a9bbc2
passwords.go commandMatches aeb27800d7eb1008
passwords.go commandWords 7abc91c0055c4f82
passwords.go commandsRead b83fab75eba7bbaf
passwords.go curlCommand 482b9a104fb14e2d
passwords.go curlPassword ecaff1010e4e6f69
passwords.go flagValue 68acb38b0b9ea5ef
passwords.go followed aaff605049987ece
passwords.go generated 26eeca2594cfaf58
passwords.go joined f5e7b7298f2c0a88
passwords.go joiners 959a392efb6bb391
passwords.go lineBreak 11f406f3d15add5c
passwords.go loginCommand f80556b1a4165c1c
passwords.go loginPassword 69aa1ff45957c491
passwords.go maxName 423c69ec5abe66f5
passwords.go measurement c787fb582a634d7c
passwords.go mysqlCommand ce07c3fc1e365aa0
passwords.go mysqlPassword dffa5b17090b394d
passwords.go nameByte f1f76701261ac418
passwords.go opensslCommand 864b7250a33bf2fd
passwords.go opensslPassword d848a7e295419e9f
passwords.go ordinaryWords fe3960a0acd97e04
passwords.go part 01e86f79a6eb5167
passwords.go part.inside c321b01988f7ea21
passwords.go passArgument 25fcb970ce8ed830
passwords.go passGate d81bc248f36851a3
passwords.go passKey 2cbbe1cbea01df51
passwords.go passKeyQuoted 15e68e448a2bf1e7
passwords.go passKeyRe 27bda4a28c98bd00
passwords.go passKeySingle 185dcf9ce0ee269f
passwords.go passValue e94e9d58d2b80dd3
passwords.go passwordAt 5b4a45f5dae6b209
passwords.go passwordLike 0690993206b7cd8c
passwords.go pathLike 4db55eb75ab2acfc
passwords.go proseGate 8187adb451b8a23a
passwords.go prosePassword c9cc50f81eb842bb
passwords.go proseValue e8a263ce05901981
passwords.go quotedPassValue 2097c5a859ec0cb0
passwords.go redirection 480e77c028bcb5cb
passwords.go redisCommand 42510f5bc1688db7
passwords.go redisPassword b8ec26539a83a7bd
passwords.go shellLiteral 2663ec9fbaa942c1
passwords.go sshpassCommand f0fb3ea586fd0372
passwords.go sshpassName b3d5a5d64e4d13cb
passwords.go sshpassOptions d1567df1b3251dcb
passwords.go sshpassPassword 6e73cf5a487f6ba1
passwords.go startsCommand c2fc85e990eb1fbf
passwords.go technicalTerm 809ca8e7de508d34
passwords.go units 09c960573c6779af
passwords.go unopened 110d5695b822f477
passwords.go userPassword 3e85e531c89a7f3d
passwords.go wordByte fb2b72dc14afaf25
passwords.go wordEnd b83a2ebdd8b9f21c
passwords.go wordParts 49447222749f22b9
redact.go (file) 0af788d66d095837
redact.go Identifier 4e8ab48568469dc8
redact.go RedactKnown 40d43f4050ddf0ad
redact.go Version a45196f3a6df89c0
redact.go aliasName 5a55f02e7680ffa9
redact.go allDigits b40f2b17fbfb8eb4
redact.go ansiRe 108e738ad1bc15f8
redact.go apply 1cb5ea134c64aa86
redact.go argSpace c93de39fea188d9a
redact.go assigned ea91f8d5d781e991
redact.go atLineStart c6755862e5739863
redact.go clean 742aabf77dff3706
redact.go closeBracket 68c058ea699f893a
redact.go closers e84189e21532f8b5
redact.go closingBracket ba708d42e8b259ff
redact.go codeAfter 78d961caf04515ee
redact.go codeEnd b20d543495bdad87
redact.go codeHead 62554bac8c3473cc
redact.go codeName 38e41dc2f63941f3
redact.go containedBy 4d6e5c927dca0e07
redact.go covered 0d877b1ef07791d7
redact.go cutRunes ea08aa9ed184873d
redact.go cuts 135a8fd86e312a75
redact.go dropOverlaps 90291c024fd304bb
redact.go enclosing 8faad9645612b902
redact.go entropy 3dbec25fddb1e6af
redact.go entropyMaxBytes 1dff00b001322699
redact.go entropyToken a3eef3ba14b04b49
redact.go entropyTrigger 9a1a58285ae36b62
redact.go entropyWords 8b2c70b60ffae3ce
redact.go exprLike 570bcfd9bd82f197
redact.go floorMatch 9a5f64a32e7077a9
redact.go floorValue cdc233fe33929485
redact.go floorValueRe f95d3428fb05eac3
redact.go functionWords f1321b8d0f2aeca3
redact.go hasDigit 82c5dba474b6d2ff
redact.go hasLetter a01fe470b0071568
redact.go hasLower 584ca4a5026fe4ed
redact.go hasUpper 291b1648e24bbd87
redact.go highEntropy 9d4a4eaf2fffab9f
redact.go hinted eaed1a8d62900a89
redact.go identifier 340f3bdb606c2f5a
redact.go inRef 39c37509f257785f
redact.go inlineContext a61b2f9027497d58
redact.go inlineContextAt 795fea3b26afd6fc
redact.go inlineEnd 3aa950da71f4b9dc
redact.go inlineLimit efa572c161f51cb0
redact.go instanceVariable f208332d4990a594
redact.go isAlnum 557b2ddb040d81de
redact.go isCode 47f479ce6dda951a
redact.go isJWT 912b117bb6f90b41
redact.go keep d887f9b1795dbb4e
redact.go keySeparator 1db44ed7dc661676
redact.go keyTail dcd6343fe53eeb40
redact.go laterKey 5f79a35a998e1d28
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
redact.go maxPasses dad48cbce8315093
redact.go memberPath a34c984a24f43e90
redact.go memberSegment 407cc4479ea5cc14
redact.go mergeOverlaps 3536eb6bb944177c
redact.go minEntropyBits fc6ead2b86a131ba
redact.go minEntropyLen 7a7f41221af73753
redact.go minValueLen 0f0f5c340930231f
redact.go mode 4a5b16c1cb7ced58
redact.go nameish 5146ecab368cac2a
redact.go nextPair 36c89a50bdfa0834
redact.go nextSecretKey 2adc8236fd41b044
redact.go opRef 84901c1f8bca5cfa
redact.go opRefRe 093df1f79ef672a0
redact.go overlapsAny ca4dd9d5a74f624c
redact.go pass fbdb0b0543c6f484
redact.go passphrase 1c982a4bcf451b4c
redact.go passwordWords 84e079dd25a074a0
redact.go percentEnd 6042053458320b4d
redact.go percentHead 6f4bf0c1f0975301
redact.go placeholder 954e9654048406ff
redact.go plainCode c3a17e81c8ed9547
redact.go quotedName 8652d6fe9e50b508
redact.go redact cfbaeaddb5a45142
redact.go redactEntropy b6be84b4d71f34fe
redact.go redactUntil 48b0c8e9ac616967
redact.go refHead 9cad60beacd249dc
redact.go regexFlags a02edcdcea91f805
redact.go regexHead bfe5a5c03b8d819a
redact.go regexLiteral f7e2ed2607d3224a
redact.go replace af9e877ebbcca0b2
redact.go rule 947efd1dc7daa5e0
redact.go rule.runs fe0c922a0633a1d7
redact.go rules 69ac4665b50064cf
redact.go secretKeyAt 30954b9bbd91ecb0
redact.go secretValue 601c27767e913900
redact.go shaRe e01f98d5e4ca71dc
redact.go shortName b5be2aaa1f1cff02
redact.go span 9293d2c1cbb245e7
redact.go tokenWords d364529ae1794fef
redact.go typeHead 4c50a5df12d86b95
redact.go typeName 823ba145a75b7699
redact.go uuidRe 3b07247cd496caac
redact.go valueRe 5eeafdd29582d69c
redact.go valueStops 74ee32d61fd71a7a
redact.go variableRef 71cebf89f2ecf1d5
redact.go window bd17c628eb3343b1
redact.go wordLike 5a6f7e7dcdcc08a5
redact.go wordy 76f362dae0c62c4d
redact.go wordyName 4a627017c7b0aab2
redact.go yamlProperties bd7a77f608a0777d
scan.go (file) 4abce62b78f18044
scan.go Apply 184cd37f6c7fa84d
scan.go Certain ba8ed71fa626987c
scan.go Finding c19365dbd0e8a414
scan.go FloorOnly 7f3d622161e16b04
scan.go Likely 0681d96b97d484f9
scan.go Result 48afdc76361ab290
scan.go RuleBaseline fbbea0ecc067077d
scan.go RuleCurrent 1976327634dc16e0
scan.go Scan a42642731a9ef4f7
scan.go Tier b17cda391434b893
scan.go baselineRedact c1e5c994a0c14ab4
scan.go bothRules 6d3522556c775689
scan.go driver 12da60e2ad993531
scan.go driver.run f28f5c890ada5162
scan.go driver.scan 2b388cc6796f3425
scan.go driver.stop 965db1179494e0ad
scan.go kindTier 00063a3a3ebbedb8
scan.go newTrace 532c53ae8e52b1c2
scan.go piece 0486352de6beb0f6
scan.go piece.size cc078e8b28c7d346
scan.go remap 370527cc2b437fd2
scan.go trace 3f9fe2105ec06bc2
scan.go trace.privateKeys 50b42b2f370df77d
scan.go trace.replace bd61a3ce21268714
scan.go trace.result 5237fb55f49a3789
scan.go trace.union 031a0da454ad5104
union.go (file) e09fb6cfa2e3999f
union.go Redact 1bd83c530c5a0565
union.go isMarker 2610dc42ab344b43
union.go privateKeyBlock 9cbf8b38ae43e070
union.go redacted 9fc99d371bbf41c8
union.go removal 27a6926a442e0573
union.go removals f635beeb0e65d402
union.go unionRanges 60ebf61aa71f916d
`

// packageSources reads the package's files but its tests, by name: every
// file of the folder whose name ends in .go but not in _test.go.
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

// receiverType returns the name of a method's receiver type, as rule for
// func (r rule) and trace for func (t *trace), or "" when it has none.
func receiverType(e ast.Expr) string {
	for {
		switch x := e.(type) {
		case *ast.StarExpr:
			e = x.X
		case *ast.ParenExpr:
			e = x.X
		case *ast.IndexExpr:
			e = x.X
		case *ast.IndexListExpr:
			e = x.X
		case *ast.Ident:
			return x.Name
		default:
			return ""
		}
	}
}

// packageDecls parses files and returns every top-level declaration of
// them by name, and for each file the tokens of its package clause, its
// imports and its //go: and +build lines (heads). A method is named by its
// receiver's type, a dot and its name. A name declared again, as a second
// init or _, is named with '#' and its count, in the order of the sorted
// files and of each file's source.
func packageDecls(files map[string]string) (decls map[string]*pkgDecl, heads map[string]string, err error) {
	fset := token.NewFileSet()
	decls = map[string]*pkgDecl{}
	heads = map[string]string{}
	count := map[string]int{}
	add := func(name, file string, pos, end token.Pos) {
		count[name]++
		if count[name] > 1 {
			name = fmt.Sprintf("%s#%d", name, count[name])
		}
		d := &pkgDecl{file: file, pos: fset.Position(pos).Offset, end: fset.Position(end).Offset}
		d.tokens = sourceTokens(files[file][d.pos:d.end])
		decls[name] = d
	}
	for _, name := range slices.Sorted(maps.Keys(files)) {
		src := files[name]
		f, err := parser.ParseFile(fset, name, src, parser.ParseComments|parser.SkipObjectResolution)
		if err != nil {
			return nil, nil, err
		}
		var head strings.Builder
		head.WriteString(sourceTokens(src[fset.Position(f.Package).Offset:fset.Position(f.Name.End()).Offset]))
		for _, g := range f.Comments {
			for _, c := range g.List {
				if strings.HasPrefix(c.Text, "//go:") || strings.HasPrefix(c.Text, "// +build") {
					head.WriteString(c.Text + "\n")
				}
			}
		}
		for _, d := range f.Decls {
			switch d := d.(type) {
			case *ast.FuncDecl:
				id := d.Name.Name
				if d.Recv != nil && len(d.Recv.List) == 1 {
					id = receiverType(d.Recv.List[0].Type) + "." + id
				}
				add(id, name, d.Pos(), d.End())
			case *ast.GenDecl:
				if d.Tok == token.IMPORT {
					head.WriteString(sourceTokens(src[fset.Position(d.Pos()).Offset:fset.Position(d.End()).Offset]))
					continue
				}
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
							add(id.Name, name, pos, end)
						}
					case *ast.TypeSpec:
						add(s.Name.Name, name, pos, end)
					}
				}
			}
		}
		heads[name] = head.String()
	}
	return decls, heads, nil
}

// exceptionListing returns the tripwire's fingerprint of files: a line for
// every top-level declaration of them and one, named (file), for each
// file's head (packageDecls), sorted. Each line is the file, the name and
// the first 16 hex digits of the SHA-256 of the tokens.
func exceptionListing(files map[string]string) (string, error) {
	decls, heads, err := packageDecls(files)
	if err != nil {
		return "", err
	}
	line := func(file, name, tokens string) string {
		sum := sha256.Sum256([]byte(tokens))
		return fmt.Sprintf("%s %s %s", file, name, hex.EncodeToString(sum[:8]))
	}
	var lines []string
	for name, d := range decls {
		lines = append(lines, line(d.file, name, d.tokens))
	}
	for file, h := range heads {
		lines = append(lines, line(file, "(file)", h))
	}
	slices.Sort(lines)
	return strings.Join(lines, "\n"), nil
}

// The tripwire reads every file of the package but its tests, and has a line
// for every declaration the parser finds in them and one for each file.
func TestOracleTripwireHoldsEveryDeclaration(t *testing.T) {
	isolate(t)
	files := packageSources(t)
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	var want []string
	for _, e := range entries {
		if n := e.Name(); !e.IsDir() && strings.HasSuffix(n, ".go") && !strings.HasSuffix(n, "_test.go") {
			want = append(want, n)
		}
	}
	if got := slices.Sorted(maps.Keys(files)); !slices.Equal(got, want) {
		t.Fatalf("the tripwire reads %v; the folder's files but its tests are %v", got, want)
	}
	listing, err := exceptionListing(files)
	if err != nil {
		t.Fatal(err)
	}
	// What the listing has, a name each, by file: a method's name without its
	// receiver and a name declared again without its count.
	held := map[string][]string{}
	for _, l := range strings.Split(listing, "\n") {
		f := strings.Fields(l)
		if len(f) != 3 {
			t.Fatalf("a line of the listing: %q", l)
		}
		name := f[1]
		if i := strings.IndexByte(name, '#'); i >= 0 {
			name = name[:i]
		}
		if i := strings.LastIndexByte(name, '.'); i >= 0 {
			name = name[i+1:]
		}
		held[f[0]] = append(held[f[0]], name)
	}
	// What the parser finds, read again on its own.
	n := 0
	for _, file := range want {
		f, err := parser.ParseFile(token.NewFileSet(), file, files[file], parser.SkipObjectResolution)
		if err != nil {
			t.Fatal(err)
		}
		found := []string{"(file)"}
		for _, d := range f.Decls {
			switch d := d.(type) {
			case *ast.FuncDecl:
				found = append(found, d.Name.Name)
			case *ast.GenDecl:
				for _, s := range d.Specs {
					switch s := s.(type) {
					case *ast.ValueSpec:
						for _, id := range s.Names {
							found = append(found, id.Name)
						}
					case *ast.TypeSpec:
						found = append(found, s.Name.Name)
					}
				}
			}
		}
		slices.Sort(found)
		got := held[file]
		slices.Sort(got)
		if !slices.Equal(got, found) {
			t.Errorf("%s: the listing holds %v; the parser finds %v", file, got, found)
		}
		n += len(found) - 1
	}
	for file := range held {
		if !slices.Contains(want, file) {
			t.Errorf("the listing holds %s, which is no file of the package", file)
		}
	}
	// Two declarations of one name each have a line, which changes when either
	// changes; a method and a function of one name do too.
	twice := "package redact\n\nfunc init() { _ = 1 }\n\nfunc init() { _ = 2 }\n\nvar _ = 3\n\nvar _ = 4\n\ntype T[K any] struct{}\n\nfunc (T[K]) runs() {}\n\nfunc runs() {}\n"
	l, err := exceptionListing(map[string]string{"x.go": twice})
	if err != nil {
		t.Fatal(err)
	}
	if lines := strings.Split(l, "\n"); len(lines) != 8 {
		t.Errorf("two declarations of one name share a line:\n%s", l)
	}
	for _, edit := range [][2]string{{"_ = 1", "_ = 5"}, {"_ = 2", "_ = 5"}, {"_ = 3", "_ = 5"}, {"_ = 4", "_ = 5"}, {"(T[K]) runs() {}", "(T[K]) runs() { _ = 5 }"}, {"func runs() {}", "func runs() { _ = 5 }"}} {
		c, err := exceptionListing(map[string]string{"x.go": strings.Replace(twice, edit[0], edit[1], 1)})
		if err != nil {
			t.Fatal(err)
		}
		if c == l {
			t.Errorf("a change of %q to %q is not seen", edit[0], edit[1])
		}
	}
	t.Logf("%d files, %d declarations", len(want), n)
}

// The tripwire sees every widening: each widening of each bound that
// oracleBounds lists, named ones too, applied to redact.go alone, changes
// the fingerprint TestOracleBoundsWidenRedactGo compares; and so does a
// change of each declaration it holds, of a file's head, a new file, a
// file removed or a declaration moved, but not a comment.
func TestOracleTripwireSeesEveryWidening(t *testing.T) {
	isolate(t)
	files := packageSources(t)
	decls, _, err := packageDecls(files)
	if err != nil {
		t.Fatal(err)
	}
	listing := func(files map[string]string) string {
		t.Helper()
		l, err := exceptionListing(files)
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
	for name, d := range decls {
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
	// Each file's head: a build constraint or an import is seen, a comment is
	// not; and so is the file's removal.
	for file, src := range files {
		changed := maps.Clone(files)
		changed[file] = "// a comment\n" + src
		if listing(changed) != before {
			t.Errorf("%s: a comment at its top changes the fingerprint", file)
		}
		changed[file] = "//go:build ignore\n\n" + src
		if listing(changed) == before {
			t.Errorf("%s: a build constraint is not seen", file)
		}
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, file, src, parser.PackageClauseOnly)
		if err != nil {
			t.Fatal(err)
		}
		i := fset.Position(f.Name.End()).Offset
		changed[file] = src[:i] + "\n\nimport _ \"unicode\"\n" + src[i:]
		if listing(changed) == before {
			t.Errorf("%s: a new import is not seen", file)
		}
		delete(changed, file)
		if listing(changed) == before {
			t.Errorf("%s: its removal is not seen", file)
		}
	}
	// A new file, and a declaration moved to one.
	changed := maps.Clone(files)
	changed["new.go"] = "package redact\n"
	if listing(changed) == before {
		t.Errorf("a new file is not seen")
	}
	d := decls["isMarker"]
	if d == nil {
		t.Fatal("no declaration isMarker")
	}
	changed = maps.Clone(files)
	changed[d.file] = files[d.file][:d.pos] + files[d.file][d.end:]
	changed["new.go"] = "package redact\n\n" + files[d.file][d.pos:d.end] + "\n"
	if listing(changed) == before {
		t.Errorf("isMarker moved to another file is not seen")
	}
	t.Logf("%d widenings seen; %d declarations held", n, len(decls))
}
