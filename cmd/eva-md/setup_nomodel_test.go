package main

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/heyNag/eva-md/internal/health"
	"github.com/heyNag/eva-md/internal/host/hostfake"
	"github.com/heyNag/eva-md/internal/inbox"
	"github.com/heyNag/eva-md/internal/install"
	"github.com/heyNag/eva-md/internal/paths"
)

// Setup makes no model call (SET-K19): a whole guided setup, with the real
// install, health check and git against the fake hosts and a fake GitHub,
// starts the host CLIs only in forms known to call no model, sends Codex's
// app server only what calls none, and leaves the chats captured before it
// set aside, so no update falls due for them.

// The fake host CLIs run this test binary again: as one, it answers and
// exits here, before any test runs.
func init() { hostfake.Main() }

// modelFree are the commands of each host's CLI that call no model, by the
// first of its arguments that is no option: those that manage plugins,
// marketplaces, MCP servers and features, or ask whether the host is
// signed in, and Codex's app server, which serves what it is sent.
var modelFree = map[string][]string{
	hostfake.Claude: {"plugin", "mcp", "auth"},
	hostfake.Codex:  {"plugin", "marketplace", "features", "mcp", "login", "app-server"},
}

// appServerMethods are the methods of Codex's app server that call no
// model: the handshake, and the list of hooks the health check asks for.
var appServerMethods = []string{"initialize", "initialized", "hooks/list"}

// modelCall says why a host CLI's start may call a model, or "" when it is
// one of the forms that call none: --version alone, a command of
// modelFree, and the app server only when every method it was sent, of
// methods, is one of appServerMethods. Any other start, a headless run of
// a prompt, an interactive one or a command eva-md has not used, may.
func modelCall(c hostfake.Call, methods []string) string {
	if slices.Equal(c.Args, []string{"--version"}) {
		return ""
	}
	i := slices.IndexFunc(c.Args, func(a string) bool { return !strings.HasPrefix(a, "-") })
	switch {
	case i < 0:
		return c.CLI + " " + strings.Join(c.Args, " ") + ", which runs no command eva-md knows to call no model"
	case !slices.Contains(modelFree[c.CLI], c.Args[i]):
		return c.CLI + " " + c.Args[i] + ", which is no command eva-md knows to call no model"
	case c.Args[i] == "app-server":
		for _, m := range methods {
			if !slices.Contains(appServerMethods, m) {
				return "codex app-server sent " + m + ", which may call a model"
			}
		}
	}
	return ""
}

// The forms a model may be called in are told from what setup and install
// run: a headless run of a prompt, an interactive one, Codex's MCP server
// and an app server sent a turn may call one.
func TestHeadlessRunsAreTold(t *testing.T) {
	for _, c := range []struct {
		call    hostfake.Call
		methods []string
		model   bool
	}{
		{hostfake.Call{CLI: "claude", Args: []string{"-p", "--safe-mode", "--tools", ""}}, nil, true},
		{hostfake.Call{CLI: "claude", Args: []string{"--model", "x", "--print"}}, nil, true},
		{hostfake.Call{CLI: "claude", Args: []string{"Say hello"}}, nil, true},
		{hostfake.Call{CLI: "claude"}, nil, true},
		{hostfake.Call{CLI: "codex", Args: []string{"--no-daemon", "exec", "--ephemeral", "--json"}}, nil, true},
		{hostfake.Call{CLI: "codex", Args: []string{"e", "hello"}}, nil, true},
		{hostfake.Call{CLI: "codex", Args: []string{"mcp-server"}}, nil, true},
		{hostfake.Call{CLI: "codex", Args: []string{"--no-daemon", "app-server", "--stdio"}}, []string{"initialize", "turn/start"}, true},
		{hostfake.Call{CLI: "claude", Args: []string{"plugin", "list", "--json"}}, nil, false},
		{hostfake.Call{CLI: "claude", Args: []string{"--version"}}, nil, false},
		{hostfake.Call{CLI: "codex", Args: []string{"features", "list"}}, nil, false},
		{hostfake.Call{CLI: "codex", Args: []string{"--no-daemon", "app-server", "--stdio"}}, []string{"initialize", "initialized", "hooks/list"}, false},
		{hostfake.Call{CLI: "codex", Args: []string{"plugin", "add", "--", "exec"}}, nil, false},
	} {
		if why := modelCall(c.call, c.methods); (why != "") != c.model {
			t.Errorf("%s, sent %q, read as calling a model %v (%q), want %v", c.call, c.methods, why != "", why, c.model)
		}
	}
}

