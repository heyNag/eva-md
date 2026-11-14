// Package arm runs one arm of the contract spike: one host, one model
// tier, one effort and one form of the contract, over every script, against
// any answering function. It records each call's answer as it goes,
// replays a recording with no call, scores each store with the fidelity
// scorer, and turns a run into result rows. It is a throwaway, deleted
// with internal/eval/spike when DV-21 lands; no release builds it. SPEC.md
// is its contract.
package arm

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"regexp"
	"slices"

	"github.com/heyNag/eva-md/internal/eval/scenario"
	"github.com/heyNag/eva-md/internal/eval/score"
	"github.com/heyNag/eva-md/internal/eval/spike"
)

// The tiers an arm may name. A model's name is never a field: the tier is
// all an arm says of its model.
const (
	TierSmall  = "small"
	TierMiddle = "middle"
	TierLarge  = "large"
)

// Tiers are the tiers, smallest first.
var Tiers = []string{TierSmall, TierMiddle, TierLarge}

// Efforts are the efforts an arm may name: those of either host's table.
var Efforts = []string{"minimal", "low", "medium", "high", "xhigh", "max"}

// The kinds of call.
const (
	KindFirst  = "first"
	KindRepair = "repair"
)

// MaxOutput is the most bytes of an answer an arm keeps. A longer answer
// is kept as no answer, which reads as not the schema's JSON.
const MaxOutput = 1 << 20

// Arm is one host, one model tier, one effort and one form of the
// contract, run Repeat times over every script.
type Arm struct {
	Host, HostVersion, Tier, Effort string
	Form                            spike.Form
	// Repeat is how many rounds are run; 0 runs one.
	Repeat int
}

// Call is one call an arm makes: the script by id, its pass and round,
// both from 1, whether it is the first call of the pass or its repair
// call, and what the model is given.
type Call struct {
	Script      string
	Pass, Round int
	Kind        string
	// System is the prompt, spike.Prompt of the arm's form.
	System string
	// Input is the rendered pass for a first call, its repair input for a
	// repair call; Schema the answer's schema or the repair answer's.
	Input, Schema []byte
}

// Reply is what the host's runner reports of a call. A reply with a Reason
// is a failed call: its Output is not read, it is scored as no answer, and
// the run goes on. CachedIn is nil when the runner reports no cached
// input; TokensIn counts cached input too. Effort is the effort the
// runner used.
type Reply struct {
	Output                        []byte
	Reason                        string
	TokensIn, TokensOut, Attempts int
	CachedIn                      *int
	Seconds                       float64
	Effort                        string
}

// Ask answers one call. An error stops the run; a failed call is a Reply
// with a Reason.
type Ask func(ctx context.Context, c Call) (Reply, error)

// Result is what a run came to: its arm, and each round's scripts and
// sums, in the run's order.
type Result struct {
	Arm    Arm
	Rounds []Round
}

// Round is one round over every script: each script's result, in the
// order given, and On and Off over all of them.
type Round struct {
	Scripts []ScriptResult
	On, Off Side
}

// ScriptResult is one script in one round: its id, and its two stores.
type ScriptResult struct {
	Script  string
	On, Off Side
}

// Side is what one store came to. On applies each pass's final verdicts,
// Off only what each pass's first check accepted, and counts the run as if
// no repair call had been made: its tally reads the first verdicts and its
// calls are the first calls.
type Side struct {
	Tally spike.Tally
	Calls Calls
	// Items are the store's records once the script's last pass is
	// applied; nil over all scripts.
	Items []spike.Item
	// Fidelity is the store scored by the fidelity scorer; over all
	// scripts, only its Metrics are set, pooled.
	Fidelity score.Result
	// Required, Held and Recalled count the facts that must be stored and
	// are still in force, and of those the ones that hold and the ones
	// found or holding.
	Required, Held, Recalled int
	// Honest counts the operations refused that the scorer finds support
	// a fact, HonestOf those and the accepted ones of the user's authority
	// whose item supports one: honest_refused is their share.
	Honest, HonestOf int
}

// Calls are a side's calls and what they cost.
type Calls struct {
	First, Repair, Failed int
	TokensIn, TokensOut   int
	RepairTokensIn        int
	RepairCachedIn        int
	RepairCachedReported  int // repair calls whose runner reported cached input
	Seconds               float64
}

