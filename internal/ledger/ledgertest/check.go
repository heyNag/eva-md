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
	// ClauseRead: every listed key reads, and is listed once, and its
	// record has the key's host and session and a valid class.
	ClauseRead Clause = "read"
	// ClauseCounts: the record's counts of events, copies, withheld events
	// and wrappers equal its events' (SPEC.md section 7).
	ClauseCounts Clause = "counts"
	// ClauseSeqs: seqs rise, from 1 or more, and every event validates.
	ClauseSeqs Clause = "seqs"
	// ClauseCopies: a copy names an earlier event with its key and turn
	// that is not itself a copy, and no two events that are not copies
	// share a key (SPEC.md section 8).
	ClauseCopies Clause = "copies"
	// ClauseResolve: a second listing and a second read give what the
	// first gave, and ledger.Resolve of e:<session>#<seq> gives that
	// event, or what the sessions the reference names hold for it (see
	// Check).
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

// read is what Check read of one session.
type read struct {
	s      event.Session
	events []event.Event
}

// Check holds r to the reader's contract (internal/ledger/SPEC.md sections
// 1, 7, 8 and 10) and returns the first Fault, or the error of a listing
// that fails or of a context that ends. It lists the sessions and reads
// each, and checks, for each in the listing's order, the clauses read,
// seqs, copies and counts; then resolve (see checkResolves), for every
// event of every session.
//
// A reference names a session by its whole id, or by its first 8
// characters, so e:<session>#<seq> names every listed session with that id,
// in any folder, and every one whose first 8 characters are the whole id.
// When it names this session alone, Resolve must give this event. When it
// names more than one, Resolve must give what those sessions hold for it
// as Check read them, by SPEC.md section 9: the one session that holds
// that seq, or of those the one whose event decodes and belongs to its
// file, and otherwise ledger.ErrAmbiguous. So a session kept in more than
// one folder may give ErrAmbiguous, and a session that is alone never does.
func Check(ctx context.Context, r ledger.Reader) error {
	keys, err := r.Sessions(ctx)
	if err != nil {
		return fmt.Errorf("ledgertest: the listing fails: %w", err)
	}
	reads := map[ledger.Key]read{}
	for _, k := range keys {
		if _, ok := reads[k]; ok {
			return fault(k, 0, ClauseRead, "listed twice")
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
		want := count(events)
		if s.Events != want.Events || s.Copies != want.Copies || s.Withheld != want.Withheld || s.Wrappers != want.Wrappers {
			return fault(k, 0, ClauseCounts, "the record counts %d events, %d copies, %d withheld and %d wrappers; the events are %d, %d, %d and %d",
				s.Events, s.Copies, s.Withheld, s.Wrappers, want.Events, want.Copies, want.Withheld, want.Wrappers)
		}
		reads[k] = read{s, events}
	}
	return checkResolves(ctx, r, keys, reads)
}

// checkResolves holds r to the clause resolve. Resolve lists and reads
// again, so a second listing and a second read of every session must give
// what the first gave. Resolve over r itself must then give the first
// event of every session, and Resolve over what r read every event: a
// session is read once for each, not once for each of its events, so Check
// stays linear in the size of the record.
func checkResolves(ctx context.Context, r ledger.Reader, keys []ledger.Key, reads map[ledger.Key]read) error {
	again, err := r.Sessions(ctx)
	if err := ctx.Err(); err != nil {
		return err
	}
	if err != nil {
		return fmt.Errorf("ledgertest: the second listing fails: %w", err)
	}
	for i := range max(len(keys), len(again)) {
		if i >= len(keys) || i >= len(again) || keys[i] != again[i] {
			k := ledger.Key{}
			if i < len(keys) {
				k = keys[i]
			} else {
				k = again[i]
			}
			return fault(k, 0, ClauseResolve, "a second listing differs from the first here")
		}
	}
	for _, k := range keys {
		s, events, err := r.Read(ctx, k)
		if err := ctx.Err(); err != nil {
			return err
		}
		first := reads[k]
		switch {
		case err != nil:
			return &Fault{Key: k, Clause: ClauseResolve, Err: fmt.Errorf("a second read: %w", err)}
		case s != first.s:
			return fault(k, 0, ClauseResolve, "a second read gives the record %+v, the first %+v", s, first.s)
		case len(events) != len(first.events):
			return fault(k, 0, ClauseResolve, "a second read gives %d events, the first %d", len(events), len(first.events))
		}
		for i, e := range events {
			if !same(e, first.events[i]) {
				return fault(k, first.events[i].Seq, ClauseResolve, "a second read gives another event here")
			}
		}
	}
	for _, k := range keys {
		if events := reads[k].events; len(events) > 0 {
			if err := checkResolve(ctx, r, keys, reads, k, events[0]); err != nil {
				return err
			}
		}
	}
	held := frozen{keys, reads}
	for _, k := range keys {
		for _, e := range reads[k].events {
			if err := checkResolve(ctx, held, keys, reads, k, e); err != nil {
				return err
			}
		}
	}
	return nil
}

// frozen is a reader of what Check read.
type frozen struct {
	keys  []ledger.Key
	reads map[ledger.Key]read
}

func (f frozen) Sessions(context.Context) ([]ledger.Key, error) { return f.keys, nil }

func (f frozen) Read(_ context.Context, k ledger.Key) (event.Session, []event.Event, error) {
	r, ok := f.reads[k]
	if !ok {
		return event.Session{}, nil, fmt.Errorf("%w: %s", ErrNoSession, name(k))
	}
	return r.s, r.events, nil
}

// checkResolve holds Resolve of e's reference to what the sessions it
// names hold for it, as Check read them.
func checkResolve(ctx context.Context, r ledger.Reader, keys []ledger.Key, reads map[ledger.Key]read, k ledger.Key, e event.Event) error {
	ref := event.Ref{Session: k.Session, First: e.Seq, Last: e.Seq}
	if len(k.Session) > 128 {
		// Longer than a reference carries whole.
		ref.Session = event.Prefix(k.Session)
	}
	// What Resolve must give: of the sessions the reference names, those
	// that hold the seq; where more than one does, those whose event
	// decodes and belongs to its file; and ambiguity unless one is left.
	var hits []read
	for _, other := range keys {
		if !ref.Names(other.Session) {
			continue
		}
		o := reads[other]
		for _, oe := range o.events {
			if oe.Seq == e.Seq {
				hits = append(hits, read{o.s, []event.Event{oe}})
				break
			}
		}
	}
	if len(hits) > 1 {
		var kept []read
		for _, h := range hits {
			if he := h.events[0]; !he.Has(event.MarkRepaired) && !(he.Has(event.MarkWithheld) && he.Why == event.WhyUntrusted) {
				kept = append(kept, h)
			}
		}
		hits = kept
	}
	s, found, err := ledger.Resolve(ctx, r, ref.String())
	if err := ctx.Err(); err != nil {
		return err
	}
	switch {
	case len(hits) != 1 && errors.Is(err, ledger.ErrAmbiguous):
		return nil
	case len(hits) != 1 && err != nil:
		return fault(k, e.Seq, ClauseResolve, "%s names sessions that hold #%d alike, and Resolve gives %w, not ambiguity", ref, e.Seq, err)
	case len(hits) != 1:
		return fault(k, e.Seq, ClauseResolve, "%s names sessions that hold #%d alike, and Resolve gives #%d of %s, not ambiguity", ref, e.Seq, found.Seq, recordName(s))
	case err != nil:
		return fault(k, e.Seq, ClauseResolve, "%s: %w", ref, err)
	case s != hits[0].s:
		return fault(k, e.Seq, ClauseResolve, "%s gives the record %+v, not %+v as it reads", ref, s, hits[0].s)
	case !same(found, hits[0].events[0]):
		return fault(k, e.Seq, ClauseResolve, "%s gives another event than #%d of %s as it reads", ref, e.Seq, recordName(hits[0].s))
	}
	return nil
}

// count is a record that holds only the counts of events, as SPEC.md
// section 7 defines them.
func count(events []event.Event) event.Session {
	s := event.Session{Events: len(events)}
	for _, e := range events {
		if e.Copy != 0 {
			s.Copies++
		}
		if e.Has(event.MarkWithheld) {
			s.Withheld++
		}
		if e.Has(event.MarkWrapper) {
			s.Wrappers++
		}
	}
	return s
}

// checkEvents holds a session's events to the clauses seqs and copies.
func checkEvents(k ledger.Key, events []event.Event) error {
	var last int64
	for _, e := range events {
		if e.Seq <= last {
			return fault(k, e.Seq, ClauseSeqs, "seq %d does not rise above %d", e.Seq, last)
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

// recordName is how a fault names a record, as name names a key.
func recordName(s event.Session) string {
	return name(ledger.Key{Host: s.Host, Session: s.ID, Folder: s.Folder})
}
