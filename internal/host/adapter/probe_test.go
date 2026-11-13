package adapter

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
)

// hangGuard bounds a wait that only a broken probe would make long.
const hangGuard = 60 * time.Second

// fakeCLI writes a shell script named name in dir that runs body.
func fakeCLI(t *testing.T, dir, name, body string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body+"\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	return path
}

// lookIn finds a command in dir only, as PATH holding dir alone would.
func lookIn(dir string) func(string) (string, error) {
	return func(file string) (string, error) { return exec.LookPath(filepath.Join(dir, file)) }
}

// realDir is a new temporary folder by the path the system resolves it to.
func realDir(t *testing.T) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return dir
}

// A fake CLI that prints a made-up version, probed below the lowest row, in
// a row and above the tested maximum, and one that prints no version.
func TestProbeFindsTheRowForTheVersion(t *testing.T) {
	for _, c := range []struct {
		id              ID
		cli, out        string
		version, lowest string
		known, untested bool
	}{
		{ClaudeCode, "claude", "2.1.300 (Claude Code)", "2.1.300", "", false, false},
		{ClaudeCode, "claude", "2.1.314 (Claude Code)", "2.1.314", "2.1.311", true, false},
		{ClaudeCode, "claude", "2.1.400 (Claude Code)", "2.1.400", "2.1.311", false, true},
		{Codex, "codex", "codex-cli 0.150.0", "0.150.0", "", false, false},
		{Codex, "codex", "codex-cli 0.165.0", "0.165.0", "0.164.0", true, false},
		{Codex, "codex", "codex-cli 0.170.2", "0.170.2", "0.164.0", false, true},
		{Codex, "codex", "codex-cli (a development build)", "", "", false, false},
		{ClaudeCode, "claude", "", "", "", false, false},
	} {
		dir := realDir(t)
		cli := fakeCLI(t, dir, c.cli, "echo '"+c.out+"'")
		p := Probe(context.Background(), c.id, Env{LookPath: lookIn(dir)})
		lowest := ""
		if p.Row != nil {
			lowest = p.Row.Lowest
			if p.Row.Host != c.id {
				t.Errorf("%q: the row is %s's", c.out, p.Row.Host)
			}
		}
		if p.Host != c.id || p.CLI != cli || p.Version != c.version || lowest != c.lowest || p.Known != c.known || p.Untested != c.untested {
			t.Errorf("%s printing %q: %+v", c.id, c.out, p)
		}
	}
}

// The probe runs <cli> --version from /, and finds the CLI through PATH with
// its folder's symlinks resolved, keeping the CLI's own name.
func TestProbeRunsVersionFromRootThroughResolvedFolders(t *testing.T) {
	dir := realDir(t)
	fakeCLI(t, dir, "codex-real", `[ "$(pwd -P)" = / ] && [ "$1" = --version ] && [ $# = 1 ] && echo 'codex-cli 0.165.0'`)
	link := filepath.Join(realDir(t), "bin")
	if err := os.Symlink(dir, link); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(dir, "codex-real"), filepath.Join(dir, "codex")); err != nil {
		t.Fatal(err)
	}
	p := Probe(context.Background(), Codex, Env{LookPath: lookIn(link)})
	if p.CLI != filepath.Join(dir, "codex") || p.Version != "0.165.0" || !p.Known {
		t.Fatalf("probe through a symlinked folder: %+v", p)
	}
}

// With no CLI on PATH, or for a host eva-md doesn't know, the probe finds
// nothing and runs nothing.
func TestProbeWithoutACLI(t *testing.T) {
	ran := false
	env := Env{LookPath: lookIn(realDir(t)), Run: func(context.Context, string, ...string) ([]byte, error) {
		ran = true
		return []byte("1.0.0"), nil
	}}
	if p := Probe(context.Background(), Codex, env); p != (ProbeResult{Host: Codex}) {
		t.Errorf("probe with no CLI: %+v", p)
	}
	looked := false
	env.LookPath = func(string) (string, error) { looked = true; return "", errors.New("not looked for") }
	if p := Probe(context.Background(), "generic", env); p != (ProbeResult{Host: "generic"}) || looked {
		t.Errorf("probe of an unknown host: %+v, looked %v", p, looked)
	}
	if ran {
		t.Error("a command ran")
	}
}

// counting is the probe's own runner, counting its runs.
func counting(n *atomic.Int32) func(context.Context, string, ...string) ([]byte, error) {
	return func(ctx context.Context, cli string, args ...string) ([]byte, error) {
		n.Add(1)
		return native(probeWaitDelay)(ctx, cli, args...)
	}
}

