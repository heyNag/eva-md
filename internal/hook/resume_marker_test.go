package hook

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/heyNag/eva-md/internal/identity"
	"github.com/heyNag/eva-md/internal/okfadapter"
	"github.com/heyNag/eva-md/internal/session"
)

// nearDeadline has a deadline closer than markerReserve but never expires,
// so the brief is built and only the marker's reserve can stop it.
type nearDeadline struct{ context.Context }

func (nearDeadline) Deadline() (time.Time, bool) { return time.Now().Add(markerReserve / 2), true }

// generalNow saves now as the Now of the general area's thread page, as an
// update would, making the wiki the first time.
func generalNow(t *testing.T, env *Env, now string) {
	t.Helper()
	if env.Config.WikiPath == "" {
		env.Config.WikiPath = filepath.Join(os.Getenv("HOME"), "wiki")
		if err := okfadapter.Init(env.Config.WikiPath); err != nil {
			t.Fatal(err)
		}
	}
	w, err := okfadapter.Open(env.Config.WikiPath, env.Dirs.Locks())
	if err != nil {
		t.Fatal(err)
	}
	body := "## Now\n<!-- eva:now:start -->\n" + now + "\n<!-- eva:now:end -->\n## Timeline\n"
	asOf := env.Now().Format(time.RFC3339)
	page, err := w.Read("general/threads/default")
	if err != nil {
		c := &okfadapter.Concept{Type: "thread", Title: "General", Extra: map[string]any{"as_of": asOf, "branch": "default"}, Body: body}
		if err := w.Create("general/threads/default", c, "eva-md/test"); err != nil {
			t.Fatal(err)
		}
		return
	}
	if _, err := w.Update(page.ID, page.Hash, func(c *okfadapter.Concept) error { c.Body = body; c.Extra["as_of"] = asOf; return nil }, "eva-md/test"); err != nil {
		t.Fatal(err)
	}
}

// A brief built too close to the deadline is not given and leaves the
// briefed marker as it was, so the next prompt delivers it. That holds for
// the first brief and for a refresh.
func TestBriefTooCloseToDeadlineIsRetried(t *testing.T) {
	env, p := setup(t)
	plant(t, env, "codex", "previous-session")
	generalNow(t, &env, "### Next\n- Write the parser")
	p.Session = "late-session"
	markers := session.Markers{Root: env.Dirs.Sessions()}
	late := nearDeadline{context.Background()}
	if out := resumeContext(late, "claude-code", p, env, identity.General(), env.Now(), startState{}, new([][]string)); out != "" {
		t.Fatalf("brief given too close to the deadline: %q", out)
	}
	if _, found, err := markers.Get("claude-code", p.Session, session.Briefed); err != nil || found {
		t.Fatalf("undelivered brief left a marker: found=%v err=%v", found, err)
	}
	if out := resumeContext(context.Background(), "claude-code", p, env, identity.General(), env.Now(), startState{}, new([][]string)); out == "" || contextFrom(t, out) == "" {
		t.Fatal("next prompt did not retry the brief")
	}
	startup, _, _ := markers.Get("claude-code", p.Session, session.Briefed)
	generalNow(t, &env, "### Next\n- Test the parser")
	if out := resumeContext(late, "claude-code", p, env, identity.General(), env.Now(), startState{}, new([][]string)); out != "" {
		t.Fatalf("refresh given too close to the deadline: %q", out)
	}
	if now, _, _ := markers.Get("claude-code", p.Session, session.Briefed); now != startup {
		t.Fatalf("undelivered refresh changed the marker from %s to %s", startup, now)
	}
	if out := resumeContext(context.Background(), "claude-code", p, env, identity.General(), env.Now(), startState{}, new([][]string)); !strings.Contains(contextFrom(t, out), "Test the parser") {
		t.Fatalf("next prompt did not retry the refresh: %q", out)
	}
	if now, _, _ := markers.Get("claude-code", p.Session, session.Briefed); !strings.Contains(now, `"refreshes":1`) || now == startup {
		t.Fatalf("delivered refresh not recorded: %s", now)
	}
}
