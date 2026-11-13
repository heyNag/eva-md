package fakehost

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/heyNag/eva-md/internal/host/adapter"
)

// A call that is neither --version nor a headless chat in the host table's
// shape is recorded as other and refused, never taken for a chat.
func TestOtherCallsAreRefused(t *testing.T) {
	dir := t.TempDir()
	tasks, _, err := Fixture(filepath.Join(dir, "data"), 1)
	if err != nil {
		t.Fatal(err)
	}
	record := filepath.Join(dir, "record")
	if err := os.Mkdir(record, 0o755); err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(Script{Dir: record, Task: filepath.Join(tasks, FixtureID)})
	file := filepath.Join(dir, "script.json")
	if err := os.WriteFile(file, data, 0o644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(t.TempDir())
	cases := []struct {
		host adapter.ID
		args []string
		code int
		mode string
	}{
		{adapter.ClaudeCode, []string{"plugin", "list", "--json"}, 2, "other"},
		{adapter.Codex, []string{"plugin", "list"}, 2, "other"},
		{adapter.Codex, []string{"exec"}, 2, "other"},
		{adapter.Codex, []string{"exec", "--json", "a made-up prompt"}, 2, "other"},
		{adapter.ClaudeCode, []string{"-p", "a made-up prompt"}, 2, "other"},
		{adapter.ClaudeCode, []string{"-p", "--", "a", "made-up prompt"}, 2, "other"},
		{adapter.ClaudeCode, []string{"--version"}, 0, "version"},
		{adapter.Codex, []string{"exec", "--json", "--", "-a made-up prompt"}, 0, "start"},
		{adapter.ClaudeCode, []string{"-p", "--", "--resume"}, 0, "start"},
	}
	for i, c := range cases {
		h, _ := adapter.ByID(string(c.host))
		var out bytes.Buffer
		if code := serve(h, file, c.args, &out); code != c.code {
			t.Fatalf("%v: exit %d, want %d", c.args, code, c.code)
		}
		calls, err := Read(record)
		if err != nil || len(calls) != i+1 || calls[i].Mode != c.mode {
			t.Fatalf("%v: %+v %v", c.args, calls, err)
		}
		if c.mode == "other" && out.Len() != 0 {
			t.Fatalf("%v answered %q", c.args, out.String())
		}
		// The prompt is the argument after "--", even one that looks like a
		// flag, and is never read as one.
		if c.mode == "start" && (calls[i].Prompt != c.args[len(c.args)-1] || !strings.HasPrefix(calls[i].Session, "fake-session-")) {
			t.Fatalf("%v: %+v", c.args, calls[i])
		}
	}
}
