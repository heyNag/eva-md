# Capture and the hook dispatcher

- **Component:** Hook dispatcher
- **Paths:** `internal/hook`
- **Status:** v1 as built, frozen on 2026-11-14

The blocks below are v1's design text, moved here word for word when the two documents were split; its verification text moved on 2026-11-14 to [hooks-checks.md](hooks-checks.md), and [INDEX.md](INDEX.md) lists every spec.

<!-- v1 docs/design.md 009 -->
## Flows

### Capture (every hook event)

1. The host runs the plugin hook, which calls the binary with the payload on stdin. On Codex the command ends with `|| echo '{}'`, so Stop always gets valid output even if the binary is missing.
2. **Guards.** Exit at once (printing `{}` on Codex Stop, nothing elsewhere) in any of these cases:
   - `EVA_MD_INNER=1`
   - a config that exists but can't be read, which a chat start says in one line (step 6 of "Resume: new chat or /clear")
   - `CLAUDE_CODE_REMOTE=true`
   - the path or remote is excluded
   - the path is the wiki, a state folder or a temp folder
   - a Codex memory-writing run (`cwd` ending in `.codex/memories`, or a prompt starting with `## Memory Writing Agent`), so Codex's own background memory jobs are never stored as your chats (pattern from ai-memory; not yet observed live, see Open host questions)

   In a desktop spare folder (`~/.claude/spares`), only the model marker is stored.
   In a chat that is off the record, only markers are stored.
   A run that stops at the folder, remote, wiki, state, temp or memory-run guard, or in a spare folder, writes no heartbeat but stamps `heartbeats/<host>/uncaptured.json` with its time alone, never the folder, session or event, rewritten in place without fsync: the host still keeps that chat, and the
   capture-freshness check reads the stamp's first record as a hook of the host's that ran (see "Health check").
   The time is written in UTC at one width, over the last stamp and never truncating it, with no lock, so runs that stamp at once, as the async hooks of parallel tool calls do, leave one whole stamp.
   The hook-failure count never reads it.
   The `EVA_MD_INNER` and `CLAUDE_CODE_REMOTE` guards stamp nothing.
3. Resolve the project and branch from the payload's `cwd`, not `CLAUDE_PROJECT_DIR` (which stays at the start folder when Claude enters a worktree).
   Cached per worktree (see Identity resolver).
   Then a catch-up pass (at most 100 ms) reads this session's known transcript up to its end.
   Until it has, the event keeps only its fixed event name and no brief, because unread rows may hold an off-the-record control a hook missed.
   A record control in a prompt you typed is honoured before this pass, so it never waits on it.
4. **UserPromptSubmit.**
   - Check for "off the record" and "back on the record".
   - If the prompt came from a scheduled task, /loop, a subagent, another session or a program-run Claude chat, store it as a `host_prompt` event. Otherwise redact it and append a prompt entry.
   - If this session has not been briefed, return the brief (the first-prompt fallback). If it has, return the hash-triggered refresh when the thread's Now or the standing rules changed.
   - Add pointers to the system pages the prompt names, the knowledge the prompt strongly matches and, on the first prompt and then every `reminder_every` prompts, the memory reminder (see "Per prompt" under Recall and fencing). None go out in a program-run chat, off the record, for a prompt you didn't type or a built-in command, or with a wiki of another instance.
   - If the session's inbox file last changed 30 minutes or more before this prompt, with 600 bytes or more unprocessed, spawn an update for the session: the burst before the gap counts as a finished chat.
     The file's modification time and size come from one stat taken before anything is appended, transcript recovery included; its watermark is read only when both thresholds hold.
     Host prompts, built-in commands and the record controls spawn nothing.
5. **PostToolUse / PostToolUseFailure** (async): append one tool or plan line and print nothing. The output of an async hook would reach the model a turn later.
6. **Stop.**
   - Append the reply. A missing or null reply is stored as a `gap: missing final reply` event; the catch-up pass recovers the reply from the transcript when it can.
   - If 60 KB or more of the session's inbox file is unprocessed once the reply is stored (its size minus the watermark: one stat, and the watermark is read only once the file is that large), spawn an update for the session. This is the trigger for desktop chats that never end.
   - On Claude print nothing: additional context there would continue the conversation. On Codex print `{}`.
