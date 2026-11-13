package modelrun

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/heyNag/eva-md/internal/host/adapter"
)

// signalPuppet is the puppet's one signal of its own.
const signalPuppet Signal = "puppet_signal"

// puppet is this package's test backend, run on the rig's script. It reads
// what the script printed on stdout, one JSON event a line, and passes on
// every word it read, with no care: an event's or an item's type, an error's
// text and the answer file whatever happened. So what reaches an Outcome is
// what Run lets through, and nothing a backend chose.
//
// Its events: {"type":"usage","in":N,"cached":N,"out":N}, {"type":"model",
// "name":M}, {"type":"answered","name":M}, {"type":"tool"},
// {"type":"class","class":C}, {"type":"refused","what":W},
// {"type":"signal","name":S}, {"type":"again"} and {"type":"item",
// "item":{"type":T}}. Any other type makes the run host. Its command line is
// run --system S [--model M] [--effort E] [--again] -o FILE -, the input on
// stdin, and its probe step is features list.
func puppet() backend {
	return backend{
		efforts: []string{"low", "medium", "high"},
		signals: []Signal{signalPuppet},
		probe:   puppetProbe,
		attempt: puppetAttempt,
	}
}

// puppetFallback is what the puppet's probe step gives when it can't read
// the list.
var puppetFallback = []string{"fallback"}

func puppetProbe(ctx context.Context, run probeRun) ([]string, bool) {
	out, err := run(ctx, "features", "list")
	if err != nil {
		return puppetFallback, false
	}
	var names []string
	for _, line := range strings.Split(string(out), "\n") {
		if f := strings.Fields(line); len(f) >= 2 && (f[len(f)-1] == "true" || f[len(f)-1] == "false") {
			names = append(names, f[0])
		}
	}
	if len(names) == 0 {
		return puppetFallback, false
	}
	return names, true
}

func puppetAttempt(ctx context.Context, a *attempt) (result, error) {
	dir, err := os.MkdirTemp("", "eva-md-puppet-")
	if err != nil {
		return result{}, err
	}
	defer os.RemoveAll(dir)
	file := filepath.Join(dir, "answer.json")
	args := []string{"run", "--system", a.call.System}
	if a.call.Model != "" {
		args = append(args, "--model", a.call.Model)
	}
	if a.call.Effort != "" {
		args = append(args, "--effort", a.call.Effort)
	}
	if a.n > 1 {
		args = append(args, "--again")
	}
	args = append(args, "-o", file, "-")
	out := &capped{limit: 1 << 20}
	e, err := a.run(ctx, args, a.call.Input, out)
	if err != nil {
		return result{}, err
	}
	res := result{class: ClassOK}
	res.answer, _ = os.ReadFile(file)
	if e.failed {
		res.class, res.refused = ClassHost, string(e.stderr)
		res.signals = append(res.signals, Signal(e.stderr))
	}
	s := bufio.NewScanner(bytes.NewReader(out.Bytes()))
	for s.Scan() {
		var ev struct {
			Type, Name, Class, What string
			In, Cached, Out         int64
			Item                    struct{ Type string }
		}
		if json.Unmarshal(s.Bytes(), &ev) != nil {
			res.signals = append(res.signals, Signal(s.Text()))
			continue
		}
		switch ev.Type {
		case "usage":
			res.usage.In += ev.In
			res.usage.CachedIn += ev.Cached
			res.usage.Out += ev.Out
			res.usageSeen = true
		case "model":
			res.models = append(res.models, ev.Name)
		case "answered":
			res.answered = ev.Name
		case "tool":
			res.toolSign = true
		case "class":
			res.class = Class(ev.Class)
		case "refused":
			res.refused = ev.What
		case "signal":
			res.signals = append(res.signals, Signal(ev.Name))
		case "again":
			res.again = true
		case "item":
			res.signals = append(res.signals, Signal(ev.Item.Type))
			res.models = append(res.models, ev.Item.Type)
		default:
			res.class, res.refused = ClassHost, ev.Type
			res.signals = append(res.signals, Signal(ev.Type))
			res.answered = ev.Type
		}
	}
	return res, nil
}

// puppetRunner is a runner on the puppet for host id, its probes through a
// counting Env.
func puppetRunner(t *testing.T, id adapter.ID, o Options) (*runner, *probeCalls) {
	t.Helper()
	calls, env := testEnv()
	if o.Env.LookPath != nil {
		env.LookPath = o.Env.LookPath
	}
	o.Env = env
	r, err := newRunner(id, puppet(), o)
	if err != nil {
		t.Fatal(err)
	}
	return r, calls
}

// puppetHost readies the fake to answer as the puppet's host would, cleanly.
func puppetHost(t *testing.T) string {
	t.Helper()
	dump := fakeCLI(t, "")
	put(t, dump, "output", answer)
	put(t, dump, "stdout", `{"type":"usage","in":120,"cached":30,"out":40}
{"type":"answered","name":"model-that-answered"}
`)
	return dump
}

// events joins the puppet's events into its stdout.
func events(lines ...string) string { return strings.Join(lines, "\n") + "\n" }

