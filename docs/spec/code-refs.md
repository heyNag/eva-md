# Code a system page cites

- **Component:** Code refs
- **Paths:** `internal/coderef`, `cmd/eva-md/code.go`
- **Status:** v1 as built, frozen on 2026-11-14

The blocks below are v1's design and verification text, moved here word for word when the two documents were split; [INDEX.md](INDEX.md) lists every spec.

<!-- v1 docs/design.md 005 -->
## Code a system page cites

A system page's code sources are its tie to the code: no other key holds it, and staleness is measured, never written into a page or the map.
`internal/coderef` does the measuring, with git calls hardened as identity's are and pathspecs read literally; it never fetches and never writes into a clone.
Lazy fetches are off: every git call runs with `GIT_NO_LAZY_FETCH=1`, and in a clone that may be partial it also passes git's `--no-lazy-fetch`.
A clone may be partial when its config or `config.worktree` has a `promisor` or `partialclonefilter` key, `extensions.partialclone`, or an `include` or `includeIf` section, read by their names, never by a word in a value such as a remote's URL, or a line continued with a backslash or a section
header it can't read; or when its git folder is not in the form git writes; eva-md reads those files without starting git.
A git older than 2.44 ignores the variable and refuses the option, so in such a clone the call fails and reads as git failing; it never fetches.
No signature is checked (`log.showSignature=false`).
A call that runs out of time is killed together with every process it started.

- **Clones.** eva-md finds a repo's clones through the identity cache, which holds an entry for every worktree a chat ran in: newest entry first, the working folder's own project first of all, at most 8 a repo listed (of at most 4,096 entries of at most 16 KB each) and 3 tried for each commit.
  Each is resolved again, through the cache, before use, so a folder that is gone or now holds another repo is never measured.
  The cache holds an entry for a worktree under each git build and environment that resolved it; the finder lists each worktree once, by its newest entry, so one clone never takes more than one of the 3 tries.
  A repo never chatted in on this Mac has no clone.
  Hooks never look for clones.
- **At write.** `eva-md system write` checks, within 3 s and 1 s a git call, each repo and commit the page cites in a local clone, the folder it runs in first: `cat-file -e` for the commit, then one `ls-tree <commit> -- <paths>`.
  A path the commit lacks refuses the page, naming the source's line, as a typo or an invented file would.
  A commit the clones lack (not fetched, rebased away or cut off by a shallow clone), a repo with no clone, git failing and the time running out are notes, printed after the `wrote` line with the clone checked in, so a write never waits on a clone.
  Nothing about movement is measured then.
- **Measured.** For a repo and commit a page cites, eva-md tries its clones in order and moves to the next one only when a clone lacks the commit.
  Git failing or running out of time in a clone ends the search there, recorded as `git` or `timeout`.
  In the clone it settles on: `rev-list --count --max-count=1001 <commit>..HEAD -- <paths>` (the whole repo when the page cites it so), which counts the commits that touch those paths up to 1,000 ("1000+"); `merge-base --is-ancestor`, since a commit off HEAD's history is counted from the commit the
  two have in common; `ls-tree HEAD -- <paths>` for the paths gone at HEAD, as after a rename, which also stops the count growing; and, for printing only, the newest change's day and subject from `log -1`, redacted, on one line and at most 80 characters.
- **The status.** `eva-md system status [<slug>...] [--json]` and the daily job measure every page, 10 s a call, 120 s a pass and at most 64 repo and commit pairs (past them, a pair is recorded as not run), and save `<state>/code-status.json` (0600, derived, local to this Mac and never committed):
  for each page its as_of and review day, and for each repo and commit its paths, its state (`unchanged`, `moved`, `not_on_branch`, `unknown_commit`, `no_clone`, `timeout`, `git` or `not_run`), the count, the paths gone at HEAD, and the clone, branch and HEAD it was measured at; the pages that
  couldn't be read; and how many pages cite code their map line lacks, as one written before the map carried code does until it is written again.
  Never a commit subject.
  With slugs, only those pages' entries change: a named page that can no longer be read loses its old entry and is listed as unreadable, and a later pass that reads it again takes it off that list.
  System status prints a line for each repo and commit, most moved first, such as `orders: code moved 14 commits since 2026-10-30 (github.com/acme/orders at 4e2bf4c: internal/orders, api/orders.proto; measured in <clone> on main; gone at HEAD: api/orders.proto; last change 2026-10-29: <subject>)` or
  `billing: not checked: no clone of github.com/acme/billing is known on this Mac; eva-md learns clones from chats in them`, a line for each page past its review day, `manual: review due since 2026-09-30`, then the counts.
  An entry that is unchanged while HEAD's tree lacks some of its paths means the cited commit lacks them too, since a removal or rename since would have been counted; a path cited with a typo is the usual case.
  System status and show lead its line with `not counted` and word it `cites a path its commit lacks: <paths>` rather than `gone at HEAD`, and no prompt pointer, search hit or `eva_status` says such code is unchanged, nor the brief's pointer once the code status has measured the page (see "Code in the pointers").
  Likewise, a commit off HEAD's history with nothing counted since the two forked gives no movement, and its line leads with `not counted`: the commit is not on this branch, with nothing counted since the commit the two have in common.
  System status sorts both kinds with the pages that couldn't be checked, and its counts line adds `; N cite a path its commit lacks` and `; N cite a commit off its clone's branch`.
  It exits 0 whatever the code did.
- **eva-md show.** After a system page or a section of it, `show` prints the same lines on stderr, each starting `eva.md: systems/<slug>: `, measured live within 3 s, 1 s a git call.
  Then one line names the system pages that link to the page, sorted, at most 12 and how many more: `eva.md: systems/<slug>: linked from systems/billing, systems/payments`.
  It reads the other system pages, never the map, up to 2,000 and each up to 25 KB, and counts their links as markdown reads them and system write lets a page link: a relative `<slug>.md`, perhaps with a `#section`, outside code spans.
  Stdout stays the page alone, so a page shown is written back byte for byte.
  The map, other refs, and a page that cites no code, has no review due and no page links to get no line.
