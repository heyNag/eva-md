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
	"slices"
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
// and counted in the session's Tail. It refuses a key whose host and
// session are not those of its file, as Sessions names them, so no file is
// read under another session's ids.
func (b Backend) Read(ctx context.Context, k ledger.Key) (event.Session, []event.Event, error) {
	if !local(k.Folder) || !local(k.File) || !strings.HasSuffix(k.File, ".jsonl") {
		return event.Session{}, nil, errors.New("inboxv1: invalid session location")
	}
	if !b.names(k) {
		return event.Session{}, nil, errors.New("inboxv1: the key's host and session are not its file's")
	}
	store := inbox.Store{Root: b.Root}
	entries, end, err := store.ReadFrom(filepath.Join(b.Root, k.Folder, k.File), 0)
	if err != nil {
		return event.Session{}, nil, err
	}
	w, size, err := b.walk(ctx, k, entries, end)
	if err != nil {
		return event.Session{}, nil, err
	}
	r := newReading(k, w.offAfter)
	r.session.Tail = max(0, size-end)
	for i, e := range entries {
		if i%1024 == 0 {
			if err := ctx.Err(); err != nil {
				return event.Session{}, nil, err
			}
		}
		ev := r.line(int64(i+1), e, w.walked[i])
		if err := ev.Validate(); err != nil {
			return event.Session{}, nil, fmt.Errorf("inboxv1: line %d: %w", i+1, err)
		}
		r.events = append(r.events, ev)
	}
	r.session.Events = len(r.events)
	r.session.Class = r.class()
	return r.session, r.events, nil
}

// names reports whether k's host and session are those its file holds, as
// Sessions names them: the file is <host>-<id>.jsonl of a host eva-md
// captures, and the session is the id v1 reads for it (Store.SessionID).
func (b Backend) names(k ledger.Key) bool {
	if k.Host != "claude-code" && k.Host != "codex" {
		return false
	}
	id, ok := strings.CutPrefix(strings.TrimSuffix(k.File, ".jsonl"), k.Host+"-")
	c := inbox.Capture{Project: k.Folder, Host: k.Host, ID: id, File: true}
	return ok && id != "" && c.Name() == k.File && (inbox.Store{Root: b.Root}).SessionID(c) == k.Session
}

// walked is what v1's walker made of a session's lines: for each, whether
// it yielded the line with the entry v1's reader decoded for it, and, when
// the reader repaired a line, whether the walker was off the record after
// each line (nil otherwise).
type walked struct {
	walked   []bool
	offAfter []bool
}

// walk runs v1's walker over the lines entries holds, the first end bytes
// of the file, and returns what it made of each, and the file's size.
//
// The walker and the reader decode the same lines but one: a line exactly
// as long as v1's limit, which the walker decodes and the reader repairs.
// When such a line is a pause, only the walker sees it, and it says so
// only in its position. So when the reader repaired any line, the walker
// is run one line at a time and its state after each line is kept, and
// the reading pauses where the walker paused at a repaired line (see
// reading.line). Otherwise one walk does: the walker's state then follows
// the markers the reader decoded.
func (b Backend) walk(ctx context.Context, k ledger.Key, entries []inbox.Entry, end int64) (walked, int64, error) {
	root, err := paths.OpenRoot(filepath.Join(b.Root, k.Folder), false)
	if err != nil {
		return walked{}, 0, err
	}
	defer root.Close()
	f, info, err := inbox.OpenSession(root, k.File)
	if err != nil {
		return walked{}, 0, err
	}
	defer f.Close()
	w := walked{walked: make([]bool, len(entries))}
	if end == 0 {
		return w, info.Size(), nil
	}
	visit := func(l inbox.Line) bool {
		if l.Line >= 1 && l.Line <= len(entries) {
			w.walked[l.Line-1] = entries[l.Line-1] == l.Entry
		}
		return true
	}
	var pos inbox.Position
	if slices.Contains(entries, repaired) {
		w.offAfter = make([]bool, len(entries))
		for i := range entries {
			next, err := inbox.Walk(ctx, f, k.Folder, k.File, pos, 1, visit)
			if err != nil {
				return walked{}, 0, err
			}
			if next.Line != i+1 {
				return walked{}, 0, errors.New("inboxv1: the session file changed while it was read")
			}
			w.offAfter[i], pos = next.Off, next
		}
	} else if pos, err = inbox.Walk(ctx, f, k.Folder, k.File, inbox.Position{}, end, visit); err != nil {
		return walked{}, 0, err
	}
	if pos.Offset != end || pos.Line != len(entries) {
		return walked{}, 0, errors.New("inboxv1: the session file changed while it was read")
	}
	return w, info.Size(), nil
}

