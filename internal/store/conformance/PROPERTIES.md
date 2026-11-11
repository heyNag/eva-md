# The store's properties

The store core must keep five properties before it is written: crash safety, exactly once, protection, restore and replay. The store core (ST-05) is held to them, and the store's fake (IF-01) is built on the model. Each is a check, `CheckX(newSubject, seed)`, that drives a subject through the schedule a seed gives and returns a `Failure` naming the property, the seed and the step; `RunX(t, newSubject, n)` checks seeds 1 to n. The reference model in `model.go` keeps all five on 1,000 seeds (40 under the race detector). A property that cannot fail proves nothing, so each is shown failing on a broken variant of the model, on a fixed seed (`TestEachVariantFailsItsProperty`).

Sources are in brackets: **P** is [the plan](../../../docs/v2/plan.md) by section, design and row; **S** [the store design](../../../docs/v2/working-papers/design/store.md) by section; **F** [the store's written format](../../../docs/spec/store-format.md) by section. One machine and one writer only (P7, decision 21): sync between machines, and the properties that need it, come later.

## Subjects and schedules

A subject is a store under test (`Subject`): `Open(clock, fault)` returns a `store.Store`; `Files`, `History` and `Items` show its tree, its commits and its items; `HandEdit` changes a file as a person would, with a change, a lock written `locked: true  # note`, a verified mark or a garble that makes it unreadable. A `Restorable` subject can also `Push`, `Lose` its store folder and nothing else, and `Restore` it.

A seed gives a schedule: a made-up record of two or three sessions, cut into chunks, and around it hand edits, acknowledgements, pushes and lost folders. Each chunk is one pass of a fake derive, which covers the chunk's range of the record and finds the items it revises or retires by title through `Items`. Half the passes crash at a step of `Apply`, and every step is reached across the schedules; a crashed pass is reopened and run again, as the worker would. The clock moves only between passes. No check sleeps, reads the wall clock, runs git or writes to disk.

## The properties

| Property | Source | Check | Killed by |
| --- | --- | --- | --- |
| CrashSafety | P6.2 design 3; S7.1, S7.3; S10, finding update-write F9 | `CheckCrashSafety` | `filesBeforeJournal`, `skipPassLookup` |
| ExactlyOnce | S7.1 steps 1 to 3; F3, F8 (`Eva-Pass`); S10, model-1 | `CheckExactlyOnce` | `skipPassLookup`, `filesBeforeJournal`, `refusalDropsPass` |
| Protection | P6.2 design 4; P8 row ST-05; F6, F7; S5.7 | `CheckProtection` | `bareLock`, `refusalDropsPass` |
| Restore | P6.2 bars; P8 row ST-08; S9.3, finding restore-1 | `CheckRestore` | `coverageOutside`, `queueFromJobs` |
| Replay | P6.2 design 3; P8 row ST-08; S7.5, S9.3, finding replay-1 | `CheckReplay` | `queueFromJobs`, `coverageOutside` |

`TestThePropertiesTable` fails if a property has no variant here, or if a variant listed is not shown failing it.

### CrashSafety

A crash at any step of a pass, followed by a reopen, leaves the store as it was before the pass or as it is after the pass ran uncrashed, never between: files and history both. A pass `Apply` returned is never lost.

The check drives two subjects through one schedule. One runs each pass uncrashed; the other crashes it at the schedule's step (or at a step the seed picks), reopens, and must equal the first before or after the pass. It then runs the pass again, which must give the uncrashed outcomes and state, and reopens once more. A pass that was not crashed must still be there after the reopen.

Variants: `filesBeforeJournal` writes the files before the journal, so a crash at the journal leaves changes no journal names, and the next open adopts them as a hand edit. `skipPassLookup` never looks a pass up among the commits, so a pass run again after a crash at `finish` is planned anew and refused as a conflict with itself.

### ExactlyOnce

A pass id is in history at most once, and once if it applied an operation or covered a range. A pass run again, before or after a reopen, returns the first outcomes, ids and commit, and changes nothing. `Plan` returns `Apply`'s outcomes and writes nothing. One seed gives one tree.

The check plans each pass, applies it, then applies it twice more with a reopen between, and counts the pass in history. At the end it plays the same schedule plainly, with no plan and no second run, on a new subject, and the two trees and histories must be equal.

Variants: `skipPassLookup` refuses the second run as a conflict. `filesBeforeJournal` adopts a crashed pass's files as a hand edit, so the pass run again is refused where `Plan` said it would apply. `refusalDropsPass` drops a pass that covered a range.

### Protection

A file a person locked or verified, one that cannot be read, or one a hand edit changed and nobody acknowledged, refuses the operation aimed at it: a lock and an unreadable file refuse every operation; a verified mark and an unacknowledged hand edit refuse each operation without the user's authority. The refusal carries its reason (`locked`, `unreadable`, `verified`, `hand_edit`, in that order), the file's bytes do not change, and the pass goes on: every other operation gets the outcome it gets without that one, unless it requires it. No operation aimed at a file nothing protects is refused for protection. A locked item is byte-identical after 1,000 passes aimed at it.

The check marks each file it edits by hand and each acknowledgement that applied, so it knows each refusal before the pass. For each operation it expects refused, it plans the pass without that operation first, its requirements renumbered, and compares every other outcome but those that require it. At the end it locks an item by hand and aims 1,000 passes at it: revisions of its statement and title, both states, an acknowledgement, under both authorities.

Variants: `bareLock` reads a lock only as the line `locked: true`, as v1 did, so `locked: true  # note` does not lock. `refusalDropsPass` refuses the whole pass when one operation is refused.

### Restore

After the store folder is lost and restored, the store opens with its store id, and its files, history and coverage are those of the last push, or those of a new store when nothing was pushed. Only the store folder is lost: the store id and the state kept outside it remain.

The check keeps the files, history and coverage of each session at every push, loses and restores the folder where the schedule says and once at the end, and compares.

Variants: `coverageOutside` keeps coverage in the store folder outside the tree, so it is never pushed and a restore loses it. `queueFromJobs` adds the local job list to coverage, so a restored store claims ranges its tree does not hold.

### Replay

Coverage never shrinks but by a loss. After a restore the queue, the record less `Coverage`, is exactly what the pushed head leaves uncovered; once it is replayed, coverage is at least what it was before the loss, and the items match those before it by kind, state, title and statement. Ids may differ, as the passes that replay are new passes.

The check has no hand edits, since a rebuild from the record cannot bring back what is not in it (S9.3: a person's explicit writes must be events of the record). It checks coverage after each step, loses the folder at the end, compares the queue with the chunks run after the last push, line by line, replays the queue in the record's order, and compares coverage and items.

Variants: `queueFromJobs` leaves out of the queue the ranges the local job list ran after the last push, so their items never come back. `coverageOutside` restores no coverage, so the queue holds ranges the restored tree already covers.

## What the model chooses where the format is silent

- An operation's `Requires` is met by an operation that applied or was a no-op.
- A range a pass covers again exactly is not covered anew, and makes no commit by itself; a range that overlaps a covered one but is not it refuses the whole pass.
- An acknowledgement is an operation, `doc.acknowledge`, aimed at the item, under the user's authority. A lock refuses it too, as it refuses every operation.
- A hand edit is adopted at every recovery: at open and at the start of each `Apply`. `Plan` recovers a copy of the folder, so it writes nothing.
- A session file, which the format does not let a person lock (F6), still refuses a note when it holds a lock or cannot be read, and then takes no coverage: the store writes nothing into it, and its ranges stay in the queue until a person repairs it. A verified mark or an unacknowledged hand edit on a session file refuses nothing, as no operation can acknowledge one.
- The model refuses as `invalid` any operation type but `item.create`, `item.revise`, `item.state`, `session.append` and `doc.acknowledge`, and scans no text for secrets.
