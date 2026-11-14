# Background update: triggers and job control

- **Component:** Updater: triggers and job control
- **Paths:** `internal/update`, `internal/hook/trigger.go`, `internal/hook/spawn.go`
- **Status:** v1 as built, frozen on 2026-11-14

The blocks below are v1's design and verification text, moved here word for word when the two documents were split; [INDEX.md](INDEX.md) lists every spec.

<!-- v1 docs/design.md 022 -->
## Background update details

### Triggers

Built in the hooks (`internal/hook`). A trigger spawns a detached `eva-md update` (see "Background update"), never waits for it, and ignores a spawn that fails. The six triggers:
1. **SessionEnd,** for the session. On Codex this also fires when a chat is closed in every client and has been idle 30 minutes. A CLI chat left open does not fire it.
2. **PreCompact,** for the session.
3. **Stop with 60 KB or more unprocessed,** for the session. Once the reply is stored, one stat of the session's inbox file gives its size; past 60 KB its watermark is read too. This is the trigger for desktop chats that never end.
4. **The next prompt after a gap of 30 minutes or more,** with 600 bytes or more unprocessed, for the session. The inbox file's modification time and size come from one stat taken before anything is appended. The burst before the gap counts as a finished chat. Only a prompt you typed counts, not a host prompt, a built-in command or a record control.
5. **SessionStart catch-up,** at every chat start, whatever its source: first the chat's own session, when its inbox file, as one stat found it before anything was appended, had not changed for 2 minutes or more and has a backlog.
   A chat resumed after it was left idle, or started again with the same id, needs this: the start appends a session_start event, so its next prompt finds the file just changed and trigger 4 never fires.
   Then, from the session files the brief listed, the project's 64 most recently changed and those of chats a program ran that it read beyond the 64 (step 2 of "Resume: new chat or /clear"), never a full inbox scan, this project's other sessions that have a backlog and whose file has not changed for 2
   minutes or more, or that start-of-chat transcript recovery has just appended to (it takes only sessions idle 2 minutes or more, and its append would make them look active), newest first.
   At most 5 sessions in all, the chat's own among them.
   They go to one job, `eva-md update --session <a> --session <b> ...`, which drains them under one lock and budget: five jobs would race for the lock, and four would exit at once.
   A person's chat start also asks that job for the day's sweep (`--sweep`, see below), or, with no session to catch up, starts `eva-md update --sweep` alone, until a job has taken the sweep that day.
6. **Manual:** `eva-md update --pending` (every backlog) or `eva-md update --session <host>:<id>`; add `--foreground` to run attached, as a person's run (see "Failures, budget and quarantine").

A hook names its trigger to the job it starts, as `--trigger NAME` after the sessions: `session_end`, `compact`, `stop_size`, `prompt_gap` or `chat_start`, for triggers 1 to 5.
It passes the flag only to the build it runs, which releases up to 2026.11.7 would refuse: the installed binary must be the file the hook was started from, with the identity, size and modification time the hook found when it started, written a minute or more before that.
Otherwise, as after an upgrade or a downgrade during the hook, or for a hook started from another file, the job starts without the flag, which every release accepts, and its records name no trigger.
A chat start's job for the day's sweep alone names none; the sessions the sweep picks are recorded as `sweep`, and a person's `--foreground` run without the flag as `manual`.

No trigger fired in a Claude chat a program runs (`CLAUDE_CODE_ENTRYPOINT` `sdk-cli`, `sdk-ts` or `sdk-py`) asks for that chat's own update, and such a chat skips the one stat on a prompt and at a chat start; its chat start still catches up the project's other idle sessions, but never asks for the day's sweep.
Another chat's start can still take a program's chat with a backlog in its catch-up; the job then passes over it as `skipped (headless)`, with no model call.

A backlog is the bytes past the watermark, unless the update last found them all too small to process (under one exchange or 600 characters) and nothing was appended since: the watermark then holds the file's size (`wait <size>`), and the triggers and the brief's pending line leave the session alone until it grows.

