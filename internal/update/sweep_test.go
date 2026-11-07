package update

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/heyNag/eva-md/internal/catchup"
	"github.com/heyNag/eva-md/internal/inbox"
)

// idleChat writes a chat with a backlog whose file last changed idle before
// the job's clock, and returns its file.
func idleChat(t *testing.T, e *env, session string, idle time.Duration) string {
	t.Helper()
	c := newChat(session, start)
	talk(c, 2, "amber")
	file := e.write(c)
	at := e.clock.Add(-idle)
	if err := os.Chtimes(file, at, at); err != nil {
		t.Fatal(err)
	}
	return file
}

// drained reports whether a session file's watermark is at its end.
func drained(t *testing.T, e *env, file string) bool {
	t.Helper()
	return e.mark(file).Offset == size(t, file)
}

// The day's sweep, which a person's first chat start of the day asks for
// with its catch-up, drains in the same job, after the sessions it names,
// at most 5 other sessions of the whole inbox whose file has been unchanged
// for an hour or more and holds a backlog, newest change first. A file
// with nothing pending takes no place among the 5, and neither does a chat
// a program ran, marked so by its catch-up record, a session the job
// names, or one still changing, each newer here than the sessions the
// sweep takes. The job records the day it took the sweep, so a second
// sweep that day takes nothing; the next day's takes what is left.
func TestSweepTakesIdleBacklogsOnceADay(t *testing.T) {
	e := newEnv(t)
	active := idleChat(t, e, "active", SweepIdle-time.Second)
	done := idleChat(t, e, "done", SweepIdle)
	if err := (inbox.Store{Root: e.dirs.Inbox()}).SetMark(done, inbox.Mark{Offset: size(t, done)}); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(done, e.clock.Add(-SweepIdle), e.clock.Add(-SweepIdle)); err != nil {
		t.Fatal(err)
	}
	named := idleChat(t, e, "named", 65*time.Minute)
	worker := programChat("worker")
	workerFile := e.write(worker)
	if err := os.Chtimes(workerFile, e.clock.Add(-66*time.Minute), e.clock.Add(-66*time.Minute)); err != nil {
		t.Fatal(err)
	}
	var idle []string
	for i, session := range []string{"idle-1", "idle-2", "idle-3", "idle-4", "idle-5", "idle-6"} {
		idle = append(idle, idleChat(t, e, session, 70*time.Minute+time.Duration(i)*10*time.Minute))
	}
	record(t, e, catchup.Source{V: 1, Host: worker.host, Session: worker.session, Headless: true})
	s, err := e.job().CatchUp(context.Background(), []Ref{{Host: HostClaude, ID: "named"}}, true)
	if err != nil || outcomes(s) != "ok, ok, ok, ok, ok, ok" || calls(e.fake) != 6 {
		t.Fatalf("outcomes %q, %d calls: %v\n%s", outcomes(s), calls(e.fake), err, e.log)
	}
	if s.Records[0].Session != "named" {
		t.Fatalf("the named session wasn't drained first: %+v", s.Records)
	}
	for i, file := range append([]string{named}, idle...) {
		if got, want := drained(t, e, file), i < 6; got != want {
			t.Errorf("%s: drained %v, want %v", filepath.Base(file), got, want)
		}
	}
	if drained(t, e, active) || drained(t, e, workerFile) {
		t.Fatalf("the sweep took a chat still changing or a program's chat\n%s", e.log)
	}
	if !strings.Contains(e.log.String(), "the day's sweep: 5 sessions idle an hour or more with a backlog") {
		t.Fatalf("log:\n%s", e.log)
	}
	if data, err := os.ReadFile(filepath.Join(e.dirs.State, "updates", sweepFile)); err != nil || string(data) != "2026-01-14\n" || SweepDue(e.dirs.State, e.clock.Format(time.DateOnly)) {
		t.Fatalf("the day of the sweep: %q %v", data, err)
	}
	// A second sweep the same day takes nothing.
	e.log.Reset()
	if s, err := e.job().CatchUp(context.Background(), nil, true); err != nil || s.Line() != "nothing pending" || drained(t, e, idle[5]) {
		t.Fatalf("second sweep: %q %v\n%s", s.Line(), err, e.log)
	}
	if !strings.Contains(e.log.String(), "an earlier job took it today") {
		t.Fatalf("log:\n%s", e.log)
	}
	// The next day's sweep takes what is left, the chat that was still
	// changing among them.
	e.clock = e.clock.Add(24 * time.Hour)
	if !SweepDue(e.dirs.State, e.clock.Format(time.DateOnly)) {
		t.Fatal("the next day's sweep is not due")
	}
	if s, err := e.job().CatchUp(context.Background(), nil, true); err != nil || outcomes(s) != "ok, ok" || !drained(t, e, idle[5]) || !drained(t, e, active) || drained(t, e, workerFile) {
		t.Fatalf("next day: %q %v\n%s", outcomes(s), err, e.log)
	}
}

