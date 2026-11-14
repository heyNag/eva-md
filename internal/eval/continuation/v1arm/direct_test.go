package v1arm

import (
	"context"
	"debug/buildinfo"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime/debug"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/heyNag/eva-md/internal/eval/continuation/drive"
	"github.com/heyNag/eva-md/internal/eval/continuation/drive/fakehost"
	"github.com/heyNag/eva-md/internal/eval/continuation/task"
	"github.com/heyNag/eva-md/internal/host/adapter"
	"github.com/heyNag/eva-md/internal/inbox"
	"github.com/heyNag/eva-md/internal/paths"
	"github.com/heyNag/eva-md/internal/update"
)

func jsonUnmarshal(s string, v any) error { return json.Unmarshal([]byte(s), v) }

func jsonString(s string) string {
	data, _ := json.Marshal(s)
	return string(data)
}

// aTrial is a trial built by hand, as the driver builds one, for the arm's
// own functions: its folders, the fixture task, the test binary as eva-md
// in its bin/, and an environment that names the fake's script.
type aTrial struct {
	drive.Trial
	base   string
	record string
}

func newTrial(t *testing.T, h adapter.Host, s fakeScript) aTrial {
	t.Helper()
	base := tempDir(t)
	tasks, _, err := fakehost.Fixture(filepath.Join(base, "data"), 1)
	if err != nil {
		t.Fatal(err)
	}
	x, err := task.Load(filepath.Join(tasks, fakehost.FixtureID))
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(base, "work", "run", x.ID, string(task.Eva), "t1")
	d := drive.Dirs{Root: root, Home: filepath.Join(root, "home"), Tmp: filepath.Join(root, "tmp"), Bin: filepath.Join(root, "bin"),
		Workspace: filepath.Join(root, "workspace"), Grade: filepath.Join(root, "grade")}
	for _, dir := range []string{d.Home, d.Tmp, d.Bin, d.Workspace, d.Grade} {
		mkdir(t, dir)
	}
	write(t, filepath.Join(d.Home, ".gitconfig"), "[user]\n\tname = Made Up\n\temail = made-up@example.invalid\n")
	if err := os.Symlink(testBinary(t), filepath.Join(d.Bin, "eva-md")); err != nil {
		t.Fatal(err)
	}
	record := filepath.Join(base, "record")
	mkdir(t, record)
	s.Dir = record
	script := filepath.Join(base, "eva.json")
	writeJSON(t, script, s)
	env := []string{"HOME=" + d.Home, "PATH=" + d.Bin + string(filepath.ListSeparator) + os.Getenv("PATH"), "TMPDIR=" + d.Tmp,
		"GIT_CONFIG_NOSYSTEM=1", fakeVar + "=" + script}
	return aTrial{Trial: drive.Trial{Task: x, Condition: task.Eva, Number: 1, Dirs: d, Host: h, Env: env}, base: base, record: record}
}

// testArm is an arm of the test's own, which looks at a held lock often.
func testArm() *arm { return &arm{poll: 10 * time.Millisecond} }

// stalled gives a an arm's limit that a test ends at an exact point: when
// the program that stalls writes a line to the named pipe it returns, the
// context of the last limit given ends. It also gives the function that
// ends that context at once, for a wait that a held lock stalls.
func stalled(t *testing.T, a *arm, dir string) (pipe string, end func()) {
	t.Helper()
	pipe = fifo(t, dir, "stalled")
	var mu sync.Mutex
	var last context.CancelFunc
	a.limit = func(ctx context.Context, d time.Duration) (context.Context, context.CancelFunc) {
		c, cancel := context.WithCancel(ctx)
		mu.Lock()
		last = cancel
		mu.Unlock()
		return c, cancel
	}
	end = func() {
		mu.Lock()
		defer mu.Unlock()
		if last != nil {
			last()
		}
	}
	go func() {
		f, err := os.Open(pipe)
		if err != nil {
			return
		}
		defer f.Close()
		var b [64]byte
		if n, _ := f.Read(b[:]); n > 0 && strings.HasPrefix(string(b[:n]), "stalled") {
			end()
		}
	}()
	return pipe, end
}

