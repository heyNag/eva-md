// Command hostrec is the recorder for the host sittings of
// docs/v2/host-sitting-runbook.md: scratch plugins whose hooks keep what each
// host sends, the commands the run-book's steps name, and the fixtures and
// checks the lead makes from a recording. It is a tool for eva-md's
// developers, kept apart from eva-md: no release or install builds it, and
// eva-md never imports it or internal/hostfacts.
//
// It runs only in an evaluation home: setup refuses a home that holds an
// eva-md install or config, or a host's session from before the account was
// made, and every command but scan, scrub, fixtures and check refuses to run
// without the marker setup writes. The hook and the MCP server then answer
// {} and write nothing.
//
// This is Part 1 of the card docs/v2/cards/HF-00.md: what the run-book's
// core sittings need. The commands of Part 2 are named, and refuse to run.
//
//	go build -o ~/hf-tools/hostrec ./cmd/hostrec
//	hostrec setup --rec ~/hf-rec
package main

import (
	"fmt"
	"io"
	"os"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"
)

func main() {
	var out io.Writer = os.Stdout
	if len(os.Args) > 1 && os.Args[1] == "hook" {
		// The hook's own work is bounded: past its deadline, half the limit
		// its plugin declares for it at most, it answers {} and exits,
		// whatever it was doing, so it stays far under the hosts' limits.
		// Only the first answer is written.
		once := &firstWrite{w: os.Stdout}
		out = once
		time.AfterFunc(hookDeadline(os.Args[2:], startPause), func() {
			fmt.Fprintln(once, "{}")
			os.Exit(0)
		})
	}
	os.Exit(run(os.Args[1:], os.Stdin, out, os.Stderr))
}

// firstWrite passes on its first write and drops every later one.
type firstWrite struct {
	mu   sync.Mutex
	w    io.Writer
	done bool
}

func (f *firstWrite) Write(p []byte) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.done {
		return len(p), nil
	}
	f.done = true
	return f.w.Write(p)
}

// cli is one run of a command: its streams, and the hook's wait for the
// recording's lock when a test gives one.
type cli struct {
	in       io.Reader
	out, err io.Writer
	lockWait time.Duration
}

// A command of the run-book. needsSetup is false only for the commands
// that read a recording handed over, which may run anywhere.
type command struct {
	run        func(c *cli, args []string) int
	needsSetup bool
	help       string
}

var commands = map[string]command{
	"setup":       {cmdSetup, false, "setup --rec <dir>: write the scratch plugins, folders and the private folder"},
	"hook":        {cmdHook, false, "hook --host <host> --event <event>: keep what the host sent"},
	"mark":        {cmdMark, true, "mark <step>: start a step, opening the sitting if it is closed"},
	"close":       {cmdClose, true, "close: close the sitting; the hooks then record nothing"},
	"note":        {cmdNote, true, "note <text>: keep one line the owner writes"},
	"tail":        {cmdTail, true, "tail: one line per event since the step's mark"},
	"arm":         {cmdArm, true, "arm <name>: what the hooks answer"},
	"disarm":      {cmdDisarm, true, "disarm: the hooks record only"},
	"trust":       {cmdTrust, true, "trust: Codex's read-only hooks/list for the scratch hooks"},
	"register":    {cmdRegister, true, "register [claude-code|codex] [<plugin>] [--check]: add the scratch plugins"},
	"unregister":  {cmdUnregister, true, "unregister [claude-code|codex|all] [<plugin>]: remove the scratch plugins"},
	"key":         {cmdKey, true, "key: make the scratch repository's deploy key"},
	"remote":      {cmdRemote, true, "remote set: store the scratch repository's address"},
	"push":        {cmdPush, true, "push: the two pushes of step G-04"},
	"summary":     {cmdSummary, true, "summary <sitting-1|sitting-2|group letter>: what the recording holds for each fact"},
	"voice-check": {cmdVoiceCheck, true, "voice-check: the voice call's counts"},
	"scan":        {cmdScan, false, "scan <dir>: look for what must not be handed over"},
	"scrub":       {cmdScrub, false, "scrub <dir> <text>: replace a hit everywhere"},
	"pack":        {cmdPack, true, "pack [<recording folder>] [--to <dir>] | pack --clean: the recording's archive to hand over"},
	"teardown":    {cmdTeardown, true, "teardown: remove what the sittings made"},
	"fixtures":    {cmdFixtures, false, "fixtures --rec <dir> --out <dir> [--host <host>]: fixtures with every value made up"},
	"check":       {cmdCheck, false, "check --rec <dir> --fixtures <dir>: fail on a recorded value a fixture holds"},
}

// part2 are the commands of the card's Part 2, which this build does not
// have yet. They are named so a step of Part 2 is told plainly.
var part2 = []string{
	"mcp", "lookup", "machine", "agent", "deploy-key", "force-lease", "keychain-names",
	"swap-binary", "plugin-edit", "children", "memory-index", "cli-report", "timing",
}

func run(args []string, in io.Reader, out, errOut io.Writer) int {
	c := &cli{in: in, out: out, err: errOut}
	if len(args) == 0 || args[0] == "help" || args[0] == "-h" || args[0] == "--help" {
		usage(out)
		return 0
	}
	name := args[0]
	cmd, ok := commands[name]
	if !ok {
		if slices.Contains(part2, name) {
			return c.fail("%s belongs to Part 2 of the recorder, which this build does not have yet; ask the lead for the commit of the group", name)
		}
		c.fail("no command %q", name)
		usage(errOut)
		return 2
	}
	return cmd.run(c, args[1:])
}

func usage(w io.Writer) {
	fmt.Fprintln(w, "hostrec, the recorder for the host sittings. Commands:")
	names := make([]string, 0, len(commands))
	for n := range commands {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		fmt.Fprintln(w, "  "+commands[n].help)
	}
	fmt.Fprintln(w, "Part 2, not built yet: "+strings.Join(part2, ", "))
}

// fail prints one line on the error stream and gives exit status 1.
func (c *cli) fail(format string, a ...any) int {
	fmt.Fprintf(c.err, "hostrec: "+format+"\n", a...)
	return 1
}

func (c *cli) say(format string, a ...any) { fmt.Fprintf(c.out, format+"\n", a...) }

// parseArgs splits args into flags, which may come anywhere, and the words
// left. A flag in valued takes the next word, or its own after "=".
func parseArgs(args []string, valued, bare []string) (flags map[string]string, words []string, err error) {
	flags = map[string]string{}
	for i := 0; i < len(args); i++ {
		a := args[i]
		if !strings.HasPrefix(a, "--") || a == "--" {
			words = append(words, a)
			continue
		}
		name, value, hasValue := strings.Cut(a[2:], "=")
		switch {
		case slices.Contains(valued, name):
			if !hasValue {
				if i+1 >= len(args) {
					return nil, nil, fmt.Errorf("--%s needs a value", name)
				}
				i++
				value = args[i]
			}
			flags[name] = value
		case slices.Contains(bare, name) && !hasValue:
			flags[name] = "true"
		default:
			return nil, nil, fmt.Errorf("no flag --%s here", name)
		}
	}
	return flags, words, nil
}
