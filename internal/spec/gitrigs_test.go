package spec

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"maps"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// Git's background work off in the test rigs (EV-10e): after a commit, a
// clone, a fetch or a push, git may start its own maintenance in the
// background, a process that outlives the command that started it and
// races the removal of the test's temporary folder and the next command's
// locks. So in a package whose tests commit, clone, fetch or push, every
// configuration a test file gives git turns both off, with gc.auto set to 0
// and maintenance.auto to false, as git reads them, the last value of a key
// taking an earlier one's place:
//
//   - each write of the file GIT_CONFIG_GLOBAL names writes both, and a
//     write that adds to it, by appending or with git config --global,
//     turns neither back on;
//   - each Setenv of GIT_CONFIG_GLOBAL is joined, in the same function, by
//     a write of the file it names, or by git config --global setting both;
//   - git given settings with -c turns neither back on, and gives both
//     itself when its file writes no global configuration and no test file
//     of its package writes one with both;
//   - a commit, clone, fetch or push through a function of the module whose
//     git reads no global configuration, as GIT_CONFIG_GLOBAL=/dev/null in
//     its environment makes it, gives both with -c.
//
// What a configuration holds is read from the expressions that give it:
// string literals, joined with +, the names a function gave them before,
// the module's constants and package-level variables, and the arguments
// of a call such as a conversion to bytes or fmt.Sprintf. A package's
// tests commit when a function of a test file starts git, itself or
// through the module's functions and methods it calls by name, and names
// one of the commands, as a word of its own, through a constant or a
// package-level list, or in a shell script it runs with sh -c; git is
// started by exec.Command with "git", a constant of the module that is
// "git", or a name given exec.LookPath("git"). All is read from the
// source, so what it cannot tell is not held: a configuration written
// through a path that is neither the environment's nor one a test of the
// package sets GIT_CONFIG_GLOBAL to, or through a function that is given
// no path; settings given through a function's parameters or what another
// function returns; a value written to the configuration in a loop, whose
// last value the source does not show; git started under a name given
// other than by exec.LookPath("git"); a function reached other than by a
// call of its name, or a method called on a value whose type the call does
// not show; and a command that starts the background work under another
// name, as a merge or a pull.

// backgroundCommands are git's commands after which git may start its
// background work.
var backgroundCommands = []string{"commit", "clone", "fetch", "push"}

// shells are the programs a test may run a shell script of git with.
var shells = []string{"sh", "bash", "zsh", "/bin/sh", "/bin/bash", "/bin/zsh"}

// shellGit finds git started in a shell script, and shellGitCommand the
// command it is started with, after git's own options.
var (
	shellGit        = regexp.MustCompile(`(?:^|[\s;&|(])git\s`)
	shellGitCommand = regexp.MustCompile(`(?:^|[\s;&|(])git(?:\s+-\S+(?:\s+[^-\s]\S*)?)*\s+([a-z-]+)`)
)

// gitSource is what the checks of git rigs read of the module: its
// constants and functions, its package-level variables of one value each,
// by <package folder>.<name>, and, by file, the names given
// exec.LookPath("git").
type gitSource struct {
	source
	vars     map[string]constDef
	lookPath map[string]map[string]bool
}

// gitSourceOf indexes files for the checks of git rigs.
func gitSourceOf(files []goFile) gitSource {
	src := gitSource{sourceOf(files), map[string]constDef{}, map[string]map[string]bool{}}
	for _, f := range files {
		for _, decl := range f.file.Decls {
			gen, ok := decl.(*ast.GenDecl)
			if !ok || gen.Tok != token.VAR {
				continue
			}
			for _, sp := range gen.Specs {
				spec := sp.(*ast.ValueSpec)
				if len(spec.Names) == len(spec.Values) {
					for i, name := range spec.Names {
						src.vars[f.pkg+"."+name.Name] = constDef{spec.Values[i], f}
					}
				}
			}
		}
		exec := f.imports()["os/exec"]
		if exec == "" {
			continue
		}
		// lookGit says whether x is exec.LookPath("git").
		lookGit := func(x ast.Expr) bool {
			call, ok := x.(*ast.CallExpr)
			if !ok || len(call.Args) != 1 || !isCallOf(call, exec, "LookPath") {
				return false
			}
			s, ok := literal(call.Args[0])
			return ok && s == "git"
		}
		names := map[string]bool{}
		ast.Inspect(f.file, func(n ast.Node) bool {
			var lhs []ast.Expr
			var rhs []ast.Expr
			switch n := n.(type) {
			case *ast.AssignStmt:
				lhs, rhs = n.Lhs, n.Rhs
			case *ast.ValueSpec:
				for _, name := range n.Names {
					lhs = append(lhs, name)
				}
				rhs = n.Values
			}
			if len(rhs) == 1 && len(lhs) > 0 && lookGit(rhs[0]) {
				if id, ok := lhs[0].(*ast.Ident); ok {
					names[id.Name] = true
				}
			}
			return true
		})
		src.lookPath[f.path] = names
	}
	return src
}

