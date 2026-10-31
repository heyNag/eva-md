package update

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/heyNag/eva-md/internal/inbox"
)

// The 41st model call of a day stops with budget: 40 sessions are updated,
// the 41st waits, and runs the next day.
func TestTheFortyFirstCallStopsWithBudget(t *testing.T) {
	e := newEnv(t)
	var chats []*chat
	for i := 0; i < 41; i++ {
		c := newChat(fmt.Sprintf("budget-%02d", i), start.Add(time.Duration(i)*time.Hour))
		talk(c, 1, "amber")
		e.write(c)
		chats = append(chats, c)
		// Session files are taken oldest first, by modification time.
		e.clock = e.clock.Add(time.Second)
	}
	s, err := e.job().Pending(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Records) != 40 || calls(e.fake) != 40 || !s.Stopped(StopBudget) {
		t.Fatalf("%d records, %d calls, stops %v\n%s", len(s.Records), calls(e.fake), s.Stops, e.log)
	}
	last := e.write(chats[40])
	if e.mark(last).Offset != 0 {
		t.Fatal("the 41st session was updated past the cap")
	}
	if s := e.run(chats[40]); !s.Stopped(StopBudget) || calls(e.fake) != 40 {
		t.Fatalf("the cap let another call through: %+v", s)
	}
	e.clock = e.clock.Add(24 * time.Hour)
	if s := e.run(chats[40]); outcomes(s) != "ok" || calls(e.fake) != 41 {
		t.Fatalf("next day: %q", outcomes(s))
	}
}

// The cap counts each host's model calls on its own, and holds a person's
// run too.
func TestBudgetIsPerHostAndHoldsManualRuns(t *testing.T) {
	e := newEnv(t)
	e.cfg.DailyCap = 1
	a, b, m := newChat("perhost-a", start), newChat("perhost-b", start.Add(time.Hour)), newChat("perhost-m", start.Add(2*time.Hour))
	b.host = HostCodex
	talk(a, 1, "amber")
	talk(b, 1, "cobalt")
	talk(m, 1, "ochre")
	for _, c := range []*chat{a, b, m} {
		e.write(c)
	}
	if s := e.run(a); outcomes(s) != "ok" {
		t.Fatalf("claude: %q\n%s", outcomes(s), e.log)
	}
	if s := e.run(b); outcomes(s) != "ok" {
		t.Fatalf("codex after claude's one call: %q %v\n%s", outcomes(s), s.Stops, e.log)
	}
	manual := e.job()
	manual.Manual = true
	s, err := manual.Session(context.Background(), m.host, m.session)
	if err != nil || !s.Stopped(StopBudget) || len(s.Records) != 0 || calls(e.fake) != 2 {
		t.Fatalf("a manual run past claude's cap: %+v %v, %d calls", s, err, calls(e.fake))
	}
}

// Once the budget stops a host, the job opens none of its remaining files,
// so their passes, skips included, wait for the next run.
func TestStoppedHostWaitsWhole(t *testing.T) {
	e := newEnv(t)
	e.cfg.DailyCap = 1
	a, b, c := newChat("stop-a", start), newChat("stop-b", start.Add(time.Hour)), newChat("stop-c", start.Add(2*time.Hour))
	c.branch = "locked-branch"
	var file string
	for _, x := range []*chat{a, b, c} {
		talk(x, 1, "amber")
		file = e.write(x)
	}
	if err := e.wiki().Create("projects/eva-md/threads/locked-branch", lockedPage(), "human:alex"); err != nil {
		t.Fatal(err)
	}
	// a's call reaches the cap, b's stops on it, and c, whose page is
	// locked, would need no call but waits too.
	s, err := e.job().Pending(context.Background())
	if err != nil || outcomes(s) != "ok" || !s.Stopped(StopBudget) || e.mark(file).Offset != 0 {
		t.Fatalf("pending: %q %v %v\n%s", outcomes(s), s.Stops, err, e.log)
	}
	e.clock = e.clock.Add(24 * time.Hour)
	if s := e.run(c); outcomes(s) != "skipped (locked)" {
		t.Fatalf("next day: %q", outcomes(s))
	}
}

