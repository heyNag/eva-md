package catchup

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/heyNag/eva-md/internal/inbox"
	"github.com/heyNag/eva-md/internal/paths"
)

func fixture(t *testing.T, host string) (paths.Dirs, string, inbox.Entry) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(home, "gitconfig"))
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	dirs, err := paths.Default()
	if err != nil {
		t.Fatal(err)
	}
	id := "session-123"
	path := filepath.Join(home, ".claude", "projects", "project", id+".jsonl")
	if host == "codex" {
		path = filepath.Join(home, ".codex", "sessions", "2026", "10", "29", "rollout-test-"+id+".jsonl")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, nil, 0600); err != nil {
		t.Fatal(err)
	}
	cwd := filepath.Join(home, "project")
	if err := os.MkdirAll(cwd, 0700); err != nil {
		t.Fatal(err)
	}
	entry := inbox.Entry{V: 1, Host: host, Session: id, Project: "project", Branch: "main", Cwd: "project", Model: "test", TS: "2026-10-29T10:00:00Z"}
	if host == "codex" {
		appendJSON(t, path, map[string]any{"type": "session_meta", "payload": map[string]any{"id": id, "cwd": cwd}})
	}
	if err := Register(context.Background(), dirs, home, path, cwd, entry, false, time.Date(2026, 10, 29, 10, 0, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	return dirs, path, entry
}
func appendJSON(t *testing.T, path string, row any) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	data, err := json.Marshal(row)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.Write(append(data, '\n')); err != nil {
		t.Fatal(err)
	}
}
func claudeRow(e inbox.Entry, role, text, turn string) map[string]any {
	return map[string]any{"type": role, "sessionId": e.Session, "cwd": filepath.Join(os.Getenv("HOME"), e.Cwd), "timestamp": "2026-10-29T10:01:00Z", "promptId": turn, "message": map[string]any{"role": role, "content": []any{map[string]any{"type": "text", "text": text}}, "stop_reason": "end_turn"}}
}
func readInbox(t *testing.T, dirs paths.Dirs, e inbox.Entry) []inbox.Entry {
	t.Helper()
	store := inbox.Store{Root: dirs.Inbox()}
	rows, _, err := store.ReadFrom(store.Path(e.Project, e.Host, e.Session), 0)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	return rows
}

func TestRepairDeduplicatesAndKeepsFinalOnly(t *testing.T) {
	dirs, file, e := fixture(t, "claude-code")
	ctx := context.Background()
	prompt := e
	prompt.Role = "user"
	prompt.Kind = "prompt"
	prompt.Text = "choose blue"
	prompt.Turn = "turn1"
	if err := (inbox.Store{Root: dirs.Inbox()}).Append(prompt); err != nil {
		t.Fatal(err)
	}
	appendJSON(t, file, claudeRow(e, "user", "choose blue", "turn1"))
	comment := claudeRow(e, "assistant", "DO NOT SAVE NARRATION", "turn1")
	comment["message"].(map[string]any)["stop_reason"] = "tool_use"
	appendJSON(t, file, comment)
	appendJSON(t, file, claudeRow(e, "assistant", "Blue is pending approval.", "turn1"))
	meta := claudeRow(e, "user", "DO NOT SAVE META", "turn2")
	meta["isMeta"] = true
	appendJSON(t, file, meta)
	side := claudeRow(e, "user", "DO NOT SAVE SIDECHAIN", "turn2")
	side["isSidechain"] = true
	appendJSON(t, file, side)
	wrong := claudeRow(e, "user", "DO NOT SAVE WRONGSESSION", "turn2")
	wrong["sessionId"] = "other"
	appendJSON(t, file, wrong)
	n, err := Recover(ctx, dirs, e.Host, e.Session)
	if err != nil || n != 1 {
		t.Fatalf("recover=%d %v", n, err)
	}
	n, err = Recover(ctx, dirs, e.Host, e.Session)
	if err != nil || n != 0 {
		t.Fatalf("repeat=%d %v", n, err)
	}
	rows := readInbox(t, dirs, e)
	if len(rows) != 2 || rows[1].Origin != "catchup" || rows[1].Text != "Blue is pending approval." {
		t.Fatalf("rows=%#v", rows)
	}
	// Repeating the same words in a genuinely new turn is not duplication.
	appendJSON(t, file, claudeRow(e, "user", "choose blue", "turn3"))
	n, err = Recover(ctx, dirs, e.Host, e.Session)
	if err != nil || n != 1 {
		t.Fatalf("new identical turn=%d %v", n, err)
	}
}

