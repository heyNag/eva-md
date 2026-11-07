package inbox

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

// put writes a file of the test's inbox, with its folders.
func put(t *testing.T, file, text string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(file), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
}

// Captures lists every session file and every watermark whose file is
// gone, by project folder and name, and nothing else: no watermark of a file
// that is there, no file of another name, no temporary file, and nothing at
// the inbox's root or in a dot folder.
func TestCapturesListsFilesAndLeftWatermarks(t *testing.T) {
	s := fixture(t)
	acme := filepath.Join(s.Root, "acme")
	put(t, filepath.Join(acme, "claude-code-aaa.jsonl"), "{}\n{}\n")
	put(t, filepath.Join(acme, ".state", "claude-code-aaa.jsonl.wm"), "3\n")
	put(t, filepath.Join(acme, "codex-bbb.jsonl"), "{}\n")
	put(t, filepath.Join(acme, ".state", "codex-gone.jsonl.wm"), "120\n")
	put(t, filepath.Join(acme, ".state", ".tmp-eva-0123"), "")
	put(t, filepath.Join(acme, "notes.txt"), "x")
	put(t, filepath.Join(acme, "other-ccc.jsonl"), "{}\n")
	put(t, filepath.Join(s.Root, "general", "codex-ddd.jsonl"), "{}\n{}\n{}\n")
	put(t, filepath.Join(s.Root, ".hidden", "codex-eee.jsonl"), "{}\n")
	put(t, filepath.Join(s.Root, ".baseline"), "2026-01-01T00:00:00Z\n")
	put(t, filepath.Join(s.Root, "codex-fff.jsonl"), "{}\n")
	old := time.Date(2026, 1, 14, 10, 0, 0, 0, time.Local)
	if err := os.Chtimes(filepath.Join(acme, ".state", "codex-gone.jsonl.wm"), old, old); err != nil {
		t.Fatal(err)
	}
	captures, unread, err := s.Captures()
	if err != nil || len(unread) != 0 {
		t.Fatal(err, unread)
	}
	var got []string
	for _, c := range captures {
		got = append(got, c.Project+"/"+c.Name())
		switch c.Name() {
		case "claude-code-aaa.jsonl":
			if !c.File || c.Size != 6 || c.Host != "claude-code" || c.ID != "aaa" {
				t.Fatalf("%+v", c)
			}
		case "codex-gone.jsonl":
			if c.File || c.Size != 0 || c.Host != "codex" || c.ID != "gone" || !c.Mod.Equal(old) {
				t.Fatalf("%+v", c)
			}
		}
	}
	want := []string{"acme/claude-code-aaa.jsonl", "acme/codex-bbb.jsonl", "acme/codex-gone.jsonl", "general/codex-ddd.jsonl"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("captures %q", got)
	}
	if captures, unread, err := (Store{Root: filepath.Join(s.Root, "missing")}).Captures(); err != nil || captures != nil || unread != nil {
		t.Fatalf("a missing inbox: %v %v %v", captures, unread, err)
	}
}

// Remove takes the session file, then its watermark, and leaves the rest of
// the folder. When the watermark can't go, the file is gone already and the
// watermark is listed on its own, for the next removal to take; a capture
// gone already is no error, and one whose folder or name could reach
// outside the inbox is refused. Tidy removes a folder left empty, and only
// then.
func TestRemoveTakesTheFileThenItsWatermark(t *testing.T) {
	s := fixture(t)
	acme := filepath.Join(s.Root, "acme")
	put(t, filepath.Join(acme, "claude-code-aaa.jsonl"), "{}\n")
	put(t, filepath.Join(acme, ".state", "claude-code-aaa.jsonl.wm"), "3\n")
	put(t, filepath.Join(acme, "codex-bbb.jsonl"), "{}\n")
	put(t, filepath.Join(acme, ".state", "codex-bbb.jsonl.wm"), "3\n")
	state := filepath.Join(acme, ".state")
	if err := os.Chmod(state, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(state, 0o700) })
	aaa := Capture{Project: "acme", Host: "claude-code", ID: "aaa", File: true}
	if err := s.Remove(aaa); err == nil {
		t.Fatal("a watermark that can't be removed was no error")
	}
	captures, _, err := s.Captures()
	if err != nil || len(captures) != 2 || captures[0].Name() != "claude-code-aaa.jsonl" || captures[0].File {
		t.Fatalf("after a cut short removal: %+v %v", captures, err)
	}
	if err := os.Chmod(state, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := s.Remove(captures[0]); err != nil {
		t.Fatal(err)
	}
	for _, gone := range []string{filepath.Join(acme, "claude-code-aaa.jsonl"), filepath.Join(state, "claude-code-aaa.jsonl.wm")} {
		if _, err := os.Lstat(gone); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("%s: %v", gone, err)
		}
	}
	for _, kept := range []string{filepath.Join(acme, "codex-bbb.jsonl"), filepath.Join(state, "codex-bbb.jsonl.wm")} {
		if _, err := os.Lstat(kept); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.Remove(aaa); err != nil {
		t.Fatalf("a capture gone already: %v", err)
	}
	for _, bad := range []Capture{{Project: "..", Host: "codex", ID: "bbb"}, {Project: "acme/..", Host: "codex", ID: "bbb"}, {Project: ".state", Host: "codex", ID: "bbb"}, {Project: "acme", Host: "codex", ID: "../acme/codex-bbb"}, {Project: "acme", Host: "other", ID: "bbb"}} {
		if err := s.Remove(bad); err == nil {
			t.Fatalf("%+v was removed", bad)
		}
	}
	s.Tidy("acme")
	if _, err := os.Lstat(acme); err != nil {
		t.Fatalf("a folder holding a session went: %v", err)
	}
	if err := s.Remove(Capture{Project: "acme", Host: "codex", ID: "bbb", File: true}); err != nil {
		t.Fatal(err)
	}
	s.Tidy("acme")
	if _, err := os.Lstat(acme); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("an empty folder stayed: %v", err)
	}
}

