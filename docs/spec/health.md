# The health check

- **Component:** init / install / uninstall / doctor: the doctor
- **Paths:** `internal/health`
- **Status:** v1 as built, frozen on 2026-11-14

The blocks below are v1's design and verification text, moved here word for word when the two documents were split; [INDEX.md](INDEX.md) lists every spec.

<!-- v1 docs/design.md 017 -->
## Health check

**Every SessionStart** (and a first-prompt fallback brief) runs quick checks, in this order:
- unknown config keys, since a misspelled key can be a missing exclusion
- a missing root index.md
- the wiki schema newer than the binary
- the wiki instance differing from the config's
- the needs-trust flag, except in the Codex chat whose hook is running, since that hook has been trusted
- the other host's heartbeat, or its stamp of a run in a folder eva-md doesn't capture, against that host's recent chat activity (folder modification times only)
- hook failures: half or more of a host's newest 20 runs in the past 7 days failed, from 4 runs on
- transcript drift on a host (see Capture)
- with updates on, from at most 64 KB of today's and of yesterday's run log: a held host, 3 or more update failures in a row, or a session quarantined
- with commits on, from at most 16 KB of each of the last two months' commit records: a diverged push, a commit failing for more than 2 days, or a push pending or failing for more than 3 days

The first problem becomes one line.

**`eva-md doctor [--json]`** reports PASS, FAIL or UNKNOWN, with a one-line fix, for:
- the binary and config, including unknown config keys
- the wiki:
  - the repo and remote: the origin and every push URL, checked as the daily commit's guard checks them, against the configured `expected_remote`, normalized when it is an SSH or HTTPS address, so a local remote (`file://<real path>`) passes, also one not mounted now, and a push URL that points elsewhere fails (fix: restore the configured wiki origin)
  - the instance
  - the schema
  - the location
  - the folder's mode: FAIL when the group or other accounts have any access, as a wiki adopted from a clone made by hand keeps git's 0755 where init makes a new one 0700, with `chmod 700 <folder>` as the fix
  - the root index.md
  - okf Validate errors, broken links and orphans
  - leftover `.tmp-okf-*` files
- inbox permissions and size
- the search index, from its manifests and the folder listings alone: PASS with what of the inbox it covers, UNKNOWN before its first build, FAIL for a damaged project folder (see "Search index")
- the plugins on each host, through the host CLIs (Claude: `readFromFolder`, `folderVersion`, `errors`); a conflicting eva-md registration is a FAIL, and so is eva-md's Codex marketplace folder missing while Codex has it registered (see "A missing Codex marketplace folder"), with uninstall and then install as the fix.
  A host whose command isn't on PATH is UNKNOWN as not installed on this Mac, with no fix, and gets no Codex trust row
- hooks firing, per host and per event, with the entrypoint the event last ran under, naming a program's chat whose prompts are stored as host prompts
- capture freshness per host, from the modification times of the host's chat folders, never the transcripts: FAIL when chat activity in the past day is more than 5 minutes newer than the newest hook of any event, or the newest stamp of a run in a folder eva-md doesn't capture, PASS when a hook ran in the past day, UNKNOWN otherwise.
  Activity is the time a chat's file last appeared in a folder that holds one.
  In `~/.claude/projects` the check lists the project folders, stats each one and reads only those changed in the past day, newest first, until one holds a chat, so its cost grows with the projects, not the chats; past 16,384 entries it stays UNKNOWN.
  In `~/.codex/sessions` it reads only the two newest date folders by name (`<YYYY>/<MM>/<DD>`), the day before for a chat started near midnight, passing over names that aren't dates and dates more than a day ahead.
  It reads only those default folders; when the host's newest run that named a transcript found it elsewhere, the row says so
- hook failures per host: FAIL when half or more of the newest 20 runs of the past 7 days failed, from 4 runs on; a run fails on a panic, an identity failure, a failed session lock, marker or inbox write, or a capture that ran out of time. The fix asks you to check the disk, the state folder and that git runs from the hook's PATH
- hook times per host, for information only, never FAIL: for each event, the 95th percentile, by the nearest rank, of the times its heartbeat keeps for its runs of the past 7 days (at most 20, so the second slowest of 20), in whole milliseconds rounded up, and how many runs; the times leave out the
  process's start and the heartbeat's own save, an atomic write with fsync of about 11 ms, so they read lower than the benchmark's.
  UNKNOWN before any run recorded its time
- hooks held back, for information only: of the hook runs in the past 7 days on both hosts, what was held back while transcript recovery was behind, and apart, while a chat was off the record, by kind, a brief still owed counted at each run that held it back; left out until a hook that keeps weeks has run
- hooks delivered, for information only: of the runs in the past 7 days that could add text to a chat, by kind, how many added text and how many characters, and how many added each route; of the briefs given at a new chat's start, a clear, a compaction or a chat's first prompt (resumed and forked
  chats left out), how many showed the thread's state and the share whose state was older than the newest captured exchange or that had an update pending; the first-prompt briefs delivered; and the runs that passed their deadline and delivered nothing; left out until such a run is recorded
