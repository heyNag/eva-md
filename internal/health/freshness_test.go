package health

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/heyNag/eva-md/internal/config"
	"github.com/heyNag/eva-md/internal/paths"
)

// installedFor marks eva-md's plugin as installed for host, as install's
// marker does, half a day before now, so the freshness check correlates.
func installedFor(t *testing.T, d paths.Dirs, host string, now time.Time) {
	t.Helper()
	marker := filepath.Join(d.Share, "marketplace", cli(host), ".eva-md-managed")
	put(t, marker, []byte("eva-md marketplace v1\n"))
	if err := os.Chtimes(marker, now.Add(-12*time.Hour), now.Add(-12*time.Hour)); err != nil {
		t.Fatal(err)
	}
}

// plant writes a file of a host's store, its folders made as needed. A
// store's files need no fsync, which hundreds of them would make slow.
func plant(t *testing.T, file string, data []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(file), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, data, 0o600); err != nil {
		t.Fatal(err)
	}
}

// chatFolder makes folder hold a chat, an unreadable .jsonl file the check
// must never open, and sets the folder's modification time to mod.
func chatFolder(t *testing.T, folder string, mod time.Time) {
	t.Helper()
	file := filepath.Join(folder, "chat.jsonl")
	plant(t, file, []byte("transcript content must not be read"))
	if err := os.Chmod(file, 0o000); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(folder, mod, mod); err != nil {
		t.Fatal(err)
	}
}

// shut makes folder unreadable, so a scan that lists it can't tell what it
// holds and reports itself incomplete; its own modification time can still
// be read.
func shut(t *testing.T, folder string) {
	t.Helper()
	mod := mustStat(t, folder).ModTime()
	if err := os.Chmod(folder, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(folder, 0o700) })
	if err := os.Chtimes(folder, mod, mod); err != nil {
		t.Fatal(err)
	}
}

func mustStat(t *testing.T, path string) os.FileInfo {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return info
}

// codexDay is the date folder Codex keeps the chats of at's day in.
func codexDay(at time.Time) string {
	return filepath.Join(os.Getenv("HOME"), ".codex", "sessions", at.Format("2006"), at.Format("01"), at.Format("02"))
}

