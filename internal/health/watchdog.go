package health

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/heyNag/eva-md/internal/catchup"
	"github.com/heyNag/eva-md/internal/paths"
)

func hostEvents(host string) []string {
	events := []string{"SessionStart", "UserPromptSubmit", "PostToolUse", "Stop", "PreCompact", "SessionEnd"}
	if host == "claude-code" {
		return append(events, "PostToolUseFailure", "StopFailure", "PostCompact", "PostModelSwitch")
	}
	return append(events, "Interrupt")
}

// The hook failure check: of a host's last hookWindow runs in hookDays, half
// or more failing is a FAIL and a start-of-chat notice, once there are
// hookMinRuns runs to judge.
const (
	hookWindow  = 20
	hookMinRuns = 4
	hookDays    = 7 * 24 * time.Hour
)

// hooksFailing reports whether failed of a host's last runs is half or
// more, with runs enough to judge.
func hooksFailing(failed, runs int) bool { return runs >= hookMinRuns && failed*2 >= runs }

// hookRuns counts a host's last hookWindow runs within hookDays, newest
// first across its events, and how many of them failed, from the history
// each event's heartbeat keeps. A run fails when it runs out of time, panics
// or can't store its event.
func hookRuns(d paths.Dirs, host string, now time.Time) (failed, runs int) {
	var all []hookRun
	for _, event := range hostEvents(host) {
		b, valid := readBeat(d, host, event, now)
		if !valid {
			continue
		}
		for _, r := range b.Runs {
			if !r.At.IsZero() && !r.At.After(now.Add(time.Minute)) && now.Sub(r.At) <= hookDays {
				all = append(all, r)
			}
		}
	}
	slices.SortFunc(all, func(a, b hookRun) int { return b.At.Compare(a.At) })
	all = all[:min(len(all), hookWindow)]
	for _, r := range all {
		if r.Failed {
			failed++
		}
	}
	return failed, len(all)
}

func hookCheck(d paths.Dirs, host string, now time.Time) Check {
	check := Check{Name: host + " hook failures", Status: "UNKNOWN", Detail: "no hook run recorded in the past 7 days"}
	failed, runs := hookRuns(d, host, now)
	switch {
	case runs == 0:
	case hooksFailing(failed, runs):
		check.Status = "FAIL"
		check.Detail = fmt.Sprintf("%d of the last %d runs failed: they ran out of time or could not store their event", failed, runs)
		check.Fix = "check that the disk is not full, that eva-md's state folder is private and writable, and that git runs from the hook's PATH, then start a new chat"
	case runs < hookMinRuns && failed > 0:
		check.Detail = fmt.Sprintf("%d of the last %d runs failed, too few runs to judge", failed, runs)
	default:
		check.Status = "PASS"
		check.Detail = fmt.Sprintf("%d of the last %d runs failed", failed, runs)
	}
	return check
}

// driftCheck reports the host's transcript-format drift signal, which
// transcript recovery records (see catchup.Drift).
func driftCheck(d paths.Dirs, host string) Check {
	check := Check{Name: host + " transcript format", Status: "PASS", Detail: "transcript recovery has recorded no format drift"}
	drift, found, err := catchup.ReadDrift(d, host)
	switch {
	case err != nil:
		check.Status = "UNKNOWN"
		check.Detail = "the drift record is unreadable"
		check.Fix = "delete catchup/drift-" + host + ".json in eva-md's state folder; recovery records it again if the drift goes on"
	case found:
		check.Status = "FAIL"
		check.Detail = fmt.Sprintf("since %s, recovery recognized none of the rows it read of %s over %d turns that ended with a reply (%d KB); replies the hooks miss are not recovered", when(drift.At), sessionRefs(host, []string{drift.Session}), drift.Turns, drift.Bytes>>10)
		check.Fix = "update eva-md to a release that reads this " + cli(host) + " version's transcripts; the hooks still capture what they receive"
	}
	return check
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
	if !Installed(d, host) {
		check.Detail = "no owned plugin installation to correlate"
		return check
	}
	info, err := os.Lstat(filepath.Join(d.Share, "marketplace", cli(host), ".eva-md-managed"))
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
