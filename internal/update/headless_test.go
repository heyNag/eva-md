package update

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/heyNag/eva-md/internal/catchup"
	"github.com/heyNag/eva-md/internal/inbox"
)

// programChat is a chat a program ran, as the hooks store one: its prompts
// are host prompts, while its replies and tool lines are kept, well past
// what would pass the size check with a typed prompt.
func programChat(session string) *chat {
	c := newChat(session, start)
	c.event("session_start: startup")
	for i := 0; i < 2; i++ {
		c.event("host_prompt")
		c.tool("Read internal/update/pass.go (exit 0)")
		c.assistant("The worker's report. " + filler)
	}
	return c
}

// A chat a program ran costs no model call: the job passes over it, skipped
// (headless), and moves its watermark to the end of its file, so it is
// never planned again. What the program appends later is passed over too,
// though the host prompt that marks it lies before the watermark.
func TestProgramRunChatIsPassedOver(t *testing.T) {
	e := newEnv(t)
	c := programChat("worker")
	file := e.write(c)
	if s := e.run(c); outcomes(s) != "skipped (headless)" || calls(e.fake) != 0 || e.mark(file) != (inbox.Mark{Offset: size(t, file)}) {
		t.Fatalf("outcomes %q, %d calls, watermark %+v of %d bytes\n%s", outcomes(s), calls(e.fake), e.mark(file), size(t, file), e.log)
	}
	if s := e.run(c); s.Line() != "nothing pending" {
		t.Fatalf("again: %q\n%s", s.Line(), e.log)
	}
	c.assistant("A last word from the worker. " + filler)
	c.event("session_end")
	file = e.write(c)
	if s := e.run(c); outcomes(s) != "skipped (headless)" || calls(e.fake) != 0 || e.mark(file) != (inbox.Mark{Offset: size(t, file)}) {
		t.Fatalf("appended: outcomes %q, %d calls, watermark %+v of %d bytes\n%s", outcomes(s), calls(e.fake), e.mark(file), size(t, file), e.log)
	}
}

// A chat with no prompt stored at all, as when its prompts were held back,
// is passed over when its catch-up record marks it a program's chat, and
// waits as before when the record doesn't. A person's chat a program
// resumed, whose record says so too, is still updated.
func TestCatchUpRecordMarksAProgramRunChat(t *testing.T) {
	for _, tc := range []struct {
		name     string
		headless bool
		typed    bool
		want     string
	}{
		{"marked", true, false, "skipped (headless)"},
		{"not marked", false, false, ""},
		{"marked, with typed prompts", true, true, "ok"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := newEnv(t)
			c := newChat("unprompted", start)
			c.event("session_start: startup")
			if tc.typed {
				talk(c, 1, "amber")
			}
			c.tool("Read internal/update/pass.go (exit 0)")
			c.assistant("The worker's report. " + filler)
			c.event("session_end")
			file := e.write(c)
			record(t, e, catchup.Source{V: 1, Host: c.host, Session: c.session, Headless: tc.headless})
			s := e.run(c)
			if outcomes(s) != tc.want {
				t.Fatalf("outcomes %q, want %q\n%s", outcomes(s), tc.want, e.log)
			}
			if tc.want == "skipped (headless)" && (calls(e.fake) != 0 || e.mark(file) != (inbox.Mark{Offset: size(t, file)})) {
				t.Fatalf("%d calls, watermark %+v of %d bytes", calls(e.fake), e.mark(file), size(t, file))
			}
		})
	}
}

// A chat a program ran whose backlog runs on past the window is passed over
// a window at a time when its catch-up record marks it, without waiting for
// the end of the file. Without the record it waits as before, since a
// person's chat can begin with host prompts and type later, until a window
// reaches the end of the file. The window's bound is lowered for the first
// pass, so a backlog just over windowBytes runs on past it.
func TestMarkedProgramRunChatIsPassedOverAWindowAtATime(t *testing.T) {
	for _, marked := range []bool{true, false} {
		t.Run(map[bool]string{true: "marked", false: "not marked"}[marked], func(t *testing.T) {
			e := newEnv(t)
			c := newChat("worker", start)
			c.event("session_start: startup")
			c.event("host_prompt")
			for i := 0; i < 90; i++ {
				c.assistant("The worker's report. " + strings.Repeat(filler, 200))
			}
			file := e.write(c)
			if size(t, file) <= windowBytes {
				t.Fatalf("%d bytes: within the window", size(t, file))
			}
			if marked {
				record(t, e, catchup.Source{V: 1, Host: c.host, Session: c.session, Headless: true})
			}
			r, files, err := e.job().start(func(f []sessionFile) ([]sessionFile, error) { return f, nil })
			if err != nil || len(files) != 1 {
				t.Fatalf("start: %d files, %v", len(files), err)
			}
			b, err := openBacklog(r.store, files[0].path)
			if err != nil {
				t.Fatal(err)
			}
			b.limit = windowBytes
			cf := &chatFile{file: files[0], b: b, commits: map[string][]string{}, heads: map[string]string{}}
			if pending, err := b.refill(); !pending || err != nil {
				t.Fatalf("refill: %v %v", pending, err)
			}
			r.pass(context.Background(), cf)
			b.close()
			m := e.mark(file)
			if marked && (outcomes(r.sum) != "skipped (headless)" || m.Offset == 0 || m.Offset > windowBytes) ||
				!marked && (outcomes(r.sum) != "" || r.sum.Waiting != 1 || m.Offset != 0) || calls(e.fake) != 0 {
				t.Fatalf("outcomes %q, waiting %d, %d calls, watermark %+v of %d bytes\n%s", outcomes(r.sum), r.sum.Waiting, calls(e.fake), m, size(t, file), e.log)
			}
			// With the whole rest in its window, the chat is passed over to
			// the end of its file, marked or not.
			if s := e.run(c); outcomes(s) != "skipped (headless)" || calls(e.fake) != 0 || e.mark(file) != (inbox.Mark{Offset: size(t, file)}) {
				t.Fatalf("the rest: outcomes %q, %d calls, watermark %+v of %d bytes\n%s", outcomes(s), calls(e.fake), e.mark(file), size(t, file), e.log)
			}
		})
	}
}

