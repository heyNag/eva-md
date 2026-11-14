// Package v1arm is the continuation instrument's v1 condition, eva: what a
// person gets. In each trial it sets eva-md up in the trial's home, as v1's
// setup does, feeds the task's earlier chat to eva-md's hooks as a host
// would, runs v1's update on that chat, waits for the jobs the hooks
// started, and starts the new chat with eva-md's plugin loaded, as a
// person's chat. After the chat it reads the inbox: a trial whose chat the
// plugin's hooks never captured, or captured as no person's on Claude Code,
// where the evaluation build's seam makes a person of the chat, is
// memory_off, never read as v1. It sums what eva-md's updates spent, from
// their run log, for the ceiling and the rows. eva-md itself is unchanged:
// the arm runs the evaluation build given as --eva-md. No release contains
// this package.
//
// Every command the arm runs, a program given with --sign-in, git, setup,
// each hook, the update, runs in a process group of its own, killed past a
// limit, with at most 64 KiB of its output kept and none of it shown: a
// failure is a code, setup_failed or update_failed, and the trial writes no
// row. Setup, the update and the sign-in program run with the trial's home
// as their folder, never the workspace, where the run-book's wrapper, given
// as --cli, would take a call of the host's CLI for the chat's; only the
// replayed hooks name the workspace, as their payload's cwd and their
// folder, and they run eva-md, not the host's CLI.
//
// When the first two trials of a run both end setup_failed or
// update_failed, each further one would too, so the third stops the run
// before it runs anything of its own (the driver has made the trial's
// folders and asked the host's CLI its version by then), and the run ends
// in an error.
//
// What this cannot bound: the jobs eva-md's hooks start in a session of
// their own, out of any group the arm kills. The arm waits, before the
// chat and before it sums the run log, while one holds the update or the
// maintain lock; one that has started and not yet taken its lock is not
// seen, and the driver's removal of the trial's folder, which tries again
// while a file is held, is what ends a trial with it. Jobs started by the
// new chat's last hooks may call the model with the sign-in the trial's
// home holds, after the arm's last wait: what they spend before the
// trial's folder, and with it that sign-in, is removed is not counted. The
// run-book's limit on each key at its provider is what bounds it.
package v1arm

import (
	"context"
	"crypto/sha256"
	"debug/buildinfo"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/heyNag/eva-md/internal/eval/continuation/drive"
	"github.com/heyNag/eva-md/internal/eval/continuation/task"
	"github.com/heyNag/eva-md/internal/redact"
	"github.com/heyNag/eva-md/internal/update"
)

// Time limits of the commands the arm runs. Tests raise them once; a test
// that needs a short one ends it at an exact point instead (arm.limit).
var (
	SignInTimeout = 10 * time.Minute // the --sign-in program
	SetupTimeout  = 10 * time.Minute // git init and eva-md setup, each
	HookTimeout   = 2 * time.Minute  // each replayed hook
	UpdateTimeout = 30 * time.Minute // the update, its tries again on a busy lock included
	LockTimeout   = 30 * time.Minute // each wait for the jobs' locks
)

// The variables of the evaluation seam: Claude Code's entrypoint, which a
// hook reads to tell a program's chat from a person's, and the evaluation
// build's actor, which makes a person of the chat. Neither may be passed:
// the replayed hooks must lack both, and the chat gets the actor from the
// arm alone.
const (
	entrypointVar = "CLAUDE_CODE_ENTRYPOINT"
	actorVar      = "EVA_MD_EVAL_ACTOR"
)

// evalTag is the build tag of eva-md's evaluation build (cmd/eva-md's
// evalbuild.go).
const evalTag = "evamd_eval"

// outputLimit is the most of a command's stdout the arm keeps.
const outputLimit = 64 << 10

// skipTagCheck lets this package's tests run a fake eva-md, which is no
// evaluation build. Nothing else sets it.
var skipTagCheck bool

// arm is the condition's state: the sign-in program, and what it needs to
// stop a run whose first two trials failed.
type arm struct {
	// signIn is the program --sign-in gives, or "".
	signIn string
	// limit gives a command's context, which ends when d has passed; nil is
	// context.WithTimeout. A test ends it at an exact point instead.
	limit func(ctx context.Context, d time.Duration) (context.Context, context.CancelFunc)
	// now is the clock the run log's days are read by; nil is time.Now.
	now func() time.Time
	// poll is how often a held lock is looked at again.
	poll time.Duration
	// held, when set, is told each time a wait finds a lock held, by its
	// name. Tests release the lock from it.
	held func(lock string)

	mu   sync.Mutex
	runs map[string]*runState
}

