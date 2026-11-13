package guard

import (
	"context"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/heyNag/eva-md/internal/testgitenv"
)

func TestMain(m *testing.M) {
	testgitenv.Clear()
	os.Exit(m.Run())
}

func TestAFolderEntryMatchesOnlyWhatIsUnderIt(t *testing.T) {
	cases := []struct {
		pattern, file string
		want          bool
	}{
		{"a/**", "a/b/c", true},
		{"a/**", "a/b", true},
		{"a/**", "ab/c", false},
		{"a/**", "a", false},
		{"a/**", "b/a/c", false},
		{"a/b.go", "a/b.go", true},
		{"a/b.go", "a/b.go.orig", false},
		{"a/b.go", "x/a/b.go", false},
		{"docs/*.md", "docs/x.md", true},
		{"docs/*.md", "docs/sub/x.md", false},
		{"docs/*.md", "docs/x.mdx", false},
	}
	for _, c := range cases {
		p, err := ParsePattern(c.pattern, true)
		if err != nil {
			t.Fatal(err)
		}
		if got := p.Match(c.file); got != c.want {
			t.Errorf("%s matches %s: %v, want %v", c.pattern, c.file, got, c.want)
		}
	}
}

func TestAnEntryIsAPathFromTheRoot(t *testing.T) {
	for _, s := range []string{"", "/a", "./a", "a/", "a//b", "a/../b", "../a", "..", ".", "**", "/**", "a/**/b", "a/**b", "a/b**", `a\b`, "a/[b",
		"c/x.go (new)", "- a/**", "* a/**", "`a.go`", "a/`b`.go", "a b.go", "a.go\t", "a/** "} {
		if _, err := ParsePattern(s, true); err == nil {
			t.Errorf("the entry %q is accepted", s)
		}
	}
	// A deletes block names files and folders, never a pattern; nor is a
	// pattern allowed before a folder's /**.
	for _, s := range []string{"a/*.go", "a/?.go", "a/[ab].go"} {
		if _, err := ParsePattern(s, false); err == nil {
			t.Errorf("the deletes entry %q is accepted", s)
		}
	}
	if _, err := ParsePattern("a/*/**", true); err == nil {
		t.Error("a pattern before /** is accepted")
	}
	for _, s := range []string{"a", "a/b.go", "a/**", ".github/workflows/ci.yml", "docs/v2/cards/*.md"} {
		if _, err := ParsePattern(s, true); err != nil {
			t.Errorf("the entry %q: %v", s, err)
		}
	}
}

func TestBlocksAreReadByTheirInfoString(t *testing.T) {
	card := "# X-1\n\n```paths\na/**\n\n  b.go  \n```\n\nText.\n\n" +
		"```go\n```paths\nnot/this\n```\n\n" +
		"````md\n```paths\nnor/this\n```\n````\n\n" +
		"```paths extra\nnor/that\n```\n\n" +
		"```deletes\nold.go\n```\n\n" +
		"```paths\nc/**\n```\n"
	paths, found, err := Block(card, "paths")
	if err != nil || !found || !slices.Equal(paths, []string{"a/**", "b.go", "c/**"}) {
		t.Fatalf("paths %q, %v, %v", paths, found, err)
	}
	deletes, found, err := Block(card, "deletes")
	if err != nil || !found || !slices.Equal(deletes, []string{"old.go"}) {
		t.Fatalf("deletes %q, %v, %v", deletes, found, err)
	}
	if _, found, err := Block("# X-1\n\nNo blocks.\n", "paths"); found || err != nil {
		t.Fatalf("a card with no block: %v, %v", found, err)
	}
	if _, _, err := Block("```paths\na/**\n", "paths"); err == nil {
		t.Fatal("a block left open is read")
	}
	if _, _, err := Patterns("```paths\na/../b\n```\n", "paths"); err == nil {
		t.Fatal("a bad entry is read")
	}
}

