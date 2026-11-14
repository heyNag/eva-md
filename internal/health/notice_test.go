package health

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/heyNag/eva-md/internal/config"
	"github.com/heyNag/eva-md/internal/inbox"
	"github.com/heyNag/eva-md/internal/paths"
	"github.com/heyNag/eva-md/internal/searchindex"
	"github.com/heyNag/eva-md/internal/update"
	"github.com/heyNag/eva-md/internal/wikigit"
)

// A month of an install replayed with an injected clock: a healthy one,
// which shows no line, and the same month with one fault planted, which
// shows that fault's line at the next start on a host where it is
// relevant, and once a day after, not more.

// monday is day 0 of the replayed month, a Monday, at midnight.
var monday = time.Date(2026, 3, 2, 0, 0, 0, 0, time.UTC)

// replay is an install replayed day by day. On each working day, every
// half hour from 9:00 to 17:30, a chat starts on each host in use, which
// reads its line as the hook does and records it as shown, and then
// Prepare runs, as that start, finding the notice 30 minutes old, would
// have asked, before any other work the start set off: so a start reads
// the notice prepared half an hour before it. The first start of the day
// takes the day's attempt marker, and the day's maintain commits and
// pushes a minute later, after that start's Prepare; five minutes after
// each start on the hour an update of each host's chat saves a page.
// Claude Code is used every working day, Codex from the day after the
// install on alternate weeks. eva-md is installed for both hosts at 8:00
// on day 0, which leaves the Codex trust flag. Nothing happens at
// weekends, so on a Monday the wiki's changes have waited past the
// commit's budget until the day's maintain has run.
type replay struct {
	t    *testing.T
	d    paths.Dirs
	c    config.Config
	wiki string
	runs map[string][]hookRun // each host's event's last runs, by host/event
	data map[string][]byte    // each session file's entries
	page int

	// The faults planted: hosts whose hooks no longer run, events whose
	// runs fail, how each host's updates end (a reason, or "quarantine"
	// for a session that fails between others that pass), and how the
	// day's commit ends (a refusal's reason) and its push ends.
	dead       map[string]bool
	fail       map[string]bool
	updateFail map[string]string
	commit     string
	push       string

	shows []shownAt
	lines []Condition // the lines Prepare wrote last
}

// shownAt is a line a chat start showed.
type shownAt struct {
	host, id, text string
	at             time.Time
}

func newReplay(t *testing.T) *replay {
	t.Helper()
	d := isolated(t)
	home := os.Getenv("HOME")
	remote, wiki := filepath.Join(home, "remote.git"), filepath.Join(home, "wiki")
	gitIn(t, "", "init", "-q", "--bare", remote)
	gitIn(t, "", "init", "-q", wiki)
	wiki, err := filepath.EvalSymlinks(wiki)
	if err != nil {
		t.Fatal(err)
	}
	gitIn(t, wiki, "remote", "add", "origin", remote)
	plant(t, filepath.Join(wiki, "index.md"), []byte("# Wiki\n"))
	if err := config.SaveSchema(filepath.Join(wiki, ".eva-md", "schema.toml"), config.WikiSchema{SchemaVersion: 1, Instance: "replay-instance", Created: "2026-03-02"}); err != nil {
		t.Fatal(err)
	}
	gitIn(t, wiki, "add", "-A")
	gitIn(t, wiki, "commit", "-q", "-m", "setup")
	gitIn(t, wiki, "push", "-q", "origin", "HEAD")
	c := config.Default()
	c.WikiPath, c.Instance = wiki, "replay-instance"
	c.Switches = map[string]any{"health": true}
	if err := config.Save(d.ConfigFile(), c); err != nil {
		t.Fatal(err)
	}
	install := monday.Add(8 * time.Hour)
	for _, host := range updateHosts {
		marker := filepath.Join(d.Share, "marketplace", cli(host), ".eva-md-managed")
		plant(t, marker, []byte("eva-md marketplace v1\n"))
		touch(t, marker, install)
	}
	plant(t, filepath.Join(d.State, "needs-trust.json"), []byte(`{"host":"codex","needs_trust":true}`+"\n"))
	touch(t, filepath.Join(d.State, "needs-trust.json"), install)
	return &replay{t: t, d: d, c: c, wiki: wiki, runs: map[string][]hookRun{}, data: map[string][]byte{},
		dead: map[string]bool{}, fail: map[string]bool{}, updateFail: map[string]string{}}
}

