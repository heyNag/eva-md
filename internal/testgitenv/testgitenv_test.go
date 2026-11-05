package testgitenv

import (
	"os"
	"testing"
)

// Clear unsets every variable that carries git configuration and the four
// that carry a commit's identity, and keeps the ones that isolate a test's
// git and the commit dates, which only look like them.
func TestClearUnsetsOnlyGitConfigAndIdentityFromTheEnvironment(t *testing.T) {
	cleared := map[string]string{
		"GIT_CONFIG_COUNT":      "2",
		"GIT_CONFIG_KEY_0":      "url.https://github.com/.insteadOf",
		"GIT_CONFIG_VALUE_0":    "git@github.com:",
		"GIT_CONFIG_KEY_1":      "credential.interactive",
		"GIT_CONFIG_VALUE_1":    "false",
		"GIT_CONFIG_KEY_7":      "core.hooksPath",
		"GIT_CONFIG_PARAMETERS": "'user.name'='Made Up'",
		"GIT_CONFIG":            "/nonexistent/config",
		"GIT_AUTHOR_NAME":       "Made Up",
		"GIT_AUTHOR_EMAIL":      "made-up@example.invalid",
		"GIT_COMMITTER_NAME":    "Made Up",
		"GIT_COMMITTER_EMAIL":   "made-up@example.invalid",
	}
	kept := map[string]string{
		"GIT_CONFIG_GLOBAL":   "/nonexistent/gitconfig",
		"GIT_CONFIG_NOSYSTEM": "1",
		"GIT_TERMINAL_PROMPT": "0",
		"GIT_AUTHOR_DATE":     "2026-10-29T10:00:00+00:00",
		"GIT_COMMITTER_DATE":  "2026-10-29T10:00:00+00:00",
	}
	for name, value := range cleared {
		t.Setenv(name, value)
	}
	for name, value := range kept {
		t.Setenv(name, value)
	}
	Clear()
	for name := range cleared {
		if value, ok := os.LookupEnv(name); ok {
			t.Errorf("%s is still set, to %q", name, value)
		}
	}
	for name, want := range kept {
		if got := os.Getenv(name); got != want {
			t.Errorf("%s = %q, want %q", name, got, want)
		}
	}
}
