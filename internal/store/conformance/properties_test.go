package conformance

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/heyNag/eva-md/internal/store"
)

func newModel() Subject         { return NewModel() }
func newRestorable() Restorable { return NewModel() }

// seeds is how many schedules each property runs on the model: 1,000, or
// fewer under the race detector, which slows the model many times over.
func seeds() int {
	if raceOn {
		return 40
	}
	return 1000
}

// Each property runs beside the others: they share no state.
func TestTheModelKeepsEveryProperty(t *testing.T) {
	for name, run := range map[string]func(*testing.T){
		PropCrashSafety: func(t *testing.T) { RunCrashSafety(t, newModel, seeds()) },
		PropExactlyOnce: func(t *testing.T) { RunExactlyOnce(t, newModel, seeds()) },
		PropProtection:  func(t *testing.T) { RunProtection(t, newModel, seeds()) },
		PropRestore:     func(t *testing.T) { RunRestore(t, newRestorable, seeds()) },
		PropReplay:      func(t *testing.T) { RunReplay(t, newRestorable, seeds()) },
		PropSessionKept: func(t *testing.T) { RunSessionKept(t, newModel, seeds()) },
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			run(t)
		})
	}
}

// variants are the broken models, each with a property it fails, the seed
// that shows it and words of the failure that say why. PROPERTIES.md's
// table lists the same pairs.
var variants = []struct {
	name, property string
	seed           int64
	why            string
	set            func(*Model)
}{
	{"filesBeforeJournal", PropCrashSafety, 1, "neither as before the pass nor as after it", func(m *Model) { m.filesBeforeJournal = true }},
	{"filesBeforeJournal", PropExactlyOnce, 1, "Plan gave", func(m *Model) { m.filesBeforeJournal = true }},
	{"skipPassLookup", PropExactlyOnce, 1, "the pass run again gave", func(m *Model) { m.skipPassLookup = true }},
	{"skipPassLookup", PropCrashSafety, 3, "the pass run again after a crash", func(m *Model) { m.skipPassLookup = true }},
	{"bareLock", PropProtection, 1, "refuses it as locked", func(m *Model) { m.bareLock = true }},
	{"refusalDropsPass", PropProtection, 1, "the pass did not cover line", func(m *Model) { m.refusalDropsPass = true }},
	{"refusalDropsPass", PropReplay, 2, "the queue is not", func(m *Model) { m.refusalDropsPass = true }},
	{"sessionUnguarded", PropProtection, 5, "the session file refuses as locked, yet the pass covered", func(m *Model) { m.sessionUnguarded = true }},
	{"requiresIgnored", PropProtection, 14, "which was refused, yet gave applied", func(m *Model) { m.requiresIgnored = true }},
	{"coverageOutside", PropRestore, 1, "coverage is not the last push's", func(m *Model) { m.coverageOutside = true }},
	{"coverageOutside", PropReplay, 1, "the queue is not", func(m *Model) { m.coverageOutside = true }},
	{"queueFromJobs", PropReplay, 1, "the queue is not", func(m *Model) { m.queueFromJobs = true }},
	{"queueFromJobs", PropRestore, 1, "coverage is not the last push's", func(m *Model) { m.queueFromJobs = true }},
	{"faultIgnored", PropCrashSafety, 1, "a crash armed at recover did not fire", func(m *Model) { m.faultIgnored = true }},
	{"faultIgnored", PropExactlyOnce, 2, "a crash armed at recover did not fire", func(m *Model) { m.faultIgnored = true }},
	{"faultIgnored", PropRestore, 1, "a crash armed at plan did not fire", func(m *Model) { m.faultIgnored = true }},
	{"faultIgnored", PropReplay, 1, "a crash armed at recover did not fire", func(m *Model) { m.faultIgnored = true }},
	{"sessionRewritten", PropSessionKept, 2, "a note a pass wrote into", func(m *Model) { m.sessionRewritten = true }},
	{"handTextDropped", PropSessionKept, 7, "a text written by hand into", func(m *Model) { m.handTextDropped = true }},
}

