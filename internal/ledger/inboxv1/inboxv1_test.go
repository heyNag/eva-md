package inboxv1

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/heyNag/eva-md/internal/inbox"
	"github.com/heyNag/eva-md/internal/ledger"
	"github.com/heyNag/eva-md/internal/ledger/event"
	"github.com/heyNag/eva-md/internal/update"
)

// newInbox makes an empty v1 inbox in a fresh home. Nothing here reads or
// writes the real one.
func newInbox(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(home, "gitconfig"))
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	return filepath.Join(home, "inbox")
}

// add appends e with v1's own writer.
func add(t *testing.T, root string, e inbox.Entry) {
	t.Helper()
	if e.V == 0 {
		e.V = 1
	}
	if e.TS == "" {
		e.TS = "2026-11-02T10:00:00Z"
	}
	if err := (inbox.Store{Root: root}).Append(e); err != nil {
		t.Fatal(err)
	}
}

// appendRaw writes bytes to a session file as they are, as a killed writer
// or a hand edit would leave them.
func appendRaw(t *testing.T, root, folder, file, data string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(root, folder), 0o700); err != nil {
		t.Fatal(err)
	}
	f, err := os.OpenFile(filepath.Join(root, folder, file), os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := f.WriteString(data); err != nil {
		t.Fatal(err)
	}
}

func line(t *testing.T, e inbox.Entry) string {
	t.Helper()
	b, err := json.Marshal(e)
	if err != nil {
		t.Fatal(err)
	}
	return string(b) + "\n"
}

// v1View is what v1's own readers make of a session file: its reader's
// entry for each complete line, the update's decoding of each, the lines
// its walker yields, by line number, and whether the walker was off the
// record before each line (walkOff[n] for line n), as it tracks it.
type v1View struct {
	read    []inbox.Entry
	decoded []update.Entry
	walked  map[int]inbox.Line
	walkOff []bool
}

func viewV1(t *testing.T, root string, k ledger.Key) v1View {
	t.Helper()
	file := filepath.Join(root, k.Folder, k.File)
	read, _, err := (inbox.Store{Root: root}).ReadFrom(file, 0)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	dir, err := os.OpenRoot(filepath.Join(root, k.Folder))
	if err != nil {
		t.Fatal(err)
	}
	defer dir.Close()
	f, _, err := inbox.OpenSession(dir, k.File)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	walked := map[int]inbox.Line{}
	if _, err := inbox.Walk(context.Background(), f, k.Folder, k.File, inbox.Position{}, 0, func(l inbox.Line) bool {
		walked[l.Line] = l
		return true
	}); err != nil {
		t.Fatal(err)
	}
	// The walker's own off-the-record state before each line, one line at
	// a time: it reports the state only where it stops.
	walkOff := make([]bool, len(read)+1)
	var pos inbox.Position
	for n := 1; n <= len(read); n++ {
		walkOff[n] = pos.Off
		next, err := inbox.Walk(context.Background(), f, k.Folder, k.File, pos, 1, func(inbox.Line) bool { return true })
		if err != nil || next.Line != n {
			t.Fatalf("%s: the walker stops at line %d before line %d: %v", k.File, next.Line, n, err)
		}
		pos = next
	}
	return v1View{read: read, decoded: update.Decode(data, 0, 1), walked: walked, walkOff: walkOff}
}

