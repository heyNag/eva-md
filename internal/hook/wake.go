package hook

import (
	"slices"
	"strings"

	"github.com/heyNag/eva-md/internal/update"
	"github.com/heyNag/eva-md/internal/work"
)

// The wake handler: with the switch worker on, each job due that would
// start eva-md update or maintain starts eva-md work --wake instead, which
// leaves a mark for the worker, so a trigger that finds a job running is
// kept rather than dropped. Off, the default, it changes nothing: the
// hooks start v1's jobs.
func init() {
	registerHandler(400, "wake the worker", atLaunch, wake)
}

// wake rewrites the run's jobs into their wake forms (see wakeJob) while
// the switch is on. It recovers its own panic and leaves the jobs as they
// were judged, so a fault here still starts every job launch judged due:
// the day's maintain, whose attempt file launch took already, among them.
func wake(t *turn) {
	defer func() { _ = recover() }()
	if !work.Switch.On(t.env.Config) {
		return
	}
	jobs := make([]job, 0, len(t.jobs))
	for _, j := range t.jobs {
		jobs = append(jobs, wakeForm(j))
	}
	t.jobs = jobs
}

// wakeForm is wakeJob; a test replaces it to see a fault in it.
var wakeForm = wakeJob

// WakeReasons are the reasons a job may carry into its wake form, each a
// word eva-md work --wake takes as its --reason: a job's reasons, as this
// build's handlers ask for them, which a test holds equal to the hooks'
// list. A job with a reason not among them keeps v1's form, so a reason
// added to the hooks never reaches a work command that would refuse it
// and lose the trigger.
var WakeReasons = []string{reasonDaily, reasonPushRetry, update.TriggerGap, update.TriggerStop, update.TriggerEnd, update.TriggerCompact, update.TriggerStart, reasonHealth}

// wakeJob is j's wake form when j starts eva-md update or maintain:
//
//	update [--session host:id ...] [--trigger NAME] [--sweep=YYYY-MM-DD]
//	  -> work --wake --reason <reason> [--session host:id ...] [--sweep=YYYY-MM-DD]
//	maintain [--push-only]
//	  -> work --wake --reason <reason> --maintain [--push-only]
//
// with the job's reason, the same sessions, sweep day and push flag, and
// git set, since the worker may run maintain. The trigger an update names
// is its reason, which the wake carries. Any other job, one with a reason
// not among WakeReasons, or one with an argument this form doesn't know, is
// left as it is, so the hook starts v1's job rather than one the worker
// would refuse.
func wakeJob(j job) job {
	if len(j.args) == 0 || !slices.Contains(WakeReasons, j.reason) {
		return j
	}
	args := []string{"work", "--wake", "--reason", j.reason}
	rest := j.args[1:]
	switch j.args[0] {
	case "update":
		for i := 0; i < len(rest); i++ {
			switch a := rest[i]; {
			case (a == "--session" || a == update.TriggerFlag) && i+1 < len(rest):
				if a == "--session" {
					args = append(args, a, rest[i+1])
				}
				i++
			case strings.HasPrefix(a, sweepFlag+"="):
				args = append(args, a)
			default:
				return j
			}
		}
	case "maintain":
		args = append(args, "--maintain")
		switch {
		case slices.Equal(rest, []string{"--push-only"}):
			args = append(args, "--push-only")
		case len(rest) > 0:
			return j
		}
	default:
		return j
	}
	return job{args: args, reason: j.reason, git: true}
}
