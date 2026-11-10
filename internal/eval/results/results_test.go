package results

import (
	"bytes"
	"context"
	"fmt"
	"math"
	"math/rand/v2"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/heyNag/eva-md/internal/testgitenv"
)

func TestMain(m *testing.M) {
	testgitenv.Clear()
	os.Exit(m.Run())
}

// isolate gives the test a temporary home whose git config is a temporary
// file with a made-up identity, and clears the variables that would point
// git elsewhere.
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
	if err := os.WriteFile(filepath.Join(home, "gitconfig"), []byte("[user]\nname = Eva Fixture\nemail = fixture@example.invalid\n[commit]\ngpgsign = false\n[init]\ndefaultBranch = main\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return home
}

// gitIn runs git in dir and returns its output without the final newline.
func gitIn(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v %s", args, err, out)
	}
	return strings.TrimSuffix(string(out), "\n")
}

// commitFile writes content to the repository's file rel and commits it,
// and returns the commit.
func commitFile(t *testing.T, repo, rel, content string) string {
	t.Helper()
	path := filepath.Join(repo, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	gitIn(t, repo, "add", "--", rel)
	gitIn(t, repo, "commit", "--quiet", "-m", "change "+rel)
	return gitIn(t, repo, "rev-parse", "HEAD")
}

// newRepo makes a repository with a file in each path the fixture
// catalogue covers and one outside them, and returns it and its last
// commit.
func newRepo(t *testing.T) (string, string) {
	t.Helper()
	repo := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	gitIn(t, repo, "init", "--quiet")
	commitFile(t, repo, "internal/hook/hook.go", "package hook\n")
	commitFile(t, repo, "scripts/benchmark.py", "print(0)\n")
	commitFile(t, repo, "internal/update/update.go", "package update\n")
	commitFile(t, repo, "internal/searchindex/index.go", "package searchindex\n")
	return repo, commitFile(t, repo, "docs/notes.md", "notes\n")
}

const commitA = "0123456789abcdef0123456789abcdef01234567"

var when = time.Date(2026, 11, 10, 9, 0, 0, 0, time.UTC)

// row is a made-up row of the latency metric.
func row(scenario string, value float64) Row {
	return Row{V: Version, Metric: "hook.capture_p95_ms", Value: value, Unit: "ms", Commit: commitA,
		Instrument: "latency", Scenario: scenario, Seed: 7, Time: when}
}

// catalogue is the catalogue the tests use.
func catalogue(t *testing.T) Catalogue {
	t.Helper()
	c, err := LoadCatalogue(filepath.Join("testdata", "metrics.json"))
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// SplitMix64 draws what the published reference does for seed 0, so a
// seed gives the same intervals on every machine and Go release.
func TestTheGeneratorIsSplitMix64(t *testing.T) {
	g := splitmix{0}
	want := []uint64{0xe220a8397b1dcdaf, 0x6e789e6aa1b965f4, 0x06c45d188009454f, 0xf88bb8a8724c81ec}
	for i, w := range want {
		if got := g.next(); got != w {
			t.Fatalf("draw %d: %#x, want %#x", i+1, got, w)
		}
	}
	// A draw below n rejects the values that would favour the low numbers:
	// for n just above 2^63, nearly half of them, so the draws are not
	// SplitMix64's own modulo n.
	g = splitmix{42}
	for i, w := range []uint64{0x3dd732262feb6e94, 0x5e4431fa3c80db05, 0x4cf635ee9e9e2fa3, 0x1e54d738297f77ad} {
		if got := g.below(1<<63 + 1); got != w {
			t.Fatalf("draw %d below 2^63+1: %#x, want %#x", i+1, got, w)
		}
	}
	g = splitmix{42}
	for _, n := range []uint64{1, 2, 3, 7, 1 << 63, 1<<63 + 1, math.MaxUint64} {
		for range 100 {
			if x := g.below(n); x >= n {
				t.Fatalf("below(%d) gave %d", n, x)
			}
		}
	}
}

// The fixture's rows give exact figures: the plain ones from the values
// alone, the interval from the fixed seed, and none of them from the
// order of the rows.
func TestFixtureRowsGiveExactStatistics(t *testing.T) {
	rows, err := Load(filepath.Join("testdata", "rows"))
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 20 {
		t.Fatalf("the fixture holds %d rows", len(rows))
	}
	want := Summary{N: 20, Mean: 10.5, Median: 10.5, P95: 19, Min: 1, Max: 20, CILow: 9.3, CIHigh: 11.7, Scenarios: 5}
	got := Summarize(rows)
	if got != want {
		t.Fatalf("got %+v\nwant %+v", got, want)
	}
	shuffled := slices.Clone(rows)
	rand.New(rand.NewPCG(1, 2)).Shuffle(len(shuffled), func(i, j int) { shuffled[i], shuffled[j] = shuffled[j], shuffled[i] })
	if again := Summarize(shuffled); again != want {
		t.Fatalf("the shuffled rows give %+v", again)
	}
	// The interval comes from the seed. At 10,000 draws another seed lands
	// on the same twentieths, so the seed shows in a few draws: the same
	// seed gives the same interval, another seed another one.
	few := summarize(rows, Seed, 9)
	if again := summarize(rows, Seed, 9); again != few {
		t.Fatalf("the same seed gives %+v, then %+v", few, again)
	}
	if few.CILow != 9.7 || few.CIHigh != 11.9 {
		t.Fatalf("9 draws with the seed give %v to %v, want 9.7 to 11.9", few.CILow, few.CIHigh)
	}
	if other := summarize(rows, Seed+1, 9); other.CILow == few.CILow && other.CIHigh == few.CIHigh {
		t.Fatalf("another seed gives the same interval %v to %v", other.CILow, other.CIHigh)
	}
}

// irregular is 24 made-up rows of 12 scenarios of one to three trials,
// whose values share no lattice, so nearly every resample has a mean of its
// own and the interval's ends move with the seed, the draws and the
// quantiles taken.
func irregular() []Row {
	var rows []Row
	for i := range 12 {
		for j := range 1 + i%3 {
			rows = append(rows, row(fmt.Sprintf("s%02d", i), float64(i*i)*0.37+float64(j)*1.13+0.011*float64(i)))
		}
	}
	return rows
}

// At the full 10,000 draws, rows off a lattice give an interval only the
// fixed seed, SplitMix64's draws and the 2.5% and 97.5% quantiles give.
func TestTheSeedFixesTheInterval(t *testing.T) {
	rows := irregular()
	want := Summary{N: 24, Mean: 17.775833333333335, Median: 12.4755, P95: 46.021, Min: 0, Max: 47.150999999999996,
		CILow: 8.624519999999997, CIHigh: 27.498538461538455, Scenarios: 12}
	if got := Summarize(rows); got != want {
		t.Fatalf("got %+v\nwant %+v", got, want)
	}
	if Seed != 0x65766120657661 || Draws != 10000 {
		t.Fatalf("the seed is %#x and the draws %d", Seed, Draws)
	}
	if other := summarize(rows, 0x12345, Draws); other.CILow == want.CILow || other.CIHigh == want.CIHigh {
		t.Fatalf("another seed gives %v to %v", other.CILow, other.CIHigh)
	}
}

func TestSummarizeByHand(t *testing.T) {
	values := func(vs ...float64) []Row {
		var rows []Row
		for i, v := range vs {
			rows = append(rows, row("s"+string(rune('a'+i)), v))
		}
		return rows
	}
	// The nearest rank: of 21 values, the 20th; of one value, that one.
	var vs []float64
	for i := 1; i <= 21; i++ {
		vs = append(vs, float64(i))
	}
	if s := Summarize(values(vs...)); s.P95 != 20 || s.Median != 11 || s.Mean != 11 {
		t.Errorf("1 to 21: %+v", s)
	}
	if s := Summarize(values(3.5)); s != (Summary{N: 1, Mean: 3.5, Median: 3.5, P95: 3.5, Min: 3.5, Max: 3.5, CILow: 3.5, CIHigh: 3.5, Scenarios: 1}) {
		t.Errorf("one value: %+v", s)
	}
	// Two scenarios, 0 and 1: a resample's mean is 0, 0.5 or 1, and a
	// quarter of the draws are each end, so the interval runs from 0 to 1.
	if s := Summarize(values(0, 1)); s.CILow != 0 || s.CIHigh != 1 || s.Mean != 0.5 {
		t.Errorf("0 and 1: %+v", s)
	}
	// One scenario's trials are resampled together, never apart: however
	// they spread, a single scenario's interval is its mean.
	one := []Row{row("only", 1), row("only", 5), row("only", 9)}
	if s := Summarize(one); s.CILow != 5 || s.CIHigh != 5 || s.Scenarios != 1 {
		t.Errorf("one scenario: %+v", s)
	}
	if s := Summarize(nil); s != (Summary{}) {
		t.Errorf("no rows: %+v", s)
	}
}

func TestAppendThenLoad(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "run")
	s := Store{Dir: dir}
	first := row("a", 1)
	first.Time = time.Date(2026, 11, 10, 10, 0, 0, 0, time.FixedZone("x", 3600))
	first.V = 0 // the store sets the format
	if err := s.Append(first, row("b", 2)); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(filepath.Join(dir, FileName))
	if err != nil {
		t.Fatal(err)
	}
	withHost := row("c", 3)
	withHost.Host, withHost.HostVersion, withHost.Model = "codex", "0.167.0", "gpt-5.1-codex"
	if err := s.Append(withHost); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(filepath.Join(dir, FileName))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(after, before) || bytes.Count(after, []byte("\n")) != 3 {
		t.Fatalf("the second append did not only append:\n%s", after)
	}
	rows, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	first.V, first.Time = Version, first.Time.UTC()
	want := []Row{first, row("b", 2), withHost}
	if len(rows) != 3 {
		t.Fatalf("got %d rows", len(rows))
	}
	for i := range want {
		if !rows[i].Time.Equal(want[i].Time) || rows[i].Time.Location() != time.UTC {
			t.Errorf("row %d: time %v", i+1, rows[i].Time)
		}
		rows[i].Time, want[i].Time = time.Time{}, time.Time{}
	}
	if !reflect.DeepEqual(rows, want) {
		t.Fatalf("got %+v\nwant %+v", rows, want)
	}
	// The file itself loads the same, and an empty file is no rows.
	if again, err := Load(filepath.Join(dir, FileName)); err != nil || len(again) != 3 {
		t.Fatalf("by file: %d rows, %v", len(again), err)
	}
	empty := filepath.Join(t.TempDir(), "empty.jsonl")
	os.WriteFile(empty, nil, 0o644)
	if rows, err := Load(empty); err != nil || rows != nil {
		t.Fatalf("empty: %v, %v", rows, err)
	}
}

// A row holds ids, numbers and a time: text, a path or a malformed figure
// is refused, nothing is written, and the error never repeats what the
// field held.
func TestAppendRefusesWhatIsNotAnId(t *testing.T) {
	const secret = "Q7VALUE"
	cases := map[string]func(*Row){
		"a sentence":           func(r *Row) { r.Scenario = "the user said " + secret },
		"capitals":             func(r *Row) { r.Metric = "Hook." + secret },
		"a path":               func(r *Row) { r.Scenario = "/home/someone/" + secret },
		"a relative path":      func(r *Row) { r.Instrument = "eval/" + secret },
		"a home path":          func(r *Row) { r.Model = "~" + secret },
		"a version with path":  func(r *Row) { r.Host, r.HostVersion = "codex", "1.0/"+secret },
		"a version with space": func(r *Row) { r.Host, r.HostVersion = "codex", "1.0 "+secret },
		"too long":             func(r *Row) { r.Scenario = strings.Repeat("a", 65) },
		"a short commit":       func(r *Row) { r.Commit = "0123456" },
		"an upper commit":      func(r *Row) { r.Commit = strings.ToUpper(commitA) },
		"no unit":              func(r *Row) { r.Unit = "" },
		"no scenario":          func(r *Row) { r.Scenario = "" },
		"a host alone":         func(r *Row) { r.Host = "codex" },
		"a version alone":      func(r *Row) { r.HostVersion = "1.0" },
		"not a number":         func(r *Row) { r.Value = math.NaN() },
		"infinite":             func(r *Row) { r.Value = math.Inf(1) },
		"no time":              func(r *Row) { r.Time = time.Time{} },
		"a newline":            func(r *Row) { r.Scenario = "a\n" + secret },
	}
	for name, change := range cases {
		dir := t.TempDir()
		bad := row("a", 1)
		change(&bad)
		err := Store{Dir: dir}.Append(row("ok", 1), bad)
		if err == nil {
			t.Errorf("%s: appended", name)
			continue
		}
		if strings.Contains(err.Error(), secret) || !strings.Contains(err.Error(), "row 2") {
			t.Errorf("%s: %v", name, err)
		}
		if _, err := os.Stat(filepath.Join(dir, FileName)); !os.IsNotExist(err) {
			t.Errorf("%s: the rows file exists: %v", name, err)
		}
	}
}

// Load refuses a whole set when any line is not a row that passes the
// store's checks, a doctored one among them, and never repeats the line.
func TestLoadRefusesADoctoredFile(t *testing.T) {
	dir := t.TempDir()
	if err := (Store{Dir: dir}).Append(row("a", 1), row("b", 2)); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(dir, FileName)
	good, _ := os.ReadFile(file)
	lines := strings.SplitAfter(string(good), "\n")
	cases := map[string]string{
		"an extra field":  strings.Replace(lines[1], `"seed"`, `"text":"WAXWING-5 said so","seed"`, 1),
		"text in a field": strings.Replace(lines[1], `"scenario":"b"`, `"scenario":"WAXWING-5 said so"`, 1),
		"another format":  strings.Replace(lines[1], `"v":1`, `"v":2`, 1),
		"two values":      strings.TrimSuffix(lines[1], "\n") + `{"v":1}` + "\n",
		"a blank line":    "\n",
		"a part of a row": lines[1][:20],
		"not json":        "WAXWING-5\n",
		// A whole row that lost only its newline is still a part of a line.
		"no final newline": strings.TrimSuffix(lines[1], "\n"),
	}
	for name, last := range cases {
		if err := os.WriteFile(file, []byte(lines[0]+last), 0o644); err != nil {
			t.Fatal(err)
		}
		rows, err := Load(dir)
		if err == nil {
			t.Errorf("%s: loaded %d rows", name, len(rows))
			continue
		}
		if strings.Contains(err.Error(), "WAXWING") {
			t.Errorf("%s: the error repeats the line: %v", name, err)
		}
	}
	if _, err := Load(filepath.Join(dir, "missing")); err == nil {
		t.Error("a missing file loaded")
	}
}

// A rows file left with a part of a line, as by an interrupted write, takes
// no more rows: they would join the part and make the whole file
// unreadable. The file stays as it was.
func TestAppendRefusesAPartOfALine(t *testing.T) {
	dir := t.TempDir()
	if err := (Store{Dir: dir}).Append(row("a", 1), row("b", 2)); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(dir, FileName)
	good, _ := os.ReadFile(file)
	for name, cut := range map[string]int{"a part of a row": len(good) - 20, "a row without its newline": len(good) - 1} {
		if err := os.WriteFile(file, good[:cut], 0o644); err != nil {
			t.Fatal(err)
		}
		err := Store{Dir: dir}.Append(row("c", 3))
		if err == nil || !strings.Contains(err.Error(), "does not end with a whole line") {
			t.Errorf("%s: %v", name, err)
		}
		if data, _ := os.ReadFile(file); !bytes.Equal(data, good[:cut]) {
			t.Errorf("%s: the file changed:\n%s", name, data)
		}
	}
	// Once whole again, it takes rows.
	os.WriteFile(file, good, 0o644)
	if err := (Store{Dir: dir}).Append(row("c", 3)); err != nil {
		t.Fatal(err)
	}
	if rows, err := Load(dir); err != nil || len(rows) != 3 {
		t.Fatalf("%d rows, %v", len(rows), err)
	}
}

// scenario is a made-up scenario as an instrument reads it: an id, input
// text with a planted codename, and the codenames it plants.
type scenario struct {
	id     string
	input  string
	plants []string
}

// instrument measures a scenario the way a real one would: it reads the
// input and writes figures about it, ids and numbers only: value, or 0
// when the input lacks its first plant.
func instrument(s scenario, value float64) Row {
	r := row(s.id, value)
	r.Metric, r.Unit, r.Instrument = "fidelity.fact_recall", "ratio", "fidelity"
	if !strings.Contains(s.input, s.plants[0]) {
		r.Value = 0
	}
	return r
}

// A codename planted in a scenario's input reaches no row, no stored file
// and no report: the store refuses any row that carries it, in any case
// and in any field, and the gate, the diff and the baseline then print
// only ids and numbers.
func TestAPlantedCodenameIsInNoRowOrReport(t *testing.T) {
	isolate(t)
	repo, commit := newRepo(t)
	sc := scenario{id: "e.reversal.0007", input: "Use flock for the per-session lock, KESTREL-41.", plants: []string{"KESTREL-41"}}
	runDir := filepath.Join(t.TempDir(), "run")
	store := Store{Dir: runDir, Forbid: sc.plants}

	leaks := map[string]func(*Row){
		"scenario":     func(r *Row) { r.Scenario = "kestrel-41" },
		"metric":       func(r *Row) { r.Metric = "fidelity.kestrel-41" },
		"model":        func(r *Row) { r.Model = "x-kestrel-41" },
		"host_version": func(r *Row) { r.Host, r.HostVersion = "codex", "KESTREL-41" },
		"instrument":   func(r *Row) { r.Instrument = "kestrel-41" },
		"unit":         func(r *Row) { r.Unit = "kestrel-41" },
	}
	for field, leak := range leaks {
		r := instrument(sc, 1)
		r.Commit = commit
		leak(&r)
		err := store.Append(r)
		if err == nil || !strings.Contains(err.Error(), field) || strings.Contains(strings.ToLower(err.Error()), "kestrel") {
			t.Errorf("a codename in %s: %v", field, err)
		}
	}
	// A codename is found whatever spells its letters and digits: a row's
	// id can hold no space or underscore, so the codename's slug is what
	// would be stored.
	slugs := Store{Dir: runDir, Forbid: []string{"Blue Heron", "KESTREL_41", "--", ""}}
	for _, scenarioID := range []string{"blue-heron", "blue.heron", "e.blueheron.1", "kestrel-41", "kestrel.41", "x-kestrel41"} {
		r := instrument(sc, 1)
		r.Commit, r.Scenario = commit, scenarioID
		if err := slugs.Append(r); err == nil || !strings.Contains(err.Error(), "scenario holds a word the run forbids") || strings.Contains(err.Error(), "heron") {
			t.Errorf("the slug %s: %v", scenarioID, err)
		}
		if err := Forbidden([]Row{instrument(sc, 1), r}, slugs.Forbid); err == nil || err.Error() != "row 2: scenario holds a word the run forbids" {
			t.Errorf("Forbidden on the slug %s: %v", scenarioID, err)
		}
	}
	if _, err := os.Stat(filepath.Join(runDir, FileName)); !os.IsNotExist(err) {
		t.Fatalf("a refused row was written: %v", err)
	}
	// Words that are only punctuation match nothing, and an id that holds
	// none of the words passes.
	if err := Forbidden([]Row{instrument(sc, 1)}, []string{"--", "", "Blue Heron"}); err != nil {
		t.Errorf("a clean row: %v", err)
	}

	// The input itself is text, refused by the checks alone.
	r := instrument(sc, 1)
	r.Commit, r.Scenario = commit, sc.input
	if err := store.Append(r); err == nil {
		t.Error("the input was stored as a scenario")
	}

	var rows []Row
	for i, v := range []float64{1, 0.5, 1} {
		r := instrument(sc, v)
		r.Commit, r.Seed = commit, uint64(i)
		rows = append(rows, r)
	}
	if err := store.Append(rows...); err != nil {
		t.Fatal(err)
	}
	cat := catalogue(t)
	stored, err := Load(runDir)
	if err != nil {
		t.Fatal(err)
	}
	var report bytes.Buffer
	b, err := Accept(context.Background(), repo, cat, stored, filepath.Join(t.TempDir(), "baseline.json"))
	if err != nil {
		t.Fatal(err)
	}
	gates, err := LoadGates(filepath.Join("testdata", "gates.json"), cat)
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range Evaluate(context.Background(), repo, "HEAD", cat, gates, stored, b) {
		report.WriteString(v.Line + "\n")
	}
	if err := Diff(&report, cat, stored, b.Rows); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(runDir, FileName))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(report.String(), "fidelity.fact_recall") || bytes.Count(data, []byte("\n")) != 3 {
		t.Fatalf("the report or the rows are not what the run made:\n%s\n%s", report.String(), data)
	}
	for name, text := range map[string]string{"the rows file": string(data), "the report": report.String()} {
		lower := strings.ToLower(text)
		if strings.Contains(lower, "kestrel") || strings.Contains(lower, "flock") {
			t.Errorf("%s holds the scenario's input:\n%s", name, text)
		}
	}
}

