# Knowledge pages

- **Component:** Knowledge pages
- **Paths:** `internal/knowledgepage`, `internal/hook/recall.go`
- **Status:** v1 as built, frozen on 2026-11-14

The blocks below are v1's design and verification text, moved here word for word when the two documents were split; [INDEX.md](INDEX.md) lists every spec.

<!-- v1 docs/design.md 006 -->
## Knowledge page

One page per lasting fact, decision, procedure or constraint, `knowledge/k<N>.md`, shared by every project in the wiki.
Only the background update writes one (see Validation 13); `eva-md doctor` and the daily maintain build again the files that list them (see "Health check" and "Daily commit and push").
`internal/knowledgepage` reads, renders and saves the pages, and every write goes through the adapter.

- **Ids.** N is a decimal number from 1, without leading zeros, of at most 9 digits. A new page takes one more than the largest N of any `k<N>.md` entry in the folder, retired pages and entries that are not regular files included, so an id is never used again. The id is `k<N>` and the ref `knowledge/k<N>`.
- **Kinds:** `fact`, `decision`, `procedure` and `constraint`, a standing limit on the work or the system: what must or must not be done, or must hold. The brief lists constraints.
- **Scope:** `project`, for the project it was learned in, or `all`, for every project. Every page records the project key it was learned in, `general` in the general area.
- **State:** `active` or `retired`. A retired page stays, listed on the map.

```
---
type: knowledge
title: Inbox format
description: The inbox stores entries as JSONL, one file per session
generated: { by: claude-code/claude-opus-5-5, at: "2026-11-02T09:30:00Z" }
status: stable
sources:
  - resource: "s:3f2a91c0#14"
  - resource: "github.com__acme__eva@4e2bf4c:internal/inbox"
as_of: "2026-10-28"
kind: "decision"
project: "github.com__acme__eva"
scope: "project"
state: "active"
---

## Statement
The inbox stores entries as JSONL, one file per session [stated] s:3f2a91c0#14 <!-- t:2026-10-28T10:00:00+01:00 -->

## History
- ~~The inbox stores entries as JSON~~ → The inbox stores entries as JSONL, one file per session (superseded 2026-10-28) [stated] s:3f2a91c0#14

Part of the [knowledge map](map.md).
```

- **Frontmatter.** okf writes its keys in its fixed order, then eva-md's flat extra keys sorted: `as_of`, `kind`, `project`, `scope` and `state`.
  The title is at most 80 characters, and the description holds the statement, at most 300, on one line; the body holds the same text, so an index line and okf's drift check see it.
  `generated.by` is the update's `<host>/<model>`.
  `as_of` is the local date, in the entry's own offset, of the newest entry the statement cites.
- **Sources.** The inbox refs of every statement the page has held, at most the last 20, then its code sources, `<project key>@<commit>:<path>`, at most 3, at the commit the batch's HEAD names, each one that `coderef.Parse` reads back. A page of the general area, or one written from a batch with no commit, cites no code, and its paths are counted as dropped.
- **Body.** `## Statement` holds one line: the statement, its tag, its refs and a hidden comment with the time of the newest entry it cites, against which a later supersede or drop is checked.
  `## History` follows once it has lines, oldest first: `- ~~<old>~~ → <new> (superseded YYYY-MM-DD) [<tag>] <refs>` for a supersede and `- ~~<statement>~~ (retired YYYY-MM-DD: <reason>) [<tag>] <refs>` for a drop.
  Code ends the page with `Part of the [knowledge map](map.md).`
  No section is named Timeline or Now, which the MCP server's cut hint reads as a thread page's.
- **Escaping.** The title, statement, reason and history go through the thread page escaper (`threadpage.CleanLine`), so no text can forge a heading, a fence, a comment, a link, a tag or a frontmatter key, and a `---` in a title or statement becomes `--`, since okf refuses it there.
  A page whose title or Statement line a person edited to hold `---` or a line break, a JSON-quoted title that unquotes to one included, reads as damaged: it is never written, an operation on it is skipped with reason `damaged` and the rest of the save goes on, and it stays listed read-only in the recall index.
