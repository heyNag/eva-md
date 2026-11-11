package audit

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/heyNag/eva-md/internal/catchup"
	"github.com/heyNag/eva-md/internal/inbox"
)

// A session is paired with its host's record only where its recovery record
// names one recovery's rules admit; otherwise it is counted by why. A
// transcript the rules refuse by its path, and that of a chat off the record
// now, is never opened, though the file is there; the one gone is tried and
// the one paired opened.
func TestRecoveryRulesDecideWhatIsOpened(t *testing.T) {
	f := newFixture(t)
	var opened []string
	saved := openTranscript
	t.Cleanup(func() { openTranscript = saved })
	openTranscript = func(s catchup.Source) (*os.File, error) {
		opened = append(opened, s.Session)
		return saved(s)
	}
	ctx := context.Background()
	sessions := map[string]func(id string){
		"no-record": func(string) {},
		"none-named": func(id string) {
			f.register("claude-code", id, "")
		},
		"named-refused": func(id string) {
			outside := filepath.Join(f.home, "elsewhere", id+".jsonl")
			writeRows(t, outside, cUser("N", at(0, 1), marker))
			f.register("claude-code", id, outside)
		},
		"refused-now": func(id string) {
			// A record whose path the rules refuse now, as a folder of chats
			// moved since leaves one, written as recovery writes a record.
			f.register("claude-code", id, f.transcript("claude-code", id, cUser("R", at(0, 1), marker)))
			s, _, err := catchup.Record(ctx, f.dirs, "claude-code", id)
			if err != nil {
				t.Fatal(err)
			}
			s.Path = filepath.Join(f.home, "outside", id+".jsonl")
			writeRows(t, s.Path, cUser("R", at(0, 1), marker))
			data, err := json.Marshal(s)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(f.dirs.State, "catchup", "claude-code", id+".json"), data, 0o600); err != nil {
				t.Fatal(err)
			}
		},
		"gone": func(id string) {
			path := f.transcript("claude-code", id, cUser("G", at(0, 1), marker))
			f.register("claude-code", id, path)
			if err := os.Remove(path); err != nil {
				t.Fatal(err)
			}
		},
		"off-now": func(id string) {
			f.register("claude-code", id, f.transcript("claude-code", id, cUser("O", at(0, 1), marker)))
			if err := catchup.SetOffRecord(ctx, f.dirs, "claude-code", id, true); err != nil {
				t.Fatal(err)
			}
		},
		"unreadable": func(id string) {
			dir := filepath.Join(f.dirs.State, "catchup", "claude-code")
			if err := os.MkdirAll(dir, 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, id+".json"), []byte("{"), 0o600); err != nil {
				t.Fatal(err)
			}
		},
		"paired": func(id string) {
			f.register("claude-code", id, f.transcript("claude-code", id, cUser("P", at(0, 1), marker)))
		},
	}
	for name, make := range sessions {
		id := marker + "-" + name
		make(id)
		f.store("claude-code", id, inbox.Entry{TS: at(0, 0), Role: "event", Kind: "event", Text: "session_start"})
	}
	r, _ := f.run("", 0)
	want := Recovery{Paired: 1, NoRecord: 1, NoneNamed: 1, NamedRefused: 1, Refused: 1, Gone: 1, OffNow: 1, Unreadable: 1}
	if r.Recovery != want {
		t.Errorf("recovery %+v, want %+v", r.Recovery, want)
	}
	slices.Sort(opened)
	if !slices.Equal(opened, []string{marker + "-gone", marker + "-paired"}) {
		t.Errorf("opened %q", opened)
	}
}

// A Codex transcript whose first record names another folder is opened and
// refused, as recovery refuses it, and counted as refused.
func TestCodexIdentityIsChecked(t *testing.T) {
	f := newFixture(t)
	f.transcript("codex", codexID, cx(at(0, 0), "session_meta", map[string]any{"id": codexID, "cwd": filepath.Join(f.home, "another")}),
		cx(at(0, 1), "event_msg", map[string]any{"type": "user_message", "message": marker}))
	f.register("codex", codexID, filepath.Join(f.home, ".codex", "sessions", "2026", "11", "02", "rollout-2026-11-02T10-00-00-"+codexID+".jsonl"))
	f.store("codex", codexID, inbox.Entry{TS: at(0, 0), Role: "event", Kind: "event", Text: "session_start"})
	r, _ := f.run("", 0)
	if r.Recovery != (Recovery{Refused: 1}) || len(r.Rows) != 0 {
		t.Errorf("recovery %+v, rows %+v", r.Recovery, r.Rows)
	}
}

// writeRows writes rows as a JSON Lines file at path.
func writeRows(t *testing.T, path string, rows ...any) {
	t.Helper()
	var data []byte
	for _, r := range rows {
		b, err := json.Marshal(r)
		if err != nil {
			t.Fatal(err)
		}
		data = append(append(data, b...), '\n')
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
}
