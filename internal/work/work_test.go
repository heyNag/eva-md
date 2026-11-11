package work

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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
	"github.com/heyNag/eva-md/internal/paths"
)

func done(context.Context, []Mark, io.Writer) Outcome { return Done }

func TestTheSwitchIsOffByDefault(t *testing.T) {
	if Switch.On(config.Config{}) {
		t.Fatal("the worker switch is on in an empty config")
	}
	if !Switch.On(config.Config{Switches: map[string]any{"worker": true}}) {
		t.Fatal("worker = true does not turn the switch on")
	}
}

func TestRegisterRefusesABadJob(t *testing.T) {
	withJobs(t, Job{Kind: "sync", Run: done})
	for name, j := range map[string]Job{
		"twice":       {Kind: "sync", Run: done},
		"empty kind":  {Kind: "", Run: done},
		"hyphen":      {Kind: "a-b", Run: done},
		"upper case":  {Kind: "Sync", Run: done},
		"digit first": {Kind: "1sync", Run: done},
		"too long":    {Kind: strings.Repeat("a", 33), Run: done},
		"no run":      {Kind: "other"},
	} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("%s: Register did not panic", name)
				}
			}()
			Register(j)
		}()
	}
}

func TestWakeRefusesABadMarkAndWritesNothing(t *testing.T) {
	withJobs(t, Job{Kind: "sync", Run: done})
	dirs := testDirs(t)
	good := Mark{Kind: "sync", Key: "claude-code-1", Reason: "stop", Host: "claude-code"}
	for name, bad := range map[string]Mark{
		"unregistered kind": {Kind: "other", Key: "k", Reason: "stop"},
		"empty key":         {Kind: "sync", Key: "", Reason: "stop"},
		"key with a slash":  {Kind: "sync", Key: "a/b", Reason: "stop"},
		"key with a colon":  {Kind: "sync", Key: "claude-code:1", Reason: "stop"},
		"dot key":           {Kind: "sync", Key: "..", Reason: "stop"},
		"upper-case key":    {Kind: "sync", Key: "ABC", Reason: "stop"},
		"mixed-case key":    {Kind: "sync", Key: "claude-code-Ab1", Reason: "stop"},
		"no reason":         {Kind: "sync", Key: "k", Reason: ""},
		"two words":         {Kind: "sync", Key: "k", Reason: "two words"},
		"upper reason":      {Kind: "sync", Key: "k", Reason: "Stop"},
		"long reason":       {Kind: "sync", Key: "k", Reason: strings.Repeat("a", 33)},
		"host with a space": {Kind: "sync", Key: "k", Reason: "stop", Host: "claude code"},
	} {
		if err := Wake(context.Background(), dirs, []Mark{good, bad}, Options{}); err == nil {
			t.Errorf("%s: Wake took %+v", name, bad)
		}
	}
	if _, err := os.Stat(filepath.Join(dirs.State, "work")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("a refused wake made the work folder: %v", err)
	}
}

func TestTheSameMarkTwiceLeavesOne(t *testing.T) {
	withJobs(t, Job{Kind: "sync", Run: done})
	dirs := testDirs(t)
	first := time.Date(2026, 11, 10, 7, 0, 0, 123e6, time.UTC)
	leaveMarksAt(t, dirs, first, Mark{Kind: "sync", Key: "k1", Reason: "stop", Host: "codex"})
	leaveMarksAt(t, dirs, first.Add(time.Hour), Mark{Kind: "sync", Key: "k1", Reason: "end"}, Mark{Kind: "sync", Key: "k1", Reason: "compact", Host: "codex"})
	if got := names(t, dirs, wakeDir); !slices.Equal(got, []string{"sync-k1"}) {
		t.Fatalf("wake holds %q", got)
	}
	// The last write's words, and the first write's time.
	data, err := os.ReadFile(filepath.Join(dirs.State, "work", wakeDir, "sync-k1"))
	if want := fmt.Sprintf("compact codex %d\n", first.UnixMilli()); err != nil || string(data) != want {
		t.Fatalf("the mark holds %q, %v, want %q", data, err, want)
	}
}

