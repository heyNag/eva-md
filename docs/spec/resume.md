# Resume flows

- **Component:** Brief builder: the resume flows
- **Paths:** `internal/resume`, `internal/hook/resume.go`
- **Status:** v1 as built, frozen on 2026-11-14

The blocks below are v1's design and verification text, moved here word for word when the two documents were split; [INDEX.md](INDEX.md) lists every spec.

<!-- v1 docs/design.md 010 -->
## Resume: new chat or /clear

1. SessionStart(startup|clear) passes the guards, runs catch-up for this project's idle sessions (at most 150 ms; it looks at the 256 most recently updated of at most 4,096 recovery records per host, and the daily maintain keeps at most 2,048 a host, so it lists them all unless more than 2,048 chats
   of a host start between two of its runs) and appends a session_start event with its source and model.
   In a spare folder, store only the model marker and exit; with no briefed marker, the first prompt from a real folder gets the brief.
2. **Pick the previous chat.** This is the newest session in this project and branch, on either host, that did real work (more than 2 exchanges, or any tool or plan activity), excluding this session.
   Chats are ordered by when they last did work: the newest prompt, reply, tool or plan entry in the last MiB, or the newest entry of any kind only when the last MiB holds none; ties go to the newest entry, then host and session id.
   A session_end, session_start, compact or interrupt event never makes an idle chat the previous chat, the lineage pick or a new thread's parent.
   The brief reads at most the 64 most recently changed session files of the project, counted as files whatever number of chats it keeps from them, and those 64 are the files it lists for the start-of-chat catch-up (step 7).
   A file it finds to hold a chat a program ran (see below) doesn't count toward the 64, so a burst of them can't push your chats out of the brief; up to 64 such files, and 2 MiB of them in all, are read and listed beyond the 64, and past either bound they count as any file does.
   Each holds its first line in the last MiB the brief reads, so none is longer than that, and a MiB of the short tool lines and replies such a chat holds takes about 13 ms to read, so they add about 25 ms at most.
   It decodes the last MiB of each and only counts the lines before it, and it scans those earlier bytes for work only when a branch's last MiB shows none, so its cost stays bounded however long the chats grow.
   That scan reads line by line, and work there (a tool or plan entry, or more than 2 prompts and 2 replies) counts only for the branch it was done on, read from the line's branch field (none means `default`); it stops once every branch that needs it has shown work, and reads whole the line the last MiB cuts in two, so its branch is known.
   A chat a program ran is never the previous chat, the lineage pick below, or one of the earlier chats or other branches the brief lists: one whose session file, read from its first line, holds host prompts and no prompt the user typed.
   The whole file is judged, every branch, as the update job judges it, so a program's chat that checks out a branch is passed over on that branch too, and a person's chat that checked out another branch is a person's on every branch.
   A program's chat longer than the MiB the brief reads is not told apart.
   The lineage that names a new thread page's parent follows the same rule.
   - If there is none, and this is a startup or clear on a branch with no Now page of its own, use the lineage fallback: the project's most recently active thread, labelled "continuing from <branch>", and its Now labelled with that branch. Resumed and forked chats never use lineage.
   - The general area only counts a chat whose newest work (prompt, reply, tool or plan) is less than 12 hours old, with the one-minute future guard on that time too, so a fresh event such as a session_end doesn't keep a day-old general chat in the window.
     A session file last changed more than 12 hours ago is not read at all, since every entry is written before its file last changes; it is still listed for the start-of-chat catch-up.
3. Load the thread's Now section, reading only that page (and the project's now.md to confirm the slug) by its path, never the rest of the wiki, and of the page only its frontmatter and its body up to the end of the Now (`okfadapter.ReadHead`), so a thread page whose Timeline grew for years still
   shows its Now, and a brief or a prompt's refresh reads at most 64 KiB past the Now's end however long the page is.
   A Now that ends past 2 MiB, as one a person moved below a long section would, shows none, and a rules page over 2 MiB is not read.
   Markers after the Now's end are never looked at, so a page damaged only there still shows its Now.
   Mark it stale if `as_of` is older than the previous chat's last prompt, reply, tool or plan entry.
   Events, such as the session_start written just before the brief, don't count.
   When the project's own folder has no now.md, the brief reads the other project folders' now.md files (at most 256) to find the one whose remote matches, as after a slug collision.
   The folder found, its own or another, is recorded in the briefed marker for the refresh.
