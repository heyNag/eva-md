package health

import (
	"bytes"
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

func init() {
	registerRows(600, "hooks", func(r *Report, in *inspection) { inspectHooks(in.ctx, r, in.d, in.now()) })
}

// inspectHooks adds each host's rows of its hooks: each event's newest
// heartbeat, then the capture's freshness, the hooks' failures, transcript
// recovery's format drift and the hooks' times.
func inspectHooks(ctx context.Context, r *Report, d paths.Dirs, now time.Time) {
	for _, host := range []string{"claude-code", "codex"} {
		for _, event := range hostEvents(host) {
			b, valid := readBeat(d, host, event, now)
			if !valid {
				r.add(host+" "+event, "UNKNOWN", "no valid heartbeat observed", "exercise this event in an enabled, trusted session")
				continue
			}
			detail := "last observed " + b.At.Format(time.RFC3339)
			if !b.TranscriptAvailable {
				detail += "; transcript unavailable for catch-up"
			}
			if b.Entrypoint != "" {
				detail += "; entrypoint " + b.Entrypoint
				if b.Headless {
					detail += ", a program's chat: its prompts are stored as host prompts"
				}
			}
			status := "PASS"
			if now.Sub(b.At) > 24*time.Hour {
				status = "UNKNOWN"
				detail += "; historical evidence only"
			}
			r.add(host+" "+event, status, detail, "")
		}
		r.Checks = append(r.Checks, captureFreshness(ctx, d, host, now), hookCheck(d, host, now), driftCheck(d, host))
		r.Checks = append(r.Checks, hookTimes(d, host, now))
	}
}

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
// transcript recovery records (see catchup.Drift), and, as UNKNOWN, a host
// whose newest chat keeps its transcript where recovery doesn't read it
// (see hostBeats).
func driftCheck(d paths.Dirs, host string) Check {
	check := Check{Name: host + " transcript format", Status: "PASS", Detail: "transcript recovery has recorded no format drift"}
	drift, found, err := catchup.ReadDrift(d, host)
	_, moved := hostBeats(d, host, time.Now())
	switch {
	case err != nil:
		check.Status = "UNKNOWN"
		check.Detail = "the drift record is unreadable"
		check.Fix = "delete catchup/drift-" + host + ".json in eva-md's state folder; recovery records it again if the drift goes on"
	case found:
		check.Status = "FAIL"
		check.Detail = fmt.Sprintf("since %s, recovery recognized none of the rows it read of %s over %d turns that ended with a reply (%d KB); replies the hooks miss are not recovered", when(drift.At), sessionRefs(host, []string{drift.Session}), drift.Turns, drift.Bytes>>10)
		check.Fix = "update eva-md to a release that reads this " + cli(host) + " version's transcripts; the hooks still capture what they receive"
	case moved:
		check.Status = "UNKNOWN"
		check.Detail = "transcript recovery is off: the newest chat keeps its transcript outside " + store(host) + ", the only folder recovery reads, as when " + moves(host) + " moves the host's folder; replies the hooks miss are not recovered, though the hooks still capture what they receive"
	}
	return check
}

func readBeat(d paths.Dirs, host, event string, now time.Time) (beat, bool) {
	var b beat
	valid := decodeBeat(d, host, event, now, &b, &b)
	return b, valid
}

// decodeBeat decodes the heartbeat of host's event into v, whose head is
// the beat it holds, and reports whether the heartbeat is valid: the
// event's own, of a run that ended no later than a minute past now.
func decodeBeat(d paths.Dirs, host, event string, now time.Time, head *beat, v any) bool {
	data, err := readPrivate(filepath.Join(d.State, "heartbeats", host, event+".json"), 16384)
	return err == nil && json.Unmarshal(data, v) == nil && head.Host == host && head.Event == event && !head.At.IsZero() && !head.At.After(now.Add(time.Minute))
}

// activityTimeout bounds the scan of a host's chat folders, which runs in a
// hook's notice. Tests raise it before any test runs, so a slow machine
// can't fail a test of what the scan finds.
var activityTimeout = 35 * time.Millisecond

// The bounds of the scan beside its time: the entries it lists of a store's
// folders of projects or dates, and the entries of the recently changed
// folders it reads for a chat.
const (
	storeEntries  = 16384
	folderEntries = 4096
)

// captureFreshness uses directory timestamps only. It never opens host
// transcripts and does not infer a gap from an idle host or a long active chat.
// New activity must postdate the latest hook of ANY event by over five
// minutes; a run in a folder eva-md doesn't capture counts, by its stamp.
// It reads the default store only, and says so when the host keeps its
// chats elsewhere.
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
	latest, moved := hostBeats(d, host, now)
	// The host's hooks ran there, though they captured nothing.
	if at, ok := uncapturedAt(d, host, now); ok && at.After(latest) {
		latest = at
	}
	if latest.After(baseline) {
		baseline = latest
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return check
	}
	ctx, cancel := context.WithTimeout(parent, activityTimeout)
	defer cancel()
	var activity time.Time
	var complete bool
	if host == "codex" {
		activity, complete = newestCodexChat(ctx, filepath.Join(home, ".codex", "sessions"), now)
	} else {
		activity, complete = newestClaudeChat(ctx, filepath.Join(home, ".claude", "projects"), now)
	}
	switch {
	case !complete:
		check.Detail = "host activity scan unavailable or incomplete"
	case !activity.IsZero() && now.Sub(activity) <= 24*time.Hour && activity.After(baseline.Add(5*time.Minute)):
		check.Status = "FAIL"
		check.Detail = "recent chat-folder activity is newer than the latest capture heartbeat"
		check.Fix = "review host plugin enablement and hook trust, then start a new chat"
	case !latest.IsZero() && now.Sub(latest) <= 24*time.Hour:
		check.Status = "PASS"
		check.Detail = "recent hook observed; no newer chat-folder gap detected"
	}
	if moved {
		check.Detail += "; the host's newest chat keeps its transcript outside " + store(host) + ", the folder this check reads"
	}
	return check
}

