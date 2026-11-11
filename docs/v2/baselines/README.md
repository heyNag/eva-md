# Baselines

`latency-macos.json` is the stored latency baseline: what v1's hooks take, by the benchmark's scenarios, before any hook changes. Until the results store holds it, it is this file. The gate itself never reads it: the gate compares two binaries on one machine in one run (`scripts/benchmark.py --compare OLD NEW`). The file is the record a later change is read against.

## Refreshing it

The `latency` job of `.github/workflows/ci.yml`, on macOS and apart from the tests, runs the gate on every push to `main` and every pull request, and saves an artifact named `latency` that holds two files:

- `gate.json`: the verdict, every figure's comparison, each run's full report, the ceilings, and every sample, by role and run, unrounded.
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

Each round runs every scenario on each binary twice, as four roles: `old`, `new`, `old_again` and `new_again`, old and new alternating, and every other round in reverse (`--rounds`, 2 by default, so eight runs, four of each binary). The gate keeps each role's samples by run. All the old binary's runs together are compared with all the new one's, by the lower bound of the bootstrap interval (95%, 2,000 draws, a fixed seed) of the ratio of the new binary's p95 to the old one's. The p95 of a resample is one of the sorted samples, and its rank is drawn directly, from the law of the 95th percentile of that many uniform draws, so no resample is built: the bound is the one resampling gives, the same for the same samples in any order, and quick to work out.

The controls are splits of the runs into two sides that each hold half of the old binary's runs and half of the new one's: 18 at two rounds, among them the two splits of whole roles, `old`+`new` against `old_again`+`new_again` and `old`+`new_again` against `old_again`+`new`. With more rounds the controls are a seeded sample of at most 36 splits that always holds those two. Each side holds as many runs of each binary, so a change cancels out and only what the machine did between runs is left. Load that falls on some runs and not others puts as much between the sides of some split as between the two binaries: load on one run of `new` and one of `new_again` is seen by the 6 splits that put both on one side, though neither split of whole roles can see it. A figure's noise is the fourth largest of its controls' 36 lower bounds, both ways (the 0.89 quantile, by nearest rank, at any number of rounds), and at least 1.

Why the fourth largest: the largest of 36 bounds is a biased measure of the noise, high whenever any one split is, and on a quiet machine it called the gate not judged on a plain rise of 20% or 30% in every hook (the first table below). The fourth largest still sees load on one run of each of the new binary's roles, whose 6 splits are well above it, with two to spare; the sixth largest would have none. In the simulation the third, fourth and sixth largest gave, on a quiet machine, a rise of 30% failed 82.7%, 89.3% and 91.3% of the time and an unchanged binary not judged 4.7%, 4.0% and 2.7%, at 150 seeds a case, and none failed an unchanged binary with one run of each of the new binary's roles loaded, or whole runs loaded.

A figure is judged only with at least 100 samples on each binary's side. In the gate's runs, and only there, the chat-start scenarios time 25 chat starts a run, not 10 or 20, so every figure has that many at the default rounds; the plain benchmark's report and its sample counts are as they were. A figure with fewer, as at `--rounds 1` or after a scenario failed in some runs, is listed as having too few samples: it neither fails the build nor stops the verdict. A judged figure whose noise is above 1.15 says the machine was too noisy, and stops the whole verdict: only the controls call the machine noisy. Otherwise a figure fails when its lower bound is above 1.15 times its noise, or when it passes its ceiling: the lower bound of the interval of the new binary's p95 is above the ceiling and the old binary's p95 is not, so a figure at its ceiling by sampling alone does not fail. The ceiling is the target times `--ceiling-factor`: 2 on the CI machine, the default, and 1 on the reference Mac at a release; 0 turns the ceilings off. The controls have no ceiling: a fixed ceiling says where a binary sits, not how noisy the machine is. A figure whose lower bound is above 1.15 but not above 1.15 times its noise is unsettled. It stops nothing: another figure that failed still fails the gate.

A scenario that fails in a run, as when load runs a hook past its time limit, leaves that run's samples of it out and counts against the binary that failed it. A hook still running after 4 seconds is killed with its whole process group, on Codex's launch path too, where the shell runs the binary as its child. The new binary failing a scenario in at least three quarters of its runs more than the old one (3 of 4 at two rounds) fails the gate, as a hook that hangs or fails would. Half of its runs more (2 of 4) makes the verdict `not judged`, with a line that says to run the gate again: load can lose a scenario that often. Fewer decides nothing. The verdict is the first of these that applies:

