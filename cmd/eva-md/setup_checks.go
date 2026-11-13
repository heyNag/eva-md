package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/heyNag/eva-md/internal/config"
	"github.com/heyNag/eva-md/internal/host/adapter"
	"github.com/heyNag/eva-md/internal/install"
	"github.com/heyNag/eva-md/internal/wikiinit"
)

// Setup's first step checks this Mac for what setup needs, in one block,
// before it asks anything: one line for each check that passes, and at the
// first that fails, what is wrong, the exact fix and "Then run setup
// again.", so a person fixes everything first and runs setup once more.

// checklist prints the lines of the "Checking this Mac" block.
type checklist struct {
	out   io.Writer
	color bool // the marks are coloured: stdout is a terminal and NO_COLOR is unset
}

// The marks' colours.
const (
	green  = "\x1b[32m"
	red    = "\x1b[31m"
	yellow = "\x1b[33m"
	reset  = "\x1b[0m"
)

func (c checklist) line(mark, color, what string) {
	if c.color && color != "" {
		mark = color + mark + reset
	}
	fmt.Fprintf(c.out, "  %s %s\n", mark, what)
}

// pass is a check that passed, saying what it found.
func (c checklist) pass(what string) { c.line("✓", green, what) }

// note is information that is neither a pass nor a problem.
func (c checklist) note(what string) { c.line("-", "", what) }

// warn is information that may need attention but doesn't stop setup.
func (c checklist) warn(what string) { c.line("!", yellow, what) }

// fail is the check that stops setup: what is wrong and how to fix it.
func (c checklist) fail(what, fix string) {
	c.line("✗", red, what)
	fmt.Fprintf(c.out, "    Fix: %s\n", fix)
	fmt.Fprintln(c.out, "    Then run setup again.")
}

// useColor reports whether setup colours its marks: only when stdout is a
// terminal and NO_COLOR is unset or empty, as no-color.org asks.
func useColor(stdout *os.File, noColor string) bool {
	return noColor == "" && isTerminal(stdout)
}

// wikiHost is the host of the wiki's repo as the flags or the answers file
// name it: the remote's, as apiHost gives it, or else the git host.
// hasRemote is set when a remote is given; a local one has no host.
func wikiHost(remote, gitHost string) (host string, hasRemote bool) {
	if remote == "" {
		return gitHost, false
	}
	id, err := wikiinit.CheckRemote(remote)
	if err != nil || strings.HasPrefix(id, "file://") {
		return "", true
	}
	host, _, _ = strings.Cut(id, "/")
	return apiHost(host), true
}

// checkHostCLI checks the CLI for the wiki's host, as signedInOn finds it,
// and says what it found: one signed in there passes, and so do both in a
// terminal, where setup asks later which it is. With none ready, a given
// remote goes on with a ! line, since setup then checks the repo by reading
// it without signing in; with only the git host, setup can't create the
// repo and stops with the fix, which names the CLI the host's name says.
// It returns false when setup must stop.
func checkHostCLI(ctx context.Context, ck checklist, r *repoCLIs, host string, hasRemote, interactive bool) bool {
	clis, reasons := r.signedInOn(ctx, host)
	switch {
	case len(clis) == 1:
		ck.pass(clis[0].name + " signed in to " + host)
		return true
	case len(clis) == 2 && interactive:
		ck.pass("gh and glab signed in to " + host)
		return true
	case len(clis) == 2:
		ck.fail("gh and glab are both signed in to "+host+", so setup can't tell which it is", "run setup in a terminal to choose")
		return false
	}
	what := "No gh or glab is signed in to " + host
	fix := "create the repo yourself and give its address in EVA_MD_WIKI_REMOTE or --remote"
	if kind := hostKind(host); kind != "" {
		login := kind + " auth login --hostname " + host
		switch err := reasons[kind]; {
		case errors.Is(err, errCLIMissing):
			what, fix = kind+" isn't installed", "install "+kind+" (brew install "+kind+"), then run "+login
		case errors.Is(err, errCLITimedOut):
			what, fix = kind+" didn't answer in time", "check that "+kind+" auth status --hostname "+host+" answers"
		default:
			what, fix = kind+" isn't signed in to "+host, login
		}
	} else if !hasRemote {
		what = "Setup can't create the repo on " + host + ": no gh or glab is signed in there"
	}
	if hasRemote {
		ck.warn(what + ": setup will check the repo is private by reading it without signing in")
		return true
	}
	ck.fail(what, fix)
	return false
}

// checkInstall runs install's checks before it changes anything, as a dry
// run that needs no config, so that whatever install would refuse, such as
// Codex with its hooks feature off, stops setup here, before it makes or
// pushes a wiki. hosts names the apps setup found for the plugins. Codex's
// hooks feature off gets both ways out: turning it on, or installing for
// Claude Code only, with --host when the flag chose the hosts (hostFlag),
// and in the answers file otherwise, after installing Claude Code when this
// Mac has no claude command. Any other refusal is said as install words it,
// with its fix. It returns false when setup must stop.
func checkInstall(ctx context.Context, ck checklist, env setupEnv, opts install.Options, hosts []string, hostFlag bool, answersFile string) bool {
	ctx, cancel := context.WithTimeout(ctx, installTimeout)
	_, err := env.preflight(ctx, opts)
	cancel()
	if err == nil {
		ck.pass("The plugins can be installed for " + strings.Join(hosts, " and "))
		return true
	}
	if errors.Is(err, install.ErrHooksOff) {
		only := "set " + config.KeyHosts + "=claude in " + answersFile
		if hostFlag {
			only = "run setup with --host claude"
		}
		// Claude Code is looked for by its command, as the host check
		// looks, not among the hosts: --host codex leaves it out of them.
		if claudeCode, _ := adapter.ByID(string(adapter.ClaudeCode)); claudeCode.FindCLI(env.lookPath) == "" {
			only = "install Claude Code, then " + only
		}
		ck.fail("Codex's hooks feature is off, so eva-md can't capture Codex chats", "run codex features enable hooks, or "+only+" to install for Claude Code only")
		return false
	}
	what, fix, _ := strings.Cut(strings.TrimPrefix(err.Error(), "install: "), "; ")
	switch command, failed := install.FailedCommand(err); {
	case errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded):
		ck.fail("Install's checks of the apps were stopped or ran out of time", "check that claude plugin list and codex plugin marketplace list answer in a terminal")
		return false
	case failed:
		// The host's own words can name paths or credentials, so install
		// never shows them; the person can run the command.
		fix = "run " + command + " to see why"
	case fix != "":
	case errors.Is(err, install.ErrConflict):
		fix = "remove the other eva-md registration with that app's plugin commands"
	default:
		fix = "fix that first; setup makes no wiki until install's checks pass"
	}
	ck.fail("The plugins can't be installed: "+what, fix)
	return false
}

// noteCLIs says, as information, which of gh and glab are installed and
// signed in where: gh at github.com and glab at its default host.
func noteCLIs(ctx context.Context, ck checklist, r *repoCLIs) {
	for _, f := range r.discoverAll(ctx) {
		name := f.cli.name
		switch {
		case f.err == nil:
			ck.pass(name + " signed in to " + f.cli.host)
		case errors.Is(f.err, errCLIMissing):
			ck.note(name + " isn't installed")
		case errors.Is(f.err, errCLITimedOut):
			ck.note(name + " didn't answer in time")
		case f.cli.host == "":
			ck.note(name + " can't name its default host")
		default:
			ck.note(name + " isn't signed in to " + f.cli.host)
		}
	}
}
