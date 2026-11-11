package hook

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/heyNag/eva-md/internal/catchup"
	"github.com/heyNag/eva-md/internal/health"
	"github.com/heyNag/eva-md/internal/identity"
	"github.com/heyNag/eva-md/internal/knowledgepage"
	"github.com/heyNag/eva-md/internal/okfadapter"
	"github.com/heyNag/eva-md/internal/paths"
	"github.com/heyNag/eva-md/internal/threadpage"
)

// Tests of what a run's heartbeat records beside its time: what the hook
// held back, what it delivered, and the event's week.

// lastRun is the newest run an event's heartbeat records.
func lastRun(t *testing.T, env Env, host, event string) beatRun {
	t.Helper()
	runs := beatRuns(t, env, host, event)
	if len(runs) == 0 {
		t.Fatalf("no %s run recorded", event)
	}
	return runs[len(runs)-1]
}

// weekOf reads an event's week from its heartbeat.
func weekOf(t *testing.T, env Env, host, event string) []health.HookDay {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(env.Dirs.State, "heartbeats", host, event+".json"))
	if err != nil {
		t.Fatal(err)
	}
	var beat struct {
		Week []health.HookDay `json:"week"`
	}
	if err := json.Unmarshal(data, &beat); err != nil {
		t.Fatal(err)
	}
	return beat.Week
}

// behind makes transcript recovery report that it has not read to the end,
// as a pass with more to read does.
func behind(env Env) Env {
	env.recoverTranscript = func(context.Context, paths.Dirs, string, string) (int, error) {
		return 0, catchup.ErrPending
	}
	return env
}

// An event held behind recovery records what it kept none of: a person's
// prompt, with the chat's first brief while it is still owed; a reply; a
// tool line, of a failed call too; a chat start's brief. Each is counted
// in the event's day, by kind, as held while recovery was behind. A run
// that would have stored no content holds nothing back: a program's
// prompt, a built-in command, a Stop without a reply.
func TestHeldBehindRecoveryByKind(t *testing.T) {
	for _, host := range []string{"claude-code", "codex"} {
		t.Run(host, func(t *testing.T) {
			env, p := setup(t)
			held := behind(env)
			reply := "a reply"
			for _, tc := range []struct {
				name   string
				change func(p *payload)
				event  string
				want   []string
			}{
				{"first prompt", func(p *payload) {}, "UserPromptSubmit", []string{"prompt", "brief"}},
				{"reply", func(p *payload) { p.Event, p.Reply = "Stop", &reply }, "Stop", []string{"reply"}},
				{"tool", func(p *payload) { p.Event, p.Tool = "PostToolUse", "Bash" }, "PostToolUse", []string{"tool"}},
				{"start", func(p *payload) { p.Event, p.Source = "SessionStart", "resume" }, "SessionStart", []string{"brief"}},
				{"agent's prompt", func(p *payload) { p.Agent = "agent-1" }, "UserPromptSubmit", nil},
				{"built-in command", func(p *payload) { p.Prompt = "/compact" }, "UserPromptSubmit", nil},
				{"stop without reply", func(p *payload) { p.Event = "Stop" }, "Stop", nil},
			} {
				q := p
				tc.change(&q)
				invoke(t, host, q, held)
				run := lastRun(t, env, host, tc.event)
				if !reflect.DeepEqual(run.Held, tc.want) || len(tc.want) > 0 && run.HeldBy != health.HeldByRecovery || len(tc.want) == 0 && run.HeldBy != "" || run.Delivered != nil {
					t.Fatalf("%s: run %+v, want held %v by recovery", tc.name, run, tc.want)
				}
			}
			if host == "claude-code" {
				q := p
				q.Event, q.Tool = "PostToolUseFailure", "Bash"
				invoke(t, host, q, held)
				if run := lastRun(t, env, host, "PostToolUseFailure"); !reflect.DeepEqual(run.Held, []string{"tool"}) || run.HeldBy != health.HeldByRecovery {
					t.Fatalf("failed tool call: %+v", run)
				}
			}
			// Once the chat has its brief, a held prompt owes none.
			p.Event, p.Source = "SessionStart", "startup"
			invoke(t, host, p, env)
			p.Event, p.Source = "UserPromptSubmit", ""
			invoke(t, host, p, held)
			if run := lastRun(t, env, host, "UserPromptSubmit"); !reflect.DeepEqual(run.Held, []string{"prompt"}) || run.HeldBy != health.HeldByRecovery {
				t.Fatalf("prompt after the brief: %+v", run)
			}
			// The prompt's day counts both held prompts and the one brief.
			week := weekOf(t, env, host, "UserPromptSubmit")
			if len(week) != 1 || week[0].Runs != 4 || !reflect.DeepEqual(week[0].Behind, []int{2, 0, 0, 1}) || !reflect.DeepEqual(week[0].OffRecord, []int{0, 0, 0, 0}) {
				t.Fatalf("week %+v", week)
			}
			for event, want := range map[string][]int{"Stop": {0, 1, 0, 0}, "PostToolUse": {0, 0, 1, 0}, "SessionStart": {0, 0, 0, 1}} {
				if week := weekOf(t, env, host, event); len(week) != 1 || !reflect.DeepEqual(week[0].Behind, want) {
					t.Fatalf("%s week %+v", event, week)
				}
			}
			// A program's prompt, in a chat a program runs, is stored as a
			// host prompt, so it holds none back.
			if host == "claude-code" {
				t.Setenv("CLAUDE_CODE_ENTRYPOINT", "sdk-cli")
				p.Turn = "turn-program"
				invoke(t, host, p, held)
				if run := lastRun(t, env, host, "UserPromptSubmit"); run.Held != nil || run.HeldBy != "" || run.Delivered != nil {
					t.Fatalf("program's prompt: %+v", run)
				}
			}
		})
	}
}

