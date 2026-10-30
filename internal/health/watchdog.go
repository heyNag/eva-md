package health

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/heyNag/eva-md/internal/paths"
)

func hostEvents(host string) []string {
	events := []string{"SessionStart", "UserPromptSubmit", "PostToolUse", "Stop", "PreCompact", "SessionEnd"}
	if host == "claude-code" {
		return append(events, "PostToolUseFailure", "StopFailure", "PostCompact", "PostModelSwitch")
	}
	return append(events, "Interrupt")
}

func readBeat(d paths.Dirs, host, event string, now time.Time) (beat, bool) {
	var b beat
	data, err := readPrivate(filepath.Join(d.State, "heartbeats", host, event+".json"), 16384)
	if err != nil || json.Unmarshal(data, &b) != nil || b.Host != host || b.Event != event || b.At.IsZero() || b.At.After(now.Add(time.Minute)) {
		return b, false
	}
	return b, true
}

// captureFreshness uses directory timestamps only. It never opens host
// transcripts and does not infer a gap from an idle host or a long active chat.
// New activity must postdate the latest hook of ANY event by over five minutes.
func captureFreshness(parent context.Context, d paths.Dirs, host string, now time.Time) Check {
	check := Check{Name: host + " capture freshness", Status: "UNKNOWN", Detail: "no recent correlated host activity"}
	marketHost := host
	if host == "claude-code" {
		marketHost = "claude"
	}
	marker := filepath.Join(d.Share, "marketplace", marketHost, ".eva-md-managed")
	data, err := readPrivate(marker, 1024)
	if err != nil || string(data) != "eva-md marketplace v1\n" {
		check.Detail = "no owned plugin installation to correlate"
		return check
	}
	info, err := os.Lstat(marker)
	if err != nil {
		return check
	}
	baseline := info.ModTime()
	latest := time.Time{}
	for _, event := range hostEvents(host) {
		if b, valid := readBeat(d, host, event, now); valid && b.At.After(latest) {
			latest = b.At
		}
	}
	if latest.After(baseline) {
		baseline = latest
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return check
	}
	hostDir := filepath.Join(home, ".claude", "projects")
	if host == "codex" {
		hostDir = filepath.Join(home, ".codex", "sessions")
	}
	ctx, cancel := context.WithTimeout(parent, 35*time.Millisecond)
	defer cancel()
	activity, complete := newestChatDirectory(ctx, hostDir, now)
	if !complete {
		check.Detail = "host activity scan unavailable or incomplete"
		return check
	}
	if !activity.IsZero() && now.Sub(activity) <= 24*time.Hour && activity.After(baseline.Add(5*time.Minute)) {
		check.Status = "FAIL"
		check.Detail = "recent chat-folder activity is newer than the latest capture heartbeat"
		check.Fix = "review host plugin enablement and hook trust, then start a new chat"
		return check
	}
	if !latest.IsZero() && now.Sub(latest) <= 24*time.Hour {
		check.Status = "PASS"
		check.Detail = "recent hook observed; no newer chat-folder gap detected"
	}
	return check
}

// Only directory names and modification times are inspected. A directory is
// considered chat activity when it contains a regular .jsonl file; no file is
// opened or its timestamp queried. Bounds protect hook latency on large stores.
func newestChatDirectory(ctx context.Context, dir string, now time.Time) (time.Time, bool) {
	root, err := paths.OpenRoot(dir, false)
	if err != nil {
		return time.Time{}, false
	}
	defer root.Close()
	type pending struct {
		name  string
		depth int
	}
	queue := []pending{{".", 0}}
	var newest time.Time
	directories, entries := 0, 0
	for len(queue) > 0 {
		if ctx.Err() != nil || directories >= 512 || entries >= 4096 {
			return newest, false
		}
		current := queue[0]
		queue = queue[1:]
		directories++
		f, err := root.Open(current.name)
		if err != nil {
			return newest, false
		}
		info, err := f.Stat()
		if err != nil || !info.IsDir() {
			f.Close()
			return newest, false
		}
		containsChat := false
		for {
			if ctx.Err() != nil || entries >= 4096 {
				f.Close()
				return newest, false
			}
			batch, e := f.ReadDir(min(128, 4096-entries))
			entries += len(batch)
			for _, entry := range batch {
				if entry.Type()&os.ModeSymlink != 0 {
					continue
				}
				if entry.IsDir() && current.depth < 4 {
					queue = append(queue, pending{filepath.Join(current.name, entry.Name()), current.depth + 1})
				}
				if entry.Type().IsRegular() && strings.HasSuffix(entry.Name(), ".jsonl") {
					containsChat = true
				}
			}
			if e == io.EOF {
				break
			}
			if e != nil {
				f.Close()
				return newest, false
			}
		}
		f.Close()
		if containsChat && !info.ModTime().After(now.Add(time.Minute)) && info.ModTime().After(newest) {
			newest = info.ModTime()
		}
	}
	return newest, true
}
