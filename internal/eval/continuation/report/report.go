// Package report is the continuation instrument's report: from the rows a
// set of results folders holds and the record each run appended beside
// them, it gives, by host, task and condition, what the runs did, what the
// judged trials measured, what a session cost, how much the figures vary
// between conditions and which tasks pass their screens. It is the pilot's
// report (EV-03) and EV-17's own screens. It reads ids and figures only,
// refuses a set it cannot count each trial of exactly once, and prints
// ids and figures in fixed words. No release contains it.
package report

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"unicode"

	"github.com/heyNag/eva-md/internal/eval/continuation/drive"
	"github.com/heyNag/eva-md/internal/eval/continuation/task"
	"github.com/heyNag/eva-md/internal/eval/results"
)

// MaxFile is the largest file the report reads: its loaders read a file
// whole, so a larger one is refused before it is opened.
const MaxFile = 64 << 20

// Folder is one results folder, read: its rows, none when it has no rows
// file, and its runs' records.
type Folder struct {
	Rows    []results.Row
	Records []drive.Record
}

// SizeOK returns an error when the file at path is larger than MaxFile,
// and nil when it is no larger or does not exist. It does not open it.
func SizeOK(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return nil
	}
	if info.Size() > MaxFile {
		return errors.New("is over 64 MiB")
	}
	return nil
}

// ReadFolder reads the results folder dir: its rows, held to the catalogue
// cat, and its records. Its errors are in fixed words and name no path:
// the loaders' own, which can, are not passed on.
func ReadFolder(dir string, cat results.Catalogue) (Folder, error) {
	info, err := os.Stat(dir)
	if err != nil || !info.IsDir() {
		return Folder{}, errors.New("it is not a folder")
	}
	var f Folder
	rowsFile := filepath.Join(dir, results.FileName)
	if err := SizeOK(rowsFile); err != nil {
		return Folder{}, errors.New("its rows file " + err.Error())
	}
	if _, err := os.Lstat(rowsFile); !errors.Is(err, fs.ErrNotExist) {
		if f.Rows, err = results.Load(rowsFile); err != nil {
			return Folder{}, errors.New("its rows cannot be read")
		}
		if err := cat.Check(f.Rows); err != nil {
			return Folder{}, errors.New("its rows name a metric the catalogue lacks, or in another unit")
		}
	}
	if err := SizeOK(filepath.Join(dir, drive.RunsFile)); err != nil {
		return Folder{}, errors.New("its runs file " + err.Error())
	}
	f.Records, err = drive.LoadRuns(dir)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return Folder{}, errors.New("it holds no run record")
	case err != nil:
		return Folder{}, errors.New("its run records cannot be read")
	}
	if len(f.Records) == 0 {
		return Folder{}, errors.New("it holds no run record")
	}
	return f, nil
}

// Rates are money per million tokens: uncached prompt, cached and output.
type Rates struct {
	In, Cached, Out float64
}

// ParseRates reads "in,cached,out", three numbers at or above 0.
func ParseRates(s string) (Rates, error) {
	parts := strings.Split(s, ",")
	if len(parts) != 3 {
		return Rates{}, errors.New("--rates is three numbers, in,cached,out")
	}
	var v [3]float64
	for i, p := range parts {
		f, err := strconv.ParseFloat(strings.TrimSpace(p), 64)
		if err != nil || math.IsNaN(f) || math.IsInf(f, 0) || f < 0 {
			return Rates{}, errors.New("--rates is three numbers at or above 0, in,cached,out")
		}
		v[i] = f
	}
	return Rates{In: v[0], Cached: v[1], Out: v[2]}, nil
}

// Price is the money a trial's tokens cost at the rates r.
func (r Rates) Price(prompt, cached, output float64) float64 {
	return ((prompt-cached)*r.In + cached*r.Cached + output*r.Out) / 1e6
}

// The measures a judged trial must give under every condition, and under
// eva besides.
var (
	required     = []string{"solved", "first_try", "corrections", "rounds", "agent_events", "prompt_tokens", "cached_tokens", "output_tokens", "wall_ms"}
	evaRequired  = []string{"memory_tokens", "memory_ms"}
	notRead      = "asked_again"
	ratioMeasure = map[string]bool{"solved": true, "first_try": true}
)

// Stat is the median, least and most of a figure over some trials; N is
// how many.
type Stat struct {
	N                   int
	Median, Least, Most float64
}

