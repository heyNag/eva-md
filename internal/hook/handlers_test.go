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
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/heyNag/eva-md/internal/catchup"
	"github.com/heyNag/eva-md/internal/inbox"
	"github.com/heyNag/eva-md/internal/paths"
	"github.com/heyNag/eva-md/internal/session"
	"github.com/heyNag/eva-md/internal/update"
	"github.com/heyNag/eva-md/internal/wikigit"
)

// The handler list, seen through a probe registered at each point, as a
// later package registers its own handler.

// points are the points of the list, in the order a run reaches them.
var points = []point{atGuarded, atStored, atBriefed, atLaunch}

// probePlace is where the probes take their places: after every handler of
// the package, so the probe at a point sees what they asked for there.
const probePlace = 9000

func init() {
	for i, at := range points {
		registerHandler(probePlace+i, fmt.Sprintf("test probe %d", at), at, func(t *turn) {
			if v, ok := probes.Load(t.session); ok {
				v.(*probe).see(at, t)
			}
		})
	}
}

// probes are the runs the probes record, by session: a run of any other
// session passes them by, so the package's other tests see no difference.
var probes sync.Map

// probeCount numbers the probes' sessions.
var probeCount atomic.Int64

// probe records what the handlers at each point saw in one session's runs,
// and with launch rewrites the jobs at atLaunch, once it has recorded them.
// With act, it then does what act does at the point it is called with.
type probe struct {
	mu     sync.Mutex
	seen   []seenTurn
	launch func([]job) []job
	act    func(point, *turn)
}

// seenTurn is a turn as a probe saw it.
type seenTurn struct {
	at                    point
	dirs                  paths.Dirs
	host, event, session  string
	source, project, file string
	headless              bool
	before                os.FileInfo
	now                   time.Time
	read                  []string // the names of the files the brief read
	recovered             []catchup.Recovered
	jobs                  []job
	contextIsLive         bool
}

func (p *probe) see(at point, t *turn) {
	p.mu.Lock()
	defer p.mu.Unlock()
	s := seenTurn{at: at, dirs: t.env.Dirs, host: t.host, event: t.event, session: t.session, source: t.source, project: t.project, file: t.file,
		headless: t.headless, before: t.before, now: t.now, recovered: slices.Clone(t.recovered)}
	for _, f := range t.read {
		s.read = append(s.read, f.Name)
	}
	for _, j := range t.jobs {
		j.args = slices.Clone(j.args)
		s.jobs = append(s.jobs, j)
	}
	s.contextIsLive = t.ctx != nil && t.ctx.Err() == nil
	p.seen = append(p.seen, s)
	if at == atLaunch && p.launch != nil {
		t.jobs = p.launch(t.jobs)
	}
	if p.act != nil {
		p.act(at, t)
	}
}

// turns returns what the probe has seen, and forgets it.
func (p *probe) turns() []seenTurn {
	p.mu.Lock()
	defer p.mu.Unlock()
	seen := p.seen
	p.seen = nil
	return seen
}

// newProbe gives the payload a session of its own that the probes record,
// with launch, when not nil, rewriting its jobs at atLaunch.
func newProbe(t *testing.T, p *payload, launch func([]job) []job) *probe {
	t.Helper()
	pr := &probe{launch: launch}
	p.Session = fmt.Sprintf("probe-%d", probeCount.Add(1))
	probes.Store(p.Session, pr)
	t.Cleanup(func() { probes.Delete(p.Session) })
	return pr
}

// atOf lists the points of seen, in order.
func atOf(seen []seenTurn) []point {
	var at []point
	for _, s := range seen {
		at = append(at, s.at)
	}
	return at
}

// argsOf lists the jobs' arguments.
func argsOf(jobs []job) [][]string {
	var out [][]string
	for _, j := range jobs {
		out = append(out, j.args)
	}
	return out
}

