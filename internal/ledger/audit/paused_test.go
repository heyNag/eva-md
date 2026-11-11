package audit

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/heyNag/eva-md/internal/inbox"
	"github.com/heyNag/eva-md/internal/ledger/event"
)

// pausedSessions writes two chats, each with a range said off the record,
// and, when inside is set, prompts, replies, tool calls, repeats, gaps,
// failures, interrupts, voice hand-overs, rows too long to read, a row with
// no time, a row in the second the range ends and a line the reader
// repairs within that range on both sides. On Claude Code only the host's record holds the controls, as when
// the hooks missed them and stored what was said; on Codex only the inbox
// does.
func pausedSessions(f *fixture, inside bool) {
	f.rowLimit = 4096
	image := map[string]any{"type": "image", "source": map[string]any{"data": strings.Repeat("A", f.rowLimit)}}
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
			cUser("T4", at(2, 5), []any{text(marker + " too long"), image}),     // a row too long to read
			cAssistant("T6", halfPast(5, 0), "tool_use", toolUse("p9", "Bash")), // in the second its range ends
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
			cx(at(2, 5), "event_msg", map[string]any{"type": "user_message", "message": marker + strings.Repeat("B", f.rowLimit)}), // too long to read
			cx("", "response_item", map[string]any{"type": "function_call", "name": "shell", "call_id": "p3"}),                     // no time
			cx(halfPast(5, 0), "response_item", map[string]any{"type": "function_call", "name": "shell", "call_id": "p4"}),         // in the second its range ends
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
	if !inside {
		f.store("codex", codexID, stored...)
		return
	}
	// A line the reader repairs, inside the range.
	f.store("codex", codexID, stored[:len(stored)-3]...)
	f.raw("codex", codexID, "{"+marker+" not JSON\n")
	f.store("codex", codexID, stored[len(stored)-3:]...)
}

// halfPast is half a second after at(min, sec).
func halfPast(min, sec int) string {
	return time.Date(2026, 11, 2, 10, min, sec, 500e6, time.UTC).Format(time.RFC3339Nano)
}

