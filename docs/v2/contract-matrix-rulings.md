# The contract matrix: the lead's rulings

These are the lead's rulings of 2026-11-12 on the eleven needs [the contract matrix](contract-matrix.md) left open, on LG-22's fake and on its three dropped rows. The matrix holds their result.
The plan changes only at a stage gate, so the plan's words below are written into [plan.md](plan.md) at the Stage 0 readout and not before; until then the cards cite this record. None of the twelve waits for a Stage 0 number.

## 1. C-S2: the words a rule or constraint quotes

**Rows:** C-S2 (store.md), now ST-01, DV-21; C-D4's Where cell, which pointed here.

**Question:** Which package gives `ledger.Excerpt`, gate-clean with the event id and a hash, for the words decision 33 commits with a rule or a constraint: LG-21, DV-21 or ST-05?

**Ruling:** v2.0 builds no `ledger.Excerpt` (excerpts beside items are section 12's). DV-21's checker fills a rule's or constraint's `words` from the typed span it verified, and puts each cited event's id and a `sha256:` hash of the quote into `store.Evidence`, fields of ST-01's format. ST-05 reads no record; its gate (C-S4) refuses flagged words with `secret`.

**Why not the other:** LG-21 cannot know which span a model cited, and ST-05 copying spans would check meaning in the store, which store.md 7.1 and S-D2 keep in derive.

**Plan words:** none.

**Cards:**
- DV-21: `words` is the first verified cited span in `ev` order with `pv` `typed` and `supports` `stated`, never joined or cut; with none, the operation is refused. `Evidence.Hash` is `sha256:` and the hex digest of the quote as `ledger.Verify` matched it.
  Redaction runs on `words`; ST-05's `secret` refusal is a counted outcome. Tests on IF-01c's fake, which keeps `words` and `Evidence` as given.
- ST-05: depends on no DV package; holds `words` to the format and checks `Hash` for its form only.
- The lead: one clause in store-format.md's `sources` row saying what `hash` covers. A flag: a hash of a span of 8 characters can be confirmed by guessing from a word list.

## 2. C-S3: what cites a forgotten event

**Rows:** C-S3 (ledger.md), now ST-05.

**Question:** Which package gives `store.ItemsCiting` and the forget transaction that retracts an item left with no evidence: ST-05, ST-08 or LG-30 itself?

**Ruling:** ST-05 adds `store.ItemsCiting(event ids)` to the item readers it owes behind `store.Store`, and defines an `Apply` pass of kind `forget`: an item every source of which is forgotten becomes `retracted`, reason `forgotten`, text cleared; any other citing item loses those sources, their footnotes and its `words`.
LG-30 writes its tombstones, asks the store, and applies one user-authority pass. ST-08 must keep a forget from being undone.

**Why not the other:** LG-30 rewriting items through the codec would make a second owner of the store's format, writing outside the store lock, where all three designs put the cascade in the store.

**Plan words:** none. Outside the plan the lead writes a dated record in docs/decisions/ amending store-format.md, which ST-05's card cites as a dependency:
1. Field table, row `words`: "required unless migrated or removed by a `forget` pass".
2. Section 8 or the item section: "A pass of kind `forget` retracts an item every source of which is in the forgotten set, with `state_reason` `forgotten`, and replaces its title and statement with a fixed placeholder and removes its `## Detail` and `words`; from any other item citing a forgotten event it removes those sources, their footnotes and its `words`."

**Cards:**
- ST-05: `ItemsCiting` matches `resource` exactly, `s:` references included; its SPEC states the pass; a lock refuses with `locked`. Tests: no cleared byte stays; a second run is a `noop`. IF-01c's fake gains `ItemsCiting`.
- LG-30: asks with the event id and, for a v1 session, its `s:` reference; never touches item files; names the gap of notes and status lines that cite a forgotten event.
- ST-08: `undo` refuses a `forget` pass and never revives a `retracted` item. DL-08: the pack leaves out `retracted` items.

## 3. S-L3 and D-L2: a person's commands, and forget, in the record

**Rows:** S-L3 (store.md), now LG-10; D-L2 (derive.md), now LG-10, LG-13, LG-21.

**Questions:** Are remember, correct and confirm a person's commands in v2.0, and which package writes them, and forget, as typed record events a replay applies?

**Ruling:** v2.0 has no `remember`, `correct` or `confirm` command; they get a section 12 row. A person says it in a chat: a `prompt` event of LG-10's record whose author LG-13 (and LG-21 on Codex) sets to `typed`, or `spoken` within decision 29.
DV-21 checks it, DV-23 applies it, and ranges ST-08 queues after a restore are organized again from it. Forget is LG-10's `control` and `gone` events, which LG-30 writes in Stage 3.

**Why not the other:** folding the commands into "Tools that let an agent write to memory" contradicts that row's rule that such a write never carries your authority, and LG-10's v1 backend never yields author `typed`.

**Plan words:** a new row of section 12, directly after the row "Tools that let an agent write to memory", with these cells:
1. A person's commands that write memory: `remember`, `correct` and `confirm`
2. You answer question 11 of section 13.6 with yes, or real use shows a correction or a "remember that" typed in a chat that organizing missed
3. S to M
4. Settled in advance: each command first appends an event under your authority to a session the record keeps for each machine, and the item cites that event, so a restore replays it like any chat; run from an agent's shell it previews and asks (section 6.4, design 8). Until then you tell, correct or confirm memory by saying it in a chat

Outside the plan, in store-format.md section 6, the lead changes "`eva-md confirm` writes a JSON list of `{"by", "at"}`; no model's operation does." to "A person writes it by hand; the `confirm` command, which waits in plan section 12, would write a JSON list of `{"by", "at"}`; no model's operation does."

**Cards:**
- ST-08: no command and no replay without a remote. Gate: a typed correction in an uncovered range is queued after a restore. A lock, `verified` and an acknowledgement come back only from the remote; that limit goes into docs/known-limitations.md.
- DV-23: the user's words are a cited span of a `prompt` event authored `typed`, or `spoken` within decision 29; no command path, no forget transition.
- LG-30 gate: after a restore from a remote that lacks a retraction, every item whose sources are all `gone` is retracted again before the next pack compile. ST-05, or HO-24 and HO-26, names the command that issues `doc.acknowledge`.

## 4. S-H4: an upgrade below the store's reader

**Rows:** S-H4 (store.md), now ST-01, HO-27.

**Question:** The writer id is the store's (store-format.md section 3). Which package makes an upgrade refuse a binary below the store's `min_reader`: HO-13, HO-27 or CUT-3?

**Ruling:** ST-05 mints the writer id by ST-01's rule, the only machine id (C-H3). The upgrade half is HO-27's, which orders the move from v1, inside which ST-07's migration raises `min_reader`.
Once it has committed, HO-27 drops the kept previous binary and makes `eva-md upgrade` refuse a target whose declared reader generation is lower, by ST-04's `migrate.Check`. HO-13, ST-04 and CUT-3 keep their scope.

**Why not the other:** CUT-3 is a release the lead owns with no card, and a check against the bridge release's tag is undefined for later releases or blocks rollbacks between releases that both read the store.

**Plan words:** section 8, row HO-27, "Outcome and gate" cell, which becomes:
> The upgrade adopts a v1 install: its files into the manifest, `config.env` into preferences, old state cleaned, in an order a crash can resume.
> Once the migration has run, no upgrade puts back a binary below the store's `min_reader`: the kept previous binary is dropped, and `eva-md upgrade` refuses a target whose declared reader generation is lower (none declared reads as 1), with one line naming `eva-md migrate --abandon` and nothing changed

**Cards:**
- HO-27: an idempotent step after the migration commit removes `<share>/versions/previous`; `eva-md version` gains a second line with the reader generation from `migrate.Known` (line 1 unchanged; none reads as 1); every upgrade path checks before the target's setup runs.
  It hands docs/known-limitations.md the old pinned install script and the hand-copied binary.
- ST-05 calls `migrate.Check` first and exposes the bound store's root with no lock. ST-07 raises `min_reader` only through `Step.MinReader`. HO-13c keeps `--previous` with no slot a no-op. CUT-3's rehearsal tests the refusal and `migrate --abandon`.

## 5. V-L3: receipts and the read tools

**Rows:** V-L3 (deliver.md), now consumer none, `dropped`.

**Question:** Does a read tool's receipt token reach the record in LG-23's typed tool events, so DL-20's receipts cover pulls, or do receipts cover only what was pushed?

**Ruling:** only what hooks pushed, DL-11's `delivery` events (C-L2). No token in a read tool's result, nothing copied from a tool result, no pull log. A call of eva-md's own read tools is a `self` tool record, from which the worker sets the `memory` exposure.

**Why not the other:** `later` needs a section 12 row no number asks for: the first report read unprompted use of the read tools as not crossed.

**Plan words:** none. Optionally, in section 6.4 design 9, between "Every delivery is recorded as ids and hashes." and "`eva-md receipts` shows ...":
> A delivery is what a hook put into the chat. A call of the read tools keeps nothing of its result, as for any call that succeeded, and is marked in the record only as a lookup by eva-md.

**Cards:**
- DL-20 consumes C-L2 and says in one sentence that read-tool results are not listed. DL-12: no token, no pull log, no file written.
- LG-23: a tool event has no result field; eva-md's own read tools are marked `self`. DV-21: text after an eva-md lookup is echo. The lead strikes DL-12, DL-20 and V-L3 from host-facts.md F-036's "Needed by".

## 6. V-L1: where a chat's delivery state lives

**Rows:** V-L1 (deliver.md), now `v1`.

**Question:** Where does DL-09 keep a chat's state, the design's `deliver.json` slot, while the record keeps v1's layout and the session-keyed one is section 12's?

**Ruling:** one new session marker, `deliver`, in v1's per-chat folder, through the kept `session.Markers` API (`internal/session` unedited), owned by DL-09: ids, revisions and the owed flag, never text, saved atomically under the session lock before any output. A value written in shadow never counts as delivered; the marker moves with the folder if section 12's layout is built.

**Why not the other:** the other proposal left the name to DL-09's package and gave DL-09 the replacing of `briefed` and `prompts`, which is DL-11's wiring and CUT-4's deletion.

**Plan words:** section 6.4, design 5, after the sentence ending "once a compaction has removed it.":
> It is the hook's own state for that chat, kept as one session marker, `deliver`, beside v1's markers in the chat's session folder, and a hook reads it besides the pack and the record's session catalog; it holds item ids, revisions and the owed flag, never text, and it moves with that folder if section 12's session-keyed layout is built.

**Cards:**
- DL-09: one read and save pair in one file; the value records its version, epoch and mode; `held` caps at 256, `seen` at 512, under `session.MaxValue` by test; anything unreadable or of another mode is a new epoch owed one brief; the epoch resets on `startup`, `clear` and `compact`. It adds `deliver` to docs/design.md's marker list and docs/known-limitations.md.
- DL-11: calls the pair only; writes nothing in `v1` mode, and in shadow writes beside `briefed` and `prompts`. DL-10 reads only through the pair. DL-15 writes nothing in a parent's marker.

## 7. V-L4: the session catalog

**Rows:** V-L4 (deliver.md), now LG-21; a new interfaces row, LG-21's session catalog, `waits: LG-21`.

**Question:** Which package builds the record's session catalog that hooks and DL-10's handoff read (plan 6.4 design 1): LG-21, or DL-08 with the pack?

**Ruling:** LG-21, in a card of its own (LG-21 is split, not grown): `ledger.Catalog`, a pure builder over `ledger.Reader` and LG-21's checked points, a bounded read for hooks, and the rebuild, needing only LG-10 and IF-01b's `ledgertest`. A cache in eva-md's state outside the inbox.

**Why not the other:** DL-08's pack compiles only when the store changes, never at a checked turn, so a catalog in it would be stale and change the pack's generation every turn.

**Plan words:** section 8, row LG-21, at the end of the "Outcome and gate" cell, before "| L |":
> The worker also keeps each project folder's session catalog (section 6.4, design 1): one entry per recent session, holding what the handoff reads: its class, start and last work, turns, branches, the head of its opening request, files edited, open tasks, the last check and its result, how it ended, where it was cleared or compacted, and how many of its turns are still held.
> It is built from what the reader exposes, rewritten each time the worker checks a turn, and kept in eva-md's state outside the inbox. It is a cache: deleting it loses nothing. Gate: a catalog deleted and rebuilt equals the one kept, and no entry reaches past the checked point.

And row DL-10's "Depends on" cell, from "DL-08, DL-09" to "DL-08, DL-09; LG-21 for the session catalog".

**Cards:**
- The catalog card: V-L4's fields and `held`, keyed by `Key.Folder` until LG-22 joins folders; deterministic; a hook reads it with a size bound and never writes it. Gate: rebuilt equals kept; nothing withheld or off the record in an entry. Two reviewers.
- DL-10: builds the handoff from `ledger.Catalog` only. `inbox clear --apply`, and later LG-30, rebuild or delete the catalog in the same step; EV-20's privacy byte search includes it.

## 8. V-D1: command keys

**Rows:** V-D1 (deliver.md), now ST-01, DV-21, DL-05, ST-07.

**Question:** Which package gives items the command heads the pack's exact keys need? store-format.md has `cues` and `anchors` and no `commands`; who fills cues on migrated items?

**Ruling:** no `commands` field in v2.0. DL-08's compile reads command keys, as symbol keys, from an item's statement, `words` and cues, only from a code span, through DL-05's one tokenizer, which DL-06 also uses on prompts. DV-21 keeps a code-like cue only when the cited words hold it. Migrated items carry anchors and no cues until a later pass replaces them.
Command keys stay in shadow until DL-06's figures cover them.

**Why not the other:** cues alone would give DL-08 a private command reader the prompt side does not share, and the reading in DL-06 would make a cycle, since DL-06 depends on DL-08.

**Plan words:** four edits.
1. Section 6.4, design 1, after the sentence ending "and rows for the read tools.":
   > No item field holds commands in v2.0. Command keys, like symbol keys, are read from an item's own text (its statement, its `words` and its cues), and only from a code span there, by the one tokenizer, which reads a prompt the same way.
   > An item migrated from v1 has no cues, since only a model's operation sets them; its keys are its anchors and what its statement holds, until a later pass replaces it.
2. Section 8, row DL-05: after "Every match in the product uses the same tokens;" insert "the tokenizer also reads the head of a command in a code span, so the pack's command keys and a prompt's are read alike;".
3. Section 8, row DL-08: the "Depends on" cell from "ST-05" to "ST-05, DL-05".
4. Section 12, a new row after "Recall when the agent edits a path or runs a command", whose cells are "A field of command heads on items, written by the organizing pass";
   "The retrieval set, once it holds command events, or the continuation tasks show commands that matter and that the keys read from an item's text miss; or recall when the agent runs a command opens";
   "S: one optional key with no schema change (store-format.md section 9), one contract field, and a decision record (section 9)"; and an empty fourth cell.

**Cards:** DL-05 owns the reading of a command head (no shell parser; golden and fuzz tests). DL-08 depends on DL-05 and writes each key with its kind. DL-06 keeps no grammar of its own. DV-21 does not import DL-05. ST-07 writes no `cues`, by test.

## 9. V-H4: the pack after install and upgrade

**Rows:** V-H4 (deliver.md), now HO-21, HO-27, HO-12, HO-23.

**Question:** Which package makes install and upgrade end with a pack rebuild: HO-21 and HO-27, or the worker? Health text reaches a chat only as HO-12's prepared line.

**Ruling:** the hosts track. HO-21's first run, which install and every upgrade (`--previous` too) run, ends outside mode `v1` by starting DL-08's compile of every pack, detached. HO-27 compiles in the foreground after the migration and before the new plugins (plan section 10, point 3). DL-08 owns the job kind and `eva-md compile`.
Health reaches a chat only as HO-12's prepared line and HO-23's read-tool line (H-V2).

**Why not the other:** the merged worker cannot know an install or upgrade happened, and a pack mended in the hook would miss the first start after an upgrade that changes the pack's version.

**Plan words:** section 8, Stage 2.
1. Row HO-21, "Outcome and gate", after "and says what redaction does not remove (decision 32)":
   > . Outside mode `v1` it ends by starting, detached, DL-08's compile of every project's pack, so the first chat after an install, or after an upgrade, which runs the new binary's first run, finds a pack its hook accepts; the hook never waits for it, and a compile that fails or cannot start never fails the first run

   and its "Depends on" cell, from "HO-11, ST-05" to "HO-11, ST-05; DL-08's `eva-md compile` only to integrate".
2. Row DL-08, "Outcome and gate", after "chat-start cost no longer grows with the inbox":
   > ; `eva-md compile` is the one way to ask for a pack, for one project or all, run now or marked for the worker, and the first run (HO-21), the move of a v1 install (HO-27) and health's repair call it

**Cards:** HO-21 spawns the compile, never calls `work.Wake`, and never fails setup on it. HO-27's compile is a step a crash resumes. DL-08 registers the kind, adds the command through SEAM-0, and logs `pack_missing` or `pack_stale`. DL-10 waits for no compile. HO-23's repair calls the same command.

## 10. E-S1: the scorer's view of a v2 store

**Rows:** E-S1 (evaluate.md), now ST-01, ST-03, EV-20.

**Question:** Which package gives the scorer a typed export of a v2 store, the design's `eva-md export --json`? EV-14b reads v1's pages itself.

**Ruling:** no export command. The fidelity scorer reads a store folder through ST-03's codec, in ST-01's format, into EV-14b's `score.Export`, as it reads v1's pages. That reader is EV-20's first part, EV-20a, which depends on ST-01, ST-03a and EV-14b only and is tested on ST-01's fixture store at the start of Stage 2.

**Why not the other:** a Stage 1 EV-14e would give v1 no row and be built against a format with no writer and an open gap.

**Plan words:** section 8, row EV-20, "Outcome and gate", after "Each rebuild judged against v1's row on the same instrument.":
> The fidelity scorer reads a v2 store as it reads v1's pages: from the store folder, through ST-03's codec, into the same typed export, so the design's `eva-md export --json` is not built and nothing is added to the binary. This reader is EV-20's first part, tested on ST-01's fixture store before ST-05 lands, and EV-21 scores both sides of the migration with it.

**Cards:** EV-20a (size S) owns `internal/eval/v2store/**`, imports only the codec, `score`, `scenario` and the standard library, by test, and is in no release build.
EV-20 runs ST-01's profile checker on every store it scores. EV-21 scores v1 with `v1page` and v2 with `v2store`. For the lead, with ST-01 and DV-23: workstream next and open lines carry no source, so their facts score as missing.

## 11. LG-22's interface and fake

**Rows:** the interfaces row "LG-22, project identity", now provider LG-22, fake `waits: LG-22` until the card LG-22a exists.

**Question:** DL-07 and ST-07 build against LG-22's Stage 1 interface and fake, as Stage 2's first paragraph says, but no Stage 1 row writes one. Who does, and what is it?

**Ruling:** LG-22a, a small first part of LG-22, is built in Stage 1 with no caller in `internal/ledger/registry`: `ProjectID(key)`, store-format.md section 3's id as a pure function of a key (v1's resolver key; `general` for the general area); the registry's types and a listing of projects with their kind, as far as ST-07 and DL-07 need them;
and a fake, `registrytest`, one project per key, no joins. A project LG-22 later joins keeps its id and gains `merged_into`.

