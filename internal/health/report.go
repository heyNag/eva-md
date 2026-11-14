package health

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/heyNag/eva-md/internal/config"
	"github.com/heyNag/eva-md/internal/paths"
	"github.com/heyNag/eva-md/internal/update"
	"github.com/heyNag/eva-md/internal/wikigit"
)

// ReportFormat is the format of eva-md doctor --report. Raise it whenever a
// key is added, removed or changes its meaning, so two reports are only
// compared key by key when their formats match.
const ReportFormat = 1

// operationDays is how many days, today's included, the report's operations
// part counts: as many as eva-md keeps of its run log and attempt markers.
const operationDays = 14

// The states a part of the report can be in, which say why a part holds
// only zeros or is left out.
const (
	PartCounted    = "counted"       // the part counts what was found
	PartNone       = "none_recorded" // nothing it counts was recorded: its figures are zeros
	PartUnreadable = "unreadable"    // its records couldn't be read: its figures are zeros
	PartIncomplete = "incomplete"    // it couldn't read all it counts, as time ran out or a folder or file couldn't be read: its figures are a floor
	PartNoWiki     = "no_wiki"       // no wiki is set up: the part is left out
)

// FieldReport is eva-md doctor --report: how eva-md has behaved on this
// Mac, in counts, durations, dates, eva-md's version and fixed names only,
// never a text, path, session, project or branch any file holds. Its keys
// are fixed by its format; every map is keyed by names eva-md's own code
// writes. Parts says, for each part, whether it counted what it found, or
// why its figures are zeros, a floor or left out. Each part names the days
// it counts, or, for the record, the first and last day an entry is dated.
type FieldReport struct {
	Format     int               `json:"format"`
	Version    string            `json:"version"`
	Day        string            `json:"day"`
	Parts      map[string]string `json:"parts"`
	Operations Operations        `json:"operations"`
	Hooks      HookFigures       `json:"hooks"`
	Updates    UpdateReport      `json:"updates"`
	Backlog    BacklogFigures    `json:"backlog"`
	Record     RecordFigures     `json:"record"`
	Wiki       *WikiFigures      `json:"wiki,omitempty"`
}

// Operations is how the daily job, the commit, the push, the trust flag and
// the hooks behaved over the operationDays days that end today: the days
// the daily job was started, by its attempt markers, and the days a commit
// run recorded something; commit runs by outcome, and by outcome and reason
// as outcome.reason; pushes by result; the Codex trust flag; each hook
// event's runs and failures among the last runs its heartbeat keeps; and
// the kind of notice a chat start would show now, since eva-md keeps no
// count of the notices it showed.
type Operations struct {
	From         string                               `json:"from"`
	To           string                               `json:"to"`
	DailyJobDays int                                  `json:"daily_job_days"`
	CommitDays   int                                  `json:"commit_days"`
	Commits      map[string]int                       `json:"commits"`
	Reasons      map[string]int                       `json:"commit_reasons"`
	Pushes       map[string]int                       `json:"pushes"`
	Trust        TrustFlag                            `json:"trust_flag"`
	HookRuns     map[string]map[string]HookRunsCounts `json:"hook_runs"`
	Notice       string                               `json:"notice_now"`
}

// TrustFlag is the flag install leaves until Codex's hooks are trusted:
// whether it is there, its age in whole days, and whether it is still
// awaited, with no Codex chat start since it was left.
type TrustFlag struct {
	Present bool `json:"present"`
	AgeDays int  `json:"age_days"`
	Awaited bool `json:"awaited"`
}

// HookRunsCounts is an event's runs, and those that failed, among the last
// runs its heartbeat keeps that fall in the days counted.
type HookRunsCounts struct {
	Runs   int `json:"runs"`
	Failed int `json:"failed"`
}

// BacklogFigures is what waits for an update now, by host (see
// Unorganized): sessions, bytes, the day of the oldest entry waiting and
// its age in whole hours, waiting sessions with no dated entry, and
// sessions whose watermark is damaged; and apart, the sessions the update
// leaves until more is appended, as a turn not yet finished, with their
// bytes and the day and age of their oldest entry.
type BacklogFigures struct {
	Hosts map[string]BacklogHost `json:"hosts"`
}

