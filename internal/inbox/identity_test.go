package inbox

import (
	"strings"
	"testing"

	"github.com/heyNag/eva-md/internal/redact"
)

// Project keys made from folder or repo names that contain words such as
// token or secret are eva-md's own identifiers, not secrets. They must be
// accepted, or nothing would ever be stored for such a project.
func TestIdentityKeysWithSecretWords(t *testing.T) {
	s := fixture(t)
	for _, key := range []string{
		"local-token-refresh-worker-c9a8c7",
		"local-secret-sharing-prototype-e82b2c",
		"local-jwt-token-validator-service-c9a8c7",
		"local-password-generator-extension-e82b2c",
		"github.com__acme__k8s-secret-rotation-operator-v1",
	} {
		e := entry()
		e.Project = key
		e.Text = "captured"
		if err := s.Append(e); err != nil {
			t.Fatalf("project %q rejected: %v", key, err)
		}
		rows, _, err := s.ReadFrom(s.Path(key, e.Host, e.Session), 0)
		if err != nil || len(rows) != 1 || rows[0].Project != key || rows[0].Text != e.Text {
			t.Fatalf("project %q: rows=%#v err=%v", key, rows, err)
		}
	}
}

// Identity fields that hold a secret in a known format are still refused,
// because they also become file names.
func TestIdentitySecretsStillRejected(t *testing.T) {
	s := fixture(t)
	token := strings.Join([]string{"gh", "p_", strings.Repeat("ab12", 10)}, "")
	for _, mutate := range []func(*Entry){
		func(e *Entry) { e.Session = token },
		func(e *Entry) { e.Project = "local-" + token },
		func(e *Entry) { e.Host = "codex-" + token },
		func(e *Entry) { e.Session = "session-" + strings.Repeat("x", 520) },
	} {
		e := entry()
		mutate(&e)
		if err := s.Append(e); err == nil {
			t.Fatalf("unsafe identity accepted: %q %q %q", e.Host, e.Session, e.Project)
		}
	}
}

// The branch groups entries into threads, so ordinary branch names that
// share a secret word must stay whole and distinct.
func TestBranchNamesStayDistinct(t *testing.T) {
	s := fixture(t)
	branches := []string{"feature/token-refresh-for-oauth2-clients", "feature/token-expiry-in-cli-v2-parser", "fix/password-reset-flow"}
	for _, branch := range branches {
		e := entry()
		e.Branch = branch
		if err := s.Append(e); err != nil {
			t.Fatal(err)
		}
	}
	e := entry()
	rows, _, err := s.ReadFrom(s.Path(e.Project, e.Host, e.Session), 0)
	if err != nil || len(rows) != len(branches) {
		t.Fatalf("rows=%d err=%v", len(rows), err)
	}
	for i, row := range rows {
		if row.Branch != branches[i] || row.Red != 0 {
			t.Errorf("branch %q stored as %q (red %d)", branches[i], row.Branch, row.Red)
		}
	}
	// Control characters are removed and secrets in a known format are still
	// replaced, the same treatment the hook gives its thread key.
	token := strings.Join([]string{"gh", "p_", strings.Repeat("ab12", 10)}, "")
	e.Branch = "fix/\x07" + token[:12] + "\n" + token[12:]
	if err := s.Append(e); err != nil {
		t.Fatal(err)
	}
	rows, _, err = s.ReadFrom(s.Path(e.Project, e.Host, e.Session), 0)
	if err != nil || len(rows) != len(branches)+1 {
		t.Fatalf("rows=%d err=%v", len(rows), err)
	}
	want, _ := redact.Identifier(e.Branch, maxBranch)
	if got := rows[len(rows)-1]; got.Branch != want || !strings.HasSuffix(got.Branch, "-fix/[REDACTED:github_token]") || got.Red != 1 {
		t.Fatalf("branch stored as %q (red %d), want %q", got.Branch, got.Red, want)
	}
}
