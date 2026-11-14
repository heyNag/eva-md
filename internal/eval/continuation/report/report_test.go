package report

import (
	"math"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/heyNag/eva-md/internal/eval/continuation/drive"
	"github.com/heyNag/eva-md/internal/eval/continuation/task"
	"github.com/heyNag/eva-md/internal/eval/results"
)

// The testdata folders hold made-up trials, written by hand from tables:
//
//	a: memory off on codex in two runs, the second of another commit, on a
//	   dirty tree, and the ideal record in one, its control trial naming no
//	   model;
//	b: eva on codex (hid1, two trials, one asked-again row among them), and
//	   memory off on claude-code (ctl1, one trial);
//	c: an eva run on codex that judged none and stopped on no_usage, with
//	   no rows file.
//
// Every figure the tests below expect was worked out by hand from those
// tables, and the logarithms checked apart.

// testTasks are the tasks the testdata's rows name, in the order the
// report gives them, each with a made-up value no output may hold.
func testTasks() []task.Task {
	mk := func(id, family, value, repo string) task.Task {
		return task.Task{ID: id, Family: family, Repos: []string{repo},
			Facts: []task.Fact{{ID: "f-one", Statement: "made up", Values: []string{value}}}}
	}
	return []task.Task{
		mk("hid1", task.FamilyHidden, "zanzibrel", "quokkaline"),
		mk("hid2", task.FamilyHidden, "marrowfen", "quokkaline"),
		mk("hid3", task.FamilyHidden, "pellucine", "thistlemoor"),
		mk("hand1", task.FamilyHandoff, "brackenhue", "thistlemoor"),
		mk("hand2", task.FamilyHandoff, "cobaltrine", "wrenhollow"),
		mk("ctl1", task.FamilyControl, "duskwillow", "wrenhollow"),
	}
}

func readTestdata(t *testing.T, names ...string) []Folder {
	t.Helper()
	var out []Folder
	for _, n := range names {
		f, err := ReadFolder(filepath.Join("testdata", n), task.Catalogue())
		if err != nil {
			t.Fatalf("%s: %v", n, err)
		}
		out = append(out, f)
	}
	return out
}

var testRates = &Rates{In: 3, Cached: 0.3, Out: 15}

