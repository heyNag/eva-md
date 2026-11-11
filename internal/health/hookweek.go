package health

import (
	"slices"
	"strings"
	"time"

	"github.com/heyNag/eva-md/internal/host/adapter"
	"github.com/heyNag/eva-md/internal/paths"
)

// The week of each hook event's runs, which its heartbeat keeps beside its
// last 20 runs: a day's counts for each of the last WeekDays days, so the
// doctor can read a week whatever the number of runs, in a file of bounded
// size. The hook adds each run to it in the one write it already makes, the
// heartbeat's. Every field is a count or a day: no text, path, project,
// branch or session is kept.

// WeekDays is how many days, today's included, a heartbeat's week keeps.
const WeekDays = 7

// DurationBounds are the upper bounds, in milliseconds, of the buckets a
// day counts its runs' times in. A run is counted in the first bucket whose
// bound is at least its time, or in one more bucket past the last bound.
var DurationBounds = []float64{2, 5, 10, 20, 30, 50, 75, 100, 150, 200, 300, 500, 1000, 1500, 3000}

// HeldKinds are what a run can hold back, in the order a day counts them:
// a prompt, a reply, a tool line and a brief.
var HeldKinds = []string{"prompt", "reply", "tool", "brief"}

// What held a run's content back: transcript recovery that had not read to
// the end of the transcript, or a chat off the record.
const (
	HeldByRecovery  = "recovery"
	HeldByOffRecord = "off_record"
)

// Routes are what can add text to a chat, in the order a day counts them:
// the standing rules, the project's constraints, the thread's state (its
// Now), captured exchanges, a recalled statement, system pointers and the
// memory reminder.
var Routes = []string{"rules", "constraints", "state", "exchanges", "recall", "pointers", "reminder"}

// DeliveryKinds are the kinds of run that can add text to a chat, in the
// order a day counts them: each kind of chat start, then a prompt.
var DeliveryKinds = []string{"startup", "clear", "resume", "fork", "compact", "prompt"}

// Delivery is what a run that can add text to a chat delivered, with no
// text: its kind (one of DeliveryKinds), the routes that added something,
// the characters added, whether the state shown was older than the newest
// captured exchange, whether an update was pending, whether a prompt was
// built to carry the chat's first brief because its start had not (the
// first-prompt fallback; a day counts it only when it delivered), and
// whether the run passed its deadline and delivered nothing.
type Delivery struct {
	Kind     string   `json:"kind"`
	Routes   []string `json:"routes,omitempty"`
	Chars    int      `json:"chars,omitempty"`
	Stale    bool     `json:"stale,omitempty"`
	Pending  bool     `json:"pending,omitempty"`
	Fallback bool     `json:"fallback,omitempty"`
	Late     bool     `json:"late,omitempty"`
}

// briefed reports whether d gave a chat the brief of a new chat or of a
// compaction, the briefs whose state can be stale or pending: a startup,
// clear or compact start, or a prompt that carried the first brief, that
// delivered text and did not pass its deadline. A resumed or forked chat's
// brief shows only what changed, and is not one.
func (d Delivery) briefed() bool {
	if d.Late || d.Chars == 0 {
		return false
	}
	switch d.Kind {
	case "startup", "clear", "compact":
		return true
	}
	return d.Kind == "prompt" && d.Fallback
}

// HookRun is one run as its day counts it.
type HookRun struct {
	Failed     bool
	DurationMS float64
	Held       []string // what it held back, of HeldKinds
	HeldBy     string   // why: HeldByRecovery or HeldByOffRecord
	Delivered  *Delivery
}

// HookDay is one day of an event's runs, by its date in UTC: how many ran
// and failed; their times, a count for each bucket of DurationBounds and
// one past them; what they held back, a count for each of HeldKinds, while
// recovery was behind and while a chat was off the record; and of the runs
// that could add text to a chat, how many of each of DeliveryKinds, how
// many added text, the characters they added, how many added each of
// Routes; how many gave a new chat's or a compaction's brief (see
// Delivery.briefed), and of those how many showed the thread's state, a
// stale state, a pending update, and either; then the first-prompt briefs
// that were delivered and the runs that passed their deadline.
type HookDay struct {
	Day            string `json:"day"`
	Runs           int    `json:"runs"`
	Failed         int    `json:"failed,omitempty"`
	MS             []int  `json:"ms"`
	Behind         []int  `json:"behind,omitempty"`
	OffRecord      []int  `json:"off_record,omitempty"`
	Kinds          []int  `json:"kinds,omitempty"`
	Added          int    `json:"added,omitempty"`
	Chars          int    `json:"chars,omitempty"`
	Routes         []int  `json:"routes,omitempty"`
	Briefs         int    `json:"briefs,omitempty"`
	WithState      int    `json:"with_state,omitempty"`
	Stale          int    `json:"stale,omitempty"`
	Pending        int    `json:"pending,omitempty"`
	StaleOrPending int    `json:"stale_or_pending,omitempty"`
	Fallback       int    `json:"fallback,omitempty"`
	Late           int    `json:"late,omitempty"`
}

