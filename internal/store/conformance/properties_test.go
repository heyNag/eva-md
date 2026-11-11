package conformance

import (
	"errors"
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
	{"skipPassLookup", PropCrashSafety, 1, "the pass run again after a crash", func(m *Model) { m.skipPassLookup = true }},
	{"bareLock", PropProtection, 1, "refuses it as locked", func(m *Model) { m.bareLock = true }},
	{"refusalDropsPass", PropProtection, 1, "without it", func(m *Model) { m.refusalDropsPass = true }},
	{"refusalDropsPass", PropExactlyOnce, 1, "in history 0 times", func(m *Model) { m.refusalDropsPass = true }},
	{"coverageOutside", PropRestore, 1, "coverage is not the last push's", func(m *Model) { m.coverageOutside = true }},
	{"coverageOutside", PropReplay, 1, "the queue is not", func(m *Model) { m.coverageOutside = true }},
	{"queueFromJobs", PropReplay, 1, "the queue is not", func(m *Model) { m.queueFromJobs = true }},
	{"queueFromJobs", PropRestore, 1, "coverage is not the last push's", func(m *Model) { m.queueFromJobs = true }},
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
			}
		}
		for _, c := range sch.record {
			for _, f := range c.facts {
				counts[f.do]++
			}
		}
	}
	for _, k := range []string{evPass, evEdit, evAck, evPush, evLose, EditChange, EditLock, EditVerify, EditGarble, doCreate, doRevise, doState, doNote} {
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
