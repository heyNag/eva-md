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
	add := func(typ, host, event, severity, text string) {
		id := typ
		for _, part := range []string{host, event} {
			if part != "" {
				id += "/" + part
			}
		}
		out = append(out, Condition{ID: id, Type: typ, Host: host, Event: event, Severity: severity, Text: text, Relevant: host == "" || used[host]})
	}

	if len(c.Unknown) > 0 {
		add(CondUnknownKeys, "", "", "high", "eva.md: its config has keys it does not know, which it ignores, so a setting may be lost. Correct or remove them in eva-md's config.toml.")
	}
	var schema config.WikiSchema
	schemaRead := false
	if c.WikiPath != "" {
		if _, err := readPrivate(filepath.Join(c.WikiPath, "index.md"), 1<<20); err != nil {
			add(CondWikiUnreadable, "", "", "high", "eva.md: the wiki's index.md cannot be read, so briefs and updates leave the wiki out. Restore index.md at the wiki's root.")
		}
		s, err := config.LoadSchema(filepath.Join(c.WikiPath, ".eva-md", "schema.toml"))
		schema, schemaRead = s, err == nil
		if schemaRead && c.Instance != "" && s.Instance != c.Instance {
			add(CondWikiInstance, "", "", "high", "eva.md: the wiki is another install's, so nothing is written to it. Point wiki_path in eva-md's config.toml at this Mac's own wiki.")
		}
	}

	if reviewer, open := trustReview(d, stages, now); open {
		app := reviewer.Names.App
		add(CondTrustReview, string(reviewer.ID), "", "high", "eva.md: "+app+" chats are not saved until "+app+" approves eva-md's hooks. In "+app+", type /hooks and approve eva-md.")
	}
	for _, s := range stages {
		if s.Name == StageCapture && !s.Off && s.Unread == "" && s.Lag > s.Budget {
			app := appName(s.Host)
			add(CondCaptureSilent, s.Host, "", "high", "eva.md: "+app+"'s hooks have not run for "+span(s.Lag)+" while it was in use, so its chats are not saved. Enable eva-md's plugin in "+app+".")
		}
	}
	failing := failingHooks(d, now)
	addHooks := func(high bool) {
		for _, f := range failing {
			if slices.Contains(startEvents, f.event) != high {
				continue
			}
			severity := "medium"
			if high {
				severity = "high"
			}
			add(CondHookFailing, f.host, f.event, severity, fmt.Sprintf("eva.md: %d of the last %d %s %s hooks failed: they ran out of time or could not save. Free disk space and check eva-md's state folder.", f.failed, f.runs, appName(f.host), f.event))
		}
	}
	addHooks(true)

	states := map[string]update.HostState{}
	if c.WikiPath != "" && c.Updates {
		if records, err := update.Recent(d.State, now, doctorLog); err == nil {
			for _, host := range updateHosts {
				states[host] = update.Host(records, host, now)
			}
		}
	}
	for _, host := range updateHosts {
		// Signed out: held today for it, or the newest of the failures in a
		// row was for it, as on the next day before a run tries again.
		if s, ok := states[host]; ok && (s.Held == update.ReasonAuth || len(s.Failures) > 0 && s.Failures[0] == update.ReasonAuth) {
			h, _ := adapter.ByID(host)
			add(CondUpdateSignIn, host, "", "high", "eva.md: "+h.Names.App+" chats are not turned into notes while "+h.Names.App+" is signed out. Run "+h.Login()+".")
		}
	}
	var cs commits
	commitsRead := false
	if c.WikiPath != "" && c.Commits {
		if records, err := commitRecords(d.State, now, doctorCommit); err == nil {
			cs, commitsRead = readCommits(records), true
		}
	}
	if commitsRead && cs.push == wikigit.PushDiverged {
		add(CondPushDiverged, "", "", "high", "eva.md: the wiki's remote has commits this clone lacks, so pushes stopped. Merge them in the wiki by hand, never with a force push.")
	}

	addHooks(false)
	for _, host := range updateHosts {
		s, ok := states[host]
		if ok && len(s.Failures) >= failuresInRow {
			add(CondUpdateFailing, host, "", "medium", fmt.Sprintf("eva.md: the last %d %s updates failed, so its chats wait to become notes. Run eva-md update --pending --foreground to see why.", len(s.Failures), appName(host)))
		}
	}
	for _, host := range updateHosts {
		if n := len(states[host].Quarantined); n > 0 {
			add(CondUpdateQuarantined, host, "", "medium", fmt.Sprintf("eva.md: %d %s %s 3 updates in a row and %s once more tomorrow. eva-md status names %s.", n, appName(host), plural(n, "chat failed", "chats failed"), plural(n, "is tried", "are tried"), plural(n, "it", "them")))
		}
	}
	if schemaRead && schema.SchemaVersion > 1 {
		add(CondSchemaNewer, "", "", "medium", "eva.md: a newer eva-md set up the wiki, so chats are captured but nothing is written to the wiki. Run eva-md upgrade.")
	}
	for _, host := range updateHosts {
		if _, found, err := catchup.ReadDrift(d, host); err == nil && found {
			add(CondRecoveryDrift, host, "", "medium", "eva.md: it no longer reads "+appName(host)+"'s transcripts, so replies its hooks miss are not recovered. Run eva-md upgrade.")
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
		add(CondCommitStale, "", "", "medium", "eva.md: the wiki's changes have waited "+span(wait)+" to be committed. Run eva-md commit to commit them, or to see what stops it.")
	}
	if wait, stale := late(StagePush); stale {
		add(CondPushStale, "", "", "medium", "eva.md: the wiki's commits have waited "+span(wait)+" to be pushed. Run eva-md commit --push-only to push them, or to see what stops it.")
	}
	return out
}