7. **StopFailure** (Claude, API errors): append a `stop_failure` event with the error type, so the brief says "no reply (stop_failure: rate_limit)" instead of guessing.
8. **PreCompact.** Append a compact event after the catch-up pass, and spawn an update for the session. Never block, and on Codex never print `continue:false`.
9. **Other events.**
   - PostCompact (Claude): store `compact_summary` as a host_summary entry, unless the chat has been off the record since its last startup or clear (`was_off`, see "Off the record"): then only the compact event.
   - PostModelSwitch (Claude): record `to_model` on the session, including `source=resume`, and print nothing.
   - Interrupt (Codex): append an interrupt marker.
10. **SessionEnd.** Append a session_end event and spawn an update for the session, within 200 ms (a p95 of about 45 ms in the benchmark, the spawn included). Claude's plugin SessionEnd budget is a fixed 1.5 s. Codex's is 1 s by default and 3 s at most.
11. Every event updates its heartbeat and exits 0.
    A spawn the event asked for starts after its output is written (see "Background update"), so it never delays or holds the output.
    The first event of the local day also spawns maintain (see "Daily commit and push").
    Each event's heartbeat keeps its last 20 runs, each marked failed on a panic, an identity failure, a failed session lock, marker read or write or inbox append, or a capture that ran out of time, which is recorded after the output, within 100 ms.
    Each run also records its time (`duration_ms`), from the hook's start to just before its heartbeat is saved, so it leaves out the process's start and the save itself; a run out of time records its time once its output is written.
    Each run also records what it held back, with no text: when a chat is off the record, or transcript recovery has not read to the end, a person's prompt (with the chat's first brief while it is still owed), a reply, a tool line or a chat start's brief, as `held`, with `held_by` `off_record` or
    `recovery`; a chat that recovery finds was put off the record, when it cannot save that boundary, holds back as `off_record`.
    A brief still owed is counted at each run that holds it back, its start's and each prompt's, so the count is of runs, not of chats.
    A run that can add text to a chat, a chat start or a person's prompt, records `delivered`: its kind (startup, clear, resume, fork, compact or prompt), the routes that added something (rules, constraints, state, exchanges, recall, pointers, reminder), the characters added, whether the state shown
    predated the newest captured exchange, whether an update was pending, whether the prompt was built to carry the chat's first brief, and whether the run passed its deadline and delivered nothing.
    Beside its last 20 runs each heartbeat keeps `week`, a day's counts for each of the last 7 days in UTC: runs, failures, run times in 16 fixed buckets, holds by kind and cause, deliveries by kind and route, the first-prompt briefs that delivered text, the runs that passed their deadline, and the
    briefs given in time at a new chat's start, a clear, a compaction or a chat's first prompt that delivered text, with how many of them showed the thread's state and how many were stale or had an update pending.
    A resumed or forked chat's brief, which shows only what changed, is not among those briefs.
    The file stays under 12 KiB.
    Two runs of one event that end together may each keep only their own run in the week, as in the run history, so parallel tool calls can be undercounted.
    The heartbeat also says when the run's transcript lies outside `~/.claude/projects` or `~/.codex/sessions`, the folders transcript recovery reads, as it does once `CLAUDE_CONFIG_DIR` or `CODEX_HOME` moves the host's folder (`transcript_elsewhere`); recovery reads none of that chat's transcript.
    The heartbeat judges by recovery's own rule (`catchup.InStore`): the folder and the transcript's own folder count where their symlinks lead, so a `~/.claude` or `~/.codex` linked elsewhere, as a dotfiles setup links it, is still the host's folder, and a transcript path a host writes through a link
    to the home lies in it: recovery reads such a transcript, opening it by the path its folder leads to, never through a link; a link from inside the folder to elsewhere leads out of it, and a folder not made yet is judged by its path as written.
    A run out of time judges it once its output is written.
    A home that is itself a symlink isn't supported (see "Shared").
    Transcript recovery also records drift: three Stops in a row whose reply was read over only rows it doesn't recognize write `catchup/drift-<host>.json`, rewritten on each such Stop and keeping its first time, until a recognized row clears it; a new transcript baseline restarts the count.
    A chat that is off the record, or whose transcript recovery is behind, spawns no update, and neither does any chat with `updates = false` or a config without a wiki.
    Nor does a Claude chat a program runs (`CLAUDE_CODE_ENTRYPOINT` `sdk-cli`, `sdk-ts` or `sdk-py`): its SessionEnd, PreCompact, Stop and prompt triggers ask for no update of its own, and its chat start leaves its own file out of the catch-up but still takes the project's other idle sessions (see "Triggers").
    The day's maintain starts all the same whenever the config names a wiki path and an instance, or no wiki at all, for its search index step.

