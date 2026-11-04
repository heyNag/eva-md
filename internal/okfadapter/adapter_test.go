package okfadapter

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/okf-memory/okf-agent-memory/pkg/okf"

	"github.com/heyNag/eva-md/internal/testgitenv"
)

// Tests check what the adapter writes, not how fast the machine syncs its
// disk: the wait for the wiki lock, which concurrent writes in a test hold
// while they save, is raised for every test, before any test runs.
func TestMain(m *testing.M) {
	testgitenv.Clear()
	LockTimeout = 10 * time.Minute
	os.Exit(m.Run())
}

func isolated(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(dir, "gitconfig"))
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	// A runner launched from a Git hook can inherit repository and injected
	// config variables. Never let fixture Git commands act on that repository.
	for _, key := range []string{"GIT_DIR", "GIT_WORK_TREE", "GIT_INDEX_FILE", "GIT_COMMON_DIR", "GIT_OBJECT_DIRECTORY", "GIT_ALTERNATE_OBJECT_DIRECTORIES", "GIT_NAMESPACE", "GIT_PREFIX", "GIT_CONFIG", "GIT_CONFIG_COUNT", "GIT_CONFIG_PARAMETERS", "GIT_TEMPLATE_DIR"} {
		t.Setenv(key, "")
		if err := os.Unsetenv(key); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func testWiki(t *testing.T) *Wiki {
	t.Helper()
	dir := isolated(t)
	root := filepath.Join(dir, "wiki")
	if err := Init(root); err != nil {
		t.Fatal(err)
	}
	w, err := Open(root, filepath.Join(dir, "locks"))
	if err != nil {
		t.Fatal(err)
	}
	return w
}

func concept(title, body string) *Concept {
	return &Concept{Type: "thread", Title: title, Description: "Fixture page", Status: "stable", Body: body}
}

func mustCreate(t *testing.T, w *Wiki, id string, c *Concept) *Page {
	t.Helper()
	if err := w.Create(id, c, Actor("codex", "fixture")); err != nil {
		t.Fatal(err)
	}
	p, err := w.Read(id)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func mustRead(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(name)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func writeFixture(t *testing.T, w *Wiki, name, content string) {
	t.Helper()
	if err := w.WriteFile(name, []byte(content)); err != nil {
		t.Fatal(err)
	}
}

func TestInitOpen(t *testing.T) {
	w := testWiki(t)
	if !filepath.IsAbs(w.Root()) {
		t.Fatal("root is not absolute")
	}
	before := mustRead(t, filepath.Join(w.root, "log.md"))
	if !bytes.Contains(before, []byte("Initialized the eva-md wiki")) {
		t.Fatalf("unexpected initial log: %q", before)
	}
	// The root index is eva-md's own, so okf writes none naming it.
	if index := mustRead(t, filepath.Join(w.root, "index.md")); string(index) != RootIndex {
		t.Fatalf("unexpected root index: %q", index)
	}
	// Init on a wiki keeps its root index and log, which hold text Init
	// never writes.
	kept := map[string]string{"index.md": RootIndex + IndexLine("Page", "page.md", "Kept") + "\n", "log.md": "## 2001-02-03\n* **Creation**: Kept.\n"}
	for name, text := range kept {
		if err := os.WriteFile(filepath.Join(w.root, name), []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := Init(w.root); err != nil {
		t.Fatal(err)
	}
	for name, text := range kept {
		if got := string(mustRead(t, filepath.Join(w.root, name))); got != text {
			t.Fatalf("Init rewrote %s: %q", name, got)
		}
	}
	alias := filepath.Join(filepath.Dir(w.root), "alias")
	if err := os.Symlink(w.root, alias); err != nil {
		t.Fatal(err)
	}
	w2, err := Open(alias, w.lockDir)
	if err != nil || w2.Root() != w.root {
		t.Fatalf("Open symlink = %v, %v", w2, err)
	}
	if err := os.Mkdir(filepath.Join(w.root, "knowledge"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(filepath.Join(w.root, "index.md"), filepath.Join(w.root, "knowledge", "index.md")); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(w.root, w.lockDir); !errors.Is(err, ErrNoRoot) {
		t.Fatalf("Open missing root = %v, want ErrNoRoot", err)
	}
	if _, err := w.Validate(); !errors.Is(err, ErrNoRoot) {
		t.Fatalf("Validate missing root = %v, want ErrNoRoot", err)
	}
}

func TestCreateUpdateAndNoOp(t *testing.T) {
	w := testWiki(t)
	c := concept("Title", "# First\n\nContent.")
	p := mustCreate(t, w, "general/threads/default", c)
	if c.ID != "" || c.Generated != nil {
		t.Fatal("Create modified the input concept")
	}
	if p.ID != "general/threads/default" || p.Hash != hashOf(p.Raw) || p.Concept.Generated.By != "codex/fixture" {
		t.Fatalf("unexpected page metadata: %+v", p)
	}
	if err := w.Create(p.ID, concept("Other", "Other"), "codex/fixture"); !errors.Is(err, ErrExists) {
		t.Fatalf("duplicate Create = %v", err)
	}
	changed, err := w.Update(p.ID, p.Hash, func(c *Concept) error { c.Body = "# Second\n\nChanged."; return nil }, "codex/fixture")
	if err != nil || !changed {
		t.Fatalf("Update = %v, %v", changed, err)
	}
	rendered := false
	if _, err := w.Update(p.ID, p.Hash, func(c *Concept) error { rendered = true; return nil }, "codex/fixture"); !errors.Is(err, ErrConflict) || rendered {
		t.Fatalf("stale Update = %v, rendered=%v", err, rendered)
	}
	p, err = w.Read(p.ID + ".md")
	if err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(w.root, p.Concept.Path)
	oldTime := time.Date(2001, 2, 3, 4, 5, 6, 0, time.UTC)
	if err := os.Chtimes(file, oldTime, oldTime); err != nil {
		t.Fatal(err)
	}
	changed, err = w.Update(p.ID, p.Hash, func(c *Concept) error {
		c.Body = "# Second\n\nChanged."
		c.Generated = &Generated{By: "other/model", At: "1999-01-01T00:00:00Z"}
		return nil
	}, "codex/fixture")
	if err != nil || changed {
		t.Fatalf("no-op Update = %v, %v", changed, err)
	}
	info, err := os.Stat(file)
	if err != nil || !info.ModTime().Equal(oldTime) || !bytes.Equal(p.Raw, mustRead(t, file)) {
		t.Fatalf("no-op changed bytes or mtime: info=%v err=%v", info, err)
	}
}

// A bounded read refuses a page over its limit, so a hook's read of a page
// that grew without bound stays bounded.
func TestReadMaxRefusesAPageOverItsLimit(t *testing.T) {
	w := testWiki(t)
	p := mustCreate(t, w, "general/threads/default", concept("Title", "# First\n\n"+strings.Repeat("Content. ", 100)))
	if got, err := w.ReadMax(p.ID, int64(len(p.Raw))); err != nil || !bytes.Equal(got.Raw, p.Raw) {
		t.Fatalf("a page at the limit: %v", err)
	}
	if _, err := w.ReadMax(p.ID, int64(len(p.Raw))-1); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("a page over the limit = %v", err)
	}
}

func TestCreateUpdateFailures(t *testing.T) {
	w := testWiki(t)
	p := mustCreate(t, w, "page", concept("Page", "Body"))
	for _, id := range []string{"", "../escape", "/absolute", "a//b", "./page", "a\\b", ".git/config", "index", "log", "AGENTS", "a/\x00b"} {
		if err := w.Create(id, concept("Bad", "Body"), "codex/test"); err == nil {
			t.Errorf("accepted unsafe id %q", id)
		}
	}
	if err := w.Create("nil", nil, "codex/test"); err == nil {
		t.Fatal("accepted nil concept")
	}
	if err := w.Create("actor", concept("Actor", "Body"), "invalid actor"); err == nil {
		t.Fatal("accepted invalid actor")
	}
	renderErr := errors.New("renderer failed")
	tests := []struct {
		name   string
		render func(*Concept) error
		actor  string
	}{
		{"nil renderer", nil, "codex/test"},
		{"renderer error", func(c *Concept) error { c.Body = "changed"; return renderErr }, "codex/test"},
		{"changed path", func(c *Concept) error { c.Path = "other.md"; return nil }, "codex/test"},
		{"changed id", func(c *Concept) error { c.ID = "other"; return nil }, "codex/test"},
		{"invalid actor", func(c *Concept) error { c.Body = "changed"; return nil }, "invalid actor"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			changed, err := w.Update(p.ID, p.Hash, tt.render, tt.actor)
			if err == nil || changed {
				t.Fatalf("Update = %v, %v", changed, err)
			}
			if !bytes.Equal(p.Raw, mustRead(t, filepath.Join(w.root, "page.md"))) {
				t.Fatal("failed update changed disk")
			}
		})
	}
	// A returned error must release the lock, too.
	if err := w.Create("after-error", concept("After", "Body"), "codex/test"); err != nil {
		t.Fatal(err)
	}
}

// A save that fails on the file system says the step that failed in
// eva-md's words, never with the name of okf's temporary file, and keeps the
// errno that tells a file system failure from a refused page.
func TestSaveFailureNamesNoTemporaryFile(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root writes a folder whatever its mode")
	}
	w := testWiki(t)
	p := mustCreate(t, w, "folder/page", concept("Page", "Body"))
	folder := filepath.Join(w.root, "folder")
	if err := os.Chmod(folder, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(folder, 0o700) })
	_, update := w.Update(p.ID, p.Hash, func(c *Concept) error { c.Body = "Changed"; return nil }, "codex/test")
	create := w.Create("folder/other", concept("Other", "Body"), "codex/test")
	for name, err := range map[string]error{"update": update, "create": create} {
		if !errors.Is(err, syscall.EACCES) || !strings.Contains(err.Error(), "a temporary file: ") || strings.Contains(strings.ToLower(err.Error()), "okf") {
			t.Errorf("%s: %v", name, err)
		}
	}
}

// tempFailure says each step of okf's atomic write that fails on its
// temporary file in eva-md's words, keeping the errno, and passes on any
// other error as it is.
func TestTempFailure(t *testing.T) {
	tmp := filepath.Join("wiki", "folder", ".tmp-okf-123456")
	page := filepath.Join("wiki", "folder", "page.md")
	wrap := func(text string, err error) error { return fmt.Errorf("failed to write concept: %s: %w", text, err) }
	for _, c := range []struct {
		err  error
		want string
	}{
		{wrap("failed to create temporary file", &fs.PathError{Op: "open", Path: tmp, Err: syscall.EACCES}), "open a temporary file: permission denied"},
		{wrap("failed to write to temporary file", &fs.PathError{Op: "write", Path: tmp, Err: syscall.ENOSPC}), "write a temporary file: no space left on device"},
		{wrap("failed to sync temporary file", &fs.PathError{Op: "sync", Path: tmp, Err: syscall.EIO}), "sync a temporary file: input/output error"},
		{wrap("failed to close temporary file", &fs.PathError{Op: "close", Path: tmp, Err: syscall.EIO}), "close a temporary file: input/output error"},
		{wrap("failed to set permissions on temporary file", &fs.PathError{Op: "chmod", Path: tmp, Err: syscall.EPERM}), "chmod a temporary file: operation not permitted"},
		{wrap("failed to atomically replace "+page, &os.LinkError{Op: "rename", Old: tmp, New: page, Err: syscall.EPERM}), "rename a temporary file: operation not permitted"},
	} {
		got := tempFailure(c.err)
		var errno syscall.Errno
		if got.Error() != c.want || !errors.As(got, &errno) {
			t.Errorf("%v: got %q", c.err, got)
		}
	}
	other := fmt.Errorf("failed to create directory: %w", &fs.PathError{Op: "mkdir", Path: filepath.Join("wiki", "folder"), Err: syscall.EACCES})
	if got := tempFailure(other); got != other {
		t.Errorf("changed %v into %v", other, got)
	}
}

func TestMetadataGoldenRoundTrip(t *testing.T) {
	w := testWiki(t)
	raw := mustRead(t, "testdata/thread.golden.md")
	c, err := okf.ParseConcept("projects/eva-md/threads/main.md", string(raw))
	if err != nil {
		t.Fatal(err)
	}
	if got := okf.SerializeConcept(c); got != string(raw) {
		t.Fatalf("Parse/Serialize did not round-trip golden:\n%s", got)
	}
	if err := w.Create(c.ID, c, "codex/golden"); err != nil {
		t.Fatal(err)
	}
	p, err := w.Read(c.ID)
	if err != nil {
		t.Fatal(err)
	}
	if withoutGenerated(string(p.Raw)) != withoutGenerated(string(raw)) {
		t.Fatal("Save changed golden beyond generated")
	}
	if changed, err := w.Update(p.ID, p.Hash, func(c *Concept) error { c.Body += "\nNew timeline entry.\n"; return nil }, "codex/golden"); err != nil || !changed {
		t.Fatalf("Update = %v, %v", changed, err)
	}
	next, err := w.Read(p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(c.Extra, next.Concept.Extra) || !reflect.DeepEqual(c.Verified, next.Concept.Verified) || !reflect.DeepEqual(c.Sources, next.Concept.Sources) {
		t.Fatal("Update lost Extra, Verified, or Sources metadata")
	}
	t.Log("golden Parse/Serialize bytes identical; Save changes only generated; Update preserves Extra, Verified and Sources")
}

func TestFullFixtureWiki(t *testing.T) {
	w := testWiki(t)
	pages := []struct{ id, title, body string }{
		{"README", "Eva wiki", "# Eva wiki\n\n[General](general/now.md)"},
		{"general/now", "General", "# General\n\n[Default](threads/default.md)"},
		{"general/threads/default", "General default", "# General default\n\nCurrent state."},
		{"projects/eva-md/now", "Eva project", "# Eva project\n\n[Main](threads/main.md)\n\n[Feature](threads/feature.md)"},
		{"projects/eva-md/threads/main", "Eva main", "# Eva main\n\nCurrent state."},
		{"projects/eva-md/threads/feature", "Eva feature", "# Eva feature\n\nCurrent state."},
	}
	for _, p := range pages {
		mustCreate(t, w, p.id, concept(p.title, p.body))
	}
	indexes := map[string]string{
		"index.md":                         "---\nokf_version: \"0.2\"\n---\n\n# Eva wiki\n\n" + IndexLine("Eva wiki", "README.md", "Fixture page") + "\n" + IndexLine("General", "general/index.md", "General area") + "\n" + IndexLine("Projects", "projects/index.md", "Project areas") + "\n",
		"general/index.md":                 "# General\n\n" + IndexLine("General", "now.md", "Fixture page") + "\n" + IndexLine("Threads", "threads/index.md", "General threads") + "\n",
		"general/threads/index.md":         "# Threads\n\n" + IndexLine("General default", "default.md", "Fixture page") + "\n",
		"projects/index.md":                "# Projects\n\n" + IndexLine("Eva", "eva-md/index.md", "Eva project") + "\n",
		"projects/eva-md/index.md":         "# Eva\n\n" + IndexLine("Eva project", "now.md", "Fixture page") + "\n" + IndexLine("Threads", "threads/index.md", "Eva threads") + "\n",
		"projects/eva-md/threads/index.md": "# Threads\n\n" + IndexLine("Eva feature", "feature.md", "Fixture page") + "\n" + IndexLine("Eva main", "main.md", "Fixture page") + "\n",
	}
	for name, content := range indexes {
		writeFixture(t, w, name, content)
	}
	b, err := loadBundle(w.root)
	if err != nil || b.RootPath != w.root || len(b.Concepts) != 6 {
		t.Fatalf("LoadBundle = %v, %v", b, err)
	}
	v, err := w.Validate()
	if err != nil {
		t.Fatal(err)
	}
	if len(v.Errors)+len(v.Warnings)+len(v.Orphans)+len(v.BrokenLinks) != 0 || !v.IsConformant {
		t.Fatalf("fixture findings: %+v", v)
	}
	t.Logf("full fixture: concepts=%d errors=%d warnings=%d orphans=%d broken_links=%d RootPath=root; folder links retained", v.ConceptCount, len(v.Errors), len(v.Warnings), len(v.Orphans), len(v.BrokenLinks))
}

func TestConcurrentCreateAndCAS(t *testing.T) {
	w := testWiki(t)
	const n = 12
	var wg sync.WaitGroup
	results := make(chan error, n)
	for range n {
		wg.Go(func() { results <- w.Create("page", concept("Page", "Initial"), "codex/test") })
	}
	wg.Wait()
	close(results)
	wins := 0
	for err := range results {
		if err == nil {
			wins++
		} else if !errors.Is(err, ErrExists) {
			t.Fatal(err)
		}
	}
	if wins != 1 {
		t.Fatalf("create winners = %d", wins)
	}
	p, err := w.Read("page")
	if err != nil {
		t.Fatal(err)
	}
	results = make(chan error, n)
	for i := range n {
		wg.Go(func() {
			_, err := w.Update(p.ID, p.Hash, func(c *Concept) error { c.Body = fmt.Sprintf("Writer %d", i); return nil }, "codex/test")
			results <- err
		})
	}
	wg.Wait()
	close(results)
	wins = 0
	for err := range results {
		if err == nil {
			wins++
		} else if !errors.Is(err, ErrConflict) {
			t.Fatal(err)
		}
	}
	if wins != 1 {
		t.Fatalf("CAS winners = %d", wins)
	}
}

func TestSymlinkAndPathProtections(t *testing.T) {
	w := testWiki(t)
	outside := filepath.Join(filepath.Dir(w.root), "outside")
	if err := os.Mkdir(outside, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(w.root, "escape")); err != nil {
		t.Fatal(err)
	}
	if err := w.Create("escape/page", concept("Page", "Body"), "codex/test"); err == nil {
		t.Fatal("Create followed directory symlink")
	}
	if err := w.WriteFile("escape/index.md", []byte("Bad")); err == nil {
		t.Fatal("WriteFile followed directory symlink")
	}
	if _, err := w.Read("escape/page"); err == nil {
		t.Fatal("Read followed directory symlink")
	}
	if err := os.Symlink(filepath.Join(outside, "missing.md"), filepath.Join(w.root, "dangling.md")); err != nil {
		t.Fatal(err)
	}
	if err := w.Create("dangling", concept("Page", "Body"), "codex/test"); err == nil {
		t.Fatal("Create accepted dangling symlink")
	}
	for _, rel := range []string{"../outside.md", "/absolute", "a//index.md", ".git/config", "x/.GIT/config", "page.md", "a/README.md", "control\nfile"} {
		if err := w.WriteFile(rel, []byte("Bad")); err == nil {
			t.Errorf("WriteFile accepted %q", rel)
		}
	}
	if entries, err := os.ReadDir(outside); err != nil || len(entries) != 0 {
		t.Fatalf("outside modified: %v, %v", entries, err)
	}
	if err := os.MkdirAll(w.lockDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(w.lockPath); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(outside, "lock"), w.lockPath); err != nil {
		t.Fatal(err)
	}
	if err := w.Create("locked", concept("Page", "Body"), "codex/test"); err == nil {
		t.Fatal("Create followed lock symlink")
	}
}

func TestInitRefusesIndexSymlink(t *testing.T) {
	dir := isolated(t)
	root := filepath.Join(dir, "wiki")
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(dir, "outside"), filepath.Join(root, "index.md")); err != nil {
		t.Fatal(err)
	}
	if err := Init(root); err == nil {
		t.Fatal("Init accepted index symlink")
	}
	if _, err := os.Lstat(filepath.Join(root, "log.md")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("Init mutated files before rejecting symlink: %v", err)
	}
}

func TestOpenedWikiRefusesReplacedRoot(t *testing.T) {
	w := testWiki(t)
	oldRoot := w.root + "-moved"
	if err := os.Rename(w.root, oldRoot); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(oldRoot, w.root); err != nil {
		t.Fatal(err)
	}
	if _, err := w.Validate(); err == nil {
		t.Fatal("Validate followed a replaced wiki root")
	}
	if err := w.Create("page", concept("Page", "Body"), "codex/test"); err == nil {
		t.Fatal("Create followed a replaced wiki root")
	}
	if err := w.WriteFile("index.md", []byte("# Changed")); err == nil {
		t.Fatal("WriteFile followed a replaced wiki root")
	}
}

func TestPermissions(t *testing.T) {
	w := testWiki(t)
	mustCreate(t, w, "nested/threads/page", concept("Page", "Body"))
	writeFixture(t, w, "nested/index.md", "# Index\n")
	writeFixture(t, w, ".eva-md/schema.toml", "schema_version = 1\n")
	writeFixture(t, w, ".gitignore", ".tmp-*\n")
	checks := map[string]os.FileMode{w.root: 0o700, w.lockDir: 0o700, w.lockPath: 0o600}
	for name, mode := range map[string]os.FileMode{"index.md": 0o644, "log.md": 0o644, "nested": 0o700, "nested/threads": 0o700, "nested/threads/page.md": 0o644, "nested/index.md": 0o644, ".eva-md": 0o700, ".eva-md/schema.toml": 0o600, ".gitignore": 0o600} {
		checks[filepath.Join(w.root, name)] = mode
	}
	for file, mode := range checks {
		fi, err := os.Stat(file)
		if err != nil || fi.Mode().Perm() != mode {
			t.Errorf("%s: info=%v err=%v want mode %o", file, fi, err, mode)
		}
	}
}

func TestBodyDelimiterRejection(t *testing.T) {
	w := testWiki(t)
	p := mustCreate(t, w, "page", concept("Page", "Initial"))
	calls := 0
	changed, err := w.Update(p.ID, p.Hash, func(c *Concept) error {
		calls++
		c.Body = "# Notes\n\n---\nstatus: manually noted\n---\n"
		return nil
	}, "codex/test")
	if err == nil || changed || calls != 1 || !strings.Contains(err.Error(), "cannot smuggle") {
		t.Fatalf("delimiter update: changed=%v err=%v calls=%d", changed, err, calls)
	}
	if !bytes.Equal(p.Raw, mustRead(t, filepath.Join(w.root, "page.md"))) {
		t.Fatal("rejected body changed page")
	}
	changed, err = w.Update(p.ID, p.Hash, func(c *Concept) error { c.Body = "# Notes\n\n***\nstatus: manually noted\n***\n"; return nil }, "codex/test")
	if err != nil || !changed {
		t.Fatalf("renderer's *** rules rejected: %v, %v", changed, err)
	}
	t.Log("okf rejects ---/status block once without changing disk; renderer's *** form saves")
}

func TestActorAndIndexLine(t *testing.T) {
	isolated(t)
	tests := []struct{ host, model, want string }{
		{"codex", "gpt-5.5-codex", "codex/gpt-5.5-codex"},
		{"claude-code", "claude-opus-5-5", "claude-code/claude-opus-5-5"},
		{"codex", "provider/model name", "codex/provider-model-name"},
		{"codex", "", "codex/unknown"},
		{"", " \t\n ", "unknown/unknown"},
		{"host/name", "model\tversion", "host-name/model-version"},
	}
	for _, tt := range tests {
		got := Actor(tt.host, tt.model)
		if got != tt.want || !okf.IsValidActor(got) {
			t.Errorf("Actor(%q, %q)=%q want %q", tt.host, tt.model, got, tt.want)
		}
	}
	for _, model := range []string{"💡", "a\x00b", strings.Repeat("x", 1000), "a\u202eb", "a/b/c\n", "---"} {
		if actor := Actor("codex", model); !okf.IsValidActor(actor) {
			t.Errorf("Actor produced invalid actor %q", actor)
		}
	}
	if got := IndexLine("A [title])\nnext", "page.md", "First\nsecond"); got != "* [A \\[title\\]\\) next](page.md) - First second" {
		t.Fatalf("IndexLine = %q", got)
	}
}

func git(t *testing.T, root string, args ...string) string {
	t.Helper()
	argv := append([]string{"-c", "core.fsmonitor=false", "-c", "core.hooksPath=/dev/null", "--no-optional-locks", "-C", root}, args...)
	cmd := exec.Command("git", argv...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v: %s", args, err, out)
	}
	return string(out)
}

// A save writes the page and its line in eva-md's log.md, never okf's index
// or log updates, and a save that changes nothing leaves git's diff empty.
func TestSaveWritesOnlyThePageAndTheLog(t *testing.T) {
	w := testWiki(t)
	p := mustCreate(t, w, "page", concept("Page", "Original"))
	git(t, w.root, "init", "--quiet")
	git(t, w.root, "add", "--", ".")
	changed, err := w.Update(p.ID, p.Hash, func(c *Concept) error { c.Body = "Changed"; return nil }, "codex/test")
	if err != nil || !changed {
		t.Fatalf("Update = %v, %v", changed, err)
	}
	if diff := git(t, w.root, "diff", "--name-only"); diff != "log.md\npage.md\n" {
		t.Fatalf("save changed files: %q", diff)
	}
	git(t, w.root, "add", "--", "page.md", "log.md")
	p, err = w.Read(p.ID)
	if err != nil {
		t.Fatal(err)
	}
	changed, err = w.Update(p.ID, p.Hash, func(c *Concept) error { c.Body = "Changed"; return nil }, "codex/test")
	if err != nil || changed {
		t.Fatalf("no-op Update = %v, %v", changed, err)
	}
	if diff := git(t, w.root, "diff", "--exit-code"); diff != "" {
		t.Fatalf("no-op git diff = %q", diff)
	}
	t.Log("changed save: git diff --name-only = log.md, page.md; repeated update: git diff --exit-code is empty")
}

// Each page write records one line in log.md as okf words its own lines,
// under the local date's group at the end of the file: a creation with the
// page's title on one line, a change without it, never a save that changed
// nothing, even on a day the log has no line for, and the same line once a
// day. A new day starts a group. A log
// that is a link, or missing, is left as it is, and the page write stands.
func TestWritesKeepTheLogCurrent(t *testing.T) {
	w := testWiki(t)
	logFile := filepath.Join(w.root, "log.md")
	initial := string(mustRead(t, logFile))
	day := time.Date(2031, 5, 6, 23, 30, 0, 0, time.Local)
	w.now = func() time.Time { return day }
	p := mustCreate(t, w, "general/threads/default", concept("Default  thread", "First"))
	change := func(body string) bool {
		t.Helper()
		cur, err := w.Read(p.ID)
		if err != nil {
			t.Fatal(err)
		}
		changed, err := w.Update(p.ID, cur.Hash, func(c *Concept) error { c.Body = body; return nil }, "codex/test")
		if err != nil {
			t.Fatal(err)
		}
		return changed
	}
	if !change("Second") || !change("Third") || change("Third") {
		t.Fatal("a save changed nothing, or a change saved nothing")
	}
	day = day.Add(time.Hour)
	if !change("Fourth") {
		t.Fatal("the next day's change saved nothing")
	}
	// A save that changes nothing on a day the log has no line for writes
	// none, so no group of its own either.
	day = day.Add(24 * time.Hour)
	if change("Fourth") {
		t.Fatal("a save on a new day that changed nothing saved")
	}
	want := initial + "\n## 2031-05-06\n* **Creation**: Documented concept `general/threads/default.md` (Default thread).\n" +
		"* **Update**: Updated concept `general/threads/default.md`.\n\n## 2031-05-07\n* **Update**: Updated concept `general/threads/default.md`.\n"
	if got := string(mustRead(t, logFile)); got != want {
		t.Fatalf("log:\n%s\nwant:\n%s", got, want)
	}
	target := filepath.Join(t.TempDir(), "elsewhere.md")
	if err := os.WriteFile(target, []byte("outside\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(logFile); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, logFile); err != nil {
		t.Fatal(err)
	}
	if !change("Fifth") || string(mustRead(t, target)) != "outside\n" {
		t.Fatal("the save failed, or wrote through the log's link")
	}
	if fi, err := os.Lstat(logFile); err != nil || fi.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("the log's link was replaced: %v", err)
	}
	if err := os.Remove(logFile); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(logFile); !change("Sixth") || !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("the save failed, or a missing log was created: %v", err)
	}
}

// appendLog adds to the last group when it is the day's, ends the file with
// a new group otherwise, and keeps a line the group holds once, whatever the
// file's line endings or last newline.
func TestAppendLog(t *testing.T) {
	const entry = "* **Update**: Updated concept `a.md`."
	for _, c := range []struct{ log, want string }{
		{"", "## 2031-05-06\n" + entry + "\n"},
		{"## 2031-05-05\n* **Creation**: x.", "## 2031-05-05\n* **Creation**: x.\n\n## 2031-05-06\n" + entry + "\n"},
		{"## 2031-05-06\r\n* **Creation**: x.\r\n", "## 2031-05-06\r\n* **Creation**: x.\r\n" + entry + "\n"},
		{"## 2031-05-06\n" + entry + "\r\n", "## 2031-05-06\n" + entry + "\r\n"},
		{"## 2031-05-06\n" + entry + "\n\n## 2031-05-05\n", "## 2031-05-06\n" + entry + "\n\n## 2031-05-05\n\n## 2031-05-06\n" + entry + "\n"},
	} {
		if got := appendLog(c.log, "2031-05-06", entry); got != c.want {
			t.Errorf("appendLog(%q) = %q, want %q", c.log, got, c.want)
		}
	}
}

// This helper is a real child process, so killing it does not run Go defers.
func TestInterruptedSaveHelper(t *testing.T) {
	root := os.Getenv("EVA_ADAPTER_CRASH_ROOT")
	if root == "" {
		isolated(t)
		return
	}
	w, err := Open(root, os.Getenv("EVA_ADAPTER_CRASH_LOCKS"))
	if err != nil {
		t.Fatal(err)
	}
	body := strings.Repeat("x", 32<<20)
	for revision := 1; ; revision++ {
		p, err := w.Read("page")
		if err != nil {
			t.Fatal(err)
		}
		_, err = w.Update("page", p.Hash, func(c *Concept) error { c.Body = fmt.Sprintf("revision: %d\n%s", revision, body); return nil }, "codex/crash-test")
		if err != nil {
			t.Fatal(err)
		}
	}
}

func TestSIGKILLDuringSaveExcludesTemporaryFiles(t *testing.T) {
	w := testWiki(t)
	original := mustCreate(t, w, "page", concept("Page", "Original"))
	writeFixture(t, w, ".gitignore", ".tmp-*\n*.jsonl\ninbox/\n.eva-md/tmp/\n")
	git(t, w.root, "init", "--quiet")
	cmd := exec.Command(os.Args[0], "-test.run=^TestInterruptedSaveHelper$")
	cmd.Env = append(os.Environ(), "EVA_ADAPTER_CRASH_ROOT="+w.root, "EVA_ADAPTER_CRASH_LOCKS="+w.lockDir)
	var childOutput bytes.Buffer
	cmd.Stdout, cmd.Stderr = &childOutput, &childOutput
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }()
	// Wait for a save in progress. The deadline is only a hang guard: the
	// helper is caught saving within a second, under the race detector too.
	const guard = 2 * time.Minute
	deadline := time.Now().Add(guard)
	var leftover string
	for time.Now().Before(deadline) {
		matches, err := filepath.Glob(filepath.Join(w.root, ".tmp-okf-*"))
		if err != nil {
			t.Fatal(err)
		}
		if len(matches) == 0 {
			// The pause only paces the polling.
			time.Sleep(100 * time.Microsecond)
			continue
		}
		if err := cmd.Process.Signal(syscall.SIGSTOP); err != nil {
			t.Fatal(err)
		}
		// Wait for the stop to be delivered before inspecting the temporary
		// file. Consuming this stop event does not reap the child process.
		var stop syscall.WaitStatus
		for {
			_, err := syscall.Wait4(cmd.Process.Pid, &stop, syscall.WUNTRACED, nil)
			if errors.Is(err, syscall.EINTR) {
				continue
			}
			// Darwin's syscall.WaitStatus.Stopped treats SIGSTOP as
			// Continued, so check the POSIX stop event's bits directly.
			if err != nil || stop&0xff != 0x7f || syscall.Signal(stop>>8) != syscall.SIGSTOP {
				t.Fatalf("wait for child stop: status=%v err=%v", stop, err)
			}
			break
		}
		// SIGSTOP may arrive after a rename. Resume and observe a later save.
		matches, _ = filepath.Glob(filepath.Join(w.root, ".tmp-okf-*"))
		if len(matches) > 0 {
			leftover = matches[0]
			break
		}
		if err := cmd.Process.Signal(syscall.SIGCONT); err != nil {
			t.Fatal(err)
		}
	}
	if leftover == "" {
		t.Fatalf("did not observe an in-progress okf temporary save in %s: the helper hangs or never saves", guard)
	}
	if err := cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	err := cmd.Wait()
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) || exitErr.ProcessState.Sys().(syscall.WaitStatus).Signal() != syscall.SIGKILL {
		t.Fatalf("child exit=%v output=%s", err, childOutput.String())
	}
	if _, err := os.Stat(leftover); err != nil {
		t.Fatalf("SIGKILL did not leave observed temp file: %v", err)
	}
	page, err := w.Read("page")
	if err != nil {
		t.Fatalf("interrupted save damaged original: %v", err)
	}
	if !bytes.Equal(page.Raw, original.Raw) {
		body := strings.TrimSpace(page.Concept.Body)
		first, rest, ok := strings.Cut(body, "\n")
		revision, revErr := strconv.Atoi(strings.TrimPrefix(first, "revision: "))
		if !ok || revErr != nil || revision < 1 || rest != strings.Repeat("x", 32<<20) {
			t.Fatal("interrupted save left a partial page")
		}
	}
	git(t, w.root, "add", "--", ".")
	if staged := git(t, w.root, "ls-files", "--cached"); strings.Contains(staged, ".tmp-okf-") || strings.Contains(staged, ".tmp-eva-") {
		t.Fatalf("temporary file staged: %s", staged)
	}
	if ignored := git(t, w.root, "check-ignore", "--", filepath.Base(leftover)); !strings.Contains(ignored, ".tmp-okf-") {
		t.Fatalf("temp file not ignored: %q", ignored)
	}
	// Kernel releases flock when the child dies, including after SIGSTOP.
	if err := w.Create("after-kill", concept("After kill", "Body"), "codex/test"); err != nil {
		t.Fatalf("lock was not released after SIGKILL: %v", err)
	}
	t.Log("SIGKILL interrupted observed .tmp-okf-* save; page remains complete; git add . stages no temporary files; lock released")
}

