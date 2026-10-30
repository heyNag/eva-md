# eva-md

eva-md is the platform for eva.md ("Eva dot M D"), a Markdown, git-based memory for Claude Code and Codex. It captures your chats into a redacted local inbox and gives every new chat a short resume brief. Ask "where did we stop?" in a new chat, in either tool, and it can answer.

Coding assistants forget everything between chats, and after a chat is compacted. eva.md keeps that memory as plain files you own: a local inbox for recent chats and a private wiki repository in git. Nothing needs a local model, a resident service or a cloud account.

## How it works

- **Hooks.** eva-md installs native plugins for Claude Code and Codex. Their hooks run a short-lived `eva-md hook` process on chat events. A hook never blocks the host: on any failure it exits successfully and does nothing.
- **Redacted inbox.** Each prompt and reply is redacted, then appended to a local inbox. When a hook misses something, eva-md recovers it from the host's transcript, within fixed limits.
- **Resume briefs.** When a chat starts, is cleared or is compacted, eva-md builds a brief from the inbox for the current project and branch. The brief is deterministic: no model writes it.
- **Private wiki.** The long-term memory is a separate private git repository of Markdown pages, which `eva-md setup` creates in a folder you choose. This repo holds only the platform.
- **okf.** Wiki pages use the format of [okf](https://github.com/okf-memory/okf-agent-memory), pinned at v0.7.2 and used as a library behind an adapter.

## Status

Works today: capture and resume. That covers the hooks, the redacted inbox, transcript recovery, resume briefs at startup and after `/clear` or compaction, recall across Claude Code and Codex, health checks, install through each host's native commands, and guided setup.

Planned, not built yet:

- Wiki updates: summarizing chats into wiki pages, a daily signed commit and push of the wiki, and `eva-md doctor --live`. Until then, capture and resume never change the wiki, and `eva-md doctor` reports background updates and daily commits as UNKNOWN.
- Support for work machines whose wiki lives on a self-hosted git server.

See [docs/known-limitations.md](docs/known-limitations.md) for what does not work yet.

## Requirements

- macOS. Setup, paths and the folder checks assume a Mac.
- Go 1.26 or newer, to build.
- git.
- Claude Code, Codex, or both.
- An empty private repository for the wiki, on GitHub, GitLab or any git host.

## Set up

1. Install Claude Code or Codex (or both), and git.
2. Create an empty private repository for your wiki, on GitHub, GitLab or any git host.
3. Build eva-md and run the guided setup in a terminal:

```sh
go build -mod=readonly -o bin/eva-md ./cmd/eva-md
bin/eva-md setup
```

Setup checks for git, Claude Code, Codex and your commit signing. It then asks two questions, with no default answers:

1. Where the wiki should live on this Mac. Choose a new or empty folder of its own, not in Documents, Desktop or iCloud Drive.
2. Which private repository it is pushed to, such as `git@github.com:you/eva-wiki.git`.

It shows a summary and asks you to confirm. Then it creates the wiki, installs the plugins and runs the health check. Your git setup may ask you to approve signing the first commit and the push, for example in 1Password. Codex asks you to approve eva-md's hooks: type `/hooks` in Codex and approve them. Then start a new chat. Chats that were already open are not restarted.

## Commands

- `eva-md setup [--dir FOLDER] [--remote URL] [--host claude|codex|all] [--no-push] [--yes]`: the guided setup above. Without a terminal, a first run needs `--dir` and `--remote`. `--no-push` makes the wiki's first commit without pushing it. `--yes` skips the confirmation. On a Mac that is already set up, it keeps the wiki and installs or updates the plugins.
- `eva-md version`: the binary, schema and okf versions.
- `eva-md init --remote URL --dir FOLDER [--no-push]`: only the wiki step of setup. Both flags are required. It clones an empty remote or adopts an existing eva wiki, validates it, makes the first commit with your normal signing settings, pushes, and saves the local config. `--no-push` still makes the local commit.
- `eva-md validate [--dir FOLDER]`: print validation counts for a wiki. It exits with an error for errors, broken links, orphan pages, or actor and parent-index warnings. Without `--dir` it checks the configured wiki.
- `eva-md install [--host claude|codex|all] [--dry-run]`: install the plugins through each host's native commands, rolling back on failure.
- `eva-md uninstall [--apply]`: preview the removal of eva-md's plugins and installed files, or remove them with `--apply`. The wiki, config and inbox are kept.
- `eva-md brief [--cwd DIR] [--as startup|clear|compact|resume|fork] [--host claude-code|codex] [--session ID]`: preview the brief a new chat would get. `compact`, `resume` and `fork` need `--session`.
- `eva-md show s:<session>#<line>` or `eva-md show page[#section]`: show a captured source line or a wiki page.
- `eva-md doctor [--json]`: local health, the native plugin inventory and a read-only check of Codex hook trust.
- `eva-md status [--json]`: local health without asking the hosts.
- `eva-md hook --host claude-code|codex`: the hook handler the plugins call. It is not meant to be run by hand.

Unknown commands print "unknown command" and exit with status 2.

The plugins call a stable copy of the binary at `~/.local/share/eva-md/bin/eva-md` by its absolute path. Installing does not change your PATH or shell files.

## Privacy

- **Redaction before disk.** Every prompt, reply and recovered transcript line is redacted before it is written. Redaction removes the union of what two rule sets catch: a frozen set of baseline rules and the current rules. So a change to the current rules can never let through a secret the baseline rules catch. Git SHAs and UUIDs are kept in ordinary text, but not as the value of a secret-looking key such as `password=`. `op://` references are kept.
- **Off the record.** Start a prompt with "off the record", or put it on a line of its own, to pause capture for that chat. Say "back on the record" to resume. Notes already captured stay saved.
- **Exclusions.** The config can exclude folders or git remotes. Chats in an excluded project are not captured and get no brief. Hooks also skip temporary folders, the wiki folder and eva-md's own state folder.
- **Local-first.** The inbox and config stay on your Mac. Only the wiki leaves it, when `setup` or `init` clones it and pushes the first commit to the private repository you chose.
- **No network calls in hooks.** Hooks read and write local files and run local git commands to identify the project. They never contact a server.

## Development

```sh
go build -mod=readonly -o bin/eva-md ./cmd/eva-md
gofmt -l cmd internal
go vet ./...
go test ./...
go test -race ./...
```

Tests use temporary home folders and git configs. They never touch your real setup. If Go's default cache is not writable, set `GOCACHE` to a writable temporary folder.

The latency benchmark runs the built binary against an isolated temporary home. Targets: capture p95 under 50 ms and startup brief p95 under 300 ms. Pass a git repository outside temporary folders as `--cwd`, such as this checkout:

```sh
python3 -I scripts/benchmark.py --binary bin/eva-md --cwd "$PWD"
```

Contributors and coding agents: read [AGENTS.md](AGENTS.md) first.

## Documentation

- [docs/design.md](docs/design.md): how eva-md works, and why.
- [docs/decisions.md](docs/decisions.md): the decisions the design rests on.
- [docs/verification.md](docs/verification.md): how the behaviour is checked, automatically and live.
- [docs/known-limitations.md](docs/known-limitations.md): what does not work yet.

## License

MIT. See [LICENSE](LICENSE).
