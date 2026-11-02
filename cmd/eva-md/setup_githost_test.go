package main

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// The host's name says GitHub or GitLab first: github.com, a name holding
// github or ending in .ghe.com is GitHub; gitlab.com or a name holding
// gitlab is GitLab; a name holding both, or neither, doesn't tell.
func TestHostKind(t *testing.T) {
	for host, want := range map[string]string{
		"github.com":                "gh",
		"gitlab.com":                "glab",
		"gitlab.example.com":        "glab",
		"code-gitlab.example.org":   "glab",
		"gitlab.example.com:8443":   "glab",
		"github.acme.example":       "gh",
		"acme-github.example.com":   "gh",
		"acme.ghe.com":              "gh",
		"gitlab.github.example.com": "",
		"github-gitlab.example.org": "",
		"code.example.com":          "",
		"ghe.example.com":           "",
		"acme.ghe.com.example.org":  "",
	} {
		if got := hostKind(host); got != want {
			t.Errorf("hostKind(%q) = %q, want %q", host, got, want)
		}
		if want != "" && !slices.Equal(hostCLIs(host), []string{want}) {
			t.Errorf("hostCLIs(%q) = %v", host, hostCLIs(host))
		}
		if want == "" && !slices.Equal(hostCLIs(host), []string{"gh", "glab"}) {
			t.Errorf("hostCLIs(%q) = %v", host, hostCLIs(host))
		}
	}
}

