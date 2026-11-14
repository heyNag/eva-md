package health

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/heyNag/eva-md/internal/ledger/event"
	"github.com/heyNag/eva-md/internal/paths"
	"github.com/heyNag/eva-md/internal/update"
	"github.com/heyNag/eva-md/internal/wikigit"
	"github.com/heyNag/eva-md/internal/work"
)

// The counted losses, the verdict, and that eva-md status with the switch
// health on writes nothing, starts no host command and shows no captured
// text.

// fakeWaits is what the fake job's Waiting gives, set by a test.
var fakeWaits struct {
	sync.Mutex
	items []work.Waiting
}

// The fake job stands in for the update job's reason, so a test can give
// work.Status each of its six reasons.
func init() {
	work.Register(work.Job{Kind: "healthfake", Priority: 9,
		Run: func(context.Context, []work.Mark, io.Writer) work.Outcome { return work.Done },
		Waiting: func(context.Context, time.Time) []work.Waiting {
			fakeWaits.Lock()
			defer fakeWaits.Unlock()
			return append([]work.Waiting(nil), fakeWaits.items...)
		}})
}

// fakeWait makes the fake job's Waiting give items until the test ends.
func fakeWait(t *testing.T, items ...work.Waiting) {
	t.Helper()
	fakeWaits.Lock()
	fakeWaits.items = items
	fakeWaits.Unlock()
	t.Cleanup(func() {
		fakeWaits.Lock()
		fakeWaits.items = nil
		fakeWaits.Unlock()
	})
}

// healthyHost makes host's capture rows pass: eva-md installed for it 12
// hours ago, an empty chat folder, and a Stop hook run a minute ago whose
// week holds runs hook runs, held of them holding back the prompt and
// briefs of them a brief.
func healthyHost(t *testing.T, d paths.Dirs, host string, now time.Time, runs, held, briefs int) {
	t.Helper()
	installedFor(t, d, host, now)
	h := HookDay{Day: weekDay(now, 0), Runs: runs, Behind: []int{held, 0, 0, briefs}}.fit()
	putWeek(t, d, host, "Stop", now.Add(-time.Minute), []HookDay{h})
	for _, folder := range []string{".claude/projects", ".codex/sessions"} {
		if err := os.MkdirAll(filepath.Join(os.Getenv("HOME"), folder), 0o700); err != nil {
			t.Fatal(err)
		}
	}
}

// captureRows are the rows a counted loss of host may not leave PASS.
func captureRows(host string) []string {
	return []string{host + " capture lag", host + " capture freshness", host + " hook failures", "hooks held back"}
}

// One event held back of 199 hook runs is past the bar of 0.5%: none of
// that host's capture rows is PASS, its losses row fails, and the loss
// leads the verdict. One of 200 is at the bar, and changes nothing. A
// brief held back is no capture loss.
func TestHeldBackLossPastItsBar(t *testing.T) {
	now := time.Now()
	for _, tc := range []struct {
		runs, held, briefs int
		past               bool
	}{{199, 1, 0, true}, {200, 1, 0, false}, {100, 0, 5, false}} {
		d, _ := stageHome(t)
		healthyHost(t, d, update.HostClaude, now, tc.runs, tc.held, tc.briefs)
		healthyHost(t, d, update.HostCodex, now, 50, 0, 0)
		r := statusAt(t, d, now)
		for _, name := range captureRows(update.HostClaude) {
			row := findRow(t, r, name)
			if (row.Status == "PASS") == tc.past {
				t.Errorf("%d held of %d runs: %+v", tc.held, tc.runs, row)
			}
		}
		for _, name := range captureRows(update.HostCodex)[:3] {
			if row := findRow(t, r, name); row.Status != "PASS" {
				t.Errorf("the other host's row %+v", row)
			}
		}
		losses := findRow(t, r, update.HostClaude+" capture losses")
		wantDetail := fmt.Sprintf("in the past 7 days %d %s held back or kept as a name only in %d hook runs (%s)", tc.held, plural(tc.held, "event was", "events were"), tc.runs, share(tc.held, tc.runs))
		if !strings.HasPrefix(losses.Detail, wantDetail) || (losses.Status == "FAIL") != tc.past {
			t.Errorf("losses row %+v, want it to start %q", losses, wantDetail)
		}
		if leads := strings.HasPrefix(r.Verdict, "Claude Code capture is losing chats: "); leads != tc.past {
			t.Errorf("%d held of %d runs: verdict %q", tc.held, tc.runs, r.Verdict)
		}
	}
}

