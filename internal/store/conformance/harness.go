package conformance

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"maps"
	"reflect"
	"slices"
	"time"

	"github.com/heyNag/eva-md/internal/store"
)

// Clock is the time a subject reads. The harness moves it only between
// passes, and never reads the wall clock.
type Clock func() time.Time

// Subject is a store under test, with what the properties look at.
type Subject interface {
	// Open takes the store's lock, recovers, and returns the store. The
	// fault, when not nil, is called at each step of Apply.
	Open(clock Clock, fault store.Fault) (store.Store, error)
	// Files is the store folder's working tree, path to bytes.
	Files() map[string][]byte
	// History is every commit, oldest first.
	History() []Commit
	// Items is every item file, by path.
	Items() []Item
	// HandEdit changes a file as a person or another tool would.
	HandEdit(path string, e Edit) error
}

// Restorable is a subject with a remote: it can push, lose its store
// folder and nothing else, and restore it.
type Restorable interface {
	Subject
	Push() error
	Lose() error
	Restore() error
}

// Commit is one commit as the history shows it: its pass, its kind, the
// ranges it covers and its counts. A hand edit's commit has no pass.
type Commit struct {
	ID      string
	Pass    string
	Kind    string
	Session string
	Covers  []store.Range
	Created int
	Revised int
	Refused int
}

// Item is one item file as the subject reads it.
type Item struct {
	ID, Path, Base                string
	Kind, State, Title, Statement string
}

// Edit is a change made by hand.
type Edit struct {
	Kind string // EditChange, EditLock, EditVerify or EditGarble
	Text string // the new statement, for EditChange
}

// The hand edits.
const (
	EditChange = "change" // the statement is rewritten
	EditLock   = "lock"   // a line locked: true  # note is added
	EditVerify = "verify" // a verified mark is added
	EditGarble = "garble" // the file is made unreadable
)

// Failure is a property that did not hold: which, on which seed, and at
// which step of the schedule.
type Failure struct {
	Property string
	Seed     int64
	Step     int
	Detail   string
}

func (f *Failure) Error() string {
	return fmt.Sprintf("%s fails on seed %d at step %d: %s", f.Property, f.Seed, f.Step, f.Detail)
}

// errCrash is the error a fault returns.
var errCrash = errors.New("conformance: a crash at a fault point")

// runner drives one subject through a schedule.
type runner struct {
	prop   string
	seed   int64
	s      Subject
	st     store.Store
	now    time.Time
	passes int
	step   int
	armed  *crash
	fired  bool
}

func newRunner(prop string, s Subject, seed int64) *runner {
	return &runner{prop: prop, seed: seed, s: s, now: time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)}
}

func (r *runner) fail(format string, a ...any) error {
	return &Failure{Property: r.prop, Seed: r.seed, Step: r.step, Detail: fmt.Sprintf(format, a...)}
}

func (r *runner) clock() time.Time { return r.now }

// fault crashes Apply at the armed step, once.
func (r *runner) fault(s store.Step, i int) error {
	if r.armed != nil && r.armed.step == s && r.armed.n == i {
		r.armed, r.fired = nil, true
		return errCrash
	}
	return nil
}

func (r *runner) open() error {
	st, err := r.s.Open(r.clock, r.fault)
	if err != nil {
		return r.fail("open: %v", err)
	}
	r.st = st
	return nil
}

func (r *runner) close() error {
	if r.st == nil {
		return nil
	}
	err := r.st.Close()
	r.st = nil
	if err != nil {
		return r.fail("close: %v", err)
	}
	return nil
}

func (r *runner) reopen() error {
	if err := r.close(); err != nil {
		return err
	}
	return r.open()
}

// passID mints a pass id: a ULID of the clock's time, after the clock has
// moved, and 80 bits from the seed and the count of passes.
func (r *runner) passID() string {
	r.now = r.now.Add(time.Duration(1+r.passes%7) * time.Second)
	r.passes++
	sum := sha256.Sum256(fmt.Appendf(nil, "%d/%d", r.seed, r.passes))
	return passAt(r.now, sum[:10])
}

// passAt is a ULID: 48 bits of milliseconds, then 80 random bits.
func passAt(t time.Time, random []byte) string {
	const alphabet = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"
	out := make([]byte, 26)
	ms := t.UnixMilli()
	for i := 9; i >= 0; i-- {
		out[i] = alphabet[ms&31]
		ms >>= 5
	}
	for i := range 16 {
		v := 0
		for j := range 5 {
			bit := i*5 + j
			v = v<<1 | int(random[bit/8]>>(7-bit%8)&1)
		}
		out[10+i] = alphabet[v]
	}
	return string(out)
}

// apply runs a pass. With a crash, it stops the pass there, reopens as a
// new process would, and runs it again, as the worker does.
func (r *runner) apply(txn store.Txn, c *crash) (store.Result, error) {
	if c != nil {
		r.armed, r.fired = c, false
		res, err := r.st.Apply(txn)
		r.armed = nil
		switch {
		case r.fired && err == nil:
			return res, r.fail("Apply went on past a fault at %s %d", c.step, c.n)
		case !r.fired && err != nil:
			return res, r.fail("Apply: %v", err)
		case !r.fired:
			return res, nil
		}
		if err := r.reopen(); err != nil {
			return res, err
		}
	}
	res, err := r.st.Apply(txn)
	if err != nil {
		return res, r.fail("Apply: %v", err)
	}
	if len(res.Outcomes) != len(txn.Ops) {
		return res, r.fail("Apply gave %d outcomes for %d operations", len(res.Outcomes), len(txn.Ops))
	}
	return res, nil
}

