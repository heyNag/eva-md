package ledgertest_test

import (
	"context"
	"errors"
	"fmt"
	"go/parser"
	"go/token"
	"io/fs"
	"math/rand/v2"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/heyNag/eva-md/internal/ledger"
	"github.com/heyNag/eva-md/internal/ledger/event"
	"github.com/heyNag/eva-md/internal/ledger/ledgertest"
)

// breaker is a reader that wraps the fake and changes what it gives:
// list changes the n-th listing, read the n-th read of a key, both from 1.
type breaker struct {
	f     *ledgertest.Fake
	list  func(n int, keys []ledger.Key) []ledger.Key
	read  func(n int, k ledger.Key, s *event.Session, events []event.Event) ([]event.Event, error)
	lists int
	reads map[ledger.Key]int
}

func (b *breaker) Sessions(ctx context.Context) ([]ledger.Key, error) {
	keys, err := b.f.Sessions(ctx)
	b.lists++
	if err == nil && b.list != nil {
		keys = b.list(b.lists, keys)
	}
	return keys, err
}

func (b *breaker) Read(ctx context.Context, k ledger.Key) (event.Session, []event.Event, error) {
	s, events, err := b.f.Read(ctx, k)
	if b.reads == nil {
		b.reads = map[ledger.Key]int{}
	}
	b.reads[k]++
	if err == nil && b.read != nil {
		events, err = b.read(b.reads[k], k, &s, events)
	}
	return s, events, err
}

var madeUp = errors.New("made-up failure")

