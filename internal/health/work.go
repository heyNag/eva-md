package health

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"syscall"
	"time"

	"github.com/heyNag/eva-md/internal/config"
	"github.com/heyNag/eva-md/internal/inbox"
	"github.com/heyNag/eva-md/internal/paths"
	sessionmarker "github.com/heyNag/eva-md/internal/session"
	"github.com/heyNag/eva-md/internal/update"
	"github.com/heyNag/eva-md/internal/work"
)

// The worker's row, in eva-md status only, with a wiki and the switch
// worker on: what waits past the worker's bar, or is held, by reason, as
// eva-md work --status prints it. It fails only when work waits for a
// sign-in, as the updates row fails on auth. With the switch off it adds
// nothing, so status prints what it printed before the worker.
func init() {
	registerRows(1210, "background work", func(r *Report, in *inspection) {
		c, ok := in.statusWiki()
		if !ok || !work.Switch.On(c) {
			return
		}
		rep := work.Status(in.ctx, in.d, in.now())
		status, fix := "PASS", ""
		if hosts := signInHosts(rep); len(hosts) > 0 {
			var fixes []string
			for _, host := range hosts {
				fixes = append(fixes, holdFix(update.ReasonAuth, host))
			}
			status, fix = "FAIL", strings.Join(fixes, ", and ")
		}
		r.add("background work", status, strings.Join(WorkLines(rep, in.now()), "; "), fix)
	})
}

// signInHosts lists, sorted, the hosts whose work waits for a sign-in.
func signInHosts(rep work.Report) []string {
	var hosts []string
	for _, w := range rep.Items {
		if w.Reason == work.WaitSignIn && w.Host != "" && !slices.Contains(hosts, w.Host) {
			hosts = append(hosts, w.Host)
		}
	}
	slices.Sort(hosts)
	return hosts
}

// WorkLines words what work.Status found, a line a reason in the plan's
// order: how many items wait for it and how long the oldest has, and for
// a hold each failure's class, its host and the next try. It holds counts,
// reasons, hosts and times only, never a session, a key or a path.
func WorkLines(rep work.Report, now time.Time) []string {
	if len(rep.Items) == 0 {
		return []string{"nothing has waited longer than " + age(work.Bar)}
	}
	var lines []string
	for _, reason := range work.Reasons {
		n, oldest := 0, time.Time{}
		var holds []string
		for _, w := range rep.Items {
			if w.Reason != reason {
				continue
			}
			n++
			if oldest.IsZero() || w.Since.Before(oldest) {
				oldest = w.Since
			}
			switch reason {
			case work.WaitHeld:
				hold := className(w.Class) + hostPart(w.Host)
				if !w.NextTry.IsZero() {
					hold += " until " + w.NextTry.In(now.Location()).Format(time.DateTime)
				}
				if !slices.Contains(holds, hold) {
					holds = append(holds, hold)
				}
			case work.WaitSignIn:
				if hold := "auth" + hostPart(w.Host); !slices.Contains(holds, hold) {
					holds = append(holds, hold)
				}
			}
		}
		if n == 0 {
			continue
		}
		line := fmt.Sprintf("%s: %d, the oldest for %s", reason.Words(), n, age(max(now.Sub(oldest), 0)))
		if len(holds) > 0 {
			slices.Sort(holds)
			line += ", " + strings.Join(holds, ", ")
		}
		lines = append(lines, line)
	}
	return lines
}

// classPattern is a class as the run log names one, such as rate_limit.
var classPattern = regexp.MustCompile(`^[a-z][a-z_]{0,31}$`)

// className is a hold's class as printed: a run-log reason as it is, and
// anything else, which no release writes, as other.
func className(class string) string {
	if classPattern.MatchString(class) {
		return class
	}
	return "other"
}

// hostPart names a host, one eva-md updates, after a class.
func hostPart(host string) string {
	if slices.Contains(updateHosts, host) {
		return " on " + host
	}
	return ""
}

