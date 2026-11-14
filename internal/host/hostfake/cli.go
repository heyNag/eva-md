package hostfake

import (
	"context"
	"errors"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
)

// The variables a written CLI sets for the test binary it runs.
const (
	rootEnv = "EVA_MD_HOSTFAKE_ROOT"
	cliEnv  = "EVA_MD_HOSTFAKE_CLI"
)

// Main answers as a fake CLI when the process was started as one by a CLI
// WriteCLIs wrote, and exits; otherwise it returns at once. A test package
// that uses the written CLIs calls it first in its TestMain, or in an init
// function of one of its test files.
func Main() {
	root, cli := os.Getenv(rootEnv), os.Getenv(cliEnv)
	if root == "" || cli == "" {
		return
	}
	// A stalled command waits until it is told to stop, or killed.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, os.Interrupt)
	code, _ := New(root).serve(ctx, os.Stdin, os.Stdout, os.Stderr, cli, os.Args[1:])
	stop()
	// Not os.Exit: under the race detector it waits a second at every exit.
	syscall.Exit(code)
}

// WriteCLIs writes executables named claude and codex in bin, which it
// makes, that answer as Run does, sharing this fake's state. They run the
// test binary that calls WriteCLIs, so its package calls Main first.
func (f *Fake) WriteCLIs(bin string) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	if !filepath.IsAbs(bin) {
		return errors.New("hostfake: WriteCLIs needs an absolute folder")
	}
	if err := os.MkdirAll(bin, 0o700); err != nil {
		return err
	}
	for _, cli := range []string{Claude, Codex} {
		script := "#!/bin/sh\n" +
			rootEnv + "=" + quote(f.root) + "\n" +
			cliEnv + "=" + quote(cli) + "\n" +
			"export " + rootEnv + " " + cliEnv + "\n" +
			"exec " + quote(exe) + " \"$@\"\n"
		if err := os.WriteFile(filepath.Join(bin, cli), []byte(script), 0o700); err != nil {
			return err
		}
	}
	return nil
}

func quote(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'" }
