package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/heyNag/eva-md/internal/config"
	"github.com/heyNag/eva-md/internal/hook"
	"github.com/heyNag/eva-md/internal/inbox"
	"github.com/heyNag/eva-md/internal/paths"
	"github.com/heyNag/eva-md/internal/session"
	"github.com/heyNag/eva-md/internal/update"
	"github.com/heyNag/eva-md/internal/work"
)

// eva-md work: the wake, the tick and the status, and the worker's two
// jobs, v1's update and maintain, run in the test's process.

// workHangGuard is how long a test waits for what must happen however slow
// the machine before it reports a hang.
const workHangGuard = time.Minute

// evaWork runs eva-md with args and returns its exit code and output.
func evaWork(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	var out, errOut bytes.Buffer
	code := run(context.Background(), args, strings.NewReader(""), &out, &errOut)
	return code, out.String(), errOut.String()
}

// workerOn turns the switch worker on in the home's config, and with
// change changes the config too.
func workerOn(t *testing.T, d paths.Dirs, change func(*config.Config)) {
	t.Helper()
	c, err := config.Load(d.ConfigFile())
	if err != nil {
		t.Fatal(err)
	}
	c.Switches = map[string]any{"worker": true}
	if change != nil {
		change(&c)
	}
	if err := config.Save(d.ConfigFile(), c); err != nil {
		t.Fatal(err)
	}
}

// chatFile writes a chat of four entries, its own thread on branch, to the
// inbox, as the hooks would have captured it at at, and returns its path.
func chatFile(t *testing.T, d paths.Dirs, host, id, branch string, at time.Time) string {
	t.Helper()
	var lines []byte
	for i, kind := range []string{"prompt", "reply", "prompt", "reply"} {
		role := map[string]string{"prompt": "user", "reply": "assistant"}[kind]
		e := inbox.Entry{V: 1, TS: at.Add(time.Duration(i) * time.Minute).Format(time.RFC3339), Host: host, Session: id,
			Role: role, Kind: kind, Text: "Plan the tangerine migration, part " + strconv.Itoa(i) + ", with the switch kept behind a flag. " + strings.Repeat("Context. ", 40),
			Project: "github.com__you__demo", Branch: branch, Origin: "hook"}
		data, err := json.Marshal(e)
		if err != nil {
			t.Fatal(err)
		}
		lines = append(append(lines, data...), '\n')
	}
	file := inbox.Store{Root: d.Inbox()}.Path("github.com__you__demo", host, id)
	if err := os.MkdirAll(filepath.Dir(file), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, lines, 0o600); err != nil {
		t.Fatal(err)
	}
	return file
}

// wakeMarks lists the marks waiting in the worker's wake folder, by file
// name.
func wakeMarks(t *testing.T, d paths.Dirs) []string {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(d.State, "work", "wake"))
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names
}

// journal reads the worker's journal, every month's file.
func journal(t *testing.T, d paths.Dirs) string {
	t.Helper()
	files, _ := filepath.Glob(filepath.Join(d.State, "work", "journal", "*.jsonl"))
	var all strings.Builder
	for _, f := range files {
		all.Write(mustRead(t, f))
	}
	return all.String()
}

// updated lists the sessions the run log of the last two days has a record
// of, by host:id, the id as their entries name it, with the trigger of
// each session's newest.
func updated(t *testing.T, d paths.Dirs) map[string]string {
	t.Helper()
	records, err := update.Days(d.State, time.Now(), 2)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]string{}
	for _, r := range records {
		if r.Host != "" {
			out[r.Host+":"+r.Session] = r.Trigger
		}
	}
	return out
}

// heldRunner makes the fake runner answer with tangerine, hold its first
// call until release is closed, and count the most calls it ran at once.
// entered is closed when the first call starts.
func heldRunner(fake *update.Fake) (entered, release chan struct{}, most *atomic.Int32) {
	entered, release, most = make(chan struct{}), make(chan struct{}), &atomic.Int32{}
	var inFlight atomic.Int32
	var once sync.Once
	fake.Respond = func(update.Request) (update.Result, error) {
		n := inFlight.Add(1)
		defer inFlight.Add(-1)
		for m := most.Load(); n > m && !most.CompareAndSwap(m, n); m = most.Load() {
		}
		once.Do(func() { close(entered); <-release })
		return update.Result{Output: []byte(tangerine), Attempts: 1}, nil
	}
	return entered, release, most
}

