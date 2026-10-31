package hook

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/heyNag/eva-md/internal/inbox"
	"github.com/heyNag/eva-md/internal/session"
)

// A Claude Code chat a program runs, through print mode or the Agent SDK,
// holds prompts the program wrote. They are stored as host prompts, never as
// the user's, while replies and tool lines are kept as in any chat. The
// heartbeats keep the entrypoint, so the doctor shows it.
func TestHeadlessChatPromptsAreHostPrompts(t *testing.T) {
	for _, tc := range []struct {
		host, entrypoint string
		headless         bool
	}{
		{"claude-code", "sdk-cli", true},
		{"claude-code", "sdk-ts", true},
		{"claude-code", "sdk-py", true},
		// The terminal, the desktop app, its Claude Code chats with another
		// provider, its agent chats and the VS Code extension.
		{"claude-code", "cli", false},
		{"claude-code", "claude-desktop", false},
		{"claude-code", "claude-desktop-3p", false},
		{"claude-code", "local-agent", false},
		{"claude-code", "claude-vscode", false},
		{"claude-code", "", false},
		// Claude Code's variable says nothing about a Codex chat that inherits it.
		{"codex", "sdk-cli", false},
	} {
		t.Run(tc.host+"/"+tc.entrypoint, func(t *testing.T) {
			env, p := setup(t)
			env.slack = busySlack
			t.Setenv("CLAUDE_CODE_ENTRYPOINT", tc.entrypoint)
			p.Prompt = "Summarize the release checklist."
			invoke(t, tc.host, p, env)
			p.Event = "Stop"
			reply := "the final reply"
			p.Reply = &reply
			invoke(t, tc.host, p, env)
			p.Event = "PostToolUse"
			p.Tool = "Read"
			p.ToolInput = json.RawMessage(`{"file_path":"checklist.md"}`)
			invoke(t, tc.host, p, env)
			rows := records(t, env)
			prompts, hostPrompts := countRecords(rows, "user", p.Prompt), countRecords(rows, "event", "host_prompt")
			if tc.headless && (prompts != 0 || hostPrompts != 1) || !tc.headless && (prompts != 1 || hostPrompts != 0) {
				t.Fatalf("headless=%v stored %#v", tc.headless, rows)
			}
			if len(rows) != 3 || countRecords(rows, "assistant", reply) != 1 || !strings.HasPrefix(rows[2].Text, "Read checklist.md") {
				t.Fatalf("reply or tool line lost: %#v", rows)
			}
			want := tc.entrypoint
			if tc.host == "codex" {
				want = ""
			}
			for _, event := range []string{"UserPromptSubmit", "Stop", "PostToolUse"} {
				if b := beatOf(t, env, tc.host, event); b.Entrypoint != want || b.Headless != tc.headless {
					t.Fatalf("%s heartbeat=%+v", event, b)
				}
			}
		})
	}
}

// A record control in a headless chat is honoured, since that only keeps
// less.
func TestHeadlessChatHonoursRecordControls(t *testing.T) {
	env, p := setup(t)
	env.slack = busySlack
	t.Setenv("CLAUDE_CODE_ENTRYPOINT", "sdk-cli")
	p.Prompt = "off the record: PRIVATE PLAN"
	checkConfirmation(t, "claude-code", invoke(t, "claude-code", p, env), false)
	p.Event = "Stop"
	reply := "PRIVATE REPLY"
	p.Reply = &reply
	invoke(t, "claude-code", p, env)
	rows := records(t, env)
	if len(rows) != 1 || rows[0].Text != "off_record" {
		t.Fatalf("records=%#v", rows)
	}
	if _, off, err := (session.Markers{Root: env.Dirs.Sessions()}).Get("claude-code", p.Session, session.OffRecord); err != nil || !off {
		t.Fatalf("off=%v err=%v", off, err)
	}
}

// Recovery reads the same chat's transcript, where the program's prompt is
// an ordinary user row with a prompt id. It never stores that row as the
// user's, while it still recovers a reply the hooks missed.
func TestHeadlessChatTranscriptPromptsAreNotRecovered(t *testing.T) {
	env, p := setup(t)
	env.slack = busySlack
	t.Setenv("CLAUDE_CODE_ENTRYPOINT", "sdk-ts")
	file := registeredTranscript(t, env, &p)
	p.Event = "UserPromptSubmit"
	p.Prompt = "Summarize the release checklist."
	invoke(t, "claude-code", p, env)
	at := env.Now().Format(time.RFC3339)
	appendRows(t, file,
		map[string]any{"type": "user", "sessionId": p.Session, "cwd": p.Cwd, "timestamp": at, "promptId": p.Turn, "message": map[string]any{"role": "user", "content": p.Prompt}},
		map[string]any{"type": "assistant", "sessionId": p.Session, "cwd": p.Cwd, "timestamp": at, "promptId": p.Turn, "message": map[string]any{"role": "assistant", "stop_reason": "end_turn", "content": []any{map[string]any{"type": "text", "text": "a reply the hooks missed"}}}})
	p.Event = "SessionEnd"
	invoke(t, "claude-code", p, env)
	rows := records(t, env)
	if countRecords(rows, "user", p.Prompt) != 0 || countRecords(rows, "event", "host_prompt") != 1 || countRecords(rows, "assistant", "a reply the hooks missed") != 1 {
		t.Fatalf("records=%#v", rows)
	}
}

