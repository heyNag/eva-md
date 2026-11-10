// Package event is the record's event model: an event, its id and the forms
// that refer to it, its kind, its author, its exposure marks, its marked
// cuts, and a session with its class. internal/ledger/SPEC.md states what
// each value means and the evidence that sets it.
package event

import (
	"errors"
	"fmt"
	"slices"
	"strconv"
	"unicode/utf8"
)

// Kind is what an event is.
type Kind string

const (
	KindPrompt       Kind = "prompt"        // a prompt
	KindReply        Kind = "reply"         // the final assistant message of a turn
	KindSegment      Kind = "segment"       // assistant text between tool calls
	KindTool         Kind = "tool"          // a tool call
	KindDialog       Kind = "dialog"        // a question with options, and the answer
	KindPlanDecision Kind = "plan_decision" // a plan's text, approved or rejected
	KindTasks        Kind = "tasks"         // the agent's task list
	KindSubTask      Kind = "sub_task"      // what a subagent was asked
	KindSubReport    Kind = "sub_report"    // a subagent's final report
	KindSummary      Kind = "summary"       // the host's compaction summary
	KindDelivery     Kind = "delivery"      // what eva-md delivered into the chat
	KindControl      Kind = "control"       // a privacy control: Op says which
	KindCtx          Kind = "ctx"           // a change of context: Ctx holds it
	KindLife         Kind = "life"          // a host lifecycle event
	KindGap          Kind = "gap"           // something is missing here: Why says what
	KindGone         Kind = "gone"          // a tombstone left by forgetting
)

var kinds = []Kind{KindPrompt, KindReply, KindSegment, KindTool, KindDialog, KindPlanDecision, KindTasks, KindSubTask, KindSubReport, KindSummary, KindDelivery, KindControl, KindCtx, KindLife, KindGap, KindGone}

// Valid reports a known kind.
func (k Kind) Valid() bool { return slices.Contains(kinds, k) }

// Author is whose words an event's text is. An author is set only on
// positive evidence; without it the author is AuthorUnknown.
type Author string

const (
	AuthorTyped    Author = "typed"       // typed by the user
	AuthorPasted   Author = "pasted"      // wholly a paste the host marked
	AuthorChosen   Author = "chosen"      // chosen by the user from options the agent wrote
	AuthorAgent    Author = "agent"       // written by the agent
	AuthorSubagent Author = "subagent"    // written by a subagent
	AuthorHost     Author = "host"        // written by the host
	AuthorProgram  Author = "program"     // written by a program that runs the chat
	AuthorSpoken   Author = "spoken"      // spoken by the user in a voice call, never typed
	AuthorFront    Author = "front_model" // the voice front model's request or spoken lines
	AuthorTool     Author = "tool"        // a tool record
	AuthorEva      Author = "eva"         // a record eva-md itself wrote
	// AuthorV1User is a prompt v1 filed as the user's. v1 holds no evidence
	// that it was typed: it files voice hand-overs and some programs'
	// prompts the same way. Only the v1 reader sets it.
	AuthorV1User  Author = "v1_user"
	AuthorUnknown Author = "unknown"
)

var authors = []Author{AuthorTyped, AuthorPasted, AuthorChosen, AuthorAgent, AuthorSubagent, AuthorHost, AuthorProgram, AuthorSpoken, AuthorFront, AuthorTool, AuthorEva, AuthorV1User, AuthorUnknown}

// Valid reports a known author.
func (a Author) Valid() bool { return slices.Contains(authors, a) }

// Class is what kind of session a session is. Like an author, a class is
// set only on positive evidence.
type Class string

const (
	ClassChat         Class = "chat"         // a person's chat, voice-led threads included
	ClassSubagent     Class = "subagent"     // a subagent's thread
	ClassProgram      Class = "program"      // a program's run
	ClassUnrun        Class = "unrun"        // a thread that was opened and never run
	ClassHousekeeping Class = "housekeeping" // the host's own housekeeping
	ClassUnknown      Class = "unknown"
)

var classes = []Class{ClassChat, ClassSubagent, ClassProgram, ClassUnrun, ClassHousekeeping, ClassUnknown}

// Valid reports a known class.
func (c Class) Valid() bool { return slices.Contains(classes, c) }

// Mark is a note on an event that a consumer must heed.
type Mark string

const (
	// MarkRepaired: the line could not be decoded, and the text is the
	// fixed gap that v1's readers put in its place.
	MarkRepaired Mark = "repaired"
	// MarkWithheld: the reader returns nothing of the event but its place;
	// the event is a gap and Why says why.
	MarkWithheld Mark = "withheld"
	// MarkWrapper: the prompt begins with a voice hand-over wrapper, which
	// this reader does not decode. Its text is not one speaker's words.
	MarkWrapper Mark = "wrapper"
	// MarkName: the text is a fixed name eva-md wrote, not anyone's words.
	MarkName Mark = "name"
	// MarkRecovered: v1 recovered the event from the host's own record
	// rather than from a hook.
	MarkRecovered Mark = "recovered"
)

