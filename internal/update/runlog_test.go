package update

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/heyNag/eva-md/internal/okfadapter"
	"github.com/heyNag/eva-md/internal/paths"
)

func at(minute int) string {
	return time.Date(2026, 1, 14, 9, minute, 0, 0, paris).Format(time.RFC3339)
}

// The start effort follows the runs that asked at least as much: a refusal
// steps it down, a run asked at that step and refused again steps it down
// further, a later run at a higher effort replaces it, and a lower
// configured effort is asked as it is. Another model, another day or a
// failed run changes nothing.
func TestStartEffort(t *testing.T) {
	ok := func(minute int, asked, used string) Record {
		return Record{At: at(minute), Host: HostClaude, Model: "m", Outcome: OutcomeOK, Effort: asked, EffortUsed: used}
	}
	for _, tc := range []struct {
		name string
		want string
		log  runLog
		got  string
	}{
		{"nothing yet", "max", nil, "max"},
		{"refused max", "max", runLog{ok(1, "max", "xhigh")}, "xhigh"},
		{"stays there", "max", runLog{ok(1, "max", "xhigh"), ok(2, "xhigh", "xhigh")}, "xhigh"},
		{"refused again", "max", runLog{ok(1, "max", "xhigh"), ok(2, "xhigh", "high")}, "high"},
		{"higher replaces", "max", runLog{ok(1, "max", "xhigh"), ok(2, "max", "max")}, "max"},
		{"lower configured", "low", runLog{ok(1, "max", "xhigh")}, "low"},
		{"refused low only", "high", runLog{ok(1, "low", "")}, "high"},
		{"no flag at all", "max", runLog{ok(1, "max", "")}, "low"},
		{"another model", "max", runLog{{At: at(1), Host: HostClaude, Model: "other", Outcome: OutcomeOK, Effort: "max", EffortUsed: "low"}}, "max"},
		{"another day", "max", runLog{{At: "2026-01-13T09:00:00+01:00", Host: HostClaude, Model: "m", Outcome: OutcomeOK, Effort: "max", EffortUsed: "low"}}, "max"},
		{"failed run", "max", runLog{{At: at(1), Host: HostClaude, Model: "m", Outcome: OutcomeFailed, Reason: ReasonTimeout, Effort: "max"}}, "max"},
	} {
		if got := tc.log.startEffort(claudeEfforts, tc.want, HostClaude, "m", "2026-01-14"); got != tc.got {
			t.Errorf("%s: %q, want %q", tc.name, got, tc.got)
		}
	}
}

// Backoff doubles from 5 minutes for each rate_limit, host_error,
// wiki_error or cli_missing in a row, up to 60; a call that ended in any
// other way ends the row, and a skip, which made no call, is passed over. A
// missing CLI made none either, but counts.
func TestBackoffUntil(t *testing.T) {
	fail := func(minute int, reason string, calls int) Record {
		return Record{At: at(minute), Host: HostCodex, Outcome: OutcomeFailed, Reason: reason, Calls: calls}
	}
	base := time.Date(2026, 1, 14, 9, 0, 0, 0, paris)
	for _, tc := range []struct {
		name string
		log  runLog
		wait time.Duration // after the newest failure; 0 for none
	}{
		{"none", nil, 0},
		{"one", runLog{fail(10, ReasonRateLimit, 1)}, 5 * time.Minute},
		{"two", runLog{fail(10, ReasonHostError, 1), fail(20, ReasonRateLimit, 1)}, 10 * time.Minute},
		{"wiki error", runLog{fail(10, ReasonWikiError, 1), fail(20, ReasonWikiError, 1), fail(30, ReasonWikiError, 1)}, 20 * time.Minute},
		{"conflict ends it", runLog{fail(10, ReasonRateLimit, 1), fail(20, ReasonConflict, 1)}, 0},
		{"capped", runLog{fail(1, ReasonRateLimit, 1), fail(2, ReasonRateLimit, 1), fail(3, ReasonRateLimit, 1), fail(4, ReasonRateLimit, 1), fail(5, ReasonRateLimit, 1), fail(6, ReasonRateLimit, 1)}, 60 * time.Minute},
		{"answered between", runLog{fail(10, ReasonRateLimit, 1), fail(20, ReasonInvalidOutput, 1), fail(30, ReasonRateLimit, 1)}, 5 * time.Minute},
		{"skips ignored", runLog{fail(10, ReasonRateLimit, 1), {At: at(15), Host: HostCodex, Outcome: OutcomeSkipped, Reason: ReasonLocked}, fail(30, ReasonRateLimit, 1)}, 10 * time.Minute},
		{"cli missing", runLog{fail(10, ReasonCLIMissing, 0)}, 5 * time.Minute},
		{"cli missing in a row", runLog{fail(10, ReasonRateLimit, 1), fail(20, ReasonCLIMissing, 0), fail(30, ReasonCLIMissing, 0)}, 20 * time.Minute},
		{"unusable watermark passed over", runLog{fail(10, ReasonHostError, 1), fail(15, ReasonWatermark, 0), fail(20, ReasonHostError, 1)}, 10 * time.Minute},
		{"success ends it", runLog{fail(10, ReasonRateLimit, 1), {At: at(20), Host: HostCodex, Outcome: OutcomeOK, Calls: 1}}, 0},
		{"other host", runLog{{At: at(10), Host: HostClaude, Outcome: OutcomeFailed, Reason: ReasonRateLimit, Calls: 1}}, 0},
	} {
		got := tc.log.backoffUntil(HostCodex)
		var want time.Time
		if tc.wait > 0 {
			last, _ := time.Parse(time.RFC3339, tc.log[len(tc.log)-1].At)
			want = last.Add(tc.wait)
		}
		if !got.Equal(want) {
			t.Errorf("%s: until %v, want %v (from %v)", tc.name, got, want, base)
		}
	}
}

