// Package inboxv1 is a read-only ledger backend over v1's inbox files. Each
// physical line of a session file is one event whose seq is the line's
// number, so every reference v1 wrote, s:<session>#<line>, names the event
// with that seq. It decodes lines with v1's own reader and honours v1's
// trust rules and off-the-record ranges through v1's own walker and trust
// check (internal/inbox), and never writes.
package inboxv1

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/heyNag/eva-md/internal/inbox"
	"github.com/heyNag/eva-md/internal/ledger"
	"github.com/heyNag/eva-md/internal/ledger/event"
	"github.com/heyNag/eva-md/internal/paths"
)

// Wrapper is how a voice hand-over begins. A prompt whose text begins with
// it, after white space, is marked as a wrapper and not decoded here.
const Wrapper = "<realtime_delegation>"

// repaired is the entry v1's reader puts in place of a line it cannot
// decode (inbox.Store.ReadFrom).
var repaired = inbox.Entry{V: 1, Role: "event", Kind: "event", Text: "gap: malformed inbox record", Origin: "hook"}

// Backend reads the v1 inbox at Root, normally paths.Dirs.Inbox().
type Backend struct{ Root string }

var _ ledger.Reader = Backend{}

// Sessions lists every session file of the inbox, in each project folder.
// A session v1 wrote into two project folders is two sessions with the
// same id, one for each folder.
func (b Backend) Sessions(ctx context.Context) ([]ledger.Key, error) {
	store := inbox.Store{Root: b.Root}
	captures, unread, err := store.Captures()
	if err != nil {
		return nil, err
	}
	if len(unread) > 0 {
		return nil, fmt.Errorf("inboxv1: %d project folders could not be read", len(unread))
	}
	var keys []ledger.Key
	for _, c := range captures {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if c.File {
			keys = append(keys, ledger.Key{Host: c.Host, Session: store.SessionID(c), Folder: c.Project, File: c.Name()})
		}
	}
	return keys, nil
}

// Read reads one session file. Its events are its complete lines, in
// order; an unfinished last line is left unread, as v1's readers leave it,
// and counted in the session's Tail.
func (b Backend) Read(ctx context.Context, k ledger.Key) (event.Session, []event.Event, error) {
	if !local(k.Folder) || !local(k.File) || !strings.HasSuffix(k.File, ".jsonl") {
		return event.Session{}, nil, errors.New("inboxv1: invalid session location")
	}
	store := inbox.Store{Root: b.Root}
	entries, end, err := store.ReadFrom(filepath.Join(b.Root, k.Folder, k.File), 0)
	if err != nil {
		return event.Session{}, nil, err
	}
	walked, size, err := b.walk(ctx, k, entries, end)
	if err != nil {
		return event.Session{}, nil, err
	}
	r := reading{key: k, keys: map[string]int64{}}
	r.session = event.Session{Host: k.Host, ID: k.Session, Folder: k.Folder, Tail: max(0, size-end)}
	for i, e := range entries {
		if i%1024 == 0 {
			if err := ctx.Err(); err != nil {
				return event.Session{}, nil, err
			}
		}
		ev := r.line(int64(i+1), e, walked[i])
		if err := ev.Validate(); err != nil {
			return event.Session{}, nil, fmt.Errorf("inboxv1: line %d: %w", i+1, err)
		}
		r.events = append(r.events, ev)
	}
	r.session.Events = len(r.events)
	r.session.Class = r.class()
	return r.session, r.events, nil
}

// walk runs v1's walker over the lines entries holds, the first end bytes
// of the file, and reports for each line whether the walker yielded it
// with the entry v1's reader decoded for it. It also returns the file's
// size.
func (b Backend) walk(ctx context.Context, k ledger.Key, entries []inbox.Entry, end int64) ([]bool, int64, error) {
	root, err := paths.OpenRoot(filepath.Join(b.Root, k.Folder), false)
	if err != nil {
		return nil, 0, err
	}
	defer root.Close()
	f, info, err := inbox.OpenSession(root, k.File)
	if err != nil {
		return nil, 0, err
	}
	defer f.Close()
	walked := make([]bool, len(entries))
	if end == 0 {
		return walked, info.Size(), nil
	}
	pos, err := inbox.Walk(ctx, f, k.Folder, k.File, inbox.Position{}, end, func(l inbox.Line) bool {
		if l.Line >= 1 && l.Line <= len(entries) {
			walked[l.Line-1] = entries[l.Line-1] == l.Entry
		}
		return true
	})
	if err != nil {
		return nil, 0, err
	}
	if pos.Offset != end || pos.Line != len(entries) {
		return nil, 0, errors.New("inboxv1: the session file changed while it was read")
	}
	return walked, info.Size(), nil
}

