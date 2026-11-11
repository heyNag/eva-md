package health

import (
	"reflect"
	"slices"
	"strings"
	"time"

	"github.com/heyNag/eva-md/internal/paths"
	"github.com/heyNag/eva-md/internal/update"
)

// The report's use of what the background update records (updates.go):
// Updates, UpdateFigures and its Spread and BusyExits, TriggerUnknown, and
// update's lists of the names this release writes, update.Outcomes,
// update.Reasons, update.RefusalRules and update.Triggers. Nothing else of
// the run log is read here.
//
// UpdateFigures already keys its counts by those lists, any other name
// under other. The report keys them again by the same lists (see
// rekeyUpdates), so that what it prints rests on its own filter as well as
// on UpdateFigures'; and it counts the whole refusals by rule alone.

// UpdateReport is UpdateFigures as the report gives it: keyed only by the
// names eva-md's code writes, its refusals by rule alone.
type UpdateReport struct {
	UpdateFigures
}

// reportUpdates reads the run log's figures (see Updates) and keys them
// again; recorded is false when the run log holds no record of the days.
func reportUpdates(d paths.Dirs, now time.Time) (u UpdateReport, recorded bool, err error) {
	f, err := Updates(d, now)
	if err != nil {
		return UpdateReport{}, false, err
	}
	return rekeyUpdates(f), f.Passes > 0 || f.Busy.Jobs > 0, nil
}

// rekeyUpdates is f keyed again by the names this release writes, as the
// report gives it: any other outcome, reason, rule, counter or trigger is
// counted under other, a whole refusal by its rule alone, and a spread
// under a trigger no list holds, which can't be merged into other's, is
// left out. UpdateFigures gives no such trigger.
func rekeyUpdates(f UpdateFigures) UpdateReport {
	triggers := append(slices.Clone(update.Triggers), TriggerUnknown)
	counters := counterNames()
	f.Outcome = rekey(f.Outcome, func(k string) string { return known(k, update.Outcomes) })
	f.Skips = rekey(f.Skips, func(k string) string { return known(k, update.Reasons) })
	f.Fails = rekey(f.Fails, func(k string) string { return known(k, update.Reasons) })
	f.Refused = rekey(f.Refused, func(k string) string { rule, _, _ := strings.Cut(k, " at "); return known(rule, update.RefusalRules) })
	f.Inside = rekey(f.Inside, func(k string) string { return known(k, counters) })
	f.Busy.ByTrigger = rekey(f.Busy.ByTrigger, func(k string) string { return known(k, triggers) })
	spreads := map[string]Spread{}
	for k, s := range f.ToMemoryMS {
		if slices.Contains(triggers, k) {
			spreads[k] = s
		}
	}
	f.ToMemoryMS = spreads
	return UpdateReport{UpdateFigures: f}
}

// zeroUpdates is the update part of a run log that can't be read: its
// figures over no record, with no daily cap read.
func zeroUpdates(now time.Time) UpdateReport {
	return UpdateReport{UpdateFigures: updateFigures(nil, now, 0)}
}

// rekey is counts keyed by name(key), counts that fall on one name added.
func rekey(counts map[string]int, name func(string) string) map[string]int {
	out := map[string]int{}
	for k, n := range counts {
		out[name(k)] += n
	}
	return out
}

// counterNames are the names UpdateFigures.Inside counts a validation
// report's counters by: each JSON name of update.Report, a section's with
// the section in front, as rules.refused.
func counterNames() []string {
	var names []string
	var walk func(t reflect.Type, prefix string)
	walk = func(t reflect.Type, prefix string) {
		for i := range t.NumField() {
			f := t.Field(i)
			name, _, _ := strings.Cut(f.Tag.Get("json"), ",")
			if name == "" || name == "-" {
				continue
			}
			if f.Type.Kind() == reflect.Struct {
				walk(f.Type, prefix+name+".")
				continue
			}
			names = append(names, prefix+name)
		}
	}
	walk(reflect.TypeFor[update.Report](), "")
	return names
}
