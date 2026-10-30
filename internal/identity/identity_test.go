package identity

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/heyNag/eva-md/internal/redact"
)

func TestNormalizeRemote(t *testing.T) {
	gitEnvForTest(t)
	const want = "github.com/heynag/eva-md"
	cases := []struct {
		in   string
		want string
		ok   bool
	}{
		{"git@github.com:heyNag/eva-md.git", want, true},
		{"git@github.com:heyNag/eva-md", want, true},
		{"github.com:heyNag/eva-md.git", want, true},
		{"  git@github.com:heyNag/eva-md.git\n", want, true},
		{"https://github.com/heyNag/eva-md.git", want, true},
		{"https://github.com/heyNag/eva-md", want, true},
		{"https://github.com/heyNag/eva-md/", want, true},
		{"https://github.com/heyNag/eva-md.git/", want, true},
		{"https://github.com//heyNag//eva-md.git", want, true},
		{"https://user:pass@github.com/heyNag/eva-md.git", want, true},
		{"https://oauth2@github.com/heyNag/eva-md.git", want, true},
		{"https://github.com:443/heyNag/eva-md.git", want, true},
		{"http://github.com/heyNag/eva-md.git", want, true},
		{"HTTPS://GitHub.COM/HeyNag/Eva-MD.GIT", want, true},
		{"https://github.com/heyNag/eva-md.git?ref=main#frag", want, true},
		{"ssh://git@github.com/heyNag/eva-md.git", want, true},
		{"ssh://git@github.com:22/heyNag/eva-md.git", want, true},
		{"ssh://github.com/heyNag/eva-md", want, true},
		{"git+ssh://git@github.com/heyNag/eva-md.git", want, true},
		{"git://github.com/heyNag/eva-md.git", want, true},
		{"git@gitlab.corp.example:team/sub/repo.git", "gitlab.corp.example/team/sub/repo", true},
		{"https://gitlab.corp.example:8443/team/sub/repo.git", "gitlab.corp.example/team/sub/repo", true},
		{"ssh://git@gitlab.corp.example:2222/team/repo.git", "gitlab.corp.example/team/repo", true},
		{"ssh://git@github.com/~nag/repo.git", "github.com/~nag/repo", true},
		{"", "", false},
		{"   ", "", false},
		{"/Users/me/repos/eva-md", "", false},
		{"/tmp/a:b", "", false},
		{"../eva-md", "", false},
		{"./eva-md.git", "", false},
		{"some/dir:repo", "", false},
		{"file:///Users/me/eva-md.git", "", false},
		{`C:\repos\eva-md`, "", false},
		{"ftp://example.com/x.git", "", false},
		{"https://github.com", "", false},
		{"https://github.com/", "", false},
		{"git@github.com:", "", false},
		{"not a url", "", false},
		{"https://github.com/heyNag/eva-md.git\x00x", "", false},
		{"https://github.com/heyNag/\neva-md", "", false},
		{"https://github.com/heyNag/eva%20md", "", false},
		{"https://github.com/heyNag/eva\u200b-md", "", false},
		{"https://github.com/heyNag/eva\\md", "", false},
		{"ssh://git@[2001:db8::1]:2222/Team/Repo.git", "2001:db8::1/team/repo", true},
	}
	if len(cases) < 25 {
		t.Fatalf("only %d cases", len(cases))
	}
	for _, c := range cases {
		got, ok := NormalizeRemote(c.in)
		if got != c.want || ok != c.ok {
			t.Errorf("NormalizeRemote(%q) = %q, %v; want %q, %v", c.in, got, ok, c.want, c.ok)
		}
	}
}

func TestFolderName(t *testing.T) {
	gitEnvForTest(t)
	for in, want := range map[string]string{
		"eva-md":                "eva-md",
		"My Repo!":              "my-repo",
		"...":                   "folder",
		strings.Repeat("a", 50): strings.Repeat("a", 40),
	} {
		if got := folderName(in); got != want {
			t.Errorf("folderName(%q) = %q, want %q", in, got, want)
		}
	}
}

