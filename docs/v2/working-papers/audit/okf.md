# The okf library, and whether eva-md should keep building on it

Audit date 2026-11-10. Read-only. Nothing was run but grep, wc, git log/diff and one HTTPS read of the public OKF spec.

Paths:
- okf = `okf-agent-memory` at v0.8.0 (HEAD `1a6303d`, 2026-11-05), the version eva-md pins (`go.mod:7`).
- eva = `the repository`.

"Verified" below means I read the code on both sides of the claim. "Inferred" means a consequence I reasoned to but did not execute. I ran no test and wrote no file but this one.

---

## 1. Short answer

1. eva-md uses nine okf functions. What they buy is a frontmatter envelope codec (parse and serialize a flat `key: value` block), an ID check, an actor regex, a body/metadata sanitizer and a whole-wiki validator. Everything else a page store needs (lock, no-clobber create, compare-and-swap, skip-unchanged, folder fsync, log, index, bounded reads, symlink refusal) eva-md already wrote itself in `internal/okfadapter`, because okf's versions were unusable.
2. About 340 lines of eva-md production code and about 420 lines of tests exist only to compensate for okf behaviour. The codec eva-md would have to write to replace okf is about 500 lines, and eva-md has already written most of it three more times in other packages (section 4.3).
3. eva-md is not bound to the Open Knowledge Format. It is bound to one library's dialect of it. The format itself (Google Cloud's OKF v0.2 spec) is tiny and permissive, and staying conformant with it costs nearly nothing. The library diverges from that spec in places eva-md has copied (section 7).
4. Upstream is two months old, has one maintainer, tagged 23 releases in 61 days, changes behaviour in minor and patch releases, and is heading toward a registry, encrypted hub sync and an agent-instruction grammar, none of which eva-md wants. Its last release cost eva-md about 500 changed lines in one day to neutralise a feature eva-md did not need.
5. Recommendation: keep the on-disk page format (OKF v0.2 conformant, byte-compatible with today's pages), write eva-md's own page store, use okf once as a differential oracle during the switch, then drop the dependency. Do not fork. Working with upstream is worth a few generic bug reports if the owner lifts the no-issues rule, but it does not change the recommendation.

design.md already names this exit ("If okf doesn't work out", `docs/design.md:187-190`) and its trigger ("if the adapter keeps growing past its protections"). The v0.8.0 upgrade met that trigger.

---

## 2. How it works today

### 2.1 Data flow

`internal/okfadapter` (`adapter.go` 939 lines, `head.go` 99 lines) is the only package importing `pkg/okf` (`adapter.go:24`, `head.go:12`). 22 other non-test files call the adapter.

- **Open** (`adapter.go:90-100`): resolves symlinks, requires a regular `<root>/index.md` (`resolveRoot`, `adapter.go:540-563`). Reads no pages. The index check exists because okf silently switches to `<root>/knowledge/` when the root has no index (`bundle.go:160-172`).
- **Read / ReadMax / ReadHead** (`adapter.go:120-142`, `head.go:23-99`): open with `O_NOFOLLOW|O_NONBLOCK`, refuse non-regular files, bounded read, `okf.ParseConcept`, SHA-256 of the raw bytes. `ReadHead` reads in 64 KiB pieces and stops at the first body line holding a marker.
- **Create** (`adapter.go:148-188`): flock, `locate` (own ID checks plus `okf.ValidateConceptID`, no symlink in any path part), `Lstat` no-clobber, `okf.IsValidActor`, pre-create folders 0700, remove a leftover okf lock, `okf.SaveConcept{IsNew, AutoLog:false, AutoIndex:false}`, fsync every folder up to the root, append a log line.
- **Update** (`adapter.go:195-244`): flock, re-read, hash compare (`ErrConflict`), parse a fresh copy, run the caller's render, refuse an ID/path change, check the actor, skip the save if the result equals the file apart from the `generated` line, remove a leftover okf lock, `okf.SaveConcept`, fsync the folder, append a log line.
- **WriteFile / RemoveFile** (`adapter.go:301-337`): index.md, log.md and non-Markdown files only, eva-md's own atomic write (`writeAtomic`, `adapter.go:674-699`).
- **Validate** (`adapter.go:358-371`): `okf.LoadBundle` (reads every page) then `okf.Validate{Drift:true, Strict:false}`. Called from `eva-md validate` (`cmd/eva-md/main.go:197`), the doctor (`internal/health/health.go:274`), wiki init/adopt (`internal/wikiinit/init.go:189`) and the daily commit (`internal/wikigit/commit.go:404`).
- **Init** (`adapter.go:381-427`): writes eva-md's own log.md and root index first so that `okf.InitBundle` finds both and writes nothing.

### 2.2 What one page save does

Under eva-md's flock: read the file, hash, parse (eva). Then inside `okf.SaveConcept`: validate the ID twice (`mutate.go:463`, `480`), resolve the path with symlink evaluation twice (`mutate.go:467`, `484`), create/write/close/remove the `.okf-write.lock` file (`lock.go:62-78`), re-read and re-parse the page for the forged-verification guard (`provenance.go:26-58`), stamp `generated` with `time.Now().UTC()` (`mutate.go:508-511`), sanitize, serialize, temp file + fsync + chmod 0644 + rename (`mutate.go:32-65`). Back in eva: fsync folders, then read and rewrite the whole of log.md with its own temp file and fsyncs (`adapter.go:256-269`).

### 2.3 Caps, budgets, limits

| Limit | Value | Where |
| --- | --- | --- |
| Wiki lock wait | 60 s, polled every 10 ms | `adapter.go:63-64` |
| okf's own lock wait / stale age | 10 s / 30 s | `lock.go:19-20` |
| log.md read/rewrite bound | 16 MiB, then no more lines, silently | `adapter.go:247`, `256-269` |
| Actor part | 100 bytes | `adapter.go:500` |
| Page mode / folder mode / other files | 0644 / 0700 / 0600 | `adapter.go:38-39`, `664-667` |
| Concept ID depth | 8 | `types.go:16` |
| ReadHead piece | 64 KiB | `head.go:68` |
| okf frontmatter line | 64 KiB, silently truncating (see 6.3) | `parser.go:224-225` |

### 2.4 What it writes where

Pages and index.md/log.md in the wiki. Temp files `.tmp-eva-*` (eva) and `.tmp-okf-*` (okf) beside their targets. The flock file at `<state>/locks/wiki.lock`, outside the wiki. okf's `.okf-write.lock` in the wiki root for the length of each save.

---

## 3. Every okf call eva-md makes, and what each buys

`grep -rn 'okf\.' --include='*.go'` outside `internal/okfadapter` finds no call. Inside it:

| Call | Sites | What it buys | What eva-md adds around it |
| --- | --- | --- | --- |
| `okf.ParseConcept` | `adapter.go:137`, `216`; `head.go:32` | Frontmatter block to a `Concept`: 9 typed keys, the rest in `Extra` | Bounded no-follow read, hash. Three more hand-written readers elsewhere (4.3) |
| `okf.SerializeConcept` | `adapter.go:830` (and inside SaveConcept) | Canonical key order and quoting | `serializeAsSaved` + `withoutGenerated` to predict the bytes and skip no-op saves (`adapter.go:821-856`) |
| `okf.ExtractFrontmatter` | `adapter.go:767` | Split front/body (36 lines) | Feeds eva-md's own `verified` scan, needed because ParseConcept reads only two shapes of `verified` |
| `okf.ValidateConceptID` | `adapter.go:583` | Control/format/bidi characters, `..`, depth, dot-parts, reserved names (`mutate.go:152-221`) | Own checks first: trim, backslash, `path.Clean`, no symlink per part (`adapter.go:578-625`) |
| `okf.IsValidActor` | `adapter.go:167`, `226`, `494` | One regex (`types.go:19`) | `Actor()` builds a value that always passes |
| `okf.SaveConcept` | `adapter.go:180`, `236` | Sanitize title/description/body, forged-human-verification guard, stamp `generated`, serialize, temp+fsync+rename | flock, no-clobber, CAS, pre-mkdir 0700, lock-file cleanup, folder fsync, log line, error rewording |
| `okf.InitBundle` | `adapter.go:423` | Nothing: both files it would write already exist by design | Pre-writes both files, removes a leftover lock first |
| `okf.LoadBundle` | `adapter.go:566` | Whole wiki in memory with a link graph | `RootPath` guard against the knowledge/ redirect |
| `okf.Validate` | `adapter.go:370` | Missing frontmatter/type, broken links, orphans, actor shape, index drift, stale | Substring classification of its free-text warnings (5.5) |

Types used: `Concept`, `Generated`, `Verified`, `Source`, `SaveOptions`, `ValidateOptions`, `ValidationResult`, `Bundle`. `okfadapter.Concept` is a type alias (`adapter.go:46`), so okf's struct is the working model in `update/wiki.go` (33 field uses), `knowledgepage/page.go` (16), `systempage/write.go` (15) and others. Fields eva-md uses: `Type, Title, Description, Status, Body, Extra, Sources[].Resource, StaleAfter, Generated, Verified, RawContent`. Fields it never uses: `Tags, Governance, CodeRefs, Resource, Attestation`.

Size of what is reached: the eight files eva-md can reach (`parser, types, mutate, lock, provenance, path, bundle, validator`) are 2,633 lines; about 2,150 are reachable from the nine calls. The other eight files of `pkg/okf` (3,046 lines: search, scope, filter, mcp, bootstrap, domains, symlink, scorecard) and all of `pkg/sync`, `pkg/vault`, `pkg/registry`, `pkg/lock`, `internal/cli`, `cmd` (7,286 non-test lines) are never imported. eva-md reaches about 16% of the module.

---

## 4. Lines eva-md wrote only to work around okf

Counted by reading; each range would be deleted, not rewritten, if eva-md owned the codec.

### 4.1 In the adapter (191 lines of `adapter.go`)

| Lines | What | Why it exists |
| --- | --- | --- |
| 172-175 | Pre-create folders 0700 | okf uses 0755 (`mutate.go:34`, `523`) |
| 177-179, 233-235 | `clearOkfLock` before each save | okf's lock file survives a killed save |
| 339-354 | `RemoveLeftoverLock` | same; setup and init call it |
| 420-425 | lock cleanup + `InitBundle` | the call writes nothing |
| 429-452 | `okfTemp`, `okfLock`, `clearOkfLock` | okf's lock busy-loops when it can't remove a stale file (`lock.go:84-87`: `continue` before the deadline check) |
| 454-479 | `okfFailure` | okf errors name `.tmp-okf-*` and `.okf-write.lock` |
| 570-572 | `RootPath` guard | knowledge/ redirect |
| 733-735 | `Serialize` | callers must predict okf's bytes |
| 752-796 | `IsVerified`, `VerifiedIn` | okf reads `verified` only as a flow mapping or list of mappings and drops the key otherwise (`parser.go:280-299`) |
| 798-819 | `reservedKey`, `SmugglesFrontmatter` | mirrors okf's private body check (`mutate.go:19`, `423-442`) so a save never fails on it |
| 821-856 | `serializeAsSaved`, `withoutGenerated` | okf restamps `generated.at` on every save with no clock parameter (`mutate.go:508-511`) |

### 4.2 Outside the adapter (about 150 lines)

| Where | What |
| --- | --- |
| `cmd/eva-md/setup.go:1058-1062`, `internal/wikiinit/init.go:198-206` | Remove okf's leftover lock before looking for uncommitted changes |
| `internal/wikiinit/init.go:336-346` | `BlockingWarning`: classify okf warnings by substring (5.5) |
| `internal/systempage/page.go:96-98`, `298-300`, `323-327`, `361-365` | Copy of okf's link regex and its external-link rule, because okf reads links inside code spans (`bundle.go:14-21` strips only fenced blocks) |
| `internal/knowledgepage/page.go:92-93`, `315`, `379-381`; `internal/systempage/page.go:78-79`, `239-241`; `internal/systempage/write.go:253`; `internal/resume/systems.go:409` | `MapLink`: a link line ending every knowledge and system page so okf counts none orphaned, and the code that strips it again on read |
| `internal/knowledgepage/page.go:186-192`, `197-200`, `574-578`; `internal/update/wiki.go:277-279` | Refuse or rewrite `---` in titles and descriptions (okf refuses it, `mutate.go:418-420`) |
| `internal/systempage/write.go:256-277` | `flowList` and `saved`: shape values and predict bytes the way okf will write them |
| `internal/knowledgepage/files.go:226-227`, `289-291`, `300-312` | List unreadable and stray pages in the map and index so okf's orphan and parent-index checks pass |
| `internal/threadpage/escape.go:18-21`, `93` | Backtick lines starting with okf's reserved keys |
| `internal/wikigit/commit.go:359-366`, `internal/health/health.go:297-299` | Refuse and count `.tmp-okf-*` |
| `internal/update/pass.go:508-521`, `internal/update/runlog.go:35` | A `page_refused` failure class for okf's refusals, which counts toward quarantining a session after 3 in a row (`runlog.go:386-400`) |

### 4.3 Duplicate readers of the same format

Because okf's API is whole-file parse into a struct, eva-md wrote its own frontmatter readers for the paths that need bounded or lenient reads:

1. `okf.ParseConcept` through the adapter.
2. `knowledgepage.Lenient` + `scalar` (`internal/knowledgepage/page.go:459-548`), "as search does, without okf".
3. `systempage.parseFront` / `checkFront` / `ReadFront` (`internal/systempage/frontmatter.go`, 400 lines; `saved.go`, 95 lines).
4. `resume.frontValue` and the line scanners around it (`internal/resume/pages.go:205-208`, `241-254`; `internal/resume/systems.go:322`, `387-390`; `internal/resume/links.go:91`).
5. `lint` (`internal/lint/lint.go:294`).
6. `okfadapter.VerifiedIn` and `withoutGenerated` (`adapter.go:766-796`, `835-856`).

So the statement "okf owns the page format" (`docs/design.md:112`) is true for the write path only. They already disagree: on a duplicated key okf keeps the last value (`parser.go:246-247`, map overwrite) and `Lenient` keeps the first (`knowledgepage/page.go:504`).

### 4.4 Tests and docs

About 420 test lines: `adapter_test.go:250-483` (temp-file and lock-file failure wording, leftover lock, unremovable lock), `750-771`, `1058-1111`; plus the setup and init tests the lock cleanup added (commit `6bf7b5f`: `cmd/eva-md/setup_test.go` +48, `internal/wikiinit/init_test.go` +60). The adapter section of design.md is 89 lines and 14.4 KB (`docs/design.md:110-198`), and design.md names okf 104 times.

### 4.5 The cost of one upstream release

okf v0.8.0 (2026-11-05) added a lock file eva-md did not need, since it already had a flock. eva-md's two follow-up commits the next day: `29bf92f` (9 files, +164 −57) and `6bf7b5f` (10 files, +335 −47). A new failure mode (a lock eva-md can't remove fails every save and makes setup refuse the wiki) and a new known limitation (`docs/known-limitations.md:321`) came with it.