**The day's sweep** takes the backlogs no trigger reached: a chat left open and never prompted again, a session whose job found the lock busy, and backlogs past the catch-up's 5 sessions or outside the session files the brief lists.
A person's chat start asks for it with its catch-up while `<state>/updates/sweep` holds neither the hook's day nor the day after it, which launch reads once the output is written, and gives the job that day, `--sweep=<YYYY-MM-DD>`; the job takes it once it holds the update lock: it writes that day
in that file, atomically, unless a job wrote that day, or the day after it, already, so the sweep runs at most once a day, even between chat starts whose days differ, and a job that finds the lock busy leaves it to a later chat start.
A day recorded earlier, or more than a day ahead, as by a clock set wrong, never keeps it from running.
The hook and the job so judge the same day, though the hook's time zone comes from the host's `TZ`, which the job's environment drops, so their clocks can be on different days; `--sweep` alone, as a person runs it, takes the job's own day.
The sweep drains, after the sessions the job names, at most 5 sessions of the whole inbox whose file has been unchanged for 60 minutes or more and holds a backlog by its watermark, newest change first; a file with nothing pending, a watermark that can't be read or lies past the end of its file, and
a chat its catch-up record marks as a program's take no place among the 5, and neither does a session the drain can't work on, which would take one every day while older backlogs wait: one of a project the config excludes, which the drain leaves as it is, or that can't be told, which it fails on,
each judged by the drain's own rule from the entry at its watermark, once a sweep for each project and chat folder, and a chat from before updates the baseline left.
Apart from the 5 and ahead of them, so the doctor names them, the sweep lists up to 16 such chats whose file has been unchanged for 60 minutes or more, newest change first, by the baseline's record alone, before reading any watermark, and the drain records each as `failed (left)` with no model call; the job's log counts them apart.
It runs in the catch-up's own job, under its lock and budget, so it never races the catch-up for the lock, and never in maintain, so it never holds back the day's commit and push, which may run beside it: pages it writes after the commit go out with the next day's.
It shares the day's first chat's rate limits.

No update spawns while a chat is off the record or its transcript recovery is behind; a later chat's catch-up takes its backlog.
The day's maintain and the push retry still start as on any other event.
`updates = false` spawns no update, and neither does a config without a wiki, since an update could only fail; the day's maintain still starts without a wiki, for its search index step.
What the triggers cost a hook: one stat on Stop, on a prompt and at a chat start, the watermark read only past a threshold, and at a chat start one watermark read for its own file when idle and for each idle session file the brief listed, and, after a person's chat start's output, one read of the day the sweep was last taken.
The day's maintain adds one stat of `<state>/commit/attempt-<date>` on every event, after the output (only the day's first event, which finds none, creates it with O_EXCL and starts maintain), and, at a chat start with commits on and a wiki set up, one stat between push checks, with up to 16 KB of each of the last two months' commit records read per check (see "Daily commit and push").
The spawn comes after the output and takes a few milliseconds.

Limits:
- Skip off-the-record ranges, and backlogs under one exchange or 600 characters.
- One job at a time (its own lock). A trigger that finds a job running exits, and the session waits for its next trigger or a day's sweep.
- At most 40 model calls per host per day (configurable). Expect 5 to 15 a day.
- No model call per turn.

### Job control

Built in `internal/update` (`Job`) and run by `eva-md update`.

- **One job at a time.** The global update lock, `<locks>/update.lock` (flock, non-blocking), is held for the whole job, and a job that finds it busy records a busy exit (see "The run log") and exits 0 at once.
  This lock, maintain's, the search index's and those of its project folders, and init's are taken through one helper, `paths.TryLock`, which creates a new lock file with O_EXCL and opens an existing one without creating it, so processes that create the same new lock at once each get the lock or busy, never an error.
  The hooks' session lock is never taken: a hook holds it for its whole run and drops the capture when it can't get it, so a job holding it for minutes would lose captures.
