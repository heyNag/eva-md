# Hosts: open questions and live checks

- **Component:** The hosts: what is observed and what is not
- **Paths:** `internal/host/adapter`
- **Status:** v1 as built, frozen on 2026-11-14

The blocks below are v1's design and verification text, moved here word for word when the two documents were split; [INDEX.md](INDEX.md) lists every spec.

<!-- v1 docs/design.md 039 -->
## Open host questions

Host behaviour not yet observed. Each is an assumption in this design until it is observed.

**Claude Code**
- Folder trust: in a never-trusted folder, whether SessionStart fires before trust is accepted. Either way, the first prompt after trust should deliver the brief.
- Prompts you didn't type: whether a reliable field or wrapper shape tells /loop iterations, scheduled tasks, background subagent reports and cross-session messages apart from typed prompts. If none exists, the risk stays as documented.
- `prompt_id` stability: one stable id per typed prompt across UserPromptSubmit, PostToolUse and Stop, in a turn that includes a background subagent report.
- Desktop spare sessions: whether SessionStart fires again with the real `cwd` when a pre-started session is claimed, and whether the first UserPromptSubmit carries it and delivers the brief.
- Entrypoints: which `CLAUDE_CODE_ENTRYPOINT` value each surface sets (the CLI, the desktop app, the editor extension), that print mode is `sdk-cli` and the Agent SDK `sdk-ts` or `sdk-py`, and that the desktop app and the editor extension set their own value, never an SDK one, which would make each of their chats pass as a program's, with no update of its own and never the brief's previous chat.
  One chat per surface, then the doctor's per-event rows, confirms it.
- Pasted text: whether hook payloads carry Claude Code's paste wrapper with matching ids. If the host strips it, the Claude side of the paste rule does nothing.
- The digest and recall skills: that `claude plugin validate --strict` accepts the plugin with them, and that each triggers.
- A deleted marketplace folder: what `claude plugin list --json` reports for eva-md once `~/.local/share/eva-md` is gone, and whether its uninstall and marketplace remove succeed then.
- The MCP server: that `claude plugin validate --strict` accepts `.mcp.json`, that `/mcp` shows eva-md connected with three tools, whether the first call of each tool asks permission, whether the tools are deferred behind ToolSearch in a session with few servers, and that quitting the app leaves no `eva-md mcp` process.
- Payload samples still missing: PostToolUseFailure (a failing Bash, whose error should start with "Exit code N"), StopFailure, PostCompact, PostModelSwitch, SessionStart with and without a model, and PostToolUse for TaskCreate, TaskUpdate and TodoWrite.

**Codex**
- Trust across surfaces: whether hooks trusted in the CLI fire in the ChatGPT app's Codex mode without a second review, and whether the app needs a restart to load the plugin.
- `--ignore-user-config` on a managed machine: auth from `CODEX_HOME`, no user MCP servers, and whether the configured provider still applies.
- Programmatic prompts in the ChatGPT app: whether UserPromptSubmit fires for tasks and automation messages, and whether a field marks them as not typed.
- Codex's own memory-writing runs: eva-md's hooks either don't fire for them or the capture guard skips them, and nothing from them reaches the inbox.
- `codex exec`: whether it fires hooks, and whether the transcript's `session_meta.payload.source` exists before the first prompt's hook runs, which would let its prompts be stored as host prompts. Codex has no known paste marker.
- The digest and recall skills: whether Codex loads them through the manifest's `skills` field, and whether each triggers.
- Plugin add and remove signed in to ChatGPT: whether `codex plugin add`, `codex plugin remove` and the marketplace's add and remove ask chatgpt.com too, and, on a network that holds that request, wait longer than the 30 seconds its list of every plugin waits.
- The MCP server: whether Codex loads the plugin's `.mcp.json` through the manifest's `mcpServers` field, the folder and environment it gives the server, whether `--disable plugins` keeps plugin servers out of `codex exec` update runs, whether it asks to approve the server or its first call, whether it shows server instructions, and the tool names its PostToolUse payloads carry.

