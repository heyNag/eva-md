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
	"github.com/heyNag/eva-md/internal/wikigit"
	"github.com/heyNag/eva-md/internal/wikiinit"
)

// setupEnv is everything the guided setup reads or changes, so tests can
// replace it.
type setupEnv struct {
	dirs         paths.Dirs
	interactive  bool
	home, cwd    string
	lookPath     func(string) (string, error)
	signing      func(context.Context) string
	initWiki     func(context.Context, wikiinit.Options) (wikiinit.Result, error)
	install      func(context.Context, install.Options) (install.Result, error)
	inspect      func(context.Context, paths.Dirs) health.Report
	executable   func() (string, error)
	peek         func(context.Context, string) (string, error) // what a remote holds, as peekRemote reads it
	machineLabel func() (string, error)
}

// plan is what setup does with the wiki once the summary is confirmed.
type plan int

const (
	planCreate      plan = iota // make a new wiki in an empty repo
	planInit                    // make one, or use the repo's, as init finds it: setup didn't look (no terminal)
	planAdoptRemote             // clone the eva wiki the repo holds
	planAdoptFolder             // use the eva wiki already in the folder
	planReclone                 // clone the configured wiki again into its missing folder
	planPlugins                 // keep the wiki as it is
)

// will says in plain words what setup will do, for the summary.
func (p plan) will(noPush bool) string {
	switch p {
	case planCreate:
		if noPush {
			return "Setup will create a new wiki in this folder without pushing it, then install the plugins."
		}
		return "Setup will create a new wiki in this folder and push it, then install the plugins."
	case planInit:
		return "Setup will create the wiki in this folder, or use the one the repo already holds, then install the plugins."
	case planAdoptRemote:
		return "Setup will clone your existing wiki into this folder, then install the plugins."
	case planAdoptFolder:
		return "Setup will use the wiki already in this folder, then install the plugins."
	case planReclone:
		return "Setup will clone the wiki again into its folder, then install or update the plugins."
	}
	return "Setup will install or update the plugins."
}

var (
	// errNoAnswer stops a question when the answers ran out.
	errNoAnswer = errors.New("setup stopped")
	// errDeclined is a no to using a wiki another Mac set up.
	errDeclined = errors.New("choose another repo for this Mac")
	// errOtherFolder is a no to using the eva wiki already in a folder.
	errOtherFolder = errors.New("choose a new or empty folder instead")
)

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
		executable:   os.Executable,
		peek:         peekRemote,
		machineLabel: wikiinit.MachineLabel,
	})
}