// A chat off the record holds back what it would have stored, recorded
// apart from what recovery held back; so does one that recovery finds was
// put off the record by a control the hooks missed. A chat on the record
// that recovery has caught up with holds nothing.
func TestHeldOffTheRecord(t *testing.T) {
	env, p := setup(t)
	p.Prompt = "off the record"
	invoke(t, "codex", p, env)
	p.Prompt, p.Turn = "a private question", "turn-2"
	invoke(t, "codex", p, env)
	if run := lastRun(t, env, "codex", "UserPromptSubmit"); !reflect.DeepEqual(run.Held, []string{"prompt", "brief"}) || run.HeldBy != health.HeldByOffRecord {
		t.Fatalf("off the record: %+v", run)
	}
	reply := "a private answer"
	p.Event, p.Reply = "Stop", &reply
	invoke(t, "codex", p, env)
	if run := lastRun(t, env, "codex", "Stop"); !reflect.DeepEqual(run.Held, []string{"reply"}) || run.HeldBy != health.HeldByOffRecord {
		t.Fatalf("off the record: %+v", run)
	}
	if week := weekOf(t, env, "codex", "UserPromptSubmit"); !reflect.DeepEqual(week[0].OffRecord, []int{1, 0, 0, 1}) || !reflect.DeepEqual(week[0].Behind, []int{0, 0, 0, 0}) {
		t.Fatalf("week %+v", week)
	}

	// A control the hooks missed, found by recovery.
	env, p = setup(t)
	file := registeredTranscript(t, env, &p)
	appendRows(t, file, map[string]any{"type": "user", "sessionId": p.Session, "cwd": p.Cwd, "timestamp": env.Now().Format(time.RFC3339), "promptId": "turn-2", "message": map[string]any{"role": "user", "content": "off the record"}})
	p.Event, p.Tool = "PostToolUse", "Read"
	invoke(t, "claude-code", p, env)
	if run := lastRun(t, env, "claude-code", "PostToolUse"); !reflect.DeepEqual(run.Held, []string{"tool"}) || run.HeldBy != health.HeldByOffRecord {
		t.Fatalf("missed control: %+v", run)
	}
	// The SessionStart that registered the transcript held nothing.
	if run := lastRun(t, env, "claude-code", "SessionStart"); run.Held != nil || run.HeldBy != "" {
		t.Fatalf("start on the record: %+v", run)
	}

	// Recovery found a control the hooks missed and could not save the
	// boundary it sets: what the run held back, it held back because the
	// chat went off the record, not because recovery was behind.
	env, p = setup(t)
	boundary := env
	boundary.recoverTranscript = func(context.Context, paths.Dirs, string, string) (int, error) {
		return 0, catchup.ErrPrivacyBoundary
	}
	reply = "an answer after the control"
	p.Event, p.Reply = "Stop", &reply
	invoke(t, "codex", p, boundary)
	if run := lastRun(t, env, "codex", "Stop"); !reflect.DeepEqual(run.Held, []string{"reply"}) || run.HeldBy != health.HeldByOffRecord {
		t.Fatalf("unsaved boundary: %+v", run)
	}
	if week := weekOf(t, env, "codex", "Stop"); !reflect.DeepEqual(week[0].OffRecord, []int{0, 1, 0, 0}) || !reflect.DeepEqual(week[0].Behind, []int{0, 0, 0, 0}) {
		t.Fatalf("unsaved boundary week %+v", week)
	}
}

