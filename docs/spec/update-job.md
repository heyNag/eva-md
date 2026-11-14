# Background update: the job, failures and the run log

- **Component:** Updater: the job
- **Paths:** `internal/update`, `cmd/eva-md/update.go`
- **Status:** v1 as built, frozen on 2026-11-14

The blocks below are v1's design and verification text, moved here word for word when the two documents were split; [INDEX.md](INDEX.md) lists every spec.

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

<!-- v1 docs/verification.md 019 -->
- **Update job** (`internal/update`, `cmd/eva-md`, `internal/paths`).
  With a fake runner, a temporary home, wiki and inbox: a pass over hook-shaped entries writes the thread page, the project's now.md and the index files, which okf validates with no finding, and moves the watermark to the end of the file; with the real hooks capturing the chat,
  `eva-md update --foreground` and a canned answer give a page `eva-md brief` shows as a fresh Now, and a detached run logs to the day's log, not the terminal.
  A pass whose output changes nothing advances the watermark once and is not run again (five runs, one model call); it still stamps the page's `as_of` and sources, so the brief then shows the Now as fresh, an older chat's such pass adds only its session to the sources, and one that moves neither writes nothing.
  A save into a folder the wiki can't write fails as `wiki_error`: the watermark stays, the host backs off 5, 10 and 20 minutes, three in a row don't quarantine the session, and once the folder can be written the next run succeeds.
  A failed save's reason tells a conflict, a wiki lock still busy, a file system error and okf's refusal of the text apart, and a page edited by hand between the read and the save is read again, keeping the edit.
  A watermark that can't be saved stops a pass before its model call; one that can't be saved after the call is recorded as `failed (watermark)`, so the cap still counts the call and later runs spend none on the chunk.
  A job killed with SIGKILL in the middle of its save (checked to have died of the signal) leaves no page, the watermark unmoved, no record and the update lock free, and an orphaned `.tmp-okf-*` file is left out of the page, the overview and the index and ignored by the wiki's .gitignore.
  A second job exits at once while one runs.
  A new lock that 8 goroutines, released together, try to take at once is taken or busy for each, never an error, over 500 lock names (`TestTryLockOnANewLockIsTakenOrBusy`, `internal/paths`).
  The 41st model call of a day stops with `budget`; the cap counts each host on its own and holds a manual run too, a skip and a missing CLI count no call, and once a host is stopped its remaining files wait whole; a stop known before a session file is opened (the budget, a hold, a backoff, or a
  quarantine of the session today) leaves the file unopened, while with no stop, or for a person's run, which a hold doesn't stop, it is opened; `rate_limit`, `host_error` and `cli_missing` back off 5 minutes, then 10 and 20, without counting toward the session; `auth` and `mcp_failed` hold the host
  until a successful manual run or the next day; three invalid outputs quarantine a session, the next day's one retry fails and its batch is skipped as `skipped (quarantined)`, and a successful manual run clears a quarantine; `updates = false` stops with `paused` and writes nothing.
  Locked, human-verified and damaged pages are skipped with their reason, without a call, and the watermark still moves; `verified: true` and a block mapping, which okf reads no entry from, count as verified.
  A chat over 60 KB whose second pass fails continues its group on the next run from the watermark's `more`, though its tail holds no prompt.
  The window grows past 4 MiB only while the chunk has room, never to a whole large backlog.
  A branch switch splits a session into groups on their own pages, skipping a closed group too small to update; a turn whose assistant checks out a branch stays whole on its prompt's branch, and the rest of a group a pass stopped in that holds only events is skipped rather than left waiting.
  The off-the-record state carries from one pass to the next, within a run and, through `Prefix`, across runs.
  The wiki's schema version and instance are checked before any call.
  A run starts at the effort the day's runs showed the model accepts, a lower configured effort is asked as it is, and the next day starts at the top again.
  The input names the branch's last commit subjects from git in the chat's worktree, or "unknown" once the folder is gone, and a new thread's input shows its parent's Now, which its page names.
  The dry run prints the same bytes twice, names the environment's variables without a value, shows a placeholder for the features a Codex run disables, and changes nothing.
  The run log and the job's log hold no entry or output text, whatever the outcome.
  `eva-md update` exits 0 on a stop and 1 on a failed pass, prints no summary line when the job can't start, and the usage line names it.
  A rest too small to process waits with the file's size in the watermark, which the triggers and the brief read, with no record in the run log and nothing written by a dry run, and the pass that takes it once a reply arrives clears it.
  One job drains several sessions named with `--session`, by id or by the id their inbox file holds, and leaves the others; `--dry-run` takes one.
  The run log's derived states have unit tests too: the start effort, a fallback from a refused model never lowering the default model's start while a plain default run accepted without the flag still does (`TestStartEffort`), the backoff ramp (`wiki_error` included), the quarantine count (which a
  conflict, a wiki error or an unsaved watermark never adds to) and the pruning of logs older than 14 days.
  Knowledge pages through the whole job (`TestPassWritesTheKnowledgePages`): a page saved before a thread save that fails keeps the watermark, and the rerun writes no second page, counting a duplicate; in a project with a commit, the page cites the code at the branch's HEAD and the recall index lists
  its path (`TestPassCitesTheCodeAtHead`); the recall index and the root index line are written and okf finds nothing; a supersede shows the entry in the input, keeps the History and writes the Timeline change line; a page locked by hand is left byte for byte, with its log line, no Timeline line and `no_change`.
  The day's sweep drains, after the named sessions, the 5 newest sessions idle an hour or more with a backlog, passing over a file with nothing pending, a chat its catch-up record marks as a program's, a named session and one still changing, records the day so a second sweep that day takes nothing,
  and the next day takes the rest (`TestSweepTakesIdleBacklogsOnceADay`); 5 newer sessions of a project the config excludes, one whose project can't be told and a chat the baseline left take no place, so an older idle backlog is swept the first day, and neither the excluded nor the untold sessions
  reach the drain, while the chat left is listed apart, ahead of the 5, and recorded as `failed (left)` with no model call, so the doctor names it (`TestSweepPassesOverSessionsTheDrainCantWorkOn`); more chats left than the sweep lists, whose watermarks can't be read, all newer than an idle backlog,
  leave that backlog drained the same day with the only model call, the 16 newest idle an hour or more recorded and the oldest, and one changed within the hour, passed over (`TestSweepListsLeftChatsApartFromItsSessions`), and a chat left older than the sweep's 5 sessions is recorded too
  (`TestSweepListsALeftChatOlderThanItsSessions`); a chat left in a project where no chat starts, with no other backlog, is recorded by each day's sweep, so the doctor names it on each of three days (`TestSweepRecordsALeftChatEachDay`); the chats left the sweep records are the 16 newest idle an hour
  or more of any project, as known-limitations.md says: 16 left in one project, all newer than the only one left in another, take every place each day, and that one goes unnamed (`TestSweepListsTheNewestLeftChatsOfAnyProject`); a job that finds the lock busy leaves the sweep due
  (`TestBusySweepStaysDue`); a job given a chat start's day, the day after its own clock's and the one it recorded, takes the sweep on that day, and a second job given it takes nothing (`TestSweepIsClaimedOnTheChatStartsDay`); chat starts a day apart, taking turns, sweep once a day between them,
  judged alike by the hook and the job, while a day recorded earlier, more than a day ahead or unreadable leaves the sweep due (`TestSweepAcrossTimeZonesRunsOnceADay`); `eva-md update --sweep` drains an idle session alone and takes the sweep once a day, with the job's clock pinned a second before
  midnight so its two runs share a day, `--sweep=<YYYY-MM-DD>` takes it on that day, and a day that isn't one is refused (`TestUpdateSweepsOnceADay`, `TestUpdateCommandErrors`).

