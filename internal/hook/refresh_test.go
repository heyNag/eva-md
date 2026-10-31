package hook

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/heyNag/eva-md/internal/okfadapter"
	"github.com/heyNag/eva-md/internal/session"
)

// After a brief, each prompt compares the thread's Now with the one the chat
// last saw. An update that lands after prompt 1 arrives with prompt 2, prompt
// 3 gets nothing, and no more than 2 refreshes follow a brief. A prompt that
// injects nothing leaves the marker as it was.
func TestRefreshFollowsTheNowHash(t *testing.T) {
	for _, host := range []string{"claude-code", "codex"} {
		t.Run(host, func(t *testing.T) {
			env, p := setup(t)
			generalNow(t, &env, "### Next\n- Write the parser")
			p.Event, p.Source = "SessionStart", "startup"
			if context := contextFrom(t, invoke(t, host, p, env)); !strings.Contains(context, "- Write the parser") {
				t.Fatalf("brief without the Now: %s", context)
			}
			markers := session.Markers{Root: env.Dirs.Sessions()}
			p.Event, p.Source = "UserPromptSubmit", ""
			prompt := func(n int, want string) {
				t.Helper()
				before, _, _ := markers.Get(host, p.Session, session.Briefed)
				p.Turn = "turn-" + string(rune('0'+n))
				out := invoke(t, host, p, env)
				if want == "" {
					if after, _, _ := markers.Get(host, p.Session, session.Briefed); out != "" || after != before {
						t.Fatalf("prompt %d: unexpected refresh %q, marker %s then %s", n, out, before, after)
					}
					return
				}
				context := contextFrom(t, out)
				if !strings.Contains(context, ", changed since it was last shown in this chat:\n### Next\n"+want) || strings.Contains(out, "systemMessage") {
					t.Fatalf("prompt %d: refresh without %q: %q", n, want, out)
				}
			}
			prompt(1, "")
			generalNow(t, &env, "### Next\n- Test the parser")
			prompt(2, "- Test the parser")
			prompt(3, "")
			generalNow(t, &env, "### Next\n- Ship the parser")
			prompt(4, "- Ship the parser")
			generalNow(t, &env, "### Next\n- Document the parser")
			prompt(5, "")
			marker, _, _ := markers.Get(host, p.Session, session.Briefed)
			if !strings.Contains(marker, `"refreshes":2`) {
				t.Fatalf("marker=%s", marker)
			}
		})
	}
}

// A compact brief is followed by refreshes too, so a Now saved after the
// compaction reaches the running chat. The count runs on through it: 2
// refreshes per chat, however often it is compacted. A /clear starts it
// again.
func TestRefreshAfterCompactBrief(t *testing.T) {
	env, p := setup(t)
	plant(t, env, "codex", p.Session)
	generalNow(t, &env, "### Next\n- Keep the compact plan")
	markers := session.Markers{Root: env.Dirs.Sessions()}
	brief := func(source string, want ...string) {
		t.Helper()
		p.Event, p.Source = "SessionStart", source
		context := contextFrom(t, invoke(t, "codex", p, env))
		for _, w := range want {
			if !strings.Contains(context, w) {
				t.Fatalf("%s brief without %q: %s", source, w, context)
			}
		}
		p.Event, p.Source, p.Prompt = "UserPromptSubmit", "", "carry on"
	}
	update := func(now string, refreshed bool) {
		t.Helper()
		generalNow(t, &env, "### Next\n- "+now)
		out := invoke(t, "codex", p, env)
		if refreshed != (out != "") || refreshed && !strings.Contains(contextFrom(t, out), "- "+now) {
			t.Fatalf("update %q: refreshed %v, want %v: %q", now, out != "", refreshed, out)
		}
	}
	brief("compact", "Keep the compact plan", "Windows")
	before, _, _ := markers.Get("codex", p.Session, session.Briefed)
	if out := invoke(t, "codex", p, env); out != "" {
		t.Fatalf("unchanged Now refreshed: %q", out)
	}
	if after, _, _ := markers.Get("codex", p.Session, session.Briefed); after != before {
		t.Fatalf("an unchanged Now changed the marker from %s to %s", before, after)
	}
	update("Ship the compact fix", true)
	brief("compact", "Ship the compact fix")
	if marker, _, _ := markers.Get("codex", p.Session, session.Briefed); !strings.Contains(marker, `"variant":"compact","refreshes":1`) {
		t.Fatalf("the compact brief reset the count: %s", marker)
	}
	update("Test the compact fix", true)
	brief("compact", "Test the compact fix")
	update("Document the compact fix", false)
	brief("clear", "Document the compact fix")
	update("Release the compact fix", true)
}

