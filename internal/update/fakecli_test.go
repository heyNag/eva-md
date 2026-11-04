package update

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/heyNag/eva-md/internal/identity"
	"github.com/heyNag/eva-md/internal/okfadapter"
	"github.com/heyNag/eva-md/internal/resume"
	"github.com/heyNag/eva-md/internal/testgitenv"
)

// fakeScript stands in for claude and codex. It works in $HOME/fake, the
// test's dump folder. Run n records its arguments (NUL-separated), stdin,
// environment and physical working folder there as args.n, stdin.n, env.n
// and pwd.n, and copies the schema file it was given to schema.n. It copies
// output.n to the file after -o, writes stderr.n and stdout.n, runs the
// shell lines in extra, and exits with the code in exit.n. Each of these
// falls back to the file without ".n". codex features list is not a run: it
// records its arguments in features.args, runs the shell lines in
// features.sh, and prints the file features, or exits 1 when there is none.
const fakeScript = `#!/bin/sh
dump=$HOME/fake
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

// fakeBin holds the fake claude and codex, written once for all the tests:
// macOS checks a new executable on its first run, which costs about 0.3 s a
// time when each test writes its own.
var fakeBin string

// Tests check what the update does, not how fast the machine runs git or
// reads a CLI's output: the time limits that protect a job from either are
// raised for every test, before any test runs. A test of a CLI run's
// timeout ends that timeout itself (see holdTimeouts).
func TestMain(m *testing.M) {
	testgitenv.Clear()
	// The time limits a slow machine could reach, raised for every test.
	// internal/testlimits can't be imported here: it imports this package.
	identity.Timeout, identity.WaitDelay = time.Minute, time.Minute
	resume.GitTimeout, resume.GitWaitDelay = time.Minute, time.Minute
	GitTimeout, GitWaitDelay = time.Minute, time.Minute
	okfadapter.LockTimeout = time.Minute
	// A run's wait for its output once the CLI has exited, raised to 30 s,
	// past any copy of that output however late the copying goroutine runs,
	// but kept to half of hangGuard: a grandchild forked while a run kills
	// the group can miss the kill and hold the pipes open, and the run then
	// ends only when this wait does, which must still be inside the test's
	// guard.
	WaitDelay = hangGuard / 2
	featuresTimeout = 10 * time.Minute
	runTimer, featuresTimer = heldTimer(timeoutsKey{}), heldTimer(featuresKey{})
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
	res Result
	err error
}

// launch starts r on host's runner in the background. The run's context ends
// with the test, so a run that a failing test leaves behind is stopped.
func launch(t *testing.T, ctx context.Context, host string, r Request) <-chan ran {
	t.Helper()
	ctx, cancel := context.WithCancel(ctx)
	t.Cleanup(cancel)
	done := make(chan ran, 1)
	go func() {
		res, err := NewRunner(host).Run(ctx, r)
		done <- ran{res, err}
	}()
	return done
}

// await waits for a launched run to end, and fails the test, naming what
// it waited for, when the run is still going after hangGuard.
func await(t *testing.T, done <-chan ran, what string) (Result, error) {
	t.Helper()
	select {
	case r := <-done:
		return r.res, r.err
	case <-time.After(hangGuard):
		t.Fatalf("%s: still running after %s", what, hangGuard)
		return Result{}, nil
	}
}

// holdTimeouts returns a context under which a CLI run's timeout ends only
// when the test calls expire, as if its time had run out, so a test of a
// timeout never races the machine's speed. A run under any other context
// gets its real timeout.
func holdTimeouts(ctx context.Context) (context.Context, *timeouts) {
	return hold(ctx, timeoutsKey{})
}

// holdFeatures is holdTimeouts for the codex features list a Codex run reads
// first, whose timeout is its own.
func holdFeatures(ctx context.Context) (context.Context, *timeouts) {
	return hold(ctx, featuresKey{})
}

func hold(ctx context.Context, key any) (context.Context, *timeouts) {
	h := &timeouts{started: make(chan time.Duration, 16), expired: make(chan struct{})}
	return context.WithValue(ctx, key, h), h
}

// timeouts holds the timeouts of the runs started under one context.
type timeouts struct {
	started chan time.Duration // each run's timeout, as the run starts
	expired chan struct{}      // closed by expire
	once    sync.Once
}

type (
	timeoutsKey struct{}
	featuresKey struct{}
)

// next waits for the next run to start and returns its timeout.
func (h *timeouts) next(t *testing.T) time.Duration {
	t.Helper()
	select {
	case d := <-h.started:
		return d
	case <-time.After(hangGuard):
		t.Fatalf("no run started in %s", hangGuard)
		return 0
	}
}

// expire ends the timeout of every run started under the context, and of
// any run that starts later.
func (h *timeouts) expire() { h.once.Do(func() { close(h.expired) }) }

// heldTimer is runTimer, or featuresTimer, in the tests: under the context
// of holdTimeouts, or holdFeatures, its timeout ends when the test expires
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
	// A run that starts once the test has expired the timeouts starts with
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

// heldContext is a run's timeout that a test holds. Expired, it ends with
// the error a timeout gives.
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
