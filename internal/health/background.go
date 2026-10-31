package health

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/heyNag/eva-md/internal/config"
	"github.com/heyNag/eva-md/internal/lint"
	"github.com/heyNag/eva-md/internal/paths"
	"github.com/heyNag/eva-md/internal/systempage"
	"github.com/heyNag/eva-md/internal/update"
	"github.com/heyNag/eva-md/internal/wikigit"
)

// How much of the newest records the checks read, from the end of each
// file: the start-of-chat notice and the push retry read little, so a long
// day or month costs a hook no more than that.
const (
	noticeLog    = 64 << 10  // bytes of each day's run log, for the notice
	doctorLog    = 1 << 20   // ... for the doctor and eva-md status
	noticeCommit = 16 << 10  // bytes of each month's commit records, for the notice and the push retry
	doctorCommit = 256 << 10 // ... for the doctor and eva-md status
	maxMap       = 256 << 10 // bytes of systems/map.md
)

// The thresholds of the start-of-chat notices (docs/design.md "Health check").
const (
	failuresInRow = 3
	commitPending = 48 * time.Hour
	pushFailing   = 72 * time.Hour
)

var updateHosts = []string{update.HostClaude, update.HostCodex}

// inspectBackground adds the rows the records on disk give: each host's
// updates from the run log, the daily commit and the push from the commit
// records, the lint's result and the system map. It reads the newest records
// only, never the inbox or the wiki whole.
func inspectBackground(r *Report, d paths.Dirs, c config.Config, now time.Time) {
	if c.WikiPath == "" {
		for _, host := range updateHosts {
			r.add(host+" updates", "UNKNOWN", "no wiki is set up", "run eva-md setup")
		}
		r.add("daily commit", "UNKNOWN", "no wiki is set up", "run eva-md setup")
		return
	}
	inspectUpdates(r, d, c, now)
	inspectCommits(r, d, c, now)
	inspectLint(r, d)
	inspectSystems(r, c)
}

func inspectUpdates(r *Report, d paths.Dirs, c config.Config, now time.Time) {
	records, err := update.Recent(d.State, now, doctorLog)
	for _, host := range updateHosts {
		name := host + " updates"
		app, absent := Absent(host)
		switch {
		case !Installed(d, host) && absent:
			r.add(name, "UNKNOWN", app+" isn't installed on this Mac", "")
		case !Installed(d, host):
			r.add(name, "UNKNOWN", "eva-md is not installed for "+host, "run eva-md install")
		case !c.Updates:
			r.add(name, "UNKNOWN", "updates are off (updates = false in the config)", "set updates = true in the eva-md config to turn them on")
		case err != nil:
			r.add(name, "UNKNOWN", "the run log is unreadable", "inspect the files in eva-md's state folder, updates/")
		default:
			status, detail, fix := updateRow(update.Host(records, host, now), host, c.DailyCap, update.CLIRecorded(d.State, host))
			r.add(name, status, detail, fix)
		}
	}
}

