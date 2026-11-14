# Wiki layout and page formats

- **Component:** Renderer + okf adapter: the renderer, and the wiki's layout
- **Paths:** `internal/threadpage`, `internal/wikiinit`
- **Status:** v1 as built, frozen on 2026-11-14

The blocks below are v1's design and verification text, moved here word for word when the two documents were split; [INDEX.md](INDEX.md) lists every spec.

<!-- v1 docs/design.md 003 -->
## Wiki layout and page formats

The wiki lives in a folder you choose during `eva-md setup` (there is no default), backed by a private repo.
Each machine points at one wiki.
The whole tree is one okf bundle.
Setup creates README.md, the root index.md, log.md, `.eva-md/schema.toml`, `.gitattributes`, `.gitignore` and the general area with placeholder pages (general/now.md and general/threads/default.md).
The placeholder thread page is rendered by the page model the background update uses (`internal/threadpage`), so the two can't drift.
The `projects/` folder, its pages, the rules pages and real thread content are written by the background update, and so is the `knowledge/` folder, created by its first knowledge page.
The `systems/` folder is written by `eva-md system write`.

```
README.md                       type: guide; what this is, how to read it, how to lock a page; links to general/now.md
index.md                        okf root index without frontmatter, written by setup and extended by code, no timestamps
log.md                          '## YYYY-MM-DD' local-date groups, oldest first: init's line, then a line for each page created or changed, once a day (owned by eva-md)
.eva-md/schema.toml             schema_version = 1, instance = "<machine>-<8 hex>", created   (hidden: okf ignores it)
.gitattributes                  log.md merge=union ; *.md text eol=lf
.gitignore                      *.jsonl  /inbox/  .eva-md/tmp/  .tmp-*  .DS_Store
projects/index.md               sub-index of project folders (no frontmatter)
projects/<slug>/index.md        lists now.md, rules.md once it exists, and threads/index.md
projects/<slug>/now.md          project overview, built by code
projects/<slug>/rules.md        the project's standing rules, linked from its now.md
projects/<slug>/threads/index.md
projects/<slug>/threads/<branch>.md   one thread per branch: Now on top, Timeline below
general/index.md, general/now.md, general/threads/index.md, general/threads/default.md
general/rules.md                rules for all projects, linked from general/now.md
systems/index.md                sub-index of system pages
systems/map.md                  the system map: one line per system page, built by code
systems/<slug>.md               one page per system, written by eva-md system write
.eva-md/names.tsv               the names index the prompt hook matches against
.eva-md/repos.tsv               the repos index: which projects share a system page, for recall
knowledge/index.md              sub-index of the knowledge pages and the map
knowledge/map.md                the knowledge map: one line per knowledge page, built by code
knowledge/k<N>.md               one lasting fact, decision, procedure or constraint, written by the update
.eva-md/knowledge.tsv           the recall index the prompt hook, the brief and the update read
```

The root index also lists `systems/index.md` after the first system write, and `knowledge/index.md` after the first knowledge page.

`knowledge/` is created by the first knowledge page, never by init. okf treats `knowledge/` as the wiki whenever the root index.md is missing, so opening the wiki checks that the root index.md exists (see "Open the wiki").

**Slugs.** The slug is the repo name, or owner-repo on a collision.
Projects without a remote use `<folder>-<hash6>`.
A project without a remote is found in its own slug folder, or in the folder with its key's hash (`identity.Project.HashedSlug`: the slug, `-` and the first 6 hex digits of the SHA-256 of the key), which the update takes when another project holds the plain name, and only when that folder's now page names no remote; another project's folder of the same name is never taken.
The update, the brief and search look in both.
The full normalized remote is stored in now.md frontmatter, and the map from identity to slug is rebuilt from it.

**Thread file names.** The branch name is turned into an okf-safe ID by one function (`threadpage.ThreadName`), which the brief, search and the update share:
- Any character other than a letter, a number, `_`, `-` or `.` becomes `-`, `/` included.
- A leading `.` or `-` gets a `_` prefix.
- The reserved names `index`, `log` and `AGENTS` (any case) get the suffix `-thread`.
- Control and bidi characters are removed. A branch with no name is `default`, and a name left empty is `thread`.
- A 6-character hash is added whenever any of these changed the name.

