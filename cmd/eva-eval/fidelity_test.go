package main

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/heyNag/eva-md/internal/eval/results"
	"github.com/heyNag/eva-md/internal/eval/scenario"
	"github.com/heyNag/eva-md/internal/eval/score"
	"github.com/heyNag/eva-md/internal/eval/v1page"
)

// fidelityRepo makes a repository with one commit, and returns it and its
// commit.
func fidelityRepo(t *testing.T) (string, string) {
	t.Helper()
	isolate(t)
	repo := filepath.Join(t.TempDir(), "repo")
	write(t, filepath.Join(repo, "internal", "eval", "score", "doc.go"), "package score\n")
	gitIn(t, filepath.Dir(repo), "init", "-q", repo)
	gitIn(t, repo, "add", "-A")
	gitIn(t, repo, "commit", "-q", "-m", "fixture")
	return repo, gitIn(t, repo, "rev-parse", "HEAD")
}

// committedPages are the pages in testdata/fidelity: v1's correct pages of
// five scripts, as threadpage.Render writes them.
var committedPages = []string{"v1.01", "v1.03", "v1.04", "v1.06", "v1.09"}

// emptyStore is a page that holds nothing: the least a page needs to be
// read, its Now with the markers that open and close it. v1page reads a
// page with no Now as an error, so an empty file would not do.
const emptyStore = "## Now\n<!-- eva:now:start -->\n<!-- eva:now:end -->\n"

// copyPages copies testdata/fidelity to a new folder and gives each of the
// seven scenarios with no committed page a page that holds nothing, so the
// folder holds all twelve; edit may change a page. It returns the folder.
func copyPages(t *testing.T, edit func(name, text string) string) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "pages")
	for _, sc := range scenario.V1() {
		name := sc.ID + ".md"
		text := emptyStore
		if slices.Contains(committedPages, sc.ID) {
			data, err := os.ReadFile(filepath.Join("testdata", "fidelity", name))
			if err != nil {
				t.Fatal(err)
			}
			text = string(data)
		}
		if edit != nil {
			text = edit(name, text)
		}
		write(t, filepath.Join(dir, name), text)
	}
	return dir
}

// A page that holds nothing reads as a store with no item, which is what
// the command scored a missing page as: the scorer gives it the result of
// an empty export.
func TestEmptyStorePage(t *testing.T) {
	ex, err := v1page.ReadThread(emptyStore)
	if err != nil || len(ex.Items) != 0 {
		t.Fatalf("%+v %v", ex, err)
	}
	for _, sc := range scenario.V1() {
		if got, want := score.Score(sc, ex), score.Score(sc, score.Export{}); !reflect.DeepEqual(got, want) {
			t.Errorf("%s: %+v, want %+v", sc.ID, got, want)
		}
	}
	if _, err := v1page.ReadThread(""); err == nil {
		t.Error("an empty file reads as a page")
	}
	entries, err := os.ReadDir(filepath.Join("testdata", "fidelity"))
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, strings.TrimSuffix(e.Name(), ".md"))
	}
	if !slices.Equal(names, committedPages) {
		t.Errorf("testdata/fidelity holds %v", names)
	}
}

// fidelityRows runs the command on pages and returns its rows and what it
// printed.
func fidelityRows(t *testing.T, repo, pages string, flags ...string) ([]results.Row, string) {
	t.Helper()
	out := filepath.Join(t.TempDir(), "out")
	code, stdout, stderr := cmd(append([]string{"fidelity", "--repo", repo, "--pages", pages, "--out", out}, flags...)...)
	if code != 0 || stderr != "" {
		t.Fatalf("exit %d: %s\n%s", code, stderr, stdout)
	}
	rows, err := results.Load(out)
	if err != nil {
		t.Fatal(err)
	}
	return rows, stdout
}

// value is the row of metric on scenario.
func value(t *testing.T, rows []results.Row, scenario, metric string) float64 {
	t.Helper()
	i := slices.IndexFunc(rows, func(r results.Row) bool { return r.Scenario == scenario && r.Metric == metric })
	if i < 0 {
		t.Fatalf("no row of %s on %s", metric, scenario)
	}
	return rows[i].Value
}

// required counts the facts of sc that must be stored and are still in
// force, read from the scenario: no end of their own, and no fact that
// supersedes them.
func required(sc scenario.Scenario) int {
	n := 0
	for _, f := range sc.Facts {
		superseded := slices.ContainsFunc(sc.Facts, func(g scenario.Fact) bool { return g.Supersedes == f.ID })
		if f.Must == scenario.MustStore && f.Until == nil && !superseded {
			n++
		}
	}
	return n
}

