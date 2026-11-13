package hook

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/heyNag/eva-md/internal/paths"
)

// Each run's heartbeat records how long the run took, in milliseconds,
// from Run's start to just before the heartbeat's save, for the doctor's
// hook times. The time is the run's own, read here from an injected clock
// that hands each run its time: the clock is asked once a run, with the
// moment Run began. Without the injected clock, a run's real time is
// recorded.
func TestHeartbeatRecordsEachRunsTime(t *testing.T) {
	env, p := setup(t)
	p.Event = "Stop"
	reply := "a reply"
	p.Reply = &reply
	took := []time.Duration{12500 * time.Microsecond, 340 * time.Millisecond}
	var starts []time.Time
	env.elapsed = func(started time.Time) time.Duration {
		starts = append(starts, started)
		return took[len(starts)-1]
	}
	before := time.Now()
	invoke(t, "codex", p, env)
	between := time.Now()
	invoke(t, "codex", p, env)
	runs := beatRuns(t, env, "codex", "Stop")
	if len(runs) != 2 || runs[0].DurationMS != 12.5 || runs[1].DurationMS != 340 || runs[0].Failed || runs[1].Failed {
		t.Fatalf("runs=%+v", runs)
	}
	if len(starts) != 2 || starts[0].Before(before) || starts[0].After(between) || starts[1].Before(between) {
		t.Fatalf("the runs' times were measured from %v, not from the start of each run (%v, %v)", starts, before, between)
	}
	env.elapsed = nil
	invoke(t, "codex", p, env)
	if runs = beatRuns(t, env, "codex", "Stop"); len(runs) != 3 || runs[2].DurationMS <= 0 {
		t.Fatalf("a run on the real clock recorded no time: %+v", runs)
	}
}

// A run whose capture ran out of time is recorded as failed once its output
// is written, with the time it took from Run's start, at the moment the
// system's clock gives, and with whether its transcript lies elsewhere, here
// in a folder CODEX_HOME moved. The capture Run gave up on writes no
// heartbeat of its own: its time is up. The test ends the capture's time
// itself, once the capture is seen waiting for the inbox's lock, so no
// clock decides the outcome. From then on the capture's next call of the
// env's clock is for its own heartbeat, and the test holds every such call
// until Run has returned, so the two writes come in a fixed order: a
// capture heartbeat that ignored its ended time would be written after the
// late run's record, and seen as a second run.
func TestLateRunRecordsItsTime(t *testing.T) {
	env, p := setup(t)
	p.Event = "Stop"
	reply := "must not arrive late"
	p.Reply = &reply
	p.Transcript = filepath.Join(os.Getenv("HOME"), ".codex-moved", "sessions", "2026", "10", "29", "rollout-2026-10-29T15-04-05-"+p.Session+".jsonl")
	data, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	release := holdInbox(t, env, "codex", p.Session)
	defer release()
	// Slack keeps busySlack. Run's waits are the test's to end and recovery
	// is a stub, so the one limit it still lengthens is the late run's
	// record's own, a real 100 ms that a busy machine can spend before the
	// record is written.
	held := env
	held.recoverTranscript = func(context.Context, paths.Dirs, string, string) (int, error) { return 0, nil }
	// The capture Run gave up on measures its time too, from its own
	// goroutine.
	var mu sync.Mutex
	var starts []time.Time
	held.elapsed = func(started time.Time) time.Duration {
		mu.Lock()
		defer mu.Unlock()
		starts = append(starts, started)
		return 290 * time.Millisecond
	}
	fixed := env.Now()
	ended, returned := make(chan struct{}), make(chan struct{})
	held.Now = func() time.Time {
		select {
		case <-ended:
			if inCapture() {
				select {
				case <-returned:
				case <-time.After(hangGuard):
				}
			}
		default:
		}
		return fixed
	}
	waits := make(chan heldWait, 3)
	held.withDeadline = holdWaits(waits)
	before := time.Now()
	go func() {
		defer close(returned)
		Run(context.Background(), "codex", bytes.NewReader(data), io.Discard, held)
	}()
	nextWait(t, waits, "its wait for the payload")
	work := nextWait(t, waits, "its wait for the capture")
	asked := time.Now()
	early := "a Stop held up by the inbox's lock returned before its deadline"
	awaitParked(t, returned, inboxWait, "/internal/hook.capture(", "the capture was not seen waiting for the inbox's lock", early)
	awaitParked(t, returned, runWait, "/hook."+t.Name()+".", "Run was not seen waiting for the capture", early)
	close(ended)
	work.end()
	mustReturn(t, "a Stop held up by the inbox's lock, past its deadline,", func() { <-returned })
	release()
	awaitNoCapture(t, "the capture Run gave up on")
	mu.Lock()
	measured := append([]time.Time(nil), starts...)
	mu.Unlock()
	if len(measured) == 0 {
		t.Fatal("the late run's time was not measured")
	}
	for _, started := range measured {
		if started.Before(before) || started.After(asked) {
			t.Fatalf("a run's time was measured from %v, not from Run's start, between %v and %v", started, before, asked)
		}
	}
	runs := beatRuns(t, env, "codex", "Stop")
	switch {
	case len(runs) != 1:
		t.Fatalf("the heartbeat holds %d runs, want the late run's record alone: %+v", len(runs), runs)
	case !runs[0].Failed:
		t.Fatalf("the late run is not recorded as failed: %+v", runs[0])
	case runs[0].DurationMS != 290:
		t.Fatalf("the late run took %v ms, want 290: %+v", runs[0].DurationMS, runs[0])
	case runs[0].At == fixed.Format(time.RFC3339Nano):
		t.Fatalf("the late run was recorded at the env's fixed time %s, not the system clock's", runs[0].At)
	}
	var beat struct {
		Elsewhere bool `json:"transcript_elsewhere"`
	}
	if data, err := os.ReadFile(filepath.Join(env.Dirs.State, "heartbeats", "codex", "Stop.json")); err != nil || json.Unmarshal(data, &beat) != nil || !beat.Elsewhere {
		t.Fatalf("the late run's heartbeat doesn't say the transcript is elsewhere: %s %v", data, err)
	}
}

