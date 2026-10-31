package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/heyNag/eva-md/internal/config"
	"github.com/heyNag/eva-md/internal/health"
	"github.com/heyNag/eva-md/internal/identity"
	"github.com/heyNag/eva-md/internal/install"
	"github.com/heyNag/eva-md/internal/paths"
	"github.com/heyNag/eva-md/internal/redact"
	"github.com/heyNag/eva-md/internal/resume"
	"github.com/heyNag/eva-md/internal/update"
)

func runResume(ctx context.Context, args []string, out, errOut io.Writer) int {
	d, err := paths.Default()
	if err != nil {
		fmt.Fprintln(errOut, "eva-md: home folder is unavailable")
		return 1
	}
	c, err := config.Load(d.ConfigFile())
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		fmt.Fprintln(errOut, "eva-md: configuration is invalid")
		return 1
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if args[0] == "show" {
		return runShow(ctx, d, c, args[1:], out, errOut)
	}
	if args[0] == "search" {
		return runSearch(ctx, d, c, args[1:], out, errOut)
	}
	f := flag.NewFlagSet("brief", flag.ContinueOnError)
	f.SetOutput(io.Discard)
	cwd := f.String("cwd", "", "")
	variant := f.String("as", "startup", "")
	host := f.String("host", "codex", "")
	sid := f.String("session", "", "")
	if err := f.Parse(args[1:]); err != nil || f.NArg() != 0 {
		return usage(errOut, "invalid arguments for brief")
	}
	if *host != "codex" && *host != "claude-code" {
		return usage(errOut, "brief --host must be codex or claude-code")
	}
	switch *variant {
	case "startup", "clear":
	case "compact", "resume", "fork":
		if *sid == "" {
			return usage(errOut, "brief --as compact|resume|fork requires --session")
		}
	default:
		return usage(errOut, "unknown brief variant")
	}
	if *cwd == "" {
		*cwd, err = os.Getwd()
		if err != nil {
			fmt.Fprintln(errOut, "eva-md: current directory is unavailable")
			return 1
		}
	}
	p, err := identity.Resolve(ctx, *cwd)
	if err != nil {
		fmt.Fprintln(errOut, "eva-md: project identity is unavailable")
		return 1
	}
	if c.Excluded(*cwd, p.Remote) {
		return 0
	}
	if p.Branch != "" {
		// The thread key the hooks store with each entry (see redact.Identifier).
		p.Branch, _ = redact.Identifier(p.Branch, 1024)
	}
	r, err := resume.Build(ctx, resume.Options{Dirs: d, Config: c, Project: p, Host: *host, Session: *sid, Variant: *variant, Now: time.Now()})
	if err != nil {
		fmt.Fprintln(errOut, "eva-md:", err)
		return 1
	}
	if r.Text != "" {
		fmt.Fprintln(out, r.Text)
	}
	return 0
}

// runShow prints a source ref's entry or a page or part of it. Flags may
// come before or after the ref.
func runShow(ctx context.Context, d paths.Dirs, c config.Config, args []string, out, errOut io.Writer) int {
	f := flag.NewFlagSet("show", flag.ContinueOnError)
	f.SetOutput(io.Discard)
	last := f.Int("last", 0, "")
	since := f.String("since", "", "")
	refs, err := interleaved(f, args)
	if err != nil {
		return usage(errOut, "invalid arguments for show")
	}
	if len(refs) != 1 {
		return usage(errOut, "show requires a source ref or page[#section]")
	}
	// ShowOptions reads a Last of 0 as not set, so a 0 typed is refused here.
	lastSet := false
	f.Visit(func(fl *flag.Flag) { lastSet = lastSet || fl.Name == "last" })
	if lastSet && *last < 1 {
		return usage(errOut, "show --last must be 1 or more")
	}
	text, err := resume.ShowWith(ctx, d, c, refs[0], resume.ShowOptions{Last: *last, Since: *since})
	if err != nil {
		fmt.Fprintln(errOut, "eva-md:", err)
		return 1
	}
	fmt.Fprintln(out, text)
	return 0
}

// interleaved parses f's flags wherever they stand among args and returns
// the other arguments in order. "--" ends the flags, so an argument after it
// may start with a dash.
func interleaved(f *flag.FlagSet, args []string) ([]string, error) {
	var rest []string
	for len(args) > 0 {
		if err := f.Parse(args); err != nil {
			return nil, err
		}
		left := f.Args()
		if used := len(args) - len(left); used > 0 && args[used-1] == "--" {
			return append(rest, left...), nil
		}
		if len(left) == 0 {
			break
		}
		rest = append(rest, left[0])
		args = left[1:]
	}
	return rest, nil
}

