package resume

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/heyNag/eva-md/internal/okfadapter"
	"github.com/heyNag/eva-md/internal/threadpage"
)

// writeRules saves a rules page with the rules given, oldest first, as the
// update writes it.
func writeRules(t *testing.T, o Options, id string, general bool, rules ...string) {
	t.Helper()
	var ops []threadpage.RuleOp
	for i, r := range rules {
		ops = append(ops, threadpage.RuleOp{General: general, Action: "add", Text: r, Tag: "stated", Refs: []string{fmt.Sprintf("s:0000aaaa#%d", i+1)}, At: o.Now.Add(time.Duration(i-100) * time.Minute)})
	}
	p, _ := threadpage.ApplyRules(threadpage.RulesPage{Title: "rules"}, ops, general)
	w, err := okfadapter.Open(o.Config.WikiPath, o.Dirs.Locks())
	if err != nil {
		t.Fatal(err)
	}
	c := &okfadapter.Concept{Type: "rules", Title: "rules", Status: "stable", Body: threadpage.RenderRules(p)}
	if old, err := w.Read(id); err == nil {
		if _, err := w.Update(id, old.Hash, func(have *okfadapter.Concept) error { have.Body = c.Body; return nil }, "eva-md/test"); err != nil {
			t.Fatal(err)
		}
		return
	}
	if err := w.Create(id, c, "eva-md/test"); err != nil {
		t.Fatal(err)
	}
}

// The block a brief shows for briefFixture's two rules pages.
const rulesShown = rulesHead + "\nFor all projects:\n- Always run go vet before committing [stated] s:0000aaaa#1\n" +
	"For this project:\n- Never push on Fridays [stated] s:0000aaaa#2\n- Never mock the database in tests [stated] s:0000aaaa#1"

// Startup, clear and compact briefs on both hosts show the standing rules
// first: all projects', then this project's, newest first, without their
// hidden comments; a new chat with nothing to resume gets them with its
// footer.
func TestBriefShowsTheRulesFirst(t *testing.T) {
	for _, host := range []string{"claude-code", "codex"} {
		for _, variant := range []string{"startup", "clear", "compact", "first"} {
			t.Run(host+"/"+variant, func(t *testing.T) {
				o := briefFixture(t)
				o.Host, o.Variant = host, variant
				switch variant {
				case "compact":
					o.Session = "compact-chat"
					capture(t, o, host, o.Session, "main", o.Now.Add(-20*time.Minute), "Compact this chat.")
				case "first":
					o.Variant, o.Project.Key, o.Project.Slug, o.Project.Remote = "startup", "github.com__team__other", "other", "github.com/team/other"
				}
				writeRules(t, o, "general/rules", true, "Always run go vet before committing")
				writeRules(t, o, "projects/resume/rules", false, "Never mock the database in tests", "Never push on Fridays")
				r, err := Build(context.Background(), o)
				if err != nil {
					t.Fatal(err)
				}
				want := rulesShown
				if variant == "first" {
					// Another project: only the rules for all projects.
					want = rulesHead + "\nFor all projects:\n- Always run go vet before committing [stated] s:0000aaaa#1"
				}
				if _, body, _ := strings.Cut(r.Text, "eva-memory\n"); !strings.HasPrefix(body, want+"\n\n") || strings.Contains(r.Text, "<!--") {
					t.Fatalf("the rules are not first:\n%s", r.Text)
				}
				cap, _, _ := budgets(o.Host, o.Variant, false)
				if runes(r.Text) > cap {
					t.Fatalf("%d runes over the cap of %d", runes(r.Text), cap)
				}
			})
		}
	}
}

