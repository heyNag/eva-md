// Package adapter is the one place that knows a host: its id, the names it
// goes by, the versions eva-md was checked against with what each declares
// and the quirks seen there, and how to find its CLI and read its version.
// Other packages ask it instead of writing a host's name themselves, so a
// third host is a row in its tables, not edits across packages.
package adapter

// ID names a host: the spelling eva-md hook --host and eva-md mcp --host
// accept.
type ID string

const (
	ClaudeCode ID = "claude-code"
	Codex      ID = "codex"
)

// Names are the spellings of one host apart from its id. They are stored on
// users' machines and never change.
type Names struct {
	// App is the host's name in what a person reads.
	App string
	// CLI is the command that is run.
	CLI string
	// InstallName is the spelling of install's, setup's and uninstall's
	// --host, of the install record's host field, of the folder
	// <share>/marketplace/<name> and of the keys of cli-paths.json.
	InstallName string
}

// Host is one host eva-md knows.
type Host struct {
	ID    ID
	Names Names
}

// registry holds the hosts in v1's order.
var registry = []Host{
	{ID: ClaudeCode, Names: Names{App: "Claude Code", CLI: "claude", InstallName: "claude"}},
	{ID: Codex, Names: Names{App: "Codex", CLI: "codex", InstallName: "codex"}},
}

// All gives every host eva-md knows, in v1's order: Claude Code, then Codex.
func All() []Host {
	return append([]Host(nil), registry...)
}

// ByID finds a host by its id, spelled exactly as the hook's --host takes it.
func ByID(id string) (Host, bool) {
	for _, h := range registry {
		if string(h.ID) == id {
			return h, true
		}
	}
	return Host{}, false
}

// ByInstallName finds a host by its install name, spelled exactly as
// install's --host takes it.
func ByInstallName(name string) (Host, bool) {
	for _, h := range registry {
		if h.Names.InstallName == name {
			return h, true
		}
	}
	return Host{}, false
}