// AddRun adds run, which ended at now, to an event's week and returns the
// week: the days of the last WeekDays, oldest first, at most WeekDays of
// them. Days outside the week, after today's as a clock set back leaves
// them included, any that isn't a date and a day's second copy are dropped,
// and a day's counts are cut or filled to their lengths, so the week stays
// within its bounds whatever an older or edited heartbeat held.
func AddRun(week []HookDay, now time.Time, run HookRun) []HookDay {
	first, today := weekOf(now)
	kept := []HookDay{HookDay{Day: today}.fit()}
	seen := map[string]bool{}
	for _, d := range week {
		if !inWeek(d.Day, first, today) || seen[d.Day] {
			continue
		}
		seen[d.Day] = true
		if d.Day == today {
			kept[0] = d.fit()
			continue
		}
		kept = append(kept, d.fit())
	}
	kept[0].add(run)
	slices.SortFunc(kept, func(a, b HookDay) int { return strings.Compare(a.Day, b.Day) })
	return kept
}

// weekOf is the first and the last day, in UTC, of the week that ends with
// now's day.
func weekOf(now time.Time) (first, today string) {
	return now.UTC().AddDate(0, 0, 1-WeekDays).Format(time.DateOnly), now.UTC().Format(time.DateOnly)
}

// inWeek reports whether day is a date, written as a heartbeat writes one,
// from first to today.
func inWeek(day, first, today string) bool {
	t, err := time.Parse(time.DateOnly, day)
	return err == nil && t.Format(time.DateOnly) == day && day >= first && day <= today
}

// fit cuts or fills each of d's counts to its length.
func (d HookDay) fit() HookDay {
	size := func(counts []int, n int) []int {
		out := make([]int, n)
		copy(out, counts)
		return out
	}
	d.MS = size(d.MS, len(DurationBounds)+1)
	d.Behind = size(d.Behind, len(HeldKinds))
	d.OffRecord = size(d.OffRecord, len(HeldKinds))
	d.Kinds = size(d.Kinds, len(DeliveryKinds))
	d.Routes = size(d.Routes, len(Routes))
	return d
}

// add counts run in d.
func (d *HookDay) add(run HookRun) {
	d.Runs++
	if run.Failed {
		d.Failed++
	}
	bucket := slices.IndexFunc(DurationBounds, func(bound float64) bool { return run.DurationMS <= bound })
	if bucket < 0 {
		bucket = len(DurationBounds)
	}
	d.MS[bucket]++
	held := d.Behind
	if run.HeldBy == HeldByOffRecord {
		held = d.OffRecord
	}
	for _, kind := range run.Held {
		if i := slices.Index(HeldKinds, kind); i >= 0 {
			held[i]++
		}
	}
	v := run.Delivered
	if v == nil {
		return
	}
	if i := slices.Index(DeliveryKinds, v.Kind); i >= 0 {
		d.Kinds[i]++
	}
	if v.Chars > 0 {
		d.Added++
		d.Chars += v.Chars
	}
	for _, route := range v.Routes {
		if i := slices.Index(Routes, route); i >= 0 {
			d.Routes[i]++
		}
	}
	if v.Late {
		d.Late++
		return
	}
	if v.Fallback && v.Chars > 0 {
		d.Fallback++
	}
	if v.briefed() {
		d.Briefs++
		if slices.Contains(v.Routes, "state") {
			d.WithState++
		}
		if v.Stale {
			d.Stale++
		}
		if v.Pending {
			d.Pending++
		}
		if v.Stale || v.Pending {
			d.StaleOrPending++
		}
	}
}

// eventWeek is the days of the week ending with now's that the heartbeat of
// host's event keeps, each once, fitted to its lengths. A heartbeat that
// isn't valid (see readBeat), or from before weeks were kept, has none. The
// week is decoded here alone, so the readers of a heartbeat's other fields,
// such as a chat start's notice, don't pay for it.
func eventWeek(d paths.Dirs, host, event string, now time.Time) []HookDay {
	var b struct {
		beat
		Week []HookDay `json:"week"`
	}
	if !decodeBeat(d, host, event, now, &b.beat, &b) {
		return nil
	}
	first, today := weekOf(now)
	var days []HookDay
	seen := map[string]bool{}
	for _, day := range b.Week {
		if inWeek(day.Day, first, today) && !seen[day.Day] && day.Runs > 0 {
			seen[day.Day] = true
			days = append(days, day.fit())
		}
	}
	return days
}

// hookWeek is the sum of every hook event's days, on both hosts, in the
// week ending with now's day.
func hookWeek(d paths.Dirs, now time.Time) HookDay {
	t := HookDay{}.fit()
	for _, h := range adapter.All() {
		host := string(h.ID)
		for _, event := range hostEvents(host) {
			for _, day := range eventWeek(d, host, event, now) {
				t.plus(day)
			}
		}
	}
	return t
}

// plus adds day's counts, fitted to their lengths, to d's.
func (d *HookDay) plus(day HookDay) {
	sum := func(to, from []int) {
		for i := range to {
			to[i] += from[i]
		}
	}
	sum(d.MS, day.MS)
	sum(d.Behind, day.Behind)
	sum(d.OffRecord, day.OffRecord)
	sum(d.Kinds, day.Kinds)
	sum(d.Routes, day.Routes)
	d.Runs += day.Runs
	d.Failed += day.Failed
	d.Added += day.Added
	d.Chars += day.Chars
	d.Briefs += day.Briefs
	d.WithState += day.WithState
	d.Stale += day.Stale
	d.Pending += day.Pending
	d.StaleOrPending += day.StaleOrPending
	d.Fallback += day.Fallback
	d.Late += day.Late
}
