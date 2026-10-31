package update

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/heyNag/eva-md/internal/identity"
	"github.com/heyNag/eva-md/internal/okfadapter"
	"github.com/heyNag/eva-md/internal/threadpage"
)

// A project's first thread page goes in after its now.md, which names the
// project's remote, so a pass that dies before writing the overview leaves
// a folder that proves its project's. Another project of the same name then
// gets a folder of its own, never the first one's page.
func TestFirstThreadPageClaimsTheFolder(t *testing.T) {
	e := newEnv(t)
	w := e.wiki()
	x := identity.Project{Kind: identity.KindRepo, Key: "github.com__x__foo", Slug: "foo", Remote: "github.com/x/foo", Branch: "main"}
	y := identity.Project{Kind: identity.KindRepo, Key: "github.com__y__foo", Slug: "foo", Remote: "github.com/y/foo", Branch: "main"}
	a, err := findArea(w, x)
	if err != nil || a.dir != "projects/foo" {
		t.Fatalf("first project: %+v %v", a, err)
	}
	th, err := readThread(w, a, "main")
	if err != nil {
		t.Fatal(err)
	}
	f := facts{asOf: start, source: "s:3f2a91c0", project: a.slug, writer: okfadapter.Actor("eva-md", "0.1.0-test"), actor: okfadapter.Actor(HostClaude, "")}
	// The pass dies after this save, before refreshArea.
	if _, wrote, _, err := saveThread(w, a, th, threadpage.Update{Summary: "x's own work."}, f); err != nil || !wrote {
		t.Fatalf("save: %v", err)
	}
	if again, err := findArea(w, x); err != nil || again.dir != a.dir {
		t.Fatalf("the first project again: %+v %v", again, err)
	}
	b, err := findArea(w, y)
	if err != nil || b.dir == a.dir {
		t.Fatalf("the second project took %+v: %v", b, err)
	}
	if now, err := w.Read("projects/foo/now"); err != nil || now.Concept.Extra["remote"] != "github.com/x/foo" {
		t.Fatalf("now.md: %+v %v", now, err)
	}
}

// A folder of the project's slug whose now page can't be read may belong
// to another project, so the project gets a folder of its own, and the
// damaged page is left as it is.
func TestADamagedNowPageKeepsItsFolder(t *testing.T) {
	e := newEnv(t)
	w := e.wiki()
	dir := filepath.Join(w.Root(), "projects", "foo")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	damaged := "---\ntype: project\n"
	if err := os.WriteFile(filepath.Join(dir, "now.md"), []byte(damaged), 0o600); err != nil {
		t.Fatal(err)
	}
	x := identity.Project{Kind: identity.KindRepo, Key: "github.com__x__foo", Slug: "foo", Remote: "github.com/x/foo", Branch: "main"}
	a, err := findArea(w, x)
	if err != nil || a.dir == "projects/foo" {
		t.Fatalf("the project took the folder of a damaged now page: %+v %v", a, err)
	}
	if got, err := os.ReadFile(filepath.Join(dir, "now.md")); err != nil || string(got) != damaged {
		t.Fatalf("the damaged now page changed: %q %v", got, err)
	}
}

// The Projects line goes into the root index under the wiki lock, so a write
// that adds another line meanwhile, as system write adds its Systems line
// under the same lock, waits for it and both lines stay.
func TestProjectsLineKeepsAConcurrentIndexLine(t *testing.T) {
	e := newEnv(t)
	w := e.wiki()
	systems := okfadapter.IndexLine("Systems", "systems/index.md", "The systems the projects share")
	done := make(chan error, 1)
	saved := beforeLinkWrite
	t.Cleanup(func() { beforeLinkWrite = saved })
	beforeLinkWrite = func() {
		go func() {
			done <- w.Locked(func(lw *okfadapter.Wiki) error {
				data, err := readWikiFile(lw, "index.md")
				if err != nil {
					return err
				}
				return lw.WriteFile("index.md", []byte(data+systems+"\n"))
			})
		}()
		// Time for the other write to take the lock, were it free.
		time.Sleep(100 * time.Millisecond)
	}
	c := newChat("3f2a91c0-link", start)
	talk(c, 2, "amber")
	e.write(c)
	if s := e.run(c); outcomes(s) != "ok" {
		t.Fatalf("outcomes %q\n%s", outcomes(s), e.log)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	index, err := os.ReadFile(filepath.Join(e.cfg.WikiPath, "index.md"))
	if err != nil || !strings.Contains(string(index), "](projects/index.md)") || !strings.Contains(string(index), systems+"\n") {
		t.Fatalf("root index %v:\n%s", err, index)
	}
}
