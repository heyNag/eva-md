package health

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	_ "time/tzdata" // America/Los_Angeles, for the change of daylight saving

	"github.com/heyNag/eva-md/internal/config"
	"github.com/heyNag/eva-md/internal/inbox"
	"github.com/heyNag/eva-md/internal/ledger/inboxv1"
	"github.com/heyNag/eva-md/internal/paths"
	"github.com/heyNag/eva-md/internal/searchindex"
	"github.com/heyNag/eva-md/internal/update"
	"github.com/heyNag/eva-md/internal/wikigit"
	"github.com/heyNag/eva-md/internal/work"
)

// The rules of the stages and the verdict that the goldens alone can't
// tell apart from rules close to them.

// stageOf finds a stage by host and name, or fails the test.
func stageOf(t *testing.T, stages []Stage, host, name string) Stage {
	t.Helper()
	for _, s := range stages {
		if s.Host == host && s.Name == name {
			return s
		}
	}
	t.Fatalf("no %s/%s stage in %+v", host, name, stages)
	return Stage{}
}

// saveConfig writes c as eva-md's config.
func saveConfig(t *testing.T, d paths.Dirs, c config.Config) {
	t.Helper()
	if err := config.Save(d.ConfigFile(), c); err != nil {
		t.Fatal(err)
	}
}

// A host eva-md is not installed for, as a one-host install leaves the
// other, is not captured: though it is in use and its newest chat is three
// hours old, its capture lag is off, not a failure, and stays out of the
// verdict. The host eva-md is installed for keeps its lag.
func TestCaptureOfAHostNotInstalledIsOff(t *testing.T) {
	now := time.Now()
	d, _ := stageHome(t)
	installedFor(t, d, update.HostClaude, now)
	beatWith(t, d, update.HostClaude, "Stop", now.Add(-time.Minute), true, false)
	chatFolder(t, codexDay(now.Add(-3*time.Hour)), now.Add(-3*time.Hour))

	r := statusAt(t, d, now)
	row := findRow(t, r, update.HostCodex+" capture lag")
	if want := "no lag, bar 5 minutes; eva-md is not installed for it, so its capture is not measured"; row.Status != "PASS" || row.Detail != want {
		t.Fatalf("capture row of a host not installed %+v, want PASS %q", row, want)
	}
	if _, ok := rowNamed(r, "Codex"); ok {
		t.Fatal("a host in use was taken for one nobody uses")
	}
	if r.Verdict != "every stage is within its bar" {
		t.Fatalf("verdict %q", r.Verdict)
	}
	if row := findRow(t, r, update.HostClaude+" capture lag"); row.Detail != "no lag, bar 5 minutes; no chat newer than the host's newest hook run" {
		t.Fatalf("capture row of the installed host %+v", row)
	}
}

// With commits = false the commit and push lags are shown but held to no
// budget: three days of pages not committed pass, and the verdict names
// them apart. With updates = false the organizing lag is held the same.
func TestStagesSwitchedOffAreHeldToNoBudget(t *testing.T) {
	now := time.Now()
	at := func(d time.Duration) string { return now.Add(-d).Format(time.RFC3339) }
	d, c := stageHome(t)
	c.Commits = false
	saveConfig(t, d, c)
	inUse(t, d, now)
	commitFixture(t, c, now)
	plantRunLog(t, d, update.Record{At: at(72 * time.Hour), Host: update.HostCodex, Outcome: update.OutcomeOK, Pages: 1})
	r := statusAt(t, d, now)
	row := findRow(t, r, "commit lag")
	want := "lag 3 days, bar 24 hours, past its bar and its budget of 2 days; 1 file written and not yet committed; daily commits are off (commits = false in the config), so its lag is held to no budget"
	if row.Status != "PASS" || row.Detail != want {
		t.Fatalf("commit row with commits off %+v, want PASS %q", row, want)
	}
	if row := findRow(t, r, "push lag"); row.Status != "PASS" || !strings.HasSuffix(row.Detail, "; daily pushes are off (commits = false in the config), so its lag is held to no budget") {
		t.Fatalf("push row with commits off %+v", row)
	}
	if want := "every stage that is switched on is within its bar; switched off, so held to no bar: the wiki's commit lags 3 days"; r.Verdict != want {
		t.Fatalf("verdict %q, want %q", r.Verdict, want)
	}

	d, c = stageHome(t)
	c.Updates = false
	saveConfig(t, d, c)
	inUse(t, d, now)
	plantSession(t, d, update.HostCodex, "waits", chatLines(t, update.HostCodex, "waits", now.Add(-50*time.Hour), 1, true), 0, now)
	r = statusAt(t, d, now)
	row = findRow(t, r, update.HostCodex+" organizing lag")
	if row.Status != "PASS" || !strings.HasPrefix(row.Detail, "lag 2 days, bar 5 minutes, past its bar and its budget of 24 hours") || !strings.HasSuffix(row.Detail, "; updates are off (updates = false in the config), so its lag is held to no budget") {
		t.Fatalf("organizing row with updates off %+v", row)
	}
	if want := "every stage that is switched on is within its bar; switched off, so held to no bar: Codex organizing lags 2 days"; r.Verdict != want {
		t.Fatalf("verdict %q, want %q", r.Verdict, want)
	}
	// An off stage beside one that can't be read.
	if err := os.RemoveAll(filepath.Join(c.WikiPath, ".git")); err != nil {
		t.Fatal(err)
	}
	if want := "every stage that is switched on and could be read is within its bar; the lag of the wiki's commit, the wiki's push can't be told; switched off, so held to no bar: Codex organizing lags 2 days"; statusAt(t, d, now).Verdict != want {
		t.Fatalf("verdict %q, want %q", statusAt(t, d, now).Verdict, want)
	}
}

