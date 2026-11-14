# Background update: the job, failures and the run log

- **Component:** Updater: the job
- **Paths:** `internal/update`, `cmd/eva-md/update.go`
- **Status:** v1 as built, frozen on 2026-11-14

The blocks below are v1's design text, moved here word for word when the two documents were split; its verification text moved on 2026-11-14 to [update-job-checks.md](update-job-checks.md), and [INDEX.md](INDEX.md) lists every spec.

<!-- v1 docs/design.md 011 -->
## Background update

The job, `eva-md update` and the triggers that spawn it are built and tested offline, with fake host CLIs and a stand-in for the spawned binary; no live update has run yet.

1. A trigger (see "Triggers") spawns the installed binary, `~/.local/share/eva-md/bin/eva-md`, as `eva-md update --session <host>:<id>`, with one `--session` for each session the start-of-chat catch-up names, once the hook's output is written.
   The child runs in a session of its own (setsid), out of reach of the signals a host sends the hook, with stdin from /dev/null, stdout and stderr appended to `<state>/logs/update-<YYYY-MM-DD>.log` (0600), the update's allowlisted environment with `EVA_MD_INNER=1` (see "Model selection"), and `/` as its folder, so it holds none of the hook's pipes.
   The hook starts it and releases it, never waiting; a spawn that fails is ignored, and the next trigger tries again.
   `eva-md update --pending` drains every session with a backlog, and `--foreground` runs attached, as a person does.
2. With `updates = false` in the config, stop at once (`paused`). Take the one global lock, `<locks>/update.lock`, without waiting; if another job holds it, exit 0, and the next trigger tries again. The hooks' session lock is never taken. Set aside, once, the chats captured before updates (see "Job control"). Check the wiki's schema version and instance against the config.
3. Drain the session's backlog oldest first, unless the budget, a hold or a backoff of its host, or a quarantine of the session today, already applies: then its file is not opened (see "Job control").
   A program-run chat (see "A program's chat" under "Job control") is passed over as `skipped (headless)`: the watermark moves past the window with no model call.
   A session whose project the config now excludes is left as it is, with no call and its watermark where it was.
   Decode a bounded window past the watermark, take the next (session, branch) group and let `Build` cut its oldest chunk, at most 60 KB.
   A group under one exchange or 600 characters waits for more entries, and the watermark records the file's size then, so the triggers and the brief leave the session alone until something is appended.
4. Read the thread page by its path.
   A locked, human-verified or damaged page is skipped with its reason, and the watermark moves on.
   Otherwise check the daily cap of model calls, a hold or backoff of the host and the session's quarantine, save the watermark as it stands (one that can't be saved stops the pass before the call), resolve the effort, and run the host CLI headless in an empty run folder.
   The model is chosen when the pass is planned.
   The timeout scales with effort, from 180 s to 600 s.
5. Validate.
6. Save the knowledge pages the pass kept changes for (see "Knowledge page"), then the rules pages, then the thread page.
   Apply the update to the page, render it and save it through the adapter's compare-and-swap; on a conflict, read the page again and apply again, at most twice.
   A page whose rendered body equals the body read back keeps its body, and only its `as_of` and `sources` are stamped, when the pass moves them.
   After a save, regenerate the project's now.md and the index files of the folders the write touched.
7. Move the watermark to exactly the end of the chunk, after the page's folder fsync when it changed, and append the pass to the day's run log. Then take the next chunk.

Details are in the next section.

<!-- v1 docs/design.md 025 -->
## Failures, budget and quarantine

- **Reasons.** A failed run is recorded with one of `cli_missing`, `auth`, `rate_limit`, `host_error` (a network, server or command-line failure), `mcp_failed` (a required Codex MCP server failed), `model_rejected`, `timeout`, `invalid_output` and `tool_used`; a failed save adds `page_refused` (okf
  refused the page's text), `wiki_error` (the wiki lock stayed busy for a minute, or a file system error such as a folder that can't be written, a full disk or a failed fsync) and, for a page that kept changing under three saves, `conflict`, which counts toward nothing; `watermark` marks a pass whose
  watermark can't be saved after its call, which counts toward nothing but keeps its call in the budget, and a watermark that can't be read, sits past the end of its file or inside a line, recorded with no call under the id the file name holds; `left` marks a chat from before updates that couldn't be
  set aside yet (see "No backfill"), recorded with no call under that id, which counts toward nothing and neither counts toward nor ends a row of failures.
  A stop is not a failure: `paused`, `budget`, `held`, `backoff` and `quarantined` end a job early, and the next trigger tries again.
