package okfadapter

import (
	"bytes"
	"errors"
	"fmt"
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
)

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
	if err := Init(w.root); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, mustRead(t, filepath.Join(w.root, "log.md"))) {
		t.Fatal("Init rewrote the log")
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
	writeFixture(t, w, ".gitignore", ".tmp-okf-*\n.tmp-eva-*\n")
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

func TestSaveExactlyOneFileAndNoOpGitDiff(t *testing.T) {
	w := testWiki(t)
	p := mustCreate(t, w, "page", concept("Page", "Original"))
	git(t, w.root, "init", "--quiet")
	git(t, w.root, "add", "--", ".")
	changed, err := w.Update(p.ID, p.Hash, func(c *Concept) error { c.Body = "Changed"; return nil }, "codex/test")
	if err != nil || !changed {
		t.Fatalf("Update = %v, %v", changed, err)
	}
	if diff := git(t, w.root, "diff", "--name-only"); diff != "page.md\n" {
		t.Fatalf("save changed files: %q", diff)
	}
	git(t, w.root, "add", "--", "page.md")
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
	t.Log("changed save: git diff --name-only = page.md; repeated update: git diff --exit-code is empty")
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
	writeFixture(t, w, ".gitignore", ".tmp-okf-*\n.tmp-eva-*\n*.jsonl\ninbox/\n.eva-md/tmp/\n")
	git(t, w.root, "init", "--quiet")
	cmd := exec.Command(os.Args[0], "-test.run=^TestInterruptedSaveHelper$")
	cmd.Env = append(os.Environ(), "EVA_ADAPTER_CRASH_ROOT="+w.root, "EVA_ADAPTER_CRASH_LOCKS="+w.lockDir)
	var childOutput bytes.Buffer
	cmd.Stdout, cmd.Stderr = &childOutput, &childOutput
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }()
	deadline := time.Now().Add(20 * time.Second)
	var leftover string
	for time.Now().Before(deadline) {
		matches, err := filepath.Glob(filepath.Join(w.root, ".tmp-okf-*"))
		if err != nil {
			t.Fatal(err)
		}
		if len(matches) == 0 {
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
		t.Fatal("did not observe an in-progress okf temporary save")
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