func TestTheIndexGivesTheMergedCards(t *testing.T) {
	index := "| Card | Package | State |\n| --- | --- | --- |\n" +
		"| [AA-1](AA-1.md) | One | merged |\n" +
		"| [AA-2](AA-2.md) | Two | merged; advisory in CI |\n" +
		"| [AA-3](AA-3.md) | Three, once merged | building since today |\n" +
		"| [AA-4](AA-4.md) | Four | after AA-1, merged when ready |\n" +
		"[AA-5](AA-5.md) merged\n" +
		"| [AA-6](AA-6.md) | A row with no closing bar | merged\n"
	if got := Merged(index); !slices.Equal(got, []string{"AA-1", "AA-2"}) {
		t.Fatalf("merged %q", got)
	}
}

// Every card in the repository has well-formed blocks, so the check CI
// runs can read them.
func TestTheRepositorysCardsAreWellFormed(t *testing.T) {
	cards, err := filepath.Glob(filepath.Join("..", "..", "..", "docs", "v2", "cards", "*-*.md"))
	if err != nil || len(cards) < 10 {
		t.Fatalf("found the cards %v: %v", cards, err)
	}
	withPaths := 0
	for _, f := range cards {
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		id := strings.TrimSuffix(filepath.Base(f), ".md")
		if CardFile(id) == "" {
			t.Errorf("%s is not named for a card's id", f)
		}
		for _, kind := range []string{"paths", "deletes"} {
			_, found, err := Patterns(string(data), kind)
			if err != nil {
				t.Errorf("%s: %v", id, err)
			}
			if found && kind == "paths" {
				withPaths++
			}
		}
	}
	index, err := os.ReadFile(filepath.Join("..", "..", "..", "docs", "v2", "cards", "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	if len(Merged(string(index))) == 0 || withPaths == 0 {
		t.Fatalf("the index lists no merged card, or no card has a paths block (%d)", withPaths)
	}
}

// isolate gives the test a temporary home whose git config is a temporary
// file with a made-up identity.
func isolate(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(home, "gitconfig"))
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	for _, name := range []string{"GIT_DIR", "GIT_WORK_TREE", "GIT_INDEX_FILE", "GIT_COMMON_DIR", "GIT_TEMPLATE_DIR"} {
		t.Setenv(name, "")
		os.Unsetenv(name)
	}
	write(t, filepath.Join(home, "gitconfig"), "[user]\nname = Eva Fixture\nemail = fixture@example.invalid\n[commit]\ngpgsign = false\n[init]\ndefaultBranch = main\n[log]\nshowSignature = true\n")
	return home
}

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func gitIn(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v %s", args, err, out)
	}
	return strings.TrimSuffix(string(out), "\n")
}

func card(paths ...string) string {
	return "# A card\n\n## Owns and may touch\n\n```paths\n" + strings.Join(paths, "\n") + "\n```\n"
}

// A history is a repository the test builds commit by commit.
type history struct {
	t    *testing.T
	repo string
}

func newHistory(t *testing.T) *history {
	t.Helper()
	isolate(t)
	h := &history{t, filepath.Join(t.TempDir(), "repo")}
	if err := os.Mkdir(h.repo, 0o755); err != nil {
		t.Fatal(err)
	}
	gitIn(t, h.repo, "init", "--quiet")
	return h
}

// commit writes the files (an empty content deletes one), commits them
// with message, and returns the commit.
func (h *history) commit(message string, files map[string]string) string {
	h.t.Helper()
	for name, content := range files {
		path := filepath.Join(h.repo, filepath.FromSlash(name))
		if content == "" {
			gitIn(h.t, h.repo, "rm", "--quiet", "--", name)
			continue
		}
		write(h.t, path, content)
		gitIn(h.t, h.repo, "add", "--", name)
	}
	gitIn(h.t, h.repo, "commit", "--quiet", "--allow-empty", "--cleanup=verbatim", "-m", message)
	return gitIn(h.t, h.repo, "rev-parse", "HEAD")
}

