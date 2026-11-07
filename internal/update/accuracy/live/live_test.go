package live

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/heyNag/eva-md/internal/threadpage"
	"github.com/heyNag/eva-md/internal/update"
	"github.com/heyNag/eva-md/internal/update/accuracy"
)

func isolate(t *testing.T) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(home, "gitconfig"))
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
}

// empty is an answer the validator accepts that changes nothing.
const empty = `{"summary":"","last_chat":"","done":[],"items":[],"timeline":[],"unknowns":[],"rules":[],"knowledge":[]}`

// yaml is the reversal's correct first answer: the decision its first chat
// states, citing its entry.
const yaml = `{"summary":"","last_chat":"","done":[],"items":[{"id":null,"kind":"decision","action":"add","text":"Store pages as YAML, OSPREY-Y","tag":"stated","refs":[1],"reason":null,"replacement":null}],"timeline":[],"unknowns":[],"rules":[],"knowledge":[]}`

// answers answers each pass from the table, by history and pass, and with
// the empty answer otherwise, and records what it was asked and the as_of
// of the page each batch was built for, by history and pass.
type answers struct {
	t     *testing.T
	table map[string]map[int]update.Result
	asked []string
	asOf  map[string]time.Time
}

func (a *answers) ask(ctx context.Context, s *accuracy.Script, n int, b update.Batch, page threadpage.Page) (update.Result, error) {
	a.asked = append(a.asked, s.Name)
	if a.asOf == nil {
		a.asOf = map[string]time.Time{}
	}
	a.asOf[fmt.Sprintf("%s, pass %d", s.Name, n)] = b.AsOf
	if len(b.Entries) == 0 || b.Host != update.HostCodex {
		a.t.Errorf("%s, pass %d: a batch of %d entries from %q", s.Name, n, len(b.Entries), b.Host)
	}
	if res, ok := a.table[s.Name][n]; ok {
		return res, nil
	}
	return update.Result{Output: []byte(empty)}, nil
}

// A refused answer, whether the runner or the validator refuses it, leaves
// the page as it was, the page's as_of with it, so the next pass's batch is
// built for the page as it stood, and counts as refused; the run goes on to
// every other pass. It costs a content obligation of its own, and the page
// is measured at the refused chat's time: the reversal's second chat,
// refused after a correct first answer, misses its new decision, and its
// old one, which only that chat ended, is still in force.
func TestRunCountsRefusedAnswers(t *testing.T) {
	isolate(t)
	a := &answers{t: t, table: map[string]map[int]update.Result{
		"a reversal":   {1: {Output: []byte(yaml)}, 2: {Output: []byte("not json")}},
		"a correction": {1: {Reason: update.ReasonInvalidOutput}, 2: {Reason: update.ReasonToolUsed}},
	}}
	histories := map[string]History{}
	res, err := Run(context.Background(), update.HostCodex, accuracy.Scripts(), a.ask, func(h History) { histories[h.Script.Name] = h })
	if err != nil {
		t.Fatal(err)
	}
	if len(a.asked) != 21 || res.Passes != 21 || res.Refused != 3 || len(histories) != 12 {
		t.Fatalf("%d asked, %d passes, %d refused, %d histories", len(a.asked), res.Passes, res.Refused, len(histories))
	}
	r := histories["a reversal"]
	if len(r.Refused) != 1 || !strings.HasPrefix(r.Refused[0], "pass 2: invalid_output: ") || len(r.Missed) != 3 ||
		r.Missed[0] != "content, pass 2: the answer was refused: "+strings.TrimPrefix(r.Refused[0], "pass 2: ") ||
		!slices.Equal(r.Missed[1:], []string{"content, pass 2: OSPREY-Y is still in force", "content, pass 2: OSPREY-J is missing"}) || len(r.Page.Now.Decisions) != 1 {
		t.Errorf("the reversal: refused %q, missed %q, page\n%s", r.Refused, r.Missed, threadpage.Render(r.Page))
	}
	c := histories["a correction"]
	if !slices.Equal(c.Refused, []string{"pass 1: invalid_output", "pass 2: tool_used"}) ||
		!slices.Equal(c.Missed, []string{"content, pass 1: the answer was refused: invalid_output", "content, pass 1: WREN-2 is missing",
			"content, pass 2: the answer was refused: tool_used", "content, pass 2: WREN-5 is missing"}) {
		t.Errorf("the correction: refused %q, missed %q", c.Refused, c.Missed)
	}
	if asOf, ok := a.asOf["a correction, pass 2"]; !ok || !asOf.IsZero() {
		t.Errorf("the correction's second batch was built for a page as of %v, after its first answer was refused", asOf)
	}
	if res.Tally.Meets() || len(res.Tally.Enforceable) != 0 {
		t.Errorf("a run of empty answers: meets the target %v, enforceable %v", res.Tally.Meets(), res.Tally.Enforceable)
	}
}

