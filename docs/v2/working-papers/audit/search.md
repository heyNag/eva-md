# eva-md audit: search, show and the search index

Scope read in full (non-test): `internal/resume/search.go` (1,088 lines), `internal/resume/search_index.go` (621), `internal/resume/show.go` (556), `internal/searchindex/*` (3,200 lines non-test: doc, token, score, query, build, refresh, merge, manifest, segment, catchup, hold, status), `internal/inbox/walk.go` (177), `cmd/eva-md/index.go` (137). Also read because search runs through them: `internal/resume/pages.go`, the search half of `internal/resume/knowledge.go`, `cmd/eva-md/commands.go` (runSearch, runShow), `internal/mcp/calls.go` and `tools.go`, `internal/hook/pointers.go` (the reminder), `internal/knowledgepage/recall.go` (the other tokenizer), `internal/install/templates/recall-skill.md`. Design read: docs/design.md 1035-1047 and 1076-1088; docs/known-limitations.md "Search" (lines 70-96). Tests were read for what they cover, not run. Nothing in the repository was changed.

Paths below are relative to `the repository`.

---

## 1. How it works

### 1.1 Who calls it, and when

- Search is never automatic. `internal/resume/search.go:31-33`: "The agent starts a search when it needs an earlier chat; no hook ever does."
- Two front ends call the same function, `resume.Search`:
  - CLI `eva-md search` (`cmd/eva-md/commands.go:166-210`), under `resumeTimeout = 5 s` (`commands.go:32,48`), with `TopUp: true` (`commands.go:203`). Defaults: `--days 90` (`commands.go:173`), `--limit 10`, range 1-50 (`commands.go:174,185`). In a folder the config excludes it prints nothing and exits 0 (`commands.go:200-202`).
  - MCP tool `eva_search` (`internal/mcp/calls.go:216-256`), 5 s deadline (`internal/mcp/tools.go:21`), no top-up (`calls.go:248`), result cut to 24 KiB keeping the trailer (`tools.go:15`, `calls.go:260-270`). It has no `--pages` / `--inbox` switch (`tools.go:85-90`).
- The agent learns search exists from: the recall skill (`internal/install/templates/recall-skill.md`), the MCP guide ("on a miss search again with other words, about 5 lookups at most", `internal/mcp/instructions.go:18`), and a one-line reminder the prompt hook adds on a chat's first prompt and then every 5 prompts when nothing else rode along (`internal/hook/pointers.go:204-234`, `internal/config/config.go:38`).
- The prompt hook's own automatic recall reads only the knowledge recall index (`internal/hook/recall.go:105`), never search or the search index.

### 1.2 One search, step by step (`search.go:295-367`)

1. Terms: each argument is a term; whitespace collapsed; duplicates dropped; a term under 3 runes matches whole tokens only; a term with `/` and no space is also a path (`search.go:392-415`, `79-88`).
2. Scope: the project's inbox folder plus the general area, or every folder with `--all` (`search.go:314-322`).
3. The search index is opened first, because its statistics also score page lines (`search.go:331-333`, `search_index.go:105-139`).
4. Wiki pages are scanned first (`search.go:335-337`, `pages.go:41-105`): thread pages of the project folder (found through its now page, `pages.go:66-77`) and `general`, plus every system page and every knowledge page in every scope. A thread page is streamed line by line through `threadpage.Scanner` and each Now item or Timeline line is one unit (`pages.go:175-299`). A knowledge page is read whole, up to 256 KiB, and gives two units: `<kind> [tag]: <title>: <statement>` and a `code:` line (`knowledge.go:187-242`). If the deadline ends among the pages the inbox is not searched at all (`pages.go:100-103`, `search.go:339`).
5. Inbox: `list` reads every project folder of the inbox, counts every session file by its first 8 id characters (for ref ambiguity), and keeps the in-scope files changed inside the window, newest first (`search.go:429-486`).
6. Index part (`search_index.go:166-212`): `classify` sorts files into covered to the end, covered in part (tail scanned from the indexed offset, with line count and off-record state), or not covered (`search_index.go:221-277`). The index returns the K = max(200, 8 x limit) best candidates holding every term and K holding some (`searchindex/doc.go:63`, `query.go:257-340`). Each candidate line is read again by offset, checked (newline before, newline after, decodes, `inbox.Trusted`, searchable kind, project, window) and matched by the same `match` as a scanned line (`search_index.go:311-388`).
7. Scan part: what the index does not cover is streamed (`search.go:506-539`). Before JSON is decoded a byte prefilter requires every filterable term in the lowered raw line; marker lines always pass (`search.go:585-641`).
8. Reconciliation (`search_index.go:441-472`): if nothing held every term, the "some" candidates are verified; if a non-plain query got fewer hits than the limit while more candidates remain ("fill"), or nothing matched at all, the covered files are scanned too, skipping lines already verified.
9. Top-up (`search_index.go:521-560`): a CLI search with at least 1.5 s left and at least 4 MiB unindexed in scope indexes up to 16 MiB, newest files first, until deadline minus 250 ms, before it prints.
10. Output (`search.go:861-1060`): a label line of facts, one line per hit (`[role] time · [project] · branch · ref · snippet`, snippet at most 200 characters, `search.go:928-995`), "N more" or "about N more", and a trailer of what was scanned.

### 1.3 Matching and ranking

- A unit is one inbox entry (one JSONL line: a prompt, reply, tool line or plan) or one page line. Every term must be in the same unit; only when no unit anywhere holds all terms are units holding any term shown (`search.go:675-725`, `854-859`).
- Scan semantics: case-insensitive substring for terms of 3+ runes, whole token for shorter ones (`search.go:729-751`). Index semantics: token or token prefix (3+ characters), camelCase and digit parts, at most 256 expansions per word (`searchindex/token.go:25-63`, `score.go:142-147`, `doc.go:43`).
- Order (`search.go:808-824`): terms matched, then a hit in the case typed or the whole phrase, then page lines before chat text, then BM25 score, then newest, then file and line.
- BM25: k1 1.2, b 0.75, statistics pooled over the opened segments; tool lines weighed at average length (`score.go:8-12,91-128`, `query.go:296-301`). Page lines and tail lines are scored with the inbox's statistics (`search_index.go:152-160`).