// Keys differ only by letter case on no file system: a file in wake/ whose
// key has an upper-case letter, which no wake writes, is not a mark.
func TestAFileWithAnUpperCaseKeyIsNotAMark(t *testing.T) {
	r := newRecorder()
	withJobs(t, Job{Kind: "sync", Run: r.run})
	dirs := testDirs(t)
	leaveMarks(t, dirs, Mark{Kind: "sync", Key: "k1", Reason: "stop"})
	if err := os.WriteFile(filepath.Join(dirs.State, "work", wakeDir, "sync-K2"), []byte("stop\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := Wake(context.Background(), dirs, nil, Options{}); err != nil {
		t.Fatal(err)
	}
	if got := r.all(); len(got) != 1 || !slices.Equal(keysOf(got[0]), []string{"k1"}) {
		t.Fatalf("ran %+v", got)
	}
	if got := names(t, dirs, wakeDir); !slices.Equal(got, []string{"sync-K2"}) {
		t.Fatalf("wake holds %q", got)
	}
}

func TestFoldersAndFilesArePrivate(t *testing.T) {
	r := newRecorder()
	withJobs(t, Job{Kind: "sync", Run: r.run})
	dirs := testDirs(t)
	leaveMarks(t, dirs, Mark{Kind: "sync", Key: "k1", Reason: "stop"})
	check := func(rel string, want os.FileMode) {
		t.Helper()
		info, err := os.Lstat(filepath.Join(dirs.State, "work", rel))
		if err != nil {
			t.Fatal(err)
		}
		if got := info.Mode().Perm(); got != want {
			t.Errorf("%s has mode %o, want %o", rel, got, want)
		}
	}
	check(".", 0o700)
	for _, d := range []string{wakeDir, runDir, journalDir} {
		check(d, 0o700)
	}
	check(filepath.Join(wakeDir, "sync-k1"), 0o600)
	if err := Wake(context.Background(), dirs, nil, Options{}); err != nil {
		t.Fatal(err)
	}
	files, _ := filepath.Glob(filepath.Join(dirs.State, "work", journalDir, "*"))
	if len(files) != 1 {
		t.Fatalf("journal files %q", files)
	}
	check(filepath.Join(journalDir, filepath.Base(files[0])), 0o600)
}

func TestTheLowestPriorityRunsFirstAndEachKindGetsAllItsMarks(t *testing.T) {
	var mu sync.Mutex
	var order []string
	job := func(kind string, prio int) Job {
		return Job{Kind: kind, Priority: prio, Run: func(_ context.Context, marks []Mark, _ io.Writer) Outcome {
			mu.Lock()
			defer mu.Unlock()
			order = append(order, kind+":"+strings.Join(keysOf(marks), ","))
			return Done
		}}
	}
	withJobs(t, job("tidy", 4), job("sync", 3), job("asked", 3), job("first", 0))
	dirs := testDirs(t)
	leaveMarks(t, dirs,
		Mark{Kind: "tidy", Key: "daily", Reason: "tick"},
		Mark{Kind: "sync", Key: "b", Reason: "stop"},
		Mark{Kind: "sync", Key: "a", Reason: "end"},
		Mark{Kind: "asked", Key: "x", Reason: "asked"},
	)
	if err := Wake(context.Background(), dirs, []Mark{{Kind: "first", Key: "f", Reason: "tick"}}, Options{}); err != nil {
		t.Fatal(err)
	}
	want := []string{"first:f", "asked:x", "sync:a,b", "tidy:daily"}
	if !slices.Equal(order, want) {
		t.Fatalf("ran %q, want %q", order, want)
	}
	if got := names(t, dirs, wakeDir); len(got) != 0 {
		t.Fatalf("marks left: %q", got)
	}
	if got := names(t, dirs, runDir); len(got) != 0 {
		t.Fatalf("runs left: %q", got)
	}
}

func TestAMarkWrittenAgainDuringItsRunRunsAgain(t *testing.T) {
	dirs := testDirs(t)
	var calls [][]Mark
	withJobs(t, Job{Kind: "sync", Run: func(ctx context.Context, marks []Mark, _ io.Writer) Outcome {
		calls = append(calls, marks)
		if len(calls) == 1 {
			// The same mark, written while its run goes on.
			root, err := openWork(dirs)
			if err != nil {
				t.Error(err)
				return Failed
			}
			defer root.Close()
			if err := writeMark(ctx, root, Mark{Kind: "sync", Key: "k1", Reason: "again"}, time.Now()); err != nil {
				t.Error(err)
			}
		}
		return Done
	}})
	if err := Wake(context.Background(), dirs, []Mark{{Kind: "sync", Key: "k1", Reason: "stop"}}, Options{}); err != nil {
		t.Fatal(err)
	}
	want := [][]Mark{{{Kind: "sync", Key: "k1", Reason: "stop"}}, {{Kind: "sync", Key: "k1", Reason: "again"}}}
	if !reflect.DeepEqual(calls, want) {
		t.Fatalf("calls %+v, want %+v", calls, want)
	}
	if got := names(t, dirs, wakeDir); len(got) != 0 {
		t.Fatalf("marks left: %q", got)
	}
}

func TestABusyJobKeepsItsMarksAndRunsOnceInAWorkersRun(t *testing.T) {
	dirs := testDirs(t)
	syncCalls, tidyCalls := 0, 0
	syncOut := Busy
	withJobs(t,
		Job{Kind: "sync", Priority: 1, Run: func(ctx context.Context, marks []Mark, _ io.Writer) Outcome {
			syncCalls++
			if syncCalls == 1 {
				// A wake for the same kind arrives while it runs.
				root, err := openWork(dirs)
				if err != nil {
					t.Error(err)
					return Failed
				}
				defer root.Close()
				if err := writeMark(ctx, root, Mark{Kind: "sync", Key: "k2", Reason: "stop"}, time.Now()); err != nil {
					t.Error(err)
				}
			}
			return syncOut
		}},
		Job{Kind: "tidy", Priority: 2, Run: func(context.Context, []Mark, io.Writer) Outcome {
			tidyCalls++
			return Done
		}},
	)
	marks := []Mark{{Kind: "sync", Key: "k1", Reason: "stop"}, {Kind: "tidy", Key: "daily", Reason: "tick"}}
	if err := Wake(context.Background(), dirs, marks, Options{}); err != nil {
		t.Fatal(err)
	}
	if syncCalls != 1 || tidyCalls != 1 {
		t.Fatalf("sync ran %d times and tidy %d, want once each", syncCalls, tidyCalls)
	}
	if got := names(t, dirs, wakeDir); !slices.Equal(got, []string{"sync-k1", "sync-k2"}) {
		t.Fatalf("wake holds %q", got)
	}
	var outcomes []string
	for _, e := range journal(t, dirs) {
		if e.Ev == evFinish {
			outcomes = append(outcomes, e.Kind+":"+string(e.Outcome))
		}
	}
	if !slices.Equal(outcomes, []string{"sync:busy", "tidy:done"}) {
		t.Fatalf("finishes %q", outcomes)
	}
	// The next wake, a tick, runs them.
	syncOut = Done
	if err := Wake(context.Background(), dirs, nil, Options{}); err != nil {
		t.Fatal(err)
	}
	if syncCalls != 2 {
		t.Fatalf("sync ran %d times after the tick, want 2", syncCalls)
	}
	if got := names(t, dirs, wakeDir); len(got) != 0 {
		t.Fatalf("marks left: %q", got)
	}
}

func TestACutRunPutsItsMarksBack(t *testing.T) {
	dirs := testDirs(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	calls := 0
	withJobs(t, Job{Kind: "sync", Run: func(ctx context.Context, _ []Mark, _ io.Writer) Outcome {
		calls++
		cancel()
		<-ctx.Done()
		return Cut
	}})
	err := Wake(ctx, dirs, []Mark{{Kind: "sync", Key: "k1", Reason: "stop"}}, Options{})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Wake returned %v", err)
	}
	if calls != 1 {
		t.Fatalf("ran %d times", calls)
	}
	if got := names(t, dirs, wakeDir); !slices.Equal(got, []string{"sync-k1"}) {
		t.Fatalf("wake holds %q", got)
	}
	if got := names(t, dirs, runDir); len(got) != 0 {
		t.Fatalf("runs left: %q", got)
	}
}

func TestAnUnknownOutcomeOrAPanicIsFailedAndUsesItsMarks(t *testing.T) {
	dirs := testDirs(t)
	withJobs(t,
		Job{Kind: "odd", Priority: 1, Run: func(context.Context, []Mark, io.Writer) Outcome { return "maybe" }},
		Job{Kind: "boom", Priority: 2, Run: func(context.Context, []Mark, io.Writer) Outcome { panic("boom") }},
	)
	var log strings.Builder
	marks := []Mark{{Kind: "odd", Key: "k", Reason: "stop"}, {Kind: "boom", Key: "k", Reason: "stop"}}
	if err := Wake(context.Background(), dirs, marks, Options{Log: &log}); err != nil {
		t.Fatal(err)
	}
	var outcomes []string
	for _, e := range journal(t, dirs) {
		if e.Ev == evFinish {
			outcomes = append(outcomes, e.Kind+":"+string(e.Outcome))
		}
	}
	if !slices.Equal(outcomes, []string{"odd:failed", "boom:failed"}) {
		t.Fatalf("finishes %q", outcomes)
	}
	if got := names(t, dirs, wakeDir); len(got) != 0 {
		t.Fatalf("marks left: %q", got)
	}
	if !strings.Contains(log.String(), "the boom job stopped") {
		t.Fatalf("log %q", log.String())
	}
}

func TestTheJournalLinesAndTheirKeys(t *testing.T) {
	dirs := testDirs(t)
	clock := time.Date(2026, 11, 10, 7, 1, 2, 123456789, time.FixedZone("x", 3600))
	var startOnDisk bool
	withJobs(t, Job{Kind: "sync", Priority: 3, Run: func(context.Context, []Mark, io.Writer) Outcome {
		// The start line is on disk before the job starts.
		startOnDisk = fileHas(filepath.Join(dirs.State, "work", journalDir, "2026-11.jsonl"), `"ev":"start"`)
		return Done
	}})
	marks := []Mark{
		{Kind: "sync", Key: "claude-code-1", Reason: "stop", Host: "claude-code"},
		{Kind: "sync", Key: "codex-2", Reason: "stop", Host: "codex"},
		{Kind: "sync", Key: "sweep-2026-11-10", Reason: "sweep"},
	}
	if err := Wake(context.Background(), dirs, marks, Options{Now: func() time.Time { return clock }}); err != nil {
		t.Fatal(err)
	}
	if !startOnDisk {
		t.Fatal("the job started before its start line was on disk")
	}
	text := journalText(t, dirs)
	lines := strings.Split(strings.TrimSuffix(text, "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("journal %q", text)
	}
	wantKeys := map[string][]string{
		evStart:  {"t", "run", "ev", "kind", "prio", "marks", "reasons", "hosts"},
		evFinish: {"t", "run", "ev", "kind", "prio", "marks", "reasons", "hosts", "outcome", "ms"},
	}
	var run string
	for i, line := range lines {
		var m map[string]any
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			t.Fatal(err)
		}
		ev := []string{evStart, evFinish}[i]
		var keys []string
		for k := range m {
			keys = append(keys, k)
		}
		if !sameSet(keys, wantKeys[ev]) {
			t.Errorf("line %d has keys %q, want %q", i, keys, wantKeys[ev])
		}
		if m["ev"] != ev || m["t"] != "2026-11-10T06:01:02.123Z" || m["kind"] != "sync" || m["prio"] != 3.0 || m["marks"] != 3.0 {
			t.Errorf("line %d: %s", i, line)
		}
		if !reflect.DeepEqual(m["reasons"], []any{"stop", "sweep"}) || !reflect.DeepEqual(m["hosts"], []any{"claude-code", "codex"}) {
			t.Errorf("line %d words: %s", i, line)
		}
		if run == "" {
			run, _ = m["run"].(string)
		}
		if m["run"] != run || !runIDPattern.MatchString(run) {
			t.Errorf("line %d run %v", i, m["run"])
		}
	}
	if !strings.Contains(lines[1], `"outcome":"done"`) {
		t.Errorf("finish %s", lines[1])
	}
}

func sameSet(a, b []string) bool {
	a, b = slices.Clone(a), slices.Clone(b)
	slices.Sort(a)
	slices.Sort(b)
	return slices.Equal(a, b)
}

func TestTheJournalEndsALineAKilledWorkerLeftUnfinished(t *testing.T) {
	dirs := testDirs(t)
	r := newRecorder()
	withJobs(t, Job{Kind: "sync", Run: r.run})
	clock := time.Date(2026, 11, 10, 7, 0, 0, 0, time.UTC)
	leaveMarks(t, dirs, Mark{Kind: "sync", Key: "k", Reason: "stop"})
	if err := os.WriteFile(filepath.Join(dirs.State, "work", journalDir, "2026-11.jsonl"), []byte(`{"t":"2026-11-10T06:00:00.000Z","run":"00`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := Wake(context.Background(), dirs, nil, Options{Now: func() time.Time { return clock }}); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSuffix(journalText(t, dirs), "\n"), "\n")
	if len(lines) != 3 {
		t.Fatalf("journal %q", lines)
	}
	for _, line := range lines[1:] {
		var e entry
		if err := json.Unmarshal([]byte(line), &e); err != nil || e.Kind != "sync" {
			t.Fatalf("line %q: %v", line, err)
		}
	}
}

func TestTheWorkerRemovesJournalFilesOlderThanThirteenMonths(t *testing.T) {
	dirs := testDirs(t)
	withJobs(t, Job{Kind: "sync", Run: done})
	leaveMarks(t, dirs)
	dir := filepath.Join(dirs.State, "work", journalDir)
	for _, name := range []string{"2025-08.jsonl", "2025-09.jsonl", "2025-10.jsonl", "2026-11.jsonl", "notes.txt"} {
		if err := os.WriteFile(filepath.Join(dir, name), nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	now := time.Date(2026, 11, 11, 12, 0, 0, 0, time.UTC)
	if err := Wake(context.Background(), dirs, nil, Options{Now: func() time.Time { return now }}); err != nil {
		t.Fatal(err)
	}
	// 2025-09 ended on 1 October 2025, more than 13 months before 11
	// November 2026; 2025-10 ended less than 13 months before.
	if got := names(t, dirs, journalDir); !slices.Equal(got, []string{"2025-10.jsonl", "2026-11.jsonl", "notes.txt"}) {
		t.Fatalf("journal holds %q", got)
	}
}

// plantRun leaves run/<id>/ with marks, as a worker killed during that run
// would, and the journal lines given.
func plantRun(t *testing.T, dirs paths.Dirs, id string, marks []Mark, lines ...entry) {
	t.Helper()
	dir := filepath.Join(dirs.State, "work", runDir, id)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, m := range marks {
		if err := os.WriteFile(filepath.Join(dir, m.name()), []byte(markText(m, time.Now())), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	root, err := openWork(dirs)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	for _, e := range lines {
		if err := appendJournal(root, e); err != nil {
			t.Fatal(err)
		}
	}
}

func TestAWorkerSettlesTheRunsAKilledWorkerLeft(t *testing.T) {
	const (
		finishedDone = "0000000000000001"
		finishedBusy = "0000000000000002"
		started      = "0000000000000003"
		interrupted  = "0000000000000004"
		neverStarted = "0000000000000005"
	)
	dirs := testDirs(t)
	r := newRecorder()
	withJobs(t, Job{Kind: "sync", Priority: 2, Run: r.run})
	leaveMarks(t, dirs, Mark{Kind: "sync", Key: "k3", Reason: "newer"})
	t0 := "2026-11-10T06:00:00.000Z"
	line := func(id, ev string, out Outcome) entry {
		return entry{T: t0, Run: id, Ev: ev, Kind: "sync", Prio: 2, Marks: 1, Reasons: []string{"stop"}, Hosts: []string{}, Outcome: out}
	}
	m := func(key string) []Mark { return []Mark{{Kind: "sync", Key: key, Reason: "stop"}} }
	plantRun(t, dirs, finishedDone, m("k1"), line(finishedDone, evStart, ""), line(finishedDone, evFinish, Done))
	plantRun(t, dirs, finishedBusy, m("k2"), line(finishedBusy, evStart, ""), line(finishedBusy, evFinish, Busy))
	plantRun(t, dirs, started, m("k3"), line(started, evStart, ""))
	plantRun(t, dirs, interrupted, m("k4"), line(interrupted, evStart, ""), line(interrupted, evInterrupted, ""))
	plantRun(t, dirs, neverStarted, m("k5"))
	now := func() time.Time { return time.Date(2026, 11, 10, 7, 0, 0, 0, time.UTC) }
	if err := Wake(context.Background(), dirs, nil, Options{Now: now}); err != nil {
		t.Fatal(err)
	}
	calls := r.all()
	if len(calls) != 1 {
		t.Fatalf("calls %+v", calls)
	}
	want := []Mark{
		{Kind: "sync", Key: "k2", Reason: "stop"},
		{Kind: "sync", Key: "k3", Reason: "newer"}, // the newer mark stays
		{Kind: "sync", Key: "k4", Reason: "stop"},
		{Kind: "sync", Key: "k5", Reason: "stop"},
	}
	if !reflect.DeepEqual(calls[0], want) {
		t.Fatalf("ran %+v, want %+v", calls[0], want)
	}
	var cuts []string
	for _, e := range journal(t, dirs) {
		if e.Ev == evInterrupted {
			cuts = append(cuts, e.Run)
		}
	}
	if !slices.Equal(cuts, []string{interrupted, started}) {
		t.Fatalf("interrupted lines for %q", cuts)
	}
	if got := names(t, dirs, runDir); len(got) != 0 {
		t.Fatalf("runs left: %q", got)
	}
}

func TestAWaiterCutByItsContextLeavesItsMarkToTheWorker(t *testing.T) {
	dirs := testDirs(t)
	release := make(chan struct{})
	started := make(chan struct{})
	var mu sync.Mutex
	var ran []string
	withJobs(t, Job{Kind: "sync", Run: func(_ context.Context, marks []Mark, _ io.Writer) Outcome {
		mu.Lock()
		first := len(ran) == 0
		ran = append(ran, keysOf(marks)...)
		mu.Unlock()
		if first {
			close(started)
			<-release
		}
		return Done
	}})
	worker := make(chan error, 1)
	go func() {
		worker <- Wake(context.Background(), dirs, []Mark{{Kind: "sync", Key: "k1", Reason: "stop"}}, Options{})
	}()
	select {
	case <-started:
	case <-time.After(hangGuard):
		t.Fatal("the worker did not start its job")
	}
	ctx, cancel := context.WithCancel(context.Background())
	waiting := make(chan struct{})
	waiter := make(chan error, 1)
	go func() {
		waiter <- Wake(ctx, dirs, []Mark{{Kind: "sync", Key: "k2", Reason: "stop"}}, Options{hook: func(p string) {
			if p == "waiter" {
				close(waiting)
			}
		}})
	}()
	select {
	case <-waiting:
	case <-time.After(hangGuard):
		t.Fatal("the second wake did not become the waiter")
	}
	cancel()
	select {
	case err := <-waiter:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("the waiter returned %v", err)
		}
	case <-time.After(hangGuard):
		t.Fatal("the waiter did not stop with its context")
	}
	// The waiter's lock is free again.
	unlock, busy, err := paths.TryLock(dirs.Locks(), waiterLock)
	if err != nil || busy {
		t.Fatalf("waiter.lock: busy %v, %v", busy, err)
	}
	unlock()
	close(release)
	select {
	case err := <-worker:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(hangGuard):
		t.Fatal("the worker did not finish")
	}
	if !slices.Equal(ran, []string{"k1", "k2"}) {
		t.Fatalf("ran %q", ran)
	}
}

func TestNoJournalLineHoldsAKeyOrAJobsErrorText(t *testing.T) {
	const marker = "zq7marker"
	dirs := testDirs(t)
	withJobs(t,
		Job{Kind: "sync", Priority: 1, Run: func(_ context.Context, _ []Mark, log io.Writer) Outcome {
			fmt.Fprintf(log, "error: %v\n", errors.New("open /home/someone/"+marker+": no such file"))
			return Failed
		}},
		Job{Kind: "tidy", Priority: 2, Run: func(context.Context, []Mark, io.Writer) Outcome {
			panic(errors.New("failed on " + marker))
		}},
		Job{Kind: "busy", Priority: 3, Run: func(context.Context, []Mark, io.Writer) Outcome { return Busy }},
	)
	var log strings.Builder
	marks := []Mark{
		{Kind: "sync", Key: "claude-code-" + marker, Reason: "stop", Host: "claude-code"},
		{Kind: "tidy", Key: marker, Reason: "tick"},
		{Kind: "busy", Key: "x" + marker + "x", Reason: "end", Host: "codex"},
	}
	if err := Wake(context.Background(), dirs, marks, Options{Log: &log}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(log.String(), marker) {
		t.Fatal("the job's error did not reach its log, so the test proves nothing")
	}
	if strings.Count(log.String(), marker) != 1 {
		t.Fatalf("the panic's text reached the log: %q", log.String())
	}
	text := journalText(t, dirs)
	if len(journal(t, dirs)) != 6 {
		t.Fatalf("journal %q", text)
	}
	if strings.Contains(strings.ToLower(text), strings.ToLower(marker)) {
		t.Fatalf("the journal holds the marker: %q", text)
	}
}
