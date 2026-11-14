# Packaging for Claude Code and Codex

- **Component:** Static plugins, and making a release
- **Paths:** `internal/install`, `internal/host/adapter`, `scripts/release.sh`, `.github/workflows`
- **Status:** v1 as built, frozen on 2026-11-14

The blocks below are v1's design and verification text, moved here word for word when the two documents were split; [INDEX.md](INDEX.md) lists every spec.

<!-- v1 docs/design.md 032 -->
## Packaging for Claude Code and Codex

### Claude Code

Plugin `eva-md` in the local marketplace folder `~/.local/share/eva-md/marketplace/claude`.

- **`.claude-plugin/marketplace.json`:** `name: "eva-md"`, a nonempty `description`, `owner: {name: ...}`, and one entry `{name: "eva-md", source: "./plugins/eva-md"}`. Claude 2.1.311 strict validation requires the marketplace description.
- **`plugins/eva-md/.claude-plugin/plugin.json`:** `name: "eva-md"`, `version: "1"` (a string, changed only when the wiring changes), `description`, `author: {name: ...}`. A missing description or author fails `validate --strict`.
- **`plugins/eva-md/hooks/hooks.json`** in exec form, with no shell:
  `{"type": "command", "command": "<home>/.local/share/eva-md/bin/eva-md", "args": ["hook", "--host", "claude-code"], "timeout": N}`

**Events and timeouts (seconds):**

