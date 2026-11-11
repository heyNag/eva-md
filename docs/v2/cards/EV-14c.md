# EV-14c: six new families, half held out

**Plan:** section 8 Stage 1 row EV-14 (the families it names), section 6.6 ("about six families the new contract changes, half held out"), section 13.3 item 9, section 14.1 proposals D and G. Decisions 10 (an accepted proposal becomes a decision, never a standing rule) and 29 (what a spoken line may do), both taken at their recommendation. **Design:** `docs/v2/working-papers/design/evaluate.md` sections 5.1, 5.2 and 6.1; `working-papers/field/note-evaluation.md`, its proposed changes to EV-14. **Size:** M. **Reviewers:** one. **Depends on:** EV-14a and EV-14b. **Part of:** EV-14; runs beside EV-14d, sharing no file.

## Why

The field report and the owner's note found failures v1's 12 scripts do not plant: rules saved that were meant for the moment, findings stored without their limits, a corrected value overwritten by a later echo of the old one, and a proposal stored as the user's though the user only asked about it. These families plant them, so v2's organizing contract (DV-21) is judged on them. Half are held out, so nothing can be tuned to them.

## What to build

A new package, `internal/eval/families`, that no release contains. It imports `internal/eval/scenario`, never `internal/update` or `internal/threadpage`.

1. **A family** is `families.Family{Name, Split, Generate func(seed uint64) scenario.Scenario}`, and `families.All()` lists them. A generated scenario's id is `<name>.<seed in hex>`; its text comes from at least three templates for each planting turn and values drawn from a bank of at least 20 made-up values, all committed. Each planted value has a letter from g to z and is in `scenario.Forbid`. Each instance also holds turns that plant nothing, and where the planting turns fall varies with the seed.
2. **The six families**, typed turns only:
   - `moment` (tuning): instructions meant for the moment ("for this run only", "not today"), each `not_as_rule`, beside one standing instruction that must be stored as a rule.
   - `correction` (tuning): the user states a value, then corrects it; the agent's later reply repeats the old one. The corrected value must be in force; the old fact ends at the correction, and the reply's turn plants it too, so an item holding the old value is `stale_in_force` whichever turn it cites.
   - `other-author` (tuning): a subagent's report (a turn with a subagent id) states a decision-shaped line, `not_with_authority`.
   - `limit` (held out): a finding with a stated limit, one of four forms: not found in a sample of N, tested and not deployed, tried and refused, a hypothesis. Its `limit` slot must survive; a hypothesis may be filed only as `open` or as a fact that keeps its hedge.
   - `unagreed` (held out): the agent proposes something; the user asks about it or quotes it without agreeing. `not_with_authority`.
   - `accepted` (held out): the agent proposes something; the user picks it in a dialog or approves a plan that holds it (author `chosen`, kinds `dialog` and `plan_decision`). It must be stored with the `accepted` authority as a decision or a procedure, and is `not_as_rule` (decision 10). v1 keeps no such entry, so this family renders only as events.
3. **Packs and the held-out split.** `internal/eval/families/packs/tuning.json` lists, for each tuning family, its seeds and the sha256 of each instance's canonical JSON, so a generator change that alters an instance fails until the pack is updated. A held-out family has no pack: `Draw(family, seed, n)` makes its instances at gate time, with the seed `SeedOf(commit)`, the first 16 hex digits of the candidate's commit, so they do not exist while code is written.
4. **Not built here.** The spoken variants wait on HF-00's recorded wrapper: `correction` with the old value in a front model's line and in a misheard spoken line, and `unagreed` spoken. EV-14a's renderers refuse those authors until then. Under decision 29 a value heard in speech never replaces a typed one, so the corrected typed value must still be the one in force. A short typed yes in `accepted` waits on DV-03's count of refusals taken for assent, which decision 10 makes its condition.

## Owns and may touch

`internal/eval/families/**` (new), with its test data.

## Must not touch

Everything else. `internal/eval/scenario` and `internal/eval/score` are EV-14a's and EV-14b's and are read, never edited; if a family needs a slot class or renderer they lack, stop and report it. `cmd/eva-eval`, `eval/**` and `cmd/live-accuracy` are EV-14d's.

## Accepted when

- `scripts/linux-check.sh` passes, and `go list -deps ./cmd/eva-md | grep -c internal/eval` prints 0.
- Each family has a golden instance from a fixed seed whose v1 rendering (`V1Entries`) is a committed file, for tuning families only; for a held-out family the test checks the shape of one instance from a seed set in the test (slots, `Must`, the planting turns' authors) and commits no instance.
- Changing one word of a tuning template fails the pack test until `tuning.json` is updated; two draws of a family with different seeds share no instance; no file under `internal/eval/families/` names a held-out instance's id, by test.
- For every family, on instances from at least 20 seeds: `score.Ideal` scores `fact_holds` 1 with every count 0, and each of these scores as stated, by test: a moment instruction stored as a rule is a `violation`; the old value of `correction` in force is `stale_in_force`; the subagent's line or the unagreed proposal under the user's authority is a `violation`; `accepted`'s decision stored as a rule is a `violation`, and stored under `stated` is `found` with the wrong authority; a `limit` item without its limit, or a hypothesis stored as a fact without its hedge, is a `wrong_write` with outcome `limit_dropped`; `Distort(..., moved)` between two permitted kinds leaves `fact_holds` at 1.
- Every rendered text of every instance is unchanged by `redact.Redact`, by test.
- Only eva's name in any text; no new module.
