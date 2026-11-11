package audit

import (
	"context"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/heyNag/eva-md/internal/inbox"
	"github.com/heyNag/eva-md/internal/ledger/event"
	"github.com/heyNag/eva-md/internal/session"
)

// notScored is the report a home of subagents' threads gives, one on each
// host of hosts, newest first, each paired the way way says and none of
// them scored: they are counted as sessions, files, a class, their pairing
// and their ranges said off the record, and in nothing else. The host
// folders' counts are taken from got, as they are not decided here.
func notScored(got Report, way, pausedInbox, pausedHost int, hosts ...string) Report {
	n := len(hosts)
	want := Report{Sessions: n, Files: n, Classes: map[event.Class]int{event.ClassSubagent: n},
		PausedInbox: pausedInbox, PausedHost: pausedHost, Stores: got.Stores}
	want.Subagents[way] = n
	if way == ByID {
		want.Recovery.NoneNamed = n
	}
	for _, h := range hosts {
		want.Rows = append(want.Rows, Row{Host: h, Way: way, Subagent: true})
	}
	return want
}

// A Codex subagent's thread, which v1 stores with the subagent's id on
// every line and Codex keeps as a session of its own, is paired by its
// recovery record or by its id, and read, but not scored: it adds nothing
// to any coverage figure, both ways together or each apart, nor to the
// repeats, the gaps, the voice hand-overs, the host's rows not compared or
// the turns with no stored reply, and it is counted on a line of its own
// by the way it was paired, its own line saying what it is with no figures.
// The same session with the subagent's id taken off its lines is scored
// as before. One off the record once whose range neither side shows is
// read and not paired.
func TestASubagentsThreadIsPairedAndNotScored(t *testing.T) {
	agent := marker + "-agent"
	scored := newFixture(t)
	scored.codexSession()
	want, _ := scored.run("", 0)
	for _, way := range []int{ByRecord, ByID} {
		t.Run(wayNames[way], func(t *testing.T) {
			f := newFixture(t)
			f.codexThread(agent)
			// A gap and a repeat, which a chat's file would count.
			f.store("codex", codexID, inbox.Entry{TS: at(3, 2), Turn: "U4", Role: "event", Kind: "event", Text: "gap: missing final reply", Agent: agent},
				inbox.Entry{TS: at(3, 3), Turn: "U4", Role: "tool", Kind: "tool", Text: "Bash " + marker, Agent: agent},
				inbox.Entry{TS: at(3, 3), Turn: "U4", Role: "tool", Kind: "tool", Text: "Bash " + marker, Agent: agent})
			if way == ByID {
				f.unname("codex", codexID)
			}
			opened := spyOpens(t)
			r, out := f.run("", 0)
			if way == ByID && !slices.Equal(*opened, []string{codexID + " true"}) {
				t.Errorf("opened %q", *opened)
			}
			if w := notScored(r, way, 0, 0, "codex"); !reflect.DeepEqual(r, w) {
				t.Errorf("a subagent's thread is scored:\n%+v\nwant\n%+v", r, w)
			}
			for _, line := range []string{
				"capture audit: 1 sessions in 1 inbox files, 0 more files unreadable; 0 paired with the host's own record, 0 by the recovery record and 0 by the session's id\n",
				"  prompts: 0 found, 0 missing, 0 extra\n",
				"  session 1 (codex, paired " + wayNames[way] + "): a subagent's thread, not scored\n",
				"inbox files by what they show: a subagent's 1,",
			} {
				if !strings.Contains(out, line) {
					t.Errorf("the report lacks %q:\n%s", line, out)
				}
			}
			counts := map[int]string{ByRecord: "1 by the recovery record, 0 by the session's id\n", ByID: "0 by the recovery record, 1 by the session's id\n"}[way]
			if !strings.Contains(out, "subagents' threads paired and not scored, as v1 keeps their lines under the subagent's id: "+counts) {
				t.Errorf("the report does not count the thread %s:\n%s", wayNames[way], out)
			}

			// The same session with no subagent's id on its lines is scored.
			g := newFixture(t)
			g.codexThread("")
			if way == ByID {
				g.unname("codex", codexID)
			}
			r, _ = g.run("", 0)
			if way == ByID {
				r = byRecord(t, r, 1)
			}
			if !reflect.DeepEqual(r, want) {
				t.Errorf("with no subagent's id, the session is scored otherwise:\n%+v\n%+v", r, want)
			}

			// Off the record once, with its range shown on neither side: read
			// and not paired.
			h := newFixture(t)
			h.codexThread(agent)
			if way == ByID {
				h.unname("codex", codexID)
			}
			if err := (session.Markers{Root: h.dirs.Sessions()}).SetContext(context.Background(), "codex", codexID, session.WasOff, "1"); err != nil {
				t.Fatal(err)
			}
			r, _ = h.run("", 0)
			unplaced := map[int]int{ByRecord: r.Recovery.OffUnplaced, ByID: r.Lookup.OffUnplaced}[way]
			if unplaced != 1 || r.Subagents != [ways]int{} || len(r.Rows) != 0 || r.Coverage != [kinds]Coverage{} {
				t.Errorf("off the record once: recovery %+v, lookup %+v, subagents %v, rows %+v", r.Recovery, r.Lookup, r.Subagents, r.Rows)
			}
		})
	}
}

