# EV-14e: the scorer tells an agreement from a statement, and asks a slot of some kinds only

**Plan:** section 6.6 (fidelity), section 8 Stage 1 row EV-14, section 14.1 proposal G; decision 10 (an accepted proposal is a decision), taken at its recommendation. **Follows:** [EV-14b](EV-14b.md) and [EV-14c](EV-14c.md), "The lead's rulings of 2026-11-13" and "As merged". **Design:** `docs/v2/working-papers/design/evaluate.md` section 6.1. **Size:** S. **Reviewers:** two: the contract, and one who looks for a store that now scores better or worse than it should. **Depends on:** EV-14b and EV-14c.

## Why

EV-14c's families ask three things today's scorer cannot judge, so two of its tests hold a wrong score on purpose and one records a wrong rule. No figure of those families may go in a gate until this lands. Evaluation harness only: no release builds it, nothing v1 does changes, so no switch.

## What to build

1. **An acceptance is found under `stated`.** An acceptance is a fact of `AuthorityUser` whose `Supersedes` names a fact of `AuthorityOther`: the rule `Ideal` uses in `internal/eval/score/seed.go` to write `accepted`. Move it into one unexported function that `Ideal` and the authority check in `verdict` (`score.go`, near line 451) both call. An item of an acceptance under `stated` is `found` with `ProblemAuthority`. `accepted` stays right for every user fact: v1.10's ideal page tags TERN-7, which is not an acceptance, `accepted`, and must still score perfectly.
2. **A slot may be left unasked of some kinds.** `scenario.Slot` gains `Unasked []Kind` (`json:"unasked,omitempty"`): permitted kinds of the fact whose item need not hold that slot. `match` takes the item's kind; there an unasked slot is neither held nor lacking. `tied`, `covers`, `hasSlot` and `wrong`'s cover keep reading every slot. An item in a kind the fact does not permit is asked every slot. `Validate` refuses a kind not in the fact's `Kinds`, a repeated kind, a permitted kind that asks no slot, and a slot no permitted kind asks.
3. **The correction's echo is history.** In `tied`, an ended fact f is also not tied by part when the item holds in full a fact listed in `enders[h]` for some fact h, and every slot of f the item holds is a slot of h (`hasSlot`): it repeats what the correction replaced. By part only, as the first exemption: an item that holds the echo in full stays `stale_in_force`. Say it in the package doc and `tied`'s doc, beside the first exemption, with its cost: value matching cannot read a negation.
4. **The families take the first wording back** (this card owns these lines): in `limit`, the hypothesis's hedge slot has `Unasked: [open]`, so an open question needs no hedge and a fact does; the doc comments of `limit` and `accepted`, and of `TestAcceptedIsADecisionNeverARule`, drop the rulings' wording. `TestAcceptedUnderStatedHoldsUntilTheScorerTellsThemApart` and `TestHypothesisKeepsItsHedgeEvenAsAnOpenQuestion` fail by design; replace them with tests named for the new rule. The echo case of `TestCorrectionKeepsTheNewValue` becomes a clean case.

## Rows and baselines

No fidelity row or baseline is committed (`eval/fidelity/` holds only `metrics.json`). Every fidelity metric's paths name `internal/eval/scenario` and `internal/eval/score`, so a baseline accepted before this merge is stale and `eva-eval gate` fails it. The lead accepts a new one at the merge commit: `eva-eval fidelity --pages <dir> --out <dir>`, then `eva-eval baseline accept --repo . --catalogue eval/fidelity/metrics.json --out <file> <rows>`. v1's row: the recorded v1.09 page tags the acceptance `[accepted]`, so it should not move; a page that tags it `[stated]` now leaves it `found [authority]`. The builder's report lists every scenario whose score moves, before and after and why: v1's 12 ideal pages and their distortions, the five pages in `cmd/eva-eval/testdata/fidelity/`, and each family on 20 seeds.

## Owns and may touch

The scorer and its tests, the slot field and its validation, and the lines of `internal/eval/families` named above.

```paths
internal/eval/score/score.go
internal/eval/score/seed.go
internal/eval/score/score_test.go
internal/eval/score/seed_test.go
internal/eval/scenario/scenario.go
internal/eval/scenario/scenario_test.go
internal/eval/families/heldout.go
internal/eval/families/score_test.go
```

## Must not touch

Every other file: `internal/eval/scenario/v1.go` and `testdata/`, `internal/eval/score/v1_test.go`, `internal/eval/families/tuning.go`, `packs/` and `testdata/`, `internal/eval/v1page`, `internal/eval/spike`, `cmd/eva-eval/**`, `eval/**`, `internal/update/**`, `internal/threadpage/**`. If a test there fails, stop and report. Adds no line to `./cmd/eva-md`.

## Accepted when

- `scripts/linux-check.sh` passes; `go list -deps ./cmd/eva-md | grep -c internal/eval` prints 0; `score/v1_test.go`, the families' pack and golden tests (`packs/tuning.json` unchanged) and `cmd/eva-eval`'s tests pass unchanged.
- On the scorer's cases (fact `w2`) and the port's v1.09: an acceptance under `stated` is `found [authority]`, fact_holds falls by its share, fact_recall and misattributed do not move; under `accepted` it holds; `k5` and v1.10's f3 hold under either tag. On 20 seeds of `accepted`, the acceptance under `stated` is `found [authority]`.
- On a small scenario of its own (so the cases' indices do not move) and on `limit`'s hypotheses: lacking an unasked slot, an item holds; lacking it in another permitted kind, or in a kind not permitted, it is `limit_dropped`.
- `Validate` refuses each of item 2's four faults, a test each.
- On every `correction` instance, the item that keeps the correction, names the old value and cites the correction and the echo, with or without the old line, holds every fact. Still `stale_in_force`: the old value alone citing the echo; the echo in full beside the correction; and, on a scenario with two corrections, an echo of one beside the other correction held in full.
- Each mutation is caught by a test: the old `userTag` check restored; `Unasked` ignored, or applied to a kind not permitted; the third exemption without its `hasSlot` condition, or applied to an item that holds the ended fact in full.
- No comment in `internal/eval/families` still says the scorer cannot tell `accepted` from `stated` or ask a slot of one kind alone.

## The lead's rulings of 2026-11-13

The card's checker left these to the lead.

- The authority rule is one-way, as carded: only an acceptance filed as stated is found with the wrong authority. A store that tags every statement accepted is not penalized; that waits for a card on the scenarios, which first settles whether v1.10's third fact is an acceptance.
- A slot is asked of every kind unless the fact says otherwise (opt-out), as carded.
- An echo held in full beside the correction stays `stale_in_force`.
- At the merge the lead amends EV-14b's notes, which say there is no third exemption, and EV-14c's row in the index.