// Only session failures count toward a quarantine; a success or a skip
// ends the row, and a host failure, a conflict, a wiki error or an unsaved
// watermark neither counts nor ends it.
func TestQuarantinedCountsSessionFailuresInARow(t *testing.T) {
	rec := func(minute int, outcome, reason string) Record {
		return Record{At: at(minute), Host: HostClaude, Session: "s", Outcome: outcome, Reason: reason}
	}
	f := func(minute int, reason string) Record { return rec(minute, OutcomeFailed, reason) }
	for _, tc := range []struct {
		name string
		log  runLog
		want string
	}{
		{"two", runLog{f(1, ReasonInvalidOutput), f(2, ReasonTimeout)}, ""},
		{"three", runLog{f(1, ReasonInvalidOutput), f(2, ReasonTimeout), f(3, ReasonOKFRefused)}, "2026-01-14"},
		{"host failures between", runLog{f(1, ReasonToolUsed), f(2, ReasonRateLimit), f(3, ReasonAuth), f(4, ReasonModelRejected), f(5, ReasonConflict), f(6, ReasonToolUsed)}, "2026-01-14"},
		{"success ends it", runLog{f(1, ReasonToolUsed), f(2, ReasonToolUsed), rec(3, OutcomeNoChange, ""), f(4, ReasonToolUsed)}, ""},
		{"skip ends it", runLog{f(1, ReasonToolUsed), f(2, ReasonToolUsed), f(3, ReasonToolUsed), rec(4, OutcomeSkipped, ReasonQuarantined)}, ""},
		{"host failures alone", runLog{f(1, ReasonRateLimit), f(2, ReasonHostError), f(3, ReasonCLIMissing)}, ""},
		{"save failures of the wiki alone", runLog{f(1, ReasonWikiError), f(2, ReasonConflict), f(3, ReasonWatermark), f(4, ReasonWikiError), f(5, ReasonConflict), f(6, ReasonWatermark)}, ""},
	} {
		if got := tc.log.quarantined(HostClaude, "s"); got != tc.want {
			t.Errorf("%s: %q, want %q", tc.name, got, tc.want)
		}
	}
}