func (h *history) judge(from, to string) map[string]Judgement {
	h.t.Helper()
	judged, err := Ownership(context.Background(), h.repo, from, to)
	if err != nil {
		h.t.Fatal(err)
	}
	out := map[string]Judgement{}
	for _, j := range judged {
		out[j.Commit] = j
	}
	if len(out) != len(judged) {
		h.t.Fatalf("a commit is judged twice: %v", judged)
	}
	return out
}

// faultsOf is the commit's faults, which must each name its card and the
// path or entry the test expects, in order.
func expectFaults(t *testing.T, name string, j Judgement, want ...string) {
	t.Helper()
	if len(j.Faults) != len(want) {
		t.Fatalf("%s: faults %q, want %d naming %q", name, j.Faults, len(want), want)
	}
	for i, w := range want {
		if !strings.Contains(j.Faults[i], w) {
			t.Errorf("%s: fault %q does not name %q", name, j.Faults[i], w)
		}
	}
}

func TestEachCommitStaysInsideItsCardsPaths(t *testing.T) {
	h := newHistory(t)
	base := h.commit("the lead's first commit", map[string]string{
		"docs/v2/cards/AA-1.md": card("a/**", "b.go"),
		"docs/v2/cards/AA-2.md": card("c/**"),
		"docs/v2/cards/AA-3.md": "# A card with no block\n",
		"docs/v2/cards/AA-5.md": card("f/**", "h/?.go", "docs/v2/cards/AA-5.md"),
		"a/x.go":                "package a\n",
		"a/y.go":                "package a\n",
		"c/old.go":              "package c\n",
		"keep.go":               "package keep\n",
	})
	inside := h.commit("inside\n\nCard: AA-1", map[string]string{"a/x.go": "package a\n\nvar X int\n", "b.go": "package b\n", "a/new/z.go": "package z\n"})
	added := h.commit("adds outside\n\nCard: AA-1", map[string]string{"ab/c.go": "package ab\n"})
	deleted := h.commit("deletes outside\n\nCard: AA-1", map[string]string{"c/old.go": ""})
	if err := os.MkdirAll(filepath.Join(h.repo, "c"), 0o755); err != nil {
		t.Fatal(err)
	}
	gitIn(t, h.repo, "mv", "a/y.go", "c/y.go")
	renamed := h.commit("renames outside\n\nCard: AA-1", nil)
	gitIn(t, h.repo, "mv", "c/y.go", "a/y.go")
	both := h.commit("renames back, owned by two cards\n\nCard: AA-2\nCard: AA-1", nil)
	missing := h.commit("names a card that does not exist\n\nCard: AA-9", map[string]string{"a/x.go": "package a\n"})
	noBlock := h.commit("names a card with no block\n\nCard: AA-3", map[string]string{"a/x.go": "package a\n\nvar Y int\n"})
	notAnID := h.commit("names no card\n\nCard: ../AA-1", map[string]string{"a/x.go": "package a\n\nvar Z int\n"})
	widens := h.commit("widens its own card\n\nCard: AA-1", map[string]string{
		"docs/v2/cards/AA-1.md": card("a/**", "b.go", "d/**", "docs/v2/cards/AA-1.md"),
		"d/x.go":                "package d\n",
	})
	narrows := h.commit("narrows its own card\n\nCard: AA-2", map[string]string{
		"docs/v2/cards/AA-2.md": card("c/sub/**", "c/one.go", "docs/v2/cards/AA-2.md"),
		"c/sub/s.go":            "package sub\n",
	})
	newCard := h.commit("adds its own card\n\nCard: AA-4", map[string]string{
		"docs/v2/cards/AA-4.md": card("e/**", "docs/v2/cards/AA-4.md"),
		"e/x.go":                "package e\n",
	})
	lead := h.commit("the lead's, anywhere", map[string]string{"anywhere/x.go": "package x\n", "keep.go": ""})
	// A commit that lands a package carries its Card: line straight after
	// the subject, where git does not read it as a trailer, and the
	// session's lines in its last paragraph.
	landed := h.commit("Lands a package\n\nCard: AA-1\n\nWhat changed, and why, over\ntwo lines.\n\nA second paragraph.\n\nCo-Authored-By: A Fixture <fixture@example.invalid>\nSession: https://example.invalid/s/1", map[string]string{"zz.go": "package zz\n"})
	landedInside := h.commit("Lands inside\n\nCard: AA-1\r\n\nWhat changed.\n\nCo-Authored-By: A Fixture <fixture@example.invalid>", map[string]string{"a/landed.go": "package a\n"})
	// A line that only mentions a card is not read as one.
	notCard := h.commit("Card AA-1: a subject that only starts with the word\n\nThe Card: AA-1 line\nA card: AA-1", map[string]string{"zz.go": "package zz\n\nvar Z int\n"})
	// A line that sets out to name cards in another form is a fault, and
	// the commit is not the lead's, even beside a line that names one.
	badCard := h.commit("Lands two cards\n\nCard: AA-1, AA-2", map[string]string{"zz.go": "package zz\n\nvar Y int\n"})
	badBeside := h.commit("Lands a package\n\nCard: AA-1\n    Card: AA-2", map[string]string{"a/x.go": "package a\n\nvar W int\n"})
	// A folder entry covers only what is under it, not a sibling whose
	// name it starts, and a pattern of one name covers no wider pattern.
	widensSibling := h.commit("widens f/** to its sibling fg\n\nCard: AA-5", map[string]string{
		"docs/v2/cards/AA-5.md": card("f/**", "fg/x.go", "h/?.go", "docs/v2/cards/AA-5.md"),
		"fg/x.go":               "package fg\n",
	})
	widensGlob := h.commit("widens one name's pattern\n\nCard: AA-5", map[string]string{
		"docs/v2/cards/AA-5.md": card("f/**", "fg/x.go", "h/*.go", "docs/v2/cards/AA-5.md"),
	})

	got := h.judge(base, widensGlob)
	if len(got) != 19 {
		t.Fatalf("judged %d commits, want 19", len(got))
	}
	if _, ok := got[base]; ok {
		t.Error("the range's first commit is judged")
	}
	expectFaults(t, "inside", got[inside])
	if j := got[inside]; j.Paths != 3 || !slices.Equal(j.Cards, []string{"AA-1"}) {
		t.Errorf("inside: %+v", j)
	}
	expectFaults(t, "added", got[added], "AA-1: ab/c.go")
	expectFaults(t, "deleted", got[deleted], "AA-1: c/old.go")
	expectFaults(t, "renamed", got[renamed], "AA-1: c/y.go")
	if got[renamed].Paths != 2 {
		t.Errorf("a rename counts %d paths, want 2", got[renamed].Paths)
	}
	expectFaults(t, "both", got[both])
	if !slices.Equal(got[both].Cards, []string{"AA-2", "AA-1"}) {
		t.Errorf("both: cards %q", got[both].Cards)
	}
	expectFaults(t, "missing", got[missing], "AA-9: the card docs/v2/cards/AA-9.md does not exist")
	expectFaults(t, "no block", got[noBlock], "AA-3: the card docs/v2/cards/AA-3.md has no paths block")
	expectFaults(t, "not an id", got[notAnID], "../AA-1: the Card: line names no card")
	expectFaults(t, "widens", got[widens], "AA-1: the commit widens its card's paths block with d/**", "AA-1: the commit widens its card's paths block with docs/v2/cards/AA-1.md")
	expectFaults(t, "narrows", got[narrows], "AA-2: the commit widens its card's paths block with docs/v2/cards/AA-2.md")
	expectFaults(t, "new card", got[newCard], "AA-4: the commit widens its card's paths block with e/**", "AA-4: the commit widens its card's paths block with docs/v2/cards/AA-4.md")
	if j := got[lead]; !j.Lead() || len(j.Faults) != 0 || j.Paths != 2 {
		t.Errorf("the lead's commit: %+v", j)
	}
	expectFaults(t, "landed", got[landed], "AA-1: zz.go is outside the card's paths")
	if j := got[landed]; !slices.Equal(j.Cards, []string{"AA-1"}) {
		t.Errorf("landed: cards %q", j.Cards)
	}
	expectFaults(t, "landed inside", got[landedInside])
	if j := got[landedInside]; !slices.Equal(j.Cards, []string{"AA-1"}) || j.Paths != 1 {
		t.Errorf("landed inside: %+v", j)
	}
	if j := got[notCard]; !j.Lead() || j.Paths != 1 {
		t.Errorf("a line that only mentions a card is read as a Card: line: %+v", j)
	}
	expectFaults(t, "bad card", got[badCard], `the line "Card: AA-1, AA-2" does not name one card`)
	if j := got[badCard]; j.Lead() || len(j.Cards) != 0 {
		t.Errorf("a commit whose only Card: line is malformed: %+v", j)
	}
	expectFaults(t, "bad beside", got[badBeside], `the line "Card: AA-2" does not name one card`)
	expectFaults(t, "widens sibling", got[widensSibling], "AA-5: the commit widens its card's paths block with fg/x.go")
	expectFaults(t, "widens glob", got[widensGlob], "AA-5: the commit widens its card's paths block with h/*.go")

	// The card is read from the commit's own tree: at the commit that
	// widened it, AA-1 owned d/**, and later d/x.go is still its own.
	later := h.commit("inside the widened card\n\nCard: AA-1", map[string]string{"d/x.go": "package d\n\nvar D int\n"})
	expectFaults(t, "later", h.judge(widensGlob, later)[later])
}

