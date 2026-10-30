package hook

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/heyNag/eva-md/internal/session"
)

// A repo without a remote is keyed by its folder name. Names with words such
// as token, secret or password are ordinary names, and the inbox must accept
// every capture for such a project. The key ends with a hash of the folder's
// path, so a rule that judged the whole key as a secret would reject most of
// these names.
func TestSecretWordFoldersAreCaptured(t *testing.T) {
	for _, name := range []string{"token-refresh-worker", "secret-sharing-prototype", "jwt-token-validator-service", "password-generator-extension", "api-token-rotation-service", "password-vault-migration"} {
		t.Run(name, func(t *testing.T) {
			env, p := setup(t)
			p.Cwd = filepath.Join(os.Getenv("HOME"), name)
			if err := os.MkdirAll(p.Cwd, 0o700); err != nil {
				t.Fatal(err)
			}
			git(t, p.Cwd, "init", "--quiet")
			invoke(t, "claude-code", p, env)
			p.Event = "Stop"
			reply := "the final reply"
			p.Reply = &reply
			invoke(t, "claude-code", p, env)
			rows := records(t, env)
			if len(rows) != 2 || rows[0].Text != "please help" || rows[1].Text != reply {
				t.Fatalf("records=%#v", rows)
			}
			for _, row := range rows {
				if !strings.HasPrefix(row.Project, "local-"+name+"-") || strings.Contains(row.Project, "REDACTED") {
					t.Fatalf("project key=%q", row.Project)
				}
			}
		})
	}
}

// Branch names that share a secret word must not be redacted to the same
// text, which would merge their threads. Each branch keeps its own thread,
// also when a detached HEAD falls back to the session's thread marker.
func TestSecretWordBranchesStaySeparate(t *testing.T) {
	env, p := setup(t)
	git(t, p.Cwd, "init", "--quiet")
	git(t, p.Cwd, "-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid", "-c", "commit.gpgsign=false", "commit", "--allow-empty", "--quiet", "-m", "fixture")
	markers := session.Markers{Root: env.Dirs.Sessions()}
	branches := []string{"feature/token-refresh-for-oauth2-clients", "feature/token-expiry-in-cli-v2-parser"}
	sessions := []string{"session-refresh", "session-expiry"}
	prompts := []string{"first branch work", "second branch work"}
	for i, branch := range branches {
		git(t, p.Cwd, "checkout", "--quiet", "-b", branch, "HEAD")
		p.Session = sessions[i]
		p.Turn = "turn-" + sessions[i]
		p.Prompt = prompts[i]
		invoke(t, "codex", p, env)
		thread, ok, err := markers.Get("codex", p.Session, session.Thread)
		if err != nil || !ok || thread != branch {
			t.Fatalf("thread marker=%q %v %v, want %q", thread, ok, err, branch)
		}
	}
	git(t, p.Cwd, "checkout", "--quiet", "--detach", "HEAD")
	p.Turn = "turn-detached"
	p.Prompt = "detached work"
	invoke(t, "codex", p, env)
	got := map[string]string{}
	for _, row := range records(t, env) {
		got[row.Text] = row.Branch
	}
	want := map[string]string{prompts[0]: branches[0], prompts[1]: branches[1], "detached work": branches[1]}
	for text, branch := range want {
		if got[text] != branch {
			t.Errorf("%q stored on branch %q, want %q", text, got[text], branch)
		}
	}
	if len(got) != len(want) {
		t.Errorf("records=%v", got)
	}
}

// Provider token formats also match ordinary names. A folder named
// ops_runbooks_2026 or sk-learn-pipeline must still give a key the inbox
// accepts, and branches named feature/sk-learn-pipeline-... must keep
// separate threads.
func TestTokenLikeFoldersAndBranches(t *testing.T) {
	for _, name := range []string{"ops_runbooks_2026", "sk-learn-pipeline"} {
		t.Run(name, func(t *testing.T) {
			env, p := setup(t)
			p.Cwd = filepath.Join(os.Getenv("HOME"), name)
			if err := os.MkdirAll(p.Cwd, 0o700); err != nil {
				t.Fatal(err)
			}
			git(t, p.Cwd, "init", "--quiet")
			invoke(t, "claude-code", p, env)
			rows := records(t, env)
			if len(rows) != 1 || rows[0].Text != "please help" || strings.Contains(rows[0].Project, "REDACTED") {
				t.Fatalf("records=%#v", rows)
			}
		})
	}
	env, p := setup(t)
	git(t, p.Cwd, "init", "--quiet")
	git(t, p.Cwd, "-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid", "-c", "commit.gpgsign=false", "commit", "--allow-empty", "--quiet", "-m", "fixture")
	want := map[string]string{"upgrade work": "feature/sk-learn-pipeline-upgrade", "refactor work": "feature/sk-learn-pipeline-refactor"}
	for i, prompt := range []string{"upgrade work", "refactor work"} {
		git(t, p.Cwd, "checkout", "--quiet", "-b", want[prompt], "HEAD")
		p.Session = "session-" + string(rune('a'+i))
		p.Turn = "turn-" + p.Session
		p.Prompt = prompt
		invoke(t, "codex", p, env)
	}
	rows := records(t, env)
	if len(rows) != 2 {
		t.Fatalf("records=%#v", rows)
	}
	for _, row := range rows {
		if row.Branch != want[row.Text] {
			t.Errorf("%q stored on branch %q, want %q", row.Text, row.Branch, want[row.Text])
		}
	}
}

// Hooks take the project and branch from the per-worktree identity cache, and
// a branch switch between two events still reaches the inbox.
func TestCaptureUsesIdentityCache(t *testing.T) {
	env, p := setup(t)
	git(t, p.Cwd, "init", "--quiet", "-b", "main")
	git(t, p.Cwd, "-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid", "-c", "commit.gpgsign=false", "commit", "--allow-empty", "--quiet", "-m", "fixture")
	invoke(t, "codex", p, env)
	entries, err := os.ReadDir(filepath.Join(env.Dirs.Cache(), "identity"))
	if err != nil || len(entries) != 1 {
		t.Fatalf("identity cache %v, %v", entries, err)
	}
	git(t, p.Cwd, "checkout", "--quiet", "-b", "feature/cached")
	p.Turn = "turn-2"
	p.Prompt = "after the switch"
	invoke(t, "codex", p, env)
	rows := records(t, env)
	if len(rows) != 2 || rows[0].Branch != "main" || rows[1].Branch != "feature/cached" {
		t.Fatalf("branches=%#v", rows)
	}
}
