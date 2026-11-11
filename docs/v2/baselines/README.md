# Baselines

`latency-macos.json` is the stored latency baseline: what v1's hooks take, by the benchmark's scenarios, before any hook changes. Until the results store holds it, it is this file. The gate itself never reads it: the gate compares two binaries on one machine in one run (`scripts/benchmark.py --compare OLD NEW`). The file is the record a later change is read against.

## Refreshing it

The `latency` job of `.github/workflows/ci.yml`, on macOS and apart from the tests, runs the gate on every push to `main` and every pull request, and saves an artifact named `latency` that holds two files:

- `gate.json`: the verdict, every metric's comparison, and each run's full report.
- `latency-macos.json`: the change's own binary's figures over its runs, `new` and `new_again`, in the format below.

To refresh the baseline, open the run of the commit to take it from (a push to `main`), download its `latency` artifact, check that `gate.json` says `"verdict": "pass"`, and copy its `latency-macos.json` over this folder's, unchanged. A pass means the machine was quiet enough: every figure's controls stayed within the gate's rule. The load average the file records says little on the CI machine, where it reads about 15 on 3 CPUs at the start of a job that has only just built two binaries. Commit it with the run's link in the message. The file names its commit, so the run can be found again.

The file in this folder now comes from the `latency` job's run of commit 84f0fc7 on `main`, the first in which the gate ran in a job of its own, and its verdict was pass: v1's hooks as they stand before any package of the v2 build has changed one. (The commit changed only the CI workflow and documents, so the two binaries it compared were alike.) The first file here was measured on Linux, in a builder's cloud session, to show the format; this one replaces it.

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

Each round runs every scenario on each binary twice, as four roles: `old`, `new`, `old_again` and `new_again`, old and new alternating, and every other round in reverse (`--rounds`, 2 by default, so eight runs). The runs of `old` and `old_again` together are compared with those of `new` and `new_again`, by the lower bound of the bootstrap interval (95%, 2,000 draws, a fixed seed) of the ratio of the new binary's p95 to the old one's.

The controls are the two other ways to split the four roles into two pairs: `old`+`new` against `old_again`+`new_again`, and `old`+`new_again` against `old_again`+`new`. Each side holds one role of each binary, so a change cancels out and only what the machine did between runs is left; with the binaries alike, the three splits are alike too, and no stretch of runs in the order the gate uses can load the new binary's runs more than the old one's without loading the two roles of one binary unevenly, which a control sees. A metric's noise is the largest lower bound of its controls' ratios, both ways, and at least 1.

A metric fails when its lower bound is above 1.15 times its noise, or when the new binary's p95 is above the metric's ceiling, the old one's was not, and its noise is at most 1.15. The ceiling is the target times `--ceiling-factor`: 2 on the CI machine, the default, and 1 on the reference Mac at a release; 0 turns the ceilings off. The controls have no ceiling: a fixed ceiling says where a binary sits, not how noisy the machine is. A metric whose lower bound is above 1.15 but not above 1.15 times its noise, or whose p95 passed the ceiling while its noise is above 1.15, is unsettled: the machine alone put as much between two sets of runs. So is one that would fail while a control has a side with no samples. An unsettled metric with at least 100 samples on every side means the machine was too noisy to judge anything: load in some runs and not others moves every figure, so the gate says `not judged` and exits 0. On a metric with fewer, whose p95 is one of its few largest samples, only that metric is not judged.

A scenario that fails in a run, as when load runs a hook past its time limit, leaves that run's samples of it out and counts against the binary that failed it. The verdict is the first of these that applies:

1. `could not run`, exit 2: a scenario failed in every run of both binaries, or the gate itself stopped on an error.
2. `fail`, exit 1: the new binary failed a scenario in at least two more runs than the old one, as a hook that hangs or fails would, or gave a metric no samples that the old one gave samples for.
3. `not judged`, exit 0: the machine was too noisy.
4. `fail`, exit 1: a metric failed.
5. `not judged`, exit 0: a metric was unsettled.
6. `pass`, exit 0.

A metric the old binary gave no samples, as one added with the change, is listed as not compared. `gate.json` keeps every sample, by role, so a verdict can be worked out again. The step is advisory until an unchanged binary has passed it twenty times in twenty; then the lead removes `continue-on-error` from it.

## Passes toward blocking

The gate may block a merge once an unchanged binary has passed it 20 times with no fail between them, in runs that count, with fewer than one in four of those runs not judged. A run counts when it is the `latency` job's run of a push to `main`, and no file under `cmd/` or `internal/`, nor `go.mod` or `go.sum`, changed between the base its step names ("Comparing BASE with SHA") and its commit (`git diff --quiet BASE SHA -- cmd internal go.mod go.sum`), so that the two binaries it compared are alike. Its verdict is read in the step's log or in its artifact's `gate.json`, never from the run's colour, since the step is advisory. A pass adds one to the count and a `fail` starts it again. A `not judged` does neither, since it blocks nothing, but it goes into the table: a gate that cannot judge one run in four or more is not ready to block. A run of a commit that changed any of them counts neither way. A change to the gate's rule (`scripts/benchmark.py`, `scripts/bench/`) starts the count again. The lead fills the table at each merge.

| Run | Commit | Verdict |
| --- | --- | --- |
| 56 | ecd8507 | pass |
| 57 | 84f0fc7 | pass |
| 58 | f52289f | pass |
| 59 | 361ed5c | fail |
| 60 | 7d824ae | pass |
| 61 | e19d195 | not judged |
| 62 | 02b9096 | not judged |
| 63 | accc638 | fail |
| 65 | c3afbd9 | pass |
| 66 | 86767db | pass |
| 67 | 10024d0 | pass |
| 69 | 8805e3c | not judged |
| 70 | 3a6a444 | not judged |
| 71 | 3997e78 | pass |
| 72 | 484017a | pass |
| 75 | c0e0eaa | not judged |
| 76 | d1e2a39 | not judged |
| 78 | 38256be | pass |

These 18 runs are the first rule's (EV-16), from before the gate's second round: 10 passes, 2 fails and 6 not judged, on binaries that were alike. So the first rule fails an unchanged binary on CI's runner about one time in nine, and the count stands at 0. The runner was busy each time the gate began (a load of 10 to 30 on 3 CPUs, left by the two builds before it), so since 2026-11-11 the job waits, five minutes at most, for the load to fall under the CPUs before it times anything. Worked out again with the second round's rule, the same 23 saved runs (these 18 and 5 of changed binaries) give no fail, 2 passes and 21 not judged: that rule does not fail on noise, and this runner, as busy as it was, gave it almost nothing it could judge. Whether the wait changes that is read from the runs after it.
