package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/heyNag/eva-md/internal/config"
	"github.com/heyNag/eva-md/internal/health"
	"github.com/heyNag/eva-md/internal/install"
	"github.com/heyNag/eva-md/internal/paths"
	"github.com/heyNag/eva-md/internal/wikiinit"
)

// setupEnv is everything the guided setup reads or changes, so tests can
// replace it.
type setupEnv struct {
	dirs        paths.Dirs
	interactive bool
	home, cwd   string
	lookPath    func(string) (string, error)
	signing     func(context.Context) string
	initWiki    func(context.Context, wikiinit.Options) (wikiinit.Result, error)
	install     func(context.Context, install.Options) (install.Result, error)
	inspect     func(context.Context, paths.Dirs) health.Report
	executable  func() (string, error)
}

func runSetupCommand(ctx context.Context, args []string, in io.Reader, out, errOut io.Writer) int {
	d, err := paths.Default()
	if err != nil {
		fmt.Fprintln(errOut, "eva-md: home folder is unavailable")
		return 1
	}
	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintln(errOut, "eva-md: home folder is unavailable")
		return 1
	}
	cwd, _ := os.Getwd()
	return runSetup(ctx, args, in, out, errOut, setupEnv{
		dirs:        d,
		interactive: isTerminal(in),
		home:        home,
		cwd:         cwd,
		lookPath:    exec.LookPath,
		signing:     func(ctx context.Context) string { return gitSigning(ctx, home) },
		initWiki:    wikiinit.Run,
		install:     install.Run,
		inspect: func(ctx context.Context, d paths.Dirs) health.Report {
			return health.Inspect(ctx, d, okfModule(), true)
		},
		executable: os.Executable,
	})
}