// updateRow words a host's run-log state as a check; recorded is whether
// install recorded a path to the host's CLI that a run would start.
func updateRow(s update.HostState, host string, dailyCap int, recorded bool) (status, detail, fix string) {
	var facts []string
	if s.Held != "" {
		facts = append(facts, "paused today after "+s.Held+" ("+holdWords(s.Held, host)+")")
	}
	if n := len(s.Failures); n > 0 {
		facts = append(facts, fmt.Sprintf("the last %d failed (%s)", n, distinct(s.Failures)))
	}
	if n := len(s.Quarantined); n > 0 {
		facts = append(facts, fmt.Sprintf("%d %s quarantined today (%s)", n, plural(n, "session", "sessions"), sessionRefs(host, s.Quarantined)))
	}
	if n := len(s.Watermarks); n > 0 {
		facts = append(facts, fmt.Sprintf("%d %s a watermark that can't be used or saved (%s)", n, plural(n, "session has", "sessions have"), sessionRefs(host, s.Watermarks)))
	}
	if n := len(s.Left); n > 0 {
		facts = append(facts, fmt.Sprintf("%d %s from before updates couldn't be set aside yet (%s)", n, plural(n, "chat", "chats"), sessionRefs(host, s.Left)))
	}
	if s.Last.At != "" {
		facts = append(facts, "last "+when(s.Last.At)+": "+runOutcome(s.Last))
	} else {
		facts = append(facts, "no update recorded today or yesterday")
	}
	facts = append(facts, fmt.Sprintf("%d of %d model calls today", s.Calls, dailyCap))
	detail = strings.Join(facts, "; ")
	retry := "eva-md update --pending --foreground"
	again, next := "run "+retry+" to retry and see the failure", "the next trigger retries; "+retry+" retries now"
	if slices.Contains(s.Failures, update.ReasonCLIMissing) && !recorded {
		// A run from a terminal finds the CLI on its PATH, while a job a hook
		// started from an app may not, until install records where it is.
		again = "run eva-md install again to record the CLI's path"
		next = again
	}
	switch {
	case s.Held != "":
		return "FAIL", detail, holdFix(s.Held, host) + ", then run " + retry
	case len(s.Failures) >= failuresInRow:
		return "FAIL", detail, again
	case len(s.Quarantined) > 0:
		return "FAIL", detail, "each gets one retry tomorrow; eva-md update --session " + host + ":<id> --foreground retries one now"
	case len(s.Watermarks) > 0:
		return "FAIL", detail, "eva-md update --session " + host + ":<id> --foreground shows why; deleting a damaged watermark, in the .state folder beside the session's inbox file, reads the session again from its start"
	case len(s.Left) > 0:
		return "FAIL", detail, "eva-md update --session " + host + ":<id> --foreground shows why; once its file can be read and its watermark saved, the next update sets it aside, never working through it"
	case s.Last.At == "":
		return "UNKNOWN", detail, "updates start when a chat ends; " + retry + " runs one now"
	case len(s.Failures) > 0:
		return "UNKNOWN", detail, next
	}
	return "PASS", detail, ""
}

// holdWords says why a hold stops a host's updates, as a person would.
func holdWords(reason, host string) string {
	switch reason {
	case update.ReasonAuth:
		return "not logged in"
	case update.ReasonMCPFailed:
		return "a required MCP server failed to start"
	}
	return reason
}

// holdFix is what ends a hold.
func holdFix(reason, host string) string {
	switch reason {
	case update.ReasonAuth:
		if host == update.HostCodex {
			return "log in with codex login"
		}
		return "log in with claude auth login"
	}
	return "fix or remove the required MCP server in Codex's settings"
}

// cli is a host's command.
func cli(host string) string {
	if host == update.HostCodex {
		return "codex"
	}
	return "claude"
}

// sessionRefs names up to 3 sessions as host:<first 8 characters>.
func sessionRefs(host string, sessions []string) string {
	var refs []string
	for _, s := range sessions[:min(len(sessions), 3)] {
		r := []rune(s)
		refs = append(refs, host+":"+string(r[:min(len(r), 8)]))
	}
	if n := len(sessions) - len(refs); n > 0 {
		refs = append(refs, fmt.Sprintf("%d more", n))
	}
	return strings.Join(refs, ", ")
}

// distinct joins reasons, each once, in their order.
func distinct(reasons []string) string {
	var out []string
	for _, r := range reasons {
		if !slices.Contains(out, r) {
			out = append(out, r)
		}
	}
	return strings.Join(out, ", ")
}

// runOutcome is a run-log record's outcome with its reason.
func runOutcome(r update.Record) string {
	if r.Reason != "" {
		return r.Outcome + " (" + r.Reason + ")"
	}
	return r.Outcome
}

// commits is what the daily commit records say, read newest first.
type commits struct {
	last    wikigit.Result // the newest run that tried to commit: not busy, not a push of its own
	failing time.Time      // the oldest refused or failed run since the last good one; zero after a good one
	push    string         // the newest push result that says something: ok, up_to_date, pending, failed or diverged
	pushAt  time.Time
	pushBad time.Time // the oldest pending, failed or diverged push since the last good one; zero after a good one
}

