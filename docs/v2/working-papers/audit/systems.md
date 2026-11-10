# System pages, the code status of what they cite, and the daily wiki lint

Audit of `internal/systempage`, `internal/coderef`, `internal/lint`, `cmd/eva-md/system.go`, `cmd/eva-md/code.go`, `internal/install/templates/digest-skill.md` and `internal/health/code.go`, read in full, with their consumers (`internal/resume/systems.go`, `internal/hook/pointers.go`, `cmd/eva-md/maintain.go`, `internal/health/background.go`, `internal/health/health.go`) read where a claim depends on them. All paths are relative to `the repository`. Read-only work: no test, setup, update, maintain or commit was run.

Size of the part: about 4,500 lines of non-test Go (systempage 2,326, coderef 1,158, lint 401, cmd 459, health/code 166) and about 3,240 lines of tests.

"KL:n" below is a line of `docs/known-limitations.md`.

---

## 1. How it works

### 1.1 What a system page is

One Markdown page per system, `systems/<slug>.md`, global to the wiki (`internal/systempage/page.go:1-49`). Frontmatter: `type: system`, `title`, `description`, `status: stable`, `generated: { by: <host>/<model> }`, `sources`, optional `repos`, `aliases`, `stale_after`, then `as_of` and `state: active` (`internal/systempage/frontmatter.go:17-22`). Body: eight fixed `##` sections in a fixed order, only `## Summary` required (`page.go:81-83`, `page.go:234-284`). Code closes every page with `Part of the [system map](map.md).` (`page.go:78-79`, `write.go:253`).

A source is either an `https://` URL (optionally with a version) or a code source `<repo>@<commit>[:<path>]` (`source.go:22-31`, `internal/coderef/coderef.go:49-65`). The code sources are the page's only tie to code.

### 1.2 Creation: only an agent, only when asked

The only writer is `eva-md system write <slug> [--replace] < page.md` (`cmd/eva-md/system.go:18-87`). It is driven by the digest skill, a prose instruction file installed with both plugins (`internal/install/templates/digest-skill.md:1-43`, `internal/install/templates.go:22-25`). The skill tells the agent to: read the map and any page it will change with `eva-md show`; run `eva-md system status <slug>`; write the page to a temp file; pipe it to `system write`; fix the lines a refusal names; and end each reply listing systems done and remaining so the background update carries that in a thread's next steps (`digest-skill.md:10-13`).

Nothing else writes a system page. `internal/update` (the background model run) has no reference to `systems/`, `systempage` or the map (grep over `internal/update/*.go`, non-test: no match). `cmd/eva-md/maintain.go:83-108` runs knowledge, commit, lint, code status, pruning and index steps, none of which writes a system page or its map.

### 1.3 The write (`systempage.Write`, `write.go:101-165`)

1. `CheckSlug`: `^[a-z][a-z0-9-]{1,63}$`, not `map`, `index`, `log`, `agents` (`write.go:37`, `write.go:82-91`).
2. Read at most 24 KB + 1 from stdin (`write.go:105`, `page.go:67`).
3. `openWiki`: schema version 1 and the config's instance must match (`write.go:170-190`).
4. `readOld`: a page that exists needs `--replace`; a person-verified or locked page, or one okf can't parse, is refused (`write.go:195-213`).
5. `check`: line by line (`page.go:128-157`). Refuses non-UTF-8, `[REDACTED:` markers, control and format characters, HTML comments, code fences (`page.go:193-229`); headings outside the eight sections or out of order, text before Summary, rule-like lines, speaker-shaped lines (`page.go:234-284`); links that are neither `https://` nor an existing system page, checked both as Markdown reads them and as okf's regex reads them (`page.go:301-328`); secrets by the daily commit's scan, on the text, on the frontmatter as okf will save it, and on the map and index lines (`page.go:152-188`). The frontmatter is read by a hand-written YAML subset (`frontmatter.go:89-128`, `frontmatter.go:333-400`). Problems are grouped, name lines and never text, at most 20 (`page.go:409-458`).
6. `verify`: within 3 s, 1 s a git call, at most 3 clones a commit, check each cited path exists at the cited commit in a local clone; a missing path refuses the page, everything it can't check becomes a printed note (`verify.go:20-23`, `verify.go:38-127`).
7. Under one hold of the wiki lock: read the map, save the page (Create, or Update with compare-and-swap on the hash read in step 4), then `refresh` (`write.go:142-152`, `write.go:224-238`).
8. `refresh` rebuilds from every page's frontmatter: `systems/index.md`, then `systems/map.md`, then `.eva-md/names.tsv` and `.eva-md/repos.tsv`, then the root index line (`write.go:306-336`). It never commits (`write.go:98`).

Output: `wrote systems/<slug>.md (<n> bytes); the map has <m> systems`, then one note per repo and commit checked or not, then a `no repos` note when the page names no repo (`cmd/eva-md/system.go:82-85`, `write.go:69`, `write.go:161-163`).

### 1.4 Derived files

| File | Built by | Read by | Bound |
| --- | --- | --- | --- |
| `systems/map.md` | every system write (`write.go:320-327`) | brief (`internal/resume/systems.go:298-334`), prompt hook (`internal/hook/pointers.go:330-342`), code pass (`code.go:95-111`), doctor (`internal/health/background.go:543-567`), search, `eva_status` | read cap 256 KB everywhere (`code.go:20`, `resume/systems.go:33`, `hook/pointers.go:44`) |
| `systems/index.md` | every system write (`write.go:308-318`) | people and agents via `show systems/index` | none on write |
| `.eva-md/names.tsv` | every system write; `eva-md doctor` repair (`names.go:169-202`) | prompt hook for name pointers (`hook/pointers.go:284-290`) | 2,000 names, 128 KB; oldest `as_of` loses aliases, then every name (`names.go:27-33`, `names.go:89-116`) |
| `.eva-md/repos.tsv` | same as names | prompt recall, for sibling projects (`internal/hook/recall.go:124-128`, `repos.go:94-106`) | 64 KB, oldest `as_of` left out (`repos.go:23`, `repos.go:49-59`) |

