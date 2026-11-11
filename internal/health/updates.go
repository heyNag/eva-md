package health

import (
	"encoding/json"
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/heyNag/eva-md/internal/config"
	"github.com/heyNag/eva-md/internal/paths"
	"github.com/heyNag/eva-md/internal/update"
)

// updateDays is how many days, today's included, eva-md status --updates
// reads of the run log.
const updateDays = 7

// TriggerUnknown is the name the report gives runs whose record names no
// trigger: those an older release wrote, and jobs started with no name.
const TriggerUnknown = "unknown"

// UpdateFigures is eva-md status --updates: what the background update did
// and cost over the run log's last 7 days, and why work waited. It holds
// counts, durations, dates and the fixed names of outcomes, reasons, rules,
// places in the answer's schema, triggers and hosts, never a session, a
// project, a branch, a model or any text a record holds.
type UpdateFigures struct {
	From    string         `json:"from"` // the first day read, YYYY-MM-DD
	To      string         `json:"to"`   // the last, today
	Passes  int            `json:"passes"`
	Outcome map[string]int `json:"outcomes"` // passes by outcome
	Skips   map[string]int `json:"skips"`    // skipped passes by reason
	Fails   map[string]int `json:"failures"` // failed passes by reason
	// Checked is how many answers the checker read: those it accepted and
	// those it refused whole, which Refused counts by "rule at place".
	Checked int            `json:"checked"`
	Refused map[string]int `json:"refused_whole"`
	// Inside sums, over the accepted answers, what the checker changed or
	// refused in each, by the name of its counter, as validation.refused.
	Inside     map[string]int    `json:"inside_accepted"`
	TokensIn   Spread            `json:"tokens_in"`   // over passes that made a model call
	TokensOut  Spread            `json:"tokens_out"`  // the same
	DurationMS Spread            `json:"duration_ms"` // the same: the call, its retries included
	ToMemoryMS map[string]Spread `json:"to_memory_ms"` // from a chunk's newest entry to its saved page, by trigger, the sweep apart
	Busy       BusyExits         `json:"busy"`
	Calls      []DayCalls        `json:"calls"`
	// Reported is how many model calls reported the model that answered,
	// and Other how many of them a model other than the one asked for.
	Reported int `json:"model_reported"`
	Other    int `json:"model_other"`
}

// Spread is a set of values: how many, their median, 95th percentile by the
// nearest rank, and maximum.
type Spread struct {
	N      int   `json:"n"`
	Median int64 `json:"median"`
	P95    int64 `json:"p95"`
	Max    int64 `json:"max"`
}

// BusyExits counts the jobs that left because another held the update
// lock: in all, the sessions they were started for, and by trigger.
type BusyExits struct {
	Jobs      int            `json:"jobs"`
	Sessions  int            `json:"sessions"`
	ByTrigger map[string]int `json:"by_trigger"`
}

// DayCalls is one day's model calls by host, against the daily cap.
type DayCalls struct {
	Day   string         `json:"day"`
	Hosts map[string]int `json:"hosts"`
	Cap   int            `json:"cap"`
}

// Updates reads the run log of the 7 days that end on now's day and sums it
// up. It reads the config only for the daily cap, the default's when it
// can't be read, and writes nothing.
func Updates(d paths.Dirs, now time.Time) (UpdateFigures, error) {
	records, err := update.Days(d.State, now, updateDays)
	if err != nil {
		return UpdateFigures{}, err
	}
	c, err := config.Load(d.ConfigFile())
	if err != nil {
		c = config.Default()
	}
	return updateFigures(records, now, c.DailyCap), nil
}

