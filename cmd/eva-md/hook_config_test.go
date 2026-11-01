package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/heyNag/eva-md/internal/paths"
)

// configNotice is what a chat start says when the config can't be read.
const configNotice = "eva.md: the config can't be read, so this chat isn't captured; run eva-md doctor."

// A config that exists but can't be read turns capture off, as
// TestHookBadConfigFailsClosed shows, and a chat start on either host says
// so in one line, to the user and to the model. Every other event says
// nothing new, a hook in eva-md's own run says nothing, and a config that is
// missing is no such case.
func TestHookSaysAtChatStartThatTheConfigCantBeRead(t *testing.T) {
	d := isolated(t)
	cwd := project(t)
	if err := paths.EnsureDir(d.Config); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(d.ConfigFile(), []byte("exclude = ["), 0o600); err != nil {
		t.Fatal(err)
	}
	hook := func(host, event string) string {
		t.Helper()
		payload, _ := json.Marshal(map[string]string{"hook_event_name": event, "source": "startup", "cwd": cwd, "session_id": "bad-config", "prompt": "please help", "last_assistant_message": "must not persist"})
		var out, errOut bytes.Buffer
		if code := run(context.Background(), []string{"hook", "--host", host}, bytes.NewReader(payload), &out, &errOut); code != 0 || errOut.Len() != 0 {
			t.Fatalf("%s %s: code %d stderr %q", host, event, code, errOut.String())
		}
		return out.String()
	}
	for _, host := range []string{"claude-code", "codex"} {
		var got struct {
			Hook struct {
				Event   string `json:"hookEventName"`
				Context string `json:"additionalContext"`
			} `json:"hookSpecificOutput"`
			Message string `json:"systemMessage"`
		}
		out := hook(host, "SessionStart")
		if err := json.Unmarshal([]byte(out), &got); err != nil || got.Hook.Event != "SessionStart" || got.Hook.Context != configNotice || got.Message != configNotice {
			t.Fatalf("%s SessionStart: %q", host, out)
		}
		stop := ""
		if host == "codex" {
			stop = "{}"
		}
		if out := hook(host, "UserPromptSubmit"); out != "" {
			t.Fatalf("%s UserPromptSubmit: %q", host, out)
		}
		if out := hook(host, "Stop"); out != stop {
			t.Fatalf("%s Stop: %q", host, out)
		}
	}
	if _, err := os.Stat(d.State); !os.IsNotExist(err) {
		t.Fatalf("a hook with an unreadable config wrote state: %v", err)
	}
	t.Setenv("EVA_MD_INNER", "1")
	if out := hook("claude-code", "SessionStart"); out != "" {
		t.Fatalf("eva-md's own run: %q", out)
	}
	t.Setenv("EVA_MD_INNER", "")
	if err := os.Remove(d.ConfigFile()); err != nil {
		t.Fatal(err)
	}
	for _, host := range []string{"claude-code", "codex"} {
		if out := hook(host, "SessionStart"); strings.Contains(out, "config can't be read") {
			t.Fatalf("%s with no config: %q", host, out)
		}
	}
}