All four live in the wiki and are staged by the daily commit (`names.go:17-19`, `repos.go:9-12`).

### 1.5 Code status (`internal/coderef`)

`coderef.Status` measures, for each page and each repo-and-commit it cites: `cat-file -e` (has the commit), `merge-base --is-ancestor` (on HEAD's history), `rev-list --count --max-count=1001 <commit>..HEAD -- <paths>` (commits touching the cited paths, capped at "1000+"), `ls-tree HEAD -- <paths>` (paths gone at HEAD), and optionally `log -1` for the newest change (`status.go:334-400`, `git.go:232-348`). Clones are found through the identity cache only: at most 4,096 entries listed, 8 clones a repo kept, 3 tried a commit, each re-resolved before use (`clones.go:13-20`, `clones.go:45-90`). The result is `<state>/code-status.json`, mode 0600, local to the machine, never committed, holding counts, keys, paths, the clone folder, branch and HEAD, and never a commit subject (`status.go:47-90`, `status.go:404-413`).

Four different measurers use it, each with its own budget:

| Where | Trigger | Budget | Saves? |
| --- | --- | --- | --- |
| `eva-md system write` | agent | 3 s total, 1 s a call; paths only, no movement (`verify.go:20-23`) | no |
| `eva-md show systems/<slug>` | agent | 3 s, 1 s a call; prints on stderr (`cmd/eva-md/code.go:30-34`, `code.go:142-163`) | no |
| daily maintain, `eva-md system status` | first hook event of a day; by hand | 120 s a pass, 10 s a call, 64 repo-and-commit pairs (`cmd/eva-md/code.go:31`, `status.go:171`, `status.go:212-214`) | yes |
| startup brief | chat start | own repo only, 60 ms a call, 150 ms total, only with 200 ms left (`internal/resume/systems.go:44-48`, `systems.go:193-216`) | no |

The prompt hook never runs git: it reads the status file (256 KB cap) and the map (`hook/pointers.go:27-33`, `pointers.go:346-370`).

### 1.6 What a chat gets

- Chat start: up to 3 `System: <title>: <description> (<note>): <bin> show systems/<slug>` lines for systems whose repos include the project, those citing the project's code first, then slug order, plus a `System index` line; 200 characters a pointer, 600 in all (`resume/systems.go:25-36`, `systems.go:64-108`, `systems.go:236-264`).
- Each prompt: up to 3 pointers (12 a chat, each page once) for systems the prompt names by title, slug or alias: `eva.md: <title>: <description> (code moved N commits since <day>): <bin> show systems/<slug>`, 160 characters (`hook/pointers.go:35-45`, `pointers.go:241-281`, `pointers.go:372-409`).
- The page itself only when the agent then runs `eva-md show` or `eva_show`.

### 1.7 The daily lint (`internal/lint`)

Run once a day by maintain after the commit (`cmd/eva-md/maintain.go:93-99`). It reads the whole inbox once to count the lines of every session file (`lint.go:155-198`), then every thread page (`general/threads/*.md`, `projects/*/threads/*.md`, `lint.go:371-390`) line by line, and records two things in `<state>/lint.json`: thread pages over 512 KB (`lint.go:29-31`, `lint.go:92-94`) and source refs `s:<session>#<line>` sorted into resolved, bad, removed, elsewhere and unknown (`lint.go:229-266`). The only reader is the doctor (`internal/health/background.go:449-509`). `lint.Citing` also serves `eva-md inbox clear`'s preview (`lint.go:320-350`, `cmd/eva-md/inbox.go:409`).

---

## 2. What must be kept

These are deliberate and correct; a redesign that loses any of them is a regression.

1. **Refusals and logs carry line numbers and counts, never text** (`page.go:409-411`, `write.go:71-75`, `lint.go:36-37`).
2. **The secret scan runs on what will actually be saved and shown**: the page's lines, the frontmatter re-serialized as the store writes it, and the page's lines on the map and index (`page.go:152-188`). A pointer the scan would flag is dropped (`hook/pointers.go:403-407`).
3. **Source hygiene**: no user name, password or credential-named query parameter in a URL (`source.go:22-49`); no remote with a password (`coderef.go:121-130`); paths without `..`, `.`, spaces, control characters or `](` (`coderef.go:137-148`).
4. **Compare-and-swap against people**: a replace applies only to the page as read; a person-verified or locked page is never written; frontmatter a person added survives (`write.go:195-254`; tested in `write_test.go:253-316`).
5. **Instance and schema guard before any write** (`write.go:170-190`).
6. **One lock hold for the page and its derived files, index before map** (`write.go:142-152`, `write.go:299-336`; `write_test.go:229-247`).
7. **Text that reaches a prompt is cleaned twice**: at write (`page.go:193-229`, `frontmatter.go:169-181`) and again when a hand-edited value is read back (`write.go:597-618`). Speaker-shaped lines, HTML comments, fences, hidden characters and non-system links cannot be stored through the write.
8. **Hooks never run git and read only bounded local files for pointers** (`hook/pointers.go:27-33`); the brief's live count is hard-budgeted (`resume/systems.go:44-48`).
9. **Read-only git hardening**: fsmonitor off, `core.hooksPath=/dev/null`, `--no-optional-locks`, `--literal-pathspecs`, lazy fetch off by environment and, in a possibly partial clone, by flag; own process group, killed whole on timeout; capped stdout and stderr; `cmd.Dir = "/"` (`git.go:59-93`, `git.go:108-195`). Never fetches, never writes into a clone (`coderef.go:1-9`).
10. **A clone is confirmed before it is measured**: the folder is resolved again and must still hold that repo (`clones.go:68-90`).
11. **Honest "unchanged"**: an entry unchanged while its commit lacks a cited path, or off-branch with nothing counted, is "not measured", never "unchanged" (`status.go:92-113`); a status taken at other commits than the map now cites is ignored (`status.go:135-159`); the brief consults the saved status so it does not contradict it (`resume/systems.go:135-138`).
12. **The status file is local, 0600, opened without following links and within a size bound, and holds no commit subject**; the subject is redacted, flattened and cut to 80 characters before it is printed (`status.go:415-447`, `git.go:322-348`).
13. **Write-time path check**: a cited path the cited commit lacks refuses the page; a write never waits on a clone (`verify.go:25-37`, `verify.go:105-126`).
14. **Lint tells apart refs that are bad from refs removed on purpose, another machine's, or unknowable while a folder is unreadable** (`lint.go:239-266`); it streams and never loads the inbox or wiki whole (`lint.go:58-62`).
15. **Sibling scope for recall comes from pages listing two or more repos** (`repos.go:28-69`, `repos.go:92-106`).
16. **The `Unverified` section is tagged as such in search hits** (`resume/systems.go:412-415`).
17. **Tests never depend on machine speed**: every time bound is a variable raised in `TestMain` (`verify.go:16-23`, `cmd/eva-md/code.go:26-34`, `status.go:168-171`, `git.go:38-41`).

---

## 3. Where it falls short of the goal

Ranked by consequence. Each says whether it was verified in code or inferred, and whether `docs/known-limitations.md` lists it.

### systems-1. A system page is a snapshot nothing keeps current (critical, memory-quality) — verified, known (KL:113)

- Staleness is measured and then only displayed. The doctor's fix for moved, gone or review-due pages is literally `in a chat, ask to digest those systems again` (`internal/health/code.go:162-164`).
- The background update, which already runs a model after every chat and already knows which files a chat changed, never reads or writes `systems/` (no reference in `internal/update`). So eva-md can capture the chat that changes a system's interface and still serve the old page for months.
- Sources are page-level, not claim-level: the skill asks that "each line under `## Decisions and constraints` names its source" (`digest-skill.md:11`) but `checkBody` enforces nothing of the kind (`page.go:234-284`). When one cited path moves, nothing can say which statement is now suspect, so a refresh has to redo the whole page.
- For document sources there is no fingerprint at all: only an optional version in the URL (`source.go:12-31`). `stale_after` is a date the agent guesses (`frontmatter.go:244-256`, KL:128).
- This directly contradicts the product's own stated goal: "You never curate, review or trigger it" (`docs/decisions.md:7`) and "Zero-touch" (`docs/decisions.md:31`).

Direction: make system knowledge a maintained artifact. Movement on cited paths and captured chats that touch those paths feed a refresh queue; the background job patches only the affected claims from a bounded diff, re-cites the commit, and code validates the result as the write does today. Claims carry their own sources. Rebuild the lifecycle; keep the write checks.

### systems-2. Creating a page is a manual, multi-step, shell-driven protocol (high, invisibility) — verified, not listed

- No page exists until a person asks an agent to digest. Setup does not bootstrap one and the README introduces the command, not the habit (`README.md:183`).
- The protocol is: `show` the map, `show` each page, `system status`, write a temp file, pipe it to `system write`, read the refusal, repeat (`digest-skill.md:10-12`). Every step is a shell call. The skill's frontmatter declares only `name` and `description` (`digest-skill.md:1-4`), so nothing pre-approves those commands; install adds no permission rule (grep for `permissions` in `internal/install`: none).
- The input is Markdown with frontmatter that a hand-written YAML subset must accept (`frontmatter.go:89-128`), with about 30 distinct refusal reasons (`page.go:104-113`, `page.go:193-284`, `frontmatter.go:47-331`). The values are then thrown away as text and the page rebuilt from fields: "The page okf saves is built from these values, never from the text" (`frontmatter.go:46`). The agent pays tokens and round trips to satisfy a text grammar the code does not keep.
- A page shown cannot always be written back: `show` prints a key a person added, and `parseFront` refuses any key outside its list (`frontmatter.go:94`, `frontmatter.go:116-118`), although the write would have preserved it (`write.go:240-254`).
- A link to a system with no page yet is refused (`page.go:344-345`, KL:115), so a digest of several related systems must write, then rewrite with `--replace`.
- Progress across a multi-system digest is carried by the agent ending each reply with a list, which the background update is hoped to copy into next steps (`digest-skill.md:13`).
- A damaged map blocks every write until someone removes it by hand: `systems/map.md can't be read as a wiki page; remove it, and the next write builds it again` (`write.go:281-293`), although the map is fully derived.
- If a derived file fails to write after the page was saved, `Write` returns an error (`write.go:153-155`) and a retry without `--replace` is refused because the page now exists.

Direction: the platform bootstraps a project's page itself and offers one structured write operation (title, description, sources, sections as data) over a transport every surface has; the platform renders the page. Rebuild.

### systems-3. A chat gets a command to run, chosen by name or by slug order; cited paths never drive recall (high, recall-quality) — verified; the pointer limits are listed (KL:62, KL:135), the missing path-driven recall is not

- The brief picks the first 3 systems whose repos include the project, code-citing ones first, then alphabetical by slug (`resume/systems.go:77-108`). In a monorepo with 30 system pages, a chat started in `services/orders` gets the three whose slugs sort first. The map line already holds every cited path (`write.go:394-400`); the working folder is known; the two are never compared.
- A prompt gets a pointer only when it names the system by title, slug or alias (`hook/pointers.go:262-279`, `pointers.go:495-547`).
- The paths a page cites are the most precise recall key the system has, and they are used only when the agent decides to run a search (`resume/systems.go:428-461`). There is no recall when the agent reads or edits a file under a cited path: the tool hooks are capture-only (`internal/install/templates.go:72-86`).
- A pointer delivers no content. The agent must spend a further tool call, possibly a permission prompt, and up to 24 KB of context to read the page; nothing selects the section that matters.
- A system page's `## Decisions and constraints` never reaches the brief; only knowledge pages' constraints do (`hook/pointers.go:168-170`).
- A review day that has passed is never shown in a hook's pointer (`hook/pointers.go:364-370`; `docs/design.md:356`).
- A page that names no repo is in no brief (`write.go:66-69`, KL:116).

Direction: index pages by the repo paths they cite and deliver bounded, fenced excerpts of the relevant sections when the chat's folder, its thread's files or a touched file falls under them; rank by path overlap and recency. Add; evolve the pointer code.

### systems-4. The staleness signal is weak, local and only ever grows (high, reliability-safety) — verified, mostly listed (KL:119-126)

- The measure is a count of commits that touch the cited paths between the cited commit and the local HEAD of one clone (`git.go:248-266`). It says nothing about whether the page is wrong (KL:119). A page citing a whole repo counts every commit.
- It never fetches (`coderef.go:7-8`), so "code unchanged" can mean the clone was never pulled. The daily pass passes no preferred clone (`cmd/eva-md/code.go:124`), so it measures in whichever worktree a hook last resolved with a change (`clones.go:55-64`, KL:124), which with agent worktrees can be a short-lived feature branch.
- The count only rises until someone re-digests, so on an active repo every pointer says `code moved N commits` indefinitely. That is recurring context cost for a signal nobody acts on, and it teaches the agent to ignore it.
- The wording joins two unrelated things: `code moved N commits since <as_of>` where N is counted from the cited commit and `as_of` is whatever day the agent declared (`coderef.go:209-217`, `frontmatter.go:225-242`).
- Nothing checks that the agent read the commit it cites. A digest in a dirty worktree describes uncommitted code and cites HEAD (`verify.go:38-127` checks only that the commit and paths exist). Inferred.
- A 7-digit commit that is ambiguous in a large repo reads as "commit not in the clone; fetch it" (`git.go:232-242`). Inferred.
- Document-only pages have no signal beyond a guessed date.

Direction: keep coderef's git layer; measure per cited path against a declared baseline (the remote-tracking default branch where the clone has it, and the chat's own worktree), classify the change, and send the result to the refresh queue instead of into every pointer. Fingerprint document sources at digest time. Evolve.