func TestLoadCatalogueRefusesABadOne(t *testing.T) {
	if c := catalogue(t); len(c.Metrics) != 3 {
		t.Fatalf("the fixture catalogue has %d metrics", len(c.Metrics))
	}
	ok := `{"name":"a.b","unit":"ms","direction":"lower","paths":["internal/hook"]}`
	cases := map[string]string{
		"another format":    `{"v":2,"metrics":[` + ok + `]}`,
		"no metric":         `{"v":1,"metrics":[]}`,
		"twice":             `{"v":1,"metrics":[` + ok + `,` + ok + `]}`,
		"a bad direction":   `{"v":1,"metrics":[{"name":"a.b","unit":"ms","direction":"up","paths":["x"]}]}`,
		"no path":           `{"v":1,"metrics":[{"name":"a.b","unit":"ms","direction":"lower","paths":[]}]}`,
		"an absolute path":  `{"v":1,"metrics":[{"name":"a.b","unit":"ms","direction":"lower","paths":["/etc"]}]}`,
		"a path outside":    `{"v":1,"metrics":[{"name":"a.b","unit":"ms","direction":"lower","paths":["../x"]}]}`,
		"an unclean path":   `{"v":1,"metrics":[{"name":"a.b","unit":"ms","direction":"lower","paths":["a/../b"]}]}`,
		"the root":          `{"v":1,"metrics":[{"name":"a.b","unit":"ms","direction":"lower","paths":["."]}]}`,
		"a magic pathspec":  `{"v":1,"metrics":[{"name":"a.b","unit":"ms","direction":"lower","paths":[":(glob)**"]}]}`,
		"an option":         `{"v":1,"metrics":[{"name":"a.b","unit":"ms","direction":"lower","paths":["--output=x"]}]}`,
		"a name with text":  `{"v":1,"metrics":[{"name":"A b","unit":"ms","direction":"lower","paths":["x"]}]}`,
		"an unknown field":  `{"v":1,"metrics":[` + ok + `],"note":"x"}`,
		"a trailing value":  `{"v":1,"metrics":[` + ok + `]} {}`,
		"not a catalogue":   `[]`,
		"a unit with space": `{"v":1,"metrics":[{"name":"a.b","unit":"m s","direction":"lower","paths":["x"]}]}`,
	}
	for name, text := range cases {
		file := filepath.Join(t.TempDir(), "metrics.json")
		os.WriteFile(file, []byte(text), 0o644)
		if _, err := LoadCatalogue(file); err == nil {
			t.Errorf("%s: loaded", name)
		}
	}
	// Rows must name a metric of the catalogue, in its unit.
	c := catalogue(t)
	unknown := row("a", 1)
	unknown.Metric = "nothing.here"
	other := row("a", 1)
	other.Unit = "s"
	for _, r := range []Row{unknown, other} {
		if err := c.Check([]Row{row("a", 1), r}); err == nil {
			t.Errorf("%s in %s passed", r.Metric, r.Unit)
		}
	}
}