// reading is the state of one session's read.
type reading struct {
	key     ledger.Key
	session event.Session
	events  []event.Event
	keys    map[string]int64 // sameness key to the first event that has it
	turn    int
	// walkOff and anyOff are the off-the-record state as v1's walker
	// tracks it, from markers that belong to the file, and as v1's update
	// tracks it, from any marker line. A line is off the record when
	// either is set.
	walkOff, anyOff bool
	// evidence for the session's class, from the entries returned
	visible, withAgent, userPrompts int
	turns, start, end               bool
	firstPromptSeen, programStart   bool
}

// line makes the event of physical line seq, whose entry as v1's reader
// decoded it is e; walked says whether v1's walker yielded it.
func (r *reading) line(seq int64, e inbox.Entry, walked bool) event.Event {
	trusted := inbox.Trusted(e, r.key.Folder, r.key.File, "")
	if !trusted && e == repaired {
		return event.Event{Seq: seq, Turn: r.turn, Kind: event.KindGap, Author: event.AuthorEva, Text: e.Text,
			Src: event.Source{Key: positionKey(seq)}, Marks: []event.Mark{event.MarkRepaired, event.MarkName}}
	}
	marker := e.Kind == "event" && (e.Text == "off_record" || e.Text == "on_record")
	if marker {
		r.anyOff = e.Text == "off_record"
	}
	if !trusted {
		return r.withhold(seq, event.WhyUntrusted)
	}
	if marker {
		r.walkOff = e.Text == "off_record"
	} else if r.walkOff || r.anyOff {
		return r.withhold(seq, event.WhyOffRecord)
	}
	kind, author, marks, ok := classify(e)
	if !ok {
		return r.withhold(seq, event.WhyUnknownKind)
	}
	content := e.Kind == "prompt" || e.Kind == "reply" || e.Kind == "tool" || e.Kind == "plan"
	if content && !walked {
		return r.withhold(seq, event.WhyUnconfirmed)
	}
	ev := event.Event{Seq: seq, T: e.TS, TID: e.Turn, Kind: kind, Author: author, Text: e.Text, Agent: e.Agent, Marks: marks,
		Ctx: &event.Context{Project: e.Project, Branch: e.Branch, Cwd: e.Cwd, Model: e.Model}}
	if marker {
		ev.Op = map[string]string{"off_record": event.OpOff, "on_record": event.OpOn}[e.Text]
	}
	switch e.Origin {
	case "hook":
		ev.Src.Hook = true
	case "catchup":
		ev.Marks = append(ev.Marks, event.MarkRecovered)
	}
	if e.Trunc > 0 {
		ev.Cut = []event.Cut{{At: -1, Lost: e.Trunc, Why: event.CutLegacy}}
	}
	ev.Src.Key = sameness(seq, e, ev)
	if first, ok := r.keys[ev.Src.Key]; ok {
		ev.Copy = first
		r.session.Copies++
	} else {
		r.keys[ev.Src.Key] = seq
	}
	if kind == event.KindPrompt && ev.Copy == 0 {
		r.turn++
	}
	ev.Turn = r.turn
	if ev.Has(event.MarkWrapper) {
		r.session.Wrappers++
	}
	r.note(e)
	return ev
}

// withhold makes the bare gap that stands in the place of a line the reader
// returns nothing of.
func (r *reading) withhold(seq int64, why string) event.Event {
	r.session.Withheld++
	return event.Event{Seq: seq, Turn: r.turn, Kind: event.KindGap, Author: event.AuthorEva, Why: why,
		Src: event.Source{Key: positionKey(seq)}, Marks: []event.Mark{event.MarkWithheld}}
}

