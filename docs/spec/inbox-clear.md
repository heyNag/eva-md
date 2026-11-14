# Removing captured chats: its offline checks

- **Component:** Inbox store: removing captured chats
- **Paths:** `internal/inbox`, `cmd/eva-md/inbox.go`
- **Status:** v1 as built, frozen on 2026-11-14

The blocks below are v1's verification text, moved here word for word when the two documents were split, then out of [inbox.md](inbox.md) on 2026-11-14 to make room for its list of what must be kept; [INDEX.md](INDEX.md) lists every spec.

<!-- v1 docs/verification.md 008 -->
- **Removing captured chats** (`cmd/eva-md`, `internal/inbox`, `internal/lint`, `internal/catchup`, `internal/searchindex`, `internal/health`, `internal/resume`).
  `eva-md inbox clear` previewing a session named by a ref, with its host, id, project, last write and size, the 3 refs a thread page holds to it and what the wiki keeps, and changing nothing; then with `--apply` removing its file, watermark, transcript recovery record and its project's search index,
  and recording it, while its off-the-record marker, its lock file, the project's other session and the general area stay; the lint then counting its refs as removed, and the index built again finding the kept session's words and none of the removed one's (`TestInboxClearPreviewsThenRemoves`);
  `--project`, `--before` and both together, a project folder left empty removed, and a file deleted by hand found by the watermark it left, whose refs the lint fails until a removal records it (`TestInboxClearChoosesByProjectAndDay`); each usage error, a filter given an empty value beside another
  among them, removing nothing, `--before 0001-01-01` read in UTC, where it is the zero time, choosing nothing, alone or beside another filter, 8 characters two sessions share choosing nothing and naming both until a project tells them apart, nothing to remove, and a project folder that can't be read
  named before the list (`TestInboxClearArguments`); the list naming 20 captures and counting the rest (`TestInboxListNamesTwentyAndCountsTheRest`); the update's lock or a project's index held removing and recording nothing, a session a hook holds stopping the removal with its file kept and
  unrecorded: the removal seen in the goroutines' stacks waiting for the hook's own lock, keeping the file and taking and recording it once the hook lets go, and saying to run it again once its wait runs out, and a session whose id its file's name holds changed locked by the id the hooks lock it by
  (`TestInboxClearRemovesNothingWhileBusy`); each session recorded just before its files go, so a removal that fails at a session's transcript recovery record records and removes nothing of it, and one that stops at a second session a hook holds records and removes the first alone
  (`TestInboxClearRecordsOnlyTheChatsItRemoves`); a chat off the record keeping its transcript recovery record, still blocked, while its file and watermark go (`TestInboxClearKeepsTheRecordOfAChatOffTheRecord`).
  In the inbox: the captures listing session files and the watermarks of files gone, and nothing else (`TestCapturesListsFilesAndLeftWatermarks`); a removal taking the file before its watermark, so one cut short leaves the watermark alone, listed for the next, and refusing a capture outside its folder
  (`TestRemoveTakesTheFileThenItsWatermark`); the record of removals appended 0600 under a flock, a line a killed writer left ended and passed over, and a link in its place refused (`TestRecordOfRemovals`); and the session id the hooks lock by read from the file's first entry (`TestSessionIDIsTheHooksID`).
  A recovery record whose cursor lags behind the hooks removed with the capture, so recovery brings none of its rows back, while one appending to another project folder stays and a damaged one goes (`TestForgetKeepsRecoveryFromBringingBackARemovedCapture`); a blocked record that alone keeps a chat off
  the record, its markers unwritable, kept, so the chat's next registration keeps it off the record and recovery stores nothing it says next (`TestForgetKeepsARecordThatKeepsTheChatOffTheRecord`).
  A held index folder refusing a search's top-up, then dropped with no file left holding a removed word, and built again by the next catch-up (`TestHoldDropsAFoldersIndexWhole`); a folder another writer holds making the hold busy, holding nothing (`TestHoldIsBusyWhileAWriterHoldsAFolder`).
  The lint sorting refs into removed, also by a short id and after a chat still open made its file again, lost, with only a watermark left, past a file's end, and another Mac's (`TestLintSortsRefsToRemovedLostAndOtherMacsChats`), failing none in a wiki with no inbox, as one adopted from another Mac
  (`TestLintWithoutInbox`), counting refs as unknown, never as another Mac's, and naming the folder while a project folder or a session file in it can't be read (`TestLintCountsRefsUnknownWhileAFolderCantBeRead`), and counting refs to chosen sessions by full and short ids (`TestCiting`); the doctor's
  source refs row naming refs to removed and other Macs' chats without failing, unknown while refs couldn't be checked, naming the folders that couldn't be read, and its fix naming the command (`TestLintRows`); `eva-md show` saying a ref's chat was removed, by a full or short id, and a ref to a chat never captured still not found (`TestShowSaysAChatWasRemoved`).

<!-- /v1 -->

## Must keep

The items this component must keep, and the tests that hold them, are listed in [inbox.md](inbox.md#must-keep).

## Since the split

No change yet.
