package v1arm

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/heyNag/eva-md/internal/eval/continuation/drive"
	"github.com/heyNag/eva-md/internal/eval/continuation/drive/fakehost"
	"github.com/heyNag/eva-md/internal/eval/continuation/task"
	"github.com/heyNag/eva-md/internal/eval/results"
	"github.com/heyNag/eva-md/internal/host/adapter"
	"github.com/heyNag/eva-md/internal/host/hostfake"
	"github.com/heyNag/eva-md/internal/inbox"
	"github.com/heyNag/eva-md/internal/paths"
	"github.com/heyNag/eva-md/internal/testgitenv"
	"github.com/heyNag/eva-md/internal/update"
)

// The test binary is run again as each program a trial starts: a fake
// eva-md (fakeVar), a fake sign-in program (signInVar), a job a fake hook
// leaves holding eva-md's locks (childVar), the driver's fake host CLI
// (fakehost) and HO-14's fake plugin commands (hostfake). The limits of
// the arm and of the driver are raised once, so that nothing a slow
// machine does reaches them; a test that needs a limit to end ends it at
// an exact point (stalled).
func TestMain(m *testing.M) {
	childMain()
	fakeMain()
	signInMain()
	fakehost.Main()
	hostfake.Main()
	testgitenv.Clear()
	drive.VersionTimeout = 10 * time.Minute
	drive.WaitDelay = 10 * time.Minute
	SignInTimeout, SetupTimeout, HookTimeout, UpdateTimeout, LockTimeout = time.Hour, time.Hour, time.Hour, time.Hour, time.Hour
	// The fake eva-md is no evaluation build; TestCheck turns the check on.
	skipTagCheck = true
	os.Exit(m.Run())
}

// The variables that name a fake's script.
const (
	fakeVar   = "EVA_V1ARM_FAKE_SCRIPT"
	signInVar = "EVA_V1ARM_SIGN_IN_SCRIPT"
	childVar  = "EVA_V1ARM_CHILD"
)

// fakeScript is what the fake eva-md does.
type fakeScript struct {
	Dir   string `json:"dir"`   // where it records
	Chats string `json:"chats"` // the fake host's record, whose chats each call counts
	// Print is a word each command prints on stdout and stderr.
	Print string `json:"print"`
	// SetupFail lists the setup calls, from 1, that exit 1; SetupStall
	// makes setup write a line to the named pipe and block until killed.
	SetupFail  []int  `json:"setup_fail"`
	SetupStall string `json:"setup_stall"`
	// NoPlugin makes setup install no plugin.
	NoPlugin bool `json:"no_plugin"`
	// HookFail makes the hook of that event exit 1; HookStall makes it
	// stall as setup does.
	HookFail  string `json:"hook_fail"`
	HookStall string `json:"hook_stall"`
	// Updates are the update's answers, one a call it does not find busy
	// by its lock, the last again past the end.
	Updates []fakeUpdate `json:"updates"`
	// Hold makes the SessionEnd hook leave a job holding locks.
	Hold *hold `json:"hold"`
	// Plant are sessions setup writes into the inbox; RunLog, records it
	// writes into the run log.
	Plant  []inbox.Entry   `json:"plant"`
	RunLog []update.Record `json:"run_log"`
}

// fakeUpdate is one answer of the fake's update.
type fakeUpdate struct {
	Line       string `json:"line"`    // printed as "update: <line>"
	Exit       int    `json:"exit"`    // its exit
	Outcome    string `json:"outcome"` // a run-log record of the session, when set
	TokensIn   int    `json:"tokens_in"`
	TokensOut  int    `json:"tokens_out"`
	DurationMS int64  `json:"duration_ms"`
	Stall      string `json:"stall"` // a named pipe: write a line, then block
}

// hold is a job a hook leaves: it takes Locks, reads a line from the named
// pipe Release, records the fake host's chats so far, writes a run-log
// record of the session with Outcome, when set, and lets go.
type hold struct {
	Locks   []string `json:"locks"`
	Release string   `json:"release"`
	Outcome string   `json:"outcome"`
	// Set by the hook.
	Home    string `json:"home"`
	Host    string `json:"host"`
	Session string `json:"session"`
	Dir     string `json:"dir"`
	Chats   string `json:"chats"`
}

