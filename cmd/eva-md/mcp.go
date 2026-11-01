package main

import (
	"context"
	"flag"
	"io"
	"os"
	"runtime/debug"
	"time"

	"github.com/heyNag/eva-md/internal/mcp"
	"github.com/heyNag/eva-md/internal/paths"
)

// runMCP is eva-md mcp --host claude-code|codex, the read-only MCP server
// the plugins' .mcp.json starts once a chat. It is not meant to be run by
// hand. A usage error exits 2 before stdin is read.
func runMCP(ctx context.Context, args []string, in io.Reader, out, errOut io.Writer) int {
	f := flag.NewFlagSet("mcp", flag.ContinueOnError)
	f.SetOutput(io.Discard)
	host := f.String("host", "", "")
	if err := f.Parse(args); err != nil || f.NArg() != 0 || (*host != "claude-code" && *host != "codex") {
		return usage(errOut, "mcp --host must be claude-code or codex; the plugins start it")
	}
	// A soft ceiling: one server runs for every open chat.
	defer debug.SetMemoryLimit(debug.SetMemoryLimit(64 << 20))
	// A host may start the server with a filtered environment. Without HOME,
	// the home folder is the one the installed binary's path names, set for
	// this process so the config's ~/ exclusions still apply; without PATH,
	// git is looked for in the system's folders.
	if home, err := os.UserHomeDir(); err != nil || home == "" {
		if exe, err := os.Executable(); err == nil {
			if home, ok := mcp.HomeFromBinary(exe); ok {
				os.Setenv("HOME", home)
			}
		}
	}
	if os.Getenv("PATH") == "" {
		os.Setenv("PATH", "/usr/bin:/bin:/usr/sbin:/sbin")
	}
	s := mcp.Server{Host: *host, Version: version, Now: time.Now, Getenv: os.Getenv, Parent: os.Getppid(), Getppid: os.Getppid}
	s.Dirs, s.DirsErr = paths.Default()
	s.Getwd, _ = os.Getwd()
	return mcp.Serve(ctx, s, in, out, errOut)
}
