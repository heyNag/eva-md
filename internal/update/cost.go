package update

import (
	"fmt"
	"slices"
	"time"

	"github.com/heyNag/eva-md/internal/config"
	"github.com/heyNag/eva-md/internal/cost"
)

// LowEffortDefault makes the background update ask for a low effort where
// the config's [effort] table names none, in place of the highest; the
// model stays the chat's. Off, the default, is v1.
var LowEffortDefault = config.NewBoolSwitch("low_effort_default", false)

// TokenBudget holds each host's background update to a daily budget of
// weighted tokens, daily_token_budget, beside daily_cap. Off, the default,
// is v1: only daily_cap stops a host.
var TokenBudget = config.NewBoolSwitch("token_budget", false)

// weekDays is how many days, today's included, a Week covers.
const weekDays = 7

// effortFor is the effort a background update of host and model asks for:
// the config's [effort] entry where there is one, else low with the switch
// low_effort_default on, else highest.
func effortFor(c config.Config, host, model string) string {
	if v, ok := c.EffortEntry(host, model); ok {
		return v
	}
	if LowEffortDefault.On(c) {
		return config.EffortLow
	}
	return config.EffortHighest
}

// usageOf is what a record spent: its input less the cached part, which a
// record an older release wrote does not have, the cached part, its output,
// its CLI runs and their time. A count below zero, which no release writes,
// counts as none.
func usageOf(r Record) cost.Usage {
	cached := int64(max(r.TokensCachedIn, 0))
	return cost.Usage{In: max(int64(r.TokensIn)-cached, 0), CachedIn: cached, Out: int64(max(r.TokensOut, 0)),
		Calls: int64(max(r.Calls, 0)), MS: max(r.DurationMS, 0)}
}

// Spent is what host's records of now's day spent, from records, a run
// log's, in any order.
func Spent(records []Record, host string, now time.Time) cost.Usage {
	today := now.Format(time.DateOnly)
	var u cost.Usage
	for _, r := range records {
		if r.Host == host && day(r.At) == today {
			u.Add(usageOf(r))
		}
	}
	return u
}

// OverBudget reports whether host's updates are stopped for the rest of
// now's day by its budget, as the job stops them: its passes that made a
// model call reached daily_cap, or, with the switch token_budget on, its
// weighted tokens reached the day's budget.
func OverBudget(c config.Config, records []Record, host string, now time.Time) bool {
	if runLog(records).calls(host, now.Format(time.DateOnly)) >= c.DailyCap {
		return true
	}
	return TokenBudget.On(c) && Spent(records, host, now).Weighted() >= c.TokenBudget()
}

// Week reads the run log of the 7 days that end on now's day and gives what
// host's updates spent over them (see WeekOf).
func Week(state, host string, now time.Time) (cost.Week, error) {
	records, err := Days(state, now, weekDays)
	return WeekOf(records, host, now), err
}

// WeekOf is what host's records of the 7 days that end on now's day spent:
// their usage; the passes that made a model call, how many of them found
// nothing to change and how many ran below the effort they asked for; and
// the sessions organized, those with a pass that saved a page or found
// nothing to change. A pass ran below its effort when its call answered at
// an effort lower in the host's table than the one it asked for, or with no
// effort flag at all.
func WeekOf(records []Record, host string, now time.Time) cost.Week {
	from := now.AddDate(0, 0, 1-weekDays).Format(time.DateOnly)
	to := now.Format(time.DateOnly)
	levels := []string{}
	if h, ok := lookupHost(host); ok {
		levels = h.efforts
	}
	var w cost.Week
	organized := map[string]bool{}
	for _, r := range records {
		if d := day(r.At); r.Host != host || d < from || d > to {
			continue
		}
		w.Add(usageOf(r))
		if success(r) {
			organized[r.Session] = true
		}
		if r.Calls <= 0 {
			continue
		}
		w.Passes++
		if r.Outcome == OutcomeNoChange {
			w.NoChange++
		}
		asked := slices.Index(levels, r.Effort)
		answered := success(r) || r.EffortUsed != ""
		if answered && asked >= 0 && slices.Index(levels, r.EffortUsed) < asked {
			w.BelowEffort++
		}
	}
	w.Organized = int64(len(organized))
	return w
}

// budgetWords says the day's weighted tokens against the budget, in
// thousands, as the job's log and the doctor give them.
func budgetWords(spent, budget int64) string {
	return fmt.Sprintf("about %d of %d thousand weighted tokens today", thousands(spent), thousands(budget))
}

// thousands is n in thousands, rounded to the nearest.
func thousands(n int64) int64 { return (n + 500) / 1000 }

// estimate is what a pass is taken to read before it runs: the rules, the
// batch and the schema, all of it uncached input (see cost.Estimate).
func estimate(p plan) cost.Usage {
	return cost.Estimate(len(Rules) + len(p.batch.Input) + len(Schema(p.thread.page)))
}

// overEstimate stops host for the rest of the job, with the switch
// token_budget on, when its weighted tokens today and the estimate of p,
// the next pass, would pass the day's budget. A pass that lands exactly on
// it runs. Off, it works out no estimate.
func (r *run) overEstimate(host string, p plan) string {
	if !TokenBudget.On(r.j.Config) {
		return ""
	}
	need := estimate(p)
	spent := Spent(r.log, host, r.j.now())
	budget := r.j.Config.TokenBudget()
	after := spent
	after.Add(need)
	if after.Weighted() <= budget {
		return ""
	}
	return r.stopHost(host, StopBudget, fmt.Sprintf("%s; the next pass needs about %d thousand", budgetWords(spent.Weighted(), budget), thousands(need.Weighted())))
}
