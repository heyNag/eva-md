// Package drive is the continuation instrument's driver: it runs a task's
// new chat on a host's CLI in a trial folder of its own, grades what the
// chat leaves with the task's hidden tests, supplies the facts the failing
// tests encode, one round at a time, and writes what the trial cost as
// result rows. A condition is registered with Register; this package
// registers two, memory off and the ideal record. No release contains it.
//
// A trial's environment is built from nothing: its own home, temporary
// folder and bin folder, and only the variables the run names. Nothing it
// starts runs outside its folder, and it never writes a host's trust
// record or settings. A trial a host failure leaves unjudged writes no row,
// and the run goes on to the next. A trial unjudged for a fault of the
// set-up, a call that gave no usage among them, stops the run: every
// further trial would spend a session to learn nothing.
package drive

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/heyNag/eva-md/internal/eval/continuation/task"
	"github.com/heyNag/eva-md/internal/eval/results"
	"github.com/heyNag/eva-md/internal/host/adapter"
)

// Codes of an unjudged trial: a closed list.
const (
	CodeSignIn       = "sign_in"
	CodeRateLimited  = "rate_limited"
	CodeTimeout      = "timeout"
	CodeOtherModel   = "other_model"
	CodeSetupFailed  = "setup_failed"
	CodeUpdateFailed = "update_failed"
	// CodeHostFailed is a call that gave no answer for a reason of the
	// host's that is no sign-in, rate limit or timeout: a failure of its
	// service (an overload or another 5xx), an exit that printed nothing or
	// no final event, a CLI a signal ended (even after it printed an
	// answer), or an answer that names no session when the trial needs a
	// follow-up.
	CodeHostFailed = "host_failed"
	// CodeNoUsage is a call that answered without its usage, or with 0
	// prompt and output tokens, or whose output ran past its limit: what it
	// spent no ceiling can count.
	CodeNoUsage = "no_usage"
	// CodeMemoryOff is a condition that found the host's own memory on
	// where it must be off. Only an Arm gives it.
	CodeMemoryOff = "memory_off"
	// CodeHiddenFound is a hidden test, or its bytes, found within a
	// chat's reach.
	CodeHiddenFound = "hidden_found"
)

// Codes lists every code of an unjudged trial, in a fixed order.
var Codes = []string{CodeSignIn, CodeRateLimited, CodeTimeout, CodeOtherModel, CodeSetupFailed, CodeUpdateFailed, CodeHostFailed,
	CodeNoUsage, CodeMemoryOff, CodeHiddenFound}

// stopCodes are the codes that stop the run: each says the set-up is
// wrong, so every further trial would spend a session to learn nothing.
var stopCodes = []string{CodeNoUsage, CodeMemoryOff, CodeHiddenFound}

// Stopped is Run's error when a trial left unjudged with one of the codes
// that stop a run ended it: the trials after it were not run.
type Stopped struct {
	Code string
}

func (e *Stopped) Error() string { return "the run stopped: a trial is unjudged, " + e.Code }

// Unjudged is the error that leaves a trial unjudged: a host or a
// condition failed in a way that says nothing of the answer.
type Unjudged struct {
	Code string
}

func (u *Unjudged) Error() string { return "the trial is unjudged: " + u.Code }

// DefaultSessionTimeout is a session's time limit when the run gives none.
const DefaultSessionTimeout = 30 * time.Minute

// Options are a run's flags.
type Options struct {
	Tasks     string // the folder of tasks
	Repos     string // the folder of fixture repositories
	Host      string // an adapter id
	CLI       string // the host's CLI, by absolute path
	Condition task.Condition
	Trials    int
	Work      string // where the trials' folders are made
	Out       string // the results folder
	// MaxSessions and MaxTokens are the ceilings; neither has a default.
	MaxSessions    int64
	MaxTokens      int64
	Model          string        // the model asked for; "" for the host's default
	SessionTimeout time.Duration // 0 for DefaultSessionTimeout
	PassEnv        []string      // the variables of the invoking environment a chat gets
	EvaMD          string        // an eva-md binary, linked into each trial's bin/ as eva-md
	Repo           string        // the repository the rows' commit is read from
	Spend          bool          // --spend-model-quota: without it nothing starts

	// Delivered says whether the chat of a trial was given a fact, by id,
	// before the round that supplied it. Nil: not measured, and no
	// asked_again row is written.
	Delivered func(t Trial, fact string) (bool, error)
	// Clock is the run's clock; nil is time.Now.
	Clock func() time.Time

	// sessionLimit gives a call's context, which ends when what is left of
	// the session's time limit, d, has passed; nil is context.WithTimeout.
	// A test ends it at an exact point instead.
	sessionLimit func(ctx context.Context, d time.Duration) (context.Context, context.CancelFunc)
	// outputLimit is the most a call may print on stdout; 0 is
	// outputLimit's. A test gives a small one.
	outputLimit int
}

