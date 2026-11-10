# Baselines

`latency-macos.json` is the stored latency baseline: what v1's hooks take, by the benchmark's scenarios, before any hook changes. Until the results store holds it, it is this file. The gate itself never reads it: the gate compares two binaries on one machine in one run (`scripts/benchmark.py --compare OLD NEW`). The file is the record a later change is read against.

## Refreshing it

The macOS job of `.github/workflows/ci.yml` runs the gate on every push to `main` and every pull request, and saves an artifact named `latency` that holds two files:

- `gate.json`: the verdict, every metric's comparison, and each run's full report.
- `latency-macos.json`: the change's own binary's figures over its runs, in the format below.

To refresh the baseline, open the run of the commit to take it from (a push to `main`), download its `latency` artifact, check that `gate.json` says `"verdict": "pass"` and that its `load_average_at_start` is not high, and copy its `latency-macos.json` over this folder's, unchanged. Commit it with the run's link in the message. The file names its commit, so the run can be found again.

The file in this folder now was measured on Linux, in a builder's cloud session, and says so (`"measured_on": "linux"`). It shows the format and v1's figures on that machine; it is replaced from the macOS job's artifact.

## Format

One JSON object:

| Key | What it holds |
| --- | --- |
| `v` | the format's version, 1 |
| `measured_on` | the system the figures were taken on, as Python's `platform.system()` names it, lower case: `darwin` on the macOS job |
| `machine` | the processor's architecture and its count of CPUs |
| `load_average_at_start` | the one-minute load average when the gate started |
| `commit` | the commit the measured binary was built from (`--commit`; empty when not given) |
| `runs` | how many runs of the binary the figures pool (the gate's `--rounds`) |
| `p95` | how the 95th percentile is taken: by the nearest rank, process start included |
| `taken_at` | when, in UTC |
| `metrics` | for each hook figure the gate compares, by its name: `n` (samples), `p95_ms`, `median_ms` (both by the nearest rank) and `target_ms`, the hook's target in the benchmark, or `null` |
| `counts` | figures that are counts, from the first run: the processes each host's launch path starts (`hook_launch_*_processes`), and the hooks run and processes started by a turn of 50 tool calls (`turn_*_hooks`, `turn_*_processes`) |
| `per_run` | figures with one value a run, listed by run: the milliseconds a turn of 50 tool calls summed over all its hooks and over the ones the host waits for (`turn_*_ms_summed`, `turn_*_blocking_ms_summed`), and the brief built by command at the real install's shape (`real_install_brief_command_median_ms`, `real_install_brief_command_p95_ms`) |

The metrics' names are the benchmark's report keys without `_p95_ms`, such as `capture` for `capture_p95_ms`. A process count depends on the machine: as root eva-md keeps no identity cache, so every hook then runs git three times.

## The gate's rule

A metric fails when the lower bound of the bootstrap interval (95%, 2,000 draws, a fixed seed) of the ratio of the new binary's p95 to the old one's is above 1.15, or when the new binary's p95 is above the metric's ceiling and the old one's was not. The ceiling is the target times `--ceiling-factor`: 2 on the CI machine, the default, and 1 on the reference Mac at a release. In the same run the new binary runs again as a control and is judged against itself, both ways, by the same rule; if that fails, the machine was too noisy to judge, and the gate says `not judged` and exits 0. The step is advisory until an unchanged binary has passed it twenty times in twenty; then the lead removes `continue-on-error` from it.