### systems-5. Nothing measures whether system pages are right or useful, and the path has never run live (high, evaluation) — verified; listed in `docs/verification.md`, not in known-limitations

- `docs/verification.md:148` and `:150`: the digest and recall skills triggering on each host, and "a real digest on each host citing code by folder at the commit it read", are not verified yet.
- The accuracy suite covers the background update's 12 fact scripts only (`internal/update/accuracy`; no mention of systems or the digest).
- Unmeasured: digest accuracy against the code it cites; whether a named system gets a pointer and whether the agent follows it; whether "code moved" predicts a wrong page; context tokens and tool calls a system page costs a chat; how often `system write` refuses and how many round trips a page takes.

Direction: an evaluation harness for system memory: fixture repos with seeded changes, claims scored against code, pointer precision and recall on scripted prompts, staleness detection against seeded edits, cost per chat; run live on each host. Add.

### systems-6. `map.md` is both a page for people and the machine index, and one page has four encodings (high, architecture) — verified; partly listed (KL:118, KL:127)

- Encodings of the same facts: the agent's Markdown, read by a hand-written YAML subset (`frontmatter.go:89-128`); the store's Concept and its serialization (`write.go:240-254`, `write.go:272-277`); a lenient second reader of the saved frontmatter for search (`saved.go:29-95`); and the map line, a third text format with its own writer and parser (`write.go:386-473`).
- The map line's grammar leaks into content rules: a title, description or alias may not hold ` · ` (`frontmatter.go:175-176`, `write.go:486-490`), and field order is fixed so that an older binary's parser still works (`write.go:381-385`).
- More readers of the same text: a regex in the doctor (`internal/health/background.go:513-516`), and the prompt hook parsing the brief's own output with a regex to learn which pages a brief named (`hook/pointers.go:163-187`).
- Derived files are committed to the wiki (`names.go:17-19`, `repos.go:9-12`). Only a system write rebuilds the map and folder index (`refresh` is private, `write.go:306`); the doctor repairs only the names and repos indexes (`names.go:169-202`); maintain rebuilds neither, though it does rebuild the knowledge pages' derived files daily (`cmd/eva-md/knowledge.go:17-48`, `cmd/eva-md/maintain.go:83-108`). After a hand edit, a hand deletion, an interrupted write or another machine's pages arriving by git, the map stays wrong until the next system write, and the brief reads only the map (`resume/systems.go:294-297`). The doctor's fix is to rewrite a page by hand (`background.go:598`).
- Two machines that each add a page both rewrite four sorted, derived files; only `log.md` has a merge rule (`internal/wikiinit/init.go:300`). Conflict on merge is inferred, not tested.