// runState is what the arm knows of one run: the trials it saw, in order,
// and the codes the first two ended in.
type runState struct {
	seen    int
	ordinal map[string]int // by the trial's folder
	first   [2]string      // the codes of the first two trials, "" for none
}

// eva is the registered condition.
var eva = &arm{poll: 200 * time.Millisecond}

func init() {
	drive.Register(task.Eva, eva.drive())
}

// SetSignIn gives the program --sign-in names, run in each trial before
// setup (see signIn). The command calls it before the run; "" runs none.
func SetSignIn(path string) { eva.signIn = path }

// drive is the arm as the driver runs it.
func (a *arm) drive() drive.Arm {
	return drive.Arm{Check: a.check, Prepare: a.prepare, After: a.after}
}

// check refuses a run that names a variable of the seam with --pass-env,
// that gives no eva-md, one that is not the evaluation build, or a
// --sign-in that is not a program.
func (a *arm) check(o drive.Options) error {
	for _, name := range o.PassEnv {
		if name == entrypointVar || name == actorVar {
			return fmt.Errorf("--pass-env %s is the evaluation seam's, which the replayed hooks must lack and the arm gives the chat itself", name)
		}
	}
	if o.EvaMD == "" {
		return errors.New("it needs --eva-md, an evaluation build of eva-md")
	}
	if !skipTagCheck {
		info, err := buildinfo.ReadFile(o.EvaMD)
		if err != nil {
			return errors.New("--eva-md is not a Go program, or its build cannot be read")
		}
		if !slices.Contains(buildTags(info), evalTag) {
			return errors.New("--eva-md is not the evaluation build: it was built without the tag " + evalTag)
		}
	}
	if a.signIn != "" {
		if !filepath.IsAbs(a.signIn) {
			return errors.New("--sign-in is not an absolute path")
		}
		info, err := os.Stat(a.signIn)
		if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0o111 == 0 {
			return errors.New("--sign-in is not a file that can be run")
		}
	}
	return nil
}

// buildTags are the tags a Go program was built with.
func buildTags(info *buildinfo.BuildInfo) []string {
	for _, s := range info.Settings {
		if s.Key == "-tags" {
			return strings.Split(s.Value, ",")
		}
	}
	return nil
}

// unjudged leaves a trial unjudged with code.
func unjudged(code string) error { return &drive.Unjudged{Code: code} }

// setupFailed and updateFailed are the two codes the arm gives itself.
var (
	setupFailed  = unjudged(drive.CodeSetupFailed)
	updateFailed = unjudged(drive.CodeUpdateFailed)
)

// errStopped is the third trial's error when the first two of its run both
// ended setup_failed or update_failed.
var errStopped = errors.New("the first two trials both ended setup_failed or update_failed, so each further one would too: the run stops")

// state is the run of trial t, by its folder: <work>/<run>.
func (a *arm) state(t drive.Trial) *runState {
	run := filepath.Dir(filepath.Dir(filepath.Dir(t.Dirs.Root)))
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.runs == nil {
		a.runs = map[string]*runState{}
	}
	s, ok := a.runs[run]
	if !ok {
		s = &runState{ordinal: map[string]int{}}
		a.runs[run] = s
	}
	return s
}

