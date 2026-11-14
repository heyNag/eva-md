package storetest_test

import (
	"sync/atomic"
	"testing"
	"time"

	"github.com/heyNag/eva-md/internal/store"
	"github.com/heyNag/eva-md/internal/store/conformance"
	"github.com/heyNag/eva-md/internal/store/storetest"
)

// subject is the conformance harness's view of a fake. The harness opens a
// subject many times; its first open makes the fake, and every other is a
// Reopen, so each store it is handed is the same fake. It counts the
// faults that stopped a pass, as a check of its own beside the harness's,
// which fails a run in which no armed crash fired.
type subject struct {
	fake   *storetest.Fake
	clock  conformance.Clock
	faults *atomic.Int64
}

var (
	_ conformance.Restorable = (*subject)(nil)
	_ store.Store            = (*storetest.Fake)(nil)
)

func (s *subject) Open(clock conformance.Clock, fault store.Fault) (store.Store, error) {
	s.clock = clock
	if fault != nil {
		given := fault
		fault = func(step store.Step, i int) error {
			err := given(step, i)
			if err != nil {
				s.faults.Add(1)
			}
			return err
		}
	}
	if s.fake == nil {
		f, err := storetest.New(func() time.Time { return s.clock() })
		if err != nil {
			return nil, err
		}
		s.fake = f
		if fault == nil {
			return f, nil
		}
	}
	if err := s.fake.Reopen(fault); err != nil {
		return nil, err
	}
	return s.fake, nil
}

func (s *subject) Files() map[string][]byte {
	if s.fake == nil {
		return map[string][]byte{}
	}
	return s.fake.Files()
}

func (s *subject) History() []conformance.Commit {
	if s.fake == nil {
		return nil
	}
	return s.fake.History()
}

func (s *subject) Items() []conformance.Item {
	if s.fake == nil {
		return nil
	}
	return s.fake.Items()
}

func (s *subject) SessionFile(session string) (string, bool) {
	if s.fake == nil {
		return "", false
	}
	return s.fake.SessionFile(session)
}

func (s *subject) HandEdit(path string, e conformance.Edit) error {
	if s.fake == nil {
		return storetest.ErrLost
	}
	return s.fake.HandEdit(path, e)
}

func (s *subject) Push() error {
	if s.fake == nil {
		return storetest.ErrLost
	}
	return s.fake.Push()
}

func (s *subject) Lose() error {
	if s.fake == nil {
		return storetest.ErrLost
	}
	return s.fake.Lose()
}

func (s *subject) Restore() error {
	if s.fake == nil {
		return storetest.ErrRestore
	}
	return s.fake.Restore()
}

// seeds is how many schedules each property runs on the fake: 200, or
// fewer under the race detector, which slows the model many times over.
func seeds() int {
	if raceOn {
		return 40
	}
	return 200
}

// The fake keeps the store's six properties, as the model it is built on
// does. Each runs beside the others: they share no state. Every property
// but Protection and SessionKept crashes passes, so for each of those a
// fault must have stopped a pass of the fake.
func TestTheFakeKeepsEveryProperty(t *testing.T) {
	type property struct {
		run     func(*testing.T, func() conformance.Subject, func() conformance.Restorable)
		crashes bool
	}
	for name, p := range map[string]property{
		conformance.PropCrashSafety: {func(t *testing.T, s func() conformance.Subject, _ func() conformance.Restorable) {
			conformance.RunCrashSafety(t, s, seeds())
		}, true},
		conformance.PropExactlyOnce: {func(t *testing.T, s func() conformance.Subject, _ func() conformance.Restorable) {
			conformance.RunExactlyOnce(t, s, seeds())
		}, true},
		conformance.PropProtection: {func(t *testing.T, s func() conformance.Subject, _ func() conformance.Restorable) {
			conformance.RunProtection(t, s, seeds())
		}, false},
		conformance.PropRestore: {func(t *testing.T, _ func() conformance.Subject, r func() conformance.Restorable) {
			conformance.RunRestore(t, r, seeds())
		}, true},
		conformance.PropReplay: {func(t *testing.T, _ func() conformance.Subject, r func() conformance.Restorable) {
			conformance.RunReplay(t, r, seeds())
		}, true},
		conformance.PropSessionKept: {func(t *testing.T, s func() conformance.Subject, _ func() conformance.Restorable) {
			conformance.RunSessionKept(t, s, seeds())
		}, false},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			faults := &atomic.Int64{}
			p.run(t,
				func() conformance.Subject { return &subject{faults: faults} },
				func() conformance.Restorable { return &subject{faults: faults} })
			if p.crashes && faults.Load() == 0 {
				t.Errorf("no fault stopped a pass of the fake in %d seeds", seeds())
			}
		})
	}
}
