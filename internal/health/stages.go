package health

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/heyNag/eva-md/internal/config"
	"github.com/heyNag/eva-md/internal/host/adapter"
	"github.com/heyNag/eva-md/internal/paths"
	"github.com/heyNag/eva-md/internal/update"
	"github.com/heyNag/eva-md/internal/wikigit"
	"github.com/heyNag/eva-md/internal/work"
)

// The stages a lag is measured at: each host's activity, capture and
// organizing, then the wiki's commit and push.
const (
	StageActivity   = "activity"
	StageCapture    = "capture"
	StageOrganizing = "organizing"
	StageCommit     = "commit"
	StagePush       = "push"
)

// Each stage's bar, the lag past which it is behind, and its budget, the lag
// past which its row fails (plan sections 5.4, 6.2 and 6.5). The commit's
// bar is v1's, which commits once a day; it becomes 2 seconds once the v2
// store commits each pass.
const (
	captureBar     = 5 * time.Minute
	captureBudget  = time.Hour
	organizeBar    = work.Bar
	organizeBudget = 24 * time.Hour
	commitBar      = 24 * time.Hour
	commitBudget   = 48 * time.Hour
	pushBar        = 10 * time.Minute
	pushBudget     = 72 * time.Hour
)

// usedWithin is how far back a host's activity and hook runs are read: a
// host with neither in that time, and no install in the last day, is one
// nobody uses.
const usedWithin = 14 * 24 * time.Hour

// commitLogDays bounds the days of the run log the commit stage reads, as
// far back as the commit records it reads go: this month's and last.
const commitLogDays = 62

// A Stage is how far one stage of a host's memory, or of the wiki, is
// behind. Lag is measured from Since, the oldest thing still waiting at the
// stage, never from a file's date, and is 0 when nothing waits; Bar and
// Budget are the stage's (see the constants above). Reason is why the work
// waits, for organizing past its bar, in work.Status's words; Waiting is
// how many things wait: sessions for organizing, files written and not yet
// committed for commit, commits not yet pushed for push.
//
// The activity stage has no lag: its Since is the host's newest chat, zero
// when there is none in the last 14 days. Unread, when it is not empty,
// says what evidence could not be read, so the lag is not known; Notes
// are what the stage counts apart, said beside its lag. Neither holds a
// path, a session or captured text.
type Stage struct {
	Name, Host       string
	Lag, Bar, Budget time.Duration
	Since            time.Time
	Reason           string
	Waiting          int
	Unread           string
	Notes            []string
}

// Stages reads each stage's lag at now: for each host, its activity, its
// capture and, with a wiki, its organizing; then, with a wiki, its commit
// and its push. It reads records, folder listings, watermarks and the
// entries past them, and runs git's local reads in the wiki; it writes
// nothing, starts no host command and makes no network call.
func Stages(ctx context.Context, d paths.Dirs, c config.Config, now time.Time) []Stage {
	var out []Stage
	var waiting Waiting
	var waitErr error
	var held work.Report
	if c.WikiPath != "" {
		waiting, waitErr = Unorganized(ctx, d)
		held = work.Status(ctx, d, now)
	}
	for _, host := range updateHosts {
		activity := activityStage(ctx, host, now)
		out = append(out, activity, captureStage(d, host, activity, now))
		if c.WikiPath != "" {
			out = append(out, organizingStage(host, waiting, waitErr, held, now))
		}
	}
	if c.WikiPath != "" {
		out = append(out, wikiStages(ctx, d, c, now)...)
	}
	return out
}

// activityStage reads a host's newest chat in the last 14 days from its
// chat folder, by names, types and modification times as the capture
// freshness reads them. A host with no chat folder has no activity.
func activityStage(ctx context.Context, host string, now time.Time) Stage {
	s := Stage{Name: StageActivity, Host: host}
	home, err := os.UserHomeDir()
	if err != nil {
		s.Unread = "the home folder is unknown"
		return s
	}
	h, _ := adapter.ByID(host)
	chats := h.Chats()
	dir := filepath.Join(home, filepath.FromSlash(chats.Folder))
	if _, err := os.Lstat(dir); errors.Is(err, fs.ErrNotExist) {
		return s
	}
	var newest time.Time
	complete := false
	switch chats.Layout {
	case adapter.PerDay:
		newest, complete = newestCodexChat(ctx, dir, now)
	case adapter.PerProject:
		newest, complete = newestClaudeChat(ctx, dir, now, usedWithin)
	}
	if !complete {
		s.Unread = "the host's chat folder, " + store(host) + ", could not be read whole within its bounds"
		return s
	}
	if !newest.IsZero() && now.Sub(newest) <= usedWithin {
		s.Since = newest
	}
	return s
}