// Only model calls count toward the cap: a skip makes none, and neither
// does a run that found no CLI.
func TestSkipsAndAMissingCLICountNoCall(t *testing.T) {
	e := newEnv(t)
	e.cfg.DailyCap = 2
	locked := newChat("free-locked", start)
	locked.branch = "locked-branch"
	talk(locked, 1, "amber")
	e.write(locked)
	if err := e.wiki().Create("projects/eva-md/threads/locked-branch", lockedPage(), "human:alex"); err != nil {
		t.Fatal(err)
	}
	missing := newChat("free-missing", start.Add(time.Hour))
	talk(missing, 1, "amber")
	e.write(missing)
	var free []Record
	if s := e.run(locked); outcomes(s) == "skipped (locked)" {
		free = append(free, s.Records...)
	}
	e.fake.Respond = func(Request) (Result, error) { return Result{Reason: ReasonCLIMissing}, nil }
	if s := e.run(missing); outcomes(s) == "failed (cli_missing)" {
		free = append(free, s.Records...)
	}
	if len(free) != 2 || free[0].Calls != 0 || free[1].Calls != 0 {
		t.Fatalf("records %+v\n%s", free, e.log)
	}
	// cli_missing backs the host off, so a person runs the rest.
	e.fake.Respond = func(r Request) (Result, error) { return Result{Output: reply(t, r, "cobalt"), Attempts: 1}, nil }
	manual := e.job()
	manual.Manual = true
	var got []string
	for i, word := range []string{"one", "two", "three"} {
		c := newChat("free-"+word, start.Add(time.Duration(2+i)*time.Hour))
		talk(c, 1, word)
		e.write(c)
		s, err := manual.Session(context.Background(), c.host, c.session)
		if err != nil {
			t.Fatal(err)
		}
		got = append(got, fmt.Sprintf("%s%v", outcomes(s), s.Stops))
	}
	if strings.Join(got, "|") != "ok[]|ok[]|[budget]" || calls(e.fake) != 3 {
		t.Fatalf("runs %q, %d calls\n%s", got, calls(e.fake), e.log)
	}
}

// rate_limit, host_error and cli_missing back off 5 minutes, then 10,
// without counting toward the session: three of them don't quarantine it. A
// missing CLI made no call, yet backs off rather than holding the host for
// the day: a job started with a PATH that lacks the CLI, as from an app,
// says nothing about the jobs a terminal starts.
func TestRateLimitBacksOffWithoutCountingTowardTheSession(t *testing.T) {
	for _, reason := range []string{ReasonRateLimit, ReasonHostError, ReasonCLIMissing} {
		t.Run(reason, func(t *testing.T) {
			e := newEnv(t)
			c := newChat("backoff-"+reason, start)
			talk(c, 2, "amber")
			file := e.write(c)
			attempts := 1
			if reason == ReasonCLIMissing {
				attempts = 0
			}
			e.fake.Respond = func(Request) (Result, error) { return Result{Reason: reason, Attempts: attempts}, nil }
			if s := e.run(c); outcomes(s) != "failed ("+reason+")" {
				t.Fatalf("first run %q", outcomes(s))
			}
			for _, step := range []struct {
				after time.Duration
				call  bool
			}{{time.Minute, false}, {4 * time.Minute, true}, {9 * time.Minute, false}, {time.Minute, true}, {19 * time.Minute, false}, {time.Minute, true}} {
				e.clock = e.clock.Add(step.after)
				before := calls(e.fake)
				s := e.run(c)
				if called := calls(e.fake) > before; called != step.call || !step.call && !s.Stopped(StopBackoff) {
					t.Fatalf("after %s: called %v, stops %v\n%s", step.after, called, s.Stops, e.log)
				}
			}
			e.fake.Respond = func(r Request) (Result, error) { return Result{Output: reply(t, r, "amber"), Attempts: 1}, nil }
			e.clock = e.clock.Add(time.Hour)
			if s := e.run(c); outcomes(s) != "ok" || e.mark(file).Offset != size(t, file) {
				t.Fatalf("after 4 failures of the host the session was not updated: %q", outcomes(s))
			}
		})
	}
}

