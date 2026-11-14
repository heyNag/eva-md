# The okf adapter

- **Component:** Renderer + okf adapter: the adapter
- **Paths:** `internal/okfadapter`
- **Status:** v1 as built, frozen on 2026-11-14

The blocks below are v1's design and verification text, moved here word for word when the two documents were split; [INDEX.md](INDEX.md) lists every spec.

<!-- v1 docs/design.md 002 -->
## The okf adapter

okf (okf-memory/okf-agent-memory, MIT) owns the page format: parsing, serializing, safe single-file writes and validation.
When a wiki is set up, it writes the new wiki's pages, and eva-md writes the root index and the log.
`eva-md search` makes no okf call: it reads the inbox and the thread, system and knowledge pages itself (see the Search row below).
One Go file in eva-md, `internal/okfadapter`, is the only code that imports `pkg/okf`.
It adds the protections okf lacks today.
Each protection is removed once an okf release covers it.

### okf calls per operation

| Operation | Exact calls (okf v0.8.0) and eva-md steps |
| --- | --- |
| **Open the wiki** | `filepath.EvalSymlinks(wiki)`. Check that `<root>/index.md` is a regular file. Without a root index.md, okf silently treats `knowledge/` as the wiki (bundle.go:160-172), so this check is required. Opening reads no pages, so the brief reads only the pages it needs and its cost doesn't grow with the wiki. **Validate** is the only call that loads the whole wiki: `okf.LoadBundle(root)`, then a check that `b.RootPath == root`. |
| **Read a page** | `os.ReadFile`, then `okf.ParseConcept(relPath, content)`. Keep a SHA-256 of the raw bytes for compare-and-swap. |
| **Create a thread or page** | Take the wiki lock. `okf.ValidateConceptID(id)`. `os.Lstat` must report the file missing, otherwise refuse. Check `okf.IsValidActor(actor)`. Remove a leftover okf lock file (see the protections below). Then `okf.SaveConcept(root, c, okf.SaveOptions{IsNew: true, AutoLog: false, AutoIndex: false, Actor: actor})`. fsync the folder. Regenerate the folder's index.md. |
| **Update a thread** | Take the wiki lock. Re-read the file and compare its hash with the one read before rendering. If it differs, re-apply the operations by item id (at most 2 retries). Render the body with escaping. Copy the parsed Concept, keeping `Extra`, `Verified` and `Sources`. If the new page equals the file on disk ignoring the `generated` line, skip the save. Otherwise remove a leftover okf lock file and call `okf.SaveConcept(..., SaveOptions{IsNew: false, AutoLog: false, AutoIndex: false, Actor: actor})`. fsync the folder. Only then advance the watermark. |
| **Project now.md, README.md** | Same as update, with actor `eva-md/<version>`. |
| **Writes that must land together** | `Wiki.Locked` runs several writes under one hold of the wiki lock, such as a system page with the map and the folder index, a pass's knowledge pages with the knowledge map, the folder index, the recall index and the root index's line, a project's first `now.md` with its thread page, or the root index's lines. |
| **index.md files** | eva-md writes them itself (see wiki layout), using okf's exact line format and its own atomic write plus folder fsync, under the wiki lock. `UpdateParentIndex` is not called. |
| **log.md** | eva-md writes it itself, with its own atomic write, under the wiki lock. Init writes the first `## YYYY-MM-DD` local-date group with its creation line. After that, each `Create` or `Update` that saves a page adds one line in okf's own words, ``* **Creation**: Documented concept `<path>` (<title>).`` or ``* **Update**: Updated concept `<path>`.``, to the local date's group at the end of the file, oldest first, the same line once a day. A log that is missing, a link or over 16 MiB gets no line, and the page write stands. okf's `SaveConcept` refuses log.md because it is a reserved name. |
| **Init** | eva-md's own checks. Write log.md and eva-md's own root index.md first, then remove a leftover okf lock file and call `okf.InitBundle(root)`, which skips both because they exist, so okf's index, which names okf, is never written. Then create README.md, general/now.md and general/threads/default.md as above, write the full root index.md, general/index.md, general/threads/index.md, `.gitattributes` and `.gitignore`, and last `.eva-md/schema.toml`. |
| **Validate** | Open the wiki as above, then `okf.Validate(b, okf.ValidateOptions{Drift: true, Strict: false})`. `eva-md validate` prints the counts and fails on any error, orphan or broken link, and on an invalid-actor or parent-index warning. The doctor's wiki validation row fails on errors, broken links and orphans. |
| **Search** | Not called: `eva-md search` reads its own rebuildable index of the inbox (see "Search index") and scans what the index lacks and the thread, system and knowledge pages itself, one file at a time, because `SearchAdvanced` reloads and re-tokenizes the whole wiki for every query. Never `SearchLayered`: it falls back to the working directory, `~/.okf`, `/etc/okf` and `OKF_*` environment variables (scope.go:182-237). |
| **Never called** | `AppendLogEntry`, `RelateConcepts` (it always logs, adds a level-1 section and bumps the timestamp), `UpdateParentIndex`, `UpdateConcept` (it loads the whole wiki to change one page), `WithBundleLock` (a `SaveConcept` inside it would wait for its own lock), `SearchLayered`, `Bootstrap`, `CreateToolSymlinks`, `RunMCPServer`, and anything outside `pkg/okf`. |

