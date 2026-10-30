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
	"runtime/debug"
	"strings"
	"time"

	"github.com/heyNag/eva-md/internal/config"
	"github.com/heyNag/eva-md/internal/hook"
	"github.com/heyNag/eva-md/internal/okfadapter"
	"github.com/heyNag/eva-md/internal/paths"
	"github.com/heyNag/eva-md/internal/wikiinit"
)

// version is set at release time with -ldflags '-X main.version=...'.
var version = "0.1.0-dev"

func main() { os.Exit(run(context.Background(), os.Args[1:], os.Stdin, os.Stdout, os.Stderr)) }

func run(ctx context.Context, args []string, in io.Reader, out, errOut io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(errOut, "usage: eva-md setup | version | init | install | uninstall | doctor | status | brief | show | validate | hook")
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
		return runSetupCommand(ctx, args[1:], in, out, errOut)
	case "init", "validate":
		return runWiki(ctx, args, in, out, errOut)
	case "brief", "show":
		return runResume(ctx, args, out, errOut)
	case "install", "uninstall":
		return runInstall(ctx, args, out, errOut)
	case "doctor", "status":
		return runDoctor(ctx, args, out, errOut)
	default:
		fmt.Fprintf(errOut, "eva-md: unknown command %q; run eva-md with no arguments for the list\n", args[0])
		return 2
	}
}

func usage(w io.Writer, msg string) int { fmt.Fprintln(w, "eva-md: "+msg); return 2 }

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
	ctx, cancel := context.WithTimeout(ctx, 1500*time.Millisecond)
	defer cancel()
	dirs, err := paths.Default()
	if err != nil {
		return hook.Run(ctx, *host, in, out, hook.Env{Getenv: func(string) string { return "1" }})
	}
	c, err := config.LoadContext(ctx, dirs.ConfigFile())
	getenv := os.Getenv
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		// Still let the dispatcher identify Stop and emit its required {}, but
		// make its first recursion guard skip all capture with unreadable config.
		getenv = func(key string) string {
			if key == "EVA_MD_INNER" {
				return "1"
			}
			return os.Getenv(key)
		}
	}
	return hook.Run(ctx, *host, in, out, hook.Env{Dirs: dirs, Config: c, Getenv: getenv, Now: time.Now})
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
	if len(v.Errors)+len(v.Orphans)+len(v.BrokenLinks) > 0 {
		return 1
	}
	for _, warning := range v.Warnings {
		lower := strings.ToLower(warning)
		if strings.Contains(lower, "actor") || strings.Contains(lower, "parent index") || strings.Contains(lower, "parent-index") {
			return 1
		}
	}
	return 0
}

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
