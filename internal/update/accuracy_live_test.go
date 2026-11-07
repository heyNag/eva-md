package update_test

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"testing"

	"github.com/heyNag/eva-md/internal/threadpage"
	"github.com/heyNag/eva-md/internal/update"
	"github.com/heyNag/eva-md/internal/update/accuracy"
	"github.com/heyNag/eva-md/internal/update/accuracy/live"
)

// The suite's live run, as cmd/live-accuracy makes it, with the suite's own
// canned outputs as the model's answers, on each host's chats. The correct
// outputs meet the target with nothing missed or refused, and every page
// scores perfectly. With the adversarial outputs only the metrics catch in
// their passes, the run counts each content failure once, at its pass,
// with nothing else missed, and misses the target.
func TestAccuracyLiveRun(t *testing.T) {
	update.Isolate(t)
	for _, host := range []string{update.HostClaude, update.HostCodex} {
		for _, content := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s, content failures %v", host, content), func(t *testing.T) {
				asked := 0
				ask := func(ctx context.Context, s *accuracy.Script, n int, b update.Batch, page threadpage.Page) (update.Result, error) {
					asked++
					if b.Host != host {
						t.Errorf("%s, pass %d: a batch of %s", s.Name, n, b.Host)
					}
					return update.Result{Output: update.CannedAnswer(t, s.Name, n, b, page, content)}, nil
				}
				var ended []string
				res, err := live.Run(context.Background(), host, accuracy.Scripts(), ask, func(h live.History) {
					ended = append(ended, h.Script.Name)
					if !content && (!h.Metrics.Perfect() || len(h.Missed) != 0 || len(h.Refused) != 0) {
						t.Errorf("%s: metrics %+v, missed %v, refused %v", h.Script.Name, h.Metrics, h.Missed, h.Refused)
					}
				})
				if err != nil {
					t.Fatal(err)
				}
				var names []string
				passes := 0
				for _, s := range accuracy.Scripts() {
					names, passes = append(names, s.Name), passes+len(s.Passes)
				}
				if !slices.Equal(ended, names) || asked != 21 || passes != 21 || res.Passes != 21 || res.Refused != 0 {
					t.Fatalf("histories %q, %d answers asked and %d counted of %d passes, %d refused", ended, asked, res.Passes, passes, res.Refused)
				}
				tl := res.Tally
				var want []string
				if content {
					want = []string{
						"a reversal, pass 2: OSPREY-Y is still in force",
						"a conditional fact, pass 1: CONDOR-8 lost its condition",
						"pasted text posing as a decision, pass 1: PELICAN-6 is asserted",
						"pasted text posing as a decision, pass 1: AVOCET-1 is missing",
					}
				}
				var got []string
				for _, m := range tl.Content {
					got = append(got, m.String())
				}
				if !slices.Equal(got, want) || len(tl.Enforceable)+len(tl.Speaker)+len(tl.Date) != 0 || tl.Meets() == content {
					t.Errorf("content missed %q, want %q; enforceable %v, speaker %v, date %v; meets the target %v", got, want, tl.Enforceable, tl.Speaker, tl.Date, tl.Meets())
				}
			})
		}
	}
}

// In a run of correct answers, a refused one counts the same however it is
// refused. It is a content obligation missed, and the page, left as it was,
// is measured at the refused chat's time, so what the chat should have done
// counts too, as content whatever its kind: the step it finished and the
// question it answered still in force, when the validator refuses an
// answer that gives most of the page's ids no action, the very failure item
// accounting plants; and the values the replace question's exit ends, with
// its acceptance found under the old tag, which is no speaker error. Each
// of these refusals misses the target, and every other history scores
// perfectly.
func TestAccuracyLiveRefusals(t *testing.T) {
	update.Isolate(t)
	accounting, replace, acceptance := accuracy.AccountingTwoPasses().Name, accuracy.ReplaceQuestion().Name, accuracy.TwoSessionAcceptance().Name
	for _, tc := range []struct {
		history string
		pass    int
		answer  func(t *testing.T, correct []byte) update.Result
		want    []string
	}{
		{accounting, 2, func(t *testing.T, correct []byte) update.Result {
			// The correct answer cut to its first item, which marks the parser
			// done: the page's 3 other ids get no action.
			var out map[string]any
			if err := json.Unmarshal(correct, &out); err != nil {
				t.Fatal(err)
			}
			out["items"] = out["items"].([]any)[:1]
			data, err := json.Marshal(out)
			if err != nil {
				t.Fatal(err)
			}
			return update.Result{Output: data}
		}, []string{
			"the answer was refused: invalid_output: update: output rejected: 3 of the page's 4 item ids have no action",
			"MARTIN-1 is still in force", "MARTIN-3 is still in force",
		}},
		{replace, 3, func(*testing.T, []byte) update.Result { return update.Result{Reason: update.ReasonInvalidOutput} }, []string{
			"the answer was refused: invalid_output",
			"GANNET-2 is still in force", "TERN-7 is still in force", "TERN-7 is tagged stated, not accepted",
		}},
		{acceptance, 2, func(*testing.T, []byte) update.Result { return update.Result{Reason: update.ReasonToolUsed} }, []string{
			"the answer was refused: tool_used",
			"DOTTEREL-D is still in force", "DOTTEREL-W is still in force", "DOTTEREL-W is missing",
		}},
	} {
		t.Run(tc.history, func(t *testing.T) {
			ask := func(ctx context.Context, s *accuracy.Script, n int, b update.Batch, page threadpage.Page) (update.Result, error) {
				correct := update.CannedAnswer(t, s.Name, n, b, page, false)
				if s.Name == tc.history && n == tc.pass {
					return tc.answer(t, correct), nil
				}
				return update.Result{Output: correct}, nil
			}
			res, err := live.Run(context.Background(), update.HostClaude, accuracy.Scripts(), ask, func(h live.History) {
				if h.Script.Name != tc.history && (!h.Metrics.Perfect() || len(h.Missed)+len(h.Refused) != 0) {
					t.Errorf("%s: metrics %+v, missed %v, refused %v", h.Script.Name, h.Metrics, h.Missed, h.Refused)
				}
			})
			if err != nil {
				t.Fatal(err)
			}
			tl := res.Tally
			var want []string
			for _, w := range tc.want {
				want = append(want, fmt.Sprintf("content, pass %d: %s", tc.pass, w))
			}
			if got := tl.Of(tc.history); !slices.Equal(got, want) || res.Passes != 21 || res.Refused != 1 || len(tl.Content) != len(want) ||
				len(tl.Enforceable)+len(tl.Speaker)+len(tl.Date) != 0 || tl.Meets() {
				t.Errorf("%d of %d refused; missed %q, want %q; enforceable %v, speaker %v, date %v; meets the target %v",
					res.Refused, res.Passes, got, want, tl.Enforceable, tl.Speaker, tl.Date, tl.Meets())
			}
		})
	}
}
