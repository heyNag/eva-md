# The shapes audit

- **Component:** Shapes audit
- **Paths:** `internal/ledger/shapes`, `cmd/eva-md/shapes.go`
- **Status:** v1 as built, frozen on 2026-11-14

The blocks below are v1's design and verification text, moved here word for word when the two documents were split; [INDEX.md](INDEX.md) lists every spec.

<!-- v1 docs/design.md 021 -->
## Shapes audit

`eva-md audit shapes` prints, as one JSON document, the shapes of the hosts' own records of the chats eva-md captured, and of eva-md's records of them, as the figures for the facts of `docs/v2/host-facts.md` marked "by the field report". It changes nothing, runs no command and makes no network or model call. The package is `internal/ledger/shapes`.

- **What it reads.** The inbox, through the ledger's v1 reader; each session's markers and recovery record; eva-md's identity cache, when the config excludes a repository's remote (see "What is left out"); and the host files the capture audit lists by name (`audit.ListStore`) of the sessions the
  inbox holds, each admitted by recovery's path rules (`catchup.Admits`) and opened by recovery's opener for a file no record registered (`catchup.OpenUnregistered`), which on Codex checks that the file's first record names the session and its folder, reading only the file's first 64 KiB for it, so a
  Codex file whose first record is longer than that is refused and counted only as not identified, with one whose first record names another session or folder, and nothing else of it is counted; the folder is the recovery record's, or the inbox's.
  On Claude Code it also reads a session's subagents' files, `<id>/subagents/*.jsonl` beside the session's file, through no link, once every session file is read, and only of a session that nothing has put out (see "What is left out").
  A file of a session the inbox does not hold is counted by name and never opened, and nothing outside the hosts' folders of chats is listed or opened.
  Each file is read once, a row at a time under the capture audit's limit of 32 MiB on a row; a longer row is scanned for a record control as recovery scans one, and counted as too long.