### 1.4 The index (`internal/searchindex`)

- Location: `<state>/cache/search/<project folder>/` with `manifest.json`, immutable `seg-<16 hex>.idx` files and a `lock` (`doc.go:16-21`, `manifest.go:20-63`).
- Contents: tokens, postings, and per document its file, offset, length, line number, timestamp, token count and kind. Never the text (`doc.go:1-6`, `segment.go:26-53`).
- Only on-record prompt, reply, tool and plan lines that pass `inbox.Trusted` are indexed, through `inbox.Walk` (`inbox/walk.go:49-90`, `refresh.go:244-250`). Pages are not indexed.
- Built by `eva-md index`, by the daily `maintain` as its last step, including capture-only installs (`cmd/eva-md/index.go:25-83`, `cmd/eva-md/maintain.go:78,105`), and by a CLI search's top-up. `Catchup` takes `<locks>/search-index.lock` without waiting, then each folder's lock (waiting up to 25 x 200 ms), verifies every segment whole, refreshes, merges, prunes (`catchup.go:17-129`).
- Caps and limits: token 64 bytes, 8 parts per run, 32 query tokens, 256 expansions (1,024 per segment before the cut), 16 MiB of inbox per segment, top-up 16 MiB once 4 MiB is missing and 1.5 s remain, retired segments kept 10 minutes, manifest at most 16 MiB (`doc.go:29-59`, `token.go:10`, `query.go:212`). Merge when more than 8 segments, more than 4 under 8 MB, or any dead document (`merge.go:15-60`). A segment is written only with twice its estimated size plus 256 MB free (`build.go:180-193`).
- Change detection: per file `To`, `Lines`, `Off`, `Ino`, `Gen` (`manifest.go:43-49`); a file shrunk, replaced or gone gets its next generation, which makes its earlier documents dead (`refresh.go:45-68`); a grown file whose byte before the indexed offset is no longer a newline is indexed whole again (`refresh.go:222-228`).
- Durability: temp file, fsync, rename, folder fsync, then the manifest replaced atomically; every read checked against a crc32 (`segment.go:187-387`, `443-533`, `manifest.go:118-127`).

### 1.5 Show (`show.go`)

- `s:<session or first 8 chars>#<line>`: lists every project folder, opens each session file whose name starts with the id, reads physical line N, checks that the entry belongs to that file, and prints that one entry, cleaned and indented (`show.go:439-524`, `read.go:703-711`). Two files matching is an error. A removed chat gets its own message (`show.go:514-517`).
- `<page>[#section]`: a whole page up to 2 MiB (`show.go:95-100`, `read.go:676-694`), or one section streamed: `#Now`, `#Timeline` (newest 10 date groups, 16 KiB cap, `--last` / `--since`), `#<YYYY-MM-DD>`, or any heading by text or anchor (`show.go:114-437`). Section ends follow the thread page model's fence tracking (`show.go:167-197`).
- The MCP front end caps show at 24 KiB under a label that says the text is data (`mcp/calls.go:274-314`, `tools.go:16`). The CLI has no cap beyond the 2 MiB page limit.

### 1.6 What it writes where

- Search writes nothing except a CLI top-up: a segment and a manifest under the index folder, under the folder's lock (`search_index.go:551`). The MCP search writes no file and takes no lock.
- `eva-md index` and `maintain` write the same folders. `eva-md inbox clear` holds the folders' index locks and deletes each cleared project's index folder at once (`searchindex/hold.go:28-81`, `cmd/eva-md/inbox.go:304`).
- No run-log entry, counter or telemetry is written for a search (the only run log writer is `internal/update/runlog.go`).

---

## 2. What must be kept

