package catchup

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// A recovery record with no transcript says why: no hook named one, a hook
// named one recovery's rules refused, or one they admit that was not
// written yet. A refused one stays refused while later events name none, a
// transcript the rules admit clears it, and an event naming none after that
// keeps the admitted one. A record written before the field was kept says
// nothing, and an event naming none keeps that. The record holds nothing of
// a refused path.
func TestRecordSaysWhyATranscriptIsMissing(t *testing.T) {
	dirs, file, e := fixture(t, "claude-code")
	ctx := context.Background()
	home, cwd := os.Getenv("HOME"), filepath.Join(os.Getenv("HOME"), "project")
	at := time.Date(2026, 10, 29, 11, 0, 0, 0, time.UTC)
	register := func(id, path string) Source {
		t.Helper()
		at = at.Add(time.Minute)
		e := e
		e.Session = id
		if err := Register(ctx, dirs, home, path, cwd, e, false, false, at); err != nil {
			t.Fatal(err)
		}
		s, found, err := Record(ctx, dirs, e.Host, id)
		if err != nil || !found {
			t.Fatal(found, err)
		}
		return s
	}
	if s := register("named-0", ""); !s.Missing || s.MissingWhy != MissingNoneNamed {
		t.Errorf("a record named by no hook: %+v", s)
	}
	outside := filepath.Join(home, "elsewhere", "tok-refused-path", "named-1.jsonl")
	s := register("named-1", outside)
	if !s.Missing || s.MissingWhy != MissingRefused || s.Path != "" {
		t.Errorf("a refused transcript: %+v", s)
	}
	data, err := os.ReadFile(filepath.Join(dirs.State, "catchup", "claude-code", "named-1.json"))
	if err != nil || strings.Contains(string(data), "tok-refused-path") {
		t.Errorf("the record holds %s (%v)", data, err)
	}
	if s := register("named-1", ""); !s.Missing || s.MissingWhy != MissingRefused {
		t.Errorf("an event naming none after a refusal: %+v", s)
	}
	admitted := filepath.Join(filepath.Dir(file), "named-1.jsonl")
	if err := os.WriteFile(admitted, nil, 0600); err != nil {
		t.Fatal(err)
	}
	if s := register("named-1", admitted); s.Missing || s.MissingWhy != "" || s.Path == "" {
		t.Errorf("an admitted transcript after a refusal: %+v", s)
	}
	if s := register("named-1", ""); s.Missing || s.MissingWhy != "" || s.Path == "" {
		t.Errorf("an event naming none after an admitted transcript: %+v", s)
	}

	// A chat whose first hooks run before the host writes its transcript.
	unwritten := filepath.Join(filepath.Dir(file), "named-2.jsonl")
	if s := register("named-2", unwritten); !s.Missing || s.MissingWhy != MissingNotWritten || s.Path != "" {
		t.Errorf("a transcript not written yet: %+v", s)
	}
	if s := register("named-2", ""); !s.Missing || s.MissingWhy != MissingNotWritten {
		t.Errorf("an event naming none after one not written: %+v", s)
	}
	// In a folder of chats not made yet, too.
	if s := register("named-3", filepath.Join(filepath.Dir(file)+"-new", "named-3.jsonl")); s.MissingWhy != MissingNotWritten {
		t.Errorf("a transcript in a folder not made yet: %+v", s)
	}
	// A path the rules admit that is not a regular file is refused.
	if err := os.Mkdir(filepath.Join(filepath.Dir(file), "named-4.jsonl"), 0700); err != nil {
		t.Fatal(err)
	}
	if s := register("named-4", filepath.Join(filepath.Dir(file), "named-4.jsonl")); s.MissingWhy != MissingRefused {
		t.Errorf("a folder in the transcript's place: %+v", s)
	}

	// A record from before the field.
	older := filepath.Join(dirs.State, "catchup", "claude-code", "named-5.json")
	old := Source{V: 1, Home: home, Host: "claude-code", Session: "named-5", Cwd: cwd, Entry: e, Missing: true, Reason: "transcript_unavailable", Updated: at.Format(time.RFC3339)}
	old.Entry.Session = "named-5"
	data, err = json.Marshal(old)
	if err != nil || bytes.Contains(data, []byte("missing_why")) {
		t.Fatalf("%s %v", data, err)
	}
	if err := os.WriteFile(older, data, 0600); err != nil {
		t.Fatal(err)
	}
	if s := register("named-5", ""); !s.Missing || s.MissingWhy != "" {
		t.Errorf("an event naming none after an older record: %+v", s)
	}
}

