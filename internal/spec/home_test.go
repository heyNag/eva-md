package spec

import (
	"fmt"
	"go/ast"
	"go/token"
	"maps"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// A temporary home (EVL-K07, and that half of PRD-K14): the tests of a
// package that starts git, or links code that does, clear the git
// configuration and identity the environment carries before any test runs;
// a test that reads the home folder is in a package whose tests give it one
// of their own; and no test names the hosts' or eva-md's folders under the
// real home, as ~, $HOME or a home written out, or builds them from the
// home before its test set HOME. All are read from the source, so what it
// cannot tell is not held: a read of the home other than of those folders
// before the test set HOME, a path built through a function of another
// package, git started by a program found other than by the name git, and
// a function reached other than by a call of its name.

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
// exec.CommandContext with a context and "git", given as a string or a
// constant of the module.
func startsGit(src source, f goFile) bool {
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
			in, arg := f, call.Args[i]
			for depth := 0; depth < 8; depth++ {
				c, ok := src.consts[moduleName(in, arg)]
				if !ok {
					break
				}
				in, arg = c.f, c.value
			}
			if s, ok := literal(arg); ok && s == "git" {
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
			switch n.(type) {
			case *ast.FuncLit, *ast.DeferStmt, *ast.GoStmt:
				// What these hold runs later, if at all.
				return false
			}
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

// gitScope lists the packages whose tests start git, each with how: a
// test file of the package starts it, or its tests link a package, its
// own or one the tests or their packages import, a file of which starts
// it, which the tests may run.
func gitScope(files []goFile) map[string]string {
	src := sourceOf(files)
	starter := map[string]string{} // a file of each package, not a test, that starts git
	imports, tested := map[string][]string{}, map[string][]string{}
	scope := map[string]string{}
	for _, f := range files {
		if startsGit(src, f) {
			switch {
			case f.test && scope[f.pkg] == "":
				scope[f.pkg] = f.path + " starts git"
			case !f.test && starter[f.pkg] == "":
				starter[f.pkg] = f.path
			}
		}
		for path := range f.imports() {
			if pkg := packageOf(path); pkg != "" && pkg != f.pkg {
				if f.test {
					tested[f.pkg] = append(tested[f.pkg], pkg)
				} else {
					imports[f.pkg] = append(imports[f.pkg], pkg)
				}
			}
		}
		if f.test {
			tested[f.pkg] = append(tested[f.pkg], f.pkg)
		}
	}
	for _, pkg := range slices.Sorted(maps.Keys(tested)) {
		if scope[pkg] != "" {
			continue
		}
		seen := map[string]bool{}
		var linked []string
		for queue := slices.Clone(tested[pkg]); len(queue) > 0; queue = queue[1:] {
			if next := queue[0]; !seen[next] {
				seen[next] = true
				if starter[next] != "" {
					linked = append(linked, next)
				}
				queue = append(queue, imports[next]...)
			}
		}
		if len(linked) > 0 {
			slices.Sort(linked)
			scope[pkg] = "its tests link " + starter[linked[0]] + ", which starts git"
		}
	}
	return scope
}

// unclearedGit are the packages whose tests reach a package that starts
// git, yet do not clear the environment's git configuration first, each
// with its reason. A new one clears it in its TestMain, or is added here
// with its reason.
var unclearedGit = map[string]string{
	"cmd/live-accuracy":                         throughOthers,
	"internal/config":                           throughOthers,
	"internal/eval/continuation/drive/fakehost": throughOthers,
	"internal/eval/continuation/report":         throughOthers,
	"internal/eval/families":                    throughOthers,
	"internal/eval/pilot/task":                  throughOthers,
	"internal/eval/retrieval":                   throughOthers,
	"internal/eval/retrieval/retrievaltest":     throughOthers,
	"internal/eval/scenario":                    throughOthers,
	"internal/eval/score":                       throughOthers,
	"internal/eval/spike":                       throughOthers,
	"internal/eval/spike/arm":                   throughOthers,
	"internal/eval/v1page":                      throughOthers,
	"internal/ledger/audit":                     throughOthers,
	"internal/ledger/inboxv1":                   throughOthers,
	"internal/ledger/registry":                  throughOthers,
	"internal/threadpage":                       throughOthers,
	"internal/update/accuracy/live":             throughOthers,
	"internal/work":                             throughOthers,
}

// throughOthers is why the packages of unclearedGit do not clear yet: no
// test of theirs starts git itself, but they link code that does, the
// program's or the evaluation's, which their tests may run. A TestMain of
// each that clears would change tests EV-10c may not change, so it is left
// to a later card.
const throughOthers = "no test of it starts git itself, but it links code that does; a TestMain that clears is a later card's, as EV-10c changes no existing test"

// realHomeNames are the folders under the real home the tests must never
// reach: the hosts' and eva-md's own.
var realHomeNames = []string{".claude", ".codex", ".config/eva-md", ".local/share/eva-md"}

// namesRealHome says whether rest, a path from a home folder with
// slashes, is one of realHomeNames or under one.
func namesRealHome(rest string) bool {
	rest = strings.TrimPrefix(rest, "/")
	return slices.ContainsFunc(realHomeNames, func(n string) bool { return rest == n || strings.HasPrefix(rest, n+"/") })
}

// literalHome is a string that names a folder of realHomeNames through
// the home's own name, ~ or $HOME, which a shell, os.ExpandEnv or the
// code given it turns into the real home.
var literalHome = regexp.MustCompile(`(?:^|[^A-Za-z0-9_.~-])(?:~|\$HOME|\$\{HOME\})(/[^\s"'\x60]*)`)

// literalRoot is a string that names a folder of realHomeNames under an
// absolute path: a home written out, which the tests' own home is not.
var literalRoot = regexp.MustCompile(`(?:^|[\s"'=:])(/[^\n]*?)/(\.claude|\.codex|\.config/eva-md|\.local/share/eva-md)(?:/|$|[\s"'])`)

// realHomeLiteral says what of realHomeNames a string literal names under
// a home it does not make, or "".
func realHomeLiteral(s string) string {
	for _, line := range strings.Split(s, "\n") {
		if m := literalHome.FindStringSubmatch(line); m != nil {
			for i := 0; i < len(m[1]); i++ {
				if m[1][i] == '/' && namesRealHome(m[1][i:]) {
					return "~" + m[1][i:]
				}
			}
		}
		if m := literalRoot.FindStringSubmatch(line); m != nil {
			return m[1] + "/" + m[2]
		}
	}
	return ""
}

// homeRead says whether x reads the home folder, in a file whose imports
// are byName: os.Getenv or os.LookupEnv of HOME, os.UserHomeDir, or the
// HomeDir of user.Current.
func homeRead(byName map[string]string, x ast.Expr) bool {
	switch x := x.(type) {
	case *ast.CallExpr:
		sel, ok := x.Fun.(*ast.SelectorExpr)
		if !ok {
			return false
		}
		pkg := ""
		if id, ok := sel.X.(*ast.Ident); ok {
			pkg = byName[id.Name]
		}
		first := ""
		if len(x.Args) > 0 {
			first, _ = literal(x.Args[0])
		}
		return pkg == "os" && (sel.Sel.Name == "Getenv" || sel.Sel.Name == "LookupEnv") && first == "HOME" ||
			pkg == "os" && sel.Sel.Name == "UserHomeDir" ||
			pkg == "os/user" && sel.Sel.Name == "Current"
	case *ast.SelectorExpr:
		return x.Sel.Name == "HomeDir" && homeRead(byName, x.X)
	}
	return false
}

// pathFrom gives what path x builds after the home it starts from, with
// slashes, and whether it starts from one: a home read itself, a name
// given one in the same function, or filepath.Join, path.Join or + of
// those with string literals.
func pathFrom(byName map[string]string, fromHome map[string]bool, x ast.Expr) (string, bool) {
	switch x := x.(type) {
	case *ast.Ident:
		return "", fromHome[x.Name]
	case *ast.ParenExpr:
		return pathFrom(byName, fromHome, x.X)
	case *ast.BinaryExpr:
		if x.Op != token.ADD {
			return "", false
		}
		rest, ok := pathFrom(byName, fromHome, x.X)
		s, lit := literal(x.Y)
		return rest + s, ok && lit
	case *ast.CallExpr:
		if homeRead(byName, x) {
			return "", true
		}
		sel, ok := x.Fun.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != "Join" || len(x.Args) == 0 {
			return "", false
		}
		if id, ok := sel.X.(*ast.Ident); !ok || byName[id.Name] != "path/filepath" && byName[id.Name] != "path" {
			return "", false
		}
		rest, ok := pathFrom(byName, fromHome, x.Args[0])
		for _, arg := range x.Args[1:] {
			s, lit := literal(arg)
			if !lit {
				break
			}
			rest += "/" + s
		}
		return rest, ok
	case *ast.SelectorExpr:
		return "", homeRead(byName, x)
	}
	return "", false
}

// setsHomeIn says whether fn sets HOME, with Setenv, directly or through
// a function of the module it calls, outside a function literal, a defer
// or a go statement, before pos when pos is valid.
func setsHomeIn(index map[string]funcBody, fn funcBody, pos token.Pos, seen map[string]bool) bool {
	return setsHomeWithin(index, fn, pos, false, seen)
}

// setsHomeWithin is setsHomeIn, with the function literals fn holds read
// too when intoLits is set.
func setsHomeWithin(index map[string]funcBody, fn funcBody, pos token.Pos, intoLits bool, seen map[string]bool) bool {
	found := false
	ast.Inspect(fn.body, func(n ast.Node) bool {
		if found || n == nil || pos.IsValid() && n.Pos() >= pos {
			return false
		}
		switch n := n.(type) {
		case *ast.FuncLit:
			return intoLits
		case *ast.DeferStmt, *ast.GoStmt:
			return false
		case *ast.CallExpr:
			sel, ok := n.Fun.(*ast.SelectorExpr)
			if ok && sel.Sel.Name == "Setenv" && len(n.Args) > 0 {
				if s, _ := literal(n.Args[0]); s == "HOME" {
					found = true
					return false
				}
			}
			if k := moduleName(fn.f, n.Fun); k != "" && !seen[k] {
				if called, ok := index[k]; ok && called.f.test {
					seen[k] = true
					found = setsHomeIn(index, called, token.NoPos, seen)
				}
			}
		}
		return !found
	})
	return found
}

// setBefore says whether HOME is set before pos in fn: by the statements of
// the innermost function literal around pos before it, or else anywhere
// in another function literal around it or in fn, the literals they hold
// included, as a function literal runs when it is called, such as a
// table's after the subtest that calls it set HOME.
func setBefore(index map[string]funcBody, fn funcBody, pos token.Pos) bool {
	var lits []*ast.FuncLit // around pos, the outermost first
	ast.Inspect(fn.body, func(n ast.Node) bool {
		if lit, ok := n.(*ast.FuncLit); ok && lit.Pos() <= pos && pos < lit.End() {
			lits = append(lits, lit)
		}
		return true
	})
	if len(lits) == 0 {
		return setsHomeIn(index, fn, pos, map[string]bool{})
	}
	inner := lits[len(lits)-1]
	if setsHomeIn(index, funcBody{fn.f, inner.Body, inner.Type}, pos, map[string]bool{}) {
		return true
	}
	for _, lit := range lits[:len(lits)-1] {
		if setsHomeWithin(index, funcBody{fn.f, lit.Body, lit.Type}, token.NoPos, true, map[string]bool{}) {
			return true
		}
	}
	return setsHomeWithin(index, fn, token.NoPos, true, map[string]bool{})
}

// homeFirst says whether HOME is the test's own when fn runs: its package's
// TestMain sets it before the tests run, or fn is no test and every call of
// it among the package's test files comes after its caller set HOME, or
// in a caller of which the same holds.
func homeFirst(index map[string]funcBody, calls map[string][]call, mains map[string]bool, fn funcBody, name string, depth int) bool {
	if mains[fn.f.pkg] {
		return true
	}
	if depth > 6 || strings.HasPrefix(name, "Test") || len(calls[fn.f.pkg+"."+name]) == 0 {
		return false
	}
	for _, c := range calls[fn.f.pkg+"."+name] {
		if !setBefore(index, c.in, c.at) && !homeFirst(index, calls, mains, c.in, c.name, depth+1) {
			return false
		}
	}
	return true
}

// call is a call of a function of a package's tests: the function it is
// in, by name, and where.
type call struct {
	in   funcBody
	name string
	at   token.Pos
}

// realHomePaths lists the places of f that name a folder of
// realHomeNames under the real home: a string literal that names one
// through ~, $HOME or a home written out, and a path built from a read of
// the home folder before the test set HOME, in a function whose package's
// tests do not set it first.
func realHomePaths(index map[string]funcBody, calls map[string][]call, mains map[string]bool, f goFile) []string {
	var out []string
	byName := map[string]string{}
	for path, name := range f.imports() {
		byName[name] = path
	}
	for _, decl := range f.file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Body == nil {
			continue
		}
		body := funcBody{f, fn.Body, fn.Type}
		fromHome := map[string]bool{}
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			switch n := n.(type) {
			case *ast.AssignStmt:
				for i, lhs := range n.Lhs {
					id, ok := lhs.(*ast.Ident)
					if !ok {
						continue
					}
					switch {
					case len(n.Rhs) == len(n.Lhs):
						_, home := pathFrom(byName, fromHome, n.Rhs[i])
						if rest, _ := pathFrom(byName, fromHome, n.Rhs[i]); home && rest == "" {
							fromHome[id.Name] = true
						}
					case i == 0 && len(n.Rhs) == 1:
						fromHome[id.Name] = homeRead(byName, n.Rhs[0])
					}
				}
			case *ast.CallExpr, *ast.BinaryExpr:
				rest, home := pathFrom(byName, fromHome, n.(ast.Expr))
				if !home || !namesRealHome(rest) {
					return true
				}
				if !setBefore(index, body, n.Pos()) && !homeFirst(index, calls, mains, body, fn.Name.Name, 0) {
					out = append(out, fmt.Sprintf("%s builds ~%s from the real home, before its test set HOME", f.line(n), rest))
				}
				return false
			}
			return true
		})
	}
	ast.Inspect(f.file, func(n ast.Node) bool {
		if lit, ok := n.(*ast.BasicLit); ok {
			if s, ok := literal(lit); ok {
				if p := realHomeLiteral(s); p != "" {
					out = append(out, fmt.Sprintf("%s names %s, a folder of a home the test did not make", f.line(lit), p))
				}
			}
		}
		return true
	})
	return out
}