func TestLoadGatesRefusesABadOne(t *testing.T) {
	cat := catalogue(t)
	if g, err := LoadGates(filepath.Join("testdata", "gates.json"), cat); err != nil || len(g) != 3 {
		t.Fatalf("the fixture gates: %v, %v", g, err)
	}
	cases := map[string]string{
		"another format":      `{"v":2,"gates":[{"metric":"hook.capture_p95_ms","stat":"p95","rule":"not_worse","margin":1}]}`,
		"no gate":             `{"v":1,"gates":[]}`,
		"an unknown metric":   `{"v":1,"gates":[{"metric":"no.such","stat":"p95","rule":"not_worse","margin":1}]}`,
		"an unknown stat":     `{"v":1,"gates":[{"metric":"hook.capture_p95_ms","stat":"p99","rule":"not_worse","margin":1}]}`,
		"an unknown rule":     `{"v":1,"gates":[{"metric":"hook.capture_p95_ms","stat":"p95","rule":"equal","margin":1}]}`,
		"a negative margin":   `{"v":1,"gates":[{"metric":"hook.capture_p95_ms","stat":"p95","rule":"not_worse","margin":-1}]}`,
		"better by nothing":   `{"v":1,"gates":[{"metric":"hook.capture_p95_ms","stat":"p95","rule":"better","margin":0}]}`,
		"a repeated gate":     `{"v":1,"gates":[{"metric":"hook.capture_p95_ms","stat":"p95","rule":"not_worse","margin":1},{"metric":"hook.capture_p95_ms","stat":"p95","rule":"not_worse","margin":1}]}`,
		"an unknown field":    `{"v":1,"gates":[{"metric":"hook.capture_p95_ms","stat":"p95","rule":"not_worse","margin":1,"blocks":["release"]}]}`,
		"a margin as a word":  `{"v":1,"gates":[{"metric":"hook.capture_p95_ms","stat":"p95","rule":"not_worse","margin":"ten"}]}`,
		"no statistic at all": `{"v":1,"gates":[{"metric":"hook.capture_p95_ms","rule":"not_worse","margin":1}]}`,
	}
	for name, text := range cases {
		file := filepath.Join(t.TempDir(), "gates.json")
		os.WriteFile(file, []byte(text), 0o644)
		if _, err := LoadGates(file, cat); err == nil {
			t.Errorf("%s: loaded", name)
		}
	}
}