func TestClaudeCompactionSummariesAreNotUserPrompts(t *testing.T) {
	dirs, file, e := fixture(t, "claude-code")
	appendJSON(t, file, claudeRow(e, "user", "Keep the original user decision.", "real1"))
	for _, flags := range []map[string]any{
		{"isCompactSummary": true, "isVisibleInTranscriptOnly": true},
		{"isCompactSummary": true}, // Partial compaction need not be transcript-only.
		{"repeatsCompactSummary": true},
	} {
		row := claudeRow(e, "user", "HOST SUMMARY MUST NOT BECOME USER WORDS", "summary-turn")
		for key, value := range flags {
			row[key] = value
		}
		appendJSON(t, file, row)
	}
	visible := claudeRow(e, "user", "Visibility is not authorship metadata.", "real2")
	visible["isVisibleInTranscriptOnly"] = true
	appendJSON(t, file, visible)
	ordinary := claudeRow(e, "user", "The isCompactSummary field should be false here.", "real3")
	ordinary["isCompactSummary"] = false
	ordinary["repeatsCompactSummary"] = false
	appendJSON(t, file, ordinary)
	// A host summary quoting a privacy phrase must neither become a real
	// privacy command nor replace the last genuine prompt's turn identity.
	quoted := claudeRow(e, "user", "off the record\nHOST QUOTED CONTROL", "summary-control")
	quoted["isCompactSummary"] = true
	appendJSON(t, file, quoted)
	final := claudeRow(e, "assistant", "The original decision still stands.", "")
	delete(final, "promptId")
	appendJSON(t, file, final)
	n, err := Recover(context.Background(), dirs, e.Host, e.Session)
	if err != nil || n != 4 {
		t.Fatalf("recover=%d %v", n, err)
	}
	rows := readInbox(t, dirs, e)
	if len(rows) != 4 || rows[3].Role != "assistant" || rows[3].Turn != "real3" {
		t.Fatalf("rows=%#v", rows)
	}
	for _, row := range rows {
		if strings.Contains(row.Text, "HOST") {
			t.Fatal("host compaction content was attributed to a user")
		}
	}
	if n, err := Recover(context.Background(), dirs, e.Host, e.Session); err != nil || n != 0 {
		t.Fatalf("repeated recovery=%d %v", n, err)
	}
}