// leftChats writes, before updates, a chat for each session in a project
// folder whose watermark folder has mode, as one that can't be written
// (0o500) or read (0o000), so the baseline leaves them, and returns their
// files.
func leftChats(t *testing.T, e *env, mode os.FileMode, sessions ...string) []string {
	t.Helper()
	return leftChatsIn(t, e, mode, map[string][]string{"github.com__you__left": sessions})["github.com__you__left"]
}

// leftChatsIn is leftChats for the sessions of each project folder
// projects maps, which one baseline leaves, and returns their files by
// project.
func leftChatsIn(t *testing.T, e *env, mode os.FileMode, projects map[string][]string) map[string][]string {
	t.Helper()
	if err := os.Remove(filepath.Join(e.dirs.Inbox(), baselineName)); err != nil {
		t.Fatal(err)
	}
	files, n := map[string][]string{}, 0
	for project, sessions := range projects {
		for _, session := range sessions {
			c := newChat(session, start)
			talk(c, 2, "walnut")
			for i := range c.rows {
				c.rows[i].Project = project
			}
			files[project] = append(files[project], e.write(c))
			n++
		}
		state := filepath.Join(filepath.Dir(files[project][0]), ".state")
		if err := os.MkdirAll(state, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(state, mode); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chmod(state, 0o700) })
	}
	var left *LeftError
	if err := Baseline(e.dirs, e.clock); !errors.As(err, &left) || left.Files != n {
		t.Fatalf("the baseline left %v", err)
	}
	return files
}