// A second probe of an unchanged CLI runs nothing; once its modification
// time or size changes, it runs again and reads the new version.
func TestProbeCachesByPathSizeAndModificationTime(t *testing.T) {
	dir := realDir(t)
	cli := fakeCLI(t, dir, "claude", "echo '2.1.314 (Claude Code)'")
	var n atomic.Int32
	env := Env{LookPath: lookIn(dir), Run: counting(&n)}
	probe := func(want string, runs int32) {
		t.Helper()
		if p := Probe(context.Background(), ClaudeCode, env); p.Version != want {
			t.Fatalf("version %q, want %q", p.Version, want)
		}
		if got := n.Load(); got != runs {
			t.Fatalf("%d runs, want %d", got, runs)
		}
	}
	probe("2.1.314", 1)
	probe("2.1.314", 1)
	// The same size, a new version: unseen while the modification time holds.
	info, err := os.Stat(cli)
	if err != nil {
		t.Fatal(err)
	}
	fakeCLI(t, dir, "claude", "echo '2.1.315 (Claude Code)'")
	if err = os.Chtimes(cli, info.ModTime(), info.ModTime()); err != nil {
		t.Fatal(err)
	}
	probe("2.1.314", 1)
	moved := info.ModTime().Add(-time.Hour)
	if err = os.Chtimes(cli, moved, moved); err != nil {
		t.Fatal(err)
	}
	probe("2.1.315", 2)
	probe("2.1.315", 2)
	// A new size with the modification time kept.
	fakeCLI(t, dir, "claude", "echo '2.1.316  (Claude Code)'")
	if err = os.Chtimes(cli, moved, moved); err != nil {
		t.Fatal(err)
	}
	probe("2.1.316", 3)
}

// A run that fails is not kept, so the next probe runs again.
func TestProbeKeepsNoFailedRun(t *testing.T) {
	dir := realDir(t)
	fakeCLI(t, dir, "codex", "echo 'codex-cli 0.165.0'; exit 3")
	var n atomic.Int32
	env := Env{LookPath: lookIn(dir), Run: counting(&n)}
	for want := int32(1); want <= 2; want++ {
		if p := Probe(context.Background(), Codex, env); p.Version != "" || p.Known || p.Row != nil {
			t.Fatalf("probe of a failing CLI: %+v", p)
		}
		if n.Load() != want {
			t.Fatalf("%d runs, want %d", n.Load(), want)
		}
	}
}

// A CLI that never exits ends when the context is cancelled, and the probe
// gives no version. The CLI says it has started through a FIFO, so the test
// cancels only once it runs.
func TestProbeEndsWhenCancelled(t *testing.T) {
	dir := realDir(t)
	fifo := filepath.Join(dir, "started")
	if err := syscall.Mkfifo(fifo, 0o600); err != nil {
		t.Fatal(err)
	}
	// The CLI blocks until it is killed; nothing waits on its clock.
	fakeCLI(t, dir, "claude", "echo started > '"+fifo+"'\nexec sleep 3600")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan ProbeResult, 1)
	go func() { done <- Probe(ctx, ClaudeCode, Env{LookPath: lookIn(dir)}) }()
	started := make(chan error, 1)
	go func() {
		data, err := os.ReadFile(fifo)
		if err == nil && string(data) != "started\n" {
			err = errors.New("unexpected start line")
		}
		started <- err
	}()
	select {
	case err := <-started:
		if err != nil {
			t.Fatal(err)
		}
	case p := <-done:
		t.Fatalf("the probe ended before the CLI started: %+v", p)
	case <-time.After(hangGuard):
		t.Fatal("the CLI never started")
	}
	select {
	case p := <-done:
		t.Fatalf("the probe ended before it was cancelled: %+v", p)
	default:
	}
	cancel()
	select {
	case p := <-done:
		if p.Version != "" || p.Known || p.Untested || p.Row != nil || p.CLI != filepath.Join(dir, "claude") {
			t.Fatalf("a cancelled probe: %+v", p)
		}
	case <-time.After(hangGuard):
		t.Fatal("the probe didn't end when cancelled")
	}
}

// probeWithin runs a probe with the given limits and gives its result, or
// fails the test if it hasn't ended within the hang guard.
func probeWithin(t *testing.T, id ID, dir string, lim limits) ProbeResult {
	t.Helper()
	done := make(chan ProbeResult, 1)
	go func() { done <- probe(context.Background(), id, Env{LookPath: lookIn(dir)}, lim) }()
	select {
	case p := <-done:
		return p
	case <-time.After(hangGuard):
		t.Fatal("the probe didn't end")
		return ProbeResult{}
	}
}

// A CLI that prints a version and then never exits ends when the probe's
// time limit does, though the caller's context never ends, and gives no
// version. The limit is this probe's alone; the package's stays raised.
func TestProbeEndsAtItsTimeLimit(t *testing.T) {
	dir := realDir(t)
	// The CLI blocks until it is killed; nothing waits on its clock.
	fakeCLI(t, dir, "claude", "echo '2.1.314 (Claude Code)'\nexec sleep 3600")
	p := probeWithin(t, ClaudeCode, dir, limits{timeout: 100 * time.Millisecond, waitDelay: probeWaitDelay})
	if p.Version != "" || p.Known || p.Row != nil || p.CLI != filepath.Join(dir, "claude") {
		t.Fatalf("a probe past its time limit: %+v", p)
	}
}