- transcript format per host: FAIL while `catchup/drift-<host>.json` exists, UNKNOWN when it can't be read, and UNKNOWN, saying that transcript recovery is off, when the host's newest run that named a transcript found it outside `~/.claude/projects` or `~/.codex/sessions`, as when `CLAUDE_CONFIG_DIR` or `CODEX_HOME` moves the host's folder
- Codex trust: asked through `codex app-server` hooks/list where that works (pattern from ai-memory), otherwise inferred from firing, so UNKNOWN until proven. Codex loading no eva-md hooks for an installed, enabled plugin is a FAIL
- managed policy: the Codex hooks feature state; Claude policy (`disableAllHooks` or `allowManagedHooksOnly`) is UNKNOWN and judged by heartbeats
- updates per host: UNKNOWN when eva-md is not installed for the host (fix: run eva-md install, or none when the host's command isn't on this Mac), with updates off, or with no run today or yesterday; FAIL for a hold, 3 failures in a row, a quarantined session, a `failed (watermark)` record or a
  `failed (left)` one (a chat from before updates not set aside yet, which is not counted among the failures in a row), naming up to 3 sessions; UNKNOWN for 1 or 2 failures; PASS otherwise.
  When the failures include `cli_missing` and no usable CLI path is recorded, the fix says to run eva-md install again to record the CLI's path
- the daily commit: FAIL when the newest run was refused or failed, or left files out, with a fix for its reason (a missing signer, a failed or declined signature and a timeout each have their own; a rebase, merge, cherry-pick or revert in progress says to finish or abort it, and unmerged paths to
  resolve the conflicts, each then to run eva-md commit); UNKNOWN with commits off or before the first run
- the push: the newest run that pushed or tried to, so a day with nothing to commit says nothing. PASS for `ok` or `up_to_date`, FAIL for `diverged` or `failed`, UNKNOWN while `pending`, and FAIL once a push has been pending for more than 3 days
- oversize pages and source refs, from the lint's `<state>/lint.json`: FAIL for a thread page over 512 KB or a ref the inbox no longer resolves: one past its file's end, or to a chat this Mac captured whose file is gone while its watermark stays, as a file deleted by hand leaves it, with the fix to restore the file or record the removal with `eva-md inbox clear --session <id> --apply`.
  A ref to a chat in `inbox/.removed`, whatever its line, counts as removed, and one to a chat this inbox holds nothing of, neither file nor watermark, as another Mac's, as in a wiki two Macs share or one set up again after the local data went; the lint keeps both counts (`removed_refs`, `elsewhere_refs`), and the row names them without failing.
  While an inbox project folder, or a session file in it, can't be read in full, such a ref may name a chat in it, so the lint counts it as unknown (`unknown_refs`), never as another Mac's, and keeps the folders' names (`unreadable_inbox_folders`); the row is then UNKNOWN, unless a ref fails it, and names the folders, with the fix to make them readable again
- the system map: FAIL when the map or the folder index, `systems/index.md`, is missing or over 256 KB, when the map doesn't match the pages, or when the index leaves one out; the fix is to write a page again with `--replace`, which rebuilds both
- system names: one of the doctor's two repairs writes the names index, and the repos index with it, again when either is missing, unreadable or out of date, waiting up to 60 s for the wiki lock whatever the doctor's own limit, and says when the bounds left aliases out; UNKNOWN when the lock stays busy.
  An index that would list nothing, the names index once no system page is left or the repos index once no page lists two repos, it removes instead.
  Each row names the index at fault, the names index, the repos index or both, what it costs and whether the doctor writes it again or removes it, each index on its own: a stale names index costs prompts their pointers, or, once no system page is left, may still have the memory reminder name the
  system index, and with a stale repos index recall may show, or leave out, the knowledge of other projects, or, once no page lists two repos, show that of projects that no longer share one.
  Its PASS says which index it wrote again or removed
- system code, from `<state>/code-status.json` (at most 1 MiB), when the wiki has system pages: never FAIL, since moved code, a repo with no clone on this Mac and a map from before code refs are no faults of eva-md.
  UNKNOWN before the first status or for one that can't be read, and while a page's code couldn't be checked (naming the pages by reason: no clone on this Mac, commit not in the clone, git ran out of time, git failed, past the bounds of a pass), a page can't be read or the map lacks the code of pages
  written before it carried code, each with its fix; otherwise PASS, with the pages tied to code, how many are unchanged, those that moved with their counts, those citing a path gone at HEAD, those citing a path their commit lacks, those citing a commit off their clone's branch with nothing counted
  since the fork, and those past their review day, and when something moved, a page cites a path gone at HEAD or one its commit lacks, or a review is due, the fix to ask in a chat for those systems to be digested again
