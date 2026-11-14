package health

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/heyNag/eva-md/internal/catchup"
	"github.com/heyNag/eva-md/internal/config"
	"github.com/heyNag/eva-md/internal/host/adapter"
	"github.com/heyNag/eva-md/internal/paths"
	"github.com/heyNag/eva-md/internal/update"
	"github.com/heyNag/eva-md/internal/wikigit"
)

// The types of condition a chat start may say, a closed list. Conditions
// evaluates them in this order, which is their order of severity: the high
// ones first, then the medium ones, a tie going to the earlier. A hook
// failing at SessionStart or UserPromptSubmit is high and comes before a
// sign-in; at any other event it is medium and comes after a diverged
// push.
const (
	CondUnknownKeys       = "config.unknown_keys"
	CondWikiUnreadable    = "wiki.unreadable"
	CondWikiInstance      = "wiki.instance"
	CondTrustReview       = "trust.review"
	CondCaptureSilent     = "capture.silent"
	CondHookFailing       = "hook.failing"
	CondUpdateSignIn      = "update.signin"
	CondPushDiverged      = "push.diverged"
	CondUpdateFailing     = "update.failing"
	CondUpdateQuarantined = "update.quarantined"
	CondSchemaNewer       = "wiki.schema_newer"
	CondRecoveryDrift     = "recovery.drift"
	CondCommitStale       = "commit.stale"
	CondPushStale         = "push.stale"
)

// conditionTypes are the types, each once, in the order of the list.
var conditionTypes = []string{CondUnknownKeys, CondWikiUnreadable, CondWikiInstance, CondTrustReview, CondCaptureSilent, CondHookFailing, CondUpdateSignIn, CondPushDiverged,
	CondUpdateFailing, CondUpdateQuarantined, CondSchemaNewer, CondRecoveryDrift, CondCommitStale, CondPushStale}

// hostTypes are the types that are about one host, whose id names it.
var hostTypes = []string{CondTrustReview, CondCaptureSilent, CondHookFailing, CondUpdateSignIn, CondUpdateFailing, CondUpdateQuarantined, CondRecoveryDrift}

// A Condition is one thing wrong that a chat start may say, in one line.
// Its ID is its type, then for a condition about a host the host's id, and
// for a failing hook the event, joined by slashes, as
// hook.failing/codex/Stop. Relevant is false for a condition about a host
// nobody uses (see unusedHost): it is kept, and never shown. Text is the
// line: at most maxLine characters, starting "eva.md: ", saying what is
// wrong and the one step; it holds counts, durations and fixed words only.
type Condition struct {
	ID, Type, Host, Event string
	Severity              string // "high" or "medium"
	Text                  string
	Relevant              bool
}

// maxLine is the most characters a condition's line may have.
const maxLine = 160

// maintainGrace is how long the day's maintain has to commit and push
// before a commit or a push past its budget is a condition: v1 commits at
// the day's first event, so after a weekend with no chat the wiki's
// changes wait past the commit's budget until that maintain has run.
const maintainGrace = time.Hour

// startEvents are the events whose failing hooks are high: a chat's start
// and its prompts, whose brief and capture a person relies on.
var startEvents = []string{"SessionStart", "UserPromptSubmit"}

