# The memory model: thread pages, the project overview, rules pages and knowledge pages

Audit of eva-md's memory model. All paths are relative to `the repository`. Every claim about the code carries a file:line. Where I infer a consequence instead of reading it in code, I say so.

Read in full: `internal/threadpage/*.go` (non-test), `internal/knowledgepage/*.go` (non-test), `cmd/eva-md/knowledge.go`, `internal/resume/now.go`, `internal/resume/projectpage.go`. Read to follow the data flow: `internal/update/{schema,knowledge,rules,validate,wiki,pass}.go`, the page-context part of `internal/update/input.go`, `internal/resume/{knowledge,rules}.go`, parts of `internal/resume/{resume,read}.go`, `internal/hook/recall.go`, `internal/okfadapter/adapter.go:20-310`. Tests: read the names and the headers, enough to know what they cover. Docs: `docs/design.md:199-333` and `358-420`, `docs/known-limitations.md` "Knowledge pages and recall" (145-172), "Standing rules and pasted text" (209-219) and "Background update" (174-207).

Nothing was run except read-only shell commands (grep, wc, git log).

---

## 1. How it works

### 1.1 The four page families

| Family | File in the wiki | Who writes it | Unit of memory |
|---|---|---|---|
| Thread page | `projects/<slug>/threads/<branch>.md`, `general/threads/default.md` | the background update, through validated operations | one page per git branch: a Now (rewritten) and a Timeline (append-only) |
| Project overview | `projects/<slug>/now.md`, `general/now.md` | code, no model | one line per thread, plus a link to the rules page |
| Rules page | `projects/<slug>/rules.md`, `general/rules.md` | the background update | one line per standing rule, at most 40 |
| Knowledge page | `knowledge/k<N>.md`, plus `knowledge/map.md`, `knowledge/index.md`, `.eva-md/knowledge.tsv` | the background update; code builds the map and the indexes | one page per one-line statement |

### 1.2 Thread page (`internal/threadpage`)

Model: `Page{Title, Now, Timeline []Group, Other []string}`, `Now{Summary, LastChat, Done, Next, Open, Decisions}`, `Item{ID, Text, Tag, Refs, At, Code}` (`internal/threadpage/page.go:117-145`).

Caps (`internal/threadpage/page.go:99-107`): summary 600 characters, an item or Timeline text 300, last chat 400, Done 8 items, Next 10, Open questions 10, Decisions in force 40.

