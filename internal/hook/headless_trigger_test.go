package hook

import (
	"reflect"
	"testing"
	"time"

	"github.com/heyNag/eva-md/internal/inbox"
	"github.com/heyNag/eva-md/internal/update"
)

// A Claude Code chat a program runs never asks for an update of its own:
// not at its end, its compaction, a Stop past 60 KB, a prompt after a gap or
// the catch-up at its start, though the state each trigger looks for is
// there. Its start still catches up the project's other idle sessions, which
// the job judges for itself. The same chat run by a person asks, as
// TestEndAndCompactSpawnAnUpdate shows.
func TestProgramRunChatAsksForNoUpdateOfItsOwn(t *testing.T) {
	for _, tc := range []struct {
		event string
		want  [][]string
	}{
		{"SessionEnd", nil},
		{"PreCompact", nil},
		{"Stop", nil},
		{"UserPromptSubmit", nil},
		{"SessionStart", updateOf(update.TriggerStart, "codex:idle-other")},
	} {
		t.Run(tc.event, func(t *testing.T) {
			env, p, spawned := triggerSetup(t)
			t.Setenv("CLAUDE_CODE_ENTRYPOINT", "sdk-cli")
			now := env.Now()
			// The chat's own file: idle an hour, with 120 KB yet to process.
			sessionFile(t, env, "claude-code", p.Session, 2*stopBacklog, inbox.Mark{}, now.Add(-time.Hour))
			sessionFile(t, env, "codex", "idle-other", 1000, inbox.Mark{}, now.Add(-time.Hour))
			reply := "Done."
			p.Event, p.Reply, p.Summary, p.Source = tc.event, &reply, "host summary", "startup"
			invoke(t, "claude-code", p, env)
			if got := spawned(); !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("spawned %q, want %q", got, tc.want)
			}
		})
	}
}