// The committed pages of v1's correct runs hold every fact of their
// scenarios; each of the 12 scenarios gets a row for each of the nine
// metrics, a page that holds nothing as an empty store, and the suite its
// totals. Every row is of the repository's commit and the flags' host and
// model, passes results.Load and the committed catalogue, and holds no
// planted word; nothing printed does either. Two runs give the same rows
// apart from the time.
func TestFidelityCommand(t *testing.T) {
	repo, commit := fidelityRepo(t)
	pages := copyPages(t, nil)
	cat, err := results.LoadCatalogue(filepath.Join(moduleRoot(t), "eval", "fidelity", "metrics.json"))
	if err != nil {
		t.Fatal(err)
	}
	flags := []string{"--host", "claude-code", "--host-version", "2.1.400", "--model", "made-up-model"}
	var runs [2][]results.Row
	var printed string
	for i := range runs {
		runs[i], printed = fidelityRows(t, repo, pages, flags...)
	}
	rows := runs[0]
	if len(rows) != 13*9 {
		t.Fatalf("%d rows", len(rows))
	}
	if err := cat.Check(rows); err != nil {
		t.Fatal(err)
	}
	var forbid []string
	for _, sc := range scenario.V1() {
		forbid = append(forbid, scenario.Forbid(sc)...)
	}
	if err := results.Forbidden(rows, forbid); err != nil {
		t.Fatal(err)
	}
	low := strings.ToLower(printed)
	for _, w := range forbid {
		if strings.Contains(low, strings.ToLower(w)) {
			t.Errorf("the output holds %q", w)
		}
	}
	for i, r := range rows {
		if r.Commit != commit || r.Dirty || r.Instrument != "fidelity" || r.Seed != 0 || r.Host != "claude-code" || r.HostVersion != "2.1.400" || r.Model != "made-up-model" {
			t.Errorf("row %d: %+v", i+1, r)
		}
		if m, _ := cat.Metric(r.Metric); r.Unit != m.Unit {
			t.Errorf("row %d: unit %s", i+1, r.Unit)
		}
	}
	for i := range runs[1] {
		runs[1][i].Time = runs[0][i].Time
	}
	if !slices.Equal(runs[0], runs[1]) {
		t.Error("two runs give different rows")
	}

	held, all := 0, 0
	lines := strings.Split(strings.TrimSuffix(printed, "\n"), "\n")
	if len(lines) != 16 {
		t.Fatalf("%d lines:\n%s", len(lines), printed)
	}
	for i, sc := range scenario.V1() {
		n := required(sc)
		all += n
		line := lines[i]
		if !strings.HasPrefix(line, sc.ID+": fact_recall ") {
			t.Errorf("line %d: %s", i+1, line)
		}
		if slices.Contains(committedPages, sc.ID) {
			held += n
			if v := value(t, rows, sc.ID, score.MetricFactHolds); v != 1 || n == 0 {
				t.Errorf("%s: fact_holds %v of %d facts", sc.ID, v, n)
			}
			if strings.Contains(line, "no_page") || strings.Contains(line, " missing") || strings.Contains(line, "; items ") {
				t.Errorf("%s: %s", sc.ID, line)
			}
		} else {
			if v := value(t, rows, sc.ID, score.MetricFactHolds); v != 0 || n == 0 {
				t.Errorf("%s with a page that holds nothing: fact_holds %v of %d facts", sc.ID, v, n)
			}
			if strings.Contains(line, "no_page") || !strings.Contains(line, " missing") {
				t.Errorf("%s: %s", sc.ID, line)
			}
		}
		if want := slices.Contains([]string{"v1.02", "v1.05", "v1.08", "v1.11"}, sc.ID); strings.HasSuffix(strings.TrimSuffix(line, " [2]"), " [1]") != want {
			t.Errorf("%s: the note on v1's port: %s", sc.ID, line)
		}
		if want := sc.ID == "v1.06"; strings.HasSuffix(line, " [2]") != want {
			t.Errorf("%s: the note on pasted text: %s", sc.ID, line)
		}
		for _, m := range []string{score.MetricWrongWrite, score.MetricNoise, score.MetricMisattributed, score.MetricStaleInForce,
			score.MetricDupes, score.MetricOTRStored, score.MetricViolations} {
			if v := value(t, rows, sc.ID, m); v != 0 {
				t.Errorf("%s: %s %v", sc.ID, m, v)
			}
		}
	}
	if got, want := value(t, rows, "v1", score.MetricFactHolds), float64(held)/float64(all); got != want || held == 0 || held == all {
		t.Errorf("the suite's fact_holds %v, want %d of %d", got, held, all)
	}
	if got, want := value(t, rows, "v1", score.MetricFactRecall), float64(held)/float64(all); got != want {
		t.Errorf("the suite's fact_recall %v, want %d of %d", got, held, all)
	}
	if !strings.HasPrefix(lines[12], "v1: fact_recall ") || !strings.HasPrefix(lines[13], "[1] ") || !strings.HasPrefix(lines[14], "[2] ") ||
		!strings.HasPrefix(lines[15], "Wrote 117 rows of commit "+commit[:12]+" to ") {
		t.Errorf("the closing lines:\n%s", strings.Join(lines[12:], "\n"))
	}
}

