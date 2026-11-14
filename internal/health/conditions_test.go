package health

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/heyNag/eva-md/internal/config"
	"github.com/heyNag/eva-md/internal/paths"
	"github.com/heyNag/eva-md/internal/update"
	"github.com/heyNag/eva-md/internal/wikigit"
)

// The conditions a chat start may say, evaluated from v1's evidence and the
// stages, with the switch health on.

// noon is the time the conditions are judged at.
var noon = time.Date(2026, 3, 4, 12, 0, 0, 0, time.UTC)

// Every line, as a chat start shows it: the golden of each type, and, for
// every host, event and the largest counts and waits the evidence gives,
// a line of at most 160 characters that starts "eva.md: ", never says to
// run the doctor, has an id this build knows and the severity of its place
// in the list.
func TestEveryLine(t *testing.T) {
	claude, codex := updateHosts[0], updateHosts[1]
	for _, c := range []struct {
		typ, host, event string
		n, of            int
		wait             time.Duration
		want             string
	}{
		{CondUnknownKeys, "", "", 0, 0, 0, "eva.md: its config has keys it does not know, which it ignores, so a setting may be lost. Correct or remove them in eva-md's config.toml."},
		{CondWikiUnreadable, "", "", 0, 0, 0, "eva.md: the wiki's index.md cannot be read, so briefs and updates leave the wiki out. Restore index.md at the wiki's root."},
		{CondWikiInstance, "", "", 0, 0, 0, "eva.md: the wiki is another install's, so nothing is written to it. Point wiki_path in eva-md's config.toml at this Mac's own wiki."},
		{CondTrustReview, codex, "", 0, 0, 0, "eva.md: Codex chats are not saved until Codex approves eva-md's hooks. In Codex, type /hooks and approve eva-md."},
		{CondCaptureSilent, claude, "", 0, 0, 3*time.Hour + 10*time.Minute, "eva.md: Claude Code's hooks have not run for 3 hours while it was in use, so its chats are not saved. Enable eva-md's plugin in Claude Code."},
		{CondHookFailing, claude, "SessionStart", 4, 4, 0, "eva.md: 4 of the last 4 Claude Code SessionStart hooks failed: they timed out or could not save. Free disk space and check eva-md's state folder."},
		{CondUpdateSignIn, claude, "", 0, 0, 0, "eva.md: Claude Code chats are not turned into notes while Claude Code is signed out. Run claude auth login."},
		{CondUpdateSignIn, codex, "", 0, 0, 0, "eva.md: Codex chats are not turned into notes while Codex is signed out. Run codex login."},
		{CondPushDiverged, "", "", 0, 0, 0, "eva.md: the wiki's remote has commits this clone lacks, so pushes stopped. Merge them in the wiki by hand, never with a force push."},
		{CondHookFailing, codex, "Stop", 10, 20, 0, "eva.md: 10 of the last 20 Codex Stop hooks failed: they timed out or could not save. Free disk space and check eva-md's state folder."},
		{CondUpdateFailing, codex, "", 3, 0, 0, "eva.md: the last 3 Codex updates failed, so its chats wait to become notes. Run eva-md update --pending --foreground to see why."},
		{CondUpdateQuarantined, claude, "", 1, 0, 0, "eva.md: 1 Claude Code chat failed 3 updates in a row and is tried once more tomorrow. eva-md status names it."},
		{CondUpdateQuarantined, claude, "", 2, 0, 0, "eva.md: 2 Claude Code chats failed 3 updates in a row and are tried once more tomorrow. eva-md status names them."},
		{CondSchemaNewer, "", "", 0, 0, 0, "eva.md: a newer eva-md set up the wiki, so chats are captured but nothing is written to the wiki. Run eva-md upgrade."},
		{CondRecoveryDrift, codex, "", 0, 0, 0, "eva.md: it no longer reads Codex's transcripts, so replies its hooks miss are not recovered. Run eva-md upgrade."},
		{CondCommitStale, "", "", 0, 0, 50 * time.Hour, "eva.md: the wiki's changes have waited 2 days to be committed. Run eva-md commit to commit them, or to see what stops it."},
		{CondPushStale, "", "", 0, 0, 73 * time.Hour, "eva.md: the wiki's commits have waited 3 days to be pushed. Run eva-md commit --push-only to push them, or to see what stops it."},
	} {
		if got := newCondition(c.typ, c.host, c.event, c.n, c.of, c.wait).Text; got != c.want {
			t.Errorf("%s:\n got %q\nwant %q", c.typ, got, c.want)
		}
	}
	types := map[string]bool{}
	for _, typ := range conditionTypes {
		hosts, events := []string{""}, []string{""}
		if slices.Contains(hostTypes, typ) {
			hosts = updateHosts
		}
		for _, host := range hosts {
			if typ == CondHookFailing {
				events = hostEvents(host)
			}
			for _, event := range events {
				for _, n := range []int{1, 2, 20, 999} {
					c := newCondition(typ, host, event, n, max(n, 20), 400*24*time.Hour)
					types[c.Type] = true
					if !strings.HasPrefix(c.Text, "eva.md: ") || utf8.RuneCountInString(c.Text) > maxLine || strings.Contains(c.Text, "doctor") || !validLine(c.Text) {
						t.Errorf("%s: %d characters: %q", c.ID, utf8.RuneCountInString(c.Text), c.Text)
					}
					if !knownID(c.ID) {
						t.Errorf("%s: an id this build does not know", c.ID)
					}
					high := slices.Index(conditionTypes, typ) <= slices.Index(conditionTypes, CondPushDiverged) && (typ != CondHookFailing || slices.Contains(startEvents, event))
					if (c.Severity == "high") != high || c.Severity != "high" && c.Severity != "medium" {
						t.Errorf("%s: severity %q", c.ID, c.Severity)
					}
				}
			}
		}
	}
	if len(types) != 14 || len(conditionTypes) != 14 {
		t.Errorf("%d types worded, %d in the list, want 14", len(types), len(conditionTypes))
	}
}

