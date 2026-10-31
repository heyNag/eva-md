package health

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/heyNag/eva-md/internal/catchup"
	"github.com/heyNag/eva-md/internal/config"
	"github.com/heyNag/eva-md/internal/paths"
)

// putRuns writes an event's heartbeat as the hook writes it, with the run
// history given, oldest first.
func putRuns(t *testing.T, d paths.Dirs, host, event string, runs []hookRun) {
	t.Helper()
	type run struct {
		At     string `json:"at"`
		Failed bool   `json:"failed,omitempty"`
	}
	var history []run
	for _, r := range runs {
		history = append(history, run{r.At.Format(time.RFC3339Nano), r.Failed})
	}
	raw, _ := json.Marshal(map[string]any{"v": 1, "at": history[len(history)-1].At, "host": host, "event": event, "transcript_available": true, "runs": history})
	put(t, filepath.Join(d.State, "heartbeats", host, event+".json"), raw)
}

// series is n runs a minute apart, ending at end; every one failed if
// failed is set.
func series(end time.Time, n int, failed bool) []hookRun {
	var runs []hookRun
	for i := n - 1; i >= 0; i-- {
		runs = append(runs, hookRun{At: end.Add(-time.Duration(i) * time.Minute), Failed: failed})
	}
	return runs
}

// Of a host's last 20 hook runs in the past week, newest first whichever
// events they were, half or more failing is a doctor FAIL and a notice
// line, from 4 runs on; fewer runs with a failure are too few to judge.
func TestHookFailures(t *testing.T) {
	now := time.Now()
	for _, tc := range []struct {
		name   string
		plant  func(t *testing.T, d paths.Dirs)
		status string
	}{
		{"no runs", func(*testing.T, paths.Dirs) {}, "UNKNOWN"},
		{"10 of the last 20 across events", func(t *testing.T, d paths.Dirs) {
			putRuns(t, d, "codex", "Stop", series(now.Add(-time.Minute), 10, true))
			putRuns(t, d, "codex", "PostToolUse", append(series(now.Add(-time.Hour), 5, true), series(now, 10, false)...))
		}, "FAIL"},
		{"9 of the last 20", func(t *testing.T, d paths.Dirs) {
			putRuns(t, d, "codex", "Stop", append(series(now.Add(-time.Hour), 9, true), series(now, 11, false)...))
		}, "PASS"},
		{"failures older than the last 20", func(t *testing.T, d paths.Dirs) {
			putRuns(t, d, "codex", "Stop", series(now.Add(-time.Hour), 20, true))
			putRuns(t, d, "codex", "UserPromptSubmit", series(now, 20, false))
		}, "PASS"},
		{"failures older than a week", func(t *testing.T, d paths.Dirs) {
			putRuns(t, d, "codex", "Stop", series(now.Add(-8*24*time.Hour), 20, true))
		}, "UNKNOWN"},
		{"all 8 runs", func(t *testing.T, d paths.Dirs) {
			putRuns(t, d, "codex", "Stop", series(now, 8, true))
		}, "FAIL"},
		{"2 of 4 runs", func(t *testing.T, d paths.Dirs) {
			putRuns(t, d, "codex", "Stop", append(series(now.Add(-time.Hour), 2, true), series(now, 2, false)...))
		}, "FAIL"},
		{"3 of 7 runs", func(t *testing.T, d paths.Dirs) {
			putRuns(t, d, "codex", "Stop", append(series(now.Add(-time.Hour), 3, true), series(now, 4, false)...))
		}, "PASS"},
		{"all 3 runs, too few to judge", func(t *testing.T, d paths.Dirs) {
			putRuns(t, d, "codex", "Stop", series(now, 3, true))
		}, "UNKNOWN"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := isolated(t)
			tc.plant(t, d)
			if c := hookCheck(d, "codex", now); c.Status != tc.status || c.Name != "codex hook failures" || (c.Status == "FAIL") != (c.Fix != "") {
				t.Fatalf("check=%+v", c)
			}
			notice := QuickNotice(context.Background(), d, config.Default(), "claude-code")
			if want := tc.status == "FAIL"; strings.Contains(notice, "codex hook runs failed") != want {
				t.Fatalf("notice=%q", notice)
			}
			if c := hookCheck(d, "claude-code", now); c.Status != "UNKNOWN" {
				t.Fatalf("other host=%+v", c)
			}
		})
	}
}