**Why not the other:** an interface of only `identity.ProjectID` and the format's record gives DL-07 no listing of folder projects, since the record holds no path or kind.

**Plan words:** section 8, Stage 2's first paragraph, after "means its Stage 1 interface and fake for building, and the package itself only for integration.":
> LG-22's is written by its own first part in Stage 1, with no caller, as DV-11 writes the runner's: the registry's types and signatures, the project id of the store format's section 3 as a function of a key (for a project v1 knew, its key as v1's resolver writes it, and `general` for the general area), and a fake that holds one project per key and joins none.
> ST-07 and DL-07 build against it; LG-22's joins and splits reach them only at integration, and a migrated project that LG-22 later joins keeps its id and gains `merged_into`.

**Cards:**
- LG-22a (size S; depends on LG-10 and ST-01): test vectors for a v1 repository key, a `local-` key and `general`; nothing in a release build. Joins, folder projects, the resolver changes and `eva-md project` stay in the rest of LG-22.
- LG-22a checks ST-01's fixture ids against the rule: `local-scratch-9f2c1a` gives its id, `p_6pbe1q7fxb5h`, but `p_00gb3367a19r` hashes the remote `github.com/acme/orders`, and its v1 key `github.com__acme__orders` gives `p_c0476xchw35b`. The fixture's README then says its ids are made up, or they are made again.
- ST-07 records a later join as `merged_into`, never a new id, and says where the v1 key it hashes comes from. DL-07 takes a project id as given.

## 12. The three dropped rows

**Rows:** S-E2 (store.md) and D-E4 (derive.md), now `later`; V-D10 (deliver.md), still `dropped`.

**Question:** Are S-E2, D-E4 and V-D10 rightly `dropped`, or are they `later`?

**Ruling:** S-E2 and D-E4 are `later`: plan 6.6 holds the further suites until a decision waits on their number, a deferral with a trigger, so section 12 gets one row for them. S-E2's Where cell also says that poisoning through foreign commits waits for sync. V-D10 stays `dropped`: the clock and hook wakes are the only wakes.

**Why not the other:** without a section 12 row the suites would have no route into Stage 3, which takes only section 12's items whose trigger has fired.

**Plan words:** a new row of section 12, directly after the row "Asking the host to confirm a tool call that breaks a rule you armed", with these cells:
1. The further suites of section 6.6: task switching (with a default-branch variant that scores workstream binding), rule compliance, drifted code, poisoning, aging over 200 sessions and a corpus audit, each built on its own
2. A decision in this plan, or the trigger of another item in this section, waits on that suite's number
3. S to M each
4. Leave for its live runs inside decision 16's ceiling

And in section 6.6, at the end of the sentence that begins "Further suites (task switching, ...": "Section 12 holds them with that trigger."

**Cards:** none; all three rows have no consumer.