// lastHookRun is when the host's hooks last ran, by their heartbeats and
// the stamp of a run in a folder eva-md doesn't capture, or the zero time.
func lastHookRun(d paths.Dirs, host string, now time.Time) time.Time {
	latest, _ := hostBeats(d, host, now)
	if at, ok := uncapturedAt(d, host, now); ok && at.After(latest) {
		latest = at
	}
	return latest
}

// installedAt is when eva-md was last installed for host, by its
// marketplace marker's modification time, or the zero time when it is not
// installed.
func installedAt(d paths.Dirs, host string) time.Time {
	if !Installed(d, host) {
		return time.Time{}
	}
	info, err := os.Lstat(filepath.Join(marketplace(d, host), ".eva-md-managed"))
	if err != nil {
		return time.Time{}
	}
	return info.ModTime()
}

// captureStage is the capture's lag: while the host's newest chat is newer
// than its newest hook run, or than its install when that is newer, by more
// than the bar, the time since that run; else 0. With neither a run nor an
// install, the lag runs from the chat.
func captureStage(d paths.Dirs, host string, activity Stage, now time.Time) Stage {
	s := Stage{Name: StageCapture, Host: host, Bar: captureBar, Budget: captureBudget}
	if activity.Unread != "" {
		s.Unread = activity.Unread
		return s
	}
	since := lastHookRun(d, host, now)
	if at := installedAt(d, host); at.After(since) && !at.After(now.Add(time.Minute)) {
		since = at
	}
	if a := activity.Since; !a.IsZero() && a.After(since.Add(captureBar)) {
		if since.IsZero() {
			since = a
		}
		s.Since, s.Lag = since, max(now.Sub(since), 0)
	}
	if _, moved := hostBeats(d, host, now); moved {
		s.Notes = append(s.Notes, "the host's newest chat keeps its transcript outside "+store(host)+", the folder this stage reads")
	}
	return s
}

// organizingStage is the organizing's lag: from the oldest entry past its
// session's watermark, in every session file of the inbox (see
// Unorganized), with the sessions that wait, what is counted apart, and,
// past the bar, the host's reason from work.Status: that of its oldest
// waiting work.
func organizingStage(host string, w Waiting, err error, held work.Report, now time.Time) Stage {
	s := Stage{Name: StageOrganizing, Host: host, Bar: organizeBar, Budget: organizeBudget}
	if err != nil {
		s.Unread = "the inbox could not be listed"
		return s
	}
	h := w.Hosts[host]
	s.Waiting = h.Sessions
	if !h.Oldest.IsZero() {
		s.Since, s.Lag = h.Oldest, max(now.Sub(h.Oldest), 0)
	}
	if s.Lag > s.Bar {
		for _, item := range held.Items {
			if item.Host == host {
				s.Reason = item.Reason.Words()
				break
			}
		}
	}
	if !w.Complete {
		s.Notes = append(s.Notes, fmt.Sprintf("a floor: %d project %s or session %s could not be read", w.Unread, plural(w.Unread, "folder", "folders"), plural(w.Unread, "file", "files")))
	}
	if h.Untimed > 0 {
		s.Notes = append(s.Notes, fmt.Sprintf("%d waiting %s no entry with a time, so %s not in the lag", h.Untimed, plural(h.Untimed, "session has", "sessions have"), plural(h.Untimed, "it is", "they are")))
	}
	if h.Held > 0 {
		note := fmt.Sprintf("%d %s a tail the update leaves until more is appended", h.Held, plural(h.Held, "session has", "sessions have"))
		if !h.HeldOldest.IsZero() {
			note += ", the oldest from " + span(max(now.Sub(h.HeldOldest), 0)) + " ago"
		}
		s.Notes = append(s.Notes, note)
	}
	if h.Damaged > 0 {
		s.Notes = append(s.Notes, fmt.Sprintf("%d %s a watermark that can't be read or is past the end of its file", h.Damaged, plural(h.Damaged, "session has", "sessions have")))
	}
	return s
}

