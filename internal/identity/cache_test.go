package identity

import (
	"bytes"
	"context"
	"fmt"
	"math/rand/v2"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// gitCounter puts a git first on PATH that counts its runs and then runs the
// real git, so a test can tell whether a resolve started any git process.
func gitCounter(t *testing.T) func() int {
	t.Helper()
	real, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git not found")
	}
	dir := t.TempDir()
	runs := filepath.Join(dir, "runs")
	script := "#!/bin/sh\necho run >> '" + runs + "'\nexec '" + real + "' \"$@\"\n"
	if err := os.WriteFile(filepath.Join(dir, "git"), []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return func() int {
		data, _ := os.ReadFile(runs)
		return bytes.Count(data, []byte("\n"))
	}
}

// cacheFixture is a temporary home with git isolated, a cache folder in it
// and a git that counts its runs.
type cacheFixture struct {
	home  string
	cache Cache
	runs  func() int
}

func newCacheFixture(t *testing.T) cacheFixture {
	t.Helper()
	home := gitEnvForTest(t)
	real, err := filepath.EvalSymlinks(home)
	if err != nil {
		t.Fatal(err)
	}
	return cacheFixture{home: real, cache: Cache{Dir: filepath.Join(real, "state", "cache", "identity")}, runs: gitCounter(t)}
}

// resolve checks that the cache answers exactly like Resolve and reports
// whether it answered without starting git.
func (f cacheFixture) resolve(t *testing.T, cwd string) (Project, bool) {
	t.Helper()
	ctx := context.Background()
	before := f.runs()
	got, err := f.cache.Resolve(ctx, cwd)
	hit := f.runs() == before
	want, wantErr := Resolve(ctx, cwd)
	if got != want || (err == nil) != (wantErr == nil) {
		t.Fatalf("cache and Resolve differ for %s:\n cache %+v, %v\n  live %+v, %v", cwd, got, err, want, wantErr)
	}
	return got, hit
}

// miss expects a resolve that starts git, then a second one that doesn't.
func (f cacheFixture) miss(t *testing.T, cwd string) Project {
	t.Helper()
	p, hit := f.resolve(t, cwd)
	if hit {
		t.Fatalf("%s: the cache answered without checking with git: %+v", cwd, p)
	}
	if again, hit := f.resolve(t, cwd); !hit || again != p {
		t.Fatalf("%s: second resolve hit=%v, %+v", cwd, hit, again)
	}
	return p
}

// live expects that the cache does not keep an answer for cwd.
func (f cacheFixture) live(t *testing.T, cwd string) Project {
	t.Helper()
	for range 2 {
		if p, hit := f.resolve(t, cwd); hit {
			t.Fatalf("%s: answered from the cache: %+v", cwd, p)
		}
	}
	p, _ := f.resolve(t, cwd)
	return p
}

func (f cacheFixture) hit(t *testing.T, cwd string) Project {
	t.Helper()
	p, hit := f.resolve(t, cwd)
	if !hit {
		t.Fatalf("%s: expected an answer from the cache: %+v", cwd, p)
	}
	return p
}

func mkdir(t *testing.T, dir string) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	return dir
}

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func commit(t *testing.T, dir, msg string) {
	t.Helper()
	runGit(t, dir, "commit", "-q", "--allow-empty", "-m", msg)
}

// tryGit runs a git command that may fail, such as checking out a branch
// another worktree holds. A failure just leaves the repository as it was.
func tryGit(dir string, args ...string) {
	cmd := exec.Command("git", append([]string{"-c", "core.hooksPath=/dev/null", "-C", dir}, args...)...)
	cmd.Env = gitEnv()
	_ = cmd.Run()
}