Direction: one typed page model with one parser; derived indexes built from the pages into local state by maintain and on write, never parsed back from prose; `map.md` rendered for people only; a damaged derived file is rebuilt, not refused. Rebuild the index layer.

### systems-7. Scale cliffs that fail silently (medium, reliability-safety) — verified, not listed

- **The 256 KB map.** No write bounds the map (`write.go:320-327`). Every reader refuses a map over 256 KB: the brief then shows no system pointer and no system index line at all (`resume/systems.go:65-68`, `systems.go:312-318`); prompt pointers lose their description and code note (`hook/pointers.go:330-342`, `pointers.go:273-274`); the code pass counts every page as "written before the map carried code" (`code.go:95-111`, `code.go:64-66`) and the doctor advises writing a page again (`internal/health/code.go:151-154`, `background.go:595-598`), which rebuilds the same oversized map. By the design's own figure of about 0.77 KB a page citing 6 sources (`docs/design.md:341`), the cliff is near 330 pages, and near 140 for pages citing 16 sources with full commit ids. Not in known-limitations.
- **The 64-pair pass.** Pages are measured in slug order (`code.go:72-90`, `status.go:196-198`) and the pass stops at 64 distinct repo-and-commit pairs (`status.go:313`). Each digest cites its own commit, so past about 64 code-citing pages the same pages, last by slug, are `not_run` every day, and their pointers silently carry no note. KL:131 lists the bound, not the starvation.
- **Names index.** Past 2,000 names or 128 KB the oldest pages lose every name and get no pointer (`names.go:101-116`, KL:139).