// updateFigures sums records up, those of the 7 days that end on now's day.
func updateFigures(records []update.Record, now time.Time, dailyCap int) UpdateFigures {
	f := UpdateFigures{From: now.AddDate(0, 0, 1-updateDays).Format(time.DateOnly), To: now.Format(time.DateOnly),
		Outcome: map[string]int{}, Skips: map[string]int{}, Fails: map[string]int{}, Refused: map[string]int{}, Inside: map[string]int{},
		ToMemoryMS: map[string]Spread{}, Busy: BusyExits{ByTrigger: map[string]int{}}}
	var in, out, took []int64
	toMemory := map[string][]int64{}
	sessions := map[string]bool{}
	calls := map[string]map[string]int{}
	for _, r := range records {
		trigger := safeName(r.Trigger)
		if r.Trigger == "" {
			trigger = TriggerUnknown
		}
		if r.Outcome == update.OutcomeBusy {
			f.Busy.Jobs++
			f.Busy.ByTrigger[trigger]++
			for _, s := range r.SessionHashes {
				sessions[s] = true
			}
			continue
		}
		f.Passes++
		f.Outcome[safeName(r.Outcome)]++
		switch r.Outcome {
		case update.OutcomeSkipped:
			f.Skips[safeName(r.Reason)]++
		case update.OutcomeFailed:
			f.Fails[safeName(r.Reason)]++
		}
		if r.Validation != nil {
			f.Checked++
			addCounters(f.Inside, "", r.Validation)
		}
		if r.Rejection != nil {
			f.Checked++
			f.Refused[safeName(r.Rejection.Rule)+" at "+safeName(r.Rejection.Place)]++
		}
		if r.Calls > 0 {
			in, out, took = append(in, int64(r.TokensIn)), append(out, int64(r.TokensOut)), append(took, r.DurationMS)
			if calls[day(r.At)] == nil {
				calls[day(r.At)] = map[string]int{}
			}
			calls[day(r.At)][r.Host]++
		}
		if r.ModelReported != "" {
			f.Reported++
			if r.ModelReported != r.Model {
				f.Other++
			}
		}
		if r.Pages > 0 && r.BoundaryToSavedMS > 0 {
			toMemory[trigger] = append(toMemory[trigger], r.BoundaryToSavedMS)
		}
	}
	f.TokensIn, f.TokensOut, f.DurationMS = spread(in), spread(out), spread(took)
	for trigger, values := range toMemory {
		f.ToMemoryMS[trigger] = spread(values)
	}
	f.Busy.Sessions = len(sessions)
	for i := range updateDays {
		date := now.AddDate(0, 0, i+1-updateDays).Format(time.DateOnly)
		hosts := map[string]int{}
		for _, host := range updateHosts {
			hosts[host] = calls[date][host]
		}
		f.Calls = append(f.Calls, DayCalls{Day: date, Hosts: hosts, Cap: dailyCap})
	}
	return f
}

// fixedName is the shape of the names the job writes: outcomes, reasons,
// triggers, rules, places in the answer's schema and counters.
var fixedName = regexp.MustCompile(`^[a-z][a-z0-9_.\[\]]{0,63}$`)

// safeName is s, a name the run log holds, when it has the shape of one the job
// writes, and otherwise other, so whatever a damaged or edited run log
// holds, the report shows no text of it.
func safeName(s string) string {
	if fixedName.MatchString(s) {
		return s
	}
	return update.TriggerOther
}

// day is the date of a record's RFC 3339 time.
func day(at string) string {
	if len(at) < len(time.DateOnly) {
		return ""
	}
	return at[:len(time.DateOnly)]
}

// addCounters adds the counts of v, a validation report, to sums, by the
// JSON name of each, with its section in front, as rules.refused; a true
// flag counts one.
func addCounters(sums map[string]int, prefix string, v any) {
	data, err := json.Marshal(v)
	var m map[string]any
	if err != nil || json.Unmarshal(data, &m) != nil {
		return
	}
	for key, value := range m {
		key = safeName(key)
		switch value := value.(type) {
		case float64:
			sums[prefix+key] += int(value)
		case bool:
			if value {
				sums[prefix+key]++
			}
		case map[string]any:
			addCounters(sums, prefix+key+".", value)
		}
	}
}

// spread works out a Spread of values, which it sorts.
func spread(values []int64) Spread {
	if len(values) == 0 {
		return Spread{}
	}
	slices.Sort(values)
	rank := func(pct int) int64 { return values[(len(values)*pct+99)/100-1] }
	return Spread{N: len(values), Median: rank(50), P95: rank(95), Max: values[len(values)-1]}
}