func TestCacheHitStartsNoGit(t *testing.T) {
	f := newCacheFixture(t)
	repo := newRepo(t, filepath.Join(f.home, "code", "eva-md"), true)
	runGit(t, repo, "remote", "add", "origin", "git@github.com:HeyNag/Eva-MD.git")
	p := f.miss(t, repo)
	if p.Key != "github.com__heynag__eva-md" || p.Branch != "main" || p.Root != repo {
		t.Fatalf("got %+v", p)
	}
	// A subfolder shares its worktree's entry.
	sub := mkdir(t, filepath.Join(repo, "internal", "x"))
	if got := f.hit(t, sub); got != p {
		t.Fatalf("subfolder: %+v", got)
	}
	// Files appearing in the top level change nothing.
	writeFile(t, filepath.Join(repo, "new.txt"), "x")
	f.hit(t, repo)

	plain := mkdir(t, filepath.Join(f.home, "plain"))
	if got := f.miss(t, plain); got != General() {
		t.Fatalf("plain folder: %+v", got)
	}

	info, err := os.Stat(f.cache.Dir)
	if err != nil || info.Mode().Perm() != 0o700 {
		t.Fatalf("cache folder: %v, %v", info, err)
	}
	entries, err := os.ReadDir(f.cache.Dir)
	if err != nil || len(entries) != 2 {
		t.Fatalf("entries %v, %v", entries, err)
	}
	for _, e := range entries {
		info, err := e.Info()
		if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 || strings.HasPrefix(e.Name(), ".") {
			t.Fatalf("entry %s: %v, %v", e.Name(), info, err)
		}
	}
}

