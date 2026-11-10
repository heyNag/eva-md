# The wiki: page store, layout, history, sync and migration

Design section for the eva-md v2 plan. Area: eva-md's own page store and codec, the layout that merges by construction, one commit per update pass as audit and undo, who owns the repository, local-first creation with a remote attached later, a machine identity for commits and signing, background fetch, merge and push, restore and replay, the schema migrator with a minimum reader version, and a storage conformance suite.

Sources: the design brief, the lead's notes, the storage, okf, pages, systems, setup, search, update-write, brief and catchup audits with their verification sections, the two research notes with their fact checks, the ledger designer's draft (`design/ledger.md`, for its contracts C-S1 to C-S4), and the code at commit `51b241c`. Finding ids are the audits' own (`sync-1`, `okf-3`, `pages-6` and so on; `storage:` or `okf:` is prefixed where two audits share a number).

Three git behaviours this design leans on were run in a scratch folder on git 2.56 (Linux) and held: a merge with `--no-commit -X no-renames` leaves the three versions of each conflicted path readable from the index; a commit can be signed with an SSH key given only by per-call `-c` settings, and `%G?` tells an enrolled signer (`G`) from an unknown one (`U`); with `core.symlinks=false` a symlink in a commit is checked out as a plain file. Section 16 says how each is confirmed on a Mac before the code that needs it is written.

---

## 1. Goal and the measurable bar

Memory that is on the disk seconds after it is derived, off the machine minutes later, the same on every machine the person uses, never waiting for a person to repair git, and always able to answer "why does it think that" and "undo that".