// The rules take at most 600 characters, whole lines only, all projects'
// first, and end with one line that counts the rest and names their pages.
func TestRulesBlockCaps(t *testing.T) {
	o := fixture(t)
	o.Dirs.Share = usualShare
	long := func(n int) []string {
		var out []string
		for i := 0; i < n; i++ {
			out = append(out, fmt.Sprintf("- Rule %d keeps the build green and the reviews short [stated] s:0000aaaa#%d", i, i))
		}
		return out
	}
	for _, c := range []struct {
		name             string
		general, project int
		more             string
	}{
		{"all fit", 2, 2, ""},
		{"this project's cut", 2, 12, "more: " + usualBin + " show projects/resume/rules"},
		{"both cut", 12, 3, "more: " + usualBin + " show general/rules, " + usualBin + " show projects/resume/rules"},
	} {
		s := standing{general: long(c.general), project: long(c.project), generalID: generalRules, projectID: "projects/resume/rules"}
		text := rulesBlock(o, s, rulesHead)
		lines := strings.Split(text, "\n")
		last := lines[len(lines)-1]
		shownRules := 0
		for _, l := range lines {
			if strings.HasPrefix(l, "- Rule ") {
				shownRules++
				if !strings.Contains(l, " [stated] s:0000aaaa#") {
					t.Errorf("%s: a cut line %q", c.name, l)
				}
			}
		}
		switch {
		case runes(text) > rulesCap || !strings.HasPrefix(text, rulesHead+"\nFor all projects:\n- Rule 0 "):
			t.Errorf("%s: %d runes:\n%s", c.name, runes(text), text)
		case c.more == "" && (shownRules != c.general+c.project || strings.Contains(text, "more:")):
			t.Errorf("%s: %d shown:\n%s", c.name, shownRules, text)
		case c.more != "" && last != fmt.Sprintf("%d %s", c.general+c.project-shownRules, c.more):
			t.Errorf("%s: last line %q", c.name, last)
		}
	}
	if rulesBlock(o, standing{}, rulesHead) != "" {
		t.Fatal("no rules, but a block")
	}
}

// A refresh follows the rules as it follows the Now: a changed rule reaches
// the chat, an unchanged one or a new id or time alone doesn't, and a marker
// written before there were rules counts them as unseen.
func TestRefreshFollowsTheRules(t *testing.T) {
	o := briefFixture(t)
	writeRules(t, o, "projects/resume/rules", false, "Never mock the database in tests")
	r, err := Build(context.Background(), o)
	if err != nil || !strings.Contains(r.NowHash, "+") {
		t.Fatalf("brief hash %q, %v", r.NowHash, err)
	}
	seen := r.NowHash
	o.Variant, o.PreviousHash, o.Slug = "refresh", seen, r.Slug
	if r, _ := Build(context.Background(), o); r.Text != "" || r.NowHash != seen {
		t.Fatalf("an unchanged page refreshed:\n%s", r.Text)
	}
	// A marker from before the rules has the Now's hash alone.
	o.PreviousHash, _ = splitHash(seen)
	if r, _ := Build(context.Background(), o); !strings.Contains(r.Text, changedRulesHead+"\nFor this project:\n- Never mock the database in tests") || strings.Contains(r.Text, "Now (as of") {
		t.Fatalf("the rules a chat never saw:\n%s", r.Text)
	}
	o.PreviousHash = seen
	writeRules(t, o, "projects/resume/rules", false, "Never mock the database in tests", "Never push on Fridays")
	r, _ = Build(context.Background(), o)
	if !strings.Contains(r.Text, "- Never push on Fridays [stated] s:0000aaaa#2\n- Never mock") || r.NowHash == seen || !r.Found {
		t.Fatalf("a changed rule:\n%s", r.Text)
	}
	// Only the hidden comment changed: nothing new to show.
	w, _ := okfadapter.Open(o.Config.WikiPath, o.Dirs.Locks())
	pg, _ := w.Read("projects/resume/rules")
	body := strings.Replace(pg.Concept.Body, "i:r2", "i:r7", 1)
	if _, err := w.Update("projects/resume/rules", pg.Hash, func(c *okfadapter.Concept) error { c.Body = body; return nil }, "eva-md/test"); err != nil {
		t.Fatal(err)
	}
	o.PreviousHash = r.NowHash
	if again, _ := Build(context.Background(), o); again.Text != "" {
		t.Fatalf("an id alone refreshed:\n%s", again.Text)
	}
	// A resumed chat is shown the rules that changed since it last saw them.
	o.Variant, o.Host, o.Session, o.PreviousHash = "resume", "codex", "system-chat", seen
	if r, _ := Build(context.Background(), o); !strings.Contains(r.Text, changedRulesHead) {
		t.Fatalf("a resumed chat:\n%s", r.Text)
	}
}
