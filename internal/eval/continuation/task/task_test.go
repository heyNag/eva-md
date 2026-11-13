package task

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"testing"
)

func TestLoadAllFixtures(t *testing.T) {
	tasks, err := LoadAll(fixtures)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, x := range tasks {
		got = append(got, x.ID+":"+x.Family+":"+x.Workdir)
	}
	want := []string{"control-max:control:repo", "handoff-split:handoff:outside", "hidden-parse:hidden:repo"}
	if !slices.Equal(got, want) {
		t.Fatalf("tasks = %v, want %v", got, want)
	}
	h := tasks[2]
	if h.Dir != filepath.Join(fixtures, "hidden-parse") || !h.Facts[0].Name || h.Tests[0].Limit() != DefaultLimit {
		t.Fatalf("hidden-parse loaded as %+v", h)
	}
	if limit := tasks[1].Tests[1].Limit(); limit != 900 {
		t.Fatalf("split-refuses-zero's limit = %d, want 900", limit)
	}
}

func TestLoadRefuses(t *testing.T) {
	dir := copyFixture(t, "control-max")
	data, err := os.ReadFile(filepath.Join(dir, FileName))
	if err != nil {
		t.Fatal(err)
	}
	unknown := strings.Replace(string(data), `"max_turns"`, `"turn_limit": 3, "max_turns"`, 1)
	write(t, dir, FileName, unknown)
	if _, err := Load(dir); err == nil {
		t.Fatal("a field the format lacks was taken")
	}

	twice := tempDir(t)
	for _, name := range []string{"a", "b"} {
		if err := copyTree(filepath.Join(fixtures, "control-max"), mkdir(t, filepath.Join(twice, name))); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := LoadAll(twice); err == nil || !strings.Contains(err.Error(), "two tasks") {
		t.Fatalf("two tasks with one id: %v", err)
	}
}

func mkfifo(p string) error { return syscall.Mkfifo(p, 0o644) }

func mkdir(t *testing.T, dir string) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	return dir
}

// base is a valid hidden-decision task with no folder, so its overlays are
// not read.
func base(t *testing.T) Task {
	t.Helper()
	x := load(t, "hidden-parse")
	x.Dir = ""
	if err := x.Validate(); err != nil {
		t.Fatal(err)
	}
	return x
}

