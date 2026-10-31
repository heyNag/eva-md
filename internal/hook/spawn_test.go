package hook

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
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
)

// hookProcess, set to 1, makes the test binary run one hook instead of the
// tests: the real-exec test needs a hook process that exits while the job
// it started goes on.
const hookProcess = "EVA_MD_TEST_HOOK_PROCESS"

func TestMain(m *testing.M) {
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
	return Run(context.Background(), "claude-code", os.Stdin, os.Stdout, Env{Dirs: dirs, Config: c, Getenv: os.Getenv, Now: time.Now, slack: busySlack})
}

// standIn takes the installed binary's place in the real-exec test. It
// records how it was started, writes a line to stdout and to stderr, then
// stays alive until the test releases it (30 seconds at most), so the test
// can see it outlive the hook that started it.
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
while [ ! -f "$out/release" ] && [ $i -lt 300 ]; do sleep 0.1; i=$((i + 1)); done
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
// an executable stand-in; it never runs, and no update log is opened.
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
	if _, ok := waitFor(ran, 500*time.Millisecond); ok {
		t.Fatal("a binary named by a relative share folder ran")
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
	// Release the stand-in whatever happens, before the temporary folders go.
	t.Cleanup(func() {
		_ = os.MkdirAll(out, 0o700)
		_ = os.WriteFile(filepath.Join(out, "release"), nil, 0o600)
		waitFor(filepath.Join(out, "done"), 10*time.Second)
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
		case <-time.After(15 * time.Second):
			t.Fatal("the hook's stdout or stderr stayed open: the job holds the hook's pipes")
		}
	}
	closed := time.Since(started)
	if err := cmd.Wait(); err != nil {
		t.Fatalf("hook process: %v", err)
	}
	pidText, ok := waitFor(filepath.Join(out, "pid"), 10*time.Second)
	if !ok {
		t.Fatal("the stand-in never started")
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
	if closed > 10*time.Second {
		t.Fatalf("the hook's output took %v to end", closed)
	}
	// Its own session: setsid made it the leader, so its session id is its
	// pid, not the test's.
	sid, err := syscall.Getsid(pid)
	own, _ := syscall.Getsid(0)
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
	if _, done := waitFor(filepath.Join(out, "done"), 10*time.Second); !done {
		t.Fatal("the job did not outlive the hook")
	}
}
