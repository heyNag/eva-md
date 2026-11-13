// Package storetest is the store's fake: an in-memory store over the
// reference model in internal/store/conformance, for the tests of packages
// that write to the store before the store core lands. It adds no
// behaviour of its own: every outcome, refusal, commit and error is the
// model's, so the fake keeps the five properties the model keeps.
//
// Nothing in eva-md imports this package but tests.
package storetest

import (
	"errors"
	"time"

	"github.com/heyNag/eva-md/internal/store"
	"github.com/heyNag/eva-md/internal/store/conformance"
)

// Item, Commit and Edit are the model's, so that a test can read and
// hand-edit the fake without importing the model.
type (
	Item   = conformance.Item
	Commit = conformance.Commit
	Edit   = conformance.Edit
)

// The hand edits, as the model makes them.
const (
	EditChange = conformance.EditChange // the statement is rewritten
	EditLock   = conformance.EditLock   // a line locked: true  # note is added
	EditVerify = conformance.EditVerify // a verified mark is added
	EditGarble = conformance.EditGarble // the file is made unreadable
)

// The model's errors.
var (
	ErrBusy    = conformance.ErrBusy
	ErrLost    = conformance.ErrLost
	ErrClosed  = conformance.ErrClosed
	ErrStopped = conformance.ErrStopped
	ErrTxn     = conformance.ErrTxn
	ErrNoFile  = conformance.ErrNoFile
	ErrRestore = conformance.ErrRestore
)

// ErrNoClock is New's error when it is given no clock.
var ErrNoClock = errors.New("storetest: no clock")

// Fake is the store as one process holds it, over the model's durable
// part, which outlives Close and Reopen. Like the store, it is used by one
// goroutine at a time.
type Fake struct {
	m   *conformance.Model
	now conformance.Clock
	st  store.Store // the open store; after Close or a failed Reopen, a closed one
}

var _ store.Store = (*Fake)(nil)

// New makes a store and opens it with no fault. now is the only time the
// fake reads: when it adopts a hand edit, and when it makes the store.
func New(now func() time.Time) (*Fake, error) {
	if now == nil {
		return nil, ErrNoClock
	}
	f := &Fake{m: conformance.NewModel(), now: now}
	st, err := f.m.Open(f.now, nil)
	if err != nil {
		return nil, err
	}
	f.st = st
	return f, nil
}

// Reopen closes the store and opens it again on its durable part, as a
// process's death and restart would: an unfinished pass is undone and a
// hand edit adopted. The fault, when not nil, is called at each step of
// Apply; a nil fault opens it with none. When the open fails, the fake
// stays closed and every call but Close returns the model's error.
func (f *Fake) Reopen(fault store.Fault) error {
	if err := f.st.Close(); err != nil {
		return err
	}
	st, err := f.m.Open(f.now, fault)
	if err != nil {
		return err
	}
	f.st = st
	return nil
}

// Apply runs a pass, as the model does.
func (f *Fake) Apply(txn store.Txn) (store.Result, error) { return f.st.Apply(txn) }

// Plan returns the outcomes Apply would give, and writes nothing.
func (f *Fake) Plan(txn store.Txn) ([]store.Outcome, error) { return f.st.Plan(txn) }

// Coverage is the ranges the store's tree holds for the session.
func (f *Fake) Coverage(session string) ([]store.Range, error) { return f.st.Coverage(session) }

// Close releases the store's lock, and nothing else: the files, commits
// and remote stay for Reopen and the readers below.
func (f *Fake) Close() error { return f.st.Close() }

// Files is the store folder's working tree, path to bytes, as copies.
func (f *Fake) Files() map[string][]byte { return f.m.Files() }

// History is every commit, oldest first.
func (f *Fake) History() []Commit { return f.m.History() }

// Items is every item file in the working tree, by path.
func (f *Fake) Items() []Item { return f.m.Items() }

// SessionFile is the path of a session's file, and whether that file is in
// the working tree.
func (f *Fake) SessionFile(session string) (string, bool) { return f.m.SessionFile(session) }

// HandEdit changes a file, an item's or a session's, as a person or
// another tool would, outside the store's lock. The store adopts it, as a
// commit of kind human, at the next Apply or Reopen; Plan reads it as
// adopted and writes nothing.
func (f *Fake) HandEdit(path string, e Edit) error { return f.m.HandEdit(path, e) }

// Push copies the store's commits to its remote.
func (f *Fake) Push() error { return f.m.Push() }

// Lose deletes the store folder, and nothing outside it. The store must be
// closed.
func (f *Fake) Lose() error { return f.m.Lose() }

// Restore brings a lost store folder back from the remote's last push, or
// as a new store with the same id when there was none. Reopen opens it.
func (f *Fake) Restore() error { return f.m.Restore() }
