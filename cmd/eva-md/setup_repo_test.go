package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/heyNag/eva-md/internal/config"
)

// fakeCLI is a fake gh or glab that a test puts on its PATH. It answers
// auth status with auth, or when signedIn is set, as signed in only to the
// hosts that shell pattern matches, printing a made-up account and token
// that setup must never show; config get with host or protocol; repo view with after
// once repo create has run, else with before for a name matching the shell
// pattern exists, else as for a repo that doesn't exist; and repo create
// with create, printing createSays when it fails, or for --internal with
// internal and internalSays; a failing repo create also prints a made-up
// account and token. It logs each call to <bin>/<name>.log with the host and repo
// variables it got, the prompt switch and which of its token variables are
// set, never their values (see tokenVars).
type fakeCLI struct {
	name     string
	auth     int    // auth status's exit status
	signedIn string // a shell pattern for the hosts auth status is signed in to, in place of auth
	hang     string // a command, such as "auth status", that sleeps past hangGuard until killed
	host     string // glab's default host
	protocol string // the git protocol
	exists   string // a shell pattern for the names repo view finds
	before   string // repo view's JSON for them
	after    string // repo view's JSON once repo create ran
	create   int    // repo create's exit status
	// createSays is what a failing repo create prints, on stderr, or on
	// stdout with createOnStdout; internal and internalSays are repo
	// create --internal's exit status and what it prints.
	createSays     string
	createOnStdout bool
	internal       int
	internalSays   string
}

// envTokens are the token variables gh, glab or a CI job read, none of which
// may reach gh or glab from setup.
var envTokens = []string{"GH_TOKEN", "GITHUB_TOKEN", "GH_ENTERPRISE_TOKEN", "GITHUB_ENTERPRISE_TOKEN", "GITLAB_TOKEN", "GITLAB_ACCESS_TOKEN", "OAUTH_TOKEN", "CI_JOB_TOKEN"}

// tokenVars is how a fake CLI logs which of envTokens it got: tokens= then
// one letter for each that is set, so "tokens=" alone means none.
const tokenVars = "tokens=${GH_TOKEN:+a}${GITHUB_TOKEN:+b}${GH_ENTERPRISE_TOKEN:+c}${GITHUB_ENTERPRISE_TOKEN:+d}${GITLAB_TOKEN:+e}${GITLAB_ACCESS_TOKEN:+f}${OAUTH_TOKEN:+g}${CI_JOB_TOKEN:+h}"

func (c fakeCLI) write(t *testing.T, bin string) {
	t.Helper()
	q := func(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }
	exists := c.exists
	if exists == "" {
		exists = "-none-"
	}
	vars := "host=$GH_HOST repo=$GH_REPO prompt=$GH_PROMPT_DISABLED " + tokenVars
	if c.name == "glab" {
		vars = "host=$GITLAB_HOST repo=$GITLAB_REPO prompt=$GLAB_NO_PROMPT " + tokenVars
	}
	created := q(filepath.Join(bin, c.name+".created"))
	says := 2
	if c.createOnStdout {
		says = 1
	}
	auth := fmt.Sprintf("exit %d", c.auth)
	if c.signedIn != "" {
		auth = fmt.Sprintf(`case "$4" in %s) exit 0 ;; *) exit 1 ;; esac`, c.signedIn)
	}
	script := fmt.Sprintf(`#!/bin/sh
echo "$* %s" >> %s
if [ "$1 $2" = %s ]; then exec /bin/sleep 300; fi
case "$1 $2" in
"auth status") echo "Logged in to the host as fake-account-7f3a"; echo "Token: fake-token-7f3a" >&2; %s ;;
"config get") if [ "$3" = host ]; then echo %s; else echo %s; fi ;;
"repo view")
	if [ -f %s ]; then printf '%%s\n' %s; exit 0; fi
	case "$3" in %s) printf '%%s\n' %s ;; *) exit 1 ;; esac ;;
"repo create")
	case " $* " in *" --internal "*) code=%d; says=%s ;; *) code=%d; says=%s ;; esac
	if [ "$code" -ne 0 ]; then
		if [ -n "$says" ]; then printf '%%s\n' "$says" >&%d; fi
		echo "Logged in to the host as fake-account-7f3a"; echo "Token: fake-token-7f3a" >&2; exit "$code"
	fi
	: > %s ;;
*) exit 2 ;;
esac
`, vars, q(filepath.Join(bin, c.name+".log")), q(c.hang), auth, q(c.host), q(c.protocol), created, q(c.after), exists, q(c.before), c.internal, q(c.internalSays), c.create, q(c.createSays), says, created)
	if err := os.WriteFile(filepath.Join(bin, c.name), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
}

func ghJSON(name, visibility string, empty bool) string {
	return ghJSONOn("github.com", name, visibility, empty)
}

// ghJSONOn is what gh says about the repo me/<name> on host.
func ghJSONOn(host, name, visibility string, empty bool) string {
	return fmt.Sprintf(`{"isEmpty":%[3]t,"sshUrl":"git@%[4]s:me/%[1]s.git","url":"https://%[4]s/me/%[1]s","visibility":%[2]q}`, name, visibility, empty, host)
}

func glabJSON(name, visibility string, empty bool) string {
	return glabJSONOn("gitlab.example.com", name, visibility, empty)
}

// glabJSONOn is what glab says about the repo me/<name> on host.
func glabJSONOn(host, name, visibility string, empty bool) string {
	return fmt.Sprintf(`{"id":7,"path_with_namespace":"me/%[1]s","visibility":%[2]q,"empty_repo":%[3]t,"web_url":"https://%[4]s/me/%[1]s","ssh_url_to_repo":"git@%[4]s:me/%[1]s.git","http_url_to_repo":"https://%[4]s/me/%[1]s.git"}`, name, visibility, empty, host)
}

// fakes puts the fake CLIs on f's PATH.
func (f *fakeSetup) fakes(t *testing.T, clis ...fakeCLI) *fakeSetup {
	t.Helper()
	for _, c := range clis {
		c.write(t, f.bin)
	}
	return f
}

// answers runs setup with a terminal, answering the folder question and then
// with lines.
func (f *fakeSetup) answers(t *testing.T, lines ...string) (int, string, string) {
	t.Helper()
	code, out, errOut := f.run(strings.Join(append([]string{"~/projects/wiki"}, lines...), "\n") + "\n")
	noCLIText(t, out+errOut)
	return code, out, errOut
}

// noCLIText fails when setup printed what a fake CLI said.
func noCLIText(t *testing.T, printed string) {
	t.Helper()
	if strings.Contains(printed, "fake-account") || strings.Contains(printed, "fake-token") {
		t.Fatalf("setup printed what gh or glab said:\n%s", printed)
	}
}

// holdCLITimers returns a context under which a gh or glab command's time
// ends only when the test calls expire, as if it had run out, so a test of
// a command that hangs never races the machine's speed. A command under any
// other context gets its real time limit. The context ends with the test,
// which stops a command that a failing test leaves running.
func holdCLITimers(t *testing.T) (context.Context, *cliTimers) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	h := &cliTimers{running: map[*heldCLIContext]time.Duration{}}
	return context.WithValue(ctx, cliTimersKey{}, h), h
}

