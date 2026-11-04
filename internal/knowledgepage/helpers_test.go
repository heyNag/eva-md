package knowledgepage

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/heyNag/eva-md/internal/coderef"
	"github.com/heyNag/eva-md/internal/identity"
	"github.com/heyNag/eva-md/internal/okfadapter"
	"github.com/heyNag/eva-md/internal/paths"
	"github.com/heyNag/eva-md/internal/testgitenv"
	"github.com/heyNag/eva-md/internal/wikigit"
	"github.com/heyNag/eva-md/internal/wikiinit"
)

// Tests check what the package does, not how fast the machine runs git or
// takes a lock: the limits that protect a hook or a job from slow git and a
// busy wiki lock are raised for every test, before any test runs.
func TestMain(m *testing.M) {
	testgitenv.Clear()
	identity.Timeout, identity.WaitDelay = 10*time.Minute, 10*time.Minute
	wikigit.GitTimeout, wikigit.CommitTimeout, wikigit.PushTimeout, wikigit.WaitDelay = 10*time.Minute, 10*time.Minute, 10*time.Minute, 10*time.Minute
	wikiinit.GitTimeout = 10 * time.Minute
	okfadapter.LockTimeout = 10 * time.Minute
	coderef.WaitDelay = 10 * time.Minute
	os.Exit(m.Run())
}

// The fixtures' project, commit, author and times.
const (
	project = "github.com__acme__eva"
	other   = "github.com__acme__billing"
	commit  = "4e2bf4c"
	actor   = "claude-code/claude-opus-5-5"
)

var (
	plusOne = time.FixedZone("", 3600)
	at1     = time.Date(2026, 10, 28, 10, 0, 0, 0, plusOne)
	at2     = time.Date(2026, 10, 31, 0, 30, 0, 0, plusOne)
)

// isolate gives the test a temporary home whose git config is a temporary
// file, and clears the variables that would point git elsewhere.
func isolate(t *testing.T) string {
	t.Helper()
	home, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(home, "gitconfig"))
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	for _, name := range []string{"GIT_CONFIG_COUNT", "GIT_CONFIG_PARAMETERS", "GIT_DIR", "GIT_WORK_TREE", "GIT_INDEX_FILE", "GIT_COMMON_DIR", "GIT_TEMPLATE_DIR"} {
		t.Setenv(name, "")
	}
	if err := os.WriteFile(filepath.Join(home, "gitconfig"), []byte("[user]\nname = Eva Fixture\nemail = fixture@example.invalid\n[commit]\ngpgsign = false\n[init]\ndefaultBranch = main\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return home
}

// fixture makes a wiki as setup does, with wikiinit, in a temporary home,
// and opens it.
func fixture(t *testing.T) *okfadapter.Wiki {
	t.Helper()
	home := isolate(t)
	dirs, err := paths.Default()
	if err != nil {
		t.Fatal(err)
	}
	remote := filepath.Join(home, "remote.git")
	if _, err := wikigit.Run(context.Background(), time.Minute, home, "init", "--bare", "--quiet", remote); err != nil {
		t.Fatal(err)
	}
	res, err := wikiinit.Run(context.Background(), wikiinit.Options{Remote: remote, Dir: filepath.Join(home, "wiki"), Dirs: dirs, Version: "test", NoPush: true})
	if err != nil {
		t.Fatal(err)
	}
	w, err := okfadapter.Open(res.WikiPath, dirs.Locks())
	if err != nil {
		t.Fatal(err)
	}
	return w
}

// meta is what the fixtures' saves stamp.
func meta() SaveMeta {
	return SaveMeta{Project: project, Commit: commit, Actor: actor, Version: "test", Now: func() time.Time { return at2 }}
}

// save runs Save and fails the test on an error.
func save(t *testing.T, w *okfadapter.Wiki, m SaveMeta, ops ...Op) Result {
	t.Helper()
	r, err := Save(w, ops, m)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

// readFile reads a file of the wiki, or "" when it is missing.
func readFile(t *testing.T, w *okfadapter.Wiki, rel string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(w.Root(), filepath.FromSlash(rel)))
	if os.IsNotExist(err) {
		return ""
	}
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// withoutGenerated drops the generated line okf stamps with its own clock.
func withoutGenerated(s string) string {
	var out []string
	for _, l := range strings.SplitAfter(s, "\n") {
		if !strings.HasPrefix(l, "generated: ") {
			out = append(out, l)
		}
	}
	return strings.Join(out, "")
}

// snapshot reads every file of the wiki outside .git, by path.
func snapshot(t *testing.T, w *okfadapter.Wiki) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.WalkDir(w.Root(), func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && d.Name() == ".git" {
			return filepath.SkipDir
		}
		if d.Type().IsRegular() {
			data, err := os.ReadFile(p)
			if err != nil {
				return err
			}
			rel, _ := filepath.Rel(w.Root(), p)
			out[filepath.ToSlash(rel)] = string(data)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// validate fails the test on any okf finding.
func validate(t *testing.T, w *okfadapter.Wiki) {
	t.Helper()
	v, err := w.Validate()
	if err != nil {
		t.Fatal(err)
	}
	if len(v.Errors)+len(v.Warnings)+len(v.Orphans)+len(v.BrokenLinks) != 0 {
		t.Fatalf("okf findings: errors %q, warnings %q, orphans %q, broken links %+v", v.Errors, v.Warnings, v.Orphans, v.BrokenLinks)
	}
}

// entry reads a saved page through okf and models it.
func entry(t *testing.T, w *okfadapter.Wiki, id int) Entry {
	t.Helper()
	pg, err := w.Read(Ref(id))
	if err != nil {
		t.Fatal(err)
	}
	e, err := Read(pg.Concept)
	if err != nil {
		t.Fatal(err)
	}
	return e
}

// edit rewrites a page's file by hand, as a person would.
func edit(t *testing.T, w *okfadapter.Wiki, id int, from, to string) {
	t.Helper()
	editFile(t, w, Folder+"/"+Name(id)+".md", from, to)
}

// editFile rewrites a file of the wiki by hand.
func editFile(t *testing.T, w *okfadapter.Wiki, rel, from, to string) {
	t.Helper()
	p := filepath.Join(w.Root(), filepath.FromSlash(rel))
	data, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), from) {
		t.Fatalf("%s holds no %q", rel, from)
	}
	if err := os.WriteFile(p, []byte(strings.Replace(string(data), from, to, 1)), 0o644); err != nil {
		t.Fatal(err)
	}
}
