package ledger

import (
	"context"
	"errors"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/heyNag/eva-md/internal/ledger/event"
)

// fake is a Reader over sessions held in memory.
type fake struct {
	keys   []Key
	events map[Key][]event.Event
	err    error
}

func (f fake) Sessions(context.Context) ([]Key, error) { return f.keys, f.err }

func (f fake) Read(_ context.Context, k Key) (event.Session, []event.Event, error) {
	return event.Session{Host: k.Host, ID: k.Session, Folder: k.Folder}, f.events[k], nil
}

func TestResolve(t *testing.T) {
	a := Key{Host: "codex", Session: "11112222-aaaa", Folder: "one"}
	b := Key{Host: "claude-code", Session: "33334444-bbbb", Folder: "one"}
	c := Key{Host: "claude-code", Session: "33334444-cccc", Folder: "two"}
	ev := func(seq int64, text string) event.Event {
		return event.Event{Seq: seq, Kind: event.KindReply, Author: event.AuthorAgent, Text: text, Src: event.Source{Key: "k" + strconv.FormatInt(seq, 10)}}
	}
	r := fake{keys: []Key{a, b, c}, events: map[Key][]event.Event{
		a: {ev(1, "a1"), ev(2, "a2")},
		b: {ev(1, "b1")},
		c: {ev(1, "c1")},
	}}
	for ref, want := range map[string]string{
		"e:11112222#2":      "a2",
		"s:11112222#1":      "a1",
		"e:11112222-aaaa#2": "a2",
		"e:11112222#1-2":    "a1",
		"s:33334444-cccc#1": "c1",
	} {
		_, e, err := Resolve(context.Background(), r, ref)
		if err != nil || e.Text != want {
			t.Errorf("%s resolves to %q, %v; want %q", ref, e.Text, err, want)
		}
	}
	for ref, want := range map[string]error{
		"e:11112222#3":   ErrNotFound,
		"e:99999999#1":   ErrNotFound,
		"e:1111222#1":    ErrNotFound, // seven characters name no session
		"e:33334444#1":   ErrAmbiguous,
		"s:33334444#1":   ErrAmbiguous,
		"e:33334444-b#1": ErrNotFound,
	} {
		if _, _, err := Resolve(context.Background(), r, ref); !errors.Is(err, want) {
			t.Errorf("%s: %v, want %v", ref, err, want)
		}
	}
	if _, _, err := Resolve(context.Background(), r, "x:11112222#1"); err == nil || errors.Is(err, ErrNotFound) {
		t.Errorf("an invalid reference: %v", err)
	}
	failing := fake{err: errors.New("cannot list")}
	if _, _, err := Resolve(context.Background(), failing, "e:11112222#1"); err == nil || errors.Is(err, ErrNotFound) {
		t.Errorf("a reader that cannot list: %v", err)
	}
}

// The ledger packages import only the standard library and eva-md's own
// packages, so they bring no new module, and only eva's name is shown in
// their text: no library's.
func TestOnlyEvasNameAndNoNewModule(t *testing.T) {
	library := "o" + "kf" // spelled apart, so this file does not hold it
	files := 0
	err := filepath.WalkDir(".", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		files++
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if strings.Contains(strings.ToLower(string(data)), library) {
			t.Errorf("%s names a library", path)
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		f, err := parser.ParseFile(token.NewFileSet(), path, data, parser.ImportsOnly)
		if err != nil {
			return err
		}
		for _, imp := range f.Imports {
			p, _ := strconv.Unquote(imp.Path.Value)
			first, _, _ := strings.Cut(p, "/")
			if strings.Contains(first, ".") && !strings.HasPrefix(p, "github.com/heyNag/eva-md/") {
				t.Errorf("%s imports %s", path, p)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if files < 10 {
		t.Fatalf("only %d files were read", files)
	}
}

// The spec stays short enough to be read whole.
func TestSpecSize(t *testing.T) {
	info, err := os.Stat("SPEC.md")
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() > 24<<10 {
		t.Fatalf("SPEC.md is %d bytes, over 24 KB", info.Size())
	}
}