// checkV1 reads a session through the backend and holds it to what v1's
// readers make of the same file: one event for each complete line, its
// seq the line's number, its text v1's text for that line, or nothing when
// the reader withholds it, and never the author typed. It returns the
// session and its events.
func checkV1(t *testing.T, root string, k ledger.Key) (event.Session, []event.Event) {
	t.Helper()
	s, events, err := Backend{Root: root}.Read(context.Background(), k)
	if err != nil {
		t.Fatal(err)
	}
	v := viewV1(t, root, k)
	if len(events) != len(v.read) || len(events) != len(v.decoded) || s.Events != len(events) {
		t.Fatalf("%s: %d events, %d lines read by v1, %d decoded by its update, session says %d", k.File, len(events), len(v.read), len(v.decoded), s.Events)
	}
	withheld, copies := 0, 0
	updateOff := false // the off-the-record state as v1's update tracks it, from any marker line
	// What prompts and replies said off the record say in their turn: the
	// same line stored again on the record is withheld too.
	offSaid := map[string]bool{}
	for i, e := range events {
		n := i + 1
		if d := v.decoded[i]; d.Kind == "event" && (d.Text == "off_record" || d.Text == "on_record") {
			updateOff = d.Text == "off_record"
		}
		if e.Seq != int64(n) || v.decoded[i].Line != n {
			t.Fatalf("%s line %d: seq %d, update's line %d", k.File, n, e.Seq, v.decoded[i].Line)
		}
		if err := e.Validate(); err != nil {
			t.Fatalf("%s line %d: %v", k.File, n, err)
		}
		if e.Author == event.AuthorTyped {
			t.Fatalf("%s line %d: the v1 reader filed an event as typed", k.File, n)
		}
		w, walked := v.walked[n]
		r := v.read[i]
		said := ""
		if r.Turn != "" && (r.Kind == "prompt" || r.Kind == "reply") {
			said = r.Turn + "\x00" + r.Kind + "\x00" + r.Agent + "\x00" + r.Text
		}
		off := updateOff || v.walkOff[n]
		if e.Kind != event.KindControl && !e.Has(event.MarkWithheld) && !e.Has(event.MarkRepaired) && off {
			t.Errorf("%s line %d: returned, but v1's walker or its update is off the record there", k.File, n)
		}
		switch {
		case e.Has(event.MarkWithheld):
			withheld++
			// What v1's walker yields is withheld only where v1's update
			// would drop it as off the record, or where the same line was
			// said off the record before.
			if walked && v.read[i] == w.Entry && (e.Why != event.WhyOffRecord || !updateOff && (said == "" || !offSaid[said])) {
				t.Errorf("%s line %d: withheld (%s), but v1's walker yields it", k.File, n, e.Why)
			}
			if e.Why == event.WhyOffRecord && off && said != "" {
				offSaid[said] = true
			}
		case e.Has(event.MarkRepaired):
			if v.read[i].Text != e.Text || v.read[i].Session != "" || v.decoded[i].Text != e.Text {
				t.Errorf("%s line %d: repaired as %q, v1 reads %q", k.File, n, e.Text, v.read[i].Text)
			}
		default:
			if e.Text != v.read[i].Text || e.Text != v.decoded[i].Text {
				t.Errorf("%s line %d: text %q, v1 reads %q", k.File, n, e.Text, v.read[i].Text)
			}
			if walked && w.Text != e.Text {
				t.Errorf("%s line %d: text %q, v1's walker yields %q", k.File, n, e.Text, w.Text)
			}
			if !walked && (e.Kind == event.KindPrompt && !e.Has(event.MarkName) || e.Kind == event.KindReply || e.Kind == event.KindTool || e.Kind == event.KindTasks) {
				t.Errorf("%s line %d: a %s v1's walker passes over is returned", k.File, n, e.Kind)
			}
		}
		if e.Copy != 0 {
			copies++
			first := events[e.Copy-1]
			if first.Src.Key != e.Src.Key || first.Text != e.Text || first.Kind != e.Kind || first.TID != e.TID || first.Agent != e.Agent || first.Copy != 0 {
				t.Errorf("%s line %d: a copy of line %d that is not the same event", k.File, n, e.Copy)
			}
		}
	}
	if s.Withheld != withheld || s.Copies != copies {
		t.Errorf("%s: the session counts %d withheld and %d copies, the events %d and %d", k.File, s.Withheld, s.Copies, withheld, copies)
	}
	return s, events
}

// The fixture's session ids, made up.
const (
	sidChat      = "5f0c9a1e-1111-4000-8000-000000000001"
	sidSubagent  = "a7a7a7a7-2222-4000-8000-000000000002"
	sidVoice     = "0193c2d1-3333-7000-8000-000000000003"
	sidProgram   = "b4b4b4b4-4444-4000-8000-000000000004"
	sidUnrun     = "c5c5c5c5-5555-7000-8000-000000000005"
	sidHousework = "d6d6d6d6-6666-7000-8000-000000000006"
)

const wrapperPrompt = "<realtime_delegation>\n  <input>made-up hand-over request</input>\n  <transcript_delta>user: made-up words</transcript_delta>\n</realtime_delegation>"

