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
		{CondWikiUnreadable, "", "", 0, 0, 0, "eva.md: the wiki's index.md or .eva-md/schema.toml can't be read, so briefs and updates leave the wiki out. Restore it from the wiki's git history."},
		{CondWikiInstance, "", "", 0, 0, 0, "eva.md: the wiki and eva-md's config name different installs, so nothing is written to the wiki. Point wiki_path and instance in config.toml at this Mac's wiki."},
		{CondTrustReview, codex, "", 0, 0, 0, "eva.md: Codex chats are not saved until Codex approves eva-md's hooks. In Codex, type /hooks and approve eva-md."},
		{CondCaptureSilent, claude, "", 0, 0, 3*time.Hour + 10*time.Minute, "eva.md: Claude Code's hooks have not run for 3 hours while it was in use, so its chats are not saved. Enable eva-md's plugin in Claude Code."},
		{CondHookFailing, claude, "SessionStart", 4, 4, 0, "eva.md: 4 of the last 4 Claude Code SessionStart hooks failed: they timed out or could not save. Free disk space and check eva-md's state folder."},
		{CondUpdateSignIn, claude, "", 0, 0, 0, "eva.md: Claude Code chats are not turned into notes while Claude Code is signed out. Run claude auth login."},
		{CondUpdateSignIn, codex, "", 0, 0, 0, "eva.md: Codex chats are not turned into notes while Codex is signed out. Run codex login."},
		{CondPushDiverged, "", "", 0, 0, 0, "eva.md: the wiki's remote has commits this clone lacks, so pushes stopped. Merge them in the wiki by hand, never with a force push."},
		{CondHookFailing, codex, "Stop", 5, 8, 0, "eva.md: 5 of the last 8 Codex Stop hooks failed: they timed out or could not save. Free disk space and check eva-md's state folder."},
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
	// 9 of 20 failed, 3 of the newest 8.
	prompts := runsOf(noon, 20, 0)
	for _, i := range []int{0, 2, 4, 6, 8, 10, 13, 16, 19} {
		prompts[i].Failed = true
	}
	beatFile(t, d, claude, "UserPromptSubmit", noon, nil, prompts)
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

	// On an install in use, whose heartbeat keeps 16 runs that passed
	// before the fault, the budget is the same 4 runs: 3 failures say
	// nothing, the 4th is the condition, of the newest 8 runs.
	beatFile(t, d, claude, "SessionStart", noon, nil, runsOf(noon, 19, 3))
	if all, _ := ids(Conditions(t.Context(), d, c, noon)); len(all) != 0 {
		t.Fatalf("16 runs that passed, then 3 failures: %q", all)
	}
	beatFile(t, d, claude, "SessionStart", noon, nil, runsOf(noon, 20, 4))
	conds = Conditions(t.Context(), d, c, noon)
	if all, _ := ids(conds); !slices.Equal(all, []string{"hook.failing/claude-code/SessionStart"}) || !strings.HasPrefix(conds[0].Text, "eva.md: 4 of the last 8 ") {
		t.Fatalf("16 runs that passed, then 4 failures: %+v", conds)
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
	// A Codex hook in a folder eva-md doesn't capture leaves only its
	// stamp, and ends it as well: its hooks are trusted.
	stamp := filepath.Join(d.State, "heartbeats", codex, "uncaptured.json")
	put(t, stamp, []byte(`{"at":"`+install.Add(-time.Minute).Format(time.RFC3339)+`"}`))
	if !hasType(Conditions(t.Context(), d, c, noon), CondTrustReview) {
		t.Fatal("a stamp from before the flag ended trust.review")
	}
	put(t, stamp, []byte(`{"at":"`+install.Add(15*time.Minute).Format(time.RFC3339)+`"}`))
	if cond := Conditions(t.Context(), d, c, noon); hasType(cond, CondTrustReview) {
		t.Fatalf("after a Codex hook in a folder not captured: %+v", cond)
	}
	if err := os.Remove(stamp); err != nil {
		t.Fatal(err)
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

// hasType reports whether conds hold a condition of typ.
func hasType(conds []Condition, typ string) bool {
	return slices.ContainsFunc(conds, func(c Condition) bool { return c.Type == typ })
}

// trustHome is a home whose Codex hooks wait for a review: the flag install
// left two hours before noon, no Codex hook since, and a Codex chat ten
// minutes after the flag.
func trustHome(t *testing.T) (paths.Dirs, config.Config, time.Time) {
	t.Helper()
	claude, codex := updateHosts[0], updateHosts[1]
	d := isolated(t)
	installedFor(t, d, claude, noon)
	installedFor(t, d, codex, noon)
	install := noon.Add(-2 * time.Hour)
	trustFlagAt(t, d, install)
	beatWith(t, d, claude, "SessionStart", noon.Add(-time.Minute), true, false)
	codexChat(t, install.Add(10*time.Minute))
	return d, config.Default(), install
}

// When Codex's chat folder could not be read, whether a Codex chat started
// is not known, and the flag alone is trust.review, Claude Code's line, as
// v1's notice said it; Codex is not taken for unused. (Root reads a folder
// whose mode shuts it, so this holds only for another user.)
func TestTrustReviewWhenCodexsChatsCannotBeRead(t *testing.T) {
	d, c, install := trustHome(t)
	if os.Geteuid() != 0 {
		shut(t, codexDay(install))
		stages := Stages(t.Context(), d, c, noon)
		if i := slices.IndexFunc(stages, func(s Stage) bool { return s.Name == StageActivity && s.Host == updateHosts[1] }); stages[i].Unread == "" {
			t.Fatalf("Codex's activity was read: %+v", stages[i])
		}
		conds := Conditions(t.Context(), d, c, noon)
		if got, _ := lineFor(conds, updateHosts[0]); got.ID != "trust.review/codex" {
			t.Fatalf("Claude Code's line %q; all %+v", got.ID, conds)
		}
	}
}

// A flag that is there but does not read as install writes it, cut short,
// a link, one whose keys differ only in case or that says needs_trust
// false, is pending all the same, as v1's notice took it: trust.review,
// and, though Codex's hooks may have run since, Prepare never removes it.
func TestATrustFlagThatDoesNotReadCleanIsPending(t *testing.T) {
	flags := map[string]string{
		"cut short":       `{"host":"codex","needs_tr`,
		"a key in case":   `{"host":"codex","NEEDS_TRUST":true}`,
		"needs no trust":  `{"host":"codex","needs_trust":false}`,
		"another host":    `{"host":"claude","needs_trust":true}`,
		"no host":         `{"needs_trust":true}`,
		"not an object":   `[1]`,
		"a link":          "",
		"can't be opened": `{"host":"codex","needs_trust":true}`,
	}
	for name, content := range flags {
		if name == "can't be opened" && os.Geteuid() == 0 {
			continue // root opens a file whose mode shuts it
		}
		t.Run(name, func(t *testing.T) {
			d, c, install := trustHome(t)
			flag := filepath.Join(d.State, "needs-trust.json")
			if err := os.Remove(flag); err != nil {
				t.Fatal(err)
			}
			if name == "a link" {
				// A link's own time can't be set back, so it is judged at
				// trustFlag: pending, not clean.
				target := filepath.Join(d.State, "elsewhere.json")
				put(t, target, []byte(`{"host":"codex","needs_trust":true}`))
				if err := os.Symlink(target, flag); err != nil {
					t.Fatal(err)
				}
				if reviewer, _, clean, ok := trustFlag(d); !ok || clean || reviewer.ID != "codex" {
					t.Fatalf("a link: %v %v %v", reviewer.ID, clean, ok)
				}
				return
			}
			put(t, flag, []byte(content))
			touch(t, flag, install)
			if name == "can't be opened" {
				if err := os.Chmod(flag, 0); err != nil {
					t.Fatal(err)
				}
			}
			conds := Conditions(t.Context(), d, c, noon)
			if got, _ := lineFor(conds, updateHosts[0]); got.ID != "trust.review/codex" {
				t.Fatalf("Claude Code's line %q; all %+v", got.ID, conds)
			}
			// Codex's hooks ran since: met, and still never removed.
			beatWith(t, d, updateHosts[1], "SessionStart", noon.Add(-time.Minute), true, false)
			if conds := Conditions(t.Context(), d, c, noon); hasType(conds, CondTrustReview) {
				t.Fatalf("met, still %+v", conds)
			}
			if err := tidy(d, noon); err != nil {
				t.Fatal(err)
			}
			if _, err := os.Lstat(flag); err != nil {
				t.Fatalf("a flag that does not read clean was removed: %v", err)
			}
		})
	}
	// The flag as install writes it is clean, and a key that differs from
	// one of its own only in case cancels nothing.
	d, c, install := trustHome(t)
	if reviewer, _, clean, ok := trustFlag(d); !ok || !clean || reviewer.ID != "codex" {
		t.Fatalf("install's own flag: %v %v %v", reviewer.ID, clean, ok)
	}
	flag := filepath.Join(d.State, "needs-trust.json")
	put(t, flag, []byte(`{"host":"codex","needs_trust":true,"NEEDS_TRUST":false}`))
	touch(t, flag, install)
	if reviewer, _, clean, ok := trustFlag(d); !ok || !clean || reviewer.ID != "codex" || !hasType(Conditions(t.Context(), d, c, noon), CondTrustReview) {
		t.Fatalf("a key in another case cancelled the flag: %v %v %v", reviewer.ID, clean, ok)
	}
}

// The wiki states the update and the daily commit refuse are conditions:
// a schema missing, not TOML or of no version is wiki.unreadable; an
// instance missing on either side, or not the config's, is wiki.instance;
// a newer schema is wiki.schema_newer. A wiki they take is none.
func TestTheWikiStatesTheUpdateRefuses(t *testing.T) {
	schemaFile := func(c config.Config) string { return filepath.Join(c.WikiPath, ".eva-md", "schema.toml") }
	for _, s := range []struct {
		name  string
		plant func(*testing.T, *config.Config)
		want  []string
	}{
		{"a wiki it takes", func(*testing.T, *config.Config) {}, nil},
		{"no schema", func(t *testing.T, c *config.Config) {
			if err := os.Remove(schemaFile(*c)); err != nil {
				t.Fatal(err)
			}
		}, []string{CondWikiUnreadable}},
		{"a schema not TOML", func(t *testing.T, c *config.Config) { put(t, schemaFile(*c), []byte("schema_version = = 1\n")) }, []string{CondWikiUnreadable}},
		{"a schema of no version", func(t *testing.T, c *config.Config) {
			put(t, schemaFile(*c), []byte("instance = \""+c.Instance+"\"\n"))
		}, []string{CondWikiUnreadable}},
		{"a schema of version 0", func(t *testing.T, c *config.Config) {
			put(t, schemaFile(*c), []byte("schema_version = 0\ninstance = \""+c.Instance+"\"\n"))
		}, []string{CondWikiUnreadable}},
		{"a config with no instance", func(_ *testing.T, c *config.Config) { c.Instance = "" }, []string{CondWikiInstance}},
		{"a schema with no instance", func(t *testing.T, c *config.Config) { put(t, schemaFile(*c), []byte("schema_version = 1\n")) }, []string{CondWikiInstance}},
		{"another instance", func(t *testing.T, c *config.Config) {
			put(t, schemaFile(*c), []byte("schema_version = 1\ninstance = \"another\"\n"))
		}, []string{CondWikiInstance}},
		{"a newer schema", func(t *testing.T, c *config.Config) {
			put(t, schemaFile(*c), []byte("schema_version = 2\ninstance = \""+c.Instance+"\"\n"))
		}, []string{CondSchemaNewer}},
	} {
		t.Run(s.name, func(t *testing.T) {
			d, c := stageHome(t)
			inUse(t, d, noon)
			put(t, filepath.Join(c.WikiPath, "index.md"), []byte("# Wiki\n"))
			if err := config.SaveSchema(schemaFile(c), config.WikiSchema{SchemaVersion: 1, Instance: c.Instance, Created: "2026-03-01"}); err != nil {
				t.Fatal(err)
			}
			s.plant(t, &c)
			var got []string
			for _, cond := range Conditions(t.Context(), d, c, noon) {
				if strings.HasPrefix(cond.Type, "wiki.") {
					got = append(got, cond.Type)
				}
			}
			if !slices.Equal(got, s.want) {
				t.Fatalf("conditions %q, want %q", got, s.want)
			}
		})
	}
}

// When today's run log or this month's commits can't be read, the records
// read before them still count, as v1's notice counts them: yesterday's
// failures in a row, and last month's diverged push. (Root reads a file
// whose mode shuts it, so this holds only for another user.)
func TestRecordsReadBeforeAnUnreadableFileCount(t *testing.T) {
	d, c := stageHome(t)
	put(t, filepath.Join(c.WikiPath, "index.md"), []byte("# Wiki\n"))
	if err := config.SaveSchema(filepath.Join(c.WikiPath, ".eva-md", "schema.toml"), config.WikiSchema{SchemaVersion: 1, Instance: c.Instance, Created: "2026-03-01"}); err != nil {
		t.Fatal(err)
	}
	inUse(t, d, noon)
	codex := updateHosts[1]
	yesterday := noon.AddDate(0, 0, -1)
	at := func(d time.Duration) string { return yesterday.Add(-d).Format(time.RFC3339) }
	plantRunLog(t, d, failedRecord(at(3*time.Hour), codex, "s1", update.ReasonHostError), failedRecord(at(2*time.Hour), codex, "s1", update.ReasonHostError),
		failedRecord(at(time.Hour), codex, "s1", update.ReasonHostError), okRecord(noon.Add(-time.Hour).Format(time.RFC3339), codex, "s2"))
	plantCommits(t, d, wikigit.Result{At: noon.AddDate(0, -1, 0).Format(time.RFC3339), Outcome: wikigit.Committed, Pages: 1, Push: wikigit.PushDiverged},
		wikigit.Result{At: noon.Add(-time.Hour).Format(time.RFC3339), Outcome: wikigit.Committed, Pages: 1, Push: wikigit.PushOK})
	if os.Geteuid() == 0 {
		return
	}
	for _, file := range []string{filepath.Join(d.State, "updates", noon.Format(time.DateOnly)+".jsonl"), filepath.Join(d.State, "commits", noon.Format("2006-01")+".jsonl")} {
		if err := os.Chmod(file, 0); err != nil {
			t.Fatal(err)
		}
	}
	conds := Conditions(t.Context(), d, c, noon)
	if !hasType(conds, CondUpdateFailing) || !hasType(conds, CondPushDiverged) {
		t.Fatalf("conditions %+v", conds)
	}
}
