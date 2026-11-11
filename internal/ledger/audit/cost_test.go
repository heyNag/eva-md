package audit

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/heyNag/eva-md/internal/catchup"
	"github.com/heyNag/eva-md/internal/inbox"
)

// What the audit costs on a store of a few hundred sessions whose records
// name no transcript, as on the one real install: the hosts' folders of
// chats are listed once, each session's file is opened once, and every
// turn of it is found. The time it took is logged for the record and
// decides nothing.
func TestTheCostOfAFewHundredSessionsPairedByID(t *testing.T) {
	const claudeSessions, codexSessions, turns = 200, 100, 10
	f := newFixture(t)
	pad := strings.Repeat("x", 800) // a row of about a kilobyte
	var size int64
	for i := range claudeSessions + codexSessions {
		host, id := "claude-code", fmt.Sprintf("%s-cost-%03d", marker, i)
		if i >= claudeSessions {
			host, id = "codex", fmt.Sprintf("%08x-0000-4000-8000-%012x", i, i)
		}
		rows := []any{}
		if host == "codex" {
			rows = append(rows, cx(at(0, 0), "session_meta", map[string]any{"id": id, "cwd": f.cwd}))
		}
		lines := []inbox.Entry{{TS: at(0, 0), Role: "event", Kind: "event", Text: "session_start"}}
		for n := range turns {
			turn := fmt.Sprintf("T%d", n)
			prompt, tool, reply := at(n+1, 0), at(n+1, 1), at(n+1, 2)
			if host == "codex" {
				rows = append(rows,
					cx(prompt, "event_msg", map[string]any{"type": "task_started", "turn_id": turn}),
					cx(prompt, "event_msg", map[string]any{"type": "user_message", "message": marker + pad}),
					cx(tool, "response_item", map[string]any{"type": "function_call", "name": "shell", "call_id": turn, "arguments": pad}),
					cx(reply, "event_msg", map[string]any{"type": "task_complete", "turn_id": turn, "last_agent_message": marker + pad}))
			} else {
				user, call, answer := cUser(turn, prompt, marker+pad), cAssistant(turn, tool, "tool_use", toolUse(turn, "Bash")), cAssistant(turn, reply, "end_turn", text(marker+pad))
				for _, row := range []map[string]any{user, call, answer} {
					row["sessionId"] = id
				}
				rows = append(rows, user, call, answer)
			}
			lines = append(lines,
				inbox.Entry{TS: prompt, Turn: turn, Role: "user", Kind: "prompt", Text: marker + pad},
				inbox.Entry{TS: tool, Turn: turn, Role: "tool", Kind: "tool", Text: "Bash " + marker},
				inbox.Entry{TS: reply, Turn: turn, Role: "assistant", Kind: "reply", Text: marker + pad})
		}
		info, err := os.Stat(f.transcript(host, id, rows...))
		if err != nil {
			t.Fatal(err)
		}
		size += info.Size()
		f.register(host, id, "")
		f.store(host, id, lines...)
	}
	listings := 0
	savedList := listStore
	t.Cleanup(func() { listStore = savedList })
	listStore = func(home string) listing {
		listings++
		return savedList(home)
	}
	opened := map[string]int{}
	savedOpen := openUnregistered
	t.Cleanup(func() { openUnregistered = savedOpen })
	openUnregistered = func(s catchup.Source) (*os.File, error) {
		opened[s.Path]++
		return savedOpen(s)
	}
	start := time.Now()
	r, _ := f.run("", 0)
	took := time.Since(start)
	sessions := claudeSessions + codexSessions
	if listings != 1 || len(opened) != sessions {
		t.Errorf("%d listings, %d files opened", listings, len(opened))
	}
	for path, n := range opened {
		if n != 1 {
			t.Errorf("%s opened %d times", filepath.Base(path), n)
		}
	}
	want := Coverage{Found: sessions * turns}
	if r.Lookup != (Lookup{Paired: sessions}) || r.ByWay[ByID] != [kinds]Coverage{want, want, want} || r.Coverage != r.ByWay[ByID] {
		t.Errorf("lookup %+v, coverage %+v", r.Lookup, r.ByWay)
	}
	t.Logf("%d sessions, %d bytes of host files: audited in %v", sessions, size, took)
}
