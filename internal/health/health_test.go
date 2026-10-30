package health

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/heyNag/eva-md/internal/config"
	"github.com/heyNag/eva-md/internal/paths"
)

func isolated(t *testing.T) paths.Dirs {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(home, "gitconfig"))
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	d, e := paths.Default()
	if e != nil {
		t.Fatal(e)
	}
	return d
}
func put(t *testing.T, path string, data []byte) {
	t.Helper()
	if e := paths.EnsureDir(filepath.Dir(path)); e != nil {
		t.Fatal(e)
	}
	if e := paths.WriteFileAtomic(path, data, 0600); e != nil {
		t.Fatal(e)
	}
}
func TestMissingConfigurationIsNotHealthy(t *testing.T) {
	d := isolated(t)
	r := Inspect(context.Background(), d, "v0.7.2", false)
	if !r.Failed() {
		t.Fatal("missing config was reported healthy")
	}
	for _, c := range r.Checks {
		if strings.Contains(c.Name, "SessionStart") && c.Status != "UNKNOWN" {
			t.Fatal(c)
		}
	}
}
func TestQuickNoticeNeedsActualCaptureAfterInstall(t *testing.T) {
	d := isolated(t)
	flag := filepath.Join(d.State, "needs-trust.json")
	put(t, flag, []byte(`{"host":"codex","needs_trust":true}`))
	if QuickNotice(context.Background(), d, config.Default()) == "" {
		t.Fatal("missing trust notice")
	}
	info, _ := os.Stat(flag)
	b := beat{At: info.ModTime().Add(time.Second), Host: "codex", Event: "SessionStart"}
	raw, _ := json.Marshal(b)
	put(t, filepath.Join(d.State, "heartbeats", "codex", "SessionStart.json"), raw)
	if got := QuickNotice(context.Background(), d, config.Default()); got != "" {
		t.Fatal(got)
	}
}
func TestNoSymlinkHealthInput(t *testing.T) {
	d := isolated(t)
	secret := filepath.Join(t.TempDir(), "outside")
	put(t, secret, []byte("do not display"))
	if e := paths.EnsureDir(d.State); e != nil {
		t.Fatal(e)
	}
	link := filepath.Join(d.State, "alias")
	if e := os.Symlink(secret, link); e != nil {
		t.Fatal(e)
	}
	if _, e := readPrivate(link, 100); e == nil {
		t.Fatal("followed symlink")
	}
}
func TestNewerWikiNotice(t *testing.T) {
	d := isolated(t)
	c := config.Default()
	c.WikiPath = filepath.Join(t.TempDir(), "wiki")
	put(t, filepath.Join(c.WikiPath, "index.md"), []byte("# Wiki"))
	if e := config.SaveSchema(filepath.Join(c.WikiPath, ".eva-md", "schema.toml"), config.WikiSchema{SchemaVersion: 2, OKFSpec: "0.2", OKFModule: "v0.7.2", Instance: "test", Created: "2026-10-29"}); e != nil {
		t.Fatal(e)
	}
	if got := QuickNotice(context.Background(), d, c); !strings.Contains(got, "newer") {
		t.Fatal(got)
	}
}
func TestInboxPrivateAndUnsafeStates(t *testing.T) {
	d := isolated(t)
	put(t, filepath.Join(d.Inbox(), "general", "codex-test.jsonl"), []byte("{}\n"))
	var r Report
	inspectInbox(context.Background(), &r, d)
	if r.Checks[0].Status != "PASS" {
		t.Fatal(r)
	}
	if e := os.Chmod(filepath.Join(d.Inbox(), "general", "codex-test.jsonl"), 0644); e != nil {
		t.Fatal(e)
	}
	r = Report{}
	inspectInbox(context.Background(), &r, d)
	if !r.Failed() {
		t.Fatal("public inbox accepted")
	}
}
func TestUnknownConfigKeysAreReported(t *testing.T) {
	d := isolated(t)
	put(t, d.ConfigFile(), []byte("excludes = [\"~/client\"]\n"))
	r := Inspect(context.Background(), d, "v0.7.2", false)
	reported := false
	for _, c := range r.Checks {
		if c.Name == "config keys" && c.Status == "FAIL" && strings.Contains(c.Detail, "excludes") {
			reported = true
		}
	}
	if !reported {
		t.Fatalf("unknown key not reported: %+v", r.Checks)
	}
	c, err := config.Load(d.ConfigFile())
	if err != nil {
		t.Fatal(err)
	}
	if got := QuickNotice(context.Background(), d, c, "codex"); !strings.Contains(got, "unknown keys") {
		t.Fatalf("notice = %q", got)
	}
}
func TestFirstTrustedCodexChatHasNoTrustNotice(t *testing.T) {
	d := isolated(t)
	flag := filepath.Join(d.State, "needs-trust.json")
	put(t, flag, []byte(`{"host":"codex","needs_trust":true}`))
	info, _ := os.Stat(flag)
	// The last SessionStart heartbeat predates install. The running Codex
	// hook writes its own heartbeat only after the notice is built.
	raw, _ := json.Marshal(beat{At: info.ModTime().Add(-time.Hour), Host: "codex", Event: "SessionStart"})
	put(t, filepath.Join(d.State, "heartbeats", "codex", "SessionStart.json"), raw)
	if got := QuickNotice(context.Background(), d, config.Default(), "codex"); got != "" {
		t.Fatalf("running trusted Codex hook got: %s", got)
	}
	if got := QuickNotice(context.Background(), d, config.Default(), "claude-code"); !strings.Contains(got, "/hooks") {
		t.Fatalf("Claude chat lost the trust notice: %q", got)
	}
}