// cliTimers holds the time limits of the commands running under one
// context.
type cliTimers struct {
	mu      sync.Mutex
	running map[*heldCLIContext]time.Duration // each running command's time limit
}

type cliTimersKey struct{}

// expire ends the time of every command running now, as if it had run out,
// and returns their time limits. A command that starts later gets its own.
func (h *cliTimers) expire() []time.Duration {
	h.mu.Lock()
	defer h.mu.Unlock()
	var limits []time.Duration
	for c, d := range h.running {
		c.end(context.DeadlineExceeded)
		limits = append(limits, d)
	}
	clear(h.running)
	return limits
}

// heldCLITimer is cliTimer in the tests: under holdCLITimers' context a
// command's time ends when the test expires it, and elsewhere when it
// passes.
func heldCLITimer(ctx context.Context, d time.Duration) (context.Context, context.CancelFunc) {
	h, ok := ctx.Value(cliTimersKey{}).(*cliTimers)
	if !ok {
		return context.WithTimeout(ctx, d)
	}
	c := &heldCLIContext{Context: ctx, done: make(chan struct{})}
	h.mu.Lock()
	h.running[c] = d
	h.mu.Unlock()
	stop := context.AfterFunc(ctx, func() { c.end(ctx.Err()) })
	return c, func() {
		stop()
		h.mu.Lock()
		delete(h.running, c)
		h.mu.Unlock()
		c.end(context.Canceled)
	}
}

// heldCLIContext is a command's time limit that a test holds. Expired, it
// ends with the error a timeout gives.
type heldCLIContext struct {
	context.Context
	done chan struct{}
	mu   sync.Mutex
	err  error
}

func (c *heldCLIContext) Done() <-chan struct{} { return c.done }

func (c *heldCLIContext) Err() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.err
}

func (c *heldCLIContext) end(err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.err == nil {
		c.err = err
		close(c.done)
	}
}

// setupResult is what a setup started with begin returned.
type setupResult struct {
	code        int
	out, errOut string
}

// begin starts setup in the background under ctx, with a terminal,
// answering the folder question and then with lines, as answers does.
func (f *fakeSetup) begin(ctx context.Context, lines ...string) <-chan setupResult {
	input := strings.Join(append([]string{"~/projects/wiki"}, lines...), "\n") + "\n"
	env := f.env
	done := make(chan setupResult, 1)
	go func() {
		var out, errOut bytes.Buffer
		code := runSetup(ctx, nil, strings.NewReader(input), &out, &errOut, env)
		done <- setupResult{code, out.String(), errOut.String()}
	}()
	return done
}

// waitCall waits until the fake name has logged a whole line for a call of
// command, such as "repo create", while the setup begin started runs. It
// fails when setup ends first, or names the hang when no such call has come
// after hangGuard.
func (f *fakeSetup) waitCall(t *testing.T, done <-chan setupResult, name, command string) {
	t.Helper()
	deadline := time.Now().Add(hangGuard)
	for {
		for _, line := range strings.SplitAfter(f.log(t, name), "\n") {
			if strings.HasPrefix(line, command+" ") && strings.HasSuffix(line, "\n") {
				return
			}
		}
		select {
		case r := <-done:
			t.Fatalf("setup ended before %s ran %s: code %d\nstdout:\n%s\nstderr:\n%s", name, command, r.code, r.out, r.errOut)
		case <-time.After(10 * time.Millisecond):
		}
		if time.Now().After(deadline) {
			t.Fatalf("setup hung: it had not run %s %s after %s", name, command, hangGuard)
		}
	}
}

// wait waits for the setup begin started to return, and names what it
// still waits for when it has not returned after hangGuard.
func (f *fakeSetup) wait(t *testing.T, done <-chan setupResult, what string) (int, string, string) {
	t.Helper()
	select {
	case r := <-done:
		noCLIText(t, r.out+r.errOut)
		return r.code, r.out, r.errOut
	case <-time.After(hangGuard):
		t.Fatalf("setup hung: it still waited for %s %s after its time ran out", what, hangGuard)
		return 0, "", ""
	}
}

