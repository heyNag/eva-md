# The brief at session start

- **Component:** Brief builder: the brief
- **Paths:** `internal/resume`
- **Status:** v1 as built, frozen on 2026-11-14

The blocks below are v1's design and verification text, moved here word for word when the two documents were split; [INDEX.md](INDEX.md) lists every spec.

<!-- v1 docs/design.md 026 -->
## Recall and fencing

### At session start

The brief is built by code from local files and git only, with no model and no network. It typically takes under 300 ms, with a 1.5 s deadline. It goes out through SessionStart, or through the first prompt when SessionStart could not deliver it.

Caps are in characters and include the label and the fence. Codex's are smaller because its plugin limits hook context to 3,000 tokens; the Now and exchange budgets scale with the cap.

| Variant | Claude | Codex | Contents |
| --- | --- | --- | --- |
| Startup or clear | 8,000 | 6,000 | (0) standing rules (at most 600, reserved first, see "Rules page"), then the project's constraints (at most 600, reserved, see "Constraints"); (1) header: project, branch, worktree, then `Previous chat: <host> / <model> · <time of its newest work, RFC 3339> · <how it ended>`, the ending being "reply recorded", "interrupted", "compacted", "no reply recorded" or "no reply (stop_failure[: <type>])", plus "Continuing from <branch>" when lineage applied; (2) opening request: the previous chat's first prompt and its ref (200), unless an exchange shows it; (3) live git, including "HEAD moved N commits since the page was written"; (4) Now by priority (3,200 on Claude and 2,400 on Codex, or 1,714 and 1,285 if stale); (5) open plan if newer than the page (600); (6) tool activity (heuristic): last test or build command and exit code (150), up to 5 files edited; (7) earlier chats on this branch (3 of about 80); (8) other branches with their last-active day (3 of about 60); (9) last exchanges verbatim with speaker and time (3,085 and 2,314, or 4,571 and 3,428 if stale or no page), the newest exchange reserved first; (10) system pointers (at most 600, reserved first); (11) footer (at most 450): the binary's absolute path and the fetch protocol, with a grep fallback when a wiki is set, reserved first |
| Compact | 6,000 | 4,500 | standing rules, header, this session's opening request, live git, Now by priority (2,100 and 1,575), open plan, tool activity (reserved before the Now), this session's last 2 exchanges (2,250 and 1,687), the newest exchange reserved first, system pointers, footer |
| Resume or fork | 1,500 | 1,500 | only what changed, the standing rules first when they changed |
| Refresh | 2,500 | 2,500 | the changed Now by priority, and the standing rules when they changed, at most twice per chat, the count running on through compactions |

A new chat in a project with no chat to resume and no Now gets the footer alone, about 150 characters, pointing at `search --all` for the other projects; a startup or clear brief puts the standing rules and the project's constraints before it and the system pointers just above it, fenced then, and still not counted as history found.

**Constraints.** Startup and clear briefs, the first prompt's fallback brief among them, list the constraints that hold for the project right after the standing rules: the recall index's constraint rows whose scope is all or whose project is the brief's, newest `as_of` first, at most 3 lines and 600 characters together, one a line: `Constraint: <title>: <statement>: <bin> show knowledge/k12`.
Compact, resume, fork and refresh briefs list none.
The block is reserved like the rules, the system pointers and the footer, and its room comes from the Now, the free text that goes whole or not at all and the older exchanges, never from those or the newest exchange.
Each line gets an even share of the room left, so one long line can't crowd out the others, and a short line leaves its rest to those after it.
To fit, the statement is cut with `…`, or left out when fewer than 20 of its characters (or all of a shorter one) would fit; then the title is cut; the command never is.
A line that cleaning as wiki text would change is left out, and the next constraint takes its place.
The brief reads `.eva-md/knowledge.tsv` by its path alone, through the wiki's pinned root after the instance check, by the strict read (see "Knowledge page"); it never lists `knowledge/` or reads a page, and a missing or damaged index gives no block.
The prompt hook records the pages these lines name, so recall never repeats them in the chat.

