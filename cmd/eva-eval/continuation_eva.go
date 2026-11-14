package main

import (
	"context"
	"io"
	"strings"

	"github.com/heyNag/eva-md/internal/eval/continuation/task"
	"github.com/heyNag/eva-md/internal/eval/continuation/v1arm"
)

// eva-eval continuation run's v1 condition, eva (EV-17c): importing v1arm
// registers it with the driver. This file gives run one flag more, for
// that condition alone: --sign-in PATH, a program run once in each trial,
// before setup, in the trial's environment and with its home as its
// folder, that signs the host's CLI in there by the host's own means, so
// that v1's update, which gives the CLI a cleared environment, finds a
// sign-in in the trial's home. The flag is taken out of run's arguments
// before run reads them, so run's own file is unchanged. A run of the
// condition with no --eva-md is a usage error, as a missing flag is.
func init() {
	c, ok := continuationCommands["run"]
	if !ok {
		panic("eva-eval: the continuation command run is not added before its eva condition's flag")
	}
	inner := c.run
	c.synopsis += " [--sign-in PATH, with --condition " + string(task.Eva) + "]"
	c.run = func(ctx context.Context, args []string, out, errOut io.Writer) int {
		rest, signIn, msg := takeSignIn(args)
		if msg != "" {
			return continuationFail(errOut, msg)
		}
		eva := lastValue(rest, "condition") == string(task.Eva)
		switch {
		case signIn != "" && !eva:
			return continuationFail(errOut, "--sign-in is for --condition "+string(task.Eva)+" alone")
		case eva && lastValue(rest, "eva-md") == "":
			return continuationFail(errOut, "--condition "+string(task.Eva)+" needs --eva-md, an evaluation build of eva-md")
		}
		v1arm.SetSignIn(signIn)
		defer v1arm.SetSignIn("")
		return inner(ctx, rest, out, errOut)
	}
	continuationCommands["run"] = c
}

// flagName splits a, an argument that is a flag, -name or --name, with
// =value or without, into its name and value; ok is false for any other.
func flagName(a string) (name, value string, inline, ok bool) {
	if !strings.HasPrefix(a, "-") {
		return "", "", false, false
	}
	name, value, inline = strings.Cut(strings.TrimPrefix(strings.TrimPrefix(a, "-"), "-"), "=")
	return name, value, inline, true
}

// takeSignIn takes --sign-in PATH, or --sign-in=PATH, with one dash or
// two, out of a run's arguments, up to a "--" that ends its flags, and
// gives the rest and the path, or a usage error's words.
func takeSignIn(args []string) (rest []string, path, msg string) {
	given := false
	for i := 0; i < len(args); i++ {
		if args[i] == "--" {
			rest = append(rest, args[i:]...)
			break
		}
		name, value, inline, ok := flagName(args[i])
		if !ok || name != "sign-in" {
			rest = append(rest, args[i])
			continue
		}
		if !inline {
			if i+1 >= len(args) {
				return nil, "", "--sign-in needs a program's path"
			}
			i++
			value = args[i]
		}
		switch {
		case given:
			return nil, "", "--sign-in is given twice"
		case value == "":
			return nil, "", "--sign-in needs a program's path"
		}
		given, path = true, value
	}
	return rest, path, ""
}

// lastValue is the value of a run's last --<flag>, as the flag package
// reads it, or "".
func lastValue(args []string, flag string) string {
	last := ""
	for i := 0; i < len(args); i++ {
		if args[i] == "--" {
			break
		}
		name, value, inline, ok := flagName(args[i])
		if !ok || name != flag {
			continue
		}
		if !inline {
			if i+1 >= len(args) {
				break
			}
			i++
			value = args[i]
		}
		last = value
	}
	return last
}