// An id names a type of the list, with a host for a type about one and an
// event of that host for a failing hook, and nothing more.
func TestKnownIDs(t *testing.T) {
	for id, want := range map[string]bool{
		"config.unknown_keys":               true,
		"push.stale":                        true,
		"capture.silent/codex":              true,
		"hook.failing/claude-code/Stop":     true,
		"hook.failing/codex/StopFailure":    false, // not a Codex event
		"hook.failing/codex":                false,
		"capture.silent":                    false,
		"capture.silent/claude":             false, // an install name, not an id
		"config.unknown_keys/codex":         false,
		"recovery.drift/codex/Stop":         false,
		"tick.stale":                        false,
		"":                                  false,
		"trust.review/codex/../../etc":      false,
		"update.signin/claude-code ":        false,
		"update.quarantined/claude-code/x/": false,
	} {
		if got := knownID(id); got != want {
			t.Errorf("knownID(%q) = %v", id, got)
		}
	}
}

// A host's line is the first relevant condition, but its own
// capture.silent and trust.review, which its running hook disproves; the
// other host's are its line.
func TestLineForLeavesOutWhatTheHooksRunDisproves(t *testing.T) {
	claude, codex := updateHosts[0], updateHosts[1]
	conds := []Condition{
		{ID: "trust.review/codex", Type: CondTrustReview, Host: codex, Relevant: true},
		{ID: "capture.silent/codex", Type: CondCaptureSilent, Host: codex, Relevant: true},
		{ID: "capture.silent/claude-code", Type: CondCaptureSilent, Host: claude, Relevant: true},
		{ID: "recovery.drift/codex", Type: CondRecoveryDrift, Host: codex, Relevant: false},
		{ID: "hook.failing/codex/Stop", Type: CondHookFailing, Host: codex, Relevant: true},
	}
	if c, _ := lineFor(conds, claude); c.ID != "trust.review/codex" {
		t.Errorf("Claude Code's line %q", c.ID)
	}
	if c, _ := lineFor(conds, codex); c.ID != "capture.silent/claude-code" {
		t.Errorf("Codex's line %q", c.ID)
	}
	if c, ok := lineFor(conds[3:4], codex); ok {
		t.Errorf("a condition that is not relevant is a line: %q", c.ID)
	}
	if c, _ := lineFor(conds[:2], codex); c.ID != "" {
		t.Errorf("Codex's line from its own conditions %q", c.ID)
	}
}

// ids are the ids of conds, and of those relevant, in their order.
func ids(conds []Condition) (all, relevant []string) {
	for _, c := range conds {
		all = append(all, c.ID)
		if c.Relevant {
			relevant = append(relevant, c.ID)
		}
	}
	return all, relevant
}