### Protections eva-md adds

| Gap in okf (v0.8.0) | eva-md protection | Evidence |
| --- | --- | --- |
| Create silently overwrites: `IsNew` never refuses a file already at the path. It only changes the log wording, keeps a non-human save from carrying over that file's human verifications, and skips the compare-and-swap | Lstat under the wiki lock, refuse if the file exists | mutate.go:459-553, provenance.go:20-40 |
| Its lock is a file, `.okf-write.lock`, in the wiki's root, which `SaveConcept` and `InitBundle` hold for one call. It isn't reentrant, so it can't hold several writes together, and a save killed midway leaves it behind: okf then makes each save wait up to 10 seconds for it and refuses the save, until the file is 30 seconds old, and from then on tries to remove it itself, with no pause and no end when it can't, as in a wiki folder it can't write | One wiki lock (flock on `~/.local/state/eva-md/locks/wiki.lock`) around every write, which the kernel lets go of when a process dies. Under it, the adapter removes a leftover okf lock file before each okf call that takes one, as it does before Init, under init's lock (`<locks>/init.lock`). A leftover it can't remove fails the save at once with that error, in eva-md's words and keeping the errno, and okf isn't called. Setup and init remove a leftover one the same way, under the wiki lock, before they look for changes not yet committed: it is never yours, git doesn't ignore it, and the commit setup advises for such changes would take it. One they can't remove, or a wiki lock that stays busy, refuses the wiki with that reason, never with that advice | lock.go:13-21, 62-119 |
| Compare-and-swap only for a page `LoadBundle` loaded, which reads the whole wiki | Hash check under the lock, re-apply by id | mutate.go:498-506, bundle.go:403-406 |
| Every save rewrites `generated.at` | Skip the save when nothing but `generated` would change | mutate.go:508-511 |
| Actor not validated on write | `IsValidActor` before every save, with `<host>/unknown` as the fallback | types.go:19, validator.go:179-181 |
| No folder fsync after rename | fsync the parent folder before advancing any watermark | mutate.go:51-61 |
| A killed save leaves `.tmp-okf-*` files | `.gitignore`'s `.tmp-*` line, and the commit refuses them | mutate.go:38-45 |
| A save that fails on the file system names its `.tmp-okf-*` file, or its lock file, in the error | The adapter says the step that failed in its own words, keeping the errno; a lock file okf waited for in vain is a busy wiki lock | mutate.go:38-63, lock.go:80-91 |
| Body check rejects `---` blocks holding `status:` and similar lines | Escape model text before saving. If your own text trips the check, mark the page damaged | mutate.go:423-442 |
| Log is newest-first, dated in UTC | eva-md owns log.md | mutate.go:95-147 |
| Index is append-only and unsorted, never pruned | eva-md generates sorted index files in okf's format | mutate.go:304-319 |
| Missing root index makes LoadBundle switch to knowledge/ | Assert root index.md exists and RootPath matches. knowledge/ is created only by the first knowledge page write, in a wiki whose root index init wrote | bundle.go:160-172 |
| No supersede helper | The renderer writes dated strikethrough lines in the Timeline, and a knowledge page's History | no supersede helper in pkg/okf; its only match for supersed, validator.go:203, is a "superseded verification" finding |

