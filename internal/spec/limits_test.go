package spec

import (
	"cmp"
	"fmt"
	"go/ast"
	"go/token"
	"maps"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
)

// Time limits (SYS-K17, SET-K20, EVL-K08): every package-level variable of
// the program that is a time limit is raised before any test runs.

// durationUnits are the time package's units, whose use makes a value a
// time.Duration.
var durationUnits = []string{"Nanosecond", "Microsecond", "Millisecond", "Second", "Minute", "Hour"}

// limitWords, in a variable's name or its comment, make it a time limit:
// how long something may take or be waited for.
var limitWords = regexp.MustCompile(`(?i)limit|timeout|times out|wait|deadline|budget|bound`)

// durationVar is a package-level variable of the program whose type is
// time.Duration, with the value it has outside the tests.
type durationVar struct {
	pkg, name, where, doc string
	value                 ast.Expr
	f                     goFile
}

// key names a variable as the checks do, <package folder>.<name>.
func (v durationVar) key() string { return v.pkg + "." + v.name }

// isTimePkg says whether x names the time package in f.
func isTimePkg(f goFile, x ast.Expr) bool {
	id, ok := x.(*ast.Ident)
	return ok && f.imports()["time"] == id.Name
}

// timeName says whether x is the time package's name, given in f as
// time.<name> or, where f imports the package with a dot, as the name
// alone.
func timeName(f goFile, x ast.Expr, name string) bool {
	switch x := x.(type) {
	case *ast.SelectorExpr:
		return x.Sel.Name == name && isTimePkg(f, x.X)
	case *ast.Ident:
		return x.Name == name && f.imports()["time"] == "."
	}
	return false
}

// constDef is a package-level constant of the module and the file that
// holds it.
type constDef struct {
	value ast.Expr
	f     goFile
}

// source is what the checks on time read of the module: its constants and
// its functions, by <package folder>.<name>.
type source struct {
	consts map[string]constDef
	funcs  map[string]funcBody
}

// sourceOf indexes files' constants and functions.
func sourceOf(files []goFile) source {
	consts := map[string]constDef{}
	for _, f := range files {
		for _, decl := range f.file.Decls {
			gen, ok := decl.(*ast.GenDecl)
			if !ok || gen.Tok != token.CONST {
				continue
			}
			for _, sp := range gen.Specs {
				spec := sp.(*ast.ValueSpec)
				for i, name := range spec.Names {
					if i < len(spec.Values) {
						consts[f.pkg+"."+name.Name] = constDef{spec.Values[i], f}
					}
				}
			}
		}
	}
	return source{consts, funcs(files)}
}

// moduleName is what x names in f, as <package folder>.<name>, when it may
// be a name of the module: a name alone, of f's own package, or one of an
// imported package of the module; else "".
func moduleName(f goFile, x ast.Expr) string {
	switch x := x.(type) {
	case *ast.Ident:
		return f.pkg + "." + x.Name
	case *ast.SelectorExpr:
		id, ok := x.X.(*ast.Ident)
		if !ok {
			return ""
		}
		for path, name := range f.imports() {
			if pkg := packageOf(path); pkg != "" && name == id.Name {
				return pkg + "." + x.Sel.Name
			}
		}
	}
	return ""
}

// isDuration says whether a variable's declared type, or else its value,
// makes it a time.Duration: the type time.Duration, a conversion to it, an
// expression that uses one of the time package's units, a constant of the
// module whose value is one, or a call of a function of the module whose
// result is one. The time package may be imported under any name, or with
// a dot.
func isDuration(src source, f goFile, typ, value ast.Expr) bool {
	return isDurationAt(src, f, typ, value, 0)
}

func isDurationAt(src source, f goFile, typ, value ast.Expr, depth int) bool {
	if typ != nil {
		return timeName(f, typ, "Duration")
	}
	if value == nil || depth > 8 {
		return false
	}
	found := false
	ast.Inspect(value, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.FuncLit:
			return false
		case *ast.SelectorExpr, *ast.Ident:
			x := n.(ast.Expr)
			if timeName(f, x, "Duration") || slices.ContainsFunc(durationUnits, func(u string) bool { return timeName(f, x, u) }) {
				found = true
			} else if c, ok := src.consts[moduleName(f, x)]; ok && isDurationAt(src, c.f, nil, c.value, depth+1) {
				found = true
			}
			if _, ok := n.(*ast.SelectorExpr); ok {
				return false
			}
		case *ast.CallExpr:
			fn, ok := src.funcs[moduleName(f, n.Fun)]
			if ok && fn.typ.Results != nil && len(fn.typ.Results.List) == 1 && timeName(fn.f, fn.typ.Results.List[0].Type, "Duration") {
				found = true
			}
		}
		return !found
	})
	return found
}

