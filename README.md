# eva-md

eva-md is the platform for eva.md ("Eva dot M D"), a Markdown, git-based memory for Claude Code and Codex. It captures your chats into a redacted local inbox and gives every new chat a short resume brief. Ask "where did we stop?" in a new chat, in either tool, and it can answer.

Coding assistants forget everything between chats, and after a chat is compacted. eva.md keeps that memory as plain files you own: a local inbox for recent chats and a private wiki repository in git. Nothing needs a local model, a resident service or a cloud account.

## How it works

- **Hooks.** eva-md installs native plugins for Claude Code and Codex. Their hooks run a short-lived `eva-md hook` process on chat events. A hook never blocks the host: on any failure it exits successfully and does nothing.
- **Redacted inbox.** Each prompt and reply, and one line for each file change, command, read, search, web fetch and MCP call naming what it acted on but never its output, is redacted, then appended to a local inbox. When a hook misses something, eva-md recovers it from the host's transcript, within fixed limits.
- **Resume briefs.** When a chat starts, is cleared or is compacted, eva-md builds a brief from the inbox for the current project and branch. The brief is deterministic: no model writes it.
- **Wiki updates.** After a chat, a detached job runs the chat's own host CLI headless to update the branch's thread page and the project's standing rules. Code checks every line before it is written. Once a day the wiki is committed with your signing and pushed.
- **Private wiki.** The long-term memory is a separate private git repository of Markdown pages, which `eva-md setup` creates in a folder you choose. This repo holds only the platform.
- **okf.** Wiki pages use the format of [okf](https://github.com/okf-memory/okf-agent-memory), pinned at v0.7.2 and used as a library behind an adapter.

## Status

Built: capture, resume and wiki updates. That covers the hooks, the redacted inbox, transcript recovery, resume briefs at startup and after `/clear` or compaction, recall across Claude Code and Codex, `eva-md search` over captured chats and the wiki's thread and system pages, the background update that keeps each branch's thread page and the standing rules, system pages, which the agent writes with `eva-md system write`, and the pointers to them, the daily signed commit and push, health checks with `eva-md doctor --live`, install through each host's native commands, and guided setup, also for a wiki on a self-hosted git server such as GitLab.

Once installed, the hooks start `eva-md update` in the background when a chat ends or is compacted, when a chat's unprocessed text passes 60 KB, at a prompt after a 30-minute gap, and at a chat start for the project's idle chats; it runs the chat's own host CLI headless, which uses your subscription. The first hook event of each day starts `eva-md maintain`, which commits the day's wiki changes with your signing and pushes them. Chats captured before the update first ran, as on an upgrade from a capture-only release, are left as they are: the update starts from the install. In `~/.config/eva-md/config.toml`, `updates = false` turns the updates off and `commits = false` the daily commit and push.

Not verified live yet: no live update has run, the new parts have not been checked in the real host apps, and no wiki on a self-hosted GitLab has been run end to end. Everything is checked offline; see [docs/verification.md](docs/verification.md).

See [docs/known-limitations.md](docs/known-limitations.md) for what does not work yet.

## Requirements

- macOS. Setup, paths and the folder checks assume a Mac.
- Go 1.26 or newer, to build.
- git.
- Claude Code, Codex, or both.
- An empty private repository for the wiki, on GitHub, GitLab or any git host.

## Set up

1. Install Claude Code or Codex (or both), and git.
2. Create an empty private repository for your wiki, on GitHub, GitLab or any git host. If gh or glab is installed and signed in, setup can create it for you, and it refuses a public repo when it can tell.
3. Build eva-md and run the guided setup in a terminal:

```sh
go build -mod=readonly -o bin/eva-md ./cmd/eva-md
bin/eva-md setup
```

Setup checks for git, Claude Code, Codex and your commit signing. It looks before it asks: it offers to use an eva wiki already in the folder or on the repo, warns when another Mac set that wiki up, and on a Mac already set up offers to clone a missing wiki folder again. On a new Mac it asks two questions, with no default answers:

1. Where the wiki should live on this Mac. Choose a new or empty folder of its own, not in Documents, Desktop or iCloud Drive.
2. Which private repository it is pushed to, such as `git@github.com:you/eva-wiki.git`.

It shows a summary and asks you to confirm. Then it creates the wiki, installs the plugins and runs the health check. Your git setup may ask you to approve signing the first commit and the push, for example in your password manager or SSH agent. Codex asks you to approve eva-md's hooks: type `/hooks` in Codex and approve them. Then start a new chat. Chats that were already open are not restarted.

## Commands

- `eva-md setup [--dir FOLDER] [--remote URL] [--host claude|codex|all] [--no-push] [--yes]`: the guided setup above. Without a terminal, a first run needs `--dir` and `--remote`. `--no-push` makes the wiki's first commit without pushing it. `--yes` skips the confirmation. On a Mac that is already set up, it keeps the wiki and installs or updates the plugins, and offers to clone the wiki folder again if it is missing or empty.
- `eva-md version`: the binary, schema and okf versions.
- `eva-md init --remote URL --dir FOLDER [--no-push]`: only the wiki step of setup. Both flags are required. It clones an empty remote or adopts an existing eva wiki, validates it, makes the first commit with your normal signing settings, pushes, and saves the local config. `--no-push` still makes the local commit.
- `eva-md validate [--dir FOLDER]`: print validation counts for a wiki. It exits with an error for errors, broken links, orphan pages, or actor and parent-index warnings. Without `--dir` it checks the configured wiki.
- `eva-md install [--host claude|codex|all] [--dry-run]`: install the plugins through each host's native commands, rolling back on failure.
- `eva-md uninstall [--apply]`: preview the removal of eva-md's plugins and installed files, or remove them with `--apply`. The wiki, config and inbox are kept.
- `eva-md brief [--cwd DIR] [--as startup|clear|compact|resume|fork] [--host claude-code|codex] [--session ID]`: preview the brief a new chat would get. `compact`, `resume` and `fork` need `--session`.
- `eva-md show s:<session>#<line>` or `eva-md show page[#section]`: show a captured source line, a wiki page or one section of it. `#Timeline` shows the newest 10 date groups (`--last N` or `--since YYYY-MM-DD` choose others) and `#YYYY-MM-DD` one day's group.
- `eva-md search TERMS... [--all] [--days N] [--limit N] [--cwd DIR] [--pages | --inbox]`: find earlier chat text in the inbox and lines of the wiki's thread and system pages (`--pages` or `--inbox` searches only one). The agent runs it when it needs an earlier chat; no hook does. Each argument is a term, and a quoted argument with spaces is a phrase. It searches the current project and the general area, or every project with `--all`, over the last 90 days (`--days 0` for no limit; system pages are always searched), and prints the 10 best hits (`--limit`, at most 50), each with a ref for `eva-md show`. Nothing said off the record is shown. It stops after 5 seconds and says so.
- `eva-md update [--session host:id ... | --pending] [--dry-run] [--foreground]`: run the background update for the sessions named (`claude-code:<id>` or `codex:<id>`, `--session` once or more) or for every session with a backlog, under one lock and the daily cap of model calls, and print one summary line. `--foreground` runs attached with progress on stderr; without it the lines go to the day's log. `--dry-run` takes one session, prints the exact command, the names of the environment variables, the rules, the input and the schema, and makes no call. The hooks spawn it after chats; run it by hand to catch up now.
- `eva-md commit [--push-only]`: the daily commit and push now, with the guards the daily job uses. `--push-only` pushes commits made earlier. It prints one line, such as `committed (3 pages, 1 left out), push ok`, and exits 1 when the commit was refused or failed, or a `--push-only` push that ran didn't end `ok` or `up_to_date`; a run that finds another one running exits 0.
- `eva-md maintain [--push-only]`: the daily job the hooks start on the day's first event: the commit and push, unless `commits = false`, then a lint of the wiki. With `--push-only`, which a chat start runs while a push is pending, it only pushes.
- `eva-md system write <slug> [--replace] < page.md`: write a system page from stdin, with the system map. It refuses a page with problems and names each by line. The digest skill in both plugins uses it: connect your own source of documentation to the agent, ask it to digest a space or a repo, and it writes one page per system, citing its sources. Read the map with `eva-md show systems/map`; the daily commit pushes the pages with the rest of the wiki.
- `eva-md doctor [--json] [--live]`: local health, the native plugin inventory, a read-only check of Codex hook trust, and the state of updates, the daily commit and the push. It writes the system names index again when it is missing or out of date, its one repair. `--live` runs the exact headless update command once for each installed host on a small fixture, a real model call that writes nothing.
- `eva-md status [--json]`: local health without asking the hosts, plus the wiki's contents and the update backlog. It never writes anything.
- `eva-md hook --host claude-code|codex`: the hook handler the plugins call. It is not meant to be run by hand.

Unknown commands print "unknown command" and exit with status 2.

The plugins call a stable copy of the binary at `~/.local/share/eva-md/bin/eva-md` by its absolute path. Installing does not change your PATH or shell files.

## Privacy

- **Redaction before disk.** Every prompt, reply and recovered transcript line is redacted before it is written. Redaction removes the union of what two rule sets catch: a frozen set of baseline rules and the current rules. So a change to the current rules can never let through a secret the baseline rules catch. Git SHAs and UUIDs are kept in ordinary text, but not as the value of a secret-looking key such as `password=`. `op://` references are kept.
- **Off the record.** Start a prompt with "off the record", or put it on a line of its own, to pause capture for that chat. Say "back on the record" to resume. Notes already captured stay saved.
- **Exclusions.** The config can exclude folders or git remotes. Chats in an excluded project are not captured and get no brief. Hooks also skip temporary folders, the wiki folder and eva-md's own state folder.
- **Local-first.** The inbox and config are stored only on your Mac. The background update sends each chat's new, redacted entries, up to 60 KB per call, to that chat's own host CLI, and so to the model provider the chat already uses; `eva-md doctor --live` sends a built-in fixture. Only the wiki is pushed: its first commit by `setup` or `init`, then one commit a day, to the private repository you chose. With gh or glab signed in, setup also asks it about a repo on its host; for a repo on any other host, or without them, it tries to read the typed repo over HTTPS with no sign-in, and when that works it asks before using it (no is the default) and refuses it without a terminal.
- **No network calls in hooks.** Hooks read and write local files and run local git commands to identify the project. They never contact a server. The jobs they start detached do: the update runs the host's CLI, and the daily job pushes the wiki.

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
