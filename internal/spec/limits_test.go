package spec

import (
	"cmp"
	"fmt"
	"go/ast"
	"maps"
	"regexp"
	"slices"
	"strings"
	"testing"
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
// time.Duration.
type durationVar struct {
	pkg, name, where, doc string
}

// key names a variable as the checks do, <package folder>.<name>.
func (v durationVar) key() string { return v.pkg + "." + v.name }

// isTimePkg says whether x names the time package in f.
func isTimePkg(f goFile, x ast.Expr) bool {
	id, ok := x.(*ast.Ident)
	return ok && f.imports()["time"] == id.Name
}

// isDuration says whether a variable's declared type, or else its value,
// makes it a time.Duration: the type time.Duration, a conversion to it, or
// an expression that uses one of the time package's units.
func isDuration(f goFile, typ, value ast.Expr) bool {
	if typ != nil {
		sel, ok := typ.(*ast.SelectorExpr)
		return ok && sel.Sel.Name == "Duration" && isTimePkg(f, sel.X)
	}
	found := false
	ast.Inspect(value, func(n ast.Node) bool {
		if sel, ok := n.(*ast.SelectorExpr); ok && isTimePkg(f, sel.X) && (sel.Sel.Name == "Duration" || slices.Contains(durationUnits, sel.Sel.Name)) {
			found = true
		}
		if _, ok := n.(*ast.FuncLit); ok {
			return false
		}
		return !found
	})
	return found
}

// durationVars lists the package-level time.Duration variables of the
// program's files, with the comment above each or above its group.
func durationVars(files []goFile) []durationVar {
	var out []durationVar
	for _, f := range files {
		if f.test {
			continue
		}
		for _, decl := range f.file.Decls {
			gen, ok := decl.(*ast.GenDecl)
			if !ok || gen.Tok.String() != "var" {
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
					if name.Name != "_" && isDuration(f, spec.Type, value) {
						out = append(out, durationVar{pkg: f.pkg, name: name.Name, where: f.line(name), doc: doc})
					}
				}
			}
		}
	}
	return out
}

// funcBody is a function's body and the file that holds it.
type funcBody struct {
	f    goFile
	body *ast.BlockStmt
}

// funcs indexes the functions of the given files by <package folder>.<name>,
// methods and init left out.
func funcs(files []goFile) map[string]funcBody {
	out := map[string]funcBody{}
	for _, f := range files {
		for _, decl := range f.file.Decls {
			if fn, ok := decl.(*ast.FuncDecl); ok && fn.Recv == nil && fn.Body != nil && fn.Name.Name != "init" {
				out[f.pkg+"."+fn.Name.Name] = funcBody{f, fn.Body}
			}
		}
	}
	return out
}

// assigned lists, as <package folder>.<name>, what fn assigns with =, and
// what every function of the module it calls assigns, followed through its
// calls: a name alone as its own package's, and <import>.<name> as the
// imported package's.
func assigned(index map[string]funcBody, fn funcBody, seen map[string]bool) []string {
	byName := map[string]string{}
	for path, name := range fn.f.imports() {
		if pkg := packageOf(path); pkg != "" {
			byName[name] = pkg
		}
	}
	// target names what x is, in the module, or "" for anything else.
	target := func(x ast.Expr) string {
		switch x := x.(type) {
		case *ast.Ident:
			return fn.f.pkg + "." + x.Name
		case *ast.SelectorExpr:
			if id, ok := x.X.(*ast.Ident); ok && byName[id.Name] != "" {
				return byName[id.Name] + "." + x.Sel.Name
			}
		}
		return ""
	}
	var out []string
	ast.Inspect(fn.body, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.AssignStmt:
			if n.Tok.String() == "=" {
				for _, lhs := range n.Lhs {
					if k := target(lhs); k != "" {
						out = append(out, k)
					}
				}
			}
		case *ast.CallExpr:
			if k := target(n.Fun); k != "" && !seen[k] {
				if called, ok := index[k]; ok {
					seen[k] = true
					out = append(out, assigned(index, called, seen)...)
				}
			}
		}
		return true
	})
	return out
}