// fixtureInbox writes a v1 inbox with v1's writer, plus what a killed
// writer and a hand edit leave: several sessions in two project folders, a
// gap line, a repaired partial line, a partial last line, an off-the-record
// range, a subagent's entries, a program's prompt, a voice hand-over, a
// duplicated entry and an entry over v1's text limit.
func fixtureInbox(t *testing.T) string {
	t.Helper()
	root := newInbox(t)
	chat := func(e inbox.Entry) inbox.Entry {
		e.Host, e.Session, e.Project, e.Branch, e.Cwd, e.Model = "claude-code", sidChat, "acme-app", "main", "src/acme-app", "model-a"
		if e.Origin == "" {
			e.Origin = "hook"
		}
		return e
	}
	for _, e := range []inbox.Entry{
		{Role: "event", Kind: "event", Text: "session_start: startup"},                                     // 1
		{Role: "user", Kind: "prompt", Turn: "p1", Text: "Add a retry to the fetcher."},                    // 2
		{Role: "tool", Kind: "tool", Turn: "p1", Text: "Bash go test ./..."},                               // 3
		{Role: "tool", Kind: "tool", Turn: "p1", Text: "Bash go test ./..."},                               // 4: the same command again
		{Role: "assistant", Kind: "reply", Turn: "p1", Text: "Added the retry."},                           // 5
		{Role: "user", Kind: "prompt", Turn: "p1", Text: "Add a retry to the fetcher.", Origin: "catchup"}, // 6: a copy of 2
		{Role: "user", Kind: "prompt", Turn: "p2", Text: "Now the docs."},                                  // 7
		{Role: "assistant", Kind: "reply", Turn: "p2", Text: "Docs, part one."},                            // 8
		{Role: "assistant", Kind: "reply", Turn: "p2", Text: "Docs, part two."},                            // 9: a second reply in the turn
		{Role: "assistant", Kind: "reply", Turn: "p1", Text: "Added the retry.", Origin: "catchup"},        // 10: a copy of 5
		{Role: "event", Kind: "event", Turn: "p3", Text: "off_record"},                                     // 11
		{Role: "user", Kind: "prompt", Turn: "p4", Text: "tok-offprompt about the merger"},                 // 12
		{Role: "assistant", Kind: "reply", Turn: "p4", Text: "tok-offreply noted", Branch: "x"},            // 13
		{Role: "event", Kind: "event", Text: "compact"},                                                    // 14
		{Role: "event", Kind: "event", Turn: "p5", Text: "on_record"},                                      // 15
		{Role: "user", Kind: "prompt", Turn: "p6", Text: "Back to the fetcher."},                           // 16
	} {
		add(t, root, chat(e))
	}
	forged := chat(inbox.Entry{V: 1, Role: "user", Kind: "prompt", Turn: "p6", Text: "tok-forged from another chat"})
	forged.Session = "another-session"
	appendRaw(t, root, "acme-app", "claude-code-"+sidChat+".jsonl", line(t, forged)) // 17
	long := strings.Repeat("a long reply line\n", 13000)                             // over 200 KiB
	for _, e := range []inbox.Entry{
		{Role: "assistant", Kind: "reply", Turn: "p6", Text: long},                                   // 18
		{Role: "plan", Kind: "plan", Turn: "p6", Text: "TodoWrite [ ] write the tests"},              // 19
		{Role: "host", Kind: "host_summary", Text: "Summary of the fetcher work."},                   // 20
		{Role: "event", Kind: "event", Turn: "p7", Text: "gap: missing final reply"},                 // 21
		{Role: "tool", Kind: "tool", Turn: "p7", Agent: "agent-7", Text: "Read fetch.go (subagent)"}, // 22
	} {
		add(t, root, chat(e))
	}

	sub := func(e inbox.Entry) inbox.Entry {
		e.Host, e.Session, e.Project, e.Agent, e.Origin, e.Turn = "claude-code", sidSubagent, "acme-app", "agent-9", "hook", "x1"
		return e
	}
	add(t, root, sub(inbox.Entry{Role: "user", Kind: "prompt", Text: "Look for the loader."}))
	add(t, root, sub(inbox.Entry{Role: "tool", Kind: "tool", Text: `Grep "loader" (subagent)`}))
	add(t, root, sub(inbox.Entry{Role: "assistant", Kind: "reply", Text: "It is in load.go."}))

	voice := func(e inbox.Entry) inbox.Entry {
		e.Host, e.Session, e.Project, e.Origin = "codex", sidVoice, "general", "hook"
		return e
	}
	add(t, root, voice(inbox.Entry{Role: "event", Kind: "event", Text: "session_start: startup"}))  // 1
	add(t, root, voice(inbox.Entry{Role: "user", Kind: "prompt", Turn: "t1", Text: wrapperPrompt})) // 2
	add(t, root, voice(inbox.Entry{Role: "assistant", Kind: "reply", Turn: "t1", Text: "Handed over."}))
	voiceFile := "codex-" + sidVoice + ".jsonl"
	appendRaw(t, root, "general", voiceFile, `{"v":1,"ts":"2026-11-02T10:05:00Z","host":"codex","session":"`+sidVoice+`","role":"assist`) // 4, then repaired
	add(t, root, voice(inbox.Entry{Role: "user", Kind: "prompt", Turn: "t2", Text: "After the crash."}))                                  // 5 the gap, 6 the prompt
	appendRaw(t, root, "general", voiceFile, "not a json line\n")                                                                         // 7
	add(t, root, voice(inbox.Entry{Role: "assistant", Kind: "reply", Turn: "t2", Text: "Recovered."}))                                    // 8
	appendRaw(t, root, "general", voiceFile, `{"v":1,"host":"codex","session":"`+sidVoice+`","role":"user","kind":"prompt","text":"tok-unfinished`)

	program := func(e inbox.Entry) inbox.Entry {
		e.Host, e.Session, e.Project, e.Origin = "claude-code", sidProgram, "general", "hook"
		return e
	}
	add(t, root, program(inbox.Entry{Role: "event", Kind: "event", Text: "session_start: startup"}))
	add(t, root, program(inbox.Entry{Role: "event", Kind: "event", Turn: "q1", Text: "host_prompt"}))
	add(t, root, program(inbox.Entry{Role: "assistant", Kind: "reply", Turn: "q1", Text: "Nightly check passed."}))
	add(t, root, program(inbox.Entry{Role: "event", Kind: "event", Text: "session_end"}))

	add(t, root, inbox.Entry{Host: "codex", Session: sidUnrun, Project: "general", Origin: "hook", Role: "event", Kind: "event", Text: "session_end"})
	for _, text := range []string{"session_start: startup", "compact"} {
		add(t, root, inbox.Entry{Host: "codex", Session: sidHousework, Project: "general", Origin: "hook", Role: "event", Kind: "event", Text: text})
	}
	return root
}

