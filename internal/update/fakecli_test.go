package update

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// fakeScript stands in for claude and codex. It works in $HOME/fake, the
// test's dump folder. Run n records its arguments (NUL-separated), stdin,
// environment and physical working folder there as args.n, stdin.n, env.n
// and pwd.n, and copies the schema file it was given to schema.n. It copies
// output.n to the file after -o, writes stderr.n and stdout.n, runs the
// shell lines in extra, and exits with the code in exit.n. Each of these
// falls back to the file without ".n".
const fakeScript = `#!/bin/sh
dump=$HOME/fake
n=0
if [ -f "$dump/count" ]; then read n < "$dump/count"; fi
n=$((n + 1))
echo "$n" > "$dump/count"
printf '%s\0' "$@" > "$dump/args.$n"
cat > "$dump/stdin.$n"
env > "$dump/env.$n"
pwd -P > "$dump/pwd.$n"
out=
schema=
while [ $# -gt 0 ]; do
	case $1 in
	-o) out=$2; shift ;;
	--output-schema) schema=$2; shift ;;
	esac
	shift
done
pick() { f=; if [ -f "$dump/$1.$n" ]; then f=$dump/$1.$n; elif [ -f "$dump/$1" ]; then f=$dump/$1; fi; }
if [ -n "$schema" ]; then cp "$schema" "$dump/schema.$n"; fi
pick output; if [ -n "$f" ] && [ -n "$out" ]; then cp "$f" "$out"; fi
pick stderr; if [ -s "$f" ]; then cat "$f" >&2; fi
pick stdout; if [ -s "$f" ]; then cat "$f"; fi
if [ -f "$dump/extra" ]; then . "$dump/extra"; fi
pick exit; if [ -n "$f" ]; then read code < "$f"; exit "$code"; fi
exit 0
`

// fakeBin holds the fake claude and codex, written once for all the tests:
// macOS checks a new executable on its first run, which costs about 0.3 s a
// time when each test writes its own.
var fakeBin string

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "eva-md-update-test-")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fakeBin = filepath.Join(dir, "bin")
	claude, codex := filepath.Join(fakeBin, "claude"), filepath.Join(fakeBin, "codex")
	err = os.Mkdir(fakeBin, 0o700)
	if err == nil {
		err = os.WriteFile(claude, []byte(fakeScript), 0o700)
	}
	if err == nil && os.Link(claude, codex) != nil {
		err = os.WriteFile(codex, []byte(fakeScript), 0o700)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		_ = os.RemoveAll(dir)
		os.Exit(1)
	}
	code := m.Run()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}

// Canned host output. Every value is made up.
const (
	answer        = `{"summary":"done"}`
	claudeSuccess = `{"type":"result","subtype":"success","is_error":false,"duration_ms":1200,"num_turns":1,"result":"","session_id":"00000000-0000-4000-8000-000000000000","usage":{"input_tokens":120,"cache_creation_input_tokens":30,"cache_read_input_tokens":50,"output_tokens":40},"permission_denials":[],"structured_output":{"summary":"done"}}`
	codexClean    = `{"type":"thread.started","thread_id":"0199a000-0000-7000-8000-000000000000"}
{"type":"turn.started"}
{"type":"item.completed","item":{"id":"item_0","type":"reasoning","text":"Reading the batch"}}
{"type":"item.completed","item":{"id":"item_1","type":"agent_message","text":"{\"summary\":\"done\"}"}}
{"type":"turn.completed","usage":{"input_tokens":500,"cached_input_tokens":100,"output_tokens":60}}
`
	// The batch marker must reach stdin and appear in no argument.
	batchMarker = "BATCH-MARKER-7"
)

var hosts = []string{HostClaude, HostCodex}

// fakeCLI readies the fake CLIs for this test and returns their dump folder.
// extra, when set, holds shell lines the fake runs before it exits.
func fakeCLI(t *testing.T, extra string) string {
	t.Helper()
	dump := filepath.Join(os.Getenv("HOME"), "fake")
	if err := os.Mkdir(dump, 0o700); err != nil {
		t.Fatal(err)
	}
	if extra != "" {
		put(t, dump, "extra", extra)
	}
	return dump
}

// fakeHost readies the fake CLIs to succeed as host with canned output.
func fakeHost(t *testing.T, host string) string {
	t.Helper()
	dump := fakeCLI(t, "")
	if host == HostClaude {
		put(t, dump, "stdout", claudeSuccess)
	} else {
		put(t, dump, "stdout", codexClean)
		put(t, dump, "output", answer)
	}
	return dump
}

func put(t *testing.T, dump, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dump, name), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func read(t *testing.T, dump, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dump, name))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// runs is how many times the fake CLI ran.
func runs(t *testing.T, dump string) int {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dump, "count"))
	if errors.Is(err, os.ErrNotExist) {
		return 0
	}
	if err != nil {
		t.Fatal(err)
	}
	n, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		t.Fatal(err)
	}
	return n
}

func argsOf(t *testing.T, dump string, n int) []string {
	t.Helper()
	parts := strings.Split(read(t, dump, fmt.Sprintf("args.%d", n)), "\x00")
	return parts[:len(parts)-1]
}

func envOf(t *testing.T, dump string, n int) map[string]string {
	t.Helper()
	env := map[string]string{}
	for _, line := range strings.Split(read(t, dump, fmt.Sprintf("env.%d", n)), "\n") {
		if key, value, ok := strings.Cut(line, "="); ok {
			env[key] = value
		}
	}
	return env
}

// request is a run for host with made-up values and an empty run folder.
func request(t *testing.T, host string) Request {
	t.Helper()
	model := "gpt-test-model"
	if host == HostClaude {
		model = "claude-test-model"
	}
	return Request{
		Host:   host,
		Model:  model,
		Effort: "highest",
		Rules:  "Fixed rules for the test.\nReturn the JSON only.",
		Input:  []byte("[1] user 2026-10-31 10:00: " + batchMarker + " plan the release\n"),
		Schema: []byte(`{"type":"object","properties":{"summary":{"type":"string"}},"required":["summary"],"additionalProperties":false}`),
		RunDir: t.TempDir(),
	}
}

// waitGone waits for the process recorded in file to be gone.
func waitGone(t *testing.T, dump, file string) {
	t.Helper()
	pid, err := strconv.Atoi(strings.TrimSpace(read(t, dump, file)))
	if err != nil || pid <= 1 {
		t.Fatalf("no child pid recorded: %v", err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for syscall.Kill(pid, 0) == nil {
		if time.Now().After(deadline) {
			_ = syscall.Kill(pid, syscall.SIGKILL)
			t.Fatalf("child %d outlived the run", pid)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// waitFile waits for the fake CLI to write file.
func waitFile(dump, file string) bool {
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(filepath.Join(dump, file)); err == nil {
			return true
		}
		time.Sleep(20 * time.Millisecond)
	}
	return false
}
