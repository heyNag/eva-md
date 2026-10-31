package wikigit

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/heyNag/eva-md/internal/okfadapter"
	"github.com/heyNag/eva-md/internal/redact"
)

func isolate(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(home, "gitconfig"))
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	return home
}

// Identifiers eva-md writes into pages, which redacting as text would
// change, don't count as secrets; secrets still do, identifier-shaped ones
// included.
func TestSecretLine(t *testing.T) {
	secret := "ghp_" + strings.Repeat("ab12", 10)
	identifiers := []string{
		`branch: "local-token-refresh-worker-c9a8c7"`,
		`title: "eva-md · local-token-refresh-worker-c9a8c7"`,
		"# eva-md · local-token-refresh-worker-c9a8c7",
		"* [local-token-refresh-worker-c9a8c7](threads/local-token-refresh-worker-c9a8c7.md) — Rotated the token. · last active 2026-10-31",
		"* [sk-learn-pipeline-refactor-version-two](threads/sk-learn-pipeline-refactor-version-two.md)",
		"- decision [stated]: Rotate the token on local-token-refresh-worker-c9a8c7 s:3f2a91c0#14",
	}
	// Branches with an issue key, an acronym or a part ending in digits, in
	// the frontmatter, the title and the area's overview.
	for _, branch := range []string{"PROJ-123-local-token-refresh-worker-c9a8c7f1e2d3b4a5", "ENG-1234-rotate-api-key-for-staging-cluster2", "AWS-secret-rotation-c9a8c7f1e2d3b4a5", "feature/API-token-cache-c9a8c7f1e2d3b4a5"} {
		identifiers = append(identifiers, `branch: "`+branch+`"`, "# eva-md · "+branch, "* ["+branch+"](threads/"+branch+".md) — Rotated the token. · last active 2026-10-31")
	}
	for i, line := range identifiers {
		if _, n := redact.Redact(line); n == 0 {
			t.Fatalf("identifier %d: redacting it as text replaces nothing, so the case proves nothing", i)
		}
		if secretLine(line) {
			t.Errorf("identifier %d counted as a secret", i)
		}
	}
	for i, line := range []string{"Plain text with a ref s:3f2a91c0#14", "- done [stated]: [REDACTED:secret_value] rotated", "\ufeffPaired on the release 👩\u200d💻"} {
		if secretLine(line) {
			t.Errorf("clean line %d counted as a secret", i)
		}
	}
	for i, line := range []string{
		"The CI token is " + secret,
		"api_key = " + secret,
		"password: correct-horse-battery-staple",
		"db_password = hunter2hunter2",
		"the token is Xy7AbC9dEf0GhI1jKl2MnO3pQ",
		"my secret key is k3j4-h5g6-f7d8-s9a0-q1w2",
		"-----BEGIN OPENSSH PRIVATE KEY-----",
	} {
		if !secretLine(line) {
			t.Errorf("secret %d missed", i)
		}
	}
}

// The diff reader judges only added lines, and counts a binary file or a
// line too long to judge as a secret.
func TestDiffScan(t *testing.T) {
	secret := "ghp_" + strings.Repeat("ab12", 10)
	header := "diff --git a/p.md b/p.md\nindex 1..2 100644\n--- a/p.md\n+++ b/p.md\n"
	for _, tc := range []struct {
		name, diff string
		secret     bool
	}{
		{"added secret", header + "@@ -1,0 +2 @@\n+token " + secret + "\n", true},
		{"removed secret", header + "@@ -2 +1,0 @@\n-token " + secret + "\n", false},
		{"clean", header + "@@ -1,0 +2 @@\n+Wrote the committer.\n", false},
		{"binary", "diff --git a/p.png b/p.png\nnew file mode 100644\nBinary files /dev/null and b/p.png differ\n", true},
		{"long line", header + "@@ -1,0 +2 @@\n+" + strings.Repeat("a", maxLine+10) + "\n", true},
	} {
		d := &diffScan{}
		// Write in small pieces, as a pipe may deliver it.
		var err error
		for rest := tc.diff; rest != "" && err == nil; {
			n := min(len(rest), 4096)
			_, err = d.Write([]byte(rest[:n]))
			rest = rest[n:]
		}
		if err != nil && !errors.Is(err, errSecret) {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if d.secret != tc.secret {
			t.Errorf("%s: secret %v, want %v", tc.name, d.secret, tc.secret)
		}
	}
}

func TestPushStatus(t *testing.T) {
	failed := errors.New("exit status 1")
	for _, tc := range []struct {
		out  string
		err  error
		want string
	}{
		{"To /r.git\n \tHEAD:refs/heads/main\t1a2b3c4..5d6e7f8\nDone\n", nil, PushOK},
		{"To /r.git\n*\tHEAD:refs/heads/main\t[new branch]\nDone\n", nil, PushOK},
		{"To /r.git\n=\tHEAD:refs/heads/main\t[up to date]\nDone\n", nil, PushUpToDate},
		{"To /r.git\n!\tHEAD:refs/heads/main\t[rejected] (fetch first)\nDone\n", failed, PushDiverged},
		{"To /r.git\n!\tHEAD:refs/heads/main\t[rejected] (non-fast-forward)\nDone\n", failed, PushDiverged},
		{"To /r.git\n!\tHEAD:refs/heads/main\t[remote rejected] (pre-receive hook declined)\nDone\n", failed, PushFailed},
		{"", failed, PushPending},
		{"", nil, PushPending},
	} {
		if got := pushStatus(tc.out, tc.err); got != tc.want {
			t.Errorf("pushStatus(%q) = %s, want %s", tc.out, got, tc.want)
		}
	}
}

// The commit's wiki lock is the lock okfadapter takes around every write: a
// write waits while a commit holds it.
func TestWikiLockIsTheAdapterLock(t *testing.T) {
	home := isolate(t)
	root, locks := filepath.Join(home, "wiki"), filepath.Join(home, "locks")
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "index.md"), []byte("# Wiki\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	w, err := okfadapter.Open(root, locks)
	if err != nil {
		t.Fatal(err)
	}
	unlock, _, err := wikiLock(context.Background(), locks)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- w.WriteFile("general/index.md", []byte("# General\n")) }()
	select {
	case err := <-done:
		t.Fatalf("a write ran while the commit held the wiki lock: %v", err)
	case <-time.After(300 * time.Millisecond):
	}
	unlock()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the write never got the lock")
	}
}
