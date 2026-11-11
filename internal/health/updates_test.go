package health

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/heyNag/eva-md/internal/config"
	"github.com/heyNag/eva-md/internal/paths"
	"github.com/heyNag/eva-md/internal/update"
)

// updatesMarker is a made-up marker planted in every text field of the
// fixture run log, which the report must never show.
const updatesMarker = "zq-planted-marker-7f3k"

// namedMarker is a made-up marker in the shape of a name the job writes,
// planted where the report shows names, which it must never show either.
const namedMarker = "zqplanted_marker"

// updatesFixture writes a week's run log, and a day before it, and a config
// with a daily cap of 12, and returns the time the report is made at and
// the session ids the log holds.
func updatesFixture(t *testing.T, d paths.Dirs) (time.Time, []string) {
	t.Helper()
	zone := time.FixedZone("", 3600)
	now := time.Date(2026, 1, 14, 12, 0, 0, 0, zone)
	at := func(day, minute int) string {
		return time.Date(2026, 1, day, 9, minute, 0, 0, zone).Format(time.RFC3339)
	}
	m := updatesMarker
	sessions := []string{"3f2a91c0-" + m + "-1", "3f2a91c0-" + m + "-2", "3f2a91c0-" + m + "-3"}
	n := 0
	text := func(r update.Record) update.Record {
		r.Project, r.Thread, r.ModelNote, r.Degraded = "github.com__you__"+m, "feature/"+m, "fallback from "+m, "an optional "+m+" failed"
		r.Session = sessions[n%len(sessions)]
		n++
		return r
	}
	days := map[string][]update.Record{
		"2026-01-14": {
			text(update.Record{At: at(14, 1), Host: update.HostClaude, Outcome: update.OutcomeOK, Calls: 1, TokensIn: 1000, TokensOut: 100, DurationMS: 30000, Pages: 2, BoundaryToSavedMS: 600000,
				Trigger: update.TriggerEnd, Model: "claude-a[1m]", ModelReported: "claude-a", Validation: &update.Report{Refused: 2, Rules: update.RuleCounts{Refused: 1}}}),
			text(update.Record{At: at(14, 2), Host: update.HostCodex, Outcome: update.OutcomeNoChange, Calls: 1, TokensIn: 2000, TokensOut: 200, DurationMS: 60000,
				Trigger: update.TriggerGap, Model: m, Validation: &update.Report{Duplicates: 1, KeptNow: true}}),
			text(update.Record{At: at(14, 3), Host: update.HostClaude, Outcome: update.OutcomeFailed, Reason: update.ReasonInvalidOutput, Calls: 1, TokensIn: 3000, TokensOut: 300, DurationMS: 90000,
				Trigger: update.TriggerStop, Model: "claude-a", ModelReported: "claude-b", Rejection: &update.Rejection{Rule: update.RuleMissingKey, Place: "output.last_chat"}}),
			text(update.Record{At: at(14, 4), Host: update.HostClaude, Outcome: update.OutcomeSkipped, Reason: update.ReasonSmall, Trigger: update.TriggerStart}),
			{At: at(14, 5), Outcome: update.OutcomeBusy, Trigger: update.TriggerStart, SessionHashes: []string{"0123456789abcdef", "fedcba9876543210"}},
			{At: at(14, 6), Outcome: update.OutcomeBusy, Trigger: update.TriggerSweep},
		},
		"2026-01-13": {
			text(update.Record{At: at(13, 1), Host: update.HostClaude, Outcome: update.OutcomeOK, Calls: 1, TokensIn: 4000, TokensOut: 400, DurationMS: 120000, Pages: 1, BoundaryToSavedMS: 3600000,
				Trigger: update.TriggerSweep, Validation: &update.Report{}}),
			{At: at(13, 3), Outcome: update.OutcomeBusy, Trigger: m, SessionHashes: []string{"0123456789abcdef"}},
			text(update.Record{At: at(13, 4), Host: update.HostCodex, Outcome: update.OutcomeFailed, Reason: update.ReasonRateLimit, Calls: 1, DurationMS: 1000, Trigger: update.TriggerEnd}),
		},
		"2026-01-12": {
			text(update.Record{At: at(12, 1), Host: update.HostClaude, Outcome: update.OutcomeSkipped, Reason: m}),
			// Two calls in one pass count once against the cap, as the
			// budget counts them, and a pass that saved no page has no time
			// to memory. It asked for the host's default.
			text(update.Record{At: at(12, 2), Host: update.HostClaude, Outcome: update.OutcomeOK, Calls: 2, TokensIn: 500, TokensOut: 50, DurationMS: 20000, BoundaryToSavedMS: 999,
				Trigger: update.TriggerGap, ModelReported: "claude-q", Validation: &update.Report{}}),
			// Names in the shape of the job's own, which it never writes.
			text(update.Record{At: at(12, 3), Host: update.HostClaude, Outcome: update.OutcomeFailed, Reason: update.ReasonInvalidOutput, Calls: 1, DurationMS: 10000,
				Trigger: namedMarker, Rejection: &update.Rejection{Rule: namedMarker, Place: "items[]." + namedMarker}}),
			text(update.Record{At: at(12, 4), Host: update.HostClaude, Outcome: update.OutcomeSkipped, Reason: namedMarker}),
			{At: at(12, 5), Outcome: update.OutcomeBusy, Trigger: namedMarker},
		},
		// The day before the week: not read.
		"2026-01-07": {
			text(update.Record{At: at(7, 1), Host: update.HostClaude, Outcome: update.OutcomeOK, Calls: 1, TokensIn: 99999, Pages: 1, BoundaryToSavedMS: 1, Trigger: update.TriggerEnd}),
		},
	}
	for day, records := range days {
		// Busy exits go to the day's file of them.
		var b, busy strings.Builder
		for _, r := range records {
			data, err := json.Marshal(r)
			if err != nil {
				t.Fatal(err)
			}
			if r.Outcome == update.OutcomeBusy {
				busy.Write(append(data, '\n'))
				continue
			}
			b.Write(append(data, '\n'))
		}
		if busy.Len() > 0 {
			put(t, filepath.Join(d.State, "updates", "busy-"+day+".jsonl"), []byte(busy.String()))
		}
		if day == "2026-01-13" {
			// A record of an older release: no trigger, no reported model.
			b.WriteString(`{"at":"` + at(13, 2) + `","host":"claude-code","session":"` + sessions[0] + `","project":"` + m + `","thread":"` + m + `","outcome":"ok","chunk_bytes":10,"pages":1,"duration_ms":150000,"tokens_in":5000,"tokens_out":500,"model":"","effort":"max","effort_used":"max","rules_version":3,"boundary_to_saved_ms":5400000,"calls":1}` + "\n")
			// A line that doesn't decode, and one of text alone.
			b.WriteString("{\"at\":\"" + m + "\n" + m + "\n")
		}
		put(t, filepath.Join(d.State, "updates", day+".jsonl"), []byte(b.String()))
	}
	c := config.Default()
	c.DailyCap = 12
	if err := config.Save(d.ConfigFile(), c); err != nil {
		t.Fatal(err)
	}
	return now, sessions
}