// statOf returns the Stat of vs. An even count's median is the mean of
// the middle two.
func statOf(vs []float64) Stat {
	if len(vs) == 0 {
		return Stat{}
	}
	s := slices.Clone(vs)
	slices.Sort(s)
	n := len(s)
	m := s[n/2]
	if n%2 == 0 {
		m = (s[n/2-1] + s[n/2]) / 2
	}
	return Stat{N: n, Median: m, Least: s[0], Most: s[n-1]}
}

// Commit is a commit the records name, and whether its tree was dirty.
type Commit struct {
	ID    string
	Dirty bool
}

// Runs is what a host's records under one condition say.
type Runs struct {
	Condition task.Condition
	Records   int
	// Ended counts the records by how their run ended.
	Ended                            map[string]int
	Planned, Judged, NotRun, NoModel int
	Unjudged                         map[string]int // by every code of drive.Codes
	Tokens                           int64
	Commits                          []Commit
	HostVersions                     []string
	// ModelsAsked and ModelsAnswered are each once, sorted; "" is none.
	ModelsAsked, ModelsAnswered []string
}

// TaskFigures is what a task's judged trials gave on a host under one
// condition.
type TaskFigures struct {
	Task, Family             string
	Condition                task.Condition
	Judged, Solved, FirstTry int
	// Stats are by measure: those of figures, and under eva memoryFigures'.
	Stats map[string]Stat
}

// Cost is what a session cost on a host under one condition: medians over
// every judged trial.
type Cost struct {
	Condition task.Condition
	Judged    int
	Medians   map[string]float64 // by measure, as TaskFigures' Stats
	// Price and MemoryPrice are medians of each trial's price, given rates;
	// MemoryPrice under eva only, memory_tokens at the uncached rate.
	Price, MemoryPrice float64
}

// Pairs are the log-ratios of one figure between an arm and memory off.
type Pairs struct {
	N    int
	Mean float64 // when N is 1 or more
	S    float64 // the standard deviation, over N less one, when N is 2 or more
	Need int     // ceil((3·S / ln(4/3))²), when N is 2 or more
}

// Variance is an arm against memory off on one host.
type Variance struct {
	Arm                 task.Condition
	AgentEvents, Prompt Pairs
}

// Screen verdicts.
const (
	ScreenPass   = "pass"
	ScreenFail   = "fail"
	ScreenTooFew = "too few"
	ScreenNone   = "none"
)

// Screen is a task's screen on a host.
type Screen struct {
	Task, Family, Verdict string
}

// Host is the report of one host.
type Host struct {
	ID         string
	Conditions []task.Condition // those its records name, in task.Conditions' order
	Runs       []Runs
	Tasks      []TaskFigures // by task, then condition
	Costs      []Cost
	Variance   []Variance
	Screens    []Screen
	Passed     int
}

// Report is the whole report.
type Report struct {
	Hosts []Host
	Rates *Rates
	// forbid are the tasks' forbidden words, by their letters and digits.
	forbid []string
}

// trialKey names one trial.
type trialKey struct {
	host string
	cond task.Condition
	task string
	n    uint64
}

// trial is one judged trial's figures, by measure.
type trial struct {
	folder int
	values map[string]float64
	models []string // the models its rows name, each once; "" is none
}

// groupKey names a host's condition in one folder.
type groupKey struct {
	folder int
	host   string
	cond   task.Condition
}