// For each clause of Check, a reader that breaks that one clause makes
// Check fail, and the fault names the clause, the session and the seq.
func TestCheckFindsEachClauseBroken(t *testing.T) {
	type broken struct {
		list   func(n int, keys []ledger.Key) []ledger.Key
		read   func(n int, k ledger.Key, s *event.Session, events []event.Event) ([]event.Event, error)
		clause ledgertest.Clause
		key    ledger.Key
		seq    int64
	}
	// onA changes keyA's n-th read and every later one, from the first
	// when n is 1.
	onA := func(from int, change func(s *event.Session, events []event.Event) []event.Event) func(int, ledger.Key, *event.Session, []event.Event) ([]event.Event, error) {
		return func(n int, k ledger.Key, s *event.Session, events []event.Event) ([]event.Event, error) {
			if k == keyA && n >= from {
				events = change(s, events)
			}
			return events, nil
		}
	}
	keyC := ledger.Key{Host: "codex", Session: "55556666-cccc", Folder: "two", File: "c.jsonl"}
	cases := map[string]broken{
		"a read that fails": {read: func(_ int, k ledger.Key, _ *event.Session, events []event.Event) ([]event.Event, error) {
			if k == keyB {
				return nil, madeUp
			}
			return events, nil
		}, clause: ledgertest.ClauseRead, key: keyB},
		"a record of another host": {read: onA(1, func(s *event.Session, ev []event.Event) []event.Event { s.Host = "codex"; return ev }),
			clause: ledgertest.ClauseRead, key: keyA},
		"a record of another session": {read: onA(1, func(s *event.Session, ev []event.Event) []event.Event { s.ID = "11112222-zzzz"; return ev }),
			clause: ledgertest.ClauseRead, key: keyA},
		"a class that is not valid": {read: onA(1, func(s *event.Session, ev []event.Event) []event.Event { s.Class = "person"; return ev }),
			clause: ledgertest.ClauseRead, key: keyA},
		"a key listed twice": {list: func(_ int, keys []ledger.Key) []ledger.Key { return append(keys, keyA) },
			clause: ledgertest.ClauseRead, key: keyA},

		"a count of events": {read: onA(1, func(s *event.Session, ev []event.Event) []event.Event { s.Events++; return ev }),
			clause: ledgertest.ClauseCounts, key: keyA},
		"a count of copies": {read: onA(1, func(s *event.Session, ev []event.Event) []event.Event { s.Copies = 0; return ev }),
			clause: ledgertest.ClauseCounts, key: keyA},
		"a count of withheld events": {read: onA(1, func(s *event.Session, ev []event.Event) []event.Event { s.Withheld = 2; return ev }),
			clause: ledgertest.ClauseCounts, key: keyA},
		"a count of wrappers": {read: onA(1, func(s *event.Session, ev []event.Event) []event.Event { s.Wrappers = 0; return ev }),
			clause: ledgertest.ClauseCounts, key: keyA},

		"seqs that fall": {read: onA(1, func(_ *event.Session, ev []event.Event) []event.Event { ev[3], ev[4] = ev[4], ev[3]; return ev }),
			clause: ledgertest.ClauseSeqs, key: keyA, seq: 5},
		"a seq given twice": {read: onA(1, func(_ *event.Session, ev []event.Event) []event.Event { ev[4].Seq = 5; return ev }),
			clause: ledgertest.ClauseSeqs, key: keyA, seq: 5},
		"a seq of 0": {read: func(_ int, k ledger.Key, _ *event.Session, events []event.Event) ([]event.Event, error) {
			if k == keyB {
				events[0].Seq = 0
			}
			return events, nil
		}, clause: ledgertest.ClauseSeqs, key: keyB},
		"an event that does not validate": {read: onA(1, func(_ *event.Session, ev []event.Event) []event.Event { ev[3].Text = "said off the record"; return ev }),
			clause: ledgertest.ClauseSeqs, key: keyA, seq: 5},

		"a copy with another key": {read: onA(1, func(_ *event.Session, ev []event.Event) []event.Event { ev[2].Src.Key = "k:3"; return ev }),
			clause: ledgertest.ClauseCopies, key: keyA, seq: 3},
		"a copy in another turn": {read: onA(1, func(_ *event.Session, ev []event.Event) []event.Event { ev[2].Turn = 2; return ev }),
			clause: ledgertest.ClauseCopies, key: keyA, seq: 3},
		"a copy of a copy": {read: onA(1, func(_ *event.Session, ev []event.Event) []event.Event {
			c := ev[2]
			c.Seq, c.Copy = 4, 3
			return slices.Insert(ev, 3, c)
		}), clause: ledgertest.ClauseCopies, key: keyA, seq: 4},
		"a copy of an event the session does not hold": {read: onA(1, func(_ *event.Session, ev []event.Event) []event.Event {
			ev[4].Copy, ev[4].Src.Key = 4, "k:2"
			return ev
		}), clause: ledgertest.ClauseCopies, key: keyA, seq: 6},
		"two events that are not copies with one key": {read: onA(1, func(_ *event.Session, ev []event.Event) []event.Event { ev[2].Copy = 0; return ev }),
			clause: ledgertest.ClauseCopies, key: keyA, seq: 3},

		"a second listing without a session": {list: func(n int, keys []ledger.Key) []ledger.Key {
			if n >= 2 {
				return keys[:1]
			}
			return keys
		}, clause: ledgertest.ClauseResolve, key: keyB},
		"a second listing with another session": {list: func(n int, keys []ledger.Key) []ledger.Key {
			if n >= 2 {
				return append(keys, keyC)
			}
			return keys
		}, clause: ledgertest.ClauseResolve, key: keyC},
		"a second read that fails": {read: func(n int, k ledger.Key, _ *event.Session, events []event.Event) ([]event.Event, error) {
			if k == keyA && n == 2 {
				return nil, madeUp
			}
			return events, nil
		}, clause: ledgertest.ClauseResolve, key: keyA},
		"a second read with another record": {read: onA(2, func(s *event.Session, ev []event.Event) []event.Event { s.Class = event.ClassUnknown; return ev }),
			clause: ledgertest.ClauseResolve, key: keyA},
		"a second read with fewer events": {read: onA(2, func(_ *event.Session, ev []event.Event) []event.Event { return ev[:4] }),
			clause: ledgertest.ClauseResolve, key: keyA},
		"a second read with another event": {read: onA(2, func(_ *event.Session, ev []event.Event) []event.Event { ev[4].Text = "another hand-over"; return ev }),
			clause: ledgertest.ClauseResolve, key: keyA, seq: 6},
		"a read for Resolve with another event": {read: onA(3, func(_ *event.Session, ev []event.Event) []event.Event { ev[0].Text = "another prompt"; return ev }),
			clause: ledgertest.ClauseResolve, key: keyA, seq: 1},
		"a listing for Resolve without the session": {list: func(n int, keys []ledger.Key) []ledger.Key {
			if n >= 3 {
				return keys[1:]
			}
			return keys
		}, clause: ledgertest.ClauseResolve, key: keyA, seq: 1},
		"a listing for Resolve that makes it ambiguous": {list: func(n int, keys []ledger.Key) []ledger.Key {
			if n >= 3 {
				return append(keys, keyA)
			}
			return keys
		}, clause: ledgertest.ClauseResolve, key: keyA, seq: 1},
	}
	ctx := context.Background()
	if err := ledgertest.Check(ctx, &breaker{f: mustNew(t, sessions()...)}); err != nil {
		t.Fatalf("the reader every case breaks fails the check: %v", err)
	}
	for name, c := range cases {
		err := ledgertest.Check(ctx, &breaker{f: mustNew(t, sessions()...), list: c.list, read: c.read})
		var f *ledgertest.Fault
		if !errors.As(err, &f) {
			t.Errorf("%s: %v, not a fault", name, err)
			continue
		}
		if f.Clause != c.clause || f.Key != c.key || f.Seq != c.seq {
			t.Errorf("%s: %v names %s, %s #%d; want %s, %s #%d", name, err, f.Clause, f.Key.Session, f.Seq, c.clause, c.key.Session, c.seq)
		}
		msg := err.Error()
		if !strings.Contains(msg, string(c.clause)) || !strings.Contains(msg, c.key.Session) || c.seq != 0 && !strings.Contains(msg, "#"+strconv.FormatInt(c.seq, 10)) {
			t.Errorf("%s: %q does not name the clause, the session and the seq", name, msg)
		}
	}

	// A read that fails is the fault's cause.
	err := ledgertest.Check(ctx, &breaker{f: mustNew(t, sessions()...), read: cases["a read that fails"].read})
	if !errors.Is(err, madeUp) {
		t.Errorf("a read that fails: %v", err)
	}
}

