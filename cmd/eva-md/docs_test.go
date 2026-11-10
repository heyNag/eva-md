package main

import (
	"bytes"
	"context"
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// readmeSynopsis matches a command's synopsis in README's Commands list.
var readmeSynopsis = regexp.MustCompile("^- `eva-md ([^`]*)`")

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

// usageBeforeTheTable is what eva-md printed with no arguments before
// commands registered themselves in the command table, byte for byte.
const usageBeforeTheTable = "usage: eva-md setup | version | upgrade | init | install | uninstall | doctor | status | brief | show | search | index | inbox | update | commit | maintain | system | validate | hook | mcp\n"

// eva-md with no arguments prints the usage line the command table builds,
// the same bytes it printed before the table, on stderr, and exits 2.
func TestUsageLineIsUnchanged(t *testing.T) {
	isolated(t)
	var out, errOut bytes.Buffer
	code := run(context.Background(), nil, strings.NewReader(""), &out, &errOut)
	if code != 2 || out.Len() != 0 || errOut.String() != usageBeforeTheTable {
		t.Fatalf("eva-md: %d %q %q, want 2 and %q", code, out.String(), errOut.String(), usageBeforeTheTable)
	}
}

// The usage line lists exactly the command table's commands. What the
// planned commands print is held here, not read from prose: inbox list and
// inbox show print inbox's usage, and migrate, like any command the table
// lacks, "unknown command", each exiting 2 with nothing on stdout.
func TestTheDesignNamesTheBuiltCommands(t *testing.T) {
	isolated(t)
	var out, errOut bytes.Buffer
	if code := run(context.Background(), nil, strings.NewReader(""), &out, &errOut); code != 2 {
		t.Fatal(code)
	}
	listed := strings.Fields(strings.NewReplacer("usage: eva-md", "", "|", " ").Replace(errOut.String()))
	var names []string
	for _, c := range commands {
		names = append(names, c.name)
	}
	slices.Sort(listed)
	slices.Sort(names)
	if len(names) == 0 || !slices.Equal(listed, names) {
		t.Fatalf("the usage line lists %q, the table %q", listed, names)
	}
	for _, args := range [][]string{{"inbox", "list"}, {"inbox", "show"}, {"inbox", "show", "3f2a91c0"}} {
		var out, errOut bytes.Buffer
		code := run(context.Background(), args, strings.NewReader(""), &out, &errOut)
		if code != 2 || out.Len() != 0 || !strings.HasPrefix(errOut.String(), "eva-md: inbox takes clear: ") {
			t.Errorf("eva-md %s: %d %q %q", strings.Join(args, " "), code, out.String(), errOut.String())
		}
	}
	for _, args := range [][]string{{"migrate"}, {"migrate", "--apply"}, {"list"}, {"Setup"}, {"--help"}, {""}} {
		var out, errOut bytes.Buffer
		code := run(context.Background(), args, strings.NewReader(""), &out, &errOut)
		want := "eva-md: unknown command " + strconv.Quote(args[0]) + "; run eva-md with no arguments for the list\n"
		if code != 2 || out.Len() != 0 || errOut.String() != want {
			t.Errorf("eva-md %s: %d %q %q", strings.Join(args, " "), code, out.String(), errOut.String())
		}
	}
}

// eva-md hook runs before the command table is looked at: with a table
// that has no hook, or one whose hook fails the test, a Stop is still
// captured and answered.
func TestTheHookSkipsTheTable(t *testing.T) {
	d := isolated(t)
	cwd := project(t)
	saved := commands
	t.Cleanup(func() { commands = saved })
	walked := command{name: "hook", file: "main.go", place: 190, synopsis: []string{"eva-md hook"},
		run: func(context.Context, []string, io.Reader, io.Writer, io.Writer) int {
			t.Error("the hook was dispatched through the table")
			return 1
		}}
	for i, table := range [][]command{nil, {walked}} {
		commands = table
		session := "table-" + strconv.Itoa(i)
		payload, _ := json.Marshal(map[string]any{"hook_event_name": "Stop", "session_id": session, "cwd": cwd, "last_assistant_message": "Finished the fixture."})
		var out, errOut bytes.Buffer
		code := run(context.Background(), []string{"hook", "--host", "codex"}, bytes.NewReader(payload), &out, &errOut)
		if code != 0 || out.String() != "{}" || errOut.Len() != 0 {
			t.Fatalf("hook with table %d: %d %q %q", i, code, out.String(), errOut.String())
		}
		if files, _ := filepath.Glob(filepath.Join(d.Inbox(), "*", "codex-*"+session+"*.jsonl")); len(files) != 1 {
			t.Fatalf("hook with table %d captured %q", i, files)
		}
	}
}

// Each command of the table has a name of its own, a place of its own in
// the usage line, a synopsis for each form that starts with eva-md and its
// name, and a handler defined in the file the entry names, so that file is
// where the command lives.
func TestTheCommandTableIsWellFormed(t *testing.T) {
	if len(commands) == 0 {
		t.Fatal("no command registered")
	}
	names, places := map[string]bool{}, map[int]bool{}
	for _, c := range commands {
		if c.name == "" || names[c.name] {
			t.Errorf("command %q is unnamed or registered twice", c.name)
		}
		if places[c.place] {
			t.Errorf("%s shares place %d in the usage line", c.name, c.place)
		}
		names[c.name], places[c.place] = true, true
		if len(c.synopsis) == 0 {
			t.Errorf("%s has no synopsis", c.name)
		}
		for _, s := range c.synopsis {
			if s != "eva-md "+c.name && !strings.HasPrefix(s, "eva-md "+c.name+" ") {
				t.Errorf("%s has the synopsis %q", c.name, s)
			}
		}
		if c.run == nil {
			t.Errorf("%s has no handler", c.name)
			continue
		}
		pc := reflect.ValueOf(c.run).Pointer()
		if file, _ := runtime.FuncForPC(pc).FileLine(pc); filepath.Base(file) != c.file {
			t.Errorf("%s names %s, and its handler is in %s", c.name, c.file, file)
		}
	}
}

// tableSynopses maps each command whose synopsis the table holds, named as
// synopses names one, as inbox clear, to the flags its synopses show.
func tableSynopses(table []command) map[string][]string {
	var lines []string
	for _, c := range table {
		for _, s := range c.synopsis {
			lines = append(lines, "- `"+s+"`")
		}
	}
	return synopses(strings.Join(lines, "\n"), readmeSynopsis)
}

// synopsisGaps says, sorted, which commands README shows that the table
// lacks, which flags README shows for a command that the table's synopses
// of it don't, and which commands the table holds that README's Commands
// list doesn't show, so no command goes undocumented.
func synopsisGaps(readme, table map[string][]string) []string {
	var gaps []string
	for command := range table {
		if _, ok := readme[command]; !ok {
			gaps = append(gaps, "the command table holds eva-md "+command+", which README's Commands list doesn't show")
		}
	}
	for command, flags := range readme {
		shown, ok := table[command]
		if !ok {
			gaps = append(gaps, "README shows eva-md "+command+", which the command table lacks")
			continue
		}
		for _, f := range flags {
			if !slices.Contains(shown, f) {
				gaps = append(gaps, "README shows eva-md "+command+" "+f+", and the command table's synopsis doesn't")
			}
		}
	}
	slices.Sort(gaps)
	return gaps
}

// Every command README's Commands list shows is in the command table, every
// flag README shows for it is in the table's synopsis of it, as --sweep for
// update, and every command the table holds is in README's Commands list.
func TestTheDesignShowsEveryFlagTheReadmeShows(t *testing.T) {
	readme := synopses(repoDoc(t, "README.md"), readmeSynopsis)
	table := tableSynopses(commands)
	if !slices.Contains(readme["update"], "--pending") || !slices.Contains(table["update"], "--pending") {
		t.Fatalf("no synopsis of update read: README %q, the table %q", readme["update"], table["update"])
	}
	for _, gap := range synopsisGaps(readme, table) {
		t.Error(gap)
	}
}

// unnamedFiles returns the source files, other than tests, that neither a
// command of table nor the support files name.
func unnamedFiles(files []string, table []command, support []struct{ file, holds string }) []string {
	named := map[string]bool{}
	for _, c := range table {
		named[c.file] = true
	}
	for _, s := range support {
		named[s.file] = true
	}
	var gaps []string
	for _, f := range files {
		if strings.HasSuffix(f, ".go") && !strings.HasSuffix(f, "_test.go") && !named[f] {
			gaps = append(gaps, f)
		}
	}
	return gaps
}

// supportPointerGaps says which support files hold part of a command of
// table, naming it as eva-md and its name, as code.go names eva-md system
// status, without naming the file of that command's entry, which dispatches
// to it, so an agent would not find the entry from the phrase.
func supportPointerGaps(table []command, support []struct{ file, holds string }) []string {
	var gaps []string
	for _, s := range support {
		for _, c := range table {
			names := regexp.MustCompile(`\beva-md ` + regexp.QuoteMeta(c.name) + `\b`)
			if names.MatchString(s.holds) && !strings.Contains(s.holds, c.file) {
				gaps = append(gaps, s.file+" holds part of eva-md "+c.name+" and doesn't name "+c.file)
			}
		}
	}
	return gaps
}

// The command table names every source file of the CLI, by the command it
// holds or in its list of the files that support them, each with what it
// holds, so an agent finds where a command lives, as inbox.go holds eva-md
// inbox clear. Every file either names is one of the CLI, a support file
// holds no command's entry and is listed once, one that holds part of a
// command names the file of its entry, and AGENTS.md's Layout points at the
// table.
func TestAgentsNamesEveryFileOfTheCLI(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil || !slices.Contains(files, "table.go") {
		t.Fatalf("files %q %v", files, err)
	}
	for _, f := range unnamedFiles(files, commands, supportFiles) {
		t.Errorf("neither a command of the table nor its support files name cmd/eva-md/%s", f)
	}
	for _, c := range commands {
		if !slices.Contains(files, c.file) {
			t.Errorf("%s names %s, which isn't a file of the CLI", c.name, c.file)
		}
	}
	listed := map[string]bool{}
	for _, s := range supportFiles {
		switch {
		case !slices.Contains(files, s.file) || strings.HasSuffix(s.file, "_test.go"):
			t.Errorf("the support files name %s, which isn't a source file of the CLI", s.file)
		case listed[s.file] || s.holds == "":
			t.Errorf("the support files list %s twice or without what it holds", s.file)
		case slices.ContainsFunc(commands, func(c command) bool { return c.file == s.file }):
			t.Errorf("the support files list %s, which holds a command", s.file)
		}
		listed[s.file] = true
	}
	for _, gap := range supportPointerGaps(commands, supportFiles) {
		t.Error(gap)
	}
	if !slices.ContainsFunc(supportFiles, func(s struct{ file, holds string }) bool {
		return s.file == "code.go" && strings.Contains(s.holds, "eva-md system status")
	}) {
		t.Error("the support files don't say that code.go holds eva-md system status")
	}
	var layout string
	for _, l := range strings.Split(repoDoc(t, "AGENTS.md"), "\n") {
		if strings.HasPrefix(l, "- `cmd/eva-md`: ") {
			layout = l
		}
	}
	if !strings.Contains(layout, "`table.go`") {
		t.Errorf("AGENTS.md's line on cmd/eva-md doesn't point at table.go: %q", layout)
	}
}

// The document checks fail on a gap, here in made-up lists: a file no entry
// names, a command README shows that the table lacks, a flag README shows
// that the table's synopsis doesn't, a command the table holds that README
// doesn't show, and a support file that holds part of a command without
// naming the file of its entry. Lists without a gap pass.
func TestTheDocumentChecksCatchAGap(t *testing.T) {
	support := []struct{ file, holds string }{{"helper.go", "a made-up helper"}}
	table := []command{{name: "tidy", file: "tidy.go", synopsis: []string{"eva-md tidy [--deep]", "eva-md tidy now [--quick]"}}}
	if gaps := unnamedFiles([]string{"tidy.go", "helper.go", "tidy_test.go", "README.md"}, table, support); gaps != nil {
		t.Errorf("a named file fails: %q", gaps)
	}
	if gaps := unnamedFiles([]string{"tidy.go", "helper.go", "stray.go", "stray_test.go"}, table, support); !slices.Equal(gaps, []string{"stray.go"}) {
		t.Errorf("a file no entry names gives %q", gaps)
	}
	if gaps := unnamedFiles([]string{"tidy.go", "helper.go"}, nil, support); !slices.Equal(gaps, []string{"tidy.go"}) {
		t.Errorf("a file whose command isn't in the table gives %q", gaps)
	}
	shown := tableSynopses(table)
	if !slices.Equal(shown["tidy"], []string{"--deep"}) || !slices.Equal(shown["tidy now"], []string{"--quick"}) {
		t.Fatalf("the table's synopses read as %q", shown)
	}
	if gaps := synopsisGaps(map[string][]string{"tidy": {"--deep"}, "tidy now": nil}, shown); gaps != nil {
		t.Errorf("README matching the table fails: %q", gaps)
	}
	gaps := synopsisGaps(map[string][]string{"tidy": {"--deep", "--dry-run"}, "sweep": nil, "tidy now": {"--deep"}}, shown)
	want := []string{
		"README shows eva-md sweep, which the command table lacks",
		"README shows eva-md tidy --dry-run, and the command table's synopsis doesn't",
		"README shows eva-md tidy now --deep, and the command table's synopsis doesn't",
	}
	if !slices.Equal(gaps, want) {
		t.Errorf("README with gaps gives %q, want %q", gaps, want)
	}
	undocumented := append(slices.Clone(table), command{name: "prune", file: "prune.go", synopsis: []string{"eva-md prune [--apply]"}})
	gaps = synopsisGaps(map[string][]string{"tidy": {"--deep"}}, tableSynopses(undocumented))
	want = []string{
		"the command table holds eva-md prune, which README's Commands list doesn't show",
		"the command table holds eva-md tidy now, which README's Commands list doesn't show",
	}
	if !slices.Equal(gaps, want) {
		t.Errorf("a table with commands README doesn't show gives %q, want %q", gaps, want)
	}

	parts := []command{{name: "tidy", file: "tidy.go"}, {name: "tidyup", file: "tidyup.go"}}
	pointing := []struct{ file, holds string }{
		{"deep.go", "eva-md tidy --deep, which tidy.go dispatches to"},
		{"helper.go", "a made-up helper"},
	}
	if gaps := supportPointerGaps(parts, pointing); gaps != nil {
		t.Errorf("support files that point at their entries fail: %q", gaps)
	}
	lost := []struct{ file, holds string }{{"deep.go", "eva-md tidy --deep"}, {"up.go", "the made-up eva-md tidyup"}}
	want = []string{"deep.go holds part of eva-md tidy and doesn't name tidy.go", "up.go holds part of eva-md tidyup and doesn't name tidyup.go"}
	if gaps := supportPointerGaps(parts, lost); !slices.Equal(gaps, want) {
		t.Errorf("support files that hide their entries give %q, want %q", gaps, want)
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