// gateOn evaluates one gate of the latency metric on rows of the given
// values against a baseline of the given values, at the baseline's own
// commit, so it is never stale.
func gateOn(t *testing.T, g Gate, baseline, rows []float64) Verdict {
	t.Helper()
	isolate(t)
	repo, commit := newRepo(t)
	mk := func(vs []float64) []Row {
		var out []Row
		for i, v := range vs {
			r := row("s"+string(rune('a'+i)), v)
			r.Commit = commit
			out = append(out, r)
		}
		return out
	}
	b := Baseline{V: Version, Commit: commit, Rows: mk(baseline)}
	v := Evaluate(context.Background(), repo, "HEAD", catalogue(t), []Gate{g}, mk(rows), b)
	if len(v) != 1 {
		t.Fatalf("%d verdicts", len(v))
	}
	return v[0]
}

// Each rule compares the figure it names with the baseline's, in the
// metric's direction, and holds at its margin exactly.
func TestGateRules(t *testing.T) {
	notWorse := Gate{Metric: "hook.capture_p95_ms", Stat: "mean", Rule: "not_worse", Margin: 2}
	tenPercent := Gate{Metric: "hook.capture_p95_ms", Stat: "max", Rule: "not_worse", Margin: 0.1, Relative: true}
	better := Gate{Metric: "hook.capture_p95_ms", Stat: "median", Rule: "better", Margin: 5}
	cases := []struct {
		name           string
		g              Gate
		baseline, rows []float64
		holds          bool
	}{
		{"the same", notWorse, []float64{10, 20}, []float64{10, 20}, true},
		{"worse at the margin", notWorse, []float64{10, 20}, []float64{12, 22}, true},
		{"worse beyond it", notWorse, []float64{10, 20}, []float64{12, 23}, false},
		{"better", notWorse, []float64{10, 20}, []float64{1, 2}, true},
		{"10% worse", tenPercent, []float64{40}, []float64{44}, true},
		{"over 10% worse", tenPercent, []float64{40}, []float64{44.5}, false},
		{"better by the margin", better, []float64{30}, []float64{25}, true},
		{"better by less", better, []float64{30}, []float64{26}, false},
		{"the same is not better", better, []float64{30}, []float64{30}, false},
	}
	for _, c := range cases {
		v := gateOn(t, c.g, c.baseline, c.rows)
		if v.Holds != c.holds {
			t.Errorf("%s: holds %v: %s", c.name, v.Holds, v.Line)
		}
		word := "FAIL "
		if c.holds {
			word = "hold "
		}
		if !strings.HasPrefix(v.Line, word+"hook.capture_p95_ms "+c.g.Stat+": ") || strings.Contains(v.Line, "\n") {
			t.Errorf("%s: %q", c.name, v.Line)
		}
	}
	// Higher is better for a ratio: a fall is worse.
	isolate(t)
	repo, commit := newRepo(t)
	recall := func(vs ...float64) []Row {
		var out []Row
		for i, v := range vs {
			r := row("s"+string(rune('a'+i)), v)
			r.Metric, r.Unit, r.Instrument, r.Commit = "fidelity.fact_recall", "ratio", "fidelity", commit
			out = append(out, r)
		}
		return out
	}
	g := Gate{Metric: "fidelity.fact_recall", Stat: "mean", Rule: "not_worse", Margin: 0.05}
	b := Baseline{V: Version, Commit: commit, Rows: recall(0.9, 0.9)}
	for _, c := range []struct {
		rows  []Row
		holds bool
	}{{recall(0.875, 0.875), true}, {recall(0.8, 0.8), false}, {recall(1, 1), true}} {
		if v := Evaluate(context.Background(), repo, "HEAD", catalogue(t), []Gate{g}, c.rows, b)[0]; v.Holds != c.holds {
			t.Errorf("recall %v: %s", c.rows[0].Value, v.Line)
		}
	}
	// At the margin exactly, in figures whose binary difference is not the
	// margin's binary value: 0.9 - 0.85 is 0.05000000000000004, 0.9 - 0.8
	// is 0.09999999999999998 and 0.29 of 100 is 28.999999999999996.
	betterBy := Gate{Metric: "fidelity.fact_recall", Stat: "mean", Rule: "better", Margin: 0.1}
	latency := Gate{Metric: "hook.capture_p95_ms", Stat: "p95", Rule: "not_worse", Margin: 0.29, Relative: true}
	ms := func(vs ...float64) []Row {
		var out []Row
		for i, v := range vs {
			r := row("s"+string(rune('a'+i)), v)
			r.Commit = commit
			out = append(out, r)
		}
		return out
	}
	for _, c := range []struct {
		g              Gate
		baseline, rows []Row
		holds          bool
		line           string
	}{
		{g, recall(0.9), recall(0.85), true, "hold fidelity.fact_recall mean: 0.85 ratio against the baseline's 0.9; may be worse (higher is better) by at most 0.05 ratio"},
		{g, recall(0.9), recall(0.8499), false, "FAIL fidelity.fact_recall mean: 0.8499 ratio"},
		{betterBy, recall(0.8), recall(0.9), true, "hold fidelity.fact_recall mean: 0.9 ratio against the baseline's 0.8; must be better (higher is better) by at least 0.1 ratio"},
		{betterBy, recall(0.8), recall(0.8999), false, "FAIL fidelity.fact_recall mean: 0.8999 ratio"},
		{latency, ms(100), ms(129), true, "hold hook.capture_p95_ms p95: 129 ms against the baseline's 100; may be worse (lower is better) by at most 29% of the baseline's figure"},
		{latency, ms(100), ms(129.01), false, "FAIL hook.capture_p95_ms p95: 129.01 ms"},
	} {
		b := Baseline{V: Version, Commit: commit, Rows: c.baseline}
		if v := Evaluate(context.Background(), repo, "HEAD", catalogue(t), []Gate{c.g}, c.rows, b)[0]; v.Holds != c.holds || !strings.HasPrefix(v.Line, c.line) {
			t.Errorf("%v against %v: %s", c.rows[0].Value, c.baseline[0].Value, v.Line)
		}
	}
}

