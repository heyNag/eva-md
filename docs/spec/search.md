# Search and show

- **Component:** Search
- **Paths:** `internal/resume/search.go`, `internal/resume/show.go`
- **Status:** v1 as built, frozen on 2026-11-14

The blocks below are v1's design and verification text, moved here word for word when the two documents were split; [INDEX.md](INDEX.md) lists every spec.

<!-- v1 docs/design.md 028 -->
## Search

`eva-md search <terms...> [--all] [--days N] [--limit N] [--cwd DIR] [--pages | --inbox]` finds earlier chat text in the inbox and lines of the wiki's thread, system and knowledge pages.
Only the agent runs it, when it needs an earlier chat; no hook ever does.
`--pages` searches only the wiki pages and `--inbox` only the inbox; with neither, or both, it searches both.
`eva-md show` resolves the refs it prints, except for a session with two capture files (see [known-limitations.md](../known-limitations.md)).

- **Terms.** Each argument is a term, and an argument with a space is a phrase. A term of 3 or more characters matches anywhere in an entry's text, ignoring case. A shorter one matches only a whole token, between characters that are not letters or digits.
- **Scope.** The project of `--cwd` (the working folder by default), identified as the brief identifies it, plus the general area.
  `--all` searches every project folder in the inbox and the wiki.
  In the wiki, the project's folder is found as the brief finds it, by the now page that names the project, so a folder under another name (as after a slug collision) is found too; a project folder whose now page is missing or unreadable is not searched, and the trailer says so.
  In a folder the config excludes, search prints nothing, as the brief does, even with `--all`.
- **Window.** `--days N`, 90 by default, 0 for no limit.
  Files last changed before the window are skipped, and so are entries dated before it or with a date that can't be read.
  With `--days 0` an entry with an unreadable date is shown with the date unknown.
  On a thread page a Timeline line counts by its group's day and a Now item by the page's `as_of`, so an item still in force stays findable while its page is current; a Now hit shows the time of the prompt it cites, or the `as_of` when it cites none.
- **What can match.** Prompts, replies, tool lines and plans, never events.
  An entry must pass the brief's checks: its project, host and session match the file it is in, and its session id has no control or format characters.
  Off-the-record ranges are tracked per file exactly as the brief tracks them, so nothing said off the record is printed or counted.
  On a thread page: the items of the Now, found at `<page>#Now`, and the Timeline lines, found at `<page>#<YYYY-MM-DD>`.
  A page line is matched on its kind and text, and a line eva-md wrote itself keeps its `eva-md: <code>:` label; its refs are shown but not matched.
  On a system page, in every scope that searches pages: its title, aliases and description as one line, `<title> (also <alias>, <alias>): <description>`, found at `systems/<slug>`; the code it cites as another, `code: <source>, <source>`, with each source as the page writes it, found there too; and
  each non-blank line of a `##` section, `###` lines included but not the map link line, found at the section's anchor, the heading with hyphens for spaces, such as `systems/orders#Depends-on`.
  System lines are tagged `[system]`, or `[unverified]` under `## Unverified`, and dated by the page's `as_of`.
  Knowledge pages are global too, searched in every scope that searches pages: each page, read whole with `knowledgepage.Lenient`, which calls no okf and reads a page up to 256 KiB, gives a line `<kind> [<tag>]: <title>: <statement>`, or `<kind>: <title>: <statement>` when a person's edit left its
  statement with no tag, and, when it cites code, a code line `code: <source>, <source>`, both found at `knowledge/k<N>`, tagged `[knowledge]`, or `[retired]` for a retired page, and dated by its `as_of`.
  A knowledge page that can't be read, is over 256 KiB or is not one is named in the trailer as unreadable.
  `--days` limits chats and thread pages only: system and knowledge pages are always searched.
