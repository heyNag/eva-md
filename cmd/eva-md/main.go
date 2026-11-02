// eva-md is the platform CLI and short-lived hook handler for Eva dot MD.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"regexp"
	"runtime/debug"
	"time"

	"github.com/heyNag/eva-md/internal/config"
	"github.com/heyNag/eva-md/internal/health"
	"github.com/heyNag/eva-md/internal/hook"
	"github.com/heyNag/eva-md/internal/install"
	"github.com/heyNag/eva-md/internal/okfadapter"
	"github.com/heyNag/eva-md/internal/paths"
	"github.com/heyNag/eva-md/internal/wikiinit"
)

// version is the binary's version. A release build may set it with
// -ldflags '-X main.version=...'; left as "dev", main takes it from the
// commit the binary was built from (see calver).
var version = "dev"

func main() {
	if version == "dev" {
		info, _ := debug.ReadBuildInfo()
		version = calver(info)
	}
	// The doctor's repairs write the version as the author of what they
	// build again.
	health.Version = version
	os.Exit(run(context.Background(), os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}

func run(ctx context.Context, args []string, in io.Reader, out, errOut io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(errOut, "usage: eva-md setup | version | upgrade | init | install | uninstall | doctor | status | brief | show | search | index | update | commit | maintain | system | validate | hook | mcp")
		return 2
	}
	if args[0] == "hook" {
		return runHook(ctx, args[1:], in, out)
	}
	switch args[0] {
	case "version":
		if len(args) != 1 {
			return usage(errOut, "version takes no arguments")
		}
		fmt.Fprintf(out, "eva-md %s\nschema_version 1\nokf_spec 0.2\nokf_module %s\n", version, okfModule())
		return 0
	case "setup":
		code := runSetupCommand(ctx, args[1:], in, out, errOut)
		recordSetupWrites(ctx)
		return code
	case "init", "validate":
		return runWiki(ctx, args, in, out, errOut)
	case "brief", "show", "search":
		return runResume(ctx, args, out, errOut)
	case "install":
		return runInstall(ctx, args, out, errOut)
	case "uninstall":
		return runUninstall(ctx, args[1:], in, out, errOut, isTerminal(in))
	case "upgrade":
		return runUpgrade(ctx, args[1:], in, out, errOut)
	case "doctor", "status":
		return runDoctor(ctx, args, out, errOut)
	case "commit":
		return runCommit(ctx, args, out, errOut)
	case "update":
		return runUpdate(ctx, args, out, errOut)
	case "maintain":
		return runMaintain(ctx, args, out, errOut)
	case "index":
		return runIndex(ctx, args, out, errOut)
	case "system":
		return runSystem(ctx, args, in, out, errOut)
	case "mcp":
		return runMCP(ctx, args[1:], in, out, errOut)
	default:
		fmt.Fprintf(errOut, "eva-md: unknown command %q; run eva-md with no arguments for the list\n", args[0])
		return 2
	}
}

// recordSetupWrites adds config.toml and config.env to the install record,
// once an install has written one: on a Mac set up before there was a
// config.env, setup writes it after its install. It fails nothing; the next
// install lists them anyway.
func recordSetupWrites(ctx context.Context) {
	d, err := paths.Default()
	if err != nil {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, installTimeout)
	defer cancel()
	_ = install.RecordConfigFiles(ctx, d)
}

func usage(w io.Writer, msg string) int { fmt.Fprintln(w, "eva-md: "+msg); return 2 }

// hookSlack lengthens every time limit of the hook. Tests set it, before any
// test runs, so a busy machine can't fail a test of what a hook stores. It
// is zero in production.
var hookSlack time.Duration

// Hook failures are always silent, fail closed and return success to the host.
func runHook(ctx context.Context, args []string, in io.Reader, out io.Writer) (code int) {
	defer func() {
		if recover() != nil {
			code = 0
		}
	}()
	f := flag.NewFlagSet("hook", flag.ContinueOnError)
	f.SetOutput(io.Discard)
	host := f.String("host", "", "")
	if err := f.Parse(args); err != nil || f.NArg() != 0 || (*host != "codex" && *host != "claude-code") {
		return 0
	}
	ctx, cancel := context.WithTimeout(ctx, 1500*time.Millisecond+hookSlack)
	defer cancel()
	dirs, err := paths.Default()
	if err != nil {
		return hook.Run(ctx, *host, in, out, hook.Env{Getenv: func(string) string { return "1" }, Slack: hookSlack})
	}
	c, err := config.LoadContext(ctx, dirs.ConfigFile())
	getenv := os.Getenv
	unreadable := false
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		// Still let the dispatcher identify Stop and emit its required {}, but
		// make its first recursion guard skip all capture with unreadable config.
		getenv = func(key string) string {
			if key == "EVA_MD_INNER" {
				return "1"
			}
			return os.Getenv(key)
		}
		// A chat start says so, unless the read ran out of time, which says
		// nothing of the config, or the hook runs in eva-md's own run.
		unreadable = ctx.Err() == nil && os.Getenv("EVA_MD_INNER") != "1"
	}
	return hook.Run(ctx, *host, in, out, hook.Env{Dirs: dirs, Config: c, Getenv: getenv, Now: time.Now, Slack: hookSlack, ConfigUnreadable: unreadable})
}