// A command stalled past its limit is killed, with its group, and leaves
// the trial unjudged: the sign-in program, setup and a hook setup_failed,
// the update update_failed; and so does a job that holds its lock past the
// wait's limit, before the chat and after it.
func TestAStalledCommandIsUnjudged(t *testing.T) {
	h := adapter.All()[0]
	ok := []fakeUpdate{{Line: "1 pass: 1 ok", Outcome: update.OutcomeOK}}
	for name, c := range map[string]struct {
		script func(pipe string) fakeScript
		signIn bool
		code   string
	}{
		"sign-in": {func(string) fakeScript { return fakeScript{Updates: ok} }, true, drive.CodeSetupFailed},
		"setup":   {func(p string) fakeScript { return fakeScript{SetupStall: p, Updates: ok} }, false, drive.CodeSetupFailed},
		"a hook":  {func(p string) fakeScript { return fakeScript{HookStall: p, Updates: ok} }, false, drive.CodeSetupFailed},
		"update":  {func(p string) fakeScript { return fakeScript{Updates: []fakeUpdate{{Stall: p}}} }, false, drive.CodeUpdateFailed},
	} {
		t.Run(name, func(t *testing.T) {
			a := testArm()
			dir := tempDir(t)
			pipe, _ := stalled(t, a, dir)
			tr := newTrial(t, h, c.script(pipe))
			if c.signIn {
				link := filepath.Join(dir, "sign-in")
				if err := os.Symlink(testBinary(t), link); err != nil {
					t.Fatal(err)
				}
				writeJSON(t, filepath.Join(dir, "sign-in.json"), signInScript{Dir: tr.record, Stall: pipe})
				tr.Env = append(tr.Env, signInVar+"="+filepath.Join(dir, "sign-in.json"))
				tr.Options.SignIn = link
			}
			_, err := a.prepare(t.Context(), tr.Trial)
			var u *drive.Unjudged
			if !errors.As(err, &u) || u.Code != c.code {
				t.Fatalf("%v", err)
			}
		})
	}
	for _, where := range []string{"before the chat", "after it"} {
		t.Run("a lock held "+where, func(t *testing.T) {
			a := testArm()
			_, end := stalled(t, a, tempDir(t))
			tr := newTrial(t, h, fakeScript{Updates: ok})
			if where == "after it" {
				if _, err := a.prepare(t.Context(), tr.Trial); err != nil {
					t.Fatal(err)
				}
				// What the run log holds when the wait runs out is still
				// what the trial spent.
				if err := writeRecord(stateDir(tr.Trial), update.Record{Host: string(h.ID), Session: "made-up-new-chat-11", Outcome: update.OutcomeOK, TokensIn: 70, TokensOut: 5, DurationMS: 11}); err != nil {
					t.Fatal(err)
				}
			}
			unlock, busy, err := paths.TryLock(filepath.Join(stateDir(tr.Trial), "locks"), maintainLock)
			if err != nil || busy {
				t.Fatal(err)
			}
			defer unlock()
			a.held = func(string) { end() }
			var cost drive.Cost
			if where == "after it" {
				cost, err = a.after(t.Context(), tr.Trial)
			} else {
				_, err = a.prepare(t.Context(), tr.Trial)
			}
			var u *drive.Unjudged
			if !errors.As(err, &u) || u.Code != drive.CodeUpdateFailed {
				t.Fatalf("%v", err)
			}
			if where == "after it" && cost != (drive.Cost{Tokens: 75, Ms: 11}) {
				t.Fatalf("the run log's spend is not counted when the wait runs out: %+v", cost)
			}
		})
	}
}

// A command stalled past its limit is killed with its whole process group:
// a process that setup or the sign-in program started and left in its
// group, holding none of its standard files, is gone once prepare returns.
func TestAStalledCommandsGroupIsKilled(t *testing.T) {
	h := adapter.All()[0]
	ok := []fakeUpdate{{Line: "1 pass: 1 ok", Outcome: update.OutcomeOK}}
	for _, name := range []string{"setup", "sign-in"} {
		t.Run(name, func(t *testing.T) {
			a := testArm()
			dir := tempDir(t)
			pipe, _ := stalled(t, a, dir)
			g := &grandchild{Pipe: fifo(t, dir, "held"), PID: filepath.Join(dir, "grandchild.pid")}
			// The pipe's end is read once no process holds it open: the
			// grandchild is gone.
			gone := make(chan struct{})
			go func() {
				defer close(gone)
				if f, err := os.Open(g.Pipe); err == nil {
					_, _ = io.Copy(io.Discard, f)
					f.Close()
				}
			}()
			s := fakeScript{Updates: ok}
			if name == "setup" {
				s.SetupStall, s.Grandchild = pipe, g
			}
			tr := newTrial(t, h, s)
			if name == "sign-in" {
				link := filepath.Join(dir, "sign-in")
				if err := os.Symlink(testBinary(t), link); err != nil {
					t.Fatal(err)
				}
				writeJSON(t, filepath.Join(dir, "sign-in.json"), signInScript{Dir: tr.record, Stall: pipe, Grandchild: g})
				tr.Env = append(tr.Env, signInVar+"="+filepath.Join(dir, "sign-in.json"))
				tr.Options.SignIn = link
			}
			_, err := a.prepare(t.Context(), tr.Trial)
			var u *drive.Unjudged
			if !errors.As(err, &u) || u.Code != drive.CodeSetupFailed {
				t.Fatalf("%v", err)
			}
			select {
			case <-gone:
			case <-time.After(2 * time.Minute):
				// A hang guard: the arm left the grandchild alive.
				if data, err := os.ReadFile(g.PID); err == nil {
					if pid, err := strconv.Atoi(string(data)); err == nil {
						_ = syscall.Kill(pid, syscall.SIGKILL)
					}
				}
				t.Fatal("a process the stalled command left in its group outlived it")
			}
		})
	}
}