// readCommits works out the commit state from records, oldest first. A busy
// run did nothing and says nothing.
func readCommits(records []wikigit.Result) commits {
	var s commits
	commitSeen, pushSeen := false, false
	for i := len(records) - 1; i >= 0; i-- {
		r := records[i]
		if r.Outcome == wikigit.Busy {
			continue
		}
		at, _ := time.Parse(time.RFC3339, r.At)
		if !commitSeen && r.Reason != wikigit.ReasonPushOnly {
			if s.last.At == "" {
				s.last = r
			}
			if r.Outcome == wikigit.Refused || r.Outcome == wikigit.Failed {
				s.failing = at
			} else {
				commitSeen = true
			}
		}
		if result, good := pushResult(r); !pushSeen && result != "" {
			if s.push == "" {
				s.push, s.pushAt = result, at
			}
			if good {
				pushSeen = true
			} else {
				s.pushBad = at
			}
		}
	}
	return s
}

// pushResult reads a record's push: good when it pushed or was up to date;
// bad when it was left pending, failed or diverged; and "" when the run
// skipped it, which says nothing about the push. A run with nothing to
// commit skips the push both when its branch is not ahead of the upstream
// and when it cannot tell, as without an upstream branch, so a pending push
// stays pending until a push says otherwise.
func pushResult(r wikigit.Result) (result string, good bool) {
	switch r.Push {
	case wikigit.PushOK, wikigit.PushUpToDate:
		return r.Push, true
	case wikigit.PushPending, wikigit.PushFailed, wikigit.PushDiverged:
		return r.Push, false
	}
	return "", false
}

func inspectCommits(r *Report, d paths.Dirs, c config.Config, now time.Time) {
	if !c.Commits {
		r.add("daily commit", "UNKNOWN", "daily commits are off (commits = false in the config)", "set commits = true in the eva-md config to turn them on")
		return
	}
	records, err := commitRecords(d.State, now, doctorCommit)
	if err != nil {
		r.add("daily commit", "UNKNOWN", "the commit records are unreadable", "inspect the files in eva-md's state folder, commits/")
		return
	}
	s := readCommits(records)
	status, detail, fix := commitRow(s, now)
	r.add("daily commit", status, detail, fix)
	status, detail, fix = pushRow(s, now)
	r.add("push", status, detail, fix)
}

// commitRow words the newest commit run as a check.
func commitRow(s commits, now time.Time) (status, detail, fix string) {
	l := s.last
	if l.At == "" {
		return "UNKNOWN", "no daily commit has run yet", "it runs at the first chat event of each day; eva-md commit runs it now"
	}
	detail = "last " + when(l.At) + ": " + l.Outcome
	switch {
	case l.Outcome == wikigit.Committed:
		detail += " (" + plural(l.Pages, "1 page", fmt.Sprintf("%d pages", l.Pages)) + ")"
	case l.Outcome == wikigit.Nothing:
		detail += " to commit"
	case l.Reason != "":
		detail += " (" + l.Reason + ")"
	}
	if !s.failing.IsZero() {
		detail += "; failing since " + s.failing.Format("2006-01-02 15:04") + " (" + age(now.Sub(s.failing)) + ")"
	}
	if n := len(l.Unstaged); n > 0 {
		detail += fmt.Sprintf("; %d left out: %s", n, list(l.Unstaged))
	}
	switch {
	case l.Outcome == wikigit.Refused || l.Outcome == wikigit.Failed:
		return "FAIL", detail, commitFix(l.Reason)
	case len(l.Unstaged) > 0:
		return "FAIL", detail, "remove what must not be committed from the files left out, then run eva-md commit"
	}
	return "PASS", detail, ""
}