func (f *fakeSetup) log(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(f.bin, name+".log"))
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		t.Fatal(err)
	}
	return string(b)
}

// wantRemote checks that setup finished and made the wiki with remote.
func (f *fakeSetup) wantRemote(t *testing.T, code int, out, errOut, remote string) {
	t.Helper()
	if code != 0 || len(f.inits) != 1 || f.inits[0].Remote != remote {
		t.Fatalf("code %d inits %+v, want remote %s\nstdout:\n%s\nstderr:\n%s", code, f.inits, remote, out, errOut)
	}
}

func wantIn(t *testing.T, what, s string, wants ...string) {
	t.Helper()
	for _, want := range wants {
		if !strings.Contains(s, want) {
			t.Errorf("%s lacks %q:\n%s", what, want, s)
		}
	}
}

func wantNotIn(t *testing.T, what, s string, unwanted ...string) {
	t.Helper()
	for _, u := range unwanted {
		if strings.Contains(s, u) {
			t.Errorf("%s has %q:\n%s", what, u, s)
		}
	}
}

func TestSetupRemoteQuestionWithoutASignedInCLI(t *testing.T) {
	// Neither gh nor glab is installed: the question is as it always was.
	f := newFakeSetup(t, true)
	code, out, errOut := f.answers(t, "git@github.com:me/eva-wiki.git", "y")
	f.wantRemote(t, code, out, errOut, "git@github.com:me/eva-wiki.git")
	wantIn(t, "stdout", out, "2. Which private git repository should it be pushed to?\n   Create an empty private repository first")
	wantNotIn(t, "stdout", out, "Setup can create", "signed in", "Checking whether gh or glab")

	// glab is installed but not signed in: one line says why first, and the
	// pasted remote goes unchecked.
	f = newFakeSetup(t, true).fakes(t, fakeCLI{name: "glab", auth: 1, host: "gitlab.example.com"})
	code, out, errOut = f.answers(t, "git@gitlab.example.com:me/eva-wiki.git", "y")
	f.wantRemote(t, code, out, errOut, "git@gitlab.example.com:me/eva-wiki.git")
	wantIn(t, "stdout", out, "  - gh isn't installed\n  - glab isn't signed in to gitlab.example.com\n", "pushed to?\n   glab isn't signed in to gitlab.example.com, so setup can't create the repository.\n   Create an empty private repository first")
	wantIn(t, "glab log", f.log(t, "glab"), "auth status --hostname gitlab.example.com host=gitlab.example.com")
	wantNotIn(t, "glab log", f.log(t, "glab"), "repo view", "repo create")
}

// With no CLI that can tell about the repo's host, a pasted repo that git
// reads with no sign-in may be public: setup asks before using it, no being
// the default, and without a terminal refuses it. One git can't read is used.
func TestSetupAsksAboutARepoAnyoneCanRead(t *testing.T) {
	const question = "   git read this repo over HTTPS without signing in, so it may be public, and the wiki holds your chats' summaries. Use it anyway? [y/N] "
	f := newFakeSetup(t, true)
	f.public["git.example.org/me/eva-wiki"] = true
	code, out, errOut := f.answers(t, "git@git.example.org:me/eva-wiki.git", "", "git@git.example.org:me/private-wiki.git", "y")
	f.wantRemote(t, code, out, errOut, "git@git.example.org:me/private-wiki.git")
	wantIn(t, "stdout", out, "   Remote: "+"   Checking whether git.example.org/me/eva-wiki can be read without signing in.\n"+question+"   Choose a private repo\n")
	if want := []string{"git.example.org/me/eva-wiki", "git.example.org/me/private-wiki"}; !slices.Equal(f.reads, want) {
		t.Fatalf("read %q, want %q", f.reads, want)
	}
	// A yes uses it, as for a host that lets its own network read repos.
	f = newFakeSetup(t, true)
	f.public["git.example.org/me/eva-wiki"] = true
	code, out, errOut = f.answers(t, "git@git.example.org:me/eva-wiki.git", "y", "y")
	f.wantRemote(t, code, out, errOut, "git@git.example.org:me/eva-wiki.git")
	// Without a terminal, --remote is refused.
	f = newFakeSetup(t, false)
	f.public["git.example.org/me/eva-wiki"] = true
	if code, _, errOut := f.run("", "--dir", filepath.Join(f.env.home, "projects", "wiki"), "--remote", "git@git.example.org:me/eva-wiki.git"); code != 1 || errOut != "eva-md: "+plain(errReadable)+"\n" {
		t.Fatalf("code %d stderr %q", code, errOut)
	}
}