// The run's own end, an interrupt or a hang-up, while a command or a wait
// of the arm's stalls, is the run's error as it is, never a trial's code:
// it leaves the trial neither setup_failed nor update_failed, and does not
// count toward the rule that stops a run.
func TestTheRunsEndIsNoTrialsFailure(t *testing.T) {
	h := adapter.All()[0]
	ok := []fakeUpdate{{Line: "1 pass: 1 ok", Outcome: update.OutcomeOK}}
	for _, name := range []string{"sign-in", "setup", "a hook", "the update", "a lock before the chat", "a lock after it"} {
		t.Run(name, func(t *testing.T) {
			a := testArm()
			dir := tempDir(t)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			pipe := fifo(t, dir, "stalled")
			go func() {
				f, err := os.Open(pipe)
				if err != nil {
					return
				}
				defer f.Close()
				var b [64]byte
				if n, _ := f.Read(b[:]); n > 0 && strings.HasPrefix(string(b[:n]), "stalled") {
					cancel()
				}
			}()
			s := fakeScript{Updates: ok}
			switch name {
			case "setup":
				s.SetupStall = pipe
			case "a hook":
				s.HookStall = pipe
			case "the update":
				s.Updates = []fakeUpdate{{Stall: pipe}}
			}
			tr := newTrial(t, h, s)
			if name == "sign-in" {
				link := filepath.Join(dir, "sign-in")
				if err := os.Symlink(testBinary(t), link); err != nil {
					t.Fatal(err)
				}
				writeJSON(t, filepath.Join(dir, "sign-in.json"), signInScript{Dir: tr.record, Stall: pipe})
				tr.Env = append(tr.Env, signInVar+"="+filepath.Join(dir, "sign-in.json"))
				tr.Options.SignIn = link
			}
			var err error
			if strings.HasPrefix(name, "a lock") {
				if name == "a lock after it" {
					if _, err := a.prepare(t.Context(), tr.Trial); err != nil {
						t.Fatal(err)
					}
				}
				unlock, busy, err := paths.TryLock(filepath.Join(stateDir(tr.Trial), "locks"), maintainLock)
				if err != nil || busy {
					t.Fatal(err)
				}
				defer unlock()
				a.held = func(string) { cancel() }
			}
			if name == "a lock after it" {
				_, err = a.after(ctx, tr.Trial)
			} else {
				_, err = a.prepare(ctx, tr.Trial)
			}
			var u *drive.Unjudged
			if !errors.Is(err, context.Canceled) || errors.As(err, &u) {
				t.Fatalf("the run's end gave %v", err)
			}
			if first := a.state(tr.Trial).first; first != [2]string{} {
				t.Fatalf("the run's end counted toward the stop rule: %q", first)
			}
		})
	}
}

// After waits while a job holds a lock, and only then sums the run log: a
// record a job writes before it lets go is counted.
func TestAfterWaitsBeforeItSums(t *testing.T) {
	h := adapter.All()[0]
	tr := newTrial(t, h, fakeScript{})
	plant(t, tr.Trial, person(h, "made-up-new-chat-2"))
	a := testArm()
	unlock, busy, err := paths.TryLock(filepath.Join(stateDir(tr.Trial), "locks"), updateLock)
	if err != nil || busy {
		t.Fatal(err)
	}
	var once sync.Once
	a.held = func(string) {
		once.Do(func() {
			if err := writeRecord(stateDir(tr.Trial), update.Record{Host: string(h.ID), Session: "made-up-new-chat-2", Outcome: update.OutcomeOK, TokensIn: 40, TokensOut: 2, DurationMS: 9}); err != nil {
				t.Error(err)
			}
			unlock()
		})
	}
	cost, err := a.after(t.Context(), tr.Trial)
	if err != nil || cost != (drive.Cost{Tokens: 42, Ms: 9}) {
		t.Fatalf("%+v, %v", cost, err)
	}
}