// durationVars lists the package-level time.Duration variables of the
// program's files, with the comment above each or above its group.
func durationVars(files []goFile) []durationVar {
	var out []durationVar
	src := sourceOf(files)
	for _, f := range files {
		if f.test {
			continue
		}
		for _, decl := range f.file.Decls {
			gen, ok := decl.(*ast.GenDecl)
			if !ok || gen.Tok != token.VAR {
				continue
			}
			for _, s := range gen.Specs {
				spec := s.(*ast.ValueSpec)
				doc := gen.Doc.Text() + spec.Doc.Text() + spec.Comment.Text()
				for i, name := range spec.Names {
					var value ast.Expr
					if i < len(spec.Values) {
						value = spec.Values[i]
					}
					if name.Name != "_" && isDuration(src, f, spec.Type, value) {
						out = append(out, durationVar{pkg: f.pkg, name: name.Name, where: f.line(name), doc: doc, value: value, f: f})
					}
				}
			}
		}
	}
	return out
}

// funcBody is a function's body, its type and the file that holds it.
type funcBody struct {
	f    goFile
	body *ast.BlockStmt
	typ  *ast.FuncType
}

// funcs indexes the functions of the given files by <package folder>.<name>,
// methods and init left out.
func funcs(files []goFile) map[string]funcBody {
	out := map[string]funcBody{}
	for _, f := range files {
		for _, decl := range f.file.Decls {
			if fn, ok := decl.(*ast.FuncDecl); ok && fn.Recv == nil && fn.Body != nil && fn.Name.Name != "init" {
				out[f.pkg+"."+fn.Name.Name] = funcBody{f, fn.Body, fn.Type}
			}
		}
	}
	return out
}

// testingMs are the names of fn's parameters of type *testing.M.
func testingMs(fn funcBody) []string {
	var names []string
	pkg := fn.f.imports()["testing"]
	for _, field := range fn.typ.Params.List {
		star, ok := field.Type.(*ast.StarExpr)
		if !ok {
			continue
		}
		sel, ok := star.X.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != "M" {
			continue
		}
		if id, ok := sel.X.(*ast.Ident); ok && pkg != "" && id.Name == pkg {
			for _, name := range field.Names {
				names = append(names, name.Name)
			}
		}
	}
	return names
}

// raise is an assignment to a package-level variable of the module, by
// <package folder>.<name>, with the value it is given and where.
type raise struct {
	key   string
	value ast.Expr // nil when the value is not an expression of its own
	where string
	f     goFile
}

// raiseWalk is a walk of what a TestMain or an init of a test file runs
// before the package's tests do.
type raiseWalk struct {
	index  map[string]funcBody
	seen   map[string]bool
	ran    bool // Run was called on a *testing.M: the tests have run
	raises []raise
}

// walk adds what fn assigns with =, and what every function of the module
// it calls assigns, followed through its calls, until Run is called on a
// *testing.M it was given: what runs before any test. What a function
// literal, a defer or a go statement holds is left out: it runs later, if
// at all.
func (w *raiseWalk) walk(fn funcBody) {
	ms := testingMs(fn)
	ast.Inspect(fn.body, func(n ast.Node) bool {
		if w.ran {
			return false
		}
		switch n := n.(type) {
		case *ast.FuncLit, *ast.DeferStmt, *ast.GoStmt:
			return false
		case *ast.AssignStmt:
			if n.Tok != token.ASSIGN {
				return true
			}
			for i, lhs := range n.Lhs {
				if k := moduleName(fn.f, lhs); k != "" {
					var value ast.Expr
					if len(n.Rhs) == len(n.Lhs) {
						value = n.Rhs[i]
					}
					w.raises = append(w.raises, raise{k, value, fn.f.line(lhs), fn.f})
				}
			}
		case *ast.CallExpr:
			if sel, ok := n.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == "Run" && len(n.Args) == 0 {
				if id, ok := sel.X.(*ast.Ident); ok && slices.Contains(ms, id.Name) {
					w.ran = true
					return false
				}
			}
			if k := moduleName(fn.f, n.Fun); k != "" && !w.seen[k] {
				if called, ok := w.index[k]; ok {
					w.seen[k] = true
					w.walk(called)
				}
			}
		}
		return true
	})
}