// classify maps a v1 entry to its kind, author and marks. Only the pairs
// of role and kind v1 writes are known.
func classify(e inbox.Entry) (event.Kind, event.Author, []event.Mark, bool) {
	sub := func(a event.Author) event.Author {
		if e.Agent != "" {
			return event.AuthorSubagent
		}
		return a
	}
	switch e.Role + "/" + e.Kind {
	case "user/prompt":
		if strings.HasPrefix(strings.TrimLeft(e.Text, " \t\r\n"), Wrapper) {
			return event.KindPrompt, sub(event.AuthorV1User), []event.Mark{event.MarkWrapper}, true
		}
		return event.KindPrompt, sub(event.AuthorV1User), nil, true
	case "assistant/reply":
		return event.KindReply, sub(event.AuthorAgent), nil, true
	case "tool/tool":
		return event.KindTool, sub(event.AuthorTool), nil, true
	case "plan/plan":
		return event.KindTasks, sub(event.AuthorAgent), nil, true
	case "host/host_summary":
		return event.KindSummary, event.AuthorHost, nil, true
	case "event/event":
	default:
		return "", "", nil, false
	}
	name := []event.Mark{event.MarkName}
	switch {
	case e.Text == "host_prompt":
		return event.KindPrompt, sub(event.AuthorProgram), name, true
	case e.Text == "off_record" || e.Text == "on_record":
		return event.KindControl, event.AuthorEva, name, true
	case strings.HasPrefix(e.Text, "gap"):
		return event.KindGap, event.AuthorEva, name, true
	case lifeEvent(e.Text):
		return event.KindLife, event.AuthorHost, name, true
	}
	return event.KindLife, event.AuthorUnknown, name, true
}

// lifeEvent reports the fixed names v1's hook writes for the host's
// lifecycle events.
func lifeEvent(text string) bool {
	name, _, _ := strings.Cut(text, ":")
	switch name {
	case "session_start", "session_end", "compact", "model_switch", "stop_failure", "interrupt":
		return true
	}
	return false
}

// sameness is an event's sameness key. v1 keeps no host row id and no tool
// call id, so a prompt or a reply with a turn id is keyed by the turn id,
// its kind, its subagent and its text: the hook and v1's recovery store
// the same prompt or reply that way, and both copies carry that turn. Every
// other line is keyed by its position, so it is never a copy: the same
// command run twice in one turn is two events.
func sameness(seq int64, e inbox.Entry, ev event.Event) string {
	if e.Turn == "" || ev.Has(event.MarkName) || (ev.Kind != event.KindPrompt && ev.Kind != event.KindReply) {
		return positionKey(seq)
	}
	sum := sha256.Sum256([]byte(e.Turn + "\x00" + string(ev.Kind) + "\x00" + e.Agent + "\x00" + e.Text))
	return "v1:turn:" + hex.EncodeToString(sum[:16])
}

func positionKey(seq int64) string { return "v1:line:" + strconv.FormatInt(seq, 10) }

// note gathers the evidence for the session's class from an entry the
// reader returns.
func (r *reading) note(e inbox.Entry) {
	r.visible++
	if e.Agent != "" {
		r.withAgent++
	}
	switch {
	case e.Kind == "event" && (e.Text == "session_start" || strings.HasPrefix(e.Text, "session_start:")):
		r.start = true
	case e.Kind == "event" && e.Text == "session_end":
		r.end = true
	}
	if turnEvidence(e) {
		r.turns = true
	}
	prompt := e.Role == "user" && e.Kind == "prompt"
	if prompt && e.Agent == "" {
		r.userPrompts++
	}
	if !r.firstPromptSeen && e.Agent == "" && (prompt || e.Kind == "event" && e.Text == "host_prompt") {
		r.firstPromptSeen, r.programStart = true, !prompt
	}
}

// turnEvidence reports an entry that shows a turn ran: a turn id, a
// prompt, reply, tool, plan or summary line, a host prompt, a record
// control, a missing reply, an interrupt or a failed stop.
func turnEvidence(e inbox.Entry) bool {
	if e.Turn != "" || e.Kind != "event" {
		return true
	}
	switch {
	case e.Text == "host_prompt", e.Text == "off_record", e.Text == "on_record", e.Text == "interrupt",
		e.Text == "gap: missing final reply", strings.HasPrefix(e.Text, "stop_failure"):
		return true
	}
	return false
}

// class is the session's class from what its file shows, in order: a
// subagent's id on every entry, a program's start, no turn at all.
func (r *reading) class() event.Class {
	switch {
	case r.visible == 0:
		return event.ClassUnknown
	case r.withAgent == r.visible:
		return event.ClassSubagent
	case r.programStart && r.userPrompts == 0:
		return event.ClassProgram
	case !r.turns && r.end && !r.start:
		return event.ClassUnrun
	case !r.turns:
		return event.ClassHousekeeping
	}
	return event.ClassUnknown
}

// local reports a single path component that stays in its folder and is
// not one of the inbox's own hidden names.
func local(name string) bool {
	return name != "" && filepath.IsLocal(name) && !strings.ContainsAny(name, `/\`) && !strings.HasPrefix(name, ".")
}