1. **Nothing is shown from the cache.** Every candidate line is read again from the inbox and must pass: newline before, newline after, JSON decodes, `inbox.Trusted`, searchable kind, project key, window (`search_index.go:311-388`). The index holds no text (`doc.go:1-6`).
2. **Off the record is never indexed, printed or counted.** State is tracked in file order, identically in `inbox.Walk` (`walk.go:77-87`) and the scan (`search.go:594-602`); marker lines always pass the prefilter (`search.go:60,587`); the indexed position carries the state for the tail (`manifest.go:43-49`, `search_index.go:263`). Tested by `TestOffRecordAndUntrustedNeverIndexed`, `TestSearchSkipsOffTheRecordRanges`, `TestSearchPrivacy`.
3. **An entry must belong to the file it is read from** (host, session, project folder, no control or format characters in the id): `inbox.Trusted` (`walk.go:127-144`). This stops a planted line from posing as another session or project.
4. **Files are opened through a pinned root** that refuses symlinks, hard links, non-regular files and a swapped inode (`read.go:33-68`, `walk.go:163-177`; segments and manifest with `O_NOFOLLOW`, `segment.go:447`, `manifest.go:78`).
5. **Output cannot be forged or steer the agent.** One line per hit; snippet cleaned (redaction applied again at display time) and escaped so stored text cannot start a line or pose as a speaker header (`search.go:923-995`); identifiers cleaned with `redact.Identifier`, not as free text (`search.go:963-966`); the label states what [user], [assistant] and page tags mean and that suggestions are not decisions (`search.go:48-53`); MCP show is labelled as data. Tested by `TestSearchSnippetCannotStartALine`.
6. **The prefilter can never hide a match**: a term JSON may escape is not used as a filter (`search.go:417-423`, `627-641`). Tested by `TestSearchFindsTermsThatJSONEscapes`.
7. **"No matches." means no match**: the covered files are scanned when the index finds nothing (`search_index.go:465-470`).
8. **A fault in the index never takes a search down**: `guard` recovers, the pre-index state is restored, every file is scanned (`search_index.go:92-100`, `166-178`, `446-464`); a damaged folder is scanned and named (`query.go:47-74`, `search_index.go:578-580`). Tested by `TestSearchRecoversFromAnIndexPanic`, `TestSearchDamagedFolderScans`, `TestSegmentByteFlips`, `FuzzSegmentReader`.
9. **Crash-safe cache writes**: temp, fsync, rename, folder fsync, then atomic manifest; the manifest advances only after the segment is in place (`refresh.go:134-199`); retired segments are kept 10 minutes for lock-free readers (`manifest.go:157-208`); free-disk check before writing (`build.go:180-193`).
10. **Honest reporting**: "about N more" when counts include unverified candidates (`search_index.go:421-429`); "cut short" when the deadline ended the scan; unreadable files, pages and folders are named, never left to look like no hits (`search.go:1001-1060`).
11. **Bounded time and memory**: streaming, only the best `limit` hits kept (`search.go:826-842`), 2 MiB line cap, unterminated last line left alone because a write may be in progress (`search.go:541-581`, `walk.go:92-120`), refresh in 16 MiB blocks (`doc.go:46`).
12. **Removal is immediate in the index**: `inbox clear` drops the folder's index under its lock (`hold.go`). Tested by `TestHoldDropsAFoldersIndexWhole`, `TestDeletedSessionLeavesNoWords`.
13. **Hooks never touch the index**, and the MCP search writes nothing and takes no lock (`search_index.go:515-520`).
14. **Scope and proof**: project plus general by default; the wiki's project folder is accepted only when its now page proves the project's remote (`read.go:608-635`).
15. **Ranking details worth keeping**: tool lines weighed at average length so `Read <file>` lines do not outrank the chat about the file (`query.go:298-300`); a whole-repo citation is never an exact path hit, so the page citing the very folder ranks first (`search.go:159-190`); Now items are windowed by the page's `as_of` so items in force stay findable (`pages.go:256-299`).
16. **Path lookup**: a path term finds the system and knowledge pages that cite the file or a folder holding it, with folder bounds (`internal/order` is not in `internal/orders`) (`search.go:131-197`).
17. **Identifier-aware tokens**: camelCase and letter/digit parts (`token.go:96-108`).
18. **Show's bounds**: section ends as the page model ends them (`show.go:167-197`), 16 KiB Timeline cap with a count of lines left, 2 MiB page cap, full session id printed where an 8-character ref would be ambiguous (`search.go:937-943`).
19. **The test discipline**: every search test runs with and without the index and compares hits (`search_test.go:25-47`); no test depends on machine speed.

---

## 3. Where it falls short of the goal

Ranked by consequence. "Known" means docs/known-limitations.md already lists it.

### F1. Retrieval of earlier chats is never automatic, and one recall costs several tool calls
Lens: invisibility. Severity: critical. Verified in code. Known: no.

- No hook searches (`search.go:31-33`). The prompt hook recalls knowledge rows only (`hook/recall.go:105`). Earlier chats and thread Timeline lines reach a chat only when the agent decides to call a tool, prompted by a reminder line every 5 prompts (`hook/pointers.go:204-234`).
- A recall is one search plus one show per hit: hits are 200-character snippets (`search.go:968-995`) and show returns exactly one entry with no neighbours (`show.go:496,511`). The MCP guide budgets "about 5 lookups" (`mcp/instructions.go:18`).
- Cost to the goal: anything not yet promoted to a knowledge page is remembered only if the agent thinks to look, and each look spends tool calls, tokens, time, and possibly a host permission prompt (whether the first call asks is still a pending live check, docs/verification.md:160).
- Direction: one retrieval core that the prompt path can query inside its latency budget across every tier (knowledge, thread items, chat exchanges), returning a few evidence-bearing lines with a precision floor. Explicit search stays as the deep path and returns passages that already carry their context. This reopens the rule that hooks never read the index, so the index must first be cheap to open (see F10). Action: add.

### F2. The unit of retrieval is a single line, and every term must be in it
Lens: recall-quality. Severity: high. Verified in code. Known: no (the design states the rule; the limitations do not list its cost).

- `match` judges one unit: one JSONL entry or one page line (`search.go:675-725`). The index's document is the same line (`build.go:48`, `walk.go:83-86`).
- A question in a prompt and its answer in the reply never match together. A two-word query whose words sit in adjacent Timeline lines falls to the "any term" tier, which is ordered by terms matched and is noisy.
- Hits are not grouped: `ranking.slot` and `insert` keep the best lines whatever their session (`search.go:826-842`), so one chat that repeats a word can fill all 10 places.
- Nothing marks an older statement that a later chat reversed; the label only says assistant lines are not decisions.
- Direction: index exchanges (a prompt, its reply, the salient tool lines) and page items as documents with fields; match across the exchange; group by session or thread; return the best passage with its neighbours and its ref range. Action: rebuild.

### F3. Matching is purely lexical on exact wording
Lens: recall-quality. Severity: high. Verified in code. Known: no.

- Scan: substring (`search.go:729-743`). Index: token or prefix (`score.go:142-147`). No stemming ("retry" does not find "retries"), no alias or synonym expansion, no similarity. The only relaxation is all terms to any term.
- The tool description itself tells the agent to retry with other words (`mcp/tools.go:84`).
- Direction: one normaliser used on both sides (light stemming, identifier splitting, hyphen and underscore variants), alias expansion from the system pages' names, and a measured decision on a semantic re-rank (see the dependency question in section 5). Measure on a labelled set first (F6). Action: add.

### F4. A 90-day default window hides older memory, and nothing widens on a miss
Lens: recall-quality. Severity: high. Verified in code. Known: no.

- Default `--days 90` on the CLI (`commands.go:173`) and the MCP tool (`mcp/calls.go:229`). Files last changed before the window are not listed (`search.go:473`), entries dated before it are dropped (`search.go:612`, `query.go:280`), and thread page lines too (`pages.go:281-289`). Knowledge and system pages are exempt.
- On a miss the output is "No matches." and a trailer that says "last 90 days" (`search.go:1002-1005`). Search widens terms (all to any) but never the window or the scope.
- Cost: a chat from four months ago looks as if it never happened unless the agent retries with `days=0`. With an index the window no longer buys speed.
- Direction: no hard default window; recency as a ranking prior; staged widening inside one call (project, then sibling projects, then all; recent, then all dates) with the stage named in the result. Action: evolve.