// inCapture reports whether the calling goroutine is running capture.
func inCapture() bool {
	buf := make([]byte, 16<<10)
	for {
		n := runtime.Stack(buf, false)
		if n < len(buf) {
			return strings.Contains(string(buf[:n]), captureFrame)
		}
		buf = make([]byte, 2*len(buf))
	}
}

// uncapturedStamp reads the stamp a run eva-md captures nothing of leaves.
func uncapturedStamp(t *testing.T, env Env, host string) ([]byte, os.FileInfo, bool) {
	t.Helper()
	file := filepath.Join(env.Dirs.State, "heartbeats", host, uncapturedFile)
	data, err := os.ReadFile(file)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil, false
	}
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Lstat(file)
	if err != nil {
		t.Fatal(err)
	}
	return data, info, true
}

// A run that stops before capture because eva-md doesn't capture its
// folder stamps the time alone, so the capture-freshness check counts the
// host's hooks as running though the chat left no heartbeat: an excluded
// path, through an alias too, or remote, a temporary folder, the wiki,
// eva-md's state folder, a Codex memory-writing run and a desktop spare.
// The stamp names no folder, session or event, and no event's heartbeat is
// written, so the hook-failure count never sees such a run. The inner and
// remote guards, whose chats leave no transcript of the host's own here,
// stamp nothing, and a captured chat writes its heartbeat and no stamp.
func TestUncapturedRunsStampTheTimeOnly(t *testing.T) {
	for _, tc := range []struct {
		name    string
		stamped bool
	}{{"inner", false}, {"remote", false}, {"excluded", true}, {"wiki", true}, {"state", true}, {"temp", true}, {"memory-cwd", true}, {"memory-prompt", true},
		{"spare", true}, {"symlink-excluded", true}, {"remote-excluded", true}, {"captured", false}} {
		t.Run(tc.name, func(t *testing.T) {
			env, p := setup(t)
			p.Event = "Stop"
			reply := "DO NOT CAPTURE"
			p.Reply = &reply
			switch tc.name {
			case "inner":
				t.Setenv("EVA_MD_INNER", "1")
			case "remote":
				t.Setenv("CLAUDE_CODE_REMOTE", "true")
			case "excluded":
				env.Config.Exclude = []string{p.Cwd}
			case "wiki":
				env.Config.WikiPath = p.Cwd
			case "state":
				p.Cwd = env.Dirs.State
			case "temp":
				p.Cwd = os.Getenv("TMPDIR")
			case "memory-cwd":
				p.Cwd = filepath.Join(os.Getenv("HOME"), ".codex", "memories")
			case "memory-prompt":
				p.Prompt = "## Memory Writing Agent\nprivate system prompt"
			case "spare":
				p.Cwd = filepath.Join(os.Getenv("HOME"), ".claude", "spares", "spare-1")
			case "symlink-excluded":
				env.Config.Exclude = []string{p.Cwd}
				alias := filepath.Join(os.Getenv("HOME"), "alias")
				if err := os.Symlink(p.Cwd, alias); err != nil {
					t.Fatal(err)
				}
				p.Cwd = alias
			case "remote-excluded":
				git(t, p.Cwd, "init", "--quiet")
				git(t, p.Cwd, "remote", "add", "origin", "https://github.com/private/project.git")
				env.Config.Exclude = []string{"remote:github.com/private/*"}
			}
			if err := os.MkdirAll(p.Cwd, 0o700); err != nil {
				t.Fatal(err)
			}
			invoke(t, "codex", p, env)
			data, info, stamped := uncapturedStamp(t, env, "codex")
			if stamped != tc.stamped {
				t.Fatalf("stamped=%v, want %v", stamped, tc.stamped)
			}
			_, err := os.Stat(filepath.Join(env.Dirs.State, "heartbeats", "codex", "Stop.json"))
			if beat := err == nil; beat != (tc.name == "captured") {
				t.Fatalf("the Stop heartbeat was written: %v", beat)
			}
			if !stamped {
				return
			}
			if string(data) != `{"at":"2026-10-29T08:04:05.000000000Z"}` || info.Mode().Perm() != 0o600 || !info.Mode().IsRegular() {
				t.Fatalf("stamp %q, mode %v", data, info.Mode())
			}
			if rows := records(t, env); len(rows) != 0 {
				t.Fatalf("an uncaptured chat stored %#v", rows)
			}
		})
	}
}