// runsOf are n runs of a hook an hour apart up to at, the last failed of
// them failing.
func runsOf(at time.Time, n, failed int) []hookRun {
	var runs []hookRun
	for i := range n {
		runs = append(runs, hookRun{At: at.Add(-time.Duration(n-1-i) * time.Hour), Failed: i >= n-failed})
	}
	return runs
}

// Many conditions at once are evaluated in the list's order, the most
// severe first: a hook failing at a chat's start before a sign-in, a
// hook failing at any other event after a diverged push.
func TestConditionsInTheListsOrder(t *testing.T) {
	d, c := stageHome(t)
	claude, codex := updateHosts[0], updateHosts[1]
	c.Unknown = []string{"mystery_key"}
	if err := config.SaveSchema(filepath.Join(c.WikiPath, ".eva-md", "schema.toml"), config.WikiSchema{SchemaVersion: 2, Instance: "another", Created: "2026-03-01"}); err != nil {
		t.Fatal(err)
	}
	beatFile(t, d, claude, "SessionStart", noon, nil, runsOf(noon, 4, 2))
	beatFile(t, d, claude, "Stop", noon, nil, runsOf(noon, 6, 3))
	beatFile(t, d, codex, "Stop", noon, nil, runsOf(noon, 6, 2))
	put(t, filepath.Join(d.State, "catchup", "drift-"+codex+".json"), []byte(`{"v":1,"host":"codex"}`))
	at := func(d time.Duration) string { return noon.Add(-d).Format(time.RFC3339) }
	plantRunLog(t, d, failedRecord(at(3*time.Hour), codex, "s1", update.ReasonHostError), failedRecord(at(2*time.Hour), codex, "s1", update.ReasonHostError),
		failedRecord(at(time.Hour), codex, "s1", update.ReasonRateLimit), failedRecord(at(time.Hour), claude, "s2", update.ReasonAuth))
	plantCommits(t, d, wikigit.Result{At: at(time.Hour), Outcome: wikigit.Committed, Pages: 1, Push: wikigit.PushDiverged})
	all, _ := ids(Conditions(t.Context(), d, c, noon))
	want := []string{"config.unknown_keys", "wiki.unreadable", "wiki.instance", "hook.failing/claude-code/SessionStart", "update.signin/claude-code", "push.diverged",
		"hook.failing/claude-code/Stop", "update.failing/codex", "wiki.schema_newer", "recovery.drift/codex"}
	if !slices.Equal(all, want) {
		t.Fatalf("conditions\n %q\nwant\n %q", all, want)
	}
	for host, line := range map[string]string{claude: "config.unknown_keys", codex: "config.unknown_keys"} {
		if got, _ := lineFor(Conditions(t.Context(), d, c, noon), host); got.ID != line {
			t.Errorf("%s's line %q", host, got.ID)
		}
	}
}

// A host's events are judged each on its own runs, never pooled: a Stop
// that fails every time among many tool runs that pass is a condition
// (the audit's miss-2), and a few failures spread over the events are not.
func TestEachEventIsJudgedOnItsOwn(t *testing.T) {
	d := isolated(t)
	claude := updateHosts[0]
	beatFile(t, d, claude, "PostToolUse", noon, nil, runsOf(noon, 20, 0))
	beatFile(t, d, claude, "Stop", noon, nil, runsOf(noon, 4, 4))
	beatFile(t, d, claude, "UserPromptSubmit", noon, nil, runsOf(noon, 20, 9))
	beatFile(t, d, claude, "SessionStart", noon, nil, runsOf(noon, 3, 3))
	got := failingHooks(d, noon)
	if len(got) != 1 || got[0] != (failingHook{claude, "Stop", 4, 4}) {
		t.Fatalf("failing hooks %+v", got)
	}
	// Runs older than 7 days, or after now, are not judged.
	beatFile(t, d, claude, "Stop", noon, nil, append(runsOf(noon.Add(-8*24*time.Hour), 4, 4), runsOf(noon.Add(2*time.Hour), 4, 4)...))
	if got := failingHooks(d, noon); len(got) != 0 {
		t.Fatalf("failing hooks %+v", got)
	}
}

