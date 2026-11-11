package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
)

// eva-md audit takes capture, and capture --session or --last N, not both;
// anything else is a usage error that prints nothing on stdout.
func TestAuditUsage(t *testing.T) {
	isolated(t)
	for _, args := range [][]string{
		{"audit"}, {"audit", "list"}, {"audit", "capture", "extra"}, {"audit", "capture", "--last", "0"},
		{"audit", "capture", "--last", "x"}, {"audit", "capture", "--session", ""}, {"audit", "capture", "--session", "a/b"},
		{"audit", "capture", "--session", "a", "--last", "2"},
	} {
		var out, errOut bytes.Buffer
		code := run(context.Background(), args, strings.NewReader(""), &out, &errOut)
		if code != 2 || out.Len() != 0 || !strings.HasPrefix(errOut.String(), "eva-md: ") {
			t.Errorf("eva-md %s: %d %q %q", strings.Join(args, " "), code, out.String(), errOut.String())
		}
	}
	var out, errOut bytes.Buffer
	if code := run(context.Background(), []string{"audit", "capture"}, strings.NewReader(""), &out, &errOut); code != 0 || errOut.Len() != 0 ||
		!strings.HasPrefix(out.String(), "capture audit: 0 sessions in 0 inbox files") {
		t.Errorf("an empty home: %d %q %q", code, out.String(), errOut.String())
	}
}

// A Claude Code chat the real hooks captured, with transcript recovery
// reading the host's record they name, is paired with that record: its
// prompt, tool call and reply are each found. The report holds no text,
// session id, path or project name, and the audit writes nothing.
func TestAuditCaptureOfAChatTheHooksCaptured(t *testing.T) {
	d := isolated(t)
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
		data, _ := json.Marshal(map[string]any{"type": typ, "sessionId": id, "promptId": "P1", "cwd": cwd,
			"timestamp": time.Now().UTC().Format(time.RFC3339Nano), "message": message})
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

	folders, err := os.ReadDir(d.Inbox())
	if err != nil || len(folders) != 1 {
		t.Fatalf("the inbox holds %v (%v)", folders, err)
	}
	before := homeTree(t, home)
	var out, errOut bytes.Buffer
	if code := run(context.Background(), []string{"audit", "capture"}, strings.NewReader(""), &out, &errOut); code != 0 || errOut.Len() != 0 {
		t.Fatalf("%d %q", code, errOut.String())
	}
	report := out.String()
	for _, want := range []string{"1 paired with the host's own record", "prompts: 1 found, 0 missing, 0 extra", "final replies: 1 found, 0 missing, 0 extra", "tool calls: 1 found, 0 missing, 0 extra"} {
		if !strings.Contains(report, want) {
			t.Errorf("the report lacks %q:\n%s", want, report)
		}
	}
	for _, leak := range []string{"zqmark", id, home, folders[0].Name(), "P1", "echo"} {
		if strings.Contains(report, leak) {
			t.Errorf("the report holds %q:\n%s", leak, report)
		}
	}
	if after := homeTree(t, home); !slices.Equal(before, after) {
		t.Errorf("the audit changed the home:\n%q\n%q", before, after)
	}
	for session, sessions := range map[string]string{id: "1", "s:" + id[:8] + "#3": "1", id[:7]: "0", id + "x": "0"} {
		out.Reset()
		code := run(context.Background(), []string{"audit", "capture", "--session", session}, strings.NewReader(""), &out, &errOut)
		if code != 0 || !strings.HasPrefix(out.String(), "capture audit: "+sessions+" sessions") {
			t.Errorf("--session %s: %d %q", session, code, out.String())
		}
	}
}

// homeTree lists every file and folder under root with its mode, last write
// and size.
func homeTree(t *testing.T, root string) []string {
	t.Helper()
	var out []string
	err := filepath.WalkDir(root, func(p string, e fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := e.Info()
		if err != nil {
			return err
		}
		out = append(out, p+" "+info.Mode().String()+" "+info.ModTime().String()+" "+strconv.FormatInt(info.Size(), 10))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}