### F5. Two matchers with different meanings, kept consistent by a reconciliation layer
Lens: architecture. Severity: high. Verified in code. Known: yes for the symptoms (lines 73-76), not for the cause.

- The scan matches substrings; the index matches tokens and prefixes. `search_index.go` (621 lines) exists to reconcile them: verify, the "fill" scan, estimated counts, the nothing-found scan, the snapshot and restore, the per-file map of lines already read (`search_index.go:180-212`, `413-472`, `82-88`, `362-368`).
- The hit set depends on what the cache covers. "base" finds "database" in today's unindexed tail, and stops finding the same line once the daily job indexes it, whenever any other hit exists (`searchindex/doc.go:7-10`). The rule "the index is a cache, never the truth" holds for a hit's text, not for which hits appear.
- The index pre-selects K candidates by BM25, then the ladder in `better` re-orders them by other keys, so a better-tier hit past K is lost (`doc.go:63`, `search.go:808-824`).
- Direction: define one match contract (tokens, with an explicit contains mode if substring matters), and use the same tokenizer and scorer for indexed documents and for unindexed tails. The proposer and judge split, and most of `search_index.go`, then go away. Action: rebuild.

### F6. Nothing measures whether search finds the right thing, or how it is used
Lens: evaluation. Severity: high. Verified in code. Known: no (line 78 lists only the unmeasured index size and build time).

- Tests check that index and scan agree on fixtures (`search_test.go:25-47`) and the safety properties. There is no labelled set of queries with expected evidence, no recall@k or rank measure, in contrast to knowledge recall, which has `TestRecallPrecision`.
- The benchmark plants one word and asserts only p95 under 5,000 ms for `eva_search`; the before and after index figures are "figures only" (`scripts/benchmark.py:91,202-220,387-388`; docs/verification.md:112).
- No counter records searches, misses, any-term fallbacks, cut-short scans, fill scans or latency (no run-log writer outside `internal/update/runlog.go`). The ranking ladder, K = 200, the 256-expansion cap and BM25 parameters were never tuned against data.
- Direction: a retrieval evaluation harness (scripted chats, queries, expected evidence; run for explicit search and for the prompt path), and counts-only local counters shown by the doctor. Action: add.

### F7. Source refs are positional and local to one machine
Lens: reliability-safety. Severity: high. Verified in code. Known: yes in part (lines 92, 101, 108).

- A ref is `s:<first 8 id characters>#<physical line>` (`search.go:937-943`). It names neither host, project nor content.
- To resolve one, show lists every project folder and every file (`show.go:455-513`). To avoid ambiguity, every search lists every session file in the whole inbox, whatever its scope (`search.go:441-466`).
- After `inbox clear` and a re-capture under the same session id, a ref resolves to whatever line now has that number (known, line 101). A ref written into a wiki page on another Mac ends in "source not found" (`show.go:518`), with nothing to say the chat lives elsewhere.
- Cost: the evidence trail behind wiki statements can point at other text or at nothing, which weakens trust in the memory across machines and for a team.
- Direction: stable entry identity (host, session, sequence, content hash) resolved through a session-to-file map; pages carry a short redacted excerpt or the hash so a ref can be verified, or reported as "captured on another machine". Action: rebuild.

### F8. Ranking is a fixed ladder with relevance in fourth place
Lens: recall-quality. Severity: medium. Verified in code. Known: in part (lines 75, 79 list two consequences).

- Order: terms matched, typed case or phrase, page before chat, BM25, newest (`search.go:808-824`). A hit that happens to match the typed capitals outranks any more relevant hit; any page line outranks any chat line in its tier.
- No recency prior, and no weight for a user's statement against an assistant's suggestion or a tool line beyond the length rule (design 1084 says so). Page lines are scored with the inbox's statistics (`search_index.go:152-160`).
- Direction: one scored model with field, kind, tier and recency weights tuned on the evaluation set, with deterministic tie-breaks. Action: rebuild, together with F2.

### F9. Four lexical engines, and pages are re-read on every search
Lens: architecture. Severity: medium. Verified in code. Known: no.

- Separate tokenizers and scorers: search's substring scan (`search.go:729`), the search index (`searchindex/token.go`, `score.go`), knowledge recall with its own word splitter, stoplist and IDF floor (`knowledgepage/recall.go:27-37,175-234`), and system pointers (`hook/pointers.go:451`). The same prompt can match in one and miss in another.
- Pages are not indexed. Each search lists and reads every thread page in scope, every system page and every knowledge page whole (`pages.go:78-104`, `knowledge.go:194`), before the inbox, under the same 5 s. Thread summaries and "Last chat" are not searched (known, line 84), nor a knowledge page's History.
- `search.go` multiplexes five source kinds and path semantics through one `match` and a `scan` struct of about 30 fields (`search.go:261-281`, `675-725`); the frontmatter is hand-parsed to avoid loading the wiki through okf (`pages.go:241-254`).
- Direction: one retrieval core (tokenizer, index, scorer) over all memory tiers, with a policy per surface: a high precision floor for the prompt path, recall for explicit search. Path lookup becomes a field query on cited code. Action: rebuild.

### F10. Index upkeep leaks into the agent's wait, and opening the index costs O(corpus)
Lens: invisibility. Severity: medium. Verified in code (timings inferred). Known: in part (lines 77, 78).

- A CLI search indexes up to 16 MiB before it prints, until deadline minus 250 ms (`search_index.go:521-560`, `topUpMargin` at line 32). The agent waits for maintenance.
- Otherwise freshness depends on the daily job; the MCP search never tops up; after `inbox clear` a project has no index until then.
- Every search re-opens everything: each manifest whole (one entry per session file, `manifest.go:77-115`), each segment's sources and docmeta whole (7 bytes per document, crc-checked, `segment.go:572-601`), and three dense arrays of one slot per document per segment (`query.go:273-275`). Cost grows with history, not with the query. At 1 GB this is tens of MB per search; it has not been measured on a real inbox.
- Direction: index incrementally in a detached worker after capture or update, never on the query path; lay segments out so a query touches only its terms' blocks (memory-mapped or block-indexed metadata). This is also what makes F1 possible. Action: evolve.

