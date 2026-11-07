package resume

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/heyNag/eva-md/internal/inbox"
)

// programChat stores a chat a program ran, as the hooks store one: its
// prompts are host prompts, while its tool lines and replies are kept.
func programChat(t *testing.T, o Options, sid, branch string, when time.Time, reply string) {
	t.Helper()
	store := inbox.Store{Root: o.Dirs.Inbox()}
	for i, part := range []struct{ role, kind, text string }{{"event", "event", "session_start: startup"}, {"event", "event", "host_prompt"},
		{"tool", "tool", "Bash go test ./... (exit 0)"}, {"assistant", "reply", reply}} {
		e := inbox.Entry{V: 1, Host: "claude-code", Session: sid, Project: o.Project.Key, Branch: branch, TS: when.Add(time.Duration(i) * time.Second).Format(time.RFC3339), Role: part.role, Kind: part.kind, Text: part.text, Origin: "hook"}
		if err := store.Append(e); err != nil {
			t.Fatal(err)
		}
	}
}

// A newer chat a program ran on the branch, with tool lines, never becomes
// the brief's previous chat: the person's older chat does. A person's chat
// that holds host prompts as well as typed ones still can.
func TestBriefPassesOverAProgramRunChat(t *testing.T) {
	o := fixture(t)
	o.Variant = "startup"
	capture(t, o, "codex", "person-chat", "main", o.Now.Add(-2*time.Hour), "PERSON_PROMPT about the replay check.")
	programChat(t, o, "worker-chat", "main", o.Now.Add(-time.Hour), "WORKER_REPLY from the program.")
	r, err := Build(context.Background(), o)
	if err != nil || !strings.Contains(r.Text, "PERSON_PROMPT") || strings.Contains(r.Text, "WORKER_REPLY") {
		t.Fatalf("%v:\n%s", err, r.Text)
	}
	store := inbox.Store{Root: o.Dirs.Inbox()}
	if err := store.Append(inbox.Entry{V: 1, Host: "claude-code", Session: "loop-chat", Project: o.Project.Key, Branch: "main", TS: o.Now.Add(-30 * time.Minute).Format(time.RFC3339), Role: "event", Kind: "event", Text: "host_prompt", Origin: "hook"}); err != nil {
		t.Fatal(err)
	}
	capture(t, o, "claude-code", "loop-chat", "main", o.Now.Add(-29*time.Minute), "LOOP_OWNER_PROMPT about the schedule.")
	if r, err = Build(context.Background(), o); err != nil || !strings.Contains(r.Text, "LOOP_OWNER_PROMPT") {
		t.Fatalf("%v:\n%s", err, r.Text)
	}
}

// A person's chat that checked out another branch is judged on its whole
// file, where the person typed on the branch it started on: the new branch's
// history, which holds a host prompt and no typed one, is still the previous
// chat there.
func TestBranchSwitchIsNoProgramRunChat(t *testing.T) {
	o := fixture(t)
	o.Variant = "startup"
	o.Project.Branch = "feature-x"
	store := inbox.Store{Root: o.Dirs.Inbox()}
	for i, part := range []struct{ branch, role, kind, text string }{{"main", "event", "event", "session_start: startup"}, {"main", "user", "prompt", "Start feature-x."},
		{"feature-x", "tool", "tool", "Bash git checkout -b feature-x (exit 0)"}, {"feature-x", "event", "event", "host_prompt"}, {"feature-x", "assistant", "reply", "SWITCHER_REPLY on feature-x."}} {
		e := inbox.Entry{V: 1, Host: "claude-code", Session: "switcher", Project: o.Project.Key, Branch: part.branch, TS: o.Now.Add(time.Duration(i-60) * time.Minute).Format(time.RFC3339), Role: part.role, Kind: part.kind, Text: part.text, Origin: "hook"}
		if err := store.Append(e); err != nil {
			t.Fatal(err)
		}
	}
	if r, err := Build(context.Background(), o); err != nil || !strings.Contains(r.Text, "SWITCHER_REPLY") {
		t.Fatalf("%v:\n%s", err, r.Text)
	}
}