// Conditions evaluates every condition of the closed list at now from v1's
// evidence and the stages (see Stages), in their order. It reads local
// files and runs git's local reads in the wiki; it writes nothing, starts no
// host command and makes no network or model call.
func Conditions(ctx context.Context, d paths.Dirs, c config.Config, now time.Time) []Condition {
	stages := Stages(ctx, d, c, now)
	used := map[string]bool{}
	for _, host := range updateHosts {
		used[host] = !unusedHost(d, host, stages, now)
	}
	var out []Condition
	add := func(typ, host, event string, n, of int, wait time.Duration) {
		cond := newCondition(typ, host, event, n, of, wait)
		cond.Relevant = host == "" || used[host]
		out = append(out, cond)
	}

	if len(c.Unknown) > 0 {
		add(CondUnknownKeys, "", "", 0, 0, 0)
	}
	// The wiki as the update and the daily commit judge it: they refuse a
	// schema they can't read or of no version, and an instance missing on
	// either side or not the config's.
	var schema config.WikiSchema
	schemaRead := false
	if c.WikiPath != "" {
		_, indexErr := readPrivate(filepath.Join(c.WikiPath, "index.md"), 1<<20)
		s, err := config.LoadSchema(filepath.Join(c.WikiPath, ".eva-md", "schema.toml"))
		schema, schemaRead = s, err == nil && s.SchemaVersion >= 1
		if indexErr != nil || !schemaRead {
			add(CondWikiUnreadable, "", "", 0, 0, 0)
		}
		if schemaRead && (c.Instance == "" || s.Instance == "" || s.Instance != c.Instance) {
			add(CondWikiInstance, "", "", 0, 0, 0)
		}
	}

	if reviewer, open := trustReview(d, stages, now); open {
		add(CondTrustReview, string(reviewer.ID), "", 0, 0, 0)
	}
	for _, s := range stages {
		if s.Name == StageCapture && !s.Off && s.Unread == "" && s.Lag > s.Budget {
			add(CondCaptureSilent, s.Host, "", 0, 0, s.Lag)
		}
	}
	failing := failingHooks(d, now)
	addHooks := func(start bool) {
		for _, f := range failing {
			if slices.Contains(startEvents, f.event) == start {
				add(CondHookFailing, f.host, f.event, f.failed, f.runs, 0)
			}
		}
	}
	addHooks(true)

	// The records read, even when a later file could not be, as v1's
	// notice uses them: an unreadable day or month hides no earlier one.
	states := map[string]update.HostState{}
	if c.WikiPath != "" && c.Updates {
		records, _ := update.Recent(d.State, now, doctorLog)
		for _, host := range updateHosts {
			states[host] = update.Host(records, host, now)
		}
	}
	for _, host := range updateHosts {
		// Signed out: held today for it, or the newest of the failures in a
		// row was for it, as on the next day before a run tries again.
		if s, ok := states[host]; ok && (s.Held == update.ReasonAuth || len(s.Failures) > 0 && s.Failures[0] == update.ReasonAuth) {
			add(CondUpdateSignIn, host, "", 0, 0, 0)
		}
	}
	var cs commits
	commitsRead := c.WikiPath != "" && c.Commits
	if commitsRead {
		records, _ := commitRecords(d.State, now, doctorCommit)
		cs = readCommits(records)
	}
	if commitsRead && cs.push == wikigit.PushDiverged {
		add(CondPushDiverged, "", "", 0, 0, 0)
	}

	addHooks(false)
	for _, host := range updateHosts {
		if n := len(states[host].Failures); n >= failuresInRow {
			add(CondUpdateFailing, host, "", n, 0, 0)
		}
	}
	for _, host := range updateHosts {
		if n := len(states[host].Quarantined); n > 0 {
			add(CondUpdateQuarantined, host, "", n, 0, 0)
		}
	}
	if schemaRead && schema.SchemaVersion > 1 {
		add(CondSchemaNewer, "", "", 0, 0, 0)
	}
	for _, host := range updateHosts {
		if _, found, err := catchup.ReadDrift(d, host); err == nil && found {
			add(CondRecoveryDrift, host, "", 0, 0, 0)
		}
	}
	// The commit and the push past their budgets, once the day's maintain
	// has had its time, and the commit failing for 48 hours by its records.
	chance := maintainRan(d, now)
	late := func(name string) (time.Duration, bool) {
		i := slices.IndexFunc(stages, func(s Stage) bool { return s.Name == name && s.Host == "" })
		if i < 0 || stages[i].Off || stages[i].Unread != "" || stages[i].Lag <= stages[i].Budget || !chance {
			return 0, false
		}
		return stages[i].Lag, true
	}
	wait, stale := late(StageCommit)
	if commitsRead && !cs.failing.IsZero() && now.Sub(cs.failing) > commitPending {
		wait, stale = max(wait, now.Sub(cs.failing)), true
	}
	if stale {
		add(CondCommitStale, "", "", 0, 0, wait)
	}
	if wait, stale := late(StagePush); stale {
		add(CondPushStale, "", "", 0, 0, wait)
	}
	return out
}