// A CLI that exits but leaves a child holding its output open is waited for
// no longer than the probe's wait delay, though its time limit is far off.
func TestProbeEndsAtItsWaitDelay(t *testing.T) {
	dir := realDir(t)
	pidFile := filepath.Join(dir, "child.pid")
	fakeCLI(t, dir, "codex", "sleep 3600 &\necho $! > '"+pidFile+"'\necho 'codex-cli 0.165.0'")
	// The CLI writes the pid as soon as it forks its child, which may not yet
	// have become the sleep: until it does, ps names it by the CLI's own
	// command line. Either is this test's child.
	t.Cleanup(func() { killSleep(pidFile, "sleep 3600", "/bin/sh "+filepath.Join(dir, "codex")+" --version") })
	p := probeWithin(t, Codex, dir, limits{timeout: probeTimeout, waitDelay: 100 * time.Millisecond})
	if p.Version != "" || p.Known || p.CLI != filepath.Join(dir, "codex") {
		t.Fatalf("a probe whose CLI left its output open: %+v", p)
	}
	if _, err := os.Stat(pidFile); err != nil {
		t.Fatalf("the CLI never started its child: %v", err)
	}
}

// killSleep kills the process pidFile names, but only while ps still names
// its command as one of own, the commands that are the test's own child, so
// a pid that has ended and been taken by another process is never killed.
// It reports whether it sent the kill.
func killSleep(pidFile string, own ...string) bool {
	data, err := os.ReadFile(pidFile)
	if err != nil {
		return false
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil || pid <= 0 {
		return false
	}
	out, err := exec.Command("ps", "-o", "command=", "-p", strconv.Itoa(pid)).Output()
	if err != nil || !slices.Contains(own, strings.TrimSpace(string(out))) {
		return false
	}
	return syscall.Kill(pid, syscall.SIGKILL) == nil
}

// The cleanup of TestProbeEndsAtItsWaitDelay kills the child its pid file
// names, whether that child has become the sleep or is still the fake CLI's
// shell, and nothing when the file names another process: here the test's
// own, which a kill would end, failing the run.
func TestKillSleepKillsOnlyThatSleep(t *testing.T) {
	dir := realDir(t)
	pidFile := filepath.Join(dir, "child.pid")
	// The fake CLI waits on its input, which the test holds open, so ps names
	// it by its own command line, as it names a child that has forked from
	// it and not yet become the sleep.
	cli := fakeCLI(t, dir, "codex", "read line")
	own := []string{"sleep 3600", "/bin/sh " + cli + " --version"}
	writePid := func(pid int) {
		t.Helper()
		if err := os.WriteFile(pidFile, []byte(strconv.Itoa(pid)+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	wantKilled := func(cmd *exec.Cmd) {
		t.Helper()
		err := cmd.Wait()
		var ee *exec.ExitError
		if !errors.As(err, &ee) || ee.Sys().(syscall.WaitStatus).Signal() != syscall.SIGKILL {
			t.Fatalf("%s ended with %v; want a kill", cmd.Path, err)
		}
	}

	writePid(os.Getpid())
	if killSleep(pidFile, own...) {
		t.Fatal("killed the process of the test, which is not the sleep")
	}

	sleep := exec.Command("sleep", "3600")
	if err := sleep.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sleep.Process.Kill() })
	writePid(sleep.Process.Pid)
	if killSleep(pidFile, "sleep 60") {
		t.Fatal("killed a sleep whose command is not the one asked for")
	}
	if !killSleep(pidFile, own...) {
		t.Fatal("did not kill the sleep the pid file names")
	}
	wantKilled(sleep)

	shell := exec.Command(cli, "--version")
	stdin, err := shell.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := shell.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = stdin.Close(); _ = shell.Process.Kill() })
	writePid(shell.Process.Pid)
	if killSleep(pidFile, "sleep 3600") {
		t.Fatal("killed the fake CLI's shell though only the sleep was asked for")
	}
	if !killSleep(pidFile, own...) {
		out, _ := exec.Command("ps", "-o", "command=", "-p", strconv.Itoa(shell.Process.Pid)).Output()
		t.Fatalf("did not kill the fake CLI's shell, which ps names %q", strings.TrimSpace(string(out)))
	}
	wantKilled(shell)
}

// Of what <cli> --version prints, only the first 64 KiB are read, and a CLI
// that prints more still runs to its end: a version past them is not seen,
// one before them is.
func TestProbeReadsTheFirst64KiB(t *testing.T) {
	const flood = "head -c 70000 /dev/zero | tr '\\0' x\necho"
	for body, want := range map[string]string{
		flood + "\necho '2.1.314 (Claude Code)'": "",
		"echo '2.1.314 (Claude Code)'\n" + flood: "2.1.314",
	} {
		dir := realDir(t)
		fakeCLI(t, dir, "claude", body)
		p := probeWithin(t, ClaudeCode, dir, limits{timeout: probeTimeout, waitDelay: probeWaitDelay})
		if p.Version != want || p.Known != (want != "") {
			t.Errorf("a CLI printing %q: %+v", body, p)
		}
	}
}