// A large store no longer leaves the check UNKNOWN: in a Claude store of
// 600 project folders and a Codex store of two years of date folders, more
// folders and entries than the old scan's bounds, a chat that came after
// the latest heartbeat is found. The Claude scan stats each project folder
// and reads only those changed in the past day, and the Codex scan reads
// only the two newest date folders, so the older ones, made unreadable
// here, are never read, not even an old day's folder changed a minute ago.
func TestCaptureFreshnessReadsLargeStores(t *testing.T) {
	now := time.Now()
	t.Run("claude-code", func(t *testing.T) {
		d := isolated(t)
		installedFor(t, d, "claude-code", now)
		saveBeat(t, d, "claude-code", "Stop", now.Add(-time.Hour))
		projects := filepath.Join(os.Getenv("HOME"), ".claude", "projects")
		for i := 0; i < 600; i++ {
			folder := filepath.Join(projects, fmt.Sprintf("-Users-me-project-%03d", i))
			plant(t, filepath.Join(folder, "memory", "MEMORY.md"), []byte("notes"))
			chatFolder(t, folder, now.Add(-time.Duration(48+i)*time.Hour))
			if i%2 == 0 {
				shut(t, folder)
			}
		}
		recent := filepath.Join(projects, "-Users-me-recent")
		chatFolder(t, recent, now.Add(-2*time.Minute))
		if c := captureFreshness(context.Background(), d, "claude-code", now); c.Status != "FAIL" {
			t.Fatalf("a chat after the latest heartbeat in a large store: %+v", c)
		}
		saveBeat(t, d, "claude-code", "PostToolUse", now.Add(-time.Minute))
		if c := captureFreshness(context.Background(), d, "claude-code", now); c.Status != "PASS" {
			t.Fatalf("a heartbeat after the chat: %+v", c)
		}
		// A folder changed in the past day that holds no chat, such as one
		// with only memory notes, is no activity.
		saveBeat(t, d, "claude-code", "PostToolUse", now.Add(-time.Hour))
		if err := os.Chmod(filepath.Join(recent, "chat.jsonl"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Remove(filepath.Join(recent, "chat.jsonl")); err != nil {
			t.Fatal(err)
		}
		plant(t, filepath.Join(recent, "memory", "MEMORY.md"), []byte("notes"))
		if err := os.Chtimes(recent, now.Add(-2*time.Minute), now.Add(-2*time.Minute)); err != nil {
			t.Fatal(err)
		}
		if c := captureFreshness(context.Background(), d, "claude-code", now); c.Status != "PASS" {
			t.Fatalf("a folder with no chat counted as activity: %+v", c)
		}
	})
	t.Run("codex", func(t *testing.T) {
		d := isolated(t)
		installedFor(t, d, "codex", now)
		saveBeat(t, d, "codex", "Stop", now.Add(-time.Hour))
		for i := 2; i < 730; i++ {
			day := now.AddDate(0, 0, -i)
			mod := now.Add(-time.Duration(i) * 24 * time.Hour)
			if i == 100 {
				mod = now.Add(-time.Minute)
			}
			chatFolder(t, codexDay(day), mod)
			if i > 3 {
				shut(t, codexDay(day))
			}
		}
		chatFolder(t, codexDay(now.AddDate(0, 0, -1)), now.Add(-20*time.Hour))
		chatFolder(t, codexDay(now), now.Add(-2*time.Minute))
		if c := captureFreshness(context.Background(), d, "codex", now); c.Status != "FAIL" {
			t.Fatalf("a chat after the latest heartbeat in a large store: %+v", c)
		}
		saveBeat(t, d, "codex", "UserPromptSubmit", now.Add(-time.Minute))
		if c := captureFreshness(context.Background(), d, "codex", now); c.Status != "PASS" {
			t.Fatalf("a heartbeat after the chat: %+v", c)
		}
	})
}

// Of Codex's date folders, the two newest by name are read: a chat that
// appeared in yesterday's folder after today's last one, as one started
// just before midnight can, is found there. A folder named for a date past
// tomorrow, as a clock set wrong can leave, is passed over, so it can't
// hide the newest real ones, and so are names that aren't dates.
func TestCaptureFreshnessReadsCodexsNewestDays(t *testing.T) {
	now := time.Now()
	sessions := func() string { return filepath.Join(os.Getenv("HOME"), ".codex", "sessions") }
	for _, tc := range []struct {
		name   string
		plant  func(t *testing.T)
		status string
	}{
		{"yesterday's chat newer than today's", func(t *testing.T) {
			chatFolder(t, codexDay(now), now.Add(-3*time.Hour))
			chatFolder(t, codexDay(now.AddDate(0, 0, -1)), now.Add(-2*time.Minute))
		}, "FAIL"},
		{"a folder dated past tomorrow", func(t *testing.T) {
			chatFolder(t, codexDay(now.AddDate(0, 3, 0)), now.Add(-72*time.Hour))
			chatFolder(t, codexDay(now), now.Add(-3*time.Hour))
			chatFolder(t, codexDay(now.AddDate(0, 0, -1)), now.Add(-2*time.Minute))
		}, "FAIL"},
		{"names that aren't dates", func(t *testing.T) {
			chatFolder(t, codexDay(now), now.Add(-3*time.Hour))
			chatFolder(t, codexDay(now.AddDate(0, 0, -1)), now.Add(-20*time.Hour))
			// Each sorts after today's folder and before tomorrow's date.
			for _, name := range []string{now.Format("02") + "x", now.Format("02") + "1"} {
				chatFolder(t, filepath.Join(filepath.Dir(codexDay(now)), name), now.Add(-2*time.Minute))
			}
			chatFolder(t, filepath.Join(sessions(), "archive"), now.Add(-2*time.Minute))
			chatFolder(t, filepath.Join(sessions(), now.Format("2006"), "notes"), now.Add(-2*time.Minute))
		}, "PASS"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := isolated(t)
			installedFor(t, d, "codex", now)
			saveBeat(t, d, "codex", "Stop", now.Add(-time.Hour))
			tc.plant(t)
			if c := captureFreshness(context.Background(), d, "codex", now); c.Status != tc.status {
				t.Fatalf("check=%+v", c)
			}
		})
	}
}

// A run in a folder eva-md doesn't capture leaves a stamp with its time, and
// the check counts it as a hook of the host: a chat there, after the latest
// heartbeat, raises no false FAIL or notice while the stamp is within 5
// minutes of it. A stamp from the future, or one that can't be read, counts
// for nothing, and the hook-failure count never reads the stamp.
func TestCaptureFreshnessCountsTheUncapturedStamp(t *testing.T) {
	now := time.Now()
	stamp := func(t *testing.T, d paths.Dirs, data string) {
		t.Helper()
		put(t, filepath.Join(d.State, "heartbeats", "claude-code", "uncaptured.json"), []byte(data))
	}
	at := func(when time.Time) string { return `{"at":"` + when.Format(time.RFC3339Nano) + `"}` }
	for _, tc := range []struct {
		name, stamp, status string
	}{
		{"no stamp", "", "FAIL"},
		{"stamp after the chat", at(now.Add(-time.Minute)), "PASS"},
		// As the hook writes it, in UTC at one width; and written over a
		// longer stamp, as an older build or a hand edit leaves one, whose
		// tail stays after it.
		{"stamp of one width", `{"at":"` + now.Add(-time.Minute).UTC().Format("2006-01-02T15:04:05.000000000Z07:00") + `"}`, "PASS"},
		{"stamp with a longer one's tail", at(now.Add(-time.Minute).UTC()) + `:00"}`, "PASS"},
		{"stamp within 5 minutes before the chat", at(now.Add(-6 * time.Minute)), "PASS"},
		{"stamp long before the chat", at(now.Add(-30 * time.Minute)), "FAIL"},
		{"stamp from the future", at(now.Add(time.Hour)), "FAIL"},
		{"stamp that isn't one", `{"at":"yesterday"}`, "FAIL"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := isolated(t)
			hostActivity(t, d, "claude-code", now.Add(-2*time.Minute))
			saveBeat(t, d, "claude-code", "Stop", now.Add(-time.Hour))
			if tc.stamp != "" {
				stamp(t, d, tc.stamp)
			}
			if c := captureFreshness(context.Background(), d, "claude-code", now); c.Status != tc.status {
				t.Fatalf("check=%+v", c)
			}
			notice := QuickNotice(context.Background(), d, config.Default(), "codex")
			if strings.Contains(notice, "claude-code activity") != (tc.status == "FAIL") {
				t.Fatalf("notice=%q", notice)
			}
			if c := hookCheck(d, "claude-code", now); c.Status != "UNKNOWN" || c.Detail != "no hook run recorded in the past 7 days" {
				t.Fatalf("the stamp reached the hook-failure count: %+v", c)
			}
		})
	}
}

