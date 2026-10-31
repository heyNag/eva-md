package hook

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/heyNag/eva-md/internal/inbox"
	"github.com/heyNag/eva-md/internal/resume"
)

// The update triggers, seen through the injected spawn: which events start
// eva-md update, for which sessions, under which state of the inbox.

var hosts = []string{"claude-code", "codex"}

// triggerSetup is setup with a wiki set up, so that updates are on, and
// with the commands the hooks start recorded, which the returned function
// reads.
func triggerSetup(t *testing.T) (Env, payload, func() [][]string) {
	t.Helper()
	env, p := setup(t)
	env.slack = busySlack
	env.Config.WikiPath = filepath.Join(os.Getenv("HOME"), "wiki")
	var runs [][]string
	env.spawn = func(args ...string) { runs = append(runs, append([]string(nil), args...)) }
	return env, p, func() [][]string { return runs }
}

// sessionFile writes a session file of exactly size bytes of valid entries
// for another chat in the project, its watermark m, and its modification
// time, and returns its path.
func sessionFile(t *testing.T, env Env, host, id string, size int64, m inbox.Mark, mod time.Time) string {
	t.Helper()
	store := inbox.Store{Root: env.Dirs.Inbox()}
	file := store.Path("general", host, id)
	if err := os.MkdirAll(filepath.Dir(file), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, padding(t, env, host, id, size), 0o600); err != nil {
		t.Fatal(err)
	}
	if m != (inbox.Mark{}) {
		if err := store.SetMark(file, m); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Chtimes(file, mod, mod); err != nil {
		t.Fatal(err)
	}
	return file
}

// padding is one event entry of the session, exactly size bytes long with
// its newline.
func padding(t *testing.T, env Env, host, id string, size int64) []byte {
	t.Helper()
	e := inbox.Entry{V: 1, TS: env.Now().Add(-time.Hour).Format(time.RFC3339), Host: host, Session: id, Role: "event", Kind: "event", Project: "general", Branch: "default", Origin: "hook"}
	base, _ := json.Marshal(e)
	n := size - int64(len(base)) - int64(len(`,"text":""`)) - 1
	if n < 1 {
		t.Fatalf("a session file can't be as short as %d bytes", size)
	}
	e.Text = strings.Repeat("x", int(n))
	data, _ := json.Marshal(e)
	if data = append(data, '\n'); int64(len(data)) != size {
		t.Fatalf("padding is %d bytes, want %d", len(data), size)
	}
	return data
}

func fileSize(t *testing.T, file string) int64 {
	t.Helper()
	info, err := os.Stat(file)
	if err != nil {
		t.Fatal(err)
	}
	return info.Size()
}

func updateOf(sessions ...string) [][]string {
	args := []string{"update"}
	for _, s := range sessions {
		args = append(args, "--session", s)
	}
	return [][]string{args}
}

// SessionEnd and PreCompact start an update for the session; the other
// events start none on their own, though the state the other triggers look
// for is there: the chat's own file, just changed, holds 120 KB unprocessed,
// which only Stop's trigger takes, and another session of the project has
// been idle an hour with a backlog, which only SessionStart's catch-up
// takes. A chat off the record starts nothing: the catch-up at a later
// chat's start takes its backlog.
func TestEndAndCompactSpawnAnUpdate(t *testing.T) {
	for _, host := range hosts {
		own := updateOf(host + ":test-session")
		for _, tc := range []struct {
			event string
			off   bool
			want  [][]string
		}{
			{"SessionEnd", false, own},
			{"PreCompact", false, own},
			{"SessionEnd", true, nil},
			{"PreCompact", true, nil},
			{"Stop", false, own},
			{"SessionStart", false, updateOf("codex:idle-other")},
			{"PostCompact", false, nil},
			{"PostToolUse", false, nil},
			{"PostToolUseFailure", false, nil},
			{"UserPromptSubmit", false, nil},
			{"PostModelSwitch", false, nil},
			{"StopFailure", false, nil},
		} {
			t.Run(fmt.Sprintf("%s/%s/off=%v", host, tc.event, tc.off), func(t *testing.T) {
				env, p, spawned := triggerSetup(t)
				now := env.Now()
				sessionFile(t, env, host, p.Session, 2*stopBacklog, inbox.Mark{}, now)
				sessionFile(t, env, "codex", "idle-other", 1000, inbox.Mark{}, now.Add(-time.Hour))
				if tc.off {
					p.Prompt = "off the record"
					invoke(t, host, p, env)
					p.Prompt = "please help"
				}
				reply := "Done."
				p.Event, p.Reply, p.Summary, p.Tool = tc.event, &reply, "host summary", "Bash"
				invoke(t, host, p, env)
				if got := spawned(); !reflect.DeepEqual(got, tc.want) {
					t.Fatalf("spawned %q, want %q", got, tc.want)
				}
			})
		}
	}
}

// Stop starts an update once 60 KB or more of the session's file is past
// its watermark, measured after the reply is stored, and not a byte before.
func TestStopSpawnsAt60KBUnprocessed(t *testing.T) {
	for _, host := range hosts {
		for _, tc := range []struct {
			name            string
			unprocessed, wm int64
			want            bool
		}{
			{"just below", stopBacklog - 1, 0, false},
			{"at 60 KB", stopBacklog, 0, true},
			{"large file, watermark just past", stopBacklog - 1, 10 << 10, false},
			{"large file, watermark at", stopBacklog, 10 << 10, true},
		} {
			t.Run(host+"/"+tc.name, func(t *testing.T) {
				env, p, spawned := triggerSetup(t)
				reply := "Finished the step."
				p.Event, p.Reply = "Stop", &reply
				// The first reply shows how long the stored line is; the
				// second, identical, comes after padding that leaves
				// exactly the bytes the case needs.
				invoke(t, host, p, env)
				file := inbox.Store{Root: env.Dirs.Inbox()}.Path("general", host, p.Session)
				line := fileSize(t, file)
				pad := tc.unprocessed + tc.wm - 2*line
				f, err := os.OpenFile(file, os.O_APPEND|os.O_WRONLY, 0)
				if err != nil {
					t.Fatal(err)
				}
				if _, err := f.Write(padding(t, env, host, p.Session, pad)); err != nil {
					t.Fatal(err)
				}
				f.Close()
				if tc.wm > 0 {
					if err := (inbox.Store{Root: env.Dirs.Inbox()}).SetMark(file, inbox.Mark{Offset: tc.wm}); err != nil {
						t.Fatal(err)
					}
				}
				if got := spawned(); got != nil {
					t.Fatalf("the first reply spawned %q", got)
				}
				invoke(t, host, p, env)
				if size := fileSize(t, file); size-tc.wm != tc.unprocessed {
					t.Fatalf("%d bytes unprocessed, want %d", size-tc.wm, tc.unprocessed)
				}
				var want [][]string
				if tc.want {
					want = updateOf(host + ":" + p.Session)
				}
				if got := spawned(); !reflect.DeepEqual(got, want) {
					t.Fatalf("spawned %q, want %q", got, want)
				}
			})
		}
	}
}

// A prompt 30 minutes or more after the session's file last changed starts
// an update when 600 bytes or more of it wait past the watermark, read
// before the prompt is stored. A backlog an update found too small, a host
// or scheduled prompt and a built-in command start nothing.
func TestPromptAfterAGapSpawns(t *testing.T) {
	for _, host := range hosts {
		for _, tc := range []struct {
			name   string
			gap    time.Duration
			size   int64
			mark   inbox.Mark
			prompt string
			agent  string
			want   bool
		}{
			{"gap and backlog", promptGap, promptBacklog, inbox.Mark{}, "please help", "", true},
			{"gap just short", promptGap - time.Second, promptBacklog, inbox.Mark{}, "please help", "", false},
			{"backlog just short", promptGap, promptBacklog - 1, inbox.Mark{}, "please help", "", false},
			{"backlog past the watermark just short", promptGap, 3 * promptBacklog, inbox.Mark{Offset: 2*promptBacklog + 1}, "please help", "", false},
			{"backlog past the watermark", promptGap, 3 * promptBacklog, inbox.Mark{Offset: 2 * promptBacklog}, "please help", "", true},
			{"backlog found too small", promptGap, 3 * promptBacklog, inbox.Mark{Offset: 100, Wait: 3 * promptBacklog}, "please help", "", false},
			{"subagent prompt", promptGap, promptBacklog, inbox.Mark{}, "please help", "agent-1", false},
			{"scheduled prompt", promptGap, promptBacklog, inbox.Mark{}, "<task-notification>done</task-notification>", "", false},
			{"built-in command", promptGap, promptBacklog, inbox.Mark{}, "/model", "", false},
		} {
			t.Run(host+"/"+tc.name, func(t *testing.T) {
				env, p, spawned := triggerSetup(t)
				sessionFile(t, env, host, p.Session, tc.size, tc.mark, env.Now().Add(-tc.gap))
				p.Prompt, p.Agent = tc.prompt, tc.agent
				invoke(t, host, p, env)
				var want [][]string
				if tc.want {
					want = updateOf(host + ":" + p.Session)
				}
				if got := spawned(); !reflect.DeepEqual(got, want) {
					t.Fatalf("spawned %q, want %q", got, want)
				}
			})
		}
	}
}

// The prompt trigger reads the session's file before the hook appends
// anything: a reply transcript recovery stores first doesn't hide the gap.
func TestGapIsReadBeforeRecovery(t *testing.T) {
	env, p, spawned := triggerSetup(t)
	transcript := registeredTranscript(t, env, &p)
	file := inbox.Store{Root: env.Dirs.Inbox()}.Path("general", "claude-code", p.Session)
	f, err := os.OpenFile(file, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.Write(padding(t, env, "claude-code", p.Session, 2*promptBacklog)); err != nil {
		t.Fatal(err)
	}
	f.Close()
	idle := env.Now().Add(-time.Hour)
	if err := os.Chtimes(file, idle, idle); err != nil {
		t.Fatal(err)
	}
	reply := "A reply the Stop hook missed."
	appendRows(t, transcript, map[string]any{"type": "assistant", "sessionId": p.Session, "cwd": p.Cwd, "timestamp": idle.Format(time.RFC3339),
		"message": map[string]any{"role": "assistant", "stop_reason": "end_turn", "content": []map[string]any{{"type": "text", "text": reply}}}})
	p.Event = "UserPromptSubmit"
	invoke(t, "claude-code", p, env)
	if n := countRecords(records(t, env), "assistant", reply); n != 1 {
		t.Fatalf("recovery stored the missed reply %d times", n)
	}
	if got, want := spawned(), updateOf("claude-code:"+p.Session); !reflect.DeepEqual(got, want) {
		t.Fatalf("spawned %q, want %q", got, want)
	}
}

// SessionStart catches up the project's other sessions idle 2 minutes or
// more with a backlog, newest first, at most 5, in one update. It never
// takes a session still active, one processed to its end or one whose rest
// an update found too small, and it looks at no other project.
func TestSessionStartCatchesUpIdleSessions(t *testing.T) {
	env, p, spawned := triggerSetup(t)
	now := env.Now()
	for _, s := range []struct {
		host, id string
		idle     time.Duration
		mark     inbox.Mark
	}{
		{"codex", "active", idleAfter - time.Second, inbox.Mark{}},
		{"codex", "idle-1", idleAfter, inbox.Mark{}},
		{"claude-code", "processed", 3 * time.Minute, inbox.Mark{Offset: 1000}},
		{"claude-code", "too-small", 4 * time.Minute, inbox.Mark{Offset: 400, Wait: 1000}},
		{"claude-code", "idle-2", 5 * time.Minute, inbox.Mark{Offset: 400}},
		{"codex", "idle-3", 6 * time.Minute, inbox.Mark{}},
		{"claude-code", "idle-4", 7 * time.Minute, inbox.Mark{}},
		{"codex", "idle-5", 8 * time.Minute, inbox.Mark{}},
		{"claude-code", "idle-6", 9 * time.Minute, inbox.Mark{}},
	} {
		sessionFile(t, env, s.host, s.id, 1000, s.mark, now.Add(-s.idle))
	}
	// Another project's idle backlog is not this project's.
	other := inbox.Store{Root: env.Dirs.Inbox()}.Path("github.com__you__other", "codex", "elsewhere")
	if err := os.MkdirAll(filepath.Dir(other), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(other, padding(t, env, "codex", "elsewhere", 1000), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(other, now.Add(-time.Hour), now.Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	p.Event, p.Source = "SessionStart", "startup"
	invoke(t, "claude-code", p, env)
	want := updateOf("codex:idle-1", "claude-code:idle-2", "codex:idle-3", "claude-code:idle-4", "codex:idle-5")
	if got := spawned(); !reflect.DeepEqual(got, want) {
		t.Fatalf("spawned %q, want %q", got, want)
	}
}

// The catch-up reads only the 64 session files the brief lists: a backlog
// older than 64 newer files waits for the brief to reach it.
func TestCatchUpReadsOnlyTheBriefsListing(t *testing.T) {
	env, p, spawned := triggerSetup(t)
	now := env.Now()
	sessionFile(t, env, "codex", "oldest", 1000, inbox.Mark{}, now.Add(-2*time.Hour))
	var newer []string
	for i := 0; i < 64; i++ {
		newer = append(newer, sessionFile(t, env, "codex", fmt.Sprintf("done-%02d", i), 1000, inbox.Mark{Offset: 1000}, now.Add(-time.Hour+time.Duration(i)*time.Second)))
	}
	p.Event, p.Source = "SessionStart", "startup"
	invoke(t, "claude-code", p, env)
	if got := spawned(); got != nil {
		t.Fatalf("spawned %q beyond the brief's listing", got)
	}
	// With this session's own file, 64 files are listed once two newer
	// ones are gone.
	for _, file := range newer[:2] {
		if err := os.Remove(file); err != nil {
			t.Fatal(err)
		}
	}
	invoke(t, "claude-code", p, env)
	if got, want := spawned(), updateOf("codex:oldest"); !reflect.DeepEqual(got, want) {
		t.Fatalf("spawned %q, want %q", got, want)
	}
}

// The host has the hook's output before any job starts: Codex Stop's {} is
// written when the spawn runs.
func TestSpawnFollowsTheOutput(t *testing.T) {
	env, p, _ := triggerSetup(t)
	var out bytes.Buffer
	var written []string
	env.spawn = func(...string) { written = append(written, out.String()) }
	sessionFile(t, env, "codex", p.Session, stopBacklog, inbox.Mark{}, env.Now())
	reply := "Done."
	p.Event, p.Reply = "Stop", &reply
	data, _ := json.Marshal(p)
	Run(context.Background(), "codex", bytes.NewReader(data), &out, env)
	if len(written) != 1 || written[0] != "{}" {
		t.Fatalf("output written when the job started: %q", written)
	}
}

// updates = false in the config starts nothing, whatever the triggers find,
// and neither does a config without a wiki, for which a job could only fail.
func TestUpdatesOffSpawnsNothing(t *testing.T) {
	for _, host := range hosts {
		for _, off := range []string{"updates = false", "no wiki"} {
			t.Run(host+"/"+off, func(t *testing.T) {
				env, p, spawned := triggerSetup(t)
				wiki := env.Config.WikiPath
				if off == "no wiki" {
					env.Config.WikiPath = ""
				} else {
					env.Config.Updates = false
				}
				now := env.Now()
				sessionFile(t, env, "codex", "idle-backlog", 1000, inbox.Mark{}, now.Add(-time.Hour))
				// A large backlog from long ago: the prompt and Stop
				// triggers' conditions hold too.
				sessionFile(t, env, host, p.Session, 2*stopBacklog, inbox.Mark{}, now.Add(-time.Hour))
				reply := "Done."
				p.Reply = &reply
				for _, event := range []string{"UserPromptSubmit", "Stop", "PreCompact", "SessionEnd"} {
					p.Event = event
					invoke(t, host, p, env)
				}
				p.Session, p.Event, p.Source = "next-session", "SessionStart", "startup"
				invoke(t, host, p, env)
				if got := spawned(); got != nil {
					t.Fatalf("spawned %q with %s", got, off)
				}
				// The same inbox with updates on starts them.
				env.Config.Updates, env.Config.WikiPath = true, wiki
				p.Session, p.Event = "test-session", "SessionEnd"
				invoke(t, host, p, env)
				if got := spawned(); len(got) != 1 {
					t.Fatalf("updates on: spawned %q", got)
				}
			})
		}
	}
}

// SessionStart catches up the chat's own backlog when its file, as it was
// before the hook, had not changed for 2 minutes or more, as for a chat
// left idle and then resumed, or started again with the same id: its own
// first, in the one job with the project's idle sessions and within its 5.
// A file changed under 2 minutes before, as right after the PreCompact that
// already started an update, one processed to its end and one whose rest
// an update found too small start nothing.
func TestSessionStartCatchesUpTheChatsOwnBacklog(t *testing.T) {
	for _, host := range hosts {
		own := host + ":test-session"
		for _, tc := range []struct {
			name, source string
			idle         time.Duration
			mark         inbox.Mark
			others       int // the project's other sessions idle with a backlog
			want         [][]string
		}{
			{"resume", "resume", time.Hour, inbox.Mark{}, 0, updateOf(own)},
			{"startup with the same id", "startup", idleAfter, inbox.Mark{}, 0, updateOf(own)},
			{"idle just short", "resume", idleAfter - time.Second, inbox.Mark{}, 0, nil},
			{"just compacted", "compact", 10 * time.Second, inbox.Mark{}, 0, nil},
			{"processed", "resume", time.Hour, inbox.Mark{Offset: 3 * promptBacklog}, 0, nil},
			{"rest too small", "resume", time.Hour, inbox.Mark{Offset: 100, Wait: 3 * promptBacklog}, 0, nil},
			{"with idle sessions", "resume", time.Hour, inbox.Mark{}, 5, updateOf(own, "codex:idle-1", "codex:idle-2", "codex:idle-3", "codex:idle-4")},
		} {
			t.Run(host+"/"+tc.name, func(t *testing.T) {
				env, p, spawned := triggerSetup(t)
				now := env.Now()
				sessionFile(t, env, host, p.Session, 3*promptBacklog, tc.mark, now.Add(-tc.idle))
				for i := 1; i <= tc.others; i++ {
					sessionFile(t, env, "codex", fmt.Sprintf("idle-%d", i), 1000, inbox.Mark{}, now.Add(-time.Duration(i)*time.Hour))
				}
				p.Event, p.Source = "SessionStart", tc.source
				invoke(t, host, p, env)
				if got := spawned(); !reflect.DeepEqual(got, tc.want) {
					t.Fatalf("spawned %q, want %q", got, tc.want)
				}
			})
		}
	}
}

// A resumed chat's backlog is updated once: its SessionStart takes it, and
// its next prompt finds the file the start has just changed and starts
// nothing more. A real clock, since the start's own append dates the file.
func TestResumedChatSpawnsOnce(t *testing.T) {
	for _, host := range hosts {
		t.Run(host, func(t *testing.T) {
			env, p, spawned := triggerSetup(t)
			env.Now = time.Now
			sessionFile(t, env, host, p.Session, 3*promptBacklog, inbox.Mark{}, time.Now().Add(-time.Hour))
			p.Event, p.Source = "SessionStart", "resume"
			invoke(t, host, p, env)
			p.Event, p.Source = "UserPromptSubmit", ""
			invoke(t, host, p, env)
			if got, want := spawned(), updateOf(host+":"+p.Session); !reflect.DeepEqual(got, want) {
				t.Fatalf("spawned %q, want %q", got, want)
			}
		})
	}
}

// Start-of-chat transcript recovery appends what an idle session's hooks
// missed, which leaves its file just changed; the catch-up takes that
// session all the same, since recovery takes only sessions idle 2 minutes
// or more.
func TestCatchUpTakesSessionsRecoveryAppendedTo(t *testing.T) {
	env, p, spawned := triggerSetup(t)
	base := env.Now()
	clock := base.Add(-time.Hour)
	env.Now = func() time.Time { return clock }
	idle := p
	idle.Session = "idle-chat"
	transcript := registeredTranscript(t, env, &idle)
	file := inbox.Store{Root: env.Dirs.Inbox()}.Path("general", "claude-code", idle.Session)
	if err := os.Chtimes(file, clock, clock); err != nil {
		t.Fatal(err)
	}
	reply := "A reply the idle chat's Stop hook missed."
	appendRows(t, transcript, map[string]any{"type": "assistant", "sessionId": idle.Session, "cwd": idle.Cwd, "timestamp": clock.Format(time.RFC3339),
		"message": map[string]any{"role": "assistant", "stop_reason": "end_turn", "content": []map[string]any{{"type": "text", "text": reply}}}})
	if got := spawned(); got != nil {
		t.Fatalf("the idle chat's start spawned %q", got)
	}
	clock = base
	p.Event, p.Source = "SessionStart", "startup"
	invoke(t, "claude-code", p, env)
	if n := countRecords(records(t, env), "assistant", reply); n != 1 {
		t.Fatalf("recovery stored the missed reply %d times", n)
	}
	if got, want := spawned(), updateOf("claude-code:"+idle.Session); !reflect.DeepEqual(got, want) {
		t.Fatalf("spawned %q, want %q", got, want)
	}
}

// The catch-up takes this chat's own session only by its file as it was
// before the hook, once and first, never by the brief's listing of it.
func TestIdleBacklogsTakeTheOwnSessionOnce(t *testing.T) {
	env, _ := setup(t)
	now := env.Now()
	store := inbox.Store{Root: env.Dirs.Inbox()}
	var files []resume.SessionFile
	for _, id := range []string{"own", "other"} {
		file := sessionFile(t, env, "codex", id, 1000, inbox.Mark{}, now.Add(-time.Hour))
		files = append(files, resume.SessionFile{Name: filepath.Base(file), Path: file, Host: "codex", ID: id, Mod: now.Add(-time.Hour), Size: 1000})
	}
	own := files[0]
	if got := idleBacklogs(t.Context(), store, own, files, nil, now); !reflect.DeepEqual(got, []string{"codex:own", "codex:other"}) {
		t.Fatalf("idle backlogs %q", got)
	}
	// A chat that had no file before the hook.
	own.Mod, own.Size = time.Time{}, 0
	if got := idleBacklogs(t.Context(), store, own, files, nil, now); !reflect.DeepEqual(got, []string{"codex:other"}) {
		t.Fatalf("idle backlogs %q", got)
	}
}