// writeRules saves the rules for all projects, as the update writes them.
func writeRules(t *testing.T, env Env, rules ...string) {
	t.Helper()
	var ops []threadpage.RuleOp
	for i, r := range rules {
		ops = append(ops, threadpage.RuleOp{General: true, Action: "add", Text: r, Tag: "stated", Refs: []string{fmt.Sprintf("s:0000aaaa#%d", i+1)}, At: env.Now().Add(-time.Hour)})
	}
	page, _ := threadpage.ApplyRules(threadpage.RulesPage{Title: "rules"}, ops, true)
	w, err := okfadapter.Open(env.Config.WikiPath, env.Dirs.Locks())
	if err != nil {
		t.Fatal(err)
	}
	if err := w.Create("general/rules", &okfadapter.Concept{Type: "rules", Title: "rules", Status: "stable", Body: threadpage.RenderRules(page)}, "eva-md/test"); err != nil {
		t.Fatal(err)
	}
}

// fullWiki makes a wiki with every part a brief or a prompt can show: the
// rules for all projects, constraints and other knowledge, system pages,
// and the general area's Now, written at asOf; and an earlier chat, planted
// as codex's previous-session, whose newest exchange is 3 minutes before
// the test's clock.
func fullWiki(t *testing.T, env *Env, asOf time.Time) {
	t.Helper()
	knowledgeWiki(t, env, fourSystems, facts)
	writeRules(t, *env, "Always run go vet before committing")
	at := *env
	at.Now = func() time.Time { return asOf }
	generalNow(t, &at, "### Next\n- Write the parser")
	plant(t, *env, "codex", "previous-session")
}

