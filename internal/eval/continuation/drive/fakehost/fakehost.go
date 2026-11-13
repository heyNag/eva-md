// Package fakehost is a fake host CLI for the continuation driver's tests,
// one executable for every host the adapter knows. It is the test binary
// run again under the name of a host's CLI (adapter's Names.CLI), as the
// driver links the CLI it is given; a test package calls Main first in its
// TestMain, or in an init function of a test file, and Main takes over
// only when ScriptVar names a script, which the driver passes only when a
// test names it with --pass-env.
//
// It answers --version, and starts and continues headless sessions in the
// shapes of the driver's host table (drive/hosts.go), which are its own
// until the pilot (EV-03) records the real ones. Each call is recorded as
// a line of Calls in the script's folder: its arguments, the names of its
// variables (and the values of the four the driver builds), its folder,
// its prompt, its session and the files of its folder before it changed
// any, and those of the folder that holds it. Then it lays the task's
// decoy/ overlay over the folder until every one of the script's facts has
// appeared in a prompt of the session, and its solution/ overlay after. A
// script can make it stall until it is killed, with a child holding its
// output; leave such a child behind an answer; fail its sign-in, a rate
// limit or with another status; exit having printed nothing; kill itself,
// before an answer or after a whole one; answer with no session, with no
// usage, or at the turn bound; name another model, or none; pad its output.
// Its failures can be kept to one session and to the rounds of a session
// from one on.
//
// Only tests import it.
package fakehost

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"

	"github.com/heyNag/eva-md/internal/eval/continuation/task"
	"github.com/heyNag/eva-md/internal/host/adapter"
)

// ScriptVar is the variable that names a script's file.
const ScriptVar = "EVA_FAKEHOST_SCRIPT"

// Calls is the record's file in the script's folder.
const Calls = "calls.jsonl"

// Script is what the fake does.
type Script struct {
	// Dir is the folder the fake records in.
	Dir string `json:"dir"`
	// Task is the task's folder, whose decoy/ and solution/ it lays.
	Task string `json:"task"`
	// Facts are words: once each has appeared in a prompt of a session,
	// the solution is laid.
	Facts []string `json:"facts"`
	// Version is what --version gives; "1.2.3" when empty.
	Version string `json:"version"`
	// Model is the model the fake says answered; when empty, the one asked
	// for, or "fake-model-1".
	Model string `json:"model"`
	// Events are the tool calls of each answer.
	Events int `json:"events"`
	// Usage is each answer's tokens: prompt (cached included), cached and
	// output.
	Prompt int64 `json:"prompt"`
	Cached int64 `json:"cached"`
	Output int64 `json:"output"`
	// Stall makes a call start a child that holds its stdout, then block,
	// as the child does, until it is killed. Ready, when set, is a named
	// pipe the child writes a line to once it runs.
	Stall bool   `json:"stall"`
	Ready string `json:"ready"`
	// Leave makes a call answer as usual and exit, leaving such a child
	// behind, blocked until it is killed.
	Leave bool `json:"leave"`
	// SignIn and RateLimited make a call fail as the host does, with 401
	// and 429; Status with any other HTTP status, such as a 529 or a 500.
	SignIn      bool `json:"sign_in"`
	RateLimited bool `json:"rate_limited"`
	Status      int  `json:"status"`
	// Empty makes a call exit 1 having printed nothing; Killed makes it
	// kill itself with SIGKILL; KilledAfter makes it print a whole answer,
	// as usual, and then kill itself so.
	Empty       bool `json:"empty"`
	Killed      bool `json:"killed"`
	KilledAfter bool `json:"killed_after"`
	// From is the first round of a session the failures (SignIn to
	// KilledAfter, and NoSession) apply to; 0 is the first.
	From int `json:"from"`
	// Session, when set, is the one session, by the number the fake gives
	// it (1 for the first started), the failures apply to; the others
	// answer as usual.
	Session int `json:"session"`
	// NoSession makes an answer name no session.
	NoSession bool `json:"no_session"`
	// NoModel makes an answer name no model anywhere.
	NoModel bool `json:"no_model"`
	// TurnBound makes an answer end at the turn bound, as the host says it,
	// exiting 1 (Claude Code's shape; Codex's exec has no turn bound, and
	// answers as usual).
	TurnBound bool `json:"turn_bound"`
	// NoUsage makes an answer give no usage: Claude Code's result event has
	// neither usage nor modelUsage, and Codex's turn.completed no usage. It
	// is one of the failures From and Session keep to some calls.
	NoUsage bool `json:"no_usage"`
	// Pad is the size of a line of padding, an event no reader knows, the
	// fake prints before its answer.
	Pad int `json:"pad"`
}

