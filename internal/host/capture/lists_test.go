package capture

import (
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
	"unicode/utf8"

	"github.com/heyNag/eva-md/internal/ledger/event"
)

// root is the repository's root, from this package's folder.
const root = "../../.."

// v1Lists are the functions of v1 that hold its lists of prefixes, sources
// and builtin commands, by file. A later package that retires one of them
// removes its entry here, by its own card.
var v1Lists = map[string][]string{
	hookGo:   {"synthetic", "builtinCommand"},
	parserGo: {"syntheticText", "hostWrapper"},
	auditGo:  {"hostWrapper"},
}

// literals gives every string literal in the bodies of the functions
// named in file, which is read from the repository's root, and fails when
// one of them is not there.
func literals(t *testing.T, file string, funcs []string) []string {
	t.Helper()
	f, err := parser.ParseFile(token.NewFileSet(), filepath.Join(root, filepath.FromSlash(file)), nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	found := map[string]bool{}
	for _, decl := range f.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Recv != nil || !slices.Contains(funcs, fn.Name.Name) {
			continue
		}
		found[fn.Name.Name] = true
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			if lit, ok := n.(*ast.BasicLit); ok && lit.Kind == token.STRING {
				s, err := strconv.Unquote(lit.Value)
				if err != nil {
					t.Fatal(err)
				}
				out = append(out, s)
			}
			return true
		})
	}
	for _, name := range funcs {
		if !found[name] {
			t.Fatalf("%s has no function %s", file, name)
		}
	}
	return out
}

func values(list []Entry) []string {
	var out []string
	for _, e := range list {
		out = append(out, e.Value)
	}
	return out
}

// Every value of v1's lists is on the one list or among the builtin
// commands, so a prefix or source added to v1 is not missed here.
func TestEveryValueOfV1sListsIsOnTheOneList(t *testing.T) {
	known := slices.Concat(values(sources), values(prefixes), builtinCommands)
	read := 0
	for file, funcs := range v1Lists {
		for _, s := range literals(t, file, funcs) {
			if s == "" { // a comparison with nothing, as p.Agent != ""
				continue
			}
			read++
			if !slices.Contains(known, strings.ToLower(s)) {
				t.Errorf("%s lists %q, which is on neither the one list nor the builtin commands", file, s)
			}
		}
	}
	if read < 40 {
		t.Fatalf("read %d values of v1's lists", read)
	}
}

// Every entry that names a file of v1 is in that file's lists, so an entry
// is not made up.
func TestEveryEntryIsInTheV1ListItNames(t *testing.T) {
	for i, list := range [][]Entry{sources, prefixes, entrypoints} {
		for _, e := range list {
			if e.From == "" {
				continue
			}
			var lits []string
			if i == 2 {
				// An entrypoint, which v1 names in its headless list
				// or the test of it.
				lits = fileLiterals(t, e.From)
			} else if funcs, ok := v1Lists[e.From]; ok {
				lits = literals(t, e.From, funcs)
			} else {
				t.Errorf("%q names %s, which holds no list of sources or prefixes", e.Value, e.From)
				continue
			}
			if !slices.ContainsFunc(lits, func(s string) bool { return strings.ToLower(s) == e.Value }) {
				t.Errorf("%q names %s, whose lists do not hold it", e.Value, e.From)
			}
		}
	}
	for _, c := range builtinCommands {
		if !slices.Contains(literals(t, hookGo, []string{"builtinCommand"}), c) {
			t.Errorf("the builtin command %q is not v1's", c)
		}
	}
	for _, e := range entrypoints {
		if e.Author == "program" && !slices.Contains(literals(t, hookGo, []string{"headless"}), e.Value) {
			t.Errorf("the program's entrypoint %q is not on v1's list", e.Value)
		}
	}
}

// wantAuthors is the author of every entry of the one list, written apart
// from lists.go: a program's are messages from another session, teammate
// messages, automation and the SDKs' entrypoints (ledger 3.6); a person's
// typing only the interactive entrypoints.
var wantAuthors = map[string]event.Author{
	// Sources.
	"scheduled": event.AuthorHost, "scheduled_task": event.AuthorHost, "scheduler": event.AuthorHost,
	"cron": event.AuthorHost, "loop": event.AuthorHost, "background": event.AuthorHost, "system": event.AuthorHost,
	"subagent": event.AuthorSubagent, "agent": event.AuthorAgent,
	"other_session": event.AuthorProgram, "session_message": event.AuthorProgram, "automation": event.AuthorProgram,
	// Prefixes.
	"<teammate-message": event.AuthorProgram, "[message from ": event.AuthorProgram,
	"<task-notification>": event.AuthorHost, "<system-reminder>": event.AuthorHost,
	"<local-command-stdout>": event.AuthorHost, "<local-command-stderr>": event.AuthorHost,
	"<command-name>": event.AuthorHost, "<command-message>": event.AuthorHost, "<command-args>": event.AuthorHost,
	"## memory writing agent": event.AuthorHost, "/loop ": event.AuthorHost,
	"# agents.md instructions": event.AuthorHost, "<environment_context>": event.AuthorHost,
	"<permissions instructions>": event.AuthorHost, "/clear": event.AuthorHost, "/compact": event.AuthorHost,
	"<bash-input>": event.AuthorHost, "<bash-stdout>": event.AuthorHost, "<bash-stderr>": event.AuthorHost,
	"[request interrupted by user": event.AuthorHost,
	// Entrypoints.
	"sdk-cli": event.AuthorProgram, "sdk-ts": event.AuthorProgram, "sdk-py": event.AuthorProgram,
	"cli": event.AuthorTyped, "claude-desktop": event.AuthorTyped, "claude-vscode": event.AuthorTyped,
}

