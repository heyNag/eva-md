package audit

import (
	"reflect"
	"testing"

	"github.com/heyNag/eva-md/internal/inbox"
)

// pausedSessions writes two chats, each with a range said off the record,
// and, when inside is set, prompts, replies, tool calls, repeats, gaps,
// failures, interrupts and voice hand-overs within that range on both
// sides. On Claude Code only the host's record holds the controls, as when
// the hooks missed them and stored what was said; on Codex only the inbox
// does.
func pausedSessions(f *fixture, inside bool) {
	line := func(ts, turn, role, kind, txt string) inbox.Entry {
		return inbox.Entry{TS: ts, Turn: turn, Role: role, Kind: kind, Text: txt}
	}
	claude := []any{
		cUser("T1", at(0, 5), marker+" before"),
		cAssistant("T1", at(0, 6), "end_turn", text(marker+" reply before")),
		cUser("T2", at(1, 0), "off the record"),
	}
	stored := []inbox.Entry{
		line(at(0, 0), "", "event", "event", "session_start"),
		line(at(0, 5), "T1", "user", "prompt", marker+" before"),
		line(at(0, 6), "T1", "assistant", "reply", marker+" reply before"),
	}
	if inside {
		claude = append(claude,
			cUser("T3", at(2, 0), marker+" said off"),
			cAssistant("T3", at(2, 1), "tool_use", toolUse("p1", "Bash")),
			cAssistant("T3", at(2, 2), "end_turn", text(marker+" off reply")),
			cUser("T4", at(2, 3), "<realtime_delegation>"+marker),
			cUser("T4", at(2, 4), "[Request interrupted by user]"),
		)
		stored = append(stored,
			line(at(2, 0), "T3", "user", "prompt", marker+" said off"),
			line(at(2, 1), "T3", "tool", "tool", "Bash "+marker),
			line(at(2, 1), "T3", "tool", "tool", "Bash "+marker),
			line(at(2, 2), "T3", "assistant", "reply", marker+" off reply"),
			line(at(2, 3), "T4", "user", "prompt", "<realtime_delegation>"+marker),
			line(at(2, 3), "T4", "user", "prompt", "<realtime_delegation>"+marker),
			line(at(2, 4), "T4", "event", "event", "gap: missing final reply"),
			line(at(2, 4), "T5", "user", "prompt", marker+" no reply"),
			line(at(2, 4), "T5", "event", "event", "stop_failure"),
		)
	}
	claude = append(claude,
		cUser("T6", at(5, 0), "back on the record"),
		cUser("T7", at(6, 0), marker+" after"),
		cAssistant("T7", at(6, 1), "end_turn", text(marker+" reply after")),
	)
	stored = append(stored,
		line(at(6, 0), "T7", "user", "prompt", marker+" after"),
		line(at(6, 1), "T7", "assistant", "reply", marker+" reply after"),
	)
	f.register("claude-code", claudeID, f.transcript("claude-code", claudeID, claude...))
	f.store("claude-code", claudeID, stored...)

	codex := []any{
		cx(at(0, 0), "session_meta", map[string]any{"id": codexID, "cwd": f.cwd}),
		cx(at(0, 1), "event_msg", map[string]any{"type": "task_started", "turn_id": "U1"}),
		cx(at(0, 2), "event_msg", map[string]any{"type": "user_message", "message": marker + " before"}),
		cx(at(0, 3), "event_msg", map[string]any{"type": "task_complete", "turn_id": "U1", "last_agent_message": marker}),
	}
	stored = []inbox.Entry{
		line(at(0, 0), "", "event", "event", "session_start"),
		line(at(0, 2), "U1", "user", "prompt", marker+" before"),
		line(at(0, 3), "U1", "assistant", "reply", marker),
		line(at(1, 0), "U2", "event", "event", "off_record"),
	}
	if inside {
		codex = append(codex,
			cx(at(2, 0), "event_msg", map[string]any{"type": "task_started", "turn_id": "U3"}),
			cx(at(2, 1), "event_msg", map[string]any{"type": "user_message", "message": "<realtime_delegation>" + marker}),
			cx(at(2, 2), "response_item", map[string]any{"type": "function_call", "name": "shell", "call_id": "p2"}),
			cx(at(2, 3), "event_msg", map[string]any{"type": "turn_aborted", "turn_id": "U3"}),
			cx(at(2, 4), "event_msg", map[string]any{"type": "task_complete", "turn_id": "U3", "last_agent_message": marker}),
		)
		stored = append(stored,
			line(at(2, 1), "U3", "user", "prompt", "<realtime_delegation>"+marker),
			line(at(2, 2), "U3", "tool", "tool", "Bash "+marker),
			line(at(2, 3), "U3", "event", "event", "interrupt"),
		)
	}
	codex = append(codex,
		cx(at(6, 0), "event_msg", map[string]any{"type": "task_started", "turn_id": "U4"}),
		cx(at(6, 1), "event_msg", map[string]any{"type": "user_message", "message": marker + " after"}),
		cx(at(6, 2), "event_msg", map[string]any{"type": "task_complete", "turn_id": "U4", "last_agent_message": marker}),
	)
	stored = append(stored,
		line(at(5, 0), "U5", "event", "event", "on_record"),
		line(at(6, 1), "U4", "user", "prompt", marker+" after"),
		line(at(6, 2), "U4", "assistant", "reply", marker),
	)
	f.register("codex", codexID, f.transcript("codex", codexID, codex...))
	f.store("codex", codexID, stored...)
}

// What was said off the record is in no count: two inboxes and host
// records that differ only within their ranges said off the record give
// the same report, whichever side holds the controls, and each range counts
// once, as paused.
func TestNothingOffTheRecordIsCounted(t *testing.T) {
	a := newFixture(t)
	pausedSessions(a, false)
	ra, _ := a.run("", 0)
	b := newFixture(t)
	pausedSessions(b, true)
	rb, _ := b.run("", 0)
	ra.Stores, rb.Stores = nil, nil
	if !reflect.DeepEqual(ra, rb) {
		t.Errorf("what was said off the record changed the report:\n%+v\n%+v", ra, rb)
	}
	want := [kinds]Coverage{Prompt: {Found: 4}, Reply: {Found: 4}}
	if rb.Coverage != want || rb.PausedHost != 1 || rb.PausedInbox != 1 || rb.Recovery.Paired != 2 {
		t.Errorf("coverage %+v, paused %d and %d, recovery %+v", rb.Coverage, rb.PausedHost, rb.PausedInbox, rb.Recovery)
	}
}