// The read runs with nothing that could sign it in: no git config, an empty
// home folder for .netrc, no Kerberos ticket or empty-user authentication,
// no proxy and no prompt.
func TestProbeCannotSignIn(t *testing.T) {
	isolated(t)
	for _, kv := range [][2]string{{"KRB5CCNAME", "API:ticket"}, {"HTTPS_PROXY", "http://proxy.example:3128"}, {"http_proxy", "http://proxy.example:3128"},
		{"GIT_ASKPASS", "/usr/bin/true"}, {"SSH_ASKPASS", "/usr/bin/true"}, {"NETRC", "/tmp/netrc"}, {"GIT_CONFIG_PARAMETERS", "'credential.helper'='store'"}} {
		t.Setenv(kv[0], kv[1])
	}
	cmd := probe(context.Background(), "git.example.org/me/eva-wiki", "/tmp/empty-home")
	if want := []string{"git", "-c", "credential.helper=", "-c", "http.emptyAuth=false", "ls-remote", "--quiet", "--", "https://git.example.org/me/eva-wiki.git"}; !slices.Equal(cmd.Args, want) {
		t.Fatalf("args %q", cmd.Args)
	}
	env := map[string]string{}
	for _, kv := range cmd.Env {
		k, v, _ := strings.Cut(kv, "=")
		if _, dup := env[k]; dup {
			t.Fatalf("%s set twice", k)
		}
		env[k] = v
	}
	for k, want := range map[string]string{"HOME": "/tmp/empty-home", "KRB5CCNAME": "FILE:/dev/null", "GIT_CONFIG_GLOBAL": "/dev/null", "GIT_CONFIG_NOSYSTEM": "1", "GIT_TERMINAL_PROMPT": "0", "GIT_ALLOW_PROTOCOL": "https"} {
		if env[k] != want {
			t.Errorf("%s=%q, want %q", k, env[k], want)
		}
	}
	for _, k := range []string{"HTTPS_PROXY", "http_proxy", "GIT_ASKPASS", "SSH_ASKPASS", "NETRC", "GIT_CONFIG_PARAMETERS"} {
		if v, ok := env[k]; ok {
			t.Errorf("%s=%q kept", k, v)
		}
	}
}

// A repo git can't reach with no sign-in, or a local one, isn't known to
// be public. The address here refuses at once, on this Mac.
func TestAnyoneCanReadFailsClosed(t *testing.T) {
	isolated(t)
	for _, id := range []string{"127.0.0.1:9/me/eva-wiki", "file:///tmp/eva-wiki"} {
		if anyoneCanRead(context.Background(), id) {
			t.Fatalf("%s reads as public", id)
		}
	}
}

func TestSetupCreatesTheRepoWithGH(t *testing.T) {
	const httpsHint = "   Git pushes over HTTPS, as gh is set to. If git can't sign in, run:\n     gh auth setup-git\n"
	for _, tc := range []struct{ protocol, remote string }{
		{"ssh", "git@github.com:me/eva-wiki.git"},
		{"https", "https://github.com/me/eva-wiki.git"},
	} {
		t.Run(tc.protocol, func(t *testing.T) {
			f := newFakeSetup(t, true).fakes(t, fakeCLI{name: "gh", protocol: tc.protocol, after: ghJSON("eva-wiki", "PRIVATE", true)})
			// Setup names the host itself; the user's GH_HOST and GH_REPO
			// never reach gh.
			t.Setenv("GH_HOST", "github.example.com")
			t.Setenv("GH_REPO", "someone/else")
			code, out, errOut := f.answers(t, "", "y")
			f.wantRemote(t, code, out, errOut, tc.remote)
			wantIn(t, "stdout", out,
				"   Setup can create it for you. Type another name if you like, or n to paste an address.\n",
				"   Create a private repo named eva-wiki with gh on github.com? [Y/n] ",
				"   Created "+tc.remote+", a private repo on github.com.\n",
				"  Pushed to     "+tc.remote+" (created just now)\n")
			wantNotIn(t, "stdout", out, "Create an empty private repository first")
			if tc.protocol == "https" {
				wantIn(t, "stdout", out, "   Created "+tc.remote+", a private repo on github.com.\n"+httpsHint)
			} else {
				wantNotIn(t, "stdout", out, "over HTTPS")
			}
			wantIn(t, "gh log", f.log(t, "gh"),
				"auth status --hostname github.com host=github.com repo= prompt=1 tokens=\n",
				"repo view eva-wiki --json visibility,isEmpty,sshUrl,url host=github.com repo= prompt=1 tokens=\n",
				"repo create eva-wiki --private host=github.com repo= prompt=1 tokens=\n",
				"config get git_protocol --host github.com host=github.com")
		})
	}
	t.Run("a name with a typo first", func(t *testing.T) {
		// A name that isn't one is answered as a name, not as an address.
		f := newFakeSetup(t, true).fakes(t, fakeCLI{name: "gh", after: ghJSON("eva-wiki", "PRIVATE", true)})
		code, out, errOut := f.answers(t, "eva wiki", "eva-wiki", "y")
		f.wantRemote(t, code, out, errOut, "git@github.com:me/eva-wiki.git")
		wantIn(t, "stdout", out, "   Repo names use letters, digits, '.', '_' and '-', with an owner or group before a slash if you like.\n   Another name, or an address: ")
		wantNotIn(t, "stdout", out, "Unsupported remote", "Paste an address like")
	})
	t.Run("declined later", func(t *testing.T) {
		f := newFakeSetup(t, true).fakes(t, fakeCLI{name: "gh", after: ghJSON("eva-wiki", "PRIVATE", true)})
		code, out, _ := f.answers(t, "", "n")
		if code != 1 || len(f.inits) != 0 || len(f.installs) != 0 {
			t.Fatalf("code %d inits %d installs %d", code, len(f.inits), len(f.installs))
		}
		wantIn(t, "stdout", out, "Nothing else was changed. The new repo stays, empty, for the next setup.")
	})
}

func TestSetupCreatesTheRepoWithGlab(t *testing.T) {
	// glab is signed in to a self-hosted GitLab, with its default git
	// protocol, and the user types another name.
	f := newFakeSetup(t, true).fakes(t, fakeCLI{name: "glab", host: "gitlab.example.com", after: glabJSON("team-memory", "private", true)})
	code, out, errOut := f.answers(t, "team-memory", "y")
	f.wantRemote(t, code, out, errOut, "git@gitlab.example.com:me/team-memory.git")
	wantIn(t, "stdout", out,
		"   Create a private repo named eva-wiki with glab on gitlab.example.com? [Y/n] ",
		"   Created git@gitlab.example.com:me/team-memory.git, a private repo on gitlab.example.com.\n")
	wantIn(t, "glab log", f.log(t, "glab"),
		"config get host host= repo= prompt=true tokens=\n",
		"auth status --hostname gitlab.example.com host=gitlab.example.com repo= prompt=true tokens=\n",
		"repo view team-memory --output json host=gitlab.example.com",
		"repo create team-memory --private --skipGitInit host=gitlab.example.com repo= prompt=true tokens=\n")
}

