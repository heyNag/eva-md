package drive

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/heyNag/eva-md/internal/eval/continuation/drive/fakehost"
	"github.com/heyNag/eva-md/internal/eval/continuation/task"
)

// zeros counts no trial under any code.
func zeros() map[string]int {
	m := map[string]int{}
	for _, c := range Codes {
		m[c] = 0
	}
	return m
}

// Two conditions run in one results folder give two records, in the order
// they ran, each exact but for its run's id, which is the run's own.
func TestTwoConditionsGiveTwoRecords(t *testing.T) {
	r := newRig(t, 1)
	r.opts.Trials, r.opts.Model = 2, "made-up-model-7"
	first, out, err := r.run()
	if err != nil {
		t.Fatalf("run: %v\n%s", err, out)
	}
	r.opts.Condition, r.opts.Work = task.Oracle, filepath.Join(r.base, "work-two")
	second, out, err := r.run()
	if err != nil {
		t.Fatalf("run: %v\n%s", err, out)
	}
	records, err := LoadRuns(r.opts.Out)
	if err != nil || len(records) != 2 {
		t.Fatalf("records %+v: %v", records, err)
	}
	if records[0].Run != first.Run || records[1].Run != second.Run || first.Run == "" || second.Run == "" {
		t.Fatalf("run ids %q %q, want %q %q", records[0].Run, records[1].Run, first.Run, second.Run)
	}
	want := func(c task.Condition, tokens int64) Record {
		return Record{V: 1, Instrument: "continuation", Host: "claude-code", HostVersions: []string{"1.2.3"}, Condition: string(c),
			Model: "made-up-model-7", Commit: r.commit(), Tasks: []string{fakehost.FixtureID}, Trials: 2, Planned: 2, Judged: 2,
			Unjudged: zeros(), Tokens: tokens, Rows: 18, Ended: EndedDone}
	}
	for i, w := range []Record{want(task.Off, 4200), want(task.Oracle, 2100)} {
		got := records[i]
		got.Run = ""
		if !reflect.DeepEqual(got, w) {
			t.Fatalf("record %d:\n%+v\nwant\n%+v", i+1, got, w)
		}
	}
	// The line holds every field, zeros and empty lists too.
	data, err := os.ReadFile(filepath.Join(r.opts.Out, RunsFile))
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal([]byte(strings.SplitN(string(data), "\n", 2)[0]), &fields); err != nil {
		t.Fatal(err)
	}
	if len(fields) != 20 || string(fields["unjudged"]) != `{"hidden_found":0,"host_failed":0,"memory_off":0,"no_usage":0,"other_model":0,"rate_limited":0,"setup_failed":0,"sign_in":0,"timeout":0,"update_failed":0}` {
		t.Fatalf("fields: %s", data)
	}
}

// A run writes its record however it ends once its folder is made: on a
// code that stops it, with the code, and on an error of its own, as error;
// the trials after the one it ended at are counted not run.
func TestARecordHoweverTheRunEnds(t *testing.T) {
	for name, c := range map[string]struct {
		set    func(r *rig)
		ended  string
		judged int
	}{
		"stopped": {func(r *rig) { r.script.NoUsage, r.script.Session = true, 2 }, CodeNoUsage, 1},
		"an error": {func(r *rig) {
			r.opts.Condition = task.Eva
			useArm(t, Arm{Prepare: func(_ context.Context, tr Trial) (Launch, error) {
				if tr.Number == 2 {
					return Launch{}, errors.New("a made-up failure")
				}
				return Launch{}, nil
			}})
		}, EndedError, 1},
	} {
		t.Run(name, func(t *testing.T) {
			r := newRig(t, 1)
			r.opts.Trials = 3
			c.set(r)
			s, out, err := r.run()
			if err == nil {
				t.Fatalf("run: %+v\n%s", s, out)
			}
			records, lerr := LoadRuns(r.opts.Out)
			if lerr != nil || len(records) != 1 {
				t.Fatalf("records %+v: %v", records, lerr)
			}
			got := records[0]
			unjudged := zeros()
			if c.ended == CodeNoUsage {
				unjudged[CodeNoUsage] = 1
			}
			if got.Ended != c.ended || got.Judged != c.judged || got.NotRun != 1 || got.Planned != 3 || !reflect.DeepEqual(got.Unjudged, unjudged) ||
				got.Run != s.Run || got.Tokens != s.Tokens || !reflect.DeepEqual(got.HostVersions, []string{"1.2.3"}) {
				t.Fatalf("record %+v\n%s", got, out)
			}
			if !strings.Contains(out, "run "+s.Run+": planned 3, judged 1, ") {
				t.Fatalf("no summary:\n%s", out)
			}
		})
	}
}

