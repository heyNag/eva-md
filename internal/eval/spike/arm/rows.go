package arm

import (
	"github.com/heyNag/eva-md/internal/eval/results"
)

// Instrument is the instrument every row of an arm names.
const Instrument = "spike"

// scenarioID is a row's scenario: <form>.<effort>.<side>.<script>, the
// script's id or all.
func scenarioID(a Arm, side, script string) string {
	return string(a.Form) + "." + a.Effort + "." + side + "." + script
}

// Rows are res's rows, from template: instrument spike, model the arm's
// tier, host and host version the arm's, the rest the template's. For
// each round, side (on, then off) and script, then for all the scripts,
// with scenario <form>.<effort>.<on|off>.<script id or all>: spike.<id>
// for every count and ratio of the side's tally; the calls, tokens and
// seconds; spike.repair_tokens_cached_in when there was a repair call and
// every one reported cached input; spike.honest_refused when it has a
// denominator; and the fidelity scorer's rows of the side's store. With
// Repeat rounds, each scenario has a row of each metric a round.
func Rows(res Result, template results.Row) []results.Row {
	base := template
	base.Instrument, base.Model, base.Host, base.HostVersion = Instrument, res.Arm.Tier, res.Arm.Host, res.Arm.HostVersion
	var rows []results.Row
	for _, rd := range res.Rounds {
		for _, side := range []string{"on", "off"} {
			pick := func(sr ScriptResult) Side { return sr.On }
			all := rd.On
			if side == "off" {
				pick, all = func(sr ScriptResult) Side { return sr.Off }, rd.Off
			}
			for _, sr := range rd.Scripts {
				rows = append(rows, sideRows(base, scenarioID(res.Arm, side, sr.Script), pick(sr))...)
			}
			rows = append(rows, sideRows(base, scenarioID(res.Arm, side, "all"), all)...)
		}
	}
	return rows
}

// sideRows are one side's rows under scenario.
func sideRows(base results.Row, scenario string, s Side) []results.Row {
	var rows []results.Row
	add := func(metric, unit string, value float64) {
		r := base
		r.Metric, r.Unit, r.Value, r.Scenario = metric, unit, value, scenario
		rows = append(rows, r)
	}
	for _, m := range s.Tally.Counts() {
		add("spike."+m.ID, "count", m.Value)
	}
	for _, m := range s.Tally.Ratios() {
		add("spike."+m.ID, "ratio", m.Value)
	}
	c := s.Calls
	add("spike.calls_first", "count", float64(c.First))
	add("spike.calls_repair", "count", float64(c.Repair))
	add("spike.calls_failed", "count", float64(c.Failed))
	add("spike.tokens_in", "tokens", float64(c.TokensIn))
	add("spike.tokens_out", "tokens", float64(c.TokensOut))
	add("spike.repair_tokens_in", "tokens", float64(c.RepairTokensIn))
	if c.Repair > 0 && c.RepairCachedReported == c.Repair {
		add("spike.repair_tokens_cached_in", "tokens", float64(c.RepairCachedIn))
	}
	add("spike.seconds", "seconds", c.Seconds)
	if s.HonestOf > 0 {
		add("spike.honest_refused", "ratio", float64(s.Honest)/float64(s.HonestOf))
	}
	for _, r := range s.Fidelity.Rows(base) {
		r.Scenario = scenario
		rows = append(rows, r)
	}
	return rows
}