// leftNamed returns the chats from before updates the doctor names for
// Claude Code from the run log of today and yesterday, sorted.
func leftNamed(t *testing.T, e *env) []string {
	t.Helper()
	records, err := Recent(e.dirs.State, e.clock, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	return slices.Sorted(slices.Values(Host(records, HostClaude, e.clock).Left))
}

// A session the drain can't work on takes no place among the sweep's 5,
// as it would every day while older backlogs wait: 5 sessions of a project
// the config now excludes, which the drain leaves as they are, one whose
// project can't be told, its key a hash and its folder gone, and a chat
// from before updates the baseline left, all newer than an idle backlog
// the drain can work on. The sweep takes that one, the first day, and
// passes neither the excluded nor the untold sessions to the drain. It
// lists the chat left apart from its 5, ahead of them, and the drain
// records it as failed (left), with no model call, so the doctor names it.
func TestSweepPassesOverSessionsTheDrainCantWorkOn(t *testing.T) {
	e := newEnv(t)
	// The chat the baseline leaves: captured before updates, in a folder
	// whose watermarks can't be written.
	stuckFile := leftChats(t, e, 0o500, "left")[0]
	e.cfg.Exclude = []string{"remote:github.com/you/eva-md"}
	for i := range 5 {
		idleChat(t, e, "excluded-"+strconv.Itoa(i), 70*time.Minute+time.Duration(i)*time.Minute)
	}
	unknown := newChat("unknown", start)
	talk(unknown, 2, "pecan")
	for i := range unknown.rows {
		unknown.rows[i].Project = "repo-0a1b2c3d4e5f"
	}
	at := e.clock.Add(-80 * time.Minute)
	for _, file := range []string{stuckFile, e.write(unknown)} {
		if err := os.Chtimes(file, at, at); err != nil {
			t.Fatal(err)
		}
	}
	waiting := newChat("waiting", start)
	talk(waiting, 2, "amber")
	for i := range waiting.rows {
		waiting.rows[i].Project = "github.com__you__other"
	}
	waitingFile := e.write(waiting)
	at = e.clock.Add(-3 * time.Hour)
	if err := os.Chtimes(waitingFile, at, at); err != nil {
		t.Fatal(err)
	}
	s, err := e.job().CatchUp(context.Background(), nil, true)
	if err != nil || outcomes(s) != "failed (left), ok" || s.Errors != 1 || calls(e.fake) != 1 || !drained(t, e, waitingFile) {
		t.Fatalf("the sweep: %q, %d errors, %d calls, %v\n%s", outcomes(s), s.Errors, calls(e.fake), err, e.log)
	}
	if got := leftNamed(t, e); !slices.Equal(got, []string{"left"}) {
		t.Fatalf("the doctor names %v as left", got)
	}
	if log := e.log.String(); !strings.Contains(log, "the day's sweep: 1 session idle an hour or more with a backlog\n") ||
		!strings.Contains(log, "the day's sweep: 1 chat from before updates not set aside yet, idle an hour or more, recorded with no model call\n") || strings.Contains(log, "excludes the project") {
		t.Fatalf("log:\n%s", log)
	}
}

// The chats from before updates the baseline left take no place among the
// sweep's 5, however many there are: here 17 idle an hour or more, one more
// than the sweep lists, and one changed within the hour, in a folder whose
// watermarks can't be read, all newer than an idle backlog the drain can
// work on, which is drained the same day with the only model call. The
// sweep picks them by the baseline's record alone, before reading any
// watermark, lists the 16 newest idle an hour or more, ahead of its
// sessions, and the drain records each as failed (left), so the doctor
// names them; it passes over the oldest, and the one changed within the
// hour, which its own trigger takes.
func TestSweepListsLeftChatsApartFromItsSessions(t *testing.T) {
	e := newEnv(t)
	var sessions []string
	for i := range SweepLeft + 2 {
		sessions = append(sessions, fmt.Sprintf("left-%02d", i))
	}
	files := leftChats(t, e, 0o000, sessions...)
	// left-00 changed within the hour; left-01 to left-16 are the 16 newest
	// of the idle ones, and left-17 the oldest.
	for i, file := range files {
		idle := SweepIdle + time.Duration(i)*time.Minute
		if i == 0 {
			idle = 30 * time.Minute
		}
		at := e.clock.Add(-idle)
		if err := os.Chtimes(file, at, at); err != nil {
			t.Fatal(err)
		}
	}
	waiting := idleChat(t, e, "waiting", 3*time.Hour)
	s, err := e.job().CatchUp(context.Background(), nil, true)
	if want := strings.Repeat("failed (left), ", SweepLeft) + "ok"; err != nil || outcomes(s) != want || calls(e.fake) != 1 || !drained(t, e, waiting) {
		t.Fatalf("the sweep: %q, %d calls, %v\n%s", outcomes(s), calls(e.fake), err, e.log)
	}
	if got, want := leftNamed(t, e), sessions[1:SweepLeft+1]; !slices.Equal(got, want) {
		t.Fatalf("the doctor names %v as left, want %v", got, want)
	}
	if log := e.log.String(); !strings.Contains(log, "the day's sweep: 1 session idle an hour or more with a backlog\n") ||
		!strings.Contains(log, fmt.Sprintf("the day's sweep: %d chats from before updates not set aside yet, idle an hour or more, recorded with no model call\n", SweepLeft)) {
		t.Fatalf("log:\n%s", log)
	}
}

// The sweep lists a chat from before updates the baseline left also when
// its 5 sessions are all newer: finding them doesn't end the scan before
// the chat left.
func TestSweepListsALeftChatOlderThanItsSessions(t *testing.T) {
	e := newEnv(t)
	file := leftChats(t, e, 0o500, "left")[0]
	at := e.clock.Add(-3 * time.Hour)
	if err := os.Chtimes(file, at, at); err != nil {
		t.Fatal(err)
	}
	var idle []string
	for i := range SweepSessions {
		idle = append(idle, idleChat(t, e, "idle-"+strconv.Itoa(i), 70*time.Minute+time.Duration(i)*time.Minute))
	}
	s, err := e.job().CatchUp(context.Background(), nil, true)
	if want := "failed (left)" + strings.Repeat(", ok", SweepSessions); err != nil || outcomes(s) != want || calls(e.fake) != SweepSessions {
		t.Fatalf("the sweep: %q, %d calls, %v\n%s", outcomes(s), calls(e.fake), err, e.log)
	}
	for _, file := range idle {
		if !drained(t, e, file) {
			t.Fatalf("%s wasn't swept\n%s", filepath.Base(file), e.log)
		}
	}
	if got := leftNamed(t, e); !slices.Equal(got, []string{"left"}) {
		t.Fatalf("the doctor names %v as left", got)
	}
}

// A chat from before updates the baseline left, in a project where no chat
// starts and with no other backlog in the inbox, as right after install, is
// recorded by the day's sweep each day, with no model call, so the doctor
// keeps naming it, as install says it will.
func TestSweepRecordsALeftChatEachDay(t *testing.T) {
	e := newEnv(t)
	file := leftChats(t, e, 0o500, "left")[0]
	at := e.clock.Add(-80 * time.Minute)
	if err := os.Chtimes(file, at, at); err != nil {
		t.Fatal(err)
	}
	for day := range 3 {
		e.log.Reset()
		s, err := e.job().CatchUp(context.Background(), nil, true)
		if err != nil || outcomes(s) != "failed (left)" || calls(e.fake) != 0 {
			t.Fatalf("day %d: %q, %d calls, %v\n%s", day, outcomes(s), calls(e.fake), err, e.log)
		}
		if got := leftNamed(t, e); !slices.Equal(got, []string{"left"}) {
			t.Fatalf("day %d: the doctor names %v as left\n%s", day, got, e.log)
		}
		e.clock = e.clock.Add(24 * time.Hour)
	}
}

// known-limitations.md says the day's sweep records at most SweepLeft
// chats from before updates, the newest idle an hour or more of any
// project, so past that many the doctor may never name one in a project
// where no chat starts. That holds: SweepLeft chats left in one project,
// all newer than the only chat left in another, take every place each
// day, and that one goes unnamed, though it is alone in its project.
func TestSweepListsTheNewestLeftChatsOfAnyProject(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "docs", "known-limitations.md"))
	if err != nil {
		t.Fatal(err)
	}
	if says := fmt.Sprintf("The day's sweep records at most the %d newest such chats idle an hour or more, of any project,", SweepLeft); !strings.Contains(string(data), says) {
		t.Fatalf("known-limitations.md doesn't say %q", says)
	}
	e := newEnv(t)
	var busy []string
	for i := range SweepLeft {
		busy = append(busy, fmt.Sprintf("busy-%02d", i))
	}
	files := leftChatsIn(t, e, 0o500, map[string][]string{"github.com__you__busy": busy, "github.com__you__quiet": {"quiet"}})
	for i, file := range files["github.com__you__busy"] {
		at := e.clock.Add(-SweepIdle - time.Duration(i)*time.Minute)
		if err := os.Chtimes(file, at, at); err != nil {
			t.Fatal(err)
		}
	}
	at := e.clock.Add(-3 * time.Hour)
	if err := os.Chtimes(files["github.com__you__quiet"][0], at, at); err != nil {
		t.Fatal(err)
	}
	want := strings.TrimSuffix(strings.Repeat("failed (left), ", SweepLeft), ", ")
	for day := range 2 {
		s, err := e.job().CatchUp(context.Background(), nil, true)
		if err != nil || outcomes(s) != want || calls(e.fake) != 0 {
			t.Fatalf("day %d: %q, %d calls, %v\n%s", day, outcomes(s), calls(e.fake), err, e.log)
		}
		if got := leftNamed(t, e); !slices.Equal(got, busy) {
			t.Fatalf("day %d: the doctor names %v as left, want %v", day, got, busy)
		}
		e.clock = e.clock.Add(24 * time.Hour)
	}
}