Direction: an index that degrades by dropping fields, not everything, and is looked up by key instead of read whole; measurement scheduled by priority with persisted progress. Rebuild with systems-6.

### systems-8. The lint only detects, covers only thread pages, and speaks only through the doctor (medium, invisibility) — verified, listed (KL:107, KL:207)

- Two checks, both on thread pages (`lint.go:371-390`): size over 512 KB and ref resolution. Rules, knowledge and system pages are not linted (KL:107).
- Nothing acts on the result. An oversize page is a doctor FAIL whose fix is `trim each page's Timeline by hand; automatic rotation is not built yet` (`internal/health/background.go:471`); a bad ref's fix is to restore inbox files or run `inbox clear` (`background.go:499-500`).
- The result is seen only if the person runs `eva-md doctor`.
- Three overlapping consistency checkers with three triggers and no shared model: `internal/lint` (daily), `internal/health` rows (on demand; the system map, names and repos checks live there, `background.go:518-602`, `health.go:325-364`), and the store's own validation (in the commit and the doctor, `internal/wikigit/commit.go:399-409`, `health.go:274-281`).
- It reads every session file in the inbox every day to count lines (`lint.go:169-186`), with no cache, though the inbox is kept without a time limit.
- Short refs are matched by the first 8 characters with `max` of the line counts (`lint.go:184-185`), so two sessions sharing a prefix can make a bad ref read as resolved. Inferred, low.

Direction: turn the lint into the maintenance planner: one consistency pass over every page type that repairs derived state itself and queues model work (timeline rotation, re-digest, contradiction review), and raises only what needs a person. Rebuild.

### systems-9. No lifecycle beyond write, no duplicate control, two unreconciled stores of constraints (medium, memory-quality) — verified, not listed

- The command has two verbs, `write` and `status` (`cmd/eva-md/system.go:18`). There is no remove, rename, merge or split. Removing a page is deleting a file by hand, which leaves the map, the index and other pages' links wrong (see systems-6).
- Nothing detects two pages for one system. A name that two pages share is listed for both (`names.go:73-81`, the `seen` set is per page; sorted at `names.go:121`), and both get pointers. Avoiding duplicates rests on the skill's "First read the map" (`digest-skill.md:10`).
- Eight fixed sections and 24 KB (`page.go:67`, `page.go:83`); "pages record names, not meaning" (KL:112).
- System pages hold `## Decisions and constraints`; knowledge pages, which the background update maintains from chats, hold constraints too. Neither store references the other (no `systems/` reference in `internal/knowledgepage` beyond sibling scope, `scope.go:10`). A decision changed in a chat updates the knowledge page and leaves the system page saying the opposite. The contradiction risk is inferred; the absence of any link is verified.

Direction: a stable identity per system with aliases resolving to one entity; rename, merge and retire with redirects; forward links allowed as unresolved; one constraint store, or system pages that reference knowledge entries rather than restating them. Add.

### systems-10. Stored text reaches the agent with uneven framing and no trust tier (medium, reliability-safety) — mechanisms verified, risk inferred

- A system page is written by an agent from arbitrary documents, URLs and repos. The checks stop structural forgery (`page.go:193-284`) but not instruction-shaped sentences.
- `eva-md show` prints the page on stdout "alone" so it can be written back (`cmd/eva-md/code.go:134-141`); there is no reference-data frame around it. Knowledge recall has one (`internal/hook/recall.go:38`), search has one (`internal/resume/search.go:51`), the per-prompt system pointer has none (`hook/pointers.go:372-409`): title and description go into the prompt's context as `eva.md: <title>: <description>`.
- A page built from an external web page is as trusted as one built from the project's own code: sources have no tier.
- A person's `verified` mark protects a page from being overwritten (`write.go:207-208`) but is not shown to a reader in any pointer.
- `show` prints the newest commit subject of a cited repo to the agent (`git.go:322-348`, `cmd/eva-md/code.go:241-243`); it is redacted and cut to 80 characters, but a third-party repo's commit author controls it.
- Digesting internal documents into a git repo that is pushed moves their content outside the documents' own access control; the only guard is a sentence in the skill (`digest-skill.md:15`) and the secret scan.

Direction: a trust tier per source and per page; every delivery of system text inside the same reference-data frame; instruction-shaped lines from external-tier sources screened or quoted; verified status shown. Evolve.

### systems-11. Authoring and staleness are tied to one machine, a shell and two hosts (medium, reach) — verified; clone discovery is listed (KL:124)

- Writing needs a shell and the plugin's skill. The MCP server is read-only (`README.md:204`), so a surface that has only MCP can read system pages and never create or refresh one.
- `generated.by` must start `claude-code/` or `codex/` (`frontmatter.go:217-221`): no other host, and no person, can be the stated author through the write.
- Clones are found only through the identity cache of chats on this machine (`clones.go:29-33`); a repo never chatted in here is "not checked", with the fix "chat once in each repo's clone" (`internal/health/code.go:137-139`).
- The code status is per machine and never shared (`status.go:20-23`), so every machine and teammate re-measures, and a machine without the clones knows nothing.
- A project matches a page by exact project key from the normalized remote (`resume/systems.go:100`, `coderef.go:150-185`). A fork or mirror has another key, so its chats get no pointer to the upstream's page. Inferred from the matching code.
- Process control is POSIX-only (`git.go:73-74`, `git.go:97-103`) and the wording says "this Mac" throughout (`verify.go:79`, `cmd/eva-md/code.go:207`).

Direction: authoring and refresh over a transport every surface has, with the host as open metadata; repo identity that tolerates forks and mirrors; staleness facts keyed by commit so they can be shared; platform-neutral process control. Evolve.