func TestCacheInvalidation(t *testing.T) {
	f := newCacheFixture(t)
	repo := newRepo(t, filepath.Join(f.home, "code", "app"), true)
	runGit(t, repo, "remote", "add", "origin", "https://github.com/acme/app.git")
	f.miss(t, repo)

	t.Run("branch switch", func(t *testing.T) {
		runGit(t, repo, "checkout", "-q", "-b", "feature/login")
		if p := f.miss(t, repo); p.Branch != "feature/login" {
			t.Fatalf("got %+v", p)
		}
		runGit(t, repo, "checkout", "-q", "main")
		if p := f.miss(t, repo); p.Branch != "main" {
			t.Fatalf("got %+v", p)
		}
		commit(t, repo, "on a branch")
		f.hit(t, repo)
	})

	t.Run("detached HEAD", func(t *testing.T) {
		runGit(t, repo, "checkout", "-q", "--detach")
		if p := f.miss(t, repo); !p.Detached || p.Branch != "" {
			t.Fatalf("got %+v", p)
		}
		commit(t, repo, "detached")
		f.miss(t, repo)
		runGit(t, repo, "checkout", "-q", "main")
		if p := f.miss(t, repo); p.Detached || p.Branch != "main" {
			t.Fatalf("got %+v", p)
		}
	})

	t.Run("remote change", func(t *testing.T) {
		runGit(t, repo, "remote", "set-url", "origin", "git@gitlab.example:team/app.git")
		if p := f.miss(t, repo); p.Remote != "gitlab.example/team/app" {
			t.Fatalf("got %+v", p)
		}
		runGit(t, repo, "remote", "rename", "origin", "upstream")
		if p := f.miss(t, repo); p.Kind != KindLocal {
			t.Fatalf("got %+v", p)
		}
		runGit(t, repo, "remote", "add", "origin", "https://github.com/acme/app.git")
		if p := f.miss(t, repo); p.Remote != "github.com/acme/app" {
			t.Fatalf("got %+v", p)
		}
		// Same length, edited in place.
		cfg := filepath.Join(repo, ".git", "config")
		data, err := os.ReadFile(cfg)
		if err != nil {
			t.Fatal(err)
		}
		writeFile(t, cfg, strings.Replace(string(data), "acme/app", "acme/apx", 1))
		if p := f.miss(t, repo); p.Remote != "github.com/acme/apx" {
			t.Fatalf("got %+v", p)
		}
	})

	t.Run("first commit", func(t *testing.T) {
		empty := newRepo(t, filepath.Join(f.home, "code", "empty"), false)
		if p := f.miss(t, empty); p.Branch != DefaultBranch {
			t.Fatalf("got %+v", p)
		}
		commit(t, empty, "first")
		if p := f.miss(t, empty); p.Branch != "main" {
			t.Fatalf("got %+v", p)
		}
	})

	t.Run("packed refs", func(t *testing.T) {
		runGit(t, repo, "pack-refs", "--all")
		if _, err := os.Lstat(filepath.Join(repo, ".git", "refs", "heads", "main")); !os.IsNotExist(err) {
			t.Fatalf("loose ref still there: %v", err)
		}
		if p := f.miss(t, repo); p.Branch != "main" {
			t.Fatalf("got %+v", p)
		}
	})

	t.Run("a tag named like the branch", func(t *testing.T) {
		runGit(t, repo, "tag", "main")
		if p := f.live(t, repo); p.Branch != "heads/main" {
			t.Fatalf("got %+v", p)
		}
		// The ambiguous state was never stored, and deleting the tag
		// restores exactly what the entry describes.
		runGit(t, repo, "tag", "-d", "main")
		if p, _ := f.resolve(t, repo); p.Branch != "main" {
			t.Fatalf("got %+v", p)
		}
		f.hit(t, repo)
	})

	t.Run("global config", func(t *testing.T) {
		local := newRepo(t, filepath.Join(f.home, "code", "local"), true)
		if p := f.miss(t, local); p.Kind != KindLocal {
			t.Fatalf("got %+v", p)
		}
		global := os.Getenv("GIT_CONFIG_GLOBAL")
		data, err := os.ReadFile(global)
		if err != nil {
			t.Fatal(err)
		}
		writeFile(t, global, string(data)+"[remote \"origin\"]\n\turl = https://example.com/global/one.git\n")
		if p := f.miss(t, local); p.Remote != "example.com/global/one" {
			t.Fatalf("got %+v", p)
		}
		// An include that does not exist yet, and one behind a condition.
		included := filepath.Join(f.home, "included.gitconfig")
		conditional := filepath.Join(f.home, "conditional.gitconfig")
		writeFile(t, global, string(data)+"[include]\n\tpath = ~/included.gitconfig\n[includeIf \"gitdir:"+local+"/\"]\n\tpath = conditional.gitconfig\n")
		if p := f.miss(t, local); p.Kind != KindLocal {
			t.Fatalf("got %+v", p)
		}
		writeFile(t, included, "[remote \"origin\"]\n\turl = https://example.com/included/two.git\n")
		if p := f.miss(t, local); p.Remote != "example.com/included/two" {
			t.Fatalf("got %+v", p)
		}
		writeFile(t, conditional, "[remote \"origin\"]\n\turl = https://example.com/conditional/three.git\n")
		if p := f.miss(t, local); p.Remote != "example.com/conditional/three" {
			t.Fatalf("got %+v", p)
		}
		writeFile(t, global, string(data))
		if p := f.miss(t, local); p.Kind != KindLocal {
			t.Fatalf("got %+v", p)
		}
	})

	t.Run("environment", func(t *testing.T) {
		t.Setenv("GIT_CEILING_DIRECTORIES", f.home)
		f.miss(t, repo)
	})

	t.Run("worktree move", func(t *testing.T) {
		wt := filepath.Join(f.home, "code", "app-wt")
		runGit(t, repo, "worktree", "add", "-q", "-b", "wt-branch", wt)
		if p := f.miss(t, wt); p.Root != wt || p.Branch != "wt-branch" {
			t.Fatalf("got %+v", p)
		}
		// The main worktree's branch is not the linked worktree's.
		runGit(t, repo, "checkout", "-q", "-b", "elsewhere")
		f.hit(t, wt)
		runGit(t, wt, "checkout", "-q", "-b", "wt-next")
		if p := f.miss(t, wt); p.Branch != "wt-next" {
			t.Fatalf("got %+v", p)
		}
		moved := filepath.Join(f.home, "code", "app-wt-moved")
		runGit(t, repo, "worktree", "move", wt, moved)
		if p := f.miss(t, moved); p.Root != moved || p.Branch != "wt-next" {
			t.Fatalf("got %+v", p)
		}
		// Resolve needs no git for a missing folder either.
		if p, _ := f.resolve(t, wt); p != General() {
			t.Fatalf("old path: %+v", p)
		}
		// A new worktree at the old path is a different worktree.
		runGit(t, repo, "worktree", "add", "-q", "-b", "wt-again", wt)
		if p := f.miss(t, wt); p.Branch != "wt-again" {
			t.Fatalf("got %+v", p)
		}
		runGit(t, repo, "checkout", "-q", "main")
		f.miss(t, repo)
	})

	t.Run("deleted and recreated", func(t *testing.T) {
		gone := newRepo(t, filepath.Join(f.home, "code", "gone"), true)
		runGit(t, gone, "remote", "add", "origin", "https://github.com/acme/gone.git")
		sub := mkdir(t, filepath.Join(gone, "sub"))
		f.miss(t, sub)
		if err := os.RemoveAll(filepath.Join(gone, ".git")); err != nil {
			t.Fatal(err)
		}
		if p := f.miss(t, sub); p != General() {
			t.Fatalf("got %+v", p)
		}
		if err := os.RemoveAll(gone); err != nil {
			t.Fatal(err)
		}
		if p, _ := f.resolve(t, sub); p != General() {
			t.Fatalf("got %+v", p)
		}
		newRepo(t, gone, false)
		mkdir(t, sub)
		if p := f.miss(t, sub); p.Kind != KindLocal || p.Branch != DefaultBranch {
			t.Fatalf("got %+v", p)
		}
		runGit(t, gone, "remote", "add", "origin", "https://github.com/acme/new.git")
		if p := f.miss(t, sub); p.Remote != "github.com/acme/new" {
			t.Fatalf("got %+v", p)
		}
	})

	t.Run("repository created inside", func(t *testing.T) {
		inner := mkdir(t, filepath.Join(repo, "vendor", "inner"))
		if p := f.hit(t, inner); p.Root != repo {
			t.Fatalf("got %+v", p)
		}
		runGit(t, inner, "init", "-q")
		if p := f.miss(t, inner); p.Root != inner {
			t.Fatalf("got %+v", p)
		}
		plain := mkdir(t, filepath.Join(f.home, "notes", "deep"))
		f.miss(t, plain)
		runGit(t, filepath.Dir(plain), "init", "-q")
		if p := f.miss(t, plain); p.Root != filepath.Dir(plain) {
			t.Fatalf("got %+v", p)
		}
	})
}