// await waits for ch, failing the test as hung after workHangGuard.
func await[T any](t *testing.T, ch <-chan T, what string) T {
	t.Helper()
	select {
	case v := <-ch:
		return v
	case <-time.After(workHangGuard):
		t.Fatalf("%s: no answer after %s", what, workHangGuard)
	}
	var zero T
	return zero
}

// eva-md work refuses arguments it doesn't take, exiting 2, before it
// reads a config or writes a mark.
func TestWorkRefusesBadArguments(t *testing.T) {
	d := isolated(t)
	for _, args := range [][]string{
		{"work"},
		{"work", "--wake", "--tick"},
		{"work", "--status", "--tick"},
		{"work", "--wake", "--session", "codex:a"},
		{"work", "--wake", "--reason", "Session_End", "--session", "codex:a"},
		{"work", "--wake", "--reason", "whenever", "--session", "codex:a"},
		{"work", "--wake", "--reason", "session_end"},
		{"work", "--wake", "--reason", "session_end", "--session", "other:a"},
		{"work", "--wake", "--reason", "session_end", "--session", "codex:"},
		{"work", "--wake", "--reason", "session_end", "--session", "codex"},
		{"work", "--wake", "--reason", "chat_start", "--sweep"},
		{"work", "--wake", "--reason", "chat_start", "--sweep=2026-13-01"},
		{"work", "--wake", "--reason", "push_retry", "--push-only"},
		{"work", "--tick", "--reason", "tick"},
		{"work", "--status", "--maintain"},
		{"work", "--tick", "extra"},
	} {
		if code, out, errOut := evaWork(t, args...); code != 2 || out != "" || !strings.HasPrefix(errOut, "eva-md: ") {
			t.Errorf("%q: %d %q %q", args, code, out, errOut)
		}
	}
	if _, err := os.Stat(filepath.Join(d.State, "work")); !os.IsNotExist(err) {
		t.Fatalf("a refused wake made the worker's folder: %v", err)
	}
}

// With the switch off, absent or set false, a wake and a tick do nothing
// and say so, exiting 0: no mark, no journal.
func TestWorkDoesNothingWithTheSwitchOff(t *testing.T) {
	d, fake := updateFixture(t, tangerine)
	chatFile(t, d, update.HostClaude, "switched-off", "main", time.Now().Add(-time.Hour))
	for _, body := range []string{"", "[switches]\nworker = false\n"} {
		f, err := os.OpenFile(d.ConfigFile(), os.O_APPEND|os.O_WRONLY, 0)
		if err != nil {
			t.Fatal(err)
		}
		f.WriteString(body)
		f.Close()
		for _, args := range [][]string{{"work", "--wake", "--reason", "session_end", "--session", "claude-code:switched-off", "--maintain"}, {"work", "--tick"}} {
			if code, out, errOut := evaWork(t, args...); code != 0 || out != "work: the worker is off; [switches] worker = true in the config turns it on\n" || errOut != "" {
				t.Fatalf("%q: %d %q %q", args, code, out, errOut)
			}
		}
	}
	if _, err := os.Stat(filepath.Join(d.State, "work")); !os.IsNotExist(err) || len(fake.Requests()) != 0 {
		t.Fatalf("the worker ran with the switch off: %v, %d calls", err, len(fake.Requests()))
	}
}

