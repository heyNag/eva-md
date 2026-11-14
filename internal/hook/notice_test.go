package hook

import (
	"bytes"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/heyNag/eva-md/internal/config"
	"github.com/heyNag/eva-md/internal/health"
)

// With the switch health on, a chat start shows the line prepared outside
// the hook and computes nothing, asks for the notice to be prepared when
// it is missing or old, and records what it showed once the output is
// written.

// noticeSetup is maintainSetup with the switch health on.
func noticeSetup(t *testing.T) (Env, payload, func() [][]string) {
	t.Helper()
	env, p, spawned := maintainSetup(t)
	env.Config.Switches = map[string]any{"health": true}
	p.Event, p.Source = "SessionStart", "startup"
	return env, p, spawned
}

// The line a test prepares for Claude Code.
const (
	preparedID   = "push.diverged"
	preparedText = "eva.md: the wiki's remote has commits this clone lacks, so pushes stopped. Merge them in the wiki by hand, never with a force push."
)

// prepared writes notice.json as Prepare would have at at, with a line for
// Claude Code only.
func prepared(t *testing.T, env Env, at time.Time) {
	t.Helper()
	data, err := json.Marshal(map[string]any{"v": 1, "prepared": at.Format(time.RFC3339), "lines": map[string]any{"claude-code": map[string]string{"id": preparedID, "text": preparedText}}})
	if err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(env.Dirs.State, "health", "notice.json")
	if err := os.MkdirAll(filepath.Dir(file), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(file, at, at); err != nil {
		t.Fatal(err)
	}
}

// messageOf is the output's systemMessage.
func messageOf(t *testing.T, out string) string {
	t.Helper()
	if out == "" {
		return ""
	}
	var data struct {
		System string `json:"systemMessage"`
	}
	if err := json.Unmarshal([]byte(out), &data); err != nil {
		t.Fatalf("output=%q: %v", out, err)
	}
	return data.System
}

// shownFile reads shown.json, or "" when there is none.
func shownFile(t *testing.T, env Env) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(env.Dirs.State, "health", "shown.json"))
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	return string(data)
}

// noticeJobs are the doctor --notice jobs among runs.
func noticeJobs(runs [][]string) [][]string {
	var out [][]string
	for _, r := range runs {
		if slices.Equal(r, []string{"doctor", "--notice"}) {
			out = append(out, r)
		}
	}
	return out
}

// With no notice.json, a chat start shows nothing and asks for the job
// doctor --notice, for the reason health, with none of git's variables; a
// notice written less than 30 minutes before asks for none, and one
// older does, on either host.
func TestANoticeIsAskedForWhenMissingOrOld(t *testing.T) {
	for _, host := range hosts {
		env, p, spawned := noticeSetup(t)
		pr := newProbe(t, &p, nil)
		jobs, runs := launched(t, pr, host, p, env, spawned)
		i := slices.IndexFunc(jobs, func(j job) bool { return slices.Equal(j.args, []string{"doctor", "--notice"}) })
		if i < 0 || jobs[i].reason != reasonHealth || jobs[i].git || len(noticeJobs(runs)) != 1 {
			t.Fatalf("%s, no notice: jobs %+v, started %q", host, jobs, runs)
		}
		if got := shownFile(t, env); got != "" {
			t.Fatalf("%s: shown.json %s", host, got)
		}
		for _, c := range []struct {
			age time.Duration
			ask bool
		}{{29 * time.Minute, false}, {31 * time.Minute, true}} {
			prepared(t, env, env.Now().Add(-c.age))
			newProbe(t, &p, nil)
			_, runs := launched(t, pr, host, p, env, spawned)
			if got := len(noticeJobs(runs)) == 1; got != c.ask {
				t.Errorf("%s, a notice %s old: started %q", host, c.age, runs)
			}
		}
	}
}

