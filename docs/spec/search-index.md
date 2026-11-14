# The search index

- **Component:** Search index
- **Paths:** `internal/searchindex`, `cmd/eva-md/index.go`
- **Status:** v1 as built, frozen on 2026-11-14

The blocks below are v1's design and verification text, moved here word for word when the two documents were split; [INDEX.md](INDEX.md) lists every spec.

<!-- v1 docs/design.md 030 -->
## Search index

Built in `internal/searchindex`.
Over years of history a scan of every file grows slow (1.7 to 2.9 s per GB for a rare word, and a common one is cut at the 5-second limit), so search reads a rebuildable index of the inbox beside its scan.
The index is a cache built from the inbox, never the truth: deleting it loses nothing, it holds the lines' lowercase words and where the lines are, never the lines, and every line it proposes is read again from the inbox before anything is shown.

- **What is indexed.** Inbox lines only: each prompt, reply, tool and plan line search could match, one that decodes, belongs to its file as the brief checks it (`inbox.Trusted`, which search's scan shares) and is not inside an off-the-record range.
  The index reads session files through `inbox.Walk`, which tracks off-the-record ranges in file order exactly as search and the brief track them; every search test but the two cut short by a deadline runs with and without the index and must find the same hits; past a "more" line, only hits ranked as the last one shown may differ.
  Events, host summaries and anything said off the record are never indexed.
  An unterminated last line is left, as a write may still be in progress, and a line over 2 MiB is passed over, as search passes it.
  Thread, system and knowledge pages are not indexed: they are rewritten in place, and they scan in tenths of a second.
- **Layout.** `~/.local/state/eva-md/cache/search/<project folder>/`, folders 0700 and files 0600: `manifest.json` (counts, names and offsets only; refused over 16 MiB), which says for each session file how far it is indexed (the byte offset just after a newline, the physical lines before it and the
  off-the-record state there) with its inode and generation; immutable segment files, `seg-<16 hex>.idx`; and the folder's `lock`.
  A segment holds, per line, its file, byte offset, length, line number, date, kind and length in tokens, and a dictionary of lowercase tokens with their postings.
  Its header, table and the sections loaded at open are checked against crc32s, and so is each dictionary block, list of postings and block of locations when it is read; a folder that fails any check, or names a segment that is missing or of another format version, is unusable, and search scans its files.
  A file found shrunk, with another inode, or gone gets its next generation, which makes its earlier lines dead, and is indexed again whole when it is there; so does a grown file whose byte before where the index stopped is no longer a newline, as a rewrite in place leaves it.
  A file rewritten in place to the same size is noticed only when its lines fail the check below.
- **Tokens and matching.** A token is a run of letters and numbers, lowered rune by rune as search lowers text, so ß stays ß.
  A run with camelCase or letter and digit boundaries also gives each of its parts when it has 8 or fewer: `ReadMax` gives `readmax`, `read` and `max`, and `gpt5` gives `gpt5`, `gpt` and `5`.
  A run over 64 bytes, such as a base64 blob, is dropped.
  A word of 3 or more characters matches each token it starts, at most the 256 such tokens in the most lines; a shorter one matches only an equal token, as search's own rule for short terms.
  A phrase needs all its tokens in a line, and its words next to each other on the line itself.
  So in indexed files a term is found as a whole token or at the start of a token or of one of its parts; a match inside a word, such as `a<b` in `data<base`, or in text without spaces is found by the scan alone, which runs over the indexed files too whenever the index finds nothing at all.
  A query with more than 32 tokens, or a term with no letter or digit or with a token over 64 bytes, is scanned as without an index.
- **A search.** Search opens the manifests and segments of the folders in scope and works out from its listing which files the index covers to their end (not scanned), in part (the tail scanned from where the index stopped, with its line count and off-the-record state there) or not at all (scanned whole).
  The `--days` window applies to each line's own date.
  It reads the K best candidates holding every term, K = max(200, 8 × `--limit`), again from their files: the byte before a line must end the previous one and the byte after it must be its newline, as in a file only ever appended to, and the line must decode and belong to its file; a line that fails is stale and skipped.
  Candidates past the K are counted with the hits, as "N more" when every term is a plain word of 3 or more letters and digits, which a candidate always holds as search matches it, otherwise as "about N more".
  Candidates holding only some terms are read when nothing holds every term.
  When a term is not a plain word (a phrase, a term with characters other than letters and digits, or one of 1 or 2 characters) and the candidates read again hold fewer hits than `--limit` while more remain, of every term or of some, their hits are kept and the files the index covers are scanned too,
  up to where the index stopped, skipping the lines already read again, since the tails are scanned anyway.
  When nothing at all matched, the files the index covers are scanned as without it, up to where the index stopped, so "No matches." keeps its meaning.
  A file covered in part whose byte before where the index stopped is no longer a newline, which only a rewrite in place leaves, is scanned whole and counted as one stale entry, until the next refresh indexes it whole under its next generation.
  A fault anywhere on the index's path turns it off for that search, which then scans every file.
- **Ranking.** Within search's tiers (terms matched, then exact case or the phrase, then page lines first) hits are ordered by BM25 before newest: for each token of each term, the inverse document frequency okf's search uses, ln(1 + (N − df + 0.5)/(df + 0.5)), times the token's frequency in the line, saturated with k1 = 1.2 and weighed against the line's length over the average with b = 0.75.
  N, df and the average length are pooled over the segments opened; a word's df sums its matching tokens' and is held to N.
  A tool line is weighed at the average length, so a short `Read <file>` line does not outrank the chats about the file.
  Page lines and scanned lines are scored with the same formula and statistics.
  There is no recency factor and no weight by kind: `--days` is the window, and identical lines still come newest first.
  With no segment opened every score is 0 and the order is as before.
  The label adds "then rarer and denser matches" before "then newest" only when hits were scored.
- **Keeping it current.** The daily `eva-md maintain` brings it up to date, even without a wiki; `eva-md index` does it now and `eva-md index --rebuild` builds it again from nothing.
  Either takes `<locks>/search-index.lock` without waiting, so a second one is busy, then each project folder's lock in turn: it checks every segment whole and builds a damaged folder again, indexes what the inbox holds beyond the index, oldest file first, 16 MiB a segment, merges segments when a
  folder has more than 8, more than 4 under 8 MB, or one holding any dead line, so the words of a session file deleted or replaced leave the segments in use at the next catch-up, and deletes a segment once no manifest has named it for 10 minutes, so a search that read the previous manifest still opens
  it; the segment those words were in is deleted by the first catch-up 10 minutes or more after; `eva-md inbox clear` instead deletes the index folder of each project it removes chats from, under that folder's lock, at once (see "Removal" under "The inbox").
  `eva-md search`, under its time limit, indexes for the next search up to 16 MiB of what the index lacks in its scope, the newest files first, once that is 4 MiB or more and 1.5 s of its time remain; below 4 MiB a scan of the tails costs under 10 ms.
  The MCP server's search never does, so it writes no file and takes no lock.
  A search's top-up skips a folder another writer holds; a catch-up waits up to about 5 seconds for it, then skips it.
  A segment is written to a temporary file, fsynced, renamed into place and the folder fsynced before the manifest is replaced atomically, so a crash leaves the previous manifest in force and no reader ever finds part of a segment.
  A segment is written only with twice its likely size plus 256 MB of disk free.
  The hooks never read or write the index, and the index adds nothing to the capture, brief or prompt paths.
  The first build of an existing inbox takes about 25 s per GB in the detached daily job; until then search scans as before.
- **Output.** Hit lines are unchanged.
  The trailer adds, when the index covered any file in scope, what it covered and when it was last written, such as "searched 23790 files (1071.7 MB) through the index of 2026-10-31 08:12"; a project folder whose index is unusable, or not built yet while others are; stale index entries skipped, with
  `eva-md index --rebuild` as the fix; the scan of the indexed files when the index found nothing ("the index found nothing, so the N files it covers (X MB) were scanned too") or when its best candidates held too few hits ("the index's best candidates held too few matches, so the N files it covers (X MB) were scanned too"); and what a search indexed for the next one.
  Without an index the output is as before.
- **Doctor.** A `search index` row read from the manifests and listings alone: PASS with the share of the inbox covered, the index's size and when and by what it was last written, such as
  `covers 1,073.2 of 1,075.0 MB of the inbox (99.8%) in 4 projects; 269.1 MB in 7 segments; built 2026-10-31 08:12 by maintain`; UNKNOWN before the first build; FAIL for a damaged folder, with `run eva-md index` as the fix.
  The doctor repairs nothing here, and `eva-md status`, which shows the row too, never writes.

<!-- v1 docs/verification.md 007 -->
- **Search index** (`internal/searchindex`, `internal/resume`, `internal/inbox`, `cmd/eva-md`, `internal/health`).
  The tokenizer (word and camelCase and digit parts, lowering as `strings.ToLower` lowers, runs over 64 bytes dropped); a segment read back exactly, every term, posting, date, length, kind and location, across dictionary and location blocks; every single byte changed in a segment caught by the open or
  a full check, never a panic, and a fuzz test of the reader; the walker yielding exactly the lines search can match, with physical line numbers and the off-the-record state carried across refreshes, an unterminated last line left and a killed writer's partial line numbered as `eva-md show` numbers
  it; refreshes that index a tail only and leave earlier segments byte for byte, budgets that take the newest files first, a busy folder skipped, and files deleted, shrunk or replaced getting their next generation; nothing off the record, no event, host summary or entry forged for another file in any
  segment's bytes or dictionary; merges equal to a fresh build and dropping dead generations, the merge policy, a segment holding any dead line merged, and a deleted session's words gone from the segments in use at the next catch-up and from the folder once pruned (`TestDeletedSessionLeavesNoWords`),
  pruning that keeps what a reader may still open; a damaged segment, a manifest of another version or over 16 MiB, a disk nearly full; BM25 scores computed by hand, the window on each line's date, prefixes from 3 characters, short terms whole, expansions capped at the 256 in the most lines, and the
  same score whether a line is scored from the index or from its text; 8 writers appending to shared session files in 10 steps, with a refresh after each while no writer writes, then every entry indexed once at its line.
  In search: every search test above but the two cut short by a deadline runs both without an index and with one built, and must find the same hits: past a "more" line only hits ranked as the last one shown may differ (`TestSameHitsCatchesAHitLeftOut`); the index's own output for words, phrases, short
  tokens, terms JSON escapes, mixed case, word starts and camelCase parts, each ref opened with `eva-md show`; the fallback scan for a match inside a word and for "No matches."; hits from the index and from a file's tail ranked together; the top-up at 4 MiB with 1.5 s left, and none at 1 MiB, with
  less time, with no time limit or without TopUp, as the MCP server searches, which writes nothing under the index folder, while `eva-md search` tops up (`TestSearchCommandTopsUpTheIndex`); a phrase held by more lines than the candidates read again, of every term and of some, including a some-list
  whose all-list holds partial matches, each hit shown once, with the covered files scanned up to where the index stopped (`TestSearchPhraseBeyondTheCandidatesRead`), the index's hits kept when the time limit cuts that scan, at a cut set by a count of the context's checks
  (`TestSearchKeepsTheIndexHitsWhenTheFillIsCut`), and the line now at a stale candidate's number found by it (`TestSearchFindsALineAtAStaleCandidatesNumber`); a file rewritten in place and grown, each line shown once, then indexed whole under its next generation at the next catch-up, with the lines
  added later numbered right (`TestSearchRewrittenFileShowsEachLineOnce`); deleted, replaced and stale files; a damaged folder scanned; "about N more" for a phrase and an exact count for words; BM25 order within the tiers, exact case still first; a deadline while candidates are read; page lines scored
  with the inbox's statistics; nothing off the record shown whether its marker is in a tail or behind the index; a fault on the index's path falling back to the scan, also one after the candidates of every term matched only some of the terms (`TestSearchRecoversFromAnIndexPanic`, `TestSearchRecoversFromAFaultAfterPartialMatches`); `--all` with a folder not indexed yet.
  The command line: `eva-md index` and its lines, `--rebuild`, `--json`, busy and a failure; maintain without a wiki running the index step; the doctor and status rows before, after and with a segment gone, which status never writes; the hooks starting the daily maintain without a wiki, but never a push retry.
  1,000 session files (about 50 MB) are indexed in under 1 s on an Apple silicon Mac, into an index under 35% of the inbox's size, after which a search with no window takes about 5 ms through the index against about 0.1 s scanning (`TestSmokeThousandSessions`, `TestSearchSmokeThousandSessions`; the times are logged, not bounded).

<!-- /v1 -->

## Since the split

No change yet.