// commitFix is what ends a refused or failed commit.
func commitFix(reason string) string {
	switch reason {
	case "origin":
		return "restore the wiki's origin and push URL to the configured remote"
	case "instance":
		return "select the intended wiki and config; another machine's wiki is never committed"
	case "index_lock":
		return "if no git is running in the wiki, remove its .git/index.lock by hand, then run eva-md commit"
	case "rebase", "merge", "cherry_pick":
		return "finish or abort the git " + strings.ReplaceAll(reason, "_", "-") + " in the wiki, then run eva-md commit"
	case "detached":
		return "check out the wiki's branch, then run eva-md commit"
	case "wiki":
		return "restore the wiki folder as a git clone"
	case "schema":
		return "a newer eva-md set up this wiki; update eva-md"
	case "commit_signer_missing":
		return "git couldn't start the signing program in the background; set gpg.program (or gpg.ssh.program) in your git config to its full path, then run eva-md commit"
	case "commit_signature":
		return "the signature failed or was declined; run eva-md commit and approve it"
	case "commit_timeout":
		return "the commit waited over 5 minutes, as for an approval nobody saw; run eva-md commit and approve it"
	case "commit":
		return "the commit failed; eva-md commit tries again now"
	}
	return "run eva-md commit to see whether it fails again"
}

// pushRow words the newest push as a check.
func pushRow(s commits, now time.Time) (status, detail, fix string) {
	if s.push == "" {
		return "UNKNOWN", "no push recorded yet", ""
	}
	detail = "last " + s.pushAt.Format("2006-01-02 15:04") + ": " + s.push
	since := ""
	if !s.pushBad.IsZero() {
		since = "; since " + s.pushBad.Format("2006-01-02 15:04") + " (" + age(now.Sub(s.pushBad)) + ")"
	}
	switch s.push {
	case wikigit.PushOK, wikigit.PushUpToDate:
		return "PASS", detail, ""
	case wikigit.PushDiverged:
		return "FAIL", detail + since + "; the remote has commits this clone lacks", "merge them by hand in the wiki, never with a force push, then run eva-md commit --push-only"
	case wikigit.PushFailed:
		return "FAIL", detail + since + "; the remote refused it", "check your access to the remote and its branch protection, then run eva-md commit --push-only"
	}
	fix = "reach the wiki's remote, or run eva-md commit --push-only; chat starts retry it every 30 minutes"
	if now.Sub(s.pushBad) > pushFailing {
		return "FAIL", detail + since, fix
	}
	return "UNKNOWN", detail + since, fix
}

// commitRecords reads the newest daily commit records, last month's and this
// month's, at most limit bytes from the end of each file, oldest first.
func commitRecords(state string, now time.Time, limit int64) ([]wikigit.Result, error) {
	var out []wikigit.Result
	month := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location())
	for _, m := range []time.Time{month.AddDate(0, -1, 0), month} {
		data, err := update.ReadTail(filepath.Join(state, "commits"), m.Format("2006-01")+".jsonl", limit)
		if err != nil {
			return out, err
		}
		for _, line := range bytes.Split(data, []byte{'\n'}) {
			var r wikigit.Result
			if json.Unmarshal(line, &r) == nil && r.Outcome != "" {
				out = append(out, r)
			}
		}
	}
	return out, nil
}

// RetryPush reports whether the newest commit run that did anything left its
// push pending, which a later chat start retries with eva-md maintain
// --push-only. A busy run and a run with nothing to commit that skipped the
// push say nothing about it and are passed over. A refused run, a failed or
// declined commit and a diverged push are never retried this way.
func RetryPush(d paths.Dirs, now time.Time) bool {
	records, _ := commitRecords(d.State, now, noticeCommit)
	for i := len(records) - 1; i >= 0; i-- {
		r := records[i]
		if r.Outcome == wikigit.Busy || r.Outcome == wikigit.Nothing && r.Push == wikigit.PushSkipped {
			continue
		}
		return r.Push == wikigit.PushPending
	}
	return false
}