// Each route that adds text to a chat is recorded on the run that added it,
// with the characters added: a chat start's brief shows the rules, the
// constraints, the state, the exchanges and the system pointers; a prompt's
// lines add pointers, a recalled statement or the reminder; a refresh adds
// the changed state. A chat start says whether its state was older than the
// newest exchange and whether an update was pending.
func TestDeliveryRecordsEachRoute(t *testing.T) {
	for _, host := range []string{"claude-code", "codex"} {
		t.Run(host, func(t *testing.T) {
			env, p := setup(t)
			fullWiki(t, &env, env.Now())
			env.Config.ReminderEvery = 2
			p.Event, p.Source = "SessionStart", "startup"
			out := invoke(t, host, p, env)
			got := lastRun(t, env, host, "SessionStart").Delivered
			want := &health.Delivery{Kind: "startup", Routes: []string{"rules", "constraints", "state", "exchanges", "pointers"}, Chars: len([]rune(contextFrom(t, out))), Pending: true}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("start delivered %+v, want %+v", got, want)
			}

			p.Event, p.Source = "UserPromptSubmit", ""
			for i, tc := range []struct {
				prompt string
				routes []string
			}{
				{"How do Ledger and Payments talk?", []string{"pointers"}},
				{"Does the inbox store entries as JSONL, one file per session?", []string{"recall"}},
				{"thanks, carry on", []string{"reminder"}},
				{"carry on again", nil},
			} {
				p.Prompt, p.Turn = tc.prompt, fmt.Sprint("turn-", i)
				out := invoke(t, host, p, env)
				got := lastRun(t, env, host, "UserPromptSubmit").Delivered
				want := &health.Delivery{Kind: "prompt", Routes: tc.routes}
				if out != "" {
					want.Chars = len([]rune(contextFrom(t, out)))
				}
				if !reflect.DeepEqual(got, want) || (out == "") != (tc.routes == nil) {
					t.Fatalf("prompt %q delivered %+v, want %+v (output %q)", tc.prompt, got, want, out)
				}
			}
			// The fifth prompt gets the reminder too.
			generalNow(t, &env, "### Next\n- Test the parser")
			p.Prompt, p.Turn = "carry on once more", "turn-refresh"
			out = invoke(t, host, p, env)
			got = lastRun(t, env, host, "UserPromptSubmit").Delivered
			if want := (&health.Delivery{Kind: "prompt", Routes: []string{"state", "reminder"}, Chars: len([]rune(contextFrom(t, out)))}); !reflect.DeepEqual(got, want) {
				t.Fatalf("refresh delivered %+v, want %+v", got, want)
			}

			// The prompt's day counts its runs by route; the starts are
			// counted in the start's day.
			week := weekOf(t, env, host, "UserPromptSubmit")
			if len(week) != 1 || week[0].Kinds[5] != 5 || week[0].Added != 4 || !reflect.DeepEqual(week[0].Routes, []int{0, 0, 1, 0, 1, 1, 2}) || week[0].Briefs != 0 {
				t.Fatalf("prompt week %+v", week)
			}
			week = weekOf(t, env, host, "SessionStart")
			if len(week) != 1 || week[0].Kinds[0] != 1 || week[0].Briefs != 1 || week[0].WithState != 1 || week[0].Pending != 1 || week[0].Stale != 0 || week[0].StaleOrPending != 1 || !reflect.DeepEqual(week[0].Routes, []int{1, 1, 1, 1, 0, 1, 0}) {
				t.Fatalf("start week %+v", week)
			}
		})
	}
}

// A chat start whose Now predates the newest captured exchange records its
// state as stale, and one with no update pending as not pending; each kind
// of start is recorded as such.
func TestDeliveryRecordsAStaleState(t *testing.T) {
	env, p := setup(t)
	fullWiki(t, &env, env.Now().Add(-time.Hour))
	env.Config.Updates = false
	for _, source := range []string{"clear", "compact", "fork", "resume", "startup", "other"} {
		p.Event, p.Source, p.Session = "SessionStart", source, "chat-"+source
		invoke(t, "codex", p, env)
		got := lastRun(t, env, "codex", "SessionStart").Delivered
		switch source {
		case "clear", "startup", "other":
			kind := source
			if source == "other" {
				kind = "startup"
			}
			if got == nil || got.Kind != kind || !got.Stale || got.Pending || !strings.Contains(strings.Join(got.Routes, " "), "state exchanges") {
				t.Fatalf("%s delivered %+v", source, got)
			}
		default:
			// A compaction, fork or resume of a chat with no history of
			// its own shows no exchange of another chat.
			if got == nil || got.Kind != source || got.Stale || got.Pending {
				t.Fatalf("%s delivered %+v", source, got)
			}
		}
	}
	week := weekOf(t, env, "codex", "SessionStart")
	if len(week) != 1 || !reflect.DeepEqual(week[0].Kinds, []int{2, 1, 1, 1, 1, 0}) || week[0].Briefs != 4 || week[0].WithState != 4 || week[0].Stale != 3 || week[0].StaleOrPending != 3 {
		t.Fatalf("week %+v", week)
	}
}

