package health

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/heyNag/eva-md/internal/config"
	"github.com/heyNag/eva-md/internal/inbox"
	"github.com/heyNag/eva-md/internal/paths"
	sessionmarker "github.com/heyNag/eva-md/internal/session"
	"github.com/heyNag/eva-md/internal/update"
	"github.com/heyNag/eva-md/internal/work"
)

// The worker's row, and the update job's Waiting it reads, on fixture run
// logs and inboxes. The jobs are registered here as eva-md registers them,
// the update job's Waiting reading the fixture home a test sets.

var waitHome struct {
	sync.Mutex
	d   paths.Dirs
	c   config.Config
	set bool
}

func init() {
	work.Register(work.Job{Kind: "update", Priority: 3,
		Run: func(context.Context, []work.Mark, io.Writer) work.Outcome { return work.Done },
		Waiting: func(ctx context.Context, now time.Time) []work.Waiting {
			waitHome.Lock()
			defer waitHome.Unlock()
			if !waitHome.set {
				return nil
			}
			return UpdateWaits(ctx, waitHome.d, waitHome.c, "update", now, nil)
		}})
	work.Register(work.Job{Kind: "maintain", Priority: 4,
		Run: func(context.Context, []work.Mark, io.Writer) work.Outcome { return work.Done }})
}

// workNow is the time the fixtures are judged at.
var workNow = time.Date(2026, 11, 14, 12, 0, 0, 0, time.Local)

// plantedMarker is planted in every fixture entry's text and session id;
// nothing the worker's row or eva-md work --status prints may hold it.
const plantedMarker = "zqxplanted"

// The two fixtures: between them, every reason work waits for.
const (
	// heldHome: Claude Code held after auth; a Codex session quarantined
	// and Codex's calls past daily_cap = 1; a Codex chat off the record;
	// and a maintain mark written two hours before.
	heldHome = iota
	// backoffHome: Codex backing off after a rate limit, and a Claude Code
	// session free to run but for the size or quiet rule.
	backoffHome
)

// waitFixture makes a fixture home, registers it with the update job's
// Waiting, and returns its folders and config, whose switch worker is on.
func waitFixture(t *testing.T, which int) (paths.Dirs, config.Config) {
	t.Helper()
	d := isolated(t)
	c := config.Default()
	c.WikiPath, c.Instance = filepath.Join(os.Getenv("HOME"), "wiki"), "test"
	c.Switches = map[string]any{"worker": true}
	store := inbox.Store{Root: d.Inbox()}
	chat := func(host, id string, off bool) string {
		id += "-" + plantedMarker
		var data []byte
		for i := range 2 {
			line, err := json.Marshal(inbox.Entry{V: 1, TS: workNow.Add(-3 * time.Hour).Add(time.Duration(i) * time.Minute).Format(time.RFC3339),
				Host: host, Session: id, Role: "user", Kind: "prompt", Text: "a prompt that holds " + plantedMarker, Project: "general", Branch: "main", Origin: "hook"})
			if err != nil {
				t.Fatal(err)
			}
			data = append(append(data, line...), '\n')
		}
		put(t, store.Path("general", host, id), data)
		if off {
			if err := (sessionmarker.Markers{Root: d.Sessions()}).Set(host, id, sessionmarker.OffRecord, "1"); err != nil {
				t.Fatal(err)
			}
		}
		return id
	}
	var records []update.Record
	record := func(host, id, reason string) {
		records = append(records, update.Record{At: workNow.Format(time.RFC3339), Host: host, Session: id + "-" + plantedMarker, Project: "general",
			Outcome: update.OutcomeFailed, Reason: reason, Calls: 1})
	}
	switch which {
	case heldHome:
		c.DailyCap = 1
		chat(update.HostClaude, "c1", false)
		record(update.HostClaude, "c1", update.ReasonAuth)
		chat(update.HostCodex, "q/x", false)
		for range 3 {
			record(update.HostCodex, "q/x", update.ReasonInvalidOutput)
		}
		chat(update.HostCodex, "b", false)
		chat(update.HostCodex, "off", true)
		// A mark written two hours ago, left by a wake that found the
		// worker and its waiter busy.
		for _, lock := range []string{"worker.lock", "waiter.lock"} {
			unlock, busy, err := paths.TryLock(d.Locks(), lock)
			if err != nil || busy {
				t.Fatal(busy, err)
			}
			defer unlock()
		}
		if err := work.Wake(t.Context(), d, []work.Mark{{Kind: "maintain", Key: "daily", Reason: "daily"}}, work.Options{Now: func() time.Time { return workNow.Add(-2 * time.Hour) }}); err != nil {
			t.Fatal(err)
		}
	case backoffHome:
		chat(update.HostCodex, "r", false)
		record(update.HostCodex, "r", update.ReasonRateLimit)
		chat(update.HostClaude, "c2", false)
	}
	var log bytes.Buffer
	for _, r := range records {
		line, err := json.Marshal(r)
		if err != nil {
			t.Fatal(err)
		}
		log.Write(append(line, '\n'))
	}
	put(t, filepath.Join(d.State, "updates", workNow.Format(time.DateOnly)+".jsonl"), log.Bytes())
	if err := config.Save(d.ConfigFile(), c); err != nil {
		t.Fatal(err)
	}
	waitHome.Lock()
	waitHome.d, waitHome.c, waitHome.set = d, c, true
	waitHome.Unlock()
	t.Cleanup(func() {
		waitHome.Lock()
		waitHome.set = false
		waitHome.Unlock()
	})
	return d, c
}

