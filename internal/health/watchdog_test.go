package health

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/heyNag/eva-md/internal/config"
	"github.com/heyNag/eva-md/internal/paths"
)

func saveBeat(t *testing.T, d paths.Dirs, host, event string, at time.Time) {
	t.Helper()
	raw, _ := json.Marshal(beat{Host: host, Event: event, At: at})
	put(t, filepath.Join(d.State, "heartbeats", host, event+".json"), raw)
}
func hostActivity(t *testing.T, d paths.Dirs, host string, at time.Time) {
	t.Helper()
	market := "codex"
	hostDir := filepath.Join(os.Getenv("HOME"), ".codex", "sessions", "2026", "10", "29")
	if host == "claude-code" {
		market = "claude"
		hostDir = filepath.Join(os.Getenv("HOME"), ".claude", "projects", "project-a")
	}
	marker := filepath.Join(d.Share, "marketplace", market, ".eva-md-managed")
	put(t, marker, []byte("eva-md marketplace v1\n"))
	if e := os.Chtimes(marker, at.Add(-12*time.Hour), at.Add(-12*time.Hour)); e != nil {
		t.Fatal(e)
	}
	put(t, filepath.Join(hostDir, "test.jsonl"), []byte("transcript content must not be read"))
	if e := os.Chmod(filepath.Join(hostDir, "test.jsonl"), 0000); e != nil {
		t.Fatal(e)
	}
	if e := os.Chtimes(hostDir, at, at); e != nil {
		t.Fatal(e)
	}
}
func TestRecentActivityRequiresNewestHeartbeatOfAnyEvent(t *testing.T) {
	d := isolated(t)
	now := time.Now()
	hostActivity(t, d, "claude-code", now.Add(-2*time.Minute))
	saveBeat(t, d, "claude-code", "SessionStart", now.Add(-time.Hour))
	c := captureFreshness(context.Background(), d, "claude-code", now)
	if c.Status != "FAIL" {
		t.Fatalf("missed activity not detected: %+v", c)
	}
	saveBeat(t, d, "claude-code", "PostToolUse", now.Add(-time.Minute))
	c = captureFreshness(context.Background(), d, "claude-code", now)
	if c.Status != "PASS" {
		t.Fatalf("long active chat incorrectly warned: %+v", c)
	}
}
func TestIdleOrUnavailableHostDoesNotTriggerWarning(t *testing.T) {
	d := isolated(t)
	now := time.Now()
	hostActivity(t, d, "codex", now.Add(-48*time.Hour))
	saveBeat(t, d, "codex", "Stop", now.Add(-72*time.Hour))
	c := captureFreshness(context.Background(), d, "codex", now)
	if c.Status != "UNKNOWN" {
		t.Fatalf("idle host should be unknown: %+v", c)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if c = captureFreshness(ctx, d, "codex", now); c.Status != "UNKNOWN" {
		t.Fatal(c)
	}
}
func TestQuickNoticeChecksOnlyOtherHost(t *testing.T) {
	d := isolated(t)
	now := time.Now()
	hostActivity(t, d, "claude-code", now.Add(-time.Minute))
	saveBeat(t, d, "claude-code", "Stop", now.Add(-time.Hour))
	if got := QuickNotice(context.Background(), d, config.Default(), "codex"); !strings.Contains(got, "claude-code activity") {
		t.Fatal(got)
	}
	if got := QuickNotice(context.Background(), d, config.Default(), "claude-code"); got != "" {
		t.Fatalf("warned about currently firing host: %s", got)
	}
}
func TestTrustNoticeRejectsSpoofedHeartbeatAndFlag(t *testing.T) {
	for _, scenario := range []string{"wrong-host", "wrong-event", "future", "bad-flag", "symlink-flag"} {
		t.Run(scenario, func(t *testing.T) {
			d := isolated(t)
			now := time.Now()
			flag := filepath.Join(d.State, "needs-trust.json")
			put(t, flag, []byte("{\"host\":\"codex\",\"needs_trust\":true}"))
			b := beat{Host: "codex", Event: "SessionStart", At: now.Add(time.Second)}
			switch scenario {
			case "wrong-host":
				b.Host = "claude-code"
			case "wrong-event":
				b.Event = "Stop"
			case "future":
				b.At = now.Add(time.Hour)
			case "bad-flag":
				put(t, flag, []byte("{}"))
			case "symlink-flag":
				other := filepath.Join(t.TempDir(), "flag")
				put(t, other, []byte("{\"host\":\"codex\",\"needs_trust\":true}"))
				if e := os.Remove(flag); e != nil {
					t.Fatal(e)
				}
				if e := os.Symlink(other, flag); e != nil {
					t.Fatal(e)
				}
			}
			raw, _ := json.Marshal(b)
			put(t, filepath.Join(d.State, "heartbeats", "codex", "SessionStart.json"), raw)
			if got := QuickNotice(context.Background(), d, config.Default()); got == "" {
				t.Fatal("spoof suppressed trust notice")
			}
		})
	}
}
func TestInboxFIFOIsUnsafe(t *testing.T) {
	d := isolated(t)
	if e := paths.EnsureDir(d.Inbox()); e != nil {
		t.Fatal(e)
	}
	if e := syscall.Mkfifo(filepath.Join(d.Inbox(), "fifo"), 0600); e != nil {
		t.Fatal(e)
	}
	var r Report
	inspectInbox(context.Background(), &r, d)
	if !r.Failed() {
		t.Fatalf("FIFO reported healthy: %+v", r)
	}
}
func TestUnknownModuleAndIncompleteTemporaryScan(t *testing.T) {
	d := isolated(t)
	c := config.Default()
	c.WikiPath = filepath.Join(t.TempDir(), "wiki")
	c.Instance = "test"
	put(t, filepath.Join(c.WikiPath, "index.md"), []byte("---\nokf_version: \"0.2\"\n---\n# Wiki\n"))
	if e := config.SaveSchema(filepath.Join(c.WikiPath, ".eva-md", "schema.toml"), config.WikiSchema{SchemaVersion: 1, OKFSpec: "0.2", OKFModule: "v0.7.2", Instance: "test", Created: "2026-10-29"}); e != nil {
		t.Fatal(e)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var r Report
	inspectWiki(ctx, &r, d, c, "unknown")
	foundPin, foundScan := false, false
	for _, check := range r.Checks {
		if check.Name == "okf module" {
			foundPin = true
			if check.Status != "UNKNOWN" {
				t.Fatal(check)
			}
		}
		if check.Name == "wiki temporary files" {
			foundScan = true
			if check.Status != "UNKNOWN" {
				t.Fatal(check)
			}
		}
	}
	if !foundPin || !foundScan {
		t.Fatalf("missing expected checks: %+v", r)
	}
}