// appName is a host's name as a person reads it, by its id.
func appName(host string) string {
	h, _ := adapter.ByID(host)
	return h.Names.App
}

// failingHook is an event of a host whose hooks fail: failed of its last
// runs.
type failingHook struct {
	host, event  string
	failed, runs int
}

// failingHooks are the events, of each host in the adapter's order and in
// each host's order of events, whose own last runs in the past 7 days, 4 or
// more of them, failed half or more of the time. Each event is judged on
// its own runs, never pooled with another's.
func failingHooks(d paths.Dirs, now time.Time) []failingHook {
	var out []failingHook
	for _, host := range updateHosts {
		for _, event := range hostEvents(host) {
			b, valid := readBeat(d, host, event, now)
			if !valid {
				continue
			}
			failed, runs := 0, 0
			for _, r := range b.Runs {
				if r.At.IsZero() || r.At.After(now.Add(time.Minute)) || now.Sub(r.At) > hookDays {
					continue
				}
				runs++
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

// trustFlag reads the needs-trust flag install leaves: the host it names,
// when that is one whose plugin needs a review, and the flag's file info.
// ok is false when there is no flag, or one that can't be read or names no
// such host.
func trustFlag(d paths.Dirs) (reviewer adapter.Host, info os.FileInfo, ok bool) {
	file := filepath.Join(d.State, "needs-trust.json")
	info, err := os.Lstat(file)
	if err != nil || !info.Mode().IsRegular() {
		return adapter.Host{}, nil, false
	}
	data, err := readPrivate(file, 16384)
	var flag struct {
		Host       string `json:"host"`
		NeedsTrust bool   `json:"needs_trust"`
	}
	if err != nil || json.Unmarshal(data, &flag) != nil || !flag.NeedsTrust {
		return adapter.Host{}, nil, false
	}
	reviewer, ok = trustPending(flag.Host)
	return reviewer, info, ok
}

// trustMet reports whether the reviewer's SessionStart hook has run since
// the flag was written: its hooks are trusted, so the flag is spent.
func trustMet(d paths.Dirs, reviewer adapter.Host, flag os.FileInfo, now time.Time) bool {
	b, valid := readBeat(d, string(reviewer.ID), "SessionStart", now)
	return valid && b.At.After(flag.ModTime())
}

// trustReview reports the host whose hooks wait for a review in it: the
// flag is there, no SessionStart hook of that host has run since it was
// written, and a chat of that host appeared more than the capture's bar
// after it. Until a chat starts there, nothing is lost and nothing is said,
// so a flag met on the day after an install raises nothing.
func trustReview(d paths.Dirs, stages []Stage, now time.Time) (adapter.Host, bool) {
	reviewer, flag, ok := trustFlag(d)
	if !ok || trustMet(d, reviewer, flag, now) {
		return adapter.Host{}, false
	}
	i := slices.IndexFunc(stages, func(s Stage) bool { return s.Name == StageActivity && s.Host == string(reviewer.ID) })
	if i < 0 || stages[i].Since.IsZero() || !stages[i].Since.After(flag.ModTime().Add(captureBar)) {
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
