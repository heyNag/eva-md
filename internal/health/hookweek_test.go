package health

import (
	"context"
	"encoding/json"
	"path/filepath"
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/heyNag/eva-md/internal/paths"
)

// putWeek writes an event's heartbeat as the hook writes it, at, with the
// week given.
func putWeek(t *testing.T, d paths.Dirs, host, event string, at time.Time, week []HookDay) {
	t.Helper()
	raw, err := json.Marshal(map[string]any{"v": 1, "at": at.Format(time.RFC3339Nano), "host": host, "event": event, "transcript_available": true,
		"runs": []map[string]any{{"at": at.Format(time.RFC3339Nano)}}, "week": week})
	if err != nil {
		t.Fatal(err)
	}
	put(t, filepath.Join(d.State, "heartbeats", host, event+".json"), raw)
}

// day is a date days after now's, in UTC, as a week names it.
func day(now time.Time, days int) string {
	return now.UTC().AddDate(0, 0, days).Format(time.DateOnly)
}

// AddRun counts a run in its day, by the date in UTC: its time in its
// bucket, a bound counted in the bucket it closes; what it held back, by
// kind and by why; and what it delivered. A brief whose state can be stale
// or pending is a new chat's or a compaction's that delivered text in time,
// or a prompt's that carried the first brief; a resumed chat's is not one,
// and a first-prompt brief that was late or delivered nothing is no
// first-prompt brief. It keeps the days of the week
// ending today, oldest first, each once, and drops the others: older ones,
// ones after today, ones that aren't dates, a day's second copy. A day's
// counts of the wrong length are cut or filled.
func TestAddRunCountsARunInItsDay(t *testing.T) {
	now := time.Date(2026, 11, 11, 23, 30, 0, 0, time.FixedZone("ICT", 7*3600)) // 16:30 UTC
	week := AddRun(nil, now, HookRun{DurationMS: 50})
	if len(week) != 1 || week[0].Day != "2026-11-11" || week[0].Runs != 1 || week[0].MS[5] != 1 || len(week[0].MS) != 16 {
		t.Fatalf("first run: %+v", week)
	}
	week = AddRun(week, now, HookRun{DurationMS: 50.001, Failed: true, Held: []string{"prompt", "brief", "unknown"}, HeldBy: HeldByRecovery})
	week = AddRun(week, now, HookRun{DurationMS: 4000, Held: []string{"reply"}, HeldBy: HeldByOffRecord})
	week = AddRun(week, now, HookRun{DurationMS: 1, Delivered: &Delivery{Kind: "clear", Routes: []string{"rules", "state", "unknown"}, Chars: 120, Stale: true}})
	week = AddRun(week, now, HookRun{DurationMS: 1, Delivered: &Delivery{Kind: "prompt", Routes: []string{"recall"}, Chars: 30, Pending: true, Fallback: true}})
	week = AddRun(week, now, HookRun{DurationMS: 1, Delivered: &Delivery{Kind: "prompt", Pending: true}})
	week = AddRun(week, now, HookRun{DurationMS: 1, Failed: true, Delivered: &Delivery{Kind: "startup", Late: true, Stale: true}})
	week = AddRun(week, now, HookRun{DurationMS: 1, Delivered: &Delivery{Kind: "prompt", Fallback: true, Late: true}})
	week = AddRun(week, now, HookRun{DurationMS: 1, Delivered: &Delivery{Kind: "prompt", Fallback: true, Pending: true}})
	week = AddRun(week, now, HookRun{DurationMS: 1, Delivered: &Delivery{Kind: "resume", Routes: []string{"state"}, Chars: 50, Stale: true, Pending: true}})
	week = AddRun(week, now, HookRun{DurationMS: 1, Delivered: &Delivery{Kind: "startup", Routes: []string{"rules"}, Chars: 10, Pending: true}})
	want := HookDay{Day: "2026-11-11", Runs: 11, Failed: 2,
		MS:     []int{8, 0, 0, 0, 0, 1, 1, 0, 0, 0, 0, 0, 0, 0, 0, 1},
		Behind: []int{1, 0, 0, 1}, OffRecord: []int{0, 1, 0, 0},
		Kinds: []int{2, 1, 1, 0, 0, 4}, Added: 4, Chars: 210, Routes: []int{2, 0, 2, 0, 1, 0, 0},
		Briefs: 3, WithState: 1, Stale: 1, Pending: 2, StaleOrPending: 3, Fallback: 1, Late: 2}
	if len(week) != 1 || !reflect.DeepEqual(week[0], want) {
		t.Fatalf("day:\n%+v\nwant\n%+v", week, want)
	}

	old := []HookDay{
		{Day: "2026-11-05", Runs: 5, MS: []int{1, 2}},                     // the week's first day, its counts short
		{Day: "2026-11-04", Runs: 9},                                      // a day before the week
		{Day: "2026-11-12", Runs: 9},                                      // after today
		{Day: "2026-11-8", Runs: 9},                                       // no date as the hook writes one
		{Day: "yesterday", Runs: 9},                                       // no date
		{Day: "2026-11-10", Runs: 2, Routes: slices.Repeat([]int{1}, 20)}, // counts too long
		{Day: "2026-11-10", Runs: 9},                                      // the day's second copy
		{Day: "2026-11-11", Runs: 3},                                      // today's, out of order
	}
	week = AddRun(old, now, HookRun{DurationMS: 1})
	var days []string
	for _, d := range week {
		days = append(days, d.Day)
		if len(d.MS) != 16 || len(d.Behind) != 4 || len(d.OffRecord) != 4 || len(d.Kinds) != 6 || len(d.Routes) != 7 {
			t.Fatalf("day %s not fitted: %+v", d.Day, d)
		}
	}
	if !reflect.DeepEqual(days, []string{"2026-11-05", "2026-11-10", "2026-11-11"}) || week[0].Runs != 5 || week[0].MS[1] != 2 || week[1].Runs != 2 || week[1].Routes[6] != 1 || week[2].Runs != 4 {
		t.Fatalf("week %+v", week)
	}
	// A week later, only today's run is left.
	if week = AddRun(week, now.AddDate(0, 0, 7), HookRun{}); len(week) != 1 || week[0].Day != "2026-11-18" || week[0].Runs != 1 {
		t.Fatalf("a week later %+v", week)
	}
}