// ended notes that trial t of the run s ended with err, when it is one of
// the run's first two.
func (a *arm) ended(s *runState, t drive.Trial, err error) {
	var u *drive.Unjudged
	if !errors.As(err, &u) {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if n := s.ordinal[t.Dirs.Root]; n == 1 || n == 2 {
		s.first[n-1] = u.Code
	}
}

// failedSetUp reports whether code is one the arm's set-up gives.
func failedSetUp(code string) bool {
	return code == drive.CodeSetupFailed || code == drive.CodeUpdateFailed
}

// prepare sets eva-md up in the trial, replays the earlier chat, runs the
// update and waits for the jobs, and gives the chat its plugin and the
// seam's actor.
func (a *arm) prepare(ctx context.Context, t drive.Trial) (drive.Launch, error) {
	s := a.state(t)
	a.mu.Lock()
	s.seen++
	s.ordinal[t.Dirs.Root] = s.seen
	stop := s.seen > 2 && failedSetUp(s.first[0]) && failedSetUp(s.first[1])
	a.mu.Unlock()
	if stop {
		return drive.Launch{}, errStopped
	}
	err := a.setUp(ctx, t)
	a.ended(s, t, err)
	if err != nil {
		return drive.Launch{}, err
	}
	return drive.Launch{Plugin: true, Env: []string{actorVar + "=person"}}, nil
}

// setUp is prepare's work, in order: the sign-in program, the wiki's bare
// remote, setup and the plugin's check, the earlier chat's hooks, the
// update, and the wait for the jobs the hooks started.
func (a *arm) setUp(ctx context.Context, t drive.Trial) error {
	if a.signIn != "" {
		if _, err := a.run(ctx, SignInTimeout, a.signIn, nil, t.Env, t.Dirs.Home, nil); err != nil {
			return a.failed(ctx, err, setupFailed)
		}
	}
	git, err := exec.LookPath("git")
	if err != nil {
		return setupFailed
	}
	remote := filepath.Join(t.Dirs.Root, "remote.git")
	if _, err := a.run(ctx, SetupTimeout, git, []string{"init", "--bare", "--quiet", remote}, t.Env, t.Dirs.Home, nil); err != nil {
		return a.failed(ctx, err, setupFailed)
	}
	evaMD := filepath.Join(t.Dirs.Bin, "eva-md")
	args := []string{"setup", "--dir", filepath.Join(t.Dirs.Home, "wiki"), "--remote", remote, "--no-push", "--yes", "--host", t.Host.Names.InstallName}
	if _, err := a.run(ctx, SetupTimeout, evaMD, args, t.Env, t.Dirs.Home, nil); err != nil {
		return a.failed(ctx, err, setupFailed)
	}
	if drive.PluginReady(t.Dirs.Home, t.Host) != nil {
		return setupFailed
	}
	session := SessionID(t)
	if clean, n := redact.Redact(session); n != 0 || clean != session {
		return errors.New("the replayed session's id is one a hook would refuse")
	}
	for _, p := range Payloads(t.Task, session, t.Dirs.Workspace) {
		data, err := json.Marshal(p)
		if err != nil {
			return err
		}
		if _, err := a.run(ctx, HookTimeout, evaMD, []string{"hook", "--host", string(t.Host.ID)}, t.Env, t.Dirs.Workspace, data); err != nil {
			return a.failed(ctx, err, setupFailed)
		}
	}
	if err := a.update(ctx, t, evaMD, session); err != nil {
		return err
	}
	return a.waitJobs(ctx, t)
}

// failed is the error a command's failure gives: the run's own end as it
// is, else code.
func (a *arm) failed(ctx context.Context, err, code error) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return code
}

// busyLine is what the update prints when another job holds its lock.
const busyLine = "stopped: " + update.StopBusy

// update runs eva-md update --session <host>:<session> --foreground, again
// each time its line says another job held the lock, once that lock is
// free, all within UpdateTimeout. The update counts when it exits 0 and
// the run log holds a pass of the session that validated, ok or no_change,
// whichever job made it: a detached job may have taken the session before
// it. Anything else is update_failed.
func (a *arm) update(ctx context.Context, t drive.Trial, evaMD, session string) error {
	uctx, cancel := a.within(ctx, UpdateTimeout)
	defer cancel()
	args := []string{"update", "--session", string(t.Host.ID) + ":" + session, "--foreground"}
	for {
		out, err := a.run(uctx, 0, evaMD, args, t.Env, t.Dirs.Home, nil)
		if err != nil {
			return a.failed(ctx, err, updateFailed)
		}
		if !busy(out) {
			break
		}
		if err := a.waitFree(uctx, t, updateLock); err != nil {
			return a.failed(ctx, err, updateFailed)
		}
	}
	records, err := update.Days(stateDir(t), a.clock().AddDate(0, 0, 1), 3)
	if err != nil {
		return updateFailed
	}
	for _, r := range records {
		if r.Host == string(t.Host.ID) && r.Session == session && (r.Outcome == update.OutcomeOK || r.Outcome == update.OutcomeNoChange) {
			return nil
		}
	}
	return updateFailed
}