The ID `projects/<slug>/threads/<file>` has 4 parts, well under okf's limit of 8.

**Index files.**
- eva-md regenerates every sub-index in the folders a write touched, sorted by file name, in okf's exact line format: `* [Title](file.md) - Description`. The root index is written once by setup; each of `projects/`, `systems/` and `knowledge/` adds its line at the end the first time it appears, and the knowledge line goes again when the folder is gone.
- `[`, `]` and `)` in titles are escaped.
- The root index has no frontmatter, since okf's spec makes its `okf_version` line optional and a wiki names eva-md alone.
  It lists README.md plus a link to `general/index.md`, to `projects/index.md` once the update writes a project's first page, to `systems/index.md` after the first system write, and to `knowledge/index.md` after the first knowledge page.
  Sub-indexes have a `# <Folder>` header and no frontmatter.
- No timestamps. A line changes only when a title or description changes, and descriptions are built from fixed text, so normal updates don't touch the index files.

**Links.** okf counts a page as orphaned unless another page's body links to it or it links out; links in index.md files don't count. So:
- README.md links to `general/now.md`.
- Each now.md links to every one of its threads, including idle, merged and gone ones.
- No page ever links to an index.md or log.md, because okf counts those links as broken.

**Frontmatter rules.**
- okf writes keys in a fixed order: type, title, description, generated, verified, status, sources, then extra keys sorted alphabetically.
- eva-md's extra keys are flat: plain values or lists of plain values, never nested maps (okf turns nested maps into strings).
- `status` is always `stable`. eva-md's own lifecycle goes in `state`.
- `generated.by` must pass okf's actor check, so it can contain no spaces. It is `<host>/<model>`, for example `claude-code/claude-opus-5-5` or `codex/gpt-5.5-codex`. Any `/` or space inside the model name becomes `-`. It is `<host>/unknown` when the model is unknown, and `eva-md/<version>` for pages built by code.
- `generated.at` is always the UTC save time (okf sets it). The time of the content is `as_of`.
- `sources` items are mappings: `- resource: "s:3f2a91c0"`.

### Thread page

The thread page is the only page the model influences, and only through validated operations.

```
---
type: thread
title: "eva-md · main"
description: "Branch main of github.com/you/eva-md"
generated: { by: claude-code/claude-opus-5-5, at: "2026-01-15T09:14:02Z" }
status: stable
sources:
  - resource: "s:3f2a91c0"
  - resource: "s:91bd0e44"
as_of: "2026-01-15T10:13:55+01:00"
branch: "main"
head: "4e2bf4c1a9d3e8f07b6c5a2d1e9f8a7b6c5d4e3f"
locked: false
model: "claude-opus-5-5"
model_note: ""
parent: ""
project: "eva-md"
state: "active"
writer: "eva-md/2026.1.15+0912.9e4f609"
---
```

- `sources` holds the last 20 sessions.
- `model` is the raw model name.
- `model_note` is filled only for "fallback from X" or "default (chat model unknown)".
- `parent` is the thread this one started from, if any.
- `state` is `active`; idle, merged and gone, worked out from git, are planned.
- okf's serializer decides the exact quoting.
- `head` is the commit HEAD named when the page was written, which the brief's live git compares with HEAD ("HEAD moved N commits since the page was written", with the last 3 subjects).
  A thread page cites no code: its sources are sessions, the refs of its items are inbox entries the validator checks against each batch, which is what keeps a model from forging them, and an update reads nothing of a repo but the branch's last commit subjects and HEAD (see "Page context"); it cites code only on a knowledge page, at that HEAD, for paths a cited entry writes (see "Knowledge page").
  Standing rules are the user's own words and cite none either.

