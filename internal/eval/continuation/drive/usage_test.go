package drive

import (
	"errors"
	"strconv"
	"strings"
	"testing"

	"github.com/heyNag/eva-md/internal/host/adapter"
)

// A call that answers without its usage, or with 0 prompt and output
// tokens, or prints past its output's limit, leaves its trial unjudged,
// no_usage, writes no row, and stops the run: the trials after it are not
// run, the summary is printed, and Run errs. Nothing the run spent after
// such a call could be counted against --max-tokens.
func TestNoUsageStopsTheRun(t *testing.T) {
	for _, h := range adapter.All() {
		for name, c := range map[string]struct {
			set    func(r *rig)
			judged int    // trials judged before the stop
			tokens int64  // what the ceiling counted
			starts int    // sessions started
			line   string // the stopped trial's line
		}{
			"no usage":     {func(r *rig) { r.script.NoUsage = true }, 0, 0, 1, "trial hidden-banner.t1: unjudged, no_usage\n"},
			"0 tokens":     {func(r *rig) { r.script.Prompt, r.script.Cached, r.script.Output = 0, 0, 0 }, 0, 0, 1, "trial hidden-banner.t1: unjudged, no_usage\n"},
			"in round two": {func(r *rig) { r.script.NoUsage, r.script.From = true, 2 }, 0, 1050, 1, "trial hidden-banner.t1: unjudged, no_usage\n"},
			"in the second session": {func(r *rig) { r.script.NoUsage, r.script.Session = true, 2 }, 1, 2100, 2,
				"trial hidden-banner.t2: unjudged, no_usage\n"},
			// The answer is past the limit, so no token of it is read.
			"past the output's limit": {func(r *rig) { r.opts.outputLimit, r.script.Pad = 64<<10, 1<<20 }, 0, 0, 1, "trial hidden-banner.t1: unjudged, no_usage\n"},
		} {
			t.Run(string(h.ID)+"/"+name, func(t *testing.T) {
				r := newRig(t, 1)
				r.opts.Host, r.opts.Trials = string(h.ID), 3
				c.set(r)
				s, out, err := r.run()
				var stopped *Stopped
				if !errors.As(err, &stopped) || stopped.Code != CodeNoUsage {
					t.Fatalf("run: %v\n%s", err, out)
				}
				if s.Judged != c.judged || s.Unjudged[CodeNoUsage] != 1 || len(s.Unjudged) != 1 || s.NotRun != 2-c.judged || s.Tokens != c.tokens {
					t.Fatalf("summary %+v\n%s", s, out)
				}
				if !strings.Contains(out, c.line) || !strings.Contains(out, ", no_usage 1, ") || !strings.Contains(out, "not run "+strconv.Itoa(2-c.judged)+",") {
					t.Fatalf("output:\n%s", out)
				}
				if got := len(r.rows()); got != 9*c.judged {
					t.Fatalf("%d rows", got)
				}
				if starts := countMode(r.calls(), "start"); starts != c.starts {
					t.Fatalf("%d sessions started", starts)
				}
			})
		}
	}
}

// A line longer than a scanner's default buffer, within the output's
// limit, is read: the answer after it is graded.
func TestALongLineWithinTheLimitIsRead(t *testing.T) {
	for _, h := range adapter.All() {
		t.Run(string(h.ID), func(t *testing.T) {
			r := newRig(t, 1)
			r.opts.Host = string(h.ID)
			r.script.Pad = 1 << 20
			if s, out, err := r.run(); err != nil || s.Judged != 1 || s.Tokens != 2100 {
				t.Fatalf("run: %+v %v\n%s", s, err, out)
			}
		})
	}
}

// A call that gives no answer is host_failed whatever its usage: a failure
// that says nothing of the set-up does not stop the run.
func TestNoAnswerWithNoUsageGoesOn(t *testing.T) {
	r := newRig(t, 1)
	r.opts.Trials = 2
	r.script.NoUsage, r.script.Status, r.script.Session = true, 529, 1
	s, out, err := r.run()
	if err != nil || s.Judged != 1 || s.Unjudged[CodeHostFailed] != 1 || s.Unjudged[CodeNoUsage] != 0 {
		t.Fatalf("run: %+v %v\n%s", s, err, out)
	}
}
