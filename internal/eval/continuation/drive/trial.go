package drive

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/heyNag/eva-md/internal/eval/continuation/task"
	"github.com/heyNag/eva-md/internal/host/adapter"
)

// Time limits of the processes a trial starts besides its chats: reading
// the CLI's version, and the wait for a killed process's output. They are
// exported so that the tests of this package and of the packages that use
// it, such as a condition's, can raise them once; nothing else changes
// them.
var (
	VersionTimeout = 30 * time.Second
	WaitDelay      = 10 * time.Second
)

// outputLimit is the most of what one call prints on stdout that is kept.
const outputLimit = 256 << 20

// gitconfig is the trial home's git configuration: a made-up identity,
// commits unsigned, and no background maintenance, since setup and the
// agent may commit.
const gitconfig = "[user]\n\tname = Continuation Trial\n\temail = trial@example.invalid\n" +
	"[commit]\n\tgpgsign = false\n[maintenance]\n\tauto = false\n"

// trialResult is what one trial gave.
type trialResult struct {
	unjudged string // a code, or "" for a judged trial
	outcome  task.Outcome
	version  string
	model    string
	tokens   int64 // what the ceiling counts, judged or not
	// stopped says the condition stopped the run before the trial started
	// (ErrStopRun): it is counted not run.
	stopped bool
}

// trial runs the trial n of task t. When it ends without After once the
// condition's Prepare has run, and Prepare gave no *Unjudged that carries
// its cost, the condition's Spent says what it spent, which counts toward
// the ceiling and in the run's record as After's cost does: an update
// that passed, before a chat that was then left unjudged, among it.
func (p *plan) trial(ctx context.Context, run string, t task.Task, n int) (trialResult, error) {
	var owed *Trial
	res, err := p.runTrial(ctx, run, t, n, &owed)
	if owed != nil && p.arm.Spent != nil {
		if cost := p.arm.Spent(ctx, *owed); cost.Tokens > 0 {
			res.tokens += cost.Tokens
		}
	}
	return res, err
}

