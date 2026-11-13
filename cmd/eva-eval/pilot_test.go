package main

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// The tests raise check's limit on each test run once, here, so a slow
// machine fails no task.
func init() { pilotCheckLimit = time.Hour }

// pilotHome gives the test a home of its own, as isolate does, for a
// pilot command that runs go. Go's build cache is the one exception
// (AGENTS.md): it is found first, with go env as go test itself finds it,
// and kept through GOCACHE, and GOENV=off keeps go from reading a
// settings file of the person running the tests. Go's telemetry is off in
// the home, as in guardGo's: a go command otherwise leaves a process behind
// that writes into the home after the command ends, while the test removes
// it (CI's macOS job failed on that once).
func pilotHome(t *testing.T) {
	t.Helper()
	out, err := exec.Command("go", "env", "GOCACHE").Output()
	cache := strings.TrimSpace(string(out))
	if err != nil || cache == "" || cache == "off" {
		t.Fatalf("go env GOCACHE gives no build cache: %v", err)
	}
	isolate(t)
	home := os.Getenv("HOME")
	config := filepath.Join(home, ".config")
	if runtime.GOOS == "darwin" {
		config = filepath.Join(home, "Library", "Application Support")
	}
	write(t, filepath.Join(config, "go", "telemetry", "mode"), "off\n")
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	t.Setenv("GOENV", "off")
	t.Setenv("GOCACHE", cache)
}

// The pilot's tests run go with a home of their own and Go's build cache
// alone kept from the caller's.
func TestPilotHome(t *testing.T) {
	cache, err := exec.Command("go", "env", "GOCACHE").Output()
	if err != nil {
		t.Fatal(err)
	}
	pilotHome(t)
	out, err := exec.Command("go", "env", "GOENV", "GOCACHE").Output()
	if err != nil {
		t.Fatal(err)
	}
	if want := "\n" + string(cache); string(out) != want {
		t.Errorf("go env GOENV GOCACHE gives %q, want %q", out, want)
	}
	if home := os.Getenv("HOME"); !strings.HasPrefix(home, os.TempDir()) {
		t.Errorf("HOME is %q, outside the temporary folder", home)
	}
}

// pilotTasks is the folder of the committed tasks.
func pilotTasks(t *testing.T) string {
	return filepath.Join(moduleRoot(t), "eval", "pilot", "testdata", "tasks")
}

// copyPilotTasks copies the committed tasks with the ids into a new
// tasks folder and returns it.
func copyPilotTasks(t *testing.T, ids ...string) string {
	t.Helper()
	dst := filepath.Join(t.TempDir(), "tasks")
	for _, id := range ids {
		src := filepath.Join(pilotTasks(t), id)
		err := filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			rel, _ := filepath.Rel(src, path)
			to := filepath.Join(dst, id, rel)
			if d.IsDir() {
				return os.MkdirAll(to, 0o755)
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			return os.WriteFile(to, data, 0o644)
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	return dst
}

// eva-eval pilot check, run from the repository's root with no flag,
// reads the committed tasks, passes every one, one line each, and exits 0.
func TestPilotCheckPassesTheCommittedTasks(t *testing.T) {
	pilotHome(t)
	t.Chdir(moduleRoot(t))
	code, out, errOut := cmd("pilot", "check")
	want := "d01 hidden ok\nd02 hidden ok\nd03 hidden ok\nd04 hidden ok\nd05 hidden ok\n" +
		"h01 handoff ok\nh02 handoff ok\nh03 handoff ok\nh04 handoff ok\nh05 handoff ok\n"
	if code != 0 || out != want || errOut != "" {
		t.Errorf("pilot check: %d\n%s%s", code, out, errOut)
	}
}

// A task that breaks a rule gets the rule on its line, whether Load or
// Check finds it, and the command exits 1; the other tasks still get
// theirs.
func TestPilotCheckNamesTheRuleBroken(t *testing.T) {
	pilotHome(t)
	dir := copyPilotTasks(t, "d04", "h04", "d05")
	replaceIn(t, filepath.Join(dir, "h04", "task.json"), `"prompt": "Let's finish`, `"prompt": "Do snooze first. Let's finish`)
	replaceIn(t, filepath.Join(dir, "d05", "decoy", "drovershaw", "importer.py"), "return len(accepted), len(rejected)", "return len(accepted)")
	if err := os.MkdirAll(filepath.Join(dir, "h09"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "h09", "task.json"), []byte("{"), 0o644); err != nil {
		t.Fatal(err)
	}
	code, out, errOut := cmd("pilot", "check", "--tasks", dir)
	want := "d04 hidden ok\n" +
		"d05 hidden decoy: hidden test t1 fails with the decoy, though it encodes no fact\n" +
		"h04 handoff prompt: the prompt holds a value of fact f1\n" +
		"h09 - format: task.json is not a task: it does not decode\n"
	if code != 1 || out != want || errOut != "" {
		t.Errorf("pilot check: %d\n%s%s", code, out, errOut)
	}
}

// A rule Check alone finds broken, with every task loaded, exits 1 too,
// and a folder named for a value or holding a line break of its own is
// not named on its line.
func TestPilotCheckExitsOneOnARuleCheckFinds(t *testing.T) {
	pilotHome(t)
	dir := copyPilotTasks(t, "d05")
	replaceIn(t, filepath.Join(dir, "d05", "decoy", "drovershaw", "importer.py"), "return len(accepted), len(rejected)", "return len(accepted)")
	code, out, errOut := cmd("pilot", "check", "--tasks", dir)
	if want := "d05 hidden decoy: hidden test t1 fails with the decoy, though it encodes no fact\n"; code != 1 || out != want || errOut != "" {
		t.Errorf("pilot check: %d\n%s%s", code, out, errOut)
	}

	dir = t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "drovershaw\nd09 hidden ok"), 0o755); err != nil {
		t.Fatal(err)
	}
	code, out, errOut = cmd("pilot", "check", "--tasks", dir)
	if want := "folder-1 - format: cannot read task.json: open: no such file or directory\n"; code != 1 || out != want || errOut != "" {
		t.Errorf("pilot check of a folder named for a value: %d\n%q%s", code, out, errOut)
	}
}

