package health

import (
	"fmt"
	"math"
	"slices"
	"strings"
	"time"

	"github.com/heyNag/eva-md/internal/paths"
)

// hookTimes is the doctor's row of how long a host's hooks take: for each
// event, the 95th percentile of the times its heartbeat keeps for its runs
// of the past 7 days, at most its last 20. A run's time goes from the start
// of the hook's work to just before its heartbeat is saved, so it leaves out
// the process's start and the save itself, an atomic write with fsync, and
// reads lower than the benchmark's, which counts both. The row is for
// information only and never FAIL: PASS with the times, or UNKNOWN before
// any run recorded one.
func hookTimes(d paths.Dirs, host string, now time.Time) Check {
	check := Check{Name: host + " hook times", Status: "UNKNOWN", Detail: "no hook run with its time recorded in the past 7 days"}
	var parts []string
	for _, event := range hostEvents(host) {
		b, valid := readBeat(d, host, event, now)
		if !valid {
			continue
		}
		var times []float64
		for _, r := range b.Runs {
			if r.DurationMS > 0 && !r.At.IsZero() && !r.At.After(now.Add(time.Minute)) && now.Sub(r.At) <= hookDays {
				times = append(times, r.DurationMS)
			}
		}
		if len(times) > 0 {
			parts = append(parts, fmt.Sprintf("%s %d ms (%d %s)", event, p95(times), len(times), plural(len(times), "run", "runs")))
		}
	}
	if len(parts) > 0 {
		check.Status = "PASS"
		check.Detail = "p95 of each event's runs in the past 7 days, without the process's start or the heartbeat's own save: " + strings.Join(parts, ", ")
	}
	return check
}

// p95 is the 95th percentile of times by the nearest rank, in whole
// milliseconds rounded up: of 20 runs, the second slowest.
func p95(times []float64) int {
	slices.Sort(times)
	return int(math.Ceil(times[(len(times)*95+99)/100-1]))
}
