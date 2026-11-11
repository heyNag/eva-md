package work

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"regexp"
	"slices"
	"time"
)

// A worker is the process that holds worker.lock, for one wake.
type worker struct {
	root *os.Root
	o    Options
	// skip holds the kinds whose run ended busy or cut in this wake: they
	// are not run again until the next wake.
	skip   map[string]bool
	pruned bool
}

// run recovers what a killed worker left, then runs jobs until no mark it
// may run is left or ctx ends.
func (w *worker) run(ctx context.Context) error {
	if err := w.recover(); err != nil {
		return err
	}
	if !w.pruned {
		if err := prune(w.root, w.o.now()); err != nil {
			return err
		}
		w.pruned = true
	}
	for ctx.Err() == nil {
		marks, err := w.runnable()
		if err != nil {
			return err
		}
		if len(marks) == 0 {
			return nil
		}
		if err := w.runNext(ctx, marks); err != nil {
			return err
		}
	}
	return nil
}

// runnable lists the marks in wake/ of registered kinds that this wake may
// still run.
func (w *worker) runnable() ([]listed, error) {
	marks, err := listMarks(w.root, wakeDir)
	if err != nil {
		return nil, err
	}
	return slices.DeleteFunc(marks, func(m listed) bool { return w.skip[m.Kind] }), nil
}

// runNext runs the job of the lowest priority that has marks, given every
// mark of its kind.
func (w *worker) runNext(ctx context.Context, marks []listed) error {
	var job Job
	found := false
	for _, m := range marks {
		j, ok := registered(m.Kind)
		if ok && (!found || j.Priority < job.Priority || j.Priority == job.Priority && j.Kind < job.Kind) {
			job, found = j, true
		}
	}
	if !found {
		return nil
	}
	mine := slices.DeleteFunc(slices.Clone(marks), func(m listed) bool { return m.Kind != job.Kind })
	id, err := newRunID()
	if err != nil {
		return err
	}
	taken, err := take(w.root, id, mine)
	if err != nil {
		return err
	}
	if len(taken) == 0 {
		return drop(w.root, id)
	}
	w.o.at("taken")
	start := startEntry(w.o.now(), id, job, taken)
	// No job runs without its start line on disk.
	if err := appendJournal(w.root, start); err != nil {
		return errors.Join(err, putBack(w.root, id))
	}
	began := time.Now()
	out := call(ctx, job, taken, w.o.log())
	ms := time.Since(began).Milliseconds()
	finish := start
	finish.T, finish.Ev, finish.Outcome, finish.Ms = stamp(w.o.now()), evFinish, out, &ms
	if err := appendJournal(w.root, finish); err != nil {
		// Leave run/<id> as it is: the next worker finds a start with no
		// finish, journals it as interrupted and puts its marks back.
		return err
	}
	if out.consumes() {
		return drop(w.root, id)
	}
	w.skip[job.Kind] = true
	return putBack(w.root, id)
}

// call runs j. An outcome that is none of the four, or a panic, is failed.
func call(ctx context.Context, j Job, marks []listed, log io.Writer) (out Outcome) {
	defer func() {
		if recover() != nil {
			fmt.Fprintf(log, "eva-md: the %s job stopped on an internal error\n", j.Kind)
			out = Failed
		}
	}()
	given := make([]Mark, len(marks))
	for i, m := range marks {
		given[i] = m.Mark
	}
	switch out = j.Run(ctx, given, log); out {
	case Done, Busy, Failed, Cut:
		return out
	}
	return Failed
}

var runIDPattern = regexp.MustCompile(`^[0-9a-f]{16}$`)

func newRunID() (string, error) {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("work: run id: %w", err)
	}
	return hex.EncodeToString(b[:]), nil
}

// recover settles each run/<id>/ a killed worker left, by the journal's
// lines for that run: a run that finished done or failed used its marks;
// any other puts them back, and a start with neither a finish nor an
// interrupted line first gains an interrupted line.
func (w *worker) recover() error {
	f, err := w.root.Open(runDir)
	if err != nil {
		return fmt.Errorf("work: %w", err)
	}
	names, err := f.Readdirnames(-1)
	f.Close()
	if err != nil {
		return fmt.Errorf("work: %w", err)
	}
	slices.Sort(names)
	for _, id := range names {
		if !runIDPattern.MatchString(id) {
			continue
		}
		lines, err := runLines(w.root, id)
		if err != nil {
			return err
		}
		var start, end *entry
		for i := range lines {
			switch lines[i].Ev {
			case evStart:
				start = &lines[i]
			case evFinish, evInterrupted:
				end = &lines[i]
			}
		}
		switch {
		case end != nil && end.Ev == evFinish && end.Outcome.consumes():
			err = drop(w.root, id)
		case start != nil && end == nil:
			cut := *start
			cut.T, cut.Ev = stamp(w.o.now()), evInterrupted
			if err = appendJournal(w.root, cut); err == nil {
				err = putBack(w.root, id)
			}
		default:
			err = putBack(w.root, id)
		}
		if err != nil {
			return err
		}
	}
	return nil
}
