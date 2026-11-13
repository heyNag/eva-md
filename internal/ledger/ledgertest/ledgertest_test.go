package ledgertest_test

import (
	"context"
	"errors"
	"os"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"testing/fstest"

	"github.com/heyNag/eva-md/internal/ledger"
	"github.com/heyNag/eva-md/internal/ledger/event"
	"github.com/heyNag/eva-md/internal/ledger/ledgertest"
)

var (
	keyA = ledger.Key{Host: "claude-code", Session: "11112222-aaaa", Folder: "one", File: "a.jsonl"}
	keyB = ledger.Key{Host: "codex", Session: "33334444-bbbb", Folder: "two", File: "b.jsonl"}
)

// chat is a session of every shape a consumer meets: a typed prompt, a
// reply with a redaction, a cut, an exposure and a context, its second
// copy, a withheld line and an undecoded voice hand-over. All made up.
func chat() []event.Event {
	return []event.Event{
		{Seq: 1, Turn: 1, TID: "t1", Kind: event.KindPrompt, Author: event.AuthorTyped, Text: "fetch the page", Src: event.Source{Hook: true, Key: "k:1"}},
		{Seq: 2, Turn: 1, TID: "t1", Kind: event.KindReply, Author: event.AuthorAgent, Text: "key [REDACTED:key] found",
			Red: []event.Span{{Start: 4, End: 18, Kind: "key", Tier: "T2"}}, Cut: []event.Cut{{At: 24, Lost: 10, Why: event.CutSize}},
			Exposure: []event.Exposure{{Kind: event.ExposureWeb, At: 1}}, Src: event.Source{Hook: true, Key: "k:2"},
			Marks: []event.Mark{event.MarkRecovered}, Ctx: &event.Context{Project: "p_made_up", Branch: "main"}},
		{Seq: 3, Turn: 1, TID: "t1", Kind: event.KindReply, Author: event.AuthorAgent, Text: "key [REDACTED:key] found", Src: event.Source{Key: "k:2"}, Copy: 2},
		{Seq: 5, Turn: 1, Kind: event.KindGap, Author: event.AuthorEva, Why: event.WhyOffRecord, Src: event.Source{Key: event.PlaceKey(5)}, Marks: []event.Mark{event.MarkWithheld}},
		{Seq: 6, Turn: 2, Kind: event.KindPrompt, Author: event.AuthorV1User, Text: "<realtime_delegation> made-up </realtime_delegation>", Src: event.Source{Key: "k:6"}, Marks: []event.Mark{event.MarkWrapper}},
	}
}

func housekeeping() []event.Event {
	return []event.Event{
		{Seq: 1, Kind: event.KindLife, Author: event.AuthorHost, Text: "compact", Src: event.Source{Key: "k:1"}, Marks: []event.Mark{event.MarkName}},
	}
}

func sessions() []ledgertest.Session {
	return []ledgertest.Session{
		{Key: keyA, Class: event.ClassChat, Events: chat()},
		{Key: keyB, Class: event.ClassHousekeeping, Events: housekeeping()},
	}
}