// runSetup is the first-run experience: it checks this Mac, asks where the
// wiki lives and where it is pushed (there are no default answers), creates
// the wiki, installs the plugins and runs the health check. Run again on a
// Mac that is already set up, it keeps the wiki and installs or updates the
// plugins.
func runSetup(ctx context.Context, args []string, in io.Reader, out, errOut io.Writer, env setupEnv) int {
	f := flag.NewFlagSet("setup", flag.ContinueOnError)
	f.SetOutput(io.Discard)
	remote := f.String("remote", "", "")
	dir := f.String("dir", "", "")
	host := f.String("host", "all", "")
	noPush := f.Bool("no-push", false, "")
	yes := f.Bool("yes", false, "")
	if err := f.Parse(args); err != nil || f.NArg() != 0 {
		return usage(errOut, "invalid arguments for setup")
	}
	if *host != "all" && *host != "claude" && *host != "codex" {
		return usage(errOut, "setup --host must be claude, codex or all")
	}
	c, err := config.LoadContext(ctx, env.dirs.ConfigFile())
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		fmt.Fprintf(errOut, "eva-md: the eva-md config at %s can't be read; fix or move it aside and run setup again\n", env.dirs.ConfigFile())
		return 1
	}
	existing := c.WikiPath != ""
	if !existing && (*dir == "" || *remote == "") && !env.interactive {
		fmt.Fprintln(errOut, "eva-md: setup can't ask questions here (no terminal); pass --dir and --remote")
		return 2
	}
	answers := bufio.NewReader(in)
	ask := func(prompt string) (string, bool) {
		fmt.Fprint(out, prompt)
		line, err := answers.ReadString('\n')
		if err != nil && line == "" {
			fmt.Fprintln(out)
			return "", false
		}
		return strings.TrimSpace(line), true
	}

	fmt.Fprintln(out, "eva.md setup")
	fmt.Fprintln(out)
	fmt.Fprintln(out, "eva.md gives Claude Code and Codex a memory of your chats. It keeps a private")
	fmt.Fprintln(out, "wiki: a git repository of Markdown pages in a folder on this Mac, pushed to a")
	fmt.Fprintln(out, "private repository that you create.")
	fmt.Fprintln(out)

	fmt.Fprintln(out, "Checking this Mac")
	if _, err := env.lookPath("git"); err != nil {
		fmt.Fprintln(out, "  git            not found")
		fmt.Fprintln(errOut, "eva-md: setup needs git; install it (xcode-select --install) and run setup again")
		return 1
	}
	fmt.Fprintln(out, "  git            found")
	var hosts []string
	for _, h := range []struct{ name, cli string }{{"Claude Code", "claude"}, {"Codex", "codex"}} {
		state := "not found"
		if _, err := env.lookPath(h.cli); err == nil {
			state = "found"
			if *host == "all" || *host == h.cli {
				hosts = append(hosts, h.name)
			}
		}
		fmt.Fprintf(out, "  %-14s %s\n", h.name, state)
	}
	if len(hosts) == 0 {
		fmt.Fprintln(errOut, "eva-md: setup needs Claude Code or Codex; install one and run setup again")
		return 1
	}
	if signing := env.signing(ctx); signing != "" {
		fmt.Fprintf(out, "  %-14s on (%s)\n", "Commit signing", signing)
	} else {
		fmt.Fprintf(out, "  %-14s off: the wiki's commits will not be signed\n", "Commit signing")
	}

	folder, remoteURL := "", *remote
	if existing {
		fmt.Fprintln(out)
		fmt.Fprintf(out, "This Mac is already set up: the wiki is in %s, pushed to %s.\n", c.WikiPath, c.ExpectedRemote)
		if info, err := os.Stat(c.WikiPath); err != nil || !info.IsDir() {
			fmt.Fprintf(errOut, "eva-md: the config points to a wiki at %s, which doesn't exist; restore it, or move %s aside to start over\n", c.WikiPath, env.dirs.ConfigFile())
			return 1
		}
		if *dir != "" {
			if p, err := answerPath(*dir, env); err != nil || !samePath(p, c.WikiPath) {
				fmt.Fprintln(errOut, "eva-md: setup won't move an existing wiki; leave out --dir to keep it")
				return 1
			}
		}
		if *remote != "" {
			if id, err := wikiinit.CheckRemote(*remote); err != nil || id != c.ExpectedRemote {
				fmt.Fprintln(errOut, "eva-md: the wiki is pushed to a different remote; leave out --remote to keep it")
				return 1
			}
		}
		folder = c.WikiPath
	} else {
		if *dir != "" {
			if folder, err = checkFolderAnswer(*dir, env); err != nil {
				fmt.Fprintln(errOut, "eva-md: "+plain(err))
				return 1
			}
		} else {
			fmt.Fprintln(out)
			fmt.Fprintln(out, "1. Where should the wiki live on this Mac?")
			fmt.Fprintln(out, "   Choose a new or empty folder of its own, such as one next to your projects.")
			fmt.Fprintln(out, "   Not in Documents, Desktop or iCloud Drive: macOS blocks background updates there.")
			if folder = askUntil(ask, out, "   Folder: ", func(a string) (string, error) { return checkFolderAnswer(a, env) }); folder == "" {
				fmt.Fprintln(errOut, "eva-md: setup stopped; nothing was changed")
				return 1
			}
		}
		if *remote != "" {
			if _, err := wikiinit.CheckRemote(remoteURL); err != nil {
				fmt.Fprintln(errOut, "eva-md: "+plain(err))
				return 1
			}
		} else {
			fmt.Fprintln(out)
			fmt.Fprintln(out, "2. Which private git repository should it be pushed to?")
			fmt.Fprintln(out, "   Create an empty private repository first, on GitHub, GitLab or any git host,")
			fmt.Fprintln(out, "   then paste its SSH or HTTPS address, such as git@github.com:you/eva-wiki.git.")
			remoteURL = askUntil(ask, out, "   Remote: ", func(a string) (string, error) {
				a = unquote(a)
				if _, err := wikiinit.CheckRemote(a); err != nil {
					return "", fmt.Errorf("%s. Paste an address like git@github.com:you/eva-wiki.git or https://github.com/you/eva-wiki.git", strings.TrimSuffix(plain(err), "."))
				}
				return a, nil
			})
			if remoteURL == "" {
				fmt.Fprintln(errOut, "eva-md: setup stopped; nothing was changed")
				return 1
			}
		}
	}

	fmt.Fprintln(out)
	fmt.Fprintln(out, "Summary")
	fmt.Fprintf(out, "  Wiki folder   %s\n", folder)
	switch {
	case existing:
		fmt.Fprintf(out, "  Pushed to     %s\n", c.ExpectedRemote)
	case *noPush:
		fmt.Fprintf(out, "  Remote        %s (not pushed yet: --no-push)\n", remoteURL)
	default:
		fmt.Fprintf(out, "  Pushed to     %s\n", remoteURL)
	}
	fmt.Fprintf(out, "  Plugins for   %s\n", strings.Join(hosts, " and "))
	if env.interactive && !*yes {
		question := "Set up eva.md with these choices? [y/N] "
		if existing {
			question = "Install or update the plugins? [y/N] "
		}
		answer, ok := ask(question)
		if !ok || !strings.EqualFold(answer, "y") && !strings.EqualFold(answer, "yes") {
			fmt.Fprintln(out, "Nothing was changed.")
			return 1
		}
	}

	if !existing {
		fmt.Fprintln(out)
		fmt.Fprintln(out, "Creating the wiki")
		fmt.Fprintln(out, "  Your git setup may ask you to approve signing the first commit and pushing it,")
		fmt.Fprintln(out, "  for example in 1Password.")
		r, err := env.initWiki(ctx, wikiinit.Options{Remote: remoteURL, Dir: folder, NoPush: *noPush, Version: version, OKFModule: okfModule(), Dirs: env.dirs})
		if err != nil {
			fmt.Fprintln(errOut, "eva-md: "+plain(err))
			fmt.Fprintln(errOut, "eva-md: setup stopped before installing anything; fix this and run setup again")
			return 1
		}
		if r.Adopted {
			fmt.Fprintf(out, "  Using the existing eva wiki in %s\n", r.WikiPath)
		} else {
			fmt.Fprintf(out, "  Wiki ready in %s\n", r.WikiPath)
		}
		if *noPush {
			fmt.Fprintln(out, "  The first commit was not pushed (--no-push).")
		}
	}

	fmt.Fprintln(out)
	fmt.Fprintln(out, "Installing the plugins")
	binary, err := env.executable()
	if err != nil {
		fmt.Fprintln(errOut, "eva-md: executable path is unavailable")
		return 1
	}
	ictx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	ir, err := env.install(ictx, install.Options{Dirs: env.dirs, Binary: binary, Host: *host})
	cancel()
	for _, action := range ir.Actions {
		fmt.Fprintln(out, "  "+action)
	}
	if err != nil {
		fmt.Fprintln(errOut, "eva-md: couldn't install the plugins: "+plain(err))
		fmt.Fprintln(errOut, "eva-md: the wiki is set up; fix this and run setup again to finish")
		return 1
	}
	if ir.NeedsTrust {
		fmt.Fprintln(out, "  Codex asks you to approve eva-md's hooks: open Codex, type /hooks and approve")
		fmt.Fprintln(out, "  eva-md. Chats that are already open are not restarted; start new ones.")
	}

	fmt.Fprintln(out)
	fmt.Fprintln(out, "Checking health")
	hctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	report := env.inspect(hctx, env.dirs)
	cancel()
	counts := map[string]int{}
	for _, check := range report.Checks {
		counts[check.Status]++
	}
	fmt.Fprintf(out, "  %d passed, %d failed, %d unknown\n", counts["PASS"], counts["FAIL"], counts["UNKNOWN"])
	for _, check := range report.Checks {
		if check.Status == "FAIL" {
			fmt.Fprintf(out, "  FAIL %s: %s", check.Name, check.Detail)
			if check.Fix != "" {
				fmt.Fprintf(out, "; %s", check.Fix)
			}
			fmt.Fprintln(out)
		}
	}

	fmt.Fprintln(out)
	if report.Failed() {
		fmt.Fprintln(out, "Setup finished, but the checks above need attention. Run eva-md doctor after fixing them.")
	} else {
		fmt.Fprintln(out, "eva.md is ready.")
	}
	fmt.Fprintln(out, "Start a new chat in Claude Code or Codex, in any project: eva.md captures it from the")
	fmt.Fprintln(out, "first message. In a later chat, ask \"where did we stop?\".")
	fmt.Fprintln(out, "To pause capture for a chat, start a message with \"off the record\".")
	return 0
}