// idPattern is a results id, as internal/eval/results checks a row's;
// versionPattern a host's version.
var (
	idPattern      = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}$`)
	versionPattern = regexp.MustCompile(`^[0-9A-Za-z][0-9A-Za-z.+_-]{0,63}$`)
)

// check reports what is wrong with a; the host's version is checked when
// version is set or a gives one.
func (a Arm) check(version bool) error {
	switch {
	case !idPattern.MatchString(a.Host):
		return errors.New("arm: the host is not an id")
	case (version || a.HostVersion != "") && !versionPattern.MatchString(a.HostVersion):
		return errors.New("arm: the host's version is not a version")
	case !slices.Contains(Tiers, a.Tier):
		return errors.New("arm: unknown tier")
	case !slices.Contains(Efforts, a.Effort):
		return errors.New("arm: unknown effort")
	case !a.Form.Valid():
		return errors.New("arm: unknown form")
	case a.Repeat < 0:
		return errors.New("arm: a negative repeat")
	}
	return nil
}

// rounds is how many rounds a runs.
func (a Arm) rounds() int { return max(a.Repeat, 1) }

// check reports a reply whose figures are out of range or whose words are
// not ids, which no recording or row may hold.
func (r Reply) check() error {
	switch {
	case r.Reason != "" && !idPattern.MatchString(r.Reason):
		return errors.New("the reason is not a short id")
	case r.Effort != "" && !idPattern.MatchString(r.Effort):
		return errors.New("the effort used is not an id")
	case r.TokensIn < 0 || r.TokensOut < 0 || r.Attempts < 0 || (r.CachedIn != nil && *r.CachedIn < 0):
		return errors.New("a count is negative")
	case math.IsNaN(r.Seconds) || math.IsInf(r.Seconds, 0) || r.Seconds < 0:
		return errors.New("the seconds are not a finite figure of at least 0")
	}
	return nil
}

// Run runs arm over scripts, asking ask, and writes each call to rec as
// one line, as it goes; rec may be nil. For each round, each script and
// each of its passes, in order: it renders the pass with what the script's
// On store shows, asks the first call, checks the answer and, when an
// operation may be repaired, asks one repair call. On applies the final
// verdicts, Off only what the first check accepted. An error from ask, a
// reply out of range, a recording it cannot write or a replay that leaves
// a line unused stops the run; nothing is returned then.
func Run(ctx context.Context, arm Arm, scripts []spike.Script, ask Ask, rec io.Writer) (Result, error) {
	if err := arm.check(true); err != nil {
		return Result{}, err
	}
	if ask == nil {
		return Result{}, errors.New("arm: no answering function")
	}
	if len(scripts) == 0 {
		return Result{}, errors.New("arm: no script")
	}
	seen := map[string]bool{}
	for _, s := range scripts {
		if err := s.Validate(); err != nil {
			return Result{}, fmt.Errorf("arm: %w", err)
		}
		id := s.Scenario.ID
		if seen[id] {
			return Result{}, fmt.Errorf("arm: script %s is given twice", id)
		}
		seen[id] = true
		if !idPattern.MatchString(scenarioID(arm, "off", id)) {
			return Result{}, fmt.Errorf("arm: script %s makes a row's scenario too long", id)
		}
	}
	fin := &finishers{}
	r := runner{arm: arm, ask: ask, rec: rec, ctx: context.WithValue(ctx, finishKey{}, fin)}
	res := Result{Arm: arm}
	for round := 1; round <= arm.rounds(); round++ {
		var rd Round
		for _, s := range scripts {
			sr, err := r.script(round, s, &rd)
			if err != nil {
				return Result{}, err
			}
			rd.Scripts = append(rd.Scripts, sr)
		}
		rd.On.pool(rd.Scripts, func(sr ScriptResult) Side { return sr.On })
		rd.Off.pool(rd.Scripts, func(sr ScriptResult) Side { return sr.Off })
		res.Rounds = append(res.Rounds, rd)
	}
	for _, check := range fin.checks {
		if err := check(); err != nil {
			return Result{}, err
		}
	}
	return res, nil
}

// finishKey keys the finishers in the context Run gives ask: a replay
// adds its check that no line was left unused, which Run makes once every
// call is made.
type finishKey struct{}

type finishers struct{ checks []func() error }

// runner is one run's state.
type runner struct {
	arm Arm
	ask Ask
	rec io.Writer
	ctx context.Context
}

// store is one side's store as a script's passes fill it.
type store struct {
	st spike.Store
	// kept are the records made by the accepted operations of the user's
	// authority, by place in st.Items().
	kept []int
	// refused are the operations refused that cite a user's shown entry,
	// each as an item alone.
	refused []score.Item
}

// script runs every pass of s in round, and adds each pass to rd's tallies.
func (r *runner) script(round int, s spike.Script, rd *Round) (ScriptResult, error) {
	sc := s.Scenario
	sr := ScriptResult{Script: sc.ID}
	var on, off store
	// twin is the handle in off of each record of on that off holds too.
	twin := map[string]string{}
	for n, p := range sc.Passes {
		sess := sc.Sessions[p.Session]
		shown := on.st.Shown()
		in, err := spike.Render(sess, shown, r.arm.Form)
		if err != nil {
			return ScriptResult{}, fmt.Errorf("arm: script %s pass %d: %w", sc.ID, n+1, err)
		}
		o, failed, err := r.pass(round, s, n+1, in, shown, &sr.On.Calls, &sr.Off.Calls)
		if err != nil {
			return ScriptResult{}, err
		}
		if failed {
			continue
		}
		offOutcome := spike.Outcome{Script: s, Input: in, Invalid: o.Invalid, First: o.First}
		sr.On.Tally.Add(o)
		rd.On.Tally.Add(o)
		sr.Off.Tally.Add(offOutcome)
		rd.Off.Tally.Add(offOutcome)
		if o.Invalid {
			continue
		}
		final := o.First
		if o.RepairCalled {
			final = o.Final
		}
		at, err := times(sess)
		if err != nil {
			return ScriptResult{}, err
		}
		onMade, err := on.apply(sess, final)
		if err != nil {
			return ScriptResult{}, fmt.Errorf("arm: script %s pass %d: %w", sc.ID, n+1, err)
		}
		// Off applies the operations the first check accepted, each naming
		// off's record where on's is named. An operation whose target off
		// does not hold, since only a repair made it, changes nothing.
		var offV spike.Verdict
		var offIdx []int
		for i, j := range o.First.Ops {
			if !j.Accepted() {
				continue
			}
			if j.Op.ID != nil {
				h, ok := twin[*j.Op.ID]
				if !ok {
					continue
				}
				j.Op.ID = &h
			}
			offV.Ops = append(offV.Ops, j)
			offIdx = append(offIdx, i)
		}
		offMade, err := off.apply(sess, offV)
		if err != nil {
			return ScriptResult{}, fmt.Errorf("arm: script %s pass %d: %w", sc.ID, n+1, err)
		}
		onItems, offItems := on.st.Items(), off.st.Items()
		for k, i := range offIdx {
			if m, ok := offMade[k]; ok {
				twin[onItems[onMade[i]].Handle] = offItems[m].Handle
			}
		}
		refuse(&off, o.First, in, at)
		refuse(&on, final, in, at)
	}
	required := mustHold(sc)
	sr.On.score(sc, &on, required)
	sr.Off.score(sc, &off, required)
	return sr, nil
}

// pass asks pass n of s in round, rendered as in with shown, and checks
// what comes back. It adds each call to on, and the first to off. failed
// is a first call that failed, which leaves the pass with no outcome.
func (r *runner) pass(round int, s spike.Script, n int, in spike.Input, shown []spike.Shown, on, off *Calls) (spike.Outcome, bool, error) {
	f := r.arm.Form
	o := spike.Outcome{Script: s, Input: in}
	system := spike.Prompt(f)
	c := Call{Script: s.Scenario.ID, Pass: n, Round: round, Kind: KindFirst, System: system, Input: in.Bytes, Schema: spike.Schema(f)}
	reply, out, err := r.call(c)
	if err != nil {
		return o, false, err
	}
	on.add(reply, false)
	off.add(reply, false)
	if reply.Reason != "" {
		return o, true, nil
	}
	a, err := spike.ParseAnswer(out, f)
	if err != nil {
		o.Invalid = true
		return o, false, nil
	}
	o.First = spike.Check(in, shown, a, f)
	o.Final = o.First
	if len(o.First.ToRepair()) == 0 {
		return o, false, nil
	}
	input := spike.RepairInput(in.Bytes, out, o.First)
	o.RepairCalled = true
	o.RepairInputBytes, o.RepairPrefixBytes = len(system)+len(input), len(system)+len(in.Bytes)
	c.Kind, c.Input, c.Schema = KindRepair, input, spike.RepairSchema(f)
	reply, out, err = r.call(c)
	if err != nil {
		return o, false, err
	}
	on.add(reply, true)
	var fix spike.Repair
	if reply.Reason == "" {
		if fix, err = spike.ParseRepair(out, f); err != nil {
			o.RepairInvalid, fix = true, spike.Repair{}
		}
	}
	o.Final = spike.ApplyRepair(in, shown, o.First, fix, f)
	return o, false, nil
}

// call asks c, checks the reply, keeps its answer and records the call.
// The answer is nil for a failed call and for one that gave no JSON.
func (r *runner) call(c Call) (Reply, []byte, error) {
	reply, err := r.ask(r.ctx, c)
	if err != nil {
		return Reply{}, nil, fmt.Errorf("arm: script %s round %d pass %d, %s call: %w", c.Script, c.Round, c.Pass, c.Kind, err)
	}
	if err := reply.check(); err != nil {
		return Reply{}, nil, fmt.Errorf("arm: script %s round %d pass %d, %s call: %w", c.Script, c.Round, c.Pass, c.Kind, err)
	}
	var out []byte
	if reply.Reason == "" {
		out = keep(reply.Output)
	}
	if r.rec != nil {
		if err := record(r.rec, r.arm, c, reply, out); err != nil {
			return Reply{}, nil, err
		}
	}
	return reply, out, nil
}

// add counts one call.
func (c *Calls) add(r Reply, repair bool) {
	if repair {
		c.Repair++
		c.RepairTokensIn += r.TokensIn
		if r.CachedIn != nil {
			c.RepairCachedIn += *r.CachedIn
			c.RepairCachedReported++
		}
	} else {
		c.First++
	}
	if r.Reason != "" {
		c.Failed++
	}
	c.TokensIn += r.TokensIn
	c.TokensOut += r.TokensOut
	c.Seconds += r.Seconds
}

// pool sums each script's side, as get gives it, into s, whose tally the
// run filled pass by pass: the counts added, and the two shares of facts
// over every script's facts that must hold.
func (s *Side) pool(scripts []ScriptResult, get func(ScriptResult) Side) {
	m := &s.Fidelity.Metrics
	for _, sr := range scripts {
		x := get(sr)
		c := x.Calls
		s.Calls.First += c.First
		s.Calls.Repair += c.Repair
		s.Calls.Failed += c.Failed
		s.Calls.TokensIn += c.TokensIn
		s.Calls.TokensOut += c.TokensOut
		s.Calls.RepairTokensIn += c.RepairTokensIn
		s.Calls.RepairCachedIn += c.RepairCachedIn
		s.Calls.RepairCachedReported += c.RepairCachedReported
		s.Calls.Seconds += c.Seconds
		s.Required += x.Required
		s.Held += x.Held
		s.Recalled += x.Recalled
		s.Honest += x.Honest
		s.HonestOf += x.HonestOf
		xm := x.Fidelity.Metrics
		m.WrongWrite += xm.WrongWrite
		m.Noise += xm.Noise
		m.Misattributed += xm.Misattributed
		m.StaleInForce += xm.StaleInForce
		m.Dupes += xm.Dupes
		m.OTRStored += xm.OTRStored
		m.Violations += xm.Violations
	}
	m.FactRecall, m.FactHolds = share(s.Recalled, s.Required), share(s.Held, s.Required)
}

// share is n of all, 1 when there is none, as the scorer's shares are.
func share(n, of int) float64 {
	if of == 0 {
		return 1
	}
	return float64(n) / float64(of)
}

// mustHold gives the facts of sc that must be stored and are still in
// force, as the scorer judges them: in an empty store, these and only
// these are missing.
func mustHold(sc scenario.Scenario) []int {
	var facts []int
	for i, fr := range score.Score(sc, score.Export{}).Facts {
		if fr.Outcome == score.Missing {
			facts = append(facts, i)
		}
	}
	return facts
}
