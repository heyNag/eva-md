package adapter

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
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
		{ClaudeCode, "claude", "2.1.314 (Claude Code)", "2.1.314", "2.1.312", true, false},
		{ClaudeCode, "claude", "2.1.400 (Claude Code)", "2.1.400", "2.1.312", false, true},
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
		return native(ctx, cli, args...)
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
