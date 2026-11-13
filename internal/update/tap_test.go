package update

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// tapped is what a tap received, one call an attempt.
type tapped struct{ stdout, stderr [][]byte }

// tap returns a tap that keeps copies of what it gets, then overwrites the
// bytes it was given, so a runner that let a tap reach its own buffers
// would read the overwritten ones.
func (tp *tapped) tap(stdout, stderr []byte) {
	tp.stdout = append(tp.stdout, bytes.Clone(stdout))
	tp.stderr = append(tp.stderr, bytes.Clone(stderr))
	for _, b := range [][]byte{stdout, stderr} {
		for i := range b {
			b[i] = 'x'
		}
	}
}

// clearRuns removes the fake's count of runs, so the next run is run 1
// again and finds the same canned files.
func clearRuns(t *testing.T, dump string) {
	t.Helper()
	if err := os.Remove(filepath.Join(dump, "count")); err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
}

// The tap gets, once after each attempt, the CLI's stdout and stderr as the
// CLI wrote them, byte for byte: here the attempt an effort refusal steps
// down from and the one that answers, each with its own made-up stderr,
// one with a NUL byte, neither ending in a newline. codex features list,
// which is not an attempt, never reaches it, though it prints. With the tap
// set, the run's Result is the one without it, field for field, though the
// tap overwrites what it is given.
func TestTapGetsEachAttemptsOutput(t *testing.T) {
	for _, host := range hosts {
		t.Run(host, func(t *testing.T) {
			isolate(t)
			dump := fakeHost(t, host)
			top := claudeEfforts[len(claudeEfforts)-1]
			if host == HostCodex {
				top = codexEfforts[len(codexEfforts)-1]
				put(t, dump, "features", "FEATURES-LIST-MARKER   stable  true\nhooks   experimental   false\n")
			}
			stdout := []string{effortRejected(host, top), map[string]string{HostClaude: claudeSuccess, HostCodex: codexClean}[host]}
			stderr := []string{"made-up warning one\x00 with a NUL", "made-up warning two"}
			for i := range stdout {
				n := fmt.Sprint(i + 1)
				put(t, dump, "stdout."+n, stdout[i])
				put(t, dump, "stderr."+n, stderr[i])
			}
			put(t, dump, "exit.1", "1")
			r := request(t, host)
			var got tapped
			r.Tap = got.tap
			res, err := NewRunner(host).Run(context.Background(), r)
			if err != nil {
				t.Fatal(err)
			}
			if res.Reason != "" || res.Attempts != 2 || runs(t, dump) != 2 || res.Degraded == "" {
				t.Fatalf("result %+v after %d runs, want an answer on the second attempt", res, runs(t, dump))
			}
			if len(got.stdout) != 2 {
				t.Fatalf("the tap was called %d times for 2 attempts", len(got.stdout))
			}
			for i := range stdout {
				if string(got.stdout[i]) != stdout[i] || string(got.stderr[i]) != stderr[i] {
					t.Errorf("attempt %d: the tap got stdout %q and stderr %q", i+1, got.stdout[i], got.stderr[i])
				}
				if bytes.Contains(got.stdout[i], []byte("FEATURES-LIST-MARKER")) {
					t.Errorf("attempt %d: the tap got codex features list", i+1)
				}
			}

			clearRuns(t, dump)
			r = request(t, host)
			without, err := NewRunner(host).Run(context.Background(), r)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(res, without) {
				t.Errorf("with a tap the result is\n%+v\nwithout one\n%+v", res, without)
			}
		})
	}
}

// A run that times out gives the tap what the CLI wrote before its time
// ran out, and its Result is the one without a tap. The test ends the
// timeout itself once the fake has written, so the machine's speed decides
// nothing.
func TestTapGetsATimedOutAttempt(t *testing.T) {
	for _, host := range hosts {
		t.Run(host, func(t *testing.T) {
			isolate(t)
			dump := fakeCLI(t, `: > "$dump/printed"; exec sleep 300`)
			stdout := claudeSuccess
			if host == HostCodex {
				stdout = codexClean
				put(t, dump, "output", answer)
			}
			put(t, dump, "stdout", stdout)
			put(t, dump, "stderr", "made-up notice before the time ran out\n")
			results := make([]Result, 2)
			for i := range results {
				clearRuns(t, dump)
				if err := os.Remove(filepath.Join(dump, "printed")); err != nil && !os.IsNotExist(err) {
					t.Fatal(err)
				}
				r := request(t, host)
				var got tapped
				if i == 0 {
					r.Tap = got.tap
				}
				ctx, timeouts := holdTimeouts(context.Background())
				done := launch(t, ctx, host, r)
				if !waitFile(dump, "printed") {
					t.Fatalf("the fake wrote nothing in %s", hangGuard)
				}
				timeouts.expire()
				res, err := await(t, done, "a run whose timeout ended")
				if err != nil || res.Reason != ReasonTimeout || res.Attempts != 1 {
					t.Fatalf("result %+v %v, want a timeout", res, err)
				}
				results[i] = res
				if i == 1 {
					break
				}
				if len(got.stdout) != 1 || string(got.stdout[0]) != stdout || string(got.stderr[0]) != "made-up notice before the time ran out\n" {
					t.Fatalf("the tap got stdout %q and stderr %q", got.stdout, got.stderr)
				}
			}
			if !reflect.DeepEqual(results[0], results[1]) {
				t.Errorf("with a tap the result is\n%+v\nwithout one\n%+v", results[0], results[1])
			}
		})
	}
}

// A run whose time ran out before its CLI could start made an attempt that
// printed nothing, and the tap gets that empty attempt; a CLI that is
// missing made no attempt, and the tap is not called.
func TestTapCallsMatchTheAttempts(t *testing.T) {
	for _, host := range hosts {
		t.Run(host+" timed out before it started", func(t *testing.T) {
			isolate(t)
			dump := fakeHost(t, host)
			r := request(t, host)
			var got tapped
			r.Tap = got.tap
			ctx, timeouts := holdTimeouts(context.Background())
			timeouts.expire()
			res, err := await(t, launch(t, ctx, host, r), "a run whose time ran out before its CLI started")
			if err != nil || res.Reason != ReasonTimeout || runs(t, dump) != 0 {
				t.Fatalf("result %+v %v after %d runs", res, err, runs(t, dump))
			}
			if len(got.stdout) != res.Attempts || res.Attempts != 1 || len(got.stdout[0]) != 0 || len(got.stderr[0]) != 0 {
				t.Fatalf("%d attempts, the tap got %q and %q", res.Attempts, got.stdout, got.stderr)
			}
		})
		t.Run(host+" missing", func(t *testing.T) {
			isolate(t)
			t.Setenv("PATH", t.TempDir())
			r := request(t, host)
			var got tapped
			r.Tap = got.tap
			res, err := NewRunner(host).Run(context.Background(), r)
			if err != nil || res.Reason != ReasonCLIMissing || res.Attempts != 0 || len(got.stdout) != 0 {
				t.Fatalf("result %+v %v, the tap called %d times", res, err, len(got.stdout))
			}
		})
	}
}