// runSetup is the first-run experience: it checks this Mac, asks where the
// wiki lives and where it is pushed (neither has a default answer), creates
// the wiki or uses the existing one, installs the plugins and runs the health
// check. It looks before it asks: at the folder and the repo the user names,
// and on a Mac that is already set up, at the wiki the config names, which it
// keeps, or clones again when its folder is missing or empty.
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
	repos := newRepoCLIs()
	s := &setupRun{ctx: ctx, env: env, ask: ask, out: out, repos: repos}

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
		state := "not found (no " + h.cli + " command)"
		if _, err := env.lookPath(h.cli); err == nil {
			state = "found"
			if *host == "all" || *host == h.cli {
				hosts = append(hosts, h.name)
			}
		}
		fmt.Fprintf(out, "  %-14s %s\n", h.name, state)
	}
	switch {
	case len(hosts) == 0 && *host != "all":
		fmt.Fprintf(errOut, "eva-md: the %s command isn't on PATH; install it, or leave out --host\n", *host)
		return 1
	case len(hosts) == 0:
		fmt.Fprintln(errOut, "eva-md: setup needs the claude or codex command; install Claude Code or Codex and run setup again")
		return 1
	}
	if signing := env.signing(ctx); signing != "" {
		fmt.Fprintf(out, "  %-14s on (%s)\n", "Commit signing", signing)
	} else {
		fmt.Fprintf(out, "  %-14s off: the wiki's commits will not be signed\n", "Commit signing")
	}

	folder, remoteURL := "", *remote
	createdRepo := false // setup made the remote's repo with gh or glab, or may have
	newRepo := false     // the remote is the repo setup made
	var p plan
	if existing {
		folder = c.WikiPath
		if p, remoteURL, err = s.configuredPlan(c, *dir, *remote); err != nil {
			fmt.Fprintln(errOut, "eva-md: "+err.Error())
			return 1
		}
	} else {
		if s.label = c.MachineLabel; s.label == "" {
			s.label, _ = env.machineLabel()
		}
		if *dir != "" {
			if folder, err = s.checkFolder(*dir, *remote); err != nil {
				switch {
				case errors.Is(err, errNoAnswer):
					fmt.Fprintln(errOut, "eva-md: setup stopped; nothing was changed")
				case errors.Is(err, errOtherFolder):
					fmt.Fprintf(errOut, "eva-md: you chose not to use the eva wiki in %s; choose a new or empty folder instead\n", *dir)
				default:
					fmt.Fprintln(errOut, "eva-md: "+plain(err))
				}
				return 1
			}
		} else {
			fmt.Fprintln(out)
			fmt.Fprintln(out, "1. Where should the wiki live on this Mac?")
			fmt.Fprintln(out, "   Choose a new or empty folder of its own, such as one next to your projects.")
			fmt.Fprintln(out, "   Not in Documents, Desktop or iCloud Drive: macOS blocks background updates there.")
			if folder = askUntil(ask, out, "   Folder: ", func(a string) (string, error) { return s.checkFolder(a, *remote) }); folder == "" {
				fmt.Fprintln(errOut, "eva-md: setup stopped; nothing was changed")
				return 1
			}
		}
		switch {
		case s.folderOrigin != "":
			// The folder's own wiki, whose repo checkFolder has checked.
			remoteURL, p = s.folderOrigin, planAdoptFolder
		case *remote != "":
			if _, err := wikiinit.CheckRemote(remoteURL); err != nil {
				fmt.Fprintln(errOut, "eva-md: "+plain(err))
				return 1
			}
			err := repos.checkPrivate(ctx, remoteURL, out)
			if errors.Is(err, errReadable) && env.interactive {
				err = confirmReadable(ask, out)
			}
			if errors.Is(err, errNoAnswer) {
				fmt.Fprintln(errOut, "eva-md: setup stopped; nothing was changed")
				return 1
			}
			if err != nil {
				fmt.Fprintln(errOut, "eva-md: "+plain(err))
				return 1
			}
			p = planInit
			if env.interactive {
				if err := s.checkRemote(remoteURL); err != nil {
					if errors.Is(err, errNoAnswer) {
						fmt.Fprintln(errOut, "eva-md: setup stopped; nothing was changed")
					} else {
						fmt.Fprintln(errOut, "eva-md: "+plain(err))
					}
					return 1
				}
				p = s.remotePlan()
			}
		default:
			fmt.Fprintln(out)
			fmt.Fprintln(out, "2. Which private git repository should it be pushed to?")
			var ok bool
			if remoteURL, createdRepo, ok = repos.offer(ctx, ask, out, s.checkRemote); !ok {
				fmt.Fprintln(errOut, "eva-md: setup stopped; nothing was changed")
				return 1
			}
			newRepo = createdRepo && remoteURL != ""
			if remoteURL == "" {
				if createdRepo {
					// offer has said what it created and why it isn't used.
					fmt.Fprintln(out, "   Paste the SSH or HTTPS address of an empty private repository, such as")
					fmt.Fprintln(out, "   git@github.com:you/eva-wiki.git.")
				} else {
					fmt.Fprintln(out, "   Create an empty private repository first, on GitHub, GitLab or any git host,")
					fmt.Fprintln(out, "   then paste its SSH or HTTPS address, such as git@github.com:you/eva-wiki.git.")
				}
				remoteURL = askUntil(ask, out, "   Remote: ", func(a string) (string, error) {
					remote, err := repos.checkAnswer(ctx, a, out)
					if errors.Is(err, errReadable) {
						err = confirmReadable(ask, out)
					}
					if err == nil {
						err = s.checkRemote(remote)
					}
					return remote, err
				})
			}
			if remoteURL == "" {
				if createdRepo {
					fmt.Fprintln(errOut, "eva-md: setup stopped; nothing else was changed (the new repo stays, empty)")
				} else {
					fmt.Fprintln(errOut, "eva-md: setup stopped; nothing was changed")
				}
				return 1
			}
			p = s.remotePlan()
		}
	}

	// A Mac that is set up has said so in a line, and the question names
	// the one thing setup does.
	if p != planPlugins {
		fmt.Fprintln(out)
		fmt.Fprintln(out, "Summary")
		fmt.Fprintf(out, "  Wiki folder   %s\n", folder)
		switch {
		case *noPush && (p == planCreate || p == planInit):
			fmt.Fprintf(out, "  Remote        %s (not pushed yet: --no-push)\n", remoteURL)
		case newRepo:
			fmt.Fprintf(out, "  Pushed to     %s (created just now)\n", remoteURL)
		default:
			fmt.Fprintf(out, "  Pushed to     %s\n", remoteURL)
		}
		fmt.Fprintf(out, "  Plugins for   %s\n", strings.Join(hosts, " and "))
		fmt.Fprintln(out, p.will(*noPush))
	}
	// The yes to cloning the wiki again was the confirmation.
	if env.interactive && !*yes && p != planReclone {
		question := "Set up eva.md with these choices? [y/N] "
		if p == planPlugins {
			question = "Install or update the plugins? [y/N] "
		}
		answer, ok := ask(question)
		if !ok || !strings.EqualFold(answer, "y") && !strings.EqualFold(answer, "yes") {
			if createdRepo {
				fmt.Fprintln(out, "Nothing else was changed. The new repo stays, empty, for the next setup.")
			} else {
				fmt.Fprintln(out, "Nothing was changed.")
			}
			return 1
		}
	}

	if p != planPlugins {
		fmt.Fprintln(out)
		switch p {
		case planAdoptRemote, planReclone:
			fmt.Fprintln(out, "Cloning the wiki")
		case planAdoptFolder:
			fmt.Fprintln(out, "Using the wiki in this folder")
		default:
			fmt.Fprintln(out, "Creating the wiki")
		}
		if p == planCreate || p == planInit {
			fmt.Fprintln(out, "  Your git setup may ask you to approve signing the first commit and pushing it,")
			fmt.Fprintln(out, "  for example in your password manager or SSH agent.")
		} else {
			fmt.Fprintln(out, "  Your git setup may ask you to approve access to the repo, for example in your password manager.")
		}
		// The wiki cloned again has nothing to push, and a push would ask for
		// another approval.
		r, err := env.initWiki(ctx, wikiinit.Options{Remote: remoteURL, Dir: folder, NoPush: *noPush || p == planReclone, Version: version, OKFModule: okfModule(), Dirs: env.dirs})
		if err != nil {
			fmt.Fprintln(errOut, "eva-md: "+plain(err))
			switch {
			case p == planReclone:
				fmt.Fprintf(errOut, "eva-md: check that %s is right, or run eva-md setup --remote with the right address\n", remoteURL)
			case createdRepo:
				fmt.Fprintln(errOut, "eva-md: the new repo stays for the next setup")
			}
			fmt.Fprintln(errOut, "eva-md: setup stopped before installing anything; fix this and run setup again")
			return 1
		}
		if r.Adopted {
			fmt.Fprintf(out, "  Using the existing eva wiki in %s\n", r.WikiPath)
		} else {
			fmt.Fprintf(out, "  Wiki ready in %s\n", r.WikiPath)
		}
		if *noPush && !r.Adopted {
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
	setAside(env.dirs, errOut)
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

// setupRun is what the questions about a new wiki share, and what they found:
// the eva wiki already in the folder, or on the repo.
type setupRun struct {
	ctx   context.Context
	env   setupEnv
	ask   func(string) (string, bool)
	out   io.Writer
	repos *repoCLIs
	label string // this Mac's label, which the instance of a wiki set up here starts with

	folderOrigin string // where the eva wiki in the chosen folder is pushed, when setup uses it
	adopt        bool   // the chosen repo holds an eva wiki
}

// checkFolder checks a folder answer as checkFolderAnswer does. With a
// terminal, when the folder is already an eva wiki pushed to a private repo,
// it asks whether to use it, and on yes remembers where it is pushed, so that
// setup skips the remote question. remote, when given, must name that repo.
// A clone of another repository is refused before setup makes a repo that
// init would then refuse to clone into it.
func (s *setupRun) checkFolder(answer, remote string) (string, error) {
	folder, err := checkFolderAnswer(answer, s.env)
	if err != nil || !s.env.interactive {
		return folder, err
	}
	origin, instance, ok := folderWiki(s.ctx, folder)
	if !ok {
		if why := otherRepo(s.ctx, folder); why != "" {
			return "", errors.New(why)
		}
		return folder, nil
	}
	if dirty(s.ctx, folder) {
		return "", fmt.Errorf("this eva wiki has uncommitted changes; commit them in %s (git add -A && git commit) and run setup again", folder)
	}
	found := "   This folder is already an eva wiki, pushed to " + origin + "."
	if remote != "" && !sameRepo(origin, remote) {
		fmt.Fprintln(s.out, found)
		return "", errors.New("--remote names another repo; leave it out to use this wiki, or choose another folder")
	}
	if err := s.repos.checkPrivate(s.ctx, origin, s.out); err != nil {
		fmt.Fprintln(s.out, found)
		if !errors.Is(err, errReadable) {
			return "", err
		}
		if err := confirmReadable(s.ask, s.out); err != nil {
			return "", err
		}
	}
	var use bool
	if s.madeHere(instance) {
		use, ok = yesNo(s.ask, s.out, found+" Use it? [Y/n] ", true)
	} else {
		fmt.Fprintln(s.out, found)
		use, ok = s.otherMac(instance)
	}
	switch {
	case !ok:
		return "", errNoAnswer
	case !use:
		return "", errOtherFolder
	}
	s.folderOrigin = origin
	return folder, nil
}

// checkRemote reads what a chosen remote holds before setup uses it: an
// empty repo, or an eva wiki, which setup uses as it is, asking first when
// another Mac set it up. Its error says what to do instead.
func (s *setupRun) checkRemote(remote string) error {
	fmt.Fprintln(s.out, "   Checking the repo with git; your git setup may ask you to approve.")
	instance, err := s.env.peek(s.ctx, remote)
	switch {
	case err != nil:
		return err
	case instance == "":
		fmt.Fprintln(s.out, "   The repo is empty: setup will create the wiki in it.")
	case s.madeHere(instance):
		fmt.Fprintln(s.out, "   The repo holds an eva wiki set up on this Mac: setup will use it.")
	default:
		use, ok := s.otherMac(instance)
		if !ok {
			return errNoAnswer
		}
		if !use {
			return errDeclined
		}
	}
	s.adopt = instance != ""
	return nil
}

// remotePlan is what setup does with the chosen repo, as checkRemote found
// it; a repo setup didn't read is empty.
func (s *setupRun) remotePlan() plan {
	if s.adopt {
		return planAdoptRemote
	}
	return planCreate
}

// madeHere reports whether the wiki with this instance was set up on this
// Mac, as its label says. Labels are compared without a domain: unless a
// HostName is set, macOS's hostname follows the network, such as my-mac.local
// on one and my-mac on another.
func (s *setupRun) madeHere(instance string) bool {
	host := func(label string) string {
		name, _, _ := strings.Cut(label, ".")
		return name
	}
	return s.label != "" && strings.EqualFold(host(wikiinit.MadeOn(instance)), host(s.label))
}

// otherMac asks before using a wiki another Mac set up. No is the default:
// two Macs pushing to one wiki would mix their memories. ok is false when the
// answers ran out.
func (s *setupRun) otherMac(instance string) (use, ok bool) {
	return yesNo(s.ask, s.out, fmt.Sprintf("   This wiki was set up on %[1]s. One wiki per machine: use it here only if %[1]s no longer pushes to it. Use it? [y/N] ", wikiinit.MadeOn(instance)), false)
}

// configuredPlan is what setup does on a Mac that is already set up: keep the
// wiki the config names, or with a terminal, clone it again into its folder
// when that is missing or empty. remote, when given, must name the configured
// repo, and is then the address to clone from. The error says why setup
// stops, and what to do.
func (s *setupRun) configuredPlan(c config.Config, dir, remote string) (plan, string, error) {
	if dir != "" {
		if path, err := answerPath(dir, s.env); err != nil || !samePath(path, c.WikiPath) {
			return 0, "", errors.New("setup won't move an existing wiki; leave out --dir to keep it")
		}
	}
	if remote != "" {
		if id, err := wikiinit.CheckRemote(remote); err != nil || id != c.ExpectedRemote {
			return 0, "", errors.New("the wiki is pushed to a different remote; leave out --remote to keep it")
		}
	}
	problem, absent := configuredProblem(s.ctx, c)
	switch {
	case problem != "":
		return 0, "", fmt.Errorf("%s; restore your wiki there, or move %s aside to start over", problem, s.env.dirs.ConfigFile())
	case absent == "":
		fmt.Fprintln(s.out)
		fmt.Fprintf(s.out, "eva-md is set up here: the wiki is in %s, pushed to %s.\n", c.WikiPath, c.ExpectedRemote)
		return planPlugins, remote, nil
	}
	// Only with a terminal: without one, nothing is cloned again.
	if s.env.interactive {
		fmt.Fprintln(s.out)
		if remote == "" {
			if cliInstalled() && !strings.HasPrefix(c.ExpectedRemote, "file://") {
				fmt.Fprintln(s.out, "Asking gh or glab for the repo's address.")
			}
			remote = s.repos.cloneAddress(s.ctx, c.ExpectedRemote)
		}
		prompt, def := "Your wiki folder %[1]s is %[3]s. Clone it again from %[2]s into %[1]s? [Y/n] ", true
		if _, err := os.Stat(filepath.Dir(c.WikiPath)); err != nil {
			// As on a volume that isn't mounted: the clone would make the
			// parent folder on this Mac's own disk.
			prompt, def = "Your wiki folder %[1]s is %[3]s, and its parent folder is missing too. Clone it again from %[2]s into %[1]s? [y/N] ", false
		}
		// The repo is read before a question whose default is yes, so the
		// question comes only when cloning again would work, and after a
		// yes to one whose default is no, which most answer with no.
		if def {
			if err := s.sameWiki(c, remote); err != nil {
				return 0, "", err
			}
			fmt.Fprintln(s.out)
		}
		if again, _ := yesNo(s.ask, s.out, fmt.Sprintf(prompt, c.WikiPath, remote, absent), def); again {
			if !def {
				if err := s.sameWiki(c, remote); err != nil {
					return 0, "", err
				}
			}
			return planReclone, remote, nil
		}
	}
	which := "doesn't exist"
	if absent == "empty" {
		which = "is empty"
	}
	return 0, "", fmt.Errorf("the config points to a wiki at %s, which %s; restore it, or move %s aside to start over", c.WikiPath, which, s.env.dirs.ConfigFile())
}

// sameWiki reads the repo a missing wiki folder would be cloned again from,
// and refuses an empty repo or another wiki, either of which would become a
// new wiki in place of the one the config names.
func (s *setupRun) sameWiki(c config.Config, remote string) error {
	fmt.Fprintln(s.out, "Checking the repo with git; your git setup may ask you to approve.")
	switch instance, err := s.env.peek(s.ctx, remote); {
	case err != nil:
		return fmt.Errorf("%s: %v; or run eva-md setup --remote with the right address", remote, err)
	case instance == "":
		return fmt.Errorf("the repo at %s is empty, so your wiki can't be cloned again from it; push your wiki there, or move %s aside to start over", remote, s.env.dirs.ConfigFile())
	case instance != c.Instance:
		return fmt.Errorf("the repo at %s holds another eva wiki than the one the config names; push yours there, or move %s aside to start over", remote, s.env.dirs.ConfigFile())
	}
	return nil
}

// configuredProblem says what is wrong with the wiki folder the config names,
// or "" when it is a clone of the configured remote that holds the config's
// instance. absent is "missing" or "empty" when the folder doesn't exist or
// holds nothing, so that setup can clone the wiki again into it.
func configuredProblem(ctx context.Context, c config.Config) (problem, absent string) {
	info, err := os.Stat(c.WikiPath)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return "", "missing"
	case err != nil:
		return fmt.Sprintf("the wiki folder %s can't be read", c.WikiPath), ""
	case !info.IsDir():
		return fmt.Sprintf("the config points to a wiki at %s, which isn't a folder", c.WikiPath), ""
	}
	if entries, err := os.ReadDir(c.WikiPath); err == nil && wikiinit.Empty(entries) {
		return "", "empty"
	}
	if git, err := os.Lstat(filepath.Join(c.WikiPath, ".git")); err != nil || !git.IsDir() {
		return fmt.Sprintf("the wiki folder %s isn't a git clone", c.WikiPath), ""
	}
	if id := originID(ctx, c.WikiPath); id == "" || id != c.ExpectedRemote {
		return fmt.Sprintf("the wiki folder %s isn't a clone of %s", c.WikiPath, c.ExpectedRemote), ""
	}
	schema, err := config.LoadSchema(filepath.Join(c.WikiPath, ".eva-md", "schema.toml"))
	if err != nil {
		return fmt.Sprintf("the wiki folder %s has no readable .eva-md/schema.toml", c.WikiPath), ""
	}
	if schema.Instance != c.Instance {
		return fmt.Sprintf("the wiki in %s is another eva wiki than the one the config names", c.WikiPath), ""
	}
	return "", ""
}

// originID is the identity of a clone's origin, as the config records a
// remote, or "" without one setup can use. A local remote that isn't there
// now, such as one on a volume that isn't mounted, is named by its path.
func originID(ctx context.Context, dir string) string {
	origin, err := wikigit.Run(ctx, 10*time.Second, dir, "config", "--get", "remote.origin.url")
	if err != nil {
		return ""
	}
	if id, err := wikiinit.CheckRemote(origin); err == nil {
		return id
	}
	if filepath.IsAbs(origin) {
		return "file://" + filepath.ToSlash(filepath.Clean(origin))
	}
	return ""
}

// folderWiki reads the eva wiki already in folder: where it is pushed and
// its instance. ok is false when the folder isn't a clone holding one, with
// an origin setup can use.
func folderWiki(ctx context.Context, folder string) (origin, instance string, ok bool) {
	for _, name := range []string{".git", ".eva-md"} {
		if info, err := os.Lstat(filepath.Join(folder, name)); err != nil || !info.IsDir() {
			return "", "", false
		}
	}
	schema, err := config.LoadSchema(filepath.Join(folder, ".eva-md", "schema.toml"))
	if err != nil || schema.Instance == "" {
		return "", "", false
	}
	origin, err = wikigit.Run(ctx, 10*time.Second, folder, "config", "--get", "remote.origin.url")
	if err != nil {
		return "", "", false
	}
	if _, err := wikiinit.CheckRemote(origin); err != nil {
		return "", "", false
	}
	return origin, schema.Instance, true
}

// otherRepo says why folder, a repository of its own that folderWiki can't
// use, can't hold the wiki: a repository with commits but no eva wiki, a
// clone or not; one with no commits and no remote, which init could neither
// clone into nor push from; or an eva wiki whose remote is missing or one
// setup can't use, or whose schema is incomplete. It returns "" for anything
// else, such as a clone of an empty repo, or a folder without a .git of its
// own, though it may be inside a repository.
func otherRepo(ctx context.Context, folder string) string {
	if info, err := os.Lstat(filepath.Join(folder, ".git")); err != nil || !info.IsDir() {
		return ""
	}
	if _, err := wikigit.Run(ctx, 10*time.Second, folder, "rev-parse", "--verify", "HEAD"); err != nil {
		if _, err := wikigit.Run(ctx, 10*time.Second, folder, "config", "--get", "remote.origin.url"); err != nil {
			return "that folder is already a git repository, with no remote; choose a new or empty folder"
		}
		return ""
	}
	schema, err := config.LoadSchema(filepath.Join(folder, ".eva-md", "schema.toml"))
	origin, originErr := wikigit.Run(ctx, 10*time.Second, folder, "config", "--get", "remote.origin.url")
	switch {
	case err != nil && originErr != nil:
		return "that folder is already a git repository with commits; choose a new or empty folder"
	case err != nil:
		return "that folder is a clone of another repository; choose a new or empty folder"
	case originErr != nil:
		return "that folder holds an eva wiki with no remote; add its remote back (git remote add origin <address>) or choose a new or empty folder"
	}
	if _, err := wikiinit.CheckRemote(origin); err != nil {
		// A local path that isn't there now, as on a volume that isn't
		// mounted, is right as it is; a file:// URL is one setup can't use.
		if filepath.IsAbs(origin) {
			return "that folder holds an eva wiki whose local remote " + origin + " isn't there now; mount or restore it and run setup again, or choose a new or empty folder"
		}
		return "that folder holds an eva wiki whose remote setup can't use; point it at the wiki's repo (git remote set-url origin <address>) or choose a new or empty folder"
	}
	if schema.Instance == "" {
		return "that folder holds an incomplete eva wiki; choose a new or empty folder"
	}
	return ""
}

// dirty reports whether the clone in folder has changes git hasn't
// committed, which init refuses to adopt.
func dirty(ctx context.Context, folder string) bool {
	status, err := wikigit.Run(ctx, 10*time.Second, folder, "status", "--porcelain", "--untracked-files=all")
	return err == nil && status != ""
}

// peekRemote reads what remote holds, from a shallow clone in a temporary
// folder that it removes: "" for an empty repo, or the instance of the eva
// wiki on it. A repo with other content, or with a wiki whose schema init
// refuses, is refused, so setup never clones one into the wiki folder.
func peekRemote(ctx context.Context, remote string) (string, error) {
	tmp, err := os.MkdirTemp("", "eva-md-setup-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(tmp)
	dir := filepath.Join(tmp, "repo")
	if _, err := wikigit.Run(ctx, 2*time.Minute, tmp, "clone", "--no-local", "--depth", "1", "--no-checkout", "--", remote, dir); err != nil {
		return "", errors.New("git couldn't read this repo; check its address and that git can sign in to it")
	}
	if _, err := wikigit.Run(ctx, 10*time.Second, dir, "rev-parse", "--verify", "HEAD"); err != nil {
		return "", nil
	}
	// Only the schema is checked out. A symlinked .eva-md is refused, as
	// init refuses it.
	if _, err := wikigit.Run(ctx, 10*time.Second, dir, "checkout", "HEAD", "--", ".eva-md/schema.toml"); err == nil {
		if info, err := os.Lstat(filepath.Join(dir, ".eva-md")); err == nil && info.IsDir() {
			if schema, err := config.LoadSchema(filepath.Join(dir, ".eva-md", "schema.toml")); err == nil && schema.Instance != "" {
				if schema.SchemaVersion != 1 || schema.OKFSpec != "0.2" {
					return "", errors.New("this repo holds an eva wiki this eva-md can't use; update eva-md")
				}
				return schema.Instance, nil
			}
		}
	}
	return "", errors.New("this repo has content but isn't an eva wiki; use an empty private repo, or the repo of your eva wiki")
}

// sameRepo reports whether two remotes name the same repo.
func sameRepo(a, b string) bool {
	idA, errA := wikiinit.CheckRemote(a)
	idB, errB := wikiinit.CheckRemote(b)
	return errA == nil && errB == nil && idA == idB
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
		if errors.Is(err, errNoAnswer) {
			return ""
		}
		fmt.Fprintln(out, "   "+plain(err))
	}
	return ""
}

// yesNo asks a yes-or-no question, up to three times for an answer it
// understands; Enter gives def, and three other answers give no. ok is false
// when the answers ran out.
func yesNo(ask func(string) (string, bool), out io.Writer, prompt string, def bool) (yes, ok bool) {
	for range 3 {
		answer, ok := ask(prompt)
		if !ok {
			return false, false
		}
		switch strings.ToLower(answer) {
		case "":
			return def, true
		case "y", "yes":
			return true, true
		case "n", "no":
			return false, true
		}
		fmt.Fprintln(out, "   Please type y or n.")
	}
	return false, true
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