// raisedVars gives, for each package, by its folder, what its tests set
// before any of them runs, by <package folder>.<name>: what its TestMain
// sets before it runs the tests, and what an init of its test files sets,
// through the functions they call, testlimits.Raise among them. The last
// value given stands.
func raisedVars(files []goFile) map[string]map[string]raise {
	index := funcs(files)
	raised := map[string]map[string]raise{}
	for _, f := range files {
		if !f.test {
			continue
		}
		for _, decl := range f.file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Recv != nil || fn.Body == nil || fn.Name.Name != "TestMain" && fn.Name.Name != "init" {
				continue
			}
			w := &raiseWalk{index: index, seen: map[string]bool{}}
			w.walk(funcBody{f, fn.Body, fn.Type})
			if raised[f.pkg] == nil {
				raised[f.pkg] = map[string]raise{}
			}
			for _, r := range w.raises {
				raised[f.pkg][r.key] = r
			}
		}
	}
	return raised
}

// unitNanos are the time package's units in nanoseconds.
var unitNanos = map[string]float64{"Nanosecond": 1, "Microsecond": 1e3, "Millisecond": 1e6, "Second": 1e9, "Minute": 60e9, "Hour": 3600e9}

// nanos evaluates x, in f, as a constant duration in nanoseconds: a
// number, a unit of the time package, a constant of the module, a
// conversion to time.Duration, and products, quotients, sums and
// differences of them. ok is false for anything else.
func nanos(src source, f goFile, x ast.Expr, depth int) (float64, bool) {
	if x == nil || depth > 8 {
		return 0, false
	}
	switch x := x.(type) {
	case *ast.BasicLit:
		if x.Kind != token.INT && x.Kind != token.FLOAT {
			return 0, false
		}
		n, err := strconv.ParseFloat(strings.ReplaceAll(x.Value, "_", ""), 64)
		return n, err == nil
	case *ast.ParenExpr:
		return nanos(src, f, x.X, depth+1)
	case *ast.CallExpr:
		if len(x.Args) == 1 && timeName(f, x.Fun, "Duration") {
			return nanos(src, f, x.Args[0], depth+1)
		}
	case *ast.BinaryExpr:
		a, okA := nanos(src, f, x.X, depth+1)
		b, okB := nanos(src, f, x.Y, depth+1)
		if !okA || !okB {
			return 0, false
		}
		switch x.Op {
		case token.MUL:
			return a * b, true
		case token.QUO:
			return a / b, b != 0
		case token.ADD:
			return a + b, true
		case token.SUB:
			return a - b, true
		}
	case *ast.Ident, *ast.SelectorExpr:
		for unit, n := range unitNanos {
			if timeName(f, x, unit) {
				return n, true
			}
		}
		if c, ok := src.consts[moduleName(f, x)]; ok {
			return nanos(src, c.f, c.value, depth+1)
		}
	}
	return 0, false
}

// unraised are the time limits of the program that no test raises, each
// with the reason it needs no raising. A new limit is raised, or added here
// with its reason.
var unraised = map[string]string{
	"internal/okfadapter.lockPoll": "how often a busy wiki lock is tried again, not how long it is waited for",
	"internal/resume.codeLeft": "the least time left before the brief's deadline for the code to be measured at all: " +
		"raising it would stop the measuring, and the raised deadlines leave far more",
	"internal/wikigit.wikiLockWait": "how long a commit waits for a page write to end: a test that holds the wiki lock " +
		"decides how long, and the test of the wait passes its own to wikiLockWithin",
}

// programPackages lists the folders of the program's packages: cmd/eva-md
// and every package of the module its files import, followed through the
// imports of their files that are not tests.
func programPackages(files []goFile) map[string]bool {
	imports := map[string][]string{}
	for _, f := range files {
		if f.test {
			continue
		}
		for path := range f.imports() {
			if pkg := packageOf(path); pkg != "" {
				imports[f.pkg] = append(imports[f.pkg], pkg)
			}
		}
	}
	seen := map[string]bool{"cmd/eva-md": true}
	queue := []string{"cmd/eva-md"}
	for len(queue) > 0 {
		pkg := queue[0]
		queue = queue[1:]
		for _, next := range imports[pkg] {
			if !seen[next] {
				seen[next] = true
				queue = append(queue, next)
			}
		}
	}
	return seen
}