// A handler at each point runs once in each run, in the order of the
// points, and sees the run's facts: from atGuarded the host, the event,
// the session, its source, whether a program runs it, the project and the
// session's inbox file; from atStored the run's time and the file as it was
// before the hook; at atBriefed the session files the brief read and those
// start-of-chat recovery appended to; and at each the jobs asked so far,
// which at atLaunch are those judged due.
func TestHandlersRunOnceAtEachPoint(t *testing.T) {
	env, p, spawned := maintainSetup(t)
	base := env.Now()
	// An idle chat whose transcript holds a reply its Stop hook missed,
	// which this chat's start recovers.
	clock := base.Add(-time.Hour)
	env.Now = func() time.Time { return clock }
	idle := p
	idle.Session = "idle-chat"
	transcript := registeredTranscript(t, env, &idle)
	idleFile := inbox.Store{Root: env.Dirs.Inbox()}.Path("general", "claude-code", idle.Session)
	if err := os.Chtimes(idleFile, clock, clock); err != nil {
		t.Fatal(err)
	}
	appendRows(t, transcript, map[string]any{"type": "assistant", "sessionId": idle.Session, "cwd": idle.Cwd, "timestamp": clock.Format(time.RFC3339),
		"message": map[string]any{"role": "assistant", "stop_reason": "end_turn", "content": []map[string]any{{"type": "text", "text": "A reply the idle chat's Stop hook missed."}}}})
	clock = base
	n := len(spawned())
	pr := newProbe(t, &p, nil)
	own := sessionFile(t, env, "claude-code", p.Session, 3*promptBacklog, inbox.Mark{}, base.Add(-time.Hour))
	p.Event, p.Source = "SessionStart", "resume"
	invoke(t, "claude-code", p, env)

	seen := pr.turns()
	if got := atOf(seen); !reflect.DeepEqual(got, points) {
		t.Fatalf("the handlers ran at %v, want %v", got, points)
	}
	for _, s := range seen {
		if s.dirs != env.Dirs || s.host != "claude-code" || s.event != "SessionStart" || s.session != p.Session || s.source != "resume" || s.headless || s.project != "general" || s.file != own {
			t.Errorf("at %d the handler saw %+v", s.at, s)
		}
	}
	guarded, stored, briefed, launched := seen[0], seen[1], seen[2], seen[3]
	if !guarded.now.IsZero() || guarded.before != nil || guarded.read != nil || guarded.recovered != nil {
		t.Errorf("at atGuarded the handler saw what is learned later: %+v", guarded)
	}
	if !guarded.contextIsLive || !stored.contextIsLive || !briefed.contextIsLive {
		t.Error("a handler before the output saw the capture's time spent")
	}
	if !stored.now.Equal(base) || stored.before == nil || stored.before.Size() != 3*promptBacklog || stored.read != nil {
		t.Errorf("at atStored the handler saw %+v", stored)
	}
	if !slices.Contains(briefed.read, filepath.Base(idleFile)) || !slices.Contains(briefed.read, filepath.Base(own)) {
		t.Errorf("at atBriefed the brief's files were %q", briefed.read)
	}
	if want := []catchup.Recovered{{Host: "claude-code", Session: "idle-chat"}}; !reflect.DeepEqual(briefed.recovered, want) {
		t.Errorf("at atBriefed recovery had appended to %v, want %v", briefed.recovered, want)
	}
	if briefed.before == nil || briefed.before.Size() != 3*promptBacklog {
		t.Errorf("at atBriefed the file before the hook was %v", briefed.before)
	}
	// The catch-up asks for the day's sweep, which a job took today, so
	// launch drops it.
	catchUp := job{args: []string{"update", "--session", "claude-code:" + p.Session, "--session", "claude-code:idle-chat", update.TriggerFlag, update.TriggerStart}, reason: update.TriggerStart}
	asked := catchUp
	asked.args = append(slices.Clone(catchUp.args), sweepFlag)
	if want := []job{dailyJob, retryJob}; !reflect.DeepEqual(guarded.jobs, want) || !reflect.DeepEqual(stored.jobs, want) {
		t.Errorf("before the brief the jobs were %+v and %+v, want %+v", guarded.jobs, stored.jobs, want)
	}
	if want := []job{dailyJob, retryJob, asked}; !reflect.DeepEqual(briefed.jobs, want) {
		t.Errorf("at atBriefed the jobs were %+v, want %+v", briefed.jobs, want)
	}
	// The idle chat's start was the day's first event, which started the
	// day's maintain, and no push is pending.
	if want := []job{catchUp}; !reflect.DeepEqual(launched.jobs, want) {
		t.Errorf("at atLaunch the jobs were %+v, want %+v", launched.jobs, want)
	}
	if got := spawned()[n:]; !reflect.DeepEqual(got, argsOf(launched.jobs)) {
		t.Errorf("spawned %q, want %q", got, argsOf(launched.jobs))
	}

	// A prompt runs the brief's handlers when it gets a brief or refresh,
	// as the first prompt after a chat start does, and none once the chat
	// has used its refreshes up.
	p.Event, p.Source = "UserPromptSubmit", ""
	invoke(t, "claude-code", p, env)
	seen = pr.turns()
	if got := atOf(seen); !reflect.DeepEqual(got, points) {
		t.Fatalf("the first prompt after a chat start ran the handlers at %v, want %v", got, points)
	}
	for _, s := range seen {
		if s.event != "UserPromptSubmit" || s.session != p.Session || s.headless {
			t.Errorf("at %d a prompt was seen as %+v", s.at, s)
		}
	}
	used, err := json.Marshal(briefMarker{Variant: "resume", Refreshes: maxRefreshes})
	if err != nil {
		t.Fatal(err)
	}
	if err := (session.Markers{Root: env.Dirs.Sessions()}).SetContext(context.Background(), "claude-code", p.Session, session.Briefed, string(used)); err != nil {
		t.Fatal(err)
	}
	invoke(t, "claude-code", p, env)
	if got, want := atOf(pr.turns()), []point{atGuarded, atStored, atLaunch}; !reflect.DeepEqual(got, want) {
		t.Fatalf("a prompt with its refreshes used up ran the handlers at %v, want %v", got, want)
	}

	// A Stop has no brief to give, and a program's chat is seen as one.
	t.Setenv("CLAUDE_CODE_ENTRYPOINT", "sdk-cli")
	p.Event, p.Source = "Stop", ""
	reply := "Done."
	p.Reply = &reply
	invoke(t, "claude-code", p, env)
	seen = pr.turns()
	if got, want := atOf(seen), []point{atGuarded, atStored, atLaunch}; !reflect.DeepEqual(got, want) {
		t.Fatalf("a Stop ran the handlers at %v, want %v", got, want)
	}
	for _, s := range seen {
		if !s.headless || s.event != "Stop" || s.source != "" {
			t.Errorf("at %d a program's Stop was seen as %+v", s.at, s)
		}
	}
}