### F11. The output spends context on boilerplate and on notes meant for a person
Lens: invisibility. Severity: medium. Verified in code. Known: no (line 96 lists only the silent excluded folder).

- Every search with a hit starts with a label of 0.5 to 1.1 KB of facts (`search.go:48-53`, `884-921`; knowledge facts 263 characters, `knowledge.go:35`). The system facts are added whenever system pages were scanned, even with no system hit shown (`search.go:906-908`); the page facts whenever a wiki was searched (`search.go:901-902`).
- The trailer carries upkeep advice to the agent: "eva-md index rebuilds it", "eva-md index --rebuild builds the index again", "indexed N files for the next search" (`search_index.go:579-596`).
- Show returns one entry with no context (`show.go:496-511`); CLI show prints a whole page up to 2 MiB while the MCP caps at 24 KiB (`show.go:95-100`, `mcp/tools.go:16`). In an excluded folder search prints nothing and exits 0, which reads as no matches (`commands.go:200-202`).
- Direction: a compact result whose tag meanings are stated once (skill or MCP instructions), self-repair instead of advice, show with a context window, and the same caps on both front ends. Action: evolve.

### F12. Reach: two hard-coded hosts, one machine, and scripts without spaces
Lens: reach. Severity: medium. Verified in code. Known: yes for CJK (lines 73, 91); not for the rest.

- Host names are literals in four readers: `inbox/walk.go:128`, `searchindex/refresh.go:332`, `resume/show.go:483`, `resume/read.go:280`. A third host means editing each.
- The inbox and its index live in this machine's state folder, so another machine's chats are not searchable; only what the update promoted travels with the wiki.
- A run of letters over 64 bytes is dropped from the index and shorter runs are one token (`token.go:43-45`), so Chinese or Japanese text is found only by the scan, and only when nothing else matched.
- Inode-based change detection and `Statfs` are Unix-only (`search_index.go:402-407`, `build.go:166-172`).
- Direction: a host registry; script-aware tokenisation (bigrams for scripts without spaces); an explicit decision on whether redacted chat evidence stays local with portable excerpts or syncs. Action: evolve.

### Smaller points, not in the top 12

- `show s:` reads a line by number without tracking off-the-record state (`show.go:539-556`), unlike search, `Walk` and the brief. The hook stores only event names while off the record (`hook/hook.go:452-459,494-497`), so this is a missing second line of defence, not a known leak. Inferred.
- `sourceLine` uses a `bufio.Scanner` with a 2 MiB buffer (`show.go:540-541`): a line over that ends the scan, so later lines would be "unavailable", while search passes such a line and keeps counting. The inbox caps text at 200 KiB (`inbox/inbox.go:24`), so this needs a hand-made line. Inferred.
- The line reader exists three times (`search.go:555-581`, `walk.go:96-120`, `show.go:539-556`), the searchable-kind list four times (`search.go:603-607`, `search_index.go:390-392`, `walk.go:83`, `segment.go:83-95`), and the off-record state machine in at least five readers.

---

## 4. Complexity: essential and accidental

Essential: the privacy shell (off-record ranges, the trust check, redaction applied at display, unforgeable one-line output), crash-safe cache writes, handling files that are appended while read, bounded time and memory, and honest reporting of what was not searched.

Accidental: keeping a substring scan and a token index as two truths and reconciling them; five source kinds and path rules pushed through one `match`; pages re-read on every query; positional refs that force whole-inbox listings; one line reader and one off-record state machine per reader; four tokenizers; index upkeep inside a query; and a hand-built segment engine (3,200 lines) that exists because of the two-dependency rule and because okf's own search needs the whole wiki loaded.

## 5. Overall call

Keep the safety shell and the segment writer's durability discipline. Rebuild the retrieval model: one tokenizer, one index over chat exchanges and page items, one scorer, serving both the prompt path and explicit search, with stable refs. The current code is careful and well tested, but its care goes into reconciling two matchers and reporting on a cache, while the things that decide whether the agent remembers (unit of retrieval, wording tolerance, automatic use, evaluation) are not there. More patches to `search_index.go` would deepen the reconciliation layer without moving those.

Whether the segment engine is evolved to a second format or replaced by an embedded store depends on the dependency rule, which is the owner's decision.

## 6. Open questions

1. May the redesign add a dependency for storage or retrieval (an embedded full-text store, or an on-device embedding model), or must retrieval stay standard-library Go?
2. May the prompt hook read a retrieval index if its p95 stays inside the hook budget? Today's rule says hooks never read or write the search index.
3. Is substring-anywhere matching a requirement, or can tokens be the contract, with an explicit contains mode?
4. Should redacted chat evidence travel between a user's machines (and to a team), or stay local with excerpts in the wiki?
5. Real-inbox figures are missing: index size, first-build time, and query latency at 1 GB and 10 GB.
6. Can any writer place text inside an off-the-record range of a session file (for example transcript recovery appending later)? If so, `show s:` would print it.
7. Does a first `eva-md search` through the shell, or a first MCP tool call, raise a host permission prompt? The live check is still pending.

---

## Verification

A second reader tried to refute each finding above against the code. Every cited file and line was opened; nothing was run but read-only commands, and nothing in the repository was changed. Paths are relative to `the repository`.

Result: no finding is refuted. 4 are confirmed as written, 8 are right at the core but off in scope, cause or severity. 3 problems the audit missed follow.

