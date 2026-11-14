# System pages

- **Component:** System pages
- **Paths:** `internal/systempage`, `cmd/eva-md/system.go`, `internal/hook/pointers.go`
- **Status:** v1 as built, frozen on 2026-11-14

The blocks below are v1's design and verification text, moved here word for word when the two documents were split; [INDEX.md](INDEX.md) lists every spec.

<!-- v1 docs/design.md 004 -->
## System page

One page per system (a service, a repo, a platform or a major documentation area), `systems/<slug>.md`, shared by every project in the wiki.
An agent writes it, usually through the digest skill (see Packaging), with `eva-md system write <slug> [--replace] < page.md`.
The slug is 2 to 64 characters of `[a-z0-9-]` starting with a letter; `map`, `index`, `log` and `agents` are reserved.
The write checks the page line by line and saves it through the adapter with the map, the folder index and the names index, under one hold of the wiki lock (`internal/systempage`).

- **Frontmatter:** `type: system`, `title` (at most 80 characters), `description` (at most 160), `generated: { by: <host>/<model> }`, `status: stable`, `sources` (at least one), `as_of` (YYYY-MM-DD, no later than tomorrow, which leaves room for time zones), `state: active`, and optionally `repos`
  (git remotes or project keys, stored as project keys), `aliases` (at most 10, of 3 to 60 characters) and `stale_after` (YYYY-MM-DD, the day to review a page built only from documentation, which eva-md can't check; it may be in the future, and okf saves it before the sources).
  Unknown or repeated keys are refused, and quoted values lose their outer spaces.
  The write saves `repos` and `aliases` as flow lists (`aliases: ["order service"]`), also with `--replace` over a key a person rewrote as a block list or a mapping.
  A title, description or alias may not hold ` · `, `](` or a speaker label.
- **Sources:** an `https://` URL without credentials, optionally with its version (a query parameter, or ` v<n>` after the URL), or `<repo>@<commit>[:<path>]` with a commit of 7 to 40 hex digits and a relative path to a file or a folder, a folder's trailing `/` dropped and a path refused when it has
  an empty, `.` or `..` part (`./x`, `x/./y`, `x/.` and `.` included) or holds `](`, a space, a control or format character, or bytes that are not UTF-8.
  System status and `eva-md show` print such a character in a path, a clone or a branch, as a status file an older eva-md wrote may hold, as `?`; a plain space prints as it is.
  A page cites at most 16 code sources, refused with "cite the folders a page describes rather than every file", and any number of documents; it may cite several commits of one repo.
  A URL is refused when a query parameter's name, or a part of it split on `-`, `_` or `.`, is sig, signature, token, key, apikey, secret, password, pwd or auth.
- **Body:** `## Summary` (required, at most 800 characters), then optionally, in this order, `## Responsibilities`, `## Interfaces`, `## Depends on`, `## Used by`, `## Decisions and constraints`, `## Open questions` and `## Unverified`, for what no source shows.
  `###` headings may sit inside them; no other heading.
  A page is at most 24 KB as written; the code pass, system status, maintain and `eva-md show` read a saved page up to 25 KB (`systempage.MaxSaved`), for the lines the save adds, so a page written at its cap is measured.
  Links go to `https://` pages or to system pages that exist.
  Code ends the page with `Part of the [system map](map.md).`
- **Map:** `systems/map.md`, built by code, links every page, one line each, sorted by slug: `- [<title>](<slug>.md): <description> · repos: <key>, <key> · code: <key>@<commit>[:<path>], <key>@<commit>[:<path>] · as of <YYYY-MM-DD> · review by <YYYY-MM-DD>`, each field after the description only when it has a value.
  `code:` lists the page's code sources, each once, in its order, with the repo as its project key; `review by` is its `stale_after`.
  Its repos are the page's own and then the repos of its code sources, so a page that cites a project's code is that project's in the brief, and `code:` never directly follows the description: an eva-md from before code refs, which takes the description to end at the first field it knows, reads the line and skips the fields it doesn't.
  A ` · ` in a hand-edited value is shown as ` - `.
  The folder index, `systems/index.md`, lists the pages and the map, one line each by file name, `* [<title>](<slug>.md) - <description>`; the brief, the prompt's reminder, `eva_status`, `eva_show`'s description and the recall skill point readers at it, since its line says what a system is without the
  repos and every code source a map line adds (30 systems citing 6 sources each take about 4.7 KB there and 23 KB on the map), while the brief, the prompt hook, the code status and the digest skill read the map for its repos and code.
  A write saves the folder index before the map, which decides whether anything points at it, so a write stopped between the two never leaves a map without its index.
  The root index gains `* [Systems](systems/index.md) - The systems the projects share` at the first write.
- **No repos.** A page that lists no repo and cites no code has a map line that names no project, so no brief and no `eva_status` points at it; search still finds it, and a prompt that names it still gets a pointer.
  The write saves it, and prints after the `wrote` line, for information only: `no repos: no project's brief or eva_status points at this page, which lists no repo and cites no code; to have them point at it, list the repos that build or use the system under repos and write it again with --replace`.
- **Never written.** The page is refused, with each problem named by its line and never its text (at most 20), when it exists and `--replace` is not given, changed since it was read, is human-verified, is locked (`locked: true`, `yes` or `on`, in any case) or can't be read, or the map can't be read.
  Its content is refused for a carriage return or another control or format character, a comment or fence, a `[REDACTED:` marker, a heading out of the list or order, a link to a system with no page yet, a bad key or source, or a line the daily commit's secret scan flags, checked in the page, in the
  frontmatter as it will be saved (named by the key's line) and in its map and index lines, or a code source whose path the cited commit lacks in a local clone (see "Code a system page cites").
  The exists, verified or locked reason comes in the same refusal as the content problems.
- **Names index.** Every write rebuilds `.eva-md/names.tsv` under the same lock, writing it only when it changes: `<name>`, the page id and the title, tab-separated, one name a line, sorted by name.
  The names are each page's title, slug and aliases, with control characters as spaces, runs of spaces collapsed, punctuation trimmed from the ends and the case kept.
  At most 2,000 names and 128 KB: past them the aliases of the pages with the oldest `as_of` go first (a page without one counts as oldest), and only then those pages' other names.
  The doctor writes it again when it is missing or out of date.
- **Repos index.** Every write also rebuilds `.eva-md/repos.tsv` under the same lock, writing it only when it changes: the header `# eva-md repos index 1`, then one line for each page that lists two or more repos, its own and those of its code sources as on the map, each once: the page id and the project keys, tab-separated, sorted by id.
  A page with one repo makes no project another's sibling and has no line, and with no such page there is no file: the write that leaves none removes it, and the doctor removes an empty one.
  At most 64 KB: past it the pages with the oldest `as_of` are left out (a page without one counts as oldest).
  Prompt recall reads it to learn which projects share a system page with the chat's (see "Per prompt").
  The doctor writes it again, with the names index, when it is missing or out of date, and its system names row covers both, naming the index at fault.

<!-- v1 docs/verification.md 022 -->
- **System pages** (`internal/systempage`, `internal/resume`, `cmd/eva-md`, `internal/okfadapter`, `internal/mcp`).
  A page written, replaced and refused for each reason, in one refusal naming lines only: an existing page without `--replace`, a page changed meanwhile, verified or locked, a carriage return, a ` · ` in a value, a link to a system with no page, a bad source (credential parameters included) and a
  secret in the page, in its frontmatter as saved or in its map line; the map, the folder and root index lines, and okf validation after each write; the daily commit committing pages Write made.
  The brief's pointers on both hosts at startup, clear and compact, at most 3 lines and 600 characters with the command never cut, then the line naming the system index, none on resume, fork or refresh, and fenced pointers in a first chat; unusable maps (missing, unreadable, linked, over 256 KB) giving none.
  The pages that cite the project's code come first, then its other pages, each in the map's order, and `eva_status` lists them in that order; near the brief's deadline, with no time for a count, they are ranked the same and no git runs (`TestBriefRanksSystemsCitingTheProjectsCode`).
  A page that lists no repo, as none or an empty list, and cites no code is written with a note that no project's brief or `eva_status` points at it, and its map line names no repo; a page whose code names a repo gets no such note, nor the page written again with its repos
  (`TestWriteNotesAPageNoRepoLists`); `eva-md system write` prints it after the `wrote` line and exits 0 (`TestSystemWriteNotesAPageNoRepoLists`).
  Search finds system lines with their tags, an old page still found, every scope but `--inbox`, and the label and trailer; a CLI test runs search and opens each printed ref with `eva-md show`.
  `cleanPage` keeps identifiers but redacts a value made suspect across lines (`TestCleanPage`), and a page shown, written back with `--replace` and shown again is unchanged.
  A person's block list or mapping for `aliases` or `repos` is written back with `--replace` as a flow list, with the alias in the names index and the repo on the map (`TestReplaceRewritesABlockListAsAList`).
  The names index: its content, its bounds dropping the oldest aliases first, rebuilt when missing, damaged or stale, and staged by the daily commit; once the last page is removed by hand, reported by status as one the doctor removes rather than writes again, which the doctor does (`TestRefreshNames`).
  The folder index written before the map, so it lists the page by the time the map does (`TestIndexBeforeTheMap`).
  The repos index: a line for each page listing two or more repos, its code sources' among them, each once, sorted by id, no line for a page with one and no file without such a page, and staged by the daily commit (`TestReposIndex`); removed, never left empty, when the last page listing two repos is
  written again with one, the removal committed, and an empty one an earlier write left reported by status as one the doctor removes, and removed by the doctor (`TestReposIndexGoesWithTheLastSharedPage`); `RemoveFile` removing a file that is not a page under the wiki lock, taking none of its own
  inside `Locked`, giving up as busy while another write holds the lock, taking a file already gone as removed and refusing a page, a path out of the wiki or through a link, a link and a folder (`TestRemoveFile`, `TestLockedHoldsTheLockAcrossWrites`, `TestBusyLockReportsErrLockBusy`); written again by
  the doctor, with the names index, when missing, damaged or stale after a hand edit, and reported stale by status, which leaves it (`TestRefreshNamesWritesTheReposIndex`); past 64 KB the oldest pages left out, one with no `as_of` first (`TestReposBounds`); the strict read, with its header, skipping any other line, and the siblings it gives (`TestParseRepos`, `TestReposIndex`).
  Backlinks: `eva-md show` and `eva_show` name the system pages that link to a page, sorted and capped at 12, never through the map or a link in a code span, and nothing for a page no page links to (`TestLinksReadAsMarkdown`, `TestLinkedFromNamesTheLinkingPages`, `TestShowNamesTheLinkingPages`, `TestShowPagesAndSections`).

<!-- v1 docs/verification.md 023 -->
- **Prompt pointers** (`internal/hook`).
  Stoplisted words, words under 5 characters and parts of words never match; distinctive names match in any case and plain ones only in exact case; names of several words; at most 3 a prompt, 12 a chat, each page once, never a page the brief named, and at most 160 characters each, with the secret
  fallback; nothing with no wiki or index, another instance's wiki or a removed page, and a damaged index line skipped; the reminder on prompts 1, 6 and 11, its forms, which name the system index, also in a default install's path for user names of 5 to 9 characters (`TestReminderForms`), and
  `reminder_every` 0 and 1; the state never written through a symlink or a hard link; only the head and tail of a long prompt matched; the marker saved before the state, and a startup or clear brief resetting it.

<!-- /v1 -->

## Since the split

No background step writes a system page: neither the update nor maintain does. Only `eva-md system write` writes one (`cmd/eva-md/system.go`).