// A session's key is <host>-<id> when the worker takes that as it is, and
// otherwise a lower-case name made from the id, the same every time, that
// tells two ids apart that differ only in letter case; the key made from a
// session's inbox file is the key made from its id.
func TestSessionKeys(t *testing.T) {
	long := strings.Repeat("a", 130)
	for _, id := range []string{"plain-id_1.2", "UpperCase", "uppercase", "with space/and:colon", ".hidden", long, "Mixed" + long} {
		key := sessionKey(update.HostCodex, id)
		if safe, err := paths.SafeName(key); err != nil || safe != key || strings.ToLower(key) != key || !strings.HasPrefix(key, "codex-") {
			t.Errorf("%q: key %q is no key the worker takes", id, key)
		}
		if again := sessionKey(update.HostCodex, id); again != key {
			t.Errorf("%q: key %q, then %q", id, key, again)
		}
		file := filepath.Base(inbox.Store{Root: "/inbox"}.Path("p", update.HostCodex, id))
		sid := strings.TrimSuffix(strings.TrimPrefix(file, "codex-"), ".jsonl")
		if fileKey(update.HostCodex, sid) != key {
			t.Errorf("%q: the inbox file %q gives key %q, the id %q", id, file, fileKey(update.HostCodex, sid), key)
		}
	}
	if sessionKey(update.HostCodex, "plain-id_1.2") != "codex-plain-id_1.2" {
		t.Error("a plain id is not its own key")
	}
	// The pairs: one id that is its own key, and two that both take the
	// hashed key.
	for _, pair := range [][2]string{{"UpperCase", "uppercase"}, {"CaseOnly", "CASEONLY"}, {"Mixed Case", "mixed CASE"}} {
		if a, b := sessionKey(update.HostCodex, pair[0]), sessionKey(update.HostCodex, pair[1]); a == b || strings.EqualFold(a, b) {
			t.Errorf("ids that differ in letter case share a key: %q, %q", a, b)
		}
	}
	if a, b := sessionKey(update.HostCodex, "same"), sessionKey(update.HostClaude, "same"); a == b {
		t.Errorf("two hosts share a key: %q", a)
	}
	for _, host := range []string{"", "other", "Codex"} {
		if key := sessionKey(host, "a"); key != "" {
			t.Errorf("host %q: key %q", host, key)
		}
	}
}

// A session whose id holds an upper-case letter, and one whose id holds
// characters paths.SafeName changes, are each woken and updated; two ids
// that differ only in letter case get two marks, and the one with no
// inbox file of its own is done with nothing to update. While a run of
// update holds update.lock the job is busy and every mark waits; a tick
// runs them all, in one call for their one reason.
func TestUpperCaseAndUnsafeIdsAreWokenAndUpdated(t *testing.T) {
	d, fake := updateFixture(t, tangerine)
	workerOn(t, d, nil)
	at := time.Now().Add(-time.Hour)
	ids := map[string]string{"CaseOnly": "one", "with space/and:colon": "two", "MixedCase-ID": "three"}
	for id, branch := range ids {
		chatFile(t, d, update.HostClaude, id, branch, at)
	}
	unlock, busy, err := update.TryLock(d.Locks(), "update.lock")
	if err != nil || busy {
		t.Fatal(busy, err)
	}
	for _, id := range []string{"CaseOnly", "caseonly", "with space/and:colon", "MixedCase-ID"} {
		if code, out, errOut := evaWork(t, "work", "--wake", "--reason", "session_end", "--session", "claude-code:"+id); code != 0 || out != "" || errOut != "" {
			t.Fatalf("wake %q: %d %q %q", id, code, out, errOut)
		}
	}
	marks := wakeMarks(t, d)
	lower := map[string]bool{}
	for _, m := range marks {
		lower[strings.ToLower(m)] = true
	}
	if len(marks) != 4 || len(lower) != 4 || len(fake.Requests()) != 0 {
		t.Fatalf("marks %q, %d calls", marks, len(fake.Requests()))
	}
	if !strings.Contains(journal(t, d), `"outcome":"busy"`) {
		t.Fatalf("no busy run journaled:\n%s", journal(t, d))
	}
	unlock()
	if code, out, errOut := evaWork(t, "work", "--tick"); code != 0 || out != "" || errOut != "" {
		t.Fatalf("tick: %d %q %q", code, out, errOut)
	}
	if marks := wakeMarks(t, d); len(marks) != 0 || len(fake.Requests()) != 3 {
		t.Fatalf("after the tick: marks %q, %d calls", marks, len(fake.Requests()))
	}
	got := updated(t, d)
	for id := range ids {
		if trigger, ok := got["claude-code:"+id]; !ok || trigger != update.TriggerEnd {
			t.Errorf("%q: run log %v", id, got)
		}
	}
	if len(got) != 3 {
		t.Errorf("run log %v", got)
	}
}