// Build checks the folders against the tasks and against each other, and
// gives the report. Its errors name folders by their place, from 1, and
// otherwise ids only, never a path or a word the tasks forbid.
func Build(tasks []task.Task, folders []Folder, rates *Rates) (Report, error) {
	byID := map[string]task.Task{}
	var words []string
	for _, t := range tasks {
		byID[t.ID] = t
		words = append(words, task.Forbid(t)...)
	}
	keys := forbidKeys(words)
	for _, t := range tasks {
		if holds(t.ID, keys) {
			return Report{}, errors.New("a task's id holds a word another task forbids")
		}
	}
	r := Report{Rates: rates, forbid: keys}

	trials := map[trialKey]*trial{}
	seen := map[[3]string]bool{} // host, metric, scenario
	runs := map[string]bool{}    // records, as JSON
	rowCount := map[groupKey]int{}
	records := map[string][]drive.Record{} // by host
	conds := map[string]map[task.Condition]bool{}
	type sums struct{ rows, judged int }
	recorded := map[groupKey]*sums{}

	for fi, f := range folders {
		where := fmt.Sprintf("folder %d", fi+1)
		if len(f.Records) == 0 {
			return Report{}, errors.New(where + ": it holds no run record")
		}
		if err := results.Forbidden(f.Rows, words); err != nil {
			return Report{}, errors.New(where + ": a row holds a word a task forbids")
		}
		for i, rec := range f.Records {
			at := fmt.Sprintf("%s, record %d", where, i+1)
			if slices.ContainsFunc(recordStrings(rec), func(s string) bool { return holds(s, keys) }) {
				return Report{}, errors.New(at + ": it holds a word a task forbids")
			}
			if rec.Instrument != task.Instrument {
				return Report{}, errors.New(at + ": its instrument is not " + task.Instrument)
			}
			c, err := task.ParseCondition(rec.Condition)
			if err != nil {
				return Report{}, errors.New(at + ": its condition is not one the instrument knows")
			}
			for _, id := range rec.Tasks {
				if _, ok := byID[id]; !ok {
					return Report{}, fmt.Errorf("%s: task %s is not in --tasks", at, id)
				}
			}
			// A record is the same run's when it is the same in every
			// field: two runs started in one second, each in a --work of
			// its own, may share an id.
			k, err := json.Marshal(rec)
			if err != nil {
				return Report{}, errors.New(at + ": it cannot be compared")
			}
			if runs[string(k)] {
				return Report{}, fmt.Errorf("%s: run %s of host %s, condition %s, is given twice", at, rec.Run, rec.Host, rec.Condition)
			}
			runs[string(k)] = true
			records[rec.Host] = append(records[rec.Host], rec)
			if conds[rec.Host] == nil {
				conds[rec.Host] = map[task.Condition]bool{}
			}
			conds[rec.Host][c] = true
			g := groupKey{fi, rec.Host, c}
			if recorded[g] == nil {
				recorded[g] = &sums{}
			}
			recorded[g].rows += rec.Rows
			recorded[g].judged += rec.Judged
		}
		judged := map[groupKey]map[trialKey]bool{}
		for i, row := range f.Rows {
			at := fmt.Sprintf("%s, row %d", where, i+1)
			if row.Instrument != task.Instrument {
				return Report{}, errors.New(at + ": its instrument is not " + task.Instrument)
			}
			if row.Host == "" {
				return Report{}, errors.New(at + ": it names no host")
			}
			family, c, measure, ok := parseMetric(row.Metric)
			if !ok {
				return Report{}, errors.New(at + ": its metric is not one of the instrument's")
			}
			id, ok := scenarioTask(row.Scenario, row.Seed)
			if !ok {
				return Report{}, errors.New(at + ": its scenario is not <task>.t<n> with n its seed")
			}
			t, ok := byID[id]
			if !ok {
				return Report{}, fmt.Errorf("%s: task %s is not in --tasks", at, id)
			}
			if t.Family != family {
				return Report{}, fmt.Errorf("%s: its metric's family is not task %s's", at, id)
			}
			if !valueOK(measure, row.Value) {
				return Report{}, errors.New(at + ": its value is not a figure its measure can take")
			}
			dk := [3]string{row.Host, row.Metric, row.Scenario}
			if seen[dk] {
				return Report{}, fmt.Errorf("%s: host %s, metric %s, scenario %s is given twice", at, row.Host, row.Metric, row.Scenario)
			}
			seen[dk] = true
			tk := trialKey{row.Host, c, id, row.Seed}
			tr := trials[tk]
			switch {
			case tr == nil:
				tr = &trial{folder: fi, values: map[string]float64{}}
				trials[tk] = tr
			case tr.folder != fi:
				return Report{}, fmt.Errorf("%s: trial %s of host %s, condition %s, has rows in two folders", at, row.Scenario, row.Host, c)
			}
			if measure != notRead {
				tr.values[measure] = row.Value
			}
			if !slices.Contains(tr.models, row.Model) {
				tr.models = append(tr.models, row.Model)
			}
			g := groupKey{fi, row.Host, c}
			rowCount[g]++
			if judged[g] == nil {
				judged[g] = map[trialKey]bool{}
			}
			judged[g][tk] = true
		}
		// Every group of the folder, from its rows or its records, counts
		// as its records say.
		groups := map[groupKey]bool{}
		for g := range rowCount {
			if g.folder == fi {
				groups[g] = true
			}
		}
		for g := range recorded {
			if g.folder == fi {
				groups[g] = true
			}
		}
		for _, g := range sortedGroups(groups) {
			s := recorded[g]
			if s == nil {
				s = &sums{}
			}
			if rowCount[g] != s.rows || len(judged[g]) != s.judged {
				return Report{}, fmt.Errorf("%s, host %s, condition %s: %d rows and %d judged trials, where its records count %d and %d",
					where, g.host, g.cond, rowCount[g], len(judged[g]), s.rows, s.judged)
			}
		}
	}
	for _, k := range sortedTrials(trials) {
		need := required
		if k.cond == task.Eva {
			need = append(slices.Clone(required), evaRequired...)
		}
		for _, m := range need {
			if _, ok := trials[k].values[m]; !ok {
				return Report{}, fmt.Errorf("host %s, condition %s, trial %s.t%d: it lacks %s", k.host, k.cond, k.task, k.n, m)
			}
		}
	}

	hosts := make([]string, 0, len(records))
	for h := range records {
		hosts = append(hosts, h)
	}
	slices.Sort(hosts)
	for _, h := range hosts {
		r.Hosts = append(r.Hosts, buildHost(h, tasks, conds[h], records[h], trials, rates))
	}
	return r, nil
}

