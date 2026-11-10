# OpenAI Codex as a platform for a memory layer: state on 2026-11-10

Research notes. Every fact has a source. Sources were read on 2026-11-10.

How to read the sources:

- **Docs.** The Codex docs moved: `developers.openai.com/codex/<page>` now answers with a 308 redirect to
  `learn.chatgpt.com/docs/<page>`. Every page has a Markdown twin at `<url>.md`, and the index is
  `https://learn.chatgpt.com/docs/llms.txt`. The docs pages carry no "last updated" date, so "as of" is the day
  they were read, 2026-11-10. The plugin developer docs stayed at `developers.openai.com/plugins/...`.
- **Source.** `github.com/openai/codex`, branch `main`, commit `1885417cd45a8da5bfdeb7864a5380a7481d48f2`
  (2026-11-10). File paths below are in that tree. Main can be ahead of the latest release; the hooks page says so
  itself about schemas ("The linked `main` branch schemas may include hook fields that are not in the current
  release").
- **Changelog.** `https://learn.chatgpt.com/docs/changelog`. Latest release: Codex CLI 0.167.0, 2026-11-06.

Confidence marks: **[H]** read in an official doc or in source, **[M]** inferred from source or from two docs that
agree, **[L]** inferred, not confirmed.

---

## 0. What changed underneath: the product shape in late 2026

- The Codex app merged into the **ChatGPT desktop app** on 2026-07-09 (macOS and Windows; Linux preview since
  2026-08-11). The desktop app has three modes: Chat, Work ("ChatGPT Work") and Codex. **[H]**
  https://learn.chatgpt.com/docs/whats-new (July 6-10 and August 10-14, 2026)
- Surfaces today: Codex CLI (TUI + `codex exec`), Codex in the ChatGPT desktop app, the IDE extension (VS Code,
  Cursor, Windsurf; Xcode and JetBrains have their own integrations), Codex Cloud, ChatGPT web/mobile (Chat and
  Work), remote control from mobile to a connected Mac, SSH remote hosts, and "dots" (always-on agents, DevDay
  2026-09-29). **[H]** https://learn.chatgpt.com/docs/llms.txt ; https://learn.chatgpt.com/docs/whats-new/devday-2026
- A local **background daemon** now exists (`daemon_auto_start`, stage Stable, default on; `/daemon`,
  `codex agents`, `codex queue --thread <id> --message ...`, `codex remote-control`). Codex itself is no longer
  "no daemon". **[H]** `codex-rs/features/src/lib.rs`; https://learn.chatgpt.com/docs/developer-commands?surface=cli
- Models: GPT-6 Astra, GPT-6.1 Sol (default recommendation, 500k context in Codex), GPT-6.1 Terra, GPT-6 Sol,
  GPT-6 Luna (cheap, fast), plus GPT-5.6 leftovers. Slugs: `gpt-6-astra`, `gpt-6.1-sol`, `gpt-6.1-terra`,
  `gpt-6-luna`. **[H]** https://learn.chatgpt.com/docs/models
- `codex mcp-server` (Codex as an MCP server) was **removed**; the replacement is the app server. **[H]**
  https://learn.chatgpt.com/docs/mcp-server ; https://learn.chatgpt.com/docs/developer-commands?surface=cli

---

## 1. Hooks

Source for this whole section unless noted: https://learn.chatgpt.com/docs/hooks **[H]**

### 1.1 Status

- Hooks are **generally available** (GA 2026-05-14) and **on by default** (`[features] hooks = true`; `codex_hooks`
  is a deprecated alias). Source flag: `hooks`, `Stage::Stable`, `default_enabled: true`.
  https://learn.chatgpt.com/docs/whats-new (May 11-15, 2026); `codex-rs/features/src/lib.rs`

### 1.2 Events (14)

| When | Events |
| --- | --- |
| During a turn | `PreToolUse`, `PermissionRequest`, `PostToolUse`, `PreCompact`, `PostCompact`, `UserPromptSubmit`, `SubagentStop`, `Stop` |
| Each model request attempt | `PreModelRequest`, `PostModelRequest` (diagnostic only) |
| User interrupts a turn | `Interrupt` (main thread only) |
| Session or subagent starts | `SessionStart`, `SubagentStart` |
| Main thread ends | `SessionEnd` (not for subagents) |

When they arrived (changelog): `SessionEnd`, configurable context limits and compact-time `SessionStart` in
0.145.0 (2026-07-21); async command hooks and `mcp_tool` handlers in 0.148.0 (2026-08-18); `Interrupt` in 0.150.0
(2026-08-26); model-request hooks and "start dormant MCP servers for hook calls" in 0.164.0 (2026-10-23).
https://learn.chatgpt.com/docs/changelog

### 1.3 Config format and sources

- `hooks.json` or inline `[hooks]` tables in `config.toml`, next to each active config layer: `~/.codex/`,
  `<repo>/.codex/` (only when the project is trusted), plugin (`hooks/hooks.json` or the manifest's `hooks`),
  managed (`requirements.toml`, MDM, cloud). All matching hooks from all sources run; layers do not replace each
  other. Matching command hooks for one event are **launched concurrently**.
- Shape: event -> matcher group (`matcher` regex) -> handlers. Handler fields: `type` (`command` or `mcp_tool`;
  `prompt` and `agent` are parsed but skipped), `command`, `commandWindows`, `timeout` (seconds), `statusMessage`,
  `additionalContextLimit`, `async`.
- Matchers work for tool events (tool name), compaction (`manual|auto`), `SessionStart` (`startup|resume|clear|
  compact|fork`), `SessionEnd` (`other`), subagent events (agent type). They are ignored for `UserPromptSubmit`,
  `Stop`, `Interrupt`.
- Commands run with the session `cwd`. In source they are launched through the user's shell as
  `$SHELL -lc "<command>"` (fallback `/bin/sh`) in a new session (setsid), with the session's environment
  snapshot. **[M]** `codex-rs/hooks/src/engine/command_runner.rs` (`build_command`, `default_shell_program`). A
  login shell start is therefore part of every command hook's latency, outside the hook binary's control.

### 1.4 What each receives (stdin JSON)

Common: `session_id`, `transcript_path` (string or **null**), `cwd`, `hook_event_name`, `model`; most also
`permission_mode`; turn-scoped ones `turn_id`.

| Event | Extra input |
| --- | --- |
| `SessionStart` | `source`: `startup`, `resume`, `clear`, `compact`, `fork` |
| `UserPromptSubmit` | `prompt` |
| `Stop` | `stop_hook_active`, `last_assistant_message` |
| `SubagentStop` | `agent_id`, `agent_type`, `agent_transcript_path`, `stop_hook_active`, `last_assistant_message` |
| `PreToolUse` / `PostToolUse` | `tool_name`, `tool_use_id`, `tool_input`, and for Post `tool_response` |
| `PreCompact` / `PostCompact` | `trigger`: `manual` or `auto` |
| `SessionEnd` | `reason` (always `other` today) |
| `PostModelRequest` | `status`, `error`, `token_usage` |

"`transcript_path` points to a chat transcript for convenience, but the transcript format isn't a stable
interface for hooks and may change over time." It can be null (ephemeral runs, non-local thread stores; changelog
#35221 "Avoid persisting non-local threads for hook transcripts").

Nothing in the input says which surface (CLI, app, IDE, exec) started the session. **[M]** generated schemas in
`codex-rs/hooks/schema/generated/`.

### 1.5 What each can return

- **Add context to the model:** `SessionStart`, `UserPromptSubmit` (plain stdout or
  `hookSpecificOutput.additionalContext`), `SubagentStart`, `PreToolUse`, `PostToolUse` (JSON only). The text is
  added as "extra developer context".
- **Block:** `UserPromptSubmit` (`decision: "block"` or exit 2), `PreToolUse` (deny, or rewrite input with
  `updatedInput`), `PermissionRequest` (allow or deny), `PreCompact` (`continue: false` stops compaction).
- **Continue the turn:** `Stop` and `SubagentStop` with `decision: "block"` turn `reason` into a new user prompt.
- **Nothing:** `SessionEnd` is advisory; `PreModelRequest`/`PostModelRequest` are diagnostic; `Interrupt` can only
  emit a `systemMessage`.
- `systemMessage` shows as a warning in the UI. `suppressOutput` is parsed but **not implemented**, so a hook cannot
  ask to be hidden.
- After a compaction of a root session, `SessionStart` hooks matching `source: "compact"` run **before the next
  model request**, also mid-turn, so a hook can re-inject what compaction dropped.

### 1.6 Limits and timeouts

- Default timeout **600 s** for most hooks. `SessionEnd` and `Interrupt`: **1 s default, 3 s maximum**.
- Model-visible hook output: about **2,500 tokens** per message by default. More is "spilled" to
  `<temp_dir>/hook_outputs/<session_id>/<uuid>.txt` and the model gets a head-and-tail preview with the path.
  `additionalContextLimit` on a handler raises or lowers it; `0` passes everything.
- "Context from multiple hooks and plugins adds up and can degrade model performance."
- **Async hooks** (`"async": true`, command handlers only): run in the background, output delivered "at the next
  safe point": mid-turn before the next model request, or at the next user turn. They cannot block or rewrite.
  Up to 8 run concurrently per session; unfinished ones are cancelled when the session ends. `SessionEnd` is
  always synchronous.
- **`SessionEnd`** fires when an open conversation is archived or deleted, when Codex closes normally, or after
  the conversation has been idle and not open in any client for **30 minutes**. Switching away does not fire it.
- **`mcp_tool` handlers** call a tool on an already-connected MCP server with templated arguments
  (`${tool_input.file_path}`), same output contract as a command hook. They are synchronous, do not start or
  reconnect servers (0.164.0 can start dormant ones), do not block on errors or a missing server, are not
  supported for `SessionEnd`, and "`SessionStart` hooks can run before an MCP server is ready. If that happens,
  they don't block the session."
- Tool coverage: shell, unified exec, `apply_patch`, MCP tools and local function tools go through
  Pre/PostToolUse; hosted tools such as `WebSearch` do not.

### 1.7 Trust

- "Non-managed hooks must be reviewed and trusted before they run." Trust is recorded "against the hook's current
  hash, so new or changed hooks are marked for review and skipped until trusted". Review: `/hooks` in the CLI
  (a startup warning points to it), **Review hooks** in the app composer or **Settings > Hooks**.
- What the hash covers, from source **[H for main, M for the release]**:
  - User and project hooks: a hash of the normalized **declaration** only (event, matcher, handler config including
    the command string). The script or binary the command points at is not hashed.
    `codex-rs/hooks/src/engine/discovery.rs` (`hook_hash`).
  - Plugin hooks: declaration hash **plus a SHA-256 over every file in the plugin root** (paths, sizes, bytes;
    skipping `__pycache__`, `.venv`, `*.pyc`, `*.egg-info`), and "Each execution verifies fresh contents".
    `codex-rs/hooks/src/engine/plugin_fingerprint.rs`, `plugin_trust.rs`; changelog 0.164.0 (2026-10-23) "#54619
    Bind plugin hook approval to plugin contents".
- The docs agree: "If a plugin update changes a bundled script, Codex requires a new review even if the hook
  definition stays the same." and "Installing or enabling a plugin doesn't automatically trust its hooks."
  https://developers.openai.com/plugins/build/plugins
- Consequences: (a) **any** change to a hook-carrying plugin's files (a skill edit, a version bump in the manifest)
  puts its hooks back in review and they are skipped until the user trusts them again; (b) the whole plugin root is
  re-hashed on each hook run, so a large file inside the plugin costs latency on every hook; (c) a user-level hook
  whose command is a stable path keeps its trust when the binary behind the path is upgraded.
- Trust state lives in the user's config layer (hook states with `trusted_hash`, `trusted_content_hash`,
  `enabled`). `--dangerously-bypass-hook-trust` runs hooks without stored trust for one invocation.
- Managed hooks (`requirements.toml`, MDM, cloud) are trusted by policy and cannot be disabled by the user.
  `allow_managed_hooks_only = true` drops every user, project, session and plugin hook. Admins can force hooks
  off with `[features].hooks = false` in `requirements.toml`.

---

## 2. Plugins and marketplaces

Sources: https://developers.openai.com/plugins/build/plugins , https://learn.chatgpt.com/docs/plugins ,
https://learn.chatgpt.com/docs/enterprise/managed-configuration ,
https://learn.chatgpt.com/docs/enterprise/plugin-management **[H]**

### 2.1 What a plugin can ship

- Skills (`skills/<name>/SKILL.md`), MCP servers (`mcp.json`, or legacy `.mcp.json`), registered app mappings
  (`.app.json`), assets, an onboarding skill (`onboardingSkill`, invoked when the user runs setup after install),
  browser extensions, UI extensions (sidebar apps, panels, forms; DevDay 2026), and **lifecycle hooks**
  (`hooks/hooks.json` or the manifest's `hooks`).
- **No** custom agents, slash commands or output styles: the Claude compatibility guide says to convert `commands`
  and `agents` to skills. https://developers.openai.com/plugins/guides/submit-claude-plugin
- Manifest: the portable **Agent Plugins** format, root `plugin.json` with
  `"$schema": "https://agent-plugins.org/schemas/1.0.0/plugin.schema.json"` and OpenAI settings under
  `extensions.com.openai`; `.codex-plugin/plugin.json` remains as a compatibility fallback; `.claude-plugin/`
  manifests and `.claude-plugin/marketplace.json` are read too. Portable Agent Plugins arrived in CLI 0.146-0.147
  (2026-07-29, 2026-08-07).
- Hook commands get `PLUGIN_ROOT`, `PLUGIN_DATA` (a writable data directory), and `CLAUDE_PLUGIN_ROOT` /
  `CLAUDE_PLUGIN_DATA` "for compatibility with existing plugin hooks".

### 2.2 Limits on plugins with hooks or local servers

- "Lifecycle hooks are supported for plugins installed manually in Codex desktop. Plugins containing lifecycle
  hooks **aren't eligible for the public plugin directory**."
- Local (stdio) MCP servers make a plugin "Desktop only", and "Local MCP submissions are limited to approved
  partners." https://developers.openai.com/plugins/deploy/desktop-only
- So a memory plugin with hooks and a local server can only be distributed through its own marketplace (Git repo,
  local folder, npm package) or a workspace import, never the public directory.

### 2.3 Install and update

- Marketplaces: `$REPO_ROOT/.agents/plugins/marketplace.json`, `~/.agents/plugins/marketplace.json`,
  `$REPO_ROOT/.claude-plugin/marketplace.json`, or sources added with `codex plugin marketplace add <owner/repo |
  git URL | local dir>` (`--ref`, `--sparse`). Entry sources: `local`, `url`, `git-subdir`, `npm`.
- Installed copy: `~/.codex/plugins/cache/$MARKETPLACE_NAME/$PLUGIN_NAME/$VERSION/` (`local` for local plugins).
  Codex loads the cached copy, not the marketplace folder.
- CLI: `codex plugin add|list|remove` with `--json`; `codex plugin marketplace add|list|upgrade|remove`. All
  scriptable.
- Updates: `codex plugin marketplace upgrade`; workspace marketplaces sync daily. "Existing sessions pick up newly
  installed plugin tools and refresh skills and hooks after external plugin upgrades or rollbacks" (0.154.0,
  2026-09-09). For local plugins: update the folder and restart the desktop app.
- Enable per repo: `[plugins."name@marketplace"] enabled = true` in a trusted `.codex/config.toml`.
- `policy.installation` values include `AVAILABLE`, `INSTALLED_BY_DEFAULT`, `NOT_AVAILABLE`.

### 2.4 Enterprise controls

- `features.plugins = false` in `requirements.toml` turns plugins off in local clients.
- `[marketplaces] restrict_to_allowed_sources = true` with `allowed_sources` (git URL + ref, host pattern, local
  path) rejects other marketplace adds, installs and refreshes.
- Admins import and sync a GitHub marketplace for the workspace (daily sync, "Sync now").
- `allow_managed_hooks_only`, pinned feature flags, `features.memories` pin, `features.plugin_sharing`.

---

## 3. Built-in memory

Sources: https://learn.chatgpt.com/docs/customization/memories ,
https://learn.chatgpt.com/docs/config-file/config-reference , and source under `codex-rs/memories/`,
`codex-rs/ext/memories/` **[H]**

### 3.1 Status

- Local Codex memories are **off by default** (`[features] memories = true`, or **Settings > Personalization >
  Enable Codex memories**). The flag is `Stage::Stable`, `default_enabled: false`. Launched 2026-04-16.
- Memory **v2** is the default for a fresh Codex home since 0.165.0 (2026-10-27): `$CODEX_HOME/memories_v2/`.
  An existing `$CODEX_HOME/memories/` keeps v1. Default models changed again in 0.167.0 (2026-11-06).
- Per-chat control with `/memories` in the app, TUI and IDE extension. ChatGPT web and Work use ChatGPT's cloud
  memory instead, not the local store.

### 3.2 How it is produced

`codex-rs/memories/README.md`:

- Triggered **when a root session starts** (not at session end), in the background, if the session is not
  ephemeral, the feature is on, it is not a subagent and the state DB is available.
- **Phase 1, per thread:** picks rollouts from interactive sources only (`cli`, `vscode`, `atlas`, `chatgpt`; not
  `exec`), at most 30 days old, idle at least **6 hours**, at most **16 per startup**, 8 in parallel. The rollout is
  filtered and truncated to 70% of the model's window, sent to the extraction model (default `gpt-6-luna`, low
  reasoning effort) with a structured output (`rollout_summary`, `rollout_slug`; v1 also `raw_memory`). Secrets are
  redacted. Results go to the SQLite state DB.
