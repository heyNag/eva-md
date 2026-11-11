package health

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
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
	PartIncomplete = "incomplete"    // time ran out before it read everything: its figures are a floor
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
// sessions whose watermark is damaged.
type BacklogFigures struct {
	Hosts map[string]BacklogHost `json:"hosts"`
}

// BacklogHost is one host's backlog.
type BacklogHost struct {
	Sessions    int    `json:"sessions"`
	Bytes       int64  `json:"bytes"`
	OldestDay   string `json:"oldest_day"`
	OldestHours int    `json:"oldest_age_hours"`
	Untimed     int    `json:"untimed"`
	Damaged     int    `json:"damaged"`
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
	r.Operations = operations(ctx, d, c, cfgErr, now)
	r.Parts["operations"] = PartCounted

	var recorded bool
	r.Hooks, recorded = hookFigures(d, now)
	r.Parts["hooks"] = partState(recorded)

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
		r.Parts["record"] = partState(record.Sessions[update.HostClaude]+record.Sessions[update.HostCodex] > 0)
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

// operations counts the operations part.
func operations(ctx context.Context, d paths.Dirs, c config.Config, cfgErr error, now time.Time) Operations {
	first := now.AddDate(0, 0, 1-operationDays).Format(time.DateOnly)
	today := now.Format(time.DateOnly)
	in := func(day string) bool { return day >= first && day <= today }
	o := Operations{From: first, To: today, Commits: map[string]int{}, Reasons: map[string]int{}, Pushes: map[string]int{},
		HookRuns: map[string]map[string]HookRunsCounts{}}

	markers, _ := os.ReadDir(filepath.Join(d.State, "commit"))
	for _, m := range markers {
		day, ok := strings.CutPrefix(m.Name(), "attempt-")
		if t, err := time.Parse(time.DateOnly, day); ok && err == nil && t.Format(time.DateOnly) == day && in(day) {
			o.DailyJobDays++
		}
	}

	days := map[string]bool{}
	for _, rec := range commitRecordsSince(d.State, now, first) {
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
	}

	since := now.AddDate(0, 0, -operationDays)
	for _, host := range updateHosts {
		o.HookRuns[host] = map[string]HookRunsCounts{}
		for _, event := range hostEvents(host) {
			var n HookRunsCounts
			if b, valid := readBeat(d, host, event, now); valid {
				for _, run := range b.Runs {
					if run.At.After(since) && !run.At.After(now.Add(time.Minute)) {
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
		o.Notice = noticeKind(QuickNotice(ctx, d, c))
	}
	return o
}

// commitRecordsSince reads the daily commit records of the months from
// first's to now's, at most maxCommitRecords bytes from the end of each
// month's file, oldest first.
func commitRecordsSince(state string, now time.Time, first string) []wikigit.Result {
	var out []wikigit.Result
	start, err := time.ParseInLocation(time.DateOnly, first, now.Location())
	if err != nil {
		return nil
	}
	end := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location())
	for m := time.Date(start.Year(), start.Month(), 1, 0, 0, 0, 0, now.Location()); !m.After(end); m = m.AddDate(0, 1, 0) {
		data, err := update.ReadTail(filepath.Join(state, "commits"), m.Format("2006-01")+".jsonl", maxCommitRecords)
		if err != nil {
			continue
		}
		for _, line := range bytes.Split(data, []byte{'\n'}) {
			var rec wikigit.Result
			if json.Unmarshal(line, &rec) == nil && rec.Outcome != "" {
				out = append(out, rec)
			}
		}
	}
	return out
}

// notices name the start-of-chat notices by kind, each by words its text
// holds (see QuickNotice and backgroundNotice), in the order they are
// tried.
var notices = [][2]string{
	{"config has unknown keys", "config_unknown_keys"},
	{"wiki index is unavailable", "wiki_index"},
	{"wiki schema is newer", "wiki_schema"},
	{"wiki instance differs", "wiki_instance"},
	{"hook trust status is unavailable", "trust_unreadable"},
	{"review its hooks in Codex", "trust_review"},
	{"has no matching capture heartbeat", "capture_freshness"},
	{"hook runs failed", "hook_failures"},
	{"transcript recovery no longer recognizes", "recovery_drift"},
	{"updates are paused today", "updates_paused"},
	{"updates failed (", "update_failures"},
	{"quarantined after 3 failed updates", "quarantined"},
	{"has commits this clone lacks", "push_diverged"},
	{"has not been committed for", "commit_late"},
	{"has not been pushed for", "push_late"},
}

// noticeKind is the kind of a start-of-chat notice: none for no notice,
// and other for one no kind names.
func noticeKind(notice string) string {
	if notice == "" {
		return "none"
	}
	for _, n := range notices {
		if strings.Contains(notice, n[0]) {
			return n[1]
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
		bh := BacklogHost{Sessions: h.Sessions, Bytes: h.Bytes, Untimed: h.Untimed, Damaged: h.Damaged}
		if !h.Oldest.IsZero() {
			bh.OldestDay = h.Oldest.In(now.Location()).Format(time.DateOnly)
			bh.OldestHours = max(int(now.Sub(h.Oldest)/time.Hour), 0)
		}
		b.Hosts[host] = bh
	}
	if !w.Complete {
		return b, PartIncomplete
	}
	return b, PartCounted
}
