package resume

import (
	"context"
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