// callsOf indexes the calls of the functions of each package's test files
// by <package folder>.<name>: where each is called, among those files.
func callsOf(files []goFile) map[string][]call {
	out := map[string][]call{}
	for _, f := range files {
		if !f.test {
			continue
		}
		for _, decl := range f.file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil || fn.Recv != nil {
				continue
			}
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				if c, ok := n.(*ast.CallExpr); ok {
					if id, ok := c.Fun.(*ast.Ident); ok {
						k := f.pkg + "." + id.Name
						out[k] = append(out[k], call{funcBody{f, fn.Body, fn.Type}, fn.Name.Name, c.Pos()})
					}
				}
				return true
			})
		}
	}
	return out
}

// checkHome holds the tests at root to a temporary home: every package
// whose tests start git, or link code that does, has a TestMain that
// clears the environment's git configuration and identity before any test
// runs, or is in uncleared with its reason; every test file that reads the
// home folder is in a package whose tests set HOME; and no test file names
// a folder of realHomeNames under a home it did not make, or is in madeUp
// with its reason.
func checkHome(files []goFile, uncleared, madeUp map[string]string) []string {
	var problems []string
	index := funcs(files)
	mains, homeMains := map[string]bool{}, map[string]bool{}
	sets, reads := map[string]bool{}, map[string]string{}
	for _, f := range files {
		if !f.test {
			continue
		}
		for _, decl := range f.file.Decls {
			if fn, ok := decl.(*ast.FuncDecl); ok && fn.Recv == nil && fn.Name.Name == "TestMain" && fn.Body != nil {
				homeMains[f.pkg] = homeMains[f.pkg] || setsHomeIn(index, funcBody{f, fn.Body, fn.Type}, token.NoPos, map[string]bool{})
			}
		}
	}
	calls := callsOf(files)
	used := map[string]bool{}
	for _, f := range files {
		if !f.test {
			continue
		}
		for _, p := range realHomePaths(index, calls, homeMains, f) {
			// A file listed as a child's builds its paths from the home it
			// is given; any other names a home as text.
			if reason, listed := madeUp[f.path]; listed && (reason == childHome) == strings.Contains(p, " from the real home") {
				used[f.path] = true
				continue
			}
			problems = append(problems, p)
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
	for _, file := range slices.Sorted(maps.Keys(madeUp)) {
		if !used[file] {
			problems = append(problems, fmt.Sprintf("%s is listed as naming a home it did not make, but names none", file))
		}
	}
	return problems
}

// madeUpHomes are the test files that name a folder of realHomeNames under
// a home the check cannot tell the test made, each with the reason it is
// not the real one.
var madeUpHomes = map[string]string{
	"cmd/eva-md/uninstall_test.go":                  madeUpUser,
	"internal/eval/continuation/v1arm/main_test.go": childHome,
	"internal/health/freshness_test.go":             tildeText,
	"internal/health/hostnames_test.go":             tildeText,
	"internal/hook/pointers_test.go":                madeUpUser,
	"internal/hostfacts/fixtures_test.go":           madeUpUser,
	"internal/install/adapter_test.go":              madeUpUser,
	"internal/install/mcp_test.go":                  madeUpUser,
	"internal/install/plugin_bytes_test.go":         madeUpUser,
	"internal/install/recall_skill_test.go":         madeUpUser,
	"internal/install/skill_test.go":                madeUpUser,
	"internal/mcp/server_test.go":                   madeUpUser,
	"internal/resume/brief_test.go":                 madeUpUser,
	"internal/resume/systems_test.go":               madeUpUser,
	"internal/spec/home_test.go":                    seededHome,
}

// The reasons of madeUpHomes.
const (
	madeUpUser = "the home of a made-up user, as text the test compares or a path it never reads"
	tildeText  = "~ as the text a message shows, which nothing expands"
	childHome  = "a fake host run as a child of a test, given the test's own home as HOME, builds its folders from it"
	seededHome = "the files this check is seeded with, which it only parses"
)

// The tests of every package that starts git, or links code that does,
// clear the git configuration and identity the environment carries before
// any test runs, or are listed with the reason they do not yet; every test
// that reads the home folder is in a package whose tests give it one of
// their own; and no test names the hosts' or eva-md's folders under the
// real home, but the few listed with the reason theirs is not it.
func TestTestsRunInATemporaryHome(t *testing.T) {
	tempHome(t)
	files := sources(t, repoRoot(t))
	for _, p := range checkHome(files, unclearedGit, madeUpHomes) {
		t.Error(p)
	}
	scope := gitScope(files)
	for _, pkg := range []string{"internal/hook", "internal/identity", "cmd/eva-md", "internal/wikigit"} {
		if scope[pkg] == "" {
			t.Errorf("%s reads as starting no git: the check read the wrong source", pkg)
		}
	}
}

// The check fails a package whose tests start git, by name or through a
// constant, or link code that does, with no TestMain, with one that clears
// only after its tests have run, in a defer, a go statement or a function
// it never calls, or with one that never clears; a listed package that
// clears, or starts and links no git; a test that reads the home folder in
// a package whose tests never set HOME; a test that names a host's or
// eva-md's folder through $HOME, ~ or a home written out, unless its file
// is listed, and a listed file that names none; and a test that builds one
// from the home, itself, through a name or through a helper, before it set
// HOME. A TestMain that clears through a function it calls, after a fake's
// early exit, passes, and so do a test that reads HOME another test file
// sets and one that builds a host's folder once its subtest set HOME.
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
		madeUp map[string]string
	}{
		{"none", nil, nil, nil, nil},
		{"git started by a test with no TestMain", map[string]string{
			"internal/b/b_test.go": "package b\n\nimport (\n\t\"context\"\n\t\"os/exec\"\n\t\"testing\"\n)\n\nfunc TestB(t *testing.T) { _ = exec.CommandContext(context.Background(), \"git\", \"init\").Run() }\n",
		}, nil, []string{`^internal/b: internal/b/b_test\.go starts git, but no TestMain of it calls testgitenv\.Clear before its tests run$`}, nil},
		{"a TestMain that clears after the tests", map[string]string{
			"internal/a/main_test.go": strings.Replace(clears, "\ttestgitenv.Clear()\n\treturn tests.Run()\n", "\tcode := tests.Run()\n\ttestgitenv.Clear()\n\treturn code\n", 1),
		}, nil, []string{`^internal/a: its tests link internal/a/a\.go, which starts git, but no TestMain`}, nil},
		{"a TestMain that never clears", map[string]string{
			"internal/a/main_test.go": "package a\n\nimport (\n\t\"os\"\n\t\"testing\"\n)\n\nfunc TestMain(m *testing.M) { os.Exit(m.Run()) }\n",
		}, nil, []string{`^internal/a: its tests link internal/a/a\.go, which starts git, but no TestMain`}, nil},
		{"a listed package that clears", nil, map[string]string{"internal/a": "x"}, []string{`^internal/a clears the git environment, yet is listed as needing no clearing$`}, nil},
		{"a listed package that starts no git", nil, map[string]string{"internal/b": "x"}, []string{`^internal/b is listed as needing no clearing, but its tests start no git$`}, nil},
		{"a test that reads HOME none sets", map[string]string{
			"internal/b/home_test.go": "package b\n\nimport (\n\t\"os\"\n\t\"testing\"\n)\n\nfunc TestHome(t *testing.T) { _, _ = os.UserHomeDir() }\n",
		}, nil, []string{`^internal/b/home_test\.go reads the home folder, but no test of internal/b sets HOME$`}, nil},
		{"a TestMain that clears in a defer", map[string]string{
			"internal/a/main_test.go": strings.Replace(clears, "\ttestgitenv.Clear()\n", "\tdefer testgitenv.Clear()\n", 1),
		}, nil, []string{`^internal/a: its tests link internal/a/a\.go, which starts git, but no TestMain`}, nil},
		{"a TestMain that clears in a function it never calls", map[string]string{
			"internal/a/main_test.go": strings.Replace(clears, "\ttestgitenv.Clear()\n", "\t_ = func() { testgitenv.Clear() }\n", 1),
		}, nil, []string{`^internal/a: its tests link internal/a/a\.go, which starts git, but no TestMain`}, nil},
		{"a TestMain that clears in a go statement", map[string]string{
			"internal/a/main_test.go": strings.Replace(clears, "\ttestgitenv.Clear()\n", "\tgo testgitenv.Clear()\n", 1),
		}, nil, []string{`^internal/a: its tests link internal/a/a\.go, which starts git, but no TestMain`}, nil},
		{"git named by a constant", map[string]string{
			"internal/b/b_test.go": "package b\n\nimport (\n\t\"os/exec\"\n\t\"testing\"\n)\n\nconst gitBin = \"git\"\n\nfunc TestB(t *testing.T) { _ = exec.Command(gitBin, \"init\").Run() }\n",
		}, nil, []string{`^internal/b: internal/b/b_test\.go starts git, but no TestMain`}, nil},
		{"git started through another package", map[string]string{
			"internal/c/c_test.go": "package c\n\nimport (\n\t\"testing\"\n\n\t_ \"" + module + "/internal/a\"\n)\n\nfunc TestC(t *testing.T) {}\n",
		}, nil, []string{`^internal/c: its tests link internal/a/a\.go, which starts git, but no TestMain`}, nil},
		{"a listed package that starts and links no git", map[string]string{
			"internal/c/c_test.go": "package c\n\nimport \"testing\"\n\nfunc TestC(t *testing.T) {}\n",
		}, map[string]string{"internal/c": "x"}, []string{`^internal/c is listed as needing no clearing, but its tests start no git$`}, nil},
		{"a host's folder through $HOME", map[string]string{
			"internal/a/x_test.go": "package a\n\nimport (\n\t\"os\"\n\t\"testing\"\n)\n\nfunc TestX(t *testing.T) { _, _ = os.ReadFile(os.ExpandEnv(\"$HOME/.claude/settings.json\")) }\n",
		}, nil, []string{`^internal/a/x_test\.go:8 names ~/\.claude/settings\.json, a folder of a home the test did not make$`}, nil},
		{"a host's folder through ~", map[string]string{
			"internal/a/x_test.go": "package a\n\nimport \"testing\"\n\nfunc TestX(t *testing.T) { _ = \"~/.codex/sessions\" }\n",
		}, nil, []string{`^internal/a/x_test\.go:5 names ~/\.codex/sessions, a folder of a home the test did not make$`}, nil},
		{"eva-md's folder written out", map[string]string{
			"internal/a/x_test.go": "package a\n\nimport \"testing\"\n\nfunc TestX(t *testing.T) { _ = \"/Users/someone/.config/eva-md/config.toml\" }\n",
		}, nil, []string{`^internal/a/x_test\.go:5 names /Users/someone/\.config/eva-md, a folder of a home the test did not make$`}, nil},
		{"a home written out, listed", map[string]string{
			"internal/a/x_test.go": "package a\n\nimport \"testing\"\n\nfunc TestX(t *testing.T) { _ = \"/Users/it's me/.local/share/eva-md/bin/eva-md\" }\n",
		}, nil, nil, map[string]string{"internal/a/x_test.go": madeUpUser}},
		{"a listed file that names no home", nil, nil, []string{`^internal/a/home_test\.go is listed as naming a home it did not make, but names none$`}, map[string]string{"internal/a/home_test.go": madeUpUser}},
		{"a host's folder built from the home before the test sets it", map[string]string{
			"internal/a/x_test.go": "package a\n\nimport (\n\t\"os\"\n\t\"path/filepath\"\n\t\"testing\"\n)\n\nfunc TestX(t *testing.T) {\n\tp := filepath.Join(os.Getenv(\"HOME\"), \".claude\", \"settings.json\")\n\tt.Setenv(\"HOME\", t.TempDir())\n\t_ = p\n}\n",
		}, nil, []string{`^internal/a/x_test\.go:10 builds ~/\.claude/settings\.json from the real home, before its test set HOME$`}, nil},
		{"a host's folder built from a name for the home", map[string]string{
			"internal/a/x_test.go": "package a\n\nimport (\n\t\"os\"\n\t\"testing\"\n)\n\nfunc TestX(t *testing.T) {\n\thome, _ := os.UserHomeDir()\n\t_ = home + \"/.codex/config.toml\"\n}\n",
		}, nil, []string{`^internal/a/x_test\.go:10 builds ~/\.codex/config\.toml from the real home, before its test set HOME$`}, nil},
		{"a host's folder built by a helper a test calls before it sets HOME", map[string]string{
			"internal/a/x_test.go": "package a\n\nimport (\n\t\"os\"\n\t\"path/filepath\"\n\t\"testing\"\n)\n\nfunc store() string { return filepath.Join(os.Getenv(\"HOME\"), \".local\", \"share\", \"eva-md\") }\n\n" +
				"func TestX(t *testing.T) {\n\t_ = store()\n\tisolate(t)\n}\n\nfunc TestY(t *testing.T) {\n\tisolate(t)\n\t_ = store()\n}\n\nfunc isolate(t *testing.T) { t.Setenv(\"HOME\", t.TempDir()) }\n",
		}, nil, []string{`^internal/a/x_test\.go:9 builds ~/\.local/share/eva-md from the real home, before its test set HOME$`}, nil},
		{"a host's folder built once the test set HOME", map[string]string{
			"internal/a/x_test.go": "package a\n\nimport (\n\t\"os\"\n\t\"path/filepath\"\n\t\"testing\"\n)\n\nfunc store() string { return filepath.Join(os.Getenv(\"HOME\"), \".local\", \"share\", \"eva-md\") }\n\n" +
				"func TestX(t *testing.T) {\n\tt.Run(\"a\", func(t *testing.T) {\n\t\tisolate(t)\n\t\t_ = store()\n\t\t_ = filepath.Join(os.Getenv(\"HOME\"), \".claude\")\n\t})\n}\n\nfunc isolate(t *testing.T) { t.Setenv(\"HOME\", t.TempDir()) }\n",
		}, nil, nil, nil},
	} {
		t.Run(c.name, func(t *testing.T) {
			files := maps.Clone(base)
			maps.Copy(files, c.files)
			expectProblems(t, files, func(files []goFile) []string { return checkHome(files, c.listed, c.madeUp) }, c.want)
		})
	}
}