// A run that a guard stops runs no handler past it: one in an excluded
// folder runs none at all, and one in a chat off the record, or that
// recovery has not caught up, runs none once its event is stored. A run
// whose capture misses its deadline starts nothing, and runs no handler at
// atLaunch.
func TestHandlersDoNotRunPastAGuard(t *testing.T) {
	// Each guard that stops a run before atGuarded: the run is still its
	// session's, so the probe would see a handler that ran in it, at
	// atLaunch too.
	for _, guard := range []struct {
		name string
		stop func(t *testing.T, env *Env, p *payload)
	}{
		{"excluded folder", func(t *testing.T, env *Env, p *payload) { env.Config.Exclude = []string{p.Cwd} }},
		{"temporary folder", func(t *testing.T, env *Env, p *payload) {
			p.Cwd = filepath.Join(os.Getenv("TMPDIR"), "work")
			if err := os.MkdirAll(p.Cwd, 0o700); err != nil {
				t.Fatal(err)
			}
		}},
		{"the wiki", func(t *testing.T, env *Env, p *payload) {
			p.Cwd = env.Config.WikiPath
			if err := os.MkdirAll(p.Cwd, 0o700); err != nil {
				t.Fatal(err)
			}
		}},
		{"an eva-md job", func(t *testing.T, env *Env, p *payload) { t.Setenv("EVA_MD_INNER", "1") }},
		{"a remote session", func(t *testing.T, env *Env, p *payload) { t.Setenv("CLAUDE_CODE_REMOTE", "true") }},
	} {
		t.Run(guard.name, func(t *testing.T) {
			env, p, spawned := maintainSetup(t)
			pr := newProbe(t, &p, nil)
			guard.stop(t, &env, &p)
			reply := "Done."
			p.Reply = &reply
			for _, event := range []string{"SessionStart", "UserPromptSubmit", "Stop", "SessionEnd", "PreCompact", "PostToolUse"} {
				p.Event = event
				invoke(t, "codex", p, env)
			}
			if seen := pr.turns(); seen != nil {
				t.Fatalf("the runs ran handlers at %v", atOf(seen))
			}
			if got := spawned(); got != nil {
				t.Fatalf("the runs spawned %q", got)
			}
		})
	}
	// A chat start whose brief can't be built runs no brief's handler, so
	// asks for no catch-up, though its own backlog would be one: the
	// project's inbox folder becomes a link once the start is stored, which
	// the brief refuses to read through.
	t.Run("no brief", func(t *testing.T) {
		env, p, spawned := maintainSetup(t)
		pr := newProbe(t, &p, nil)
		sessionFile(t, env, "codex", p.Session, 3*promptBacklog, inbox.Mark{}, env.Now().Add(-time.Hour))
		pr.act = func(at point, tn *turn) {
			if at != atStored {
				return
			}
			folder := filepath.Join(env.Dirs.Inbox(), tn.project)
			if err := os.Rename(folder, folder+".moved"); err != nil {
				t.Error(err)
				return
			}
			if err := os.Symlink(folder+".moved", folder); err != nil {
				t.Error(err)
			}
		}
		p.Event, p.Source = "SessionStart", "resume"
		invoke(t, "codex", p, env)
		if got, want := atOf(pr.turns()), []point{atGuarded, atStored, atLaunch}; !reflect.DeepEqual(got, want) {
			t.Fatalf("a chat start with no brief ran the handlers at %v, want %v", got, want)
		}
		if got, want := spawned(), [][]string{{"maintain"}}; !reflect.DeepEqual(got, want) {
			t.Fatalf("a chat start with no brief spawned %q, want %q", got, want)
		}
	})
	t.Run("off the record", func(t *testing.T) {
		env, p, _ := maintainSetup(t)
		pr := newProbe(t, &p, nil)
		p.Prompt = "off the record"
		invoke(t, "codex", p, env)
		p.Prompt = "please help"
		reply := "Done."
		p.Reply = &reply
		for _, event := range []string{"UserPromptSubmit", "Stop", "SessionEnd", "PreCompact", "SessionStart"} {
			p.Event = event
			invoke(t, "codex", p, env)
		}
		seen := pr.turns()
		if len(seen) != 12 {
			t.Fatalf("six runs ran the handlers at %v", atOf(seen))
		}
		for _, s := range seen {
			if s.at != atGuarded && s.at != atLaunch {
				t.Fatalf("a chat off the record ran the handlers at %v", atOf(seen))
			}
		}
	})
	t.Run("recovery behind", func(t *testing.T) {
		env, p, _ := maintainSetup(t)
		pr := newProbe(t, &p, nil)
		env.recoverTranscript = func(context.Context, paths.Dirs, string, string) (int, error) {
			return 0, catchup.ErrPending
		}
		for _, event := range []string{"UserPromptSubmit", "SessionStart", "SessionEnd"} {
			p.Event = event
			invoke(t, "claude-code", p, env)
			if got, want := atOf(pr.turns()), []point{atGuarded, atLaunch}; !reflect.DeepEqual(got, want) {
				t.Fatalf("a %s recovery left behind ran the handlers at %v, want %v", event, got, want)
			}
		}
	})
	// As TestLateCaptureStartsNoMaintain: recovery holds the capture up,
	// past atGuarded, and ends the hook's time at that exact point.
	t.Run("past the deadline", func(t *testing.T) {
		env, p, spawned := maintainSetup(t)
		pr := newProbe(t, &p, nil)
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		release := make(chan struct{})
		held := env
		held.recoverTranscript = func(context.Context, paths.Dirs, string, string) (int, error) {
			cancel()
			<-release
			return 0, nil
		}
		p.Event = "PostToolUse"
		invokeContext(t, ctx, "codex", p, held)
		close(release)
		// Once the capture Run gave up on lets go of the session's lock, it
		// has run every handler it ever will.
		lockCtx, cancelLock := context.WithTimeout(context.Background(), hangGuard)
		defer cancelLock()
		unlock, err := sessionLock(lockCtx, env.Dirs.Locks(), "codex", p.Session)
		if err != nil {
			t.Fatalf("the capture Run gave up on still held the session's lock after %v: %v", hangGuard, err)
		}
		unlock()
		// It still writes its heartbeat after letting go of the lock: wait
		// for it to return, so that nothing writes under the test's folders
		// while they are removed.
		awaitNoCapture(t, "the capture Run gave up on")
		if got := atOf(pr.turns()); !slices.Contains(got, atGuarded) || slices.Contains(got, atLaunch) {
			t.Fatalf("a run past its deadline ran the handlers at %v", got)
		}
		if got := spawned(); got != nil {
			t.Fatalf("a run past its deadline spawned %q", got)
		}
	})
}