// The record of removals gets a line for each capture, 0600, read back in
// order; a line a killed writer left unfinished is ended by the next
// append and passed over, as is a line that doesn't decode; a missing
// record lists none, and a link in its place is refused.
func TestRecordOfRemovals(t *testing.T) {
	s := fixture(t)
	if got, err := s.Removed(); err != nil || got != nil {
		t.Fatalf("no record: %v %v", got, err)
	}
	at := time.Date(2026, 11, 7, 10, 0, 0, 0, time.UTC)
	first := []Capture{{Project: "acme", Host: "codex", ID: "aaa"}, {Project: "general", Host: "claude-code", ID: "bbb"}}
	if err := s.RecordRemoved(context.Background(), at, first); err != nil {
		t.Fatal(err)
	}
	record := filepath.Join(s.Root, ".removed")
	f, err := os.OpenFile(record, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString("not json\n" + `{"v":1,"at":"2026-11-07T10:00:00Z","host":"codex","session":"cut`); err != nil {
		t.Fatal(err)
	}
	f.Close()
	if err := s.RecordRemoved(context.Background(), at, []Capture{{Project: "acme", Host: "codex", ID: "ccc"}}); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(record)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("%v %v", info, err)
	}
	got, err := s.Removed()
	if err != nil {
		t.Fatal(err)
	}
	want := []Removal{{1, "2026-11-07T10:00:00Z", "codex", "aaa"}, {1, "2026-11-07T10:00:00Z", "claude-code", "bbb"}, {1, "2026-11-07T10:00:00Z", "codex", "ccc"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("record %+v", got)
	}
	if err := s.RecordRemoved(context.Background(), at, []Capture{{Project: "../x", Host: "codex", ID: "ddd"}}); err == nil {
		t.Fatal("a capture outside the inbox was recorded")
	}
	other := fixture(t)
	if err := os.MkdirAll(other.Root, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(record, filepath.Join(other.Root, ".removed")); err != nil {
		t.Fatal(err)
	}
	if err := other.RecordRemoved(context.Background(), at, first); err == nil {
		t.Fatal("a link took the record")
	}
	if _, err := other.Removed(); err == nil {
		t.Fatal("a link was read as the record")
	}
}

// SessionID gives the id the hooks were given, which the file's first entry
// carries, also when the file's name holds it changed; a file whose first
// line doesn't belong to it, or a watermark alone, gives the id of its name.
func TestSessionIDIsTheHooksID(t *testing.T) {
	s := fixture(t)
	e := entry()
	e.Session = "abc:def"
	if err := s.Append(e); err != nil {
		t.Fatal(err)
	}
	captures, _, err := s.Captures()
	if err != nil || len(captures) != 1 || captures[0].ID == "abc:def" || !strings.HasPrefix(captures[0].ID, "abc_def-") {
		t.Fatalf("%+v %v", captures, err)
	}
	if id := s.SessionID(captures[0]); id != "abc:def" {
		t.Fatalf("id %q", id)
	}
	put(t, filepath.Join(s.Root, "general", "codex-xyz.jsonl"), `{"v":1,"host":"codex","session":"other","project":"general","role":"event","kind":"event","origin":"hook"}`+"\n")
	if id := s.SessionID(Capture{Project: "general", Host: "codex", ID: "xyz", File: true}); id != "xyz" {
		t.Fatalf("a line of another session gave %q", id)
	}
	if id := s.SessionID(Capture{Project: "general", Host: "codex", ID: "gone"}); id != "gone" {
		t.Fatalf("a watermark alone gave %q", id)
	}
}