// A prepared line is shown as written, as the message; it is recorded as
// shown once the output is written, and not shown again within 24 hours,
// on either host, but after them. A line prepared over 24 hours before is
// not shown, nor another host's, and one whose output did not reach the
// host is not recorded as shown.
func TestAPreparedLineIsShownOnceADay(t *testing.T) {
	env, p, _ := noticeSetup(t)
	now := env.Now()
	prepared(t, env, now.Add(-24*time.Hour-time.Second))
	if out := invoke(t, "claude-code", p, env); messageOf(t, out) != "" || shownFile(t, env) != "" {
		t.Fatalf("a line prepared over 24 hours before: %q", out)
	}
	prepared(t, env, now.Add(-24*time.Hour))
	p.Session = "second-session"
	if out := invoke(t, "codex", p, env); messageOf(t, out) != "" || shownFile(t, env) != "" {
		t.Fatalf("Claude Code's line on Codex: %q", out)
	}
	// A host that closed the hook's output, as on its own timeout: the line
	// did not reach it, and shows at the next start.
	p.Session = "closed-session"
	data, _ := json.Marshal(p)
	attempted := false
	Run(t.Context(), "claude-code", bytes.NewReader(data), writerFunc(func(b []byte) (int, error) {
		attempted = true
		return 0, syscall.EPIPE
	}), env)
	if !attempted || shownFile(t, env) != "" {
		t.Fatalf("an output the host did not get: written %v, shown.json %s", attempted, shownFile(t, env))
	}
	p.Session = "third-session"
	var written bool
	var out bytes.Buffer
	data, _ = json.Marshal(p)
	Run(t.Context(), "claude-code", bytes.NewReader(data), writerFunc(func(b []byte) (int, error) {
		_, err := os.Lstat(filepath.Join(env.Dirs.State, "health", "shown.json"))
		written = err == nil
		return out.Write(b)
	}), env)
	if messageOf(t, out.String()) != preparedText {
		t.Fatalf("the prepared line: %q", out.String())
	}
	if written {
		t.Fatal("shown.json was written before the output")
	}
	want := `{"id":"` + preparedID + `","at":"` + now.Format(time.RFC3339) + `"}` + "\n"
	if got := shownFile(t, env); got != want {
		t.Fatalf("shown.json %s, want %s", got, want)
	}
	for _, c := range []struct {
		later time.Duration
		show  bool
	}{{time.Hour, false}, {24*time.Hour - time.Minute, false}, {24 * time.Hour, true}} {
		at := now.Add(c.later)
		env.Now = func() time.Time { return at }
		prepared(t, env, at)
		p.Session = "session-" + at.Format("150405")
		if got := messageOf(t, invoke(t, "claude-code", p, env)); (got == preparedText) != c.show {
			t.Errorf("%s later: %q", c.later, got)
		}
	}
}

// writerFunc is an io.Writer of a function.
type writerFunc func([]byte) (int, error)

func (f writerFunc) Write(b []byte) (int, error) { return f(b) }

// A chat's first prompt with no brief before it, as after a chat start
// that ran out of time, shows the line; a later prompt, after a brief,
// does not read it.
func TestTheFirstPromptShowsTheLine(t *testing.T) {
	env, p, _ := noticeSetup(t)
	prepared(t, env, env.Now())
	p.Event, p.Source = "UserPromptSubmit", ""
	if got := messageOf(t, invoke(t, "claude-code", p, env)); got != preparedText {
		t.Fatalf("the first prompt: %q", got)
	}
	if err := os.Remove(filepath.Join(env.Dirs.State, "health", "shown.json")); err != nil {
		t.Fatal(err)
	}
	p.Prompt, p.Turn = "and then", "turn-2"
	if got := messageOf(t, invoke(t, "claude-code", p, env)); got != "" {
		t.Fatalf("a later prompt: %q", got)
	}
	if got := shownFile(t, env); got != "" {
		t.Fatalf("a later prompt recorded %s", got)
	}
}

// With the switch off, a chat start says what v1's QuickNotice finds, asks
// for no notice and records nothing, whatever notice.json holds.
func TestWithTheSwitchOffTheHookIsV1s(t *testing.T) {
	env, p, spawned := noticeSetup(t)
	env.Config.Switches = nil
	env.Config.Unknown = []string{"mystery_key"}
	prepared(t, env, env.Now())
	// The day's maintain started earlier, so this chat start checks the push.
	if !firstOfDay(env.Dirs.State, env.Now()) {
		t.Fatal("the day's maintain had started")
	}
	if got := messageOf(t, invoke(t, "claude-code", p, env)); got != "eva.md: config has unknown keys, which are ignored; run eva-md doctor." {
		t.Fatalf("switch off: %q", got)
	}
	if got := noticeJobs(spawned()); len(got) != 0 || shownFile(t, env) != "" {
		t.Fatalf("switch off: started %q, shown %s", got, shownFile(t, env))
	}
	if _, err := os.Lstat(filepath.Join(env.Dirs.State, "commit", "push-retry")); err != nil {
		t.Fatalf("switch off: the push check's timer is not v1's: %v", err)
	}
}

