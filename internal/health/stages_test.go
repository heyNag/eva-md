package health

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/heyNag/eva-md/internal/config"
	"github.com/heyNag/eva-md/internal/inbox"
	"github.com/heyNag/eva-md/internal/paths"
	"github.com/heyNag/eva-md/internal/update"
	"github.com/heyNag/eva-md/internal/wikigit"
)

// Each stage's lag, measured from the oldest thing still waiting at it and
// never from a file's date, beside its bar, on fixture homes with the
// switch health on.

// stageHome is an isolated home whose config names a wiki, a git
// repository with no commit, and turns the switch health on.
func stageHome(t *testing.T) (paths.Dirs, config.Config) {
	t.Helper()
	d := isolated(t)
	wiki := filepath.Join(os.Getenv("HOME"), "wiki")
	gitIn(t, "", "init", "-q", wiki)
	wiki, err := filepath.EvalSymlinks(wiki)
	if err != nil {
		t.Fatal(err)
	}
	c := config.Default()
	c.WikiPath, c.Instance = wiki, "test"
	c.Switches = map[string]any{"health": true}
	if err := config.Save(d.ConfigFile(), c); err != nil {
		t.Fatal(err)
	}
	return d, c
}

// gitIn runs git, in dir when it is not empty, with a made-up identity and
// no signing.
func gitIn(t *testing.T, dir string, args ...string) {
	t.Helper()
	argv := append([]string{"-c", "user.name=Test", "-c", "user.email=test@example.invalid", "-c", "commit.gpgsign=false"}, args...)
	if dir != "" {
		argv = append([]string{"-C", dir}, argv...)
	}
	if out, err := exec.Command("git", argv...).CombinedOutput(); err != nil {
		t.Fatalf("git %q: %v %s", args, err, out)
	}
}

// inUse makes both hosts used: a hook of each ran a minute before now.
func inUse(t *testing.T, d paths.Dirs, now time.Time) {
	t.Helper()
	for _, host := range updateHosts {
		beatWith(t, d, host, "Stop", now.Add(-time.Minute), true, false)
	}
}

// statusAt is eva-md status's rows, judged at now.
func statusAt(t *testing.T, d paths.Dirs, now time.Time) Report {
	t.Helper()
	return rows(&inspection{ctx: t.Context(), d: d, status: true, at: now})
}

// rowNamed finds a row of r by name, or reports false.
func rowNamed(r Report, name string) (Check, bool) {
	i := slices.IndexFunc(r.Checks, func(c Check) bool { return c.Name == name })
	if i < 0 {
		return Check{}, false
	}
	return r.Checks[i], true
}

// chatLines are n prompts of a session in the folder general, a minute
// apart from at, each its own turn, each with its reply when replies is
// true; every text holds plantedMarker.
func chatLines(t *testing.T, host, session string, at time.Time, n int, replies bool) []byte {
	t.Helper()
	var data []byte
	add := func(e inbox.Entry) {
		e.V, e.Host, e.Session, e.Project, e.Origin = 1, host, session, "general", "hook"
		line, err := json.Marshal(e)
		if err != nil {
			t.Fatal(err)
		}
		data = append(append(data, line...), '\n')
	}
	for i := range n {
		ts := at.Add(time.Duration(i) * time.Minute).Format(time.RFC3339)
		turn := fmt.Sprintf("turn-%d", i)
		add(inbox.Entry{TS: ts, Turn: turn, Role: "user", Kind: "prompt", Text: "a prompt that holds " + plantedMarker})
		if replies {
			add(inbox.Entry{TS: ts, Turn: turn, Role: "assistant", Kind: "reply", Text: "a reply that holds " + plantedMarker})
		}
	}
	return data
}