| Measure | Bar | v1 today |
| --- | --- | --- |
| Questions and network calls needed before the store exists | 0 and 0 | a folder, a remote, a confirmation and a push (onboard-1, setup-1) |
| `time_to_durable`: validated pass to local commit, p95 | under 2 s | pages are files at once, but a commit comes at most once per active day (durability-1) |
| `time_to_synced`: local commit to the remote, p95, network up, tick running | under 10 min | the next active day's first chat |
| `unsynced_oldest_age` exists as a gauge and a typed condition | yes | no such figure (storage:eval-1) |
| Writes acknowledged and then lost, over seeded multi-machine schedules with crashes | 0 in 10,000 schedules | no test has two writers |
| Schedules that end with every machine holding the same tree | 100% | a second machine ends `diverged` for good (sync-1) |
| States in which a person must repair git by hand | 0. What is left is a short list of plain asks: sign in, the remote is gone or not this store's, confirm a new machine or an outside change, a hand edit was held, the repository is no longer private, git is missing or eva-md is too old | 15 refusal and failure reasons with hand fixes (ownership-1) |
| Cold restore from any pushed head | 100% succeed; replay queues exactly the uncovered ranges | adoption can refuse what the commit pushed (restore-1); what was summarized since the last push is lost (replay-1) |
| Smallest unit that can be undone | one pass | one day (history-1) |
| Frontmatter readers in non-test code | 1 | 6 (okf-4) |
| Protection keys read fail-closed over a corpus of hand edits | 100% | `locked: true  # note` reads as not locked (okf-3) |
| v1 text units unaccounted for after migration (fixture, generated corpus, a copy of the owner's wiki) | 0 | no migration path exists (migrate-1) |
| Git processes and store-tree reads in a hook | 0 | 0 git; bounded page reads |
| Third-party modules compiled into the binary | 1 (the TOML reader) | 2 |

The time bars are measured by the scale and soak package (ST-12) on the CI Mac and on a simulated day, and reported to the evaluation area's results store. They are budgets to confirm, never assertions inside `go test`.

---

## 2. The three options, weighed

The charter asks for a choice between pages as the truth with a merge driver, an operation log as the truth with pages as projections, and items and episodes as files with rendered views.

| | A. Pages plus a merge driver | B. Operation log, pages projected | C. Items and episodes as files, views rendered |
| --- | --- | --- | --- |
| What is the truth | today's thread, rules and knowledge pages | an append-only log of validated operations per writer | one small file per item, one append-only file per session and writer; git history is the log |
| How two machines merge | a three-way merge of each page family, written by hand: union of items by id, last writer for the summary, caps applied again, a text merge for people's sections | by union of logs; needs a total order of operations across machines and a fold that every future version can still run | by construction: different items are different files. One item changed on two machines is a field merge over about a dozen typed fields |
| Reading current state | read the page | fold the log, or trust a projection that is then a cache to keep in step | read the file |
| Hand edits | natural, and the reason for the damaged-page class and nine parsers (pages-6) | none: an edit must become an operation | natural on one small file; a damaged file stops only itself |
| Audit and undo | needs per-pass commits added | the log is the audit; undo is a new operation | per-pass commits are the audit; undo restores paths |
| Forgetting | edit the page; history keeps it | fights the append-only rule | retire or retract the file; history keeps it unless purged (ST-D8) |
| Schema change | migrate pages | every operation type ever written must stay replayable | migrate files |
| New code, rough | merge driver about 900 lines; keeps 4,100 lines of page formats on the live path | log, fold, snapshots and projections about 2,500 lines | store, codec, merge and sync about 3,600 lines; the page formats leave the live path |
| Findings left open | merge-guard-1 (one `as_of` per page), pages-6, update-write F7, okf-7 (a protected page stops a branch) | none of the storage ones, but adds compaction and ordering problems nobody has measured | none of the storage ones |
| Outside evidence | Beads kept JSONL only as an export and moved to a database; its issue volume is migrations and sync | Mem0 and Letta both walked back from operation machinery to plain files in git | Letta's context repositories, Claude Code's and Codex's memory folders, Copilot's cited facts |

**Choice: C.** It is the lead's leaning, and the weighing supports it for three reasons.

1. Git already is an append-only, signed, mergeable operation log. Option B builds a second one inside it and then has to solve ordering, compaction and replay compatibility. One commit per pass, with trailers, gives the same audit and a simpler undo.
2. Option A looks smaller and is not. With collision-free ids and derived files out of git, a page merge driver still has to merge three line micro-formats, and it keeps the two properties the audits rate highest: a page as the unit of protection (a locked or damaged page stops all memory for its branch, okf-7, pages-6) and one clock per page (merge-guard-1).
3. In C the conflict surface is small enough to enumerate and test: section 8 lists every path class and every field with its rule.

What C costs: a repository of many small files that reads worse in a git host's file browser than v1's pages. Views answer that locally (section 4.6), and ST-D7 puts the choice to the owner.

---

## 3. The design in one page

```
 derive (worker) ── Txn{pass, ops, covers} ──► store.Apply ──► files ──► ONE COMMIT (trailers, machine-signed)
 deliver (tools)  ── Txn{remember|correct|…} ─►      │                         │
 a person         ── edits files / a checkout ─► adopted as a commit           │
                                                     ▼                         ▼
                         <data>/store   (git repo eva-md owns; main is written only by the engine)
                                                     │  change feed (paths, classes, ids)
                                                     ▼
                         <state>/…  indexes, context packs, views, code drift, usage   (derived, never in git)
                                                     │
            worker tick ── store.Sync ── fetch ─ migrate-then-merge ─ typed resolvers ─ push ──► remote (optional)
```

1. **eva-md owns the repository.** It lives in eva-md's data folder and is created at install with no question and no remote. The engine is the only writer of `main`. A person's checkout is optional and separate.
2. **Items and episodes are files; ids never collide; paths never change.** Nothing volatile or machine-relative is committed. Derived indexes and rendered views live in local state.
3. **One pass, one commit.** `store.Apply` is the only write path. The commit carries the pass id, the writer, the session and the ledger range it covers. That range, stored in the tree, is the truth about progress, so a restored store says what must be replayed.
4. **Protection is per item and lives in the store.** A locked, verified or unreadable file refuses the one operation aimed at it; the pass and its coverage go on.
5. **A machine identity.** Commits are authored by eva-md on a named machine and signed silently with a per-machine SSH key. The person's own signing is never asked for (ST-D4, with a fallback that keeps decision 7).
6. **Sync is a background job, never a precondition.** Fetch, migrate first when schemas differ, merge with typed resolvers, push without force. A rejected push is a loop, never a terminal state.
7. **The migrator and the minimum reader version exist before the new layout does.** The v1 wiki is migrated by a deterministic, resumable step that keeps the old clone as a backup.
8. **A conformance suite with a reference model** states the storage guarantees as executable properties, and is written first.

---

## 4. The store

### 4.1 Ownership and location (ownership-1, onboard-1, setup-1)

- The store is a non-bare git repository at `Dirs.Store()`, proposed `~/.local/share/eva-md/store`, mode 0700, files 0600. It sits outside the folders install and upgrade replace, outside the folders macOS protects from background processes and outside cloud-synced folders by construction (storage must-keep 16).
- `main` is written only by the engine. Everything else that wants to change memory arrives as commits the engine merges: another machine through the remote, a person through a checkout (4.7), or a stray edit inside the store folder, which the engine adopts as a commit of its own (7.3).
- Because nobody else works in this repository, the engine may do what v1 never could: abort its own interrupted merge, reset paths its own crashed transaction left dirty, remove a stale `index.lock` older than its own longest git time limit, and re-clone. v1's eleven guard refusals shrink to three checks: this is my store (store id), the remote is the bound one, and the schema and epoch are ones this binary may write.
- Storage must-keep 12 ("never commit over a state it did not make") changes form, not purpose. v1 refused to act in a clone a person might be using. v2 keeps people's git work safe by never operating in their checkout at all.
- The store API takes a root, so a second instance is cheap: tests, the evaluation harness, a shadow run and a consolidation export all use the same code on another folder.

### 4.2 Local-first creation

`store.Ensure()` is idempotent, asks nothing, uses no network, and is called by the install's reconcile step and lazily before the first write:

1. `git init -b main`, repo-local settings (4.3), the manifest, this machine's writer record, the static root files.
2. One commit, kind `init`.
3. The binding in program-owned state, `<state>/store/binding.json`: path, store id, writer id, epoch, remote identity (empty), identity mode.

Git 2.36 or newer is required (section 16). Without it the store is *deferred*: capture and the ledger go on, derive's jobs wait in their queue, and one typed condition asks for git. Nothing is lost, because the ledger is the truth.

### 4.3 Git discipline

Kept from v1, moved to a shared `internal/gitrun` (storage must-keep 8): `core.fsmonitor=false`, `core.hooksPath=/dev/null`, `--no-optional-locks`, the scrubbed environment of `identity.GitEnviron`, prompts off, English messages, a time limit per call, capped output, diagnostics classified and never stored or shown.

Added, per call and again in the repo's own config:

| Setting | Why |
| --- | --- |
| `core.symlinks=false` | no commit from anywhere can put a symlink in the working tree |
| `core.fsync` covering loose objects and references | `Apply` returns only when the commit survives power loss (storage must-keep 3) |
| `gc.auto=0`; `git maintenance run --auto` from the worker's maintain step | no garbage collection under the store lock |
| `merge.renames=false`, `-X no-renames` | paths are stable by rule, so rename guessing can only be wrong |
| `fetch.fsckObjects=true`, `transfer.fsckObjects=true` | a remote cannot hand over malformed objects |
| `add -f --literal-pathspecs -- <exact paths>` | a person's global ignore file or pathspec settings cannot change what is committed (storage must-keep 10) |
| identity and signing by `-c` and environment on each call | the person's git config is read for transport only and is never written (4.5) |

### 4.4 Locks and readers

- One `store.lock`, a flock in the state folder taken through `internal/paths/lock.go`, the one helper every writer uses (scale-1: the wiki lock exists twice today, flock helpers in six packages). One `sync.lock`, taken without waiting, so one sync runs at a time. Network calls never hold `store.lock`.
- Background readers that need a consistent tree (`Walk`, the index builder) take the lock shared. Hooks never take it.
- **Hooks never read the store tree and never run git** (storage must-keep 14). At a hook they read the compiled artifacts the deliver area builds in state. The bounded, link-safe single-file readers (`ReadMax`, `ReadHead`: `O_NOFOLLOW`, a byte limit, no folder listing, no git) stay in the API for any reader that wants one page (storage must-keep 5).

### 4.5 Machine identity and signing (signing-1; reopens decision 7)

- **Writer id.** One per machine, random, minted once, in program-owned state. It is the machine id the hosts area keeps (ledger contract C-H3); the two are one value. Form `w-` plus 8 base32 characters. It is never the host name (setup-4). Its record, `.eva-md/writers/<writer>.toml`, is committed: a label with no personal data by default (`mac-k3f9`), the public key, first seen, and the schema range its binary reads. One file per writer, so writer records never conflict.
- **One key per machine.** An ed25519 key made with `ssh-keygen`, no passphrase, 0600, under `<state>/store/keys/`. It signs every commit the engine makes (`gpg.format=ssh`, `user.signingkey`, `commit.gpgsign=true`, all per call). It can also authenticate pushes when the person enrols it once as a deploy key (`auth = "machine-key"`), which makes sync silent under a scheduler that has no agent socket. Otherwise transport uses whatever git already does for that person (`auth = "user"`).
- **Author.** `eva.md (<label>) <<writer>@machines.eva-md.invalid>`, passed by environment.
- **What the signature is for.** Sync makes the remote an input to every chat on every machine (brief F12). The signature lets a machine tell commits by its own enrolled writers from anything else (8.6). It is not for a badge on a git host.
- The person's signing program is never started, so the daily approval prompt, the 300 s wait and the word-matching for a missing signer go away. Storage must-keep 15 holds in the sense that matters: the person's settings are never changed or bypassed, because eva-md no longer commits in their name.
- **Fallback that keeps decision 7** (`identity = "user"`): per-pass commits stay local and unsigned under the machine author; at each publish the unpublished run is squashed into one commit made with the person's identity and signing settings, as v1's daily commit is. Cost: the daily approval stays, a pass can be undone singly only until it is published, the remote's history has one commit per publish, and writers cannot be told apart on fetch.

### 4.6 What is not in git

| Derived state | Home | Rebuilt from |
| --- | --- | --- |
| Retrieval index, context packs, session catalog joins | `<state>` (deliver, ledger) | the change feed and `Walk` |
| Rendered views: a workstream's Now, a project overview, maps | `<state>/views/`, and `eva-md show` | items, workstreams, episodes |
| Code drift of an item's anchors, usage and delivery counts | `<state>` | local clones; delivery records |
| id to path map, evidence-ref to item map, pass to commit map | `<state>/store/*.idx` | the tree and `git log` |
| Trust set, quarantine list, sync records | `<state>/store/` | local decisions; not rebuildable, small |

This removes every committed derived file of v1: all folder `index.md` files, `knowledge/map.md`, `systems/map.md` and `index.md`, `.eva-md/knowledge.tsv`, `names.tsv`, `repos.tsv`, the project overviews, `log.md` (model-1, systems-6, pages-10, scale-1).

The lead's "validity record" therefore splits in two. What must travel and what a person should read stays in the file: scope, anchors, last confirmed, supersedes, branch. What is relative to one machine stays in state: drift of cited code against a local clone, usage, delivery history. Committing the second kind would bring back model-1 under a new name.

### 4.7 The optional human checkout

- With a remote: a person clones the remote anywhere and works as with any repository. Their commits reach the store by fetch.
- Without one: `eva-md wiki checkout <dir>` clones the store, applies v1's folder rules (setup must-keep 12), and sets the clone to push to `refs/heads/in/<name>` in the store. The engine merges `in/*` branches at the next transaction.
- Views are rendered into a checkout's ignored `views/` folder on request, so a person can read whole pages there.

---

## 5. Layout v2

### 5.1 Tree

```
index.md                                   static; okf_version "0.2"; lists the top folders
README.md                                  static explainer for people (type: readme)
.gitattributes  .gitignore                 owned by the store; rewritten only by a schema step
.eva-md/store.toml                         schema, min_reader, store_id, epoch
.eva-md/schema.toml                        schema_version = 2, kept so a v1 binary refuses to write
.eva-md/writers/<writer>.toml              one per machine
projects/<pid>-<slug>/project.md           the project identity record (ledger contract C-S1)
projects/<pid>-<slug>/workstreams/<wsid>-<slug>.md
projects/<pid>-<slug>/items/<yyyy>/<mm>/<id>-<slug>.md
projects/<pid>-<slug>/episodes/<yyyy>/<mm>/<host>.<session>.<writer>.md
projects/<pid>-<slug>/notes/*.md           people's own text; eva-md never rewrites it
global/                                    the same four folders for scope "all" and chats outside a project
systems/<slug>.md                          system pages, carried as documents (section 5.6)
legacy/v1/…                                v1 files the migrator could not read, byte for byte
```

Rules that make it merge by construction:

1. **A path is a function of immutable things only**: the id, the creation month the id encodes, and a slug frozen at creation. A changed title, kind, scope or state never moves a file. A change of scope or kind is a new item that supersedes the old one.
2. **No file is shared by two writers unless it is one item.** Episode files carry the writer in their name. Writer records are one per writer.
3. **No fact is stored twice.** A successor names what it supersedes; the predecessor is not edited at that moment (8.3).
4. **Nothing in a committed file changes unless its content does.** No timestamp is restamped on save (okf-9).

### 5.2 Ids

| Thing | Form | Minted by |
| --- | --- | --- |
| Item | `i` + 13 lower-case Crockford base32 characters: 35 bits of seconds since 2025-01-01 UTC, then 30 bits taken from SHA-256 of the pass id and the operation's index | the store, in `Apply` |
| Workstream | `w` + the same 13 characters; derive may pass a key that replaces the hash input, so two machines agree on a workstream they both met | the store |
| Project | the ledger's registry id (`p_` + 12 characters) | the ledger |
| Episode | the file name: host, session id, writer | the store |
| Pass | a ULID minted when the job is created; the idempotency key | the worker |

- The model never writes an id, and neither does derive: code owns ids (principle B, pages must-keep 1 and 6).
- Ids sort by creation time and are 14 characters, against 26 for a ULID. They are shown to agents as handles, so length is tokens. `store.Resolve(prefix)` serves shorter display handles.
- Ids from one pass are a function of the pass, so a retried pass mints the same ids (pages must-keep 7).
- Uniqueness is by entropy and by detection: create is no-clobber, and an add of one path with two contents at merge is resolved by a fixed rule (8.3). Migrated items take their time from the v1 item and their 30 bits from the v1 instance, path, id and creation stamp, so migration is deterministic and two v1 items that shared `n3` on two Macs get two ids.

### 5.3 The item file

```markdown
---
type: decision
id: i01k2m3n4p5q6r
title: "Sign store commits with a machine key"
description: "Store commits are signed with a per-machine SSH key, never the person's."
status: stable
state: active
scope: project
project: p_k3v9x2ab7q1m
workstream: w01k2m3n4p5q6a
tag: stated
generated: {"by": "claude-code/claude-…", "at": "2026-11-10T09:14:03+01:00"}
learned: "2026-11-10T09:14:03+01:00"
asserted_at: "2026-11-10T09:14:03+01:00"
confirmed: "2026-11-12T16:02:11+01:00"
supersedes: ["i01k1zz8q0c4de"]
branch: "feature/sync"
anchors: ["github.com/acme/app@4e2bf4c:internal/store/sync.go"]
cues: ["commit signing prompt", "1Password approval every day"]
sources:
  - {"id": "e1", "resource": "claude-code:5f0c9a1e-…#142", "author": "human:owner", "hash": "9c1f…"}
  - {"id": "e2", "resource": "claude-code:5f0c9a1e-…#143", "author": "claude-code/claude-…", "hash": "07aa…"}
---
Store commits are signed with a per-machine SSH key, never the person's.[^e1][^e2]

## Detail
Reason: the daily approval prompt. Rejected: unsigned commits; the person's key with a cached approval.

## Evidence
[^e1]: "sign with a machine key so I never see the prompt"
[^e2]: "I'll generate an ed25519 key at install and pass it per call"
```

- **The envelope is OKF v0.2.** `type`, `title`, `description`, `status`, `generated`, `verified`, `sources` and per-claim footnotes keyed to `sources[].id` are the specification's own vocabulary; `author` uses its actor convention, so the three voices map to `human:<id>`, `<host>/<model>` and `process:<tool>`. `status` is derived from `state`: candidate is `draft`; active, disputed and stale are `stable`; superseded, retired and retracted are `deprecated`.
- **Kinds, states, tags and the body grammar of `## Detail` belong to the derive area.** The store validates them against a table derive owns, versioned with the schema. An unknown kind written by a newer binary is kept and read as a generic item.
- **Evidence travels as bounded excerpts** (evidence-1, search-7, ledger contract C-S2): at most 3 per item and 240 characters each, the text `ledger.Excerpt` returns, with the event id and a hash. A reference alone resolves only on the machine that captured the chat. This puts short redacted quotes of the person's chats in the remote, which v1 never did, so it is an owner decision (ST-D6).
- **Size caps**: an item 16 KiB, an episode 256 KiB (then a numbered part file), a document 24 KiB as today.
- The predecessor of a superseded item keeps its text. History is two files and the commit, so no `## History` section is needed.

### 5.4 Workstream, project and writer records

```markdown
---
type: workstream
id: w01k2m3n4p5q6a
title: "Background sync"
status: stable
state: active                      # derive's lifecycle: started, active, merged, archived
project: p_k3v9x2ab7q1m
branches: ["feature/sync"]
parent: ""
state_as_of: "2026-11-10T09:14:03+01:00"
generated: {"by": "claude-code/claude-…", "at": "2026-11-10T09:14:03+01:00"}
---
Where the work stands, as a short maintained paragraph.
```

`project.md` holds what the ledger's contract C-S1 asks for and nothing local: id, display name, remotes, root commits, v1 keys, `merged_into`, `related`, `shares_with`, first seen. Folder paths never appear. `shares_with` is the explicit relation that decides whose knowledge is recalled in whose chats; it is written only with the person's authority, which replaces v1's inference from an agent-written `repos` list (systems-m2).

### 5.5 The episode file

One per session, project and writer. It is the append-only derived record of a session and the home of coverage.

```markdown
---
type: episode
session: "claude-code:5f0c9a1e-…"
project: p_k3v9x2ab7q1m
workstreams: ["w01k2m3n4p5q6a"]
writer: w-k3f9a2bq
started: "2026-11-10T08:40:12+01:00"
generated: {"by": "claude-code/claude-…", "at": "2026-11-10T09:14:03+01:00"}
covers:
  - {"from": 1, "to": 184, "pass": "01JD…", "outcome": "done", "by": "x2"}
  - {"from": 185, "to": 230, "pass": "01JE…", "outcome": "refused", "reason": "invalid_output", "by": "x2"}
---
## Summary
…
## Log
- 2026-11-10T09:02 decided to sign with a machine key [^e1] <!-- l:01 -->
## Changes
- 01JD… +i01k2m3n4p5q6r ~w01k2m3n4p5q6a
```

- `covers` lists ledger sequence ranges with an outcome: `done`, `skipped` or `refused`, the reason as an enum, and the version of the extraction contract that produced it. Derive decides what each outcome means for replay; the store records it in the same commit as the items.
- The sections' content is derive's. The store owns the container and the append-only rule: `episode.append` adds, it never rewrites earlier lines.

### 5.6 System pages

They stay as documents under `systems/<slug>.md`, carried over unchanged apart from the map link line. The store gives them a real compare-and-swap (`doc.write` takes the hash the writer read, which closes systems-m1), the same protection guard and the same commit trailers. Their map, index, names and repos files become local derived state. Whether system pages earn further investment is for the derive and deliver areas to show by measurement; the lead's warning applies, and the store builds nothing new for them.

### 5.7 What a person may edit by hand

| A person may | How it is read | What follows |
| --- | --- | --- |
| Set `locked: true` on any item, workstream or document | fail-closed: any value that is not an explicit false, an unreadable value, or a repeated key counts as locked | eva-md writes nothing to that file. An operation aimed at it is refused with `locked`; the pass goes on. Derived facts about it (drift, use) stay in local state |
| Confirm an item (`eva-md confirm`, or a `verified` entry by hand) | any `verified` key with a value, in any YAML shape | the statement, title and detail cannot be changed by a model's operation; lifecycle fields still can, with their evidence rules. No operation open to a model writes `verified` or `locked` |
| Edit an item's text | the commit is not by an enrolled writer, or it is an adopted edit | the content takes human authority: it wins a merge against a model's concurrent change and, under derive's rules, only the person's later words change it |
| Delete an item's file | a delete by a non-eva commit | the item is kept as `retired`, reason `deleted by hand`. Real removal is `forget` |
| Add sections to any file, or files under `notes/` | unknown sections and unknown keys are opaque runs | kept byte for byte, through every later write |
| Break a file (conflict markers, no closing `---`, a 100 KiB line) | unreadable | that file is never written, never delivered, and is listed for repair. Nothing else stops |

Rendered views are not editable; they are local and rebuilt.

This is where the design departs from v1 most for a reader of the audits. In v1 a locked, verified or damaged thread page skipped the pass and moved the watermark, so every later chat on that branch was lost to memory, rules and knowledge included (okf-7, pages-6, update-write F7). In v2 the unit of protection is the file that carries the mark, and the guard is in `Apply`, for every file class, including the ones code renders (okf missed-2: `now.md` and the system map were rewritten whatever `verified` said).

---

## 6. The codec

### 6.1 Rewrite or copy

The okf verification asked for two ways of owning the code to be compared at their real sizes: copy the eight okf files eva-md reaches (2,633 lines) under their MIT notice and fix them, or rewrite and prove it with a differential test. The test is which gives a line-preserving, fail-closed reader sooner.

**Rewrite.** I read `parser.go` to decide. `ParseConcept` collects the frontmatter into a map of blocks, fills a typed struct, and `SerializeConcept` writes the struct back in a canonical key order. Comments, key order, block scalars on known keys and anything it cannot type are lost or changed by design (okf-3). A line-preserving codec has a different model at its centre, an ordered list of entries each of which is either a key eva-md owns or an opaque run of lines, and both of okf's central functions would be replaced. What would survive a copy is the id check, the actor pattern and the sanitizers, about 150 lines; they are reimplemented and held equal to okf's by a differential fuzz test while okf is still in `go.mod`.

A second reason makes the rewrite smaller than the okf audit's option B. That option asks for a writer byte-identical to okf's for today's pages. v2 replaces the layout, so nothing needs to write v1 pages again: the new codec must *read* v1 pages exactly as okf does, for the migrator, and that is a parse-equality test, far cheaper than byte-equal saves.

### 6.2 What it is

- **One reader**, streaming, with a byte bound: `ReadHead` (frontmatter only) and `ReadDoc` (frontmatter and sections) are the same code. Hook-side compilers, the index builder, the writer, the lint, the migrator and `show` all use it. `knowledgepage.Lenient`, `systempage.parseFront` and `saved.go`, `resume.frontValue`, the lint's scanner, `VerifiedIn` and `withoutGenerated` are deleted (okf-4, pages-6, systems-12).
- **Line-preserving.** Owned keys are typed. Every other line, comment and section is kept byte for byte in its place. The property `Write(Read(b)) == b` holds for every file, hand-edited or not, and is fuzzed.
- **Fail-closed on protection keys.** YAML comments are stripped before a scalar is read. `locked` and `verified` are protecting unless explicitly empty or false. A frontmatter that does not close, a line over 64 KiB, a byte-order mark or conflict markers make the file unreadable, which also protects it.
- **The writer emits only the JSON-compatible subset of YAML**: a plain scalar from a safe alphabet that no YAML reader types as a boolean, null, number or date; otherwise a JSON string, array or object with a space after each colon and comma; a block list only as `- ` plus one JSON object per line. JSON is a subset of YAML 1.2, so what eva-md writes is parseable YAML by construction, and a test can prove it with `encoding/json` and no YAML dependency.
- **One escaper**, moved from `threadpage/escape.go` unchanged in behaviour, applied by `Apply` to every free-text field (pages must-keep 8, okf must-keep 13): idempotent, and no stored text can forge a heading, fence, comment, link, label, footnote definition or frontmatter key.
- **Typed lint findings** `{kind, path, line, detail}` replace okf's English warnings and their substring matching (okf-5). No finding ever blocks a write, a commit, a push or a restore (storage must-keep 19); only the secret gate and protection refuse.
- The clock is injected. `generated.at` moves only when content does (okf must-keep 4).

### 6.3 OKF v0.2 as a written profile and a test

`docs/spec/store-format.md` is the "eva profile of OKF v0.2", pinned to the specification's commit and date. `TestOKFProfile` runs over a fixture store and over every tree the conformance suite produces, and asserts the specification's section 11 and the parts of section 5 eva-md uses:

1. Every `.md` file except `index.md` has a frontmatter block that passes the JSON-subset check, with a non-empty `type`. That includes `README.md` and the notes the migrator writes.
2. The one `index.md`, at the root, follows section 8. There is no `log.md`. v1's log would fail the specification's newest-first rule (okf-8), nothing reads it, and git history replaces it.
3. Every timestamp is ISO 8601 with an explicit offset. `stale_after`, where present, is a datetime, which is the specification's form and not the library's.
4. `generated.by` and `verified[].by` follow the actor convention.

The test checks what eva-md writes. A file a person adds without frontmatter is reported by the lint and left alone.

### 6.4 Dropping the module (reopens decision 15)

okf stays in `go.mod` on the build branch as a test oracle for the differential read test and the id fuzz test. At cut-over (ST-13) the goldens are frozen, `internal/okfadapter` is deleted, the module leaves `go.mod`, and its entry leaves `scripts/LICENSES.txt`, since no okf code is compiled in or copied. The two exceptions to "only eva's name shows", `.okf-write.lock` and `.tmp-okf-*`, end (okf-12).

If the owner keeps okf (ST-D1's fallback): each item file is saved through `okf.SaveConcept` behind the existing adapter, `locked` and `verified` are read from the raw text, the doctor stops failing on orphans, and `log.md` is dropped. That keeps the rule and leaves okf-3's hand-edit faults, the lock file in the store, the restamp of `generated` on every save and the upgrade ritual. okf keeps unknown keys but not comments, key order or the form a person wrote a value in, so the merge of a hand-edited item can drop a person's comment.

---

## 7. Transactions, history and progress

### 7.1 `Apply`: the only write path

```go
type Txn struct {
    Pass    PassID      // idempotency key
    Kind    string      // fast | consolidate | remember | correct | confirm | forget | redact | registry | undo | migrate | init | human | merge
    Actor   string      // <host>/<model>, human:<id> or process:eva-md
    Session SessionKey  // optional
    Covers  []Range     // ledger ranges this pass consumed, each with an outcome
    Ops     []Op
    Atomic  bool        // all operations or none; default is per operation
}
type Op struct {
    Type      string // item.create | item.revise | item.state | item.confirm | item.retract | ws.upsert | episode.append | project.upsert | doc.write
    ID, Base  string // target, and the blob hash the operation was computed against ("" on create)
    Fields    map[string]any
    Evidence  []Evidence // {ref, voice, excerpt, hash}
    Authority string     // model | user
    Requires  []int      // operations that must apply for this one to apply
}
type Outcome struct{ Result, Reason, ID, Rev string } // applied | noop | refused: locked, verified, unreadable, conflict, secret, invalid, unknown_id, requires
```

Steps, under `store.lock`:

1. Recover (7.3). A pass id already committed returns its recorded outcomes: exactly once per pass.
2. Plan each operation: resolve, read through the one reader, check protection, compare `Base` with the file's hash, validate structure and size against the kind table, escape free text, build the bytes, run `redact.Scan` with the `Gate` policy on every line that would be added (ledger contract C-S4; storage must-keeps 10 and 11: binary or unreadable counts as a secret), and check that the bytes read back to the same document.
3. Nothing applied and nothing covered: return, with no commit. A pass that changes only coverage still commits, since coverage in the tree is what a restore replays from.
4. Write the journal, `<state>/store/txn.json`: the pass and the exact paths.
5. Write each file: temporary file, fsync, rename, fsync of the folder and of any new parent.
6. Stage exactly those paths, read the index back, and fail if the staged set differs.
7. Commit with the machine identity and the trailers of 7.2. The commit returns after its objects and the reference are flushed.
8. Remove the journal, record pass to commit, move the generation, release the lock, queue a sync, publish the change feed.

`store.Plan(txn)` runs steps 1 and 2 and returns the outcomes without writing. That is the lint gate for a consolidation batch.

Per-operation outcomes are what lets derive accept per product. The store checks structure, protection, secrets and compare-and-swap; meaning (whose words, freshness, admission, taint) is checked by derive before `Apply`, never by the store.

### 7.2 The commit

```
fast: p_k3v9x2ab7q1m w01k2m3n4p5q6a (+2 ~1 refused 1)

Eva-Pass: 01JD3K8Q2M7V5T9R4N6W0X1Y2Z
Eva-Kind: fast
Eva-Writer: w-k3f9a2bq
Eva-Session: claude-code:5f0c9a1e-…
Eva-Covers: claude-code:5f0c9a1e-… 120-184 done
Eva-Actor: claude-code/claude-…@high
Eva-Ops: create=2 revise=1 refused=1
Eva-Schema: 2
```

Commit messages hold ids, counts and enums, never memory text (storage must-keep 13). That keeps a later purge to file paths alone.

### 7.3 Crash recovery and stray edits

On every open under the lock:

1. A journal exists: the transaction did not commit. Restore the journalled paths to `HEAD`, remove journalled paths that are untracked, delete the journal. The worker runs the job again, because a job is done only when `Apply` returned a commit.
2. `MERGE_HEAD` or a cherry-pick in progress: it is the engine's own, so abort it. Sync runs again.
3. Any other dirty path is a person's or a tool's edit made inside the store folder. It is committed as kind `human` after the secret scan. An edit that trips the scan is moved to a held reference, the working tree is restored, and a typed condition says that a hand edit was held because it looks like it holds a secret. `main` keeps flowing either way.

### 7.4 Audit and undo (history-1)

- `eva-md log [--item|--session|--since]` lists passes: time, kind, host and model, counts. `eva-md why <item>` shows the item's evidence and every pass that touched it. Both read `git log` on stable paths.
- `eva-md undo <pass|last>` is a new commit, kind `undo`, made through `Apply`. For each path the pass changed: when the file is still what the pass left, it goes back to what it was before, or is retired if the pass created it; when a later pass or a person changed it, it is left and reported. This is path-level and typed, so it cannot fail half way the way a textual revert of an old commit can.
- Undo leaves the pass's coverage in place, so the worker does not derive the same range again and add it back. `--redo` also uncovers the range, which is how a range is replayed after an extractor fix.
- A consolidation is a pass like any other. A bad one is one undo.

### 7.5 Progress is what the store holds (replay-1, update-write F9, F1)

- `store.Coverage(session)` is the union of the `covers` ranges in that session's episode files, across writers, plus the frozen v1 table the migration writes (9.3). It is in the tree, so it survives exactly as far as the memory it describes.
- The worker's job ledger is a queue and a cache. After any change of generation the worker did not cause (a restore, a merge, an undo with `--redo`, an epoch change) it reconciles to `Coverage`: a range the local ledger holds and the store does not cover is queued; a range the store covers is done.
- v1's five places of state with no transaction between them become one: the items and the range they came from land in one commit, so a rerun can always tell that the store already holds a batch.
- A refused range stays recorded with its reason and contract version, so "replay dead letters after an upgrade" is a query over coverage and not a guess.

---

## 8. Sync

### 8.1 Attach, change and remove a remote (setup missed-2, setup-10)

`eva-md remote set <address>`, also callable by the agent on request:

1. The address passes `RemoteIdentity`: no remote helpers, option lookalikes, passwords, queries or fragments, and it is never echoed (storage must-keep 9).
2. Setup's privacy rules are kept as they are (setup must-keeps 1 to 6): a public repository is refused, an internal one needs an explicit yes, nothing gh, glab or git prints is shown or kept, and the look is taken from a shallow copy in a temporary folder.
3. The remote is empty: show what will be pushed (commit count, author, signed or not) before the first push when a person is present (storage must-keep 17), then push.
4. The remote holds this store (same store id): fetch and merge.
5. The remote holds another eva-md store: **join**. The local store adopts the remote's store id and the two histories are merged with an empty base. This is the second-machine case under local-first: the machine has a day of local memory before the person attaches the remote the first machine filled. It is safe because ids are unique and paths are functions of ids; only singletons meet, and each has a typed rule.
6. The remote holds something that is not a store: refused.
7. The binding and `origin` change together under the lock. `eva-md remote remove` returns to local-only.

The privacy check is repeated, where a cheap one exists, before a day's first push: the anonymous probe that must fail. When it starts to succeed, pushing pauses and one condition says the repository is no longer private.

### 8.2 One sync

```
sync.lock (no wait)                                          no remote → record "local only"
fetch origin main                                            network; store.lock not held
store.lock:
  recover; adopt edits; merge local in/* branches
  read the remote's manifest:
     store id differs                → stop: condition wrong_remote
     epoch higher                    → adopt the new history, then re-apply unpublished local commits (8.7)
     schema above this binary's      → stop: condition reader_too_old; read and write nothing from it
     schemas differ otherwise        → migrate first, then merge (8.5)
  equal → nothing │ behind → fast-forward │ ahead → nothing to merge
  diverged → git merge --no-commit -X no-renames; resolve each unmerged path by class (8.3);
             lint; commit, kind merge, with counts by resolver
  classify the incoming commits (8.6); move the generation; publish the change feed
push origin main, never forced                               network; store.lock not held
  rejected → loop from fetch, at most 3 times, then back off
record one line in <state>/store/sync.jsonl; update the gauges
```

- A rejected push is the ordinary case of two machines, and the loop handles it. Because the remote orders pushes, two machines cannot bounce merges for ever: the one that loses the race merges the winner's head and fast-forwards it.
- `Apply` never waits for sync, and sync never runs in a hook. The worker's tick and the end of each pass start it. With the OS-scheduled tick (lead's D-A) the delay to the remote is bounded without a chat; without it, sync rides the host-driven ticks and the bar in section 1 holds only while the person is chatting.
- Failure classes keep v1's split: `pending` (sign-in or reachability; retried with backoff, and after repeated failure one plain ask), `failed`, `wrong_remote`. `diverged` no longer exists as an outcome.

### 8.3 Resolvers by path class

| Class | Paths | Rule when both sides changed |
| --- | --- | --- |
| Item | `**/items/**` | field merge, below |
| Workstream | `**/workstreams/*` | body and title as one register by (authority, `state_as_of`, writer); lifecycle state by its own time; `branches` union; `merged_into` set once, lower id on a tie |
| Project | `**/project.md` | remotes, root commits, v1 keys, `related`, `shares_with`: union; display name: newest; `merged_into`: follow to the lowest id |
| Episode | `**/episodes/**` | cannot happen with distinct writers. If it does, union the ranges and the log lines, and treat it as a duplicated writer id (8.6) |
| Document | `systems/*` | the whole file from the side with the greater (authority, `as_of`, writer); the other stays in history and is counted |
| Note or unknown text | `**/notes/*`, anything else | git's union text merge: both sides' lines kept |
| Manifest | `.eva-md/store.toml` | equal after migrate-first; `min_reader` is the higher |
| Static | `index.md`, `README.md`, git files | the canonical bytes for the schema |
| Add of one path with two contents | any id-named file | not in the base, so two different things: keep both, give the one with the greater content hash a new id derived from the old id and that hash, and record `was: <old id>` |
| Delete against modify | any | modify wins. An item deleted by a non-eva commit becomes `retired`, reason `deleted by hand` |

**Item fields.** Three-way first: a field only one side changed is taken. For a field both changed:

| Field group | Rule |
| --- | --- |
| `id`, `type`, `project`, `learned` | immutable; a difference is the add rule above |
| Content: statement, title, description, `## Detail`, `tag`, `scope`, `workstream`, `anchors`, `cues`, validity dates, `branch`, `generated` | one register: the whole group from the side with the greater (authority, `asserted_at`, writer). Authority is human over model. `asserted_at` is the event time of the newest evidence behind the statement, so the newer evidence wins and not the later save |
| Lifecycle: `state`, its time and reason | newest by the state's own time; `retracted` is final and always wins; a person's retire beats a model's change |
| `confirmed` | the later |
| `supersedes`, `verified`, evidence entries | union; evidence removed by a forget stays removed |
| `locked` | true wins |
| Unknown keys and sections | per run, three-way; on a true conflict the content winner's run, and the count is reported |

Every rule is a total order or a union, so the result does not depend on which machine merges or in what order. That property is what the conformance suite checks.

Two points earn their place:

- **A supersede does not edit the predecessor.** The successor's `supersedes` is the truth, and "superseded" is computed by the index. Two machines that each supersede the same item therefore touch no common file and cannot conflict. Both successors exist, the index marks the predecessor *contested*, and consolidation settles it. A later normalizing pass may write `status: deprecated` on predecessors for readers on a git host; it writes the same bytes on every machine, and when it does not, the rule is recompute and never merge.
- **No per-page clock.** v1's out-of-order guard compares a batch with the page's single `as_of`, so a page merged from another machine would silently ignore this machine's older chats, and two machines would give `n3` to different items (merge-guard-1). In v2 freshness is per item (`asserted_at`), and ids cannot collide.

### 8.4 What sync never does

Force a push (the one exception is the explicit purge of ST-D8), rewrite published commits, push anywhere but the bound remote, show or store what git printed, leave a state that needs a hand merge, or hold up a write.

### 8.5 Schema skew: migrate first, then merge

Two schema generations are never merged. Every migration step is a pure function from tree to tree (9.1), so:

- A machine whose binary is newer than its store migrates its own tree, commits, and then merges with a remote of the new schema. Files neither side changed migrate to the same bytes on both sides and merge as identical.
- A machine that meets a remote of an *older* schema, which is the v1 wiki another Mac still pushes to, migrates the remote's head and the merge base in a temporary worktree and merges the results.
- A machine whose binary is too old for the remote stops reading and writing shared history, keeps its unpublished commits, and raises one condition. After its upgrade it follows the first case.

### 8.6 Trust: sync makes the remote an input

v1's remote was write-only, so nobody who could push to it could reach a chat. With fetch, a commit on the remote becomes memory on every machine, and memory is shown to agents as the person's own decisions and rules (brief F12, the poisoning evidence in the research notes).

- Each machine keeps a **trust set** of writer keys in local state: its own, and those the person confirmed. It is never taken from the repository, so a commit cannot enrol its own key.
- Incoming commits signed by a trusted writer are trusted. Everything else is *foreign*: merged, never lost, and the paths it changed are listed in a local quarantine. The change feed marks those items `origin: unverified`, and delivery must not inject them unasked.
- One condition, with one plain ask, clears it: "A new machine, mac-9f2k, is writing to your memory. Is it yours?" or "Memory was changed outside eva.md: 3 items. Keep the changes?" A yes enrols the writer or acknowledges the commits.
- Edits made on this machine, in the store folder or through a checkout's `in/` branch, are trusted as the person's: anything that runs as the person can already write those files.
- A fetched commit that claims this machine's writer id and that this machine did not make means a state folder was copied. The machine mints a new writer id and key and goes on.
- This is the room for an untrusted or short-lived writer, such as a cloud session without an enrolled key: it pushes to `in/<name>` on the remote and a trusted machine merges it after the ask. It is designed for and not built in v2.0.

Without machine signing (ST-D4 answered no) there is nothing to classify by: every commit on the remote is trusted, and the documentation says that anyone who can push to the repository can write the person's memory.

### 8.7 Epoch: the one exception to "never rewrite"

`store.toml` carries an `epoch`. It rises only when history is rewritten on purpose: `eva-md forget --purge <item>` removes a file from every commit, raises the epoch, records the purged path, and pushes with a lease. Another machine that fetches a higher epoch adopts the new history and re-applies its own unpublished commits path by path through the same resolvers, dropping changes to purged paths.

Purge exists because redaction misses some secrets and a person who finds one in their memory needs a real removal. The field is in the manifest from the first version of the schema; the command is the last and optional package (ST-11, ST-D8).

---

## 9. Restore, replay and migration

### 9.1 The migrator and the minimum reader version (migrate-1)

Built and proven on synthetic steps before any v2 file format exists (ST-04).

```toml
# .eva-md/store.toml
schema = 2
min_reader = 2      # the lowest schema generation a binary must know to read this store
store_id = "s-7q1m…"
epoch = 1
```

- Every binary declares the schema range it reads and the one it writes. A binary below `min_reader` reads nothing from the store, writes nothing to it, and raises one condition. It never parses a newer layout with an older parser, which is what v1's readers would do today, since they check only the instance.
- Additive changes (a new optional key, a new kind) change neither number: readers keep what they do not know. A breaking change raises `schema` and sets `min_reader`.
- A step is `{from, to, plan, apply}` and is a **pure function of the tree**: no clock, no random ids, nothing of the machine. It is therefore idempotent, resumable, and gives the same tree on every machine, which is what 8.5 needs.
- A step runs under the store lock with a journal, tags the commit before it, and makes one commit, kind `migrate`, that also updates the manifest and the store-owned `.gitattributes` and `.gitignore`. A crash before the commit resets to the tag and runs again.
- `eva-md migrate --dry-run` prints counts only: files read by class, files to be written by class, unreadable files, people's sections, protected files, units unaccounted for.
- A hosts-area upgrade must not install a binary below the store's `min_reader`.

No v1 point release is needed for the reader rule. A v1 binary never fetches, so it can meet a v2 tree only on the machine that migrated, and there the binary was replaced. The migration also removes `wiki_path` from the config, so a v1 binary put back by hand finds no wiki and captures only. `.eva-md/schema.toml` keeps `schema_version = 2`, which makes any v1 binary that does see the tree refuse to write it.

### 9.2 Migrating a v1 wiki

Everything of the person's survives: the old clone is never modified, its whole history is kept, and every text unit of every page lands in exactly one v2 file.

1. Take v1's locks so no v1 job is mid-write.
2. Clone the v1 wiki folder into the store location. Unpushed commits come with it. A clone reads objects and references, so a stale `index.lock`, an unfinished merge or a diverged branch in the old folder does not stop it.
3. Copy the old working tree's page files over the clone and commit them as `v1 final`, so pages written since the last daily commit are included.
4. Run step 1→2 on that tree:

| v1 | v2 |
| --- | --- |
| `projects/<slug>/now.md` and its `remote` | `projects/<pid>-<slug>/project.md` with the remote and the v1 key as evidence; the project id comes from the ledger's registry, which the ledger's own migration fills first |
| A thread page | a workstream: title from the branch, `branches`, the summary as its body |
| Decisions in force, including the "decision over cap, kept" lines parked under Open questions (pages missed-3) | items of kind decision |
| Next steps, open questions | items of derive's working kinds |
| Done, last chat, the Timeline | one frozen episode archive per workstream, lines byte for byte with their `s:` references, which the ledger resolves for good |
| Any other `##` section a person wrote | `notes/<workstream>.md`, byte for byte, with a two-line frontmatter |
| A rules page | items of kind rule, project or all scope; retired rules as retired items |
| `knowledge/k<N>.md` | an item: kind, scope, state, statement, tag, references, code sources as anchors; its History section kept verbatim as a preserved section |
| A locked or verified page | every file made from it carries the same protection |
| A page that cannot be parsed | `legacy/v1/<path>`, byte for byte, listed for repair |
| `systems/<slug>.md` | the same path, minus the map link line |
| Every `index.md` but the root's, both maps, the three `.tsv` files, `log.md`, the overviews | removed from the tree; they stay in history and are rebuilt as local state where still wanted |

5. Write the frozen coverage table, `.eva-md/coverage-v1/<writer>.tsv`, from the v1 watermarks the ledger's migration converted to sequence numbers. Without it a later restore would replay, at model cost, every chat v1 already summarized.
6. Commit, kind `migrate`. Write the binding. Move `wiki_path` in the config to `legacy_wiki_path`.
7. If the v1 wiki had a remote, it becomes the store's remote and the commits are pushed as a fast-forward of the v1 history. If that remote has commits this machine lacks (v1's `diverged`), pushing stays paused with a condition until the sync engine (ST-09) merges it by 8.5.