// A healthy state, built by the hooks of both hosts and Prepare with the
// switch on, holds no file whose name says something is wrong or waits:
// the push check's timer is push-checked, the trust flag a Codex chat
// start has met is gone, and so is the old timer. A heartbeat is named for
// its host's event, which is no name eva-md gave a state of its own: a
// tool call that failed, as one does on any healthy day, leaves
// PostToolUseFailure.json, and the check passes over it. A marker planted
// in the chats' entries appears in neither file the notice writes.
func TestAHealthyStateHoldsNoNameOfAFault(t *testing.T) {
	const marker = "zqxnoticemarker"
	env, p, _ := noticeSetup(t)
	if err := config.Save(env.Dirs.ConfigFile(), env.Config); err != nil {
		t.Fatal(err)
	}
	now := env.Now()
	for name, at := range map[string]time.Time{"needs-trust.json": now.Add(-time.Hour), filepath.Join("commit", "push-retry"): now.Add(-time.Hour)} {
		file := filepath.Join(env.Dirs.State, name)
		if err := os.MkdirAll(filepath.Dir(file), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(file, []byte(`{"host":"codex","needs_trust":true}`), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(file, at, at); err != nil {
			t.Fatal(err)
		}
	}
	prepared(t, env, now)
	for _, host := range hosts {
		p.Session = "healthy-" + host
		p.Event, p.Source = "SessionStart", "startup"
		invoke(t, host, p, env)
		for i, event := range []string{"UserPromptSubmit", "PostToolUse", "PostToolUseFailure", "Stop", "SessionEnd"} {
			p.Event, p.Prompt, p.Turn, p.Tool = event, "a prompt that holds "+marker, "turn-"+event, "Bash"
			reply := "a reply that holds " + marker
			p.Reply = &reply
			p.ToolInput = json.RawMessage(`{"command":"echo ` + marker + `"}`)
			env.Now = func() time.Time { return now.Add(time.Duration(i+1) * time.Minute) }
			invoke(t, host, p, env)
		}
		env.Now = func() time.Time { return now }
	}
	if shownFile(t, env) == "" {
		t.Fatal("no line was shown")
	}
	if err := health.Prepare(t.Context(), env.Dirs, now.Add(10*time.Minute)); err != nil {
		t.Fatal(err)
	}
	var names, heartbeats []string
	err := filepath.WalkDir(env.Dirs.State, func(path string, e fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		names = append(names, e.Name())
		if rel, _ := filepath.Rel(env.Dirs.State, path); !e.IsDir() && strings.HasPrefix(filepath.ToSlash(rel), "heartbeats/") && e.Name() != "uncaptured.json" {
			heartbeats = append(heartbeats, e.Name())
			return nil
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
	if !slices.Contains(names, "push-checked") || !slices.Contains(names, "notice.json") || !slices.Contains(names, "shown.json") {
		t.Fatalf("the state holds %q", names)
	}
	if !slices.Contains(heartbeats, "PostToolUseFailure.json") {
		t.Fatalf("the failed tool call left no heartbeat of its event: %q", heartbeats)
	}
	inbox, _ := os.ReadFile(firstFile(t, env.Dirs.Inbox()))
	if !strings.Contains(string(inbox), marker) {
		t.Fatal("the marker was not captured")
	}
	for _, name := range []string{"notice.json", "shown.json"} {
		if data, err := os.ReadFile(filepath.Join(env.Dirs.State, "health", name)); err != nil || strings.Contains(string(data), marker) {
			t.Errorf("%s: %v, holds the marker: %s", name, err, data)
		}
	}
}

// firstFile is the first .jsonl file under dir.
func firstFile(t *testing.T, dir string) string {
	t.Helper()
	var found string
	_ = filepath.WalkDir(dir, func(path string, e fs.DirEntry, err error) error {
		if err == nil && found == "" && strings.HasSuffix(path, ".jsonl") {
			found = path
		}
		return nil
	})
	if found == "" {
		t.Fatalf("no session file under %s", dir)
	}
	return found
}

// A handler at a chat's launch that panics starts nothing of its own and
// stops no other handler: the jobs the launch judged due start, a handler
// after it runs, and the hook exits 0, with the switch off and on (the
// lead's ruling 3, SEAM-0b's point).
func TestAPanicAtLaunchStartsWhatWasJudged(t *testing.T) {
	before := slices.Clone(handlers)
	t.Cleanup(func() { handlers = before })
	var session string
	registerHandler(405, "a step that fails", atLaunch, func(tn *turn) {
		if tn.session == session {
			tn.jobs = append(tn.jobs[:0], job{args: []string{"its own"}, reason: reasonDaily})
			panic("a step that fails")
		}
	})
	for _, on := range []bool{false, true} {
		env, p, spawned := noticeSetup(t)
		if !on {
			env.Config.Switches = nil
		}
		prepared(t, env, env.Now())
		pr := newProbe(t, &p, nil)
		session = p.Session
		jobs, runs := launched(t, pr, "claude-code", p, env, spawned)
		if slices.ContainsFunc(runs, func(r []string) bool { return r[0] == "its own" }) || !slices.ContainsFunc(runs, func(r []string) bool { return slices.Equal(r, []string{"maintain"}) }) {
			t.Fatalf("switch %v: started %q", on, runs)
		}
		if !reflect.DeepEqual(argsOf(jobs), runs) {
			t.Errorf("switch %v: the probe after the step saw %q, and %q started", on, argsOf(jobs), runs)
		}
		if got := shownFile(t, env) != ""; got != on {
			t.Errorf("switch %v: the handler after the step recorded the line: %v", on, got)
		}
	}
}
