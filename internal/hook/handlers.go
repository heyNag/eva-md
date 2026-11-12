package hook

import (
	"cmp"
	"context"
	"fmt"
	"os"
	"slices"
	"time"

	"github.com/heyNag/eva-md/internal/catchup"
	"github.com/heyNag/eva-md/internal/resume"
	"github.com/heyNag/eva-md/internal/update"
)

// The hook's handler list. Each step that decides what a hook asks for
// beyond storing its event and giving its output is a handler, registered
// from its own file's init with registerHandler, which runs at one point of
// the run. A new step is a new file that registers a handler; the per-event
// capture in capture, the brief, the pointers and recall stay where they are.

// point is where in a hook's run a handler runs.
type point int

const (
	// atGuarded is after the guards and before the session lock: the run
	// captures this chat, which no exclusion, temporary folder, wiki or
	// spare stops. The time and before are not known yet.
	atGuarded point = iota
	// atStored is once the event's line is stored, just before the brief or
	// refresh is built, in a run whose chat is on the record and caught up.
	atStored
	// atBriefed is in the brief of a chat start or prompt, once
	// resume.Build has returned in time, with the session files it read.
	atBriefed
	// atLaunch is once the output is written and the jobs asked for have
	// been judged due, in a run whose capture passed the guards and
	// returned in time. A handler here may change or remove jobs; each job
	// left is then started. The capture's context is done by then.
	atLaunch
)

// handler is one step of the list: its place, which orders the handlers
// and which no two share, its name, the point it runs at and what it does.
type handler struct {
	place int
	name  string
	at    point
	run   func(*turn)
}

// handlers are the registered handlers, in the order of their places.
var handlers []handler

// registerHandler adds a handler at place, from its own file's init. Two
// handlers at one place are a mistake the build must not hide: it panics,
// naming both.
func registerHandler(place int, name string, at point, run func(*turn)) {
	for _, h := range handlers {
		if h.place == place {
			panic(fmt.Sprintf("hook: handlers %q and %q both take place %d; give each its own", h.name, name, place))
		}
	}
	handlers = append(handlers, handler{place, name, at, run})
	slices.SortFunc(handlers, func(a, b handler) int { return cmp.Compare(a.place, b.place) })
}

// turn is what the handlers of one hook run read, and the jobs they ask
// for. It holds no prompt, reply or other captured text.
type turn struct {
	ctx      context.Context // the capture's; done by atLaunch
	env      Env
	host     string
	event    string
	session  string
	source   string // a chat start's source, as the payload gives it
	headless bool   // a chat a program runs (see entrypoint)
	project  string // the project's key
	// file is the session's inbox file, and before that file as one stat
	// found it before the hook appended anything, from atStored on: nil
	// when there was none, and taken only for a prompt or a chat start
	// while updates are on, in a chat no program runs, as the handlers of
	// this release read it.
	file   string
	before os.FileInfo
	now    time.Time // the run's time, from atStored on
	// At atBriefed, the session files the brief read, and the sessions
	// start-of-chat transcript recovery appended to.
	read      []resume.SessionFile
	recovered []catchup.Recovered
	jobs      []job
	// guarded is set once the run has passed the guards, at atGuarded.
	guarded bool
}

// job is a detached eva-md command a handler asks for: its command line
// after the binary, exactly as it is started; why it was asked, one of
// reasons; and whether it runs git, for which it keeps gitEnv (see jobEnv).
type job struct {
	args   []string
	reason string
	git    bool
}

// The reasons a job is asked for. An update's reason is the trigger it
// names with update.TriggerFlag, when it names one; nothing is started
// with its reason in its arguments that it would not have had without.
const (
	reasonDaily     = "daily"      // the day's maintain
	reasonPushRetry = "push_retry" // the retry of a pending push
)

// reasons is the closed list of a job's reasons.
var reasons = []string{reasonDaily, reasonPushRetry, update.TriggerGap, update.TriggerStop, update.TriggerEnd, update.TriggerCompact, update.TriggerStart}

// ask adds j to the jobs of the run, with arguments of its own, so no
// handler that changes them later changes another job's.
func (t *turn) ask(j job) {
	j.args = slices.Clone(j.args)
	t.jobs = append(t.jobs, j)
}

// run runs the handlers at point at, in the order of their places.
func (t *turn) run(at point) {
	if at == atGuarded {
		t.guarded = true
	}
	for _, h := range handlers {
		if h.at == at {
			h.run(t)
		}
	}
}
