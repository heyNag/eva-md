# Scope, roadmap, risks and borrowed ideas

- **Component:** eva-md as a whole: what v1 covers and what it risks
- **Paths:** `docs/known-limitations.md`
- **Status:** v1 as built, frozen on 2026-11-14

The blocks below are v1's design and verification text, moved here word for word when the two documents were split; [INDEX.md](INDEX.md) lists every spec.

<!-- v1 docs/design.md 036 -->
## v1 scope

### In

- One Go binary, eva-md (CLI, hooks, updater, maintain), plus okf v0.8.0 through a single adapter.
- Static plugins for Claude Code (CLI, desktop Code tab local and SSH) and Codex (CLI, ChatGPT app Codex mode), installed only through the hosts' plugin CLIs from local marketplaces.
- Releases on GitHub for Apple silicon and Intel Macs, cut with the Cut a release workflow or `scripts/release.sh`; `install.sh` and `eva-md upgrade` install or upgrade from them, and `eva-md uninstall` and `uninstall.sh` remove eva-md, never the wiki.
- Capture:
  - prompts and final replies
  - one-line tool records (edits, commands, reads, searches, web fetches and MCP calls, successes and failures; never output)
  - plan updates where the host provides them
  - Claude compaction summaries, model switches and API-error markers
  - redaction, off the record and the exclude list
  - desktop spare sessions deferred to the first prompt
  - subagent and non-typed prompts kept apart from your words
- Limited transcript catch-up for missing turns in known sessions, plus gap markers.
- Local JSONL inbox, 0600, never committed; `eva-md inbox clear` removes whole sessions from it, and without `--apply` previews what it would remove.
- Code-built brief for startup, clear, resume, fork and compact, plus the first-prompt fallback and the hash-triggered refresh, live git, freshness lines, fencing and per-host caps.
- `eva-md search` over the inbox, the thread pages, the system pages and the knowledge pages, run by the agent, with refs that `eva-md show` resolves.
- A rebuildable search index of the inbox, which the daily maintain and `eva-md index` keep and search reads beside its scan.
- A read-only MCP mode of the binary, `eva-md mcp`, with `eva_search`, `eva_show` and `eva_status`, which both plugins declare.
- Background update of the per-branch thread Now section, the standing rules and the knowledge pages by the chat's own host and model (headless, JSON schema), with code validation, accountable items, idempotent apply and catch-up.
- Wiki: init, thread pages, rules pages, a project now.md built by code, system pages with their map and names index, knowledge pages with their map and recall index, generated sorted index files, a log.md with a line for each page created or changed, and a schema file with the schema version, the instance id and when it was created.
- Pointers to system pages in the brief and on prompts that name a system, and a short memory reminder on prompts.
- Knowledge a prompt strongly matches, fenced, at most 3 facts a prompt; the project's constraints in startup and clear briefs; and the recall skill in both plugins.
- How far the code a system page cites has moved since it was written, measured in local clones: in the brief's and the prompts' pointers, in `eva-md show`, in `eva-md system status` and the daily job's code status, and in the doctor.
- Daily commit with one signing attempt and the push right after, with retries, remote and instance guards, and a secret scan.
- doctor (PASS, FAIL or UNKNOWN), a one-line start notice on Claude, problem notices on both hosts, and an opt-in live probe.
- Tests: the deterministic resume suite, the accuracy suite, hook contracts, redaction, okf adapter goldens, git and packaging; the scope suite and an automated live resume test are planned.

All of this is built except what is marked planned above. The live resume test with the update on, the live accuracy runs and the live checks listed in [verification.md](../verification.md) are still to run, by hand.

### Out

- Claude Desktop Chat tab, regular ChatGPT chat, Claude Code cloud sessions, and the Codex IDE extension.
- Local models, vector databases, daemons or sleeping jobs, model calls on every turn, automatic deletion, history backfill, telemetry and auto-update.
- Writing host settings files, CLAUDE.md or AGENTS.md. GitHub or GitLab APIs, apart from setup running gh or glab to create or check the wiki's repo (so no open PRs). Running tests at chat start.
- Committing raw captures, per-chat transcripts or quotes of your words in pages.
- okf's registry, hub sync, vendor bundles, scope layers, governance tiers, scorecards, agent instruction block and MCP server, for knowledge pages too.
- Homebrew or another package manager, builds for Linux or Windows, and checking a release's build attestation in the scripts or `eva-md upgrade`.