func sessions(t *testing.T, root string) map[string]ledger.Key {
	t.Helper()
	keys, err := Backend{Root: root}.Sessions(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]ledger.Key{}
	for _, k := range keys {
		out[k.Session] = k
	}
	return out
}

// The fixture inbox read through the backend gives, for every line, the
// event whose seq is the line number and whose text is v1's text for that
// line, and every session its class.
func TestFixtureInboxReadsAsV1Does(t *testing.T) {
	root := fixtureInbox(t)
	keys := sessions(t, root)
	if len(keys) != 6 {
		t.Fatalf("%d sessions listed: %v", len(keys), keys)
	}
	folders := map[string]bool{}
	for _, k := range keys {
		folders[k.Folder] = true
	}
	if !folders["acme-app"] || !folders["general"] || len(folders) != 2 {
		t.Fatalf("project folders %v", folders)
	}
	want := map[string]struct {
		class              event.Class
		events, copies, wh int
		wrappers           int
		tail               bool
	}{
		sidChat:      {event.ClassUnknown, 22, 2, 4, 0, false},
		sidSubagent:  {event.ClassSubagent, 3, 0, 0, 0, false},
		sidVoice:     {event.ClassUnknown, 8, 0, 0, 1, true},
		sidProgram:   {event.ClassProgram, 4, 0, 0, 0, false},
		sidUnrun:     {event.ClassUnrun, 1, 0, 0, 0, false},
		sidHousework: {event.ClassHousekeeping, 2, 0, 0, 0, false},
	}
	all := map[string][]event.Event{}
	for sid, w := range want {
		k, ok := keys[sid]
		if !ok {
			t.Fatalf("session %s is not listed", sid)
		}
		s, events := checkV1(t, root, k)
		if s.Class != w.class || s.Events != w.events || s.Copies != w.copies || s.Withheld != w.wh || s.Wrappers != w.wrappers || (s.Tail > 0) != w.tail {
			t.Errorf("%s: %+v, want %+v", sid, s, w)
		}
		if s.Host != k.Host || s.ID != sid || s.Folder != k.Folder {
			t.Errorf("%s: session names %s %s %s", sid, s.Host, s.ID, s.Folder)
		}
		all[sid] = events
	}

	chat := all[sidChat]
	expect := func(n int, kind event.Kind, author event.Author, why string, copyOf int64, turn int) {
		t.Helper()
		e := chat[n-1]
		if e.Kind != kind || e.Author != author || e.Why != why || e.Copy != copyOf || e.Turn != turn {
			t.Errorf("chat line %d: %s %s why %q copy %d turn %d; want %s %s %q %d %d", n, e.Kind, e.Author, e.Why, e.Copy, e.Turn, kind, author, why, copyOf, turn)
		}
	}
	expect(1, event.KindLife, event.AuthorHost, "", 0, 0)
	expect(2, event.KindPrompt, event.AuthorV1User, "", 0, 1)
	expect(3, event.KindTool, event.AuthorTool, "", 0, 1)
	expect(4, event.KindTool, event.AuthorTool, "", 0, 1)
	expect(5, event.KindReply, event.AuthorAgent, "", 0, 1)
	expect(6, event.KindPrompt, event.AuthorV1User, "", 2, 1)
	expect(7, event.KindPrompt, event.AuthorV1User, "", 0, 2)
	expect(8, event.KindReply, event.AuthorAgent, "", 0, 2)
	expect(9, event.KindReply, event.AuthorAgent, "", 0, 2)
	expect(10, event.KindReply, event.AuthorAgent, "", 5, 1) // a copy is in its first's turn
	expect(11, event.KindControl, event.AuthorEva, "", 0, 3) // a pause is a turn of its own
	expect(12, event.KindGap, event.AuthorEva, event.WhyOffRecord, 0, 3)
	expect(13, event.KindGap, event.AuthorEva, event.WhyOffRecord, 0, 3)
	expect(14, event.KindGap, event.AuthorEva, event.WhyOffRecord, 0, 3)
	expect(15, event.KindControl, event.AuthorEva, "", 0, 4)
	expect(16, event.KindPrompt, event.AuthorV1User, "", 0, 5)
	expect(17, event.KindGap, event.AuthorEva, event.WhyUntrusted, 0, 5)
	expect(18, event.KindReply, event.AuthorAgent, "", 0, 5)
	expect(19, event.KindTasks, event.AuthorAgent, "", 0, 5)
	expect(20, event.KindSummary, event.AuthorHost, "", 0, 5)
	expect(21, event.KindGap, event.AuthorEva, "", 0, 6)
	expect(22, event.KindTool, event.AuthorSubagent, "", 0, 6)
	if chat[10].Op != event.OpOff || chat[14].Op != event.OpOn {
		t.Errorf("the controls hold %q and %q", chat[10].Op, chat[14].Op)
	}
	if !chat[5].Has(event.MarkRecovered) || chat[1].Has(event.MarkRecovered) || !chat[1].Src.Hook {
		t.Error("the line v1 recovered is not told from the hook's")
	}
	if c := chat[17].Cut; len(c) != 1 || c[0].Why != event.CutLegacy || c[0].At != -1 || c[0].Lost < 1 || len(chat[17].Text) > 200<<10 {
		t.Errorf("the reply over v1's limit has cuts %+v and %d bytes", c, len(chat[17].Text))
	}
	if ctx := chat[1].Ctx; ctx == nil || *ctx != (event.Context{Project: "acme-app", Branch: "main", Cwd: "src/acme-app", Model: "model-a"}) {
		t.Errorf("context %+v", ctx)
	}

	voice := all[sidVoice]
	if !voice[1].Has(event.MarkWrapper) || voice[1].Author != event.AuthorV1User || voice[1].Text != wrapperPrompt {
		t.Errorf("the voice hand-over reads as %+v", voice[1])
	}
	if !voice[3].Has(event.MarkRepaired) || voice[4].Kind != event.KindGap || voice[4].Text != "gap: interrupted append" || !voice[6].Has(event.MarkRepaired) {
		t.Errorf("the repaired lines read as %+v, %+v, %+v", voice[3], voice[4], voice[6])
	}
	if p := all[sidProgram][1]; p.Kind != event.KindPrompt || p.Author != event.AuthorProgram || p.Turn != 1 {
		t.Errorf("the program's prompt reads as %+v", p)
	}
	for _, e := range all[sidSubagent] {
		if e.Author != event.AuthorSubagent || e.Agent != "agent-9" {
			t.Errorf("a subagent's entry reads as %+v", e)
		}
	}

	// Nothing said off the record, forged into the file or left unfinished
	// is anywhere in what the reader returns.
	for sid, events := range all {
		for _, e := range events {
			b, err := event.Encode(e)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(b), "tok-") {
				t.Errorf("%s line %d returns %s", sid, e.Seq, b)
			}
		}
	}
}

