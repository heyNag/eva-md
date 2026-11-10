# Claude Code and the Claude desktop app as a platform for a memory layer

Research notes, written 2026-11-10. Every fact has its source URL. Unless a date is given, the source is the live documentation as fetched on 2026-11-10; the newest Claude Code release at that time was **v2.1.321 (2026-11-09)** and the newest Claude Desktop release **v4.23118.1 (2026-11-09, bundles Claude Code 2.1.321)**.

- Changelog (version and date for every release): https://code.claude.com/docs/en/changelog.md
- Desktop changelog: https://claude.com/docs/cowork/changelog.md
- Docs index used: https://code.claude.com/docs/llms.txt and https://claude.com/docs/llms.txt and https://platform.claude.com/llms.txt

Short names used below for the pages cited most:

| Short | URL |
| :- | :- |
| HOOKS | https://code.claude.com/docs/en/hooks.md |
| MEMORY | https://code.claude.com/docs/en/memory.md |
| SESSIONS | https://code.claude.com/docs/en/sessions.md |
| HEADLESS | https://code.claude.com/docs/en/headless.md |
| CLI | https://code.claude.com/docs/en/cli-reference.md |
| CTX | https://code.claude.com/docs/en/context-window.md |
| DIR | https://code.claude.com/docs/en/claude-directory.md |
| PLUG-COMP | https://code.claude.com/docs/en/plugins/components.md |
| PLUG-MAN | https://code.claude.com/docs/en/plugins/manifest-reference.md |
| PLUG-INST | https://code.claude.com/docs/en/plugins/install.md |
| PLUG-LOAD | https://code.claude.com/docs/en/plugins/loading.md |
| PLUG-ORG | https://code.claude.com/docs/en/plugins/org.md |
| PLATFORM-SUPPORT | https://claude.com/docs/plugins/platform-support.md |
| MODS | https://code.claude.com/docs/en/plugins/mods/overview.md |
| MODS-REF | https://code.claude.com/docs/en/plugins/mods/reference.md |
| MODS-API | https://code.claude.com/docs/en/plugins/mods/api.md |
| MODS-EVENTS | https://code.claude.com/docs/en/plugins/mods/events.md |
| MODS-ADMIN | https://code.claude.com/docs/en/plugins/mods/admin.md |
| MCP | https://code.claude.com/docs/en/mcp.md |
| SKILLS | https://code.claude.com/docs/en/skills.md |
| SUBAGENTS | https://code.claude.com/docs/en/sub-agents.md |
| CLOUD-ENV | https://code.claude.com/docs/en/cloud-environments.md |
| DESKTOP | https://code.claude.com/docs/en/desktop.md |
| PLATFORMS | https://code.claude.com/docs/en/platforms.md |
| AGENT-VIEW | https://code.claude.com/docs/en/agent-view.md |
| LEGAL | https://code.claude.com/docs/en/legal-and-compliance.md |
| AUTH | https://code.claude.com/docs/en/authentication.md |
| HELP-MEMORY | https://support.claude.com/en/articles/11817273-use-claude-s-chat-search-and-memory-to-build-on-previous-context |
| HELP-SDK-PLAN | https://support.claude.com/en/articles/15036540-use-the-claude-agent-sdk-with-your-claude-plan |
| HELP-COWORK-CLOUD | https://support.claude.com/en/articles/15811196-what-to-expect-with-claude-cowork-in-the-cloud |

---

## 0. The ten findings that change the design most

