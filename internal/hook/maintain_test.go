package hook

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/heyNag/eva-md/internal/paths"
	"github.com/heyNag/eva-md/internal/wikigit"
)

// The daily maintain and the push retry, seen through the injected spawn.

var (
	daily = [][]string{{"maintain"}}
	retry = [][]string{{"maintain", "--push-only"}}
)

// maintainSetup is triggerSetup with a wiki set up by init, its path and
// its instance in the config, so the hooks start maintain.
func maintainSetup(t *testing.T) (Env, payload, func() [][]string) {
	t.Helper()
	env, p, spawned := triggerSetup(t)
	env.Config.Instance = "test-0a1b2c3d"
	return env, p, spawned
}

// maintainSpawns filters the maintain jobs out of what the hooks started.
func maintainSpawns(runs [][]string) [][]string {
	var out [][]string
	for _, r := range runs {
		if r[0] == "maintain" {
			out = append(out, r)
		}
	}
	return out
}

// since returns what spawned recorded after the first n runs, nil for
// nothing, and the new count.
func since(spawned func() [][]string, n int) ([][]string, int) {
	runs := spawned()
	if len(runs) == n {
		return nil, n
	}
	return maintainSpawns(runs[n:]), len(runs)
}

// commitRecords writes the month's commit records, oldest first, as
// eva-md commit appends them.
func commitRecords(t *testing.T, env Env, records ...wikigit.Result) {
	t.Helper()
	var b strings.Builder
	for _, r := range records {
		if r.Unstaged == nil {
			r.Unstaged = []string{}
		}
		data, err := json.Marshal(r)
		if err != nil {
			t.Fatal(err)
		}
		b.Write(append(data, '\n'))
	}
	dir := filepath.Join(env.Dirs.State, "commits")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, env.Now().Format("2006-01")+".jsonl"), []byte(b.String()), 0o600); err != nil {
		t.Fatal(err)
	}
}

// commitFolder lists <state>/commit.
func commitFolder(t *testing.T, env Env) []string {
	t.Helper()
	entries, _ := os.ReadDir(filepath.Join(env.Dirs.State, "commit"))
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names
}

// The first hook event of the local day starts maintain, whatever the
// event; every later event that day starts none, and the day after starts
// it again. Each costs one open of the day's attempt marker, which is all
// the commit folder holds besides the marker of a chat start's push-retry
// check. updates = false and commits = false still start it (maintain
// lints, and commits unless commits = false); a config without a wiki set
// up by init, or the headless child's hooks, never do.
func TestDailyMaintainStartsOnce(t *testing.T) {
	for _, host := range hosts {
		env, p, spawned := maintainSetup(t)
		day := env.Now()
		n := 0
		checked := false
		for i, event := range []string{"UserPromptSubmit", "Stop", "PostToolUse", "SessionStart", "SessionEnd"} {
			p.Event = event
			reply := "Done."
			p.Reply = &reply
			p.Turn = "turn-" + event
			invoke(t, host, p, env)
			want := daily
			if i > 0 {
				want = nil
			}
			var got [][]string
			if got, n = since(spawned, n); !reflect.DeepEqual(maintainSpawns(got), want) {
				t.Fatalf("%s %s: maintain spawns %q", host, event, got)
			}
			checked = checked || event == "SessionStart"
			names := []string{"attempt-" + day.Format(time.DateOnly)}
			if checked {
				names = append(names, "push-retry")
			}
			if got := commitFolder(t, env); !reflect.DeepEqual(got, names) {
				t.Fatalf("%s %s: the commit folder holds %q", host, event, got)
			}
		}
		env.Now = func() time.Time { return day.Add(24 * time.Hour) }
		p.Event = "PostToolUse"
		invoke(t, host, p, env)
		if got, _ := since(spawned, n); !reflect.DeepEqual(got, daily) {
			t.Fatalf("%s: the next day started %q", host, got)
		}
	}
	for _, tc := range []struct {
		name  string
		edit  func(*Env)
		inner bool
		want  [][]string
	}{
		{"updates off", func(e *Env) { e.Config.Updates = false }, false, daily},
		{"commits off", func(e *Env) { e.Config.Commits = false }, false, daily},
		{"no instance", func(e *Env) { e.Config.Instance = "" }, false, nil},
		{"no wiki", func(e *Env) { e.Config.WikiPath = "" }, false, nil},
		{"headless child", func(*Env) {}, true, nil},
	} {
		env, p, spawned := maintainSetup(t)
		tc.edit(&env)
		if tc.inner {
			t.Setenv("EVA_MD_INNER", "1")
		}
		p.Event = "PostToolUse"
		invoke(t, "codex", p, env)
		if got, _ := since(spawned, 0); !reflect.DeepEqual(maintainSpawns(got), tc.want) {
			t.Errorf("%s: maintain spawns %q", tc.name, got)
		}
	}
}