- **Phase 2, global:** under one global lock, syncs `raw_memories.md` and `rollout_summaries/` into the memory
  root (a git-baselined directory), writes `phase2_workspace_diff.md`, and, if anything changed, spawns an
  internal consolidation agent (default `gpt-6.1-terra`, medium effort, no approvals, no network, local write
  only) that rewrites `memory_summary.md` (v1 also `MEMORY.md` and `skills/`).
- Guards: skipped when the remaining rate limit is under `memories.min_rate_limit_remaining_percent` (default 25);
  `memories.disable_on_external_context` keeps threads that used MCP, web search or tool search out; memories
  unused for `max_unused_days` (30) drop out of consolidation; ranking by `usage_count` then recency.
- The v2 extraction prompt is careful about over-generalising: "Write task history, not a user profile", "the
  user asked to show a plan before editing" rather than "the user prefers...".
  `codex-rs/memories/write/templates/memories/stage_one_system_v2.md`

### 3.3 How, and how much, is loaded

- At thread start, `memory_summary.md` is read, **truncated to 2,500 tokens**, wrapped in a read-path prompt and
  added as developer instructions. `codex-rs/ext/memories/src/lib.rs`
  (`MEMORY_TOOL_DEVELOPER_INSTRUCTIONS_SUMMARY_TOKEN_LIMIT = 2_500`), `prompts.rs`, `extension.rs`.