// record writes a session's catch-up record where the hooks register it.
func record(t *testing.T, e *env, s catchup.Source) {
	t.Helper()
	dir := filepath.Join(e.dirs.State, "catchup", s.Host)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, s.Session+".json"), data, 0o600); err != nil {
		t.Fatal(err)
	}
}

// A person's chat that holds host prompts, from a scheduled task, a /loop
// run or a subagent's report, is still updated: with its typed prompts in
// the same pass, in the rest of a group the first pass stopped in, and
// waiting for more when its typed prompts lie before the watermark.
func TestPersonsChatWithHostPromptsIsUpdated(t *testing.T) {
	t.Run("same pass", func(t *testing.T) {
		e := newEnv(t)
		c := newChat("person", start)
		c.event("host_prompt")
		c.assistant("Ran the scheduled cleanup. " + filler)
		talk(c, 1, "amber")
		e.write(c)
		if s := e.run(c); outcomes(s) != "ok" || calls(e.fake) != 1 {
			t.Fatalf("outcomes %q, %d calls\n%s", outcomes(s), calls(e.fake), e.log)
		}
	})
	t.Run("rest of a group", func(t *testing.T) {
		e := newEnv(t)
		c := newChat("person", start)
		talk(c, 1, "amber")
		// Over 60 KB of host prompts and replies after the typed one: the
		// first pass stops inside the group, and the rest holds no typed
		// prompt.
		for i := 0; i < 25; i++ {
			c.event("host_prompt")
			c.assistant("A /loop run's report. " + strings.Repeat(filler, 10))
		}
		file := e.write(c)
		if s := e.run(c); outcomes(s) != "ok, ok" || calls(e.fake) != 2 || e.mark(file) != (inbox.Mark{Offset: size(t, file)}) {
			t.Fatalf("outcomes %q, %d calls, watermark %+v\n%s", outcomes(s), calls(e.fake), e.mark(file), e.log)
		}
	})
	t.Run("typed prompts before the watermark", func(t *testing.T) {
		e := newEnv(t)
		c := newChat("person", start)
		talk(c, 2, "amber")
		done := size(t, e.write(c))
		e.run(c)
		c.event("host_prompt")
		c.assistant("Ran the scheduled cleanup. " + filler)
		file := e.write(c)
		if s := e.run(c); len(s.Records) != 0 || s.Waiting != 1 || e.mark(file).Offset != done {
			t.Fatalf("outcomes %q, waiting %d, watermark %+v at %d\n%s", outcomes(s), s.Waiting, e.mark(file), done, e.log)
		}
	})
}

// scanPrefix finds a typed prompt in a line longer than its read, by the
// role and kind at the line's start, and a host prompt, while Prefix counts
// the lines and the record state as before.
func TestScanPrefixFindsPrompts(t *testing.T) {
	c := newChat("prefix", start)
	c.event("off_record")
	c.event("host_prompt")
	data := c.jsonl(t)
	p, err := scanPrefix(bytes.NewReader(data))
	if err != nil || p != (prefixScan{lines: 2, off: true, hosted: true}) {
		t.Fatalf("%+v %v", p, err)
	}
	c.user(strings.Repeat("a long paste ", 10<<10))
	data = c.jsonl(t)
	if p, err = scanPrefix(bytes.NewReader(data)); err != nil || p != (prefixScan{lines: 3, off: true, typed: true, hosted: true}) {
		t.Fatalf("%+v %v", p, err)
	}
	if lines, off, err := Prefix(bytes.NewReader(data)); lines != 3 || !off || err != nil {
		t.Fatalf("Prefix: %d %v %v", lines, off, err)
	}
}
