package audit

import (
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/heyNag/eva-md/internal/catchup"
	"github.com/heyNag/eva-md/internal/inbox"
	"github.com/heyNag/eva-md/internal/ledger/event"
)

// threadID is the id of the Codex subagent's thread that is not paired, a
// UUID as Codex names its files.
const threadID = "1b2c3d4e-5f6a-4b7c-8d9e-0f1a2b3c4d5e"

// unpairedThread stores a Codex subagent's thread, every line under the
// subagent's id, whose recovery record names no transcript, with a gap, a
// repeat, a voice hand-over and a turn with no stored reply, which a chat's
// file would count. files is how many files of the host's folder of chats
// are named for its id: none, or two, which leave it not paired.
func (f *fixture) unpairedThread(files int) {
	f.t.Helper()
	agent := marker + "-agent"
	for i, day := range []string{"02", "03"}[:files] {
		path := filepath.Join(f.home, ".codex", "sessions", "2026", "11", day, "rollout-2026-11-"+day+"T1"+string(rune('1'+i))+"-00-00-"+threadID+".jsonl")
		writeRows(f.t, path,
			cx(at(20, 0), "session_meta", map[string]any{"id": threadID, "cwd": f.cwd}),
			cx(at(20, 1), "event_msg", map[string]any{"type": "user_message", "message": marker + " thread prompt"}))
	}
	f.register("codex", threadID, "")
	f.store("codex", threadID,
		inbox.Entry{TS: at(20, 0), Role: "event", Kind: "event", Text: "session_start: startup", Agent: agent},
		inbox.Entry{TS: at(20, 1), Turn: "S1", Role: "user", Kind: "prompt", Text: marker + " thread prompt", Agent: agent},
		inbox.Entry{TS: at(20, 2), Turn: "S1", Role: "tool", Kind: "tool", Text: "Bash " + marker, Agent: agent},
		inbox.Entry{TS: at(20, 2), Turn: "S1", Role: "tool", Kind: "tool", Text: "Bash " + marker, Agent: agent},
		inbox.Entry{TS: at(20, 3), Turn: "S1", Role: "event", Kind: "event", Text: "gap: missing final reply", Agent: agent},
		inbox.Entry{TS: at(20, 4), Turn: "S2", Role: "user", Kind: "prompt", Text: "<realtime_delegation>" + marker + "</realtime_delegation>", Agent: agent},
	)
}

// spyTranscripts records the session of each transcript the audit asks to
// open as a recovery record names it.
func spyTranscripts(t *testing.T) *[]string {
	t.Helper()
	var opened []string
	saved := openTranscript
	t.Cleanup(func() { openTranscript = saved })
	openTranscript = func(s catchup.Source) (*os.File, error) {
		opened = append(opened, s.Session)
		return saved(s)
	}
	return &opened
}

