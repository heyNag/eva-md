package shapes

import (
	"encoding/json"
	"maps"
	"slices"
	"strings"
	"testing"

	"github.com/heyNag/eva-md/internal/inbox"
)

// oneChatEach writes one Claude Code chat and one Codex chat whose every
// figure is known, with their inbox lines and recovery records.
func oneChatEach(f *fixture) {
	const cid = "c1"
	other := f.cwd + "-two"
	path := f.claudeFile("", cid,
		map[string]any{"type": "user", "sessionId": cid, "promptId": "p1", "timestamp": at(0, 0), "cwd": f.cwd, branchField: "b1",
			"version": "2.1.3", "entrypoint": "cli", "message": map[string]any{"role": "user", "content": "hello"}},
		// A tool's input holds names the lists hold, and a tool's result
		// holds a text item: neither is looked into.
		map[string]any{"type": "assistant", "sessionId": cid, "promptId": "p1", "timestamp": at(0, 5), "cwd": f.cwd,
			"message": map[string]any{"role": "assistant", "model": "m", "stop_reason": "tool_use", "content": []any{
				map[string]any{"type": "text", "text": "x"},
				map[string]any{"type": "tool_use", "id": "u1", "name": "Bash", "input": map[string]any{"type": "text", "text": "y", "isMeta": true, "uuid": "u"}}}}},
		map[string]any{"type": "user", "sessionId": cid, "promptId": "p1", "timestamp": at(0, 7), "cwd": f.cwd, "toolUseResult": map[string]any{"type": "text"},
			"message": map[string]any{"role": "user", "content": []any{
				map[string]any{"type": "tool_result", "tool_use_id": "u1", "content": []any{map[string]any{"type": "text", "text": "z"}}}}}},
		map[string]any{"type": "assistant", "sessionId": cid, "promptId": "p1", "timestamp": at(0, 7), "cwd": other,
			"message": map[string]any{"role": "assistant", "stop_reason": "end_turn", "content": []any{map[string]any{"type": "text", "text": "done"}}}},
		map[string]any{"type": "system", "subtype": "boundary", "timestamp": at(0, 6), marker + "-key": 1},
	)
	f.session(claude, cid, path,
		inbox.Entry{TS: at(0, 0), Turn: "p1", Role: "user", Kind: "prompt", Text: "hello"},
		inbox.Entry{TS: at(0, 8), Turn: "p1", Role: "assistant", Kind: "reply", Text: "done"})

	const xid = "0a1b2c3d-4e5f-4a6b-8c7d-9e0f1a2b3c4d"
	xpath := f.codexFile(2, xid,
		xMeta(f, xid, map[string]any{"source": "cli", "originator": marker + "-orig", "cli_version": "0.46.0"}),
		xRow(at(0, 1), "turn_context", map[string]any{"turn_id": "t1", "cwd": f.cwd}),
		xRow(at(0, 1), "event_msg", map[string]any{"type": "task_started", "turn_id": "t1"}),
		xRow(at(0, 2), "event_msg", map[string]any{"type": "user_message", "message": "hi"}),
		xRow(at(0, 3), "event_msg", map[string]any{"type": "user_message", "message": "more"}),
		xRow(at(0, 4), "response_item", map[string]any{"type": "function_call", "name": "shell", "call_id": "c", "arguments": `{"type":"message","role":"user"}`}),
		xRow(at(0, 5), "event_msg", map[string]any{"type": "task_complete", "turn_id": "t1", "last_agent_message": "ok"}),
		xRow(at(0, 6), "compacted", map[string]any{"message": "a summary"}),
		xRow(at(0, 7), "event_msg", map[string]any{"type": "user_message", "message": "  <realtime_delegation><input>x</input>"}),
	)
	f.session(codex, xid, xpath,
		inbox.Entry{TS: at(0, 2), Turn: "t1", Role: "user", Kind: "prompt", Text: "hi"},
		inbox.Entry{TS: at(0, 5), Turn: "t1", Role: "assistant", Kind: "reply", Text: "ok"})
}

