package main

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/heyNag/eva-md/internal/config"
)

// fakeCLI is a fake gh or glab that a test puts on its PATH. It answers
// auth status with auth, printing a made-up account and token that setup
// must never show; config get with host or protocol; repo view with after
// once repo create has run, else with before for a name matching the shell
// pattern exists, else as for a repo that doesn't exist; and repo create
// with create. It logs each call to <bin>/<name>.log with the host and repo
// variables it got and the prompt switch; glab adds which of its token
// variables are set, never their values.
type fakeCLI struct {
	name     string
	auth     int    // auth status's exit status
	hang     string // a command, such as "auth status", that never answers
	host     string // glab's default host
	protocol string // the git protocol
	exists   string // a shell pattern for the names repo view finds
	before   string // repo view's JSON for them
	after    string // repo view's JSON once repo create ran
	create   int    // repo create's exit status
}

func (c fakeCLI) write(t *testing.T, bin string) {
	t.Helper()
	q := func(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }
	exists := c.exists
	if exists == "" {
		exists = "-none-"
	}
	vars := "host=$GH_HOST repo=$GH_REPO prompt=$GH_PROMPT_DISABLED"
	if c.name == "glab" {
		vars = "host=$GITLAB_HOST repo=$GITLAB_REPO prompt=$GLAB_NO_PROMPT tokens=${GITLAB_TOKEN:+G}${GITLAB_ACCESS_TOKEN:+A}${OAUTH_TOKEN:+O}"
	}
	created := q(filepath.Join(bin, c.name+".created"))
	script := fmt.Sprintf(`#!/bin/sh
echo "$* %s" >> %s
if [ "$1 $2" = %s ]; then exec /bin/sleep 30; fi
case "$1 $2" in
"auth status") echo "Logged in to the host as fake-account-7f3a"; echo "Token: fake-token-7f3a" >&2; exit %d ;;
"config get") if [ "$3" = host ]; then echo %s; else echo %s; fi ;;
"repo view")
	if [ -f %s ]; then printf '%%s\n' %s; exit 0; fi
	case "$3" in %s) printf '%%s\n' %s ;; *) exit 1 ;; esac ;;
"repo create") if [ %d -eq 0 ]; then : > %s; fi; exit %d ;;
*) exit 2 ;;
esac
`, vars, q(filepath.Join(bin, c.name+".log")), q(c.hang), c.auth, q(c.host), q(c.protocol), created, q(c.after), exists, q(c.before), c.create, created, c.create)
	if err := os.WriteFile(filepath.Join(bin, c.name), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
}

func ghJSON(name, visibility string, empty bool) string {
	return fmt.Sprintf(`{"isEmpty":%[3]t,"sshUrl":"git@github.com:me/%[1]s.git","url":"https://github.com/me/%[1]s","visibility":%[2]q}`, name, visibility, empty)
}

func glabJSON(name, visibility string, empty bool) string {
	return fmt.Sprintf(`{"id":7,"path_with_namespace":"me/%[1]s","visibility":%[2]q,"empty_repo":%[3]t,"web_url":"https://gitlab.example.com/me/%[1]s","ssh_url_to_repo":"git@gitlab.example.com:me/%[1]s.git","http_url_to_repo":"https://gitlab.example.com/me/%[1]s.git"}`, name, visibility, empty)
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

// warm runs the fake name once and clears its log. macOS checks a new script
// on its first run, which can take longer than the short timeouts the tests
// that use this set.
func (f *fakeSetup) warm(t *testing.T, name string) {
	t.Helper()
	_ = exec.Command(filepath.Join(f.bin, name), "warm", "up").Run()
	if err := os.Remove(filepath.Join(f.bin, name+".log")); err != nil {
		t.Fatal(err)
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
	wantIn(t, "stdout", out, "pushed to?\n   Checking whether gh or glab is signed in.\n   glab isn't signed in to gitlab.example.com, so setup can't create the repository.\n   Create an empty private repository first")
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
				"auth status --hostname github.com host=github.com repo= prompt=1\n",
				"repo view eva-wiki --json visibility,isEmpty,sshUrl,url host=github.com repo= prompt=1\n",
				"repo create eva-wiki --private host=github.com repo= prompt=1\n",
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

func TestSetupGivesGlabTheEnvironmentTokenOnlyForItsDefaultHost(t *testing.T) {
	// glab's default host is gitlab.com, and a token for it is in the
	// environment: glab gets the token to check a remote there, never for
	// another host.
	for _, tc := range []struct{ host, tokens string }{
		{"gitlab.com", "tokens=GAO\n"},
		{"gitlab.example.com", "tokens=\n"},
	} {
		f := newFakeSetup(t, false).fakes(t, fakeCLI{name: "glab", host: "gitlab.com", exists: "me/eva-wiki", before: glabJSON("eva-wiki", "private", true)})
		for _, key := range []string{"GITLAB_TOKEN", "GITLAB_ACCESS_TOKEN", "OAUTH_TOKEN"} {
			t.Setenv(key, "fake-env-token-7f3a")
		}
		remote := "git@" + tc.host + ":me/eva-wiki.git"
		code, out, errOut := f.run("", "--dir", filepath.Join(f.env.home, "wiki"), "--remote", remote)
		f.wantRemote(t, code, out, errOut, remote)
		wantNotIn(t, "output", out+errOut, "fake-env-token")
		wantIn(t, "glab log", f.log(t, "glab"),
			"config get host host= repo= prompt=true tokens=\n",
			"auth status --hostname "+tc.host+" host="+tc.host+" repo= prompt=true "+tc.tokens,
			"repo view me/eva-wiki --output json host="+tc.host+" repo= prompt=true "+tc.tokens)
	}
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

func TestSetupFallsBackWhenACLIHangs(t *testing.T) {
	defer func(d time.Duration) { repoCLITimeout = d }(repoCLITimeout)
	repoCLITimeout = time.Second
	for _, tc := range []struct {
		cli    fakeCLI
		remote string
	}{
		{fakeCLI{name: "gh", hang: "auth status"}, "git@github.com:me/eva-wiki.git"},
		{fakeCLI{name: "glab", hang: "config get"}, "git@gitlab.example.com:me/eva-wiki.git"},
	} {
		f := newFakeSetup(t, true).fakes(t, tc.cli)
		f.warm(t, tc.cli.name)
		start := time.Now()
		code, out, errOut := f.answers(t, tc.remote, "y")
		f.wantRemote(t, code, out, errOut, tc.remote)
		if !strings.Contains(out, "   "+tc.cli.name+" didn't answer in time, so setup can't create the repository.\n   Create an empty private repository first") {
			t.Fatalf("%s's timeout not reported:\n%s", tc.cli.name, out)
		}
		if waited := time.Since(start); waited > 10*time.Second {
			t.Fatalf("setup waited %s for a CLI that hangs", waited)
		}
		// It isn't asked again about the pasted remote.
		if log := f.log(t, tc.cli.name); strings.Count(log, "\n") != 1 || !strings.HasPrefix(log, tc.cli.hang+" ") {
			t.Fatalf("%s asked more than once, or not as expected:\n%s", tc.cli.name, log)
		}
	}
}

func TestSetupCreateThatHangsMayHaveMadeTheRepo(t *testing.T) {
	// repo create doesn't answer in time, though the host may have made the
	// repo: setup says so, and doesn't claim it changed nothing.
	defer func(d time.Duration) { repoCLITimeout = d }(repoCLITimeout)
	repoCLITimeout = time.Second
	f := newFakeSetup(t, true).fakes(t, fakeCLI{name: "gh", hang: "repo create"})
	f.warm(t, "gh")
	code, out, _ := f.answers(t, "", "git@github.com:me/eva-wiki.git", "n")
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
		f.bareRepo(t, "eva-memory")
		const refusal = "   This repo has content but isn't an eva wiki; use an empty private repo, or the repo of your eva wiki\n   Another name, or an address: "
		code, out, errOut := f.answers(t, "", "")
		if code != 1 || !strings.Contains(out, contentQuestion+checking+refusal) || !strings.Contains(errOut, "nothing was changed") {
			t.Fatalf("code %d\n%s\n%s", code, out, errOut)
		}
		if _, err := os.Stat(filepath.Join(f.env.home, "projects", "wiki")); !errors.Is(err, fs.ErrNotExist) {
			t.Fatalf("the folder was made: %v", err)
		}
		f.noTempLeft(t)
		code, out, errOut = f.answers(t, "", "", "eva-memory", "y")
		f.wantRemote(t, code, out, errOut, "git@github.com:me/eva-memory.git")
		wantIn(t, "stdout", out, refusal, "Setup will create a new wiki in this folder and push it, then install the plugins.\n", "  Wiki ready in ")
		wantIn(t, "gh log", f.log(t, "gh"), "repo create eva-memory --private")
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
	f.bareRepo(t, "empty-wiki")
	folder := filepath.Join(f.env.home, "projects", "wiki")

	t.Run("one git can't read", func(t *testing.T) {
		code, out, _ := f.answers(t, "git@github.com:me/missing.git")
		if code != 1 || !strings.Contains(out, "   Remote: "+strings.Replace(readable, "eva-wiki", "missing", 1)+checking+"   Git couldn't read this repo; check its address and that git can sign in to it\n   Remote: ") {
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
		code, out, errOut := f.answers(t, "git@github.com:me/notes.git", "git@github.com:me/empty-wiki.git", "y")
		f.wantRemote(t, code, out, errOut, "git@github.com:me/empty-wiki.git")
		wantIn(t, "stdout", out,
			checking+"   This repo has content but isn't an eva wiki; use an empty private repo, or the repo of your eva wiki\n   Remote: ",
			checking+"   The repo is empty: setup will create the wiki in it.\n",
			"  Wiki ready in "+folder+"\n")
	})
}

func TestSetupWarnsAboutAWikiFromAnotherMac(t *testing.T) {
	f := newFakeSetup(t, true)
	f.realGit(t)
	f.bareRepo(t, "eva-wiki")
	evaWiki(t, filepath.Join(t.TempDir(), "wiki"), "git@github.com:me/eva-wiki.git", "old-laptop")

	// With --remote in a terminal, it asks too, and no is the default.
	code, out, errOut := f.run("\n", "--dir", filepath.Join(f.env.home, "projects", "wiki"), "--remote", "git@github.com:me/eva-wiki.git")
	if code != 1 || len(f.inits) != 0 || !strings.HasSuffix(out, checking+otherMac) || errOut != "eva-md: Choose another repo for this Mac\n" {
		t.Fatalf("code %d inits %d\n%s\n%s", code, len(f.inits), out, errOut)
	}

	// Enter keeps the default; asked again, the user says yes.
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