// With a fake runner held on a channel, 50 wakes for 50 sessions during
// one update all reach the worker: after release every session's backlog
// has a run-log record, and the runner never ran two calls at once.
func TestFiftyWakesDuringOneUpdateLoseNone(t *testing.T) {
	d, fake := updateFixture(t, tangerine)
	workerOn(t, d, func(c *config.Config) { c.DailyCap = 100 })
	entered, release, most := heldRunner(fake)
	at := time.Now().Add(-time.Hour)
	const n = 50
	for i := range n {
		chatFile(t, d, update.HostCodex, "fifty-"+strconv.Itoa(i), "branch-"+strconv.Itoa(i), at)
	}
	wake := func(i int) int {
		code, _, _ := evaWork(t, "work", "--wake", "--reason", "stop_size", "--session", "codex:fifty-"+strconv.Itoa(i))
		return code
	}
	first := make(chan int, 1)
	go func() { first <- wake(0) }()
	await(t, entered, "the first update's call")
	rest := make(chan int, n-1)
	for i := 1; i < n; i++ {
		go func() { rest <- wake(i) }()
	}
	// All but one find the worker and its waiter busy and return at once;
	// the one left waits for the worker.
	for i := 0; i < n-2; i++ {
		if code := await(t, rest, "a wake during the update"); code != 0 {
			t.Fatalf("a wake exited %d", code)
		}
	}
	close(release)
	if code := await(t, first, "the first wake"); code != 0 {
		t.Fatalf("the first wake exited %d", code)
	}
	if code := await(t, rest, "the waiter"); code != 0 {
		t.Fatalf("the waiter exited %d", code)
	}
	got := updated(t, d)
	for i := range n {
		if trigger, ok := got["codex:fifty-"+strconv.Itoa(i)]; !ok || trigger != update.TriggerStop {
			t.Errorf("session %d has no run-log record: %q", i, trigger)
		}
	}
	if m := most.Load(); m != 1 || len(fake.Requests()) != n {
		t.Fatalf("at most %d calls at once, %d calls", m, len(fake.Requests()))
	}
	if marks := wakeMarks(t, d); len(marks) != 0 {
		t.Fatalf("marks left: %q", marks)
	}
}

// A person's update --foreground holding update.lock: the worker's update
// job ends busy and its marks stay; once the person's run ends, a tick
// runs them, with the trigger the wake gave.
func TestAForegroundUpdateMakesTheWorkerBusy(t *testing.T) {
	d, fake := updateFixture(t, tangerine)
	workerOn(t, d, nil)
	entered, release, _ := heldRunner(fake)
	at := time.Now().Add(-time.Hour)
	chatFile(t, d, update.HostClaude, "person", "main", at)
	chatFile(t, d, update.HostClaude, "worker", "other", at)
	person := make(chan int, 1)
	go func() {
		code, _, _ := evaWork(t, "update", "--session", "claude-code:person", "--foreground")
		person <- code
	}()
	await(t, entered, "the person's update")
	if code, out, errOut := evaWork(t, "work", "--wake", "--reason", "compact", "--session", "claude-code:worker"); code != 0 || out != "" || errOut != "" {
		t.Fatalf("wake: %d %q %q", code, out, errOut)
	}
	if marks := wakeMarks(t, d); !slices.Equal(marks, []string{"update-claude-code-worker"}) {
		t.Fatalf("marks %q", marks)
	}
	if j := journal(t, d); !strings.Contains(j, `"kind":"update"`) || !strings.Contains(j, `"outcome":"busy"`) {
		t.Fatalf("journal:\n%s", j)
	}
	close(release)
	if code := await(t, person, "the person's update"); code != 0 {
		t.Fatalf("the person's update exited %d", code)
	}
	if code, out, errOut := evaWork(t, "work", "--tick"); code != 0 || out != "" || errOut != "" {
		t.Fatalf("tick: %d %q %q", code, out, errOut)
	}
	got := updated(t, d)
	if got["claude-code:worker"] != update.TriggerCompact || got["claude-code:person"] != update.TriggerManual || len(fake.Requests()) != 2 {
		t.Fatalf("run log %v, %d calls", got, len(fake.Requests()))
	}
	if marks := wakeMarks(t, d); len(marks) != 0 {
		t.Fatalf("marks left: %q", marks)
	}
}