| Finding | Verdict | Severity given | Severity I assign |
| --- | --- | --- | --- |
| search-1 | partly | critical | high |
| search-2 | confirmed | high | high |
| search-3 | partly | high | medium |
| search-4 | partly | high | high |
| search-5 | partly | high | medium |
| search-6 | confirmed | high | high |
| search-7 | partly | high | medium |
| search-8 | confirmed | medium | medium |
| search-9 | confirmed | medium | medium |
| search-10 | partly | medium | low |
| search-11 | partly | medium | low |
| search-12 | partly | medium | medium |

### search-1: partly, high

- Right: no hook searches (`internal/resume/search.go:31-33`); the prompt hook adds only system pointers and knowledge rows (`internal/hook/pointers.go:192-207`, `internal/hook/recall.go:105-109`); the reminder is one line of at most 100 characters, on the first prompt and every 5th, and only when nothing else was added (`pointers.go:201-205`, `config.go:38`); `show s:` returns one entry (`show.go:496-511`, `read.go:703-711`); the MCP guide budgets about 5 lookups (`mcp/instructions.go:18`).
- Off: "anything not yet promoted to a knowledge page is remembered only if the agent thinks to search" is too wide. Every new chat gets the thread page's Now, the last chat's exchanges, the standing rules and the project's constraints without asking (`resume/resume.go:79-170`), and a changed Now arrives mid-chat as a refresh (`resume.go:196-219`). What is never automatic is retrieval by the prompt's content from older chats, Timeline lines, other branches and other projects.
- Severity high, not critical: the newest state of the thread does arrive unasked. The permission prompt is still unobserved (docs/verification.md, "The MCP server on real hosts").

### search-2: confirmed, high

- `match` judges one unit (`search.go:675-725`); the index's document is the same unit (`searchindex/build.go:48`, `inbox/walk.go:83-86`); `slot` and `insert` keep the best units with no grouping by session (`search.go:826-842`).
- One clarification for the builders: an inbox "line" is a whole prompt or a whole reply, up to 200 KiB (`inbox/inbox.go:24`), not a line of text. The narrow units are the page ones: a single Now item, Timeline line or system section line, and one statement line per knowledge page (`pages.go:263-298`, `knowledge.go:187-231`).

### search-3: partly, medium

- Right: no stemming, synonyms or similarity (`search.go:729-743`, `searchindex/token.go`, `score.go:142-147`); `retry` is not a substring of `retries`; the tool tells the agent to retry with other words (`mcp/tools.go:84`).
- Off: "exact wording" overstates it. A term of 3 or more characters matches as a substring in the scan and as a token prefix in the index, ignoring case, with camelCase and digit parts indexed too (`token.go:12-63`), so `deploy` finds `deployed`, `deploying` and `deployment`. What fails is a changed stem (`retry`/`retries`), a longer query form (`deployed` does not find `deploy`), synonyms and paraphrase.
- Severity medium: most lookups carry a distinctive term (a path, identifier, name or error), where this works; the cost is extra round trips on paraphrase.

### search-4: partly, high

- Right, and a little worse than stated: the default is 90 days on both front ends (`cmd/eva-md/commands.go:173`, `mcp/calls.go:229`); a thread page whose file was last changed before the window is skipped whole (`pages.go:128,162`), and its Now items are tested against the page's `as_of` (`pages.go:274-289`), so a thread dormant for three months loses its Now and its Timeline from search, although the brief would still show that Now. Knowledge and system pages are exempt (`knowledge.go:168-169`, `systems.go:339-340`). Nothing widens on a miss (`search.go:854-859`).
- Wrong clause: "with an index the window no longer buys speed". On a miss, search scans every file the index covers (`search_index.go:465-470`), and those are the files inside the window (`search.go:473`). design.md:1078 gives 1.7 to 2.9 s per GB for that scan. The window also bounds the thread pages read. So dropping the window, or widening on a miss, makes every miss a scan of the whole history unless search-5 is fixed first. Build search-4 after search-5.

### search-5: partly, medium

- Right: scan is substring, index is token prefix (`searchindex/doc.go:7-10`, `search_index.go:21-29`); the `base`/`database` case holds (a plain query never triggers the fill scan, `search_index.go:413-415`); K candidates are chosen by BM25 before `better` re-orders them (`doc.go:63`, `query.go:257-340`, `search.go:808-824`). Known: docs/known-limitations.md:73-76.
- Off: "the proposer and judge split, and most of search_index.go, then go away". Under one match contract only the matching glue goes: `short`, `estimate`, the fill and the nothing-found scan, roughly 80 of 621 lines. What stays is required by "the index is a cache, never the truth" and by this audit's own keep-list item 1: reading each candidate line again with its trust and window checks (`verify`, `lineAt`, 311-388), working out what the index covers and scanning tails (`classify`, `tail`, 221-291, 500-513), the fault guard and snapshot (82-100, 166-178), the top-up and the trailer notes.
- Severity medium: the user-facing symptoms are documented edge cases; the largest real cost is the full scan on a miss (see search-m3).

### search-6: confirmed, high

- The search tests compare index and scan on fixtures (`resume/search_test.go:25-47`) and assert fixed orders (`TestSearchRanksTermsThenExactCaseThenNewest`, `TestSearchRanksByScore`); none measures recall or rank on labelled queries. The benchmark plants one word and checks only p95 under 5,000 ms (`scripts/benchmark.py:91,387-388`). No counter exists: `internal/health/searchindex.go` reads manifests only, and the run log is written only by `internal/update`.
- The repository already has the pattern for the other two matchers: `knowledgepage/recall_test.go:255` (`TestRecallPrecision`) and `hook/pointers_test.go:427` (`TestNamedPrecision`).

### search-7: partly, medium

- Right: the ref is the first 8 characters of the session id and a physical line (`inbox/inbox.go:585-591`, `search.go:937-943`); show lists every project folder (`show.go:455-513`); every search lists every session file in the inbox to count prefixes (`search.go:441-466`); another Mac's ref ends in "source not found" (`show.go:518`).
- Off: "cannot be trusted" overstates it. Session files are only appended to, so on one Mac a line number stays put; a ref that can't be resolved fails closed (not found, or ambiguous at `show.go:520-522`); the one case that resolves to other text is the documented clear and re-capture (docs/known-limitations.md:101). The lint already tells another Mac's refs apart (docs/design.md:773); show does not say so. Keeping the inbox local is a recorded decision (docs/design.md:1240).
- Severity medium on one machine. It becomes high only if the redesign makes memory span machines or a team.

