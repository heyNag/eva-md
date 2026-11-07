package main

import (
	"bytes"
	"context"
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

// readmeSynopsis and designSynopsis match a command's synopsis in README's
// Commands list and in design.md's command table.
var (
	readmeSynopsis = regexp.MustCompile("^- `eva-md ([^`]*)`")
	designSynopsis = regexp.MustCompile("^\\| `eva-md ([^`]*)` \\|")
)

// repoDoc reads one of the checkout's own docs, by its path from the repo
// root.
func repoDoc(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", filepath.FromSlash(name)))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// synopses maps each command whose synopsis starts a line of doc, as line
// matches it, to the flags the synopsis shows. A command is named by its
// words up to its first argument or flag, as update or inbox clear.
func synopses(doc string, line *regexp.Regexp) map[string][]string {
	word := regexp.MustCompile(`^[a-z]+$`)
	flag := regexp.MustCompile(`--[a-z][a-z-]*`)
	out := map[string][]string{}
	for _, l := range strings.Split(doc, "\n") {
		m := line.FindStringSubmatch(l)
		if m == nil {
			continue
		}
		var name []string
		for _, w := range strings.Fields(m[1]) {
			if !word.MatchString(w) {
				break
			}
			name = append(name, w)
		}
		command := strings.Join(name, " ")
		out[command] = append(out[command], flag.FindAllString(m[1], -1)...)
	}
	return out
}

// design.md names as built the commands the usage line lists, inbox among
// them, and says what the planned ones print: those it names before "print
// inbox's usage", inbox's usage, and those after "any other command",
// "unknown command", each exiting 2. The commands run are the ones the
// sentence names, so the test fails when it says otherwise.
func TestTheDesignNamesTheBuiltCommands(t *testing.T) {
	isolated(t)
	_, built, ok := strings.Cut(repoDoc(t, "docs/design.md"), "\nThe built commands are ")
	if !ok {
		t.Fatal("design.md names no built commands")
	}
	sentence, _, _ := strings.Cut(built, ";")
	named := strings.Split(strings.Replace(sentence, " and ", ", ", 1), ", ")
	var out, errOut bytes.Buffer
	if code := run(context.Background(), nil, strings.NewReader(""), &out, &errOut); code != 2 {
		t.Fatal(code)
	}
	listed := strings.Fields(strings.NewReplacer("usage: eva-md", "", "|", " ").Replace(errOut.String()))
	slices.Sort(named)
	slices.Sort(listed)
	if !slices.Equal(named, listed) {
		t.Fatalf("design.md names %q as built, the usage line %q", named, listed)
	}
	_, planned, ok := strings.Cut(built, "The planned ")
	usage, unknown, ok2 := strings.Cut(planned, " print inbox's usage and exit 2; any other command, ")
	unknown, _, ok3 := strings.Cut(unknown, ` prints "unknown command" and exits 2.`)
	names := regexp.MustCompile("`([a-z ]+)`")
	usages, unknowns := names.FindAllStringSubmatch(usage, -1), names.FindAllStringSubmatch(unknown, -1)
	if !ok || !ok2 || !ok3 || len(usages) == 0 || len(unknowns) == 0 {
		t.Fatalf("design.md doesn't say what the planned commands print: %.200q", planned)
	}
	for _, c := range append(usages, unknowns...) {
		args := strings.Fields(c[1])
		var out, errOut bytes.Buffer
		code := run(context.Background(), args, strings.NewReader(""), &out, &errOut)
		inbox := strings.HasPrefix(errOut.String(), "eva-md: inbox takes clear: ")
		wantInbox := slices.ContainsFunc(usages, func(u []string) bool { return u[1] == c[1] })
		if code != 2 || out.Len() != 0 || inbox != wantInbox || strings.Contains(errOut.String(), "unknown command") == wantInbox {
			t.Errorf("eva-md %s: %d %q %q", c[1], code, out.String(), errOut.String())
		}
	}
}

// design.md's command table, the source of truth, shows every flag README's
// Commands list shows for a command, as --sweep for update.
func TestTheDesignShowsEveryFlagTheReadmeShows(t *testing.T) {
	readme := synopses(repoDoc(t, "README.md"), readmeSynopsis)
	design := synopses(repoDoc(t, "docs/design.md"), designSynopsis)
	if !slices.Contains(readme["update"], "--pending") || !slices.Contains(design["update"], "--pending") {
		t.Fatalf("no synopsis of update read: README %q, design.md %q", readme["update"], design["update"])
	}
	for command, flags := range readme {
		table, ok := design[command]
		if !ok {
			t.Errorf("design.md's command table has no row for eva-md %s", command)
			continue
		}
		for _, f := range flags {
			if !slices.Contains(table, f) {
				t.Errorf("README shows eva-md %s %s, and design.md's command table doesn't", command, f)
			}
		}
	}
}

// AGENTS.md's Layout names every source file of the CLI, so an agent finds
// where a command lives, as inbox.go holds eva-md inbox clear.
func TestAgentsNamesEveryFileOfTheCLI(t *testing.T) {
	var layout string
	for _, l := range strings.Split(repoDoc(t, "AGENTS.md"), "\n") {
		if strings.HasPrefix(l, "- `cmd/eva-md`: ") {
			layout = l
		}
	}
	files, err := filepath.Glob("*.go")
	if layout == "" || err != nil || len(files) == 0 {
		t.Fatalf("AGENTS.md's line on cmd/eva-md %q, files %q %v", layout, files, err)
	}
	for _, f := range files {
		if !strings.HasSuffix(f, "_test.go") && !strings.Contains(layout, "`"+f+"`") {
			t.Errorf("AGENTS.md's Layout doesn't name cmd/eva-md/%s", f)
		}
	}
}

// dontRun returns the items of AGENTS.md's bold list of what not to run
// against the real machine, as uninstall or inbox clear --apply: the list is
// split on its commas and on "or", so the uninstall script names no
// uninstall of its own.
func dontRun(dont string) map[string]bool {
	list, ok := strings.CutPrefix(dont, "Don't run ")
	list, ok2 := strings.CutSuffix(list, " against the real machine")
	if !ok || !ok2 {
		return nil
	}
	items := map[string]bool{}
	for _, item := range strings.Split(list, ", ") {
		for _, part := range strings.Split(item, " or ") {
			items[part] = true
		}
	}
	return items
}

// A command README shows with --apply changes nothing without it, as
// uninstall and inbox clear: AGENTS.md's rule names it, or it with --apply,
// as an item of what not to run against the real machine, and its run
// without --apply among the previews. A list missing the item fails, even
// when another item holds the word, as "the install or uninstall scripts".
func TestAgentsPreviewsWhatTakesApply(t *testing.T) {
	var rule string
	for _, l := range strings.Split(repoDoc(t, "AGENTS.md"), "\n") {
		if strings.HasPrefix(l, "- **Don't run ") {
			rule = l
		}
	}
	dont, previews, ok := strings.Cut(strings.TrimPrefix(rule, "- **"), "**")
	readme := synopses(repoDoc(t, "README.md"), readmeSynopsis)
	items := dontRun(dont)
	if !ok || items == nil || !slices.Contains(readme["uninstall"], "--apply") {
		t.Fatalf("AGENTS.md's rule %q, README's uninstall %q", rule, readme["uninstall"])
	}
	for _, drop := range []string{"uninstall, ", "inbox clear --apply, "} {
		cut := strings.Replace(dont, drop, "", 1)
		if gone := strings.TrimSuffix(drop, ", "); cut == dont || dontRun(cut)[gone] {
			t.Errorf("a list without %q still names it", gone)
		}
	}
	for command, flags := range readme {
		if !slices.Contains(flags, "--apply") {
			continue
		}
		if !items[command] && !items[command+" --apply"] {
			t.Errorf("AGENTS.md doesn't say not to run eva-md %s against the real machine", command)
		}
		if !strings.Contains(previews, "`"+command+"` without `--apply`") {
			t.Errorf("AGENTS.md doesn't give eva-md %s without --apply as a preview", command)
		}
	}
}

// verification.md says how many lines the redaction test that keeps
// ordinary text keeps as they are; that is the test's own count.
func TestVerificationCountsTheKeptLines(t *testing.T) {
	m := regexp.MustCompile(`([0-9]+) lines kept as they are`).FindStringSubmatch(repoDoc(t, "docs/verification.md"))
	f, err := parser.ParseFile(token.NewFileSet(), filepath.Join("..", "..", "internal", "redact", "passwords_test.go"), nil, 0)
	if m == nil || err != nil {
		t.Fatalf("verification.md's count %q, the test file %v", m, err)
	}
	count := -1
	for _, d := range f.Decls {
		if fn, ok := d.(*ast.FuncDecl); ok && fn.Name.Name == "TestPasswordShapesKept" {
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				if r, ok := n.(*ast.RangeStmt); ok && count < 0 {
					if lit, ok := r.X.(*ast.CompositeLit); ok {
						count = len(lit.Elts)
					}
				}
				return count < 0
			})
		}
	}
	if strconv.Itoa(count) != m[1] {
		t.Fatalf("verification.md says %s lines kept as they are; TestPasswordShapesKept keeps %d", m[1], count)
	}
}

