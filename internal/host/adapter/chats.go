package adapter

// Chats is where a host keeps its chats' transcripts unless it is told to
// keep them elsewhere: the folder transcript recovery reads and the doctor's
// capture freshness check scans.
type Chats struct {
	// Folder is the folder of the host's chats, relative to the home folder,
	// with slashes. A person reads it as ~/ followed by it.
	Folder string
	// MovedBy is the environment variable that moves the host's folder, and
	// its chats with it, elsewhere.
	MovedBy string
	// Layout is how the folder holds the chats.
	Layout Layout
}

// Layout is how a host's chat folder holds its chats. The zero Layout is a
// host eva-md doesn't know.
type Layout int

const (
	// PerProject is a folder per project, each holding the project's chats
	// as .jsonl files.
	PerProject Layout = iota + 1
	// PerDay is a folder per day, <YYYY>/<MM>/<DD>, each holding the chats
	// started that day as .jsonl files.
	PerDay
)

// chats is each host's Chats, by its id.
var chats = map[ID]Chats{
	ClaudeCode: {Folder: ".claude/projects", MovedBy: "CLAUDE_CONFIG_DIR", Layout: PerProject},
	Codex:      {Folder: ".codex/sessions", MovedBy: "CODEX_HOME", Layout: PerDay},
}

// Chats gives where the host keeps its chats; the zero Chats for a host
// eva-md doesn't know.
func (h Host) Chats() Chats {
	return chats[h.ID]
}

// logins is the command that logs each host's CLI in, by its id.
var logins = map[ID]string{
	ClaudeCode: "claude auth login",
	Codex:      "codex login",
}

// Login is the command, as a person types it, that logs the host's CLI in to
// the account its headless runs use; "" for a host eva-md doesn't know.
func (h Host) Login() string {
	return logins[h.ID]
}
