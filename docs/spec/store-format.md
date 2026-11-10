# The store's written format (schema 2)

The format of eva.md's memory store: what is on disk, who may set each part, and how it merges. The codec (ST-03), the migrator (ST-04), the storage properties (ST-02) and every reader or writer of memory build against it.
`internal/store/profile` checks a folder against it; `internal/store/testdata/fixture-store` is a hand-written store that passes.

Sources are in brackets. **P** is [the plan](../v2/plan.md): P6.2d3 is section 6.2, design 3; P6.3R the rulings table ending 6.3; D33 its decision 33. **S** is [the store design](../v2/working-papers/design/store.md) and **V** [the organizing design](../v2/working-papers/design/derive.md), by section.
Where S and P differ, P wins, and the rule says "overrides S…".

## 1. The format it profiles

Pages are an eva profile of the Open Knowledge Format v0.2, pinned to `GoogleCloudPlatform/knowledge-catalog` commit `d2aa4a49c018290835b50da2656d10235b8d7e20`, file `okf/SPEC.md`, SHA-256 `26aa5da029278939f914e578107242d9607d4f2dc5fe153272b82f9ed1030101`, read on 2026-11-10 [P6.2d5, S6.3].
Every `.md` file outside `notes/` and `legacy/`, but `index.md`, has frontmatter with a non-empty `type` (its §11); the root `index.md` follows its §8, with no frontmatter; there is no `log.md`; timestamps carry an offset (§5); `generated.by` and `verified[].by` follow its actor convention (§7).
No path, frontmatter key or static file names the library eva-md v1 was built on (`okf` as a word: `.okf-x`, `okfVersion`, not `bookfair`); free text is its author's [P6.2d5]. So the root index has no version key (overrides S5.1).

## 2. Layout

`<area>` is `projects/<pid>-<slug>` or `global`. Every file is in exactly one class; any other path is a fault [P6.2d2, S5.1]. The four root files and the two `.eva-md` files above the writers are required, and every project folder holds its `project.md`; as ids are unique, a pid names one folder.

```
index.md  README.md  .gitattributes  .gitignore       static, rewritten only by a schema step
.eva-md/store.toml  .eva-md/schema.toml               the manifest, and v1's refusal marker
.eva-md/writers/<writer>.toml                         one per machine
.eva-md/hand-edits/<writer>.tsv                       the hand-edit record (section 7)
.eva-md/coverage-v1/<writer>.tsv                      v1's frozen coverage, from the migration
projects/<pid>-<slug>/project.md                      the project identity record
projects/<pid>-<slug>/workstreams/<wid>-<slug>.md
<area>/items/<yyyy>/<mm>/<iid>-<slug>.md
<area>/disagreements/<yyyy>/<mm>/<did>-<slug>.md
projects/<pid>-<slug>/sessions/<yyyy>/<mm>/<host>.<skey>.<writer>.md   one per session, project and writer
<area>/notes/…                                        people's own text, never rewritten
systems/<slug>.md                                     frozen system pages
legacy/v1/…                                           v1 files the migration could not read, byte for byte
```

- A path is a function of what never changes: an id, its month and a slug frozen at creation. A new title, kind, scope or state never moves a file; a change of kind or scope is a new item superseding the old [S5.1].
- `global/` holds items of scope `all` only: each folder outside git is its own project (D20), so `global/` has no workstreams or sessions (overrides S5.1). `sessions/` replaces S's `episodes/`: P calls it a session file (overrides S5.1).
- A slug is 1 to 48 of `a-z0-9` with inner single hyphens. `<yyyy>/<mm>` is the UTC month of the id's time, or of a session's `started`. `<skey>` is the session id if it is 1 to 80 of `A-Za-z0-9_-`, else `x` and the first 13 alphabet characters of its SHA-256.
- No symlinks (the repository sets `core.symlinks=false`). Text is UTF-8 with LF ends and a final newline, unless the hand-edit record lists the file. Caps: 16 KiB for an item, workstream, disagreement or project record, 256 KiB a session file, 24 KiB a system page [S5.3].

## 3. Ids

