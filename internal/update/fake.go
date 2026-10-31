package update

import (
	"context"
	"slices"
	"sync"
)

// Fake is a Runner for tests of the update job. It never starts a process: it
// records every request, then answers with Respond when that is set, and
// otherwise with Result and Err. Set the fields before the first Run; Run may
// then be called from several goroutines. It lives outside the _test files
// so cmd/eva-md's tests can use it too: a package of its own would import
// this one, which this package's own tests can't import back.
type Fake struct {
	Result  Result
	Err     error
	Respond func(Request) (Result, error)

	mu       sync.Mutex
	requests []Request
}

var _ Runner = (*Fake)(nil)

// Run records a copy of r and answers. A done ctx gets its error, as a real
// run would.
func (f *Fake) Run(ctx context.Context, r Request) (Result, error) {
	r.Input = slices.Clone(r.Input)
	r.Schema = slices.Clone(r.Schema)
	r.CodexDisable = slices.Clone(r.CodexDisable)
	f.mu.Lock()
	f.requests = append(f.requests, r)
	f.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	if f.Respond != nil {
		return f.Respond(r)
	}
	return f.Result, f.Err
}

// Requests returns the requests seen so far, oldest first.
func (f *Fake) Requests() []Request {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.requests)
}