// Dirs are a trial's folders.
type Dirs struct {
	Root      string // <work>/<run>/<task>/<condition>/t<n>
	Home      string
	Tmp       string
	Bin       string
	Workspace string // where the chat runs
	Grade     string
}

// Trial is one trial, as a condition sees it.
type Trial struct {
	Task      task.Task
	Condition task.Condition
	Number    int
	Dirs      Dirs
	Host      adapter.Host
	CLI       string   // the CLI's link in Dirs.Bin
	Env       []string // the chat's environment, before Launch.Env
	Options   Options
}

// Launch is what a condition adds to a trial's chat.
type Launch struct {
	Args []string // added to each call's flags
	Env  []string // NAME=value, added to the chat's environment
	// Plugin loads eva-md's plugin from the trial's home, as the host
	// table says; otherwise the host's own memory is off and no plugin is
	// loaded.
	Plugin bool
}

// Cost is what a condition spent outside the chat.
type Cost struct {
	Tokens int64
	Ms     int64
}

// Arm is a condition's part in a trial.
type Arm struct {
	// Check runs once before any trial; an error starts none.
	Check func(o Options) error
	// Prepare runs in each trial before its chat.
	Prepare func(ctx context.Context, t Trial) (Launch, error)
	// After runs once the trial's chat has ended.
	After func(ctx context.Context, t Trial) (Cost, error)
}

var arms = map[task.Condition]Arm{}

// Register adds the condition c. It panics on a condition the task format
// does not know or one registered twice: each is a mistake in the program.
func Register(c task.Condition, a Arm) {
	if _, err := task.ParseCondition(string(c)); err != nil {
		panic("drive: unknown condition " + string(c))
	}
	if _, taken := arms[c]; taken {
		panic("drive: the condition " + string(c) + " is registered twice")
	}
	arms[c] = a
}

func init() {
	// Memory off: the repository alone. The ideal record: the task's note
	// heads the first prompt (task.FirstPrompt); nothing else differs.
	Register(task.Off, Arm{})
	Register(task.Oracle, Arm{})
}

// UsageError is a run refused before anything starts.
type UsageError struct{ msg string }

func (e *UsageError) Error() string { return e.msg }

func usagef(format string, args ...any) error { return &UsageError{fmt.Sprintf(format, args...)} }

// ErrNoQuota is Run's error when the plan was printed and nothing started,
// since the run was not given leave to spend model quota.
var ErrNoQuota = errors.New("nothing started: the run needs --spend-model-quota to start a chat")

// Summary is what a run did.
type Summary struct {
	Run      string // the run's id, the name of its folder under Work
	Planned  int
	Judged   int
	NotRun   int
	Unjudged map[string]int // by code
	// NoModel counts the judged trials whose rows carry no model, since the
	// host named none, or named one that is no id: a session that names no
	// model is judged, with --model given or not.
	NoModel int
	Tokens  int64 // prompt and output tokens of every chat, and what conditions spent
	Rows    int
}

// envName is a variable's name that --pass-env may take.
var envName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// builtEnv are the variables the driver sets itself.
var builtEnv = []string{"HOME", "PATH", "TMPDIR", "GIT_CONFIG_NOSYSTEM"}

// redirectEnv are the variables --pass-env never passes besides the
// hosts' ConfigEnv: those that point git, or any program that follows the
// XDG folders, at a configuration other than the trial home's, and those
// that reach the invoking user's keys.
var (
	redirectPrefixes = []string{"GIT_", "XDG_"}
	redirectNames    = []string{"GNUPGHOME", "SSH_AUTH_SOCK"}
)

// setsOff reports whether a host's OffEnv sets the variable name.
func setsOff(name string) bool {
	for _, e := range hosts {
		for _, kv := range e.OffEnv {
			if strings.HasPrefix(kv, name+"=") {
				return true
			}
		}
	}
	return false
}

// redirects reports whether --pass-env may not pass the variable name.
func redirects(name string) bool {
	if slices.Contains(redirectNames, name) {
		return true
	}
	for _, p := range redirectPrefixes {
		if strings.HasPrefix(name, p) {
			return true
		}
	}
	for _, e := range hosts {
		if slices.Contains(e.ConfigEnv, name) {
			return true
		}
	}
	return false
}

