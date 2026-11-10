// Package ledger is the one reader of the record: everything that reads
// captured chats reads them through a Reader, as typed events with ids,
// authors, exposure marks and marked cuts (see SPEC.md beside this file).
// The model is in internal/ledger/event; internal/ledger/inboxv1 is a
// read-only backend over v1's inbox files.
package ledger

import (
	"context"
	"errors"
	"slices"

	"github.com/heyNag/eva-md/internal/ledger/event"
)

// Key names one session a backend holds.
type Key struct {
	Host    string
	Session string
	Folder  string // where the backend keeps it; v1: the project folder
	File    string // the backend's name for its file; v1: <host>-<session>.jsonl
}

// Reader reads sessions. It returns nothing said off the record and never
// writes.
type Reader interface {
	// Sessions lists the sessions the backend holds, without reading them.
	Sessions(ctx context.Context) ([]Key, error)
	// Read reads one session: its record and every event, in order.
	Read(ctx context.Context, k Key) (event.Session, []event.Event, error)
}

// ErrNotFound is returned by Resolve when no session holds the event.
var ErrNotFound = errors.New("ledger: no such event")

// ErrAmbiguous is returned by Resolve when more than one session the
// reference names holds its event: two sessions that share their first 8
// characters, or one session id kept in more than one folder. The full
// session id tells the first apart; nothing in a reference tells the
// second apart until the folders are joined.
var ErrAmbiguous = errors.New("ledger: the reference matches more than one session")

// Resolve returns the session and the event a short reference names, e:
// or v1's s:. For a range it returns the first event. A reference that
// names a withheld event resolves to that event, which holds nothing but
// its place. A v1 reference names a line, and resolves here as the event
// whose seq is that line, which is what the v1 backend yields; a backend
// whose seq is not v1's line maps the reference first. Of the sessions the
// reference names, only those that hold the event count, as in v1's
// eva-md show; it is ambiguous only when more than one does.
func Resolve(ctx context.Context, r Reader, ref string) (event.Session, event.Event, error) {
	parsed, err := event.ParseRef(ref)
	if err != nil {
		return event.Session{}, event.Event{}, err
	}
	keys, err := r.Sessions(ctx)
	if err != nil {
		return event.Session{}, event.Event{}, err
	}
	// Every session the reference names is read, and those that hold the
	// event are kept, as v1's eva-md show keeps only the files that hold
	// the line: a session v1 wrote into two folders, or two sessions that
	// share their first 8 characters, still resolve when only one holds it.
	type hit struct {
		s event.Session
		e event.Event
	}
	var hits []hit
	for _, k := range keys {
		if !parsed.Names(k.Session) {
			continue
		}
		s, events, err := r.Read(ctx, k)
		if err != nil {
			return event.Session{}, event.Event{}, err
		}
		for _, e := range events {
			if e.Seq == parsed.First {
				hits = append(hits, hit{s, e})
				break
			}
		}
	}
	if len(hits) > 1 {
		// Where more than one holds it, v1 keeps a line only when it
		// decodes and belongs to its file: not one it repaired, and not one
		// withheld as another session's.
		hits = slices.DeleteFunc(hits, func(h hit) bool {
			return h.e.Has(event.MarkRepaired) || h.e.Has(event.MarkWithheld) && h.e.Why == event.WhyUntrusted
		})
		if len(hits) != 1 {
			return event.Session{}, event.Event{}, ErrAmbiguous
		}
	}
	if len(hits) == 0 {
		return event.Session{}, event.Event{}, ErrNotFound
	}
	return hits[0].s, hits[0].e, nil
}