func replaceIn(t *testing.T, path, old, new string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), old) {
		t.Fatalf("%s does not hold %q", path, old)
	}
	if err := os.WriteFile(path, []byte(strings.Replace(string(data), old, new, 1)), 0o644); err != nil {
		t.Fatal(err)
	}
}

// Each way pilot cannot run exits 2 and says why on stderr.
func TestPilotUsageErrors(t *testing.T) {
	for name, args := range map[string][]string{
		"no pilot command":    {"pilot"},
		"an unknown command":  {"pilot", "score"},
		"an argument":         {"pilot", "check", "extra"},
		"an unknown flag":     {"pilot", "check", "--fast"},
		"a missing folder":    {"pilot", "check", "--tasks", filepath.Join(t.TempDir(), "none")},
		"a folder of nothing": {"pilot", "check", "--tasks", t.TempDir()},
	} {
		code, out, errOut := cmd(args...)
		if code != 2 || out != "" || errOut == "" {
			t.Errorf("%s: %d %q %q", name, code, out, errOut)
		}
	}
	_, _, errOut := cmd("pilot")
	if !strings.Contains(errOut, "usage:\n") || !strings.Contains(errOut, "\n  eva-eval pilot check [--tasks dir]") {
		t.Errorf("the pilot's usage: %q", errOut)
	}
	_, _, errOut = cmd()
	if !strings.Contains(errOut, "\n  eva-eval pilot ") {
		t.Errorf("eva-eval's usage lists no pilot: %q", errOut)
	}
}

// Another file adds a pilot command from its init: pilot reaches it, its
// usage lists it by name, and a name that is taken or malformed, or a
// command with no usage line or nothing to run, stops the program.
func TestAddPilot(t *testing.T) {
	saved := pilotCommands
	pilotCommands = map[string]pilotCommand{}
	t.Cleanup(func() { pilotCommands = saved })
	ran := func(_ context.Context, args []string, out, _ io.Writer) int {
		fmt.Fprintln(out, "ran", strings.Join(args, " "))
		return 7
	}
	addPilot("run", pilotCommand{synopsis: "run --out dir", run: ran})
	addPilot("a-report", pilotCommand{synopsis: "a-report rows", run: ran})
	if code, out, _ := cmd("pilot", "run", "--out", "x"); code != 7 || out != "ran --out x\n" {
		t.Errorf("run: %d %q", code, out)
	}
	_, _, errOut := cmd("pilot", "nothing")
	if want := "eva-eval: there is no pilot command nothing\nusage:\n  eva-eval pilot a-report rows\n  eva-eval pilot run --out dir\n"; errOut != want {
		t.Errorf("usage:\n%s\nwant:\n%s", errOut, want)
	}
	for name, c := range map[string]struct {
		name string
		cmd  pilotCommand
	}{
		"a name taken":         {"run", pilotCommand{synopsis: "run", run: ran}},
		"no name":              {"", pilotCommand{synopsis: "x", run: ran}},
		"a capital":            {"Run", pilotCommand{synopsis: "Run", run: ran}},
		"no usage line":        {"score", pilotCommand{run: ran}},
		"another name's usage": {"score", pilotCommand{synopsis: "other", run: ran}},
		"a usage of two lines": {"score", pilotCommand{synopsis: "score\n  more", run: ran}},
		"nothing to run":       {"score", pilotCommand{synopsis: "score"}},
	} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("%s: addPilot did not stop the program", name)
				}
			}()
			addPilot(c.name, c.cmd)
		}()
	}
	if len(pilotCommands) != 2 {
		t.Errorf("a refused command was kept: %d commands", len(pilotCommands))
	}
}