// The arm's Spent waits, as After does, while a job holds a lock, and only
// then sums the run log; once the run has ended it waits for nothing and
// sums what the run log holds. Neither counts toward the stop rule.
func TestSpentSumsTheRunLog(t *testing.T) {
	h := adapter.All()[0]
	record := func(tr drive.Trial, tokens int) {
		t.Helper()
		if err := writeRecord(stateDir(tr), update.Record{Host: string(h.ID), Session: "made-up-new-chat-3", Outcome: update.OutcomeOK, TokensIn: tokens, TokensOut: 2, DurationMS: 9}); err != nil {
			t.Fatal(err)
		}
	}
	t.Run("the run goes on", func(t *testing.T) {
		tr := newTrial(t, h, fakeScript{})
		a := testArm()
		unlock, busy, err := paths.TryLock(filepath.Join(stateDir(tr.Trial), "locks"), updateLock)
		if err != nil || busy {
			t.Fatal(err)
		}
		var once sync.Once
		a.held = func(string) {
			once.Do(func() {
				record(tr.Trial, 40)
				unlock()
			})
		}
		if cost := a.spentSoFar(t.Context(), tr.Trial); cost != (drive.Cost{Tokens: 42, Ms: 9}) {
			t.Fatalf("%+v", cost)
		}
		if first := a.state(tr.Trial).first; first != [2]string{} {
			t.Fatalf("Spent counted toward the stop rule: %q", first)
		}
	})
	t.Run("the run has ended", func(t *testing.T) {
		tr := newTrial(t, h, fakeScript{})
		record(tr.Trial, 70)
		a := testArm()
		unlock, busy, err := paths.TryLock(filepath.Join(stateDir(tr.Trial), "locks"), maintainLock)
		if err != nil || busy {
			t.Fatal(err)
		}
		defer unlock()
		a.held = func(string) { t.Error("Spent waited for a lock after the run's end") }
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		if cost := a.spentSoFar(ctx, tr.Trial); cost != (drive.Cost{Tokens: 72, Ms: 9}) {
			t.Fatalf("%+v", cost)
		}
	})
}

// plant writes made-up inbox lines into the trial's inbox.
func plant(t *testing.T, tr drive.Trial, entries ...inbox.Entry) {
	t.Helper()
	store := inbox.Store{Root: filepath.Join(stateDir(tr), "inbox")}
	for _, e := range entries {
		if err := store.Append(e); err != nil {
			t.Fatal(err)
		}
	}
}

