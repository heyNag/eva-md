# Baselines

`latency-macos.json` is the stored latency baseline: what v1's hooks take, by the benchmark's scenarios, before any hook changes. Until the results store holds it, it is this file. The gate itself never reads it: the gate compares two binaries on one machine in one run (`scripts/benchmark.py --compare OLD NEW`). The file is the record a later change is read against.

## Refreshing it

The macOS job of `.github/workflows/ci.yml` runs the gate on every push to `main` and every pull request, and saves an artifact named `latency` that holds two files:

- `gate.json`: the verdict, every metric's comparison, and each run's full report.
- `latency-macos.json`: the change's own binary's figures over its runs, `new` and `new_again`, in the format below.

To refresh the baseline, open the run of the commit to take it from (a push to `main`), download its `latency` artifact, check that `gate.json` says `"verdict": "pass"` and that its `load_average_at_start` is not high, and copy its `latency-macos.json` over this folder's, unchanged. Commit it with the run's link in the message. The file names its commit, so the run can be found again.

The file in this folder now was measured on Linux, in a builder's cloud session, and says so (`"measured_on": "linux"`): the binary of commit eeb63ed, v1 as SEAM-0c left it, in the first of the twenty gates its builder ran of that binary against itself, as root, so its turn's process counts hold three git runs a hook. It shows the format and v1's figures on that machine; it is replaced from the macOS job's artifact.

## Format

One JSON object:

| Key | What it holds |
| --- | --- |
| `v` | the format's version, 1 |
| `measured_on` | the system the figures were taken on, as Python's `platform.system()` names it, lower case: `darwin` on the macOS job |
| `machine` | the processor's architecture and its count of CPUs |
| `user` | `not root`, or `root`, as which the benchmark ran: as root eva-md keeps no identity cache, so every hook runs git three times, unlike any real install |
| `load_average_at_start` | the one-minute load average when the gate started |
| `commit` | the commit the measured binary was built from (`--commit`; empty when not given) |
| `runs` | how many runs of the binary the figures pool: both its roles, twice the gate's `--rounds` |
| `p95` | how the 95th percentile is taken: by the nearest rank, process start included |
| `taken_at` | when, in UTC |
| `metrics` | for each hook figure the gate compares, by its name: `n` (samples), `p95_ms`, `median_ms` (both by the nearest rank) and `target_ms`, the hook's target in the benchmark, or `null` |
| `counts` | figures that are counts, from the first run: the processes each host's launch path starts (`hook_launch_*_processes`), and the hooks run and processes started by a turn of 50 tool calls (`turn_*_hooks`, `turn_*_processes`) |
| `per_run` | figures with one value a run, listed by run: the milliseconds a turn of 50 tool calls summed over all its hooks and over the ones the host waits for (`turn_*_ms_summed`, `turn_*_blocking_ms_summed`), and the brief built by command at the real install's shape (`real_install_brief_command_median_ms`, `real_install_brief_command_p95_ms`) |

The metrics' names are the benchmark's report keys without `_p95_ms`, such as `capture` for `capture_p95_ms`. A process count depends on the machine: as root eva-md keeps no identity cache, so every hook then runs git three times.

## The gate's rule

Each round runs every scenario on each binary twice, as four roles: `old`, `new`, `old_again` and `new_again`, old and new alternating, and every other round in reverse (`--rounds`, 2 by default, so eight runs). The runs of `old` and `old_again` together are compared with those of `new` and `new_again`. A metric fails when the lower bound of the bootstrap interval (95%, 2,000 draws, a fixed seed) of the ratio of the new binary's p95 to the old one's is above 1.15, or when the new binary's p95 is above the metric's ceiling and the old one's was not. The ceiling is the target times `--ceiling-factor`: 2 on the CI machine, the default, and 1 on the reference Mac at a release; 0 turns the ceilings off.

The controls are the two other ways to split the four roles into two pairs: `old`+`new` against `old_again`+`new_again`, and `old`+`new_again` against `old_again`+`new`. Each side holds one role of each binary, so a change cancels out and only what the machine did between runs is left; with the binaries alike, the three splits are alike too, and no stretch of runs in the order the gate uses can load the new binary's runs more than the old one's without loading the two roles of one binary unevenly, which a control sees. Each control is judged both ways by the ratio rule alone: a fixed ceiling says where a binary sits, not how noisy the machine is. A control that fails means two sets of runs a change cancels out of differed as a regression would. On a metric with at least 100 samples on every side, the machine was too noisy to judge anything: load in some runs and not others moves every figure, so the gate says `not judged` and exits 0. On a metric with fewer, whose p95 is one of its few largest samples, only that metric is not judged.

A scenario that fails in a run, as when load runs a hook past its time limit, leaves that run's samples of it out and counts against the binary that failed it. The verdict is the first of these that applies:

1. `could not run`, exit 2: a scenario failed in every run of both binaries, or the gate itself stopped on an error.
2. `fail`, exit 1: the new binary failed a scenario in at least two more runs than the old one, as a hook that hangs or fails would, or gave a metric no samples that the old one gave samples for.
3. `not judged`, exit 0: the machine was too noisy.
4. `fail`, exit 1: a judged metric failed.
5. `not judged`, exit 0: a metric's control failed.
6. `pass`, exit 0.

A metric the old binary gave no samples, as one added with the change, is listed as not compared. `gate.json` keeps every sample, by role, so a verdict can be worked out again. The step is advisory until an unchanged binary has passed it twenty times in twenty; then the lead removes `continue-on-error` from it.
