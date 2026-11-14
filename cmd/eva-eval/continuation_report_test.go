package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/heyNag/eva-md/internal/eval/continuation/drive"
	"github.com/heyNag/eva-md/internal/eval/continuation/drive/fakehost"
	"github.com/heyNag/eva-md/internal/eval/continuation/report"
	"github.com/heyNag/eva-md/internal/eval/results"
)

// reportCatalogue is the continuation's committed catalogue, from this
// package's folder.
var reportCatalogue = filepath.Join("..", "..", "eval", "continuation", "metrics.json")

// runConditions runs each condition on the rig into the results folder
// out, each with an empty --work of its own, and fails the test unless
// each judges every trial.
func runConditions(t *testing.T, r contRig, out string, more []string, conditions ...string) {
	t.Helper()
	for _, c := range conditions {
		args := append(r.args("--spend-model-quota", "--condition", c, "--out", out, "--work", filepath.Join(r.base, "work-"+filepath.Base(out)+"-"+c)), more...)
		if code, stdout, errOut := cmd(args...); code != 0 {
			t.Fatalf("%s: exit %d\n%s\n%s", c, code, stdout, errOut)
		}
	}
}

func reportOf(r contRig, more ...string) (int, string, string) {
	return cmd(append([]string{"continuation", "report", "--tasks", r.tasks, "--catalogue", reportCatalogue}, more...)...)
}

// assertNoPathOrWord fails when s holds a path of the rig's or a word the
// fixture task forbids.
func assertNoPathOrWord(t *testing.T, r contRig, s string) {
	t.Helper()
	for _, w := range []string{r.base, "/", fakehost.FixtureRepo, fakehost.FixtureColor, fakehost.FixtureLimit} {
		if strings.Contains(s, w) {
			t.Errorf("it holds %q:\n%s", w, s)
		}
	}
}

// Memory off and the ideal record run on the fake into one --out, each
// with an empty --work; the report reads their rows and records and
// exits 0 with their judged counts, the fixture task passing its screen.
func TestContinuationReport(t *testing.T) {
	r := newContRig(t)
	out := filepath.Join(r.base, "out")
	runConditions(t, r, out, nil, "off", "oracle")
	code, stdout, errOut := reportOf(r, "--rates", "3,0.3,15", out)
	if code != 0 || errOut != "" {
		t.Fatalf("exit %d\n%s\n%s", code, stdout, errOut)
	}
	for _, line := range []string{
		"host codex\n",
		"  runs, condition off: records 1, ended done 1, error 0, no_usage 0, memory_off 0, hidden_found 0, web_used 0, arm_stopped 0; planned 2, judged 2, not run 0, ",
		"  runs, condition oracle: records 1, ended done 1, error 0, no_usage 0, memory_off 0, hidden_found 0, web_used 0, arm_stopped 0; planned 2, judged 2, not run 0, ",
		"  task hidden-banner hidden, condition off: judged 2, solved 2, solved first try 0; corrections median 1, least 1, most 1; rounds median 2, least 2, most 2; agent events median 6, ",
		"  task hidden-banner hidden, condition oracle: judged 2, solved 2, solved first try 2; corrections median 0, least 0, most 0; rounds median 1, least 1, most 1; agent events median 3, ",
		"  cost, condition off: judged 2; medians ",
		"  variance, oracle against off: agent events pairs 2, mean -0.6931, s 0.0000, n 0; prompt tokens pairs 2, mean -0.6931, s 0.0000, n 0\n",
		"  screen, task hidden-banner hidden: pass\n",
		"  screens: passed 1, failed 0, too few 0, none 0\n",
	} {
		if !strings.Contains(stdout, line) {
			t.Errorf("the report lacks\n%s\nin\n%s", line, stdout)
		}
	}
	assertNoPathOrWord(t, r, stdout)
	// The rows' tokens: off's two calls a trial at 900 prompt (400 cached)
	// and 30 output, priced (500·3 + 400·0.3 + 30·15)·2 / 10⁶.
	if !strings.Contains(stdout, "price median 0.004140, at 3 uncached prompt, 0.3 cached and 15 output a million tokens") {
		t.Errorf("off's price:\n%s", stdout)
	}
}

