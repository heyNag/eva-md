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
	"time"

	"github.com/heyNag/eva-md/internal/config"
	"github.com/heyNag/eva-md/internal/health"
	"github.com/heyNag/eva-md/internal/identity"
	"github.com/heyNag/eva-md/internal/install"
	"github.com/heyNag/eva-md/internal/paths"
	"github.com/heyNag/eva-md/internal/redact"
	"github.com/heyNag/eva-md/internal/resume"
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
		if len(args) != 2 {
			return usage(errOut, "show requires a source ref or page[#section]")
		}
		text, err := resume.Show(ctx, d, c, args[1])
		if err != nil {
			fmt.Fprintln(errOut, "eva-md:", err)
			return 1
		}
		fmt.Fprintln(out, text)
		return 0
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
	} else {
		r, err = install.Uninstall(ctx, d, apply)
	}
	for _, action := range r.Actions {
		fmt.Fprintln(out, action)
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

func runDoctor(ctx context.Context, args []string, out, errOut io.Writer) int {
	f := flag.NewFlagSet(args[0], flag.ContinueOnError)
	f.SetOutput(io.Discard)
	asJSON := f.Bool("json", false, "")
	live := f.Bool("live", false, "")
	if err := f.Parse(args[1:]); err != nil || f.NArg() != 0 {
		return usage(errOut, "invalid arguments for "+args[0])
	}
	if *live {
		return usage(errOut, "doctor --live needs background wiki updates, which are not built yet; no model call was made")
	}
	d, err := paths.Default()
	if err != nil {
		fmt.Fprintln(errOut, "eva-md: home folder is unavailable")
		return 1
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	r := health.Inspect(ctx, d, okfModule(), args[0] == "doctor")
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