// Memory is on when the inbox holds a session other than the replayed one
// that holds an event, on Claude Code a person's prompt: the replayed
// session alone, a program's prompts alone on Claude Code, a session's
// start alone on Claude Code, a file over 64 MiB, and an inbox that cannot
// be read are memory_off; a chat's start alone on Codex is not, and
// neither is the replayed id under the other host.
func TestMemoryOn(t *testing.T) {
	var claude, codex adapter.Host
	for _, h := range adapter.All() {
		switch h.ID {
		case adapter.ClaudeCode:
			claude = h
		case adapter.Codex:
			codex = h
		}
	}
	replayed := func(tr drive.Trial) string { return SessionID(tr) }
	cases := []struct {
		name string
		host adapter.Host
		plan func(t *testing.T, tr drive.Trial)
		on   bool
	}{
		{"nothing", claude, func(*testing.T, drive.Trial) {}, false},
		{"the replayed session alone", claude, func(t *testing.T, tr drive.Trial) {
			plant(t, tr, person(tr.Host, replayed(tr)))
		}, false},
		{"the replayed session alone, on Codex", codex, func(t *testing.T, tr drive.Trial) {
			plant(t, tr, person(tr.Host, replayed(tr)))
		}, false},
		{"another with a person's prompt", claude, func(t *testing.T, tr drive.Trial) {
			plant(t, tr, person(tr.Host, replayed(tr)), person(tr.Host, "made-up-new-chat-3"))
		}, true},
		{"a program's prompts alone", claude, func(t *testing.T, tr drive.Trial) {
			plant(t, tr, entry(tr.Host, "made-up-new-chat-4", "event", "event", "session_start"), entry(tr.Host, "made-up-new-chat-4", "event", "event", "host_prompt"),
				entry(tr.Host, "made-up-new-chat-4", "assistant", "reply", "A made-up reply."))
		}, false},
		{"a program's prompts alone, on Codex", codex, func(t *testing.T, tr drive.Trial) {
			plant(t, tr, entry(tr.Host, "made-up-new-chat-4", "event", "event", "host_prompt"))
		}, true},
		{"a start alone, on Codex", codex, func(t *testing.T, tr drive.Trial) {
			plant(t, tr, entry(tr.Host, "made-up-new-chat-5", "event", "event", "session_start"))
		}, true},
		{"a start alone", claude, func(t *testing.T, tr drive.Trial) {
			plant(t, tr, entry(tr.Host, "made-up-new-chat-5", "event", "event", "session_start"))
		}, false},
		{"the replayed id under the other host", codex, func(t *testing.T, tr drive.Trial) {
			plant(t, tr, entry(claude, replayed(tr), "event", "event", "session_start"))
		}, true},
		{"a subagent's prompt", claude, func(t *testing.T, tr drive.Trial) {
			e := person(tr.Host, "made-up-new-chat-6")
			e.Agent = "made-up-agent"
			plant(t, tr, e)
		}, false},
		{"a file over 64 MiB", claude, func(t *testing.T, tr drive.Trial) {
			e := person(tr.Host, "made-up-new-chat-7")
			plant(t, tr, e)
			path := inbox.Store{Root: filepath.Join(stateDir(tr), "inbox")}.Path(e.Project, e.Host, e.Session)
			if err := os.Truncate(path, maxSession+1); err != nil {
				t.Fatal(err)
			}
		}, false},
		{"a file of 64 MiB", claude, func(t *testing.T, tr drive.Trial) {
			e := person(tr.Host, "made-up-new-chat-8")
			plant(t, tr, e)
			path := inbox.Store{Root: filepath.Join(stateDir(tr), "inbox")}.Path(e.Project, e.Host, e.Session)
			if err := os.Truncate(path, maxSession); err != nil {
				t.Fatal(err)
			}
		}, true},
		{"an inbox that cannot be read", claude, func(t *testing.T, tr drive.Trial) {
			plant(t, tr, person(tr.Host, "made-up-new-chat-9"))
			folder := filepath.Join(stateDir(tr), "inbox", "made-up-project")
			if err := os.Chmod(folder, 0); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = os.Chmod(folder, 0o755) })
		}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			tr := newTrial(t, c.host, fakeScript{})
			c.plan(t, tr.Trial)
			on, err := memoryOn(t.Context(), tr.Trial, SessionID(tr.Trial))
			if err != nil || on != c.on {
				t.Fatalf("memory on %t, %v", on, err)
			}
			cost, err := testArm().after(t.Context(), tr.Trial)
			var u *drive.Unjudged
			switch {
			case c.on && err != nil:
				t.Fatalf("after: %v", err)
			case !c.on && (!errors.As(err, &u) || u.Code != drive.CodeMemoryOff):
				t.Fatalf("after: %+v, %v", cost, err)
			}
		})
	}
}

// After sums the tokens and the time of the run log's records of
// yesterday, today and tomorrow, by the arm's clock, and gives them with
// memory_off too; a record of another day is not summed.
func TestAfterSumsTheRunLog(t *testing.T) {
	h := adapter.All()[0]
	now := time.Date(2026, 11, 14, 12, 0, 0, 0, time.Local)
	for _, on := range []bool{true, false} {
		tr := newTrial(t, h, fakeScript{})
		if on {
			plant(t, tr.Trial, person(h, "made-up-new-chat-10"))
		}
		dir := filepath.Join(stateDir(tr.Trial), "updates")
		days := map[string]update.Record{
			now.AddDate(0, 0, -1).Format(time.DateOnly): {Host: string(h.ID), Session: "a", Outcome: update.OutcomeOK, TokensIn: 1000, TokensOut: 200, DurationMS: 3000},
			now.Format(time.DateOnly):                   {Host: string(h.ID), Session: "b", Outcome: update.OutcomeFailed, TokensIn: 30, TokensOut: 4, DurationMS: 500},
			now.AddDate(0, 0, 1).Format(time.DateOnly):  {Host: string(h.ID), Session: "c", Outcome: update.OutcomeNoChange, TokensIn: 5, TokensOut: 0, DurationMS: 7},
			now.AddDate(0, 0, -2).Format(time.DateOnly): {Host: string(h.ID), Session: "d", Outcome: update.OutcomeOK, TokensIn: 99999, TokensOut: 1, DurationMS: 1},
		}
		for day, r := range days {
			r.At = day + "T10:00:00Z"
			mkdir(t, dir)
			if err := appendJSON(filepath.Join(dir, day+".jsonl"), r); err != nil {
				t.Fatal(err)
			}
		}
		a := testArm()
		a.now = func() time.Time { return now }
		cost, err := a.after(t.Context(), tr.Trial)
		want := drive.Cost{Tokens: 1239, Ms: 3507}
		var u *drive.Unjudged
		if cost != want || on && err != nil || !on && (!errors.As(err, &u) || u.Code != drive.CodeMemoryOff) {
			t.Fatalf("memory on %t: %+v, %v", on, cost, err)
		}
	}
}