**Host differences:**
- **Claude.** The model arrives only at SessionStart (optional, often missing after /clear) and PostModelSwitch. Stop does not fire after an Esc interrupt, so catch-up covers it.
- **Codex.** `model` arrives on every event except SessionEnd.
  `turn_id` arrives only on turn events.
  `transcript_path` may be null.
  The session then keeps the transcript it registered before, under the same rules as an event that names it: a change of project, folder or branch, or the chat going off the record, starts again at the transcript's end, and a transcript that can no longer be opened marks the session missing (`transcript_unavailable`).
  Only a session that never named a transcript gets no recovery; this is counted and reported.
  Codex hooks run only after you trust them in /hooks.

<!-- v1 docs/design.md 019 -->
### Off the record

1. A prompt that starts with "off the record", or contains a line that is only that phrase (ignoring case and punctuation), sets the session's off marker. The prompt itself is not stored, only an off_record event.
2. Capture, catch-up and the update all skip that range. The hook confirms through additionalContext, and on Claude also through systemMessage.
3. "back on the record" clears the marker. Anything said earlier stays saved, and `eva-md inbox clear` removes it with its whole session, after a preview (see "Removal" under "The inbox").
4. The marker holds `asked` for your request, or `1` when recovery finds a control a hook missed. A size limit never sets it: an oversized transcript row leaves a gap event instead.
5. Going off the record also sets the session marker `was_off` (value `1`): the prompt hook's off_record control sets it, and so does transcript recovery when it finds a control a hook missed, also inside an oversized row it scans, or asserts an off_record boundary again, and "back on the record" when the chat's off marker was set.
   Going back on the record keeps it.
   Only a SessionStart whose source is `startup` or `clear`, which begins a new context, removes it; a start while the chat is off the record or while transcript recovery is behind leaves it, and a `resume` or `compact` start never removes it.
   While it is set, PostCompact stores only its `compact` event and never the compaction summary, because the summary restates the whole context, the off-the-record range included.
   Transcript recovery never stores a compaction summary (rows flagged `isCompactSummary` or `repeatsCompactSummary` are skipped).

<!-- /v1 -->

## Must keep

The must-keep items of [findings.md](../v2/findings.md) this component touches, numbered from K01 in each list, and the tests that hold them.
An item that tests hold only in part ends with what of it no test holds, as `(untested: ...)`.

