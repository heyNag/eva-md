package spike

import (
	"errors"
	"fmt"
	"slices"
	"strconv"
	"time"

	"github.com/heyNag/eva-md/internal/eval/scenario"
)

// The states of a stored item.
const (
	// StateActive: in force, and shown to the next pass.
	StateActive = "active"
	// StateRetired: retired, superseded, done or answered.
	StateRetired = "retired"
	// StateLog: a note, dated by its day.
	StateLog = "log"
)

// Source is an entry an item cites: its session's id and its number.
type Source struct {
	Session string
	N       int64
}

// Item is one record of the store: an item of one of the plan's five
// kinds, a next step (KindNext), an open question (KindQuestion) or a note
// (its kind did, found, failed or left, in StateLog).
type Item struct {
	Handle string
	Kind   string
	State  string
	Tag    string
	Text   string
	// Day is the day of its newest cited entry, YYYY-MM-DD in that entry's
	// own offset.
	Day     string
	Sources []Source
	// Replaces is the handle of the item it superseded, if any.
	Replaces string
	// Ended is the text of the operation that retired it, marked it done
	// or answered it, "" when it gave none or it was superseded; EndedBy
	// are the entries that operation cited.
	Ended   string
	EndedBy []Source
}

// Store is the spike's store across the passes of one script: it applies
// the operations each pass accepted, in order, and mints handles h1, h2,
// and so on. Its zero value is empty and ready.
type Store struct {
	items []Item
}

// Shown is what the next pass is shown: the active items, next steps and
// open questions, by handle.
func (st *Store) Shown() []Shown {
	var out []Shown
	for _, it := range st.items {
		if it.State == StateActive {
			out = append(out, Shown{Handle: it.Handle, Kind: it.Kind, Tag: it.Tag, Text: it.Text})
		}
	}
	return out
}

// Items is every record of the store in the order it was minted, each a
// copy.
func (st *Store) Items() []Item {
	out := make([]Item, len(st.items))
	for i, it := range st.items {
		it.Sources, it.EndedBy = slices.Clone(it.Sources), slices.Clone(it.EndedBy)
		out[i] = it
	}
	return out
}

// Apply applies the accepted operations of v, a verdict on a pass of
// session s judged against Shown, in the answer's order. It changes
// nothing and returns an error when an accepted operation does not fit:
// it cites an entry s does not have, or names a handle that is not active
// or not of its sort.
func (st *Store) Apply(s scenario.Session, v Verdict) error {
	events, err := scenario.Events(s)
	if err != nil {
		return err
	}
	at := map[int64]time.Time{}
	for _, e := range events {
		t, err := time.Parse(time.RFC3339Nano, e.T)
		if err != nil {
			return fmt.Errorf("spike: event %d has no time: %w", e.Seq, err)
		}
		at[e.Seq] = t
	}
	next := slices.Clone(st.items)
	find := func(handle string) int {
		return slices.IndexFunc(next, func(it Item) bool { return it.Handle == handle && it.State == StateActive })
	}
	for i, j := range v.Ops {
		if !j.Accepted() {
			continue
		}
		op := j.Op
		sources, day, err := cited(s.ID, j.Spans, at)
		if err != nil {
			return fmt.Errorf("spike: operation %d: %w", i+1, err)
		}
		mint := func(kind, state string) Item {
			return Item{Handle: "h" + strconv.Itoa(len(next)+1), Kind: kind, State: state, Tag: j.Tag, Text: text(op), Day: day, Sources: sources}
		}
		if needsKind := op.Op == "item.add" || op.Op == "item.supersede" || op.Op == "note"; needsKind && op.Kind == nil {
			return fmt.Errorf("spike: operation %d has no kind", i+1)
		}
		if needsID(op.Op) != (op.ID != nil) {
			return fmt.Errorf("spike: operation %d names a handle where its op takes none, or none where it takes one", i+1)
		}
		t := -1
		if op.ID != nil {
			if t = find(*op.ID); t < 0 || !fits(op.Op, next[t].Kind) {
				return fmt.Errorf("spike: operation %d names %s, which is not an active target of its sort", i+1, *op.ID)
			}
		}
		switch op.Op {
		case "item.add":
			next = append(next, mint(*op.Kind, StateActive))
		case "item.supersede":
			it := mint(*op.Kind, StateActive)
			it.Replaces = next[t].Handle
			next[t].State, next[t].EndedBy = StateRetired, sources
			next = append(next, it)
		case "item.retire", "next.done", "ask.answer":
			next[t].State, next[t].Ended, next[t].EndedBy = StateRetired, text(op), sources
		case "next.add":
			next = append(next, mint(KindNext, StateActive))
		case "ask.add":
			next = append(next, mint(KindQuestion, StateActive))
		case "note":
			next = append(next, mint(*op.Kind, StateLog))
		default:
			return fmt.Errorf("spike: operation %d is an unknown %q", i+1, op.Op)
		}
	}
	st.items = next
	return nil
}

// fits reports whether an operation op may change a record of kind kind.
func fits(op, kind string) bool {
	switch op {
	case "next.done":
		return kind == KindNext
	case "ask.answer":
		return kind == KindQuestion
	case "item.supersede", "item.retire":
		return slices.Contains(itemKinds, kind)
	}
	return false
}

// text is op's text, or "".
func text(op Op) string {
	if op.Text == nil {
		return ""
	}
	return *op.Text
}

// cited lists the entries spans cite, once each in the order cited, and
// the day of the newest, in its own offset.
func cited(session string, spans []Cited, at map[int64]time.Time) ([]Source, string, error) {
	var out []Source
	var newest time.Time
	for _, sp := range spans {
		t, ok := at[sp.E]
		if sp.Session != session || !ok {
			return nil, "", fmt.Errorf("it cites entry %d of session %q, which the pass's session does not have", sp.E, sp.Session)
		}
		if src := (Source{Session: session, N: sp.E}); !slices.Contains(out, src) {
			out = append(out, src)
		}
		if t.After(newest) {
			newest = t
		}
	}
	if len(out) == 0 {
		return nil, "", errors.New("it cites no entry")
	}
	return out, newest.Format(time.DateOnly), nil
}