// fakeCall is one call of the fake eva-md, or of the fake sign-in program.
type fakeCall struct {
	Args       []string `json:"args"`
	Dir        string   `json:"dir"`
	Env        []string `json:"env"` // the variables' names, sorted
	Home       string   `json:"home"`
	Entrypoint bool     `json:"entrypoint"` // CLAUDE_CODE_ENTRYPOINT is set
	Actor      bool     `json:"actor"`      // EVA_MD_EVAL_ACTOR is set
	Stdin      string   `json:"stdin"`
	Chats      int      `json:"chats"`  // the fake host's chats so far
	Remote     bool     `json:"remote"` // setup's --remote is a repository by then
	Busy       bool     `json:"busy"`   // an update that found its lock held
	Before     int      `json:"before"` // the sign-in program: eva-md's calls so far
}

// chatCount counts the fake host's chats in its record dir.
func chatCount(dir string) int {
	if dir == "" {
		return 0
	}
	calls, err := fakehost.Read(dir)
	if err != nil {
		return -1
	}
	n := 0
	for _, c := range calls {
		if c.Mode == "start" || c.Mode == "continue" {
			n++
		}
	}
	return n
}

// appendJSON appends v as a line to path.
func appendJSON(path string, v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.Write(append(data, '\n'))
	return err
}

// readLines reads the JSON lines of path into a slice of T; none is none.
func readLines[T any](path string) ([]T, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []T
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if line == "" {
			continue
		}
		var v T
		if err := json.Unmarshal([]byte(line), &v); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, nil
}

// newCall records what this process was given.
func newCall(stdin []byte) fakeCall {
	dir, _ := os.Getwd()
	c := fakeCall{Args: os.Args[1:], Dir: dir, Home: os.Getenv("HOME"), Stdin: string(stdin)}
	for _, kv := range os.Environ() {
		name, _, _ := strings.Cut(kv, "=")
		c.Env = append(c.Env, name)
	}
	slices.Sort(c.Env)
	_, c.Entrypoint = os.LookupEnv(entrypointVar)
	_, c.Actor = os.LookupEnv(actorVar)
	return c
}

// flagValue is the value of --name in args.
func flagValue(args []string, name string) string {
	for i, a := range args {
		if a == "--"+name && i+1 < len(args) {
			return args[i+1]
		}
	}
	return ""
}

// stallOn writes a line to the named pipe and blocks until killed.
func stallOn(pipe string) {
	if f, err := os.OpenFile(pipe, os.O_WRONLY, 0); err == nil {
		_, _ = f.WriteString("stalled\n")
		f.Close()
	}
	for {
		time.Sleep(time.Hour)
	}
}

// fakeMain is the fake eva-md, when the process is one.
func fakeMain() {
	file := os.Getenv(fakeVar)
	if file == "" || filepath.Base(os.Args[0]) != "eva-md" {
		return
	}
	os.Exit(fake(file))
}

