package hook

import (
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/heyNag/eva-md/internal/config"
	"github.com/heyNag/eva-md/internal/inbox"
	"github.com/heyNag/eva-md/internal/update"
	"github.com/heyNag/eva-md/internal/wikigit"
)

// The wake handler, seen through the probe at atLaunch, which runs after
// it, and through the injected spawn.

// askCase is one of today's asks: how a test brings it about, and what
// the hook starts for it with the switch off, v1's arguments, and on, its
// wake form, given the run's payload and day.
type askCase struct {
	name    string
	prepare func(t *testing.T, env *Env, p *payload)
	v1      func(own, day string) [][]string
	wake    func(own, day string) [][]string
}

// wakeOf is a wake form's arguments.
func wakeOf(reason string, rest ...string) []string {
	return append([]string{"work", "--wake", "--reason", reason}, rest...)
}

// askCases are each of today's asks, the day's maintain with each.
func askCases(host string) []askCase {
	dailyV1, dailyWake := []string{"maintain"}, wakeOf("daily", "--maintain")
	updateV1 := func(trigger, own string) []string {
		return []string{"update", "--session", own, update.TriggerFlag, trigger}
	}
	return []askCase{
		{"daily", func(t *testing.T, env *Env, p *payload) { p.Event = "PostToolUse" },
			func(string, string) [][]string { return [][]string{dailyV1} },
			func(string, string) [][]string { return [][]string{dailyWake} }},
		{"push retry", func(t *testing.T, env *Env, p *payload) {
			commitRecords(t, *env, wikigit.Result{At: env.Now().Add(-2 * time.Hour).Format(time.RFC3339), Outcome: wikigit.Committed, Pages: 2, Push: wikigit.PushPending})
			if !firstOfDay(env.Dirs.State, env.Now()) {
				t.Fatal("the day's maintain had started")
			}
			p.Event, p.Source = "SessionStart", "startup"
		},
			func(string, string) [][]string { return [][]string{{"maintain", "--push-only"}} },
			func(string, string) [][]string { return [][]string{wakeOf("push_retry", "--maintain", "--push-only")} }},
		{"prompt gap", func(t *testing.T, env *Env, p *payload) {
			sessionFile(t, *env, host, p.Session, promptBacklog, inbox.Mark{}, env.Now().Add(-promptGap))
		},
			func(own, _ string) [][]string { return [][]string{dailyV1, updateV1("prompt_gap", own)} },
			func(own, _ string) [][]string { return [][]string{dailyWake, wakeOf("prompt_gap", "--session", own)} }},
		{"stop backlog", func(t *testing.T, env *Env, p *payload) {
			sessionFile(t, *env, host, p.Session, 2*stopBacklog, inbox.Mark{}, env.Now())
			reply := "Done."
			p.Event, p.Reply = "Stop", &reply
		},
			func(own, _ string) [][]string { return [][]string{dailyV1, updateV1("stop_size", own)} },
			func(own, _ string) [][]string { return [][]string{dailyWake, wakeOf("stop_size", "--session", own)} }},
		{"session end", func(t *testing.T, env *Env, p *payload) { p.Event = "SessionEnd" },
			func(own, _ string) [][]string { return [][]string{dailyV1, updateV1("session_end", own)} },
			func(own, _ string) [][]string { return [][]string{dailyWake, wakeOf("session_end", "--session", own)} }},
		{"compaction", func(t *testing.T, env *Env, p *payload) { p.Event = "PreCompact" },
			func(own, _ string) [][]string { return [][]string{dailyV1, updateV1("compact", own)} },
			func(own, _ string) [][]string { return [][]string{dailyWake, wakeOf("compact", "--session", own)} }},
		{"chat start, the sweep taken today", func(t *testing.T, env *Env, p *payload) {
			sessionFile(t, *env, "codex", "idle-other", 1000, inbox.Mark{}, env.Now().Add(-time.Hour))
			p.Event, p.Source = "SessionStart", "startup"
		},
			func(string, string) [][]string {
				return [][]string{dailyV1, updateV1("chat_start", "codex:idle-other")}
			},
			func(string, string) [][]string {
				return [][]string{dailyWake, wakeOf("chat_start", "--session", "codex:idle-other")}
			}},
		{"chat start, the sweep due", func(t *testing.T, env *Env, p *payload) {
			sessionFile(t, *env, "codex", "idle-other", 1000, inbox.Mark{}, env.Now().Add(-time.Hour))
			sweptOn(t, *env, env.Now().Add(-24*time.Hour))
			p.Event, p.Source = "SessionStart", "startup"
		},
			func(_, day string) [][]string {
				return [][]string{dailyV1, append(updateV1("chat_start", "codex:idle-other"), "--sweep="+day)}
			},
			func(_, day string) [][]string {
				return [][]string{dailyWake, wakeOf("chat_start", "--session", "codex:idle-other", "--sweep="+day)}
			}},
		{"chat start, the sweep alone", func(t *testing.T, env *Env, p *payload) {
			sweptOn(t, *env, env.Now().Add(-24*time.Hour))
			p.Event, p.Source = "SessionStart", "startup"
		},
			func(_, day string) [][]string { return [][]string{dailyV1, {"update", "--sweep=" + day}} },
			func(_, day string) [][]string { return [][]string{dailyWake, wakeOf("chat_start", "--sweep="+day)} }},
	}
}