// A refused answer costs a content obligation of its own, on top of its
// chat's facts: a refused answer to a chat of one fact costs 2, which the
// target allows once, and two such refusals miss it, though every answer
// the validator took was right.
func TestARefusedAnswerCountsAgainstTheTarget(t *testing.T) {
	isolate(t)
	right := `{"summary":"","last_chat":"","done":[],"items":[{"id":null,"kind":"decision","action":"add","text":"Switch to the streaming parser if scripts/benchmark.py passes 300 ms, CONDOR-8","tag":"stated","refs":[1],"reason":null,"replacement":null}],"timeline":[],"unknowns":[],"rules":[],"knowledge":[]}`
	for _, tc := range []struct {
		refused int
		meets   bool
	}{{0, true}, {1, true}, {2, false}} {
		var scripts []*accuracy.Script
		for i := 0; i < 2; i++ {
			s := accuracy.Conditional()
			s.Name = fmt.Sprintf("%s %d", s.Name, i+1)
			scripts = append(scripts, s)
		}
		n := 0
		res, err := Run(context.Background(), update.HostClaude, scripts, func(context.Context, *accuracy.Script, int, update.Batch, threadpage.Page) (update.Result, error) {
			if n++; n <= tc.refused {
				return update.Result{Reason: update.ReasonInvalidOutput}, nil
			}
			return update.Result{Output: []byte(right)}, nil
		}, func(History) {})
		if err != nil {
			t.Fatal(err)
		}
		if res.Refused != tc.refused || len(res.Tally.Content) != 2*tc.refused || len(res.Tally.Enforceable)+len(res.Tally.Speaker)+len(res.Tally.Date) != 0 || res.Tally.Meets() != tc.meets {
			t.Errorf("%d refused: refused %d, content %v, enforceable %v, speaker %v, date %v; meets the target %v", tc.refused, res.Refused,
				res.Tally.Content, res.Tally.Enforceable, res.Tally.Speaker, res.Tally.Date, res.Tally.Meets())
		}
	}
}

// Once a script's answer is refused, its later pages are older than its
// passes expect, so they aren't held to the out-of-order guard the script
// expects; the validator's own guarantees still hold them. Here the
// overlapping sessions' first two answers are refused, so the third chat,
// which predates the page the script expects, meets a page with no as_of,
// and its answer that changes nothing breaks nothing. The value the first
// chat replaced isn't missed: it stopped holding by the second chat's time.
func TestARefusalLeavesTheGuardUnjudged(t *testing.T) {
	isolate(t)
	s := accuracy.OverlappingSessions()
	a := &answers{t: t, table: map[string]map[int]update.Result{s.Name: {1: {Reason: update.ReasonInvalidOutput}, 2: {Reason: update.ReasonToolUsed}}}}
	res, err := Run(context.Background(), update.HostCodex, []*accuracy.Script{s}, a.ask, func(History) {})
	if err != nil {
		t.Fatal(err)
	}
	if !s.Passes[2].Kept || !a.asOf[s.Name+", pass 3"].IsZero() || len(a.asked) != 3 || res.Refused != 2 || len(res.Tally.Enforceable) != 0 {
		t.Fatalf("the third batch's page as of %v; %d asked, %d refused; enforceable %v", a.asOf[s.Name+", pass 3"], len(a.asked), res.Refused, res.Tally.Enforceable)
	}
	if got, want := res.Tally.Of(s.Name), []string{
		"content, pass 1: the answer was refused: invalid_output", "content, pass 1: KITE-5 is missing",
		"content, pass 2: the answer was refused: tool_used", "content, pass 2: KITE-LOG is missing",
		"content, pass 3: KITE-COLD is not logged",
	}; !slices.Equal(got, want) {
		t.Fatalf("missed %q, want %q", got, want)
	}
}