// A context that ends, or a listing that fails, ends the check with its
// error, at once or part way.
func TestCheckEnds(t *testing.T) {
	f := mustNew(t, sessions()...)
	ctx, cancel := context.WithCancel(context.Background())
	b := &breaker{f: f, read: func(n int, k ledger.Key, _ *event.Session, events []event.Event) ([]event.Event, error) {
		if k == keyB && n == 2 {
			cancel()
		}
		return events, nil
	}}
	if err := ledgertest.Check(ctx, b); !errors.Is(err, context.Canceled) {
		t.Errorf("a context cancelled part way: %v", err)
	}
	b = &breaker{f: f, list: func(n int, keys []ledger.Key) []ledger.Key {
		if n == 1 {
			f.FailSessions(madeUp)
		}
		return keys
	}}
	if err := ledgertest.Check(context.Background(), b); !errors.Is(err, madeUp) {
		t.Errorf("a listing that fails part way: %v", err)
	}
	if err := ledgertest.Check(context.Background(), f); !errors.Is(err, madeUp) {
		t.Errorf("a listing that fails at once: %v", err)
	}
}

// A session kept in two folders, and two sessions whose references name
// both, pass the check, and are what lets Resolve be ambiguous; a line only
// one of them holds as one that decodes resolves to it.
func TestCheckAllowsAmbiguityOnlyWhereTheRecordHasIt(t *testing.T) {
	ctx := context.Background()
	prompt := func(seq int64, text string) event.Event {
		return event.Event{Seq: seq, Turn: 1, Kind: event.KindPrompt, Author: event.AuthorV1User, Text: text, Src: event.Source{Key: "k:" + strconv.FormatInt(seq, 10)}}
	}
	repaired := event.Event{Seq: 2, Turn: 1, Kind: event.KindGap, Author: event.AuthorEva, Text: "gap: malformed inbox record", Src: event.Source{Key: "k:2"}, Marks: []event.Mark{event.MarkRepaired, event.MarkName}}
	one := ledger.Key{Host: "codex", Session: "abcdefgh-1", Folder: "one"}
	two := ledger.Key{Host: "codex", Session: "abcdefgh-1", Folder: "two"}
	short := ledger.Key{Host: "codex", Session: "abcdefgh", Folder: "one"}
	long := ledger.Key{Host: "codex", Session: "abcdefgh" + strings.Repeat("x", 200), Folder: "one"}
	f := mustNew(t,
		ledgertest.Session{Key: one, Class: event.ClassUnknown, Events: []event.Event{prompt(1, "in one"), prompt(2, "one: second")}},
		ledgertest.Session{Key: two, Class: event.ClassUnknown, Events: []event.Event{prompt(1, "in two"), repaired, prompt(3, "two: third")}},
		ledgertest.Session{Key: short, Class: event.ClassUnknown, Events: []event.Event{prompt(5, "short")}},
		ledgertest.Session{Key: long, Class: event.ClassUnknown, Events: []event.Event{prompt(5, "long")}},
	)
	if err := ledgertest.Check(ctx, f); err != nil {
		t.Fatal(err)
	}
	for ref, want := range map[string]string{"e:abcdefgh-1#2": "one: second", "e:abcdefgh-1#3": "two: third"} {
		if _, e, err := ledger.Resolve(ctx, f, ref); err != nil || e.Text != want {
			t.Errorf("%s: %q, %v", ref, e.Text, err)
		}
	}
	for _, ref := range []string{"e:abcdefgh-1#1", "e:abcdefgh#5"} {
		if _, _, err := ledger.Resolve(ctx, f, ref); !errors.Is(err, ledger.ErrAmbiguous) {
			t.Errorf("%s: %v, want it ambiguous", ref, err)
		}
	}
}