// A range said off the record in subagents' threads, paired either way, is
// honoured: two homes that differ only within their ranges, whichever side
// shows them, give the same report, which counts each range once, as
// paused, and nothing else of the threads.
func TestNothingOffTheRecordIsReadInASubagentsThread(t *testing.T) {
	agent := marker + "-agent"
	for _, way := range []int{ByRecord, ByID} {
		t.Run(wayNames[way], func(t *testing.T) {
			var reports [2]Report
			for i, inside := range []bool{false, true} {
				f := newFixture(t)
				pausedThreads(f, inside, agent)
				if way == ByID {
					f.unname("claude-code", claudeID)
					f.unname("codex", codexID)
				}
				reports[i], _ = f.run("", 0)
				reports[i].Stores = nil
			}
			if !reflect.DeepEqual(reports[0], reports[1]) {
				t.Errorf("what was said off the record changed the report:\n%+v\n%+v", reports[0], reports[1])
			}
			r := reports[1]
			if w := notScored(r, way, 1, 1, "codex", "claude-code"); !reflect.DeepEqual(r, w) {
				t.Errorf("the threads are scored, or a range is not counted once:\n%+v\nwant\n%+v", r, w)
			}
		})
	}
}

// A Claude Code chat that holds a subagent's lines among its own is not a
// subagent's thread: it is scored, as TestClaudeCounts gives it, paired
// either way.
func TestAChatWithASubagentsLinesIsScored(t *testing.T) {
	for _, way := range []int{ByRecord, ByID} {
		f := newFixture(t)
		f.claudeSession()
		if way == ByID {
			f.unname("claude-code", claudeID)
		}
		r, out := f.run("", 0)
		want := [kinds]Coverage{
			Prompt: {Found: 5, Missing: 0, Extra: 3},
			Reply:  {Found: 3, Missing: 1, Extra: 0},
			Tool:   {Found: 3, Missing: 1, Extra: 1},
		}
		if r.Coverage != want || r.ByWay[way] != want || len(r.Rows) != 1 || r.Rows[0].Subagent || r.Subagents != [ways]int{} || r.Classes[event.ClassSubagent] != 0 {
			t.Errorf("%s: coverage %+v, rows %+v, subagents %v, classes %v", wayNames[way], r.Coverage, r.Rows, r.Subagents, r.Classes)
		}
		if strings.Contains(out, "a subagent's thread, not scored") {
			t.Errorf("%s: the chat is said to be a subagent's thread:\n%s", wayNames[way], out)
		}
	}
}
