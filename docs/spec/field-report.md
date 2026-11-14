# The field report

- **Component:** init / install / uninstall / doctor: the field report
- **Paths:** `internal/health`
- **Status:** v1 as built, frozen on 2026-11-14

The blocks below are v1's design and verification text, moved here word for word when the two documents were split; [INDEX.md](INDEX.md) lists every spec.

<!-- v1 docs/design.md 018 -->
## Field report

`eva-md doctor --report` (`health.Field`) prints JSON with a `format` number (1), eva-md's version, the day, and a `parts` map that says of each part `counted`, `none_recorded` (its figures are zeros), `unreadable` (its records couldn't be read: zeros), `incomplete` (a folder or file there couldn't
be read, or its 10-minute limit ran out: its figures are a floor) or `no_wiki` (the part is left out).
A record that is missing counts as nothing recorded; one that is there and can't be read is never counted as zeros.

- **Operations** count the 14 days that end today: days with an attempt marker, days a commit run recorded something, commit runs by outcome and by outcome.reason, pushes by result, the needs-trust flag's age in days and whether it is still awaited, each hook event's runs and failures among the last
  runs its heartbeat keeps from the start of the first day, and the kind of notice a chat start would show now, since eva-md keeps no count of the notices it showed.
  The kind comes from the notice's fixed words alone, never from text a record put into it, and is `unknown` when the report was cut before the notice was read.
- **Hooks** give the heartbeats' week (UTC days): each event's days, runs, failures and time buckets, what was held back, and what was delivered, as the doctor's rows read it.
- **Updates** give the figures of `status --updates`, keyed again by the names this release writes.
- **Backlog** comes from `health.Unorganized`, which reads every session file, not only a folder's 64 newest: per host, the sessions longer than their watermark, their bytes, the day and age of the oldest entry past a watermark, those with no dated entry and those with a damaged watermark, and
  apart, the sessions the update leaves until more is appended, as a turn not yet finished, with their bytes and oldest entry.
  A project folder or session file it can't open or read is counted as unread, and it then says it is not complete.
- **The record** gives the inbox's sessions and bytes by host, entries by kind, lines passed over (too long, or not a well-formed entry of their file), folders and files unread, redaction markers by kind, the replacements entries say were made, each entry's up to the complete markers its text holds,
  and what the current redaction rules (`rules_version`) would catch in the stored text, by marker kind and tier, with the entries and sessions that hold it.
  That is what an older release missed, since redacting redacted text catches nothing more; entries cut at the size limit when stored are left out of it and counted apart, since a cut can join a word that marks a secret to a value the rules never saw beside it.
- **The wiki** gives pages by kind from the folder listing, the tool lines' runs of `eva-md system write` and edits of files under the wiki (lines naming only relative paths are counted apart), and the code status's entries by state.

Every key and every word is one eva-md's code writes; any other name a file holds is counted under `other`.
It runs no command, makes no network or model call, repairs nothing and writes nothing.
It does hold, by kind, counts of secrets the current rules would catch in what is stored, how much was held back while a chat was off the record, and the day the first entry was captured: counts and dates, but the person who passes the report on should know they are there.

<!-- v1 docs/verification.md 037 -->
- **Field report** (`internal/health`, `cmd/eva-md`).
  On a fixture home whose waiting session sits past its folder's 64 newest files, `eva-md doctor --report` gives exact counts of operations, the hooks' week, update figures, the backlog with a session held for more apart, the record with a line too long, an entry cut at the size limit and one that
  claims far more replacements than its markers, what the redaction rules would catch and the wiki, the same bytes on a second run (`TestFieldReportCountsTheFixture`); a made-up marker planted in every text, id, path, project, branch and name a counter reads, a commit's outcome, reason and push result
  and a skipped update's reason among them, is in no line of it (`TestFieldReportHoldsNoText`), and the update figures keyed by the marker are keyed again by the names eva-md writes (`TestTheReportKeysTheUpdateFiguresAgain`); it changes no file (`TestFieldReportChangesNoFile`); a home with nothing
  recorded, no wiki, records it can't read, a session file or project folder it can't open, a month of commit records or a heartbeat it can't read, or a context that has ended gives zeros or a floor and says which, never zeros for what it couldn't read, and a notice it never read as unknown
  (`TestFieldReportOnHomesWithLittleRecorded`); its keys are pinned (`TestFieldReportKeysArePinned`); the names it counts by are read back from the code that writes them (`TestTheReportsNamesAreTheCodes`, `TestEveryNoticeHasItsKind`), and a notice's kind comes from its fixed words, whatever a record
  put into it (`TestANoticesKindIsNotTakenFromARecord`); at the command, a limit already spent still prints valid JSON with the parts it couldn't finish incomplete and exits 0 (`TestFieldReportWithinItsLimit`), and the command runs no git, host CLI or network tool and repairs nothing a doctor would (`TestDoctorReportRunsNothingAndWritesNothing`).

<!-- /v1 -->

## Since the split

No change yet.