// signedIn is o with the sign-in program path, as --sign-in gives it.
func signedIn(o drive.Options, path string) drive.Options {
	o.SignIn = path
	return o
}

// Check refuses a run with no eva-md, one whose build lacks the evaluation
// tag or cannot be read, a --pass-env of either of the seam's variables,
// and a --sign-in, read from the run's options, that is not a program; a
// run that gives each right passes.
func TestCheck(t *testing.T) {
	dir := tempDir(t)
	program := filepath.Join(dir, "program")
	write(t, program, "#!/bin/sh\nexit 0\n")
	if err := os.Chmod(program, 0o755); err != nil {
		t.Fatal(err)
	}
	plain := filepath.Join(dir, "plain")
	write(t, plain, "not a program\n")
	good := drive.Options{EvaMD: testBinary(t), PassEnv: []string{"MADE_UP_KEY"}}
	if err := (&arm{}).check(signedIn(good, program)); err != nil {
		t.Fatal(err)
	}
	for name, c := range map[string]struct {
		o      drive.Options
		signIn string
		tags   bool
		want   string
	}{
		"no eva-md":      {drive.Options{}, "", false, "--eva-md"},
		"the entrypoint": {drive.Options{EvaMD: good.EvaMD, PassEnv: []string{"MADE_UP_KEY", entrypointVar}}, "", false, entrypointVar},
		"the actor":      {drive.Options{EvaMD: good.EvaMD, PassEnv: []string{actorVar}}, "", false, actorVar},
		"untagged":       {good, "", true, "evamd_eval"},
		"no Go program":  {drive.Options{EvaMD: program}, "", true, "cannot be read"},
		"relative":       {good, "program", false, "--sign-in"},
		"not a program":  {good, plain, false, "--sign-in"},
		"a folder":       {good, dir, false, "--sign-in"},
		"no file":        {good, filepath.Join(dir, "none"), false, "--sign-in"},
	} {
		t.Run(name, func(t *testing.T) {
			if c.tags {
				skipTagCheck = false
				t.Cleanup(func() { skipTagCheck = true })
			}
			err := (&arm{}).check(signedIn(c.o, c.signIn))
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("%v", err)
			}
		})
	}
	tagged := &buildinfo.BuildInfo{Settings: []debug.BuildSetting{{Key: "-tags", Value: "evamd_testlimits,evamd_eval"}}}
	if tags := buildTags(tagged); len(tags) != 2 || tags[1] != evalTag {
		t.Fatalf("tags %q", tags)
	}
	if tags := buildTags(&buildinfo.BuildInfo{Settings: []debug.BuildSetting{{Key: "-ldflags", Value: "evamd_eval"}}}); len(tags) != 0 {
		t.Fatalf("tags %q", tags)
	}
}

// The session id is the same for the same trial, differs between trials,
// and is one a hook takes.
func TestSessionID(t *testing.T) {
	one := drive.Trial{Task: task.Task{ID: "made-up-task"}, Number: 1}
	two := drive.Trial{Task: task.Task{ID: "made-up-task"}, Number: 2}
	other := drive.Trial{Task: task.Task{ID: "made-up-other"}, Number: 1}
	if SessionID(one) != SessionID(one) || SessionID(one) == SessionID(two) || SessionID(one) == SessionID(other) {
		t.Fatal("the ids do not follow the trial")
	}
	if id := SessionID(one); len(id) != 36 || strings.Count(id, "-") != 4 {
		t.Fatalf("id %s", id)
	}
}