// ChildVar is the variable that makes the test binary a fake's child: it
// writes a line to the named pipe the variable names, when it names one,
// and blocks until it is killed.
const ChildVar = "EVA_FAKEHOST_CHILD"

// Call is one recorded call.
type Call struct {
	Host    adapter.ID        `json:"host"`
	Mode    string            `json:"mode"` // "version", "start", "continue", or "other" for a call that is none
	Args    []string          `json:"args"`
	Env     []string          `json:"env"`   // every variable's name, sorted
	Fixed   map[string]string `json:"fixed"` // HOME, PATH, TMPDIR, GIT_CONFIG_NOSYSTEM
	Dir     string            `json:"dir"`
	Session string            `json:"session,omitempty"`
	Prompt  string            `json:"prompt,omitempty"`
	Model   string            `json:"model,omitempty"` // the model asked for
	Files   []string          `json:"files,omitempty"` // the folder's files, slash paths, .git left out
	// Around are the files of the folder that holds the call's folder,
	// which the driver makes a trial's, as Files lists them.
	Around []string `json:"around,omitempty"`
}

// Main runs the fake and exits when the process is a host's CLI with a
// script; otherwise it returns at once.
func Main() {
	if ready, ok := os.LookupEnv(ChildVar); ok {
		child(ready)
	}
	file := os.Getenv(ScriptVar)
	if file == "" {
		return
	}
	var host adapter.Host
	for _, h := range adapter.All() {
		if filepath.Base(os.Args[0]) == h.Names.CLI {
			host = h
		}
	}
	if host.ID == "" {
		return
	}
	os.Exit(serve(host, file, os.Args[1:], os.Stdout))
}

func serve(host adapter.Host, file string, args []string, out io.Writer) int {
	data, err := os.ReadFile(file)
	if err != nil {
		return 3
	}
	var s Script
	if err := json.Unmarshal(data, &s); err != nil {
		return 3
	}
	dir, _ := os.Getwd()
	c := Call{Host: host.ID, Args: args, Dir: dir, Fixed: map[string]string{}}
	for _, kv := range os.Environ() {
		name, value, _ := strings.Cut(kv, "=")
		c.Env = append(c.Env, name)
		if name == "HOME" || name == "PATH" || name == "TMPDIR" || name == "GIT_CONFIG_NOSYSTEM" {
			c.Fixed[name] = value
		}
	}
	slices.Sort(c.Env)
	if slices.Equal(args, []string{"--version"}) {
		c.Mode = "version"
		if err := record(s, c); err != nil {
			return 3
		}
		v := s.Version
		if v == "" {
			v = "1.2.3"
		}
		fmt.Fprintf(out, "%s (fake)\n", v)
		return 0
	}
	if !parse(host.ID, &c) {
		// Not a headless chat: recorded, and refused.
		c.Mode = "other"
		if err := record(s, c); err != nil {
			return 3
		}
		return 2
	}
	calls, err := Read(s.Dir)
	if err != nil {
		return 3
	}
	if c.Session == "" {
		c.Mode = "start"
		c.Session = "fake-session-" + strconv.Itoa(1+count(calls, func(x Call) bool { return x.Mode == "start" }))
	} else {
		c.Mode = "continue"
	}
	if c.Files, err = files(dir); err != nil {
		return 3
	}
	if c.Around, err = files(filepath.Dir(dir)); err != nil {
		return 3
	}
	if err := record(s, c); err != nil {
		return 3
	}
	if s.Stall {
		if spawn(s.Ready) != nil {
			return 3
		}
		block()
	}
	model := s.Model
	if model == "" {
		model = c.Model
	}
	if model == "" {
		model = "fake-model-1"
	}
	prompts := []string{c.Prompt}
	for _, x := range calls {
		if x.Session == c.Session && x.Mode != "version" && x.Mode != "other" {
			prompts = append(prompts, x.Prompt)
		}
	}
	failing := len(prompts) >= s.From && (s.Session == 0 || c.Session == "fake-session-"+strconv.Itoa(s.Session))
	if failing {
		switch {
		case s.Killed:
			_ = syscall.Kill(os.Getpid(), syscall.SIGKILL)
			block()
		case s.Empty:
			return 1
		case s.SignIn || s.RateLimited || s.Status != 0:
			fail(host.ID, s, c.Session, out)
			return 1
		}
	}
	overlay := task.OverlayDecoy
	if allIn(s.Facts, prompts) {
		overlay = task.OverlaySolution
	}
	if err := lay(s.Task, overlay, dir); err != nil {
		return 3
	}
	session := c.Session
	if s.NoSession && failing {
		session = ""
	}
	if s.NoModel {
		model = ""
	}
	if s.Leave && spawn("") != nil {
		return 3
	}
	if s.Pad > 0 {
		emit(out, map[string]any{"type": "padding", "text": strings.Repeat("p", s.Pad)})
	}
	s.NoUsage = s.NoUsage && failing
	answer(host.ID, s, session, model, out)
	if s.KilledAfter && failing {
		_ = syscall.Kill(os.Getpid(), syscall.SIGKILL)
		block()
	}
	if s.TurnBound && host.ID == adapter.ClaudeCode {
		return 1
	}
	return 0
}