- **Budget.** `daily_cap` counts model calls per host per day (default 40; an explicit 0 makes none), not jobs; a pass's retries after a refusal are part of its call. Reaching it stops the host's calls for the day (`budget`).
- **Holds.** `auth` and `mcp_failed` hold every update of that host until the next day or a successful manual run (`held`), and count toward no session.
- **Backoff.** `rate_limit`, `host_error`, `wiki_error` and `cli_missing` hold the host's updates for 5 minutes, doubling with each one in a row up to 60 (`backoff`), and count toward no session. A missing CLI makes no call, so one spawn whose PATH lacked the CLI no longer stops the host for the day.
- **Quarantine.** `invalid_output`, `tool_used`, `page_refused`, `timeout` and `model_rejected` count toward the session. After 3 in a row it is quarantined for the day (`quarantined`); the next day it gets one retry, and if that fails too, its watermark moves past the batch with `skipped (quarantined)`, so the triggers and the brief stop reporting it. A pass that validates, or a skip, ends a row.
- **Manual runs.** A person's run (`--foreground`) is not stopped by a hold, a backoff or a quarantine, and when it succeeds it clears them; the budget and the kill switch still apply.
- **Kill switch.** `updates = false` in the config stops every update at once with `paused`, and every trigger: no hook spawns one. A dry run still works, since it makes no call.
- A one-line notice at the next chat start reports 3 failures in a row, a held host or a quarantined session (see "Health check").
- The resume never depends on the update, because the brief carries the raw tail.

## The run log