// backgroundNotice is the start-of-chat notice the records give, or "": a
// hold, 3 or more update failures in a row, a quarantined session, a
// diverged wiki, a commit failing for more than 2 days or a push for more
// than 3.
func backgroundNotice(d paths.Dirs, c config.Config, now time.Time) string {
	if c.WikiPath == "" {
		return ""
	}
	if c.Updates {
		records, _ := update.Recent(d.State, now, noticeLog)
		var states []update.HostState
		for _, host := range updateHosts {
			states = append(states, update.Host(records, host, now))
		}
		for i, s := range states {
			if s.Held != "" {
				return "eva.md: " + updateHosts[i] + " updates are paused today: " + holdWords(s.Held, updateHosts[i]) + "; run eva-md doctor."
			}
		}
		for i, s := range states {
			if n := len(s.Failures); n >= failuresInRow {
				return fmt.Sprintf("eva.md: the last %d %s updates failed (%s); run eva-md doctor.", n, updateHosts[i], distinct(s.Failures))
			}
		}
		for i, s := range states {
			if n := len(s.Quarantined); n > 0 {
				return fmt.Sprintf("eva.md: %d %s %s quarantined after 3 failed updates; run eva-md doctor.", n, updateHosts[i], plural(n, "session is", "sessions are"))
			}
		}
	}
	if !c.Commits {
		return ""
	}
	records, _ := commitRecords(d.State, now, noticeCommit)
	s := readCommits(records)
	switch {
	case s.push == wikigit.PushDiverged:
		return "eva.md: the wiki's remote has commits this clone lacks, so pushes stopped; run eva-md doctor."
	case !s.failing.IsZero() && now.Sub(s.failing) > commitPending:
		return "eva.md: the wiki has not been committed for " + age(now.Sub(s.failing)) + " (" + s.last.Reason + "); run eva-md doctor."
	case (s.push == wikigit.PushPending || s.push == wikigit.PushFailed) && now.Sub(s.pushBad) > pushFailing:
		return "eva.md: the wiki has not been pushed for " + age(now.Sub(s.pushBad)) + " (" + s.push + "); run eva-md doctor."
	}
	return ""
}

// inspectLint adds the rows of the newest lint, <state>/lint.json.
func inspectLint(r *Report, d paths.Dirs) {
	file := filepath.Join(d.State, "lint.json")
	if _, err := os.Lstat(file); errors.Is(err, fs.ErrNotExist) {
		for _, name := range []string{"oversize pages", "source refs"} {
			r.add(name, "UNKNOWN", "the daily lint has not run yet", "it runs with the day's maintenance at the first chat event of a day")
		}
		return
	}
	var l lint.Result
	data, err := readPrivate(file, 1<<20)
	if err == nil {
		err = json.Unmarshal(data, &l)
	}
	if err != nil || l.At == "" {
		for _, name := range []string{"oversize pages", "source refs"} {
			r.add(name, "UNKNOWN", "the lint's result is unreadable", "the next day's maintenance writes it again")
		}
		return
	}
	of := fmt.Sprintf(" (lint of %s)", when(l.At))
	if n := len(l.Oversize); n > 0 {
		r.add("oversize pages", "FAIL", fmt.Sprintf("%d of %d thread pages over 512 KB: %s%s", n, l.Pages, list(l.Oversize), of), "trim each page's Timeline by hand; automatic rotation is not built yet")
	} else {
		r.add("oversize pages", "PASS", fmt.Sprintf("none of %d thread pages is over 512 KB%s", l.Pages, of), "")
	}
	if l.BadRefs > 0 {
		r.add("source refs", "FAIL", fmt.Sprintf("%d %s in %d %s name inbox lines that are gone: %s%s", l.BadRefs, plural(l.BadRefs, "ref", "refs"), len(l.BadRefPages), plural(len(l.BadRefPages), "page", "pages"), list(l.BadRefPages), of),
			"restore the inbox files they name, or leave them if those chats were removed on purpose")
	} else {
		r.add("source refs", "PASS", fmt.Sprintf("every ref in %d thread pages resolves%s", l.Pages, of), "")
	}
}