// uncapturedAt reads the time the host's hooks last ran in a folder eva-md
// doesn't capture, which the hook stamps in
// <state>/heartbeats/<host>/uncaptured.json with the time alone, or false
// when there is no valid stamp. Only the first record counts: the hook
// writes each stamp over the last without truncating the file, so a stamp
// written over a longer one, as by an older build or a hand edit, leaves
// that one's tail after it.
func uncapturedAt(d paths.Dirs, host string, now time.Time) (time.Time, bool) {
	var stamp struct {
		At time.Time `json:"at"`
	}
	data, err := readPrivate(filepath.Join(d.State, "heartbeats", host, "uncaptured.json"), 1024)
	if err != nil || json.NewDecoder(bytes.NewReader(data)).Decode(&stamp) != nil || stamp.At.IsZero() || stamp.At.After(now.Add(time.Minute)) {
		return time.Time{}, false
	}
	return stamp.At, true
}

// newestClaudeChat returns when a chat last appeared in Claude Code's
// store, dir, which holds a folder per project: the modification time of
// the newest project folder changed in the past day that holds a chat, a
// regular .jsonl file. It lists the store and stats each project folder,
// and reads only those changed in the past day, newest first, until one
// holds a chat, so its cost grows with the projects, not the chats. Only
// names, types and modification times are read; no file is opened.
// complete is false when the store can't be read within the bounds.
func newestClaudeChat(ctx context.Context, dir string, now time.Time) (newest time.Time, complete bool) {
	root, err := paths.OpenRoot(dir, false)
	if err != nil {
		return time.Time{}, false
	}
	defer root.Close()
	f, err := root.Open(".")
	if err != nil {
		return time.Time{}, false
	}
	defer f.Close()
	type folder struct {
		name string
		mod  time.Time
	}
	var changed []folder
	for listed := 0; ; {
		if ctx.Err() != nil || listed >= storeEntries {
			return time.Time{}, false
		}
		batch, e := f.ReadDir(min(128, storeEntries-listed))
		listed += len(batch)
		for _, entry := range batch {
			if !entry.IsDir() {
				continue
			}
			info, err := root.Lstat(entry.Name())
			if err != nil || !info.IsDir() {
				continue
			}
			if mod := info.ModTime(); !mod.After(now.Add(time.Minute)) && now.Sub(mod) <= 24*time.Hour {
				changed = append(changed, folder{entry.Name(), mod})
			}
		}
		if e == io.EOF {
			break
		}
		if e != nil {
			return time.Time{}, false
		}
	}
	slices.SortFunc(changed, func(a, b folder) int { return b.mod.Compare(a.mod) })
	left := folderEntries
	for _, c := range changed {
		chat, ok := holdsChat(ctx, root, c.name, &left)
		if !ok {
			return time.Time{}, false
		}
		if chat {
			return c.mod, true
		}
	}
	return time.Time{}, true
}

