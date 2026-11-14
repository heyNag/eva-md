package v1arm

import (
	"errors"
	"flag"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"syscall"
	"testing"

	"github.com/heyNag/eva-md/internal/eval/continuation/drive"
	"github.com/heyNag/eva-md/internal/eval/continuation/task"
	"github.com/heyNag/eva-md/internal/eval/results"
	"github.com/heyNag/eva-md/internal/host/adapter"
	"github.com/heyNag/eva-md/internal/update"
)

var updateGolden = flag.Bool("update", false, "rewrite the golden payloads")

// commandOf names a fake eva-md call by its command and, for a hook, its
// event.
func commandOf(t *testing.T, c fakeCall) string {
	t.Helper()
	if len(c.Args) == 0 {
		return ""
	}
	if c.Args[0] != "hook" {
		return c.Args[0]
	}
	var p Payload
	if err := jsonUnmarshal(c.Stdin, &p); err != nil {
		t.Fatalf("a hook's stdin is no payload: %v", err)
	}
	return "hook " + p.Event
}

// On each host, a trial runs, in order, the sign-in program, the bare
// remote, setup with its exact arguments, the earlier chat's hooks, the
// update and the chat, each in its folder: the sign-in, setup and the
// update in the trial's home, the hooks in the workspace. The hooks lack
// the seam's two variables, the chat has the actor; the payloads equal
// the golden file; a judged trial writes no asked_again row.
func TestATrialRunsInOrder(t *testing.T) {
	for _, h := range adapter.All() {
		t.Run(string(h.ID), func(t *testing.T) {
			r := newRig(t, h)
			r.useSignIn(signInScript{})
			s, out, err := r.run()
			if err != nil || s.Judged != 1 {
				t.Fatalf("judged %d, %v\n%s", s.Judged, err, out)
			}
			// The trial's folders, as the driver made them; its run is the
			// one folder under --work it has removed by now.
			root := ""
			calls := r.calls()
			want := []string{"setup", "hook SessionStart", "hook UserPromptSubmit", "hook Stop", "hook SessionEnd", "update"}
			var got []string
			for _, c := range calls {
				got = append(got, commandOf(t, c))
				if c.Chats != 0 {
					t.Fatalf("%s ran after a chat had started", commandOf(t, c))
				}
			}
			if !slices.Equal(got, want) {
				t.Fatalf("eva-md's calls: %q", got)
			}
			home := calls[0].Home
			root = filepath.Dir(home)
			if filepath.Base(home) != "home" || filepath.Dir(filepath.Dir(filepath.Dir(filepath.Dir(root)))) != r.opts.Work {
				t.Fatalf("eva-md's home: %s", home)
			}
			workspace := filepath.Join(root, "workspace")
			setup := []string{"setup", "--dir", filepath.Join(home, "wiki"), "--remote", filepath.Join(root, "remote.git"), "--no-push", "--yes", "--host", h.Names.InstallName}
			if !slices.Equal(calls[0].Args, setup) || !calls[0].Remote {
				t.Fatalf("setup's arguments %q, the remote made before it %t", calls[0].Args, calls[0].Remote)
			}
			for _, c := range calls {
				wantDir := home
				if strings.HasPrefix(commandOf(t, c), "hook ") {
					wantDir = workspace
					if !slices.Equal(c.Args, []string{"hook", "--host", string(h.ID)}) {
						t.Fatalf("a hook's arguments: %q", c.Args)
					}
				}
				if c.Dir != wantDir || c.Home != home {
					t.Fatalf("%s ran in %s with home %s", commandOf(t, c), c.Dir, c.Home)
				}
				if c.Entrypoint || c.Actor {
					t.Fatalf("%s has a variable of the seam", commandOf(t, c))
				}
				if !slices.Contains(c.Env, fakeVar) || slices.Contains(c.Env, "GIT_CONFIG_GLOBAL") {
					t.Fatalf("%s's environment is not the trial's: %q", commandOf(t, c), c.Env)
				}
			}
			last := calls[len(calls)-1]
			if !slices.Equal(last.Args, []string{"update", "--session", string(h.ID) + ":" + SessionID(drive.Trial{Task: task.Task{ID: "hidden-banner"}, Number: 1}), "--foreground"}) {
				t.Fatalf("the update's arguments: %q", last.Args)
			}
			signIns := r.signIns()
			if len(signIns) != 1 || signIns[0].Before != 0 || signIns[0].Dir != home || signIns[0].Home != home ||
				!slices.Contains(signIns[0].Env, signInVar) || signIns[0].Entrypoint || signIns[0].Actor {
				t.Fatalf("the sign-in program's calls: %+v", signIns)
			}
			if _, err := os.Stat(filepath.Join(root, "remote.git")); err == nil {
				t.Fatal("the trial's folder was kept")
			}
			chats := r.chats()
			if len(chats) == 0 || chats[0].Dir != workspace {
				t.Fatalf("the chats: %+v", chats)
			}
			for _, c := range chats {
				if !slices.Contains(c.Env, actorVar) || slices.Contains(c.Env, entrypointVar) {
					t.Fatalf("a chat's variables: %q", c.Env)
				}
			}
			checkGolden(t, calls, workspace)
			for _, row := range r.rows() {
				if strings.HasSuffix(row.Metric, ".asked_again") {
					t.Fatalf("an asked_again row: %+v", row)
				}
			}
		})
	}
}

