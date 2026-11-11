package hostfacts

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Everything in these rows is made up.
const (
	planted     = "A planted sentence that must never reach a recording"
	madeUpModel = "made-up-model-7q"
	sessionUUID = "0f1e2d3c-4b5a-4968-8776-655443322110"
)

func writeRows(t *testing.T, rows ...any) string {
	t.Helper()
	var b strings.Builder
	for _, r := range rows {
		data, err := json.Marshal(r)
		if err != nil {
			t.Fatal(err)
		}
		b.Write(data)
		b.WriteByte('\n')
	}
	p := filepath.Join(t.TempDir(), "rollout.jsonl")
	if err := os.WriteFile(p, []byte(b.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func fieldAt(t *testing.T, row Row, path string) Field {
	t.Helper()
	for _, f := range row.Fields {
		if f.Path == path {
			return f
		}
	}
	t.Fatalf("row %d (%s) has no field %s", row.Index, row.RowType, path)
	return Field{}
}

// The enum allowlist keeps the values the core facts read, on Codex's
// first, turn-start and realtime rows and on Claude Code's user rows; an
// enum-shaped value at an unlisted path on those rows is kept too; free text
// keeps no text anywhere; a model's name stays a hash even where enums are
// kept; and an unlisted leading tag on a user row that matches no payload
// keeps its name.
func TestSnapshotKeepsTheAllowlistAndNoText(t *testing.T) {
	key := []byte("a made-up key for this test only")
	codex := writeRows(t,
		map[string]any{"timestamp": "2026-01-02T03:04:05.678Z", "type": "session_meta", "payload": map[string]any{
			"id": sessionUUID, "originator": "made_up_originator", "cwd": "/made/up/folder", "instructions": planted,
			"source": map[string]any{"subagent": map[string]any{"thread_spawn": map[string]any{"depth": 1}}},
			"model":  madeUpModel, "cli_version": "9.8.7",
		}},
		map[string]any{"type": "event_msg", "payload": map[string]any{
			"type": "task_started", "turn_id": "turn-made-up-1", "made_up_unlisted": "unlisted_enum_value",
			"turn_attribution": map[string]any{"turn_trigger": "made_up_trigger", "parent_turn_id": "turn-made-up-0", "root_turn_id": "turn-made-up-1"},
			"note":             planted, "model": madeUpModel,
		}},
		map[string]any{"type": "realtime_item", "payload": map[string]any{
			"type": "made_up_item", "role": "made_up_role", "outcome": "made_up_outcome",
			"presentation": map[string]any{"type": "made_up_presentation"}, "text": planted,
		}},
		map[string]any{"type": "response_item", "payload": map[string]any{
			"type": "message", "role": "assistant", "id": "msg_madeup01",
			"content":    []any{map[string]any{"type": "output_text", "text": planted}},
			"not_listed": "plainvalue",
		}},
		map[string]any{"type": "event_msg", "payload": map[string]any{"type": "user_message", "message": "<made_up_wrapper>" + planted + "</made_up_wrapper>"}},
	)
	snap, next, index, err := TakeSnapshot(SnapshotInput{Host: "codex", Path: codex, Key: key, PayloadHashes: map[string]bool{}, Turn: Hash(key, "turn-made-up-1"), MaxBytes: 1 << 20, MaxFields: 1000})
	if err != nil {
		t.Fatal(err)
	}
	if len(snap.Rows) != 5 || index != 5 || next != snap.Size {
		t.Fatalf("rows %d, next %d of %d, index %d", len(snap.Rows), next, snap.Size, index)
	}
	first, start, realtime, response, user := snap.Rows[0], snap.Rows[1], snap.Rows[2], snap.Rows[3], snap.Rows[4]
	kept := map[string]string{
		"type": "session_meta", "payload.originator": "made_up_originator", "payload.id": sessionUUID,
	}
	for path, want := range kept {
		if got := fieldAt(t, first, path).Value; got != want {
			t.Errorf("first row %s: %v, want %q", path, got, want)
		}
	}
	if f := fieldAt(t, first, "payload.source.subagent.thread_spawn"); f.Type != "object" || *f.Items != 1 {
		t.Errorf("the source's key names one level down are not kept: %+v", f)
	}
	for path, want := range map[string]string{
		"payload.type": "task_started", "payload.turn_attribution.turn_trigger": "made_up_trigger",
		"payload.made_up_unlisted": "unlisted_enum_value",
	} {
		if got := fieldAt(t, start, path).Value; got != want {
			t.Errorf("turn-start row %s: %v, want %q", path, got, want)
		}
	}
	if !start.Derived["parent_turn_id_present"] || !start.Derived["root_turn_id_is_turn_id"] {
		t.Errorf("turn-start row's booleans: %v", start.Derived)
	}
	for path, want := range map[string]string{
		"payload.type": "made_up_item", "payload.role": "made_up_role", "payload.outcome": "made_up_outcome", "payload.presentation.type": "made_up_presentation",
	} {
		if got := fieldAt(t, realtime, path).Value; got != want {
			t.Errorf("realtime row %s: %v, want %q", path, got, want)
		}
	}
	if got := fieldAt(t, response, "payload.role").Value; got != "assistant" {
		t.Errorf("a response item's role: %v", got)
	}
	if got := fieldAt(t, response, "payload.content.0.type").Value; got != "output_text" {
		t.Errorf("a response item's content type: %v", got)
	}
	if got := fieldAt(t, response, "payload.id").Value; got != "msg_madeup01" {
		t.Errorf("an id of a listed shape is not kept: %v", got)
	}
	if f := fieldAt(t, response, "payload.not_listed"); f.Value != nil || f.Hash == "" || f.Len != len("plainvalue") {
		t.Errorf("an enum-shaped value off the list, on a row type the core facts do not read, is kept: %+v", f)
	}
	for _, row := range []Row{first, start} {
		if f := fieldAt(t, row, "payload.model"); f.Value != nil || f.Hash != Hash(key, madeUpModel) {
			t.Errorf("a model's name is kept on row %d: %+v", row.Index, f)
		}
	}
	if f := fieldAt(t, first, "payload.cli_version"); f.Value != nil {
		t.Errorf("a version is kept: %+v", f)
	}
	if f := fieldAt(t, user, "payload.message"); f.Tag != "made_up_wrapper" || f.Value != nil || f.Len == 0 {
		t.Errorf("an unlisted leading tag on a user row matching no payload: %+v", f)
	}
	if snap.Presence == nil || snap.Presence.TurnStart == nil || !*snap.Presence.TurnStart {
		t.Errorf("the turn-start row's presence: %+v", snap.Presence)
	}
	// The same row, its text carried by a payload, keeps no tag that is
	// not on the vocabulary.
	again, _, _, err := TakeSnapshot(SnapshotInput{Host: "codex", Path: codex, Key: key, From: 0,
		PayloadHashes: map[string]bool{Hash(key, "<made_up_wrapper>"+planted+"</made_up_wrapper>"): true}, MaxBytes: 1 << 20})
	if err != nil {
		t.Fatal(err)
	}
	if f := fieldAt(t, again.Rows[4], "payload.message"); f.Tag != "" {
		t.Errorf("an unlisted tag is kept on a row a payload matches: %+v", f)
	}

	claude := writeRows(t,
		map[string]any{"type": "user", "entrypoint": "made-up-entry", "userType": "made_up_user_type", "isMeta": true, "sessionId": sessionUUID,
			"version": "9.9.9", "cwd": "/made/up/folder", "promptId": "made-up-prompt-id",
			"message": map[string]any{"role": "user", "content": planted}},
		map[string]any{"type": "assistant", "message": map[string]any{"role": "assistant", "model": madeUpModel, "content": []any{
			map[string]any{"type": "text", "text": planted},
			map[string]any{"type": "tool_use", "id": "toolu_01madeup", "name": "Read", "input": map[string]any{"file_path": "/made/up/folder/plums.txt"}},
		}}},
		map[string]any{"type": "system", "subtype": "made_up_subtype", "level": "info", "content": planted, "isSidechain": false},
	)
	csnap, _, _, err := TakeSnapshot(SnapshotInput{Host: "claude-code", Path: claude, Key: key, PayloadHashes: map[string]bool{}, Prompt: Hash(key, planted), Reply: Hash(key, "a reply no row holds"), MaxBytes: 1 << 20})
	if err != nil {
		t.Fatal(err)
	}
	cu, ca, cs := csnap.Rows[0], csnap.Rows[1], csnap.Rows[2]
	for path, want := range map[string]any{
		"type": "user", "entrypoint": "made-up-entry", "userType": "made_up_user_type", "isMeta": true,
		"message.role": "user", "sessionId": sessionUUID, "promptId": "made-up-prompt-id",
	} {
		if got := fieldAt(t, cu, path).Value; got != want {
			t.Errorf("user row %s: %v, want %v", path, got, want)
		}
	}
	if f := fieldAt(t, cu, "version"); f.Value != nil {
		t.Errorf("a version is kept on a user row: %+v", f)
	}
	if got := fieldAt(t, ca, "message.content.1.type").Value; got != "tool_use" {
		t.Errorf("a content block's type: %v", got)
	}
	if got := fieldAt(t, ca, "message.content.1.id").Value; got != "toolu_01madeup" {
		t.Errorf("a tool-use id: %v", got)
	}
	if f := fieldAt(t, ca, "message.model"); f.Value != nil {
		t.Errorf("the model is kept on an assistant row: %+v", f)
	}
	if got := fieldAt(t, cs, "subtype").Value; got != "made_up_subtype" {
		t.Errorf("a system row's subtype: %v", got)
	}
	if got := fieldAt(t, cs, "isSidechain").Value; got != false {
		t.Errorf("a boolean: %v", got)
	}
	if csnap.Presence == nil || csnap.Presence.Prompt == nil || !*csnap.Presence.Prompt || csnap.Presence.Reply == nil || *csnap.Presence.Reply {
		t.Errorf("presence: %+v", csnap.Presence)
	}
	for _, s := range []Snapshot{snap, csnap} {
		data, err := json.Marshal(s)
		if err != nil {
			t.Fatal(err)
		}
		for _, bad := range []string{planted, "planted sentence", madeUpModel, "9.9.9", "9.8.7", "/made/up/folder", "plums.txt"} {
			if strings.Contains(string(data), bad) {
				t.Errorf("the snapshot holds %q", bad)
			}
		}
	}
}

// A row is described once: the next snapshot starts after the last whole
// row, and a row still being written waits for it.
func TestSnapshotDescribesEachRowOnce(t *testing.T) {
	key := []byte("another made-up key")
	p := writeRows(t, map[string]any{"type": "user", "message": map[string]any{"role": "user", "content": "one"}})
	f, err := os.OpenFile(p, os.O_WRONLY|os.O_APPEND, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	in := SnapshotInput{Host: "claude-code", Path: p, Key: key, MaxBytes: 1 << 20}
	s1, next, index, err := TakeSnapshot(in)
	if err != nil || len(s1.Rows) != 1 {
		t.Fatalf("%v %d", err, len(s1.Rows))
	}
	if _, err := f.WriteString(`{"type":"assistant","message":{"role":"assistant"}}` + "\n" + `{"type":"user","mess`); err != nil {
		t.Fatal(err)
	}
	in.From, in.Index = next, index
	s2, next, index, err := TakeSnapshot(in)
	if err != nil || len(s2.Rows) != 1 || s2.Rows[0].Index != 1 || s2.Rows[0].RowType != "assistant" || s2.PartialTail == 0 {
		t.Fatalf("second snapshot: %v %+v", err, s2)
	}
	if _, err := f.WriteString(`age":{"role":"user","content":"two"}}` + "\n"); err != nil {
		t.Fatal(err)
	}
	in.From, in.Index = next, index
	s3, _, _, err := TakeSnapshot(in)
	if err != nil || len(s3.Rows) != 1 || s3.Rows[0].Index != 2 || s3.Rows[0].RowType != "user" || s3.Rows[0].Offset != next {
		t.Fatalf("third snapshot: %v %+v", err, s3)
	}
	missing, _, _, err := TakeSnapshot(SnapshotInput{Path: filepath.Join(t.TempDir(), "none.jsonl"), Key: key, MaxBytes: 1 << 20})
	if err != nil || !missing.Missing {
		t.Errorf("a missing file: %v %+v", err, missing)
	}
}

func TestNumberShape(t *testing.T) {
	for _, c := range []struct {
		in     string
		sign   string
		digits int
		frac   bool
	}{{"0", "0", 0, false}, {"12", "+", 2, false}, {"-4096", "-", 4, false}, {"3.25", "+", 1, true}, {"0.5", "+", 0, true}, {"1e3", "+", 1, false}} {
		s, d, f := numberShape(c.in)
		if s != c.sign || d != c.digits || f != c.frac {
			t.Errorf("%s: %s %d %v", c.in, s, d, f)
		}
	}
}