- **No backfill.** The chats an inbox holds from before updates are set aside once: `eva-md install` and setup, after installing the plugins, or else the first job, for a binary replaced without an install, give every session file the update hasn't touched a watermark at the end of its last whole
  line, and write `inbox/.baseline` at that first try; install and setup take the update lock for it and leave the work to a job that holds the lock.
  A fresh install, whose inbox is empty, sets aside nothing, and an upgrade from a capture-only release never spends the daily cap on old chats.
  No file made after that first try is ever set aside.
  A file whose watermark can't be read or written then, as in a folder that can't be written, or that can't be opened or read, is listed in the record (`left <size> <file>`, its size at the first try), and so is a project folder that can't be read (`folder <dir>`).
  Every later job tries them again, a file up to that size and a folder's session files last changed by the first try (its time kept to the nanosecond; a time that can't be read counts as now, so every file of the folder is set aside), and never drains them in the meantime; a folder deleted since
  leaves the record; a job that lists such a file (its own trigger, a start-of-chat catch-up, the day's sweep, which lists the 16 newest idle an hour or more, or `--pending`) records it as `failed (left)` with why, so the doctor names it, and once a later job sets it aside, or finds it gone, that job records it as `skipped (left)`, so the doctor stops naming it.
  A dry run says the next update would set the chat aside, or that it waits to be set aside and why (its file can't be opened or its watermark can't be read, or else the next update tries again), also before the first try, and writes nothing.
  Install and setup say how many chats are left, which the doctor names once an update has listed each (the day's sweep lists up to 16, newest first), and how many folders, which only the update's log names.
- **Draining.** Pending work is worked out from the inbox watermarks, never queued: a session file is listed only when its watermark says something is pending, so a rest found too small (`wait`) is not reopened, or when the watermark can't be read, sits past the end of the file or inside a line; such a file is recorded as `failed (watermark)` with no model call.
  `--session` drains that session's files and `--pending` every file with a backlog, oldest change first, under the same lock and budget.
  A session is drained pass by pass, one chunk and one model call each, until its backlog is empty, it waits for more entries, a pass fails, or a stop applies.
  The stops are checked before each session file is opened as well as before each model call: once the budget, a hold or a backoff stops a host, or when the session, by the id its file name holds, is quarantined today, the job opens none of those files, and their passes, skips included, wait for the next run.
  So a chat past 60 KB, whose every Stop spawns a job, costs no read of its file while a stop lasts, only the job's start: the lock, the wiki check, the run log and the inbox listing.
- **Groups and chunks.** The entries from the watermark that share a branch, as each entry records it, are one group; an entry without a branch, such as a gap, joins the group it is in.
  Only a user prompt of another branch ends a group: the hooks record each entry's branch as HEAD was at that moment, so a turn in which the assistant checks out a branch has its tool lines and reply on the new branch, and they stay in the group of their prompt's branch.
  `Build` takes the group's oldest entries up to 60 KB of trimmed input.
  A group a branch switch closed under one exchange or 600 characters, such as a prompt with no reply before a prompt on another branch, is skipped (`skipped (small)`), since nothing can join it; at the end of the file such a group waits.
  The rest of a group a pass stopped in that holds only events, such as a closed chat's `session_end`, is skipped the same way, since events add nothing to a page.
- **A program's chat.** At each pass, when no prompt the user typed comes before the watermark (found while the lines before it are counted, from the raw `"role":"user","kind":"prompt"` in each line, which text can't forge since entries are JSON-escaped) or in the window, and either the session's
  catch-up record (`<state>/catchup/<host>/<session>.json`) marks it `Headless`, or the window reaches the end of the file and a host prompt comes before the watermark or in the window, the pass is skipped as `skipped (headless)`.
  The watermark moves to the end of the window with no model call, and later appends are passed over the same way, since the host prompt before the watermark still counts.
  A group with no typed prompt never passes the size check, so the window keeps growing, up to its 64 MiB, until it reaches the end of the file.
  A marked chat whose backlog is longer than that is passed over a window at a time.
  An unmarked one waits, as before, with no model call, since a person's chat can begin with host prompts, from a scheduled task or a /loop run, and type later.
  A typed prompt before the watermark or in the window keeps a chat a person's, so a person's chat that a `claude -p --resume` marked is still updated.
  The dry run prints the skip.
- **A bounded window.** The job counts the lines before the watermark once (`Prefix`), then decodes a 4 MiB window past it. The window grows, doubling up to 64 MiB, only while the group runs on past it and its chunk still has room, so a large backlog is never read whole.
- **The watermark rule (change 2).** A pass whose output validates moves the watermark to exactly the end of the entries it covered, whether or not the page changed, and only after the page's folder fsync when it did; the run log records `no_change` for a pass that changed nothing.
  A crash, kill, timeout or rejected output leaves the watermark where it was.
  Before each model call the job saves the watermark as it stands, so a watermark that can't be saved stops the pass before it spends a call; one that can't be saved after the call is recorded as `failed (watermark)`, and the call still counts toward the budget.
  The watermark file (`inbox/<key>/.state/<file>.wm`) holds the byte offset, followed by `more` when the pass stopped inside a group, so the next pass, in this job or a later one after a crash, continues that group (`Options.Continued`) rather than judging its tail, such as a closed chat's last
  replies with no prompt among them, as a group of its own; and last by `wait <size>` when the job found the rest too small to process at that file size, which the triggers and the brief read (see "Triggers").
  The dry run never writes it.
- **The page write.** The thread page is read by its path through the adapter, never the whole wiki.
  A page with `locked: true`, a `verified` entry (okf reserves it for people, and every save would supersede it) or a body eva-md can't read back safely is never written: the pass is skipped with that reason, without a model call, and the watermark moves on (`skipped (locked)`, `skipped (verified)`, `skipped (damaged)`).
  Otherwise the validated update is applied, rendered and saved with compare-and-swap, retried at most twice on a conflict by reading the page again and applying the same update again.
  A pass that changes nothing in the body still stamps the page's `as_of` and `sources` when it moves them, so the brief shows the Now it confirmed as fresh; it writes nothing when it moves neither.
  The first page of a project creates its folder (the repo name; when a folder of that name exists and isn't this project's, owner-repo; then the repo name with 6 hex digits of a hash of the project key), its now.md and its index files.
  A new thread's page records its parent, the branch the brief's lineage rule picks, and the model sees the parent's Now for context.
- **Overview and index files.** After a save, a stamp included, the job rewrites the project's now.md, one line per thread, newest activity first, and the index files of the folders the write touched: the threads folder, the project folder, `projects/` and the root.
  These read every page in their folder, so only the update job runs them, never a hook.
  An index file is written only when its text changes. log.md gets a line for each page a pass creates or changes, once a day (see the log.md row of the okf calls); the run log is the record of the passes themselves.
- **Page context.** The commit subjects come from git in the chat's worktree: the branch's last 5, with the hardened flags the identity package uses, no signature checked (`log.showSignature=false`), lazy fetches off (`GIT_NO_LAZY_FETCH=1`) and a 2-second timeout, or "unknown".
  The page's `head` records the branch's newest commit, `as_of` the newest entry the pass covered (it never moves back), and `sources` the last 20 sessions.
- **The command.** `eva-md update --session <host>:<id>` is what the triggers spawn detached; the start-of-chat catch-up gives `--session` once for each session it names, and one job drains them all, and with `--sweep`, given with `--session` or alone, the job also takes the day's sweep (see
  "Triggers"), which `--pending` and `--dry-run` refuse; `--sweep=<YYYY-MM-DD>`, as a chat start gives it, takes it on that day rather than the job's own.
  Its lines go to `<state>/logs/update-<YYYY-MM-DD>.log` (0600, appended; the first open of a day's log, normally by the hooks' first spawn, deletes the logs older than 14 days), and it prints one summary line.
  `--foreground` runs attached, with progress on stderr; such a run is manual (see "Failures, budget and quarantine").
  `--pending` drains every backlog.
  `--dry-run` builds the session's next chunk and prints the exact command, the names (never the values) of the child's environment variables, the rules, the input and the schema; it makes no call, takes no lock and writes nothing, so two dry runs print the same bytes.
  Logs hold counts and reasons only.