// launched runs the payload's event on host and returns the jobs judged due
// that the probe at atLaunch saw, and what env.spawn started.
func launched(t *testing.T, pr *probe, host string, p payload, env Env, spawned func() [][]string) ([]job, [][]string) {
	t.Helper()
	n := len(spawned())
	invoke(t, host, p, env)
	var jobs []job
	for _, s := range pr.turns() {
		if s.at == atLaunch {
			jobs = s.jobs
		}
	}
	var got [][]string
	if runs := spawned(); len(runs) > n {
		got = runs[n:]
	}
	return jobs, got
}

// Each of today's asks reaches env.spawn with today's exact arguments and
// is asked for its reason, one of reasons; only maintain's jobs run git,
// and only they keep its variables.
func TestEachAskReachesTheSpawnWithItsReason(t *testing.T) {
	t.Setenv("SSH_AUTH_SOCK", "/private/tmp/agent.sock")
	type want struct {
		args   []string
		reason string
	}
	for _, host := range hosts {
		own := func(p payload) string { return host + ":" + p.Session }
		for _, tc := range []struct {
			name    string
			prepare func(t *testing.T, env *Env, p *payload)
			want    func(p payload, day string) []want
		}{
			{"daily", func(t *testing.T, env *Env, p *payload) { p.Event = "PostToolUse" },
				func(payload, string) []want { return []want{{[]string{"maintain"}, reasonDaily}} }},
			{"push retry", func(t *testing.T, env *Env, p *payload) {
				commitRecords(t, *env, wikigit.Result{At: env.Now().Add(-2 * time.Hour).Format(time.RFC3339), Outcome: wikigit.Committed, Pages: 2, Push: wikigit.PushPending})
				if !firstOfDay(env.Dirs.State, env.Now()) {
					t.Fatal("the day's maintain had started")
				}
				p.Event, p.Source = "SessionStart", "startup"
			}, func(payload, string) []want { return []want{{[]string{"maintain", "--push-only"}, reasonPushRetry}} }},
			{"prompt gap", func(t *testing.T, env *Env, p *payload) {
				sessionFile(t, *env, host, p.Session, promptBacklog, inbox.Mark{}, env.Now().Add(-promptGap))
			}, func(p payload, _ string) []want {
				return []want{{[]string{"maintain"}, reasonDaily}, {[]string{"update", "--session", own(p), "--trigger", "prompt_gap"}, update.TriggerGap}}
			}},
			{"stop backlog", func(t *testing.T, env *Env, p *payload) {
				sessionFile(t, *env, host, p.Session, 2*stopBacklog, inbox.Mark{}, env.Now())
				reply := "Done."
				p.Event, p.Reply = "Stop", &reply
			}, func(p payload, _ string) []want {
				return []want{{[]string{"maintain"}, reasonDaily}, {[]string{"update", "--session", own(p), "--trigger", "stop_size"}, update.TriggerStop}}
			}},
			{"session end", func(t *testing.T, env *Env, p *payload) { p.Event = "SessionEnd" }, func(p payload, _ string) []want {
				return []want{{[]string{"maintain"}, reasonDaily}, {[]string{"update", "--session", own(p), "--trigger", "session_end"}, update.TriggerEnd}}
			}},
			{"compaction", func(t *testing.T, env *Env, p *payload) { p.Event = "PreCompact" }, func(p payload, _ string) []want {
				return []want{{[]string{"maintain"}, reasonDaily}, {[]string{"update", "--session", own(p), "--trigger", "compact"}, update.TriggerCompact}}
			}},
			{"chat start, the sweep taken today", func(t *testing.T, env *Env, p *payload) {
				sessionFile(t, *env, "codex", "idle-other", 1000, inbox.Mark{}, env.Now().Add(-time.Hour))
				p.Event, p.Source = "SessionStart", "startup"
			}, func(p payload, _ string) []want {
				return []want{{[]string{"maintain"}, reasonDaily}, {[]string{"update", "--session", "codex:idle-other", "--trigger", "chat_start"}, update.TriggerStart}}
			}},
			{"chat start, the sweep due", func(t *testing.T, env *Env, p *payload) {
				sessionFile(t, *env, "codex", "idle-other", 1000, inbox.Mark{}, env.Now().Add(-time.Hour))
				sweptOn(t, *env, env.Now().Add(-24*time.Hour))
				p.Event, p.Source = "SessionStart", "startup"
			}, func(p payload, day string) []want {
				return []want{{[]string{"maintain"}, reasonDaily}, {[]string{"update", "--session", "codex:idle-other", "--trigger", "chat_start", "--sweep=" + day}, update.TriggerStart}}
			}},
			{"chat start, the sweep alone", func(t *testing.T, env *Env, p *payload) {
				sweptOn(t, *env, env.Now().Add(-24*time.Hour))
				p.Event, p.Source = "SessionStart", "startup"
			}, func(p payload, day string) []want {
				return []want{{[]string{"maintain"}, reasonDaily}, {[]string{"update", "--sweep=" + day}, update.TriggerStart}}
			}},
			{"chat start, nothing to catch up", func(t *testing.T, env *Env, p *payload) {
				p.Event, p.Source = "SessionStart", "startup"
			}, func(payload, string) []want { return []want{{[]string{"maintain"}, reasonDaily}} }},
		} {
			t.Run(host+"/"+tc.name, func(t *testing.T) {
				env, p, spawned := maintainSetup(t)
				pr := newProbe(t, &p, nil)
				tc.prepare(t, &env, &p)
				jobs, got := launched(t, pr, host, p, env, spawned)
				want := tc.want(p, env.Now().Format(time.DateOnly))
				var wantArgs [][]string
				for i, w := range want {
					wantArgs = append(wantArgs, w.args)
					if i >= len(jobs) {
						continue
					}
					if jobs[i].reason != w.reason {
						t.Errorf("job %q was asked for %q, want %q", jobs[i].args, jobs[i].reason, w.reason)
					}
				}
				if !reflect.DeepEqual(got, wantArgs) || !reflect.DeepEqual(argsOf(jobs), wantArgs) {
					t.Fatalf("spawned %q from the jobs %q, want %q", got, argsOf(jobs), wantArgs)
				}
				for _, j := range jobs {
					maintain := j.reason == reasonDaily || j.reason == reasonPushRetry
					if !slices.Contains(reasons, j.reason) || j.git != maintain {
						t.Errorf("job %q: reason %q, git %v", j.args, j.reason, j.git)
					}
					if kept := slices.Contains(jobEnv(j), "SSH_AUTH_SOCK=/private/tmp/agent.sock"); kept != maintain {
						t.Errorf("job %q kept the SSH agent: %v", j.args, kept)
					}
				}
			})
		}
	}
}