// mapLine is a line of the system map, which names its page's slug.
var mapLine = regexp.MustCompile(`^- \[.*?\]\(([a-z][a-z0-9-]{1,63})\.md\)`)

// inspectSystems checks that the system map lists exactly the system pages
// in systems/. It reads the map, bounded, and the folder's names, never a
// page.
func inspectSystems(r *Report, c config.Config) {
	dir := filepath.Join(c.WikiPath, "systems")
	entries, err := os.ReadDir(dir)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		r.add("system map", "UNKNOWN", "no system pages yet", "")
		return
	case err != nil:
		r.add("system map", "UNKNOWN", "the systems folder is unreadable", "restore the wiki's systems folder")
		return
	}
	pages := map[string]bool{}
	for _, e := range entries {
		if slug, ok := strings.CutSuffix(e.Name(), ".md"); ok && e.Type().IsRegular() && systempage.CheckSlug(slug) == nil {
			pages[slug] = true
		}
	}
	rebuild := "write any system page again with eva-md system write <slug> --replace, which rebuilds the map"
	data, err := readPrivate(filepath.Join(dir, "map.md"), maxMap)
	if err != nil {
		if len(pages) == 0 {
			r.add("system map", "UNKNOWN", "no system pages yet", "")
		} else {
			r.add("system map", "FAIL", fmt.Sprintf("%d system pages, but systems/map.md is missing, unreadable or over 256 KB", len(pages)), rebuild)
		}
		return
	}
	listed := map[string]bool{}
	for _, line := range strings.Split(string(data), "\n") {
		if m := mapLine.FindStringSubmatch(line); m != nil {
			listed[m[1]] = true
		}
	}
	var missing, gone []string
	for slug := range pages {
		if !listed[slug] {
			missing = append(missing, slug)
		}
	}
	for slug := range listed {
		if !pages[slug] {
			gone = append(gone, slug)
		}
	}
	if len(missing)+len(gone) == 0 {
		r.add("system map", "PASS", fmt.Sprintf("%d system %s, all on the map", len(pages), plural(len(pages), "page", "pages")), "")
		return
	}
	slices.Sort(missing)
	slices.Sort(gone)
	var facts []string
	if len(missing) > 0 {
		facts = append(facts, fmt.Sprintf("the map leaves out %d of %d system pages: %s", len(missing), len(pages), list(missing)))
	}
	if len(gone) > 0 {
		facts = append(facts, fmt.Sprintf("it lists %d %s: %s", len(gone), plural(len(gone), "page that no longer exists", "pages that no longer exist"), list(gone)))
	}
	r.add("system map", "FAIL", strings.Join(facts, "; "), rebuild)
}

// Installed reports whether eva-md's plugin is installed for host,
// claude-code or codex, by its marketplace's marker, without asking the host.
func Installed(d paths.Dirs, host string) bool {
	data, err := readPrivate(filepath.Join(d.Share, "marketplace", cli(host), ".eva-md-managed"), 1024)
	return err == nil && string(data) == "eva-md marketplace v1\n"
}

// when shows a record's RFC 3339 time as its local date and minute.
func when(at string) string {
	t, err := time.Parse(time.RFC3339, at)
	if err != nil {
		return at
	}
	return t.Format("2006-01-02 15:04")
}

// age words a duration in whole days, hours or minutes.
func age(d time.Duration) string {
	switch {
	case d >= 48*time.Hour:
		return fmt.Sprintf("%d days", int(d/(24*time.Hour)))
	case d >= 2*time.Hour:
		return fmt.Sprintf("%d hours", int(d/time.Hour))
	}
	return plural(int(d/time.Minute), "1 minute", fmt.Sprintf("%d minutes", int(d/time.Minute)))
}

// list names up to 3 items and counts the rest.
func list(items []string) string {
	s := strings.Join(items[:min(len(items), 3)], ", ")
	if n := len(items) - 3; n > 0 {
		s += fmt.Sprintf(" and %d more", n)
	}
	return s
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}