### systems-12. Code that exists to fit the page store, and a YAML reader written by hand (medium, architecture) — verified, not listed

Accommodations of the store (okf) inside `internal/systempage`:

- Every page ends with a link to the map only so the store finds no orphan (`page.go:78-79`); the body check, search and `show` each then special-case that line (`page.go:239-241`, `resume/systems.go:409`, KL:117).
- The store's link regex is copied so links inside code spans are judged as the store judges them (`page.go:96-98`, `page.go:323-327`), and its external-link rule is copied too (`page.go:361-365`).
- Four slugs are reserved for the store's files (`write.go:86-89`).
- `flowList` exists because the store writes a `[]string` back as raw lines under a key a person wrote as a block (`write.go:256-268`).
- A page the store can't parse is still listed, because the store counts an unlinked page as orphaned (`write.go:504-506`).
- `---` is refused in a title or description because the store refuses it there (`frontmatter.go:156-158`).
- `checkSaved` re-serializes the page as the store will, to scan what really lands (`page.go:159-188`); `MaxSaved` adds 1 KB for the lines the save adds (`code.go:22-24`).
- Two listings of the same pages: `index.md` in the store's format and `map.md` in eva-md's.

And because the module may have no YAML dependency (AGENTS.md, "Go and dependencies"), `frontmatter.go` is a 400-line YAML subset reader with its own quoting, flow-list and block-list rules, and `saved.go` a second, lenient one.

Direction: state the contract eva-md needs from a page store (typed fields in, validated page out, link policy, no orphan rule for generated pages) and keep the on-disk Markdown format; with structured input (systems-2) most of `frontmatter.go` and the accommodations go away. Evolve; remove the parts structured input makes unnecessary.

---

## 4. Where the complexity is essential and where it is accidental

Essential:

- Treating page text as untrusted before it reaches a prompt (`page.go:193-284`).
- Running read-only git safely in clones eva-md does not own: partial clones, repo hooks, timeouts, process groups (`git.go:59-195`).
- Honest semantics for "unchanged", off-branch commits and missing paths (`status.go:92-159`).
- Compare-and-swap against a person's edits (`write.go:195-238`).
- Bounded reads in hooks.

Accidental:

- The page to map-line to parser chain and everything that guards its separator (`write.go:378-490`).
- Three frontmatter readers for one file (`frontmatter.go`, `saved.go`, the store).
- Four measurers of the same thing with four budgets and the reconciliation between them (`verify.go`, `cmd/eva-md/code.go:142-163`, `status.go:191-201`, `resume/systems.go:119-146`).
- Three consistency checkers (lint, doctor rows, store validation).
- About 350 lines that word status lines for a person to read (`cmd/eva-md/code.go:179-362`, `internal/health/code.go:59-166`), which exist because the only remedy is a person reading the doctor and asking for a digest.
- The store accommodations in systems-12.

History agrees: of 8 commits to `internal/coderef`, 4 patch the killed-child tests (`e8cb9b1`, `594fb50`, `ef0fd5e`, `b9f97bf`), and `internal/systempage` gained 783 and 512 lines in two feature commits (`3f372cb`, `51b241c`) that each added another derived file or map field.

---

## 5. Keep, evolve or rebuild

- **Keep**: `internal/coderef/git.go` and `clones.go` (the hardened git runner and clone confirmation); the semantics in `status.go`; the text and secret checks in `page.go` and `source.go`; the compare-and-swap and instance guard in `write.go`.
- **Evolve**: `coderef` measurement (baseline, per-path, shareable); pointer selection; trust framing.
- **Rebuild**: the lifecycle (creation and refresh by the platform, not by a person asking); the write interface (structured, not Markdown through a YAML subset); the derived index layer (`map.md` as machine index, `names.tsv`, `repos.tsv`); the lint as a maintenance planner that repairs.
- **Add**: path-driven recall; lifecycle operations and identity; an evaluation harness.
- **Remove**: with the above, most of `frontmatter.go`, `saved.go`, `ParseMapLine` and its separator rules, and the status-wording code.

The core judgment: the safety work in this part is good and should survive intact; the product shape around it (a page a person must ask for, which then only reports its own decay) is the opposite of the stated goal and is where rebuilding pays.

---

## 6. Open questions

1. Is a model run for refreshing system pages acceptable on the user's subscription, beyond the per-chat update? `docs/decisions.md` rules out "a model call on every turn", not a queued background refresh, but the quota cost needs an explicit budget.
2. May the platform read remote-tracking refs it did not fetch (`origin/HEAD`) as a baseline, given it must never fetch?
3. Should derived indexes leave the wiki repo (local state, rebuilt on each machine), or stay committed for people browsing the repo on a git host?
4. Are system pages and knowledge pages one store with two views, or two stores with links? Today they are two with none.
5. Does the "no new dependency" rule hold for the redesign? It is the reason for the hand-written YAML reader.
6. Is a write tool on the MCP server acceptable, given the README's privacy promise that the server only reads (`README.md:204`)?
7. What is the intended scale: one developer's dozens of systems, or an organisation's hundreds in a shared wiki? The 256 KB map and 64-pair pass decide differently.
8. Should a person's `verified` mark be something the product asks for and shows, or stay a hand edit of frontmatter?

---

## Verification

A second reader tried to refute each finding against the code at commit `51b241c`. Read-only: no test, setup, update, maintain or commit was run. The files of the part were read in full, with `internal/resume/systems.go`, `internal/hook/pointers.go`, `internal/hook/recall.go`, `internal/hook/resume.go`, `cmd/eva-md/maintain.go`, `cmd/eva-md/knowledge.go`, `internal/health/background.go`, `internal/okfadapter/adapter.go`, `internal/knowledgepage/scope.go`, the two skills and the docs the findings cite.

None of the 12 is refuted. 7 are confirmed and 5 are partly right. I rate 8 of them lower than the audit did.

