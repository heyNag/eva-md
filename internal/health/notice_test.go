package health

import (
	"context"
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
// reads its line as the hook does and records it as shown, and then, when
// a hook ran and found the notice due (see NoticeDue), Prepare runs, as
// the job that start asked for, before any other work the start set off:
// so a start reads the notice prepared at the last start that asked for
// it, an hour before, or on the day's first start the evening before, or
// the Friday before. The first start of the day
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
	home string // the home the test made
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
	return &replay{t: t, d: d, c: c, home: home, wiki: wiki, runs: map[string][]hookRun{}, data: map[string][]byte{},
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
			if r.starts(day, now) {
				r.prepare(now)
			}
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
// event, and leaves its heartbeats and the chat's entries. It reports
// whether a hook asked for the notice, as the hook does when it is due.
func (r *replay) starts(day int, now time.Time) (asked bool) {
	t := r.t
	due := NoticeDue(r.d, now)
	for _, host := range updateHosts {
		if !r.uses(host, day) {
			continue
		}
		folder := filepath.Join(r.home, ".claude", "projects", "-project")
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
		asked = asked || due
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
	return asked
}

// attempted reports whether a hook took the day's attempt marker.
func (r *replay) attempted(now time.Time) bool {
	_, err := os.Lstat(filepath.Join(r.d.State, "commit", "attempt-"+now.Format(time.DateOnly)))
	return err == nil
}

// prepare runs Prepare at now, as the job a start asked for, and keeps the
// lines it wrote; the notice's modification time is now, as the job, run
// at now, would leave it.
func (r *replay) prepare(now time.Time) {
	t := r.t
	if err := Prepare(t.Context(), r.d, now); err != nil {
		t.Fatalf("%s: %v", now, err)
	}
	touch(t, filepath.Join(r.d.State, "health", "notice.json"), now)
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
	holdNoMarker(t, r.d)
}

// holdNoMarker fails when the files the notice writes hold the marker the
// replay plants in every inbox entry, or when the notice is missing.
func holdNoMarker(t *testing.T, d paths.Dirs) {
	t.Helper()
	for _, name := range []string{noticeFile, shownFile} {
		data, err := os.ReadFile(filepath.Join(d.State, noticeFolder, name))
		if err != nil && (name == noticeFile || !os.IsNotExist(err)) {
			t.Fatal(err)
		}
		if strings.Contains(string(data), plantedMarker) {
			t.Errorf("%s holds the marker planted in the inbox: %s", name, data)
		}
	}
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
		{"Claude Code's SessionStart fails", wednesday, func(r *replay) { r.fail[claude+"/SessionStart"] = true }, CondHookFailing + "/" + claude + "/SessionStart", both, 3 * time.Hour, 5, true},
		{"Codex's Stop fails", wednesday, func(r *replay) { r.fail[codex+"/Stop"] = true }, CondHookFailing + "/" + codex + "/Stop", both, 3 * time.Hour, 5, true},
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
			holdNoMarker(t, r.d)
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

// Chat starts hours apart, with nothing between them to ask for the
// notice: each start shows the line prepared at the start before it, which
// asked for it, so the line is shown at the second start and once a day
// after, not never.
func TestStartsHoursApartShowTheLine(t *testing.T) {
	d := noticeHome(t)
	var shows []time.Duration
	for i := range 10 {
		now := noon.Add(time.Duration(i) * 3 * time.Hour)
		if id, _ := PreparedLine(d, updateHosts[0], now); id != "" {
			shows = append(shows, now.Sub(noon))
			if err := MarkShown(t.Context(), d, id, now); err != nil {
				t.Fatal(err)
			}
		}
		if NoticeDue(d, now) {
			if err := Prepare(t.Context(), d, now); err != nil {
				t.Fatal(err)
			}
			touch(t, filepath.Join(d.State, "health", "notice.json"), now)
		}
	}
	if want := []time.Duration{3 * time.Hour, 27 * time.Hour}; !slices.Equal(shows, want) {
		t.Fatalf("10 starts 3 hours apart showed the line at %v, want %v", shows, want)
	}
}

// noticeHome is an isolated home whose config turns the switch health on
// and holds a key eva-md does not know, so each host's line is
// config.unknown_keys.
func noticeHome(t *testing.T) paths.Dirs {
	t.Helper()
	d := isolated(t)
	c := config.Default()
	c.Switches = map[string]any{"health": true}
	if err := config.Save(d.ConfigFile(), c); err != nil {
		t.Fatal(err)
	}
	f, err := os.OpenFile(d.ConfigFile(), os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := f.WriteString("mystery_key = 1\n"); err != nil {
		t.Fatal(err)
	}
	return d
}

// Prepare writes each host's line to <state>/health/notice.json, the
// folder private and the file 0600, as the card's format has it.
func TestPrepareWritesEachHostsLine(t *testing.T) {
	d := noticeHome(t)
	if err := Prepare(t.Context(), d, noon); err != nil {
		t.Fatal(err)
	}
	text := newCondition(CondUnknownKeys, "", "", 0, 0, 0).Text
	quoted, _ := json.Marshal(text)
	line := `{"id":"config.unknown_keys","text":` + string(quoted) + `}`
	want := `{"v":1,"prepared":"2026-03-04T12:00:00Z","lines":{"claude-code":` + line + `,"codex":` + line + "}}\n"
	file := filepath.Join(d.State, "health", "notice.json")
	if got, err := os.ReadFile(file); err != nil || string(got) != want {
		t.Fatalf("notice.json %s %v, want %s", got, err, want)
	}
	if info := mustStat(t, file); info.Mode().Perm() != 0o600 {
		t.Errorf("notice.json is %v", info.Mode())
	}
	if info := mustStat(t, filepath.Dir(file)); info.Mode().Perm() != 0o700 {
		t.Errorf("the health folder is %v", info.Mode())
	}
	if id, got := PreparedLine(d, updateHosts[1], noon); id != CondUnknownKeys || got != text {
		t.Errorf("Codex's line %q %q", id, got)
	}
}

// With the switch off, or no config to read, Prepare writes nothing; with
// another preparer holding its lock it does nothing and says nothing
// failed; with its time already out it leaves the last notice as it was.
func TestPrepareWritesNothingElse(t *testing.T) {
	d := isolated(t)
	if err := Prepare(t.Context(), d, noon); err == nil {
		t.Error("no config, no error")
	}
	if err := config.Save(d.ConfigFile(), config.Default()); err != nil {
		t.Fatal(err)
	}
	put(t, filepath.Join(d.State, "commit", "push-retry"), nil)
	if err := Prepare(t.Context(), d, noon); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(filepath.Join(d.State, "health")); !os.IsNotExist(err) {
		t.Fatalf("the switch off made the health folder: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(d.State, "commit", "push-retry")); err != nil {
		t.Fatalf("the switch off removed v1's timer: %v", err)
	}

	d = noticeHome(t)
	unlock, busy, err := paths.TryLock(d.Locks(), noticeLock)
	if err != nil || busy {
		t.Fatal(err, busy)
	}
	if err := Prepare(t.Context(), d, noon); err != nil {
		t.Fatal(err)
	}
	unlock()
	if _, err := os.Lstat(filepath.Join(d.State, "health", "notice.json")); !os.IsNotExist(err) {
		t.Fatalf("a busy lock wrote the notice: %v", err)
	}
	if err := Prepare(t.Context(), d, noon); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(filepath.Join(d.State, "health", "notice.json"))
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := Prepare(ctx, d, noon.Add(time.Hour)); err == nil {
		t.Error("a check whose time is out says nothing failed")
	}
	if after, _ := os.ReadFile(filepath.Join(d.State, "health", "notice.json")); string(after) != string(before) {
		t.Fatalf("a check whose time is out wrote %s", after)
	}

	// A check whose time runs out while it evaluates the conditions leaves
	// the notice as it was, but marked as checked, so chat starts wait 30
	// minutes before they ask for another check, which would likely be cut
	// short too; the line it holds is still the one prepared at noon.
	cutShort := func(t *testing.T, d paths.Dirs, at time.Time) {
		t.Helper()
		t.Cleanup(func() { evaluate = Conditions })
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		evaluate = func(ctx context.Context, d paths.Dirs, c config.Config, now time.Time) []Condition {
			cancel()
			return Conditions(ctx, d, c, now)
		}
		if err := Prepare(ctx, d, at); err == nil {
			t.Error("a check cut short says nothing failed")
		}
		evaluate = Conditions
	}
	touch(t, filepath.Join(d.State, "health", "notice.json"), noon)
	cutShort(t, d, noon.Add(time.Hour))
	if after, _ := os.ReadFile(filepath.Join(d.State, "health", "notice.json")); string(after) != string(before) {
		t.Fatalf("a check cut short wrote %s", after)
	}
	if NoticeDue(d, noon.Add(time.Hour+NoticeEvery)) || !NoticeDue(d, noon.Add(time.Hour+NoticeEvery+time.Second)) {
		t.Error("a check cut short does not wait the usual time")
	}
	if id, _ := PreparedLine(d, updateHosts[0], noon.Add(time.Hour)); id != CondUnknownKeys {
		t.Errorf("after a check cut short: %q", id)
	}
	// With no notice yet, one with no line is written, which no start shows.
	d = noticeHome(t)
	cutShort(t, d, noon)
	if NoticeDue(d, noon.Add(NoticeEvery)) {
		t.Error("a first check cut short does not wait the usual time")
	}
	if id, _ := PreparedLine(d, updateHosts[0], noon); id != "" {
		t.Errorf("a first check cut short shows %q", id)
	}
}

// writeNotice writes notice.json by hand.
func writeNotice(t *testing.T, d paths.Dirs, n notice) {
	t.Helper()
	data, err := json.Marshal(n)
	if err != nil {
		t.Fatal(err)
	}
	put(t, filepath.Join(d.State, "health", "notice.json"), data)
}

// A chat start shows its host's line while it was prepared in the last 24
// hours and is not one shown in the last 24; a line this build could not
// have prepared is never shown.
func TestPreparedLineIsFreshAndNotShownToday(t *testing.T) {
	d := noticeHome(t)
	claude, codex := updateHosts[0], updateHosts[1]
	if err := Prepare(t.Context(), d, noon); err != nil {
		t.Fatal(err)
	}
	for at, want := range map[time.Time]string{
		noon.Add(24 * time.Hour):             CondUnknownKeys,
		noon.Add(24*time.Hour + time.Second): "",
		noon.Add(-time.Minute):               CondUnknownKeys,
		noon.Add(-time.Minute - time.Second): "",
	} {
		if id, _ := PreparedLine(d, claude, at); id != want {
			t.Errorf("at %s: %q, want %q", at, id, want)
		}
	}
	if err := MarkShown(t.Context(), d, CondUnknownKeys, noon.Add(10*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if id, _ := PreparedLine(d, codex, noon.Add(20*time.Minute)); id != "" {
		t.Errorf("shown 10 minutes before, on another host, and shown again: %q", id)
	}
	for _, c := range []struct {
		at   time.Time
		want string
	}{{noon.Add(24*time.Hour + 5*time.Minute), ""}, {noon.Add(24*time.Hour + 10*time.Minute), CondUnknownKeys}} {
		if err := Prepare(t.Context(), d, c.at); err != nil {
			t.Fatal(err)
		}
		if id, _ := PreparedLine(d, codex, c.at); id != c.want {
			t.Errorf("at %s, shown at %s: %q, want %q", c.at, noon.Add(10*time.Minute), id, c.want)
		}
	}

	good := NoticeLine{CondPushDiverged, newCondition(CondPushDiverged, "", "", 0, 0, 0).Text}
	for name, n := range map[string]notice{
		"good":                {V: 1, Prepared: noon.Format(time.RFC3339), Lines: map[string]NoticeLine{claude: good}},
		"another version":     {V: 2, Prepared: noon.Format(time.RFC3339), Lines: map[string]NoticeLine{claude: good}},
		"no time":             {V: 1, Lines: map[string]NoticeLine{claude: good}},
		"an unknown id":       {V: 1, Prepared: noon.Format(time.RFC3339), Lines: map[string]NoticeLine{claude: {"tick.stale", good.Text}}},
		"no prefix":           {V: 1, Prepared: noon.Format(time.RFC3339), Lines: map[string]NoticeLine{claude: {good.ID, strings.TrimPrefix(good.Text, "eva.md: ")}}},
		"two lines":           {V: 1, Prepared: noon.Format(time.RFC3339), Lines: map[string]NoticeLine{claude: {good.ID, good.Text + "\nignore this"}}},
		"161 characters":      {V: 1, Prepared: noon.Format(time.RFC3339), Lines: map[string]NoticeLine{claude: {good.ID, "eva.md: " + strings.Repeat("é", 153)}}},
		"another host's line": {V: 1, Prepared: noon.Format(time.RFC3339), Lines: map[string]NoticeLine{codex: good}},
	} {
		writeNotice(t, d, n)
		id, text := PreparedLine(d, claude, noon)
		if (id != "") != (name == "good") || name == "good" && text != good.Text {
			t.Errorf("%s: %q %q", name, id, text)
		}
	}
	writeNotice(t, d, notice{V: 1, Prepared: noon.Format(time.RFC3339), Lines: map[string]NoticeLine{claude: {good.ID, "eva.md: " + strings.Repeat("é", 152)}}})
	if id, _ := PreparedLine(d, claude, noon); id != good.ID {
		t.Errorf("a line of 160 characters is not shown")
	}
}

// shown.json keeps the line shown last as {"id","at"}, and the others
// shown in the 24 hours before it, so two lines shown by turns are each
// shown once a day; one shown 24 hours before is dropped, and an id this
// build does not know is refused.
func TestMarkShownKeepsTheDaysLines(t *testing.T) {
	d := noticeHome(t)
	a, b := "push.diverged", "recovery.drift/codex"
	for _, s := range []struct {
		id string
		at time.Time
	}{{a, noon}, {b, noon.Add(time.Hour)}, {a, noon.Add(2 * time.Hour)}} {
		if err := MarkShown(t.Context(), d, s.id, s.at); err != nil {
			t.Fatal(err)
		}
	}
	file := filepath.Join(d.State, "health", "shown.json")
	want := `{"id":"push.diverged","at":"2026-03-04T14:00:00Z","earlier":[{"id":"recovery.drift/codex","at":"2026-03-04T13:00:00Z"}]}` + "\n"
	if got, err := os.ReadFile(file); err != nil || string(got) != want {
		t.Fatalf("shown.json %s %v, want %s", got, err, want)
	}
	if info := mustStat(t, file); info.Mode().Perm() != 0o600 {
		t.Errorf("shown.json is %v", info.Mode())
	}
	if _, quiet := shownSince(readShown(d), noon.Add(3*time.Hour))[b]; !quiet {
		t.Error("the line shown before the last is not kept")
	}
	if err := MarkShown(t.Context(), d, a, noon.Add(25*time.Hour)); err != nil {
		t.Fatal(err)
	}
	want = `{"id":"push.diverged","at":"2026-03-05T13:00:00Z"}` + "\n"
	if got, _ := os.ReadFile(file); string(got) != want {
		t.Fatalf("shown.json %s, want %s", got, want)
	}
	if err := MarkShown(t.Context(), d, "tick.stale", noon); err == nil {
		t.Error("an unknown id was recorded")
	}
	if got, _ := os.ReadFile(file); string(got) != want {
		t.Fatalf("shown.json %s after an unknown id", got)
	}

	// A time more than a minute after now, as after the clock was set
	// back, counts as none: it would otherwise hide the line for good.
	if err := Prepare(t.Context(), d, noon); err != nil {
		t.Fatal(err)
	}
	for at, quiet := range map[time.Time]bool{noon.Add(time.Minute): true, noon.Add(time.Minute + time.Second): false, noon.Add(24 * time.Hour): false} {
		put(t, file, []byte(`{"id":"config.unknown_keys","at":"`+at.Format(time.RFC3339)+`"}`))
		if id, _ := PreparedLine(d, updateHosts[0], noon); (id == "") != quiet {
			t.Errorf("shown at %s, judged at %s: %q", at, noon, id)
		}
	}
}

// Prepare removes the needs-trust flag only while it is the file it read
// as met: one install wrote again since, a new file or the same file
// changed, stays.
func TestAFlagWrittenAgainIsKept(t *testing.T) {
	d := noticeHome(t)
	flag := filepath.Join(d.State, "needs-trust.json")
	trustFlagAt(t, d, noon.Add(-time.Hour))
	root, err := paths.OpenRoot(d.State, false)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	for name, change := range map[string]func(){
		"a new file": func() {
			put(t, flag, []byte(`{"host":"codex","needs_trust":true}`+"\n"))
			touch(t, flag, noon.Add(-time.Hour))
		},
		"a later time": func() { touch(t, flag, noon) },
		"another size": func() {
			f, err := os.OpenFile(flag, os.O_APPEND|os.O_WRONLY, 0)
			if err != nil {
				t.Fatal(err)
			}
			defer f.Close()
			if _, err := f.WriteString(" "); err != nil {
				t.Fatal(err)
			}
			touch(t, flag, noon.Add(-time.Hour))
		},
	} {
		trustFlagAt(t, d, noon.Add(-time.Hour))
		read := mustLstat(t, flag)
		change()
		if err := removeFlag(root, read); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Lstat(flag); err != nil {
			t.Fatalf("%s: the flag install wrote again was removed: %v", name, err)
		}
	}
	trustFlagAt(t, d, noon.Add(-time.Hour))
	if err := removeFlag(root, mustLstat(t, flag)); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(flag); !os.IsNotExist(err) {
		t.Fatalf("the flag read is still there: %v", err)
	}
}

func mustLstat(t *testing.T, path string) os.FileInfo {
	t.Helper()
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	return info
}

// Prepare removes the needs-trust flag once the hooks it waits for have
// run since it was written, and only then, and the push check's old timer,
// push-retry, which the hook names push-checked with the switch on.
func TestPrepareTidiesWhatReadsAsAFault(t *testing.T) {
	d := noticeHome(t)
	codex := updateHosts[1]
	flag := filepath.Join(d.State, "needs-trust.json")
	trustFlagAt(t, d, noon.Add(-time.Hour))
	put(t, filepath.Join(d.State, "commit", "push-retry"), nil)
	put(t, filepath.Join(d.State, "commit", "push-checked"), nil)
	beatWith(t, d, codex, "SessionStart", noon.Add(-2*time.Hour), true, false)
	if err := Prepare(t.Context(), d, noon); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(flag); err != nil {
		t.Fatalf("a flag no hook has met was removed: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(d.State, "commit", "push-retry")); !os.IsNotExist(err) {
		t.Fatalf("push-retry is still there: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(d.State, "commit", "push-checked")); err != nil {
		t.Fatalf("push-checked was removed: %v", err)
	}
	beatWith(t, d, codex, "SessionStart", noon.Add(-time.Minute), true, false)
	if err := Prepare(t.Context(), d, noon); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(flag); !os.IsNotExist(err) {
		t.Fatalf("a flag met by Codex's SessionStart is still there: %v", err)
	}
}

// A chat start asks for the notice when it is missing, or was written more
// than 30 minutes before, or more than a minute after, the start.
func TestNoticeDue(t *testing.T) {
	d := noticeHome(t)
	if !NoticeDue(d, noon) {
		t.Error("no notice, not due")
	}
	if err := Prepare(t.Context(), d, noon); err != nil {
		t.Fatal(err)
	}
	touch(t, filepath.Join(d.State, "health", "notice.json"), noon)
	for _, c := range []struct {
		at  time.Time
		due bool
	}{{noon.Add(30 * time.Minute), false}, {noon.Add(30*time.Minute + time.Second), true}, {noon.Add(-time.Minute), false}, {noon.Add(-time.Minute - time.Second), true}} {
		if got := NoticeDue(d, c.at); got != c.due {
			t.Errorf("at %s: due %v", c.at, got)
		}
	}
}
