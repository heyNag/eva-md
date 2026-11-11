package catchup

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// A recovery record with no transcript says whether a hook named one that
// recovery's rules refused, or none was named: a refused one stays refused
// while later events name none, a transcript the rules admit clears it, and
// an event naming none after that keeps the admitted one. The record holds
// nothing of a refused path.
func TestRecordSaysWhetherAMissingTranscriptWasRefused(t *testing.T) {
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
	outside := filepath.Join(home, "elsewhere", "tok-refused-path", "named-1.jsonl")
	if s := register("named-0", ""); !s.Missing || s.Refused {
		t.Errorf("a record named by no hook: %+v", s)
	}
	s := register("named-1", outside)
	if !s.Missing || !s.Refused || s.Path != "" {
		t.Errorf("a refused transcript: %+v", s)
	}
	data, err := os.ReadFile(filepath.Join(dirs.State, "catchup", "claude-code", "named-1.json"))
	if err != nil || strings.Contains(string(data), "tok-refused-path") {
		t.Errorf("the record holds %s (%v)", data, err)
	}
	if s := register("named-1", ""); !s.Missing || !s.Refused {
		t.Errorf("an event naming none after a refusal: %+v", s)
	}
	admitted := filepath.Join(filepath.Dir(file), "named-1.jsonl")
	if err := os.WriteFile(admitted, nil, 0600); err != nil {
		t.Fatal(err)
	}
	if s := register("named-1", admitted); s.Missing || s.Refused || s.Path == "" {
		t.Errorf("an admitted transcript after a refusal: %+v", s)
	}
	if s := register("named-1", ""); s.Missing || s.Refused || s.Path == "" {
		t.Errorf("an event naming none after an admitted transcript: %+v", s)
	}
}

// Admits and OpenTranscript judge a transcript as recovery does: a path
// outside the host's folder of chats, or another session's file, is refused
// by its path alone; the registered one opens; a Codex file whose first
// record names another session is refused once opened.
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
	}
}
