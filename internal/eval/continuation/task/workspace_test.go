package task

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestPrepareGivesTheSameCommitsWhateverGitTheCallerCarries(t *testing.T) {
	for _, name := range []string{"handoff-split", "hidden-parse", "control-max"} {
		t.Run(name, func(t *testing.T) {
			x := load(t, name)
			first := filepath.Join(tempDir(t), "ws")
			if err := Prepare(context.Background(), x, repos, first); err != nil {
				t.Fatal(err)
			}

			// The caller's git now carries configuration and an identity.
			t.Setenv("GIT_CONFIG_COUNT", "2")
			t.Setenv("GIT_CONFIG_KEY_0", "user.name")
			t.Setenv("GIT_CONFIG_VALUE_0", "Carried Name")
			t.Setenv("GIT_CONFIG_KEY_1", "core.autocrlf")
			t.Setenv("GIT_CONFIG_VALUE_1", "true")
			t.Setenv("GIT_CONFIG_PARAMETERS", "'user.email'='carried@example.invalid'")
			t.Setenv("GIT_AUTHOR_NAME", "Carried Author")
			t.Setenv("GIT_COMMITTER_NAME", "Carried Committer")
			t.Setenv("GIT_AUTHOR_DATE", "2030-05-05T05:05:05Z")
			t.Setenv("GIT_COMMITTER_DATE", "2030-05-05T05:05:05Z")
			second := filepath.Join(tempDir(t), "ws")
			if err := Prepare(context.Background(), x, repos, second); err != nil {
				t.Fatal(err)
			}
			for _, k := range []string{"GIT_CONFIG_COUNT", "GIT_CONFIG_PARAMETERS", "GIT_AUTHOR_NAME", "GIT_COMMITTER_NAME", "GIT_AUTHOR_DATE", "GIT_COMMITTER_DATE"} {
				os.Unsetenv(k)
			}

			for _, repo := range x.Repos {
				a, b := repoRoot(x, first, repo), repoRoot(x, second, repo)
				ida, idb := gitOut(t, a, "rev-parse", "HEAD"), gitOut(t, b, "rev-parse", "HEAD")
				if ida != idb {
					t.Fatalf("two builds gave commits %s and %s", ida, idb)
				}
				who := gitOut(t, a, "log", "-1", "--format=%an <%ae> %aI|%cn <%ce> %cI|%s")
				want := "Continuation Fixture <fixture@example.invalid> 2026-01-01T00:00:00Z|Continuation Fixture <fixture@example.invalid> 2026-01-01T00:00:00Z|Start"
				if !strings.EqualFold(strings.ReplaceAll(who, "+00:00", "Z"), want) {
					t.Fatalf("the commit is %q, want %q", who, want)
				}
				if branch := gitOut(t, a, "branch", "--show-current"); branch != "main" {
					t.Fatalf("the branch is %q, want main", branch)
				}
				if count := gitOut(t, a, "rev-list", "--count", "HEAD"); count != "1" {
					t.Fatalf("the repository holds %s commits, want 1", count)
				}
				if status := gitOut(t, a, "status", "--porcelain", "--untracked-files=all"); status != "" {
					t.Fatalf("the workspace has changes the commit lacks: %q", status)
				}
			}
		})
	}
}

func TestPrepareHoldsNoHiddenSolutionOrDecoyFile(t *testing.T) {
	cases := map[string][]string{
		"handoff-split": {"ledgerkit/go.mod", "ledgerkit/ledgerkit.go", "ledgerkit/ledgerkit_test.go", "ledgerkit/split.go"},
		"hidden-parse":  {"go.mod", "ledgerkit.go", "ledgerkit_test.go"},
		"control-max":   {"go.mod", "ledgerkit.go", "ledgerkit_test.go"},
	}
	for name, want := range cases {
		t.Run(name, func(t *testing.T) {
			x := load(t, name)
			ws := filepath.Join(tempDir(t), "ws")
			if err := Prepare(context.Background(), x, repos, ws); err != nil {
				t.Fatal(err)
			}
			if got := tree(t, ws); !slices.Equal(got, want) {
				t.Fatalf("the workspace holds %v, want %v", got, want)
			}
			// Files in git's own folder too: its index lists the same.
			root := repoRoot(x, ws, x.Repos[0])
			rel, err := filepath.Rel(ws, root)
			if err != nil {
				t.Fatal(err)
			}
			var tracked []string
			for _, f := range strings.Split(gitOut(t, root, "ls-files"), "\n") {
				tracked = append(tracked, filepath.ToSlash(filepath.Join(rel, f)))
			}
			if !slices.Equal(tracked, want) {
				t.Fatalf("the commit holds %v, want %v", tracked, want)
			}
		})
	}

	// The start overlay's file is the start state's, not the solution's.
	x := load(t, "handoff-split")
	ws := filepath.Join(tempDir(t), "ws")
	if err := Prepare(context.Background(), x, repos, ws); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(ws, "ledgerkit", "split.go"))
	if err != nil {
		t.Fatal(err)
	}
	want, err := os.ReadFile(filepath.Join(x.Dir, "start", "ledgerkit", "split.go"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(want) {
		t.Fatal("split.go is not the start overlay's")
	}
}

func TestPrepareTwoRepositoriesOutside(t *testing.T) {
	dir := tempDir(t)
	reposDir := filepath.Join(dir, "repos")
	write(t, reposDir, "ledgerkit/a.txt", "first\n")
	write(t, reposDir, "tallybook/sub/b.txt", "second\n")
	x := base(t)
	x.Workdir, x.Repos = WorkdirOutside, []string{"ledgerkit", "tallybook"}
	ws := filepath.Join(dir, "ws")
	if err := Prepare(context.Background(), x, reposDir, ws); err != nil {
		t.Fatal(err)
	}
	if got, want := tree(t, ws), []string{"ledgerkit/a.txt", "tallybook/sub/b.txt"}; !slices.Equal(got, want) {
		t.Fatalf("the workspace holds %v, want %v", got, want)
	}
	for _, r := range x.Repos {
		if top := gitOut(t, filepath.Join(ws, r), "rev-parse", "--show-toplevel"); top != filepath.Join(ws, r) {
			t.Fatalf("%s's repository is %s", r, top)
		}
	}
	if _, err := os.Stat(filepath.Join(ws, ".git")); err == nil {
		t.Fatal("the folder outside the repositories is a repository")
	}
}

func TestPrepareRefuses(t *testing.T) {
	x := load(t, "control-max")

	full := tempDir(t)
	write(t, full, "left.txt", "x\n")
	if err := Prepare(context.Background(), x, repos, full); err == nil {
		t.Fatal("Prepare built a workspace in a folder that was not empty")
	}

	linked := tempDir(t)
	if err := copyTree(repos, linked); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("/etc/hosts", filepath.Join(linked, "ledgerkit", "hosts")); err != nil {
		t.Fatal(err)
	}
	if err := Prepare(context.Background(), x, linked, filepath.Join(tempDir(t), "ws")); err == nil || !strings.Contains(err.Error(), "link") {
		t.Fatalf("Prepare copied a repository holding a link: %v", err)
	}

	withGit := tempDir(t)
	if err := copyTree(repos, withGit); err != nil {
		t.Fatal(err)
	}
	write(t, withGit, "ledgerkit/.git/HEAD", "ref: refs/heads/main\n")
	if err := Prepare(context.Background(), x, withGit, filepath.Join(tempDir(t), "ws")); err == nil {
		t.Fatal("Prepare copied a repository's own git folder")
	}
}