// newestCodexChat returns when a chat last appeared in Codex's store, dir,
// which keeps a folder per day, <YYYY>/<MM>/<DD>: the newer modification
// time of the two newest day folders by name that hold a chat. A chat
// started in the past day is in one of them, the day before included for
// one started near midnight, so its cost doesn't grow with the store. A
// folder named for a date more than a day after now, as a clock set wrong
// can leave, is passed over. Only names, types and modification times are
// read; no file is opened. complete is false when the store can't be read
// within the bounds.
func newestCodexChat(ctx context.Context, dir string, now time.Time) (newest time.Time, complete bool) {
	root, err := paths.OpenRoot(dir, false)
	if err != nil {
		return time.Time{}, false
	}
	defer root.Close()
	days, ok := newestDays(ctx, root, now.AddDate(0, 0, 1).Format("2006/01/02"), 2)
	if !ok {
		return time.Time{}, false
	}
	left := folderEntries
	for _, day := range days {
		info, err := root.Lstat(day)
		if err != nil || !info.IsDir() {
			return time.Time{}, false
		}
		mod := info.ModTime()
		if mod.After(now.Add(time.Minute)) || !mod.After(newest) {
			continue
		}
		chat, ok := holdsChat(ctx, root, day, &left)
		if !ok {
			return time.Time{}, false
		}
		if chat {
			newest = mod
		}
	}
	return newest, true
}

// newestDays names up to n of root's day folders, <YYYY>/<MM>/<DD>, newest
// by name first, passing over names that aren't digits of those widths and
// dates after last, a YYYY/MM/DD. ok is false when a folder on the way
// can't be listed within the bounds.
func newestDays(ctx context.Context, root *os.Root, last string, n int) (days []string, ok bool) {
	years, ok := numbered(ctx, root, ".", 4)
	for _, y := range years {
		if !ok || len(days) == n {
			break
		}
		if y > last[:4] {
			continue
		}
		var months []string
		months, ok = numbered(ctx, root, y, 2)
		for _, m := range months {
			if !ok || len(days) == n {
				break
			}
			if y+"/"+m > last[:7] {
				continue
			}
			var dates []string
			dates, ok = numbered(ctx, root, filepath.Join(y, m), 2)
			for _, day := range dates {
				if !ok || len(days) == n {
					break
				}
				if y+"/"+m+"/"+day <= last {
					days = append(days, filepath.Join(y, m, day))
				}
			}
		}
	}
	return days, ok
}

// numbered lists the folders in root's dir whose names are width digits,
// greatest first. It reports false when dir can't be listed whole within
// the bounds.
func numbered(ctx context.Context, root *os.Root, dir string, width int) ([]string, bool) {
	if ctx.Err() != nil {
		return nil, false
	}
	f, err := root.Open(dir)
	if err != nil {
		return nil, false
	}
	defer f.Close()
	entries, err := f.ReadDir(folderEntries + 1)
	if err != nil && err != io.EOF || len(entries) > folderEntries {
		return nil, false
	}
	var names []string
	for _, e := range entries {
		if name := e.Name(); e.IsDir() && len(name) == width && strings.Trim(name, "0123456789") == "" {
			names = append(names, name)
		}
	}
	slices.Sort(names)
	slices.Reverse(names)
	return names, true
}

// holdsChat reports whether root's folder name holds a chat, a regular
// .jsonl file, from its entries' names and types, reading at most *left of
// them and taking what it read from *left. ok is false when it can't tell.
func holdsChat(ctx context.Context, root *os.Root, name string, left *int) (chat, ok bool) {
	f, err := root.Open(name)
	if err != nil {
		return false, false
	}
	defer f.Close()
	if info, err := f.Stat(); err != nil || !info.IsDir() {
		return false, false
	}
	for {
		if ctx.Err() != nil || *left <= 0 {
			return false, false
		}
		batch, e := f.ReadDir(min(128, *left))
		*left -= len(batch)
		for _, entry := range batch {
			if entry.Type().IsRegular() && strings.HasSuffix(entry.Name(), ".jsonl") {
				return true, true
			}
		}
		if e == io.EOF {
			return false, true
		}
		if e != nil {
			return false, false
		}
	}
}

// store names the folder of a host's chats that transcript recovery and
// the capture-freshness check read.
func store(host string) string {
	if host == "codex" {
		return "~/.codex/sessions"
	}
	return "~/.claude/projects"
}

// hostBeats reads the host's heartbeats: the time of the newest of any
// event, and whether the newest run that named a transcript found it
// outside the folder transcript recovery reads, as when CLAUDE_CONFIG_DIR
// or CODEX_HOME moves the host's folder (see the hook's
// transcriptElsewhere).
func hostBeats(d paths.Dirs, host string, now time.Time) (latest time.Time, moved bool) {
	var named beat
	for _, event := range hostEvents(host) {
		b, valid := readBeat(d, host, event, now)
		if !valid {
			continue
		}
		if b.At.After(latest) {
			latest = b.At
		}
		if b.TranscriptAvailable && b.At.After(named.At) {
			named = b
		}
	}
	return latest, named.TranscriptElsewhere
}

// moves names the variable that moves a host's folder elsewhere.
func moves(host string) string {
	if host == "codex" {
		return "CODEX_HOME"
	}
	return "CLAUDE_CONFIG_DIR"
}
