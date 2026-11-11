package audit

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/heyNag/eva-md/internal/catchup"
	"github.com/heyNag/eva-md/internal/inbox"
	"github.com/heyNag/eva-md/internal/session"
)

// unname makes the session's recovery record one that names no
// transcript, as a hook whose payload named none leaves it.
func (f *fixture) unname(host, id string) {
	f.t.Helper()
	if err := os.Remove(filepath.Join(f.dirs.State, "catchup", host, id+".json")); err != nil {
		f.t.Fatal(err)
	}
	f.register(host, id, "")
	s, found, err := catchup.Record(context.Background(), f.dirs, host, id)
	if err != nil || !found || !s.Missing || s.MissingWhy != catchup.MissingNoneNamed {
		f.t.Fatalf("%+v %v %v", s, found, err)
	}
}

// spyOpens records each file the audit asks to open by a session's id,
// and whether it was opened.
func spyOpens(t *testing.T) *[]string {
	t.Helper()
	var opened []string
	saved := openUnregistered
	t.Cleanup(func() { openUnregistered = saved })
	openUnregistered = func(s catchup.Source) (*os.File, error) {
		f, err := saved(s)
		opened = append(opened, fmt.Sprintf("%s %v", s.Session, err == nil))
		return f, err
	}
	return &opened
}

// byRecord turns a report of sessions paired by their id into the report
// they give paired by their recovery record, after checking how they were
// paired.
func byRecord(t *testing.T, r Report, paired int) Report {
	t.Helper()
	if r.Lookup != (Lookup{Paired: paired}) || r.Recovery != (Recovery{NoneNamed: paired}) || r.ByWay[ByRecord] != [kinds]Coverage{} {
		t.Errorf("lookup %+v, recovery %+v, by record %+v", r.Lookup, r.Recovery, r.ByWay[ByRecord])
	}
	r.Lookup, r.Recovery = Lookup{}, Recovery{Paired: paired}
	r.ByWay[ByRecord], r.ByWay[ByID] = r.ByWay[ByID], [kinds]Coverage{}
	r.Rows = slices.Clone(r.Rows)
	for i := range r.Rows {
		if r.Rows[i].Way != ByID {
			t.Errorf("row %d paired by %d", i, r.Rows[i].Way)
		}
		r.Rows[i].Way = ByRecord
	}
	return r
}

// A session whose recovery record names no transcript, and whose file is
// found by its id in the host's folder of chats, is paired by its id and
// scored exactly as the same session paired by its recovery record, on
// both hosts' formats, and the report says how each was paired.
func TestASessionPairedByItsIDIsScoredAsByItsRecord(t *testing.T) {
	a := newFixture(t)
	a.claudeSession()
	a.codexSession()
	ra, outA := a.run("", 0)
	b := newFixture(t)
	b.claudeSession()
	b.codexSession()
	b.unname("claude-code", claudeID)
	b.unname("codex", codexID)
	opened := spyOpens(t)
	rb, outB := b.run("", 0)
	if !slices.Equal(*opened, []string{claudeID + " true", codexID + " true"}) {
		t.Errorf("opened %q", *opened)
	}
	if ra.Recovery != (Recovery{Paired: 2}) || len(ra.Rows) != 2 || ra.ByWay[ByRecord] != ra.Coverage {
		t.Fatalf("by record: recovery %+v, rows %+v", ra.Recovery, ra.Rows)
	}
	if rb.ByWay[ByID] != rb.Coverage {
		t.Errorf("by id: coverage %+v, by way %+v", rb.Coverage, rb.ByWay)
	}
	if got := byRecord(t, rb, 2); !reflect.DeepEqual(got, ra) {
		t.Errorf("paired by its id, the session is scored otherwise:\n%+v\n%+v", got, ra)
	}
	for _, want := range []string{"2 paired with the host's own record, 2 by the recovery record and 0 by the session's id", "session 1 (claude-code, paired by its recovery record)"} {
		if !strings.Contains(outA, want) {
			t.Errorf("the report by record lacks %q:\n%s", want, outA)
		}
	}
	for _, want := range []string{"2 paired with the host's own record, 0 by the recovery record and 2 by the session's id", "session 1 (claude-code, paired by its id)", "session 2 (codex, paired by its id)",
		"paired by its id: prompts 9/0/3 final replies 4/2/0 tool calls 5/2/1", "paired by its recovery record: prompts 0/0/0", "by the session's id in the host's folder of chats: paired 2, no file by that id 0"} {
		if !strings.Contains(outB, want) {
			t.Errorf("the report by id lacks %q:\n%s", want, outB)
		}
	}
	// One session, by --session: the folders of chats are listed for it.
	r, _ := b.run(codexID, 0)
	if r.Lookup != (Lookup{Paired: 1}) || len(r.Rows) != 1 || r.Rows[0].Way != ByID || r.Stores != nil {
		t.Errorf("--session: lookup %+v, rows %+v", r.Lookup, r.Rows)
	}
}

