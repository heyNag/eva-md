package drive

import (
	"fmt"
	"strings"
	"testing"

	"github.com/heyNag/eva-md/internal/host/adapter"
)

// spy stands in for a test's testing.TB in a rig: it keeps what the rig
// registers to run at the test's end, says the test failed once told so,
// and keeps what is logged, so that a test can read what a failing test's
// output would end with. What it does not override goes to the test.
type spy struct {
	testing.TB
	failed   bool
	logs     []string
	cleanups []func()
}

func (s *spy) Cleanup(f func()) { s.cleanups = append(s.cleanups, f) }
func (s *spy) Failed() bool     { return s.failed }
func (s *spy) Log(args ...any)  { s.logs = append(s.logs, fmt.Sprint(args...)) }
func (s *spy) Logf(f string, args ...any) {
	s.logs = append(s.logs, fmt.Sprintf(f, args...))
}

// end runs what the rig registered, the last first, once.
func (s *spy) end() {
	for len(s.cleanups) > 0 {
		f := s.cleanups[len(s.cleanups)-1]
		s.cleanups = s.cleanups[:len(s.cleanups)-1]
		f()
	}
}

// A test of the rig that fails ends its output with the rig's diagnosis,
// and one that passes says nothing more. When the version's call or
// Codex's features list fails, so that the driver's words are only "the
// host's CLI gave no version" or "did not answer", the diagnosis says how
// that call ended as the driver saw it, by its process and the kind of
// its wait's end, and what the fake recorded of that process: nothing for
// one that died before it reached its record, as a process its runtime
// ended does, a fault of its own before its record by the end alone, or
// the call and its end. The fake fails each on purpose.
func TestAFailedTestShowsHowTheVersionCallEnded(t *testing.T) {
	const noVersion = "the host's CLI gave no version"
	for name, c := range map[string]struct {
		host  adapter.ID
		set   func(r *rig)
		err   string
		wants func(r *rig) []string
	}{
		"killed before its record": {adapter.ClaudeCode, func(r *rig) { r.script.VersionFault = "killed" }, noVersion,
			func(r *rig) []string {
				return []string{fmt.Sprintf("  1: version, pid %d, signal: killed; the fake's record: nothing, it never reached its record\n", r.ended[0].Pid)}
			}},
		"an exit": {adapter.ClaudeCode, func(r *rig) { r.script.VersionFault = "exit" }, noVersion,
			func(r *rig) []string {
				return []string{
					fmt.Sprintf("  1: pid %d, claude-code version, session \"\": exit 1\n", r.ended[0].Pid),
					fmt.Sprintf("  1: version, pid %d, exit status 1; the fake's record: a version call and an end, exit 1\n", r.ended[0].Pid),
				}
			}},
		"a fault before its record": {adapter.Codex, func(r *rig) { r.script.VersionFault = "fault" }, noVersion,
			func(r *rig) []string {
				return []string{
					fmt.Sprintf("  pid %d, no call recorded: exit 3, recording the call: a fault its script asked for\n", r.ended[0].Pid),
					fmt.Sprintf("  1: version, pid %d, exit status 3; the fake's record: an end, exit 3, recording the call: a fault its script asked for\n", r.ended[0].Pid),
				}
			}},
		"Codex's features list": {adapter.Codex, func(r *rig) { r.script.NoFeatures = true }, "web tools cannot be turned off",
			func(r *rig) []string {
				return []string{
					fmt.Sprintf("  1: version, pid %d, exit status 0; the fake's record: a version call and an end, exit 0\n", r.ended[0].Pid),
					fmt.Sprintf("  2: tools, pid %d, exit status 1; the fake's record: a tools call and an end, exit 1\n", r.ended[1].Pid),
				}
			}},
	} {
		t.Run(name, func(t *testing.T) {
			s := &spy{TB: t}
			t.Cleanup(s.end)
			r := newRig(s, 1)
			r.opts.Host = string(c.host)
			c.set(r)
			_, out, err := r.run()
			if err == nil || !strings.Contains(err.Error(), c.err) {
				t.Fatalf("run: %v\n%s", err, out)
			}
			if len(r.ended) == 0 || r.ended[0].Call != traceVersion || r.ended[0].Pid == 0 {
				t.Fatalf("the driver's calls: %+v", r.ended)
			}
			wants := c.wants(r)
			s.failed = true
			s.end()
			if len(s.logs) != 1 || !strings.HasPrefix(s.logs[0], "what the rig's runs did:\n") {
				t.Fatalf("logged: %q", s.logs)
			}
			for _, want := range wants {
				if !strings.Contains(s.logs[0], want) {
					t.Fatalf("the diagnosis has no %q:\n%s", want, s.logs[0])
				}
			}
		})
	}
	// A run that stops before the driver runs the CLI at all, as one whose
	// probe finds no CLI to run does, says so.
	t.Run("no call", func(t *testing.T) {
		r := newRig(t, 1)
		if d := r.diagnose(); !strings.Contains(d, "the calls of the CLI as the driver saw them:\n  none\n") {
			t.Fatalf("the diagnosis of no call:\n%s", d)
		}
	})
	t.Run("a test that passes", func(t *testing.T) {
		s := &spy{TB: t}
		t.Cleanup(s.end)
		r := newRig(s, 1)
		if sum, out, err := r.run(); err != nil || sum.Judged != 1 {
			t.Fatalf("run: %+v %v\n%s", sum, err, out)
		}
		s.end()
		if len(s.logs) != 0 {
			t.Fatalf("logged: %q", s.logs)
		}
	})
}