// The stamp is rewritten in place, never replaced by an atomic save, which
// would cost an fsync: a later run keeps the file and its inode and writes
// only its own time, in UTC and at one width, so it leaves nothing of the
// earlier one. A stamp that isn't a regular file of its own is left as it
// is.
func TestUncapturedStampIsWrittenInPlace(t *testing.T) {
	env, p := setup(t)
	env.Config.Exclude = []string{p.Cwd}
	p.Event = "UserPromptSubmit"
	at := time.Date(2026, 10, 29, 15, 4, 5, 123456789, time.FixedZone("ICT", 7*3600))
	env.Now = func() time.Time { return at }
	invoke(t, "claude-code", p, env)
	first, before, ok := uncapturedStamp(t, env, "claude-code")
	if !ok || string(first) != `{"at":"2026-10-29T08:04:05.123456789Z"}` {
		t.Fatalf("first stamp %q", first)
	}
	at = time.Date(2026, 10, 29, 15, 9, 0, 0, time.FixedZone("ICT", 7*3600))
	p.Event = "Stop"
	invoke(t, "claude-code", p, env)
	second, after, _ := uncapturedStamp(t, env, "claude-code")
	if string(second) != `{"at":"2026-10-29T08:09:00.000000000Z"}` || !os.SameFile(before, after) {
		t.Fatalf("second stamp %q, same file %v", second, os.SameFile(before, after))
	}
	file := filepath.Join(env.Dirs.State, "heartbeats", "claude-code", uncapturedFile)
	other := filepath.Join(env.Dirs.State, "other")
	if err := os.WriteFile(other, []byte("kept"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(file); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(other, file); err != nil {
		t.Fatal(err)
	}
	invoke(t, "claude-code", p, env)
	if data, err := os.ReadFile(other); err != nil || string(data) != "kept" {
		t.Fatalf("a stamp with a second link was written: %q %v", data, err)
	}
	if err := os.Remove(file); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(other, file); err != nil {
		t.Fatal(err)
	}
	invoke(t, "claude-code", p, env)
	if data, err := os.ReadFile(other); err != nil || string(data) != "kept" {
		t.Fatalf("a stamp written through a symlink: %q %v", data, err)
	}
	var st syscall.Stat_t
	if err := syscall.Lstat(file, &st); err != nil || st.Mode&syscall.S_IFMT != syscall.S_IFLNK {
		t.Fatalf("the symlink was replaced: %v", err)
	}
}

// Two runs that stamp at once, as the async hooks of parallel tool calls
// do, leave one whole stamp, the one written last, and the file holds a
// whole stamp at every step between: each run writes a stamp as long as
// any other over the last, never truncating it first. One run is held just
// before it writes, through the env's seam, while the other writes, in both
// orders, with times whose fractions a trimmed layout would write at
// different lengths, so neither the steps nor the outcome rests on timing.
func TestStampsWrittenAtOnceLeaveOneWhole(t *testing.T) {
	ict := time.FixedZone("ICT", 7*3600)
	long := time.Date(2026, 10, 29, 15, 4, 5, 123456000, ict) // .123456
	short := long.Add(4 * time.Microsecond)                   // .12346
	earlier := long.Add(-time.Minute)
	for _, held := range []time.Time{short, long} {
		other := long
		if held.Equal(long) {
			other = short
		}
		t.Run(held.Format(time.RFC3339Nano), func(t *testing.T) {
			env, _ := setup(t)
			file := filepath.Join(env.Dirs.State, "heartbeats", "codex", uncapturedFile)
			whole := func(step string, want time.Time) {
				t.Helper()
				data, err := os.ReadFile(file)
				var stamp struct {
					At time.Time `json:"at"`
				}
				if err != nil || json.Unmarshal(data, &stamp) != nil || !stamp.At.Equal(want) {
					t.Fatalf("%s: the stamp holds %q (%v), want one whole stamp of %s", step, data, err, want.Format(time.RFC3339Nano))
				}
			}
			uncaptured(env, "codex", "PostToolUse", earlier)
			whole("the first stamp", earlier)
			reached, release, done := make(chan struct{}), make(chan struct{}), make(chan struct{})
			hold := env
			hold.stamping = func() { close(reached); <-release }
			go func() {
				defer close(done)
				uncaptured(hold, "codex", "PostToolUse", held)
			}()
			select {
			case <-reached:
			case <-time.After(hangGuard):
				t.Fatal("the held run never came to its write")
			}
			whole("one run held before its write", earlier)
			uncaptured(env, "codex", "PostToolUse", other)
			whole("the other run written", other)
			close(release)
			select {
			case <-done:
			case <-time.After(hangGuard):
				t.Fatal("the held run never returned")
			}
			whole("the held run written last", held)
		})
	}
}

// The heartbeat says the transcript lies elsewhere exactly when recovery
// can't read it, by the one rule they share: a transcript path a host
// writes through a link to the home, or a host folder linked elsewhere, as
// a dotfiles setup links ~/.claude, is still the host's folder, and
// recovery reads a reply the hooks missed there; a link from inside the
// folder out of it, or a folder CLAUDE_CONFIG_DIR moved, is not, and
// recovery reads nothing of it. HOME is the real folder throughout, and
// eva-md's folders come from it, as paths.Default gives them.
func TestRecoveryAndTheHeartbeatJudgeTheFolderAlike(t *testing.T) {
	for _, tc := range []struct {
		name      string
		elsewhere bool
	}{{"plain", false}, {"path through a link to the home", false}, {"linked host folder", false}, {"link out of the folder", true}, {"moved folder", true}} {
		t.Run(tc.name, func(t *testing.T) {
			env, p := setup(t)
			home := os.Getenv("HOME")
			folder := filepath.Join(home, ".claude", "projects", "fixture")
			link := func(target, name string) {
				t.Helper()
				if err := os.MkdirAll(target, 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.MkdirAll(filepath.Dir(name), 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(target, name); err != nil {
					t.Fatal(err)
				}
			}
			switch tc.name {
			case "path through a link to the home":
				linked := filepath.Join(t.TempDir(), "home-link")
				link(home, linked)
				folder = filepath.Join(linked, ".claude", "projects", "fixture")
			case "linked host folder":
				link(filepath.Join(home, "dotfiles", "claude"), filepath.Join(home, ".claude"))
			case "link out of the folder":
				link(filepath.Join(home, "elsewhere", "fixture"), folder)
			case "moved folder":
				folder = filepath.Join(home, ".claude-moved", "projects", "fixture")
			}
			file := filepath.Join(folder, p.Session+".jsonl")
			if err := os.MkdirAll(folder, 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(file, nil, 0o600); err != nil {
				t.Fatal(err)
			}
			p.Transcript = file
			p.Event, p.Source = "SessionStart", "startup"
			invoke(t, "claude-code", p, env)
			reply := "A reply the Stop hook missed."
			appendRows(t, file, map[string]any{"type": "assistant", "sessionId": p.Session, "cwd": p.Cwd, "timestamp": env.Now().Format(time.RFC3339),
				"message": map[string]any{"role": "assistant", "stop_reason": "end_turn", "content": []map[string]any{{"type": "text", "text": reply}}}})
			p.Event, p.Source, p.Prompt, p.Turn = "UserPromptSubmit", "", "next question", "turn-2"
			invoke(t, "claude-code", p, env)
			data, err := os.ReadFile(filepath.Join(env.Dirs.State, "heartbeats", "claude-code", "UserPromptSubmit.json"))
			if err != nil {
				t.Fatal(err)
			}
			var beat map[string]any
			if err := json.Unmarshal(data, &beat); err != nil {
				t.Fatal(err)
			}
			elsewhere := beat["transcript_elsewhere"] == true
			recovered := countRecords(records(t, env), "assistant", reply)
			if elsewhere != tc.elsewhere || recovered != map[bool]int{false: 1, true: 0}[tc.elsewhere] {
				t.Fatalf("transcript %s: elsewhere %v, the missed reply recovered %d times", file, elsewhere, recovered)
			}
		})
	}
}

// A home folder that is itself a symlink isn't supported: paths.Default
// joins eva-md's folders onto HOME as given, and eva-md opens them through
// no link but macOS's /var, /tmp and /etc aliases, so it refuses them.
// Here HOME is a link, eva-md's folders come from it, as the hook command
// gets them, and the folder it leads to holds eva-md's state from before.
// Every event on both hosts exits 0 and gives no brief, Codex's Stop the {}
// it always gets; the hooks capture nothing, recover none of a reply they
// missed, write nothing, through the link or beside it, not even the day's
// attempt marker or a chat start's push retry check, and start nothing.
func TestLinkedHomeCapturesNothing(t *testing.T) {
	for _, host := range hosts {
		t.Run(host, func(t *testing.T) {
			env, p, spawned := maintainSetup(t)
			home := os.Getenv("HOME")
			linked := filepath.Join(t.TempDir(), "home-link")
			if err := os.Symlink(home, linked); err != nil {
				t.Fatal(err)
			}
			t.Setenv("HOME", linked)
			dirs, err := paths.Default()
			if err != nil {
				t.Fatal(err)
			}
			env.Dirs = dirs
			env.Config.WikiPath = filepath.Join(linked, "wiki")
			p.Cwd = filepath.Join(linked, "project")
			// The host writes its transcript under HOME, the link.
			transcript := filepath.Join(".claude", "projects", "fixture", p.Session+".jsonl")
			if host == "codex" {
				transcript = filepath.Join(".codex", "sessions", "2026", "10", "29", "rollout-2026-10-29T15-04-05-"+p.Session+".jsonl")
			}
			p.Transcript = filepath.Join(linked, transcript)
			if err := os.MkdirAll(filepath.Dir(p.Transcript), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(p.Transcript, nil, 0o600); err != nil {
				t.Fatal(err)
			}
			before := tree(t, home, filepath.Dir(linked))
			reply := "A reply the Stop hook missed."
			p.Reply = &reply
			p.Tool = "Write"
			p.ToolInput = json.RawMessage(`{"file_path":"file.txt"}`)
			p.ToolResponse = json.RawMessage(`"Exit code: 0"`)
			p.Summary = "host summary"
			// Every event, then a resumed chat's start, which looks for a
			// push to retry even after the day's first event.
			for i, run := range []struct{ event, source string }{{"SessionStart", "startup"}, {"UserPromptSubmit", ""}, {"PostToolUse", ""}, {"Stop", ""}, {"PreCompact", ""}, {"PostCompact", ""}, {"SessionEnd", ""}, {"SessionStart", "resume"}} {
				p.Event, p.Source = run.event, run.source
				want := ""
				if host == "codex" && run.event == "Stop" {
					want = "{}"
				}
				if out := invoke(t, host, p, env); out != want {
					t.Fatalf("%s: the hook said %q", run.event, out)
				}
				if i == 0 {
					appendRows(t, p.Transcript, map[string]any{"type": "assistant", "sessionId": p.Session, "cwd": p.Cwd, "timestamp": env.Now().Format(time.RFC3339),
						"message": map[string]any{"role": "assistant", "stop_reason": "end_turn", "content": []map[string]any{{"type": "text", "text": reply}}}})
					// The test's own write, to the file the link leads to.
					before[filepath.Join(home, transcript)] = treeEntryOf(t, filepath.Join(home, transcript))
				}
			}
			after := tree(t, home, filepath.Dir(linked))
			for path, was := range before {
				if now, ok := after[path]; !ok || now != was {
					t.Errorf("the hooks changed %s", path)
				}
			}
			for path := range after {
				if _, ok := before[path]; !ok {
					t.Errorf("the hooks wrote %s", path)
				}
			}
			if got := spawned(); got != nil {
				t.Errorf("the hooks started %q", got)
			}
		})
	}
}

// treeEntry is what tree records of a file or folder: its type and
// permissions, size and modification time.
type treeEntry struct {
	mode fs.FileMode
	size int64
	mod  int64
}

// treeEntryOf reads path's entry, without following a link.
func treeEntryOf(t *testing.T, path string) treeEntry {
	t.Helper()
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	return treeEntry{info.Mode(), info.Size(), info.ModTime().UnixNano()}
}

// tree records every file and folder under each of roots, by its path,
// following no link.
func tree(t *testing.T, roots ...string) map[string]treeEntry {
	t.Helper()
	out := map[string]treeEntry{}
	for _, root := range roots {
		err := filepath.WalkDir(root, func(path string, _ fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			out[path] = treeEntryOf(t, path)
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	return out
}

// A run whose transcript lies outside the folder transcript recovery
// reads, as when CLAUDE_CONFIG_DIR or CODEX_HOME moves the host's folder,
// says so in its heartbeat, for the doctor. One in the host's own folder,
// through the /private alias of its path too, or with its path written
// through a link to the home, and one that names no transcript, don't; a
// moved folder written through that link still does. HOME is the real
// folder throughout, and eva-md's folders come from it, as paths.Default
// gives them.
func TestHeartbeatSaysWhenTheTranscriptIsElsewhere(t *testing.T) {
	for _, tc := range []struct {
		host, folder string
		home         string // the path's home: HOME, resolved, or a link to HOME
		elsewhere    bool
	}{
		{"claude-code", ".claude/projects/fixture", "", false},
		{"claude-code", ".claude/projects/fixture", "resolved", false},
		{"claude-code", ".claude/projects/fixture", "linked", false},
		{"claude-code", ".claude-moved/projects/fixture", "", true},
		{"claude-code", ".claude-moved/projects/fixture", "linked", true},
		{"codex", ".codex/sessions/2026/10/29", "", false},
		{"codex", ".codex/sessions/2026/10/29", "resolved", false},
		{"codex", ".codex/sessions/2026/10/29", "linked", false},
		{"codex", ".codex-moved/sessions/2026/10/29", "", true},
		{"codex", ".claude/projects/fixture", "", true},
		{"claude-code", "", "", false},
	} {
		t.Run(tc.host+"/"+tc.folder+"/"+tc.home, func(t *testing.T) {
			env, p := setup(t)
			p.Event = "Stop"
			reply := "a reply"
			p.Reply = &reply
			if tc.folder != "" {
				home := os.Getenv("HOME")
				switch tc.home {
				case "resolved":
					resolved, err := filepath.EvalSymlinks(home)
					if err != nil {
						t.Fatal(err)
					}
					home = resolved
				case "linked":
					linked := filepath.Join(t.TempDir(), "home-link")
					if err := os.Symlink(home, linked); err != nil {
						t.Fatal(err)
					}
					home = linked
				}
				name := p.Session + ".jsonl"
				if tc.host == "codex" {
					name = "rollout-2026-10-29T15-04-05-" + p.Session + ".jsonl"
				}
				p.Transcript = filepath.Join(home, filepath.FromSlash(tc.folder), name)
				if err := os.MkdirAll(filepath.Dir(p.Transcript), 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(p.Transcript, nil, 0o600); err != nil {
					t.Fatal(err)
				}
			}
			invoke(t, tc.host, p, env)
			data, err := os.ReadFile(filepath.Join(env.Dirs.State, "heartbeats", tc.host, "Stop.json"))
			if err != nil {
				t.Fatal(err)
			}
			var beat map[string]any
			if err := json.Unmarshal(data, &beat); err != nil {
				t.Fatal(err)
			}
			if got := beat["transcript_elsewhere"] == true; got != tc.elsewhere || beat["transcript_available"] != (tc.folder != "") {
				t.Fatalf("heartbeat %s", data)
			}
		})
	}
}