func TestClaudeSyntheticAndAPIErrorRowsAreNotReplies(t *testing.T) {
	dirs, file, e := fixture(t, "claude-code")
	appendJSON(t, file, claudeRow(e, "user", "Explain the placeholder text.", "real-turn"))
	placeholder := claudeRow(e, "assistant", "No response requested.", "interrupted-turn")
	placeholder["message"].(map[string]any)["model"] = "<synthetic>"
	placeholder["message"].(map[string]any)["stop_reason"] = "stop_sequence"
	appendJSON(t, file, placeholder)
	errorRow := claudeRow(e, "assistant", "HOST API ERROR MUST NOT BE A MODEL REPLY", "error-turn")
	errorRow["isApiErrorMessage"] = true
	appendJSON(t, file, errorRow)
	// Those same words are legitimate when a real model produces them. Neither
	// a missing model name nor an explicitly false error flag rejects a reply.
	real := claudeRow(e, "assistant", "No response requested.", "real-turn")
	real["message"].(map[string]any)["model"] = "claude-test"
	real["message"].(map[string]any)["stop_reason"] = "stop_sequence"
	real["isApiErrorMessage"] = false
	appendJSON(t, file, real)
	appendJSON(t, file, claudeRow(e, "assistant", "A final answer without a model field.", "next-turn"))
	n, err := Recover(context.Background(), dirs, e.Host, e.Session)
	if err != nil || n != 3 {
		t.Fatalf("recover=%d %v", n, err)
	}
	rows := readInbox(t, dirs, e)
	if len(rows) != 3 || rows[1].Text != "No response requested." || rows[1].Turn != "real-turn" || rows[2].Kind != "reply" {
		t.Fatalf("rows=%#v", rows)
	}
}
func TestOffRecordCannotBeRecoveredAfterResume(t *testing.T) {
	dirs, file, e := fixture(t, "claude-code")
	ctx := context.Background()
	appendJSON(t, file, claudeRow(e, "user", "off the record", "turn1"))
	appendJSON(t, file, claudeRow(e, "assistant", "PRIVATE MISSED CONTROL", "turn1"))
	if _, err := Recover(ctx, dirs, e.Host, e.Session); err != nil {
		t.Fatal(err)
	}
	appendJSON(t, file, claudeRow(e, "user", "PRIVATE AFTER RECOVERY", "turn2"))
	if n, err := Recover(ctx, dirs, e.Host, e.Session); err != nil || n != 0 {
		t.Fatalf("blocked=%d %v", n, err)
	}
	if err := SetOffRecord(ctx, dirs, e.Host, e.Session, false); err != nil {
		t.Fatal(err)
	}
	appendJSON(t, file, claudeRow(e, "user", "public again", "turn3"))
	if n, err := Recover(ctx, dirs, e.Host, e.Session); err != nil || n != 1 {
		t.Fatalf("public=%d %v", n, err)
	}
	rows := readInbox(t, dirs, e)
	if len(rows) != 1 || rows[0].Text != "public again" {
		t.Fatalf("privacy leak: %#v", rows)
	}
}
func TestCodexFinalPhaseAndIdentity(t *testing.T) {
	dirs, file, e := fixture(t, "codex")
	ctx := context.Background()
	emit := func(payload any) {
		appendJSON(t, file, map[string]any{"type": "event_msg", "timestamp": "2026-10-29T10:01:00Z", "payload": payload})
	}
	emit(map[string]any{"type": "task_started", "turn_id": "turn1"})
	emit(map[string]any{"type": "user_message", "message": "decided on blue"})
	emit(map[string]any{"type": "agent_message", "phase": "commentary", "message": "PRIVATE NARRATION"})
	emit(map[string]any{"type": "agent_message", "phase": "final", "message": "blue remains pending"})
	emit(map[string]any{"type": "task_complete", "turn_id": "turn1", "last_agent_message": "blue remains pending"})
	n, err := Recover(ctx, dirs, e.Host, e.Session)
	if err != nil || n != 2 {
		t.Fatalf("recover=%d %v", n, err)
	}
	rows := readInbox(t, dirs, e)
	if len(rows) != 2 || rows[0].Role != "user" || rows[1].Role != "assistant" {
		t.Fatalf("rows=%#v", rows)
	}
	for _, r := range rows {
		if strings.Contains(r.Text, "PRIVATE") {
			t.Fatal("commentary captured")
		}
	}
}
func TestNoBackfillAndPartialTail(t *testing.T) {
	dirs, file, e := fixture(t, "claude-code")
	ctx := context.Background()
	appendJSON(t, file, claudeRow(e, "user", "new fact", "turn1"))
	f, _ := os.OpenFile(file, os.O_APPEND|os.O_WRONLY, 0600)
	f.WriteString(`{"type":`)
	f.Close()
	n, err := Recover(ctx, dirs, e.Host, e.Session)
	if err != nil || n != 1 {
		t.Fatalf("partial=%d %v", n, err)
	}
	s, _, _ := load(ctx, dirs, e.Host, e.Session)
	info, _ := os.Stat(file)
	if s.Offset >= info.Size() {
		t.Fatal("partial line consumed")
	}
	n, err = Recover(ctx, dirs, e.Host, e.Session)
	if err != nil || n != 0 {
		t.Fatalf("repeat=%d %v", n, err)
	}
}
func TestTranscriptPathAndReplacementRefused(t *testing.T) {
	dirs, file, e := fixture(t, "claude-code")
	ctx := context.Background()
	s, _, _ := load(ctx, dirs, e.Host, e.Session)
	s.Path = filepath.Join(os.Getenv("HOME"), "secrets.jsonl")
	if _, err := openTranscript(s); err == nil {
		t.Fatal("outside path accepted")
	}
	replacement := file + ".new"
	os.WriteFile(replacement, []byte("{}\n"), 0600)
	os.Rename(replacement, file)
	if _, err := Recover(ctx, dirs, e.Host, e.Session); err == nil {
		t.Fatal("replacement accepted")
	}
}