// Fault-matrix row 6: SessionStart fails every time from the install; the
// other events are fine. After 3 runs there is nothing to say; after 4 it
// is a high condition, the line of both hosts: the other host's chat
// start says it, and so does the same host's next prompt, whose hook ran.
func TestRowSixSessionStartFailsEveryTime(t *testing.T) {
	d := isolated(t)
	claude, codex := updateHosts[0], updateHosts[1]
	c := config.Default()
	beatFile(t, d, claude, "Stop", noon, nil, runsOf(noon, 4, 0))
	beatFile(t, d, claude, "SessionStart", noon, nil, runsOf(noon, 3, 3))
	if all, _ := ids(Conditions(t.Context(), d, c, noon)); len(all) != 0 {
		t.Fatalf("after 3 runs: %q", all)
	}
	beatFile(t, d, claude, "SessionStart", noon, nil, runsOf(noon, 4, 4))
	conds := Conditions(t.Context(), d, c, noon)
	if all, relevant := ids(conds); !slices.Equal(all, []string{"hook.failing/claude-code/SessionStart"}) || !slices.Equal(relevant, all) || conds[0].Severity != "high" {
		t.Fatalf("after 4 runs: %q, relevant %q, %+v", all, relevant, conds)
	}
	for _, host := range []string{claude, codex} {
		if got, _ := lineFor(conds, host); got.ID != "hook.failing/claude-code/SessionStart" {
			t.Errorf("%s's line %q", host, got.ID)
		}
	}
}

// codexChat makes a Codex chat appear at at, in the folder of its day.
func codexChat(t *testing.T, at time.Time) {
	t.Helper()
	plant(t, filepath.Join(codexDay(at), "rollout.jsonl"), nil)
	touch(t, codexDay(at), at)
}

// trustFlagAt leaves the needs-trust flag install writes, at at.
func trustFlagAt(t *testing.T, d paths.Dirs, at time.Time) {
	t.Helper()
	flag := filepath.Join(d.State, "needs-trust.json")
	put(t, flag, []byte(`{"host":"codex","needs_trust":true}`+"\n"))
	touch(t, flag, at)
}

// Fault-matrix rows 2 and 3: Codex's hooks wait for a review. While no
// Codex chat starts after the flag, as when Codex is installed for and
// never used, nothing is said; a chat more than 5 minutes after it, with
// no Codex SessionStart hook since, is trust.review, Claude Code's line
// and not Codex's; a SessionStart hook after the flag ends it.
func TestRowsTwoAndThreeTrust(t *testing.T) {
	claude, codex := updateHosts[0], updateHosts[1]
	d := isolated(t)
	c := config.Default()
	installedFor(t, d, claude, noon)
	install := noon.Add(-2 * time.Hour)
	installedFor(t, d, codex, noon)
	touch(t, filepath.Join(d.Share, "marketplace", cli(codex), ".eva-md-managed"), install)
	trustFlagAt(t, d, install)
	beatWith(t, d, claude, "SessionStart", noon.Add(-time.Minute), true, false)
	codexChat(t, install.Add(-time.Hour))
	if all, _ := ids(Conditions(t.Context(), d, c, noon)); len(all) != 0 {
		t.Fatalf("no Codex chat since the flag: %q", all)
	}
	codexChat(t, install.Add(4*time.Minute))
	if all, _ := ids(Conditions(t.Context(), d, c, noon)); len(all) != 0 {
		t.Fatalf("a Codex chat inside the bar: %q", all)
	}
	codexChat(t, install.Add(10*time.Minute))
	conds := Conditions(t.Context(), d, c, noon)
	if got, _ := lineFor(conds, claude); got.ID != "trust.review/codex" || got.Severity != "high" {
		t.Fatalf("Claude Code's line %+v; all %+v", got, conds)
	}
	if got, ok := lineFor(conds, codex); ok && got.Type == CondTrustReview {
		t.Fatalf("Codex's own line is %q", got.ID)
	}
	beatWith(t, d, codex, "SessionStart", install.Add(20*time.Minute), true, false)
	for _, cond := range Conditions(t.Context(), d, c, noon) {
		if cond.Type == CondTrustReview {
			t.Fatalf("after Codex's SessionStart: %+v", cond)
		}
	}

	// Codex installed for two days and never used: its conditions are
	// kept and never a line, the flag included.
	d = isolated(t)
	installedFor(t, d, claude, noon)
	marker := filepath.Join(d.Share, "marketplace", cli(codex), ".eva-md-managed")
	put(t, marker, []byte("eva-md marketplace v1\n"))
	touch(t, marker, noon.Add(-48*time.Hour))
	trustFlagAt(t, d, noon.Add(-48*time.Hour))
	beatWith(t, d, claude, "SessionStart", noon.Add(-time.Minute), true, false)
	put(t, filepath.Join(d.State, "catchup", "drift-"+codex+".json"), []byte(`{"v":1,"host":"codex"}`))
	conds = Conditions(t.Context(), d, c, noon)
	all, relevant := ids(conds)
	if !slices.Equal(all, []string{"recovery.drift/codex"}) || len(relevant) != 0 {
		t.Fatalf("an unused Codex: %q, relevant %q", all, relevant)
	}
	for _, host := range updateHosts {
		if got, ok := lineFor(conds, host); ok {
			t.Errorf("%s's line %q", host, got.ID)
		}
	}
	// The day after its install it is used, and its conditions are lines.
	touch(t, marker, noon.Add(-12*time.Hour))
	if _, relevant := ids(Conditions(t.Context(), d, c, noon)); !slices.Equal(relevant, []string{"recovery.drift/codex"}) {
		t.Fatalf("Codex installed 12 hours ago: relevant %q", relevant)
	}
}