// spawn starts the test binary again as a child (ChildVar) that holds
// this process's stdout and blocks until it is killed, writing a line to
// the named pipe ready, when given, once it runs. It stays in this
// process's group.
func spawn(ready string) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	cmd := exec.Command(exe)
	cmd.Env = append(os.Environ(), ChildVar+"="+ready)
	cmd.Stdout = os.Stdout
	return cmd.Start()
}

// child is the fake's child: it tells the named pipe ready, when given,
// that it runs, then blocks until it is killed.
func child(ready string) {
	if ready != "" {
		f, err := os.OpenFile(ready, os.O_WRONLY, 0)
		if err != nil {
			os.Exit(3)
		}
		_, _ = f.WriteString("running\n")
		_ = f.Close()
	}
	block()
}

// parse reads the prompt, the session continued and the model asked for
// from the arguments, as the driver's host table writes them, and reports
// whether they are a headless chat in that shape.
func parse(id adapter.ID, c *Call) bool {
	// The prompt is the one argument after "--"; the flags come before it.
	end := slices.Index(c.Args, "--")
	if end < 0 || end != len(c.Args)-2 {
		return false
	}
	args := c.Args[:end]
	c.Prompt = c.Args[end+1]
	value := func(flag string) string {
		if i := slices.Index(args, flag); i >= 0 && i+1 < len(args) {
			return args[i+1]
		}
		return ""
	}
	c.Model = value("--model")
	switch id {
	case adapter.ClaudeCode:
		c.Session = value("--resume")
		return slices.Contains(args, "-p")
	case adapter.Codex:
		if len(args) < 1 || args[0] != "exec" {
			return false
		}
		c.Session = value("resume")
		return true
	}
	return false
}

func count(calls []Call, f func(Call) bool) int {
	n := 0
	for _, c := range calls {
		if f(c) {
			n++
		}
	}
	return n
}

// allIn reports whether every word is in one of the prompts.
func allIn(words, prompts []string) bool {
	for _, w := range words {
		if !slices.ContainsFunc(prompts, func(p string) bool { return strings.Contains(p, w) }) {
			return false
		}
	}
	return true
}

// lay lays the task's overlay o, when it has one, over the folder dir.
func lay(taskDir, o, dir string) error {
	t, err := task.Load(taskDir)
	if err != nil {
		return err
	}
	for _, name := range t.Repos {
		root := dir
		if t.Workdir == task.WorkdirOutside {
			root = filepath.Join(dir, name)
		}
		src := filepath.Join(taskDir, o, name)
		if _, err := os.Stat(src); errors.Is(err, fs.ErrNotExist) {
			continue
		}
		err := filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return err
			}
			rel, err := filepath.Rel(src, p)
			if err != nil {
				return err
			}
			data, err := os.ReadFile(p)
			if err != nil {
				return err
			}
			info, err := d.Info()
			if err != nil {
				return err
			}
			to := filepath.Join(root, rel)
			if err := os.MkdirAll(filepath.Dir(to), 0o755); err != nil {
				return err
			}
			return os.WriteFile(to, data, info.Mode().Perm())
		})
		if err != nil {
			return err
		}
	}
	return nil
}

// files lists the files under dir, as slash paths, leaving out git's own
// folders.
func files(dir string) ([]string, error) {
	var list []string
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && d.Name() == ".git" {
			return filepath.SkipDir
		}
		if !d.IsDir() {
			rel, err := filepath.Rel(dir, p)
			if err != nil {
				return err
			}
			list = append(list, filepath.ToSlash(rel))
		}
		return nil
	})
	return list, err
}