Checks: a property test migrates generated v1 wikis and finds every text unit exactly once; the fixture wiki has a golden result; the dry run on a copy of the owner's wiki must report 0 unaccounted units before the real run. For 14 days `eva-md migrate --abandon` clears the v2 binding and restores `wiki_path`, after which a v1 install works on the old folder exactly as it was; the v2 store is set aside, not deleted.

The inbox is migrated by the ledger area. This migration does not touch it.

### 9.3 Restore and replay (restore-1, replay-1)

| Situation | What `Ensure` and the worker do |
| --- | --- |
| A new machine | local store at install; `remote set` joins; nothing to replay there |
| The store folder is lost or fails `git fsck`, a remote is bound | move what is left aside; clone into a temporary folder on the same volume; check the store id and schema; rename into place. The generation resets, so derived state is rebuilt, and the worker reconciles to `Coverage` and queues what the local ledger holds beyond it |
| The same, with no remote | a new store with the same store id; the worker replays the ledger, newest sessions first, inside its quota gates; a condition says memory is being rebuilt from the record |
| A remote was replaced or purged elsewhere | the epoch path of 8.7 |

A restore never validates before it adopts. The only gates are the store id and the schema range. Unreadable files are flagged in the index, and derived files cannot be wrong because none are stored.

Explicit writes by the person (remember, correct, confirm, forget) must also be events in the ledger, or a rebuild from the record would lose them. That is a contract with the deliver and ledger areas (S-L3).