| Finding | Verdict | Audit's severity | Mine |
| --- | --- | --- | --- |
| systems-1 | confirmed | critical | high |
| systems-2 | partly | high | medium |
| systems-3 | partly | high | medium |
| systems-4 | confirmed | high | medium |
| systems-5 | confirmed | high | high |
| systems-6 | confirmed | high | medium |
| systems-7 | partly | medium | low |
| systems-8 | confirmed | medium | medium |
| systems-9 | confirmed | medium | medium |
| systems-10 | confirmed | medium | medium |
| systems-11 | partly | medium | low |
| systems-12 | partly | medium | low |

Citations checked out except one: the brief's pointer limit is KL:64, not KL:62 (KL:62 is the footer's cut).

### systems-1: confirmed, high

- No non-test file in `internal/update` mentions `systems/`, `systempage` or the map (grep). `cmd/eva-md/maintain.go:83-108` runs the knowledge, commit, lint, code status, prune and index steps; none writes a system page.
- `internal/health/code.go:162-164` is the only remedy for moved code, and it is a sentence for a person. `page.go:234-284` checks section order and links, nothing about a source per line. KL:113 lists the gap.
- High rather than critical: a page that cites code carries a staleness note on its pointers, on `show` and on search hits, so the decay is announced, and thread and knowledge memory, which answer "where did we stop?", do not depend on system pages. A page built from documents alone has only `stale_after`, which no hook pointer shows.

### systems-2: partly, medium

- Verified: the skill's steps (`digest-skill.md:10-13`), its frontmatter of name and description only, no permission rule anywhere in `internal/install`, and the map refusal (`write.go:281-293`).
- Correction: only the frontmatter is rebuilt from values (`frontmatter.go:46`). The body is saved line for line as written, less the blank lines around it and the map link (`page.go:165-166`, `page.go:276-283`, `write.go:253`).
- Not pre-approving commands is a stated stance, not an oversight: "eva-md never writes host settings" (`README.md:204`).
- The cost of refusals and round trips is unmeasured, as systems-5 itself says, and "no page until someone asks" is already systems-1's point. What is left for this finding is protocol friction, hence medium.

### systems-3: partly, medium

- Verified: the brief takes the first 3 of the project's systems, those citing its code first, then slug order (`resume/systems.go:96-108`, KL:64); a prompt pointer needs a name from the names index (`hook/pointers.go:262-279`); tool hooks are async and capture only (`install/templates.go:74-75`, `84`); the brief's constraints come from knowledge pages only (`resume/knowledge.go:68-84`).
- "Cited paths never drive recall" overstates. Search matches a path term against the folders and files a page cites (`resume/search.go:131-167`, `resume/systems.go:447-460`), and the recall skill tells the agent to search a file's path before changing code (`recall-skill.md:13`). That is path-driven recall the agent pulls.
- Reading a page need not cost 24 KB: `show systems/<slug>#<section>` reads one section, and a search hit's ref names the section.
- What is missing is automatic, path-ranked delivery.

### systems-4: confirmed, medium

- Verified: `git.go:248-266` counts `commit..HEAD` on the cited paths; nothing fetches; the daily pass sets no preferred clone (`cmd/eva-md/code.go:124`) so the newest cache entry goes first (`clones.go:55-64`, `identity/cache.go:594`); `coderef.go:212-217` joins a count from the cited commit with the page's `as_of`. The same wording is in search's label (`resume/search.go:51`).
- KL:66 and KL:119-126 list nearly all of it.
- The context cost is overstated: the note is about 40 characters in a pointer sent once a page a chat (KL:140), and the digest skill does act on it (`digest-skill.md:10`).
- Medium while the signal is only displayed. It becomes high the day a refresh queue is driven by it, since "unchanged" in an unpulled clone is false assurance.

### systems-5: confirmed, high

- `docs/verification.md:148` and `:150` list the skills triggering and a real digest citing code as not verified. `internal/update/accuracy` holds `scripts.go`, `measure.go`, a test and `live/`, all for the update's fact scripts. No test runs or scores a digest; the system page tests check refusals and derived files.

### systems-6: confirmed, medium

- Verified: the map line writer and parser (`write.go:386-473`), the separator ban (`frontmatter.go:175-176`), the private `refresh` (`write.go:306`), maintain's daily rebuild of the knowledge files (`cmd/eva-md/knowledge.go:27-48`) with no systems step, and the doctor repairing only the names and repos indexes (`names.go:169-202`). The doctor holds one more reader of the map, a regex (`health/background.go:513-516`).
- Nuance: `saved.go` is not a second parser. It calls `parseFront`, `list` and `unquote` from `frontmatter.go` and reads the fields leniently.
- Drift is detected, by the doctor's system map row (`background.go:518-602`), but repaired only by a system write.
- Medium: the verified harm is a stale map after a hand edit, an interrupted write or a merge. Merge conflicts are inferred.

### systems-7: partly, low

- The bounds are verified: `resume/systems.go:312-318`, `hook/pointers.go:330-342`, no bound in `write.go:320-327`, and the pair bound at `coderef/status.go:313`.
- "Fail silently" is wrong for the doctor. Its system map row FAILs with "missing, unreadable or over 256 KB" (`health/background.go:595`), and its system code row is UNKNOWN naming the pages "past the bounds of a pass" (`health/code.go:56`, `:136`). The brief and the pointers do degrade without a word.
- The real fault is the fixes: writing a page again rebuilds the same oversized map (`background.go:598`, `health/code.go:153`), and `eva-md system status` without slugs hits the same 64 pairs. `eva-md system status <slug>` does measure the named pages (`cmd/eva-md/code.go:83-87`), but the next daily pass overwrites the file whole (`code.go:126`).
- The bound is 64 distinct repo-and-commit pairs, not pages: pages digested at one commit share a pair.
- Low: both thresholds, 140 to 330 pages and more than 64 pairs, are far above one developer's wiki.

### systems-8: confirmed, medium