- knowledge, only when `knowledge/` exists, or a recall index or root index line left from it does, and the wiki's schema is schema_version 1 and names the config's instance (`health.WritableWiki`); a `knowledge` that is not a folder gets no row: PASS
  `<N> knowledge pages (<F> facts, <D> decisions, <P> procedures, <C> constraints), <R> retired; index current`, N counting every page and the kinds the active ones, with `, <U> unreadable` when some can't be read.
  The doctor's other repair builds the knowledge map, `knowledge/index.md`, the recall index and the root index's line again from the pages when any is missing or out of date (`knowledgepage.Refresh`, under the wiki lock), and then says `; built again from the pages: <paths>` in place of `; index current`.
  It adds `; past 512 KB, the recall index leaves out the <L> oldest pages` when the bound left pages out, and `; the map is <locked|verified|damaged|other type>, so eva-md leaves it as it is` for a map a person locked or verified, or that can't be read as a map, which is then no fault.
  FAIL in `eva-md status`, which repairs nothing: `<counts>; out of date: <paths>`, with the fix to run eva-md doctor, which builds them again from the pages.
  FAIL when the pages can't be read or the files can't be written, with the fix to check that `knowledge/` and `.eva-md/knowledge.tsv` are a folder and files eva-md can write.
  UNKNOWN when the files are out of date and the wiki lock stays busy
- the MCP server's declaration per plugin host (`claude mcp manifest`, `codex mcp manifest`): PASS when the installed `.mcp.json` is the one install renders, FAIL with the fix `eva-md install` when it is missing or differs, as in a folder from before the server, and UNKNOWN when eva-md is not installed for the host
- the MCP server itself (`mcp server`, doctor only): it starts the installed binary as the Claude plugin does, `eva-md mcp --host claude-code`, from `/` with only HOME and PATH, and checks within 3 s, reading at most 64 KB, that it answers initialize and lists the three tools.
  FAIL when it doesn't answer, as a binary from before the server, or lists other tools; UNKNOWN when the list is empty (`mcp = false`) or no binary is installed.
  The probe names itself `eva-md-doctor` in initialize, so the server answers without waiting for a SessionStart hook, as the probe starts no chat, and the time the row reports is the server's own.
  Whether a host connected the server can't be seen from the command line

The doctor reads at most 1 MiB of each day's run log and 256 KB of each month's commit records.

**`eva-md status [--json]`** runs the same local checks without asking the hosts, the MCP manifest rows included but not the server probe, and never writes: a missing or stale names or repos index, and knowledge files missing or out of date, are a FAIL whose fix is `eva-md doctor`.
It adds the wiki's contents, `<P> projects, <T> threads, <K> knowledge pages`, counted from the folder listings alone, and the update backlog: the sessions with a backlog among each project's 64 newest session files, listing at most 4,096 entries in all and saying "at least" past that, and a FAIL naming unreadable or past-end watermarks.
`status --live` is refused.
**`eva-md status --updates [--json]`** reads the run log and the busy exits of the 7 days that end today, and the config only for the daily cap, and prints, a line each: passes by outcome, skips and failures by reason, answers refused whole by rule and place against all the answers checked, what the
checker changed or refused inside the accepted ones, tokens in and out and call time per pass with a model call (median, 95th percentile by the nearest rank and maximum), the time from a chunk's newest entry to its saved page by trigger, with the day's sweep on a line of its own and records with no
trigger as `unknown`, busy exits with the distinct sessions they were started for and their triggers, each day's model calls per host against the cap, and the passes whose host named the model that answered, how many of them asked for the host's default and how many named a model other than the one asked for, a suffix in brackets such as `[1m]` aside.
It shows only counts, durations, dates, hosts and the outcomes, reasons, rules, schema places and triggers this release writes; any other name a record holds shows as `other`, so no session, project, branch, model or text reaches it.
It writes nothing; `doctor --updates` is refused.

**`eva-md doctor --live`** (opt-in) runs the exact headless update command for each installed host on a built-in two-exchange fixture for an empty page, and checks the answer with the real validator.
It uses the host's default model and the config's effort, stepping down after a refusal as a run does, within the run's timeout.
It starts the CLI a run starts: the path install recorded in `<state>/cli-paths.json`, while that is still the CLI, and otherwise the one on PATH.
It counts toward no budget, holds nothing and writes nothing.
PASS names the time and the effort accepted; FAIL names the reason with its fix, such as `claude auth login` or `codex login` for `auth`.
A host eva-md isn't installed for gets UNKNOWN and no call.