// wikiStages are the commit's and the push's lags, from the commit records,
// the run log and what git says the wiki holds that its remote lacks.
func wikiStages(ctx context.Context, d paths.Dirs, c config.Config, now time.Time) []Stage {
	commit := Stage{Name: StageCommit, Bar: commitBar, Budget: commitBudget}
	push := Stage{Name: StagePush, Bar: pushBar, Budget: pushBudget}
	if !c.Commits {
		commit.Notes = append(commit.Notes, "daily commits are off (commits = false in the config)")
	}
	records, err := commitRecords(d.State, now, doctorCommit)
	if err != nil {
		commit.Unread, push.Unread = "the commit records are unreadable", "the commit records are unreadable"
		return []Stage{commit, push}
	}
	commits, files, err := wikigit.Unsaved(ctx, c.WikiPath)
	if err != nil {
		commit.Unread, push.Unread = "git could not read the wiki", "git could not read the wiki"
		return []Stage{commit, push}
	}
	commit.Waiting, push.Waiting = files, commits
	// The newest run that committed or found nothing to commit, the oldest
	// commit whose push has not succeeded since, and the newest good push.
	var saved, unpushed, pushed time.Time
	for _, r := range records {
		if r.Outcome == wikigit.Busy {
			continue
		}
		at, err := time.Parse(time.RFC3339, r.At)
		if err != nil {
			continue
		}
		if r.Reason != wikigit.ReasonPushOnly && (r.Outcome == wikigit.Committed || r.Outcome == wikigit.Nothing) {
			saved = at
		}
		if r.Outcome == wikigit.Committed && unpushed.IsZero() {
			unpushed = at
		}
		if _, good := pushResult(r); good {
			unpushed, pushed = time.Time{}, at
		}
	}
	if files > 0 {
		since, ok := firstWritten(d, saved, now)
		switch {
		case !ok:
			commit.Unread = "the run log is unreadable"
		case since.IsZero() && saved.IsZero():
			commit.Unread = "no run-log or commit record says since when the files have waited"
		case since.IsZero():
			// Nothing an update wrote since the last commit is on record, so
			// the files have waited at most since that commit.
			commit.Since = saved
		default:
			commit.Since = since
		}
	}
	if commits > 0 {
		switch {
		case !unpushed.IsZero():
			push.Since = unpushed
		case !pushed.IsZero():
			// No commit since the last good push is on record, so the
			// commits have waited at most since that push.
			push.Since = pushed
		default:
			push.Unread = "no commit record says since when the commits have waited"
		}
	}
	for _, s := range []*Stage{&commit, &push} {
		if !s.Since.IsZero() {
			s.Lag = max(now.Sub(s.Since), 0)
		}
	}
	return []Stage{commit, push}
}

// firstWritten is the time of the oldest run-log record that saved pages
// after saved, the last commit, read from that commit's day, or from as far
// back as the commit records go when there is none; the zero time when no
// such record is found. ok is false when the run log can't be read.
func firstWritten(d paths.Dirs, saved, now time.Time) (time.Time, bool) {
	days := commitLogDays
	if !saved.IsZero() {
		first := time.Date(saved.Year(), saved.Month(), saved.Day(), 0, 0, 0, 0, now.Location())
		today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
		days = min(max(int(today.Sub(first).Hours()/24)+1, 1), commitLogDays)
	}
	records, err := update.Days(d.State, now, days)
	if err != nil {
		return time.Time{}, false
	}
	var oldest time.Time
	for _, r := range records {
		if r.Pages <= 0 {
			continue
		}
		at, err := time.Parse(time.RFC3339, r.At)
		if err != nil || !at.After(saved) || at.After(now.Add(time.Minute)) {
			continue
		}
		if oldest.IsZero() || at.Before(oldest) {
			oldest = at
		}
	}
	return oldest, true
}

// span words a duration in whole days past two, else hours past one, else
// minutes, and "under a minute" below one.
func span(d time.Duration) string {
	switch {
	case d >= 48*time.Hour:
		return fmt.Sprintf("%d days", int(d/(24*time.Hour)))
	case d >= time.Hour:
		n := int(d / time.Hour)
		return plural(n, "1 hour", fmt.Sprintf("%d hours", n))
	case d >= time.Minute:
		n := int(d / time.Minute)
		return plural(n, "1 minute", fmt.Sprintf("%d minutes", n))
	}
	return "under a minute"
}