// A handler at atLaunch sees the jobs judged due, and what it leaves is
// what starts: a job whose arguments it rewrites starts with them, and one
// that removes every job starts nothing, the day's maintain included.
func TestLaunchHandlersChangeWhatStarts(t *testing.T) {
	rewritten := []string{"update", "--session", "codex:rewritten"}
	for _, tc := range []struct {
		name   string
		launch func([]job) []job
		want   [][]string
	}{
		{"rewrite", func(jobs []job) []job {
			for i := range jobs {
				if jobs[i].reason == update.TriggerEnd {
					jobs[i].args = rewritten
				}
			}
			return jobs
		}, [][]string{{"maintain"}, rewritten}},
		{"remove all", func([]job) []job { return nil }, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			env, p, spawned := maintainSetup(t)
			pr := newProbe(t, &p, tc.launch)
			p.Event = "SessionEnd"
			jobs, got := launched(t, pr, "codex", p, env, spawned)
			if want := [][]string{{"maintain"}, {"update", "--session", "codex:" + p.Session, "--trigger", "session_end"}}; !reflect.DeepEqual(argsOf(jobs), want) {
				t.Fatalf("the handler at atLaunch saw %q, want %q", argsOf(jobs), want)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("spawned %q, want %q", got, tc.want)
			}
			// The day's maintain was judged due, and so taken, before the
			// handler ran: the day's next event starts none.
			p.Event = "Stop"
			if _, got := launched(t, pr, "codex", p, env, spawned); slices.ContainsFunc(got, func(args []string) bool { return args[0] == "maintain" }) {
				t.Fatalf("the next event spawned %q", got)
			}
		})
	}
	// A handler that changes a job's arguments in place changes no other
	// run's: the next day's maintain is still "maintain".
	env, p, spawned := maintainSetup(t)
	pr := newProbe(t, &p, func(jobs []job) []job {
		for _, j := range jobs {
			j.args[0] = "changed"
		}
		return jobs
	})
	p.Event = "Stop"
	if _, got := launched(t, pr, "codex", p, env, spawned); !reflect.DeepEqual(got, [][]string{{"changed"}}) {
		t.Fatalf("spawned %q", got)
	}
	if !reflect.DeepEqual(dailyJob.args, []string{"maintain"}) {
		t.Fatalf("the daily job became %q", dailyJob.args)
	}
}

