package main

import (
	"context"
	"io"
	"slices"
	"strings"
)

// command is one command of eva-md. Each registers itself, from the file
// that holds it, with register in an init function, so a new command is a
// new file and no edit to run or to this table.
type command struct {
	// name is the command as typed, as inbox in eva-md inbox clear.
	name string
	// file is the file of cmd/eva-md that holds the command.
	file string
	// place orders the usage line, lowest first. Places are unique and
	// leave gaps, so a new command fits between two without renumbering.
	place int
	// synopsis is each synopsis of the command as README's Commands list
	// shows it, from eva-md on, one for each form of the command.
	synopsis []string
	// run runs the command. args[0] is its name.
	run func(ctx context.Context, args []string, in io.Reader, out, errOut io.Writer) int
}

// commands is the command table, in the order the files registered.
var commands []command

func register(c command) { commands = append(commands, c) }

// lookup finds the command typed as name.
func lookup(name string) (command, bool) {
	for _, c := range commands {
		if c.name == name {
			return c, true
		}
	}
	return command{}, false
}

// usageLine is what eva-md prints with no arguments: every command, in the
// order of their places.
func usageLine() string {
	sorted := slices.Clone(commands)
	slices.SortFunc(sorted, func(a, b command) int { return a.place - b.place })
	names := make([]string, len(sorted))
	for i, c := range sorted {
		names[i] = c.name
	}
	return "usage: eva-md " + strings.Join(names, " | ")
}

// supportFiles are the files of cmd/eva-md that hold no command but support
// those that do, each with what it holds. Every other file is named by a
// command of the table; a test fails on a file in neither.
var supportFiles = []struct{ file, holds string }{
	{"table.go", "the command table and the list of these files"},
	{"code.go", "the code status of system pages, eva-md system status"},
	{"knowledge.go", "maintain's knowledge step"},
	{"rosetta_darwin.go", "upgrade's Rosetta check"},
	{"rosetta_other.go", "the Rosetta check's stand-in off macOS, so eva-md builds on Linux"},
	{"setup_checks.go", `setup's "Checking this Mac" checklist`},
	{"setup_refusals.go", "how setup says why gh, glab or git refused, and the internal-repo question"},
	{"setup_repo.go", "how setup creates the wiki's repo with gh or glab, checks that it is private and finds it to clone again"},
	{"timelimits.go", "the time limits tests raise"},
	{"timelimits_raised.go", "the tests' release stand-in, a build with the evamd_testlimits tag: it raises the time limits and sets hostOS to darwin"},
}
