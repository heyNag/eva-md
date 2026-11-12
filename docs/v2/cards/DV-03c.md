# DV-03c: the contract spike's arm: passes, recording, replay, fidelity and rows

**Plan:** section 8, Stage 0, row DV-03; section 6.6 (one black-box scorer reads the store's output); the lead's choice in section 7 that no model judges a gated number; section 6.3's bars. **Design:** `docs/v2/working-papers/design/derive.md` section 1 (`fact_recall`, `wrong_write`, `misattributed`, `honest_refused`), 5.8 and row DV-03; `docs/v2/host-facts.md` F-065. **Size:** M. **Reviewers:** one. **Depends on:** [DV-03b](DV-03b.md) and [EV-14b](EV-14b.md). **Part of:** DV-03 (a, b, c, [d](DV-03d.md)).

## Why

An arm is one host, one model tier, one effort and one form of the contract, run over every script. The frontier compares arms, so each must be run the same way, judged by the same scorer as v1 and v2 (EV-14b's), and replayable with no model call, so that a live run's recording can be scored again when the scorer changes. This part runs an arm against any answering function and turns it into rows. It changes nothing v1 does: no switch.

## What to build

A new package `internal/eval/spike/arm`. It imports from eva-md only `internal/eval/spike`, `internal/eval/scenario`, `internal/eval/score` and `internal/eval/results`, never `internal/update`, so it runs on canned answers alone.

1. **The arm.** `Arm{Host, HostVersion, Tier, Effort string; Form spike.Form; Repeat int}`, `Tier` one of `small`, `middle`, `large`. A model's name is never a field: the tier is all an arm says of its model.
2. **One answering function.** `Ask func(ctx context.Context, c Call) (Reply, error)`; `Call{Script string; Pass, Round int; Kind string; System string; Input, Schema []byte}`, `Kind` `first` or `repair`; `Reply{Output []byte; Reason string; TokensIn, TokensOut, Attempts int; CachedIn *int; Seconds float64; Effort string}`, as the host runner reports them, `CachedIn` nil when it reports no cached input. A reply with a reason is a failed call, scored as no answer, and the run goes on.
3. **`Run(ctx, arm, scripts []spike.Script, ask Ask, rec io.Writer) (Result, error)`.** For each round (`Repeat`, at least 1), each script and each of its passes: render the pass's session with what the script's store shows, ask with DV-03a's prompt and schema, check, and when anything is repairable ask once more with DV-03b's repair input and repair schema. Two stores per script: `on` applies the final verdicts, `off` only what the first check accepted. Later passes are rendered from `on`, so `off` measures what repair changed in what was kept, not a run without repair; `SPEC.md` says so.
4. **The recording.** Each call is one JSON line on `rec`, written as the run goes: `{"v":1,"host","host_version","tier","effort","form","round","script","pass","call","output","reason","tokens_in","tokens_out","tokens_cached_in","attempts","seconds","effort_used"}`, `output` the answer's JSON or null, `tokens_cached_in` null when not reported. Nothing else: no model name, no path, no prompt, no input.
5. **Replay.** `Replay(r io.Reader, arm Arm) (Ask, string, error)`, the string the recording's host version, which every line must share. It refuses, before any call, a recording of another host, tier, effort or form, a line given twice, or a reason that is not a short id. Its `Ask` answers each call from its line and fails on a call it lacks; `Run` then fails, and so does a replay that leaves a line unused.
6. **Fidelity, by EV-14b's scorer.** Each store's items become a `score.Export`: an item of kind k is a k, a next step `next`, a question `open`, a note a `log` dated by its day; a retired one keeps its kind with state `retired`; tag as authority; sources from its cited entries. They are scored with `score.Score`. `honest_refused`: the ops finally refused that cite a shown typed or chosen entry are each exported alone as a `stated` item and scored; it is those the scorer finds `supports` a fact, over those plus the accepted `stated` or `accepted` ops whose item does.
7. **Rows.** `Rows(res Result, template results.Row) []results.Row`: instrument `spike`; model the tier word; host and host version from the arm; scenario `<form>.<effort>.<on|off>.<script id>` and `<form>.<effort>.<on|off>.all`; metrics `spike.<id>` for every count (unit `count`) and ratio (`ratio`) of DV-03b's `Tally`, plus `spike.calls_first`, `spike.calls_repair`, `spike.calls_failed` (`count`), `spike.tokens_in`, `spike.tokens_out`, `spike.repair_tokens_in` (`tokens`), `spike.repair_tokens_cached_in` (`tokens`, only when every repair call reported it: F-065), `spike.seconds` (`seconds`), `spike.honest_refused` (`ratio`), and EV-14b's `fidelity.*` rows for each store.
8. **`SPEC.md`** of its own: the arm, the recording's format and the rows, and that it is deleted when DV-21 lands.

## Owns and may touch

The new package, its spec and its test data.

```paths
internal/eval/spike/arm/**
```

## Must not touch

Everything else. `internal/eval/spike`, `internal/eval/scenario`, `internal/eval/score` and `internal/eval/results` are read, never edited.

## Accepted when

- `scripts/linux-check.sh` passes, and `go list -deps ./cmd/eva-md | grep -c internal/eval` prints 0.
- A test parses every non-test file's imports and finds no eva-md package beyond the four named above.
- An arm over three v1 scripts and two `assent` scripts on faithful canned answers (DV-03b's, or more in this package's test data) gives `fidelity.fact_holds` 1, every refusal count 0 and no repair call, in both forms; an answer with one quote off by a word gives one repair call, and a repair that fixes it gives `on` and `off` stores that differ by exactly that item: by test.
- A run recorded and then replayed, with an `Ask` that fails the test if called, gives the same rows apart from time; each refusal of item 5 calls nothing: by test.
- A failed call (a runner's reason) is counted and the run goes on; a reply without `CachedIn` writes no `spike.repair_tokens_cached_in` row: a test each.
- Rows go through `results.Store` with `Forbid` holding every script's `scenario.Forbid`, and pass `results.Load`; the recording holds no prompt and no input, only answers: by test.
- Two runs on the same answers give identical rows apart from time.
