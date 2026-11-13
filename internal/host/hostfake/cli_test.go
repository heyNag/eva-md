package hostfake

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// answer is what a command printed and how it ended.
type answer struct {
	stdout, stderr string
	code           int
}

func viaRun(t *testing.T, f *Fake, stdin, cli string, args ...string) answer {
	t.Helper()
	out, err := f.RunStdin(context.Background(), []byte(stdin), cli, args...)
	a := answer{stdout: string(out)}
	var e *ExitError
	switch {
	case err == nil:
	case errors.As(err, &e):
		a.code, a.stderr = e.Code, e.Stderr
	default:
		t.Fatalf("%s %v: %v", cli, args, err)
	}
	return a
}

func viaCLI(t *testing.T, bin, stdin, cli string, args ...string) answer {
	t.Helper()
	cmd := exec.Command(filepath.Join(bin, cli), args...)
	cmd.Dir = "/"
	cmd.Stdin = strings.NewReader(stdin)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	a := answer{stdout: stdout.String(), stderr: stderr.String()}
	var e *exec.ExitError
	switch {
	case err == nil:
	case errors.As(err, &e) && e.ExitCode() > 0:
		a.code = e.ExitCode()
	default:
		t.Fatalf("%s %v: %v", cli, args, err)
	}
	return a
}

// Run and the written CLIs give the same bytes and exit codes for the same
// commands, hooks/list included, failures and refusals too, and record the
// same calls. Two fakes with roots of their own share the marketplaces.
func TestRunAndTheWrittenCLIsAgree(t *testing.T) {
	dir := realTemp(t)
	viaFn, viaBin := New(filepath.Join(dir, "run")), New(filepath.Join(dir, "cli"))
	bin := filepath.Join(dir, "bin")
	if err := viaBin.WriteCLIs(bin); err != nil {
		t.Fatal(err)
	}
	// The marketplaces both fakes add, laid out as newHost lays them out.
	codex, claude := newHost(t, Codex), newHost(t, Claude)
	other := filepath.Join(dir, "other")
	write(t, filepath.Join(other, ".agents", "plugins", "marketplace.json"), `{"name": "other", "plugins": []}`+"\n")
	both := func(fn func(f *Fake) error) {
		t.Helper()
		for _, f := range []*Fake{viaFn, viaBin} {
			if err := fn(f); err != nil {
				t.Fatal(err)
			}
		}
	}
	type step struct {
		do    func()
		stdin string
		cli   string
		args  []string
		code  int // the exit code both must give
	}
	cmd := func(code int, cli string, args ...string) step { return step{cli: cli, args: args, code: code} }
	list := step{stdin: listRequest, cli: Codex, args: []string{"--no-daemon", "app-server", "--stdio"}}
	steps := []step{
		cmd(0, Codex, "--version"),
		cmd(0, Claude, "--version"),
		cmd(0, Codex, "features", "list"),
		cmd(0, Codex, "plugin", "marketplace", "list", "--json"),
		cmd(0, Codex, "plugin", "marketplace", "add", codex.market, "--json"),
		cmd(0, Codex, "plugin", "marketplace", "add", other, "--json"),
		cmd(0, Codex, "plugin", "add", "eva-md@eva-md", "--json"),
		cmd(0, Codex, "plugin", "list", "--json", "--marketplace=eva-md"),
		cmd(0, Codex, "plugin", "list", "--json"),
		list,
		{do: func() { both(func(f *Fake) error { return f.Approve(Codex) }) }},
		list,
		{stdin: `{"id":7,"method":"made/up","params":{}}` + "\n", cli: Codex, args: []string{"app-server", "--stdio"}},
		cmd(0, Claude, "plugin", "marketplace", "add", claude.market, "--json"),
		cmd(0, Claude, "plugin", "install", "eva-md@eva-md", "--scope", "user", "--json"),
		cmd(1, Claude, "plugin", "install", "eva-md@eva-md", "--scope", "user", "--json"),
		cmd(0, Claude, "plugin", "list", "--json"),
		cmd(0, Claude, "plugin", "validate", "--strict", "--json", claude.plugin),
		cmd(1, Claude, "plugin", "validate", "--strict", "--json", dir),
		{do: func() {
			both(func(f *Fake) error { return f.FailNext("codex plugin remove", Before) })
			both(func(f *Fake) error { return f.FailNext("claude plugin uninstall", After) })
		}},
		cmd(1, Codex, "plugin", "remove", "eva-md@eva-md", "--json"),
		cmd(0, Codex, "plugin", "list", "--json", "--marketplace=eva-md"),
		cmd(1, Claude, "plugin", "uninstall", "eva-md@eva-md", "--scope", "user", "--json"),
		cmd(0, Claude, "plugin", "list", "--json"),
		cmd(0, Claude, "plugin", "marketplace", "remove", "eva-md", "--scope", "user", "--json"),
		cmd(0, Claude, "plugin", "marketplace", "list", "--json"),
		cmd(0, Claude, "plugin", "marketplace", "remove", "eva-md", "--json"),
		cmd(0, Claude, "plugin", "marketplace", "list", "--json"),
		cmd(2, Codex, "--dangerously-bypass-hook-trust", "exec", "made-up prompt"),
		cmd(2, Claude, "-p", "made-up prompt"),
		cmd(2, Codex, "made-up", "command"),
		cmd(1, Codex, "plugin", "marketplace", "add", "relative/folder", "--json"),
		{do: func() {
			if err := os.Remove(filepath.Join(other, ".agents", "plugins", "marketplace.json")); err != nil {
				t.Fatal(err)
			}
		}},
		cmd(1, Codex, "plugin", "marketplace", "list", "--json"),
		cmd(1, Codex, "plugin", "list", "--json", "--marketplace=other"),
		cmd(0, Codex, "plugin", "list", "--json", "--marketplace=eva-md"),
		{do: func() { both(func(f *Fake) error { return f.SetPolicy(Codex, Policy{CodexHooksOff: true}) }) }},
		cmd(0, Codex, "features", "list"),
		list,
		{do: func() { both(func(f *Fake) error { return f.FailNext("codex --no-daemon app-server", After) }) }},
		{stdin: listRequest, cli: Codex, args: []string{"--no-daemon", "app-server", "--stdio"}, code: 1},
		list,
	}
	for _, s := range steps {
		if s.do != nil {
			s.do()
			continue
		}
		got := viaRun(t, viaFn, s.stdin, s.cli, s.args...)
		want := viaCLI(t, bin, s.stdin, s.cli, s.args...)
		name := s.cli + " " + strings.Join(s.args, " ")
		if got != want {
			t.Errorf("%s:\nRun gave  %+v\nthe CLI   %+v", name, got, want)
		}
		if got.code != s.code {
			t.Errorf("%s ended with %d, want %d: %s", name, got.code, s.code, got.stderr)
		}
		if got.code == 0 && got.stdout == "" {
			t.Errorf("%s printed nothing", name)
		}
	}
	runCalls, err := viaFn.Calls()
	if err != nil {
		t.Fatal(err)
	}
	cliCalls, err := viaBin.Calls()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(runCalls, cliCalls) || len(runCalls) == 0 {
		t.Errorf("the calls differ:\nRun %v\nCLI %v", runCalls, cliCalls)
	}
}