// A folder that lacks any of the twelve pages, as after a run that stopped
// part way, gets no row at all, since the suite's row from fewer pages
// would read like v1's: the command still prints each scenario's line, as
// a run on all twelve prints it for each page it finds and a missing page
// as an empty store marked no_page, then the notes, but no suite's line;
// it says on stderr how many pages were missing, naming none, and exits 1.
func TestFidelityMissingPages(t *testing.T) {
	repo, _ := fidelityRepo(t)
	_, printed := fidelityRows(t, repo, copyPages(t, nil))
	all := strings.Split(strings.TrimSuffix(printed, "\n"), "\n")
	var forbid []string
	for _, sc := range scenario.V1() {
		forbid = append(forbid, scenario.Forbid(sc)...)
	}
	without := func(names ...string) string {
		dir := copyPages(t, nil)
		for _, n := range names {
			if err := os.Remove(filepath.Join(dir, n+".md")); err != nil {
				t.Fatal(err)
			}
		}
		return dir
	}
	var uncommitted []string
	for _, sc := range scenario.V1() {
		if !slices.Contains(committedPages, sc.ID) {
			uncommitted = append(uncommitted, sc.ID)
		}
	}
	for _, tc := range []struct {
		name    string
		pages   string
		missing []string
	}{
		{"eleven pages, a committed one missing", without("v1.04"), []string{"v1.04"}},
		{"eleven pages, one that holds nothing missing", without("v1.12"), []string{"v1.12"}},
		{"the five committed pages alone", filepath.Join("testdata", "fidelity"), uncommitted},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out := filepath.Join(t.TempDir(), "out")
			code, stdout, stderr := cmd("fidelity", "--repo", repo, "--pages", tc.pages, "--out", out)
			if code != 1 {
				t.Fatalf("exit %d: %s\n%s", code, stderr, stdout)
			}
			if _, err := os.Stat(out); !os.IsNotExist(err) {
				t.Fatalf("rows were written: %v", err)
			}
			want := fmt.Sprintf("eva-eval: %d of v1's 12 pages are missing, so no row was written: ", len(tc.missing))
			if !strings.HasPrefix(stderr, want) || strings.Count(stderr, "\n") != 1 || strings.Contains(stderr, "v1.") {
				t.Errorf("stderr %q, want it to start %q and name no page", stderr, want)
			}
			low := strings.ToLower(stdout + stderr)
			for _, w := range forbid {
				if strings.Contains(low, strings.ToLower(w)) {
					t.Errorf("the output holds %q", w)
				}
			}
			lines := strings.Split(strings.TrimSuffix(stdout, "\n"), "\n")
			if len(lines) != 14 || lines[12] != all[13] || lines[13] != all[14] {
				t.Fatalf("%d lines:\n%s", len(lines), stdout)
			}
			for i, sc := range scenario.V1() {
				line := lines[i]
				switch {
				case !slices.Contains(tc.missing, sc.ID):
					if line != all[i] {
						t.Errorf("%s: %s\nwant the line of a run on all twelve pages\n%s", sc.ID, line, all[i])
					}
				case slices.Contains(committedPages, sc.ID):
					if !strings.HasPrefix(line, sc.ID+": fact_recall 0.00, fact_holds 0.00, ") || !strings.Contains(line, "; no_page") {
						t.Errorf("%s: %s", sc.ID, line)
					}
				default:
					// A page that held nothing, and none, score alike.
					head, marks, _ := strings.Cut(all[i], " [")
					if marks != "" {
						marks = " [" + marks
					}
					if line != head+"; no_page"+marks {
						t.Errorf("%s: %s\nwant\n%s", sc.ID, line, head+"; no_page"+marks)
					}
				}
			}
		})
	}
}

