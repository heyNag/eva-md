package catchup

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// Tests for when recovery starts again at the transcript's end, and for a
// Codex turn in another folder, which recovery passes over without ever
// stopping for good.

// codexRow appends a Codex row of the given type and payload.
func codexRow(t *testing.T, file, kind string, payload map[string]any) {
	t.Helper()
	appendJSON(t, file, map[string]any{"type": kind, "timestamp": "2026-10-29T10:01:00Z", "payload": payload})
}

// A Codex turn in another folder is not stored, but recovery reads on: a
// turn back in the registered folder is stored again, and an off-the-record
// control the hooks missed is honoured, in the same pass or a later one.
func TestCodexTurnInAnotherFolderDoesNotStopRecovery(t *testing.T) {
	ctx := context.Background()
	t.Run("later pass", func(t *testing.T) {
		dirs, file, e := fixture(t, "codex")
		home := os.Getenv("HOME")
		cwd := filepath.Join(home, "project")
		codexRow(t, file, "turn_context", map[string]any{"cwd": filepath.Join(home, "other"), "turn_id": "t1"})
		codexRow(t, file, "event_msg", map[string]any{"type": "user_message", "message": "a prompt in another folder"})
		if n, err := Recover(ctx, dirs, e.Host, e.Session); err != nil || n != 0 {
			t.Fatalf("other folder=%d %v", n, err)
		}
		// The hooks register the chat again in its own folder, a minute on.
		if err := Register(ctx, dirs, home, file, cwd, e, false, false, time.Date(2026, 10, 29, 10, 1, 0, 0, time.UTC)); err != nil {
			t.Fatal(err)
		}
		codexRow(t, file, "turn_context", map[string]any{"cwd": cwd, "turn_id": "t2"})
		codexRow(t, file, "event_msg", map[string]any{"type": "user_message", "message": "a prompt back home"})
		if n, err := Recover(ctx, dirs, e.Host, e.Session); err != nil || n != 1 {
			t.Fatalf("back home=%d %v", n, err)
		}
		codexRow(t, file, "event_msg", map[string]any{"type": "user_message", "message": "off the record"})
		codexRow(t, file, "event_msg", map[string]any{"type": "user_message", "message": "PRIVATE plan"})
		if n, err := Recover(ctx, dirs, e.Host, e.Session); err != nil || n != 0 || !offMarker(t, dirs, e) {
			t.Fatalf("missed control=%d %v", n, err)
		}
		rows := readInbox(t, dirs, e)
		if len(rows) != 1 || rows[0].Text != "a prompt back home" {
			t.Fatalf("%d rows stored", len(rows))
		}
	})
	t.Run("later pass, still in the other folder", func(t *testing.T) {
		// A pass ends inside the other folder's turn, and the turn goes on: the
		// next pass still stores none of it, and honours the control in it.
		dirs, file, e := fixture(t, "codex")
		home := os.Getenv("HOME")
		codexRow(t, file, "turn_context", map[string]any{"cwd": filepath.Join(home, "other"), "turn_id": "t1"})
		codexRow(t, file, "event_msg", map[string]any{"type": "user_message", "message": "a prompt in another folder"})
		if n, err := Recover(ctx, dirs, e.Host, e.Session); err != nil || n != 0 || offMarker(t, dirs, e) {
			t.Fatalf("other folder=%d %v", n, err)
		}
		codexRow(t, file, "event_msg", map[string]any{"type": "user_message", "message": "another prompt in that folder"})
		codexRow(t, file, "event_msg", map[string]any{"type": "user_message", "message": "off the record"})
		codexRow(t, file, "event_msg", map[string]any{"type": "user_message", "message": "PRIVATE plan"})
		if n, err := Recover(ctx, dirs, e.Host, e.Session); err != nil || n != 0 || !offMarker(t, dirs, e) {
			t.Fatalf("control later in the other folder's turn=%d %v", n, err)
		}
		if rows := readInbox(t, dirs, e); len(rows) != 0 {
			t.Fatalf("%d rows stored", len(rows))
		}
	})
	t.Run("same pass", func(t *testing.T) {
		dirs, file, e := fixture(t, "codex")
		home := os.Getenv("HOME")
		codexRow(t, file, "turn_context", map[string]any{"cwd": filepath.Join(home, "other"), "turn_id": "t1"})
		codexRow(t, file, "event_msg", map[string]any{"type": "user_message", "message": "off the record"})
		codexRow(t, file, "turn_context", map[string]any{"cwd": filepath.Join(home, "project"), "turn_id": "t2"})
		codexRow(t, file, "event_msg", map[string]any{"type": "user_message", "message": "PRIVATE plan"})
		if n, err := Recover(ctx, dirs, e.Host, e.Session); err != nil || n != 0 || !offMarker(t, dirs, e) {
			t.Fatalf("control in another folder=%d %v", n, err)
		}
		if rows := readInbox(t, dirs, e); len(rows) != 0 {
			t.Fatalf("%d rows stored", len(rows))
		}
	})
	t.Run("old block lifted", func(t *testing.T) {
		// An older version blocked the chat for good, with no reason, at a
		// turn in another folder. The next registration lifts that block
		// and starts again at the transcript's end.
		dirs, file, e := fixture(t, "codex")
		home := os.Getenv("HOME")
		s, _, err := load(ctx, dirs, e.Host, e.Session)
		if err != nil {
			t.Fatal(err)
		}
		s.Blocked = true
		if err := save(ctx, dirs, s); err != nil {
			t.Fatal(err)
		}
		codexRow(t, file, "event_msg", map[string]any{"type": "user_message", "message": "written while blocked"})
		if err := Register(ctx, dirs, home, file, filepath.Join(home, "project"), e, false, false, time.Date(2026, 10, 29, 10, 1, 0, 0, time.UTC)); err != nil {
			t.Fatal(err)
		}
		codexRow(t, file, "event_msg", map[string]any{"type": "user_message", "message": "a later prompt"})
		if n, err := Recover(ctx, dirs, e.Host, e.Session); err != nil || n != 1 {
			t.Fatalf("after lift=%d %v", n, err)
		}
		if rows := readInbox(t, dirs, e); len(rows) != 1 || rows[0].Text != "a later prompt" {
			t.Fatalf("%d rows stored", len(rows))
		}
	})
	t.Run("off the record stays blocked", func(t *testing.T) {
		dirs, file, e := fixture(t, "codex")
		home := os.Getenv("HOME")
		codexRow(t, file, "event_msg", map[string]any{"type": "user_message", "message": "off the record"})
		if _, err := Recover(ctx, dirs, e.Host, e.Session); err != nil {
			t.Fatal(err)
		}
		if err := Register(ctx, dirs, home, file, filepath.Join(home, "project"), e, false, false, time.Date(2026, 10, 29, 10, 1, 0, 0, time.UTC)); err != nil {
			t.Fatal(err)
		}
		if s, _, err := load(ctx, dirs, e.Host, e.Session); err != nil || !s.Blocked || s.Reason != "off_record" {
			t.Fatalf("blocked=%v reason=%q err=%v", s.Blocked, s.Reason, err)
		}
	})
}