// inUTC is waits with their times in UTC, so two that name the same
// instants compare equal whatever location each was read in.
func inUTC(waits []work.Waiting) []work.Waiting {
	out := slices.Clone(waits)
	for i := range out {
		out[i].Since, out[i].NextTry = out[i].Since.UTC(), out[i].NextTry.UTC()
	}
	return out
}

// The update job's Waiting gives one item for each session with a backlog
// and the reason it waits, with v1's reason as a hold's class and when the
// hold ends as its next try: the backoff's end as the run log gives it, and
// the next day for a quarantine. It names no session.
func TestUpdateWaitsGiveEachSessionsReason(t *testing.T) {
	tomorrow := time.Date(2026, 11, 15, 0, 0, 0, 0, time.Local)
	since := workNow.Add(-3 * time.Hour)
	for _, tc := range []struct {
		which int
		want  []work.Waiting
	}{
		{heldHome, []work.Waiting{
			{Kind: "update", Host: update.HostClaude, Since: since, Reason: work.WaitSignIn},
			{Kind: "update", Host: update.HostCodex, Since: since, Reason: work.WaitBudget},
			{Kind: "update", Host: update.HostCodex, Since: since, Reason: work.WaitHeld, Class: update.StopQuarantined, NextTry: tomorrow},
			{Kind: "update", Host: update.HostCodex, Since: since, Reason: work.WaitOff},
		}},
		{backoffHome, []work.Waiting{
			{Kind: "update", Host: update.HostClaude, Since: since, Reason: work.WaitRule},
			{Kind: "update", Host: update.HostCodex, Since: since, Reason: work.WaitHeld, Class: update.ReasonRateLimit, NextTry: workNow.Add(5 * time.Minute)},
		}},
	} {
		d, c := waitFixture(t, tc.which)
		got := inUTC(UpdateWaits(t.Context(), d, c, "update", workNow, nil))
		slices.SortFunc(got, func(a, b work.Waiting) int {
			return strings.Compare(a.Host+string(a.Reason), b.Host+string(b.Reason))
		})
		if !reflect.DeepEqual(got, inUTC(tc.want)) {
			t.Errorf("fixture %d: waits %+v, want %+v", tc.which, got, tc.want)
		}
		if tc.which == backoffHome {
			records, err := update.Recent(d.State, workNow, doctorLog)
			if err != nil {
				t.Fatal(err)
			}
			if end := update.Host(records, update.HostCodex, workNow).BackoffUntil; !end.Equal(workNow.Add(5 * time.Minute)) {
				t.Errorf("v1's backoff ends at %v", end)
			}
		}
		// With updates off, every backlog is switched off.
		c.Updates = false
		for _, w := range UpdateWaits(t.Context(), d, c, "update", workNow, nil) {
			if w.Reason != work.WaitOff {
				t.Errorf("fixture %d, updates off: %+v", tc.which, w)
			}
		}
	}
}

