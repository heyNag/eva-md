package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"

	"github.com/heyNag/eva-md/internal/config"
	"github.com/heyNag/eva-md/internal/ledger/audit"
	"github.com/heyNag/eva-md/internal/paths"
)

func init() {
	register(command{name: "audit", file: "audit.go", place: 135,
		synopsis: []string{"eva-md audit capture [--session ID | --last N]", "eva-md audit shapes"},
		run: func(ctx context.Context, args []string, _ io.Reader, out, errOut io.Writer) int {
			return runAudit(ctx, args, out, errOut)
		}})
}

// runAudit runs eva-md audit capture: it compares what v1 captured of each
// session with the host's own record of it and prints numbers only. It
// reads the inbox, the recovery records and the transcripts they name, and
// writes nothing. --session audits one session, by its id or a ref, and
// --last the newest N. eva-md audit shapes is in shapes.go.
func runAudit(ctx context.Context, args []string, out, errOut io.Writer) int {
	if len(args) >= 2 && args[1] == "shapes" {
		return runShapes(ctx, args, out, errOut, shapesTimeout)
	}
	if len(args) < 2 || args[1] != "capture" {
		return usage(errOut, "audit takes capture or shapes: eva-md audit capture [--session ID | --last N] | eva-md audit shapes")
	}
	var o audit.Options
	f := flag.NewFlagSet("audit capture", flag.ContinueOnError)
	f.SetOutput(io.Discard)
	f.Func("session", "", func(v string) error {
		id, err := inboxSessionArg(v)
		o.Session = id
		return err
	})
	f.IntVar(&o.Last, "last", 0, "")
	if err := f.Parse(args[2:]); err != nil || f.NArg() != 0 {
		return usage(errOut, "invalid arguments for audit capture; --session takes a session id or a ref such as s:3f2a91c0, --last a number above 0")
	}
	given := map[string]bool{}
	f.Visit(func(fl *flag.Flag) { given[fl.Name] = true })
	if given["last"] && o.Last < 1 || given["session"] && given["last"] {
		return usage(errOut, "audit capture takes --session or --last, not both, and --last a number above 0")
	}
	dirs, err := paths.Default()
	home, herr := os.UserHomeDir()
	if err != nil || herr != nil {
		fmt.Fprintln(errOut, "eva-md: home folder is unavailable")
		return 1
	}
	o.Dirs, o.Home, o.TempDir = dirs, home, os.Getenv("TMPDIR")
	o.Config, err = config.Load(dirs.ConfigFile())
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		fmt.Fprintln(errOut, "eva-md: the config could not be read, so the folders left out by design are unknown")
		return 1
	}
	report, err := audit.Run(ctx, o)
	if err != nil {
		fmt.Fprintln(errOut, "eva-md: the inbox could not be read in full; nothing was audited")
		return 1
	}
	if err := report.Write(out); err != nil {
		return 1
	}
	return 0
}