// A run that judged none leaves a folder of records and no rows, which
// the report reads, its trials counted unjudged.
func TestContinuationReportReadsRecordsAlone(t *testing.T) {
	r := newContRig(t)
	r.setScript(t, fakehost.Script{SignIn: true})
	out := filepath.Join(r.base, "out")
	if code, stdout, errOut := cmd(r.args("--spend-model-quota")...); code != 1 {
		t.Fatalf("exit %d\n%s\n%s", code, stdout, errOut)
	}
	if _, err := os.Stat(filepath.Join(out, results.FileName)); !os.IsNotExist(err) {
		t.Fatal("the run wrote rows")
	}
	code, stdout, errOut := reportOf(r, out)
	if code != 0 || errOut != "" || !strings.Contains(stdout, "; planned 2, judged 0, not run 0, unjudged sign_in 2, ") ||
		!strings.Contains(stdout, "  task hidden-banner hidden, condition off: judged 0, ") {
		t.Fatalf("exit %d\n%s\n%s", code, stdout, errOut)
	}
}

// editLines rewrites each JSON line of the file with f, which may drop a
// line by returning nil or add some.
func editLines(t *testing.T, file string, f func(lines []map[string]any) []map[string]any) {
	t.Helper()
	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	var lines []map[string]any
	for _, l := range bytes.Split(bytes.TrimSuffix(data, []byte("\n")), []byte("\n")) {
		var m map[string]any
		if err := json.Unmarshal(l, &m); err != nil {
			t.Fatal(err)
		}
		lines = append(lines, m)
	}
	var buf bytes.Buffer
	for _, m := range f(lines) {
		l, err := json.Marshal(m)
		if err != nil {
			t.Fatal(err)
		}
		buf.Write(append(l, '\n'))
	}
	if err := os.WriteFile(file, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
}

// copyFolder copies the results folder from into a new folder and returns
// it.
func copyFolder(t *testing.T, from string) string {
	t.Helper()
	to := filepath.Join(t.TempDir(), "copy")
	if err := os.Mkdir(to, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{results.FileName, drive.RunsFile} {
		data, err := os.ReadFile(filepath.Join(from, name))
		if err != nil {
			t.Fatal(err)
		}
		write(t, filepath.Join(to, name), string(data))
	}
	return to
}

// rowOf returns the first line whose metric ends with suffix.
func rowOf(t *testing.T, lines []map[string]any, suffix string) int {
	t.Helper()
	for i, l := range lines {
		if strings.HasSuffix(l["metric"].(string), suffix) {
			return i
		}
	}
	t.Fatalf("no row of %s", suffix)
	return -1
}

// recordOf returns the line of the record of the condition c.
func recordOf(t *testing.T, lines []map[string]any, c string) map[string]any {
	t.Helper()
	for _, l := range lines {
		if l["condition"] == c {
			return l
		}
	}
	t.Fatalf("no record of %s", c)
	return nil
}

func sparseFile(t *testing.T, path string, size int64) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(size); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}

// Each refusal of the card's item 3, a file over 64 MiB, and each usage
// error, exits 2 with no figure, and its message holds no path and no
// word the task forbids. The refusals are made on copies of a real run's
// folder, which itself is read.
func TestContinuationReportRefuses(t *testing.T) {
	r := newContRig(t)
	full := filepath.Join(r.base, "out")
	runConditions(t, r, full, nil, "off", "oracle")
	smoke := filepath.Join(r.base, "smoke")
	runConditions(t, r, smoke, []string{"--trials", "1"}, "off")
	if code, stdout, errOut := reportOf(r, full); code != 0 {
		t.Fatalf("the unchanged folder: exit %d\n%s\n%s", code, stdout, errOut)
	}
	rowsEdit := func(f func(t *testing.T, rows []map[string]any) []map[string]any) func(t *testing.T, dir string) []string {
		return func(t *testing.T, dir string) []string {
			editLines(t, filepath.Join(dir, results.FileName), func(l []map[string]any) []map[string]any { return f(t, l) })
			return []string{dir}
		}
	}
	runsEdit := func(f func(t *testing.T, recs []map[string]any) []map[string]any) func(t *testing.T, dir string) []string {
		return func(t *testing.T, dir string) []string {
			editLines(t, filepath.Join(dir, drive.RunsFile), func(l []map[string]any) []map[string]any { return f(t, l) })
			return []string{dir}
		}
	}
	cases := []struct {
		name, want string
		make       func(t *testing.T, dir string) []string
	}{
		{"a row of another instrument", "row 1: its instrument is not continuation", rowsEdit(func(_ *testing.T, l []map[string]any) []map[string]any {
			l[0]["instrument"] = "fidelity"
			return l
		})},
		{"a row with no host", "row 1: it names no host", rowsEdit(func(_ *testing.T, l []map[string]any) []map[string]any {
			delete(l[0], "host")
			delete(l[0], "host_version")
			return l
		})},
		{"a metric of another family", "row 1: its metric's family is not task hidden-banner's", rowsEdit(func(_ *testing.T, l []map[string]any) []map[string]any {
			l[0]["metric"] = strings.Replace(l[0]["metric"].(string), ".hidden.", ".handoff.", 1)
			return l
		})},
		{"a scenario whose number is not its seed", "row 1: its scenario is not <task>.t<n> with n its seed", rowsEdit(func(_ *testing.T, l []map[string]any) []map[string]any {
			l[0]["seed"] = 2
			return l
		})},
		{"a row of a task not in --tasks", "row 1: task made-up-task is not in --tasks", rowsEdit(func(_ *testing.T, l []map[string]any) []map[string]any {
			l[0]["scenario"] = "made-up-task.t1"
			return l
		})},
		{"a record of a task not in --tasks", "record 1: task made-up-task is not in --tasks", runsEdit(func(_ *testing.T, l []map[string]any) []map[string]any {
			l[0]["tasks"] = []string{"hidden-banner", "made-up-task"}
			return l
		})},
		{"a row given twice in a folder", "metric continuation.hidden.off.solved, scenario hidden-banner.t1 is given twice", rowsEdit(func(_ *testing.T, l []map[string]any) []map[string]any {
			return append(l, l[0])
		})},
		{"one folder given twice", "folder 2, record 1: run ", func(_ *testing.T, dir string) []string {
			return []string{dir, dir}
		}},
		{"two copies of one run", "of host codex, condition off, is given twice", func(t *testing.T, dir string) []string {
			return []string{dir, copyFolder(t, dir)}
		}},
		{"a smoke run's folder beside the full one", "folder 2, row 1: host codex, metric continuation.hidden.off.solved, scenario hidden-banner.t1 is given twice", func(_ *testing.T, dir string) []string {
			return []string{dir, smoke}
		}},
		{"a trial lacking wall_ms", "host codex, condition off, trial hidden-banner.t1: it lacks wall_ms", func(t *testing.T, dir string) []string {
			editLines(t, filepath.Join(dir, results.FileName), func(l []map[string]any) []map[string]any {
				i := rowOf(t, l, ".off.wall_ms")
				return append(l[:i], l[i+1:]...)
			})
			editLines(t, filepath.Join(dir, drive.RunsFile), func(l []map[string]any) []map[string]any {
				rec := recordOf(t, l, "off")
				rec["rows"] = rec["rows"].(float64) - 1
				return l
			})
			return []string{dir}
		}},
		{"eva's trials lacking memory", "condition eva, trial hidden-banner.t1: it lacks memory_tokens", func(t *testing.T, dir string) []string {
			editLines(t, filepath.Join(dir, results.FileName), func(l []map[string]any) []map[string]any {
				for _, row := range l {
					row["metric"] = strings.Replace(row["metric"].(string), ".off.", ".eva.", 1)
				}
				return l
			})
			editLines(t, filepath.Join(dir, drive.RunsFile), func(l []map[string]any) []map[string]any {
				recordOf(t, l, "off")["condition"] = "eva"
				return l
			})
			return []string{dir}
		}},
		{"a folder with no record", "folder 1: it holds no run record", func(t *testing.T, dir string) []string {
			if err := os.Remove(filepath.Join(dir, drive.RunsFile)); err != nil {
				t.Fatal(err)
			}
			return []string{dir}
		}},
		{"rows the records do not count", "folder 1, host codex, condition off: 18 rows and 2 judged trials, where its records count 19 and 2", runsEdit(func(t *testing.T, l []map[string]any) []map[string]any {
			rec := recordOf(t, l, "off")
			rec["rows"] = rec["rows"].(float64) + 1
			return l
		})},
		{"judged trials the records do not count", "folder 1, host codex, condition oracle: 18 rows and 2 judged trials, where its records count 18 and 1", runsEdit(func(t *testing.T, l []map[string]any) []map[string]any {
			rec := recordOf(t, l, "oracle")
			rec["judged"] = rec["judged"].(float64) - 1
			return l
		})},
		{"a killed run's rows, which no record covers", "folder 1, host codex, condition oracle: 18 rows and 2 judged trials, where its records count 0 and 0", runsEdit(func(t *testing.T, l []map[string]any) []map[string]any {
			return []map[string]any{recordOf(t, l, "off")}
		})},
		{"a row naming a word the task forbids", "folder 1: a row holds a word a task forbids", rowsEdit(func(_ *testing.T, l []map[string]any) []map[string]any {
			l[3]["model"] = fakehost.FixtureColor + "-1"
			return l
		})},
		{"a rows file over 64 MiB", "folder 1: its rows file is over 64 MiB", func(t *testing.T, dir string) []string {
			sparseFile(t, filepath.Join(dir, results.FileName), report.MaxFile+1)
			return []string{dir}
		}},
		{"a runs file over 64 MiB", "folder 1: its runs file is over 64 MiB", func(t *testing.T, dir string) []string {
			sparseFile(t, filepath.Join(dir, drive.RunsFile), report.MaxFile+1)
			return []string{dir}
		}},
		{"a catalogue over 64 MiB", "the catalogue is over 64 MiB", func(t *testing.T, dir string) []string {
			big := filepath.Join(t.TempDir(), "metrics.json")
			sparseFile(t, big, report.MaxFile+1)
			return []string{"--catalogue", big, dir}
		}},
		{"rows the loader refuses", "folder 1: its rows cannot be read", func(t *testing.T, dir string) []string {
			write(t, filepath.Join(dir, results.FileName), "{\"v\":1,\"metric\":\"made-up\"")
			return []string{dir}
		}},
		{"a folder that is none", "folder 2: it is not a folder", func(t *testing.T, dir string) []string {
			return []string{dir, filepath.Join(dir, "none")}
		}},
		{"tasks that cannot be read", "the tasks cannot be read", func(t *testing.T, dir string) []string {
			return []string{"--tasks", filepath.Join(dir, "none"), dir}
		}},
		{"no folder", "usage:", func(*testing.T, string) []string { return nil }},
		{"rates of two numbers", "--rates is three numbers", func(_ *testing.T, dir string) []string { return []string{"--rates", "3,15", dir} }},
		{"a rate below 0", "--rates is three numbers at or above 0", func(_ *testing.T, dir string) []string { return []string{"--rates", "3,-1,15", dir} }},
		{"an unknown flag", "a flag of continuation report is unknown", func(_ *testing.T, dir string) []string { return []string{"--made-up", dir} }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			args := c.make(t, copyFolder(t, full))
			code, stdout, errOut := reportOf(r, args...)
			if code != 2 || stdout != "" || !strings.Contains(errOut, c.want) {
				t.Fatalf("exit %d\n%s\n%s", code, stdout, errOut)
			}
			// A refusal is one line in fixed words; a usage error lists the
			// continuation's commands, as every usage error does.
			if !strings.Contains(errOut, "usage:") {
				if strings.Count(errOut, "\n") != 1 {
					t.Errorf("not one line:\n%s", errOut)
				}
				assertNoPathOrWord(t, r, errOut)
			}
			for _, w := range []string{r.base, fakehost.FixtureRepo, fakehost.FixtureColor, fakehost.FixtureLimit} {
				if strings.Contains(errOut, w) {
					t.Errorf("it holds %q:\n%s", w, errOut)
				}
			}
		})
	}
	// --tasks is needed.
	if code, _, errOut := cmd("continuation", "report", full); code != 2 || !strings.Contains(errOut, "takes --tasks and one results folder or more") {
		t.Fatalf("no --tasks: exit %d\n%s", code, errOut)
	}
}

// The command is listed among the continuation's.
func TestContinuationReportIsListed(t *testing.T) {
	code, _, errOut := cmd("continuation")
	if code != 2 || !strings.Contains(errOut, "\n  eva-eval continuation report --tasks DIR [--catalogue FILE] [--rates IN,CACHED,OUT] OUT...\n") {
		t.Fatalf("exit %d\n%s", code, errOut)
	}
}