// Lineage, for the brief of a new branch and for a new thread page's
// parent, passes over a newer chat a program ran on another branch.
func TestLineagePassesOverAProgramRunChat(t *testing.T) {
	o := fixture(t)
	o.Variant = "startup"
	capture(t, o, "codex", "person-chat", "main", o.Now.Add(-2*time.Hour), "PERSON_PROMPT about the replay check.")
	programChat(t, o, "worker-chat", "worker-branch", o.Now.Add(-time.Hour), "WORKER_REPLY from the program.")
	o.Project.Branch = "feature/new"
	r, err := Build(context.Background(), o)
	if err != nil || !strings.Contains(r.Text, "Continuing from main") || strings.Contains(r.Text, "WORKER_REPLY") {
		t.Fatalf("%v:\n%s", err, r.Text)
	}
	if branch, err := Lineage(context.Background(), o); err != nil || branch != "main" {
		t.Fatalf("Lineage: %q %v", branch, err)
	}
}

// A chat a program ran that checks out a branch is passed over on that
// branch too: the whole session file is judged, as the job judges it. It is
// neither the new branch's previous chat nor the lineage's pick.
func TestProgramRunChatThatSwitchesBranch(t *testing.T) {
	o := fixture(t)
	o.Variant = "startup"
	capture(t, o, "codex", "person-chat", "main", o.Now.Add(-3*time.Hour), "PERSON_PROMPT on main.")
	store := inbox.Store{Root: o.Dirs.Inbox()}
	for i, part := range []struct{ branch, role, kind, text string }{{"main", "event", "event", "session_start: startup"}, {"main", "event", "event", "host_prompt"},
		{"fix-12", "tool", "tool", "Bash git checkout -b fix-12 (exit 0)"}, {"fix-12", "tool", "tool", "Bash go test ./... (exit 0)"}, {"fix-12", "assistant", "reply", "WORKER_REPLY on fix-12."}} {
		e := inbox.Entry{V: 1, Host: "claude-code", Session: "worker", Project: o.Project.Key, Branch: part.branch, TS: o.Now.Add(time.Duration(i-60) * time.Minute).Format(time.RFC3339), Role: part.role, Kind: part.kind, Text: part.text, Origin: "hook"}
		if err := store.Append(e); err != nil {
			t.Fatal(err)
		}
	}
	o.Project.Branch = "fix-12"
	if r, err := Build(context.Background(), o); err != nil || strings.Contains(r.Text, "WORKER_REPLY") {
		t.Fatalf("fix-12: %v:\n%s", err, r.Text)
	}
	o.Project.Branch = "feature/new"
	if branch, err := Lineage(context.Background(), o); err != nil || branch != "main" {
		t.Fatalf("Lineage: %q %v", branch, err)
	}
	if r, err := Build(context.Background(), o); err != nil || !strings.Contains(r.Text, "Continuing from main") || strings.Contains(r.Text, "WORKER_REPLY") {
		t.Fatalf("feature/new: %v:\n%s", err, r.Text)
	}
}

// programChats stores n chats a program ran, newest first from newest, a
// minute apart, each with replies until its file holds size bytes or more.
func programChats(t *testing.T, o Options, n int, newest time.Time, size int64) {
	t.Helper()
	store := inbox.Store{Root: o.Dirs.Inbox()}
	for i := 0; i < n; i++ {
		sid := fmt.Sprintf("worker-%03d", i)
		when := newest.Add(-time.Duration(i) * time.Minute)
		programChat(t, o, sid, "main", when.Add(-10*time.Second), "WORKER_REPLY from the program.")
		file := store.Path(o.Project.Key, "claude-code", sid)
		for {
			info, err := os.Stat(file)
			if err != nil {
				t.Fatal(err)
			}
			if info.Size() >= size {
				break
			}
			// An entry's text is kept up to 200 KB.
			e := inbox.Entry{V: 1, Host: "claude-code", Session: sid, Project: o.Project.Key, Branch: "main", TS: when.Format(time.RFC3339), Role: "assistant", Kind: "reply", Text: strings.Repeat("x", int(min(size-info.Size(), 150<<10))), Origin: "hook"}
			if err := store.Append(e); err != nil {
				t.Fatal(err)
			}
		}
		touch(t, o, "claude-code", sid, when)
	}
}