// runTrial is trial's work. It sets *owed to the trial once the
// condition's Prepare has run, or would have, and back to nil when what
// the condition spent is counted otherwise: by Prepare's *Unjudged, by
// After, or not at all, since ErrStopRun ran nothing.
func (p *plan) runTrial(ctx context.Context, run string, t task.Task, n int, owed **Trial) (trialResult, error) {
	tr := Trial{Task: t, Condition: p.opts.Condition, Number: n, Host: p.host, Options: p.opts}
	root := trialRoot(p.work, run, t.ID, p.opts.Condition, n)
	tr.Dirs = Dirs{Root: root, Home: filepath.Join(root, "home"), Tmp: filepath.Join(root, "tmp"),
		Bin: filepath.Join(root, "bin"), Workspace: filepath.Join(root, "workspace"), Grade: filepath.Join(root, "grade")}
	if err := os.MkdirAll(filepath.Dir(root), 0o755); err != nil {
		return trialResult{}, pathless(err)
	}
	if err := os.Mkdir(root, 0o755); err != nil {
		return trialResult{}, pathless(err)
	}
	for _, dir := range []string{tr.Dirs.Home, tr.Dirs.Tmp, tr.Dirs.Bin} {
		if err := os.Mkdir(dir, 0o755); err != nil {
			return trialResult{}, pathless(err)
		}
	}
	// The grading's copies, which hold the hidden tests, are the driver's
	// user's alone: a chat run as another user cannot read or write there.
	if err := os.Mkdir(tr.Dirs.Grade, 0o700); err != nil {
		return trialResult{}, pathless(err)
	}
	if err := os.WriteFile(filepath.Join(tr.Dirs.Home, ".gitconfig"), []byte(gitconfig), 0o644); err != nil {
		return trialResult{}, pathless(err)
	}
	// A go command the chat runs keeps no counters in its home, and leaves
	// no process writing there once the trial's folder is to be removed.
	if err := task.GoTelemetryOff(tr.Dirs.Home); err != nil {
		return trialResult{}, pathless(err)
	}
	if err := os.Symlink(p.cli, filepath.Join(tr.Dirs.Bin, p.host.Names.CLI)); err != nil {
		return trialResult{}, pathless(err)
	}
	if p.evaMD != "" {
		if err := os.Symlink(p.evaMD, filepath.Join(tr.Dirs.Bin, "eva-md")); err != nil {
			return trialResult{}, pathless(err)
		}
	}
	if err := task.Prepare(ctx, t, p.opts.Repos, tr.Dirs.Workspace); err != nil {
		return trialResult{}, err
	}
	// A hidden file's bytes within the chat's reach before any chat is the
	// task's fault, or that of code an earlier trial's grading ran, and
	// stops the run.
	hidden, err := loadHidden(t, tr.Dirs.Workspace)
	if err != nil {
		return trialResult{}, err
	}
	if in, err := hidden.found(p.work); err != nil {
		return trialResult{}, err
	} else if in {
		return trialResult{unjudged: CodeHiddenFound}, nil
	}
	tr.Env = []string{
		"HOME=" + tr.Dirs.Home,
		"PATH=" + tr.Dirs.Bin + string(filepath.ListSeparator) + os.Getenv("PATH"),
		"TMPDIR=" + tr.Dirs.Tmp,
		"GIT_CONFIG_NOSYSTEM=1",
	}
	for _, name := range p.opts.PassEnv {
		tr.Env = append(tr.Env, name+"="+os.Getenv(name))
	}
	tr.Env = append(tr.Env, p.entry.OffEnv...)
	calls, err := p.newCallTrace(run, t, n)
	if err != nil {
		return trialResult{}, err
	}
	// A trace that cannot be written stops the run, before its next call.
	var traceErr error
	link := filepath.Join(tr.Dirs.Bin, p.host.Names.CLI)
	probe := adapter.Probe(ctx, p.host.ID, adapter.Env{
		LookPath: func(file string) (string, error) {
			if file != p.host.Names.CLI {
				return "", exec.ErrNotFound
			}
			return link, nil
		},
		// The probe's own limit, 5 seconds, is not used: the version is read
		// within VersionTimeout, in the trial's environment.
		Run: func(_ context.Context, cli string, args ...string) ([]byte, error) {
			files, err := calls.start(traceVersion)
			if err != nil {
				traceErr = err
				return nil, err
			}
			vctx, cancel := context.WithTimeout(ctx, VersionTimeout)
			defer cancel()
			c, err := runCall(vctx, cli, args, tr.Env, tr.Dirs.Home, 64<<10, files)
			if p.opts.ended != nil {
				p.opts.ended(callEnd{Call: traceVersion, Pid: c.pid, Err: err, Over: c.over})
			}
			if c.traceErr != nil {
				traceErr = traceFault(c.traceErr)
				return nil, traceErr
			}
			if err != nil || c.over {
				return nil, errors.New("the host's version is unavailable")
			}
			return c.out, nil
		},
	})
	if traceErr != nil {
		return trialResult{}, traceErr
	}
	if probe.Version == "" {
		return trialResult{}, errors.New("the host's CLI gave no version")
	}
	tr.CLI = probe.CLI
	res := trialResult{version: probe.Version}
	// The flags that turn the host's web tools off, for every call of the
	// chat: an entry that must ask the CLI first does so here, in the
	// trial's environment, within VersionTimeout.
	web, err := p.entry.WebOff(func(args ...string) ([]byte, error) {
		files, err := calls.start(traceTools)
		if err != nil {
			traceErr = err
			return nil, err
		}
		lctx, cancel := context.WithTimeout(ctx, VersionTimeout)
		defer cancel()
		c, err := runCall(lctx, tr.CLI, args, tr.Env, tr.Dirs.Home, 1<<20, files)
		if p.opts.ended != nil {
			p.opts.ended(callEnd{Call: traceTools, Pid: c.pid, Err: err, Over: c.over})
		}
		if c.traceErr != nil {
			traceErr = traceFault(c.traceErr)
			return nil, traceErr
		}
		if err != nil || c.over {
			return nil, errors.New("the host's CLI did not answer")
		}
		return c.out, nil
	})
	if traceErr != nil {
		return res, traceErr
	}
	if err != nil {
		return res, err
	}
	if len(web) == 0 {
		return res, errors.New("the host table turns no web tool off")
	}

	var launch Launch
	*owed = &tr
	if p.arm.Prepare != nil {
		l, err := p.arm.Prepare(ctx, tr)
		if errors.Is(err, ErrStopRun) {
			*owed = nil
			res.stopped = true
			return res, nil
		}
		// What the condition spent before it left the trial unjudged counts
		// toward the ceiling, as what After returns does.
		var u *Unjudged
		if errors.As(err, &u) {
			*owed = nil
			if u.Cost.Tokens > 0 {
				res.tokens += u.Cost.Tokens
			}
		}
		if code, err := unjudgedCode(err); code != "" || err != nil {
			res.unjudged = code
			return res, err
		}
		launch = l
	}
	env := slices.Clone(tr.Env)
	for _, kv := range launch.Env {
		name, _, ok := strings.Cut(kv, "=")
		if !ok || !envName.MatchString(name) || slices.ContainsFunc(env, func(e string) bool { return strings.HasPrefix(e, name+"=") }) {
			return res, errors.New("the condition adds a variable that is not NAME=value, or one the chat already has")
		}
		env = append(env, kv)
	}
	extra := slices.Clone(p.entry.Off)
	if launch.Plugin {
		load, err := p.entry.Plugin(tr.Dirs.Home, p.host)
		if err == nil {
			err = writeFiles(tr.Dirs.Home, load.Files)
		}
		if err != nil {
			res.unjudged = CodeSetupFailed
			return res, nil
		}
		extra = load.Args
	}
	extra = append(extra, launch.Args...)
	// What the condition wrote into the trial, its plugin's files among
	// them, is checked too, before any chat can read it.
	if in, err := hidden.found(p.work); err != nil {
		return res, err
	} else if in {
		res.unjudged = CodeHiddenFound
		return res, nil
	}

	s := session{p: p, tr: tr, env: env, extra: extra, web: web, hidden: hidden, calls: calls}
	code, err := s.run(ctx)
	res.tokens += s.counted
	res.outcome, res.model = s.outcome, s.model
	if ctx.Err() != nil {
		return res, ctx.Err()
	}
	if code != "" || err != nil {
		res.unjudged = code
		return res, err
	}
	if p.arm.After != nil {
		*owed = nil
		cost, err := p.arm.After(ctx, tr)
		res.tokens += cost.Tokens
		if code, err := unjudgedCode(err); code != "" || err != nil {
			res.unjudged = code
			return res, err
		}
		res.outcome.Memory = &task.Spend{Tokens: cost.Tokens, Ms: cost.Ms}
	}
	return res, nil
}