// beatWith writes an event's heartbeat as the hook writes it, with whether
// its run named a transcript and whether it lay elsewhere.
func beatWith(t *testing.T, d paths.Dirs, host, event string, at time.Time, transcript, elsewhere bool) {
	t.Helper()
	raw, _ := json.Marshal(map[string]any{"v": 1, "at": at.Format(time.RFC3339Nano), "host": host, "event": event, "transcript_available": transcript, "transcript_elsewhere": elsewhere,
		"runs": []map[string]any{{"at": at.Format(time.RFC3339Nano)}}})
	put(t, filepath.Join(d.State, "heartbeats", host, event+".json"), raw)
}

// When the host's newest run that named a transcript found it outside the
// folder transcript recovery reads, as CLAUDE_CONFIG_DIR or CODEX_HOME
// leaves it, the transcript format row is UNKNOWN and says recovery is off
// and why, and the freshness row says it can't see that folder. A newer
// run in the host's own folder, or one that named no transcript, decide
// nothing more; drift still FAILs first.
func TestDoctorSaysWhenTheHostsFolderIsMoved(t *testing.T) {
	now := time.Now()
	for _, host := range []string{"claude-code", "codex"} {
		t.Run(host, func(t *testing.T) {
			d := isolated(t)
			installedFor(t, d, host, now)
			beatWith(t, d, host, "UserPromptSubmit", now.Add(-time.Hour), true, false)
			beatWith(t, d, host, "Stop", now.Add(-30*time.Minute), true, true)
			c := driftCheck(d, host)
			variable := map[string]string{"claude-code": "CLAUDE_CONFIG_DIR", "codex": "CODEX_HOME"}[host]
			folder := map[string]string{"claude-code": "~/.claude/projects", "codex": "~/.codex/sessions"}[host]
			if c.Status != "UNKNOWN" || !strings.HasPrefix(c.Detail, "transcript recovery is off: ") || !strings.Contains(c.Detail, folder) || !strings.Contains(c.Detail, variable) {
				t.Fatalf("drift row=%+v", c)
			}
			if f := captureFreshness(context.Background(), d, host, now); !strings.HasSuffix(f.Detail, "; the host's newest chat keeps its transcript outside "+folder+", the folder this check reads") {
				t.Fatalf("freshness row=%+v", f)
			}
			// A newer run that named no transcript says nothing of where
			// the host keeps them.
			beatWith(t, d, host, "SessionEnd", now.Add(-10*time.Minute), false, false)
			if c := driftCheck(d, host); c.Status != "UNKNOWN" {
				t.Fatalf("a run with no transcript cleared it: %+v", c)
			}
			beatWith(t, d, host, "PreCompact", now.Add(-5*time.Minute), true, false)
			if c := driftCheck(d, host); c.Status != "PASS" {
				t.Fatalf("a newer run in the host's own folder: %+v", c)
			}
			if f := captureFreshness(context.Background(), d, host, now); strings.Contains(f.Detail, "outside") {
				t.Fatalf("freshness row=%+v", f)
			}
		})
	}
}