Body (built in `internal/threadpage`, whose package comment has the exact format and a full sample):
- `# eva-md · main`
- `## Now`, between `<!-- eva:now:start -->` and `<!-- eva:now:end -->` markers:
  - summary, at most 600 characters
  - `### Last chat`: date, host/model, one line (at most 400 characters)
  - `### Done`: the last chat's results, up to 8 items, replaced each pass
  - `### Next`: up to 10 items
  - `### Open questions`: up to 10, including `Proposed (assistant): ...` items
  - `### Decisions in force`: up to 40, stated or accepted only
  - An item is one line: `- `, an optional label, the text, the tag in brackets, the refs, and a hidden comment with its id and the cited user prompt's time, for example `- Use JSONL for the inbox [stated] s:3f2a91c0#14 <!-- i:n3 t:2026-01-14T10:00:00+01:00 -->`. Empty sections are left out.
  - Labels: `Proposed (assistant): ` marks an Open question tagged suggested, `Unattributed: ` one tagged inferred, and `eva-md: <code>: ` a line eva-md wrote itself, which keeps its stored tag. No rule ever changes a speaker tag or shows user speech under an assistant label.
  - A brief removes the hidden comments and shows these sections by priority (see "The Now in a brief" under Recall and fencing).
- Caps never drop a decision. A decision added past 40 is kept under Open questions as `eva-md: decision over cap, kept: ...` with its tag. Next and Open questions past 10 move to the Timeline marked `(moved: cap)`: inferred and suggested items first, then the oldest, and never a stated item while an assistant-tagged one remains.
- `## Timeline`: `### YYYY-MM-DD` groups of lines reading `- <word> [<tag>]: <text> <refs>`, for example `- decision [stated]: Use JSONL for the inbox s:3f2a91c0#14`.
  Retired items keep their tag, refs and id comment: `done`, `answered: q → a`, `~~old~~ → new (superseded 2026-01-14)` and `~~item~~ (dropped: reason)`.
  A date out of order starts a new group at the end.
  Ids (`n<k>`) are never reused, and an item written by hand gets one at the next update.
- Timeline lines are never rewritten. Any other `##` section, such as your own `## Notes`, keeps its text byte for byte, placed after the Timeline.
- Free text goes through one escaper (see Validation 8), so no item can forge a heading, a rule, a comment, a fence, a link or a label. The renderer never writes a line that is exactly `---`.

**When a page is never written:**
- **Locked:** `locked: true`, `yes` or `on`, in any case.
- **Human-verified:** the page has a human `verified` entry, which would otherwise be marked superseded by every save (validator.go:200-205).
- **Damaged:** the eva markers are broken, or okf refuses the save because of text you wrote, for example horizontal rules around a `Status:` line. A notice explains the fix. eva-md never rewrites your sections.

### Project now.md

Built by code with no model, by the update job after each save. A project's first page write creates it first, holding the project's remote, so another project of the same name can't take the folder. This is decision 11's Now page, as an overview of the project's threads.
- **Frontmatter:** type: now, title, description (fixed text), generated `{by: eva-md/<version>}`, status: stable, then locked, remote, writer.
- **Body:** `## Threads` has one line per thread, newest activity first: `* [<branch>](threads/<file>.md) — <first sentence of its summary> · last active <date>`.
  Every thread is listed, so okf never finds one orphaned.
  Folding idle threads and marking merged or gone ones are not in v1.
  Once the folder has a rules page, a `## Rules` section before `## Threads` links it (`* [Standing rules](rules.md)`), so okf finds no orphan there either.

### Rules page

Standing rules are what the user asked to hold from now on, in the user's own words. `projects/<slug>/rules.md` holds a project's, and `general/rules.md` the rules for all projects. The background update writes them (see Validation 12), and a person may edit them.
- **Frontmatter:** type `rules`, title `<slug> · rules` or `General · rules`.
- **Body:** `# <title>`, then `## Rules`: one rule a line in the thread item format, `- <text> [stated] <refs> <!-- i:r1 t:<time> -->`, at most 40, newest last.
  Ids are `r<k>` on a project's page and `g<k>` on the general page, so one pass can name rules on both.
  An edit replaces a rule and moves it to the end.
  A dropped rule moves, struck through, to `## Retired rules` as `- ~~<text>~~ (dropped <date>: <reason>) [stated] <refs> <!-- i:r1 ... -->`; that section isn't shown in briefs and has no size limit, and new ids count past it, so an id is never reused.
