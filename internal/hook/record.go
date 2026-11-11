package hook

import (
	"context"
	"slices"

	"github.com/heyNag/eva-md/internal/health"
	"github.com/heyNag/eva-md/internal/session"
)

// runRecord is what a run's heartbeat says of it beside its time, with no
// text: what it held back and why, and, for a run that can add text to a
// chat, what it delivered (see health.Delivery). The heartbeat adds it to
// the event's week too (see health.AddRun).
type runRecord struct {
	held     []string // of health.HeldKinds
	heldBy   string   // health.HeldByRecovery or health.HeldByOffRecord
	delivery *health.Delivery
	// lines is the routes of the lines a prompt gets beside its brief,
	// which count only once the lines are known to go.
	lines []string
}

// hold records what p's event would have stored or given and keeps none of,
// held back by by: a person's prompt, with the chat's first brief when it
// is still owed; a reply; a tool line; or a chat start's brief. A prompt
// that would have been stored as a host prompt, or not at all, holds none,
// and nor does a later prompt, whose refresh may not have been due.
func (r *runRecord) hold(ctx context.Context, markers session.Markers, host string, p payload, by string) {
	if r == nil {
		return
	}
	var kinds []string
	switch p.Event {
	case "SessionStart":
		kinds = []string{"brief"}
	case "UserPromptSubmit":
		if _, ok := deliverable(p); ok {
			kinds = []string{"prompt"}
			if _, found, err := markers.GetContext(ctx, host, p.Session, session.Briefed); err == nil && !found {
				kinds = append(kinds, "brief")
			}
		}
	case "Stop":
		if p.Reply != nil && *p.Reply != "" {
			kinds = []string{"reply"}
		}
	case "PostToolUse", "PostToolUseFailure":
		kinds = []string{"tool"}
	}
	if len(kinds) > 0 {
		r.held, r.heldBy = kinds, by
	}
}

// deliver starts the record of what p's run delivers, for a run that can
// add text to a chat, and returns it; the caller fills it in. Without a
// record, as when a test calls resumeContext alone, it returns one that
// goes nowhere.
func (r *runRecord) deliver(p payload) *health.Delivery {
	kind, _ := deliverable(p)
	d := &health.Delivery{Kind: kind}
	if r != nil {
		r.delivery = d
	}
	return d
}

// deliverable is the kind of p's run as health.DeliveryKinds names it, and
// whether it can add text to a chat: a chat start, by the brief variant
// its source asks for, or a prompt that is no built-in command and that no
// program or agent wrote, in a chat that no program runs.
func deliverable(p payload) (string, bool) {
	switch p.Event {
	case "SessionStart":
		switch p.Source {
		case "startup", "clear", "resume", "fork", "compact":
			return p.Source, true
		}
		return "startup", true
	case "UserPromptSubmit":
		return "prompt", p.Prompt != "" && !builtinCommand(p.Prompt) && !synthetic(p) && !p.headless
	}
	return "", false
}

// ordered is routes each once, in the order of health.Routes.
func ordered(routes []string) []string {
	var out []string
	for _, route := range health.Routes {
		if slices.Contains(routes, route) {
			out = append(out, route)
		}
	}
	return out
}

// run is the run as its day in the event's week counts it.
func (r *runRecord) run(failed bool, ms float64) health.HookRun {
	run := health.HookRun{Failed: failed, DurationMS: ms}
	if r != nil {
		run.Held, run.HeldBy, run.Delivered = r.held, r.heldBy, r.delivery
	}
	return run
}