// The two rows sum the weeks of every event's heartbeat on both hosts, over
// the 7 days ending today, and say exactly what was held back and
// delivered. Days outside the week, a day's second copy and heartbeats that
// aren't valid add nothing.
func TestHeldAndDeliveredRows(t *testing.T) {
	now := time.Now()
	d := isolated(t)
	fit := func(day HookDay) HookDay { return day.fit() }
	prompt := fit(HookDay{Day: day(now, 0), Runs: 40, Behind: []int{3, 0, 0, 1}, OffRecord: []int{2},
		Kinds: []int{0, 0, 0, 0, 0, 30}, Added: 20, Chars: 4000, Routes: []int{1, 0, 1, 0, 6, 4, 10}, Briefs: 2, WithState: 1, Stale: 1, StaleOrPending: 1, Fallback: 2, Late: 1})
	putWeek(t, d, "claude-code", "UserPromptSubmit", now, []HookDay{
		fit(HookDay{Day: day(now, -7), Runs: 1000, Behind: []int{1000}}), // before the week
		prompt,
		fit(HookDay{Day: day(now, 0), Runs: 1000, Behind: []int{1000}}), // a second copy
	})
	putWeek(t, d, "claude-code", "Stop", now, []HookDay{fit(HookDay{Day: day(now, -6), Runs: 50, Behind: []int{0, 4}})})
	putWeek(t, d, "codex", "SessionStart", now, []HookDay{fit(HookDay{Day: day(now, -1), Runs: 10, Behind: []int{0, 0, 0, 2},
		Kinds: []int{6, 2, 1, 0, 1, 0}, Added: 9, Chars: 30000, Routes: []int{5, 3, 8, 9, 0, 6, 0}, Briefs: 9, WithState: 7, Stale: 2, Pending: 3, StaleOrPending: 4, Late: 1})})
	putWeek(t, d, "codex", "PostToolUse", now, []HookDay{fit(HookDay{Day: day(now, -2), Runs: 300, Behind: []int{0, 0, 7}})})
	// Not valid: a heartbeat of another host's event, and one from the future.
	putWeek(t, d, "codex", "PostToolUseFailure", now, []HookDay{fit(HookDay{Day: day(now, 0), Runs: 1000, Behind: []int{1000}})})
	putWeek(t, d, "codex", "Stop", now.Add(time.Hour), []HookDay{fit(HookDay{Day: day(now, 0), Runs: 1000, Behind: []int{1000}})})

	held, ok := heldCheck(d, now)
	want := Check{Name: "hooks held back", Status: "PASS", Detail: "of 400 hook runs in the past 7 days: held back while transcript recovery was behind: 3 prompts, 4 replies, 7 tool lines, 3 briefs; while a chat was off the record: 2 prompts (a brief still owed is counted at each run that held it back)"}
	if !ok || held != want {
		t.Fatalf("held row\n%+v\nwant\n%+v", held, want)
	}
	delivered, ok := deliveredCheck(d, now)
	want = Check{Name: "hooks delivered", Status: "PASS", Detail: "of 40 hook runs in the past 7 days that could add text to a chat (6 startups, 2 clears, 1 resume, 1 compaction, 30 prompts), 29 added 34000 characters; " +
		"runs that added each: rules 6, constraints 3, state 9, exchanges 9, recalled statements 6, pointers 10, reminder 10; " +
		"of 11 briefs given at a new chat's start, a compaction or a chat's first prompt (resumed and forked chats left out), 8 showed the thread's state, and 5 (45% of the 11) showed a state older than the newest captured exchange or an update pending (3 stale, 3 pending); " +
		"2 briefs came with the chat's first prompt; 2 runs passed the deadline and delivered nothing"}
	if !ok || delivered != want {
		t.Fatalf("delivered row\n%+v\nwant\n%+v", delivered, want)
	}

	// Both rows come after the hooks' rows, in status and the doctor.
	for _, r := range []Report{Status(context.Background(), d), Inspect(context.Background(), d, false)} {
		var names []string
		for _, c := range r.Checks {
			names = append(names, c.Name)
		}
		at := slices.Index(names, "hooks held back")
		if at < 1 || names[at-1] != "codex hook times" || names[at+1] != "hooks delivered" || slices.Index(names, "hooks delivered") != at+1 {
			t.Fatalf("rows out of place: %q", names)
		}
	}
}

