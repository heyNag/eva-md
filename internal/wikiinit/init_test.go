package wikiinit

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/heyNag/eva-md/internal/config"
	"github.com/heyNag/eva-md/internal/okfadapter"
	"github.com/heyNag/eva-md/internal/paths"
)

func fixture(t *testing.T) Options {
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
	dirs, err := paths.Default()
	if err != nil {
		t.Fatal(err)
	}
	remote := filepath.Join(home, "remote.git")
	testGit(t, home, "init", "--bare", remote)
	return Options{Remote: remote, Dir: filepath.Join(home, "wiki"), Dirs: dirs, Version: "test", OKFModule: "v0.7.2"}
}

func testGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := git(context.Background(), dir, args...)
	if err != nil {
		t.Fatalf("git %s: %v", args[0], err)
	}
	return out
}

func TestInitPushAndAdopt(t *testing.T) {
	o := fixture(t)
	r, err := Run(context.Background(), o)
	if err != nil {
		t.Fatal(err)
	}
	if r.Adopted || r.Instance == "" {
		t.Fatalf("result %+v", r)
	}
	w, err := okfadapter.Open(o.Dir, o.Dirs.Locks())
	if err != nil {
		t.Fatal(err)
	}
	v, err := w.Validate()
	if err != nil {
		t.Fatal(err)
	}
	if len(v.Errors)+len(v.Warnings)+len(v.Orphans)+len(v.BrokenLinks) != 0 {
		t.Fatalf("findings: %+v", v)
	}
	if v.ConceptCount != 3 {
		t.Fatalf("concepts %d", v.ConceptCount)
	}
	head := testGit(t, o.Dir, "rev-parse", "HEAD")
	if got := testGit(t, o.Remote, "rev-parse", "refs/heads/main"); got != head {
		t.Fatalf("remote head differs")
	}
	if status := testGit(t, o.Dir, "status", "--porcelain"); status != "" {
		t.Fatalf("dirty: %s", status)
	}
	c, err := config.Load(o.Dirs.ConfigFile())
	if err != nil {
		t.Fatal(err)
	}
	if c.Instance != r.Instance || c.WikiPath != r.WikiPath || c.ExpectedRemote != "file://"+o.Remote {
		t.Fatalf("config mismatch: %+v", c)
	}
	s, err := config.LoadSchema(filepath.Join(o.Dir, ".eva-md", "schema.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if s.SchemaVersion != 1 || s.OKFSpec != "0.2" || s.OKFModule != "v0.7.2" || s.Created == "" {
		t.Fatalf("schema %+v", s)
	}
	for name, mode := range map[string]os.FileMode{"README.md": 0o644, "general/now.md": 0o644, "general/threads/default.md": 0o644, ".eva-md/schema.toml": 0o600, ".gitignore": 0o600, ".gitattributes": 0o600, "general": 0o700, "general/threads": 0o700, ".eva-md": 0o700} {
		fi, err := os.Stat(filepath.Join(o.Dir, name))
		if err != nil {
			t.Fatal(err)
		}
		if fi.Mode().Perm() != mode {
			t.Errorf("%s mode %o want %o", name, fi.Mode().Perm(), mode)
		}
	}
	c.Exclude = []string{"~/private"}
	c.Effort = map[string]string{"codex": "high"}
	if err := config.Save(o.Dirs.ConfigFile(), c); err != nil {
		t.Fatal(err)
	}
	r2, err := Run(context.Background(), o)
	if err != nil {
		t.Fatal(err)
	}
	if !r2.Adopted || r2.Instance != r.Instance || testGit(t, o.Dir, "rev-parse", "HEAD") != head {
		t.Fatal("adoption changed wiki")
	}
	c2, _ := config.Load(o.Dirs.ConfigFile())
	if len(c2.Exclude) != 1 || c2.EffortFor("codex", "") != "high" {
		t.Fatal("adoption dropped settings")
	}
}

func TestNoPush(t *testing.T) {
	o := fixture(t)
	o.NoPush = true
	if _, err := Run(context.Background(), o); err != nil {
		t.Fatal(err)
	}
	if refs := testGit(t, o.Remote, "for-each-ref", "--format=%(refname)"); refs != "" {
		t.Fatal("unexpected pushed refs")
	}
}

func TestRefusals(t *testing.T) {
	for _, name := range []string{"foreign folder", "nonempty clone", "wrong origin", "push URL", "dirty wiki", "new schema", "instance mismatch", "different config", "unexpected initial file"} {
		t.Run(name, func(t *testing.T) {
			o := fixture(t)
			switch name {
			case "foreign folder":
				os.Mkdir(o.Dir, 0o700)
				os.WriteFile(filepath.Join(o.Dir, "user.txt"), []byte("keep me"), 0o600)
			case "nonempty clone":
				testGit(t, filepath.Dir(o.Dir), "clone", o.Remote, o.Dir)
				os.WriteFile(filepath.Join(o.Dir, "user.txt"), []byte("keep me"), 0o600)
			case "wrong origin":
				testGit(t, filepath.Dir(o.Dir), "clone", o.Remote, o.Dir)
				testGit(t, o.Dir, "remote", "set-url", "origin", "git@example.invalid:other/wiki.git")
			case "push URL":
				testGit(t, filepath.Dir(o.Dir), "clone", o.Remote, o.Dir)
				testGit(t, o.Dir, "remote", "set-url", "--push", "origin", "git@example.invalid:other/wiki.git")
			default:
				if _, err := Run(context.Background(), o); err != nil {
					t.Fatal(err)
				}
				switch name {
				case "dirty wiki":
					os.WriteFile(filepath.Join(o.Dir, "user.txt"), []byte("keep me"), 0o600)
				case "new schema":
					s, _ := config.LoadSchema(filepath.Join(o.Dir, ".eva-md/schema.toml"))
					s.SchemaVersion = 2
					config.SaveSchema(filepath.Join(o.Dir, ".eva-md/schema.toml"), s)
				case "instance mismatch":
					c, _ := config.Load(o.Dirs.ConfigFile())
					c.Instance = "wrong"
					config.Save(o.Dirs.ConfigFile(), c)
				case "different config":
					c, _ := config.Load(o.Dirs.ConfigFile())
					c.WikiPath = filepath.Join(filepath.Dir(o.Dir), "other")
					config.Save(o.Dirs.ConfigFile(), c)
				case "unexpected initial file":
					testGit(t, o.Dir, "update-ref", "-d", "refs/heads/main")
					os.WriteFile(filepath.Join(o.Dir, "user.txt"), []byte("keep me"), 0o600)
					testGit(t, o.Dir, "add", "user.txt")
				}
			}
			if _, err := Run(context.Background(), o); err == nil {
				t.Fatal("wanted refusal")
			}
			if b, err := os.ReadFile(filepath.Join(o.Dir, "user.txt")); err == nil && string(b) != "keep me" {
				t.Fatal("modified user file")
			}
		})
	}
}

func TestProtectedLocations(t *testing.T) {
	o := fixture(t)
	home := filepath.Dir(o.Dir)
	for _, name := range []string{"Documents/wiki", "Desktop/wiki", "Library/Mobile Documents/wiki", ".local/state/eva-md/wiki"} {
		o.Dir = filepath.Join(home, name)
		if _, err := Run(context.Background(), o); err == nil {
			t.Fatalf("accepted %s", name)
		}
		if _, err := os.Stat(o.Dir); !os.IsNotExist(err) {
			t.Fatalf("created %s", name)
		}
	}
	if err := os.MkdirAll(filepath.Join(home, "Documents"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(home, "Documents"), filepath.Join(home, "alias")); err != nil {
		t.Fatal(err)
	}
	o.Dir = filepath.Join(home, "alias", "wiki")
	if _, err := Run(context.Background(), o); err == nil {
		t.Fatal("accepted symlink into Documents")
	}
	if runtime.GOOS == "darwin" {
		for _, name := range []string{"documents/wiki", "desktop/wiki", "library/mobile documents/wiki"} {
			o.Dir = filepath.Join(home, name)
			if _, err := Run(context.Background(), o); err == nil {
				t.Fatalf("accepted case alias %s", name)
			}
		}
	}
}

func TestAdoptionRefusesMetadataSymlink(t *testing.T) {
	o := fixture(t)
	if _, err := Run(context.Background(), o); err != nil {
		t.Fatal(err)
	}
	external := filepath.Join(filepath.Dir(o.Dir), "outside-metadata")
	if err := os.Rename(filepath.Join(o.Dir, ".eva-md"), external); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(external, filepath.Join(o.Dir, ".eva-md")); err != nil {
		t.Fatal(err)
	}
	testGit(t, o.Dir, "add", "--all")
	testGit(t, o.Dir, "commit", "-m", "Fixture external metadata")
	if _, err := Run(context.Background(), o); err == nil {
		t.Fatal("adopted metadata outside wiki")
	}
}

func TestRejectedRemoteDoesNotEchoCredentials(t *testing.T) {
	o := fixture(t)
	secret := strings.Repeat("S", 24)
	o.Remote = "https://user:" + secret + "@example.invalid/wiki.git"
	_, err := Run(context.Background(), o)
	if err == nil || strings.Contains(err.Error(), secret) {
		t.Fatal("credentials not refused safely")
	}
	for _, remote := range []string{"ext::arbitrary-command", "--upload-pack=command", "https://example.invalid/repo?token=" + secret} {
		if _, _, err := remoteIdentity(remote); err == nil {
			t.Fatal("unsafe remote accepted")
		}
	}
}

func TestSignatureFailureIsNotRetried(t *testing.T) {
	o := fixture(t)
	home := filepath.Dir(o.Dir)
	program := filepath.Join(home, "signer")
	if err := os.WriteFile(program, []byte("#!/bin/sh\nprintf x >> \"$HOME/sign-attempts\"\nexit 1\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	testGit(t, home, "config", "--global", "commit.gpgsign", "true")
	testGit(t, home, "config", "--global", "gpg.program", program)
	testGit(t, home, "config", "--global", "user.signingkey", "fixture")
	_, err := Run(context.Background(), o)
	if err == nil {
		t.Fatal("failed signer accepted")
	}
	b, err := os.ReadFile(filepath.Join(home, "sign-attempts"))
	if err != nil || string(b) != "x" {
		t.Fatalf("attempts %q %v", b, err)
	}
	if _, err := os.Stat(o.Dirs.ConfigFile()); !os.IsNotExist(err) {
		t.Fatal("config written after failed commit")
	}
	if got := testGit(t, home, "config", "--global", "commit.gpgsign"); got != "true" {
		t.Fatal("signing config changed")
	}
	if _, err := os.Stat(filepath.Join(o.Dir, "README.md")); err != nil {
		t.Fatal("failed init discarded wiki")
	}
}

func TestGitIgnoresInheritedRepository(t *testing.T) {
	o := fixture(t)
	foreign := filepath.Join(filepath.Dir(o.Dir), "foreign")
	testGit(t, filepath.Dir(o.Dir), "init", foreign)
	t.Setenv("GIT_DIR", filepath.Join(foreign, ".git"))
	t.Setenv("GIT_WORK_TREE", foreign)
	if _, err := Run(context.Background(), o); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("git", "-C", foreign, "rev-parse", "--verify", "HEAD")
	cmd.Env = gitEnvironment()
	if err := cmd.Run(); err == nil {
		t.Fatal("committed into inherited repository")
	}
}

func TestFolderIsTheUsersChoice(t *testing.T) {
	o := fixture(t)
	o.Dir = ""
	if _, err := Run(context.Background(), o); err == nil || !strings.Contains(err.Error(), "choose a folder") {
		t.Fatalf("Run without a folder: %v", err)
	}
}

func TestCheckFolder(t *testing.T) {
	o := fixture(t)
	home := filepath.Dir(o.Dir)
	if _, err := CheckFolder(filepath.Join(home, "new", "wiki"), o.Dirs); err != nil {
		t.Fatalf("new folder: %v", err)
	}
	empty := filepath.Join(home, "empty")
	if err := os.Mkdir(empty, 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := CheckFolder(empty, o.Dirs); err != nil {
		t.Fatalf("empty folder: %v", err)
	}
	if err := os.WriteFile(filepath.Join(empty, "notes.txt"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := CheckFolder(empty, o.Dirs); err == nil || !strings.Contains(err.Error(), "not empty") {
		t.Fatalf("non-empty folder: %v", err)
	}
	if _, err := CheckFolder(filepath.Join(empty, "notes.txt"), o.Dirs); err == nil || !strings.Contains(err.Error(), "file") {
		t.Fatalf("file: %v", err)
	}
	for _, protected := range []string{"Documents", "Desktop", filepath.Join("Library", "Mobile Documents")} {
		if _, err := CheckFolder(filepath.Join(home, protected, "wiki"), o.Dirs); err == nil || !strings.Contains(err.Error(), "protects") {
			t.Fatalf("%s: %v", protected, err)
		}
	}
	if _, err := CheckFolder(home, o.Dirs); err == nil {
		t.Fatal("the home folder itself was accepted")
	}
	clone := filepath.Join(home, "clone")
	testGit(t, home, "clone", "--quiet", o.Remote, clone)
	if _, err := CheckFolder(clone, o.Dirs); err != nil {
		t.Fatalf("existing clone: %v", err)
	}
	if _, err := CheckRemote("git@github.com:me/eva-wiki.git"); err != nil {
		t.Fatalf("ssh remote: %v", err)
	}
	if _, err := CheckRemote("not a remote"); err == nil {
		t.Fatal("bad remote accepted")
	}
}