---

## Roadmap

| Stage | What it covers | Status |
| --- | --- | --- |
| **Capture and resume** | The Go module with okf pinned; the hook dispatcher, redacted inbox, redactor and identity resolver; the okf adapter with all its protections; `eva-md init` and guided `eva-md setup`; both plugins installed through the hosts' own CLIs, with uninstall and rollback; capture of every entry kind, transcript recovery and off the record; all brief variants with the fallback and refresh, live git and freshness lines; heartbeats, the doctor and notices; `eva-md brief`, `eva-md show` and `eva-md search` over the inbox. | Built. What is checked is in [verification.md](../verification.md). |
| **Wiki updates** | The updater for both hosts (including the Codex tool-use rejection), the validator, the renderer through the okf adapter, triggers and catch-up, project now.md, index files, the maintain job, the daily signed commit and push with all guards, and `doctor --live`. Done when the live resume test passes with the update on, the accuracy suite run live on each host meets every enforceable obligation with zero speaker or date errors and misses at most 2 content obligations, and a week of daily use needs no manual step except the daily signing approval. System pages, standing rules and pointers ship in this stage too. | Built and checked offline. The live checks in [verification.md](../verification.md) are pending, so it is not done yet. |
| **Self-hosted git and managed machines** | Init of a wiki on a self-hosted GitLab, install, managed-settings detection, a proxy-aware live probe, and push retries when the server is out of reach. Done when the doctor is green or every FAIL or UNKNOWN is explained, and the instance guard is shown to refuse another machine's wiki. This closes v1. | Built and checked offline, with a fake ssh and an unreachable host. Setup has made a wiki on a self-hosted GitLab from a release, and the install command has installed eva-md there; the doctor, a day's commit and push and the instance guard on such a wiki are pending, so it is not done yet. |
| **Search index** | A rebuildable index of the inbox for `eva-md search`, with BM25 ranking within its tiers, the scan of what it lacks, the full scan when it finds nothing, the daily maintain and `eva-md index`. | Built and checked offline; its size and build time on a real inbox are still to be read from the doctor. |
| **MCP server** | `eva-md mcp`, a read-only MCP server over search, show and a project status, declared in both plugins, with its deadlines, caps, off switch and the doctor's rows. | Built and checked offline. The live checks in [verification.md](../verification.md) are pending. |
| **Knowledge and recall** | Knowledge pages the update promotes, supersedes and drops, with their map and recall index; recall of strongly matching knowledge on prompts; the brief's constraints; search, show and `eva_status` over the pages; the doctor's row and repair, maintain's step; and the recall skill. Each piece ships with its own test, and recall with its own latency case in the benchmark, which must not slow the hooks or the resume. | Built and checked offline, at the user's request rather than after two weeks of daily v1 use (see "Changes to decisions"). The recall floor is tuned on fixtures; real use and the live checks in [verification.md](../verification.md) are pending. |
| **Releases, install and upgrade** | GitHub releases cut with the Cut a release workflow or `scripts/release.sh` and built, checked, attested and published by the release workflow; `install.sh`, `uninstall.sh` and `eva-md upgrade`; the install record, the link on PATH and `eva-md uninstall --all`. | Built and checked offline: the release script against a temporary origin and a fake gh, the workflows by reading them, and the scripts and `eva-md upgrade` against a release stand-in. Releases cut from a Mac and, since 2026.11.5, from GitHub are published on GitHub; the install command has installed eva-md from one, and the uninstall script has run from one up to the removal of Claude Code's marketplace. `eva-md upgrade` and a whole uninstall against a real release are pending. |

---

<!-- v1 docs/design.md 038 -->
## Risks

