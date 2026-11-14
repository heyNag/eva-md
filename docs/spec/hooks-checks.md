# Capture and the hook dispatcher: their offline checks

- **Component:** Hook dispatcher: its offline checks
- **Paths:** `internal/hook`
- **Status:** v1 as built, frozen on 2026-11-14

The blocks below are v1's verification text, moved here word for word when the two documents were split, then out of [hooks.md](hooks.md) on 2026-11-14 to make room for its list of what must be kept; [INDEX.md](INDEX.md) lists every spec.

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

## Must keep

The items this component must keep, and the tests that hold them, are listed in [hooks.md](hooks.md#must-keep).

## Since the split

No change yet.
