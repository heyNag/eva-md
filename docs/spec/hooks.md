# Capture and the hook dispatcher

- **Component:** Hook dispatcher
- **Paths:** `internal/hook`
- **Status:** v1 as built, frozen on 2026-11-14

The blocks below are v1's design and verification text, moved here word for word when the two documents were split; [INDEX.md](INDEX.md) lists every spec.

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

<!-- v1 docs/verification.md 004 -->
- **Hooks** (`internal/hook`).
  Every event on both hosts, the folder and remote guards, off-the-record and back on, no compaction summary stored after a chat went off the record and came back, on both hosts, kept through a resume or compact start and stored again after a clear (`TestCompactionSummaryAfterOffRecordIsNotStored`),
  also when recovery found the control and neither the PostCompact summary nor the transcript's summary row is stored (`TestCompactionSummaryAfterRecoveredOffRecordIsNotStored`), input, work and output deadlines, malformed input, panics, writes that arrive after a deadline, parallel hooks, the rule
  that a pending recovery pass stores only event names, and the briefed marker saved only with time to spare before the deadline, for a brief and for a refresh.
  The hash-triggered refresh on both hosts: a Now updated after the first prompt arrives with the second, the third gets nothing and leaves the marker as it was, and no more than 2 refreshes follow a brief; refreshes after a compact brief, with the count running on through compactions and a /clear
  starting it again; a marker from before the count, with `"refreshed": true`, counting one refresh used; the marker keeping the project's wiki folder found under another name, the only folder a refresh then reads.
  A first chat gets the footer alone.
  The update triggers, through an injected spawn on both hosts: SessionEnd and PreCompact spawn an update for the session, and a chat off the record none; the other events none of their own, though the chat's own file, just changed, holds 120 KB unprocessed and another session of the project idles
  with a backlog, so a tool line never takes the Stop trigger and a prompt never the catch-up; Stop spawns one at exactly 60 KB unprocessed and not a byte before, after the reply is stored, also with a watermark inside a larger file; a prompt spawns one 30 minutes after the file last changed and not a
  second before, with 600 bytes unprocessed and not 599, the file read before transcript recovery stores a reply a hook missed, not when an update found that backlog too small, and never for a subagent's or scheduled prompt or a built-in command; SessionStart catches up the project's other sessions
  idle 2 minutes and not a second less, newest first, at most 5 in one update, leaving out a session processed to its end, one whose rest was found too small and another project's, and reading only the session files the brief lists (`TestCatchUpReadsOnlyTheBriefsListing`); a session that start-of-chat
  transcript recovery has just appended a missed reply to is taken though its file has just changed; the chat's own backlog goes first in that update when its file, as it was before the hook, had not changed for 2 minutes and not a second less, on a resume and on a startup with the same id, within the
  5, and its next prompt (with a real clock) spawns nothing more, but not right after a compaction, nor when processed to its end or found too small; Codex Stop's `{}` is written before the spawn; `updates = false`, or a config without a wiki, spawns no update, while with no wiki the day's maintain
  still starts (`TestUpdatesOffSpawnsNothing`); a Claude chat a program runs asks for no update of its own at SessionEnd, PreCompact, a Stop past 60 KB, a prompt after a gap or the start-of-chat catch-up, while its start still catches up another idle session
  (`TestProgramRunChatAsksForNoUpdateOfItsOwn`); and a share folder that is not an absolute path spawns nothing and opens no log, though, from the hook's folder, it names an executable stand-in.
  A real spawn, from the test binary run as a hook process with a stand-in for the installed binary: the job runs in its own session (its session id is its pid), with stdin from /dev/null, its output in the day's update log (0600), `/` as its folder and the update allowlist with `EVA_MD_INNER=1` (no
  API key, CLAUDECODE or git config variable); the hook's stdout and stderr reach their end while the job still runs, and the job goes on after the hook has exited.
  Each run's heartbeat records the time the run took, from an injected clock asked once a run with the moment Run began, and a real time without it (`TestHeartbeatRecordsEachRunsTime`), and a run out of time records its own once its output is written, with whether its transcript lies elsewhere, while the capture Run gave up on writes no heartbeat (`TestLateRunRecordsItsTime`).
  A run stopped by the folder, remote, wiki, state, temp or memory-run guard, or in a desktop spare, stamps its time alone and writes no heartbeat, while the inner and remote guards and a captured chat stamp nothing (`TestUncapturedRunsStampTheTimeOnly`); the stamp is rewritten in place, keeping its
  inode, and never through a second link or a symlink (`TestUncapturedStampIsWrittenInPlace`); a transcript outside `~/.claude/projects` or `~/.codex/sessions` is marked in the heartbeat, also with its path written through a link to the home, and one inside them, through macOS's `/private` alias too
  or with its path written through a link to the home, or none is not (`TestHeartbeatSaysWhenTheTranscriptIsElsewhere`); the heartbeat marks a transcript elsewhere exactly when recovery reads none of a reply the hooks missed, in the plain folder, with its path written through a link to the home, under
  a `~/.claude` linked from a dotfiles folder, through a link from inside the folder out of it and in a moved folder (`TestRecoveryAndTheHeartbeatJudgeTheFolderAlike`).
  With HOME itself a symlink, which isn't supported, and eva-md's folders from it, every event on both hosts exits 0 and gives no brief, and the hooks capture nothing, recover none of a reply they missed, write nothing, not even the day's attempt marker or a push retry's, and start nothing (`TestLinkedHomeCapturesNothing`).
  A person's chat start asks for the day's sweep with its catch-up, or alone, giving the job the hook's day, until the day the update recorded is the hook's, other events never ask, and the next day's first chat start asks again (`TestChatStartAsksForTheDaysSweep`, `TestResumedChatSpawnsOnce`); a chat a program runs never asks, and updates off asks for nothing (`TestProgramRunChatAsksForNoSweep`).