okf already supports recording the real author through `SaveOptions.Actor` (mutate.go:489-511), so eva-md does not need to add that protection.

Side effects to accept:
- Pages are written with mode 0644 whatever the umask (mutate.go:58). The wiki is a private repo, and the inbox stays 0600.
- `SaveConcept` normalises leading and trailing blank lines in the body.

### Gaps covered in eva-md (watch okf for these)

eva-md opens no issues or pull requests on okf. Each gap below is covered on top of okf in eva-md's adapter. At every okf review, check whether a new release covers a gap. If it does, switch to okf's version and delete eva-md's.

1. No-clobber create, and compare-and-swap for a page read on its own.
2. A bundle-level write lock that can hold several writes together and that a killed process can't leave behind.
3. A folder fsync after writes, and honouring the umask or accepting a file mode.
4. Actor validation in `SaveConcept`.
5. Skipping unchanged saves, and a clock the caller can set.
6. Log options: an append mode and a time zone setting, and `RelateConcepts` respecting `AutoLog`.
7. A sorted, pruned index rebuild with folder links.
8. A `LoadBundle` mode with no knowledge/ redirect, or a flag saying the redirect happened.
9. A narrower body check, exported so callers can check text before saving.
10. A layered search that ignores the working folder and environment, plus matched terms and snippets in search results.
11. Round-tripping flow mappings in unknown frontmatter keys.
12. Escaping `[`, `]` and `)` in index titles.
13. A parser fix so `- s:abc` doesn't split at a colon with no space after it.

### Pin and upgrade policy

- **Pin.** `require github.com/okf-memory/okf-agent-memory v0.8.0`, go.sum committed, `go 1.26.0` or later in go.mod, builds with `-mod=readonly`. v0.7.1 is the minimum version: atomic writes arrived in v0.4.1, `SaveOptions` in v0.4.4, the verification guard in v0.6.0, and v0.7.1 stopped dropping unknown keys in sources.
  `golang.org/x/crypto` is in okf's module graph but is not compiled in, because only `pkg/vault` uses it.
  In v0.7.2, `pkg/okf` resolves a relative bundle root to an absolute path before following symlinks; eva-md always passes absolute, resolved roots. v0.8.0 added a lock file in the wiki's root for each save, which the adapter clears after a killed save (see the protections above), a compare-and-swap
  for pages `LoadBundle` loaded, and an optional `scopes` list in the root index's frontmatter that `LoadBundle` refuses when it is empty or names an unknown scope; eva-md writes no frontmatter in the root index and reads no scopes.
  Its parser and serializer are unchanged, so pages are saved byte for byte as before.
  The okf line references in this section are to v0.8.0.
- **Record.** eva-md records no okf version in a wiki, and names okf nowhere you see it but each release's `LICENSES.txt`, which carries okf's license as that license asks (see "Changes to decisions", 13 and 15).
  `.eva-md/schema.toml`'s `schema_version` is eva-md's own wiki format, whose pages are okf spec 0.2 pages, and `eva-md version` prints the binary's version and that schema.
  The binary's own version is its release tag, or for a source build the day, time and hash of its commit (see `eva-md version` in "Command-line surface").
