package inboxv1

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/heyNag/eva-md/internal/inbox"
	"github.com/heyNag/eva-md/internal/ledger"
	"github.com/heyNag/eva-md/internal/ledger/event"
)

// padded is e's line padded with JSON white space before its closing brace
// to exactly n bytes, with its newline.
func padded(t *testing.T, e inbox.Entry, n int) string {
	t.Helper()
	s := strings.TrimSuffix(line(t, e), "\n")
	if len(s) > n {
		t.Fatalf("a line of %d bytes cannot be padded to %d", len(s), n)
	}
	return s[:len(s)-1] + strings.Repeat(" ", n-len(s)) + "}\n"
}

// A pause exactly as long as v1's limit is decoded by v1's walker, which
// pauses there, and repaired by its reader and its update. The backend
// pauses with the walker, so nothing after it is returned, whatever its
// kind, until a resume: not a host summary, not a lifecycle event's
// context, not a prompt. A resume as long is seen by the walker alone, and
// the backend never resumes there.
func TestPauseAtV1sLimit(t *testing.T) {
	root := newInbox(t)
	base := inbox.Entry{V: 1, Host: "codex", Session: "pause-1", Project: "general", Origin: "hook"}
	with := func(role, kind, turn, text string) inbox.Entry {
		e := base
		e.Role, e.Kind, e.Turn, e.Text = role, kind, turn, text
		return e
	}
	summary := with("host", "host_summary", "", "tok-summary said off the record")
	life := with("event", "event", "", "model_switch")
	life.Branch, life.Model = "tok-branch", "tok-model"
	data := line(t, with("user", "prompt", "t1", "before")) + // 1
		padded(t, with("event", "event", "", "off_record"), inbox.MaxLine) + // 2
		line(t, summary) + // 3
		line(t, life) + // 4
		line(t, with("user", "prompt", "t2", "tok-prompt said off the record")) + // 5
		line(t, with("event", "event", "", "on_record")) + // 6
		line(t, with("user", "prompt", "t3", "after")) + // 7
		line(t, with("event", "event", "", "off_record")) + // 8
		padded(t, with("event", "event", "", "on_record"), inbox.MaxLine) + // 9
		line(t, with("user", "prompt", "t4", "tok-still off the record")) // 10
	appendRaw(t, root, "general", "codex-pause-1.jsonl", data)
	k := sessions(t, root)["pause-1"]
	v := viewV1(t, root, k)
	if v.read[1] != repaired || !v.walkOff[3] || v.read[8] != repaired || v.walkOff[10] {
		t.Fatal("v1's walker and reader no longer disagree on a marker at the limit; revisit this test")
	}
	_, events := checkV1(t, root, k)
	whys := []string{"", "", event.WhyOffRecord, event.WhyOffRecord, event.WhyOffRecord, "", "", "", "", event.WhyOffRecord}
	for i, e := range events {
		if e.Why != whys[i] {
			t.Errorf("line %d: why %q, want %q", i+1, e.Why, whys[i])
		}
		b, err := event.Encode(e)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(b), "tok-") {
			t.Errorf("line %d returns %s", i+1, b)
		}
	}
	if !events[1].Has(event.MarkRepaired) || !events[8].Has(event.MarkRepaired) || events[6].Text != "after" {
		t.Errorf("the markers at the limit read as %+v and %+v, the prompt after the resume as %+v", events[1], events[8], events[6])
	}
}

// A turn is told by its turn id as well as by its prompt: a reply in a
// turn that stored no prompt, as a pause and a resume store none, is not
// filed under the last prompt's turn. Nothing withheld counts, so the
// turns said off the record are not told.
func TestTurnsFollowTurnIDs(t *testing.T) {
	root := newInbox(t)
	e := func(role, kind, turn, text string) inbox.Entry {
		return inbox.Entry{Host: "codex", Session: "turns-1", Project: "general", Role: role, Kind: kind, Turn: turn, Text: text, Origin: "hook"}
	}
	for _, x := range []inbox.Entry{
		e("user", "prompt", "t1", "should we pick plan A?"),            // 1: turn 1
		e("assistant", "reply", "t1", "plan A, then."),                 // 2: turn 1
		e("event", "event", "t2", "off_record"),                        // 3: turn 2, a pause
		e("assistant", "reply", "t2", "tok-withheld"),                  // 4: withheld
		e("user", "prompt", "t9", "tok-another turn, off the record"),  // 5: withheld, opens no turn
		e("assistant", "reply", "t9", "tok-its reply"),                 // 6: withheld
		e("event", "event", "t3", "on_record"),                         // 7: turn 3, the resume
		e("assistant", "reply", "t3", "yes, go ahead and delete it"),   // 8: turn 3, not turn 1
		e("event", "event", "", "compact"),                             // 9: no turn id, the current turn
		e("user", "prompt", "", "a prompt with no turn id"),            // 10: turn 4
		e("assistant", "reply", "", "its reply"),                       // 11: turn 4
		e("assistant", "reply", "t1", "a late reply, recovered to t1"), // 12: turn 1, where t1 is
		e("tool", "tool", "", "Bash ls"),                               // 13: turn 4 still
		e("user", "prompt", "t1", "a second prompt sent into t1"),      // 14: turn 1
	} {
		add(t, root, x)
	}
	_, events := checkV1(t, root, sessions(t, root)["turns-1"])
	want := []int{1, 1, 2, 2, 2, 2, 3, 3, 3, 4, 4, 1, 4, 1}
	for i, ev := range events {
		if ev.Turn != want[i] {
			t.Errorf("line %d: turn %d, want %d", i+1, ev.Turn, want[i])
		}
	}
}