// checkGolden compares the hooks' payloads, the workspace's path written
// as <workspace>, with testdata/payloads.golden.
func checkGolden(t *testing.T, calls []fakeCall, workspace string) {
	t.Helper()
	var b strings.Builder
	for _, c := range calls {
		if len(c.Args) > 0 && c.Args[0] == "hook" {
			b.WriteString(strings.ReplaceAll(c.Stdin, jsonString(workspace), `"<workspace>"`) + "\n")
		}
	}
	golden := filepath.Join("testdata", "payloads.golden")
	if *updateGolden {
		write(t, golden, b.String())
	}
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatal(err)
	}
	if b.String() != string(want) {
		t.Fatalf("the payloads:\n%s\nwant:\n%s", b.String(), want)
	}
}

// The launch a prepared trial gives the chat: the plugin, and the actor
// person, nothing else.
func TestPrepareGivesThePluginAndThePerson(t *testing.T) {
	for _, h := range adapter.All() {
		tr := newTrial(t, h, fakeScript{Updates: []fakeUpdate{{Line: "1 pass: 1 ok", Outcome: update.OutcomeOK}}})
		a := testArm()
		l, err := a.prepare(t.Context(), tr.Trial)
		if err != nil || !l.Plugin || !slices.Equal(l.Env, []string{"EVA_MD_EVAL_ACTOR=person"}) || len(l.Args) != 0 {
			t.Fatalf("%s: %+v, %v", h.ID, l, err)
		}
	}
}

