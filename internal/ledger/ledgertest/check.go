package ledgertest

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strconv"

	"github.com/heyNag/eva-md/internal/ledger"
	"github.com/heyNag/eva-md/internal/ledger/event"
)

// Clause is a clause of the reader's contract that Check holds a reader to.
type Clause string

const (
	// ClauseRead: every listed key reads, and its record has the key's
	// host and session and a valid class.
	ClauseRead Clause = "read"
	// ClauseSeqs: seqs rise, from 1 or more, and every event validates.
	ClauseSeqs Clause = "seqs"
	// ClauseCopies: a copy names an earlier event with its key and turn
	// that is not itself a copy, and no two events that are not copies
	// share a key.
	ClauseCopies Clause = "copies"
	// ClauseCounts: the record's counts of events, copies, withheld
	// events and wrappers equal its events'.
	ClauseCounts Clause = "counts"
	// ClauseResolve: ledger.Resolve of e:<session>#<seq> gives that event,
	// or ledger.ErrAmbiguous when the reference names more than one listed
	// session.
	ClauseResolve Clause = "resolve"
)

// Fault is a way a reader breaks its contract: the session, the event's
// seq (0 when the fault is the session's), the clause and what is wrong.
type Fault struct {
	Key    ledger.Key
	Seq    int64
	Clause Clause
	Err    error
}

func (f *Fault) Error() string {
	at := name(f.Key)
	if f.Seq != 0 {
		at += " #" + strconv.FormatInt(f.Seq, 10)
	}
	return fmt.Sprintf("ledgertest: %s: %s: %v", at, f.Clause, f.Err)
}

func (f *Fault) Unwrap() error { return f.Err }

func fault(k ledger.Key, seq int64, c Clause, format string, args ...any) *Fault {
	return &Fault{Key: k, Seq: seq, Clause: c, Err: fmt.Errorf(format, args...)}
}

// Check holds r to the reader's contract (internal/ledger/SPEC.md sections
// 1, 7, 8 and 10) and returns the first Fault, or the error of a listing
// that fails or of a context that ends. It reads every listed session and
// checks, for each in the listing's order, the clauses in this order:
// read, seqs, copies, counts; then, for every event, resolve.
//
// A reference names a session by its whole id, or by its first 8
// characters, so e:<session>#<seq> names every listed session with that id,
// in any folder, and every one whose first 8 characters are the whole id.
// Where it names more than one, Resolve may give ledger.ErrAmbiguous, or
// that seq's event of another session it names, as that session reads.
func Check(ctx context.Context, r ledger.Reader) error {
	keys, err := r.Sessions(ctx)
	if err != nil {
		return fmt.Errorf("ledgertest: the listing fails: %w", err)
	}
	type read struct {
		s      event.Session
		events []event.Event
	}
	reads := map[ledger.Key]read{}
	for _, k := range keys {
		if _, ok := reads[k]; ok {
			continue
		}
		s, events, err := r.Read(ctx, k)
		if err := ctx.Err(); err != nil {
			return err
		}
		switch {
		case err != nil:
			return &Fault{Key: k, Clause: ClauseRead, Err: err}
		case s.Host != k.Host || s.ID != k.Session:
			return fault(k, 0, ClauseRead, "the record is of %s:%s", s.Host, s.ID)
		case !s.Class.Valid():
			return fault(k, 0, ClauseRead, "class %q is not valid", s.Class)
		}
		if err := checkEvents(k, events); err != nil {
			return err
		}
		var want event.Session
		want.Events = len(events)
		for _, e := range events {
			if e.Copy != 0 {
				want.Copies++
			}
			if e.Has(event.MarkWithheld) {
				want.Withheld++
			}
			if e.Has(event.MarkWrapper) {
				want.Wrappers++
			}
		}
		if s.Events != want.Events || s.Copies != want.Copies || s.Withheld != want.Withheld || s.Wrappers != want.Wrappers {
			return fault(k, 0, ClauseCounts, "the record counts %d events, %d copies, %d withheld and %d wrappers; the events %d, %d, %d and %d",
				s.Events, s.Copies, s.Withheld, s.Wrappers, want.Events, want.Copies, want.Withheld, want.Wrappers)
		}
		reads[k] = read{s, events}
	}
	for _, k := range keys {
		got := reads[k]
		for _, e := range got.events {
			if err := ctx.Err(); err != nil {
				return err
			}
			ref := event.Ref{Session: k.Session, First: e.Seq, Last: e.Seq}
			if len(k.Session) > 128 {
				// The longest session a reference carries whole.
				ref.Session = event.Prefix(k.Session)
			}
			if _, err := event.ParseRef(ref.String()); err != nil {
				return fault(k, e.Seq, ClauseResolve, "%s cannot be read: %v", ref, err)
			}
			named := map[ledger.Key]bool{}
			for _, other := range keys {
				if ref.Names(other.Session) {
					named[other] = true
				}
			}
			s, found, err := ledger.Resolve(ctx, r, ref.String())
			if err := ctx.Err(); err != nil {
				return err
			}
			if err != nil {
				if errors.Is(err, ledger.ErrAmbiguous) && len(named) > 1 {
					continue
				}
				return fault(k, e.Seq, ClauseResolve, "%s: %w", ref, err)
			}
			if s == got.s && same(found, e) {
				continue
			}
			ok := false
			for other := range named {
				o := reads[other]
				if other == k || s != o.s {
					continue
				}
				for _, oe := range o.events {
					if oe.Seq == e.Seq && same(found, oe) {
						ok = true
					}
				}
			}
			if !ok {
				return fault(k, e.Seq, ClauseResolve, "%s gives another event, #%d of %s:%s", ref, found.Seq, s.Host, s.ID)
			}
		}
	}
	return nil
}

// checkEvents holds a session's events to the clauses seqs and copies.
func checkEvents(k ledger.Key, events []event.Event) error {
	var last int64
	for _, e := range events {
		if e.Seq <= last {
			return fault(k, e.Seq, ClauseSeqs, "seq %d does not rise from %d", e.Seq, last)
		}
		last = e.Seq
		if err := e.Validate(); err != nil {
			return &Fault{Key: k, Seq: e.Seq, Clause: ClauseSeqs, Err: err}
		}
	}
	bySeq := map[int64]event.Event{}
	firsts := map[string]int64{} // the sameness key of each event that is not a copy
	for _, e := range events {
		bySeq[e.Seq] = e
		if e.Copy == 0 {
			if seq, ok := firsts[e.Src.Key]; ok {
				return fault(k, e.Seq, ClauseCopies, "not a copy, but its key is #%d's", seq)
			}
			firsts[e.Src.Key] = e.Seq
			continue
		}
		first, ok := bySeq[e.Copy]
		switch {
		case !ok:
			return fault(k, e.Seq, ClauseCopies, "a copy of #%d, which the session does not hold", e.Copy)
		case first.Copy != 0:
			return fault(k, e.Seq, ClauseCopies, "a copy of #%d, which is itself a copy", e.Copy)
		case first.Src.Key != e.Src.Key:
			return fault(k, e.Seq, ClauseCopies, "a copy of #%d with another key", e.Copy)
		case first.Turn != e.Turn:
			return fault(k, e.Seq, ClauseCopies, "a copy of #%d in turn %d, not its turn %d", e.Copy, e.Turn, first.Turn)
		}
	}
	return nil
}

// same reports two events that encode alike: the codec's form is an
// event's whole value.
func same(a, b event.Event) bool {
	ea, errA := event.Encode(a)
	eb, errB := event.Encode(b)
	return errA == nil && errB == nil && bytes.Equal(ea, eb)
}
