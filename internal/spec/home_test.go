package spec

import (
	"fmt"
	"go/ast"
	"go/token"
	"maps"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// A temporary home (EVL-K07, and that half of PRD-K14): the tests of a
// package that starts git clear the git configuration and identity the
// environment carries before any test runs, and a test that reads the home
// folder is in a package whose tests give it one of their own. Both are
// read from the source: a package whose tests start git only through
// another package's code, and a test that reads the home folder before its
// package's tests set it, are not told.

// literal is a string literal's value, or "" for any other expression.
func literal(x ast.Expr) (string, bool) {
	lit, ok := x.(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return "", false
	}
	s, err := strconv.Unquote(lit.Value)
	return s, err == nil
}

// startsGit says whether f starts git: it calls exec.Command with "git", or
// exec.CommandContext with a context and "git".
func startsGit(f goFile) bool {
	exec := f.imports()["os/exec"]
	found := false
	ast.Inspect(f.file, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok || found || exec == "" {
			return !found
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		if id, ok := sel.X.(*ast.Ident); !ok || id.Name != exec {
			return true
		}
		at := map[string]int{"Command": 0, "CommandContext": 1}
		if i, ok := at[sel.Sel.Name]; ok && len(call.Args) > i {
			if s, ok := literal(call.Args[i]); ok && s == "git" {
				found = true
			}
		}
		return !found
	})
	return found
}

// homeUse is how a test file uses HOME: sets it for itself or a child, or
// reads the home folder.
type homeUse struct{ sets, reads bool }

// homeOf reads how f uses HOME. It sets it with Setenv, as a key of a
// literal map or struct, or as HOME= in a child's environment; and reads
// it with os.Getenv or os.LookupEnv, os.UserHomeDir, user.Current or
// paths.Default.
func homeOf(f goFile) homeUse {
	var u homeUse
	byName := map[string]string{}
	for path, name := range f.imports() {
		byName[name] = path
	}
	ast.Inspect(f.file, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.BasicLit:
			if s, ok := literal(n); ok && strings.HasPrefix(s, "HOME=") {
				u.sets = true
			}
		case *ast.KeyValueExpr:
			if s, ok := literal(n.Key); ok && s == "HOME" {
				u.sets = true
			}
		case *ast.CallExpr:
			sel, ok := n.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			first := ""
			if len(n.Args) > 0 {
				first, _ = literal(n.Args[0])
			}
			pkg := ""
			if id, ok := sel.X.(*ast.Ident); ok {
				pkg = byName[id.Name]
			}
			switch {
			case sel.Sel.Name == "Setenv" && first == "HOME":
				u.sets = true
			case pkg == "os" && (sel.Sel.Name == "Getenv" || sel.Sel.Name == "LookupEnv") && first == "HOME",
				pkg == "os" && sel.Sel.Name == "UserHomeDir",
				pkg == "os/user" && sel.Sel.Name == "Current",
				pkg == module+"/internal/paths" && sel.Sel.Name == "Default":
				u.reads = true
			}
		}
		return true
	})
	return u
}

// clearsFirst says whether a TestMain, through the functions of the module
// it calls, calls testgitenv.Clear before it calls Run on its *testing.M:
// before any test runs.
func clearsFirst(index map[string]funcBody, main funcBody, param string) bool {
	cleared, ran := false, false
	seen := map[string]bool{}
	var walk func(fn funcBody, m string)
	walk = func(fn funcBody, m string) {
		byName := map[string]string{}
		for path, name := range fn.f.imports() {
			byName[name] = path
		}
		ast.Inspect(fn.body, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok || ran {
				return !ran
			}
			switch x := call.Fun.(type) {
			case *ast.SelectorExpr:
				id, ok := x.X.(*ast.Ident)
				switch {
				case !ok:
				case id.Name == m && x.Sel.Name == "Run" && byName[id.Name] == "":
					ran = true
				case byName[id.Name] == module+"/internal/testgitenv" && x.Sel.Name == "Clear":
					cleared = !ran
				}
			case *ast.Ident:
				key := fn.f.pkg + "." + x.Name
				if called, ok := index[key]; ok && !seen[key] {
					seen[key] = true
					// The *testing.M passed on keeps its role under the
					// callee's name for it.
					next := ""
					for i, arg := range call.Args {
						if id, ok := arg.(*ast.Ident); ok && id.Name == m {
							next = paramName(called, i)
						}
					}
					walk(called, next)
				}
			}
			return !ran
		})
	}
	walk(main, param)
	return cleared
}