// Every reference v1 could have written to the fixture, s:<first 8>#<line>
// and with the whole session id, resolves to the event of that line, and
// so to v1's text for it; the event's own short form resolves the same way.
func TestV1ReferencesResolve(t *testing.T) {
	root := fixtureInbox(t)
	b := Backend{Root: root}
	for sid, k := range sessions(t, root) {
		v := viewV1(t, root, k)
		_, events, err := b.Read(context.Background(), k)
		if err != nil {
			t.Fatal(err)
		}
		for i := range v.read {
			n := i + 1
			for _, ref := range []string{inbox.Ref(sid, n), "s:" + sid + "#" + strconv.Itoa(n), event.ID{Host: k.Host, Session: sid, Seq: int64(n)}.Ref()} {
				s, e, err := ledger.Resolve(context.Background(), b, ref)
				if err != nil {
					t.Fatalf("%s: %v", ref, err)
				}
				if s.ID != sid || e.Seq != int64(n) || e.Text != events[i].Text {
					t.Fatalf("%s resolves to %s line %d %q", ref, s.ID, e.Seq, e.Text)
				}
				if !e.Has(event.MarkWithheld) && e.Text != v.read[i].Text {
					t.Fatalf("%s resolves to %q, v1 reads %q", ref, e.Text, v.read[i].Text)
				}
			}
		}
		if _, _, err := ledger.Resolve(context.Background(), b, inbox.Ref(sid, len(v.read)+1)); !errors.Is(err, ledger.ErrNotFound) {
			t.Fatalf("a line past the end: %v", err)
		}
	}
	if _, _, err := ledger.Resolve(context.Background(), b, "s:ffffffff#1"); !errors.Is(err, ledger.ErrNotFound) {
		t.Fatalf("an unknown session: %v", err)
	}
}

