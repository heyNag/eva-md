package main

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/heyNag/eva-md/internal/install"
	"github.com/heyNag/eva-md/internal/paths"
	"github.com/heyNag/eva-md/internal/release"
)

// versionTimeout bounds the downloaded binary's eva-md version. Tests raise
// it before any test runs.
var versionTimeout = 30 * time.Second

// upgradeArch is this Mac's architecture as the release assets name it:
// arm64, also for an amd64 binary that Rosetta runs on an Apple silicon
// Mac, or amd64.
var upgradeArch = func() string {
	if runtime.GOARCH == "amd64" {
		if v, err := syscall.SysctlUint32("sysctl.proc_translated"); err == nil && v == 1 {
			return "arm64"
		}
	}
	return runtime.GOARCH
}

// runUpgrade is eva-md upgrade: it finds the latest release, or takes
// --version, and when that is newer than this binary, or --force says so,
// downloads the release's binary for this Mac with SHA256SUMS into a new
// temporary folder in the data folder, checks the binary's sum and the
// version it reports, and runs its setup attached to this terminal. On a Mac
// that is set up, setup keeps the wiki and updates the stable binary and the
// plugins. Nothing is changed before setup runs, and setup rolls back its own
// install when that fails. It is the only network call eva-md makes itself.
func runUpgrade(ctx context.Context, args []string, in io.Reader, out, errOut io.Writer) int {
	f := flag.NewFlagSet("upgrade", flag.ContinueOnError)
	f.SetOutput(io.Discard)
	check := f.Bool("check", false, "")
	force := f.Bool("force", false, "")
	pinned := f.String("version", "", "")
	if err := f.Parse(args); err != nil || f.NArg() != 0 {
		return usage(errOut, "invalid arguments for upgrade")
	}
	if *pinned != "" && !release.ValidTag(*pinned) {
		return usage(errOut, "upgrade --version must be a release tag, such as 2026.11.3")
	}
	d, err := paths.Default()
	if err != nil {
		fmt.Fprintln(errOut, "eva-md: home folder is unavailable")
		return 1
	}
	// Ctrl-C ends a download; once setup runs, it gets the signal itself
	// and finishes or rolls back its own install.
	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()
	tag := *pinned
	if tag == "" {
		if tag, err = release.Latest(ctx); err != nil {
			fmt.Fprintf(errOut, "eva-md: upgrade: %v; nothing was changed\n", err)
			return 1
		}
	}
	target, _ := release.ParseTag(tag)
	current := release.Parse(version)
	newer := release.Replaces(target, current)
	if *check || !newer && !*force {
		switch c := release.Compare(target, current); {
		case newer && *pinned != "":
			fmt.Fprintf(out, "eva-md %s is available; this is %s. Run eva-md upgrade --version %s to install it.\n", tag, version, tag)
		case newer:
			fmt.Fprintf(out, "eva-md %s is available; this is %s. Run eva-md upgrade to install it.\n", tag, version)
		case current.Dev:
			fmt.Fprintf(out, "eva-md %s has no release version; eva-md upgrade --force replaces it with %s.\n", version, tag)
		case current.Source:
			fmt.Fprintf(out, "eva-md %s is a source build of the same day as %s or later; eva-md upgrade --force replaces it with %s.\n", version, tag, tag)
		case c == 0:
			fmt.Fprintf(out, "eva-md %s is up to date\n", version)
		default:
			fmt.Fprintf(out, "eva-md %s is up to date; %s is older, and eva-md upgrade --version %s --force installs it.\n", version, tag, tag)
		}
		return 0
	}
	code, err := upgradeTo(ctx, d, tag, in, out, errOut)
	if err != nil {
		fmt.Fprintf(errOut, "eva-md: upgrade failed: %v; the installed eva-md and its plugins are as they were\n", err)
		return 1
	}
	if code != 0 {
		fmt.Fprintln(errOut, "eva-md: "+setupStopped(ctx, d, tag))
	}
	return code
}

// setupStopped is the closing line when the new binary's setup stopped. Once
// its install has put the release in place, eva-md upgrade would say it is
// up to date and never run setup again, so the line names setup itself.
func setupStopped(ctx context.Context, d paths.Dirs, tag string) string {
	stable := filepath.Join(d.Share, "bin", "eva-md")
	if got, err := binaryVersion(ctx, stable); err != nil || got != tag {
		return "setup stopped; when it named a problem, fix it, then run eva-md upgrade again"
	}
	name := stable
	if to, err := os.Readlink(install.LinkPath(d)); err == nil && to == stable {
		name = "eva-md"
	}
	return fmt.Sprintf("setup stopped after installing eva-md %s; when it named a problem, fix it, then run %s setup to finish", tag, name)
}

// upgradeTo downloads and checks release tag, then runs its setup and
// returns setup's exit status. An error comes before setup runs.
func upgradeTo(ctx context.Context, d paths.Dirs, tag string, in io.Reader, out, errOut io.Writer) (int, error) {
	arch := upgradeArch()
	asset := release.Asset(arch)
	fmt.Fprintf(out, "Upgrading to eva-md %s for %s\n", tag, arch)
	if err := paths.EnsureDir(d.Share); err != nil {
		return 0, err
	}
	tmp, err := os.MkdirTemp(d.Share, ".upgrade-")
	if err != nil {
		return 0, err
	}
	defer os.RemoveAll(tmp)
	binary, sums := filepath.Join(tmp, "eva-md"), filepath.Join(tmp, release.SumsName)
	if err := release.Download(ctx, tag, asset, binary, release.MaxBinary); err != nil {
		return 0, err
	}
	if err := release.Download(ctx, tag, release.SumsName, sums, release.MaxSums); err != nil {
		return 0, err
	}
	list, err := os.ReadFile(sums)
	if err != nil {
		return 0, err
	}
	if err := release.Verify(list, asset, binary); err != nil {
		return 0, err
	}
	fmt.Fprintln(out, "Checksum verified")
	if err := os.Chmod(binary, 0o700); err != nil {
		return 0, err
	}
	if got, err := binaryVersion(ctx, binary); err != nil {
		return 0, err
	} else if got != tag {
		return 0, fmt.Errorf("the downloaded binary says it is eva-md %s, not %s", got, tag)
	}
	fmt.Fprintln(out)
	// Setup runs to its own end, even after Ctrl-C, which reaches it from
	// the terminal.
	cmd := exec.Command(binary, "setup")
	cmd.Stdin, cmd.Stdout, cmd.Stderr = in, out, errOut
	err = cmd.Run()
	var exit *exec.ExitError
	switch {
	case errors.As(err, &exit):
		return exit.ExitCode(), nil
	case err != nil:
		return 0, fmt.Errorf("couldn't run the new binary's setup: %v", err)
	}
	return 0, nil
}

// binaryVersion is the version binary reports, from the first line of its
// eva-md version.
func binaryVersion(ctx context.Context, binary string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, versionTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, binary, "version")
	cmd.WaitDelay = time.Second
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	if err := cmd.Run(); err != nil {
		return "", errors.New("the downloaded binary doesn't run on this Mac")
	}
	line, _, _ := strings.Cut(stdout.String(), "\n")
	v, ok := strings.CutPrefix(line, "eva-md ")
	if !ok {
		return "", errors.New("the downloaded binary doesn't say its version")
	}
	return strings.TrimSpace(v), nil
}
