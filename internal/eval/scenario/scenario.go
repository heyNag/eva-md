// Package scenario is the fidelity instrument's scenario model: scripted
// chats whose turns plant facts, and for each fact which values must
// survive, where a correct store may file it, under whose authority, and at
// which turn of which session it was said. It renders a session as the
// inbox entries v1's hooks would store and as the record's events, finds
// the turn behind a line a store cites, and lists the planted words no
// result row may hold. v1's 12 accuracy scripts are its first instances
// (V1). No release contains it.
package scenario

import (
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/heyNag/eva-md/internal/ledger/event"
)

// Split says whether a scenario may be tuned on.
type Split string

const (
	SplitTuning  Split = "tuning"
	SplitHeldout Split = "heldout"
)

// Kind is a place a correct store may file a fact: one of the store's five
// kinds, a workstream's working items, or a dated line.
type Kind string

const (
	KindFact       Kind = "fact"
	KindDecision   Kind = "decision"
	KindConstraint Kind = "constraint"
	KindProcedure  Kind = "procedure"
	KindRule       Kind = "rule"
	KindNext       Kind = "next"
	KindOpen       Kind = "open"
	KindLog        Kind = "log"
)

var kinds = []Kind{KindFact, KindDecision, KindConstraint, KindProcedure, KindRule, KindNext, KindOpen, KindLog}

// Authority is whose authority a stored fact must carry.
type Authority string

const (
	// AuthorityUser: the user's, stated or accepted.
	AuthorityUser Authority = "user"
	// AuthorityOther: anyone's but the user's.
	AuthorityOther Authority = "other"
)

// Must is what a store must do with a fact.
type Must string

const (
	MustStore Must = "store"
	// MustNotStore: it must not be kept at all, as a fact said off the
	// record.
	MustNotStore Must = "not_store"
	// MustNotWithAuthority: it may be kept, never under the user's
	// authority, as pasted text posing as a decision.
	MustNotWithAuthority Must = "not_with_authority"
	// MustNotAsRule: it may be kept, never as a rule or a constraint.
	MustNotAsRule Must = "not_as_rule"
)

// SlotClass is the class of a value that must survive, which says how a
// scorer compares it.
type SlotClass string

const (
	SlotInt      SlotClass = "int"
	SlotQuantity SlotClass = "quantity" // a number with its unit
	SlotDate     SlotClass = "date"
	SlotPath     SlotClass = "path"
	SlotIdent    SlotClass = "ident"
	SlotVersion  SlotClass = "version"
	SlotPhrase   SlotClass = "phrase"
	SlotLimit    SlotClass = "limit"
	SlotCond     SlotClass = "cond"
)

var slotClasses = []SlotClass{SlotInt, SlotQuantity, SlotDate, SlotPath, SlotIdent, SlotVersion, SlotPhrase, SlotLimit, SlotCond}

// Scenario is one scripted history.
type Scenario struct {
	ID       string    `json:"id"` // a results id, as a row's scenario
	Family   string    `json:"family"`
	Name     string    `json:"name"` // what it plants, in a few words
	Split    Split     `json:"split"`
	Seed     uint64    `json:"seed"`
	Sessions []Session `json:"sessions"`
	Facts    []Fact    `json:"facts"`
	// Passes is the order in which v1's update passes take its sessions.
	Passes []Pass `json:"passes"`
}

// Session is one chat of a scenario.
type Session struct {
	Host   string `json:"host"`
	ID     string `json:"session"`
	Branch string `json:"branch"`
	Turns  []Turn `json:"turns"`
}

// Turn is one thing said or done in a session. Going off and on the
// record is a control turn, and a prompt a program sent in the user's
// place is a prompt by event.AuthorProgram.
type Turn struct {
	At     time.Time    `json:"at"`
	Author event.Author `json:"author"`
	Kind   event.Kind   `json:"kind"`
	Op     string       `json:"op,omitempty"`    // a control's operation: event.OpOff or event.OpOn
	Agent  string       `json:"agent,omitempty"` // the subagent's id
	Text   string       `json:"text,omitempty"`
	Plants []string     `json:"plants,omitempty"` // the ids of the facts it plants
}

// Fact is one planted fact.
type Fact struct {
	ID string `json:"id"`
	// Kinds are the places a correct store may file it; any one counts.
	Kinds     []Kind    `json:"kinds"`
	Authority Authority `json:"authority"`
	// Slots are the values that must survive.
	Slots []Slot `json:"slots"`
	// LogDate is the day, YYYY-MM-DD, under which it stands when it is filed
	// as a dated line; set exactly when Kinds holds KindLog.
	LogDate string `json:"log_date,omitempty"`
	// Until is the turn that ends it, if any.
	Until *Ref `json:"until,omitempty"`
	// Supersedes is the id of the fact it replaces, if any.
	Supersedes string `json:"supersedes,omitempty"`
	Must       Must   `json:"must"`
}