The grammar: a letter and 13 lower-case Crockford base32 characters (`0123456789abcdefghjkmnpqrstvwxyz`), 14 in all. The 13 are 65 bits, high first: 35 bits of seconds since 2025-01-01T00:00:00Z, then the first 30 bits of SHA-256 of the pass id, LF and the operation's index.
Ids sort by time, a retried pass mints the same ids, and machines collide only by a 30-bit chance that a no-clobber create detects [P6.2d2, S5.2]. The model never writes an id [P6.3d2].

| Thing | Form |
| --- | --- |
| item, disagreement, working line | `i`, `d`, and `n` (next step) or `q` (open question), in the grammar |
| store | `s` in the grammar |
| workstream | `w`. With a branch, the 65 bits are the first of SHA-256 of `<pid>` LF `<branch>`, so a branch is one workstream on every machine (P6.3R; overrides S5.2's time part); with none, the grammar |
| project | `p_` and 12 alphabet characters: the first 60 bits of SHA-256 of the first key that named it, the same on any machine; not time-sorted, since P derives it [P6.1d6] |
| writer | `w-` and 8 alphabet characters, random, minted once per machine [S4.5] |
| pass | a ULID: 26 upper-case Crockford characters [S5.2] |

## 4. Values

Frontmatter is the JSON subset of YAML [S6.2]. A written line is `key: value`: a plain scalar (a lower-case letter, then `a-z0-9_.-`, not `y`, `n`, `yes`, `no`, `on`, `off`, `true`, `false` or `null`) or a JSON string, array or object with one space after each `:` and `,` outside strings and no other space;
or `key:` then lines of two spaces, `- ` and one such JSON object. Only a file the hand-edit record lists, or a system page, may hold a line in no written form, a repeated key (`locked` and `verified` aside, section 6) or a key outside its class's table.

A datetime is a JSON string in RFC 3339 with seconds and an offset. Text is a single-line JSON string `Apply` has escaped. Nothing volatile is committed: no timestamp is restamped on save; indexes, maps, rendered views, anchor drift and use counts are local [P6.2d2, D22].
Short excerpts beside items are not committed in v2.0 (overrides S5.3); the one quoted text is a rule's or constraint's `words` [D33].

## 5. Files

**Item** [P6.2d2, S5.3]. `type` is its kind: `fact`, `decision`, `constraint` (a limit on the work or the system), `procedure` or `rule` (a standing instruction to the agent) [P6.3R]. The body's first line is the statement, citing sources as `[^e1]`; an optional `## Detail` holds a reason or steps; `## Evidence` defines each source, `[^e1]: <resource>`.
A rule or constraint carries `words`: the user's own words, redacted, at most 240 characters, committed beside the model's sentence [P6.3d3, D33]. State is `active`, `retired` or `retracted`; `status` is `stable` when active, else `deprecated`.
Superseded is no state: the successor lists its predecessors in `supersedes`, which are not edited, and it is computed [S8.3]; disputed comes from disagreement records; stale is local (overrides S5.3's states, which had candidates: P6.3d5 admits only items with authority).

**Tags** [P6.3R, V5.5]. Each source carries `pv`, the record's author of the cited event, and `supports`, the tag code computes for the span from that author and the turn's exposure marks:

| `pv` of the span | `supports` may be |
| --- | --- |
| `typed` | `stated`; `accepted` (the first typed prompt after a proposal); `external` (a pasted part) |
| `chosen` | `accepted` (a dialog choice or plan approval) |
| `spoken` (the user's line in a voice call) | `stated`, `accepted`; never a rule's or constraint's (D29) |
| `tool` | `observed`; `external` (a fetch or another server's tool) |
| `agent`, `subagent` | `suggested`; `external` (after outside text); `echo` (after eva-md's own delivery) |
| `front_model` (the voice front model's words), `pasted`, `host`, `program`, `unknown` | `external` |
| `eva` | `echo` |
| `v1_user` | on a migrated item only: `stated`, `accepted`, `external` |

An item's `tag` is its sources' highest `supports` in the order `stated`, `accepted`, `observed`; `echo` adds nothing [V5.5]. Only those three can tag an item: `suggested` and `external` stay notes in a session log [P6.3d5]. `accepted` keeps both spans: two sources (D10).
By kind: `rule` and `constraint` are `stated` from a `typed` span; `decision` `stated` or `accepted`; `procedure` `stated`, `accepted` or `observed`; `fact` `stated` or `observed`.
A migrated item (`migrated_from`) keeps v1's tag without matching support, and has two labels of its own: `reading`, the agent's reading, which v1's observed pages become, and `inferred`, read-only [P8 ST-01, V5.5].

**Workstream** [P6.3R, S5.4]. State `active`, `merged` or `archived`. The body is one cited status line the model maintains; `## Next steps` and `## Open questions`, lines `- <text> <!-- id:<n…|q…> -->` (a closed line is removed);
`## Decisions in force`, lines `- [<title>](/<path of an active decision of the project>)`. No model-written paragraph (overrides S5.4).

**Session file** [P6.2d3, P6.3R, S5.5]. Notes and the ranges of the record it covers; no summary (overrides S5.5). `## Log` lines are `- <did|found|failed|left>: <text> [^e1]`, append-only. `covers` holds one object per range: `from`, `to`, `pass`, `outcome`, `contract`, and `reason` unless `done`.
Outcomes: `done`; `skipped` (`no_user_turn`, `program`, `subagent`, `empty`); `refused` (`timeout`, `answer_invalid`, `tool_used`) [V3.6]. Ranges ascend, apart. A session skipped at no model cost is local and makes no commit [P6.2d3].

**Project identity record** [P6.1d6, S5.4]: the id, a display `title`, `remotes`, `root_commits`, v1's `legacy_keys`, `merged_into`, `related`, `shares_with`; never a folder path. `shares_with` takes the person's authority (D13).

**Disagreement** [P6.3d6, P8 ST-01]. A stored record in v2.0, in the area of the item it sits beside: `item`; `op`, the operation refused (`supersede`, `retire`); `sources`, the span it cited; `reason` (`locked`, `verified`, `hand_edit` for an unacknowledged hand edit, `authority` for the user's, `project` for another project's item); `at`, the date; `session`.
The body is the statement the operation proposed. The item is never written for it, so a locked item can have one (S5.7 had no record).

**Frozen system page** [P6.3d9, S5.6]. `systems/<slug>.md`, read through the same codec. Every field and section is kept as v1 wrote it (`type: system`, `title`, `description`, `status`, `generated`, `sources`, `repos`, `aliases`, `as_of`, `state`, `locked`, `verified`),
but the migration drops the map link line, as the map is local, and writes `stale_after` as its day's start in UTC, the datetime the format asks for. Written only when a person asks, with compare-and-swap.

**Static and record files.** `.gitattributes` is `* -text -filter -ident` and `.gitignore` `.tmp-*`, so no setting of a person's git changes bytes [P6.2d5]. `store.toml` is `schema = 2`, `min_reader = 2`, `store_id = "<sid>"`, `epoch = 1`, in that order. `schema.toml` is `schema_version = 2`, which a v1 binary refuses to write past [S9.1].
A writer record is `label = "<label>"`, `signing_key = "<key or empty>"`, `reads = [<lowest>, <highest>]`. `coverage-v1/<writer>.tsv` is the header `session` TAB `through`, then `<host>:<session>` TAB the last v1 line covered [S9.2].

## 6. Protection

A file a person locked or verified, or one that cannot be read, refuses the one operation aimed at it; the pass and every other file go on [P6.2d4, S5.7].

- **Locked**: a `locked` key with any value but a bare `false` (`False`, `FALSE`, a comment after it allowed), so an empty or quoted value locks, and so does a repeated key. Only items, workstreams and system pages may be locked. The engine never writes it (overrides S6.2, where an empty value unlocked).
- **Verified**: a `verified` key with a value in any shape but `[]`, `{}`, `""`, `null`, `~` or none. `eva-md confirm` writes a JSON list of `{"by", "at"}`; no model's operation does. A model cannot change a verified file's statement, title or detail [S5.7].
- **Key forms**: a lock is honoured in every form YAML allows [P6.2]. Only a bare `locked:` or `verified:` at a line's start is read by its value; in any other form (quoted, with escapes, spaces before the colon, `? locked`, after a tag or anchor, a block indented whole) the key protects whatever its value. An alias key (`*a`), a merge key (`<<`) or an explicit key that is no scalar does both.
- **Unreadable**: a byte-order mark, invalid UTF-8, a NUL, a line over 64 KiB, a conflict marker line, or frontmatter not opened on line 1 or never closed. Never written or delivered; listed for repair [S6.2]. Only documents may be unreadable: an unreadable static or record file is a broken store.

## 7. A hand edit

A file changed in the store folder by anything but the engine is kept, committed (kind `human`) and listed in the hand-edit record of the machine that found it: header `path` TAB `at` TAB `state`, then a line per adoption, `unacknowledged` until the person acknowledges it, then `acknowledged`; the newest line for a path, across writers, holds, and on a tie `unacknowledged` does.
A line names a document, `README.md` or `index.md` (then the person's text, only to be readable). One for a gone path (forgotten, removed by hand) stays, as a union cannot drop it, and holds nothing.
**A hand edit carries no authority until it is acknowledged**: its words are not taken as the person's, and a model's operation on it becomes a disagreement, reason `hand_edit`. This overrides S5.7, which gave it a person's authority: an agent with a file tool cannot write memory in the person's name [P6.2d4, P8 ST-01].
The record is outside the file because the engine writes nothing into a locked file.

## 8. Commits

One write path, one commit per pass [P6.2d3], authored as `eva.md on <label> <<writer>@machines.eva-md.invalid>`, never with the person's identity or signing [P6.2d6]. Both answers to D3 fit: **signed** with an ed25519 key eva-md makes for the machine, its public half the writer record's `signing_key`; or **unsigned**, `signing_key` empty.
The subject is `<kind>: <pid or ->` and counts; the body is trailers, one per line:

```
Eva-Pass: <ULID>                 the pass, the idempotency key
Eva-Kind: <kind>                 fast consolidate remember correct confirm acknowledge forget undo migrate init human merge
Eva-Writer: <writer>
Eva-Session: <host>:<session>    when the pass read a session
Eva-Model: <host>/<model>[@<effort>]   when a model ran
Eva-Covers: <host>:<session> <from>-<to> <outcome>   once per range
Eva-Ops: create=<n> revise=<n> refused=<n>
Eva-Schema: 2
```

A message holds ids, counts and enums, never memory text [S7.2]. A pass's ranges are in the same commit's tree (`covers`), so a restored store says what to replay [P6.2d3].

## 9. Schema, minimum reader and epoch

`schema` is the format's generation; `min_reader` the lowest one a binary must know to read the store. An additive change (a new optional key or kind) changes neither, and the profile gains its row; a breaking one raises `schema` and sets `min_reader`. A binary below `min_reader` reads nothing, writes nothing and says so once [P6.2d7, S9.1].
`epoch` rises only when a purge rewrites history [S8.7]. Decisions this format reopens: v1's 15 (D1), 1 and 16 (D2), 7 (D3), 2 (D22), and "no captured text is ever committed" (D33).

## 10. Fields and path classes

"Set by": **E** the engine's code, **M** a model's operation that code checks, **P** a person, **G** the migration. "Merge" is the rule when two writers changed it [S8.3]; the **content register** moves as one group, from the side with the greater authority (a person's acknowledged edit over a model's), then `asserted_at`, then writer.
The profile checker has one check for each row, named by its first column.

| Name | Where | Rule | Set by | Merge | Source |
| --- | --- | --- | --- | --- | --- |
| `path` | every file | in exactly one class of section 2 | E | — | P6.2d2 |
| `symlink` | every path | no symlinks | E | — | S4.3 |
| `name` | paths, keys, static files | never names the library v1 was built on, as a word (section 1) | E | — | P6.2d5 |
| `encoding` | text files | UTF-8, LF, final newline; unless listed as hand-edited | E | — | S6.2 |
| `size` | documents | within the caps of section 2 | E | — | S5.3 |
| `unreadable` | every file | only documents may be unreadable | E | — | S6.2 |
| `frontmatter` | documents | each key once; only the class's keys; unless listed | E | — | S6.2 |
| `written-form` | documents | each line in a written form of section 4; unless listed or a system page | E | — | S6.2 |
| `month` | items, disagreements, sessions | `<yyyy>/<mm>` is the id's UTC month, or `started`'s | E | — | S5.1 |
| `index` | `index.md` | required; no frontmatter; headings (`#` to `######`) and `* [t](u) - d` lines | E | static | OKF §8 |
| `readme` | `README.md` | required; `type: readme`; `title`, `description` optional | E | static | S5.1 |
| `gitattributes` | `.gitattributes` | exactly `* -text -filter -ident` | E | static | P6.2d5 |
| `gitignore` | `.gitignore` | exactly `.tmp-*` | E | static | S5.1 |
| `manifest` | `.eva-md/store.toml` | the four lines of section 5, in order | E | migrate first | S9.1 |
| `schema-marker` | `.eva-md/schema.toml` | one line, `schema_version = <n>` | E | static | S9.1 |
| `writer-record` | `.eva-md/writers/<writer>.toml` | writer id grammar; the three lines of section 5 | E | one file per writer | S4.5 |
| `hand-edits` | `.eva-md/hand-edits/<writer>.tsv` | section 7's header, lines and paths; the writer has a record | E | union; newest per path, a tie unacknowledged | P6.2d4 |
| `coverage-v1` | `.eva-md/coverage-v1/<writer>.tsv` | section 5's header; a session once; a positive line number | G | union | S9.2 |
| `project-record` | `projects/<pid>-<slug>/project.md` | pid grammar and slug; in every project folder | E | — | P6.1d6 |
| `workstream-file` | `…/workstreams/<wid>-<slug>.md` | in a project; id and slug | E | — | P6.3R |
| `item-file` | `<area>/items/<yyyy>/<mm>/<iid>-<slug>.md` | id and slug | E | field merge | S5.1 |
| `disagreement-file` | `<area>/disagreements/<yyyy>/<mm>/<did>-<slug>.md` | id and slug | E | add only | P6.3d6 |
| `session-file` | `…/sessions/<yyyy>/<mm>/<host>.<skey>.<writer>.md` | name matches `session` and `writer` | E | one writer per file | S5.5 |
| `notes` | `<area>/notes/…` | segments of `A-Za-z0-9._-`; content not checked | P | union text | S5.1 |
| `legacy` | `legacy/v1/…` | segments of `a-z0-9._-`, as v1 named them; content not checked | G | — | S9.2 |
| `system-page` | `systems/<slug>.md` | `type: system`; slug; no map link line; fields as v1 wrote them | P, E | whole file | P6.3d9 |
| `type` | documents | the class's type; an item's is one of the five kinds | E, M | immutable | P6.3R |
| `id` | item, workstream, disagreement, project | grammar of section 3 for the class; the file name's prefix; unique | E | immutable | S5.2 |
| `title` | item, workstream, project | required; 1 to 200 characters | M, P | content register | S5.3 |
| `description` | item, workstream, project, readme | optional; at most 300 characters | M | content register | S5.3 |
| `status` | item, workstream | `stable` when `active`, else `deprecated` | E | follows `state` | S5.3 |
| `state` | item, workstream | item: `active`, `retired`, `retracted`; workstream: `active`, `merged`, `archived` | E, M | newest `state_at`; `retracted` final | P6.3R |
| `state_at` | item, workstream | datetime; an item's not before `learned` | E | with `state` | S8.3 |
| `state_reason` | item | only when not active: `user` or `deleted_by_hand` for retired, `forgotten` for retracted | E | with `state` | S5.7 |
| `scope` | item | `here` or `project` in a project, `all` in `global/` | M, E | content register | V6.2 |
| `project` | item, workstream, session, disagreement | in a project, its pid; in `global/`, an existing project or absent | E | immutable | P6.1d6 |
| `workstream` | item | an existing workstream of the item's project; required for `here` | E | content register | V6.5 |
| `tag` | item | section 5's tag rules | E | content register | P6.3R |
| `supports` | sources | allowed for its `pv` by section 5's table | E | with sources | V5.5 |
| `words` | rule, constraint | required unless migrated; 1 to 240 characters; nowhere else | E | content register | D33 |
| `generated` | item, workstream, session, disagreement | `{"by": <actor>, "at": <datetime>}`; `at` moves only with content | E | content register | S6.2 |
| `learned` | item | datetime of the first evidence | E | immutable | V6.2 |
| `asserted_at` | item | datetime of the newest evidence behind the statement; not before `learned` | E | orders the register | S8.3 |
| `confirmed` | item | optional datetime, not before `learned` | E | the later | V6.2 |
| `supersedes` | item | ids of existing items, not its own, once each | E | union | S8.3 |
| `branch` | item, workstream | optional; a git branch name | E | content register | P6.3d7 |
| `anchors` | item | optional; at most 16 `<repo>@<commit>:<path>` | E | content register | V6.2 |
| `cues` | item | optional; at most 8 strings of 1 to 60 characters | M | content register | V6.2 |
| `sources` | item, workstream, disagreement, session | objects of `id` (`e<n>`, once), `resource` (an event id, or `s:` on a migrated item), `pv`, `supports`, `hash` (`sha256:` and 64 hex); 1 to 3 on an item, a disagreement and a status line | E | union | P6.3d3 |
| `migrated_from` | item | optional; the v1 page's path | G | immutable | S9.2 |
| `locked` | item, workstream, system | read fail-closed; nowhere else | P | true wins | P6.2d4 |
| `verified` | item, workstream, system | read fail-closed; each `by` an actor, each `at` a datetime; nowhere else | P | union | P6.2d4 |
| `remotes` | project | optional list of `host/path`, no scheme and no credentials | E | union | P6.1d6 |
| `root_commits` | project | optional list of 40-hex commits | E | union | P6.1d6 |
| `legacy_keys` | project | optional list of v1 keys | G | union | P6.1d6 |
| `merged_into` | project | optional; another existing project | P | lowest id | S8.3 |
| `related` | project | optional list of other existing projects | E | union | S8.3 |
| `shares_with` | project | optional list of other existing projects; the person's authority only | P | union | D13 |
| `session` | session, disagreement | `<host>:<session id>` | E | immutable | S5.5 |
| `writer` | session | the file name's writer; has a writer record | E | immutable | S5.5 |
| `started` | session | datetime | E | immutable | S5.5 |
| `workstreams` | session | list of existing workstreams of its project | E | union | S5.5 |
| `covers` | session | section 5's coverage objects; ranges ascend, apart | E | union | P6.2d3 |
| `item` | disagreement | an existing item in the same area | E | immutable | P8 ST-01 |
| `op` | disagreement | `supersede` or `retire` | E | immutable | P8 ST-01 |
| `reason` | disagreement | `locked`, `verified`, `hand_edit`, `authority`, `project` | E | immutable | P8 ST-01 |
| `at` | disagreement | datetime | E | immutable | P8 ST-01 |
| `stale_after` | system | optional datetime | G | whole file | S6.3 |
| `schema` | manifest | `2` | E | migrate first | S9.1 |
| `min_reader` | manifest | from 2 to `schema` | E | the higher | S9.1 |
| `store_id` | manifest | a string holding an `s` id | E | equal | S4.2 |
| `epoch` | manifest | 1 or more | E | the higher, by purge | S8.7 |
| `schema_version` | schema marker | `2` | E | static | S9.1 |
| `label` | writer | 1 to 32 of `a-z0-9-`, no personal data | E | one file per writer | S4.5 |
| `signing_key` | writer | empty, or `ssh-ed25519` and its 68-character key | E | one file per writer | D3 |
| `reads` | writer | two generations, lowest first | E | one file per writer | S4.5 |
| `statement` | item, workstream, disagreement | the body's first line is a non-empty statement of at most 500 characters | M | content register | P6.3R |
| `citations` | item, workstream, disagreement, session | every footnote names a source; every source is cited; an item defines each as `[^e1]: <resource>` | E | with sources | OKF §5.1 |
| `working-items` | workstream | next-step and open-question lines with `n`, `q` ids, once each | M, E | per line | P6.3R |
| `decisions-in-force` | workstream | links to active decision items of its project that no active item supersedes | E | recompute | P6.3R |
| `log` | session | `## Log` lines of section 5, each cited | M | append | P6.3R |
