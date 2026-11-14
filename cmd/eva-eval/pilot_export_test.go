package main

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	contask "github.com/heyNag/eva-md/internal/eval/continuation/task"
	"github.com/heyNag/eva-md/internal/eval/pilot/task"
	"github.com/heyNag/eva-md/internal/eval/results"
)

// pilotIDs are the committed tasks' ids.
var pilotIDs = []string{"d01", "d02", "d03", "d04", "d05", "h01", "h02", "h03", "h04", "h05"}

// copyTasksAsTheyAre copies the committed tasks with the ids into a new
// tasks folder, keeping whether each file is executable, and returns it.
func copyTasksAsTheyAre(t *testing.T, ids ...string) string {
	t.Helper()
	dst := filepath.Join(t.TempDir(), "tasks")
	for _, id := range ids {
		if err := os.CopyFS(filepath.Join(dst, id), os.DirFS(filepath.Join(pilotTasks(t), id))); err != nil {
			t.Fatal(err)
		}
	}
	return dst
}

// snapshot maps every path under dir, by its slash path relative to dir,
// to what it is: a folder, a link and where it leads, or a file, whether
// it is executable and its bytes. A dir that does not exist is "absent".
func snapshot(t *testing.T, dir string) map[string]string {
	t.Helper()
	if _, err := os.Lstat(dir); os.IsNotExist(err) {
		return map[string]string{".": "absent"}
	}
	snap := map[string]string{}
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(dir, path)
		rel = filepath.ToSlash(rel)
		info, err := d.Info()
		if err != nil {
			return err
		}
		switch {
		case d.IsDir():
			snap[rel] = "folder"
		case d.Type()&fs.ModeSymlink != 0:
			to, err := os.Readlink(path)
			snap[rel] = "link to " + to
			return err
		default:
			data, err := os.ReadFile(path)
			snap[rel] = fmt.Sprintf("file executable=%v %q", info.Mode().Perm()&0o111 != 0, data)
			return err
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return snap
}

// pilotWords are the values the committed tasks plant and their
// repositories' names, and the value the continuation's refusal plants.
func pilotWords(t *testing.T) []string {
	t.Helper()
	all, err := task.Load(pilotTasks(t))
	if err != nil {
		t.Fatal(err)
	}
	words := []string{"continuation"}
	for _, x := range all {
		words = append(words, task.Forbid(x)...)
	}
	return words
}

// noValue fails the test when a text holds one of words.
func noValue(t *testing.T, words []string, texts ...string) {
	t.Helper()
	for _, text := range texts {
		if err := results.Forbidden([]results.Row{{Scenario: text}}, words); err != nil {
			t.Errorf("a value or repository name is shown: %q", text)
		}
	}
}

// firstFile returns the first plain file under dir.
func firstFile(t *testing.T, dir string) string {
	t.Helper()
	var first string
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err == nil && first == "" && d.Type().IsRegular() {
			first = path
		}
		return err
	})
	if err != nil || first == "" {
		t.Fatalf("no file under the folder: %v", err)
	}
	return first
}

// expectedExport is the snapshot an export of the tasks in src should
// give, task.json files aside, which the test reads apart: each task's
// overlays at tasks/<id>/ and each of its repositories at repos/<name>/,
// as they are in src.
func expectedExport(t *testing.T, src string, ids []string) map[string]string {
	t.Helper()
	want := map[string]string{".": "folder", "tasks": "folder", "repos": "folder"}
	for _, id := range ids {
		want["tasks/"+id] = "folder"
		for rel, what := range snapshot(t, filepath.Join(src, id)) {
			switch top, _, _ := strings.Cut(rel, "/"); top {
			case ".", "task.json":
			case "repos":
				if rel != "repos" {
					want[rel] = what
				}
			default:
				want["tasks/"+id+"/"+rel] = what
			}
		}
	}
	return want
}

