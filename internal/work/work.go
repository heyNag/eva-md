// Package work is the worker: one process at a time runs eva-md's
// background jobs, woken by marks on disk that outlive the process that
// left them, so a wake that finds the worker busy loses nothing. Its
// contract is SPEC.md, beside this file.
//
// Nothing here runs unless a caller calls it, and the callers check Switch.
// The package imports no hook, update, health or command code, so the hook
// can import it at no cost.
package work

import (
	"context"
	"errors"
	"fmt"
	"io"
	"regexp"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/heyNag/eva-md/internal/config"
	"github.com/heyNag/eva-md/internal/paths"
)

// Switch turns the worker on. Off, the default, is v1: the hooks start v1's
// jobs as they always have, and nothing calls this package.
var Switch = config.NewBoolSwitch("worker", false)

// The lock files, in the locks folder.
const (
	workerLock = "worker.lock"
	waiterLock = "waiter.lock"
)

// An Outcome is how a job's run ended.
type Outcome string

const (
	// Done: the job did what its marks asked. Its marks are removed.
	Done Outcome = "done"
	// Busy: the job's own lock was held, as v1's update and maintain locks
	// can be. Its marks go back, and its kind is not run again in this
	// worker's run.
	Busy Outcome = "busy"
	// Failed: the job tried and failed. Its marks are removed; the job
	// keeps whatever it needs to try again.
	Failed Outcome = "failed"
	// Cut: the job's context ended. Its marks go back.
	Cut Outcome = "cut"
)

// consumes reports whether a run that ended with o removes its marks.
func (o Outcome) consumes() bool { return o == Done || o == Failed }

// A Job is a kind of background work the worker runs.
type Job struct {
	// Kind names the job: lower-case letters, digits and _, starting with
	// a letter, at most 32 bytes. A mark names it.
	Kind string
	// Priority orders kinds: lower runs first.
	Priority int
	// Run does the work its marks ask for, writing what a person may read
	// to log, and says how it ended. It must return when ctx ends.
	Run func(ctx context.Context, marks []Mark, log io.Writer) Outcome
	// Waiting says, for Status, what of this job's work waits and why. It
	// may be nil.
	Waiting func(ctx context.Context, now time.Time) []Waiting
}

var (
	jobsMu sync.Mutex
	jobs   = map[string]Job{}
)

var (
	kindPattern = regexp.MustCompile(`^[a-z][a-z0-9_]{0,31}$`)
	wordPattern = regexp.MustCompile(`^[a-z][a-z0-9_]{0,31}$`)
	hostPattern = regexp.MustCompile(`^[a-z][a-z0-9-]{0,31}$`)
)

// Register adds a job. Call it from a package's init or a package-level
// variable. A kind registered twice, a kind that is not a valid name, or a
// job with no Run panics.
func Register(j Job) {
	if !kindPattern.MatchString(j.Kind) {
		panic(fmt.Sprintf("work: job kind %q must be lower-case letters, digits and _, starting with a letter", j.Kind))
	}
	if j.Run == nil {
		panic(fmt.Sprintf("work: job %q has no Run", j.Kind))
	}
	jobsMu.Lock()
	defer jobsMu.Unlock()
	if _, taken := jobs[j.Kind]; taken {
		panic(fmt.Sprintf("work: job %q is registered twice", j.Kind))
	}
	jobs[j.Kind] = j
}

// registered returns the job of kind, if any.
func registered(kind string) (Job, bool) {
	jobsMu.Lock()
	defer jobsMu.Unlock()
	j, ok := jobs[kind]
	return j, ok
}

// registeredJobs returns every job, by kind.
func registeredJobs() []Job {
	jobsMu.Lock()
	defer jobsMu.Unlock()
	out := make([]Job, 0, len(jobs))
	for _, j := range jobs {
		out = append(out, j)
	}
	slices.SortFunc(out, func(a, b Job) int { return strings.Compare(a.Kind, b.Kind) })
	return out
}

// A Mark asks a job to run. It is a file, work/wake/<kind>-<key>, that
// holds its reason and host, so it outlives the process that wrote it.
type Mark struct {
	// Kind is the registered job the mark is for.
	Kind string
	// Key names what the job is asked to do, chosen by the job's caller:
	// a name paths.SafeName leaves as it is. One mark is kept per kind
	// and key.
	Key string
	// Reason is one word: lower-case letters, digits and _, starting with
	// a letter, at most 32 bytes.
	Reason string
	// Host is the host the mark is for, as claude-code or codex, or empty.
	Host string
}