4. **Read live git** (800 ms limit, and never so long that the brief misses the hook's deadline; otherwise "unknown"):
   - branch, and ahead/behind from local refs
   - whether HEAD moved since the page's `head` (the count and the last 3 subjects)
   - up to 15 changed paths
   - any rebase, merge, cherry-pick or bisect in progress, or "unknown" when git can't tell
   - stash count, which git counts itself (`git rev-list --walk-reflogs --count refs/stash --`), so the 64 KiB a live git call keeps never cuts it

   Live git runs with lazy fetches off, as the code tie's calls do (`GIT_NO_LAZY_FETCH=1`, and `--no-lazy-fetch` in a clone that may be partial), so the hook never fetches; it checks no signature (`log.showSignature=false`), and a call killed at its deadline takes the processes it started with it.
5. **Assemble at most 8,000 characters on Claude and 6,000 on Codex** in this order.
   The footer and the newest exchange are reserved first, so the cap never cuts them: the newest message keeps its share and the message before it, usually the question it answers, at least 300 characters.
   Then the header, the opening request and live git; then the Now, up to its budget; then the tool activity, the plan, the earlier chats and the other branches share what is left, in that order of priority, each whole or left out; older messages take what remains.
   Every part is measured as the fence will show it, after cleaning and escaping, and should cleaning the whole or a longer fence still add a little, older messages and then the free text give way.
   1. header.
      When the previous chat, on this branch, has a backlog at brief time that a pass could take (its watermark is 600 bytes or more behind its session file, since a group needs 600 characters, or any bytes behind when the last pass stopped inside a group, whose rest needs only one more entry; and no
      update has found that rest too small to process yet), with updates on and a wiki set up, the line "An update for the previous chat is pending; its Now arrives with a later prompt." follows it, and the hash-triggered refresh then delivers that Now.
      A compact brief and a lineage brief never show it.
   2. opening request: the previous chat's first prompt on this branch with its ref (200), from a read of the first 64 KiB of its session file, never a whole file. It is left out when an off_record event comes before it there, or when an exchange below shows it.
   3. live git
   4. Now, by priority (see "The Now in a brief" under Recall and fencing): 3,200 characters on Claude and 2,400 on Codex, or 1,714 and 1,285 if stale
   5. open plan, if newer than the page (600)
   6. tool activity, a heuristic: the last test or build command with its exit code (150), meaning a shell command that starts with go test, go build, go vet, npm test, pnpm test, yarn test, pytest, make, cargo test or swift test, after any `cd <folder> &&` or `cd <folder>;` (the folder a word or
      quoted), shown as it ran, cd included; and up to 5 distinct files from edit and write tool lines, newest first, shown relative to the worktree
   7. earlier chats on this branch: up to 3 other chats that did real work, newest first, in about 80 characters each: the day of its newest work, the host, and its first prompt with its ref, from the same bounded head read
   8. other branches: up to 3 with real work in the project's 64 most recent session files, newest first, with the day of each one's newest work (about 60 characters each)
   9. last exchanges verbatim, newest last: 3,085 on Claude and 2,314 on Codex, or 4,571 and 3,428 if stale or there is no page. The newest message keeps half of that, and the one before it at least 300, whatever else the brief holds. Long messages are cut in the middle with an `eva-md show` ref.
   10. footer, at most 450: the installed binary's absolute path (`~/.local/share/eva-md/bin/eva-md`, since a chat's PATH may not reach eva-md), named once, with a short fetch protocol: `<bin> search <terms>` when the answer depends on earlier chats, rephrasing with other words, file names or dates
       on a miss and stopping after about 5 lookups; `<bin> show <ref>`, or `<thread id>#Now` or `#Timeline --last 10` when a thread page exists; check what may have changed against the code.
       With a wiki configured, a plain `grep -ri <terms>` fallback names the project's threads folder.
       With the usual binary path the footer is about 250 characters without a wiki, and about 380 with the thread id and the threads folder; a 40-character branch name brings it to about 425.
       Past 450 the grep fallback is left out first, then the thread id with its Now and Timeline pointers; the two commands stay.

   Three more parts are reserved with the footer, so the cap never cuts them: the standing rules, first in the brief (see "Rules page"), the project's constraints, right after them (see "Constraints" under Recall and fencing), and the system pointers, just before the footer (see "System pointers" under Recall and fencing).

   A new chat with no chat to resume and no Now gets the footer alone, about 150 characters and unfenced, pointing at `<bin> search --all` for the other projects. With standing rules, constraints or a system map it carries those too, fenced. It is not counted as history found.