// Admits and OpenTranscript judge a transcript as recovery does: a path
// outside the host's folder of chats, or another session's file, is refused
// by its path alone; the registered one opens; one put in its place since,
// or cut shorter than recovery's cursor, is refused once opened; a Codex
// file whose first record names another session is refused once opened.
func TestOpenTranscriptKeepsRecoverysRules(t *testing.T) {
	for _, host := range []string{"claude-code", "codex"} {
		dirs, file, e := fixture(t, host)
		s, found, err := Record(context.Background(), dirs, host, e.Session)
		if err != nil || !found || !Admits(s) {
			t.Fatalf("%s: %v %v %v", host, found, err, Admits(s))
		}
		f, err := OpenTranscript(s)
		if err != nil {
			t.Fatalf("%s: %v", host, err)
		}
		f.Close()
		for _, path := range []string{filepath.Join(os.Getenv("HOME"), filepath.Base(file)), filepath.Join(filepath.Dir(file), "other.jsonl")} {
			bad := s
			bad.Path = path
			if Admits(bad) {
				t.Errorf("%s admits %s", host, path)
			}
			if f, err := OpenTranscript(bad); err == nil {
				f.Close()
				t.Errorf("%s opens %s", host, path)
			}
		}
		if host == "codex" {
			other := s
			other.Cwd = filepath.Join(os.Getenv("HOME"), "another")
			if !Admits(other) {
				t.Fatal("codex: a path the rules admit is refused")
			}
			if f, err := OpenTranscript(other); err == nil {
				f.Close()
				t.Error("codex: a transcript of another folder opens")
			}
		}
		// Recovery's cursor past the file's end: cut shorter.
		info, err := os.Stat(file)
		if err != nil {
			t.Fatal(err)
		}
		cut := s
		cut.Offset = info.Size() + 1
		if f, err := OpenTranscript(cut); err == nil {
			f.Close()
			t.Errorf("%s opens a transcript shorter than the cursor", host)
		}
		cut = s
		cut.Skip = &rowScan{Start: s.Offset, Pos: info.Size() + 1}
		if f, err := OpenTranscript(cut); err == nil {
			f.Close()
			t.Errorf("%s opens a transcript shorter than a long row's scan", host)
		}
		// Another file put in its place.
		data, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(file+".new", data, 0600); err != nil {
			t.Fatal(err)
		}
		if err := os.Rename(file+".new", file); err != nil {
			t.Fatal(err)
		}
		if f, err := OpenTranscript(s); err == nil {
			f.Close()
			t.Errorf("%s opens a transcript put in the registered one's place", host)
		}
	}
}

// LongRow finds a record control in a user's row of any length fed in
// parts, as recovery's scan does, with the row's time, in any folder; it
// finds none in a row that is not a user's or another session's, and says
// when the row is not JSON it follows.
func TestLongRowFindsARecordControl(t *testing.T) {
	ts := "2026-10-29T10:00:01Z"
	image := map[string]any{"type": "image", "source": map[string]any{"data": strings.Repeat("A", 200<<10)}}
	claude := func(session, typ, words string) map[string]any {
		return map[string]any{"type": typ, "sessionId": session, "timestamp": ts, "cwd": "/somewhere/else",
			"message": map[string]any{"role": typ, "content": []any{map[string]any{"type": "text", "text": words}, image}}}
	}
	s := Source{Host: "claude-code", Session: "session-123", Cwd: "/project"}
	cx := Source{Host: "codex", Session: "session-123", Cwd: "/project"}
	codex := map[string]any{"timestamp": ts, "type": "event_msg", "payload": map[string]any{"type": "user_message", "message": "off the record\n" + strings.Repeat("B", 200<<10)}}
	for _, c := range []struct {
		name          string
		s             Source
		row           any
		raw           string
		control, time string
		ok            bool
	}{
		{"off", s, claude("session-123", "user", "off the record"), "", "off_record", ts, true},
		{"on", s, claude("session-123", "user", "back on the record"), "", "on_record", ts, true},
		{"none", s, claude("session-123", "user", "something else"), "", "", ts, true},
		{"another session's", s, claude("session-9", "user", "off the record"), "", "", ts, true},
		{"the assistant's", s, claude("session-123", "assistant", "off the record"), "", "", ts, true},
		{"codex", cx, codex, "", "off_record", ts, true},
		{"not JSON", s, nil, `{"type": "user", "message": ` + strings.Repeat("[", 10) + "x", "", "", false},
		{"unfinished", s, nil, `{"type": "user", "message": {"role": "user", "content": "off the record`, "", "", false},
	} {
		data := []byte(c.raw)
		if c.row != nil {
			var err error
			if data, err = json.Marshal(c.row); err != nil {
				t.Fatal(err)
			}
		}
		var l LongRow
		for len(data) > 0 {
			n := min(len(data), 4096)
			l.Feed(data[:n])
			data = data[n:]
		}
		control, at, ok := l.Control(c.s)
		if control != c.control || at != c.time || ok != c.ok {
			t.Errorf("%s: %q %q %v", c.name, control, at, ok)
		}
	}
}