// While the newest commit record's push is pending, a chat start retries it
// with maintain --push-only, at most every 30 minutes, by the retry
// marker's time, which every check sets, whether it retried or not. A start
// that starts the day's maintain retries nothing, since that run pushes
// too; no other event retries. A diverged push, a refused run, a failed or
// declined commit and a push that went through are never retried, while a
// busy run and a run with nothing to commit, which skipped the push, are
// passed over. commits = false retries nothing.
func TestPushRetrySpacing(t *testing.T) {
	env, p, spawned := maintainSetup(t)
	now := env.Now()
	pending := wikigit.Result{At: now.Add(-2 * time.Hour).Format(time.RFC3339), Outcome: wikigit.Committed, Pages: 2, Push: wikigit.PushPending}
	commitRecords(t, env, pending)
	n := 0
	start := func(at time.Time) [][]string {
		t.Helper()
		env.Now = func() time.Time { return at }
		p.Event, p.Source = "SessionStart", "startup"
		invoke(t, "claude-code", p, env)
		var got [][]string
		got, n = since(spawned, n)
		return maintainSpawns(got)
	}
	for _, step := range []struct {
		at   time.Duration
		want [][]string
	}{
		{0, daily},
		{time.Minute, retry},
		{29 * time.Minute, nil},
		{32 * time.Minute, retry},
		{40 * time.Minute, nil},
	} {
		if got := start(now.Add(step.at)); !reflect.DeepEqual(got, step.want) {
			t.Fatalf("a start at +%s spawned %q, want %q", step.at, got, step.want)
		}
	}
	p.Event = "UserPromptSubmit"
	env.Now = func() time.Time { return now.Add(3 * time.Hour) }
	invoke(t, "claude-code", p, env)
	if got, _ := since(spawned, n); got != nil {
		t.Fatalf("a prompt retried the push: %q", got)
	}
	var at time.Time
	for i, tc := range []struct {
		name   string
		newest wikigit.Result
		want   [][]string
	}{
		{"busy passed over", wikigit.Result{Outcome: wikigit.Busy, Reason: wikigit.ReasonPushOnly, Push: wikigit.PushSkipped}, retry},
		{"nothing to commit passed over", wikigit.Result{Outcome: wikigit.Nothing, Push: wikigit.PushSkipped}, retry},
		{"diverged", wikigit.Result{Outcome: wikigit.Committed, Push: wikigit.PushDiverged}, nil},
		{"refused", wikigit.Result{Outcome: wikigit.Refused, Reason: "origin", Push: wikigit.PushSkipped}, nil},
		{"declined signature", wikigit.Result{Outcome: wikigit.Failed, Reason: "commit", Push: wikigit.PushSkipped}, nil},
		{"pushed", wikigit.Result{Outcome: wikigit.Nothing, Reason: wikigit.ReasonPushOnly, Push: wikigit.PushOK}, nil},
	} {
		at = now.Add(4*time.Hour + time.Duration(i)*(retryEvery+time.Minute))
		tc.newest.At = at.Add(-time.Minute).Format(time.RFC3339)
		commitRecords(t, env, pending, tc.newest)
		if got := start(at); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%s: spawned %q", tc.name, got)
		}
	}
	// The last check found nothing to retry and spaces the next one all the
	// same, so a push pending since then waits for it.
	commitRecords(t, env, pending)
	if got := start(at.Add(20 * time.Minute)); got != nil {
		t.Fatalf("a start 20 minutes after a check retried the push: %q", got)
	}
	env.Config.Commits = false
	if got := start(at.Add(40 * time.Minute)); got != nil {
		t.Fatalf("commits = false retried the push: %q", got)
	}
	env.Config.Commits = true
	if got := start(at.Add(45 * time.Minute)); !reflect.DeepEqual(got, retry) {
		t.Fatalf("the next check spawned %q", got)
	}
}

// A capture that misses its deadline delivers nothing, its spawns included:
// the day's first event, run out of time, creates no attempt marker and
// starts nothing, so the next event is still the day's first.
func TestLateCaptureStartsNoMaintain(t *testing.T) {
	env, p, spawned := maintainSetup(t)
	env.slack = 0
	release := make(chan struct{})
	env.recoverTranscript = func(context.Context, paths.Dirs, string, string) (int, error) {
		<-release
		return 0, nil
	}
	p.Event = "PostToolUse"
	running := runtime.NumGoroutine()
	invoke(t, "codex", p, env)
	// Let the capture Run gave up on finish before looking: even then it
	// leaves no marker.
	close(release)
	for end := time.Now().Add(2 * time.Second); runtime.NumGoroutine() > running && time.Now().Before(end); {
		time.Sleep(5 * time.Millisecond)
	}
	if got, names := spawned(), commitFolder(t, env); got != nil || names != nil {
		t.Fatalf("a capture past its deadline spawned %q and left %q", got, names)
	}
	env.slack = busySlack
	env.recoverTranscript = nil
	invoke(t, "codex", p, env)
	if got := maintainSpawns(spawned()); !reflect.DeepEqual(got, daily) {
		t.Fatalf("the next event started %q", got)
	}
}

// A detached maintain keeps what its git commit and push need beyond the
// update's allowlist, the SSH agent's socket and the user's git config
// among them; an update job keeps neither, and neither keeps an API key.
func TestMaintainJobEnvironment(t *testing.T) {
	maintainSetup(t)
	t.Setenv("SSH_AUTH_SOCK", "/private/tmp/agent.sock")
	t.Setenv("ANTHROPIC_API_KEY", "test-made-up-key")
	vars := func(env []string) map[string]string {
		m := map[string]string{}
		for _, kv := range env {
			if key, value, ok := strings.Cut(kv, "="); ok {
				m[key] = value
			}
		}
		return m
	}
	maintain, updates := vars(jobEnv(dailyJob)), vars(jobEnv([]string{"update", "--session", "codex:x"}))
	if maintain["SSH_AUTH_SOCK"] != "/private/tmp/agent.sock" || maintain["GIT_CONFIG_GLOBAL"] != os.Getenv("GIT_CONFIG_GLOBAL") || maintain["EVA_MD_INNER"] != "1" {
		t.Fatalf("maintain's environment: %v", maintain)
	}
	for _, key := range []string{"SSH_AUTH_SOCK", "GIT_CONFIG_GLOBAL"} {
		if _, ok := updates[key]; ok {
			t.Errorf("an update job kept %s", key)
		}
	}
	for _, env := range []map[string]string{maintain, updates} {
		if _, ok := env["ANTHROPIC_API_KEY"]; ok {
			t.Error("a job kept an API key")
		}
	}
}
