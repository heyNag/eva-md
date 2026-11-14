package conformance

import (
	"bytes"
	"fmt"
	"maps"
	"reflect"
	"slices"
	"testing"

	"github.com/heyNag/eva-md/internal/store"
)

// The properties, by the names PROPERTIES.md gives them.
const (
	PropCrashSafety = "CrashSafety"
	PropExactlyOnce = "ExactlyOnce"
	PropProtection  = "Protection"
	PropRestore     = "Restore"
	PropReplay      = "Replay"
	PropSessionKept = "SessionKept"
)

// LockedPasses is how many passes Protection aims at one locked item.
const LockedPasses = 1000

// everyStepFrom is the least run of seeds over which a crash property also
// wants an armed crash to have fired at every step of Apply.
const everyStepFrom = 200

// run checks seeds 1 to n, and stops at the first that fails.
func run(t testing.TB, n int, check func(seed int64) error) {
	t.Helper()
	if n < 1 {
		t.Fatal("no seed to check")
	}
	for seed := int64(1); seed <= int64(n); seed++ {
		if err := check(seed); err != nil {
			t.Fatal(err)
		}
	}
}

// crashed counts, by step, the armed crashes that fired over a run.
type crashed map[store.Step]int

// upTo is seeds 1 to n.
func upTo(n int) []int64 {
	var seeds []int64
	for seed := int64(1); seed <= int64(n); seed++ {
		seeds = append(seeds, seed)
	}
	return seeds
}

// runCrashes checks a property that crashes passes on each seed in turn,
// and stops at the first that fails. A store that dropped its fault would
// pass every check, so over the seeds an armed crash must have fired, and
// over everyStepFrom seeds or more, one at every step of Apply.
func runCrashes(prop string, seeds []int64, check func(seed int64, c crashed) error) error {
	if len(seeds) == 0 {
		return &Failure{Property: prop, Detail: "no seed to check"}
	}
	c := crashed{}
	for _, seed := range seeds {
		if err := check(seed, c); err != nil {
			return err
		}
	}
	last := seeds[len(seeds)-1]
	if len(c) == 0 {
		return &Failure{Property: prop, Seed: last, Detail: fmt.Sprintf("no armed crash fired in %d seeds", len(seeds))}
	}
	if len(seeds) >= everyStepFrom {
		for _, s := range store.Steps {
			if c[s] == 0 {
				return &Failure{Property: prop, Seed: last, Detail: fmt.Sprintf("no armed crash fired at %s in %d seeds", s, len(seeds))}
			}
		}
	}
	return nil
}