1. `could not run`, exit 2: a scenario failed in every run of both binaries, or the gate itself stopped on an error.
2. `fail`, exit 1: the new binary failed a scenario in three quarters of its runs more than the old one, or gave a figure no samples that the old one gave samples for, when it lost no scenario in every run.
3. `not judged`, exit 0: a judged figure's noise called the machine too noisy.
4. `fail`, exit 1: a figure failed.
5. `not judged`, exit 0: a scenario was lost in half the new binary's runs more, or a figure was unsettled.
6. `pass`, exit 0.

A figure the old binary gave no samples, as one added with the change, is listed as not compared. `gate.json` keeps every sample by role and run, unrounded, with the ceilings, so that `redecide` in `scripts/bench/core.py` works the verdict out again exactly. The step is advisory until an unchanged binary has passed it twenty times in twenty; then the lead removes `continue-on-error` from it.

### The rule measured

`python3 -I scripts/bench/sim_gate.py` feeds the rule made-up gates with the gate's own sample counts and the shapes the proof's samples have (a long right tail, a few slow outliers, runs whose medians differ by 3.5% to 10% on a quiet machine), under a load and with a change to the new binary, 300 seeds a case. It takes minutes, and is run by hand. The loads: none; light bursts (about one a run, a twentieth of a run long, 1.2 to 1.6 times slower); heavy bursts (three a run, a tenth of a run long, 1.5 to 3 times); whole runs (each run loaded or not on its own, a quarter of them, 1.2 to 1.8 times); a drift (the machine half as slow again by the end); and one run of `new` and one of `new_again` loaded, 1.2 to 2 times. For the rule as it now is:

| Load | Change to the new binary | Fail | Pass | Not judged |
| --- | --- | --- | --- | --- |
| none | none | 0.3% | 97.3% | 2.3% |
| none | every hook 20% slower | 93.0% | 0% | 7.0% |
| none | every hook 30% slower | 89.7% | 0% | 10.3% |
| none | 20 ms added to capture | 98.0% | 0% | 2.0% |
| light bursts | none | 0% | 6.7% | 93.3% |
| heavy bursts | none | 0% | 0% | 100% |
| whole runs | none | 0.3% | 12.0% | 87.7% |
| a drift | none | 0% | 42.7% | 57.3% |
| one run of `new` and one of `new_again` | none | 0% | 0% | 100% |

Against the card's aims: an unchanged binary failed at most 1 time in 300 under every load, the quiet machine's among them (aim: at most 1 in 100), and was not judged 2.3 times in 100 on a quiet machine (aim: at most 5); on a quiet machine a rise of 30% in every hook failed 89.7 times in 100 (aim: at least 85), one of 20% 93 (aim: at least 70), and 20 ms added to capture alone 98 (aim: at least 95). Every aim is reached. What is given for it: a rise of 30% is caught a little less often than one of 20%, since a real change makes the controls noisier too (each side's p95 then comes from its two runs of the new binary alone), and the figure whose runs differ most on a quiet machine, `transcript_capture`, is what leaves most of the rest not judged. Under load an unchanged binary is mostly not judged, which is what the rule is for: it never fails a build on noise, and the gate is to be run again on a quieter machine.

The rule as it was merged first, for comparison, measured by its reviewer on the proof's own samples: an unchanged binary failed 4.7, 9.3 and 5 times in 100 under light bursts, heavy bursts and whole runs loaded, mostly from load on one run of each of the new binary's roles, which neither control of whole roles sees; on a quiet machine a rise of 20% failed 33 times in 100 and one of 30% 43.5, being not judged the rest; and two runs of a scenario lost to load failed the build. With noise from all 18 splits but the largest of the 36 bounds as the noise, an unchanged binary failed 0.3 to 1 time in 100 under load, and a rise of 20%, 30% and 50% failed 79, 74.5 and 86.5 times in 100.

The 21 gates of that proof, 20 of an unchanged binary and one with 20 ms added to capture, worked out again by this rule from their samples by run (`sim_gate.py --rework`): the unchanged ones passed 17 times and were not judged 3 times, by the controls of `transcript_capture` (twice) and `capture_with_spawn`, and never failed; the one with 20 ms added failed, on 12 figures. Their chat-start figures had 10 samples a run then, so 5 of them were listed as having too few samples.
