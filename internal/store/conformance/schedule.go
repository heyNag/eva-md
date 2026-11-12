package conformance

import (
	"fmt"
	"math/rand/v2"

	"github.com/heyNag/eva-md/internal/store"
)

// What a fact of the record asks the derive for.
const (
	doCreate = "create"
	doRevise = "revise"
	doState  = "state"
	doNote   = "note"
)

// fact is one line of the made-up record that the fake derive turns into
// an operation.
type fact struct {
	do    string
	kind  string // for a create
	title string // the item's, for a create, a revision or a change of state
	text  string // the statement, the state, or a note's text
	user  bool   // the line carries the user's authority
}

// chunk is the range of one session's record one pass covers.
type chunk struct {
	session  string
	from, to int
	facts    []fact
}

// What an event of a schedule is.
const (
	evPass = "pass"
	evEdit = "edit"
	evAck  = "ack"
	evPush = "push"
	evLose = "lose"
)

type crash struct {
	step store.Step
	n    int
}

type event struct {
	what    string
	chunk   int    // a pass's chunk of the record
	crash   *crash // where a pass crashes before its retry
	pick    int    // the file an edit or an acknowledgement aims at
	session bool   // the edit aims at a session file, not an item
	edit    Edit
}

// schedule is a made-up record and what happens around it.
type schedule struct {
	record []chunk
	events []event
}

// options says which events a schedule may hold beside passes.
type options struct {
	edits  bool // hand edits and acknowledgements
	pushes bool
	loses  bool
}

var (
	sessions = []string{"claude-code:a1", "codex:b2", "claude-code:c3"}
	nouns    = []string{"cache", "parser", "release", "hook", "index", "journal", "remote", "lock"}
	verbs    = []string{"lives under bin", "runs on push", "waits for review", "skips temporary folders", "is rebuilt nightly", "stays local"}
	edits    = []string{EditChange, EditLock, EditVerify, EditGarble}
)

// newSchedule makes the schedule of a seed: one seed, one schedule.
func newSchedule(seed int64, o options) *schedule {
	rng := rand.New(rand.NewPCG(uint64(seed), 0x5eed))
	words := func(n int) string {
		return fmt.Sprintf("s%d the %s %s", n, nouns[rng.IntN(len(nouns))], verbs[rng.IntN(len(verbs))])
	}
	sch := &schedule{}
	next := map[string]int{}
	var titles []string
	n := 0
	chunks := 6 + rng.IntN(10)
	live := sessions[:2+rng.IntN(2)]
	for range chunks {
		s := live[rng.IntN(len(live))]
		c := chunk{session: s, from: next[s] + 1}
		used := map[string]bool{}
		var made []string
		for range 1 + rng.IntN(4) {
			n++
			f := fact{user: rng.IntN(4) == 0, text: words(n)}
			var title string
			for range 4 {
				if len(titles) == 0 {
					break
				}
				if t := titles[rng.IntN(len(titles))]; !used[t] {
					title = t
					break
				}
			}
			switch k := rng.IntN(10); {
			case title == "" || k < 4:
				f.do, f.kind, f.title = doCreate, kinds[rng.IntN(len(kinds))], fmt.Sprintf("t%03d", n)
				made = append(made, f.title)
			case k < 7:
				f.do, f.title = doRevise, title
			case k < 9:
				f.do, f.title, f.text = doState, title, []string{"active", "retired", "retracted"}[rng.IntN(3)]
			default:
				f.do = doNote
			}
			if f.title != "" {
				used[f.title] = true
			}
			c.facts = append(c.facts, f)
		}
		c.to = c.from + len(c.facts) - 1 + rng.IntN(2)
		next[s] = c.to
		titles = append(titles, made...)
		sch.record = append(sch.record, c)
	}
	for i := range sch.record {
		if o.edits && rng.IntN(3) == 0 {
			ev := event{what: evEdit, pick: rng.IntN(1000), edit: Edit{Kind: edits[rng.IntN(len(edits))], Text: words(1000 + i)}}
			ev.session = rng.IntN(3) == 0
			sch.events = append(sch.events, ev)
		}
		if o.edits && rng.IntN(4) == 0 {
			sch.events = append(sch.events, event{what: evAck, pick: rng.IntN(1000)})
		}
		if o.pushes && rng.IntN(3) == 0 {
			sch.events = append(sch.events, event{what: evPush})
		}
		if o.loses && rng.IntN(6) == 0 {
			sch.events = append(sch.events, event{what: evLose})
		}
		ev := event{what: evPass, chunk: i}
		if rng.IntN(2) == 0 {
			ev.crash = &crash{step: store.Steps[rng.IntN(len(store.Steps))]}
			if ev.crash.step == store.StepWrite {
				ev.crash.n = rng.IntN(3)
			}
		}
		sch.events = append(sch.events, ev)
	}
	return sch
}
