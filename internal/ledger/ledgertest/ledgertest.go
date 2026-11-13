// Package ledgertest is a fake of the record's one reader, ledger.Reader,
// for tests, and the check that holds the fake and every backend to the
// same contract (internal/ledger/SPEC.md). Only tests import it.
//
// A test builds the sessions it needs as events, of any shape, such as a
// voice hand-over, a second copy or a withheld line, without writing a v1
// inbox:
//
//	f, err := ledgertest.New(ledgertest.Session{Key: k, Class: event.ClassChat, Events: events})
//
// New refuses what the reader's contract forbids: an event that
// event.Event.Validate refuses, seqs that do not rise, a copy that does not
// name an earlier event with its key and turn that is not itself a copy,
// two events that are not copies with one key, a key given twice, a key
// with no host or a session id no reference can name, and a class that is
// not valid. So whatever New accepts passes Check.
//
// The fake lists the keys in the order given and reads each session as
// copies of its events, so a caller cannot change it. A session's record
// takes its host, id and folder from its key, its class from the Session,
// and its counts from its events, as SPEC.md section 7 defines them.
// FailSessions and FailRead make those calls fail, and a cancelled context
// ends either with its error.
//
// ReadSession reads a session from a file in the codec's form: a header
// line (event.DecodeHeader) that names the key's host and session, then one
// event line (event.Decode) for each event, each line ending in a newline.
//
// Check holds any ledger.Reader to the contract and returns the first
// Fault, which names the session, the seq where there is one, and the
// clause it breaks.
package ledgertest

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"slices"
	"strings"
	"sync"

	"github.com/heyNag/eva-md/internal/ledger"
	"github.com/heyNag/eva-md/internal/ledger/event"
)

// Session is one session the fake holds: its key, its class and its
// events, in order.
type Session struct {
	Key    ledger.Key
	Class  event.Class
	Events []event.Event
}

// ErrNoSession is returned by Read for a key the fake does not hold.
var ErrNoSession = errors.New("ledgertest: no such session")

// Fake is a ledger.Reader over sessions held in memory. It is safe for
// concurrent use.
type Fake struct {
	mu           sync.Mutex
	sessions     []Session
	failSessions error
	failRead     map[ledger.Key]error
}

var _ ledger.Reader = (*Fake)(nil)

// New makes a fake that holds ss, in that order. It refuses sessions the
// reader's contract forbids (see the package comment). It keeps copies, so
// a caller that changes ss afterwards does not change the fake.
func New(ss ...Session) (*Fake, error) {
	f := &Fake{failRead: map[ledger.Key]error{}}
	seen := map[ledger.Key]bool{}
	for _, s := range ss {
		k := s.Key
		switch {
		case seen[k]:
			return nil, fmt.Errorf("ledgertest: the key %s is given twice", name(k))
		case k.Host == "" || !event.ValidSession(k.Session):
			return nil, fmt.Errorf("ledgertest: the key %s names no host or no valid session", name(k))
		case !s.Class.Valid():
			return nil, fmt.Errorf("ledgertest: %s: class %q is not valid", name(k), s.Class)
		}
		seen[k] = true
		if err := checkEvents(k, s.Events); err != nil {
			return nil, err
		}
		f.sessions = append(f.sessions, Session{Key: k, Class: s.Class, Events: cloneAll(s.Events)})
	}
	return f, nil
}

// Sessions lists the keys in the order New was given them.
func (f *Fake) Sessions(ctx context.Context) ([]ledger.Key, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failSessions != nil {
		return nil, f.failSessions
	}
	keys := make([]ledger.Key, 0, len(f.sessions))
	for _, s := range f.sessions {
		keys = append(keys, s.Key)
	}
	return keys, nil
}

// Read returns the session k names: its record and copies of its events.
func (f *Fake) Read(ctx context.Context, k ledger.Key) (event.Session, []event.Event, error) {
	if err := ctx.Err(); err != nil {
		return event.Session{}, nil, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.failRead[k]; err != nil {
		return event.Session{}, nil, err
	}
	i := slices.IndexFunc(f.sessions, func(s Session) bool { return s.Key == k })
	if i < 0 {
		return event.Session{}, nil, fmt.Errorf("%w: %s", ErrNoSession, name(k))
	}
	s := f.sessions[i]
	rec := event.Session{Host: k.Host, ID: k.Session, Folder: k.Folder, Class: s.Class, Events: len(s.Events)}
	for _, e := range s.Events {
		if e.Copy != 0 {
			rec.Copies++
		}
		if e.Has(event.MarkWithheld) {
			rec.Withheld++
		}
		if e.Has(event.MarkWrapper) {
			rec.Wrappers++
		}
	}
	return rec, cloneAll(s.Events), nil
}

// FailSessions makes every later call of Sessions return err; nil ends it.
func (f *Fake) FailSessions(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.failSessions = err
}

// FailRead makes every later call of Read for k return err; nil ends it.
func (f *Fake) FailRead(k ledger.Key, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err == nil {
		delete(f.failRead, k)
		return
	}
	f.failRead[k] = err
}

// ReadSession reads the session file name of fsys: a header line in the
// codec's form that names k's host and session, then one event line for
// each event, every line ending in a newline. It returns the session with
// key k and the class given; New checks its events.
func ReadSession(fsys fs.FS, name string, k ledger.Key, class event.Class) (Session, error) {
	data, err := fs.ReadFile(fsys, name)
	if err != nil {
		return Session{}, err
	}
	body, ok := strings.CutSuffix(string(data), "\n")
	if !ok {
		return Session{}, fmt.Errorf("ledgertest: %s: the last line does not end in a newline", name)
	}
	lines := strings.Split(body, "\n")
	h, err := event.DecodeHeader([]byte(lines[0]))
	if err != nil {
		return Session{}, fmt.Errorf("ledgertest: %s line 1: %w", name, err)
	}
	if h.Host != k.Host || h.Session != k.Session {
		return Session{}, fmt.Errorf("ledgertest: %s: the header names %s:%s, not %s:%s", name, h.Host, h.Session, k.Host, k.Session)
	}
	s := Session{Key: k, Class: class}
	for i, l := range lines[1:] {
		e, err := event.Decode([]byte(l))
		if err != nil {
			return Session{}, fmt.Errorf("ledgertest: %s line %d: %w", name, i+2, err)
		}
		s.Events = append(s.Events, e)
	}
	return s, nil
}

// clone copies e so that it shares nothing with the original: every slice
// and the context are copied.
func clone(e event.Event) event.Event {
	e.Red = slices.Clone(e.Red)
	e.Cut = slices.Clone(e.Cut)
	e.Exposure = slices.Clone(e.Exposure)
	e.Marks = slices.Clone(e.Marks)
	if e.Ctx != nil {
		c := *e.Ctx
		e.Ctx = &c
	}
	return e
}

func cloneAll(events []event.Event) []event.Event {
	if events == nil {
		return nil
	}
	out := make([]event.Event, len(events))
	for i, e := range events {
		out[i] = clone(e)
	}
	return out
}

// name is how a fault names a session: its host and id, and its folder
// when it has one.
func name(k ledger.Key) string {
	s := k.Host + ":" + k.Session
	if k.Folder != "" {
		s += " in " + k.Folder
	}
	return s
}
