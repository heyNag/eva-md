package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/heyNag/eva-md/internal/config"
	"github.com/heyNag/eva-md/internal/identity"
	"github.com/heyNag/eva-md/internal/paths"
	"github.com/heyNag/eva-md/internal/wikigit"
	"github.com/heyNag/eva-md/internal/wikiinit"
)

// gitHomeFixture is isolated with the home's real path, as init resolves it, and a
// git config of the test's own: a made-up author, no signing, main as the
// first branch, plus any extra lines.
func gitHomeFixture(t *testing.T, extra string) (paths.Dirs, string) {
	t.Helper()
	isolated(t)
	home, err := filepath.EvalSymlinks(os.Getenv("HOME"))
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(home, "gitconfig"))
	for _, name := range []string{"GIT_CONFIG_COUNT", "GIT_CONFIG_PARAMETERS", "GIT_DIR", "GIT_WORK_TREE", "GIT_INDEX_FILE", "GIT_COMMON_DIR", "GIT_TEMPLATE_DIR", "GIT_SSH", "GIT_SSH_COMMAND"} {
		t.Setenv(name, "")
	}
	gitconfig := "[user]\nname = Eva Fixture\nemail = fixture@example.invalid\n[commit]\ngpgsign = false\n[init]\ndefaultBranch = main\n" + extra
	if err := os.WriteFile(filepath.Join(home, "gitconfig"), []byte(gitconfig), 0o600); err != nil {
		t.Fatal(err)
	}
	d, err := paths.Default()
	if err != nil {
		t.Fatal(err)
	}
	return d, home
}

func gitRun(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := wikigit.Run(context.Background(), time.Minute, dir, args...)
	if err != nil {
		t.Fatalf("git %s: %v", args[0], err)
	}
	return out
}

// addTimelineLine appends a Timeline line to the wiki's default thread page.
func addTimelineLine(t *testing.T, wiki, line string) {
	t.Helper()
	file := filepath.Join(wiki, "general", "threads", "default.md")
	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, append(data, []byte("### 2026-10-31\n- "+line+"\n")...), 0o644); err != nil {
		t.Fatal(err)
	}
}

func evaMD(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	var out, errOut bytes.Buffer
	code := run(context.Background(), args, strings.NewReader(""), &out, &errOut)
	return code, out.String(), errOut.String()
}

// commitRecordsSeen reads this month's commit records.
func commitRecordsSeen(t *testing.T, d paths.Dirs) []wikigit.Result {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(d.State, "commits", time.Now().Format("2006-01")+".jsonl"))
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	var out []wikigit.Result
	s := bufio.NewScanner(bytes.NewReader(data))
	for s.Scan() {
		var r wikigit.Result
		if err := json.Unmarshal(s.Bytes(), &r); err != nil {
			t.Fatal(err)
		}
		out = append(out, r)
	}
	return out
}