// A line that sets out to name cards is read only in the one form, and in
// any other it is a fault; a line that only mentions a card is not read.
func TestOnlyALineOfOneCardNamesACard(t *testing.T) {
	cases := []struct {
		message string
		cards   []string
		bad     int
	}{
		{"Subject\n\nCard: AA-1\n\nBody.", []string{"AA-1"}, 0},
		{"Subject\n\nCard: AA-1\r\nCard:AA-2 \nCard: AA-1", []string{"AA-1", "AA-2"}, 0},
		{"Subject\n\nBody.\n\nCard: AA-1", []string{"AA-1"}, 0},
		{"Card AA-1: a subject\n\nThe Card: AA-1 line\nA card: AA-1\nDiscard: AA-1", nil, 0},
		{"Subject\n\nCard: AA-1 (part 2)", nil, 1},
		{"Subject\n\ncard: AA-1", nil, 1},
		{"Subject\n\nCARD: AA-1", nil, 1},
		{"Subject\n\nCard: AA-1, AA-2", nil, 1},
		{"Subject\n\nCard: AA-1 AA-2", nil, 1},
		{"Subject\n\nCard: AA-1 and AA-2", nil, 1},
		{"Subject\n\nCards: AA-1", nil, 1},
		{"Subject\n\nCard :AA-1", nil, 1},
		{"Subject\n\nCard:", nil, 1},
		// git's message for a squash indents each commit's lines.
		{"Squashed commit of the following:\n\n    Lands it\n\n    Card: AA-1", nil, 1},
		{"Subject\n\n\tCard: AA-1", nil, 1},
		{"Subject\n\nCard: AA-1\nCard: AA-2, AA-3", []string{"AA-1"}, 1},
	}
	for _, c := range cases {
		cards, bad := cardsOf(c.message)
		if !slices.Equal(cards, c.cards) || len(bad) != c.bad {
			t.Errorf("%q: cards %q, bad %q; want %q and %d bad", c.message, cards, bad, c.cards, c.bad)
		}
	}
}

