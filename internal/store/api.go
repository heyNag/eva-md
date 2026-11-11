// Package store is the interface of eva.md's memory store: one write path,
// Apply, that takes a pass's operations and makes at most one commit, as
// docs/spec/store-format.md and the store design's section 7 describe. This
// file holds types only. The reference model in internal/store/conformance
// implements them, and the store core is held to the same properties.
package store

// Txn is one pass: the operations it asks for and the ranges of the record
// it consumed. Its pass id makes it idempotent: a pass already committed
// returns its recorded outcomes and makes no commit.
type Txn struct {
	Pass    string  // the pass id, a ULID: the idempotency key
	Kind    string  // one of Kinds
	Actor   string  // <host>/<model>, human:<id> or process:eva-md
	Session string  // <host>:<session id> when the pass read a session
	Covers  []Range // ranges of Session's record this pass consumed
	Ops     []Op
}

// Op is one operation of a pass. Each gets its own outcome: a refusal of
// one leaves the others to go on, unless they require it.
type Op struct {
	Type      string         // one of the Op constants
	ID        string         // the target's id; "" on a create
	Base      string         // the hash of the target's bytes the operation was computed against; "" on a create
	Fields    map[string]any // what the operation sets
	Evidence  []Evidence
	Authority string // AuthorityModel or AuthorityUser
	Requires  []int  // indexes of earlier operations that must apply for this one to apply
}

// Evidence is one source an operation cites, as the format's sources.
type Evidence struct {
	Resource string // an event id of the record
	PV       string // the record's author of the cited event
	Supports string // the tag code computes for the span
	Hash     string // sha256: and 64 hex
}

// Outcome is what became of one operation.
type Outcome struct {
	Result string // ResultApplied, ResultNoop or ResultRefused
	Reason string // one of the Reason constants when refused; "" otherwise
	ID     string // the target's id; on a create, the id it was given
}

// Range is one range of a session's record a pass consumed, with the
// fields of the format's coverage objects.
type Range struct {
	From     int    `json:"from"`
	To       int    `json:"to"`
	Pass     string `json:"pass"`
	Outcome  string `json:"outcome"` // done, skipped or refused
	Contract string `json:"contract"`
	Reason   string `json:"reason,omitempty"` // unless done
}

// Result is what Apply returns: an outcome per operation, in order, and
// the commit the pass made, or "" when it made none.
type Result struct {
	Outcomes []Outcome
	Commit   string
}

// Store is the store as one process holds it, under its lock.
type Store interface {
	// Apply runs a pass: recover, plan, journal, write, stage, commit,
	// finish. An error means the pass may not have committed; a retry of
	// the same Txn is always safe.
	Apply(Txn) (Result, error)
	// Plan returns the outcomes Apply would give, and writes nothing.
	Plan(Txn) ([]Outcome, error)
	// Coverage is the union of the ranges the store's tree holds for the
	// session.
	Coverage(session string) ([]Range, error)
	// Close releases the lock and the open files, and nothing else.
	Close() error
}

// The kinds of pass, the format's section 8.
const (
	KindFast        = "fast"
	KindConsolidate = "consolidate"
	KindRemember    = "remember"
	KindCorrect     = "correct"
	KindConfirm     = "confirm"
	KindAcknowledge = "acknowledge"
	KindForget      = "forget"
	KindUndo        = "undo"
	KindMigrate     = "migrate"
	KindInit        = "init"
	KindHuman       = "human"
	KindMerge       = "merge"
)

// Kinds lists every kind of pass.
var Kinds = []string{KindFast, KindConsolidate, KindRemember, KindCorrect, KindConfirm, KindAcknowledge, KindForget, KindUndo, KindMigrate, KindInit, KindHuman, KindMerge}

// The types of operation.
const (
	OpItemCreate       = "item.create"
	OpItemRevise       = "item.revise"
	OpItemState        = "item.state"
	OpItemConfirm      = "item.confirm"
	OpItemRetract      = "item.retract"
	OpWorkstreamUpsert = "ws.upsert"
	OpSessionAppend    = "session.append"
	OpProjectUpsert    = "project.upsert"
	OpDocWrite         = "doc.write"
	OpDocAcknowledge   = "doc.acknowledge"
)

// Who an operation speaks for. Only the user's authority passes a verified
// mark or an unacknowledged hand edit; nothing passes a lock.
const (
	AuthorityModel = "model"
	AuthorityUser  = "user"
)

// The results of an operation.
const (
	ResultApplied = "applied"
	ResultNoop    = "noop"
	ResultRefused = "refused"
)

// The reasons an operation is refused.
const (
	ReasonLocked     = "locked"
	ReasonVerified   = "verified"
	ReasonUnreadable = "unreadable"
	ReasonHandEdit   = "hand_edit"
	ReasonConflict   = "conflict"
	ReasonSecret     = "secret"
	ReasonInvalid    = "invalid"
	ReasonUnknownID  = "unknown_id"
	ReasonRequires   = "requires"
)

// Step names a point in Apply where a fault can stop it.
type Step string

// Apply's steps, in order. StepWrite is reached once per file written,
// with the file's index.
const (
	StepRecover Step = "recover"
	StepPlan    Step = "plan"
	StepJournal Step = "journal"
	StepWrite   Step = "write"
	StepStage   Step = "stage"
	StepCommit  Step = "commit"
	StepFinish  Step = "finish"
)

// Steps lists Apply's steps in order.
var Steps = []Step{StepRecover, StepPlan, StepJournal, StepWrite, StepStage, StepCommit, StepFinish}

// Fault is called at each step of Apply, with the file's index at
// StepWrite and 0 elsewhere. An error stops Apply there, as a process's
// death would: nothing is rolled back or cleaned up, Apply returns the
// error, and Close then only releases the lock and the open files. The
// next open recovers.
type Fault func(Step, int) error