Codex trust is stored in `~/.codex/config.toml`, which eva-md never reads or writes, so a tool that rewrites that file can reset it. The doctor asks Codex itself through `codex app-server` hooks/list when that works, and otherwise infers trust from hooks firing. Claude policy can come from files, MDM or the server, so it is judged by its effects (heartbeats).

<!-- v1 docs/verification.md 035 -->
- **Health, setup and CLI** (`internal/health`, `internal/wikiinit`, `internal/config`, `cmd/eva-md`, `internal/wikigit`, `internal/hook`, `internal/install`).
  Doctor checks and hook-trust parsing, unknown config keys, wiki creation and adoption, and the command-line surface, whose usage line matches the dispatch.
  Every new doctor row in its PASS, FAIL and UNKNOWN cases from planted records (`TestUpdateRows`, the not-installed row and the `cli_missing` fix included), each notice and none when healthy, bounded tail reads, the status rows, the backlog's 4,096-entry limit (`TestBacklogListingIsBounded`) and
  unusable watermarks; `status` never writes and never needs a model (`TestDoctorJSONAndStatusNeverNeedModels`); `doctor --live` with fake CLIs.
  The names check rewrites a stale index, fails with its fix, skips another instance's wiki and reports a busy lock, each row naming the names index, the repos index or both and what each costs, and its PASS which it wrote again, or removed when it would list nothing
  (`TestDoctorWritesTheNamesIndexAgain`, `TestStatusNamesTheReposIndex`, `TestDoctorWithTheWikiBusy`); an index that would list nothing, the names index once no system page is left or the repos index once no page lists two repos, is worded on its own, also beside the other written again, with what it
  costs and the fix that the doctor removes it, in status, which leaves it, and as not removed by a doctor that found the wiki busy, and the doctor's PASS says it removed it (`TestStatusOfAnIndexTheDoctorRemoves`, `TestDoctorWithTheWikiBusy`).
  The system map check fails for a missing map or folder index, either over 256 KB, a page the map or the index leaves out and a page the map lists that is gone, with the write that rebuilds both (`TestSystemMapRow`). 

<!-- /v1 -->

## Must keep

The must-keep items of [findings.md](../v2/findings.md) this component touches, numbered from K01 in each list, and the tests that hold them.

| Id | Item | Tests |
| --- | --- | --- |
| CAP-K20 | Each event keeps its run history and a stamp for a run that captured nothing by design | internal/hook:TestHeartbeatsKeepTheLastRuns, internal/hook:TestUncapturedRunsStampTheTimeOnly, internal/hook:TestUncapturedStampIsWrittenInPlace, internal/hook:TestStampsWrittenAtOnceLeaveOneWhole, internal/health:TestCaptureFreshnessCountsTheUncapturedStamp |
| OPS-K01 | Hosts are registered only through their own CLIs; host settings and trust records are never read or written | internal/install:TestFirstInstallOnTheFakeHosts, internal/install:TestInstallRunsTheAdaptersCommands, internal/health:TestTrustProtocolIsReadOnly |
| OPS-K16 | The chat-start notice is bounded, local and content-free, reading only file tails | internal/health:TestQuickNoticeQuietWhenHealthy, internal/health:TestRecordsAreReadFromTheEnd, internal/health:TestQuickNoticeChecksOnlyOtherHost |
| OPS-K17 | Capture freshness reads folder metadata within a bound, and the uncaptured stamp prevents false alarms | internal/health:TestCaptureFreshnessReadsLargeStores, internal/health:TestCaptureFreshnessCountsTheUncapturedStamp, internal/health:TestCaptureFreshnessReadsCodexsNewestDays |
| OPS-K18 | The trust notice clears only on a real, newer Codex chat start, and the running hook is not warned | internal/health:TestTrustNoticeRejectsSpoofedHeartbeatAndFlag, internal/health:TestFirstTrustedCodexChatHasNoTrustNotice |
| OPS-K19 | The trust probe is read-only, wants exactly 7 events and no duplicate or diagnostic, each host on its own budget | internal/health:TestTrustProtocolIsReadOnly, internal/health:TestPartialTrustIsNotSevenEvents, internal/health:TestDuplicateAndDiagnosticsNeverPassTrust, internal/health:TestStalledInventoryDoesNotHideTrust |
| OPS-K20 | status never writes; the doctor repairs only derived indexes, under the wiki lock, in a wiki of this instance | internal/health:TestStatusLeavesTheCodeStatus, internal/health:TestStatusLeavesTheKnowledgeIndex, internal/health:TestStatusLeavesTheNamesIndex, internal/health:TestDoctorWritesTheNamesIndexAgain, internal/health:TestDoctorWithTheWikiBusy |
| OPS-K21 | Only eva's name is shown, and counts rather than captured content | internal/health:TestFieldReportHoldsNoText, internal/hook:TestRecordsHoldNoContent, internal/update:TestRunLogHoldsNoEntryText |

## Since the split

No change yet.