func TestValidateFaults(t *testing.T) {
	long := strings.Repeat("a", maxID+1)
	cases := []struct {
		name   string
		change func(x *Task)
		want   string
	}{
		{"id not an id", func(x *Task) { x.ID = "Hidden Parse" }, "not a results id"},
		{"id too long", func(x *Task) { x.ID = long }, "not a results id"},
		{"unknown family", func(x *Task) { x.Family = "lesson" }, "unknown family"},
		{"unknown workdir", func(x *Task) { x.Workdir = "home" }, "unknown workdir"},
		{"two repositories in a repository", func(x *Task) { x.Repos = []string{"ledgerkit", "tallybook"} }, "two need the workdir outside"},
		{"three repositories", func(x *Task) { x.Workdir, x.Repos = WorkdirOutside, []string{"ledgerkit", "tallybook", "abacuskit"} }, "one or two"},
		{"a repository twice", func(x *Task) { x.Workdir, x.Repos = WorkdirOutside, []string{"ledgerkit", "ledgerkit"} }, "given twice"},
		{"a repository name with a slash", func(x *Task) { x.Repos = []string{"../ledgerkit"} }, "not an id"},
		{"a fact no turn plants", func(x *Task) { x.History[2].Plants = nil }, "no turn plants it"},
		{"a plant of no fact", func(x *Task) { x.History[0].Plants = []string{"f-none"} }, "unknown fact"},
		{"hidden: a fact no test encodes", func(x *Task) { x.Tests[1].Facts = nil }, "no test encodes it"},
		{"control: a test encodes a fact", func(x *Task) { x.Family = FamilyControl }, "encodes no fact"},
		{"a test encodes an unknown fact", func(x *Task) { x.Tests[0].Facts = []string{"f-none"} }, "unknown fact"},
		{"a value inside the task's id", func(x *Task) { x.ID = "hidden-quillspar-parse" }, "inside the task's id"},
		{"a value inside the task's id, spelled otherwise", func(x *Task) { x.ID = "quill.spar" }, "inside the task's id"},
		{"a value across the id and a trial", func(x *Task) { x.Facts[0].Values = []string{"parse-t12"} }, "inside the task's id"},
		{"a repository name inside the task's id", func(x *Task) { x.ID = "ledgerkit-parse" }, "inside the task's id"},
		{"a value inside a test's id", func(x *Task) { x.Tests[0].ID = "quillspar-reads" }, "inside its id"},
		{"a repository name inside a test's id", func(x *Task) { x.Tests[0].ID = "ledger-kit-reads" }, "inside its id"},
		{"a value inside every row", func(x *Task) { x.Facts[0].Values = []string{"Prompt_Tokens"} }, "every row"},
		{"a value of under six letters", func(x *Task) { x.Facts[0].Values = []string{"qu-il!"} }, "under 6"},
		{"a value of hex digits only", func(x *Task) { x.Facts[0].Values = []string{"Dead-Beef-42"} }, "hex digits only"},
		{"a repository name of under six letters", func(x *Task) { x.Repos = []string{"kit"}; x.Tests[0].Repo, x.Tests[1].Repo = "kit", "kit" }, "under 6"},
		{"a repository name of hex digits only", func(x *Task) { x.Repos = []string{"c0ffee"}; x.Tests[0].Repo, x.Tests[1].Repo = "c0ffee", "c0ffee" }, "hex digits only"},
		{"a fact with no value", func(x *Task) { x.Facts[0].Values = nil }, "no value"},
		{"a history that ends with the user", func(x *Task) { x.History = x.History[:3] }, "agent's turn"},
		{"a history under 600 characters", func(x *Task) {
			for i := range x.History {
				x.History[i].Text = x.History[i].Text[:60]
			}
		}, "under 600"},
		{"a history with no user", func(x *Task) {
			for i := range x.History {
				x.History[i].By = "agent"
			}
		}, "no user's turn"},
		{"an unknown speaker", func(x *Task) { x.History[0].By = "system" }, "user or agent"},
		{"a test in another repository", func(x *Task) { x.Tests[0].Repo = "tallybook" }, "not the task's"},
		{"a test with no argv", func(x *Task) { x.Tests[0].Run = nil }, "not an argv"},
		{"a test with no runner", func(x *Task) { x.Tests[0].Run = []string{"", "test"} }, "not an argv"},
		{"a negative limit", func(x *Task) { x.Tests[0].LimitS = -1 }, "limit_s"},
		{"a limit over an hour", func(x *Task) { x.Tests[0].LimitS = MaxLimit + 1 }, "limit_s"},
		{"a hidden-decision task with no fact", func(x *Task) {
			x.Facts, x.Tests[1].Facts, x.History[2].Plants = nil, nil, nil
		}, "hides no fact"},
		{"two tests with one id", func(x *Task) { x.Tests[1].ID = x.Tests[0].ID }, "given twice"},
		{"no test", func(x *Task) { x.Tests = nil }, "no test"},
		{"max_rounds 0", func(x *Task) { x.MaxRounds = 0 }, "max_rounds"},
		{"max_rounds 6", func(x *Task) { x.MaxRounds = 6 }, "max_rounds"},
		{"max_turns 0", func(x *Task) { x.MaxTurns = 0 }, "max_turns"},
		{"no prompt", func(x *Task) { x.Prompt = " " }, "no prompt"},
		{"no note", func(x *Task) { x.Note = "" }, "no note"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			x := base(t)
			c.change(&x)
			err := x.Validate()
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("Validate = %v, want an error saying %q", err, c.want)
			}
			for _, w := range Forbid(x) {
				if len(w) >= 4 && strings.Contains(strings.ToLower(err.Error()), strings.ToLower(w)) {
					t.Fatalf("the error names a value or repository name: %v", err)
				}
			}
		})
	}
}

func TestValidateAcceptsEveryTrialOfTheFixtures(t *testing.T) {
	tasks, err := LoadAll(fixtures)
	if err != nil {
		t.Fatal(err)
	}
	for _, x := range tasks {
		words := Forbid(x)
		for trial := 1; trial <= 120; trial++ {
			scenario := letters(x.ID + ".t" + strconv.Itoa(trial))
			for _, w := range words {
				if strings.Contains(scenario, letters(w)) {
					t.Fatalf("%s: trial %d's scenario holds a forbidden word", x.ID, trial)
				}
			}
		}
	}
}

func TestInScenario(t *testing.T) {
	id := letters("hidden-parse")
	for _, k := range []string{"parse", "hiddenp", "parset", "parset12", "t123456", "set3"} {
		if !inScenario(k, id) {
			t.Errorf("%q is inside some scenario of hidden-parse", k)
		}
	}
	for _, k := range []string{"parsex", "parset1x", "tt12", "hiddent1"} {
		if inScenario(k, id) {
			t.Errorf("%q is inside no scenario of hidden-parse", k)
		}
	}
}

