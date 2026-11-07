// Package live runs the update's accuracy suite as the update job runs its
// passes: each fact script's chats stored as one host's hooks store them,
// each pass's batch built for the page the passes before it made, the
// answer an Ask gives checked by the update's validator and applied with
// threadpage.Apply, and the page measured after every pass, all tallied
// against the suite's live target. cmd/live-accuracy asks a host's real
// update command; the update's tests ask its canned outputs. Nothing else
// imports this package, so it never ships in eva-md.
package live

import (
	"context"
	"fmt"
	"time"

	"github.com/heyNag/eva-md/internal/threadpage"
	"github.com/heyNag/eva-md/internal/update"
	"github.com/heyNag/eva-md/internal/update/accuracy"
)

// Ask gets the answer to pass n of script s, numbered from 1: the model's
// structured output for batch b, built for page. A Result with a Reason is
// a run that failed; an error is one that could not run, or a context that
// ended.
type Ask func(ctx context.Context, s *accuracy.Script, n int, b update.Batch, page threadpage.Page) (update.Result, error)

// History is how one fact script's run ended.
type History struct {
	Script  *accuracy.Script
	Metrics accuracy.Metrics // of the page after the last pass
	Page    threadpage.Page  // after the last pass
	Refused []string         // each answer refused, with its pass and why
	Missed  []string         // what the tally counts against the script, with each line's kind and pass, refused answers included
}

// Result is a whole run of the suite.
type Result struct {
	Tally   accuracy.Tally
	Passes  int // the passes answered
	Refused int // of those, the answers the runner or the validator refused
}

// Stopped is a run that stopped before it could be judged: the host failed
// in a way that says nothing of its answers, such as auth, rate_limit or
// timeout, or answered with a model it wasn't asked for.
type Stopped struct {
	History string
	Pass    int
	Reason  string
}

func (s *Stopped) Error() string {
	return fmt.Sprintf("%s, pass %d: %s; the run stopped before it could be judged", s.History, s.Pass, s.Reason)
}

// Run runs the fact scripts, normally the suite's, on host, each pass's
// answer from ask, and calls done with each history as it ends. An answer
// the runner refuses as invalid_output or tool_used, or the validator
// refuses, leaves the page and its as_of as they were, as a failed pass
// leaves them in the job, and counts as refused, and in the tally as a
// content obligation missed. The page is still measured at the refused
// chat's time, so each fact the chat says and each value it ends shows as
// missed, as content whatever its kind: the page never took that chat. From
// then on the script's later passes aren't held to the out-of-order guard
// its passes expect, since their page is older than the script assumes. Any
// other failure stops the run with a *Stopped error.
func Run(ctx context.Context, host string, scripts []*accuracy.Script, ask Ask, done func(History)) (Result, error) {
	var res Result
	for _, s := range scripts {
		h, err := script(ctx, host, ask, s, &res)
		if err != nil {
			return res, err
		}
		done(h)
	}
	return res, nil
}

// script runs one fact script's passes.
func script(ctx context.Context, host string, ask Ask, s *accuracy.Script, res *Result) (History, error) {
	h := History{Script: s}
	var page threadpage.Page
	// asOf is the page's as_of, which only an applied pass moves; at is the
	// newest entry of the chats seen, where the page is measured.
	var asOf, at time.Time
	carried := 0
	seen, refused := map[int]bool{}, map[int]bool{}
	for i, ps := range s.Passes {
		n := i + 1
		b, err := batch(s, ps.Chat, host, page, asOf)
		if err != nil {
			return h, fmt.Errorf("%s, pass %d: %w", s.Name, n, err)
		}
		res.Tally.Enforce(s.Name, n, s.CheckInput(ps.Chat, b.Input, b.Branch, texts(b)))
		seen[ps.Chat], at = true, later(at, b.Last)
		out, err := ask(ctx, s, n, b, page)
		if err != nil {
			return h, err
		}
		res.Passes++
		why := out.Reason
		switch why {
		case update.ReasonInvalidOutput, update.ReasonToolUsed:
		case "":
			u, r, err := update.Validate(out.Output, b, page)
			if err != nil {
				why = update.ReasonInvalidOutput + ": " + err.Error()
				break
			}
			next, _ := threadpage.Apply(page, u)
			res.Tally.Enforce(s.Name, n, broken(s, ps, len(refused) == 0, page, next, b, u, r))
			page, asOf, carried = next, later(asOf, b.Last), carried+r.Carried
		default:
			return h, &Stopped{History: s.Name, Pass: n, Reason: why}
		}
		if why != "" {
			res.Refused++
			refused[ps.Chat] = true
			h.Refused = append(h.Refused, fmt.Sprintf("pass %d: %s", n, why))
			res.Tally.Refuse(s.Name, n, why)
		}
		h.Metrics = s.Measure(page, carried, seen, at)
		unanswered(s, h.Metrics.Misses, refused)
		res.Tally.Measure(s.Name, n, h.Metrics)
	}
	h.Page, h.Missed = page, res.Tally.Of(s.Name)
	return h, nil
}

// unanswered counts as content each miss of a fact said in a chat whose
// answer was refused. The page never took that chat, so such a fact found
// under an old tag or date, as a proposal the chat accepts, is a value the
// refused answer left as it was, not a speaker or date error.
func unanswered(s *accuracy.Script, misses []accuracy.Miss, refused map[int]bool) {
	for i, m := range misses {
		if refused[s.Facts[m.Fact].Session] {
			misses[i].Kind = accuracy.KindContent
		}
	}
}

// broken is what a validated pass of s broke, from the page before to the
// page after, with the update and report Validate gave for batch b: the
// out-of-order guard against the script's pass when every earlier pass of
// the script was applied, as the pass's Kept assumes, the obligations on
// the script's facts and the validator's own guarantees.
func broken(s *accuracy.Script, ps accuracy.Pass, applied bool, before, after threadpage.Page, b update.Batch, u threadpage.Update, r update.Report) []string {
	var missed []string
	if applied && r.KeptNow != ps.Kept {
		missed = append(missed, fmt.Sprintf("the out-of-order guard kept the Now: %v, want %v", r.KeptNow, ps.Kept))
	}
	missed = append(missed, s.Enforce(before, after)...)
	return append(missed, update.Guarantees(before, after, b, u)...)
}

// batch builds the batch of chat i of s, stored as host's hooks store it,
// for page, whose as_of is asOf, as the job reads a session file.
func batch(s *accuracy.Script, i int, host string, page threadpage.Page, asOf time.Time) (update.Batch, error) {
	c := s.Chat(i, host)
	data, err := c.JSONL()
	if err != nil {
		return update.Batch{}, err
	}
	return update.Build(update.Decode(data, 0, 1), page, update.Options{Branch: c.Branch, AsOf: asOf})
}

// texts are the texts of the batch's numbered entries, in order.
func texts(b update.Batch) []string {
	out := make([]string, len(b.Entries))
	for i, e := range b.Entries {
		out[i] = e.Text
	}
	return out
}

func later(a, b time.Time) time.Time {
	if b.After(a) {
		return b
	}
	return a
}