// isCallOf says whether call is pkg.name(...), pkg being the name a
// package is known by in the file.
func isCallOf(call *ast.CallExpr, pkg, name string) bool {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != name {
		return false
	}
	id, ok := sel.X.(*ast.Ident)
	return ok && pkg != "" && id.Name == pkg
}

// gitCalls are the calls n, of f, holds that start git: exec.Command with
// "git", or exec.CommandContext with a context and "git", given as a
// string, a constant of the module or a name given exec.LookPath("git"),
// or a shell given a script that starts git.
func gitCalls(src gitSource, f goFile, n ast.Node) []*ast.CallExpr {
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
		at := map[string]int{"Command": 0, "CommandContext": 1}
		i, ok := at[callName(call)]
		if !ok || len(call.Args) <= i || !isCallOf(call, exec, callName(call)) {
			return true
		}
		in, arg := f, call.Args[i]
		for depth := 0; depth < 8; depth++ {
			c, ok := src.consts[moduleName(in, arg)]
			if !ok {
				break
			}
			in, arg = c.f, c.value
		}
		s, ok := literal(arg)
		id, named := arg.(*ast.Ident)
		switch {
		case ok && s == "git", named && in.path == f.path && src.lookPath[f.path][id.Name]:
			out = append(out, call)
		case ok && slices.Contains(shells, s) && slices.ContainsFunc(literals(call), shellGit.MatchString):
			out = append(out, call)
		}
		return true
	})
	return out
}