func mustNew(t *testing.T, ss ...ledgertest.Session) *ledgertest.Fake {
	t.Helper()
	f, err := ledgertest.New(ss...)
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func TestNewRefuses(t *testing.T) {
	if _, err := ledgertest.New(sessions()...); err != nil {
		t.Fatalf("the sessions every case breaks are refused: %v", err)
	}
	cases := map[string]func(ss []ledgertest.Session) []ledgertest.Session{
		"an event Validate refuses": func(ss []ledgertest.Session) []ledgertest.Session {
			ss[0].Events[1].Kind = "speech"
			return ss
		},
		"a withheld event that holds text": func(ss []ledgertest.Session) []ledgertest.Session {
			ss[0].Events[3].Text = "said off the record"
			return ss
		},
		"a seq given twice": func(ss []ledgertest.Session) []ledgertest.Session {
			ss[0].Events[4].Seq = 5
			return ss
		},
		"seqs that fall": func(ss []ledgertest.Session) []ledgertest.Session {
			ss[0].Events[3], ss[0].Events[4] = ss[0].Events[4], ss[0].Events[3]
			return ss
		},
		"a seq of 0": func(ss []ledgertest.Session) []ledgertest.Session {
			ss[1].Events[0].Seq = 0
			return ss
		},
		"a copy with another key": func(ss []ledgertest.Session) []ledgertest.Session {
			ss[0].Events[2].Src.Key = "k:3"
			return ss
		},
		"a copy in another turn": func(ss []ledgertest.Session) []ledgertest.Session {
			ss[0].Events[2].Turn = 2
			return ss
		},
		"a copy of a copy": func(ss []ledgertest.Session) []ledgertest.Session {
			c := ss[0].Events[2]
			c.Seq, c.Copy = 4, 3
			ss[0].Events = slices.Insert(ss[0].Events, 3, c)
			return ss
		},
		"a copy of an event the session does not hold": func(ss []ledgertest.Session) []ledgertest.Session {
			ss[0].Events[4].Copy = 4
			ss[0].Events[4].Src.Key = "k:2"
			return ss
		},
		"two events that are not copies with one key": func(ss []ledgertest.Session) []ledgertest.Session {
			ss[0].Events[2].Copy = 0
			return ss
		},
		"a key given twice": func(ss []ledgertest.Session) []ledgertest.Session {
			return append(ss, ledgertest.Session{Key: keyA, Class: event.ClassUnknown})
		},
		"a class that is not valid": func(ss []ledgertest.Session) []ledgertest.Session {
			ss[1].Class = "person"
			return ss
		},
		"no class": func(ss []ledgertest.Session) []ledgertest.Session {
			ss[1].Class = ""
			return ss
		},
		"a session id no reference can name": func(ss []ledgertest.Session) []ledgertest.Session {
			ss[1].Key.Session = "a#b"
			return ss
		},
		"no host": func(ss []ledgertest.Session) []ledgertest.Session {
			ss[1].Key.Host = ""
			return ss
		},
	}
	for name, breakIt := range cases {
		if _, err := ledgertest.New(breakIt(sessions())...); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestFakeReads(t *testing.T) {
	ctx := context.Background()
	in := sessions()
	f := mustNew(t, in...)
	keys, err := f.Sessions(ctx)
	if err != nil || !slices.Equal(keys, []ledger.Key{keyA, keyB}) {
		t.Fatalf("Sessions: %v, %v", keys, err)
	}
	// Reversed, the listing follows.
	g := mustNew(t, in[1], in[0])
	if keys, _ := g.Sessions(ctx); !slices.Equal(keys, []ledger.Key{keyB, keyA}) {
		t.Fatalf("Sessions in the order given: %v", keys)
	}
	keys[0] = ledger.Key{}
	if again, _ := g.Sessions(ctx); again[0] != keyB {
		t.Fatal("changing the listing changed the fake")
	}

	s, events, err := f.Read(ctx, keyA)
	if err != nil {
		t.Fatal(err)
	}
	want := event.Session{Host: keyA.Host, ID: keyA.Session, Folder: keyA.Folder, Class: event.ClassChat, Events: 5, Copies: 1, Withheld: 1, Wrappers: 1}
	if s != want {
		t.Fatalf("the record is %+v, want %+v", s, want)
	}
	if !reflect.DeepEqual(events, chat()) {
		t.Fatalf("Read returns %+v, want the events given", events)
	}
	if s, _, _ := f.Read(ctx, keyB); s != (event.Session{Host: "codex", ID: keyB.Session, Folder: "two", Class: event.ClassHousekeeping, Events: 1}) {
		t.Fatalf("the second record is %+v", s)
	}

	// Changing what Read returned, or what New was given, changes nothing
	// the fake holds.
	events[1].Text = "changed"
	events[1].Red[0].Kind = "changed"
	events[1].Cut[0].Lost = 99
	events[1].Exposure[0].Kind = "changed"
	events[1].Marks[0] = "changed"
	events[1].Ctx.Project = "changed"
	in[0].Events[0].Text = "changed"
	in[0].Events[1].Ctx.Branch = "changed"
	in[0].Events[1].Marks[0] = "changed"
	if _, again, _ := f.Read(ctx, keyA); !reflect.DeepEqual(again, chat()) {
		t.Fatalf("a change to returned or given events reached the fake: %+v", again)
	}

	if _, _, err := f.Read(ctx, ledger.Key{Host: keyA.Host, Session: keyA.Session, Folder: "two", File: keyA.File}); !errors.Is(err, ledgertest.ErrNoSession) {
		t.Fatalf("an unknown key: %v", err)
	}
	if err := ledgertest.Check(ctx, f); err != nil {
		t.Fatalf("the fake breaks the contract: %v", err)
	}
}

// Every field of an event that can share memory is one the fake copies:
// a new one makes this test fail until the fake copies it too.
func TestEventsReferenceFieldsAreCopied(t *testing.T) {
	var found []string
	var walk func(path string, typ reflect.Type)
	walk = func(path string, typ reflect.Type) {
		switch typ.Kind() {
		case reflect.Struct:
			for i := range typ.NumField() {
				walk(path+"."+typ.Field(i).Name, typ.Field(i).Type)
			}
		case reflect.Slice, reflect.Pointer:
			found = append(found, path)
			walk(path+"[]", typ.Elem())
		case reflect.Map, reflect.Chan, reflect.Func, reflect.Interface, reflect.UnsafePointer, reflect.Array:
			found = append(found, path+" ("+typ.Kind().String()+")")
		}
	}
	walk("Event", reflect.TypeFor[event.Event]())
	want := []string{"Event.Red", "Event.Cut", "Event.Exposure", "Event.Marks", "Event.Ctx"}
	if !slices.Equal(found, want) {
		t.Fatalf("an event's fields that share memory are %v; the fake copies %v", found, want)
	}
}

func TestFaults(t *testing.T) {
	ctx := context.Background()
	f := mustNew(t, sessions()...)
	listing := errors.New("made-up listing failure")
	f.FailSessions(listing)
	if _, err := f.Sessions(ctx); !errors.Is(err, listing) {
		t.Fatalf("FailSessions: %v", err)
	}
	if err := ledgertest.Check(ctx, f); !errors.Is(err, listing) {
		t.Fatalf("Check over a listing that fails: %v", err)
	}
	f.FailSessions(nil)
	if _, err := f.Sessions(ctx); err != nil {
		t.Fatalf("FailSessions(nil): %v", err)
	}

	reading := errors.New("made-up read failure")
	f.FailRead(keyB, reading)
	if _, _, err := f.Read(ctx, keyB); !errors.Is(err, reading) {
		t.Fatalf("FailRead: %v", err)
	}
	if _, _, err := f.Read(ctx, keyA); err != nil {
		t.Fatalf("FailRead of another key: %v", err)
	}
	f.FailRead(keyB, nil)
	if _, _, err := f.Read(ctx, keyB); err != nil {
		t.Fatalf("FailRead(nil): %v", err)
	}

	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := f.Sessions(cancelled); !errors.Is(err, context.Canceled) {
		t.Fatalf("Sessions with a cancelled context: %v", err)
	}
	if _, _, err := f.Read(cancelled, keyA); !errors.Is(err, context.Canceled) {
		t.Fatalf("Read with a cancelled context: %v", err)
	}
	if err := ledgertest.Check(cancelled, f); !errors.Is(err, context.Canceled) {
		t.Fatalf("Check with a cancelled context: %v", err)
	}
}

// The fake can be read and failed from several goroutines at once; run
// under the race detector this catches a fake that is not.
func TestFakeConcurrentUse(t *testing.T) {
	f := mustNew(t, sessions()...)
	var wg sync.WaitGroup
	for i := range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 50 {
				if i%2 == 0 {
					f.FailRead(keyB, errors.New("made-up"))
					f.FailRead(keyB, nil)
					f.FailSessions(nil)
				} else {
					_, _ = f.Sessions(context.Background())
					_, _, _ = f.Read(context.Background(), keyA)
				}
			}
		}()
	}
	wg.Wait()
}

var voiceKey = ledger.Key{Host: "codex", Session: "0193c2d1-aaaa-7000-8000-00000000000a", Folder: "general", File: "voice.jsonl"}

func TestReadSession(t *testing.T) {
	s, err := ledgertest.ReadSession(os.DirFS("testdata"), "voice.jsonl", voiceKey, event.ClassChat)
	if err != nil {
		t.Fatal(err)
	}
	if s.Key != voiceKey || s.Class != event.ClassChat || len(s.Events) != 8 {
		t.Fatalf("read %+v with %d events", s.Key, len(s.Events))
	}
	authors := map[event.Author]bool{}
	for _, e := range s.Events {
		authors[e.Author] = true
	}
	if !authors[event.AuthorSpoken] || !authors[event.AuthorFront] {
		t.Fatalf("the fixture's voice hand-over has no spoken line or no front model's: %v", authors)
	}
	f := mustNew(t, s)
	if err := ledgertest.Check(context.Background(), f); err != nil {
		t.Fatalf("a fake read from a fixture breaks the contract: %v", err)
	}
	rec, _, _ := f.Read(context.Background(), voiceKey)
	if rec.Copies != 1 || rec.Withheld != 1 || rec.Wrappers != 1 {
		t.Fatalf("the fixture's record is %+v", rec)
	}

	data, err := os.ReadFile("testdata/voice.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	header, rest, _ := strings.Cut(string(data), "\n")
	refused := map[string]string{
		"a header of another session": strings.Replace(header, "0193c2d1-aaaa", "0193c2d1-bbbb", 1) + "\n" + rest,
		"a header of another host":    strings.Replace(header, `"codex"`, `"claude-code"`, 1) + "\n" + rest,
		"no header":                   rest,
		"an empty file":               "",
		"a line in another form":      header + "\n" + strings.Replace(rest, `"seq":1,`, `"seq": 1,`, 1),
		"an event the codec refuses":  header + "\n" + strings.Replace(rest, `"k":"ctx"`, `"k":"speech"`, 1),
		"an empty line":               header + "\n\n" + rest,
		"an unfinished last line":     strings.TrimSuffix(string(data), "\n"),
	}
	for name, body := range refused {
		fsys := fstest.MapFS{"s.jsonl": {Data: []byte(body)}}
		if _, err := ledgertest.ReadSession(fsys, "s.jsonl", voiceKey, event.ClassChat); err == nil {
			t.Errorf("%s: read", name)
		}
	}
	if _, err := ledgertest.ReadSession(fstest.MapFS{"s.jsonl": {Data: data}}, "s.jsonl", voiceKey, event.ClassChat); err != nil {
		t.Fatalf("the file every case breaks is refused: %v", err)
	}
	if _, err := ledgertest.ReadSession(fstest.MapFS{}, "s.jsonl", voiceKey, event.ClassChat); err == nil {
		t.Error("a file that is not there: read")
	}
	headerOnly := fstest.MapFS{"s.jsonl": {Data: []byte(header + "\n")}}
	if s, err := ledgertest.ReadSession(headerOnly, "s.jsonl", voiceKey, event.ClassUnknown); err != nil || len(s.Events) != 0 {
		t.Fatalf("a session with no event: %d events, %v", len(s.Events), err)
	}
}