// With a git host, from --git-host or EVA_MD_GIT_HOST_URL, setup offers to
// create the repo only there, with the CLI for it: the one the host's name
// says, asking only that one when it is ready there, or else the other when
// that one is signed in there, as for a GitHub Enterprise server whose name
// holds gitlab; for a name that doesn't tell, the one signed in there, or the
// one the user chooses of two. Every call about the repo points that CLI at
// the host, in its own environment only, and glab is never asked for its
// default host.
func TestSetupPicksTheCLIForTheGitHost(t *testing.T) {
	for _, tc := range []struct {
		name, host    string
		gh, glab      string // the hosts each is signed in to
		answers       []string
		viaFile       bool
		cli, protocol string
		prompt        string // what setup asked
		byName        bool   // the name tells and its CLI is ready: the other is never run
	}{
		{name: "github.com", host: "github.com", gh: "*", glab: "*", cli: "gh", byName: true},
		{name: "gitlab.com", host: "gitlab.com", gh: "*", glab: "*", cli: "glab", viaFile: true, byName: true},
		{name: "a gitlab-named host", host: "code-gitlab.example.org", gh: "*", glab: "*", cli: "glab", byName: true},
		{name: "a github-named host", host: "github.acme.example", gh: "*", glab: "*", cli: "gh", protocol: "https", byName: true},
		{name: "a .ghe.com host", host: "acme.ghe.com", gh: "*", glab: "*", cli: "gh", viaFile: true, byName: true},
		{name: "a GitHub Enterprise server whose name holds gitlab", host: "ghe-gitlab.example.com", gh: "*", glab: "none.invalid", cli: "gh"},
		{name: "a GitLab whose name holds github", host: "code-github.example.org", gh: "none.invalid", glab: "*", cli: "glab", viaFile: true},
		{name: "a name holding both", host: "gitlab.github.example.com", gh: "none.invalid", glab: "*", cli: "glab"},
		{name: "gh on a neutral host", host: "code.example.com", gh: "code.example.com", glab: "none.invalid", cli: "gh", protocol: "https"},
		{name: "glab on a neutral host", host: "code.example.com", gh: "none.invalid", glab: "code.example.com", cli: "glab", viaFile: true},
		{name: "both on a neutral host, asked", host: "code.example.com", gh: "*", glab: "*", cli: "glab", answers: []string{"neither", "glab"},
			prompt: "   gh and glab are both signed in to code.example.com.\n   Is it GitHub or GitLab? [gh/glab]    Type gh or glab\n   Is it GitHub or GitLab? [gh/glab]    Setup can create it for you."},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFakeSetup(t, true).fakes(t,
				fakeCLI{name: "gh", signedIn: tc.gh, protocol: tc.protocol, after: ghJSONOn(tc.host, "eva-wiki", "PRIVATE", true)},
				fakeCLI{name: "glab", signedIn: tc.glab, host: "gitlab.example.com", after: glabJSONOn(tc.host, "eva-wiki", "private", true)})
			// Any page on the host names it.
			var args []string
			if tc.viaFile {
				f.answersFile(t, "EVA_MD_GIT_HOST_URL=https://"+tc.host+"/users/your-username/projects\n")
			} else {
				args = []string{"--git-host", "https://" + tc.host + "/your-username"}
			}
			input := strings.Join(append(append([]string{"~/projects/wiki"}, tc.answers...), "", "y"), "\n") + "\n"
			code, out, errOut := f.run(input, args...)
			noCLIText(t, out+errOut)
			remote := "git@" + tc.host + ":me/eva-wiki.git"
			if tc.protocol == "https" {
				remote = "https://" + tc.host + "/me/eva-wiki.git"
			}
			f.wantRemote(t, code, out, errOut, remote)
			other := map[string]string{"gh": "glab", "glab": "gh"}[tc.cli]
			signedIn := "  ✓ " + tc.cli + " signed in to " + tc.host + "\n"
			if tc.answers != nil {
				signedIn = "  ✓ gh and glab signed in to " + tc.host + "\n"
			}
			wantIn(t, "stdout", out, signedIn,
				"   Create a private repo named eva-wiki with "+tc.cli+" on "+tc.host+"? [Y/n] ",
				"   Created "+remote+", a private repo on "+tc.host+".\n", tc.prompt)
			wantNotIn(t, "stdout", out, "Which one? [GH/glab/n]")
			if tc.protocol == "https" {
				wantIn(t, "stdout", out, "   Git pushes over HTTPS, as gh is set to. If git can't sign in, run:\n     gh auth setup-git --hostname "+tc.host+"\n")
			}
			// The CLI picked was pointed at the host for each call about the
			// repo, and asked once whether it is signed in there.
			log := f.log(t, tc.cli)
			wantIn(t, tc.cli+" log", log, "auth status --hostname "+tc.host+" host="+tc.host+" ", "repo view eva-wiki ", "repo create eva-wiki --private")
			if n := strings.Count(log, "auth status"); n != 1 {
				t.Errorf("%s was asked %d times whether it is signed in:\n%s", tc.cli, n, log)
			}
			for _, line := range strings.Split(strings.TrimSpace(log), "\n") {
				if strings.HasPrefix(line, "repo ") && !strings.Contains(line, " host="+tc.host+" ") {
					t.Errorf("%s wasn't pointed at %s: %s", tc.cli, tc.host, line)
				}
			}
			// With a host known, glab is never asked for its default host.
			wantNotIn(t, "glab log", f.log(t, "glab"), "config get host")
			// Nothing reached eva-md's own environment.
			if os.Getenv("GH_HOST") != "" || os.Getenv("GITLAB_HOST") != "" {
				t.Errorf("GH_HOST=%q GITLAB_HOST=%q", os.Getenv("GH_HOST"), os.Getenv("GITLAB_HOST"))
			}
			if tc.byName {
				// The name told and its CLI was ready: the other was never run.
				if l := f.log(t, other); l != "" {
					t.Errorf("%s was run for %s:\n%s", other, tc.host, l)
				}
				return
			}
			// The other was asked once whether it is signed in there, pointed
			// at the host, and did nothing with the repo.
			if l := f.log(t, other); !strings.HasPrefix(l, "auth status --hostname "+tc.host+" host="+tc.host+" ") || strings.Count(l, "\n") != 1 {
				t.Errorf("%s log:\n%s", other, l)
			}
		})
	}
}