// A copy of the pages with one value changed, the correction's variable
// set back to the value it corrected, raises fidelity.wrong_write by one
// on that scenario and in the suite's totals, and changes no other
// scenario's rows.
func TestFidelityWrongValue(t *testing.T) {
	repo, _ := fidelityRepo(t)
	before, _ := fidelityRows(t, repo, copyPages(t, nil))
	const right, wrong = "- The token variable is EVA_MD_TOKEN, WREN-5 [stated]", "- The token variable is EVA_TOKEN, WREN-5 [stated]"
	after, printed := fidelityRows(t, repo, copyPages(t, func(name, text string) string {
		if name != "v1.04.md" {
			return text
		}
		if strings.Count(text, right) != 1 {
			t.Fatalf("v1.04.md has %d such lines", strings.Count(text, right))
		}
		return strings.Replace(text, right, wrong, 1)
	}))
	for _, s := range []string{"v1.04", "v1"} {
		if b, a := value(t, before, s, score.MetricWrongWrite), value(t, after, s, score.MetricWrongWrite); a != b+1 {
			t.Errorf("%s: wrong_write %v then %v", s, b, a)
		}
	}
	if v := value(t, after, "v1.04", score.MetricFactHolds); v != 0 {
		t.Errorf("v1.04: fact_holds %v", v)
	}
	if !strings.Contains(printed, "v1.04: fact_recall 0.00, fact_holds 0.00, wrong_write 1, ") || !strings.Contains(printed, " value_wrong") || !strings.Contains(printed, "; items value_wrong 1") {
		t.Errorf("output:\n%s", printed)
	}
	for i := range before {
		if s := before[i].Scenario; s == "v1.04" || s == "v1" {
			continue
		}
		after[i].Time = before[i].Time
		if before[i] != after[i] {
			t.Errorf("row %d changed: %+v then %+v", i+1, before[i], after[i])
		}
	}
}

// The facts the command counts in the suite's shares are those the
// scenario says must be stored and are still in force, on every scenario.
func TestMustHold(t *testing.T) {
	for _, sc := range scenario.V1() {
		if got, want := len(mustHold(sc)), required(sc); got != want || want == 0 {
			t.Errorf("%s: %d facts, want %d", sc.ID, got, want)
		}
	}
}

// Every way the command cannot run exits 2, says why on stderr, prints
// nothing on stdout and writes no row: a row would name a planted word of
// any scenario, as the model, or a page cannot be read.
func TestFidelityErrors(t *testing.T) {
	repo, _ := fidelityRepo(t)
	pages := copyPages(t, nil)
	unread := copyPages(t, func(name, text string) string {
		if name == "v1.09.md" {
			return "## Now\nsome text\n"
		}
		return text
	})
	empty := t.TempDir()
	write(t, filepath.Join(empty, "notes.md"), "nothing\n")
	file := filepath.Join(pages, "v1.01.md")
	for name, args := range map[string][]string{
		"an argument":                 {"--pages", pages, "now"},
		"no pages":                    {},
		"no out":                      {"--pages", pages, "--no-out"},
		"a host with no version":      {"--pages", pages, "--host", "codex"},
		"a version with no host":      {"--pages", pages, "--host-version", "0.167.0"},
		"pages that are a file":       {"--pages", file},
		"pages that are missing":      {"--pages", filepath.Join(empty, "none")},
		"pages with no page of v1":    {"--pages", empty},
		"a page that cannot be read":  {"--pages", unread},
		"a model that is not an id":   {"--pages", pages, "--model", "Made Up"},
		"a model of the first's word": {"--pages", pages, "--model", "flock-2"},
		"a model of the sixth's word": {"--pages", pages, "--model", "sqlite"},
		"a model of a later fact's":   {"--pages", pages, "--model", "kite-warm"},
		"a repository that is none":   {"--pages", pages, "--repo", empty},
	} {
		t.Run(name, func(t *testing.T) {
			out := filepath.Join(t.TempDir(), "out")
			args := append([]string{"fidelity", "--repo", repo, "--out", out}, args...)
			if name == "no out" {
				args = []string{"fidelity", "--repo", repo, "--pages", pages}
			}
			code, stdout, stderr := cmd(args...)
			if code != 2 || stdout != "" || !strings.HasPrefix(stderr, "eva-eval: ") {
				t.Fatalf("%d %q %q", code, stdout, stderr)
			}
			if _, err := os.Stat(out); !os.IsNotExist(err) {
				t.Fatalf("rows were written: %v", err)
			}
			if name == "a page that cannot be read" && !strings.Contains(stderr, "v1.09.md: v1page: line 2: ") {
				t.Errorf("%q", stderr)
			}
		})
	}
}

