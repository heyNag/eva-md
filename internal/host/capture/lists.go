package capture

import "github.com/heyNag/eva-md/internal/ledger/event"

// The one list: every source and prefix of v1's lists, and Claude Code's
// entrypoints, each with the author it gives a prompt, the host fact
// behind it and how far that fact is settled. v1 keeps three lists of
// prefixes that disagree (the hook's synthetic, recovery's syntheticText
// and hostWrapper); here each prefix is once, and a test holds every value
// of those lists to an entry here or to builtinCommands.

// State is how far the fact behind an entry is settled.
type State string

const (
	// FromSource: read in the host's published source.
	FromSource State = "from source"
	// FromV1: taken from v1's lists; no sitting has recorded it yet.
	FromV1 State = "v1's list"
	// Waiting: the rule waits for a sitting's recording.
	Waiting State = "waiting for the sitting"
)

// Entry is one value on the list.
type Entry struct {
	// Value is matched as it is written here: a source and a prefix in
	// lower case, against the payload's lower-cased; an entrypoint exactly.
	Value  string
	Author event.Author
	// Fact is the host fact of docs/v2/host-facts.md behind the entry.
	Fact  string
	State State
	// From is the v1 file whose list holds it, from the repository's root,
	// or "" for an entry that is in none.
	From string
}

// v1's files whose lists the entries come from.
const (
	hookGo    = "internal/hook/hook.go"
	parserGo  = "internal/catchup/parser.go"
	auditGo   = "internal/ledger/audit/host.go"
	factWrite = "F-016" // a prompt the host writes is marked as the host's
	factEntry = "F-079" // the entrypoint tells a person's chat from a program's
)

// sources are the values of a prompt's source field that say who wrote it.
// Neither host's documents give the field today; v1 lists them in case one
// does.
var sources = []Entry{
	{"scheduled", event.AuthorHost, factWrite, FromV1, hookGo},
	{"scheduled_task", event.AuthorHost, factWrite, FromV1, hookGo},
	{"scheduler", event.AuthorHost, factWrite, FromV1, hookGo},
	{"cron", event.AuthorHost, factWrite, FromV1, hookGo},
	{"loop", event.AuthorHost, factWrite, FromV1, hookGo},
	{"subagent", event.AuthorSubagent, factWrite, FromV1, hookGo},
	{"agent", event.AuthorAgent, factWrite, FromV1, hookGo},
	{"background", event.AuthorHost, factWrite, FromV1, hookGo},
	{"other_session", event.AuthorProgram, factWrite, FromV1, hookGo},
	{"session_message", event.AuthorProgram, factWrite, FromV1, hookGo},
	{"system", event.AuthorHost, factWrite, FromV1, hookGo},
	{"automation", event.AuthorProgram, factWrite, FromV1, hookGo},
}

// prefixes are how a prompt the host or a program wrote begins, after
// white space, in lower case. Messages from another session and teammate
// messages are a program's (ledger 3.6); the rest the host's.
var prefixes = []Entry{
	{"<task-notification>", event.AuthorHost, factWrite, FromV1, hookGo},
	{"<teammate-message", event.AuthorProgram, factWrite, FromV1, hookGo},
	{"<system-reminder>", event.AuthorHost, factWrite, FromV1, hookGo},
	{"<local-command-stdout>", event.AuthorHost, factWrite, FromV1, hookGo},
	{"<command-name>", event.AuthorHost, factWrite, FromV1, hookGo},
	{"[message from ", event.AuthorProgram, factWrite, FromV1, hookGo},
	{"## memory writing agent", event.AuthorHost, factWrite, FromV1, hookGo},
	{"/loop ", event.AuthorHost, factWrite, FromV1, hookGo},
	{"# agents.md instructions", event.AuthorHost, factWrite, FromV1, parserGo},
	{"<environment_context>", event.AuthorHost, factWrite, FromV1, parserGo},
	{"<permissions instructions>", event.AuthorHost, factWrite, FromV1, parserGo},
	{"/clear", event.AuthorHost, factWrite, FromV1, parserGo},
	{"/compact", event.AuthorHost, factWrite, FromV1, parserGo},
	{"<bash-input>", event.AuthorHost, factWrite, FromV1, parserGo},
	{"<bash-stdout>", event.AuthorHost, factWrite, FromV1, parserGo},
	{"<bash-stderr>", event.AuthorHost, factWrite, FromV1, parserGo},
	{"<command-message>", event.AuthorHost, factWrite, FromV1, parserGo},
	{"<command-args>", event.AuthorHost, factWrite, FromV1, parserGo},
	{"<local-command-stderr>", event.AuthorHost, factWrite, FromV1, parserGo},
	{"[request interrupted by user", event.AuthorHost, factWrite, FromV1, parserGo},
}

// entrypoints are Claude Code's values of CLAUDE_CODE_ENTRYPOINT that say
// who runs a chat: a program (print mode and the Agent SDKs) or a person
// (the terminal, the desktop app and the VS Code extension). Any other
// value, claude-desktop-3p and local-agent among them, gives no author
// until a recording shows whose it is.
var entrypoints = []Entry{
	{"sdk-cli", event.AuthorProgram, factEntry, FromV1, hookGo},
	{"sdk-ts", event.AuthorProgram, factEntry, FromV1, hookGo},
	{"sdk-py", event.AuthorProgram, factEntry, FromV1, hookGo},
	{"cli", event.AuthorTyped, factEntry, FromV1, "internal/hook/headless_test.go"},
	{"claude-desktop", event.AuthorTyped, factEntry, FromV1, "internal/hook/headless_test.go"},
	{"claude-vscode", event.AuthorTyped, factEntry, FromV1, "internal/hook/headless_test.go"},
}

// builtinCommands are v1's: a prompt whose first word is one of them, in
// any letter case, is the host's own command and yields no event.
var builtinCommands = []string{"/clear", "/compact", "/resume", "/fork", "/model", "/help", "/exit", "/quit", "/memory", "/memories", "/hooks", "/status", "/config", "/permissions", "/cost", "/context", "/login", "/logout"}