// gitEnvForTest points git at a temp home and global config with a fixed
// identity and no signing, so tests never read the user's git config.
func gitEnvForTest(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not found")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	cfg := filepath.Join(home, "gitconfig")
	body := "[user]\n\tname = Test\n\temail = test@example.com\n[commit]\n\tgpgsign = false\n[tag]\n\tgpgsign = false\n[init]\n\tdefaultBranch = main\n"
	if err := os.WriteFile(cfg, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GIT_CONFIG_GLOBAL", cfg)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	return home
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-c", "core.fsmonitor=false", "-c", "core.hooksPath=/dev/null", "-c", "init.templateDir=", "-C", dir}, args...)...)
	cmd.Env = gitEnv()
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func newRepo(t *testing.T, dir string, commit bool) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "init", "-q")
	if commit {
		if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte("x\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		runGit(t, dir, "add", "f.txt")
		runGit(t, dir, "commit", "-q", "-m", "first")
	}
	real, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatal(err)
	}
	return real
}

func hash6(root string) string {
	sum := sha256.Sum256([]byte(root))
	return hex.EncodeToString(sum[:])[:6]
}

func TestResolve(t *testing.T) {
	home := gitEnvForTest(t)
	ctx := context.Background()

	repo := newRepo(t, filepath.Join(home, "code", "eva-md"), true)
	runGit(t, repo, "remote", "add", "origin", "git@github.com:HeyNag/Eva-MD.git")
	sub := filepath.Join(repo, "internal", "x")
	if err := os.MkdirAll(sub, 0o700); err != nil {
		t.Fatal(err)
	}
	wantRepo := Project{
		Kind: KindRepo, Key: "github.com__heynag__eva-md", Slug: "eva-md",
		Remote: "github.com/heynag/eva-md", Root: repo, Branch: "main",
	}
	got, err := Resolve(ctx, sub)
	if err != nil || got != wantRepo {
		t.Fatalf("repo:\n got %+v, %v\nwant %+v", got, err, wantRepo)
	}

	t.Run("linked worktree", func(t *testing.T) {
		wt := filepath.Join(home, "code", "eva-md-feature")
		runGit(t, repo, "worktree", "add", "-q", "-b", "feature", wt)
		realWT, _ := filepath.EvalSymlinks(wt)
		got, err := Resolve(ctx, wt)
		want := wantRepo
		want.Root, want.Branch = realWT, "feature"
		if err != nil || got != want {
			t.Fatalf("got %+v, %v\nwant %+v", got, err, want)
		}
	})

	t.Run("second clone shares the key", func(t *testing.T) {
		other := newRepo(t, filepath.Join(home, "elsewhere", "copy"), true)
		runGit(t, other, "remote", "add", "origin", "https://github.com/heynag/eva-md")
		got, err := Resolve(ctx, other)
		if err != nil || got.Key != wantRepo.Key || got.Slug != "eva-md" || got.Root != other {
			t.Fatalf("got %+v, %v", got, err)
		}
	})

	t.Run("detached HEAD", func(t *testing.T) {
		d := newRepo(t, filepath.Join(home, "code", "detached"), true)
		runGit(t, d, "remote", "add", "origin", "https://github.com/heynag/detached.git")
		runGit(t, d, "checkout", "-q", "--detach")
		got, err := Resolve(ctx, d)
		if err != nil || !got.Detached || got.Branch != "" || got.Kind != KindRepo || got.Slug != "detached" {
			t.Fatalf("got %+v, %v", got, err)
		}
	})

	t.Run("no remote", func(t *testing.T) {
		l := newRepo(t, filepath.Join(home, "code", "My Repo!"), true)
		got, err := Resolve(ctx, l)
		h := hash6(l)
		want := Project{Kind: KindLocal, Key: "local-my-repo-" + h, Slug: "my-repo-" + h, Root: l, Branch: "main"}
		if err != nil || got != want {
			t.Fatalf("got %+v, %v\nwant %+v", got, err, want)
		}
	})

	t.Run("no commits", func(t *testing.T) {
		e := newRepo(t, filepath.Join(home, "code", "empty"), false)
		got, err := Resolve(ctx, e)
		if err != nil || got.Kind != KindLocal || got.Branch != DefaultBranch || got.Detached || got.Root != e {
			t.Fatalf("got %+v, %v", got, err)
		}
	})

	t.Run("no commits with a remote", func(t *testing.T) {
		e := newRepo(t, filepath.Join(home, "code", "empty-remote"), false)
		runGit(t, e, "remote", "add", "origin", "https://github.com/heynag/eva-md.git")
		got, err := Resolve(ctx, e)
		if err != nil || got.Kind != KindRepo || got.Branch != DefaultBranch || got.Detached || got.Key != wantRepo.Key {
			t.Fatalf("got %+v, %v", got, err)
		}
	})

	t.Run("symlinked root shares local key", func(t *testing.T) {
		real := newRepo(t, filepath.Join(home, "code", "real-root"), true)
		alias := filepath.Join(home, "alias")
		if err := os.Symlink(real, alias); err != nil {
			t.Fatal(err)
		}
		viaReal, err := Resolve(ctx, real)
		if err != nil {
			t.Fatal(err)
		}
		viaAlias, err := Resolve(ctx, alias)
		if err != nil || viaAlias != viaReal {
			t.Fatalf("symlink differs: %+v, %v", viaAlias, err)
		}
	})

	t.Run("outside any repo", func(t *testing.T) {
		out := filepath.Join(home, "plain")
		if err := os.MkdirAll(out, 0o700); err != nil {
			t.Fatal(err)
		}
		t.Setenv("GIT_CEILING_DIRECTORIES", home)
		got, err := Resolve(ctx, out)
		if err != nil || got != General() {
			t.Fatalf("got %+v, %v", got, err)
		}
	})

	t.Run("missing folder", func(t *testing.T) {
		got, err := Resolve(ctx, filepath.Join(home, "does-not-exist"))
		if err != nil || got != General() {
			t.Fatalf("got %+v, %v", got, err)
		}
	})

	t.Run("GIT_DIR is ignored", func(t *testing.T) {
		other := newRepo(t, filepath.Join(home, "code", "decoy"), true)
		t.Setenv("GIT_DIR", filepath.Join(other, ".git"))
		t.Setenv("GIT_WORK_TREE", other)
		got, err := Resolve(ctx, sub)
		if err != nil || got != wantRepo {
			t.Fatalf("got %+v, %v", got, err)
		}
	})

	t.Run("injected origin config is ignored", func(t *testing.T) {
		t.Setenv("GIT_CONFIG_COUNT", "1")
		t.Setenv("GIT_CONFIG_KEY_0", "remote.origin.url")
		t.Setenv("GIT_CONFIG_VALUE_0", "https://example.com/decoy/repo.git")
		got, err := Resolve(ctx, sub)
		if err != nil || got != wantRepo {
			t.Fatalf("got %+v, %v", got, err)
		}
	})

	t.Run("relative cwd", func(t *testing.T) {
		if got, err := Resolve(ctx, "code/eva-md"); err == nil || got != General() {
			t.Fatalf("got %+v, %v; want an error", got, err)
		}
	})

	t.Run("cancelled context", func(t *testing.T) {
		c, cancel := context.WithCancel(ctx)
		cancel()
		if got, err := Resolve(c, repo); err == nil || got != General() {
			t.Fatalf("got %+v, %v; want an error", got, err)
		}
	})
}