- The v2 consolidation prompt asks for a summary "comfortably under 10,000 UTF-8 bytes" with fixed sections:
  `## User Profile`, `## User preferences`, `## General Tips`, `## What's in Memory` (by project scope and date,
  each entry a pointer to a `rollout_summaries/<file>` with its thread id).
- Deeper recall is by the model: it reads `rollout_summaries/` files on demand, and memory tools (`list`, `read`,
  `search`, `add ad-hoc note`) exist. Used memories are cited in an `<oai-mem-citation>` block, which feeds
  `usage_count`/`last_usage`.
- "Memory is not proof of current behavior... inspect the actual owning source when warranted" is part of the
  read prompt. `codex-rs/ext/memories/templates/memories/read_path_v2.md`
- Explicit "remember/forget" requests append a note under `extensions/ad_hoc/notes/`; the model is told not to
  edit generated memory files.

### 3.4 The extension folder: how other sources feed Codex memory

- Phase 2 reads `<memory root>/extensions/<name>/instructions.md` for every extension folder and treats the
  folder's `resources/*.md` as extra inputs; resource files whose filename timestamp is older than **14 days**
  are pruned; deleted resources remove the memories derived only from them.
  `codex-rs/memories/write/src/lib.rs` (`prompt_blocks`), `extensions/prune.rs`.
- First-party users of this path: `ad_hoc` (user notes), `skysight` (Computer History, files under
  `$CODEX_HOME/memories/extensions/skysight/`), and `external_agent_import` (Claude Code project memories brought
  in by `/import`, with a `scope.json` giving the project cwd).
  https://learn.chatgpt.com/docs/customization/computer-history ;
  `codex-rs/external-agent-migration/src/memory_import.rs`