// plantSession writes a session file of the folder general, with its watermark
// at done bytes when done is not negative, and sets its modification time
// to mod. It returns the file.
func plantSession(t *testing.T, d paths.Dirs, host, id string, data []byte, done int64, mod time.Time) string {
	t.Helper()
	store := inbox.Store{Root: d.Inbox()}
	file := store.Path("general", host, id)
	put(t, file, data)
	if done >= 0 {
		if err := store.SetMark(file, inbox.Mark{Offset: done}); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Chtimes(file, mod, mod); err != nil {
		t.Fatal(err)
	}
	return file
}

// The organizing lag runs from the oldest entry past its session's
// watermark, not from the file's date: a file touched ten minutes ago whose
// oldest waiting entry is two days old reads two days, though it is the
// 65th newest file of its folder, which v1's backlog row does not read.
// What waits but is counted apart is said beside the lag.
func TestOrganizingLagIsTheOldestWaitingEntrys(t *testing.T) {
	now := time.Now()
	d, _ := stageHome(t)
	inUse(t, d, now)
	for i := range 64 {
		data := chatLines(t, update.HostCodex, fmt.Sprintf("fresh%02d", i), now.Add(-2*time.Minute), 1, true)
		plantSession(t, d, update.HostCodex, fmt.Sprintf("fresh%02d", i), data, int64(len(data)), now.Add(-time.Minute))
	}
	plantSession(t, d, update.HostCodex, "old", chatLines(t, update.HostCodex, "old", now.Add(-50*time.Hour), 2, true), 0, now.Add(-10*time.Minute))
	// A session whose entries carry no time is counted apart.
	plantSession(t, d, update.HostCodex, "untimed", []byte("{}\n"), 0, now.Add(-time.Hour))

	r := statusAt(t, d, now)
	row := findRow(t, r, update.HostCodex+" organizing lag")
	want := "lag 2 days, bar 5 minutes, past its bar and its budget of 24 hours; 2 sessions wait; 1 waiting session has no entry with a time, so it is not in the lag"
	if row.Status != "FAIL" || !strings.HasPrefix(row.Detail, want) || row.Fix == "" {
		t.Fatalf("organizing row %+v, want FAIL starting %q", row, want)
	}
	if backlog := findRow(t, r, "update backlog"); strings.Contains(backlog.Detail, "2 sessions") {
		t.Fatalf("v1's backlog row read past a folder's 64 newest files: %+v", backlog)
	}
	if row := findRow(t, r, update.HostClaude+" organizing lag"); row.Status != "PASS" || row.Detail != "no lag, bar 5 minutes; no session waits" {
		t.Fatalf("a host with nothing waiting: %+v", row)
	}
}

// A watermark past the end of its file is counted apart, as v1's backlog
// row names it.
func TestOrganizingCountsADamagedWatermarkApart(t *testing.T) {
	now := time.Now()
	d, _ := stageHome(t)
	inUse(t, d, now)
	file := plantSession(t, d, update.HostCodex, "damaged", chatLines(t, update.HostCodex, "damaged", now.Add(-time.Hour), 1, true), 0, now)
	data, _ := os.ReadFile(file)
	if err := (inbox.Store{Root: d.Inbox()}).SetMark(file, inbox.Mark{Offset: int64(len(data)) + 100}); err != nil {
		t.Fatal(err)
	}
	row := findRow(t, statusAt(t, d, now), update.HostCodex+" organizing lag")
	if row.Status != "PASS" || !strings.Contains(row.Detail, "1 session has a watermark that can't be read or is past the end of its file") {
		t.Fatalf("organizing row %+v", row)
	}
}

// The capture lag runs from the host's newest hook run while its chat
// folder holds a chat newer than that run by more than the bar: a chat
// folder changed an hour ago, after a run two days ago, reads two days and
// fails; a chat within the bar of the run reads no lag.
func TestCaptureLagRunsFromTheNewestHookRun(t *testing.T) {
	now := time.Now()
	d, _ := stageHome(t)
	installedFor(t, d, update.HostClaude, now.Add(-10*24*time.Hour))
	beatWith(t, d, update.HostClaude, "Stop", now.Add(-49*time.Hour), true, false)
	project := filepath.Join(os.Getenv("HOME"), ".claude", "projects", "a-project")
	chatFolder(t, project, now.Add(-time.Hour))

	row := findRow(t, statusAt(t, d, now), update.HostClaude+" capture lag")
	want := "lag 2 days, bar 5 minutes, past its bar and its budget of 1 hour; the host's newest chat is newer than its newest hook run"
	if row.Status != "FAIL" || row.Detail != want {
		t.Fatalf("capture row %+v, want FAIL %q", row, want)
	}

	beatWith(t, d, update.HostClaude, "Stop", now.Add(-time.Hour-2*time.Minute), true, false)
	if row := findRow(t, statusAt(t, d, now), update.HostClaude+" capture lag"); row.Status != "PASS" || row.Detail != "no lag, bar 5 minutes; no chat newer than the host's newest hook run" {
		t.Fatalf("a chat within the bar of the run: %+v", row)
	}
}

// commitFixture plants a wiki file not yet committed, as an update
// writes one, touched now.
func commitFixture(t *testing.T, c config.Config, now time.Time) {
	t.Helper()
	file := filepath.Join(c.WikiPath, "projects", "a", "threads", "main.md")
	put(t, file, []byte("a page\n"))
	if err := os.Chtimes(file, now, now); err != nil {
		t.Fatal(err)
	}
}

// The commit lag runs from the oldest update that saved pages after the
// newest commit run that committed or found nothing, while the wiki holds
// files not yet committed: a last commit three weeks ago with files
// waiting fails; files written 30 hours ago, though touched now, pass past
// the bar; with no file waiting there is no lag, whatever the records say.
func TestCommitLag(t *testing.T) {
	now := time.Now()
	at := func(d time.Duration) string { return now.Add(-d).Format(time.RFC3339) }

	d, c := stageHome(t)
	commitFixture(t, c, now)
	plantCommits(t, d, wikigit.Result{At: at(21 * 24 * time.Hour), Outcome: wikigit.Committed, Pages: 2, Push: wikigit.PushOK})
	row := findRow(t, statusAt(t, d, now), "commit lag")
	if row.Status != "FAIL" || !strings.HasPrefix(row.Detail, "lag 21 days, bar 24 hours, past its bar and its budget of 2 days; 1 file written and not yet committed") || row.Fix != "eva-md commit commits them now" {
		t.Fatalf("a commit three weeks old: %+v", row)
	}

	d, c = stageHome(t)
	commitFixture(t, c, now)
	plantCommits(t, d, wikigit.Result{At: at(40 * time.Hour), Outcome: wikigit.Nothing, Push: wikigit.PushSkipped})
	plantRunLog(t, d,
		update.Record{At: at(39 * time.Hour), Host: update.HostCodex, Outcome: update.OutcomeNoChange},
		update.Record{At: at(30 * time.Hour), Host: update.HostCodex, Outcome: update.OutcomeOK, Pages: 1},
		update.Record{At: at(2 * time.Hour), Host: update.HostCodex, Outcome: update.OutcomeOK, Pages: 3})
	row = findRow(t, statusAt(t, d, now), "commit lag")
	if row.Status != "PASS" || row.Detail != "lag 30 hours, bar 24 hours, past its bar; 1 file written and not yet committed" {
		t.Fatalf("files written 30 hours ago: %+v", row)
	}

	// The same records with every file committed: no lag.
	gitIn(t, c.WikiPath, "add", "-A")
	gitIn(t, c.WikiPath, "commit", "-q", "-m", "pages")
	row = findRow(t, statusAt(t, d, now), "commit lag")
	if row.Status != "PASS" || row.Detail != "no lag, bar 24 hours; 0 files written and not yet committed" {
		t.Fatalf("nothing waiting: %+v", row)
	}
}

// The push lag runs from the oldest commit run whose push has not
// succeeded since, while the wiki holds commits its remote lacks: a commit
// four days ago left pending fails, one an hour ago passes past the bar,
// and a later good push ends the lag.
func TestPushLag(t *testing.T) {
	now := time.Now()
	at := func(d time.Duration) string { return now.Add(-d).Format(time.RFC3339) }
	d, c := stageHome(t)
	commitFixture(t, c, now)
	gitIn(t, c.WikiPath, "add", "-A")
	gitIn(t, c.WikiPath, "commit", "-q", "-m", "pages")
	plantCommits(t, d,
		wikigit.Result{At: at(5 * 24 * time.Hour), Outcome: wikigit.Committed, Pages: 1, Push: wikigit.PushOK},
		wikigit.Result{At: at(4 * 24 * time.Hour), Outcome: wikigit.Committed, Pages: 1, Push: wikigit.PushPending},
		wikigit.Result{At: at(3 * 24 * time.Hour), Outcome: wikigit.Committed, Pages: 1, Push: wikigit.PushFailed})
	row := findRow(t, statusAt(t, d, now), "push lag")
	if row.Status != "FAIL" || row.Detail != "lag 4 days, bar 10 minutes, past its bar and its budget of 3 days; 1 commit not yet pushed" {
		t.Fatalf("a push pending for four days: %+v", row)
	}

	plantCommits(t, d, wikigit.Result{At: at(2 * time.Hour), Outcome: wikigit.Nothing, Push: wikigit.PushOK},
		wikigit.Result{At: at(time.Hour), Outcome: wikigit.Committed, Pages: 1, Push: wikigit.PushPending})
	row = findRow(t, statusAt(t, d, now), "push lag")
	if row.Status != "PASS" || row.Detail != "lag 1 hour, bar 10 minutes, past its bar; 1 commit not yet pushed" {
		t.Fatalf("a push pending for an hour: %+v", row)
	}
}

// Stages gives each host's activity, capture and organizing, then the
// wiki's commit and push; without a wiki only the first two of each host.
// A stage whose evidence can't be read says which, and its row is UNKNOWN.
func TestStagesAndWhatTheyCannotRead(t *testing.T) {
	now := time.Now()
	d, c := stageHome(t)
	var got []string
	for _, s := range Stages(t.Context(), d, c, now) {
		got = append(got, s.Host+"/"+s.Name)
	}
	want := []string{
		update.HostClaude + "/activity", update.HostClaude + "/capture", update.HostClaude + "/organizing",
		update.HostCodex + "/activity", update.HostCodex + "/capture", update.HostCodex + "/organizing",
		"/commit", "/push"}
	if !slices.Equal(got, want) {
		t.Fatalf("stages %q, want %q", got, want)
	}
	c.WikiPath = ""
	if n := len(Stages(t.Context(), d, c, now)); n != 4 {
		t.Fatalf("without a wiki: %d stages", n)
	}

	// The wiki's git state can't be read: a folder that is no repository.
	d, c = stageHome(t)
	if err := os.RemoveAll(filepath.Join(c.WikiPath, ".git")); err != nil {
		t.Fatal(err)
	}
	r := statusAt(t, d, now)
	for _, name := range []string{"commit lag", "push lag"} {
		if row := findRow(t, r, name); row.Status != "UNKNOWN" || !strings.Contains(row.Detail, "git could not read the wiki") {
			t.Fatalf("%s %+v", name, row)
		}
	}

	// A chat folder that can't be listed leaves the capture lag unknown,
	// and the host is not taken for one nobody uses.
	d, _ = stageHome(t)
	projects := filepath.Join(os.Getenv("HOME"), ".claude", "projects")
	chatFolder(t, filepath.Join(projects, "a-project"), now.Add(-time.Hour))
	shut(t, projects)
	r = statusAt(t, d, now)
	if row := findRow(t, r, update.HostClaude+" capture lag"); row.Status != "UNKNOWN" || !strings.Contains(row.Detail, "could not be read whole") {
		t.Fatalf("capture lag %+v", row)
	}
	if _, ok := rowNamed(r, "Claude Code"); ok {
		t.Fatal("a host whose chat folder can't be read was taken for one nobody uses")
	}
}

// A host nobody uses, with no chat and no hook run in 14 days and no
// install in the last day, has one PASS row in place of all of its own,
// none of them UNKNOWN; a day after its install it has its rows again. The
// other host keeps its rows.
func TestAHostNobodyUsesHasOneRow(t *testing.T) {
	now := time.Now()
	d, _ := stageHome(t)
	// installedFor dates the install 12 hours before the time it is given.
	installedFor(t, d, update.HostCodex, now.Add(-24*time.Hour))
	beatWith(t, d, update.HostCodex, "Stop", now.Add(-15*24*time.Hour), true, false)
	beatWith(t, d, update.HostClaude, "Stop", now.Add(-13*24*time.Hour), true, false)
	day := codexDay(now.Add(-20 * 24 * time.Hour))
	chatFolder(t, day, now.Add(-20*24*time.Hour))

	for _, r := range []Report{statusAt(t, d, now), rows(&inspection{ctx: t.Context(), d: d, hosts: true, at: now})} {
		one, ok := rowNamed(r, "Codex")
		if !ok || one.Status != "PASS" || one.Detail != "not used in the last 14 days" {
			t.Fatalf("no row for a host nobody uses: %+v", r.Checks)
		}
		for _, c := range r.Checks {
			if strings.HasPrefix(c.Name, update.HostCodex+" ") {
				t.Fatalf("a host nobody uses kept its row %+v", c)
			}
		}
		// Claude Code ran a hook 13 days ago: it keeps its rows.
		if _, ok := rowNamed(r, update.HostClaude+" capture lag"); !ok {
			t.Fatal("a host used in the last 14 days lost its rows")
		}
	}

	installedFor(t, d, update.HostCodex, now.Add(-11*time.Hour))
	r := statusAt(t, d, now)
	if _, ok := rowNamed(r, "Codex"); ok {
		t.Fatal("a host installed in the last day was taken for one nobody uses")
	}
	if _, ok := rowNamed(r, update.HostCodex+" capture lag"); !ok {
		t.Fatal("a host installed in the last day has no capture row")
	}

	// A chat in its folder in the last 14 days makes it used too.
	installedFor(t, d, update.HostCodex, now.Add(-24*time.Hour))
	chatFolder(t, codexDay(now.Add(-24*time.Hour)), now.Add(-24*time.Hour))
	if _, ok := rowNamed(statusAt(t, d, now), "Codex"); ok {
		t.Fatal("a host with a chat 1 day ago was taken for one nobody uses")
	}
}

// With the switch off, or set false, nothing of this package's is added:
// no stage row, no loss row, no verdict, and a host nobody uses keeps its
// rows.
func TestSwitchOffAddsNoStage(t *testing.T) {
	now := time.Now()
	for _, set := range []bool{false, true} {
		d, c := stageHome(t)
		c.Switches = nil
		if set {
			c.Switches = map[string]any{"health": false}
		}
		if err := config.Save(d.ConfigFile(), c); err != nil {
			t.Fatal(err)
		}
		r := statusAt(t, d, now)
		if r.Verdict != "" {
			t.Fatalf("a verdict with the switch off: %q", r.Verdict)
		}
		for _, c := range r.Checks {
			if strings.HasSuffix(c.Name, " lag") || strings.HasSuffix(c.Name, " capture losses") || c.Name == "Codex" || c.Name == "Claude Code" {
				t.Fatalf("a row with the switch off: %+v", c)
			}
		}
		if _, ok := rowNamed(r, update.HostCodex+" capture freshness"); !ok {
			t.Fatal("the switch off lost a v1 row")
		}
	}
}

func TestSpan(t *testing.T) {
	for d, want := range map[time.Duration]string{
		0: "under a minute", 59 * time.Second: "under a minute", time.Minute: "1 minute", 5 * time.Minute: "5 minutes",
		time.Hour: "1 hour", 47*time.Hour + 59*time.Minute: "47 hours", 48 * time.Hour: "2 days", 21*24*time.Hour + 5*time.Hour: "21 days",
	} {
		if got := span(d); got != want {
			t.Errorf("span(%v) = %q, want %q", d, got, want)
		}
	}
}