// The run log is read by calendar date from the last commit's own day: a
// change to daylight saving between that day and today, which puts their
// midnights 47 hours apart, does not cut the last commit's day off, and the
// commit's time is read in today's zone, though its record is in UTC.
func TestFirstWrittenReadsTheLastCommitsDayAcrossDaylightSaving(t *testing.T) {
	zone, err := time.LoadLocation("America/Los_Angeles")
	if err != nil {
		t.Fatal(err)
	}
	d := isolated(t)
	now := time.Date(2026, 3, 9, 10, 0, 0, 0, zone)
	saved := time.Date(2026, 3, 7, 23, 0, 0, 0, zone)
	written := time.Date(2026, 3, 7, 23, 30, 0, 0, zone)
	plantRunLog(t, d, update.Record{At: written.Format(time.RFC3339), Host: update.HostCodex, Outcome: update.OutcomeOK, Pages: 1})
	for _, s := range []time.Time{saved, saved.UTC()} {
		got, ok := firstWritten(d, s, now)
		if !ok || !got.Equal(written) {
			t.Fatalf("saved %v: first written %v, %v; want %v", s, got, ok, written)
		}
	}
	for _, tc := range []struct {
		from, to time.Time
		want     int
	}{
		{saved, now, 2},
		{time.Date(2026, 10, 31, 0, 0, 0, 0, zone), time.Date(2026, 11, 2, 23, 59, 0, 0, zone), 2},
		{now, now, 0},
	} {
		if got := calendarDays(tc.from, tc.to); got != tc.want {
			t.Errorf("calendarDays(%v, %v) = %d, want %d", tc.from, tc.to, got, tc.want)
		}
	}
}

// errAfter is a context whose Err reports it cancelled from its calls'
// limit on, so a test can end the check's time at an exact point.
type errAfter struct {
	context.Context
	calls, limit int
}

func (c *errAfter) Err() error {
	c.calls++
	if c.limit >= 0 && c.calls > c.limit {
		return context.Canceled
	}
	return nil
}

// When the check's time runs out once the inbox is listed, the sessions
// left are counted as not read, so the losses row says its count is short.
func TestLossesCountTheSessionsLeftWhenTimeRunsOut(t *testing.T) {
	now := time.Now()
	d, _ := stageHome(t)
	for _, id := range []string{"one", "two"} {
		plantSession(t, d, update.HostCodex, id, turnChat(t, update.HostCodex, id, now, 3), -1, now.Add(-time.Minute))
	}
	// How many times listing the inbox asks the context.
	count := &errAfter{Context: t.Context(), limit: -1}
	if _, err := (inboxv1.Backend{Root: d.Inbox()}).Sessions(count); err != nil {
		t.Fatal(err)
	}
	l := countLosses(&errAfter{Context: t.Context(), limit: count.calls}, d, now)[update.HostCodex]
	if !l.Listed || l.Unread != 2 || l.Closed != 0 {
		t.Fatalf("losses %+v, want both sessions unread", l)
	}
	if row := lossRow(update.HostCodex, l); !strings.Contains(row.Detail, "2 session files could not be read, so the turns counted are a floor") {
		t.Fatalf("losses row %+v", row)
	}
	if l := countLosses(t.Context(), d, now)[update.HostCodex]; l.Unread != 0 || l.Closed != 6 {
		t.Fatalf("losses in time %+v", l)
	}
}