// raisedVars lists, as <package folder>.<name>, what tests raise before
// any test runs: what testlimits.Raise sets, and what each package's
// TestMain, or an init of its test files, sets of its own package's
// variables, through the functions they call.
func raisedVars(files []goFile) map[string]bool {
	index := funcs(files)
	raised := map[string]bool{}
	if fn, ok := index["internal/testlimits.Raise"]; ok {
		for _, k := range assigned(index, fn, map[string]bool{}) {
			raised[k] = true
		}
	}
	for _, f := range files {
		if !f.test {
			continue
		}
		for _, decl := range f.file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Recv != nil || fn.Body == nil || fn.Name.Name != "TestMain" && fn.Name.Name != "init" {
				continue
			}
			for _, k := range assigned(index, funcBody{f, fn.Body}, map[string]bool{}) {
				if strings.HasPrefix(k, f.pkg+".") {
					raised[k] = true
				}
			}
		}
	}
	return raised
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
// limit or a wait is raised before any test runs, or is in unraised, and
// every name in unraised is such a variable that is not raised.
func checkTimeLimits(files []goFile, unraised map[string]string) []string {
	var problems []string
	program, raised := programPackages(files), raisedVars(files)
	limits := map[string]bool{}
	for _, v := range durationVars(files) {
		if !program[v.pkg] || !limitWords.MatchString(v.name+" "+v.doc) {
			continue
		}
		limits[v.key()] = true
		_, listed := unraised[v.key()]
		switch {
		case raised[v.key()] && listed:
			problems = append(problems, fmt.Sprintf("%s: %s is raised, yet listed as needing no raising", v.where, v.key()))
		case !raised[v.key()] && !listed:
			problems = append(problems, fmt.Sprintf("%s: %s is a time limit no test raises: raise it in testlimits.Raise or its package's TestMain, or list it with the reason it needs none", v.where, v.key()))
		}
	}
	for _, k := range slices.Sorted(maps.Keys(unraised)) {
		if !limits[k] {
			problems = append(problems, fmt.Sprintf("%s is listed as a limit that needs no raising, but the program has no such limit", k))
		}
	}
	return problems
}