func TestGitReadDeadlineAndHardening(t *testing.T) {
	home := gitEnvForTest(t)
	bin := filepath.Join(home, "bin")
	if err := os.Mkdir(bin, 0o700); err != nil {
		t.Fatal(err)
	}
	script := "#!/bin/sh\nexec /bin/sleep 5\n"
	if err := os.WriteFile(filepath.Join(bin, "git"), []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	started := time.Now()
	_, err := Resolve(ctx, home)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("got %v, want deadline", err)
	}
	if elapsed := time.Since(started); elapsed > 250*time.Millisecond {
		t.Fatalf("Resolve took %s", elapsed)
	}
}

func TestGitDoesNotRunFSMonitor(t *testing.T) {
	home := gitEnvForTest(t)
	repo := newRepo(t, filepath.Join(home, "repo"), true)
	marker := filepath.Join(home, "fsmonitor-ran")
	script := filepath.Join(home, "fsmonitor.sh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\ntouch '"+marker+"'\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	runGit(t, repo, "config", "core.fsmonitor", script)
	if _, err := Resolve(context.Background(), repo); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(marker); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("fsmonitor ran: %v", err)
	}
}

func TestDistinctRemotesHaveDistinctKeys(t *testing.T) {
	home := gitEnvForTest(t)
	seen := make(map[string]string)
	for i, remote := range []string{
		"https://gitlab.example/team__sub/repo.git",
		"https://gitlab.example/team/sub__repo.git",
		"https://gitlab.example/team_u_usub/repo.git",
		"https://gitlab.example/team/sub_u_urepo.git",
	} {
		repo := newRepo(t, filepath.Join(home, "repo"+string(rune('a'+i))), false)
		runGit(t, repo, "remote", "add", "origin", remote)
		project, err := Resolve(context.Background(), repo)
		if err != nil {
			t.Fatal(err)
		}
		if other, ok := seen[project.Key]; ok {
			t.Fatalf("remote scope collision between %s and %s", remote, other)
		}
		seen[project.Key] = remote
	}
}

