package task

import (
	"encoding/json"
	"errors"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/heyNag/eva-md/internal/host/adapter"
)

// moduleRoot is the repository's root.
func moduleRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", "..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Fatal(err)
	}
	return root
}

// committed is the folder of the committed tasks.
func committed(t *testing.T) string {
	return filepath.Join(moduleRoot(t), "eval", "pilot", "testdata", "tasks")
}

// copyTasks copies the committed tasks with the ids into a new tasks
// folder and returns it.
func copyTasks(t *testing.T, ids ...string) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "tasks")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, id := range ids {
		if err := copyTree(filepath.Join(committed(t), id), filepath.Join(dir, id)); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// readTask decodes the task.json of the task folder dir.
func readTask(t *testing.T, dir string) Task {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, "task.json"))
	if err != nil {
		t.Fatal(err)
	}
	var task Task
	if err := json.Unmarshal(data, &task); err != nil {
		t.Fatal(err)
	}
	return task
}

// editTask rewrites the task.json of the task folder dir through edit.
func editTask(t *testing.T, dir string, edit func(*Task)) {
	t.Helper()
	task := readTask(t, dir)
	edit(&task)
	data, err := json.MarshalIndent(task, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(dir, "task.json"), string(data))
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// refusal returns the one *Error a joined error from Load holds.
func refusal(t *testing.T, err error) *Error {
	t.Helper()
	var joined interface{ Unwrap() []error }
	if !errors.As(err, &joined) || len(joined.Unwrap()) != 1 {
		t.Fatalf("Load gave %v, want one task refused", err)
	}
	var e *Error
	if !errors.As(joined.Unwrap()[0], &e) {
		t.Fatalf("Load gave %v, not a rule broken", err)
	}
	return e
}

// The committed tasks load, and are what the card's item 3 asks for: five
// handoffs h01 to h05 and five hidden decisions d01 to d05; three or more
// in each language; a handoff across two repositories run from outside
// both; a hidden decision whose fact is a name; no voice turn, no control
// task and no JavaScript.
func TestTheCommittedTasks(t *testing.T) {
	tasks, err := Load(committed(t))
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	langs := map[string]int{}
	outside, named := false, false
	for _, task := range tasks {
		ids = append(ids, task.ID)
		langs[task.Lang]++
		if task.Family == FamilyHandoff && task.Workdir == WorkdirOutside && len(task.Repos) == 2 {
			outside = true
		}
		for _, f := range task.Facts {
			if task.Family == FamilyHidden && f.Name {
				named = true
			}
		}
		if want := map[string]string{"h": FamilyHandoff, "d": FamilyHidden}[task.ID[:1]]; task.Family != want {
			t.Errorf("%s is a %s", task.ID, task.Family)
		}
		for _, turn := range task.History {
			if turn.By != "user" && turn.By != "agent" {
				t.Errorf("%s has a turn by %s", task.ID, turn.By)
			}
		}
		err := filepath.WalkDir(task.Dir, func(path string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return err
			}
			ext := filepath.Ext(path)
			other := map[string]string{LangGo: ".py", LangPython: ".go"}[task.Lang]
			if ext == ".js" || ext == ".mjs" || ext == ".ts" || ext == other {
				t.Errorf("%s holds %s", task.ID, path)
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	if want := []string{"d01", "d02", "d03", "d04", "d05", "h01", "h02", "h03", "h04", "h05"}; !slices.Equal(ids, want) {
		t.Errorf("the tasks are %v, want %v", ids, want)
	}
	if len(langs) != 2 || langs[LangGo] < 3 || langs[LangPython] < 3 {
		t.Errorf("the tasks' languages are %v", langs)
	}
	if !outside {
		t.Error("no handoff runs across two repositories from outside both")
	}
	if !named {
		t.Error("no hidden decision's fact is a name")
	}
}

// pythonStdlib are the modules of Python's standard library a task may
// import: a short list, so a task that wants another adds it here.
var pythonStdlib = []string{
	"argparse", "collections", "csv", "dataclasses", "datetime", "decimal", "functools", "io", "itertools",
	"json", "math", "os", "pathlib", "re", "shutil", "string", "subprocess", "sys", "tempfile", "textwrap",
	"typing", "unittest",
}

var (
	pyImport = regexp.MustCompile(`^\s*import\s+(.+)$`)
	pyFrom   = regexp.MustCompile(`^\s*from\s+(\S+)\s+import\b`)
)

// The tasks use the standard library alone: no Go file imports a module on
// a host (its go.mod requires none, which the go.mod rule holds), and every
// Python module imported is in pythonStdlib or a module of the repository's
// own, in any of its trees. A task that imported anything else would pass
// on a machine where it is installed and fail on one where it is not.
func TestTheTasksUseTheStandardLibraryOnly(t *testing.T) {
	tasks, err := Load(committed(t))
	if err != nil {
		t.Fatal(err)
	}
	goFiles, pyFiles := 0, 0
	for _, task := range tasks {
		for _, repo := range task.Repos {
			own := map[string]bool{}
			var files []string
			for _, tree := range []string{treeRepos, treeHidden, treeSolution, treeDecoy} {
				root := filepath.Join(task.Dir, tree, repo)
				err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
					if errors.Is(err, os.ErrNotExist) && path == root {
						return filepath.SkipDir
					}
					if err != nil || d.IsDir() {
						return err
					}
					if rel, _ := filepath.Rel(root, path); filepath.Ext(rel) == ".py" {
						own[strings.SplitN(filepath.ToSlash(rel), "/", 2)[0]] = true
						own[strings.TrimSuffix(filepath.Base(rel), ".py")] = true
					}
					files = append(files, path)
					return nil
				})
				if err != nil {
					t.Fatal(err)
				}
			}
			for _, path := range files {
				rel, _ := filepath.Rel(task.Dir, path)
				switch filepath.Ext(path) {
				case ".go":
					goFiles++
					f, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
					if err != nil {
						t.Fatal(err)
					}
					for _, imp := range f.Imports {
						p, _ := strconv.Unquote(imp.Path.Value)
						if first, _, _ := strings.Cut(p, "/"); strings.Contains(first, ".") {
							t.Errorf("%s %s imports a module on a host", task.ID, rel)
						}
					}
				case ".py":
					pyFiles++
					data, err := os.ReadFile(path)
					if err != nil {
						t.Fatal(err)
					}
					for _, line := range strings.Split(string(data), "\n") {
						var mods []string
						if m := pyFrom.FindStringSubmatch(line); m != nil {
							mods = append(mods, m[1])
						} else if m := pyImport.FindStringSubmatch(line); m != nil {
							for _, part := range strings.Split(m[1], ",") {
								if fields := strings.Fields(part); len(fields) > 0 {
									mods = append(mods, fields[0])
								}
							}
						}
						for _, mod := range mods {
							top, _, _ := strings.Cut(mod, ".")
							if top != "" && !own[top] && !slices.Contains(pythonStdlib, top) {
								t.Errorf("%s %s imports %s, which is neither the standard library's nor the repository's", task.ID, rel, top)
							}
						}
					}
				}
			}
		}
	}
	if goFiles < 10 || pyFiles < 10 {
		t.Fatalf("read %d Go files and %d Python files; the walk read the wrong folders", goFiles, pyFiles)
	}
}

// The Python import patterns find the modules a line imports, so the
// check above can fail.
func TestTheImportPatterns(t *testing.T) {
	for line, want := range map[string]string{
		"import requests":                "requests",
		"    import yaml as y":           "yaml",
		"from numpy.linalg import norm":  "numpy.linalg",
		"import os, requests":            "os, requests",
		"from . import agenda":           ".",
		"# a comment that says import x": "",
	} {
		got := ""
		if m := pyFrom.FindStringSubmatch(line); m != nil {
			got = m[1]
		} else if m := pyImport.FindStringSubmatch(line); m != nil {
			got = m[1]
		}
		if !strings.HasPrefix(got, want) || (want == "" && got != "") {
			t.Errorf("%q gives %q, want %q", line, got, want)
		}
	}
}

// eva-md's Go version, which no task's go.mod may pass, is the one its
// go.mod names.
func TestEvaGoIsTheModulesGo(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(moduleRoot(t), "go.mod"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "\ngo "+EvaGo+"\n") {
		t.Errorf("go.mod does not name go %s", EvaGo)
	}
}

// Forbid gives every value and every repository's name.
func TestForbid(t *testing.T) {
	task := readTask(t, filepath.Join(committed(t), "h05"))
	want := []string{"amount_minor", "reversal", "ledgerhub", "tallyport"}
	if got := Forbid(task); !slices.Equal(got, want) {
		t.Errorf("Forbid = %v, want %v", got, want)
	}
}

// Each rule that runs nothing, broken in a copy of a committed task: Load
// refuses that task alone, naming the task and the rule, and no message
// holds a value or a repository's name.
func TestLoadRefusesEachRule(t *testing.T) {
	host := adapter.All()[0].Names.CLI
	cases := []struct {
		name, id, rule string
		edit           func(t *testing.T, dir string)
	}{
		{"another format", "h01", ruleFormat, edited(func(k *Task) { k.V = 2 })},
		{"an unknown field", "h01", ruleFormat, func(t *testing.T, dir string) {
			raw(t, dir, func(s string) string { return strings.Replace(s, `"v": 1,`, `"v": 1, "voice": true,`, 1) })
		}},
		{"an unknown field named for a value", "h01", ruleFormat, func(t *testing.T, dir string) {
			raw(t, dir, func(s string) string { return strings.Replace(s, `"v": 1,`, `"v": 1, "widestword": true,`, 1) })
		}},
		{"something after the object", "h01", ruleFormat, func(t *testing.T, dir string) {
			raw(t, dir, func(s string) string { return s + "{}" })
		}},
		{"a control task", "h01", ruleID, edited(func(k *Task) { k.Family = "control" })},
		{"an id past five", "h01", ruleID, edited(func(k *Task) { k.ID = "h06" })},
		{"a handoff's id for a hidden decision", "d01", ruleID, edited(func(k *Task) { k.ID = "h01" })},
		{"a folder not named for the id", "h01", ruleID, edited(func(k *Task) { k.ID = "h02" })},
		{"JavaScript", "h04", ruleLang, edited(func(k *Task) { k.Lang = "javascript" })},
		{"three repositories", "h05", ruleRepos, edited(func(k *Task) { k.Repos = append(k.Repos, "thirdrepo") })},
		{"two repositories run in one", "h05", ruleRepos, edited(func(k *Task) { k.Workdir = WorkdirRepo })},
		{"an unknown workdir", "h01", ruleRepos, edited(func(k *Task) { k.Workdir = "inside" })},
		{"a repository's name too short", "h01", ruleRepos, edited(func(k *Task) { k.Repos = []string{"quill"} })},
		{"a repository's name of hex digits", "h01", ruleRepos, edited(func(k *Task) { k.Repos = []string{"c0ffee12"} })},
		{"a repository's name that is a path", "h01", ruleRepos, edited(func(k *Task) { k.Repos = []string{"../quillmark"} })},
		{"max_rounds past five", "h01", ruleLimits, edited(func(k *Task) { k.MaxRounds = 6 })},
		{"no round", "h01", ruleLimits, edited(func(k *Task) { k.MaxRounds = 0 })},
		{"no turn", "h01", ruleLimits, edited(func(k *Task) { k.MaxTurns = 0 })},
		{"no minute", "h01", ruleLimits, edited(func(k *Task) { k.Minutes = 0 })},
		{"no fact", "h01", ruleValues, edited(func(k *Task) { k.Facts = nil })},
		{"a fact with no value", "h01", ruleValues, edited(func(k *Task) { k.Facts[0].Values = nil })},
		{"a fact listed twice", "h01", ruleValues, edited(func(k *Task) { k.Facts[1].ID = k.Facts[0].ID })},
		{"a fact's id that holds a value", "h01", ruleValues, edited(func(k *Task) { renameFact(k, "f1", "widestword-1") })},
		{"a fact's id that holds a repository's name", "h01", ruleValues, edited(func(k *Task) { renameFact(k, "f2", "quillmark") })},
		{"a test's id that holds a value", "h01", ruleTests, edited(func(k *Task) { k.Tests[2].ID = "uniqtally" })},
		{"a test's id that is not an id", "h01", ruleTests, edited(func(k *Task) { k.Tests[0].ID = "T 1" })},
		{"a value of five letters", "h01", ruleValues, edited(func(k *Task) { k.Facts[0].Values = []string{"widest"[:5]} })},
		{"a value of hex digits", "h01", ruleValues, edited(func(k *Task) { k.Facts[0].Values = []string{"BEEF-1234"} })},
		{"a value inside a metric", "h01", ruleValues, edited(func(k *Task) { k.Facts[0].Values = []string{"First_Try"} })},
		{"a value inside an arm", "h01", ruleValues, edited(func(k *Task) { k.Facts[0].Values = []string{"oracle"} })},
		{"a value inside a scenario", "h01", ruleValues, edited(func(k *Task) { k.Facts[0].Values = []string{"h01.eva.t12"} })},
		{"a value inside a host", "h01", ruleValues, edited(func(k *Task) { k.Facts[0].Values = []string{host} })},
		{"a short history", "h01", ruleHistory, edited(func(k *Task) {
			for i := range k.History {
				k.History[i].Text = k.History[i].Text[:len(k.History[i].Text)/3]
			}
		})},
		{"a history that ends with the user", "h01", ruleHistory, edited(func(k *Task) {
			k.History = append(k.History, Turn{By: "user", Text: "Thanks."})
		})},
		{"a voice turn", "h01", ruleHistory, edited(func(k *Task) { k.History[0].By = "voice" })},
		{"a fact no turn plants", "h01", rulePlanted, edited(func(k *Task) {
			for i := range k.History {
				k.History[i].Plants = slices.DeleteFunc(k.History[i].Plants, func(id string) bool { return id == "f2" })
			}
		})},
		{"a turn planting a fact the task lacks", "h01", rulePlanted, edited(func(k *Task) { k.History[0].Plants = []string{"f9"} })},
		{"a turn planting a fact it does not state", "h01", rulePlanted, edited(func(k *Task) { k.History[0].Plants = []string{"f1"} })},
		{"no test", "h01", ruleTests, edited(func(k *Task) { k.Tests = nil })},
		{"a test listed twice", "h01", ruleTests, edited(func(k *Task) { k.Tests[1].ID = k.Tests[0].ID })},
		{"a test in no repository", "h05", ruleTests, edited(func(k *Task) { k.Tests[0].Repo = "otherrepo" })},
		{"a test through a shell", "h04", ruleTests, edited(func(k *Task) { k.Tests[0].Run = []string{"sh", "-c", "python3 -m unittest"} })},
		{"a Python test in a Go task", "h01", ruleTests, edited(func(k *Task) { k.Tests[0].Run = []string{"python3", "-m", "unittest", "x"} })},
		{"a go command that is not go test", "h01", ruleTests, edited(func(k *Task) { k.Tests[0].Run = []string{"go", "run", "."} })},
		{"an empty argument", "h04", ruleTests, edited(func(k *Task) { k.Tests[0].Run = append(k.Tests[0].Run, "") })},
		{"a test encoding a fact the task lacks", "h01", ruleTests, edited(func(k *Task) { k.Tests[0].Facts = []string{"f9"} })},
		{"a hidden decision's fact no test encodes", "d01", ruleEncoded, edited(func(k *Task) { k.Tests[1].Facts = nil })},
		{"a note missing a fact", "h01", ruleNote, edited(func(k *Task) { k.Note = "Hand-over: see the chat." })},
		{"a prompt holding a value", "h04", rulePrompt, edited(func(k *Task) { k.Prompt += " Start with the snooze command." })},
		{"a prompt holding a value in other letters", "d05", rulePrompt, edited(func(k *Task) { k.Prompt += " Use Quarnel Rejects." })},
		{"an empty prompt", "h01", rulePrompt, edited(func(k *Task) { k.Prompt = " " })},
		{"a file beside task.json", "h01", ruleLayout, file("NOTES.txt", "notes\n")},
		{"a decoy for a handoff", "h01", ruleLayout, file("decoy/quillmark/stats.go", "package quillmark\n")},
		{"a hidden decision with no decoy", "d01", ruleLayout, remove("decoy")},
		{"a repository missing from repos", "h05", ruleLayout, remove("repos/ledgerhub")},
		{"no hidden tests for a test's repository", "h01", ruleLayout, remove("hidden/quillmark")},
		{"a file where a repository's folder goes", "h01", ruleLayout, file("solution/stray.txt", "x\n")},
		{"an overlay for no repository", "h01", ruleOverlay, file("solution/otherrepo/x.go", "package x\n")},
		{"a link in an overlay", "h01", ruleOverlay, func(t *testing.T, dir string) {
			if err := os.Symlink(filepath.Join("..", "..", "repos", "quillmark", "go.mod"), filepath.Join(dir, "solution", "quillmark", "go.mod")); err != nil {
				t.Fatal(err)
			}
		}},
		{"a .git folder", "h01", ruleOverlay, file("repos/quillmark/.git/HEAD", "ref: refs/heads/main\n")},
		{"a repository over 300 lines", "h04", ruleSize, file("repos/pebblewick/data.txt", strings.Repeat("x\n", 300))},
		{"a solution over 300 lines", "h04", ruleSize, file("solution/pebblewick/data.txt", strings.Repeat("x\n", 300))},
		{"a requirement", "h01", ruleGoMod, file("repos/quillmark/go.mod", "module quillmark\n\ngo 1.22\n\nrequire other v1.0.0\n")},
		{"a toolchain line", "h01", ruleGoMod, file("repos/quillmark/go.mod", "module quillmark\n\ngo 1.22\ntoolchain go1.22.1\n")},
		{"a module on a host", "h01", ruleGoMod, file("repos/quillmark/go.mod", "module example.com/quillmark\n\ngo 1.22\n")},
		{"a Go newer than eva-md's", "h01", ruleGoMod, file("repos/quillmark/go.mod", "module quillmark\n\ngo 1.26.1\n")},
		{"a Go two minors newer", "h01", ruleGoMod, file("repos/quillmark/go.mod", "module quillmark\n\ngo 1.100\n")},
		{"no go.mod", "h01", ruleGoMod, remove("repos/quillmark/go.mod")},
		{"a go.mod in an overlay with a requirement", "h01", ruleGoMod, file("solution/quillmark/go.mod", "module quillmark\n\ngo 1.22\nrequire other v1.0.0\n")},
		{"a value in a repository's file", "d01", ruleLeak, func(t *testing.T, dir string) {
			path := filepath.Join(dir, "repos", "harrowgate", "sched.go")
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			writeFile(t, path, string(data)+"\n// Lines are tagged [Pel-Mora].\n")
		}},
		{"a value in a repository's path", "d03", ruleLeak, file("repos/marlbrook/quorvane.txt", "x\n")},
		{"a value in the decoy", "d04", ruleLeak, file("decoy/veltmoor/NOTES.md", "header: brisolde\n")},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir := copyTasks(t, c.id, "d02")
			orig := readTask(t, filepath.Join(dir, c.id))
			c.edit(t, filepath.Join(dir, c.id))
			tasks, err := Load(dir)
			e := refusal(t, err)
			if e.Rule != c.rule || e.Task != c.id || !strings.Contains(e.Error(), "task "+c.id+": "+c.rule+": ") {
				t.Errorf("Load refused %s for %q, want %s", e.Task, e.Error(), c.rule)
			}
			if len(tasks) != 1 || tasks[0].ID != "d02" {
				t.Errorf("Load kept %d tasks, want d02 alone", len(tasks))
			}
			mutated := readTaskLoose(filepath.Join(dir, c.id))
			if words := append(Forbid(orig), Forbid(mutated)...); holds(e.Error(), words) {
				t.Errorf("the message %q holds a value or a repository's name", e.Error())
			}
		})
	}
}

