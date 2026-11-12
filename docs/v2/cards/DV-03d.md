# DV-03d: the contract spike's command, the plan of the run, and the frontier

**Plan:** section 8, Stage 0, row DV-03 and its column "Needs from you", and the threshold "Typed refusals cited as assent"; section 6.3, design 8; decisions 5, 10, 16 and 28, at their recommendation; sections 15.2 and 15.4. **Design:** `docs/v2/working-papers/design/derive.md` 5.4, 5.8, 8.3 and row DV-03; `docs/v2/host-facts.md` F-064, F-065. **Size:** M. **Reviewers:** one. **Depends on:** [DV-03c](DV-03c.md), and DL-04 merged (see Why). **Part of:** DV-03 (a, b, c, d).

## Why

The row's live run is the owner's to allow, inside a price the lead puts before the owner. This part gives the lead a plan of the run, with calls and tokens per arm; the run itself, which calls no model without the flag `cmd/live-accuracy` asks for; and a frontier from which the lead writes the table in `docs/v2/` and the decision record naming the default row, the evidence form and the repair policy. It is an instrument of `eva-eval`, in a file of its own: an arm runs the host CLI through `update.NewRunner`, which `cmd/eva-eval` may import since DL-04 let its `TestOnlyEvasNameShows` pass over the packages eva-md's own binary imports. It changes nothing v1 does: no switch.

## What to build

`cmd/eva-eval/spike.go`, which adds `eva-eval spike` with `register` from its `init`; `main.go` is not edited, and no release or install builds it. Hosts come from `internal/host/adapter`, never as literals (`TestHostLiteralCount`). The file's comment says it is deleted with `internal/eval/spike` when DV-21 lands.

1. **`eva-eval spike plan [--repeat n] [--efforts low,medium] [--output-allowance n] [--overhead host=n,...] --out file`.** A Markdown plan for every arm: both hosts, the tiers `small`, `middle`, `large`, each effort, both forms. Per arm: first calls (passes of `spike.Scripts()` times the repeat), repair calls at most as many, and tokens: a first call's input is the bytes of prompt, rendered input (nothing shown) and schema, divided by 4 and rounded up, plus the host's overhead per call; a repair call's adds the output allowance (default 2,000), which is also each call's output. Totals per host and in all, beside the row's allowance (about 1,900 calls, 25 million tokens), saying which side each is on. The head states every rule and default, that the overhead is unknown until DV-02's recordings, and that the spoken scripts are not counted (the row allows about a tenth more); where the run happens and its price are the lead's. `eval/spike/run-plan.md` is that file at the defaults.
2. **`eva-eval spike run --host h --tier t --effort e --form f --out dir [--model name] [--repeat n] [--record file | --replay file] [--spend-model-quota] [--cli path] [--codex-ignore-user-config] [--repo dir]`.**
   - Live, `--model` required. Before anything runs: `update.CheckFlags`; the host's version from `adapter.Probe`, a run with none refused; and `results.Forbidden` on every row id the run would write, with the model's name as the word, a match refused. Without `--spend-model-quota` it prints how many calls it would make, calls nothing, and exits 2, as `cmd/live-accuracy` does. With it, each call goes through `update.NewRunner` in an empty run folder in a temporary home, removed at the end. A host's fallback from the model asked for stops the run, as in `cmd/live-accuracy`. `CachedIn` comes from `update.Result`'s cached count if DV-13 has merged, else nil.
   - The model's name comes only from `--model` and is written to no file: rows carry the tier, and the run's `Forbid` holds the name.
   - `--replay` calls no runner, needs no `--spend-model-quota`, and takes the host version from the recording.
   - Rows go to `--out` through `results.Store`, commit and dirty from `results.Source`.
3. **`eva-eval spike frontier [--catalogue file] rows...`**, from each arm's `<form>.<effort>.on.all` rows, prints a Markdown table, one line per host, tier, effort and form: `fidelity.fact_recall`, `fidelity.wrong_write`, `fidelity.misattributed`, `spike.honest_refused`, `spike.quote_miss_final`, refusals and hedges cited as assent and accepted, `spike.repair_yield`, tokens and seconds per call. Then, by rule: the default row (8.3: the cheapest, by tier then measured tokens, with `fact_recall` at least 0.90, `wrong_write` at most 0.02, `misattributed` 0; if no small row passes, the middle tier's cheapest, said to miss the bar when it does); the evidence form (5.4: segments if `quote_miss_final` is over 2% on either host at the default tier and effort); whether a typed refusal or hedge was accepted as assent at the default row (section 8: then a short typed yes does not count); and repair's yield, share of tokens and cached share (F-065), or "not measured" without cached rows. No model name: the rows hold none.
4. **`eval/spike/metrics.json`**, the catalogue of every metric DV-03c writes, each with paths `internal/eval/spike` and `internal/eval/score`.

## Owns and may touch

The instrument's file, its test and test data, and the spike's files under `eval/`.

```paths
cmd/eva-eval/spike.go
cmd/eva-eval/spike_test.go
cmd/eva-eval/testdata/spike/**
eval/spike/**
```

## Must not touch

Everything else; `internal/update`, `internal/host/adapter` and `internal/eval/**` are read only. `AGENTS.md` is the lead's: the report gives words for its layout line and for what is not run against the real machine. No one runs `run` against a real host: the live run is the owner's, on the lead's word.

## Accepted when

- `scripts/linux-check.sh` passes, and `go list -deps ./cmd/eva-md | grep -c internal/eval` prints 0. Tests run git, so `TestMain` calls `testgitenv.Clear()`.
- `eval/spike/run-plan.md` equals a fresh `plan` at the defaults, byte for byte, and its call counts equal the sum of the scripts' passes: by test.
- On a fake runner, as `cmd/live-accuracy/main_test.go` makes one, and a probe given a fake `adapter.Env`: no `--spend-model-quota` calls nothing and exits 2; a model name a row id holds is refused with exit 2; a fallback from the model stops the run; `--record` then `--replay`, with a runner that fails the test if called, gives the same rows apart from time; a made-up model name is in no byte of the rows, the recording or the output: by test.
- `frontier` on hand-made rows names the default row, the form and the assent line right in each case of item 3's rules, a small row passing, only a middle one, and none: a test per case.
- Every row passes `results.Load` and the catalogue's `Check`.
