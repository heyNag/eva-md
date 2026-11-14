package health

import (
	"encoding/json"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/heyNag/eva-md/internal/config"
	"github.com/heyNag/eva-md/internal/inbox"
	"github.com/heyNag/eva-md/internal/paths"
	"github.com/heyNag/eva-md/internal/update"
)

// costMarker is planted in the session ids, projects, branches, models
// and notes of the cost fixtures' records, and in an inbox entry; no line
// the updates row or the cost section prints, and none of the JSON, may
// hold it.
const costMarker = "zqcostplanted"

// plantCostEntry writes an inbox entry holding the marker for a session the
// records name, so a reader of the inbox would find it.
func plantCostEntry(t *testing.T, d paths.Dirs, host, session string) {
	t.Helper()
	data, err := json.Marshal(inbox.Entry{V: 1, TS: clock.Format(time.RFC3339), Host: host, Session: session, Role: "user", Kind: "prompt",
		Text: "Plan the " + costMarker + " step.", Project: costMarker, Branch: costMarker, Origin: "hook"})
	if err != nil {
		t.Fatal(err)
	}
	put(t, inbox.Store{Root: d.Inbox()}.Path(costMarker, host, session), append(data, '\n'))
}

// With the switch token_budget on, each host's updates row adds what its
// updates spent today in weighted tokens against the day's budget, from
// today's records only; off, the row is as before. Neither shows the
// marker planted in the records and entries.
func TestTheUpdatesRowSaysTheDaysTokens(t *testing.T) {
	session := "3f2a91c0-" + costMarker
	for _, tc := range []struct {
		name   string
		on     bool
		budget int64
		want   string
	}{
		{"off", false, 10000, ""},
		{"on", true, 10000, "; about 4 thousand weighted tokens today, 36% of the day's budget"},
		{"on, the default budget", true, 0, "; about 4 thousand weighted tokens today, 0% of the day's budget"},
		{"on, the budget reached", true, 3600, "; about 4 thousand weighted tokens today, 100% of the day's budget"},
		{"on, one token short of it", true, 3601, "; about 4 thousand weighted tokens today, 99% of the day's budget"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := isolated(t)
			c := wikiConfig(t)
			c.DailyTokenBudget = tc.budget
			if tc.on {
				c.Switches = map[string]any{"token_budget": true}
			}
			fakeOnPath(t, "codex")
			put(t, filepath.Join(d.Share, "marketplace", "codex", ".eva-md-managed"), []byte("eva-md marketplace v1\n"))
			plantCostEntry(t, d, update.HostCodex, session)
			mark := func(r update.Record) update.Record {
				r.Project, r.Thread, r.Model, r.ModelReported, r.Degraded = costMarker, costMarker, costMarker, costMarker, costMarker
				return r
			}
			ok := mark(okRecord(ago(2*time.Hour), update.HostCodex, session))
			ok.TokensIn, ok.TokensCachedIn, ok.TokensOut = 3000, 1000, 200
			failed := mark(failedRecord(ago(time.Hour), update.HostCodex, session, update.ReasonRateLimit))
			failed.TokensIn = 500
			// Yesterday's, and the other host's, count for nothing today.
			old := mark(okRecord(clock.AddDate(0, 0, -1).Format(time.RFC3339), update.HostCodex, session))
			old.TokensIn = 900000
			other := mark(okRecord(ago(time.Hour), update.HostClaude, session))
			other.TokensIn = 900000
			plantRunLog(t, d, old, ok, failed, other)
			var r Report
			inspectUpdates(&r, d, c, clock)
			row := findRow(t, r, "codex updates")
			if !strings.HasSuffix(row.Detail, "2 of 40 model calls today"+tc.want) {
				t.Fatalf("detail %q, want it to end %q", row.Detail, "2 of 40 model calls today"+tc.want)
			}
			if !tc.on && strings.Contains(row.Detail, "weighted") {
				t.Fatalf("switch off: %q", row.Detail)
			}
			for _, check := range r.Checks {
				if strings.Contains(check.Detail+check.Fix, costMarker) {
					t.Fatalf("a row shows the marker: %+v", check)
				}
			}
		})
	}
}

// costFixture plants a run log of 8 days for eva-md status --updates, with
// and without the cached field, every record carrying the marker, and an
// inbox entry holding it, and returns the day it ends on.
func costFixture(t *testing.T, d paths.Dirs) time.Time {
	t.Helper()
	now := time.Date(2026, 1, 14, 12, 0, 0, 0, time.UTC)
	at := func(daysAgo int) string { return now.AddDate(0, 0, -daysAgo).Add(-time.Hour).Format(time.RFC3339) }
	rec := func(daysAgo int, session, outcome, reason string, calls, in, cached, out int, effort, used string) update.Record {
		return update.Record{At: at(daysAgo), Host: update.HostClaude, Session: session + "-" + costMarker, Project: costMarker, Thread: costMarker,
			Model: costMarker, ModelReported: costMarker, Degraded: costMarker, Outcome: outcome, Reason: reason, Calls: calls,
			TokensIn: in, TokensCachedIn: cached, TokensOut: out, Effort: effort, EffortUsed: used, DurationMS: 1000}
	}
	plantRunLog(t, d,
		rec(0, "3f2a91c0", update.OutcomeOK, "", 1, 4000, 3000, 300, "max", "max"),
		rec(0, "4a3b92d1", update.OutcomeNoChange, "", 1, 1000, 0, 100, "max", "high"),
		rec(2, "5b4ca3e2", update.OutcomeFailed, update.ReasonInvalidOutput, 2, 600, 100, 60, "max", "max"),
		// The 8th day, outside the 7 the report reads.
		rec(7, "6c5db4f3", update.OutcomeOK, "", 1, 900000, 0, 900000, "max", "max"),
	)
	plantCostEntry(t, d, update.HostClaude, "3f2a91c0-"+costMarker)
	return now
}