func TestValidateOverlays(t *testing.T) {
	cases := []struct {
		name    string
		fixture string
		change  func(t *testing.T, dir string)
		want    string
	}{
		{"a link to a file", "hidden-parse", func(t *testing.T, dir string) {
			if err := os.Symlink("parse.go", filepath.Join(dir, "solution", "ledgerkit", "other.go")); err != nil {
				t.Fatal(err)
			}
		}, "link"},
		{"a link to a folder", "hidden-parse", func(t *testing.T, dir string) {
			if err := os.Symlink(filepath.Join(dir, "solution"), filepath.Join(dir, "hidden", "ledgerkit", "sub")); err != nil {
				t.Fatal(err)
			}
		}, "link"},
		{"an overlay that is a link", "hidden-parse", func(t *testing.T, dir string) {
			if err := os.RemoveAll(filepath.Join(dir, "decoy", "ledgerkit")); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(filepath.Join(dir, "solution", "ledgerkit"), filepath.Join(dir, "decoy", "ledgerkit")); err != nil {
				t.Fatal(err)
			}
		}, "link"},
		{"an overlay folder that is a link", "control-max", func(t *testing.T, dir string) {
			if err := os.Symlink(filepath.Join(dir, "solution"), filepath.Join(dir, "start")); err != nil {
				t.Fatal(err)
			}
		}, "link"},
		{"a path that is not clean", "hidden-parse", func(t *testing.T, dir string) {
			write(t, dir, `hidden/ledgerkit/x\..\..\y.go`, "package ledgerkit\n")
		}, "not clean"},
		{"a path in git's own folder", "hidden-parse", func(t *testing.T, dir string) {
			write(t, dir, "start/ledgerkit/.git/config", "[core]\n")
		}, "git's own folder"},
		{"a path in git's own folder, in capitals", "hidden-parse", func(t *testing.T, dir string) {
			write(t, dir, "start/ledgerkit/sub/.GIT/HEAD", "x\n")
		}, "git's own folder"},
		{"an overlay of a repository the task lacks", "hidden-parse", func(t *testing.T, dir string) {
			write(t, dir, "hidden/tallybook/x_test.go", "package tallybook\n")
		}, "other than the task's repositories"},
		{"a file beside the repositories", "hidden-parse", func(t *testing.T, dir string) {
			write(t, dir, "solution/notes.txt", "x\n")
		}, "other than the task's repositories"},
		{"a decoy outside a hidden-decision task", "control-max", func(t *testing.T, dir string) {
			write(t, dir, "decoy/ledgerkit/max.go", "package ledgerkit\n")
		}, "only a hidden-decision task"},
		{"an overlay that is a file", "control-max", func(t *testing.T, dir string) {
			write(t, dir, "start", "x\n")
		}, "not a folder"},
		{"a named pipe", "control-max", func(t *testing.T, dir string) {
			if err := mkfifo(filepath.Join(dir, "solution", "ledgerkit", "pipe")); err != nil {
				t.Fatal(err)
			}
		}, "neither a folder nor a file"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir := copyFixture(t, c.fixture)
			if _, err := Load(dir); err != nil {
				t.Fatalf("the copy before the change: %v", err)
			}
			c.change(t, dir)
			_, err := Load(dir)
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("Load = %v, want an error saying %q", err, c.want)
			}
		})
	}
}

func TestOverlayPath(t *testing.T) {
	for rel, want := range map[string]string{
		"a.go":          "",
		"sub/a_test.go": "",
		"..a/b.go":      "",
		"/etc/x":        "absolute",
		"":              "not clean",
		"a//b":          "not clean",
		"a/./b":         "not clean",
		"a/../b":        "not clean",
		"a/":            "not clean",
		`a\b`:           "not clean",
		"..":            "leaves",
		"../a":          "leaves",
		".git/config":   "git's own folder",
		"sub/.Git/HEAD": "git's own folder",
	} {
		err := overlayPath(rel)
		switch {
		case want == "" && err != nil:
			t.Errorf("overlayPath(%q) = %v", rel, err)
		case want != "" && (err == nil || !strings.Contains(err.Error(), want)):
			t.Errorf("overlayPath(%q) = %v, want an error saying %q", rel, err, want)
		}
	}
}

// TestImports holds the package's code to the standard library and the
// results store.
func TestImports(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		parsed, err := parser.ParseFile(token.NewFileSet(), f, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatal(err)
		}
		for _, imp := range parsed.Imports {
			p, _ := strconv.Unquote(imp.Path.Value)
			first, _, _ := strings.Cut(p, "/")
			if strings.Contains(first, ".") && p != "github.com/heyNag/eva-md/internal/eval/results" {
				t.Errorf("%s imports %s", f, p)
			}
		}
	}
}