// checkTimeLimits holds the source to its time limits: every package-level
// time.Duration variable of the program whose name or comment makes it a
// limit or a wait is raised by its own package's tests before any of them
// runs, to no less than it is outside them, or is in unraised; and every
// name in unraised is such a variable that is not raised. testlimits.Raise
// counts for a package whose TestMain calls it: the packages whose limits
// it raises cannot call it from their own tests without an import cycle,
// so they raise their own.
func checkTimeLimits(files []goFile, unraised map[string]string) []string {
	var problems []string
	program, raised, src := programPackages(files), raisedVars(files), sourceOf(files)
	limits := map[string]bool{}
	for _, v := range durationVars(files) {
		if !program[v.pkg] || !limitWords.MatchString(v.name+" "+v.doc) {
			continue
		}
		limits[v.key()] = true
		_, listed := unraised[v.key()]
		r, ok := raised[v.pkg][v.key()]
		switch {
		case ok && listed:
			problems = append(problems, fmt.Sprintf("%s: %s is raised, yet listed as needing no raising", v.where, v.key()))
		case !ok && !listed:
			problems = append(problems, fmt.Sprintf("%s: %s is a time limit the tests of %s do not raise before they run: "+
				"raise it in its package's TestMain, or list it with the reason it needs none", v.where, v.key(), v.pkg))
		case ok:
			test, okTest := nanos(src, r.f, r.value, 0)
			prod, okProd := nanos(src, v.f, v.value, 0)
			if okTest && okProd && test < prod {
				problems = append(problems, fmt.Sprintf("%s: %s is set to %s for the tests of %s, below the %s it is outside them",
					r.where, v.key(), time.Duration(test), v.pkg, time.Duration(prod)))
			}
		}
	}
	for _, k := range slices.Sorted(maps.Keys(unraised)) {
		if !limits[k] {
			problems = append(problems, fmt.Sprintf("%s is listed as a limit that needs no raising, but the program has no such limit", k))
		}
	}
	return problems
}

// Every time limit of the program is raised, to no less than it is
// outside the tests, by its own package's TestMain, or an init of its test
// files, through what they call, testlimits.Raise among them, before any
// of its tests runs, or is one of the few listed with the reason it needs
// no raising.
func TestEveryTimeLimitIsRaised(t *testing.T) {
	tempHome(t)
	files := sources(t, repoRoot(t))
	for _, p := range checkTimeLimits(files, unraised) {
		t.Error(p)
	}
	// The check read what it should: limits raised each way.
	raised := raisedVars(files)
	for _, k := range []string{"internal/identity.Timeout", "cmd/eva-md.setupGitTimeout", "internal/wikigit.PushTimeout", "internal/health.activityTimeout"} {
		pkg, _, _ := strings.Cut(k, ".")
		if _, ok := raised[pkg][k]; !ok {
			t.Errorf("%s reads as not raised", k)
		}
	}
}

// expectProblems runs check on a tree of files written into a folder of
// the test's own, and fails unless its problems, in order, match want.
func expectProblems(t *testing.T, files map[string]string, check func(files []goFile) []string, want []string) {
	t.Helper()
	dir := t.TempDir()
	writeTree(t, dir, files)
	got := check(sources(t, dir))
	if len(got) != len(want) {
		t.Fatalf("problems %q, want %d", got, len(want))
	}
	for i, w := range want {
		if !regexp.MustCompile(w).MatchString(got[i]) {
			t.Errorf("problem %q does not say %q", got[i], w)
		}
	}
}

