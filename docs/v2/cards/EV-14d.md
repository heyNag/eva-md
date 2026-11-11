# EV-14d: v1's fidelity row, from a recording

**Plan:** section 8 Stage 1 row EV-14 ("with v1's score"), the Stage 0 text that EV-14 "takes v1's row from DV-02's recordings", the Stage 0 row DV-02 ("the output is recorded as fixtures"), section 6.6. **Design:** `docs/v2/working-papers/design/evaluate.md` sections 4.2 (recorded backend) and 5.3, and its rows EV-01 and EV-14 in section 21. **Size:** M. **Reviewers:** one. **Depends on:** EV-14a and EV-14b. Before EV-10a, which freezes the documents item 5 writes in. **Part of:** EV-14; runs beside EV-14c, with no file in common.

## Why

v1's row on the fidelity scorer must come from what real models wrote, which the owner's live gates (DV-02) record. Two programs must agree on that recording: `cmd/live-accuracy`, which runs v1's update on the 12 scripts, and the scorer. This part fixes the format, lets the live run write it and replay it with no model call, and turns the pages it ends with into rows. Nothing in eva-md changes, so there is no switch; a run of `live-accuracy` without the new flags does exactly what it does today.

## What to build

1. **The recording**, one JSON line for each pass, written as the run goes: `{"v":1,"host":…,"effort":…,"model":…,"script":<1 to 12, the order of accuracy.Scripts()>,"pass":<from 1>,"output":<the answer's JSON, or null>,"reason":<the runner's reason, or "">}`. Nothing else: no token count, no time, no path.
2. **Three flags on `cmd/live-accuracy`.** `--record <file>` writes the recording during a live run, to a file that must not exist yet. `--replay <file>` answers each pass from a recording: it calls no runner, needs no `--spend-model-quota`, and prints the same report a live run prints. It refuses, before anything runs, a recording of another host or version, one that lacks a pass of the suite or holds one twice, and one with a reason the update does not know. `--pages <dir>` writes each script's page after its last pass, rendered with `threadpage.Render`, as `<dir>/v1.NN.md`, to a folder that is new or empty. `TestOnlyItImportsTheSuite` allows only `main.go` to import the suite, so `record.go` holds the format and its checks and imports neither `internal/update/accuracy` nor its `live` package; what needs them stays in `main.go`, and that test passes unchanged.
3. **`eva-eval fidelity [--repo dir] --pages dir --out dir [--host h --host-version v] [--model m]`**, in `cmd/eva-eval/fidelity.go`, which adds the command with `register` from its `init`; `main.go` is not edited. For each of `scenario.V1()` it reads `v1.NN.md` with `v1page.ReadThread` (a missing page is an empty store), scores it with `score.Score`, and appends rows through `results.Store` with `Forbid` set to every scenario's `scenario.Forbid`: instrument `fidelity`, seed 0, scenario `v1.NN` for each script and `v1` for the suite's totals, commit and dirty from `results.Source`. It prints one line for each scenario, of numbers and outcome codes only.
4. **`eval/fidelity/metrics.json`**, the catalogue of EV-14b's nine metrics, each with paths `internal/update`, `internal/threadpage`, `internal/eval/scenario`, `internal/eval/score` and `internal/eval/v1page`. No gates file and no baseline yet: both wait for DV-02's recordings.
5. **Docs.** The live accuracy run's text gains the three flags: in `docs/design.md` and `docs/verification.md` (their "Live accuracy run" text) and the sentence in `README.md`; if EV-10a has merged, in the `## Since the split` section of the spec that holds that text instead. `AGENTS.md` is not edited: its sentence stays true of a live run.

## Owns and may touch

`cmd/live-accuracy/main.go`, `cmd/live-accuracy/record.go` and `record_test.go` (new), `cmd/eva-eval/fidelity.go` and `fidelity_test.go` (new), `cmd/eva-eval/testdata/fidelity/**` (new), `eval/fidelity/**` (new), and the docs text named in item 5.

## Must not touch

Everything else. `cmd/live-accuracy/main_test.go`, `internal/update/**`, `internal/threadpage/**`, `internal/eval/scenario`, `internal/eval/score`, `internal/eval/v1page`, `internal/eval/results` and `AGENTS.md` are read, never edited.

## Accepted when

- `scripts/linux-check.sh` passes, and `go list -deps ./cmd/eva-md | grep -c internal/eval` prints 0.
- A live run on a fake runner (as `main_test.go`'s tests make one) with `--record` and `--pages` writes 21 lines and 12 pages. `--replay` of that file, with a runner that fails the test if called, prints the same report on stdout, exits with the same code and writes the same pages byte for byte, by test.
- Each refusal of item 2 exits 2 and calls nothing, by test, and so does a recording from a run that stopped part way.
- On committed pages of at least three scripts in `cmd/eva-eval/testdata/fidelity/`, `eva-eval fidelity` gives `fidelity.fact_holds` 1 for each; a copy with one value changed raises `fidelity.wrong_write` by one on that scenario, by test.
- Every row passes `results.Load` and the catalogue's `Check`; no row field and no line printed holds any word of `scenario.Forbid`; two runs give identical rows apart from time, by test.
- A live run without the new flags prints and does what it did before: `cmd/live-accuracy/main_test.go` passes unchanged.
- No new module; only eva's name in any output.