// newCondition is a condition of typ, about host and, for a failing hook,
// event, with its line: n and of are the counts it says (a hook's failed
// runs and all its runs, failed updates, quarantined chats), wait how long
// the work has waited. Its severity comes from its place in the list and,
// for a failing hook, its event.
func newCondition(typ, host, event string, n, of int, wait time.Duration) Condition {
	id := typ
	for _, part := range []string{host, event} {
		if part != "" {
			id += "/" + part
		}
	}
	severity := "high"
	if slices.Index(conditionTypes, typ) > slices.Index(conditionTypes, CondPushDiverged) || typ == CondHookFailing && !slices.Contains(startEvents, event) {
		severity = "medium"
	}
	h, _ := adapter.ByID(host)
	app := h.Names.App
	var text string
	switch typ {
	case CondUnknownKeys:
		text = "its config has keys it does not know, which it ignores, so a setting may be lost. Correct or remove them in eva-md's config.toml."
	case CondWikiUnreadable:
		text = "the wiki's index.md or .eva-md/schema.toml can't be read, so briefs and updates leave the wiki out. Restore it from the wiki's git history."
	case CondWikiInstance:
		text = "the wiki and eva-md's config name different installs, so nothing is written to the wiki. Point wiki_path and instance in config.toml at this Mac's wiki."
	case CondTrustReview:
		text = app + " chats are not saved until " + app + " approves eva-md's hooks. In " + app + ", type /hooks and approve eva-md."
	case CondCaptureSilent:
		text = app + "'s hooks have not run for " + span(wait) + " while it was in use, so its chats are not saved. Enable eva-md's plugin in " + app + "."
	case CondHookFailing:
		text = fmt.Sprintf("%d of the last %d %s %s hooks failed: they timed out or could not save. Free disk space and check eva-md's state folder.", n, of, app, event)
	case CondUpdateSignIn:
		text = app + " chats are not turned into notes while " + app + " is signed out. Run " + h.Login() + "."
	case CondPushDiverged:
		text = "the wiki's remote has commits this clone lacks, so pushes stopped. Merge them in the wiki by hand, never with a force push."
	case CondUpdateFailing:
		text = fmt.Sprintf("the last %d %s updates failed, so its chats wait to become notes. Run eva-md update --pending --foreground to see why.", n, app)
	case CondUpdateQuarantined:
		text = fmt.Sprintf("%d %s %s 3 updates in a row and %s once more tomorrow. eva-md status names %s.", n, app, plural(n, "chat failed", "chats failed"), plural(n, "is tried", "are tried"), plural(n, "it", "them"))
	case CondSchemaNewer:
		text = "a newer eva-md set up the wiki, so chats are captured but nothing is written to the wiki. Run eva-md upgrade."
	case CondRecoveryDrift:
		text = "it no longer reads " + app + "'s transcripts, so replies its hooks miss are not recovered. Run eva-md upgrade."
	case CondCommitStale:
		text = "the wiki's changes have waited " + span(wait) + " to be committed. Run eva-md commit to commit them, or to see what stops it."
	case CondPushStale:
		text = "the wiki's commits have waited " + span(wait) + " to be pushed. Run eva-md commit --push-only to push them, or to see what stops it."
	}
	return Condition{ID: id, Type: typ, Host: host, Event: event, Severity: severity, Text: "eva.md: " + text}
}

// failingHook is an event of a host whose hooks fail: failed of its last
// runs.
type failingHook struct {
	host, event  string
	failed, runs int
}

// eventWindow is how many of an event's newest runs it is judged on: twice
// the runs needed to judge, so a fault that starts after weeks of runs that
// passed is a condition after hookMinRuns failures, as on a fresh install,
// and not only once it is half of every run a heartbeat keeps.
const eventWindow = 2 * hookMinRuns

// failingHooks are the events, of each host in the adapter's order and in
// each host's order of events, whose own newest runs in the past 7 days, at
// most eventWindow of them and 4 or more, failed half or more of the time.
// Each event is judged on its own runs, never pooled with another's.
func failingHooks(d paths.Dirs, now time.Time) []failingHook {
	var out []failingHook
	for _, host := range updateHosts {
		for _, event := range hostEvents(host) {
			b, valid := readBeat(d, host, event, now)
			if !valid {
				continue
			}
			var recent []hookRun
			for _, r := range b.Runs {
				if !r.At.IsZero() && !r.At.After(now.Add(time.Minute)) && now.Sub(r.At) <= hookDays {
					recent = append(recent, r)
				}
			}
			// Newest first; of runs at the same time, the one the heartbeat
			// lists later, which it keeps oldest first.
			slices.Reverse(recent)
			slices.SortStableFunc(recent, func(a, b hookRun) int { return b.At.Compare(a.At) })
			recent = recent[:min(len(recent), eventWindow)]
			failed, runs := 0, len(recent)
			for _, r := range recent {
				if r.Failed {
					failed++
				}
			}
			if hooksFailing(failed, runs) {
				out = append(out, failingHook{host, event, failed, runs})
			}
		}
	}
	return out
}