func TestCacheUnusualLayoutsResolveLive(t *testing.T) {
	f := newCacheFixture(t)

	t.Run("symlinked path", func(t *testing.T) {
		repo := newRepo(t, filepath.Join(f.home, "code", "real"), true)
		alias := filepath.Join(f.home, "alias")
		if err := os.Symlink(repo, alias); err != nil {
			t.Fatal(err)
		}
		f.miss(t, repo)
		if p := f.live(t, alias); p.Root != repo {
			t.Fatalf("got %+v", p)
		}
	})

	t.Run("core.worktree", func(t *testing.T) {
		repo := newRepo(t, filepath.Join(f.home, "code", "split"), true)
		other := mkdir(t, filepath.Join(f.home, "code", "split-tree"))
		runGit(t, repo, "config", "core.worktree", other)
		f.live(t, repo)
	})

	t.Run("symlinked HEAD", func(t *testing.T) {
		repo := newRepo(t, filepath.Join(f.home, "code", "oddhead"), true)
		head := filepath.Join(repo, ".git", "HEAD")
		writeFile(t, filepath.Join(repo, ".git", "HEAD.real"), "ref: refs/heads/main\n")
		if err := os.Remove(head); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink("HEAD.real", head); err != nil {
			t.Fatal(err)
		}
		f.live(t, repo)
	})

	t.Run("submodule", func(t *testing.T) {
		upstream := newRepo(t, filepath.Join(f.home, "code", "lib"), true)
		super := newRepo(t, filepath.Join(f.home, "code", "super"), true)
		runGit(t, super, "-c", "protocol.file.allow=always", "submodule", "add", "-q", upstream, "lib")
		sub := filepath.Join(super, "lib")
		if p := f.miss(t, sub); p.Root != sub {
			t.Fatalf("got %+v", p)
		}
		runGit(t, sub, "checkout", "-q", "-b", "sub-branch")
		if p := f.miss(t, sub); p.Branch != "sub-branch" {
			t.Fatalf("got %+v", p)
		}
	})

	t.Run("missing folder", func(t *testing.T) {
		if p, _ := f.resolve(t, filepath.Join(f.home, "does-not-exist")); p != General() {
			t.Fatalf("got %+v", p)
		}
	})
}