// A failure that says nothing of the answers, such as a rate limit, stops
// the run at once, before the histories after it, and names where and why;
// so does an answer that could not be asked for at all.
func TestRunStops(t *testing.T) {
	isolate(t)
	limited := &answers{t: t, table: map[string]map[int]update.Result{"a correction": {2: {Reason: update.ReasonRateLimit}}}}
	var ended []string
	_, err := Run(context.Background(), update.HostCodex, accuracy.Scripts(), limited.ask, func(h History) { ended = append(ended, h.Script.Name) })
	var stopped *Stopped
	if !errors.As(err, &stopped) || *stopped != (Stopped{History: "a correction", Pass: 2, Reason: update.ReasonRateLimit}) ||
		err.Error() != "a correction, pass 2: rate_limit; the run stopped before it could be judged" {
		t.Fatalf("err %v", err)
	}
	if !slices.Equal(ended, []string{"an assistant's suggestion against the user's decision", "a weakened event date", "a reversal"}) || len(limited.asked) != 6 {
		t.Fatalf("ended %q after %d answers", ended, len(limited.asked))
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	n := 0
	_, err = Run(ctx, update.HostCodex, accuracy.Scripts(), func(ctx context.Context, s *accuracy.Script, _ int, _ update.Batch, _ threadpage.Page) (update.Result, error) {
		n++
		return update.Result{}, ctx.Err()
	}, func(History) { t.Error("a history ended") })
	if !errors.Is(err, context.Canceled) || n != 1 {
		t.Fatalf("err %v after %d answers", err, n)
	}
}

// Each enforceable obligation a pass breaks reaches the tally at its pass.
// Here a script is changed so that its chat no longer says one fact as it
// lists it, the user's decision counts as the assistant's, and the pass
// expects the out-of-order guard to keep the Now; the answer that is right
// for the chat then breaks all three.
func TestRunTalliesBrokenObligations(t *testing.T) {
	isolate(t)
	s := accuracy.SuggestionAgainstDecision()
	s.Facts[0].Speaker = "assistant"
	s.Facts[1].Says = "I could switch the lock to a pid file, PIDGEON-7."
	s.Passes[0].Kept = true
	decision := `{"summary":"","last_chat":"","done":[],"items":[{"id":null,"kind":"decision","action":"add","text":"Use flock for the per-session lock, KESTREL-41","tag":"stated","refs":[1],"reason":null,"replacement":null}],"timeline":[],"unknowns":[],"rules":[],"knowledge":[]}`
	res, err := Run(context.Background(), update.HostClaude, []*accuracy.Script{s}, func(context.Context, *accuracy.Script, int, update.Batch, threadpage.Page) (update.Result, error) {
		return update.Result{Output: []byte(decision)}, nil
	}, func(History) {})
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, m := range res.Tally.Enforceable {
		got = append(got, m.String())
	}
	if want := []string{
		s.Name + ", pass 1: PIDGEON-7 didn't reach the batch as said, on branch main",
		s.Name + ", pass 1: the out-of-order guard kept the Now: false, want true",
		s.Name + ", pass 1: KESTREL-41 is a decision tagged stated",
	}; !slices.Equal(got, want) || res.Tally.Meets() {
		t.Fatalf("enforceable %q, want %q", got, want)
	}
}

// A miss of a fact said in a chat whose answer was refused counts as
// content, whatever its kind; the misses of the other chats' facts keep
// theirs.
func TestUnanswered(t *testing.T) {
	s := accuracy.ReplaceQuestion() // GANNET-2 in chat 0, TERN-7 in chat 1, its acceptance in chat 2
	misses := []accuracy.Miss{
		{Kind: accuracy.KindSpeaker, Fact: 2, Code: "TERN-7", What: "TERN-7 is tagged stated, not accepted"},
		{Kind: accuracy.KindSpeaker, Fact: 1, Code: "TERN-7", What: "TERN-7 is tagged inferred, not stated"},
		{Kind: accuracy.KindDate, Fact: 0, Code: "GANNET-2", What: "GANNET-2 is logged under 2026-01-12, not 2026-01-13"},
		{Kind: accuracy.KindContent, Fact: 2, Code: "TERN-7", What: "TERN-7 is in 2 Now items"},
	}
	unanswered(s, misses, map[int]bool{2: true})
	var kinds []string
	for _, m := range misses {
		kinds = append(kinds, m.Kind)
	}
	if want := []string{accuracy.KindContent, accuracy.KindSpeaker, accuracy.KindDate, accuracy.KindContent}; !slices.Equal(kinds, want) {
		t.Fatalf("kinds %q, want %q", kinds, want)
	}
}

// What a validated pass broke is the out-of-order guard against the pass,
// the obligations on the script's facts, then the validator's guarantees,
// such as no id leaving the page. The guard is judged only while every
// earlier pass of the script was applied.
func TestBroken(t *testing.T) {
	isolate(t)
	before := threadpage.Page{Now: threadpage.Now{Decisions: []threadpage.Item{{ID: "n1", Text: "Store pages as JSONL, OSPREY-J", Tag: "stated"}}}}
	after := threadpage.Page{Now: threadpage.Now{Decisions: []threadpage.Item{{ID: "n2", Text: "Store pages as YAML, OSPREY-Y", Tag: "stated"}}}}
	want := []string{"the out-of-order guard kept the Now: false, want true", "OSPREY-Y replaced the newer OSPREY-J", "id n1 left the page"}
	for _, applied := range []bool{true, false} {
		got := broken(accuracy.Reversal(), accuracy.Pass{Chat: 1, Kept: true}, applied, before, after, update.Batch{}, threadpage.Update{}, update.Report{})
		if !applied {
			want = want[1:]
		}
		if !slices.Equal(got, want) {
			t.Fatalf("every earlier pass applied %v: broken %q, want %q", applied, got, want)
		}
	}
}