// A transcript truncated or rewritten in place keeps its file identity, but
// the cursor is past its end: registration starts again at its end, and
// recovery goes on from there.
func TestTranscriptTruncatedInPlaceStartsAgainAtItsEnd(t *testing.T) {
	ctx := context.Background()
	dirs, file, e := fixture(t, "claude-code")
	home := os.Getenv("HOME")
	appendJSON(t, file, claudeRow(e, "user", "first prompt", "turn1"))
	appendJSON(t, file, claudeRow(e, "assistant", "first reply", "turn1"))
	if n, err := Recover(ctx, dirs, e.Host, e.Session); err != nil || n != 2 {
		t.Fatalf("before=%d %v", n, err)
	}
	if err := os.Truncate(file, 0); err != nil {
		t.Fatal(err)
	}
	appendJSON(t, file, claudeRow(e, "user", "written before registration", "turn2"))
	if err := Register(ctx, dirs, home, file, filepath.Join(home, "project"), e, false, false, time.Date(2026, 10, 29, 10, 1, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	appendJSON(t, file, claudeRow(e, "user", "a prompt the hook missed", "turn3"))
	if n, err := Recover(ctx, dirs, e.Host, e.Session); err != nil || n != 1 {
		t.Fatalf("after=%d %v", n, err)
	}
	rows := readInbox(t, dirs, e)
	if len(rows) != 3 || rows[2].Text != "a prompt the hook missed" {
		t.Fatalf("%d rows stored", len(rows))
	}
}

// An event that names no transcript keeps the one registered before, and
// still starts a new baseline when the branch changes: a row written on the
// old branch is never stored under the new one. With nothing changed, the
// cursor is kept and a missed row is recovered.
func TestEventWithoutTranscriptStartsNewBaselineOnBranchChange(t *testing.T) {
	ctx := context.Background()
	dirs, file, e := fixture(t, "codex")
	home := os.Getenv("HOME")
	cwd := filepath.Join(home, "project")
	codexRow(t, file, "event_msg", map[string]any{"type": "user_message", "message": "a prompt on main"})
	if err := Register(ctx, dirs, home, "", cwd, e, false, false, time.Date(2026, 10, 29, 10, 1, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	if n, err := Recover(ctx, dirs, e.Host, e.Session); err != nil || n != 1 {
		t.Fatalf("same branch=%d %v", n, err)
	}
	codexRow(t, file, "event_msg", map[string]any{"type": "user_message", "message": "a later prompt on main"})
	switched := e
	switched.Branch = "feature-x"
	if err := Register(ctx, dirs, home, "", cwd, switched, false, false, time.Date(2026, 10, 29, 10, 2, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	if n, err := Recover(ctx, dirs, e.Host, e.Session); err != nil || n != 0 {
		t.Fatalf("after switch=%d %v", n, err)
	}
	codexRow(t, file, "event_msg", map[string]any{"type": "user_message", "message": "a prompt on feature-x"})
	if n, err := Recover(ctx, dirs, e.Host, e.Session); err != nil || n != 1 {
		t.Fatalf("on feature-x=%d %v", n, err)
	}
	rows := readInbox(t, dirs, e)
	if len(rows) != 2 || rows[0].Branch != "main" || rows[1].Branch != "feature-x" || rows[1].Text != "a prompt on feature-x" {
		t.Fatalf("%d rows stored", len(rows))
	}
}