// A failing sign-in program, setup, plugin, hook or update, an update
// that exits 0 with no pass of the session that validated, or one whose
// only pass of it was skipped, leaves the trial unjudged with its code
// and writes no row; no chat starts.
func TestFailuresAreUnjudged(t *testing.T) {
	cases := map[string]struct {
		edit func(r *rig)
		code string
	}{
		"sign-in fails":  {func(r *rig) { r.useSignIn(signInScript{Fail: true}) }, drive.CodeSetupFailed},
		"setup fails":    {func(r *rig) { r.eva.SetupFail = []int{1} }, drive.CodeSetupFailed},
		"no plugin":      {func(r *rig) { r.eva.NoPlugin = true }, drive.CodeSetupFailed},
		"a hook fails":   {func(r *rig) { r.eva.HookFail = "Stop" }, drive.CodeSetupFailed},
		"update exits 1": {func(r *rig) { r.eva.Updates = []fakeUpdate{{Line: "1 pass: 1 ok", Outcome: update.OutcomeOK, Exit: 1}} }, drive.CodeUpdateFailed},
		"no pass":        {func(r *rig) { r.eva.Updates = []fakeUpdate{{Line: "nothing pending"}} }, drive.CodeUpdateFailed},
		"a failed pass":  {func(r *rig) { r.eva.Updates = []fakeUpdate{{Line: "1 pass: 1 failed", Outcome: update.OutcomeFailed}} }, drive.CodeUpdateFailed},
		"a skipped pass": {func(r *rig) {
			r.eva.Updates = []fakeUpdate{{Line: "1 pass: 1 skipped", Outcome: update.OutcomeSkipped}}
		}, drive.CodeUpdateFailed},
		"another session's pass": {func(r *rig) {
			r.eva.Updates = []fakeUpdate{{Line: "nothing pending"}}
			r.eva.RunLog = []update.Record{{Host: r.opts.Host, Session: "made-up-other-session", Outcome: update.OutcomeOK}}
		}, drive.CodeUpdateFailed},
		"another host's pass": {func(r *rig) {
			r.eva.Updates = []fakeUpdate{{Line: "nothing pending"}}
			r.eva.RunLog = []update.Record{{Host: "made-up-host", Session: SessionID(drive.Trial{Task: task.Task{ID: "hidden-banner"}, Number: 1}), Outcome: update.OutcomeOK}}
		}, drive.CodeUpdateFailed},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			r := newRig(t, adapter.All()[0])
			c.edit(r)
			s, out, err := r.run()
			if err != nil || s.Judged != 0 || s.Unjudged[c.code] != 1 || !strings.Contains(out, "unjudged, "+c.code+"\n") {
				t.Fatalf("judged %d, unjudged %v, %v\n%s", s.Judged, s.Unjudged, err, out)
			}
			if rows := r.rows(); len(rows) != 0 {
				t.Fatalf("%d rows", len(rows))
			}
			if chats := r.chats(); len(chats) != 0 {
				t.Fatalf("%d chats", len(chats))
			}
		})
	}
}

// The run log of the replayed session that the busy update leaves to a
// detached job counts: the job's ok, then the update's nothing pending,
// is judged. A run log with a pass of the session that validated as
// no_change counts too.
func TestADetachedJobsPassCounts(t *testing.T) {
	r := newRig(t, adapter.All()[0])
	release := fifo(t, r.base, "release")
	r.eva.Hold = &hold{Locks: []string{updateLock}, Release: release, Outcome: update.OutcomeOK}
	r.eva.Updates = []fakeUpdate{{Line: "nothing pending"}}
	releaseWhenHeld(t, release)
	s, out, err := r.run()
	if err != nil || s.Judged != 1 {
		t.Fatalf("judged %d, %v\n%s", s.Judged, err, out)
	}
	calls := r.calls()
	var busy, updates int
	for _, c := range calls {
		if c.Args[0] == "update" {
			updates++
			if c.Busy {
				busy++
			}
		}
	}
	if busy != 1 || updates != 2 {
		t.Fatalf("updates %d, busy %d", updates, busy)
	}
	if got := childChats(t, r.record); !slices.Equal(got, []int{0}) {
		t.Fatalf("the job let go with chats %v started", got)
	}

	r = newRig(t, adapter.All()[0])
	r.eva.Updates = []fakeUpdate{{Line: "1 pass: 1 no_change", Outcome: update.OutcomeNoChange}}
	if s, out, err := r.run(); err != nil || s.Judged != 1 {
		t.Fatalf("no_change: judged %d, %v\n%s", s.Judged, err, out)
	}
}