func runWiki(ctx context.Context, args []string, in io.Reader, out, errOut io.Writer) int {
	f := flag.NewFlagSet(args[0], flag.ContinueOnError)
	f.SetOutput(io.Discard)
	dir := f.String("dir", "", "")
	remote := ""
	noPush := false
	if args[0] == "init" {
		f.StringVar(&remote, "remote", "", "")
		f.BoolVar(&noPush, "no-push", false, "")
	}
	if err := f.Parse(args[1:]); err != nil || f.NArg() != 0 {
		return usage(errOut, "invalid arguments for "+args[0])
	}
	dirs, err := paths.Default()
	if err != nil {
		fmt.Fprintln(errOut, "eva-md: home folder is unavailable")
		return 1
	}
	if args[0] == "init" {
		if remote == "" || *dir == "" {
			return usage(errOut, "init requires --remote and --dir; eva-md setup asks for both")
		}
		r, err := wikiinit.Run(ctx, wikiinit.Options{Remote: remote, Dir: *dir, NoPush: noPush, Version: version, OKFModule: okfModule(), Dirs: dirs})
		if err != nil {
			fmt.Fprintln(errOut, "eva-md:", err)
			return 1
		}
		fmt.Fprintf(out, "Wiki ready: %s\nInstance: %s\n", r.WikiPath, r.Instance)
		if noPush {
			fmt.Fprintln(out, "Push skipped (--no-push).")
		}
		return 0
	}
	if *dir == "" {
		c, err := config.Load(dirs.ConfigFile())
		if err != nil || c.WikiPath == "" {
			fmt.Fprintln(errOut, "eva-md: pass --dir or initialize the wiki first")
			return 1
		}
		*dir = c.WikiPath
	}
	w, err := okfadapter.Open(*dir, dirs.Locks())
	if err != nil {
		fmt.Fprintln(errOut, "eva-md:", err)
		return 1
	}
	v, err := w.Validate()
	if err != nil {
		fmt.Fprintln(errOut, "eva-md:", err)
		return 1
	}
	fmt.Fprintf(out, "Concepts: %d\nErrors: %d\nWarnings: %d\nOrphans: %d\nBroken links: %d\n", v.ConceptCount, len(v.Errors), len(v.Warnings), len(v.Orphans), len(v.BrokenLinks))
	// Findings can contain page titles or metadata. Report counts, not captured
	// content; inspection remains local in the named wiki.
	if len(v.Errors)+len(v.Orphans)+len(v.BrokenLinks) > 0 || wikiinit.BlockingWarning(v.Warnings) {
		return 1
	}
	return 0
}

// calver returns a build's version from the commit it was built from: the
// commit's day in UTC as year.month.day, then as build metadata the commit's
// UTC time as hours and minutes, so builds of one day sort, and its short
// hash, so each is its own, with ".dirty" for a build of a changed working
// tree, as in 2026.11.2+0915.9e4f609. It is a valid semantic version. Go records
// the commit in the binary when it builds from a git checkout, and puts it
// in a pseudo-version, such as v0.0.0-20261102091551-9e4f609abcde, when it
// builds a module it fetched. Without either, as in a test binary, it is
// "dev".
func calver(info *debug.BuildInfo) string {
	if info == nil {
		return "dev"
	}
	var at time.Time
	revision, dirty := "", false
	for _, s := range info.Settings {
		switch s.Key {
		case "vcs.time":
			at, _ = time.Parse(time.RFC3339, s.Value)
		case "vcs.revision":
			revision = s.Value
		case "vcs.modified":
			dirty = s.Value == "true"
		}
	}
	if at.IsZero() {
		if m := pseudoVersion.FindStringSubmatch(info.Main.Version); m != nil {
			at, _ = time.Parse("20060102150405", m[1])
			revision, dirty = m[2], false
		}
	}
	if at.IsZero() {
		return "dev"
	}
	at = at.UTC()
	v := fmt.Sprintf("%d.%d.%d+%s", at.Year(), at.Month(), at.Day(), at.Format("1504"))
	if len(revision) >= 7 {
		v += "." + revision[:7]
		if dirty {
			v += ".dirty"
		}
	}
	return v
}

// pseudoVersion is the end of a Go pseudo-version: the commit's time in UTC
// and the first 12 characters of its hash, in each of the three forms Go
// writes.
var pseudoVersion = regexp.MustCompile(`[-.]([0-9]{14})-([0-9a-f]{12})(?:\+incompatible)?$`)

func okfModule() string {
	if info, ok := debug.ReadBuildInfo(); ok {
		for _, d := range info.Deps {
			if d.Path == "github.com/okf-memory/okf-agent-memory" {
				return d.Version
			}
		}
	}
	return "unknown"
}
