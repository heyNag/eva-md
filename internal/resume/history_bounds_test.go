package resume

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/heyNag/eva-md/internal/identity"
	"github.com/heyNag/eva-md/internal/inbox"
)

// countReads counts the session files a brief opens until the test ends.
func countReads(t *testing.T) *int {
	t.Helper()
	n := 0
	t.Cleanup(func() { openSession = regular })
	openSession = func(root *os.Root, name string) (*os.File, error) {
		n++
		return regular(root, name)
	}
	return &n
}

// touch sets a session file's modification time.
func touch(t *testing.T, o Options, host, sid string, when time.Time) {
	t.Helper()
	file := (inbox.Store{Root: o.Dirs.Inbox()}).Path(o.Project.Key, host, sid)
	if err := os.Chtimes(file, when, when); err != nil {
		t.Fatal(err)
	}
}

// The brief reads at most the 64 most recently changed session files,
// however many chats it keeps from them: the general area drops every chat
// older than 12 hours, and a repo project's chat on two branches gives two.
// In the general area it reads no file changed before that window at all,
// though it still lists it.
func TestBriefReadsAtMost64SessionFiles(t *testing.T) {
	t.Run("general area, old chats in recently changed files", func(t *testing.T) {
		o := fixture(t)
		o.Project = identity.General()
		for i := 0; i < 80; i++ {
			sid := fmt.Sprintf("old-%02d", i)
			capture(t, o, "codex", sid, "default", o.Now.Add(-48*time.Hour), "old general work")
			touch(t, o, "codex", sid, o.Now.Add(-time.Duration(i+1)*time.Minute))
		}
		reads := countReads(t)
		if _, err := Build(context.Background(), o); err != nil {
			t.Fatal(err)
		}
		if *reads != maxFiles {
			t.Fatalf("the brief read %d session files, want %d", *reads, maxFiles)
		}
	})
	t.Run("general area, files changed before the window", func(t *testing.T) {
		o := fixture(t)
		o.Project = identity.General()
		for i := 0; i < 10; i++ {
			sid := fmt.Sprintf("old-%02d", i)
			capture(t, o, "codex", sid, "default", o.Now.Add(-48*time.Hour), "old general work")
			touch(t, o, "codex", sid, o.Now.Add(-13*time.Hour-time.Duration(i)*time.Minute))
		}
		reads := countReads(t)
		r, err := Build(context.Background(), o)
		if err != nil {
			t.Fatal(err)
		}
		if *reads != 0 {
			t.Fatalf("the brief read %d session files changed before its 12-hour window", *reads)
		}
		// The start-of-chat catch-up still sees them.
		if len(r.Files) != 10 {
			t.Fatalf("the brief listed %d session files, want 10", len(r.Files))
		}
	})
	t.Run("repo project, chats on two branches", func(t *testing.T) {
		o := fixture(t)
		for i := 0; i < 70; i++ {
			sid := fmt.Sprintf("chat-%02d", i)
			when := o.Now.Add(-time.Duration(i+1) * time.Hour)
			capture(t, o, "codex", sid, "main", when, "main work")
			capture(t, o, "codex", sid, "feature", when.Add(time.Minute), "feature work")
			touch(t, o, "codex", sid, when.Add(2*time.Minute))
		}
		reads := countReads(t)
		r, err := Build(context.Background(), o)
		if err != nil {
			t.Fatal(err)
		}
		if *reads != maxFiles || len(r.Files) != maxFiles {
			t.Fatalf("the brief read %d session files and listed %d, want %d", *reads, len(r.Files), maxFiles)
		}
	})
}

// A chat that only closed, compacted or restarted since its work never
// outranks a chat with newer work: the previous chat, its time, and a
// resumed chat's note of other chats go by work, not lifecycle events.
func TestEventsDoNotMakeAnIdleChatNewest(t *testing.T) {
	event := func(t *testing.T, o Options, host, sid string, when time.Time, text string) {
		t.Helper()
		e := inbox.Entry{Host: host, Session: sid, Project: o.Project.Key, Branch: o.Project.Branch, TS: when.Format(time.RFC3339), Role: "event", Kind: "event", Text: text, Origin: "hook"}
		if err := (inbox.Store{Root: o.Dirs.Inbox()}).Append(e); err != nil {
			t.Fatal(err)
		}
	}
	t.Run("new chat", func(t *testing.T) {
		o := fixture(t)
		o.Variant = "startup"
		capture(t, o, "codex", "old-work", "main", o.Now.Add(-24*time.Hour), "OLD-WORK-PROMPT")
		capture(t, o, "claude-code", "new-work", "main", o.Now.Add(-2*time.Hour), "NEW-WORK-PROMPT")
		event(t, o, "codex", "old-work", o.Now.Add(-time.Hour), "session_end")
		r, err := Build(context.Background(), o)
		if err != nil {
			t.Fatal(err)
		}
		want := "Previous chat: claude-code / test-model · " + o.Now.Add(-2*time.Hour+2*time.Second).Format(time.RFC3339)
		if !strings.Contains(r.Text, want) || !strings.Contains(r.Text, "codex · OLD-WORK-PROMPT · s:old-work#1") {
			t.Fatalf("an old chat's session_end made it the previous chat:\n%s", r.Text)
		}
	})
	t.Run("resumed chat", func(t *testing.T) {
		o := fixture(t)
		o.Variant = "resume"
		o.Host, o.Session = "codex", "me"
		capture(t, o, o.Host, o.Session, "main", o.Now.Add(-3*time.Hour), "my work")
		capture(t, o, "claude-code", "other", "main", o.Now.Add(-5*time.Hour), "other work")
		event(t, o, "claude-code", "other", o.Now.Add(-time.Hour), "session_end")
		r, err := Build(context.Background(), o)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(r.Text, "Other chat:") {
			t.Fatalf("another chat's session_end was reported as new work:\n%s", r.Text)
		}
	})
	t.Run("general area", func(t *testing.T) {
		o := fixture(t)
		o.Project = identity.General()
		capture(t, o, "codex", "old-general", "default", o.Now.Add(-24*time.Hour), "OLD-GENERAL-WORK")
		event(t, o, "codex", "old-general", o.Now.Add(-time.Hour), "session_end")
		r, err := Build(context.Background(), o)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(r.Text, "OLD-GENERAL-WORK") {
			t.Fatalf("a fresh event kept a day-old general chat in its 12-hour window:\n%s", r.Text)
		}
	})
}