| Id | Item | Tests |
| --- | --- | --- |
| CAP-K01 | A hook exits 0, contains every panic, never blocks the host, and Codex Stop gets {} | internal/hook:TestMalformedMissingAndPanicOutput, internal/hook:TestRunOutOfTimeIsMarkedFailed, internal/hook:TestDeadlinesAndNoLateCapture |
| CAP-K02 | A late capture delivers nothing, spawns included; output has reserved time and comes before any job starts | internal/hook:TestDeadlinesAndNoLateCapture, internal/hook:TestLateCaptureStartsNoMaintain, internal/hook:TestCancelledCallbacksCannotWriteLater, internal/hook:TestBlockedOutputHonorsDeadline, internal/hook:TestSpawnFollowsTheOutput |
| CAP-K03 | EVA_MD_INNER=1 stops capture inside eva-md's own model runs, and every detached job carries it | internal/hook:TestGuardsAndOrdering, internal/hook:TestMaintainJobEnvironment, internal/update:TestChildEnvironmentIsAnAllowlist |
| CAP-K04 | A detached job runs the installed binary by absolute path, in its own session, stdin from /dev/null, output to a log, with an allowlisted environment | internal/hook:TestDetachedJobOutlivesTheHook, internal/hook:TestDetachNeedsAnAbsoluteShareFolder, internal/hook:TestMaintainJobEnvironment, internal/hook:TestJobEnvFollowsGit |
| CAP-K05 | Hooks make no network call and no model call (untested: a program started by its full path) | internal/hook:TestHooksStartNothingOutside, internal/hook:TestHooksInAGitProjectOnlyReadGit, internal/hook:TestHooksInAPartialCloneFetchNothing, internal/hook:TestHookLinksNothingThatConnects |
| CAP-K07 | Text is redacted before it is cut, so a token across the cut leaks no prefix | internal/hook:TestToolLines |
| CAP-K08 | Tool lines hold no output, file content or error; URLs and tool arguments lose their credentials | internal/hook:TestToolLines |
| CAP-K09 | Exclusions are checked on the path as written and as resolved, and remote ones before any marker is written | internal/hook:TestGuardsAndOrdering |
| CAP-K10 | Off the record survives a missed hook: the control is honoured first, held in marker and record, and keeps compaction summaries out | internal/hook:TestMissedOffRecordHookPausesNextCapture, internal/hook:TestCompactionSummaryAfterOffRecordIsNotStored, internal/hook:TestCompactionSummaryAfterRecoveredOffRecordIsNotStored, internal/hook:TestDeferredRecoveryKeepsOnlyEventNames, internal/catchup:TestOffRecordMarksChatAsHavingBeenOff |
| CAP-K11 | A prompt the person did not type is never stored as theirs | internal/hook:TestControlDetectionAndSyntheticPrompts, internal/hook:TestHeadlessChatPromptsAreHostPrompts |
| CAP-K16 | One writer a session: the session lock serializes hooks and transcript recovery | internal/session:TestTryLockTakesTheSessionsLockWithoutWaiting, internal/catchup:TestPruneLeavesTheRecordOfASessionAHookHolds |
| CAP-K18 | Capture p95 under 50 ms and the brief under 300 ms, checked by the benchmark | check:scripts/benchmark.py, internal/spec:TestTheBenchmarkHoldsTheSpecsTargets |
| CAP-K19 | Hook tests never depend on machine speed: injected clock, slack, deadlines and hang guards | internal/hook:TestBudget, internal/hook:TestCancelledCallbacksCannotWriteLater, internal/hook:TestEarlierCallerDeadlineWins |
| CAP-K20 | Each event keeps its run history and a stamp for a run that captured nothing by design | internal/hook:TestHeartbeatsKeepTheLastRuns, internal/hook:TestUncapturedRunsStampTheTimeOnly, internal/hook:TestUncapturedStampIsWrittenInPlace, internal/hook:TestStampsWrittenAtOnceLeaveOneWhole, internal/health:TestCaptureFreshnessCountsTheUncapturedStamp |
| SRC-K12 | Hooks never read or write the index, and the MCP search writes no file and takes no lock | internal/mcp:TestServerWritesNothing, internal/hook:TestHooksIgnoreTheSearchIndex |
| UPW-K01 | Hooks never wait for or share a lock with the job: it starts after the output, detached, and trigger checks cost a stat | internal/hook:TestSpawnFollowsTheOutput, internal/hook:TestDetachedJobOutlivesTheHook, internal/hook:TestStopSpawnsAt60KBUnprocessed, internal/update:TestSecondJobExitsAtOnce |
| STO-K14 | Hooks run no git and no network for maintain: a stat, an exclusive create, and a detached start after the output (untested: a program started by its full path) | internal/hook:TestDailyMaintainStartsOnce, internal/hook:TestLateCaptureStartsNoMaintain, internal/hook:TestPushRetrySpacing, internal/hook:TestHooksStartNothingOutside |
| MCP-K16 | eva-md's own tool calls are captured without their search terms or results; v1 keeps the terms, as for any MCP tool | none yet |
| SET-K13 | Exclusions match the written and resolved path and every parent; path before identity, remote before any marker | internal/config:TestExcluded, internal/config:TestExcludedSymlinkAliases, internal/config:TestRemoteExclusionForms, internal/hook:TestGuardsAndOrdering |

## Since the split

**2026-11-14, EV-10c: what a hook starts and links is held by tests.**
No behaviour changes.
`internal/hook/reach_test.go` holds that a hook starts no program by its name but git, and links no package that can open a connection, in Go, through the system's calls or through cgo.
It holds that a hook runs git only to read, and only at a chat's start, for identity and the brief: with `--no-optional-locks`, fsmonitor and hooks off, lazy fetches off, and `--no-lazy-fetch` on the reads of objects in a partial clone.
The first hook of a chat in a folder that is no repository runs `git rev-parse --show-toplevel` once; a prompt, the push retry and the next day's maintain start no git.
A hook's output and what it stores are the same with a search index that is whole, damaged or gone, and the index's files are unchanged after it.
A program a hook started by its full path would not be seen by these tests.