1. **Plugins now have an in-process extension API called mods (v2.1.287, 2026-10-01).** A mod is JavaScript in a plugin that Claude Code calls inside its own process. It gets structured events for every conversation row (`session.append`), every prompt (`prompt.submit`, which can add Claude-only context), the first-message context (`prompt.context`), each system prompt section (`prompt.section`), compaction (`session.compact`), turn end (`turn.complete` with the final answer text), plus an API that reads the transcript as structured messages (`$.session.messages()`), calls a model on the user's own plan (`$.model.complete`, `$.model.fork`), runs timers (`$.clock.every`), registers tools and commands with no MCP server (`$.tool.register`, `$.command.register`), keeps a store shared by all sessions on the machine (`$.store`), runs processes (`$.process.run`) and draws panes, toasts and status lines. Anthropic's own AGENTS.md loader, `/diff` and its policy guard are mods. This is the closest thing to "built in" that a third party can get. (MODS, MODS-REF, MODS-API)
2. **The same plugin folder installs on three surfaces, each loading a different subset.** Claude Code (terminal, IDE extensions, Desktop Code tab) loads everything; Cowork loads skills, commands, agents, hooks, remote MCP and (when it runs on the user's computer) local MCP; Chat loads only skills, commands-as-skills and remote MCP connectors. A plugin with a top-level `bin/` directory cannot be installed in Chat or Cowork at all. (PLATFORM-SUPPORT)
3. **Claude Code has its own built-in memory that overlaps the product directly.** Auto memory is on by default (since v2.1.59, 2026-02-26): Claude writes typed notes (`user`, `feedback`, `project`, `reference`) to `~/.claude/projects/<project>/memory/`, with a `MEMORY.md` index whose first 200 lines or 25 KB load into every session and survive compaction. The directory is a setting (`autoMemoryDirectory`). (MEMORY)
4. **The apps have a separate, server-side memory.** Chat memory is saved "as a set of individual topics as you chat, rather than summarizing conversations after they end", is on by default on Free/Pro/Max/Ultra, has per-project memory, and is shared with Cowork only when Cowork runs in the cloud. It has no documented API for third parties. (HELP-MEMORY)
5. **The transcript format is officially unstable.** "The entry format is internal to Claude Code and changes between versions, so scripts that parse these files directly can break on any release." Supported alternatives: hook input fields (`prompt`, `last_assistant_message`, `tool_input`, `tool_response`, `compact_summary`), `claude -p --resume`, the Agent SDK's `listSessions()`/`getSessionMessages()`, and a mod's `$.session.messages()`/`session.append`. (SESSIONS, HOOKS, MODS-REF)
6. **Headless `claude -p` on a subscription still works and still draws from plan limits (confirmed 2026-10-07), but `--bare` cannot use the subscription and "will become the default for `-p` in a future release".** A background summarizer that shells out to `claude -p` with the user's login is on a path Anthropic has twice tried to re-bill in 2026. (HEADLESS, HELP-SDK-PLAN)
7. **Hook context injection is capped at 10,000 characters per string**, with overflow replaced by a file path and a 2,000-character preview that Claude is not asked to read. There is no setting to raise it. (HOOKS)
8. **Cloud sessions and Cowork do not read `~/.claude`.** A cloud session gets what is committed to the repository plus what is enabled on the claude.ai account; Cowork gets only what is enabled on the claude.ai account. A memory layer that lives only in the user's home folder is invisible there. (CLOUD-ENV, https://claude.com/docs/cowork/overview.md)
9. **Claude Code now runs its own background supervisor ("daemon") and scheduled tasks**, so "no daemon" is no longer a property the host itself has. (AGENT-VIEW, https://code.claude.com/docs/en/desktop-scheduled-tasks.md)
10. **Anthropic's own server-side agent memory (Managed Agents memory stores, beta) is a directory of small Markdown-like text files with immutable versions, plus "dreams" that rebuild the store from past transcripts into a new store for review.** That is the same shape as a Markdown-and-git memory, and a useful reference design. (https://platform.claude.com/docs/en/managed-agents/memory, https://platform.claude.com/docs/en/managed-agents/dreams)

---

## 1. Hooks

### 1.1 Where hooks run

- "Claude Code fires the same hook events wherever it runs: sessions in the terminal, IDE extensions, the Desktop app, and cloud sessions." (HOOKS)
- Hook locations: `~/.claude/settings.json`, `.claude/settings.json`, `.claude/settings.local.json`, managed policy settings, a plugin's `hooks/hooks.json`, skill frontmatter (registered once the skill is invoked, for the rest of the session), subagent frontmatter (only while the subagent runs). (HOOKS, "Hook locations")
- Cloud sessions do not read the local `~/.claude/settings.json`. In an Anthropic-hosted environment they run hooks from server-managed settings and, outside a project thread, from the repository. (HOOKS; CLOUD-ENV "What carries over from your setup")
- Hooks from settings, managed policy and plugins also run inside subagents; the input carries `agent_id` and `agent_type`. (HOOKS)
- Hook entries merge across levels. `disableAllHooks` in user settings turns off the user's hooks and installed mods but not managed ones. Admins can set `allowManagedHooksOnly` (blocks user, project, local and plugin hooks except plugins force-enabled in managed settings) and `strictPluginOnlyCustomization: ["hooks"]` (hooks only from plugins and managed settings). (HOOKS; https://code.claude.com/docs/en/settings-reference.md)
- Workspace trust: in an interactive session no settings-file hook runs, including the user's own, until the folder is trusted; a `-p` or SDK session treats the folder as trusted. (HOOKS, "Workspace trust")
- `$CLAUDE_CODE_REMOTE` is `"true"` in remote web environments. `CLAUDECODE=1` is set in commands and hooks Claude Code runs; `CLAUDE_CODE_CHILD_SESSION=1` since v2.1.172. (HOOKS; https://code.claude.com/docs/en/plugins/cli-hints.md)
- On macOS and Linux command hooks run in their own session with no controlling terminal; they cannot open `/dev/tty`. (HOOKS)

### 1.2 Every event that exists now (33)

Source for the whole table: HOOKS, "Hook lifecycle", "Exit code 2 behavior per event", "Decision control" and each event's section.

| Event | Fires | Can block | Can add context for Claude | Notes for a memory layer |
| :- | :- | :- | :- | :- |
| `SessionStart` | New session, resume, `/clear`, after compaction, fork. Matcher: `startup`, `resume`, `clear`, `compact`, `fork` | No | Yes: `additionalContext` or plain stdout, placed at the start of the conversation | Only `command` and `mcp_tool` handlers. Also returns `initialUserMessage` (in `-p`), `sessionTitle`, `watchPaths`, `reloadSkills`. Input has `source`, sometimes `model`, `session_title`; on resume or fork also `seconds_since_last_response`, `context_tokens`, `prompt_cache_likely_expired`, `estimated_cache_write_usd` (v2.1.251+). At interactive launch hooks run in the background; the user can type at once but "Claude's first response still waits for the hooks to finish" |
| `Setup` | Only with `--init-only`, or `--init`/`--maintenance` in `-p` | No | No (output discarded) | Not fired on normal startup |
| `InstructionsLoaded` | A CLAUDE.md or `.claude/rules/*.md` file loads (session start, lazy, include, after compact) | No | No | Observability only; does not fire for AGENTS.md read directly |
| `UserPromptSubmit` | A prompt is submitted; also on session-scoped scheduled tasks and `/loop` iterations, a background subagent reporting back, a message from another session | Yes | Yes: `additionalContext` or plain stdout, alongside the prompt | Cannot rewrite the prompt. Default timeout 30 s; on timeout the context is discarded and the prompt goes ahead. Also `sessionTitle` |
| `UserPromptExpansion` | A typed `/command` or MCP prompt expands | Yes | Yes | Input: `command_name`, `command_args`, `command_source` |
| `MessageDisplay` | While assistant text streams | No | No | Display-only replacement (`displayContent`); the transcript keeps the original. Default timeout 10 s |
| `PreToolUse` | Before a tool call | Yes | Yes | `permissionDecision` allow/deny/ask/defer, `updatedInput` |
| `PermissionRequest` | A tool call needs a permission decision | Via `decision` object | No | |
| `PermissionDenied` | Auto mode denies a call | No | `retry: true` only | |
| `PostToolUse` | After a tool call succeeds | No (already ran) | Yes | `updatedToolOutput` replaces the result. Input has `tool_input` and `tool_response` |
| `PostToolUseFailure` | After a tool call fails | No | Yes | |
| `PostToolBatch` | After a batch of parallel tool calls, before the next model call | Yes | Yes | |
| `Notification` | Claude Code sends a notification | No | No | |
| `SubagentStart` | A subagent is spawned or resumed | No | Yes: into the subagent's context at its start | |
| `SubagentStop` | A subagent finishes | Yes | Yes | Input: `agent_transcript_path` (under `<session>/subagents/`), `last_assistant_message`. Also fires for internal agents (prompt suggestions, `/btw`) with empty `agent_type` |
| `TaskCreated`, `TaskCompleted` | Task tools | Yes | No | |
| `Stop` | Main agent finishes responding (not on user interrupt) | Yes | Yes (keeps the turn going) | Input: `last_assistant_message`, `stop_hook_active`, `background_tasks`, `session_crons`. 8-consecutive-continuation cap |
| `StopFailure` | Turn ends on an API error | No | No | Output ignored |
| `TeammateIdle` | Agent-team teammate about to idle | Yes | No | |
| `ConfigChange` | A config file changes mid-session | Yes | No | |
| `CwdChanged` | Working directory changes | No | No | Has `CLAUDE_ENV_FILE` |
| `DirectoryAdded` | `/add-dir` or SDK `register_repo_root` | No | No | |
| `FileChanged` | A watched file changes on disk, whatever changed it | No | No | Watch list from matcher or `watchPaths` |
| `WorktreeCreate`, `WorktreeRemove` | Worktree lifecycle | Yes | No | |
| `PreCompact` | Before compaction (`manual` or `auto`) | Yes | **stdout becomes instructions for the compaction summary** (10,000-char cap) | A memory layer can steer what the summary keeps |
| `PostCompact` | After compaction | No | No | **Input has `compact_summary`: the generated summary** |
| `PreModelSwitch`, `PostModelSwitch` | Model changes | Pre: yes | Post: yes | |
| `Elicitation`, `ElicitationResult` | MCP server asks the user for input | Yes | No | |
| `SessionEnd` | Session ends. Reasons: `clear`, `resume`, `logout`, `prompt_input_exit`, `other` | No | No | **Default budget 1.5 s for all SessionEnd hooks together.** A per-hook `timeout` in the user's settings files raises it up to 60 s, but "Timeouts set on plugin-provided hooks don't raise the budget." `CLAUDE_CODE_SESSIONEND_HOOKS_TIMEOUT_MS` overrides |

Common input on every event: `session_id`, `prompt_id`, `transcript_path`, `cwd`, `scratchpad_dir` (v2.1.257+), `permission_mode`, `effort`, `hook_event_name`, and in subagents `agent_id`/`agent_type`. The docs warn that the transcript file "is written asynchronously and may lag the in-memory conversation", so hooks should use `last_assistant_message` instead of reading the file. (HOOKS, "Common input fields")

### 1.3 Handler types

Five types (HOOKS, "Hook handler fields"):

- `command`: shell command (shell form) or direct exec with `args` (exec form, no shell). Options: `async`, `asyncRewake`, `shell`, `onFailure` (v2.1.295+).
- `http`: POSTs the event JSON to a URL; response body uses the same JSON output. Subject to `allowedHttpHookUrls` and `httpHookAllowedEnvVars`.
- `mcp_tool`: calls a tool on a configured MCP server; for a plugin server the name is `plugin:<plugin>:<server>`. Skipped on `SessionStart` at launch and on `Setup` because MCP servers are not yet available.
- `prompt`: one model call that returns a JSON decision; default model is the one Claude Code uses for background functionality.
- `agent`: a subagent with tools, up to 50 turns; **experimental**.

Which events accept which types (HOOKS, "Prompt-based hooks"): all five on `PermissionDenied`, `PostToolBatch`, `PostToolUse`, `PostToolUseFailure`, `PreToolUse`, `Stop`, `SubagentStop`, `TaskCompleted`, `TaskCreated`, `TeammateIdle`, `UserPromptExpansion`, `UserPromptSubmit`. `SessionStart` and `Setup` accept only `command` and `mcp_tool`. `SessionEnd`, `PreCompact`, `PostCompact`, `SubagentStart` and the rest accept `command`, `http`, `mcp_tool`.

When more than 12 prompt or agent hooks match one event, 12 run at once. Matching hooks otherwise run in parallel. The same handler in several settings files runs once; a plugin's copy stays separate. (HOOKS)

### 1.4 Timeouts

Defaults (HOOKS, "Common fields"): 600 s for `command`, `http`, `mcp_tool`; 30 s for `prompt`; 60 s for `agent`. Lowered to 30 s on `UserPromptSubmit`, `PreModelSwitch`, `PostModelSwitch`; 10 s on `MessageDisplay`; `SessionEnd` hooks share 1.5 s. A timed-out hook's output is discarded and the action goes ahead unless `onFailure: "block"`. After `Ctrl+Enter` ends a turn, `PostToolUse` hooks get 10 s.

### 1.5 Output channels and limits

- Exit 0 with JSON on stdout applies the fields; exit 2 blocks on events that can block; any other exit is a non-blocking error. (HOOKS, "Exit code output")
- Plain stdout reaches Claude only on `UserPromptSubmit`, `UserPromptExpansion`, `SessionStart`, `PostModelSwitch`; on `PreCompact` it becomes compaction instructions. Elsewhere stdout goes to the debug log. (HOOKS, "Exit code 0")
- `additionalContext` is wrapped in a system reminder and inserted where the hook fired; "it doesn't appear as a chat message in the interface." On `UserPromptSubmit` "Neither channel produces a visible transcript entry." (HOOKS, "Add context for Claude", "UserPromptSubmit decision control")
- **Cap: "A hook's `additionalContext`, `systemMessage`, and `initialUserMessage` strings, and its plain stdout, are capped at 10,000 characters."** Each string is measured on its own. Over the limit, Claude Code saves the text to a file in the session directory and passes the path with a preview of the first 2,000 characters; "Claude Code doesn't ask Claude to read the file". "this cap has no setting or environment variable to raise it." (HOOKS, "JSON output")
- When several hooks return `additionalContext` for one event, Claude receives all of them. (HOOKS)
- A `<system-reminder>` tag inside the string is escaped. (HOOKS)
- Wording guidance: "Write the text as factual statements rather than imperative system instructions... Text framed as out-of-band system commands can trigger Claude's prompt-injection defenses, which causes Claude to surface the text to you instead of treating it as context." (HOOKS)
- Injected text is saved in the transcript; on resume mid-session injections are replayed, not re-run, while `SessionStart` hooks run again with `source` `resume` or `fork`. (HOOKS)
- `systemMessage` shows a warning to the user; `terminalSequence` emits allowlisted OSC sequences (titles, desktop notifications, bell) in interactive sessions only. (HOOKS)
- `suppressOutput` has no effect. (HOOKS)
- Context that hooks added earlier is "Summarized with the rest of the conversation" at compaction; `SessionStart` hooks matching `compact` run again and their output is added to the compacted context. (CTX, "What survives compaction")
- Plugin content does not invalidate the prompt cache: "It appends their content after the existing conversation". (https://code.claude.com/docs/en/prompt-caching.md)

### 1.6 Async hooks

- `async: true` (command hooks only) runs in the background with no enforced timeout; when it exits, its `additionalContext` and `systemMessage` are delivered to Claude "on the next conversation turn". Neither is shown to the user. (HOOKS, "Run hooks in the background")
- `asyncRewake: true` also wakes Claude at once when the hook exits 2, even if the session is idle; timeout is enforced. (HOOKS)
- In `-p` mode "Claude Code kills any async hook still running at teardown"; "If your hook's work must outlive a `claude -p` session, start a fully detached process from it". (HOOKS)
- An async `SessionStart` hook's unchanged output is not re-added on resume (v2.1.295+). (HOOKS)
- A non-async hook that starts a background process must redirect its stdout and stderr, or its decision is refused ("refusing to honor the suspect capture"). (HOOKS)

### 1.7 What fires for subagents, compaction, resume

- Subagents: `SubagentStart` (context injection), `SubagentStop` (with the subagent's own transcript path and final message), and all tool events with `agent_id`. The main conversation's auto memory is not loaded into subagents, except a fork. (HOOKS; MEMORY)
- Compaction: `PreCompact` → compaction → `PostCompact` (with summary) → `SessionStart` with `source: "compact"` → `InstructionsLoaded` with `load_reason: "compact"`. (HOOKS)
- Resume: `SessionStart` with `source: "resume"`; `--fork-session`, `/branch`, `/fork` and moving a conversation to the background give `source: "fork"`. `/clear` gives `SessionEnd` reason `clear` then `SessionStart` source `clear`. (HOOKS)

---

## 2. Plugins

### 2.1 What a plugin can ship

Source: PLUG-COMP, PLUG-MAN ("Fields").

| Component | Location | Notes |
| :- | :- | :- |
| Manifest | `.claude-plugin/plugin.json` | Only `name` required. `version`, `defaultEnabled`, `dependencies` (with version ranges), `userConfig`, `settings` |
| Skills | `skills/<name>/SKILL.md` | Namespaced `/plugin:skill`. Description is in context every turn unless `disable-model-invocation` |
| Commands | `commands/*.md` | |
| Agents | `agents/*.md` | Agent type is `plugin:name` |
| Hooks | `hooks/hooks.json` or `hooks` manifest key | Registered when the session loads the plugin |
| Hooks module (a mod) | `modules` key in `hooks/hooks.json` | JavaScript or TypeScript run inside Claude Code; section 3 |
| MCP servers | `.mcp.json` or `mcpServers` | Named `plugin:<plugin>:<server>`; tools `mcp__plugin_<plugin>_<server>__<tool>`. Accepts `.mcpb` bundles |
| LSP servers | `.lsp.json` | |
| Executables | `bin/` | On the Bash tool's PATH after the user's own entries. **Makes the plugin uninstallable in Chat and Cowork** |
| Default settings | `settings.json` | Only `agent` and `subagentStatusLine` take effect |
| Themes, output styles | `themes/`, `output-styles/` | |
| Channels | `channels` + an MCP server | Research preview; pushes outside events into a session |
| Monitors | `monitors/monitors.json` | A shell command that runs for the whole session; each stdout line reaches Claude as a notification. Not in `-p`; not from project-scope plugins; no `userConfig` values |
| Workflows | `workflows/*.js` | |
| Evals | `evals/` | `claude plugin eval`, baseline comparison, CI gating |

- **A plugin cannot ship a CLAUDE.md**: "Claude Code doesn't load a `CLAUDE.md` at the plugin root". Instructions go in a skill, or are injected by a hook or mod. (PLUG-COMP)
- `userConfig` prompts the user for values in a dialog; `sensitive: true` values go to secure storage; values reach hooks as `CLAUDE_PLUGIN_OPTION_<KEY>` and exec-form `${user_config.*}`. Cowork does not prompt for values. (PLUG-COMP; PLATFORM-SUPPORT)
- `${CLAUDE_PLUGIN_DATA}` resolves to `~/.claude/plugins/data/<id>/` and survives plugin updates. (PLUG-COMP)
- Setup is not automatic: `Setup` fires only with explicit flags, so "The practical pattern is to check for the dependency on first use and install on miss". (HOOKS, "Setup")

### 2.2 Per-surface support

Source: PLATFORM-SUPPORT (table "Compare component support by app"). "Chat" is claude.ai on web, desktop and mobile; "Cowork" is Cowork tasks in the desktop app; "Claude Code" is the terminal, the IDE extensions and the Desktop Code tab.

| Component | Chat | Cowork | Claude Code |
| :- | :- | :- | :- |
| Skills | Loads | Loads | Loads |
| Commands | Loads as a skill | Loads | Loads |
| Agents | Ignored | Loads | Loads |
| Hooks (`hooks/hooks.json`) | Ignored | **Loads** | Loads |
| Remote MCP (fixed URL) | Works once connected on the plugin's Connectors tab | Loads | Loads |
| Local MCP (stdio, `.mcpb`) | Ignored | Loads when the Cowork session runs on the user's computer | Loads |
| MCP server using `${user_config.*}` | Ignored when the URL has the reference | Ignored when an option has no default | Loads, prompts |
| Top-level `bin/` | **Can't be installed** | **Can't be installed** | Loads |
| LSP, output styles, themes, `settings` | Ignored | Ignored | Loads |
| Mods | Not listed | Not listed | The directory "lists a mod ... for Claude Code" |

Could not confirm: whether a hooks module (mod) runs in Cowork. MODS "Where mods run" lists the CLI, the Desktop Code tab, VS Code, `-p`/SDK, Remote Control and cloud sessions, and does not mention Cowork.

### 2.3 Install, sync and update

- Claude Code: `/plugin install name@marketplace` opens a details pane (shows what it will install, and for official-marketplace plugins a context cost estimate) and the user picks a scope: user (`~/.claude/settings.json`), project (`.claude/settings.json`), local. `claude plugin install` from the shell works too, including in `-p` environments. (PLUG-INST)
- "The terminal, the desktop app's local sessions, and the VS Code extension on one computer read the same settings files, so a plugin you install at user scope in any of them is available in the other two." (PLUG-INST)
- Chat and Cowork: Customize > Plugins on claude.ai or in the desktop app; installs attach to the account. "A plugin you install on your account also appears in Claude Code as a synced plugin at the next session start"; the reverse is not true. Upload a zip via Add > Upload plugin; limits 5,000 files and 200 MB. (PLATFORM-SUPPORT)
- Synced plugins load in terminal sessions as `<name>@synced` with "skills, agents, hooks, MCP servers, and LSP servers", same trust as a marketplace plugin (terminal sync needs v2.1.273+, 2026-09-15). They do not sync in sessions authenticated by `ANTHROPIC_AUTH_TOKEN`, `CLAUDE_CODE_OAUTH_TOKEN` or `apiKeyHelper`, in bare or safe mode, or without feature-flag fetching. `syncClaudeAiPlugins: false` turns it off. (PLUG-LOAD)
- Changelog v2.1.287 (2026-10-01): "Fixed SessionStart hooks from synced plugins not running in new cloud sessions", which implies account-synced plugins and their hooks do load in cloud sessions. No docs page states this directly. (https://code.claude.com/docs/en/changelog.md)
- Auto-update: on by default for Anthropic's official marketplaces and marketplaces added from claude.ai; **off by default for every third-party marketplace**. It runs after the first message plus a random delay of up to ten minutes; the running session keeps the old version until `/reload-plugins` or the next launch. (PLUG-INST; PLUG-LOAD)
- Repository-shared plugins (project scope) load only after the workspace trust check; their MCP servers need per-server approval and their monitors do not load. A plugin directory under `.claude/skills/` with a manifest loads as `@skills-dir`. (PLUG-LOAD)
- `--plugin-dir`, `--plugin-url`, `CLAUDE_CODE_PLUGIN_DIRS` load a plugin for one session (`@inline`). (PLUG-LOAD)
- Install prompts: a CLI can print `<claude-code-hint v="1" type="plugin" value="name@claude-plugins-official" />` to stderr when `CLAUDECODE` is set and Claude Code shows a one-time install prompt, but only for plugins in Anthropic's official marketplaces. (https://code.claude.com/docs/en/plugins/cli-hints.md)
- VS Code install link: `vscode://anthropic.claude-code/install-plugin?plugin=...&marketplace=...`. (https://code.claude.com/docs/en/vs-code.md)
- Directory: "Anyone on a paid Claude plan can submit one to Anthropic's directory for review without applying to a partner program first". (https://claude.com/docs/extend/overview.md)

### 2.4 Managed and enterprise controls

- Managed settings (server-managed from the claude.ai admin console, or MDM / `managed-settings.json`) can force-install (`extraKnownMarketplaces` + `enabledPlugins`), allowlist (`strictKnownMarketplaces`), blocklist (`blockedMarketplaces`), set update policy and release channels. (PLUG-ORG)
- On claude.ai an Owner sets each plugin to Not available, Available to install, Installed by default, or Required. A required plugin loads in Claude Code even if the user disabled it. (PLATFORM-SUPPORT; PLUG-LOAD)
- Mods: on by default. On Team or Enterprise logins, or machines with managed settings, a built-in guard (`cc-plugin-sec-default`) loads first; with it, a user's mod "can't change what your managed hooks receive or decide, the system prompt, your managed `CLAUDE.md` and other managed instructions". `allowManagedModsOnly` stops all user mods. (MODS-ADMIN)
- Zero Data Retention and HIPAA configurations exclude several features (cloud sessions, Remote Control). (LEGAL; https://code.claude.com/docs/en/zero-data-retention.md)

---

## 3. Mods (new 2026-10-01, v2.1.287)

### 3.1 What they are and where they run

- "A mod is a plugin that changes how Claude Code looks and behaves. It's made of JavaScript or TypeScript event handlers" that run "inside Claude Code". (MODS)
- Runs in: terminal (hooks and drawing), Desktop Code tab (hooks and drawing, except WSL sessions), VS Code chat panel (extension 2.1.299+), `claude -p` and the Agent SDK (hooks, no drawing), Remote Control (in the session on the user's machine), cloud sessions ("for a plugin that reaches the cloud session"; no drawing). (MODS, "Where mods run")
- The hooks module has "no Node.js APIs, no timer globals such as `setTimeout`, and no network or file access of its own"; everything goes through the `$` API. (MODS-API)
- Not sandboxed; runs with the user's permissions; "Spend your usage: call a model on your plan or API key". `claude plugin validate <dir>` lists a mod's events and calls before install. (MODS)
- Built-in mods whose source is public: `diff`, `agents-md` ("loads `AGENTS.md` as project instructions"), `sec-default`, `telemetry`. A built-in opt-in mod, You should know, is "a side agent [that] shows a note above the prompt when it finds something you might have missed". (MODS; https://github.com/anthropics/claude-code/tree/main/mods)
- Changelog v2.1.321 (2026-11-09): "Changed mods' own tool calls (`$.tool.call`, `$.mcp.call`) to raise no permission prompt, even under your own ask rules or in `dontAsk` and `-p` runs; deny rules and organization policy still apply". (https://code.claude.com/docs/en/changelog.md)

### 3.2 Events that matter for memory

Source: MODS-REF, "Events".

| Event | What a hook gets and can return |
| :- | :- |
| `session.start` | Once per loaded mod before the first prompt. Not after `/clear`, `/resume`, `/branch` |
| `session.end` | Session ends or `/clear`, `/resume`, `/branch`. Shares the 1.5 s SessionEnd budget |
| `session.append` | "Once for each row the conversation keeps, such as a prompt, a response block, a tool result, or a notice, **before it's stored**"; can rewrite the row's text or a `tool_result` content |
| `session.compact` | Before compaction; can return `{ skip: reason }` |
| `prompt.submit` | Can rewrite the prompt text, add "text only Claude reads, after the prompt" (`context`), or drop it |
| `prompt.context` | "Once for each conversation, for the context sent with the first message"; returns `{ blocks }` |
| `prompt.compose`, `prompt.section` | The system prompt's sections; a hook can change or omit a section. Blocked for user mods where the guard is loaded |
| `prompt.attachment` | Claude Code's own reminders; can rewrite or omit |
| `prompt.autocomplete` | Add suggestion rows as the user types |
| `turn.start`, `turn.step`, `turn.complete` | `turn.complete` has `e.answer` (final text), `e.durationMs`, `e.usage`, `e.isAborted`; can show a line under the answer. `turn.step` can reroute one request to another model |
| `tool.call`, `tool.check`, `tool.describe` | Observe, deny, answer or rewrite tool calls; change tool descriptions and deferral |
| `agent.spawn` | Subagent about to start |
| `skill.prompt` | Rewrite a skill's text as it expands |
| `classic.<Event>` | Every settings hook event is also visible to mods |

### 3.3 API that matters for memory

Source: MODS-REF ("Mods API methods", "Limits"), MODS-API.

- `$.session.messages()` returns the transcript "as a list of `{ role, text, toolUses }`", newest 4,096 entries. Also `$.session.id`, `cwd`, `root`, `repo`, `model`, `usage()`, `compact`, `append`, `send`.
- `$.model.complete({ model, system, prompt, maxTokens, timeoutMs })` sends one prompt to a model the mod names (for example `'haiku'`); `$.model.fork({ prompt })` appends a prompt to the current conversation, served mostly from the prompt cache; `$.model.classify`. "Both calls use the session's credentials, so they bill to the user's plan, API key, or cloud provider." `maxTokens` default 1,024, up to 64,000.
- `$.clock.every`, `$.clock.after`: timers that run between turns without starting one.
- `$.prompt.submit({ text })` starts a turn when the session is idle; `asUser: true` sends it as the user's words.
- `$.tool.register`, `$.command.register` (a command runs "at once, with no Claude turn, even while Claude is working"), `$.agent.register/spawn`.
- `$.store`: "A key-value store that every session on the machine shares", 4 MiB of JSON in total. `$.state`: reactive per-session state.
- `$.fs.read/write/list/exists/stat` (4 MiB per file, write is not atomic), `$.process.run` (no shell, 30 s default, 10 min max, 4 MiB output), `$.process.spawn`, `$.http.fetch` (30 s, 4 MiB), `$.mcp.call/connect`, `$.env`, `$.settings.read`.
- `$.ui`: panes, a band above the prompt, `toast`, `notify`, `status`, `log`, `notice`, `ask`, `place` (a row in the transcript).
- Limits: a hook's own execution time is 10 s per event (not counting time inside `next` or an API call), 50 ms for `prompt.edit`; "Claude Code skips a hook that exceeds a time limit".
- In `-p` and SDK runs, calls can be refused once `--max-turns` or `--max-budget-usd` is reached.

---

## 4. Built-in memory

### 4.1 CLAUDE.md, imports, rules

Source: MEMORY unless noted.

- Scopes, in load order: managed policy (`/Library/Application Support/ClaudeCode/CLAUDE.md` on macOS), user (`~/.claude/CLAUDE.md`), project (`./CLAUDE.md` or `./.claude/CLAUDE.md`), local (`./CLAUDE.local.md`). Files from the working directory and every directory above load at launch and are concatenated; subdirectory files load when Claude reads or edits a file there.
- Imports: `@path/to/file`, relative to the importing file, recursive to four hops, skipped inside code spans and fences. Imports do not reduce context cost. An import in a project file that resolves outside the working directory needs a one-time approval dialog; user-scope imports do not, except in Cowork sessions, which skip user-scope imports that point outside the session's working directory.
- Size: a CLAUDE.md that loads at launch is given whole up to 4 MiB; guidance is under 200 lines per file. Block-level HTML comments are stripped before injection.
- Rules: `.claude/rules/**/*.md` and `~/.claude/rules/`; unscoped rules load at launch; `paths:` frontmatter (the only field read) makes a rule load when Claude works with matching files. Symlinks are supported; a symlink out of the working directory is treated as an external import.
- AGENTS.md: read directly since v2.1.277 (2026-09-18) when no CLAUDE.md, `.claude/CLAUDE.md` or CLAUDE.local.md exists at or above the working directory. Setting `instructionFiles` (`claude-md-or-agents-md` default, `claude-md-and-agents-md`, `claude-md`, `managed-only`) lives under `pluginConfigs["cc-plugin-agents-md@builtin"]`.
- "Claude treats them as context, not enforced configuration."
- After compaction: project-root CLAUDE.md and unscoped rules are re-injected from disk; path-scoped rules and nested CLAUDE.md reload on demand. (CTX)
- `claudeMdExcludes` skips files; managed `claudeMd` injects organization instructions. (https://code.claude.com/docs/en/settings-reference.md)
- `/doctor prompt-audit` (v2.1.283+) checks instruction files for outdated or conflicting content.
- CLAUDE.md edits do not apply mid-session (they are part of the cached prefix). (https://code.claude.com/docs/en/prompt-caching.md)

### 4.2 Auto memory

Source: MEMORY, "Auto memory".

- On by default in local sessions (changelog v2.1.59, 2026-02-26: "Claude automatically saves useful context to auto-memory"). Off by default in self-hosted environments; cannot be turned on from a background session or a session another Claude Code session started. Off in `--bare` and `--safe-mode`.
- What it saves: four typed kinds, recorded as a `type` frontmatter field: `user`, `feedback`, `project` ("ongoing work, deadlines, and decisions that Claude can't derive from the code or git history"), `reference`. "Claude skips anything it can derive from the codebase... It also skips anything your CLAUDE.md files already say."
- Where: `~/.claude/projects/<project>/memory/`, with `<project>` derived from the git repository so all worktrees and subdirectories share one directory. Layout: `MEMORY.md` index ("one line per memory") plus one topic file per memory. Claude Code stamps a `modified` ISO 8601 frontmatter field on write.
- How much loads: "The first 200 lines of `MEMORY.md`, or the first 25KB, whichever comes first, are loaded at the start of every conversation." Topic files are read on demand with the normal file tools. Over the limit, the write succeeds but Claude gets an error telling it to rewrite the index.
- Re-injected from disk after compaction. (CTX)
- Not loaded into subagents (except forks). A subagent can have its own memory through the `memory` frontmatter field (`user` → `~/.claude/agent-memory/<agent>/`, `project` → `.claude/agent-memory/<agent>/`, `local`), with the same 200-line / 25 KB index rule. (SUBAGENTS, "Enable persistent memory")
- "Auto memory is machine-local... Files are not shared across machines or cloud environments." Memory files are excluded from the 30-day transcript retention sweep.
- Settings: `autoMemoryEnabled`, **`autoMemoryDirectory`** (any settings scope; absolute or `~/` path; from v2.1.74, 2026-03-12), `CLAUDE_CODE_DISABLE_AUTO_MEMORY`. A directory chosen by a repository's settings needs workspace trust.
- UI: `/memory` lists and opens the files and toggles auto memory; the interface shows "Saved 2 memories" / "Recalled 2 memories"; VS Code has a Memory entry under Customize (v2.1.274+). (MEMORY; https://code.claude.com/docs/en/vs-code.md)
- Hardening (changelog v2.1.284, 2026-09-28): "invisible characters and tags that imitate Claude Code's own markup are neutralized in `MEMORY.md` and recalled memory notes before they reach Claude". (https://code.claude.com/docs/en/changelog.md)
- The system prompt embeds the auto memory path, which scopes the prompt cache to one machine and directory. (https://code.claude.com/docs/en/prompt-caching.md)

### 4.3 Cloud project memory (Claude Code projects)

- A Claude Code project (many cloud threads under one conversation) has "Project memory: Notes Claude keeps about the project, such as requirements, decisions, and pitfalls, stored as files. Every cloud thread reads the index file `MEMORY.md` when it starts and opens the other files when it needs them". It is separate from local auto memory and from CLAUDE.md. Project instructions are up to 16,000 characters. (https://code.claude.com/docs/en/claude-projects.md)

### 4.4 Memory in the Claude apps

Source: HELP-MEMORY ("Updated this week" as of 2026-11-10).

- Search past chats: paid plans, on web, desktop and mobile; RAG; shown as tool calls; scoped to all non-project chats, or within one project.
- Memory: "Claude saves memory as a set of individual topics as you chat, rather than summarizing conversations after they end." On by default on Free, Pro, Max, Ultra; on Team and Enterprise owners control it, and in organizations created on or after 2026-10-31 it starts on.
- Project memory: each project has its own; a project also uses account memory unless "Use account memory" is off.
- "Memory is shared between Chat and Claude Cowork when Cowork runs in the cloud; Cowork sessions that run locally on your computer don't use memory."
- Controls: view, edit and delete topics in Settings > Your Claude; pause or reset; turn memory off for one chat before the first message; incognito chats. Sensitive topics are off by default; some data (government IDs, financial account numbers, immigration status, criminal history) is never saved.
- Import and export of memory between Claude and other AI tools is "experimental".
- No API for third parties to read or write this memory was found in the help center, claude.com docs or platform docs. Could not confirm that none exists.

Cowork projects (local): "A Cowork project collects ... the local folders to read and write, standing instructions, useful links, and a dedicated memory store"; "what Claude learns during the session is saved to the project's memory for next time". "Projects live on your computer. They aren't synced to the cloud". (https://claude.com/docs/cowork/guide/projects.md)

### 4.5 API memory tool and context editing

- Memory tool: **stable**, type `memory_20250818`, name `memory`, no beta header. Client-side: Claude requests file operations (`view`, `create`, `str_replace`, `insert`, `delete`, `rename`) under `/memories` and the application runs them against its own storage. "When the memory tool is enabled, Claude automatically checks its memory directory before starting a task." SDK helpers exist for Python, TypeScript, C#, Java; none for Go or Ruby. (https://platform.claude.com/docs/en/agents-and-tools/tool-use/memory-tool)
- Context editing: beta header `context-management-2025-06-27`; strategies `clear_tool_uses_20250919` and `clear_thinking_20251015`. With the memory tool, "Claude receives an automatic warning to preserve important information" before results are cleared. Server-side compaction is "the primary strategy" for long conversations. (https://platform.claude.com/docs/en/build-with-claude/context-editing)
- Managed Agents memory stores (beta, header `agent-memory-2026-07-22`): "a workspace-scoped collection of text documents", mounted as a directory under `/mnt/memory/` and read and written "with the same file tools"; each memory capped at 100 kB, 10,000 memories per store, up to 8 stores per session, `read_only` or `read_write`; "Every change to a memory creates an immutable memory version"; versions can be redacted. Guidance: "Structure memory as many small focused files, not a few large ones." Warning: a prompt injection can write into a read-write store and "Later sessions then read that content as trusted memory." (https://platform.claude.com/docs/en/managed-agents/memory)
- Dreams (research preview, header `dreaming-2026-04-21`): an asynchronous job that reads a memory store and 1 to 100 past session transcripts and "produces a new, reorganized memory store: duplicates merged, stale or contradicted entries replaced with the latest value, and new insights surfaced. The input store is never modified, so you can review the output and discard it". (https://platform.claude.com/docs/en/managed-agents/dreams)

### 4.6 How a third-party layer can coexist with or feed these

- Feed CLAUDE.md: a user-scope file such as `~/.claude/rules/<name>.md` or an `@~/...` import from `~/.claude/CLAUDE.md` loads at launch with no dialog, is re-injected after compaction, and costs no hook. Limits: read at session start only, no per-prompt selection, not in cloud sessions or Cowork. (MEMORY; CLOUD-ENV)
- Path-scoped rules give free, host-native recall keyed on file paths: a generated rule with `paths:` loads when Claude touches matching files. (MEMORY)
- Point auto memory at the wiki with `autoMemoryDirectory`, or read the auto memory directory as an input to consolidation. Both are plain Markdown with frontmatter. Risk: the format of auto memory files is Claude's own and not a documented contract. (MEMORY)
- `InstructionsLoaded` tells a hook which instruction files loaded and why. (HOOKS)
- Skills: account-synced skills reach Chat, Cowork, cloud sessions and the terminal. (SKILLS)
- A remote MCP connector is the only component that reaches Chat on web and mobile. (PLATFORM-SUPPORT)

---

## 5. Transcripts and sessions

- Location: `~/.claude/projects/<project>/<session-id>.jsonl`, `<project>` being the working directory path with non-alphanumeric characters replaced by `-` (truncated to 200 characters plus a hash when longer). Subagent transcripts are in `<session>/subagents/`, large tool outputs in `<session>/tool-results/`. (SESSIONS; DIR)
- Content: "a message, a tool use or result, a record of a request Claude Code sent the model, or a metadata entry", including "the system prompt, tool definitions, and parameters of the requests". (SESSIONS; DIR)
- **Stability: "The entry format is internal to Claude Code and changes between versions, so scripts that parse these files directly can break on any release. To build on session data, use `/export` or the script interfaces instead."** (SESSIONS)
- Script interfaces named by the docs: `claude -p --output-format json|stream-json`; `claude -p --resume <session-id> "..."`; the `transcript_path` hook field ("A `SessionEnd` hook can archive the transcript"); the Agent SDK. (SESSIONS)
- Agent SDK: `listSessions()` and `getSessionMessages()` (TypeScript), `list_sessions()` and `get_session_messages()` (Python); a `SessionStore` adapter mirrors transcript entries to any backend as they are appended. (https://code.claude.com/docs/en/agent-sdk/sessions.md; https://code.claude.com/docs/en/agent-sdk/session-storage.md)
- Retention: transcripts are deleted after `cleanupPeriodDays`, default 30, minimum 1. `desktopSessionCleanupPeriodDays` for Desktop and Cowork transcripts. `CLAUDE_CODE_SKIP_PROMPT_HISTORY` and `--no-session-persistence` suppress writes. (SESSIONS; DIR)
- `CLAUDE_CONFIG_DIR` moves storage; `CLAUDE_CODE_PROJECT_DIR_NAME` names the project directory (v2.1.234+). (SESSIONS)
- Resume: `--continue`, `--resume [name|id|path]`, `--from-pr`, `/resume`. A resumed compacted session continues from the last summary plus later messages. Sessions created with `-p` or the SDK are left out of the picker and `--continue` but resume by ID. (SESSIONS)
- Fork: `/branch [name]` copies the transcript in the same process; `--fork-session` with `--resume` or `--continue` makes a new session ID. Two terminals resuming one session without forking interleave into one transcript. (SESSIONS)
- `/clear` saves the previous session and starts a new one; `/cd` moves a session to another directory's project storage. (SESSIONS)
- Teleport: `claude --teleport [id]` or `/teleport` pulls a cloud session into the terminal (one-way from the CLI); it needs a clean checkout of the same repository; the terminal gets its own copy. Desktop can move a local session to the cloud "with your conversation carried over as a summary". `/desktop` and `claude --desktop --resume <id>` move a CLI session into Desktop, continuing the same session. (https://code.claude.com/docs/en/claude-code-on-the-web.md; DESKTOP)
- Checkpoints: every prompt that starts a turn creates one; file snapshots for the 100 most recent; `/rewind` restores code, conversation or both, or summarizes from or up to a point. "the original messages stay in the session transcript". Only edits by the file tools are tracked. (https://code.claude.com/docs/en/checkpointing.md)
- Compaction keeps: system prompt and output style; project-root CLAUDE.md and unscoped rules (re-injected); auto memory (re-injected); skill descriptions; the plan; up to five recently modified files re-read (over 5,000 tokens → path only); invoked skill bodies (5,000 tokens each, 25,000 total); a reminder of running background work. It summarizes hook-added context and path-scoped rules with the rest. Idle compaction also exists (`idleCompaction`). (CTX)

---

## 6. Headless runs for a background summarizer

### 6.1 `claude -p`

Source: HEADLESS, CLI.

- Model: `--model` with an alias (`sonnet`, `opus`, `haiku`, `fable`) or a full name; `--fallback-model a,b`; `--effort low|medium|high|xhigh|max`.
- Structured output: `--output-format json` with `--json-schema '<schema>'`; the result is in `structured_output`; "If a run ends without valid structured output... the response has no `structured_output` field." `format` keywords are not enforced.
- Turning tools off: `--tools ""` disables all built-in tools (it "doesn't affect MCP tools; to deny those too, use `--disallowedTools "mcp__*"`"); `--strict-mcp-config` with no `--mcp-config` drops MCP servers; `--disable-slash-commands` drops skills and commands.
- System prompt: `--system-prompt[-file]` replaces, `--append-system-prompt[-file]` appends.
- Bounds: `--max-turns`, `--max-budget-usd` (client-side estimate), `--no-session-persistence`, `--session-id <uuid>`, `--setting-sources user,project,local`, `--settings <file-or-json>` (for example `'{"disableAllHooks": true}'`).
- Without `--bare`, a `-p` run "discovers and loads" hooks, skills, plugins, MCP servers, auto memory and CLAUDE.md "as an interactive session would", and treats the folder as trusted. So a summarizer's own run fires the layer's own hooks unless it turns them off.
- `--bare` (v2.1.81, 2026-03-20) skips all of that and gives only Bash, file read and file edit. **"In bare mode, Claude Code never reads OAuth credentials or the system keychain"**: it needs `ANTHROPIC_API_KEY` or an `apiKeyHelper`. **"`--bare` is the recommended mode for scripted and SDK calls, and will become the default for `-p` in a future release."**
- `--safe-mode` disables customizations but keeps authentication. (CLI)
- Sessions made by `-p` are hidden from the picker and from `--continue`. (SESSIONS)
- SIGTERM exits 143 and runs only `SessionEnd` hooks. A `-p` run can wait up to 10 minutes for background work it started. (HEADLESS)
- `claude --bg "<prompt>"` starts a background session under the supervisor and returns at once; `--bg --exec` runs a shell command as a background job; neither combines with `-p`. (CLI; AGENT-VIEW)

### 6.2 Which limits and costs apply

- "Update October 7, 2026: Claude Max, Ultra, and Team plans now include monthly API credits. They cover the Claude API, Claude Managed Agents, and the Claude Agent SDK and claude -p when you run them with an API key from your linked Claude Console organization. Nothing changes for your plan: when you're signed in with your Claude plan, the Claude Agent SDK and claude -p still draw from your subscription limits." (HELP-SDK-PLAN)
- "Update June 15, 2026: We've paused the previously-announced changes to Claude Agent SDK usage. For now, nothing has changed". (HELP-SDK-PLAN) Secondary coverage says the paused plan would have moved programmatic usage to a separate monthly credit: https://dev.to/max_quimby/anthropic-walked-back-the-agent-sdk-credit-change-103a (not a primary source; low confidence on the details).
- "Advertised usage limits for Pro, Max, and Ultra plans assume ordinary, individual usage of Claude Code and the Agent SDK." (LEGAL)
- "Anthropic does not permit third-party developers to offer Claude.ai login into their own applications, or to route requests through Free, Pro, Max, or Ultra plan credentials on behalf of their users... Nor does it prevent an end user from signing in to the unmodified Claude Code binary with their own Claude subscription". "Anthropic reserves the right to take measures to enforce these restrictions and may do so without prior notice." (LEGAL)
- Agent SDK note: "Unless previously approved, Anthropic does not allow third party developers to offer claude.ai login or rate limits for their products, including agents built on the Claude Agent SDK." (https://code.claude.com/docs/en/agent-sdk/overview.md)
- `claude setup-token` makes a one-year OAuth token (`CLAUDE_CODE_OAUTH_TOKEN`) that "authenticates with your Claude subscription"; sessions on it do not sync plugins or skills. (AUTH; PLUG-LOAD)
- Usage credits let a subscriber continue past plan limits at extra cost; while on usage credits the prompt cache drops to the 5-minute TTL. (https://code.claude.com/docs/en/costs.md; https://code.claude.com/docs/en/agent-sdk/cost-tracking.md)
- `/usage` attributes plan usage to skills, subagents, plugins and MCP servers, so a plugin's cost is visible to the user. (https://code.claude.com/docs/en/whats-new/2026-w21.md)

### 6.3 Other ways to run a model in the background

- A mod's `$.model.complete` or `$.model.fork`, on the session's credentials, with timers. Runs only while a session is open. (MODS-API)
- `prompt` and `agent` hook handlers on `Stop`, `SubagentStop`, `UserPromptSubmit`, `PostToolUse` and others; default model is the background-functionality model. (HOOKS)
- A skill with `context: fork` runs in a forked subagent, in the background by default; `model:` and `effort:` set its model. (SKILLS)
- A plugin agent with `memory:` and a `Stop`/`SubagentStop` flow; subagents run in the background by default since mid-2026. (SUBAGENTS; https://code.claude.com/docs/en/whats-new/2026-w27.md)
- Desktop local scheduled tasks: start a new local session on a schedule, with the user's files, skills and config; "only fires while the app is open and your computer is awake"; minimum interval 1 minute when Claude sets it, 1 hour in the forms. (https://code.claude.com/docs/en/desktop-scheduled-tasks.md)
- Cloud scheduled tasks (formerly routines, research preview): prompt + repositories + connectors, on a schedule (minimum 1 hour), an API call or a GitHub event; fresh clone, no local files. (https://code.claude.com/docs/en/routines.md)
- `/loop` and the cron tools inside a session. (https://code.claude.com/docs/en/scheduled-tasks.md)
- Background sessions under the supervisor (`claude --bg`, `claude agents`, `claude daemon status|logs|stop`): "A separate supervisor process runs them"; it stops idle session processes after about an hour and restarts itself after auto-updates. Auto memory cannot be turned on from a background session. (AGENT-VIEW; MEMORY)
- Agent SDK (Python, TypeScript): the same loop as a library; third-party products must use API keys. (https://code.claude.com/docs/en/agent-sdk/overview.md)

---

## 7. Surfaces

| Surface | Runs where | Reads `~/.claude` | Hooks | Plugins | Mods | Local MCP | Notes | Source |
| :- | :- | :- | :- | :- | :- | :- | :- | :- |
| CLI | User's machine | Yes | Yes | Yes | Yes | Yes | "The CLI is the most complete surface"; scripting and SDK are CLI-only | PLATFORMS |
| Desktop Code tab, local session | User's machine (bundled Claude Code) | Yes: "Desktop and CLI read the same configuration files" | Yes | Yes (plugin manager UI) | Yes, drawing included | Yes, also from `claude_desktop_config.json` | No `-p`; no agent teams; not in WSL sessions (no plugins) | DESKTOP |
| Desktop Code tab, SSH session | Remote machine | The remote's | Yes (remote's settings) | Yes; Desktop copies plugins from the user's computer to `~/.claude/remote/plugins/` | Not stated | Yes | | DESKTOP |
| Desktop Code tab, cloud session | Anthropic VM | No | Repository hooks and server-managed hooks | Not the user's; not those the repository declares | Only if the plugin reaches the session | Repository `.mcp.json` only | | CLOUD-ENV; DESKTOP |
| VS Code extension | User's machine | Yes | Yes | Yes (Manage plugins UI) | Yes (2.1.299+) | Yes | Has Hooks, Memory and Instructions entries under Customize | https://code.claude.com/docs/en/vs-code.md |
| JetBrains | User's machine: "runs the `claude` command in your IDE's integrated terminal" | Yes | Yes | Yes (terminal steps) | Yes | Yes | | https://code.claude.com/docs/en/jetbrains.md |
| Claude Code on the web, cloud sessions | Anthropic VM, fresh clone | No | Repository `.claude/settings.json` hooks (one-repository session), server-managed hooks | User-scope plugins no; repository-declared plugins no; account-synced skills yes; account-synced plugins appear to (changelog) | See left | Repository `.mcp.json` | `CLAUDE_CODE_REMOTE=true`. Setup script can write `~/.claude/CLAUDE.md` | CLOUD-ENV |
| Cloud project threads | Anthropic VM | No | No repository hooks | Account skills only | | | Load only root CLAUDE.md and unscoped rules, plus project memory | https://code.claude.com/docs/en/claude-projects.md |
| Remote Control | The local session, driven from web or phone | Yes | Yes | Yes | Yes, in the local session | Yes | | https://code.claude.com/docs/en/remote-control.md |
| Mobile | Thin client to cloud sessions, Remote Control, Dispatch | n/a | As the target | As the target | | | "a client for Claude Code sessions rather than a place where code runs" | https://code.claude.com/docs/en/mobile.md |
| Cowork, local | User's machine, in a VM (a "Claude VM Service" exists on Windows) | **No**: "doesn't read the Claude Code CLI's `~/.claude` directory" | Plugin hooks load | Account plugins | Not stated | Yes | No account memory; Cowork projects have their own local memory store | https://claude.com/docs/cowork/overview.md; PLATFORM-SUPPORT; HELP-COWORK-CLOUD |
| Cowork, cloud | Anthropic servers | No | Plugin hooks load (per the Cowork column) | Account plugins | Not stated | Only while the desktop app is open and connected | Shares memory with Chat. In the new unified experience "tasks run in the cloud, and there's no option to run them locally" | HELP-COWORK-CLOUD |
| Chat (web, desktop, mobile) | Anthropic servers | No | **No** | Skills, commands as skills, remote connectors only | No | Desktop app only: desktop extensions (`.mcpb`) and `claude_desktop_config.json` | Built-in memory and chat search | PLATFORM-SUPPORT; https://claude.com/docs/connectors/custom/add-unlisted.md |
| Slack / Claude Tag | Anthropic cloud | No | | Admin-configured | | | Claude Tag keeps per-channel memory | https://claude.com/docs/claude-tag/users/memory.md |

Chat tab extension points:

- MCP connectors: remote servers over Streamable HTTP (HTTP+SSE "being deprecated"), with OAuth, a static header, or no auth; reachable "from claude.ai, Claude Desktop, Claude mobile, Cowork, and Claude Code". Claude's client supports tools, prompts, resources, text and image results; it does **not** support resource subscriptions or sampling. Limits on hosted surfaces: about 150,000 characters per tool result and 240 s per call (Claude Code: 25,000 tokens, configurable). All tools must declare `readOnlyHint` and `destructiveHint`. (https://claude.com/docs/connectors/building/index.md; https://claude.com/docs/connectors/building/mcp.md)
- Desktop extensions (MCPB): a zip of a local stdio MCP server and `manifest.json`, one-click install in Claude Desktop on macOS and Windows; Node.js ships with the app. Organizations can turn extensions off or allowlist them. (https://claude.com/docs/connectors/building/mcpb.md)
- Skills: account skills load in Chat, Cowork and Claude Code. (https://claude.com/docs/skills/overview.md)
- MCP Apps: interactive UI rendered in the conversation from a `ui://` resource. (https://claude.com/docs/connectors/building/mcp-apps/getting-started.md)
- MCP tunnels reach servers inside a private network. (https://claude.com/docs/connectors/mcp-tunnels/overview.md)

---

## 8. Skills and subagents as ways to recall; MCP features

### 8.1 Skills

Source: SKILLS.

- A skill's name and description are in context every turn (budget: 1% of the context window for the whole listing; each entry capped at 1,536 characters; least-used descriptions dropped first). The body loads when invoked and stays in the conversation.
- Frontmatter useful for recall: `description`, `when_to_use`, `paths` (auto-load only when working with matching files), `user-invocable: false` (Claude-only background knowledge), `disable-model-invocation`, `context: fork` + `agent` + `model` + `effort` + `background`, `allowed-tools`, `hooks` (registered when the skill is invoked).
- Dynamic context: `` !`command` `` runs a shell command before the skill text reaches Claude and substitutes the output, so a recall skill can call a binary and hand Claude the result in one step. Claude Code does not run these for skills synced from claude.ai (v2.1.228+).
- After compaction the most recent invocation of each skill is re-attached, first 5,000 tokens each, 25,000 total.
- Account-synced skills download to `~/.claude/skills/synced/` and update in a running session (checks about every 10 minutes while active).
- Follows the open Agent Skills spec (https://agentskills.io).

### 8.2 Subagents

Source: SUBAGENTS.

- Fresh context; gets the CLAUDE.md hierarchy (Explore and Plan skip it), a git status snapshot, preloaded skills (`skills:` field), and whatever `SubagentStart` hooks inject. Returns only a summary.
- `memory: user|project|local` gives a persistent directory with its own `MEMORY.md`; `omitClaudeMd`; `model`; `background`.
- A plugin can set `agent` in its default settings to run one of its agents as the main thread (user settings override). (PLUG-COMP)
- A recall subagent keeps large reads out of the main context, at the cost of one model call and a delay.

### 8.3 MCP

Source: MCP.

- Tool search (deferred loading): "Only tool names and server instructions load at session start". Limits: server instructions and upfront tool descriptions 4,096 characters; deferred tool descriptions 16,384. `anthropic/alwaysLoad` in a tool's `_meta`, or `alwaysLoad: true` on the server entry, loads tools upfront; `alwaysLoad: true` makes startup wait up to 5 s for the server.
- Server instructions "help Claude understand when to search for your tools, similar to how skills work".
- Resources: referenced as `@server:protocol://path`, "automatically fetched and included as attachments when referenced", fuzzy-searchable in the `@` menu; Claude Code provides tools to list and read them.
- Prompts: appear as `/server:prompt (MCP)` commands; `UserPromptExpansion` sees them as `mcp_prompt`.
- Elicitation is supported (form and URL modes), with hooks. Channels push events into a session (research preview). MCP tasks run on the server and survive `/clear` and resume.
- `MAX_MCP_OUTPUT_TOKENS` (25,000 default), per-tool overrides.
- Plugin MCP servers start with each session that loads the plugin; `${CLAUDE_PROJECT_DIR}` is set in stdio servers' environment.
- `claude mcp serve` exposes Claude Code itself as an MCP server.

---

## 9. What is new in 2026 that a memory layer could use to feel built in

Dates are release dates from https://code.claude.com/docs/en/changelog.md unless noted.

| When | What | Use |
| :- | :- | :- |
| 2026-02-18 (v2.1.47) | `last_assistant_message` on `Stop`/`SubagentStop` | Capture the answer without reading the transcript |
| 2026-02-26 (v2.1.59) | Auto memory on by default | Coexist: do not duplicate what it saves; possibly read it |
| 2026-02-28 (v2.1.63) | HTTP hooks | A local service could receive events without a process spawn |
| 2026-03-05 (v2.1.69) | `InstructionsLoaded` | Know which instruction files Claude has |
| 2026-03-12 (v2.1.74) | `autoMemoryDirectory` | Auto memory can live inside or beside the wiki |
| 2026-03-14 (v2.1.76) | `PostCompact` with `compact_summary`; `PreCompact` stdout as summary instructions | A free, model-written summary of every compacted span; steer what survives |
| 2026-03-20 (v2.1.81) | `--bare` | Faster, isolated headless runs, but API key only |
| 2026-04 (w16) | Cloud scheduled tasks (routines); native binaries | |
| 2026-04-13 (v2.1.105) | Plugin monitors | A per-session background process whose output Claude sees |
| 2026-04-23 (v2.1.118) | `mcp_tool` hooks | A hook can call the plugin's own long-lived MCP server instead of spawning a binary |
| 2026-05-11 (v2.1.139) | Agent view, background sessions, supervisor | The host has a daemon |
| 2026-06-04 (v2.1.163) | `Stop` hooks can return `additionalContext` | Non-error feedback at turn end |
| 2026-06-15 | Agent SDK billing change paused | Subscription-backed `claude -p` survives for now (HELP-SDK-PLAN) |
| 2026-08-21 to 09-15 (v2.1.239 to 2.1.273) | Plugins and skills synced from the claude.ai account to the terminal | One install reaches Chat, Cowork, terminal, cloud |
| 2026-08-28 (v2.1.251) | Resume fields on `SessionStart` (`seconds_since_last_response`, `prompt_cache_likely_expired`) | Tailor the brief to how stale the session is |
| 2026-09-01 (v2.1.257) | `scratchpad_dir` in hook input | A per-session place for spill files |
| 2026-09-18 (v2.1.277) | AGENTS.md read directly | One instruction file serves Claude Code and Codex |
| 2026-10-01 (v2.1.287) | **Mods**; `asyncRewake` | In-process capture, recall, model calls, UI |
| 2026-10-07 | Monthly API credits on Max, Ultra, Team for API-key use of `claude -p` and the SDK | A sanctioned budget for a `--bare` summarizer on those plans (HELP-SDK-PLAN) |
| 2026-10-08 (v2.1.295) | `onFailure`; async hook JSON on multiple lines | |
| 2026-10-31 | Chat memory starts on in new Team and Enterprise organizations | The apps' own memory is becoming universal (HELP-MEMORY) |
| 2026 | Cowork, Cowork in the cloud, the unified Chat + Cowork experience, memory shared across them | Consumer desktop use is moving to cloud execution (HELP-COWORK-CLOUD) |
| 2026 | Plugin directory open to any paid plan; `claude plugin eval` | Distribution and a quality gate (https://claude.com/docs/extend/overview.md; https://code.claude.com/docs/en/plugin-evals.md) |
| 2026 | Managed Agents memory stores (beta) and dreams (research preview) | Reference design for file-based memory with versions and offline consolidation |

Concrete "feels built in" moves these make possible:

1. The plugin as the only install unit: marketplace install, auto-update, account sync, org rollout, no settings-file edits by the product.
2. A mod that registers a recall tool, shows "recalled N" as a toast or status line like the host's own "Recalled 2 memories", and adds first-message context through `prompt.context` in the same way Anthropic's `agents-md` mod adds instructions.
3. Capture from `session.append` and `turn.complete` (structured, pre-storage) instead of parsing JSONL.
4. Summarize with `$.model.complete` on a small model during or right after the session, billed to the same plan the user already sees in `/usage`, with no second process, login or API key.
5. `PreCompact` instructions plus `PostCompact` summary capture so nothing is lost at compaction, and a `SessionStart` `compact` hook to restore the brief.
6. Generated `paths:` rules and a user-scope rules file for zero-latency recall of standing rules and constraints.
7. A remote MCP connector plus skills in the same plugin so the memory is also readable from Chat, mobile and Cowork.

---

## 10. What stays impossible or closed

- **No hooks, mods, local processes or local files in Chat on web and mobile.** Only skills and remote MCP connectors. Capture of a chat conversation is not possible from outside; recall there needs a hosted server. (PLATFORM-SUPPORT)
- **No third-party access to the apps' built-in memory** (topics, project memory, chat search): no API found. (HELP-MEMORY; not confirmed absent)
- **No way to rewrite the user's prompt from a settings hook**; only a mod can. (HOOKS, "UserPromptSubmit: can't replace the prompt")
- **No way to raise the 10,000-character hook context cap.** (HOOKS)
- **No stable transcript file format.** (SESSIONS)
- **No model call or long work at session end from a plugin hook**: 1.5 s budget that plugin timeouts cannot raise; mods share it. Work must be detached or deferred to the next session start. (HOOKS; MODS-REF)
- **A plugin cannot ship CLAUDE.md content or arbitrary settings**; only `agent` and `subagentStatusLine` defaults. It cannot set `autoMemoryDirectory` or add user rules by declaration. (PLUG-COMP)
- **A plugin with `bin/` cannot install on Chat or Cowork**, so one cross-surface plugin cannot carry a native binary in the standard place. (PLATFORM-SUPPORT)
- **User-level configuration never reaches cloud sessions or Cowork.** (CLOUD-ENV)
- **Third-party marketplaces do not auto-update by default**; the user must enable it per marketplace, or an admin must. Install hints work only for official-marketplace plugins. (PLUG-INST; https://code.claude.com/docs/en/plugins/cli-hints.md)
- **A user mod cannot change the system prompt on Team and Enterprise logins** (guard), and admins can turn user mods or all non-managed hooks off. (MODS-ADMIN)
- **MCP sampling is not supported** by Claude's client, so an MCP server cannot borrow the host's model. (https://claude.com/docs/connectors/building/index.md)
- **A third-party product cannot offer claude.ai login or route requests through a user's plan credentials itself**; it can only have the user run the unmodified Claude Code binary. (LEGAL)
- **SessionStart cannot use `http`, `prompt` or `agent` handlers**, and `mcp_tool` handlers are skipped at launch. (HOOKS)
- **Hooks cannot draw UI or talk to the terminal**; only `systemMessage`, `terminalSequence` and (for mods) the `$.ui` API. (HOOKS)
- **Local scheduled tasks need the desktop app open and the computer awake**; cloud ones have no local files. (https://code.claude.com/docs/en/desktop-scheduled-tasks.md)

---

## 11. Things I could not confirm

1. Whether hooks modules (mods) load in Cowork sessions. The platform-support table has no row for them and MODS does not list Cowork.
2. Whether account-synced plugins (not only skills) load in cloud sessions with their hooks. A changelog fix (v2.1.287) implies yes; CLOUD-ENV's table speaks only of user-settings and repository-declared plugins (no) and account skills (yes).
3. When `--bare` becomes the default for `-p`, and whether a flag will keep subscription login for non-bare `-p` runs after that.
4. Whether Anthropic counts a locally installed tool that invokes the user's own `claude -p` on a schedule as "ordinary, individual usage". LEGAL permits the user running the unmodified binary on their own subscription and forbids third parties routing requests through plan credentials; a helper that shells out to the user's binary sits between the two.
5. How `$.model.complete` calls from a mod count toward plan limits in practice (rate, model availability on each plan), and whether there is a per-mod budget. The docs say only that it bills to the user's plan.
6. The exact size limits of mod-injected context (`prompt.submit` `context`, `prompt.context` blocks). The 10,000-character cap is documented for settings hooks; MODS-REF's limits table lists none for these.
7. The stability promise for the mods API. It shipped 2026-10-01 and changed in most releases since (v2.1.289 to v2.1.321 each note additions or behaviour changes).
8. Whether auto memory's file format (frontmatter `type`, `modified`, the `MEMORY.md` line shape) is a contract.
9. What `CLAUDE_MEMORY_STORES` is. One changelog line (v2.1.172, June 2026) mentions "mounted team memory stores (`CLAUDE_MEMORY_STORES`) in remote sessions"; no docs page describes it.
10. Whether Cowork's local project memory store is readable by a plugin and where it is on disk.
11. Whether the "new unified Claude experience" will remove local Cowork execution for everyone, and on what timetable.

---

## Fact check

Checked 2026-11-10 by a second reader against the live pages, fetched as raw Markdown and searched for the exact text (not summarized by a model). Changelog head at the time: v2.1.321, November 9, 2026. Verdicts: confirmed, partly (what is off), refuted, could-not-check. Nothing was refuted. Four facts are partly right, and the one that changes the design is F12.

### Verdicts

| Fact | Verdict | What the source says, and the correction | Source |
| :- | :- | :- | :- |
| F01 | confirmed | 33 event sections counted on the page. "There are five types"; agent hooks "are experimental and may change". Both quoted sentences are verbatim. | https://code.claude.com/docs/en/hooks.md |
| F02 | confirmed | Verbatim, including "this cap has no setting or environment variable to raise it". Two details to add: each string is measured on its own "even when several hooks run for the same event", and all values are delivered; PreCompact stdout over the cap is cut with a marker, not saved to a file. | https://code.claude.com/docs/en/hooks.md |
| F03 | confirmed | Verbatim. A fourth resume field exists, `estimated_cache_write_usd`. `mcp_tool` hooks are skipped only at launch; they run when SessionStart fires again after `/clear` or a compaction. | https://code.claude.com/docs/en/hooks.md |
| F04 | confirmed | Verbatim. Add: a timed-out hook is not silent: "The transcript shows a notice naming the hook, the timeout that fired, and that the output was discarded." The injected reminder "starts with the hook's name". | https://code.claude.com/docs/en/hooks.md |
| F05 | partly | 1.5 s default, 60 s ceiling, and the plugin sentence are verbatim. Off: a mod's `session.end` hooks do not share the pool; they get a window of the same length "counted from when your settings `SessionEnd` hooks finish". Omitted: `CLAUDE_CODE_SESSIONEND_HOOKS_TIMEOUT_MS` also raises it. The kill of async hooks is stated for `-p` runs only. | https://code.claude.com/docs/en/hooks.md ; https://code.claude.com/docs/en/plugins/mods/reference.md |
| F06 | confirmed | All rows match the "What survives compaction" table. PostCompact is in v2.1.76. Idle compaction fires the same hooks with matcher `auto`. The table names "Project-root CLAUDE.md and unscoped rules"; it does not name user-level CLAUDE.md. | https://code.claude.com/docs/en/context-window.md ; https://code.claude.com/docs/en/hooks.md |
| F07 | confirmed | Verbatim; v2.1.163 changelog entry found. | https://code.claude.com/docs/en/hooks.md |
| F08 | partly | `async` and `asyncRewake` behave as stated. Off: `asyncRewake` did not ship in v2.1.287. That release only fixed an `asyncRewake` bug, so the field is older; the changelog has no entry for its introduction. | https://code.claude.com/docs/en/hooks.md ; https://code.claude.com/docs/en/changelog.md |
| F09 | confirmed | Verbatim. The page's own list of script interfaces is: `claude -p` JSON output, `claude -p --resume`, the `transcript_path` field ("A `SessionEnd` hook can archive the transcript"), and the Agent SDK. So copying the file is supported; parsing its entries is what is unstable. | https://code.claude.com/docs/en/sessions.md ; https://code.claude.com/docs/en/claude-directory.md |
| F10 | confirmed | Changelog 2.1.287, October 1, 2026: "Added Claude Mods: plugins may now modify deeper behavior". Events and quotes verbatim. Add: not in Desktop WSL sessions; `--bare` and `--safe-mode` stop installed mods; Desktop bundles its own Claude Code copy (mods from 2.1.286). | https://code.claude.com/docs/en/plugins/mods/overview.md ; https://code.claude.com/docs/en/plugins/mods/reference.md |
| F11 | confirmed | Billing sentence verbatim; 10 s, 4 MiB and 4,096-entry limits are in the limits table. Add: a `$.model.complete` request carries no tools and no CLAUDE.md, and an API failure does not reject: check `r.isAnswered`. | https://code.claude.com/docs/en/plugins/mods/api.md ; https://code.claude.com/docs/en/plugins/mods/reference.md |
| F12 | partly | The guard blocks far more than the system prompt. Where it loads, a user's mod hooks on `prompt.context`, `session.compact`, `prompt.attachment`, `skill.prompt`, `settings.read` and every `classic.<Event>` "never run", and `session.append` does not get hook-context, attachment or compaction rows. `allowManagedHooksOnly` also blocks installed mods. | https://code.claude.com/docs/en/plugins/mods/admin.md |
| F13 | confirmed | Verbatim: "Only two keys take effect from a plugin"; "Claude Code doesn't load a `CLAUDE.md` at the plugin root". | https://code.claude.com/docs/en/plugins/components.md |
| F14 | confirmed | The table matches row for row. The same page says the directory "lists a mod ... for Claude Code". That points the same way as the mods page, which leaves Cowork out of "Where mods run", but no page says outright that Cowork skips a hooks module, so open question 1 stays open. | https://claude.com/docs/plugins/platform-support.md |
| F15 | confirmed | Verbatim. Add: sync is also off when `--setting-sources` leaves out `user`. A third source type exists, `command`, that neither auto-update default covers (see below). | https://code.claude.com/docs/en/plugins/loading.md ; https://code.claude.com/docs/en/plugins/install.md |
| F16 | confirmed | Table matches. Limits to add: repository hooks and `.mcp.json` load "in a session with one repository"; a cloud thread in a project loads only root CLAUDE.md and unscoped rules; a setup script may write `~/.claude/CLAUDE.md`. | https://code.claude.com/docs/en/cloud-environments.md ; https://claude.com/docs/cowork/overview.md |
| F17 | confirmed | Verbatim, and v2.1.59 is February 26, 2026. Add: "When you ask Claude to remember something ... Claude saves it to auto memory." | https://code.claude.com/docs/en/memory.md |
| F18 | confirmed | "maximum depth of four hops"; AGENTS.md needs v2.1.277. Mid-session edits do not apply, but "The new content loads on the next `/clear`, `/compact`, or restart", so a compaction picks up a rewritten file. | https://code.claude.com/docs/en/memory.md ; https://code.claude.com/docs/en/prompt-caching.md |
| F19 | confirmed | Both quotes verbatim; article marked "Updated this week". On Team and Enterprise, owners control it. No third-party API is mentioned. | https://support.claude.com/en/articles/11817273-use-claude-s-chat-search-and-memory-to-build-on-previous-context |
| F20 | confirmed | Beta header, immutable versions, redact, `read_only`, "many small focused files", dreams with "1 to 100 sessions" and "The input store is never modified" all found. | https://platform.claude.com/docs/en/managed-agents/memory ; https://platform.claude.com/docs/en/managed-agents/dreams |
| F21 | confirmed | Every flag found in the CLI reference with the stated behaviour. `--effort` also accepts `ultracode`. | https://code.claude.com/docs/en/headless.md ; https://code.claude.com/docs/en/cli-reference.md |
| F22 | confirmed | Both quotes verbatim. The page also says bare mode "doesn't use your subscription login". | https://code.claude.com/docs/en/headless.md |
| F23 | confirmed | Both updates verbatim. The June 15 text reads "Claude Agent SDK, `claude -p`, and third-party app usage still draw from your subscription limits." | https://support.claude.com/en/articles/15036540-use-the-claude-agent-sdk-with-your-claude-plan |
| F24 | confirmed | All quotes verbatim. Exact wording: OAuth "is intended exclusively for purchasers of" the plans. | https://code.claude.com/docs/en/legal-and-compliance.md |
| F25 | confirmed | Supervisor, `claude daemon status/logs/stop`, restart after auto-update, and the local-task limit "only fires while the app is open and your computer is awake" all found. Agent view is v2.1.139, May 11, 2026. | https://code.claude.com/docs/en/agent-view.md ; https://code.claude.com/docs/en/desktop-scheduled-tasks.md |
| F26 | confirmed | 1% and 1,536 found. Both are settings, not fixed: `skillListingBudgetFraction` and `skillListingMaxDescChars`. | https://code.claude.com/docs/en/skills.md |
| F27 | confirmed | Verbatim. The 4,096 limit can be changed by the user with `CLAUDE_CODE_MAX_MCP_DESCRIPTION_LENGTH` (v2.1.280+). | https://code.claude.com/docs/en/mcp.md ; https://claude.com/docs/connectors/building/index.md |
| F28 | confirmed | Verbatim. The same article says the unified experience "is rolling out to Pro, Max, and Ultra plans". | https://code.claude.com/docs/en/desktop.md ; https://support.claude.com/en/articles/15811196-what-to-expect-with-claude-cowork-in-the-cloud |
| F29 | confirmed | "What loads at startup" lists the CLAUDE.md hierarchy, git status and preloaded skills; "Only Explore and Plan skip it". The subagent's prompt includes "the first 200 lines or 25KB of `MEMORY.md`". Subagents Claude spawns from the main conversation "start in the background". Explore and Plan also skip the git status snapshot. | https://code.claude.com/docs/en/sub-agents.md ; https://code.claude.com/docs/en/memory.md |
| F30 | partly | Wrapping, wording advice, replay on resume and v2.1.284 are verbatim. Off: the cache sentence is about turning a plugin on or off mid-session and covers "skills, commands, agents, hooks, monitors, or themes" only. A plugin's MCP servers follow the MCP rules and can invalidate the cache where tool search is off. | https://code.claude.com/docs/en/hooks.md ; https://code.claude.com/docs/en/prompt-caching.md |

### What F12 changes

Section 9's move 2 ("adds first-message context through `prompt.context`") and any plan to steer or observe compaction from a mod do not work for a user-installed mod where the guard loads. The guard loads when "The machine has managed settings or a `managed-mcp.json` file" or "The session runs on `/login` credentials for a Team or Enterprise plan". The blocked list, from "Know which hooks of a user's mod don't run": `classic.<Event>`, `prompt.compose`, `prompt.section`, `prompt.context`, `prompt.attachment`, `skill.prompt`, `attribution.text`, `session.compact`, `settings.read`, and `session.append` rows for attachments, hook context and compaction.

Not in that table, so by inference still available to a user's mod there: `prompt.submit` (including `context`), `turn.start`/`turn.step`/`turn.complete`, `session.start`/`session.end`, `session.append` for prompts, responses and tool results, `tool.call`, and the `$` API. The page supports this in general terms ("It can also rewrite tool calls and prompts, deny a tool call, and draw in the interface") and warns that the guard "also passes over a user's hooks in some other cases", so each event the design uses needs a test on a Team login. Settings hooks in the plugin's `hooks/hooks.json` are not affected by the guard: "Users' other customizations keep working: their hooks in settings files and in plugins' `hooks/hooks.json`". So on Team and Enterprise logins the brief at session start and the compaction hooks have to come from settings hooks (SessionStart, PreCompact, PostCompact), not from the mod.

### Facts the notes lack

1. **Anthropic can turn installed mods off remotely.** `claude plugin test` reports "hooks modules are turned off in this process: the rollout switch served off", explained as "Anthropic has turned installed mods off remotely." A mod cannot be the only path for capture or recall. https://code.claude.com/docs/en/plugins/mods/troubleshoot.md
2. **One `hooks/hooks.json` holds both.** `modules` names the hooks module and the same file "Can also hold settings hooks under `hooks`". One plugin can ship command hooks as the base and a mod as an addition, with no second install. https://code.claude.com/docs/en/plugins/mods/reference.md
3. **A plugin's source can be a command on the user's machine.** "Use a `command` source when a tool installed on the user's machine produces the plugin directory". Claude Code runs it on install and update and "again once per session", and reloads the plugin in the running session when the output changed, independent of the marketplace's auto-update setting. Users accept the exact command once; admins can turn it off with `disableCommandPluginSources`. This removes the drift between an installed binary and its plugin without relying on third-party auto-update. https://code.claude.com/docs/en/plugins/marketplace-reference.md ; https://code.claude.com/docs/en/plugins/loading.md
4. **The `bin/` rule has a documented way round.** Only a top-level `bin/` directory blocks install on claude.ai and Cowork: "Keep executables in another directory, such as `scripts/`, and reference them as `${CLAUDE_PLUGIN_ROOT}/scripts/<name>`". https://claude.com/docs/plugins/org-sync.md
5. **Other mods can see and refuse a mod's calls.** "Every mods API method is also an event ... A hook on one intercepts calls from the mods that run after it, and can return `next(e)`, `{ deny: reason }`, or `{ value }`." Another installed mod, or an organization's policy mod, can read or deny the memory mod's `model.complete`, `fs.read` and `http.fetch` calls, and any mod sees every prompt. https://code.claude.com/docs/en/plugins/mods/reference.md
6. **Mods changed in most releases since launch.** 17 of the 30 releases from v2.1.287 to v2.1.321 mention mods, including behaviour changes (for example `breakPromptCache`: "From v2.1.299 until v2.1.321, a running conversation got the changed text on the next turn"). The reference says the GitHub type declarations "can be older than the Claude Code version you have installed". No stability statement was found. https://code.claude.com/docs/en/changelog.md ; https://code.claude.com/docs/en/plugins/mods/reference.md
7. **Two host versions on one machine.** "The Desktop app includes its own copy of Claude Code", so the terminal and the Desktop Code tab can run different versions against the same user-scope plugin at the same time. https://code.claude.com/docs/en/plugins/mods/overview.md
8. **Mods have an offline test runner.** `claude plugin test` runs a mod's `.test.ts` files: tests "fire events, stub Claude Code's answers, and press buttons, with no session, sign-in, or network". https://code.claude.com/docs/en/plugins/mods/test.md
9. **A prompt hook that times out is shown to the user**, and injected context is labelled with the hook's name (see F04). Latency failures are visible, so the per-prompt path needs a hard internal deadline well under 30 s. https://code.claude.com/docs/en/hooks.md
10. **Explicit "remember this" goes to the host's auto memory by default** (see F17). A second memory layer has to decide whether it reads that directory, redirects it with `autoMemoryDirectory`, or leaves it alone. https://code.claude.com/docs/en/memory.md
11. **Account-synced plugins do run in cloud sessions, hooks and MCP servers included.** Besides the v2.1.287 entry the notes cite, v2.1.269 ("Fixed synced plugin MCP servers not connecting when a remote session resumes") and v2.1.317 ("in cloud sessions when a synced plugin included the same server") say the same. This closes most of open question 2, from changelog entries only. https://code.claude.com/docs/en/changelog.md
12. **OpenTelemetry is another documented, structured feed of a session.** `OTEL_LOG_USER_PROMPTS`, `OTEL_LOG_ASSISTANT_RESPONSES` ("assistant response text on `assistant_response` events") and `OTEL_LOG_RAW_API_BODIES` are off by default and need an OTLP endpoint to receive them. https://code.claude.com/docs/en/monitoring-usage.md
13. **Naming and bundling rules.** "you can't use the Claude Code or Anthropic names or logos as part of your own product, feature, or [organization] name" (paraphrased in one word to keep this repository's neutral wording). A product that preinstalls or runs Claude Code must leave the binary unmodified and "may not pay for, resell, or intermediate Claude usage on their end users' behalf". https://code.claude.com/docs/en/legal-and-compliance.md
14. **The unified app has a stated rollout.** "The new Claude experience, which combines chat and Cowork, is rolling out to Pro, Max, and Ultra plans", and in it "tasks run in the cloud, and there's no option to run them locally". For consumer plans the non-Code side of the desktop app is moving off the user's machine now, which bears on open question 11. https://support.claude.com/en/articles/15811196-what-to-expect-with-claude-cowork-in-the-cloud

### Process note

One download in this check was written by mistake to the root of the eva-md checkout (`plugins_mods_test.md`, untracked). The scratch folder the check was using had been removed (cause not known; the scratchpad is shared), a `cd` into it failed, and the download on the same command line ran in the repository root. The file was removed at once and `git status` was clean afterwards. Nothing else outside the scratchpad was written.