// renameFact gives a fact a new id, in every turn and test that names it.
func renameFact(k *Task, from, to string) {
	for i := range k.Facts {
		if k.Facts[i].ID == from {
			k.Facts[i].ID = to
		}
	}
	for i := range k.History {
		for j, id := range k.History[i].Plants {
			if id == from {
				k.History[i].Plants[j] = to
			}
		}
	}
	for i := range k.Tests {
		for j, id := range k.Tests[i].Facts {
			if id == from {
				k.Tests[i].Facts[j] = to
			}
		}
	}
}

// readTaskLoose decodes what it can of a task.json, for its words.
func readTaskLoose(dir string) Task {
	var task Task
	data, _ := os.ReadFile(filepath.Join(dir, "task.json"))
	_ = json.Unmarshal(data, &task)
	return task
}

// edited returns an edit of a task's task.json.
func edited(edit func(*Task)) func(*testing.T, string) {
	return func(t *testing.T, dir string) { editTask(t, dir, edit) }
}

// raw edits a task.json as text.
func raw(t *testing.T, dir string, edit func(string) string) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, "task.json"))
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(dir, "task.json"), edit(string(data)))
}

// file returns an edit that writes a file of the task.
func file(rel, content string) func(*testing.T, string) {
	return func(t *testing.T, dir string) { writeFile(t, filepath.Join(dir, filepath.FromSlash(rel)), content) }
}

// remove returns an edit that removes a path of the task.
func remove(rel string) func(*testing.T, string) {
	return func(t *testing.T, dir string) {
		if err := os.RemoveAll(filepath.Join(dir, filepath.FromSlash(rel))); err != nil {
			t.Fatal(err)
		}
	}
}

// A tasks folder holding a file, or nothing, is refused; one Load cannot
// list is an error of its own.
func TestLoadRefusesAFolderOfNoTasks(t *testing.T) {
	dir := copyTasks(t, "h01")
	writeFile(t, filepath.Join(dir, "README.md"), "tasks\n")
	tasks, err := Load(dir)
	if e := refusal(t, err); e.Rule != ruleLayout || len(tasks) != 1 {
		t.Errorf("a stray file: %v, %d tasks", err, len(tasks))
	}
	if _, err := Load(t.TempDir()); err == nil {
		t.Error("an empty folder loaded")
	}
	if _, err := Load(filepath.Join(t.TempDir(), "none")); err == nil {
		t.Error("a missing folder loaded")
	}
}
