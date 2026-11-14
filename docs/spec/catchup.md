# Transcript recovery

- **Component:** Catch-up reader
- **Paths:** `internal/catchup`
- **Status:** v1 as built, frozen on 2026-11-14

The blocks below are v1's design and verification text, moved here word for word when the two documents were split; [INDEX.md](INDEX.md) lists every spec.

<!-- v1 docs/verification.md 003 -->
- **Transcript recovery** (`internal/catchup`).
  The 1 MiB read window, a row longer than the window scanned in bounded chunks, a row of 256 KiB or more that records an attempt before it is decoded, the cursor saved after every row, slow rows that step down to tighter head-and-tail bounds and then to a gap event, a shorter row whose decoding used
  up the pass marked slow, a redactor panic that becomes a gap, duplicate suppression, host-generated rows that are never stored as the user's prompts, record controls found in missed rows, also beside an attachment, start-of-chat catch-up finding the newest records first, and recovered rows that never take a subagent's id.
  Going off the record marks the chat as having been off, whether recovery finds the control, the hook is asked or recovery asserts the boundary again; back on the record keeps the mark, and a compaction summary row is not recovered (`TestOffRecordMarksChatAsHavingBeenOff`).
  A Codex turn in another folder stores none of its rows but never stops recovery: a turn back home is stored, a control in the other folder is honoured in the same pass or, with the turn still running there, in a later one that stores none of it either, a block an older version saved with no reason
  is lifted at the next registration, and a chat off the record stays blocked (`TestCodexTurnInAnotherFolderDoesNotStopRecovery`).
  A transcript truncated in place starts again at its end, so a prompt missed after that is recovered (`TestTranscriptTruncatedInPlaceStartsAgainAtItsEnd`).
  An event that names no transcript keeps the cursor when nothing changed, and starts a new baseline on a branch change, so a row written on the old branch is not recovered and a later prompt is stored with the new branch (`TestEventWithoutTranscriptStartsNewBaselineOnBranchChange`).
  The host's folder and the transcript's own folder count where their symlinks lead: a path written through a link to the home, a host folder linked from elsewhere, the path written through it or through the folder it leads to, and a folder not made yet are in it, and a link from inside it out, also
  through a link to the home, a moved folder, the folder itself, relative paths and another host's folder are not (`TestInStoreFollowsSymlinks`); recovery reads a reply from a transcript whose path a host wrote through a link to the home, and none through a link out of the folder (`TestRecoveryReadsATranscriptWrittenThroughALinkToTheHome`).
  A tail window that would start on a later line of a command written over lines ending in a backslash, more than 4 KiB after the command's name, starts after the command, so the password on its `-u` line is never kept (`TestBoundedRedactDropsACommandCutFromItsName`).
  A fuzz test checks the row scanner against arbitrary input.

<!-- v1 docs/verification.md 032 -->
- **Recovery records' pruning** (`internal/catchup`, `internal/session`, `cmd/eva-md`).
  A record not saved for 30 days goes, and one exactly 30 days old, one saved in the future, the drift records, a temp file, another file and a symlink stay, and a second run removes nothing (`TestPruneRemovesRecordsOlderThan30Days`); past the newest a host keeps, the older ones go, each host counted
  alone and ties by name (`TestPruneKeepsTheNewestOfEachHost`); the record of a session a hook holds, also one whose id its file's name can't hold, left until the hook lets go (`TestPruneLeavesTheRecordOfASessionAHookHolds`); one a hook saved again after the listing kept
  (`TestPruneKeepsARecordSavedAgainAfterTheListing`), as checked under the session's lock, so also one a hook saved again after the prune read it and before it took the lock (`TestPruneKeepsARecordSavedBeforeItLocks`); one that alone keeps a chat off the record left until its marker is set, while a
  block saved for another reason goes (`TestPruneLeavesARecordThatAloneKeepsAChatOffTheRecord`); the session's lock taken without waiting, by the name the hooks lock it by, and busy while one holds it (`TestTryLockTakesTheSessionsLockWithoutWaiting`); no state folder, nothing to prune
  (`TestPruneWithoutRecords`); `eva-md maintain` prunes on a capture-only install and beside a wiki, says how many, and says nothing when none went (`TestMaintainPrunesRecoveryRecords`).

<!-- /v1 -->

## Since the split

No change yet.