| Risk | Mitigation |
| --- | --- |
| Codex hook trust lapses after an update, and capture stops silently. Trust covers more than the command: a manifest-only change invalidated it in live CLI checks. A tool that rewrites `~/.codex/config.toml` (where trust is stored) can also reset it. | Byte-identical plugin folder; a needs-trust flag shown in the next Claude brief; heartbeats compared with Codex activity; the doctor; Codex's own startup warning. How the desktop app shows the trust prompt is still an open host question. |
| A Codex update run can still use tools (unified exec can't be disabled), and the read-only sandbox can read the whole disk, so injected text could pull local secrets into a page. | Disable each tool feature on eva-md's list that `codex features list` names; reject any run whose event stream shows a tool; refs must point to batch entries; redaction on output; optionally ignore user config. |
| Prompts you didn't type (scheduled tasks, /loop, subagent reports, cross-session messages) get stored as your words and promoted as "stated". | Detect them by payload field or wrapper shape; tag `agent_id` payloads; tell a program-run Claude chat by its entrypoint, store its prompts as host prompts, and have the update pass over it with no model call and the brief never take it as the previous chat, which the doctor shows per event; later, have the doctor count suspicious prompts. No reliable signal for scheduled tasks, /loop, subagent reports or cross-session messages on Claude has been observed yet (see Open host questions); if none exists, accept the risk: these sources are rare in normal use. |
| Desktop spare sessions, untrusted folders or a failed SessionStart deliver no brief. | The first-prompt fallback, keyed by a marker file; to be checked by hand in both desktop apps. |
| On current Claude models the plan tools are off by default, so the "open plan" part of the Claude brief is usually empty. | The brief and tests never rely on plan entries; Now items and raw exchanges carry the next steps. |
| The local Claude marketplace loads in place, so a half-written or deleted folder breaks hooks in every new Claude session at once. | Write only on change, swap with renames, never delete while installed, uninstall in order; the doctor checks `readFromFolder` and `errors`. |
| Background updates use your subscription quota on large models, more so at the default highest effort. | Effort configurable per host and per model; 60 KB input cap; triggers only at chat boundaries and thresholds; tiny backlogs skipped; 40 model calls per host per day; `updates = false` stops them; budget shown by the doctor. |
| Headless flags or authentication change in a host release, or the subscription login can't be read from a detached job. | Only the update degrades and the resume is unaffected; `doctor --live` catches it; per-machine flag overrides are planned; a notice after 3 failures. |
| `--tools ""` removes the tool `--json-schema` relies on, so every Claude update returns nothing. | It works on Claude 2.1.311; `doctor --live` will recheck it; fallback to JSON in the text result, validated in code. |
| Managed-policy hooks on a managed machine still run inside the Claude update job despite `--safe-mode`. | `EVA_MD_INNER=1` is checked first in every eva-md hook; the doctor reports what it can see of managed policy. |
| Model output drifts, misattributes speakers or drops open items. | Strict schemas checked by the host and again in code; speaker and date checks; accountable item ids; the watermark is held on failure; the accuracy suite as a release gate. |
| okf behaviour shifts on upgrade: input rules got stricter in 5 releases, one patch release broke the API, and the serializer or index format could change. | Pin (v0.8.0); upgrade only on need, on a branch, behind golden and live-wiki round-trip tests; anything okf lacks is covered in eva-md. |
| okf's direction drifts toward its hub, vault and registry, with one maintainer writing about 88% of commits. | eva-md uses only a small core; the adapter is the only coupling; the wiki stays plain markdown, so the core can be swapped. |
| A power loss after the watermark advances loses a page update. | fsync the folder before advancing the watermark. |
| One stray symlink, unreadable .md or frontmatter-less .md in the wiki fails LoadBundle or Validate for the whole wiki. | The doctor names the file; Validate runs only in the doctor and the commit, never in hooks or the resume. |
| okf's body check makes a page with your own horizontal-rule sections impossible to save. | The page becomes read-only (damaged) with a notice explaining the fix; no retry loop. |
| okf raises its Go version again, forcing a newer toolchain at upgrade time. | Accept it at upgrade time; the toolchain is local. |
| Go 1.27's encoding/json (backed by v2) changes edge cases in okf's parser. | `go test ./pkg/okf` and the adapter goldens run under the Go 1.27 toolchain. |
| Redaction misses a new secret format. | The inbox is stored only on this Mac (0600) and never committed, though the background update sends each chat's new entries to that chat's host CLI and its model; pages are short summaries; redaction runs again on the update's output and on the staged diff; quarantine; private repos. |
| Stored prompt injection persists through memory. | Fence and factual label; escaping; no-tool updates; assistant text never shown as yours; decisions you didn't make demoted to proposals; no knowledge observed from a turn that read the web, as far as its pass can see; recall of another project's knowledge only when it shares a system page or the prompt names it. |
| Several signing prompts in a day (for example from a password manager). | One signing attempt per day; the push runs right after in the same process; the approval count is still to be measured (see Open host questions); signing never disabled. |
| Transcript formats drift and break catch-up. | Capture never depends on transcripts; catch-up fails soft with fixtures; gaps reported as markers; three turns in a row with no recognized row record drift, which the doctor and a notice report. |
| Codex PostToolUse for long unified-exec commands arrives late or is cancelled at session end, so the last tool line can be missing. | Tool lines are a convenience; the brief marks missing exit codes as unknown. |
| Your MCP servers start on every Codex update run (slow; a required one that fails aborts the run). This is accepted by default (decision 17). | The `mcp_failed` reason and a notice; `codex_ignore_user_config` can switch it off for one machine. |
| Codex loading a plugin's `.mcp.json` through the manifest's `mcpServers` field is inferred from OpenAI's own plugins, not observed with eva-md, and the folder and environment Codex gives the server are not documented. | Hooks and the skill load either way; only the tools would be missing, which the live check catches. The server takes its home folder from the binary's path without HOME, looks for git in the system's folders without PATH, and says when it fell back to the general area, so the agent passes `cwd`. |
| Claude Code asks permission for an MCP tool unless a rule allows it. | eva-md never writes host settings; the README says how to allow `mcp__plugin_eva-md_eva-md__*` in your own Claude settings. |
| Codex shows no sign of using server instructions, so a hook problem may not reach the agent there. | `eva_status` says it; the Claude brief's notice and the doctor report it as before. |
| One idle MCP server per open chat. | No idle work but a parent check every 5 s, a 64 MiB soft memory limit, and an exit with its chat. |
| Threads keyed by branch pile up after merges. | Nothing is deleted. Working out idle, merged and gone threads from git, and folding idle ones in the overview, are planned. |
| Scope creep. | A written v1 scope; every stage gated on "installed and used daily". Knowledge pages and recall were built before two weeks of use, at the user's request, so the promotion bar and the recall floor are judged on fixtures until real use shows them. |
| Large monorepos make git slow at chat start. | `--no-optional-locks`; an 800 ms deadline; "unknown" on timeout; no fetch. |

---

## Ideas borrowed from ai-memory

akitaonrails/ai-memory (v3.2.8, MIT) was evaluated both on its own and as the core under eva-md, and not chosen as an engine because of fit:
- It needs a server that is in practice always running. Hooks can't reliably stop it.
- It keeps sessions, handoffs and history in SQLite, which the markdown can't rebuild.
- It commits unsigned on every event, with no switch to turn that off, in an ID-named layout. Your repo would hold only a copy.
- By default it downloads a local model, imports past sessions and deletes pages. These can be turned off.
- It can't be pinned safely: security fixes go only to the latest release, and migrations are forward-only.

These patterns are still worth taking into eva-md's own code:

| Pattern | Where it applies | Source in ai-memory |
| --- | --- | --- |
| Skip Codex's own memory-writing runs | Capture guards. Done | `crates/ai-memory-core/src/scaffolding.rs:104-136` |
| Ask Codex for hook trust through `codex app-server` hooks/list | Doctor. Done | `crates/ai-memory-cli/src/commands/codex_hook_trust.rs` |
| Keep the hook command identical across releases | Codex packaging. Done: the whole plugin folder stays byte-identical | `plugins/claude-code/scripts/ai-memory-hook` |
| Transcript filter rules: Claude meta rows, task notifications and compaction markers; Codex environment, permissions and AGENTS.md text inside user turns | Catch-up reader. Done | `crates/ai-memory-workstream/src/transcript.rs` |
| One shared fixture table for remote-URL identity | Identity resolver tests | `crates/ai-memory-core/fixtures/remote_identity_cases.json` |
| Redact before truncating; match across zero-width characters; strip ANSI, bidi and NUL characters | Redactor. Done | `crates/ai-memory-core/src/sanitize.rs` |
| When picking the previous chat, skip prompts that are only acknowledgments and flag a trailing unanswered question | Brief builder | `crates/ai-memory-hooks/src/router.rs:5145-5287` |
| Call a host CLI safely: cleared environment with an allowlist, a private temp folder, capped output and a timeout | Background update. Done | `crates/ai-memory-llm/src/codex.rs`, `cursor_agent.rs` |
| A CI check that released schema migrations never change | Schema versioning | `scripts/check-migrations-immutable.sh` |

Its own benchmark also supports eva-md's choice of search without a model. Plain full-text search beat its default local embedder: hit@1 was 0.857 against 0.766, and the median query took 11 ms against 157 ms.

---

<!-- /v1 -->

## Must keep

The must-keep items of [findings.md](../v2/findings.md) this component touches, numbered from K01 in each list, and the tests that hold them.
An item that tests hold only in part ends with what of it no test holds, as `(untested: ...)`.

| Id | Item | Tests |
| --- | --- | --- |
| PRD-K01 | Hooks fail closed and stay fast: exit 0, panics contained, Codex Stop gets {}, no model or network (untested: no model or network, and the p95 targets) | internal/hook:TestMalformedMissingAndPanicOutput, internal/hook:TestDeadlinesAndNoLateCapture, internal/hook:TestRunOutOfTimeIsMarkedFailed |
| PRD-K02 | Redaction before disk is the union of the baseline and current rules, fails closed, and runs again on model output and the staged diff | internal/redact:TestNoSecretVersion1Removed, internal/inbox:TestRedactionTruncationAndFailClosed, internal/update:TestValidateRedactsCapsAndEscapes |
| PRD-K03 | The inbox is the word-for-word truth, append-only and private, never committed; the indexes are caches | internal/inbox:TestConcurrentAppend, internal/inbox:TestPermissionsAndRefs, internal/wikigit:TestLeavesOutWhatIsNeverCommitted |
| PRD-K04 | Resume works with no model, from the inbox and live git | internal/resume:TestCrossHostStartupClearAndLineage, internal/hook:TestAutomaticCrossHostResumeAndOneFallback, cmd/eva-md:TestBriefAndShowCLIResumeAcrossHosts |
| PRD-K05 | Provenance is enforced by code: speakers, dates and refs from entries, tags checked, other prompts never the user's | internal/update:TestValidateSpeakerTags, internal/update:TestValidateResolvesRefs, internal/update:TestPastedSpans |
| PRD-K06 | The watermark moves only after a validated, synced save; compare-and-swap; history kept; protected pages never written | internal/update:TestSaveFailureKeepsTheWatermark, internal/update:TestKillMidSave, internal/okfadapter:TestConcurrentCreateAndCAS |
| PRD-K07 | Injected memory is fenced and labelled as data, and cross-project recall is scoped | internal/resume:TestFence, internal/resume:TestFreeTextCannotForgeSpeakerHeaders, internal/knowledgepage:TestRecallScope |
| PRD-K08 | Off the record, exclusions, markers only while recovery is behind, and an unreadable config captures nothing and says so | internal/hook:TestOffRecordAndResume, internal/hook:TestGuardsAndOrdering, internal/hook:TestDeferredRecoveryKeepsOnlyEventNames |
| PRD-K09 | Update runs cannot recurse or use tools | internal/update:TestChildEnvironmentIsAnAllowlist, internal/update:TestRequestsThatCannotRunAreRefused, internal/update:TestClaudeArgumentsMatchTheDesign |
| PRD-K10 | Instance and remote are checked on every write, commit and push; never a force or rewrite; signing unchanged; explicit staging with a scan | internal/update:TestWikiSchemaAndInstanceAreChecked, internal/wikigit:TestGuardsRefuse, internal/wikigit:TestDivergedRemoteIsLeftAlone |
| PRD-K11 | Codex hook trust survives binary upgrades because the plugin folder stays byte-identical and calls a stable path | internal/install:TestBinaryOnlyReinstallKeepsCodexTrust, internal/install:TestPluginFolderBytes |
| PRD-K12 | One hardened environment for every git call, and the identity cache that keeps capture fast | internal/wikigit:TestEveryGitDropsTheSameVariables, internal/resume:TestLiveGitRunsWithTheReadEnvironment, internal/hook:TestCaptureUsesIdentityCache |
| PRD-K13 | A chat a program runs is told by its entrypoint, so automation never becomes the user | internal/hook:TestHeadlessChatPromptsAreHostPrompts, internal/catchup:TestHeadlessReadsTheRecord |
| PRD-K14 | Tests never touch the real machine or depend on its speed; the fact scripts seed live evaluation (untested: the temporary home, and independence from speed) | internal/testgitenv:TestClearUnsetsOnlyGitConfigAndIdentityFromTheEnvironment, internal/update/accuracy:TestScripts |
| PRD-K15 | Only eva's name shows, uninstall never removes the wiki, and destructive commands preview first | internal/install:TestUninstallAll, cmd/eva-md:TestUninstallPreviewRemovesNothing, cmd/eva-md:TestInboxClearPreviewsThenRemoves |

## Since the split

No change yet.