var marks = []Mark{MarkRepaired, MarkWithheld, MarkWrapper, MarkName, MarkRecovered}

// Exposure kinds: what outside text entered the turn before an event.
const (
	ExposureWeb      = "web"      // a web fetch or search
	ExposureNet      = "net"      // a shell command that reaches the network
	ExposureMCP      = "mcp"      // a tool of an MCP server other than eva-md's
	ExposureMemory   = "memory"   // something eva-md delivered or looked up
	ExposureSubagent = "subagent" // a subagent's report
)

var exposures = []string{ExposureWeb, ExposureNet, ExposureMCP, ExposureMemory, ExposureSubagent}

// Exposure marks that outside text entered the turn before an event, and
// the event of the tool call that brought it in, so text written before a
// fetch is told from text written after it.
type Exposure struct {
	Kind string `json:"k"`
	At   int64  `json:"at"` // the seq of the event that brought the text in
}

// Cut reasons.
const (
	CutSize     = "size"     // over the size limit
	CutDeadline = "deadline" // redaction ran out of time
	CutPolicy   = "policy"   // a policy keeps only part of it
	CutLegacy   = "legacy"   // v1 cut it and recorded only how much
)

var cutReasons = []string{CutSize, CutDeadline, CutPolicy, CutLegacy}

// Cut is one truncation of an event's text: At is the byte position in
// Text where Lost bytes were taken out, or -1 when the writer recorded only
// how much was lost (v1).
type Cut struct {
	At   int    `json:"at"`
	Lost int    `json:"lost"`
	Why  string `json:"why"`
}

// Span is where a redaction marker stands in an event's text: bytes Start
// to End of Text, what kind of value it replaced, its tier and an optional
// keyed handle. It is written as the array [start,end,kind,tier,handle].
type Span struct {
	Start, End int
	Kind, Tier string
	Handle     string
}

// Source is where an event came from: a hook (Hook), a row of the host's
// own record (Row, its id) and Key, the sameness key that makes two events
// one (see SPEC.md).
type Source struct {
	Hook bool   `json:"h,omitempty"`
	Row  string `json:"r,omitempty"`
	Key  string `json:"key"`
}

// Context is the context an event was written in. In a stored session
// only ctx events carry it; a reader hands every event out with it filled.
type Context struct {
	Project string `json:"project,omitempty"`
	Branch  string `json:"branch,omitempty"`
	Cwd     string `json:"cwd,omitempty"`
	Head    string `json:"head,omitempty"`
	Model   string `json:"model,omitempty"`
}

// Control operations.
const (
	OpOff    = "off"
	OpOn     = "on"
	OpForget = "forget"
)

// Why values of a gap the reader made in place of what it withheld.
const (
	WhyOffRecord   = "off_record"   // said off the record
	WhyUntrusted   = "untrusted"    // the line does not belong to its file
	WhyUnknownKind = "unknown_kind" // a line of a kind the reader does not know
	WhyUnconfirmed = "unconfirmed"  // v1's walker did not yield the line
)

var withheldReasons = []string{WhyOffRecord, WhyUntrusted, WhyUnknownKind, WhyUnconfirmed}

// Event is one event of a session. Its id is the session's host and id with
// Seq (see ID).
type Event struct {
	Seq      int64      `json:"seq"`             // the event's place in its session, from 1
	T        string     `json:"t,omitempty"`     // the source's own time, RFC 3339
	Turn     int        `json:"turn"`            // the turn's ordinal, from 1; 0 before the first prompt
	TID      string     `json:"tid,omitempty"`   // the host's turn id
	Kind     Kind       `json:"k"`               // what the event is
	Author   Author     `json:"pv"`              // whose words Text is
	Text     string     `json:"text,omitempty"`  // redacted text
	Red      []Span     `json:"red,omitempty"`   // where redaction markers stand in Text
	RV       int        `json:"rv,omitempty"`    // the redaction rule version; 0 when not known
	Cut      []Cut      `json:"cut,omitempty"`   // every truncation of Text
	Agent    string     `json:"agent,omitempty"` // the subagent's id
	Exposure []Exposure `json:"taint,omitempty"` // outside text that entered the turn before it
	Src      Source     `json:"src"`             // where it came from, and its sameness key
	Op       string     `json:"op,omitempty"`    // a control's operation
	Why      string     `json:"why,omitempty"`   // a gap's reason
	Marks    []Mark     `json:"mark,omitempty"`  // notes a consumer must heed
	Copy     int64      `json:"copy,omitempty"`  // the seq of the event this is a second copy of
	Ctx      *Context   `json:"ctx,omitempty"`   // its context (see Context)
}