// A marker saved before the count, with its one refresh sent, has one
// refresh left.
func TestOldRefreshedMarkerCountsOneRefresh(t *testing.T) {
	env, p := setup(t)
	generalNow(t, &env, "### Next\n- First update")
	markers := session.Markers{Root: env.Dirs.Sessions()}
	if err := markers.Set("codex", p.Session, session.Briefed, `{"now_hash":"older","variant":"startup","refreshed":true}`); err != nil {
		t.Fatal(err)
	}
	if out := invoke(t, "codex", p, env); !strings.Contains(contextFrom(t, out), "- First update") {
		t.Fatalf("the refresh left was not sent: %q", out)
	}
	if marker, _, _ := markers.Get("codex", p.Session, session.Briefed); !strings.Contains(marker, `"refreshes":2`) || strings.Contains(marker, "refreshed") {
		t.Fatalf("marker=%s", marker)
	}
	generalNow(t, &env, "### Next\n- Second update")
	if out := invoke(t, "codex", p, env); out != "" {
		t.Fatalf("a third refresh was sent: %q", out)
	}
}

// The briefed marker keeps the wiki folder the brief found for the project,
// here under another name than the repo's, and a refresh reads only that
// folder: a marker without it finds nothing there.
func TestRefreshReadsTheBriefsSlug(t *testing.T) {
	env, p := setup(t)
	git(t, p.Cwd, "init", "--quiet", "-b", "main")
	git(t, p.Cwd, "remote", "add", "origin", "https://github.com/team/proj.git")
	git(t, p.Cwd, "-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid", "-c", "commit.gpgsign=false", "commit", "--allow-empty", "--quiet", "-m", "fixture")
	env.Config.WikiPath = filepath.Join(os.Getenv("HOME"), "wiki")
	if err := okfadapter.Init(env.Config.WikiPath); err != nil {
		t.Fatal(err)
	}
	w, err := okfadapter.Open(env.Config.WikiPath, env.Dirs.Locks())
	if err != nil {
		t.Fatal(err)
	}
	thread := "projects/team-proj/threads/main"
	body := func(now string) string { return "## Now\n<!-- eva:now:start -->\n" + now + "\n<!-- eva:now:end -->\n" }
	asOf := env.Now().Format(time.RFC3339)
	if err := w.Create("projects/team-proj/now", &okfadapter.Concept{Type: "now", Title: "Proj", Extra: map[string]any{"remote": "github.com/team/proj"}, Body: "[main](threads/main.md)"}, "eva-md/test"); err != nil {
		t.Fatal(err)
	}
	if err := w.Create(thread, &okfadapter.Concept{Type: "thread", Title: "Main", Extra: map[string]any{"as_of": asOf, "branch": "main"}, Body: body("### Next\n- Alias first")}, "eva-md/test"); err != nil {
		t.Fatal(err)
	}
	update := func(now string) {
		t.Helper()
		page, err := w.Read(thread)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Update(page.ID, page.Hash, func(c *okfadapter.Concept) error { c.Body = body(now); return nil }, "eva-md/test"); err != nil {
			t.Fatal(err)
		}
	}
	p.Event, p.Source = "SessionStart", "startup"
	if context := contextFrom(t, invoke(t, "codex", p, env)); !strings.Contains(context, "- Alias first") {
		t.Fatalf("brief without the Now: %s", context)
	}
	markers := session.Markers{Root: env.Dirs.Sessions()}
	marker, _, _ := markers.Get("codex", p.Session, session.Briefed)
	if !strings.Contains(marker, `"slug":"team-proj"`) {
		t.Fatalf("marker=%s", marker)
	}
	p.Event, p.Source = "UserPromptSubmit", ""
	update("### Next\n- Alias second")
	if out := invoke(t, "codex", p, env); !strings.Contains(contextFrom(t, out), "- Alias second") {
		t.Fatalf("no refresh from the brief's folder: %q", out)
	}
	if err := markers.Set("codex", p.Session, session.Briefed, strings.Replace(marker, `,"slug":"team-proj"`, "", 1)); err != nil {
		t.Fatal(err)
	}
	update("### Next\n- Alias third")
	if out := invoke(t, "codex", p, env); out != "" {
		t.Fatalf("a refresh looked beyond the project's own folder: %q", out)
	}
}
