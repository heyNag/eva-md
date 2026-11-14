# The capture audit

- **Component:** Capture audit
- **Paths:** `internal/ledger/audit`, `cmd/eva-md/audit.go`
- **Status:** v1 as built, frozen on 2026-11-14

The blocks below are v1's design and verification text, moved here word for word when the two documents were split; [INDEX.md](INDEX.md) lists every spec.

<!-- v1 docs/design.md 020 -->
## Capture audit

`eva-md audit capture [--session ID | --last N]` measures how much of a chat v1 captures, against the host's own record, and changes nothing.
`--session` takes a session's whole id, a ref, or the first 8 characters a ref keeps when no session's whole id is that (every session sharing them is audited); `--last N` the N sessions whose newest stored line is newest.
It reads the inbox through the ledger's v1 reader (`internal/ledger/inboxv1`), and a host's transcript only where the session's transcript recovery record names one that recovery's own rules admit (`catchup.Admits`, then `catchup.OpenTranscript`): a path those rules refuse is counted and never opened, and a chat off the record now, by its recovery record or its marker, is not opened either.
As recovery does, it refuses once opened a file put in the registered one's place (another device or inode), one cut shorter than recovery's cursor, and a Codex file whose first record names another session or folder.
Where the record names no transcript, because no hook named one, one was named before the host wrote it, or for a reason an older record does not keep, or names one that is gone, the audit looks for the session's file by its id among the files it lists by name in the hosts' folders of chats (the
listing it counts host sessions with no eva-md file from, made once a run): a Claude Code file named `<id>.jsonl` in a project folder, a Codex file named `rollout-<time>-<id>.jsonl` in a day's folder.
It takes the file only when exactly one matches.
The listing follows the folder of chats itself where a symlink leads, as recovery does, and no symlink inside it: a file in a linked folder is not listed, and a session file that is itself a symlink is refused and never opened.
The file is held to recovery's path rules under the home the record names (`catchup.Admits`), so a file those rules refuse, as one outside the folder of chats of that home, is refused unopened; an admitted file is opened by `catchup.OpenUnregistered`, recovery's opener for a file no record
registered: a regular file with one link, reached through no symlink, and on Codex with a first record that names the session and its folder; it checks nothing of a cursor, which no record of the file keeps.
A record whose path recovery's rules refused is not looked for by id, nor is a chat off the record now, by its marker or a block its record keeps.
A session paired by its id is audited exactly as one paired by its record, off-the-record ranges included, and its file is read once, under the same limit on a row; there is no limit on a file's size, so the audit's cost grows with the length of the files it reads.
The scorer is `internal/ledger/audit`.
Its report holds numbers, the hosts' names and fixed words only, never a text, session id, path, project or branch name; a paired session is named by its place, newest first, and by how it was paired, `by its recovery record` or `by its id`.

- **Coverage**, for each paired session, in total over the sessions paired either way, and over those paired each way apart: prompts that reached the agent (a user's prompt, also one inside a voice hand-over or from a program, and not a record control, a host's own command or a wrapper the host puts
  around bash-mode input or a command banner), final replies (on Codex, one a turn, a turn no row names told by the rows that begin it) and calls of the tools the plugins' PostToolUse matchers name, each found, missing from the inbox and extra in it.
  They are paired turn by turn; an item with no turn id pairs with the other side's unpaired ones.
  A subagent's lines, the reader's marked copies and anything withheld are not counted.
  Host rows more than 10 seconds before the session's first stored line, which v1 never imports, are counted apart, without kinds; so are the prompts, replies and tool calls of a folder the hooks leave out by design (a Claude Code row's folder, or on Codex the folder of the session or its turn), as
  the hook judges a folder but for an exclusion by its remote, which needs git; and so are rows over 32 MiB, each first scanned for a record control as recovery scans one (`catchup.LongRow`).
