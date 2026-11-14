package spec

import (
	"fmt"
	"go/ast"
	"go/token"
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
// with gc.auto set to 0 and maintenance.auto to false, in its own text or
// in a constant of the module it names. All is read from the source, so
// what it cannot tell is not held: a configuration written through a path
// neither the environment's nor one the package's tests set
// GIT_CONFIG_GLOBAL to, settings another file gives, a second
// configuration in the same file that leaves them out, git started by a
// program found other than by the name git, a function reached other than
// by a call of its name, and a command that starts the background work
// under another name, as a merge or a pull.

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
// stand, literals joined with + read as one.
func literals(n ast.Node) []string {
	var out []string
	ast.Inspect(n, func(n ast.Node) bool {
		if x, ok := n.(ast.Expr); ok {
			if s, ok := joined(x); ok {
				out = append(out, s)
				return false
			}
		}
		return true
	})
	return out
}

// joined is the value of a string literal, or of string literals joined
// with +, and whether x is one.
func joined(x ast.Expr) (string, bool) {
	switch x := x.(type) {
	case *ast.BasicLit:
		return literal(x)
	case *ast.ParenExpr:
		return joined(x.X)
	case *ast.BinaryExpr:
		if x.Op != token.ADD {
			return "", false
		}
		a, ok := joined(x.X)
		if !ok {
			return "", false
		}
		b, ok := joined(x.Y)
		return a + b, ok
	}
	return "", false
}

// settingsText is what a test file gives as text: its string literals,
// and the values of the constants of the module it names, followed
// through the constants they name.
func settingsText(src source, f goFile) []string {
	out := literals(f.file)
	seen := map[string]bool{}
	var follow func(in goFile, n ast.Node, depth int)
	follow = func(in goFile, n ast.Node, depth int) {
		ast.Inspect(n, func(n ast.Node) bool {
			x, ok := n.(ast.Expr)
			if !ok {
				return true
			}
			key := moduleName(in, x)
			if c, ok := src.consts[key]; ok && !seen[key] && depth < 8 {
				seen[key] = true
				out = append(out, literals(c.value)...)
				follow(c.f, c.value, depth+1)
			}
			_, sel := x.(*ast.SelectorExpr)
			return !sel
		})
	}
	follow(f, f.file, 0)
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

// isIdent says whether x is a name alone.
func isIdent(x ast.Expr) bool {
	_, ok := x.(*ast.Ident)
	return ok
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
	// Names given the global configuration's path from the environment:
	// assigned it, or ranging over a list that holds it.
	fromEnv := map[string]bool{}
	ast.Inspect(f.file, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.AssignStmt:
			for i, rhs := range n.Rhs {
				if id, ok := n.Lhs[i].(*ast.Ident); ok && len(n.Lhs) == len(n.Rhs) && getenv(rhs) {
					fromEnv[id.Name] = true
				}
			}
		case *ast.RangeStmt:
			list, ok := n.X.(*ast.CompositeLit)
			id, named := n.Value.(*ast.Ident)
			if ok && named && slices.ContainsFunc(list.Elts, getenv) {
				fromEnv[id.Name] = true
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
	// By package, the values its tests set GIT_CONFIG_GLOBAL to, as their
	// source, but a name alone, which means what it does only where it is
	// given.
	globals := map[string]map[string]bool{}
	for _, f := range files {
		if !f.test {
			continue
		}
		ast.Inspect(f.file, func(n ast.Node) bool {
			if call, ok := n.(*ast.CallExpr); ok {
				if v, ok := setsGlobal(call); ok && !isIdent(call.Args[1]) {
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
		gc, maintenance := offSettings(settingsText(src, f))
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
var backgroundOn = map[string]string{
	"cmd/eva-md/uninstall_test.go":                 "TestFakeMacStartsNoMaintenance holds that the fake Mac's own git config, which release_test.go writes, starts no maintenance after a commit and a push; settings given to its git with -c would hide what it holds",
	"internal/eval/continuation/task/main_test.go": "its git config is a decoy the program must not read: Prepare's git, which commits, runs with an empty global config and gc.auto=0 and maintenance.auto=false given with -c (workspace.go), and the tests' own git only reads",
	"internal/store/migrate/run_test.go":           "it rewrites the global config as a person's, a core.excludesFile or a signing that fails, to show what the program does with it; the program's git (git.go) and the tests' gitT (helpers_test.go) are given gc.auto=0 and maintenance.auto=false with -c, and the one commit it runs with neither fails by design",
}

// Every test rig that gives git a configuration of its own, in a package
// whose tests commit, clone, fetch or push, turns git's background work
// off, or is listed with the reason it does not.
func TestGitRigsTurnBackgroundWorkOff(t *testing.T) {
	tempHome(t)
	files := sources(t, repoRoot(t))
	for _, p := range checkGitRigs(files, backgroundOn) {
		t.Error(p)
	}
	// The check read what it should: packages whose tests commit through
	// their own helpers, a method or the program, and packages whose tests
	// only read with git or link a package that commits without calling it.
	scope := backgroundScope(files)
	for _, pkg := range []string{"cmd/eva-md", "cmd/hostrec", "internal/hook", "internal/identity", "internal/knowledgepage", "internal/release", "internal/wikigit", "internal/wikiinit"} {
		if scope[pkg] == "" {
			t.Errorf("%s reads as committing, cloning, fetching and pushing nothing: the check read the wrong source", pkg)
		}
	}
	for _, pkg := range []string{"internal/config", "internal/install", "internal/spec"} {
		if scope[pkg] != "" {
			t.Errorf("%s reads as committing, cloning, fetching or pushing (%s): the check reads too much", pkg, scope[pkg])
		}
	}
}

// The check fails a rig that commits and turns neither setting off, one
// that turns off only one of the two, one that writes the file the
// environment names through a name or a list, a -c helper, one that sets
// a value git does not read as off or sets auto in another section; a
// rig whose package commits through a method of its own or a function of
// another package; a listed rig that sets both, and a listed file that is
// no rig. It passes a rig whose settings are in a constant of another
// file, in any case of letters or joined with +, given as git config's
// words or -c's, a listed rig that sets neither, and a rig of a package
// whose tests only read with git, or link a package that commits without
// calling it.
func TestARigThatLeavesBackgroundWorkOnIsFound(t *testing.T) {
	tempHome(t)
	const (
		head   = "package b\n\nimport (\n\t\"os\"\n\t\"os/exec\"\n\t\"path/filepath\"\n\t\"testing\"\n)\n\n"
		commit = "func TestB(t *testing.T) { _ = exec.Command(\"git\", \"-C\", t.TempDir(), \"commit\", \"-m\", \"x\").Run() }\n\n"
	)
	// rig is a test file of package b that commits and writes text as its
	// git config.
	rig := func(text string) string {
		return head + commit + "func isolate(t *testing.T) {\n\thome := t.TempDir()\n\tt.Setenv(\"GIT_CONFIG_GLOBAL\", filepath.Join(home, \"gitconfig\"))\n" +
			"\t_ = os.WriteFile(filepath.Join(home, \"gitconfig\"), []byte(" + text + "), 0o600)\n}\n"
	}
	base := map[string]string{
		"internal/a/a.go":         "package a\n\nimport \"os/exec\"\n\nfunc Commit(dir string) error { return run(dir, \"commit\", \"-m\", \"x\") }\n\nfunc Head(dir string) error { return run(dir, \"rev-parse\", \"HEAD\") }\n\nfunc run(dir string, args ...string) error {\n\treturn exec.Command(\"git\", append([]string{\"-C\", dir}, args...)...).Run()\n}\n",
		"internal/a/main_test.go": "package a\n\nimport (\n\t\"os\"\n\t\"path/filepath\"\n\t\"testing\"\n)\n\nfunc TestA(t *testing.T) {\n\thome := t.TempDir()\n\tt.Setenv(\"GIT_CONFIG_GLOBAL\", filepath.Join(home, \"gitconfig\"))\n\t_ = os.WriteFile(filepath.Join(home, \"gitconfig\"), []byte(\"[gc]\\n\\tauto = 0\\n[maintenance]\\n\\tauto = false\\n\"), 0o600)\n\t_ = Commit(home)\n}\n",
		// A package whose tests read with git, and link a, which commits,
		// without calling it.
		"internal/c/c_test.go": "package c\n\nimport (\n\t\"path/filepath\"\n\t\"testing\"\n\n\t\"" + module + "/internal/a\"\n)\n\nfunc TestC(t *testing.T) {\n\tt.Setenv(\"GIT_CONFIG_GLOBAL\", filepath.Join(t.TempDir(), \"gitconfig\"))\n\t_ = a.Head(t.TempDir())\n}\n",
	}
	const unset = `^internal/b/b_test\.go: internal/b/b_test\.go:\d+ (sets GIT_CONFIG_GLOBAL|writes the file GIT_CONFIG_GLOBAL names|gives its git settings with -c), and the tests of internal/b commit, clone, fetch or push \(internal/b/b_test\.go:\d+ starts git and names its commit\), but it does not set `
	for _, c := range []struct {
		name   string
		files  map[string]string
		listed map[string]string
		want   []string
	}{
		{"none", nil, nil, nil},
		{"a rig that commits and sets neither", map[string]string{
			"internal/b/b_test.go": rig(`"[user]\n\tname = Made Up\n"`),
		}, nil, []string{unset + `gc\.auto=0 or maintenance\.auto=false$`}},
		{"a rig that sets gc.auto only", map[string]string{
			"internal/b/b_test.go": rig(`"[gc]\n\tauto = 0\n"`),
		}, nil, []string{unset + `maintenance\.auto=false$`}},
		{"a rig that sets maintenance.auto only", map[string]string{
			"internal/b/b_test.go": rig(`"[maintenance]\n\tauto = false\n"`),
		}, nil, []string{unset + `gc\.auto=0$`}},
		{"a rig that sets both", map[string]string{
			"internal/b/b_test.go": rig(`"[user]\n\tname = Made Up\n[gc]\n\tauto = 0\n[maintenance]\n\tauto = false\n"`),
		}, nil, nil},
		{"settings in a constant of another file, in capitals and joined with +", map[string]string{
			"internal/b/b_test.go":     rig("quiet"),
			"internal/b/quiet_test.go": "package b\n\nconst quiet = \"[GC]\\n\\tAuto = 0\\n\" + \"[Maintenance]\\n\\tauto = no\\n\"\n",
		}, nil, nil},
		{"a value git does not read as off, and auto in another section", map[string]string{
			"internal/b/b_test.go": rig(`"[gc]\n\tauto = 1\n[core]\n\tauto = false\n"`),
		}, nil, []string{unset + `gc\.auto=0 or maintenance\.auto=false$`}},
		{"settings as git config's words", map[string]string{
			"internal/b/b_test.go": rig(`""`) + "\nfunc quiet(t *testing.T) {\n\t_ = exec.Command(\"git\", \"config\", \"--global\", \"gc.auto\", \"0\").Run()\n\t_ = exec.Command(\"git\", \"config\", \"--global\", \"maintenance.auto\", \"false\").Run()\n}\n",
		}, nil, nil},
		{"the file the environment names, written through a name", map[string]string{
			"internal/b/b_test.go": head + commit + "func write(t *testing.T) {\n\tglobal := os.Getenv(\"GIT_CONFIG_GLOBAL\")\n\t_ = os.WriteFile(global, nil, 0o600)\n\t_ = filepath.Join\n}\n",
		}, nil, []string{unset + `gc\.auto=0 or maintenance\.auto=false$`}},
		{"the file the environment names, written through a list", map[string]string{
			"internal/b/b_test.go": head + commit + "func write(t *testing.T) {\n\tfor _, p := range []string{os.Getenv(\"GIT_CONFIG_GLOBAL\"), filepath.Join(t.TempDir(), \".gitconfig\")} {\n\t\t_ = os.WriteFile(p, nil, 0o600)\n\t}\n}\n",
		}, nil, []string{unset + `gc\.auto=0 or maintenance\.auto=false$`}},
		{"a -c helper", map[string]string{
			"internal/b/b_test.go": head + commit + "func git(dir string, args ...string) error {\n\t_ = os.Getenv\n\t_ = filepath.Join\n\treturn exec.Command(\"git\", append([]string{\"-c\", \"user.name=Made Up\", \"-C\", dir}, args...)...).Run()\n}\n",
		}, nil, []string{unset + `gc\.auto=0 or maintenance\.auto=false$`}},
		{"a -c helper that sets both", map[string]string{
			"internal/b/b_test.go": head + commit + "func git(dir string, args ...string) error {\n\t_ = os.Getenv\n\t_ = filepath.Join\n\treturn exec.Command(\"git\", append([]string{\"-c\", \"gc.auto=0\", \"-c\", \"maintenance.auto=false\", \"-C\", dir}, args...)...).Run()\n}\n",
		}, nil, nil},
		{"a package that commits through a method of its own", map[string]string{
			"internal/d/d.go":      "package d\n\nimport \"os/exec\"\n\ntype Repo struct{ Dir string }\n\nfunc (r Repo) Push() error { return exec.Command(\"git\", \"-C\", r.Dir, \"push\", \"origin\", \"main\").Run() }\n",
			"internal/d/d_test.go": "package d\n\nimport (\n\t\"path/filepath\"\n\t\"testing\"\n)\n\nfunc TestD(t *testing.T) {\n\tt.Setenv(\"GIT_CONFIG_GLOBAL\", filepath.Join(t.TempDir(), \"gitconfig\"))\n\t_ = Repo{t.TempDir()}.Push()\n}\n",
		}, nil, []string{`^internal/d/d_test\.go: internal/d/d_test\.go:\d+ sets GIT_CONFIG_GLOBAL, and the tests of internal/d commit, clone, fetch or push \(internal/d/d_test\.go:\d+ calls internal/d\.\(method\)\.Push, through which internal/d/d\.go:\d+ starts git and names its push\), but it does not set gc\.auto=0 or maintenance\.auto=false$`}},
		{"a package that commits through the program", map[string]string{
			"internal/c/c_test.go": "package c\n\nimport (\n\t\"path/filepath\"\n\t\"testing\"\n\n\t\"" + module + "/internal/a\"\n)\n\nfunc TestC(t *testing.T) {\n\tt.Setenv(\"GIT_CONFIG_GLOBAL\", filepath.Join(t.TempDir(), \"gitconfig\"))\n\t_ = a.Commit(t.TempDir())\n}\n",
		}, nil, []string{`^internal/c/c_test\.go: internal/c/c_test\.go:\d+ sets GIT_CONFIG_GLOBAL, and the tests of internal/c commit, clone, fetch or push \(internal/c/c_test\.go:\d+ calls internal/a\.Commit, through which internal/a/a\.go:\d+ starts git and names its commit\), but it does not set gc\.auto=0 or maintenance\.auto=false$`}},
		{"a listed rig that sets both", nil, map[string]string{"internal/a/main_test.go": "x"}, []string{`^internal/a/main_test\.go turns git's background work off, yet is listed as leaving it on$`}},
		{"a listed rig that sets neither", map[string]string{
			"internal/b/b_test.go": rig(`""`),
		}, map[string]string{"internal/b/b_test.go": "x"}, nil},
		{"a listed file that is no rig", nil, map[string]string{"internal/c/c_test.go": "x"}, []string{`^internal/c/c_test\.go is listed as leaving git's background work on, but gives git no configuration in a package whose tests commit, clone, fetch or push$`}},
	} {
		t.Run(c.name, func(t *testing.T) {
			files := maps.Clone(base)
			maps.Copy(files, c.files)
			expectProblems(t, files, func(files []goFile) []string { return checkGitRigs(files, c.listed) }, c.want)
		})
	}
}