func TestCacheBadEntriesFallBack(t *testing.T) {
	f := newCacheFixture(t)
	repo := newRepo(t, filepath.Join(f.home, "code", "repo"), true)
	want := f.miss(t, repo)
	entries, err := os.ReadDir(f.cache.Dir)
	if err != nil || len(entries) != 1 {
		t.Fatalf("entries %v, %v", entries, err)
	}
	entry := filepath.Join(f.cache.Dir, entries[0].Name())
	good, err := os.ReadFile(entry)
	if err != nil {
		t.Fatal(err)
	}
	damage := func(t *testing.T, change func()) {
		t.Helper()
		change()
		if p := f.miss(t, repo); p != want {
			t.Fatalf("got %+v", p)
		}
	}
	t.Run("garbage", func(t *testing.T) {
		damage(t, func() { writeFile(t, entry, "not json") })
	})
	t.Run("truncated", func(t *testing.T) {
		damage(t, func() { writeFile(t, entry, string(good[:len(good)/2])) })
	})
	t.Run("empty", func(t *testing.T) {
		damage(t, func() { writeFile(t, entry, "") })
	})
	t.Run("edited project", func(t *testing.T) {
		damage(t, func() { writeFile(t, entry, strings.Replace(string(good), `"Branch":"main"`, `"Branch":"mainx"`, 1)) })
	})
	t.Run("unreadable", func(t *testing.T) {
		damage(t, func() {
			if err := os.Chmod(entry, 0); err != nil {
				t.Fatal(err)
			}
		})
	})
	t.Run("another folder's entry", func(t *testing.T) {
		other := newRepo(t, filepath.Join(f.home, "code", "other"), true)
		f.miss(t, other)
		names, _ := os.ReadDir(f.cache.Dir)
		for _, n := range names {
			path := filepath.Join(f.cache.Dir, n.Name())
			if path != entry {
				data, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				damage(t, func() { writeFile(t, entry, string(data)) })
			}
		}
	})
	t.Run("entry is a folder", func(t *testing.T) {
		if err := os.Remove(entry); err != nil {
			t.Fatal(err)
		}
		mkdir(t, entry)
		f.live(t, repo)
		if err := os.Remove(entry); err != nil {
			t.Fatal(err)
		}
		f.miss(t, repo)
	})
	t.Run("cache folder is a file", func(t *testing.T) {
		c := f
		c.cache.Dir = filepath.Join(f.home, "state", "file")
		writeFile(t, c.cache.Dir, "x")
		c.live(t, repo)
	})
	t.Run("cache folder through a symlink", func(t *testing.T) {
		c := f
		link := filepath.Join(f.home, "link")
		if err := os.Symlink(f.home, link); err != nil {
			t.Fatal(err)
		}
		c.cache.Dir = filepath.Join(link, "cache")
		c.live(t, repo)
	})
	t.Run("relative cache folder", func(t *testing.T) {
		c := f
		c.cache.Dir = "state/cache"
		c.live(t, repo)
	})
	t.Run("leftover temporary file", func(t *testing.T) {
		writeFile(t, filepath.Join(f.cache.Dir, ".tmp-eva-leftover"), "junk")
		f.hit(t, repo)
	})
}

func TestCacheConcurrentHooks(t *testing.T) {
	f := newCacheFixture(t)
	repo := newRepo(t, filepath.Join(f.home, "code", "busy"), true)
	runGit(t, repo, "branch", "other")
	ctx := context.Background()
	valid := map[string]bool{"main": true, "other": true}
	var wg sync.WaitGroup
	stop := make(chan struct{})
	errs := make(chan error, 64)
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				p, err := f.cache.Resolve(ctx, repo)
				if err != nil || p.Root != repo || p.Kind != KindLocal || !valid[p.Branch] {
					errs <- fmt.Errorf("got %+v, %v", p, err)
					return
				}
			}
		}()
	}
	for i := range 20 {
		branch := "other"
		if i%2 == 1 {
			branch = "main"
		}
		runGit(t, repo, "checkout", "-q", branch)
	}
	close(stop)
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
	// Once the switching stops, the cache settles on the live answer.
	if p, _ := f.resolve(t, repo); p.Branch != "main" {
		t.Fatalf("got %+v", p)
	}
	f.hit(t, repo)
	names, _ := os.ReadDir(f.cache.Dir)
	for _, n := range names {
		if strings.HasPrefix(n.Name(), ".") {
			t.Fatalf("temporary file left behind: %s", n.Name())
		}
	}
}