- **Regular review.** okf is reviewed weekly. Each review checks new releases for security fixes and against the list of gaps covered in eva-md.
- **When to upgrade.** Upgrade for a security fix in path, ID or parser handling, or for a release that covers a gap eva-md works around, so its version can be deleted. Don't upgrade just because a release exists.
- **How.** Work on a branch named `okf-vX.Y.Z`:
  1. Read `docs/releases/vX.Y.Z.md` and `git diff old new -- pkg/okf/{mutate,parser,validator,bundle,types}.go`.
  2. Run apidiff on the identifiers the adapter calls.
  3. Run the adapter golden tests: Parse then Serialize must round-trip byte for byte on real eva.md pages. `SaveConcept` must produce the same bytes apart from `generated.at`. Validate must give the same result on a fixture wiki. A body-escaping corpus must still be accepted.
  4. Load and re-save every page of a copy of the live wiki, and expect `git diff --exit-code` to be clean.
  5. Merge only if all of these pass.
- **Treat every okf tag as possibly breaking, patch tags included.** v0.4.4 changed the `SaveConcept` signature in a patch release, and 7 of 20 releases changed behaviour in functions the adapter calls.
- **If an upgrade breaks things.** A source break is fixed inside the adapter only.
  If an upgrade changes page bytes or rejects content eva-md writes, stay on the current pin until eva-md's adapter handles the change.
  A temporary `replace` to a local fork is allowed only to apply a security fix; it changes the module's heading in `scripts/LICENSES.txt`, whose test gives the text to use.
  An upgrade never rewrites existing pages on its own.
- **If okf doesn't work out.** This applies if okf stops round-tripping eva.md pages byte for byte (the adapter golden tests check that it does), or if the adapter keeps growing past its protections.
  - eva-md then writes its own pages. That takes a few hundred lines: parsing and serializing its few page types, an atomic write with a folder fsync, and the lock.
  - okf stays as the format spec, and as the validator in the doctor and the tests.
  - The wiki format doesn't change, so your memory is untouched.
- **Not ai-memory as the core.** ai-memory was also evaluated as the core under eva-md, and not chosen. Under eva-md, its own capture and handoff would be switched off. What remains:
  - a page store behind a server that can't be reliably stopped
  - a second store in SQLite that the markdown can't rebuild
  - unsigned commits on every event, which can't be turned off
  - an ID-named layout, so your repo would hold only a copy

---

<!-- v1 docs/verification.md 030 -->
- **Verified pages and okf's body rule** (`internal/okfadapter`, `internal/systempage`).
  `IsVerified` reading a flow mapping, `verified: true`, a plain or quoted key, a block mapping and a list, never an empty value or a nested key (`TestIsVerified`), and a system write refusing a page verified by `true` or a block mapping; `SmugglesFrontmatter` agreeing with okf's save on a `---` block holding a reserved key, in a fence too, and on bodies it saves (`TestSmugglesFrontmatter`).

<!-- v1 docs/verification.md 031 -->
- **log.md** (`internal/okfadapter`).
  Each page created or changed adds one line in okf's words under the local date's group at the end of the log; a save that changed nothing adds none, even on a day the log has no group for, the same line comes once a day, a new day starts a group, whatever the line endings; a log that is a link, or
  missing, is left alone and the page write stands; a save changes only the page and the log (`TestWritesKeepTheLogCurrent`, `TestAppendLog`, `TestSaveWritesOnlyThePageAndTheLog`).