// eva-md maintain commits and pushes the day's changes, then lints the wiki
// into <state>/lint.json and removes attempt markers older than 14 days. A
// maintain already running makes it exit at once; commits = false skips the
// commit and keeps the lint; --push-only pushes only while the newest
// record's push is pending, which a remote out of reach leaves it.
func TestMaintain(t *testing.T) {
	d, home := gitHomeFixture(t, "")
	remote, wiki := filepath.Join(home, "remote.git"), filepath.Join(home, "wiki")
	gitRun(t, home, "init", "--bare", "--quiet", remote)
	if _, err := wikiinit.Run(context.Background(), wikiinit.Options{Remote: remote, Dir: wiki, Dirs: d, Version: "test", OKFModule: "v0.7.2"}); err != nil {
		t.Fatal(err)
	}
	markers := filepath.Join(d.State, "commit")
	today := "attempt-" + time.Now().Format(time.DateOnly)
	old := "attempt-" + time.Now().AddDate(0, 0, -15).Format(time.DateOnly)
	for _, name := range []string{today, old, "push-retry"} {
		if err := paths.EnsureDir(markers); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(markers, name), nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	addTimelineLine(t, wiki, "Chose the amber switch.")
	code, out, errOut := evaMD(t, "maintain")
	if code != 0 || out != "maintain: committed (1 page), push ok\nmaintain: lint: 1 thread pages, 0 over 512 KB, 0 bad refs\n" || errOut != "" {
		t.Fatalf("maintain: %d %q %q", code, out, errOut)
	}
	if got := gitRun(t, remote, "log", "-1", "--format=%s"); !strings.HasPrefix(got, "eva-md daily ") {
		t.Fatalf("the remote's newest commit is %q", got)
	}
	if info, err := os.Stat(filepath.Join(d.State, "lint.json")); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("lint.json: %v", err)
	}
	if _, err := os.Stat(filepath.Join(markers, old)); !os.IsNotExist(err) {
		t.Fatal("an attempt marker older than 14 days was kept")
	}
	for _, name := range []string{today, "push-retry"} {
		if _, err := os.Stat(filepath.Join(markers, name)); err != nil {
			t.Fatalf("%s was removed: %v", name, err)
		}
	}
	runs := len(commitRecordsSeen(t, d))

	// Another maintain holds the lock: this one does nothing.
	lock, err := os.OpenFile(filepath.Join(d.Locks(), "maintain.lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		t.Fatal(err)
	}
	addTimelineLine(t, wiki, "Chose the teal switch.")
	if code, out, errOut := evaMD(t, "maintain"); code != 0 || out != "maintain: busy\n" || errOut != "" || len(commitRecordsSeen(t, d)) != runs {
		t.Fatalf("busy: %d %q %q", code, out, errOut)
	}
	lock.Close()

	// commits = false: no commit, the lint still runs.
	c, err := config.Load(d.ConfigFile())
	if err != nil {
		t.Fatal(err)
	}
	c.Commits = false
	if err := config.Save(d.ConfigFile(), c); err != nil {
		t.Fatal(err)
	}
	if code, out, _ := evaMD(t, "maintain"); code != 0 || !strings.HasPrefix(out, "maintain: commits are off (commits = false in the config)\nmaintain: lint: ") || len(commitRecordsSeen(t, d)) != runs {
		t.Fatalf("commits off: %d %q", code, out)
	}
	c.Commits = true
	if err := config.Save(d.ConfigFile(), c); err != nil {
		t.Fatal(err)
	}
	if code, out, _ := evaMD(t, "maintain", "--push-only"); code != 0 || out != "maintain: no push is pending\n" {
		t.Fatalf("nothing pending: %d %q", code, out)
	}

	// The remote is out of reach: the commit is made and its push waits.
	away := remote + ".away"
	if err := os.Rename(remote, away); err != nil {
		t.Fatal(err)
	}
	if code, out, _ := evaMD(t, "maintain"); code != 0 || !strings.HasPrefix(out, "maintain: committed (1 page), push pending\n") {
		t.Fatalf("offline: %d %q", code, out)
	}
	if code, out, _ := evaMD(t, "maintain", "--push-only"); code != 0 || out != "maintain: push pending\n" {
		t.Fatalf("still offline: %d %q", code, out)
	}
	if err := os.Rename(away, remote); err != nil {
		t.Fatal(err)
	}
	if code, out, _ := evaMD(t, "maintain", "--push-only"); code != 0 || out != "maintain: push ok\n" {
		t.Fatalf("back online: %d %q", code, out)
	}
	if code, out, _ := evaMD(t, "maintain", "--push-only"); code != 0 || out != "maintain: no push is pending\n" {
		t.Fatalf("pushed: %d %q", code, out)
	}
	if got, want := gitRun(t, remote, "rev-parse", "HEAD"), gitRun(t, wiki, "rev-parse", "HEAD"); got != want {
		t.Fatalf("the remote is at %s, the wiki at %s", got, want)
	}
}

// fakeSSH stands in for ssh to the made-up gitlab.example.com. It serves the
// bare repos under ROOT to git's upload-pack and receive-pack; while
// ROOT/offline exists it fails as a host out of reach does, and while
// ROOT/offline-push exists it fails pushes only. Shell built-ins and git
// only, as isolated's PATH holds.
const fakeSSH = `#!/bin/sh
root='ROOT'
for last; do :; done
cmd=${last%% *}
repo=${last#* }
repo=${repo#\'}
repo=${repo%\'}
if [ -e "$root/offline" ] || { [ -e "$root/offline-push" ] && [ "$cmd" = git-receive-pack ]; }; then
	printf '%s\n' 'ssh: connect to host gitlab.example.com port 22: Network is unreachable' >&2
	exit 255
fi
exec git "${cmd#git-}" "$root/${repo#/}"
`

// A wiki on a self-hosted GitLab, in each form a remote takes: identity
// names the project the same way in every form; init clones, scaffolds and
// pushes over SSH, fails plainly while the host is out of reach and works
// when run again; the daily commit's origin guard accepts the remote; and a
// host out of reach leaves the push pending until a retry finds it. Git
// reaches gitlab.example.com through a fake ssh that serves bare repos from
// a local folder, and an https remote through a local proxy that drops
// every connection, so nothing leaves the machine.
func TestSelfHostedGitLabRemotes(t *testing.T) {
	const (
		scp   = "git@gitlab.example.com:group/sub/repo.git"
		port  = "ssh://git@gitlab.example.com:2222/group/repo.git"
		https = "https://gitlab.example.com/group/repo.git"
	)
	// A proxy that drops every connection: an https host out of reach.
	proxy, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { proxy.Close() })
	go func() {
		for {
			conn, err := proxy.Accept()
			if err != nil {
				return
			}
			conn.Close()
		}
	}()
	setup := func(t *testing.T) (paths.Dirs, string, string) {
		d, home := gitHomeFixture(t, "[http]\nproxy = http://"+proxy.Addr().String()+"\n")
		for _, key := range []string{"HTTP_PROXY", "HTTPS_PROXY", "ALL_PROXY", "NO_PROXY", "http_proxy", "https_proxy", "all_proxy", "no_proxy"} {
			t.Setenv(key, "")
		}
		root := filepath.Join(home, "gitlab")
		for _, repo := range []string{"group/sub/repo.git", "group/repo.git"} {
			if err := os.MkdirAll(filepath.Join(root, repo), 0o700); err != nil {
				t.Fatal(err)
			}
			gitRun(t, home, "init", "--bare", "--quiet", filepath.Join(root, repo))
		}
		ssh := filepath.Join(home, "fake-ssh", "ssh")
		if err := os.MkdirAll(filepath.Dir(ssh), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(ssh, []byte(strings.ReplaceAll(fakeSSH, "ROOT", root)), 0o700); err != nil {
			t.Fatal(err)
		}
		t.Setenv("GIT_SSH_COMMAND", ssh)
		return d, home, root
	}
	t.Run("identity", func(t *testing.T) {
		_, home, _ := setup(t)
		for _, tc := range []struct{ remote, id, key string }{
			{scp, "gitlab.example.com/group/sub/repo", "gitlab.example.com__group__sub__repo"},
			{port, "gitlab.example.com/group/repo", "gitlab.example.com__group__repo"},
			{https, "gitlab.example.com/group/repo", "gitlab.example.com__group__repo"},
		} {
			dir := filepath.Join(home, "checkout-"+strings.NewReplacer(":", "-", "/", "-").Replace(tc.remote))
			gitRun(t, home, "init", "--quiet", dir)
			gitRun(t, dir, "remote", "add", "origin", tc.remote)
			p, err := identity.Resolve(context.Background(), dir)
			if err != nil || p.Remote != tc.id || p.Key != tc.key || p.Slug != "repo" {
				t.Fatalf("%s: %+v %v", tc.remote, p, err)
			}
			if _, id, err := wikigit.RemoteIdentity(tc.remote); err != nil || id != tc.id {
				t.Fatalf("init's check of %s: %q %v", tc.remote, id, err)
			}
		}
	})
	t.Run("ssh with a nested group", func(t *testing.T) {
		d, home, root := setup(t)
		wiki := filepath.Join(home, "eva-wiki")
		offline := func(name string, on bool) {
			t.Helper()
			flag := filepath.Join(root, name)
			if !on {
				if err := os.Remove(flag); err != nil {
					t.Fatal(err)
				}
				return
			}
			if err := os.WriteFile(flag, nil, 0o600); err != nil {
				t.Fatal(err)
			}
		}
		// Out of reach, init fails plainly at the clone and leaves nothing.
		offline("offline", true)
		if code, _, errOut := evaMD(t, "init", "--remote", scp, "--dir", wiki); code != 1 || !strings.Contains(errOut, "git clone failed") || strings.Contains(errOut, "unsupported remote") {
			t.Fatalf("offline init: %d %q", code, errOut)
		}
		if _, err := os.Stat(wiki); !os.IsNotExist(err) {
			t.Fatalf("an offline init left the folder: %v", err)
		}
		offline("offline", false)
		// The clone works and the push can't reach the host: init fails,
		// saves no config and keeps its scaffold commit for the next run.
		offline("offline-push", true)
		if code, _, errOut := evaMD(t, "init", "--remote", scp, "--dir", wiki); code != 1 || !strings.Contains(errOut, "git push failed") {
			t.Fatalf("init with the push out of reach: %d %q", code, errOut)
		}
		if _, err := os.Stat(d.ConfigFile()); !os.IsNotExist(err) {
			t.Fatalf("init saved a config without its push: %v", err)
		}
		offline("offline-push", false)
		if code, out, errOut := evaMD(t, "init", "--remote", scp, "--dir", wiki); code != 0 || !strings.Contains(out, "Wiki ready") {
			t.Fatalf("init again: %d %q %q", code, out, errOut)
		}
		c, err := config.Load(d.ConfigFile())
		if err != nil || c.ExpectedRemote != "gitlab.example.com/group/sub/repo" {
			t.Fatalf("config %+v %v", c, err)
		}
		bare := filepath.Join(root, "group/sub/repo.git")
		if gitRun(t, bare, "rev-parse", "HEAD") != gitRun(t, wiki, "rev-parse", "HEAD") {
			t.Fatal("init's commit never reached the remote")
		}
		addTimelineLine(t, wiki, "Chose the amber switch.")
		if code, out, _ := evaMD(t, "maintain"); code != 0 || !strings.HasPrefix(out, "maintain: committed (1 page), push ok\n") {
			t.Fatalf("maintain: %d %q", code, out)
		}
		offline("offline", true)
		addTimelineLine(t, wiki, "Chose the teal switch.")
		if code, out, _ := evaMD(t, "maintain"); code != 0 || !strings.HasPrefix(out, "maintain: committed (1 page), push pending\n") {
			t.Fatalf("offline maintain: %d %q", code, out)
		}
		offline("offline", false)
		if code, out, _ := evaMD(t, "maintain", "--push-only"); code != 0 || out != "maintain: push ok\n" {
			t.Fatalf("the retry: %d %q", code, out)
		}
		if gitRun(t, bare, "rev-parse", "HEAD") != gitRun(t, wiki, "rev-parse", "HEAD") {
			t.Fatal("the retried push never reached the remote")
		}
	})
	t.Run("ssh with a port, then https", func(t *testing.T) {
		_, home, root := setup(t)
		wiki := filepath.Join(home, "eva-wiki")
		if code, out, errOut := evaMD(t, "init", "--remote", port, "--dir", wiki); code != 0 || !strings.Contains(out, "Wiki ready") {
			t.Fatalf("init: %d %q %q", code, out, errOut)
		}
		addTimelineLine(t, wiki, "Chose the amber switch.")
		if code, out, _ := evaMD(t, "maintain"); code != 0 || !strings.HasPrefix(out, "maintain: committed (1 page), push ok\n") {
			t.Fatalf("maintain: %d %q", code, out)
		}
		if gitRun(t, filepath.Join(root, "group/repo.git"), "rev-parse", "HEAD") != gitRun(t, wiki, "rev-parse", "HEAD") {
			t.Fatal("the push never reached the remote")
		}
		// The same project's https form passes the origin guard; its host
		// is out of reach, so the push waits.
		gitRun(t, wiki, "remote", "set-url", "origin", https)
		addTimelineLine(t, wiki, "Chose the teal switch.")
		if code, out, _ := evaMD(t, "maintain"); code != 0 || !strings.HasPrefix(out, "maintain: committed (1 page), push pending\n") {
			t.Fatalf("https maintain: %d %q", code, out)
		}
	})
}