// A line said off the record and stored again on the record under the same
// turn, as v1's recovery may store it, is withheld again, as said off the
// record. A line with the same words in another turn is returned.
func TestOffRecordLineStoredAgain(t *testing.T) {
	root := newInbox(t)
	e := func(role, kind, turn, text, origin string) inbox.Entry {
		return inbox.Entry{Host: "codex", Session: "again-1", Project: "general", Role: role, Kind: kind, Turn: turn, Text: text, Origin: origin}
	}
	for _, x := range []inbox.Entry{
		e("event", "event", "t1", "off_record", "hook"),                   // 1
		e("user", "prompt", "t2", "tok-said off the record", "hook"),      // 2: withheld
		e("assistant", "reply", "t2", "tok-its reply", "hook"),            // 3: withheld
		e("event", "event", "t3", "on_record", "hook"),                    // 4
		e("user", "prompt", "t2", "tok-said off the record", "catchup"),   // 5: the same, recovered
		e("assistant", "reply", "t2", "tok-its reply", "hook"),            // 6: the same again
		e("user", "prompt", "t4", "tok-said off the record", "hook"),      // 7: another turn: returned
		e("assistant", "reply", "t2", "a reply with other words", "hook"), // 8: other words: returned
	} {
		add(t, root, x)
	}
	s, events := checkV1(t, root, sessions(t, root)["again-1"])
	whys := []string{"", event.WhyOffRecord, event.WhyOffRecord, "", event.WhyOffRecord, event.WhyOffRecord, "", ""}
	for i, ev := range events {
		if ev.Why != whys[i] {
			t.Errorf("line %d: why %q, want %q", i+1, ev.Why, whys[i])
		}
	}
	if events[6].Text != "tok-said off the record" || s.Withheld != 4 {
		t.Errorf("the same words in another turn read as %+v; %d withheld", events[6], s.Withheld)
	}
}

// Read refuses a key that names a session other than its file's, so no
// file is read under another session's ids.
func TestReadRefusesAnotherSessionsKey(t *testing.T) {
	root := newInbox(t)
	add(t, root, inbox.Entry{Host: "claude-code", Session: "sess-real-1", Project: "general", Role: "user", Kind: "prompt", Turn: "t1", Text: "made-up words", Origin: "hook"})
	add(t, root, inbox.Entry{Host: "codex", Session: "sess-other-2", Project: "general", Role: "user", Kind: "prompt", Turn: "t1", Text: "other words", Origin: "hook"})
	b := Backend{Root: root}
	good := sessions(t, root)["sess-real-1"]
	if _, _, err := b.Read(context.Background(), good); err != nil {
		t.Fatal(err)
	}
	for _, k := range []ledger.Key{
		{Host: "codex", Session: "someone-else-0000", Folder: "general", File: good.File},
		{Host: "claude-code", Session: "someone-else-0000", Folder: "general", File: good.File},
		{Host: "codex", Session: "sess-real-1", Folder: "general", File: good.File},
		{Host: "claude-code", Session: "sess-other-2", Folder: "general", File: good.File},
		{Host: "other-host", Session: "sess-real-1", Folder: "general", File: good.File},
	} {
		if _, _, err := b.Read(context.Background(), k); err == nil {
			t.Errorf("Read(%+v) succeeded", k)
		}
	}
	// A session id the file's name had to change is read under the id its
	// lines carry, as Sessions names it.
	add(t, root, inbox.Entry{Host: "codex", Session: "odd id: 3", Project: "general", Role: "user", Kind: "prompt", Turn: "t1", Text: "made-up", Origin: "hook"})
	k, ok := sessions(t, root)["odd id: 3"]
	if !ok || k.File == "codex-odd id: 3.jsonl" {
		t.Fatalf("the odd id is listed as %+v", k)
	}
	if s, events, err := b.Read(context.Background(), k); err != nil || s.ID != "odd id: 3" || events[0].Text != "made-up" {
		t.Fatalf("%+v, %+v, %v", s, events, err)
	}
}

