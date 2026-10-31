package update

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/heyNag/eva-md/internal/inbox"
	"github.com/heyNag/eva-md/internal/paths"
	"github.com/heyNag/eva-md/internal/threadpage"
)

// A rest too small to process waits for more entries, and the watermark
// records the file's size then, so the triggers and the brief leave the
// session alone until something is appended. The offset stays, the run log
// gets no record, and a dry run writes nothing.
func TestWaitingRestIsRecordedInTheWatermark(t *testing.T) {
	e := newEnv(t)
	c := newChat("waitrest", start)
	talk(c, 2, "amber")
	done := size(t, e.write(c))
	e.run(c)
	c.user("And the tests? " + filler)
	file := e.write(c)
	var dry bytes.Buffer
	if err := e.job().DryRun(context.Background(), c.host, c.session, &dry); err != nil || e.mark(file) != (inbox.Mark{Offset: done}) {
		t.Fatalf("dry run: %v, watermark %+v", err, e.mark(file))
	}
	s := e.run(c)
	m := e.mark(file)
	if s.Waiting != 1 || len(s.Records) != 0 || m != (inbox.Mark{Offset: done, Wait: size(t, file)}) || m.Pending(size(t, file)) != 0 {
		t.Fatalf("summary %q, watermark %+v of %d bytes\n%s", s.Line(), m, size(t, file), e.log)
	}
	logged, err := readDay(e.dirs.State, "2026-01-14")
	if err != nil || len(logged) != 1 {
		t.Fatalf("run log %+v %v", logged, err)
	}
	// The job leaves the rest alone too, as the triggers do, until something
	// is appended: it doesn't open the file again to find it waiting.
	if s := e.run(c); s.Line() != "nothing pending" {
		t.Fatalf("again: %q\n%s", s.Line(), e.log)
	}
	// A reply makes it pending again; the pass that takes it clears the
	// wait.
	c.assistant("The tests come next. " + filler)
	file = e.write(c)
	if m := e.mark(file); m.Pending(size(t, file)) != size(t, file)-done {
		t.Fatalf("pending %d after the reply", m.Pending(size(t, file)))
	}
	if s := e.run(c); outcomes(s) != "ok" || e.mark(file) != (inbox.Mark{Offset: size(t, file)}) {
		t.Fatalf("outcomes %q, watermark %+v\n%s", outcomes(s), e.mark(file), e.log)
	}
}

// A watermark that can't be read, or is past the end of its file, as after
// the inbox file was deleted and written again, never makes a hook spawn an
// update. The job records it as failed (watermark), under the id the file's
// name holds, with no call, so the doctor names the session.
func TestUnusableWatermarkIsRecorded(t *testing.T) {
	for _, name := range []string{"past the end", "unreadable"} {
		t.Run(name, func(t *testing.T) {
			e := newEnv(t)
			c := newChat("stalemark", start)
			talk(c, 2, "amber")
			file := e.write(c)
			wm := filepath.Join(filepath.Dir(file), ".state", filepath.Base(file)+".wm")
			if err := os.MkdirAll(filepath.Dir(wm), 0o700); err != nil {
				t.Fatal(err)
			}
			mark := strconv.FormatInt(size(t, file)+100, 10) + "\n"
			if name == "unreadable" {
				mark = "garbage\n"
			}
			if err := os.WriteFile(wm, []byte(mark), 0o600); err != nil {
				t.Fatal(err)
			}
			s, err := e.job().Pending(context.Background())
			if err != nil || outcomes(s) != "failed (watermark)" || s.Errors != 1 || calls(e.fake) != 0 {
				t.Fatalf("%q %v, %d calls\n%s", s.Line(), err, calls(e.fake), e.log)
			}
			logged, _ := readDay(e.dirs.State, "2026-01-14")
			if h := Host(logged, HostClaude, e.clock); len(logged) != 1 || logged[0].Calls != 0 || len(h.Watermarks) != 1 || h.Watermarks[0] != "stalemark" {
				t.Fatalf("run log %+v, host state %+v", logged, h)
			}
		})
	}
}

