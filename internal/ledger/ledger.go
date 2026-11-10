// Package ledger is the one reader of the record: everything that reads
// captured chats reads them through a Reader, as typed events with ids,
// authors, exposure marks and marked cuts (see SPEC.md beside this file).
// The model is in internal/ledger/event; internal/ledger/inboxv1 is a
// read-only backend over v1's inbox files.
package ledger

import (
	"context"
	"errors"

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

// ErrAmbiguous is returned by Resolve when more than one session matches a
// reference by its first 8 characters, or one session id is kept in more
// than one folder; the full session id, or the folder, tells them apart.
var ErrAmbiguous = errors.New("ledger: the reference matches more than one session")

// Resolve returns the session and the event a short reference names, e:
// or v1's s:. For a range it returns the first event. A reference that
// names a withheld event resolves to that event, which holds nothing but
// its place. A v1 reference names a line, and resolves here as the event
// whose seq is that line, which is what the v1 backend yields; a backend
// whose seq is not v1's line maps the reference first.
func Resolve(ctx context.Context, r Reader, ref string) (event.Session, event.Event, error) {
	parsed, err := event.ParseRef(ref)
	if err != nil {
		return event.Session{}, event.Event{}, err
	}
	keys, err := r.Sessions(ctx)
	if err != nil {
		return event.Session{}, event.Event{}, err
	}
	var matches []Key
	for _, k := range keys {
		if parsed.Names(k.Session) {
			matches = append(matches, k)
		}
	}
	switch len(matches) {
	case 0:
		return event.Session{}, event.Event{}, ErrNotFound
	case 1:
	default:
		return event.Session{}, event.Event{}, ErrAmbiguous
	}
	s, events, err := r.Read(ctx, matches[0])
	if err != nil {
		return event.Session{}, event.Event{}, err
	}
	for _, e := range events {
		if e.Seq == parsed.First {
			return s, e, nil
		}
	}
	return event.Session{}, event.Event{}, ErrNotFound
}