// Lines words the figures, a line each, as eva-md status --updates prints
// them.
func (f UpdateFigures) Lines() []string {
	lines := []string{
		fmt.Sprintf("updates from %s to %s: %d %s recorded", f.From, f.To, f.Passes, plural(f.Passes, "pass", "passes")),
		"outcomes: " + counts(f.Outcome, []string{update.OutcomeOK, update.OutcomeNoChange, update.OutcomeSkipped, update.OutcomeFailed}),
		"skips by reason: " + counts(f.Skips, nil),
		"failures by reason: " + counts(f.Fails, nil),
		fmt.Sprintf("answers refused whole: %d of %d checked; by rule: %s", total(f.Refused), f.Checked, counts(f.Refused, nil)),
		"inside accepted answers: " + counts(f.Inside, nil),
		"tokens in per call: " + f.TokensIn.words(1, ""),
		"tokens out per call: " + f.TokensOut.words(1, ""),
		"time per call: " + f.DurationMS.words(1000, " s"),
	}
	var memory []string
	for _, trigger := range append(slices.Clone(update.Triggers), TriggerUnknown) {
		if s, ok := f.ToMemoryMS[trigger]; ok && trigger != update.TriggerSweep {
			memory = append(memory, trigger+" "+s.words(60000, " min"))
		}
	}
	lines = append(lines, "chat boundary to memory on file, by trigger: "+orNone(strings.Join(memory, "; ")))
	if s, ok := f.ToMemoryMS[update.TriggerSweep]; ok {
		lines = append(lines, "chat boundary to memory on file, the day's sweep: "+s.words(60000, " min"))
	} else {
		lines = append(lines, "chat boundary to memory on file, the day's sweep: none")
	}
	lines = append(lines, fmt.Sprintf("busy exits: %d, for %d %s; by trigger: %s", f.Busy.Jobs, f.Busy.Sessions, plural(f.Busy.Sessions, "session", "sessions"), counts(f.Busy.ByTrigger, nil)))
	var days []string
	for _, d := range f.Calls {
		var hosts []string
		for _, host := range updateHosts {
			hosts = append(hosts, fmt.Sprintf("%s %d", host, d.Hosts[host]))
		}
		days = append(days, fmt.Sprintf("%s %s of %d", d.Day, strings.Join(hosts, ", "), d.Cap))
	}
	lines = append(lines, "model calls a day, each host against the cap: "+strings.Join(days, "; "))
	lines = append(lines, fmt.Sprintf("model reported: %d %s named the model that answered, %d of them one other than asked", f.Reported, plural(f.Reported, "call", "calls"), f.Other))
	return lines
}

// words says a Spread in unit, values divided by per: "median 1.5 s, p95
// 3.0 s, max 4.2 s (12)".
func (s Spread) words(per float64, unit string) string {
	if s.N == 0 {
		return "none"
	}
	n := func(v int64) string {
		if per == 1 {
			return fmt.Sprint(v)
		}
		return fmt.Sprintf("%.1f%s", float64(v)/per, unit)
	}
	return fmt.Sprintf("median %s, p95 %s, max %s (%d)", n(s.Median), n(s.P95), n(s.Max), s.N)
}

// counts says m's counts as "3 small, 1 headless": the names of order
// first, in its order, then the rest by name; those at zero are left out.
func counts(m map[string]int, order []string) string {
	var parts []string
	for _, key := range order {
		if m[key] > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", m[key], key))
		}
	}
	for _, key := range slices.Sorted(maps.Keys(m)) {
		if m[key] > 0 && !slices.Contains(order, key) {
			parts = append(parts, fmt.Sprintf("%d %s", m[key], key))
		}
	}
	return orNone(strings.Join(parts, ", "))
}

func total(m map[string]int) int {
	n := 0
	for _, v := range m {
		n += v
	}
	return n
}

func orNone(s string) string {
	if s == "" {
		return "none"
	}
	return s
}
