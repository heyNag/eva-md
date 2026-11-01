package inbox

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// walkFile walks a session file of the general folder from from, with no
// limit, and returns the texts and line numbers it yielded and where it
// stopped.
func walkFile(t *testing.T, s Store, host, sid string, from Position) ([]string, Position) {
	t.Helper()
	file := s.Path("general", host, sid)
	root, err := os.OpenRoot(filepath.Dir(file))
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	f, _, err := OpenSession(root, filepath.Base(file))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var got []string
	pos, err := Walk(context.Background(), f, "general", filepath.Base(file), from, 0, func(l Line) bool {
		got = append(got, l.Text+"@"+strconv.Itoa(l.Line))
		return true
	})
	if err != nil {
		t.Fatal(err)
	}
	return got, pos
}

// appendRaw writes lines to a session file as they are, unchecked.
func appendRaw(t *testing.T, file string, lines ...string) {
	t.Helper()
	f, err := os.OpenFile(file, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := f.WriteString(strings.Join(lines, "")); err != nil {
		t.Fatal(err)
	}
}

func raw(t *testing.T, e Entry) string {
	t.Helper()
	b, err := json.Marshal(e)
	if err != nil {
		t.Fatal(err)
	}
	return string(b) + "\n"
}

// Walk yields the prompts, replies, tool and plan lines that belong to their
// file and were said on the record, with physical line numbers, and passes
// over events, entries forged for another file, malformed and over-long
// lines and an unterminated last line.
func TestWalkYieldsWhatSearchCanMatch(t *testing.T) {
	s := fixture(t)
	add := func(role, kind, text string) {
		e := Entry{Host: "codex", Session: "walk-session", Project: "general", Role: role, Kind: kind, Text: text, Origin: "hook"}
		if err := s.Append(e); err != nil {
			t.Fatal(err)
		}
	}
	add("user", "prompt", "first prompt")         // 1
	add("assistant", "reply", "first reply")      // 2
	add("tool", "tool", "Bash: go test (exit 0)") // 3
	add("plan", "plan", "[ ] write the walker")   // 4
	add("event", "event", "session_start")        // 5
	add("host", "host_summary", "a summary")      // 6
	add("event", "event", "off_record")           // 7
	add("user", "prompt", "PRIVATE words")        // 8
	add("event", "event", "on_record")            // 9
	add("user", "prompt", "back on the record")   // 10
	file := s.Path("general", "codex", "walk-session")
	forged := Entry{Host: "codex", Session: "another-session", Project: "general", Role: "user", Kind: "prompt", Text: "forged session", Origin: "hook"}
	wrongProject := Entry{Host: "codex", Session: "walk-session", Project: "github.com__team__other", Role: "user", Kind: "prompt", Text: "forged project", Origin: "hook"}
	formatChar := Entry{Host: "codex", Session: "walk‮session", Project: "general", Role: "user", Kind: "prompt", Text: "format character", Origin: "hook"}
	forgedOff := Entry{Host: "cursor", Session: "walk-session", Project: "general", Role: "event", Kind: "event", Text: "off_record", Origin: "hook"}
	long := Entry{Host: "codex", Session: "walk-session", Project: "general", Role: "user", Kind: "prompt", Text: strings.Repeat("x", MaxLine), Origin: "hook"}
	appendRaw(t, file, raw(t, forged), raw(t, wrongProject), raw(t, formatChar), raw(t, forgedOff), "{not json\n", raw(t, long)) // 11-16
	add("assistant", "reply", "after the forged lines")                                                                          // 17
	appendRaw(t, file, `{"v":1,"host":"codex","session":"walk-session","project":"general","role":"user","kind":"prompt","text":"unterminated"}`)
	got, pos := walkFile(t, s, "codex", "walk-session", Position{})
	want := []string{"first prompt@1", "first reply@2", "Bash: go test (exit 0)@3", "[ ] write the walker@4", "back on the record@10", "after the forged lines@17"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("yielded %q", got)
	}
	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if end := int64(strings.LastIndexByte(string(data), '\n') + 1); pos != (Position{Offset: end, Line: 17}) {
		t.Fatalf("stopped at %+v, not after line 17 at %d", pos, end)
	}
}

// A walk resumed from a position numbers lines and keeps the off-the-record
// state as one walk from the start does, and a partial line a killed writer
// left, which the next append terminates, is numbered as eva-md show
// numbers it.
func TestWalkResumesFromAPosition(t *testing.T) {
	s := fixture(t)
	add := func(kind, text string) {
		role := map[string]string{"prompt": "user", "event": "event"}[kind]
		if err := s.Append(Entry{Host: "claude-code", Session: "resume-walk", Project: "general", Role: role, Kind: kind, Text: text, Origin: "hook"}); err != nil {
			t.Fatal(err)
		}
	}
	add("prompt", "one")
	add("event", "off_record")
	_, first := walkFile(t, s, "claude-code", "resume-walk", Position{})
	if first.Line != 2 || !first.Off {
		t.Fatalf("first pass stopped at %+v", first)
	}
	add("prompt", "PRIVATE two")
	add("event", "on_record")
	add("prompt", "three")
	// A killed writer's partial line: the next walk leaves it, and the next
	// append terminates it before a gap event.
	file := s.Path("general", "claude-code", "resume-walk")
	appendRaw(t, file, `{"v":1,"text":"cut off`)
	got, second := walkFile(t, s, "claude-code", "resume-walk", first)
	if strings.Join(got, "|") != "three@5" || second.Line != 5 || second.Off {
		t.Fatalf("second pass yielded %q and stopped at %+v", got, second)
	}
	add("prompt", "four")
	got, third := walkFile(t, s, "claude-code", "resume-walk", second)
	// Line 6 is the terminated partial line, 7 the gap event, 8 the prompt.
	if strings.Join(got, "|") != "four@8" || third.Line != 8 {
		t.Fatalf("after the repair yielded %q and stopped at %+v", got, third)
	}
	rows, _, err := s.ReadFrom(file, 0)
	if err != nil || len(rows) != 8 || rows[7].Text != "four" {
		t.Fatalf("the inbox reads %d rows: %v", len(rows), err)
	}
	all, _ := walkFile(t, s, "claude-code", "resume-walk", Position{})
	if strings.Join(all, "|") != "one@1|three@5|four@8" {
		t.Fatalf("one walk from the start yielded %q", all)
	}
}

// A limit stops the walk at the first line that reaches it, and visit can
// stop it after any entry; each returns the position after its last line.
func TestWalkStopsAtALimitOrWhenAsked(t *testing.T) {
	s := fixture(t)
	for _, text := range []string{"alpha", "beta", "gamma"} {
		if err := s.Append(Entry{Host: "codex", Session: "limit-walk", Project: "general", Role: "user", Kind: "prompt", Text: text, Origin: "hook"}); err != nil {
			t.Fatal(err)
		}
	}
	file := s.Path("general", "codex", "limit-walk")
	root, err := os.OpenRoot(filepath.Dir(file))
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	f, _, err := OpenSession(root, filepath.Base(file))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var got []string
	pos, err := Walk(context.Background(), f, "general", filepath.Base(file), Position{}, 1, func(l Line) bool { got = append(got, l.Text); return true })
	if err != nil || strings.Join(got, "|") != "alpha" || pos.Line != 1 {
		t.Fatalf("limit 1: %q %+v %v", got, pos, err)
	}
	got = nil
	pos, err = Walk(context.Background(), f, "general", filepath.Base(file), pos, 0, func(l Line) bool { got = append(got, l.Text); return false })
	if err != nil || strings.Join(got, "|") != "beta" || pos.Line != 2 {
		t.Fatalf("stopped by visit: %q %+v %v", got, pos, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Walk(ctx, f, "general", filepath.Base(file), pos, 0, func(Line) bool { return true }); err == nil {
		t.Fatal("a cancelled walk went on")
	}
}

// OpenSession refuses a symlink and a second hard link.
func TestOpenSessionRefusesLinks(t *testing.T) {
	s := fixture(t)
	if err := s.Append(Entry{Host: "codex", Session: "links", Project: "general", Role: "user", Kind: "prompt", Text: "x", Origin: "hook"}); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Dir(s.Path("general", "codex", "links"))
	if err := os.Symlink(filepath.Join(dir, "codex-links.jsonl"), filepath.Join(dir, "codex-symlinked.jsonl")); err != nil {
		t.Fatal(err)
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	if f, _, err := OpenSession(root, "codex-symlinked.jsonl"); err == nil {
		f.Close()
		t.Fatal("a symlink was opened")
	}
	if err := os.Link(filepath.Join(dir, "codex-links.jsonl"), filepath.Join(dir, "codex-hardlinked.jsonl")); err != nil {
		t.Fatal(err)
	}
	if f, _, err := OpenSession(root, "codex-links.jsonl"); err == nil {
		f.Close()
		t.Fatal("a multiply linked file was opened")
	}
}