func TestSetupAsksWhichCLICreatesTheRepo(t *testing.T) {
	f := newFakeSetup(t, true).fakes(t,
		fakeCLI{name: "gh", after: ghJSON("eva-wiki", "PRIVATE", true)},
		fakeCLI{name: "glab", host: "gitlab.example.com", after: glabJSON("eva-wiki", "private", true)})
	code, out, errOut := f.answers(t, "both", "glab", "", "y")
	f.wantRemote(t, code, out, errOut, "git@gitlab.example.com:me/eva-wiki.git")
	wantIn(t, "stdout", out,
		"   Setup can create it with gh on github.com or glab on gitlab.example.com.\n   Which one? [GH/glab/n] ",
		"   Type gh, glab or n\n",
		"   Type another name if you like, or n to paste an address.\n   Create a private repo named eva-wiki with glab on gitlab.example.com? [Y/n] ")
	wantNotIn(t, "gh log", f.log(t, "gh"), "repo view", "repo create")
	wantIn(t, "glab log", f.log(t, "glab"), "repo create eva-wiki --private --skipGitInit")

	// Enter takes gh.
	f = newFakeSetup(t, true).fakes(t,
		fakeCLI{name: "gh", after: ghJSON("eva-wiki", "PRIVATE", true)},
		fakeCLI{name: "glab", host: "gitlab.example.com", after: glabJSON("eva-wiki", "private", true)})
	code, out, errOut = f.answers(t, "", "", "y")
	f.wantRemote(t, code, out, errOut, "git@github.com:me/eva-wiki.git")
	wantIn(t, "stdout", out, "   Which one? [GH/glab/n]    Type another name if you like, or n to paste an address.\n   Create a private repo named eva-wiki with gh on github.com? [Y/n] ")
	wantNotIn(t, "glab log", f.log(t, "glab"), "repo view", "repo create")
}

func TestSetupRepoOfferDeclined(t *testing.T) {
	f := newFakeSetup(t, true).fakes(t, fakeCLI{name: "gh", after: ghJSON("eva-wiki", "PRIVATE", true)})
	code, out, errOut := f.answers(t, "n", "git@github.com:me/own-wiki.git", "y")
	f.wantRemote(t, code, out, errOut, "git@github.com:me/own-wiki.git")
	// Straight to the question as it always was, with no line saying why.
	wantIn(t, "stdout", out, "with gh on github.com? [Y/n]    Create an empty private repository first")
	// gh was asked whether the pasted repo is private; it doesn't know the
	// repo, so the remote is taken as before.
	wantIn(t, "gh log", f.log(t, "gh"), "repo view me/own-wiki --json")
	wantNotIn(t, "gh log", f.log(t, "gh"), "repo create")
}

func TestSetupFallsBackWhenCreateFails(t *testing.T) {
	f := newFakeSetup(t, true).fakes(t, fakeCLI{name: "gh", create: 1})
	code, out, errOut := f.answers(t, "", "git@github.com:me/eva-wiki.git", "y")
	f.wantRemote(t, code, out, errOut, "git@github.com:me/eva-wiki.git")
	wantIn(t, "stdout", out, "   gh couldn't create eva-wiki.\n   Create an empty private repository first")
}

func TestSetupFallsBackWhenTheNewRepoCantBeRead(t *testing.T) {
	// gh created the repo, but repo view doesn't answer as expected, as an
	// older gh might not: setup asks for an address without sending the user
	// off to create another repo first.
	f := newFakeSetup(t, true).fakes(t, fakeCLI{name: "gh"})
	code, out, errOut := f.answers(t, "", "git@github.com:me/eva-wiki.git", "y")
	f.wantRemote(t, code, out, errOut, "git@github.com:me/eva-wiki.git")
	wantIn(t, "stdout", out, "   gh created eva-wiki but couldn't read back its address.\n   Paste the SSH or HTTPS address of an empty private repository, such as\n   git@github.com:you/eva-wiki.git.\n   Remote: ")
	wantNotIn(t, "stdout", out, "Create an empty private repository first")
}