// Three invalid outputs in a row quarantine the session for the day; the
// next day it gets one retry, and when that fails too its watermark moves
// past the batch with skipped (quarantined).
func TestThreeInvalidOutputsQuarantineTheSession(t *testing.T) {
	e := newEnv(t)
	c := newChat("quarantine-a", start)
	talk(c, 2, "amber")
	file := e.write(c)
	e.fake.Respond = func(Request) (Result, error) { return Result{Output: []byte(`{"summary":"x"}`), Attempts: 1}, nil }
	for i := 0; i < 3; i++ {
		if s := e.run(c); outcomes(s) != "failed (invalid_output)" {
			t.Fatalf("run %d: %q", i+1, outcomes(s))
		}
		e.clock = e.clock.Add(time.Minute)
	}
	if s := e.run(c); !s.Stopped(StopQuarantined) || calls(e.fake) != 3 {
		t.Fatalf("a quarantined session ran: %+v, %d calls", s, calls(e.fake))
	}
	e.clock = e.clock.Add(24 * time.Hour)
	s := e.run(c)
	if outcomes(s) != "failed (invalid_output), skipped (quarantined)" || calls(e.fake) != 4 {
		t.Fatalf("next-day retry: %q, %d calls", outcomes(s), calls(e.fake))
	}
	if m := e.mark(file); m.Offset != size(t, file) {
		t.Fatalf("the watermark stayed behind a quarantined batch: %+v", m)
	}
}

// A successful manual run clears a quarantine, and the triggers' runs go on
// afterwards.
func TestManualRunClearsAQuarantine(t *testing.T) {
	e := newEnv(t)
	c := newChat("quarantine-b", start)
	talk(c, 2, "amber")
	e.write(c)
	e.fake.Respond = func(Request) (Result, error) { return Result{Reason: ReasonToolUsed, Attempts: 1}, nil }
	for i := 0; i < 3; i++ {
		e.run(c)
	}
	if s := e.run(c); !s.Stopped(StopQuarantined) {
		t.Fatalf("not quarantined: %+v", s)
	}
	e.fake.Respond = func(r Request) (Result, error) { return Result{Output: reply(t, r, "amber"), Attempts: 1}, nil }
	manual := e.job()
	manual.Manual = true
	s, err := manual.Session(context.Background(), c.host, c.session)
	if err != nil || outcomes(s) != "ok" || !s.Records[0].Manual {
		t.Fatalf("manual run: %q %v", outcomes(s), err)
	}
	talk(c, 1, "cobalt")
	e.write(c)
	if s := e.run(c); outcomes(s) != "ok" {
		t.Fatalf("a trigger's run after the manual one: %q %v", outcomes(s), s.Stops)
	}
}