// fatal fails a test with err, when there is one.
func fatal(t testing.TB, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

// RunCrashSafety checks CrashSafety on seeds 1 to n, and that an armed
// crash fired over them: at every step when n is everyStepFrom or more.
func RunCrashSafety(t testing.TB, newSubject func() Subject, n int) {
	t.Helper()
	fatal(t, runCrashes(PropCrashSafety, upTo(n), func(seed int64, c crashed) error { return checkCrashSafety(newSubject, seed, c) }))
}

// RunExactlyOnce checks ExactlyOnce on seeds 1 to n, and that an armed
// crash fired over them: at every step when n is everyStepFrom or more.
func RunExactlyOnce(t testing.TB, newSubject func() Subject, n int) {
	t.Helper()
	fatal(t, runCrashes(PropExactlyOnce, upTo(n), func(seed int64, c crashed) error {
		return checkExactlyOnce(newSubject, seed, newSchedule(seed, options{edits: true}), c)
	}))
}

// RunProtection checks Protection on seeds 1 to n.
func RunProtection(t testing.TB, newSubject func() Subject, n int) {
	t.Helper()
	run(t, n, func(seed int64) error { return CheckProtection(newSubject, seed) })
}

// RunRestore checks Restore on seeds 1 to n, and that an armed crash fired
// over them: at every step when n is everyStepFrom or more.
func RunRestore(t testing.TB, newSubject func() Restorable, n int) {
	t.Helper()
	fatal(t, runCrashes(PropRestore, upTo(n), func(seed int64, c crashed) error { return checkRestore(newSubject, seed, c) }))
}

// RunReplay checks Replay on seeds 1 to n, and that an armed crash fired
// over them: at every step when n is everyStepFrom or more.
func RunReplay(t testing.TB, newSubject func() Restorable, n int) {
	t.Helper()
	fatal(t, runCrashes(PropReplay, upTo(n), func(seed int64, c crashed) error { return checkReplay(newSubject, seed, c) }))
}

// RunSessionKept checks SessionKept on seeds 1 to n.
func RunSessionKept(t testing.TB, newSubject func() Subject, n int) {
	t.Helper()
	run(t, n, func(seed int64) error { return CheckSessionKept(newSubject, seed) })
}

// CheckCrashSafety: after a crash at any step of a pass and a reopen, the
// files and history are those before the pass or those a twin that ran it
// uncrashed has; the pass run again then gives the twin's outcomes and
// state; and a pass Apply returned is never lost across a reopen.
func CheckCrashSafety(newSubject func() Subject, seed int64) error {
	return checkCrashSafety(newSubject, seed, crashed{})
}

func checkCrashSafety(newSubject func() Subject, seed int64, tally crashed) error {
	sch := newSchedule(seed, options{edits: true})
	a := newRunner(PropCrashSafety, newSubject(), seed)
	b := newRunner(PropCrashSafety, newSubject(), seed)
	a.crashed = tally
	if err := a.open(); err != nil {
		return err
	}
	defer a.close()
	if err := b.open(); err != nil {
		return err
	}
	defer b.close()
	for i, ev := range sch.events {
		a.step, b.step = i, i
		var ta, tb store.Txn
		switch ev.what {
		case evPass:
			ta, tb = a.derive(sch.record[ev.chunk]), b.derive(sch.record[ev.chunk])
		case evAck:
			var ok bool
			if ta, _, ok = a.ackTxn(ev); !ok {
				continue
			}
			tb, _, _ = b.ackTxn(ev)
		default:
			if err := a.run(sch, ev); err != nil {
				return err
			}
			if err := b.run(sch, ev); err != nil {
				return err
			}
			if got, want := a.snap(), b.snap(); !got.equal(want) {
				return a.fail("two subjects differ after the same hand edit: %s", got.diff(want))
			}
			continue
		}
		c := ev.crash
		if c == nil {
			c = &crash{step: store.Steps[(i+int(seed%7))%len(store.Steps)]}
		}
		before := a.snap()
		want, err := b.apply(tb, nil)
		if err != nil {
			return err
		}
		after := b.snap()
		known := a.known(ta.Pass)
		a.armed, a.fired = c, false
		_, err = a.st.Apply(ta)
		a.armed = nil
		switch {
		case a.fired && err == nil:
			return a.fail("Apply went on past a fault at %s %d", c.step, c.n)
		case !a.fired && err != nil:
			return a.fail("Apply: %v", err)
		}
		if err := a.reopen(); err != nil {
			return err
		}
		got := a.snap()
		if !a.fired {
			if err := a.unfired(c, known); err != nil {
				return err
			}
			if !got.equal(after) {
				return a.fail("a pass Apply returned is not there after a reopen: %s", got.diff(after))
			}
			continue
		}
		if !got.equal(before) && !got.equal(after) {
			return a.fail("after a crash at %s %d and a reopen, the store is neither as before the pass nor as after it: %s", c.step, c.n, got.diff(after))
		}
		res, err := a.apply(ta, nil)
		if err != nil {
			return err
		}
		if !reflect.DeepEqual(res.Outcomes, want.Outcomes) {
			return a.fail("the pass run again after a crash at %s %d gave %s; uncrashed it gave %s", c.step, c.n, results(res.Outcomes), results(want.Outcomes))
		}
		if err := a.reopen(); err != nil {
			return err
		}
		if got := a.snap(); !got.equal(after) {
			return a.fail("the pass run again after a crash at %s %d left another state: %s", c.step, c.n, got.diff(after))
		}
	}
	return nil
}

// results is outcomes as enums and indexes only, for a failure's text.
func results(outs []store.Outcome) []string {
	var s []string
	for _, o := range outs {
		s = append(s, o.Result+":"+o.Reason)
	}
	return s
}

// CheckExactlyOnce: Plan gives Apply's outcomes and writes nothing; a pass
// run again, before or after a reopen, returns the first outcomes, ids and
// commit and changes nothing; a pass is in history once if it applied or
// covered anything, and never twice; and one seed gives one tree.
func CheckExactlyOnce(newSubject func() Subject, seed int64) error {
	return checkExactlyOnce(newSubject, seed, newSchedule(seed, options{edits: true}), crashed{})
}

// covered is the lines of a session's coverage, none when the pass names
// no session.
func (r *runner) covered(session string) (map[int]bool, error) {
	if session == "" {
		return map[int]bool{}, nil
	}
	c, err := r.st.Coverage(session)
	if err != nil {
		return nil, r.fail("Coverage: %v", err)
	}
	return lines(c), nil
}

func checkExactlyOnce(newSubject func() Subject, seed int64, sch *schedule, tally crashed) error {
	r := newRunner(PropExactlyOnce, newSubject(), seed)
	r.crashed = tally
	if err := r.open(); err != nil {
		return err
	}
	defer r.close()
	for i, ev := range sch.events {
		r.step = i
		var txn store.Txn
		switch ev.what {
		case evPass:
			txn = r.derive(sch.record[ev.chunk])
		case evAck:
			var ok bool
			if txn, _, ok = r.ackTxn(ev); !ok {
				continue
			}
		default:
			if err := r.run(sch, ev); err != nil {
				return err
			}
			continue
		}
		had, err := r.covered(txn.Session)
		if err != nil {
			return err
		}
		before := r.snap()
		plan, err := r.st.Plan(txn)
		if err != nil {
			return r.fail("Plan: %v", err)
		}
		if got := r.snap(); !got.equal(before) {
			return r.fail("Plan wrote: %s", got.diff(before))
		}
		res, err := r.apply(txn, ev.crash)
		if err != nil {
			return err
		}
		if !reflect.DeepEqual(plan, res.Outcomes) {
			return r.fail("Plan gave %s and Apply %s", results(plan), results(res.Outcomes))
		}
		after := r.snap()
		for _, reopen := range []bool{false, true} {
			if reopen {
				if err := r.reopen(); err != nil {
					return err
				}
			}
			again, err := r.apply(txn, nil)
			if err != nil {
				return err
			}
			if !reflect.DeepEqual(again, res) {
				return r.fail("the pass run again gave %s, commit %q; first %s, commit %q", results(again.Outcomes), again.Commit, results(res.Outcomes), res.Commit)
			}
			if got := r.snap(); !got.equal(after) {
				return r.fail("the pass run again changed the store: %s", got.diff(after))
			}
		}
		n := 0
		for _, c := range r.s.History() {
			if c.Pass == txn.Pass {
				n++
			}
		}
		// The pass did something if it applied an operation or covered a
		// line no pass covered before it, not merely if it carried ranges:
		// a range covered again, or one of a session file that is shut,
		// covers nothing.
		has, err := r.covered(txn.Session)
		if err != nil {
			return err
		}
		did := slices.ContainsFunc(res.Outcomes, func(o store.Outcome) bool { return o.Result == store.ResultApplied })
		for l := range has {
			did = did || !had[l]
		}
		if did && n != 1 || !did && n != 0 {
			return r.fail("the pass is in history %d times; it applied or newly covered something: %v", n, did)
		}
	}
	seen := map[string]bool{}
	for _, c := range r.s.History() {
		if c.Pass != "" && seen[c.Pass] {
			return r.fail("pass %s is in history twice", c.Pass)
		}
		seen[c.Pass] = true
	}
	// One seed, one tree: the same schedule, played plainly on a new
	// subject, ends in the same files and history.
	other := newRunner(PropExactlyOnce, newSubject(), seed)
	other.crashed = tally
	if err := other.open(); err != nil {
		return err
	}
	defer other.close()
	for i, ev := range sch.events {
		other.step = i
		if err := other.run(sch, ev); err != nil {
			return err
		}
	}
	if got, want := other.snap(), r.snap(); !got.equal(want) {
		return r.fail("the same seed gave another tree: %s", got.diff(want))
	}
	return nil
}

// mark is what the harness did to a file by hand, an item's or a
// session's.
type mark struct {
	session                            bool
	locked, verified, garbled, unacked bool
}

// expect is the reason an operation with that authority is refused for.
// A session file that holds a lock or a verified mark, or cannot be read,
// refuses whatever the authority, since no operation can clear the mark
// there; a hand edit of one refuses nothing, since none can acknowledge it.
func (k *mark) expect(authority string) string {
	switch {
	case k == nil:
		return ""
	case k.session && k.garbled:
		return store.ReasonUnreadable
	case k.session && k.locked:
		return store.ReasonLocked
	case k.session && k.verified:
		return store.ReasonVerified
	case k.session:
		return ""
	case k.garbled:
		return store.ReasonUnreadable
	case k.locked:
		return store.ReasonLocked
	case authority != store.AuthorityUser && k.verified:
		return store.ReasonVerified
	case authority != store.AuthorityUser && k.unacked:
		return store.ReasonHandEdit
	}
	return ""
}

var protectionReasons = []string{store.ReasonLocked, store.ReasonVerified, store.ReasonUnreadable, store.ReasonHandEdit}

// CheckProtection: an operation aimed at a locked, verified, unreadable or
// hand-edited file is refused with its reason and leaves the file's bytes
// as they were; every other operation of the pass gets the outcome it gets
// without that one, unless it requires it; no operation aimed at another
// file is refused for protection; and a locked item is byte-identical
// after LockedPasses passes aimed at it.
func CheckProtection(newSubject func() Subject, seed int64) error {
	return checkProtection(newSubject, seed, newSchedule(seed, options{edits: true}))
}

func checkProtection(newSubject func() Subject, seed int64, sch *schedule) error {
	r := newRunner(PropProtection, newSubject(), seed)
	if err := r.open(); err != nil {
		return err
	}
	defer r.close()
	marks := map[string]*mark{}
	for i, ev := range sch.events {
		r.step = i
		var txn store.Txn
		var target Item
		switch ev.what {
		case evEdit:
			path, err := r.handEdit(ev)
			if err != nil {
				return err
			}
			if path == "" {
				continue
			}
			k := marks[path]
			if k == nil {
				k = &mark{session: ev.session}
				marks[path] = k
			}
			k.locked = k.locked || ev.edit.Kind == EditLock
			k.verified = k.verified || ev.edit.Kind == EditVerify
			k.garbled = k.garbled || ev.edit.Kind == EditGarble
			k.unacked = true
			continue
		case evAck:
			var ok bool
			if txn, target, ok = r.ackTxn(ev); !ok {
				continue
			}
		case evPass:
			txn = r.derive(sch.record[ev.chunk])
		default:
			continue
		}
		res, err := r.protected(txn, marks)
		if err != nil {
			return err
		}
		if ev.what == evAck && res.Outcomes[0].Result == store.ResultApplied && marks[target.Path] != nil {
			marks[target.Path].unacked = false
		}
	}
	r.step = len(sch.events)
	return r.lockedSurvives(marks)
}

// protected applies a pass and checks each operation aimed at a file the
// harness marked.
func (r *runner) protected(txn store.Txn, marks map[string]*mark) (store.Result, error) {
	paths := map[string]string{}
	for _, it := range r.s.Items() {
		paths[it.ID] = it.Path
	}
	// The session file, when the pass names a session whose file is there.
	var session string
	if txn.Session != "" {
		if p, ok := r.s.SessionFile(txn.Session); ok {
			session = p
		}
	}
	target := func(op store.Op) string {
		if op.Type == store.OpSessionAppend {
			return session
		}
		return paths[op.ID]
	}
	reasons := map[int]string{}
	without := map[int][]store.Outcome{}
	for k, op := range txn.Ops {
		path := target(op)
		if path == "" {
			continue
		}
		if reasons[k] = marks[path].expect(op.Authority); reasons[k] == "" {
			continue
		}
		plan, err := r.st.Plan(drop(txn, k))
		if err != nil {
			return store.Result{}, r.fail("Plan: %v", err)
		}
		without[k] = plan
	}
	had, err := r.covered(txn.Session)
	if err != nil {
		return store.Result{}, err
	}
	before := r.s.Files()
	res, err := r.apply(txn, nil)
	if err != nil {
		return res, err
	}
	files := r.s.Files()
	has, err := r.covered(txn.Session)
	if err != nil {
		return res, err
	}
	// A session file that is shut takes no coverage and keeps its bytes;
	// any other takes every range of the pass no pass covered before, as
	// the pass goes on beside a refusal.
	shut := ""
	if session != "" {
		shut = marks[session].expect(store.AuthorityUser)
	}
	for _, c := range txn.Covers {
		for l := c.From; l <= c.To; l++ {
			switch {
			case shut != "" && has[l] && !had[l]:
				return res, r.fail("the session file refuses as %s, yet the pass covered line %d of it", shut, l)
			case shut == "" && !has[l] && !had[l]:
				return res, r.fail("the pass did not cover line %d, of a session file nothing protects", l)
			}
		}
	}
	if shut != "" && !bytes.Equal(before[session], files[session]) {
		return res, r.fail("the session file refuses as %s, yet the pass changed it", shut)
	}
	for k, op := range txn.Ops {
		o, reason := res.Outcomes[k], reasons[k]
		if reason == "" {
			if o.Result == store.ResultRefused && slices.Contains(protectionReasons, o.Reason) && (op.ID != "" || op.Type == store.OpSessionAppend) {
				return res, r.fail("operation %d, aimed at a file nothing protects, was refused as %s", k, o.Reason)
			}
			continue
		}
		// An operation whose requirement was refused is refused as
		// requires, before any reason of its own file.
		if slices.ContainsFunc(op.Requires, func(q int) bool { return q >= 0 && q < k && res.Outcomes[q].Result == store.ResultRefused }) {
			reason = store.ReasonRequires
		}
		if o.Result != store.ResultRefused || o.Reason != reason {
			return res, r.fail("operation %d (%s, %s) aimed at a file that refuses it as %s gave %s %s", k, op.Type, op.Authority, reason, o.Result, o.Reason)
		}
		if path := target(op); !bytes.Equal(before[path], files[path]) {
			return res, r.fail("operation %d was refused as %s but its file changed", k, reason)
		}
		deps := dependents(txn, k)
		for j := range txn.Ops {
			if deps[j] {
				if got := res.Outcomes[j]; got.Result != store.ResultRefused || got.Reason != store.ReasonRequires {
					return res, r.fail("operation %d requires operation %d, which was refused, yet gave %s %s", j, k, got.Result, got.Reason)
				}
			}
			if j == k || deps[j] {
				continue
			}
			w := without[k][j-btoi(j > k)]
			got := res.Outcomes[j]
			if got.Result != w.Result || got.Reason != w.Reason || txn.Ops[j].Type != store.OpItemCreate && got.ID != w.ID {
				return res, r.fail("operation %d gave %s %s beside a refused operation %d, and %s %s without it", j, got.Result, got.Reason, k, w.Result, w.Reason)
			}
		}
	}
	return res, nil
}

func btoi(b bool) int {
	if b {
		return 1
	}
	return 0
}

// drop is the pass without operation k, its requirements renumbered.
func drop(txn store.Txn, k int) store.Txn {
	out := txn
	out.Ops = nil
	for j, op := range txn.Ops {
		if j == k {
			continue
		}
		var req []int
		for _, q := range op.Requires {
			if q != k {
				req = append(req, q-btoi(q > k))
			}
		}
		op.Requires = req
		out.Ops = append(out.Ops, op)
	}
	return out
}

// dependents is every operation that requires k, directly or not.
func dependents(txn store.Txn, k int) map[int]bool {
	deps := map[int]bool{k: true}
	for j, op := range txn.Ops {
		for _, q := range op.Requires {
			if deps[q] {
				deps[j] = true
			}
		}
	}
	delete(deps, k)
	return deps
}

// lockedSurvives locks an item by hand and aims LockedPasses passes at it,
// with each operation that can target it and either authority.
func (r *runner) lockedSurvives(marks map[string]*mark) error {
	it, ok := r.pick(int(r.seed))
	if !ok {
		return r.fail("the schedule made no item")
	}
	if _, err := r.handEdit(event{pick: int(r.seed), edit: Edit{Kind: EditLock}}); err != nil {
		return err
	}
	k := marks[it.Path]
	if k == nil {
		k = &mark{}
	}
	k.locked, k.unacked = true, true
	reason := k.expect(store.AuthorityUser)
	before := r.s.Files()[it.Path]
	for _, cur := range r.s.Items() {
		if cur.Path == it.Path {
			it = cur
		}
	}
	ops := []store.Op{
		{Type: store.OpItemRevise, Fields: map[string]any{"statement": "s0 the lock was ignored"}},
		{Type: store.OpItemRevise, Fields: map[string]any{"title": "t-unlocked"}},
		{Type: store.OpItemState, Fields: map[string]any{"state": "retired"}},
		{Type: store.OpItemState, Fields: map[string]any{"state": "retracted"}},
		{Type: store.OpDocAcknowledge},
	}
	for i := range LockedPasses {
		r.step++
		op := ops[i%len(ops)]
		op.ID, op.Base, op.Authority = it.ID, it.Base, []string{store.AuthorityModel, store.AuthorityUser}[i/len(ops)%2]
		res, err := r.st.Apply(store.Txn{Pass: r.passID(), Kind: store.KindCorrect, Actor: "human:person", Ops: []store.Op{op}})
		if err != nil {
			return r.fail("Apply: %v", err)
		}
		if o := res.Outcomes; len(o) != 1 || o[0].Result != store.ResultRefused || o[0].Reason != reason {
			return r.fail("pass %d at a locked item gave %s, want refused %s", i+1, results(o), reason)
		}
		if i%100 == 99 || i == LockedPasses-1 {
			if !bytes.Equal(r.s.Files()[it.Path], before) {
				return r.fail("a locked item changed after %d passes aimed at it", i+1)
			}
		}
	}
	return nil
}

// kept is a text a session file must hold: a note a pass wrote, or a text
// a person wrote by hand, and the step at which it was put on its list.
type kept struct {
	text   string
	step   int
	byHand bool
}

// keptCounts is what SessionKept put on its lists and what it passed
// over, so a test can see that a run plays each case.
type keptCounts struct {
	notes, refused, byHand int
}

// CheckSessionKept: a pass keeps what a session file holds. Each note a
// pass applied, and each text a person wrote into the file by hand, is in
// the file byte for byte after every later event, until a person's own
// edit takes it out.
func CheckSessionKept(newSubject func() Subject, seed int64) error {
	return checkSessionKept(newSubject, seed, newSchedule(seed, options{edits: true}), &keptCounts{})
}

func checkSessionKept(newSubject func() Subject, seed int64, sch *schedule, counts *keptCounts) error {
	r := newRunner(PropSessionKept, newSubject(), seed)
	if err := r.open(); err != nil {
		return err
	}
	defer r.close()
	want := map[string][]kept{}
	for i, ev := range sch.events {
		r.step = i
		switch ev.what {
		case evPass:
			txn := r.derive(sch.record[ev.chunk])
			res, err := r.apply(txn, nil)
			if err != nil {
				return err
			}
			path, ok := r.s.SessionFile(txn.Session)
			for k, op := range txn.Ops {
				if op.Type != store.OpSessionAppend {
					continue
				}
				if res.Outcomes[k].Result != store.ResultApplied {
					counts.refused++
					continue
				}
				if !ok {
					return r.fail("operation %d, a note, applied, yet the session has no file", k)
				}
				counts.notes++
				text, _ := op.Fields["text"].(string)
				want[path] = append(want[path], kept{text: text, step: i})
			}
		case evEdit:
			path, err := r.editFile(ev)
			if err != nil {
				return err
			}
			if path == "" {
				continue
			}
			// A person may rewrite what a pass wrote: what the edit left
			// stays on the list, beside the person's own text.
			if ev.session && ev.edit.Kind == EditChange {
				b := r.s.Files()[path]
				list := []kept{{text: ev.edit.Text, step: i, byHand: true}}
				for _, k := range want[path] {
					if bytes.Contains(b, []byte(k.text)) {
						list = append(list, k)
					}
				}
				want[path] = list
				counts.byHand++
			}
			if err := r.open(); err != nil {
				return err
			}
		case evAck:
			if txn, _, ok := r.ackTxn(ev); ok {
				if _, err := r.apply(txn, nil); err != nil {
					return err
				}
			}
		}
		files := r.s.Files()
		for _, path := range slices.Sorted(maps.Keys(want)) {
			for _, k := range want[path] {
				if bytes.Contains(files[path], []byte(k.text)) {
					continue
				}
				if k.byHand {
					return r.fail("a text written by hand into %s at step %d is no longer there", path, k.step)
				}
				return r.fail("a note a pass wrote into %s at step %d is no longer there", path, k.step)
			}
		}
	}
	return nil
}

// coverage is each session's coverage, nil where it is empty.
func (r *runner) coverage() (map[string][]store.Range, error) {
	out := map[string][]store.Range{}
	for _, s := range sessions {
		c, err := r.st.Coverage(s)
		if err != nil {
			return nil, r.fail("Coverage: %v", err)
		}
		if len(c) > 0 {
			out[s] = c
		}
	}
	return out, nil
}

// CheckRestore: after the store folder is lost and restored, the store
// opens with its store id, and its files, history and coverage are those
// of the last push, or those of a new store when there was none.
func CheckRestore(newSubject func() Restorable, seed int64) error {
	return checkRestore(newSubject, seed, crashed{})
}

func checkRestore(newSubject func() Restorable, seed int64, tally crashed) error {
	sch := newSchedule(seed, options{edits: true, pushes: true, loses: true})
	r := newRunner(PropRestore, newSubject(), seed)
	r.crashed = tally
	if err := r.open(); err != nil {
		return err
	}
	defer r.close()
	type state struct {
		snap snapshot
		cov  map[string][]store.Range
	}
	cov, err := r.coverage()
	if err != nil {
		return err
	}
	last := state{r.snap(), cov}
	manifest := last.snap.files[manifestPath]
	if len(manifest) == 0 {
		return r.fail("a new store has no manifest at %s", manifestPath)
	}
	for i, ev := range append(slices.Clip(sch.events), event{what: evLose}) {
		r.step = i
		if err := r.run(sch, ev); err != nil {
			return err
		}
		switch ev.what {
		case evPush:
			cov, err := r.coverage()
			if err != nil {
				return err
			}
			last = state{r.snap(), cov}
		case evLose:
			cov, err := r.coverage()
			if err != nil {
				return err
			}
			got := state{r.snap(), cov}
			switch {
			case !bytes.Equal(got.snap.files[manifestPath], manifest):
				return r.fail("the restored store opened with another store id")
			case !got.snap.equal(last.snap):
				return r.fail("the restored store is not the last push: %s", got.snap.diff(last.snap))
			case !reflect.DeepEqual(got.cov, last.cov):
				return r.fail("the restored store's coverage is not the last push's")
			}
		}
	}
	return nil
}

// lines is the set of record lines a list of ranges covers.
func lines(rs []store.Range) map[int]bool {
	out := map[int]bool{}
	for _, r := range rs {
		for l := r.From; l <= r.To; l++ {
			out[l] = true
		}
	}
	return out
}

// itemKeys is the items by kind, state, title and statement, sorted.
func itemKeys(items []Item) []string {
	var out []string
	for _, it := range items {
		out = append(out, it.Kind+"\x00"+it.State+"\x00"+it.Title+"\x00"+it.Statement)
	}
	slices.Sort(out)
	return out
}

// CheckReplay: coverage never shrinks but by a loss. After a restore, the
// queue, the record less Coverage, is exactly what the pushed head leaves
// uncovered; once it is replayed, coverage is at least what it was before
// the loss and the items match those before it by kind, state, title and
// statement.
func CheckReplay(newSubject func() Restorable, seed int64) error {
	return checkReplay(newSubject, seed, crashed{})
}

func checkReplay(newSubject func() Restorable, seed int64, tally crashed) error {
	sch := newSchedule(seed, options{pushes: true})
	r := newRunner(PropReplay, newSubject(), seed)
	r.crashed = tally
	if err := r.open(); err != nil {
		return err
	}
	defer r.close()
	seen := map[string]map[int]bool{}
	grows := func() error {
		now, err := r.coverage()
		if err != nil {
			return err
		}
		for s, old := range seen {
			have := lines(now[s])
			for l := range old {
				if !have[l] {
					return r.fail("coverage of %s shrank: line %d is no longer covered", s, l)
				}
			}
		}
		for s, c := range now {
			seen[s] = lines(c)
		}
		return nil
	}
	done, pushed := map[int]bool{}, map[int]bool{}
	for i, ev := range sch.events {
		r.step = i
		if err := r.run(sch, ev); err != nil {
			return err
		}
		switch ev.what {
		case evPass:
			done[ev.chunk] = true
		case evPush:
			pushed = maps.Clone(done)
		}
		if err := grows(); err != nil {
			return err
		}
	}
	r.step = len(sch.events)
	items := itemKeys(r.s.Items())
	if err := r.restore(); err != nil {
		return err
	}
	// The queue is the record less Coverage; the pushed head leaves
	// uncovered exactly the chunks run after the last push.
	cov, err := r.coverage()
	if err != nil {
		return err
	}
	queue, want := map[string]map[int]bool{}, map[string]map[int]bool{}
	for ci, c := range sch.record {
		if queue[c.session] == nil {
			queue[c.session], want[c.session] = map[int]bool{}, map[int]bool{}
		}
		have := lines(cov[c.session])
		for l := c.from; l <= c.to; l++ {
			if !have[l] {
				queue[c.session][l] = true
			}
			if !pushed[ci] {
				want[c.session][l] = true
			}
		}
	}
	if !reflect.DeepEqual(queue, want) {
		return r.fail("after a restore the queue is not what the pushed head leaves uncovered")
	}
	for _, c := range sch.record {
		if queue[c.session][c.from] {
			r.step++
			if _, err := r.apply(r.derive(c), nil); err != nil {
				return err
			}
		}
	}
	if err := grows(); err != nil {
		return err
	}
	if got := itemKeys(r.s.Items()); !slices.Equal(got, items) {
		return r.fail("after the replay the store holds %d items; before the loss it held %d, and they differ", len(got), len(items))
	}
	return nil
}