// touch sets a file's modification time.
func touch(t *testing.T, file string, at time.Time) {
	t.Helper()
	if err := os.Chtimes(file, at, at); err != nil {
		t.Fatal(err)
	}
}

// uses reports whether host is used on day.
func (r *replay) uses(host string, day int) bool {
	return host == updateHosts[0] || day >= 1 && day/7%2 == 0
}

// run replays the days before days; plant, when not nil, runs once,
// before the events of the first half hour at or after at.
func (r *replay) run(days int, at time.Time, plant func(*replay)) {
	for day := range days {
		date := monday.AddDate(0, 0, day)
		if wd := date.Weekday(); wd == time.Saturday || wd == time.Sunday {
			continue
		}
		if day == 10 {
			// The search index covers what the inbox holds by now, and so
			// part of what it holds at the month's end.
			if _, err := searchindex.Catchup(r.t.Context(), r.d, "index"); err != nil {
				r.t.Fatal(err)
			}
		}
		maintained := false
		for slot := range 18 {
			now := date.Add(9*time.Hour + time.Duration(slot)*30*time.Minute)
			if plant != nil && !now.Before(at) {
				plant(r)
				plant = nil
			}
			r.starts(day, now)
			r.prepare(now)
			if slot%2 == 0 && !maintained && r.attempted(now) {
				r.maintain(now.Add(time.Minute))
				maintained = true
			}
			if slot%2 == 0 {
				r.updates(day, now.Add(5*time.Minute))
			}
		}
	}
}

// starts starts a chat on each host used on day: it appears in the host's
// chat folder and, unless the host's hooks no longer run, its hook reads
// its line and shows it, takes the day's attempt marker as the day's first
// event, and leaves its heartbeats and the chat's entries.
func (r *replay) starts(day int, now time.Time) {
	t := r.t
	for _, host := range updateHosts {
		if !r.uses(host, day) {
			continue
		}
		home := os.Getenv("HOME")
		folder := filepath.Join(home, ".claude", "projects", "-project")
		if host != updateHosts[0] {
			folder = codexDay(now)
		}
		if _, err := os.Lstat(filepath.Join(folder, "chat.jsonl")); err != nil {
			plant(t, filepath.Join(folder, "chat.jsonl"), nil)
		}
		touch(t, folder, now)
		if r.dead[host] {
			continue
		}
		if id, text := PreparedLine(r.d, host, now); id != "" {
			r.shows = append(r.shows, shownAt{host, id, text, now})
			if err := MarkShown(t.Context(), r.d, id, now); err != nil {
				t.Fatal(err)
			}
		}
		attempt := filepath.Join(r.d.State, "commit", "attempt-"+now.Format(time.DateOnly))
		if _, err := os.Lstat(attempt); err != nil {
			plant(t, attempt, nil)
			touch(t, attempt, now)
		}
		for _, event := range []string{"SessionStart", "UserPromptSubmit", "Stop"} {
			key := host + "/" + event
			runs := append(r.runs[key], hookRun{At: now, Failed: r.fail[key]})
			r.runs[key] = runs[max(0, len(runs)-20):]
			raw, _ := json.Marshal(beat{At: now, Host: host, Event: event, Runs: r.runs[key]})
			plant(t, filepath.Join(r.d.State, "heartbeats", host, event+".json"), raw)
		}
		id := fmt.Sprintf("%s-day-%d", host, day)
		r.data[id] = append(r.data[id], chatLines(t, host, id, now, 1, true)...)
		file := (inbox.Store{Root: r.d.Inbox()}).Path("general", host, id)
		plant(t, file, r.data[id])
	}
}

// attempted reports whether a hook took the day's attempt marker.
func (r *replay) attempted(now time.Time) bool {
	_, err := os.Lstat(filepath.Join(r.d.State, "commit", "attempt-"+now.Format(time.DateOnly)))
	return err == nil
}