// A closed chat's last exchange reaches the page though it is under 600
// characters: nothing more joins a chat once its session_end is written, and
// the closing exchange is where a decision often lands. Without the
// session_end, the same exchange waits.
func TestClosedChatsLastShortExchangeIsUpdated(t *testing.T) {
	e := newEnv(t)
	c := newChat("closing", start)
	talk(c, 2, "amber")
	e.write(c)
	e.run(c)
	c.user("Go with B?")
	c.assistant("Yes, go with B.")
	e.write(c)
	if s := e.run(c); s.Waiting != 1 || calls(e.fake) != 1 {
		t.Fatalf("open chat: %q, %d calls\n%s", s.Line(), calls(e.fake), e.log)
	}
	c.event("session_end")
	file := e.write(c)
	e.fake.Respond = func(r Request) (Result, error) { return Result{Output: reply(t, r, "cobalt"), Attempts: 1}, nil }
	if s := e.run(c); outcomes(s) != "ok" || s.Waiting != 0 || e.mark(file) != (inbox.Mark{Offset: size(t, file)}) {
		t.Fatalf("closed chat: %q, watermark %+v of %d\n%s", s.Line(), e.mark(file), size(t, file), e.log)
	}
	if input := string(e.fake.Requests()[1].Input); !strings.Contains(input, "Go with B?") || !strings.Contains(input, "Yes, go with B.") {
		t.Fatalf("input:\n%s", input)
	}
	if p, _ := e.page(testPage); p.Now.Summary != "The cobalt work goes on." {
		t.Fatalf("page Now: %+v", p.Now)
	}
}

// A pass that comes at a prompt, as the trigger on a prompt after a gap
// does, takes the entries before it and leaves the prompt waiting for its
// reply, so a chat closed after the answer gets both to the page. Had the
// pass taken the prompt, the reply would be a rest with no prompt, which
// never passes the size check.
func TestPromptAtTheEndWaitsForItsReply(t *testing.T) {
	e := newEnv(t)
	c := newChat("gapclose", start)
	talk(c, 2, "amber")
	burst := size(t, e.write(c))
	c.user("Go with B?")
	file := e.write(c)
	if s := e.run(c); outcomes(s) != "ok" || s.Waiting != 1 || e.mark(file) != (inbox.Mark{Offset: burst, Wait: size(t, file)}) {
		t.Fatalf("at the prompt: %q, watermark %+v\n%s", outcomes(s), e.mark(file), e.log)
	}
	if input := string(e.fake.Requests()[0].Input); strings.Contains(input, "Go with B?") {
		t.Fatalf("the pass took the prompt:\n%s", input)
	}
	c.assistant("Yes, go with B.")
	c.event("session_end")
	file = e.write(c)
	if s := e.run(c); outcomes(s) != "ok" || e.mark(file) != (inbox.Mark{Offset: size(t, file)}) {
		t.Fatalf("closed chat: %q, watermark %+v of %d\n%s", outcomes(s), e.mark(file), size(t, file), e.log)
	}
	if input := string(e.fake.Requests()[1].Input); !strings.Contains(input, "Go with B?") || !strings.Contains(input, "Yes, go with B.") {
		t.Fatalf("input:\n%s", input)
	}
}

// answered leaves out a last prompt with no reply yet and what follows it,
// unless the chat's session_end ends the group or only events come before
// the prompt.
func TestAnsweredLeavesOutAnUnansweredPrompt(t *testing.T) {
	exchange := func(c *chat) { c.user("Plan it."); c.assistant("Planned.") }
	for _, tc := range []struct {
		name string
		chat func(*chat)
		kept int
	}{
		{"a prompt", func(c *chat) { exchange(c); c.user("Go with B?") }, 2},
		{"a prompt and a tool line", func(c *chat) { exchange(c); c.user("Go with B?"); c.tool("Read notes.md (exit 0)") }, 2},
		{"a prompt and its reply", func(c *chat) { exchange(c); c.user("Go with B?"); c.assistant("Yes.") }, 4},
		{"a prompt and the session_end", func(c *chat) { exchange(c); c.user("Go with B?"); c.event("session_end") }, 4},
		{"only events before the prompt", func(c *chat) { c.event("session_start"); c.user("Go with B?") }, 1},
		{"a lone prompt", func(c *chat) { c.user("Go with B?") }, 0},
		{"a lone prompt and a tool line", func(c *chat) { c.user("Go with B?"); c.tool("Read notes.md (exit 0)") }, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := newChat("answered", start)
			tc.chat(c)
			if got := answered(c.entries(t)); len(got) != tc.kept {
				t.Fatalf("kept %d of %d entries, want %d", len(got), len(c.rows), tc.kept)
			}
		})
	}
}