// A prompt that carries the chat's first brief, because the chat's start
// gave none, is recorded as the first-prompt fallback, with the brief's
// routes and freshness and the prompt's own lines.
func TestDeliveryRecordsTheFirstPromptFallback(t *testing.T) {
	env, p := setup(t)
	fullWiki(t, &env, env.Now().Add(-time.Hour))
	p.Prompt = "How do Ledger and Payments talk?"
	out := invoke(t, "claude-code", p, env)
	got := lastRun(t, env, "claude-code", "UserPromptSubmit").Delivered
	want := &health.Delivery{Kind: "prompt", Routes: []string{"rules", "constraints", "state", "exchanges", "pointers"}, Chars: len([]rune(contextFrom(t, out))), Stale: true, Pending: true, Fallback: true}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("fallback delivered %+v, want %+v", got, want)
	}
	if week := weekOf(t, env, "claude-code", "UserPromptSubmit"); week[0].Fallback != 1 || week[0].Briefs != 1 || week[0].WithState != 1 || week[0].StaleOrPending != 1 {
		t.Fatalf("week %+v", week)
	}
	// The next prompt is no fallback, and no start.
	p.Prompt, p.Turn = "carry on", "turn-2"
	invoke(t, "claude-code", p, env)
	if got := lastRun(t, env, "claude-code", "UserPromptSubmit").Delivered; got == nil || got.Fallback || got.Stale || got.Pending {
		t.Fatalf("second prompt delivered %+v", got)
	}
	if week := weekOf(t, env, "claude-code", "UserPromptSubmit"); week[0].Fallback != 1 || week[0].Briefs != 1 {
		t.Fatalf("week %+v", week)
	}
}

// A prompt's lines go only once the chat's state that records them is
// saved, and so do their routes: a first prompt whose state can't be saved
// delivers its brief and records the brief's routes alone, where one whose
// state is saved records the recalled statement too.
func TestDeliveryRecordsNoLinesNotSaved(t *testing.T) {
	env, p := setup(t)
	fullWiki(t, &env, env.Now())
	p.Prompt = "Does the inbox store entries as JSONL, one file per session?"
	p.Session = "saved-session"
	invoke(t, "claude-code", p, env)
	if got := lastRun(t, env, "claude-code", "UserPromptSubmit").Delivered; got == nil || !slices.Contains(got.Routes, "recall") {
		t.Fatalf("saved state delivered %+v", got)
	}

	// The prompts marker reads, and can't be written.
	p.Session = "unsaved-session"
	file := filepath.Join(env.Dirs.Sessions(), "claude-code", p.Session, promptsMarker)
	if err := os.MkdirAll(filepath.Dir(file), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, []byte(`{"prompts":3}`), 0o400); err != nil {
		t.Fatal(err)
	}
	out := invoke(t, "claude-code", p, env)
	got := lastRun(t, env, "claude-code", "UserPromptSubmit").Delivered
	want := &health.Delivery{Kind: "prompt", Routes: []string{"rules", "constraints", "state", "exchanges", "pointers"}, Chars: len([]rune(contextFrom(t, out))), Pending: true, Fallback: true}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unsaved state delivered %+v, want %+v", got, want)
	}
}