// On a fixture run log the report gives exact counts: outcomes and reasons,
// whole refusals by rule and place, what the checker did inside accepted
// answers, tokens and call times, the time from a chat's boundary to
// memory on file by trigger with the sweep apart and an older release's
// records as unknown, busy exits, and each day's calls against the cap. It
// shows no session, project, branch, model or other text the log holds.
func TestTheUpdateReportCounts(t *testing.T) {
	d := isolated(t)
	now, sessions := updatesFixture(t, d)
	f, err := Updates(d, now)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"updates from 2026-01-08 to 2026-01-14: 11 passes recorded",
		"outcomes: 4 ok, 1 no_change, 3 skipped, 3 failed",
		"skips by reason: 2 other, 1 small",
		"failures by reason: 2 invalid_output, 1 rate_limit",
		"answers refused whole: 2 of 6 checked; by rule: 1 missing_key at output.last_chat, 1 other at other",
		"inside accepted answers: 1 duplicates, 1 kept_now, 2 refused, 1 rules.refused",
		"tokens in per call: median 1000, p95 5000, max 5000 (8)",
		"tokens out per call: median 100, p95 500, max 500 (8)",
		"time per call: median 30.0 s, p95 150.0 s, max 150.0 s (8)",
		"chat boundary to memory on file, by trigger: session_end median 10.0 min, p95 10.0 min, max 10.0 min (1); unknown median 90.0 min, p95 90.0 min, max 90.0 min (1)",
		"chat boundary to memory on file, the day's sweep: median 60.0 min, p95 60.0 min, max 60.0 min (1)",
		"busy exits: 4, for 2 sessions; by trigger: 1 chat_start, 2 other, 1 sweep",
		"model calls a day, each host against the cap: 2026-01-08 claude-code 0, codex 0 of 12; 2026-01-09 claude-code 0, codex 0 of 12; 2026-01-10 claude-code 0, codex 0 of 12; " +
			"2026-01-11 claude-code 0, codex 0 of 12; 2026-01-12 claude-code 2, codex 0 of 12; 2026-01-13 claude-code 2, codex 1 of 12; 2026-01-14 claude-code 2, codex 1 of 12",
		"model reported: 3 passes named the model that answered; of them 1 asked for the host's default and 1 named a model other than the one asked for",
	}
	got := f.Lines()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("report:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	data, err := json.Marshal(f)
	if err != nil {
		t.Fatal(err)
	}
	for _, out := range []string{strings.Join(got, "\n"), string(data)} {
		for _, s := range append([]string{updatesMarker, namedMarker, "zqplanted", "3f2a91c0", "claude-a", "claude-b", "claude-q", "github.com", "feature/", d.State, os.Getenv("HOME")}, sessions...) {
			if strings.Contains(out, s) {
				t.Errorf("the report shows %q:\n%s", s, out)
			}
		}
	}
	// The JSON holds the same figures.
	var back UpdateFigures
	if err := json.Unmarshal(data, &back); err != nil || !reflect.DeepEqual(back, f) {
		t.Fatalf("JSON %s: %v", data, err)
	}
	if f.Busy.Jobs != 4 || f.Busy.Sessions != 2 || f.ToMemoryMS[update.TriggerSweep] != (Spread{N: 1, Median: 3600000, P95: 3600000, Max: 3600000}) || f.Calls[6].Hosts[update.HostClaude] != 2 {
		t.Fatalf("figures %+v", f)
	}
}