6. Fence the brief and return it as `additionalContext`.
   Claude also gets a one-line `systemMessage`, "eva.md: earlier work is available for this chat.", when history was found, unless the config sets `notice = "problems"`; a health notice takes its place when there is one.
   Both hosts get a one-line notice only when the quick health checks find a problem.
   When the config exists but can't be read, a chat start on either host gets one line instead, "eva.md: the config can't be read, so this chat isn't captured; run eva-md doctor.", as `additionalContext` and as the `systemMessage`, and nothing is captured; every other event is as before, Codex Stop still printing `{}`.
   A config read cut off by the hook's time limit, a hook in eva-md's own run (`EVA_MD_INNER=1`) and a missing config give no such line.
   Write the per-session briefed marker last, and only with 20 ms to spare before the deadline, so a brief that was not delivered is retried by the first-prompt fallback.
   It records the hash of the Now section the chat has seen, the brief variant, how many refreshes have been sent, and the project's wiki folder the brief found.
7. Spawn the catch-up update (see "Triggers"), in one `eva-md update` job that catches up at most 5 sessions: first this chat's own, unless a program runs the chat, when its session file, as one stat found it before the hook appended anything, had not changed for 2 minutes or more and has a backlog,
   as for a chat resumed, or started again with the same id, after it was left idle; then, from the session files the brief listed in step 2 (the project's 64 most recently changed, and those of chats a program ran that it read beyond the 64; never a full inbox scan), the other sessions that have a
   backlog and whose file has not changed for 2 minutes or more, or that start-of-chat transcript recovery appended to in step 1 (it takes only sessions idle that long), newest first.
   A person's chat start also asks that job for the day's sweep, at most 5 more sessions from the whole inbox, or starts it alone when no session needs catching up, until a job has taken the sweep that day (see "The day's sweep" under "Triggers").
   When a push is pending, the start also spawns `eva-md maintain --push-only`, at most every 30 minutes (see "Daily commit and push").

Host differences:
- Claude caps additionalContext at 10,000 characters, so 8,000 is a safe hard cap.
- Codex limits hook context by tokens (2,500 by default), so the Codex hooks set `additionalContextLimit` to 3000, and the brief's 6,000 characters stay well inside it.
- Codex shows systemMessage as a warning, so a healthy start is silent there.

## Resume: resumed or forked chat

SessionStart(resume|fork): the conversation is already loaded. Inject only what changed since this session's last entry:
- the standing rules, first, when a rule changed
- the Now, by priority in 700 characters, when it changed
- other sessions' activity on this thread, one line each: `Other chat: <host> at <time> (<its last activity>)` only when another chat on the branch did work (a prompt, reply, tool or plan) after this chat's last work, with that work's time; a later event alone, such as the other chat closing, adds no line
- live git

At most 1,500 characters, and nothing if nothing changed. Same on both hosts.

The catch-up update of step 7 above runs at this start too, with the same rules: 2 minutes idle, at most 5 sessions, from the session files the brief listed, and on a person's chat start the day's sweep in the same job.
A chat resumed after it was left idle has its own backlog in that job, first: the start appends to its file, so its next prompt finds the file just changed and the prompt trigger would never take that backlog.

## Resume: first-prompt fallback

If no brief was delivered at SessionStart, the first UserPromptSubmit returns the full startup brief. This covers a desktop spare session, a hook trusted mid-session, or a folder that wasn't trusted yet. It is keyed by the per-session briefed marker, so it happens once.

## Resume: hash-triggered refresh

On every UserPromptSubmit after a brief, read the thread page by its path, under the wiki folder the briefed marker records (or the project's own when it records none), and compare its Now hash and the standing rules' hash with the ones in the marker.
A refresh never lists or reads the other projects' folders, so it costs about what capture does.
The hash is taken after the hidden comments are removed, so a change to item ids or dates alone is no change.
If either changed, an update finished while you were working: inject the changed rules (see "Rules page") and, when it changed, the new Now, by priority, in at most 2,500 characters, fenced, and record the hashes and one more refresh in the marker.
When neither changed, nothing is injected, no refresh is used and the marker stays as it is.

A chat gets at most 2 refreshes.
A compact brief keeps the count, so compacting a chat never grants more; any other brief, such as after a /clear, starts it again.
A Now saved after a compaction still reaches the chat while it has a refresh left.
A marker written before the count, with `"refreshed": true`, counts as one refresh used.
As with the brief, the marker is saved only with 20 ms to spare before the deadline, so a refresh that was not delivered is retried on the next prompt.

## After compaction

1. PreCompact: catch up, append a compact event and spawn an update for the session. Exit within 1 s with no output.
2. The host compacts the conversation.
3. Claude PostCompact: store `compact_summary`, unless the chat has been off the record since its last startup or clear.
4. SessionStart(compact): inject the header, this session's opening request (from the same bounded head read), live git, the thread's Now by priority (2,100 characters on Claude, 1,575 on Codex), the open plan, the tool activity line, this session's last 2 exchanges before compaction, verbatim (2,250 and 1,687), and the footer.
   At most 6,000 characters on Claude and 4,500 on Codex in total, with the footer and the newest exchange reserved first, and the tool activity line before the Now takes its budget.
   If the update has not finished, the brief says the Now section predates these exchanges.
   The catch-up update of a chat start (step 7 of "Resume: new chat or /clear") runs here too, with the same rules; the chat's own file changed at PreCompact, which spawned its update, so it is left out unless the compaction took 2 minutes or more.
5. The hash-triggered refresh applies here too: when the update for the compacted part finishes, the next prompt carries the new Now, if the chat has a refresh left (the count runs on through compactions).

Codex runs SessionStart(compact) before the next model request, even after automatic compaction mid-turn.

<!-- /v1 -->

## Must keep

The must-keep items of [findings.md](../v2/findings.md) this component touches, numbered from K01 in each list, and the tests that hold them.
An item that tests hold only in part ends with what of it no test holds, as `(untested: ...)`.

| Id | Item | Tests |
| --- | --- | --- |
| BRF-K01 | The brief needs no model or network; any failure gives no context and the hook still exits 0 | internal/resume:TestGeneralAgeScopeOffRecordAndFailure, internal/hook:TestSessionStartBudgetAndMarkerErrors, internal/hook:TestHooksStartNothingOutside, internal/hook:TestHooksInAGitProjectOnlyReadGit, internal/hook:TestHookLinksNothingThatConnects |
| BRF-K04 | Off-the-record ranges leave the brief, and no brief comes while a chat is off the record or its transcript is unread | internal/resume:TestGeneralAgeScopeOffRecordAndFailure, internal/resume:TestOpeningRequestFromHeadRead, internal/hook:TestCompactResumeAndOffRecordSuppression, internal/hook:TestOffRecordAndResume |
| BRF-K07 | Another branch's Now comes only as labelled lineage at a startup or clear, never on a resume, fork or compact | internal/resume:TestResumeNeverInjectsAnotherBranchNow, internal/resume:TestResumeReportsLiveGitWithoutAnotherBranchPage, internal/resume:TestStartupLineageNowNamesItsBranch, internal/resume:TestStartupKeepsOwnBranchNow, internal/resume:TestCompactDoesNotBorrowAnotherSession |
| BRF-K13 | A brief not delivered is given again with the next prompt | internal/hook:TestBriefTooCloseToDeadlineIsRetried, internal/hook:TestDeliveryRecordsTheFirstPromptFallback |
| BRF-K14 | The refresh hash ignores hidden comments, so ids and dates alone change nothing | internal/resume:TestStripComments, internal/resume:TestRefreshFollowsTheRules, internal/hook:TestRefreshFollowsTheNowHash |

## Since the split

No change yet.