// A run that passes its deadline delivers nothing and is recorded so: a
// chat start whose capture ran out of time, recorded once its output was
// written, and a brief built too close to the deadline to save its marker.
// A run that could add nothing, such as a program's prompt, records no
// delivery.
func TestDeliveryRecordsALateRun(t *testing.T) {
	env, p := setup(t)
	p.Event, p.Source = "SessionStart", "clear"
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	release := make(chan struct{})
	held := env
	held.recoverTranscript = func(context.Context, paths.Dirs, string, string) (int, error) {
		cancel()
		<-release
		return 0, nil
	}
	out := invokeContext(t, ctx, "codex", p, held)
	close(release)
	awaitNoCapture(t, "the capture Run gave up on")
	if out != "" {
		t.Fatalf("late start returned %q", out)
	}
	run := lastRun(t, env, "codex", "SessionStart")
	if want := (&health.Delivery{Kind: "clear", Late: true}); !run.Failed || !reflect.DeepEqual(run.Delivered, want) {
		t.Fatalf("late start %+v", run)
	}
	if week := weekOf(t, env, "codex", "SessionStart"); week[0].Late != 1 || week[0].Briefs != 0 || week[0].Kinds[1] != 1 || week[0].Failed != 1 {
		t.Fatalf("week %+v", week)
	}

	// Too close to the deadline to save the briefed marker.
	plant(t, env, "codex", "previous-session")
	p.Event, p.Source, p.Session = "UserPromptSubmit", "", "late-session"
	p.record = new(runRecord)
	if out := resumeContext(nearDeadline{context.Background()}, "claude-code", p, env, identity.General(), env.Now(), startState{}, new([][]string)); out != "" {
		t.Fatalf("brief given too close to the deadline: %q", out)
	}
	if want := (&health.Delivery{Kind: "prompt", Fallback: true, Late: true}); !reflect.DeepEqual(p.record.delivery, want) {
		t.Fatalf("brief too close to the deadline: %+v", p.record.delivery)
	}

	// A late prompt that no person typed could add nothing: an agent's, and
	// a program's. Each part has variables of its own, since the closure of
	// an earlier part may still be running in the capture Run gave up on.
	for _, tc := range []struct {
		name   string
		change func(t *testing.T, p *payload)
	}{
		{"agent's prompt", func(_ *testing.T, p *payload) { p.Agent = "agent-1" }},
		{"program's prompt", func(t *testing.T, _ *payload) { t.Setenv("CLAUDE_CODE_ENTRYPOINT", "sdk-cli") }},
	} {
		env, p := setup(t)
		tc.change(t, &p)
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		release := make(chan struct{})
		held := env
		held.recoverTranscript = func(context.Context, paths.Dirs, string, string) (int, error) {
			cancel()
			<-release
			return 0, nil
		}
		invokeContext(t, ctx, "claude-code", p, held)
		close(release)
		awaitNoCapture(t, "the capture Run gave up on")
		if run := lastRun(t, env, "claude-code", "UserPromptSubmit"); !run.Failed || run.Delivered != nil {
			t.Fatalf("late %s %+v", tc.name, run)
		}
	}
}

// maxBeatSize is the most a heartbeat's file holds: its last maxRuns runs,
// each with the largest record, and a week of days whose every count has 8
// digits, with the longest entrypoint a heartbeat keeps. It is below the
// 16 KiB the hook and the doctor read of a heartbeat.
const maxBeatSize = 12 << 10