// A session v1 wrote into two project folders, and two sessions that share
// their first 8 characters, are not resolved by a short reference to a line
// both hold; the whole id tells the second pair apart. A line only one of
// them holds resolves to it, as v1's eva-md show resolves it, and so does
// a line only one of them holds as a line that decodes.
func TestAmbiguousReferences(t *testing.T) {
	root := newInbox(t)
	for _, project := range []string{"one", "two"} {
		add(t, root, inbox.Entry{Host: "codex", Session: "11112222-aaaa", Project: project, Role: "user", Kind: "prompt", Text: "in " + project, Origin: "hook"})
	}
	for _, text := range []string{"one: second", "one: third"} {
		add(t, root, inbox.Entry{Host: "codex", Session: "11112222-aaaa", Project: "one", Role: "assistant", Kind: "reply", Text: text, Origin: "hook"})
	}
	appendRaw(t, root, "two", "codex-11112222-aaaa.jsonl", "not a json line\n") // two's line 2 is repaired
	add(t, root, inbox.Entry{Host: "codex", Session: "33334444-aaaa", Project: "one", Role: "user", Kind: "prompt", Text: "first", Origin: "hook"})
	add(t, root, inbox.Entry{Host: "codex", Session: "33334444-aaaa", Project: "one", Role: "assistant", Kind: "reply", Text: "first's reply", Origin: "hook"})
	add(t, root, inbox.Entry{Host: "codex", Session: "33334444-bbbb", Project: "one", Role: "user", Kind: "prompt", Text: "second", Origin: "hook"})
	b := Backend{Root: root}
	for _, ref := range []string{"s:11112222#1", "s:11112222-aaaa#1", "s:33334444#1", "e:33334444#1"} {
		if _, _, err := ledger.Resolve(context.Background(), b, ref); !errors.Is(err, ledger.ErrAmbiguous) {
			t.Errorf("%s: %v", ref, err)
		}
	}
	for ref, want := range map[string]string{
		"s:33334444-bbbb#1": "second",
		"s:33334444#2":      "first's reply",
		"s:11112222#3":      "one: third",
		"s:11112222-aaaa#3": "one: third",
		"s:11112222#2":      "one: second", // two's line 2 does not decode
	} {
		s, e, err := ledger.Resolve(context.Background(), b, ref)
		if err != nil || e.Text != want {
			t.Errorf("%s resolves to %q, %v; want %q", ref, e.Text, err, want)
		}
		if strings.HasPrefix(ref, "s:1111") && s.Folder != "one" {
			t.Errorf("%s resolves in folder %q", ref, s.Folder)
		}
	}
	if _, _, err := ledger.Resolve(context.Background(), b, "s:11112222#4"); !errors.Is(err, ledger.ErrNotFound) {
		t.Errorf("a line neither folder holds: %v", err)
	}
}