func TestIndentedPrivacyControlMissedByHook(t *testing.T) {
	dirs, file, e := fixture(t, "claude-code")
	ctx := context.Background()
	appendJSON(t, file, claudeRow(e, "user", "One note first.\n  OFF THE RECORD!  \nPrivate subject follows.", "turn1"))
	appendJSON(t, file, claudeRow(e, "assistant", "PRIVATE FOLLOWUP", "turn1"))
	n, err := Recover(ctx, dirs, e.Host, e.Session)
	if err != nil || n != 0 {
		t.Fatalf("privacy recovery=%d %v", n, err)
	}
	s, _, err := load(ctx, dirs, e.Host, e.Session)
	if err != nil || !s.Blocked {
		t.Fatal("privacy boundary not persisted")
	}
	if rows := readInbox(t, dirs, e); len(rows) != 0 {
		t.Fatal("private rows captured")
	}
}

func TestLargeBacklogDoesNotSkipPrivacyBoundary(t *testing.T) {
	dirs, file, e := fixture(t, "claude-code")
	ctx := context.Background()
	appendJSON(t, file, claudeRow(e, "user", "off the record", "turn1"))
	for i := 0; i < 1100; i++ {
		appendJSON(t, file, map[string]any{"type": "metadata", "padding": strings.Repeat("x", 1024)})
	}
	appendJSON(t, file, claudeRow(e, "assistant", "PRIVATE NEWEST TAIL", "turn1"))
	for i := 0; i < 3; i++ {
		if n, err := Recover(ctx, dirs, e.Host, e.Session); err != nil || n != 0 {
			t.Fatalf("pass%d=%d %v", i, n, err)
		}
	}
	if len(readInbox(t, dirs, e)) != 0 {
		t.Fatal("private tail recovered")
	}
}
func TestRepeatedWordsWithoutTurnIDAndRetry(t *testing.T) {
	dirs, file, e := fixture(t, "claude-code")
	ctx := context.Background()
	// Replies: a user row without a prompt id is never a prompt (see
	// TestHostRowsAreNeverUserPrompts), but a final reply may lack a turn.
	for _, at := range []string{"2026-10-29T10:01:00Z", "2026-10-29T10:03:00Z"} {
		row := claudeRow(e, "assistant", "yes", "")
		delete(row, "promptId")
		row["timestamp"] = at
		appendJSON(t, file, row)
	}
	n, err := Recover(ctx, dirs, e.Host, e.Session)
	if err != nil || n != 2 {
		t.Fatalf("repeated=%d %v", n, err)
	}
	s, _, _ := load(ctx, dirs, e.Host, e.Session)
	s.Offset = 0
	if err := save(ctx, dirs, s); err != nil {
		t.Fatal(err)
	}
	n, err = Recover(ctx, dirs, e.Host, e.Session)
	if err != nil || n != 0 {
		t.Fatalf("retry=%d %v", n, err)
	}
}

func TestLaterChunkPrivacyStopsCurrentCaptureUntilRecovered(t *testing.T) {
	dirs, file, e := fixture(t, "claude-code")
	ctx := context.Background()
	for i := 0; i < 1100; i++ {
		appendJSON(t, file, map[string]any{"type": "metadata", "padding": strings.Repeat("x", 1024)})
	}
	appendJSON(t, file, claudeRow(e, "user", "off the record", "turn1"))
	appendJSON(t, file, claudeRow(e, "assistant", "PRIVATE LAST CHUNK", "turn1"))
	if _, err := Recover(ctx, dirs, e.Host, e.Session); !errors.Is(err, ErrPending) {
		t.Fatalf("expected deferred capture: %v", err)
	}
	if n, err := Recover(ctx, dirs, e.Host, e.Session); err != nil || n != 0 {
		t.Fatalf("second chunk=%d %v", n, err)
	}
	if len(readInbox(t, dirs, e)) != 0 {
		t.Fatal("private rows recovered")
	}
}