// reading is the state of one session's read.
type reading struct {
	key     ledger.Key
	session event.Session
	events  []event.Event
	keys    map[string]first // sameness key to the first event that has it
	hooks   map[string]int   // how many hook lines had each turn key (see sameness)
	// offKeys holds the turn keys of lines withheld as off the record, so
	// that the same line stored again on the record is withheld too. It is
	// never returned.
	offKeys map[string]bool
	// turn is the current turn's ordinal, last the highest ordinal given,
	// and tidTurn the ordinal each turn id seen on a returned line was
	// given (see turnOf).
	turn, last int
	tidTurn    map[string]int
	// walkOff and anyOff are the off-the-record state as v1's walker
	// tracks it, from markers that belong to the file, and as v1's update
	// tracks it, from any marker line. A line is off the record when
	// either is set. walkerOff is the walker's own state after each line,
	// when walk kept it.
	walkOff, anyOff bool
	walkerOff       []bool
	// userPrompts counts the prompts v1 filed as the user's that belong to
	// the file, withheld or not: it only ever denies a class. The rest of
	// the evidence for the session's class comes from the lines returned.
	userPrompts                   int
	visible, withAgent            int
	turns, start, end             bool
	firstPromptSeen, programStart bool
}

// first is the first event with a sameness key: its seq and its turn.
type first struct {
	seq  int64
	turn int
}

// newReading starts the read of the session k names. walkerOff is the
// walker's state after each line, or nil (see walk).
func newReading(k ledger.Key, walkerOff []bool) *reading {
	return &reading{key: k, keys: map[string]first{}, hooks: map[string]int{}, offKeys: map[string]bool{}, tidTurn: map[string]int{},
		walkerOff: walkerOff, session: event.Session{Host: k.Host, ID: k.Session, Folder: k.Folder}}
}