// prepare runs Prepare at now, and keeps the lines it wrote.
func (r *replay) prepare(now time.Time) {
	t := r.t
	if err := Prepare(t.Context(), r.d, now); err != nil {
		t.Fatalf("%s: %v", now, err)
	}
	var n notice
	data, err := os.ReadFile(filepath.Join(r.d.State, "health", "notice.json"))
	if err != nil || json.Unmarshal(data, &n) != nil || n.Prepared != now.Format(time.RFC3339) {
		t.Fatalf("%s: notice %s %v", now, data, err)
	}
	r.lines = nil
	for _, host := range updateHosts {
		if l, ok := n.Lines[host]; ok {
			r.lines = append(r.lines, Condition{ID: l.ID, Host: host, Text: l.Text})
		}
	}
}

// maintain is the day's maintain at now: it commits what the updates
// wrote and pushes it, or records the planted refusal or push.
func (r *replay) maintain(now time.Time) {
	t := r.t
	at := now.Format(time.RFC3339)
	if r.commit != "" {
		plantCommits(t, r.d, wikigit.Result{At: at, Outcome: wikigit.Refused, Reason: r.commit, Push: wikigit.PushSkipped})
		return
	}
	gitIn(t, r.wiki, "add", "-A")
	result := wikigit.Result{At: at, Outcome: wikigit.Nothing, Push: wikigit.PushSkipped}
	if commits, files, err := wikigit.Unsaved(t.Context(), r.wiki); err != nil {
		t.Fatal(err)
	} else if files > 0 {
		gitIn(t, r.wiki, "commit", "-q", "-m", "day")
		result.Outcome, result.Pages = wikigit.Committed, files
	} else if commits == 0 {
		plantCommits(t, r.d, result)
		return
	}
	switch r.push {
	case "":
		gitIn(t, r.wiki, "push", "-q", "origin", "HEAD")
		result.Push = wikigit.PushOK
	default:
		result.Push = r.push
	}
	plantCommits(t, r.d, result)
}

// updates runs an update of each host's chat of the day at now, unless
// its hooks no longer run: it saves a page and moves the session's
// watermark to its end, or fails as planted.
func (r *replay) updates(day int, now time.Time) {
	t := r.t
	at := now.Format(time.RFC3339)
	for _, host := range updateHosts {
		if !r.uses(host, day) || r.dead[host] {
			continue
		}
		id := fmt.Sprintf("%s-day-%d", host, day)
		switch reason := r.updateFail[host]; reason {
		case "":
		case "quarantine":
			plantRunLog(t, r.d, failedRecord(at, host, host+"-stuck", update.ReasonInvalidOutput))
		default:
			plantRunLog(t, r.d, failedRecord(at, host, id, reason))
			continue
		}
		r.page++
		plant(t, filepath.Join(r.wiki, "general", "threads", fmt.Sprintf("page-%d.md", r.page)), []byte("a page\n"))
		rec := okRecord(at, host, id)
		rec.Pages = 1
		plantRunLog(t, r.d, rec)
		store := inbox.Store{Root: r.d.Inbox()}
		if err := store.SetMark(store.Path("general", host, id), inbox.Mark{Offset: int64(len(r.data[id]))}); err != nil {
			t.Fatal(err)
		}
	}
}

// A replayed healthy month shows no line at any start, the trust flag
// met the day after install is gone, no notice holds a line, and the
// search index covers part of the inbox.
func TestReplayedHealthyMonthShowsNoLine(t *testing.T) {
	r := newReplay(t)
	r.run(30, time.Time{}, nil)
	if len(r.shows) > 0 {
		t.Fatalf("a healthy month showed %d lines, the first %+v", len(r.shows), r.shows[0])
	}
	if len(r.lines) > 0 {
		t.Fatalf("the last notice holds %+v", r.lines)
	}
	if _, err := os.Lstat(filepath.Join(r.d.State, "needs-trust.json")); !os.IsNotExist(err) {
		t.Fatalf("the trust flag, met on day 1, is still there: %v", err)
	}
	if st := searchindex.Inspect(r.d); st.Covered == 0 || st.Covered >= st.Inbox {
		t.Fatalf("the search index covers %d of %d bytes, not a part", st.Covered, st.Inbox)
	}
	healthyNames(t, r.d.State)
}

