package work

import (
	"cmp"
	"context"
	"slices"
	"time"

	"github.com/heyNag/eva-md/internal/paths"
)

// Bar is how long work may wait before Status reports it: the plan's bar
// for nine chats in ten to be on file after they end.
const Bar = 5 * time.Minute

// A WaitReason is why work waits: one of a short closed list.
type WaitReason string

const (
	// WaitRule: waiting for the size or quiet rule.
	WaitRule WaitReason = "rule"
	// WaitBehind: behind a running job.
	WaitBehind WaitReason = "behind"
	// WaitBudget: deferred by the day's budget.
	WaitBudget WaitReason = "budget"
	// WaitHeld: held after a failure; Class and NextTry say which and
	// until when.
	WaitHeld WaitReason = "held"
	// WaitSignIn: waiting for sign-in.
	WaitSignIn WaitReason = "sign_in"
	// WaitOff: switched off, paused by the owner or excluded.
	WaitOff WaitReason = "off"
)

// Reasons are the six reasons, in the plan's order.
var Reasons = []WaitReason{WaitRule, WaitBehind, WaitBudget, WaitHeld, WaitSignIn, WaitOff}

// Words are the reason's words for printing, the plan's.
func (r WaitReason) Words() string {
	switch r {
	case WaitRule:
		return "waiting for the size or quiet rule"
	case WaitBehind:
		return "behind a running job"
	case WaitBudget:
		return "deferred by the day's budget"
	case WaitHeld:
		return "held after a failure"
	case WaitSignIn:
		return "waiting for sign-in"
	case WaitOff:
		return "switched off, paused by the owner or excluded"
	}
	return ""
}

// A Waiting is one piece of work that waits, and why. It names no session
// and holds no captured text.
type Waiting struct {
	Kind   string
	Host   string
	Since  time.Time
	Reason WaitReason
	// Class is the failure's class, for WaitHeld.
	Class string
	// NextTry is when the hold ends, for WaitHeld.
	NextTry time.Time
}

// A Report is what Status found: the work that waited longer than Bar, or
// is held or waiting for sign-in, oldest first.
type Report struct {
	Items []Waiting
}

// Status says why work waits: what each job's Waiting returns and, for each
// mark not yet run, WaitBehind since the mark was written. It keeps an item
// only when it has waited longer than Bar, or is held or waiting for
// sign-in. It reads the marks' names and words and no captured text; a
// wake folder it can't read adds nothing.
func Status(ctx context.Context, dirs paths.Dirs, now time.Time) Report {
	var all []Waiting
	for _, j := range registeredJobs() {
		if j.Waiting != nil {
			all = append(all, j.Waiting(ctx, now)...)
		}
	}
	if root, err := openWork(dirs); err == nil {
		marks, err := listMarks(root, wakeDir)
		root.Close()
		if err == nil {
			for _, m := range marks {
				all = append(all, Waiting{Kind: m.Kind, Host: m.Host, Since: m.written, Reason: WaitBehind})
			}
		}
	}
	var r Report
	for _, w := range all {
		if w.Reason == WaitHeld || w.Reason == WaitSignIn || now.Sub(w.Since) > Bar {
			r.Items = append(r.Items, w)
		}
	}
	slices.SortStableFunc(r.Items, func(a, b Waiting) int {
		return cmp.Or(a.Since.Compare(b.Since), cmp.Compare(a.Kind, b.Kind), cmp.Compare(a.Reason, b.Reason))
	})
	return r
}
