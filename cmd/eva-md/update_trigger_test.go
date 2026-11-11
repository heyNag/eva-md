package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/heyNag/eva-md/internal/health"
	"github.com/heyNag/eva-md/internal/update"
)

// triggerMarker is a made-up marker, as a hook of a later release might
// pass for a trigger this one doesn't know.
const triggerMarker = "zq-planted-marker-7f3k"

// eva-md update records the trigger --trigger names on each record of its
// job: each name the hooks pass, a name it doesn't know as other, none
// without the flag, as from a hook of 2026.11.7 or earlier, whose command
// it still takes, and manual for a person's --foreground run. The job's
// clock is pinned, so its records and the reading of them share a day
// however long the test runs.
func TestUpdateRecordsItsTrigger(t *testing.T) {
	d, fake := updateFixture(t, tangerine)
	at := time.Now()
	saved := updateClock
	updateClock = func() time.Time { return at }
	t.Cleanup(func() { updateClock = saved })
	// Each pass's answer keeps the page's items and adds one of its own.
	ids := regexp.MustCompile(`(?m)^- (n[0-9]+) (next|question|decision) `)
	fake.Respond = func(r update.Request) (update.Result, error) {
		var keeps string
		for _, m := range ids.FindAllStringSubmatch(string(r.Input), -1) {
			keeps += `{"id":"` + m[1] + `","kind":"` + m[2] + `","action":"keep","text":null,"tag":"stated","refs":[],"reason":null,"replacement":null},`
		}
		n := strconv.Itoa(len(fake.Requests()))
		answer := strings.Replace(strings.ReplaceAll(tangerine, "tangerine", "tangerine "+n), `"items":[`, `"items":[`+keeps, 1)
		return update.Result{Output: []byte(answer), Attempts: 1}, nil
	}
	cwd := project(t)
	for i, tc := range []struct {
		flags []string
		want  string
	}{
		{[]string{"--trigger", "session_end"}, update.TriggerEnd},
		{[]string{"--trigger", "compact"}, update.TriggerCompact},
		{[]string{"--trigger", "stop_size"}, update.TriggerStop},
		{[]string{"--trigger", "prompt_gap"}, update.TriggerGap},
		{[]string{"--trigger=chat_start"}, update.TriggerStart},
		{[]string{"--trigger", triggerMarker}, update.TriggerOther},
		{nil, ""},
		{[]string{"--foreground"}, update.TriggerManual},
		{[]string{"--foreground", "--trigger", "compact"}, update.TriggerCompact},
	} {
		sid := "trigger-session-" + string(rune('a'+i))
		hookTurn(t, cwd, sid, "Plan the tangerine migration with the switch kept behind a flag until the dry run looks right. "+strings.Repeat("Context. ", 40),
			"Planned the tangerine migration; the flag stays off until the dry run passes. "+strings.Repeat("Detail. ", 40))
		var out, errOut bytes.Buffer
		args := append([]string{"update", "--session", "claude-code:" + sid}, tc.flags...)
		if code := run(context.Background(), args, strings.NewReader(""), &out, &errOut); code != 0 || out.String() != "update: 1 pass: 1 ok\n" {
			t.Fatalf("%q: %d %q %q", args, code, out.String(), errOut.String())
		}
		records, err := update.Days(d.State, at, 1)
		if err != nil || len(records) == 0 {
			t.Fatalf("%q: run log %v", args, err)
		}
		last := records[len(records)-1]
		if last.Session != sid || last.Trigger != tc.want {
			t.Fatalf("%q: recorded %q for %s, want %q", args, last.Trigger, last.Session, tc.want)
		}
	}
	data, err := os.ReadFile(filepath.Join(d.State, "updates", at.Format(time.DateOnly)+".jsonl"))
	if err != nil || strings.Contains(string(data), triggerMarker) {
		t.Fatalf("the run log holds the flag's text: %v", err)
	}
}

// eva-md status --updates prints the run log's last 7 days in counts, and
// with --json the same figures; it shows no session id or other text of
// the log. Only status takes the flag.
func TestStatusUpdates(t *testing.T) {
	d := isolated(t)
	now := time.Now()
	if err := os.MkdirAll(filepath.Join(d.State, "updates"), 0o700); err != nil {
		t.Fatal(err)
	}
	// A pass in the day's run log, and a busy exit in its file of them.
	for name, r := range map[string]update.Record{
		now.Format(time.DateOnly) + ".jsonl": {At: now.Format(time.RFC3339), Host: update.HostClaude, Session: "3f2a91c0-" + triggerMarker, Project: triggerMarker, Thread: triggerMarker, Outcome: update.OutcomeOK,
			Calls: 1, TokensIn: 1200, TokensOut: 90, DurationMS: 42000, Pages: 1, BoundaryToSavedMS: 300000, Trigger: update.TriggerEnd, Model: triggerMarker},
		"busy-" + now.Format(time.DateOnly) + ".jsonl": {At: now.Format(time.RFC3339), Outcome: update.OutcomeBusy, Trigger: update.TriggerStart, SessionHashes: []string{"0123456789abcdef"}},
	} {
		data, _ := json.Marshal(r)
		if err := os.WriteFile(filepath.Join(d.State, "updates", name), append(data, '\n'), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	var out, errOut bytes.Buffer
	if code := run(context.Background(), []string{"status", "--updates"}, strings.NewReader(""), &out, &errOut); code != 0 || errOut.Len() != 0 {
		t.Fatalf("status --updates: %d %q", code, errOut.String())
	}
	text := out.String()
	for _, line := range []string{
		"outcomes: 1 ok\n",
		"time per call: median 42.0 s, p95 42.0 s, max 42.0 s (1)\n",
		"chat boundary to memory on file, by trigger: session_end median 5.0 min, p95 5.0 min, max 5.0 min (1)\n",
		"busy exits: 1, for 1 session; by trigger: 1 chat_start\n",
	} {
		if !strings.Contains(text, line) {
			t.Errorf("status --updates lacks %q:\n%s", line, text)
		}
	}
	if strings.Contains(text, triggerMarker) || strings.Contains(text, "3f2a91c0") || strings.Count(text, "\n") != 14 {
		t.Fatalf("status --updates:\n%s", text)
	}
	out.Reset()
	if code := run(context.Background(), []string{"status", "--updates", "--json"}, strings.NewReader(""), &out, &errOut); code != 0 {
		t.Fatalf("status --updates --json: %d %q", code, errOut.String())
	}
	var f health.UpdateFigures
	if err := json.Unmarshal(out.Bytes(), &f); err != nil || f.Passes != 1 || f.Busy.Jobs != 1 || f.TokensIn.Max != 1200 || strings.Contains(out.String(), triggerMarker) {
		t.Fatalf("status --updates --json: %v %s", err, out.String())
	}
	for _, args := range [][]string{{"doctor", "--updates"}, {"status", "--updates", "--live"}} {
		out.Reset()
		errOut.Reset()
		if code := run(context.Background(), args, strings.NewReader(""), &out, &errOut); code != 2 || out.Len() != 0 {
			t.Errorf("%q: %d %q %q", args, code, out.String(), errOut.String())
		}
	}
}