// Whatever New accepts passes Check: random sessions with copies, withheld
// and repaired lines, in folders and with ids whose references name more
// than one of them.
func TestWhatNewAcceptsPassesCheck(t *testing.T) {
	ids := []string{"abcdefgh", "abcdefgh-1", "abcdefgh-2", "zz-9", "abcdefgh" + strings.Repeat("y", 130)}
	ambiguous := 0
	for seed := uint64(1); seed <= 300; seed++ {
		r := rand.New(rand.NewPCG(seed, 0x1f01b))
		var ss []ledgertest.Session
		seen := map[ledger.Key]bool{}
		for range 1 + r.IntN(4) {
			k := ledger.Key{Host: []string{"codex", "claude-code"}[r.IntN(2)], Session: ids[r.IntN(len(ids))], Folder: []string{"one", "two"}[r.IntN(2)]}
			if seen[k] {
				continue
			}
			seen[k] = true
			ss = append(ss, ledgertest.Session{Key: k, Class: event.ClassUnknown, Events: randomEvents(r)})
		}
		f, err := ledgertest.New(ss...)
		if err != nil {
			t.Fatalf("seed %d: %v", seed, err)
		}
		if err := ledgertest.Check(context.Background(), f); err != nil {
			t.Fatalf("seed %d: %v", seed, err)
		}
		for _, s := range ss {
			for _, e := range s.Events {
				ref := event.ID{Host: s.Key.Host, Session: s.Key.Session, Seq: e.Seq}.Ref()
				if _, _, err := ledger.Resolve(context.Background(), f, ref); errors.Is(err, ledger.ErrAmbiguous) {
					ambiguous++
				}
			}
		}
	}
	if ambiguous == 0 {
		t.Fatal("no reference was ambiguous, so the check's allowance went untried")
	}
}

func randomEvents(r *rand.Rand) []event.Event {
	var events []event.Event
	var seq int64
	turn := 0
	for range r.IntN(9) {
		seq += 1 + int64(r.IntN(2))
		turn += r.IntN(2)
		key := "k:" + strconv.FormatInt(seq, 10)
		var e event.Event
		switch r.IntN(5) {
		case 0:
			e = event.Event{Kind: event.KindGap, Author: event.AuthorEva, Why: []string{event.WhyOffRecord, event.WhyUntrusted}[r.IntN(2)],
				Src: event.Source{Key: event.PlaceKey(seq)}, Marks: []event.Mark{event.MarkWithheld}}
		case 1:
			e = event.Event{Turn: turn, Kind: event.KindGap, Author: event.AuthorEva, Text: "gap: malformed inbox record", Src: event.Source{Key: key}, Marks: []event.Mark{event.MarkRepaired, event.MarkName}}
		case 2:
			var firsts []event.Event
			for _, x := range events {
				if x.Kind == event.KindReply && x.Copy == 0 {
					firsts = append(firsts, x)
				}
			}
			if len(firsts) > 0 {
				e = firsts[r.IntN(len(firsts))]
				e.Copy, e.Marks = e.Seq, []event.Mark{event.MarkRecovered}
				break
			}
			fallthrough
		default:
			e = event.Event{Turn: turn, TID: fmt.Sprintf("t%d", turn), Kind: event.KindReply, Author: event.AuthorAgent, Text: fmt.Sprintf("made-up reply %d", r.IntN(3)), Src: event.Source{Key: key}}
		}
		e.Seq = seq
		if e.Has(event.MarkWithheld) {
			e.Turn = turn
		}
		events = append(events, e)
	}
	return events
}

// Only tests import the package: a file that is built into eva-md and
// imports it fails here.
func TestOnlyTestsImportIt(t *testing.T) {
	const self = "github.com/heyNag/eva-md/internal/ledger/ledgertest"
	root, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Fatalf("the module's root is not %s: %v", root, err)
	}
	here, err := filepath.Abs(".")
	if err != nil {
		t.Fatal(err)
	}
	files, byTests := 0, 0
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if path != root && (strings.HasPrefix(d.Name(), ".") || d.Name() == "testdata") {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		files++
		f, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
		if err != nil {
			return err
		}
		for _, imp := range f.Imports {
			if p, _ := strconv.Unquote(imp.Path.Value); p != self {
				continue
			}
			switch {
			case strings.HasSuffix(path, "_test.go"):
				byTests++
			case filepath.Dir(path) != here:
				t.Errorf("%s imports the fake, and is not a test", path)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if files < 100 || byTests == 0 {
		t.Fatalf("%d Go files read, %d tests import the fake: the walk missed the module", files, byTests)
	}
}