// check runs one property on one seed.
func check(property string, seed int64, set func(*Model)) error {
	subject := func() Subject { m := NewModel(); set(m); return m }
	restorable := func() Restorable { m := NewModel(); set(m); return m }
	switch property {
	case PropCrashSafety:
		return CheckCrashSafety(subject, seed)
	case PropExactlyOnce:
		return CheckExactlyOnce(subject, seed)
	case PropProtection:
		return CheckProtection(subject, seed)
	case PropRestore:
		return CheckRestore(restorable, seed)
	case PropReplay:
		return CheckReplay(restorable, seed)
	case PropSessionKept:
		return CheckSessionKept(subject, seed)
	}
	return errors.New("no such property: " + property)
}

func TestEachVariantFailsItsProperty(t *testing.T) {
	for _, v := range variants {
		if err := check(v.property, v.seed, func(*Model) {}); err != nil {
			t.Fatalf("the model itself fails %s on seed %d: %v", v.property, v.seed, err)
		}
		err := check(v.property, v.seed, v.set)
		var f *Failure
		if !errors.As(err, &f) {
			t.Errorf("%s passes %s on seed %d (%v)", v.name, v.property, v.seed, err)
			continue
		}
		if f.Property != v.property || f.Seed != v.seed || !strings.Contains(f.Detail, v.why) {
			t.Errorf("%s fails %s on seed %d, but as %v", v.name, v.property, v.seed, err)
		}
		if !strings.HasPrefix(err.Error(), v.property+" fails on seed ") {
			t.Errorf("the failure does not name its property and seed: %v", err)
		}
	}
}

// A model that reads a lock only in its bare form changes a locked item
// within the passes Protection aims at it, even with no schedule before.
func TestALockedItemOutlastsItsPasses(t *testing.T) {
	for _, bare := range []bool{false, true} {
		m := NewModel()
		m.bareLock = bare
		r := newRunner(PropProtection, m, 3)
		if err := r.open(); err != nil {
			t.Fatal(err)
		}
		txn := r.derive(chunk{session: sessions[0], from: 1, to: 1, facts: []fact{{do: doCreate, kind: "fact", title: "t1", text: "s1 the cache stays local"}}})
		if _, err := r.apply(txn, nil); err != nil {
			t.Fatal(err)
		}
		err := r.lockedSurvives(map[string]*mark{})
		if bare != (err != nil) {
			t.Fatalf("bare lock %v: %v", bare, err)
		}
		if bare && !strings.Contains(err.Error(), "pass 1 at a locked item gave") {
			t.Fatalf("the bare-lock model failed otherwise: %v", err)
		}
		r.close()
	}
}

func TestEverySeedRunsEachKindOfEvent(t *testing.T) {
	counts := map[string]int{}
	steps := map[store.Step]bool{}
	for seed := int64(1); seed <= 50; seed++ {
		sch := newSchedule(seed, options{edits: true, pushes: true, loses: true})
		if len(sch.record) < 6 || sch.record[0].facts[0].do != doCreate {
			t.Fatalf("seed %d: a record of %d chunks that does not start with a create", seed, len(sch.record))
		}
		for _, ev := range sch.events {
			counts[ev.what]++
			if ev.crash != nil {
				steps[ev.crash.step] = true
			}
			if ev.what == evEdit {
				counts[ev.edit.Kind]++
				if ev.session {
					counts["session "+ev.edit.Kind]++
				}
			}
		}
		for _, c := range sch.record {
			for _, f := range c.facts {
				counts[f.do]++
			}
		}
	}
	for _, k := range []string{evPass, evEdit, evAck, evPush, evLose, EditChange, EditLock, EditVerify, EditGarble, "session " + EditChange, "session " + EditLock, "session " + EditVerify, "session " + EditGarble, doCreate, doRevise, doState, doNote} {
		if counts[k] < 5 {
			t.Errorf("50 seeds hold %d of %s", counts[k], k)
		}
	}
	for _, s := range store.Steps {
		if !steps[s] {
			t.Errorf("no schedule crashes at %s", s)
		}
	}
	// One seed, one schedule; another seed, another.
	a, b, c := newSchedule(7, options{edits: true}), newSchedule(7, options{edits: true}), newSchedule(8, options{edits: true})
	if len(a.events) != len(b.events) || len(a.record) != len(b.record) || a.record[0].facts[0] != b.record[0].facts[0] {
		t.Fatal("one seed gave two schedules")
	}
	if len(a.events) == len(c.events) && a.record[0].facts[0] == c.record[0].facts[0] && a.record[0].to == c.record[0].to {
		t.Fatal("two seeds gave one schedule")
	}
}