func TestCorruptRepositoryFailsClosed(t *testing.T) {
	home := gitEnvForTest(t)
	repo := newRepo(t, filepath.Join(home, "repo"), true)
	runGit(t, repo, "remote", "add", "origin", "https://example.com/private/repo.git")
	if err := os.WriteFile(filepath.Join(repo, ".git", "config"), []byte("[invalid config\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Resolve(context.Background(), repo); err == nil {
		t.Fatal("corrupt repository was treated as general/local")
	}
}

func TestGitOutputIsBounded(t *testing.T) {
	gitEnvForTest(t)
	out := cappedOutput{limit: 64 << 10}
	data := strings.Repeat("x", 128<<10)
	if _, err := io.Copy(&out, io.LimitReader(strings.NewReader(data), int64(len(data)))); err != nil {
		t.Fatal(err)
	}
	if !out.truncated || len(out.String()) != out.limit {
		t.Fatalf("captured %d bytes, truncated=%v", len(out.String()), out.truncated)
	}
}

// A folder or repo name can look like a provider token, as
// ops_runbooks_2026 looks like a 1Password token and sk-learn-pipeline like
// an OpenAI key. The key made from it would be refused by the inbox, which
// refuses identity fields in a token format, so nothing would ever be stored
// for the project. Such a project is keyed by a hash instead.
func TestTokenLikeNamesGetPlainKeys(t *testing.T) {
	home := gitEnvForTest(t)
	ctx := context.Background()
	for _, name := range []string{"ops_runbooks_2026", "ops_runbooks_and_scripts", "sk-learn-pipeline", "sk-learn-notebooks", "xoxb-notification-relay"} {
		repo := newRepo(t, filepath.Join(home, "code", name), false)
		got, err := Resolve(ctx, repo)
		if err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256([]byte(repo))
		if got.Kind != KindLocal || got.Key != "local-"+hex.EncodeToString(sum[:])[:12] || got.Slug != folderName(name)+"-"+hash6(repo) {
			t.Errorf("%s: %+v", name, got)
		}
		if clean, n := redact.RedactKnown(got.Key); n != 0 || clean != got.Key {
			t.Errorf("%s: key %q still in a token format", name, got.Key)
		}
		if again, _ := Resolve(ctx, repo); again.Key != got.Key {
			t.Errorf("%s: key not stable", name)
		}
	}
	// Ordinary names keep the readable key.
	repo := newRepo(t, filepath.Join(home, "code", "token-refresh-worker"), false)
	if got, _ := Resolve(ctx, repo); got.Key != "local-token-refresh-worker-"+hash6(repo) {
		t.Errorf("readable key lost: %q", got.Key)
	}
	// So does a remote, unless its key would look like a token too.
	remote := newRepo(t, filepath.Join(home, "code", "tools"), false)
	runGit(t, remote, "remote", "add", "origin", "https://github.com/acme/tools.sk-learn-pipeline-tools.git")
	got, err := Resolve(ctx, remote)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte("github.com/acme/tools.sk-learn-pipeline-tools"))
	if got.Kind != KindRepo || got.Key != "repo-"+hex.EncodeToString(sum[:])[:12] || got.Remote != "github.com/acme/tools.sk-learn-pipeline-tools" || got.Slug != "tools.sk-learn-pipeline-tools" {
		t.Errorf("remote: %+v", got)
	}
}