// With only a git host, setup has to create the repo there: a CLI for it
// must be ready, and when neither is, setup stops in its checks with the
// exact fix for the CLI the host's name says, after asking the other only
// whether it is signed in there, and asks nothing after it.
func TestSetupStopsWhenTheNamedHostsCLIIsNotReady(t *testing.T) {
	for _, tc := range []struct {
		name, host string
		clis       []fakeCLI
		args       []string
		file       string
		fail, fix  string
	}{
		{name: "glab missing", host: "gitlab.example.com", args: []string{"--git-host", "gitlab.example.com"},
			clis: []fakeCLI{{name: "gh", signedIn: "github.com"}},
			fail: "glab isn't installed", fix: "install glab (brew install glab), then run glab auth login --hostname gitlab.example.com"},
		{name: "glab signed out", host: "gitlab.example.com", file: "EVA_MD_GIT_HOST_URL=https://gitlab.example.com/your-username\n",
			clis: []fakeCLI{{name: "gh", signedIn: "github.com"}, {name: "glab", signedIn: "gitlab.com", host: "gitlab.com"}},
			fail: "glab isn't signed in to gitlab.example.com", fix: "glab auth login --hostname gitlab.example.com"},
		{name: "gh signed out", host: "github.com", args: []string{"--git-host", "https://github.com/your-username"},
			clis: []fakeCLI{{name: "gh", signedIn: "ghe.example.com"}, {name: "glab", signedIn: "gitlab.com", host: "gitlab.com"}},
			fail: "gh isn't signed in to github.com", fix: "gh auth login --hostname github.com"},
		{name: "gh missing for .ghe.com, from the file", host: "acme.ghe.com", file: "EVA_MD_GIT_HOST_URL=https://acme.ghe.com/orgs/your-org\n",
			clis: []fakeCLI{{name: "glab", signedIn: "gitlab.com", host: "gitlab.com"}},
			fail: "gh isn't installed", fix: "install gh (brew install gh), then run gh auth login --hostname acme.ghe.com"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFakeSetup(t, true).fakes(t, tc.clis...)
			if tc.file != "" {
				f.answersFile(t, tc.file)
			}
			code, out, errOut := f.run("~/projects/wiki\ngit@gitlab.example.com:me/eva-wiki.git\ny\n", tc.args...)
			noCLIText(t, out+errOut)
			if code != 1 || len(f.inits) != 0 || len(f.installs) != 0 || errOut != "" {
				t.Fatalf("code %d inits %d installs %d\n%s\n%s", code, len(f.inits), len(f.installs), out, errOut)
			}
			if want := "  ✗ " + tc.fail + "\n    Fix: " + tc.fix + "\n    Then run setup again.\n"; !strings.HasSuffix(out, want) {
				t.Fatalf("stdout doesn't end with %q:\n%s", want, out)
			}
			kind := strings.Fields(tc.fail)[0]
			other := map[string]string{"gh": "glab", "glab": "gh"}[kind]
			if l := f.log(t, other); l != "" && (!strings.HasPrefix(l, "auth status --hostname "+tc.host+" ") || strings.Count(l, "\n") != 1) {
				t.Errorf("%s did more than say whether it is signed in to %s:\n%s", other, tc.host, l)
			}
		})
	}
}