**Both hosts and git (for wiki updates)**
- Detached jobs: a 60-second setsid job started from a hook finishes after quitting the Claude desktop app and, separately, the ChatGPT app.
- Signing approvals (for example from a password manager) for the daily commit: the number of approvals for a signed commit plus an SSH push from a detached job (target 1, accept 2), with no retry after a decline.

<!-- v1 docs/verification.md 047 -->
## Live checks

These were run by hand on real hosts. The recall checks used synthetic test chats: each planted a few made-up facts in one chat, then asked for them in another with tools and the host's own memory turned off, so the answer had to come from eva-md's brief.

Passed:

- **Claude Code CLI.** Recall of all planted facts in a new chat, after forced compaction (`/compact`), and after `/clear`. The answer after `/clear` was slow because of the model's reasoning effort; the hook had already delivered the facts.
- **Codex CLI and cross-host.** Facts planted in Claude Code were recalled in a new Codex chat, in a Codex chat started by a clear, and again in that chat after forced compaction.
- **Claude Code desktop app.** Recall at startup and after `/compact`. After compaction the context also held the host's own compaction summary, so this check does not prove eva-md was the only source.
- **Install through the native CLIs.** Claude Code's strict plugin validation and Codex's plugin validator passed. After the user approved the hooks in Codex `/hooks`, all seven eva-md hooks showed as trusted and enabled, and `eva-md doctor` reported no FAIL checks.
- **Transcript recovery on real hosts.** Replies missed by a hook were recovered from the known transcript without duplicates. Typed prompts in real Claude Code transcripts carry the `promptId` field that recovery relies on.
- **Codex tool features.** Against the real codex 0.166.0, with no model call, `codex features list` lists all 30 names an update run may disable, and `codex features --disable <each> list` exits 0 with nothing on stderr and shows each one off.
- **Codex plugin inventory.** Against the real codex 0.166.0, signed in to ChatGPT, through a proxy that accepts connections and never answers: `codex plugin list --json` waits 30 s for chatgpt.com, then warns and exits 0, even with its remote plugin features off, while
  `codex plugin list --json --marketplace=<name>` answers at once, with no request, for each local marketplace, and so do `codex plugin marketplace list --json` and `codex features list`.
  In a Codex home not signed in, `plugin marketplace add`, `plugin add`, both lists, `plugin remove` and `plugin marketplace remove` made no request, the eva-md entry from `--marketplace=eva-md` matched the one from the list of every plugin, and that list doesn't show a plugin whose marketplace was removed.
  With eva-md's marketplace folder deleted there, the marketplace list, the list of every plugin and `--marketplace=eva-md` exit 1 with "- `eva-md` at <folder>: marketplace root does not contain a supported manifest" on stderr, a line for each marketplace that can't load, naming the folder with its
  symlinks resolved as it was when added, while `--marketplace=` with another local marketplace and `features list` still exit 0.
  `plugin remove eva-md@eva-md`, even for a plugin never added, and `plugin marketplace remove eva-md` both exit 0, after which the lists succeed.
  Putting the folder back lets them succeed with the plugin still there, enabled or disabled as it was, and `plugin add` then enables a disabled one.
- **Releases on GitHub.** The first releases, 2026.11.2 and 2026.11.2.2, were cut with `scripts/release.sh` and published by the release workflow: the signed tag, the lookups of CI's run and the release's, `gh run watch`, `gh release download`, `SHA256SUMS`, `gh attestation verify` and the arm64 binary's version all passed.
  A check by hand in a new folder matched, with both scripts identical to the tagged tree, the tag's and the commit's signatures verified on GitHub and `releases/latest` redirecting to 2026.11.2.2.