// A stalled command waits until its context ends, Run's and a written
// CLI's, and stalls once.
func TestStallUntilTheContextEnds(t *testing.T) {
	f := New(realTemp(t))
	if err := f.FailNext("codex features list", Stall); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	stalled := false
	f.onStall = func() { stalled = true; cancel() }
	_, err := f.Run(ctx, Codex, "features", "list")
	if !stalled || !errors.Is(err, context.Canceled) {
		t.Fatalf("a stalled Run gave %v, stalled %t", err, stalled)
	}
	f.onStall = nil
	if _, err := f.Run(context.Background(), Codex, "features", "list"); err != nil {
		t.Fatalf("the stall was not used up: %v", err)
	}

	bin := filepath.Join(realTemp(t), "bin")
	if err := f.WriteCLIs(bin); err != nil {
		t.Fatal(err)
	}
	if err := f.FailNext("claude plugin list", Stall); err != nil {
		t.Fatal(err)
	}
	ctx, cancel = context.WithCancel(context.Background())
	defer cancel()
	cmd := exec.CommandContext(ctx, filepath.Join(bin, Claude), "plugin", "list", "--json")
	stderr, err := cmd.StderrPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	line, err := bufio.NewReader(stderr).ReadString('\n')
	if line != stallNotice {
		t.Fatalf("the written CLI printed %q (%v) before it stalled", line, err)
	}
	cancel()
	if err := cmd.Wait(); err == nil || cmd.ProcessState.ExitCode() != -1 {
		t.Fatalf("the stalled CLI ended with %v, exit %d; want it killed", err, cmd.ProcessState.ExitCode())
	}
	if a := viaCLI(t, bin, "", Claude, "plugin", "list", "--json"); a.code != 0 || a.stdout != "[]\n" {
		t.Fatalf("after the stall: %+v", a)
	}
}

// A command made to fail before it takes effect changes nothing; one made
// to fail after it does. Each fails once.
func TestFailBeforeAndAfter(t *testing.T) {
	h := newHost(t, Codex)
	h.run(t, "plugin", "marketplace", "remove", "eva-md", "--json")
	for _, c := range []struct {
		when  When
		added bool
	}{{Before, false}, {After, true}} {
		if err := h.f.FailNext("codex plugin marketplace add", c.when); err != nil {
			t.Fatal(err)
		}
		_, err := h.f.Run(context.Background(), Codex, "plugin", "marketplace", "add", h.market, "--json")
		var e *ExitError
		if !errors.As(err, &e) || e.Code != 1 {
			t.Fatalf("a command made to fail gave %v", err)
		}
		listed := strings.Contains(string(h.run(t, "plugin", "marketplace", "list", "--json")), h.market)
		if listed != c.added {
			t.Errorf("failed at %d, the marketplace is listed: %t", c.when, listed)
		}
		h.run(t, "plugin", "marketplace", "add", h.market, "--json")
		h.run(t, "plugin", "marketplace", "remove", "eva-md", "--json")
	}
	if err := h.f.FailNext("codex plugin add", 0); err == nil {
		t.Error("FailNext took no when")
	}
}
