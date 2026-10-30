package resume

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/heyNag/eva-md/internal/okfadapter"
)

// threadPage adds a branch's thread page to a wiki made by wiki().
func threadPage(t *testing.T, o Options, branch string, asOf time.Time, now string) {
	t.Helper()
	w, err := okfadapter.Open(o.Config.WikiPath, o.Dirs.Locks())
	if err != nil {
		t.Fatal(err)
	}
	c := &okfadapter.Concept{Type: "thread", Title: branch, Extra: map[string]any{"as_of": asOf.Format(time.RFC3339), "branch": branch}, Body: "## Now\n<!-- eva:now:start -->\n" + now + "\n<!-- eva:now:end -->\n## Timeline\nPast."}
	if err := w.Create("projects/resume/threads/"+ThreadName(branch), c, "eva-md/test"); err != nil {
		t.Fatal(err)
	}
}

// newRepo makes a repository with one commit on branch. Call it after
// fixture, which points git at a temporary global config.
func newRepo(t *testing.T, branch string) string {
	t.Helper()
	repo := filepath.Join(t.TempDir(), "repo")
	if err := os.Mkdir(repo, 0o700); err != nil {
		t.Fatal(err)
	}
	gitOut(t, repo, "init", "-b", branch)
	gitOut(t, repo, "config", "user.name", "Fixture")
	gitOut(t, repo, "config", "user.email", "fixture@example.invalid")
	gitOut(t, repo, "config", "commit.gpgsign", "false")
	gitOut(t, repo, "commit", "--allow-empty", "-m", "initial")
	return repo
}