- **Where it shows.** The brief's pointers count the project's own code live (see "System pointers"); a prompt's pointers read the status (see "Per prompt"); the doctor's `system code` row reads it (see "Health check").
  A system page's search hits and `eva_status`'s systems read it too, with the system map, never git, and say it only where a prompt's pointer would: a hit after its ref, without the day it already shows, as in `[system] 2026-10-28 · systems/orders#Interfaces · code moved 14 commits · <snippet>`, and `eva_status` after the slug, as in `orders (code moved 14 commits since 2026-10-30)`.
  A search reads them once, and only when a system page's line is among the hits it prints.
  A review day is shown by show, system status and the doctor, never in a hook's pointer.

<!-- v1 docs/verification.md 026 -->
- **Code refs and staleness** (`internal/coderef`, `internal/identity`, `internal/systempage`, `internal/resume`, `internal/hook`, `internal/health`, `cmd/eva-md`).
  Code sources read with folders, trailing slashes, every remote form and project keys, and refused for `..`, a `.` part (`./x`, `x/./y`, `x/.` and `.`), spaces, absolute paths and `](`, and for control, format and space characters and bytes that are not UTF-8 (`TestParse`, `TestFromMap`); counts
  against temporary repos, unchanged, moved, by one path and by several together, a folder, a commit the clone lacks, one off HEAD's history and a path renamed away, and the cap at 1,000 over a history made with fast-import (`TestCommitsAndMissing`, `TestCommitsCapped`); the newest change redacted and
  cut (`TestLast`); a slow git killed as a timeout (`TestTimeout`), along with the child it started (`TestTimeoutKillsTheGroup`); partial and treeless clones of a `file://` origin measured with no object added, with a git that honours `GIT_NO_LAZY_FETCH` and with one that ignores it
  (`TestNoLazyFetchInAPartialClone`), and how a partial clone is found, by the names of keys and sections, never by words in a remote's URL or a branch's name, past a byte order mark, and from an `includeIf` section (`TestPartial`); the newest change read with `log.showSignature` on
  (`TestLastWithShowSignature`); `GitEnv` turning lazy fetches off (`TestGitEnvTurnsLazyFetchesOff`); the identity cache listed newest first without starting git, leaving out a folder outside any repo and a damaged entry (`TestEntries`), and clones found through it, the working folder's first, at most
  8, never a gone folder or one now holding another repo, with no git for a worktree unchanged since a hook resolved it (`TestFinder`), and a worktree resolved under several gits or environments listed once (`TestFinderListsAWorktreeOnce`); a path its commit lacks giving no movement
  (`TestAPathTheCommitLacks`); a pass recording each state, its bounds of pairs and time, and never a commit subject (`TestStatus`); the status file saved private and refused when damaged, of another format, over its bound or behind a link, and a pass over some pages merged into it, a named page it
  can't read losing its old entry and listed as unreadable until a later pass measures it again (`TestSaveLoadMerge`).
  The write: 16 code sources and any number of documents pass and a 17th is refused, and `stale_after` is read, refused when not a day and saved by okf before the sources (`TestCodeSourcesAndReviewDay`); the map's code and review fields read back, an older line read without them, and commits of 7, 12
  and 40 digits passing the commit's scan in the map and index lines (`TestParseMapLineWithCode`, `TestWriteMapsCode`); a path the cited commit lacks refused by its line, and a commit the clone lacks, a repo with no clone and a check out of time written with notes (`TestWriteChecksCodeInAClone`); the
  pages' code read with the unreadable pages and those the map lacks (`TestReadCode`), a page saved at its cap read (`TestReadCodeOfAPageAtItsCap`) and a path with control characters no code source (`TestReadCodeRefusesAPathWithControlCharacters`).
  The brief's pointer on both hosts at startup, clear and compact: moved by path, unchanged, and the day for another repo's code or a commit the worktree lacks (`TestBriefSaysHowFarTheCodeMoved`); no count when HEAD is the commit (`TestBriefCountsNothingAtHead`); none near the deadline, and a slow git
  that never keeps the brief past it (`TestBriefCodeWithinItsDeadline`); the order things give way to fit (`TestPointerFitsItsNote`); the brief in blobless and treeless clones fetching nothing, also when only the global config names the promisor (`TestBriefFetchesNothingInAPartialClone`), live git
  listing only the subjects with `log.showSignature` on (`TestLiveGitWithShowSignature`), and a count of none showing the day when the saved status found a path the commit lacks, or the commit off the branch at HEAD, at the cited commit itself too (`TestBriefTakesNoCountTheStatusCantMeasure`,
  `TestBriefAtTheCitedCommitTakesTheStatus`), and live git out of time taking its children (`TestLiveGitKillsItsChildren`).
  The prompt's pointer: the note from a planted status with the description cut first and no git run, none for a status at other commits or one missing, damaged or over its bound, the order things give way (`TestPointerCarriesTheCodeNote`, `TestPointerFitsTheNote`), and the note from a real pass once a clone moved (`TestPromptNoteFromAPass`).
  A system page's search hit says the count of a planted status after its ref, and nothing for a status at other commits or none (`TestSearchSaysHowFarASystemsCodeMoved`); a page's movement is said by its map line, never for other commits, no code, an as_of that is not a day or code not measured (`TestMovementSince`).
  The doctor's row in each state, never FAIL, counting the pages that cite a path their commit lacks or a commit off their clone's branch, and `eva-md status` leaving the status as it is (`TestCodeRow`, `TestStatusLeavesTheCodeStatus`).
  The CLI: a page written in a clone, the clone two commits on, then the brief, system status, show's stderr, with its stdout still the page byte for byte, and the doctor's row agreeing (`TestCodeEndToEnd`); system status's lines, order, counts, `--json`, a run over one page and its exit codes
  (`TestSystemStatusCommand`), and its lines wording those pages, ranking them with the pages not checked and printing cleaned paths, clones and branches (`TestCodeLinesForPathsAndBranches`); maintain taking the status and failing only for an unreadable systems folder (`TestMaintainTakesTheCodeStatus`).
  The digest skill's new sentences render on both hosts with hooks.json unchanged.
  With any of git's pathspec settings in the environment, `GIT_GLOB_PATHSPECS` and `GIT_ICASE_PATHSPECS` among them, which git refuses beside `--literal-pathspecs`, the code reads still find a cited path missing, count the commits since and give the last change (`TestPathspecSettingsNeverReachGit` in `internal/coderef`).