// Each bullet of verification.md that names its packages in brackets after
// its title, as - **Search** (`cmd/eva-md`, `internal/resume`), cites only
// tests that exist, each defined in one of those packages, so a reader can
// run what the bullet describes.
func TestVerificationBulletsNameTheirPackages(t *testing.T) {
	root := filepath.Join("..", "..")
	defined := map[string][]string{} // a test's name, the packages that define it
	testFunc := regexp.MustCompile(`(?m)^func ((?:Test|Benchmark|Fuzz)[A-Za-z0-9_]*)\(`)
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		switch {
		case err != nil:
			return err
		case d.IsDir() && path != root && strings.HasPrefix(d.Name(), "."):
			return filepath.SkipDir
		case d.IsDir() || !strings.HasSuffix(path, "_test.go"):
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		pkg, _ := filepath.Rel(root, filepath.Dir(path))
		for _, m := range testFunc.FindAllStringSubmatch(string(data), -1) {
			defined[m[1]] = append(defined[m[1]], filepath.ToSlash(pkg))
		}
		return nil
	})
	if err != nil || len(defined["TestVerificationBulletsNameTheirPackages"]) != 1 {
		t.Fatalf("reading the tests: %v", err)
	}
	bullet := regexp.MustCompile("^- \\*\\*([^*]+)\\*\\* \\(([^)]*)\\)")
	cited := regexp.MustCompile("`((?:Test|Benchmark|Fuzz)[A-Za-z0-9_]*)`")
	quoted := regexp.MustCompile("`([^`]+)`")
	read := 0
	for _, l := range strings.Split(repoDoc(t, "docs/verification.md"), "\n") {
		m := bullet.FindStringSubmatch(l)
		if m == nil {
			continue
		}
		read++
		var pkgs []string
		for _, q := range quoted.FindAllStringSubmatch(m[2], -1) {
			pkgs = append(pkgs, q[1])
		}
		for _, c := range cited.FindAllStringSubmatch(l, -1) {
			where := defined[c[1]]
			switch {
			case len(where) == 0:
				t.Errorf("%s cites %s, which no test file defines", m[1], c[1])
			case !slices.ContainsFunc(where, func(p string) bool { return slices.Contains(pkgs, p) }):
				t.Errorf("%s cites %s, defined in %v, which its packages %v leave out", m[1], c[1], where, pkgs)
			}
		}
	}
	if read < 10 {
		t.Fatalf("read %d bullets of verification.md", read)
	}
}
