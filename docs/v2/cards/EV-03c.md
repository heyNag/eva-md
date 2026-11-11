# EV-03c: the pilot's report and its README

**Plan:** section 8, Stage 0 row EV-03 ("variance, tokens and minutes per session, and whether the tasks can show a difference at all"); section 15.4 (a price for one evaluation session); decision 16. **Design:** `docs/v2/working-papers/design/evaluate.md` sections 6.3 (the screens), 7 (paired log-ratios) and 9 (the budget the report replaces). **Size:** S. **Reviewers:** one. **Depends on:** EV-03a (the `pilot` map). It may run beside [EV-03b](EV-03b.md): the rows it reads are the ones EV-03b's item 6 names.

## Why

The pilot exists for three figures: what one session costs, how much a paired outcome varies, which sets how many trials a pack needs, and which tasks can show a difference at all. They come from rows, never from a chat's text. Throwaway: in no release, deleted when EV-17 lands. Nothing v1 does changes: no switch.

## What to build

1. **`eva-eval pilot report [--catalogue file] [--rates in,cached,out] rows`**, in `cmd/eva-eval/pilot_report.go`, added to EV-03a's `pilot` map; the work in a package `internal/eval/pilot/report`. It reads the rows with `results.Load`, checks them with the catalogue's `Check` (default `eval/pilot/metrics.json`), and refuses a row whose instrument is not `pilot` or whose scenario is not `<task>.<arm>.t<n>` with arm `off`, `eva` or `oracle`. The family is the task id's first letter: `h` a handoff, `d` a hidden decision (EV-03a). It prints ids and numbers only. Exit 0, or 2 for a usage error or rows it cannot read.
2. **Per host, task and arm**: trials judged, and unjudged by reason (`pilot.unjudged.*`); trials solved, solved at the first try, and corrections; the median, least and most of `agent_events`, `prompt_tokens`, `cached_tokens`, `output_tokens` and minutes (`wall_ms`). An `eva` trial whose `hook_seen` is 0 is printed as memory not on and counts in no figure.
3. **A session's cost**, per host and arm: the median prompt, cached and output tokens and minutes. With `--rates`, money for a million uncached prompt tokens, cached prompt tokens and output tokens, it adds the median price, (prompt − cached) × in + cached × cached + output × out, over a million, and the `eva` arm's update apart, `update_tokens` at the `in` rate (a floor: it sums input and output) and `update_ms` in minutes. No rate is kept in the repository: the lead gives them at run time.
4. **Variance**, per host, for `eva` and `oracle` each against `off`: for every task and trial number where both trials are judged and both figures above 0, `ln(arm/off)` of `agent_events` and of `prompt_tokens`; `s`, their sample standard deviation (divided by pairs less one; none under two pairs, said so); `n = ceil((3·s / ln(4/3))²)`, the paired trials a pack needs to see a 25% reduction at three standard errors.
5. **Screens**, per host and task (design 6.3): a hidden decision passes when `oracle` solved at the first try in two trials or more and `off` did not in two or more; a handoff passes when `off` solved in one trial or more and `oracle`'s median `agent_events` is at most 0.7 of `off`'s. Each prints pass, fail, or too few trials.
6. **`eval/pilot/README.md`**: the pilot is throwaway and is run only with the owner's leave and inside the ceiling the owner names; what a container needs (git 2.44 or newer, Go, python3, the host CLIs, an eva-md built from the checkout `--repo` names, the provider's API host open, the tasks folder out of the sessions' reach); what a key needs (made for evaluation only, reaching the CLI from the container's environment or proxy, never on a command line or in a file); that each host's command line is in `internal/eval/pilot/run/hosts.go` (EV-03b); and which rows answer which host fact of `docs/v2/host-facts.md`: `hook_seen` F-115 and F-163, `agent_events` F-068, corrections by resume F-069, a judged trial F-067, tokens and minutes F-207. F-070, F-071, F-072, F-116, F-117 and F-169 are not answered by the pilot.

## Owns and may touch

The package, the command's file and test, and the README, all new.

```paths
internal/eval/pilot/report/**
eval/pilot/README.md
cmd/eva-eval/pilot_report.go
cmd/eva-eval/pilot_report_test.go
```

## Must not touch

Everything else. `internal/eval/results`, `cmd/eva-eval/main.go` and `cmd/eva-eval/pilot.go` are read only; `eval/pilot/metrics.json` is EV-03b's. Section 9 of the design and decision 16's ceiling are rewritten by the lead from the report.

## Accepted when

- `scripts/linux-check.sh` passes, and `TestNoReleaseContainsIt` passes unchanged.
- On hand-made rows and a catalogue in the package's `testdata`, every median, least and most, `s`, `n`, price and screen, each screen both ways, is exact, by test. One pair, and a pair with 0 events, give no `s` and say so.
- An `eva` trial with `hook_seen` 0 changes no figure and is printed as memory not on.
- A row of another instrument, an unknown arm, or a metric not in the catalogue exits 2.
- The README names no person, model or machine, and EV-03a's test of every file under `eval/pilot/` passes with it.
- A reviewer's mutations (`s` divided by the pairs, a screen one trial short, cached tokens at the uncached rate, a memory-not-on trial counted) each fail a test.