func TestAPuppetRunIsOK(t *testing.T) {
	for _, id := range hostIDs {
		t.Run(string(id), func(t *testing.T) {
			isolate(t)
			dump := puppetHost(t)
			r, _ := puppetRunner(t, id, Options{})
			c := call(t, id)
			c.Effort = "medium"
			out, err := r.Run(context.Background(), c)
			if err != nil {
				t.Fatal(err)
			}
			cli, err := filepath.EvalSymlinks(filepath.Join(fakeBin, cliName(t, id)))
			if err != nil {
				t.Fatal(err)
			}
			want := Outcome{
				Class: ClassOK, Answer: []byte(answer),
				ModelAsked: c.Model, ModelAnswered: "model-that-answered", ModelsSeen: []string{"model-that-answered"},
				Effort: "medium", CLI: cli, CLIVersion: "0.1.0", Billing: BillingPlan,
				Usage: Usage{In: 120, CachedIn: 30, Out: 40}, UsageSeen: true, Attempts: 1,
			}
			if fmt.Sprintf("%#v", out) != fmt.Sprintf("%#v", want) {
				t.Fatalf("outcome\n got %#v\nwant %#v", out, want)
			}
			if got := argsOf(t, dump, 1); !slices.Equal(got[:6], []string{"run", "--system", c.System, "--model", c.Model, "--effort"}) {
				t.Fatalf("arguments %q", got)
			}
			// The run folder is the child's, and is left empty.
			if pwd, err := filepath.EvalSymlinks(c.RunDir); err != nil || strings.TrimSpace(read(t, dump, "pwd.1")) != pwd {
				t.Fatalf("the child ran in %q, not the run folder %q (%v)", read(t, dump, "pwd.1"), pwd, err)
			}
			if names, err := os.ReadDir(c.RunDir); err != nil || len(names) != 0 {
				t.Fatalf("the run folder holds %v (%v)", names, err)
			}
		})
	}
}

func TestChildGetsOnlyTheClearedEnvironment(t *testing.T) {
	for _, id := range hostIDs {
		t.Run(string(id), func(t *testing.T) {
			isolate(t)
			dump := puppetHost(t)
			home := os.Getenv("HOME")
			set := map[string]string{
				"ANTHROPIC_API_KEY": "fake-anthropic-value", "OPENAI_API_KEY": "fake-openai-value",
				"CLAUDECODE": "1", "CLAUDE_CODE_ENTRYPOINT": "cli", "CLAUDE_CODE_SSE_PORT": "1234",
				"CLAUDE_CODE_DISABLE_AUTO_MEMORY": "0", "EVA_MD_INNER": "0", "TERM": "xterm-256color",
				"HTTPS_PROXY": "http://proxy.example.invalid:8080", "https_proxy": "http://proxy.example.invalid:8080",
				"ALL_PROXY": "socks5://proxy.example.invalid:1080", "all_proxy": "socks5://proxy.example.invalid:1080",
				"NO_PROXY": "localhost,.example.invalid", "CODEX_HOME": filepath.Join(home, ".codex-test"),
				"CLAUDE_CONFIG_DIR": filepath.Join(home, ".claude-test"),
				"SSL_CERT_FILE":     "/etc/ssl/cert.pem", "SSL_CERT_DIR": "/etc/ssl/certs", "NODE_EXTRA_CA_CERTS": "/etc/ssl/custom-ca.pem",
				"CODEX_CA_CERTIFICATE": "/etc/ssl/custom-ca.pem", "LC_CTYPE": "UTF-8", "UNRELATED_SETTING": "dropped",
			}
			for k, v := range set {
				t.Setenv(k, v)
			}
			r, _ := puppetRunner(t, id, Options{})
			if out, err := r.Run(context.Background(), call(t, id)); err != nil || out.Class != ClassOK {
				t.Fatalf("run failed: %+v %v", out, err)
			}
			env := envOf(t, dump, 1)
			for _, key := range []string{"ANTHROPIC_API_KEY", "OPENAI_API_KEY", "CLAUDECODE", "UNRELATED_SETTING", "GIT_CONFIG_GLOBAL", "GIT_CONFIG_NOSYSTEM"} {
				if _, ok := env[key]; ok {
					t.Errorf("%s reached the child", key)
				}
			}
			for key := range env {
				if strings.HasPrefix(key, "CLAUDE_CODE_") && key != "CLAUDE_CODE_DISABLE_AUTO_MEMORY" {
					t.Errorf("%s reached the child", key)
				}
			}
			want := map[string]string{
				"HTTPS_PROXY": set["HTTPS_PROXY"], "https_proxy": set["https_proxy"], "NO_PROXY": set["NO_PROXY"],
				"ALL_PROXY": set["ALL_PROXY"], "all_proxy": set["all_proxy"],
				"SSL_CERT_DIR": set["SSL_CERT_DIR"], "NODE_EXTRA_CA_CERTS": set["NODE_EXTRA_CA_CERTS"], "CODEX_CA_CERTIFICATE": set["CODEX_CA_CERTIFICATE"],
				"CODEX_HOME": set["CODEX_HOME"], "CLAUDE_CONFIG_DIR": set["CLAUDE_CONFIG_DIR"], "SSL_CERT_FILE": set["SSL_CERT_FILE"], "LC_CTYPE": "UTF-8",
				"HOME": home, "PATH": os.Getenv("PATH"), "TMPDIR": os.Getenv("TMPDIR"),
				"TERM": "dumb", "EVA_MD_INNER": "1", "CLAUDE_CODE_DISABLE_AUTO_MEMORY": "1",
			}
			for key, value := range want {
				if env[key] != value {
					t.Errorf("%s = %q, want %q", key, env[key], value)
				}
			}
		})
	}
}

func TestInputGoesOnStdinOnly(t *testing.T) {
	isolate(t)
	dump := puppetHost(t)
	r, _ := puppetRunner(t, adapter.Codex, Options{})
	c := call(t, adapter.Codex)
	if _, err := r.Run(context.Background(), c); err != nil {
		t.Fatal(err)
	}
	if got := read(t, dump, "stdin.1"); got != string(c.Input) {
		t.Fatalf("stdin %q, want %q", got, c.Input)
	}
	for _, arg := range argsOf(t, dump, 1) {
		if strings.Contains(arg, batchMarker) {
			t.Fatalf("argument %q carries the input", arg)
		}
	}
}