// Sessions lists only session files: a watermark whose file is gone is not
// a session. A project folder it cannot read in full fails the listing.
func TestSessionsListing(t *testing.T) {
	root := newInbox(t)
	add(t, root, inbox.Entry{Host: "codex", Session: "kept-1", Project: "general", Role: "event", Kind: "event", Text: "compact", Origin: "hook"})
	store := inbox.Store{Root: root}
	if err := os.WriteFile(filepath.Join(root, "general", "codex-gone-2.jsonl"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := store.SetWatermark(filepath.Join(root, "general", "codex-gone-2.jsonl"), 0); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(root, "general", "codex-gone-2.jsonl")); err != nil {
		t.Fatal(err)
	}
	captures, _, err := store.Captures()
	if err != nil || !slices.ContainsFunc(captures, func(c inbox.Capture) bool { return !c.File && c.ID == "gone-2" }) {
		t.Fatalf("v1 no longer lists the watermark alone; revisit this test: %+v, %v", captures, err)
	}
	keys, err := Backend{Root: root}.Sessions(context.Background())
	if err != nil || len(keys) != 1 || keys[0].Session != "kept-1" {
		t.Fatalf("%+v, %v", keys, err)
	}
	// A folder whose watermarks cannot be listed is one v1 could not read
	// in full; this holds whoever runs the test, root included.
	if err := os.MkdirAll(filepath.Join(root, "other"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "other", ".state"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if keys, err := (Backend{Root: root}).Sessions(context.Background()); err == nil {
		t.Fatalf("a folder that cannot be read is passed over: %+v", keys)
	}
}

// Each event name v1 writes is read with its author and the name mark; any
// other event text is no one's name and has no author the reader can name.
func TestV1EventNames(t *testing.T) {
	for _, c := range []struct {
		text   string
		kind   event.Kind
		author event.Author
		name   bool
	}{
		{"session_start: startup", event.KindLife, event.AuthorHost, true},
		{"session_start: resume", event.KindLife, event.AuthorHost, true},
		{"session_end", event.KindLife, event.AuthorHost, true},
		{"compact", event.KindLife, event.AuthorHost, true},
		{"model_switch", event.KindLife, event.AuthorHost, true},
		{"stop_failure: rate_limit", event.KindLife, event.AuthorHost, true},
		{"interrupt", event.KindLife, event.AuthorHost, true},
		{"host_prompt", event.KindPrompt, event.AuthorProgram, true},
		{"off_record", event.KindControl, event.AuthorEva, true},
		{"on_record", event.KindControl, event.AuthorEva, true},
		{"gap: missing final reply", event.KindGap, event.AuthorEva, true},
		{"gap: interrupted append", event.KindGap, event.AuthorEva, true},
		{"gapless thing", event.KindLife, event.AuthorUnknown, false},
		{"gap", event.KindLife, event.AuthorUnknown, false},
		{"something else", event.KindLife, event.AuthorUnknown, false},
	} {
		kind, author, marks, ok := classify(inbox.Entry{Role: "event", Kind: "event", Text: c.text})
		if !ok || kind != c.kind || author != c.author || slices.Contains(marks, event.MarkName) != c.name {
			t.Errorf("%q reads as %s %s %v %v", c.text, kind, author, marks, ok)
		}
	}
	for _, text := range v1Events {
		if _, author, marks, _ := classify(inbox.Entry{Role: "event", Kind: "event", Text: text}); author == event.AuthorUnknown || !slices.Contains(marks, event.MarkName) {
			t.Errorf("v1's event %q reads as no one's name", text)
		}
	}
}

// A prompt that begins with a voice hand-over after white space is marked
// as a wrapper; one with other words before it is not.
func TestWrapperAfterWhiteSpace(t *testing.T) {
	for text, want := range map[string]bool{
		wrapperPrompt:                         true,
		"\n" + wrapperPrompt:                  true,
		"  \t\r\n" + wrapperPrompt:            true,
		"made-up words " + wrapperPrompt:      false,
		"<realtime_delegation_other> made-up": false, // another tag
		"realtime_delegation":                 false,
	} {
		_, _, marks, _ := classify(inbox.Entry{Role: "user", Kind: "prompt", Text: text})
		if slices.Contains(marks, event.MarkWrapper) != want {
			t.Errorf("%q: marks %v", text, marks)
		}
	}
}