// crashChecks is each property that crashes passes, run on a model that
// newModel makes, with the tally of a run.
func crashChecks(newModel func() *Model) map[string]func(int64, crashed) error {
	subject := func() Subject { return newModel() }
	restorable := func() Restorable { return newModel() }
	return map[string]func(int64, crashed) error{
		PropCrashSafety: func(seed int64, c crashed) error { return checkCrashSafety(subject, seed, c) },
		PropExactlyOnce: func(seed int64, c crashed) error {
			return checkExactlyOnce(subject, seed, newSchedule(seed, options{edits: true}), c)
		},
		PropRestore: func(seed int64, c crashed) error { return checkRestore(restorable, seed, c) },
		PropReplay:  func(seed int64, c crashed) error { return checkReplay(restorable, seed, c) },
	}
}

// wantFailure fails the test unless err is a failure of a whole run of the
// property, which names its last seed and no step, with exactly the detail.
func wantFailure(t *testing.T, err error, property string, seed int64, detail string) {
	t.Helper()
	var f *Failure
	if !errors.As(err, &f) || f.Property != property || f.Seed != seed || f.Step != runLevel || f.Detail != detail {
		t.Errorf("got %v; want a run's failure: %s fails on seed %d: %s", err, property, seed, detail)
		return
	}
	if want := fmt.Sprintf("%s fails on seed %d: %s", property, seed, detail); err.Error() != want {
		t.Errorf("the run's failure reads %q, want %q", err.Error(), want)
	}
}

// A failure at a step names it; one of a whole run names none.
func TestAFailureNamesItsStepWhenItHasOne(t *testing.T) {
	for _, c := range []struct {
		f    Failure
		want string
	}{
		{Failure{Property: PropReplay, Seed: 3, Step: 0, Detail: "made up"}, "Replay fails on seed 3 at step 0: made up"},
		{Failure{Property: PropReplay, Seed: 3, Step: 12, Detail: "made up"}, "Replay fails on seed 3 at step 12: made up"},
		{Failure{Property: PropReplay, Seed: 3, Step: runLevel, Detail: "made up"}, "Replay fails on seed 3: made up"},
	} {
		if got := c.f.Error(); got != c.want {
			t.Errorf("got %q, want %q", got, c.want)
		}
	}
}

// The count of a run, on checks that fire what they are told to.
func TestARunWantsArmedCrashesToFire(t *testing.T) {
	fires := func(steps ...store.Step) func(int64, crashed) error {
		return func(_ int64, c crashed) error {
			for _, s := range steps {
				c[s]++
			}
			return nil
		}
	}
	wantFailure(t, runCrashes(PropReplay, upTo(3), fires()), PropReplay, 3, "no armed crash fired in 3 seeds")
	wantFailure(t, runCrashes(PropRestore, []int64{9, 4}, fires()), PropRestore, 4, "no armed crash fired in 2 seeds")
	if err := runCrashes(PropReplay, []int64{9, 4}, fires(store.StepFinish)); err != nil {
		t.Errorf("a run of two seeds wants one crash only: %v", err)
	}
	if err := runCrashes(PropCrashSafety, upTo(everyStepFrom-1), fires(store.StepRecover)); err != nil {
		t.Errorf("a run of %d seeds wants a crash at no step in particular: %v", everyStepFrom-1, err)
	}
	wantFailure(t, runCrashes(PropCrashSafety, upTo(everyStepFrom), fires(store.StepRecover, store.StepPlan)), PropCrashSafety, everyStepFrom, "no armed crash fired at journal in 200 seeds")
	wantFailure(t, runCrashes(PropExactlyOnce, upTo(everyStepFrom), fires(store.Steps[:len(store.Steps)-1]...)), PropExactlyOnce, everyStepFrom, "no armed crash fired at finish in 200 seeds")
	if err := runCrashes(PropExactlyOnce, upTo(everyStepFrom), fires(store.Steps...)); err != nil {
		t.Errorf("a run that crashed at every step fails: %v", err)
	}
	if err := runCrashes(PropReplay, nil, fires(store.Steps...)); err == nil {
		t.Error("a run of no seed passes")
	}
	// A check that fails stops the run, and its failure is the run's.
	var checked []int64
	stop := &Failure{Property: PropReplay, Seed: 2, Detail: "made up"}
	err := runCrashes(PropReplay, upTo(5), func(seed int64, c crashed) error {
		checked = append(checked, seed)
		c[store.StepRecover]++
		if seed == 2 {
			return stop
		}
		return nil
	})
	if err != stop || len(checked) != 2 {
		t.Errorf("the run checked %v and gave %v", checked, err)
	}
}