// name is the mark's file name.
func (m Mark) name() string { return m.Kind + "-" + m.Key }

func (m Mark) check() error {
	if _, ok := registered(m.Kind); !ok {
		return fmt.Errorf("work: no job of kind %q is registered", m.Kind)
	}
	if safe, err := paths.SafeName(m.Key); err != nil || safe != m.Key {
		return errors.New("work: a mark's key must be a plain file name")
	}
	if !wordPattern.MatchString(m.Reason) {
		return errors.New("work: a mark's reason must be one lower-case word")
	}
	if m.Host != "" && !hostPattern.MatchString(m.Host) {
		return errors.New("work: a mark's host must be one lower-case word")
	}
	return nil
}

// Options are what a caller may set for one wake.
type Options struct {
	// Now is the journal's clock; time.Now when nil.
	Now func() time.Time
	// Log is given to each job's Run; io.Discard when nil.
	Log io.Writer

	// hook, for tests only, is called at the named points of a wake:
	// "marked", "waiter", "worker", "taken", "idle" and "last".
	hook func(point string)
}

func (o Options) now() time.Time {
	if o.Now != nil {
		return o.Now()
	}
	return time.Now()
}

func (o Options) log() io.Writer {
	if o.Log != nil {
		return o.Log
	}
	return io.Discard
}

func (o Options) at(point string) {
	if o.hook != nil {
		o.hook(point)
	}
}

// inside counts the callers inside the worker or the waiter in this
// process, and peak the most it has been, for tests: however many wakes
// arrive, never more than two.
var inside, peak atomic.Int32

func enter() {
	n := inside.Add(1)
	for p := peak.Load(); n > p && !peak.CompareAndSwap(p, n); p = peak.Load() {
	}
}

func leave() { inside.Add(-1) }

// Wake writes marks, none for a tick, and then sees that they are run:
//
//  1. It tries worker.lock. If it gets it, it is the worker.
//  2. If the worker lock is held, it tries waiter.lock without waiting. If
//     that is held too, it returns: the waiter will see the marks.
//  3. As the waiter, it blocks on worker.lock until the worker releases it
//     or ctx ends, then drops waiter.lock and is the worker.
//
// The worker runs the registered kind with marks and the lowest priority,
// one job at a time, until none is left; a kind whose run ended busy or cut
// is not run again in this wake. It then releases worker.lock and lists the
// marks once more: if any it would run are there, it starts again at step
// 1, so a mark written while it was finishing always has a process to run
// it. Every mark must be of a registered kind. Wake returns nil when the
// worker is busy and a waiter exists.
func Wake(ctx context.Context, dirs paths.Dirs, marks []Mark, o Options) error {
	for _, m := range marks {
		if err := m.check(); err != nil {
			return err
		}
	}
	root, err := openWork(dirs)
	if err != nil {
		return err
	}
	defer root.Close()
	for _, m := range marks {
		if err := writeMark(ctx, root, m); err != nil {
			return err
		}
	}
	o.at("marked")
	locks := dirs.Locks()
	w := &worker{root: root, o: o, skip: map[string]bool{}}
	for {
		unlock, busy, err := paths.TryLock(locks, workerLock)
		if err != nil {
			return fmt.Errorf("work: %w", err)
		}
		if busy {
			unlockWaiter, busy, err := paths.TryLock(locks, waiterLock)
			if err != nil {
				return fmt.Errorf("work: %w", err)
			}
			if busy {
				return nil
			}
			enter()
			o.at("waiter")
			unlock, err = lockWait(ctx, locks, workerLock)
			unlockWaiter()
			if err != nil {
				leave()
				return err
			}
		} else {
			enter()
		}
		o.at("worker")
		err = w.run(ctx)
		o.at("idle")
		// Leave before unlocking, so that the next worker and its waiter
		// never meet this one inside.
		leave()
		unlock()
		if err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		o.at("last")
		left, err := w.runnable()
		if err != nil {
			return err
		}
		if len(left) == 0 {
			return nil
		}
	}
}