// With no run log the report is all zeros and nones, and reads no further
// than the run log and the config.
func TestTheUpdateReportWithNothing(t *testing.T) {
	d := isolated(t)
	now := time.Date(2026, 1, 14, 12, 0, 0, 0, time.UTC)
	f, err := Updates(d, now)
	if err != nil {
		t.Fatal(err)
	}
	lines := f.Lines()
	if lines[0] != "updates from 2026-01-08 to 2026-01-14: 0 passes recorded" || lines[1] != "outcomes: none" || lines[6] != "tokens in per call: none" ||
		lines[11] != "busy exits: 0, for 0 sessions; by trigger: none" || !strings.HasSuffix(lines[12], "2026-01-14 claude-code 0, codex 0 of 40") {
		t.Fatalf("report:\n%s", strings.Join(lines, "\n"))
	}
	if _, err := os.Stat(d.State); !os.IsNotExist(err) {
		t.Fatalf("the report made the state folder: %v", err)
	}
}

// The percentiles are by the nearest rank.
func TestSpread(t *testing.T) {
	values := make([]int64, 20)
	for i := range values {
		values[i] = int64(20 - i)
	}
	if got := spread(values); got != (Spread{N: 20, Median: 10, P95: 19, Max: 20}) {
		t.Fatalf("%+v", got)
	}
	if got := spread([]int64{7}); got != (Spread{N: 1, Median: 7, P95: 7, Max: 7}) {
		t.Fatalf("%+v", got)
	}
	if got := spread(nil); got != (Spread{}) {
		t.Fatalf("%+v", got)
	}
}

// Only the names and places this release writes reach the report, each
// where it belongs.
func TestSafeName(t *testing.T) {
	for _, tc := range []struct {
		in    string
		known []string
		want  string
	}{
		{"session_end", update.Triggers, "session_end"}, {"small", update.Reasons, "small"}, {"busy", update.Outcomes, "busy"},
		{"carried", update.RefusalRules, "carried"}, {"small", update.Triggers, "other"}, {"session_end", update.Reasons, "other"},
		{"", update.Triggers, "other"}, {namedMarker, update.Reasons, "other"}, {"acme_payments", update.Reasons, "other"},
		{"customer.portal", update.Outcomes, "other"}, {"Has Space", update.Reasons, "other"}, {"/Users/x", update.Triggers, "other"},
	} {
		if got := safeName(tc.in, tc.known); got != tc.want {
			t.Errorf("safeName(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
	for _, tc := range []struct{ in, want string }{
		{"output", "output"}, {"items[].kind", "items[].kind"}, {"output.last_chat", "output.last_chat"}, {"knowledge[].paths[]", "knowledge[].paths[]"},
		{"timeline[].refs[]", "timeline[].refs[]"}, {"items", "items"},
		{"", "other"}, {"items[]." + namedMarker, "other"}, {"customer.portal", "other"}, {"items[3].kind", "other"}, {"output.output", "other"},
		{"items[].kind.", "other"}, {"items]", "other"}, {"items[]x", "other"},
	} {
		if got := safePlace(tc.in); got != tc.want {
			t.Errorf("safePlace(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