- **Releases cut from GitHub.** Cut a release ran a dry run on 2026-11-04, whose plan passed and whose approval, tag, publish and check jobs were skipped, then cut 2026.11.5 and 2026.11.6 from main.
  In each release the plan job ran `scripts/release.sh --plan` under the workflow's token on GitHub's runner and named the tag and the notes since the previous tag; the approve job waited in the `release` environment until the maintainer approved, and
  `gh api repos/heyNag/eva-md/actions/runs/<id>/approvals` lists who did; the tag job pushed an annotated tag on the planned commit, unsigned as GitHub reports it, with the maintainer, who started the run, as its tagger; the release workflow, called through `workflow_call`, built, verified, attested
  and published the six assets; the notes end with the line linking the run; and the check job verified the five files' provenance with `--source-ref refs/heads/main --source-digest` and the arm64 binary's version.
  2026.11.6 was also checked by hand in a new folder: `SHA256SUMS`, the provenance of all five against `release.yml` on main at the planned commit, `eva-md 2026.11.6` from the arm64 binary, both binaries built with Go 1.26.0, `install.sh`, `uninstall.sh` and `LICENSES.txt` identical to the tagged tree, and `releases/latest` redirecting to it.
- **The install command from a release.** Piped from `curl` into a terminal, with setup asking on `/dev/tty`, it set up a wiki in an internal repo on a self-hosted GitLab that `config.env` named, and pushed its signed first commit.
  Its plugin step then stopped at Codex's list of every plugin, before anything was installed; install no longer asks for that list (see "Codex plugin inventory").
  With 2026.11.3 the install command finished there: both plugins registered, and the stable binary, the link and the install record in place, as the uninstall preview then listed.
  The uninstall script with `--apply` then stopped at Claude Code's marketplace (see "Claude Code marketplace removal").
- **Claude Code marketplace removal.** Against the real Claude Code 2.1.316 in a throwaway home: `plugin marketplace add <folder>` declares the marketplace in user settings, and `plugin install eva-md@eva-md --scope user` and `plugin uninstall eva-md@eva-md --scope user` add and remove the plugin.
  Run from the home folder, where Claude Code reads the user settings file as project settings too, `plugin marketplace remove eva-md --scope user` removes the user settings' declaration, but Claude Code still lists the marketplace as another scope's, and a second such remove fails with "Marketplace 'eva-md' is not declared in user settings.
  Omit --scope to remove it from all scopes.".
  Run from `/`, the same remove takes it off the list, and without `--scope` it goes from either folder.
  Every install and removal command of both hosts succeeds from `/`.
  Uninstall on the Mac above, run from the home folder, stopped at exactly that, so host commands now run from `/`, and the marketplace is removed without `--scope`.

<!-- v1 docs/verification.md 050 -->
## Not verified yet

- **Codex desktop app.** Recall in a new chat and after compaction have not been checked in the Codex desktop app.
- **Real use.** A stretch of everyday use with zero hook errors and capture p95 under 50 ms, measured on an installed build.
  The doctor's hook times row now records each event's p95 in real use, without the process's start or the heartbeat's own save; reading it after a week, with the benchmark's figure for what it leaves out, is still to do.
  So are the day's sweep on real chats, and the capture-freshness check on real stores of each host, which may lay out their folders other than the offline tests assume.
- **Live updates.** No update has run live yet: the live resume test with the update on, the live accuracy runs, and a real `eva-md doctor --live` on each host are still to do.
- **The daily commit on real hosts.** A day's signed commit and push from a detached job, and the number of signing approvals it takes.
- **A wiki on a self-hosted GitLab.** Setup has made one from a release (see "The install command from a release"); a day's commit and push, a live update on each host, and the instance guard refusing another machine's wiki are still to check. Offline tests use a fake ssh and an unreachable host only.
- **The digest and recall skills.** `claude plugin validate --strict` with the skills, Codex loading them through the manifest's `skills` field, and each skill triggering on both hosts.
- **Knowledge pages and recall in real use.** No live update has promoted a knowledge page yet, and the update rules at version 4 have not been run by a real model or by `eva-md doctor --live`: that a model keeps what a web page said out of observed knowledge is enforced only by the validator, and that it copies no credential only by redaction and the daily commit's secret scan.
  The recall floor is tuned on fixtures only; its precision on a real index, whether recalled facts help a chat, and whether real system pages make the right projects siblings, have not been measured.
  Whether Claude Code runs the PostToolUse hook for WebSearch, which its matcher now names, has not been seen live.
