# The update job: its offline checks

- **Component:** Updater: the job's offline checks
- **Paths:** `internal/update`, `cmd/eva-md/update.go`
- **Status:** v1 as built, frozen on 2026-11-14

The blocks below are v1's verification text, moved here word for word when the two documents were split, then out of [update-job.md](update-job.md) on 2026-11-14 to make room for its list of what must be kept; [INDEX.md](INDEX.md) lists every spec.

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

## Must keep

The items this component must keep, and the tests that hold them, are listed in [update-job.md](update-job.md#must-keep).

## Since the split

No change yet.