---

## 5. Where it falls short

Ranked by consequence for the goal.

### 5.1 The dependency costs more than it gives (architecture, high, verified)

Everything in sections 3 and 4. Each page save takes two locks, uses one of two atomic-write implementations (`mutate.go:32-65` for pages, `adapter.go:674-699` for everything else), validates the ID three times and reads the page three times. `InitBundle` is called to do nothing. The adapter's own header says each protection "is removed once an okf release covers it" (`docs/design.md:112`), but decision 15 sends no issues or pull requests upstream (`docs/decisions.md:87`), so nothing ever brings such a release about. The 13 gaps in `docs/design.md:155-171` can only grow.

Direction: one owned package (call it the page store) with a codec, the store operations the adapter already has, and a validator with typed findings. Rebuild the codec, keep the store.

### 5.2 Upstream risk (reliability-safety, high, verified)

- Age: first commit 2026-09-05. 242 commits, 23 tags to 2026-11-05: one release every 2.7 days (15 in September, 7 in October, 1 in November). Commits: 179 in September, 58 in October, 5 in November.
- People: `sknr` (Stephan Knauer) wrote 214 of 242 commits (88%) and is the only human committer. 13 commits come from 7 outside contributors, 12 from the `google-labs-jules` bot, 3 from dependabot. The project's own AGENTS.md describes an agent staging changes for the maintainer to commit.
- Stability: v0.4.4, a patch release, changed `SaveConcept`'s signature (`git diff v0.4.3 v0.4.4 -- pkg/okf/mutate.go`). Six of the last seven releases changed the files the adapter depends on (v0.7.2→v0.8.0: +427 −65 in four of them). v0.8.0 removed a CLI/MCP option as a breaking change in a minor release (`docs/releases/v0.8.0.md`).
- Direction: registry and vendor bundles (v0.5.0), quality scorecards (v0.7.0), zero-knowledge hub sync (v0.4.0), an instruction grammar and "dual-memory architecture" (v0.3.0), scope isolation (v0.8.0). The open roadmap items are search scoring, migration tools, MCP recipes and vendor overrides (`docs/project/ROADMAP.md:80-87`). None of eva-md's 13 gaps is on it. okf's model is an agent told by always-loaded rules to call `okf_search` and `okf_create` (`pkg/okf/bootstrap.go:16-46`), the opposite of a memory nobody has to operate.
- Engineering: 147 test functions in `pkg/okf`, strong on path and MCP security (34 + 28). No fuzz test for the hand-written parser. CI is Ubuntu only (`.github/workflows/ci.yml`); eva-md ships only on macOS.