// A hold other than auth is held until the next day with its reason as
// the class; a backoff that has ended, a session whose watermark is at its
// end and one whose watermark can't be used wait for nothing, or give no
// item; and a session's oldest entry past the watermark, not its file's
// time, is when it started to wait.
func TestUpdateWaitsReadTheRunLogAsTheJobStopsByIt(t *testing.T) {
	d, c := waitFixture(t, backoffHome)
	file := filepath.Join(d.State, "updates", workNow.Format(time.DateOnly)+".jsonl")
	line, err := json.Marshal(update.Record{At: workNow.Add(-time.Hour).Format(time.RFC3339), Host: update.HostClaude, Session: "x", Outcome: update.OutcomeFailed, Reason: update.ReasonMCPFailed})
	if err != nil {
		t.Fatal(err)
	}
	f, err := os.OpenFile(file, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	f.Write(append(line, '\n'))
	f.Close()
	got := UpdateWaits(t.Context(), d, c, "update", workNow.Add(10*time.Minute), nil)
	tomorrow := time.Date(2026, 11, 15, 0, 0, 0, 0, time.Local)
	var claude, codex []work.Waiting
	for _, w := range got {
		if w.Host == update.HostClaude {
			claude = append(claude, w)
		} else {
			codex = append(codex, w)
		}
	}
	if len(claude) != 1 || claude[0].Reason != work.WaitHeld || claude[0].Class != update.ReasonMCPFailed || !claude[0].NextTry.Equal(tomorrow) {
		t.Errorf("claude-code held after mcp_failed: %+v", claude)
	}
	if len(codex) != 1 || codex[0].Reason != work.WaitRule {
		t.Errorf("codex once its backoff ended: %+v", codex)
	}
	// Watermarks at the end, and past it, give no item.
	store := inbox.Store{Root: d.Inbox()}
	for _, id := range []string{"r", "c2"} {
		path := store.Path("general", map[string]string{"r": update.HostCodex, "c2": update.HostClaude}[id], id+"-"+plantedMarker)
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		offset := info.Size()
		if id == "c2" {
			offset++
		}
		if err := store.SetMark(path, inbox.Mark{Offset: offset}); err != nil {
			t.Fatal(err)
		}
	}
	if got := UpdateWaits(t.Context(), d, c, "update", workNow, nil); len(got) != 0 {
		t.Errorf("drained or damaged sessions wait: %+v", got)
	}
}

// The worker's row, in eva-md status with the switch on, and eva-md work
// --status's lines: each of the six reasons, counted, with the oldest wait,
// a hold's class, host and next try, and a FAIL only for a sign-in, with
// the fix the updates row gives for auth. Neither holds the planted marker.
func TestTheWorkRowGivesEachReason(t *testing.T) {
	for _, tc := range []struct {
		which          int
		status, detail string
		fix            string
	}{
		{heldHome, "FAIL", "behind a running job: 1, the oldest for 2 hours; " +
			"deferred by the day's budget: 1, the oldest for 3 hours; " +
			"held after a failure: 1, the oldest for 3 hours, quarantined on codex until 2026-11-15 00:00:00; " +
			"waiting for sign-in: 1, the oldest for 3 hours, auth on claude-code; " +
			"switched off, paused by the owner or excluded: 1, the oldest for 3 hours", "log in with claude auth login"},
		{backoffHome, "PASS", "waiting for the size or quiet rule: 1, the oldest for 3 hours; " +
			"held after a failure: 1, the oldest for 3 hours, rate_limit on codex until 2026-11-14 12:05:00", ""},
	} {
		d, _ := waitFixture(t, tc.which)
		r := rows(&inspection{ctx: t.Context(), d: d, status: true, at: workNow})
		row := findRow(t, r, "background work")
		if row.Status != tc.status || row.Detail != tc.detail || row.Fix != tc.fix {
			t.Errorf("fixture %d: row %+v\nwant %s %q %q", tc.which, row, tc.status, tc.detail, tc.fix)
		}
		if lines := strings.Join(WorkLines(work.Status(t.Context(), d, workNow), workNow), "; "); lines != tc.detail {
			t.Errorf("fixture %d: lines %q", tc.which, lines)
		}
		if strings.Contains(strings.ToLower(row.Detail+row.Fix), plantedMarker) {
			t.Errorf("fixture %d: the row printed the planted marker", tc.which)
		}
	}
}

// Nothing past the bar is one line, and a class no release writes is
// printed as other.
func TestWorkLinesForNothingAndForAnUnknownClass(t *testing.T) {
	if got := WorkLines(work.Report{}, workNow); !reflect.DeepEqual(got, []string{"nothing has waited longer than 5 minutes"}) {
		t.Errorf("nothing: %q", got)
	}
	got := WorkLines(work.Report{Items: []work.Waiting{{Kind: "update", Host: "codex", Since: workNow.Add(-time.Hour), Reason: work.WaitHeld, Class: "Planted text", NextTry: workNow}}}, workNow)
	if want := []string{"held after a failure: 1, the oldest for 60 minutes, other on codex until 2026-11-14 12:00:00"}; !reflect.DeepEqual(got, want) {
		t.Errorf("unknown class: %q", got)
	}
}

// With the switch off, absent or set false, and with it on in a home with
// no wiki, eva-md status and doctor print what they printed before the
// worker, byte for byte: the golden rows.
func TestTheWorkRowIsAbsentWithTheSwitchOff(t *testing.T) {
	for _, tc := range []struct {
		name, golden string
		home         int
		body         string
		run          func(context.Context, paths.Dirs) Report
	}{
		{"status, switch false", "status", setUpHome, "[switches]\nworker = false\n", Status},
		{"doctor, switch false", "doctor", setUpHome, "[switches]\nworker = false\n", doctor},
		{"status, no wiki, switch true", "status-nowiki", noWikiHome, "[switches]\nworker = true\n", Status},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d, home := goldenHome(t, tc.home)
			f, err := os.OpenFile(d.ConfigFile(), os.O_APPEND|os.O_WRONLY, 0)
			if err != nil {
				t.Fatal(err)
			}
			_, err = f.WriteString(tc.body)
			f.Close()
			if err != nil {
				t.Fatal(err)
			}
			got := render(t, tc.run(t.Context(), d), home)
			want, err := os.ReadFile(filepath.Join("testdata", tc.golden+".golden"))
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(got, want) {
				t.Fatalf("rows differ from %s.golden:\n--- got\n%s--- want\n%s", tc.golden, got, want)
			}
		})
	}
	// On, with a wiki, the row is there.
	d, home := goldenHome(t, setUpHome)
	f, err := os.OpenFile(d.ConfigFile(), os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	f.WriteString("[switches]\nworker = true\n")
	f.Close()
	if !strings.Contains(string(render(t, Status(t.Context(), d), home)), "PASS  background work: ") {
		t.Fatal("no background work row with the switch on")
	}
}