- **Paths.** A term with a slash and no space is also read as a path, cleaned first (`./internal/orders/` reads as `internal/orders`), and matches a system or knowledge page's code line when a source holds it: the source cites that file, or a folder it lies in, ignoring case.
  A relative path is read in the project's repo, where a page citing the whole repo holds every path, and with `--all` in any repo's cited folder or file, never a whole repo.
  An absolute path inside the project's worktree is read relative to it; any other absolute path matches a source whose path it ends with or holds as a folder, in any repo.
  A source that holds the path in the case typed counts as an exact hit, and a whole-repo source never does, so the page citing the very folder ranks first.
  On a code line a path term matches a source's path only so, never as text, so these rules hold for the cited folder itself and for a prefix of its name too: `internal/order` is no path in `internal/orders`.
  It still matches a source's repo and commit as text, so `github.com/acme/ledger` finds the pages that cite that repo.
  Other lines match a path as they match any term.
- **Engine.** The search index (see "Search index") proposes the inbox lines that may hold the terms, and search reads every one again from its file and matches it as it matches a scanned line, so the index changes which lines are read and how equally good hits are ordered, never the text of a hit it shows.
  In the files it covers it can leave out a match inside a word, or in text without spaces, when it finds other hits (see "Tokens and matching").
  A streaming scan covers the rest: the tails of session files appended to since the index last saw them, files it lacks, and the pages; with no index, as on a new install, the scan covers everything, as before.
  The scan reads one file at a time, newest first, line by line, never a whole file in memory.
  Line numbers are physical, as the inbox and `eva-md show` count them.
  Before any JSON is decoded, the lowercased line must contain every term, longest first.
  A term holding a character that encoding/json may escape or replace (`"`, `\`, `<`, `>`, `&`, a control character, U+2028, U+2029 or U+FFFD) is left out of this filter, so the filter can never hide a match, and a line holding an off_record or on_record marker always passes it.
  Matching is then done on the decoded text.
  Thread pages are read the same way, one page at a time and line by line, through the page model's scanner, which reads what `threadpage.Parse` reads; okf's `LoadBundle` is never called, so the cost grows with the pages in scope, not with the wiki.
  Page lines are not JSON, so they skip the prefilter.
- **Ranking.** Every term is required first.
  Only when no entry matches all of them does search fall back to entries matching any.
  Hits are ordered by the number of distinct terms matched, then by a hit on a term in the case it was typed or on all the terms together as one phrase, then page lines before chat text, then by score (BM25 with the index's statistics, 0 without an index; see "Search index"), then newest first.
  Only the best `--limit` hits (10 by default, at most 50) are kept in memory; the rest are counted, as "about N more" when the count includes candidates of the index that were not read again and may not match.
- **Output.** Plain text.
  First one label line worded as facts: the terms, the order, and that [user] lines are the user's words and [assistant] lines earlier suggestions, not decisions; when pages are searched, also that on page lines [stated] and [accepted] rest on the user's words, [observed] on a tool result the
  assistant reported, [suggested] on the assistant's and [inferred] on neither, and that [suggested] and [inferred] lines are not decisions.
  Then one line per hit: `[role] <local date and time> · <branch> · s:<ref>#<line> · <snippet>`, with the project key after the date on `--all`.
  A page hit has the same shape, with its tag where the speaker goes (`[untagged]` when it has none), its page ref, and a snippet that starts with its kind, such as `decision:` or `next:`, and ends with its refs; on `--all` it shows its wiki folder where an inbox hit shows the project key.
  A system hit reads `[system] <day> · <ref> · <snippet>`, with no kind and no branch, and `· systems` after the day on `--all`, and with how far the page's code moved after the ref when the code status measured it (see "Code a system page cites"); when system pages were scanned, the label says
  "thread and system page lines" and adds that [system] lines are notes an agent wrote from documentation and code, that [unverified] lines were not found in a source, and what a count of code moved means.
  A knowledge hit reads `[knowledge] <day> · knowledge/k<N> · <snippet>`, such as `[knowledge] 2026-10-28 · knowledge/k1 · decision [stated]: Inbox format: The inbox stores entries as JSONL, one file per session`, with `· knowledge` after the day on `--all`; when knowledge pages were scanned, the
  label names them among the page lines searched ("thread and knowledge page lines", or "thread, system and knowledge page lines"), and when a knowledge hit is shown it adds "[knowledge] lines are lasting facts the background update promoted from earlier chats; decision and constraint lines rest on
  the user's words, and fact and procedure lines on them too or, tagged [observed], on a command, file or MCP result the assistant reported."
  When a code line is among the hits, the label also says that `code:` lines name the code a page cites and that a path finds the pages that cite it or a folder holding it, and the snippet shows the source that holds the path.
  Page snippets keep identifiers, judged line by line as the daily commit's scan judges them.
  The speaker comes from the entry's role field.
  The project key, branch and ref are cleaned as identifiers (`redact.Identifier`, as the inbox stores them), not as free text, so the entropy rule can't rewrite a name such as `local-token-refresh-worker-c9a8c7`.
  The snippet is at most 200 characters around the first match, on one line, cleaned and unforged like the brief's text, so a pasted speaker header can never start a line.
  A ref keeps the first 8 characters of the session id, as in the brief, or the full id when another session file in the inbox starts with the same 8 characters, because `eva-md show` would find both.
  After the hits, "N more; refine with other terms" when there are more.
  Last, a trailer: the files and MB scanned, the number of projects, the pages by kind, such as "1 thread page, 2 system pages and 3 knowledge pages (… MB)", the window and the time taken, "cut short" when the 5-second limit ended the scan, with "some wiki pages" or the inbox named as not searched, the number of unreadable files when there are any, and each unreadable page or wiki folder by name.
  When no page could be searched it says why, ending "so no wiki pages were searched": the wiki is unreadable, it is not the instance the config names, or, when only pages were asked for, no wiki is set up.
  With no hits it prints "No matches." and the trailer.

<!-- v1 docs/verification.md 006 -->
- **Search** (`internal/resume`, `cmd/eva-md`, `internal/systempage`).
  A planted term found with a ref that `eva-md show` resolves, and a term held only in a session id or branch not found; off-the-record ranges never printed, including one whose marker is older than the window; a speaker header pasted into a message or a branch name kept inside its hit line; the
  project plus the general area by default, and every project with `--all`; a project key and branch that the entropy rule would rewrite printed as stored; every term first, then the any-term fallback; the ranking order (terms matched, then exact case or the whole phrase, then page lines before chat
  text, then newest); a phrase given as one argument; `--limit` and the "N more" line; the window, for old files and old entries, and `--days 0`; terms that JSON escapes, such as `a<b` and `x&y`; case ignored beyond ASCII, rune by rune, so ß does not match ss; short terms matched as whole tokens; the
  full session id when another session file shares the 8-character ref, the same session in two folders included; entries that don't match their file; an unreadable file counted in the trailer; a scan cut short by the deadline inside one file; and 1,000 session files (about 50 MB), which take about
  0.1 s on an Apple silicon Mac and 0.9 s under the race detector, with no deadline: the time is logged, not bounded.
  Thread pages: a Timeline decision found by two of its words with its `#<day>` ref and tag, which `eva-md show` opens; a Now item dated by the prompt it cites; the project's pages under another folder name proved by its now page, the general area, and every project with `--all`; a forged speaker line
  kept inside its hit; an unreadable or symlinked page, a project folder with no now page and a wiki of another instance named in the trailer; `--pages` and `--inbox`; a deadline that passes among the pages.
  The command-line tests cover flags after the terms, `--` before a term that starts with a dash, usage errors, an excluded folder, `--pages` and `--inbox`, and show's `--last` and `--since` errors.
  A system page found by its title and aliases, on one line with its description, and by the code it cites: a path cleaned first, in a cited folder or file of the project's repo or, with `--all`, of any repo, an absolute path inside the worktree or by its end, a whole-repo citation holding every path
  of the project's repo and ranking below the very folder, a longer folder name never matched as a prefix, the cited folder itself and a prefix of its name judged as paths, never as a code line's text, so neither finds another repo's page without `--all` and a prefix finds none with it, while a repo's
  name still finds the pages that cite it, the snippet at the source that holds the path, and the label's sentence about code lines only when one is shown (`TestSearchFindsSystemPagesByNameAndCode`, `TestReadFrontIsLenient`).
  Knowledge pages: the statement line and the code line with their `[knowledge]` and `[retired]` tags, dated by `as_of` outside the window, path terms matched as on system pages, the label's sentence only when a knowledge hit is shown, the trailer's count, every scope but `--inbox`, a relative path of
  another project missing, the cited folder and a prefix of its name included, and found with `--all`, a prefix found by no project, and an unreadable page named (`TestSearchFindsKnowledgePages`); the label saying that [observed] page lines rest on a tool result the assistant reported, and observed
  fact and procedure lines on a command, file or MCP result (`TestLabelsSayWhatObservedRestsOn`); the label and trailer with thread, system and knowledge pages together (`TestSearchCountsEveryKindOfPage`); the hits helper reads `[knowledge]` and `[retired]` hit lines too.

<!-- v1 docs/verification.md 009 -->
- **Show by section** (`internal/resume`).
  `#Now` of a 3.1 MB page comes back exactly after reading at most 256 KiB (64 KiB today, logged); `#Timeline` (the newest 10 dates by default, never an old day filed last), `--last` and a day each read at most the page plus 32 KiB, while no test bounds what `--since` reads; at 200 days the output
  stops at 16 KB and the lines shown plus the lines counted come to all of them; a day filed twice returns both groups; a heading inside a fenced block ends no section, checked against `threadpage.Parse`.
  A knowledge page shown whole is its saved bytes, and its `#Statement` and `#History` sections, in any case, and `knowledge/map` show as any page's (`TestShowKnowledgePages`); `eva_status`'s reading of the recall index: its counts, another project's, a wiki of another instance, a linked index, and a
  missing index beside the map or the knowledge pages reported as missing, while one in a wiki with no knowledge is none yet (`TestKnowledgeFor`).

<!-- /v1 -->

## Must keep

The must-keep items of [findings.md](../v2/findings.md) this component touches, numbered from K01 in each list, and the tests that hold them.

| Id | Item | Tests |
| --- | --- | --- |
| CAP-K15 | An entry is trusted only in the file its host, session and project name, and nothing said off the record reaches search | internal/inbox:TestWalkYieldsWhatSearchCanMatch, internal/inbox:TestWalkResumesFromAPosition |
| SRC-K01 | Nothing is shown from the cache: every candidate is read again from the inbox and checked, and the index stores no text | internal/resume:TestSearchIndexAndScanAgree, internal/resume:TestSearchFindsALineAtAStaleCandidatesNumber, internal/resume:TestSearchTrustsOnlyEntriesThatMatchTheirFile, internal/resume:TestSearchDeadAndStaleFiles |
| SRC-K02 | Off-the-record ranges are never indexed, printed or counted | internal/resume:TestSearchPrivacy, internal/resume:TestSearchSkipsOffTheRecordRanges, internal/searchindex:TestOffRecordAndUntrustedNeverIndexed, internal/inbox:TestWalkYieldsWhatSearchCanMatch |
| SRC-K03 | An entry must belong to the file it is read from | internal/resume:TestSearchTrustsOnlyEntriesThatMatchTheirFile, internal/inbox:TestWalkYieldsWhatSearchCanMatch, internal/searchindex:TestOffRecordAndUntrustedNeverIndexed |
| SRC-K04 | Files open through a pinned root that refuses links, odd files and swapped inodes | internal/inbox:TestOpenSessionRefusesLinks, internal/resume:TestWikiInstanceAndProjectGuards |
| SRC-K05 | One line a hit, cleaned and escaped so stored text cannot start a line or pose as a speaker | internal/resume:TestSearchSnippetCannotStartALine, internal/resume:TestShowDoesNotTrustSpeakerOrDateMetadata, internal/resume:TestSearchPrintsIdentifiersAsStored |
| SRC-K06 | The byte prefilter never hides a match | internal/resume:TestSearchFindsTermsThatJSONEscapes, internal/resume:TestSearchIgnoresCaseBeyondASCII |
| SRC-K07 | When the index finds nothing, the files it covers are scanned | internal/resume:TestSearchFallsBackToTheScan |
| SRC-K08 | A fault in the index never takes search down: it falls back to a full scan and names a damaged folder | internal/resume:TestSearchRecoversFromAnIndexPanic, internal/resume:TestSearchRecoversFromAFaultAfterPartialMatches, internal/resume:TestSearchDamagedFolderScans |
| SRC-K10 | A replaced or rewritten file is seen by its generation, inode and line ends | internal/resume:TestSearchDeadAndStaleFiles, internal/resume:TestSearchRewrittenFileShowsEachLineOnce, internal/searchindex:TestMergeDropsDeadGenerations |
| SRC-K13 | Counts not checked say about, a deadline says cut short, and unreadable files, pages and folders are named | internal/resume:TestSearchPhraseCountSaysAbout, internal/resume:TestSearchCutShortStillReports, internal/resume:TestSearchCountsUnreadableFiles, internal/resume:TestSearchNamesAnUnreadablePage, internal/resume:TestSearchDeadlineDuringVerification |
| SRC-K14 | Time and memory are bounded: streamed reads, only the best hits kept, capped lines and blocks | internal/resume:TestSearchLimitAndMore, internal/resume:TestSearchSmokeThousandSessions, internal/searchindex:TestCandidatesKeepTheBestK, internal/searchindex:TestLineNumbersAcrossTails, internal/searchindex:TestRefreshBudgetNewestFirst |
| SRC-K15 | A wiki folder counts only when its now page proves the project's remote; the default scope is the project and the general area | internal/resume:TestSearchNamesAFolderWithoutANowPage, internal/resume:TestSearchScopeIsTheProjectAndGeneralUnlessAll, internal/resume:TestSearchPageScopeAndContract |
| SRC-K16 | Ranking weighs tool lines, puts an exact path before a whole repo, and dates Now items by their prompt | internal/searchindex:TestBM25HandComputed, internal/resume:TestSearchFindsSystemPagesByNameAndCode, internal/resume:TestSearchDatesANowItemByItsPrompt |
| SRC-K17 | Pages are found by a file they cite or a folder holding it, and tokens split at camelCase and digits | internal/resume:TestSearchFindsSystemPagesByNameAndCode, internal/searchindex:TestTokenize, internal/searchindex:TestPrefixAndShortTokens |
| SRC-K18 | Show ends a section as the page model does, caps a Timeline, and prints a full id where a short ref is ambiguous | internal/resume:TestShowSectionsSkipFencedHeadings, internal/resume:TestShowTimelineChoosesDateGroups, internal/resume:TestSearchPrintsTheFullIDForASharedPrefix, internal/resume:TestShowAcceptsFullSessionID |
| SRC-K19 | Search tests run with and without the index and compare hits; segments are byte-flipped and fuzzed | internal/resume:TestSearchIndexAndScanAgree, internal/searchindex:TestSegmentByteFlips, internal/searchindex:FuzzSegmentReader |
| SYS-K16 | The Unverified section is tagged unverified in search hits | internal/resume:TestSearchFindsSystemLines |
| MCP-K05 | Everything returned is labelled as data and cleaned against forgery | internal/mcp:TestSearchFindsWhatShowOpens, internal/mcp:TestInstructions, internal/resume:TestSearchSnippetCannotStartALine |

## Since the split

No change yet.
