package health

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/heyNag/eva-md/internal/config"
	"github.com/heyNag/eva-md/internal/inbox"
	"github.com/heyNag/eva-md/internal/lint"
	"github.com/heyNag/eva-md/internal/paths"
	"github.com/heyNag/eva-md/internal/update"
	"github.com/heyNag/eva-md/internal/wikigit"
)

// The clock of the record tests: records are planted around it.
var clock = time.Date(2026, 10, 31, 15, 0, 0, 0, time.Local)

// ago is a record time d before the clock.
func ago(d time.Duration) string { return clock.Add(-d).Format(time.RFC3339) }

// wikiConfig is a config naming a wiki folder, with updates and commits on.
func wikiConfig(t *testing.T) config.Config {
	t.Helper()
	c := config.Default()
	c.WikiPath = filepath.Join(os.Getenv("HOME"), "wiki")
	c.Instance = "test-0a1b2c3d"
	if err := os.MkdirAll(c.WikiPath, 0o700); err != nil {
		t.Fatal(err)
	}
	return c
}

// plantRunLog writes run-log records into the files of their days.
func plantRunLog(t *testing.T, d paths.Dirs, records ...update.Record) {
	t.Helper()
	for _, r := range records {
		data, _ := json.Marshal(r)
		file := filepath.Join(d.State, "updates", r.At[:len(time.DateOnly)]+".jsonl")
		if err := paths.EnsureDir(filepath.Dir(file)); err != nil {
			t.Fatal(err)
		}
		f, err := os.OpenFile(file, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
		if err != nil {
			t.Fatal(err)
		}
		_, err = f.Write(append(data, '\n'))
		f.Close()
		if err != nil {
			t.Fatal(err)
		}
	}
}

// plantCommits writes commit records into the files of their months.
func plantCommits(t *testing.T, d paths.Dirs, records ...wikigit.Result) {
	t.Helper()
	for _, r := range records {
		if r.Unstaged == nil {
			r.Unstaged = []string{}
		}
		data, _ := json.Marshal(r)
		file := filepath.Join(d.State, "commits", r.At[:len("2006-01")]+".jsonl")
		if err := paths.EnsureDir(filepath.Dir(file)); err != nil {
			t.Fatal(err)
		}
		f, err := os.OpenFile(file, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
		if err != nil {
			t.Fatal(err)
		}
		_, err = f.Write(append(data, '\n'))
		f.Close()
		if err != nil {
			t.Fatal(err)
		}
	}
}

// findRow finds a row of r by name.
func findRow(t *testing.T, r Report, name string) Check {
	t.Helper()
	for _, c := range r.Checks {
		if c.Name == name {
			return c
		}
	}
	t.Fatalf("no %q row in %+v", name, r.Checks)
	return Check{}
}

func okRecord(at string, host, session string) update.Record {
	return update.Record{At: at, Host: host, Session: session, Outcome: update.OutcomeOK, Calls: 1}
}

func failedRecord(at, host, session, reason string) update.Record {
	return update.Record{At: at, Host: host, Session: session, Outcome: update.OutcomeFailed, Reason: reason, Calls: 1}
}

// Each host's updates row comes from the run log: the last update and the
// day's calls against the cap pass; a hold, 3 failures in a row, a session
// quarantined today or one whose newest record is failed (watermark) fail,
// each with its fix; no record, fewer
// failures in a row or updates = false are unknown, and so is a host
// eva-md is not installed for, whatever its records. A cli_missing failure
// with no usable CLI path recorded asks for a reinstall.
func TestUpdateRows(t *testing.T) {
	const s1, s2 = "0199a000-1111-7000", "0199b000-2222-7000"
	cliMissing := update.Record{At: ago(time.Hour), Host: update.HostCodex, Session: s1, Outcome: update.OutcomeFailed, Reason: update.ReasonCLIMissing}
	for _, tc := range []struct {
		name     string
		edit     func(*config.Config)
		records  []update.Record
		cli      bool // install recorded a usable path to codex
		status   string
		detail   []string
		fix      string
		noticeOf string // the notice the same records give, "" for none
	}{
		{name: "healthy", records: []update.Record{okRecord(ago(3*time.Hour), update.HostCodex, s1), okRecord(ago(time.Hour), update.HostCodex, s1)},
			status: "PASS", detail: []string{"last " + clock.Add(-time.Hour).Format("2006-01-02 15:04") + ": ok", "2 of 40 model calls today"}},
		{name: "none", status: "UNKNOWN", detail: []string{"no update recorded today or yesterday", "0 of 40"}, fix: "updates start when a chat ends"},
		{name: "off", edit: func(c *config.Config) { c.Updates = false }, records: []update.Record{failedRecord(ago(time.Hour), update.HostCodex, s1, update.ReasonAuth)},
			status: "UNKNOWN", detail: []string{"updates are off"}, fix: "set updates = true"},
		{name: "held", records: []update.Record{failedRecord(ago(time.Hour), update.HostCodex, s1, update.ReasonAuth)},
			status: "FAIL", detail: []string{"paused today after auth (not logged in)", "the last 1 failed (auth)"}, fix: "log in with codex login, then run eva-md update --pending --foreground",
			noticeOf: "eva.md: codex updates are paused today: not logged in; run eva-md doctor."},
		{name: "held, then a manual success", records: []update.Record{failedRecord(ago(2*time.Hour), update.HostCodex, s1, update.ReasonAuth), {At: ago(time.Hour), Host: update.HostCodex, Session: s1, Outcome: update.OutcomeOK, Calls: 1, Manual: true}},
			status: "PASS"},
		{name: "two in a row", records: []update.Record{failedRecord(ago(2*time.Hour), update.HostCodex, s1, update.ReasonRateLimit), failedRecord(ago(time.Hour), update.HostCodex, s2, update.ReasonHostError)},
			status: "UNKNOWN", detail: []string{"the last 2 failed (host_error, rate_limit)"}, fix: "the next trigger retries"},
		{name: "three in a row across the night", records: []update.Record{okRecord(ago(30*time.Hour), update.HostCodex, s1), failedRecord(ago(20*time.Hour), update.HostCodex, s1, update.ReasonTimeout),
			{At: ago(19 * time.Hour), Host: update.HostCodex, Session: s2, Outcome: update.OutcomeSkipped, Reason: update.ReasonLocked},
			failedRecord(ago(2*time.Hour), update.HostCodex, s2, update.ReasonRateLimit), failedRecord(ago(time.Hour), update.HostCodex, s2, update.ReasonRateLimit)},
			status: "FAIL", detail: []string{"the last 3 failed (rate_limit, timeout)"}, fix: "eva-md update --pending --foreground to retry",
			noticeOf: "eva.md: the last 3 codex updates failed (rate_limit, timeout); run eva-md doctor."},
		{name: "quarantined", records: []update.Record{failedRecord(ago(3*time.Hour), update.HostCodex, s1, update.ReasonInvalidOutput), failedRecord(ago(2*time.Hour), update.HostCodex, s1, update.ReasonInvalidOutput),
			failedRecord(ago(90*time.Minute), update.HostCodex, s1, update.ReasonInvalidOutput), okRecord(ago(time.Hour), update.HostCodex, s2)},
			status: "FAIL", detail: []string{"1 session quarantined today (codex:0199a000)"}, fix: "each gets one retry tomorrow",
			noticeOf: "eva.md: 1 codex session is quarantined after 3 failed updates; run eva-md doctor."},
		{name: "quarantined yesterday", records: []update.Record{failedRecord(ago(23*time.Hour), update.HostCodex, s1, update.ReasonTimeout), failedRecord(ago(22*time.Hour), update.HostCodex, s1, update.ReasonTimeout),
			failedRecord(ago(21*time.Hour), update.HostCodex, s1, update.ReasonTimeout), okRecord(ago(time.Hour), update.HostCodex, s2)},
			status: "PASS"},
		{name: "an unusable watermark", records: []update.Record{{At: ago(2 * time.Hour), Host: update.HostCodex, Session: s1, Outcome: update.OutcomeFailed, Reason: update.ReasonWatermark},
			okRecord(ago(time.Hour), update.HostCodex, s2)},
			status: "FAIL", detail: []string{"1 session has a watermark that can't be used or saved (codex:0199a000)"}, fix: "eva-md update --session codex:<id> --foreground shows why"},
		{name: "a watermark saved since", records: []update.Record{failedRecord(ago(2*time.Hour), update.HostCodex, s1, update.ReasonWatermark), okRecord(ago(time.Hour), update.HostCodex, s1)},
			status: "PASS"},
		{name: "chats left", records: []update.Record{leftRecord(ago(3*time.Hour), s1), leftRecord(ago(2*time.Hour), s2), leftRecord(ago(time.Hour), "0199c000-3333-7000")},
			status: "FAIL", detail: []string{"3 chats from before updates couldn't be set aside yet"}, fix: "once its file can be read and its watermark saved"},
		{name: "a chat set aside since", records: []update.Record{leftRecord(ago(2*time.Hour), s1), {At: ago(time.Hour), Host: update.HostCodex, Session: s1, Outcome: update.OutcomeSkipped, Reason: update.ReasonLeft}, okRecord(ago(30*time.Minute), update.HostCodex, s2)},
			status: "PASS"},
		{name: "cli missing, with no path recorded", records: []update.Record{cliMissing},
			status: "UNKNOWN", detail: []string{"the last 1 failed (cli_missing)"}, fix: "run eva-md install again to record the CLI's path"},
		{name: "cli missing, with a path recorded", records: []update.Record{cliMissing}, cli: true,
			status: "UNKNOWN", detail: []string{"the last 1 failed (cli_missing)"}, fix: "the next trigger retries"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := isolated(t)
			c := wikiConfig(t)
			if tc.edit != nil {
				tc.edit(&c)
			}
			// Claude Code is on this Mac, though eva-md is not installed for it.
			fakeOnPath(t, "claude")
			if tc.cli {
				codex := filepath.Join(os.Getenv("HOME"), "bin", "codex")
				put(t, codex, []byte("#!/bin/sh\nexit 0\n"))
				if err := os.Chmod(codex, 0o700); err != nil {
					t.Fatal(err)
				}
				data, _ := json.Marshal(map[string]string{"codex": codex})
				put(t, filepath.Join(d.State, "cli-paths.json"), data)
			}
			put(t, filepath.Join(d.Share, "marketplace", "codex", ".eva-md-managed"), []byte("eva-md marketplace v1\n"))
			// A record of the host eva-md is not installed for changes nothing.
			plantRunLog(t, d, okRecord(ago(4*time.Hour), update.HostClaude, s1))
			plantRunLog(t, d, tc.records...)
			var r Report
			inspectUpdates(&r, d, c, clock)
			row := findRow(t, r, "codex updates")
			if row.Status != tc.status || !strings.Contains(row.Fix, tc.fix) {
				t.Fatalf("row %+v", row)
			}
			for _, want := range tc.detail {
				if !strings.Contains(row.Detail, want) {
					t.Fatalf("detail %q lacks %q", row.Detail, want)
				}
			}
			if other := findRow(t, r, "claude-code updates"); other.Status != "UNKNOWN" || other.Detail != "eva-md is not installed for claude-code" || other.Fix != "run eva-md install" {
				t.Fatalf("the other host: %+v", other)
			}
			if got := backgroundNotice(d, c, clock); got != tc.noticeOf {
				t.Fatalf("notice %q, want %q", got, tc.noticeOf)
			}
		})
	}
}

// fakeOnPath makes PATH a folder holding only fake commands of these names.
func fakeOnPath(t *testing.T, names ...string) {
	t.Helper()
	bin := t.TempDir()
	for _, name := range names {
		put(t, filepath.Join(bin, name), []byte("#!/bin/sh\nexit 0\n"))
		if err := os.Chmod(filepath.Join(bin, name), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", bin)
}

// A host whose app isn't on this Mac at all gets no fix to install eva-md
// for it.
func TestUpdateRowsForAnAppNotOnThisMac(t *testing.T) {
	d := isolated(t)
	c := wikiConfig(t)
	fakeOnPath(t, "codex")
	put(t, filepath.Join(d.Share, "marketplace", "codex", ".eva-md-managed"), []byte("eva-md marketplace v1\n"))
	var r Report
	inspectUpdates(&r, d, c, clock)
	if row := findRow(t, r, "claude-code updates"); row.Status != "UNKNOWN" || row.Detail != "Claude Code isn't installed on this Mac" || row.Fix != "" {
		t.Fatalf("row %+v", row)
	}
	if row := findRow(t, r, "codex updates"); strings.Contains(row.Detail, "isn't installed") {
		t.Fatalf("row %+v", row)
	}
}

// The daily commit and push rows come from the commit records: a commit
// passes and a refusal fails with the fix for its reason, with how long
// commits have failed; files left out fail too. A push that went through
// passes; one pending is unknown for 3 days, then fails, and is retried; a
// diverged or refused push fails. A busy run says nothing, nor does the
// push a run with nothing to commit skipped. The notices follow: a commit
// failing more than 2 days, a push more than 3, and a diverged wiki.
func TestCommitAndPushRows(t *testing.T) {
	committed := func(at string, push string) wikigit.Result {
		return wikigit.Result{At: at, Outcome: wikigit.Committed, Pages: 3, Push: push}
	}
	for _, tc := range []struct {
		name               string
		records            []wikigit.Result
		commit, push       string // statuses
		commitHas, pushHas string
		fix                string
		notice             string
		retry              bool // whether a chat start retries the push
	}{
		{name: "none", commit: "UNKNOWN", push: "UNKNOWN", commitHas: "no daily commit has run yet"},
		{name: "committed and pushed", records: []wikigit.Result{committed(ago(5*time.Hour), wikigit.PushOK)},
			commit: "PASS", push: "PASS", commitHas: "committed (3 pages)", pushHas: ": ok"},
		{name: "nothing to commit or push", records: []wikigit.Result{committed(ago(30*time.Hour), wikigit.PushOK), {At: ago(5 * time.Hour), Outcome: wikigit.Nothing, Push: wikigit.PushSkipped}},
			commit: "PASS", push: "PASS", commitHas: ": nothing", pushHas: clock.Add(-30*time.Hour).Format("2006-01-02 15:04") + ": ok"},
		// A run with nothing to commit skips the push when it can't tell
		// whether the branch is ahead, as without an upstream branch: a
		// pending push stays pending.
		{name: "pending, then nothing to commit", records: []wikigit.Result{committed(ago(30*time.Hour), wikigit.PushPending), {At: ago(5 * time.Hour), Outcome: wikigit.Nothing, Push: wikigit.PushSkipped}},
			commit: "PASS", push: "UNKNOWN", commitHas: ": nothing", pushHas: "pending; since " + clock.Add(-30*time.Hour).Format("2006-01-02 15:04"), retry: true},
		{name: "refused for 3 days", records: []wikigit.Result{committed(ago(80*time.Hour), wikigit.PushOK), {At: ago(72 * time.Hour), Outcome: wikigit.Refused, Reason: "index_lock", Push: wikigit.PushSkipped, LockAge: 30},
			{At: ago(48 * time.Hour), Outcome: wikigit.Refused, Reason: "index_lock", Push: wikigit.PushSkipped}, {At: ago(time.Hour), Outcome: wikigit.Busy, Push: wikigit.PushSkipped}},
			commit: "FAIL", push: "PASS", commitHas: "refused (index_lock); failing since", fix: "remove its .git/index.lock by hand",
			notice: "eva.md: the wiki has not been committed for 3 days (index_lock); run eva-md doctor."},
		{name: "declined signature yesterday", records: []wikigit.Result{committed(ago(30*time.Hour), wikigit.PushOK), {At: ago(5 * time.Hour), Outcome: wikigit.Failed, Reason: "commit", Push: wikigit.PushSkipped}},
			commit: "FAIL", push: "PASS", commitHas: "failed (commit)", fix: "eva-md commit tries again now"},
		{name: "left out", records: []wikigit.Result{{At: ago(5 * time.Hour), Outcome: wikigit.Committed, Pages: 1, Push: wikigit.PushOK, Unstaged: []string{"general/threads/leak.md"}}},
			commit: "FAIL", push: "PASS", commitHas: "1 left out: general/threads/leak.md", fix: "remove what must not be committed"},
		{name: "pending a day", records: []wikigit.Result{committed(ago(26*time.Hour), wikigit.PushPending), {At: ago(2 * time.Hour), Outcome: wikigit.Nothing, Reason: wikigit.ReasonPushOnly, Push: wikigit.PushPending}},
			commit: "PASS", push: "UNKNOWN", pushHas: "pending; since " + clock.Add(-26*time.Hour).Format("2006-01-02 15:04") + " (26 hours)", retry: true},
		{name: "pending 4 days", records: []wikigit.Result{committed(ago(120*time.Hour), wikigit.PushOK), committed(ago(97*time.Hour), wikigit.PushPending), committed(ago(73*time.Hour), wikigit.PushPending), committed(ago(time.Hour), wikigit.PushPending)},
			commit: "PASS", push: "FAIL", pushHas: "(4 days)", fix: "", retry: true,
			notice: "eva.md: the wiki has not been pushed for 4 days (pending); run eva-md doctor."},
		{name: "diverged", records: []wikigit.Result{committed(ago(5*time.Hour), wikigit.PushDiverged)},
			commit: "PASS", push: "FAIL", pushHas: "the remote has commits this clone lacks",
			notice: "eva.md: the wiki's remote has commits this clone lacks, so pushes stopped; run eva-md doctor."},
		{name: "refused by the remote", records: []wikigit.Result{committed(ago(5*time.Hour), wikigit.PushFailed)},
			commit: "PASS", push: "FAIL", pushHas: "the remote refused it"},
		{name: "last month too", records: []wikigit.Result{{At: clock.AddDate(0, -1, 0).Format(time.RFC3339), Outcome: wikigit.Committed, Pages: 2, Push: wikigit.PushOK}},
			commit: "PASS", push: "PASS", commitHas: "committed (2 pages)"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := isolated(t)
			c := wikiConfig(t)
			plantCommits(t, d, tc.records...)
			var r Report
			inspectCommits(&r, d, c, clock)
			commit, push := findRow(t, r, "daily commit"), findRow(t, r, "push")
			if commit.Status != tc.commit || push.Status != tc.push || !strings.Contains(commit.Detail, tc.commitHas) || !strings.Contains(push.Detail, tc.pushHas) {
				t.Fatalf("rows\n%+v\n%+v", commit, push)
			}
			if tc.fix != "" && !strings.Contains(commit.Fix+push.Fix, tc.fix) {
				t.Fatalf("fixes %q %q lack %q", commit.Fix, push.Fix, tc.fix)
			}
			if got := backgroundNotice(d, c, clock); got != tc.notice {
				t.Fatalf("notice %q, want %q", got, tc.notice)
			}
			if got := RetryPush(d, clock); got != tc.retry {
				t.Fatalf("retry %v, want %v", got, tc.retry)
			}
		})
	}
	d := isolated(t)
	c := wikiConfig(t)
	c.Commits = false
	plantCommits(t, d, committed(ago(time.Hour), wikigit.PushDiverged))
	var r Report
	inspectCommits(&r, d, c, clock)
	if row := findRow(t, r, "daily commit"); row.Status != "UNKNOWN" || !strings.Contains(row.Detail, "commits = false") || backgroundNotice(d, c, clock) != "" {
		t.Fatalf("commits = false: %+v", row)
	}
}

// The notice and the push retry read only the end of the newest files: a
// month of records past the window doesn't change what the newest say, and
// a line cut by the window is skipped.
func TestRecordsAreReadFromTheEnd(t *testing.T) {
	d := isolated(t)
	c := wikiConfig(t)
	var old []wikigit.Result
	for i := 0; i < 400; i++ {
		old = append(old, wikigit.Result{At: clock.Add(-time.Duration(800-i) * time.Minute).Format(time.RFC3339), Outcome: wikigit.Refused, Reason: "origin", Push: wikigit.PushSkipped, Unstaged: []string{strings.Repeat("x", 60)}})
	}
	plantCommits(t, d, old...)
	plantCommits(t, d, wikigit.Result{At: ago(time.Minute), Outcome: wikigit.Committed, Pages: 1, Push: wikigit.PushPending})
	if info, _ := os.Stat(filepath.Join(d.State, "commits", clock.Format("2006-01")+".jsonl")); info.Size() < 2*noticeCommit {
		t.Fatalf("the fixture is only %d bytes", info.Size())
	}
	records, err := commitRecords(d.State, clock, noticeCommit)
	if err != nil || len(records) == 0 || len(records) > noticeCommit/100 || records[len(records)-1].Push != wikigit.PushPending {
		t.Fatalf("%d records, %v", len(records), err)
	}
	if !RetryPush(d, clock) || backgroundNotice(d, c, clock) != "" {
		t.Fatal("the newest record was not the one read")
	}
	data, err := update.ReadTail(filepath.Join(d.State, "commits"), clock.Format("2006-01")+".jsonl", 1000)
	if err != nil || len(data) > 1000 || len(data) == 0 || data[0] != '{' {
		t.Fatalf("tail %d bytes %v: %.40q", len(data), err, data)
	}
}

// The lint rows read <state>/lint.json: pages over 512 KB and refs that
// resolve to nothing fail, a clean result passes, and a lint that hasn't run
// or can't be read is unknown.
func TestLintRows(t *testing.T) {
	d := isolated(t)
	var r Report
	inspectLint(&r, d)
	if row := findRow(t, r, "oversize pages"); row.Status != "UNKNOWN" || !strings.Contains(row.Detail, "has not run yet") {
		t.Fatalf("no lint: %+v", row)
	}
	put(t, filepath.Join(d.State, "lint.json"), []byte("{not json"))
	r = Report{}
	inspectLint(&r, d)
	if row := findRow(t, r, "source refs"); row.Status != "UNKNOWN" || !strings.Contains(row.Detail, "unreadable") {
		t.Fatalf("bad lint: %+v", row)
	}
	for _, tc := range []struct {
		result       lint.Result
		size, refs   string
		sizeHas, has string
	}{
		{lint.Result{At: ago(time.Hour), Oversize: []string{}, BadRefPages: []string{}, Pages: 12}, "PASS", "PASS", "none of 12 thread pages", "every ref in 12 thread pages resolves"},
		{lint.Result{At: ago(time.Hour), Oversize: []string{"projects/a/threads/main", "projects/b/threads/main", "projects/c/threads/main", "projects/d/threads/main"}, BadRefs: 7, BadRefPages: []string{"general/threads/default"}, Pages: 12},
			"FAIL", "FAIL", "4 of 12 thread pages over 512 KB: projects/a/threads/main, projects/b/threads/main, projects/c/threads/main and 1 more", "7 refs in 1 page name inbox lines that are gone: general/threads/default"},
	} {
		data, _ := json.Marshal(tc.result)
		put(t, filepath.Join(d.State, "lint.json"), data)
		r = Report{}
		inspectLint(&r, d)
		size, refs := findRow(t, r, "oversize pages"), findRow(t, r, "source refs")
		if size.Status != tc.size || refs.Status != tc.refs || !strings.Contains(size.Detail, tc.sizeHas) || !strings.Contains(refs.Detail, tc.has) || !strings.Contains(refs.Detail, "(lint of ") {
			t.Fatalf("rows\n%+v\n%+v", size, refs)
		}
	}
}

// The system map row checks the map against the folder: every page on the
// map passes; a page left out, a listed page gone or a missing map fails;
// no system pages is unknown. Only the map and the folder's names are read.
func TestSystemMapRow(t *testing.T) {
	isolated(t)
	c := wikiConfig(t)
	dir := filepath.Join(c.WikiPath, "systems")
	row := func() Check {
		var r Report
		inspectSystems(&r, c)
		return findRow(t, r, "system map")
	}
	if got := row(); got.Status != "UNKNOWN" || got.Detail != "no system pages yet" {
		t.Fatalf("no folder: %+v", got)
	}
	page := func(slug string) {
		// A page okf could not read: the row never reads one.
		if err := os.WriteFile(filepath.Join(dir, slug+".md"), []byte("not a page"), 0o000); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	page("orders")
	page("billing")
	if got := row(); got.Status != "FAIL" || !strings.Contains(got.Detail, "systems/map.md is missing") {
		t.Fatalf("no map: %+v", got)
	}
	writeMap := func(slugs ...string) {
		text := "---\ntype: map\ntitle: System map\n---\n\n# System map\n\n"
		for _, s := range slugs {
			text += fmt.Sprintf("- [%s \\[v2\\]](%s.md): Takes it · repos: github.com__example__%s · as of 2026-10-30\n", strings.ToUpper(s), s, s)
		}
		put(t, filepath.Join(dir, "map.md"), []byte(text))
	}
	writeMap("billing", "orders")
	if got := row(); got.Status != "PASS" || got.Detail != "2 system pages, all on the map" {
		t.Fatalf("consistent: %+v", got)
	}
	writeMap("orders", "shipping")
	if got := row(); got.Status != "FAIL" || !strings.Contains(got.Detail, "leaves out 1 of 2 system pages: billing") || !strings.Contains(got.Detail, "it lists 1 page that no longer exists: shipping") || !strings.Contains(got.Fix, "system write") {
		t.Fatalf("inconsistent: %+v", got)
	}
}

// eva-md status adds what the wiki holds, from folder names, and the
// sessions whose entries wait for an update, from their watermarks.
func TestStatusRows(t *testing.T) {
	d := isolated(t)
	c := wikiConfig(t)
	if err := config.Save(d.ConfigFile(), c); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"general/threads/default.md", "general/threads/index.md", "projects/a/threads/main.md", "projects/a/threads/feature-x.md", "projects/a/now.md", "projects/b/threads/main.md", "projects/index.md"} {
		put(t, filepath.Join(c.WikiPath, f), []byte("x"))
	}
	store := inbox.Store{Root: d.Inbox()}
	for i, id := range []string{"one", "two", "three"} {
		file := store.Path("github.com__example__a", "codex", id)
		put(t, file, []byte(strings.Repeat("{}\n", 10)))
		switch i {
		case 0: // fully processed
			if err := store.SetMark(file, inbox.Mark{Offset: 30}); err != nil {
				t.Fatal(err)
			}
		case 1: // too small to process until it grows
			if err := store.SetMark(file, inbox.Mark{Offset: 0, Wait: 30}); err != nil {
				t.Fatal(err)
			}
		}
	}
	r := Status(t.Context(), d, "v0.7.2")
	if row := findRow(t, r, "wiki contents"); row.Status != "PASS" || row.Detail != "2 projects, 4 threads" {
		t.Fatalf("contents: %+v", row)
	}
	if row := findRow(t, r, "update backlog"); row.Status != "PASS" || row.Detail != "1 session waits for an update" {
		t.Fatalf("backlog: %+v", row)
	}
	for _, name := range []string{"codex updates", "daily commit", "push", "oversize pages", "system map"} {
		findRow(t, r, name)
	}
	for _, c := range Inspect(t.Context(), d, "v0.7.2", false).Checks {
		if c.Name == "wiki contents" || c.Name == "update backlog" {
			t.Fatalf("the doctor's checks hold a status row: %+v", c)
		}
	}
}

// eva-md status names the session files whose watermark can't be read or is
// past the end of the file, which the hooks never start an update for.
func TestStatusNamesUnusableWatermarks(t *testing.T) {
	d := isolated(t)
	c := wikiConfig(t)
	if err := config.Save(d.ConfigFile(), c); err != nil {
		t.Fatal(err)
	}
	store := inbox.Store{Root: d.Inbox()}
	for _, id := range []string{"0199a000-past", "0199b000-garbled", "0199c000-fine"} {
		file := store.Path("github.com__example__a", "codex", id)
		put(t, file, []byte(strings.Repeat("{}\n", 10)))
		switch id {
		case "0199a000-past":
			if err := store.SetMark(file, inbox.Mark{Offset: 31}); err != nil {
				t.Fatal(err)
			}
		case "0199b000-garbled":
			put(t, filepath.Join(filepath.Dir(file), ".state", filepath.Base(file)+".wm"), []byte("garbage\n"))
		}
	}
	row := findRow(t, Status(t.Context(), d, "v0.7.2"), "update backlog")
	if row.Status != "FAIL" || !strings.HasPrefix(row.Detail, "1 session waits for an update; 2 sessions have a watermark that can't be read or is past the end of its file: ") ||
		!strings.Contains(row.Detail, "codex:0199a000") || !strings.Contains(row.Detail, "codex:0199b000") || !strings.Contains(row.Fix, "delete each damaged watermark") {
		t.Fatalf("backlog: %+v", row)
	}
}

// The backlog's listing of a project folder stops at the entries left of the
// bound, and says it was cut.
func TestBacklogListingIsBounded(t *testing.T) {
	d := isolated(t)
	dir := filepath.Join(d.Inbox(), "github.com__example__a")
	for i := range 10 {
		put(t, filepath.Join(dir, fmt.Sprintf("codex-%02d.jsonl", i)), []byte("{}\n"))
	}
	left := 4
	if files, all := newestSessions(dir, &left); len(files) != 4 || all || left != 0 {
		t.Fatalf("cut listing: %d files, all %v, %d left", len(files), all, left)
	}
	left = 100
	if files, all := newestSessions(dir, &left); len(files) != 10 || !all || left != 90 {
		t.Fatalf("whole listing: %d files, all %v, %d left", len(files), all, left)
	}
}

// With nothing wrong in the records, the start-of-chat notice stays quiet,
// and without a wiki the records are not read at all.
func TestQuickNoticeQuietWhenHealthy(t *testing.T) {
	d := isolated(t)
	c := wikiConfig(t)
	put(t, filepath.Join(c.WikiPath, "index.md"), []byte("# Wiki"))
	now := time.Now()
	plantRunLog(t, d, okRecord(now.Format(time.RFC3339), update.HostClaude, "a"))
	plantCommits(t, d, wikigit.Result{At: now.Format(time.RFC3339), Outcome: wikigit.Committed, Pages: 1, Push: wikigit.PushOK})
	if got := QuickNotice(t.Context(), d, c, "codex"); got != "" {
		t.Fatalf("healthy notice %q", got)
	}
	plantCommits(t, d, wikigit.Result{At: now.Format(time.RFC3339), Outcome: wikigit.Committed, Pages: 1, Push: wikigit.PushDiverged})
	if got := QuickNotice(t.Context(), d, c, "codex"); !strings.Contains(got, "pushes stopped") {
		t.Fatalf("diverged notice %q", got)
	}
	c.WikiPath = ""
	if got := backgroundNotice(d, c, now); got != "" {
		t.Fatalf("no wiki, notice %q", got)
	}
}

// leftRecord is a job's record of a chat from before updates it couldn't
// set aside yet.
func leftRecord(at, session string) update.Record {
	return update.Record{At: at, Host: update.HostCodex, Session: session, Outcome: update.OutcomeFailed, Reason: update.ReasonLeft}
}