// A remote given before the questions, by --remote or EVA_MD_WIKI_REMOTE,
// needs no CLI: when none is ready for its host, the checks say in a ! line,
// naming the CLI the host's name says, that setup will check the repo is
// private by reading it without signing in, and setup goes on, as it did
// before it had checks. A remote through an SSH alias from ~/.ssh/config is
// such a case: no CLI is signed in to the alias.
func TestSetupGoesOnWithAGivenRemoteWhenNoCLIIsReady(t *testing.T) {
	for _, tc := range []struct {
		name, remote, read, warn string
		clis                     []fakeCLI
		viaFile                  bool
	}{
		{name: "gh signed out", remote: "git@github.com:me/eva-wiki.git", read: "github.com/me/eva-wiki",
			clis: []fakeCLI{{name: "gh", signedIn: "ghe.example.com"}, {name: "glab", signedIn: "gitlab.com", host: "gitlab.com"}},
			warn: "gh isn't signed in to github.com"},
		{name: "gh missing for .ghe.com, from the file", remote: "git@acme.ghe.com:me/eva-wiki.git", read: "acme.ghe.com/me/eva-wiki", viaFile: true,
			clis: []fakeCLI{{name: "glab", signedIn: "gitlab.com", host: "gitlab.com"}},
			warn: "gh isn't installed"},
		{name: "glab signed out", remote: "git@gitlab.example.com:me/eva-wiki.git", read: "gitlab.example.com/me/eva-wiki",
			clis: []fakeCLI{{name: "glab", auth: 1}},
			warn: "glab isn't signed in to gitlab.example.com"},
		{name: "an SSH alias", remote: "git@github.com-alt:me/eva-wiki.git", read: "github.com-alt/me/eva-wiki",
			clis: []fakeCLI{{name: "gh", signedIn: "github.com"}, {name: "glab", signedIn: "gitlab.com", host: "gitlab.com"}},
			warn: "gh isn't signed in to github.com-alt"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFakeSetup(t, false).fakes(t, tc.clis...)
			dir := filepath.Join(f.env.home, "wiki")
			args := []string{"--dir", dir, "--remote", tc.remote}
			if tc.viaFile {
				f.answersFile(t, "EVA_MD_WIKI_DIR="+dir+"\nEVA_MD_WIKI_REMOTE="+tc.remote+"\n")
				args = nil
			}
			code, out, errOut := f.run("", args...)
			noCLIText(t, out+errOut)
			f.wantRemote(t, code, out, errOut, tc.remote)
			wantIn(t, "stdout", out, "  ! "+tc.warn+": setup will check the repo is private by reading it without signing in\n")
			wantNotIn(t, "stdout", out, "✗", "Fix:")
			if !slices.Equal(f.reads, []string{tc.read}) {
				t.Fatalf("read %q with no sign-in, want %s", f.reads, tc.read)
			}
		})
	}
}

// GitHub's SSH host ssh.github.com and GitLab's altssh.gitlab.com, on port
// 443, are read as github.com and gitlab.com: the remote's repo is checked
// with gh or glab signed in there, never with a read without signing in.
func TestSetupReadsTheSSHHostsAsGitHubAndGitLab(t *testing.T) {
	for _, tc := range []struct {
		name, remote, cli, host, view string
		public                        bool
		clis                          []fakeCLI
	}{
		{name: "ssh.github.com", remote: "ssh://git@ssh.github.com:443/me/eva-wiki.git", cli: "gh", host: "github.com", public: true,
			view: "repo view me/eva-wiki --json visibility,isEmpty,sshUrl,url host=github.com ",
			clis: []fakeCLI{{name: "gh", signedIn: "github.com", exists: "me/eva-wiki", before: ghJSON("eva-wiki", "PUBLIC", true)}, {name: "glab", signedIn: "gitlab.com", host: "gitlab.com"}}},
		{name: "altssh.gitlab.com", remote: "ssh://git@altssh.gitlab.com:443/me/eva-wiki.git", cli: "glab", host: "gitlab.com",
			view: "repo view me/eva-wiki --output json host=gitlab.com ",
			clis: []fakeCLI{{name: "gh", signedIn: "github.com"}, {name: "glab", signedIn: "gitlab.com", host: "gitlab.com", exists: "me/eva-wiki", before: glabJSONOn("gitlab.com", "eva-wiki", "private", true)}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFakeSetup(t, false).fakes(t, tc.clis...)
			code, out, errOut := f.run("", "--dir", filepath.Join(f.env.home, "wiki"), "--remote", tc.remote)
			noCLIText(t, out+errOut)
			switch {
			case !tc.public:
				f.wantRemote(t, code, out, errOut, tc.remote)
			case code != 1 || len(f.inits) != 0 || errOut != "eva-md: This repo is public; the wiki holds your chats' summaries, so use a private repo\n":
				t.Fatalf("code %d inits %d\n%s\n%s", code, len(f.inits), out, errOut)
			}
			wantIn(t, "stdout", out, "  ✓ "+tc.cli+" signed in to "+tc.host+"\n")
			log := f.log(t, tc.cli)
			wantIn(t, tc.cli+" log", log, "auth status --hostname "+tc.host+" host="+tc.host+" ", tc.view)
			wantNotIn(t, tc.cli+" log", log, "ssh.github.com", "altssh")
			if len(f.reads) != 0 {
				t.Fatalf("read %q with no sign-in", f.reads)
			}
		})
	}
}