func TestCacheWithoutSystemConfigPath(t *testing.T) {
	home := gitEnvForTest(t)
	real, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git not found")
	}
	// A git that predates `git var GIT_CONFIG_SYSTEM` exits with its usage.
	dir := t.TempDir()
	runs := filepath.Join(dir, "runs")
	script := "#!/bin/sh\necho run >> '" + runs + "'\nfor a in \"$@\"; do [ \"$a\" = var ] && exit 129; done\nexec '" + real + "' \"$@\"\n"
	if err := os.WriteFile(filepath.Join(dir, "git"), []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	root, _ := filepath.EvalSymlinks(home)
	f := cacheFixture{home: root, cache: Cache{Dir: filepath.Join(root, "cache")}, runs: func() int {
		data, _ := os.ReadFile(runs)
		return bytes.Count(data, []byte("\n"))
	}}
	repo := newRepo(t, filepath.Join(root, "repo"), true)
	want, err := Resolve(context.Background(), repo)
	if err != nil {
		t.Fatal(err)
	}
	before := f.runs()
	for range 3 {
		if got, err := f.cache.Resolve(context.Background(), repo); err != nil || got != want {
			t.Fatalf("got %+v, %v; want %+v", got, err, want)
		}
	}
	// Each resolve starts its 3 git processes; `git var` ran only once.
	if n := f.runs() - before; n != 3*3+1 {
		t.Fatalf("git ran %d times", n)
	}
}

func TestSystemAndGlobalConfigPaths(t *testing.T) {
	home := gitEnvForTest(t)
	ctx := context.Background()
	if got, ok := systemConfig(ctx); !ok || got != "" {
		t.Fatalf("GIT_CONFIG_NOSYSTEM: %q, %v", got, ok)
	}
	t.Setenv("GIT_CONFIG_NOSYSTEM", "")
	t.Setenv("GIT_CONFIG_SYSTEM", filepath.Join(home, "system.gitconfig"))
	if got, ok := systemConfig(ctx); !ok || got != filepath.Join(home, "system.gitconfig") {
		t.Fatalf("GIT_CONFIG_SYSTEM: %q, %v", got, ok)
	}
	gitVar := func() []string {
		out, err := git(ctx, "/", "var", "GIT_CONFIG_GLOBAL")
		if err != nil {
			t.Fatal(err)
		}
		return strings.Split(out, "\n")
	}
	for _, env := range []map[string]string{
		{"GIT_CONFIG_GLOBAL": filepath.Join(home, "g")},
		{"XDG_CONFIG_HOME": filepath.Join(home, "xdg")},
		{"XDG_CONFIG_HOME": ""},
	} {
		os.Unsetenv("GIT_CONFIG_GLOBAL")
		for k, v := range env {
			t.Setenv(k, v)
		}
		if got, want := globalConfigs(), gitVar(); strings.Join(got, "\n") != strings.Join(want, "\n") {
			t.Errorf("%v: got %q, git says %q", env, got, want)
		}
	}
}

func TestIncludeTarget(t *testing.T) {
	home := gitEnvForTest(t)
	for _, c := range []struct {
		in, want string
		ok       bool
	}{
		{"~/x.gitconfig", home + "/x.gitconfig", true},
		{"~", home, true},
		{"/etc/x", "/etc/x", true},
		{"rel/x", "/cfg/dir/rel/x", true},
		{"../x", "/cfg/dir/../x", true},
		{"~other/x", "", false},
		{"%(prefix)/etc/x", "", false},
		{"", "", false},
	} {
		got, ok := includeTarget(c.in, "/cfg/dir/config")
		if got != c.want || ok != c.ok {
			t.Errorf("includeTarget(%q) = %q, %v; want %q, %v", c.in, got, ok, c.want, c.ok)
		}
	}
}