// No chat starts while the update says another job holds its lock, nor
// while a job the hooks left holds the update or the maintain lock: the
// update is run again once the lock is free, and the chat starts once
// both are.
func TestNoChatWhileAJobHoldsALock(t *testing.T) {
	t.Run("the update's line", func(t *testing.T) {
		r := newRig(t, adapter.All()[0])
		r.eva.Updates = []fakeUpdate{{Line: "stopped: busy"}, {Line: "1 pass: 1 skipped; stopped: busy, budget"}, {Line: "1 pass: 1 ok", Outcome: update.OutcomeOK}}
		s, out, err := r.run()
		if err != nil || s.Judged != 1 {
			t.Fatalf("judged %d, %v\n%s", s.Judged, err, out)
		}
		n := 0
		for _, c := range r.calls() {
			if c.Args[0] == "update" {
				n++
			}
		}
		if n != 3 {
			t.Fatalf("the update ran %d times", n)
		}
	})
	for _, locks := range [][]string{{maintainLock}, {updateLock, maintainLock}} {
		t.Run(strings.Join(locks, " and "), func(t *testing.T) {
			r := newRig(t, adapter.All()[0])
			release := fifo(t, r.base, "release")
			r.eva.Hold = &hold{Locks: locks, Release: release, Outcome: update.OutcomeOK}
			releaseWhenHeld(t, release)
			if slices.Contains(locks, updateLock) {
				r.eva.Updates = []fakeUpdate{{Line: "nothing pending"}}
			}
			s, out, err := r.run()
			if err != nil || s.Judged != 1 {
				t.Fatalf("judged %d, %v\n%s", s.Judged, err, out)
			}
			if got := childChats(t, r.record); !slices.Equal(got, []int{0}) {
				t.Fatalf("the job let go with chats %v started", got)
			}
			if len(r.chats()) == 0 {
				t.Fatal("no chat started once the locks were free")
			}
		})
	}
}

// fifo makes a named pipe in dir.
func fifo(t *testing.T, dir, name string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := syscall.Mkfifo(path, 0o600); err != nil {
		t.Fatal(err)
	}
	// A job still waiting when the test ends is let go.
	t.Cleanup(func() { letGo(path) })
	return path
}

// letGo writes a line to the named pipe a job waits on, if one does.
func letGo(path string) {
	f, err := os.OpenFile(path, os.O_WRONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return
	}
	_, _ = f.WriteString("go\n")
	f.Close()
}

// releaseWhenHeld lets the job go, through the named pipe release, once
// the registered arm has found one of its locks held: so the arm is
// waiting on it when it lets go.
func releaseWhenHeld(t *testing.T, release string) {
	t.Helper()
	var once sync.Once
	eva.held = func(string) {
		once.Do(func() {
			go func() {
				f, err := os.OpenFile(release, os.O_WRONLY, 0)
				if err != nil {
					return
				}
				_, _ = f.WriteString("go\n")
				f.Close()
			}()
		})
	}
	t.Cleanup(func() { eva.held = nil })
}

