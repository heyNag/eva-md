package hook

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/heyNag/eva-md/internal/inbox"
	"github.com/heyNag/eva-md/internal/session"
)

func plant(t *testing.T, env Env, host, id string) {
	t.Helper()
	store := inbox.Store{Root: env.Dirs.Inbox()}
	for i, text := range []string{"The codename is amber-finch.", "Amber-finch still has an open portability question.", "Use plain Markdown; next test Windows."} {
		for _, role := range []string{"user", "assistant"} {
			kind := "prompt"
			if role == "assistant" {
				kind = "reply"
			}
			e := inbox.Entry{V: 1, TS: env.Now().Add(time.Duration(i-5) * time.Minute).Format(time.RFC3339), Host: host, Session: id, Turn: string(rune('a' + i)), Role: role, Kind: kind, Text: text, Project: "general", Branch: "default", Cwd: "project", Model: "fixture-model", Origin: "hook"}
			if err := store.Append(e); err != nil {
				t.Fatal(err)
			}
		}
	}
}
func contextFrom(t *testing.T, out string) string {
	t.Helper()
	var value struct {
		HookSpecificOutput struct {
			Context string `json:"additionalContext"`
		} `json:"hookSpecificOutput"`
	}
	if err := json.Unmarshal([]byte(out), &value); err != nil {
		t.Fatalf("output=%q: %v", out, err)
	}
	return value.HookSpecificOutput.Context
}
func TestAutomaticCrossHostResumeAndOneFallback(t *testing.T) {
	for _, host := range []string{"claude-code", "codex"} {
		t.Run(host, func(t *testing.T) {
			env, p := setup(t)
			other := "codex"
			if host == other {
				other = "claude-code"
			}
			plant(t, env, other, "previous-session")
			p.Session = "new-session"
			p.Event = "SessionStart"
			p.Source = "startup"
			out := invoke(t, host, p, env)
			context := contextFrom(t, out)
			for _, fact := range []string{"amber-finch", "portability", "Markdown", "Windows", "[user]", "[assistant]"} {
				if !strings.Contains(context, fact) {
					t.Errorf("missing %q in %s", fact, context)
				}
			}
			p.Event = "UserPromptSubmit"
			p.Source = ""
			if out := invoke(t, host, p, env); out != "" {
				t.Fatalf("unchanged first refresh=%q", out)
			}
			if out := invoke(t, host, p, env); out != "" {
				t.Fatalf("repeat prompt=%q", out)
			}
			p.Session = "mid-session-trust"
			if out := invoke(t, host, p, env); !strings.Contains(contextFrom(t, out), "amber-finch") {
				t.Fatal("missing firstpromptfallback")
			}
			if out := invoke(t, host, p, env); out != "" {
				t.Fatalf("fallback repeated=%q", out)
			}
		})
	}
}
func TestCompactResumeAndOffRecordSuppression(t *testing.T) {
	env, p := setup(t)
	plant(t, env, "codex", p.Session)
	p.Event = "SessionStart"
	p.Source = "compact"
	if context := contextFrom(t, invoke(t, "codex", p, env)); !strings.Contains(context, "Windows") {
		t.Fatal("compact lost own latest exchanges")
	}
	p.Event = "UserPromptSubmit"
	p.Source = ""
	p.Prompt = "off the record"
	if out := invoke(t, "codex", p, env); !strings.Contains(out, "paused") {
		t.Fatal("missing offrecord confirmation")
	}
	p.Event = "SessionStart"
	p.Source = "compact"
	if out := invoke(t, "codex", p, env); out != "" {
		t.Fatalf("offrecord brief=%q", out)
	}
	p.Event = "UserPromptSubmit"
	p.Prompt = "PRIVATE"
	invoke(t, "codex", p, env)
	for _, r := range records(t, env) {
		if strings.Contains(r.Text, "PRIVATE") {
			t.Fatal("offrecord captured")
		}
	}
}
func TestHeartbeatsAndSafeMissingTranscript(t *testing.T) {
	env, p := setup(t)
	p.Event = "Stop"
	invoke(t, "codex", p, env)
	file := filepath.Join(env.Dirs.State, "heartbeats", "codex", "Stop.json")
	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	var beat map[string]any
	if json.Unmarshal(data, &beat) != nil || beat["host"] != "codex" || beat["transcript_available"] != false {
		t.Fatalf("heartbeat=%s", data)
	}
	info, _ := os.Stat(file)
	if info.Mode().Perm() != 0600 {
		t.Fatal("heartbeat not private")
	}
	value, ok, err := (session.Markers{Root: env.Dirs.Sessions()}).GetContext(context.Background(), "codex", p.Session, session.Briefed)
	if err != nil || ok || value != "" {
		t.Fatal("Stop set brief marker")
	}
}