// On one chat of each host, every figure is exact: rows by type, fields by
// row type, a message's fields, content items by type and their fields,
// the values the lists name, order and gaps, folders and branches, turn
// ids on both sides, and what was read. A tool's input, a tool's result
// and a call's arguments are never looked into for names, though they hold
// names the lists hold. The figures are the same with a link where macOS
// or a person may put one.
func TestFiguresOfOneChatEach(t *testing.T) {
	for _, link := range links {
		t.Run(link, func(t *testing.T) { figuresOfOneChatEach(t, link) })
	}
}

func figuresOfOneChatEach(t *testing.T, link string) {
	f := newFixture(t, link)
	oneChatEach(f)
	r, out := f.run()
	want := map[string]int{
		"F-085/claude-code/row_types/rows/user": 2, "F-085/claude-code/row_types/rows/assistant": 2, "F-085/claude-code/row_types/rows/system": 1,
		"F-085/claude-code/row_types/chats/user": 1, "F-085/claude-code/row_types/chats/assistant": 1, "F-085/claude-code/row_types/chats/system": 1,
		"F-085/claude-code/subtypes/rows/other": 1, "F-085/claude-code/subtypes/chats/other": 1, "F-085/claude-code/subtypes/other_names": 1,
		"F-085/claude-code/bytes/user/under 1 KiB": 2, "F-085/claude-code/bytes/assistant/under 1 KiB": 2, "F-085/claude-code/bytes/system/under 1 KiB": 1,
		"F-084/claude-code/fields/user/rows/type": 2, "F-084/claude-code/fields/user/rows/sessionId": 2, "F-084/claude-code/fields/user/rows/promptId": 2,
		"F-084/claude-code/fields/user/rows/timestamp": 2, "F-084/claude-code/fields/user/rows/cwd": 2, "F-084/claude-code/fields/user/rows/" + branchField: 1,
		"F-084/claude-code/fields/user/rows/version": 1, "F-084/claude-code/fields/user/rows/entrypoint": 1, "F-084/claude-code/fields/user/rows/message": 2,
		"F-084/claude-code/fields/user/rows/toolUseResult": 1,
		"F-084/claude-code/fields/assistant/rows/type":     2, "F-084/claude-code/fields/assistant/rows/sessionId": 2, "F-084/claude-code/fields/assistant/rows/promptId": 2,
		"F-084/claude-code/fields/assistant/rows/timestamp": 2, "F-084/claude-code/fields/assistant/rows/cwd": 2, "F-084/claude-code/fields/assistant/rows/message": 2,
		"F-084/claude-code/fields/system/rows/type": 1, "F-084/claude-code/fields/system/rows/subtype": 1, "F-084/claude-code/fields/system/rows/timestamp": 1,
		"F-084/claude-code/fields/system/rows/other": 1, "F-084/claude-code/fields/system/other_names": 1, "F-084/claude-code/fields/system/chats/other": 1,
		"F-084/claude-code/message_fields/rows/role": 4, "F-084/claude-code/message_fields/rows/content": 4, "F-084/claude-code/message_fields/rows/model": 1,
		"F-084/claude-code/message_fields/rows/stop_reason": 2,
		"F-084/claude-code/item_types/rows/text":            2, "F-084/claude-code/item_types/rows/tool_use": 1, "F-084/claude-code/item_types/rows/tool_result": 1,
		"F-084/claude-code/item_fields/text/rows/type": 2, "F-084/claude-code/item_fields/text/rows/text": 2,
		"F-084/claude-code/item_fields/tool_use/rows/type": 1, "F-084/claude-code/item_fields/tool_use/rows/id": 1, "F-084/claude-code/item_fields/tool_use/rows/name": 1,
		"F-084/claude-code/item_fields/tool_use/rows/input":   1,
		"F-084/claude-code/item_fields/tool_result/rows/type": 1, "F-084/claude-code/item_fields/tool_result/rows/tool_use_id": 1, "F-084/claude-code/item_fields/tool_result/rows/content": 1,
		"F-084/claude-code/versions/chats/2.1.3": 1,
		"F-079/claude-code/entrypoint/rows/cli":  1, "F-079/claude-code/entrypoint/chats/cli": 1,
		"F-018/claude-code/entrypoint/rows/cli": 1, "F-018/claude-code/entrypoint/chats/cli": 1,
		"F-022/claude-code/assistant_rows/text_and_tools": 1, "F-022/claude-code/assistant_rows/text_only": 1,
		"F-022/claude-code/tool_results": 1, "F-022/claude-code/with_tool_use_result": 1,
		"F-020/claude-code/order/later": 2, "F-020/claude-code/order/same_time": 1, "F-020/claude-code/order/earlier": 1,
		"F-020/claude-code/gaps/1 to 10 s": 2,
		"F-021/claude-code/user/time":      2, "F-021/claude-code/user/folder": 2, "F-021/claude-code/user/branch": 1,
		"F-021/claude-code/assistant/time": 2, "F-021/claude-code/assistant/folder": 2, "F-021/claude-code/system/time": 1,
		"F-021/claude-code/folder_changed": 1,
		"F-001/claude-code/rows":           4, "F-001/claude-code/with_turn_id": 4, "F-001/claude-code/inbox_turns/one_id": 1, "F-001/claude-code/inbox_ids/on_rows": 1,
		"F-004/claude-code/replies_by_hook": 1,
		"F-005/claude-code/records":         1, "F-005/claude-code/path_named": 1,
		"F-013/claude-code/file_ids/other_form": 1, "F-013/claude-code/inbox_ids/other_form": 1,

		"F-132/codex/row_types/rows/session_meta": 1, "F-132/codex/row_types/rows/turn_context": 1, "F-132/codex/row_types/rows/event_msg": 5,
		"F-132/codex/row_types/rows/response_item": 1, "F-132/codex/row_types/rows/compacted": 1,
		"F-132/codex/row_types/chats/session_meta": 1, "F-132/codex/row_types/chats/turn_context": 1, "F-132/codex/row_types/chats/event_msg": 1,
		"F-132/codex/row_types/chats/response_item": 1, "F-132/codex/row_types/chats/compacted": 1,
		"F-132/codex/fields/rows/timestamp": 9, "F-132/codex/fields/rows/type": 9, "F-132/codex/fields/rows/payload": 9,
		"F-132/codex/fields/chats/timestamp": 1, "F-132/codex/fields/chats/type": 1, "F-132/codex/fields/chats/payload": 1,
		"F-132/codex/payload_types/rows/task_started": 1, "F-132/codex/payload_types/rows/user_message": 3, "F-132/codex/payload_types/rows/task_complete": 1,
		"F-132/codex/payload_types/rows/function_call": 1,
		"F-132/codex/payload_types/chats/task_started": 1, "F-132/codex/payload_types/chats/user_message": 1, "F-132/codex/payload_types/chats/task_complete": 1,
		"F-132/codex/payload_types/chats/function_call":         1,
		"F-132/codex/payload_fields/session_meta/other/rows/id": 1, "F-132/codex/payload_fields/session_meta/other/rows/cwd": 1,
		"F-132/codex/payload_fields/session_meta/other/rows/source": 1, "F-132/codex/payload_fields/session_meta/other/rows/originator": 1,
		"F-132/codex/payload_fields/session_meta/other/rows/cli_version": 1,
		"F-132/codex/payload_fields/session_meta/other/chats/id":         1, "F-132/codex/payload_fields/session_meta/other/chats/cwd": 1,
		"F-132/codex/payload_fields/session_meta/other/chats/source": 1, "F-132/codex/payload_fields/session_meta/other/chats/originator": 1,
		"F-132/codex/payload_fields/session_meta/other/chats/cli_version": 1,
		"F-132/codex/payload_fields/turn_context/other/rows/turn_id":      1, "F-132/codex/payload_fields/turn_context/other/rows/cwd": 1,
		"F-132/codex/payload_fields/turn_context/other/chats/turn_id": 1, "F-132/codex/payload_fields/turn_context/other/chats/cwd": 1,
		"F-132/codex/payload_fields/event_msg/task_started/rows/type": 1, "F-132/codex/payload_fields/event_msg/task_started/rows/turn_id": 1,
		"F-132/codex/payload_fields/event_msg/task_started/chats/type": 1, "F-132/codex/payload_fields/event_msg/task_started/chats/turn_id": 1,
		"F-132/codex/payload_fields/event_msg/user_message/rows/type": 3, "F-132/codex/payload_fields/event_msg/user_message/rows/message": 3,
		"F-132/codex/payload_fields/event_msg/user_message/chats/type": 1, "F-132/codex/payload_fields/event_msg/user_message/chats/message": 1,
		"F-132/codex/payload_fields/event_msg/task_complete/rows/type": 1, "F-132/codex/payload_fields/event_msg/task_complete/rows/turn_id": 1,
		"F-132/codex/payload_fields/event_msg/task_complete/rows/last_agent_message": 1,
		"F-132/codex/payload_fields/event_msg/task_complete/chats/type":              1, "F-132/codex/payload_fields/event_msg/task_complete/chats/turn_id": 1,
		"F-132/codex/payload_fields/event_msg/task_complete/chats/last_agent_message": 1,
		"F-132/codex/payload_fields/response_item/function_call/rows/type":            1, "F-132/codex/payload_fields/response_item/function_call/rows/name": 1,
		"F-132/codex/payload_fields/response_item/function_call/rows/call_id": 1, "F-132/codex/payload_fields/response_item/function_call/rows/arguments": 1,
		"F-132/codex/payload_fields/response_item/function_call/chats/type": 1, "F-132/codex/payload_fields/response_item/function_call/chats/name": 1,
		"F-132/codex/payload_fields/response_item/function_call/chats/call_id": 1, "F-132/codex/payload_fields/response_item/function_call/chats/arguments": 1,
		"F-132/codex/payload_fields/compacted/other/rows/message": 1, "F-132/codex/payload_fields/compacted/other/chats/message": 1,
		"F-132/codex/versions/chats/0.46.0": 1,
		"F-018/codex/source/rows/cli":       1, "F-018/codex/source/chats/cli": 1,
		"F-018/codex/originator/rows/other": 1, "F-018/codex/originator/chats/other": 1, "F-018/codex/originator/other_names": 1,
		"F-010/codex/prompts": 3, "F-010/codex/began_turn": 2, "F-010/codex/while_working": 1, "F-010/codex/chats/while_working": 1,
		"F-141/codex/wrapper_prompts": 1,
		"F-034/codex/compacted":       1, "F-136/codex/summary_bytes/under 1 KiB": 1,
		"F-134/codex/first_row/rows/session_meta": 1, "F-134/codex/first_row/chats/session_meta": 1, "F-134/codex/first_row_bytes/under 1 KiB": 1,
		"F-134/codex/payload_fields/rows/id": 1, "F-134/codex/payload_fields/rows/cwd": 1, "F-134/codex/payload_fields/rows/source": 1,
		"F-134/codex/payload_fields/rows/originator": 1, "F-134/codex/payload_fields/rows/cli_version": 1,
		"F-134/codex/payload_fields/chats/id": 1, "F-134/codex/payload_fields/chats/cwd": 1, "F-134/codex/payload_fields/chats/source": 1,
		"F-134/codex/payload_fields/chats/originator": 1, "F-134/codex/payload_fields/chats/cli_version": 1,
		"F-138/codex/with_a_turn": 1, "F-138/claude-code/with_a_turn": 1,
		"F-027/claude-code/subagent_files/read": 0,
		"F-001/codex/rows":                      5, "F-001/codex/with_turn_id": 2, "F-001/codex/inbox_turns/one_id": 1, "F-001/codex/inbox_ids/on_rows": 1,
		"F-004/codex/replies_by_hook": 1,
		"F-005/codex/records":         1, "F-005/codex/path_named": 1,
		"F-013/codex/file_ids/uuid_form": 1, "F-013/codex/inbox_ids/uuid_form": 1,
		"F-020/codex/order/later": 7, "F-020/codex/order/same_time": 1, "F-020/codex/gaps/under 1 s": 0, "F-020/codex/gaps/1 to 10 s": 7,
		"F-021/codex/session_meta/time": 1, "F-021/codex/session_meta/folder": 1, "F-021/codex/turn_context/time": 1, "F-021/codex/turn_context/folder": 1,
		"F-021/codex/event_msg/time": 5, "F-021/codex/response_item/time": 1, "F-021/codex/compacted/time": 1,
	}
	// Each name counted in rows is counted in one chat, as each host has
	// one.
	for p := range maps.Clone(want) {
		if i := strings.LastIndex(p, "/rows/"); i >= 0 {
			want[p[:i]+"/chats/"+p[i+len("/rows/"):]] = 1
		}
	}
	got := leaves(r.Facts)
	// The gap from the last row to the file's last write depends on when
	// the test ran; only that it is counted once is checked.
	for _, h := range []string{claude, codex} {
		n := 0
		for p, c := range got {
			if strings.HasPrefix(p, "F-020/"+h+"/file_written_after_last_row/") {
				n += c
				delete(got, p)
			}
		}
		if n != 1 {
			t.Errorf("%s: the file's last write counted %d times", h, n)
		}
	}
	for p, n := range want {
		if n == 0 {
			delete(want, p)
		}
	}
	for _, p := range slices.Sorted(maps.Keys(want)) {
		if got[p] != want[p] {
			t.Errorf("%s: %d, want %d", p, got[p], want[p])
		}
	}
	for _, p := range slices.Sorted(maps.Keys(got)) {
		if _, ok := want[p]; !ok {
			t.Errorf("%s: %d, not expected", p, got[p])
		}
	}
	hosts := map[string]int{
		"claude-code/files/listed": 1, "claude-code/files/read": 1, "claude-code/chats/read": 1, "claude-code/chats/files_per_chat/1": 1,
		"claude-code/rows/read": 5,
		"codex/files/listed":    1, "codex/files/read": 1, "codex/chats/read": 1, "codex/chats/files_per_chat/1": 1, "codex/rows/read": 9,
	}
	if got := leaves(r.Hosts); !maps.Equal(got, hosts) {
		t.Errorf("hosts:\n%v\nwant\n%v", got, hosts)
	}
	if r.Parts["inbox"] != PartCounted || r.Parts[claude] != PartCounted || r.Parts[codex] != PartCounted || len(r.Parts) != 3 {
		t.Errorf("parts %v", r.Parts)
	}
	if r.Days[claude] != [2]string{"2026-11-02", "2026-11-02"} || r.Days[codex] != [2]string{"2026-11-02", "2026-11-02"} {
		t.Errorf("days %v", r.Days)
	}
	// As printed, each count of 1 or 2 is "under 3".
	var printed map[string]any
	if err := json.Unmarshal([]byte(out), &printed); err != nil {
		t.Fatal(err)
	}
	facts := printed["facts"].(map[string]any)
	if v := facts["F-132"].(map[string]any)["codex"].(map[string]any)["fields"].(map[string]any)["rows"].(map[string]any)["type"]; v != float64(9) {
		t.Errorf("9 rows print as %v", v)
	}
	if v := facts["F-010"].(map[string]any)["codex"].(map[string]any)["prompts"]; v != float64(3) {
		t.Errorf("3 prompts print as %v", v)
	}
	for _, v := range []any{facts["F-010"].(map[string]any)["codex"].(map[string]any)["while_working"],
		facts["F-085"].(map[string]any)["claude-code"].(map[string]any)["row_types"].(map[string]any)["rows"].(map[string]any)["user"]} {
		if v != under3 {
			t.Errorf("a count of 1 or 2 prints as %v", v)
		}
	}
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimRight(strings.TrimSpace(line), ",")
		if (strings.HasSuffix(line, ": 1") || strings.HasSuffix(line, ": 2")) && line != `"format": 1` {
			t.Errorf("a count of 1 or 2 is printed as a number: %s", line)
		}
	}
}

// A count of 1 or 2 prints as "under 3", any other as its number.
func TestCountsUnderThree(t *testing.T) {
	for n, want := range map[Count]string{0: "0", 1: `"under 3"`, 2: `"under 3"`, 3: "3", 40: "40"} {
		got, err := json.Marshal(n)
		if err != nil || string(got) != want {
			t.Errorf("%d prints as %s (%v), want %s", n, got, err, want)
		}
	}
}
