package drive

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/heyNag/eva-md/internal/eval/continuation/task"
)

// When a trial the condition readied ends without After, what the
// condition's Spent gives counts toward --max-tokens and in the run's
// record: a chat the host leaves unjudged, a plugin that does not load, a
// launch the driver refuses, an error of Prepare's own and the run's end
// in Prepare. It is not asked when After is, when Prepare's *Unjudged
// carries its cost, or when the condition stops the run.
func TestSpentCountsWhatAfterDoesNot(t *testing.T) {
	const spent = 340
	type outcome struct {
		asked   int   // Spent's calls
		tokens  int64 // the run's tokens, less the chat's
		notRun  int
		failing bool // the run ends on an error
	}
	for name, c := range map[string]struct {
		set     func(r *rig)
		prepare func(ctx context.Context, cancel context.CancelFunc) (Launch, error)
		want    outcome
	}{
		"a rate-limited chat":         {set: func(r *rig) { r.script.RateLimited = true }, want: outcome{asked: 1, tokens: spent, notRun: 1}},
		"a chat that printed nothing": {set: func(r *rig) { r.script.Empty = true }, want: outcome{asked: 1, tokens: spent, notRun: 1}},
		"a plugin that does not load": {
			prepare: func(context.Context, context.CancelFunc) (Launch, error) { return Launch{Plugin: true}, nil },
			want:    outcome{asked: 1, tokens: spent, notRun: 1},
		},
		"a launch the driver refuses": {
			prepare: func(context.Context, context.CancelFunc) (Launch, error) {
				return Launch{Env: []string{"HOME=/elsewhere"}}, nil
			},
			want: outcome{asked: 1, tokens: spent, notRun: 1, failing: true},
		},
		"an error of Prepare's own": {
			prepare: func(context.Context, context.CancelFunc) (Launch, error) {
				return Launch{}, errors.New("made-up failure")
			},
			want: outcome{asked: 1, tokens: spent, notRun: 1, failing: true},
		},
		"the run's end in Prepare": {
			prepare: func(ctx context.Context, cancel context.CancelFunc) (Launch, error) {
				cancel()
				return Launch{}, ctx.Err()
			},
			want: outcome{asked: 1, tokens: spent, notRun: 1, failing: true},
		},
		"Prepare's unjudged with its cost": {
			prepare: func(context.Context, context.CancelFunc) (Launch, error) {
				return Launch{}, &Unjudged{Code: CodeUpdateFailed, Cost: Cost{Tokens: 5}}
			},
			want: outcome{tokens: 10},
		},
		"the condition stops the run": {
			prepare: func(context.Context, context.CancelFunc) (Launch, error) { return Launch{}, ErrStopRun },
			want:    outcome{notRun: 2, failing: true},
		},
		"a judged chat": {want: outcome{tokens: 2 * 70}},
	} {
		t.Run(name, func(t *testing.T) {
			r := newRig(t, 1)
			r.opts.Condition = task.Eva
			r.opts.Trials = 2
			if c.set != nil {
				c.set(r)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			asked := 0
			arm := Arm{
				After: func(context.Context, Trial) (Cost, error) { return Cost{Tokens: 70}, nil },
				Spent: func(_ context.Context, tr Trial) Cost {
					asked++
					if tr.Number != asked || tr.Dirs.Home == "" {
						t.Errorf("Spent was asked of trial %d, its home %q", tr.Number, tr.Dirs.Home)
					}
					return Cost{Tokens: spent, Ms: 900}
				},
			}
			if c.prepare != nil {
				arm.Prepare = func(context.Context, Trial) (Launch, error) { return c.prepare(ctx, cancel) }
			}
			useArm(t, arm)
			if c.want.tokens >= spent {
				// The condition's spend alone reaches the ceiling, so the
				// second trial is not run.
				r.opts.MaxTokens = spent
			}
			data, err := json.Marshal(r.script)
			if err != nil {
				t.Fatal(err)
			}
			write(t, r.file, string(data))
			var buf bytes.Buffer
			s, err := Run(ctx, r.opts, &buf)
			out := buf.String()
			chat := int64(0)
			if name == "a judged chat" {
				chat = 2 * (r.script.Prompt + r.script.Output) * 2
			}
			if asked != c.want.asked || s.Tokens != c.want.tokens+chat || s.NotRun != c.want.notRun || (err != nil) != c.want.failing {
				t.Fatalf("Spent asked %d times, tokens %d, not run %d, %v\n%s", asked, s.Tokens, s.NotRun, err, out)
			}
			if c.want.tokens >= spent && !c.want.failing && !strings.Contains(out, "not run, --max-tokens reached") {
				t.Fatalf("the ceiling was not reached:\n%s", out)
			}
			records, err := LoadRuns(r.opts.Out)
			if err != nil || len(records) != 1 || records[0].Tokens != s.Tokens {
				t.Fatalf("the run's record: %+v, %v", records, err)
			}
		})
	}
}