// TestConfigIncludesMatchGit parses config files with the cache's parser and
// with git itself and expects the same include paths, and the same verdict on
// files git rejects.
func TestConfigIncludesMatchGit(t *testing.T) {
	home := gitEnvForTest(t)
	cases := []string{
		"[include]\n\tpath = a\n",
		"[Include]\n\tPATH = a\n",
		"[include]path=a",
		"[include] path = a # comment\n",
		"[include]\n\tpath = \"a b\" ; comment\n",
		"[include]\n\tpath = a\\\n b\n",
		"[include]\n\tpath = \"a\\\"b\\\\c\"\n",
		"[include]\n\tpath = a\\tb\n",
		"[include]\n\tpath = a\\qb\n",
		"[include]\n\tpath = \"unterminated\n",
		"[include]\n\tpath\n",
		"[include]\n\tpath # no value\n",
		"[includeIf \"gitdir:~/work/\"]\n\tpath = ~/work.gitconfig\n",
		"[includeIf \"onbranch:main\"]\r\n\tpath = crlf\r\n",
		"\xef\xbb\xbf[include]\n\tpath = bom\n",
		"\xef\xbb[include]\n\tpath = partial-bom\n",
		"[alias]\n\tx = !echo \\\n[include]\n\tpath = hidden\n",
		"[core]\n\tbare = false\n[include]\n\tpath = a\n[core]\n\tpath = not-an-include\n",
		"[include \"sub\"]\n\tpath = a\n",
		"[include.sub]\n\tpath = a\n",
		"[]\n\tpath = a\n",
		"[ \"sub\"]\n\tpath = a\n",
		"[include\n\tpath = a\n",
		"[include]\n\t1path = a\n",
		"[include]\n\tpa_th = a\n",
		"[include]\n\tpath = a\n\tpath = b\n",
		"[include]\n\tpath =   spaced value   \n",
		"[include]\n\tpath = \"\"\n",
		"[include]\n\tpath = \"q\" \"r\"\n",
		"; comment\n# comment\n[include]\n\tpath = a\n",
		"[section \"sub\\\"quote\"]\n\tkey = v\n[include]\npath = a",
		"[section \"sub\\\nline\"]\n\tkey = v\n",
		"key = no section\n",
		"[include]\n\tpath = a\n\x00",
		"[include]\n\tpath = tab\there\n",
		"[include]\n\tpath = \"in#side\";out\n",
		"[include] path = one [include] path = two\n",
	}
	rng := rand.New(rand.NewPCG(1, 2))
	parts := []string{"[include]\n", "[includeIf \"gitdir:x\"]\n", "[core]\n", "\tpath = a\n", "\tpath = \"b c\"\n", "\tpath = d \\\n e\n", "# c\n", "\tkey = v ; c\n", "\r\n", "[x \"y\"]", " path = f\n", "\tpath=\"g;h\"#i\n"}
	for range 200 {
		var b strings.Builder
		for range 1 + rng.IntN(6) {
			b.WriteString(parts[rng.IntN(len(parts))])
		}
		cases = append(cases, b.String())
	}
	file := filepath.Join(home, "probe.gitconfig")
	for _, c := range cases {
		writeFile(t, file, c)
		got, ok := configIncludes([]byte(c))
		cmd := exec.Command("git", "config", "--file", file, "--no-includes", "--list", "-z")
		cmd.Env = gitEnv()
		out, err := cmd.Output()
		var want []string
		wantOK := err == nil
		if wantOK {
			for _, kv := range strings.Split(strings.TrimSuffix(string(out), "\x00"), "\x00") {
				key, value, hasValue := strings.Cut(kv, "\n")
				parts := strings.Split(key, ".")
				if len(parts) < 2 || parts[len(parts)-1] != "path" || (parts[0] != "include" && parts[0] != "includeif") {
					continue
				}
				if !hasValue {
					wantOK = false // git refuses a path without a value when it follows includes
					break
				}
				want = append(want, value)
			}
		}
		if ok != wantOK || ok && strings.Join(got, "\x00") != strings.Join(want, "\x00") {
			t.Errorf("config %q:\n parser %q, %v\n    git %q, %v", c, got, ok, want, wantOK)
		}
	}
}