- It is **not a documented public interface**. The docs say: "Treat these files as generated state... don't rely
  on editing them by hand as your primary control surface." **[M]** that a third party writing its own extension
  folder would work today; **[L]** that it will keep working.

### 3.5 Related built-ins

- **Computer History** (2026-08-11; replaces the Chronicle preview): macOS desktop, Pro/Business/Enterprise, off
  by default, needs Memories. Turns interaction events into local Markdown memories and a timeline; summaries are
  made by "an ephemeral Codex session" and the events are processed on OpenAI servers.
- **Import from another agent** (`/import`, Settings > Import; 2026-08-11): imports instructions (`CLAUDE.md` ->
  `AGENTS.md`), settings, skills, plugins, MCP config, hooks, slash commands, subagents, "Project memories from
  Claude Code" -> Memories, and chats from the last 30 days (CLI: up to 50). The desktop app can "keep imported
  work in sync with automatic updates". https://learn.chatgpt.com/docs/import
- **Recaps:** `/recap` and `tui.auto_recap` (default on) summarize a conversation without compacting it.
- **dots** keep "their own saved notes" separate from ChatGPT memory and can search past ChatGPT conversations.
  https://learn.chatgpt.com/docs/dots/tasks-and-memory
- An alpha, server-backed "history" and "notes" tool namespace exists in source for model-private recovery of
  earlier context windows (`codex-rs/ext/history-notes`). **[L]** on availability.

### 3.6 AGENTS.md

https://learn.chatgpt.com/docs/agent-configuration/agents-md **[H]**

- Chain: `~/.codex/AGENTS.override.md` else `~/.codex/AGENTS.md`; then from the project root down to the cwd, one
  file per directory (`AGENTS.override.md`, `AGENTS.md`, then `project_doc_fallback_filenames`). Concatenated root
  first; stops at `project_doc_max_bytes`, **32 KiB** by default.
- Built "once per run; in the TUI this usually means once per launched session". No imports, no per-prompt reload.
- OpenAI's own positioning: "Keep required team guidance in `AGENTS.md` or checked-in documentation. Treat
  memories as a helpful recall layer, not as the only source for rules that must always apply."
- `developer_instructions` in `config.toml` adds static developer instructions to every session.

### 3.7 How a third-party layer can coexist or feed these

- Coexist: hook context (`SessionStart`, `UserPromptSubmit`) and Codex's memory summary are both developer
  context; both can be on at once, and can then say the same thing twice or disagree.
- Feed, supported: write `AGENTS.md` (rules), ship skills, serve an MCP server.
- Feed, unsupported: a memory extension folder (3.4).
- Keep own background runs out of Codex memory: `exec` sessions are not memory inputs, and `--ephemeral` runs do
  not start the memory pipeline.

---

## 4. Skills, subagents, MCP

