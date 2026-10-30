package hook

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/heyNag/eva-md/internal/inbox"
	"github.com/heyNag/eva-md/internal/paths"
	"github.com/heyNag/eva-md/internal/session"
)

// Tests for the hook side of transcript recovery: when recovery cannot
// finish, the hook still keeps the event's fixed name, as off-record mode
// does, so no event is lost without a trace.

// registeredTranscript gives the session a Claude transcript and registers
// it with a SessionStart, so recovery runs on every later event.
func registeredTranscript(t *testing.T, env Env, p *payload) string {
	t.Helper()
	file := filepath.Join(os.Getenv("HOME"), ".claude", "projects", "fixture", p.Session+".jsonl")
	if err := os.MkdirAll(filepath.Dir(file), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	p.Transcript = file
	p.Event, p.Source = "SessionStart", "startup"
	invoke(t, "claude-code", *p, env)
	p.Source = ""
	return file
}

func appendRows(t *testing.T, file string, rows ...map[string]any) {
	t.Helper()
	f, err := os.OpenFile(file, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	for _, row := range rows {
		data, err := json.Marshal(row)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.Write(append(data, '\n')); err != nil {
			t.Fatal(err)
		}
	}
}

func countRecords(rows []inbox.Entry, role, text string) int {
	n := 0
	for _, r := range rows {
		if r.Role == role && r.Text == text {
			n++
		}
	}
	return n
}

// A pass that has more than one window of transcript to read stops after
// one window, and one that runs out of time stops where it is. Either way
// the rows after the cursor may hold an off-the-record control a hook
// missed, so the event is kept only as an off-the-record chat would keep it:
// its fixed name, never its text, and no brief.
func TestDeferredRecoveryKeepsOnlyEventNames(t *testing.T) {
	env, p := setup(t)
	plant(t, env, "claude-code", p.Session)
	file := registeredTranscript(t, env, &p)
	// Rows recovery has no use for, each under the 1 MiB window and more
	// than two windows in all: each pass reads one and defers the rest.
	var filler []map[string]any
	for i := 0; i < 4; i++ {
		filler = append(filler, map[string]any{"type": "progress", "sessionId": p.Session, "data": strings.Repeat("x", 600<<10)})
	}
	appendRows(t, file, filler...)

	p.Event = "StopFailure"
	p.Error = json.RawMessage(`{"type":"rate_limit_error","message":"DO NOT SAVE THIS ERROR"}`)
	invoke(t, "claude-code", p, env)
	p.Event = "UserPromptSubmit"
	p.Prompt = "PRIVATE-WHILE-DEFERRED"
	if out := invoke(t, "claude-code", p, env); out != "" {
		t.Fatalf("deferred prompt returned %q", out)
	}
	p.Event, p.Source = "SessionStart", "compact"
	if out := invoke(t, "claude-code", p, env); out != "" {
		t.Fatalf("deferred SessionStart returned a brief: %q", out)
	}
	p.Source = ""

	rows := records(t, env)
	if countRecords(rows, "event", "stop_failure") != 1 || countRecords(rows, "event", "session_start") != 1 {
		t.Fatalf("event names lost while recovery was deferred: %#v", rows)
	}
	for _, r := range rows {
		if strings.Contains(r.Text, "PRIVATE") || strings.Contains(r.Text, "DO NOT SAVE") {
			t.Fatalf("content stored while recovery was deferred: %#v", r)
		}
	}

	// Once recovery reaches the end, capture goes on as before.
	p.Event = "UserPromptSubmit"
	p.Prompt = "public after catch-up"
	for i := 0; i < 8 && countRecords(records(t, env), "user", p.Prompt) == 0; i++ {
		p.Turn = "turn-public-" + string(rune('a'+i))
		invoke(t, "claude-code", p, env)
	}
	if n := countRecords(records(t, env), "user", p.Prompt); n != 1 {
		t.Fatalf("capture after catch-up stored %d copies", n)
	}
}

// A missed off-the-record control that recovery finds pauses the chat. The
// event that found it is kept as an off-the-record chat keeps any event.
func TestMissedOffRecordKeepsOnlyEventNames(t *testing.T) {
	env, p := setup(t)
	file := registeredTranscript(t, env, &p)
	appendRows(t, file, map[string]any{"type": "user", "sessionId": p.Session, "cwd": p.Cwd, "timestamp": env.Now().Format(time.RFC3339), "promptId": "turn-2", "message": map[string]any{"role": "user", "content": "off the record\nPRIVATE PLAN"}})
	p.Event = "PreCompact"
	invoke(t, "claude-code", p, env)
	_, off, err := (session.Markers{Root: env.Dirs.Sessions()}).Get("claude-code", p.Session, session.OffRecord)
	if err != nil || !off {
		t.Fatalf("missed control did not pause capture: off=%v err=%v", off, err)
	}
	rows := records(t, env)
	if countRecords(rows, "event", "compact") != 1 {
		t.Fatalf("event name lost: %#v", rows)
	}
	for _, r := range rows {
		if strings.Contains(r.Text, "PRIVATE") {
			t.Fatalf("private text stored: %#v", r)
		}
	}
}

// A pass that read to the end of the transcript has caught up, even if
// saving its cursor took it past its own deadline, into the grace recovery
// gets for that. The event is stored, not dropped.
func TestRecoveryFinishedLateStillCaptures(t *testing.T) {
	env, p := setup(t)
	env.recoverTranscript = func(ctx context.Context, _ paths.Dirs, _, _ string) (int, error) {
		<-ctx.Done()
		return 0, nil
	}
	invoke(t, "claude-code", p, env)
	p.Event = "PostToolUse"
	p.Tool = "Bash"
	p.ToolInput = json.RawMessage(`{"command":"go test ./..."}`)
	invoke(t, "claude-code", p, env)
	rows := records(t, env)
	if countRecords(rows, "user", "please help") != 1 || countRecords(rows, "tool", "Bash go test ./... (exit 0)") != 1 {
		t.Fatalf("records=%#v", rows)
	}
}