func gitOut(t *testing.T, repo string, args ...string) string {
	t.Helper()
	out, err := exec.Command("git", append([]string{"-C", repo}, args...)...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v %s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

// Resuming or forking the only chat on feature-x must never inject
// main's Now, whether feature-x has no page, an older page or a newer one.
func TestResumeNeverInjectsAnotherBranchNow(t *testing.T) {
	for _, variant := range []string{"resume", "fork"} {
		for _, page := range []string{"none", "older", "newer"} {
			t.Run(variant+"/"+page, func(t *testing.T) {
				o := fixture(t)
				o.Variant = variant
				o.Project.Branch = "feature-x"
				o.Host, o.Session = "claude-code", "feature-chat"
				capture(t, o, "codex", "main-chat", "main", o.Now.Add(-3*time.Hour), "main work")
				capture(t, o, o.Host, o.Session, "feature-x", o.Now.Add(-2*time.Hour), "feature work")
				// main's Now is newer than this chat's last work.
				wiki(t, o, o.Now.Add(-time.Hour), "MAIN_NOW_TEXT")
				switch page {
				case "older":
					threadPage(t, o, "feature-x", o.Now.Add(-3*time.Hour), "FEATURE_NOW_TEXT")
				case "newer":
					threadPage(t, o, "feature-x", o.Now.Add(-time.Hour), "FEATURE_NOW_TEXT")
				}
				r, err := Build(context.Background(), o)
				if err != nil {
					t.Fatal(err)
				}
				if strings.Contains(r.Text, "MAIN_NOW_TEXT") {
					t.Fatalf("%s took main's Now:\n%s", variant, r.Text)
				}
				if page == "newer" {
					if !strings.Contains(r.Text, "Updated Now:\nFEATURE_NOW_TEXT") {
						t.Fatalf("own newer Now missing:\n%s", r.Text)
					}
				} else if r.Text != "" || r.Found {
					t.Fatalf("nothing changed on feature-x, got:\n%s", r.Text)
				}
			})
		}
	}
}

// A resumed chat still reports live git when the project has earlier
// work, but takes neither main's Now nor main's page head.
func TestResumeReportsLiveGitWithoutAnotherBranchPage(t *testing.T) {
	o := fixture(t)
	repo := newRepo(t, "feature-x")
	first := gitOut(t, repo, "rev-parse", "HEAD")
	gitOut(t, repo, "commit", "--allow-empty", "-m", "second")
	if err := os.WriteFile(filepath.Join(repo, "dirty.txt"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	o.Project.Root = repo
	o.Project.Branch = "feature-x"
	o.Variant = "resume"
	o.Host, o.Session = "claude-code", "feature-chat"
	capture(t, o, "codex", "main-chat", "main", o.Now.Add(-3*time.Hour), "main work")
	capture(t, o, o.Host, o.Session, "feature-x", o.Now.Add(-2*time.Hour), "feature work")
	wiki(t, o, o.Now.Add(-time.Hour), "MAIN_NOW_TEXT")
	w, err := okfadapter.Open(o.Config.WikiPath, o.Dirs.Locks())
	if err != nil {
		t.Fatal(err)
	}
	pg, err := w.Read("projects/resume/threads/main")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Update(pg.ID, pg.Hash, func(c *okfadapter.Concept) error { c.Extra["head"] = first; return nil }, "eva-md/test"); err != nil {
		t.Fatal(err)
	}
	r, err := Build(context.Background(), o)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(r.Text, "Live git:") || !strings.Contains(r.Text, "dirty.txt") {
		t.Fatalf("live git missing:\n%s", r.Text)
	}
	if strings.Contains(r.Text, "MAIN_NOW_TEXT") || strings.Contains(r.Text, "HEAD moved") {
		t.Fatalf("resume used main's page:\n%s", r.Text)
	}
}

// A new chat on a branch with its own Now page keeps that page, even
// with no chat of its own in the history window, instead of continuing
// from main.
func TestStartupKeepsOwnBranchNow(t *testing.T) {
	for _, variant := range []string{"startup", "clear"} {
		t.Run(variant, func(t *testing.T) {
			o := fixture(t)
			o.Variant = variant
			o.Project.Branch = "feature-x"
			capture(t, o, "codex", "main-chat", "main", o.Now.Add(-time.Hour), "MAIN_EXCHANGE")
			wiki(t, o, o.Now.Add(-30*time.Minute), "MAIN_NOW_TEXT")
			asOf := o.Now.Add(-2 * time.Hour)
			threadPage(t, o, "feature-x", asOf, "FEATURE_NOW_TEXT")
			r, err := Build(context.Background(), o)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(r.Text, "Now (as of "+asOf.Format(time.RFC3339)+"):\nFEATURE_NOW_TEXT") || !strings.Contains(r.Text, "Thread: projects/resume/threads/feature-x.md") {
				t.Fatalf("own Now dropped:\n%s", r.Text)
			}
			for _, unwanted := range []string{"MAIN_NOW_TEXT", "MAIN_EXCHANGE", "Continuing from", "lineage"} {
				if strings.Contains(r.Text, unwanted) {
					t.Fatalf("found %q:\n%s", unwanted, r.Text)
				}
			}
		})
	}
}

// When lineage does apply (no chat and no page on this branch), the
// borrowed Now names its branch.
func TestStartupLineageNowNamesItsBranch(t *testing.T) {
	for _, variant := range []string{"startup", "clear"} {
		t.Run(variant, func(t *testing.T) {
			o := fixture(t)
			o.Variant = variant
			o.Project.Branch = "feature-x"
			capture(t, o, "codex", "main-chat", "main", o.Now.Add(-time.Hour), "MAIN_EXCHANGE")
			asOf := o.Now.Add(-30 * time.Minute)
			wiki(t, o, asOf, "MAIN_NOW_TEXT")
			r, err := Build(context.Background(), o)
			if err != nil {
				t.Fatal(err)
			}
			for _, want := range []string{"Continuing from main", "Now from main (lineage, as of " + asOf.Format(time.RFC3339) + "):\nMAIN_NOW_TEXT", "MAIN_EXCHANGE", "Thread: projects/resume/threads/main.md"} {
				if !strings.Contains(r.Text, want) {
					t.Fatalf("missing %q:\n%s", want, r.Text)
				}
			}
			if strings.Contains(r.Text, "\nNow (") {
				t.Fatalf("lineage Now unlabelled:\n%s", r.Text)
			}
		})
	}
}