<!-- v1 docs/verification.md 020 -->
- **Update job, later fixes** (`internal/update`, `cmd/eva-md`, `internal/health`).
  A closed chat's last short exchange reaches its page; a prompt at the end of the file waits for its reply, also when only events, or nothing, come before it in the rest of a group a pass stopped in, so the chat's closing reply is sent with it (`TestPromptAtTheEndWaitsForItsReply`,
  `TestAnsweredLeavesOutAnUnansweredPrompt`, `TestContinuedRestLeavesALonePromptWaiting`); chats captured before updates are set aside once, by install or the first job, a last line still being written left for the update, a dry run before it saying so and writing nothing
  (`TestBaselineSetsAsideChatsFromBeforeUpdates`), and install's baseline leaving the work to a job that holds the lock (`TestBaselineWaitsForNoJob`); a folder of the project's slug whose now page can't be read is left to its owner (`TestADamagedNowPageKeepsItsFolder`), and a project without a remote
  that took the folder with its key's hash then finds it again on later passes, while another project without a remote doesn't take it (`TestAProjectWithoutARemoteFindsItsHashFolderAgain`); the words for all projects count for a new rule only where no negation of scope, with no word of the rule
  between, turns them around, and name the scope of a change in any sense (`TestRuleScope`, `TestRuleScopeOfAnAdd`, `TestValidateRuleChanges`); a claim rests on the user's words only through the same word, or one the start of the other from 4 letters, and never through a negative contraction such as
  don't (`TestRestsOn`); a folder whose watermarks can't be written has its chats listed in the record and recorded as failed, never worked through, while a chat begun later is processed, and they are set aside once the folder can be written (`TestBaselineLeavesAFolderItCantWrite`); watermarks or a
  project folder that can't be read at the first try are left too, set aside with no model call once readable, while a chat begun since is processed (`TestBaselineLeavesWhatItCantRead`); install says how many it left, and that the doctor names each once an update has listed it, as the day's sweep does
  for up to 16 (`TestSetAsideSaysWhatIsLeft`); a chat changed in the first try's own second, a stamp that can't be read, a file that can't be opened and a folder deleted since, and the dry run once a folder is readable again and before the first try (`TestBaselineFolderEdges`,
  `TestBaselineLeavesWhatItCantRead`); a chat set aside since no longer named by the doctor (`TestBaselineLeavesAFolderItCantWrite`, `TestUpdateRows`); chats left reported by the doctor without counting as failures in a row (`TestUpdateRows`); `cli_missing` backs off, and only `auth` and `mcp_failed`
  hold; the job lists a file only when its watermark says something is pending, and records an unreadable or past-end watermark as `failed (watermark)`; a project's first thread page claims its folder with its now.md; the recorded CLI path is preferred and passed to the runner, with a PATH lookup as
  the fallback, and the dry run prints it; inferred Open questions are labelled `Unattributed:` in the page, the brief and search.
  `doctor --live`'s runner (`Live`) is checked against fake CLIs for success on both hosts and each failure class, writing no state.

<!-- /v1 -->

## Since the split

No change yet.