// Second copies: the same entry twice is marked and counted; two different
// replies in one turn, and the same command run twice in one turn, are two
// events and no copy. Nothing is dropped, and a copy needs its turn: the
// same words in another turn, as another kind, from another subagent or
// with no turn id are not a copy. Two lines the hook stored are two events
// even with the same words in one turn, as a prompt sent twice into a
// running turn (plan 14.3); a recovered line is a copy of the first line
// that says the same in its turn, and a hook line of a recovered one.
func TestSecondCopies(t *testing.T) {
	root := newInbox(t)
	e := func(role, kind, turn, agent, text, origin string) inbox.Entry {
		return inbox.Entry{Host: "codex", Session: "copies-1", Project: "general", Role: role, Kind: kind, Turn: turn, Agent: agent, Text: text, Origin: origin}
	}
	for _, x := range []inbox.Entry{
		e("user", "prompt", "t1", "", "Same words.", "hook"),       // 1
		e("user", "prompt", "t1", "", "Same words.", "catchup"),    // 2: copy of 1
		e("assistant", "reply", "t1", "", "Reply one.", "hook"),    // 3
		e("assistant", "reply", "t1", "", "Reply two.", "hook"),    // 4: another reply, no copy
		e("tool", "tool", "t1", "", "Bash make check", "hook"),     // 5
		e("tool", "tool", "t1", "", "Bash make check", "hook"),     // 6: the command again, no copy
		e("assistant", "reply", "t1", "", "Reply one.", "catchup"), // 7: copy of 3
		e("user", "prompt", "t2", "", "Same words.", "hook"),       // 8: another turn
		e("assistant", "reply", "t2", "", "Same words.", "hook"),   // 9: another kind
		e("assistant", "reply", "t2", "ag", "Same words.", "hook"), // 10: a subagent's
		e("assistant", "reply", "", "", "Untimed.", "hook"),        // 11
		e("assistant", "reply", "", "", "Untimed.", "catchup"),     // 12: no turn id, no copy
		e("event", "event", "t2", "", "host_prompt", "hook"),       // 13
		e("event", "event", "t2", "", "host_prompt", "hook"),       // 14: a name says nothing, no copy
		e("assistant", "reply", "t1", "", "Reply one.", "hook"),    // 15: the hook's again, no copy
		e("user", "prompt", "t3", "", "continue", "hook"),          // 16
		e("user", "prompt", "t3", "", "continue", "hook"),          // 17: sent twice into the turn, no copy
		e("user", "prompt", "t3", "", "continue", "catchup"),       // 18: copy of 16
		e("user", "prompt", "t3", "", "continue", "catchup"),       // 19: copy of 16
		e("assistant", "reply", "t4", "", "Recovered.", "catchup"), // 20
		e("assistant", "reply", "t4", "", "Recovered.", "catchup"), // 21: recovered twice, copy of 20
		e("assistant", "reply", "t4", "", "Recovered.", "hook"),    // 22: the hook's after recovery, copy of 20
		e("assistant", "reply", "t4", "", "Recovered.", "hook"),    // 23: the hook's again, no copy
	} {
		add(t, root, x)
	}
	k := sessions(t, root)["copies-1"]
	s, events := checkV1(t, root, k)
	if len(events) != 23 {
		t.Fatalf("%d events for 23 lines", len(events))
	}
	want := map[int64]int64{2: 1, 7: 3, 18: 16, 19: 16, 21: 20, 22: 20}
	for _, ev := range events {
		if ev.Copy != want[ev.Seq] {
			t.Errorf("line %d: copy of %d, want %d", ev.Seq, ev.Copy, want[ev.Seq])
		}
	}
	if s.Copies != len(want) {
		t.Errorf("%d copies counted, want %d", s.Copies, len(want))
	}
}

// A line exactly as long as v1's limit is decoded by v1's walker and
// replaced by a gap by its reader and its update. The backend sides with
// the reader, so the line's text is never returned; one byte shorter, all
// three decode it and the backend returns it.
func TestLineAtV1sLimit(t *testing.T) {
	root := newInbox(t)
	base := inbox.Entry{V: 1, Host: "codex", Session: "limit-1", Project: "general", Role: "assistant", Kind: "reply", Turn: "t1", Origin: "hook"}
	sized := func(n int) string {
		e := base
		e.Text = "x"
		e.Text = strings.Repeat("x", n-len(line(t, e))+2)
		s := line(t, e)
		if len(s)-1 != n {
			t.Fatalf("a line of %d bytes, not %d", len(s)-1, n)
		}
		return s
	}
	appendRaw(t, root, "general", "codex-limit-1.jsonl", sized(inbox.MaxLine)+sized(inbox.MaxLine-1))
	k := sessions(t, root)["limit-1"]
	v := viewV1(t, root, k)
	if _, ok := v.walked[1]; !ok || v.read[0] != repaired {
		t.Fatal("v1's walker and reader no longer disagree on a line at the limit; revisit this test")
	}
	_, events := checkV1(t, root, k)
	if !events[0].Has(event.MarkRepaired) || events[0].Text != repaired.Text {
		t.Errorf("the line at the limit reads as %s with %d bytes", events[0].Kind, len(events[0].Text))
	}
	if events[1].Kind != event.KindReply || len(events[1].Text) < inbox.MaxLine-200 {
		t.Errorf("the line under the limit reads as %s with %d bytes", events[1].Kind, len(events[1].Text))
	}
}