### search-8: confirmed, medium

- `better` orders by terms matched, exact case or phrase, page before chat, BM25, newest (`search.go:808-824`); page and tail lines are scored with the inbox's statistics (`search_index.go:152-160`); the design says there is no recency factor and no weight by kind (docs/design.md, "Search index", Ranking).
- Two consequences the audit did not spell out: a query typed in lower case counts every lower-case occurrence as "exact", so a line holding only `Retry` or `RETRY` ranks below all of them whatever its score (`search.go:755-765`); and the first key counts distinct terms with no stoplist or rarity, so in the any-term tier a long reply holding several common words outranks a short line holding the two rare ones, and the index picks its K "some" candidates the same way (`query.go:389-394`).

### search-9: confirmed, medium

- Four matchers: `search.go:729`, `searchindex/token.go` with `score.go`, `knowledgepage/recall.go:170-234` (own splitter, stoplist, floor) and `hook/pointers.go:446-470` (words joined by `-`, `_`, `.`). Pages are read on every search before the inbox (`pages.go:78-104`, `knowledge.go:187-199`, `search.go:335-339`).
- Note for the design: the recall and pointer matchers are precision-first on purpose (titles and names, a tuned floor), so one tokenizer and index is sound but the scoring policy must stay per surface, as the direction says. The inbox is left unsearched only when the 5 s limit ends among the pages (`pages.go:100-103`).

### search-10: partly, low

- Right: a CLI search tops up before it prints (`search.go:362`, `search_index.go:521-560`); open reads each segment's sources, docmeta and dictionary index whole (`segment.go:495-503,572-601`) and a query allocates three arrays of one slot per document per segment (`query.go:273-275`).
- Off: not "every manifest": only the folders in scope are opened, the project's and the general area's unless `--all` (`search_index.go:118-129`). The top-up runs only for the CLI, only when 4 MiB or more is unindexed and 1.5 s remain, and reads at most 16 MiB (`search_index.go:524,535,545`), about half a second at the design's 25 s per GB. Freshness does not affect what is found, since tails are scanned.
- Severity low as a defect today. It matters as a precondition of search-1: this layout cannot be opened inside a hook's budget.

### search-11: partly, low

- Right: the label is about 500 to 1,100 characters once a wiki is searched (`search.go:48-53,884-921`, `knowledge.go:35`); the system sentence is added whenever system pages were scanned (`search.go:906-908`); the trailer names upkeep commands (`search_index.go:579,585`); CLI show prints a page up to 2 MiB (`show.go:95-100`, `read.go:676-694`) where the MCP caps at 24 KiB (`mcp/tools.go:16`).
- Off: the excluded-folder silence is already listed (docs/known-limitations.md:96), and the MCP front end answers it with a plain line (`mcp/calls.go:246-248`). The label is part of the provenance shell this audit lists as must-keep (item 5), and the MCP instructions are capped at 600 characters (`mcp/tools.go:36`), so moving the tag meanings out of the result is a trade against safety, to be decided, not a free saving.
- Severity low: a few hundred tokens a search.

### search-12: partly, medium

- Right: host names are literals in `inbox/walk.go:128`, `searchindex/refresh.go:332`, `resume/show.go:483`, `resume/read.go:280` (and `health/status.go:168`); the index is under this machine's state folder.
- Off, hosts: in this part a third host is a five-line change. The real cost of a host is in capture and transcript recovery, outside this part.
- Off, CJK: text is not "mostly unindexed". A run of letters up to 64 bytes, about 21 CJK characters, is indexed as one token, so only its first characters can be found (`token.go:25-63`); longer runs are dropped. The harder limit is not cited: a term under 3 characters matches only between non-letters, in the scan too (`search.go:403,729-751`), so a two-character Chinese word, the commonest length, is never found inside running text by either path. Both are known (docs/known-limitations.md:73,91).

### Problems the audit missed

**search-m1. A project's search shows every project's knowledge pages, unattributed and ranked first.** Lens: recall-quality. Severity: high. Verified in code. Known: no.

- `resume/knowledge.go:168-176` lists every `k<N>.md`; `knowledgeFile` (187-242) never reads the page's Scope or Project, and uses State only for a `[retired]` tag; a knowledge hit prints no project, and on `--all` only the folder name `knowledge` (`search.go:945-951`); page lines rank before chat text (`search.go:814`).
- Prompt recall does the opposite: active rows only (`knowledgepage/index.go:100`) and only rows in the chat's scope (`knowledgepage/recall.go:119`), which docs/design.md:1438 lists as a defence against stored prompt injection. The knowledge map prints scope and project on every line (`knowledgepage/files.go:208-222`).
- Cost: in project A, a search shows project B's `decision [stated]` or `constraint` line under a label saying it rests on the user's words, above A's own chats, with nothing naming B. A retired fact ranks the same and is exempt from the window. The agent can apply another project's decision.
- Direction: one scope and state policy for every surface: this project's, all-projects and sibling rows by default; others only with all, ranked below; retired last. Print the project and scope on each hit. Action: evolve.

**search-m2. Standing rules pages are never searched.** Lens: recall-quality. Severity: medium. Verified in code. Known: no.

- Search lists only `<folder>/threads`, `systems` and `knowledge` (`resume/pages.go:78-83,127-128`). Rules live in `general/rules.md` and `projects/<slug>/rules.md` (`threadpage/rules.go:11-12`). The brief shows them in 600 characters and ends with "N more" and a show command (`resume/rules.go:19,82-120`).
- Cost: the user's own standing rules, the tier with the most authority, are outside search. A rule cut from the brief can be found only through a chat or Timeline line that mentions it, inside the window. The recall skill tells the agent to search before re-deciding a tool or a limit, and that search says "No matches." for a rule that exists.
- Direction: make rule items documents of the one index, tagged as rules with their scope, outside any window, ranked above chat text. Action: add.