// byTitle finds an item by its title.
func byTitle(items []Item, title string) (Item, bool) {
	for _, it := range items {
		if it.Title == title {
			return it, true
		}
	}
	return Item{}, false
}

// derive is the fake derive: a pass over one chunk of the record, whose
// targets it finds by title.
func (r *runner) derive(c chunk) store.Txn {
	items := r.s.Items()
	txn := store.Txn{Pass: r.passID(), Kind: store.KindFast, Actor: "claude-code/model-a", Session: c.session}
	first := -1
	for i, f := range c.facts {
		op := store.Op{Authority: store.AuthorityModel, Evidence: []store.Evidence{{Resource: fmt.Sprintf("%s#%d", c.session, c.from+i), PV: "typed", Supports: "stated"}}}
		if f.user {
			op.Authority = store.AuthorityUser
		}
		switch f.do {
		case doCreate:
			op.Type, op.Fields = store.OpItemCreate, map[string]any{"kind": f.kind, "title": f.title, "statement": f.text}
		case doRevise, doState:
			it, ok := byTitle(items, f.title)
			if !ok {
				continue
			}
			op.Type, op.ID, op.Base, op.Fields = store.OpItemRevise, it.ID, it.Base, map[string]any{"statement": f.text}
			if f.do == doState {
				op.Type, op.Fields = store.OpItemState, map[string]any{"state": f.text}
			}
		case doNote:
			op.Type, op.Fields = store.OpSessionAppend, map[string]any{"text": f.text}
			if first >= 0 {
				op.Requires = []int{first}
			}
		}
		if op.Type != store.OpSessionAppend && first < 0 {
			first = len(txn.Ops)
		}
		txn.Ops = append(txn.Ops, op)
	}
	cover := store.Range{From: c.from, To: c.to, Outcome: "done", Contract: "1"}
	if len(txn.Ops) == 0 {
		cover.Outcome, cover.Reason = "skipped", "empty"
	}
	txn.Covers = []store.Range{cover}
	return txn
}

// pick is the item an edit or an acknowledgement aims at.
func (r *runner) pick(n int) (Item, bool) {
	items := r.s.Items()
	if len(items) == 0 {
		return Item{}, false
	}
	return items[n%len(items)], true
}

// handEdit makes an edit with the store closed, and reopens, so the store
// adopts it. It returns the path, or "" when there was no item.
func (r *runner) handEdit(ev event) (string, error) {
	it, ok := r.pick(ev.pick)
	if !ok {
		return "", nil
	}
	if err := r.close(); err != nil {
		return "", err
	}
	if err := r.s.HandEdit(it.Path, ev.edit); err != nil {
		return "", r.fail("hand edit: %v", err)
	}
	return it.Path, r.open()
}

// ackTxn is a person's acknowledgement of an item's hand edit.
func (r *runner) ackTxn(ev event) (store.Txn, Item, bool) {
	it, ok := r.pick(ev.pick)
	if !ok {
		return store.Txn{}, it, false
	}
	op := store.Op{Type: store.OpDocAcknowledge, ID: it.ID, Base: it.Base, Authority: store.AuthorityUser}
	return store.Txn{Pass: r.passID(), Kind: store.KindAcknowledge, Actor: "human:person", Ops: []store.Op{op}}, it, true
}

// restore loses the store folder and restores it.
func (r *runner) restore() error {
	rs, ok := r.s.(Restorable)
	if !ok {
		return r.fail("the subject cannot be restored")
	}
	if err := r.close(); err != nil {
		return err
	}
	if err := rs.Lose(); err != nil {
		return r.fail("lose: %v", err)
	}
	if err := rs.Restore(); err != nil {
		return r.fail("restore: %v", err)
	}
	return r.open()
}

// run plays one event the plain way: a pass with its crash and retry, a
// hand edit, an acknowledgement, a push or a loss.
func (r *runner) run(sch *schedule, ev event) error {
	switch ev.what {
	case evPass:
		_, err := r.apply(r.derive(sch.record[ev.chunk]), ev.crash)
		return err
	case evEdit:
		_, err := r.handEdit(ev)
		return err
	case evAck:
		if txn, _, ok := r.ackTxn(ev); ok {
			_, err := r.apply(txn, ev.crash)
			return err
		}
	case evPush:
		if err := r.s.(Restorable).Push(); err != nil {
			return r.fail("push: %v", err)
		}
	case evLose:
		return r.restore()
	}
	return nil
}

// snapshot is a subject's files and history.
type snapshot struct {
	files   map[string][]byte
	history []Commit
}

func (r *runner) snap() snapshot {
	return snapshot{files: r.s.Files(), history: r.s.History()}
}

func (a snapshot) equal(b snapshot) bool {
	return maps.EqualFunc(a.files, b.files, bytes.Equal) && reflect.DeepEqual(a.history, b.history)
}

// diff says how two snapshots differ, by path and count only.
func (a snapshot) diff(b snapshot) string {
	var paths []string
	for p := range a.files {
		if !bytes.Equal(a.files[p], b.files[p]) {
			paths = append(paths, p)
		}
	}
	for p := range b.files {
		if _, ok := a.files[p]; !ok {
			paths = append(paths, p)
		}
	}
	slices.Sort(paths)
	return fmt.Sprintf("files differ at %q; %d commits against %d", paths, len(a.history), len(b.history))
}