- **Skills** (https://learn.chatgpt.com/docs/build-skills **[H]**): `SKILL.md` with `name` and `description`;
  locations `.agents/skills` from cwd up to the repo root, `~/.agents/skills`, `/etc/codex/skills`, bundled, and
  plugin skills. Progressive disclosure: the initial list (name, description, path) takes at most **2% of the
  context window** or 8,000 characters (`skills.max_context_tokens`, up to 10,000 tokens). Explicit `$skill` or
  `/skills`; implicit by description unless `allow_implicit_invocation: false` in `agents/openai.yaml`, which can
  also declare MCP dependencies that Codex installs. Standalone skills work in the app, CLI and IDE extension.
- **Subagents** (https://learn.chatgpt.com/docs/agent-configuration/subagents **[H]**): on by default; built-in
  `default`, `worker`, `explorer`; custom agents are TOML files in `~/.codex/agents/` or `.codex/agents/` with
  `name`, `description`, `developer_instructions`, optional `model`, `model_reasoning_effort`, and feature
  switches (`features.shell_tool`, `features.apps`, `features.plugins`, `features.memories` = false). Codex
  delegates only on a direct request or when `AGENTS.md` or a skill asks. `SubagentStart` hooks can add context.
- **MCP** (https://learn.chatgpt.com/docs/extend/mcp **[H]**): stdio and Streamable HTTP; config in `config.toml`
  shared by the app, CLI and IDE extension; `startup_timeout_sec` 10, `tool_timeout_sec` 60,
  `mcp_optional_startup_grace_ms` 1000; per-tool approval modes.
  - **Server instructions:** "Codex reads the MCP `instructions` field returned during initialization and uses it
    as server-wide guidance alongside the server's tools... Keep the first 512 characters self-contained".
  - **Resources:** model-callable `list_mcp_resources`, `list_mcp_resource_templates`, `read_mcp_resource` exist in
    source (`codex-rs/core/src/tools/handlers/mcp_resource.rs`). **[M]** They are not in the docs' feature list.
  - **Prompts:** no MCP prompts support found in source or docs. **[M]**
  - Elicitation is supported. MCP tool use in a thread can exclude it from Codex memory generation
    (`memories.disable_on_external_context`, default false).
  - MCP Events (webhook delivery, DevDay 2026) are for hosted servers in ChatGPT.

---

## 5. Headless runs for a background summarizer

Sources: https://learn.chatgpt.com/docs/non-interactive-mode ,
https://learn.chatgpt.com/docs/developer-commands?surface=cli , `codex-rs/exec/src/cli.rs`,
`codex-rs/utils/cli/src/shared_options.rs`, `codex-rs/cli/src/main.rs` **[H]**

- `codex exec "<prompt>"`: progress on stderr, final message on stdout. Stdin is appended as a `<stdin>` block when
  a prompt argument is given; `-` reads the whole prompt from stdin.
- **Structured output:** `--output-schema <file>` (JSON Schema for the final response), `-o/--output-last-message
  <file>`, `--json` (JSONL events: `thread.started`, `turn.*`, `item.*`, `error`; `turn.completed` carries token
  usage).
- **Model and effort:** `-m <slug>`, `-c model_reasoning_effort=<level>`, `--profile`.
- **Sandbox:** read-only by default; `--sandbox workspace-write|danger-full-access`; `--full-auto` is gone;
  `--approve-for-me` routes approvals to automatic review.
- **Isolation:** `--ephemeral` (no rollout on disk, and no memory pipeline), `--ignore-user-config` (no
  `$CODEX_HOME/config.toml`; auth still from `CODEX_HOME`), `--ignore-rules`, `--skip-git-repo-check`,
  `--thread-source <SOURCE>` (labels the thread), `--strict-config`.
- **Turning tools off:** no single "no tools" switch. Features can be disabled per run with `--disable <feature>`
  or `-c features.<name>=false` (`shell_tool`, `apps`, `plugins`, `memories`, `multi_agent`, `hooks`), and
  `web_search = "disabled"`. **[M]** that this leaves a run with no callable tools; it needs a test.
- **Hooks do run in `exec`** (the exec client handles hook notifications), so a summarizer run can fire the memory
  layer's own capture hooks unless it is isolated. **[M]**
- **Auth and cost:** reuses the saved CLI login, so it spends the user's ChatGPT plan allowance: five-hour and
  weekly limits shared with interactive work. Plus, per five hours: GPT-6 Luna 350-3,000 local messages, GPT-6.1
  Sol 15-160, GPT-6 Astra 5-45. Credits per 1M tokens (input / cached / output): Luna 2.5 / 0.25 / 12.5, GPT-6.1
  Terra 25 / 1.25 / 125, GPT-6.1 Sol 50 / 2.5 / 250, Astra 250 / 25 / 1,250. `CODEX_API_KEY` switches a run to API
  billing. https://learn.chatgpt.com/docs/pricing
- Codex's own memory writer is a calibration point: Luna at low effort for extraction, Terra at medium for
  consolidation, and it stands down below 25% remaining rate limit.
- **A second path, new on 2026-09-29: Sign in with ChatGPT, "ChatGPT plan usage" for open-source and locally
  hosted apps.** The app registers itself by OAuth (dynamic registration, PKCE, loopback redirect, no client
  secret, no partner key), the user consents once, and the app then calls `POST https://api.openai.com/v1/responses`
  with the token, billed to the user's Plus or Pro plan. Limits: `store: false`, `stream: true`, no
  `max_output_tokens`, `temperature`, `metadata`; no hosted tools. "ChatGPT plan usage is available to all
  open-source partners and selected private clients." **[H]**
  https://developers.openai.com/siwc/token-sharing-open-source ,
  https://developers.openai.com/siwc/token-sharing-open-source/preview-limitations ,
  https://developers.openai.com/siwc/quickstart
- Also available: the Codex SDK (TypeScript) and the app server (`codex app-server`, JSON-RPC over stdio).

---

## 6. Transcripts

- **Where:** `$CODEX_HOME/sessions/` and `$CODEX_HOME/archived_sessions/`, files named
  `rollout-<YYYY-MM-DDTHH-MM-SS>-<thread id>[_<rollout id>].jsonl`. `history.jsonl` is a separate message history.
  A SQLite state DB (`sqlite_home`) holds thread metadata and memory jobs. **[H]** `codex-rs/rollout/src/lib.rs`,
  `rollout_file_name.rs`; https://learn.chatgpt.com/docs/config-file/config-advanced
- **Format stability: poor, and getting worse for outside readers.**
  - The hooks page: "the transcript format isn't a stable interface for hooks and may change over time."
  - Lines are `{timestamp, ordinal?, <item>}` with many item kinds (`SessionMeta`, `ResponseItem`, `Compacted`,
    `TurnContext`, `EventMsg`, `WorldState`, `RetainedContext`, `InterAgentCommunication`, ...).
    `codex-rs/history/src/lib.rs`
  - Two history modes exist, "legacy" and "paginated" (experimental since 0.145.0), which persist user messages
    differently (`UserMessage` vs `ItemCompleted(UserMessage)`). `codex-rs/rollout/src/list.rs`
  - **Cold rollouts are being compressed to `.jsonl.zst`** (zstd level 3, files older than 7 days). The flag
    `local_thread_store_compression` is still "under development, default off", but 0.164.0 (2026-10-23) already
    fixed "Resuming sessions now finds compressed histories". `codex-rs/rollout/src/compression.rs`,
    `codex-rs/features/src/lib.rs`
  - Threads can live in non-local stores (synced Work threads); then there is no local file.
- **Supported ways to read a thread:** the app server (`thread/list`, `thread/read`, `thread/turns/list`,
  `thread/items/list` with cursors, without resuming the thread), `/export` (Markdown), `codex exec --json`.
  `thread/inject_items` appends items to a loaded thread's history without starting a turn.
  https://learn.chatgpt.com/docs/app-server
- **Resume and fork:** `codex resume`, `codex fork`, `codex exec resume|fork`, `/side` (ephemeral fork), chat
  handoff between hosts. `SessionStart` tells them apart: `resume`, `fork`, `clear`, `startup`.
- **Compaction:** auto at `model_auto_compact_token_limit`, optionally after a turn
  (`model_post_turn_compact_threshold_percent`), or `/compact`. Local compaction keeps recent **user messages up
  to 20,000 tokens** plus a model-written handoff summary ("Current progress and key decisions... What remains to
  be done"), then rebuilds the initial context. OpenAI-hosted models use remote compaction through Responses.
  `compact_prompt` overrides the prompt. Hook-injected developer context is not kept, which is what the
  compact-time `SessionStart` is for. **[M]** `codex-rs/core/src/compact.rs`,
  `codex-rs/prompts/templates/compact/prompt.md`

---

## 7. Which surface runs what

| Surface | Plugins | Hooks | Local memories | Skills | MCP |
| --- | --- | --- | --- | --- | --- |
| Codex CLI (TUI) | yes (`/plugins`) | yes (`/hooks`) | yes | yes | yes |
| `codex exec` | follows config | yes **[M]** | not an input; `--ephemeral` skips the pipeline | yes | yes |
| Codex in the ChatGPT desktop app | yes | yes (Review hooks, Settings > Hooks) | yes | yes | yes |
| ChatGPT Work, local-only threads | yes | yes ("when both orchestration and execution are local") | no: ChatGPT memory | plugin skills | yes |
| ChatGPT Work with cloud orchestration, dots | plugins yes | **only admin-defined Global `mcp_tool` hooks**; no command, local or plugin hooks | no | plugin skills | hosted |
| IDE extension | **no** ("The IDE extension doesn't support plugins") | not documented; shares the host's config **[L: likely runs user-level hooks]**; no `/hooks` review UI | yes (`/memories`) | yes | yes (shared config) |
| Codex Cloud | not documented | not documented | no | repo skills only ("Personal skills from your local computer aren't synced") | per environment |
| Mobile remote, SSH host | runs on the connected host, so that host's plugins, hooks and config apply | | | | |

Sources: https://learn.chatgpt.com/docs/plugins , https://learn.chatgpt.com/docs/hooks ,
https://learn.chatgpt.com/docs/customization/memories ,
https://learn.chatgpt.com/docs/environments/cloud-environments , https://learn.chatgpt.com/docs/remote-connections ,
https://learn.chatgpt.com/docs/developer-commands?surface=ide

---

## 8. New in 2026 that a memory layer could use to feel built in

1. **Async hooks** (0.148.0): recall that takes longer than a blocking budget can run in the background and land
   before the next model request of the same turn.
2. **`mcp_tool` hook handlers** (0.148.0, dormant-server start in 0.164.0): lifecycle events delivered as tool
   calls to a long-lived MCP server the host already runs. No shell, no process start, warm state.
3. **`SessionStart` with `source: "compact"`** (0.145.0): re-inject memory right after a compaction, mid-turn.
4. **`SessionEnd`** (0.145.0) and `Stop` with `last_assistant_message`: a real end-of-session signal and a
   per-turn capture point that does not need the transcript file.
5. **`additionalContextLimit`** and spill files: a known budget, and a path the model can read for more.
6. **`PLUGIN_ROOT` / `PLUGIN_DATA` and `CLAUDE_PLUGIN_*` compatibility; Agent Plugins manifests; Codex reading
   `.claude-plugin/`**: one plugin tree can serve both hosts.
7. **Scriptable plugin install** (`codex plugin marketplace add`, `codex plugin add --json`) and an
   `onboardingSkill`.
8. **MCP server `instructions`**: standing guidance without touching `AGENTS.md`.
9. **The app server's thread API**: a documented way to read threads, instead of parsing rollouts.
10. **Sign in with ChatGPT plan usage** (2026-09-29): direct Responses calls on the user's plan.
11. **Memory extensions folder and memory v2** (2026-10-27): a file drop that Codex's own consolidation reads.
12. **`/import` with sync**: Codex already reads Claude Code's instructions, memories and chats.
13. **Enterprise:** managed hooks, marketplace allowlists, default-installed plugins.

## 9. What stays impossible or closed

- No hook-free injection: there is no public API to add context to an interactive session other than hooks,
  `AGENTS.md`, skills, MCP, and the app server's `thread/inject_items` for a client that owns the thread.
- **Trust cannot be granted for the user.** Every non-managed hook needs a review; a plugin's hooks need it again
  after any change to the plugin. Only managed (admin) hooks skip it.
- A hook cannot hide itself (`suppressOutput` not implemented) and cannot learn which surface it runs in.
- The public plugin directory takes no plugin with hooks, and local MCP servers only from approved partners.
- The IDE extension has no plugins; cloud-orchestrated Work and dots run no local or plugin hooks; Codex Cloud
  has no documented user hooks or local memory.
- Codex's memory store is generated state with no public write API; the extension folder is internal.
- Rollout files are not an interface: format in flux, compression coming, some threads not on disk.
- `prompt` and `agent` hook handlers are parsed but skipped; `PreModelRequest` cannot change the request.
- MCP prompts are not supported; `codex mcp-server` is gone.
- Hosted tools (web search) are invisible to tool hooks.

## 9a. Ideas for the redesign (each rests on the facts above)

1. **Keep the hooks out of anything that changes.** Since 0.164.0 a plugin's hook approval is bound to a hash of
   the whole plugin folder, so every plugin update silently switches capture off until the user reviews again.
   Either register hooks at user level (`~/.codex/hooks.json`) with a command that is a stable path, or ship a
   tiny hook-only plugin that never changes and a second plugin for skills and the MCP server that can. (1.7)
2. **Notice when the hooks are not running.** Untrusted hooks are skipped with only a startup warning. The MCP
   server and the skills still load, so they can see that no hook event arrived and say so once. (1.7, 4)
3. **Let the host run the long-lived process.** A local MCP server that Codex starts is a daemon the user never
   sees. `mcp_tool` hook handlers then deliver prompt and tool events to it with no shell and no process start.
   Keep command hooks for `SessionStart` (server may not be ready) and `SessionEnd` (not supported). (1.3, 1.6)
4. **Two-speed recall.** A fast synchronous pass on `UserPromptSubmit` for strong matches, and an `async` pass for
   slower, wider recall that lands before the next model request. (1.6)
5. **Re-inject after compaction**, and use `PreCompact` as a signal to save state first. (1.5, 6)
6. **Capture from hook payloads; read threads through the app server.** Stop depending on rollout files. (1.4, 6)
7. **`SessionEnd` and `Stop` as update triggers**, with the 30-minute idle rule in mind. (1.6)
8. **A sealed summarizer run:** `codex exec --ephemeral --ignore-user-config --output-schema ... -m gpt-6-luna`
   with features disabled and a thread-source label; stand down when the user's limit is low, as Codex does. (5)
9. **A second summarizer backend through Sign in with ChatGPT plan usage:** plain Responses calls, no agent
   harness. (5)
10. **Decide what to do when Codex's own memory is on:** trim the brief to what it lacks; optionally feed it
    through a memory extension folder, marked experimental. (3.4, 3.7)
11. **Borrow Codex's memory craft:** wording calibrated to evidence, use-counted decay, a pointer-style summary
    under a hard budget, a "memory is not proof" clause, diff-driven consolidation, idle and quota gates. (3.2, 3.3)
12. **One plugin tree for both hosts** on the Agent Plugins manifest with the Claude-compatible variables. (2.1)
13. **A hook-free floor:** MCP `instructions` plus an implicitly invoked skill, for surfaces without hooks. (4, 7)
14. **Install inside the host:** scriptable `codex plugin` commands and an onboarding skill. (2.3)
15. **An enterprise mode:** managed hooks need no review and cannot be turned off; marketplaces can be pinned. (2.4)

## 9b. Cautions

- A plugin update turns its hooks off until re-reviewed, and each hook run re-hashes the plugin folder.
- Command hooks start through a login shell; the latency floor is the user's shell profile, not the hook.
- Three memories can now speak at once (Codex memories, Computer History, a third-party brief). Each hook message
  is cut to a preview past 2,500 tokens.
- If a user sets `memories.disable_on_external_context`, any thread in which the model calls a memory MCP tool is
  dropped from Codex's own memory generation.
- Background `exec` runs spend the user's plan limits, fire hooks, and without `--ephemeral` start Codex's memory
  pipeline and leave rollouts.
- Trust state sits in the user's config file. Writing it for the user is possible and must not be done.
- Internal layouts read from source (extension folders, hash rules, rollout items) change without notice. Codex
  shipped 22 minor CLI releases between 2026-07-21 (0.145.0) and 2026-11-06 (0.167.0).
- OpenAI is building the same things natively and quickly: memory v2, Computer History, import with sync, dots'
  notes, recaps.

## 10. Open questions

- How visible hook runs are in each UI when no `statusMessage` is set, and whether injected context is shown.
- Whether the IDE extension runs user-level hooks, and where a user would review them there.
- Whether a detached child started by a hook survives the hook's exit and the session's end (hooks run in their
  own session; timed-out hook process trees are terminated).
- Whether `--ignore-user-config` also drops `~/.codex/hooks.json` hooks (it drops the trust state, so they would
  be skipped as untrusted; not confirmed).
- Whether a run with shell, apps, plugins, multi-agent and web search disabled really has no tools.
- How much a `$SHELL -lc` start adds per command hook on real machines.
- Whether Sign in with ChatGPT plan usage is open to any open-source app by self-registration, or needs partner
  status; what its rate limits are; whether Business/Enterprise seats can use it.
- Whether Codex Cloud runs repo-level `.codex/hooks.json`.
- Whether third-party memory extension folders will be honoured in future releases.
- The exact output contract of an `mcp_tool` hook (how a tool result maps to `additionalContext`).

---

## Fact check

Done on 2026-11-10 by a second reader whose job was to prove the facts above wrong. Docs were read as Markdown
from `learn.chatgpt.com` and `developers.openai.com`. Source was read at commit `1885417c` (main, 2026-11-10) and
at the release tags `rust-v0.163.0`, `rust-v0.164.0` and `rust-v0.167.0`. Nothing was run: every source finding
below comes from reading code, not from a test, so the ones the design leans on still need a live check.

### The four corrections that matter most

1. **Command hooks do not start a login shell (F11 is wrong).** In a normal local session Codex launches a hook as
   `<the user's detected shell> -c "<command>"`. `build_hooks_config` calls
   `shell.derive_exec_args("", /*use_login_shell*/ false)`, and that gives `-c` for zsh, bash and sh. `$SHELL -lc`
   (fallback `/bin/sh`) is only used when the session has no single local environment with a known shell. The same
   code is in 0.163.0, 0.164.0, 0.167.0 and main. So the "latency floor is the user's shell profile" caution in 9b
   does not hold: a hook pays for a non-login shell start (zsh still reads `.zshenv`), not for `.zprofile` or
   `.bash_profile`. The new session (setsid) and the environment snapshot are right.
   `codex-rs/core/src/session/mod.rs` (`build_hooks_config`), `codex-rs/core/src/shell.rs` (`derive_exec_args`),
   `codex-rs/hooks/src/engine/command_runner.rs` (`build_command`).
2. **A plugin with the portable root `plugin.json` gets no hooks loaded (F13, idea 12).** `load_plugin` sets the
   hook sources to empty when the manifest format is `AgentPlugin`, and the format is `AgentPlugin` whenever the
   plugin root has a `plugin.json` whose `$schema` starts with `https://agent-plugins.org/schemas/`. Hooks are
   loaded only for the legacy format (`.codex-plugin/plugin.json`, with no agent-plugins root manifest). This is
   so at 0.167.0 and at main. The plugin packaging page says hooks can be set under `extensions.com.openai` in the
   root manifest; the hooks page, which calls itself the release behaviour reference, names only
   `.codex-plugin/plugin.json`. Docs and source disagree, so test before shipping a hook-carrying plugin on the
   portable manifest. `codex-rs/core-plugins/src/loader.rs` (line 986 at 0.167.0),
   `codex-rs/core-plugins/src/manifest/manifest_cache.rs`, `codex-rs/utils/plugins/src/plugin_namespace.rs`.
3. **An `mcp_tool` hook now waits for its server (F06, idea 3).** Since 0.164.0 the hook's tool call is made with
   `wait_for_server = true` (0.163.0 had `false`): it starts a dormant server and waits for it, bounded only by the
   hook's timeout, which is 600 seconds by default. The hooks page still says "They don't start or reconnect
   servers", and it says a `SessionStart` hook that runs before the server is ready "doesn't block the session",
   not that it is skipped. A slow or hung server can therefore hold up the event the hook is attached to; give
   such hooks a short `timeout`. `codex-rs/core/src/hook_mcp_executor.rs`,
   `codex-rs/codex-mcp/src/connection_manager.rs` (`client`, `call_tool`).
4. **Codex's memory writer is smaller than the notes say (F18).** In source the defaults are 2 rollouts per
   startup and a 10-day age limit (`DEFAULT_MEMORIES_MAX_ROLLOUTS_PER_STARTUP = 2`,
   `DEFAULT_MEMORIES_MAX_ROLLOUT_AGE_DAYS = 10`), at 0.167.0 and at main. The docs' config reference says 16 and
   30. The README the fact cites has no numbers at all. `codex-rs/config/src/types.rs`.

### Verdicts

| Fact | Verdict | What the source says |
| --- | --- | --- |
| F01 | partly | Dates, versions, the daemon flag (Stable, default on) and the 22 releases after 0.145.0 are right. Not every page has a Markdown twin: `/docs/changelog.md` answers 404. |
| F02 | confirmed | The hooks page lists the 14 events, the layers, concurrent start and the handler types. GA is in the May 11-15 What's New entry (launch notes of May 14). Source flag `hooks`: Stable, default on. |
| F03 | confirmed | All on the hooks page, word for word in places. `SubagentStart` also takes plain stdout. |
| F04 | confirmed | All on the hooks page. `additionalContextLimit` exists only on command handlers: source sets it to none for `mcp_tool`, so an MCP hook always gets the default cap. The limit applies to `additionalContext` only. |
| F05 | confirmed | Hooks page; changelog 0.148.0 (#37533). `async` is a command-handler field: `mcp_tool` hooks always run synchronously. |
| F06 | partly | See correction 3. Also from source: the tool's text content, joined by newlines, becomes the hook's stdout, and a result with `isError` fails the hook. |
| F07 | partly | The payload facts are right. The conclusion is too strong. `Stop` carries only the last assistant message, which can be null. Earlier assistant messages in a turn, reasoning, hosted tools such as web search, and the text of an interrupted turn never reach a hook. Capture from payloads alone is lossy. |
| F08 | confirmed | Hooks page. The desktop app also shows Review hooks in the composer. |
| F09 | confirmed | `plugin_fingerprint.rs` is byte-identical at 0.164.0, 0.167.0 and main, and absent at 0.163.0; changelog 0.164.0 has #54619. The skip rule matches those four names at any depth. The quoted docs sentence is on the hooks page, not the plugin page. |
| F10 | confirmed | `hook_hash` covers event, matcher, command, timeout, async, statusMessage and a non-default additionalContextLimit, and no file contents. State is `[hooks.state."<key>"]` in the user's `config.toml` (session flags can carry it too). Confidence can go to high for 0.167.0. |
| F11 | refuted | See correction 1. |
| F12 | confirmed | Docs and the generated schemas agree. No field names the surface. `SessionEnd` has no `model` field. Source: the path is null when the thread store is not local or there is no rollout. |
| F13 | partly | The docs say what the fact says. Source disagrees on one point: see correction 2. |
| F14 | confirmed | Both quotes are on the cited pages. |
| F15 | confirmed | Packaging page and command reference. `PLUGIN_DATA` is not in the cache path: see below. |
| F16 | partly | Plugin and hook switches, the source allowlist, daily sync and `INSTALLED_BY_DEFAULT` are in the docs. The cited page has a general "pin feature flags" section and never names `features.memories`. |
| F17 | confirmed | Memories page. One addition: during the migration the desktop app can read `memories/` and write both folders, even on a fresh profile. |
| F18 | partly | See correction 4. Confirmed in source: start-of-session trigger, 6 idle hours, 8 in parallel, interactive sources only (`cli`, `vscode`, `atlas`, `chatgpt`), `gpt-6-luna` at low effort, `gpt-6.1-terra` at medium, the 25% guard. |
| F19 | confirmed | Spot check: the 2,500-token constant, "comfortably under 10,000 UTF-8 bytes" and the `<oai-mem-citation>` block are in source. |
| F20 | confirmed | Spot check: `extensions/<name>/instructions.md`, `resources/`, 14-day retention (#54033) and `external_agent_import` with `scope.json` are in source; the `skysight` path is on the Computer History page. |
| F21 | confirmed | AGENTS.md page and the quote on the Memories page. |
| F22 | confirmed | Import page and the app-server page. |
| F23 | confirmed | Skills and subagents pages. |
| F24 | confirmed | MCP page; resource tools are in source; no prompts handling in the MCP crate; `codex mcp-server` is removed. Not checked: how and when the model is shown a server's `instructions`. |
| F25 | confirmed | Docs and `exec/src/cli.rs`. Two notes: `multi_agent` is marked Deprecated in source (`multi_agent_v2` is a separate flag, Stable, default off), and a prompt is required with `--ephemeral`, `--output-schema` or `-o`. |
| F26 | confirmed | Pricing page. Pro plans have no five-hour limit today. |
| F27 | partly | Right in substance. "No hosted tools" is too broad: the limits page excludes image generation, file search, Code Interpreter, computer use, hosted MCP and `tool_search`, and says web search "remains subject to model and account/workspace policy". The unsupported field list is longer than the three named. |
| F28 | confirmed | `compression.rs`: zstd level 3, 7 days; flag UnderDevelopment, default off; changelog 0.164.0 (#54008). |
| F29 | confirmed | App-server page. `thread/items/list` needs a thread store that supports item pagination. |
| F30 | confirmed | Checked: CLI and desktop app, local Work, IDE extension without plugins, cloud orchestration, the 20,000-token constant. Not checked: Codex Cloud, mobile remote and SSH. |

Sources for the table: https://learn.chatgpt.com/docs/hooks , https://learn.chatgpt.com/docs/changelog ,
https://learn.chatgpt.com/docs/whats-new , https://developers.openai.com/plugins/build/plugins ,
https://developers.openai.com/plugins/deploy/desktop-only , https://learn.chatgpt.com/docs/developer-commands ,
https://learn.chatgpt.com/docs/customization/memories , https://learn.chatgpt.com/docs/config-file/config-reference ,
https://learn.chatgpt.com/docs/non-interactive-mode , https://learn.chatgpt.com/docs/app-server ,
https://learn.chatgpt.com/docs/pricing , https://learn.chatgpt.com/docs/enterprise/managed-configuration ,
https://learn.chatgpt.com/docs/enterprise/plugin-management ,
https://developers.openai.com/siwc/token-sharing-open-source/preview-limitations ,
https://developers.openai.com/siwc/quickstart , and `github.com/openai/codex` at `rust-v0.167.0` and `1885417c`.

### Facts the notes lack

All from source at `rust-v0.167.0` (same at main) unless a docs URL is given.

1. **The trust key is positional.** Hook state is stored under
   `<hooks file path>:<event>:<group index>:<handler index>` (plugin hooks:
   `<plugin>@<marketplace>:<relative path>:<event>:<group>:<handler>`). Adding a group for the same event above an
   existing one, moving the file, or changing `timeout` or `statusMessage` sends a hook back to review. An
   installer that writes a user's `hooks.json` should add its groups after the ones already there, and another
   tool that edits the file can untrust ours. `codex-rs/hooks/src/lib.rs` (`hook_key`),
   `codex-rs/hooks/src/engine/discovery.rs`, `codex-rs/protocol/src/hooks.rs`.
2. **Nothing may write inside the plugin root.** The fingerprint covers every file but the four skipped names, so
   a `.DS_Store`, a log, a cache file or a `node_modules` change inside `PLUGIN_ROOT` untrusts the plugin's
   hooks. A symlink or other special file makes fingerprinting fail outright ("unsupported plugin entry type").
   The hash runs inside each hook's timeout, which is 1 second for `SessionEnd` and `Interrupt`. At run time a
   mismatch is a hook failure ("plugin contents changed; hook approval required"), not a silent skip.
   `PLUGIN_DATA` is `$CODEX_HOME/plugins/data/<plugin>-<marketplace>`, outside the root and the same across
   versions. `plugin_fingerprint.rs`, `plugin_trust.rs`, `command_runner.rs`, `codex-rs/core-plugins/src/store.rs`.
3. **An `mcp_tool` hook can keep a thread out of Codex's own memory.** With
   `memories.disable_on_external_context = true` (desktop: "Allow memories from tool-assisted chats" off), a
   completed `mcp_tool` hook marks the thread `polluted`, the same as a model MCP call. Command hooks do not.
   `codex-rs/core/src/hook_runtime.rs` (`emit_hook_completed_events`).
4. **A hook may leave a helper running.** When a command hook exits in time, Codex does not kill its process
   group ("Successfully completed hooks may intentionally leave detached helpers running"); only a timed-out or
   failed hook has its process tree killed. This answers the open question in section 10.
   `codex-rs/hooks/src/engine/command_runner.rs`.
5. **`--ignore-user-config` still finds `~/.codex/hooks.json` but drops its trust.** The user layer is kept, empty,
   so the hooks file next to it is still discovered, while `[hooks.state]` and the plugin settings are gone: user
   hooks are listed as untrusted and skipped, and no plugin hooks load. The flag cannot be combined with
   `--trust` or `--worktree`. This answers another open question, from reading only.
   `codex-rs/config/src/loader/mod.rs` (`load_user_config_layer`), `codex-rs/config/src/state.rs`.
6. **A hook sees the Codex process's environment.** The hook environment is a snapshot of the process environment
   taken when the session is built, less six restricted names. A variable set on a background `codex exec` run
   therefore reaches that run's hooks, which gives the layer's own summarizer runs a way to tell its capture
   hooks to stand down. Not checked: whether a session hosted by the background daemon gets the daemon's
   environment instead of the terminal's. `codex-rs/hooks/src/registry.rs`.
7. **User, project and plugin hooks do not run for Codex's memory consolidation agent**; only managed ones do.
   `codex-rs/hooks/src/engine/mod.rs` (`runs_for_memory_consolidation`).
8. **A hook's `cwd` can be a managed worktree.** The desktop app, and `codex exec --worktree`, run chats in Git
   worktrees under `$CODEX_HOME/worktrees`, on a detached HEAD. A layer that keys a project by path must go
   through git to find the real repository. https://learn.chatgpt.com/docs/environments/git-worktrees
9. **Sign in with ChatGPT plan usage has per-app limits and UI rules.** A usage limit "may apply to their ChatGPT
   plan or specifically to your app", users manage it in ChatGPT's usage settings, and an app must show "Using
   ChatGPT plan" with a Manage usage link. The quickstart also says Sign in with ChatGPT itself is in "a limited
   trial" with "selected commercial partners". The Codex app-server can run on such a token too.
   https://developers.openai.com/siwc/ui-ux-guidelines ,
   https://developers.openai.com/siwc/token-sharing-open-source/codex-app-server
10. **Two memory folders can be live at once.** During the v2 migration the desktop app "can read from
    `memories/` and write to both directories, even on a fresh profile". Anything that reads or feeds Codex's
    memory folder has to handle both. https://learn.chatgpt.com/docs/customization/memories