// Slot is one value that must survive: its class, the value as said, and
// any other forms a store may give it.
type Slot struct {
	Class SlotClass `json:"class"`
	Value string    `json:"value"`
	Forms []string  `json:"forms,omitempty"`
}

// Ref names a turn: its session's full id and its seq, from 1.
type Ref struct {
	Session string `json:"session"`
	Seq     int64  `json:"seq"`
}

// Pass is one of v1's update passes: the session it takes, by its place in
// Sessions, and whether every entry of it predates the page, so the
// out-of-order guard keeps the Now.
type Pass struct {
	Session int  `json:"session"`
	Kept    bool `json:"kept,omitempty"`
}

// idPattern is a results id, as internal/eval/results checks a row's.
var idPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}$`)

// Validate reports the first way sc breaks the model.
func (sc Scenario) Validate() error {
	switch {
	case !idPattern.MatchString(sc.ID):
		return errors.New("scenario: the id is not a results id")
	case !idPattern.MatchString(sc.Family):
		return errors.New("scenario: the family is not an id")
	case sc.Split != SplitTuning && sc.Split != SplitHeldout:
		return fmt.Errorf("scenario %s: unknown split %q", sc.ID, sc.Split)
	case len(sc.Sessions) == 0:
		return fmt.Errorf("scenario %s: no session", sc.ID)
	case len(sc.Passes) == 0:
		return fmt.Errorf("scenario %s: no pass", sc.ID)
	}
	facts := map[string]int{}
	for i, f := range sc.Facts {
		if f.ID == "" {
			return fmt.Errorf("scenario %s: fact %d has no id", sc.ID, i+1)
		}
		if _, ok := facts[f.ID]; ok {
			return fmt.Errorf("scenario %s: fact %s is given twice", sc.ID, f.ID)
		}
		facts[f.ID] = i
	}
	planted := map[string]Turn{}
	sessions, prefixes := map[string]bool{}, map[string]bool{}
	for _, s := range sc.Sessions {
		switch {
		case s.Host != "claude-code" && s.Host != "codex":
			return fmt.Errorf("scenario %s: unknown host %q", sc.ID, s.Host)
		case !event.ValidSession(s.ID) || sessions[s.ID] || prefixes[event.Prefix(s.ID)]:
			return fmt.Errorf("scenario %s: a session id is invalid, or its first 8 characters are another's", sc.ID)
		case s.Branch == "":
			return fmt.Errorf("scenario %s: session %s has no branch", sc.ID, s.ID)
		case len(s.Turns) == 0:
			return fmt.Errorf("scenario %s: session %s has no turn", sc.ID, s.ID)
		}
		sessions[s.ID], prefixes[event.Prefix(s.ID)] = true, true
		for n, t := range s.Turns {
			if err := t.check(); err != nil {
				return fmt.Errorf("scenario %s: %s#%d: %w", sc.ID, s.ID, n+1, err)
			}
			for _, id := range t.Plants {
				if _, ok := facts[id]; !ok {
					return fmt.Errorf("scenario %s: %s#%d plants no fact %s", sc.ID, s.ID, n+1, id)
				}
				if _, ok := planted[id]; ok {
					return fmt.Errorf("scenario %s: fact %s is planted twice", sc.ID, id)
				}
				planted[id] = t
			}
		}
	}
	for _, f := range sc.Facts {
		at, ok := planted[f.ID]
		if !ok {
			return fmt.Errorf("scenario %s: fact %s is planted by no turn", sc.ID, f.ID)
		}
		if err := sc.checkFact(f, at, facts); err != nil {
			return fmt.Errorf("scenario %s: fact %s: %w", sc.ID, f.ID, err)
		}
	}
	for _, p := range sc.Passes {
		if p.Session < 0 || p.Session >= len(sc.Sessions) {
			return fmt.Errorf("scenario %s: a pass takes no session", sc.ID)
		}
	}
	return nil
}

// check reports what is wrong with a turn.
func (t Turn) check() error {
	switch {
	case t.At.IsZero():
		return errors.New("no time")
	case !t.Author.Valid() || t.Author == event.AuthorUnknown:
		return fmt.Errorf("unknown author %q", t.Author)
	case !t.Kind.Valid():
		return fmt.Errorf("unknown kind %q", t.Kind)
	case (t.Kind == event.KindControl) != (t.Op == event.OpOff || t.Op == event.OpOn):
		return errors.New("a control, and only a control, goes off or on the record")
	}
	return nil
}

// checkFact reports what is wrong with fact f, which turn at plants.
func (sc Scenario) checkFact(f Fact, at Turn, facts map[string]int) error {
	if len(f.Kinds) == 0 {
		return errors.New("no kind")
	}
	for i, k := range f.Kinds {
		if !slices.Contains(kinds, k) || slices.Contains(f.Kinds[:i], k) {
			return fmt.Errorf("unknown or repeated kind %q", k)
		}
	}
	switch f.Must {
	case MustStore, MustNotStore, MustNotWithAuthority, MustNotAsRule:
	default:
		return fmt.Errorf("unknown must %q", f.Must)
	}
	if f.Authority != AuthorityUser && f.Authority != AuthorityOther {
		return fmt.Errorf("unknown authority %q", f.Authority)
	}
	if len(f.Slots) == 0 {
		return errors.New("no slot")
	}
	for _, s := range f.Slots {
		if !slices.Contains(slotClasses, s.Class) || strings.TrimSpace(s.Value) == "" || slices.Contains(s.Forms, "") {
			return errors.New("a slot needs a known class and a value")
		}
	}
	if !slices.ContainsFunc(f.Slots, func(s Slot) bool { return forbiddable(s.Value) }) {
		return errors.New("no slot holds a letter from g to z, so it has no forbidden word")
	}
	if slices.Contains(f.Kinds, KindLog) != (f.LogDate != "") {
		return errors.New("a log date is set exactly when the fact may be a dated line")
	}
	if f.LogDate != "" {
		if _, err := time.Parse(time.DateOnly, f.LogDate); err != nil {
			return errors.New("the log date is not a day")
		}
	}
	if f.Until != nil {
		end, err := sc.turn(*f.Until)
		if err != nil {
			return fmt.Errorf("until: %w", err)
		}
		if !end.At.After(at.At) {
			return errors.New("it ends no later than it is said")
		}
	}
	if f.Supersedes != "" {
		if _, ok := facts[f.Supersedes]; !ok || f.Supersedes == f.ID {
			return errors.New("it supersedes no other fact")
		}
	}
	return nil
}

// turn is the turn r names.
func (sc Scenario) turn(r Ref) (Turn, error) {
	for _, s := range sc.Sessions {
		if s.ID == r.Session {
			if r.Seq < 1 || r.Seq > int64(len(s.Turns)) {
				return Turn{}, errors.New("no such turn")
			}
			return s.Turns[r.Seq-1], nil
		}
	}
	return Turn{}, errors.New("no such session")
}

// ErrNoTurn is Locate's error for a line that renders no turn: v1's filler
// exchange at the end of each session.
var ErrNoTurn = errors.New("scenario: the line is the filler, which renders no turn")

// Locate finds the turn behind seq of a session, as a v1 reference
// s:<sid8>#<line> or a v2 source names it: session is the session's full id
// or its first 8 characters (event.Prefix). It returns the turn and the
// facts it plants, in the order the turn names them. A seq of the filler
// exchange returns ErrNoTurn.
func (sc Scenario) Locate(session string, seq int64) (Turn, []Fact, error) {
	var found []Session
	for _, s := range sc.Sessions {
		if s.ID == session {
			found = []Session{s}
			break
		}
		if event.Prefix(s.ID) == session {
			found = append(found, s)
		}
	}
	if len(found) != 1 {
		return Turn{}, nil, fmt.Errorf("scenario %s: the reference names %d sessions, not one", sc.ID, len(found))
	}
	s := found[0]
	switch n := int64(len(s.Turns)); {
	case seq >= 1 && seq <= n:
	case seq > n && seq <= n+int64(len(fillerExchange)):
		return Turn{}, nil, ErrNoTurn
	default:
		return Turn{}, nil, fmt.Errorf("scenario %s: session %s has no line %d", sc.ID, event.Prefix(s.ID), seq)
	}
	t := s.Turns[seq-1]
	t.Plants = slices.Clone(t.Plants)
	var facts []Fact
	for _, id := range t.Plants {
		for _, f := range sc.Facts {
			if f.ID == id {
				facts = append(facts, f)
			}
		}
	}
	return t, facts, nil
}

// Forbid lists every planted word of sc, for results.Store's Forbid: each
// slot's value as said that holds a letter from g to z, once, in the order
// of the facts. A value with no such letter, a bare number or a date in
// digits, is left out, since a row's commit, a hex string, could hold it.
// Validate makes sure every fact keeps at least one word in the list.
func Forbid(sc Scenario) []string {
	var words []string
	for _, f := range sc.Facts {
		for _, s := range f.Slots {
			if forbiddable(s.Value) && !slices.Contains(words, s.Value) {
				words = append(words, s.Value)
			}
		}
	}
	return words
}

// forbiddable reports a value that holds a letter from g to z, in either
// case, so it is never mistaken for a hex string.
func forbiddable(v string) bool {
	return strings.ContainsFunc(strings.ToLower(v), func(r rune) bool { return r >= 'g' && r <= 'z' })
}