// Every rule holds at its margin exactly, as a person writes the figures,
// and fails a hundredth of a unit beyond it: margins of 0.01 to 0.50 on
// ratios of 0.50 to 0.95 either way, and shares of 1% to 100% of whole
// milliseconds.
func TestGatesHoldAtTheirMarginExactly(t *testing.T) {
	cat := catalogue(t)
	ratio, _ := cat.Metric("fidelity.fact_recall")
	ms, _ := cat.Metric("hook.capture_p95_ms")
	dec := func(hundredths int) float64 { return float64(hundredths) / 100 }
	for base := 50; base <= 95; base += 5 {
		for m := 1; m <= 50; m++ {
			notWorse := Gate{Rule: "not_worse", Margin: dec(m)}
			better := Gate{Rule: "better", Margin: dec(m)}
			a := dec(base)
			if !notWorse.holds(ratio, a, dec(base-m)) || notWorse.holds(ratio, a, dec(base-m)-0.0001) {
				t.Errorf("%v worse by at most %v: %v", a, dec(m), dec(base-m))
			}
			if !better.holds(ratio, a, dec(base+m)) || better.holds(ratio, a, dec(base+m)-0.0001) {
				t.Errorf("%v better by at least %v: %v", a, dec(m), dec(base+m))
			}
		}
	}
	for _, base := range []int{100, 200, 300, 1000} {
		for p := 1; p <= 100; p++ {
			g := Gate{Rule: "not_worse", Margin: dec(p), Relative: true}
			a, c := float64(base), float64(base+base*p/100)
			if !g.holds(ms, a, c) || g.holds(ms, a, c+0.01) {
				t.Errorf("%v worse by at most %d%%: %v", a, p, c)
			}
			g.Rule = "better"
			if c := float64(base - base*p/100); !g.holds(ms, a, c) || g.holds(ms, a, c+0.01) {
				t.Errorf("%v better by at least %d%%: %v", a, p, c)
			}
		}
	}
}