- **Never written.** A page that is locked (`locked: true`, `yes` or `on`, in any case), verified, of another type or can't be read back safely is never written: the operation on it is skipped with that reason.
  A page is verified when okf reads a `verified` entry from it, or its frontmatter has a top-level `verified` key with any value but an empty one, inline or in the lines under it, so `verified: true`, a plain value and a block mapping count too, though okf reads no entry from them and drops the key when it writes (`okfadapter.IsVerified`, which thread, rules and system pages use too).
  Text eva-md doesn't know in the Statement or History section, or before the first section, makes a page damaged, and so does a body okf's save would refuse: a line that trims to `---` opens or closes a block, and a line in such a block that starts with `type`, `status`, `generated`, `verified`,
  `governance`, `code_refs` or `stale_after` and a colon, in any case, code fence or not (`okfadapter.SmugglesFrontmatter`), so the operation is skipped rather than failing the pass.
  Other `##` sections a person adds, extra frontmatter keys and extra sources are kept through a write.
- **A damaged page is still listed.** A page okf reads as `type: knowledge` whose frontmatter values eva-md reads, but whose body it can't read back safely, is read leniently, as search reads a page: text eva-md doesn't know is passed over, and the statement is the first statement line with its tag, or else the first one without, or else the description.
  It is listed on the map and in the folder index by its title, counted by its kind, held against a repeated add, and listed in the recall index as `read-only`, with no tag when its statement has none, so it is still recalled and, as a constraint, listed by the brief; it is never written.
- **Saving.** `knowledgepage.Save` holds the wiki lock once: it reads every page, applies the pass's operations in order, then writes the map, `knowledge/index.md` and the recall index, and adds the root index's line once, each only when it changes.
  Running the same operations again writes nothing: an add whose statement, as lowercased words of letters and digits, an active page that applies to the save's project (one for all projects, or the project's own) already holds is a duplicate, a locked, verified or damaged one included, a supersede the page already holds is the same, and a drop of a retired page is passed over.
  Another project's own page with the same statement is no duplicate: the statement may hold here too.
  A supersede may change the kind, the title and the scope, and its paths replace the code sources when they give at least one; when the update narrows a page for all projects to the chat's project, the operation names that project and the page moves to it.
  A compare-and-swap conflict is tried again, up to 3 tries in all.
  When an operation fails after the save wrote a page, the map, the folder index and the recall index are still built for the pages written, and then the error is returned.
  A save that writes no page creates no folder.
  Each page saved adds its line to log.md, as any page does.
- **Map.** `knowledge/map.md`, of `type: map`, titled `Knowledge map` with the description `Every knowledge page, one line each`, built by code with actor `eva-md/<version>`, lists every page in id order, retired ones too, so none is orphaned: `- [<title>](k<N>.md): <kind> · <this project | all projects> · <project key> · as of <YYYY-MM-DD>`, with ` · retired` after a retired page's line.
  A page okf can't read, or one of another type, or a damaged page whose frontmatter eva-md can't read either, is listed as `- [k<N> (unreadable)](k<N>.md)`.
  A map a person locked or verified, or one okf can't read as a map, is left as it is: the pages are still written, and the doctor says why the map is out of date.
  The map is read without a page's 256 KiB bound, since it grows with the pages: a large map is out of date at worst, never damaged.
- **Folder and root index.** `knowledge/index.md` lists the map, every page and every other regular `.md` page a person put in the folder under `# Knowledge`, in okf's line format, a person's page by its title and description as okf reads them, or by its file name, so a save never drops its line.
  The root index gains `* [Knowledge](knowledge/index.md) - Lasting facts, decisions, procedures and constraints from earlier chats` at the first knowledge page, and loses it once the folder is gone.