// The maintain job runs v1's maintain for the day's mark, and only the
// push for a push retry's, its lines to the day's log; while another holds
// maintain.lock it is busy and its mark waits for the next tick.
func TestTheMaintainJob(t *testing.T) {
	d := isolated(t)
	c := config.Default()
	if err := config.Save(d.ConfigFile(), c); err != nil {
		t.Fatal(err)
	}
	workerOn(t, d, nil)
	unlock, busy, err := update.TryLock(d.Locks(), "maintain.lock")
	if err != nil || busy {
		t.Fatal(busy, err)
	}
	if code, out, errOut := evaWork(t, "work", "--wake", "--reason", "daily", "--maintain"); code != 0 || out != "" || errOut != "" {
		t.Fatalf("wake: %d %q %q", code, out, errOut)
	}
	if marks := wakeMarks(t, d); !slices.Equal(marks, []string{"maintain-daily"}) {
		t.Fatalf("marks %q", marks)
	}
	unlock()
	if code, _, _ := evaWork(t, "work", "--tick"); code != 0 || len(wakeMarks(t, d)) != 0 {
		t.Fatalf("tick: %d, marks %q", code, wakeMarks(t, d))
	}
	logs, _ := filepath.Glob(filepath.Join(d.Logs(), "update-*.log"))
	var log string
	for _, f := range logs {
		log += string(mustRead(t, f))
	}
	// Without a wiki, maintain keeps the search index: the full run.
	if !strings.Contains(log, "maintain: busy") || !strings.Contains(log, "maintain: index: ") {
		t.Fatalf("log:\n%s", log)
	}
	// A push retry alone runs maintain --push-only, which needs a wiki.
	if code, _, _ := evaWork(t, "work", "--wake", "--reason", "push_retry", "--maintain", "--push-only"); code != 0 {
		t.Fatal(code)
	}
	if j := journal(t, d); !strings.Contains(j, `"reasons":["push_retry"],"hosts":[],"outcome":"failed"`) {
		t.Fatalf("journal:\n%s", j)
	}
}

// workNow is the time the status fixtures are judged at.
var workNow = time.Date(2026, 11, 14, 12, 0, 0, 0, time.Local)

// plantedMarker is planted in the status fixtures' entries and session
// ids; no line eva-md work --status prints, and none of the journal, may
// hold it.
const plantedMarker = "zqxplanted"

// statusFixture makes a home with a wiki, the switch on and eva-md work
// --status's clock pinned to workNow, and with held, Claude Code held after
// auth, a Codex session quarantined, Codex's calls past daily_cap = 1, a
// Codex chat off the record and a maintain mark written two hours before;
// without it, Codex backing off after a rate limit and a Claude Code
// session free but for the size or quiet rule. It returns the folders and
// the session ids.
func statusFixture(t *testing.T, held bool) (paths.Dirs, []string) {
	t.Helper()
	d, _ := updateFixture(t, tangerine)
	saved := workClock
	workClock = func() time.Time { return workNow }
	t.Cleanup(func() { workClock = saved })
	workerOn(t, d, func(c *config.Config) {
		if held {
			c.DailyCap = 1
		}
	})
	var ids []string
	var log []byte
	chat := func(host, id string, records int, reason string, off bool) {
		id += "-" + plantedMarker
		ids = append(ids, host+":"+id)
		chatFile(t, d, host, id, "main", workNow.Add(-3*time.Hour))
		for range records {
			line, err := json.Marshal(update.Record{At: workNow.Format(time.RFC3339), Host: host, Session: id, Outcome: update.OutcomeFailed, Reason: reason, Calls: 1})
			if err != nil {
				t.Fatal(err)
			}
			log = append(append(log, line...), '\n')
		}
		if off {
			if err := (session.Markers{Root: d.Sessions()}).Set(host, id, session.OffRecord, "1"); err != nil {
				t.Fatal(err)
			}
		}
	}
	if held {
		chat(update.HostClaude, "c1", 1, update.ReasonAuth, false)
		chat(update.HostCodex, "q/x", 3, update.ReasonInvalidOutput, false)
		chat(update.HostCodex, "b", 0, "", false)
		chat(update.HostCodex, "off", 0, "", true)
		for _, lock := range []string{"worker.lock", "waiter.lock"} {
			unlock, busy, err := paths.TryLock(d.Locks(), lock)
			if err != nil || busy {
				t.Fatal(busy, err)
			}
			defer unlock()
		}
		if err := work.Wake(t.Context(), d, []work.Mark{{Kind: maintainKind, Key: dailyKey, Reason: "daily"}}, work.Options{Now: func() time.Time { return workNow.Add(-2 * time.Hour) }}); err != nil {
			t.Fatal(err)
		}
	} else {
		chat(update.HostCodex, "r", 1, update.ReasonRateLimit, false)
		chat(update.HostClaude, "c2", 0, "", false)
	}
	file := filepath.Join(d.State, "updates", workNow.Format(time.DateOnly)+".jsonl")
	if err := os.MkdirAll(filepath.Dir(file), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, log, 0o600); err != nil {
		t.Fatal(err)
	}
	return d, ids
}