// glab is asked about a known host directly, pointed at it, never for its
// default host: a glab signed in there whose default host is one setup can't
// use, as a GitLab served under a path, passes. With no host known, a glab
// that can't name its default host says so in its information line.
func TestSetupAsksGlabAboutAKnownHostDirectly(t *testing.T) {
	glab := fakeCLI{name: "glab", signedIn: "*", host: "https://gitlab.example.com/gitlab", after: glabJSON("eva-wiki", "private", true)}
	f := newFakeSetup(t, true).fakes(t, glab)
	code, out, errOut := f.run("~/projects/wiki\n\ny\n", "--git-host", "gitlab.example.com")
	noCLIText(t, out+errOut)
	f.wantRemote(t, code, out, errOut, "git@gitlab.example.com:me/eva-wiki.git")
	wantIn(t, "stdout", out, "  ✓ glab signed in to gitlab.example.com\n")
	log := f.log(t, "glab")
	if !strings.HasPrefix(log, "auth status --hostname gitlab.example.com host=gitlab.example.com ") {
		t.Fatalf("glab log:\n%s", log)
	}
	wantNotIn(t, "glab log", log, "config get host")

	f = newFakeSetup(t, true).fakes(t, glab, fakeCLI{name: "gh", signedIn: "none.invalid"})
	code, out, errOut = f.answers(t, "git@code.example.com:me/eva-wiki.git", "y")
	f.wantRemote(t, code, out, errOut, "git@code.example.com:me/eva-wiki.git")
	wantIn(t, "stdout", out, "  - gh isn't signed in to github.com\n  - glab can't name its default host\n")
	wantNotIn(t, "stdout", out, "glab isn't signed in")
}

// A host whose name doesn't tell and that no CLI is signed in to is a plain
// git server: with the remote given, setup goes on with plain git after a !
// line; with only the git host, it stops, saying to create the repo and give
// its address.
func TestSetupWorksWithAPlainGitServer(t *testing.T) {
	clis := []fakeCLI{{name: "gh", signedIn: "github.com"}, {name: "glab", signedIn: "gitlab.com", host: "gitlab.com"}}
	f := newFakeSetup(t, false).fakes(t, clis...)
	const remote = "git@code.example.com:me/eva-wiki.git"
	code, out, errOut := f.run("", "--dir", filepath.Join(f.env.home, "wiki"), "--remote", remote)
	f.wantRemote(t, code, out, errOut, remote)
	wantIn(t, "stdout", out, "  ! No gh or glab is signed in to code.example.com: setup will check the repo is private by reading it without signing in\n")
	if !slices.Equal(f.reads, []string{"code.example.com/me/eva-wiki"}) {
		t.Fatalf("read %q with no sign-in", f.reads)
	}

	f = newFakeSetup(t, true).fakes(t, clis...)
	code, out, errOut = f.run("~/projects/wiki\n"+remote+"\ny\n", "--git-host", "code.example.com")
	if code != 1 || len(f.inits) != 0 || errOut != "" {
		t.Fatalf("code %d inits %d\n%s\n%s", code, len(f.inits), out, errOut)
	}
	if want := "  ✗ Setup can't create the repo on code.example.com: no gh or glab is signed in there\n    Fix: create the repo yourself and give its address in EVA_MD_WIKI_REMOTE or --remote\n    Then run setup again.\n"; !strings.HasSuffix(out, want) {
		t.Fatalf("stdout doesn't end with %q:\n%s", want, out)
	}
}