- **The recall index.** `.eva-md/knowledge.tsv` (0600) is built from the pages by every save, by the doctor's repair and by maintain, and written only when it is out of date: when its bytes differ, or when the strict read below refuses it, as for a file with a second hard link, which the write replaces.
  It is a cache of the pages: deleting it loses nothing that a rebuild doesn't restore.
  When a person removes `knowledge/`, a recall index or root index line left from it is out of date: the repair leaves the index its header alone and removes the line, without making the folder again.
  Its first line is `# eva-md knowledge index 1`, then one line per active page, newest `as_of` first, then the newest cited entry, then the highest id, with 11 tab-separated fields: `k<N>`, the kind, the tag (empty only on a `read-only` row whose page lost its tag to a person's edit), the scope, the
  project key, the as_of, the time of the newest cited entry, `read-only` for a page a person locked or verified, or a damaged page read leniently (or nothing), the title, the statement, and the code paths separated by commas.
  Tabs, line breaks and control characters in a field become spaces, and runs of spaces one.
  It holds at most 512 KiB: past it, the pages with the oldest `as_of` are left out, still pages that search finds, but neither recalled nor listed.
  Retired pages are never in it.
  A reader skips a line that is not such a row, and reads nothing from a file without that header.
- **The strict read.** The prompt hook and the brief read the recall index by its path alone, through the wiki's pinned root: a regular file with no link anywhere on its path and one hard link, at most 512 KiB, read up to 512 KiB and one byte. They never list `knowledge/` or read a page. The update's plan and `eva_status` read it the same way.

<!-- v1 docs/verification.md 024 -->
- **Knowledge pages** (`internal/knowledgepage`).
  A save's exact page, map, folder index and recall index bytes, the index 0600, the root line once, the log.md line, okf's validation clean, `Read`, `Lenient` and `Render` agreeing and every code source read back by `coderef.Parse` (`TestSaveWritesPagesMapAndIndexes`); the same adds, supersede and
  drop run again leaving every file byte for byte, a restated add a duplicate (`TestSaveIsIdempotent`); the exact History lines, `as_of` in the entry's own offset, sources merged, a retired page on the map and out of the index, and kind, title, scope and code changed by a supersede
  (`TestSupersedeAndDropKeepHistory`); a retired page and a stray `k7.md` folder both counting toward the next id (`TestIDsAreNeverReused`); locked, verified, damaged and other-type pages never written and skipped with their reasons, missing, invalid and duplicate changes skipped, a page of another
  type unreadable on the map, and a damaged page listed by its title, read-only in the index and holding its statement against a repeat (`TestPagesEvaMdLeavesAlone`); a person's `---` block with a `Status:` line making a page damaged, skipped without an error and still listed
  (`TestSaveSkipsAPageOKFWouldRefuse`); a title edited to hold `---`, a JSON-quoted title holding a line break and a Statement line holding `---` each making a page damaged, its drop skipped as damaged while an add beside it is written and the page left as it was, read-only in the index
  (`TestSaveSkipsATitleOrStatementOKFWouldRefuse`); a failed write after a page was saved still building the map and the indexes for it (`TestSaveRebuildsAfterAFailedWrite`); `verified: true` and a block mapping never written and kept (`TestVerifiedForms`); a removed folder leaving the recall index
  its header and the root index without its line, the folder not made again (`TestRefreshWithoutTheFolder`); a map past 256 KiB rebuilt, not damaged (`TestRefreshRebuildsALargeMap`); a person's page in the folder keeping its index line (`TestFolderIndexKeepsOtherPages`); a hard-linked recall index
  rebuilt so the readers read it again (`TestRefreshRewritesAnIndexTheReadersRefuse`); a narrowed supersede moving its page to the chat's project (`TestSupersedeMovesThePage`); `Written` giving only lines the save then holds (`TestWrittenIsWhatSaveWrites`); another project's own page no duplicate of
  an add (`TestDuplicatesApplyToTheProject`); forged headings, `---`, comments, tags, links, fences and key lines escaped (`TestHostileTextIsEscaped`); at most 3 code sources, bad, repeated, extra, commit-less and general-area paths dropped and counted (`TestCodeSources`); `Refresh` doing nothing
  without the folder, reporting stale files without repair and restoring a save's exact bytes with it, a locked map reported and left (`TestRefreshRepairs`); a person's section with a fenced `##` line, an https source and an extra key kept through a supersede (`TestSupersedeKeepsWhatAPersonAdded`);
  3,000 pages keeping the index within 512 KiB, the oldest left out, retired pages never listed, ties by time then id, fields cleaned and paths with commas dropped (`TestIndexBoundsAndOrder`); every kind of bad line skipped, and another header or none read as nothing (`TestParseIndex`); the strict
  read refusing a missing file, a hard link, a symlinked file or parent, a parent linked inside the wiki, which only its own check refuses, an oversize file and a folder, and reading an index of exactly 512 KiB while refusing one byte more (`TestReadIndexStrict`); a page whose statement a person left
  without a tag, or without a statement line, staying in the recall index read-only with no tag, recalled and listed as a constraint, while a writable row with no tag is refused (`TestATaglessPageStaysInRecall`); `TestParseID`, `TestLenientHandEdits` and `TestConstraints`.
  `BenchmarkRecall` takes about 1.7 ms a recall over a full 512 KiB index on an Apple silicon Mac.

<!-- v1 docs/verification.md 025 -->
- **Prompt recall** (`internal/hook`, `internal/knowledgepage`).
  The scoring: whole words only, the 2-term minimum, the whole title or 3 terms, the skip, the project and all projects first among equal scores, a sibling's row after them and left out apart, the floor, only the first and last 32 KB of a long prompt, the 64-term cap, the stoplist, case and Unicode
  (`TestRecallRules`); 73 labelled prompts, each asked in its project, positives, the other project's facts asked in it, by name in the first project and unnamed there, which recall them only when the two share a system page, one- and two-word near misses, common words and three very long prompts, at
  indexes of 1, 3, 10, 36 and 500 rows, with the two projects apart and sharing a page: each row alone, runs of 3 and of 10 of the 36 rows from two projects, all 36, and the 36 with 464 neutral rows that hold none of the prompts' words, failing below 90% precision or 60% recall at any size in either
  and logging a sweep of floors at each size with the false positives and misses (`TestRecallPrecision`; at 2.5, 100% precision at every size in both, and 90.0% to 92.5% recall apart and 87.8% to 89.8% sharing a page); a fact's score the same alone and among 1,000 rows, and a prompt that restates it recalled from both (`TestRecallScoreHoldsAtEverySize`).
  The scope: a project's name and owner from each form of key, and none from a key that hides it (`TestProjectName`); a name matched as a whole word in any case, never joined by `-`, `_` or `.` but for a clone URL's `.git`, nor alone by a slash before it, alone only from a chat under the same owner or
  under none and as a content word, and with its owner's whole path from any chat, also inside a longer path and an ssh or https clone URL, while the end of an owner's path, as `backend/api` alone or in a code path, names no other owner's project in nested groups, an owner that is a host alone named
  with a slash or a colon, and a local repo by its whole key from any chat, never by a hashed key (`TestPromptNamesAProject`); rows of the project, of every project, of a sibling and of a named project kept and no other, from a repo, the general area and a `repo-<hash12>` project, with the siblings
  asked once and only when a strong row of another project needs them, another client's row in nested groups kept out of a chat by `backend/api` and a code path and recalled by its owner's whole path, and a local repo's row recalled by its key in a repo with a remote (`TestRecallScope`).
  The hook, its index built through the real save with 30 facts in two projects: a weak match gets nothing, a strong one exactly the label, the fence and its line, the same prompt again nothing, and the prompts marker exact (`TestPromptRecallsKnowledge`, both hosts); the block after the pointer lines
  with a blank line between, alone without pointers, and no reminder on either prompt (`TestRecallAfterPointers`); nothing with `recall = false`, for another instance's wiki, in a program-run chat, for an agent's prompt or a built-in command, or off the record, and the fact back once on the record
  (`TestRecallGates`); nothing for a missing, symlinked, hard-linked, oversize or headerless index, a `.eva-md` folder that is a relative link inside the wiki, which only the index's own check of its folders refuses, or a damaged row, while a damaged extra line is skipped (`TestRecallNeedsTheIndex`);
  the brief's column-0 `System:` and `Constraint:` lines recorded and never recalled (`TestRecallSkipsTheBriefsConstraints`); a startup or clear brief letting a fact come again and a compact one not (`TestClearRecallsAgain`); at most 3 of 4 rows, a row that doesn't fit passed over for a shorter one,
  the 'Read one' end left off when it doesn't fit, a line the secret scan flags dropped, a long identifier that cleaning alone redacts kept as it is and a line cleaning changes for another reason dropped, as the brief's own check judges them, a fact of every project shown as `all projects` and another
  project's by its key, a row with no tag shown without brackets (`TestRecallLineWithoutATag`), a longer fence around a backtick run, and every block within 1,000 characters, three facts of a usual length whole with the end of the label (`TestRecallBlockFits`); the label saying what each tag its lines
  show rests on, the user's two tags together, and naming no tag of a row left out for room (`TestRecallLabelNamesTheTagsShown`); in a repo's chat, another project's fact left out unasked, recalled when the prompt names the project, and recalled once a system page saved through the real system write lists both repos, but not after the repos index is removed (`TestRecallKeepsToTheChatsScope`).
  The pointer, reminder and brief outputs are as before.

<!-- /v1 -->

## Must keep

The must-keep items of [findings.md](../v2/findings.md) this component touches, numbered from K01 in each list, and the tests that hold them.
An item that tests hold only in part ends with what of it no test holds, as `(untested: ...)`.

| Id | Item | Tests |
| --- | --- | --- |
| RCL-K01 | Recall and pointers read bounded local files; no git, model or network; nothing injected on error (untested: a program started by its full path) | internal/hook:TestRecallNeedsTheIndex, internal/hook:TestPointersNeedTheIndex, internal/hook:TestRecallGates, internal/hook:TestHooksStartNothingOutside, check:scripts/benchmark.py |
| RCL-K02 | Anything injected is read strictly, by path, with no link, one hard link and a size bound; a rebuild replaces an index the readers refuse | internal/knowledgepage:TestReadIndexStrict, internal/hook:TestRecallNeedsTheIndex, internal/knowledgepage:TestRefreshRewritesAnIndexTheReadersRefuse |
| RCL-K03 | Indexes are rebuildable caches written atomically under the wiki lock; pages are the truth | internal/knowledgepage:TestRefreshRepairs, internal/knowledgepage:TestSaveRebuildsAfterAFailedWrite, internal/knowledgepage:TestRefreshWithoutTheFolder |
| RCL-K06 | Each recalled line carries its tag and the label says what the tags rest on | internal/hook:TestRecallLabelNamesTheTagsShown, internal/hook:TestRecallLineWithoutATag, internal/resume:TestLabelsSayWhatObservedRestsOn |
| RCL-K07 | Scope is closed by default: own project and all-projects facts, another project only by its whole owner path | internal/knowledgepage:TestRecallScope, internal/knowledgepage:TestPromptNamesAProject, internal/knowledgepage:TestProjectName, internal/hook:TestRecallKeepsToTheChatsScope |
| RCL-K08 | A weak match adds nothing, and a score does not grow with the index | internal/knowledgepage:TestRecallPrecision, internal/knowledgepage:TestRecallScoreHoldsAtEverySize, internal/knowledgepage:TestRecallRules, internal/hook:TestNamedPrecision |
| RCL-K13 | Saving knowledge is idempotent, never reuses an id, leaves locked, verified and damaged pages alone, keeps a person's text and escapes free text | internal/knowledgepage:TestSaveIsIdempotent, internal/knowledgepage:TestIDsAreNeverReused, internal/knowledgepage:TestPagesEvaMdLeavesAlone, internal/knowledgepage:TestVerifiedForms, internal/knowledgepage:TestSupersedeKeepsWhatAPersonAdded, internal/knowledgepage:TestHostileTextIsEscaped |
| PGS-K06 | Ids are never reused, past retired lines and pages | internal/threadpage:TestApplyIDsNeverReused, internal/threadpage:TestMaxIDSeesEveryPart, internal/knowledgepage:TestIDsAreNeverReused |
| PGS-K07 | The same update applied twice changes nothing | internal/threadpage:TestApplyTwiceChangesNothing, internal/knowledgepage:TestSaveIsIdempotent |
| PGS-K09 | A person's text is never lost: other sections kept byte for byte, locked, verified or unreadable pages never written | internal/threadpage:TestParseKeepsTimelineAndSections, internal/update:TestProtectedPagesAreSkipped, internal/update:TestConflictIsRetriedOnTheEditedPage, internal/knowledgepage:TestPagesEvaMdLeavesAlone, internal/knowledgepage:TestSupersedeKeepsWhatAPersonAdded |
| PGS-K11 | The hot path reads a thread page to its Now's end and the recall index by path, refusing links | internal/resume:TestLongPageKeepsItsNow, internal/knowledgepage:TestReadIndexStrict, internal/hook:TestRecallNeedsTheIndex |
| PGS-K12 | The recall index, the map and the indexes are caches rebuilt from the pages | internal/knowledgepage:TestRefreshRepairs, internal/knowledgepage:TestRefreshWithoutTheFolder, internal/knowledgepage:TestRefreshRebuildsALargeMap |
| PGS-K13 | Another project's knowledge comes through a shared system page or by name; scope all needs the user's own words | internal/knowledgepage:TestRecallScope, internal/knowledgepage:TestPromptNamesAProject, internal/update:TestKnowledgeScope, internal/update:TestKnowledgeScopeOfASupersede |
| PGS-K15 | Every supersede and drop of knowledge or a rule leaves a Timeline line and keeps its history | internal/knowledgepage:TestSupersedeAndDropKeepHistory, internal/update:TestPassWritesTheRulesPages, internal/update:TestSettleSkippedRules, internal/update:TestALockedRulesPageLeavesNoTimelineLine |
| PGS-K16 | Model output is redacted again, and every line a knowledge save writes passes the commit's scan first | internal/update:TestValidateRedactsCapsAndEscapes, internal/update:TestKnowledgeSecretScan, internal/update:TestKnowledgeSecretScanPassesPlainText |
| SYS-K15 | Sibling scope for recall comes from pages listing two or more repos | internal/systempage:TestReposIndex, internal/systempage:TestReposIndexGoesWithTheLastSharedPage, internal/hook:TestRecallKeepsToTheChatsScope |
| EVL-K11 | Recall's precision-first floor and a score independent of index size are held in CI | internal/knowledgepage:TestRecallPrecision, internal/knowledgepage:TestRecallScoreHoldsAtEverySize |

## Since the split

No change yet.