<!-- v1 docs/verification.md 034 -->
- **okf's lock file** (`internal/okfadapter`, `internal/wikiinit`, `cmd/eva-md`).
  A leftover okf lock file in the wiki's root, stamped as taken a day ahead so okf would always wait for it and refuse the save, is removed before Create, Update and Init, which each go ahead at once and leave none (`TestLeftoverOkfLock`); one eva-md can't remove, a non-empty folder of that name or a
  file in a wiki folder it can't write, stamped an hour back so okf would retry its removal without end, fails Create and Update at once, and Init for the folder, in eva-md's words and keeping the errno, under a hang guard (`TestUnremovableOkfLock`); a save killed by SIGKILL midway leaves okf's
  temporary file, which git ignores, and its lock file, and the next save goes ahead at once and removes the lock file, after which `git add .` stages nothing naming okf (`TestSIGKILLDuringSaveExcludesTemporaryFiles`); `RemoveLeftoverLock` removes the lock file, does nothing without one and fails in
  eva-md's words on one it can't remove (`TestRemoveLeftoverLock`), and while another write holds the wiki lock it gives up as busy and leaves the file (`TestBusyLockReportsErrLockBusy`); init removes it, then adopts a wiki whose only change it was and still refuses one with another change, with the
  file gone either way, and refuses a wiki whose leftover it can't remove with that reason (`TestAdoptionRemovesTheSavesLockFile`), and setup's dirty check does the same, naming that reason, never advising a commit of everything (`TestSetupRefusesARepoWithoutRemoteAndADirtyWiki`); an error about a page whose name only holds the lock file's passes on unchanged (`TestOkfFailure`).

<!-- /v1 -->

## Must keep

The must-keep items of [findings.md](../v2/findings.md) this component touches, numbered from K01 in each list, and the tests that hold them.
An item that tests hold only in part ends with what of it no test holds, as `(untested: ...)`.