- Verified: `threadPages` lists only thread pages (`lint.go:371-390`); the hand-trim fix (`health/background.go:471`); the only reader of `lint.json` is `health/background.go:449-509`, beside one line in maintain's log (`maintain.go:98`); the store's validation runs in the commit (`wikigit/commit.go:399-409`).
- Nuance on the impact: okf's validation, in the commit and the doctor, does cover system pages' links, and a knowledge save refuses a duplicate statement (`knowledgepage/save.go:33`, `:244`). The lint itself checks neither. KL:107 and KL:207 list the gaps.

### systems-9: confirmed, medium

- Verified: two verbs (`cmd/eva-md/system.go:18`); names are deduplicated within a page only, and the hook keeps one name a page, so two pages sharing a name both get a pointer (`names.go:73-81`, `hook/pointers.go:517-527`); `internal/knowledgepage` names system pages only for sibling scope (`scope.go:10`), and `internal/systempage` names knowledge only in `repos.go:11`.
- One more consequence of hand deletion: the brief reads only the map and checks no file (`resume/systems.go:294-297`), so it keeps pointing at a page that is gone, while the prompt hook does check (`hook/pointers.go:322-325`).
- The contradiction between the two constraint stores is inferred, as the audit says.

### systems-10: confirmed, medium

- Verified: prompt pointers are appended to the context outside any fence or label (`hook/resume.go:81-86`, `hook/pointers.go:380-409`); CLI `show` prints the page alone (`cmd/eva-md/commands.go:133`); the brief's system lines sit inside its fence (`resume/resume.go:151-154`, `:697-701`), recall has its label (`hook/recall.go:38`), search has one (`resume/search.go:51`) and `eva_show` has one (`mcp/calls.go:26`). So the framing is uneven, and the uneven path is the one every pointer sends the agent down.
- No reader in `internal/resume`, `internal/hook` or `internal/mcp` uses the verified mark. Sources carry no tier.
- Mitigations the redesign should keep: the write refuses speaker labels, comments, fences and hidden characters, and the commit subject is redacted, flattened and cut. The injection risk itself is inferred.

### systems-11: partly, low

- The facts are verified: the author's host (`frontmatter.go:217-221`), the read-only server (`mcp/server.go:1`, `README.md:204`), clones from the identity cache (`clones.go:29-33`), the local status (`status.go:20-23`), exact key match (`resume/systems.go:100`).
- They are agreed limits rather than defects: hosts in scope are Claude Code and Codex (`docs/decisions.md:32-35`), eva-md runs only on macOS (AGENTS.md), and the MCP server reads only as a privacy promise.
- A second machine is not blind: pages, map and indexes reach it through git, and it measures its own clones daily. Only the measurement is local, which is right for a count against a local HEAD.
- The fork and mirror mismatch is inferred.

### systems-12: partly, low

- The accommodations are verified: the map link (`page.go:78-79`), the copied link rules (`page.go:96-98`, `:361-365`), `flowList` (`write.go:256-268`), the reserved slugs (`write.go:86-89`).
- "A 400-line YAML subset" overstates. About 150 lines of `frontmatter.go` read syntax (`parseFront`, `scalar`, `list`, `splitFlow`, `unquote` and the flow mapping in `generated`). The rest are value rules that structured input would keep: lengths, dates, the source rule, the separator, alias bounds. `saved.go` reuses that parser.
- "The largest source of write friction" is unmeasured.
- The hand-written reader follows from the two-dependency rule (AGENTS.md). okf already parses the same frontmatter (`okf.ParseConcept`, used at `okfadapter/adapter.go:137`), so the duplication is with the store, which supports the finding's direction.

### Missed

**systems-m1. `--replace` is a blind overwrite, and any chat can replace any system's page under an author it declares itself (reliability-safety, medium, verified, not listed).**

- The hash the save compares is the one `Write` itself read a moment earlier (`write.go:116`, `:232`), not the page the agent read. `eva-md system write` has no flag but `--replace` (`cmd/eva-md/system.go:38`). The audit's "must keep" item 4 credits this as a compare-and-swap against people; it protects only the few seconds of the write.
- `generated.by` comes from the page's own text (`frontmatter.go:186-222`), and `Options` carry no session (`write.go:44-54`), so no page names the chat that wrote it. The write works "from any folder" (`cmd/eva-md/system.go:20-22`) and pages are global to the wiki.
- So a chat in any project can discard a shared page without reading it, and two digests of one system lose one without a word. Until the daily commit, or always with `commits = false`, the replaced text is gone.
- The only guard is a hand-set verified or locked mark, which then freezes the page against every later digest (`write.go:207-210`) while the doctor's fix is still "digest again".
- Direction: make a replace conditional on the version the writer read; have the platform, not the page text, record the writing chat and host; keep the replaced version recoverable; replace the all-or-nothing lock with reviewed or per-section updates. This matters more if a write tool is added to the MCP server, as systems-2 and systems-11 propose.

**systems-m2. An agent-written `repos` list decides which projects share recalled knowledge, without a bound and across owners (reliability-safety, medium, verified, listed in part at KL:157).**

- `repos` has no bound on count and takes any remote (`frontmatter.go:286-305`); the repos of the code sources are added (`write.go:367-376`); every key of a page with two or more becomes a sibling of the others (`repos.go:28-69`, `:94-106`); a sibling skips the owner test that naming a project applies (`knowledgepage/scope.go:32-38`). The skill tells the agent to list "the repos that build or use the system" (`digest-skill.md:11`).
- So one digest that lists many repos makes each project's knowledge recallable in all the others' chats, across clients in one wiki, and no person confirms it. The audit lists sibling scope under "must keep" (item 15) without the risk.
- A redesign in which the platform writes pages or infers repos from code would change recall scope silently.
- Direction: separate "whose briefs point at this page" from "whose knowledge is shared"; make sharing an explicit, bounded, owner-aware relation that a person or the config confirms; say so when a page write changes it.