Direction: no runtime dependency on a library whose releases must each be treated as breaking (`docs/design.md:185`).

### 5.3 okf's parser is not YAML, and misreads valid hand edits (reliability-safety, medium, verified by reading)

The wiki is Markdown a person may open and edit. okf's frontmatter reader is a line splitter:

- **A trailing comment defeats a lock.** `locked: true  # leave alone` is stored as the string `true  # leave alone` (`parser.go:239-247`, `375-388`: no comment stripping), `IsLocked` compares it with `true/yes/on` (`adapter.go:739-750`) and returns false. The page is rewritten, and the next save writes the comment into the value as `locked: "true  # leave alone"` (`parser.go:634-637`). `knowledgepage.Lenient` has the same reading (`page.go:522-525`). `locked_test.go:188-207` sets `Extra` directly and never goes through the parser.
- **Comments are dropped or turned into keys.** A comment line with no colon is skipped (`parser.go:239-242`) and lost on the next save. `# note: x` becomes a key named `# note`.
- **Block scalars on known keys lose their text.** `description: |` keeps only `|` (`parser.go:256-257` reads the inline value only).
- **Unknown flow mappings become strings.** `usage_window: { from: ..., to: ... }`, which the OKF spec itself uses, comes back as a quoted string (`parser.go:375-388`). This is eva-md's gap 11.
- **A line over 64 KiB truncates the rest of the frontmatter silently.** `bufio.Scanner` with the default buffer and no `scanner.Err()` check (`parser.go:224-225`). Every key after that line is gone, `locked` and `verified` included.
- **Plain `null`, `true`, `123`, `2026-01-01` titles are written unquoted** (`parser.go:490-508` quotes on punctuation only), so a YAML reader types them differently.
- **The body is trimmed** (`parser.go:648`), which eats leading indentation of a body that starts with an indented block.

Direction: a line-preserving codec. It owns the keys eva-md writes and keeps every other line byte for byte. It strips YAML comments when reading a scalar. Protection keys fail closed: a `locked` or `verified` key whose value it can't read counts as set.

### 5.4 Several readers, several answers (architecture and recall-quality, medium, verified)

Section 4.3. Search and recall read a page through one reader, the writer acts on another. A page with two `state:` lines after a hand-resolved merge is `active` to one and `retired` to the other. Each new page family has added a reader.

Direction: one streaming reader used by hooks, search and the writer, with a property test that every view of a page agrees.

### 5.5 Validation findings are classified by matching English text (reliability-safety, medium, verified by reading)

`okf.ValidationResult` carries `Warnings []string` only (`validator.go:29-42`). `BlockingWarning` returns true when any warning contains `actor`, `parent index` or `parent-index` (`internal/wikiinit/init.go:338-346`). Every okf warning starts with the page's path (`validator.go:113`, `at := c.Path`), and okf emits `"<path>: concept is stale (stale_after …)"` for any page past its `stale_after` whatever the options (`validator.go:233-238`). System pages carry `stale_after` as their review day (`internal/systempage/write.go:252`).