// recordAppServer puts a stand-in for the fake codex in bin that writes
// what an app server it starts is sent to a log, one request a line, with
// the shell's own commands, and hands over to the fake. It returns the
// log.
func recordAppServer(t *testing.T, bin string) string {
	t.Helper()
	log := filepath.Join(t.TempDir(), "app-server.log")
	fake := filepath.Join(bin, "codex.fake")
	if err := os.Rename(filepath.Join(bin, "codex"), fake); err != nil {
		t.Fatal(err)
	}
	script := "#!/bin/sh\ncase \" $* \" in\n*\" app-server \"*)\n" +
		"\twhile IFS= read -r line; do printf '%s\\n' \"$line\" >> '" + log + "'; printf '%s\\n' \"$line\"; done | '" + fake + "' \"$@\"\n" +
		"\texit $?\n\t;;\nesac\nexec '" + fake + "' \"$@\"\n"
	if err := os.WriteFile(filepath.Join(bin, "codex"), []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	return log
}

// sentMethods reads the methods an app server's log holds.
func sentMethods(t *testing.T, log string) []string {
	t.Helper()
	data, err := os.ReadFile(log)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	var methods []string
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		var req struct {
			Method string `json:"method"`
		}
		switch {
		case line == "":
		case json.Unmarshal([]byte(line), &req) != nil || req.Method == "":
			methods = append(methods, "a line that is no request")
		default:
			methods = append(methods, req.Method)
		}
	}
	return methods
}