// healthyNames fails on a file under state whose name says something is
// wrong or waits, as a reader diagnosing a healthy install would take it.
func healthyNames(t *testing.T, state string) {
	t.Helper()
	err := filepath.WalkDir(state, func(path string, e fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		name := strings.ToLower(e.Name())
		for _, word := range []string{"needs", "retry", "fail", "error", "stale", "pending", "missing"} {
			if strings.Contains(name, word) {
				t.Errorf("a healthy state holds %s, whose name has %q", path, word)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// A fault planted in the same month: when, how, the line it shows, on
// which hosts it may show first, how long after it is planted it is shown
// at the latest, and whether it lasts, so that it is shown again on each
// working day after.
type fault struct {
	name  string
	at    time.Time
	plant func(*replay)
	id    string
	hosts []string
	by    time.Duration
	days  int
	daily bool
}

// faults are one fault for each type of condition, each planted on a
// Wednesday of a week Codex is used, at 10:30; the replay runs to the
// Friday after, or, for the commit and the push, whose lines wait for
// days, to the Tuesday after the weekend.
func faults(t *testing.T) []fault {
	claude, codex := updateHosts[0], updateHosts[1]
	both := updateHosts
	wednesday := monday.AddDate(0, 0, 2).Add(10*time.Hour + 30*time.Minute)
	return []fault{
		{"an unknown key", wednesday, func(r *replay) {
			f, err := os.OpenFile(r.d.ConfigFile(), os.O_APPEND|os.O_WRONLY, 0)
			if err != nil {
				t.Fatal(err)
			}
			defer f.Close()
			if _, err := f.WriteString("mystery_key = 1\n"); err != nil {
				t.Fatal(err)
			}
		}, CondUnknownKeys, both, time.Hour, 5, true},
		{"the wiki's index gone", wednesday, func(r *replay) {
			if err := os.Remove(filepath.Join(r.wiki, "index.md")); err != nil {
				t.Fatal(err)
			}
		}, CondWikiUnreadable, both, time.Hour, 5, true},
		{"another install's wiki", wednesday, func(r *replay) {
			if err := config.SaveSchema(filepath.Join(r.wiki, ".eva-md", "schema.toml"), config.WikiSchema{SchemaVersion: 1, Instance: "another-instance", Created: "2026-03-02"}); err != nil {
				t.Fatal(err)
			}
			r.commit = "instance"
		}, CondWikiInstance, both, time.Hour, 5, true},
		{"Codex's hooks wait for a review again", wednesday, func(r *replay) {
			flag := filepath.Join(r.d.State, "needs-trust.json")
			plant(t, flag, []byte(`{"host":"codex","needs_trust":true}`+"\n"))
			touch(t, flag, wednesday)
			r.dead[codex] = true
		}, CondTrustReview + "/" + codex, []string{claude}, 2 * time.Hour, 5, true},
		{"Codex's hooks stop", wednesday, func(r *replay) { r.dead[codex] = true }, CondCaptureSilent + "/" + codex, []string{claude}, 2 * time.Hour, 5, true},
		{"Claude Code's hooks stop", wednesday, func(r *replay) { r.dead[claude] = true }, CondCaptureSilent + "/" + claude, []string{codex}, 2 * time.Hour, 5, true},
		{"Claude Code's SessionStart fails", wednesday, func(r *replay) { r.fail[claude+"/SessionStart"] = true }, CondHookFailing + "/" + claude + "/SessionStart", both, 2 * 24 * time.Hour, 5, true},
		{"Codex's Stop fails", wednesday, func(r *replay) { r.fail[codex+"/Stop"] = true }, CondHookFailing + "/" + codex + "/Stop", both, 2 * 24 * time.Hour, 5, true},
		{"Claude Code signed out", wednesday, func(r *replay) { r.updateFail[claude] = update.ReasonAuth }, CondUpdateSignIn + "/" + claude, both, 2 * time.Hour, 5, true},
		{"a diverged remote", wednesday, func(r *replay) { r.push = wikigit.PushDiverged }, CondPushDiverged, both, 24 * time.Hour, 5, true},
		{"Codex's updates fail", wednesday, func(r *replay) { r.updateFail[codex] = update.ReasonHostError }, CondUpdateFailing + "/" + codex, both, 4 * time.Hour, 5, true},
		{"a Claude Code chat quarantined", wednesday, func(r *replay) { r.updateFail[claude] = "quarantine" }, CondUpdateQuarantined + "/" + claude, both, 4 * time.Hour, 5, false},
		{"a newer wiki", wednesday, func(r *replay) {
			if err := config.SaveSchema(filepath.Join(r.wiki, ".eva-md", "schema.toml"), config.WikiSchema{SchemaVersion: 2, Instance: "replay-instance", Created: "2026-03-02"}); err != nil {
				t.Fatal(err)
			}
			r.commit = "schema"
		}, CondSchemaNewer, both, time.Hour, 5, true},
		{"Codex's transcripts drift", wednesday, func(r *replay) {
			plant(t, filepath.Join(r.d.State, "catchup", "drift-"+codex+".json"), []byte(`{"v":1,"host":"codex","turns":3}`))
		}, CondRecoveryDrift + "/" + codex, both, time.Hour, 5, true},
		{"commits refused", wednesday, func(r *replay) { r.commit = "origin" }, CondCommitStale, both, 3 * 24 * time.Hour, 9, true},
		{"pushes pending", wednesday, func(r *replay) { r.push = wikigit.PushPending }, CondPushStale, both, 5*24*time.Hour + time.Hour, 9, true},
	}
}

// The same month with one fault planted, for each type of condition:
// nothing is shown before it; its line is the first shown after it, on a
// host where it is relevant, within its time; no other line is shown; and
// it is shown at most once in 24 hours, and, while it lasts, on each
// working day after.
func TestReplayedMonthWithAFaultShowsItsLine(t *testing.T) {
	seen := map[string]bool{}
	for _, f := range faults(t) {
		seen[strings.Split(f.id, "/")[0]] = true
		t.Run(f.name, func(t *testing.T) {
			r := newReplay(t)
			r.run(f.days, f.at, f.plant)
			if len(r.shows) == 0 {
				t.Fatalf("nothing shown; the last notice holds %+v", r.lines)
			}
			first := r.shows[0]
			if first.at.Before(f.at) || first.id != f.id || !slices.Contains(f.hosts, first.host) || first.at.Sub(f.at) > f.by {
				t.Fatalf("first shown %+v at %s; want %s on %q within %s of %s", first, first.at, f.id, f.hosts, f.by, f.at)
			}
			for i, s := range r.shows {
				if s.id != f.id {
					t.Fatalf("%s also showed %+v", f.id, s)
				}
				if i > 0 && s.at.Sub(r.shows[i-1].at) < 24*time.Hour {
					t.Fatalf("%s shown at %s and again at %s", f.id, r.shows[i-1].at, s.at)
				}
				if !strings.HasPrefix(s.text, "eva.md: ") || strings.Contains(s.text, "doctor") {
					t.Fatalf("%s shown as %q", f.id, s.text)
				}
			}
			if !f.daily {
				return
			}
			days := map[string]bool{}
			for _, s := range r.shows {
				days[s.at.Format(time.DateOnly)] = true
			}
			for day := first.at; day.Before(monday.AddDate(0, 0, f.days)); day = day.AddDate(0, 0, 1) {
				if wd := day.Weekday(); wd != time.Saturday && wd != time.Sunday && !days[day.Format(time.DateOnly)] {
					t.Errorf("%s was not shown on %s; shown %+v", f.id, day.Format(time.DateOnly), r.shows)
				}
			}
		})
	}
	for _, typ := range conditionTypes {
		if !seen[typ] {
			t.Errorf("no fault plants %s", typ)
		}
	}
}