// The check fails a limit its package's tests do not raise, whether its
// name or its comment makes it one, or a constant, a function or the time
// package under a dot gives its type; one testlimits raises but its own
// package's tests do not, one whose package's TestMain stops calling
// testlimits.Raise, one set lower for the tests than outside them, one
// raised only after the tests ran, in a function literal, a defer or a go
// statement; and a listed limit that is raised or gone. It passes one a
// TestMain raises through a function it calls, one an init of a test file
// raises, one raised through testlimits.Raise that an external test
// package's TestMain calls, a limit of a package the program does not
// link, and a duration that is no limit.
func TestALimitNoTestRaisesIsFound(t *testing.T) {
	tempHome(t)
	const mainTest = "package a\n\nimport (\n\t\"os\"\n\t\"testing\"\n\t\"time\"\n)\n\nfunc TestMain(m *testing.M) {\n\tTimeout = time.Hour\n\traise()\n\tos.Exit(m.Run())\n}\n"
	base := map[string]string{
		"cmd/eva-md/main.go":           "package main\n\nimport (\n\t_ \"" + module + "/internal/a\"\n\t_ \"" + module + "/internal/b\"\n)\n\nfunc main() {}\n",
		"internal/testlimits/raise.go": "package testlimits\n\nimport (\n\t\"time\"\n\n\t\"" + module + "/internal/b\"\n)\n\nfunc Raise() { b.Wait = time.Hour }\n",
		"internal/a/a.go":              "package a\n\nimport \"time\"\n\nvar Timeout = time.Second\n\n// slowCall bounds a call.\nvar slowCall time.Duration = 2 * time.Second\n\n// every is how often a pass runs.\nvar every = time.Hour\n\nvar quietWait = 3 * time.Second\n\nvar initWait = time.Second\n",
		"internal/a/init_test.go":      "package a\n\nimport \"time\"\n\nfunc init() { initWait = time.Hour }\n",
		"internal/a/main_test.go":      mainTest,
		"internal/a/raise_test.go":     "package a\n\nimport \"time\"\n\nfunc raise() { slowCall = time.Hour }\n",
		"internal/b/b.go":              "package b\n\nimport \"time\"\n\nvar Wait = time.Minute\n",
		"internal/b/main_test.go":      "package b_test\n\nimport (\n\t\"os\"\n\t\"testing\"\n\n\t\"" + module + "/internal/testlimits\"\n)\n\nfunc TestMain(m *testing.M) {\n\ttestlimits.Raise()\n\tos.Exit(m.Run())\n}\n",
		"internal/notlinked/n.go":      "package notlinked\n\nimport \"time\"\n\nvar runTimeout = time.Second\n",
	}
	listed := map[string]string{"internal/a.quietWait": "a made-up reason"}
	slowCall := []string{`^internal/a/a\.go:8: internal/a\.slowCall is a time limit the tests of internal/a do not raise before they run`}
	for _, c := range []struct {
		name   string
		files  map[string]string
		listed map[string]string
		want   []string
	}{
		{"none", nil, listed, nil},
		{"a new limit by its name", map[string]string{
			"internal/a/b.go": "package a\n\nimport \"time\"\n\nvar lockWait = 5 * time.Second\n",
		}, listed, []string{`^internal/a/b\.go:5: internal/a\.lockWait is a time limit the tests of internal/a do not raise`}},
		{"a new limit by its comment, in a group", map[string]string{
			"internal/a/b.go": "package a\n\nimport \"time\"\n\n// The deadline of a push and its retry.\nvar (\n\tpush  = time.Minute\n\tretry time.Duration\n)\n",
		}, listed, []string{`^internal/a/b\.go:7: internal/a\.push is a time limit`, `^internal/a/b\.go:8: internal/a\.retry is a time limit`}},
		{"a new limit whose value is a constant", map[string]string{
			"internal/a/c.go": "package a\n\nimport \"time\"\n\nconst defaultWait = 5 * time.Second\n\nvar lockWait = defaultWait\n",
		}, listed, []string{`^internal/a/c\.go:7: internal/a\.lockWait is a time limit`}},
		{"a new limit whose value is a call", map[string]string{
			"internal/a/c.go": "package a\n\nimport \"time\"\n\nfunc d() time.Duration { return time.Second }\n\nvar callTimeout = d()\n",
		}, listed, []string{`^internal/a/c\.go:7: internal/a\.callTimeout is a time limit`}},
		{"a new limit through a dot import", map[string]string{
			"internal/a/c.go": "package a\n\nimport . \"time\"\n\nvar pushTimeout = Minute\n",
		}, listed, []string{`^internal/a/c\.go:5: internal/a\.pushTimeout is a time limit`}},
		{"a TestMain that stops raising it", map[string]string{
			"internal/a/raise_test.go": "package a\n\nfunc raise() {}\n",
		}, listed, slowCall},
		{"an init of a test file that stops raising it", map[string]string{
			"internal/a/init_test.go": "package a\n",
		}, listed, []string{`^internal/a/a\.go:15: internal/a\.initWait is a time limit the tests of internal/a do not raise`}},
		{"the owning package that stops raising it, though testlimits does", map[string]string{
			"internal/testlimits/raise.go": "package testlimits\n\nimport (\n\t\"time\"\n\n\t\"" + module + "/internal/a\"\n\t\"" + module + "/internal/b\"\n)\n\nfunc Raise() { a.Timeout, b.Wait = time.Hour, time.Hour }\n",
			"internal/a/main_test.go":      strings.Replace(mainTest, "\tTimeout = time.Hour\n", "\t_ = time.Hour\n", 1),
		}, listed, []string{`^internal/a/a\.go:5: internal/a\.Timeout is a time limit the tests of internal/a do not raise`}},
		{"testlimits that stops raising it", map[string]string{
			"internal/testlimits/raise.go": "package testlimits\n\nfunc Raise() {}\n",
		}, listed, []string{`^internal/b/b\.go:5: internal/b\.Wait is a time limit the tests of internal/b do not raise`}},
		{"a TestMain that stops calling testlimits", map[string]string{
			"internal/b/main_test.go": "package b_test\n\nimport (\n\t\"os\"\n\t\"testing\"\n)\n\nfunc TestMain(m *testing.M) { os.Exit(m.Run()) }\n",
		}, listed, []string{`^internal/b/b\.go:5: internal/b\.Wait is a time limit the tests of internal/b do not raise`}},
		{"a limit set lower for the tests", map[string]string{
			"internal/a/raise_test.go": "package a\n\nimport \"time\"\n\nfunc raise() { slowCall = time.Nanosecond }\n",
		}, listed, []string{`^internal/a/raise_test\.go:5: internal/a\.slowCall is set to 1ns for the tests of internal/a, below the 2s it is outside them$`}},
		{"a limit set lower through a constant", map[string]string{
			"internal/a/raise_test.go": "package a\n\nimport \"time\"\n\nconst short = 3 * time.Millisecond\n\nfunc raise() { slowCall = short }\n",
		}, listed, []string{`^internal/a/raise_test\.go:7: internal/a\.slowCall is set to 3ms for the tests of internal/a, below the 2s it is outside them$`}},
		{"a raise after the tests ran", map[string]string{
			"internal/a/main_test.go": strings.Replace(mainTest, "\traise()\n\tos.Exit(m.Run())\n", "\tcode := m.Run()\n\traise()\n\tos.Exit(code)\n", 1),
		}, listed, slowCall},
		{"a raise in a function literal", map[string]string{
			"internal/a/main_test.go": strings.Replace(mainTest, "\traise()\n", "\t_ = func() { raise() }\n", 1),
		}, listed, slowCall},
		{"a raise in a defer", map[string]string{
			"internal/a/main_test.go": strings.Replace(mainTest, "\traise()\n", "\tdefer raise()\n", 1),
		}, listed, slowCall},
		{"a raise in a go statement", map[string]string{
			"internal/a/main_test.go": strings.Replace(mainTest, "\traise()\n", "\tgo raise()\n", 1),
		}, listed, slowCall},
		{"a listed limit that is raised", nil, map[string]string{"internal/a.quietWait": "x", "internal/a.Timeout": "x"}, []string{`^internal/a/a\.go:5: internal/a\.Timeout is raised, yet listed`}},
		{"a listed limit that is gone", nil, map[string]string{"internal/a.quietWait": "x", "internal/a.gone": "x"}, []string{`^internal/a\.gone is listed as a limit that needs no raising, but the program has no such limit$`}},
		{"an unlisted limit", nil, map[string]string{}, []string{`^internal/a/a\.go:13: internal/a\.quietWait is a time limit the tests of internal/a do not raise`}},
	} {
		t.Run(c.name, func(t *testing.T) {
			files := maps.Clone(base)
			maps.Copy(files, c.files)
			expectProblems(t, files, func(files []goFile) []string { return checkTimeLimits(files, c.listed) }, c.want)
		})
	}
}

