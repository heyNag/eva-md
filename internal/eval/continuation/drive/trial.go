package drive

import (
	"bytes"
	"context"
	"errors"
	"fmt"
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
	root := filepath.Join(p.work, run, t.ID, string(p.opts.Condition), "t"+strconv.Itoa(n))
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
	tr.Env = []string{
		"HOME=" + tr.Dirs.Home,
		"PATH=" + tr.Dirs.Bin + string(filepath.ListSeparator) + os.Getenv("PATH"),
		"TMPDIR=" + tr.Dirs.Tmp,
		"GIT_CONFIG_NOSYSTEM=1",
	}
	for _, name := range p.opts.PassEnv {
		tr.Env = append(tr.Env, name+"="+os.Getenv(name))
	}
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
			out, err := runGroup(vctx, cli, args, tr.Env, tr.Dirs.Home, 64<<10)
			if err != nil {
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

	s := session{p: p, tr: tr, env: env, extra: extra}
	sctx, cancel := context.WithTimeout(ctx, p.opts.SessionTimeout)
	code, err := s.run(sctx)
	cancel()
	res.tokens = s.outcome.PromptTokens + s.outcome.OutputTokens
	res.outcome, res.model = s.outcome, s.model
	switch {
	case ctx.Err() != nil:
		return res, ctx.Err()
	case code == "" && err != nil && sctx.Err() != nil:
		code, err = CodeTimeout, nil
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
}

// run runs the chat until every hidden test passes or the task's rounds
// are spent. It returns a code when the trial is unjudged. An error with
// ctx ended is the session's time limit.
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
		if err != nil {
			return "", err
		}
		s.outcome.Rounds = round
		s.outcome.AgentEvents += r.AgentEvents
		s.outcome.PromptTokens += r.PromptTokens
		s.outcome.CachedTokens += r.CachedTokens
		s.outcome.OutputTokens += r.OutputTokens
		switch {
		case r.Fail != "":
			return r.Fail, nil
		case s.p.opts.Model != "" && r.Model != s.p.opts.Model:
			return CodeOtherModel, nil
		}
		if idPattern.MatchString(r.Model) {
			s.model = r.Model
		}
		if r.Session != "" {
			id = r.Session
		}
		g, err := task.Grade(ctx, t, t.Dir, s.tr.Dirs.Workspace, filepath.Join(s.tr.Dirs.Grade, "r"+strconv.Itoa(round)))
		if err != nil {
			return "", err
		}
		if g.Solved() {
			s.outcome.Solved = true
			s.outcome.FirstTry = round == 1
			break
		}
		if round >= t.MaxRounds || id == "" {
			break
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

// call runs one headless call of the chat in the workspace and reads its
// output. The time it took is added to the outcome.
func (s *session) call(ctx context.Context, c chat) (report, error) {
	start := s.p.opts.Clock()
	out, err := runGroup(ctx, s.tr.CLI, s.p.entry.Argv(c), s.env, s.tr.Dirs.Workspace, outputLimit)
	s.outcome.WallMs += s.p.opts.Clock().Sub(start).Milliseconds()
	if ctx.Err() != nil {
		return report{}, ctx.Err()
	}
	r := s.p.entry.Read(out)
	var exit *exec.ExitError
	if err != nil && !errors.As(err, &exit) {
		return report{}, fmt.Errorf("the host's CLI: %w", pathless(err))
	}
	// A call that exits other than 0 for no reason the host names, such as
	// the turn bound, is graded as it stands.
	return r, nil
}

// cappedBuffer keeps the first limit bytes written to it.
type cappedBuffer struct {
	buffer bytes.Buffer
	limit  int
}

func (b *cappedBuffer) Write(p []byte) (int, error) {
	n := len(p)
	if room := b.limit - b.buffer.Len(); n > room {
		p = p[:max(room, 0)]
	}
	_, _ = b.buffer.Write(p)
	return n, nil
}

// runGroup runs cli in dir with exactly the environment env, in a process
// group of its own that is killed when ctx ends and once it has exited,
// and gives the first limit bytes it printed on stdout. Its stderr is
// dropped.
func runGroup(ctx context.Context, cli string, args, env []string, dir string, limit int) ([]byte, error) {
	cmd := exec.CommandContext(ctx, cli, args...)
	cmd.Dir = dir
	cmd.Env = env
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return killGroup(cmd.Process.Pid) }
	cmd.WaitDelay = waitDelay
	out := &cappedBuffer{limit: limit}
	cmd.Stdout = out
	err := cmd.Run()
	if cmd.Process != nil {
		_ = killGroup(cmd.Process.Pid)
	}
	return out.buffer.Bytes(), err
}

// killGroup kills the process group pid leads.
func killGroup(pid int) error {
	err := syscall.Kill(-pid, syscall.SIGKILL)
	if errors.Is(err, syscall.ESRCH) {
		return os.ErrProcessDone
	}
	return err
}