// A merge is judged on its diff against its first parent; the commits it
// brings in by its second parent are not judged alone.
func TestAMergesSecondParentIsNotJudgedAlone(t *testing.T) {
	h := newHistory(t)
	base := h.commit("the lead's first commit", map[string]string{
		"docs/v2/cards/AA-1.md": card("a/**"),
		"a/x.go":                "package a\n",
	})
	gitIn(t, h.repo, "switch", "--quiet", "-c", "work")
	side := h.commit("a commit that would fail alone\n\nCard: AA-9", map[string]string{"a/side.go": "package a\n", "outside.go": "package o\n"})
	h.commit("takes the outside file back\n\nCard: AA-9", map[string]string{"outside.go": ""})
	gitIn(t, h.repo, "switch", "--quiet", "main")
	gitIn(t, h.repo, "merge", "--quiet", "--no-ff", "-m", "merge the work\n\nCard: AA-1", "work")
	merge := gitIn(t, h.repo, "rev-parse", "HEAD")
	got := h.judge(base, merge)
	if len(got) != 1 {
		t.Fatalf("judged %v, want the merge alone", got)
	}
	if _, ok := got[side]; ok {
		t.Fatal("a second-parent commit is judged")
	}
	expectFaults(t, "merge", got[merge])
	if got[merge].Paths != 1 {
		t.Errorf("the merge touches %d paths, want 1", got[merge].Paths)
	}
}