// armedAt reports whether a schedule arms a crash at all, and whether one
// at recover or plan, where it must fire.
func armedAt(sch *schedule) (some, early bool) {
	for _, ev := range sch.events {
		if ev.crash != nil {
			some = true
			early = early || ev.crash.step == store.StepRecover || ev.crash.step == store.StepPlan
		}
	}
	return some, early
}

// The count catches what the rule of each Apply cannot: on seeds whose
// schedules arm no crash at recover or plan, a model that ignores its
// fault passes every check, and the run over them fails; the model passes
// the same run. CrashSafety arms a crash on every pass, at a step that
// moves with the pass, so it has no such seeds to speak of.
func TestARunCatchesAFaultNeverPassedOn(t *testing.T) {
	ignored := crashChecks(func() *Model { m := NewModel(); m.faultIgnored = true; return m })
	model := crashChecks(NewModel)
	for _, p := range []struct {
		name string
		o    options
	}{
		{PropExactlyOnce, options{edits: true}},
		{PropRestore, options{edits: true, pushes: true, loses: true}},
		{PropReplay, options{pushes: true}},
	} {
		var list []int64
		for seed := int64(1); seed <= 100 && len(list) < 5; seed++ {
			if some, early := armedAt(newSchedule(seed, p.o)); some && !early {
				list = append(list, seed)
			}
		}
		if len(list) < 2 {
			t.Fatalf("%s: 100 seeds give %d schedules with a crash and none at recover or plan", p.name, len(list))
		}
		for _, seed := range list {
			if err := ignored[p.name](seed, crashed{}); err != nil {
				t.Fatalf("%s: the rule of each Apply catches seed %d alone: %v", p.name, seed, err)
			}
		}
		last := list[len(list)-1]
		wantFailure(t, runCrashes(p.name, list, ignored[p.name]), p.name, last, fmt.Sprintf("no armed crash fired in %d seeds", len(list)))
		if err := runCrashes(p.name, list, model[p.name]); err != nil {
			t.Errorf("%s: the model fails the run of seeds %v: %v", p.name, list, err)
		}
	}
}

// A model that passes its fault on at recover and plan alone passes each
// check and the rule of at least one crash, and fails each crash property
// over 200 seeds, at the first step it never crashes.
func TestARunOf200SeedsWantsACrashAtEveryStep(t *testing.T) {
	early := crashChecks(func() *Model { m := NewModel(); m.faultEarlyOnly = true; return m })
	for name, check := range early {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			wantFailure(t, runCrashes(name, upTo(everyStepFrom), check), name, everyStepFrom, "no armed crash fired at journal in 200 seeds")
		})
	}
}