// With the switch token_budget on, eva-md status --updates ends with a
// line a host of what its updates spent over the 7 days: each token count
// and the weighted sum, model calls and passes, chats organized and the
// weighted tokens per chat, and the shares unchanged and below the effort
// asked. Its JSON is as before, on or off, since the field report, which
// carries these figures, has a format whose keys are pinned. Off, the
// report is as before, line for line. Neither shows the planted marker.
func TestTheUpdateReportsCostSection(t *testing.T) {
	want := []string{
		"cost, claude-code: tokens 2500 in, 3100 cached in, 460 out, 5110 weighted; 4 model calls in 3 passes; 2 chats organized, 2555 weighted tokens a chat; unchanged 33%, below the effort asked 33%",
		"cost, codex: tokens 0 in, 0 cached in, 0 out, 0 weighted; 0 model calls in 0 passes; 0 chats organized, 0 weighted tokens a chat; unchanged none, below the effort asked none",
	}
	var offLines []string
	var offJSON []byte
	for _, on := range []bool{false, true} {
		d := isolated(t)
		now := costFixture(t, d)
		c := config.Default()
		if on {
			c.Switches = map[string]any{"token_budget": true}
		}
		if err := config.Save(d.ConfigFile(), c); err != nil {
			t.Fatal(err)
		}
		f, err := Updates(d, now)
		if err != nil {
			t.Fatal(err)
		}
		lines := f.Lines()
		data, err := json.Marshal(f)
		if err != nil {
			t.Fatal(err)
		}
		for _, out := range []string{strings.Join(lines, "\n"), string(data)} {
			if strings.Contains(out, costMarker) || strings.Contains(out, "3f2a91c0") {
				t.Fatalf("on %v: the report shows the marker:\n%s", on, out)
			}
		}
		if !on {
			if f.Cost != nil || strings.Contains(string(data), `"cost"`) || strings.Contains(strings.Join(lines, "\n"), "cost, ") {
				t.Fatalf("switch off: %+v\n%s", f.Cost, strings.Join(lines, "\n"))
			}
			offLines, offJSON = lines, data
			continue
		}
		if !reflect.DeepEqual(lines[:len(lines)-2], offLines) || !reflect.DeepEqual(lines[len(lines)-2:], want) {
			t.Fatalf("report:\n%s\nwant the report with the switch off and:\n%s", strings.Join(lines, "\n"), strings.Join(want, "\n"))
		}
		wantCost := []HostCost{
			{Host: update.HostClaude, TokensIn: 2500, CachedIn: 3100, TokensOut: 460, Weighted: 5110, Calls: 4, Passes: 3, Organized: 2, PerChat: 2555, NoChange: 1, BelowEffort: 1},
			{Host: update.HostCodex},
		}
		if !reflect.DeepEqual(f.Cost, wantCost) {
			t.Fatalf("cost %+v", f.Cost)
		}
		if string(data) != string(offJSON) {
			t.Fatalf("the JSON differs with the switch on:\n%s\n%s", offJSON, data)
		}
	}
}

// budgetDetail rounds the tokens to the nearest thousand and the share
// down, so it says 100% only once the budget is reached.
func TestBudgetDetailRounds(t *testing.T) {
	on := config.Default()
	on.Switches = map[string]any{"token_budget": true}
	on.DailyTokenBudget = 2000
	now := time.Date(2026, 1, 14, 12, 0, 0, 0, time.UTC)
	for in, want := range map[int]string{
		0:    "; about 0 thousand weighted tokens today, 0% of the day's budget",
		499:  "; about 0 thousand weighted tokens today, 24% of the day's budget",
		500:  "; about 1 thousand weighted tokens today, 25% of the day's budget",
		1999: "; about 2 thousand weighted tokens today, 99% of the day's budget",
		2000: "; about 2 thousand weighted tokens today, 100% of the day's budget",
		5000: "; about 5 thousand weighted tokens today, 250% of the day's budget",
	} {
		records := []update.Record{{At: now.Format(time.RFC3339), Host: update.HostCodex, Outcome: update.OutcomeOK, Calls: 1, TokensIn: in}}
		if got := budgetDetail(on, records, update.HostCodex, now); got != want {
			t.Errorf("%d tokens: %q, want %q", in, got, want)
		}
		if got := budgetDetail(config.Default(), records, update.HostCodex, now); got != "" {
			t.Errorf("switch off, %d tokens: %q", in, got)
		}
	}
}