// A run whose time ran out before its CLI started, as a retry left with
// none of the call's deadline, is a timeout like one that ran out later, not
// an error, and its CLI never starts.
func TestTimeoutBeforeTheCLIStartsIsATimeout(t *testing.T) {
	isolate(t)
	dump := puppetHost(t)
	r, _ := puppetRunner(t, adapter.Codex, Options{})
	ctx, timeouts := holdTimeouts(context.Background())
	timeouts.expire()
	out, err := await(t, launch(t, ctx, r, call(t, adapter.Codex)), "a run whose time ran out before its CLI started")
	if err != nil || out.Class != ClassTimeout || out.Answer != nil || out.Attempts != 0 {
		t.Fatalf("outcome %+v %v, want a timeout", out, err)
	}
	if n := runs(t, dump); n != 0 {
		t.Fatalf("the CLI ran %d times", n)
	}
}

// A timeout kills the CLI and every process it started, and is not retried.
// The test ends the timeout once the fake has started its child, so the
// machine's speed decides nothing; TestTimeoutEndsTheRunAtItsDeadline lets
// a timeout's whole time pass.
func TestTimeoutKillsTheChildAndItsGroup(t *testing.T) {
	isolate(t)
	dump := fakeCLI(t, startChild+"exec sleep 300")
	put(t, dump, "output", answer)
	put(t, dump, "stdout", events(`{"type":"again"}`))
	r, _ := puppetRunner(t, adapter.Codex, Options{})
	// The wait for output past the CLI's end is longer than the test waits:
	// only a kill of the whole group, the child that holds the output too,
	// ends the run in time.
	r.waitDelay = 3 * hangGuard
	c := call(t, adapter.Codex)
	c.Deadline = 1500 * time.Millisecond
	ctx, timeouts := holdTimeouts(context.Background())
	done := launch(t, ctx, r, c)
	if d := timeouts.next(t); d != c.Deadline {
		t.Fatalf("the run's limit is %s, want %s", d, c.Deadline)
	}
	if !waitFile(dump, "child") {
		t.Fatalf("the fake started no child in %s", hangGuard)
	}
	timeouts.expire()
	out, err := await(t, done, "a run whose timeout ended")
	if err != nil {
		t.Fatal(err)
	}
	if out.Class != ClassTimeout || out.Answer != nil || out.Attempts != 1 {
		t.Fatalf("outcome %+v, want a timeout", out)
	}
	if runs(t, dump) != 1 {
		t.Fatal("a timeout must not be retried")
	}
	waitGone(t, dump, "child")
}

// A run ends when its deadline has passed, not before, and doesn't wait for
// the CLI, which would go on for minutes. The test starts the clock once the
// fake runs: the run sets its timer before it starts the CLI.
func TestTimeoutEndsTheRunAtItsDeadline(t *testing.T) {
	isolate(t)
	dump := fakeCLI(t, `: > "$dump/started"; exec sleep 300`)
	put(t, dump, "output", answer)
	r, _ := puppetRunner(t, adapter.Codex, Options{})
	c := call(t, adapter.Codex)
	c.Deadline = 300 * time.Millisecond
	ctx, timeouts := holdTimeouts(context.Background())
	done := launch(t, ctx, r, c)
	d := timeouts.next(t)
	if d != c.Deadline {
		t.Fatalf("the run's limit is %s, want %s", d, c.Deadline)
	}
	if !waitFile(dump, "started") {
		t.Fatalf("the fake didn't start in %s", hangGuard)
	}
	began := time.Now()
	time.AfterFunc(d, timeouts.expire)
	out, err := await(t, done, "a run past its deadline")
	elapsed := time.Since(began)
	if err != nil || out.Class != ClassTimeout || out.Answer != nil {
		t.Fatalf("outcome %+v %v, want a timeout", out, err)
	}
	if elapsed < d {
		t.Fatalf("the run ended %s after the fake started, before its %s deadline", elapsed, d)
	}
}

// A call with no deadline gets 600 s for all its attempts.
func TestNoDeadlineIs600Seconds(t *testing.T) {
	isolate(t)
	puppetHost(t)
	r, _ := puppetRunner(t, adapter.Codex, Options{})
	ctx, timeouts := holdTimeouts(context.Background())
	done := launch(t, ctx, r, call(t, adapter.Codex))
	if d := timeouts.next(t); d != 600*time.Second {
		t.Fatalf("the run's limit is %s, want 600s", d)
	}
	if out, err := await(t, done, "a run with no deadline"); err != nil || out.Class != ClassOK {
		t.Fatalf("outcome %+v %v", out, err)
	}
}

