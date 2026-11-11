package health

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/heyNag/eva-md/internal/coderef"
	"github.com/heyNag/eva-md/internal/identity"
	"github.com/heyNag/eva-md/internal/inbox"
	"github.com/heyNag/eva-md/internal/paths"
	"github.com/heyNag/eva-md/internal/update"
	"github.com/heyNag/eva-md/internal/wikigit"
)

// planted is the made-up marker the report's fixture puts in every text a
// counter reads: lower-case letters only, so it has the shape of a name the
// run log or a heartbeat holds, as a project's name can.
const planted = "zqxwvplant"

// reportNow is the clock of the report's fixture.
var reportNow = time.Date(2026, 11, 11, 15, 0, 0, 0, time.UTC)

// fakeToken is a made-up value in the shape of a GitHub token, which the
// current redaction rules catch, stored as an older release that missed it
// would have stored it.
var fakeToken = "ghp_" + strings.Repeat("Ab3dE", 7) + "x"

// line is an inbox line for host's session in project, at, of kind.
func line(t *testing.T, project, host, session string, at time.Time, kind, text string) string {
	t.Helper()
	role := map[string]string{"prompt": "user", "reply": "assistant", "tool": "tool", "plan": "plan", "event": "event", "host_summary": "host"}[kind]
	data, err := json.Marshal(inbox.Entry{V: 1, TS: at.Format(time.RFC3339), Host: host, Session: session, Role: role, Kind: kind, Text: text,
		Project: project, Branch: planted + "/branch", Cwd: planted, Model: planted, Agent: planted, Origin: "hook", Red: strings.Count(text, "[REDACTED:")})
	if err != nil {
		t.Fatal(err)
	}
	return string(data) + "\n"
}

// session writes a session file of lines into the inbox, with mode 0600.
func session(t *testing.T, d paths.Dirs, project, host, id string, lines ...string) string {
	t.Helper()
	file := filepath.Join(d.Inbox(), project, host+"-"+id+".jsonl")
	put(t, file, []byte(strings.Join(lines, "")))
	return file
}

