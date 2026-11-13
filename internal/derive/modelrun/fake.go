package modelrun

import (
	"bytes"
	"context"
	"slices"
	"sync"
)

// Fake is a Runner for tests of the packages that call one. It never starts
// a process: Probe gives Caps, and Run records a copy of every call, then
// answers with Respond when that is set, and otherwise with Outcome and Err.
// Set the fields before the first call; Probe and Run may then be called
// from several goroutines. It lives outside the _test files, as v1's
// update.Fake does, so other packages' tests can use it.
type Fake struct {
	Caps    Caps
	Outcome Outcome
	Err     error
	Respond func(Call) (Outcome, error)

	mu    sync.Mutex
	calls []Call
}

var _ Runner = (*Fake)(nil)

// Probe gives a copy of Caps. A done ctx gets its error, as a real probe
// would.
func (f *Fake) Probe(ctx context.Context) (Caps, error) {
	if err := ctx.Err(); err != nil {
		return Caps{}, err
	}
	c := f.Caps
	c.Efforts = slices.Clone(c.Efforts)
	c.Features = slices.Clone(c.Features)
	return c, nil
}

// Run records a copy of c and answers. A done ctx gets its error, as a real
// run would.
func (f *Fake) Run(ctx context.Context, c Call) (Outcome, error) {
	c.Input = bytes.Clone(c.Input)
	c.Schema = bytes.Clone(c.Schema)
	f.mu.Lock()
	f.calls = append(f.calls, c)
	f.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return Outcome{}, err
	}
	if f.Respond != nil {
		c.Input, c.Schema = bytes.Clone(c.Input), bytes.Clone(c.Schema)
		return f.Respond(c)
	}
	return cloneOutcome(f.Outcome), f.Err
}

// Calls returns copies of the calls seen so far, oldest first, each Input
// and Schema its own.
func (f *Fake) Calls() []Call {
	f.mu.Lock()
	defer f.mu.Unlock()
	calls := slices.Clone(f.calls)
	for i := range calls {
		calls[i].Input = bytes.Clone(calls[i].Input)
		calls[i].Schema = bytes.Clone(calls[i].Schema)
	}
	return calls
}

func cloneOutcome(o Outcome) Outcome {
	o.Answer = bytes.Clone(o.Answer)
	o.ModelsSeen = slices.Clone(o.ModelsSeen)
	o.Signals = slices.Clone(o.Signals)
	return o
}
