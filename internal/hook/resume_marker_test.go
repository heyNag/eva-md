package hook

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/heyNag/eva-md/internal/identity"
	"github.com/heyNag/eva-md/internal/session"
)

// nearDeadline has a deadline closer than markerReserve but never expires,
// so the brief is built and only the marker's reserve can stop it.
type nearDeadline struct{ context.Context }

func (nearDeadline) Deadline() (time.Time, bool) { return time.Now().Add(markerReserve / 2), true }

// A brief built too close to the deadline is not given and leaves the
// briefed marker as it was, so the next prompt delivers it. That holds for
// the first brief and for the first-prompt refresh.
func TestBriefTooCloseToDeadlineIsRetried(t *testing.T) {
	env, p := setup(t)
	plant(t, env, "codex", "previous-session")
	p.Session = "late-session"
	markers := session.Markers{Root: env.Dirs.Sessions()}
	late := nearDeadline{context.Background()}
	if out := resumeContext(late, "claude-code", p, env, identity.General(), env.Now()); out != "" {
		t.Fatalf("brief given too close to the deadline: %q", out)
	}
	if _, found, err := markers.Get("claude-code", p.Session, session.Briefed); err != nil || found {
		t.Fatalf("undelivered brief left a marker: found=%v err=%v", found, err)
	}
	if out := resumeContext(context.Background(), "claude-code", p, env, identity.General(), env.Now()); out == "" || contextFrom(t, out) == "" {
		t.Fatal("next prompt did not retry the brief")
	}
	startup, _, _ := markers.Get("claude-code", p.Session, session.Briefed)
	if out := resumeContext(late, "claude-code", p, env, identity.General(), env.Now()); out != "" {
		t.Fatalf("refresh given too close to the deadline: %q", out)
	}
	if now, _, _ := markers.Get("claude-code", p.Session, session.Briefed); now != startup {
		t.Fatalf("undelivered refresh changed the marker from %s to %s", startup, now)
	}
	resumeContext(context.Background(), "claude-code", p, env, identity.General(), env.Now())
	if now, _, _ := markers.Get("claude-code", p.Session, session.Briefed); !strings.Contains(now, `"refreshed":true`) {
		t.Fatalf("next prompt did not retry the refresh: %s", now)
	}
}