// Fault-matrix row 8: organizing held for a sign-in is said at the next
// chat start on either host, also on the day after the hold, before an
// update has tried again; updates = false says nothing of it.
func TestRowEightSignIn(t *testing.T) {
	d, c := wikiWithRemote(t)
	claude := updateHosts[0]
	inUse(t, d, noon)
	plantRunLog(t, d, okRecord(noon.Add(-26*time.Hour).Format(time.RFC3339), claude, "s1"), failedRecord(noon.Add(-25*time.Hour).Format(time.RFC3339), claude, "s1", update.ReasonAuth))
	conds := Conditions(t.Context(), d, c, noon)
	for _, host := range updateHosts {
		if got, _ := lineFor(conds, host); got.ID != "update.signin/claude-code" {
			t.Errorf("%s's line %q; all %+v", host, got.ID, conds)
		}
	}
	c.Updates = false
	if all, _ := ids(Conditions(t.Context(), d, c, noon)); len(all) != 0 {
		t.Fatalf("updates off: %q", all)
	}
}

// wikiWithRemote is stageHome's wiki with a commit pushed to a bare remote,
// origin, and a second commit not pushed.
func wikiWithRemote(t *testing.T) (paths.Dirs, config.Config) {
	t.Helper()
	d, c := stageHome(t)
	remote := filepath.Join(os.Getenv("HOME"), "remote.git")
	gitIn(t, "", "init", "-q", "--bare", remote)
	gitIn(t, c.WikiPath, "remote", "add", "origin", remote)
	put(t, filepath.Join(c.WikiPath, "index.md"), []byte("# Wiki\n"))
	if err := config.SaveSchema(filepath.Join(c.WikiPath, ".eva-md", "schema.toml"), config.WikiSchema{SchemaVersion: 1, Instance: c.Instance, Created: "2026-03-01"}); err != nil {
		t.Fatal(err)
	}
	gitIn(t, c.WikiPath, "add", "-A")
	gitIn(t, c.WikiPath, "commit", "-q", "-m", "one")
	gitIn(t, c.WikiPath, "push", "-q", "origin", "HEAD")
	put(t, filepath.Join(c.WikiPath, "general", "threads", "a.md"), []byte("a page\n"))
	gitIn(t, c.WikiPath, "add", "-A")
	gitIn(t, c.WikiPath, "commit", "-q", "-m", "two")
	return d, c
}

// attemptAt leaves the day's attempt marker, taken at at.
func attemptAt(t *testing.T, d paths.Dirs, day, at time.Time) {
	t.Helper()
	file := filepath.Join(d.State, "commit", "attempt-"+day.Format(time.DateOnly))
	put(t, file, nil)
	touch(t, file, at)
}