**System pointers.** Startup, clear and compact briefs read `systems/map.md` by its path alone (never a listing or a page; a symlinked file or folder or a map over 256 KB gives none) and add one line for each system whose `repos` include this project, at most 3: those whose `code:` cites this
project's code first, then the rest, each part in the map's order, by slug, so the page about the project's own code comes before a platform page that only lists it.
The order reads the map's lines alone and runs no git, so it takes none of the brief's time.
A line reads `System: <title>: <description> (as of <day>): <bin> show systems/<slug>`, at most 200 characters.
Then `System index: <N> systems: <bin> show systems/index`, whenever the map lists a system: the folder index, which `system write` builds with the map, lists each system in one line of its title and description, where a map line adds the page's repos and every code source it cites.
The block is at most 600 characters, reserved with the footer, and sits just before it, outside the final cleaning, which would redact slugs; a pointer that cleaning would change is left out.
To fit, a description is cut, or dropped when fewer than 20 of its characters would fit; then the day goes, then the title is cut; the command is never cut.
Resume, fork and refresh briefs have none, and a change to the map alone triggers no refresh.

**Code in the pointers.** A pointer to a system that cites the project's own code says how far it moved in place of the day: `System: <title>: <description> (code moved 14 commits since 2026-10-30): <bin> show systems/<slug>`, `(code moved 1000+ commits since ...)` or `(code unchanged since ...)`,
the largest count when the page cites several commits of the project's repo, unchanged only when every one is.
For each such commit the brief runs one `rev-list --count` in the project's worktree, with the paths the page cites there, and none when HEAD, which live git reads when the thread page has a `head`, is that commit.
It counts only while at least 200 ms remain before the deadline less the 50 ms kept for git, at most 60 ms a call and 150 ms in all, and the first call to run out of time, which is killed, stops the rest; the first-prompt fallback's 300 ms normally leaves no time for one.
A system citing only other repos' code, a count that fails and no time left show the day, and so does a count of none when the saved code status found that the page's commit lacks a cited path, or found the commit off the branch at the HEAD live git read (see "The status"); without such a status the count stands.
To fit, the description is cut or dropped first, then the note gives way to the day, then the day goes, then the title is cut.

**The Now in a brief.** Every hidden comment (`<!-- ... -->`, such as item ids and dates) is removed from the Now before it is measured or shown; source refs stay.
A Now in the thread page format is shown by priority: Decisions in force, Next, Open questions, then the summary, Last chat and Done.
Within the Now budget, Next keeps at least 25% and Open questions at least 14% when they have items (700 and 400 of 2,800); decisions in force take the rest, newest first (the page lists the newest last); the summary, Last chat and Done keep room for their heading and a `N more` line pointing at the
page, so the agent always knows they exist; room a section leaves, or a section with no items, such as decisions in force before there are any, flows to the next.
Sections are trimmed by whole items, never mid-item, and each trimmed section ends with one `N more: <thread id>#Now` line, which `eva-md show` opens.
A Now in any other format, such as a hand-written or placeholder page, is cut to the budget as it is.