| Id | Item | Tests |
| --- | --- | --- |
| STO-K01 | Page writes go through one boundary: no-clobber create, compare-and-swap on a hash, and no save that changes only the generated line | internal/okfadapter:TestCreateUpdateAndNoOp, internal/okfadapter:TestConcurrentCreateAndCAS, internal/okfadapter:TestCreateUpdateFailures |
| STO-K02 | The wiki lock is a file lock in the state folder that a dead process cannot leave behind | internal/okfadapter:TestLockedHoldsTheLockAcrossWrites, internal/okfadapter:TestBusyLockReportsErrLockBusy, internal/wikigit:TestWikiLockIsTheAdapterLock |
| STO-K03 | Temporary file, sync, rename and folder sync come before any watermark moves | internal/okfadapter:TestSIGKILLDuringSaveExcludesTemporaryFiles, internal/update:TestKillMidSave |
| STO-K04 | No link is followed anywhere in a page's path; .git and control characters are refused; the root is checked again | internal/okfadapter:TestSymlinkAndPathProtections, internal/okfadapter:TestOpenedWikiRefusesReplacedRoot, internal/okfadapter:TestInitRefusesIndexSymlink, internal/okfadapter:TestCreateUpdateFailures |
| STO-K05 | Hooks read bounded data: opening reads no page, and bounded reads stop at their limit | internal/okfadapter:TestReadMaxRefusesAPageOverItsLimit, internal/okfadapter:TestReadHeadStopsAtTheMarker |
| STO-K06 | Writes that belong together land under one lock, and the commit stages under it too | internal/okfadapter:TestLockedHoldsTheLockAcrossWrites, internal/wikigit:TestBusyWhileAPageWriteHoldsTheWikiLock, internal/wikigit:TestWikiLockIsTheAdapterLock |
| STO-K07 | Locked and verified pages are found in every YAML shape, and a body that smuggles frontmatter is refused before the save | internal/okfadapter:TestIsLocked, internal/okfadapter:TestIsVerified, internal/okfadapter:TestSmugglesFrontmatter, internal/okfadapter:TestBodyDelimiterRejection |
| STO-K17 | Pages stay plain Markdown with small frontmatter that round-trips byte for byte | internal/okfadapter:TestMetadataGoldenRoundTrip, internal/okfadapter:TestFullFixtureWiki, internal/threadpage:TestPagesThroughOKF |
| OKF-K01 | One package owns the page codec, and nothing else parses or writes frontmatter (untested: that no other package parses frontmatter) | internal/spec:TestEachLibraryHasOneImporter |
| OKF-K02 | One wiki lock that the kernel frees when a process dies, and a way to hold it across writes | internal/okfadapter:TestLockedHoldsTheLockAcrossWrites, internal/okfadapter:TestBusyLockReportsErrLockBusy |
| OKF-K03 | Create never overwrites, and an update compares the raw bytes' hash | internal/okfadapter:TestCreateUpdateAndNoOp, internal/okfadapter:TestConcurrentCreateAndCAS |
| OKF-K04 | A save that would change only the timestamp is skipped | internal/okfadapter:TestCreateUpdateAndNoOp, internal/okfadapter:TestSaveWritesOnlyThePageAndTheLog |
| OKF-K05 | Temporary file, sync, rename and a sync of every folder touched come before any watermark moves | internal/okfadapter:TestSIGKILLDuringSaveExcludesTemporaryFiles |
| OKF-K06 | No link is followed on read or write, and the root must be a real folder with a regular index | internal/okfadapter:TestSymlinkAndPathProtections, internal/okfadapter:TestInitRefusesIndexSymlink, internal/okfadapter:TestOpenedWikiRefusesReplacedRoot |
| OKF-K07 | Hooks read bounded data only | internal/okfadapter:TestReadMaxRefusesAPageOverItsLimit, internal/okfadapter:TestReadHeadStopsAtTheMarker |
| OKF-K08 | A locked or verified page is never overwritten, and no agent write can add a verification | internal/okfadapter:TestIsLocked, internal/okfadapter:TestIsVerified, internal/knowledgepage:TestPagesEvaMdLeavesAlone, internal/update:TestProtectedPagesAreSkipped |
| OKF-K09 | Unknown frontmatter keys, verified and sources survive an update | internal/okfadapter:TestMetadataGoldenRoundTrip, internal/okfadapter:TestSerializeIsWhatCreateSaves |
| OKF-K10 | Pages save again byte for byte apart from generated, and no upgrade rewrites them | internal/okfadapter:TestMetadataGoldenRoundTrip, internal/okfadapter:TestFullFixtureWiki |
| OKF-K11 | Page ids with control characters, traversal, deep nesting or reserved names are refused | internal/okfadapter:TestCreateUpdateFailures, internal/okfadapter:TestSymlinkAndPathProtections |
| OKF-K12 | Every save carries a valid actor, host and model, with a safe fallback | internal/okfadapter:TestActorAndIndexLine |
| OKF-K13 | Model text is escaped before saving, so it cannot forge markup | internal/threadpage:TestEscape, internal/threadpage:TestEscaperCorpusThroughOKF, internal/threadpage:TestRenderEscapesFreeText |
| OKF-K14 | Index lines keep the OKF form, sorted and rebuilt, titles escaped | internal/okfadapter:TestActorAndIndexLine, internal/okfadapter:TestFullFixtureWiki |
| OKF-K15 | Folders 0700, other files 0600, and errors in eva-md's own words with the errno kept | internal/okfadapter:TestPermissions, internal/okfadapter:TestSaveFailureNamesNoTemporaryFile, internal/okfadapter:TestSaveFailureNamesNoLockFile, internal/okfadapter:TestOkfFailure, internal/okfadapter:TestUnremovableOkfLock |
| OKF-K16 | Pages stay OKF v0.2 conformant: frontmatter with a non-empty type | internal/okfadapter:TestFullFixtureWiki, internal/okfadapter:TestInitOpen |
| OKF-K17 | The tests of a kill during a save and of concurrent writers are kept | internal/okfadapter:TestSIGKILLDuringSaveExcludesTemporaryFiles, internal/okfadapter:TestConcurrentCreateAndCAS, internal/okfadapter:TestLockedHoldsTheLockAcrossWrites |

## Since the split

No change yet.
