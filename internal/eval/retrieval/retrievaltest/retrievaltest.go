// Package retrievaltest is the retrieval core's fake: a retrieval.System
// that answers from a table it is given, for the tests of packages that
// show what the core adds before the core lands. It reads no set file.
//
// Nothing in eva-md imports this package but tests.
package retrievaltest

import (
	"context"
	"slices"
	"sync"

	"github.com/heyNag/eva-md/internal/eval/retrieval"
)

// Fake adds, for each event, the item ids it was given for that event's
// id, and nothing for any other. It is safe for use by several goroutines.
type Fake struct {
	mu      sync.Mutex
	answers map[string][]string
	fail    map[string]error
	asked   []string
}

var _ retrieval.System = (*Fake)(nil)

// New makes a fake that answers each event id with its item ids, in the
// order given. It keeps copies, so changing the map or its slices after
// does not change the fake.
func New(answers map[string][]string) *Fake {
	f := &Fake{answers: make(map[string][]string, len(answers)), fail: map[string]error{}}
	for id, items := range answers {
		f.answers[id] = slices.Clone(items)
	}
	return f
}

// Add records the event's id as asked, then returns the error FailOn gave
// for it, or a copy of the item ids given for it, or nothing.
func (f *Fake) Add(_ context.Context, e retrieval.Event) ([]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.asked = append(f.asked, e.ID)
	if err, ok := f.fail[e.ID]; ok {
		return nil, err
	}
	return slices.Clone(f.answers[e.ID]), nil
}

// FailOn makes Add return err for the event of the id, and for no other.
// A nil err takes back an earlier FailOn for that event.
func (f *Fake) FailOn(eventID string, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err == nil {
		delete(f.fail, eventID)
		return
	}
	f.fail[eventID] = err
}

// Asked returns the event ids Add was asked for, in order, once for each
// time, as a copy.
func (f *Fake) Asked() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.asked)
}