// The first log of a day deletes the logs dated more than 14 days before
// it, and keeps every other file.
func TestOpenLogPrunesOldLogs(t *testing.T) {
	isolate(t)
	dirs, err := paths.Default()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dirs.Logs(), 0o700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"update-2026-01-01.log", "update-2026-01-16.log", "update-2026-01-17.log", "notes.txt", "update-later.log"} {
		if err := os.WriteFile(filepath.Join(dirs.Logs(), name), nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	f, err := OpenLog(dirs, time.Date(2026, 1, 31, 8, 0, 0, 0, paris))
	if err != nil {
		t.Fatal(err)
	}
	f.Close()
	entries, _ := os.ReadDir(dirs.Logs())
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	if want := []string{"notes.txt", "update-2026-01-17.log", "update-2026-01-31.log", "update-later.log"}; !slices.Equal(names, want) {
		t.Fatalf("logs %q, want %q", names, want)
	}
	if info, _ := os.Stat(filepath.Join(dirs.Logs(), "update-2026-01-31.log")); info.Mode().Perm() != 0o600 {
		t.Fatalf("mode %v", info.Mode().Perm())
	}
}

// A failed save is a conflict when the page kept changing under it, a wiki
// error when the wiki lock stayed busy or the file system refused, and
// otherwise okf's refusal of the page's text, which alone counts toward
// the session.
func TestSaveFailureReasons(t *testing.T) {
	for _, tc := range []struct {
		err  error
		want string
	}{
		{fmt.Errorf("%w: projects/x/threads/main.md", okfadapter.ErrConflict), ReasonConflict},
		{fmt.Errorf("%w: projects/x/threads/main.md", okfadapter.ErrExists), ReasonConflict},
		{fmt.Errorf("%w after 1m0s", okfadapter.ErrLockBusy), ReasonWikiError},
		{fmt.Errorf("okfadapter: create x: failed to write concept: %w", &fs.PathError{Op: "open", Path: "x", Err: syscall.EACCES}), ReasonWikiError},
		{fmt.Errorf("okfadapter: update x: %w", &os.LinkError{Op: "rename", Old: "a", New: "b", Err: syscall.ENOSPC}), ReasonWikiError},
		{fmt.Errorf("okfadapter: fsync folder x: %w", syscall.EIO), ReasonWikiError},
		{errors.New("okfadapter: update x: concept title cannot contain newlines"), ReasonOKFRefused},
	} {
		if got := saveFailure(tc.err); got != tc.want {
			t.Errorf("%v: %q, want %q", tc.err, got, tc.want)
		}
	}
}

// Recent reads only the end of each day's file, dropping the line the cut
// falls in, so a long day costs the doctor and the notice no more than the
// limit; Host works out a host's state from what it read as the job does.
func TestRecentReadsTheNewestRecords(t *testing.T) {
	isolate(t)
	dirs, err := paths.Default()
	if err != nil {
		t.Fatal(err)
	}
	day := time.Date(2026, 1, 14, 22, 0, 0, 0, paris)
	var old []byte
	for i := 0; i < 300; i++ {
		r := Record{At: day.Add(-time.Duration(300-i) * time.Minute).Format(time.RFC3339), Host: HostCodex, Session: "s-old", Outcome: OutcomeOK, Calls: 1, ModelNote: strings.Repeat("x", 100)}
		data, _ := json.Marshal(r)
		old = append(append(old, data...), '\n')
	}
	if err := paths.EnsureDir(filepath.Join(dirs.State, "updates")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dirs.State, "updates", "2026-01-14.jsonl"), old, 0o600); err != nil {
		t.Fatal(err)
	}
	for i, r := range []Record{
		{Host: HostCodex, Session: "s-new", Outcome: OutcomeFailed, Reason: ReasonInvalidOutput, Calls: 1},
		{Host: HostCodex, Session: "s-skip", Outcome: OutcomeSkipped, Reason: ReasonLocked},
		{Host: HostCodex, Session: "s-new", Outcome: OutcomeFailed, Reason: ReasonInvalidOutput, Calls: 1},
		{Host: HostCodex, Session: "s-new", Outcome: OutcomeFailed, Reason: ReasonTimeout, Calls: 1},
		{Host: HostClaude, Session: "s-other", Outcome: OutcomeFailed, Reason: ReasonAuth},
	} {
		r.At = day.Add(time.Duration(i+1) * time.Second).Format(time.RFC3339)
		if err := appendRecord(dirs.State, r); err != nil {
			t.Fatal(err)
		}
	}
	all, err := readDay(dirs.State, "2026-01-14")
	if err != nil || len(all) != 305 {
		t.Fatalf("%d records %v", len(all), err)
	}
	recent, err := Recent(dirs.State, day, 4<<10)
	if err != nil || len(recent) == 0 || len(recent) > 30 || recent[len(recent)-1].Host != HostClaude || recent[0].At == "" {
		t.Fatalf("%d recent records %v", len(recent), err)
	}
	s := Host(recent, HostCodex, day)
	if s.Last.Reason != ReasonTimeout || !slices.Equal(s.Failures, []string{ReasonTimeout, ReasonInvalidOutput, ReasonInvalidOutput}) || s.Held != "" ||
		!slices.Equal(s.Quarantined, []string{"s-new"}) || s.Calls != len(recent)-2 {
		t.Fatalf("codex: %+v", s)
	}
	if s := Host(recent, HostClaude, day); s.Held != ReasonAuth || s.Calls != 0 || len(s.Quarantined) != 0 {
		t.Fatalf("claude: %+v", s)
	}
	if s := Host(recent, HostCodex, day.AddDate(0, 0, 1)); s.Calls != 0 || len(s.Quarantined) != 0 || len(s.Failures) != 3 {
		t.Fatalf("the next day: %+v", s)
	}
}