- **What it prints.** A format number (`shapes.Format`, 1), eva-md's version, the day; `parts`, for the inbox and each host, `counted`, `none_recorded` or `incomplete` (time ran out, or a file could not be read, so a floor); `days`, the first and last day a row read is dated, by host; `hosts`, by
  host, the files listed, read and not read by why (not known to eva-md, off the record, left out, not read whole, no folder known, not identified, refused, gone, unreadable, not reached), the chats read and left out by why, files per chat in buckets, and the rows read, too long, not JSON, with no
  time and with a time out of range (before 2000, or after the year after the day's, whose time is not used, so that the days printed stay days); `facts`, the figures under each fact's number, then by host; and `settled`, the facts settled in full, in part, and those shapes cannot settle.
  A figure that is 0 is left out, and a count of 1 or 2 is printed as `under 3`.
- **Names come from fixed lists.** A row's type, a field's name, a message's field, a content item's type and field, a payload's type and field, and the one-word values `entrypoint`, `turnOrigin`, Codex's `source` and `originator` are each counted under their own name only when the list for their
  level in `internal/ledger/shapes/lists.go` holds it, and otherwise under `other`, with how many distinct other names there were, by hash while the report is made, and in how many chats.
  The lists are the HF-01 card's, shortened to the names a source gives as a host's, and each name says its source: a file of this repository, which a test checks holds the name, or a host's published source.
  A version (Claude Code's `version`, Codex's `cli_version`) is printed only as three numbers of ASCII digits, and otherwise counted as `other`.
  A field that holds free data (a content item's text, input or content, `toolUseResult`, a payload's message, content, arguments, output, source or `turn_attribution`) is never looked into for names.
  Ids and turn ids are kept as hashes while the report is made, to count a chat once and to find the inbox's turn ids on the host's rows, and are never printed; a file's distinct other names and turn ids are kept up to 4,096 of each kind, so past that a count of distinct names is a floor.
  Before it prints, the report checks that every byte is printable ASCII or a line end and every string a word of its lists, a day, a version of three numbers or eva-md's version, and prints nothing otherwise.
- **A chat is counted once.** A session's files, its copies (the same id in another folder), its forks and resumed copies (a file whose rows name another session's id, or, on Codex, whose first record names the session it was forked from in `forked_from_id`, since a fork's file need not repeat that
  session's rows), and its subagents' files (on Claude Code, the files beside the session's; on Codex, where a subagent's thread is a session of its own, a session whose first record names the thread that spawned it in `parent_thread_id` or in its source's `thread_spawn`, or the root thread in
  `session_id`, also a thread of a thread) make one chat, wherever a figure is in how many chats; rows are counted in each file read.
  A Codex subagent's thread whose first record names no parent, as an older review or compact thread may, counts as a chat of its own.
  Codex's hooks give a subagent's thread the root thread's session id, so v1 stores its events in that chat's inbox file and holds no session under the thread's own id; the thread's file is then counted by name as one eva-md has no file of, under F-024, and is never opened, so the join by
  `parent_thread_id`, `thread_spawn` and `session_id` reaches only threads whose hooks gave their own id, as earlier versions' did.
  Those ids are kept only as hashes, and nothing else of a source is read.
  A Codex fork or subagent's thread is found to be one only once its file is read, so one of a chat put out by eva-md's records is opened and read, and what was read of it is dropped with its chat.
- **What is left out.** A session said off the record at any time, by eva-md's records (a pause in the inbox, the `off_record` or `was_off` marker, a recovery record blocked for it) or by the host's (a prompt, or a Claude Code row's own content, that is a record control, also in a row too long to
  read), and one in a folder the hooks leave out by design (as the capture audit judges it, by the inbox's and the record's folders, or by a folder the host's rows name), adds only to the count of files and chats left out, by why.
  When eva-md's records show it, its files are not opened (a Codex fork's or subagent's thread's are, as sessions of their own, and dropped with it; see "A chat is counted once"); when only the host's record does, what was read of it is dropped, its subagents' files are not listed, and nothing the
  rows of any of its files name joins it to another chat: every file first puts its chat out or not, joined to other files by their names alone, and only then are chats joined by the ids rows name: a file still in that names a chat put out goes out with that chat and joins no chat still in, which is repeated until nothing changes, and the files still in then join the chats they name.
  So on Codex a fork or a subagent's thread that is itself put out, by its own rows or by eva-md's records of it, is left out alone, and the chat it was forked or spawned from stays in, since v1's record control is per session and a fork, or a thread whose hooks gave its own id, is a session of its
  own; a thread or fork of it goes out with it, though a thread also names the root thread, and the two make one chat off the record.
  On Claude Code a subagent's file is part of its session, so one whose rows say off the record puts the session out.
  A folder is judged by its path, as written and where its links lead, and, when the config holds an exclusion by a repository's remote (`remote:`), by the remote eva-md's identity cache last gave the deepest repository holding it, its newest entry's, as the hook judges a folder by the remote it
  resolves; the cache is eva-md's own, read with no git run and no chat's folder opened, so a folder no entry holds, as after the cache was cleared, is judged by its path alone, and an entry out of date can only leave out more.
  The folder of a row too long to read is not judged, since its scan keeps no folder.
  A chat with a file that could not be read whole, or a row that could not be told, is left out the same way, and a session put out so is read no further: a record control that only its subagents' files hold is not seen, and it is counted as not read whole, not as off the record.
- **Bounded.** It stops at 10 minutes (`shapesTimeout`): what it did not reach, also a session whose inbox file or records it had not finished reading, is counted as not reached, nothing of it is counted, and every part says it is incomplete.

---

<!-- v1 docs/verification.md 043 -->
- **Shapes audit** (`internal/ledger/shapes`, `cmd/eva-md`).
  On one made-up chat of each host, every figure of `eva-md audit shapes` is exact, a tool's input, a tool's result and a call's arguments are never looked into for names though they hold names the lists hold, and the figures are the same when the home, `~/.claude` and `~/.codex`, or the folders of
  chats are named through a link (`TestFiguresOfOneChatEach`); a home with a different made-up marker in every place a value or a key can sit, a one-word value, an object in a value's place, a version with a suffix and one in full-width digits, keys at every level, one of 41 characters, row and
  payload types and subtypes in six chats, words in other scripts, files with other names and one the opener refuses, gives a report with none of them, whose every byte is printable ASCII or a line end and every string a word of the package's lists, a day or a version
  (`TestThePlantedMarkersReachNoOutput`), and the report refuses to print otherwise (`TestWriteRefusesWhatNoListHolds`); every name on a list says its source, a file of this repository that holds it or a host's published source (`TestEveryListedNameSaysWhereItComesFrom`); a count of 1 or 2 prints as
  `under 3` (`TestCountsUnderThree`); a chat with five copies, five forks and six subagents' files counts once (`TestAChatIsCountedOnce`), and so does a Codex chat with five forks whose files name it only in `forked_from_id`, whose forks are left out with it when it is off the record, and a marker
  planted as a fork's parent id reaches no output (`TestACodexForkCountsWithItsChat`), as does a Codex chat with five subagents' threads, one a thread of a thread, each naming the thread that spawned it in `parent_thread_id`, in its source's `thread_spawn` or the root in `session_id`, alone or
  together, whose threads add to no figure when it is off the record, and a marker planted in any of those places, as a string or an object, reaches no output (`TestACodexSubagentThreadCountsWithItsChat`); a session off the record by the inbox (a pause, or a line withheld for one), by either marker,
  by a recovery record blocked for it, by the host's record in a row read whole or too long to read, of its own or of a session an earlier row named, also with subagents' files or a copy in another folder that name another chat, or on Codex with subagents' threads whose files are read and dropped, one
  off the record only by a subagent's file, one in a folder left out by eva-md's records or by a row of either host, also by the remote eva-md's identity cache gives its repository, and one with a row that is not JSON or too long to follow, adds only to its own count, and two homes that differ only
  inside it print the same report; no subagents' file of a session put out is opened (`TestASessionLeftOutAddsOnlyItsCount`, `TestNoSubagentFileOfASessionLeftOutIsOpened`); a folder is left out by the remote of the deepest repository the identity cache holds it in, as written or through a link, and
  not when that repository's remote is not excluded, when no entry holds it, or when no exclusion names a remote (`TestAFolderIsJudgedByItsRepositorysRemote`); a file of the right name outside the folders of chats, in a folder linked out of them, that is itself a link, in a subagents' folder linked
  out, or of a session the inbox does not hold, is never opened, and a Codex file of another session is opened and counted as not identified (`TestOnlyAdmittedFilesAreOpened`), as is one whose first record is longer than the opener's 64 KiB (`TestALongFirstCodexRowIsNotIdentified`); a row dated
  outside the years taken is counted and its day not printed (`TestATimeOutOfRangeIsNotADay`); a report cut short by time, at a host's file or anywhere in the inbox, counts nothing of what it did not reach and says so in every part (`TestAReportCutShortSaysSo`, `TestAReportCutInTheInboxSaysSo`,
  `TestAuditShapesWithinItsLimit`); and a chat the real hooks captured is read with no text, id, path or folder of it in the report, and the command writes nothing (`TestAuditShapesOfAChatTheHooksCaptured`).

<!-- /v1 -->

## Since the split

No change yet.