// buildHost gives the report of host h.
func buildHost(h string, tasks []task.Task, present map[task.Condition]bool, recs []drive.Record, trials map[trialKey]*trial, rates *Rates) Host {
	host := Host{ID: h}
	for _, c := range task.Conditions {
		if present[c] {
			host.Conditions = append(host.Conditions, c)
		}
	}
	of := func(c task.Condition, t task.Task) []*trial {
		var out []*trial
		for _, k := range sortedTrials(trials) {
			if k.host == h && k.cond == c && k.task == t.ID {
				out = append(out, trials[k])
			}
		}
		return out
	}
	for _, c := range host.Conditions {
		r := runsOf(c, recs)
		for _, t := range tasks {
			for _, tr := range of(c, t) {
				for _, m := range tr.models {
					if !slices.Contains(r.ModelsAnswered, m) {
						r.ModelsAnswered = append(r.ModelsAnswered, m)
					}
				}
			}
		}
		slices.Sort(r.ModelsAnswered)
		host.Runs = append(host.Runs, r)
	}
	for _, t := range tasks {
		for _, c := range host.Conditions {
			host.Tasks = append(host.Tasks, figuresOf(t, c, of(c, t)))
		}
	}
	for _, c := range host.Conditions {
		var all []*trial
		for _, t := range tasks {
			all = append(all, of(c, t)...)
		}
		host.Costs = append(host.Costs, costOf(c, all, rates))
	}
	if present[task.Off] {
		for _, arm := range []task.Condition{task.Eva, task.Oracle} {
			if present[arm] {
				host.Variance = append(host.Variance, varianceOf(h, arm, trials))
			}
		}
	}
	for _, t := range tasks {
		s := screenOf(t, of(task.Off, t), of(task.Oracle, t))
		if s.Verdict == ScreenPass {
			host.Passed++
		}
		host.Screens = append(host.Screens, s)
	}
	return host
}

// runsOf sums the records of condition c.
func runsOf(c task.Condition, recs []drive.Record) Runs {
	r := Runs{Condition: c, Ended: map[string]int{}, Unjudged: map[string]int{}}
	for _, rec := range recs {
		if rec.Condition != string(c) {
			continue
		}
		r.Records++
		r.Ended[rec.Ended]++
		r.Planned += rec.Planned
		r.Judged += rec.Judged
		r.NotRun += rec.NotRun
		r.NoModel += rec.NoModel
		r.Tokens += rec.Tokens
		for _, code := range drive.Codes {
			r.Unjudged[code] += rec.Unjudged[code]
		}
		if cm := (Commit{rec.Commit, rec.Dirty}); !slices.Contains(r.Commits, cm) {
			r.Commits = append(r.Commits, cm)
		}
		for _, v := range rec.HostVersions {
			if !slices.Contains(r.HostVersions, v) {
				r.HostVersions = append(r.HostVersions, v)
			}
		}
		if !slices.Contains(r.ModelsAsked, rec.Model) {
			r.ModelsAsked = append(r.ModelsAsked, rec.Model)
		}
	}
	slices.SortFunc(r.Commits, func(a, b Commit) int {
		if a.ID != b.ID {
			return strings.Compare(a.ID, b.ID)
		}
		if a.Dirty == b.Dirty {
			return 0
		}
		if a.Dirty {
			return 1
		}
		return -1
	})
	slices.Sort(r.HostVersions)
	slices.Sort(r.ModelsAsked)
	return r
}

