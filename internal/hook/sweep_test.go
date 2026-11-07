package hook

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/heyNag/eva-md/internal/inbox"
)

// A person's chat start asks for the day's sweep with its catch-up, in the
// same job, while no update job has taken the sweep on the hook's day: the
// catch-up's sessions with --sweep, or --sweep alone when it has none,
// either giving the job the hook's day to claim the sweep on. Once a job
// records today as the day it took the sweep, chat starts ask only for
// their catch-up again, and one with none starts nothing; the next day's
// first chat start asks again. Other events never ask, and the day's
// maintain stays a job of its own.
func TestChatStartAsksForTheDaysSweep(t *testing.T) {
	for _, host := range hosts {
		t.Run(host, func(t *testing.T) {
			env, p, spawned := maintainSetup(t)
			now := env.Now()
			sweep := "--sweep=" + now.Format(time.DateOnly)
			sweptOn(t, env, now.AddDate(0, 0, -1))
			p.Event, p.Source = "SessionStart", "startup"
			invoke(t, host, p, env)
			if got, want := spawned(), [][]string{{"maintain"}, {"update", sweep}}; !reflect.DeepEqual(got, want) {
				t.Fatalf("first chat start: spawned %q, want %q", got, want)
			}
			sessionFile(t, env, "codex", "idle-other", 1000, inbox.Mark{}, now.Add(-time.Hour))
			p.Session = "second-chat"
			invoke(t, host, p, env)
			if got, want := updateSpawns(spawned())[1:], [][]string{{"update", "--session", "codex:idle-other", sweep}}; !reflect.DeepEqual(got, want) {
				t.Fatalf("with an idle session: spawned %q, want %q", got, want)
			}
			n := len(spawned())
			reply := "Done."
			p.Reply = &reply
			for _, event := range []string{"UserPromptSubmit", "PostToolUse", "Stop", "PreCompact"} {
				p.Event = event
				invoke(t, host, p, env)
			}
			for _, args := range spawned()[n:] {
				if strings.HasPrefix(args[len(args)-1], sweepFlag) {
					t.Fatalf("%q asked for the sweep", args)
				}
			}
			sweptOn(t, env, now)
			p.Session, p.Event = "third-chat", "SessionStart"
			n = len(spawned())
			invoke(t, host, p, env)
			if got, want := spawned()[n:], [][]string{{"update", "--session", "codex:idle-other"}}; !reflect.DeepEqual(got, want) {
				t.Fatalf("once the sweep was taken: spawned %q, want %q", got, want)
			}
			if err := os.Remove(inbox.Store{Root: env.Dirs.Inbox()}.Path("general", "codex", "idle-other")); err != nil {
				t.Fatal(err)
			}
			p.Session = "fourth-chat"
			n = len(spawned())
			invoke(t, host, p, env)
			if got := spawned()[n:]; len(got) != 0 {
				t.Fatalf("no catch-up and the sweep taken: spawned %q", got)
			}
			env.Now = func() time.Time { return now.Add(24 * time.Hour) }
			p.Session = "next-day"
			n = len(spawned())
			invoke(t, host, p, env)
			if got, want := spawned()[n:], [][]string{{"maintain"}, {"update", "--sweep=" + now.Add(24*time.Hour).Format(time.DateOnly)}}; !reflect.DeepEqual(got, want) {
				t.Fatalf("the next day: spawned %q, want %q", got, want)
			}
		})
	}
}

// A chat a program runs never asks for the sweep, though its start still
// catches up the project's other idle sessions, and with updates off a
// chat start asks for nothing.
func TestProgramRunChatAsksForNoSweep(t *testing.T) {
	env, p, spawned := triggerSetup(t)
	now := env.Now()
	if err := os.Remove(filepath.Join(env.Dirs.State, "updates", "sweep")); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CLAUDE_CODE_ENTRYPOINT", "sdk-cli")
	p.Event, p.Source = "SessionStart", "startup"
	invoke(t, "claude-code", p, env)
	if got := spawned(); got != nil {
		t.Fatalf("a program's chat start with no catch-up: spawned %q", got)
	}
	sessionFile(t, env, "codex", "idle-other", 1000, inbox.Mark{}, now.Add(-time.Hour))
	p.Session = "second-worker"
	invoke(t, "claude-code", p, env)
	if got, want := spawned(), updateOf("codex:idle-other"); !reflect.DeepEqual(got, want) {
		t.Fatalf("a program's chat start: spawned %q, want %q", got, want)
	}
	t.Setenv("CLAUDE_CODE_ENTRYPOINT", "")
	env.Config.Updates = false
	p.Session = "person-chat"
	invoke(t, "claude-code", p, env)
	if got, want := spawned(), updateOf("codex:idle-other"); !reflect.DeepEqual(got, want) {
		t.Fatalf("updates off: spawned %q", got)
	}
}