---

## 10. The conformance suite

`internal/store/conformance`, a test-only package nothing imports, written against the store's interface before the store (ST-02).

- **A reference model**: a small in-memory state machine that implements the merge and protection rules of sections 5 to 8. It is the executable form of the specification. The real store must agree with it on every schedule.
- **Machines**: 2 to 5, each a temporary home, state folder, store, writer id and injected clock; one bare repository as the remote.
- **Schedules**, generated from a seed: passes with random operations, hand edits in the store folder and through a checkout, syncs, a crash at each named step of `Apply`, of a merge and of a migration (the step returns and the machine reopens; nothing is killed and nothing is timed), a rejected push, an unreachable remote, a deleted store folder, clocks that disagree, a machine on an older schema, a copied state folder, an unsigned commit pushed straight to the remote, a planted line that looks like a secret.

| Property, after every machine has synced to quiet | Finding it guards |
| --- | --- |
| All trees are byte-identical, and the same seed gives the same tree | sync-1, model-1 |
| Every applied operation is visible: its item exists with that content or with content that wins by the stated order, and the loser is in history | durability-1 |
| A retired, superseded or retracted item never comes back without a newer operation | merge-guard-1 |
| Coverage never shrinks; after a restore and replay it is at least what it was | replay-1 |
| A locked file's bytes never change; a verified statement never changes | okf-3, okf-7, okf missed-2 |
| After a crash at any step the store holds the state before or after the transaction, never between, and no job is done without its commit | update-write F9 |
| A clone of any pushed head opens, and replay queues exactly the uncovered ranges | restore-1 |
| No planted secret is reachable from any pushed reference | storage must-keeps 10, 11 |
| Every push went to the bound remote, and none was forced | storage must-keep 9 |
| Ids are unique across all machines | model-1, pages-7 |
| Every tree passes `TestOKFProfile` | okf-8 |
| A machine below `min_reader` wrote and read nothing shared; after its upgrade it converges | migrate-1 |
| An unsigned commit's items are marked unverified until acknowledged | brief F12 |
| No schedule ends in a state a person must repair by hand; every stop is one of the typed asks of section 1 | ownership-1 |

