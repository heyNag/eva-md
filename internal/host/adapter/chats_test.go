package adapter

import (
	"slices"
	"testing"
)

// Each host's events, in the order v1's doctor and status list them: the
// six both hosts declare, then Claude Code's four more, or Codex's one.
func TestEventsInTheDoctorsOrder(t *testing.T) {
	want := map[ID][]string{
		ClaudeCode: {"SessionStart", "UserPromptSubmit", "PostToolUse", "Stop", "PreCompact", "SessionEnd", "PostToolUseFailure", "StopFailure", "PostCompact", "PostModelSwitch"},
		Codex:      {"SessionStart", "UserPromptSubmit", "PostToolUse", "Stop", "PreCompact", "SessionEnd", "Interrupt"},
	}
	if len(want) != len(registry) {
		t.Fatalf("the test knows %d hosts, the registry %d", len(want), len(registry))
	}
	for _, h := range All() {
		if got := h.Events(); !slices.Equal(got, want[h.ID]) {
			t.Errorf("%s's events %q, want %q", h.ID, got, want[h.ID])
		}
		rows := Rows(h.ID)
		if got := rows[len(rows)-1].Events; !slices.Equal(got, h.Events()) {
			t.Errorf("%s's events are not its newest row's: %q", h.ID, got)
		}
	}
	if got := (Host{ID: "generic"}).Events(); got != nil {
		t.Errorf("a host eva-md doesn't know has events: %q", got)
	}
}

// A change to what Events gave leaves the table alone.
func TestEventsAreACopy(t *testing.T) {
	for _, h := range All() {
		events := h.Events()
		first := events[0]
		events[0] = "Changed"
		if h.Events()[0] != first {
			t.Fatalf("changing %s's events changed the table", h.ID)
		}
	}
}

// Each host's chat folder, the variable that moves it and its layout, and
// the command that logs its CLI in, are v1's; every host has them, and a
// host eva-md doesn't know has none.
func TestChatsAndLoginHoldV1sValues(t *testing.T) {
	want := map[ID]struct {
		chats Chats
		login string
	}{
		ClaudeCode: {Chats{Folder: ".claude/projects", MovedBy: "CLAUDE_CONFIG_DIR", Layout: PerProject}, "claude auth login"},
		Codex:      {Chats{Folder: ".codex/sessions", MovedBy: "CODEX_HOME", Layout: PerDay}, "codex login"},
	}
	if len(chats) != len(registry) || len(logins) != len(registry) {
		t.Errorf("the tables have %d chats and %d logins for %d hosts", len(chats), len(logins), len(registry))
	}
	for _, h := range All() {
		w, ok := want[h.ID]
		if !ok {
			t.Errorf("the test doesn't know %s", h.ID)
			continue
		}
		if got := h.Chats(); got != w.chats {
			t.Errorf("%s's chats %+v, want %+v", h.ID, got, w.chats)
		}
		if got := h.Login(); got != w.login {
			t.Errorf("%s's login %q, want %q", h.ID, got, w.login)
		}
	}
	generic := Host{ID: "generic"}
	if got := generic.Chats(); got != (Chats{}) || got.Layout == PerProject || got.Layout == PerDay {
		t.Errorf("a host eva-md doesn't know has chats: %+v", got)
	}
	if got := generic.Login(); got != "" {
		t.Errorf("a host eva-md doesn't know has a login: %q", got)
	}
}