// A chat start's hook takes its time zone from the host's TZ, which the
// job's environment drops, so the two can be on different days. The hook
// finds the sweep due on its own day and gives the job that day, which the
// job claims the sweep on, whatever its own clock says: here the record
// holds the job's own day, the hook's is the next, and the sweep runs once,
// recorded on the hook's day. A second job given that day takes nothing.
func TestSweepIsClaimedOnTheChatStartsDay(t *testing.T) {
	e := newEnv(t)
	file := idleChat(t, e, "idle", 2*time.Hour)
	if err := os.MkdirAll(filepath.Join(e.dirs.State, "updates"), 0o700); err != nil {
		t.Fatal(err)
	}
	record := filepath.Join(e.dirs.State, "updates", sweepFile)
	if err := os.WriteFile(record, []byte(e.clock.Format(time.DateOnly)+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	day := e.clock.AddDate(0, 0, 1).Format(time.DateOnly)
	if !SweepDue(e.dirs.State, day) {
		t.Fatal("the sweep is not due on the hook's day")
	}
	j := e.job()
	j.SweepDay = day
	if s, err := j.CatchUp(context.Background(), nil, true); err != nil || outcomes(s) != "ok" || !drained(t, e, file) {
		t.Fatalf("the hook's day: %q %v\n%s", outcomes(s), err, e.log)
	}
	if data, err := os.ReadFile(record); err != nil || string(data) != day+"\n" || SweepDue(e.dirs.State, day) {
		t.Fatalf("the day of the sweep: %q %v", data, err)
	}
	e.log.Reset()
	idleChat(t, e, "idle-later", 3*time.Hour)
	if s, err := j.CatchUp(context.Background(), nil, true); err != nil || s.Line() != "nothing pending" || !strings.Contains(e.log.String(), "an earlier job took it today") {
		t.Fatalf("a second job that day: %q %v\n%s", s.Line(), err, e.log)
	}
}

// A job that finds the update lock busy exits before it takes the sweep,
// so the sweep stays due and a later chat start's job takes it. A sweep
// with no session named and none to take is no error.
func TestBusySweepStaysDue(t *testing.T) {
	e := newEnv(t)
	file := idleChat(t, e, "idle", 2*time.Hour)
	unlock, busy, err := TryLock(e.dirs.Locks(), "update.lock")
	if err != nil || busy {
		t.Fatalf("lock: %v %v", busy, err)
	}
	s, err := e.job().CatchUp(context.Background(), nil, true)
	unlock()
	if err != nil || !s.Stopped(StopBusy) || drained(t, e, file) || !SweepDue(e.dirs.State, e.clock.Format(time.DateOnly)) {
		t.Fatalf("busy: %+v %v", s, err)
	}
	if s, err := e.job().CatchUp(context.Background(), nil, true); err != nil || outcomes(s) != "ok" || !drained(t, e, file) || SweepDue(e.dirs.State, e.clock.Format(time.DateOnly)) {
		t.Fatalf("after: %q %v\n%s", outcomes(s), err, e.log)
	}
	if s, err := e.job().CatchUp(context.Background(), []Ref{{Host: HostCodex, ID: "missing"}}, true); err != nil || s.Line() != "nothing pending" {
		t.Fatalf("nothing to do: %q %v", s.Line(), err)
	}
}

// Chat starts of hosts whose time zones are a day apart, taking turns, take
// the sweep once between them: one taken a day ahead counts for the day
// behind it too, and the next day's sweep is due again. A record taken on an
// earlier day, or more than a day ahead, as one a clock set wrong left,
// doesn't count, so it can't keep the sweep from running.
func TestSweepAcrossTimeZonesRunsOnceADay(t *testing.T) {
	e := newEnv(t)
	day := func(n int) string { return e.clock.AddDate(0, 0, n).Format(time.DateOnly) }
	record := filepath.Join(e.dirs.State, "updates", sweepFile)
	swept := []string{}
	for i, d := range []string{day(1), day(0), day(1), day(0), day(2), day(1), day(2)} {
		idleChat(t, e, "idle-"+strconv.Itoa(i), 2*time.Hour)
		due := SweepDue(e.dirs.State, d)
		before, _ := os.ReadFile(record)
		// The job, given the day, judges as the hook does, so a job a
		// hook started before the sweep was taken takes it only once.
		e.log.Reset()
		j := e.job()
		j.SweepDay = d
		if _, err := j.CatchUp(context.Background(), nil, true); err != nil {
			t.Fatal(err)
		}
		after, _ := os.ReadFile(record)
		switch {
		case due && string(after) == d+"\n":
			swept = append(swept, d)
		case due, string(after) != string(before), !strings.Contains(e.log.String(), "an earlier job took it today"):
			t.Fatalf("a chat start on %s, due %v: the record %q, then %q\n%s", d, due, before, after, e.log)
		}
	}
	if want := []string{day(1), day(2)}; strings.Join(swept, " ") != strings.Join(want, " ") {
		t.Fatalf("sweeps taken on %v, want %v", swept, want)
	}
	for _, tc := range []struct {
		recorded string
		due      bool
	}{{day(0), false}, {day(1), false}, {day(-1), true}, {day(2), true}, {day(30), true}, {"", true}, {"not a day", true}} {
		if err := os.WriteFile(record, []byte(tc.recorded+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if got := SweepDue(e.dirs.State, day(0)); got != tc.due {
			t.Errorf("a sweep recorded on %q: due on %s %v, want %v", tc.recorded, day(0), got, tc.due)
		}
	}
	// The job's claim judges the same way: a record more than a day ahead
	// gives way to today's sweep.
	if err := os.WriteFile(record, []byte(day(5)+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	idleChat(t, e, "idle-ahead", 2*time.Hour)
	j := e.job()
	j.SweepDay = day(0)
	if s, err := j.CatchUp(context.Background(), nil, true); err != nil || !strings.HasPrefix(outcomes(s), "ok") {
		t.Fatalf("a record far ahead: %q %v\n%s", outcomes(s), err, e.log)
	}
	if data, err := os.ReadFile(record); err != nil || string(data) != day(0)+"\n" {
		t.Fatalf("the record after the sweep: %q %v", data, err)
	}
}