Each property has at least one deliberately broken variant of the model that the suite must catch, so a property that cannot fail is noticed. The suite runs offline with `t.TempDir()` and `testgitenv.Clear()`, has no sleep, and logs its seeds. CI runs 500 schedules; a nightly job runs 10,000. v1's kill-during-save and concurrent-writer tests are ported into it (okf must-keep 17).

---

## 11. Kept, rebuilt, removed

### 11.1 Kept as code

`wikigit/git.go` and `scan.go` (the runner, `RemoteIdentity`, `SecretLines`), moved to `internal/gitrun` and the store; the adapter's path and link safety, atomic write, folder fsync and bounded reads; `paths/lock.go`; `threadpage/escape.go`; `okfadapter.Actor`; setup's privacy checks and peek; init's folder rules, for the checkout. The v1 page parsers (`threadpage`, `knowledgepage`, `systempage` frontmatter) are frozen under `internal/store/migrate/v1` with their tests, as the migrator's readers.

### 11.2 Invariants

All 19 storage must-keeps, the 17 of the okf audit and the store-facing ones of the pages, systems, setup and search audits are kept. Five change form, each on purpose:

| Invariant | v1 | v2 | Why |
| --- | --- | --- | --- |
| Storage 12: never commit over a state it did not make | refuse on any merge, rebase, conflict, detached head | the store is eva-md's alone; its own interrupted work is aborted and redone; a person's git work happens elsewhere | ownership-1 |
| Storage 15: the person's signing is never changed or bypassed, and a failed signature is never retried | commits use the person's signing | eva-md commits as a machine and never starts the person's signer | signing-1; ST-D4, with a fallback that keeps v1's rule |
| Storage 9: never a forced push | absolute | absolute, except the explicit purge with an epoch | ST-D8; absolute again if the owner says no |
| okf 10: no upgrade rewrites pages on its own | holds | one explicit, versioned, reversible migration; it holds again within a schema | migrate-1 |
| Pages 9 and okf 8: a locked or verified page is never written | the page, and with it the branch's memory | the file that carries the mark | okf-7, pages-6 |