func fake(file string) int {
	data, err := os.ReadFile(file)
	if err != nil {
		return 3
	}
	var s fakeScript
	if json.Unmarshal(data, &s) != nil {
		return 3
	}
	stdin, _ := io.ReadAll(os.Stdin)
	c := newCall(stdin)
	c.Chats = chatCount(s.Chats)
	record := filepath.Join(s.Dir, "eva-md.jsonl")
	earlier, err := readLines[fakeCall](record)
	if err != nil {
		return 3
	}
	if s.Print != "" {
		os.Stdout.WriteString(s.Print + "\n")
		os.Stderr.WriteString(s.Print + "\n")
	}
	home := os.Getenv("HOME")
	state := filepath.Join(home, ".local", "state", "eva-md")
	if len(os.Args) < 2 {
		return 2
	}
	switch os.Args[1] {
	case "setup":
		remote := flagValue(c.Args, "remote")
		_, err := os.Stat(filepath.Join(remote, "HEAD"))
		c.Remote = remote != "" && err == nil
		if appendJSON(record, c) != nil {
			return 3
		}
		n := 1 + count(earlier, "setup")
		if s.SetupStall != "" {
			stallOn(s.SetupStall)
		}
		if slices.Contains(s.SetupFail, n) {
			return 1
		}
		if !s.NoPlugin {
			dir := filepath.Join(home, ".local", "share", "eva-md", "marketplace", flagValue(c.Args, "host"), "plugins", "eva-md", "hooks")
			if os.MkdirAll(dir, 0o755) != nil || os.WriteFile(filepath.Join(dir, "hooks.json"), []byte(fakeHooks), 0o644) != nil {
				return 3
			}
		}
		for _, e := range s.Plant {
			if (inbox.Store{Root: filepath.Join(state, "inbox")}).Append(e) != nil {
				return 3
			}
		}
		for _, r := range s.RunLog {
			if writeRecord(state, r) != nil {
				return 3
			}
		}
		return 0
	case "hook":
		if appendJSON(record, c) != nil {
			return 3
		}
		var p Payload
		if json.Unmarshal(stdin, &p) != nil {
			return 3
		}
		if s.HookStall != "" {
			stallOn(s.HookStall)
		}
		if s.HookFail == p.Event {
			return 1
		}
		if p.Event == "SessionEnd" && s.Hold != nil {
			h := *s.Hold
			h.Home, h.Host, h.Session, h.Dir, h.Chats = home, flagValue(c.Args, "host"), p.Session, s.Dir, s.Chats
			if leave(h) != nil {
				return 3
			}
		}
		return 0
	case "update":
		host, session, _ := strings.Cut(flagValue(c.Args, "session"), ":")
		unlock, busy, err := paths.TryLock(filepath.Join(state, "locks"), "update.lock")
		if err != nil {
			return 3
		}
		c.Busy = busy
		if appendJSON(record, c) != nil {
			return 3
		}
		if busy {
			os.Stdout.WriteString("update: stopped: busy\n")
			return 0
		}
		defer unlock()
		n := 0
		for _, e := range earlier {
			if len(e.Args) > 0 && e.Args[0] == "update" && !e.Busy {
				n++
			}
		}
		if len(s.Updates) == 0 {
			return 3
		}
		u := s.Updates[min(n, len(s.Updates)-1)]
		if u.Stall != "" {
			stallOn(u.Stall)
		}
		if u.Outcome != "" {
			r := update.Record{Host: host, Session: session, Outcome: u.Outcome, TokensIn: u.TokensIn, TokensOut: u.TokensOut, DurationMS: u.DurationMS}
			if writeRecord(state, r) != nil {
				return 3
			}
		}
		os.Stdout.WriteString("update: " + u.Line + "\n")
		return u.Exit
	}
	return 2
}

// fakeHooks is the hooks file the fake's setup installs, in the shape the
// host table reads.
const fakeHooks = `{"hooks":{"SessionStart":[{"hooks":[{"type":"command","command":"eva-md hook","timeout":5}]}]}}`

func count(calls []fakeCall, command string) int {
	n := 0
	for _, c := range calls {
		if len(c.Args) > 0 && c.Args[0] == command {
			n++
		}
	}
	return n
}

// writeRecord appends r to the run log of today, by this process's clock.
func writeRecord(state string, r update.Record) error {
	now := time.Now()
	if r.At == "" {
		r.At = now.Format(time.RFC3339)
	}
	dir := filepath.Join(state, "updates")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	return appendJSON(filepath.Join(dir, now.Format(time.DateOnly)+".jsonl"), r)
}

// leave starts the job h in a session of its own, as eva-md's hooks start
// theirs, and returns once it holds its locks.
func leave(h hold) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	spec, err := json.Marshal(h)
	if err != nil {
		return err
	}
	r, w, err := os.Pipe()
	if err != nil {
		return err
	}
	defer r.Close()
	cmd := exec.Command(exe)
	cmd.Env = []string{childVar + "=" + string(spec)}
	cmd.ExtraFiles = []*os.File{w}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	err = cmd.Start()
	w.Close()
	if err != nil {
		return err
	}
	_ = cmd.Process.Release()
	var b [1]byte
	if n, _ := r.Read(b[:]); n != 1 {
		return os.ErrInvalid
	}
	return nil
}