// jobEnv gives gitEnv's variables by a job's git, not by its arguments:
// both of maintain's jobs get them, an update job gets none, and neither
// does an argument list alone, whatever its first word.
func TestJobEnvFollowsGit(t *testing.T) {
	setup(t)
	t.Setenv("SSH_AUTH_SOCK", "/private/tmp/agent.sock")
	agent := func(env []string) bool { return slices.Contains(env, "SSH_AUTH_SOCK=/private/tmp/agent.sock") }
	for _, tc := range []struct {
		name string
		env  []string
		want bool
	}{
		{"daily maintain", jobEnv(dailyJob), true},
		{"push retry", jobEnv(retryJob), true},
		{"update", jobEnv(job{args: []string{"update", "--session", "codex:x"}, reason: update.TriggerEnd}), false},
		{"maintain's arguments without git", jobEnv(job{args: []string{"maintain"}, reason: reasonDaily}), false},
		{"an argument list alone", jobEnv([]string{"maintain"}), false},
		{"another job that runs git", jobEnv(job{args: []string{"update"}, git: true}), true},
	} {
		if got := agent(tc.env); got != tc.want {
			t.Errorf("%s: kept the SSH agent %v, want %v", tc.name, got, tc.want)
		}
	}
}

// Two handlers at one place panic, naming both, and leave the list as it
// was; the list runs in the order of the places.
func TestHandlersAtOnePlacePanic(t *testing.T) {
	before := slices.Clone(handlers)
	func() {
		defer func() {
			msg := fmt.Sprint(recover())
			if !strings.Contains(msg, `"maintain"`) || !strings.Contains(msg, `"a second step"`) {
				t.Fatalf("panic: %s", msg)
			}
		}()
		registerHandler(100, "a second step", atStored, func(*turn) {})
	}()
	if len(handlers) != len(before) {
		t.Fatalf("the list holds %d handlers after the panic, %d before", len(handlers), len(before))
	}
	if !slices.IsSortedFunc(handlers, func(a, b handler) int { return a.place - b.place }) {
		t.Fatal("the handlers are not in the order of their places")
	}
	var names []string
	for _, h := range handlers {
		if h.place < probePlace {
			names = append(names, fmt.Sprintf("%d %s", h.at, h.name))
		}
	}
	want := []string{"0 maintain", "1 prompt gap", "1 stop backlog", "1 session end and compaction", "2 chat-start catch-up"}
	if !reflect.DeepEqual(names, want) {
		t.Fatalf("the package's handlers are %q, want %q", names, want)
	}
}

// Handlers registered out of the order of their places run in that order
// all the same: the list is kept in the order of the places, whichever
// file's init registers first.
func TestHandlersRunInTheOrderOfTheirPlaces(t *testing.T) {
	before := slices.Clone(handlers)
	t.Cleanup(func() { handlers = before })
	env, p, _ := maintainSetup(t)
	newProbe(t, &p, nil)
	var ran []string
	step := func(name string) func(*turn) {
		return func(tn *turn) {
			if tn.session == p.Session {
				ran = append(ran, name)
			}
		}
	}
	registerHandler(9500, "a later step", atStored, step("later"))
	registerHandler(9400, "an earlier step", atStored, step("earlier"))
	var places []int
	for _, h := range handlers {
		places = append(places, h.place)
	}
	if !slices.IsSorted(places) {
		t.Fatalf("the list holds the places %v", places)
	}
	invoke(t, "codex", p, env)
	if want := []string{"earlier", "later"}; !reflect.DeepEqual(ran, want) {
		t.Fatalf("the steps ran as %q, want %q", ran, want)
	}
}
