package ledgertest

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

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
	// that is not itself a copy, nor withheld, nor a tombstone; no two
	// events that are not copies share a key; and no event takes another
	// place's key, place:<seq> (SPEC.md section 8).
	ClauseCopies Clause = "copies"
	// ClauseResolve: a second listing lists the keys of the first, in any
	// order, and a second read gives what the first gave; every event's
	// reference e:<session>#<seq> parses and names its session; and
	// ledger.Resolve over the reader gives, for the events of each session
	// it is tried on, that event, or what the sessions the reference names
	// hold for it (see Check).
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
// seqs, copies and counts; then resolve (see checkResolves).
//
// Resolve is tried through r on every event of a session of at most 8
// events, and on 8 of a longer one: its first, its last and 6 spread
// evenly between them. A reader whose later reads change an event it does
// not try can pass; the second read, which must equal the first, is what
// holds every event.
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

// resolveOnReader is how many events of a session Check resolves through
// the reader itself: all of a session with no more, and otherwise its first,
// its last and as many spread evenly between them.
const resolveOnReader = 8

// checkResolves holds r to the clause resolve. Resolve lists and reads
// again, so a second listing must hold the keys of the first, each once, in
// any order, and a second read of every session must give what the first
// gave. Resolve over r itself must then give the events resolveOnReader
// picks, and the reference of every event must parse and name its session.
// Resolve reads each session it names once for every call, so resolving
// every event of a long session through r would read that session once for
// each of its events; resolving at most resolveOnReader keeps Check linear
// in the size of the record.
func checkResolves(ctx context.Context, r ledger.Reader, keys []ledger.Key, reads map[ledger.Key]read) error {
	again, err := r.Sessions(ctx)
	if err := ctx.Err(); err != nil {
		return err
	}
	if err != nil {
		return fmt.Errorf("ledgertest: the second listing fails: %w", err)
	}
	listed := map[ledger.Key]bool{}
	for _, k := range again {
		switch {
		case listed[k]:
			return fault(k, 0, ClauseResolve, "a second listing lists it twice")
		case !hasKey(reads, k):
			return fault(k, 0, ClauseResolve, "a second listing lists it, and the first does not")
		}
		listed[k] = true
	}
	for _, k := range keys {
		if !listed[k] {
			return fault(k, 0, ClauseResolve, "a second listing does not list it")
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
		events := reads[k].events
		for _, e := range events {
			ref := refOf(k, e.Seq)
			if parsed, err := event.ParseRef(ref.String()); err != nil || !parsed.Names(k.Session) || parsed.First != e.Seq {
				return fault(k, e.Seq, ClauseResolve, "no reference names it: %s does not parse back (%v)", ref, err)
			}
		}
		for _, i := range picks(len(events)) {
			if err := checkResolve(ctx, r, keys, reads, k, events[i]); err != nil {
				return err
			}
		}
	}
	return nil
}

func hasKey(reads map[ledger.Key]read, k ledger.Key) bool {
	_, ok := reads[k]
	return ok
}

// picks gives the indexes of the events of a session of n that Check
// resolves through the reader: every one when n is at most
// resolveOnReader, and otherwise the first, the last and the rest spread
// evenly between them, resolveOnReader in all.
func picks(n int) []int {
	if n <= resolveOnReader {
		out := make([]int, n)
		for i := range out {
			out[i] = i
		}
		return out
	}
	out := make([]int, resolveOnReader)
	for j := range out {
		out[j] = j * (n - 1) / (resolveOnReader - 1)
	}
	return out
}

// refOf is the reference of seq in the session k names: its whole id, or
// its first 8 characters for an id longer than a reference carries whole.
func refOf(k ledger.Key, seq int64) event.Ref {
	ref := event.Ref{Session: k.Session, First: seq, Last: seq}
	if len(k.Session) > 128 {
		ref.Session = event.Prefix(k.Session)
	}
	return ref
}

// checkResolve holds Resolve of e's reference to what the sessions it
// names hold for it, as Check read them.
func checkResolve(ctx context.Context, r ledger.Reader, keys []ledger.Key, reads map[ledger.Key]read, k ledger.Key, e event.Event) error {
	ref := refOf(k, e.Seq)
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

// placePrefix begins every place key, event.PlaceKey's form.
var placePrefix = strings.TrimSuffix(event.PlaceKey(0), "0")

// checkEvents holds a session's events to the clauses seqs and copies. A
// copy is never of a withheld event or a tombstone, and no event takes the
// place key of another place (SPEC.md section 8: such an event is never a
// copy and never copied).
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
		if first, ok := bySeq[e.Copy]; e.Copy != 0 && ok && (first.Has(event.MarkWithheld) || first.Kind == event.KindGone) {
			// A withheld event and a tombstone hold nothing but their place,
			// so a copy of one could only carry what they withhold.
			return fault(k, e.Seq, ClauseCopies, "a copy of #%d, which holds nothing but its place", e.Copy)
		}
		if strings.HasPrefix(e.Src.Key, placePrefix) && e.Src.Key != event.PlaceKey(e.Seq) {
			// A place key is never another event's key (SPEC.md section 8).
			return fault(k, e.Seq, ClauseCopies, "keyed by another place, %s", e.Src.Key)
		}
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