// Work found before the tail counts only for the branch it was done on. A
// branch of the same chat with a single exchange is still not real work, as
// in a short file, while the branch that worked still counts. A work line the
// tail cuts in two is read whole, so its branch is known.
func TestWorkBeforeTheTailCountsOnlyOnItsBranch(t *testing.T) {
	write := func(t *testing.T, o Options, data []byte) {
		t.Helper()
		file := (inbox.Store{Root: o.Dirs.Inbox()}).Path(o.Project.Key, "codex", "mixed")
		if err := os.MkdirAll(filepath.Dir(file), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(file, data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	lines := func(t *testing.T, o Options) func(branch, role, kind, text string) []byte {
		ts := o.Now.Add(-2 * time.Hour)
		return func(branch, role, kind, text string) []byte {
			e := inbox.Entry{V: 1, Host: "codex", Session: "mixed", Project: o.Project.Key, Branch: branch, TS: ts.Format(time.RFC3339), Role: role, Kind: kind, Text: text, Origin: "hook"}
			ts = ts.Add(time.Second)
			data, err := json.Marshal(e)
			if err != nil {
				t.Fatal(err)
			}
			return append(data, '\n')
		}
	}
	t.Run("other branch's work", func(t *testing.T) {
		o := fixture(t)
		o.Variant = "startup"
		o.Project.Branch = "feature"
		line := lines(t, o)
		var buf bytes.Buffer
		buf.Write(line("main", "user", "prompt", "Run the tests."))
		buf.Write(line("main", "tool", "tool", "Bash: go test ./... (exit 0)"))
		for n := 0; n < tailBytes+64<<10; n += 100 << 10 {
			buf.Write(line("main", "event", "event", strings.Repeat("p", 100<<10)))
		}
		buf.Write(line("feature", "user", "prompt", "FEATURE-ONLY-QUESTION what is this branch?"))
		buf.Write(line("feature", "assistant", "reply", "It is a feature branch."))
		write(t, o, buf.Bytes())
		r, err := Build(context.Background(), o)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(r.Text, "FEATURE-ONLY-QUESTION") || !strings.Contains(r.Text, "Continuing from main") {
			t.Fatalf("main's work before the tail counted for feature's single exchange:\n%s", r.Text)
		}
	})
	t.Run("work line cut by the tail", func(t *testing.T) {
		o := fixture(t)
		o.Variant = "startup"
		o.Project.Branch = "feature"
		line := lines(t, o)
		var head, tail bytes.Buffer
		head.Write(line("feature", "user", "prompt", "Start the feature."))
		head.Write(line("feature", "tool", "tool", "Bash: go test ./... (exit 0) "+strings.Repeat("o", 200<<10)))
		for tail.Len() < tailBytes-150<<10 {
			tail.Write(line("feature", "event", "event", strings.Repeat("p", 10<<10)))
		}
		tail.Write(line("feature", "user", "prompt", "Where are we?"))
		tail.Write(line("feature", "assistant", "reply", "TAIL-ANSWER"))
		start := head.Len() + tail.Len() - tailBytes
		if tool := bytes.Index(head.Bytes(), toolMarker); start <= tool || start >= head.Len()-1 {
			t.Fatalf("the tail starts at byte %d, not inside the tool line, so this case proves nothing", start)
		}
		write(t, o, append(head.Bytes(), tail.Bytes()...))
		r, err := Build(context.Background(), o)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(r.Text, "TAIL-ANSWER") {
			t.Fatalf("the tool line the tail cut did not count for its branch:\n%s", cut(r.Text, 800))
		}
	})
}

func TestLineBranch(t *testing.T) {
	for line, want := range map[string]string{
		`{"kind":"tool","text":"say \"branch\":\"x\"","branch":"feat\"ure"}`: `feat"ure`,
		`{"kind":"tool","branch":"main","cwd":"/r"}`:                         "main",
		`{"kind":"tool","text":"no branch"}`:                                 "default",
		`{"kind":"tool","branch":""}`:                                        "default",
	} {
		if got := lineBranch([]byte(line)); got != want {
			t.Errorf("lineBranch(%s) = %q, want %q", line, got, want)
		}
	}
}