<!-- /v1 -->

## Must keep

The must-keep items of [findings.md](../v2/findings.md) this component touches, numbered from K01 in each list, and the tests that hold them.

| Id | Item | Tests |
| --- | --- | --- |
| SYS-K03 | Sources hold no user, password or credential parameter, and paths no .., spaces or control characters | internal/systempage:TestSourcesAreGeneric, internal/coderef:TestParse, internal/systempage:TestReadCodeRefusesAPathWithControlCharacters |
| SYS-K09 | Read-only git is hardened: no hooks, locks or lazy fetch, literal paths, the group killed on timeout, output capped, never a write | internal/coderef:TestNoLazyFetchInAPartialClone, internal/coderef:TestPathspecSettingsNeverReachGit, internal/coderef:TestTimeoutKillsTheGroup, internal/coderef:TestTimeout, internal/coderef:TestLastWithShowSignature, internal/coderef:TestCommitsCapped |
| SYS-K10 | A clone is resolved again and must still hold the repo before it is measured | internal/coderef:TestFinder, internal/coderef:TestFinderListsAWorktreeOnce |
| SYS-K11 | Unchanged is never claimed for a path the commit lacks or with nothing counted, and a status at other commits is ignored | internal/coderef:TestAPathTheCommitLacks, internal/coderef:TestMovementSince, internal/coderef:TestStatus, internal/resume:TestBriefTakesNoCountTheStatusCantMeasure |
| SYS-K12 | The code status is local, private, read without links within a bound, and holds no subject; a printed subject is redacted and cut | internal/coderef:TestSaveLoadMerge, internal/coderef:TestLast |
| SYS-K13 | A write checks each cited path exists at its commit, never waiting on a clone | internal/systempage:TestWriteChecksCodeInAClone |
| SYS-K17 | Every time bound is a variable tests raise, so no test depends on speed | internal/coderef:TestMain, internal/systempage:TestMain |

## Since the split

No change yet.