// The attempts share the call's one deadline: the retry a first attempt asks
// for doesn't get a fresh one, and once the deadline has passed no retry
// starts. A call makes two attempts at most. The test ends the time limits
// itself, so the machine's speed decides nothing.
func TestAttemptsShareTheDeadline(t *testing.T) {
	isolate(t)
	// The first attempt asks for another at once, and the second runs until
	// its time ends; only a third would succeed.
	dump := fakeCLI(t, `if [ "$n" = 2 ]; then : > "$dump/second"; exec sleep 300; fi`)
	put(t, dump, "stdout.1", events(`{"type":"usage","in":5,"cached":1,"out":2}`, `{"type":"again"}`))
	put(t, dump, "exit.1", "1")
	put(t, dump, "output", answer)
	r, _ := puppetRunner(t, adapter.Codex, Options{})
	c := call(t, adapter.Codex)
	c.Deadline = time.Minute
	ctx, timeouts := holdTimeouts(context.Background())
	done := launch(t, ctx, r, c)
	if first, second := timeouts.next(t), timeouts.next(t); first != c.Deadline || second >= first {
		t.Fatalf("attempts' limits %s then %s, want %s then less", first, second, c.Deadline)
	}
	if !waitFile(dump, "second") {
		t.Fatalf("the second attempt didn't start in %s", hangGuard)
	}
	timeouts.expire()
	out, err := await(t, done, "a second attempt whose time ended")
	if err != nil {
		t.Fatal(err)
	}
	if n := runs(t, dump); n != 2 {
		t.Fatalf("ran %d times, want 2", n)
	}
	if !slices.Contains(argsOf(t, dump, 2), "--again") || slices.Contains(argsOf(t, dump, 1), "--again") {
		t.Fatalf("the attempts' numbers did not reach the backend: %q, %q", argsOf(t, dump, 1), argsOf(t, dump, 2))
	}
	if out.Class != ClassTimeout || out.Attempts != 2 || out.Answer != nil || out.Usage != (Usage{5, 1, 2}) {
		t.Fatalf("outcome %+v, want a timeout on the second attempt with the first's usage", out)
	}
	// A deadline of a nanosecond has passed by the time the first attempt
	// asks for another; the held limit let that attempt finish, and no
	// second one starts.
	for _, file := range []string{"count", "extra"} {
		if err := os.Remove(filepath.Join(dump, file)); err != nil {
			t.Fatal(err)
		}
	}
	c = call(t, adapter.Codex)
	c.Deadline = time.Nanosecond
	ctx, _ = holdTimeouts(context.Background())
	out, err = await(t, launch(t, ctx, r, c), "a call whose deadline passed in its first attempt")
	if err != nil || out.Class != ClassTimeout || out.Attempts != 1 || out.Answer != nil {
		t.Fatalf("outcome %+v %v, want a timeout after the first attempt", out, err)
	}
	if n := runs(t, dump); n != 1 {
		t.Fatalf("ran %d times with the deadline passed, want 1", n)
	}
	// Two attempts that each ask for another end at the second, as it read.
	if err := os.Remove(filepath.Join(dump, "count")); err != nil {
		t.Fatal(err)
	}
	put(t, dump, "stdout", events(`{"type":"again"}`, `{"type":"class","class":"auth"}`))
	put(t, dump, "exit.1", "0")
	out, err = r.Run(context.Background(), call(t, adapter.Codex))
	if err != nil || out.Class != ClassAuth || out.Attempts != 2 || runs(t, dump) != 2 {
		t.Fatalf("outcome %+v %v after %d runs, want auth after 2", out, err, runs(t, dump))
	}
}

func TestCanceledRunIsAnErrorAndEndsTheChild(t *testing.T) {
	isolate(t)
	dump := fakeCLI(t, startChild+"exec sleep 300")
	r, _ := puppetRunner(t, adapter.Codex, Options{})
	r.waitDelay = 3 * hangGuard // as in TestTimeoutKillsTheChildAndItsGroup
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := launch(t, ctx, r, call(t, adapter.Codex))
	if !waitFile(dump, "child") {
		t.Fatalf("the fake started no child in %s", hangGuard)
	}
	cancel()
	if _, err := await(t, done, "a canceled run"); !errors.Is(err, context.Canceled) {
		t.Fatalf("error %v, want context.Canceled", err)
	}
	waitGone(t, dump, "child")
	// A call whose context has ended starts nothing.
	r2, calls := puppetRunner(t, adapter.Codex, Options{})
	if _, err := r2.Run(ctx, call(t, adapter.Codex)); !errors.Is(err, context.Canceled) || calls.total() != 0 || runs(t, dump) != 1 {
		t.Fatalf("a call with its context ended: %v, %d probe commands, %d runs", err, calls.total(), runs(t, dump))
	}
}

// A CLI that exits cleanly, leaving a grandchild that holds its output open,
// has not failed: the exit status is the CLI's, not Wait's, which ends
// waiting for the output with an error. The run's output wait is short, and
// given to that run alone; the grandchild is killed with the group.
func TestACleanExitThatLeavesAGrandchildIsNotFailed(t *testing.T) {
	isolate(t)
	dump := fakeCLI(t, startChild)
	put(t, dump, "output", answer)
	r, _ := puppetRunner(t, adapter.Codex, Options{})
	r.waitDelay = 100 * time.Millisecond
	out, err := await(t, launch(t, context.Background(), r, call(t, adapter.Codex)), "a run whose grandchild holds its output")
	if err != nil || out.Class != ClassOK || string(out.Answer) != answer {
		t.Fatalf("outcome %+v %v, want ok", out, err)
	}
	waitGone(t, dump, "child")
}

func TestMissingCLIIsCLIMissing(t *testing.T) {
	for _, id := range hostIDs {
		t.Run(string(id), func(t *testing.T) {
			isolate(t)
			t.Setenv("PATH", t.TempDir())
			r, _ := puppetRunner(t, id, Options{})
			c := call(t, id)
			out, err := r.Run(context.Background(), c)
			want := Outcome{Class: ClassCLIMissing, ModelAsked: c.Model, Billing: BillingPlan}
			if err != nil || fmt.Sprintf("%#v", out) != fmt.Sprintf("%#v", want) {
				t.Fatalf("outcome %#v %v, want %#v", out, err, want)
			}
		})
	}
}