- **Subagents' threads**: a session whose every inbox file the reader classes as a subagent's is paired and read as any other, so that a range said off the record in it counts as paused and nothing inside it is counted, but it is not scored: paired or not, it adds nothing to any coverage figure,
  both ways together or each apart, nor to the repeats, gaps, voice hand-overs, host rows not compared or turns with no stored reply.
  Those paired are counted on a line of their own by the way they were paired, not among the sessions paired, and each one's own line says `a subagent's thread, not scored`, with no figures. v1 stores a subagent's lines with its agent id, and the audit's coverage counts only a person's and a program's lines.
  On Codex a subagent's thread is a session of its own, whose every host row would then read as missing.
  A session that holds a subagent's lines among its own, as a Claude Code chat with a subagent's tool lines does, is scored as any other; scoring a subagent's thread against its own stored lines is left for later.
- **Exact repeats** by kind: a stored prompt, reply or tool line that says exactly what an earlier one of its session, subagent and turn says, and how many of them pair a hook's line with one recovery stored.
- **Gaps** by their fixed reason, lines the reader withholds by why, and **turns with a prompt and no stored reply** by cause: a failed stop, an interrupt the inbox or the host's record shows, a stop whose payload held no reply, otherwise no stop line at all (hook never ran).
- **Prompts inside a voice hand-over**, by their prefix alone, in the inbox and the host's record.
- **Inbox files by what they show**, as the reader classes them: a subagent's, a program's start, an end with no start, no turn at all.
- **Sessions paired** by their recovery record and by their id, subagents' threads apart, and **sessions not paired by the recovery record**, by why: no recovery record; a chat off the record now; a transcript missing because no hook named one, because a hook named one recovery's rules refused, or
  because a hook named one they admit before the host wrote it (the record's `missing_why`, which Register sets and keeps while later events name none); a missing transcript in a record written before that field was kept, whose why is not known; one the rules refuse now; one gone; a record or a transcript that can't be read.
  Of those looked for by their id: paired, no file by that id, more than one, refused, gone before it was opened, unreadable, and read but set aside as a chat off the record once (its `was_off` marker) whose range neither side shows, so that these figures, with the subagents' threads paired by id, add up to the sessions looked for.
  Apart: such a chat read by the transcript its recovery record names, also read and not paired.
- **Host sessions with no eva-md file**, counted only when every session is audited, by the file names in `~/.claude/projects` and `~/.codex/sessions`, without opening a file: those written before the inbox's oldest line apart, and of the rest, on Claude Code, those whose project folder's name is
  that of a folder left out by design (temporary, spares, eva-md's state, the wiki, an excluded folder, by its path or where its symlinks lead).
  Several folders share a name, so this may count a folder captured, and a folder excluded by its remote is not told.
  Codex's file names give no folder, so there the report says how many are left out by design is not known.

Nothing off the record is counted by kind.
A range said off the record counts once as paused, in the inbox (a pause control, or lines withheld as off the record) and in the host's record (a prompt that is a record control, as `session.RecordControl` reads it, also in a row over 32 MiB, which is scanned for one).
A control that brings the chat back with no range open still closes one: in the host's record, one that opened at a row since the last control that could not be read, which may have been the control; in the inbox, one that opened at its last line before it, since recovery stores nothing for a control it finds in a row the hooks missed.
Host rows inside a range, too long to read or not, and inbox lines and host rows with no time while either side shows a range, are counted in nothing; an inbox line with no time, or one the reader repaired, neither opens nor closes a range.
A chat that was off the record once, where neither side shows a range, is not paired.
Ranges found only by the fallbacks are wider than what was said off the record, so a few items beside them go uncounted on both sides.
Which row names Codex gives a shell call, and what marks a typed prompt, are host facts the host sitting records; until then a shell call counts under the names `shell`, `container.exec`, `exec_command`, `shell_command` and `local_shell`.

<!-- v1 docs/verification.md 042 -->
- **Capture audit** (`internal/ledger/audit`, `internal/catchup`, `cmd/eva-md`).
  On made-up Claude Code and Codex transcripts and inboxes with planted losses, extras and copies, `eva-md audit capture` gives the exact counts of prompts, final replies and tool calls found, missing and extra, repeats, gaps, turns with no stored reply by cause and voice hand-overs
  (`TestClaudeCounts`, `TestCodexCounts`, `TestScope`), with each host's rows read as the scorer reads them, rows in folders left out by design apart, a Codex chat's by its first record's folder too (`TestClaudeRows`, `TestCodexRows`, `TestACodexChatInAFolderLeftOut`, `TestOutside`); a marker planted
  in every text, id, path, project and branch is in no line of the report, and the audit changes no file (`TestTheReportHoldsNumbersOnly`); a transcript recovery's rules refuse by its path, and one of a chat off the record now, by its record or its marker alone, is counted and never opened, and one
  replaced since it was registered is opened and refused (`TestRecoveryRulesDecideWhatIsOpened`, `TestCodexIdentityIsChecked`, `TestOpenTranscriptKeepsRecoverysRules`); inboxes and host records that differ only within a range said off the record give the same report, also with rows too long to read,
  rows with no time and repaired lines inside it, and with the control that opened it only in a host row too long to decode, which is scanned for one as recovery scans it, and with the control that opened it in the first of two rows that could not be read (`TestNothingOffTheRecordIsCounted`,
  `TestAControlInALongRowTheHooksMissed`, `TestALongRowIsScannedForARecordControl`, `TestAnUnreadRowMayHaveOpenedARange`, `TestTheFirstUnreadRowOpensTheRange`, `TestPauses`, `TestLongRowFindsARecordControl`); the recovery record says why a transcript is missing, none named, refused or not written yet,
  and a record from before it said so is counted apart (`TestRecordSaysWhyATranscriptIsMissing`); a session whose record names no transcript, or one gone, is paired with the one file named for its id in the host's folder of chats and scored exactly as when its record names it, also within a range said
  off the record, whichever side shows it (`TestASessionPairedByItsIDIsScoredAsByItsRecord`, `TestNothingOffTheRecordIsCountedByID`); no file by that id and two are counted, and a file of the right name outside the folders of chats, in a linked folder, under another home than the record's, or that is
  a symlink, is never opened, while a Codex file of another session or folder is opened and refused, one that cannot be read once opened is counted unreadable, and a chat off the record once whose range neither side shows is counted among those looked for (`TestWhatIsLookedForByIDAndOpened`,
  `TestOpenUnregisteredKeepsRecoverysPathRules`); a session is paired by its id, and scored the same, also when the home is named through a link, by its record, by the audit or both, and when `~/.claude` and `~/.codex`, or `~/.claude/projects` and `~/.codex/sessions` themselves, are links to another
  folder (`TestPairedByIDThroughALink`); a Codex subagent's thread, paired by its record or by its id, is read but adds nothing to coverage, repeats, gaps or turns with no stored reply, and is counted on a line of its own by how it was paired, while the same session with the subagent's id taken off
  its lines is scored, and a Claude Code chat with a subagent's lines among its own is scored as before (`TestASubagentsThreadIsPairedAndNotScored`, `TestAChatWithASubagentsLinesIsScored`); a range said off the record in subagents' threads is counted as paused and nothing in it is read into any count
  (`TestNothingOffTheRecordIsReadInASubagentsThread`), and the planted-marker test holds subagents' threads paired both ways; on a few hundred sessions the folders of chats are listed once and each file opened once (`TestTheCostOfAFewHundredSessionsPairedByID`); and a chat the real hooks captured
  pairs with its host record, by the transcript they name or, when they name none, by its id (`TestAuditCaptureOfAChatTheHooksCaptured`).

<!-- /v1 -->

## Must keep

No must-keep item of [findings.md](../v2/findings.md) is this component's: it was built after the audit that listed what v1 must keep.

## Since the split

No change yet.
