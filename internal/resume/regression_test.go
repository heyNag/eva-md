package resume

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/heyNag/eva-md/internal/inbox"
)

// fakeGit puts a git script first on PATH for the rest of the test.
func fakeGit(t *testing.T, script string) {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "git"), []byte("#!/bin/sh\n"+script+"\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

// A slow git must not use up the prompt hook's budget: the brief still
// arrives in time, with live git marked unknown.
func TestSlowGitLeavesTimeToDeliverBrief(t *testing.T) {
	o := fixture(t)
	capture(t, o, "codex", "slow-git", "main", o.Now.Add(-time.Hour), "Keep the slow-git brief")
	o.Project.Root = t.TempDir()
	fakeGit(t, "exec sleep 5")
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	r, err := Build(ctx, o)
	if err != nil || ctx.Err() != nil {
		t.Fatalf("brief built past the deadline: %v %v", err, ctx.Err())
	}
	if !strings.Contains(r.Text, "Live git: unknown") || !strings.Contains(r.Text, "slow-git brief") {
		t.Fatal(r.Text)
	}
}

func TestFailedOperationLookupSaysUnknown(t *testing.T) {
	o := fixture(t)
	o.Project.Root = newRepo(t, "main")
	real, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	fakeGit(t, `for a; do [ "$a" = --git-path ] && exit 1; done; exec '`+real+`' "$@"`)
	g := gitSummary(context.Background(), o.Project, "")
	if !strings.Contains(g.text, "operation in progress: unknown") || !strings.Contains(g.text, "branch main") {
		t.Fatal(g.text)
	}
}

// The session_start the hook writes just before a compact brief is not
// newer work, so it can't make a fresh Now stale.
func TestSessionStartDoesNotMakeNowStale(t *testing.T) {
	o := fixture(t)
	o.Variant = "compact"
	o.Host = "codex"
	o.Session = "compact-session"
	capture(t, o, o.Host, o.Session, "main", o.Now.Add(-time.Hour), "Keep the compact plan")
	wiki(t, o, o.Now.Add(-30*time.Minute), "### Next\nShip the compact fix")
	writeEntries(t, o, entry(o, o.Host, o.Session, "main", o.Now.Add(-time.Second), "event", "event", "session_start: compact"))
	r, err := Build(context.Background(), o)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(r.Text, "Ship the compact fix") || strings.Contains(r.Text, "stale") {
		t.Fatal(r.Text)
	}
}

// How a chat ended comes from its event entries, never from what someone
// typed or the model replied.
func TestEndingIgnoresEventWordsInMessages(t *testing.T) {
	for _, text := range []string{"stop_failure: rate_limit is in my logs", "interrupt", "compact"} {
		t.Run(text, func(t *testing.T) {
			o := fixture(t)
			capture(t, o, "codex", "words", "main", o.Now.Add(-time.Hour), "Earlier work")
			writeEntries(t, o, entry(o, "codex", "words", "main", o.Now.Add(-time.Minute), "user", "prompt", text))
			r, err := Build(context.Background(), o)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(r.Text, "· no reply recorded") {
				t.Fatal(r.Text)
			}
		})
	}
	o := fixture(t)
	capture(t, o, "codex", "words", "main", o.Now.Add(-time.Hour), "Earlier work")
	writeEntries(t, o, entry(o, "codex", "words", "main", o.Now.Add(-time.Minute), "assistant", "reply", "stop_failure is a hook event"))
	r, err := Build(context.Background(), o)
	if err != nil || !strings.Contains(r.Text, "· reply recorded") {
		t.Fatalf("%v %s", err, r.Text)
	}
}

// Time-ordered ids can share their first 8 characters; the full id still
// names one session, and the short ref still works when it is unique.
func TestShowAcceptsFullSessionID(t *testing.T) {
	o := fixture(t)
	capture(t, o, "codex", "019a2b3c-0001", "main", o.Now.Add(-time.Hour), "first chat words")
	capture(t, o, "codex", "019a2b3c-0002", "main", o.Now.Add(-time.Minute), "second chat words")
	capture(t, o, "claude-code", "unique-session-id", "main", o.Now.Add(-time.Minute), "unique words")
	if _, err := Show(context.Background(), o.Dirs, o.Config, inbox.Ref("019a2b3c-0001", 1)); err == nil {
		t.Fatal("ambiguous short ref accepted")
	}
	for ref, want := range map[string]string{"s:019a2b3c-0001#1": "first chat words", "s:019a2b3c-0002#1": "second chat words", "s:unique-s#1": "unique words", "s:unique-session-id#1": "unique words"} {
		text, err := Show(context.Background(), o.Dirs, o.Config, ref)
		if err != nil || !strings.Contains(text, want) {
			t.Errorf("%s: %q %v", ref, text, err)
		}
	}
	if _, err := Show(context.Background(), o.Dirs, o.Config, "s:019a2b3c-00#1"); err == nil {
		t.Fatal("partial id longer than 8 characters accepted")
	}
}