// A run starts the CLI by the path install recorded, though PATH lacks it,
// as an app's may, and adds the CLI's folder to the end of the child's PATH,
// where an npm install puts the node its #! line names. A recorded path that
// is gone, or names another program, gives way to PATH, and the child's PATH
// is left as it was.
func TestRecordedCLIPathIsPreferred(t *testing.T) {
	for _, id := range hostIDs {
		t.Run(string(id), func(t *testing.T) {
			isolate(t)
			dump := puppetHost(t)
			name := cliName(t, id)
			bin, err := filepath.EvalSymlinks(fakeBin)
			if err != nil {
				t.Fatal(err)
			}
			t.Setenv("PATH", "/usr/bin:/bin")
			r, _ := puppetRunner(t, id, Options{CLI: filepath.Join(fakeBin, name)})
			out, err := r.Run(context.Background(), call(t, id))
			if err != nil || out.Class != ClassOK || runs(t, dump) != 1 || out.CLI != filepath.Join(bin, name) {
				t.Fatalf("recorded path: %+v %v", out, err)
			}
			if path := envOf(t, dump, 1)["PATH"]; path != "/usr/bin:/bin:"+bin {
				t.Fatalf("child PATH %q", path)
			}
			t.Setenv("PATH", t.TempDir())
			r, _ = puppetRunner(t, id, Options{CLI: filepath.Join(t.TempDir(), name)})
			if out, err := r.Run(context.Background(), call(t, id)); err != nil || out.Class != ClassCLIMissing || runs(t, dump) != 1 {
				t.Fatalf("a recorded path that is gone, with no CLI on PATH: %+v %v", out, err)
			}
			t.Setenv("PATH", fakeBin+":/usr/bin:/bin")
			plain := filepath.Join(t.TempDir(), name)
			if err := os.WriteFile(plain, []byte("#!/bin/sh\nexit 3\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			for i, recorded := range []string{filepath.Join(t.TempDir(), name), "/bin/sh", name, plain, "bin/" + name} {
				r, _ = puppetRunner(t, id, Options{CLI: recorded})
				out, err := r.Run(context.Background(), call(t, id))
				if err != nil || out.Class != ClassOK || runs(t, dump) != i+2 || out.CLI != filepath.Join(bin, name) {
					t.Fatalf("recorded %q: %+v %v", recorded, out, err)
				}
				if path := envOf(t, dump, i+2)["PATH"]; path != fakeBin+":/usr/bin:/bin" {
					t.Fatalf("recorded %q: child PATH %q", recorded, path)
				}
			}
		})
	}
}

func TestCallsThatCannotRunAreRefused(t *testing.T) {
	isolate(t)
	dump := puppetHost(t)
	notEmpty := t.TempDir()
	put(t, notEmpty, "AGENTS.md", "planted instructions")
	file := filepath.Join(notEmpty, "AGENTS.md")
	cases := map[string]func(*Call){
		"no task":                        func(c *Call) { c.Task = "" },
		"another task":                   func(c *Call) { c.Task = "summarize" },
		"a task in another case":         func(c *Call) { c.Task = "Extract" },
		"model read as a flag":           func(c *Call) { c.Model = "--dangerously-skip-permissions" },
		"model with a space":             func(c *Call) { c.Model = "claude test" },
		"effort the backend lacks":       func(c *Call) { c.Effort = "max" },
		"v1's highest":                   func(c *Call) { c.Effort = "highest" },
		"effort in another case":         func(c *Call) { c.Effort = "Low" },
		"relative run folder":            func(c *Call) { c.RunDir = "runs/1" },
		"no run folder":                  func(c *Call) { c.RunDir = "" },
		"missing run folder":             func(c *Call) { c.RunDir = filepath.Join(notEmpty, "absent") },
		"run folder not empty":           func(c *Call) { c.RunDir = notEmpty },
		"run folder is a file":           func(c *Call) { c.RunDir = file },
		"instructions start with a dash": func(c *Call) { c.System = "--verbose" },
		"instructions with a NUL byte":   func(c *Call) { c.System = "a\x00b" },
		"empty instructions":             func(c *Call) { c.System = "" },
		"blank instructions":             func(c *Call) { c.System = " \n" },
		"empty input":                    func(c *Call) { c.Input = nil },
		"blank input":                    func(c *Call) { c.Input = []byte("\n\t ") },
		"schema not an object":           func(c *Call) { c.Schema = []byte(`[1]`) },
		"schema not JSON":                func(c *Call) { c.Schema = []byte(`{"type":`) },
		"no schema":                      func(c *Call) { c.Schema = nil },
		"negative deadline":              func(c *Call) { c.Deadline = -time.Second },
	}
	r, calls := puppetRunner(t, adapter.Codex, Options{})
	for name, change := range cases {
		c := call(t, adapter.Codex)
		change(&c)
		if out, err := r.Run(context.Background(), c); err == nil {
			t.Errorf("%s: ran with outcome %+v", name, out)
		} else if strings.Contains(err.Error(), notEmpty) {
			t.Errorf("%s: the error names the run folder: %v", name, err)
		}
	}
	if n := runs(t, dump); n != 0 {
		t.Fatalf("the CLI started %d times for refused calls", n)
	}
	if n := calls.total(); n != 0 {
		t.Fatalf("refused calls ran %d probe commands", n)
	}
	// Each task, and each effort of the backend, runs.
	for _, task := range tasks {
		for _, effort := range append([]string{""}, puppet().efforts...) {
			c := call(t, adapter.Codex)
			c.Task, c.Effort = task, effort
			if out, err := r.Run(context.Background(), c); err != nil || out.Class != ClassOK || out.Effort != effort {
				t.Fatalf("task %s, effort %q: %+v %v", task, effort, out, err)
			}
		}
	}
}

// A host the adapter lacks has no runner; nor has one without a backend.
// Registering a backend for such a host, a second one for a host, or one
// that can't run panics.
func TestNewAndRegister(t *testing.T) {
	isolate(t)
	if r, err := New("not-a-host", Options{}); err == nil || r != nil {
		t.Fatalf("New for a host the adapter lacks: %v %v", r, err)
	}
	if r, err := newRunner("not-a-host", puppet(), Options{}); err == nil || r != nil {
		t.Fatalf("newRunner for a host the adapter lacks: %v %v", r, err)
	}
	saved := backends
	t.Cleanup(func() { backends = saved })
	backends = map[adapter.ID]backend{}
	if r, err := New(adapter.Codex, Options{}); err == nil || r != nil {
		t.Fatalf("New for a host with no backend: %v %v", r, err)
	}
	register(adapter.Codex, puppet())
	if r, err := New(adapter.Codex, Options{}); err != nil || r == nil {
		t.Fatalf("New for a registered host: %v %v", r, err)
	}
	bad := puppet()
	bad.signals = []Signal{"Not a name"}
	noAttempt := puppet()
	noAttempt.attempt = nil
	flagEffort := puppet()
	flagEffort.efforts = []string{"--max"}
	for name, f := range map[string]func(){
		"a second backend":         func() { register(adapter.Codex, puppet()) },
		"a host the adapter lacks": func() { register("not-a-host", puppet()) },
		"a signal that is text":    func() { register(adapter.ClaudeCode, bad) },
		"no attempt":               func() { register(adapter.ClaudeCode, noAttempt) },
		"an effort like a flag":    func() { register(adapter.ClaudeCode, flagEffort) },
	} {
		if !panics(f) {
			t.Errorf("%s did not panic", name)
		}
	}
	if _, ok := backends[adapter.ClaudeCode]; ok {
		t.Fatal("an invalid backend was registered")
	}
	if _, err := newRunner(adapter.ClaudeCode, bad, Options{}); err == nil {
		t.Fatal("newRunner took a backend with a signal that is text")
	}
}

func panics(f func()) (p bool) {
	defer func() { p = recover() != nil }()
	f()
	return false
}

// What Run enforces on every backend (SPEC.md, section 4), each rule run
// through the puppet, which passes on whatever it read.

// runPuppet runs one call on the puppet with the fake readied by files, a
// map of the dump's file names to their content, and gives the outcome.
func runPuppet(t *testing.T, files map[string]string) Outcome {
	t.Helper()
	isolate(t)
	dump := fakeCLI(t, "")
	for name, content := range files {
		put(t, dump, name, content)
	}
	r, _ := puppetRunner(t, adapter.Codex, Options{})
	out, err := r.Run(context.Background(), call(t, adapter.Codex))
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// A sign of a tool makes the run tool_used, beside a valid answer and
// whatever the backend read as the class, and keeps no answer.
func TestAToolSignIsToolUsedBesideAnyAnswer(t *testing.T) {
	for name, files := range map[string]map[string]string{
		"a tool beside a valid answer": {"output": answer, "stdout": events(`{"type":"tool"}`)},
		"a tool beside auth":           {"output": answer, "stdout": events(`{"type":"tool"}`, `{"type":"class","class":"auth"}`)},
		"a tool on a failed exit":      {"output": answer, "stdout": events(`{"type":"tool"}`), "exit": "1"},
		"the class tool_used alone":    {"output": answer, "stdout": events(`{"type":"class","class":"tool_used"}`)},
	} {
		t.Run(name, func(t *testing.T) {
			if out := runPuppet(t, files); out.Class != ClassToolUsed || out.Answer != nil {
				t.Fatalf("outcome %+v, want tool_used and no answer", out)
			}
		})
	}
}

// ok needs an answer that is one JSON object; otherwise the run is
// answer_invalid, with no answer.
func TestOKNeedsOneJSONObject(t *testing.T) {
	for _, bad := range []string{"", "[1]", `"done"`, `{"summary":"done"} {"summary":"again"}`, `{"summary":`, "null", `{"summary":"done"}x`} {
		t.Run(bad, func(t *testing.T) {
			files := map[string]string{"stdout": events(`{"type":"answered","name":"m"}`)}
			if bad != "" {
				files["output"] = bad
			}
			if out := runPuppet(t, files); out.Class != ClassAnswerInvalid || out.Answer != nil {
				t.Fatalf("answer %q: outcome %+v, want answer_invalid", bad, out)
			}
		})
	}
	t.Run("space around", func(t *testing.T) {
		if out := runPuppet(t, map[string]string{"output": " \n" + answer + "\n"}); out.Class != ClassOK || string(out.Answer) != answer {
			t.Fatalf("an answer with space around it: %+v", out)
		}
	})
}

// An answer is kept only on ok, whatever the backend kept.
func TestTheAnswerIsKeptOnlyOnOK(t *testing.T) {
	for _, class := range []Class{ClassAuth, ClassRateLimit, ClassHost, ClassAnswerInvalid} {
		t.Run(string(class), func(t *testing.T) {
			out := runPuppet(t, map[string]string{"output": answer, "stdout": events(`{"type":"class","class":"` + string(class) + `"}`)})
			if out.Class != class || out.Answer != nil {
				t.Fatalf("class %s: outcome %+v", class, out)
			}
		})
	}
	t.Run("a failed exit", func(t *testing.T) {
		if out := runPuppet(t, map[string]string{"output": answer, "exit": "1"}); out.Class != ClassHost || out.Answer != nil {
			t.Fatalf("a failed exit: outcome %+v", out)
		}
	})
}

// A class comes from the eight words; cli_missing and timeout come from the
// process alone, and any other word is host.
func TestTheBackendsClassIsOneOfTheWords(t *testing.T) {
	for _, word := range []string{"timeout", "cli_missing", "", "OK", "invalid_output", "model_rejected", "planted"} {
		t.Run(word, func(t *testing.T) {
			out := runPuppet(t, map[string]string{"output": answer, "stdout": events(`{"type":"class","class":"` + word + `"}`)})
			if out.Class != ClassHost || out.Answer != nil || out.CLI == "" || out.Attempts != 1 {
				t.Fatalf("class %q: outcome %+v, want host", word, out)
			}
		})
	}
}

// The models are the host's: names a model flag could take, never the model
// asked for, and a run whose host names none that answered says so.
func TestTheModelsAreOnlyTheHosts(t *testing.T) {
	t.Run("none named", func(t *testing.T) {
		out := runPuppet(t, map[string]string{"output": answer})
		if out.ModelAsked != "gpt-test-model" || out.ModelAnswered != "" || out.ModelsSeen != nil || !slices.Equal(out.Signals, []Signal{SignalModelUnreported}) {
			t.Fatalf("no model named: %+v", out)
		}
	})
	t.Run("several named", func(t *testing.T) {
		out := runPuppet(t, map[string]string{"output": answer, "stdout": events(
			`{"type":"model","name":"model-b"}`, `{"type":"model","name":"model-a"}`, `{"type":"model","name":"model-b"}`,
			`{"type":"model","name":"a name with spaces"}`, `{"type":"model","name":"--flag"}`, `{"type":"model","name":""}`,
			`{"type":"answered","name":"model-c[1m]"}`,
		)})
		if out.ModelAnswered != "model-c[1m]" || !slices.Equal(out.ModelsSeen, []string{"model-a", "model-b", "model-c[1m]"}) || out.Signals != nil {
			t.Fatalf("models named: %+v", out)
		}
	})
	t.Run("an answering model that is no name", func(t *testing.T) {
		out := runPuppet(t, map[string]string{"output": answer, "stdout": events(`{"type":"model","name":"model-a"}`, `{"type":"answered","name":"not a model"}`)})
		if out.ModelAnswered != "" || !slices.Equal(out.ModelsSeen, []string{"model-a"}) || !slices.Equal(out.Signals, []Signal{SignalModelUnreported}) {
			t.Fatalf("an answering model that is no name: %+v", out)
		}
	})
	// The host may name the model asked for; that is the host's word.
	t.Run("the model asked for", func(t *testing.T) {
		out := runPuppet(t, map[string]string{"output": answer, "stdout": events(`{"type":"answered","name":"gpt-test-model"}`)})
		if out.ModelAnswered != "gpt-test-model" || out.Signals != nil {
			t.Fatalf("the host named the model asked for: %+v", out)
		}
	})
	// A failed run says when the host named no model.
	t.Run("a failed run", func(t *testing.T) {
		out := runPuppet(t, map[string]string{"exit": "1"})
		if out.Class != ClassHost || out.ModelAnswered != "" || !slices.Equal(out.Signals, []Signal{SignalModelUnreported}) {
			t.Fatalf("a failed run: %+v", out)
		}
	})
}

// Refused is set only on host, and only to its two words.
func TestRefusedIsOnlyItsWordsAndOnlyOnHost(t *testing.T) {
	for _, tc := range []struct {
		class, what, want string
	}{
		{"host", RefusedModel, RefusedModel},
		{"host", RefusedEffort, RefusedEffort},
		{"host", "the model", ""},
		{"host", "Model", ""},
		{"auth", RefusedModel, ""},
		{"ok", RefusedEffort, ""},
	} {
		t.Run(tc.class+" "+tc.what, func(t *testing.T) {
			out := runPuppet(t, map[string]string{"output": answer, "stdout": events(
				`{"type":"class","class":"`+tc.class+`"}`, `{"type":"refused","what":"`+tc.what+`"}`)})
			if string(out.Class) != tc.class || out.Refused != tc.want {
				t.Fatalf("%s refusing %q: outcome %+v, want refused %q", tc.class, tc.what, out, tc.want)
			}
		})
	}
}

// Signals are the backend's declared constants, each once, then the core's;
// a backend can't give the core's.
func TestSignalsAreOnlyDeclaredConstants(t *testing.T) {
	out := runPuppet(t, map[string]string{"output": answer, "stdout": events(
		`{"type":"signal","name":"puppet_signal"}`, `{"type":"signal","name":"other_signal"}`,
		`{"type":"signal","name":"model_unreported"}`, `{"type":"signal","name":"puppet_signal"}`,
		`{"type":"answered","name":"model-a"}`)})
	if !slices.Equal(out.Signals, []Signal{signalPuppet}) {
		t.Fatalf("signals %q", out.Signals)
	}
	out = runPuppet(t, map[string]string{"output": answer, "stdout": events(`{"type":"signal","name":"puppet_signal"}`)})
	if !slices.Equal(out.Signals, []Signal{signalPuppet, SignalModelUnreported}) {
		t.Fatalf("signals %q", out.Signals)
	}
}

// Usage is summed over the attempts, never below 0, and kept on a timeout,
// as is the probe's version; the answer printed before the timeout is not.
func TestUsageAndVersionSeenBeforeATimeoutAreKept(t *testing.T) {
	isolate(t)
	dump := fakeCLI(t, `: > "$dump/printed"; exec sleep 300`)
	put(t, dump, "output", answer)
	put(t, dump, "version", "fake-cli 3.4.5\n")
	put(t, dump, "stdout", events(`{"type":"usage","in":100,"cached":-7,"out":30}`, `{"type":"usage","in":20,"cached":10,"out":5}`,
		`{"type":"answered","name":"model-a"}`))
	cli := ownCLI(t, adapter.Codex)
	r, _ := puppetRunner(t, adapter.Codex, Options{CLI: cli})
	ctx, timeouts := holdTimeouts(context.Background())
	done := launch(t, ctx, r, call(t, adapter.Codex))
	if !waitFile(dump, "printed") {
		t.Fatalf("the fake printed nothing in %s", hangGuard)
	}
	timeouts.expire()
	out, err := await(t, done, "a run whose time ended")
	if err != nil {
		t.Fatal(err)
	}
	// The puppet sums a run's events itself, so -7 + 10 reaches the core as 3.
	want := Usage{In: 120, CachedIn: 3, Out: 35}
	if out.Class != ClassTimeout || out.Answer != nil || out.Usage != want || !out.UsageSeen || out.CLIVersion != "3.4.5" || out.CLI != cli || out.ModelAnswered != "model-a" {
		t.Fatalf("outcome %+v, want a timeout with usage %+v and version 3.4.5", out, want)
	}
	// A run that reported no usage says so.
	if err := os.Remove(filepath.Join(dump, "extra")); err != nil {
		t.Fatal(err)
	}
	put(t, dump, "stdout", "")
	if out, err := r.Run(context.Background(), call(t, adapter.Codex)); err != nil || out.Class != ClassOK || out.UsageSeen || out.Usage != (Usage{}) || out.CLIVersion != "3.4.5" {
		t.Fatalf("a run with no usage: %+v %v", out, err)
	}
	// A count below 0 is 0.
	put(t, dump, "stdout", events(`{"type":"usage","in":-5,"cached":2,"out":1}`))
	if out, err := r.Run(context.Background(), call(t, adapter.Codex)); err != nil || out.Usage != (Usage{In: 0, CachedIn: 2, Out: 1}) {
		t.Fatalf("a negative count: %+v %v", out, err)
	}
}

// A marker planted where a host writes text, in an event's type, an item's
// type, an error's text, a model's name and the answer of a failed run,
// reaches no field of the Outcome, though the puppet passes it on.
func TestAPlantedMarkerReachesNoOutcomeField(t *testing.T) {
	const marker = "planted marker 41"
	for name, files := range map[string]map[string]string{
		"an event type": {"output": answer, "stdout": events(`{"type":"` + marker + `"}`)},
		"an item type":  {"output": answer, "stdout": events(`{"type":"item","item":{"type":"` + marker + `"}}`)},
		"an error text": {"output": `{"summary":"` + marker + `"}`, "stderr": marker + "\n", "exit": "1"},
		"every field": {"output": `{"summary":"` + marker + `"}`, "stderr": marker, "stdout": events(
			`{"type":"class","class":"`+marker+`"}`, `{"type":"refused","what":"`+marker+`"}`,
			`{"type":"signal","name":"`+marker+`"}`, `{"type":"model","name":"`+marker+`"}`,
			`{"type":"answered","name":"`+marker+`"}`, marker, `{"type":"item","item":{"type":"`+marker+`"}}`)},
	} {
		t.Run(name, func(t *testing.T) {
			out := runPuppet(t, files)
			if out.Attempts != 1 || strings.Contains(fmt.Sprintf("%#v", out), "marker") || strings.Contains(string(out.Answer), "marker") {
				t.Fatalf("the marker reached the outcome: %#v", out)
			}
		})
	}
}

// settle, the rules alone: their order, and what each keeps.
func TestSettle(t *testing.T) {
	allowed := map[Signal]bool{signalPuppet: true}
	base := Outcome{ModelAsked: "asked", CLI: "/x/cli", CLIVersion: "1.2.3", Billing: BillingPlan}
	for _, tc := range []struct {
		name string
		t    tally
		want Class
	}{
		{"a missing CLI before a tool sign", tally{missing: true, toolSign: true, timedOut: true}, ClassCLIMissing},
		{"a tool sign before a timeout", tally{attempts: 1, toolSign: true, timedOut: true}, ClassToolUsed},
		{"a timeout before the backend's ok", tally{attempts: 1, timedOut: true}, ClassTimeout},
		{"the backend's ok", tally{attempts: 1}, ClassOK},
	} {
		out := settle(base, result{class: ClassOK, answer: []byte(answer), refused: RefusedModel}, tc.t, allowed)
		if out.Class != tc.want || (out.Answer != nil) != (tc.want == ClassOK) || out.Refused != "" {
			t.Errorf("%s: %+v, want %s", tc.name, out, tc.want)
		}
		if tc.want == ClassCLIMissing && (out.CLI != "" || out.CLIVersion != "" || out.Signals != nil) {
			t.Errorf("%s: %+v keeps the CLI or a signal", tc.name, out)
		}
		if tc.want != ClassCLIMissing && (out.CLI != base.CLI || out.CLIVersion != base.CLIVersion || out.ModelAsked != base.ModelAsked) {
			t.Errorf("%s: %+v lost what the run gave", tc.name, out)
		}
	}
	// The answer kept is a copy.
	res := result{class: ClassOK, answer: []byte(answer)}
	out := settle(base, res, tally{attempts: 1}, allowed)
	res.answer[2] = 'X'
	if string(out.Answer) != answer {
		t.Fatalf("the answer is the backend's own slice: %s", out.Answer)
	}
	// Counts that overflow or go below 0 are bounded per attempt.
	var tl tally
	tl.add(result{usage: Usage{In: -1, CachedIn: 1 << 62, Out: 3}})
	tl.add(result{usage: Usage{In: 2, CachedIn: 1 << 62, Out: 3}})
	if tl.usage != (Usage{In: 2, CachedIn: 2 * maxTokens, Out: 6}) {
		t.Fatalf("usage %+v", tl.usage)
	}
}