// BacklogHost is one host's backlog.
type BacklogHost struct {
	Sessions        int    `json:"sessions"`
	Bytes           int64  `json:"bytes"`
	OldestDay       string `json:"oldest_day"`
	OldestHours     int    `json:"oldest_age_hours"`
	Untimed         int    `json:"untimed"`
	Damaged         int    `json:"damaged"`
	Held            int    `json:"held"`
	HeldBytes       int64  `json:"held_bytes"`
	HeldOldestDay   string `json:"held_oldest_day"`
	HeldOldestHours int    `json:"held_oldest_age_hours"`
}

// The words of the commit records the report counts by.
var (
	commitOutcomes = []string{wikigit.Committed, wikigit.Nothing, wikigit.Refused, wikigit.Failed, wikigit.Busy}
	commitReasons  = []string{"lock", "wiki_lock", "commit", "commit_" + wikigit.ClassTimeout, "commit_" + wikigit.ClassSignerMissing,
		"commit_" + wikigit.ClassSignature, "add", "staged", "unstage", "wiki", "origin", "instance", "schema", "index_lock",
		"rebase", "merge", "cherry_pick", "revert", "conflicts", "detached", wikigit.ReasonPushOnly}
	pushResults = []string{wikigit.PushOK, wikigit.PushUpToDate, wikigit.PushPending, wikigit.PushDiverged, wikigit.PushFailed, wikigit.PushSkipped}
)

// maxCommitRecords bounds the bytes of each month's commit records read.
const maxCommitRecords = 8 << 20

// Field makes the report of eva-md doctor --report at now. It reads
// eva-md's config, state and inbox and the wiki's folder listing; it runs
// no command, makes no network or model call, repairs nothing and writes no
// file. A part whose records are missing or can't be read holds zeros, and
// says so in Parts; without a wiki, or with a config that can't be read,
// the wiki's part is left out, and Parts says which. When ctx
// ends, what is left to read is counted as far as it was read and the
// parts say so.
func Field(ctx context.Context, d paths.Dirs, now time.Time) FieldReport {
	r := FieldReport{Format: ReportFormat, Version: Version, Day: now.Format(time.DateOnly), Parts: map[string]string{}}
	c, cfgErr := config.Load(d.ConfigFile())
	o, complete := operations(ctx, d, c, cfgErr, now)
	r.Operations = o
	r.Parts["operations"] = PartCounted
	if !complete {
		r.Parts["operations"] = PartIncomplete
	}

	var recorded bool
	r.Hooks, recorded, complete = hookFigures(d, now)
	r.Parts["hooks"] = partState(recorded)
	if !complete {
		r.Parts["hooks"] = PartIncomplete
	}

	u, recorded, err := reportUpdates(d, now)
	r.Updates = u
	r.Parts["updates"] = partState(recorded)
	if err != nil {
		r.Updates, r.Parts["updates"] = zeroUpdates(now), PartUnreadable
	}

	r.Backlog, r.Parts["backlog"] = backlogFigures(ctx, d, now)

	var wiki []string
	if cfgErr == nil && c.WikiPath != "" {
		wiki = wikiRoots(c.WikiPath)
	}
	record, tools, complete, recordErr := readRecord(ctx, d.Inbox(), wiki, now.Location())
	r.Record = record
	switch {
	case recordErr != nil:
		r.Parts["record"] = PartUnreadable
	case !complete:
		r.Parts["record"] = PartIncomplete
	default:
		sessions := 0
		for _, host := range updateHosts {
			sessions += record.Sessions[host]
		}
		r.Parts["record"] = partState(sessions > 0)
	}

	switch {
	case cfgErr != nil && !errors.Is(cfgErr, fs.ErrNotExist):
		r.Parts["wiki"] = PartUnreadable
		return r
	case wiki == nil:
		r.Parts["wiki"] = PartNoWiki
		return r
	}
	w := &WikiFigures{toolUse: tools}
	pages, done, err := wikiPages(ctx, wiki[len(wiki)-1])
	w.Pages, w.Complete = pages, done && complete
	code, codeErr := codeFigures(d)
	w.Code = code
	switch {
	case err != nil || codeErr != nil || recordErr != nil:
		r.Parts["wiki"] = PartUnreadable
	case !w.Complete:
		r.Parts["wiki"] = PartIncomplete
	default:
		r.Parts["wiki"] = PartCounted
	}
	r.Wiki = w
	return r
}

