# EV-03c: the pilot's report, over the continuation's rows

**Plan:** section 8, Stage 0 row EV-03 (variance, tokens and minutes per session, whether tasks can show a difference), as ruled on 2026-11-13; 15.4 (a session's price); decision 16. **Design:** `working-papers/design/evaluate.md` 6.3 (screens), 7 and 9. **Size:** M. **Reviewers:** one. **Depends on:** [EV-17g](EV-17g.md).

## Why

The pilot's figures come from the driver's rows and EV-17g's run records. EV-17 screens its tasks on the same rows, so the report is kept beside the instrument; only the README is the pilot's. No switch; no line added to `./cmd/eva-md`. Elsewhere: the export (EV-03b); the record, the no-usage stop, what a chat can reach (EV-17g); `memory_off` and memory's cost (EV-17c); the live run (the lead).

## What to build

1. **`eva-eval continuation report --tasks DIR [--catalogue file] [--rates in,cached,out] OUT...`** in `cmd/eva-eval/continuation_report.go`; the work in `internal/eval/continuation/report`, which takes loaded tasks, so tests may build them. `report` adds itself from its file to the map EV-17g gives `continuation`'s subcommands. Output: ids and figures in fixed words, by host, task, then condition. Exit 0, or 2 with no figure for a usage error or refused data.
2. **Reads.** Each OUT is a results folder: rows by `results.Load` (none if there is no `results.jsonl`), held by `Check` to the catalogue (default `eval/continuation/metrics.json`); records by `drive.LoadRuns` (`runs.jsonl`). Tasks by `task.LoadAll`: for the pilot, the export's `tasks/`. A file over 64 MiB is refused unopened, as both loaders read whole. No host output is read; the loaders admit ids and numbers. Their errors, which can hold a path, are not shown.
3. **Refused:** instrument not `continuation`, or no host; a metric whose family is not its task's; a scenario not `<task>.t<n>` with n the seed; a task, in a row or a record, not in `--tasks`; one host, metric and scenario twice, in a folder or across two; a trial lacking one of `solved` to `wall_ms`, or, under `eva`, `memory_tokens` or `memory_ms`; a folder with no record; per folder, host and condition, rows or judged trials not the sum of the records' `rows` and `judged`. `asked_again` is not read.
4. **Per host and condition, from the records:** runs by `ended`; `planned`, `judged`, `not_run`, `unjudged` by every code of `drive.Codes` (`memory_off` among them), `no_model`, `tokens`; each once, `commit`, `dirty`, `host_versions`, `model` asked, and the models the rows name. Two commits are both shown.
5. **Per host, task and condition, from rows:** trials judged, solved, solved first try; median, least and most of corrections, rounds, `agent_events`, prompt, cached and output tokens and minutes (`wall_ms`); under `eva`, `memory_tokens` and memory minutes. An even count's median is the mean of the middle two.
6. **A session's cost**, per host and condition: those medians over every judged trial. With `--rates`, money per million tokens, uncached prompt, cached and output; each trial's price, ((prompt − cached) × in + cached × cached + output × out) / 10⁶, and its median; under `eva`, memory apart: `memory_tokens` at `in` (a floor: it sums input and output) and its minutes. No rate is committed.
7. **Variance**, per host, `eva` and `oracle` each against `off`: per task and trial number both judged with both figures above 0, `ln(arm/off)` of `agent_events` and of `prompt_tokens`; pairs, mean, `s` (over pairs less one; none under two pairs, said so), `n = ceil((3·s / ln(4/3))²)`.
8. **Screens**, per host and task (design 6.3): a hidden decision passes when `oracle` solved at the first try in two trials or more and `off` did not in two or more; a handoff when `off` solved once or more and `oracle`'s median `agent_events` is at most 0.7 of `off`'s; a control task, none. Too few: under two judged trials of either. Tasks passed, per host.
9. **`eval/pilot/README.md`**: tasks, checker and export are throwaway; a run needs the owner's leave and ceiling. In order: `pilot check`; `pilot export`; `continuation check` on it; eva-md built `-tags evamd_eval`; a smoke run, one task and trial per host and condition, in its own `--out`; six runs (2 hosts × 3 conditions), `--trials 3`, the export's `--session-timeout`, one fresh `--out`, an empty `--work` each, one `--model`; `report` over every folder with the full export's `tasks/`. A task with no trial judged, or of a killed run (rows no record covers), reruns alone: `pilot export --task`, a fresh `--out`. Container: git 2.44+, Go, python3, the CLIs, the provider open, the CLI as a second user via `--cli`, an evaluation key by `--pass-env`. Command lines are in `drive/hosts.go`. Facts: F-067 judged trials, F-068 `agent_events`, F-069 corrections, F-115 and F-163 `memory_off`, F-207 tokens and minutes; not F-070 to F-072, F-116, F-117, F-169.

## Owns and may touch

All new.

```paths
internal/eval/continuation/report/**
cmd/eva-eval/continuation_report.go
cmd/eva-eval/continuation_report_test.go
eval/pilot/README.md
```

## Must not touch

Everything else; the driver, `task`, `results`, the catalogue, `main.go` and `pilot.go` are read only; `internal/eval/pilot` is not imported. Design 9 and decision 16's ceiling are the lead's.

## Accepted when

- `scripts/linux-check.sh` passes; `TestNoReleaseContainsIt`, `TestContinuationIsRegistered` and `TestHostLiteralCount` unchanged.
- On hand-made rows and records in the package's `testdata`, every median (odd, even), least, most, mean, `s`, `n`, price and screen (pass, fail, too few) is exact; one pair, or two with one at 0 events, gives no `s`.
- Each refusal of item 3, and a file just over 64 MiB (sparse), exits 2: a test each. A folder of records and no rows is read.
- Two records' unjudged counts add; an unjudged trial moves no figure.
- `off` and `oracle` run on `newContRig` into one `--out`, each with an empty `--work`; `report` exits 0 with their judged counts.
- Output and errors hold no path and no word of the tasks' `Forbid`; the README passes `TestEvalPilotHoldsNoSecretAndNoWorkplace`, naming no person, model or machine.
- Mutations (`s` over pairs, a screen a trial short, cached at the `in` rate, one middle for an even median, a duplicate let through, a code dropped) each fail a test.

## The lead's rulings of 2026-11-13

The card's checker left these to the lead.

- **EV-17g adds the command's map**, so this card owns only its own files.
- **The run records are `drive.Record`**, read with `drive.LoadRuns`, as EV-17g's item 4 fixes them.
- **The README holds the commands and the rules; the run-book is the lead's** and holds the split of the owner's ceiling over the runs, the container and the order of the live run.
- **Rows of more than one commit for a host and condition are shown, not refused**: the host table may be corrected during the pilot.
- **The screens are read as carded**: a hidden decision passes twice when it is solved at the first try; a handoff is solvable from the repository alone when memory off solved it once or more; and 30% fewer agent events means the ideal record's median is at most 0.7 of memory off's.
- When EV-17e deletes the pilot it takes `eval/pilot/README.md` with the rest; its paths cover the folder.