// withoutTaskJSON returns snap without the task.json files of tasks/.
func withoutTaskJSON(snap map[string]string) map[string]string {
	out := map[string]string{}
	for rel, what := range snap {
		if parts := strings.Split(rel, "/"); len(parts) == 3 && parts[0] == "tasks" && parts[2] == contask.FileName {
			continue
		}
		out[rel] = what
	}
	return out
}

// emptied returns v with every false, empty list and empty object left out
// of the objects in it, as fields with omitempty are.
func emptied(v any) any {
	switch x := v.(type) {
	case map[string]any:
		out := map[string]any{}
		for k, e := range x {
			e = emptied(e)
			if e == false || reflect.DeepEqual(e, []any{}) || reflect.DeepEqual(e, map[string]any{}) {
				continue
			}
			out[k] = e
		}
		return out
	case []any:
		out := []any{}
		for _, e := range x {
			out = append(out, emptied(e))
		}
		return out
	}
	return v
}

func readJSON(t *testing.T, path string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatal(err)
	}
	return m
}

// The ten committed tasks export with exit 0, a line a task and the
// session timeout. The folder is the owner's alone and holds exactly the
// tasks' task.json files, overlays and repositories, each file with its
// source's bytes and executable bit; each task.json holds every field both
// formats have, as the pilot's has it, and no other; the continuation
// loads all ten and its check passes them; each task's Forbid is the
// pilot's. A second export gives the same bytes, and the home, the working
// folder and the source are as they were.
func TestPilotExportTheCommittedTasks(t *testing.T) {
	pilotHome(t)
	words := pilotWords(t)
	src := copyTasksAsTheyAre(t, pilotIDs...)
	for _, file := range []string{firstFile(t, filepath.Join(src, "h05", "repos")), firstFile(t, filepath.Join(src, "d01", "decoy"))} {
		if err := os.Chmod(file, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	work := t.TempDir()
	t.Chdir(work)
	home := os.Getenv("HOME")
	homeBefore, workBefore, srcBefore := snapshot(t, home), snapshot(t, work), snapshot(t, src)

	out := filepath.Join(t.TempDir(), "export")
	code, stdout, errOut := cmd("pilot", "export", "--tasks", src, "--out", out)
	want := "d01 hidden exported\nd02 hidden exported\nd03 hidden exported\nd04 hidden exported\nd05 hidden exported\n" +
		"h01 handoff exported\nh02 handoff exported\nh03 handoff exported\nh04 handoff exported\nh05 handoff exported\n" +
		"session-timeout 20m\n"
	if code != 0 || stdout != want || errOut != "" {
		t.Fatalf("pilot export: %d\n%s%s", code, stdout, errOut)
	}
	noValue(t, words, stdout, errOut)

	info, err := os.Lstat(out)
	if err != nil || !info.IsDir() || info.Mode().Perm() != 0o700 {
		t.Fatalf("the folder written: %v, %v", info.Mode(), err)
	}
	err = filepath.WalkDir(out, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := d.Info()
		if err == nil && info.Mode().Perm()&0o077 != 0 {
			t.Errorf("a path under the folder written has mode %v", info.Mode().Perm())
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}

	got := snapshot(t, out)
	if wantSnap := expectedExport(t, src, pilotIDs); !reflect.DeepEqual(withoutTaskJSON(got), wantSnap) {
		for rel := range wantSnap {
			if got[rel] != wantSnap[rel] {
				t.Errorf("%s differs from its source, or is missing", rel)
			}
		}
		for rel := range withoutTaskJSON(got) {
			if _, ok := wantSnap[rel]; !ok {
				t.Errorf("%s has no source", rel)
			}
		}
	}

	pilot, err := task.Load(src)
	if err != nil {
		t.Fatal(err)
	}
	keys := []string{"facts", "family", "history", "id", "max_rounds", "max_turns", "note", "prompt", "repos", "tests", "workdir"}
	for _, p := range pilot {
		written := readJSON(t, filepath.Join(out, "tasks", p.ID, contask.FileName))
		var gotKeys []string
		for k := range written {
			gotKeys = append(gotKeys, k)
		}
		slices.Sort(gotKeys)
		if !slices.Equal(gotKeys, keys) {
			t.Errorf("%s: task.json holds the fields %v", p.ID, gotKeys)
		}
		for _, x := range written["tests"].([]any) {
			if _, ok := x.(map[string]any)["limit_s"]; ok {
				t.Errorf("%s: a test has a time limit", p.ID)
			}
		}
		source := readJSON(t, filepath.Join(src, p.ID, "task.json"))
		for _, k := range []string{"v", "lang", "minutes"} {
			delete(source, k)
		}
		if !reflect.DeepEqual(emptied(written), emptied(source)) {
			t.Errorf("%s: a field both formats have differs", p.ID)
		}
	}

	loaded, err := contask.LoadAll(filepath.Join(out, "tasks"))
	if err != nil || len(loaded) != len(pilot) {
		t.Fatalf("the continuation loads %d tasks: %v", len(loaded), err)
	}
	for i, c := range loaded {
		p := pilot[i]
		forbid := task.Forbid(p)
		slices.Sort(forbid)
		if c.ID != p.ID || !slices.Equal(contask.Forbid(c), slices.Compact(forbid)) {
			t.Errorf("%s: the continuation forbids other words than the pilot", c.ID)
		}
	}

	code, stdout, errOut = cmd("continuation", "check", "--tasks", filepath.Join(out, "tasks"), "--repos", filepath.Join(out, "repos"))
	if want := "d01 ok\nd02 ok\nd03 ok\nd04 ok\nd05 ok\nh01 ok\nh02 ok\nh03 ok\nh04 ok\nh05 ok\n"; code != 0 || stdout != want || errOut != "" {
		t.Errorf("continuation check: %d\n%s%s", code, stdout, errOut)
	}

	again := filepath.Join(t.TempDir(), "again")
	if code, stdout, errOut := cmd("pilot", "export", "--tasks", src, "--out", again); code != 0 || stdout != want || errOut != "" {
		t.Fatalf("a second export: %d\n%s%s", code, stdout, errOut)
	}
	if !reflect.DeepEqual(snapshot(t, again), got) {
		t.Error("a second export of one source gives other bytes")
	}

	if !reflect.DeepEqual(snapshot(t, home), homeBefore) {
		t.Error("the export changed the home")
	}
	if !reflect.DeepEqual(snapshot(t, work), workBefore) {
		t.Error("the export changed the working folder")
	}
	if !reflect.DeepEqual(snapshot(t, src), srcBefore) {
		t.Error("the export changed its source")
	}
}

// Run from the repository's root with no --tasks, the export reads the
// committed tasks; with --task it checks and writes only the tasks named,
// and only their repositories.
func TestPilotExportOnlyTheTasksNamed(t *testing.T) {
	pilotHome(t)
	words, committed := pilotWords(t), pilotTasks(t)
	t.Chdir(moduleRoot(t))
	out := filepath.Join(t.TempDir(), "export")
	code, stdout, errOut := cmd("pilot", "export", "--out", out, "--task", "h04", "--task", "d03", "--task", "h04")
	if want := "d03 hidden exported\nh04 handoff exported\nsession-timeout 20m\n"; code != 0 || stdout != want || errOut != "" {
		t.Fatalf("pilot export: %d\n%s%s", code, stdout, errOut)
	}
	noValue(t, words, stdout, errOut)
	if got, want := withoutTaskJSON(snapshot(t, out)), expectedExport(t, committed, []string{"d03", "h04"}); !reflect.DeepEqual(got, want) {
		t.Errorf("the export holds %d paths, want %d", len(got), len(want))
	}
}

// Each refusal of a usage, an out folder or a repository two tasks name
// exits 2, says why on stderr, shows no value, and writes nothing.
func TestPilotExportRefusesBeforeAnyCheck(t *testing.T) {
	pilotHome(t)
	words := pilotWords(t)
	src := copyTasksAsTheyAre(t, "h04", "d04")
	clash := copyTasksAsTheyAre(t, "h04")
	if err := os.CopyFS(filepath.Join(clash, "h03"), os.DirFS(filepath.Join(clash, "h04"))); err != nil {
		t.Fatal(err)
	}
	replaceIn(t, filepath.Join(clash, "h03", "task.json"), `"id": "h04"`, `"id": "h03"`)
	linkToSrc := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(src, linkToSrc); err != nil {
		t.Fatal(err)
	}
	// A folder Load does not take, whose name it does not show: its line
	// names it folder-2, a label --task does not take for an id.
	labelled := copyTasksAsTheyAre(t, "d04")
	if err := os.Mkdir(filepath.Join(labelled, "zz-not-a-task-name"), 0o755); err != nil {
		t.Fatal(err)
	}
	// The tasks spelt in another letter case: on a file system that ignores
	// case, as macOS's does, it is a second name of the folder that no link
	// gives, and the folder to write lies inside the tasks through it;
	// elsewhere it is a folder that does not exist.
	otherCase := filepath.Join(filepath.Dir(src), strings.ToUpper(filepath.Base(src)))
	caseWhy := "the folder to write is in no folder that exists"
	if a, err := os.Stat(otherCase); err == nil {
		if b, err := os.Stat(src); err == nil && os.SameFile(a, b) {
			caseWhy = "the folder to write lies inside the tasks folder"
		}
	}
	srcBefore, clashBefore, labelledBefore := snapshot(t, src), snapshot(t, clash), snapshot(t, labelled)
	// why is what stderr says of each, so a refusal is not passed for
	// another, such as a clash for the write that fails after it.
	why := map[string]string{
		"no --out":                   "pilot export needs --out",
		"an argument":                "pilot export takes no argument but its flags",
		"an unknown flag":            "a flag of pilot export is unknown or has no value",
		"a --task with no id":        "a flag of pilot export is unknown or has no value",
		"an unknown --task id":       "a task named by --task is not in the tasks folder",
		"a --task that is a value":   "a task named by --task is not in the tasks folder",
		"a --task that is a label":   "a task named by --task is not in the tasks folder",
		"inside them in other case":  caseWhy,
		"a folder that is not empty": "the folder to write is not empty",
		"a file":                     "the folder to write is not a folder",
		"a link to an empty folder":  "the folder to write is a link",
		"a link with a slash after":  "the folder to write is a link",
		"in no folder":               "the folder to write is in no folder that exists",
		"inside the tasks":           "the folder to write lies inside the tasks folder",
		"the tasks themselves":       "the folder to write is not empty",
		"inside a task":              "the folder to write lies inside the tasks folder",
		"inside them by a link":      "the folder to write lies inside the tasks folder",
		"two tasks, one repository":  "tasks h03 and h04 name one repository",
	}

	for name, c := range map[string]struct {
		setup func(base string) string // makes the out folder's surroundings in base, and returns it
		args  func(out string) []string
	}{
		"no --out": {args: func(string) []string { return []string{"--tasks", src} }},
		"an argument": {args: func(out string) []string {
			return []string{"--tasks", src, "--out", out, "extra"}
		}},
		"an unknown flag": {args: func(out string) []string { return []string{"--tasks", src, "--out", out, "--fast"} }},
		"a --task with no id": {args: func(out string) []string {
			return []string{"--tasks", src, "--out", out, "--task"}
		}},
		"an unknown --task id": {args: func(out string) []string {
			return []string{"--tasks", src, "--out", out, "--task", "h09"}
		}},
		"a --task that is a value": {args: func(out string) []string {
			return []string{"--tasks", src, "--out", out, "--task", "pebblewick"}
		}},
		"a --task that is a label": {args: func(out string) []string {
			return []string{"--tasks", labelled, "--out", out, "--task", "folder-2"}
		}},
		"inside them in other case": {setup: func(string) string { return filepath.Join(otherCase, "out") }},
		"a folder that is not empty": {
			setup: func(base string) string {
				write(t, filepath.Join(base, "out", "left.txt"), "x\n")
				return filepath.Join(base, "out")
			},
		},
		"a file": {
			setup: func(base string) string {
				write(t, filepath.Join(base, "out"), "x\n")
				return filepath.Join(base, "out")
			},
		},
		"a link to an empty folder": {
			setup: func(base string) string {
				if err := os.Mkdir(filepath.Join(base, "empty"), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(filepath.Join(base, "empty"), filepath.Join(base, "out")); err != nil {
					t.Fatal(err)
				}
				return filepath.Join(base, "out")
			},
		},
		"a link with a slash after": {
			setup: func(base string) string {
				if err := os.Mkdir(filepath.Join(base, "empty"), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(filepath.Join(base, "empty"), filepath.Join(base, "out")); err != nil {
					t.Fatal(err)
				}
				return filepath.Join(base, "out") + string(filepath.Separator)
			},
		},
		"in no folder":          {setup: func(base string) string { return filepath.Join(base, "none", "out") }},
		"inside the tasks":      {setup: func(string) string { return filepath.Join(src, "out") }},
		"the tasks themselves":  {setup: func(string) string { return src }},
		"inside a task":         {setup: func(string) string { return filepath.Join(src, "h04", "out") }},
		"inside them by a link": {setup: func(string) string { return filepath.Join(linkToSrc, "out") }},
		"two tasks, one repository": {args: func(out string) []string {
			return []string{"--tasks", clash, "--out", out}
		}},
	} {
		base := t.TempDir()
		out := filepath.Join(base, "out")
		if c.setup != nil {
			out = c.setup(base)
		}
		args := []string{"--tasks", src, "--out", out}
		if c.args != nil {
			args = c.args(out)
		}
		before := snapshot(t, base)
		code, stdout, errOut := cmd(append([]string{"pilot", "export"}, args...)...)
		if code != 2 || stdout != "" || !strings.HasPrefix(errOut, "eva-eval: ") || !strings.Contains(errOut, why[name]) || why[name] == "" {
			t.Errorf("%s: %d %q %q", name, code, stdout, errOut)
		}
		noValue(t, words, stdout, errOut)
		if !reflect.DeepEqual(snapshot(t, base), before) {
			t.Errorf("%s: the export wrote", name)
		}
	}
	// The scratch folder the check runs in, made in TMPDIR, would lie inside
	// the empty folder to write: refused, and the folder is left empty.
	t.Run("TMPDIR inside the folder to write", func(t *testing.T) {
		out := filepath.Join(t.TempDir(), "out")
		if err := os.Mkdir(out, 0o755); err != nil {
			t.Fatal(err)
		}
		t.Setenv("TMPDIR", out)
		code, stdout, errOut := cmd("pilot", "export", "--tasks", src, "--out", out)
		if code != 2 || stdout != "" || !strings.HasPrefix(errOut, "eva-eval: ") ||
			!strings.Contains(errOut, "the scratch folder, in TMPDIR, would be inside the folder to write") {
			t.Errorf("%d %q %q", code, stdout, errOut)
		}
		noValue(t, words, stdout, errOut)
		if entries, err := os.ReadDir(out); err != nil || len(entries) != 0 {
			t.Errorf("the folder to write holds %d entries, %v", len(entries), err)
		}
	})
	if !reflect.DeepEqual(snapshot(t, src), srcBefore) || !reflect.DeepEqual(snapshot(t, clash), clashBefore) ||
		!reflect.DeepEqual(snapshot(t, labelled), labelledBefore) {
		t.Error("the export wrote into the tasks")
	}
	_, _, errOut := cmd("pilot")
	if !strings.Contains(errOut, "\n  eva-eval pilot export --out dir [--tasks dir] [--task id]...") {
		t.Errorf("the pilot's usage: %q", errOut)
	}
}

// A task that Load refuses, one only the pilot's check refuses (a broken
// decoy), and one only the continuation's format refuses (a value that is
// inside every continuation metric's name) each get their line and exit
// 1; a task that cannot be checked exits 2. In each case nothing is
// written, whether the out folder did not exist or was an empty one,
// which keeps its mode.
func TestPilotExportWritesNothingForARefusedTask(t *testing.T) {
	pilotHome(t)
	words := pilotWords(t)
	loadRefuses := copyTasksAsTheyAre(t, "d04", "h04")
	replaceIn(t, filepath.Join(loadRefuses, "h04", "task.json"), `"prompt": "Let's finish`, `"prompt": "Do snooze first. Let's finish`)
	checkRefuses := copyTasksAsTheyAre(t, "d05")
	replaceIn(t, filepath.Join(checkRefuses, "d05", "decoy", "drovershaw", "importer.py"), "return len(accepted), len(rejected)", "return len(accepted)")
	formatRefuses := copyTasksAsTheyAre(t, "h01")
	file := filepath.Join(formatRefuses, "h01", "task.json")
	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, []byte(strings.ReplaceAll(string(data), "widestword", "continuation")), 0o644); err != nil {
		t.Fatal(err)
	}
	// The pilot's own check passes the task the continuation refuses.
	if code, stdout, errOut := cmd("pilot", "check", "--tasks", formatRefuses); code != 0 || stdout != "h01 handoff ok\n" {
		t.Fatalf("pilot check of the task only the continuation refuses: %d %q %q", code, stdout, errOut)
	}
	unchecked := copyTasksAsTheyAre(t, "d04")

	for name, c := range map[string]struct {
		tasks, want string
		code        int
		path        string
	}{
		"load":   {loadRefuses, "d04 hidden ok\nh04 handoff prompt: the prompt holds a value of fact f1\n", 1, ""},
		"check":  {checkRefuses, "d05 hidden decoy: hidden test t1 fails with the decoy, though it encodes no fact\n", 1, ""},
		"format": {formatRefuses, "h01 handoff driver format: a value or repository name is inside a word every row of the task holds\n", 1, ""},
		"unchecked": {unchecked, "d04 hidden cannot be checked: task d04: the runner python3 is not installed\n", 2,
			t.TempDir()},
	} {
		for _, existed := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s, existed %v", name, existed), func(t *testing.T) {
				if c.path != "" {
					t.Setenv("PATH", c.path)
				}
				refusedTask(t, words, c.tasks, c.want, c.code, existed)
			})
		}
	}
}

// refusedTask runs the export of tasks, which one refuses, into a folder
// that does or does not exist, and fails the test unless it prints want,
// exits code, and writes nothing.
func refusedTask(t *testing.T, words []string, tasks, want string, wantCode int, existed bool) {
	t.Helper()
	base := t.TempDir()
	out := filepath.Join(base, "out")
	if existed {
		if err := os.Mkdir(out, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	before := snapshot(t, base)
	code, stdout, errOut := cmd("pilot", "export", "--tasks", tasks, "--out", out)
	if code != wantCode || stdout != want || errOut != "" {
		t.Errorf("exit %d\n%s%s", code, stdout, errOut)
	}
	noValue(t, words, stdout, errOut)
	if !reflect.DeepEqual(snapshot(t, base), before) {
		t.Error("the export wrote")
	}
	if info, err := os.Stat(out); existed && (err != nil || info.Mode().Perm() != 0o755) {
		t.Errorf("the empty folder's mode is %v, %v", info.Mode().Perm(), err)
	}
}