// A range said off the record in sessions paired by their id is counted as
// paused, with no kinds, whichever side shows it: two homes that differ only
// within their ranges give the same report, and the same as when the
// sessions are paired by their recovery records.
func TestNothingOffTheRecordIsCountedByID(t *testing.T) {
	recorded := newFixture(t)
	pausedSessions(recorded, true)
	rr, _ := recorded.run("", 0)
	var reports [2]Report
	for i, inside := range []bool{false, true} {
		f := newFixture(t)
		pausedSessions(f, inside)
		f.unname("claude-code", claudeID)
		f.unname("codex", codexID)
		reports[i], _ = f.run("", 0)
		reports[i].Stores = nil
	}
	if !reflect.DeepEqual(reports[0], reports[1]) {
		t.Errorf("what was said off the record changed the report:\n%+v\n%+v", reports[0], reports[1])
	}
	rb := reports[1]
	want := [kinds]Coverage{Prompt: {Found: 4}, Reply: {Found: 4}}
	if rb.Coverage != want || rb.PausedHost != 1 || rb.PausedInbox != 1 || rb.TooLong != 0 {
		t.Errorf("coverage %+v, paused %d and %d, too long %d", rb.Coverage, rb.PausedHost, rb.PausedInbox, rb.TooLong)
	}
	rr.Stores = nil
	if got := byRecord(t, rb, 2); !reflect.DeepEqual(got, rr) {
		t.Errorf("paired by their id, the sessions are scored otherwise:\n%+v\n%+v", got, rr)
	}
}