One append-only file per day, `<state>/updates/<YYYY-MM-DD>.jsonl` (0600), one record per pass: a model call, or a skip that moved the watermark without one.
A record holds `at`, `host`, `session`, `project`, `thread`, `outcome` (`ok`, `no_change`, `skipped` or `failed`), `reason`, `chunk_bytes`, `pages`, `duration_ms`, `tokens_in`, `tokens_out`, `model`, `effort` (asked) and `effort_used` (accepted), `rules_version`, `boundary_to_saved_ms` (from the
chunk's newest entry to the saved page: the time to a fresh Now, change 13), `calls`, `manual`, any model or degraded note, and the validator's report counts, among them `pasted`, `unattributed`, the grounding misses by class (`misses.numbers` included) the rule changes (`rules.added`, `edited`,
`dropped`, `general`, `refused`, `stale`, `duplicates`, `ignored` and `read_only`, after the save has moved those it passed over) and the knowledge changes (`knowledge.added`, `superseded`, `dropped`, `refused`, `stale`, `duplicates`, `near_duplicates`, `ignored`, `over_cap`, `read_only` and `paths_dropped`, after the save has moved those it passed over); `rules_version` is 4.
`pages` counts the knowledge, rules and thread pages written, and a pass that wrote a knowledge page is `ok`.
It holds counts, names and reasons only, never entry or output text.
Every state the job derives is a scan of today's records, and yesterday's for a backoff or a quarantine: the budget, holds, backoff, quarantine and the effort a model starts at.
There is no other state file.

Each record also names the trigger that started its job (`trigger`: `session_end`, `compact`, `stop_size`, `prompt_gap`, `chat_start`, `sweep`, `manual`, or `other` for a name this release doesn't know); records written by 2026.11.7 and earlier, and those of a job started with no name, have none.
A record keeps the model the host's output says answered (`model_reported`), which only Claude Code gives, from `modelUsage`: the model with the most output tokens, when its name is one a model flag could take.
For an answer the checker refused whole, it keeps the rule and the place (`rejection`: `rule`, one of `not_object`, `missing_key`, `extra_key`, `not_string`, `not_string_or_null`, `not_enum`, `not_array`, `array_size`, `not_integer`, `dates`, `carried` or `other`, and `place`, a path of the schema's
key names with each index written `[]`, such as `items[].kind`, or `output`), never the answer's text.
A job that finds the update lock held appends one record to a file of its own beside the day's run log, `<state>/updates/busy-<YYYY-MM-DD>.jsonl` (0600): outcome `busy`, no host, its trigger, and the sessions it was started for as `session_hashes`, the first 16 hex digits of a SHA-256 of host and id, from which no id can be read back.
Only `eva-md status --updates` reads that file, so however many busy exits a day has, the run log's newest records, which the start-of-chat notice, the doctor, status and the MCP server read, and every older release's reading of it, are as they were.

---

<!-- /v1 -->

## Must keep

The must-keep items of [findings.md](../v2/findings.md) this component touches, numbered from K01 in each list, and the tests that hold them.
An item that tests hold only in part ends with what of it no test holds, as `(untested: ...)`.

| Id | Item | Tests |
| --- | --- | --- |
| PGS-K17 | Times keep their own offset, and as_of only moves forward | internal/threadpage:TestItemLineRoundTrip, internal/threadpage:TestItemLineForms, internal/update:TestNoChangePassStampsAsOf |
| PGS-K18 | A project's first page write creates now.md with its remote under the same lock, claiming the folder | internal/update:TestFirstThreadPageClaimsTheFolder, internal/update:TestADamagedNowPageKeepsItsFolder, internal/update:TestAProjectWithoutARemoteFindsItsHashFolderAgain |
| UPM-K11 | Refs name physical inbox lines; the watermark moves exactly past what a pass covered; a prompt waits for its reply | internal/update:TestDecodeKeepsPhysicalLines, internal/update:TestPromptAtTheEndWaitsForItsReply, internal/update:TestChunkedChatContinuesAcrossRuns, internal/update:TestBuildChunksAtExactly60KB, internal/update:TestWaitingRestIsRecordedInTheWatermark |
| UPM-K17 | Chats from before updates are never summarized unasked, and a watermark that cannot be saved costs no call | internal/update:TestBaselineSetsAsideChatsFromBeforeUpdates, internal/update:TestWatermarkFailureStaysWithinTheBudget |
| UPW-K02 | The inbox watermark is the truth about pending work | internal/inbox:TestPendingSkipsAWaitingBacklog, internal/update:TestSessionsDrainsTheNamedSessions |
| UPW-K03 | The watermark moves to the batch's end only after a validated save, and is saved first so a bad one costs no call | internal/update:TestSaveFailureKeepsTheWatermark, internal/update:TestKillMidSave, internal/update:TestWatermarkFailureStaysWithinTheBudget, internal/update:TestNoChangePassRunsOnce |
| UPW-K04 | Knowledge, rules, then the thread page are saved; a no-change pass may stamp as_of; a conflict is read and applied again | internal/update:TestPassWritesTheKnowledgePages, internal/update:TestNoChangePassStampsAsOf, internal/update:TestConflictIsRetriedOnTheEditedPage, internal/update:TestSaveFailureReasons |
| UPW-K05 | A project folder is proved by its now.md remote, created under the first page's lock | internal/update:TestFirstThreadPageClaimsTheFolder, internal/update:TestADamagedNowPageKeepsItsFolder |
| UPW-K06 | Locked, verified or unreadable pages are never written; an untagged item is a person's | internal/update:TestProtectedPagesAreSkipped, internal/update:TestValidateProtectsHandWrittenItems |
| UPW-K11 | The run log and the job log hold counts, names and reasons, never text | internal/update:TestRunLogHoldsNoEntryText, internal/update:TestTriggerName, internal/update:TestSessionHash |
| UPW-K13 | Program-run chats cost no call, excluded projects are left alone, and old chats are not worked through unasked | internal/update:TestProgramRunChatIsPassedOver, internal/update:TestMarkedProgramRunChatIsPassedOverAWindowAtATime, internal/update:TestBaselineSetsAsideChatsFromBeforeUpdates |
| UPW-K14 | A prompt waits for its reply, and a cut group is continued through the watermark | internal/update:TestPromptAtTheEndWaitsForItsReply, internal/update:TestBuildContinuesAGroup, internal/update:TestChunkedChatContinuesAcrossRuns, internal/inbox:TestMarkKeepsMoreWithTheOffset |
| UPW-K15 | The dry run changes nothing and is byte-stable; validation is pure; the guarantees check any pass | internal/update:TestDryRunIsByteIdenticalAndShowsNoValues, internal/update:TestValidateIsDeterministic, internal/update:TestValidateKnowledgeIsDeterministic |
| UPW-K16 | The wiki's schema version and instance are checked before any write | internal/update:TestWikiSchemaAndInstanceAreChecked |
| EVL-K12 | The run log holds counts and names only, never chat text | internal/update:TestRunLogHoldsNoEntryText |

## Since the split

No change yet.