// askUntil asks up to three times for an answer that check accepts, and
// returns the accepted value, or "" when the user gives up or input ends.
func askUntil(ask func(string) (string, bool), out io.Writer, prompt string, check func(string) (string, error)) string {
	for attempt := 0; attempt < 3; attempt++ {
		answer, ok := ask(prompt)
		if !ok {
			return ""
		}
		if answer == "" {
			fmt.Fprintln(out, "   Please type an answer, or press Ctrl-C to stop.")
			continue
		}
		value, err := check(answer)
		if err == nil {
			return value
		}
		fmt.Fprintln(out, "   "+plain(err))
	}
	return ""
}

// checkFolderAnswer turns a typed folder into an absolute path and checks
// that the wiki can live there.
func checkFolderAnswer(answer string, env setupEnv) (string, error) {
	p, err := answerPath(answer, env)
	if err != nil {
		return "", err
	}
	return wikiinit.CheckFolder(p, env.dirs)
}

// answerPath accepts ~/..., absolute paths and paths relative to the current
// folder, with or without surrounding quotes.
func answerPath(answer string, env setupEnv) (string, error) {
	p := unquote(answer)
	switch {
	case p == "~":
		p = env.home
	case strings.HasPrefix(p, "~/"):
		p = filepath.Join(env.home, p[2:])
	case strings.HasPrefix(p, "~"):
		return "", errors.New("use ~/ for your home folder, or a full path")
	case !filepath.IsAbs(p):
		if env.cwd == "" {
			return "", errors.New("type a full path, such as ~/projects/wiki")
		}
		p = filepath.Join(env.cwd, p)
	}
	return filepath.Clean(p), nil
}