// turnChat is n closed turns of a session, a reply to each but the first,
// and one turn still open after them, which is not counted.
func turnChat(t *testing.T, host, id string, now time.Time, n int) []byte {
	t.Helper()
	data := chatLines(t, host, id, now.Add(-time.Hour), n, true)
	lines := bytes.SplitAfter(data, []byte{'\n'})
	// The first turn's reply is its second line.
	data = append(lines[0], bytes.Join(lines[2:], nil)...)
	return append(data, chatLines(t, host, id, now.Add(-time.Minute), 1, false)...)
}

// One closed turn with a prompt and no stored reply of 199 is past the
// bar; one of 200 is not; a session last changed 8 days ago is not read.
func TestNoReplyLossPastItsBar(t *testing.T) {
	now := time.Now()
	for _, tc := range []struct {
		turns int
		past  bool
	}{{199, true}, {200, false}} {
		d, _ := stageHome(t)
		healthyHost(t, d, update.HostClaude, now, 50, 0, 0)
		healthyHost(t, d, update.HostCodex, now, 50, 0, 0)
		plantSession(t, d, update.HostCodex, "turns-"+plantedMarker, turnChat(t, update.HostCodex, "turns-"+plantedMarker, now, tc.turns), -1, now.Add(-time.Minute))
		plantSession(t, d, update.HostCodex, "old", turnChat(t, update.HostCodex, "old", now.Add(-8*24*time.Hour), 2), -1, now.Add(-8*24*time.Hour))
		r := statusAt(t, d, now)
		losses := findRow(t, r, update.HostCodex+" capture losses")
		want := fmt.Sprintf("1 of %d closed turns with a prompt have no stored reply (%s)", tc.turns, share(1, tc.turns))
		if !strings.Contains(losses.Detail, want) || (losses.Status == "FAIL") != tc.past {
			t.Fatalf("losses row %+v, want %q", losses, want)
		}
		for _, name := range captureRows(update.HostCodex) {
			if row := findRow(t, r, name); (row.Status == "PASS") == tc.past {
				t.Errorf("%d turns: %+v", tc.turns, row)
			}
		}
		if leads := strings.HasPrefix(r.Verdict, "Codex capture is losing chats: 1 of "); leads != tc.past {
			t.Errorf("%d turns: verdict %q", tc.turns, r.Verdict)
		}
	}
}

// A turn is closed when a later turn opened, or it holds a reply, the stop
// hook's mark of a missing reply, a failed stop, an interrupt or the
// chat's end; a failed or interrupted turn is no loss; a subagent's lines,
// withheld lines and a prompt's second copy open no turn of their own.
func TestTurnLosses(t *testing.T) {
	prompt := func(turn int) event.Event { return event.Event{Turn: turn, Kind: event.KindPrompt} }
	reply := func(turn int) event.Event { return event.Event{Turn: turn, Kind: event.KindReply} }
	life := func(turn int, text string) event.Event {
		return event.Event{Turn: turn, Kind: event.KindLife, Text: text}
	}
	for _, tc := range []struct {
		name            string
		events          []event.Event
		closed, noReply int
	}{
		{"a reply", []event.Event{prompt(1), reply(1)}, 1, 0},
		{"a later turn", []event.Event{prompt(1), prompt(2), reply(2)}, 2, 1},
		{"the last turn, still open", []event.Event{prompt(1)}, 0, 0},
		{"a missing reply", []event.Event{prompt(1), {Turn: 1, Kind: event.KindGap, Text: "gap: missing final reply"}}, 1, 1},
		{"an interrupt", []event.Event{prompt(1), life(1, "interrupt")}, 1, 0},
		{"a failed stop", []event.Event{prompt(1), life(1, "stop_failure:rate_limit")}, 1, 0},
		{"the chat's end", []event.Event{prompt(1), life(1, "session_end:exit")}, 1, 1},
		{"a compaction", []event.Event{prompt(1), life(1, "compact")}, 0, 0},
		{"a subagent", []event.Event{prompt(1), reply(1), {Turn: 2, Kind: event.KindPrompt, Agent: "a1"}, {Turn: 2, Kind: event.KindReply, Agent: "a1"}}, 1, 0},
		{"withheld", []event.Event{prompt(1), reply(1), {Turn: 2, Kind: event.KindGap, Marks: []event.Mark{event.MarkWithheld}}, prompt(3), reply(3)}, 2, 0},
		{"a copy", []event.Event{{Turn: 1, Kind: event.KindPrompt, Copy: 1}, prompt(2), reply(2)}, 1, 0},
		{"before the first prompt", []event.Event{{Turn: 0, Kind: event.KindLife, Text: "session_start"}, prompt(1), reply(1)}, 1, 0},
	} {
		closed, noReply := turnLosses(tc.events)
		if closed != tc.closed || noReply != tc.noReply {
			t.Errorf("%s: %d closed, %d with no reply; want %d and %d", tc.name, closed, noReply, tc.closed, tc.noReply)
		}
	}
}