Line format: `- <label><text> [<tag>] <refs> <!-- i:<id> t:<RFC3339> -->` (`page.go:222-277`). The five tags are stated, accepted, suggested, observed, inferred (`page.go:160`). Per-item metadata (id and the cited prompt's time) lives in an HTML comment on the line.

`Render` (`page.go:168-176`) writes title, `## Now` between `<!-- eva:now:start -->` and `<!-- eva:now:end -->`, `## Timeline` with `### YYYY-MM-DD` groups, then any other `##` section byte for byte. `Parse` (`page.go:304-343`) is lenient inside the markers and strict outside: any text it cannot place returns `ErrDamaged` (`page.go:113-115`, `345`), and a damaged page is never written.

`Apply(p, u)` (`apply.go:85-116`) is pure:
- Unless `KeepNow`, a non-empty summary and last chat replace the old ones and Done is replaced wholesale (`apply.go:139-154`).
- Operations on existing ids run first, the first one per id only; then items without an id are numbered; then the adds (`apply.go:161-183`).
- New ids are `n<k>`, k one more than the largest on the page, retired lines included (`apply.go:91`, `458-484`).
- Retiring writes a Timeline line that keeps the tag, refs and id comment (`apply.go:276-323`, `380-382`). A superseded item stays in place under its id with the new text (`apply.go:305-319`).
- Past the Next and Open caps, assistant-tagged items move to the Timeline first, then the oldest (`apply.go:326-358`). A decision past 40 is kept under Open questions as an `eva-md: decision over cap, kept` line that never moves and does not count toward the Open cap (`apply.go:213-216`, `329-331`).
- Events are appended unless their text and refs already match a Timeline line (`apply.go:364-376`).
- Lines of a pass go under the update's date, in the last group when it has that date, else in a new group at the end (`apply.go:392-402`).

One escaper cleans all free text (`escape.go:35-94`): control and format characters are stripped, `<!--`/`-->` and fence runs are broken, bracketed speaker labels and tags become parenthesized, `](` is broken, a heading or `---` line is backslash-escaped, and a line that starts with a frontmatter key okf refuses, or one of eva-md's own labels, gets backticks.

`Scanner` (`scan.go:14-105`) reads a body line by line for search, never failing where `Parse` would.

`ThreadName` (`name.go:14-40`) maps a branch to a file name and adds a 6-hex hash when it changed the name. It does not bound the length (`name.go:11-13`).

### 1.3 What drives a thread page (the background update)

Triggers (`internal/hook/trigger.go:18-23`): a Stop with 60 KiB unprocessed, a prompt 30 minutes after the file last changed with 600 bytes pending, a chat start that finds sessions idle 2 minutes (at most 5), and chat end and compaction. Daily cap: 40 model calls per host (`internal/config/config.go:34`).

One pass (`internal/update/pass.go:138-203`, `379-454`):
1. Group the entries of one (session, branch) past the watermark; read the thread page (`update/wiki.go:220-254`), both rules pages (`wiki.go:423-431`) and the recall index (`pass.go:459-465`).
2. `Build` (`update/input.go:266-319`) cuts a batch of at most 60 KiB (`input.go:223`) and prepends page context (`input.go:747-781`): the branch and 5 commit subjects, every Now item with id, kind, tag and refs, the standing rules, at most 12 lines / 2,400 characters of knowledge that shares tokens with the batch (`input.go:230-231`, `864-907`), the last 5 Timeline lines, and for a new thread the parent's Now items "for context only". The page's summary and last chat are not shown to the model (`input.go:264-265`).
3. The model answers a strict JSON schema (`update/schema.go:38-133`): summary, last_chat, done (≤8), items (one action per Now id plus up to 40 adds, `schema.go:147-149`), timeline (≤20), unknowns (≤5), rules (≤10), knowledge (≤10, 5 kept).
4. `checkOutput` (`update/validate.go:95-118`) mends or refuses line by line: refs are resolved against the batch (`validate.go:245-286`), tags are downgraded to what the citations support (`validate.go:307-350`), a decision without the user's tag is filed as a question (`validate.go:454-460`), dates must appear in a cited entry (`validate.go:869-884`), and the output is rejected when more than half the page's ids got no action (`validate.go:924-925`). `unknowns` is only counted (`validate.go:112`); its text is discarded.
5. Saves run in the order knowledge, rules, thread (`pass.go:410-435`), each by compare-and-swap with up to 3 tries (`wiki.go:30`, `344-381`; `knowledgepage/save.go:39`). Then the overview and index files are rebuilt (`pass.go:443-444`, `wiki.go:605-641`), and last the watermark moves (`pass.go:449`).

A thread page that is locked, verified or damaged gets no model call: the pass is skipped and the watermark moves past the batch (`pass.go:196-199`, `120-121`, `612-623`).

The out-of-order guard: a session older than the page's `as_of` only appends Timeline lines (`validate.go:103`), and changes no rule and no knowledge (`update/rules.go:110-113`, `update/knowledge.go:92-95`).

### 1.4 How a thread page reaches a chat

The brief reads the page head up to the Now end marker, within 2 MiB (`internal/resume/read.go:647-674`), strips the hidden comments but keeps the refs (`internal/resume/now.go:11-26`), and shows the sections by priority: decisions, next, open questions, summary, last chat, done (`now.go:44`). Next keeps at least a quarter of the Now budget and open questions 14%; decisions take the rest, newest first by page order (`now.go:75-132`). A section that does not fit ends with `N more: <id>#Now` (`now.go:88`).

Budgets (`internal/resume/resume.go:186-202`): the Now gets 3,200 characters on Claude Code (cap 8,000 × 2800/7000) and 2,400 on Codex; 2,100 and 1,575 in a compact brief. A Now at its caps is roughly 20,000 characters (600 + 400 + 8×200 + 10×300 + 10×300 + 40×300), so a full Now shows at about one sixth.

The Timeline is never put into a chat's context. It is read only by `eva-md search` and `eva-md show` (`internal/resume/show.go:52-60`, `internal/resume/pages.go:256-259`; grep of `Timeline` in `internal/resume`, `internal/hook` and `internal/mcp` finds no other reader).

A new chat on a branch with neither an earlier chat nor a Now of its own borrows the Now of the most recently active other branch (`resume.go:124-141`, `160-165`; `internal/resume/lineage.go:16-30`). Once the branch has a page of its own, that stops.

### 1.5 Project overview

`RenderOverview` (`internal/threadpage/overview.go:31-73`) writes `## Rules` with a link to `rules.md` when it exists, then `## Threads`, one line per thread: `* [<branch>](threads/<file>.md) — <first sentence of the summary> · last active <date>`. Every thread is linked "because okf counts a page nothing links to as orphaned" (`overview.go:27-30`).

It is written by `writeOverview` after any pass that wrote a page (`update/wiki.go:685-727`), which first reads and parses every page in the area's threads folder (`wiki.go:653-680`). The overview is also the folder claim: `claimArea` creates it first with the project's remote so another project of the same name can't take the folder (`wiki.go:564-578`).

The brief reads the overview only for its `remote` value, to find the project's folder (`resume/read.go:613-635`). Its thread lines never reach a chat. `ProjectPage` (`internal/resume/projectpage.go:33-65`) counts decisions, next steps, open questions and rules for `eva_status`.

### 1.6 Rules pages

Model: `RulesPage{Title, Rules []Item, Other}` (`internal/threadpage/rules.go:42-46`), at most 40 rules (`rules.go:34`). Ids are `r<k>` on a project's page and `g<k>` on the general page (`rules.go:143-150`). `ApplyRules` (`rules.go:188-254`): an edit replaces text, tag, refs and time and moves the rule to the end; a drop moves it, struck through, to `## Retired rules` (`rules.go:259-276`); an add whose text is on the page, or past the cap, changes nothing.

A rule is accepted only when tagged stated and one cited prompt's own words hold every distinctive word of the rule with the same polarity (`update/rules.go:128-153`, `271-274`, `315-329`). It goes to the general page when that prompt names all projects or the chat is outside a project (`update/rules.go:132`, `40-41`, `70-73`). An edit or drop leaves a `change` line on the thread page (`update/rules.go:205-209`).

In a brief: the rules block comes first, at most 600 runes, all projects' rules before the project's, newest first, ending with `N more: <bin> show <page>` (`internal/resume/rules.go:19`, `82-121`). The briefed marker keeps a hash of the rules, so a changed rule re-shows the whole block (`resume/rules.go:123-137`, `resume/resume.go:206-228`).

### 1.7 Knowledge pages (`internal/knowledgepage`)

Model: `Entry{ID, Kind, Title, Statement, Tag, Scope, Project, State, AsOf, At, Refs, Sources, Code, Paths, History, Locked, Verified}` (`page.go:120-145`). Kinds: fact, decision, procedure, constraint (`page.go:61-66`). Scopes: project, all (`page.go:69-72`). States: active, retired (`page.go:76-79`). Bounds: title 80, statement 300, reason 300, 20 inbox refs, 3 code paths, 256 KiB per page (`page.go:82-90`).

The page is frontmatter plus `## Statement` (one line: statement, tag, refs, hidden time), `## History` (struck-through lines for supersedes and drops) and a closing link to the map (`page.go:303-331`). The statement is also the frontmatter description (`page.go:276`).

Validation before a save (`internal/update/knowledge.go`):
- At most 5 changes a pass; the rest are counted over the cap (`knowledge.go:17`, `112-115`, `168-170`).
- A decision or constraint needs the user's tag; a fact or procedure the user's tag or `observed`, and `observed` is refused when a cited entry is of a turn that read the web (`knowledge.go:72-80`).
- A statement under the user's tag must hold at least half its distinctive words in the cited prompts' own words (`knowledge.go:128`, `267`; `update/rules.go:278-281`).
- Every code-like token and number must appear somewhere in the batch (`knowledge.go:134`, `309-315`).
- Exact and near duplicates of knowledge that applies to the project are refused (`knowledge.go:130-133`, `274-292`).
- A supersede needs a cited user prompt at or after the entry's time; a drop needs such a prompt whose own words name the entry (`knowledge.go:176-203`, `212-214`).
- Scope `all` only when a cited prompt both holds the statement and names all projects, or the chat is outside a project (`knowledge.go:323-335`).
- Every line the save would write is run through the daily commit's secret scan first (`knowledge.go:404-409`, `knowledgepage/save.go:512-537`).

`Save` (`knowledgepage/save.go:97-119`) holds the wiki lock once, reads every page (`save.go:144-161`), applies the operations, then `refresh` reads every page again and rewrites the map, the folder index, the recall index and the root index line when they differ (`files.go:83-152`). New ids are one more than the largest `k<N>.md` in the folder (`save.go:150`, `166-186`).

The recall index `.eva-md/knowledge.tsv` (`index.go:21-29`) holds one 11-field tab-separated row per active page, newest `as_of` first, at most 512 KiB; past that, the oldest rows are left out (`index.go:97-115`). Readers open it by path through a pinned root, refuse links and a second hard link, and never list the folder (`index.go:186-254`).

Recall on a prompt (`recall.go:53-154`, `internal/hook/recall.go:91-117`): tokenize the prompt (lowercased runs of letters and digits, at least 3 characters, minus an English stoplist; `recall.go:175-197`, `249-271`), keep rows that hold at least 2 prompt terms and either every content token of their title or at least 3 terms, score by IDF with title terms doubled, keep those at or over the floor 2.5 (`recall.go:37`) that are in scope: the project's, all projects', siblings through a shared system page, or a project the prompt names (`scope.go:16-83`). The hook shows at most 3, in at most 1,000 characters with label and fence, each page once a chat (`hook/recall.go:31`, `131-163`).

In a brief: at most 3 constraints, newest first, in 600 runes, at startup and clear only (`internal/resume/knowledge.go:24-27`, `43-66`; `resume/resume.go:321-324`). No other kind of knowledge is in a brief.

Daily maintain and the doctor rebuild the derived files (`cmd/eva-md/knowledge.go:27-48`, `knowledgepage/files.go:60-70`).

---

## 2. What must be kept

These are the invariants and hard-won fixes that a redesign must carry over, whatever the storage becomes.

1. **Provenance on every item.** Each stored line carries a speaker tag and refs to the inbox entries that support it; the model never writes a ref or an id, the validator resolves them from entry numbers (`update/validate.go:245-296`, `update/schema.go:31`).
2. **The user's words never appear under the assistant's label, and the assistant's never as the user's** (S1): tags are downgraded to what the citations support (`validate.go:307-350`), a decision without the user's tag is a question (`validate.go:454-460`), suggestions show as `Proposed (assistant):` and inferences as `Unattributed:` (`threadpage/page.go:236-248`).
3. **Pasted text is never stated or accepted** (`validate.go:313`, `352-366`), and **a turn that read the web settles no observed knowledge** (`update/knowledge.go:72-80`).
4. **Only the user retires what the user owns.** A decision leaves the Now only superseded or dropped through the user (`validate.go:565-571`, `616-621`, `684-695`); a supersede, edit or drop needs a user prompt at or after the item's time, so an older chat never overrides a newer one (`validate.go:619-621`, `update/rules.go:185-186`, `update/knowledge.go:246-259`). The out-of-order guard protects the Now from an older session (`validate.go:103`).
5. **Caps never drop a decision** (`threadpage/apply.go:213-216`), and at a cap the assistant's items leave before the user's (`apply.go:350-360`).
6. **Ids are never reused**: `n<k>`, `r<k>`, `g<k>` count past retired lines (`apply.go:458-484`, `threadpage/rules.go:217-225`); `k<N>` counts past any `k<N>.md` entry (`knowledgepage/save.go:166-186`).
7. **Idempotent application.** Applying the same update twice changes nothing (`apply.go:66-78`, `threadpage/rules.go:184-187`, `knowledgepage/save.go:88-96`). This is what makes a retry after a failed save safe.
8. **One escaper, idempotent, for all stored free text** (`threadpage/escape.go:26-94`), reused by knowledge pages (`knowledgepage/page.go:575-581`). No stored text can forge a heading, fence, comment, link, label, tag or frontmatter key. Control, format and bidi characters are stripped.
9. **A person's text is never lost or rewritten.** Other sections are kept byte for byte (`threadpage/page.go:144`, `406-408`); a page eva-md can't read back safely is never written (`page.go:292-303`, `knowledgepage/page.go:171-195`); locked and verified pages are never written (`update/wiki.go:242-249`, `knowledgepage/save.go:213-216`).
10. **Round-trip properties, tested**: `Parse(Render(p)) == p` and `Render(Parse(b)) == b` (`threadpage/page.go:162-167`; `threadpage/property_test.go`, `FuzzParse`, `okf_test.go`).
11. **Bounded, link-safe hot-path reads.** The brief reads a thread page only to the Now end marker (`resume/read.go:647-650`); the hook and the brief read the recall index by path alone through a pinned root, refusing symlinks, a second hard link and a file over 512 KiB (`knowledgepage/index.go:212-254`). They never list a folder or parse a page.
12. **Derived files are caches.** The recall index, the map and the indexes are rebuilt from the pages; deleting them loses nothing (`knowledgepage/index.go:16-21`, `files.go:50-70`).
13. **Scope discipline in recall.** Another project's knowledge reaches a chat only as a sibling through a shared system page or when the prompt names it, and a bare name never crosses owners (`knowledgepage/scope.go:54-83`). `all` scope needs the user's own words (`update/knowledge.go:323-335`).
14. **Recalled text is fenced as reference data with what each tag rests on, at most 3 facts, each once a chat** (`hook/recall.go:38-40`, `60-85`, `105-109`).
15. **No change off the record.** Every knowledge or rule supersede and drop leaves a Timeline line (`update/knowledge.go:419-436`, `update/rules.go:205-209`), removed again when the save skipped the change (`update/knowledge.go:469-475`). History is kept on the knowledge page and in `## Retired rules`.
16. **Secrets**: model output is redacted again (`validate.go:999-1008`), and every line a knowledge save would write is checked by the commit's secret scan before saving (`update/knowledge.go:404-409`).
17. **Times keep their own offset and `as_of` only moves forward** (`threadpage/page.go:281-290`, `update/wiki.go:300-305`).
18. **The folder claim**: a project's first page write creates `now.md` with the remote under the same lock (`update/wiki.go:564-578`).

---

## 3. Which kinds of memory the model can and cannot hold

| Kind a coding agent needs | Where eva-md holds it | Verdict |
|---|---|---|
| Working state: where the task stands, what is next | The Now of the branch's thread page | Held well for one task on one branch. Not held per task: the unit is the branch (finding 1). Summary and Done are the last batch's view (finding 8). |
| What happened (episodic) | Timeline lines (≤20 a pass, ≤300 characters each), the inbox | Written but pull-only: never recalled unless the agent runs a search (finding 4). Never consolidated or trimmed (known). |
| What is true (semantic) | Knowledge `fact`, system pages | One 300-character line a fact; no links between facts; no confidence, no confirmation, no staleness (findings 3 and 5). |
| How to do things (procedural) | Knowledge `procedure` | One 300-character line. A multi-step procedure with commands does not fit (finding 5). |
| What the user wants | Rules pages (explicit, imperative, in the user's exact words); knowledge `constraint` | Explicit rules held well. Inferred preferences, repeated corrections and style cannot be held: anything not in the user's words is refused (finding 5). |
| What was decided and why | Decisions in force on the thread; knowledge `decision` | Thread decisions are branch-local and capped at 40. The durable form is rarely created, because the commonest pattern (assistant proposes, user says yes) is refused (finding 2). The reason for a decision has no field. |
| What failed and why | Timeline `problem` and `finding` lines; `(dropped: reason)` | No recallable form. The update's rules forbid promoting "this chat's progress" (`update/schema.go:28`). A later chat, above all on another branch, will repeat a failed approach (finding 5). |
| Who and what the entities are | System pages, written by a separate command | No entity model for people, services, environments or tickets; knowledge pages cannot link to each other (finding 5). |
| What changed over time | History on a knowledge page, struck-through Timeline lines, retired rules | Change is recorded, but there is no validity interval and nothing ages (finding 3). An edited rule leaves no history on the rules page (`threadpage/rules.go:203-210`). |

---

## 4. Findings, ranked by consequence for the goal

### pages-1. Working memory is keyed to the git branch, and nothing sits between a branch thread and a one-line fact

Lens: memory-quality. Severity: critical. Action: rebuild. Known: no (design.md 294 and 324 say thread states and folding are planned; known-limitations does not list it). Basis: verified in code.

Evidence:
- A thread's id is the branch name; every chat outside a git repo goes to the single page `general/threads/default` (`update/wiki.go:77-82`; `internal/identity/identity.go:74`).
- `state` is always written `active` (`update/wiki.go:313`); nothing reads git to mark a thread merged, idle or gone (grep for those states in `internal` finds only this line and `wikiinit`).
- A new thread sees its parent's Now items "for context only: they are not on this page" (`update/input.go:768-779`), and the parent is the most recently active other branch, not git ancestry (`resume/lineage.go:16-30`).
- The brief borrows another branch's Now only while the branch has no chat and no Now of its own (`resume/resume.go:124-141`). Other branches are otherwise listed by name and date only (`resume/lines.go:147-165`).
- The project overview holds no project state: one line per thread (`threadpage/overview.go:31-73`), read by the brief only for the remote (`resume/read.go:613-635`).
- Every pass that writes re-reads and parses every thread page of the project to rebuild the overview (`update/wiki.go:653-680`), and no thread is ever archived.

Impact: a decision made on a feature branch is invisible from the second chat on any other branch and after the merge. Trunk-based users and every chat outside a repo (the Claude Desktop case) share one Now: one 600-character summary, 10 next steps and 40 decisions for unrelated work. A repo with many short-lived branches accumulates thread pages forever, and each pass costs a parse of all of them.

Direction: make the project the home of what holds for the project (decisions, constraints, open questions, the state of each workstream) and make a thread a workstream with a lifecycle: started, active, merged into its parent, archived. The branch becomes an attribute a workstream can change, not its identity. On merge, the workstream's decisions fold into the project. Outside git, workstreams are keyed by folder or topic, not one `default`.

### pages-2. Durable knowledge is almost never created: promotion is single-shot, capped and gated on word overlap

Lens: memory-quality. Severity: critical. Action: rebuild. Known: yes (known-limitations 147-149, 171, 188). Basis: verified in code.

Evidence:
- A statement can cite only the entries of the one batch, at most 60 KiB (`update/input.go:223`; `update/validate.go:245-250`).
- A pass keeps 5 knowledge changes; the rest are counted and the watermark moves past them (`update/knowledge.go:17`, `112-115`; `update/pass.go:449`).
- A statement under the user's tag must hold half its distinctive words in the user's own prompt (`update/knowledge.go:128`, `267`; `update/rules.go:278-281`), so "yes, do that" after an assistant's proposal promotes nothing, although the same exchange is accepted as a thread decision (`update/validate.go:320-329`).
- A near duplicate is refused, not counted as support (`update/knowledge.go:132-133`).
- Nothing re-reads a Timeline or looks across threads: the only writer of knowledge is the per-batch pass (`knowledgepage/page.go:1-8`; consumers of `knowledgepage.Save` are `update/pass.go:468-473` alone).

Impact: the long-term store stays thin, so decisions and facts live only in branch threads (finding 1) and in the pull-only Timeline (finding 4). The commonest way a decision is made in agentic coding is the one the gate refuses.

Direction: separate noticing from promoting. A pass records candidates with their evidence; a consolidation stage, run when the machine is idle, reviews candidates, thread decisions and Timeline findings across batches and threads, merges support, and promotes. Acceptance provenance is carried from the thread item (the proposal's ref plus the user's accepting ref), not re-derived from word overlap. Nothing over a cap is dropped; it queues.

### pages-3. Knowledge has no lifecycle, and "observed" proves nothing about the statement

Lens: reliability-safety. Severity: high. Action: rebuild. Known: partly (known-limitations 150-151 cover MCP and web results; the rest is not listed). Basis: verified in code.

Evidence:
- `observed` is granted when the line cites any tool entry or the assistant's message after a tool (`update/validate.go:275-279`, `334-337`; `update/input.go:300-305`). No check ties the statement's words to the result; the word tests apply only to the user's tags (`update/knowledge.go:128`).
- A page has two states, active and retired (`knowledgepage/page.go:76-79`), and no field for support, last confirmation, contradiction or validity (`page.go:120-145`).
- A drop needs the user's own words naming the entry (`update/knowledge.go:177-203`). Nothing else retires a page.
- Pages cite code at a commit (`knowledgepage/save.go:427-444`), and the paths are written into the recall index (`knowledgepage/index.go:63-69`), but nothing reads them: recall never looks at `Row.Paths` (`knowledgepage/recall.go:53-154`), and the code-staleness check serves system pages only (no reference to knowledge in `cmd/eva-md/code.go` or `internal/coderef`).
- When the index passes 512 KiB the oldest `as_of` rows leave recall, whatever their use (`knowledgepage/index.go:97-115`).

Impact: an assistant's wrong claim made after any tool call, or text from a repo file or an MCP result, can become a `fact [observed]` that later chats are told "rests on a command, file or MCP result" (`hook/recall.go:50`). It stays until the user happens to contradict it in words that overlap half of it. Facts about code that has since changed are recalled with only a date to warn the agent.

Direction: give every item a lifecycle: support (how many independent entries, of which kinds), last confirmed, contradicted-by, a derived staleness from how far the cited code has moved (the `coderef` machinery exists), and states beyond active and retired (candidate, disputed, stale). Recall ranks and labels by these. A newer observation that contradicts an observed fact demotes it without waiting for the user. Observed needs evidence the result supports the claim, judged by the model with the result quoted, not by a citation alone.

### pages-4. Most stored memory never reaches the agent unless the agent asks: recall is keyed on the prompt's words only

Lens: recall-quality. Severity: high. Action: rebuild. Known: partly (no stemming 155, fixture tuning 156, 3 constraints 163, 600 characters of rules 67; the rest is not listed). Basis: verified in code.

Evidence:
- The Timeline is read only by search and show (section 1.4).
- A brief carries no knowledge but 3 constraints (`resume/knowledge.go:24-27`; `resume/resume.go:321-324`). Decisions, facts and procedures reach a chat only through prompt recall.
- Prompt recall needs 2 shared whole-word tokens and either the whole title or 3 tokens, with no stemming (`knowledgepage/recall.go:111`, `124`, `211-243`).
- The trigger is the user's prompt. Nothing recalls on what the agent is about to read, edit or run, although each row carries the code paths it is about (`knowledgepage/index.go:44`, never read in `recall.go`).
- A full Now shows at about one sixth (section 1.4), and decisions are ranked by insertion order, so a decision superseded yesterday keeps its old rank (`threadpage/apply.go:310-317`; `resume/now.go:86-87`).
- Rules get 600 runes of a possible 80 rules × 300 characters (`resume/rules.go:19`; `threadpage/rules.go:34`).

Impact: in agentic coding the user's prompt is often "fix the failing test", and the knowledge that matters is about the file the agent then opens. That knowledge is stored, indexed by path and never offered. What is offered is chosen by recency and word overlap, not by usefulness.

Direction: one retrieval layer over all kinds of memory, ranked for the current context: project, workstream, the files and commands in play, and the prompt. Path-triggered recall on tool use within a strict budget. A project digest at chat start chosen by utility (use, recency of confirmation, scope), not newest-N per kind. Keep the scope rules (must-keep 13) and the once-a-chat rule.

### pages-5. The taxonomy cannot hold procedures, failures, preferences or entities

Lens: memory-quality. Severity: high. Action: add. Known: no. Basis: verified in code.

Evidence:
- Four kinds (`knowledgepage/page.go:61-66`), each one line of at most 300 characters (`page.go:84`, `321-331`); line breaks are collapsed and fences broken (`threadpage/escape.go:35-37`, `82-84`).
- The update's rules forbid promoting "this chat's progress" and suggestions (`update/schema.go:28`); `problem` and `finding` exist only as Timeline kinds (`schema.go:81`).
- A rule needs every distinctive word in one prompt of the user's (`update/rules.go:271-274`), so nothing inferred from behaviour can be held.
- A page has no link to another page: the body is the statement, the history and the map link (`knowledgepage/page.go:303-317`).
- No field for the reason behind a decision, or for the context in which a fact holds (`page.go:120-145`).

Impact: "to release, run A, then B, then check C" does not fit. "We tried X and it failed because Y" has no recallable home, so later chats repeat it. A user who corrects the same thing three times has left no trace an agent will see. Nothing connects the facts about one service.

Direction: typed memory items with a one-line headline for recall and an optional structured body (steps, commands, cause, context). Add a lesson kind (what was tried, what happened, why), a preference kind with strength and evidence count, and entity pages (generalize system pages) that items link to. Decisions carry their reason and the alternatives rejected.

### pages-6. Markdown is the system of record for structured, model-written data

Lens: architecture. Severity: high. Action: rebuild. Known: no. Basis: verified in code.

Evidence:
- Per-item metadata is a line micro-format with an HTML comment (`threadpage/page.go:222-277`), because okf flattens nested frontmatter (design.md 257).
- That format is read by at least nine hand-written parsers in four packages: `threadpage.parseNow` and `parseItem` (`page.go:466-551`), `Scanner` and `timelineUnit` (`scan.go:45-140`), `lineTail` (`apply.go:433-446`), `ParseRules` (`rules.go:91-141`), `update.timelineTexts` (`update/validate.go:965-980`), `resume.parseNow` (`resume/now.go:49-73`), `resume.ruleLines` (`resume/rules.go:58-76`), `knowledgepage.parseBody` and `parseStatement` (`page.go:348-457`), `knowledgepage.Lenient` (`page.go:466-534`).
- A whole class of state exists only because the writable store is also the hand-editable view: damaged pages (`threadpage/page.go:113-115`; `knowledgepage/page.go:113-117`), a second lenient reader and read-only index rows for them (`knowledgepage/page.go:202-219`; `index.go:31-33`, `144-146`), numbering of hand-written items (`threadpage/apply.go:185-198`).
- The page code is 1,881 non-test lines in `threadpage` and 2,244 in `knowledgepage`, most of it parse, render, escape and damage handling.
- A locked, verified or damaged thread page makes every later batch on that branch skip with the watermark moved (`update/pass.go:196-199`, `612-623`). Those chats never reach memory, and nothing replays them after the page is repaired. Their rule and knowledge changes go too, since the skip comes before the model call.

Impact: every new field or kind means a new line format and more parsers. One stray line typed under `## Now` silently stops a branch's memory. Items cannot be queried, joined or merged across machines.

Direction: an append-only log of validated memory operations is the truth, stored in the wiki so git still carries it; pages are deterministic projections rendered from it (the renderer and the escaper are kept). A person's notes live in sections the projection preserves; a hand edit to a projected line is taken in as an explicit operation with the person as its source, or reported, never a reason to stop writing. A page that can't be written queues its operations.

### pages-7. One machine, one user, by construction

Lens: reach. Severity: high. Action: rebuild. Known: partly (known-limitations 223: eva-md never pulls; decisions.md 1: one wiki's memory never mixes with another's). Basis: inferred from verified parts.

Evidence:
- Ids are local counters: `n<k>` from the page (`threadpage/apply.go:91`, `224`), `r<k>`/`g<k>` (`threadpage/rules.go:217-250`), `k<N>` from the folder listing (`knowledgepage/save.go:150`, `248-250`). Two writers mint the same id.
- Every pass rewrites whole files: the thread page, the map, the folder index, the recall index (`update/wiki.go:354-358`; `knowledgepage/files.go:129-150`).
- The update refuses a wiki whose instance is not this machine's (`update/wiki.go:43-53`).
- Refs point into the machine-local inbox (`update/validate.go:291-296`), which the wiki's `.gitignore` excludes (design.md 209).

Impact: a second Mac, a cloud session or a teammate starts from nothing, and two machines can't share a wiki without id collisions and whole-file merge conflicts. Provenance refs resolve on one machine only.

Direction: globally unique ids (instance plus counter, or time-ordered random), one append-only operation log per instance merged by union, pages projected from the merged log, refs namespaced by instance. This falls out of finding 6's design; it does not fit the current one.

### pages-8. Nothing measures whether the memory helps

Lens: evaluation. Severity: high. Action: add. Known: partly (known-limitations 156 and 177). Basis: verified in code.

Evidence:
- The accuracy suite's metrics are about facts on a thread page: recall, updates, stale, tags, dates, carried, duplicates (`internal/update/accuracy/measure.go:61-77`). Its scripts and measures name no knowledge, rule or brief (grep).
- The seven knowledge histories run against canned outputs and check what the validator enforces (`internal/update/knowledge_accuracy_test.go:19-23`, `263-558`).
- The recall floor was "tuned on fixtures, not on real use" (`knowledgepage/recall.go:27-37`), 36 rows of two projects (known-limitations 156).
- No counter records whether a recalled fact or a briefed item was used, and the model's `unknowns` are discarded (`update/validate.go:112`).

Impact: promotion yield, the share of stale or wrong items, the precision of what is injected, tokens spent per chat and, above all, whether a later chat does better with memory than without are unknown. A redesign can't be steered or defended without them.

Direction: an end-to-end benchmark of multi-session coding scripts in which a later task is solvable only with what an earlier chat settled (across branches, across compaction, after a contradiction), scored with and without memory; plus component measures (promotion yield and precision, recall precision at the budget, staleness rate) and local counters of what was injected and what was then referred to.

### pages-9. The Now drifts: summary and Done are the last batch's, items show no age, and each pass pays for every item

Lens: memory-quality. Severity: medium. Action: evolve. Known: no. Basis: verified in code.

Evidence:
- Summary, last chat and Done are replaced each pass (`threadpage/apply.go:139-154`); the model is not shown the old summary (`update/input.go:264-265`) and sees one batch of at most 60 KiB (`input.go:223`). A long chat is several passes, and several chats share a branch.
- The brief strips each item's time (`resume/now.go:11-26`), so a three-month-old Next item reads like yesterday's.
- A Next item leaves only on a cited tool result, a drop or the cap (`update/validate.go:565-571`, `684-695`; `threadpage/apply.go:326-346`). Nothing checks it against the repo.
- Over-cap decisions pile up under Open questions without bound (`threadpage/apply.go:82-84`, `213-216`).
- The model is told to give every Now id an action, each an object of 8 required fields, and the run is rejected when more than half the ids get none (`update/schema.go:26`, `71`; `update/validate.go:924-925`), so output tokens grow with the page on every pass.

Impact: "where the thread stands" can describe the tail of the last chat. Stale next steps take a quarter of the Now budget and steer new chats. A page at its caps (60 or more items) costs, by my estimate, a few thousand output tokens a pass just to say "keep"; I did not measure it.

Direction: the summary is a maintained rollup the model updates from the previous one. Items carry freshness that ranking and display use, and unconfirmed next steps are demoted against git activity. The model reports changes only; silence means keep, with a periodic full review instead of one on every pass.

### pages-10. The okf conformance tax inside the model

Lens: architecture. Severity: medium. Action: remove. Known: no. Basis: verified in code.

Evidence:
- The orphan rule: every knowledge page ends with a link to the map (`knowledgepage/page.go:92-93`), the map lists every page, retired and unreadable too (`files.go:225-234`), and the overview links every thread forever (`threadpage/overview.go:27-30`).
- The drift check: the statement is duplicated in the description and again in the folder index (`knowledgepage/page.go:36-37`, `276`; `files.go:279-298`).
- Refused text: `---` becomes `--` in a title or statement (`page.go:575-581`), a line starting with `type:`, `status:` and five more keys gets backticks (`threadpage/escape.go:18-21`, `93`), and a body okf would refuse makes a page damaged (`knowledgepage/page.go:186-200`).
- Reserved names `index`, `log`, `AGENTS` (`threadpage/name.go:33`); `knowledge/` is the folder okf falls back to as the wiki root (design.md 231).
- Each knowledge save reads every page twice and rebuilds four derived files under the wiki lock (`knowledgepage/save.go:144-161`; `files.go:107-151`).

Impact: three listings of the same pages, altered text, and a damaged state exist to pass a validator that adds no memory value. Cost per save grows with the number of pages.

Direction: own a small page codec and the few checks eva-md needs; treat okf-compatible output as an optional export, checked off the write path. Decide this together with finding 6.

### pages-11. English word lists decide provenance, scope, duplicates and recall

Lens: reach. Severity: medium. Action: rebuild. Known: partly (known-limitations 155, 214: words, not meaning; the language limit is not listed). Basis: verified in code, consequence inferred.

Evidence:
- English stoplists, negations and contractions (`update/tokens.go:34-47`, `75-77`; `update/rules.go:303-304`; `knowledgepage/recall.go:249-271`).
- Fixed English phrases set scope: "all my projects", "everywhere", "only this repo" (`update/rules.go:40-41`; `update/knowledge.go:339-342`).
- Tokens are runs of letters split at non-letters (`update/tokens.go:55-67`; `knowledgepage/recall.go:211-243`), so text without spaces between words becomes one token per clause.
- Word form is prefix matching (`update/tokens.go:25-30`).

Impact: for a user who writes in another language, rules and knowledge are refused or scoped wrongly, and recall does not fire. For English users the same tests refuse faithful paraphrases.

Direction: the model judges meaning (does the user's prompt entail this rule, is this a duplicate, what scope did the user give), returning the quoted span as evidence; code checks structure, refs and that the span is in the cited prompt. Retrieval uses language-neutral matching.

### pages-12. A third of what is injected is scaffolding, and several fixes are by hand

Lens: invisibility. Severity: medium. Action: evolve. Known: partly (Timeline trimming by hand: known-limitations 207). Basis: verified in code.

Evidence:
- Every Now and rule line in a brief carries its tag and up to 5 refs (`resume/now.go:15-17`; `update/schema.go:172`), about 23 characters for one ref.
- The recall block's label is about 310 to 390 characters of its 1,000 (`hook/recall.go:31`, `38-40`, `60-85`, `159`), and each line starts with about 66 characters of id, kind, tag, project key and date (`hook/recall.go:170-184`). One statement at the 300-character bound and one more are all that fit.
- What does not fit is a shell command for the agent to run: `N more: ...#Now`, `N more: <bin> show general/rules` (`resume/now.go:88`; `resume/rules.go:107-115`).
- The doctor's fix for a thread page over 512 KiB is "trim each page's Timeline by hand; automatic rotation is not built yet" (`internal/health/background.go:471`; `internal/lint/lint.go:31`).
- A damaged page waits for a person to repair it (finding 6).

Impact: tokens and attention go to provenance marks the agent rarely needs in line, and the user is asked to maintain files.

Direction: provenance on demand: a short stable handle per item and one line saying how to expand it, with tags shown only where they change how to act (a suggestion, a stale item). Timelines roll into dated archives automatically. No fix asks for a hand edit.

---

## 5. Where the complexity is essential and where it is accidental

Essential:
- Provenance and the rules about who may change what (must-keep 1-5). This is what makes the memory trustworthy, and it is the hardest-won part.
- Idempotent, operation-based writes (must-keep 7).
- The escaper (must-keep 8).
- Scope rules for recall across projects (must-keep 13).
- Bounded, link-safe reads on the hot path (must-keep 11).

Accidental:
- The line micro-format and its nine parsers, the damaged-page class and the lenient second readers (finding 6).
- The okf-driven listings, links and text changes (finding 10).
- Word-overlap heuristics standing in for judgments of meaning: `usersWords`, `inOwnWords`, `samePolarity`, `narrows`, `everywhere`, `nearKnowledge`, and the project-naming rules in `scope.go` (finding 11). `scope.go` and the recall changes were the heaviest patching in this area: +461 lines in commit 51b241c on 2026-11-07.
- Per-kind caps and budgets tuned separately (Now 3,200, rules 600, constraints 600, recall 1,000) in place of one ranked budget (finding 4).

---

## 6. Keep, evolve or rebuild

Rebuild the model and its storage; keep the semantics.

The code in `threadpage` and `knowledgepage` is careful and well tested, and I found no correctness bug in it. The limits are in the model: the branch as the only unit of working memory, one-line facts with no lifecycle, single-shot promotion, and Markdown as the database. More patches to the line formats will not remove any of those.

Keep: the operation vocabulary and its validation rules, the tags and refs, the escaper, `Render`, the scope rules, the bounded-read discipline, the property and fuzz tests as the specification of behaviour.

Rebuild: a typed item store (an operation log in the wiki) with pages as projections; a project layer above workstreams; item lifecycle; a consolidation stage; one retrieval layer.

Add: the missing kinds (lesson, preference, entity, structured procedure) and the end-to-end evaluation.

Remove: the okf-only listings and text changes, once the write path no longer needs okf's validator.

---

## 7. What the tests cover and do not

Covered:
- `threadpage`: every action of `Apply`, tag changes, ids never reused, cap order, the over-cap decision, `KeepNow`, Timeline grouping, numbering of hand-written items, idempotence (`apply_test.go`); the escaper's cases and idempotence (`escape_test.go`); round trips, damaged bodies, `FuzzParse` and generated-page properties (`page_test.go`, `property_test.go`); round trips through okf itself (`okf_test.go`); rules pages (`rules_test.go`); the scanner against `Parse`, with `FuzzScanner` (`scan_test.go`); the overview (`overview_test.go`).
- `knowledgepage`: saves, idempotence, history, ids, pages left alone, hostile text, code sources, refresh and repair (`save_test.go`, `fixes_test.go`); the index's bounds, order and strict read (`index_test.go`); recall precision on fixtures, score stability by index size, the recall rules, project naming and scope (`recall_test.go`).

Not covered:
- Any behaviour over time on real use: a page after hundreds of passes, a Now at its caps in a brief, an index near 512 KiB with real statements.
- Recall on real prompts; text in languages other than English.
- Whether a saved item was right, or was ever useful to a later chat (finding 8).
- Several chats or several chunks overwriting one summary (finding 9).
- A thread page repaired after being damaged: nothing checks that skipped chats are recovered, because they are not (finding 6).

---

## 8. Open questions

1. Is a hand-editable Markdown page a product commitment, or is "inspectable as plain Markdown and git history" (decisions.md 2) enough? The answer decides whether an operation log can be the truth with pages as projections.
2. Must the wiki stay a conformant okf bundle for other okf tools, or is okf heritage that the write path can leave?
3. Should one person's memory follow them across machines and cloud sessions, and should a team share project memory? decisions.md 1 says one wiki's memory never mixes with another's.
4. What is the unit of work outside git, for Claude Desktop chats: a folder, a host project, a topic?
5. May the agent write memory during a chat (an explicit "remember this" tool), or must every write come from the background pass? Today the MCP server is read-only.
6. May eva-md retire or demote knowledge without the user's own words, when a newer observation or changed code contradicts it?
7. How much model quota a day may memory spend? A consolidation stage costs calls beyond the 40-a-day cap per host.
8. Is a local model or a new dependency acceptable for retrieval by meaning? AGENTS.md allows two dependencies and no network in hooks.
9. What happens to memory when a repo's remote is renamed or a local repo's folder moves? The project key comes from the remote or the root path (`internal/identity/identity.go:148-153`), so I infer the old pages and project-scoped knowledge are orphaned; I did not trace it further.

---

## Verification

A second reader tried to refute each finding against the code at commit 51b241c. Nothing was run but read-only shell commands (grep, sed, wc, git log). Paths are relative to `the repository`. A verdict is confirmed, partly (the core holds; scope, cause or severity is off) or refuted. The severity is the verifier's own.

| Finding | Verdict | Auditor's severity | Verifier's severity |
|---|---|---|---|
| pages-1 | confirmed | critical | critical |
| pages-2 | partly | critical | high |
| pages-3 | partly | high | high |
| pages-4 | confirmed | high | high |
| pages-5 | partly | high | medium |
| pages-6 | confirmed | high | high |
| pages-7 | partly | high | medium |
| pages-8 | confirmed | high | high |
| pages-9 | confirmed | medium | medium |
| pages-10 | partly | medium | low |
| pages-11 | partly | medium | medium |
| pages-12 | partly | medium | low |

No finding was refuted outright. Five carry a correction a builder should read before acting on them: pages-2, pages-3, pages-5, pages-7 and pages-11.

### pages-1: confirmed, critical

- `internal/update/wiki.go:76-82`: a thread's id is the branch, and the general area has the one page `general/threads/default`. `internal/identity/identity.go:74-76, 99-104`: every folder outside a git repo resolves to the general area with branch `default`.
- `wiki.go:313`: `stamp` always writes `state: active`. `docs/design.md:294` calls idle, merged and gone planned.
- `internal/update/input.go:768-779`: the parent's Now is shown to the model "for context only". `internal/resume/resume.go:124-141`: lineage runs only when no chat was chosen and the branch has no Now. `internal/resume/lines.go:147-165`: other branches are a name and a date.
- `wiki.go:653-680`: `threadPages` reads and parses every page of the folder on each pass that wrote.
- One qualification, which does not change the verdict: rules pages and knowledge pages are project-wide, so a decision is invisible on other branches only when it was not promoted. pages-2 is why that is the usual case for accepted decisions.
- The common flow this breaks: finish a feature branch, merge, open a chat on main. main has its own page and its own earlier chats, so the brief shows main's old Now and the feature branch only as a line under "Other branches, last active".

### pages-2: partly, high

Confirmed in code:
- `internal/update/knowledge.go:17, 112-115, 168-170`: 5 changes a pass, the rest counted `over_cap`; `pass.go:449` then moves the watermark.
- `knowledge.go:128, 267` with `internal/update/rules.go:278-281`: a statement under stated or accepted must hold half its distinctive words in the cited prompts' own words. `validate.go:227-235, 260-270`: `own` leaves out pasted text and the proposal, so "yes, do that" promotes nothing.
- `knowledge.go:132`: a near duplicate is refused and counted, never added as support.
- `pass.go:468-473` is the only caller of `knowledgepage.Save`.
- All of it is listed in `docs/known-limitations.md:147-149, 171`.

What is off:
- "Almost never created" is a prediction, not a measurement. `docs/known-limitations.md:176` says no live update has run yet, so no promotion yield exists.
- The words gate applies only to the user's tags. A fact or procedure tagged observed skips it (`knowledge.go:128`: `userTag(tag) && !usersWords(...)`), and needs a cited tool entry, grounding of its code-like tokens and numbers (`knowledge.go:134`) and no web turn. So tool-grounded facts and procedures can be promoted freely; what the gate refuses is the accepted decision or constraint, which no other tag can carry (`knowledge.go:72-80`).
- The cap is 5 a pass, and a long chat is several passes, so the cap is a lesser cause than the gate.

### pages-3: partly, high

Confirmed in code:
- `internal/update/validate.go:334-337`: observed needs only `c.result`, a cited tool entry or an assistant message after a tool.
- `internal/knowledgepage/page.go:76-79, 120-145`: two states and no support, confirmation or validity field.
- `internal/knowledgepage/index.go:63-69`: paths are written to the index. Nothing in `recall.go` reads `Row.Paths`; the only reader outside the page model is `internal/update/knowledge.go:416`, which rebuilds an entry for the secret scan. `cmd/eva-md/code.go` and `internal/coderef` have no reference to knowledge.

Corrections:
- Grounding does apply to an observed statement: every code-like token and number in it must appear in the batch (`knowledge.go:134`, `validate.go:889-910`). It is checked against the whole batch, not the cited result, so the finding's point stands for the prose of a claim.
- "It stays until the user contradicts it in overlapping words" is wrong. That is the rule for a drop (`knowledge.go:176-203`) and for a supersede under the user's tag. A supersede tagged observed skips the words test and needs only any cited user prompt at or after the entry's time (`knowledge.go:212-218`). So a newer observation can already replace an observed fact. The same path also replaces what the user stated, which is the first missed finding below.

### pages-4: confirmed, high

- `internal/knowledgepage/recall.go:53-154`: the score is built from `promptTerms(prompt)` against each row's title and statement; `Row.Paths` is never read. `internal/install/templates.go:72-78`: recall and pointers ride only with UserPromptSubmit (`internal/hook/pointers.go:95-112`).
- `internal/resume/knowledge.go:24-27`: 3 constraints in 600 runes; `internal/resume/resume.go:321-324` leaves them out of a compact brief.
- The Timeline has no reader in `internal/hook` or `internal/mcp`; in `internal/resume` only search (`pages.go:256-259`) and show (`show.go:52-60`) read it. The brief's footer names `#Timeline --last 10` for the agent to pull (`lines.go:299-308`).
- `resume.go:186-202`: 8000 × 2800 / 7000 = 3,200 on Claude Code, 2,400 on Codex. "One sixth" is the worst case of a Now at every cap (about 20,600 characters before tags and refs), not a typical page.
- The rules block is 600 runes for up to 80 rules (`internal/resume/rules.go:19`), general rules first, so that budget is the same kind of problem.

### pages-5: partly, medium

Confirmed in code:
- `internal/knowledgepage/page.go:61-66, 84`: four kinds, one statement of 300 characters; `internal/threadpage/escape.go:35-37` collapses line breaks. The model has no way to write a body (`page.go:303-317`).
- `internal/update/schema.go:28`: "never the assistant's suggestions, pasted text or this chat's progress". `schema.go:81`: `problem` and `finding` are Timeline kinds.
- `internal/update/rules.go:268-274`: a rule needs every distinctive word in one prompt, so nothing inferred from behaviour is held.
- `page.go:120-145`: no field for a decision's reason or a fact's context.

What the title overstates:
- A `procedure` kind exists. What does not fit is a procedure of several steps.
- A failure is not without a home: "X fails because Y" can be a `fact [observed]`, and nothing in the validator refuses it. What works against it is the instruction at `schema.go:28` and the six-month bar, and the missing kind.
- Entities exist for systems. `internal/systempage/page.go:1-40`: one page per service, repo or platform, with Summary, Interfaces, Depends on, Used by and Decisions and constraints, links between pages, a names index a prompt is matched against (`internal/hook/pointers.go:26-33`) and code staleness. Missing are people, environments and tickets, and any link from a knowledge page to a system page.
- A person can add sections to a knowledge page and they are kept (`page.go:142`, `348-419`); only the model cannot.

### pages-6: confirmed, high

- `internal/threadpage/page.go:222-277`: the item line with its HTML comment. The nine parsers listed are there; `internal/resume/show.go:169-200` is a tenth reader of section ends.
- `internal/update/pass.go:196-199`: a thread page with a skip reason is planned as a skip, before any model call. `pass.go:612-623`: `skip` advances the watermark and records `skipped (<reason>)`. No code replays the batch. `docs/design.md:605, 836, 964` state this as the design.
- "Silently" holds, and more firmly than the report says. `docs/design.md:318` promises "A notice explains the fix". No such notice exists: `internal/health/health.go:53-119` and `internal/health/background.go:407-447` have no case for a skipped pass, the doctor's update row stays PASS and shows the reason only while it is the last record (`background.go:89-137`), and the lint reads pages through the lenient scanner. The brief keeps showing the old Now, marked stale.
- One correction of scope: `Parse` is lenient inside the markers (`page.go:291-302, 466-498`), so a stray line typed among the Now's items is read as an item or as summary text and damages nothing. What damages a page is a non-blank line in `## Now` outside the markers, a missing, repeated or shared marker, an unknown `###` heading inside them, text before the first section, or Timeline lines before the first date (`page.go:304-343, 380-400`). A page a person locks on purpose stops the branch's memory the same way, rules and knowledge included.

### pages-7: partly, medium

Confirmed in code:
- Ids are local counters: `internal/threadpage/apply.go:91, 458-484`, `internal/threadpage/rules.go:217-250`, `internal/knowledgepage/save.go:150, 166-186`.
- Refs are `s:<id8>#<line>` into the local inbox (`internal/update/validate.go:286-296`).
- `docs/known-limitations.md:223`: eva-md never pulls, rebases or forces.

Corrections:
- "A second Mac starts from nothing" is wrong for a Mac that replaces another. Setup offers a wiki another Mac set up ("One wiki per machine: use it here only if <Mac> no longer pushes to it", `cmd/eva-md/setup.go:755-756`), and init takes the wiki's instance into the config (`internal/wikiinit/init.go:178-183, 256`). Pages, rules and knowledge come along. The lint counts the old Mac's refs as `elsewhere_refs`, not as bad (`internal/lint/lint.go:44-47`).
- `internal/update/wiki.go:33-53` compares the config's instance with the wiki's. It does not stop two Macs that both adopted one wiki; what stops them is the push, which ends as `diverged`.
- One machine per wiki is a recorded decision (`docs/decisions.md` decision 1, and decision 3 leaves cloud sessions out of v1), not an oversight. What is true by construction is that two writers at once cannot share a wiki. Whether that is high depends on a product choice the report itself lists as open question 3.

### pages-8: confirmed, high

- `internal/update/accuracy/measure.go:61-77`: every metric is about facts on a thread page.
- `internal/update/knowledge_accuracy_test.go:19-23`: the knowledge histories save "the correct one" of canned outputs.
- `internal/knowledgepage/recall.go:27-37`: the floor was "tuned on fixtures, not on real use".
- `internal/hook/pointers.go:54-59`: a chat's state counts pointers and recalled facts sent. Nothing aggregates it and nothing records use.
- One thing the report does not mention: `docs/design.md:1297-1301` specifies a live resume test, run by hand, with the update off and on. It plants four facts in one chat on one branch. It has not been run (`docs/verification.md:145`), is not automated (`design.md:1353`), and covers no knowledge, rule, recall or second branch. The finding stands.

### pages-9: confirmed, medium

- `internal/threadpage/apply.go:139-154`: a non-empty summary and last chat replace the old ones; Done is replaced always, by an empty list too.
- `internal/update/input.go:264-265, 746-781`: the page context holds the Now items, rules, knowledge and the last 5 Timeline lines, never the summary.
- `internal/resume/now.go:11-26`: hidden comments, the item times among them, are stripped. The page's own `as_of` is shown in the Now's heading with a stale mark (`internal/resume/lines.go:72-87`), so age is shown for the page, not for an item.
- `internal/update/schema.go:26, 62-71` and `internal/update/validate.go:915-927`: one action per id, eight required fields, rejection when more than half the ids have none. A keep is about 35 output tokens, so 60 items are about 2,000.

### pages-10: partly, low

- The facts hold: the map link on every page (`internal/knowledgepage/page.go:92-93`), the statement as the description (`page.go:276`), `---` made `--` (`page.go:575-581`), the map and folder index built for okf's orphan and drift checks (`files.go:225-298`), and every page read once in `load` (`save.go:144-161`) and again in `refresh` (`files.go:107-125`) under the wiki lock.
- The cost is small. The save runs in the background job, never in a hook, and a page is a few hundred bytes.
- "A validator that adds no memory value" overreaches. The map is also a reader's listing: it is where constraints past the brief's 3 are found (`docs/known-limitations.md:163`).
- Removing okf conformance reverses a standing rule (AGENTS.md: okf stays pinned, build what it lacks inside eva-md). That is a product decision to take with pages-6, not a defect in this code.

### pages-11: partly, medium

Confirmed in code:
- English stoplist, contractions and negations: `internal/update/tokens.go:34-47, 75-77`, `internal/update/rules.go:303-304`, `internal/knowledgepage/recall.go:249-271`.
- Fixed English scope phrases: `rules.go:40-41`, `internal/update/knowledge.go:339-342`.
- A token is a run of letters (`tokens.go:55-67`, `recall.go:211-243`), so Chinese or Japanese text is one token a clause. For those languages the user's-words tests fail and recall does not fire.

What is off:
- For a language written with spaces the claim is too strong. Words still match words: a rule kept in the user's words passes `inOwnWords`, a statement in the user's language passes `mentions` (a substring test, `rules.go:278-299`), and recall fires on shared content words, with function words weighed down by their frequency.
- The real loss there is different and not in the finding: the polarity check finds no negation (`rules.go:303-304, 315-334`), so a rule that drops the user's "never" is accepted, and no phrase can make a rule or a fact one for all projects.
- The English-paraphrase half is listed in `docs/known-limitations.md:155, 214, 219`.

### pages-12: partly, low

- The sizes hold, with one note. The label is 237 characters with one tag named and 313 with two; "310 to 390" is with the "Read one" sentence, which is added only when it fits (`internal/hook/recall.go:159-162`). A line's prefix is 66 characters for a GitHub project key and 51 for all projects.
- `internal/resume/now.go:15-17`: refs stay. `internal/health/background.go:471`: trimming by hand, listed in `docs/known-limitations.md:207`.
- The report contradicts itself here: its own must-keep 14 says recalled text is fenced "with what each tag rests on". The label is that safeguard. In absolute terms it is about 80 to 100 tokens on a prompt where recall fires.
- "The user is asked to repair files by hand" is too kind for a damaged page: the user is not asked at all (see pages-6).

### Missed by the audit

Each was checked in the code.

**missed-1. An assistant's observation can supersede knowledge the user stated, and the change is logged as `[stated]`.** Lens: reliability-safety. Severity: high. Known: no.
- `internal/update/knowledge.go:157-237`: `changeKnowledge` never reads the entry's own tag or kind. With the new text tagged observed, the words test is skipped (`knowledge.go:218`: `userTag(tag) && !usersWords(text, fresh)`), and the supersede needs only a cited tool entry or report and any cited user prompt at or after the entry's time (`knowledge.go:212`). `promotable` judges the op's kind, so a decision or constraint can be superseded as a fact (`knowledge.go:72-80`; `internal/knowledgepage/save.go:369-371` then changes the kind).
- `knowledge.go:433-436`: the Timeline line of every kept supersede is written with `Tag: "stated"`.
- The repository's own test asserts it: `internal/update/knowledge_test.go:240-241, 247-251, 266`. k3 is a fact tagged stated, "The runner pool is fixed". An op tagged observed that cites the prompt "And the notes?" and an MCP tool's report supersedes it, and the test expects `Superseded: 1`.
- The thread page has the guard knowledge lacks: `internal/update/validate.go:616-618` refuses a supersede of a decision, or of an item the user owns, without the user's tag.
- The update's rules tell the model to do this: "When a newer entry contradicts knowledge on file, supersede it" (`internal/update/schema.go:28`), and a tool result is an entry.
- Impact: a file's content, a command's output or an MCP result, or text injected through one, can replace what the user stated; a constraint turned into a fact leaves the brief. The thread's Timeline then says the user stated the change. This breaks must-keep 2 and 4 of this report for knowledge, so a redesign that copies the knowledge validator carries the hole over.
- Direction: apply the thread rule to knowledge. An entry under the user's tag, and any decision or constraint, changes only under the user's tag with the words test. A contradicting observation is recorded beside the entry as a dispute, for recall to show. The trail carries the change's own tag.

**missed-2. A project's memory is orphaned when its identity changes.** Lens: memory-quality. Severity: high. Known: no (`docs/known-limitations.md:20` covers only a key moved by a redaction change; the report raised it as open question 9 without tracing it).
- `internal/identity/key.go:13-36`: the key is the normalized remote, or `local-<folder>-<hash of the folder's path>`.
- `internal/resume/read.go:110-115, 297`: the brief reads the inbox folder named by the key and only entries with that key.
- `internal/update/wiki.go:87-134` and `internal/resume/read.go:613-634`: a wiki folder is the project's only when its `now.md` names the same remote, or no remote for a local repo.
- `internal/knowledgepage/recall.go:137, 159-167` and `internal/update/knowledge.go:46-48`: project knowledge and constraints are matched by `Row.Project == key`.
- No command rekeys, merges or aliases a project (grep of `cmd` and `internal/identity`).
- Impact: after `git remote add` on a repo that began local, after a rename or transfer of the remote, or after moving a local repo's folder, the project starts empty. Its thread pages, rules page, project knowledge and constraints, and its chats stop reaching briefs and recall; search `--all` still finds them. Nothing reports it. Starting local and adding a remote later is how most new projects begin.
- Direction: a stable project id kept in the wiki, with the remotes and folder keys that name it as aliases. Recognize a changed key (the repo's root commit, or the folder's previous key) and add an alias. A command to merge two projects.

**missed-3. Past 40 decisions on a thread, each new decision is filed where the brief shows it last.** Lens: memory-quality. Severity: medium. Known: no. The report notes that these lines pile up (pages-9), not what the brief does with them.
- `internal/threadpage/apply.go:213-216`: a decision added past the cap is appended to Open questions as an `eva-md: decision over cap, kept` line. `apply.go:329-331`: it never moves, and no code returns it to Decisions when a slot frees.
- `internal/resume/now.go:86-87`: only Decisions in force is reversed to newest first. Open questions is shown in page order (`now.go:137-162`) within 14% of the Now budget (`now.go:103-105`), about 450 characters on Claude Code.
- `internal/update/validate.go:562-571`: a decision never leaves as done, so on a long-lived thread the 40 fill and stay.
- Impact: on main in trunk-based work, or on the one general thread, the brief keeps showing decisions 28 to 40 as in force while each newer one sits unseen at the tail of Open questions, after up to 10 older questions. Each still needs a keep action on every pass.
- Direction: choose what is shown by recency and use, not by slot. At the cap, move the oldest decision out of the shown set, still in force and recallable, and keep the newest in it. A project-level store of decisions (pages-1) removes the cap.