// One doctored row is enough to flip a gate, either way: a passing set
// fails when one row is raised past the margin, and a failing set passes
// when one row is lowered inside it.
func TestADoctoredRowFlipsAGate(t *testing.T) {
	g := Gate{Metric: "hook.capture_p95_ms", Stat: "p95", Rule: "not_worse", Margin: 0.1, Relative: true}
	baseline := []float64{30, 32, 34, 36, 40}
	for _, c := range []struct {
		name          string
		honest        []float64
		doctoredValue float64
		holds         bool
	}{
		{"a passing set, one row raised", []float64{30, 32, 34, 36, 44}, 45, true},
		{"a failing set, one row lowered", []float64{30, 32, 34, 36, 45}, 44, false},
	} {
		if v := gateOn(t, g, baseline, c.honest); v.Holds != c.holds {
			t.Fatalf("%s: the honest rows: %s", c.name, v.Line)
		}
		doctored := slices.Clone(c.honest)
		doctored[4] = c.doctoredValue
		if v := gateOn(t, g, baseline, doctored); v.Holds == c.holds {
			t.Fatalf("%s: the doctored rows did not flip it: %s", c.name, v.Line)
		}
	}
}

// A gate fails, saying why, when it has nothing fair to compare: no rows
// of the metric, no baseline of it, rows that mix hosts or models, or rows
// measured on another host or model than the baseline's.
func TestAGateFailsWithoutAFairComparison(t *testing.T) {
	isolate(t)
	repo, commit := newRepo(t)
	cat := catalogue(t)
	g := Gate{Metric: "hook.capture_p95_ms", Stat: "mean", Rule: "not_worse", Margin: 100}
	on := func(host, model string) Row {
		r := row("a", 1)
		r.Commit, r.Host, r.Model = commit, host, model
		if host != "" {
			r.HostVersion = "1.0"
		}
		return r
	}
	other := row("a", 1)
	other.Metric, other.Unit, other.Instrument, other.Commit = "fidelity.fact_recall", "ratio", "fidelity", commit
	cases := []struct {
		name     string
		rows     []Row
		baseline []Row
		says     string
	}{
		{"no rows", []Row{other}, []Row{on("", "")}, "the rows hold no figure"},
		{"no baseline", []Row{on("", "")}, []Row{other}, "the baseline holds no figure"},
		{"mixed hosts", []Row{on("codex", ""), on("claude-code", "")}, []Row{on("codex", "")}, "the rows mix"},
		{"mixed baseline", []Row{on("codex", "")}, []Row{on("codex", "m1"), on("codex", "m2")}, "the baseline: the rows mix"},
		{"another model", []Row{on("codex", "m2")}, []Row{on("codex", "m1")}, "measured by latency on codex with m2, the baseline by latency on codex with m1"},
		{"another host", []Row{on("claude-code", "")}, []Row{on("codex", "")}, "measured by"},
	}
	for _, c := range cases {
		v := Evaluate(context.Background(), repo, "HEAD", cat, []Gate{g}, c.rows, Baseline{V: Version, Commit: commit, Rows: c.baseline})[0]
		if v.Holds || !strings.Contains(v.Line, c.says) || !strings.HasPrefix(v.Line, "FAIL ") {
			t.Errorf("%s: %s", c.name, v.Line)
		}
	}
	if Hold(nil) {
		t.Error("no verdicts hold")
	}
}

// A baseline goes stale, and fails its gates saying so, when a path the
// catalogue lists for its metric changes after its commit; a change
// elsewhere leaves it fresh, and so does a base before the change.
func TestABaselineGoesStaleWhenACoveredPathChanges(t *testing.T) {
	isolate(t)
	repo, commit := newRepo(t)
	cat := catalogue(t)
	g := Gate{Metric: "hook.capture_p95_ms", Stat: "mean", Rule: "not_worse", Margin: 1}
	mk := func(c string) Row {
		r := row("a", 10)
		r.Commit = c
		return r
	}
	b := Baseline{V: Version, Commit: commit, Rows: []Row{mk(commit)}}
	verdict := func(base string) Verdict {
		return Evaluate(context.Background(), repo, base, cat, []Gate{g}, []Row{mk(commit)}, b)[0]
	}
	if v := verdict("HEAD"); !v.Holds {
		t.Fatalf("fresh: %s", v.Line)
	}
	commitFile(t, repo, "docs/notes.md", "more notes\n")
	if v := verdict("HEAD"); !v.Holds {
		t.Fatalf("after a change outside the covered paths: %s", v.Line)
	}
	before := gitIn(t, repo, "rev-parse", "HEAD")
	commitFile(t, repo, "internal/hook/capture.go", "package hook\n\nvar x = 1\n")
	v := verdict("HEAD")
	if v.Holds || !strings.Contains(v.Line, "the baseline is stale: internal/hook changed between its commit "+commit[:12]) {
		t.Fatalf("after a change to internal/hook: %s", v.Line)
	}
	if v := verdict("refs/heads/main"); v.Holds || !strings.Contains(v.Line, commit[:12]+" and refs/heads/main; accept a new baseline") {
		t.Fatalf("at a named branch: %s", v.Line)
	}
	if v := verdict(before); !v.Holds {
		t.Fatalf("at the commit before the change: %s", v.Line)
	}
	// A file the catalogue names alone, removed, counts too.
	if p, err := Changed(context.Background(), repo, commit, "HEAD", []string{"scripts/benchmark.py", "internal/hook"}); err != nil || p != "internal/hook" {
		t.Fatalf("changed: %q, %v", p, err)
	}
	commitFile(t, repo, "scripts/benchmark.py", "print(1)\n")
	at := gitIn(t, repo, "rev-parse", "HEAD")
	gitIn(t, repo, "rm", "--quiet", "scripts/benchmark.py")
	gitIn(t, repo, "commit", "--quiet", "-m", "remove")
	if p, err := Changed(context.Background(), repo, at, "HEAD", []string{"scripts/benchmark.py"}); err != nil || p != "scripts/benchmark.py" {
		t.Fatalf("removed: %q, %v", p, err)
	}
	// A path is taken literally, never as a pattern, and from the root of
	// the repository wherever in it the command runs; a path in neither
	// commit, which could never go stale, is an error.
	if p, err := Changed(context.Background(), repo, commit, "HEAD", []string{"internal/*"}); err == nil || p != "" || !strings.Contains(err.Error(), "in neither commit") {
		t.Fatalf("a pattern: %q, %v", p, err)
	}
	if p, err := Changed(context.Background(), filepath.Join(repo, "docs"), commit, "HEAD", []string{"internal/hook"}); err != nil || p != "internal/hook" {
		t.Fatalf("from a folder of the repository: %q, %v", p, err)
	}
	if p, err := Changed(context.Background(), repo, commit, commit, []string{"internal/hok"}); err == nil || p != "" {
		t.Fatalf("a misspelt path: %q, %v", p, err)
	}
	// A commit the repository lacks, or a base that is an option, is an
	// error, and the gate fails saying it cannot tell.
	missing := Baseline{V: Version, Commit: commitA, Rows: []Row{row("a", 10)}}
	if v := Evaluate(context.Background(), repo, "HEAD", cat, []Gate{g}, []Row{row("a", 10)}, missing)[0]; v.Holds || !strings.Contains(v.Line, "cannot tell whether the baseline is stale") {
		t.Fatalf("a missing commit: %s", v.Line)
	}
	if v := verdict("--output=x"); v.Holds || !strings.Contains(v.Line, "cannot tell") {
		t.Fatalf("an option as the base: %s", v.Line)
	}
}