// UpdateWaits is the update job's Waiting, for work.Status: for each
// session file with entries past its watermark, among each project
// folder's 64 newest and 4,096 entries of the inbox in all, as the update
// backlog row lists them, one item of kind, since its oldest entry past
// the watermark, and why it waits:
//
//   - work.WaitOff when updates = false or the chat is off the record;
//   - work.WaitSignIn when its host is held after auth;
//   - work.WaitHeld for any other hold, a backoff or the session's
//     quarantine, with v1's reason as the class and when it ends as the
//     next try: the end of the backoff, and the next day for a hold or a
//     quarantine;
//   - work.WaitBudget when the host's model calls today reached daily_cap;
//   - work.WaitRule otherwise.
//
// It reads the run log's newest records, as status does, a watermark, a
// marker and the start of the first entry past the watermark a file, and
// writes nothing. A watermark that can't be read, or past the end of its
// file, gives no item: the update backlog row names it.
func UpdateWaits(ctx context.Context, d paths.Dirs, c config.Config, kind string, now time.Time) []work.Waiting {
	records, _ := update.Recent(d.State, now, doctorLog)
	states := map[string]update.HostState{}
	for _, host := range updateHosts {
		states[host] = update.Host(records, host, now)
	}
	tomorrow := time.Date(now.Year(), now.Month(), now.Day()+1, 0, 0, 0, 0, now.Location())
	store := inbox.Store{Root: d.Inbox()}
	markers := sessionmarker.Markers{Root: d.Sessions()}
	projects, _ := os.ReadDir(d.Inbox())
	left := maxEntries
	var out []work.Waiting
	for _, p := range projects {
		if !p.IsDir() || strings.HasPrefix(p.Name(), ".") {
			continue
		}
		if left == 0 || ctx.Err() != nil {
			break
		}
		files, _ := newestSessions(filepath.Join(d.Inbox(), p.Name()), &left)
		for _, f := range files {
			m, err := store.Mark(f.path)
			if err != nil || m.Offset >= f.size {
				continue
			}
			host, sid, ok := sessionOf(filepath.Base(f.path))
			if !ok {
				continue
			}
			since := oldestPast(f.path, m.Offset)
			if since.IsZero() {
				continue
			}
			w := work.Waiting{Kind: kind, Host: host, Since: since}
			s := states[host]
			switch {
			case !c.Updates || offRecord(ctx, markers, host, sid):
				w.Reason = work.WaitOff
			case s.Held == update.ReasonAuth:
				w.Reason = work.WaitSignIn
			case s.Held != "":
				w.Reason, w.Class, w.NextTry = work.WaitHeld, s.Held, tomorrow
			case now.Before(s.BackoffUntil):
				w.Reason, w.Class, w.NextTry = work.WaitHeld, update.StopBackoff, s.BackoffUntil
				if len(s.Failures) > 0 {
					w.Class = s.Failures[0]
				}
			case slices.ContainsFunc(s.Quarantined, func(q string) bool { return sameSession(q, sid) }):
				w.Reason, w.Class, w.NextTry = work.WaitHeld, update.StopQuarantined, tomorrow
			case s.Calls >= c.DailyCap:
				w.Reason = work.WaitBudget
			default:
				w.Reason = work.WaitRule
			}
			out = append(out, w)
		}
	}
	return out
}

// sessionOf is the host and the session part of a session file's name,
// <host>-<id>.jsonl, as the update names its records' sessions.
func sessionOf(name string) (host, sid string, ok bool) {
	name, ok = strings.CutSuffix(name, ".jsonl")
	if !ok {
		return "", "", false
	}
	for _, h := range updateHosts {
		if s, found := strings.CutPrefix(name, h+"-"); found && s != "" {
			return h, s, true
		}
	}
	return "", "", false
}

// sameSession reports whether id, a session as the run log records it, the
// id its entries carry, is sid, the session part of its inbox file's name.
func sameSession(id, sid string) bool {
	safe, err := paths.SafeName(id)
	return err == nil && safe == sid
}

// offRecord reports whether the chat is off the record now, by the marker
// the hooks keep while it is.
func offRecord(ctx context.Context, markers sessionmarker.Markers, host, sid string) bool {
	_, off, err := markers.GetContext(ctx, host, sid, sessionmarker.OffRecord)
	return err == nil && off
}

// firstEntryRead is how much of a session file oldestPast reads.
const firstEntryRead = 64 << 10

// oldestPast is when the first entry past offset, the oldest the update
// has yet to process, was captured, or the file's modification time when
// that entry can't be read in the first 64 KB past offset or carries no
// time. It opens the file through no link; one it can't open, or that is
// no regular file, gives the zero time.
func oldestPast(file string, offset int64) time.Time {
	f, err := os.OpenFile(file, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return time.Time{}
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return time.Time{}
	}
	data, _ := io.ReadAll(io.NewSectionReader(f, offset, firstEntryRead))
	if line, _, found := bytes.Cut(data, []byte{'\n'}); found {
		var e struct {
			TS string `json:"ts"`
		}
		if json.Unmarshal(line, &e) == nil {
			if t, err := time.Parse(time.RFC3339, e.TS); err == nil {
				return t
			}
		}
	}
	return info.ModTime()
}