// A week with nothing held back says so; one whose holds were all off the
// record says recovery held nothing back; one with no run that could add
// text gives no delivered row; and with no week, or one whose days are all
// outside it, neither row is given, as with heartbeats from a hook that
// kept no week.
func TestRowsWithLittleToSay(t *testing.T) {
	now := time.Now()
	d := isolated(t)
	if _, ok := heldCheck(d, now); ok {
		t.Fatal("held row without a heartbeat")
	}
	putTimes(t, d, "codex", "Stop", now, 12, 15)
	putWeek(t, d, "codex", "PreCompact", now, []HookDay{{Day: day(now, -7), Runs: 4}, {Day: day(now, 1), Runs: 4}})
	if _, ok := heldCheck(d, now); ok {
		t.Fatal("held row without a week")
	}
	if _, ok := deliveredCheck(d, now); ok {
		t.Fatal("delivered row without a week")
	}
	// A day that counts no run is no day of the week, whatever else it
	// counts.
	putWeek(t, d, "codex", "UserPromptSubmit", now, []HookDay{{Day: day(now, 0), Behind: []int{1}, Kinds: []int{0, 0, 0, 0, 0, 1}}})
	if c, ok := heldCheck(d, now); ok {
		t.Fatalf("held row from a day of no runs: %+v", c)
	}
	if c, ok := deliveredCheck(d, now); ok {
		t.Fatalf("delivered row from a day of no runs: %+v", c)
	}
	putWeek(t, d, "codex", "Stop", now, []HookDay{{Day: day(now, 0), Runs: 1}})
	if c, ok := heldCheck(d, now); !ok || c.Detail != "of 1 hook run in the past 7 days: none held anything back" {
		t.Fatalf("held row %+v", c)
	}
	if _, ok := deliveredCheck(d, now); ok {
		t.Fatal("delivered row without a run that could deliver")
	}
	putWeek(t, d, "codex", "Stop", now, []HookDay{{Day: day(now, 0), Runs: 2, OffRecord: []int{0, 2}}})
	if c, ok := heldCheck(d, now); !ok || c.Detail != "of 2 hook runs in the past 7 days: nothing held back while transcript recovery was behind; held back while a chat was off the record: 2 replies" {
		t.Fatalf("held row %+v", c)
	}
	putWeek(t, d, "codex", "UserPromptSubmit", now, []HookDay{{Day: day(now, 0), Runs: 1, Kinds: []int{0, 0, 0, 0, 0, 1}}})
	if c, ok := deliveredCheck(d, now); !ok || c.Detail != "of 1 hook run in the past 7 days that could add text to a chat (1 prompt), 0 added 0 characters" {
		t.Fatalf("delivered row %+v", c)
	}
}