// trustFlag reads the needs-trust flag install leaves, which can only be
// for the host whose plugin needs a review (see reviewHost): that host, the
// flag's file info, and whether the flag reads as install writes it, naming
// that host with needs_trust true by those exact keys. ok is false when
// there is no flag or no host needs a review. A flag that is there but does
// not read so, as one cut short, a link, one that can't be opened or one
// whose keys differ, is pending all the same, as v1's notice took it, and
// is not clean, so Prepare never removes it.
func trustFlag(d paths.Dirs) (reviewer adapter.Host, info os.FileInfo, clean, ok bool) {
	file := filepath.Join(d.State, "needs-trust.json")
	info, err := os.Lstat(file)
	if err != nil {
		return adapter.Host{}, nil, false, false
	}
	if reviewer, ok = reviewHost(); !ok {
		return adapter.Host{}, nil, false, false
	}
	if !info.Mode().IsRegular() {
		return reviewer, info, false, true
	}
	data, err := readPrivate(file, 16384)
	// Exact keys: encoding/json would match a key in another case, the
	// last of them winning.
	var fields map[string]json.RawMessage
	if err != nil || json.Unmarshal(data, &fields) != nil {
		return reviewer, info, false, true
	}
	var host string
	var needs bool
	if json.Unmarshal(fields["host"], &host) != nil || json.Unmarshal(fields["needs_trust"], &needs) != nil {
		return reviewer, info, false, true
	}
	named, known := trustPending(host)
	return reviewer, info, known && named.ID == reviewer.ID && needs, true
}

// trustMet reports whether the reviewer's hooks have run since the flag
// was written, so they are trusted and the flag is spent: its SessionStart
// hook, or a hook of it in a folder eva-md doesn't capture, which leaves
// only its stamp (see uncapturedAt).
func trustMet(d paths.Dirs, reviewer adapter.Host, flag os.FileInfo, now time.Time) bool {
	if b, valid := readBeat(d, string(reviewer.ID), "SessionStart", now); valid && b.At.After(flag.ModTime()) {
		return true
	}
	at, ok := uncapturedAt(d, string(reviewer.ID), now)
	return ok && at.After(flag.ModTime())
}

// trustReview reports the host whose hooks wait for a review in it: the
// flag is there, no hook of that host has run since it was written (see
// trustMet), and a chat of that host appeared more than the capture's bar
// after it. Until a chat starts there, nothing is lost and nothing is said,
// so a flag met on the day after an install raises nothing. When that
// host's chat folder could not be read, whether a chat started is not
// known, and the flag alone is the condition, as the card defines it.
func trustReview(d paths.Dirs, stages []Stage, now time.Time) (adapter.Host, bool) {
	reviewer, flag, _, ok := trustFlag(d)
	if !ok || trustMet(d, reviewer, flag, now) {
		return adapter.Host{}, false
	}
	i := slices.IndexFunc(stages, func(s Stage) bool { return s.Name == StageActivity && s.Host == string(reviewer.ID) })
	switch {
	case i >= 0 && stages[i].Unread != "":
		return reviewer, true
	case i < 0 || stages[i].Since.IsZero() || !stages[i].Since.After(flag.ModTime().Add(captureBar)):
		return adapter.Host{}, false
	}
	return reviewer, true
}

// maintainRan reports whether the day's maintain has had its time: the
// day's attempt marker, which the hook creates as it starts that maintain,
// is at least maintainGrace old.
func maintainRan(d paths.Dirs, now time.Time) bool {
	info, err := os.Lstat(filepath.Join(d.State, "commit", "attempt-"+now.Format(time.DateOnly)))
	return err == nil && now.Sub(info.ModTime()) >= maintainGrace
}

// knownID reports whether id names a condition this build can raise: a
// type of the list, then for a type about a host one of the hosts, and for
// a failing hook one of that host's events, and nothing more.
func knownID(id string) bool {
	parts := strings.Split(id, "/")
	if !slices.Contains(conditionTypes, parts[0]) {
		return false
	}
	if !slices.Contains(hostTypes, parts[0]) {
		return len(parts) == 1
	}
	if len(parts) < 2 || !slices.Contains(updateHosts, parts[1]) {
		return false
	}
	if parts[0] == CondHookFailing {
		return len(parts) == 3 && slices.Contains(hostEvents(parts[1]), parts[2])
	}
	return len(parts) == 2
}

// lineFor is the condition a host's chat start says: the first relevant one,
// leaving out the host's own capture.silent and trust.review, which its
// hook, running as it reads the line, disproves.
func lineFor(conds []Condition, host string) (Condition, bool) {
	for _, c := range conds {
		if !c.Relevant || c.Host == host && (c.Type == CondCaptureSilent || c.Type == CondTrustReview) {
			continue
		}
		return c, true
	}
	return Condition{}, false
}