func build(t *testing.T) Report {
	t.Helper()
	r, err := Build(testTasks(), readTestdata(t, "a", "b", "c"), testRates)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func near(a, b float64) bool { return math.Abs(a-b) <= 1e-12*math.Max(1, math.Abs(b)) }

func hostOf(t *testing.T, r Report, id string) Host {
	t.Helper()
	for _, h := range r.Hosts {
		if h.ID == id {
			return h
		}
	}
	t.Fatalf("no host %s", id)
	return Host{}
}

func taskOf(t *testing.T, h Host, id string, c task.Condition) TaskFigures {
	t.Helper()
	for _, f := range h.Tasks {
		if f.Task == id && f.Condition == c {
			return f
		}
	}
	t.Fatalf("no figures of %s under %s", id, c)
	return TaskFigures{}
}

func costOfHost(t *testing.T, h Host, c task.Condition) Cost {
	t.Helper()
	for _, k := range h.Costs {
		if k.Condition == c {
			return k
		}
	}
	t.Fatalf("no cost under %s", c)
	return Cost{}
}

func codes(m map[string]int) map[string]int {
	out := map[string]int{}
	for _, c := range drive.Codes {
		out[c] = 0
	}
	for k, v := range m {
		out[k] = v
	}
	return out
}

// The hosts come by id, a host's conditions in the instrument's order, and
// the runs' figures are the records' summed: two records' unjudged counts
// add, two commits are both shown, and the models each once.
func TestRunsFromTheRecords(t *testing.T) {
	r := build(t)
	if len(r.Hosts) != 2 || r.Hosts[0].ID != "claude-code" || r.Hosts[1].ID != "codex" {
		t.Fatalf("hosts %+v", r.Hosts)
	}
	h := hostOf(t, r, "codex")
	if !slices.Equal(h.Conditions, []task.Condition{task.Off, task.Eva, task.Oracle}) {
		t.Fatalf("conditions %v", h.Conditions)
	}
	a, b := strings.Repeat("1", 40), strings.Repeat("2", 40)
	want := []Runs{
		{Condition: task.Off, Records: 2, Ended: map[string]int{"done": 2}, Planned: 18, Judged: 11, NotRun: 2,
			Unjudged: codes(map[string]int{"sign_in": 3, "timeout": 1, "host_failed": 1}), Tokens: 45000,
			Commits: []Commit{{a, false}, {b, true}}, HostVersions: []string{"1.2.3", "1.2.4"},
			ModelsAsked: []string{"made-up-model-1"}, ModelsAnswered: []string{"made-up-model-1"}},
		{Condition: task.Eva, Records: 2, Ended: map[string]int{"done": 1, "no_usage": 1}, Planned: 3, Judged: 2,
			Unjudged: codes(map[string]int{"no_usage": 1}), Tokens: 15000,
			Commits: []Commit{{a, false}}, HostVersions: []string{"1.2.3"},
			ModelsAsked: []string{"made-up-model-1"}, ModelsAnswered: []string{"made-up-model-1"}},
		{Condition: task.Oracle, Records: 1, Ended: map[string]int{"done": 1}, Planned: 18, Judged: 12, NotRun: 5,
			Unjudged: codes(map[string]int{"rate_limited": 1}), NoModel: 1, Tokens: 30000,
			Commits: []Commit{{a, false}}, HostVersions: []string{"1.2.3"},
			ModelsAsked: []string{""}, ModelsAnswered: []string{"", "made-up-model-2"}},
	}
	if !reflect.DeepEqual(h.Runs, want) {
		t.Fatalf("runs\n%+v\nwant\n%+v", h.Runs, want)
	}
}

// Each task's figures under each condition, from its judged trials: odd
// and even counts' medians, least and most, and eva's memory.
func TestTaskFigures(t *testing.T) {
	h := hostOf(t, build(t), "codex")
	type st = Stat
	cases := []struct {
		task                     string
		cond                     task.Condition
		judged, solved, firstTry int
		stats                    map[string]Stat
	}{
		{"hid1", task.Off, 3, 2, 1, map[string]Stat{
			"corrections": st{3, 2, 0, 5}, "rounds": st{3, 3, 1, 5}, "agent_events": st{3, 10, 8, 20},
			"prompt_tokens": st{3, 1000, 500, 3000}, "cached_tokens": st{3, 800, 400, 2000}, "output_tokens": st{3, 100, 50, 300},
			"wall_ms": st{3, 120000, 60000, 300000}}},
		{"hand1", task.Oracle, 2, 2, 2, map[string]Stat{
			"corrections": st{2, 0, 0, 0}, "rounds": st{2, 1, 1, 1}, "agent_events": st{2, 14, 12, 16},
			"prompt_tokens": st{2, 1250, 1000, 1500}, "cached_tokens": st{2, 750, 500, 1000}, "output_tokens": st{2, 125, 100, 150},
			"wall_ms": st{2, 135000, 120000, 150000}}},
		{"hid1", task.Eva, 2, 2, 1, map[string]Stat{
			"corrections": st{2, 0.5, 0, 1}, "rounds": st{2, 1.5, 1, 2}, "agent_events": st{2, 6, 0, 12},
			"prompt_tokens": st{2, 1150, 800, 1500}, "cached_tokens": st{2, 800, 600, 1000}, "output_tokens": st{2, 115, 80, 150},
			"wall_ms": st{2, 150000, 100000, 200000}, "memory_tokens": st{2, 6000, 5000, 7000}, "memory_ms": st{2, 40000, 30000, 50000}}},
		// A task with no judged trial under a condition is listed, with none.
		{"hid2", task.Eva, 0, 0, 0, map[string]Stat{
			"corrections": {}, "rounds": {}, "agent_events": {}, "prompt_tokens": {}, "cached_tokens": {}, "output_tokens": {},
			"wall_ms": {}, "memory_tokens": {}, "memory_ms": {}}},
	}
	for _, c := range cases {
		f := taskOf(t, h, c.task, c.cond)
		if f.Judged != c.judged || f.Solved != c.solved || f.FirstTry != c.firstTry || !reflect.DeepEqual(f.Stats, c.stats) {
			t.Errorf("%s %s: %+v\nwant %+v", c.task, c.cond, f, c)
		}
	}
	// Six tasks by three conditions, in the tasks' order, then the
	// conditions'.
	if len(h.Tasks) != 18 || h.Tasks[0].Task != "hid1" || h.Tasks[1].Condition != task.Eva || h.Tasks[17].Task != "ctl1" {
		t.Fatalf("order: %d figures", len(h.Tasks))
	}
}

// A session's cost: medians over every judged trial, the price of each at
// the rates, and eva's memory apart at the uncached rate.
func TestCost(t *testing.T) {
	h := hostOf(t, build(t), "codex")
	off := costOfHost(t, h, task.Off)
	wantOff := map[string]float64{"corrections": 1, "rounds": 2, "agent_events": 10, "prompt_tokens": 1000, "cached_tokens": 800, "output_tokens": 100, "wall_ms": 120000}
	// Prices of the eleven trials, in millionths: 1170, 1530, 1890, 2340,
	// 2430, 3150, 4050, 4950, 6300, 8100, 9900.
	if off.Judged != 11 || !reflect.DeepEqual(off.Medians, wantOff) || !near(off.Price, 0.00315) || off.MemoryPrice != 0 {
		t.Errorf("off %+v", off)
	}
	oracle := costOfHost(t, h, task.Oracle)
	wantOracle := map[string]float64{"corrections": 0, "rounds": 1, "agent_events": 5.5, "prompt_tokens": 650, "cached_tokens": 500, "output_tokens": 55, "wall_ms": 65000}
	// Twelve prices: the middle two are 1680 and 1710 millionths.
	if oracle.Judged != 12 || !reflect.DeepEqual(oracle.Medians, wantOracle) || !near(oracle.Price, 0.001695) {
		t.Errorf("oracle %+v", oracle)
	}
	eva := costOfHost(t, h, task.Eva)
	wantEva := map[string]float64{"corrections": 0.5, "rounds": 1.5, "agent_events": 6, "prompt_tokens": 1150, "cached_tokens": 800, "output_tokens": 115,
		"wall_ms": 150000, "memory_tokens": 6000, "memory_ms": 40000}
	// Prices 1980 and 4050 millionths; memory 15000 and 21000.
	if eva.Judged != 2 || !reflect.DeepEqual(eva.Medians, wantEva) || !near(eva.Price, 0.003015) || !near(eva.MemoryPrice, 0.018) {
		t.Errorf("eva %+v", eva)
	}
	// Without rates there is no price.
	r, err := Build(testTasks(), readTestdata(t, "a", "b", "c"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if k := costOfHost(t, hostOf(t, r, "codex"), task.Eva); k.Price != 0 || k.MemoryPrice != 0 {
		t.Errorf("a price with no rates: %+v", k)
	}
}

// The price of one trial counts uncached prompt, cached and output tokens
// each at its own rate.
func TestPrice(t *testing.T) {
	r := Rates{In: 2, Cached: 0.5, Out: 10}
	// 600 uncached at 2, 400 cached at 0.5, 30 output at 10.
	if got := r.Price(1000, 400, 30); !near(got, (1200+200+300)/1e6) {
		t.Fatalf("price %v", got)
	}
}

// The variance of each arm against memory off, from the pairs both judged
// with both figures above 0: eva's events give one pair, since its first
// trial made none, so no s.
func TestVariance(t *testing.T) {
	h := hostOf(t, build(t), "codex")
	if len(h.Variance) != 2 || h.Variance[0].Arm != task.Eva || h.Variance[1].Arm != task.Oracle {
		t.Fatalf("variance %+v", h.Variance)
	}
	check := func(name string, got Pairs, n int, mean, s float64, need int) {
		t.Helper()
		if got.N != n || !near(got.Mean, mean) || !near(got.S, s) || got.Need != need {
			t.Errorf("%s: %+v, want %d %v %v %d", name, got, n, mean, s, need)
		}
	}
	eva, oracle := h.Variance[0], h.Variance[1]
	check("eva events", eva.AgentEvents, 1, math.Log(12.0/20), 0, 0)
	check("eva prompt", eva.Prompt, 2, (math.Log(0.8)+math.Log(0.5))/2, math.Abs(math.Log(0.8)-math.Log(0.5))/math.Sqrt2, 13)
	check("oracle events", oracle.AgentEvents, 11, -0.468780836974485, 0.5335288352327011, 31)
	check("oracle prompt", oracle.Prompt, 11, -0.601824836957305, 0.6731580851551637, 50)
	// A host with memory off alone has none.
	if c := hostOf(t, build(t), "claude-code"); len(c.Variance) != 0 {
		t.Errorf("claude-code: %+v", c.Variance)
	}
}

// The screens: a hidden decision passes, fails on the ideal record's one
// first try or on memory off's one miss; a handoff passes at exactly 0.7;
// one judged trial of memory off is too few; a control task has none.
func TestScreens(t *testing.T) {
	r := build(t)
	want := []Screen{
		{"hid1", "hidden", ScreenPass}, {"hid2", "hidden", ScreenFail}, {"hid3", "hidden", ScreenFail},
		{"hand1", "handoff", ScreenPass}, {"hand2", "handoff", ScreenTooFew}, {"ctl1", "control", ScreenNone},
	}
	h := hostOf(t, r, "codex")
	if !reflect.DeepEqual(h.Screens, want) || h.Passed != 2 {
		t.Fatalf("screens %+v, passed %d", h.Screens, h.Passed)
	}
	c := hostOf(t, r, "claude-code")
	for _, s := range c.Screens {
		if want := ScreenTooFew; s.Family == "control" {
			want = ScreenNone
			if s.Verdict != want {
				t.Errorf("%+v", s)
			}
		} else if s.Verdict != want {
			t.Errorf("%+v", s)
		}
	}
}

// The edges of each screen, on trials made here.
func TestScreenEdges(t *testing.T) {
	tr := func(solved, first, events float64) *trial {
		return &trial{values: map[string]float64{"solved": solved, "first_try": first, "agent_events": events}}
	}
	hidden := task.Task{ID: "x", Family: task.FamilyHidden}
	handoff := task.Task{ID: "y", Family: task.FamilyHandoff}
	cases := []struct {
		name        string
		t           task.Task
		off, oracle []*trial
		want        string
	}{
		{"hidden, two and two", hidden, []*trial{tr(0, 0, 1), tr(1, 0, 1)}, []*trial{tr(1, 1, 1), tr(1, 1, 1)}, ScreenPass},
		{"hidden, the ideal record once", hidden, []*trial{tr(0, 0, 1), tr(0, 0, 1)}, []*trial{tr(1, 1, 1), tr(1, 0, 1)}, ScreenFail},
		{"hidden, memory off missed once", hidden, []*trial{tr(1, 1, 1), tr(0, 0, 1), tr(1, 1, 1)}, []*trial{tr(1, 1, 1), tr(1, 1, 1)}, ScreenFail},
		{"hidden, one trial of the ideal record", hidden, []*trial{tr(0, 0, 1), tr(0, 0, 1)}, []*trial{tr(1, 1, 1)}, ScreenTooFew},
		{"hidden, one trial of memory off", hidden, []*trial{tr(0, 0, 1)}, []*trial{tr(1, 1, 1), tr(1, 1, 1)}, ScreenTooFew},
		{"handoff, at 0.7", handoff, []*trial{tr(1, 1, 10), tr(0, 0, 10)}, []*trial{tr(1, 1, 7), tr(1, 1, 7)}, ScreenPass},
		{"handoff, past 0.7", handoff, []*trial{tr(1, 1, 10), tr(0, 0, 10)}, []*trial{tr(1, 1, 7), tr(1, 1, 8)}, ScreenFail},
		{"handoff, memory off never solved", handoff, []*trial{tr(0, 0, 10), tr(0, 0, 10)}, []*trial{tr(1, 1, 1), tr(1, 1, 1)}, ScreenFail},
		{"handoff, too few", handoff, []*trial{tr(1, 1, 10), tr(1, 1, 10)}, []*trial{tr(1, 1, 1)}, ScreenTooFew},
		{"control", task.Task{ID: "z", Family: task.FamilyControl}, nil, nil, ScreenNone},
	}
	for _, c := range cases {
		if got := screenOf(c.t, c.off, c.oracle); got.Verdict != c.want {
			t.Errorf("%s: %s, want %s", c.name, got.Verdict, c.want)
		}
	}
}

// Medians of odd and even counts, least and most; the statistics of the
// pairs: none with no pair, a mean and no s with one, s over the pairs
// less one, and the count of pairs it needs.
func TestStatistics(t *testing.T) {
	if s := statOf([]float64{7, 1, 3}); s != (Stat{3, 3, 1, 7}) {
		t.Errorf("odd: %+v", s)
	}
	if s := statOf([]float64{8, 1, 3, 4}); s != (Stat{4, 3.5, 1, 8}) {
		t.Errorf("even: %+v", s)
	}
	if s := statOf(nil); s != (Stat{}) {
		t.Errorf("none: %+v", s)
	}
	if p := pairsOf(nil); p != (Pairs{}) {
		t.Errorf("no pair: %+v", p)
	}
	if p := pairsOf([]float64{-0.25}); p != (Pairs{N: 1, Mean: -0.25}) {
		t.Errorf("one pair: %+v", p)
	}
	// 1, 2, 3, 6: mean 3, squares 4+1+0+9 = 14 over 3, s = sqrt(14/3).
	p := pairsOf([]float64{1, 2, 3, 6})
	s := math.Sqrt(14.0 / 3)
	if p.N != 4 || p.Mean != 3 || !near(p.S, s) || p.Need != int(math.Ceil(math.Pow(3*s/math.Log(4.0/3), 2))) || p.Need != 508 {
		t.Errorf("four pairs: %+v", p)
	}
}

// The text: ids and figures in fixed words, by host, task, then
// condition, with no word a task forbids and no path.
func TestText(t *testing.T) {
	text, err := build(t).Text()
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range []string{
		"host claude-code\n",
		"  runs, condition off: records 2, ended done 2, error 0, no_usage 0, memory_off 0, hidden_found 0; planned 18, judged 11, not run 2, unjudged sign_in 3, rate_limited 0, timeout 1, other_model 0, setup_failed 0, update_failed 0, host_failed 1, no_usage 0, memory_off 0, hidden_found 0; no model 0, tokens 45000\n",
		"  runs, condition off: commits " + strings.Repeat("1", 40) + " clean, " + strings.Repeat("2", 40) + " dirty; host versions 1.2.3, 1.2.4; models asked made-up-model-1; models answered made-up-model-1\n",
		"  runs, condition oracle: commits " + strings.Repeat("1", 40) + " clean; host versions 1.2.3; models asked none; models answered none, made-up-model-2\n",
		"  task hid1 hidden, condition off: judged 3, solved 2, solved first try 1; corrections median 2, least 0, most 5; rounds median 3, least 1, most 5; agent events median 10, least 8, most 20; prompt tokens median 1000, least 500, most 3000; cached tokens median 800, least 400, most 2000; output tokens median 100, least 50, most 300; minutes median 2.00, least 1.00, most 5.00\n",
		"  task hid1 hidden, condition eva: judged 2, solved 2, solved first try 1; corrections median 0.5, least 0, most 1; rounds median 1.5, least 1, most 2; agent events median 6, least 0, most 12; prompt tokens median 1150, least 800, most 1500; cached tokens median 800, least 600, most 1000; output tokens median 115, least 80, most 150; minutes median 2.50, least 1.67, most 3.33; memory tokens median 6000, least 5000, most 7000; memory minutes median 0.67, least 0.50, most 0.83\n",
		"  task hid2 hidden, condition eva: judged 0, solved 0, solved first try 0; corrections none; rounds none; agent events none; prompt tokens none; cached tokens none; output tokens none; minutes none; memory tokens none; memory minutes none\n",
		"  cost, condition off: judged 11; medians corrections 1, rounds 2, agent events 10, prompt tokens 1000, cached tokens 800, output tokens 100, minutes 2.00; price median 0.003150, at 3 uncached prompt, 0.3 cached and 15 output a million tokens\n",
		"  cost, condition eva: judged 2; medians corrections 0.5, rounds 1.5, agent events 6, prompt tokens 1150, cached tokens 800, output tokens 115, minutes 2.50; price median 0.003015, at 3 uncached prompt, 0.3 cached and 15 output a million tokens; memory apart: memory tokens 6000, memory minutes 0.67, memory price median 0.018000, a floor, every memory token at the uncached prompt's rate\n",
		"  variance, eva against off: agent events pairs 1, mean -0.5108, no s: under two pairs; prompt tokens pairs 2, mean -0.4581, s 0.3323, n 13\n",
		"  variance, oracle against off: agent events pairs 11, mean -0.4688, s 0.5335, n 31; prompt tokens pairs 11, mean -0.6018, s 0.6732, n 50\n",
		"  screen, task hand1 handoff: pass\n",
		"  screen, task hand2 handoff: too few\n",
		"  screen, task ctl1 control: none\n",
		"  screens: passed 2, failed 2, too few 1, none 1\n",
	} {
		if !strings.Contains(text, line) {
			t.Errorf("the text lacks\n%s", line)
		}
	}
	if !strings.HasPrefix(text, "host claude-code\n") || strings.Index(text, "host codex\n") < strings.Index(text, "host claude-code\n") {
		t.Errorf("hosts out of order:\n%s", text)
	}
	assertClean(t, text)
}

// assertClean fails when text holds a path or a word a test task forbids.
func assertClean(t *testing.T, text string) {
	t.Helper()
	wd, _ := os.Getwd()
	if strings.Contains(text, "/") || strings.Contains(text, "testdata") || strings.Contains(text, wd) {
		t.Errorf("a path:\n%s", text)
	}
	for _, tk := range testTasks() {
		for _, w := range task.Forbid(tk) {
			if strings.Contains(strings.ToLower(text), w) {
				t.Errorf("the word %s:\n%s", w, text)
			}
		}
	}
}

// A folder of records and no rows is read; an unjudged trial moves no
// figure: without folder c, the eva figures are the same, and only the
// runs' counts change.
func TestRecordsAlone(t *testing.T) {
	c := readTestdata(t, "c")
	if len(c[0].Rows) != 0 || len(c[0].Records) != 1 {
		t.Fatalf("%+v", c)
	}
	with := hostOf(t, build(t), "codex")
	r, err := Build(testTasks(), readTestdata(t, "a", "b"), testRates)
	if err != nil {
		t.Fatal(err)
	}
	without := hostOf(t, r, "codex")
	if !reflect.DeepEqual(with.Tasks, without.Tasks) || !reflect.DeepEqual(with.Costs, without.Costs) ||
		!reflect.DeepEqual(with.Variance, without.Variance) || !reflect.DeepEqual(with.Screens, without.Screens) {
		t.Fatal("an unjudged trial moved a figure")
	}
	if with.Runs[1].Unjudged[drive.CodeNoUsage] != 1 || without.Runs[1].Unjudged[drive.CodeNoUsage] != 0 || with.Runs[1].Planned != without.Runs[1].Planned+1 {
		t.Fatalf("eva's runs: %+v and %+v", with.Runs[1], without.Runs[1])
	}
}

// mutate returns copies of the testdata's folders with f applied to them.
func mutate(t *testing.T, f func(fs []Folder)) []Folder {
	t.Helper()
	fs := readTestdata(t, "a", "b", "c")
	for i := range fs {
		fs[i].Rows = slices.Clone(fs[i].Rows)
		fs[i].Records = slices.Clone(fs[i].Records)
	}
	f(fs)
	return fs
}

// rowIndex returns the index in rows of the row of metric and scenario.
func rowIndex(t *testing.T, rows []results.Row, metric, scenario string) int {
	t.Helper()
	i := slices.IndexFunc(rows, func(r results.Row) bool { return r.Metric == metric && r.Scenario == scenario })
	if i < 0 {
		t.Fatalf("no row %s %s", metric, scenario)
	}
	return i
}

// Each refusal of the card's item 3, on the testdata: each is refused with
// its own message, which holds no word a task forbids.
func TestRefusals(t *testing.T) {
	cases := []struct {
		name, want string
		f          func(fs []Folder)
	}{
		{"a row of another instrument", "row 1: its instrument is not continuation", func(fs []Folder) { fs[0].Rows[0].Instrument = "fidelity" }},
		{"a row with no host", "row 1: it names no host", func(fs []Folder) { fs[0].Rows[0].Host, fs[0].Rows[0].HostVersion = "", "" }},
		{"a metric of another family", "its metric's family is not task hid1's", func(fs []Folder) {
			fs[0].Rows[0].Metric = "continuation.handoff.off.solved"
		}},
		{"a metric of no condition", "its metric is not one of the instrument's", func(fs []Folder) {
			fs[0].Rows[0].Metric = "continuation.hidden.host.solved"
		}},
		{"a scenario whose number is not its seed", "row 1: its scenario is not <task>.t<n> with n its seed", func(fs []Folder) { fs[0].Rows[0].Seed = 2 }},
		{"a scenario with a leading 0", "its scenario is not", func(fs []Folder) { fs[0].Rows[0].Scenario = "hid1.t01" }},
		{"a scenario of no trial number", "its scenario is not", func(fs []Folder) { fs[0].Rows[0].Scenario = "hid1" }},
		{"a seed of 0", "its scenario is not", func(fs []Folder) { fs[0].Rows[0].Scenario, fs[0].Rows[0].Seed = "hid1.t0", 0 }},
		{"a row of a task not in --tasks", "task other1 is not in --tasks", func(fs []Folder) { fs[0].Rows[0].Scenario = "other1.t1" }},
		{"a record of a task not in --tasks", "record 1: task other1 is not in --tasks", func(fs []Folder) {
			fs[0].Records[0].Tasks = []string{"hid1", "other1"}
		}},
		{"a record of another instrument", "record 1: its instrument is not continuation", func(fs []Folder) { fs[0].Records[0].Instrument = "fidelity" }},
		{"a record of no condition", "record 1: its condition is not one the instrument knows", func(fs []Folder) { fs[0].Records[0].Condition = "host" }},
		{"a row twice in a folder", "host codex, metric continuation.hidden.off.solved, scenario hid1.t1 is given twice", func(fs []Folder) {
			fs[0].Rows = append(fs[0].Rows, fs[0].Rows[0])
		}},
		{"a row twice across two folders", "is given twice", func(fs []Folder) {
			fs[1].Rows = append(fs[1].Rows, fs[0].Rows[0])
		}},
		{"a trial's rows in two folders", "trial hid1.t1 of host codex, condition off, has rows in two folders", func(fs []Folder) {
			i := rowIndex(t, fs[0].Rows, "continuation.hidden.off.wall_ms", "hid1.t1")
			fs[1].Rows = append(fs[1].Rows, fs[0].Rows[i])
			fs[0].Rows = slices.Delete(fs[0].Rows, i, i+1)
			fs[0].Records[0].Rows--
		}},
		{"a run given twice", "run 20261114t140000z of host codex, condition eva, is given twice", func(fs []Folder) {
			fs[2].Records = append(fs[2].Records, fs[2].Records[0])
		}},
		{"a trial lacking wall_ms", "host codex, condition off, trial hid1.t1: it lacks wall_ms", func(fs []Folder) {
			i := rowIndex(t, fs[0].Rows, "continuation.hidden.off.wall_ms", "hid1.t1")
			fs[0].Rows = slices.Delete(fs[0].Rows, i, i+1)
			fs[0].Records[0].Rows--
		}},
		{"a trial lacking solved", "trial hid1.t1: it lacks solved", func(fs []Folder) {
			i := rowIndex(t, fs[0].Rows, "continuation.hidden.off.solved", "hid1.t1")
			fs[0].Rows = slices.Delete(fs[0].Rows, i, i+1)
			fs[0].Records[0].Rows--
		}},
		{"an eva trial lacking memory_ms", "condition eva, trial hid1.t2: it lacks memory_ms", func(fs []Folder) {
			i := rowIndex(t, fs[1].Rows, "continuation.hidden.eva.memory_ms", "hid1.t2")
			fs[1].Rows = slices.Delete(fs[1].Rows, i, i+1)
			fs[1].Records[0].Rows--
		}},
		{"an eva trial lacking memory_tokens", "condition eva, trial hid1.t1: it lacks memory_tokens", func(fs []Folder) {
			i := rowIndex(t, fs[1].Rows, "continuation.hidden.eva.memory_tokens", "hid1.t1")
			fs[1].Rows = slices.Delete(fs[1].Rows, i, i+1)
			fs[1].Records[0].Rows--
		}},
		{"a folder with no record", "folder 3: it holds no run record", func(fs []Folder) { fs[2].Records = nil }},
		{"rows the records do not count", "folder 1, host codex, condition off: 99 rows and 11 judged trials, where its records count 100 and 11", func(fs []Folder) {
			fs[0].Records[1].Rows++
		}},
		{"judged trials the records do not count", "folder 1, host codex, condition oracle: 108 rows and 12 judged trials, where its records count 108 and 11", func(fs []Folder) {
			fs[0].Records[2].Judged--
		}},
		{"a killed run: rows no record covers", "folder 1, host codex, condition oracle: 108 rows and 12 judged trials, where its records count 0 and 0", func(fs []Folder) {
			fs[0].Records = fs[0].Records[:2]
		}},
		{"a record of a host with no rows", "folder 3, host claude-code, condition eva: 0 rows and 0 judged trials, where its records count 9 and 1", func(fs []Folder) {
			r := fs[1].Records[1]
			r.Condition, r.Run = "eva", "20261114t150000z"
			fs[2].Records = append(fs[2].Records, r)
		}},
		{"a ratio not 0 or 1", "row 1: its value is not a figure its measure can take", func(fs []Folder) { fs[0].Rows[0].Value = 0.5 }},
		{"a count below 0", "its value is not a figure", func(fs []Folder) {
			fs[0].Rows[rowIndex(t, fs[0].Rows, "continuation.hidden.off.corrections", "hid1.t1")].Value = -1
		}},
		{"a row with a forbidden word", "folder 2: a row holds a word a task forbids", func(fs []Folder) { fs[1].Rows[3].Model = "zanzibrel-7" }},
		{"a record with a forbidden word", "folder 1, record 2: it holds a word a task forbids", func(fs []Folder) { fs[0].Records[1].Model = "made-up-quokkaline" }},
		{"a host version with a forbidden word", "record 1: it holds a word a task forbids", func(fs []Folder) {
			fs[0].Records[0].HostVersions = []string{"Marrowfen.1"}
		}},
	}
	for _, c := range cases {
		_, err := Build(testTasks(), mutate(t, c.f), testRates)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: %v", c.name, err)
			continue
		}
		assertClean(t, err.Error())
	}
	// The unchanged folders build, so each refusal above is its change's;
	// and so do two runs that share an id and a host and a condition but
	// are not the same, as two started in one second may.
	if _, err := Build(testTasks(), mutate(t, func([]Folder) {}), testRates); err != nil {
		t.Fatal(err)
	}
	if _, err := Build(testTasks(), mutate(t, func(fs []Folder) { fs[2].Records[0].Run = fs[1].Records[0].Run }), testRates); err != nil {
		t.Fatal(err)
	}
}

// The text refuses a fixed word that a task forbids, and a task's id that
// holds another's word.
func TestTextHoldsNoForbiddenWord(t *testing.T) {
	// One word, and two that the text has apart.
	for _, w := range []string{"Variance", "Price Median"} {
		tasks := testTasks()
		tasks[5].Facts[0].Values = []string{w}
		r, err := Build(tasks, readTestdata(t, "a", "b", "c"), testRates)
		if err != nil {
			t.Fatal(err)
		}
		if text, err := r.Text(); err == nil || text != "" || err.Error() != "the report would hold a word a task forbids" {
			t.Fatalf("%s: %v\n%s", w, err, text)
		}
	}
	tasks := testTasks()
	tasks[0].Facts[0].Values = []string{"hand2x"}
	tasks[4].ID = "hand2xy"
	if _, err := Build(tasks, nil, nil); err == nil || !strings.Contains(err.Error(), "a task's id holds a word another task forbids") {
		t.Fatalf("%v", err)
	}
}

// ReadFolder refuses a folder with no record, and a rows or runs file over
// 64 MiB before reading it; a folder with no rows file has no rows.
func TestReadFolder(t *testing.T) {
	cat := task.Catalogue()
	dir := t.TempDir()
	if _, err := ReadFolder(dir, cat); err == nil || err.Error() != "it holds no run record" {
		t.Fatalf("empty: %v", err)
	}
	if _, err := ReadFolder(filepath.Join(dir, "none"), cat); err == nil || err.Error() != "it is not a folder" {
		t.Fatalf("missing: %v", err)
	}
	copyFile(t, filepath.Join("testdata", "c", drive.RunsFile), filepath.Join(dir, drive.RunsFile))
	if f, err := ReadFolder(dir, cat); err != nil || f.Rows != nil || len(f.Records) != 1 {
		t.Fatalf("records alone: %+v %v", f, err)
	}
	for _, name := range []string{results.FileName, drive.RunsFile} {
		big := t.TempDir()
		copyFile(t, filepath.Join("testdata", "c", drive.RunsFile), filepath.Join(big, drive.RunsFile))
		sparse(t, filepath.Join(big, name), MaxFile+1)
		_, err := ReadFolder(big, cat)
		if err == nil || !strings.HasSuffix(err.Error(), "is over 64 MiB") {
			t.Errorf("%s over 64 MiB: %v", name, err)
		}
		// At 64 MiB exactly it is read, and refused for what it holds.
		sparse(t, filepath.Join(big, name), MaxFile)
		if _, err := ReadFolder(big, cat); err == nil || strings.Contains(err.Error(), "64 MiB") {
			t.Errorf("%s at 64 MiB: %v", name, err)
		}
	}
	// A rows file the loader refuses, or the catalogue lacks, is refused
	// without the loader's words.
	bad := t.TempDir()
	copyFile(t, filepath.Join("testdata", "c", drive.RunsFile), filepath.Join(bad, drive.RunsFile))
	if err := os.WriteFile(filepath.Join(bad, results.FileName), []byte("{\"v\":1}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadFolder(bad, cat); err == nil || err.Error() != "its rows cannot be read" {
		t.Fatalf("bad rows: %v", err)
	}
	copyFile(t, filepath.Join("testdata", "a", results.FileName), filepath.Join(bad, results.FileName))
	if _, err := ReadFolder(bad, results.Catalogue{V: 1, Metrics: []results.Metric{{Name: "other", Unit: "count", Direction: "lower", Paths: []string{"x"}}}}); err == nil ||
		err.Error() != "its rows name a metric the catalogue lacks, or in another unit" {
		t.Fatalf("catalogue: %v", err)
	}
}

func copyFile(t *testing.T, from, to string) {
	t.Helper()
	data, err := os.ReadFile(from)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(to, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

// sparse makes the file at path size bytes long, of zeros, taking no disk.
func sparse(t *testing.T, path string, size int64) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(0); err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(size); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestParseRates(t *testing.T) {
	if r, err := ParseRates("3,0.3,15"); err != nil || r != (Rates{3, 0.3, 15}) {
		t.Fatalf("%+v %v", r, err)
	}
	for _, s := range []string{"", "3,0.3", "3,0.3,15,1", "3,-1,15", "3,x,15", "3,NaN,15", "3,Inf,15"} {
		if _, err := ParseRates(s); err == nil {
			t.Errorf("%q taken", s)
		}
	}
}
