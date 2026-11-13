package drive

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
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
// the CLI's version, and the wait for a killed process's output. Tests
// raise them.
var (
	versionTimeout = 30 * time.Second
	waitDelay      = 10 * time.Second
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
}

// trial runs the trial n of task t.
func (p *plan) trial(ctx context.Context, run string, t task.Task, n int) (trialResult, error) {
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
	for _, dir := range []string{tr.Dirs.Home, tr.Dirs.Tmp, tr.Dirs.Bin, tr.Dirs.Grade} {
		if err := os.Mkdir(dir, 0o755); err != nil {
			return trialResult{}, pathless(err)
		}
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
	// A hidden file's bytes in the trial's folder before any chat is the
	// task's fault, and stops the run.
	hidden, err := loadHidden(t, tr.Dirs.Workspace)
	if err != nil {
		return trialResult{}, err
	}
	if in, err := hidden.found(root, tr.Dirs.Grade); err != nil {
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
	link := filepath.Join(tr.Dirs.Bin, p.host.Names.CLI)
	probe := adapter.Probe(ctx, p.host.ID, adapter.Env{
		LookPath: func(file string) (string, error) {
			if file != p.host.Names.CLI {
				return "", exec.ErrNotFound
			}
			return link, nil
		},
		// The probe's own limit, 5 seconds, is not used: the version is read
		// within versionTimeout, in the trial's environment.
		Run: func(_ context.Context, cli string, args ...string) ([]byte, error) {
			vctx, cancel := context.WithTimeout(ctx, versionTimeout)
			defer cancel()
			out, over, err := runGroup(vctx, cli, args, tr.Env, tr.Dirs.Home, 64<<10)
			if err != nil || over {
				return nil, errors.New("the host's version is unavailable")
			}
			return out, nil
		},
	})
	if probe.Version == "" {
		return trialResult{}, errors.New("the host's CLI gave no version")
	}
	tr.CLI = probe.CLI
	res := trialResult{version: probe.Version}

	var launch Launch
	if p.arm.Prepare != nil {
		l, err := p.arm.Prepare(ctx, tr)
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

	s := session{p: p, tr: tr, env: env, extra: extra, hidden: hidden}
	code, err := s.run(ctx)
	res.tokens = s.outcome.PromptTokens + s.outcome.OutputTokens
	res.outcome, res.model = s.outcome, s.model
	if ctx.Err() != nil {
		return res, ctx.Err()
	}
	if code != "" || err != nil {
		res.unjudged = code
		return res, err
	}
	if p.arm.After != nil {
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
	outcome task.Outcome
	model   string
	// spent is the time the session's calls took, which --session-timeout
	// bounds; grading is not counted.
	spent time.Duration
	// hidden are the task's hidden files, which the session looks for in
	// the trial's folder after each call and each grading a prompt follows.
	hidden *hidden
}

// reached reports whether a hidden file is within the chat's reach: in the
// trial's folder, but the grading's.
func (s *session) reached() (bool, error) {
	return s.hidden.found(s.tr.Dirs.Root, s.tr.Dirs.Grade)
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
		r, err := s.call(ctx, chat{Prompt: prompt, Turns: t.MaxTurns, Model: s.p.opts.Model, Resume: id, Extra: s.extra})
		s.outcome.AgentEvents += r.AgentEvents
		s.outcome.PromptTokens += r.PromptTokens
		s.outcome.CachedTokens += r.CachedTokens
		s.outcome.OutputTokens += r.OutputTokens
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
// errTimeout. A call that gives no answer leaves the trial unjudged, and
// its report says why in Fail: sign-in and rate limit when the host says
// so, and CodeHostFailed for any other, a CLI a signal ended among them,
// even one that had printed an answer, since its work may be cut short,
// or an output that holds no answer (a failure of the host's service, an
// exit that printed nothing, an output with no final event). An answer
// without its usage, or with 0 prompt and output tokens, and an output
// past its limit, which may hide the usage, are CodeNoUsage: what the call
// spent no ceiling can count. The tokens the output names are kept, for
// the ceiling. A call that exits other than 0 with an answer, as at the
// turn bound, is graded as it stands. An error is the driver's own failure
// to run the CLI, which stops the run.
func (s *session) call(ctx context.Context, c chat) (report, error) {
	left := s.p.opts.SessionTimeout - s.spent
	if left <= 0 {
		return report{}, errTimeout
	}
	limit := s.p.opts.sessionLimit
	if limit == nil {
		limit = context.WithTimeout
	}
	cctx, cancel := limit(ctx, left)
	defer cancel()
	start, began := s.p.opts.Clock(), time.Now()
	most := s.p.opts.outputLimit
	if most <= 0 {
		most = outputLimit
	}
	out, over, err := runGroup(cctx, s.tr.CLI, s.p.entry.Argv(c), s.env, s.tr.Dirs.Workspace, most)
	s.spent += time.Since(began)
	s.outcome.WallMs += s.p.opts.Clock().Sub(start).Milliseconds()
	switch {
	case ctx.Err() != nil:
		return report{}, ctx.Err()
	case cctx.Err() != nil:
		return report{}, errTimeout
	}
	var exit *exec.ExitError
	if err != nil && !errors.As(err, &exit) {
		return report{}, fmt.Errorf("the host's CLI: %w", pathless(err))
	}
	r := s.p.entry.Read(out)
	if over {
		r.Fail = CodeNoUsage
		return r, nil
	}
	if r.Fail != "" {
		return r, nil
	}
	if exit != nil {
		if ws, ok := exit.Sys().(syscall.WaitStatus); ok && ws.Signaled() {
			r.Fail = CodeHostFailed
			return r, nil
		}
	}
	switch {
	case !r.Answered:
		r.Fail = CodeHostFailed
	case !r.Usage || r.PromptTokens+r.OutputTokens == 0:
		r.Fail = CodeNoUsage
	}
	return r, nil
}

// cappedBuffer keeps the first limit bytes written to it, and says
// whether more came.
type cappedBuffer struct {
	buffer bytes.Buffer
	limit  int
	over   bool
}

func (b *cappedBuffer) Write(p []byte) (int, error) {
	n := len(p)
	if room := b.limit - b.buffer.Len(); n > room {
		p = p[:max(room, 0)]
		b.over = true
	}
	_, _ = b.buffer.Write(p)
	return n, nil
}

// runGroup runs cli in dir with exactly the environment env, in a process
// group of its own that is killed when ctx ends and as soon as cli has
// exited, so that nothing it left running goes on editing the workspace
// while it is graded, and gives the first limit bytes the group printed on
// stdout, and whether it printed more. Its stderr is dropped. Its stdout
// is a pipe of runGroup's own, so that the wait ends when cli exits, not
// when the last process holding the pipe does; one that left the group and
// still holds it after waitDelay is an error.
func runGroup(ctx context.Context, cli string, args, env []string, dir string, limit int) ([]byte, bool, error) {
	r, w, err := os.Pipe()
	if err != nil {
		return nil, false, pathless(err)
	}
	cmd := exec.CommandContext(ctx, cli, args...)
	cmd.Dir = dir
	cmd.Env = env
	cmd.Stdout = w
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return killGroup(cmd.Process.Pid) }
	cmd.WaitDelay = waitDelay
	out := &cappedBuffer{limit: limit}
	copied := make(chan struct{})
	go func() {
		_, _ = io.Copy(out, r)
		close(copied)
	}()
	err = cmd.Start()
	_ = w.Close()
	if err == nil {
		err = cmd.Wait()
		_ = killGroup(cmd.Process.Pid)
	}
	held := time.NewTimer(waitDelay)
	defer held.Stop()
	select {
	case <-copied:
	case <-held.C:
		_ = r.Close()
		<-copied
		if err == nil {
			err = errors.New("a process the host's CLI started still holds its output")
		}
	}
	_ = r.Close()
	return out.buffer.Bytes(), out.over, err
}

// killGroup kills the process group pid leads.
func killGroup(pid int) error {
	err := syscall.Kill(-pid, syscall.SIGKILL)
	if errors.Is(err, syscall.ESRCH) {
		return os.ErrProcessDone
	}
	return err
}