// A Codex subagent's thread that is not paired, as no file of the host's
// folder of chats is named for its id or two are, adds nothing to any
// coverage figure, both ways together or each apart, nor to the repeats,
// the gaps, the voice hand-overs, the turns with no stored reply, the
// subagents' threads paired or the rows: two homes that differ only by
// that thread give the same report but for the counts that name it, its
// session and file, its class, and why it was not paired. No host file is
// opened for it, by its id or as a recovery record's transcript.
func TestASubagentsThreadNotPairedIsCountedOnlyAsNotPaired(t *testing.T) {
	without := newFixture(t)
	without.claudeSession()
	without.codexSession()
	want, wantOut := without.run("", 0)
	if want.Recovery.Paired != 2 || want.Coverage == ([kinds]Coverage{}) || want.Gaps == (Gaps{}) {
		t.Fatalf("the home without the thread: recovery %+v, coverage %+v, gaps %+v", want.Recovery, want.Coverage, want.Gaps)
	}

	for _, files := range []int{0, 2} {
		t.Run(map[int]string{0: "no file by its id", 2: "two files by its id"}[files], func(t *testing.T) {
			f := newFixture(t)
			f.claudeSession()
			f.codexSession()
			f.unpairedThread(files)
			opened := spyOpens(t)
			transcripts := spyTranscripts(t)
			got, out := f.run("", 0)
			if len(*opened) != 0 {
				t.Errorf("opened by a session's id %q", *opened)
			}
			// The two sessions paired by their recovery record open their
			// transcripts; the thread opens none.
			if slices.Contains(*transcripts, threadID) || len(*transcripts) != want.Recovery.Paired {
				t.Errorf("opened as a recovery record's transcript %q, want the %d paired sessions' only", *transcripts, want.Recovery.Paired)
			}

			// The counts that name the thread.
			lookup := Lookup{None: 1}
			if files == 2 {
				lookup = Lookup{Many: 1}
			}
			if got.Sessions != want.Sessions+1 || got.Files != want.Files+1 || got.Classes[event.ClassSubagent] != want.Classes[event.ClassSubagent]+1 ||
				got.Recovery.NoneNamed != want.Recovery.NoneNamed+1 || got.Lookup != lookup {
				t.Errorf("the thread's counts: sessions %d, files %d, classes %v, recovery %+v, lookup %+v; without it %d, %d, %v, %+v, %+v",
					got.Sessions, got.Files, got.Classes, got.Recovery, got.Lookup, want.Sessions, want.Files, want.Classes, want.Recovery, want.Lookup)
			}
			// The host's folder of chats holds its two files, both of a
			// session eva-md has a file of.
			codex := slices.IndexFunc(got.Stores, func(s Store) bool { return s.Host == "codex" })
			if codex < 0 || len(got.Stores) != len(want.Stores) {
				t.Fatalf("stores %+v, without the thread %+v", got.Stores, want.Stores)
			}
			if got.Stores[codex].Files != want.Stores[codex].Files+files {
				t.Errorf("the Codex folder of chats holds %d files, want %d", got.Stores[codex].Files, want.Stores[codex].Files+files)
			}

			// Every other figure is the home's without it.
			rest := got
			rest.Sessions, rest.Files = want.Sessions, want.Files
			rest.Classes = map[event.Class]int{}
			for class, n := range got.Classes {
				rest.Classes[class] = n
			}
			rest.Classes[event.ClassSubagent]--
			if rest.Classes[event.ClassSubagent] == 0 {
				delete(rest.Classes, event.ClassSubagent)
			}
			rest.Recovery.NoneNamed--
			rest.Lookup = Lookup{}
			rest.Stores = slices.Clone(got.Stores)
			rest.Stores[codex].Files -= files
			if !reflect.DeepEqual(rest, want) {
				t.Errorf("a subagent's thread not paired changed other figures:\n%+v\nwant\n%+v", rest, want)
			}

			// And in the text, only the lines of those counts differ.
			named := []string{"capture audit: ", "inbox files by what they show: ", "sessions not paired by the recovery record: ",
				"of those named none, not written yet, missing for a reason not kept or gone, looked for by the session's id", "host sessions with no eva-md file on codex: "}
			a, b := strings.Split(wantOut, "\n"), strings.Split(out, "\n")
			if len(a) != len(b) {
				t.Fatalf("the report has %d lines, without the thread %d:\n%s\nwithout it\n%s", len(b), len(a), out, wantOut)
			}
			for i := range a {
				if a[i] != b[i] && !slices.ContainsFunc(named, func(p string) bool { return strings.HasPrefix(b[i], p) }) {
					t.Errorf("the thread changed the line\n%s\nto\n%s", a[i], b[i])
				}
			}
			why := map[int]string{0: "no file by that id 1, more than one 0,", 2: "no file by that id 0, more than one 1,"}[files]
			for _, line := range []string{"capture audit: 3 sessions in 3 inbox files, 0 more files unreadable; 2 paired with the host's own record, 2 by the recovery record and 0 by the session's id\n",
				"subagents' threads paired and not scored, as v1 keeps their lines under the subagent's id: 0 by the recovery record, 0 by the session's id\n", why} {
				if !strings.Contains(out, line) {
					t.Errorf("the report lacks %q:\n%s", line, out)
				}
			}
			if strings.Contains(out, threadID) || strings.Contains(out, marker) {
				t.Errorf("the report names the thread:\n%s", out)
			}
		})
	}
}
