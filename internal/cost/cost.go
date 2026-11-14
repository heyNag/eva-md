// Package cost is what organizing spends, as one number: the tokens a
// host's model calls read and wrote, with cached input apart, their calls
// and their time, and the weighted sum a daily token budget is held to.
// It holds counts only and reads nothing: internal/update fills it from
// the run log. It imports the standard library alone.
package cost

// Usage is what model calls spent. In is the uncached input, CachedIn the
// input read from the host's cache, Out the output, reasoning included;
// Calls the CLI runs, retries included; MS their time in milliseconds.
type Usage struct {
	In, CachedIn, Out, Calls, MS int64
}

// Add adds o to u.
func (u *Usage) Add(o Usage) {
	u.In += o.In
	u.CachedIn += o.CachedIn
	u.Out += o.Out
	u.Calls += o.Calls
	u.MS += o.MS
}

// Weighted is u in weighted tokens, the unit of the daily budget: uncached
// input, plus cached input at a tenth, plus output at five times, rounded
// up to a whole token.
func (u Usage) Weighted() int64 {
	return (10*u.In + u.CachedIn + 50*u.Out + 9) / 10
}

// Estimate is what a call on bytes of input is taken to read before it
// runs: about four bytes a token, all of it uncached, rounded up. Its
// weight is its In.
func Estimate(bytes int) Usage {
	return Usage{In: (int64(bytes) + 3) / 4}
}

// Week is a host's spending over a week of the run log.
type Week struct {
	Usage
	// Passes is how many passes made a model call.
	Passes int64
	// Organized is how many sessions had a pass whose answer was accepted:
	// one that saved a page, or found nothing to change.
	Organized int64
	// NoChange is how many passes found nothing to change.
	NoChange int64
	// BelowEffort is how many passes ran below the effort they asked for.
	BelowEffort int64
}

// PerChat is the weighted tokens per organized chat, rounded to the
// nearest, or 0 when no chat was organized.
func (w Week) PerChat() int64 {
	if w.Organized <= 0 {
		return 0
	}
	return (w.Weighted() + w.Organized/2) / w.Organized
}
