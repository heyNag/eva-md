package spec

import (
	"fmt"
	"go/ast"
	"go/types"
	"maps"
	"slices"
	"strings"
	"testing"
)

// Git's background work off in the test rigs (EV-10e): after a commit, a
// clone, a fetch or a push, git may start its own maintenance in the
// background, a process that outlives the command that started it and
// races the removal of the test's temporary folder and the next command's
// locks. So a test file that gives git a configuration of its own, in a
// package whose tests commit, clone, fetch or push, turns both off in it,
// with gc.auto set to 0 and maintenance.auto to false. All is read from
// the source, so what it cannot tell is not held: a configuration written
// through a path neither the environment's nor one the package's tests
// set GIT_CONFIG_GLOBAL to, settings given in another file or package, a
// second configuration in the same file that leaves them out, git started
// by a program found other than by the name git, a function reached other
// than by a call of its name, and a command that starts the background
// work under another name, as a merge or a pull.

// backgroundCommands are git's commands after which git may start its
// background work.
var backgroundCommands = []string{"commit", "clone", "fetch", "push"}

// gitCalls are the calls n, of f, holds that start git: exec.Command with
// "git", or exec.CommandContext with a context and "git", given as a
// string or a constant of the module.
func gitCalls(src source, f goFile, n ast.Node) []*ast.CallExpr {
	exec := f.imports()["os/exec"]
	if exec == "" {
		return nil
	}
	var out []*ast.CallExpr
	ast.Inspect(n, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
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
				out = append(out, call)
			}
		}
		return true
	})
	return out
}

// literals are the values of n's string literals, in the order they
// stand.
func literals(n ast.Node) []string {
	var out []string
	ast.Inspect(n, func(n ast.Node) bool {
		if lit, ok := n.(*ast.BasicLit); ok {
			if s, ok := literal(lit); ok {
				out = append(out, s)
			}
		}
		return true
	})
	return out
}

// declared is a function or method of the module.
type declared struct {
	f    goFile
	decl *ast.FuncDecl
}

// declsOf indexes the functions and methods of files: a function by
// <package folder>.<name>, a method by <package folder>.(method).<name>.
// Init and functions with no body are left out.
func declsOf(files []goFile) map[string][]declared {
	out := map[string][]declared{}
	for _, f := range files {
		for _, decl := range f.file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil || fn.Name.Name == "init" {
				continue
			}
			key := f.pkg + "." + fn.Name.Name
			if fn.Recv != nil {
				key = f.pkg + ".(method)." + fn.Name.Name
			}
			out[key] = append(out[key], declared{f, fn})
		}
	}
	return out
}

// callees are the functions and methods of the module that n, of f, calls
// by name: a function of f's package or of a package of the module f
// imports, or a method of f's package, which a call on any value but a
// package may name.
func callees(f goFile, n ast.Node) []string {
	var out []string
	ast.Inspect(n, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		switch fn := call.Fun.(type) {
		case *ast.Ident:
			out = append(out, f.pkg+"."+fn.Name)
		case *ast.SelectorExpr:
			if name := moduleName(f, fn); name != "" && !strings.HasPrefix(name, f.pkg+".") {
				out = append(out, name)
			} else {
				out = append(out, f.pkg+".(method)."+fn.Sel.Name)
			}
		}
		return true
	})
	return out
}

// committing says, of each function and method of the module that
// commits, clones, fetches or pushes, how: it starts git, itself or
// through what it calls, and names one of backgroundCommands as a string
// literal, or it calls one that does. Calls are followed by name only.
func committing(src source, decls map[string][]declared) map[string]string {
	keys := slices.Sorted(maps.Keys(decls))
	// spread marks every function that calls a marked one, until none is
	// left to mark, and says which it called.
	spread := func(marked func(string) bool, mark func(key, callee string, d declared)) {
		for changed := true; changed; {
			changed = false
			for _, key := range keys {
				for _, d := range decls[key] {
					if marked(key) {
						break
					}
					called := callees(d.f, d.decl.Body)
					if i := slices.IndexFunc(called, marked); i >= 0 {
						mark(key, called[i], d)
						changed = true
					}
				}
			}
		}
	}
	runs := map[string]bool{}
	for _, key := range keys {
		for _, d := range decls[key] {
			if len(gitCalls(src, d.f, d.decl.Body)) > 0 {
				runs[key] = true
			}
		}
	}
	spread(func(k string) bool { return runs[k] }, func(key, _ string, _ declared) { runs[key] = true })
	how, root := map[string]string{}, map[string]string{}
	for _, key := range keys {
		for _, d := range decls[key] {
			if !runs[key] || how[key] != "" {
				break
			}
			for _, s := range literals(d.decl.Body) {
				if slices.Contains(backgroundCommands, s) {
					how[key] = d.f.line(d.decl) + " starts git and names its " + s
					root[key] = how[key]
					break
				}
			}
		}
	}
	spread(func(k string) bool { return how[k] != "" }, func(key, callee string, d declared) {
		root[key] = root[callee]
		how[key] = d.f.line(d.decl) + " calls " + callee + ", through which " + root[key]
	})
	return how
}