// A person verifies a page in any form YAML allows, not only the flow
// mapping or list okf reads entries from: a plain value or a block mapping
// counts too, and an empty value doesn't.
func TestIsVerified(t *testing.T) {
	head := "---\ntype: thread\ntitle: Page\n"
	for _, tc := range []struct {
		front string
		want  bool
	}{
		{"", false},
		{"verified: { by: human/fixture, at: \"2026-10-29T10:00:00Z\" }\n", true},
		{"verified: true\n", true},
		{"verified: human/fixture\n", true},
		{"\"verified\": yes\n", true},
		{"verified:\n  by: human/fixture\n  at: \"2026-10-29T10:00:00Z\"\n", true},
		{"verified:\n  - by: human/fixture\n", true},
		{"verified:\nstatus: stable\n", false},
		{"verified: \"\"\n", false},
		{"verified: []\n", false},
		{"notes:\n  verified: true\n", false},
	} {
		raw := []byte(head + tc.front + "---\n\nBody.\n")
		c, err := okf.ParseConcept("page.md", string(raw))
		if err != nil {
			t.Fatalf("%q: %v", tc.front, err)
		}
		if got := IsVerified(&Page{Concept: c, Raw: raw}); got != tc.want {
			t.Errorf("%q: verified %v, want %v", tc.front, got, tc.want)
		}
	}
	if VerifiedIn([]byte("no frontmatter\nverified: true\n")) {
		t.Error("a page without frontmatter is verified")
	}
}