// With no host known yet, as in a terminal with no answers, the checks say
// which of gh and glab are installed and signed in where, as information,
// and setup goes on; the offer then asks neither again.
func TestSetupNotesTheCLIsWhenNoHostIsKnown(t *testing.T) {
	f := newFakeSetup(t, true).fakes(t,
		fakeCLI{name: "gh", signedIn: "*", after: ghJSON("eva-wiki", "PRIVATE", true)},
		fakeCLI{name: "glab", signedIn: "*", host: "gitlab.example.com", after: glabJSON("eva-wiki", "private", true)})
	code, out, errOut := f.answers(t, "", "", "y")
	f.wantRemote(t, code, out, errOut, "git@github.com:me/eva-wiki.git")
	wantIn(t, "stdout", out, "  ✓ git\n  ✓ Claude Code and Codex\n  ✓ Wrote ", "\n  ✓ gh signed in to github.com\n  ✓ glab signed in to gitlab.example.com\n  ✓ Commit signing on (ssh)\n",
		"pushed to?\n   Setup can create it with gh on github.com or glab on gitlab.example.com.\n")
	if log := f.log(t, "gh"); !strings.HasPrefix(log, "auth status --hostname github.com ") || strings.Count(log, "auth status") != 1 {
		t.Fatalf("gh log:\n%s", log)
	}
	wantIn(t, "glab log", f.log(t, "glab"), "config get host host= ", "auth status --hostname gitlab.example.com ")
	wantNotIn(t, "glab log", f.log(t, "glab"), "github.com", "repo ")

	// Neither ready: the lines say so, and setup asks for an address.
	f = newFakeSetup(t, true).fakes(t, fakeCLI{name: "gh", signedIn: "none.invalid"})
	code, out, errOut = f.answers(t, "git@code.example.com:me/eva-wiki.git", "y")
	f.wantRemote(t, code, out, errOut, "git@code.example.com:me/eva-wiki.git")
	wantIn(t, "stdout", out, "  - gh isn't signed in to github.com\n  - glab isn't installed\n", "1. Where should the wiki live")
}

// Without a terminal, a remote on a host gh and glab are both signed in to,
// whose name says neither, stops setup in its checks: it can't ask which one
// to check the repo with.
func TestSetupStopsWhenItCantTellTheCLIForAHost(t *testing.T) {
	f := newFakeSetup(t, false).fakes(t, fakeCLI{name: "gh", signedIn: "*"}, fakeCLI{name: "glab", signedIn: "*", host: "gitlab.com"})
	code, out, errOut := f.run("", "--dir", f.env.home+"/wiki", "--remote", "git@code.example.com:me/eva-wiki.git")
	noCLIText(t, out+errOut)
	if code != 1 || len(f.inits) != 0 || errOut != "" || !strings.HasSuffix(out, "  ✗ gh and glab are both signed in to code.example.com, so setup can't tell which it is\n    Fix: run setup in a terminal to choose\n    Then run setup again.\n") {
		t.Fatalf("code %d inits %d\n%s\n%s", code, len(f.inits), out, errOut)
	}
}