// A copy of the pages with faults of every kind the counts measure, on
// several scenarios, and a fact found under another's tag: the suite's
// row of each count is the sum of the scenarios' rows and is not 0, its
// fact_recall counts the found fact and its fact_holds does not, and the
// scenario's line names the fact's problem.
func TestFidelityTotals(t *testing.T) {
	repo, _ := fidelityRepo(t)
	edits := map[string][2]string{
		// A second item of the decision (dupes), the off-the-record budget
		// (otr_stored and violations) and the assistant's suggestion under
		// the user's tag (misattributed).
		"v1.01.md": {"- Use flock for the per-session lock, KESTREL-41 [stated] s:00001b1f#1\n",
			"- Use flock for the per-session lock, KESTREL-41 [stated] s:00001b1f#1\n- Use flock for the lock, KESTREL-41 [stated] s:00001b1f#1\n" +
				"- The budget is SHRIKE-99 [stated] s:00001b1f#4\n- Switch the lock to a pid file, PIDGEON-7 [stated] s:00001b1f#2\n"},
		// The reversed decision kept in force (stale_in_force).
		"v1.03.md": {"- Store pages as JSONL, OSPREY-J [stated] s:0000051f#1\n",
			"- Store pages as JSONL, OSPREY-J [stated] s:0000051f#1\n- Store pages as YAML, OSPREY-Y [stated] s:0000051e#1\n"},
		// The correction found, under the assistant's tag.
		"v1.04.md": {"EVA_MD_TOKEN, WREN-5 [stated] s:00000625#1", "EVA_MD_TOKEN, WREN-5 [inferred] s:00000625#1"},
		// The pasted decision under the user's tag (violations).
		"v1.06.md": {"- Keep SQLite for now, AVOCET-1 [stated] s:00001060#2\n",
			"- Keep SQLite for now, AVOCET-1 [stated] s:00001060#2\n- We will use Postgres, PELICAN-6 [stated] s:00001060#1\n"},
	}
	pages := copyPages(t, func(name, text string) string {
		e, ok := edits[name]
		if !ok {
			return text
		}
		if strings.Count(text, e[0]) != 1 {
			t.Fatalf("%s lacks %q", name, e[0])
		}
		return strings.Replace(text, e[0], e[1], 1)
	})
	// A page of v1.07 with a request that plants nothing (noise) and an
	// item citing a line its chat lacks (wrong_write).
	write(t, filepath.Join(pages, "v1.07.md"), "\n## Now\n<!-- eva:now:start -->\n### Next\n"+
		"- Keep an eye on the nightly jobs [stated] s:00001601#1\n- Rotate the signing keys [stated] s:00001601#9\n<!-- eva:now:end -->\n\n## Timeline\n")
	rows, printed := fidelityRows(t, repo, pages)
	for _, m := range []string{score.MetricWrongWrite, score.MetricNoise, score.MetricMisattributed, score.MetricStaleInForce,
		score.MetricDupes, score.MetricOTRStored, score.MetricViolations} {
		sum := 0.0
		for _, sc := range scenario.V1() {
			sum += value(t, rows, sc.ID, m)
		}
		if got := value(t, rows, "v1", m); got != sum || got == 0 {
			t.Errorf("the suite's %s is %v, the scenarios' sum %v", m, got, sum)
		}
	}
	if v := value(t, rows, "v1", score.MetricViolations); v < 2 {
		t.Errorf("the suite's violations %v, from two scenarios", v)
	}
	all, held, recalled := 0, 0, 0
	for _, sc := range scenario.V1() {
		n := required(sc)
		all += n
		held += int(value(t, rows, sc.ID, score.MetricFactHolds)*float64(n) + 0.5)
		recalled += int(value(t, rows, sc.ID, score.MetricFactRecall)*float64(n) + 0.5)
	}
	if got := value(t, rows, "v1", score.MetricFactHolds); got != float64(held)/float64(all) {
		t.Errorf("the suite's fact_holds %v, want %d of %d", got, held, all)
	}
	if got := value(t, rows, "v1", score.MetricFactRecall); got != float64(recalled)/float64(all) || recalled != held+1 {
		t.Errorf("the suite's fact_recall %v, want %d of %d, one more than hold", got, recalled, all)
	}
	if !strings.Contains(printed, "\nv1.04: fact_recall 1.00, fact_holds 0.00, ") || !strings.Contains(printed, " found/authority") ||
		!strings.Contains(printed, fmt.Sprintf("; facts that must hold: %d, holding: %d, found: 1\n", all, held)) {
		t.Errorf("output:\n%s", printed)
	}
}