// Every entry gives the author written above, and SPEC.md's table of the
// one list gives each value it names the same author, so an author changed
// on the list alone is noticed.
func TestEveryEntryGivesItsAuthor(t *testing.T) {
	kinds := map[string][]Entry{"source": sources, "prefix": prefixes, "entrypoint": entrypoints}
	n := 0
	for kind, list := range kinds {
		for _, e := range list {
			n++
			want, ok := wantAuthors[e.Value]
			if !ok {
				t.Errorf("the %s %q has no author written in the test", kind, e.Value)
			} else if e.Author != want {
				t.Errorf("the %s %q gives %s, want %s", kind, e.Value, e.Author, want)
			}
		}
	}
	if n != len(wantAuthors) {
		t.Errorf("the lists hold %d entries, the test %d", n, len(wantAuthors))
	}
	data, err := os.ReadFile("SPEC.md")
	if err != nil {
		t.Fatal(err)
	}
	_, table, _ := strings.Cut(string(data), "### The one list")
	table, _, _ = strings.Cut(table, "\n## ")
	named := 0
	for _, row := range strings.Split(table, "\n") {
		cells := strings.Split(row, "|")
		if len(cells) < 3 || !strings.Contains(cells[1], "`") {
			continue
		}
		author := event.Author(strings.Trim(strings.TrimSpace(cells[2]), "`"))
		values := regexp.MustCompile("`([^`]+)`").FindAllStringSubmatch(cells[1], -1)
		for _, v := range values {
			named++
			if want, ok := wantAuthors[v[1]]; !ok || want != author {
				t.Errorf("SPEC.md gives %q the author %s; the test %s", v[1], author, want)
			}
		}
	}
	if named < 20 {
		t.Fatalf("SPEC.md's table of the one list names %d values", named)
	}
}

// fileLiterals gives every string literal of a Go file.
func fileLiterals(t *testing.T, file string) []string {
	t.Helper()
	f, err := parser.ParseFile(token.NewFileSet(), filepath.Join(root, filepath.FromSlash(file)), nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	ast.Inspect(f, func(n ast.Node) bool {
		if lit, ok := n.(*ast.BasicLit); ok && lit.Kind == token.STRING {
			if s, err := strconv.Unquote(lit.Value); err == nil {
				out = append(out, s)
			}
		}
		return true
	})
	return out
}

// The lists hold each value once, sources and prefixes in lower case, no
// prefix that only white space could begin, and nothing longer than the
// part of a prompt that is lower-cased to match it.
func TestTheListsAreWellFormed(t *testing.T) {
	seen := map[string]bool{}
	for _, list := range [][]Entry{sources, prefixes, entrypoints} {
		for _, e := range list {
			if seen[e.Value] || e.Value == "" || !e.Author.Valid() || e.Fact == "" || e.State == "" {
				t.Errorf("entry %+v is empty, unknown or there twice", e)
			}
			seen[e.Value] = true
		}
	}
	for _, list := range [][]Entry{sources, prefixes} {
		for _, e := range list {
			if strings.ToLower(e.Value) != e.Value || strings.TrimSpace(e.Value[:1]) == "" {
				t.Errorf("%q is not in lower case, or begins with white space", e.Value)
			}
			if utf8.RuneCountInString(e.Value) >= headRunes {
				t.Errorf("%q is longer than the %d runes matched", e.Value, headRunes)
			}
		}
	}
	for _, c := range builtinCommands {
		if seen[c] && !slices.Contains(values(prefixes), c) {
			t.Errorf("the builtin command %q is also a source or an entrypoint", c)
		}
	}
	for _, e := range entrypoints {
		if e.Author != "typed" && e.Author != "program" {
			t.Errorf("the entrypoint %q gives %s", e.Value, e.Author)
		}
	}
	for _, v := range []string{"claude-desktop-3p", "local-agent", ""} {
		if slices.Contains(values(entrypoints), v) {
			t.Errorf("%q is on a list before a recording shows whose it is", v)
		}
	}
}