// reportHome makes the report's fixture home at reportNow, and returns its
// folders and the wiki's.
func reportHome(t *testing.T) (paths.Dirs, string) {
	t.Helper()
	d := isolated(t)
	home := os.Getenv("HOME")
	wiki := filepath.Join(home, "wiki")
	put(t, d.ConfigFile(), []byte(fmt.Sprintf("wiki_path = %q\ninstance = \"test-0a1b2c3d\"\n%s = true\n", wiki, planted)))

	// The wiki: 2 thread pages, 2 rules pages, 1 knowledge page, 1 system
	// page, 3 indexes and 1 other, beside what is passed over.
	for _, page := range []string{"index.md", "general/threads/index.md", "general/threads/" + planted + ".md", "general/rules.md",
		"projects/" + planted + "/threads/" + planted + ".md", "projects/" + planted + "/rules.md", "knowledge/k1.md", "knowledge/" + planted + ".md",
		"systems/" + planted + ".md", "systems/map.md", ".git/" + planted + ".md", ".eva-md/schema.toml", "projects/" + planted + "/notes.txt"} {
		put(t, filepath.Join(wiki, page), []byte("# "+planted+"\n"))
	}

	// The inbox. One Codex session holds every kind of entry, two markers,
	// one of a kind no rule writes, a token the current rules catch, the
	// tool lines the wiki part counts, two lines passed over and an
	// unterminated last one; an entry recovered after the others is its
	// oldest. It has no watermark, so all of it waits.
	project := planted + "-proj"
	id := planted + "-s1"
	at := reportNow.Add(-2 * time.Hour)
	other, _ := json.Marshal(inbox.Entry{V: 1, TS: at.Format(time.RFC3339), Host: "codex", Session: planted + "-elsewhere", Kind: "prompt", Text: planted, Project: project})
	session(t, d, project, "codex", id,
		line(t, project, "codex", id, at, "event", "session_start"),
		line(t, project, "codex", id, at, "prompt", "please look at "+planted),
		line(t, project, "codex", id, at, "reply", "the key was "+fakeToken+" in "+planted),
		line(t, project, "codex", id, at, "reply", "kept [REDACTED:github_token] and [REDACTED:"+planted+"] and [REDACTED:Upper]"),
		line(t, project, "codex", id, at, "tool", "Bash /usr/local/bin/eva-md system write "+planted+" --from x (exit 0)"),
		line(t, project, "codex", id, at, "tool", "Bash (subagent) eva-md system status (exit 0)"),
		line(t, project, "codex", id, at, "tool", "Edit "+filepath.Join(wiki, "projects", planted, "threads", planted+".md")+" (exit 0)"),
		line(t, project, "codex", id, at, "tool", "apply_patch "+planted+"/notes.md, "+filepath.Join(wiki, "general", "rules.md")+" (exit 0)"),
		line(t, project, "codex", id, at, "tool", "apply_patch "+planted+"/notes.md (exit 1)"),
		line(t, project, "codex", id, at, "tool", "Write "+filepath.Join(home, planted, "elsewhere.md")+" (exit 0)"),
		line(t, project, "codex", id, at, "tool", "Write "+wiki+"-other/"+planted+".md (exit 0)"),
		line(t, project, "codex", id, at, "plan", "TodoWrite "+planted),
		line(t, project, "codex", id, at, "host_summary", planted),
		line(t, project, "codex", id, at, planted, planted),
		line(t, project, "codex", id, reportNow.Add(-50*time.Hour), "prompt", "recovered "+planted),
		"not json "+planted+"\n",
		string(other)+"\n",
		`{"v":1,"ts":"`+at.Format(time.RFC3339)+`","host":"codex","session":"`+id+`","kind":"prompt","text":"`+planted)

	// 66 Claude Code sessions in one folder, each with a watermark at its
	// end, but for one waiting with an entry three days old, the folder's
	// oldest by its file's time, so past the 64 newest; one whose
	// watermark is damaged; one the update found too small to process yet;
	// and one waiting whose entries carry no time.
	store := inbox.Store{Root: d.Inbox()}
	folder := planted + "-two"
	for i := range 66 {
		sid := fmt.Sprintf("%s-c%02d", planted, i)
		file := session(t, d, folder, "claude-code", sid, line(t, folder, "claude-code", sid, reportNow.Add(-time.Hour), "prompt", planted))
		info, err := os.Stat(file)
		if err != nil {
			t.Fatal(err)
		}
		mark := inbox.Mark{Offset: info.Size()}
		switch i {
		case 0:
			mark.Offset = 0
			old := line(t, folder, "claude-code", sid, reportNow.Add(-72*time.Hour), "reply", planted)
			put(t, file, append(must(os.ReadFile(file)), old...))
			if err := os.Chtimes(file, reportNow.Add(-96*time.Hour), reportNow.Add(-96*time.Hour)); err != nil {
				t.Fatal(err)
			}
		case 1:
			put(t, filepath.Join(d.Inbox(), folder, ".state", filepath.Base(file)+".wm"), []byte(planted))
			continue
		case 2:
			mark = inbox.Mark{Wait: info.Size()}
		case 3:
			mark.Offset = 0
			untimed, _ := json.Marshal(inbox.Entry{V: 1, Host: "claude-code", Session: sid, Kind: "event", Text: "session_end", Project: folder})
			put(t, file, append(untimed, '\n'))
		}
		if err := store.SetMark(file, mark); err != nil {
			t.Fatal(err)
		}
	}

	// The run log, CT-2's records, with names no release writes in every
	// place a name goes.
	day := reportNow.Add(-24 * time.Hour).Format(time.RFC3339)
	plantRunLog(t, d,
		update.Record{At: day, Host: "claude-code", Session: planted, Project: planted, Thread: planted, Outcome: update.OutcomeOK, Calls: 1, Pages: 1,
			TokensIn: 1000, TokensOut: 100, DurationMS: 30000, BoundaryToSavedMS: 120000, Model: planted, ModelReported: planted + "x", ModelNote: planted,
			Degraded: planted, Trigger: update.TriggerEnd, Validation: &update.Report{BadRefs: 2, Rules: update.RuleCounts{Refused: 1}}},
		update.Record{At: day, Host: "codex", Session: planted, Project: planted, Outcome: update.OutcomeOK, Calls: 1, Pages: 2, TokensIn: 3000,
			TokensOut: 300, DurationMS: 90000, BoundaryToSavedMS: 600000, Trigger: planted},
		update.Record{At: day, Host: "codex", Session: planted, Outcome: update.OutcomeFailed, Reason: planted, Calls: 1, DurationMS: 1000},
		update.Record{At: day, Host: "codex", Session: planted, Outcome: update.OutcomeFailed, Reason: update.ReasonInvalidOutput, Calls: 1,
			Rejection: &update.Rejection{Rule: planted, Place: planted}},
		update.Record{At: day, Host: "codex", Session: planted, Outcome: update.OutcomeSkipped, Reason: update.ReasonSmall, Trigger: update.TriggerSweep},
		update.Record{At: day, Host: "codex", Session: planted, Outcome: planted},
		update.Record{At: day, Outcome: update.OutcomeBusy, Trigger: planted, SessionHashes: []string{planted}},
	)

	// The commit records: four runs in the 14 days, one of them in the
	// month before, and one before them.
	plantCommits(t, d,
		wikigit.Result{At: "2026-10-20T09:00:00Z", Outcome: wikigit.Committed, Pages: 1, Push: wikigit.PushOK},
		wikigit.Result{At: "2026-10-30T09:00:00Z", Outcome: wikigit.Committed, Pages: 3, Push: wikigit.PushOK, Unstaged: []string{planted}},
		wikigit.Result{At: "2026-11-09T09:00:00Z", Outcome: wikigit.Refused, Reason: "origin", Push: wikigit.PushSkipped},
		wikigit.Result{At: "2026-11-10T09:00:00Z", Outcome: wikigit.Failed, Reason: planted, Push: wikigit.PushSkipped},
		wikigit.Result{At: "2026-11-10T09:05:00Z", Outcome: wikigit.Busy, Reason: "wiki_lock"},
		wikigit.Result{At: "2026-11-11T09:00:00Z", Outcome: wikigit.Nothing, Push: wikigit.PushPending},
	)

	// The daily job's attempt markers: three in the 14 days.
	for _, name := range []string{"attempt-2026-11-11", "attempt-2026-11-01", "attempt-2026-10-29", "attempt-2026-10-28", "attempt-" + planted, "attempt-2026-13-01", "push-retry"} {
		put(t, filepath.Join(d.State, "commit", name), nil)
	}

	// The trust flag, three days old, still awaited.
	flag := filepath.Join(d.State, "needs-trust.json")
	put(t, flag, []byte(`{"host":"codex","needs_trust":true}`))
	if err := os.Chtimes(flag, reportNow.Add(-73*time.Hour), reportNow.Add(-73*time.Hour)); err != nil {
		t.Fatal(err)
	}

	// The heartbeats: Claude Code's Stop kept three runs, one of them older
	// than the 14 days and one failed, and a week of three runs; Codex's
	// prompts a week of two.
	var stop []HookDay
	stop = AddRun(stop, reportNow.Add(-48*time.Hour), HookRun{DurationMS: 4, Held: []string{"reply"}, HeldBy: HeldByRecovery})
	stop = AddRun(stop, reportNow.Add(-2*time.Hour), HookRun{DurationMS: 40, Failed: true})
	stop = AddRun(stop, reportNow.Add(-time.Hour), HookRun{DurationMS: 4000, Held: []string{"reply"}, HeldBy: HeldByOffRecord})
	beatFile(t, d, "claude-code", "Stop", reportNow.Add(-time.Hour), stop, []hookRun{
		{At: reportNow.Add(-20 * 24 * time.Hour)}, {At: reportNow.Add(-2 * time.Hour), Failed: true}, {At: reportNow.Add(-time.Hour)}})
	var prompts []HookDay
	prompts = AddRun(prompts, reportNow.Add(-3*time.Hour), HookRun{DurationMS: 120, Delivered: &Delivery{Kind: "prompt", Routes: []string{"recall", "pointers"}, Chars: 300}})
	prompts = AddRun(prompts, reportNow.Add(-time.Hour), HookRun{DurationMS: 90, Delivered: &Delivery{Kind: "prompt", Fallback: true, Stale: true, Routes: []string{"state"}, Chars: 900}})
	beatFile(t, d, "codex", "UserPromptSubmit", reportNow.Add(-time.Hour), prompts, nil)

	// The code status: three system pages, two citing code, one of them in
	// a repo with no clone.
	if err := coderef.Save(d.State, coderef.Report{V: 1, At: day, Pages: []coderef.PageStatus{
		{Slug: planted, Repos: []coderef.RepoStatus{{Key: planted, Commit: planted, Paths: []string{planted}, State: coderef.NoClone},
			{Key: planted, Commit: planted, State: coderef.Moved, Moved: 3, Clone: filepath.Join(home, planted), Branch: planted}}},
		{Slug: planted + "b"},
		{Slug: planted + "c", Repos: []coderef.RepoStatus{{Key: planted, State: planted}}},
	}}); err != nil {
		t.Fatal(err)
	}
	return d, wiki
}