// TestCacheDifferential builds many repositories with generated names,
// branches, remotes and layouts, changes them at random, and expects the
// cache to answer exactly like Resolve after every step.
func TestCacheDifferential(t *testing.T) {
	home, err := filepath.EvalSymlinks(gitEnvForTest(t))
	if err != nil {
		t.Fatal(err)
	}
	// Only the answers are compared here, so git runs uncounted.
	f := cacheFixture{home: home, cache: Cache{Dir: filepath.Join(home, "state", "cache", "identity")}, runs: func() int { return 0 }}
	rng := rand.New(rand.NewPCG(20261030, 7))
	names := []string{"app", "My Repo!", "ops_runbooks_2026", "sk-learn-pipeline", "ünïcode", "dots.and-dashes", "x"}
	branches := []string{"main", "master", "feature/x", "release-1.2", "fix_underscore", "ünï-branch", "default", "a/b/c", "token-refresh", "HEADS"}
	remotes := []string{
		"git@github.com:HeyNag/Eva-MD.git", "https://github.com/heynag/eva-md", "ssh://git@gitlab.example:2222/team/sub/repo.git",
		"https://user:pw@example.com/a/b.git", "git://example.com/x/y", "/local/path/repo", "file:///srv/repo.git", "not a url",
		"https://gitlab.example/team__sub/repo.git", "https://github.com/acme/tools.sk-learn-pipeline-tools.git",
	}
	pick := func(list []string) string { return list[rng.IntN(len(list))] }
	compared := 0
	check := func(t *testing.T, cwd string) {
		t.Helper()
		for range 2 {
			f.resolve(t, cwd)
			compared++
		}
	}
	for i := range 20 {
		root := filepath.Join(f.home, "gen", fmt.Sprintf("%02d", i), pick(names))
		mkdir(t, root)
		runGit(t, root, "init", "-q", "-b", pick(branches))
		if rng.IntN(4) > 0 {
			commit(t, root, "first")
		}
		if rng.IntN(3) > 0 {
			runGit(t, root, "remote", "add", "origin", pick(remotes))
		}
		real, err := filepath.EvalSymlinks(root)
		if err != nil {
			t.Fatal(err)
		}
		cwds := []string{real, mkdir(t, filepath.Join(real, "src", "deep"))}
		check(t, cwds[0])
		wt := ""
		for step := range 8 {
			cwd := pick(cwds)
			switch rng.IntN(11) {
			case 0:
				tryGit(real, "checkout", "-q", "-B", pick(branches))
			case 1:
				if commitExists(real) {
					runGit(t, real, "checkout", "-q", "--detach")
				}
			case 2:
				commit(t, real, fmt.Sprintf("step %d", step))
			case 3:
				runGit(t, real, "config", "remote.origin.url", pick(remotes))
			case 4:
				tryGit(real, "config", "--unset-all", "remote.origin.url")
			case 5:
				if commitExists(real) {
					runGit(t, real, "tag", "-f", pick(branches))
				}
			case 6:
				runGit(t, real, "pack-refs", "--all")
			case 7:
				if wt == "" && commitExists(real) {
					wt = filepath.Join(f.home, "gen", fmt.Sprintf("%02d-wt", i))
					runGit(t, real, "worktree", "add", "-q", "--detach", wt)
					wt, _ = filepath.EvalSymlinks(wt)
					cwds = append(cwds, wt)
				}
			case 8:
				runGit(t, real, "config", "core.warnAmbiguousRefs", fmt.Sprint(rng.IntN(2) == 0))
			case 9:
				if wt != "" {
					tryGit(wt, "checkout", "-q", "-B", "wt-"+pick(branches))
				}
			case 10:
				global := os.Getenv("GIT_CONFIG_GLOBAL")
				data, _ := os.ReadFile(global)
				writeFile(t, global, string(data)+"[remote \"origin\"]\n\turl = "+pick(remotes)+"\n")
			}
			check(t, cwd)
		}
	}
	if compared < 300 {
		t.Fatalf("only %d comparisons", compared)
	}
}

func commitExists(dir string) bool {
	cmd := exec.Command("git", "-C", dir, "rev-parse", "--verify", "--quiet", "HEAD")
	cmd.Env = gitEnv()
	return cmd.Run() == nil
}

// BenchmarkCacheHit and BenchmarkResolveLive compare one answer from a fresh
// entry with one live resolve of the same repository.
func BenchmarkCacheHit(b *testing.B) {
	c, repo := benchRepo(b)
	if _, err := c.Resolve(context.Background(), repo); err != nil {
		b.Fatal(err)
	}
	for b.Loop() {
		if _, err := c.Resolve(context.Background(), repo); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkResolveLive(b *testing.B) {
	_, repo := benchRepo(b)
	for b.Loop() {
		if _, err := Resolve(context.Background(), repo); err != nil {
			b.Fatal(err)
		}
	}
}

func benchRepo(b *testing.B) (Cache, string) {
	home, err := filepath.EvalSymlinks(b.TempDir())
	if err != nil {
		b.Fatal(err)
	}
	b.Setenv("HOME", home)
	b.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(home, "gitconfig"))
	b.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	repo := filepath.Join(home, "repo")
	for _, args := range [][]string{{"init", "-q", "-b", "main", repo}, {"-C", repo, "-c", "user.name=B", "-c", "user.email=b@example.com", "commit", "-q", "--allow-empty", "-m", "x"}, {"-C", repo, "remote", "add", "origin", "https://github.com/acme/bench.git"}} {
		cmd := exec.Command("git", args...)
		cmd.Env = gitEnv()
		if out, err := cmd.CombinedOutput(); err != nil {
			b.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	return Cache{Dir: filepath.Join(home, "cache")}, repo
}