// The verdict ranks the stages past their bars by lag over bar, not by
// lag: a push 2 hours behind its bar of 10 minutes is further past it than
// a commit 30 hours behind its bar of 24 hours. The commit lag runs from
// the pages written after the last run that committed or found nothing, a
// push-only run being none, and the push lag, with no commit on record
// since the last good push, from that push.
func TestVerdictRanksByLagOverBar(t *testing.T) {
	now := time.Now()
	at := func(d time.Duration) string { return now.Add(-d).Format(time.RFC3339) }
	d, c := stageHome(t)
	inUse(t, d, now)
	commitFixture(t, c, now)
	gitIn(t, c.WikiPath, "add", "-A")
	gitIn(t, c.WikiPath, "commit", "-q", "-m", "pages")
	put(t, filepath.Join(c.WikiPath, "projects", "a", "threads", "next.md"), []byte("a page\n"))
	plantCommits(t, d,
		wikigit.Result{At: at(40 * time.Hour), Outcome: wikigit.Nothing, Push: wikigit.PushSkipped},
		wikigit.Result{At: at(2 * time.Hour), Outcome: wikigit.Nothing, Reason: wikigit.ReasonPushOnly, Push: wikigit.PushOK})
	plantRunLog(t, d, update.Record{At: at(30 * time.Hour), Host: update.HostCodex, Outcome: update.OutcomeOK, Pages: 1})

	r := statusAt(t, d, now)
	if row := findRow(t, r, "commit lag"); row.Detail != "lag 30 hours, bar 24 hours, past its bar; 1 file written and not yet committed" {
		t.Fatalf("commit row %+v", row)
	}
	if row := findRow(t, r, "push lag"); row.Detail != "lag 2 hours, bar 10 minutes, past its bar; 1 commit not yet pushed" {
		t.Fatalf("push row %+v", row)
	}
	if want := "the wiki's push lags 2 hours, past its bar of 10 minutes"; r.Verdict != want {
		t.Fatalf("verdict %q, want %q", r.Verdict, want)
	}
}

// A stage that lags but within its bar leaves every stage within its bar.
func TestVerdictWithALagUnderItsBar(t *testing.T) {
	now := time.Now()
	d, _ := stageHome(t)
	inUse(t, d, now)
	plantSession(t, d, update.HostCodex, "waits", chatLines(t, update.HostCodex, "waits", now.Add(-2*time.Minute), 1, true), 0, now)
	r := statusAt(t, d, now)
	if row := findRow(t, r, update.HostCodex+" organizing lag"); row.Detail != "lag 2 minutes, bar 5 minutes; 1 session waits" {
		t.Fatalf("organizing row %+v", row)
	}
	if r.Verdict != "every stage is within its bar" {
		t.Fatalf("verdict %q", r.Verdict)
	}
}

// A host nobody uses is left out of the verdict, though its old entries
// still wait in the inbox.
func TestVerdictLeavesOutAHostNobodyUses(t *testing.T) {
	now := time.Now()
	d, c := stageHome(t)
	beatWith(t, d, update.HostClaude, "Stop", now.Add(-time.Minute), true, false)
	plantSession(t, d, update.HostCodex, "old", chatLines(t, update.HostCodex, "old", now.Add(-20*24*time.Hour), 1, true), 0, now.Add(-20*24*time.Hour))
	r := statusAt(t, d, now)
	if _, ok := rowNamed(r, "Codex"); !ok {
		t.Fatalf("Codex was not taken for a host nobody uses: %+v", r.Checks)
	}
	if s := stageOf(t, Stages(t.Context(), d, c, now), update.HostCodex, StageOrganizing); s.Lag < 19*24*time.Hour {
		t.Fatalf("the fixture's organizing lag %v", s.Lag)
	}
	if r.Verdict != "every stage is within its bar" {
		t.Fatalf("verdict %q", r.Verdict)
	}
}

// Through status, an index that covers less than all of the inbox says
// search scans the rest with the switch on, and not with it off.
func TestStatusSearchIndexRowFollowsTheSwitch(t *testing.T) {
	now := time.Now()
	d, c := stageHome(t)
	appendEntry := func() {
		t.Helper()
		e := inbox.Entry{Host: update.HostCodex, Session: "row", Project: "general", Role: "user", Kind: "prompt", Text: "words to index", TS: now.Format(time.RFC3339), Origin: "hook"}
		if err := (inbox.Store{Root: d.Inbox()}).Append(e); err != nil {
			t.Fatal(err)
		}
	}
	appendEntry()
	if _, err := searchindex.Catchup(t.Context(), d, "maintain"); err != nil {
		t.Fatal(err)
	}
	appendEntry()
	if row := findRow(t, statusAt(t, d, now), "search index"); !strings.HasSuffix(row.Detail, "; search scans the rest") {
		t.Fatalf("switch on: %+v", row)
	}
	c.Switches = nil
	saveConfig(t, d, c)
	if row := findRow(t, statusAt(t, d, now), "search index"); strings.Contains(row.Detail, "search scans the rest") {
		t.Fatalf("switch off: %+v", row)
	}
}