// Sleeps and skips (EVL-K08, and the speed half of PRD-K14, CUP-K16 and
// SRC-K19): no test waits for time to pass to decide what it checks, or
// skips. The few calls of time.Sleep and of a test's Skip in the tests are
// counted for each package, each count with its reason; a new one fails
// the check until it is added here with its reason. A count may fall.
type pinned struct {
	n      int
	reason string
}

var (
	sleeps = map[string]pinned{
		"cmd/live-accuracy":                {1, "a fake host holds its answer for the time its case asks, so a run meets a slow host; nothing checks how long"},
		"internal/coderef":                 {1, "a loop that looks again for what must happen, under a hang guard"},
		"internal/derive/modelrun":         {2, "the fake CLI keeps a child alive until it is killed"},
		"internal/eval/continuation/v1arm": {2, "a fake that must be killed hangs until it is"},
		"internal/hook":                    {1, "a loop that looks again for the detached job's record, under a hang guard"},
		"internal/mcp":                     {1, "a loop that yields while a call is held"},
		"internal/okfadapter":              {1, "a loop that yields while a writer waits for the lock"},
		"internal/resume":                  {1, "a loop that looks again for what must happen, under a hang guard"},
		"internal/update":                  {3, "the fake CLI keeps a child alive until it is killed, and a loop yields while a save waits for the lock"},
	}
	skips = map[string]pinned{
		"internal/identity":   {4, "git missing from PATH, which the checks never allow"},
		"internal/okfadapter": {3, "root, which writes a folder whatever its mode"},
		"internal/redact":     {1, "a run that widens the oracle's bounds, asked for with a flag"},
		"internal/resume":     {4, "root, which reads a file whatever its mode"},
		"internal/systempage": {1, "the child process of a test, which runs only as that"},
		"internal/update":     {1, "the child process of a test, which runs only as that"},
		"internal/wikigit":    {1, "the child process of a test, which runs only as that"},
		"internal/wikiinit":   {1, "a file system that tells letter case apart, on which the case does not arise"},
	}
)

// skipNames are the methods that skip a test.
var skipNames = []string{"Skip", "Skipf", "SkipNow"}