// beatFile writes an event's heartbeat at, with its week and its last runs.
func beatFile(t *testing.T, d paths.Dirs, host, event string, at time.Time, week []HookDay, runs []hookRun) {
	t.Helper()
	raw, err := json.Marshal(beat{At: at, Host: host, Event: event, Runs: runs, Week: week, Entrypoint: planted})
	if err != nil {
		t.Fatal(err)
	}
	put(t, filepath.Join(d.State, "heartbeats", host, event+".json"), raw)
}

func must(data []byte, err error) []byte {
	if err != nil {
		panic(err)
	}
	return data
}

// fieldJSON is the report of d at reportNow, as eva-md doctor --report
// gives it before it puts lists of numbers on one line, with the version
// test.
func fieldJSON(t *testing.T, d paths.Dirs) []byte {
	t.Helper()
	version := Version
	Version = "test"
	defer func() { Version = version }()
	data, err := json.MarshalIndent(Field(context.Background(), d, reportNow), "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	return append(data, '\n')
}

// On the fixture, the report gives exact counts, as the golden file holds
// them, and the same bytes on a second run.
func TestFieldReportCountsTheFixture(t *testing.T) {
	d, _ := reportHome(t)
	got := fieldJSON(t, d)
	if again := fieldJSON(t, d); !bytes.Equal(again, got) {
		t.Errorf("a second report of the same state differs:\n%s", again)
	}
	var r FieldReport
	if err := json.Unmarshal(got, &r); err != nil {
		t.Fatal(err)
	}

	// The Codex session's lines name paths under the home folder, whose
	// length differs from run to run, so its size is checked against the
	// file's and left out of the golden file.
	info, err := os.Stat(filepath.Join(d.Inbox(), planted+"-proj", "codex-"+planted+"-s1.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if r.Record.Bytes["codex"] != int(info.Size()) || r.Backlog.Hosts["codex"].Bytes != info.Size() {
		t.Errorf("the Codex session's bytes: %d and %d waiting, want %d", r.Record.Bytes["codex"], r.Backlog.Hosts["codex"].Bytes, info.Size())
	}
	sized := r
	sized.Record.Bytes = map[string]int{"claude-code": r.Record.Bytes["claude-code"], "codex": -1}
	sized.Backlog.Hosts = maps.Clone(r.Backlog.Hosts)
	waits := sized.Backlog.Hosts["codex"]
	waits.Bytes = -1
	sized.Backlog.Hosts["codex"] = waits
	stable, err := json.MarshalIndent(sized, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	stable = append(stable, '\n')
	golden := filepath.Join("testdata", "report.golden")
	if *updateGolden {
		if err := os.WriteFile(golden, stable, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(stable, want) {
		t.Errorf("the report differs from %s (go test ./internal/health -run TestFieldReportCountsTheFixture -update-golden writes it again):\n%s", golden, stable)
	}
	o := r.Operations
	check := func(what string, got, want any) {
		t.Helper()
		if !reflect.DeepEqual(got, want) {
			t.Errorf("%s: %v, want %v", what, got, want)
		}
	}
	check("format, version and day", []any{r.Format, r.Version, r.Day}, []any{1, "test", "2026-11-11"})
	check("parts", r.Parts, map[string]string{"operations": "counted", "hooks": "counted", "updates": "counted", "backlog": "counted", "record": "counted", "wiki": "counted"})
	check("operations' days", []any{o.From, o.To, o.DailyJobDays, o.CommitDays}, []any{"2026-10-29", "2026-11-11", 3, 4})
	check("commits", o.Commits, map[string]int{"committed": 1, "refused": 1, "failed": 1, "busy": 1, "nothing": 1})
	check("commit reasons", o.Reasons, map[string]int{"refused.origin": 1, "failed.other": 1, "busy.wiki_lock": 1})
	check("pushes", o.Pushes, map[string]int{"ok": 1, "skipped": 2, "pending": 1})
	check("trust flag", o.Trust, TrustFlag{Present: true, AgeDays: 3, Awaited: true})
	check("Stop's kept runs", o.HookRuns["claude-code"]["Stop"], HookRunsCounts{Runs: 2, Failed: 1})
	check("notice", o.Notice, "config_unknown_keys")

	h := r.Hooks
	stop := h.Events["claude-code"]["Stop"]
	check("Stop's week", []any{stop.Days, stop.Runs, stop.Failed, stop.MS[1], stop.MS[5], stop.MS[len(stop.MS)-1], stop.P50, stop.P95}, []any{2, 3, 1, 1, 1, 1, 50.0, -1.0})
	check("held back", []any{h.Behind["reply"], h.OffRecord["reply"], h.Behind["prompt"]}, []any{1, 1, 0})
	v := h.Delivered
	check("delivered", []any{v.Kinds["prompt"], v.Added, v.Chars, v.Routes["recall"], v.Routes["state"], v.Starts, v.Stale, v.Fallback}, []any{2, 2, 1200, 1, 1, 1, 1, 1})

	u := r.Updates
	check("update outcomes", u.Outcome, map[string]int{"ok": 2, "failed": 2, "skipped": 1, "other": 1})
	check("update failures", u.Fails, map[string]int{"other": 1, "invalid_output": 1})
	check("refused whole", u.Refused, map[string]int{"other": 1})
	check("inside", u.Inside["bad_refs"]+u.Inside["rules.refused"], 3)
	check("busy", []any{u.Busy.Jobs, u.Busy.Sessions, u.Busy.ByTrigger}, []any{1, 1, map[string]int{"other": 1}})
	check("to memory", []any{len(u.ToMemoryMS), u.ToMemoryMS[update.TriggerEnd].N, u.Unnamed}, []any{1, 1, 1})

	b := r.Backlog.Hosts
	check("Codex backlog", []any{b["codex"].Sessions, b["codex"].OldestDay, b["codex"].OldestHours, b["codex"].Untimed}, []any{1, "2026-11-09", 50, 0})
	check("Claude Code backlog", []any{b["claude-code"].Sessions, b["claude-code"].OldestDay, b["claude-code"].OldestHours, b["claude-code"].Untimed, b["claude-code"].Damaged},
		[]any{2, "2026-11-08", 72, 1, 1})

	rec := r.Record
	check("record days", []any{rec.From, rec.To}, []any{"2026-11-08", "2026-11-11"})
	check("sessions", rec.Sessions, map[string]int{"codex": 1, "claude-code": 66})
	check("entries", rec.Entries, map[string]int{"event": 2, "prompt": 67, "reply": 3, "tool": 7, "plan": 1, "host_summary": 1, "other": 1})
	check("passed over", rec.PassedOver, 2)
	check("markers", []any{rec.Markers, rec.Replacements}, []any{map[string]int{"github_token": 1, "other": 1}, 3})
	check("would catch", []any{rec.Catch.Kinds, rec.Catch.Tiers, rec.Catch.Entries, rec.Catch.Sessions},
		[]any{map[string]int{"github_token": 1}, map[string]int{"certain": 1}, 1, 1})

	// The Claude Code session waiting past its folder's 64 newest files is
	// one eva-md status's count leaves out.
	if n, _, err := Backlog(context.Background(), d, identity.Project{Key: planted + "-two"}); err != nil || n != 1 {
		t.Errorf("the 64 newest files hold %d waiting sessions (%v), want 1", n, err)
	}
	waiting, err := Unorganized(context.Background(), d)
	if err != nil || !waiting.Complete || !waiting.Hosts["claude-code"].Oldest.Equal(reportNow.Add(-72*time.Hour)) {
		t.Errorf("Unorganized: %+v, %v", waiting, err)
	}

	w := r.Wiki
	check("pages", w.Pages, map[string]int{"thread": 2, "rules": 2, "knowledge": 1, "system": 1, "index": 3, "other": 1})
	check("tool lines", []any{w.SystemWrites, w.Edits, w.Unplaced}, []any{1, 2, 1})
	check("code status", []any{w.Code.Found, w.Code.Pages, w.Code.CitingCode, w.Code.Entries, w.Code.PagesNoClone},
		[]any{true, 3, 2, map[string]int{"no_clone": 1, "moved": 1, "other": 1}, 1})
}

// No text a counter reads reaches the report: not the marker planted in
// every one, a session id, a project or branch name, a token, a path under
// the home folder or the wiki's.
func TestFieldReportHoldsNoText(t *testing.T) {
	d, wiki := reportHome(t)
	got := string(fieldJSON(t, d))
	for _, text := range []string{planted, fakeToken, "ghp_", os.Getenv("HOME"), wiki, "Upper", "elsewhere", "/"} {
		if strings.Contains(got, text) {
			t.Errorf("the report holds %q", text)
		}
	}
}

// fileState is what a file of a home is: its mode, size, time and bytes.
type fileState struct {
	mode fs.FileMode
	size int64
	mod  time.Time
	sum  [32]byte
}

// snapshot is every file and folder under root.
func snapshot(t *testing.T, root string) map[string]fileState {
	t.Helper()
	files := map[string]fileState{}
	err := filepath.WalkDir(root, func(path string, e fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := os.Lstat(path)
		if err != nil {
			return err
		}
		s := fileState{mode: info.Mode(), size: info.Size(), mod: info.ModTime()}
		if info.Mode().IsRegular() {
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			s.sum = sha256.Sum256(data)
		}
		files[path] = s
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return files
}

// The report reads the stored text, the redaction count included, and
// changes no byte, time or mode of any file, adds none and removes none:
// it repairs nothing.
func TestFieldReportChangesNoFile(t *testing.T) {
	d, _ := reportHome(t)
	home := os.Getenv("HOME")
	before := snapshot(t, home)
	r := Field(context.Background(), d, reportNow)
	if r.Record.Catch.Kinds["github_token"] != 1 {
		t.Fatalf("the redaction count read nothing: %+v", r.Record.Catch)
	}
	after := snapshot(t, home)
	if !reflect.DeepEqual(before, after) {
		for path := range after {
			if before[path] != after[path] {
				t.Errorf("%s changed", path)
			}
		}
		for path := range before {
			if _, ok := after[path]; !ok {
				t.Errorf("%s was removed", path)
			}
		}
	}
}

// A home with nothing recorded gives zeros and says so, and one without a
// wiki leaves the wiki's part out; none fails. A run log, inbox or wiki
// that can't be read gives zeros and says it is unreadable.
func TestFieldReportOnHomesWithLittleRecorded(t *testing.T) {
	t.Run("bare", func(t *testing.T) {
		d := isolated(t)
		r := Field(context.Background(), d, reportNow)
		want := map[string]string{"operations": "counted", "hooks": "none_recorded", "updates": "none_recorded", "backlog": "counted", "record": "none_recorded", "wiki": "no_wiki"}
		if !reflect.DeepEqual(r.Parts, want) || r.Wiki != nil || r.Operations.Notice != "none" || r.Format != ReportFormat {
			t.Fatalf("%+v", r)
		}
		if len(r.Updates.Calls) != 7 || r.Record.Sessions["codex"] != 0 || r.Hooks.Events["codex"]["Stop"].MS == nil {
			t.Fatalf("%+v", r)
		}
	})
	t.Run("a wiki and nothing else", func(t *testing.T) {
		d := isolated(t)
		wiki := filepath.Join(os.Getenv("HOME"), "wiki")
		put(t, filepath.Join(wiki, "index.md"), []byte("# Wiki\n"))
		put(t, d.ConfigFile(), []byte(fmt.Sprintf("wiki_path = %q\n", wiki)))
		r := Field(context.Background(), d, reportNow)
		if r.Parts["wiki"] != PartCounted || r.Wiki == nil || r.Wiki.Pages["index"] != 1 || r.Wiki.Code.Found || r.Parts["updates"] != PartNone {
			t.Fatalf("%+v %+v", r.Parts, r.Wiki)
		}
	})
	t.Run("unreadable", func(t *testing.T) {
		d := isolated(t)
		wiki := filepath.Join(os.Getenv("HOME"), "wiki")
		put(t, wiki, []byte("not a folder"))
		put(t, d.ConfigFile(), []byte(fmt.Sprintf("wiki_path = %q\n", wiki)))
		put(t, filepath.Join(d.State, "updates", reportNow.Format(time.DateOnly)+".jsonl", "x"), nil)
		put(t, d.Inbox(), []byte("not a folder"))
		r := Field(context.Background(), d, reportNow)
		want := map[string]string{"operations": "counted", "hooks": "none_recorded", "updates": "unreadable", "backlog": "unreadable", "record": "unreadable", "wiki": "unreadable"}
		if !reflect.DeepEqual(r.Parts, want) || len(r.Updates.Calls) != 7 || r.Updates.Calls[0].Cap != 0 {
			t.Fatalf("%+v %+v", r.Parts, r.Updates)
		}
	})
	t.Run("cut short", func(t *testing.T) {
		d, _ := reportHome(t)
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		r := Field(ctx, d, reportNow)
		if r.Parts["record"] != PartIncomplete || r.Parts["backlog"] != PartIncomplete || r.Parts["wiki"] != PartIncomplete {
			t.Fatalf("%+v", r.Parts)
		}
	})
}

// reportKeys are the report's keys as its format fixes them, each a path
// from the top; * stands for a map's keys and [] for a list's items.
var reportKeys = []string{
	"backlog", "backlog.hosts", "backlog.hosts.*", "backlog.hosts.*.bytes", "backlog.hosts.*.damaged", "backlog.hosts.*.oldest_age_hours",
	"backlog.hosts.*.oldest_day", "backlog.hosts.*.sessions", "backlog.hosts.*.untimed",
	"day", "format",
	"hooks", "hooks.delivered", "hooks.delivered.added", "hooks.delivered.chars", "hooks.delivered.fallback", "hooks.delivered.kinds",
	"hooks.delivered.kinds.*", "hooks.delivered.late", "hooks.delivered.pending", "hooks.delivered.routes", "hooks.delivered.routes.*",
	"hooks.delivered.stale", "hooks.delivered.stale_or_pending", "hooks.delivered.starts", "hooks.events", "hooks.events.*", "hooks.events.*.*",
	"hooks.events.*.*.days", "hooks.events.*.*.failed", "hooks.events.*.*.ms", "hooks.events.*.*.ms.[]", "hooks.events.*.*.p50_ms_within",
	"hooks.events.*.*.p95_ms_within", "hooks.events.*.*.runs", "hooks.from", "hooks.held_behind", "hooks.held_behind.*", "hooks.held_off_record",
	"hooks.held_off_record.*", "hooks.ms_bounds", "hooks.ms_bounds.[]", "hooks.to",
	"operations", "operations.commit_days", "operations.commit_reasons", "operations.commit_reasons.*", "operations.commits", "operations.commits.*",
	"operations.daily_job_days", "operations.from", "operations.hook_runs", "operations.hook_runs.*", "operations.hook_runs.*.*",
	"operations.hook_runs.*.*.failed", "operations.hook_runs.*.*.runs", "operations.notice_now", "operations.pushes", "operations.pushes.*",
	"operations.to", "operations.trust_flag", "operations.trust_flag.age_days", "operations.trust_flag.awaited", "operations.trust_flag.present",
	"parts", "parts.*",
	"record", "record.bytes", "record.bytes.*", "record.entries", "record.entries.*", "record.from", "record.markers", "record.markers.*",
	"record.passed_over", "record.replacements", "record.rules_version", "record.sessions", "record.sessions.*", "record.to", "record.would_catch",
	"record.would_catch.entries", "record.would_catch.kinds", "record.would_catch.kinds.*", "record.would_catch.sessions", "record.would_catch.tiers",
	"record.would_catch.tiers.*",
	"updates", "updates.busy", "updates.busy.by_trigger", "updates.busy.by_trigger.*", "updates.busy.jobs", "updates.busy.sessions", "updates.calls",
	"updates.calls.[]", "updates.calls.[].cap", "updates.calls.[].day", "updates.calls.[].hosts", "updates.calls.[].hosts.*", "updates.checked",
	"updates.duration_ms", "updates.duration_ms.max", "updates.duration_ms.median", "updates.duration_ms.n", "updates.duration_ms.p95",
	"updates.failures", "updates.failures.*", "updates.from", "updates.inside_accepted", "updates.inside_accepted.*", "updates.model_other",
	"updates.model_reported", "updates.outcomes", "updates.outcomes.*", "updates.passes", "updates.refused_whole", "updates.refused_whole.*",
	"updates.skips", "updates.skips.*", "updates.to", "updates.to_memory_ms", "updates.to_memory_ms.*", "updates.to_memory_ms.*.max",
	"updates.to_memory_ms.*.median", "updates.to_memory_ms.*.n", "updates.to_memory_ms.*.p95", "updates.tokens_in", "updates.tokens_in.max",
	"updates.tokens_in.median", "updates.tokens_in.n", "updates.tokens_in.p95", "updates.tokens_out", "updates.tokens_out.max",
	"updates.tokens_out.median", "updates.tokens_out.n", "updates.tokens_out.p95", "updates.unnamed",
	"version",
	"wiki", "wiki.code_status", "wiki.code_status.entries", "wiki.code_status.entries.*", "wiki.code_status.found", "wiki.code_status.pages",
	"wiki.code_status.pages_citing_code", "wiki.code_status.pages_no_clone", "wiki.complete", "wiki.edits", "wiki.edits_unplaced", "wiki.pages",
	"wiki.pages.*", "wiki.system_write_runs",
}

// typeKeys are the keys a value of t can have, as reportKeys writes them.
func typeKeys(t reflect.Type, prefix string, keys map[string]bool) {
	switch t.Kind() {
	case reflect.Pointer:
		typeKeys(t.Elem(), prefix, keys)
	case reflect.Map:
		keys[prefix+".*"] = true
		typeKeys(t.Elem(), prefix+".*", keys)
	case reflect.Slice:
		keys[prefix+".[]"] = true
		typeKeys(t.Elem(), prefix+".[]", keys)
	case reflect.Struct:
		for i := range t.NumField() {
			f := t.Field(i)
			name, _, _ := strings.Cut(f.Tag.Get("json"), ",")
			if f.Anonymous && name == "" {
				typeKeys(f.Type, prefix, keys)
				continue
			}
			if name == "" || name == "-" {
				continue
			}
			path := strings.TrimPrefix(prefix+"."+name, ".")
			keys[path] = true
			typeKeys(f.Type, path, keys)
		}
	}
}

// valueKeys are the keys of a decoded JSON value, the keys of the maps
// typeKeys gives as * given as * too.
func valueKeys(v any, prefix string, maps map[string]bool, keys map[string]bool) {
	switch v := v.(type) {
	case map[string]any:
		for k, item := range v {
			if maps[prefix+".*"] {
				k = "*"
			}
			path := strings.TrimPrefix(prefix+"."+k, ".")
			keys[path] = true
			valueKeys(item, path, maps, keys)
		}
	case []any:
		keys[prefix+".[]"] = true
		for _, item := range v {
			valueKeys(item, prefix+".[]", maps, keys)
		}
	}
}

// The report's keys are pinned: the format's type has exactly reportKeys,
// and a report of the fixture uses no key outside them. A change to a key
// must change this list and raise ReportFormat.
func TestFieldReportKeysArePinned(t *testing.T) {
	if ReportFormat != 1 {
		t.Fatalf("the format is %d; pin its keys here", ReportFormat)
	}
	typed := map[string]bool{}
	typeKeys(reflect.TypeFor[FieldReport](), "", typed)
	got := slices.Sorted(func(yield func(string) bool) {
		for k := range typed {
			if !yield(k) {
				return
			}
		}
	})
	want := slices.Sorted(slices.Values(reportKeys))
	if !slices.Equal(got, want) {
		t.Fatalf("the report's keys are\n%q\nnot\n%q", got, want)
	}
	d, _ := reportHome(t)
	var v any
	if err := json.Unmarshal(fieldJSON(t, d), &v); err != nil {
		t.Fatal(err)
	}
	maps := map[string]bool{}
	for _, k := range reportKeys {
		if strings.HasSuffix(k, ".*") {
			maps[k] = true
		}
	}
	used := map[string]bool{}
	valueKeys(v, "", maps, used)
	for k := range used {
		if !typed[k] {
			t.Errorf("the report uses the key %s, which its format lacks", k)
		}
	}
	if len(used) != len(typed) {
		t.Errorf("the fixture's report uses %d of the format's %d keys", len(used), len(typed))
	}
}