| Event | Matcher | Timeout |
| --- | --- | --- |
| SessionStart | startup\|resume\|clear\|compact\|fork | 10 |
| UserPromptSubmit | | 5 |
| PostToolUse | `Bash\|Edit\|Write\|NotebookEdit\|TaskCreate\|TaskUpdate\|TodoWrite\|Read\|Grep\|Glob\|WebFetch\|WebSearch\|mcp__.*`, async | none (not enforced on async hooks) |
| PostToolUseFailure | same matcher, async | none |
| Stop | | 5 |
| StopFailure | | 3 |
| PreCompact | manual\|auto | 10 |
| PostCompact | | 5 |
| PostModelSwitch | | 3 |
| SessionEnd | | default budget (1.5 s, which plugin timeouts can't raise); handler under 200 ms |

- The digest skill, `skills/eva-md-digest/SKILL.md`, rendered with the installed binary's path and the host, as hooks.json is.
  It teaches an agent to digest documentation and repos into system pages, one system at a time: read the map and the page first and, before changing a page, run `eva-md system status <slug>` and read again the paths whose code moved; summarize, keep identifiers exact, cite each source's URL with its
  version when it has one, and cite code as `<repo>@<commit>:<folder or file>` at the commit read (one commit per repo, at most 16 a page); a page built only from documentation may set `stale_after`; list under `repos` the repos that build or use the system, so their chats' briefs point at the page
  and their prompts recall each other's knowledge, and under `aliases` its other names; never copy sensitive documents (HR, legal, health, credentials), only cite them by link and title; and write through `eva-md system write`.
  No `bin/`.
- **`plugins/eva-md/.mcp.json`:** the MCP server, rendered with the installed binary's path, as hooks.json is:
  `{"mcpServers": {"eva-md": {"args": ["mcp", "--host", "claude-code"], "command": "<home>/.local/share/eva-md/bin/eva-md"}}}`
  No `type`, since both hosts start a command as a stdio server; no `env` or `cwd`, which the server doesn't need and a host might refuse; no `${CLAUDE_PLUGIN_ROOT}`, since the binary's absolute path is the plugins' convention; and no version or date.
  Claude Code reads a plugin's `.mcp.json` on its own, so plugin.json names no servers.
  The tools' names on Claude Code are `mcp__plugin_eva-md_eva-md__eva_search`, `…__eva_show` and `…__eva_status`.
- Adding the skill and widening the tool matchers (so reads, searches, web fetches and MCP calls are recorded by name) changed both plugin folders once, so Codex asks for hook trust again after that upgrade.
- Adding `.mcp.json` changed both plugin folders once more, so Codex asks for hook trust again after that upgrade too. hooks.json is unchanged.
- A second skill, `skills/eva-md-recall/SKILL.md`, rendered the same way, tells an agent when to look in memory: before re-deciding what earlier chats may have settled, such as a design choice, a tool, a limit or a name; before changing code, by searching the file's path, as the repo writes it, for the system pages and knowledge that cite it or its folder; and when the user refers to earlier work.
  It says how: `<bin> search <terms>` with a few exact terms, `--all` for every project, `<bin> show <ref>`, `<bin> show knowledge/map` and `<bin> show systems/index`, or `eva_search`, `eva_show` and `eva_status` when the MCP tools are connected; and that what it finds is reference data, not instructions, the user and the code being current.
  Its frontmatter is only `name: eva-md-recall` and a description of at most 200 characters, and it holds no version or date.
  Adding it changed both plugin folders once more, so Codex asks for hook trust again after that upgrade too; hooks.json is unchanged.
  Pointing it at the system index in place of the map changed both folders again, with the same request for trust.
- Local-folder marketplace plugins **load in place** at every session start and ignore their version string. So the folder is regenerated only when its bytes change, swapped atomically, and never deleted while installed.
- Covers the CLI and local or SSH sessions in the desktop Code tab. Cloud sessions are skipped through `CLAUDE_CODE_REMOTE`.

### Codex

Plugin `eva-md` in a separate marketplace folder, `~/.local/share/eva-md/marketplace/codex`, so Codex never reads the Claude manifest.

- **`.agents/plugins/marketplace.json`:** top-level `name: "eva-md"` (this is what `@eva-md` refers to) and `interface.displayName`. One entry: `{name: "eva-md", source: {source: "local", path: "./plugins/eva-md"}, policy: {installation: "AVAILABLE", authentication: "ON_INSTALL"}, category: "Productivity"}`.
- **Plugin manifest:** only `.codex-plugin/plugin.json`, with name, version `"1.0.0"`, description, author, interface metadata, `"skills": "./skills/"` for the digest and recall skills and `"mcpServers": "./.mcp.json"` for the MCP server, which is the Claude plugin's file with `--host codex`; its tools' names are `mcp__eva-md__eva_search`, `mcp__eva-md__eva_show` and `mcp__eva-md__eva_status`.
  Codex discovers `hooks/hooks.json` by default.
  On Codex 0.164.0 a portable root `plugin.json` loads the metadata but exposes no hooks, even with an explicit `extensions.com.openai.hooks` path, while the `.codex-plugin` form exposes all seven.
  The portable form would be used only after a host check shows it loading hooks, and the plugin never ships both forms.
- **`hooks/hooks.json`:** each command is a shell string, `'<home>/.local/share/eva-md/bin/eva-md' hook --host codex || echo '{}'`.
- **The whole plugin folder must stay byte-identical** across binary-only releases. On Codex 0.164.0, hook trust covers a fingerprint of the plugin's contents: a manifest-only change invalidates it. So no version numbers, dates or build ids appear anywhere in it.

**Events:**

| Event | Matcher | Timeout | additionalContextLimit (tokens) |
| --- | --- | --- | --- |
| SessionStart | startup\|resume\|clear\|compact\|fork | 10 | 3000 |
| UserPromptSubmit | | 5 | 3000 (it can carry the full fallback brief) |
| PostToolUse | `^(Bash\|apply_patch\|update_plan\|mcp__.*)$`, async | 5 | not set |
| Stop | | 5 | not set |
| PreCompact | manual\|auto | 10 | not set |
| Interrupt | | 1 | not set |
| SessionEnd | | 2 (maximum 3) | not set |

- `Bash` covers both the shell tool and unified exec. Setting the limit on events that can't return context produces a config warning, so it is set only on SessionStart and UserPromptSubmit.
- Installed with `codex plugin marketplace add <dir>` and `codex plugin add eva-md@eva-md`, and checked one local marketplace at a time with `codex plugin list --json --marketplace=<name>` (see "Install and uninstall").
- One manual step: trust the hooks once in /hooks, and again only if the plugin folder ever changes.
- Targets the CLI and the ChatGPT app's Codex mode, which share `~/.codex/config.toml`; cross-surface trust is still unverified (see Open host questions). The IDE extension is outside this plugin integration's scope.

### Shared

- One binary at a stable path outside both plugins, so both hosts always run the same version and binary updates never change the plugins.
- Plugin templates are embedded in the binary. Each plugin also carries an icon, `plugins/eva-md/assets/icon.png`.
- Per-machine config at `~/.config/eva-md/config.toml`:
  - wiki path, expected remote, instance id, machine label
  - `exclude` (path globs and remote patterns; a remote can be written as a URL, in scp form or with `.git`, and is normalized like the origin)
  - `notice` (start or problems)
  - `updates` (default true; false stops every background update at once)
  - effort per host and per model (default `highest`) and the daily cap of model calls per host (default 40; an explicit 0 is kept)
  - `codex_ignore_user_config` (default false)
  - `commits` (default true; false keeps the daily commit, push and push retry out of maintain, which `updates = false` leaves on)
  - `reminder_every` (default 5; how many prompts apart the memory reminder rides with a prompt; 0 turns it off; a negative number is refused)
  - `mcp` (default true; false turns the MCP tools off, while the plugins, hooks and command line stay on)
  - `recall` (default true; false sends no knowledge with a prompt, while search, show and the brief's constraints stay on)
  - planned: optional per-host flag overrides
  - unknown keys are ignored, never saved back, and reported by the doctor, status and the chat-start notice
- State at `~/.local/state/eva-md`. eva-md joins its folders onto `$HOME` as given, and opens this one and `~/.local/share/eva-md` through no link but macOS's `/var`, `/tmp` and `/etc` aliases, so a home that is itself a symlink, or that `$HOME` reaches through one, isn't supported: install can't
  create them, and the hooks capture nothing, give no brief and write nothing, not even the daily trigger's marker.
- No host settings files are written, nothing is added to CLAUDE.md or AGENTS.md, and there is no telemetry and no auto-update.
- The binary is distributed as a GitHub release (see "Making a release") and installed or upgraded with `install.sh` or `eva-md upgrade` (see "The install scripts and upgrade"). Install links it into `~/.local/bin` only when that folder is on PATH (see "Link on PATH").

### Making a release

[releasing.md](../releasing.md) is the maintainer's guide; this is the contract.

- **Tag.** A release's tag is its version: the UTC day of the released commit's commit date as `YYYY.M.D` without zero padding, then `.2`, `.3` and so on for later releases the same day, with no `v` prefix: `^[0-9]{4}\.[1-9][0-9]?\.[1-9][0-9]?(\.[2-9]|\.[1-9][0-9]+)?$`.
  A tag is annotated, its message is the release notes (the subjects of the commits since the previous release tag), and it is never moved, deleted or reused.
  A tag cut from a Mac is signed; one cut from GitHub is unsigned, and the approval GitHub records for it stands in for the signature.
- **Cutting one.** There are two ways, the Cut a release workflow on GitHub and `scripts/release.sh` from a Mac, and both make the same checks, with the script.
  It checks, in this order, that it runs from the checkout's root on main, with a clean tree and no untracked files, that HEAD carries the CI and release workflows, both install scripts and the licenses, that tag signing is set up, that origin and any push URL it has are `github.com/heyNag/eva-md`,
  that HEAD is origin/main after a fetch, that gh is signed in to github.com, that CI passed for HEAD and that HEAD isn't released yet (a release tag on HEAD that only this clone has stops with the command that pushes it); it stops at the first failure with its fix.
  It names the tag and shows the tag, commit and notes.
  Its git calls that format commits run with `log.showSignature=false`, so a signature check can't reach the tag's name or the notes.
  Run as `--plan`, the script is the one place the tag and notes are worked out for a release from GitHub: it makes every check but the signing check, checks that gh can read the repository (`gh api repos/heyNag/eva-md`) instead of that it is signed in, since a workflow's gh signs in with a token
  `gh auth status` may not recognise, asks nothing, and when `$GITHUB_OUTPUT` is set writes the tag, the commit, the previous release and the notes there, the notes between lines of a random delimiter.
  `--plan` and `--dry-run` can't be combined.
  In a workflow (`GITHUB_ACTIONS=true`), each failure is also printed to stdout as an error command, `::error::<problem>. Fix: <fix>`, with `%`, CR and LF encoded, so a failed plan shows as an annotation on the run.
- **From GitHub.** `.github/workflows/cut-release.yml`, Cut a release, is run by hand (`workflow_dispatch`, with a boolean `dry_run`).
  Its jobs run in a line, each needing the one before: **plan** fails off main, when main moved since the run started, and unless the repository's `release` environment exists with at least one required reviewer (it warns when the environment allows any branch), then runs `scripts/release.sh --plan` and writes the tag, a link to the commit and the notes to the run's summary; a dry run stops there.
  **approve** runs in the `release` environment, so GitHub holds it until a required reviewer approves and records who did; that approval is in place of a signature.
  **tag** checks the plan's tag and commit, makes an unsigned annotated tag on the planned commit with the notes, its tagger who started the run, pushes only that tag, never with force, and checks origin has that tag.
  When origin has the tag already, the job makes and pushes none and goes on only when origin's tag is annotated, on the planned commit and has the planned notes, as when its run's failed jobs are re-run after a failure that followed the push; otherwise it fails, since a tag is never moved.
  **publish** calls the release workflow with the tag.
  **check**, on macOS, checks the published release as the script does, and also that each asset's provenance names the planned commit (`--source-digest`); its source ref is `refs/heads/main`, since a called workflow's source ref is its caller's.
  Its provenance failure says to re-run the run's failed jobs first, and to make the release a draft and release a fix if it fails again.
  The workflow may do nothing by default; plan may read Actions and contents, approve nothing, tag write contents, publish grant exactly what the release workflow's jobs ask for (`contents: write`, `id-token: write`, `attestations: write`), and check read contents and attestations.
  Every job but publish has a time limit, only the tag job pushes and only its checkout keeps credentials, one run goes at a time, and every action is pinned to a full commit SHA.
  A dry run's run is named "Cut a release (dry run)" and a release's "Cut a release" (`run-name`), so the runs list tells them apart.
  No job's name holds an expression, since GitHub doesn't evaluate the name of a job it skips: the approve and tag jobs, which a dry run skips, are "Approve the release" and "Tag the release", and the plan's summary names the tag.
- **From a Mac.** `scripts/release.sh`, the fallback, asks `Release <tag> from <short commit>? [y/N]` on the terminal, reading the answer from the terminal, never stdin; `--dry-run` stops before the question.
  After a yes it makes the signed tag, pushes only that tag, watches the release workflow's run with `gh run watch --exit-status`, then downloads the release and checks every asset, `SHA256SUMS`, each asset's build provenance, with the tag as its source ref (`--source-ref refs/tags/<tag>`), and then
  the version this Mac's binary prints, so nothing downloaded runs before its provenance verifies, and prints the release's URL and the install command.
- **Workflow.** `.github/workflows/release.yml` runs on a pushed tag, and when Cut a release calls it with the tag (`workflow_call`), since a tag pushed with a workflow's token starts no workflow; called, it reads the tag from its input, and either way one run per tag goes at a time.
  It refuses a tag not in the format, not annotated, whose commit isn't on main, or whose release exists; builds both darwin binaries with `CGO_ENABLED=0`, `-trimpath`, `-mod=readonly` and the tag as `main.version`; copies the install scripts and the licenses from the tagged tree; writes `SHA256SUMS`;
  checks the sums and the arm64 binary's version on a macOS runner; attests the five files' build provenance, whose signer is `release.yml` whichever way the release was cut; and creates the release with the tag's annotation as its notes, followed, when Cut a release called it, by a line linking the
  run, the caller's, which records the approval; the tag doesn't name the run. gh matches `--signer-workflow` as a prefix, so every provenance check also names the source ref.
  The provenance proves the files were built by `release.yml` from the commit, not who released them: a Mac tag's signature or a GitHub run's approval shows that.
  Each job has only the permissions it needs and a time limit, checkout keeps no credentials, and every action is pinned to a full commit SHA.
- **Assets.** `eva-md-darwin-arm64` and `eva-md-darwin-amd64` (plain executables), `install.sh`, `uninstall.sh`, `LICENSES.txt` and `SHA256SUMS`, one `shasum -a 256` line (`<64 hex>  <name>`) for each of the other five.
  `LICENSES.txt` is `scripts/LICENSES.txt`: eva-md's license, then the license file of each module the binaries are built with, the license of Go, whose runtime and standard library they contain, and the notices in Go's own code that ask to go with a copy of the program (fdlibm's in package math, and
  Inferno's in the Intel binary's runtime), since each of those licenses and notices asks to go with every copy.
  They are at `https://github.com/heyNag/eva-md/releases/download/<tag>/<name>`, and the newest release's at `https://github.com/heyNag/eva-md/releases/latest/download/<name>`; `https://github.com/heyNag/eva-md/releases/latest` redirects to the newest release's tag page.

---

<!-- v1 docs/verification.md 044 -->
- **Host adapter** (`internal/host/adapter`).
  Each host has one id and its names, and the lookups take exactly v1's spellings.
  Every quirk in the spec table has a fixture that shows it, and a table missing one fails.
  Fake CLIs below, inside and above the tested range get no row, their row, and the newest row marked untested.
  A probe ends at its time limit and when it is cancelled, and a CLI's version is read again only when its file's size or modification time changes.
  A count of host-name literals outside `internal/host`, for each package, may only go down (`TestHostLiteralCount`, with its baseline in `testdata/host-literals.txt`).
  Nothing imports the package yet.

<!-- /v1 -->

## Must keep

The must-keep items of [findings.md](../v2/findings.md) this component touches, numbered from K01 in each list, and the tests that hold them.

| Id | Item | Tests |
| --- | --- | --- |
| MCP-K07 | Plugin folders hold no version or build id and render the same each time, so a binary-only release keeps Codex's trust | internal/install:TestPluginFolderBytes, internal/install:TestBinaryOnlyReinstallKeepsCodexTrust |
| MCP-K08 | Plugins load in place: rendered again only when bytes change, swapped atomically, never deleted while installed | internal/install:TestBinaryOnlyReinstallKeepsCodexTrust, internal/install:TestUpgradeWhenTheHostRefusesToAddAnInstalledPlugin, internal/install:TestFailedUpgradeRestoresOldPluginBytes, internal/install:TestIncompleteRollbackKeepsLoadableFiles |
| MCP-K09 | One marketplace folder a host, and the Codex manifest in the form that exposes hooks | internal/install:TestPluginFolderBytes, internal/host/adapter:TestPluginHoldsV1sValues |
| MCP-K10 | Claude Code's hook command in exec form, Codex's a shell string that ends by printing {}, a context limit only where context returns | internal/install:TestGeneratedHookContracts, internal/install:TestPluginFolderBytes |
| MCP-K11 | PostToolUse stays async on both hosts, and SessionEnd keeps within the host's fixed limit | internal/install:TestPluginFolderBytes |
| MCP-K12 | The SessionStart matcher covers startup, resume, clear, compact and fork | internal/install:TestPluginFolderBytes |
| MCP-K15 | eva-md never writes host settings and never grants Codex hook trust | internal/install:TestFirstInstallOnTheFakeHosts, internal/install:TestUpgradeFrom2026_11_5OnTheFakeHosts |
| MCP-K17 | Only eva's name shows: the server, the tools and the plugin | cmd/eva-md:TestMCPCommand, internal/mcp:TestToolListIsFixedAndSmall, internal/install:TestPluginFolderBytes |
| OPS-K05 | Plugin folders carry no version or date and are rewritten only when bytes change | internal/install:TestPluginFolderBytes, internal/install:TestBinaryOnlyReinstallKeepsCodexTrust |
| OPS-K15 | Releases need main, green checks and an approval or signed tag; provenance is checked against one signer; actions are pinned | internal/release:TestReleaseChecksProvenanceAgainstOneSigner, internal/release:TestReleaseWorkflowPinsEveryAction, internal/release:TestReleaseCutWorkflowNeedsAnApproval, internal/release:TestReleaseStopsAtTheFirstFailedCheck, internal/release:TestReleasePlanStopsAtTheFirstFailedCheck |

## Since the split

No change yet.