// SessionKept follows what a person wrote: a rewrite by hand takes the
// notes it removed off the list and puts the person's text on it, and a
// note a shut file refused never goes on it.
func TestSessionKeptFollowsWhatAPersonWrote(t *testing.T) {
	note := func(n int) chunk {
		return chunk{session: "codex:b2", from: n, to: n, facts: []fact{{do: doNote, text: fmt.Sprintf("s%d the hook stays local", n)}}}
	}
	sch := &schedule{
		record: []chunk{note(1), note(2), note(3), note(4)},
		events: []event{
			{what: evPass, chunk: 0},
			{what: evPass, chunk: 1},
			{what: evEdit, session: true, edit: Edit{Kind: EditChange, Text: "s1000 the cache is the person's"}},
			{what: evPass, chunk: 2},
			{what: evEdit, session: true, edit: Edit{Kind: EditLock}},
			{what: evPass, chunk: 3},
		},
	}
	m := NewModel()
	counts := &keptCounts{}
	if err := checkSessionKept(func() Subject { return m }, 1, sch, counts); err != nil {
		t.Fatal(err)
	}
	if want := (keptCounts{notes: 3, refused: 1, byHand: 1}); *counts != want {
		t.Errorf("the check counted %+v, want %+v", *counts, want)
	}
	got := string(m.Files()[sessionPath("codex:b2")])
	for text, there := range map[string]bool{"s1 ": false, "s2 ": false, "s1000 ": true, "s3 ": true, "s4 ": false} {
		if strings.Contains(got, text) != there {
			t.Errorf("the file holds %q: %v, want %v", text, !there, there)
		}
	}
	for _, v := range []struct {
		name   string
		set    func(*Model)
		detail string
	}{
		{"sessionRewritten", func(m *Model) { m.sessionRewritten = true }, "a note a pass wrote into sessions/codex.b2.md at step 0 is no longer there"},
		{"handTextDropped", func(m *Model) { m.handTextDropped = true }, "a text written by hand into sessions/codex.b2.md at step 2 is no longer there"},
	} {
		err := checkSessionKept(func() Subject { m := NewModel(); v.set(m); return m }, 1, sch, &keptCounts{})
		var f *Failure
		if !errors.As(err, &f) || f.Detail != v.detail {
			t.Errorf("%s gave %v; want %s", v.name, err, v.detail)
		}
	}
}

// recoverOnly is the model behind an adapter that passes its fault on at
// recover alone.
type recoverOnly struct{ *Model }

func (s recoverOnly) Open(clock Clock, fault store.Fault) (store.Store, error) {
	return s.Model.Open(clock, func(st store.Step, i int) error {
		if st != store.StepRecover || fault == nil {
			return nil
		}
		return fault(st, i)
	})
}

// A crash armed at plan on a pass not yet in history must fire, in
// CheckCrashSafety's own loop as in the other checks. Behind an adapter
// that passes its fault on at recover alone, the first crash that does not
// fire where it must is armed at plan, at the step each row names.
func TestACrashArmedAtPlanMustFire(t *testing.T) {
	subject := func() Subject { return recoverOnly{NewModel()} }
	restorable := func() Restorable { return recoverOnly{NewModel()} }
	for _, c := range []struct {
		property   string
		seed       int64
		step       int
		check      func(int64) error
		modelCheck func(int64) error
	}{
		{PropCrashSafety, 8, 0, func(s int64) error { return CheckCrashSafety(subject, s) }, func(s int64) error { return CheckCrashSafety(newModel, s) }},
		{PropCrashSafety, 3, 3, func(s int64) error { return CheckCrashSafety(subject, s) }, func(s int64) error { return CheckCrashSafety(newModel, s) }},
		{PropExactlyOnce, 8, 0, func(s int64) error { return CheckExactlyOnce(subject, s) }, func(s int64) error { return CheckExactlyOnce(newModel, s) }},
		{PropRestore, 6, 3, func(s int64) error { return CheckRestore(restorable, s) }, func(s int64) error { return CheckRestore(newRestorable, s) }},
		{PropReplay, 5, 1, func(s int64) error { return CheckReplay(restorable, s) }, func(s int64) error { return CheckReplay(newRestorable, s) }},
	} {
		if err := c.modelCheck(c.seed); err != nil {
			t.Fatalf("the model itself fails %s on seed %d: %v", c.property, c.seed, err)
		}
		err := c.check(c.seed)
		var f *Failure
		if !errors.As(err, &f) || f.Property != c.property || f.Seed != c.seed || f.Step != c.step || f.Detail != "a crash armed at plan did not fire" {
			t.Errorf("%s on seed %d gave %v; want a crash armed at plan that did not fire, at step %d", c.property, c.seed, err, c.step)
		}
	}
}