// record appends c to the record, in one write.
func record(s Script, c Call) error {
	line, err := json.Marshal(c)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(filepath.Join(s.Dir, Calls), os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0o644)
	if err != nil {
		return err
	}
	_, err = f.Write(append(line, '\n'))
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	return err
}

// Read reads the record in dir; none is an empty record.
func Read(dir string) ([]Call, error) {
	data, err := os.ReadFile(filepath.Join(dir, Calls))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var calls []Call
	for _, line := range strings.Split(strings.TrimSuffix(string(data), "\n"), "\n") {
		if line == "" {
			continue
		}
		var c Call
		if err := json.Unmarshal([]byte(line), &c); err != nil {
			return nil, err
		}
		calls = append(calls, c)
	}
	return calls, nil
}

// block blocks until the process is killed: nothing ever sends SIGUSR1.
func block() {
	c := make(chan os.Signal, 1)
	signal.Notify(c, syscall.SIGUSR1)
	<-c
	os.Exit(3)
}

func emit(out io.Writer, v any) {
	line, _ := json.Marshal(v)
	fmt.Fprintf(out, "%s\n", line)
}

// answer writes an answer in the host's shape. A model of "" is named
// nowhere: Claude Code's result then gives its tokens by usage alone.
func answer(id adapter.ID, s Script, session, model string, out io.Writer) {
	// withModel adds the model to an event under key, when there is one.
	withModel := func(e map[string]any, key string) map[string]any {
		if model != "" {
			e[key] = model
		}
		return e
	}
	switch id {
	case adapter.ClaudeCode:
		emit(out, withModel(map[string]any{"type": "system", "subtype": "init", "session_id": session}, "model"))
		for range s.Events {
			emit(out, map[string]any{"type": "assistant", "session_id": session,
				"message": withModel(map[string]any{"content": []any{map[string]any{"type": "tool_use", "name": "Edit"}}}, "model")})
		}
		emit(out, map[string]any{"type": "assistant", "session_id": session,
			"message": withModel(map[string]any{"content": []any{map[string]any{"type": "text", "text": "Done."}}}, "model")})
		subtype := "success"
		if s.TurnBound {
			subtype = "error_max_turns"
		}
		result := map[string]any{"type": "result", "subtype": subtype, "is_error": s.TurnBound, "session_id": session}
		if !s.NoUsage {
			result["usage"] = map[string]any{"input_tokens": s.Prompt - s.Cached, "cache_read_input_tokens": s.Cached, "cache_creation_input_tokens": 0, "output_tokens": s.Output}
			if model != "" {
				result["modelUsage"] = map[string]any{model: map[string]any{"inputTokens": s.Prompt - s.Cached, "cacheReadInputTokens": s.Cached, "cacheCreationInputTokens": 0, "outputTokens": s.Output}}
			}
		}
		emit(out, result)
	case adapter.Codex:
		emit(out, map[string]any{"type": "thread.started", "thread_id": session})
		emit(out, map[string]any{"type": "turn.started"})
		for range s.Events {
			emit(out, map[string]any{"type": "item.completed", "item": map[string]any{"type": "command_execution"}})
		}
		emit(out, map[string]any{"type": "item.completed", "item": map[string]any{"type": "agent_message", "text": "Done."}})
		done := map[string]any{"type": "turn.completed"}
		if !s.NoUsage {
			done["usage"] = map[string]any{"input_tokens": s.Prompt, "cached_input_tokens": s.Cached, "output_tokens": s.Output}
		}
		emit(out, withModel(done, "model"))
	}
}

// fail writes a failed answer in the host's shape: a sign-in refused, a
// rate limit, or the script's status.
func fail(id adapter.ID, s Script, session string, out io.Writer) {
	status := s.Status
	switch {
	case s.SignIn:
		status = 401
	case s.RateLimited:
		status = 429
	}
	switch id {
	case adapter.ClaudeCode:
		emit(out, map[string]any{"type": "result", "subtype": "error", "is_error": true, "session_id": session, "api_error_status": status})
	case adapter.Codex:
		emit(out, map[string]any{"type": "thread.started", "thread_id": session})
		emit(out, map[string]any{"type": "turn.failed", "error": map[string]any{"message": "unexpected status " + strconv.Itoa(status)}})
	}
}