<!-- /v1 -->

## Must keep

The must-keep items of [findings.md](../v2/findings.md) this component touches, numbered from K01 in each list, and the tests that hold them.

| Id | Item | Tests |
| --- | --- | --- |
| CAP-K03 | EVA_MD_INNER=1 stops capture inside eva-md's own model runs, and every detached job carries it | internal/hook:TestGuardsAndOrdering, internal/hook:TestMaintainJobEnvironment, internal/update:TestChildEnvironmentIsAnAllowlist |
| CAP-K04 | A detached job runs the installed binary by absolute path, in its own session, stdin from /dev/null, output to a log, with an allowlisted environment | internal/hook:TestDetachedJobOutlivesTheHook, internal/hook:TestDetachNeedsAnAbsoluteShareFolder, internal/hook:TestMaintainJobEnvironment, internal/hook:TestJobEnvFollowsGit |
| UPM-K16 | The chat's own host runs the update; a failure holds the host, backs off or blames the session | internal/update:TestAuthHoldsTheHostUntilAManualRunOrTomorrow, internal/update:TestRateLimitBacksOffWithoutCountingTowardTheSession, internal/update:TestThreeInvalidOutputsQuarantineTheSession, internal/update:TestClassifyFailureText |
| UPW-K01 | Hooks never wait for or share a lock with the job: it starts after the output, detached, and trigger checks cost a stat | internal/hook:TestSpawnFollowsTheOutput, internal/hook:TestDetachedJobOutlivesTheHook, internal/hook:TestStopSpawnsAt60KBUnprocessed, internal/update:TestSecondJobExitsAtOnce |
| UPW-K12 | Host failures hold or back off without blaming a session; a person's run clears holds; a failed watermark save still counts | internal/update:TestAuthHoldsTheHostUntilAManualRunOrTomorrow, internal/update:TestRateLimitBacksOffWithoutCountingTowardTheSession, internal/update:TestManualRunClearsAQuarantine, internal/update:TestSkipsAndAMissingCLICountNoCall, internal/update:TestBackoffUntil, internal/update:TestWatermarkFailureStaysWithinTheBudget |

## Since the split

No change yet.
