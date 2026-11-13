package task

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// check runs Check under a hang guard.
func check(t *testing.T, task Task) error {
	t.Helper()
	done := make(chan error, 1)
	go func() { done <- Check(context.Background(), task, filepath.Join(t.TempDir(), "scratch"), time.Hour) }()
	select {
	case err := <-done:
		return err
	case <-time.After(hangGuard):
		t.Fatalf("Check did not return within %v", hangGuard)
		return nil
	}
}

// replace rewrites a file of a task folder through edit, failing the
// test when edit changes nothing.
func replace(t *testing.T, path string, edit func(string) string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	out := edit(string(data))
	if out == string(data) {
		t.Fatalf("the edit of %s changed nothing", filepath.Base(path))
	}
	writeFile(t, path, out)
}

// Each rule that runs tests, broken in a copy of a committed task that
// keeps every other rule: Check refuses it naming that rule, in a message
// that holds no value; the copy unbroken passes.
func TestCheckRefusesEachRule(t *testing.T) {
	// The state header, written so that no file holds its letters.
	const chrHeader = `"".join(chr(c) for c in (35, 98, 114, 105, 115, 111, 108, 100, 101, 45, 115, 116, 97, 116, 101, 32, 50))`
	cases := []struct {
		name, id, rule, why string
		edit                func(t *testing.T, dir string)
	}{
		{"unbroken handoff", "h04", "", "", func(*testing.T, string) {}},
		{"unbroken hidden decision", "d04", "", "", func(*testing.T, string) {}},
		{"a repository's own test that fails", "h04", ruleOwnTests, "the own tests of repository 1 of 1 fail as it stands",
			func(t *testing.T, dir string) {
				replace(t, filepath.Join(dir, "repos", "pebblewick", "test_todo.py"), func(s string) string {
					return strings.Replace(s, `self.assertEqual(run([], "dance")[0], 2)`, `self.assertEqual(run([], "dance")[0], 0)`, 1)
				})
			}},
		{"the second repository's own test that fails", "h05", ruleOwnTests, "the own tests of repository 2 of 2 fail as it stands",
			func(t *testing.T, dir string) {
				replace(t, filepath.Join(dir, "repos", "tallyport", "test_tally.py"), func(s string) string {
					return strings.Replace(s, `"north": 1375`, `"north": 1376`, 1)
				})
			}},
		{"a hidden test that passes on the start", "h04", ruleStart, "hidden test t1 passes on the repositories as they stand",
			func(t *testing.T, dir string) {
				data, err := os.ReadFile(filepath.Join(dir, "solution", "pebblewick", "todo.py"))
				if err != nil {
					t.Fatal(err)
				}
				writeFile(t, filepath.Join(dir, "repos", "pebblewick", "todo.py"), string(data))
			}},
		{"one hidden test of two that passes on the start", "d05", ruleStart, "hidden test t2 passes on the repositories as they stand",
			func(t *testing.T, dir string) {
				writeFile(t, filepath.Join(dir, "hidden", "drovershaw", "test_hidden.py"), "import unittest\n\n\nclass HiddenTests(unittest.TestCase):\n"+
					"    def test_counts(self):\n        self.fail()\n\n    def test_rejects_file(self):\n        pass\n")
			}},
		{"a hidden test that fails with the solution", "h04", ruleSolution, "hidden test t2 fails with the solution",
			func(t *testing.T, dir string) {
				replace(t, filepath.Join(dir, "solution", "pebblewick", "todo.py"), func(s string) string {
					return strings.Replace(s, `items[:] = kept`, `pass`, 1)
				})
			}},
		{"a decoy that fails a test encoding no fact", "d04", ruleDecoy, "hidden test t1 fails with the decoy, though it encodes no fact",
			func(t *testing.T, dir string) {
				replace(t, filepath.Join(dir, "decoy", "veltmoor", "counters.py"), func(s string) string {
					return strings.Replace(s, "c.counts = json.load(f)", "c.counts = {}", 1)
				})
			}},
		{"a decoy that passes a test encoding a fact", "d04", ruleDecoy, "hidden test t2 passes with the decoy, though it encodes a fact",
			func(t *testing.T, dir string) {
				data, err := os.ReadFile(filepath.Join(dir, "solution", "veltmoor", "counters.py"))
				if err != nil {
					t.Fatal(err)
				}
				s := strings.Replace(string(data), `HEADER = "#brisolde-state 2"`, "HEADER = "+chrHeader, 1)
				writeFile(t, filepath.Join(dir, "decoy", "veltmoor", "counters.py"), s)
			}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			tasks := copyTasks(t, c.id)
			c.edit(t, filepath.Join(tasks, c.id))
			task := loaded(t, tasks)
			err := check(t, task)
			if c.rule == "" {
				if err != nil {
					t.Fatalf("Check: %v", err)
				}
				return
			}
			var e *Error
			if !errors.As(err, &e) || e.Rule != c.rule || e.Task != c.id || e.Family != task.Family || e.Why != c.why {
				t.Fatalf("Check gave %v, want %s: %s", err, c.rule, c.why)
			}
			if holds(e.Error(), Forbid(task)) {
				t.Errorf("the message %q holds a value or a repository's name", e.Error())
			}
		})
	}
}

// Check runs the Go tasks too: one passes, and its decoy, laid as the
// solution, fails with the hidden test that encodes the fact named.
func TestCheckAGoTask(t *testing.T) {
	tasks := copyTasks(t, "d01")
	if err := check(t, loaded(t, tasks)); err != nil {
		t.Fatalf("d01: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(tasks, "d01", "decoy", "harrowgate", "sched.go"))
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(tasks, "d01", "solution", "harrowgate", "sched.go"), string(data))
	var e *Error
	if err := check(t, loaded(t, tasks)); !errors.As(err, &e) || e.Rule != ruleSolution || e.Why != "hidden test t2 fails with the solution" {
		t.Errorf("the decoy as the solution: %v", err)
	}
}

// Check needs an empty scratch folder of its own.
func TestCheckNeedsAnEmptyScratch(t *testing.T) {
	task := loaded(t, copyTasks(t, "h04"))
	scratch := t.TempDir()
	writeFile(t, filepath.Join(scratch, "left.txt"), "x\n")
	err := Check(context.Background(), task, scratch, time.Hour)
	var e *Error
	if err == nil || errors.As(err, &e) {
		t.Errorf("a scratch folder in use: %v", err)
	}
}
