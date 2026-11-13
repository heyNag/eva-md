//go:build evamd_eval

package main

import "os"

// A build with the evamd_eval tag is the evaluation build, never a release:
// the continuation sessions of the evaluation run it in Linux containers, as
// scripted chats. Its setup takes the machine for a Mac (see hostOS), as the
// tests' release stand-in does, and it has one seam: when the command is
// the hook and EVA_MD_EVAL_ACTOR is person, the hook takes a Claude Code
// chat for a person's whatever entrypoint the program that started it gave,
// by giving the process the terminal's entrypoint, cli, before the hook
// reads it. Any other value of the variable, or none, changes nothing; a
// Codex chat reads no entrypoint. No release, script or workflow passes the
// tag, and evalbuild_test.go holds that.
func init() {
	hostOS = "darwin"
	if len(os.Args) > 1 && os.Args[1] == "hook" && os.Getenv("EVA_MD_EVAL_ACTOR") == "person" {
		_ = os.Setenv("CLAUDE_CODE_ENTRYPOINT", "cli")
	}
}