// workerConfig loads a config whose [switches] table is body, as a person
// writes it, with the wiki the hooks' tests use, so the switch is read as
// eva-md reads it.
func workerConfig(t *testing.T, env Env, body string) config.Config {
	t.Helper()
	file := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(file, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	c, err := config.Load(file)
	if err != nil {
		t.Fatal(err)
	}
	c.ReminderEvery, c.WikiPath, c.Instance = env.Config.ReminderEvery, env.Config.WikiPath, env.Config.Instance
	return c
}

// With the switch worker off, absent or set false, each of today's asks
// starts today's arguments, as v1's own jobs, with their reasons and git
// only for maintain; with it on, each starts its wake form, with the same
// reason, the same sessions, sweep day and push flag, and git set.
func TestEachAskReachesTheSpawnAsItsWakeFormWithTheSwitchOn(t *testing.T) {
	for _, sw := range []struct {
		name string
		body string
		on   bool
	}{
		{"absent", "", false},
		{"false", "[switches]\nworker = false\n", false},
		{"true", "[switches]\nworker = true\n", true},
	} {
		for _, host := range hosts {
			for _, tc := range askCases(host) {
				t.Run(sw.name+"/"+host+"/"+tc.name, func(t *testing.T) {
					env, p, spawned := maintainSetup(t)
					env.Config = workerConfig(t, env, sw.body)
					pr := newProbe(t, &p, nil)
					tc.prepare(t, &env, &p)
					jobs, got := launched(t, pr, host, p, env, spawned)
					own, day := host+":"+p.Session, env.Now().Format(time.DateOnly)
					want := tc.v1(own, day)
					if sw.on {
						want = tc.wake(own, day)
					}
					if !reflect.DeepEqual(got, want) || !reflect.DeepEqual(argsOf(jobs), want) {
						t.Fatalf("spawned %q from the jobs %q, want %q", got, argsOf(jobs), want)
					}
					for _, j := range jobs {
						maintain := j.reason == reasonDaily || j.reason == reasonPushRetry
						if !slices.Contains(reasons, j.reason) || j.git != (sw.on || maintain) {
							t.Errorf("job %q: reason %q, git %v", j.args, j.reason, j.git)
						}
						if sw.on && j.args[3] != j.reason {
							t.Errorf("job %q wakes for %q, asked for %q", j.args, j.args[3], j.reason)
						}
					}
				})
			}
		}
	}
}

// A fault in the wake leaves the jobs as launch judged them, so each
// starts as v1's would: the day's maintain, whose attempt file launch took
// before the handler ran, included, rather than nothing until tomorrow.
func TestAFaultInTheWakeStartsTheJudgedJobs(t *testing.T) {
	saved := wakeForm
	t.Cleanup(func() { wakeForm = saved })
	calls := 0
	wakeForm = func(j job) job {
		if calls++; calls == 2 {
			panic("a fault in the wake")
		}
		return wakeJob(j)
	}
	env, p, spawned := maintainSetup(t)
	env.Config.Switches = map[string]any{"worker": true}
	p.Event = "SessionEnd"
	invoke(t, "codex", p, env)
	want := [][]string{{"maintain"}, {"update", "--session", "codex:" + p.Session, update.TriggerFlag, update.TriggerEnd}}
	if got := spawned(); calls != 2 || !reflect.DeepEqual(got, want) {
		t.Fatalf("after %d calls spawned %q, want %q", calls, got, want)
	}
	if firstOfDay(env.Dirs.State, env.Now()) {
		t.Fatal("the day's attempt file was not taken")
	}
}

// The wake form takes only what it knows: any other command, a job with no
// reason, an update without a session or a sweep's day, and an argument it
// doesn't know are left as they are, so the hook starts v1's job; a
// maintain and an update it knows are rewritten, git set, and the job's
// own arguments are not changed.
func TestTheWakeFormTakesOnlyWhatItKnows(t *testing.T) {
	for _, tc := range []struct {
		in   job
		want []string
	}{
		{job{args: []string{"index"}, reason: reasonDaily}, nil},
		{job{args: []string{"update", "--session", "codex:a"}}, nil},
		{job{args: []string{"update", "--session", "codex:a"}, reason: "a_reason_to_come"}, nil},
		{job{args: []string{"maintain"}, reason: "a_reason_to_come"}, nil},
		{job{args: nil, reason: reasonDaily}, nil},
		{job{args: []string{"update", "--pending"}, reason: update.TriggerEnd}, nil},
		{job{args: []string{"update", "--sweep"}, reason: update.TriggerStart}, nil},
		{job{args: []string{"update", "--session"}, reason: update.TriggerEnd}, nil},
		{job{args: []string{"maintain", "--all"}, reason: reasonDaily}, nil},
		{job{args: []string{"maintain", "--push-only", "--push-only"}, reason: reasonPushRetry}, nil},
		{job{args: []string{"maintain"}, reason: reasonDaily}, wakeOf("daily", "--maintain")},
		{job{args: []string{"update", "--session", "codex:A b", "--session", "claude-code:c", "--trigger", "session_end", "--sweep=2026-01-02"}, reason: update.TriggerEnd},
			wakeOf("session_end", "--session", "codex:A b", "--session", "claude-code:c", "--sweep=2026-01-02")},
	} {
		in := job{args: slices.Clone(tc.in.args), reason: tc.in.reason}
		got := wakeJob(in)
		if !reflect.DeepEqual(in.args, tc.in.args) {
			t.Errorf("%q: the job's own arguments became %q", tc.in.args, in.args)
		}
		if tc.want == nil {
			if !reflect.DeepEqual(got, in) {
				t.Errorf("%q: became %+v, want it left", tc.in.args, got)
			}
			continue
		}
		if !reflect.DeepEqual(got.args, tc.want) || got.reason != tc.in.reason || !got.git {
			t.Errorf("%q: became %+v, want %q with git", tc.in.args, got, tc.want)
		}
	}
}

// The reasons a wake form carries are the hooks' reasons, every one but
// health, whose job, doctor --notice, is never an update or a maintain and
// so never a wake: a reason added to the hooks is added to WakeReasons, and
// so to the words eva-md work --wake takes, or this fails.
func TestWakeReasonsAreTheHooksReasons(t *testing.T) {
	hooks := slices.DeleteFunc(slices.Clone(reasons), func(r string) bool { return r == reasonHealth })
	got, want := slices.Sorted(slices.Values(WakeReasons)), slices.Sorted(slices.Values(hooks))
	if !slices.Equal(got, want) {
		t.Fatalf("WakeReasons %q, the hooks' reasons %q", got, want)
	}
	if j := wakeJob(noticeJob); !reflect.DeepEqual(j, noticeJob) {
		t.Fatalf("the notice's job became %+v", j)
	}
}