// On fixture run logs and inboxes, eva-md work --status gives each of the
// six reasons, by the jobs eva-md registers, with the oldest wait, and for
// a hold its class, its host and its next try: for a backoff, the end v1's
// update stops by. Once the planted sessions are woken and run, neither
// its lines nor the journal hold the marker planted in their entries and
// ids.
func TestWorkStatusGivesEachReason(t *testing.T) {
	for _, tc := range []struct {
		held bool
		want string
	}{
		{true, "work: behind a running job: 1, the oldest for 2 hours\n" +
			"work: deferred by the day's budget: 1, the oldest for 3 hours\n" +
			"work: held after a failure: 1, the oldest for 3 hours, quarantined on codex until 2026-11-15 00:00:00\n" +
			"work: waiting for sign-in: 1, the oldest for 3 hours, auth on claude-code\n" +
			"work: switched off, paused by the owner or excluded: 1, the oldest for 3 hours\n"},
		{false, "work: waiting for the size or quiet rule: 1, the oldest for 3 hours\n" +
			"work: held after a failure: 1, the oldest for 3 hours, rate_limit on codex until 2026-11-14 12:05:00\n"},
	} {
		d, ids := statusFixture(t, tc.held)
		code, out, errOut := evaWork(t, "work", "--status")
		if code != 0 || out != tc.want || errOut != "" {
			t.Errorf("held %v: %d\n%s\nwant\n%s%q", tc.held, code, out, tc.want, errOut)
		}
		if !tc.held {
			records, err := update.Recent(d.State, workNow, 1<<20)
			if err != nil {
				t.Fatal(err)
			}
			if end := update.Host(records, update.HostCodex, workNow).BackoffUntil; !strings.Contains(out, "until "+end.Format(time.DateTime)+"\n") {
				t.Errorf("v1's backoff ends at %v: %q", end, out)
			}
		}
		args := []string{"work", "--wake", "--reason", "session_end", "--maintain"}
		for _, id := range ids {
			args = append(args, "--session", id)
		}
		if code, _, errOut := evaWork(t, args...); code != 0 || errOut != "" {
			t.Fatalf("wake: %d %q", code, errOut)
		}
		_, out, _ = evaWork(t, "work", "--status")
		j := journal(t, d)
		if !strings.Contains(j, `"ev":"finish"`) {
			t.Fatalf("held %v: nothing ran:\n%s", tc.held, j)
		}
		if all := strings.ToLower(out + j); strings.Contains(all, plantedMarker) {
			t.Errorf("held %v: the planted marker was printed or journaled:\n%s\n%s", tc.held, out, j)
		}
	}
}

// journalFinishes lists the outcomes of the journal's finish lines of kind.
func journalFinishes(t *testing.T, d paths.Dirs, kind string) []string {
	t.Helper()
	var out []string
	for line := range strings.Lines(journal(t, d)) {
		var e struct{ Ev, Kind, Outcome string }
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			t.Fatalf("journal line %q: %v", line, err)
		}
		if e.Ev == "finish" && e.Kind == kind {
			out = append(out, e.Outcome)
		}
	}
	return out
}

// Each word the hooks carry into a wake form is a reason eva-md work
// --wake takes, with a maintain, a session and a sweep alike, and so is
// tick.
func TestWorkTakesEveryReasonTheHooksWakeFor(t *testing.T) {
	d := isolated(t)
	if err := config.Save(d.ConfigFile(), config.Default()); err != nil {
		t.Fatal(err)
	}
	day := time.Now().Format(time.DateOnly)
	for _, reason := range append(slices.Clone(hook.WakeReasons), "tick") {
		for _, rest := range [][]string{{"--maintain"}, {"--maintain", "--push-only"}, {"--session", "codex:a"}, {"--sweep=" + day}} {
			args := append([]string{"work", "--wake", "--reason", reason}, rest...)
			if code, _, errOut := evaWork(t, args...); code != 0 || errOut != "" {
				t.Errorf("%q: %d %q", args, code, errOut)
			}
		}
	}
}