### 11.3 Rebuilt, with what a smaller change would leave

| Rebuild | Findings | What a smaller change leaves unsolved |
| --- | --- | --- |
| Own codec and store, okf dropped | okf-1, okf-3, okf-4, okf-5, okf-6, okf-12, okf missed-2, pages-10, systems-12 | Three patches are possible under okf and worth knowing: read `locked` from the raw text, stop failing on orphans, drop `log.md`. They leave a reader that loses a person's comments and rewrites what it cannot type, which a field merge cannot be built on, the lock file in the store, and six readers |
| Layout v2 | model-1, merge-guard-1, pages-6, pages-7, systems-6, update-write F7, okf-7, okf-9 | Collision-free ids and derived files out of git, on today's pages, remove most add conflicts. They leave one clock per page, the page as the unit of protection, and a page merge driver still to write |
| One commit per pass, coverage in the tree | history-1, durability-1, replay-1, update-write F9 | A daily commit with a pass log beside it gives audit and no undo, and progress still lives outside what it describes |
| An eva-md-owned repository, local-first | ownership-1, onboard-1, setup-1, setup-12, scale-1 | Clearing only eva-md's own stale locks in the person's clone leaves the race with their git work, the guard ladder, and a remote before the first memory |
| Machine identity | signing-1, setup-4, part of setup-8 | Caching the person's approval still fails for a signer off the app's PATH and still speaks in their name |
| Sync engine | sync-1, setup-4, setup missed-2, setup-10, brief F12 | On layout v2 a plain `git pull` would merge most days. It leaves the same-item cases to a text merge, where a lost retire brings back what the person retired, and it has no answer to who wrote a commit |
| Restore and replay | restore-1, replay-1 | Loosening the adoption gate alone restores the files and still loses what was summarized since the last push |
| Migrator and minimum reader | migrate-1 | nothing smaller exists; without it no layout can ever change |
| Evidence excerpts | evidence-1, search-7 | References alone are honest on one machine and empty on the next |
| Conformance suite | storage:eval-1, okf-11 | Unit tests per resolver do not show convergence |

Where evolving wins, it is chosen: the git runner, the secret scan, the path safety, the atomic writer, the lock helper, the escaper and setup's privacy checks are kept as they are, and the daily commit's push classification is kept inside the loop.

### 11.4 Removed

The okf module and its `LICENSES.txt` entry; `internal/okfadapter`; `log.md`; every committed derived file (4.6); the map link line on every knowledge and system page; the restamp of `generated`; per-page `as_of`, `head`, `writer` and `model` stamps; the machine-named `instance`; eight of the eleven guard refusals; the `attempt-<date>` and `push-retry` markers and the daily lump commit; the 300 s signing wait and the signer word-matching, in machine mode; init's remote and folder requirement and the "one wiki per machine" question; the adoption gate and `BlockingWarning`; `eva-md commit --push-only`, replaced by `eva-md sync`; five of the six frontmatter readers; the second implementation of the wiki lock; `commit.lock`, `maintain.lock` and `init.lock`, replaced by `store.lock` and `sync.lock`.

---

## 12. Contracts

Ledger contracts C-S1 to C-S4 are the ledger draft's ids; the store accepts all four.

| Id | With | Direction | One line |
| --- | --- | --- | --- |
| C-S1 | ledger | promise | The store holds one project identity record per project (id, display, remotes, root commits, v1 keys, `merged_into`, `related`; no local paths); `store.Projects()` seeds the registry and `project.upsert` exports to it |
| C-S2 | ledger | need | `ledger.Excerpt(id, span)` returns gate-clean text with the event id and a hash; the store commits only that text as evidence |
| C-S3 | ledger | promise | `store.ItemsCiting(event ids)` and a `forget` transaction: an item with no evidence left becomes `retracted` and its text is blanked; other items lose the excerpt; history keeps what was pushed unless purged |
| C-S4 | ledger | need and promise | `redact.Scan(text, Gate)` gates every line `Apply` adds; when `redact.Version` rises the store rescans every file and masks hits in a `redact` transaction |
| S-L1 | ledger | need | Event references are stable and opaque to the store; v1 `s:` references resolve for good, so no migrated file is rewritten for them |
| S-L2 | ledger | need | The ledger's migration runs before the store's and supplies project ids for v1 keys and the v1 watermarks as sequence numbers |
| S-L3 | ledger, deliver | need | Remember, correct, confirm and forget are ledger events as well as transactions, so a rebuild from the record replays them |
| S-L4 | ledger | promise | When two project records from two machines share two kinds of evidence, the store writes `merged_into` on the younger by the ledger's own two-evidence rule; root commit alone only sets `related` |
| S-D1 | derive | promise | `store.Apply(Txn)` is the only write path: typed operations, one commit, per-operation outcomes, exactly once per pass id; `store.Plan` is the same without writing |
| S-D2 | derive | need | Derive validates meaning before `Apply`; it never writes files, runs git, or supplies an id, a date or a path |
| S-D3 | derive | promise | `store.Coverage(session)` is the truth about derived ranges; the worker's job ledger reconciles to it after any generation change it did not cause, and v1 watermarks reach derive already as coverage |
| S-D4 | derive | need | Derive owns the table of kinds, states, tags and working kinds, and the body grammar of `## Detail` and of episode sections, versioned with the schema |
| S-D5 | derive | promise | A refused operation never holds coverage hostage; refused ranges are recorded with reason and contract version for replay |
| S-D6 | derive | promise | Consolidation reads a snapshot at a generation and submits operations with their `Base` hashes; there is no candidate branch (section 13, point 2) |
| S-D7 | derive | promise | A merge report lists what a merge dropped under a lock or by authority, so derive can propose it again as a dispute |
| S-V1 | deliver | promise | `store.Changes(since)` and `store.Generation()`; a reset tells consumers to rebuild from `Walk`; nothing derived is committed |
| S-V2 | deliver | promise | One reader: `ReadHead`, `ReadDoc`, `Walk(class)`, `Resolve(prefix)`; deliver never parses frontmatter |
| S-V3 | deliver | need | Items flagged `unreadable` or `origin: unverified` are never injected unasked; `contested` and `locked` are shown as such |
| S-V4 | deliver | need | Hooks read only compiled artifacts in state; views, usage, delivery records and code drift stay local |
| S-V5 | deliver | need | The write tools call `Apply` with `Authority: user` only when a user-voiced event backs it; `shares_with` changes only that way |
| S-H1 | hosts | need | The reconcile step calls `store.Ensure()` and then `store.Migrate()`; no install, upgrade or uninstall writes or removes the store folder unless the person asks to delete memory in so many words |
| S-H2 | hosts | need | The scheduler tick calls `store.Sync` and `store.Maintain` (garbage collection, `fsck`, the redaction rescan); until it exists, pass end and chat start do |
| S-H3 | hosts | promise | Typed conditions for the freshness ledger, each with a remediation class: `unsynced{age}`, `sign_in_needed`, `wrong_remote`, `new_writer`, `foreign_changes`, `reader_too_old`, `held_edit`, `remote_not_private`, `git_missing`, `rebuilding` |
| S-H4 | hosts | need | The machine id in program-owned state is the store's writer id; an upgrade never installs a binary below the store's `min_reader` |
| S-H5 | hosts | promise | `store.Attach`, `store.MachineKey` and the commands `remote set|show|remove`, `sync`, `wiki checkout`, `undo`, `log`, `why`, `migrate --dry-run`; hosts own the wording and the thin gh and glab helper that creates a repository and enrols the key |
| S-E1 | evaluate | promise | The conformance suite, the scale benchmark and the sync records report in the results store's format: `time_to_durable`, `time_to_synced`, `unsynced_oldest_age`, resolutions by class, restore success, convergence |
| S-E2 | evaluate | need | The corpus audit reads through the one reader; poisoning tests inject through foreign commits, so they exercise 8.6 |