// A run that starts nothing writes no record: one refused, one without
// leave, and one whose condition's check fails.
func TestNoRecordWhenNothingStarts(t *testing.T) {
	for name, set := range map[string]func(r *rig){
		"refused":       func(r *rig) { r.opts.MaxSessions = 0 },
		"without leave": func(r *rig) { r.opts.Spend = false },
		"a failed check": func(r *rig) {
			r.opts.Condition = task.Eva
			useArm(t, Arm{Check: func(Options) error { return errors.New("made-up refusal") }})
		},
	} {
		t.Run(name, func(t *testing.T) {
			r := newRig(t, 1)
			set(r)
			if _, _, err := r.run(); err == nil {
				t.Fatal("the run started")
			}
			if _, err := os.Lstat(filepath.Join(r.opts.Out, RunsFile)); !errors.Is(err, fs.ErrNotExist) {
				t.Fatal("a record was written")
			}
			if _, err := LoadRuns(r.opts.Out); !errors.Is(err, fs.ErrNotExist) {
				t.Fatalf("LoadRuns: %v", err)
			}
		})
	}
}

// LoadRuns refuses the whole file for a field it does not know, a code not
// on the list, a way of ending that is none, a value that is no id, and a
// last line that is not whole; a file of good lines loads.
func TestLoadRunsRefuses(t *testing.T) {
	good := Record{V: 1, Run: "20261113t090001z", Instrument: "continuation", Host: "codex", HostVersions: []string{"1.2.3"}, Condition: "off",
		Commit: strings.Repeat("ab", 20), Tasks: []string{"made-up-task"}, Trials: 1, Planned: 1, Judged: 1, Unjudged: zeros(), Tokens: 9, Rows: 9, Ended: EndedDone}
	line := func(edit func(m map[string]any)) string {
		data, err := json.Marshal(good)
		if err != nil {
			t.Fatal(err)
		}
		var m map[string]any
		if err := json.Unmarshal(data, &m); err != nil {
			t.Fatal(err)
		}
		if edit != nil {
			edit(m)
		}
		data, err = json.Marshal(m)
		if err != nil {
			t.Fatal(err)
		}
		return string(data) + "\n"
	}
	dir := t.TempDir()
	load := func(body string) ([]Record, error) {
		write(t, filepath.Join(dir, RunsFile), body)
		return LoadRuns(dir)
	}
	if got, err := load(line(nil) + line(nil)); err != nil || len(got) != 2 || !reflect.DeepEqual(got[0], good) {
		t.Fatalf("good lines: %+v %v", got, err)
	}
	stopped := line(func(m map[string]any) {
		m["ended"], m["judged"] = CodeHiddenFound, 0
		m["unjudged"].(map[string]any)[CodeHiddenFound] = 1
	})
	if got, err := load(stopped); err != nil || len(got) != 1 || got[0].Ended != CodeHiddenFound {
		t.Fatalf("a stopped run's line: %+v %v", got, err)
	}
	// A run its condition stopped at its first trial: that trial not run.
	armStopped := func(edit func(m map[string]any)) string {
		return line(func(m map[string]any) {
			m["ended"], m["judged"], m["not_run"], m["rows"] = EndedArmStopped, 0, 1, 0
			if edit != nil {
				edit(m)
			}
		})
	}
	if got, err := load(armStopped(nil)); err != nil || len(got) != 1 || got[0].Ended != EndedArmStopped {
		t.Fatalf("a run its condition stopped: %+v %v", got, err)
	}
	for name, body := range map[string]string{
		"an unknown field":   line(func(m map[string]any) { m["made_up"] = 1 }),
		"an unknown code":    line(func(m map[string]any) { m["unjudged"].(map[string]any)["made_up"] = 0 }),
		"a code renamed":     line(func(m map[string]any) { u := m["unjudged"].(map[string]any); delete(u, "timeout"); u["made_up"] = 0 }),
		"a code missing":     line(func(m map[string]any) { delete(m["unjudged"].(map[string]any), "timeout") }),
		"an unknown ending":  line(func(m map[string]any) { m["ended"] = "made_up" }),
		"a code as ending":   line(func(m map[string]any) { m["ended"] = CodeSignIn }),
		"a model not an id":  line(func(m map[string]any) { m["model"] = "Made Up" }),
		"a task not an id":   line(func(m map[string]any) { m["tasks"] = []string{"a/b"} }),
		"versions unsorted":  line(func(m map[string]any) { m["host_versions"] = []string{"2.0", "1.0"} }),
		"another instrument": line(func(m map[string]any) { m["instrument"] = "pilot" }),
		"another format":     line(func(m map[string]any) { m["v"] = 2 }),
		"more counted":       line(func(m map[string]any) { m["not_run"] = 1 }),
		"a stop with no trial": line(func(m map[string]any) {
			m["ended"] = CodeHiddenFound
		}),
		"arm_stopped, one uncounted":    armStopped(func(m map[string]any) { m["planned"] = 2 }),
		"arm_stopped, none not run":     armStopped(func(m map[string]any) { m["judged"], m["not_run"] = 1, 0 }),
		"arm_stopped as a trial's code": line(func(m map[string]any) { m["unjudged"].(map[string]any)[EndedArmStopped] = 0 }),
		"dirty missing":                 line(func(m map[string]any) { delete(m, "dirty") }),
		"kept missing":                  line(func(m map[string]any) { delete(m, "kept") }),
		"judged missing":                line(func(m map[string]any) { delete(m, "judged") }),
		"not_run missing":               line(func(m map[string]any) { delete(m, "not_run") }),
		"no_model missing":              line(func(m map[string]any) { delete(m, "no_model") }),
		"tokens missing":                line(func(m map[string]any) { delete(m, "tokens") }),
		"rows missing":                  line(func(m map[string]any) { delete(m, "rows") }),
		"model missing":                 line(func(m map[string]any) { delete(m, "model") }),
		"a null count":                  line(func(m map[string]any) { m["judged"] = nil }),
		"a null flag":                   line(func(m map[string]any) { m["dirty"] = nil }),
		"a partial line":                line(nil) + strings.TrimSuffix(line(nil), "\n"),
		"not a record":                  line(nil) + "{\n",
	} {
		if got, err := load(body); err == nil {
			t.Errorf("%s: loaded %+v", name, got)
		}
	}
}

