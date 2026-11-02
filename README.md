# eva-md

eva-md is the platform for eva.md ("Eva dot M D"), a Markdown, git-based memory for Claude Code and Codex. It captures your chats into a redacted local inbox and gives every new chat a short resume brief. Ask "where did we stop?" in a new chat, in either tool, and it can answer.

Coding assistants forget everything between chats, and after a chat is compacted. eva.md keeps that memory as plain files you own: a local inbox for recent chats and a private wiki repository in git. Nothing needs a local model, a resident service or a cloud account.

## How it works

- **Hooks.** eva-md installs native plugins for Claude Code and Codex. Their hooks run a short-lived `eva-md hook` process on chat events. A hook never blocks the host: on any failure it exits successfully, and when its config can't be read it says so in one line at a chat start.
- **Redacted inbox.** Each prompt and reply, and one line for each file change, command, read, search, web fetch and MCP call naming what it acted on but never its output, is redacted, then appended to a local inbox. When a hook misses something, eva-md recovers it from the host's transcript, within fixed limits.
- **Resume briefs.** When a chat starts, is cleared or is compacted, eva-md builds a brief from the inbox for the current project and branch. The brief is deterministic: no model writes it.
- **Wiki updates.** After a chat, a detached job runs the chat's own host CLI headless to update the branch's thread page and the project's standing rules, and to save lasting facts, decisions, procedures and constraints as knowledge pages. Code checks every line before it is written.
- **Recall.** A prompt that strongly matches knowledge pages gets those facts, at most 3, fenced as reference notes; a weak match gets nothing. A new chat's brief lists the project's constraints. Both read one local index file, with no model. Once a day the wiki is committed with your signing and pushed. A Claude chat a program runs, such as `claude -p` or the Agent SDK (told apart by the `CLAUDE_CODE_ENTRYPOINT` it inherits), is captured and searchable, but it gets no update of its own and is never the brief's previous chat. Codex `codex exec` runs can't be told apart.
- **Private wiki.** The long-term memory is a separate private git repository of Markdown pages, which `eva-md setup` creates in a folder you choose. This repo holds only the platform.
- **okf.** Wiki pages use the format of [okf](https://github.com/okf-memory/okf-agent-memory), pinned at v0.7.2 and used as a library behind an adapter.
- **MCP tools.** Both plugins also declare a read-only MCP server, the same binary, which each host starts once a chat, so the agent can call `eva_search`, `eva_show` and `eva_status`: the search and show commands, and a status of the project's memory, its thread page, pending update, systems, knowledge pages and hook health. A recall skill in both plugins tells the agent when to look in memory and how.

## Status

Built: capture, resume and wiki updates. That covers the hooks, the redacted inbox, transcript recovery, resume briefs at startup and after `/clear` or compaction, recall across Claude Code and Codex, `eva-md search` over captured chats and the wiki's thread, system and knowledge pages, the background update that keeps each branch's thread page, the standing rules and the knowledge pages, recall of matching knowledge on prompts and the project's constraints in the brief, a recall skill in both plugins, system pages, which the agent writes with `eva-md system write`, and the pointers to them, how far the code each system page cites has moved since it was written (`eva-md system status`, also shown in the pointers and the doctor), the daily signed commit and push, health checks with `eva-md doctor --live`, install through each host's native commands, and guided setup, also for a wiki on a self-hosted git server such as GitLab.

Once installed, the hooks start `eva-md update` in the background when a chat ends or is compacted, when a chat's unprocessed text passes 60 KB, at a prompt after a 30-minute gap, and at a chat start for the project's idle chats; it runs the chat's own host CLI headless, which uses your subscription. The first hook event of each day starts `eva-md maintain`, which commits the day's wiki changes with your signing and pushes them. Chats captured before the update first ran, as on an upgrade from a capture-only release, are left as they are: the update starts from the install. In `~/.config/eva-md/config.toml`, `updates = false` turns the updates off, `commits = false` the daily commit and push, and `recall = false` the knowledge a prompt gets, while search, show and the brief's constraints stay on. If the config can't be read after an edit, as with a TOML mistake or a value it refuses, eva-md captures nothing and each new chat says so in one line: "eva.md: the config can't be read, so this chat isn't captured; run eva-md doctor." Unknown keys are reported, not refused.

The daily maintenance also keeps a search index of the inbox, even on a Mac without a wiki, so `eva-md search` stays fast over years of history. Running `eva-md index` builds it at once, which is optional: until the index exists, search scans the inbox as before.
Both plugins declare `eva-md mcp`, a read-only MCP server that the hosts start once a chat; `mcp = false` in the config turns its tools off while the hooks and the command line stay on. It is checked offline only, like the rest.

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
2. Create an empty private repository for your wiki, on GitHub, GitLab or any git host. If gh or glab is installed and signed in, setup can create it for you, and it refuses a public repo when it can tell. Setup offers to create it with glab only on glab's default host, which is gitlab.com unless `GITLAB_HOST` or glab's config names another. For a self-hosted GitLab, run setup as `GITLAB_HOST=<your GitLab host> bin/eva-md setup`, or set glab's host first with `glab config set -g host <your GitLab host>`.
3. Build eva-md and run the guided setup in a terminal:

```sh
go build -mod=readonly -o bin/eva-md ./cmd/eva-md
bin/eva-md setup
```

Setup checks for git, Claude Code, Codex and your commit signing. It looks before it asks: it offers to use an eva wiki already in the folder or on the repo, warns when another Mac set that wiki up, and on a Mac already set up offers to clone a missing wiki folder again. On a new Mac it asks two questions, with no default answers:

1. Where the wiki should live on this Mac. Choose a new or empty folder of its own, not in Documents, Desktop or Downloads, and not in iCloud Drive or other cloud storage such as OneDrive, Box, Google Drive or Dropbox (`~/Library/CloudStorage`), which syncs into `.git`.
2. Which private repository it is pushed to, such as `git@github.com:you/eva-wiki.git`.

It shows a summary and asks you to confirm. Then it creates the wiki, installs the plugins and runs the health check. Your git setup may ask you to approve signing the first commit and the push, for example in your password manager or SSH agent. Codex asks you to approve eva-md's hooks: type `/hooks` in Codex and approve them. Then start a new chat. Chats that were already open are not restarted.

Before it pushes a wiki branch the remote has never had, such as a new wiki's first commit, setup shows the commit's author and whether it is signed, as git made it in the wiki folder. Per-folder git settings such as `includeIf` can make these differ from the Commit signing line, which comes from your home folder. In a terminal it asks "Push it to <remote>? [Y/n]", with `--yes` too. A no keeps the commit and pushes nothing. Push it later with `~/.local/share/eva-md/bin/eva-md commit --push-only`, or, to change its author, fix git's settings for the wiki folder and run `git commit --amend --reset-author` in it; the daily commit pushes it along with the next changes. Without a terminal, setup shows the line and pushes. Setup's closing lines name the installed binary by its full path, since eva-md isn't on PATH.

After setup, run `~/.local/share/eva-md/bin/eva-md doctor --live`. It runs the headless update command once for each installed host, a real model call that writes nothing, and is the only check that updates work with this Mac's account, proxy and settings.

If Codex has MCP servers of its own in `~/.codex/config.toml`, such as one for a wiki or an issue tracker, add `codex_ignore_user_config = true` to `~/.config/eva-md/config.toml`. Otherwise every Codex update run starts them, and a stray tool call could send them text from your chats. The flag makes update runs skip the rest of that file too, so a custom model provider set there may not apply (not yet checked on a real machine); leave it off if Codex needs those settings, and run `~/.local/share/eva-md/bin/eva-md doctor --live` after changing it. Auth still comes from Codex's own folder. Claude update runs start none of your MCP servers.

An upgrade that changes the plugin folders, as the ones that add the MCP server and the recall skill do, re-renders both of them once, so Codex asks you to approve eva-md's hooks in `/hooks` again.

## Commands

- `eva-md setup [--dir FOLDER] [--remote URL] [--host claude|codex|all] [--no-push] [--yes]`: the guided setup above. Without a terminal, a first run needs `--dir` and `--remote`. `--no-push` makes the wiki's first commit without pushing it. `--yes` skips the confirmation. On a Mac that is already set up, it keeps the wiki and installs or updates the plugins, and offers to clone the wiki folder again if it is missing or empty.
- `eva-md version`: the binary, schema and okf versions. The binary's version is the date of the commit it was built from, in UTC, then that commit's time and short hash, such as `2026.11.2+0915.9e4f609`, so two builds of one day sort by their time; `.dirty` marks a build of a tree with uncommitted changes, and `dev` a build Go recorded no commit for. No step sets it by hand.
- `eva-md init --remote URL --dir FOLDER [--no-push]`: only the wiki step of setup. Both flags are required. It clones an empty remote or adopts an existing eva wiki, validates it, makes the first commit with your normal signing settings, pushes, and saves the local config. `--no-push` still makes the local commit.
- `eva-md validate [--dir FOLDER]`: print validation counts for a wiki. It exits with an error for errors, broken links, orphan pages, or actor and parent-index warnings. Without `--dir` it checks the configured wiki.
- `eva-md install [--host claude|codex|all] [--dry-run]`: install the plugins through each host's native commands, rolling back on failure.
- `eva-md uninstall [--apply]`: preview the removal of eva-md's plugins and installed files, or remove them with `--apply`. The wiki, config and inbox are kept.
- `eva-md brief [--cwd DIR] [--as startup|clear|compact|resume|fork] [--host claude-code|codex] [--session ID]`: preview the brief a new chat would get. `compact`, `resume` and `fork` need `--session`.
- `eva-md show s:<session>#<line>` or `eva-md show page[#section]`: show a captured source line, a wiki page or one section of it. `#Timeline` shows the newest 10 date groups (`--last N` or `--since YYYY-MM-DD` choose others) and `#YYYY-MM-DD` one day's group. For a system page it also prints on stderr how far the code the page cites has moved since its `as_of` day, measured in a local clone, whether the page is past its review day, and which system pages link to it; stdout stays the page. `eva-md show knowledge/map` lists the knowledge pages, and `knowledge/k<N>` shows one.
- `eva-md search TERMS... [--all] [--days N] [--limit N] [--cwd DIR] [--pages | --inbox]`: find earlier chat text in the inbox and lines of the wiki's thread, system and knowledge pages (`--pages` or `--inbox` searches only one). The agent runs it when it needs an earlier chat; no hook does. Each argument is a term, and a quoted argument with spaces is a phrase. It searches the current project and the general area, or every project with `--all`, over the last 90 days (`--days 0` for no limit; system and knowledge pages are always searched), and prints the 10 best hits (`--limit`, at most 50), each with a ref for `eva-md show`. A system page is found by its title, aliases and description too, and a path finds the system and knowledge pages that cite that file or a folder holding it, in this project's repo (any repo with `--all`). Nothing said off the record is shown. It stops after 5 seconds and says so. It reads a search index of the inbox where there is one and scans whatever the index lacks, and among equally good hits it puts lines with rarer words, used more densely, before newer ones.
- `eva-md index [--rebuild] [--json]`: bring the search index up to date with the inbox now, as the daily maintenance does, or with `--rebuild` build it again from nothing. It is optional: without it search scans the inbox, as it does before the first daily maintenance. It prints a line per project and a total, counts only, or with `--json` what the index covers.
- `eva-md update [--session host:id ... | --pending] [--dry-run] [--foreground]`: run the background update for the sessions named (`claude-code:<id>` or `codex:<id>`, `--session` once or more) or for every session with a backlog, under one lock and the daily cap of model calls, and print one summary line. `--foreground` runs attached with progress on stderr; without it the lines go to the day's log. `--dry-run` takes one session, prints the exact command, the names of the environment variables, the rules, the input and the schema, and makes no call. The hooks spawn it after chats; run it by hand to catch up now.
- `eva-md commit [--push-only]`: the daily commit and push now, with the guards the daily job uses. `--push-only` pushes commits made earlier. It prints one line, such as `committed (3 pages, 1 left out), push ok`, and exits 1 when the commit was refused or failed, or a `--push-only` push that ran didn't end `ok` or `up_to_date`; a run that finds another one running exits 0.
- `eva-md maintain [--push-only]`: the daily job the hooks start on the day's first event: the knowledge map and indexes built again when they are out of date, the commit and push, unless `commits = false`, then a lint of the wiki, the code status of the system pages and the search index. Without a wiki it keeps only the search index. With `--push-only`, which a chat start runs while a push is pending, it only pushes.
- `eva-md system write <slug> [--replace] < page.md`: write a system page from stdin, with the system map. It refuses a page with problems and names each by line. A page cites code as `<repo>@<commit>:<folder or file>`, at most 16 of them; the write checks those paths in a local clone of the repo, refuses one the cited commit lacks, and says which clone it checked, or why it couldn't. The digest skill in both plugins uses it: connect your own source of documentation to the agent, ask it to digest a space or a repo, and it writes one page per system, citing its sources. Read the map with `eva-md show systems/map`; the daily commit pushes the pages with the rest of the wiki.
- `eva-md system status [SLUG...] [--json]`: how far the code each system page cites has moved since its page was written, measured in the local clones eva-md knows from your chats in them, with the pages that couldn't be checked and those past their review day. It saves what it found for the prompt pointers and the doctor; the daily job runs it too.
- `eva-md doctor [--json] [--live]`: local health, the native plugin inventory, a read-only check of Codex hook trust, and the state of updates, the daily commit, the push, the system pages' code, the knowledge pages and the search index. It has two repairs: it writes the system names index again, and builds the knowledge map, folder index and recall index again from the pages, when they are missing or out of date. If you remove the `knowledge/` folder, the repair empties the recall index, so nothing recalls a removed page. Neither repair writes a wiki a newer eva-md set up, and neither does maintain. `--live` runs the exact headless update command once for each installed host on a small fixture, a real model call that writes nothing.
- `eva-md status [--json]`: local health without asking the hosts, plus the wiki's contents (projects, threads and knowledge pages) and the update backlog. It never writes anything.
- `eva-md hook --host claude-code|codex`: the hook handler the plugins call. It is not meant to be run by hand.
- `eva-md mcp --host claude-code|codex`: the read-only MCP server the plugins start, which serves `eva_search`, `eva_show` and `eva_status` until the host closes it. It is not meant to be run by hand.

Unknown commands print "unknown command" and exit with status 2.

The plugins call a stable copy of the binary at `~/.local/share/eva-md/bin/eva-md` by its absolute path. Installing does not change your PATH or shell files.

## Privacy

- **Redaction before disk.** Every prompt, reply and recovered transcript line is redacted before it is written. Redaction removes the union of what two rule sets catch: a frozen set of baseline rules and the current rules. So a change to the current rules can never let through a secret the baseline rules catch. Git SHAs and UUIDs are kept in ordinary text, but not as the value of a secret-looking key such as `password=`. `op://` references are kept.
- **Off the record.** Start a prompt with "off the record", or put it on a line of its own, to pause capture for that chat. Say "back on the record" to resume. Notes already captured stay saved.
- **Exclusions.** In `~/.config/eva-md/config.toml`, `exclude = ["~/work/private", "remote:github.com/your-org/*"]` excludes folders (path globs, where `~/` is your home folder and an entry covers everything below it) and git remotes (`remote:` globs, against the remote as eva-md normalizes it). [docs/design.md](docs/design.md) lists every config key. Chats in an excluded project are not captured and get no brief. Hooks also skip temporary folders, the wiki folder and eva-md's own state folder.
- **Local-first.** The inbox and config are stored only on your Mac. The background update sends each chat's new, redacted entries, up to 60 KB per call, to that chat's own host CLI, and so to the model provider the chat already uses; `eva-md doctor --live` sends a built-in fixture. Only the wiki is pushed: its first commit by `setup` or `init`, then one commit a day, to the private repository you chose. With gh signed in, or glab signed in to the repo's host, setup also asks it about the repo; for a repo on any other host, or without them, it tries to read the typed repo over HTTPS with no sign-in, and when that works it asks before using it (no is the default) and refuses it without a terminal.
- **Local clone paths stay local.** `eva-md show` and `eva-md system status` print the folder of the clone they measured a system page's code in, and the code status in eva-md's state folder keeps it. Nothing of it goes into the wiki.
- **Search index.** The search index holds lowercase words and line positions of the redacted, on-record inbox, never its text and never anything said off the record. It lives in eva-md's state folder with the inbox's permissions, is never committed or pushed, and can be deleted at any time; search then scans the inbox until the next daily maintenance builds it again. Removing a session file from the inbox takes its words out of the index at the next `eva-md index` or daily maintenance, and the file that held them goes at the first such run 10 minutes or more after.
- **Knowledge pages and recall.** Knowledge pages live in the wiki and are committed and pushed with it; each statement cites the chat entries it came from, and a change the daily commit's secret scan would flag in any line it writes is never saved. A page you lock (`locked: true`) or mark `verified`, in any form, is never rewritten. Recall on a prompt reads one local index file in the wiki, makes no network call and runs no model. What it adds is fenced and labelled as reference notes, it never shows a line the secret scan flags, and it sends nothing off the record or in a chat a program runs.
- **No network calls in hooks.** Hooks read and write local files and run local git commands to identify the project. They never contact a server. The jobs they start detached do: the update runs the host's CLI, and the daily job pushes the wiki.
- **MCP tools.** The MCP server only reads: it writes no file, makes no network call, and caps what it returns at 24 KB for a search or a page, 25 KB for a whole system page, and 4 KB for a status. Claude Code may ask before each tool's first use; to skip that, add `mcp__plugin_eva-md_eva-md__*` to `permissions.allow` in your own Claude settings. eva-md never writes host settings.

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