// busy reports whether an update's output says another job held its lock:
// its summary line, "update: <line>", names busy among its stops.
func busy(out []byte) bool {
	for _, line := range strings.Split(string(out), "\n") {
		if summary, ok := strings.CutPrefix(line, "update: "); ok {
			for _, part := range strings.Split(summary, "; ") {
				if stops, ok := strings.CutPrefix(part, "stopped: "); ok && slices.Contains(strings.Split(stops, ", "), update.StopBusy) {
					return true
				}
			}
		}
	}
	return false
}

// after waits for the jobs the chat's hooks started, sums what eva-md's
// updates spent, and leaves the trial memory_off when the inbox shows the
// plugin's hooks never captured the new chat as v1 must.
func (a *arm) after(ctx context.Context, t drive.Trial) (drive.Cost, error) {
	s := a.state(t)
	if err := a.waitJobs(ctx, t); err != nil {
		a.ended(s, t, err)
		// What the run log holds is spent all the same, the trial's own
		// update's included, and counts toward the ceiling; a job still
		// spending is why the wait ran out.
		cost, _ := a.cost(t)
		return cost, err
	}
	cost, err := a.cost(t)
	if err != nil {
		return drive.Cost{}, err
	}
	on, err := memoryOn(ctx, t, SessionID(t))
	if err != nil {
		return cost, err
	}
	if !on {
		return cost, unjudged(drive.CodeMemoryOff)
	}
	return cost, nil
}

// cost sums the tokens and the time of every record in eva-md's run log of
// the trial's days: its home is the trial's own, so each is one of the
// trial's updates.
func (a *arm) cost(t drive.Trial) (drive.Cost, error) {
	records, err := update.Days(stateDir(t), a.clock().AddDate(0, 0, 1), 3)
	if err != nil {
		return drive.Cost{}, errors.New("eva-md's run log cannot be read, so what its updates spent is not known")
	}
	var c drive.Cost
	for _, r := range records {
		c.Tokens += int64(r.TokensIn) + int64(r.TokensOut)
		c.Ms += r.DurationMS
	}
	return c, nil
}

// clock is the time the run log's days are read by. The days read run
// from yesterday to tomorrow: eva-md names a day's file by its own local
// time, which may not be the arm's.
func (a *arm) clock() time.Time {
	if a.now != nil {
		return a.now()
	}
	return time.Now()
}

// stateDir is eva-md's state folder in the trial's home.
func stateDir(t drive.Trial) string {
	return filepath.Join(t.Dirs.Home, ".local", "state", "eva-md")
}

// SessionID is the earlier chat's session id in trial t: made from the
// trial alone, the task's id and the trial's number, in the shape of a
// host's own (a UUID), so that a hook takes it.
func SessionID(t drive.Trial) string {
	sum := sha256.Sum256([]byte("eva-md continuation earlier chat\x00" + t.Task.ID + "\x00" + strconv.Itoa(t.Number)))
	b := sum[:16]
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// Payload is one hook call of the replayed chat, in the fields of eva-md's
// hook payload (internal/hook) the arm sets. It names no transcript.
type Payload struct {
	Session string  `json:"session_id"`
	Cwd     string  `json:"cwd"`
	Event   string  `json:"hook_event_name"`
	Source  string  `json:"source,omitempty"`
	Prompt  string  `json:"prompt,omitempty"`
	Reply   *string `json:"last_assistant_message,omitempty"`
}

// Payloads is the earlier chat of t as a host's hooks would give it, in
// order: the start, a prompt for each user's turn, a stop for each agent's,
// and the end.
func Payloads(t task.Task, session, cwd string) []Payload {
	ps := []Payload{{Session: session, Cwd: cwd, Event: "SessionStart", Source: "startup"}}
	for _, turn := range t.History {
		if turn.By == "user" {
			ps = append(ps, Payload{Session: session, Cwd: cwd, Event: "UserPromptSubmit", Prompt: turn.Text})
			continue
		}
		reply := turn.Text
		ps = append(ps, Payload{Session: session, Cwd: cwd, Event: "Stop", Reply: &reply})
	}
	return append(ps, Payload{Session: session, Cwd: cwd, Event: "SessionEnd"})
}
