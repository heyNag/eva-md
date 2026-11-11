package work

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestEachReasonHasThePlansWords(t *testing.T) {
	want := map[WaitReason]string{
		WaitRule:   "waiting for the size or quiet rule",
		WaitBehind: "behind a running job",
		WaitBudget: "deferred by the day's budget",
		WaitHeld:   "held after a failure",
		WaitSignIn: "waiting for sign-in",
		WaitOff:    "switched off, paused by the owner or excluded",
	}
	if len(Reasons) != len(want) {
		t.Fatalf("%d reasons, want %d", len(Reasons), len(want))
	}
	for _, r := range Reasons {
		if r.Words() != want[r] {
			t.Errorf("%q says %q, want %q", r, r.Words(), want[r])
		}
	}
	if WaitReason("other").Words() != "" {
		t.Error("an unknown reason has words")
	}
}

func TestStatusGivesEachReasonPastTheBar(t *testing.T) {
	now := time.Date(2026, 11, 11, 9, 0, 0, 0, time.UTC)
	ago := func(d time.Duration) time.Time { return now.Add(-d) }
	held := Waiting{Kind: "sync", Host: "codex", Since: now, Reason: WaitHeld, Class: "rate_limit", NextTry: now.Add(30 * time.Minute)}
	signIn := Waiting{Kind: "sync", Host: "claude-code", Since: ago(time.Second), Reason: WaitSignIn}
	kept := []Waiting{
		{Kind: "sync", Host: "codex", Since: ago(Bar + time.Millisecond), Reason: WaitRule},
		{Kind: "sync", Host: "codex", Since: ago(10 * time.Minute), Reason: WaitBehind},
		{Kind: "sync", Host: "codex", Since: ago(time.Hour), Reason: WaitBudget},
		{Kind: "sync", Host: "claude-code", Since: ago(6 * time.Minute), Reason: WaitOff},
		held, signIn,
	}
	dropped := []Waiting{
		{Kind: "sync", Host: "codex", Since: ago(Bar), Reason: WaitRule}, // exactly at the bar
		{Kind: "sync", Host: "codex", Since: ago(time.Minute), Reason: WaitBudget},
		{Kind: "sync", Host: "codex", Since: ago(4 * time.Minute), Reason: WaitOff},
		{Kind: "sync", Host: "codex", Since: now, Reason: WaitBehind},
	}
	var asked time.Time
	withJobs(t,
		Job{Kind: "sync", Run: done, Waiting: func(_ context.Context, at time.Time) []Waiting {
			asked = at
			return append(append([]Waiting{}, dropped...), kept...)
		}},
		Job{Kind: "tidy", Run: done},
	)
	dirs := testDirs(t)
	leaveMarksAt(t, dirs, ago(7*time.Minute), Mark{Kind: "tidy", Key: "daily", Reason: "tick", Host: "codex"})
	leaveMarksAt(t, dirs, ago(2*time.Minute), Mark{Kind: "tidy", Key: "push", Reason: "tick"})
	wake := filepath.Join(dirs.State, "work", wakeDir)
	// A mark whose file holds no time is measured from the file's own.
	if err := os.WriteFile(filepath.Join(wake, "tidy-old"), []byte("tick\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(filepath.Join(wake, "tidy-old"), ago(8*time.Minute), ago(8*time.Minute)); err != nil {
		t.Fatal(err)
	}
	// A mark of a kind no job registered is not reported.
	if err := os.WriteFile(filepath.Join(wake, "gone-x"), []byte("stop\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(filepath.Join(wake, "gone-x"), ago(time.Hour), ago(time.Hour)); err != nil {
		t.Fatal(err)
	}

	got := Status(context.Background(), dirs, now)
	if !asked.Equal(now) {
		t.Fatalf("the job was asked at %v, want %v", asked, now)
	}
	want := []Waiting{
		kept[2], // an hour
		kept[1], // ten minutes
		{Kind: "tidy", Since: ago(8 * time.Minute), Reason: WaitBehind},
		{Kind: "tidy", Host: "codex", Since: ago(7 * time.Minute), Reason: WaitBehind},
		kept[3], // six minutes
		kept[0], // just past the bar
		signIn,
		held,
	}
	for i := range got.Items {
		// A file's time is in the local zone.
		got.Items[i].Since = got.Items[i].Since.UTC()
	}
	if !reflect.DeepEqual(got.Items, want) {
		t.Fatalf("Status gave\n%+v\nwant\n%+v", got.Items, want)
	}
	seen := map[WaitReason]bool{}
	for _, w := range got.Items {
		seen[w.Reason] = true
	}
	for _, r := range Reasons {
		if !seen[r] {
			t.Errorf("no item %s", r)
		}
	}
}

// A chat that keeps going writes its mark again on each stop. The wait is
// measured from the oldest wake still waiting, not from the latest write,
// so work stuck behind a long job is reported.
func TestStatusMeasuresAMarkFromItsFirstWrite(t *testing.T) {
	t0 := time.Date(2026, 11, 11, 9, 0, 0, 0, time.UTC)
	withJobs(t, Job{Kind: "sync", Run: done})
	dirs := testDirs(t)
	m := Mark{Kind: "sync", Key: "k1", Reason: "stop", Host: "codex"}
	for _, d := range []time.Duration{0, 4 * time.Minute, 6 * time.Minute} {
		leaveMarksAt(t, dirs, t0.Add(d), m)
	}
	now := t0.Add(6*time.Minute + time.Second)
	got := Status(context.Background(), dirs, now).Items
	if len(got) != 1 || !got[0].Since.Equal(t0) || got[0].Reason != WaitBehind || got[0].Host != "codex" {
		t.Fatalf("Status gave %+v, want the mark since %v", got, t0)
	}

	// A mark written again while its run goes on was first written then:
	// the run took the older one.
	t1 := t0.Add(20 * time.Minute)
	calls := 0
	withJobs(t, Job{Kind: "sync", Run: func(_ context.Context, _ []Mark, _ io.Writer) Outcome {
		calls++
		if calls == 1 {
			writeMarkNow(t, dirs, m, t1)
			return Done
		}
		return Busy
	}})
	if err := Wake(context.Background(), dirs, nil, Options{Now: func() time.Time { return t1 }}); err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Fatalf("the job ran %d times, want 2", calls)
	}
	if got := Status(context.Background(), dirs, t1.Add(time.Minute)).Items; len(got) != 0 {
		t.Fatalf("Status a minute after the new mark gave %+v", got)
	}
	got = Status(context.Background(), dirs, t1.Add(6*time.Minute)).Items
	if len(got) != 1 || !got[0].Since.Equal(t1) {
		t.Fatalf("Status gave %+v, want the mark since %v", got, t1)
	}
}

func TestStatusCreatesNothing(t *testing.T) {
	withJobs(t, Job{Kind: "sync", Run: done})
	dirs := testDirs(t)
	if r := Status(context.Background(), dirs, time.Now()); len(r.Items) != 0 {
		t.Fatalf("Status of nothing gave %+v", r.Items)
	}
	if _, err := os.Stat(dirs.State); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("Status made the state folder: %v", err)
	}
}