// runSearch finds earlier chat text in the inbox and lines of the wiki's
// thread pages. Flags may come before or after the terms, and "--" ends
// them, so a term may start with a dash.
func runSearch(ctx context.Context, d paths.Dirs, c config.Config, args []string, out, errOut io.Writer) int {
	f := flag.NewFlagSet("search", flag.ContinueOnError)
	f.SetOutput(io.Discard)
	cwd := f.String("cwd", "", "")
	all := f.Bool("all", false, "")
	pages := f.Bool("pages", false, "")
	inboxOnly := f.Bool("inbox", false, "")
	days := f.Int("days", 90, "")
	limit := f.Int("limit", 10, "")
	terms, err := interleaved(f, args)
	if err != nil {
		return usage(errOut, "invalid arguments for search")
	}
	if strings.TrimSpace(strings.Join(terms, "")) == "" {
		return usage(errOut, "search requires at least one term")
	}
	if *days < 0 {
		return usage(errOut, "search --days must be 0 (no limit) or more")
	}
	if *limit < 1 || *limit > 50 {
		return usage(errOut, "search --limit must be from 1 to 50")
	}
	if *cwd == "" {
		*cwd, err = os.Getwd()
		if err != nil {
			fmt.Fprintln(errOut, "eva-md: current directory is unavailable")
			return 1
		}
	}
	p, err := identity.Resolve(ctx, *cwd)
	if err != nil {
		fmt.Fprintln(errOut, "eva-md: project identity is unavailable")
		return 1
	}
	if c.Excluded(*cwd, p.Remote) {
		return 0
	}
	text, err := resume.Search(ctx, resume.SearchOptions{Dirs: d, Config: c, Project: p, All: *all, Pages: *pages, Inbox: *inboxOnly, Terms: terms, Days: *days, Limit: *limit, Now: time.Now()})
	if err != nil {
		fmt.Fprintln(errOut, "eva-md:", err)
		return 1
	}
	fmt.Fprintln(out, text)
	return 0
}