// reachesHome reports whether value would point a chat back at the
// invoking home: when it holds one of homes (the home as given and with
// its links resolved) whole, as a path or the start of one, wherever it
// is in the value, so that a home whose name holds a space or a comma is
// found too; or when a part of it, split at the separators of lists and
// options, is a path in the home or one that holds it, or starts with "~".
// A part that is a relative path is read from chatDir, the folder the chat
// runs in, as "../../.." would be, and its links resolved as far as they
// exist, as --work's are.
func reachesHome(value string, homes []string, chatDir string) bool {
	for _, home := range homes {
		for rest := value; ; {
			i := strings.Index(rest, home)
			if i < 0 {
				break
			}
			rest = rest[i+len(home):]
			if rest == "" || !isNameByte(rest[0]) {
				return true
			}
		}
	}
	home := homes[len(homes)-1]
	parts := strings.FieldsFunc(value, func(r rune) bool {
		return r == filepath.ListSeparator || r == '=' || r == ',' || r == ';' || r == ' ' || r == '\t' || r == '\n'
	})
	for _, part := range parts {
		if strings.HasPrefix(part, "~") {
			return true
		}
		if !filepath.IsAbs(part) {
			part = filepath.Join(chatDir, part)
		}
		p := resolveOr(filepath.Clean(part))
		if within(p, home) || within(home, p) {
			return true
		}
	}
	return false
}

// isNameByte reports whether b can go on a file's name past its end, so
// that "/home/a" in "/home/ab" is no match: a letter, a digit, or one of
// "-", "_" and ".".
func isNameByte(b byte) bool {
	return b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' || b >= '0' && b <= '9' || b == '-' || b == '_' || b == '.'
}

// forbidMinimum is the fewest letters and digits a passed value has for
// the rows to be checked for it. The store matches a word by its letters
// and digits in any field, so a short value, such as a switch's 1, would
// be found in every commit; a key or a path is longer.
const forbidMinimum = 8

func lettersAndDigits(s string) int {
	n := 0
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			n++
		}
	}
	return n
}

// plan is a checked run.
type plan struct {
	opts   Options
	tasks  []task.Task
	host   adapter.Host
	entry  hostEntry
	arm    Arm
	cli    string // the CLI, links resolved
	evaMD  string
	work   string // links resolved
	forbid []string
	// passed are the variables passed whose values are among forbid.
	passed []passedVar
}

// passedVar is a variable --pass-env passes.
type passedVar struct{ name, value string }

// trialRoot is the folder of the trial n of task id under condition c, in
// the run's folder under work.
func trialRoot(work, run, id string, c task.Condition, n int) string {
	return filepath.Join(work, run, id, string(c), "t"+strconv.Itoa(n))
}

// checkPassed refuses a passed value that every row of the run would hold
// as soon as it is written: the host's id, the model asked for, the
// condition, a task's id, the commit and the rows' own names, as the
// store matches a forbidden word. It is checked before any trial starts,
// so that no quota is spent on rows the store would refuse. What only a
// trial gives, the host's version and the model it names, and a trial's
// number past the first, is checked by the store at the rows.
func (p *plan) checkPassed(commit string, dirty bool) error {
	one := 1
	o := task.Outcome{AskedAgain: &one, Memory: &task.Spend{}}
	base := results.Row{Commit: commit, Dirty: dirty, Host: string(p.host.ID), Model: p.opts.Model}
	for _, t := range p.tasks {
		rows, err := task.Rows(t, p.opts.Condition, 1, o, base)
		if err != nil {
			return err
		}
		for _, v := range p.passed {
			if results.Forbidden(rows, []string{v.value}) != nil {
				return usagef("--pass-env %s holds a value the run's rows would hold, which they may not", v.name)
			}
		}
	}
	return nil
}