- **Hand edits:** any non-blank line under `## Rules` is a rule, and one without a usable id gets one at the next write; other sections are kept byte for byte. A heading or fence among the rules, text before the first section other than the title, a second `## Rules` or another type marks the page damaged, and it is never rewritten. Locked and verified pages are left alone too.
- **In the brief:** startup, clear and compact briefs start with the rules under "Standing rules, the user's own words, newest first:", those for all projects first, each with its tag and refs, at most 600 characters, reserved like the footer.
  Trimmed from the end, the block closes with `N more: <bin> show general/rules`, or the project's page, or both.
  The briefed marker's hash becomes `<Now hash>+<rules hash>` when rules exist (older markers read as before), so a changed rule triggers a refresh, headed "Standing rules (a rule changed since this chat last saw them), newest first:", which spends one of the chat's 2 refreshes; a resumed or forked chat gets changed rules too.

<!-- v1 docs/design.md 007 -->
### Branches and threads

- The thread key is the branch read live from the payload's `cwd`.
- A detached HEAD keeps the session's last thread. A repo with no commits and the general area use `default`.
- If a session switches branch midway, its entries are grouped by the branch recorded on each entry.
- A new branch with no thread uses the lineage fallback (see Resume).

### Schema versioning

The binary declares the highest schema it supports.
- If the wiki is newer, eva-md goes read-only: capture and briefs continue, writes and commits stop (the daily commit is refused with reason `schema`), and a notice appears.
- If the wiki is older, `eva-md migrate` (later; not built) will preview and then apply in one commit.
- Unknown frontmatter keys and sections are kept.

---

<!-- v1 docs/verification.md 014 -->
- **Thread pages** (`internal/threadpage`).
  Render and Parse round-trip byte for byte, including through okf with zero validation findings; the escaper keeps okf from accepting smuggled structure; a fuzz test of Parse; 1,500 random updates showing that applying an update twice changes nothing, that ids stay unique and are never reused, and
  that the cap rules hold (decisions never dropped, items moved in tag-then-age order); speaker labels never shown on user speech; the project overview links every thread.
  The line scanner search uses reads the same items and lines as Parse on every page Parse accepts (a fuzz test), and still reports what it can of a damaged page.
  The exported escaper keeps a line to one line under its cap, forges no structure and changes nothing cleaned twice (`TestCleanLine`).

<!-- v1 docs/verification.md 021 -->
- **Standing rules and pasted text** (`internal/update`, `internal/threadpage`, `internal/resume`).
  Five rule histories in the accuracy suite: a rule the user states is kept, and one tagged suggested, one in words the user didn't say, one only the assistant suggested and one in pasted text (also behind a forged closing tag) are refused; a replacement, an older replacement refused, a duplicate refused, and scope with and without the scope words, said in the stating prompt only.
  Adversarial rules, such as a reworded question, a "Do not" the user didn't say and a negation moved to another clause, are refused, as is a rule whose only negation is a contraction such as aren't against a prompt that says the opposite (`TestRulePolarity`, `TestValidateRuleChanges`).
  The paste parser, Codex prompts never parsed, numbers with units, drops, outside a project and older sessions.
  The rules page model: round trip, damage cases, the cap, ids and the retired section, with no id reused (`TestApplyRules`).
  A real pass writes both rules pages through okf, with their overview links, index lines and a clean validation, then drops a rule (`TestPassWritesTheRulesPages`); a locked rules page is left alone, and a drop on it ends `no_change`, counted read-only, with the page byte for byte and no Timeline line
  on the thread page (`TestALockedRulesPageLeavesNoTimelineLine`), while the counts and Timeline lines of rule changes on a locked or damaged page are taken out and the other lines kept (`TestSettleSkippedRules`).
  The brief: rules first on both hosts in every variant and in a first chat, the caps and the "more" line, a refresh and a resume after a rule changed.

<!-- /v1 -->

## Since the split

No change yet.
