package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/heyNag/eva-md/internal/ledger/shapes"
)

// eva-md audit shapes takes no argument; anything else is a usage error
// that prints nothing on stdout.
func TestAuditShapesUsage(t *testing.T) {
	isolated(t)
	for _, args := range [][]string{{"audit", "shapes", "extra"}, {"audit", "shapes", "--last", "2"}, {"audit", "shape"}} {
		var out, errOut bytes.Buffer
		code := run(context.Background(), args, strings.NewReader(""), &out, &errOut)
		if code != 2 || out.Len() != 0 || !strings.HasPrefix(errOut.String(), "eva-md: ") {
			t.Errorf("eva-md %s: %d %q %q", strings.Join(args, " "), code, out.String(), errOut.String())
		}
	}
}

// capturedChat runs the real hooks over a Claude Code chat whose
// transcript the test writes as the host would, and returns the home.
func capturedChat(t *testing.T) string {
	t.Helper()
	cwd, err := filepath.EvalSymlinks(project(t))
	if err != nil {
		t.Fatal(err)
	}
	home := os.Getenv("HOME")
	const id = "zqmark-session-1"
	transcript := filepath.Join(home, ".claude", "projects", "-zqmark-folder", id+".jsonl")
	if err := os.MkdirAll(filepath.Dir(transcript), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(transcript, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	row := func(typ string, message map[string]any) {
		t.Helper()
		data, _ := json.Marshal(map[string]any{"type": typ, "sessionId": id, "promptId": "P1", "cwd": cwd, "version": "2.1.7",
			"entrypoint": "zqmark-entry", "timestamp": time.Now().UTC().Format(time.RFC3339Nano), "message": message})
		f, err := os.OpenFile(transcript, os.O_APPEND|os.O_WRONLY, 0)
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		if _, err := f.Write(append(data, '\n')); err != nil {
			t.Fatal(err)
		}
	}
	hook := func(payload map[string]any) {
		t.Helper()
		payload["session_id"], payload["cwd"], payload["transcript_path"] = id, cwd, transcript
		data, _ := json.Marshal(payload)
		var out, errOut bytes.Buffer
		if code := run(context.Background(), []string{"hook", "--host", "claude-code"}, bytes.NewReader(data), &out, &errOut); code != 0 {
			t.Fatalf("%s: %d %q", payload["hook_event_name"], code, errOut.String())
		}
	}
	hook(map[string]any{"hook_event_name": "SessionStart", "source": "startup"})
	row("user", map[string]any{"role": "user", "content": "zqmark question"})
	hook(map[string]any{"hook_event_name": "UserPromptSubmit", "prompt": "zqmark question", "prompt_id": "P1"})
	row("assistant", map[string]any{"role": "assistant", "stop_reason": "tool_use", "content": []any{map[string]any{"type": "tool_use", "id": "tu1", "name": "Bash", "input": map[string]any{"command": "echo zqmark"}}}})
	hook(map[string]any{"hook_event_name": "PostToolUse", "tool_name": "Bash", "tool_input": map[string]any{"command": "echo zqmark"}, "prompt_id": "P1"})
	row("assistant", map[string]any{"role": "assistant", "stop_reason": "end_turn", "content": []any{map[string]any{"type": "text", "text": "zqmark answer"}}})
	hook(map[string]any{"hook_event_name": "Stop", "last_assistant_message": "zqmark answer", "prompt_id": "P1"})
	return home
}

// jsonAt is the value at a path of keys in a decoded report, nil when none.
func jsonAt(doc map[string]any, path ...string) any {
	var node any = doc
	for _, p := range path {
		m, ok := node.(map[string]any)
		if !ok {
			return nil
		}
		node = m[p]
	}
	return node
}

// A Claude Code chat the real hooks captured is read in the host's folder
// of chats: the report gives the shapes of its rows, a count of 1 or 2 as
// "under 3", holds no text, id, path, folder or value of it, and the
// command writes nothing.
func TestAuditShapesOfAChatTheHooksCaptured(t *testing.T) {
	isolated(t)
	home := capturedChat(t)
	folders, err := os.ReadDir(filepath.Join(home, ".local", "state", "eva-md", "inbox"))
	if err != nil || len(folders) != 1 {
		t.Fatalf("the inbox holds %v (%v)", folders, err)
	}
	before := homeTree(t, home)
	var out, errOut bytes.Buffer
	if code := run(context.Background(), []string{"audit", "shapes"}, strings.NewReader(""), &out, &errOut); code != 0 || errOut.Len() != 0 {
		t.Fatalf("%d %q", code, errOut.String())
	}
	var doc map[string]any
	if err := json.Unmarshal(out.Bytes(), &doc); err != nil {
		t.Fatalf("%v: %s", err, out.String())
	}
	for _, c := range []struct {
		path []string
		want any
	}{
		{[]string{"format"}, float64(shapes.Format)},
		{[]string{"parts", "inbox"}, shapes.PartCounted},
		{[]string{"parts", "claude-code"}, shapes.PartCounted},
		{[]string{"parts", "codex"}, shapes.PartNone},
		{[]string{"hosts", "claude-code", "files", "read"}, "under 3"},
		{[]string{"hosts", "claude-code", "rows", "read"}, float64(3)},
		{[]string{"facts", "F-085", "claude-code", "row_types", "rows", "user"}, "under 3"},
		{[]string{"facts", "F-085", "claude-code", "row_types", "rows", "assistant"}, "under 3"},
		{[]string{"facts", "F-022", "claude-code", "assistant_rows", "tools_only"}, "under 3"},
		{[]string{"facts", "F-084", "claude-code", "versions", "chats", "2.1.7"}, "under 3"},
		{[]string{"facts", "F-079", "claude-code", "entrypoint", "rows", "other"}, float64(3)},
		{[]string{"facts", "F-001", "claude-code", "inbox_ids", "on_rows"}, "under 3"},
	} {
		if got := jsonAt(doc, c.path...); got != c.want {
			t.Errorf("%s: %v, want %v", strings.Join(c.path, "."), got, c.want)
		}
	}
	report := out.String()
	for _, leak := range []string{"zqmark", "zqmark-session-1", home, folders[0].Name(), "P1", "echo", "tu1"} {
		if strings.Contains(report, leak) {
			t.Errorf("the report holds %q:\n%s", leak, report)
		}
	}
	if after := homeTree(t, home); !slices.Equal(before, after) {
		t.Errorf("the report changed the home:\n%q\n%q", before, after)
	}
}

// The report reads within its limit. When the limit has run out, it still
// prints its JSON and exits 0, and every part says it is incomplete;
// within the command's 10 minutes, the same home is counted in full.
func TestAuditShapesWithinItsLimit(t *testing.T) {
	isolated(t)
	capturedChat(t)
	if shapesTimeout != 10*time.Minute {
		t.Fatalf("the report's limit is %v", shapesTimeout)
	}
	for _, c := range []struct {
		limit time.Duration
		parts map[string]string
	}{
		{0, map[string]string{"inbox": "incomplete", "claude-code": "incomplete", "codex": "incomplete"}},
		{shapesTimeout, map[string]string{"inbox": "counted", "claude-code": "counted", "codex": "none_recorded"}},
	} {
		var out, errOut bytes.Buffer
		if code := runShapes(context.Background(), []string{"audit", "shapes"}, &out, &errOut, c.limit); code != 0 {
			t.Fatalf("limit %v: exit %d %q", c.limit, code, errOut.String())
		}
		var r struct{ Parts map[string]string }
		if err := json.Unmarshal(out.Bytes(), &r); err != nil {
			t.Fatalf("limit %v: %v: %s", c.limit, err, out.String())
		}
		if fmt.Sprint(r.Parts) != fmt.Sprint(c.parts) {
			t.Errorf("limit %v: %v", c.limit, r.Parts)
		}
	}
}
