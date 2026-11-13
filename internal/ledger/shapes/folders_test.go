package shapes

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/heyNag/eva-md/internal/identity"
	"github.com/heyNag/eva-md/internal/testgitenv"
)

func TestMain(m *testing.M) {
	testgitenv.Clear()
	identity.Timeout, identity.WaitDelay = time.Minute, time.Minute
	os.Exit(m.Run())
}

// repo makes a git repository at the folder name of the home, with remote
// as its origin ("" for none), and has eva-md's identity cache resolve it
// as a hook does. It returns the folder as the hosts name it, through the
// home's link when there is one.
func (f *fixture) repo(name, remote string) string {
	f.t.Helper()
	dir := filepath.Join(f.real, name)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		f.t.Fatal(err)
	}
	git := func(args ...string) {
		f.t.Helper()
		if out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput(); err != nil {
			f.t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, ".git")); err != nil {
		git("init", "--quiet")
	}
	if remote != "" {
		git("remote", "add", "origin", remote)
	}
	p, err := identity.Cache{Dir: filepath.Join(f.dirs.Cache(), "identity")}.Resolve(context.Background(), dir)
	if err != nil || p.Root == "" {
		f.t.Fatalf("resolving %s: %+v, %v", dir, p, err)
	}
	return filepath.Join(f.home, name)
}

// excludedRepo makes a repository whose remote the config excludes, as
// repo does, and returns its folder as the hosts name it.
func (f *fixture) excludedRepo() string {
	f.t.Helper()
	f.config.Exclude = append(f.config.Exclude, "remote:github.com/"+marker+"-org/*")
	return f.repo(marker+"-secret", "git@github.com:"+marker+"-org/"+marker+"-secret.git")
}

// A folder is left out by the remote of the deepest repository eva-md's
// identity cache holds it in, as written or through a link; a folder no
// entry holds, or in a repository whose remote is not excluded, is not.
// Without a remote exclusion the cache is not needed.
func TestAFolderIsJudgedByItsRepositorysRemote(t *testing.T) {
	for _, link := range links {
		t.Run(link, func(t *testing.T) {
			f := newFixture(t, link)
			secret := f.excludedRepo()
			sub := filepath.Join(secret, "cmd")
			if err := os.MkdirAll(filepath.Join(f.real, marker+"-secret", "cmd"), 0o700); err != nil {
				t.Fatal(err)
			}
			nested := f.repo(filepath.Join(marker+"-secret", "vendor"), "git@github.com:"+marker+"-open/lib.git")
			other := f.repo(marker+"-open", "git@github.com:"+marker+"-open/app.git")
			plain := filepath.Join(f.home, marker+"-cwd")
			away := outside(f.options())
			for dir, want := range map[string]bool{
				secret: true, sub: true, filepath.Join(f.real, marker+"-secret", "gone", "since"): true,
				nested: false, other: false, plain: false,
				filepath.Join(f.real, marker+"-secret"): true,
			} {
				if got := away(dir); got != want {
					t.Errorf("%s left out: %v, want %v", dir, got, want)
				}
			}
			f.config.Exclude = nil
			if outside(f.options())(secret) {
				t.Errorf("left out with no exclusion")
			}
		})
	}
}