func unquote(s string) string {
	s = strings.TrimSpace(s)
	if len(s) >= 2 && (s[0] == '"' && s[len(s)-1] == '"' || s[0] == '\'' && s[len(s)-1] == '\'') {
		s = s[1 : len(s)-1]
	}
	return s
}

func samePath(a, b string) bool {
	if ra, err := filepath.EvalSymlinks(a); err == nil {
		a = ra
	}
	if rb, err := filepath.EvalSymlinks(b); err == nil {
		b = rb
	}
	return strings.EqualFold(filepath.Clean(a), filepath.Clean(b))
}

// plain drops the package prefix from an error for people reading setup.
func plain(err error) string {
	msg := err.Error()
	if i := strings.Index(msg, ": "); i > 0 && !strings.Contains(msg[:i], " ") {
		msg = msg[i+2:]
	}
	if msg != "" {
		msg = strings.ToUpper(msg[:1]) + msg[1:]
	}
	return msg
}

// isTerminal reports whether r is an interactive terminal. /dev/null is a
// character device too, so it is ruled out by name.
func isTerminal(r io.Reader) bool {
	f, ok := r.(*os.File)
	if !ok {
		return false
	}
	info, err := f.Stat()
	if err != nil || info.Mode()&os.ModeCharDevice == 0 {
		return false
	}
	null, err := os.Stat(os.DevNull)
	return err != nil || !os.SameFile(info, null)
}

// gitSigning reports the commit signing format git uses from the home
// folder, or "" when signing is off. It reads only git's settings, never key
// material.
func gitSigning(ctx context.Context, home string) string {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	get := func(key string) string {
		cmd := exec.CommandContext(ctx, "git", "config", "--get", key)
		cmd.Dir = home
		b, err := cmd.Output()
		if err != nil {
			return ""
		}
		return strings.TrimSpace(string(b))
	}
	if !strings.EqualFold(get("commit.gpgsign"), "true") {
		return ""
	}
	if format := get("gpg.format"); format != "" {
		return format
	}
	return "openpgp"
}