// recordedBeat is what tests read of an event's heartbeat.
type recordedBeat struct {
	Entrypoint string    `json:"entrypoint"`
	Headless   bool      `json:"headless"`
	Runs       []beatRun `json:"runs"`
}

func beatOf(t *testing.T, env Env, host, event string) recordedBeat {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(env.Dirs.State, "heartbeats", host, event+".json"))
	if err != nil {
		t.Fatal(err)
	}
	var beat recordedBeat
	if err := json.Unmarshal(data, &beat); err != nil {
		t.Fatal(err)
	}
	return beat
}

// beatRuns reads the run history of an event's heartbeat.
func beatRuns(t *testing.T, env Env, host, event string) []beatRun {
	t.Helper()
	return beatOf(t, env, host, event).Runs
}

// Each event's heartbeat keeps its last 20 runs, for the doctor's count of
// a host's failed runs.
func TestHeartbeatsKeepTheLastRuns(t *testing.T) {
	env, p := setup(t)
	env.slack = busySlack
	p.Event = "Stop"
	reply := "a reply"
	p.Reply = &reply
	for i := 0; i < maxRuns+5; i++ {
		invoke(t, "codex", p, env)
	}
	runs := beatRuns(t, env, "codex", "Stop")
	if len(runs) != maxRuns {
		t.Fatalf("kept %d runs", len(runs))
	}
	for _, r := range runs {
		if r.Failed || r.At != env.Now().Format(time.RFC3339Nano) {
			t.Fatalf("runs=%#v", runs)
		}
	}
}

// A run that an error stops before its event is stored is marked failed:
// here the off-record marker can't be read, then identity can't be resolved
// because git can't be found.
func TestRunsStoppedByAnErrorAreMarkedFailed(t *testing.T) {
	env, p := setup(t)
	env.slack = busySlack
	invoke(t, "codex", p, env)
	marker := filepath.Join(env.Dirs.Sessions(), "codex", p.Session, session.OffRecord)
	if err := os.Symlink(filepath.Join(os.Getenv("HOME"), "missing"), marker); err != nil {
		t.Fatal(err)
	}
	invoke(t, "codex", p, env)
	if runs := beatRuns(t, env, "codex", "UserPromptSubmit"); len(runs) != 2 || runs[0].Failed || !runs[1].Failed {
		t.Fatalf("runs=%#v", runs)
	}
	t.Setenv("PATH", t.TempDir())
	p.Cwd = filepath.Join(os.Getenv("HOME"), "elsewhere")
	if err := os.Mkdir(p.Cwd, 0o700); err != nil {
		t.Fatal(err)
	}
	p.Event = "PostToolUse"
	invoke(t, "codex", p, env)
	if runs := beatRuns(t, env, "codex", "PostToolUse"); len(runs) != 1 || !runs[0].Failed {
		t.Fatalf("runs=%#v", runs)
	}
	if rows := records(t, env); len(rows) != 1 {
		t.Fatalf("records=%#v", rows)
	}
}

// holdInbox locks the session's inbox file, so no capture can store an event
// until the returned release.
func holdInbox(t *testing.T, env Env, host, id string) func() {
	t.Helper()
	file := (inbox.Store{Root: env.Dirs.Inbox()}).Path("general", host, id)
	if err := os.MkdirAll(filepath.Dir(file), 0o700); err != nil {
		t.Fatal(err)
	}
	f, err := os.OpenFile(file, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		t.Fatal(err)
	}
	return func() { _ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN); f.Close() }
}

// A run whose capture runs out of time is marked failed too, by Run, once
// the output is written.
func TestRunOutOfTimeIsMarkedFailed(t *testing.T) {
	env, p := setup(t)
	p.Event = "Stop"
	reply := "held up"
	p.Reply = &reply
	release := holdInbox(t, env, "codex", p.Session)
	if out := invoke(t, "codex", p, env); out != "{}" {
		t.Fatal(out)
	}
	release()
	if runs := beatRuns(t, env, "codex", "Stop"); len(runs) != 1 || !runs[0].Failed {
		t.Fatalf("runs=%#v", runs)
	}
	// The next run waits for the abandoned capture's session lock, with time
	// to spare, and is not marked failed.
	env.slack = busySlack
	invoke(t, "codex", p, env)
	if runs := beatRuns(t, env, "codex", "Stop"); len(runs) != 2 || runs[1].Failed {
		t.Fatalf("runs=%#v", runs)
	}
}