// v1's update ends an off-the-record range at any marker line, and its
// walker only at a marker that belongs to the file. The backend withholds
// a line when either would, so a marker forged into the file can pause the
// record but never resume it.
func TestForgedMarkers(t *testing.T) {
	root := newInbox(t)
	own := func(text, kind string) inbox.Entry {
		role := map[string]string{"prompt": "user", "event": "event"}[kind]
		return inbox.Entry{V: 1, Host: "codex", Session: "forge-1", Project: "general", Role: role, Kind: kind, Text: text, Origin: "hook"}
	}
	forged := func(text string) inbox.Entry {
		e := own(text, "event")
		e.Session = "forge-2"
		return e
	}
	var data string
	for _, e := range []inbox.Entry{
		own("before", "prompt"),               // 1
		forged("off_record"),                  // 2
		own("tok-after-forged-off", "prompt"), // 3: v1's update drops it
		own("on_record", "event"),             // 4
		own("visible again", "prompt"),        // 5
		own("off_record", "event"),            // 6
		forged("on_record"),                   // 7
		own("tok-after-forged-on", "prompt"),  // 8: still off
	} {
		data += line(t, e)
	}
	appendRaw(t, root, "general", "codex-forge-1.jsonl", data)
	_, events := checkV1(t, root, sessions(t, root)["forge-1"])
	whys := []string{"", event.WhyUntrusted, event.WhyOffRecord, "", "", "", event.WhyUntrusted, event.WhyOffRecord}
	for i, e := range events {
		if e.Why != whys[i] {
			t.Errorf("line %d: why %q, want %q", i+1, e.Why, whys[i])
		}
	}
}

// Read refuses a location that leaves the inbox.
func TestReadRefusesOutsideLocations(t *testing.T) {
	root := newInbox(t)
	add(t, root, inbox.Entry{Host: "codex", Session: "s-1", Project: "general", Role: "event", Kind: "event", Text: "compact", Origin: "hook"})
	b := Backend{Root: root}
	for _, k := range []ledger.Key{
		{Host: "codex", Session: "s-1", Folder: "..", File: "codex-s-1.jsonl"},
		{Host: "codex", Session: "s-1", Folder: "general/..", File: "codex-s-1.jsonl"},
		{Host: "codex", Session: "s-1", Folder: "general", File: "../general/codex-s-1.jsonl"},
		{Host: "codex", Session: "s-1", Folder: ".state", File: "codex-s-1.jsonl"},
		{Host: "codex", Session: "s-1", Folder: "general", File: "codex-s-1.jsonl.wm"},
		{Host: "codex", Session: "s-1", Folder: "", File: "codex-s-1.jsonl"},
	} {
		if _, _, err := b.Read(context.Background(), k); err == nil {
			t.Errorf("Read(%+v) succeeded", k)
		}
	}
	if _, _, err := b.Read(context.Background(), ledger.Key{Host: "codex", Session: "s-1", Folder: "general", File: "codex-s-1.jsonl"}); err != nil {
		t.Fatal(err)
	}
}

// A symlinked session file is refused, as v1's readers refuse it.
func TestReadRefusesSymlink(t *testing.T) {
	root := newInbox(t)
	add(t, root, inbox.Entry{Host: "codex", Session: "s-1", Project: "general", Role: "event", Kind: "event", Text: "compact", Origin: "hook"})
	if err := os.Symlink(filepath.Join(root, "general", "codex-s-1.jsonl"), filepath.Join(root, "general", "codex-s-2.jsonl")); err != nil {
		t.Fatal(err)
	}
	_, _, err := Backend{Root: root}.Read(context.Background(), ledger.Key{Host: "codex", Session: "s-2", Folder: "general", File: "codex-s-2.jsonl"})
	if err == nil {
		t.Fatal("a symlinked session file was read")
	}
}

func TestEmptyInbox(t *testing.T) {
	root := newInbox(t)
	keys, err := Backend{Root: root}.Sessions(context.Background())
	if err != nil || len(keys) != 0 {
		t.Fatalf("%v, %v", keys, err)
	}
	appendRaw(t, root, "general", "codex-empty.jsonl", "")
	s, events, err := Backend{Root: root}.Read(context.Background(), sessions(t, root)["empty"])
	if err != nil || len(events) != 0 || s.Class != event.ClassUnknown {
		t.Fatalf("%+v, %v, %v", s, events, err)
	}
}
