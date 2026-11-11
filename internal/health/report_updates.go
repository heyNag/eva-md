package health

import (
	"reflect"
	"strings"
	"time"

	"github.com/heyNag/eva-md/internal/paths"
	"github.com/heyNag/eva-md/internal/update"
)

// The report's use of what the background update records (updates.go):
// Updates, UpdateFigures and its Spread and BusyExits, TriggerUnknown, and
// update's names of triggers, outcomes, reasons and rules. Nothing else of
// the run log is read here.
//
// UpdateFigures keys its counts by the names the run log holds, which a
// damaged or edited run log could make any short lower-case text, such as
// a project's name. The report keys them again by the names eva-md's own
// code writes, and counts any other under other, or, for a spread, which
// can't be merged, drops it and counts it in Unnamed.

// updateOutcomes are the outcomes of a pass; a busy exit is counted apart.
var updateOutcomes = []string{update.OutcomeOK, update.OutcomeNoChange, update.OutcomeSkipped, update.OutcomeFailed}

// updateReasons are the reasons a pass is skipped or fails for.
var updateReasons = []string{
	update.ReasonPageRefused, update.ReasonWikiError, update.ReasonConflict, update.ReasonWatermark, update.ReasonLeft,
	update.ReasonLocked, update.ReasonVerified, update.ReasonDamaged, update.ReasonQuarantined, update.ReasonSmall,
	update.ReasonHeadless, update.ReasonCLIMissing, update.ReasonAuth, update.ReasonRateLimit, update.ReasonHostError,
	update.ReasonModelRejected, update.ReasonTimeout, update.ReasonInvalidOutput, update.ReasonToolUsed, update.ReasonMCPFailed,
}

// rejectionRules are the rules that refuse an answer whole.
var rejectionRules = []string{
	update.RuleNotObject, update.RuleMissingKey, update.RuleExtraKey, update.RuleNotString, update.RuleNotNull, update.RuleNotEnum,
	update.RuleNotArray, update.RuleArraySize, update.RuleNotInteger, update.RuleDates, update.RuleCarried, update.RuleOther,
}

// UpdateReport is UpdateFigures as the report gives it: keyed only by the
// names eva-md's code writes, its refusals by rule alone, with Unnamed
// counting the spreads dropped for a name it doesn't know.
type UpdateReport struct {
	UpdateFigures
	Unnamed int `json:"unnamed"`
}

// reportUpdates reads the run log's figures (see Updates) and keys them
// again; recorded is false when the run log holds no record of the days.
func reportUpdates(d paths.Dirs, now time.Time) (u UpdateReport, recorded bool, err error) {
	f, err := Updates(d, now)
	if err != nil {
		return UpdateReport{}, false, err
	}
	triggers := append(append([]string{}, update.Triggers...), TriggerUnknown)
	counters := counterNames()
	f.Outcome = rekey(f.Outcome, func(k string) string { return known(k, updateOutcomes) })
	f.Skips = rekey(f.Skips, func(k string) string { return known(k, updateReasons) })
	f.Fails = rekey(f.Fails, func(k string) string { return known(k, updateReasons) })
	f.Refused = rekey(f.Refused, func(k string) string { rule, _, _ := strings.Cut(k, " at "); return known(rule, rejectionRules) })
	f.Inside = rekey(f.Inside, func(k string) string { return known(k, counters) })
	f.Busy.ByTrigger = rekey(f.Busy.ByTrigger, func(k string) string { return known(k, triggers) })
	for k := range f.ToMemoryMS {
		if known(k, triggers) != k {
			delete(f.ToMemoryMS, k)
			u.Unnamed++
		}
	}
	u.UpdateFigures = f
	return u, f.Passes > 0 || f.Busy.Jobs > 0, nil
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