// Which sessions are looked for by their id, and what is opened. A record
// that names none, one not written yet when it was named, one from before
// eva-md kept why, and one whose transcript is gone are looked for; one
// whose path was refused is not, nor is a chat off the record now. Of those
// looked for, a session with no file by its id, with two, with a file of
// the right name outside the hosts' folders of chats, in a folder linked
// from inside them, or under another home than its record's, and a file
// that is a symlink, are never opened; a Codex file that says it is another
// session's, or another folder's, is opened and refused; one gone before it
// was opened is counted gone; and a chat off the record once whose range
// neither side shows is read and not paired.
func TestWhatIsLookedForByIDAndOpened(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	markers := session.Markers{Root: f.dirs.Sessions()}
	projects := filepath.Join(f.home, ".claude", "projects")
	codexDay := filepath.Join(f.home, ".codex", "sessions", "2026", "11", "02")
	claude := func(id, folder string) string {
		path := filepath.Join(projects, folder, id+".jsonl")
		writeRows(t, path, cUser("T1", at(0, 1), marker))
		return path
	}
	codexMeta := func(id, cwd string) []any {
		return []any{
			cx(at(0, 0), "session_meta", map[string]any{"id": id, "cwd": cwd}),
			cx(at(0, 1), "event_msg", map[string]any{"type": "user_message", "message": marker}),
		}
	}
	codex := func(id, day, clock string, rows ...any) string {
		if rows == nil {
			rows = codexMeta(id, f.cwd)
		}
		path := filepath.Join(filepath.Dir(codexDay), day, "rollout-2026-11-"+day+"T"+clock+"-"+id+".jsonl")
		writeRows(t, path, rows...)
		return path
	}
	uuid := func(n int) string { return fmt.Sprintf("%08x-0000-4000-8000-%012x", n, n) }
	outside := filepath.Join(f.home, marker+"-outside")
	type want struct {
		recovery string // the record's count
		lookup   string // the count by id, "" for none
		opened   string // what openUnregistered gave, "" for not asked
	}
	cases := map[string]struct {
		host string
		make func(id string)
		want want
	}{
		"claude none": {"claude-code", func(id string) { f.register("claude-code", id, "") }, want{"NoneNamed", "None", ""}},
		"claude paired": {"claude-code", func(id string) {
			claude(id, "-"+marker+"-cwd")
			f.register("claude-code", id, "")
		}, want{"NoneNamed", "Paired", "true"}},
		"claude two": {"claude-code", func(id string) {
			claude(id, "-"+marker+"-cwd")
			claude(id, "-"+marker+"-other")
			f.register("claude-code", id, "")
		}, want{"NoneNamed", "Many", ""}},
		// The right name outside the folder of chats, and in a folder too
		// deep in it.
		"claude outside": {"claude-code", func(id string) {
			writeRows(t, filepath.Join(f.home, ".claude", id+".jsonl"), cUser("T1", at(0, 1), marker))
			writeRows(t, filepath.Join(outside, id+".jsonl"), cUser("T1", at(0, 1), marker))
			writeRows(t, filepath.Join(projects, "-"+marker+"-cwd", "deeper", id+".jsonl"), cUser("T1", at(0, 1), marker))
			f.register("claude-code", id, "")
		}, want{"NoneNamed", "None", ""}},
		"claude linked file": {"claude-code", func(id string) {
			target := filepath.Join(outside, "file-"+id+".jsonl")
			writeRows(t, target, cUser("T1", at(0, 1), marker))
			if err := os.MkdirAll(filepath.Join(projects, "-"+marker+"-linked"), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(target, filepath.Join(projects, "-"+marker+"-linked", id+".jsonl")); err != nil {
				t.Fatal(err)
			}
			f.register("claude-code", id, "")
		}, want{"NoneNamed", "Refused", ""}},
		"claude linked folder": {"claude-code", func(id string) {
			dir := filepath.Join(outside, "folder-"+id)
			writeRows(t, filepath.Join(dir, id+".jsonl"), cUser("T1", at(0, 1), marker))
			if err := os.Symlink(dir, filepath.Join(projects, "-"+marker+"-"+id)); err != nil {
				t.Fatal(err)
			}
			f.register("claude-code", id, "")
		}, want{"NoneNamed", "None", ""}},
		// The record's home is another: the file is outside its folder of
		// chats, though inside the audit's.
		"claude another home": {"claude-code", func(id string) {
			claude(id, "-"+marker+"-cwd")
			other := filepath.Join(f.home, marker+"-home")
			if err := os.MkdirAll(other, 0o700); err != nil {
				t.Fatal(err)
			}
			e := inbox.Entry{V: 1, Host: "claude-code", Session: id, Project: marker + "-project"}
			if err := catchup.Register(ctx, f.dirs, other, "", f.cwd, e, false, false, at0()); err != nil {
				t.Fatal(err)
			}
		}, want{"NoneNamed", "Refused", ""}},
		"claude not written": {"claude-code", func(id string) {
			path := filepath.Join(projects, "-"+marker+"-cwd", id+".jsonl")
			f.register("claude-code", id, path)
			writeRows(t, path, cUser("T1", at(0, 1), marker))
		}, want{"NotWritten", "Paired", "true"}},
		"claude older": {"claude-code", func(id string) {
			claude(id, "-"+marker+"-cwd")
			f.register("claude-code", id, "")
			name := filepath.Join(f.dirs.State, "catchup", "claude-code", id+".json")
			data, err := os.ReadFile(name)
			if err != nil {
				t.Fatal(err)
			}
			data = []byte(strings.Replace(string(data), `,"missing_why":"none_named"`, "", 1))
			if strings.Contains(string(data), "missing_why") {
				t.Fatalf("%s", data)
			}
			if err := os.WriteFile(name, data, 0o600); err != nil {
				t.Fatal(err)
			}
		}, want{"WhyUnknown", "Paired", "true"}},
		// The named transcript is gone, and the session's file is in
		// another folder of chats.
		"claude gone": {"claude-code", func(id string) {
			path := claude(id, "-"+marker+"-cwd")
			f.register("claude-code", id, path)
			if err := os.Remove(path); err != nil {
				t.Fatal(err)
			}
			claude(id, "-"+marker+"-moved")
		}, want{"Gone", "Paired", "true"}},
		"claude named and refused": {"claude-code", func(id string) {
			writeRows(t, filepath.Join(outside, "named-"+id+".jsonl"), cUser("T1", at(0, 1), marker))
			f.register("claude-code", id, filepath.Join(outside, "named-"+id+".jsonl"))
			claude(id, "-"+marker+"-cwd")
		}, want{"NamedRefused", "", ""}},
		// Off the record now, by its marker, or by its record alone,
		// whose block keeps no reason once a later event names none.
		"claude off now": {"claude-code", func(id string) {
			claude(id, "-"+marker+"-cwd")
			f.register("claude-code", id, "")
			if err := markers.SetContext(ctx, "claude-code", id, session.OffRecord, "1"); err != nil {
				t.Fatal(err)
			}
		}, want{"OffNow", "", ""}},
		"claude off by its record": {"claude-code", func(id string) {
			claude(id, "-"+marker+"-cwd")
			e := inbox.Entry{V: 1, Host: "claude-code", Session: id, Project: marker + "-project"}
			if err := catchup.Register(ctx, f.dirs, f.home, "", f.cwd, e, true, false, at0()); err != nil {
				t.Fatal(err)
			}
		}, want{"OffNow", "", ""}},
		"claude was off": {"claude-code", func(id string) {
			claude(id, "-"+marker+"-cwd")
			f.register("claude-code", id, "")
			if err := markers.SetContext(ctx, "claude-code", id, session.WasOff, "1"); err != nil {
				t.Fatal(err)
			}
		}, want{"NoneNamed", "", "true"}},
		"codex paired": {"codex", func(id string) {
			codex(id, "02", "10-00-00")
			f.register("codex", id, "")
		}, want{"NoneNamed", "Paired", "true"}},
		"codex none": {"codex", func(id string) {
			writeRows(t, filepath.Join(outside, "rollout-2026-11-02T10-00-00-"+id+".jsonl"), codexMeta(id, f.cwd)...)
			writeRows(t, filepath.Join(codexDay, "rollout-2026-11-02T10-00-00-"+id+".jsonl.bak"), codexMeta(id, f.cwd)...)
			f.register("codex", id, "")
		}, want{"NoneNamed", "None", ""}},
		"codex two": {"codex", func(id string) {
			codex(id, "02", "10-00-00")
			codex(id, "03", "09-00-00")
			f.register("codex", id, "")
		}, want{"NoneNamed", "Many", ""}},
		"codex another session's": {"codex", func(id string) {
			codex(id, "02", "11-00-00", codexMeta(uuid(999), f.cwd)...)
			f.register("codex", id, "")
		}, want{"NoneNamed", "Refused", "false"}},
		"codex another folder's": {"codex", func(id string) {
			codex(id, "02", "12-00-00", codexMeta(id, filepath.Join(f.home, "another"))...)
			f.register("codex", id, "")
		}, want{"NoneNamed", "Refused", "false"}},
		"codex linked file": {"codex", func(id string) {
			target := filepath.Join(outside, "codex-"+id+".jsonl")
			writeRows(t, target, codexMeta(id, f.cwd)...)
			if err := os.MkdirAll(codexDay, 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(target, filepath.Join(codexDay, "rollout-2026-11-02T13-00-00-"+id+".jsonl")); err != nil {
				t.Fatal(err)
			}
			f.register("codex", id, "")
		}, want{"NoneNamed", "Refused", ""}},
	}
	type session struct{ name, host, id string }
	var all []session
	for name, c := range cases {
		id := marker + "-" + strings.ReplaceAll(name, " ", "-")
		if c.host == "codex" {
			id = uuid(len(all) + 1)
		}
		all = append(all, session{name, c.host, id})
	}
	for _, s := range all {
		cases[s.name].make(s.id)
		f.store(s.host, s.id, inbox.Entry{TS: at(0, 0), Role: "event", Kind: "event", Text: "session_start"})
	}
	// Gone between the listing and the open.
	gone := uuid(500)
	codex(gone, "02", "14-00-00")
	f.register("codex", gone, "")
	f.store("codex", gone, inbox.Entry{TS: at(0, 0), Role: "event", Kind: "event", Text: "session_start"})
	var opened []string
	saved := openUnregistered
	t.Cleanup(func() { openUnregistered = saved })
	openUnregistered = func(s catchup.Source) (*os.File, error) {
		if s.Session == gone {
			if err := os.Remove(s.Path); err != nil {
				t.Fatal(err)
			}
		}
		file, err := saved(s)
		opened = append(opened, fmt.Sprintf("%s %v", s.Session, err == nil))
		return file, err
	}
	r, out := f.run("", 0)

	var wantRecovery Recovery
	wantLookup := Lookup{Gone: 1}
	wantRecovery.NoneNamed = 1 // the one gone
	var wantOpened []string
	add := func(v any, field string) {
		if field == "" {
			return
		}
		x := reflect.ValueOf(v).Elem().FieldByName(field)
		if !x.IsValid() {
			t.Fatalf("no count %s", field)
		}
		x.SetInt(x.Int() + 1)
	}
	for _, s := range all {
		w := cases[s.name].want
		add(&wantRecovery, w.recovery)
		add(&wantLookup, w.lookup)
		if w.opened != "" {
			wantOpened = append(wantOpened, s.id+" "+w.opened)
		}
	}
	wantRecovery.OffUnplaced = 1 // the one off the record once
	wantOpened = append(wantOpened, gone+" false")
	if r.Recovery != wantRecovery {
		t.Errorf("recovery %+v, want %+v", r.Recovery, wantRecovery)
	}
	if r.Lookup != wantLookup {
		t.Errorf("lookup %+v, want %+v", r.Lookup, wantLookup)
	}
	slices.Sort(opened)
	slices.Sort(wantOpened)
	if !slices.Equal(opened, wantOpened) {
		t.Errorf("opened %q, want %q", opened, wantOpened)
	}
	if len(r.Rows) != wantLookup.Paired {
		t.Errorf("rows %+v", r.Rows)
	}
	for _, leak := range []string{marker, f.home, "0000-4000-8000"} {
		if strings.Contains(out, leak) {
			t.Errorf("the report holds %q:\n%s", leak, out)
		}
	}
}

// at0 is the time the fixtures' hooks register at.
func at0() time.Time { return time.Date(2026, 11, 2, 9, 0, 0, 0, time.UTC) }