So a system page whose slug contains the letters `actor` (`refactor…`, `factory…`, `redactor`, `extractor`, `contractor`) blocks as soon as its review day passes: `eva-md validate` exits 1 (`cmd/eva-md/main.go:205`) and adopting the wiki on another Mac is refused with "wiki validation has findings; run eva-md validate to inspect" (`internal/wikiinit/init.go:195-196`). `eva-md validate` prints counts only (`main.go:202-204`), so the person can't see which finding it is. The only test of the rule uses `general/now.md` (`internal/wikiinit/init_test.go:576-601`). The same classification changes silently if upstream rewords a message.

Direction: typed findings (kind, page, detail) from a validator eva-md owns.

### 5.6 Page text is bent to pass okf's rules, which are stricter than the format (memory-quality, medium, verified)

- Every knowledge and system page ends with `Part of the [knowledge map](map.md).` / `[system map](map.md)` (4.2), because okf counts a page as orphaned unless a concept page links to or from it; a listing in index.md does not count (`bundle.go:447-535`: the index loop at 498-525 never marks a node linked). Readers then strip that line (`resume/systems.go:409`).
- `---` in a title or description becomes `--` (`update/wiki.go:277-279`, `knowledgepage/page.go:574-578`). A body line `---` becomes `\---` and a line starting `status:` or `type:` gets backticks (`threadpage/escape.go:88-93`).
- A link in a code span is checked as a real link (`systempage/page.go:298-327`).
- The doctor and `validate` fail on orphans and broken links (`health.go:277`, `main.go:205`). The OKF spec says consumers MUST tolerate broken links and must not reject a bundle for them (SPEC §6.1, §11), and has no orphan rule.
- log.md is written on every save and read by nothing in eva-md (`grep log.md` finds only the writer and path lists). It is optional in the spec (§3, §9). It costs a full read, rewrite and fsync per save, grows without bound, stops silently at 16 MiB, and is the one file every machine touches (`known-limitations.md:234` lists the last two points).

Direction: the rule eva-md needs is "every page is reachable from an index". The body is free Markdown after the first frontmatter block. Drop the log or derive it from git on demand.

### 5.7 Trust is per page, and a protected page stops all memory for its branch (memory-quality, medium, verified)

A thread page with `locked: true`, any `verified` value, or that fails to parse ("damaged") is never written, the pass is skipped with no model call, and the watermark moves past the batch (`update/pass.go:28`, `135-136`, `196-198`; `update/wiki.go:225-252`). Later chats on that branch stay in the inbox and are never summarized or promoted to knowledge. "Damaged" includes a page with merge-conflict markers above its frontmatter. The reason given is okf's model: "okf reserves verified for people, and every save would supersede it" (`update/wiki.go:218`; `validator.go:200-205` flags a `verified.at` older than `generated.at`; `mutate.go:508-511` restamps on every save). design.md documents the skip (`docs/design.md:836`); known-limitations does not.

Direction: verification and locking at the level of a statement or section, so an update can add around what a person confirmed. A page that can't be written holds its backlog (or writes to a sibling) instead of dropping it.

### 5.8 eva-md conforms to a dialect, not to the spec (architecture, medium, verified in code; spec read at HEAD today)

See section 7.

### 5.9 The page format fights multi-machine use (reach, medium, inferred)

Each save rewrites `generated`, and thread pages also `as_of`, `head`, `writer`, `model`. Two machines updating the same page between pushes change the same lines. Only log.md has a merge strategy (`internal/wikiinit/init.go:300`: `log.md merge=union`). A diverged push is "fixed by hand" (`internal/wikigit/commit.go:37`, `known-limitations.md:223`). The lock is a flock on one machine. The adapter uses `syscall.Flock`, `O_NOFOLLOW` and `O_NONBLOCK` directly (`adapter.go:702`, `878-891`; `head.go:52`), so it is Unix only.

okf has semantic three-way merges for index.md and log.md and distinct conflict-fork paths (`pkg/sync/index_merge.go`, `log_merge.go`, `reconcile.go:27-64`), but only inside its hub sync. The idea is worth having; the code is not importable into a git workflow.

Direction: eva-md's thread items already carry ids (`<!-- i:n3 -->` in `internal/okfadapter/testdata/thread.golden.md`). A git merge driver that parses both sides with the owned codec and unions by item id would make most cross-machine merges automatic. Put the per-save volatile fields where they can't conflict.

### 5.10 One unused okf idea is worth adopting (recall-quality, low to medium, inferred)

okf binds a concept to code paths (`code_refs`) with a governance tier (`hold`, `constraint`, `context`), and looks up what governs a path before an edit (`search.go:117-209`, `446-485`; `types.go:52-60`). eva-md stores paths on knowledge and system pages but recalls on prompt words only (`internal/knowledgepage/recall.go:53`) and installs no hook before a tool runs (`internal/install/templates.go:74-84`). An agent about to edit a file gets the constraint for it only if the prompt happened to name it.

Direction: path-triggered recall from eva-md's own recall index, once per path per chat, with no model. The idea, not okf's code, which loads the whole wiki per lookup.

### 5.11 The storage layer is barely measured (evaluation, medium, verified)

- One golden page of one type, 815 bytes (`internal/okfadapter/testdata/thread.golden.md`; `adapter_test.go:484-515`).
- No fuzz or property test of parse then serialize. No corpus of hand-edited frontmatter (comments, block scalars, duplicates, long lines).
- Step 4 of the upgrade policy, re-saving a copy of the live wiki and expecting a clean diff, is a manual step (`docs/design.md:183`).
- Nothing measures `LoadBundle` + `Validate` as the wiki grows, though the daily commit and the doctor run it over every page. `Validate` compiles two regular expressions per page (`validator.go:140`, `166`).
- Nothing checks conformance with the OKF spec itself.

### 5.12 A third party's lock and temp files in the user's wiki (invisibility, low to medium, known)

`.okf-write.lock` after a killed save is not ignored by git and shows in `git status`; one that can't be removed fails every save and makes setup refuse the wiki; `.tmp-okf-*` carries another product's name (`known-limitations.md:321`; `adapter.go:429-452`). All of it goes away with an owned store.

---

## 6. okf itself

### 6.1 What it offers that eva-md does not use

