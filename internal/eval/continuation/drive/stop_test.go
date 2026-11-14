package drive

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/heyNag/eva-md/internal/eval/continuation/task"
)

// An Arm whose Prepare gives ErrStopRun, wrapped or not, stops the run
// before that trial starts: no chat of it starts, it is counted not run
// with every later one, so the record counts each trial planned once, the
// run errs with a *Stopped of arm_stopped, its record ends arm_stopped and
// loads, and no trial folder is left.
func TestAConditionStopsTheRun(t *testing.T) {
	for name, c := range map[string]struct {
		at       int // the trial whose Prepare stops the run
		before   string
		judged   int
		unjudged int
		wrapped  bool
	}{
		"at the first trial": {at: 1},
		"after two unjudged": {at: 3, before: CodeSetupFailed, unjudged: 2, wrapped: true},
		"after two judged":   {at: 3, judged: 2},
	} {
		t.Run(name, func(t *testing.T) {
			r := newRig(t, 1)
			r.opts.Condition, r.opts.Trials = task.Eva, 4
			useArm(t, Arm{Prepare: func(_ context.Context, tr Trial) (Launch, error) {
				switch {
				case tr.Number == c.at && c.wrapped:
					return Launch{}, fmt.Errorf("made-up rule: %w", ErrStopRun)
				case tr.Number == c.at:
					return Launch{}, ErrStopRun
				case tr.Number > c.at:
					t.Errorf("trial %d was prepared after the stop", tr.Number)
				case c.before != "":
					return Launch{}, &Unjudged{Code: c.before}
				}
				return Launch{}, nil
			}})
			s, out, err := r.run()
			notRun := 4 - c.at + 1
			var stopped *Stopped
			if !errors.As(err, &stopped) || stopped.Code != EndedArmStopped || s.Judged != c.judged || s.NotRun != notRun ||
				s.Unjudged[CodeSetupFailed] != c.unjudged || s.Planned != s.Judged+s.NotRun+c.unjudged {
				t.Fatalf("run: %+v %v\n%s", s, err, out)
			}
			want := fmt.Sprintf("trial hidden-banner.t%d and the %d after it: not run, the condition stopped the run\n", c.at, notRun-1)
			if !strings.Contains(out, want) || !strings.Contains(err.Error(), EndedArmStopped) {
				t.Fatalf("the output does not say the condition stopped the run:\n%s\n%v", out, err)
			}
			if starts := countMode(r.calls(), "start"); starts != c.judged {
				t.Fatalf("%d sessions started, want %d", starts, c.judged)
			}
			records, err := LoadRuns(r.opts.Out)
			if err != nil || len(records) != 1 {
				t.Fatalf("records %+v: %v", records, err)
			}
			got := records[0]
			sum := got.Judged + got.NotRun
			for _, n := range got.Unjudged {
				sum += n
			}
			if got.Ended != EndedArmStopped || got.Planned != 4 || sum != got.Planned || got.NotRun != notRun || got.Judged != c.judged {
				t.Fatalf("record %+v", got)
			}
			emptyWork(t, r)
		})
	}
}

// What a condition's Prepare spent before it left the trial unjudged
// counts toward --max-tokens and in the run's record; an unjudged trial
// that gives no cost counts nothing.
func TestPreparesSpendIsCounted(t *testing.T) {
	for name, c := range map[string]struct {
		cost   int64
		tokens int64 // the run's
		notRun int
	}{
		"a cost":  {cost: 700, tokens: 700, notRun: 1},
		"no cost": {cost: 0, tokens: 0, notRun: 0},
	} {
		t.Run(name, func(t *testing.T) {
			r := newRig(t, 1)
			r.opts.Condition, r.opts.Trials, r.opts.MaxTokens = task.Eva, 2, 500
			useArm(t, Arm{Prepare: func(context.Context, Trial) (Launch, error) {
				return Launch{}, &Unjudged{Code: CodeUpdateFailed, Cost: Cost{Tokens: c.cost, Ms: 9}}
			}})
			s, out, err := r.run()
			if err != nil || s.Tokens != c.tokens || s.NotRun != c.notRun || s.Unjudged[CodeUpdateFailed] != 2-c.notRun {
				t.Fatalf("run: %+v %v\n%s", s, err, out)
			}
			if c.notRun == 1 && !strings.Contains(out, "trial hidden-banner.t2 and the 0 after it: not run, --max-tokens reached\n") {
				t.Fatalf("the ceiling was not reached:\n%s", out)
			}
			records, err := LoadRuns(r.opts.Out)
			if err != nil || len(records) != 1 || records[0].Tokens != c.tokens || records[0].Ended != EndedDone {
				t.Fatalf("records %+v: %v", records, err)
			}
			emptyWork(t, r)
		})
	}
}