// noModelSetup is a fake setup with the real install, its checks and the
// health check, against fake host CLIs that record every command they are
// given, and a codex that records what its app server is sent, git against
// a bare repo behind a fake GitHub address, and a chat captured before
// setup. It returns the fake hosts, the app server's log and the chat's
// file.
func noModelSetup(t *testing.T) (*fakeSetup, *hostfake.Fake, string, string) {
	t.Helper()
	f := newFakeSetup(t, true).ghOnGitHub(t)
	f.realGit(t)
	f.bareRepo(t, "eva-wiki")
	hosts := hostfake.New(filepath.Join(t.TempDir(), "hosts"))
	if err := hosts.WriteCLIs(f.bin); err != nil {
		t.Fatal(err)
	}
	sent := recordAppServer(t, f.bin)
	f.env.lookPath = exec.LookPath
	binary := filepath.Join(t.TempDir(), "eva-md")
	if err := os.WriteFile(binary, []byte("#!/bin/sh\nexit 0\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	f.env.executable = func() (string, error) { return binary, nil }
	f.env.install = install.Run
	f.env.preflight = func(ctx context.Context, o install.Options) (install.Result, error) {
		o.Preflight = true
		return install.Run(ctx, o)
	}
	f.env.inspect = func(ctx context.Context, d paths.Dirs) health.Report { return health.Inspect(ctx, d, true) }
	store := inbox.Store{Root: f.env.dirs.Inbox()}
	for i, text := range []string{"Plan the ledger export.", "Export it as CSV, one file a month."} {
		role, kind := "user", "prompt"
		if i == 1 {
			role, kind = "assistant", "reply"
		}
		e := inbox.Entry{V: 1, TS: time.Now().Add(-time.Hour).Format(time.RFC3339), Host: "codex", Session: "before-setup", Turn: "a",
			Role: role, Kind: kind, Text: text, Project: "general", Branch: "default", Cwd: "project", Model: "fixture-model", Origin: "hook"}
		if err := store.Append(e); err != nil {
			t.Fatal(err)
		}
	}
	file := store.Path("general", "codex", "before-setup")
	if pending(t, store, file) == 0 {
		t.Fatal("the chat captured before setup has nothing pending before setup runs")
	}
	return f, hosts, sent, file
}

// pending is how many bytes of a captured chat the update has yet to
// process.
func pending(t *testing.T, store inbox.Store, file string) int64 {
	t.Helper()
	info, err := os.Stat(file)
	if err != nil {
		t.Fatal(err)
	}
	m, err := store.Mark(file)
	if err != nil {
		t.Fatal(err)
	}
	return m.Pending(info.Size())
}

// modelRuns are the starts of a host CLI that may have called a model, each
// with why, of all those the fake hosts were given, and the methods the
// app server was sent, from its log at sent.
func modelRuns(t *testing.T, hosts *hostfake.Fake, sent string) (calls, runs, methods []string) {
	t.Helper()
	all, err := hosts.Calls()
	if err != nil {
		t.Fatal(err)
	}
	methods = sentMethods(t, sent)
	for _, c := range all {
		calls = append(calls, c.String())
		if why := modelCall(c, methods); why != "" {
			runs = append(runs, c.String()+": "+why)
		}
	}
	return calls, runs, methods
}

// A whole guided setup, which installs the plugins for both hosts and runs
// the health check, starts the host CLIs, but only in forms that call no
// model; and the chat captured before it is set aside, so no update falls
// due for it.
func TestSetupMakesNoModelCall(t *testing.T) {
	f, hosts, sent, chat := noModelSetup(t)
	code, out, errOut := f.run("~/projects/wiki\ngit@github.com:me/eva-wiki.git\ny\n")
	if code != 0 || !strings.Contains(out, "Setup will create a new wiki in this folder and push it, then install the plugins.") {
		t.Fatalf("setup: %d\n%s\n%s", code, out, errOut)
	}
	calls, runs, methods := modelRuns(t, hosts, sent)
	if len(runs) > 0 {
		t.Errorf("setup started a host in a form that may call a model: %q", runs)
	}
	// Setup did start each host: install registered the plugins, and the
	// health check asked about them.
	for _, want := range []string{"claude plugin install", "codex plugin add", "codex features list"} {
		if !slices.ContainsFunc(calls, func(c string) bool { return strings.HasPrefix(c, want) }) {
			t.Errorf("no host CLI was started as %q, so setup did not run whole: %q", want, calls)
		}
	}
	// An app server it started was heard: what it was sent is in the log.
	if slices.ContainsFunc(calls, func(c string) bool { return strings.Contains(c, " app-server") }) && !slices.Contains(methods, "initialize") {
		t.Errorf("setup started codex's app server, but its log holds %q", methods)
	}
	if n := pending(t, inbox.Store{Root: f.env.dirs.Inbox()}, chat); n != 0 {
		t.Errorf("setup left %d bytes of a chat captured before it for an update", n)
	}
	if _, err := os.Stat(filepath.Join(f.env.dirs.Inbox(), ".baseline")); err != nil {
		t.Errorf("setup recorded no baseline: %v", err)
	}
}

// The check fails a setup that starts a host in a form that may call a
// model: here the health check it runs at the end starts, through the fake
// hosts, claude -p, claude with a prompt to answer, codex's MCP server, or
// codex's app server sent a turn, as a health check that made a model call
// would.
func TestASetupThatRunsAPromptIsFound(t *testing.T) {
	for _, c := range []struct {
		name  string
		start func(ctx context.Context)
		want  string
	}{
		{"a headless prompt", func(ctx context.Context) { _ = exec.CommandContext(ctx, "claude", "-p", "Say hello").Run() }, "claude -p Say hello: "},
		{"an interactive prompt", func(ctx context.Context) { _ = exec.CommandContext(ctx, "claude", "Say hello").Run() }, "claude Say hello: "},
		{"codex's MCP server", func(ctx context.Context) { _ = exec.CommandContext(ctx, "codex", "mcp-server").Run() }, "codex mcp-server: "},
		{"a turn sent to the app server", func(ctx context.Context) {
			cmd := exec.CommandContext(ctx, "codex", "--no-daemon", "app-server", "--stdio")
			cmd.Stdin = strings.NewReader(`{"id":1,"method":"initialize","params":{}}` + "\n" + `{"id":2,"method":"turn/start","params":{"input":"Say hello"}}` + "\n")
			_ = cmd.Run()
		}, "codex --no-daemon app-server --stdio: codex app-server sent turn/start, which may call a model"},
	} {
		t.Run(c.name, func(t *testing.T) {
			f, hosts, sent, _ := noModelSetup(t)
			inspect := f.env.inspect
			f.env.inspect = func(ctx context.Context, d paths.Dirs) health.Report {
				c.start(ctx)
				return inspect(ctx, d)
			}
			if code, out, errOut := f.run("~/projects/wiki\ngit@github.com:me/eva-wiki.git\ny\n"); code != 0 {
				t.Fatalf("setup: %d\n%s\n%s", code, out, errOut)
			}
			_, runs, _ := modelRuns(t, hosts, sent)
			if !slices.ContainsFunc(runs, func(r string) bool { return strings.HasPrefix(r, c.want) }) {
				t.Errorf("the check found %q, not the seeded %q", runs, c.want)
			}
			// Nothing setup itself started is found.
			for _, r := range runs {
				if !strings.HasPrefix(r, c.want) && !strings.Contains(c.want, " app-server ") {
					t.Errorf("the check found %q beside the seeded start", r)
				}
			}
		})
	}
}
