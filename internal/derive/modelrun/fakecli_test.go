package modelrun

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/heyNag/eva-md/internal/host/adapter"
)

// The rig, ported from v1's internal/update/fakecli_test.go for every
// backend's tests: a fake CLI script, the helpers that read what it recorded,
// and timers a test holds. It knows no host's output; a backend's tests
// write their own.

// fakeScript stands in for every host's CLI. It works in $HOME/fake, the
// test's dump folder. Run n records its arguments (NUL-separated), stdin,
// environment and physical working folder there as args.n, stdin.n, env.n
// and pwd.n, and copies the schema file it was given to schema.n. It copies
// output.n to the file after -o, writes stderr.n and stdout.n, runs the
// shell lines in extra, and exits with the code in exit.n. Each of these
// falls back to the file without ".n". --version is not a run: it runs the
// shell lines in version.sh, then prints the file version, or 0.1.0.
// features list is not a run either: it records its arguments in
// features.args, runs the shell lines in features.sh, and prints the file
// features, or exits 1 when there is none.
const fakeScript = `#!/bin/sh
dump=$HOME/fake
if [ "$1" = --version ]; then
	if [ -f "$dump/version.sh" ]; then . "$dump/version.sh"; fi
	if [ -f "$dump/version" ]; then cat "$dump/version"; else echo "0.1.0 (fake)"; fi
	exit 0
fi
if [ "$1" = features ]; then
	printf '%s\0' "$@" > "$dump/features.args"
	if [ -f "$dump/features.sh" ]; then . "$dump/features.sh"; fi
	if [ -f "$dump/features" ]; then cat "$dump/features"; exit 0; fi
	exit 1
fi
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

// fakeBin holds the fake CLI under each host's CLI name, written once for
// all the tests: macOS checks a new executable on its first run, which costs
// about 0.3 s a time when each test writes its own.
var fakeBin string

// Tests check what a run does, not how fast the machine starts a process:
// the time limits are raised for every test, before any test runs. A test
// of a limit ends it itself (holdTimeouts, holdProbeStep). The adapter's own
// limit on --version can't be raised from here, so every test sets
// Options.Env (testEnv), whose Run gives --version no limit.
func TestMain(m *testing.M) {
	// The wait for a run's output once the CLI has exited, raised past any
	// copy of that output however late the copying goroutine runs, but kept
	// to half of hangGuard: a grandchild forked while a run kills the group
	// can miss the kill and hold the pipes open, and the run then ends only
	// when this wait does, which must still be inside the test's guard.
	waitDelay = hangGuard / 2
	probeStepTimeout = 10 * time.Minute
	runTimer, probeStepTimer = heldTimer(timeoutsKey{}), heldTimer(probeStepKey{})
	dir, err := os.MkdirTemp("", "eva-md-modelrun-test-")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fakeBin = filepath.Join(dir, "bin")
	err = os.Mkdir(fakeBin, 0o700)
	var first string
	for _, h := range adapter.All() {
		if err != nil {
			break
		}
		path := filepath.Join(fakeBin, h.Names.CLI)
		if first == "" {
			first, err = path, os.WriteFile(path, []byte(fakeScript), 0o700)
		} else if os.Link(first, path) != nil {
			err = os.WriteFile(path, []byte(fakeScript), 0o700)
		}
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

// Made-up values.
const (
	// answer is a valid answer for the rig's schema.
	answer = `{"summary":"done"}`
	// The input marker must reach stdin and appear in no argument.
	batchMarker = "BATCH-MARKER-7"
)

// hostIDs are the hosts the rig's script stands in for.
var hostIDs = []adapter.ID{adapter.ClaudeCode, adapter.Codex}

// cliName is a host's CLI name.
func cliName(t *testing.T, id adapter.ID) string {
	t.Helper()
	h, ok := adapter.ByID(string(id))
	if !ok {
		t.Fatalf("no host %q", id)
	}
	return h.Names.CLI
}

// isolate gives the test a home, a TMPDIR and a PATH of its own, with the
// fake CLIs first on it.
func isolate(t *testing.T) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(home, "gitconfig"))
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("TMPDIR", t.TempDir())
	t.Setenv("PATH", fakeBin+":/usr/bin:/bin")
}

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

// ownCLI gives the test a link of its own to the fake script, named for
// id's CLI, in a folder of its own, by its path with every symlink
// resolved. The adapter and the probe cache a version by path, so a test
// that reads a version, or changes it, probes its own link.
func ownCLI(t *testing.T, id adapter.ID) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, cliName(t, id))
	if os.Link(filepath.Join(fakeBin, cliName(t, id)), path) != nil {
		if err := os.WriteFile(path, []byte(fakeScript), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	return path
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

// runs is how many times the fake CLI ran, --version and features list
// aside.
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

// call is a call for host id with made-up values and an empty run folder,
// with no effort flag.
func call(t *testing.T, id adapter.ID) Call {
	t.Helper()
	model := "gpt-test-model"
	if id == adapter.ClaudeCode {
		model = "claude-test-model"
	}
	return Call{
		Task:   TaskExtract,
		System: "Fixed rules for the test.\nReturn the JSON only.",
		Input:  []byte("[1] user 2026-10-31 10:00: " + batchMarker + " plan the release\n"),
		Schema: []byte(`{"type":"object","properties":{"summary":{"type":"string"}},"required":["summary"],"additionalProperties":false}`),
		Model:  model,
		RunDir: t.TempDir(),
	}
}

// probeCalls is an adapter.Env for a test that counts what it runs. Its
// LookPath is PATH's. Its Run starts the command from the test's process,
// as the adapter's own would: --version with no limit, since the adapter's
// can't be raised here and the script answers at once, and anything else,
// such as a probe step, under ctx, which a test may hold.
type probeCalls struct {
	mu    sync.Mutex
	calls []string // each call's arguments, space-separated
}

func testEnv() (*probeCalls, adapter.Env) {
	p := &probeCalls{}
	return p, adapter.Env{Run: p.run}
}

func (p *probeCalls) run(ctx context.Context, cli string, args ...string) ([]byte, error) {
	p.mu.Lock()
	p.calls = append(p.calls, strings.Join(args, " "))
	p.mu.Unlock()
	if len(args) == 1 && args[0] == "--version" {
		ctx = context.WithoutCancel(ctx)
	}
	cmd := exec.CommandContext(ctx, cli, args...)
	cmd.WaitDelay = hangGuard / 2
	return cmd.Output()
}

// count is how many calls had these arguments.
func (p *probeCalls) count(args string) int {
	p.mu.Lock()
	defer p.mu.Unlock()
	n := 0
	for _, c := range p.calls {
		if c == args {
			n++
		}
	}
	return n
}

// total is how many calls there were.
func (p *probeCalls) total() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.calls)
}

// hangGuard is how long a test waits for what must happen, such as a run's
// end, a process's exit or a file the fake writes, before it fails naming
// what hung. No time limit under test comes near it, and a fake that only a
// kill should end sleeps 300 s, past it, so it fires only when the code
// hangs.
const hangGuard = time.Minute

// startChild is a fake's shell line that starts a child, which sleeps past
// hangGuard, and records the child's pid in the file child, whole once it is
// there. The fakes that use it exec their last command rather than fork it:
// a process forked while the run kills the group can miss the kill and hold
// the run's output open.
const startChild = `sleep 300 & echo $! > "$dump/child.tmp"; mv "$dump/child.tmp" "$dump/child"; `

// waitGone waits for the process recorded in file to be gone.
func waitGone(t *testing.T, dump, file string) {
	t.Helper()
	pid, err := strconv.Atoi(strings.TrimSpace(read(t, dump, file)))
	if err != nil || pid <= 1 {
		t.Fatalf("no child pid recorded: %v", err)
	}
	deadline := time.Now().Add(hangGuard)
	for syscall.Kill(pid, 0) == nil {
		if time.Now().After(deadline) {
			_ = syscall.Kill(pid, syscall.SIGKILL)
			t.Fatalf("child %d outlived the run by %s", pid, hangGuard)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// waitFile waits for the fake CLI to write file, and reports false when it
// hasn't after hangGuard.
func waitFile(dump, file string) bool {
	deadline := time.Now().Add(hangGuard)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(filepath.Join(dump, file)); err == nil {
			return true
		}
		time.Sleep(20 * time.Millisecond)
	}
	return false
}

// ran is how a run ended.
type ran struct {
	out Outcome
	err error
}

// launch starts c on r in the background. The run's context ends with the
// test, so a run that a failing test leaves behind is stopped.
func launch(t *testing.T, ctx context.Context, r Runner, c Call) <-chan ran {
	t.Helper()
	ctx, cancel := context.WithCancel(ctx)
	t.Cleanup(cancel)
	done := make(chan ran, 1)
	go func() {
		out, err := r.Run(ctx, c)
		done <- ran{out, err}
	}()
	return done
}

// await waits for a launched run to end, and fails the test, naming what
// it waited for, when the run is still going after hangGuard.
func await(t *testing.T, done <-chan ran, what string) (Outcome, error) {
	t.Helper()
	select {
	case r := <-done:
		return r.out, r.err
	case <-time.After(hangGuard):
		t.Fatalf("%s: still running after %s", what, hangGuard)
		return Outcome{}, nil
	}
}

// holdTimeouts returns a context under which an attempt's time limit ends
// only when the test calls expire, as if its time had run out, so a test of
// a time limit never races the machine's speed. A run under any other
// context gets its real limit.
func holdTimeouts(ctx context.Context) (context.Context, *timeouts) {
	return hold(ctx, timeoutsKey{})
}

// holdProbeStep is holdTimeouts for a backend's probe step, whose limit is
// its own.
func holdProbeStep(ctx context.Context) (context.Context, *timeouts) {
	return hold(ctx, probeStepKey{})
}

func hold(ctx context.Context, key any) (context.Context, *timeouts) {
	h := &timeouts{started: make(chan time.Duration, 16), expired: make(chan struct{})}
	return context.WithValue(ctx, key, h), h
}

// timeouts holds the time limits started under one context.
type timeouts struct {
	started chan time.Duration // each limit, as it starts
	expired chan struct{}      // closed by expire
	once    sync.Once
}

type (
	timeoutsKey  struct{}
	probeStepKey struct{}
)

// next waits for the next limit to start and returns it.
func (h *timeouts) next(t *testing.T) time.Duration {
	t.Helper()
	select {
	case d := <-h.started:
		return d
	case <-time.After(hangGuard):
		t.Fatalf("no limit started in %s", hangGuard)
		return 0
	}
}

// expire ends every limit started under the context, and any that starts
// later.
func (h *timeouts) expire() { h.once.Do(func() { close(h.expired) }) }

// heldTimer is runTimer, or probeStepTimer, in the tests: under the context
// of holdTimeouts, or holdProbeStep, its limit ends when the test expires
// it, and elsewhere when it passes.
func heldTimer(key any) func(context.Context, time.Duration) (context.Context, context.CancelFunc) {
	return func(ctx context.Context, d time.Duration) (context.Context, context.CancelFunc) {
		h, ok := ctx.Value(key).(*timeouts)
		if !ok {
			return context.WithTimeout(ctx, d)
		}
		return h.timer(ctx, d)
	}
}

func (h *timeouts) timer(ctx context.Context, d time.Duration) (context.Context, context.CancelFunc) {
	select {
	case h.started <- d:
	case <-ctx.Done():
	}
	c := &heldContext{Context: ctx, done: make(chan struct{})}
	// A limit that starts once the test has expired the limits starts with
	// no time left at all.
	select {
	case <-h.expired:
		c.end(context.DeadlineExceeded)
		return c, func() {}
	default:
	}
	go func() {
		select {
		case <-h.expired:
			c.end(context.DeadlineExceeded)
		case <-ctx.Done():
			c.end(ctx.Err())
		case <-c.done:
		}
	}()
	return c, func() { c.end(context.Canceled) }
}

// heldContext is a time limit that a test holds. Expired, it ends with the
// error a timeout gives.
type heldContext struct {
	context.Context
	done chan struct{}
	mu   sync.Mutex
	err  error
}

func (c *heldContext) Done() <-chan struct{} { return c.done }

func (c *heldContext) Err() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.err
}

func (c *heldContext) end(err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.err == nil {
		c.err = err
		close(c.done)
	}
}