// testHelpers lists the packages only tests link: those a test file
// imports, of another package, directly or through the imports of the
// files that are not tests, that no command of the module links.
func testHelpers(files []goFile) map[string]bool {
	imports := map[string][]string{}
	var mains, fromTests []string
	for _, f := range files {
		if !f.test && f.file.Name.Name == "main" {
			mains = append(mains, f.pkg)
		}
		for path := range f.imports() {
			switch pkg := packageOf(path); {
			case pkg == "" || pkg == f.pkg:
			case f.test:
				fromTests = append(fromTests, pkg)
			default:
				imports[f.pkg] = append(imports[f.pkg], pkg)
			}
		}
	}
	reach := func(start []string) map[string]bool {
		seen := map[string]bool{}
		for queue := start; len(queue) > 0; queue = queue[1:] {
			if pkg := queue[0]; !seen[pkg] {
				seen[pkg] = true
				queue = append(queue, imports[pkg]...)
			}
		}
		return seen
	}
	program, helpers := reach(mains), map[string]bool{}
	for pkg := range reach(fromTests) {
		if !program[pkg] {
			helpers[pkg] = true
		}
	}
	return helpers
}

// countCalls counts, for each package, the uses in its test files, and in
// the files of a package only tests link, of time.Sleep, called or taken
// as a value, through the time package under any name or a dot, and of a
// Skip method: Skipf and SkipNow called or taken as a value, and Skip
// called, as a field may be named Skip. It gives the first place of each,
// by "sleep <package>" and "skip <package>".
func countCalls(files []goFile) (sleep, skip map[string]int, where map[string]string) {
	sleep, skip, where = map[string]int{}, map[string]int{}, map[string]string{}
	helpers := testHelpers(files)
	for _, f := range files {
		if !f.test && !helpers[f.pkg] {
			continue
		}
		selected := map[*ast.Ident]bool{} // the names after a dot, read with their selector
		called := map[ast.Expr]bool{}     // what is called
		ast.Inspect(f.file, func(n ast.Node) bool {
			switch n := n.(type) {
			case *ast.CallExpr:
				called[n.Fun] = true
			case *ast.SelectorExpr:
				selected[n.Sel] = true
				switch {
				case timeName(f, n, "Sleep"):
					sleep[f.pkg]++
					where["sleep "+f.pkg] = cmp.Or(where["sleep "+f.pkg], f.line(n))
				case slices.Contains(skipNames, n.Sel.Name) && (called[n] || n.Sel.Name != "Skip"):
					skip[f.pkg]++
					where["skip "+f.pkg] = cmp.Or(where["skip "+f.pkg], f.line(n))
				}
			case *ast.Ident:
				if !selected[n] && timeName(f, n, "Sleep") {
					sleep[f.pkg]++
					where["sleep "+f.pkg] = cmp.Or(where["sleep "+f.pkg], f.line(n))
				}
			}
			return true
		})
	}
	return sleep, skip, where
}

// checkCalls holds the counts of the source's sleeps and skips to the
// tables: no package has more than its count.
func checkCalls(files []goFile, sleepTable, skipTable map[string]pinned) []string {
	var problems []string
	sleep, skip, where := countCalls(files)
	for _, c := range []struct {
		what  string
		got   map[string]int
		table map[string]pinned
	}{{"sleep", sleep, sleepTable}, {"skip", skip, skipTable}} {
		for _, pkg := range slices.Sorted(maps.Keys(c.got)) {
			if n := c.got[pkg]; n > c.table[pkg].n {
				problems = append(problems, fmt.Sprintf("%s: the tests of %s call %s %d times, more than the %d the table allows: "+
					"a test may not wait for time to pass, or skip, unless the table says why", where[c.what+" "+pkg], pkg, c.what, n, c.table[pkg].n))
			}
		}
	}
	return problems
}

// No package's tests sleep or skip more often than the table allows, each
// count with its reason.
func TestSleepsAndSkipsArePinned(t *testing.T) {
	tempHome(t)
	files := sources(t, repoRoot(t))
	for _, p := range checkCalls(files, sleeps, skips) {
		t.Error(p)
	}
	sleep, skip, _ := countCalls(files)
	if sleep["internal/hook"] == 0 || skip["internal/identity"] == 0 {
		t.Errorf("counted %d sleeps in internal/hook and %d skips in internal/identity: the count read the wrong source", sleep["internal/hook"], skip["internal/identity"])
	}
	for _, table := range []map[string]pinned{sleeps, skips} {
		for pkg, c := range table {
			if strings.TrimSpace(c.reason) == "" || c.n < 1 {
				t.Errorf("%s: a count of %d with the reason %q", pkg, c.n, c.reason)
			}
		}
	}
}