// childMain is the job a fake hook leaves, when the process is one.
func childMain() {
	spec := os.Getenv(childVar)
	if spec == "" {
		return
	}
	var h hold
	if json.Unmarshal([]byte(spec), &h) != nil {
		os.Exit(3)
	}
	state := filepath.Join(h.Home, ".local", "state", "eva-md")
	var unlocks []func()
	for _, name := range h.Locks {
		unlock, busy, err := paths.TryLock(filepath.Join(state, "locks"), name)
		if err != nil || busy {
			os.Exit(3)
		}
		unlocks = append(unlocks, unlock)
	}
	ready := os.NewFile(3, "ready")
	_, _ = ready.Write([]byte{1})
	ready.Close()
	if f, err := os.Open(h.Release); err == nil {
		_, _ = io.ReadAll(f)
		f.Close()
	}
	_ = appendJSON(filepath.Join(h.Dir, "child.jsonl"), map[string]int{"chats": chatCount(h.Chats)})
	if h.Outcome != "" {
		_ = writeRecord(state, update.Record{Host: h.Host, Session: h.Session, Outcome: h.Outcome})
	}
	for _, unlock := range unlocks {
		unlock()
	}
	os.Exit(0)
}

// signInScript is what the fake sign-in program does.
type signInScript struct {
	Dir   string `json:"dir"`
	Fail  bool   `json:"fail"`
	Stall string `json:"stall"`
	Print string `json:"print"`
}

// signInMain is the fake sign-in program, when the process is one: it
// records its call, with the number of eva-md's calls before it.
func signInMain() {
	file := os.Getenv(signInVar)
	if file == "" || filepath.Base(os.Args[0]) != "sign-in" {
		return
	}
	data, err := os.ReadFile(file)
	var s signInScript
	if err != nil || json.Unmarshal(data, &s) != nil {
		os.Exit(3)
	}
	c := newCall(nil)
	calls, err := readLines[fakeCall](filepath.Join(s.Dir, "eva-md.jsonl"))
	if err != nil {
		os.Exit(3)
	}
	c.Before = len(calls)
	if appendJSON(filepath.Join(s.Dir, "sign-in.jsonl"), c) != nil {
		os.Exit(3)
	}
	if s.Print != "" {
		os.Stdout.WriteString(s.Print + "\n")
		os.Stderr.WriteString(s.Print + "\n")
	}
	if s.Stall != "" {
		stallOn(s.Stall)
	}
	if s.Fail {
		os.Exit(1)
	}
	os.Exit(0)
}

// rig is one test's made-up world for runs through the driver: the
// driver's fake host and its fixture task, the fake eva-md and the fake
// sign-in program, a repository the rows' commit is read from, and the
// folders of a run.
type rig struct {
	t      *testing.T
	base   string // the test's folder, links resolved
	exe    string // the test binary
	opts   drive.Options
	host   fakehost.Script
	eva    fakeScript
	signIn *signInScript
	record string // the fakes' records
}

func newRig(t *testing.T, h adapter.Host) *rig {
	t.Helper()
	base := tempDir(t)
	home := filepath.Join(base, "home")
	mkdir(t, home)
	t.Setenv("HOME", home)
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(home, "gitconfig"))
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	write(t, filepath.Join(home, "gitconfig"), "[user]\nname = Rig Fixture\nemail = rig@example.invalid\n[commit]\ngpgsign = false\n[init]\ndefaultBranch = main\n")
	repo := filepath.Join(base, "repo")
	write(t, filepath.Join(repo, "README.md"), "measured\n")
	git(t, repo, "init", "-q")
	git(t, repo, "add", ".")
	git(t, repo, "commit", "-q", "-m", "first")
	tasks, repos, err := fakehost.Fixture(filepath.Join(base, "data"), 1)
	if err != nil {
		t.Fatal(err)
	}
	record := filepath.Join(base, "record")
	mkdir(t, filepath.Join(record, "host"))
	r := &rig{t: t, base: base, exe: testBinary(t), record: record}
	r.host = fakehost.Script{Dir: filepath.Join(record, "host"), Task: filepath.Join(tasks, fakehost.FixtureID), Facts: []string{fakehost.FixtureColor},
		Events: 2, Prompt: 1000, Cached: 600, Output: 50}
	r.eva = fakeScript{Dir: record, Chats: r.host.Dir, Updates: []fakeUpdate{{Line: "1 pass: 1 ok", Outcome: update.OutcomeOK}},
		Plant: []inbox.Entry{person(h, "made-up-new-chat-1")}}
	t.Setenv(fakehost.ScriptVar, filepath.Join(base, "host.json"))
	t.Setenv(fakeVar, filepath.Join(base, "eva.json"))
	was := drive.TempFolders
	drive.TempFolders = []string{filepath.Join(base, "made-up-tmp")}
	t.Cleanup(func() { drive.TempFolders = was })
	r.opts = drive.Options{
		Tasks: tasks, Repos: repos, Host: string(h.ID), CLI: r.exe, Condition: task.Eva, Trials: 1,
		Work: filepath.Join(base, "work"), Out: filepath.Join(base, "out"),
		MaxSessions: 100, MaxTokens: 1 << 40, SessionTimeout: time.Hour,
		PassEnv: []string{fakehost.ScriptVar, fakeVar}, EvaMD: r.exe, Repo: repo, Spend: true,
	}
	return r
}