// The week stays within its days and the file within maxBeatSize: a
// simulated month of runs, a day at a time on an injected clock, keeps the
// last 7 days and the last 20 runs, and a heartbeat whose week has the
// largest counts, written by an older run, stays within the bound once a
// run is added.
func TestWeekStaysBounded(t *testing.T) {
	env, _ := setup(t)
	p := payload{Event: "UserPromptSubmit", Transcript: "/t", entrypoint: strings.Repeat("e", 32), headless: true}
	largest := func() *runRecord {
		return &runRecord{held: []string{"prompt", "brief"}, heldBy: health.HeldByOffRecord,
			delivery: &health.Delivery{Kind: "compact", Routes: health.Routes, Chars: 99_999_999, Stale: true, Pending: true, Fallback: true, Late: true}}
	}
	file := filepath.Join(env.Dirs.State, "heartbeats", "codex", "UserPromptSubmit.json")
	start := time.Date(2026, 10, 1, 23, 30, 0, 0, time.UTC)
	for day := 0; day < 30; day++ {
		for run := 0; run < 3; run++ {
			now := start.AddDate(0, 0, day).Add(time.Duration(run) * 20 * time.Minute)
			p.record = largest()
			heartbeat(context.Background(), env.Dirs, "codex", p, run == 0, now, 3500*time.Millisecond)
			info, err := os.Stat(file)
			if err != nil || info.Size() > maxBeatSize {
				t.Fatalf("day %d run %d: %v, size %v", day, run, err, info)
			}
		}
	}
	week := weekOf(t, env, "codex", "UserPromptSubmit")
	if len(week) != 7 || len(beatRuns(t, env, "codex", "UserPromptSubmit")) != maxRuns {
		t.Fatalf("kept %d days and %d runs", len(week), len(beatRuns(t, env, "codex", "UserPromptSubmit")))
	}
	for i, d := range week {
		// Each day's runs fall at 23:30 and 23:50 UTC and 00:10 the next
		// day, so the last day of the month has one run, and the others
		// three, the first of which failed.
		runs, failed := 3, 1
		if i == 6 {
			runs, failed = 1, 0
		}
		if want := start.AddDate(0, 0, 24+i).Format(time.DateOnly); d.Day != want || d.Runs != runs || d.Failed != failed || d.MS[len(d.MS)-1] != runs || d.OffRecord[0] != runs {
			t.Fatalf("day %d: %+v, want %s with %d runs", i, d, want, runs)
		}
	}

	// The largest week an older run can leave.
	end := start.AddDate(0, 0, 30)
	var days []health.HookDay
	for i := 0; i < health.WeekDays; i++ {
		d := health.HookDay{Day: end.AddDate(0, 0, i+1-health.WeekDays).Format(time.DateOnly)}
		fill := func(n int) []int {
			counts := make([]int, n)
			for j := range counts {
				counts[j] = 99_999_999
			}
			return counts
		}
		d.MS, d.Behind, d.OffRecord, d.Kinds, d.Routes = fill(len(health.DurationBounds)+1), fill(4), fill(4), fill(6), fill(7)
		d.Runs, d.Failed, d.Added, d.Chars, d.Briefs, d.WithState, d.Stale, d.Pending, d.StaleOrPending, d.Fallback, d.Late = 99_999_999, 99_999_999, 99_999_999, 99_999_999, 99_999_999, 99_999_999, 99_999_999, 99_999_999, 99_999_999, 99_999_999, 99_999_999
		days = append(days, d)
	}
	var old map[string]any
	data, err := os.ReadFile(file)
	if err != nil || json.Unmarshal(data, &old) != nil {
		t.Fatal(err)
	}
	old["week"] = days
	if data, err = json.Marshal(old); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, data, 0o600); err != nil {
		t.Fatal(err)
	}
	p.record = largest()
	heartbeat(context.Background(), env.Dirs, "codex", p, true, end, 3500*time.Millisecond)
	info, err := os.Stat(file)
	if err != nil || info.Size() > maxBeatSize {
		t.Fatalf("largest week: %v, size %d of %d", err, info.Size(), maxBeatSize)
	}
	if week := weekOf(t, env, "codex", "UserPromptSubmit"); len(week) != 7 || week[6].Runs != 100_000_000 {
		t.Fatalf("largest week %+v", week[len(week)-1])
	}
	t.Logf("largest heartbeat: %d bytes", info.Size())
}

// marker is planted in every text the counters read.
const marker = "zqxmarker"

