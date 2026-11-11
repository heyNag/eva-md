package score

import (
	"slices"

	"github.com/heyNag/eva-md/internal/eval/results"
	"github.com/heyNag/eva-md/internal/eval/scenario"
)

// The metrics a result gives, as results rows name them.
const (
	MetricFactRecall    = "fidelity.fact_recall"
	MetricFactHolds     = "fidelity.fact_holds"
	MetricWrongWrite    = "fidelity.wrong_write"
	MetricNoise         = "fidelity.noise"
	MetricMisattributed = "fidelity.misattributed"
	MetricStaleInForce  = "fidelity.stale_in_force"
	MetricDupes         = "fidelity.dupes"
	MetricOTRStored     = "fidelity.otr_stored"
	MetricViolations    = "fidelity.violations"
)

// Metrics are a result's figures. The two shares are of the facts that
// must be stored and are still in force, 1 when there is none; the rest
// count items, but StaleInForce, which counts facts.
type Metrics struct {
	FactRecall float64 `json:"fact_recall"` // facts found or holding
	FactHolds  float64 `json:"fact_holds"`  // facts holding
	// WrongWrite counts items in force, active items and dated lines, with
	// a wrong or dropped value, or that cite no line of the scenario. A
	// retired item is a record of what no longer holds, and is left out.
	WrongWrite int `json:"wrong_write"`
	// Noise counts items that cite only lines that plant nothing.
	Noise int `json:"noise"`
	// Misattributed counts items under the user's tag whose every cited
	// turn is someone else's words.
	Misattributed int `json:"misattributed"`
	// StaleInForce counts facts that ended and that an active item keeps
	// in force.
	StaleInForce int `json:"stale_in_force"`
	// Dupes counts active items that support a fact an earlier active item
	// supports.
	Dupes int `json:"dupes"`
	// OTRStored counts items that hold a fact said off the record.
	OTRStored int `json:"otr_stored"`
	// Violations counts items that break a fact's Must, those of OTRStored
	// among them.
	Violations int `json:"violations"`
}

func (s *scorer) metrics(ex Export, reads []read, res Result) Metrics {
	var m Metrics
	required, recalled, held := 0, 0, 0
	for f, fr := range res.Facts {
		if fr.Outcome == StaleInForce {
			m.StaleInForce++
		}
		if !s.required(f) {
			continue
		}
		required++
		switch fr.Outcome {
		case Holds:
			held++
			recalled++
		case Found:
			recalled++
		}
	}
	m.FactRecall, m.FactHolds = share(recalled, required), share(held, required)
	activeSupported := map[int]bool{}
	for i, it := range ex.Items {
		r := reads[i]
		if r.outcome == Unscored {
			continue
		}
		active := it.State == StateActive
		switch r.outcome {
		case ValueWrong, ValueDropped, DateDropped, CondDropped, LimitDropped, Unsupported:
			if active || it.State == StateLog {
				m.WrongWrite++
			}
		case Noise:
			m.Noise++
		case Violation:
			m.Violations++
		}
		if userTag(it.Authority) && len(r.turns) > 0 && !slices.ContainsFunc(r.turns, userWords) {
			m.Misattributed++
		}
		if slices.ContainsFunc(r.supports, func(sp support) bool {
			return sp.verdict == violation && s.sc.Facts[sp.fact].Must == scenario.MustNotStore
		}) {
			m.OTRStored++
		}
		if active {
			dupe := false
			for _, sp := range r.supports {
				dupe = dupe || activeSupported[sp.fact]
				activeSupported[sp.fact] = true
			}
			if dupe {
				m.Dupes++
			}
		}
	}
	return m
}

func share(n, of int) float64 {
	if of == 0 {
		return 1
	}
	return float64(n) / float64(of)
}

// Rows gives one row for each metric, from template: each with the
// metric's name, value and unit, ratio for a share and count for the rest,
// and the result's scenario. Every other field is the template's.
func (r Result) Rows(template results.Row) []results.Row {
	m := r.Metrics
	var rows []results.Row
	for _, x := range []struct {
		name, unit string
		value      float64
	}{
		{MetricFactRecall, "ratio", m.FactRecall}, {MetricFactHolds, "ratio", m.FactHolds},
		{MetricWrongWrite, "count", float64(m.WrongWrite)}, {MetricNoise, "count", float64(m.Noise)},
		{MetricMisattributed, "count", float64(m.Misattributed)}, {MetricStaleInForce, "count", float64(m.StaleInForce)},
		{MetricDupes, "count", float64(m.Dupes)}, {MetricOTRStored, "count", float64(m.OTRStored)},
		{MetricViolations, "count", float64(m.Violations)},
	} {
		row := template
		row.Metric, row.Unit, row.Value, row.Scenario = x.name, x.unit, x.value, r.Scenario
		rows = append(rows, row)
	}
	return rows
}
