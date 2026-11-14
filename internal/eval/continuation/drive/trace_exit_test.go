package drive

import (
	"context"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/heyNag/eva-md/internal/eval/continuation/drive/fakehost"
)

// With --trace, each call's .exit file says how its wait ended, in one
// line and nothing else, for its user alone: an exit status, a signal the
// call took, or the signal the driver's kill at the session's limit gave,
// which says the limit or the run's end came first. The version's call,
// which exits 0, says so beside it. Each case leaves the trial unjudged as
// it would without a trace.
func TestTraceSaysHowEachCallEnded(t *testing.T) {
	for name, c := range map[string]struct {
		edit func(t *testing.T, r *rig)
		code string // the trial's code
		want string // the chat's .exit
	}{
		"an exit status": {func(t *testing.T, r *rig) { r.script.Empty = true }, CodeHostFailed, "exit 1\n"},
		"a signal":       {func(t *testing.T, r *rig) { r.script.Killed = true }, CodeHostFailed, "signal killed\n"},
		"the session's limit": {func(t *testing.T, r *rig) {
			ready := filepath.Join(r.base, "ready")
			if err := syscall.Mkfifo(ready, 0o600); err != nil {
				t.Fatal(err)
			}
			r.script.Stall, r.script.Ready = true, ready
			r.opts.sessionLimit = func(ctx context.Context, d time.Duration) (context.Context, context.CancelFunc) {
				c, cancel := context.WithCancel(ctx)
				go func() {
					// Opening blocks until the fake's child opens the pipe to
					// write: the call runs, and the limit ends it then.
					if f, err := os.Open(ready); err == nil {
						_, _ = io.ReadAll(f)
						_ = f.Close()
					}
					cancel()
				}()
				return c, cancel
			}
		}, CodeTimeout, "signal killed, after its time limit or the run's end\n"},
	} {
		t.Run(name, func(t *testing.T) {
			r := traceRig(t)
			c.edit(t, r)
			res := guarded(t, r)
			if res.err != nil || res.s.Unjudged[c.code] != 1 {
				t.Fatalf("run: %+v %v\n%s\n%s", res.s, res.err, res.out, r.diagnose())
			}
			dir := filepath.Join(r.opts.Trace, res.s.Run, fakehost.FixtureID, "off", "t1")
			for file, want := range map[string]string{"1-version.exit": "exit 0\n", "2-chat.exit": c.want} {
				path := filepath.Join(dir, file)
				got, err := os.ReadFile(path)
				if err != nil || string(got) != want {
					t.Fatalf("%s holds %q, want %q: %v", file, got, want, err)
				}
				if info, err := os.Lstat(path); err != nil || info.Mode() != 0o600 {
					t.Fatalf("%s: %v, %v", file, info.Mode(), err)
				}
			}
			var exits []string
			err := filepath.WalkDir(r.opts.Trace, func(p string, d fs.DirEntry, err error) error {
				if err == nil && strings.HasSuffix(p, ".exit") {
					exits = append(exits, filepath.Base(p))
				}
				return err
			})
			if err != nil || len(exits) != 2 {
				t.Fatalf("the trace's .exit files: %v, %v", exits, err)
			}
		})
	}
}

// exitLine reads a process's end as the wait gave it: a program that could
// not be started, and a held output, are said too.
func TestExitLine(t *testing.T) {
	if got := exitLine(nil, false, false); got != "not started\n" {
		t.Fatalf("no process: %q", got)
	}
	cmd := waited(t, "-c", "exit 3")
	if got := exitLine(cmd.ProcessState, false, true); got != "exit 3, its output held past the wait\n" {
		t.Fatalf("exit 3: %q", got)
	}
	cmd = waited(t, "-c", "kill -TERM $$")
	if got := exitLine(cmd.ProcessState, true, false); got != "signal terminated, after its time limit or the run's end\n" {
		t.Fatalf("a signal: %q", got)
	}
}

// waited runs the shell with args and waits for it, whatever it gives.
func waited(t *testing.T, args ...string) *exec.Cmd {
	t.Helper()
	cmd := exec.Command("/bin/sh", args...)
	_ = cmd.Run()
	if cmd.ProcessState == nil {
		t.Fatal("the shell did not run")
	}
	return cmd
}
