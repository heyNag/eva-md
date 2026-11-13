package update

import (
	"bytes"
	"context"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
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

// A failure that only stderr names, a rate limit on a run that printed
// nothing and exited 1, is read from the runner's own stderr, so the
// Result with a tap that overwrites what it is given is the one without a
// tap: rate_limit, not invalid_output.
func TestTapLeavesTheStderrThatDecides(t *testing.T) {
	const notice = "made-up: rate limit reached, try again later"
	for _, host := range hosts {
		t.Run(host, func(t *testing.T) {
			isolate(t)
			dump := fakeCLI(t, "")
			put(t, dump, "stderr", notice)
			put(t, dump, "exit", "1")
			results := make([]Result, 2)
			for i := range results {
				clearRuns(t, dump)
				r := request(t, host)
				var got tapped
				if i == 0 {
					r.Tap = got.tap
				}
				res, err := NewRunner(host).Run(context.Background(), r)
				if err != nil || res.Reason != ReasonRateLimit || res.Attempts != 1 {
					t.Fatalf("result %+v %v, want a rate limit (with a tap: %v)", res, err, i == 0)
				}
				if i == 0 && (len(got.stderr) != 1 || string(got.stderr[0]) != notice || len(got.stdout[0]) != 0) {
					t.Fatalf("the tap got stdout %q and stderr %q", got.stdout, got.stderr)
				}
				results[i] = res
			}
			if !reflect.DeepEqual(results[0], results[1]) {
				t.Errorf("with a tap the result is\n%+v\nwithout one\n%+v", results[0], results[1])
			}
		})
	}
}

// Nothing in eva-md sets a Request's Tap: a tap hands on what a host
// printed, raw and unredacted, which only the live accuracy run's trace,
// a developer's command, may keep. Every Go file of the module but tests
// is read, and a Tap given in a composite literal or assigned anywhere
// outside cmd/live-accuracy fails the test.
func TestOnlyTheLiveRunSetsATap(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Fatalf("the module's root: %v", err)
	}
	allowed := filepath.Join(root, "cmd", "live-accuracy")
	files, live := 0, 0
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if path != root && (strings.HasPrefix(d.Name(), ".") || d.Name() == "testdata" || d.Name() == "bin" || d.Name() == "scratch") {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		f, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.SkipObjectResolution)
		if err != nil {
			return err
		}
		files++
		sets := 0
		ast.Inspect(f, func(n ast.Node) bool {
			switch n := n.(type) {
			case *ast.KeyValueExpr:
				if k, ok := n.Key.(*ast.Ident); ok && k.Name == "Tap" {
					sets++
				}
			case *ast.AssignStmt:
				for _, l := range n.Lhs {
					if s, ok := l.(*ast.SelectorExpr); ok && s.Sel.Name == "Tap" {
						sets++
					}
				}
			}
			return true
		})
		if filepath.Dir(path) == allowed {
			live += sets
		} else if sets != 0 {
			rel, _ := filepath.Rel(root, path)
			t.Errorf("%s sets a Tap, which only cmd/live-accuracy may set", rel)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if files < 100 || live == 0 {
		t.Fatalf("read %d files, %d taps set in cmd/live-accuracy: the walk missed the module", files, live)
	}
}