// partState is PartCounted for a part that found what it counts, and
// PartNone for one that found nothing.
func partState(found bool) string {
	if found {
		return PartCounted
	}
	return PartNone
}

// wikiRoots are the folders the wiki is reached by: its path as the config
// names it and, when that differs, as links resolve it, which comes last.
func wikiRoots(wiki string) []string {
	roots := []string{filepath.Clean(wiki)}
	if real, err := filepath.EvalSymlinks(wiki); err == nil && real != roots[0] {
		roots = append(roots, real)
	}
	return roots
}

// operations counts the operations part. complete is false when a record
// it counts is there but couldn't be read: a folder of commit markers
// that can't be listed, a month of commit records, the trust flag or a
// heartbeat that can't be read; or when ctx ended before the notice was read.
func operations(ctx context.Context, d paths.Dirs, c config.Config, cfgErr error, now time.Time) (o Operations, complete bool) {
	first := now.AddDate(0, 0, 1-operationDays).Format(time.DateOnly)
	today := now.Format(time.DateOnly)
	in := func(day string) bool { return day >= first && day <= today }
	o = Operations{From: first, To: today, Commits: map[string]int{}, Reasons: map[string]int{}, Pushes: map[string]int{},
		HookRuns: map[string]map[string]HookRunsCounts{}}
	complete = true

	markers, err := os.ReadDir(filepath.Join(d.State, "commit"))
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		complete = false
	}
	for _, m := range markers {
		day, ok := strings.CutPrefix(m.Name(), "attempt-")
		if t, err := time.Parse(time.DateOnly, day); ok && err == nil && t.Format(time.DateOnly) == day && in(day) {
			o.DailyJobDays++
		}
	}

	days := map[string]bool{}
	records, read := commitRecordsSince(d.State, now, first)
	complete = complete && read
	for _, rec := range records {
		at := day(rec.At)
		if !in(at) {
			continue
		}
		outcome := known(rec.Outcome, commitOutcomes)
		o.Commits[outcome]++
		if rec.Reason != "" {
			o.Reasons[outcome+"."+known(rec.Reason, commitReasons)]++
		}
		if rec.Outcome == wikigit.Busy {
			continue
		}
		days[at] = true
		if rec.Push != "" {
			o.Pushes[known(rec.Push, pushResults)]++
		}
	}
	o.CommitDays = len(days)

	if info, err := os.Lstat(filepath.Join(d.State, "needs-trust.json")); err == nil {
		o.Trust = TrustFlag{Present: true, AgeDays: max(int(now.Sub(info.ModTime())/(24*time.Hour)), 0), Awaited: TrustAwaited(d, now)}
	} else if !errors.Is(err, fs.ErrNotExist) {
		complete = false
	}

	// The runs counted are those from the start of the first day counted,
	// in now's time zone, as From names it, to a minute past now.
	since, _ := time.ParseInLocation(time.DateOnly, first, now.Location())
	for _, host := range updateHosts {
		o.HookRuns[host] = map[string]HookRunsCounts{}
		for _, event := range hostEvents(host) {
			var n HookRunsCounts
			b, valid := readBeat(d, host, event, now)
			if !valid && beatUnreadable(d, host, event) {
				complete = false
			}
			if valid {
				for _, run := range b.Runs {
					if !run.At.Before(since) && !run.At.After(now.Add(time.Minute)) {
						n.Runs++
						if run.Failed {
							n.Failed++
						}
					}
				}
			}
			o.HookRuns[host][event] = n
		}
	}

	switch {
	case cfgErr != nil && !errors.Is(cfgErr, fs.ErrNotExist):
		o.Notice = "config_unreadable"
	default:
		notice := QuickNotice(ctx, d, c)
		o.Notice = noticeKind(notice)
		if notice == "" && ctx.Err() != nil {
			// QuickNotice gives none once ctx has ended, so whether a
			// notice would show is not known.
			o.Notice, complete = "unknown", false
		}
	}
	return o, complete
}

// beatUnreadable reports whether the heartbeat of host's event is there
// but can't be read, as against missing: a file or folder that can't be
// opened or read, or that is no regular file or too large.
func beatUnreadable(d paths.Dirs, host, event string) bool {
	_, err := readPrivate(filepath.Join(d.State, "heartbeats", host, event+".json"), 16384)
	return err != nil && !errors.Is(err, fs.ErrNotExist)
}