// prepare checks the options and loads the tasks. It starts and writes
// nothing.
func prepare(o Options) (*plan, error) {
	p := &plan{opts: o}
	h, ok := adapter.ByID(o.Host)
	if !ok {
		return nil, usagef("--host is not a host id")
	}
	p.host, p.entry = h, hosts[h.ID]
	if p.entry.Argv == nil {
		return nil, usagef("--host has no entry in the host table")
	}
	arm, ok := arms[o.Condition]
	if !ok {
		return nil, usagef("--condition %s is not registered", o.Condition)
	}
	p.arm = arm
	switch {
	case o.Trials < 1:
		return nil, usagef("--trials is under 1")
	case o.MaxSessions < 1:
		return nil, usagef("--max-sessions is missing or under 1")
	case o.MaxTokens < 1:
		return nil, usagef("--max-tokens is missing or under 1")
	case o.SessionTimeout < 0:
		return nil, usagef("--session-timeout is negative")
	case o.Out == "":
		return nil, usagef("--out is missing")
	case o.Tasks == "" || o.Repos == "":
		return nil, usagef("--tasks and --repos are needed")
	case o.Model != "" && !idPattern.MatchString(o.Model):
		return nil, usagef("--model is not a model id")
	}
	var err error
	if p.cli, err = executable(o.CLI); err != nil {
		return nil, usagef("--cli: %v", err)
	}
	if o.EvaMD != "" {
		if p.evaMD, err = executable(o.EvaMD); err != nil {
			return nil, usagef("--eva-md: %v", err)
		}
	}
	homes, err := invokingHomes()
	if err != nil {
		return nil, err
	}
	if p.work, err = checkWork(o); err != nil {
		return nil, err
	}
	// The folder a trial's chat runs in, as deep under --work as any is; its
	// names stand for those the run gives it.
	chatDir := filepath.Join(trialRoot(p.work, "run", "task", o.Condition, 1), "workspace")
	for i, name := range o.PassEnv {
		switch {
		case !envName.MatchString(name):
			return nil, usagef("--pass-env %d is not a variable's name", i+1)
		case slices.Contains(builtEnv, name) || setsOff(name):
			return nil, usagef("--pass-env %s is one the driver sets itself", name)
		case redirects(name):
			return nil, usagef("--pass-env %s would point the chat at a configuration outside its trial", name)
		case slices.Contains(o.PassEnv[:i], name):
			return nil, usagef("--pass-env %s is given twice", name)
		}
		value, set := os.LookupEnv(name)
		if !set {
			return nil, usagef("--pass-env %s is not set", name)
		}
		if reachesHome(value, homes, chatDir) {
			return nil, usagef("--pass-env %s holds a path that reaches the invoking home", name)
		}
		if lettersAndDigits(value) >= forbidMinimum {
			p.forbid = append(p.forbid, value)
			p.passed = append(p.passed, passedVar{name, value})
		}
	}
	if p.tasks, err = task.LoadAll(o.Tasks); err != nil {
		return nil, usagef("%v", err)
	}
	for _, t := range p.tasks {
		p.forbid = append(p.forbid, task.Forbid(t)...)
	}
	return p, nil
}

// invokingHomes are the invoking home as given, cleaned, and then with
// its links resolved, the last.
func invokingHomes() ([]string, error) {
	home, err := os.UserHomeDir()
	if err != nil || !filepath.IsAbs(home) {
		return nil, usagef("the invoking home is not known, so the trials cannot be shown to be outside it")
	}
	real, err := resolve(home)
	if err != nil {
		return nil, usagef("the invoking home cannot be read")
	}
	return []string{filepath.Clean(home), real}, nil
}