---

## 13. Where the lead's draft is wrong or over-built

1. **Sync makes the remote an input, and the draft does not say so.** L7 lists a writer id and a machine identity as conveniences. They are a security boundary: a fetched commit becomes text an agent is told is the person's. The minimum is section 8.6, and it should gate the first release that fetches.
2. **A candidate branch or worktree for consolidation is over-built.** Letta uses worktrees to let several memory agents write at once. eva-md has one worker. `store.Plan` gives the candidate and the lint gate in memory, per-operation compare-and-swap handles passes that land meanwhile, and one commit with path-level undo gives the way back. If derive wants a model to browse memory with file tools during consolidation, the store exports a read-only copy and takes back operations, never files.
3. **The validity record must be split.** "Drift of cited code" is relative to one machine's clone and changes without any write. Committed, it would conflict on every machine every day. It belongs in local state with usage and delivery counts.
4. **Views should be out of git, not only indexes.** A committed rendered page must be rewritten after every merge and by every binary version in its own format, which produces commits whose only cause is a renderer. The draft's "views rendered by code" is right; they are local (ST-D7).
5. **Do not rebuild okf's writer.** The okf audit's option B asks for byte-identical saves of today's pages. The layout is being replaced, so only reads of v1 pages need to match, and only for the migrator.
6. **ULIDs are the wrong default id.** Ids are shown to agents as handles. Fourteen characters with collision detection cost about half the tokens of a ULID and lose nothing at this scale.
7. **Collision-free ids are not enough by themselves** for the registry and for workstreams: two machines that meet the same repository before they sync make two projects and two workstreams. The draft needs the redirect (`merged_into`) and the two-evidence rule the ledger draft has, in the store, as merge rules.
8. **"Fetch and merge before each push" must not become "before each write".** Writes never wait for the network. Cut-over can ship push-only, with fetch and merge in Stage 4, and nothing in Stage 2 depends on it.
9. **The store should not carry a v1 extractor on a v2 store.** A shim that maps v1's page operations onto items would port the code three areas are rebuilding. Store, derive and deliver cut over together behind one flag, and before that derive can run in shadow against a scratch store, which costs the store nothing.
10. **Team memory is not designed here.** Enrolment, trust tiers and `in/` branches leave room for it. Nothing is built for it, and the plan should say so.

---

## 14. Owner decisions