// unjudgedCode splits a condition's error: an *Unjudged with a code of the
// list gives its code, and any other error is returned as it is.
func unjudgedCode(err error) (string, error) {
	var u *Unjudged
	switch {
	case err == nil:
		return "", nil
	case errors.As(err, &u) && slices.Contains(Codes, u.Code):
		return u.Code, nil
	case errors.As(err, &u):
		return "", errors.New("the condition left the trial unjudged with a code not on the list")
	}
	return "", err
}

// writeFiles writes files under home, by their paths relative to it.
func writeFiles(home string, files map[string][]byte) error {
	for _, rel := range sortedKeys(files) {
		if !filepath.IsLocal(rel) {
			return errors.New("a file outside the home")
		}
		path := filepath.Join(home, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(path, files[rel], 0o644); err != nil {
			return err
		}
	}
	return nil
}

// session is a trial's chat: its first prompt and the rounds that follow.
type session struct {
	p       *plan
	tr      Trial
	env     []string
	extra   []string
	web     []string // hostEntry.WebOff's flags, which every call has
	outcome task.Outcome
	model   string
	// spent is the time the session's calls took, which --session-timeout
	// bounds; grading is not counted.
	spent time.Duration
	// counted are the tokens --max-tokens counts of the session's calls
	// (report.Spent).
	counted int64
	// hidden are the task's hidden files, which the session looks for in
	// the trial's folder after each call and each grading a prompt follows.
	hidden *hidden
	// calls keeps what each call printed, when the run keeps a trace.
	calls *callTrace
}

// reached reports whether a hidden file is within the chat's reach: in the
// work folder, which holds the trial's (see hidden.found).
func (s *session) reached() (bool, error) {
	return s.hidden.found(s.p.work)
}

// gradingFolder makes scratch, the folder of a grading's copy, in the
// trial's grading folder, for the driver's user alone. It reports false,
// making nothing, when the grading folder is no longer the real folder the
// trial made, or something already stands at scratch, such as a link a
// chat run as the driver's user planted: the copy, which holds the hidden
// tests, would be written where it points, out of the check's sight. That
// is a hidden file put within a chat's reach.
func (s *session) gradingFolder(scratch string) (bool, error) {
	if info, err := os.Lstat(s.tr.Dirs.Grade); err != nil || !info.IsDir() {
		return false, nil
	}
	err := os.Mkdir(scratch, 0o700)
	if errors.Is(err, fs.ErrExist) {
		return false, nil
	}
	return err == nil, pathless(err)
}

// errTimeout is a call's error when the session's time limit ended it.
var errTimeout = errors.New("the session's time limit was reached")

// run runs the chat until every hidden test passes or the task's rounds
// are spent. It returns a code when the trial is unjudged, CodeTimeout
// among them when the session's calls ran past its time limit. Grading
// runs under ctx, the run's, each hidden test within its own limit.
func (s *session) run(ctx context.Context) (string, error) {
	t := s.tr.Task
	prompt, err := task.FirstPrompt(t, s.tr.Condition)
	if err != nil {
		return "", err
	}
	supplied := map[string]bool{}
	var asked int
	id := ""
	for round := 1; ; round++ {
		r, err := s.call(ctx, chat{Prompt: prompt, Turns: t.MaxTurns, Model: s.p.opts.Model, Resume: id, Extra: s.extra, Web: s.web})
		s.outcome.AgentEvents += r.AgentEvents
		s.outcome.PromptTokens += r.PromptTokens
		s.outcome.CachedTokens += r.CachedTokens
		s.outcome.OutputTokens += r.OutputTokens
		s.counted += r.Spent
		// Whatever the call gave, a hidden file it left within the chat's
		// reach is a fault of the set-up.
		if ctx.Err() == nil {
			if in, err := s.reached(); err != nil {
				return "", err
			} else if in {
				return CodeHiddenFound, nil
			}
		}
		if errors.Is(err, errTimeout) {
			return CodeTimeout, nil
		}
		if err != nil {
			return "", err
		}
		s.outcome.Rounds = round
		switch {
		case r.Fail != "":
			return r.Fail, nil
		case s.p.opts.Model != "" && r.Model != "" && r.Model != s.p.opts.Model:
			// A session that names another model is no measure of the one
			// asked for; one that names none is judged, and its rows carry
			// no model.
			return CodeOtherModel, nil
		}
		if idPattern.MatchString(r.Model) {
			s.model = r.Model
		}
		if r.Session != "" {
			id = r.Session
		}
		// The grading's copy holds the hidden tests, and the chat can reach
		// the trial's folder, so it is removed before any next prompt.
		scratch := filepath.Join(s.tr.Dirs.Grade, "r"+strconv.Itoa(round))
		if made, err := s.gradingFolder(scratch); err != nil {
			return "", err
		} else if !made {
			return CodeHiddenFound, nil
		}
		g, err := task.Grade(ctx, t, t.Dir, s.tr.Dirs.Workspace, scratch)
		if rerr := s.p.remove(ctx, scratch); err == nil && rerr != nil {
			err = fmt.Errorf("removing the grading's copy: %w", rerr)
		}
		if err != nil {
			return "", err
		}
		if g.Solved() {
			s.outcome.Solved = true
			s.outcome.FirstTry = round == 1
			break
		}
		if round >= t.MaxRounds {
			break
		}
		if id == "" {
			// The follow-up the card gives this trial cannot be sent to the
			// same session: the trial is no measure of the condition.
			return CodeHostFailed, nil
		}
		// The code the grading ran may have left a hidden file within the
		// reach of the prompt that follows.
		if in, err := s.reached(); err != nil {
			return "", err
		} else if in {
			return CodeHiddenFound, nil
		}
		var failing []string
		var facts []task.Fact
		for _, f := range g.Fail {
			failing = append(failing, f.Test)
			for _, id := range f.Facts {
				if !supplied[id] {
					supplied[id] = true
					facts = append(facts, factByID(t, id))
				}
			}
		}
		for _, f := range facts {
			if s.p.opts.Delivered == nil {
				continue
			}
			given, err := s.p.opts.Delivered(s.tr, f.ID)
			if err != nil {
				return "", err
			}
			if given {
				asked++
			}
		}
		s.outcome.Corrections += len(facts)
		prompt = followUp(failing, facts)
	}
	if s.p.opts.Delivered != nil {
		s.outcome.AskedAgain = &asked
	}
	return "", nil
}

func factByID(t task.Task, id string) task.Fact {
	for _, f := range t.Facts {
		if f.ID == id {
			return f
		}
	}
	return task.Fact{ID: id}
}

// followUp is the prompt of a round after the first, in a fixed form: the
// failing tests by id, then the statement of each fact they encode that no
// earlier round supplied.
func followUp(failing []string, facts []task.Fact) string {
	var b strings.Builder
	b.WriteString("These hidden tests still fail: " + strings.Join(failing, ", ") + ".")
	if len(facts) > 0 {
		b.WriteString("\n\nWhat they check:")
		for _, f := range facts {
			b.WriteString("\n- " + f.Statement)
		}
	}
	b.WriteString("\n\nPlease make them pass.")
	return b.String()
}

// call runs one headless call of the chat in the workspace, within what
// is left of the session's time limit, and reads its output. The time it
// took is added to the outcome. A call ended by the limit gives
// errTimeout, with the tokens its output named before the limit. A call
// that gives no answer leaves the trial unjudged, and its report says why
// in Fail: sign-in and rate limit when the host says so, and
// CodeHostFailed for any other, a CLI a signal ended among them, even one
// that had printed an answer, since its work may be cut short, or an
// output that holds no answer (a failure of the host's service, an exit
// that printed nothing, an output with no final event). An answer without
// its usage, or with 0 prompt and output tokens, whatever ended the CLI
// after it, and an output past its limit, which may hide the usage, are
// CodeNoUsage: what the call spent no ceiling can count. An output that
// names a web tool's call is CodeWebUsed before all of these, and also
// when the session's limit ended the call. The tokens the output names
// are kept, for the ceiling. A call that exits other than 0
// with an answer, as at the turn bound, is graded as it stands. An error
// is the driver's own failure to run the CLI, which stops the run, or to
// keep the call's trace, which stops it too, the call's tokens counted.
func (s *session) call(ctx context.Context, c chat) (report, error) {
	left := s.p.opts.SessionTimeout - s.spent
	if left <= 0 {
		return report{}, errTimeout
	}
	limit := s.p.opts.sessionLimit
	if limit == nil {
		limit = context.WithTimeout
	}
	files, err := s.calls.start(traceChat)
	if err != nil {
		return report{}, err
	}
	cctx, cancel := limit(ctx, left)
	defer cancel()
	start, began := s.p.opts.Clock(), time.Now()
	most := s.p.opts.outputLimit
	if most <= 0 {
		most = outputLimit
	}
	got, err := runCall(cctx, s.tr.CLI, s.p.entry.Argv(c), s.env, s.tr.Dirs.Workspace, most, files)
	out, over := got.out, got.over
	s.spent += time.Since(began)
	s.outcome.WallMs += s.p.opts.Clock().Sub(start).Milliseconds()
	switch {
	case ctx.Err() != nil:
		return spentOnly(s.p.entry.Read(out)), ctx.Err()
	case got.traceErr != nil:
		// The call's tokens are counted; the run stops before its next.
		return spentOnly(s.p.entry.Read(out)), traceFault(got.traceErr)
	case cctx.Err() != nil:
		// What the call printed before the limit ended it is read for the
		// tokens it names, which the ceiling counts, and for a web tool's
		// call, which stops the run however the call ended.
		r := s.p.entry.Read(out)
		if r.Web {
			cut := spentOnly(r)
			cut.Fail = CodeWebUsed
			return cut, nil
		}
		return spentOnly(r), errTimeout
	}
	var exit *exec.ExitError
	if err != nil && !errors.As(err, &exit) {
		return report{}, fmt.Errorf("the host's CLI: %w", pathless(err))
	}
	r := s.p.entry.Read(out)
	switch {
	case r.Web:
		// The chat had a web tool it should not have had, whatever else
		// the call gave: the set-up is wrong.
		r.Fail = CodeWebUsed
	case over:
		r.Fail = CodeNoUsage
	case r.Fail != "":
	case r.Answered && (!r.Usage || r.Spent == 0):
		// Whatever ended the CLI afterwards, a signal among them: an answer
		// spent what no ceiling can count.
		r.Fail = CodeNoUsage
	case exit != nil && signaled(exit):
		r.Fail = CodeHostFailed
	case !r.Answered:
		r.Fail = CodeHostFailed
	}
	if s.p.opts.ended != nil {
		s.p.opts.ended(callEnd{Call: traceChat, Pid: got.pid, Err: err, Over: over, Report: r})
	}
	return r, nil
}

// signaled reports whether a signal ended the process.
func signaled(exit *exec.ExitError) bool {
	ws, ok := exit.Sys().(syscall.WaitStatus)
	return ok && ws.Signaled()
}

// spentOnly is r's tokens alone: what a call that the session's limit or
// the run's end cut short named, which the ceiling counts, though nothing
// of it is graded.
func spentOnly(r report) report {
	return report{PromptTokens: r.PromptTokens, CachedTokens: r.CachedTokens, OutputTokens: r.OutputTokens, Spent: r.Spent}
}

// cappedBuffer keeps the first limit bytes written to it, unless it drops
// them, and says whether more came. Given a file, it writes the bytes it
// keeps there too, as they come, and keeps the first error that gives.
type cappedBuffer struct {
	buffer bytes.Buffer
	limit  int
	n      int // the bytes kept
	over   bool
	drop   bool
	file   io.Writer
	ferr   error
}

func (b *cappedBuffer) Write(p []byte) (int, error) {
	n := len(p)
	if room := b.limit - b.n; n > room {
		p = p[:max(room, 0)]
		b.over = true
	}
	b.n += len(p)
	if !b.drop {
		_, _ = b.buffer.Write(p)
	}
	if b.file != nil && b.ferr == nil && len(p) > 0 {
		if _, err := b.file.Write(p); err != nil {
			b.ferr = err
		}
	}
	return n, nil
}

// runGroup runs cli in dir with exactly the environment env, in a process
// group of its own that is killed when ctx ends and as soon as cli has
// exited, so that nothing it left running goes on editing the workspace
// while it is graded, and gives the first limit bytes the group printed on
// stdout, and whether it printed more. Its stderr is dropped. Its stdout
// is a pipe of runGroup's own, so that the wait ends when cli exits, not
// when the last process holding the pipe does; one that left the group and
// still holds it after WaitDelay is an error.
func runGroup(ctx context.Context, cli string, args, env []string, dir string, limit int) ([]byte, bool, error) {
	c, err := runCall(ctx, cli, args, env, dir, limit, nil)
	return c.out, c.over, err
}

// callOutput is what runCall read of a call.
type callOutput struct {
	out  []byte // the first limit bytes of stdout
	over bool   // stdout ran past them
	pid  int    // the process the call ran as; 0 when it did not start
	// traceErr is the first error writing or closing the call's trace.
	traceErr error
}

// runCall is runGroup that, given files, keeps in them what the call
// printed as it is read: the first limit bytes of stdout, those runGroup
// gives, in one, and as many of stderr, then also a pipe of its own, in
// the other, and once the wait has ended, how it ended, in the third
// (exitLine). It closes the files, and a failure to write or close them is
// the result's traceErr; the call runs to its end all the same.
func runCall(ctx context.Context, cli string, args, env []string, dir string, limit int, files *callFiles) (callOutput, error) {
	r, w, err := os.Pipe()
	if err != nil {
		closeFiles(files)
		return callOutput{}, pathless(err)
	}
	stdout := &cappedBuffer{limit: limit}
	readers, writers, sinks := []*os.File{r}, []*os.File{w}, []*cappedBuffer{stdout}
	if files != nil {
		stdout.file = files.stdout
		er, ew, err := os.Pipe()
		if err != nil {
			_, _ = r.Close(), w.Close()
			closeFiles(files)
			return callOutput{}, pathless(err)
		}
		readers, writers = append(readers, er), append(writers, ew)
		sinks = append(sinks, &cappedBuffer{limit: limit, drop: true, file: files.stderr})
	}
	cmd := exec.CommandContext(ctx, cli, args...)
	cmd.Dir = dir
	cmd.Env = env
	cmd.Stdout = w
	if files != nil {
		cmd.Stderr = writers[1]
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return killGroup(cmd.Process.Pid) }
	cmd.WaitDelay = WaitDelay
	copied := make(chan struct{}, len(readers))
	for i := range readers {
		go func() {
			_, _ = io.Copy(sinks[i], readers[i])
			copied <- struct{}{}
		}()
	}
	err = cmd.Start()
	for _, w := range writers {
		_ = w.Close()
	}
	if err == nil {
		err = cmd.Wait()
		_ = killGroup(cmd.Process.Pid)
	}
	// Whether the call's context had ended once its wait did: the driver
	// killed its group, at its time limit or at the run's end.
	ended := ctx.Err() != nil
	timer := time.NewTimer(WaitDelay)
	defer timer.Stop()
	held := false
	for done := 0; done < len(readers); {
		select {
		case <-copied:
			done++
		case <-timer.C:
			held = true
			for _, r := range readers {
				_ = r.Close()
			}
			for ; done < len(readers); done++ {
				<-copied
			}
			if err == nil {
				err = errors.New("a process the host's CLI started still holds its output")
			}
		}
	}
	for _, r := range readers {
		_ = r.Close()
	}
	c := callOutput{out: stdout.buffer.Bytes(), over: stdout.over}
	if cmd.Process != nil {
		c.pid = cmd.Process.Pid
	}
	if files != nil {
		_, xerr := io.WriteString(files.exit, exitLine(cmd.ProcessState, ended, held))
		c.traceErr = errors.Join(sinks[0].ferr, sinks[1].ferr, xerr)
		if cerr := closeFiles(files); c.traceErr == nil {
			c.traceErr = cerr
		}
	}
	return c, err
}

// closeFiles closes a call's trace files, when it has them, and gives the
// first error.
func closeFiles(files *callFiles) error {
	if files == nil {
		return nil
	}
	return errors.Join(files.stdout.Close(), files.stderr.Close(), files.exit.Close())
}

// killGroup kills the process group pid leads.
func killGroup(pid int) error {
	err := syscall.Kill(-pid, syscall.SIGKILL)
	if errors.Is(err, syscall.ESRCH) {
		return os.ErrProcessDone
	}
	return err
}
