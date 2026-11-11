package health

import (
	"time"

	"github.com/heyNag/eva-md/internal/paths"
)

// The report's use of what the hooks record of their week (hookweek.go):
// eventWeek, weekOf, HookDay and its counts, and the names they are counted
// by, DurationBounds, HeldKinds, DeliveryKinds and Routes. Nothing else of
// the heartbeats' weeks is read here.

// HookFigures is the hooks' week, in UTC days as the heartbeats keep it:
// for each event of each host, the days it ran, its runs, those that
// failed, and its runs' times, a count for each of MSBounds and one past
// them, with the bound the median and the 95th percentile fall within, -1
// past the last; what the hooks held back, while transcript recovery was
// behind and while a chat was off the record, by kind; and what they
// delivered (see deliveredCheck).
type HookFigures struct {
	From      string                          `json:"from"`
	To        string                          `json:"to"`
	MSBounds  []float64                       `json:"ms_bounds"`
	Events    map[string]map[string]EventWeek `json:"events"`
	Behind    map[string]int                  `json:"held_behind"`
	OffRecord map[string]int                  `json:"held_off_record"`
	Delivered Delivered                       `json:"delivered"`
}

// EventWeek is one hook event's week.
type EventWeek struct {
	Days   int     `json:"days"`
	Runs   int     `json:"runs"`
	Failed int     `json:"failed"`
	MS     []int   `json:"ms"`
	P50    float64 `json:"p50_ms_within"`
	P95    float64 `json:"p95_ms_within"`
}

// Delivered is what the hooks of both hosts added to chats in the week:
// runs by kind, those that added text and the characters they added, the
// runs each route added to, the briefs of a new chat or a compaction given
// in time (see Delivery.briefed) and of those the ones that showed the
// thread's state, a stale state, a pending update or either, the briefs the
// chat's first prompt carried, and the runs that passed their deadline.
type Delivered struct {
	Kinds          map[string]int `json:"kinds"`
	Added          int            `json:"added"`
	Chars          int            `json:"chars"`
	Routes         map[string]int `json:"routes"`
	Briefs         int            `json:"briefs"`
	WithState      int            `json:"with_state"`
	Stale          int            `json:"stale"`
	Pending        int            `json:"pending"`
	StaleOrPending int            `json:"stale_or_pending"`
	Fallback       int            `json:"fallback"`
	Late           int            `json:"late"`
}

// hookFigures reads the week of every hook event of both hosts that ends
// on now's day, and reports whether any run was recorded.
func hookFigures(d paths.Dirs, now time.Time) (HookFigures, bool) {
	from, to := weekOf(now)
	f := HookFigures{From: from, To: to, MSBounds: DurationBounds, Events: map[string]map[string]EventWeek{}}
	sum := HookDay{}.fit()
	for _, host := range updateHosts {
		f.Events[host] = map[string]EventWeek{}
		for _, event := range hostEvents(host) {
			e := HookDay{}.fit()
			days := eventWeek(d, host, event, now)
			for _, day := range days {
				e.plus(day)
				sum.plus(day)
			}
			f.Events[host][event] = EventWeek{Days: len(days), Runs: e.Runs, Failed: e.Failed, MS: e.MS, P50: within(e.MS, 50), P95: within(e.MS, 95)}
		}
	}
	f.Behind, f.OffRecord = byName(sum.Behind, HeldKinds), byName(sum.OffRecord, HeldKinds)
	f.Delivered = Delivered{Kinds: byName(sum.Kinds, DeliveryKinds), Added: sum.Added, Chars: sum.Chars, Routes: byName(sum.Routes, Routes),
		Briefs: sum.Briefs, WithState: sum.WithState, Stale: sum.Stale, Pending: sum.Pending, StaleOrPending: sum.StaleOrPending, Fallback: sum.Fallback, Late: sum.Late}
	return f, sum.Runs > 0
}

// byName is counts keyed by names, the name of each count in its order.
func byName(counts []int, names []string) map[string]int {
	m := map[string]int{}
	for i, name := range names {
		if i < len(counts) {
			m[name] = counts[i]
		}
	}
	return m
}

// within is the bound of DurationBounds that pct percent of the runs ms
// counts took at most, by the nearest rank: -1 when that rank is past the
// last bound, and 0 with no run.
func within(ms []int, pct int) float64 {
	n := 0
	for _, c := range ms {
		n += c
	}
	if n == 0 {
		return 0
	}
	rank := (n*pct + 99) / 100
	seen := 0
	for i, c := range ms {
		if seen += c; seen >= rank {
			if i < len(DurationBounds) {
				return DurationBounds[i]
			}
			break
		}
	}
	return -1
}