func TestARangeGitCannotReadIsAnError(t *testing.T) {
	h := newHistory(t)
	first := h.commit("first", map[string]string{"a.go": "package a\n"})
	if _, err := Ownership(context.Background(), h.repo, "no-such-ref", first); err == nil {
		t.Error("a range from a ref that does not exist is read")
	}
	if _, err := Ownership(context.Background(), t.TempDir(), "HEAD~1", "HEAD"); err == nil {
		t.Error("a folder that is no repository is read")
	}
	if got := h.judge(first, first); len(got) != 0 {
		t.Errorf("an empty range judged %v", got)
	}
}

// goEnv points the go command at a temporary home, offline and on the
// installed toolchain, with Go's own caches, which go test itself uses. It
// also sets what Built must override, in the environment or, with
// envFile, in a go env file: another system, cgo, and a build tag.
func goEnv(t *testing.T, envFile bool) {
	t.Helper()
	out, err := exec.Command("go", "env", "GOCACHE", "GOMODCACHE").Output()
	if err != nil {
		t.Fatal(err)
	}
	caches := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(caches) != 2 {
		t.Fatalf("go env gave %q", out)
	}
	home := isolate(t)
	config := filepath.Join(home, ".config")
	if runtime.GOOS == "darwin" {
		config = filepath.Join(home, "Library", "Application Support")
	}
	write(t, filepath.Join(config, "go", "telemetry", "mode"), "off\n")
	env := map[string]string{
		"XDG_CONFIG_HOME": filepath.Join(home, ".config"), "GOCACHE": caches[0], "GOMODCACHE": caches[1],
		"GOTOOLCHAIN": "local", "GOPROXY": "off",
	}
	wrong := map[string]string{"GOOS": "linux", "GOARCH": "amd64", "CGO_ENABLED": "1", "GOFLAGS": "-tags=extra"}
	if envFile {
		// A go env file a developer wrote with go env -w; GOFLAGS there
		// applies only while the environment's is empty.
		file := filepath.Join(home, "goenv")
		write(t, file, "GOFLAGS=-tags=extra\nCGO_ENABLED=1\nGOOS=linux\n")
		env["GOENV"] = file
		env["GOFLAGS"] = ""
	} else {
		env["GOENV"] = "off"
		maps.Copy(env, wrong)
	}
	for k, v := range env {
		t.Setenv(k, v)
	}
}