// run writes the scripts and runs the driver.
func (r *rig) run() (drive.Summary, string, error) {
	r.t.Helper()
	writeJSON(r.t, filepath.Join(r.base, "host.json"), r.host)
	writeJSON(r.t, filepath.Join(r.base, "eva.json"), r.eva)
	if r.signIn != nil {
		writeJSON(r.t, filepath.Join(r.base, "sign-in.json"), r.signIn)
	}
	var out bytes.Buffer
	s, err := drive.Run(context.Background(), r.opts, &out)
	return s, out.String(), err
}

// useSignIn gives the run a fake sign-in program, the test binary linked
// under that name, and its script.
func (r *rig) useSignIn(s signInScript) {
	r.t.Helper()
	s.Dir = r.record
	r.signIn = &s
	link := filepath.Join(r.base, "programs", "sign-in")
	if _, err := os.Lstat(link); err != nil {
		mkdir(r.t, filepath.Dir(link))
		if err := os.Symlink(r.exe, link); err != nil {
			r.t.Fatal(err)
		}
	}
	r.t.Setenv(signInVar, filepath.Join(r.base, "sign-in.json"))
	r.opts.PassEnv = append(r.opts.PassEnv, signInVar)
	SetSignIn(link)
	r.t.Cleanup(func() { SetSignIn("") })
}

// calls reads the fake eva-md's record.
func (r *rig) calls() []fakeCall {
	r.t.Helper()
	calls, err := readLines[fakeCall](filepath.Join(r.record, "eva-md.jsonl"))
	if err != nil {
		r.t.Fatal(err)
	}
	return calls
}

// signIns reads the fake sign-in program's record.
func (r *rig) signIns() []fakeCall {
	r.t.Helper()
	calls, err := readLines[fakeCall](filepath.Join(r.record, "sign-in.jsonl"))
	if err != nil {
		r.t.Fatal(err)
	}
	return calls
}

// chats reads the fake host's chats.
func (r *rig) chats() []fakehost.Call {
	r.t.Helper()
	calls, err := fakehost.Read(r.host.Dir)
	if err != nil {
		r.t.Fatal(err)
	}
	var out []fakehost.Call
	for _, c := range calls {
		if c.Mode == "start" || c.Mode == "continue" {
			out = append(out, c)
		}
	}
	return out
}

// rows reads the results folder; none is no row.
func (r *rig) rows() []results.Row {
	r.t.Helper()
	if _, err := os.Stat(filepath.Join(r.opts.Out, results.FileName)); os.IsNotExist(err) {
		return nil
	}
	rows, err := results.Load(r.opts.Out)
	if err != nil {
		r.t.Fatal(err)
	}
	return rows
}

// person is a made-up session of a person's prompt on host h.
func person(h adapter.Host, session string) inbox.Entry {
	return entry(h, session, "user", "prompt", "Please look at the made-up banner again.")
}

// entry is a made-up inbox line.
func entry(h adapter.Host, session, role, kind, text string) inbox.Entry {
	return inbox.Entry{V: 1, TS: "2026-11-13T09:00:00Z", Host: string(h.ID), Session: session, Role: role, Kind: kind, Text: text, Project: "made-up-project", Origin: "hook"}
}

// testBinary is this test binary, links resolved.
func testBinary(t *testing.T) string {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	if exe, err = filepath.EvalSymlinks(exe); err != nil {
		t.Fatal(err)
	}
	return exe
}

// tempDir is t.TempDir with its links resolved, as macOS's /var is.
func tempDir(t *testing.T) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return dir
}

func mkdir(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
}

func write(t *testing.T, path, body string) {
	t.Helper()
	mkdir(t, filepath.Dir(path))
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func writeJSON(t *testing.T, path string, v any) {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	write(t, path, string(data))
}

func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v %s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}