// A session file found to hold a chat a program ran doesn't count toward
// the 64 files the brief reads, so a burst of 70 program-run chats newer
// than the person's last one, as a script that runs claude -p in a loop
// leaves, doesn't push it out of the brief: it is still the previous chat.
// The brief reads, and lists for the catch-up, all 71 files.
func TestProgramRunChatsDontCountTowardThe64(t *testing.T) {
	o := fixture(t)
	o.Variant = "startup"
	capture(t, o, "codex", "person-chat", "main", o.Now.Add(-5*time.Hour), "PERSON_PROMPT about the replay check.")
	touch(t, o, "codex", "person-chat", o.Now.Add(-5*time.Hour))
	programChats(t, o, 70, o.Now.Add(-time.Minute), 100)
	reads := countReads(t)
	r, err := Build(context.Background(), o)
	if err != nil || !strings.Contains(r.Text, "PERSON_PROMPT") || strings.Contains(r.Text, "WORKER_REPLY") {
		t.Fatalf("%v:\n%s", err, r.Text)
	}
	if *reads != 71 || len(r.Files) != 71 {
		t.Fatalf("the brief read %d session files and listed %d, want 71", *reads, len(r.Files))
	}
}

// The files of chats a program ran that don't count are bounded too, so
// the brief stays within its time: at most 64 of them, and 2 MiB of them in
// all, are read beyond the 64, and the rest count as any file does.
func TestProgramRunFilesBeyondThe64AreBounded(t *testing.T) {
	t.Run("files", func(t *testing.T) {
		o := fixture(t)
		o.Variant = "startup"
		capture(t, o, "codex", "person-chat", "main", o.Now.Add(-5*time.Hour), "PERSON_PROMPT about the replay check.")
		touch(t, o, "codex", "person-chat", o.Now.Add(-5*time.Hour))
		programChats(t, o, 130, o.Now.Add(-time.Minute), 100)
		reads := countReads(t)
		r, err := Build(context.Background(), o)
		if err != nil || strings.Contains(r.Text, "PERSON_PROMPT") {
			t.Fatalf("%v:\n%s", err, r.Text)
		}
		if *reads != programFiles+maxFiles || len(r.Files) != programFiles+maxFiles {
			t.Fatalf("the brief read %d session files and listed %d, want %d", *reads, len(r.Files), programFiles+maxFiles)
		}
	})
	t.Run("bytes", func(t *testing.T) {
		o := fixture(t)
		o.Variant = "startup"
		// 10 program-run chats of about 900 KB each: 2 of them fit in the
		// 2 MiB, and the other 8 count, with 56 of the person's chats.
		programChats(t, o, 10, o.Now.Add(-time.Minute), 900<<10)
		for i := 0; i < 70; i++ {
			sid := fmt.Sprintf("person-%02d", i)
			when := o.Now.Add(-time.Hour - time.Duration(i)*time.Minute)
			capture(t, o, "codex", sid, "main", when.Add(-10*time.Second), "PERSON_PROMPT about the replay check.")
			touch(t, o, "codex", sid, when)
		}
		reads := countReads(t)
		if _, err := Build(context.Background(), o); err != nil {
			t.Fatal(err)
		}
		if *reads != 2+maxFiles {
			t.Fatalf("the brief read %d session files, want %d", *reads, 2+maxFiles)
		}
	})
}