// The doctor reports the drift signal transcript recovery records for a
// host, and the start-of-chat notice shows it in one line.
func TestTranscriptDrift(t *testing.T) {
	d := isolated(t)
	if c := driftCheck(d, "claude-code"); c.Status != "PASS" {
		t.Fatalf("check=%+v", c)
	}
	raw, _ := json.Marshal(catchup.Drift{V: 1, Host: "claude-code", Session: "0f1e2d3c-aaaa-4bbb-8ccc-123456789abc", At: "2026-10-31T09:30:00+07:00", Bytes: 48 << 10, Turns: 3})
	file := filepath.Join(d.State, "catchup", "drift-claude-code.json")
	put(t, file, raw)
	c := driftCheck(d, "claude-code")
	if c.Status != "FAIL" || !strings.Contains(c.Detail, "claude-code:0f1e2d3c") || !strings.Contains(c.Detail, "48 KB") || c.Fix == "" {
		t.Fatalf("check=%+v", c)
	}
	if c := driftCheck(d, "codex"); c.Status != "PASS" {
		t.Fatalf("other host=%+v", c)
	}
	if notice := QuickNotice(context.Background(), d, config.Default(), "codex"); !strings.Contains(notice, "claude-code's transcripts") {
		t.Fatalf("notice=%q", notice)
	}
	put(t, file, []byte("not a record"))
	if c := driftCheck(d, "claude-code"); c.Status != "UNKNOWN" {
		t.Fatalf("damaged record=%+v", c)
	}
}

// An event's row shows the Claude Code entrypoint its last run inherited,
// and says when the hook took it for a program's chat.
func TestInspectShowsTheEntrypoint(t *testing.T) {
	d := isolated(t)
	at := time.Now().Format(time.RFC3339Nano)
	for event, beat := range map[string]map[string]any{
		"UserPromptSubmit": {"entrypoint": "claude-desktop"},
		"Stop":             {"entrypoint": "sdk-cli", "headless": true},
		"PostToolUse":      {},
	} {
		beat["v"], beat["at"], beat["host"], beat["event"], beat["transcript_available"] = 1, at, "claude-code", event, true
		raw, _ := json.Marshal(beat)
		put(t, filepath.Join(d.State, "heartbeats", "claude-code", event+".json"), raw)
	}
	details := map[string]string{}
	for _, c := range Inspect(context.Background(), d, "v0.7.2", false).Checks {
		details[c.Name] = c.Detail
	}
	if got := details["claude-code UserPromptSubmit"]; !strings.HasSuffix(got, "; entrypoint claude-desktop") {
		t.Fatalf("interactive=%q", got)
	}
	if got := details["claude-code Stop"]; !strings.HasSuffix(got, "; entrypoint sdk-cli, a program's chat: its prompts are stored as host prompts") {
		t.Fatalf("headless=%q", got)
	}
	if got := details["claude-code PostToolUse"]; strings.Contains(got, "entrypoint") {
		t.Fatalf("none recorded=%q", got)
	}
}

// The doctor has both rows for each host.
func TestInspectReportsHookFailuresAndDrift(t *testing.T) {
	d := isolated(t)
	names := map[string]bool{}
	for _, c := range Inspect(context.Background(), d, "v0.7.2", false).Checks {
		names[c.Name] = true
	}
	for _, host := range []string{"claude-code", "codex"} {
		if !names[host+" hook failures"] || !names[host+" transcript format"] {
			t.Fatalf("checks=%v", names)
		}
	}
}