// figures are the measures item 5 of the card reads under every condition,
// and memoryFigures those it reads under eva besides.
var (
	figures       = []string{"corrections", "rounds", "agent_events", "prompt_tokens", "cached_tokens", "output_tokens", "wall_ms"}
	memoryFigures = []string{"memory_tokens", "memory_ms"}
)

func measuresOf(c task.Condition) []string {
	if c == task.Eva {
		return append(slices.Clone(figures), memoryFigures...)
	}
	return figures
}

func figuresOf(t task.Task, c task.Condition, ts []*trial) TaskFigures {
	f := TaskFigures{Task: t.ID, Family: t.Family, Condition: c, Judged: len(ts), Stats: map[string]Stat{}}
	for _, tr := range ts {
		f.Solved += int(tr.values["solved"])
		f.FirstTry += int(tr.values["first_try"])
	}
	for _, m := range measuresOf(c) {
		f.Stats[m] = statOf(valuesOf(ts, m))
	}
	return f
}

func valuesOf(ts []*trial, m string) []float64 {
	var vs []float64
	for _, tr := range ts {
		vs = append(vs, tr.values[m])
	}
	return vs
}

func costOf(c task.Condition, ts []*trial, rates *Rates) Cost {
	k := Cost{Condition: c, Judged: len(ts), Medians: map[string]float64{}}
	for _, m := range measuresOf(c) {
		k.Medians[m] = statOf(valuesOf(ts, m)).Median
	}
	if rates != nil {
		var prices, memory []float64
		for _, tr := range ts {
			v := tr.values
			prices = append(prices, rates.Price(v["prompt_tokens"], v["cached_tokens"], v["output_tokens"]))
			memory = append(memory, v["memory_tokens"]*rates.In/1e6)
		}
		k.Price = statOf(prices).Median
		if c == task.Eva {
			k.MemoryPrice = statOf(memory).Median
		}
	}
	return k
}

// varianceOf pairs each trial of arm on host h with the trial of memory
// off of its task and number, both judged.
func varianceOf(h string, arm task.Condition, trials map[trialKey]*trial) Variance {
	v := Variance{Arm: arm}
	var events, prompt []float64
	for _, k := range sortedTrials(trials) {
		if k.host != h || k.cond != arm {
			continue
		}
		off := trials[trialKey{h, task.Off, k.task, k.n}]
		if off == nil {
			continue
		}
		a := trials[k].values
		if a["agent_events"] > 0 && off.values["agent_events"] > 0 {
			events = append(events, math.Log(a["agent_events"]/off.values["agent_events"]))
		}
		if a["prompt_tokens"] > 0 && off.values["prompt_tokens"] > 0 {
			prompt = append(prompt, math.Log(a["prompt_tokens"]/off.values["prompt_tokens"]))
		}
	}
	v.AgentEvents, v.Prompt = pairsOf(events), pairsOf(prompt)
	return v
}

// detectable is ln(4/3): a 25% reduction, as a log-ratio.
var detectable = math.Log(4.0 / 3.0)

func pairsOf(xs []float64) Pairs {
	p := Pairs{N: len(xs)}
	if p.N == 0 {
		return p
	}
	sum := 0.0
	for _, x := range xs {
		sum += x
	}
	p.Mean = sum / float64(p.N)
	if p.N < 2 {
		return p
	}
	ss := 0.0
	for _, x := range xs {
		ss += (x - p.Mean) * (x - p.Mean)
	}
	p.S = math.Sqrt(ss / float64(p.N-1))
	p.Need = int(math.Ceil(math.Pow(3*p.S/detectable, 2)))
	return p
}