// The rest of a group an earlier pass stopped in passes the size check with
// one entry that isn't an event, so a last prompt with no reply must not
// be that entry. Here the first chunk ends just before the events of a
// compaction, and the rest is those events and the prompt: the model would
// get the prompt alone, and once the reply came and the chat closed, the
// rest would hold no prompt, wait, and the closing reply would never reach
// the page. Instead the prompt waits, and its reply is sent with it.
func TestContinuedRestLeavesALonePromptWaiting(t *testing.T) {
	long := strings.Repeat("word ", 2000)
	chatWith := func(pad int) (*chat, int) {
		c := newChat("lonepr", start)
		for i := 0; i < 4; i++ {
			c.user("Plan step. " + long)
			c.assistant("Noted step. " + long)
		}
		last := c.tool(strings.Repeat("x", pad))
		c.event("compact")
		c.event("session_start: compact")
		c.user("Go with B?")
		return c, last
	}
	// The longest tool line the first chunk still takes leaves no room for
	// the events after it.
	ends := func(pad int) (bool, bool) {
		c, last := chatWith(pad)
		entries := c.entries(t)
		b, err := Build(answered(entries), threadpage.Page{}, Options{Branch: "main"})
		if err != nil {
			t.Fatal(err)
		}
		return b.End >= entries[last-1].End, b.More && b.End == entries[last-1].End
	}
	lo, hi := 0, 1<<16
	for lo+1 < hi {
		if mid := (lo + hi) / 2; func() bool { in, _ := ends(mid); return in }() {
			lo = mid
		} else {
			hi = mid
		}
	}
	if _, at := ends(lo); !at {
		t.Fatal("no tool line size ends the first chunk just before the events")
	}
	e := newEnv(t)
	c, _ := chatWith(lo)
	file := e.write(c)
	s := e.run(c)
	for _, r := range e.fake.Requests() {
		if strings.Contains(string(r.Input), "Go with B?") {
			t.Fatalf("the prompt was sent without its reply (%q)", outcomes(s))
		}
	}
	if m := e.mark(file); m.Pending(size(t, file)) != 0 {
		t.Fatalf("the waiting prompt leaves the session pending: %+v", m)
	}
	c.assistant("Yes, go with B. That settles the plan.")
	c.event("session_end")
	file = e.write(c)
	s = e.run(c)
	sent := false
	for _, r := range e.fake.Requests() {
		sent = sent || strings.Contains(string(r.Input), "Go with B?") && strings.Contains(string(r.Input), "That settles the plan")
	}
	if !sent || e.mark(file).Pending(size(t, file)) != 0 {
		t.Fatalf("the prompt and its closing reply weren't sent together (%q, mark %+v)", outcomes(s), e.mark(file))
	}
}

// One job drains several sessions under one lock and budget, oldest change
// first, and leaves the others. A session can be named by its id or by the
// id its inbox file holds, as the catch-up names it.
func TestSessionsDrainsTheNamedSessions(t *testing.T) {
	e := newEnv(t)
	var files []string
	for i, id := range []string{"first-session", "odd id/2", "left-alone"} {
		c := newChat(id, start.Add(-time.Duration(3-i)*time.Hour))
		talk(c, 2, "amber")
		files = append(files, e.write(c))
	}
	safe, err := paths.SafeName("odd id/2")
	if err != nil || safe == "odd id/2" {
		t.Fatalf("safe name %q %v", safe, err)
	}
	s, err := e.job().Sessions(context.Background(), []Ref{{Host: HostClaude, ID: "first-session"}, {Host: HostClaude, ID: safe}})
	if err != nil || outcomes(s) != "ok, ok" || calls(e.fake) != 2 {
		t.Fatalf("outcomes %q, %d calls: %v\n%s", outcomes(s), calls(e.fake), err, e.log)
	}
	if s.Records[0].Session != "first-session" || s.Records[1].Session != "odd id/2" {
		t.Fatalf("records %+v", s.Records)
	}
	for i, file := range files {
		if done := e.mark(file).Offset == size(t, file); done != (i < 2) {
			t.Errorf("%s: watermark %+v of %d bytes", filepath.Base(file), e.mark(file), size(t, file))
		}
	}
	if _, err := e.job().Sessions(context.Background(), []Ref{{Host: HostCodex, ID: "missing"}, {Host: HostClaude, ID: "also-missing"}}); !errors.Is(err, ErrNoSession) {
		t.Fatalf("missing sessions: %v", err)
	}
}