// A given remote on a GitHub Enterprise server is checked with gh, signed in
// there, not with glab or a read with no sign-in.
func TestSetupChecksAGitHubEnterpriseRemoteWithGH(t *testing.T) {
	const remote = "git@ghe.example.com:me/eva-wiki.git"
	for _, tc := range []struct {
		visibility string
		code       int
	}{{"PUBLIC", 1}, {"PRIVATE", 0}} {
		f := newFakeSetup(t, false).fakes(t,
			fakeCLI{name: "gh", signedIn: "ghe.example.com", exists: "me/eva-wiki", before: ghJSONOn("ghe.example.com", "eva-wiki", tc.visibility, true)},
			fakeCLI{name: "glab", signedIn: "none.invalid", host: "gitlab.com"})
		code, out, errOut := f.run("", "--dir", f.env.home+"/wiki", "--remote", remote)
		noCLIText(t, out+errOut)
		if code != tc.code {
			t.Fatalf("%s: code %d\n%s\n%s", tc.visibility, code, out, errOut)
		}
		if tc.code == 1 && errOut != "eva-md: This repo is public; the wiki holds your chats' summaries, so use a private repo\n" {
			t.Fatalf("stderr %q", errOut)
		}
		wantIn(t, "gh log", f.log(t, "gh"), "repo view me/eva-wiki --json visibility,isEmpty,sshUrl,url host=ghe.example.com ")
		wantNotIn(t, "glab log", f.log(t, "glab"), "repo view")
		if len(f.reads) != 0 {
			t.Fatalf("read %q with no sign-in", f.reads)
		}
	}
}

// No token from the environment reaches gh or glab, on any call setup makes
// (auth status, repo view, repo create, config get), for GitHub, a GitHub
// Enterprise server, gitlab.com or a self-hosted GitLab: each uses only its
// own sign-in.
func TestSetupNeverGivesTheCLIsATokenFromTheEnvironment(t *testing.T) {
	tokens := func(t *testing.T) {
		for _, key := range envTokens {
			t.Setenv(key, "fake-env-token-7f3a")
		}
	}
	none := func(t *testing.T, f *fakeSetup, out string) {
		t.Helper()
		wantNotIn(t, "output", out, "fake-env-token")
		for _, name := range []string{"gh", "glab"} {
			for _, line := range strings.Split(strings.TrimSpace(f.log(t, name)), "\n") {
				if line != "" && !strings.HasSuffix(line, " tokens=") {
					t.Errorf("%s got a token: %s", name, line)
				}
			}
		}
	}
	for _, tc := range []struct{ host, cli, view string }{
		{"github.com", "gh", "repo view eva-wiki --json "},
		{"github.example.com", "gh", "repo view eva-wiki --json "},
		{"gitlab.com", "glab", "repo view eva-wiki --output json "},
		{"gitlab.example.com", "glab", "repo view eva-wiki --output json "},
	} {
		t.Run(tc.host, func(t *testing.T) {
			f := newFakeSetup(t, true).fakes(t,
				fakeCLI{name: "gh", signedIn: "*", after: ghJSONOn(tc.host, "eva-wiki", "PRIVATE", true)},
				fakeCLI{name: "glab", signedIn: "*", host: "gitlab.example.com", after: glabJSONOn(tc.host, "eva-wiki", "private", true)})
			tokens(t)
			code, out, errOut := f.run("~/projects/wiki\n\ny\n", "--git-host", tc.host)
			f.wantRemote(t, code, out, errOut, "git@"+tc.host+":me/eva-wiki.git")
			wantIn(t, tc.cli+" log", f.log(t, tc.cli), "auth status --hostname "+tc.host+" ", tc.view, "repo create eva-wiki --private", "config get git_protocol --host "+tc.host+" ")
			none(t, f, out+errOut)
			// Setup's own environment keeps them.
			if os.Getenv("GITLAB_TOKEN") != "fake-env-token-7f3a" {
				t.Fatal("eva-md's own environment changed")
			}
		})
	}
	// With no host known: gh at github.com and glab naming its default host.
	f := newFakeSetup(t, true).fakes(t,
		fakeCLI{name: "gh", signedIn: "*", after: ghJSON("eva-wiki", "PRIVATE", true)},
		fakeCLI{name: "glab", signedIn: "*", host: "gitlab.example.com"})
	tokens(t)
	code, out, errOut := f.answers(t, "", "", "y")
	f.wantRemote(t, code, out, errOut, "git@github.com:me/eva-wiki.git")
	wantIn(t, "glab log", f.log(t, "glab"), "config get host ", "auth status --hostname gitlab.example.com ")
	none(t, f, out+errOut)
}