// The day's sweep through the worker takes an idle chat that has a
// backlog, as v1's update --sweep=DAY does: one call, recorded with the
// trigger sweep, since a sweep alone names none, and the sweep is no
// longer due.
func TestTheSweepRunsThroughTheWorker(t *testing.T) {
	d, fake := updateFixture(t, tangerine)
	workerOn(t, d, nil)
	at := time.Now().Add(-3 * time.Hour)
	file := chatFile(t, d, update.HostCodex, "idle-sweep", "main", at)
	if err := os.Chtimes(file, at, at); err != nil {
		t.Fatal(err)
	}
	day := time.Now().Format(time.DateOnly)
	if !update.SweepDue(d.State, day) {
		t.Fatal("the sweep was not due")
	}
	if code, out, errOut := evaWork(t, "work", "--wake", "--reason", update.TriggerStart, "--sweep="+day); code != 0 || out != "" || errOut != "" {
		t.Fatalf("wake: %d %q %q", code, out, errOut)
	}
	if got := updated(t, d); got["codex:idle-sweep"] != update.TriggerSweep || len(got) != 1 || len(fake.Requests()) != 1 {
		t.Fatalf("run log %v, %d calls", got, len(fake.Requests()))
	}
	if update.SweepDue(d.State, day) {
		t.Fatal("the sweep is still due")
	}
	if marks := wakeMarks(t, d); len(marks) != 0 {
		t.Fatalf("marks left: %q", marks)
	}
}

// A call of the update job names its reason as its trigger, as the hooks
// pass it to v1's update, a word no release names as other, and the sweep
// alone names none, as the hooks start v1's update --sweep=DAY.
func TestTheUpdateJobsTrigger(t *testing.T) {
	refs := []update.Ref{{Host: update.HostCodex, ID: "a"}}
	for _, tc := range []struct {
		reason string
		refs   []update.Ref
		want   string
	}{
		{update.TriggerStart, nil, ""},
		{update.TriggerStart, refs, update.TriggerStart},
		{update.TriggerEnd, refs, update.TriggerEnd},
		{"tick", refs, update.TriggerOther},
	} {
		if got := jobTrigger(tc.reason, tc.refs); got != tc.want {
			t.Errorf("%q with %d sessions: %q, want %q", tc.reason, len(tc.refs), got, tc.want)
		}
	}
}

// Marks of two reasons, left while update.lock is held and run by one
// tick, run as two calls, each recording its own trigger; a mark names its
// host, so a session of the other host with the same id is not updated.
func TestMarksOfTwoReasonsRecordTwoTriggers(t *testing.T) {
	d, fake := updateFixture(t, tangerine)
	workerOn(t, d, nil)
	at := time.Now().Add(-time.Hour)
	chatFile(t, d, update.HostClaude, "ra", "one", at)
	chatFile(t, d, update.HostClaude, "rb", "two", at)
	chatFile(t, d, update.HostCodex, "ra", "three", at)
	unlock, busy, err := update.TryLock(d.Locks(), "update.lock")
	if err != nil || busy {
		t.Fatal(busy, err)
	}
	for _, args := range [][]string{
		{"work", "--wake", "--reason", update.TriggerEnd, "--session", "claude-code:ra"},
		{"work", "--wake", "--reason", update.TriggerStop, "--session", "claude-code:rb"},
	} {
		if code, out, errOut := evaWork(t, args...); code != 0 || out != "" || errOut != "" {
			t.Fatalf("%q: %d %q %q", args, code, out, errOut)
		}
	}
	unlock()
	if code, out, errOut := evaWork(t, "work", "--tick"); code != 0 || out != "" || errOut != "" {
		t.Fatalf("tick: %d %q %q", code, out, errOut)
	}
	got := updated(t, d)
	if got["claude-code:ra"] != update.TriggerEnd || got["claude-code:rb"] != update.TriggerStop || len(got) != 2 || len(fake.Requests()) != 2 {
		t.Fatalf("run log %v, %d calls", got, len(fake.Requests()))
	}
	if outcomes := journalFinishes(t, d, updateKind); !slices.Equal(outcomes, []string{"busy", "busy", "done"}) {
		t.Fatalf("the update job's runs ended %q", outcomes)
	}
}

