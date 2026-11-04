package hook

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/heyNag/eva-md/internal/config"
	"github.com/heyNag/eva-md/internal/paths"
	"github.com/heyNag/eva-md/internal/testlimits"
)

// hookProcess, set to 1, makes the test binary run one hook instead of the
// tests: the real-exec test needs a hook process that exits while the job
// it started goes on.
const hookProcess = "EVA_MD_TEST_HOOK_PROCESS"

// Tests check what a hook stores, not how fast the machine runs git: the
// git limits are raised for every test, before any test runs, and before a
// hook process runs its hook, so the real-exec test's hook gets them too.
func TestMain(m *testing.M) {
	testlimits.Raise()
	if os.Getenv(hookProcess) == "1" {
		os.Exit(runHookProcess())
	}
	os.Exit(m.Run())
}

// runHookProcess runs one Claude Code hook on stdin and stdout with the
// real spawn, as the installed binary does, for a config naming a wiki.
func runHookProcess() int {
	dirs, err := paths.Default()
	if err != nil {
		return 1
	}
	c := config.Default()
	c.WikiPath = filepath.Join(os.Getenv("HOME"), "wiki")
	return Run(context.Background(), "claude-code", os.Stdin, os.Stdout, Env{Dirs: dirs, Config: c, Getenv: os.Getenv, Now: time.Now, Slack: busySlack})
}

// standIn takes the installed binary's place in the real-exec test. It
// records how it was started, writes a line to stdout and to stderr, then
// stays alive until the test releases it (two minutes at least, longer than
// any wait of the test), so the test can see it outlive the hook that
// started it.
const standIn = `#!/bin/sh
out=$HOME/stand-in
mkdir -p "$out"
printf '%s\n' "$@" > "$out/args"
if [ /dev/fd/0 -ef /dev/null ]; then echo null > "$out/stdin"; else echo other > "$out/stdin"; fi
env > "$out/env"
pwd -P > "$out/pwd"
echo "stand-in stdout"
echo "stand-in stderr" >&2
echo $$ > "$out/pid.tmp" && mv "$out/pid.tmp" "$out/pid"
i=0
while [ ! -f "$out/release" ] && [ $i -lt 1200 ]; do sleep 0.1; i=$((i + 1)); done
echo done > "$out/done"
`

// waitFor polls for a file until it exists or the time is up.
func waitFor(file string, limit time.Duration) ([]byte, bool) {
	for end := time.Now().Add(limit); ; time.Sleep(20 * time.Millisecond) {
		if data, err := os.ReadFile(file); err == nil {
			return data, true
		}
		if time.Now().After(end) {
			return nil, false
		}
	}
}