<!-- v1 docs/verification.md 005 -->
- **Resume briefs** (`internal/resume`, `internal/okfadapter`).
  Every brief variant, lineage only for startup and clear, empty wiki scaffolds, text that tries to forge a speaker header, slow live git, a stray file elsewhere in the wiki that must not cost the brief its Now, and `eva-md show` with a full session id.
  The Now by priority: a Now of 40 decisions in force, 6 next steps and 3 open questions keeps every next step and open question in the startup, stale and compact briefs on both hosts' caps, shows the newest decisions first with one "more" line for the rest, and shows no hidden comment; at every
  budget from 0 to 4,200 the renderer stays within it, keeps whole lines only and accounts for every line it leaves out, and from 700 up leaves no room unused that a left-out line would fit, also for a young thread with 30 next steps and no decisions yet; a Now in another format is cut as it is.
  The size caps: with 71 chats, long messages, a long plan, many tool lines and live git at its limit, every variant on each host stays within its cap, and the startup, clear and compact briefs keep the newest exchange (the newest message and the question before it) and the whole footer though label-like lines and a 40-backtick run lengthen the text; the compact brief keeps its tool activity.
  The opening request from the head of a 5 MB session file, with a speaker header pasted into it kept on its line, hidden by an off_record event before it, and left out when an exchange shows it.
  Earlier chats on the branch (at most 3, newest first, without this session's chat or a chat that did no work), other branches (at most 3, newest first) and the tool activity line (the last test command found among other tools, also after `cd <folder> &&` or `;`, a quoted folder or several cds, shown with its cd, but not one after `||`; files once each, at most 5; `TestToolActivityHeuristic`).
  A thread page whose Timeline grew past 3 MiB still gives its Now to a startup brief, a refresh, which then repeats nothing, and `eva_status`, while one whose Now ends past 2 MiB gives none (`TestLongPageKeepsItsNow`); a second Now block below the Timeline leaves the first Now shown, while a second
  start marker before the first end still gives none (`TestNowMarkersAfterTheNowAreNotLookedAt`); the read behind it stops at the end of the marker's line within a limit the whole page is over, never at a marker in the frontmatter, reads a page without the marker whole, and refuses a start over its
  limit, a linked page and one without frontmatter (`TestReadHeadStopsAtTheMarker`, `internal/okfadapter`).
  The footer's absolute binary path, named once, its whole text at usual paths, its length without a wiki, its 450-character bound with long paths (the grep fallback left out first, then the thread id), and a first chat's footer alone.
  A refresh reads only the wiki folder it is given or the project's own, and a folder name that could break a line is never used.
  A project without a remote is found in its own folder or the one with its key's hash, never in another project's folder of that name (`TestBriefFindsALocalProjectsHashedFolder`).
  A page the page model renders through two update passes and okf saves is read as fresh, or stale when its `as_of` predates the last exchange, with its items by priority, no hidden comment and no retired decision; the page setup writes parses and renders back byte for byte.
  The pending line: a startup or clear brief says an update for the previous chat is pending, right below that chat, while its file has a backlog a pass could take, 600 bytes past a watermark and not 599, or any rest of a group a pass stopped in, and not once the update caught up, for a rest it found
  too small, with updates off or no wiki, in a compact brief or for a lineage chat on another branch.
  The brief lists the session files it read, newest first, with their host, id, time and size, for the catch-up.
  It reads at most 64 session files, counting the files opened, never time: 64 of 80 recently changed general-area files of old chats, none of the general-area files changed before the 12-hour window while it still lists them, and exactly 64, read and listed, of 70 chats each on two branches (`TestBriefReadsAtMost64SessionFiles`).
  Events never make an idle chat newest: a day-old chat's later session_end never makes it the previous chat, the header shows the other chat's newest work time, a resumed chat gets no `Other chat` line for another chat's session_end alone, and a fresh event doesn't keep a day-old general chat in the 12-hour window (`TestEventsDoNotMakeAnIdleChatNewest`).
  Work before the tail counts only on its branch: work on main before the tail doesn't make feature's single exchange the previous chat while lineage still continues from main, and a tool line the tail cuts in two counts for its own branch (`TestWorkBeforeTheTailCountsOnlyOnItsBranch`); the branch read from a raw line, with escaped quotes, a missing branch and an empty one (`TestLineBranch`).
  The project's constraints: listed after the standing rules in startup, clear and first-prompt briefs on both hosts, newest first, the project's own and those for all projects, never a fact or another project's, and none in a compact brief; an unreadable `knowledge/` folder changes nothing, since the
  brief reads the recall index by its path alone (`TestBriefListsTheProjectsConstraints`); a wiki of another instance, or one without its root index, lists no constraint (`TestWikiInstanceAndProjectGuards`); a first chat with no history gets them before the footer (`TestFirstChatGetsConstraints`); at
  most 3 lines and 600 characters, the statement cut, then left out, then the title cut, the command never, one constraint able to use the whole room, a line cleaning would change left out with the next in its place, and none from a missing, headerless or hard-linked index
  (`TestConstraintsBlockCaps`); and constraints written through the real save listed end to end (`TestBriefListsWrittenConstraints`).
  The label says what each tag of the Now rests on, [observed] on a tool result the assistant reported, and that Constraint lines rest on the user's words (`TestLabelsSayWhatObservedRestsOn`).
  The fence: its length rules, and the brief's wrap giving exactly `Fence`'s output (`TestFence`).

<!-- /v1 -->

## Must keep

The must-keep items of [findings.md](../v2/findings.md) this component touches, numbered from K01 in each list, and the tests that hold them.

| Id | Item | Tests |
| --- | --- | --- |
| CAP-K13 | Escaping and quoting keep pasted text from forging an entry, a speaker or the brief's notes | internal/freetext:TestShown, internal/resume:TestFreeTextCannotForgeSpeakerHeaders |
| BRF-K01 | The brief needs no model or network; any failure gives no context and the hook still exits 0 | internal/resume:TestGeneralAgeScopeOffRecordAndFailure, internal/hook:TestSessionStartBudgetAndMarkerErrors |
| BRF-K02 | One label outside a fence longer than any run inside; speakers only from entry fields; quoted lines indented, also after a cut | internal/resume:TestFence, internal/resume:TestHostileFencesUnicodeAndLimits, internal/resume:TestMiddleCutCannotForgeSpeakerHeader, internal/resume:TestMiddleQuotedIndentsEveryLine, internal/resume:TestFreeTextCannotForgeSpeakerHeaders, internal/resume:TestOtherChatLineCannotForgeSpeaker, internal/resume:TestGitSubjectCannotForgeSpeaker |
| BRF-K03 | What the brief shows is redacted again with the union of the rules | internal/freetext:TestShown, internal/resume:TestCleanPage |
| BRF-K04 | Off-the-record ranges leave the brief, and no brief comes while a chat is off the record or its transcript is unread | internal/resume:TestGeneralAgeScopeOffRecordAndFailure, internal/resume:TestOpeningRequestFromHeadRead, internal/hook:TestCompactResumeAndOffRecordSuppression, internal/hook:TestOffRecordAndResume |
| BRF-K05 | Wiki reads are pinned and bounded: no link or swapped file, the instance checked first, pages read by path, the Now read from the head | internal/resume:TestWikiInstanceAndProjectGuards, internal/resume:TestStrayWikiFileKeepsTheNow, internal/resume:TestLongPageKeepsItsNow, internal/resume:TestNowMarkersAfterTheNowAreNotLookedAt, internal/resume:TestUnusableMapGivesNoPointers |
| BRF-K06 | The brief's cost is bounded however long chats grow | internal/resume:TestBriefReadsAtMost64SessionFiles, internal/resume:TestProgramRunFilesBeyondThe64AreBounded, internal/resume:TestLargeInboxTailReferencesAndCancellation, internal/resume:TestOpeningRequestFromHeadRead |
| BRF-K07 | Another branch's Now comes only as labelled lineage at a startup or clear, never on a resume, fork or compact | internal/resume:TestResumeNeverInjectsAnotherBranchNow, internal/resume:TestResumeReportsLiveGitWithoutAnotherBranchPage, internal/resume:TestStartupLineageNowNamesItsBranch, internal/resume:TestStartupKeepsOwnBranchNow, internal/resume:TestCompactDoesNotBorrowAnotherSession |
| BRF-K08 | Events never make a chat the newest or the Now stale | internal/resume:TestEventsDoNotMakeAnIdleChatNewest, internal/resume:TestSessionStartDoesNotMakeNowStale |
| BRF-K09 | A chat a program ran is never the previous chat and cannot crowd a person's chats out | internal/resume:TestBriefPassesOverAProgramRunChat, internal/resume:TestLineagePassesOverAProgramRunChat, internal/resume:TestProgramRunChatsDontCountTowardThe64 |
| BRF-K10 | The newest exchange, the footer, the rules, the constraints and the system pointers are reserved and never cut | internal/resume:TestBudgetRetainsNewestReplyAndFooter, internal/resume:TestEveryVariantStaysWithinItsCap, internal/resume:TestBriefShowsTheRulesFirst |
| BRF-K11 | Freshness and provenance are shown: as_of, stale and lineage labels, the pending update, and refs with physical line numbers | internal/resume:TestVariantsFreshStaleMissingAndRefresh, internal/resume:TestBriefSaysWhenAnUpdateIsPending, internal/resume:TestRecoveredEntriesUseOriginalChronology, internal/resume:TestLargeInboxTailReferencesAndCancellation |
| BRF-K12 | Live git cannot harm or stall: no hooks or lazy fetch, a scrubbed environment, children killed at the deadline, output capped | internal/resume:TestLiveGitWithShowSignature, internal/resume:TestBriefFetchesNothingInAPartialClone, internal/resume:TestLiveGitKillsItsChildren, internal/resume:TestLiveGitRunsWithTheReadEnvironment, internal/resume:TestLiveGitOutputIsBounded, internal/resume:TestSlowGitLeavesTimeToDeliverBrief |
| BRF-K15 | An undated placeholder Now is not memory | internal/resume:TestInitialGeneralScaffoldIsNotMemory, internal/resume:TestUndatedRealNowIsPreservedWithUnknownFreshness |
| PGS-K11 | The hot path reads a thread page to its Now's end and the recall index by path, refusing links | internal/resume:TestLongPageKeepsItsNow, internal/knowledgepage:TestReadIndexStrict, internal/hook:TestRecallNeedsTheIndex |
| SYS-K08 | Hooks run no git for pointers and read bounded local files; the brief's live count has a hard budget | internal/hook:TestPointerCarriesTheCodeNote, internal/resume:TestBriefCodeWithinItsDeadline |

## Since the split

No change yet.
