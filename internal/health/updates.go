package health

import (
	"encoding/json"
	"fmt"
	"maps"
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
	// refused in each, by the name of its counter, as refused or rules.refused.
	Inside     map[string]int    `json:"inside_accepted"`
	TokensIn   Spread            `json:"tokens_in"`    // over passes that made a model call
	TokensOut  Spread            `json:"tokens_out"`   // the same
	DurationMS Spread            `json:"duration_ms"`  // the same: the call, its retries included
	ToMemoryMS map[string]Spread `json:"to_memory_ms"` // from a chunk's newest entry to its saved page, by trigger, the sweep apart
	Busy       BusyExits         `json:"busy"`
	Calls      []DayCalls        `json:"calls"`
	// Reported is how many passes named the model that answered. Default
	// is how many of them asked for the host's default, so asked for no
	// name, and Other how many asked for a name and were answered under
	// another, a suffix in brackets, as [1m], aside.
	Reported int `json:"model_reported"`
	Default  int `json:"model_default"`
	Other    int `json:"model_other"`
	// Cost is what each host's updates spent, with the update's switch
	// token_budget on (see costSection); off, it is absent.
	Cost []HostCost `json:"cost,omitempty"`
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
// up. It reads the config only for the daily cap and the switch
// token_budget, which adds the cost section, the default's when it can't be
// read, and writes nothing.
func Updates(d paths.Dirs, now time.Time) (UpdateFigures, error) {
	records, err := update.Days(d.State, now, updateDays)
	if err != nil {
		return UpdateFigures{}, err
	}
	c, err := config.Load(d.ConfigFile())
	if err != nil {
		c = config.Default()
	}
	f := updateFigures(records, now, c.DailyCap)
	f.Cost = costSection(c, records, now)
	return f, nil
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
		trigger := safeName(r.Trigger, update.Triggers)
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
		f.Outcome[safeName(r.Outcome, update.Outcomes)]++
		switch r.Outcome {
		case update.OutcomeSkipped:
			f.Skips[safeName(r.Reason, update.Reasons)]++
		case update.OutcomeFailed:
			f.Fails[safeName(r.Reason, update.Reasons)]++
		}
		if r.Validation != nil {
			f.Checked++
			addCounters(f.Inside, "", r.Validation)
		}
		if r.Rejection != nil {
			f.Checked++
			f.Refused[safeName(r.Rejection.Rule, update.RefusalRules)+" at "+safePlace(r.Rejection.Place)]++
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
			switch {
			case r.Model == "":
				f.Default++
			case modelBase(r.ModelReported) != modelBase(r.Model):
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

// safeName is s, a name the run log holds, when it is one of known, the
// outcomes, reasons, rules or triggers this release writes, and otherwise
// other, so whatever a damaged or edited run log holds, the report shows
// no text of it.
func safeName(s string, known []string) string {
	if slices.Contains(known, s) {
		return s
	}
	return update.TriggerOther
}

// modelBase is a model's name without a suffix in brackets, as [1m], which
// a name asked for may carry and the name the host reports doesn't.
func modelBase(model string) string {
	base, _, _ := strings.Cut(model, "[")
	return base
}

// safePlace is s, the place of a whole refusal, when it is one this
// release can name, and otherwise other, as safeName.
func safePlace(s string) string {
	if update.KnownPlace(s) {
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
// flag counts one. The names are those of update.Report's fields, which
// the record's report was decoded into and is encoded from here, never a
// key the run log holds.
func addCounters(sums map[string]int, prefix string, v any) {
	data, err := json.Marshal(v)
	var m map[string]any
	if err != nil || json.Unmarshal(data, &m) != nil {
		return
	}
	for key, value := range m {
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
	lines = append(lines, fmt.Sprintf("model reported: %d %s named the model that answered; of them %d asked for the host's default and %d named a model other than the one asked for",
		f.Reported, plural(f.Reported, "pass", "passes"), f.Default, f.Other))
	return append(lines, costLines(f.Cost)...)
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