// The check fails a new sleep in a test, through the time package under
// another name or a dot too, or taken as a value, a new skip, called or
// taken as a value, a sleep in a package with none, and a sleep or a skip
// in a package only tests link; a time.Sleep in a string, a sleep of the
// program's own code, a field named Skip and a count that fell pass.
func TestANewSleepOrSkipIsFound(t *testing.T) {
	tempHome(t)
	table := map[string]pinned{"internal/a": {1, "a made-up reason"}}
	base := map[string]string{
		"internal/a/a.go":      "package a\n\nimport \"time\"\n\nfunc wait() { time.Sleep(time.Second) }\n",
		"internal/a/a_test.go": "package a\n\nimport (\n\t\"testing\"\n\t\"time\"\n)\n\nfunc TestA(t *testing.T) {\n\ttime.Sleep(time.Millisecond)\n\tt.Skip(\"root\")\n\t_ = \"time.Sleep(1)\"\n}\n",
	}
	for _, c := range []struct {
		name  string
		files map[string]string
		want  []string
	}{
		{"none", nil, nil},
		{"a count that fell", map[string]string{"internal/a/a_test.go": "package a\n"}, nil},
		{"a second sleep", map[string]string{
			"internal/a/b_test.go": "package a\n\nimport (\n\t\"testing\"\n\tclock \"time\"\n)\n\nfunc TestB(t *testing.T) { clock.Sleep(clock.Second) }\n",
		}, []string{`^internal/a/a_test\.go:9: the tests of internal/a call sleep 2 times, more than the 1 the table allows`}},
		{"a second skip", map[string]string{
			"internal/a/b_test.go": "package a\n\nimport \"testing\"\n\nfunc TestB(t *testing.T) { t.SkipNow() }\n",
		}, []string{`^internal/a/a_test\.go:10: the tests of internal/a call skip 2 times, more than the 1 the table allows`}},
		{"a sleep in a package with none", map[string]string{
			"internal/b/b_test.go": "package b\n\nimport (\n\t\"testing\"\n\t\"time\"\n)\n\nfunc TestB(t *testing.T) { time.Sleep(time.Millisecond) }\n",
		}, []string{`^internal/b/b_test\.go:8: the tests of internal/b call sleep 1 times, more than the 0 the table allows`}},
		{"a sleep through a dot import", map[string]string{
			"internal/b/b_test.go": "package b\n\nimport (\n\t\"testing\"\n\t. \"time\"\n)\n\nfunc TestB(t *testing.T) { Sleep(Second) }\n",
		}, []string{`^internal/b/b_test\.go:8: the tests of internal/b call sleep 1 times`}},
		{"a sleep taken as a value", map[string]string{
			"internal/b/b_test.go": "package b\n\nimport (\n\t\"testing\"\n\t\"time\"\n)\n\nfunc TestB(t *testing.T) {\n\tnap := time.Sleep\n\tnap(time.Second)\n}\n",
		}, []string{`^internal/b/b_test\.go:9: the tests of internal/b call sleep 1 times`}},
		{"a skip taken as a value", map[string]string{
			"internal/b/b_test.go": "package b\n\nimport \"testing\"\n\nfunc TestB(t *testing.T) {\n\tskip := t.SkipNow\n\tskip()\n}\n",
		}, []string{`^internal/b/b_test\.go:6: the tests of internal/b call skip 1 times`}},
		{"a sleep and a skip in a package only tests link", map[string]string{
			"cmd/eva-md/main.go":   "package main\n\nimport _ \"" + module + "/internal/a\"\n\nfunc main() {}\n",
			"internal/b/bt/h.go":   "package bt\n\nimport (\n\t\"testing\"\n\t\"time\"\n)\n\nfunc Wait(t *testing.T) {\n\ttime.Sleep(time.Second)\n\tt.Skip()\n}\n",
			"internal/b/b_test.go": "package b\n\nimport (\n\t\"testing\"\n\n\t\"" + module + "/internal/b/bt\"\n)\n\nfunc TestB(t *testing.T) { bt.Wait(t) }\n",
		}, []string{
			`^internal/b/bt/h\.go:9: the tests of internal/b/bt call sleep 1 times`,
			`^internal/b/bt/h\.go:10: the tests of internal/b/bt call skip 1 times`,
		}},
		{"a field named Skip", map[string]string{
			"internal/b/b_test.go": "package b\n\nimport \"testing\"\n\ntype cursor struct{ Skip *int }\n\nfunc TestB(t *testing.T) {\n\tvar c cursor\n\tc.Skip = nil\n\t_ = c.Skip == nil\n}\n",
		}, nil},
	} {
		t.Run(c.name, func(t *testing.T) {
			files := maps.Clone(base)
			maps.Copy(files, c.files)
			expectProblems(t, files, func(files []goFile) []string { return checkCalls(files, table, table) }, c.want)
		})
	}
}