// Every time limit of the program is raised by testlimits or its own
// package's TestMain, through what they call, before any test runs, or is
// one of the few listed with the reason it needs no raising.
func TestEveryTimeLimitIsRaised(t *testing.T) {
	tempHome(t)
	files := sources(t, repoRoot(t))
	for _, p := range checkTimeLimits(files, unraised) {
		t.Error(p)
	}
	// The check read what it should: limits raised each way.
	raised := raisedVars(files)
	for _, k := range []string{"internal/identity.Timeout", "cmd/eva-md.setupGitTimeout", "internal/wikigit.PushTimeout", "internal/health.activityTimeout"} {
		if !raised[k] {
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

// The check fails a limit no test raises, whether its name or its comment
// makes it one, and a listed limit that is raised or gone; it passes one
// that testlimits raises, one a TestMain raises through a function it
// calls, one an init of a test file raises, a limit of a package the
// program does not link, and a duration that is no limit.
func TestALimitNoTestRaisesIsFound(t *testing.T) {
	tempHome(t)
	base := map[string]string{
		"cmd/eva-md/main.go":           "package main\n\nimport _ \"" + module + "/internal/a\"\n\nfunc main() {}\n",
		"internal/testlimits/raise.go": "package testlimits\n\nimport (\n\t\"time\"\n\n\t\"" + module + "/internal/a\"\n)\n\nfunc Raise() { a.Timeout = time.Hour }\n",
		"internal/a/a.go":              "package a\n\nimport \"time\"\n\nvar Timeout = time.Second\n\n// slowCall bounds a call.\nvar slowCall time.Duration = 2 * time.Second\n\n// every is how often a pass runs.\nvar every = time.Hour\n\nvar quietWait = 3 * time.Second\n\nvar initWait = time.Second\n",
		"internal/a/init_test.go":      "package a\n\nimport \"time\"\n\nfunc init() { initWait = time.Hour }\n",
		"internal/a/main_test.go":      "package a\n\nimport (\n\t\"os\"\n\t\"testing\"\n)\n\nfunc TestMain(m *testing.M) {\n\traise()\n\tos.Exit(m.Run())\n}\n",
		"internal/a/raise_test.go":     "package a\n\nimport \"time\"\n\nfunc raise() { slowCall = time.Hour }\n",
		"internal/notlinked/n.go":      "package notlinked\n\nimport \"time\"\n\nvar runTimeout = time.Second\n",
	}
	listed := map[string]string{"internal/a.quietWait": "a made-up reason"}
	for _, c := range []struct {
		name   string
		files  map[string]string
		listed map[string]string
		want   []string
	}{
		{"none", nil, listed, nil},
		{"a new limit by its name", map[string]string{
			"internal/a/b.go": "package a\n\nimport \"time\"\n\nvar lockWait = 5 * time.Second\n",
		}, listed, []string{`^internal/a/b\.go:5: internal/a\.lockWait is a time limit no test raises`}},
		{"a new limit by its comment, in a group", map[string]string{
			"internal/a/b.go": "package a\n\nimport \"time\"\n\n// The deadline of a push and its retry.\nvar (\n\tpush  = time.Minute\n\tretry time.Duration\n)\n",
		}, listed, []string{`^internal/a/b\.go:7: internal/a\.push is a time limit`, `^internal/a/b\.go:8: internal/a\.retry is a time limit`}},
		{"a TestMain that stops raising it", map[string]string{
			"internal/a/raise_test.go": "package a\n\nfunc raise() {}\n",
		}, listed, []string{`^internal/a/a\.go:8: internal/a\.slowCall is a time limit no test raises`}},
		{"an init of a test file that stops raising it", map[string]string{
			"internal/a/init_test.go": "package a\n",
		}, listed, []string{`^internal/a/a\.go:15: internal/a\.initWait is a time limit no test raises`}},
		{"testlimits that stops raising it", map[string]string{
			"internal/testlimits/raise.go": "package testlimits\n\nfunc Raise() {}\n",
		}, listed, []string{`^internal/a/a\.go:5: internal/a\.Timeout is a time limit no test raises`}},
		{"a listed limit that is raised", nil, map[string]string{"internal/a.quietWait": "x", "internal/a.Timeout": "x"}, []string{`^internal/a/a\.go:5: internal/a\.Timeout is raised, yet listed`}},
		{"a listed limit that is gone", nil, map[string]string{"internal/a.quietWait": "x", "internal/a.gone": "x"}, []string{`^internal/a\.gone is listed as a limit that needs no raising, but the program has no such limit$`}},
		{"an unlisted limit", nil, map[string]string{}, []string{`^internal/a/a\.go:13: internal/a\.quietWait is a time limit no test raises`}},
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

// countCalls counts, for each package, the calls in its test files of
// time.Sleep and of a Skip method, and the first place of each, by
// "sleep <package>" and "skip <package>".
func countCalls(files []goFile) (sleep, skip map[string]int, where map[string]string) {
	sleep, skip, where = map[string]int{}, map[string]int{}, map[string]string{}
	for _, f := range files {
		if !f.test {
			continue
		}
		ast.Inspect(f.file, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			switch {
			case !ok:
			case sel.Sel.Name == "Sleep" && isTimePkg(f, sel.X):
				sleep[f.pkg]++
				where["sleep "+f.pkg] = cmp.Or(where["sleep "+f.pkg], f.line(call))
			case slices.Contains(skipNames, sel.Sel.Name):
				skip[f.pkg]++
				where["skip "+f.pkg] = cmp.Or(where["skip "+f.pkg], f.line(call))
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
// another name too, a new skip, and a sleep in a package with none; a
// time.Sleep in a string, a sleep of the program's own code, and a count
// that fell pass.
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
	} {
		t.Run(c.name, func(t *testing.T) {
			files := maps.Clone(base)
			maps.Copy(files, c.files)
			expectProblems(t, files, func(files []goFile) []string { return checkCalls(files, table, table) }, c.want)
		})
	}
}
