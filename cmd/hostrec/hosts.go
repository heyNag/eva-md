package main

import "github.com/heyNag/eva-md/internal/host/adapter"

// The hosts as the recorder names them: by their ids, which the hook's
// --host takes and the marketplaces' folders are named by, and by their
// CLIs. They come from internal/host/adapter, the one place that writes a
// host's name.
var (
	hostClaude = string(adapter.ClaudeCode)
	hostCodex  = string(adapter.Codex)
	cliClaude  = cliOf(adapter.ClaudeCode)
	cliCodex   = cliOf(adapter.Codex)
)

func cliOf(id adapter.ID) string {
	h, ok := adapter.ByID(string(id))
	if !ok {
		panic("the host adapter does not know " + string(id))
	}
	return h.Names.CLI
}