func runInstall(ctx context.Context, args []string, out, errOut io.Writer) int {
	f := flag.NewFlagSet(args[0], flag.ContinueOnError)
	f.SetOutput(io.Discard)
	host := "all"
	dry, apply := false, false
	if args[0] == "install" {
		f.StringVar(&host, "host", "all", "")
		f.BoolVar(&dry, "dry-run", false, "")
	} else {
		f.BoolVar(&apply, "apply", false, "")
	}
	if err := f.Parse(args[1:]); err != nil || f.NArg() != 0 {
		return usage(errOut, "invalid arguments for "+args[0])
	}
	d, err := paths.Default()
	if err != nil {
		fmt.Fprintln(errOut, "eva-md: home folder is unavailable")
		return 1
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	var r install.Result
	if args[0] == "install" {
		binary, e := os.Executable()
		if e != nil {
			fmt.Fprintln(errOut, "eva-md: executable path is unavailable")
			return 1
		}
		r, err = install.Run(ctx, install.Options{Dirs: d, Binary: binary, Host: host, DryRun: dry})
		if err == nil && !dry {
			setAside(d, errOut)
		}
	} else {
		r, err = install.Uninstall(ctx, d, apply)
	}
	// Say whether the actions were only previewed, done, or not completed.
	// A failed install's error says whether its changes were rolled back, or
	// that none of the planned actions ran.
	if len(r.Actions) > 0 {
		switch {
		case dry || args[0] == "uninstall" && !apply:
			fmt.Fprintln(out, "Preview; nothing was changed. Planned actions:")
		case err != nil:
			fmt.Fprintln(out, "Failed. Planned actions:")
		default:
			fmt.Fprintln(out, "Done:")
		}
	}
	for _, action := range r.Actions {
		fmt.Fprintln(out, action)
	}
	if len(r.Actions) == 0 && err == nil {
		if args[0] == "uninstall" {
			fmt.Fprintln(out, "Nothing to remove: eva-md is not installed.")
		} else {
			fmt.Fprintln(out, "Nothing to change: eva-md is installed and up to date.")
		}
	}
	if err != nil {
		fmt.Fprintln(errOut, "eva-md:", err)
		return 1
	}
	if r.NeedsTrust {
		fmt.Fprintln(out, "Review eva-md in Codex /hooks. Start new chats after approval; existing chats are not restarted.")
	}
	return 0
}

// plural picks one or many by n.
func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

// setAside sets aside the chats captured before updates, once, so the
// update starts from the install; if it can't now, the first update does.
func setAside(d paths.Dirs, errOut io.Writer) {
	err := update.Baseline(d, time.Now())
	var left *update.LeftError
	switch {
	case errors.As(err, &left):
		if left.Files > 0 {
			fmt.Fprintf(errOut, "eva-md: %d %s captured before updates couldn't be set aside yet, as %s or watermarks can't be read or written; each update tries again, and eva-md doctor names each once an update has tried it\n", left.Files, plural(left.Files, "chat", "chats"), plural(left.Files, "its file", "their files"))
		}
		if left.Folders > 0 {
			fmt.Fprintf(errOut, "eva-md: %d inbox project %s couldn't be read, so %s chats from before updates wait; each update tries again and says so in its log\n", left.Folders, plural(left.Folders, "folder", "folders"), plural(left.Folders, "its", "their"))
		}
	case err != nil:
		fmt.Fprintln(errOut, "eva-md: the chats captured before updates will be set aside by the first update: "+err.Error())
	}
}

func runDoctor(ctx context.Context, args []string, out, errOut io.Writer) int {
	f := flag.NewFlagSet(args[0], flag.ContinueOnError)
	f.SetOutput(io.Discard)
	asJSON := f.Bool("json", false, "")
	live := f.Bool("live", false, "")
	if err := f.Parse(args[1:]); err != nil || f.NArg() != 0 {
		return usage(errOut, "invalid arguments for "+args[0])
	}
	if *live && args[0] != "doctor" {
		return usage(errOut, "status makes no model call; run eva-md doctor --live")
	}
	d, err := paths.Default()
	if err != nil {
		fmt.Fprintln(errOut, "eva-md: home folder is unavailable")
		return 1
	}
	checkCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	var r health.Report
	if args[0] == "doctor" {
		r = health.Inspect(checkCtx, d, okfModule(), true)
	} else {
		r = health.Status(checkCtx, d, okfModule())
	}
	if *live {
		ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
		defer stop()
		r.Checks = append(r.Checks, liveChecks(ctx, d, errOut)...)
	}
	if *asJSON {
		if err := json.NewEncoder(out).Encode(r); err != nil {
			return 1
		}
	} else {
		for _, check := range r.Checks {
			fmt.Fprintf(out, "%s  %s: %s", check.Status, check.Name, check.Detail)
			if check.Fix != "" {
				fmt.Fprintf(out, "; %s", check.Fix)
			}
			fmt.Fprintln(out)
		}
	}
	if r.Failed() {
		return 1
	}
	return 0
}

// liveChecks runs eva-md doctor --live: the update's exact headless command
// once for each host eva-md is installed for, against a built-in fixture,
// its answer checked by the update's validator, with nothing written. Each
// call's progress goes to errOut, since a call can take minutes.
func liveChecks(ctx context.Context, d paths.Dirs, errOut io.Writer) []health.Check {
	c, err := config.Load(d.ConfigFile())
	if err != nil {
		c = config.Default()
	}
	var checks []health.Check
	for _, host := range []string{update.HostClaude, update.HostCodex} {
		name := host + " live update"
		if app, absent := health.Absent(host); absent && !health.Installed(d, host) {
			checks = append(checks, health.Check{Name: name, Status: "UNKNOWN", Detail: app + " isn't installed on this Mac; no call was made"})
			continue
		}
		if !health.Installed(d, host) {
			checks = append(checks, health.Check{Name: name, Status: "UNKNOWN", Detail: "eva-md is not installed for " + host + "; no call was made", Fix: "run eva-md install"})
			continue
		}
		fmt.Fprintf(errOut, "eva-md doctor --live: running %s's update command on a built-in fixture (one model call)\n", host)
		res, err := update.Live(ctx, newRunner(host), host, c)
		if err != nil {
			checks = append(checks, health.Check{Name: name, Status: "UNKNOWN", Detail: "the check could not run: " + err.Error()})
			continue
		}
		checks = append(checks, liveCheck(name, res))
	}
	return checks
}

// liveCheck words one host's live check.
func liveCheck(name string, res update.LiveResult) health.Check {
	took := fmt.Sprintf("%.1f s", res.Duration.Seconds())
	effort := "effort " + res.Effort + " accepted"
	if res.Effort == "" {
		effort = "accepted without an effort flag"
	}
	if res.Effort != res.Asked {
		effort += " (asked " + res.Asked + ")"
	}
	if res.Reason == "" {
		detail := "valid output in " + took + " from the host's default model; " + effort
		if res.Degraded != "" {
			detail += "; " + res.Degraded
		}
		return health.Check{Name: name, Status: "PASS", Detail: detail}
	}
	return health.Check{Name: name, Status: "FAIL", Detail: res.Reason + " after " + took, Fix: liveFix(res.Reason, res.Host)}
}

// liveFix is what to do about a live check's failure class.
func liveFix(reason, host string) string {
	cli := "claude"
	login := "claude auth login"
	if host == update.HostCodex {
		cli, login = "codex", "codex login"
	}
	switch reason {
	case update.ReasonAuth:
		return "log in with " + login + ", then run eva-md doctor --live again"
	case update.ReasonRateLimit:
		return "the account's usage limit is reached; try again later"
	case update.ReasonHostError:
		return "a network, server or command-line failure; check the connection, and whether a " + cli + " upgrade changed its flags"
	case update.ReasonModelRejected:
		return "the host refused the model or effort; check the effort setting in the eva-md config"
	case update.ReasonToolUsed:
		return "the run used a tool, which update runs forbid; check the " + cli + " version"
	case update.ReasonMCPFailed:
		return "a required MCP server failed to start; fix or remove it in Codex's settings"
	case update.ReasonCLIMissing:
		return "install the " + cli + " CLI or put it on PATH"
	case update.ReasonTimeout:
		return "the run passed its time limit; try a lower effort in the eva-md config"
	}
	return "the answer failed validation; run eva-md doctor --live again, and eva-md update --dry-run --session " + host + ":<id> shows the exact command"
}