// line makes the event of physical line seq, whose entry as v1's reader
// decoded it is e; walked says whether v1's walker yielded it.
func (r *reading) line(seq int64, e inbox.Entry, walked bool) event.Event {
	trusted := inbox.Trusted(e, r.key.Folder, r.key.File, "")
	if !trusted && e == repaired {
		// The walker may have decoded the line the reader repaired, and
		// paused at it: the reading pauses too, and never resumes there.
		if r.walkerOff != nil && r.walkerOff[seq-1] {
			r.walkOff = true
		}
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
	if e.Role == "user" && e.Kind == "prompt" && e.Agent == "" {
		r.userPrompts++
	}
	kind, author, marks, ok := classify(e)
	key := ""
	if ok {
		key = turnKey(e, kind, marks)
	}
	if marker {
		r.walkOff = e.Text == "off_record"
	} else if r.walkOff || r.anyOff {
		if key != "" {
			r.offKeys[key] = true
		}
		return r.withhold(seq, event.WhyOffRecord)
	}
	if !ok {
		return r.withhold(seq, event.WhyUnknownKind)
	}
	if key != "" && r.offKeys[key] {
		// The same line was said off the record: this is it stored again.
		return r.withhold(seq, event.WhyOffRecord)
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
	ev.Src.Key = r.sameness(seq, e, key)
	if f, ok := r.keys[ev.Src.Key]; ok {
		ev.Copy, ev.Turn = f.seq, f.turn
		r.session.Copies++
	} else {
		ev.Turn = r.turnOf(e, kind)
		r.keys[ev.Src.Key] = first{seq: seq, turn: ev.Turn}
	}
	if ev.Has(event.MarkWrapper) {
		r.session.Wrappers++
	}
	r.note(e)
	return ev
}

// turnOf is the ordinal of the turn of a returned line that is not a copy.
// A turn id not seen before on a returned line opens a new turn, and one
// seen before is in the turn it opened; a prompt with no turn id opens a
// new turn; any other line is in the current turn. Only returned lines
// count, so nothing withheld tells how many turns it held.
func (r *reading) turnOf(e inbox.Entry, kind event.Kind) int {
	switch {
	case e.Turn != "":
		if t, ok := r.tidTurn[e.Turn]; ok {
			return t
		}
		r.last++
		r.turn = r.last
		r.tidTurn[e.Turn] = r.turn
	case kind == event.KindPrompt:
		r.last++
		r.turn = r.last
	}
	return r.turn
}

// withhold makes the bare gap that stands in the place of a line the reader
// returns nothing of.
func (r *reading) withhold(seq int64, why string) event.Event {
	r.session.Withheld++
	return event.Event{Seq: seq, Turn: r.turn, Kind: event.KindGap, Author: event.AuthorEva, Why: why,
		Src: event.Source{Key: event.PlaceKey(seq)}, Marks: []event.Mark{event.MarkWithheld}}
}

// classify maps a v1 entry to its kind, author and marks. An author comes
// only from a pair of role and kind v1 writes; ok is false for a kind the
// reader does not know.
func classify(e inbox.Entry) (event.Kind, event.Author, []event.Mark, bool) {
	sub := func(a event.Author) event.Author {
		if e.Agent != "" {
			return event.AuthorSubagent
		}
		return a
	}
	var wrapper []event.Mark
	if e.Kind == "prompt" && strings.HasPrefix(strings.TrimLeft(e.Text, " \t\r\n"), Wrapper) {
		wrapper = []event.Mark{event.MarkWrapper}
	}
	switch e.Role + "/" + e.Kind {
	case "user/prompt":
		return event.KindPrompt, sub(event.AuthorV1User), wrapper, true
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
		// v1's walker yields these kinds whatever the role, so their text
		// is returned, by no author the reader can name.
		kind, ok := map[string]event.Kind{"prompt": event.KindPrompt, "reply": event.KindReply, "tool": event.KindTool, "plan": event.KindTasks}[e.Kind]
		return kind, event.AuthorUnknown, wrapper, ok
	}
	name := []event.Mark{event.MarkName}
	switch {
	case e.Text == "host_prompt":
		return event.KindPrompt, sub(event.AuthorProgram), name, true
	case e.Text == "off_record" || e.Text == "on_record":
		return event.KindControl, event.AuthorEva, name, true
	case strings.HasPrefix(e.Text, "gap: "):
		return event.KindGap, event.AuthorEva, name, true
	case lifeEvent(e.Text):
		return event.KindLife, event.AuthorHost, name, true
	}
	return event.KindLife, event.AuthorUnknown, nil, true
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

// turnKey is what a line says in its turn, hashed: for a prompt or a reply
// with a turn id that is not a fixed name, its turn id, kind, subagent and
// text. Any other line has none ("").
func turnKey(e inbox.Entry, kind event.Kind, marks []event.Mark) string {
	if e.Turn == "" || slices.Contains(marks, event.MarkName) || (kind != event.KindPrompt && kind != event.KindReply) {
		return ""
	}
	sum := sha256.Sum256([]byte(e.Turn + "\x00" + string(kind) + "\x00" + e.Agent + "\x00" + e.Text))
	return "v1:turn:" + hex.EncodeToString(sum[:16])
}

// sameness is an event's sameness key. v1 keeps no host row id and no tool
// call id, so a prompt or a reply with a turn key (turnKey) is keyed by it:
// v1's recovery stores again, under the same turn, a prompt or a reply the
// hook stored. The hook stores each prompt it is given, so two hook lines
// are two events even when they say the same words in one turn, as a
// prompt sent twice into a running turn does (plan 14.3): the n-th hook
// line with a turn key is keyed by n, and any other line by 0, so a
// recovered line is a copy of the first line with its turn key, and a hook
// line only of a recovered one. Every other line is keyed by its position,
// so it is never a copy: the same command run twice in one turn is two
// events.
func (r *reading) sameness(seq int64, e inbox.Entry, key string) string {
	if key == "" {
		return positionKey(seq)
	}
	n := 0
	if e.Origin == "hook" {
		n = r.hooks[key]
		r.hooks[key]++
	}
	return key + ":" + strconv.Itoa(n)
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