// Fault-matrix row 9: pushes failing for 72 hours with a remote is
// push.stale, once the day's maintain has had an hour; a commit failing
// for 48 hours is commit.stale by its records alone. Neither is said
// with commits = false.
func TestRowNinePushAndCommitStale(t *testing.T) {
	d, c := wikiWithRemote(t)
	inUse(t, d, noon)
	plantCommits(t, d, wikigit.Result{At: noon.Add(-73 * time.Hour).Format(time.RFC3339), Outcome: wikigit.Committed, Pages: 1, Push: wikigit.PushPending})
	if all, _ := ids(Conditions(t.Context(), d, c, noon)); len(all) != 0 {
		t.Fatalf("no maintain today: %q", all)
	}
	attemptAt(t, d, noon, noon.Add(-30*time.Minute))
	if all, _ := ids(Conditions(t.Context(), d, c, noon)); len(all) != 0 {
		t.Fatalf("today's maintain 30 minutes old: %q", all)
	}
	attemptAt(t, d, noon, noon.Add(-time.Hour))
	conds := Conditions(t.Context(), d, c, noon)
	if all, _ := ids(conds); !slices.Equal(all, []string{"push.stale"}) || !strings.Contains(conds[0].Text, "3 days") {
		t.Fatalf("today's maintain an hour old: %+v", conds)
	}
	for _, host := range updateHosts {
		if got, _ := lineFor(conds, host); got.ID != "push.stale" {
			t.Errorf("%s's line %q", host, got.ID)
		}
	}
	plantCommits(t, d, wikigit.Result{At: noon.Add(-47 * time.Hour).Format(time.RFC3339), Outcome: wikigit.Refused, Reason: "origin", Push: wikigit.PushSkipped})
	if all, _ := ids(Conditions(t.Context(), d, c, noon)); !slices.Equal(all, []string{"push.stale"}) {
		t.Fatalf("a commit failing for 47 hours: %q", all)
	}
	if all, _ := ids(Conditions(t.Context(), d, c, noon.Add(2*time.Hour))); !slices.Equal(all, []string{"commit.stale", "push.stale"}) {
		t.Fatalf("a commit failing for 49 hours: %q", all)
	}
	c.Commits = false
	if all, _ := ids(Conditions(t.Context(), d, c, noon.Add(2*time.Hour))); len(all) != 0 {
		t.Fatalf("commits off: %q", all)
	}
}

// A stage that is off or could not be read raises no condition (the
// lead's ruling 2): the capture of a host eva-md is not installed for,
// though its chats are hours newer than its hooks, and a chat folder that
// can't be read; the same capture installed for is capture.silent.
func TestOffOrUnreadStagesRaiseNothing(t *testing.T) {
	d := isolated(t)
	claude := updateHosts[0]
	c := config.Default()
	folder := filepath.Join(os.Getenv("HOME"), ".claude", "projects", "-project")
	beatWith(t, d, claude, "Stop", noon.Add(-5*time.Hour), true, false)
	chatFolder(t, folder, noon.Add(-time.Minute))
	if all, _ := ids(Conditions(t.Context(), d, c, noon)); len(all) != 0 {
		t.Fatalf("a host not installed for: %q", all)
	}
	installedFor(t, d, claude, noon)
	conds := Conditions(t.Context(), d, c, noon)
	if all, _ := ids(conds); !slices.Equal(all, []string{"capture.silent/claude-code"}) || !strings.Contains(conds[0].Text, "5 hours") {
		t.Fatalf("installed for: %+v", conds)
	}
	if os.Geteuid() != 0 {
		shut(t, filepath.Dir(folder))
		if all, _ := ids(Conditions(t.Context(), d, c, noon)); len(all) != 0 {
			t.Fatalf("a chat folder that can't be read: %q", all)
		}
	}
}

// Fault-matrix row 15: an unknown config key and a diverged remote at once
// show the more severe, the key, and the remote once the key is fixed;
// neither hides the other for good.
func TestRowFifteenTwoConditions(t *testing.T) {
	d, c := stageHome(t)
	inUse(t, d, noon)
	plantCommits(t, d, wikigit.Result{At: noon.Add(-time.Hour).Format(time.RFC3339), Outcome: wikigit.Committed, Pages: 1, Push: wikigit.PushDiverged})
	c.Unknown = []string{"mystery_key"}
	put(t, filepath.Join(c.WikiPath, "index.md"), []byte("# Wiki\n"))
	if err := config.SaveSchema(filepath.Join(c.WikiPath, ".eva-md", "schema.toml"), config.WikiSchema{SchemaVersion: 1, Instance: c.Instance, Created: "2026-03-01"}); err != nil {
		t.Fatal(err)
	}
	for i, want := range []string{"config.unknown_keys", "push.diverged"} {
		conds := Conditions(t.Context(), d, c, noon)
		for _, host := range updateHosts {
			if got, _ := lineFor(conds, host); got.ID != want {
				t.Errorf("step %d: %s's line %q, want %q", i, host, got.ID, want)
			}
		}
		c.Unknown = nil
	}
}