// backgroundScope lists the packages whose tests commit, clone, fetch or
// push, each with how: a function or method of one of its test files does,
// itself or through what it calls.
func backgroundScope(files []goFile) map[string]string {
	decls := declsOf(files)
	how := committing(sourceOf(files), decls)
	scope := map[string]string{}
	for _, key := range slices.Sorted(maps.Keys(decls)) {
		for _, d := range decls[key] {
			if d.f.test && how[key] != "" && strings.HasPrefix(how[key], d.f.path+":") && scope[d.f.pkg] == "" {
				scope[d.f.pkg] = how[key]
			}
		}
	}
	return scope
}

// setsGlobal is the value a call of Setenv gives GIT_CONFIG_GLOBAL, as
// its source, and whether it is one.
func setsGlobal(call *ast.CallExpr) (string, bool) {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "Setenv" || len(call.Args) != 2 {
		return "", false
	}
	if s, ok := literal(call.Args[0]); !ok || s != "GIT_CONFIG_GLOBAL" {
		return "", false
	}
	return types.ExprString(call.Args[1]), true
}

// rigOf says how a test file gives git a configuration of its own, or "":
// it sets GIT_CONFIG_GLOBAL with Setenv; writes the file the environment's
// GIT_CONFIG_GLOBAL names, or a file named as one of globals, the values
// its package's tests set GIT_CONFIG_GLOBAL to; or gives the git it starts
// settings with -c.
func rigOf(src source, f goFile, globals map[string]bool) string {
	os := ""
	for path, name := range f.imports() {
		if path == "os" {
			os = name
		}
	}
	// getenv says whether x is os.Getenv("GIT_CONFIG_GLOBAL").
	getenv := func(x ast.Expr) bool {
		call, ok := x.(*ast.CallExpr)
		if !ok || len(call.Args) != 1 {
			return false
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != "Getenv" {
			return false
		}
		id, ok := sel.X.(*ast.Ident)
		s, _ := literal(call.Args[0])
		return ok && os != "" && id.Name == os && s == "GIT_CONFIG_GLOBAL"
	}
	// Names given the global configuration's path from the environment.
	fromEnv := map[string]bool{}
	ast.Inspect(f.file, func(n ast.Node) bool {
		if as, ok := n.(*ast.AssignStmt); ok && len(as.Lhs) == len(as.Rhs) {
			for i, rhs := range as.Rhs {
				if id, ok := as.Lhs[i].(*ast.Ident); ok && getenv(rhs) {
					fromEnv[id.Name] = true
				}
			}
		}
		return true
	})
	how := ""
	ast.Inspect(f.file, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok || how != "" {
			return how == ""
		}
		if _, ok := setsGlobal(call); ok {
			how = f.line(call) + " sets GIT_CONFIG_GLOBAL"
			return false
		}
		name := ""
		switch fn := call.Fun.(type) {
		case *ast.SelectorExpr:
			name = fn.Sel.Name
		case *ast.Ident:
			name = fn.Name
		}
		if strings.Contains(strings.ToLower(name), "write") || name == "OpenFile" {
			for _, arg := range call.Args {
				if id, ok := arg.(*ast.Ident); ok && fromEnv[id.Name] || getenv(arg) || globals[types.ExprString(arg)] {
					how = f.line(call) + " writes the file GIT_CONFIG_GLOBAL names"
					return false
				}
			}
		}
		return true
	})
	if how != "" {
		return how
	}
	for _, call := range gitCalls(src, f, f.file) {
		if slices.Contains(literals(call), "-c") {
			return f.line(call) + " gives its git settings with -c"
		}
	}
	return ""
}