// callName is the name a call calls: a function's, or a method's or a
// package's function's after the dot; "" for any other.
func callName(call *ast.CallExpr) string {
	switch fn := call.Fun.(type) {
	case *ast.SelectorExpr:
		return fn.Sel.Name
	case *ast.Ident:
		return fn.Name
	}
	return ""
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

// resolver reads what an expression of a function gives as words or as
// text: through the names the function gave values before it, and the
// module's constants and package-level variables.
type resolver struct {
	src  gitSource
	f    goFile
	body ast.Node // the function's body, or nil outside one
}

// assigned is what the function gave the name id last before id: by an
// assignment or a declaration that ends before it, or, for the value of a
// range id is in, the list ranged over; or nil.
func (r resolver) assigned(id *ast.Ident) ast.Expr {
	if r.body == nil {
		return nil
	}
	var out ast.Expr
	ast.Inspect(r.body, func(n ast.Node) bool {
		var lhs []*ast.Ident
		var rhs []ast.Expr
		switch n := n.(type) {
		case *ast.AssignStmt:
			if n.End() < id.Pos() && len(n.Lhs) == len(n.Rhs) {
				for _, l := range n.Lhs {
					name, _ := l.(*ast.Ident)
					lhs = append(lhs, name)
				}
				rhs = n.Rhs
			}
		case *ast.ValueSpec:
			if n.End() < id.Pos() && len(n.Names) == len(n.Values) {
				lhs, rhs = n.Names, n.Values
			}
		case *ast.RangeStmt:
			if v, ok := n.Value.(*ast.Ident); ok && v.Name == id.Name && n.Body.Pos() <= id.Pos() && id.Pos() < n.Body.End() {
				out = n.X
			}
		}
		for i, name := range lhs {
			if name != nil && name.Name == id.Name {
				out = rhs[i]
			}
		}
		return true
	})
	return out
}

// named is the expression a name stands for, and the resolver to read it
// with; nil for a name the source does not show the value of.
func (r resolver) named(x ast.Expr) (resolver, ast.Expr) {
	if id, ok := x.(*ast.Ident); ok {
		if v := r.assigned(id); v != nil {
			return r, v
		}
	}
	key := moduleName(r.f, x)
	if c, ok := r.src.consts[key]; ok {
		return resolver{r.src, c.f, nil}, c.value
	}
	if c, ok := r.src.vars[key]; ok {
		return resolver{r.src, c.f, nil}, c.value
	}
	return r, nil
}

// words are the strings x gives as a list of words: a string, the
// elements of a list written out, what append joins, and what a name was
// given.
func (r resolver) words(x ast.Expr, depth int) []string {
	if depth > 8 {
		return nil
	}
	switch x := x.(type) {
	case *ast.BasicLit, *ast.BinaryExpr:
		if s, ok := joined(x); ok {
			return []string{s}
		}
		if s := r.text(x, depth+1); s != "" {
			return []string{s}
		}
	case *ast.ParenExpr:
		return r.words(x.X, depth+1)
	case *ast.CompositeLit:
		var out []string
		for _, e := range x.Elts {
			if kv, ok := e.(*ast.KeyValueExpr); ok {
				e = kv.Value
			}
			out = append(out, r.words(e, depth+1)...)
		}
		return out
	case *ast.CallExpr:
		if id, ok := x.Fun.(*ast.Ident); ok && id.Name == "append" {
			var out []string
			for _, a := range x.Args {
				out = append(out, r.words(a, depth+1)...)
			}
			return out
		}
	case *ast.Ident, *ast.SelectorExpr:
		if in, v := r.named(x); v != nil {
			return in.words(v, depth+1)
		}
	}
	return nil
}

// argWords are the words of a call's arguments, one after another.
func (r resolver) argWords(call *ast.CallExpr) []string {
	var out []string
	for _, a := range call.Args {
		out = append(out, r.words(a, 0)...)
	}
	return out
}

// text is what x gives as text: a string, strings joined with +, what a
// name was given, and the arguments of a call, such as a conversion to
// bytes or fmt.Sprintf, one after another; "" for what the source does not
// show.
func (r resolver) text(x ast.Expr, depth int) string {
	if depth > 8 {
		return ""
	}
	switch x := x.(type) {
	case *ast.BasicLit:
		s, _ := literal(x)
		return s
	case *ast.ParenExpr:
		return r.text(x.X, depth+1)
	case *ast.BinaryExpr:
		if x.Op == token.ADD {
			return r.text(x.X, depth+1) + r.text(x.Y, depth+1)
		}
	case *ast.CallExpr:
		var b strings.Builder
		for _, a := range x.Args {
			b.WriteString(r.text(a, depth+1))
		}
		return b.String()
	case *ast.Ident, *ast.SelectorExpr:
		if in, v := r.named(x); v != nil {
			return in.text(v, depth+1)
		}
	}
	return ""
}

// bodyWords are the words a function's body names: its string literals,
// the values of the module's constants and package-level variables it
// names, and the command each shell script among its literals starts git
// with.
func bodyWords(r resolver, body ast.Node) []string {
	var out []string
	ast.Inspect(body, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.BasicLit:
			if s, ok := literal(x); ok {
				out = append(out, s)
				for _, m := range shellGitCommand.FindAllStringSubmatch(s, -1) {
					out = append(out, m[1])
				}
			}
		case *ast.Ident, *ast.SelectorExpr:
			key := moduleName(r.f, x.(ast.Expr))
			if _, ok := r.src.consts[key]; ok {
				out = append(out, r.words(x.(ast.Expr), 0)...)
			} else if _, ok := r.src.vars[key]; ok {
				in, v := r.named(x.(ast.Expr))
				if v != nil {
					out = append(out, in.words(v, 0)...)
				}
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

// typePackages are, by the names n gives them, the module packages of the
// values n writes out as a composite literal of another package's type,
// or the address of one.
func typePackages(f goFile, n ast.Node) map[string]string {
	out := map[string]string{}
	ast.Inspect(n, func(n ast.Node) bool {
		var lhs []ast.Expr
		var rhs []ast.Expr
		switch n := n.(type) {
		case *ast.AssignStmt:
			lhs, rhs = n.Lhs, n.Rhs
		case *ast.ValueSpec:
			for _, name := range n.Names {
				lhs = append(lhs, name)
			}
			rhs = n.Values
		}
		for i, l := range lhs {
			if id, ok := l.(*ast.Ident); ok && len(lhs) == len(rhs) {
				if pkg := typePackage(f, rhs[i]); pkg != "" {
					out[id.Name] = pkg
				}
			}
		}
		return true
	})
	return out
}

// typePackage is the module package of the type of x when x writes out a
// value of another package's type, or its address; else "".
func typePackage(f goFile, x ast.Expr) string {
	for {
		switch y := x.(type) {
		case *ast.ParenExpr:
			x = y.X
			continue
		case *ast.UnaryExpr:
			x = y.X
			continue
		case *ast.CompositeLit:
			if name := moduleName(f, y.Type); name != "" && !strings.HasPrefix(name, f.pkg+".") {
				return name[:strings.LastIndex(name, ".")]
			}
		}
		return ""
	}
}

// calleeOf is the function or method of the module a call calls by name:
// a function of f's package or of a package of the module f imports, a
// method on a value of another package's type the call or locals shows,
// or else a method of f's package, which a call on any value but a
// package may name; "" for a call of no name.
func calleeOf(f goFile, call *ast.CallExpr, locals map[string]string) string {
	switch fn := call.Fun.(type) {
	case *ast.Ident:
		return f.pkg + "." + fn.Name
	case *ast.SelectorExpr:
		if name := moduleName(f, fn); name != "" && !strings.HasPrefix(name, f.pkg+".") {
			return name
		}
		if pkg := typePackage(f, fn.X); pkg != "" {
			return pkg + ".(method)." + fn.Sel.Name
		}
		if id, ok := fn.X.(*ast.Ident); ok && locals[id.Name] != "" {
			return locals[id.Name] + ".(method)." + fn.Sel.Name
		}
		return f.pkg + ".(method)." + fn.Sel.Name
	}
	return ""
}

// callees are the functions and methods of the module that n, of f,
// calls by name, as calleeOf reads each call.
func callees(f goFile, n ast.Node) []string {
	locals := typePackages(f, n)
	var out []string
	ast.Inspect(n, func(n ast.Node) bool {
		if call, ok := n.(*ast.CallExpr); ok {
			if key := calleeOf(f, call, locals); key != "" {
				out = append(out, key)
			}
		}
		return true
	})
	return out
}

// committing says, of each function and method of the module that
// commits, clones, fetches or pushes, how: it starts git, itself or
// through what it calls, and names one of backgroundCommands among its
// words, or it calls one that does. Calls are followed by name only.
func committing(src gitSource, decls map[string][]declared) map[string]string {
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
			for _, s := range bodyWords(resolver{src, d.f, d.decl.Body}, d.decl.Body) {
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
	how := committing(gitSourceOf(files), decls)
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

// nullsGlobal says whether n gives git an environment in which it reads no
// global configuration: GIT_CONFIG_GLOBAL=/dev/null, written out or with
// os.DevNull.
func nullsGlobal(f goFile, n ast.Node) bool {
	os := f.imports()["os"]
	found := false
	ast.Inspect(n, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.BasicLit:
			if s, ok := literal(x); ok && s == "GIT_CONFIG_GLOBAL=/dev/null" {
				found = true
			}
		case *ast.BinaryExpr:
			s, ok := literal(x.X)
			sel, dev := x.Y.(*ast.SelectorExpr)
			if x.Op == token.ADD && ok && s == "GIT_CONFIG_GLOBAL=" && dev && sel.Sel.Name == "DevNull" {
				if id, ok := sel.X.(*ast.Ident); ok && id.Name == os {
					found = true
				}
			}
		}
		return !found
	})
	return found
}

// isolating lists the functions and methods of the module that start git
// in an environment in which it reads no global configuration, given in
// their own body or in that of a function they call.
func isolating(src gitSource, decls map[string][]declared) map[string]bool {
	nulls := map[string]bool{}
	for key, ds := range decls {
		for _, d := range ds {
			if nullsGlobal(d.f, d.decl.Body) {
				nulls[key] = true
			}
		}
	}
	out := map[string]bool{}
	for key, ds := range decls {
		for _, d := range ds {
			if len(gitCalls(src, d.f, d.decl.Body)) > 0 && (nulls[key] || slices.ContainsFunc(callees(d.f, d.decl.Body), func(k string) bool { return nulls[k] })) {
				out[key] = true
			}
		}
	}
	return out
}

// configValues reads a configuration's text into values, by key in lower
// case, a later value of a key taking an earlier one's place, as git reads
// it; a key with no value is true.
func configValues(text string, values map[string]string) {
	section := ""
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "[") {
			section = ""
			if end := strings.Index(line, "]"); end > 0 {
				section = strings.ToLower(strings.TrimSpace(line[1:end]))
			}
			continue
		}
		if line == "" || line[0] == '#' || line[0] == ';' || section == "" {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			v = "true"
		}
		values[section+"."+strings.ToLower(strings.TrimSpace(k))] = strings.ToLower(strings.Trim(strings.TrimSpace(v), `"`))
	}
}

// dashCSetting is a setting git's -c takes: a key, with or without a value.
var dashCSetting = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9-]*(\.[^=\s]+)*\.[A-Za-z][A-Za-z0-9-]*(=|$)`)

// dashC reads the settings words give git with -c into values, and says
// whether they give any.
func dashC(words []string, values map[string]string) bool {
	found := false
	for i := 0; i+1 < len(words); i++ {
		if words[i] == "-c" && dashCSetting.MatchString(words[i+1]) {
			k, v, ok := strings.Cut(words[i+1], "=")
			if !ok {
				v = "true"
			}
			values[strings.ToLower(k)] = strings.ToLower(v)
			found = true
		}
	}
	return found
}

// configWords is the key and value words set in the global configuration
// as git config --global <key> <value> does, with no other option.
func configWords(words []string) (key, value string, ok bool) {
	i := slices.Index(words, "config")
	if i < 0 || !slices.Contains(words[i+1:], "--global") {
		return "", "", false
	}
	rest := slices.DeleteFunc(slices.Clone(words[i+1:]), func(w string) bool { return w == "--global" })
	if len(rest) != 2 || strings.HasPrefix(rest[0], "-") || strings.HasPrefix(rest[1], "-") {
		return "", "", false
	}
	return strings.ToLower(rest[0]), strings.ToLower(rest[1]), true
}

// background are the settings that turn git's background work off, each
// with the values git reads as off.
var background = []struct {
	key string
	off []string
}{
	{"gc.auto", []string{"0"}},
	{"maintenance.auto", []string{"false", "no", "off", "0"}},
}

// notOff lists the settings values leaves on, as key=off-value, and those
// it turns on.
func notOff(values map[string]string) (missing, on []string) {
	for _, s := range background {
		v, set := values[s.key]
		switch {
		case !set:
			missing = append(missing, s.key+"="+s.off[0])
		case !slices.Contains(s.off, v):
			missing = append(missing, s.key+"="+s.off[0])
			on = append(on, s.key+"="+v)
		}
	}
	return missing, on
}

// siteKind is how a test file gives git configuration.
type siteKind int

const (
	writes   siteKind = iota // writes the global configuration whole
	adds                     // adds to it
	setenv                   // sets GIT_CONFIG_GLOBAL
	flags                    // gives git settings with -c
	isolated                 // commits through a function whose git reads no global configuration
)

// configSite is one place a test file gives git configuration.
type configSite struct {
	kind   siteKind
	how    string            // where and how
	fn     *ast.FuncDecl     // the function it is in
	path   string            // the configuration's path, as its source, for writes and setenv
	values map[string]string // the settings it gives
}

// rigSites are the places test file f gives git configuration, globals
// being the paths its package's tests set GIT_CONFIG_GLOBAL to, as their
// source, and isolates the functions whose git reads no global
// configuration.
func rigSites(src gitSource, f goFile, globals, isolates map[string]bool) []configSite {
	os := f.imports()["os"]
	// getenv says whether x is os.Getenv("GIT_CONFIG_GLOBAL").
	getenv := func(x ast.Expr) bool {
		call, ok := x.(*ast.CallExpr)
		if !ok || len(call.Args) != 1 || !isCallOf(call, os, "Getenv") {
			return false
		}
		s, _ := literal(call.Args[0])
		return s == "GIT_CONFIG_GLOBAL"
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
	var sites []configSite
	for _, decl := range f.file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Body == nil {
			continue
		}
		r := resolver{src, f, fn.Body}
		var setenvs []string
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			if call, ok := n.(*ast.CallExpr); ok {
				if v, ok := setsGlobal(call); ok {
					setenvs = append(setenvs, v)
					sites = append(sites, configSite{setenv, f.line(call) + " sets GIT_CONFIG_GLOBAL", fn, v, nil})
				}
			}
			return true
		})
		// global is the index of call's argument that is the global
		// configuration's path, or -1.
		global := func(call *ast.CallExpr) int {
			return slices.IndexFunc(call.Args, func(x ast.Expr) bool {
				id, ok := x.(*ast.Ident)
				s := types.ExprString(x)
				return getenv(x) || ok && fromEnv[id.Name] || globals[s] || slices.Contains(setenvs, s)
			})
		}
		// Names the function gives what it reads back from the global
		// configuration: a write that holds one adds to what was there.
		readBack := map[string]bool{}
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			if as, ok := n.(*ast.AssignStmt); ok && len(as.Rhs) == 1 {
				if call, ok := as.Rhs[0].(*ast.CallExpr); ok && callName(call) == "ReadFile" && global(call) >= 0 {
					if id, ok := as.Lhs[0].(*ast.Ident); ok {
						readBack[id.Name] = true
					}
				}
			}
			return true
		})
		gits := gitCalls(src, f, fn.Body)
		locals := typePackages(f, fn.Body)
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			name := callName(call)
			switch i := global(call); {
			case i < 0:
			case name == "OpenFile" || name == "Create":
				kind := writes
				if strings.Contains(types.ExprString(call), "O_APPEND") {
					kind = adds
				}
				values := map[string]string{}
				configValues(handleText(r, fn.Body, call), values)
				sites = append(sites, configSite{kind, f.line(call) + " writes the file GIT_CONFIG_GLOBAL names", fn, types.ExprString(call.Args[i]), values})
			case strings.Contains(strings.ToLower(name), "write"):
				kind := writes
				var text strings.Builder
				for j, a := range call.Args {
					if j == i {
						continue
					}
					text.WriteString(r.text(a, 0))
					ast.Inspect(a, func(n ast.Node) bool {
						if id, ok := n.(*ast.Ident); ok && readBack[id.Name] {
							kind = adds
						}
						return true
					})
				}
				values := map[string]string{}
				configValues(text.String(), values)
				sites = append(sites, configSite{kind, f.line(call) + " writes the file GIT_CONFIG_GLOBAL names", fn, types.ExprString(call.Args[i]), values})
			}
			if id, ok := call.Fun.(*ast.Ident); ok && id.Name == "append" {
				return true
			}
			words := r.argWords(call)
			if k, v, ok := configWords(words); ok {
				sites = append(sites, configSite{adds, f.line(call) + " sets " + k + " with git config --global", fn, "", map[string]string{k: v}})
			}
			values := map[string]string{}
			given := dashC(words, values)
			commits := slices.ContainsFunc(words, func(w string) bool { return slices.Contains(backgroundCommands, w) })
			switch callee := calleeOf(f, call, locals); {
			case isolates[callee] && commits:
				sites = append(sites, configSite{isolated, f.line(call) + " commits, clones, fetches or pushes through " + callee + ", whose git reads no global configuration", fn, "", values})
			case given && (slices.Contains(gits, call) || isolates[callee]):
				sites = append(sites, configSite{flags, f.line(call) + " gives its git settings with -c", fn, "", values})
			}
			return true
		})
	}
	return sites
}

// handleText is what fn's body writes to the file call opens: the text
// given to the Write and WriteString methods of the name the call's
// result is given, or after it to fmt.Fprint, Fprintf, Fprintln and
// io.WriteString.
func handleText(r resolver, body ast.Node, open *ast.CallExpr) string {
	handle := ""
	ast.Inspect(body, func(n ast.Node) bool {
		if as, ok := n.(*ast.AssignStmt); ok && len(as.Rhs) == 1 && as.Rhs[0] == open {
			if id, ok := as.Lhs[0].(*ast.Ident); ok {
				handle = id.Name
			}
		}
		return handle == ""
	})
	if handle == "" {
		return ""
	}
	isHandle := func(x ast.Expr) bool {
		id, ok := x.(*ast.Ident)
		return ok && id.Name == handle
	}
	var b strings.Builder
	ast.Inspect(body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		if sel, ok := call.Fun.(*ast.SelectorExpr); ok && isHandle(sel.X) && strings.HasPrefix(sel.Sel.Name, "Write") {
			for _, a := range call.Args {
				b.WriteString(r.text(a, 0))
			}
		} else if name := callName(call); (strings.HasPrefix(name, "Fprint") || name == "WriteString") && len(call.Args) > 0 && isHandle(call.Args[0]) {
			for _, a := range call.Args[1:] {
				b.WriteString(r.text(a, 0))
			}
		}
		return true
	})
	return b.String()
}

// checkGitRigs holds the test files of the packages whose tests commit,
// clone, fetch or push to git's background work off, as the comment at
// the top of this file says, unless a file is in listed with its reason.
func checkGitRigs(files []goFile, listed map[string]string) []string {
	var problems []string
	src := gitSourceOf(files)
	scope := backgroundScope(files)
	isolates := isolating(src, declsOf(files))
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
	// The sites of each test file in scope, and the packages one of whose
	// test files sets GIT_CONFIG_GLOBAL and writes that file, in the same
	// function, with both settings off.
	sitesOf := map[string][]configSite{}
	protected := map[string]bool{}
	var rigFiles []goFile
	for _, f := range files {
		if !f.test || scope[f.pkg] == "" {
			continue
		}
		sites := rigSites(src, f, globals[f.pkg], isolates)
		if len(sites) == 0 {
			continue
		}
		sitesOf[f.path] = sites
		rigFiles = append(rigFiles, f)
		for _, s := range sites {
			if s.kind != setenv {
				continue
			}
			for _, w := range sites {
				if missing, _ := notOff(w.values); w.kind == writes && w.fn == s.fn && w.path == s.path && len(missing) == 0 {
					protected[f.pkg] = true
				}
			}
		}
	}
	for _, f := range rigFiles {
		sites := sitesOf[f.path]
		ownGlobal := slices.ContainsFunc(sites, func(s configSite) bool { return s.kind == writes || s.kind == setenv })
		var found []string
		for _, s := range sites {
			missing, on := notOff(s.values)
			why := ""
			switch s.kind {
			case writes, isolated:
				if len(missing) > 0 {
					why = "it does not set " + strings.Join(missing, " or ")
				}
			case adds:
				if len(on) > 0 {
					why = "it turns " + strings.Join(on, " and ") + " back on"
				}
			case flags:
				switch {
				case len(on) > 0:
					why = "it turns " + strings.Join(on, " and ") + " back on"
				case len(missing) > 0 && !ownGlobal && !protected[f.pkg]:
					why = "it does not set " + strings.Join(missing, " or ") + ", and no test file of its package writes a global configuration that does"
				}
			case setenv:
				paired := slices.ContainsFunc(sites, func(w configSite) bool { return w.kind == writes && w.fn == s.fn && w.path == s.path })
				values := map[string]string{}
				for _, w := range sites {
					if w.kind == adds && w.fn == s.fn && w.path == "" {
						maps.Copy(values, w.values)
					}
				}
				if missing, _ := notOff(values); !paired && len(missing) > 0 {
					why = "the function that sets it neither writes that file nor sets " + strings.Join(missing, " and ") + " with git config --global"
				}
			}
			if why != "" {
				found = append(found, fmt.Sprintf("%s: %s, and the tests of %s commit, clone, fetch or push (%s), but %s", f.path, s.how, f.pkg, scope[f.pkg], why))
			}
		}
		_, isListed := listed[f.path]
		switch {
		case isListed && len(found) == 0:
			problems = append(problems, fmt.Sprintf("%s turns git's background work off, yet is listed as leaving it on", f.path))
		case !isListed:
			problems = append(problems, found...)
		}
	}
	for _, path := range slices.Sorted(maps.Keys(listed)) {
		if sitesOf[path] == nil {
			problems = append(problems, fmt.Sprintf("%s is listed as leaving git's background work on, but gives git no configuration in a package whose tests commit, clone, fetch or push", path))
		}
	}
	return problems
}

// backgroundOn are the test files that give git a configuration of their
// own in a package whose tests commit, clone, fetch or push, yet do not
// turn git's background work off in it themselves, each with its reason.
var backgroundOn = map[string]string{
	"internal/store/migrate/helpers_test.go":       "isolate writes an empty global config; every git the tests run is given gc.auto=0 and maintenance.auto=false with -c, the program's (git.go) and the tests' gitT alike, but execGit, whose one commit (run_test.go) fails by design",
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