// A pass that fails makes the update job's run failed in the journal, and
// its mark is used: the next tick runs nothing.
func TestAFailedPassFailsTheUpdateJob(t *testing.T) {
	d, fake := updateFixture(t, "This is not what an update answers.")
	workerOn(t, d, nil)
	chatFile(t, d, update.HostCodex, "fails", "main", time.Now().Add(-time.Hour))
	if code, out, errOut := evaWork(t, "work", "--wake", "--reason", update.TriggerEnd, "--session", "codex:fails"); code != 0 || out != "" || errOut != "" {
		t.Fatalf("wake: %d %q %q", code, out, errOut)
	}
	records, err := update.Days(d.State, time.Now(), 2)
	if err != nil || len(records) != 1 || records[0].Outcome != update.OutcomeFailed {
		t.Fatalf("run log %+v: %v", records, err)
	}
	if outcomes := journalFinishes(t, d, updateKind); !slices.Equal(outcomes, []string{"failed"}) {
		t.Fatalf("the update job's runs ended %q", outcomes)
	}
	calls := len(fake.Requests())
	if code, _, _ := evaWork(t, "work", "--tick"); code != 0 || len(fake.Requests()) != calls || len(wakeMarks(t, d)) != 0 {
		t.Fatalf("tick: %d, %d calls after %d, marks %q", code, len(fake.Requests()), calls, wakeMarks(t, d))
	}
}

// A session marked and waiting behind a busy worker is counted once, as
// behind a running job, not also as waiting for the size or quiet rule for
// its backlog; a session with a backlog and no mark still waits for the
// rule.
func TestAMarkedSessionWaitsForOneReason(t *testing.T) {
	d, _ := updateFixture(t, tangerine)
	saved := workClock
	workClock = func() time.Time { return workNow }
	t.Cleanup(func() { workClock = saved })
	workerOn(t, d, nil)
	chatFile(t, d, update.HostCodex, "Marked one", "main", workNow.Add(-3*time.Hour))
	chatFile(t, d, update.HostClaude, "unmarked", "other", workNow.Add(-3*time.Hour))
	for _, lock := range []string{"worker.lock", "waiter.lock"} {
		unlock, busy, err := paths.TryLock(d.Locks(), lock)
		if err != nil || busy {
			t.Fatal(busy, err)
		}
		defer unlock()
	}
	if err := work.Wake(t.Context(), d, []work.Mark{{Kind: updateKind, Key: sessionKey(update.HostCodex, "Marked one"), Reason: update.TriggerEnd, Host: update.HostCodex}},
		work.Options{Now: func() time.Time { return workNow.Add(-2 * time.Hour) }}); err != nil {
		t.Fatal(err)
	}
	want := "work: waiting for the size or quiet rule: 1, the oldest for 3 hours\n" +
		"work: behind a running job: 1, the oldest for 2 hours\n"
	if code, out, errOut := evaWork(t, "work", "--status"); code != 0 || out != want || errOut != "" {
		t.Fatalf("%d\n%s\nwant\n%s%q", code, out, want, errOut)
	}
	if rep := work.Status(t.Context(), d, workNow); len(rep.Items) != 2 {
		t.Fatalf("two sessions wait, %d items: %+v", len(rep.Items), rep.Items)
	}
	// A mark a run has taken is being run: its session waits for nothing.
	run := filepath.Join(d.State, "work", "run", "a-run")
	if err := os.MkdirAll(run, 0o700); err != nil {
		t.Fatal(err)
	}
	name := updateKind + "-" + sessionKey(update.HostCodex, "Marked one")
	if err := os.Rename(filepath.Join(d.State, "work", "wake", name), filepath.Join(run, name)); err != nil {
		t.Fatal(err)
	}
	if rep := work.Status(t.Context(), d, workNow); len(rep.Items) != 1 || rep.Items[0].Host != update.HostClaude || rep.Items[0].Reason != work.WaitRule {
		t.Fatalf("with the mark taken by a run: %+v", rep.Items)
	}
}