// A CLI that doesn't answer in time is given up on: once the time of the
// command that hangs runs out, setup says so and asks for an address, as it
// does with no CLI.
func TestSetupFallsBackWhenACLIHangs(t *testing.T) {
	for _, tc := range []struct {
		cli    fakeCLI
		remote string
	}{
		{fakeCLI{name: "gh", hang: "auth status"}, "git@github.com:me/eva-wiki.git"},
		{fakeCLI{name: "glab", hang: "config get"}, "git@gitlab.example.com:me/eva-wiki.git"},
	} {
		f := newFakeSetup(t, true).fakes(t, tc.cli)
		ctx, timers := holdCLITimers(t)
		done := f.begin(ctx, tc.remote, "y")
		f.waitCall(t, done, tc.cli.name, tc.cli.hang)
		// The command that hangs is the only one running, under the time
		// every command but repo create gets.
		if limits := timers.expire(); !slices.Equal(limits, []time.Duration{repoCLITimeout}) {
			t.Fatalf("expired the time limits %v, want only the %s of %s %s", limits, repoCLITimeout, tc.cli.name, tc.cli.hang)
		}
		code, out, errOut := f.wait(t, done, tc.cli.name+" "+tc.cli.hang)
		f.wantRemote(t, code, out, errOut, tc.remote)
		if !strings.Contains(out, "   "+tc.cli.name+" didn't answer in time, so setup can't create the repository.\n   Create an empty private repository first") {
			t.Fatalf("%s's timeout not reported:\n%s", tc.cli.name, out)
		}
		// What hung isn't run again. gh isn't asked again about the pasted
		// remote; glab, which hung naming its default host, is asked about
		// the remote's own host directly.
		log := f.log(t, tc.cli.name)
		if strings.Count(log, tc.cli.hang+" ") != 1 || !strings.HasPrefix(log, tc.cli.hang+" ") {
			t.Fatalf("%s asked more than once, or not as expected:\n%s", tc.cli.name, log)
		}
		if tc.cli.name == "gh" && strings.Count(log, "\n") != 1 {
			t.Fatalf("gh asked again:\n%s", log)
		}
		if tc.cli.name == "glab" {
			wantIn(t, "glab log", log, "auth status --hostname gitlab.example.com host=gitlab.example.com ")
		}
	}
}

func TestSetupCreateThatHangsMayHaveMadeTheRepo(t *testing.T) {
	// repo create doesn't answer in time, though the host may have made the
	// repo: setup says so, and doesn't claim it changed nothing.
	f := newFakeSetup(t, true).fakes(t, fakeCLI{name: "gh", hang: "repo create"})
	ctx, timers := holdCLITimers(t)
	done := f.begin(ctx, "", "git@github.com:me/eva-wiki.git", "n")
	f.waitCall(t, done, "gh", "repo create")
	// repo create, the only command running, gets three times as long.
	if limits := timers.expire(); !slices.Equal(limits, []time.Duration{3 * repoCLITimeout}) {
		t.Fatalf("expired the time limits %v, want only the %s of gh repo create", limits, 3*repoCLITimeout)
	}
	code, out, _ := f.wait(t, done, "gh repo create")
	if code != 1 || len(f.inits) != 0 || len(f.installs) != 0 {
		t.Fatalf("code %d inits %d installs %d\n%s", code, len(f.inits), len(f.installs), out)
	}
	wantIn(t, "stdout", out,
		"   gh didn't answer in time; eva-wiki may or may not have been created.\n   Paste the SSH or HTTPS address",
		"Nothing else was changed. The new repo stays, empty, for the next setup.")
	wantNotIn(t, "stdout", out, "Create an empty private repository first", "Nothing was changed.")
}

func TestSetupUsesAnExistingPrivateRepo(t *testing.T) {
	t.Run("private and empty", func(t *testing.T) {
		f := newFakeSetup(t, true).fakes(t, fakeCLI{name: "gh", exists: "eva-wiki", before: ghJSON("eva-wiki", "PRIVATE", true)})
		code, out, errOut := f.answers(t, "", "y")
		f.wantRemote(t, code, out, errOut, "git@github.com:me/eva-wiki.git")
		wantIn(t, "stdout", out, "   eva-wiki already exists on github.com, private and empty: using git@github.com:me/eva-wiki.git.\n")
		wantNotIn(t, "gh log", f.log(t, "gh"), "repo create")
		// gh has said it is empty, so setup doesn't read it.
		if len(f.peeks) != 0 {
			t.Fatalf("read the repo %q", f.peeks)
		}
	})
	t.Run("public", func(t *testing.T) {
		f := newFakeSetup(t, true).fakes(t, fakeCLI{name: "gh", exists: "eva-wiki", before: ghJSON("eva-wiki", "PUBLIC", true), after: ghJSON("eva-memory", "PRIVATE", true)})
		code, out, errOut := f.answers(t, "", "eva-memory", "y")
		f.wantRemote(t, code, out, errOut, "git@github.com:me/eva-memory.git")
		wantIn(t, "stdout", out, "   eva-wiki already exists on github.com and is public; the wiki holds your chats' summaries, so use a private repo.\n   Another name, or an address: ")
		wantIn(t, "gh log", f.log(t, "gh"), "repo create eva-memory --private")
		wantNotIn(t, "gh log", f.log(t, "gh"), "repo create eva-wiki")
	})
}

// The wiki on another Mac, or something else: gh finds eva-wiki private and
// with content.
var withContent = fakeCLI{name: "gh", exists: "eva-wiki", before: ghJSON("eva-wiki", "PRIVATE", false), after: ghJSON("eva-memory", "PRIVATE", true)}

const (
	contentQuestion = "   eva-wiki already has content: use it as your wiki? [Y/n] "
	checking        = "   Checking the repo with git; your git setup may ask you to approve.\n"
	// readable is the line before the no-sign-in read of eva-wiki, on a
	// host no CLI can tell about, as github.com is when gh isn't signed in.
	readable = "   Checking whether github.com/me/eva-wiki can be read without signing in.\n"
	otherMac = "   This wiki was set up on old-laptop. One wiki per machine: use it here only if old-laptop no longer pushes to it. Use it? [y/N] "
)