// A gate judges a branch's change against the baseline of the commit the
// branch starts from: a covered path the branch changes is what the gate
// judges and leaves the baseline fresh, while one the main line changes
// after the baseline makes it stale.
func TestStartOfAChange(t *testing.T) {
	isolate(t)
	repo, commit := newRepo(t)
	cat := catalogue(t)
	ctx := context.Background()
	g := Gate{Metric: "hook.capture_p95_ms", Stat: "mean", Rule: "not_worse", Margin: 1}
	at := func(c string) []Row {
		r := row("a", 10)
		r.Commit = c
		return []Row{r}
	}
	b := Baseline{V: Version, Commit: commit, Rows: at(commit)}

	gitIn(t, repo, "switch", "--quiet", "-c", "change")
	branch := commitFile(t, repo, "internal/hook/capture.go", "package hook\n\nvar x = 1\n")
	if base, err := StartOf(ctx, repo, at(branch)); err != nil || base != commit {
		t.Fatalf("the branch starts from %q, %v; want %s", base, err, commit)
	}
	if v := Evaluate(ctx, repo, commit, cat, []Gate{g}, at(branch), b)[0]; !v.Holds {
		t.Fatalf("the branch's own change made the baseline stale: %s", v.Line)
	}

	gitIn(t, repo, "switch", "--quiet", "main")
	moved := commitFile(t, repo, "internal/hook/other.go", "package hook\n\nvar y = 2\n")
	if base, err := StartOf(ctx, repo, at(branch)); err != nil || base != commit {
		t.Fatalf("after main moved, the branch starts from %q, %v", base, err)
	}
	base, err := StartOf(ctx, repo, at(moved))
	if err != nil || base != moved {
		t.Fatalf("a commit of main starts from %q, %v", base, err)
	}
	if v := Evaluate(ctx, repo, base, cat, []Gate{g}, at(moved), b)[0]; v.Holds || !strings.Contains(v.Line, "the baseline is stale: internal/hook changed between its commit "+commit[:12]+" and "+moved[:12]) {
		t.Fatalf("main moved past the baseline: %s", v.Line)
	}

	// A gate judges rows of one commit: StartOf refuses more, and so does
	// each gate whose metric's rows mix them.
	mixed := append(at(commit), at(branch)...)
	if _, err := StartOf(ctx, repo, mixed); err == nil || !strings.Contains(err.Error(), "measured on 2 commits") {
		t.Fatalf("two commits: %v", err)
	}
	if v := Evaluate(ctx, repo, commit, cat, []Gate{g}, mixed, b)[0]; v.Holds || !strings.Contains(v.Line, "the rows were measured on 2 commits") {
		t.Fatalf("a gate on two commits: %s", v.Line)
	}
	for name, rows := range map[string][]Row{"no rows": nil, "a missing commit": at(commitA)} {
		if base, err := StartOf(ctx, repo, rows); err == nil || base != "" {
			t.Errorf("%s: %q", name, base)
		}
	}

	// Without a local main, the remote's is the main line; without either,
	// there is no start to find.
	gitIn(t, repo, "branch", "--quiet", "-m", "main", "trunk")
	if _, err := StartOf(ctx, repo, at(branch)); err == nil || !strings.Contains(err.Error(), "no main branch") {
		t.Fatalf("no main: %v", err)
	}
	gitIn(t, repo, "update-ref", "refs/remotes/origin/main", moved)
	if base, err := StartOf(ctx, repo, at(branch)); err != nil || base != commit {
		t.Fatalf("origin/main: %q, %v", base, err)
	}
}

// The gate's first line names what it judges: the rows' commit and tree,
// the baseline's commit and the base.
func TestHeader(t *testing.T) {
	const commitB = "89abcdef0123456789abcdef0123456789abcdef"
	at := func(c string, dirty bool) Row {
		r := row("a", 1)
		r.Commit, r.Dirty = c, dirty
		return r
	}
	b := Baseline{V: Version, Commit: commitA}
	for _, c := range []struct {
		rows []Row
		base string
		want string
	}{
		{[]Row{at(commitB, false)}, commitA, "rows of commit 89abcdef0123 on a clean tree, against the baseline of commit 0123456789ab, which must still describe 0123456789ab"},
		{[]Row{at(commitB, false), at(commitB, true)}, "HEAD", "rows of commit 89abcdef0123 on a dirty tree, against the baseline of commit 0123456789ab, which must still describe HEAD"},
		{[]Row{at(commitB, false), at(commitA, false)}, commitB, "rows of 2 commits, against the baseline of commit 0123456789ab, which must still describe 89abcdef0123"},
		{nil, commitA, "no rows, against the baseline of commit 0123456789ab, which must still describe 0123456789ab"},
	} {
		if got := Header(c.rows, b, c.base); got != c.want {
			t.Errorf("got  %q\nwant %q", got, c.want)
		}
	}
}