**search-m3. A miss, the outcome agents retry on, costs a full scan of every indexed file in scope.** Lens: invisibility. Severity: medium. Verified in code. Known: yes (docs/known-limitations.md:72, last sentence).

- `search_index.go:465-470` scans every covered file when nothing matched, and also when a query that is not plain got too few hits (203-207). docs/design.md:1078 measures 1.7 to 2.9 s per GB, under a 5 s limit. `mcp/tools.go:84` and `mcp/instructions.go:18` tell the agent to search again on a miss, about 5 times. A path with no memory, which the recall skill asks the agent to search before changing code, is such a miss.
- The audit lists this behaviour as must-keep item 7 and does not count its cost. The index gives no speed-up exactly where the agent retries, and past about 2 GB in scope a miss can only answer "cut short".
- Direction: make a miss final from the index: one token contract for indexed files and tails, so "No matches." needs no scan; keep substring search as an explicit contains mode that alone scans. This is the part of search-5 that must land before search-4. Action: rebuild.

### Checked and found sound

The keep-list in section 2 was spot-checked where a finding leaned on it: the re-read and trust check of every candidate (`search_index.go:311-388`), the off-the-record state carried in the manifest (`manifest.go:43-49`, `search_index.go:263`), the prefilter never dropping a line JSON may escape (`search.go:417-423,627-641`), and the fault guard (`search_index.go:92-100`). All hold as described. The two "smaller points" about `show s:` (no off-the-record tracking, a 2 MiB scanner limit) are as the audit says: real in code, with no path found that writes chat text inside an off-the-record range (`hook/hook.go:452-459,494-497`).

---

## Verification: second pass, after the run was resumed

The "Verification" section above was written by the run that was interrupted. After the resume, each of the 12 findings was checked again from the code, without leaning on that section. Every cited file and line shows what the finding says; nothing is refuted. The verdicts and severities came out the same as above:

| Finding | Verdict | Severity I assign |
| --- | --- | --- |
| search-1 | partly | high |
| search-2 | confirmed | high |
| search-3 | partly | medium |
| search-4 | partly | high |
| search-5 | partly | medium |
| search-6 | confirmed | high |
| search-7 | partly | medium |
| search-8 | confirmed | medium |
| search-9 | confirmed | medium |
| search-10 | partly | low |
| search-11 | partly | low |
| search-12 | partly | medium |

The evidence for each verdict is in the section above and holds. What follows is only what the second pass adds or corrects.

### Additions and corrections

- **search-1.** docs/verification.md:149 says no live update has promoted a knowledge page yet, so the one automatic path keyed on a prompt's content is unproven in real use. It stays high, not critical, because the newest state does arrive unasked: Now, the last chat's exchanges, rules and constraints (`internal/resume/resume.go:79-170`).
- **search-2.** The every-term rule costs most on the curated tier. A chat unit is a whole prompt or reply (up to 200 KiB, `internal/inbox/inbox.go:24`), but a page unit is one Now item or Timeline line, a sentence. A three-term query therefore matches long replies and skips the page lines that summarise them.
- **search-4 and search-5, order of work.** On a miss, search scans every file the index covers (`search_index.go:465-470`), and those are the files inside the window (`search.go:473`). Dropping the window or widening on a miss, on today's engine, turns every miss into a scan of the whole history. The token contract of search-5 has to land first. This is the earlier section's search-m3; it is already listed in docs/known-limitations.md:72, so it is carried here and in the verdict notes, not as a missed problem.
- **search-9.** docs/verification.md:112 measures `eva_search` at about 0.11 s with a wiki of 200 projects and 400 system pages, so a page pass that leaves the inbox unsearched is theoretical today. Two of the gaps it lists are already known (docs/known-limitations.md:84 and :165).
- **search-11.** Measured label length: about 290 characters without a wiki, 733 with thread and system pages, 1,099 with a knowledge hit and a code line.
- **search-12.** A run of CJK letters up to 64 bytes, about 21 characters, is one token and can be found only from its first characters (`searchindex/token.go:25-63`); longer runs are dropped. "Mostly unindexed" should read "indexed in a form a query can't reach".
- **Line number.** Recall's scope filter is at `internal/knowledgepage/recall.go:137`, not 119.

### Missed problems carried in the structured result

search-m1 and search-m2 are kept as written above. The third is new.

**search-m4. Search returns the agent's own earlier lookups and other tool lines as top-tier hits.** Lens: recall-quality. Severity: medium. Verified in code. Known: no.

- A Bash tool line stores the first line of the command, up to 200 characters, and a Grep line its pattern (`internal/hook/hook.go:795-808, 839-845`), as kind `tool` (`hook.go:536-537`). Tool lines are indexed and matched like chat text (`internal/inbox/walk.go:83`, `internal/resume/search.go:603-604`). Nothing leaves out eva-md's own commands or the current chat: `SearchOptions` has no session (`search.go:34-46`).
- So `eva-md search retry policy` leaves `Bash <bin> search retry policy (exit 0)` in the inbox, and the next search for those terms finds it holding every term in the case typed, the two best tiers (`search.go:808-813`). Only the command line does this; the MCP tool's terms are never captured (docs/known-limitations.md:315). The brief's footer, the prompt reminder and the recall skill all name the command line first.
- Cost: a repeated query never says "No matches."; it shows the earlier searches. A real line that holds the word only with a capital ranks below every such line. In the score tier a tool line is weighed at the average length (`searchindex/query.go:296-301`), so with one occurrence it outranks any longer-than-average reply with one, and tool lines are many and short, which pulls the average down. Without an index, scores are 0 and newest first puts the latest lookups on top. A path search, which the recall skill asks for before each code change, fills the chat places with `Read` and `Edit` lines.
- Direction: treat kind as a field. Keep eva-md's own commands out of capture or out of results, leave out the current chat's lookups, and show other tool lines only when asked or below prompts, replies and page lines. Action: evolve.

Not carried: search-m3 (see "search-4 and search-5, order of work" above).