func TestSetupUsesARepoWithContent(t *testing.T) {
	t.Run("an eva wiki", func(t *testing.T) {
		// eva-wiki holds a wiki made on old-laptop.
		f := newFakeSetup(t, true).fakes(t, withContent)
		f.realGit(t)
		f.bareRepo(t, "eva-wiki")
		evaWiki(t, filepath.Join(t.TempDir(), "wiki"), "git@github.com:me/eva-wiki.git", "old-laptop")

		// Declined: setup asks for another name, without reading the repo.
		code, out, _ := f.answers(t, "", "n")
		if code != 1 || len(f.peeks) != 0 || len(f.inits) != 0 {
			t.Fatalf("code %d read %q inits %d\n%s", code, f.peeks, len(f.inits), out)
		}
		wantIn(t, "stdout", out, "[Y/n] "+contentQuestion+"   Another name, or an address: ")

		// This Mac is this-mac, and no is the default.
		code, out, _ = f.answers(t, "", "", "")
		if code != 1 || len(f.inits) != 0 {
			t.Fatalf("code %d inits %d\n%s", code, len(f.inits), out)
		}
		wantIn(t, "stdout", out, contentQuestion+checking+otherMac+"   Choose another repo for this Mac\n   Another name, or an address: ")

		// On old-laptop itself, it is used without another question.
		f.env.machineLabel = func() (string, error) { return "old-laptop", nil }
		code, out, errOut := f.answers(t, "", "", "y")
		f.wantRemote(t, code, out, errOut, "git@github.com:me/eva-wiki.git")
		wantIn(t, "stdout", out,
			contentQuestion+checking+"   The repo holds an eva wiki set up on this Mac: setup will use it.\n",
			"Setup will clone your existing wiki into this folder, then install the plugins.\n",
			"Cloning the wiki\n", "  Using the existing eva wiki in "+filepath.Join(f.env.home, "projects", "wiki")+"\n")
		wantNotIn(t, "gh log", f.log(t, "gh"), "repo create")
		if c, err := config.Load(f.env.dirs.ConfigFile()); err != nil || c.Instance != "old-laptop-1a2b3c4d" {
			t.Fatalf("config %+v %v", c, err)
		}
		f.noTempLeft(t)
	})
	t.Run("not an eva wiki", func(t *testing.T) {
		// Refused before anything is cloned into the folder, which stays as
		// it was; another name follows.
		f := newFakeSetup(t, true).fakes(t, withContent)
		f.realGit(t)
		f.plainRepo(t, "eva-wiki")
		bare := f.bareRepo(t, "eva-memory")
		const refusal = "   This repo has content but isn't an eva wiki; use an empty private repo, or the repo of your eva wiki\n   Another name, or an address: "
		code, out, errOut := f.answers(t, "", "")
		if code != 1 || !strings.Contains(out, contentQuestion+checking+refusal) || !strings.Contains(errOut, "nothing was changed") {
			t.Fatalf("code %d\n%s\n%s", code, out, errOut)
		}
		if _, err := os.Stat(filepath.Join(f.env.home, "projects", "wiki")); !errors.Is(err, fs.ErrNotExist) {
			t.Fatalf("the folder was made: %v", err)
		}
		f.noTempLeft(t)
		code, out, errOut = f.answers(t, "", "", "eva-memory", "y", "y")
		f.wantRemote(t, code, out, errOut, "git@github.com:me/eva-memory.git")
		wantIn(t, "stdout", out, refusal, "Setup will create a new wiki in this folder and push it, then install the plugins.\n", "  Wiki ready in ")
		wantIn(t, "gh log", f.log(t, "gh"), "repo create eva-memory --private")
		if refs, head := gitIn(t, bare, "for-each-ref", "--format=%(objectname)"), gitIn(t, filepath.Join(f.env.home, "projects", "wiki"), "rev-parse", "HEAD"); refs != head {
			t.Fatalf("pushed %q, want %s", refs, head)
		}
	})
}

func TestSetupReadsATypedRepo(t *testing.T) {
	// Neither gh nor glab: the address is typed, and setup reads the repo.
	// eva-wiki holds the wiki this Mac made; notes holds something else.
	f := newFakeSetup(t, true)
	f.realGit(t)
	f.bareRepo(t, "eva-wiki")
	evaWiki(t, filepath.Join(t.TempDir(), "wiki"), "git@github.com:me/eva-wiki.git", "this-mac")
	f.plainRepo(t, "notes")
	empty := f.bareRepo(t, "empty-wiki")
	folder := filepath.Join(f.env.home, "projects", "wiki")

	t.Run("one git can't read", func(t *testing.T) {
		code, out, _ := f.answers(t, "git@github.com:me/missing.git")
		if code != 1 || !strings.Contains(out, "   Remote: "+strings.Replace(readable, "eva-wiki", "missing", 1)+checking+"   Git found no repo at this address on github.com, or none the account it signs in with can see; create it first as an empty private repo, or check the address\n   Remote: ") {
			t.Fatalf("code %d\n%s", code, out)
		}
	})
	t.Run("a declined summary", func(t *testing.T) {
		code, out, _ := f.answers(t, "git@github.com:me/eva-wiki.git", "n")
		if code != 1 || len(f.inits) != 0 || !strings.Contains(out, "Setup will clone your existing wiki into this folder, then install the plugins.\nSet up eva.md with these choices? [y/N] Nothing was changed.\n") {
			t.Fatalf("code %d inits %d\n%s", code, len(f.inits), out)
		}
		for _, p := range []string{folder, f.env.dirs.ConfigFile()} {
			if _, err := os.Stat(p); !errors.Is(err, fs.ErrNotExist) {
				t.Fatalf("%s was made: %v", p, err)
			}
		}
		f.noTempLeft(t)
	})
	t.Run("this Mac's wiki", func(t *testing.T) {
		code, out, errOut := f.answers(t, "git@github.com:me/eva-wiki.git", "y")
		f.wantRemote(t, code, out, errOut, "git@github.com:me/eva-wiki.git")
		wantIn(t, "stdout", out, "   Remote: "+readable+checking+"   The repo holds an eva wiki set up on this Mac: setup will use it.\n", "  Using the existing eva wiki in "+folder+"\n")
	})
	t.Run("not an eva wiki, then an empty one", func(t *testing.T) {
		// A Mac that isn't set up yet, whichever subtests ran before.
		if err := os.RemoveAll(folder); err != nil {
			t.Fatal(err)
		}
		if err := os.Remove(f.env.dirs.ConfigFile()); err != nil && !errors.Is(err, fs.ErrNotExist) {
			t.Fatal(err)
		}
		f.inits = nil
		code, out, errOut := f.answers(t, "git@github.com:me/notes.git", "git@github.com:me/empty-wiki.git", "y", "y")
		f.wantRemote(t, code, out, errOut, "git@github.com:me/empty-wiki.git")
		wantIn(t, "stdout", out,
			checking+"   This repo has content but isn't an eva wiki; use an empty private repo, or the repo of your eva wiki\n   Remote: ",
			checking+"   The repo is empty: setup will create the wiki in it.\n",
			"  Wiki ready in "+folder+"\n")
		if refs, head := gitIn(t, empty, "for-each-ref", "--format=%(objectname)"), gitIn(t, folder, "rev-parse", "HEAD"); refs != head {
			t.Fatalf("pushed %q, want %s", refs, head)
		}
	})
}

