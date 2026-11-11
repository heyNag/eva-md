package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"time"

	"github.com/heyNag/eva-md/internal/config"
	"github.com/heyNag/eva-md/internal/health"
	"github.com/heyNag/eva-md/internal/ledger/shapes"
	"github.com/heyNag/eva-md/internal/paths"
)

// shapesTimeout bounds eva-md audit shapes, which reads every host file of
// a session the inbox holds, as reportTimeout bounds the doctor's report.
const shapesTimeout = 10 * time.Minute

// runShapes runs eva-md audit shapes: the shapes of the hosts' records of
// the chats eva-md captured, and of eva-md's own, as JSON of counts,
// buckets, days, versions and fixed names only (see shapes.Run). It reads
// within limit; a part cut short says so, and the report still exits 0. It
// writes nothing, runs no command and makes no network or model call.
func runShapes(ctx context.Context, args []string, out, errOut io.Writer, limit time.Duration) int {
	if len(args) != 2 {
		return usage(errOut, "audit shapes takes no arguments: eva-md audit shapes")
	}
	dirs, err := paths.Default()
	home, herr := os.UserHomeDir()
	if err != nil || herr != nil {
		fmt.Fprintln(errOut, "eva-md: home folder is unavailable")
		return 1
	}
	o := shapes.Options{Dirs: dirs, Home: home, TempDir: os.Getenv("TMPDIR"), Version: health.Version, Now: time.Now()}
	o.Config, err = config.Load(dirs.ConfigFile())
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		fmt.Fprintln(errOut, "eva-md: the config could not be read, so the folders left out by design are unknown")
		return 1
	}
	ctx, cancel := context.WithTimeout(ctx, limit)
	defer cancel()
	report, err := shapes.Run(ctx, o)
	if err != nil {
		fmt.Fprintln(errOut, "eva-md: the inbox could not be read; nothing was counted")
		return 1
	}
	if err := report.Write(out); err != nil {
		fmt.Fprintln(errOut, "eva-md: the report held something no list holds, so it was not printed")
		return 1
	}
	return 0
}