// fixtureModule writes a module whose ./cmd/eva-md builds some of its
// files on macOS and returns its root and the hand count of each file
// that must count.
func fixtureModule(t *testing.T) (string, map[string]int) {
	t.Helper()
	root := filepath.Join(t.TempDir(), "module")
	files := map[string]string{
		"go.mod":                  "module example.com/app\n\ngo 1.22\n\nrequire example.com/lib v0.0.0\n\nreplace example.com/lib => ./lib\n",
		"cmd/eva-md/main.go":      "package main\n\nimport (\n\t\"example.com/app/internal/a\"\n\t\"example.com/lib\"\n)\n\nfunc main() { a.F(); lib.F() }\n",
		"cmd/eva-md/m_test.go":    "package main\n\nimport \"testing\"\n\nfunc TestM(t *testing.T) {}\n",
		"internal/a/a.go":         "package a\n\nfunc F() {}\n",
		"internal/a/a_darwin.go":  "package a\n\n// macOS only.\nvar Darwin = 1\n",
		"internal/a/a_arm64.go":   "package a\n\nvar Arch = 1\n",
		"internal/a/a_amd64.go":   "package a\n\nvar Arch = 2\n\n\n",
		"internal/a/a_linux.go":   "package a\n\nvar Linux = 1\n",
		"internal/a/a_extra.go":   "//go:build extra\n\npackage a\n\nvar Extra = 1\n",
		"internal/a/a_cgo.go":     "package a\n\nimport \"C\"\n\nvar Cgo = 1\n",
		"internal/a/a_tag_cgo.go": "//go:build cgo\n\npackage a\n\nvar TagCgo = 1\n",
		"internal/a/a_test.go":    "package a\n\nimport \"testing\"\n\nfunc TestA(t *testing.T) {}\n",
		"internal/a/doc.txt":      "not Go\n",
		"internal/unused/u.go":    "package unused\n\nvar U = 1\n",
		"lib/go.mod":              "module example.com/lib\n\ngo 1.22\n",
		"lib/lib.go":              "package lib\n\nfunc F() {}\n",
	}
	for name, content := range files {
		write(t, filepath.Join(root, filepath.FromSlash(name)), content)
	}
	return root, map[string]int{
		"cmd/eva-md/main.go":     8,
		"internal/a/a.go":        3,
		"internal/a/a_darwin.go": 4,
		"internal/a/a_arm64.go":  3,
		"internal/a/a_amd64.go":  5,
	}
}

// What the environment or a go env file sets, another system, cgo or a
// build tag, changes nothing of the count.
func TestBuiltCountsWhatAMacOSReleaseBuilds(t *testing.T) {
	for _, envFile := range []bool{false, true} {
		t.Run(map[bool]string{false: "environment", true: "go env file"}[envFile], func(t *testing.T) {
			goEnv(t, envFile)
			root, want := fixtureModule(t)
			got, err := Built(context.Background(), root)
			if err != nil {
				t.Fatal(err)
			}
			if len(got) != len(want) {
				t.Errorf("counted %v, want %v", got, want)
			}
			for f, n := range want {
				if got[f] != n {
					t.Errorf("%s: %d lines, want %d", f, got[f], n)
				}
			}
			s, err := Count(root, got, Ceiling, nil)
			if err != nil || s.Built != 23 || s.Net() != 23 || !s.Holds() {
				t.Errorf("count %+v, %v", s, err)
			}
			if _, err := Built(context.Background(), t.TempDir()); err == nil {
				t.Error("a folder with no module is counted")
			}
		})
	}
}

// A repository of cards for Count: the index and each card's file.
func cardsRepo(t *testing.T, index string, cards map[string]string, present ...string) string {
	t.Helper()
	repo := t.TempDir()
	if index != "" {
		write(t, filepath.Join(repo, "docs", "v2", "cards", "README.md"), index)
	}
	for id, text := range cards {
		write(t, filepath.Join(repo, "docs", "v2", "cards", id+".md"), text)
	}
	for _, f := range present {
		write(t, filepath.Join(repo, filepath.FromSlash(f)), "package x\n")
	}
	return repo
}

func row(id, state string) string {
	return "| [" + id + "](" + id + ".md) | A package | " + state + " |\n"
}

func deletes(entries ...string) string {
	return "# A card\n\n```paths\nnew/**\n```\n\n```deletes\n" + strings.Join(entries, "\n") + "\n```\n"
}

var v1Had = map[string]bool{"old/a.go": true, "old/gone.go": true, "v1.go": true, "other.go": true, "keep.go": true, "old/a_test.go": true}

func v1(t *testing.T) func() (map[string]bool, error) {
	return func() (map[string]bool, error) { return v1Had, nil }
}

var builtLines = map[string]int{"old/a.go": 5, "old/new.go": 7, "v1.go": 3, "other.go": 11, "keep.go": 100}