// SmugglesFrontmatter refuses what okf's save refuses, so a writer can pass
// over a page instead of failing on it.
func TestSmugglesFrontmatter(t *testing.T) {
	w := testWiki(t)
	p := mustCreate(t, w, "page", concept("Page", "Initial"))
	for _, tc := range []struct {
		body string
		want bool
	}{
		{"# Notes\n\n---\nstatus: manually noted\n---\n", true},
		{"## Notes\nAgreed.\n\n---\n\nStatus: confirmed with the team.\n", true},
		{"```yaml\n---\n  'Type': x\n```\n", true},
		{"## Notes\n\n---\n\nA plain line.\n---\nstatus: after the block\n", false},
		{"# Notes\n\n***\nstatus: manually noted\n***\n", false},
	} {
		_, err := w.Update(p.ID, p.Hash, func(c *Concept) error { c.Body = tc.body; return nil }, "codex/test")
		if refused := err != nil && strings.Contains(err.Error(), "cannot smuggle"); refused != tc.want || SmugglesFrontmatter(tc.body) != tc.want {
			t.Errorf("%q: okf refused %v (%v), SmugglesFrontmatter %v, want %v", tc.body, refused, err, SmugglesFrontmatter(tc.body), tc.want)
		}
		p, _ = w.Read("page")
	}
}