| Feature | Where | Worth using? |
| --- | --- | --- |
| BM25 search, filters, `stale_within` | `search.go`, `filter.go` | No. Re-tokenizes every page per query (`search.go:243-254`). eva-md's index is better and already borrowed the IDF (`internal/searchindex/score.go:87`) |
| Layered scopes: project, vendor, user, system | `scope.go` | No. Reads the working directory, `~/.okf`, `/etc/okf` and `OKF_*` variables (`scope.go:182-238`) |
| Path-bound governance lookup | `search.go:117-209` | The idea, yes (5.10) |
| `UpdateConcept`, `RelateConcepts`, `UpdateParentIndex`, `AppendLogEntry` | `mutate.go` | No. Load the whole wiki per change (`mutate.go:569`, `615`), always log (`662-666`), append unsorted (`304-319`) |
| Link graph (`Graph`, `InboundGraph`) | `bundle.go:447-535` | The idea, for recall expansion; 30 lines to own |
| Scorecard | `scorecard.go` | No. Scores an agent-written page 20/100 for stewardship by design (`scorecard.go:149`); an eva wiki would fail by construction |
| MCP server with create/update/relate tools | `mcp.go` | No. eva-md's server is read-only on purpose |
| Bootstrap, AGENTS.md block, tool symlinks, domain codex, instruction linter | `bootstrap.go`, `symlink.go`, `domains.go`, `aag/` | No. Always-loaded rules telling the agent to operate the memory |
| Hub sync, vault, registry, lock file | `pkg/sync`, `pkg/vault`, `pkg/registry`, `pkg/lock` | Not now. The merge ideas, yes (5.9). Read-only shared team bundles could be a later reach feature |

### 6.2 Quality

Good: thorough release notes, a security checklist with matching negative tests, atomic single-file writes, a guard against forging human verification, unknown top-level keys and unknown keys under `sources` preserved.

Weak:
- Hand-written YAML subset with the failure modes in 5.3.
- Lock: stale removal loops with no sleep and no deadline when the remove fails (`lock.go:84-87`); two waiters can both judge a lock stale and one remove the other's fresh lock (`lock.go:84-86`, no ownership check).
- Compare-and-swap only for pages loaded through `LoadBundle` (`mutate.go:498-506`).
- `Attestation` is a struct field that nothing parses or writes (`types.go:37`; no case in `parser.go:250-330`), while `docs/spec/OKF-COMPATIBILITY.md:18` claims full support.
- Validator diverges from the spec (section 7).
- README performance table compares against other products with figures the repo does not reproduce.

### 6.3 Pace, people, direction

Section 5.2.

---

## 7. Is the page format worth staying compatible with?

There are two things called the format.

**The Open Knowledge Format v0.2** is a Google Cloud spec (`GoogleCloudPlatform/knowledge-catalog/okf/SPEC.md`; okf names it as normative at `docs/spec/OKF-COMPATIBILITY.md:6`). I read it at HEAD today. It is a directory of Markdown files with YAML frontmatter. Conformance is three rules: every non-reserved `.md` has parseable frontmatter, with a non-empty `type`, and index.md/log.md follow their shape when present (§11). Consumers must not reject a bundle for unknown keys, unknown types, broken links or missing indexes. It gives a small vocabulary: `sources`, `generated`, `verified`, `status`, `stale_after`, actors as `<producer>/<version>` or `human:<id>`.

Worth keeping: yes. It costs a frontmatter block with `type`. It keeps the wiki plain files any tool reads, keeps a published vocabulary for provenance and trust, and keeps the way out open. Risk: it is v0.2, written for data catalogs, and v0.1→v0.2 already renamed fields (§13.1). Pin the spec revision eva-md targets.

**okf-agent-memory's dialect** is what eva-md is actually bound to:

| Topic | Spec | okf v0.8.0 | eva-md today |
| --- | --- | --- | --- |
| `stale_after` | ISO 8601 datetime with offset (§5, §5.5) | Warns unless `YYYY-MM-DD` (`validator.go:233-236`); a datetime is never stale (`filter.go:81-94`) | Writes a date |
| `sources[].last_modified` | datetime | Warns unless a date (`validator.go:159-161`) | not written |
| `generated.at` | "last meaningful change" (§5.2) | Restamped on every save | Skips no-op saves; closer to the spec than the library |
| Broken links | MUST tolerate (§6.1, §11) | Gate failure under strict | Doctor and validate fail |
| Orphans | no rule | Reported; gate failure under strict | Doctor and validate fail |
| `title` | optional | Required on save (`mutate.go:392-394`) | always set |
| log.md order | newest first (§9) | newest first | oldest first, for union merge |
| Nested YAML in unknown keys | allowed, used in spec examples | kept verbatim as lines, or stringified | not written |

And the part of the format that carries eva-md's meaning is not in either: the body grammar (Now and Timeline sections, item ids, source refs `s:<id>#<line>`, tags, History lines), about 13,000 lines across `threadpage`, `knowledgepage` and `systempage`. okf knows the envelope, which is the easy fifth.

Answer: stay conformant with the OKF v0.2 spec; stop treating okf-agent-memory's validator as the definition. Write down an "eva profile of OKF v0.2" (page types, keys, body grammar) under the existing `schema_version` (`.eva-md/schema.toml`).

---

## 8. The options

### A. Keep okf as it is (decision 15)

- Cost: the ~340 + ~420 workaround lines stay; a review each week; every tag treated as breaking; 13 gaps that can't close while no issue goes upstream; text contortions (5.6); two exceptions to "only eva's name shows".
- Risk: one maintainer; a security fix can arrive only inside a release that also changes behaviour, as v0.8.0 did; parser faults (5.3) are outside eva-md's control.
- Gain: no new work now; inherited path-security tests.
- Verdict: the costs recur and grow; the gain is one-time.

### B. Keep the page format, own the codec (recommended)