| Id | Question | Options | Recommendation | Why | If no |
| --- | --- | --- | --- | --- | --- |
| ST-D1 (lead's D-C) | Drop the okf module and own the page codec, keeping OKF v0.2 conformance as a written profile and a test? Reopens decision 15 | drop; keep pinned | Drop | The format is kept and the library is not needed for it. A field merge and fail-closed protection need a reader that keeps every line, which okf's model cannot | Items are saved through okf behind today's adapter, with protection read from the raw text. Cost: comments and the form of hand-written frontmatter are not preserved, okf's lock file sits in the store, every release stays a risk |
| ST-D2 | May eva-md own the memory repository in its own data folder, with a person's checkout optional? Reopens decisions 1 and 16 (a folder you chose) | owned store; the person's clone as today | Owned store | It is what lets the store heal itself and what removes the folder question | The store stays the person's chosen folder. Cost: the guard refusals stay, a stray merge or lock still stops memory until a person acts, and sync must refuse whenever the tree is dirty |
| ST-D3 | May memory start with no remote and no question, and a remote be attached or changed later? Reopens decision 1 (a separate private repo) on when, not whether | local-first; remote at setup | Local-first | Four questions and a push stand between install and the first memory today | A remote is asked for at setup as in v1. Cost: every failure of the git host or sign-in is a failure to install memory |
| ST-D4 (lead's D-L) | May commits be made as "eva.md on <machine>" and signed silently with a per-machine SSH key, the person's signing never asked? Reopens decision 7 | machine identity signed; machine identity unsigned; the person's identity | Machine identity, signed | Memory commits are machine records. The signature is what lets a machine tell its own writers from anyone else on fetch | `identity = "user"`: local per-pass commits, squashed at each publish into one commit with the person's identity and signing. Cost: the daily approval stays, single-pass undo ends at publish, the remote has no per-pass history, and fetched commits cannot be told apart |
| ST-D5 | May several of one person's machines share a store through background fetch, merge and push? Reopens decision 1 ("one wiki per machine", "no syncing between machines") | sync; push-only backup | Sync, in Stage 4, gated on the conformance suite | A memory that stays on one disk cannot follow the person, and the hosts leave exactly this gap | Push-only with per-pass commits. Cost: a second machine is still unsupported; saves ST-09 |
| ST-D6 | May short redacted excerpts of cited chat lines be committed beside the items they support (at most 3 of 240 characters each)? Reverses "no captured text is ever committed" | excerpts; references only | Excerpts | Span-checked evidence is the extraction contract's base, and without the span it cannot be read on another machine or after a reinstall | References only. Cost: on any machine but the capturing one an item is an unverifiable summary, and consolidation can run only where the chat was captured |
| ST-D7 | Should rendered pages (a workstream's Now, a project overview) be committed so they read well on a git host, or stay local? | local; committed | Local | Committed renderings add merge work, churn and renderer-version noise for a reader who has `eva-md show` and a checkout with views | A small set is committed and regenerated after every merge, with a renderer version stamp. Cost: one more package (M) and commits caused only by rendering |
| ST-D8 | May `forget --purge` rewrite history and force one push, marked by an epoch, as the only exception to "never force"? | yes, explicit only; no | Yes, built last | Redaction misses some secrets, and retiring a file leaves it in history | Forget retires and blanks; removing a pushed secret is a documented hand procedure; "never force" stays absolute |

---

## 15. Risks

1. **Injection through the remote.** Addressed by 8.6. Residual: with signing declined, or once a person has said yes to a writer, that writer's commits are trusted whole.
2. **A migration nobody could rehearse on real data.** One real wiki exists. The dry run on a copy, the untouched old clone and the 14-day abandon are the answer; the first real run should be watched.
3. **The real install's self-hosted GitLab may refuse machine commits.** Push rules that require a known committer email or verified signatures would reject them. Section 16 confirms this first; the fallbacks are an author address the person chooses and `identity = "user"`.
4. **Background git that triggers an approver.** An SSH agent that asks per use would raise prompts at arbitrary times. Attach measures whether a non-interactive read succeeds; where it does not, the machine key as a deploy key is offered, and attempts are rate-limited after one that needed approval.
5. **Clock skew.** The content and lifecycle rules use times. A machine with a wrong clock can win a same-item conflict it should lose. It is bounded to that case, the loser stays in history, and the count is reported.
6. **Many small files and commits.** Month folders keep tree objects small; maintenance packs them. Growth per thousand passes and the cost of the dirty-tree check at 50,000 files are measured in ST-12 before anything is tuned.
7. **Two schema generations in the field.** A machine left on an old version stops sharing until it is upgraded. How loud that is depends on the hosts area's answer on self-update.
8. **Readability on a git host.** A person who liked reading v1 pages in a browser gets a tree of small files (ST-D7).
9. **Sync depends on the tick.** Without the OS-scheduled tick the delay to the remote is unbounded while no chat runs. A job spawned by a hook may also run inside a host's sandbox; whether such a job can reach the network is a field question.
10. **A cut-over of three areas at once.** The shadow run against a scratch store, the dry run and the abandon window reduce it; they do not remove it.
11. **A hand edit that races a pass.** A person saving a file in the store folder while a transaction runs can have the edit swept into that pass's commit. The content is kept; the attribution is wrong.
12. **The purge path is rare and therefore little exercised.** It is in the conformance schedules, and it is optional.

---

## 16. Host facts this design leans on, and how each is confirmed first

| Fact | Used by | Confirm before building |
| --- | --- | --- |
| The Mac's git is 2.36 or newer and supports SSH signing by `-c`, `core.fsync` for objects and references, `merge --no-commit -X no-renames`, and `core.symlinks=false` as tested on Linux | 4.3, 4.5, 8.2 | `git --version` in the field report; ST-00 runs the scratch script of this design's experiment on a Mac and records each result |
| `ssh-keygen` is present and can make and use an ed25519 key with no passphrase | 4.5 | the same script |
| The exact `core.fsync` value that flushes loose objects and references, and that git uses a full flush on macOS | 4.3 | read `git help config` for the installed version; a power-loss test is not possible, so the claim is limited to what git documents |
| The self-hosted GitLab accepts commits by an unknown author address and unsigned or SSH-signed by an unregistered key | 4.5, risk 3 | field question on push rules; ST-00 pushes one machine commit to a scratch repository on that server, with the owner's leave |
| Deploy keys with write access exist on the hosts in use, and the current `gh` and `glab` can add one | 4.5 | read both CLIs' help at the installed versions; one manual run per host |
| A non-interactive `git ls-remote` either succeeds silently or fails fast under the person's credential setup | risk 4 | ST-00 on the real install with prompts off and a short time limit |
| A job started by the OS scheduler can reach the remote: agent socket, credential helper, network | 8.2 | with the hosts area's tick prototype |
| A background job spawned from each host's hook can run git against the network | risk 9 | field question; the v1 commit records by day show whether pushes succeeded |
| macOS lets a background process read and write the proposed store folder without a privacy prompt | 4.1 | ST-00 |

---

## 17. Work packages

Sizes are relative: S a few days of one agent, M about a week, L two or more. Stages are the plan's: 0 ground truth, 1 foundations, 2 core rebuilds, 4 reach and sync, 5 proof.

| Id | Stage | Size | Outcome | Scope | Depends on | Acceptance |
| --- | --- | --- | --- | --- | --- | --- |
| ST-00 | 0 | S | The host facts of section 16 are confirmed or refuted | a scratch script and a short record; no product code; nothing run on the real wiki | the field report | Each row of section 16 is marked with evidence; any refuted row names the fallback it switches on. Measure: 9 facts with an answer |
| ST-01 | 1 | M | The store's written specification and the OKF profile | `docs/spec/store-format.md`: layout, id grammar, each file format, the kind table's shape, every path class and field with its merge rule, protection, the trailer grammar, schema, `min_reader`, epoch; the decision records it reopens. Out: any code | ST-D1 to ST-D7 answered | A reviewer finds a rule for every field and path class; the OKF specification is pinned by commit; a hand-written fixture store passes the profile checker. Measure: fields without a stated rule, 0 |
| ST-02 | 1 | L | The conformance suite and its reference model | `internal/store/conformance`: model, machine harness, schedule generator, fault points, the properties of section 10; runs against the model through the store interface. Out: the real store | ST-01 | Each property is caught failing by at least one broken model variant; 1,000 seeded schedules pass on the model; no sleep; seeds logged. Measure: properties with a killing variant, all |
| ST-03 | 1 | M | One line-preserving, fail-closed codec | `internal/store/codec`: reader, writer, JSON-subset emitter, protection, the moved escaper, typed lint; okf as a test oracle on this branch. Out: any caller | ST-01 | `Write(Read(b)) == b` over a corpus of hand edits (comments, block scalars, repeated keys, CRLF, a 100 KiB line, a byte-order mark); `locked: true  # x` is locked; every unreadable protection value protects; parse equality with okf on the fixture wiki and 10,000 generated v1 pages for the keys eva-md uses; id check equal to okf's under a differential fuzz; 10 minutes of fuzzing without a panic. Measure: protection misreads on the corpus, 0 |
| ST-04 | 1 | M | The migrator framework and the minimum reader rule, before any new layout | `internal/store/migrate`: steps, journal, tag, dry run, pure tree steps in a temporary worktree; the reader gate; store-owned git files. Proven on synthetic steps. Out: step 1→2 | ST-01 | A crash injected at every step boundary reruns to the same tree id; the same tree migrated on two simulated machines gives one tree id; a binary below `min_reader` reads nothing, writes nothing and raises one condition. Measure: crash points that converge, all |
| ST-05 | 2 | L | The store core | `internal/store`, `internal/gitrun`: `Ensure`, `Apply`, `Plan`, the guard, the secret gate, journal and recovery, adoption of stray edits, episodes and coverage, the change feed, the machine identity and key, the readers. Out: sync, migration of v1 | ST-03, ST-04 | The single-machine properties of the suite pass on the real store; v1's kill-during-save and concurrent-writer tests pass, ported; one commit per pass in an integration test with a fake derive; a hook test passes with the store folder unreadable; a locked item is byte-identical after 1,000 random passes aimed at it. Measure: `time_to_durable` emitted and under 2 s p95 in the benchmark |
| ST-06 | 2 | S | A remote can be attached, changed and removed, and passes are pushed | `store.Attach`, the privacy checks kept, push with backoff and v1's classification, sync records, the two gauges and their conditions. Out: fetch and merge | ST-05 | Scripted: empty remote, the same store, a non-store, a public repository, a changed address, removal; nothing git printed appears anywhere; a push is never forced. Measure: `unsynced_oldest_age` reported; `time_to_synced` under 10 min p95 on a simulated day with a tick |
| ST-07 | 2 | L | A v1 wiki becomes a v2 store with nothing lost | step 1→2, the mapping of 9.2, the frozen v1 readers, the coverage table with the ledger area, the binding and config change, `--abandon`. Out: merging a diverged v1 remote | ST-05, the ledger's migration | A property test over generated v1 wikis finds every text unit exactly once; the fixture wiki matches its golden; two runs give one tree id; the old clone is byte-identical after the run; protected pages yield protected files; the dry run on a copy of the owner's wiki reports 0 unaccounted (owner-run, counts only). Measure: unaccounted units, 0 |
| ST-08 | 2 | M | Audit, undo, restore and replay | `log`, `why`, `undo [--redo]`, the restore paths of 9.3, `Coverage` and the worker's reconcile hook, `fsck` in maintain | ST-05 | Undo of pass k after later passes restores the untouched files and reports the rest; delete the store folder with a remote bound: the next tick restores it and queues exactly the uncovered ranges; with no remote, replay with canned model output reaches the same items by content. Measure: restore success over every pushed head of 200 schedules, 100% |
| ST-09 | 4 | L | Machines converge | fetch, the merge and its resolvers, join, migrate-then-merge, trust classification and enrolment, the duplicated-writer case, the loop and its backoff | ST-02, ST-05, ST-06, ST-D5 | The whole suite passes on the real store, 500 schedules in CI and 10,000 nightly; no schedule ends needing a hand repair of git; an unsigned commit's items are held from delivery until acknowledged; a live check, with the owner's leave, on GitHub and on the self-hosted GitLab: attach, a second machine joins, concurrent edits converge. Measure: convergence 100%; terminal states 0 |
| ST-10 | 4 | S | A person can work on memory by hand | `wiki checkout`, the `in/` branch merge, views rendered into a checkout, the hand-edit table of 5.7 end to end | ST-09 | Scripted edits each land as the table says: a text edit, a lock, a delete, a new note, a broken file, a planted secret (held, not synced). Measure: hand-edit cases as specified, all |
| ST-11 | 4 | M | A secret can be removed from history | `forget --purge`, the epoch change, adoption on other machines. Optional (ST-D8) | ST-09 | In the suite: after a purge and sync no machine can reach the blob from any reference, and unpublished work on other machines survives. Measure: purged content reachable, 0 |
| ST-12 | 5 | M | Scale and soak figures | a benchmark at 1,000, 10,000 and 50,000 items: `Apply`, the dirty check, a merge of 100 conflicted items, a cold restore, a full walk; a simulated month of 200 sessions on 3 machines; repository growth per 1,000 passes | ST-09 | Figures in the results store against the budgets of section 1, or a recorded decision to change a budget. Measure: `Apply` p95 at 10,000 items under 250 ms |
| ST-13 | 2 | S | The cut-over, and okf gone | delete `internal/okfadapter`, `internal/wikigit`'s commit path, `internal/wikiinit`'s clone flow and the live use of the v1 page packages; remove okf from `go.mod` and `LICENSES.txt`; update `docs/design.md`, `decisions.md`, `known-limitations.md`, `verification.md`, the README | ST-07, ST-08, derive's and deliver's cut-over | `go list -deps ./cmd/eva-md` names no okf package; one frontmatter reader in non-test code; the licenses test passes; `scripts/linux-check.sh` passes. Measure: readers 6 to 1; modules 2 to 1 |

Order: ST-01, then ST-02, ST-03 and ST-04 side by side, then ST-05, then ST-06, ST-07 and ST-08 side by side, then ST-13 with the other areas, then ST-09, ST-10 and ST-11, then ST-12.

---

## 18. What a field report from the real install should answer for this area

1. Which git version the Mac runs, whether `ssh-keygen` is there, how commits are signed today (not at all, GPG, SSH) and whether signing or pushing shows an approval prompt.
2. How the wiki's remote is reached (SSH or HTTPS, what kind of agent or helper), and whether a read with prompts off succeeds without anyone touching the machine.
3. Whether the GitLab server restricts pushes: committer address rules, required signatures, protected branches, deploy keys with write access.
4. A census of the wiki in counts only: pages by type, knowledge by kind and state, projects, the largest pages, total commits, repository size, files.
5. How many pages are locked, verified or unreadable, and how many carry sections a person wrote. Whether any commit in the wiki was made by hand.
6. The commit and push record over the install's life: outcomes by reason, days with activity and no commit, paths left out, validation error counts, and whether `diverged` ever appeared.
7. How many commits are unpushed now, and how old the oldest page change is that has not left the machine.
8. Whether any second machine was ever pointed at this wiki.
9. Whether pushes succeed whichever host's hook started the job.
10. How far the update's watermarks are from the end of the inbox: sessions fully summarized, behind, never summarized.
11. Whether lock or temporary files of the page library are lying in the wiki, and what `validate` counts at the current head.
12. Whether the remote is reachable only on a VPN, and what share of push attempts ended as pending.
13. For the owner to fill in: do you ever open the wiki folder or read it on GitLab; have you edited a page by hand; would you miss reading whole pages in a browser.
