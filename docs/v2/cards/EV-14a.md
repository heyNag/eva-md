# EV-14a: the scenario model, with v1's 12 scripts ported

**Plan:** section 6.6 (the fidelity instrument), section 8 Stage 1 row EV-14 ("EV-13 shrunk into it"), section 13.3 item 9, section 14.1 proposal G. **Design:** `docs/v2/working-papers/design/evaluate.md` sections 4.1, 5.1 and 6.1, and its rows EV-13 and EV-14 in section 21; `audit/eval.md` F3 and F9. **Size:** M. **Reviewers:** one. **Depends on:** LG-10 and EV-12 (both merged). **Part of:** EV-14, built as a, then b, then c and d at once.

## Why

v1's accuracy suite finds a fact by its codename in one named section of a page, so it cannot see a wrong value, a dropped date or an invented item. The fidelity scorer (EV-14b) judges a store by value and by the events each item cites. It needs scenarios that say, for every planted fact, which values must survive, where it may be filed, under whose authority, and at which event of which session it was said. This part is that model, with v1's 12 scripts as its first instances. Nothing in eva-md changes, so there is no switch.

## What to build

A new package, `internal/eval/scenario`, that no release contains. Its non-test files import nothing from `internal/update` or `internal/threadpage`.

1. **The model.** Types `Scenario` (an `ID` that is a results id, a `Family`, a `Split` of `tuning` or `heldout`, a `Seed`, its sessions, its facts, and the order in which v1's passes take its chats), `Session` (host, session id, branch, turns) and `Turn` (time, `Author` and `Kind` from `internal/ledger/event`, a subagent id, text, and the ids of the facts it plants; off and on the record, and a host prompt, are turns too). `Fact` holds:
   - `Kinds`: the set of places a correct store may file it: `fact`, `decision`, `constraint`, `procedure`, `rule` (the store's five kinds), `next`, `open` (a workstream's working items) and `log` (a dated line). Any one of them counts.
   - `Authority`: `user` (it must carry the user's authority, `stated` or `accepted`) or `other` (it must not).
   - `Slots`: what must survive, each a `Slot` of a class (`int`, `quantity` for a number with its unit, `date`, `path`, `ident`, `version`, `phrase`, `limit`, `cond`) with the value as said and any accepted forms; and `LogDate` for a fact filed as a dated line.
   - `Until` (the turn that ends it, if any), `Supersedes` (the fact it replaces) and `Must`: `store`, `not_store` (said off the record), `not_with_authority`, or `not_as_rule` (it may be kept, never as a rule or a constraint).
2. **v1's 12 scripts ported**, `scenario.V1()`, in the order of `accuracy.Scripts()`, with ids `v1.01` to `v1.12` and family `v1`, all `tuning`. Each keeps its turns, times, codenames and passes. Every fact whose words hold a value (a number and its unit, a date, a name, a path, an identifier) gets it as a slot, its codename as an `ident` slot, and a conditional fact its condition. Where the port judges a fact differently from v1's `Measure`, a comment says so: a pasted decision is `not_with_authority`, and an off-the-record fact `not_store`.
3. **Two renderings.** `V1Entries(session, host)` gives the inbox entries v1's hooks would store, filler exchange included. `Events(session)` gives `event.Event` values with seq from 1, authors as the turns say, and a sameness key on each. A turn whose author is `spoken` or `front_model` has no rendering yet: both return an error that says it waits on HF-00's recorded wrapper. A `dialog` or `plan_decision` turn renders as an event only; `V1Entries` returns an error for it, since v1 keeps no such entry.
4. **Locate.** `Locate(session string, seq int64)` takes a full session id or its 8-character prefix (`event.Prefix`) and the seq, as a v1 reference `s:<sid8>#<line>` or a v2 source gives them, and returns the turn and its planted facts, or reports a line with no turn (the filler).
5. **Forbid.** `Forbid(Scenario)` lists every planted word for `results.Store.Forbid`. Every value in it holds a letter from g to z, so it is never mistaken for a hex string (EV-12's ruling); a value without one (a bare number or a date) is left out, and every fact still has at least one word in the list.

## Owns and may touch

`internal/eval/scenario/**` (new), with its test data.

## Must not touch

Everything else. `internal/update/accuracy`, `internal/update/accuracy/live`, `cmd/live-accuracy`, `internal/ledger/**` and `internal/eval/results` are read, never edited. A test of this package may import `internal/update/accuracy`; a non-test file may not, and `TestOnlyItImportsTheSuite` in `cmd/live-accuracy/main_test.go` must still pass unchanged.

## Accepted when

- `scripts/linux-check.sh` passes, and `go list -deps ./cmd/eva-md | grep -c internal/eval` prints 0.
- For each of the 12 scripts, each chat and both hosts, the JSONL of `V1Entries` is byte-identical to `accuracy.Script.Chat(i, host).JSONL()`, by test.
- Written to a temporary inbox and read back through `inboxv1.Backend`, each rendered turn's text is at the seq `Locate` gives for it, by test; `Locate` on a filler line finds no turn.
- Every fact of the port has at least one slot, a `Kinds` set and a `Must`; golden files hold the 12 ported scenarios as JSON and round-trip.
- `Forbid` holds every codename of the 12 scripts, and every word in it holds a letter from g to z, by test. A doctored scenario with a fact that has no forbidden word fails that test.
- A turn by a spoken or front-model author makes both renderings return their error, and a dialog turn makes `V1Entries` return its error while `Events` renders it, by test.
- A test runs every rendered text through `redact.Redact`. Any text it changes is listed in the test by scenario and turn, with why; the list is expected to be empty, and a builder who finds otherwise reports it rather than changing a script.
- The package imports no new module, and only eva's name appears in its text.