// The verdict's goldens: every stage within its bar; organizing furthest
// past its bar with each of work.Status's six reasons, from the fake job;
// the commit furthest; a loss leading a stage further past its bar; and
// stages whose lag can't be told.
func TestVerdictGoldens(t *testing.T) {
	now := time.Now()
	at := func(d time.Duration) string { return now.Add(-d).Format(time.RFC3339) }
	var out bytes.Buffer
	verdictOf := func(name string, d paths.Dirs) {
		t.Helper()
		r := statusAt(t, d, now)
		if r.Verdict == "" {
			t.Fatalf("%s: no verdict", name)
		}
		fmt.Fprintf(&out, "%s: %s\n", name, r.Verdict)
	}

	d, _ := stageHome(t)
	inUse(t, d, now)
	verdictOf("within", d)

	for _, reason := range work.Reasons {
		d, _ := stageHome(t)
		inUse(t, d, now)
		plantSession(t, d, update.HostCodex, "waits", chatLines(t, update.HostCodex, "waits", now.Add(-3*time.Hour), 2, true), 0, now)
		fakeWait(t, work.Waiting{Kind: "healthfake", Host: update.HostCodex, Since: now.Add(-3 * time.Hour), Reason: reason, Class: update.ReasonRateLimit, NextTry: now.Add(time.Hour)})
		verdictOf("organizing "+string(reason), d)
	}
	fakeWait(t)

	commitFurthest := func() paths.Dirs {
		d, c := stageHome(t)
		inUse(t, d, now)
		commitFixture(t, c, now)
		plantCommits(t, d, wikigit.Result{At: at(5 * 24 * time.Hour), Outcome: wikigit.Committed, Pages: 1, Push: wikigit.PushOK})
		plantRunLog(t, d, update.Record{At: at(4 * 24 * time.Hour), Host: update.HostCodex, Outcome: update.OutcomeOK, Pages: 1})
		// Organizing waits too, but within its ratio of lag to bar.
		plantSession(t, d, update.HostCodex, "waits", chatLines(t, update.HostCodex, "waits", now.Add(-10*time.Minute), 1, true), 0, now)
		return d
	}
	verdictOf("commit", commitFurthest())

	d = commitFurthest()
	healthyHost(t, d, update.HostClaude, now, 199, 1, 0)
	verdictOf("loss", d)

	d, c := stageHome(t)
	inUse(t, d, now)
	if err := os.RemoveAll(filepath.Join(c.WikiPath, ".git")); err != nil {
		t.Fatal(err)
	}
	verdictOf("unread", d)

	file := filepath.Join("testdata", "verdict.golden")
	if *updateGolden {
		if err := os.WriteFile(file, out.Bytes(), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(out.Bytes(), want) {
		t.Fatalf("verdicts differ from %s:\n--- got\n%s--- want\n%s", file, out.Bytes(), want)
	}
}

// homeState is every file and folder under root: its mode, size,
// modification time and, for a file, a hash of its bytes.
func homeState(t *testing.T, root string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.WalkDir(root, func(path string, e fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := e.Info()
		if err != nil {
			return err
		}
		sum := ""
		if info.Mode().IsRegular() {
			// A chat the host keeps unreadable is compared by its mode,
			// size and time alone.
			if data, err := os.ReadFile(path); err == nil {
				sum = fmt.Sprintf("%x", sha256.Sum256(data))
			}
		}
		out[path] = fmt.Sprintf("%v %d %d %s", info.Mode(), info.Size(), info.ModTime().UnixNano(), sum)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// eva-md status with the switch health on, on a home where every stage
// has something to say, changes no file or folder of the home, the state
// folder and the wiki included; starts neither host's command nor ssh,
// which a fetch or push to the wiki's remote would; and shows no captured
// text in a row or the verdict, though a marker is planted in every entry
// and session id it reads.
func TestHealthStatusWritesNothing(t *testing.T) {
	now := time.Now()
	at := func(d time.Duration) string { return now.Add(-d).Format(time.RFC3339) }
	d, c := stageHome(t)
	home := os.Getenv("HOME")
	healthyHost(t, d, update.HostClaude, now, 199, 1, 0)
	healthyHost(t, d, update.HostCodex, now, 50, 0, 0)
	// Claude Code's hooks last ran two hours ago, before its newest chat.
	putWeek(t, d, update.HostClaude, "Stop", now.Add(-2*time.Hour), []HookDay{HookDay{Day: weekDay(now, 0), Runs: 199, Behind: []int{1, 0, 0, 0}}.fit()})
	chatFolder(t, filepath.Join(home, ".claude", "projects", "a-project"), now.Add(-30*time.Minute))
	plantSession(t, d, update.HostCodex, "turns-"+plantedMarker, turnChat(t, update.HostCodex, "turns-"+plantedMarker, now.Add(-2*time.Hour), 3), 0, now.Add(-time.Minute))
	plantSession(t, d, update.HostClaude, "c-"+plantedMarker, chatLines(t, update.HostClaude, "c-"+plantedMarker, now.Add(-30*time.Hour), 2, false), 0, now.Add(-time.Minute))
	fakeWait(t, work.Waiting{Kind: "healthfake", Host: update.HostClaude, Since: now.Add(-30 * time.Hour), Reason: work.WaitHeld, Class: update.ReasonRateLimit})
	commitFixture(t, c, now)
	gitIn(t, c.WikiPath, "remote", "add", "origin", "ssh://git@example.invalid/wiki.git")
	gitIn(t, c.WikiPath, "add", "-A")
	gitIn(t, c.WikiPath, "commit", "-q", "-m", "pages")
	put(t, filepath.Join(c.WikiPath, "general", "threads", "new.md"), []byte("another page\n"))
	plantCommits(t, d, wikigit.Result{At: at(3 * 24 * time.Hour), Outcome: wikigit.Committed, Pages: 1, Push: wikigit.PushPending})
	plantRunLog(t, d, update.Record{At: at(2 * 24 * time.Hour), Host: update.HostClaude, Outcome: update.OutcomeOK, Pages: 1})

	git, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(home, "bin")
	if err := os.MkdirAll(bin, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(git, filepath.Join(bin, "git")); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"claude", "codex", "ssh"} {
		put(t, filepath.Join(bin, name), []byte("#!/bin/sh\n: > \"$HOME/ran-"+name+"\"\nexit 1\n"))
		if err := os.Chmod(filepath.Join(bin, name), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", bin)

	before := homeState(t, home)
	r := statusAt(t, d, now)
	after := homeState(t, home)
	for path, was := range before {
		if is, ok := after[path]; !ok || is != was {
			t.Errorf("status changed %s: %q, then %q", path, was, is)
		}
	}
	for path := range after {
		if _, ok := before[path]; !ok {
			t.Errorf("status made %s", path)
		}
	}
	// Each kind of stage has something to say on this home.
	for _, name := range []string{update.HostClaude + " capture lag", update.HostClaude + " organizing lag", "commit lag", "push lag"} {
		if row := findRow(t, r, name); !strings.Contains(row.Detail, "past its bar") {
			t.Errorf("%s says nothing past its bar: %+v", name, row)
		}
	}
	if r.Verdict == "" {
		t.Fatal("no verdict")
	}
	raw, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(raw, []byte(plantedMarker)) {
		t.Fatalf("a row or the verdict holds captured text: %s", raw)
	}
}