// detach starts nothing for a share folder that is not an absolute path,
// which would name a binary by the folder the hook runs in. Here the hook
// runs in /, where the share folder's path without its leading slash names
// an executable stand-in. detach returns an error and opens no update log,
// and as it opens the log before it starts any job, nothing was started.
func TestDetachNeedsAnAbsoluteShareFolder(t *testing.T) {
	env, _ := setup(t)
	empty := filepath.Join(t.TempDir(), "bin")
	if err := os.Mkdir(empty, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", empty+":/usr/bin:/bin")
	ran := filepath.Join(os.Getenv("HOME"), "ran")
	binary := filepath.Join(env.Dirs.Share, "bin", "eva-md")
	if err := os.MkdirAll(filepath.Dir(binary), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(binary, []byte("#!/bin/sh\necho ran > '"+ran+"'\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Chdir("/")
	for _, share := range []string{strings.TrimPrefix(env.Dirs.Share, "/"), ""} {
		dirs := env.Dirs
		dirs.Share = share
		if err := detach(dirs, time.Now(), "update", "--session", "codex:test-session"); err == nil {
			t.Fatalf("share folder %q: detach started a job", share)
		}
	}
	if _, err := os.Stat(env.Dirs.Logs()); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("the update log folder exists: %v", err)
	}
}

// The real spawn, from a hook process: SessionEnd's update job runs in a
// session of its own, with stdin from /dev/null, its stdout and stderr in
// the day's update log (0600), the update allowlist with EVA_MD_INNER=1 and
// / as its folder. The hook's own stdout and stderr end as soon as the hook
// exits, while the job is still running, and the job goes on after it.
func TestDetachedJobOutlivesTheHook(t *testing.T) {
	env, p := setup(t)
	home := os.Getenv("HOME")
	// Only the system folders and an empty one are on PATH, so nothing the
	// hook starts could find a real claude or codex.
	empty := filepath.Join(t.TempDir(), "bin")
	if err := os.Mkdir(empty, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", empty+":/usr/bin:/bin")
	t.Setenv("ANTHROPIC_API_KEY", "test-made-up-key")
	t.Setenv("CLAUDECODE", "1")
	binary := filepath.Join(env.Dirs.Share, "bin", "eva-md")
	if err := os.MkdirAll(filepath.Dir(binary), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(binary, []byte(standIn), 0o700); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(home, "stand-in")
	// Release the stand-in whatever happens, and once the hook has started
	// it, which opens the update log first, wait for it to end before the
	// temporary folders go.
	t.Cleanup(func() {
		_ = os.MkdirAll(out, 0o700)
		_ = os.WriteFile(filepath.Join(out, "release"), nil, 0o600)
		if logs, _ := filepath.Glob(filepath.Join(env.Dirs.Logs(), "update-*.log")); len(logs) > 0 {
			waitFor(filepath.Join(out, "done"), hangGuard)
		}
	})

	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	p.Event = "SessionEnd"
	payload, _ := json.Marshal(p)
	cmd := exec.Command(exe)
	cmd.Env = append(os.Environ(), hookProcess+"=1")
	cmd.Stdin = bytes.NewReader(payload)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	outputs := make(chan []byte, 2)
	for _, r := range []io.Reader{stdout, stderr} {
		go func() { data, _ := io.ReadAll(r); outputs <- data }()
	}
	var hookOut []byte
	for range 2 {
		select {
		case data := <-outputs:
			hookOut = append(hookOut, data...)
		case <-time.After(hangGuard):
			t.Fatalf("the hook's stdout or stderr was still open after %v: the hook hung, or the job holds its pipes", hangGuard)
		}
	}
	t.Logf("the hook's output ended after %v", time.Since(started))
	if err := cmd.Wait(); err != nil {
		t.Fatalf("hook process: %v", err)
	}
	// The hook opens the day's update log before it starts the job, and
	// starts it before it exits: without a log, no job is coming.
	if logs, _ := filepath.Glob(filepath.Join(env.Dirs.Logs(), "update-*.log")); len(logs) == 0 {
		t.Fatalf("the hook started no job: %s", hookRecord(env, hookOut))
	}
	pidText, ok := waitFor(filepath.Join(out, "pid"), hangGuard)
	if !ok {
		t.Fatalf("the stand-in never started in %v: %s", hangGuard, hookRecord(env, hookOut))
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(pidText)))
	if err != nil {
		t.Fatal(err)
	}
	if _, done := waitFor(filepath.Join(out, "done"), 0); done || syscall.Kill(pid, 0) != nil {
		t.Fatal("the job ended before the hook's output was read")
	}
	if len(hookOut) != 0 {
		t.Fatalf("the hook printed %q", hookOut)
	}
	// Its own session: setsid made it the leader, so its session id is its
	// pid, not the test's.
	sid, err := getsid(pid)
	own, _ := getsid(0)
	if err != nil || sid != pid || sid == own {
		t.Fatalf("session %d (%v) for pid %d; the test's is %d", sid, err, pid, own)
	}
	read := func(name string) string {
		data, err := os.ReadFile(filepath.Join(out, name))
		if err != nil {
			t.Fatal(err)
		}
		return string(data)
	}
	if got := read("args"); got != "update\n--session\nclaude-code:test-session\n" {
		t.Fatalf("args %q", got)
	}
	if got := read("stdin"); got != "null\n" {
		t.Fatalf("stdin is %q, not /dev/null", got)
	}
	if got := read("pwd"); got != "/\n" {
		t.Fatalf("folder %q", got)
	}
	vars := map[string]string{}
	for _, line := range strings.Split(strings.TrimSpace(read("env")), "\n") {
		if key, value, ok := strings.Cut(line, "="); ok {
			vars[key] = value
		}
	}
	for _, dropped := range []string{"ANTHROPIC_API_KEY", "CLAUDECODE", "GIT_CONFIG_GLOBAL", hookProcess} {
		if _, ok := vars[dropped]; ok {
			t.Errorf("the job inherited %s", dropped)
		}
	}
	if vars["EVA_MD_INNER"] != "1" || vars["HOME"] != home || vars["PATH"] != empty+":/usr/bin:/bin" {
		t.Fatalf("the job's environment is not the update allowlist: %v", vars)
	}
	logs, _ := filepath.Glob(filepath.Join(env.Dirs.Logs(), "update-*.log"))
	if len(logs) != 1 {
		t.Fatalf("logs %v", logs)
	}
	info, err := os.Stat(logs[0])
	logged, _ := os.ReadFile(logs[0])
	if err != nil || info.Mode().Perm() != 0o600 || !strings.Contains(string(logged), "stand-in stdout\n") || !strings.Contains(string(logged), "stand-in stderr\n") {
		t.Fatalf("log %s %v: %q", logs[0], err, logged)
	}
	// The hook has exited; the job goes on and finishes once released.
	if err := os.WriteFile(filepath.Join(out, "release"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, done := waitFor(filepath.Join(out, "done"), hangGuard); !done {
		t.Fatalf("the job did not outlive the hook: it had not finished %v after its release", hangGuard)
	}
}

// hookRecord says what the real-exec test's hook left, for a job that never
// started: what it printed, the inbox's row count, its SessionEnd heartbeat,
// whose runs say whether it failed, and the update log.
func hookRecord(env Env, printed []byte) string {
	files, _ := filepath.Glob(filepath.Join(env.Dirs.Inbox(), "*", "*.jsonl"))
	rows := 0
	for _, file := range files {
		data, _ := os.ReadFile(file)
		rows += bytes.Count(data, []byte("\n"))
	}
	beat, err := os.ReadFile(filepath.Join(env.Dirs.State, "heartbeats", "claude-code", "SessionEnd.json"))
	if err != nil {
		beat = []byte(err.Error())
	}
	logs, _ := filepath.Glob(filepath.Join(env.Dirs.Logs(), "update-*.log"))
	var logged []byte
	for _, log := range logs {
		data, _ := os.ReadFile(log)
		logged = append(logged, data...)
	}
	return fmt.Sprintf("the hook printed %q; the inbox holds %d rows; its SessionEnd heartbeat: %s; the update logs %q hold %q", printed, rows, beat, logs, logged)
}