// appendRecord adds r to the fixture's run log of workNow's day.
func appendRecord(t *testing.T, d paths.Dirs, r update.Record) {
	t.Helper()
	line, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	f, err := os.OpenFile(filepath.Join(d.State, "updates", workNow.Format(time.DateOnly)+".jsonl"), os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := f.Write(append(line, '\n')); err != nil {
		t.Fatal(err)
	}
}

// A session whose tail the update set aside as too small to process, with
// nothing appended since, has no backlog by its watermark, as v1's update
// and its backlog row judge it, so it waits for nothing; once an entry is
// appended past the size the update saw, it waits again.
func TestUpdateWaitsPassOverATailTooSmallToProcess(t *testing.T) {
	d, c := waitFixture(t, backoffHome)
	store := inbox.Store{Root: d.Inbox()}
	path := store.Path("general", update.HostClaude, "c2-"+plantedMarker)
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SetMark(path, inbox.Mark{Offset: 0, Wait: info.Size()}); err != nil {
		t.Fatal(err)
	}
	for _, w := range UpdateWaits(t.Context(), d, c, "update", workNow, nil) {
		if w.Host == update.HostClaude {
			t.Errorf("a tail too small to process waits: %+v", w)
		}
	}
	if lines := strings.Join(WorkLines(work.Status(t.Context(), d, workNow), workNow), "; "); strings.Contains(lines, "size or quiet") {
		t.Errorf("the row counts a tail too small to process: %q", lines)
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	f.WriteString(`{"v":1,"ts":"2026-11-14T11:00:00Z","host":"claude-code","session":"x","role":"assistant","kind":"reply","text":"more"}` + "\n")
	f.Close()
	var rule int
	for _, w := range UpdateWaits(t.Context(), d, c, "update", workNow, nil) {
		if w.Host == update.HostClaude && w.Reason == work.WaitRule {
			rule++
		}
	}
	if rule != 1 {
		t.Errorf("a tail grown past what the update saw: %d items waiting for the rule", rule)
	}
}

// skip passes over a session by its host and the session part of its
// inbox file's name, and no other.
func TestUpdateWaitsPassOverWhatSkipNames(t *testing.T) {
	d, c := waitFixture(t, backoffHome)
	var asked []string
	got := UpdateWaits(t.Context(), d, c, "update", workNow, func(host, sid string) bool {
		asked = append(asked, host+":"+sid)
		return host == update.HostClaude && sid == "c2-"+plantedMarker
	})
	slices.Sort(asked)
	if want := []string{"claude-code:c2-" + plantedMarker, "codex:r-" + plantedMarker}; !slices.Equal(asked, want) {
		t.Errorf("skip was asked %q, want %q", asked, want)
	}
	if len(got) != 1 || got[0].Host != update.HostCodex || got[0].Reason != work.WaitHeld {
		t.Errorf("waits %+v", got)
	}
}

// A backoff's class is the failure that started it, not a newer failure
// that made no call, which the backoff passes over; and backoffReasons are
// the failures that start v1's backoff, by the run log's own rule.
func TestABackoffsClassIsTheFailureThatStartedIt(t *testing.T) {
	d, c := waitFixture(t, backoffHome)
	appendRecord(t, d, update.Record{At: workNow.Add(time.Minute).Format(time.RFC3339), Host: update.HostCodex, Session: "other", Outcome: update.OutcomeFailed, Reason: update.ReasonWatermark})
	var codex []work.Waiting
	for _, w := range UpdateWaits(t.Context(), d, c, "update", workNow.Add(2*time.Minute), nil) {
		if w.Host == update.HostCodex {
			codex = append(codex, w)
		}
	}
	if len(codex) != 1 || codex[0].Reason != work.WaitHeld || codex[0].Class != update.ReasonRateLimit || !codex[0].NextTry.Equal(workNow.Add(5*time.Minute)) {
		t.Errorf("a backoff after a rate limit, then a watermark: %+v", codex)
	}
	if got := backoffClass([]string{update.ReasonWatermark}); got != update.StopBackoff {
		t.Errorf("a row with no backoff's failure: %q", got)
	}
	for _, reason := range []string{update.ReasonRateLimit, update.ReasonHostError, update.ReasonWikiError, update.ReasonCLIMissing,
		update.ReasonAuth, update.ReasonMCPFailed, update.ReasonInvalidOutput, update.ReasonToolUsed, update.ReasonPageRefused,
		update.ReasonTimeout, update.ReasonModelRejected, update.ReasonWatermark, update.ReasonLeft} {
		r := update.Record{At: workNow.Format(time.RFC3339), Host: update.HostCodex, Session: "s", Outcome: update.OutcomeFailed, Reason: reason, Calls: 1}
		backs := !update.Host([]update.Record{r}, update.HostCodex, workNow).BackoffUntil.IsZero()
		if backs != slices.Contains(backoffReasons, reason) {
			t.Errorf("%s: v1 backs off %v, backoffReasons says %v", reason, backs, !backs)
		}
	}
}