// screenOf screens task t from its judged trials under memory off and the
// ideal record (design 6.3, as the lead ruled on 2026-11-13).
func screenOf(t task.Task, off, oracle []*trial) Screen {
	s := Screen{Task: t.ID, Family: t.Family}
	switch {
	case t.Family == task.FamilyControl:
		s.Verdict = ScreenNone
		return s
	case len(off) < 2 || len(oracle) < 2:
		s.Verdict = ScreenTooFew
		return s
	}
	pass := false
	switch t.Family {
	case task.FamilyHidden:
		oracleFirst, offNot := 0, 0
		for _, tr := range oracle {
			if tr.values["first_try"] == 1 {
				oracleFirst++
			}
		}
		for _, tr := range off {
			if tr.values["first_try"] != 1 {
				offNot++
			}
		}
		pass = oracleFirst >= 2 && offNot >= 2
	case task.FamilyHandoff:
		solved := slices.ContainsFunc(off, func(tr *trial) bool { return tr.values["solved"] == 1 })
		// At most 0.7 of off's, compared in tenths so that no rounding of
		// 0.7 moves a task on the line.
		o := statOf(valuesOf(oracle, "agent_events")).Median
		f := statOf(valuesOf(off, "agent_events")).Median
		pass = solved && 10*o <= 7*f
	}
	s.Verdict = ScreenFail
	if pass {
		s.Verdict = ScreenPass
	}
	return s
}

// parseMetric reads continuation.<family>.<condition>.<measure>.
func parseMetric(name string) (family string, c task.Condition, measure string, ok bool) {
	parts := strings.Split(name, ".")
	if len(parts) != 4 || parts[0] != task.Instrument || !slices.Contains(task.Families, parts[1]) {
		return "", "", "", false
	}
	c, err := task.ParseCondition(parts[2])
	if err != nil || !slices.ContainsFunc(task.Measures, func(m task.Measure) bool { return m.Name == parts[3] }) {
		return "", "", "", false
	}
	return parts[1], c, parts[3], true
}

// scenarioTask returns the task of the scenario s, "<task>.t<n>", when n
// is seed, from 1, written as the driver writes it.
func scenarioTask(s string, seed uint64) (string, bool) {
	i := strings.LastIndex(s, ".t")
	if i < 1 || seed < 1 {
		return "", false
	}
	id := s[:i]
	return id, s == id+".t"+strconv.FormatUint(seed, 10)
}

// valueOK reports whether v is a figure the measure can take: 0 or 1 for a
// ratio, a whole number at or above 0 otherwise.
func valueOK(measure string, v float64) bool {
	if ratioMeasure[measure] {
		return v == 0 || v == 1
	}
	return v >= 0 && v == math.Trunc(v)
}

func recordStrings(r drive.Record) []string {
	s := []string{r.Run, r.Instrument, r.Host, r.Condition, r.Model, r.Commit, r.Ended}
	s = append(s, r.HostVersions...)
	return append(s, r.Tasks...)
}

// forbidKeys returns the letters and digits of each word that has some.
func forbidKeys(words []string) []string {
	var keys []string
	for _, w := range words {
		if k := letters(w); k != "" && !slices.Contains(keys, k) {
			keys = append(keys, k)
		}
	}
	return keys
}

// holds reports whether s holds one of keys, matched by its letters and
// digits as a results Store matches them.
func holds(s string, keys []string) bool {
	s = letters(s)
	return slices.ContainsFunc(keys, func(k string) bool { return strings.Contains(s, k) })
}

// letters returns s's letters and digits, in lower case, and nothing else.
func letters(s string) string {
	return strings.Map(func(r rune) rune {
		r = unicode.ToLower(r)
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return r
		}
		return -1
	}, s)
}

func sortedTrials(m map[trialKey]*trial) []trialKey {
	keys := make([]trialKey, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.SortFunc(keys, func(a, b trialKey) int {
		switch {
		case a.host != b.host:
			return strings.Compare(a.host, b.host)
		case a.cond != b.cond:
			return strings.Compare(string(a.cond), string(b.cond))
		case a.task != b.task:
			return strings.Compare(a.task, b.task)
		case a.n < b.n:
			return -1
		case a.n > b.n:
			return 1
		}
		return 0
	})
	return keys
}

func sortedGroups(m map[groupKey]bool) []groupKey {
	keys := make([]groupKey, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.SortFunc(keys, func(a, b groupKey) int {
		if a.host != b.host {
			return strings.Compare(a.host, b.host)
		}
		return strings.Compare(string(a.cond), string(b.cond))
	})
	return keys
}