// raw appends text to the session's inbox file as it is.
func (f *fixture) raw(host, id, text string) {
	f.t.Helper()
	name := filepath.Join(f.dirs.Inbox(), marker+"-project", host+"-"+id+".jsonl")
	file, err := os.OpenFile(name, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		f.t.Fatal(err)
	}
	defer file.Close()
	if _, err := file.WriteString(text); err != nil {
		f.t.Fatal(err)
	}
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

// An off-the-record control the hooks missed, in a Claude Code row too long
// to decode: recovery finds it there, stores nothing in its place and only
// marks the chat, so the inbox holds at most the control that brought the
// chat back. What was said in between is in no count, whether the row is
// one the scan follows or not, and whether the inbox holds that control or
// not.
func TestAControlInALongRowTheHooksMissed(t *testing.T) {
	run := func(bad, inboxOn, inside bool) Report {
		f := newFixture(t)
		f.rowLimit = 4096
		var off any = cUser("T2", at(1, 0), []any{text("off the record"), map[string]any{"type": "image", "source": map[string]any{"data": strings.Repeat("A", f.rowLimit)}}})
		if bad {
			off = `{"type":"user","sessionId":"` + claudeID + `","message":{"role":"user","content":"off the record ` + strings.Repeat("A", f.rowLimit)
		}
		rows := []any{cUser("T1", at(0, 5), marker+" first"), cAssistant("T1", at(0, 6), "end_turn", text(marker+" first reply")), off}
		if inside {
			rows = append(rows,
				cUser("T3", at(2, 0), marker+" said off"),
				cAssistant("T3", at(2, 1), "tool_use", toolUse("p1", "Bash")),
				cAssistant("T3", at(2, 2), "end_turn", text(marker+" off reply")))
		}
		rows = append(rows,
			cUser("T4", at(4, 0), "back on the record"),
			cUser("T5", at(5, 0), marker+" after"),
			cAssistant("T5", at(5, 1), "end_turn", text(marker+" reply after")))
		f.register("claude-code", claudeID, f.transcript("claude-code", claudeID, rows...))
		line := func(ts, turn, role, kind, txt string) inbox.Entry {
			return inbox.Entry{TS: ts, Turn: turn, Role: role, Kind: kind, Text: txt}
		}
		stored := []inbox.Entry{
			line(at(0, 0), "", "event", "event", "session_start"),
			line(at(0, 5), "T1", "user", "prompt", marker+" first"),
			line(at(0, 6), "T1", "assistant", "reply", marker+" first reply"),
		}
		if inboxOn {
			stored = append(stored, line(at(4, 0), "T4", "event", "event", "on_record"))
		}
		stored = append(stored,
			line(at(5, 0), "T5", "user", "prompt", marker+" after"),
			line(at(5, 1), "T5", "assistant", "reply", marker+" reply after"))
		f.store("claude-code", claudeID, stored...)
		r, _ := f.run("", 0)
		r.Stores = nil
		return r
	}
	for _, bad := range []bool{false, true} {
		for _, inboxOn := range []bool{false, true} {
			without, with := run(bad, inboxOn, false), run(bad, inboxOn, true)
			if !reflect.DeepEqual(without, with) {
				t.Errorf("bad %v, inbox control %v: what was said off the record changed the report:\n%+v\n%+v", bad, inboxOn, without, with)
			}
			// A row that can't be read opens its range at the last row
			// before it, and the inbox's control with no range open at its
			// last line before it: the first reply's.
			want := [kinds]Coverage{Prompt: {Found: 2}, Reply: {Found: 2}}
			if bad || inboxOn {
				want[Reply].Found = 1
			}
			if with.Coverage != want || with.PausedHost != 1 || with.Recovery.Paired != 1 {
				t.Errorf("bad %v, inbox control %v: coverage %+v, paused %d, recovery %+v", bad, inboxOn, with.Coverage, with.PausedHost, with.Recovery)
			}
		}
	}
}

// The inbox's ranges: a pause control opens one and the next line with a
// time that is neither withheld nor repaired closes it, a second after it;
// a control that brings the chat back with no range open closes one that
// began at the last line before it; a range still open at the end is open.
func TestPauses(t *testing.T) {
	ts := func(min, sec int) time.Time { v, _ := time.Parse(time.RFC3339, at(min, sec)); return v }
	ev := func(min, sec int, kind event.Kind, op string, marks ...event.Mark) event.Event {
		e := event.Event{Kind: kind, Op: op, Marks: marks}
		if min >= 0 {
			e.T = at(min, sec)
		}
		return e
	}
	withheld := ev(2, 1, event.KindPrompt, "", event.MarkWithheld)
	withheld.Why = event.WhyUnconfirmed
	events := []event.Event{
		ev(0, 0, event.KindPrompt, ""),
		ev(1, 0, event.KindControl, event.OpOff),
		ev(-1, 0, event.KindGap, "", event.MarkRepaired),
		ev(-1, 0, event.KindPrompt, ""),
		ev(2, 0, event.KindGap, "", event.MarkRepaired),
		withheld,
		ev(3, 0, event.KindPrompt, ""),
		ev(4, 0, event.KindReply, ""),
		ev(5, 0, event.KindControl, event.OpOn),
		ev(6, 0, event.KindPrompt, ""),
		ev(7, 0, event.KindControl, event.OpOff),
	}
	got, n := pauses(events)
	want := []interval{{ts(1, 0), ts(3, 1)}, {ts(4, 0), ts(5, 1)}, {start: ts(7, 0)}}
	if n != 3 || !reflect.DeepEqual(got, want) {
		t.Errorf("%d ranges %+v, want %+v", n, got, want)
	}
}