- **Code refs in real use.** A real digest on each host citing code by folder at the commit it read, and reading show's stderr lines before a page changes; the brief's count and the prompt's note timed on an installed build, in a large repo.
- **Host signals.** The entrypoint each Claude surface sets, and that the desktop app and editor extension never report an SDK value (one chat per surface, then its doctor row), the paste wrapper in a real hook payload, and whether `codex exec` can be told apart.
- **Removing captured chats on a real inbox.** `eva-md inbox clear` has run only in temporary homes: not yet on a Mac with real chats, a chat still open in the session removed, or a search index the daily job is building at the time.
- **Setup with real gh and glab.** Real glab has run only on one self-hosted GitLab, in the runs from a release (see "The install command from a release"), where its create of a private repo was refused and it read an internal repo's visibility.
  Real gh, the choice of gh or glab for a GitHub Enterprise server, and the no-sign-in read of a repo on another host are unchecked: offline tests use fake scripts only, and the no-sign-in read is checked only to fail closed.
- **A deleted install folder.** Install, uninstall and the doctor with eva-md's Codex marketplace folder deleted have run against the fake Codex only, with the real Codex's own commands checked by hand (see "Codex plugin inventory"); Claude Code with its marketplace folder deleted is unchecked.
- **Install, upgrades and uninstall from a release.** The install command from a real release has run on one Mac only, where setup's health check and Codex's hook approval after it were not recorded, and the uninstall script has run from one there only up to the removal of Claude Code's marketplace; `eva-md upgrade` has run only against the local stand-in.
  An Intel Mac is unchecked: no check runs the amd64 binary, since the release workflow's macOS runner and Cut a release's check job run only the arm64 one, `scripts/release.sh` only the one for the Mac it runs on, and the tests serve one build under both names; no test covers `eva-md upgrade`'s pick of the arm64 binary under Rosetta either.
  A release's build attestation is checked when the release is published, by `scripts/release.sh` or Cut a release's check job, and by hand with `gh attestation verify`; the install scripts and `eva-md upgrade` check `SHA256SUMS` only.
- **A release cut from GitHub, off its usual path.** A dry run and two releases have been cut from GitHub (see "Releases cut from GitHub" above).
  Still to check: the plan's failures as annotations on the run; GitHub refusing the tag job's push with the workflow's token when main's workflow files changed after the planned commit; the release workflow's concurrency group inside a called run, with two runs at once; and, after a failure that
  follows the tag job's push, re-running the run's failed jobs, with the tag job finding origin's tag and going on to publish.
  `scripts/release.sh`'s `--source-ref refs/tags/<tag>`, added after the first releases, is still to check on a release from a Mac.
- **An upgrade over an installed plugin.** Install re-runs each host's add command when the plugin folders change, as this release's do; whether each host accepts it, and the fallback that accepts a registration that still verifies, are checked with fake hosts only.
- **A burst of parallel tool calls.** Whether Codex waits for its async PostToolUse hook, and how long a burst of 5 parallel tool calls waits for the session lock on each host, have not been timed.
- **Codex `mcp__` tool lines.** The Codex plugin's matcher includes `mcp__.*`; the tool names Codex reports for MCP calls have not been seen in a real payload.
- **The MCP server on real hosts.** Claude Code's `/mcp` showing eva-md connected with three tools, `claude plugin validate --strict` with `.mcp.json`, and whether the first call of each tool asks permission; Codex loading the plugin's server after its hooks are approved again, the tool name in a
  real PostToolUse payload, and the folder and environment it gives the server; a fact planted in an earlier chat found through `eva_search` in a new chat on each host; no eva-md tool in a live `eva-md doctor --live` run on either host; and no `eva-md mcp` process left after quitting either app.

<!-- /v1 -->

## Since the split

No change yet.
