package adapter

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"
)

// ConditionVersionUntested is the condition a host's version above its
// tested maximum opens. It is information until a capability check fails.
const ConditionVersionUntested = "host.version_untested"

// Env is what a probe reaches the machine through.
type Env struct {
	// LookPath finds a command as PATH does; nil is exec.LookPath.
	LookPath func(file string) (string, error)
	// Run runs a command and gives what it printed on stdout; nil runs it
	// from /, with what it prints capped and its diagnostics dropped.
	Run func(ctx context.Context, cli string, args ...string) ([]byte, error)
}

// ProbeResult is what a probe learned of a host's CLI.
type ProbeResult struct {
	Host ID
	// CLI is the CLI's absolute path, its folder's symlinks resolved, or ""
	// when PATH has none.
	CLI string
	// Version is the first N.N.N token of what <cli> --version printed, or
	// "".
	Version string
	// Row is the spec row the version falls in, or for an untested version
	// the newest row; nil otherwise.
	Row *Row
	// Known says a row's range holds the version.
	Known bool
	// Untested says the version is above the tested maximum, so the newest
	// row is used and ConditionVersionUntested opens.
	Untested bool
}

// Time limits on <cli> --version: the whole probe, and the wait for its
// output once it has exited or run out of time. Tests raise them.
var (
	probeTimeout   = 5 * time.Second
	probeWaitDelay = time.Second
)

// limits are the time limits one probe keeps. Probe takes the package's; a
// test that needs a short limit gives it to one probe here.
type limits struct {
	timeout   time.Duration
	waitDelay time.Duration
}

// Probe finds a host's CLI on PATH and reads its version. It reads no
// settings file, calls no model and uses no network. A CLI's version is read
// once while its file keeps its path, size and modification time.
func Probe(ctx context.Context, id ID, env Env) ProbeResult {
	return probe(ctx, id, env, limits{timeout: probeTimeout, waitDelay: probeWaitDelay})
}

func probe(ctx context.Context, id ID, env Env, lim limits) ProbeResult {
	result := ProbeResult{Host: id}
	h, ok := ByID(string(id))
	if !ok {
		return result
	}
	lookPath, run := env.LookPath, env.Run
	if lookPath == nil {
		lookPath = exec.LookPath
	}
	if run == nil {
		run = native(lim.waitDelay)
	}
	result.CLI = cliPath(h.Names.CLI, lookPath)
	if result.CLI == "" {
		return result
	}
	result.Version = version(ctx, result.CLI, run, lim.timeout)
	result.Row, result.Known, result.Untested = match(id, result.Version)
	return result
}

// cliPath is the CLI as lookPath finds it, by absolute path, or "". Its
// folder's symlinks are resolved, as install's cliPath does, since a folder
// a version manager puts on PATH for one shell is gone once that shell
// exits; the CLI keeps its own name.
func cliPath(cli string, lookPath func(string) (string, error)) string {
	path, err := lookPath(cli)
	if err != nil {
		return ""
	}
	if path, err = filepath.Abs(path); err != nil {
		return ""
	}
	if dir, err := filepath.EvalSymlinks(filepath.Dir(path)); err == nil {
		path = filepath.Join(dir, filepath.Base(path))
	}
	return path
}

// versionEntry is a CLI's version as read while its file had this size and
// modification time.
type versionEntry struct {
	size    int64
	mod     time.Time
	version string
}

var versions = struct {
	sync.Mutex
	byPath map[string]versionEntry
}{byPath: map[string]versionEntry{}}

// version reads the CLI's version, from the cache while its file is
// unchanged. A run that fails is not cached, so it is tried again.
func version(ctx context.Context, cli string, run func(context.Context, string, ...string) ([]byte, error), timeout time.Duration) string {
	info, err := os.Stat(cli)
	if err != nil {
		return ""
	}
	versions.Lock()
	e, ok := versions.byPath[cli]
	versions.Unlock()
	if ok && e.size == info.Size() && e.mod.Equal(info.ModTime()) {
		return e.version
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	out, err := run(ctx, cli, "--version")
	if err != nil {
		return ""
	}
	v := findVersion(out)
	versions.Lock()
	versions.byPath[cli] = versionEntry{size: info.Size(), mod: info.ModTime(), version: v}
	versions.Unlock()
	return v
}

// probeOutputLimit is the most of what <cli> --version prints that is kept.
const probeOutputLimit = 64 << 10

// cappedBuffer keeps the first probeOutputLimit bytes written to it. The
// buffer is a named field, not an embedded bytes.Buffer, whose ReadFrom
// os/exec would call in place of Write, so the limit would never hold.
type cappedBuffer struct {
	buffer bytes.Buffer
}

func (b *cappedBuffer) Write(p []byte) (int, error) {
	n := len(p)
	if room := probeOutputLimit - b.buffer.Len(); n > room {
		p = p[:max(room, 0)]
	}
	_, _ = b.buffer.Write(p)
	return n, nil
}

// native is the runner that runs a command from /, so no project's own
// settings take part, and keeps the first probeOutputLimit bytes it prints on
// stdout. Its diagnostics are dropped. Once the command has exited or its
// context has ended, its output is waited for no longer than waitDelay, as a
// child it leaves behind can hold the output open.
func native(waitDelay time.Duration) func(ctx context.Context, cli string, args ...string) ([]byte, error) {
	return func(ctx context.Context, cli string, args ...string) ([]byte, error) {
		cmd := exec.CommandContext(ctx, cli, args...)
		cmd.Dir = "/"
		cmd.WaitDelay = waitDelay
		var stdout cappedBuffer
		cmd.Stdout = &stdout
		if err := cmd.Run(); err != nil {
			return nil, errors.New("host version unavailable")
		}
		return stdout.buffer.Bytes(), nil
	}
}
