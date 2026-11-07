package health

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/heyNag/eva-md/internal/paths"
)

// putTimes writes an event's heartbeat as the hook writes it, with runs a
// minute apart ending at end, each taking the time given, in milliseconds;
// a time of 0 is a run from before times were recorded.
func putTimes(t *testing.T, d paths.Dirs, host, event string, end time.Time, times ...float64) {
	t.Helper()
	var runs []map[string]any
	for i, ms := range times {
		run := map[string]any{"at": end.Add(-time.Duration(len(times)-1-i) * time.Minute).Format(time.RFC3339Nano)}
		if ms > 0 {
			run["duration_ms"] = ms
		}
		runs = append(runs, run)
	}
	raw, _ := json.Marshal(map[string]any{"v": 1, "at": end.Format(time.RFC3339Nano), "host": host, "event": event, "transcript_available": true, "runs": runs})
	put(t, filepath.Join(d.State, "heartbeats", host, event+".json"), raw)
}

// The doctor's hook times row shows, for each event, the 95th percentile of
// the times its heartbeat keeps for the past 7 days, by the nearest rank:
// of 20 runs, the second slowest, rounded up to a whole millisecond. It
// leaves out runs older than a week, from the future or from before times
// were recorded, says what the times leave out, and is for information
// only: however slow, never FAIL. Before any timed run it is UNKNOWN.
func TestHookTimes(t *testing.T) {
	now := time.Now()
	d := isolated(t)
	if c := hookTimes(d, "codex", now); c.Status != "UNKNOWN" || c.Name != "codex hook times" || c.Fix != "" {
		t.Fatalf("no heartbeat: %+v", c)
	}
	putTimes(t, d, "codex", "Stop", now, 0, 0, 0)
	if c := hookTimes(d, "codex", now); c.Status != "UNKNOWN" {
		t.Fatalf("runs from before times were recorded: %+v", c)
	}
	var twenty []float64
	for i := 20; i >= 1; i-- {
		twenty = append(twenty, float64(i)+0.25)
	}
	putTimes(t, d, "codex", "Stop", now, twenty...)
	putTimes(t, d, "codex", "SessionStart", now.Add(-time.Hour), 95.5, 120.2, 80)
	// The two slow runs are 8 days old: the past week's p95 is 41 ms.
	putTimes(t, d, "codex", "UserPromptSubmit", now.Add(-8*24*time.Hour), 5000, 4000)
	putTimes(t, d, "codex", "PostToolUse", now, 0, 41, 12)
	future := now.Add(time.Hour)
	putTimes(t, d, "codex", "SessionEnd", future, 3000)
	c := hookTimes(d, "codex", now)
	want := "p95 of each event's runs in the past 7 days, without the process's start or the heartbeat's own save: SessionStart 121 ms (3 runs), PostToolUse 41 ms (2 runs), Stop 20 ms (20 runs)"
	if c.Status != "PASS" || c.Detail != want || c.Fix != "" {
		t.Fatalf("row=%+v\nwant %q", c, want)
	}
	putTimes(t, d, "codex", "Stop", now, 60000)
	if c := hookTimes(d, "codex", now); c.Status != "PASS" || !strings.Contains(c.Detail, "Stop 60000 ms (1 run)") {
		t.Fatalf("a slow run: %+v", c)
	}
	names := map[string]bool{}
	for _, c := range Inspect(context.Background(), d, false).Checks {
		names[c.Name] = true
	}
	if !names["claude-code hook times"] || !names["codex hook times"] {
		t.Fatalf("the doctor has no hook times rows: %v", names)
	}
}

// p95 takes the nearest rank: of 1 run that run, of 10 the slowest, of 20
// the second slowest, of 100 the 95th.
func TestP95(t *testing.T) {
	series := func(n int) []float64 {
		var s []float64
		for i := n; i >= 1; i-- {
			s = append(s, float64(i))
		}
		return s
	}
	for n, want := range map[int]int{1: 1, 10: 10, 19: 19, 20: 19, 21: 20, 100: 95} {
		if got := p95(series(n)); got != want {
			t.Errorf("p95 of 1..%d = %d, want %d", n, got, want)
		}
	}
	if got := p95([]float64{0.2}); got != 1 {
		t.Errorf("p95 of 0.2 ms = %d, want 1", got)
	}
}