// commitRecordsSince reads the daily commit records of the months from
// first's to now's, at most maxCommitRecords bytes from the end of each
// month's file, oldest first. read is false when a month's records are
// there but can't be read.
func commitRecordsSince(state string, now time.Time, first string) (out []wikigit.Result, read bool) {
	start, err := time.ParseInLocation(time.DateOnly, first, now.Location())
	if err != nil {
		return nil, true
	}
	read = true
	end := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location())
	for m := time.Date(start.Year(), start.Month(), 1, 0, 0, 0, 0, now.Location()); !m.After(end); m = m.AddDate(0, 1, 0) {
		data, err := update.ReadTail(filepath.Join(state, "commits"), m.Format("2006-01")+".jsonl", maxCommitRecords)
		if err != nil {
			read = false
			continue
		}
		for _, line := range bytes.Split(data, []byte{'\n'}) {
			var rec wikigit.Result
			if json.Unmarshal(line, &rec) == nil && rec.Outcome != "" {
				out = append(out, rec)
			}
		}
	}
	return out, read
}

// notices name the start-of-chat notices by kind (see QuickNotice and
// backgroundNotice). Each pattern matches the start of a notice, after
// "eva.md: ", up to words no record gives: the notices differ there, and
// all that comes before those words is a number or a host's name, which
// eva-md's code gives. So text a record puts later into a notice, as a
// commit's or a failed update's reason, can't change the notice's kind.
var notices = []struct{ pattern, kind string }{
	{`config has unknown keys`, "config_unknown_keys"},
	{`wiki index is unavailable`, "wiki_index"},
	{`wiki schema is newer`, "wiki_schema"},
	{`wiki instance differs`, "wiki_instance"},
	{`hook trust status is unavailable`, "trust_unreadable"},
	{`review its hooks in Codex`, "trust_review"},
	{`recent [a-z-]+ activity has no matching capture heartbeat`, "capture_freshness"},
	{`[0-9]+ of the last [0-9]+ [a-z-]+ hook runs failed`, "hook_failures"},
	{`transcript recovery no longer recognizes`, "recovery_drift"},
	{`[a-z-]+ updates are paused today`, "updates_paused"},
	{`the last [0-9]+ [a-z-]+ updates failed \(`, "update_failures"},
	{`[0-9]+ [a-z-]+ [a-z ]+ quarantined after 3 failed updates`, "quarantined"},
	{`the wiki's remote has commits this clone lacks`, "push_diverged"},
	{`the wiki has not been committed for`, "commit_late"},
	{`the wiki has not been pushed for`, "push_late"},
}

// noticeKind is the kind of a start-of-chat notice: none for no notice,
// and other for one no kind names.
func noticeKind(notice string) string {
	if notice == "" {
		return "none"
	}
	rest, ok := strings.CutPrefix(notice, "eva.md: ")
	if !ok {
		return other
	}
	for _, n := range notices {
		if regexp.MustCompile(`^` + n.pattern).MatchString(rest) {
			return n.kind
		}
	}
	return other
}

// backlogFigures counts what waits for an update now, and its part's state.
func backlogFigures(ctx context.Context, d paths.Dirs, now time.Time) (BacklogFigures, string) {
	b := BacklogFigures{Hosts: map[string]BacklogHost{}}
	for _, host := range updateHosts {
		b.Hosts[host] = BacklogHost{}
	}
	w, err := Unorganized(ctx, d)
	if err != nil {
		return b, PartUnreadable
	}
	for host, h := range w.Hosts {
		bh := BacklogHost{Sessions: h.Sessions, Bytes: h.Bytes, Untimed: h.Untimed, Damaged: h.Damaged, Held: h.Held, HeldBytes: h.HeldBytes}
		bh.OldestDay, bh.OldestHours = dayAndAge(h.Oldest, now)
		bh.HeldOldestDay, bh.HeldOldestHours = dayAndAge(h.HeldOldest, now)
		b.Hosts[host] = bh
	}
	if !w.Complete {
		return b, PartIncomplete
	}
	return b, PartCounted
}

// dayAndAge is the day of t in now's time zone and its age at now in whole
// hours, or no day and 0 for no time.
func dayAndAge(t, now time.Time) (string, int) {
	if t.IsZero() {
		return "", 0
	}
	return t.In(now.Location()).Format(time.DateOnly), max(int(now.Sub(t)/time.Hour), 0)
}