func TestPersistedPromptIsCapturedOnlyOnce(t *testing.T) {
	env, p := setup(t)
	file := filepath.Join(os.Getenv("HOME"), ".claude", "projects", "fixture", p.Session+".jsonl")
	if err := os.MkdirAll(filepath.Dir(file), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, nil, 0600); err != nil {
		t.Fatal(err)
	}
	p.Transcript = file
	p.Event = "SessionStart"
	invoke(t, "claude-code", p, env)
	row := map[string]any{"type": "user", "sessionId": p.Session, "cwd": p.Cwd, "timestamp": env.Now().Format(time.RFC3339), "promptId": p.Turn, "message": map[string]any{"role": "user", "content": "persisted before prompt hook"}}
	data, _ := json.Marshal(row)
	if err := os.WriteFile(file, append(data, '\n'), 0600); err != nil {
		t.Fatal(err)
	}
	p.Event = "UserPromptSubmit"
	p.Prompt = "persisted before prompt hook"
	invoke(t, "claude-code", p, env)
	count := 0
	for _, r := range records(t, env) {
		if r.Role == "user" && r.Text == p.Prompt {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("prompt records=%d", count)
	}
}

func TestMissedOffRecordHookPausesNextCapture(t *testing.T) {
	env, p := setup(t)
	file := filepath.Join(os.Getenv("HOME"), ".claude", "projects", "fixture", p.Session+".jsonl")
	os.MkdirAll(filepath.Dir(file), 0700)
	os.WriteFile(file, nil, 0600)
	p.Transcript = file
	p.Event = "SessionStart"
	invoke(t, "claude-code", p, env)
	f, err := os.OpenFile(file, os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	for _, text := range []string{"Earlier text.\n  OFF THE RECORD!\n", "PRIVATE PREVIOUS PROMPT"} {
		row := map[string]any{"type": "user", "sessionId": p.Session, "cwd": p.Cwd, "timestamp": env.Now().Format(time.RFC3339), "message": map[string]any{"role": "user", "content": text}}
		data, _ := json.Marshal(row)
		f.Write(append(data, '\n'))
	}
	f.Close()
	p.Event = "UserPromptSubmit"
	p.Prompt = "PRIVATE NEXT PROMPT"
	invoke(t, "claude-code", p, env)
	for _, r := range records(t, env) {
		if strings.Contains(r.Text, "PRIVATE") || strings.Contains(r.Text, "Earlier text") {
			t.Fatalf("private captured=%#v", r)
		}
	}
	_, off, err := (session.Markers{Root: env.Dirs.Sessions()}).Get("claude-code", p.Session, session.OffRecord)
	if err != nil || !off {
		t.Fatal("missed control did not pause subsequent capture")
	}
	p.Prompt = "back on the record"
	invoke(t, "claude-code", p, env)
	p.Prompt = "public restored"
	invoke(t, "claude-code", p, env)
	found := false
	for _, r := range records(t, env) {
		found = found || r.Text == "public restored"
	}
	if !found {
		t.Fatal("back on record did not restore capture")
	}
}
