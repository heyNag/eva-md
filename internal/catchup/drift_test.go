package catchup

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/heyNag/eva-md/internal/inbox"
	"github.com/heyNag/eva-md/internal/paths"
	"github.com/heyNag/eva-md/internal/session"
)

// A chat a program runs keeps its prompts out of the inbox: recovery never
// stores one as the user's, though it recovers the replies and honours a
// record control.
func TestHeadlessChatPromptsAreNotRecovered(t *testing.T) {
	dirs, file, e := fixture(t, "claude-code")
	ctx := context.Background()
	home := os.Getenv("HOME")
	if err := Register(ctx, dirs, home, file, filepath.Join(home, e.Cwd), e, false, true, time.Date(2026, 10, 29, 10, 0, 30, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	appendJSON(t, file, claudeRow(e, "user", "Summarize the release checklist.", "turn1"))
	appendJSON(t, file, claudeRow(e, "assistant", "The checklist has three steps.", "turn1"))
	if n, err := Recover(ctx, dirs, e.Host, e.Session); err != nil || n != 1 {
		t.Fatalf("recover=%d %v", n, err)
	}
	if rows := readInbox(t, dirs, e); len(rows) != 1 || rows[0].Role != "assistant" {
		t.Fatalf("rows=%#v", rows)
	}
	appendJSON(t, file, claudeRow(e, "user", "off the record", "turn2"))
	if _, err := Recover(ctx, dirs, e.Host, e.Session); err != nil {
		t.Fatal(err)
	}
	if _, off, err := (session.Markers{Root: dirs.Sessions()}).Get(e.Host, e.Session, session.OffRecord); err != nil || !off {
		t.Fatalf("off=%v err=%v", off, err)
	}
}

// renamedRow is a row after a host update renamed a field the parser needs:
// Claude's sessionId, or the type of Codex's user message.
func renamedRow(e inbox.Entry, n int) map[string]any {
	if e.Host == "codex" {
		return map[string]any{"type": "event_msg", "timestamp": "2026-10-29T10:01:00Z", "payload": map[string]any{"type": "userMessage", "message": "step " + string(rune('a'+n))}}
	}
	row := claudeRow(e, "assistant", "step "+string(rune('a'+n)), "")
	delete(row, "sessionId")
	row["session_id"] = e.Session
	return row
}

// knownRow is a reply row in the format the parser reads.
func knownRow(e inbox.Entry) map[string]any {
	if e.Host == "codex" {
		return map[string]any{"type": "event_msg", "timestamp": "2026-10-29T10:02:00Z", "payload": map[string]any{"type": "agent_message", "phase": "final", "message": "Back in a known format."}}
	}
	return claudeRow(e, "assistant", "Back in a known format.", "")
}

// Every row of a chat a host update has renamed a field in goes
// unrecognized. Three turns in a row that end with a reply over such rows
// record the host's drift signal; a pass that recognizes a row clears it.
func TestDriftSignal(t *testing.T) {
	for _, host := range []string{"claude-code", "codex"} {
		t.Run(host, func(t *testing.T) {
			dirs, file, e := fixture(t, host)
			ctx := context.Background()
			for turn := 0; turn < driftTurns; turn++ {
				if _, found, err := ReadDrift(dirs, host); err != nil || found {
					t.Fatalf("turn %d: found=%v err=%v", turn, found, err)
				}
				appendJSON(t, file, renamedRow(e, turn))
				if _, err := RecoverAfterReply(ctx, dirs, host, e.Session); err != nil {
					t.Fatal(err)
				}
			}
			d, found, err := ReadDrift(dirs, host)
			if err != nil || !found || d.Turns != driftTurns || d.Session != e.Session || d.Bytes == 0 {
				t.Fatalf("drift=%#v found=%v err=%v", d, found, err)
			}
			appendJSON(t, file, knownRow(e))
			if n, err := Recover(ctx, dirs, host, e.Session); err != nil || n != 1 {
				t.Fatalf("recover=%d %v", n, err)
			}
			if _, found, err := ReadDrift(dirs, host); err != nil || found {
				t.Fatalf("found=%v err=%v after a recognized row", found, err)
			}
		})
	}
}

// The record follows a drift that goes on: a kept record keeps the time the
// drift began while its counts are the latest turn's, and one deleted, as
// the doctor's fix for a damaged record says, is written again on the next
// blind turn.
func TestDriftRecordFollowsTheDrift(t *testing.T) {
	dirs, file, e := fixture(t, "claude-code")
	ctx := context.Background()
	blind := func(turn int) (Drift, bool) {
		t.Helper()
		appendJSON(t, file, renamedRow(e, turn))
		if _, err := RecoverAfterReply(ctx, dirs, e.Host, e.Session); err != nil {
			t.Fatal(err)
		}
		d, found, err := ReadDrift(dirs, e.Host)
		if err != nil {
			t.Fatal(err)
		}
		return d, found
	}
	for turn := 0; turn < driftTurns; turn++ {
		blind(turn)
	}
	record := filepath.Join(dirs.State, "catchup", driftName(e.Host))
	began := Drift{V: 1, Host: e.Host, Session: e.Session, At: "2026-10-29T09:00:00Z", Bytes: 1, Turns: driftTurns}
	data, _ := json.Marshal(began)
	if err := paths.WriteFileAtomic(record, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if d, found := blind(driftTurns); !found || d.At != began.At || d.Turns != driftTurns+1 || d.Bytes <= began.Bytes {
		t.Fatalf("kept record=%#v found=%v", d, found)
	}
	if err := os.Remove(record); err != nil {
		t.Fatal(err)
	}
	if d, found := blind(driftTurns + 1); !found || d.At == began.At || d.Turns != driftTurns+2 {
		t.Fatalf("record after delete=%#v found=%v", d, found)
	}
}

// A new transcript baseline, as after the file is replaced, drops the blind
// span the old file built: recovery has read none of the new file.
func TestNewBaselineDropsTheBlindSpan(t *testing.T) {
	dirs, file, e := fixture(t, "claude-code")
	ctx := context.Background()
	appendJSON(t, file, renamedRow(e, 0))
	if _, err := RecoverAfterReply(ctx, dirs, e.Host, e.Session); err != nil {
		t.Fatal(err)
	}
	if s, _, err := load(ctx, dirs, e.Host, e.Session); err != nil || s.Blind == nil || s.Blind.Turns != 1 {
		t.Fatalf("blind=%#v err=%v", s.Blind, err)
	}
	if err := os.WriteFile(file+".new", nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(file+".new", file); err != nil {
		t.Fatal(err)
	}
	home := os.Getenv("HOME")
	if err := Register(ctx, dirs, home, file, filepath.Join(home, e.Cwd), e, false, false, time.Date(2026, 10, 29, 10, 5, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	if s, _, err := load(ctx, dirs, e.Host, e.Session); err != nil || s.Blind != nil {
		t.Fatalf("blind=%#v err=%v after a new baseline", s.Blind, err)
	}
}

// Rows recovery recognizes but never stores, as in a chat a loop drives,
// count as read, and a recognized row between blind turns starts the count
// again. Passes for events that did not end a turn with a reply never
// count at all.
func TestDriftNeedsBlindTurnsInARow(t *testing.T) {
	dirs, file, e := fixture(t, "claude-code")
	ctx := context.Background()
	// pass runs one recovery pass, for a turn that ended with a reply when
	// replied is set, and checks that no drift is recorded.
	pass := func(phase string, replied bool) {
		t.Helper()
		run := Recover
		if replied {
			run = RecoverAfterReply
		}
		if _, err := run(ctx, dirs, e.Host, e.Session); err != nil {
			t.Fatal(err)
		}
		if _, found, err := ReadDrift(dirs, e.Host); err != nil || found {
			t.Fatalf("%s: found=%v err=%v", phase, found, err)
		}
	}
	for turn := 0; turn < 2*driftTurns; turn++ {
		prompt := claudeRow(e, "user", "<task-notification>build check</task-notification>", "")
		reply := claudeRow(e, "assistant", "The build passes.", "")
		prompt["turnOrigin"], reply["turnOrigin"] = "loop", "loop"
		appendJSON(t, file, prompt)
		appendJSON(t, file, map[string]any{"type": "progress", "sessionId": e.Session, "timestamp": "2026-10-29T10:01:00Z"})
		appendJSON(t, file, reply)
		pass("turns a loop drives", true)
	}
	for turn := 0; turn < 2*driftTurns; turn++ {
		appendJSON(t, file, renamedRow(e, turn))
		if turn%2 == 1 {
			appendJSON(t, file, knownRow(e))
		}
		pass("a recognized row every other turn", true)
	}
	for turn := 0; turn < 2*driftTurns; turn++ {
		appendJSON(t, file, renamedRow(e, turn))
		pass("passes that ended no turn", false)
	}
	if rows := readInbox(t, dirs, e); len(rows) != 1 {
		t.Fatalf("rows=%#v", rows)
	}
}

// A drift record that can't be read is an error, not a signal.
func TestReadDriftRejectsADamagedRecord(t *testing.T) {
	dirs, _, _ := fixture(t, "codex")
	file := filepath.Join(dirs.State, "catchup", driftName("codex"))
	if err := paths.WriteFileAtomic(file, []byte(`{"v":1,"host":"claude-code"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, found, err := ReadDrift(dirs, "codex"); err == nil || found {
		t.Fatalf("found=%v err=%v", found, err)
	}
}