<!-- v1 docs/verification.md 028 -->
- **Program-run chats, tool lines and hook failures** (`internal/hook`, `internal/catchup`, `internal/update`, `internal/resume`, `internal/health`, `cmd/eva-md`).
  Host prompts for the three SDK entrypoints, and the five interactive values, an empty one and Codex never headless; a record control honoured; a program-run chat's transcript prompt not recovered while its reply is.
  The update job passes over a program's chat as `skipped (headless)`, with no call and its watermark at the end, also for what is appended later (`TestProgramRunChatIsPassedOver`), and over one with no prompts whose catch-up record marks it, but never one with typed prompts
  (`TestCatchUpRecordMarksAProgramRunChat`); a marked one whose backlog runs on past its window is passed over a window at a time, while an unmarked one waits until a window reaches the end of the file (`TestMarkedProgramRunChatIsPassedOverAWindowAtATime`, with the window's bound lowered for that one
  backlog); the catch-up record's mark read through catchup's own naming (`TestHeadlessReadsTheRecord`); a person's chat holding host prompts is still updated in the same pass, in a group's rest and when its typed prompts lie before the watermark (`TestPersonsChatWithHostPromptsIsUpdated`, `TestScanPrefixFindsPrompts`, which also finds a typed prompt in a line longer than one read).
  The brief and lineage pass over a program's chat (`TestBriefPassesOverAProgramRunChat`, `TestLineagePassesOverAProgramRunChat`), also on a branch it checked out (`TestProgramRunChatThatSwitchesBranch`), but not a person's chat on the branch it checked out (`TestBranchSwitchIsNoProgramRunChat`).
  A config that exists but can't be read is said in one line at a chat start on both hosts, with nothing captured, and nothing is said on other events, in eva-md's own run or with no config (`TestHookSaysAtChatStartThatTheConfigCantBeRead`).
  Every new tool-line form and a token across the 200-character cut; MCP arguments and a fetched URL's user part, fragment and query parameters left out when their names say they carry a credential, and an MCP argument that is a URL cleaned the same way, also one named as an address with a space in its path, and a WebSearch kept by its name alone, without its query or results (`TestToolLines`).
  The 20-run heartbeat, and failed runs for a marker error, an identity failure and a timeout.
  Drift recorded and cleared on both hosts, rewritten from the third blind turn, reset by a new baseline, never for turns with no reply or with a recognized row; a damaged record reported as unreadable.
  The doctor's thresholds and notices.
  A burst of 70 newer chats a program ran doesn't push the person's chat out of the brief, which reads and lists all 71 files (`TestProgramRunChatsDontCountTowardThe64`), and the files that don't count stop at 64 and at 2 MiB (`TestProgramRunFilesBeyondThe64AreBounded`).
  `BenchmarkProgramRunFiles` measures what they cost the startup brief: 64 files of 32 KiB of short tool lines and replies with quotes and line breaks, 2 MiB in all, as an agent loop leaves them, add about 26 ms to a brief of about 19 ms over 64 person files on an Apple M1 Max with other jobs running, about 13 ms a MiB, three times a MiB of long plain strings.

<!-- /v1 -->

## Since the split

No change yet.
