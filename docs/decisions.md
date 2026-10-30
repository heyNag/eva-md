# eva.md decisions

These are the decisions behind eva.md. They come from evaluating ten reference memory repos, a heavier memory system built on self-hosted Mem0 (called eva below), and the Claude Code and Codex documentation. [design.md](design.md) turns them into a design, and its "Changes to decisions" section lists where the design refines them. What is built and checked is in [verification.md](verification.md), and known gaps are in [known-limitations.md](known-limitations.md).

## Goal

Memory you can depend on while working in Claude Code and Codex. It saves what happens, keeps it organized and brings the right context back on its own. You never curate, review or trigger it. The test: when the context fills up or you open a new chat in the same project, asking "where did we stop?" continues as if it were the same chat.

## Why a lighter option

eva, the Mem0-based alternative, runs a local 12B model on every write, and its own measurements show the cost:

- Gemma 12B took about 11 GB of weights and 15.7 to 17.9 GiB of RAM. A write took 14 s warm and about 56 s cold, and a search took 28 s after the model went to sleep.
- A second model server for embeddings (2.2 GiB), two database VMs with relays, and about 197 pinned packages.
- Seven incidents in one week of testing.

Lessons that carry over:

- Model work on the write path was the root cause in all three engines tried (Hindsight, Cognee, Mem0).
- Word-for-word writes with no model stored 72 of 72 records.
- When the agent ran follow-up searches it found everything 10 times out of 10. A single automatic search was complete only 4 times out of 10.
- LLM extraction got speakers and dates wrong (an assistant suggestion stored as the user's decision; a weakened event date).

## Decisions

Some decisions cover parts that are not built yet, such as the background update and the daily commit (wiki updates). [design.md](design.md) marks those parts as planned.

1. **Platform separate from the wiki.**
   - eva.md is only the platform: tools, plugins and schema.
   - The memory lives in a separate private git repo of markdown that the platform is pointed at by a per-machine config.
   - For example, a personal machine uses a private GitHub repo and a work machine a private repo on a self-hosted GitLab.
   - The same code runs on every machine; personal and work memory never mix.
   - You create the empty repo; `init` adds the structure and the first commit.
   - The platform uses only the plain git command line, never GitHub or GitLab APIs.
2. **Zero-touch.** Capture, organizing and recall all happen automatically. Everything stays inspectable as plain markdown and git history.
3. **Hosts in scope:**
   - Claude Code: the desktop app's Code tab and the CLI.
   - Codex: the ChatGPT app's Codex mode and the CLI.
   - Not in v1: Claude Desktop's Chat tab, regular ChatGPT chat, and Claude Code cloud sessions.
   - The Codex VS Code extension has no plugin support and would need manual setup.
4. **Capture.**
   - Hooks save every exchange word for word to a local inbox on each machine, with who said it, when and which session. Secrets are redacted.
   - No model is involved, and the inbox is never committed.
   - It is kept with no time limit. Clearing it is a command that previews first.
5. **Organizing (the background update).**
   - After each chat, a detached job runs the same host and model that ran the chat in headless mode, with tools off.
   - It updates the project's Now page and promotes lasting facts into wiki pages.
   - It supersedes old facts and never deletes or rewrites a whole page.
   - Every page links to its source entries, and pages you lock are left alone.
   - Dates come from entry timestamps.
   - The bar for promotion: would it matter in six months, and can it not be worked out from the code?
6. **Recall.**
   - Each new chat starts with the project's Now page plus the last exchanges of the previous chat, size-capped and marked as data, not instructions.
   - If the background update hasn't finished yet, raw inbox entries fill the gap.
   - The Now page is re-added after compaction.
   - Strong matches on a prompt are added quietly; weak matches add nothing.
   - The agent searches further on its own when it needs to.
7. **Commits.**
   - Pages are written as soon as the update runs.
   - One commit a day, at the start of the first chat that day (one signing approval, for example a 1Password prompt), with the push right after.
   - Signing stays as configured. A failed push, for example to a self-hosted GitLab that is unreachable off the work network, retries later and never blocks a hook.
8. **Built-in memories.** Claude Code's auto memory and Codex's memories are left as they are for now.
9. **Language.**
   - One Go binary serves as the CLI, the hook handler and the MCP server.
   - eva's packaging approach (plugin layout, marketplace, verify, rollback) is rebuilt inside it.
   - Standard-library Python is the fallback.
10. **Project identity.**
    - A project is identified by the repo's remote URL, so all checkouts and worktrees share memory. Without a remote, the folder identifies it.
    - Chats outside any project go to a general area.
11. **Format.**
    - OKF-compatible concept pages: one concept per page with small, fixed-order frontmatter.
    - Each project has a Now page: current state on top, dated timeline below.
    - The generated index has no timestamps, and logs are append-only with a union merge rule.
12. **Reliability.**
    - Hooks capture every turn, so capture never depends on the model remembering.
    - A checkpoint is saved before compaction, and the next chat catches up on anything a hook missed.
    - Hooks always finish fast and never block.
    - Unknowns are reported as unknown.
    - A health check confirms the hooks are installed, approved and firing. Codex needs re-approval after each update.
    - A resume test, plus eva's Conflict24 and scope suites, check dates, speakers, corrections and project scope.
13. **Independent of eva.**
    - eva and eva.md are separate options, evaluated independently.
    - They are not meant to run at the same time and don't share memory.
    - eva.md uses its own names (plugin, command, MCP server, state folders), so installing it never interferes with eva.
14. **Name.**
    - The product is eva.md, said aloud as "Eva dot M D": the markdown edition of Eva.
    - The technical name is `eva-md`, used for the plugin, the command, the MCP server and the state folder. Both Claude Code and Codex ask for kebab-case plugin names.
    - By convention the wiki repo is named `eva-wiki`, on any git host.
15. **okf at the core.**
    - eva-md is the orchestrator. okf (okf-memory/okf-agent-memory, MIT license) is the core memory engine, compiled in as a Go library at a pinned version.
    - okf owns the page format, safe writes, search, validation and setting up a new wiki.
    - eva-md owns the hooks, the inbox, the background update, resume, the daily commit, the plugins, the config and the health check.
    - One adapter in eva-md is the only code that calls okf. It holds the protections okf lacks today: no overwrite on create, one writer at a time, supersede, and the real host recorded as author. Each one is removed once okf ships it.
    - No issues or pull requests go to okf. Anything okf lacks is built on top of it inside eva-md, and removed once an okf release covers it.
    - okf is reviewed regularly, weekly or monthly (the cadence is still to be decided). Upgrades happen only for security fixes or releases that cover a gap eva-md works around, and are tested on a branch before merging.
    - Not used: okf's registry, hub sync, vendor bundles, governance tiers, scorecards, its agent instruction block and its own MCP server.
    - Way out: the wiki stays in okf's markdown format, so the core can be swapped without touching memory.
16. **Settled details.**
    - **Day one:** resuming only, meaning capture, the background update, the Now page, the context at chat start and the health check. Knowledge pages and search come next.
    - **Coverage:** every project automatically, with an exclude list for sensitive folders.
    - **Parallel chats:** one thread per branch, plus a short project overview.
    - **What resuming shows:**
      - a summary of the last chat, what's done, next steps, and open decisions and questions
      - the current git branch and uncommitted changes, read live
      - open PRs and failing tests come later
    - **Tool activity:** the conversation plus a short record of files changed and commands run, without their full output.
    - **Visibility:** one line at the start of a chat, plus a notice only when something is wrong.
    - **Off switch:** saying "off the record" stops saving for that chat.
    - **Work machines:** eva-md assumes plugins, hooks and background headless runs are allowed there. The health check still reports managed settings that switch hooks off, where it can see them.
    - **Repos:** the platform repo (github.com/heyNag/eva-md) holds no personal data, so it can be public. Each wiki repo stays private.
    - **Wiki location:** chosen by you during setup; there is no default. Any folder of its own, outside Documents, Desktop and iCloud Drive, which macOS protects from background processes.
17. **Background update settings.**
    - Codex update runs use your normal Codex config (`~/.codex/config.toml`) by default, so its provider settings apply. Your MCP servers start on those runs too.
    - Effort is configurable per host and per model. The default is the highest effort the chat's model supports.
    - If Codex asks you to re-approve eva-md's hooks after an update, you approve them. A notice and the health check point you to Codex's hooks settings.

## Ruled out

- Local models
- Vector databases and resident servers
- A model call on every turn
- Automatic deletion
- History backfill on install
- Telemetry and auto-update
- Writing host settings files directly
- Committing raw captures

## What we borrow

| Source | Idea |
| --- | --- |
| okf-agent-memory | Page format, provenance the tool stamps itself (agents can't mark anything human-verified), reload from disk on every call, a cap on always-loaded text |
| brain.md | Page shape: current truth on top, dated timeline below |
| iwe | In-process keyword and fuzzy search, a titles-only list at session start, near-duplicate check after writes. It shipped automatic capture and removed it, so the promotion bar matters. |
| tigerless agent-memory | Markdown as truth with a rebuildable search cache and a test proving it; hook habits (always exit 0, timeouts, recursion guard, per-session watermark) |
| basic-memory | Thin plugin shims, a session-start brief marked as data, the Codex compaction workaround |
| markdown-agent-memory | Provenance tags (stated, observed, inferred, suggested), dated strikethrough supersession |
| memsearch | Links from entries back to the source conversation; recall in a sub-agent |
| obsidian-second-brain | A per-prompt recall hook that stays silent on weak matches; validation after writes |
| eva (the Mem0 option) | Plugin packaging for both hosts, hook guards (desktop spare sessions, headless runs), transcript filter rules, the Conflict24 and scope suites, privacy conventions (0600 files, logs with counts only) |

## For the design

- The v1 scope limit
- The wiki schema: folders, page types, frontmatter, Now page sections, the schema version file
- Inbox format, location and redaction rules
- Background update: triggers, prompt, output contract, validation, catch-up
- Recall: sizes, data fencing, the strong-match rule
- Plugin packaging for both hosts, plus install, uninstall and the health check
- Daily commit and push mechanics
- Tests: the resume test and a Conflict24 port