// auth and mcp_failed hold every update of the host for the day without
// counting toward the session; a successful manual run lifts the hold, and
// so does the next day.
func TestAuthHoldsTheHostUntilAManualRunOrTomorrow(t *testing.T) {
	for _, reason := range []string{ReasonAuth, ReasonMCPFailed} {
		t.Run(reason, func(t *testing.T) {
			e := newEnv(t)
			a, b := newChat("hold-a-"+reason, start), newChat("hold-b-"+reason, start.Add(time.Hour))
			talk(a, 2, "amber")
			talk(b, 2, "cobalt")
			e.write(a)
			e.write(b)
			e.fake.Respond = func(Request) (Result, error) { return Result{Reason: reason, Attempts: 1}, nil }
			s, err := e.job().Pending(context.Background())
			if err != nil || outcomes(s) != "failed ("+reason+")" || !s.Stopped(StopHeld) || calls(e.fake) != 1 {
				t.Fatalf("pending: %q, stops %v, %d calls", outcomes(s), s.Stops, calls(e.fake))
			}
			if s := e.run(b); !s.Stopped(StopHeld) || calls(e.fake) != 1 {
				t.Fatalf("a held host ran: %+v", s)
			}
			e.fake.Respond = func(r Request) (Result, error) { return Result{Output: reply(t, r, "amber"), Attempts: 1}, nil }
			manual := e.job()
			manual.Manual = true
			if s, err := manual.Session(context.Background(), b.host, b.session); err != nil || outcomes(s) != "ok" {
				t.Fatalf("manual run: %q %v", outcomes(s), err)
			}
			// a is older than the page b's run wrote, so its pass keeps the
			// Now and its one line is already there: it runs, and changes
			// nothing.
			if s := e.run(a); outcomes(s) != "no_change" || calls(e.fake) != 3 {
				t.Fatalf("the hold outlived a successful manual run: %q %v", outcomes(s), s.Stops)
			}
		})
	}
	e := newEnv(t)
	c := newChat("hold-c", start)
	talk(c, 2, "amber")
	e.write(c)
	e.fake.Respond = func(Request) (Result, error) { return Result{Reason: ReasonAuth, Attempts: 1}, nil }
	e.run(c)
	e.fake.Respond = func(r Request) (Result, error) { return Result{Output: reply(t, r, "amber"), Attempts: 1}, nil }
	e.clock = e.clock.Add(24 * time.Hour)
	if s := e.run(c); outcomes(s) != "ok" {
		t.Fatalf("the hold outlived the day: %q %v", outcomes(s), s.Stops)
	}
}

// A stop known before a session file is opened costs no read of it: the
// file of a host the budget, a hold or a backoff stops, or of a session
// quarantined today, is never opened, so a chat past 60 KB whose every Stop
// spawns a job reads nothing while the stop lasts. The file here has a
// watermark inside a line, which opening it reports as an error and records
// as failed (watermark), as it does with no stop and for a person's run,
// which a hold doesn't stop.
func TestStopsComeBeforeTheFileIsOpened(t *testing.T) {
	ok := Record{Outcome: OutcomeOK, Calls: 1}
	invalid := Record{Outcome: OutcomeFailed, Reason: ReasonInvalidOutput, Calls: 1}
	for _, tc := range []struct {
		name    string
		records []Record
		manual  bool
		stop    string
	}{
		{"budget", []Record{ok}, false, StopBudget},
		{"held", []Record{{Outcome: OutcomeFailed, Reason: ReasonAuth, Calls: 1}}, false, StopHeld},
		{"backoff", []Record{{Outcome: OutcomeFailed, Reason: ReasonRateLimit, Calls: 1}}, false, StopBackoff},
		{"quarantined", []Record{invalid, invalid, invalid}, false, StopQuarantined},
		{"no stop", nil, false, ""},
		{"manual run, held", []Record{{Outcome: OutcomeFailed, Reason: ReasonAuth, Calls: 1}}, true, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := newEnv(t)
			if tc.stop == StopBudget {
				e.cfg.DailyCap = 1
			}
			c := newChat("pregate-1", start)
			talk(c, 2, "amber")
			file := e.write(c)
			if err := (inbox.Store{Root: e.dirs.Inbox()}).SetMark(file, inbox.Mark{Offset: 10}); err != nil {
				t.Fatal(err)
			}
			for _, r := range tc.records {
				r.At, r.Host, r.Session = e.clock.Add(-time.Minute).Format(time.RFC3339), c.host, c.session
				if err := appendRecord(e.dirs.State, r); err != nil {
					t.Fatal(err)
				}
			}
			j := e.job()
			j.Manual = tc.manual
			s, err := j.Session(context.Background(), c.host, c.session)
			want := Summary{Errors: 1}
			if tc.stop != "" {
				want = Summary{Stops: []string{tc.stop}}
			} else if outcomes(s) == "failed (watermark)" && s.Records[0].Calls == 0 {
				s.Records = nil
			}
			if err != nil || !reflect.DeepEqual(s, want) || calls(e.fake) != 0 {
				t.Fatalf("%+v %v, %d calls\n%s", s, err, calls(e.fake), e.log)
			}
			if m := e.mark(file); m != (inbox.Mark{Offset: 10}) {
				t.Fatalf("watermark %+v", m)
			}
		})
	}
}