// paramName is the name of a function's i-th parameter, or "".
func paramName(fn funcBody, i int) string {
	for _, decl := range fn.f.file.Decls {
		d, ok := decl.(*ast.FuncDecl)
		if !ok || d.Body != fn.body {
			continue
		}
		n := 0
		for _, field := range d.Type.Params.List {
			for _, name := range field.Names {
				if n == i {
					return name.Name
				}
				n++
			}
		}
	}
	return ""
}

// gitScope lists the packages whose tests start git, each with the first
// file that starts it: a test file of the package, or a file of its own
// program, which its tests run. A package whose tests start git only
// through another package's code is not told by the source alone, and is
// left out: twenty such packages clear nothing today.
func gitScope(files []goFile) map[string]string {
	scope := map[string]string{}
	for _, f := range files {
		if _, ok := scope[f.pkg]; !ok && startsGit(f) {
			scope[f.pkg] = f.path + " starts git"
		}
	}
	return scope
}

// unclearedGit are the packages whose tests reach a package that starts
// git, yet need not clear the environment's git configuration, each with
// its reason. A new one clears it in its TestMain, or is added here with
// its reason.
var unclearedGit = map[string]string{}

// checkHome holds the tests at root to a temporary home: every package
// whose tests start git has a TestMain that clears the environment's git
// configuration and identity before any test runs, or is in uncleared
// with its reason, and every test file that reads the home folder is in a
// package whose tests set HOME.
func checkHome(files []goFile, uncleared map[string]string) []string {
	var problems []string
	index := funcs(files)
	mains := map[string]bool{}
	sets, reads := map[string]bool{}, map[string]string{}
	for _, f := range files {
		if !f.test {
			continue
		}
		u := homeOf(f)
		sets[f.pkg] = sets[f.pkg] || u.sets
		if u.reads && reads[f.pkg] == "" {
			reads[f.pkg] = f.path
		}
		for _, decl := range f.file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Recv != nil || fn.Name.Name != "TestMain" || fn.Body == nil || len(fn.Type.Params.List) != 1 || len(fn.Type.Params.List[0].Names) != 1 {
				continue
			}
			mains[f.pkg] = mains[f.pkg] || clearsFirst(index, funcBody{f, fn.Body, fn.Type}, fn.Type.Params.List[0].Names[0].Name)
		}
	}
	scope := gitScope(files)
	for _, pkg := range slices.Sorted(maps.Keys(scope)) {
		_, listed := uncleared[pkg]
		switch {
		case mains[pkg] && listed:
			problems = append(problems, fmt.Sprintf("%s clears the git environment, yet is listed as needing no clearing", pkg))
		case !mains[pkg] && !listed:
			problems = append(problems, fmt.Sprintf("%s: %s, but no TestMain of it calls testgitenv.Clear before its tests run", pkg, scope[pkg]))
		}
	}
	for _, pkg := range slices.Sorted(maps.Keys(uncleared)) {
		if _, ok := scope[pkg]; !ok {
			problems = append(problems, fmt.Sprintf("%s is listed as needing no clearing, but its tests start no git", pkg))
		}
	}
	for _, pkg := range slices.Sorted(maps.Keys(reads)) {
		if !sets[pkg] {
			problems = append(problems, fmt.Sprintf("%s reads the home folder, but no test of %s sets HOME", reads[pkg], pkg))
		}
	}
	return problems
}

// The tests of every package that starts git clear the git configuration
// and identity the environment carries before any test runs, and every
// test that reads the home folder is in a package whose tests give it one
// of their own.
func TestTestsRunInATemporaryHome(t *testing.T) {
	tempHome(t)
	files := sources(t, repoRoot(t))
	for _, p := range checkHome(files, unclearedGit) {
		t.Error(p)
	}
	scope := gitScope(files)
	for _, pkg := range []string{"internal/hook", "internal/identity", "cmd/eva-md", "internal/wikigit"} {
		if scope[pkg] == "" {
			t.Errorf("%s reads as starting no git: the check read the wrong source", pkg)
		}
	}
}