// The records, the week and the doctor's rows hold none of the text the
// hooks read to count what they held back and delivered: not the prompts,
// replies, tool lines or transcript, not the wiki's pages, and not the
// folder, project, branch or session the chat ran in.
func TestRecordsHoldNoContent(t *testing.T) {
	env, p := setup(t)
	env.Now = time.Now
	knowledgeWiki(t, &env, map[string]string{"zqx": systemPage("Zqxmarker service", "Holds the zqxmarker books", nil, marker+"-alias")},
		[]fact{{knowledgepage.Constraint, knowledgepage.ScopeAll, "", marker + " title", marker + " statement that hooks keep"}})
	writeRules(t, env, "Always "+marker+" before committing")
	generalNow(t, &env, "### Next\n- "+marker+" next step")
	env.Config.ReminderEvery = 1

	// A project whose folder, remote and branch hold the marker.
	project := filepath.Join(os.Getenv("HOME"), marker+"-folder")
	if err := os.Mkdir(project, 0o700); err != nil {
		t.Fatal(err)
	}
	git(t, project, "init", "-q", "-b", marker+"-branch")
	git(t, project, "remote", "add", "origin", "git@github.com:acme/"+marker+"-repo.git")
	reply := "the " + marker + " reply"
	for _, cwd := range []string{p.Cwd, project} {
		p := p
		p.Cwd, p.Session = cwd, "sess-"+marker+filepath.Base(cwd)
		send := func(env Env, q payload) {
			t.Helper()
			invoke(t, "claude-code", q, env)
			invoke(t, "codex", q, env)
		}
		start, prompt, stop, tool, agent := p, p, p, p, p
		// The start's source, which picks the kind recorded, holds the
		// marker too, and reads as a startup.
		start.Event, start.Source = "SessionStart", marker+"-source"
		prompt.Prompt = "Does " + marker + " hold the zqxmarker books?"
		stop.Event, stop.Reply = "Stop", &reply
		tool.Event, tool.Tool, tool.ToolInput = "PostToolUse", "Bash", json.RawMessage(`{"command":"echo `+marker+`"}`)
		agent.Prompt, agent.Agent, agent.Turn = "the "+marker+" task", marker+"-agent", "turn-agent"
		for _, q := range []payload{start, prompt, stop, tool, agent} {
			send(behind(env), q)
		}
		for _, q := range []payload{start, prompt, stop, tool, agent} {
			send(env, q)
		}
		off := prompt
		off.Prompt, off.Turn = "off the record", "turn-off"
		send(env, off)
		prompt.Turn = "turn-off-2"
		send(env, prompt)
	}

	var found []string
	for _, report := range []health.Report{health.Status(context.Background(), env.Dirs), health.Inspect(context.Background(), env.Dirs, false)} {
		// The rows count what was held back on both counts, and what was
		// delivered by the routes the wiki holds.
		var held, delivered string
		for _, c := range report.Checks {
			switch c.Name {
			case "hooks held back":
				held = c.Detail
			case "hooks delivered":
				delivered = c.Detail
			}
		}
		if !strings.Contains(held, "while transcript recovery was behind: 4 prompts, 4 replies, 4 tool lines, 8 briefs; while a chat was off the record: 4 prompts") ||
			!strings.Contains(delivered, "(4 startups, 4 prompts), 8 added") || !strings.Contains(delivered, "rules 4, constraints 4, state 2, pointers 4, reminder 4") {
			t.Fatalf("rows:\n%s\n%s", held, delivered)
		}
		data, err := json.Marshal(report)
		if err != nil {
			t.Fatal(err)
		}
		found = append(found, leaks(string(data), os.Getenv("HOME"))...)
	}
	err := filepath.WalkDir(filepath.Join(env.Dirs.State, "heartbeats"), func(path string, entry os.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, leak := range leaks(string(data), os.Getenv("HOME")) {
			found = append(found, filepath.Base(path)+": "+leak)
		}
		return nil
	})
	if err != nil || len(found) > 0 {
		t.Fatalf("content in the records or rows: %v %q", err, found)
	}
}

// leaks names what of the planted text, or of the home's path, s holds.
func leaks(s, home string) []string {
	var found []string
	for _, text := range []string{marker, "sess-", home} {
		if strings.Contains(strings.ToLower(s), strings.ToLower(text)) {
			found = append(found, text)
		}
	}
	return found
}