// idPattern is a results id.
var idPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}$`)

// executable returns the absolute path p, links resolved, of a file that
// can be run.
func executable(p string) (string, error) {
	if !filepath.IsAbs(p) {
		return "", errors.New("not an absolute path")
	}
	real, err := filepath.EvalSymlinks(p)
	if err != nil {
		return "", errors.New("no such file")
	}
	info, err := os.Stat(real)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0o111 == 0 {
		return "", errors.New("not a file that can be run")
	}
	return real, nil
}

// Run checks the options, prints the plan to out, and, given leave to
// spend model quota, runs every trial within the ceilings and appends the
// rows of each judged one to the results folder. A *UsageError, or
// ErrNoQuota, says nothing started; a *Stopped says a trial's code stopped
// the run, after its summary was printed. out gets ids and figures only.
func Run(ctx context.Context, o Options, out io.Writer) (Summary, error) {
	if o.Clock == nil {
		o.Clock = time.Now
	}
	if o.SessionTimeout == 0 {
		o.SessionTimeout = DefaultSessionTimeout
	}
	p, err := prepare(o)
	if err != nil {
		return Summary{}, err
	}
	s := Summary{Unjudged: map[string]int{}}
	// The plan's sessions, counted only when the count is an int: a count
	// past that is over any ceiling.
	sessions := "over " + strconv.Itoa(math.MaxInt)
	countable := len(p.tasks) == 0 || o.Trials <= math.MaxInt/len(p.tasks)
	if countable {
		s.Planned = len(p.tasks) * o.Trials
		sessions = strconv.Itoa(s.Planned)
	}
	fmt.Fprintf(out, "plan: condition %s, host %s, tasks %d, trials %d each, sessions %s; ceilings: sessions %d, tokens %d\n",
		o.Condition, p.host.ID, len(p.tasks), o.Trials, sessions, o.MaxSessions, o.MaxTokens)
	if !countable || int64(s.Planned) > o.MaxSessions {
		return s, usagef("the plan's %s sessions are over --max-sessions %d", sessions, o.MaxSessions)
	}
	if !o.Spend {
		return s, ErrNoQuota
	}
	if p.arm.Check != nil {
		if err := p.arm.Check(o); err != nil {
			return s, fmt.Errorf("the condition %s: %w", o.Condition, err)
		}
	}
	repo := o.Repo
	if repo == "" {
		repo = "."
	}
	commit, dirty, err := results.Source(ctx, repo)
	if err != nil {
		return s, err
	}
	if err := p.checkPassed(commit, dirty); err != nil {
		return s, err
	}
	if s.Run, err = newRun(p.work, o.Clock()); err != nil {
		return s, err
	}
	fmt.Fprintf(out, "run %s\n", s.Run)
	store := results.Store{Dir: o.Out, Forbid: p.forbid}
	done := 0
	var stopped error
trials:
	for _, t := range p.tasks {
		for n := 1; n <= o.Trials; n++ {
			if s.Tokens >= o.MaxTokens {
				// The rest are counted, in one line, not listed.
				s.NotRun = s.Planned - done
				fmt.Fprintf(out, "trial %s.t%d and the %d after it: not run, --max-tokens reached\n", t.ID, n, s.NotRun-1)
				break trials
			}
			done++
			r, err := p.trial(ctx, s.Run, t, n)
			s.Tokens += r.tokens
			if err != nil {
				return s, fmt.Errorf("trial %s.t%d: %w", t.ID, n, err)
			}
			if r.unjudged != "" {
				s.Unjudged[r.unjudged]++
				fmt.Fprintf(out, "trial %s.t%d: unjudged, %s\n", t.ID, n, r.unjudged)
				if slices.Contains(stopCodes, r.unjudged) {
					s.NotRun = s.Planned - done
					stopped = &Stopped{Code: r.unjudged}
					break trials
				}
				continue
			}
			base := results.Row{Commit: commit, Dirty: dirty, Host: string(p.host.ID), HostVersion: r.version, Model: r.model, Time: o.Clock()}
			rows, err := task.Rows(t, o.Condition, n, r.outcome, base)
			if err == nil {
				err = store.Append(rows...)
			}
			if err != nil {
				return s, fmt.Errorf("trial %s.t%d: %w", t.ID, n, err)
			}
			s.Judged++
			if r.model == "" {
				s.NoModel++
			}
			s.Rows += len(rows)
			fmt.Fprintf(out, "trial %s.t%d: solved %d, first try %d, corrections %d, rounds %d\n",
				t.ID, n, b2i(r.outcome.Solved), b2i(r.outcome.FirstTry), r.outcome.Corrections, r.outcome.Rounds)
		}
	}
	var unjudged []string
	for _, c := range Codes {
		unjudged = append(unjudged, fmt.Sprintf("%s %d", c, s.Unjudged[c]))
	}
	fmt.Fprintf(out, "run %s: planned %d, judged %d, no model %d, not run %d, unjudged %s; tokens %d, rows %d\n",
		s.Run, s.Planned, s.Judged, s.NoModel, s.NotRun, strings.Join(unjudged, ", "), s.Tokens, s.Rows)
	return s, stopped
}

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}

// newRun makes the run's folder under work and returns its id, made from
// the time it starts and, when a run started in the same second, a count.
func newRun(work string, now time.Time) (string, error) {
	if err := os.MkdirAll(work, 0o755); err != nil {
		return "", fmt.Errorf("the work folder: %w", pathless(err))
	}
	stamp := strings.ToLower(now.UTC().Format("20060102T150405Z"))
	for i := 1; i < 1000; i++ {
		id := stamp
		if i > 1 {
			id = fmt.Sprintf("%s-%d", stamp, i)
		}
		err := os.Mkdir(filepath.Join(work, id), 0o755)
		if err == nil {
			return id, nil
		}
		if !errors.Is(err, os.ErrExist) {
			return "", fmt.Errorf("the run's folder: %w", pathless(err))
		}
	}
	return "", errors.New("the run's folder: too many runs in one second")
}

// pathless returns err without the path an *os.PathError or *os.LinkError
// carries.
func pathless(err error) error {
	var pe *os.PathError
	if errors.As(err, &pe) {
		return fmt.Errorf("%s: %w", pe.Op, pe.Err)
	}
	var le *os.LinkError
	if errors.As(err, &le) {
		return fmt.Errorf("%s: %w", le.Op, le.Err)
	}
	return err
}