// The check fails a package whose tests start git with no TestMain, with
// one that clears only after its tests have run, or with one that never
// clears; a listed package that clears, or starts no git; and a test that
// reads the home folder in a package whose tests never set HOME. A
// TestMain that clears through a function it calls, after a fake's early
// exit, passes, and so does a test that reads HOME another test file sets.
func TestATestOutsideATemporaryHomeIsFound(t *testing.T) {
	tempHome(t)
	const clears = "package a\n\nimport (\n\t\"os\"\n\t\"testing\"\n\n\t\"" + module + "/internal/testgitenv\"\n)\n\n" +
		"func TestMain(m *testing.M) {\n\tif os.Getenv(\"FAKE\") != \"\" {\n\t\tos.Exit(0)\n\t}\n\tos.Exit(run(m))\n}\n\n" +
		"func run(tests *testing.M) int {\n\ttestgitenv.Clear()\n\treturn tests.Run()\n}\n"
	base := map[string]string{
		"internal/a/a.go":         "package a\n\nimport \"os/exec\"\n\nfunc head() { _ = exec.Command(\"git\", \"rev-parse\").Run() }\n",
		"internal/a/main_test.go": clears,
		"internal/a/home_test.go": "package a\n\nimport (\n\t\"os\"\n\t\"testing\"\n)\n\nfunc TestHome(t *testing.T) { _ = os.Getenv(\"HOME\") }\n",
		"internal/a/set_test.go":  "package a\n\nimport \"testing\"\n\nfunc TestSet(t *testing.T) { t.Setenv(\"HOME\", t.TempDir()) }\n",
		"internal/b/b_test.go":    "package b\n\nimport (\n\t\"context\"\n\t\"os/exec\"\n\t\"testing\"\n)\n\nfunc TestB(t *testing.T) { _ = exec.CommandContext(context.Background(), \"ls\").Run() }\n",
	}
	for _, c := range []struct {
		name   string
		files  map[string]string
		listed map[string]string
		want   []string
	}{
		{"none", nil, nil, nil},
		{"git started by a test with no TestMain", map[string]string{
			"internal/b/b_test.go": "package b\n\nimport (\n\t\"context\"\n\t\"os/exec\"\n\t\"testing\"\n)\n\nfunc TestB(t *testing.T) { _ = exec.CommandContext(context.Background(), \"git\", \"init\").Run() }\n",
		}, nil, []string{`^internal/b: internal/b/b_test\.go starts git, but no TestMain of it calls testgitenv\.Clear before its tests run$`}},
		{"a TestMain that clears after the tests", map[string]string{
			"internal/a/main_test.go": strings.Replace(clears, "\ttestgitenv.Clear()\n\treturn tests.Run()\n", "\tcode := tests.Run()\n\ttestgitenv.Clear()\n\treturn code\n", 1),
		}, nil, []string{`^internal/a: internal/a/a\.go starts git, but no TestMain`}},
		{"a TestMain that never clears", map[string]string{
			"internal/a/main_test.go": "package a\n\nimport (\n\t\"os\"\n\t\"testing\"\n)\n\nfunc TestMain(m *testing.M) { os.Exit(m.Run()) }\n",
		}, nil, []string{`^internal/a: internal/a/a\.go starts git, but no TestMain`}},
		{"a listed package that clears", nil, map[string]string{"internal/a": "x"}, []string{`^internal/a clears the git environment, yet is listed as needing no clearing$`}},
		{"a listed package that starts no git", nil, map[string]string{"internal/b": "x"}, []string{`^internal/b is listed as needing no clearing, but its tests start no git$`}},
		{"a test that reads HOME none sets", map[string]string{
			"internal/b/home_test.go": "package b\n\nimport (\n\t\"os\"\n\t\"testing\"\n)\n\nfunc TestHome(t *testing.T) { _, _ = os.UserHomeDir() }\n",
		}, nil, []string{`^internal/b/home_test\.go reads the home folder, but no test of internal/b sets HOME$`}},
	} {
		t.Run(c.name, func(t *testing.T) {
			files := maps.Clone(base)
			maps.Copy(files, c.files)
			expectProblems(t, files, func(files []goFile) []string { return checkHome(files, c.listed) }, c.want)
		})
	}
}