// offSettings says which of gc.auto and maintenance.auto the string
// literals lits turn off: as -c's gc.auto=0, as the words of git config
// gc.auto 0, or as the text of a configuration with auto = 0 in its [gc]
// section. Names are read in any case, as git reads them.
func offSettings(lits []string) (gc, maintenance bool) {
	off := func(key, value string) {
		key, value = strings.ToLower(strings.TrimSpace(key)), strings.ToLower(strings.TrimSpace(value))
		switch {
		case key == "gc.auto" && value == "0":
			gc = true
		case key == "maintenance.auto" && slices.Contains([]string{"false", "no", "off", "0"}, value):
			maintenance = true
		}
	}
	for i, s := range lits {
		if k, v, ok := strings.Cut(s, "="); ok && !strings.ContainsAny(s, "\n[") {
			off(k, v)
		}
		if i+1 < len(lits) {
			off(s, lits[i+1])
		}
		section := ""
		for _, line := range strings.Split(s, "\n") {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
				section = strings.TrimSpace(line[1 : len(line)-1])
				continue
			}
			if k, v, ok := strings.Cut(line, "="); ok && section != "" && !strings.ContainsAny(section, " \"") {
				off(section+"."+k, v)
			}
		}
	}
	return gc, maintenance
}

// checkGitRigs holds the test files to git's background work off: every
// test file that gives git a configuration of its own, in a package whose
// tests commit, clone, fetch or push, turns off both gc.auto and
// maintenance.auto, or is in listed with its reason.
func checkGitRigs(files []goFile, listed map[string]string) []string {
	var problems []string
	src := sourceOf(files)
	scope := backgroundScope(files)
	globals := map[string]map[string]bool{} // by package, the values its tests set GIT_CONFIG_GLOBAL to
	for _, f := range files {
		if !f.test {
			continue
		}
		ast.Inspect(f.file, func(n ast.Node) bool {
			if call, ok := n.(*ast.CallExpr); ok {
				if v, ok := setsGlobal(call); ok {
					if globals[f.pkg] == nil {
						globals[f.pkg] = map[string]bool{}
					}
					globals[f.pkg][v] = true
				}
			}
			return true
		})
	}
	rigs := map[string]bool{}
	for _, f := range files {
		if !f.test || scope[f.pkg] == "" {
			continue
		}
		how := rigOf(src, f, globals[f.pkg])
		if how == "" {
			continue
		}
		rigs[f.path] = true
		gc, maintenance := offSettings(literals(f.file))
		_, isListed := listed[f.path]
		var missing []string
		if !gc {
			missing = append(missing, "gc.auto=0")
		}
		if !maintenance {
			missing = append(missing, "maintenance.auto=false")
		}
		switch {
		case len(missing) == 0 && isListed:
			problems = append(problems, fmt.Sprintf("%s turns git's background work off, yet is listed as leaving it on", f.path))
		case len(missing) > 0 && !isListed:
			problems = append(problems, fmt.Sprintf("%s: %s, and the tests of %s commit, clone, fetch or push (%s), but it does not set %s", f.path, how, f.pkg, scope[f.pkg], strings.Join(missing, " or ")))
		}
	}
	for _, path := range slices.Sorted(maps.Keys(listed)) {
		if !rigs[path] {
			problems = append(problems, fmt.Sprintf("%s is listed as leaving git's background work on, but gives git no configuration in a package whose tests commit, clone, fetch or push", path))
		}
	}
	return problems
}

// backgroundOn are the test files that give git a configuration of their
// own in a package whose tests commit, clone, fetch or push, yet do not
// turn git's background work off in it themselves, each with its reason.
var backgroundOn = map[string]string{}

// Every test rig that gives git a configuration of its own, in a package
// whose tests commit, clone, fetch or push, turns git's background work
// off, or is listed with the reason it does not.
func TestGitRigsTurnBackgroundWorkOff(t *testing.T) {
	tempHome(t)
	files := sources(t, repoRoot(t))
	for _, p := range checkGitRigs(files, backgroundOn) {
		t.Error(p)
	}
}
