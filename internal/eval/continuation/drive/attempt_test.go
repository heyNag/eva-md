package drive

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/heyNag/eva-md/internal/eval/continuation/task"
	"github.com/heyNag/eva-md/internal/eval/results"
)

// refusedBeforeThePlan runs the rig and fails unless the run was refused
// before its plan, with reason in its error, and started nothing: the fake
// records no call, and the results folder holds what it held.
func refusedBeforeThePlan(t *testing.T, r *rig, reason string) {
	t.Helper()
	before := snapshotOf(t, r.opts.Out)
	calls := len(r.calls())
	_, out, err := r.run()
	var usage *UsageError
	if !errors.As(err, &usage) || out != "" || !strings.Contains(err.Error(), reason) {
		t.Fatalf("run: %v\n%s", err, out)
	}
	if got := len(r.calls()); got != calls {
		t.Fatalf("the fake ran: %d calls, %d before", got, calls)
	}
	if after := snapshotOf(t, r.opts.Out); after != before {
		t.Fatalf("the results folder changed:\n%s\nwas\n%s", after, before)
	}
}

// snapshotOf is what the results folder's two files hold.
func snapshotOf(t *testing.T, dir string) string {
	t.Helper()
	var b strings.Builder
	for _, name := range []string{results.FileName, RunsFile} {
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil && !os.IsNotExist(err) {
			t.Fatal(err)
		}
		b.WriteString(name + ":\n" + string(data))
	}
	return b.String()
}

const again = "--out holds an earlier attempt"

// A second run of a host, a condition and a task in one results folder
// starts nothing: refused by the first's rows, by its record when it
// judged none, and by its rows when it wrote no record. Another
// condition, another host, and another task, run.
func TestASecondAttemptIsRefused(t *testing.T) {
	t.Run("judged", func(t *testing.T) {
		r := newRig(t, 1)
		if _, out, err := r.run(); err != nil {
			t.Fatalf("run: %v\n%s", err, out)
		}
		r.opts.Work = filepath.Join(r.base, "work-two")
		refusedBeforeThePlan(t, r, again)
		// By the rows alone, as when the first was killed before its record.
		if err := os.Remove(filepath.Join(r.opts.Out, RunsFile)); err != nil {
			t.Fatal(err)
		}
		refusedBeforeThePlan(t, r, again)
	})
	t.Run("none judged", func(t *testing.T) {
		r := newRig(t, 1)
		r.script.SignIn = true
		if s, out, err := r.run(); err != nil || s.Judged != 0 {
			t.Fatalf("run: %+v %v\n%s", s, err, out)
		}
		if _, err := os.Lstat(filepath.Join(r.opts.Out, results.FileName)); !os.IsNotExist(err) {
			t.Fatal("rows were written")
		}
		r.script.SignIn = false
		r.opts.Work = filepath.Join(r.base, "work-two")
		refusedBeforeThePlan(t, r, again)
	})
	t.Run("others run", func(t *testing.T) {
		r := newRig(t, 1)
		if _, out, err := r.run(); err != nil {
			t.Fatalf("run: %v\n%s", err, out)
		}
		for i, set := range []func(){
			func() { r.opts.Condition = task.Oracle },
			func() { r.opts.Condition, r.opts.Host = task.Off, "codex" },
			func() { r.opts.Host = "claude-code"; r.opts.Tasks = otherTask(t, r) },
		} {
			set()
			r.opts.Work = filepath.Join(r.base, "work-"+string(rune('a'+i)))
			if s, out, err := r.run(); err != nil || s.Judged != 1 {
				t.Fatalf("run %d: %+v %v\n%s", i+2, s, err, out)
			}
		}
		records, err := LoadRuns(r.opts.Out)
		if err != nil || len(records) != 4 {
			t.Fatalf("records %+v: %v", records, err)
		}
	})
}

// otherTask writes a tasks folder holding the fixture's task alone, under
// another id, and returns it.
func otherTask(t *testing.T, r *rig) string {
	t.Helper()
	tk, err := task.Load(r.script.Task)
	if err != nil {
		t.Fatal(err)
	}
	tasks := filepath.Join(r.base, "other-tasks")
	dir := filepath.Join(tasks, "hidden-banner-two")
	if err := os.CopyFS(dir, os.DirFS(r.script.Task)); err != nil {
		t.Fatal(err)
	}
	tk.ID = "hidden-banner-two"
	data, err := json.MarshalIndent(tk, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(dir, task.FileName), string(data)+"\n")
	return tasks
}

// A rows or runs file that cannot be read, and a results folder that is
// a file, refuse the run before its plan.
func TestAnUnreadableResultsFolderIsRefused(t *testing.T) {
	for name, c := range map[string]struct {
		file, body, reason string
	}{
		"rows":            {results.FileName, "not a row\n", "--out's rows cannot be read"},
		"a partial row":   {results.FileName, `{"v":1`, "--out's rows cannot be read"},
		"runs":            {RunsFile, "not a record\n", "--out's runs cannot be read"},
		"a partial run":   {RunsFile, `{"v":1`, "--out's runs cannot be read"},
		"an unknown code": {RunsFile, `{"v":1,"unjudged":{"made_up":1}}` + "\n", "--out's runs cannot be read"},
	} {
		t.Run(name, func(t *testing.T) {
			r := newRig(t, 1)
			write(t, filepath.Join(r.opts.Out, c.file), c.body)
			refusedBeforeThePlan(t, r, c.reason)
		})
	}
	r := newRig(t, 1)
	write(t, r.opts.Out, "a file\n")
	_, out, err := r.run()
	if err == nil || out != "" || !strings.Contains(err.Error(), "--out is not a folder") || len(r.calls()) != 0 {
		t.Fatalf("run: %v\n%s", err, out)
	}
}

// A results folder inside --work, or one that holds it, where a chat could
// reach it, is refused before the plan.
func TestAResultsFolderInTheWorkIsRefused(t *testing.T) {
	r := newRig(t, 1)
	r.opts.Out = filepath.Join(r.opts.Work, "out")
	refusedBeforeThePlan(t, r, "--out overlaps --work")
	r.opts.Out = filepath.Join(r.base, "outer")
	r.opts.Work = filepath.Join(r.opts.Out, "work")
	refusedBeforeThePlan(t, r, "--out overlaps --work")
	// Through a link.
	r = newRig(t, 1)
	mkdir(t, r.opts.Work)
	link := filepath.Join(r.base, "to-work")
	if err := os.Symlink(r.opts.Work, link); err != nil {
		t.Fatal(err)
	}
	r.opts.Out = filepath.Join(link, "out")
	refusedBeforeThePlan(t, r, "--out overlaps --work")
}