// A record that holds a word the run forbids, or is not whole, is not
// appended, and neither is one after a partial line.
func TestAppendRecordRefuses(t *testing.T) {
	dir := t.TempDir()
	good := Record{V: 1, Run: "20261113t090001z", Instrument: "continuation", Host: "codex", HostVersions: []string{}, Condition: "off",
		Model: "made-up-quarry-model", Commit: strings.Repeat("ab", 20), Tasks: []string{}, Trials: 1, Planned: 1, Unjudged: zeros(), Ended: EndedDone}
	if err := appendRecord(dir, good, []string{"Quarry"}); err == nil {
		t.Fatal("a forbidden word was appended")
	}
	bad := good
	bad.Unjudged = map[string]int{}
	if err := appendRecord(dir, bad, nil); err == nil {
		t.Fatal("a record without its counts was appended")
	}
	if _, err := os.Lstat(filepath.Join(dir, RunsFile)); !errors.Is(err, fs.ErrNotExist) {
		t.Fatal("a refused record made the file")
	}
	write(t, filepath.Join(dir, RunsFile), `{"v":1`)
	if err := appendRecord(dir, good, nil); err == nil {
		t.Fatal("a record was appended after a partial line")
	}
	if err := appendRecord(t.TempDir(), good, nil); err != nil {
		t.Fatalf("a good record: %v", err)
	}
}