// Accept refuses rows it cannot stand behind and writes the baseline
// otherwise, and LoadBaseline reads back only what Accept could write.
func TestAcceptAndLoadBaseline(t *testing.T) {
	isolate(t)
	repo, commit := newRepo(t)
	cat := catalogue(t)
	ctx := context.Background()
	at := func(c string, dirty bool) Row {
		r := row("a", 10)
		r.Commit, r.Dirty = c, dirty
		return r
	}
	file := filepath.Join(t.TempDir(), "baselines", "baseline.json")
	refusals := map[string][]Row{
		"no rows":           nil,
		"a dirty tree":      {at(commit, false), at(commit, true)},
		"two commits":       {at(commit, false), at(commitA, false)},
		"a missing commit":  {at(commitA, false)},
		"an unknown metric": {func() Row { r := at(commit, false); r.Metric = "no.such"; return r }()},
		"two hosts":         {at(commit, false), func() Row { r := at(commit, false); r.Host, r.HostVersion = "codex", "1.0"; return r }()},
		"two models": {func() Row { r := at(commit, false); r.Model = "m1"; return r }(),
			func() Row { r := at(commit, false); r.Model = "m2"; return r }()},
	}
	for name, rows := range refusals {
		_, err := Accept(ctx, repo, cat, rows, file)
		if err == nil {
			t.Errorf("%s: accepted", name)
		} else if strings.HasPrefix(name, "two ") && name != "two commits" && !strings.Contains(err.Error(), "hook.capture_p95_ms: the rows mix") {
			t.Errorf("%s: %v", name, err)
		}
		if _, err := os.Stat(file); !os.IsNotExist(err) {
			t.Fatalf("%s: the file was written", name)
		}
	}
	b, err := Accept(ctx, repo, cat, []Row{at(commit, false), at(commit, false)}, file)
	if err != nil {
		t.Fatal(err)
	}
	got, err := LoadBaseline(file, cat)
	if err != nil || got.Commit != commit || len(got.Rows) != 2 || !reflect.DeepEqual(got, b) {
		t.Fatalf("loaded %+v, %v", got, err)
	}
	names, _ := os.ReadDir(filepath.Dir(file))
	if len(names) != 1 {
		t.Fatalf("the folder holds %d files", len(names))
	}
	data, _ := os.ReadFile(file)
	doctored := map[string]string{
		"a dirty row":    strings.Replace(string(data), `"dirty": false`, `"dirty": true`, 1),
		"another commit": strings.Replace(string(data), `"commit": "`+commit+`",`+"\n  \"rows\"", `"commit": "`+commitA+`",`+"\n  \"rows\"", 1),
		"text in a row":  strings.Replace(string(data), `"scenario": "a"`, `"scenario": "said so"`, 1),
		"an extra field": strings.Replace(string(data), `"rows"`, `"note": "x", "rows"`, 1),
		"no rows":        `{"v":1,"commit":"` + commit + `","rows":[]}`,
		"another format": strings.Replace(string(data), `"v": 1,`, `"v": 3,`, 1),
		"a short commit": `{"v":1,"commit":"abc","rows":[]}`,
		"not a baseline": `[]`,
		// One metric measured on two hosts: every other check passes.
		"a second host": strings.Replace(string(data), `"scenario": "a",`, `"scenario": "a", "host": "codex", "host_version": "1.0",`, 1),
	}
	for name, text := range doctored {
		if text == string(data) {
			t.Fatalf("%s: the doctoring changed nothing", name)
		}
		bad := filepath.Join(t.TempDir(), "baseline.json")
		os.WriteFile(bad, []byte(text), 0o644)
		if _, err := LoadBaseline(bad, cat); err == nil {
			t.Errorf("%s: loaded", name)
		} else if name == "a second host" && !strings.Contains(err.Error(), "hook.capture_p95_ms: the rows mix") {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestSource(t *testing.T) {
	isolate(t)
	repo, commit := newRepo(t)
	ctx := context.Background()
	if c, dirty, err := Source(ctx, repo); err != nil || c != commit || dirty {
		t.Fatalf("clean: %s %v %v", c, dirty, err)
	}
	os.WriteFile(filepath.Join(repo, "new.go"), []byte("package x\n"), 0o644)
	if _, dirty, err := Source(ctx, repo); err != nil || !dirty {
		t.Fatalf("an untracked file: %v %v", dirty, err)
	}
	os.Remove(filepath.Join(repo, "new.go"))
	os.WriteFile(filepath.Join(repo, "docs", "notes.md"), []byte("changed\n"), 0o644)
	if _, dirty, err := Source(ctx, repo); err != nil || !dirty {
		t.Fatalf("a changed file: %v %v", dirty, err)
	}
	if _, _, err := Source(ctx, t.TempDir()); err == nil {
		t.Fatal("a folder that is no repository has a commit")
	}
}

func TestDiff(t *testing.T) {
	cat := catalogue(t)
	recall := func(v float64) Row {
		r := row("a", v)
		r.Metric, r.Unit, r.Instrument = "fidelity.fact_recall", "ratio", "fidelity"
		return r
	}
	codex := func(v float64) Row {
		r := row("a", v)
		r.Host, r.HostVersion = "codex", "1.0"
		return r
	}
	first := []Row{row("a", 10), row("b", 20), recall(0.5), codex(5)}
	second := []Row{row("a", 12), row("b", 22), row("c", 23), recall(0.75)}
	var out bytes.Buffer
	if err := Diff(&out, cat, first, second); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSuffix(out.String(), "\n"), "\n")
	want := [][]string{
		{"metric", "measured", "by", "unit", "first", "n", "mean", "median", "p95", "second", "n", "mean", "median", "p95", "second", "against", "first"},
		{"fidelity.fact_recall", "fidelity", "ratio", "1", "0.5", "0.5", "0.5", "1", "0.75", "0.75", "0.75", "better", "by", "0.25"},
		{"hook.capture_p95_ms", "latency", "ms", "2", "15", "15", "20", "3", "19", "22", "23", "worse", "by", "4"},
		{"hook.capture_p95_ms", "latency", "on", "codex", "ms", "1", "5", "5", "5", "0", "-", "-", "-", "-"},
	}
	if len(lines) != len(want) {
		t.Fatalf("got:\n%s", out.String())
	}
	for i := range want {
		if got := strings.Fields(lines[i]); !slices.Equal(got, want[i]) {
			t.Errorf("line %d: %q, want %q", i+1, got, want[i])
		}
	}
	unknown := row("a", 1)
	unknown.Unit = "s"
	if err := Diff(&out, cat, first, []Row{unknown}); err == nil {
		t.Error("a row in another unit was compared")
	}
	// A difference reads as the figures do, without the rounding of
	// binary figures: 0.8 to 0.9 is better by 0.1, and 0.1 and 0.2 make
	// the same mean as 0.15 and 0.15.
	for _, c := range []struct {
		first, second []Row
		says          string
	}{
		{[]Row{recall(0.8)}, []Row{recall(0.9)}, "better by 0.1"},
		{[]Row{recall(0.9)}, []Row{recall(0.85)}, "worse by 0.05"},
		{[]Row{recall(0.1), recall(0.2)}, []Row{recall(0.15), recall(0.15)}, "the same"},
	} {
		out.Reset()
		if err := Diff(&out, cat, c.first, c.second); err != nil {
			t.Fatal(err)
		}
		if line := strings.Split(out.String(), "\n")[1]; !strings.HasSuffix(strings.TrimRight(line, " "), "  "+c.says) {
			t.Errorf("%v to %v: %q", c.first[0].Value, c.second[0].Value, line)
		}
	}
}