// childChats reads, for each job a fake hook left, the chats started when
// it let go.
func childChats(t *testing.T, record string) []int {
	t.Helper()
	lines, err := readLines[map[string]int](filepath.Join(record, "child.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	var got []int
	for _, l := range lines {
		got = append(got, l["chats"])
	}
	return got
}

// When the first two trials of a run both end setup_failed or
// update_failed, the third stops the run before it starts anything; a
// later failure is counted and the run goes on.
func TestARunStopsWhenItsFirstTwoTrialsFail(t *testing.T) {
	for name, c := range map[string]struct {
		setupFail []int
		updates   []fakeUpdate
	}{
		"setup twice":           {setupFail: []int{1, 2, 3}},
		"setup, then update":    {setupFail: []int{1}, updates: []fakeUpdate{{Line: "nothing pending"}}},
		"the update both times": {updates: []fakeUpdate{{Line: "1 pass: 1 failed", Outcome: update.OutcomeFailed}}},
	} {
		t.Run(name, func(t *testing.T) {
			r := newRig(t, adapter.All()[0])
			r.opts.Trials = 4
			r.eva.SetupFail = c.setupFail
			if c.updates != nil {
				r.eva.Updates = c.updates
			}
			s, out, err := r.run()
			if err == nil || !strings.Contains(err.Error(), "first two trials") || s.Unjudged[drive.CodeSetupFailed]+s.Unjudged[drive.CodeUpdateFailed] != 2 || s.NotRun != 1 {
				t.Fatalf("unjudged %v, not run %d, %v\n%s", s.Unjudged, s.NotRun, err, out)
			}
			if n := count(r.calls(), "setup"); n != 2 {
				t.Fatalf("setup ran %d times", n)
			}
			records, rerr := drive.LoadRuns(r.opts.Out)
			if rerr != nil || len(records) != 1 || records[0].Ended != drive.EndedError {
				t.Fatalf("the run's record: %+v, %v", records, rerr)
			}
		})
	}
	t.Run("a later failure", func(t *testing.T) {
		r := newRig(t, adapter.All()[0])
		r.opts.Trials = 4
		r.eva.SetupFail = []int{2, 3, 4}
		s, out, err := r.run()
		if err != nil || s.Judged != 1 || s.Unjudged[drive.CodeSetupFailed] != 3 || s.NotRun != 0 {
			t.Fatalf("judged %d, unjudged %v, %v\n%s", s.Judged, s.Unjudged, err, out)
		}
	})
}

// The sign-in program runs once in each trial, before setup, and a word it
// prints is in no row, output or error.
func TestTheSignInProgramRunsOnceATrial(t *testing.T) {
	const word = "madeupsigninword"
	r := newRig(t, adapter.All()[0])
	r.opts.Trials = 2
	r.useSignIn(signInScript{Print: word})
	s, out, err := r.run()
	if err != nil || s.Judged != 2 {
		t.Fatalf("judged %d, %v\n%s", s.Judged, err, out)
	}
	signIns := r.signIns()
	if len(signIns) != 2 || signIns[0].Before != 0 || signIns[1].Before != 6 || signIns[0].Home == signIns[1].Home {
		t.Fatalf("the sign-in program's calls: %+v", signIns)
	}
	noWord(t, r, word, out, err)
}

// noWord fails when word is in the run's output, its error, a row or the
// run's record.
func noWord(t *testing.T, r *rig, word, out string, err error) {
	t.Helper()
	if strings.Contains(out, word) || err != nil && strings.Contains(err.Error(), word) {
		t.Fatalf("the run's output or error holds the word:\n%s\n%v", out, err)
	}
	for _, name := range []string{results.FileName, drive.RunsFile} {
		data, rerr := os.ReadFile(filepath.Join(r.opts.Out, name))
		if rerr != nil && !errors.Is(rerr, os.ErrNotExist) {
			t.Fatal(rerr)
		}
		if strings.Contains(string(data), word) {
			t.Fatalf("%s holds the word", name)
		}
	}
}

// What eva-md's updates spent, by its run log, is in the rows as
// memory_tokens and memory_ms, and counts toward --max-tokens; under
// memory_off too, which stops the run.
func TestTheRunLogsSpendIsCounted(t *testing.T) {
	h := adapter.All()[0]
	spend := func(r *rig) {
		r.eva.Updates = []fakeUpdate{{Line: "1 pass: 1 ok", Outcome: update.OutcomeOK, TokensIn: 300, TokensOut: 40, DurationMS: 1200}}
		r.eva.RunLog = []update.Record{{Host: string(h.ID), Session: "made-up-other-session", Outcome: update.OutcomeSkipped, TokensIn: 7, TokensOut: 3, DurationMS: 5}}
	}
	r := newRig(t, h)
	spend(r)
	s, out, err := r.run()
	if err != nil || s.Judged != 1 {
		t.Fatalf("judged %d, %v\n%s", s.Judged, err, out)
	}
	values := map[string]float64{}
	for _, row := range r.rows() {
		values[row.Metric[strings.LastIndex(row.Metric, ".")+1:]] = row.Value
	}
	if values["memory_tokens"] != 350 || values["memory_ms"] != 1205 {
		t.Fatalf("rows: %v", values)
	}
	// The chat's tokens: the whole less the run log's.
	chat := s.Tokens - 350
	if chat <= 0 {
		t.Fatalf("tokens %d", s.Tokens)
	}

	// A ceiling the chat alone would not reach stops the second trial.
	r = newRig(t, h)
	spend(r)
	r.opts.Trials, r.opts.MaxTokens = 2, chat+1
	s, out, err = r.run()
	if err != nil || s.Judged != 1 || s.NotRun != 1 || s.Tokens != chat+350 {
		t.Fatalf("judged %d, not run %d, tokens %d, %v\n%s", s.Judged, s.NotRun, s.Tokens, err, out)
	}

	// memory_off: no row, and the spend counted.
	r = newRig(t, h)
	spend(r)
	r.eva.Plant = nil
	r.opts.Trials = 2
	s, out, err = r.run()
	var stopped *drive.Stopped
	if !errors.As(err, &stopped) || stopped.Code != drive.CodeMemoryOff || s.Unjudged[drive.CodeMemoryOff] != 1 || s.NotRun != 1 || s.Tokens != chat+350 {
		t.Fatalf("unjudged %v, not run %d, tokens %d, %v\n%s", s.Unjudged, s.NotRun, s.Tokens, err, out)
	}
	if rows := r.rows(); len(rows) != 0 {
		t.Fatalf("%d rows", len(rows))
	}
	records, err := drive.LoadRuns(r.opts.Out)
	if err != nil || len(records) != 1 || records[0].Tokens != chat+350 || records[0].Ended != drive.CodeMemoryOff {
		t.Fatalf("the run's record: %+v, %v", records, err)
	}
}

// A word planted in every turn of the earlier chat, which eva-md prints
// on stdout and stderr, as the sign-in program does, is in no row, output
// or error, whether the trial is judged, fails its setup, a hook or its
// update, or finds memory off; the hooks did get it.
func TestTheEarlierChatsWordsStayOut(t *testing.T) {
	const word = "madeupturnwordqx"
	for name, edit := range map[string]func(r *rig){
		"judged":       func(r *rig) {},
		"setup fails":  func(r *rig) { r.eva.SetupFail = []int{1} },
		"a hook fails": func(r *rig) { r.eva.HookFail = "UserPromptSubmit" },
		"update fails": func(r *rig) { r.eva.Updates = []fakeUpdate{{Line: "1 pass: 1 failed " + word, Outcome: update.OutcomeFailed, Exit: 1}} },
		"memory off":   func(r *rig) { r.eva.Plant = nil },
	} {
		t.Run(name, func(t *testing.T) {
			r := newRig(t, adapter.All()[0])
			plantWord(t, r.opts.Tasks, word)
			r.eva.Print = word
			r.useSignIn(signInScript{Print: word})
			edit(r)
			_, out, err := r.run()
			noWord(t, r, word, out, err)
			reached := false
			for _, c := range r.calls() {
				reached = reached || strings.Contains(c.Stdin, word)
			}
			if !reached && name != "setup fails" {
				t.Fatal("no hook was given the word")
			}
		})
	}
}

// plantWord adds word to every turn of the fixture task's earlier chat.
func plantWord(t *testing.T, tasks, word string) {
	t.Helper()
	file := filepath.Join(tasks, "hidden-banner", task.FileName)
	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	var x map[string]any
	if err := jsonUnmarshal(string(data), &x); err != nil {
		t.Fatal(err)
	}
	for _, turn := range x["history"].([]any) {
		m := turn.(map[string]any)
		m["text"] = m["text"].(string) + " The made-up word is " + word + "."
	}
	writeJSON(t, file, x)
}