func TestSetupWarnsAboutAWikiFromAnotherMac(t *testing.T) {
	f := newFakeSetup(t, true).ghOnGitHub(t)
	f.realGit(t)
	f.bareRepo(t, "eva-wiki")
	evaWiki(t, filepath.Join(t.TempDir(), "wiki"), "git@github.com:me/eva-wiki.git", "old-laptop")

	// With --remote in a terminal, it asks too, and no is the default.
	code, out, errOut := f.run("\n", "--dir", filepath.Join(f.env.home, "projects", "wiki"), "--remote", "git@github.com:me/eva-wiki.git")
	if code != 1 || len(f.inits) != 0 || !strings.HasSuffix(out, checking+otherMac) || errOut != "eva-md: Choose another repo for this Mac\n" {
		t.Fatalf("code %d inits %d\n%s\n%s", code, len(f.inits), out, errOut)
	}

	// Enter keeps the default; asked again, the user says yes. With no gh,
	// the typed repo is read with no sign-in.
	if err := os.Remove(filepath.Join(f.bin, "gh")); err != nil {
		t.Fatal(err)
	}
	code, out, errOut = f.answers(t, "git@github.com:me/eva-wiki.git", "", "git@github.com:me/eva-wiki.git", "y", "y")
	f.wantRemote(t, code, out, errOut, "git@github.com:me/eva-wiki.git")
	wantIn(t, "stdout", out,
		checking+otherMac+"   Choose another repo for this Mac\n   Remote: "+readable+checking+otherMac+"\n",
		"Setup will clone your existing wiki into this folder")
	// The wiki keeps its instance.
	if c, err := config.Load(f.env.dirs.ConfigFile()); err != nil || c.Instance != "old-laptop-1a2b3c4d" {
		t.Fatalf("config %+v %v", c, err)
	}
}

func TestSetupKnowsThisMacWhateverItsDomain(t *testing.T) {
	// macOS's hostname follows the network: a wiki made as this-mac.local is
	// this Mac's as this-mac, and the other way round. Nothing is asked.
	for _, tc := range []struct{ made, here string }{{"this-mac.local", "this-mac"}, {"this-mac", "this-mac.lan"}} {
		f := newFakeSetup(t, true)
		f.realGit(t)
		f.bareRepo(t, "eva-wiki")
		evaWiki(t, filepath.Join(t.TempDir(), "wiki"), "git@github.com:me/eva-wiki.git", tc.made)
		f.env.machineLabel = func() (string, error) { return tc.here, nil }
		code, out, errOut := f.answers(t, "git@github.com:me/eva-wiki.git", "y")
		f.wantRemote(t, code, out, errOut, "git@github.com:me/eva-wiki.git")
		wantIn(t, "stdout", out, checking+"   The repo holds an eva wiki set up on this Mac: setup will use it.\n")
		wantNotIn(t, "stdout", out, "This wiki was set up on")
	}
}

// To clone a missing wiki again from a host gh and glab are both signed in
// to, whose name says neither, setup asks which one it is, with the line
// that says why and the hint after an answer it doesn't understand; three
// such answers stop setup, as other questions do, with nothing cloned.
func TestSetupStopsWhenTheCloneAddressQuestionGoesUnanswered(t *testing.T) {
	f := newFakeSetup(t, true).fakes(t, fakeCLI{name: "gh", signedIn: "*"}, fakeCLI{name: "glab", signedIn: "*", host: "gitlab.com"})
	f.realGit(t)
	f.bareRepo(t, "eva-wiki")
	c := f.setUp(t, "git@code.example.com:me/eva-wiki.git")
	if err := os.RemoveAll(c.WikiPath); err != nil {
		t.Fatal(err)
	}
	code, out, errOut := f.run("x\ny\nz\n\n")
	noCLIText(t, out+errOut)
	if code != 1 || len(f.inits) != 0 || len(f.peeks) != 0 || errOut != "eva-md: setup stopped; nothing was changed\n" {
		t.Fatalf("code %d inits %d peeks %q\n%s\n%s", code, len(f.inits), f.peeks, out, errOut)
	}
	wantIn(t, "stdout", out, "   gh and glab are both signed in to code.example.com.\n   Is it GitHub or GitLab? [gh/glab]    Type gh or glab\n")
	if _, err := os.Stat(c.WikiPath); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("the wiki was cloned again: %v", err)
	}
}