// Has reports whether the event carries mark m.
func (e Event) Has(m Mark) bool { return slices.Contains(e.Marks, m) }

// Validate reports the first way e breaks the model's invariants.
func (e Event) Validate() error {
	switch {
	case e.Seq < 1:
		return errors.New("event: seq must be at least 1")
	case e.Turn < 0:
		return errors.New("event: negative turn")
	case !e.Kind.Valid():
		return fmt.Errorf("event: unknown kind %q", e.Kind)
	case !e.Author.Valid():
		return fmt.Errorf("event: unknown author %q", e.Author)
	case !utf8.ValidString(e.Text):
		return errors.New("event: text is not UTF-8")
	case e.RV < 0:
		return errors.New("event: negative rule version")
	case e.Src.Key == "":
		return errors.New("event: no sameness key")
	case e.Copy < 0 || e.Copy >= e.Seq:
		return errors.New("event: a copy names an earlier event")
	case (e.Kind == KindControl) != (e.Op != ""):
		return errors.New("event: a control, and only a control, has an operation")
	case e.Op != "" && e.Op != OpOff && e.Op != OpOn && e.Op != OpForget:
		return fmt.Errorf("event: unknown operation %q", e.Op)
	case e.Why != "" && e.Kind != KindGap:
		return errors.New("event: only a gap has a reason")
	}
	end := 0
	for _, s := range e.Red {
		if s.Start < end || s.End <= s.Start || s.End > len(e.Text) || s.Kind == "" || s.Tier == "" {
			return errors.New("event: redaction spans must be ordered, apart and inside the text")
		}
		end = s.End
	}
	for _, c := range e.Cut {
		if c.Lost < 1 || !slices.Contains(cutReasons, c.Why) || c.At < -1 || c.At > len(e.Text) || (c.At == -1) != (c.Why == CutLegacy) {
			return errors.New("event: a cut needs bytes lost, a known reason and a place in the text")
		}
	}
	for _, x := range e.Exposure {
		if !slices.Contains(exposures, x.Kind) || x.At < 1 || x.At >= e.Seq {
			return errors.New("event: an exposure needs a known kind and an earlier event")
		}
	}
	for i, m := range e.Marks {
		if !slices.Contains(marks, m) || slices.Contains(e.Marks[:i], m) {
			return fmt.Errorf("event: unknown or repeated mark %q", m)
		}
	}
	if e.Has(MarkWithheld) && (e.Kind != KindGap || !slices.Contains(withheldReasons, e.Why) || !slices.Equal(e.Marks, []Mark{MarkWithheld}) ||
		e.Ctx != nil || e.TID != "" || e.T != "" || !e.placeOnly()) {
		return errors.New("event: a withheld event is a bare gap with a reason, which holds nothing but its place")
	}
	if e.Kind == KindGone && !e.placeOnly() {
		return errors.New("event: a tombstone holds nothing of what was forgotten")
	}
	if e.Has(MarkWrapper) && (e.Kind != KindPrompt || e.Author == AuthorTyped || e.Author == AuthorSpoken) {
		return errors.New("event: an undecoded wrapper is a prompt of no single speaker")
	}
	return nil
}

// placeOnly reports an event that holds nothing of a text but its place:
// written by eva-md, with no text, rule version, cut, subagent, exposure,
// copy, hook or row, and its place as its sameness key (PlaceKey), so not
// even a key derived from a text it stands for.
func (e Event) placeOnly() bool {
	return e.Author == AuthorEva && e.Text == "" && len(e.Red) == 0 && e.RV == 0 && len(e.Cut) == 0 && e.Agent == "" &&
		len(e.Exposure) == 0 && e.Copy == 0 && !e.Src.Hook && e.Src.Row == "" && e.Src.Key == PlaceKey(e.Seq)
}

// PlaceKey is the sameness key of an event that holds nothing but its
// place, a withheld event or a tombstone: place:<seq>. It is never another
// event's key, so such an event is never a copy and never copied.
func PlaceKey(seq int64) string { return "place:" + strconv.FormatInt(seq, 10) }

// Session is one session, with its class and what reading it found.
type Session struct {
	Host     string `json:"host"`
	ID       string `json:"session"`
	Folder   string `json:"folder,omitempty"` // where the backend keeps it; v1: the project folder
	Class    Class  `json:"class"`
	Events   int    `json:"events"`         // events read
	Copies   int    `json:"copies"`         // events that are a second copy of an earlier one
	Withheld int    `json:"withheld"`       // events withheld, of any reason
	Wrappers int    `json:"wrappers"`       // prompts that are an undecoded voice hand-over
	Tail     int64  `json:"tail,omitempty"` // bytes of an unfinished last line left unread
}