func TestTheCeilingIsPassedToTheCount(t *testing.T) {
	repo := cardsRepo(t, "", nil)
	never := func() (map[string]bool, error) { t.Fatal("v1's files are read with no index"); return nil, nil }
	at, err := Count(repo, builtLines, 126, never)
	if err != nil || at.Built != 126 || at.Net() != 126 || at.Headroom() != 0 || !at.Holds() {
		t.Fatalf("at the ceiling: %+v, %v", at, err)
	}
	over, err := Count(repo, builtLines, 125, never)
	if err != nil || over.Headroom() != -1 || over.Holds() {
		t.Fatalf("one line over: %+v, %v", over, err)
	}
	if !strings.Contains(over.Report(), "The net lines exceed the ceiling by 1.") || !strings.Contains(at.Report(), "Headroom: 0\n") {
		t.Errorf("reports:\n%s\n%s", at.Report(), over.Report())
	}
}

func TestAMergedCardsDeletesSubtract(t *testing.T) {
	index := "| Card | Package | State |\n| --- | --- | --- |\n" + row("AA-1", "merged") + row("AA-2", "building since today") + row("AA-3", "merged; a note")
	repo := cardsRepo(t, index, map[string]string{
		// old/new.go is under old/** but v1 did not have it; old/a.go is
		// named twice and counts once; old/gone.go is not built.
		"AA-1": deletes("old/**", "v1.go"),
		"AA-2": deletes("other.go"),
		"AA-3": deletes("old/a.go") + "\n```deletes\n```\n",
	})
	s, err := Count(repo, builtLines, 118, v1(t))
	if err != nil {
		t.Fatal(err)
	}
	if s.Built != 126 || s.Scheduled != 8 || s.Files != 2 || s.Net() != 118 || !s.Holds() || s.Plain {
		t.Fatalf("count %+v", s)
	}
	if !slices.Equal(s.Merged, []string{"AA-1", "AA-3"}) {
		t.Errorf("merged %q", s.Merged)
	}
	if s, _ := Count(repo, builtLines, 117, v1(t)); s.Holds() {
		t.Errorf("one line over holds: %+v", s)
	}

	// Once the index lists the cut-over card as merged, nothing subtracts,
	// and a scheduled file still present is a fault.
	plain := cardsRepo(t, index+row("CUT-4", "merged"), map[string]string{
		"AA-1": deletes("old/**", "v1.go"), "AA-2": deletes("other.go"), "AA-3": deletes("old/a.go"), "CUT-4": "# The cut-over\n",
	}, "old/a.go", "other.go")
	s, err = Count(plain, builtLines, 126, v1(t))
	if err != nil {
		t.Fatal(err)
	}
	if s.Scheduled != 0 || s.Net() != 126 || !s.Plain || s.Holds() || len(s.Faults) != 1 || !strings.Contains(s.Faults[0], "old/a.go is scheduled for deletion and still present") {
		t.Fatalf("after the cut-over: %+v", s)
	}
	if !strings.Contains(s.Report(), "the ceiling is plain") {
		t.Errorf("report:\n%s", s.Report())
	}
}

func TestADeletesBlocksFaults(t *testing.T) {
	index := row("AA-1", "merged") + row("AA-2", "merged") + row("AA-3", "merged")
	repo := cardsRepo(t, index, map[string]string{
		"AA-1": deletes("v1.go", "never/had.go", "never/**"),
		"AA-2": deletes("old/*.go"),
	})
	s, err := Count(repo, builtLines, 1000, v1(t))
	if err != nil {
		t.Fatal(err)
	}
	if s.Scheduled != 3 || s.Holds() || len(s.Faults) != 3 {
		t.Fatalf("count %+v", s)
	}
	for i, want := range []string{"AA-2: its deletes block", "AA-3: the index lists it as merged", "AA-1: its deletes block names never/had.go, which v1 did not have"} {
		if !strings.Contains(s.Faults[i], want) {
			t.Errorf("fault %q does not say %q", s.Faults[i], want)
		}
	}
	if !strings.Contains(s.Report(), "Fault: AA-1") {
		t.Errorf("report:\n%s", s.Report())
	}
}