// The capture lag runs from the install when it is newer than the newest
// hook run, and from the chat when there is neither a run nor an install
// time that is not in the future.
func TestCaptureLagFromTheInstallOrTheChat(t *testing.T) {
	now := time.Now()
	d, _ := stageHome(t)
	// installedFor dates the install 12 hours before the time it is given.
	installedFor(t, d, update.HostClaude, now.Add(10*time.Hour))
	beatWith(t, d, update.HostClaude, "Stop", now.Add(-3*24*time.Hour), true, false)
	chatFolder(t, filepath.Join(os.Getenv("HOME"), ".claude", "projects", "a-project"), now.Add(-time.Hour))
	if row := findRow(t, statusAt(t, d, now), update.HostClaude+" capture lag"); row.Detail != "lag 2 hours, bar 5 minutes, past its bar and its budget of 1 hour; the host's newest chat is newer than its newest hook run" {
		t.Fatalf("an install newer than the newest run: %+v", row)
	}

	d, _ = stageHome(t)
	installedFor(t, d, update.HostClaude, now.Add(13*time.Hour))
	chatFolder(t, filepath.Join(os.Getenv("HOME"), ".claude", "projects", "a-project"), now.Add(-3*time.Hour))
	if row := findRow(t, statusAt(t, d, now), update.HostClaude+" capture lag"); row.Detail != "lag 3 hours, bar 5 minutes, past its bar and its budget of 1 hour; the host's newest chat is newer than its newest hook run" {
		t.Fatalf("no run and an install in the future: %+v", row)
	}
}

// Pages written before the last commit, on its own day, are not what
// waits: the commit lag runs from the first pages after it.
func TestCommitLagSkipsPagesBeforeTheLastCommit(t *testing.T) {
	now := time.Now()
	y := now.AddDate(0, 0, -1)
	noon := time.Date(y.Year(), y.Month(), y.Day(), 12, 0, 0, 0, now.Location())
	d, c := stageHome(t)
	commitFixture(t, c, now)
	plantCommits(t, d, wikigit.Result{At: noon.Format(time.RFC3339), Outcome: wikigit.Nothing, Push: wikigit.PushSkipped})
	plantRunLog(t, d,
		update.Record{At: noon.Add(-time.Hour).Format(time.RFC3339), Host: update.HostCodex, Outcome: update.OutcomeOK, Pages: 1},
		update.Record{At: noon.Add(time.Hour).Format(time.RFC3339), Host: update.HostCodex, Outcome: update.OutcomeOK, Pages: 1})
	if s := stageOf(t, Stages(t.Context(), d, c, now), "", StageCommit); !s.Since.Equal(noon.Add(time.Hour)) {
		t.Fatalf("commit lag since %v, want %v", s.Since, noon.Add(time.Hour))
	}
}

// A Claude Code project folder changed 10 days ago makes the host used:
// its activity is read over 14 days, as Codex's is.
func TestClaudeActivityIsReadOverFourteenDays(t *testing.T) {
	now := time.Now()
	d, _ := stageHome(t)
	chatFolder(t, filepath.Join(os.Getenv("HOME"), ".claude", "projects", "a-project"), now.Add(-10*24*time.Hour))
	r := statusAt(t, d, now)
	if _, ok := rowNamed(r, "Claude Code"); ok {
		t.Fatal("a host with a chat 10 days ago was taken for one nobody uses")
	}
	if _, ok := rowNamed(r, update.HostClaude+" capture lag"); !ok {
		t.Fatal("a host with a chat 10 days ago has no capture row")
	}
}

// Each host's organizing row gives its own reason, that of its own oldest
// waiting work, though the other host's is older.
func TestOrganizingReasonIsTheHostsOwn(t *testing.T) {
	now := time.Now()
	d, _ := stageHome(t)
	inUse(t, d, now)
	plantSession(t, d, update.HostClaude, "waits", chatLines(t, update.HostClaude, "waits", now.Add(-4*time.Hour), 1, true), 0, now)
	plantSession(t, d, update.HostCodex, "waits", chatLines(t, update.HostCodex, "waits", now.Add(-3*time.Hour), 1, true), 0, now)
	fakeWait(t,
		work.Waiting{Kind: "healthfake", Host: update.HostClaude, Since: now.Add(-4 * time.Hour), Reason: work.WaitSignIn},
		work.Waiting{Kind: "healthfake", Host: update.HostCodex, Since: now.Add(-3 * time.Hour), Reason: work.WaitBudget})
	r := statusAt(t, d, now)
	for host, reason := range map[string]work.WaitReason{update.HostClaude: work.WaitSignIn, update.HostCodex: work.WaitBudget} {
		if row := findRow(t, r, host+" organizing lag"); !strings.Contains(row.Detail, "past its bar: "+reason.Words()+";") {
			t.Errorf("%s organizing row %+v, want the reason %q", host, row, reason.Words())
		}
	}
}
