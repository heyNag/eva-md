package fakehost

import (
	"bytes"
	"encoding/json"
	"io"
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
		if code := serve(h, file, c.args, &out, io.Discard); code != c.code {
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

// Each call that returns records how it ended, beside the record of the
// call: its process, its exit status and, for a fault of the fake's own,
// what failed, which it also says on stderr. A script that cannot be read
// is said on stderr, and no end is recorded, since there is no folder to
// record it in.
func TestEachCallsEndIsRecorded(t *testing.T) {
	dir := t.TempDir()
	tasks, _, err := Fixture(filepath.Join(dir, "data"), 1)
	if err != nil {
		t.Fatal(err)
	}
	record := filepath.Join(dir, "record")
	if err := os.Mkdir(record, 0o755); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(dir, "script.json")
	script := func(s Script) {
		t.Helper()
		s.Dir = record
		data, _ := json.Marshal(s)
		if err := os.WriteFile(file, data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	t.Chdir(t.TempDir())
	h, _ := adapter.ByID(string(adapter.ClaudeCode))
	start := []string{"-p", "--", "a made-up prompt"}
	cases := []struct {
		script Script
		args   []string
		exit   int
		fault  string
	}{
		{Script{Task: filepath.Join(tasks, FixtureID)}, []string{"--version"}, 0, ""},
		{Script{Task: filepath.Join(tasks, FixtureID), SignIn: true}, start, 1, ""},
		{Script{Task: filepath.Join(tasks, FixtureID)}, start, 0, ""},
		{Script{Task: filepath.Join(dir, "no-such-task")}, start, 3, "laying the overlay: "},
	}
	for i, c := range cases {
		script(c.script)
		var out, errOut bytes.Buffer
		if code := serve(h, file, c.args, &out, &errOut); code != c.exit {
			t.Fatalf("%d: exit %d, want %d", i, code, c.exit)
		}
		calls, err := Read(record)
		if err != nil || len(calls) != i+1 || calls[i].Pid != os.Getpid() {
			t.Fatalf("%d: calls %+v %v", i, calls, err)
		}
		ends, err := ReadEnds(record)
		if err != nil || len(ends) != i+1 {
			t.Fatalf("%d: ends %+v %v", i, ends, err)
		}
		e := ends[i]
		if e.Pid != calls[i].Pid || e.Exit != c.exit || !strings.HasPrefix(e.Fault, c.fault) || (c.fault == "") != (e.Fault == "") {
			t.Fatalf("%d: end %+v", i, e)
		}
		if said := strings.Contains(errOut.String(), "fakehost: "+c.fault); said != (c.fault != "") {
			t.Fatalf("%d: stderr %q", i, errOut.String())
		}
	}
	if err := os.WriteFile(file, []byte("not a script"), 0o644); err != nil {
		t.Fatal(err)
	}
	var out, errOut bytes.Buffer
	if code := serve(h, file, start, &out, &errOut); code != 3 || !strings.Contains(errOut.String(), "fakehost: reading the script: ") {
		t.Fatalf("exit %d, stderr %q", code, errOut.String())
	}
	if ends, err := ReadEnds(record); err != nil || len(ends) != len(cases) {
		t.Fatalf("ends %+v %v", ends, err)
	}
}