- Build: codec about 300-400 lines (strict writer byte-identical to today's output; tolerant, line-preserving reader), ID and actor checks about 90, validator with typed findings about 200, plus tests. Delete about 340 production and about 300 test lines. Replace readers 2-6 of section 4.3 with the one reader. Net production code about flat.
- Risk: reproducing okf's ID and path checks (the adapter already has stricter ones: `adapter.go:594-625`, `701-726`); byte compatibility of existing pages (controlled by a differential test against okf on a corpus and on a copy of a live wiki); losing upstream's fixes to code eva-md no longer runs.
- Gain: one lock, one atomic writer, an injectable clock, no foreign files in the wiki, typed validation, rules chosen for eva-md, human edits preserved, bounded streaming reads on every path, room for per-statement provenance (5.7), a merge driver (5.9), and other operating systems.
- Dependency: use okf as a test-only oracle on the migration branch, freeze the goldens it produces, then remove it from go.mod. `scripts/LICENSES.txt` drops its entry once no okf code is compiled in. If parser code is copied from okf instead of rewritten, MIT requires keeping its notice.

### C. Fork okf

- Cost: own 13,000 lines to use about 900; track upstream security fixes by hand; reverse the "never patch" rule; keep the line-splitter parser.
- Gain: small diffs fix the gaps in place.
- Verdict: no. It keeps an API shaped for agent-called CRUD and adds a maintenance duty.

### D. Work with upstream

- Evidence it could work: outside pull requests are merged within days (#61, #63, #64, #66 in the log).
- Limits: several of eva-md's needs contradict okf's choices (lock file in the bundle, strict orphans, UTC newest-first log, which is the spec's order). Fixes arrive bundled with other changes. It needs the owner to lift "no issues or pull requests", which may have been a privacy choice.
- Verdict: worth doing for generic faults (the unchecked scanner, the lock loop, datetime `stale_after`, comment handling) as a courtesy, with B regardless.

---

## 9. What a redesign must not lose

1. One package owns the codec; nothing else parses or writes frontmatter.
2. One wiki lock the kernel releases when a process dies, and `Locked` for writes that must land together (`adapter.go:105-117`, `858-909`).
3. No-clobber create and compare-and-swap update on the raw bytes' hash (`adapter.go:162-166`, `209-215`).
4. A save that would change nothing but the timestamp is skipped (`adapter.go:229-231`), so `generated.at` means the last real change and the wiki has no churn commits.
5. Temp file, fsync, rename, then fsync of every folder touched, before any watermark moves (`adapter.go:183`, `239`, `674-699`, `911-939`).
6. No symlink is followed on read or write; the root must be a real folder with a regular index.md (`adapter.go:540-563`, `594-625`, `701-726`).
7. Hooks read bounded data: `ReadMax`, `ReadHead`, and `Open` reads no pages (`adapter.go:85-100`, `125-142`; `head.go`).
8. A locked or human-verified page is never overwritten, and no agent write can add a human verification (`adapter.go:737-796`; `provenance.go:26-41`).
9. Keys eva-md doesn't know, `verified` and `sources` survive an update (`adapter.go:216`; `adapter_test.go:510-512`).
10. Existing pages re-save byte for byte apart from `generated`; no upgrade rewrites pages on its own (`docs/design.md:178-186`).
11. ID safety: control, format and bidi characters, traversal, depth, dot-parts, reserved names (`mutate.go:152-221`; `adapter.go:578-592`).
12. A valid actor on every save, `<host>/<model>` with a safe fallback (`adapter.go:481-523`).
13. Model text is escaped before it is saved so it can't forge a heading, fence, comment, tag or link (`threadpage/escape.go`).
14. Index lines in the spec's `* [Title](file) - Description` form, sorted and rebuilt, with titles escaped (`adapter.go:525-536`).
15. Folders 0700, non-Markdown files 0600; errors in eva-md's words with the errno kept.
16. Pages stay OKF v0.2 conformant: a frontmatter block with a non-empty `type`.
17. The kill-during-save and concurrent-writer tests (`adapter_test.go:555-600`, `947-1057`; `locked_test.go`).

---

## 10. Sketch of the owned page store

- **Codec.** Reads a frontmatter block line by line into an ordered list of entries, each either a key eva-md owns (typed) or an opaque run of lines kept byte for byte. Writes owned keys in today's order and quoting, opaque runs where they were. Strips comments when reading scalars. One streaming reader with a byte bound serves hooks, search and the writer.
- **Store.** The adapter's operations as they are, minus okf: flock, no-clobber, CAS, skip-unchanged, atomic write, folder fsync, with the clock passed in.
- **Validator.** Typed findings. Rules: frontmatter and `type` present; every page reachable from an index; links between pages resolve (a warning, per the spec); actors valid; index lines match descriptions. No orphan rule, no link-in-code-span rule.
- **Profile.** A written schema per page type under `schema_version`, and a test that a fixture wiki is OKF v0.2 conformant.
- **Migration.** On a branch: differential tests against okf v0.8.0 over a generated corpus and a copy of a live wiki (parse equality on eva-written pages, byte equality of saves). Freeze goldens. Remove the module. Existing wikis need no rewrite. Dropping `MapLink` and log.md, and moving `stale_after` to a datetime, are separate, optional, schema-versioned steps.

---

## 11. Open questions

1. Was "no issues or pull requests to okf" a privacy choice? May it be lifted for generic bug reports?
2. Must existing wikis stay byte-identical for ever, or is one schema-versioned normalizing commit acceptable (drop `MapLink`, datetime `stale_after`)?
3. Is working with okf's own tools on an eva wiki a goal, or only conformance with the OKF spec?
4. Should the doctor keep failing on orphans and broken links, which the spec tells consumers to tolerate?
5. Should `verified` freeze a whole page, or only the statements a person confirmed?
6. Does anyone want log.md? Nothing reads it.
7. Are Windows or Linux hosts planned? It decides whether the lock needs an abstraction now.
8. Which revision of the OKF spec does eva-md target? The spec's `stale_after` is a datetime today; the library's is a date.

---

## Verification

Checked 2026-11-10 by a second reader whose job was to refute each finding. Read-only: the cited code on both sides, okf's git history, eva-md's git history, and the OKF spec at HEAD (`GoogleCloudPlatform/knowledge-catalog`, `okf/SPEC.md`). No test was run and no parser input was executed; the parser claims were traced by hand. Paths are in eva-md unless they name an okf file (`parser.go`, `mutate.go`, `validator.go`, `bundle.go`, `lock.go`, `types.go`, `filter.go`, `search.go`).

### Result

6 confirmed, 6 partly, none refuted. Every cited line shows what the finding says it shows. The corrections are to cause, scope and severity, and four of them change how the options compare:

1. **The v0.8.0 upgrade was elective.** The release holds no security fix and covers no gap eva-md can use, and eva-md's own policy says not to upgrade for such a release. The lock file, finding okf-12 and the "cost of one upstream release" in 4.5 all follow from that one choice, and pinning v0.7.2 again undoes them with pages unchanged (missed-1).
2. **The fork option is costed at five times its size.** A fork needs the eight files eva-md reaches, 2,633 lines, not the module's 13,000 (missed-3).
3. **Much of what the report calls okf's rules is eva-md's own policy**, changeable under any option: failing on orphans and broken links, writing log.md, skipping verified pages (okf-6, okf-7).
4. **The line count doesn't favour a rebuild.** 340 workaround lines are fewer than the 600 to 700 the report estimates for the replacement. The case for owning the codec is control over its behaviour (okf-1).

The recommendation (own the codec, keep the format) survives these, but as a judgment about control, with a cheaper first step available and a second candidate for how to own the code.

### Verdicts

| Finding | Verdict | My severity | Reported severity |
| --- | --- | --- | --- |
| okf-1 | partly | medium | high |
| okf-2 | partly | medium | high |
| okf-3 | confirmed | medium | medium |
| okf-4 | confirmed | low | medium |
| okf-5 | confirmed | medium | medium |
| okf-6 | partly | low | medium |
| okf-7 | confirmed | medium | medium |
| okf-8 | confirmed | medium | medium |
| okf-9 | partly | low | medium |
| okf-10 | confirmed | low | medium |
| okf-11 | partly | low | medium |
| okf-12 | confirmed | low | low |

**okf-1, partly.**
- Confirmed: `grep 'okf\.[A-Z]'` finds calls only in `internal/okfadapter/adapter.go` and `head.go`, nine functions as listed. The eleven adapter ranges sum to 191 lines and the ranges outside to 144. `InitBundle` writes no file when both exist (`mutate.go:73-92`); it only takes and removes the lock. Each save takes eva-md's flock and okf's lock file (`adapter.go:861`, `lock.go:62-78`). The test ranges sum to 418 lines.
- Off, the title's comparison: 340 workaround lines are fewer than the replacement the report itself sizes in 8.B (codec 300 to 400, ID and actor checks 90, validator 200, plus tests), and 8.B says net production code stays about flat.
- Off, "deleted, not rewritten": about 100 of the 340 lines are needed in some form by any store: the `verified` scan, the no-op comparison, listing stray pages in an index, the temp-file checks, the save failure classes.
- Off, "the 13 gaps cannot close": v0.8.0 shipped a write lock and a compare-and-swap unprompted (gaps 1 and 2), in a form eva-md can't use. Decision 15 stops eva-md from asking, not upstream from shipping.
- Not weighed: `docs/design.md:187-190` already describes a narrower exit than the report's, own the writes and keep okf as the validator in the doctor and the tests. Kept as a test-only dependency, okf stays a differential oracle for as long as wanted and isn't compiled into the binary.
- Medium: nothing a user sees is broken; the cost is maintenance.

**okf-2, partly.**
- Confirmed: first commit 2026-09-05, HEAD 2026-11-05, 23 tags, 242 commits, 214 by one person under two names, 13 by 7 outside contributors, 12 by a bot, 3 by dependabot. v0.4.4 changed `SaveConcept`'s signature. Commits `29bf92f` and `6bf7b5f` are dated 2026-11-06. No fuzz test in the module; CI runs on Ubuntu only. The pace is slowing: 179, 58 and 5 commits, and 15, 7 and 1 tags, by month.
- Off, the cost of v0.8.0: it was chosen, not forced (missed-1). Net of deletions the two commits add 83 production and 311 test lines (`git diff --numstat 29bf92f~1 6bf7b5f -- internal cmd`); the rest of the +499 is docs.
- Off, "a security fix can only arrive inside a release that also changes behaviour": `docs/design.md:186` and AGENTS.md allow a temporary `replace` to a local fork for exactly that.
- Off, "13,000 lines to own for 900 used": see missed-3. The report's own section 3 says about 2,150 lines are reachable.
- Medium: with a pin, an MIT licence and under 3,000 reachable lines, the worst case, upstream gone or gone elsewhere, costs a copy of that code.

**okf-3, confirmed.**
- Traced: `locked: true  # leave alone` goes through `cutYAMLPair`, `setExtra` and `parseExtraValue`, where `json.Unmarshal` fails, it isn't a flow list, and `unquote` returns the whole string (`parser.go:239-247`, `375-388`). `IsLocked` then reads false (`adapter.go:743-747`), `readThread` doesn't skip (`internal/update/wiki.go:243`), `stamp` keeps the key (`wiki.go:318`), and the save writes `locked: "true  # leave alone"` (`parser.go:634-637`). `bufio.Scanner` has a 64 KiB token limit and `scanner.Err()` is never read (`parser.go:224-225`), so that line and every key after it are dropped. `description: |` keeps `|` (`parser.go:256-257`). `TestIsLocked` sets `Extra` directly (`locked_test.go:190-211`). No test or doc covers a trailing comment.
- Correction: after a 64 KiB line `verified` is still seen, because `VerifiedIn` scans the raw text with `strings.Split` (`adapter.go:766-796`). `locked` is lost, and so is every later key on the next save.
- Correction: a block scalar under an unknown key keeps its lines and loses only the `|` (`parser.go:346`, `624-631`).
- Not listed: numbers under unknown keys go through float64 (`parser.go:380-382`), so `1.10` is saved as `1.1` and an integer over 2^53 changes.
- The lock misreading can be fixed in the adapter now, by reading `locked` from the raw frontmatter as `VerifiedIn` reads `verified`. It doesn't wait for a rebuild.

**okf-4, confirmed.**
- The readers exist as listed. On a repeated key okf keeps the last (`parser.go:246-247`), `Lenient` the first (`internal/knowledgepage/page.go:504`). A second disagreement: for `verified:` with no value, or `[]`, `Lenient` reports verified (`page.go:526`, the key is present) and the adapter reports not verified (`adapter.go:788-790`).
- Scope: both need a hand edit. okf never writes a repeated key, since `Extra` is a map, and system write refuses one. `internal/lint/lint.go:294` only finds the `---` lines, and `systempage/frontmatter.go` is mainly the checker of `system write`'s input, which any design needs.
- Cause: okf's API doesn't force the copies. `head.go:23-37` does a bounded read through `okf.ParseConcept`.
- Low for harm today. The structural point stands.

**okf-5, confirmed.**
- `internal/wikiinit/init.go:338-346` as cited. `validator.go:113` puts the path first, and `:233-238` warns for any past `stale_after` whatever the options. A system page is `systems/<slug>.md` (`internal/systempage/write.go:225`) with `stale_after` as its review day (`write.go:252`), and a slug is a-z, 0-9 and `-` (`write.go:82-91`), so `refactor-x`, `factory` and `extractor` pass. `eva-md validate` then exits 1 (`cmd/eva-md/main.go:205`) and adoption is refused (`init.go:195`).
- `TestAdoptionAllowsNonBlockingWarnings` (`init_test.go:576-601`) says a stale page must not block and tests only `general/now.md`, so this is a bug against the test's own intent.
- Correction: the doctor and the daily commit don't call `BlockingWarning` (`internal/health/health.go:277`, `internal/wikigit/commit.go:399-410`), so both pass.
- The larger half: counts-only output (`main.go:202-204`) hides the page of every finding, an orphan or a broken link included, and no command names it.
- The substring match can be fixed under okf in a few lines, by matching the warning's fixed ending.

**okf-6, partly.**
- Confirmed: `MapLink` (`knowledgepage/page.go:93`, `315`; `systempage/write.go:253`). okf's orphan rule counts only links between concept pages (`bundle.go:447-535`). `---` becomes `--` (`update/wiki.go:277-279`; `mutate.go:418-420`). Nothing outside the writer reads log.md.
- Off, cause and action: eva-md runs okf with `Strict` off (`adapter.go:370`), so okf fails nothing for orphans or broken links. The doctor, validate and adopt choose to (`health.go:277`, `main.go:205`, `init.go:195`). Stop counting `Orphans` and `MapLink` can go, with okf in place. log.md is written by eva-md's own code with okf's `AutoLog` off (`adapter.go:176`, `256-269`), and okf's validator doesn't ask for one (`validator.go:100`). Only the `---` refusal and the body check are okf's rules. None of this needs a rebuild.
- Off, impact: recalled text doesn't carry the filler. Every reader strips it (`internal/resume/systems.go:409`, `knowledgepage/page.go:379`, `systempage/page.go:239`). Only `eva-md show` of a whole page prints the line.

**okf-7, confirmed.**
- `update/wiki.go:231-252` sets the skip, `internal/update/pass.go:196-198` makes it a `planSkip`, and the watermark moves with no model call. Since the watermark has moved, those chats are not read again after the page is unlocked or repaired; they stay in the inbox and in search only. `docs/design.md:605` and `836` say so; known-limitations has no line.
- Cause: eva-md's own policy. okf doesn't enforce it here: "superseded verification" is a gate finding (`validator.go:200-205`), and eva-md reads no `GateFindings` and runs `Strict` off. The spec says the opposite of the stated reason: "`verified` is independent of `generated.at`: content can change without re-confirmation" (section 5.2). So this can change under any of the four options.

**okf-8, confirmed.**
- Spec at HEAD, section 5: "Every timestamp-valued key in OKF is an ISO 8601 datetime with an explicit UTC offset"; section 5.5: `stale_after: 2026-09-23T00:00:00Z`. okf: `validator.go:233-236`, and `filter.go:84-87` parses the date layout only. okf's own compatibility page still gives `stale_after: YYYY-MM-DD` under "§ 5.3" (`docs/spec/OKF-COMPATIBILITY.md:32`), so the spec has moved under the library. `types.go:37` is the only mention of `Attestation` in `pkg`.
- Correction: the body grammar is 6,451 production lines (12,255 with tests), not 13,000.
- Correction: "conformance costs a frontmatter block with `type`" leaves out section 11's third rule, that log.md follows section 9, newest first. eva-md writes oldest first for the union merge (`adapter.go:249-256`). A test against the spec fails on that until log.md is dropped or reordered, and reordering gives up the merge rule.

**okf-9, partly.**
- Confirmed: `generated` is restamped (`mutate.go:508-511`); `as_of`, `head`, `model` and `writer` are set on each pass (`update/wiki.go:296-314`); only log.md has a merge rule (`init.go:300`).
- Off, cause: serialization isn't the first blocker. eva-md never pulls, rebases or merges (`docs/known-limitations.md:223`), and one wiki per Mac is the design, with an instance guard (`docs/decisions.md:25-28`, `docs/design.md:1295`). A merge driver does nothing until there is a pull step, and it could be built on okf's parser as well as on an owned one.
- Off, scope: Windows is outside the product, since setup refuses anything but macOS, and the flock and `O_NOFOLLOW` calls are eva-md's own code, not okf's.
- Inferred, as the report says. Low against today's goals.

**okf-10, confirmed.**
- `search.go:117-209` as cited. `internal/install/templates.go:72-88` installs no hook before a tool runs, for either host. `internal/knowledgepage/recall.go` scores prompt terms only.
- An idea, not a fault. Neither of us checked whether Codex has a hook before a tool runs, or what a synchronous hook on every edit does to the latency targets.

**okf-11, partly.**
- Confirmed: one golden file of 815 bytes; no fuzz or property test of frontmatter parse and serialize; no benchmark of Validate; step 4 of the upgrade policy is manual.
- Off: more is measured than stated. `internal/threadpage/okf_test.go` writes thread, rules, now and guide pages through okf, reads each body back byte for byte and validates the wiki (`TestPagesThroughOKF`, `TestEscaperCorpusThroughOKF`). `threadpage` has `FuzzParse` and `FuzzScanner` for the body. `TestFullFixtureWiki`, `TestSerializeIsWhatCreateSaves` and the system and knowledge page tests go through the adapter. The gap is the envelope under hand edits, and scale.
- Off: the two regular expressions "per page" (`validator.go:140`, `166`) don't run on an eva wiki. The root index declares no `okf_version`, and no source has an `id`.

**okf-12, confirmed.**
- As cited; `docs/known-limitations.md:321` lists it. It exists only since the pin moved to v0.8.0 on 2026-11-06 (missed-1).

### Missed

**missed-1. The pin moved to v0.8.0 with no security fix and no usable gap covered, against the upgrade policy** (architecture, medium, verified in code and history).
- Evidence: `docs/design.md:178`, "Don't upgrade just because a release exists". okf's `docs/releases/v0.8.0.md` lists no security fix in `pkg/okf`. The message of commit `29bf92f` says "Nothing else in v0.8.0 reaches eva-md" and that okf's parser and serializer are unchanged. `go.mod:7`.
- Impact: the report's main evidence of upstream cost (4.5, okf-2, all of okf-12) is the price of one upgrade eva-md chose. It is reversible: pinning v0.7.2 again removes the lock file and about 83 production and 311 test lines, with pages byte for byte the same.
- Direction: cost option A at v0.7.2. Pin back, or record why v0.8.0 is needed, before the rebuild decision, so that decision isn't made on a cost eva-md chose.

**missed-2. now.md and the system map are rewritten whatever `verified` says, and the system map whatever `locked` says** (reliability-safety, low, verified in code).
- Evidence: `internal/update/wiki.go:689-721` checks `IsLocked` only. `internal/systempage/write.go:339-348` checks neither. okf reads `verified` only as a flow mapping or a list (`parser.go:280-299`) and keeps the key nowhere else, so `verified: true` is erased by the save. The wiki's own README tells the person "Human-verified pages are also left alone" (`internal/wikiinit/init.go:281`). No test covers it.
- Impact: a verification mark on a project's overview is erased, or outdated, by the next pass with no message. The report's invariant 8 in section 9 ("never overwritten") holds for thread, rules, knowledge and system pages only; a team would carry it over as wider than it is.
- Direction: one guard in the store, not one in each caller. `Update` refuses a locked or verified page unless the caller says the page is built by code.

**missed-3. The fork option is costed at 13,000 lines; the code a fork needs is 2,633** (architecture, medium, verified in code).
- Evidence: `wc -l` over `parser.go`, `types.go`, `mutate.go`, `lock.go`, `provenance.go`, `path.go`, `bundle.go` and `validator.go` gives 2,633. All of `pkg/okf` is 5,679 non-test lines and the module 13,235. Those eight files reach outside themselves for five names only: `Scope`, `IsExternalLink`, `ComputeScorecard`, `ScorecardResult` and `IsStaleWithin`.
- Impact: option C is dismissed on a figure five times too high, and "900 used" contradicts section 3's 2,150. A copy of that subset under its MIT notice saves today's bytes by construction, which is the main risk the report names for option B, and drops the lock file, the 0755 folders and the restamp with small diffs. It keeps the parser faults of okf-3, which then have to be fixed in the copy.
- Direction: compare two ways to own the codec at their real sizes: copy the subset and fix it, or rewrite it and prove it with a differential test. Decide on which gives a line-preserving reader sooner.
