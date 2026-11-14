# Baselines

`latency-macos.json` is the stored latency baseline: what v1's hooks take, by the benchmark's scenarios, before any hook changes. Until the results store holds it, it is this file. The gate itself never reads it: the gate compares two binaries on one machine in one run (`scripts/benchmark.py --compare OLD NEW`). The file is the record a later change is read against.

## Refreshing it

The `latency` job of `.github/workflows/ci.yml`, on macOS and apart from the tests, runs the gate on every push to `main` and every pull request, and saves an artifact named `latency` that holds two files:

- `gate.json`: the verdict, every figure's comparison, each run's full report, the ceilings, the scenario each figure comes from, and every sample, by role and run, unrounded. It keeps neither binary's path, which wherever the gate runs lies under a home folder.
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

This is the gate's third rule (EV-16d), `"rule": 3` in `gate.json`. Each round runs every scenario on each binary twice, as four roles: `old`, `new`, `old_again` and `new_again`, old and new alternating, and every other round in reverse (`--rounds`, 3 by default, so twelve runs, six of each binary). The gate keeps each role's samples by run. All the old binary's runs together are compared with all the new one's, twice: by the lower bound of the bootstrap interval (95%, 2,000 draws, a fixed seed) of the ratio of the new binary's p95 to the old one's, and by the lower bound of that of the ratio of their medians. The p95 or the median of a resample is one of the sorted samples, and its rank is drawn directly, from the law of that order statistic of that many uniform draws, so no resample is built: the bound is the one resampling gives, the same for the same samples in any order, and quick to work out. The draws are counted by their pair of ranks, which gather about the quantile's own rank, so each pair's ratio is sorted once; the ends of the interval are the same as when all 2,000 ratios are sorted.

The controls are splits of the runs into two sides that each hold half of the old binary's runs and half of the new one's: 18 at two rounds, among them the two splits of whole roles, `old`+`new` against `old_again`+`new_again` and `old`+`new_again` against `old_again`+`new`. With more rounds the controls are a seeded sample of at most 36 splits that always holds those two: 36 of the 200 at three rounds. Each side holds as many runs of each binary, so only what the machine did between runs is left between the sides. Load that falls on some runs and not others puts as much between the sides of some split as between the two binaries: load on one run of `new` and one of `new_again` is seen by the splits that put both on one side, though neither split of whole roles can see it. A figure's noise is the 0.92 quantile, by nearest rank, of its controls' lower bounds of the p95, both ways, and at least 1: the sixth largest of 72 at three rounds, the third largest of 36 at two. Its median's noise is the same quantile of the controls' lower bounds of the median.

A change cancels out of the controls too, unless it is large. When the new binary is much slower, each side's p95 comes from its runs of the new binary alone, so the controls measure its runs against each other, on a quarter of the samples, and say the machine was noisier than it was. So when the binaries' typical runs (the lower median of each binary's runs' medians, which load on half of a binary's runs or fewer leaves on a run the load missed) differ by more than 1.15, the controls first bring the new binary's samples to 1.15 times the old one's, and work out the bounds on those. They bring it to 1.15 and not to 1: a gap between two unchanged binaries larger than that comes from runs that differ a lot, and it should still show in the controls.

A figure is judged only with at least 100 samples on each binary's side. In the gate's runs, and only there, every hook a scenario times fewer than 20 times a run is timed 20 times (the three chat-start figures and `code_brief`, timed 10 times a run in the plain benchmark), so that a binary's five runs still make 100 when one of its six lost the scenario; and one chat start is timed and thrown away before the first chat-start figure's, since a run's first chat start takes about twice as long as the rest and, among 100 samples, would sit at the p95. Those extra chat starts' files are removed before the chat starts with chats a program ran, which a brief reads beyond its 64 newest files: 20 of them, and 20 of those, would push the person's chat further down the 64 than the plain benchmark's 10 ever do. The plain benchmark's report and its sample counts are as they were. A figure with fewer than 100 samples on a side is listed as having too few: it neither fails nor passes. When it is short only because a scenario failed in some runs (the scenario that gives it failed, and each run, given as many samples as its fullest, would have made 100), the gate is not judged, with a line that says to run it again; when it is short by design, it is only listed. Below three rounds the figures timed 20 times a run are short by design (80 samples a side at `--rounds 2`, 40 at `--rounds 1`), so they are never judged there. `gate.json` records the scenario each figure comes from, so a figure the new binary gave no samples is excused only when the new binary lost that figure's own scenario in every run.

A judged figure whose noise is above 1.15 has a p95 too noisy to judge. Two or more such figures say the machine was too noisy, and stop the whole verdict. So does one of a binary's runs being more than 1.10 times as quick as the lower median of its other runs, by the median over the judged figures (those too noisy to judge among them) of each run's median against that figure's typical run: load on all of a binary's runs but one, or on half of them, is what no control can tell from a change, since every split has loaded runs of that binary on each side, but the runs the load missed show in every figure. So does the slower half of a binary's runs being more than 1.10 times as slow as its quicker half, by the same levels, both on their own and against the other binary's runs beside them (each run over the other binary's run of the same number, which ran next to it in its round): a half's run is the slower half's lower median and the quicker half's upper median, the second slowest and the second quickest of six. Load on three of the new binary's runs and none of the old one's, each run taking its own share of it, can leave no run quick while the pooled samples read it as a change; load on a whole round falls on both binaries' runs and cancels out against the runs beside them; and one slow run alone does not count. Only the runs themselves call the machine noisy.

Otherwise each figure is judged on its p95 when its noise is at most 1.15, and on its median when the median's noise is at most 1.15: on both, as a rule, and on the median alone when the p95 is too noisy. A figure fails when the p95's lower bound is above 1.15 times its noise, or the median's lower bound above 1.15 times the median's noise, or when it passes its ceiling: the lower bound of the interval of the new binary's p95 is above the ceiling and the old binary's p95 is not, so a figure at its ceiling by sampling alone does not fail. The ceiling is the target times `--ceiling-factor`: 2 on the CI machine, the default, and 1 on the reference Mac at a release; 0 turns the ceilings off. The ceiling is read only with the p95. The controls have no ceiling: a fixed ceiling says where a binary sits, not how noisy the machine is. A figure one of whose lower bounds is above 1.15 but not above 1.15 times its noise is unsettled; one whose p95 and median are both too noisy is too noisy to judge. Either stops nothing: another figure that failed still fails the gate, and without one the gate is not judged.

A scenario that fails in a run, as when load runs a hook past its time limit, leaves that run's samples of it out and counts against the binary that failed it. A hook still running after 4 seconds is killed with every process in its process group, on Codex's launch path too, where the shell runs the binary as its child; a process the hook puts in a session or group of its own on purpose, as eva-md's detached spawns and its git calls are, is not reached, and may run on after its hook was killed. The new binary failing a scenario in at least three quarters of its runs more than the old one (5 of 6 at three rounds, 3 of 4 at two) fails the gate, as a hook that hangs or fails would. Half of its runs more (3 or 4 of 6) makes the verdict `not judged`, with a line that says to run the gate again: load can lose a scenario that often. Fewer decides nothing. The verdict is the first of these that applies:

1. `could not run`, exit 2: a scenario failed in every run of both binaries, or the gate itself stopped on an error.
2. `fail`, exit 1: the new binary failed a scenario in three quarters of its runs more than the old one, or gave no samples of a figure the old one gave samples of, without losing that figure's scenario in every run.
3. `not judged`, exit 0: two or more judged figures had a p95 too noisy to judge, one run of a binary was more than 1.10 times as quick as the lower median of its other runs, or the slower half of a binary's runs was more than 1.10 times as slow as its quicker half, on their own and against the other binary's runs beside them.
4. `fail`, exit 1: a figure failed, by its p95, its median or its ceiling.
5. `not judged`, exit 0: a figure was too noisy to judge or unsettled, a scenario was lost in half the new binary's runs more, or a figure had too few samples because a scenario failed in some runs.
6. `pass`, exit 0.

A figure the old binary gave no samples, as one added with the change, is listed as not compared. `gate.json` keeps every sample by role and run, unrounded, with the ceilings and the scenario of each figure, and its decision keeps `"rule": 3`, the rule's parameters, the binaries whose runs were quick (`one_quick_run`) or uneven (`uneven_load`), and for each figure the bounds the verdict read: the p95's lower bound and noise, the median's (`median_ratio_low`, `median_noise`), and which of them judged it (`judged_on`). The gate's printed lines give the same. `redecide` in `scripts/bench/core.py` works any `gate.json` out again by this rule: one this rule wrote by the parameters it recorded, so exactly to the verdict it printed, and an earlier rule's by this rule's own. The first rule's `gate.json` kept each role's samples pooled and its ceilings only in its decision: `samples_by_run` splits them into runs by the sample counts its runs' reports give, as `sim_gate.py --rework` does. The step is advisory until the count in "Passes toward blocking" below says it may block; then the lead removes `continue-on-error` from it.

What the card offered, what was tried, and what was kept, each by the tables below, at three rounds, on 100 seeds a case first and then on the full counts:

- **Three rounds.** Kept. Six runs of each binary give the controls 200 splits to sample, and put half the new binary's runs, not three quarters, under a mild load on three of them.
- **The quick run against the median of a binary's other runs**, not its next (a). Kept, with a gap of 1.10. Against its next run, a mild load on three of six runs leaves two of them quick, and is not seen at all. The gap trades the gate not judged on a quiet machine, and a rise of one hook caught, against fails under a mild load on three of the new binary's runs: on the made-up base, at 600 seeds and before the uneven halves below, 1.08 leaves an unchanged binary not judged 11.3 times in 100 and fails it under that load once in 600; 1.09, 7.8 and once; 1.10, 5.7 and twice. At 300 seeds, `session_end` alone 30% slower, the hardest figure, is caught 72.7, 77.3 and 78.7 times in 100, and 50% slower 88.7, 93.7 and 95.0: 1.08 misses the aim of 90.
- **Uneven halves of a binary's runs**, added after review. A reviewer's own simulation gave the mild load on three of the new binary's runs a factor for each run, not one they share, as load that comes and goes would; when the least loaded of the three took little of it, no run was quick, and an unchanged binary failed 13 times in 600 there. `sim_gate.py` now draws that load too (`new-3-mild-each`): without this check it failed an unchanged binary 9 and 7 times in 600 on the two bases, past the aim of 5; with it, 2 and 2, and 3 in 600 in the reviewer's harness. It costs almost nothing elsewhere: on a quiet machine an unchanged binary is not judged 6.0 times in 100 on the made-up base, where it was 5.7, and the saved gates' base, one hook alone and the round's load are judged as before, within half a point. What was tried on the way, at 600 seeds for the loads and 300 for the changes: the halves' means on their own only caught that load as well, but called the round's load uneven too, so an unchanged binary under it was never judged, where it passes 28 and 38.5 times in 100; against the other binary's runs only, an unchanged binary was not judged 8.0 times in 100 on a quiet machine and 20 ms added to capture was caught 93.0 times, under its aim of 95; the halves' means, both ways, counted one run half as slow again as uneven, which the middles of the halves do not; and the middles at a gap of 1.08 caught 20 ms added to capture 94.7 times in 100.
- **A figure judged on its median** (b). Kept, and widened: every figure is judged on its median, not only one whose p95 is too noisy, since a rise of 30% in one hook alone shows in the p95 of 100 to 150 samples a side too seldom (the second rule caught it 36 to 81 times in 100), and in the median, whose controls are far tighter, almost always. The median's threshold is 1.15 too. At 1.10 an unchanged binary failed on a quiet machine, by the median of `session_end`, where its runs differed by chance: once in 100 seeds on each base. When a figure's p95 is too noisy, it still counts toward the two that call the machine too noisy, as in the second rule. Letting the median take such figures over, so that two of them no longer stopped the verdict, failed two of CI's saved gates of unchanged binaries (runs 125 and 140) by the p95 of figures the second rule never reached.
- **Each round compared on its own, a figure's bound the median over the rounds** (c). Dropped: it does not beat the pooled runs where the aims are. With two runs a side each round's bounds are wide: at 100 seeds it caught `session_end` alone 30% slower 60 times in 100 on the made-up base and 82 on the saved gates' base, and `code_brief` 75 on the made-up base (pooled, at 300 seeds: 78.7, 87.3 and 82.3), and it failed an unchanged binary once in 100 on the made-up base, on a quiet machine and again with one round loaded. What it does better is that load on one round: an unchanged binary was not judged 10 times in 100 on the saved gates' base, where the pooled runs leave it not judged 61.5 times in 100. CI's loaded first round is better met by the job's wait (below) than by a rule that catches less.
- **The noise**, the 0.92 quantile (the sixth largest of 72), where the second rule took the 0.89 (the eighth). The eighth failed an unchanged binary by the p95 under a mild load on three of the new binary's runs on the saved gates' base (once in 100 seeds at first), and two of CI's saved gates of unchanged binaries, runs 82 and 85, by the p95 of one figure each (the second rule fails both too); the sixth fails neither, and costs a rise of one hook alone at most 3 in 100 seeds.
- **The samples.** 100 a side to be judged (`WHOLE`), as before, and 20 a run in the gate's runs (`GATE_RUN_SAMPLES`), so that five runs of six make 100: a run that lost a scenario counts for none of that scenario's figures, and the binary's other five still judge them. 20 a run meets every aim below; it times four figures 10 more times a run than the plain benchmark, where 34 timed eight more, so a run of three rounds takes fewer hook calls than one of two did.

### The rule measured

`python3 -I scripts/bench/sim_gate.py` feeds the rule gates with the gate's own sample counts, under a load and with a change to the new binary, 300 seeds a case, and prints the share of fail, pass and not judged. It is run by hand: at three rounds a gate's decision takes about a third of a second on a quiet machine, so a case of 300 seeds takes a couple of minutes, and the tables below, worked out with its functions on many processes at once, took hours. It has two bases. The made-up one has the shapes the proof's samples have (a long right tail, a few slow outliers), and runs that differ on a quiet machine as the saved gates' runs do: by a factor each run shares with every figure (the standard deviation of its logarithm 0.025, the median over the saved gates, and itself differing from gate to gate) and by one of each figure's own (2.6% to 8.2%, the median over the saved gates), which differs from gate to gate (a lognormal factor of standard deviation 0.4), with a share of each run's samples in the tail that differs from run to run, so that its runs' p95s differ about as much as the saved gates' do. The saved base (`--gates`) takes the gate reports it is given, all of an unchanged binary and from one machine. At two rounds it deals one gate's eight runs to the four roles at random. At three it draws two gates, brings each figure of the second to the first's level (its typical run, the lower median of its runs' medians), so that the gap between two gates is not read as load, and deals twelve of their sixteen runs, never one twice. The lead's order named no reports for it, and none are committed, so the tables take, as the second round's did, the 18 of the proof's 20 gates of an unchanged binary that lost no run, all from one Linux machine; their chat-start figures and `code_brief` had 10 samples a run then, 60 a side at three rounds, so they are too few to judge there, and only the other 15 figures are. The loads: none; light bursts (about one a run, a twentieth of a run long, 1.2 to 1.6 times slower); heavy bursts (three a run, a tenth of a run long, 1.5 to 3 times); whole runs (each run loaded or not on its own, a quarter of them, 1.2 to 1.8 times); a drift (the machine half as slow again by the end); one run of `new` and one of `new_again` loaded, 1.2 to 2 times; the four runs of one round, 1.2 to 2 times, as CI's runner loads its first round (below); three of the new binary's runs loaded and none of the old one's, by one factor of 1.2 to 2, or a mild one of 1.1 to 1.3; the mild one on all the new binary's runs but one, or on all of them; and the mild one on three, on all but one or on all, by a factor each loaded run draws on its own (`new-3-mild-each`, `new-but-1-mild-each`, `new-all-mild-each`). The changes: every hook 20%, 30% or 50% slower, or two or three times as slow; 20 ms added to capture; one hook alone 30% or 50% slower, or two or three times as slow; capture slower by a factor each of the new binary's runs draws; every hook 30% slower in all the new binary's runs but one. `--monotonic` counts the seeds in which a larger rise, of every hook or of one hook alone, did not fail where a smaller one did. `--rounds 2` gives the second round's bases. `--common` and `--own-scale` widen the made-up base's runs: its common factor's standard deviation, and how many times its own spread each figure has, as CI's runner's runs differ (below).

For this rule, at three rounds, 300 seeds a case and 600 for an unchanged binary under each load (fail, pass, not judged, with the count and its 95% interval where it is near an aim):

| Load | Change to the new binary | Made-up base | Saved gates' base |
| --- | --- | --- | --- |
| none | none | 0.3% (2 of 600, 0.1% to 1.2%), 93.7%, 6.0% (36 of 600, 4.4% to 8.2%) | 0.2% (1 of 600, 0.0% to 0.9%), 95.7%, 4.2% (25 of 600, 2.8% to 6.1%) |
| none | every hook 20% slower | 95.7%, 0%, 4.3% | 97.3%, 0%, 2.7% |
| none | every hook 30% slower | 96.0%, 0%, 4.0% | 97.7%, 0%, 2.3% |
| none | every hook 50% slower | 96.0%, 0%, 4.0% | 97.7%, 0%, 2.3% |
| none | every hook twice as slow | 96.0%, 0%, 4.0% | 97.7%, 0%, 2.3% |
| none | every hook three times as slow | 96.0%, 0%, 4.0% | 97.7%, 0%, 2.3% |
| none | 20 ms added to capture | 96.0%, 0%, 4.0% | 98.3%, 0%, 1.7% |
| none | capture slower by a factor of 1.2 to 2 that each of the new binary's runs draws | 93.3%, 0%, 6.7% | 94.7%, 0%, 5.3% |
| none | every hook 30% slower in all the new binary's runs but one | 0%, 0%, 100% | 0%, 0%, 100% |
| light bursts | none | 0.2% (1 of 600, 0.0% to 0.9%), 11.7%, 88.2% | 0.2% (1 of 600, 0.0% to 0.9%), 12.0%, 87.8% |
| heavy bursts | none | 0% (0 of 600, 0.0% to 0.6%), 0%, 100% | 0% (0 of 600, 0.0% to 0.6%), 0%, 100% |
| whole runs | none | 0% (0 of 600, 0.0% to 0.6%), 4.3%, 95.7% | 0% (0 of 600, 0.0% to 0.6%), 4.2%, 95.8% |
| a drift | none | 0% (0 of 600, 0.0% to 0.6%), 0%, 100% | 0% (0 of 600, 0.0% to 0.6%), 0%, 100% |
| one run of `new` and one of `new_again` | none | 0% (0 of 600, 0.0% to 0.6%), 0%, 100% | 0% (0 of 600, 0.0% to 0.6%), 0.3%, 99.7% |
| the four runs of one round | none | 0% (0 of 600, 0.0% to 0.6%), 27.8%, 72.2% | 0% (0 of 600, 0.0% to 0.6%), 38.5%, 61.5% |
| three of the new binary's runs, none of the old one's, 1.2 to 2 | none | 0% (0 of 600, 0.0% to 0.6%), 0%, 100% | 0% (0 of 600, 0.0% to 0.6%), 0%, 100% |
| three of the new binary's runs, none of the old one's, 1.1 to 1.3 | none | 0.2% (1 of 600, 0.0% to 0.9%), 6.2%, 93.7% | 0.2% (1 of 600, 0.0% to 0.9%), 5.0%, 94.8% |
| three of the new binary's runs, none of the old one's, 1.1 to 1.3, a factor each | none | 0.3% (2 of 600, 0.1% to 1.2%), 1.5%, 98.2% | 0.3% (2 of 600, 0.1% to 1.2%), 1.2%, 98.5% |
| all the new binary's runs but one, none of the old one's, 1.1 to 1.3 (no aim) | none | 2.3%, 3.7%, 94.0% | 1.7%, 4.0%, 94.3% |
| all the new binary's runs, none of the old one's, 1.1 to 1.3 (no aim) | none | 86.0%, 3.7%, 10.3% | 78.3%, 9.0%, 12.7% |
| all the new binary's runs but one, none of the old one's, 1.1 to 1.3, a factor each (no aim) | none | 3.3%, 0.3%, 96.3% | 1.0%, 0%, 99.0% |
| all the new binary's runs, none of the old one's, 1.1 to 1.3, a factor each (no aim) | none | 60.7%, 0%, 39.3% | 61.0%, 0%, 39.0% |
| light bursts | every hook 30% slower (100 seeds) | 21.0%, 0%, 79.0% | 12.0%, 0%, 88.0% |
| light bursts | 20 ms added to capture (100 seeds) | 15.0%, 0%, 85.0% | 12.0%, 0%, 88.0% |
| whole runs | every hook 30% slower (100 seeds) | 3.0%, 0%, 97.0% | 4.0%, 0%, 96.0% |
| whole runs | 20 ms added to capture (100 seeds) | 4.0%, 0%, 96.0% | 3.0%, 0%, 97.0% |

One hook alone, every other figure level, was measured for every gated figure, at 300 seeds: 30% slower, it failed 78.3 to 95.7 times in 100 on the made-up base (`session_end` 78.3 and `code_brief` 82.0, whose runs differ most, the rest 88.3 or more), and 87.3 to 98.3 on the saved gates' base, for the 15 figures that base judges (`session_end` 87.3, the rest 94.3 or more); 50% slower, 94.7 to 96.0 and 98.0 to 98.3. The second rule caught one hook alone 30% slower 36 to 81 times in 100 at two rounds. On the same base samples a seed, on a quiet machine, at 300 seeds a base, a larger rise never missed where a smaller one failed: for every hook (20%, 30% and 50% slower, two and three times as slow), or for any one hook alone (30% and 50% slower, two and three times as slow), in none of the 300 seeds on either base.

Against the card's aims. An unchanged binary fails at most 5 times in 600 on both bases under every load the card names, the mild load on three of the new binary's runs drawn both ways: on the made-up base 2 times in 600 on a quiet machine (by `session_end` and `capture`), once under light bursts (`capture`), once under the mild load by one factor (`session_end`), twice under it by a factor each (`startup`, `session_end`) and never under the rest; on the saved gates' base once on a quiet machine and once under light bursts (`session_end` both times), once under the mild load by one factor (`prompt_after_brief`), twice under it by a factor each (`session_end` both times, with `code_prompt` and `capture_with_spawn`) and never under the rest. Every interval's upper end is below 1.3 in 100. On a quiet machine it is not judged 36 times in 600 on the made-up base (6.0%, a 95% interval of 4.4% to 8.2%) and 25 times in 600 on the saved gates' base (4.2%, 2.8% to 6.1%): within 15 in 100. Every hook 20% slower fails 95.7 and 97.3 times in 100 (aim: at least 70), 30% slower 96.0 and 97.7 (at least 85), and 20 ms added to capture 96.0 and 98.3 (at least 95). Each gated figure alone 30% slower fails at least 78.3 and 87.3 times in 100 (at least 70), and 50% slower at least 94.7 and 98.0 (at least 90). Every aim is met on both bases. The narrowest are one hook alone 30% slower on the made-up base, `session_end`; 20 ms added to capture on the made-up base, which fails whenever the gate is judged, so 96.0 is 100 less the 4.0 not judged; and the fails of an unchanged binary under the mild load by a factor each, 2 and 2 in 600, each by figures whose runs differed between the binaries by chance as well as by the load.

What the gate cannot tell. Load on all the new binary's runs and none of the old one's looks to every control exactly like a change: a mild load (1.1 to 1.3) there fails 86.0 and 78.3 times in 100, as a change would. On all of them but one, the run the load missed is quick when the load is past 1.10, and the rest fail 2.3 and 1.7 times in 100 (by a factor each run draws, 3.3 and 1.0). By a factor each, on all of them, it fails 60.7 and 61.0 times in 100: the runs it took least of are left uneven against the rest. Runs alternate, so load on one binary's runs alone is not a shape a machine gives, and neither has an aim. A regression in all the new binary's runs but one looks like that load, and leaves a quick run: every hook 30% slower so is never failed, and always not judged. A regression whose cost differs from run to run is now caught by the medians: capture slower by a factor each run draws between 1.2 and 2 failed 93.3 and 94.7 times in 100 (the second rule, 58). Under load the gate is mostly not judged, and a regression under load is too: at 100 seeds, under light bursts a rise of 30% in every hook failed 21 and 12 times in 100 and 20 ms added to capture 15 and 12, and with whole runs loaded 3 and 4, and 4 and 3; the rest were not judged. With the four runs of one round loaded, as CI's runner loads its first, an unchanged binary is not judged 72.2 and 61.5 times in 100, and never fails: the controls that put that round's runs of both binaries on one side see it, in the p95 and in the median. So a `not judged` is not a pass: the gate is to be run again, and one that is not judged run after run on a quiet machine may be a regression of these kinds.

Runs that differ as much as CI's runner's do. On CI's saved gates the factor a run shares with every figure has a standard deviation of its logarithm of about 0.06 to 0.09, and each figure's own about 0.09, where the proof's quiet Linux machine had 0.024 and 0.037 (medians over each machine's gates). On the made-up base with runs that differ so (`--common 0.06 --own-scale 2.4`) and no load at all, the rule fails an unchanged binary 7 times in 600 (1.2%, 0.6% to 2.4%), passes it 9.8 times in 100 and leaves it not judged 89.0: about one verdict in nine that it gives there is a false fail. Two of the 7 were by a figure's median alone, three by its p95 alone and two by both. A reviewer's own harness, with longer tails in each run and runs that differ as much, measured 16 in 600 (2.7%), half of them by a median, for the rule before the uneven halves, and 15 in 600 (2.5%, 1.5% to 4.1%) for this one, 8 of them by a median, against 222 passes: one verdict in sixteen a false fail. The second rule, at two rounds there, failed 14 in 300. A change is seldom judged on such runs either: every hook 20% slower fails 17 times in 100 and `session_end` alone 30% slower 10. Three guards were measured against those 7 fails and none is kept: failing a figure on its median only when its p95's lower bound is above 1 removed none of them; the fourth largest bound, not the sixth, as the median's noise removed one, and as the p95's noise four, but then an unchanged binary with one round loaded passed 11.7 and 24.0 times in 100, not 27.8 and 38.5, and a larger rise of every hook missed where a smaller one failed in 2 seeds of 300. Whether CI's runner, after its wait, gives runs like these is what the count below reads: the gate blocks only after twenty passes there with no fail among the runs that count.

The gate's hook calls, counted, not timed: one of the gate's runs, on a build of this commit, with every call through `run_hook` counted, makes 1,384 hook calls at 34 samples a run (the padded inbox 832, process start 200, the real install's shape 138, the turn of 50 tool calls 214) and 1,288 at 20 (750, 200, 124 and 214). So the second round's gate, eight runs at 34, made 11,072 hook calls, and this one, twelve runs at 20, makes 15,456: 1.40 times as many. Each run also builds its homes, wikis and inboxes, which do not shrink with the samples, so the gate takes up to 1.5 times as long as at two rounds. On CI's runner a run of the second round's gate took 61 to 65 seconds (run 149's log), so its eight runs took about 8.6 minutes, and twelve take about 12 to 13.

The 21 gates of the proof worked out again by this rule from their samples by run (`sim_gate.py --rework`), at the two rounds they ran: the 20 of an unchanged binary pass 17 times and are not judged 3 times, and never fail; the one with 20 ms added to capture fails, on 12 figures. `same-1` and `same-20` had the p95 of two figures too noisy to judge (by the third largest bound of 36, a little higher than the second rule's fourth), and `same-19` and `same-20` lost the scenario of the padded inbox in one run, which left 3 of its figures short of 100 a side at the samples a run they took then, so the gate was not judged, to be run again. One figure each in `same-3`, `same-10`, `same-11` and `same-15` had a p95 too noisy to judge, was judged on its median, and passed; the second rule left `same-3` and `same-10` not judged for it.

CI's saved gates, worked out again. The `latency` artifacts of the 69 runs on `main` from 80 to 149 (all but 119), at their two rounds: 44 of binaries that were alike, by the test of "Passes toward blocking", and 25 of commits that changed files under `cmd/` or `internal/`, few of them files the binary holds. This rule fails none of them, passes 5 (2 of the alike) and leaves 64 not judged: in every one of those, two or more figures' p95 were too noisy, or one run was quick. The second rule fails 2 of the alike, runs 82 and 85, each by the p95 of one figure, and passes 4 (2 alike). The load on CI's runner falls on the gate's first round: by the median over figures, the first run of the gate was on average 1.073 times as slow as a binary's typical run, the rest of the first round 1.04, and the second round 0.99 to 1.01; the slowest of a gate's eight runs was in its first round in 47 of the 69. On a three-round base of these gates (two drawn from the 21 of alike binaries in runs 110 to 146, brought to one level, as above), an unchanged binary was not judged 98 times in 100 at 100 seeds, failed none, and every hook 30% slower was caught 3 times in 100: the runs of one CI gate differ more than a quick run and two noisy p95s allow, by far. What a longer wait would have given, read from the same gates: worked out on their second round alone, as if the job had waited a round longer (about 4.3 minutes there) and then run one round, they give 10 passes, 3 fails (2 of them of alike binaries, runs 96 and 102, on figures one round's runs cannot settle) and 56 not judged. One round is too few runs for the rule; and the second rounds' own runs still differed too much to judge in most gates. So the saved runs show a longer wait taking off the first round's load, but not that it would let the gate judge on this runner: that is read from the runs after the job takes three rounds.

### The second round's rule, measured

Kept as it was measured, by the same simulation at two rounds; where this rule differs is said above.

For the second round's rule (EV-16b), at two rounds and 300 seeds a case, 600 for an unchanged binary on a quiet machine on the made-up base (fail, pass, not judged, with the count where it is near an aim):

| Load | Change to the new binary | Made-up base | Saved gates' base |
| --- | --- | --- | --- |
| none | none | 0.8% (5 of 600), 86.0%, 13.2% (79 of 600) | 0.3% (1 of 300), 85.0%, 14.7% (44 of 300) |
| none | every hook 20% slower | 91.7%, 0.7%, 7.7% | 86.7%, 3.3%, 10.0% |
| none | every hook 30% slower | 94.7%, 0%, 5.3% | 94.0%, 0%, 6.0% |
| none | 20 ms added to capture | 98.0%, 0%, 2.0% | 99.7%, 0%, 0.3% |
| none | the chat start alone half as slow again | 89.0%, 5.7%, 5.3% | not judged there |
| none | the chat start alone twice as slow | 97.3%, 0.3%, 2.3% | not judged there |
| none | one hook alone 30% slower: `session_end`, the worst | 35.7%, 44.7%, 19.7% | |
| none | one hook alone 30% slower: `code_brief` | 46.3%, 39.0%, 14.7% | |
| none | one hook alone 30% slower: `prompt_with_pointers`, the best | 80.7%, 8.0%, 11.3% | |
| none | capture slower by a factor of 1.2 to 2 that each of the new binary's runs draws | 58.0%, 0%, 42.0% | |
| none | every hook 30% slower in three of the new binary's four runs | 0%, 0%, 100% | |
| light bursts | none | 0%, 6.3%, 93.7% | 0%, 4.7%, 95.3% |
| heavy bursts | none | 0%, 0%, 100% | 0%, 0%, 100% |
| whole runs | none | 0.3% (1 of 300), 11.7%, 88.0% | 0%, 9.3%, 90.7% |
| a drift | none | 0%, 0.7%, 99.3% | 0%, 0.3%, 99.7% |
| one run of `new` and one of `new_again` | none | 0%, 0%, 100% | 0.7% (2 of 300), 0.3%, 99.0% |
| three of the new binary's runs, none of the old one's, 1.2 to 2 | none | 0%, 0%, 100% | 0%, 0%, 100% |
| three of the new binary's runs, none of the old one's, 1.1 to 1.3 | none | 4.0% (12 of 300), 4.7%, 91.3% | 2.0% (6 of 300), 3.0%, 95.0% |
| all four of the new binary's runs, none of the old one's | none | 95.7%, 0%, 4.3% | 96.7%, 0%, 3.3% |
| light bursts | every hook 30% slower | 22.3%, 0%, 77.7% | |
| light bursts | 20 ms added to capture | 17.7%, 0%, 82.3% | |
| whole runs | every hook 30% slower | 14.3%, 0.7%, 85.0% | |
| whole runs | 20 ms added to capture | 15.0%, 0%, 85.0% | |

One hook alone 30% slower was measured for every gated figure on the made-up base: it failed 35.7 times in 100 for `session_end`, 46 to 59 for the chat starts, `code_brief`, `code_prompt`, `capture_with_spawn` and `turn_codex_tool`, 62 to 75 for the rest but one, and 80.7 for `prompt_with_pointers`: the figures timed 34 times a run, and the one whose runs differ most, were caught least. A pass says little of a rise of 30% in one hook.

On the same base samples a seed, on a quiet machine, every hook 20%, 30% and 50% slower, and two and three times as slow (`--monotonic`): on the made-up base, at 100 seeds, they failed 90, 92, 93, 93 and 93 times in 100, and in 1 seed of 100 a rise of 20% failed where the larger ones were not judged; on the saved gates' base, at 150 seeds, 87.3, 92.7, 92.7, 92.7 and 92.7 times in 100, and in 2 seeds of 150 the same. Those 2 are what bringing a large change to 1.15, not to 1, leaves: in those gates the rise of 20% left two figures' typical runs about 1.11 times the old ones', within 1.15, so the controls took them as they were, while the larger rises were brought to 1.15, further apart, and those two figures' controls then just passed 1.15. As merged, a hook twice as slow was caught 74 times in 100 where a rise of 20% was caught 87 times, by its reviewer's model.

Against the card's aims, as the lead amended the one for not judged to at most 15 in 100. An unchanged binary failed at most 1 time in 100 under each of the card's loads: on the made-up base 5 times in 600 on a quiet machine (0.8%, a 95% interval of 0.4% to 1.9%), once in 300 with whole runs loaded and never under the other four; on the saved gates' base once in 300 on a quiet machine, twice in 300 with one run of each of the new binary's roles loaded, and never under the rest. It was not judged on a quiet machine 79 times in 600 on the made-up base (13.2%, an interval of 10.7% to 16.1%) and 44 times in 300 on the saved gates' base (14.7%, 11.1% to 19.1%): both within 15, though neither interval is wholly below it. The saved base is in effect 18 gates, and two of them make most of it: dealt every one of the 70 ways (1,260 gates in all), its unchanged gates failed 3 times, passed 1,083 and were not judged 174 times (13.8%), and 139 of the 174 are `same-10` and `same-3`, not judged in 70 and 69 of their 70 deals, each by one figure too noisy to judge (`capture_with_spawn`, `transcript_capture`). A rise of 30% in every hook failed 94.7 and 94.0 times in 100 (aim: at least 85), one of 20% 91.7 and 86.7 (aim: at least 70), and 20 ms added to capture alone 98.0 and 99.7 (aim: at least 95). Every aim is reached on both bases, the amended one by its point estimate only. What leaves an unchanged binary not judged is a figure whose own runs differ by more than 1.15 between splits: on the saved gates `capture_with_spawn` and `transcript_capture`, on the made-up base most often `session_end`. Calling the machine noisy only from two figures made a rise caught far more often, but cannot lower this: a figure too noisy to judge might hide a rise of its own, so with nothing else failing the gate is not judged. A lower quantile barely lowers it (the sixth largest bound gave 12.0 and 14.7 in 100 on the two bases at 150 seeds, where the fourth gave 14.0 on the made-up base on the same seeds), and the largest raises it to 42.0 and 45.3 in 100.

More rounds lower it, at a cost in time: each round is four more runs, so three rounds take 1.5 times as long as two, and four rounds twice as long. On the made-up base, on a quiet machine, an unchanged binary was not judged 57 times in 600 at three rounds (9.5%, 7.4% to 12.1%) and 35 times in 600 at four (5.8%, 4.2% to 8.0%), and failed once in 600 at each; a rise of 20% in every hook failed 93.7 and 95.3 times in 100 (91.7 at two); with whole runs loaded an unchanged binary failed 0 and 1 times in 300, and with one run of each of the new binary's roles loaded 1 and 0 times. So even four rounds do not bring it under 5 in 100 on this base. The saved gates' base could not be taken past two rounds then: each saved gate has eight runs (the third round's base draws two).

What the second rule could not tell. Load on all four of the new binary's runs and none of the old one's looks to every control exactly like a change: it fails 95.7 and 96.7 times in 100, as a change would. Load on three of them is caught by the quick run left when it is heavy: from 1.2 to 2 it never failed in 300 seeds on either base, where without that check the rule failed it 93 times in 100 (on round one's made-up base, at 150 seeds). A mild load on three of them, 1.1 to 1.3, still fails 4.0 and 2.0 times in 100: in the 12 made-up gates it failed, the load was 1.11 to 1.23, the run it missed was, by chance, no more than 1.064 times as quick as its next, and one to three figures' pooled samples were past 1.15 with a noise of about 1. Such load falls on the new binary's runs and skips the old one's between them, which load that comes and goes over minutes rarely does. A regression whose cost differs from run to run makes its own figure's runs differ, so that figure is too noisy to judge: capture slower by a factor each run draws between 1.2 and 2 failed 58 times in 100 and was not judged the rest. A regression in three of the new binary's four runs looks like load on three of them, and is never judged. Under load the gate is mostly not judged, and a regression under load is too: under light bursts a rise of 30% in every hook failed 22.3 times in 100 and 20 ms added to capture 17.7, and with whole runs loaded 14.3 and 15.0, and the rest were not judged. So a `not judged` is not a pass: the gate is to be run again, and one that is not judged run after run on a quiet machine may be a regression of these kinds.

The rule as it was merged first, measured by its reviewers: on the proof's samples an unchanged binary failed 4.7, 9.3 and 5 times in 100 under light bursts, heavy bursts and whole runs loaded, a rise of 20% failed 33 times in 100 and one of 30% 43.5, and two runs of a scenario lost failed the build. The rule of the first round of this card (one noisy figure stopping the verdict, controls of the raw samples, 25 chat starts a run) was measured on a made-up base without the spread from gate to gate: every aim met there. On the saved gates' samples it was not judged on an unchanged binary 16 to 19 times in 100, and caught a rise of 20% 65 times in 100, one of 30% 67 times and 20 ms added to capture 84 to 87 times; and a larger rise was caught less often than a smaller one (a tripled hook not judged where a rise of 20% failed), since the change itself made the controls noisy and one noisy figure stopped the verdict. The second round's rule as first reworked, with a quick run at 1.15, failed an unchanged binary 28 times in 100 under a load of 1.15 to 1.25 on three of the new binary's runs, on a verifier's base fitted to the saved gates (25 times in 100 when run again at 100 seeds); the gap of 1.07 is for that, and on that base it brings those fails to 3.7 in 100, at 300 seeds, while an unchanged binary on a quiet machine is not judged 18.3 times in 100 (15.8 at 600 seeds with the gap at 1.15): above 15 on that base.

The 21 gates of that proof, 20 of an unchanged binary and one with 20 ms added to capture, worked out again by the second rule from their samples by run (`sim_gate.py --rework`): the unchanged ones passed 16 times and were not judged 4 times, and never failed; the one with 20 ms added failed, on 12 figures. Of the 4, `same-3` and `same-10` had one figure too noisy to judge (`transcript_capture`, `capture_with_spawn`) and `same-20` two (`transcript_capture` and `turn_claude_code_tool`); `same-19` and `same-20` each lost the scenario of the padded inbox in one run, which left 3 of its figures (`capture_with_spawn`, `code_prompt` and `session_end`, at 30 and 25 a run then) short of 100 a side, so the gate was not judged, to be run again. Their chat-start figures had 10 samples a run then: 5 figures were listed as having too few samples in each gate, and 8 in `same-19` and `same-20`. None of them had a run more than 1.043 times as quick as its next.

## Passes toward blocking

The gate may block a merge once an unchanged binary has passed it 20 times with no fail among the runs that count, with fewer than one in four of those runs not judged. A run the gate could not judge counts neither way. A run counts when it is the `latency` job's run of a push to `main`, and no file under `cmd/` or `internal/`, nor `go.mod` or `go.sum`, changed between the base its step names ("Comparing BASE with SHA") and its commit (`git diff --quiet BASE SHA -- cmd internal go.mod go.sum`), so that the two binaries it compared are alike. Its verdict is read in the step's log or in its artifact's `gate.json`, never from the run's colour, since the step is advisory. A pass adds one to the count and a `fail` starts it again. A `not judged` does neither, since it blocks nothing, but it goes into the table: a gate that cannot judge one run in four or more is not ready to block. A run of a commit that changed any of them counts neither way. A change to the gate's rule (`scripts/benchmark.py`, `scripts/bench/`) starts the count again. The lead fills the table at each merge.

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

These 18 runs are the first rule's (EV-16), from before the gate's second round: 10 passes, 2 fails and 6 not judged, on binaries that were alike. So the first rule fails an unchanged binary on CI's runner about one time in nine, and the count stands at 0. The runner was busy each time the gate began (a load of 10 to 30 on 3 CPUs, left by the two builds before it), so since 2026-11-11 the job waits, five minutes at most, for the load to fall under the CPUs before it times anything. Worked out again with the second round's rule, the same 23 saved runs (these 18 and 5 of changed binaries) give no fail, no pass and 23 not judged (with its quick run's gap at 1.15, as it was before its last round, 2 passes and 21 not judged; at 1.07 both of those have a run more than 1.07 times as quick as its next): that rule does not fail on noise, and this runner, as busy as it was, gave it nothing it could judge. Whether the wait changes that is read from the runs after it.

The second round's rule (EV-16b) was the gate's from the commit that merged it, and the count started again at 0 with it; its runs are listed here as they came. The third round's rule (EV-16d) is the gate's from the commit that merges it, and the count starts again at 0 with it. The first rule's later runs, 79 to 109, are not listed, since the rule they were judged by is replaced: their reports are kept as those runs' `latency` artifacts, and what the newer rule makes of them is read when the third round (EV-16d) takes its saved base from them.

| Run | Commit | Verdict |
| --- | --- | --- |
| 110 | f62c9ce | not judged |
| 111 | 267a182 | not judged |
| 112 | 509f874 | not judged |
| 113 | 52e6f82 | not judged |
| 114 | 58b73bc | not judged |
| 115 | 07a6da3 | not judged |
| 116 | 3720998 | not judged |
| 117 | 2565fde | not judged |
| 118 | 3639fa6 | not judged |
| 119 | 245fc65 | not judged, and not judged again when the run was started a second time |
| 120 | d734c7e | pass |
| 121 | 7e80307 | not judged |
| 122 | adc9fd2 | not judged |
| 123 | 3a8a396 | not judged |
| 124 | ff52541 | not judged |
| 125 | b7223d6 | not judged |
| 126 | 75ba0be | not judged |
| 127 | 1411db3 | not judged |
| 128 | 6accb73 | not judged |
| 129 | c62f93b | not judged |
| 130 | 0de4141 | not judged |
| 131 | e38176b | not judged |
| 132 | 06f0cf7 | not judged |
| 133 | d63a644 | not judged |
| 134 | 3593796 | not judged |
| 135 | 4faf7c6 | not judged |
| 136 | 33ece1c | not judged |
| 137 | 63dfbfa | not judged |
| 138 | f5c0d69 | not judged |
| 139 | d07758a | pass |
| 140 | f1de822 | not judged |
| 141 | b31a372 | not judged |
| 142 | abae3e3 | not judged |
| 143 | c33523f | not judged |
| 144 | 6612f77 | not judged |
| 145 | fe39012 | not judged |
| 146 | 3b7f116 | not judged |
| 147 | ba6b116 | not judged |
| 148 | e15a80c | not judged |
| 149 | ec9df30 | not judged |
| 150 | 532bf7c | not judged |
| 151 | 816fad7 | not judged |
| 152 | 67333a8 | not judged |
| 153 | c32f821 | not judged |
| 154 | 16b0dc7 | not judged |
| 155 | f789382 | not judged |
| 156 | 1cc6a8b | not judged |
| 157 | 52a642d | not judged |
| 158 | 4230f6b | not judged |
| 159 | f35869c | not judged |

Run 110 is the second rule's first: every figure but one was too noisy to judge. The job had waited its five minutes, and the load was still 8.9 on 3 CPUs, down from 40 when the machine started.

Of these fifty runs the rule judged two: run 120, whose job began timing at a load of 2.9, and run 139, which began at 10.9 and passed. Where the lead read the others (runs 119 to 159), they began at loads of 2.6 to 17 after the same wait and were too noisy to judge. So on CI's runner the second rule seldom gives a verdict. The count stood at 1 when the third rule replaced this one: run 139's push changed documents only, so it counts, and run 120's changed files under `internal/`, though none the binary is built from, so by the rule above it counts neither way. Four of these runs changed what a hook runs: 119 (install and setup ask the host adapter), 121 (the hook's handler list), 126 (redaction can run the current rules alone, behind a switch that is off) and 137 (the words of an identity error, and one line of the command's table). Run 154 changed what the binary holds, though nothing a hook runs (the update's runner can hand what a host printed to the live accuracy run). None of these changes was judged on CI. What stands in for a verdict there is each run's `latency` artifact, which holds the new binary's own figures on macOS, and the gate on a quiet Linux machine, which judged all 19 figures and passed for run 126's change and for run 137's. In the runs the lead read, every figure was inside its target on macOS but in four, each on a busy runner and on a commit that changed no package the binary holds: run 125 (the prompt with pointers, 52.8 ms against 50), runs 129 and 130 (one and two prompt figures at 51 to 54 ms) and run 132 (one figure). The third round (EV-16d) takes up a rule that judges a quiet machine's gates; worked out on CI's 69 saved gates it fails none and judges 5.

The third round's rule is the gate's from run 160, the push that merged it, and its count starts at 0. Its runs are listed here as the lead reads them.

| Run | Commit | Verdict |
| --- | --- | --- |
| 160 | 995bcc2 | not judged |
| 161 | 28516e8 | not judged |
| 162 | ca7105b | not judged |
| 163 | 6164f7e | not judged |
| 164 | d3b7116 | not judged |
| 165 | deaf734 | not judged |
| 166 | 5eb5480 | not judged |
| 167 | fc5a8e2 | not judged |
| 168 | be7e1a9 | not judged |
| 169 | 08f1fb0 | not judged |
| 170 | 68c129c | not judged |
| 171 | c7c31c1 | not judged |
| 172 | ab6faaa | not judged |
| 173 | 812377c | not judged |
| 174 | f96278f | not judged |
| 175 | 4724d99 | not judged |
| 176 | d0d31f9 | not judged |
| 177 | 7ed952b | not judged |
| 178 | 443f76d | not judged |
| 179 | b286f00 | not judged |
| 180 | 5d7b1be | pass |
| 181 | ee5feb8 | not judged |
| 182 | 02aed0d | not judged |
| 183 | c2ca020 | not judged |
| 184 | 9e06c2e | not judged |
| 185 | 5447fa1 | not judged |
| 186 | 234de46 | pass |
| 187 | 8125166 | not judged |
| 188 | 1c17786 | not judged |
| 189 | ae62e22 | not judged |
| 190 | 03d6553 | not judged |
| 191 | b27a172 | not judged |
| 192 | dca581a | not judged |
| 193 | b8924db | not judged |
| 194 | 3319eef | not judged |
| 195 | cb8e9b2 | not judged |
| 196 | 47a8460 | pass |
| 197 | 7f6e71b | not judged |
| 198 | a861d45 | not judged |
| 199 | 6037fe4 | pass |
| 200 | ca15703 | not judged |
| 201 | a9dc7bd | not judged |
| 202 | 4adfb87 | not judged |
| 203 | b5537ba | not judged |
| 204 | 4a93540 | not judged |
| 205 | 8b1361e | pass |
| 206 | 98c62fe | not judged |
| 207 | 9462f65 | not judged |
| 208 | 75779a3 | not judged |
| 209 | b57d4dc | not judged |
| 210 | a360c1e | not judged |
| 211 | 244dcda | not judged |
| 212 | 4adf3c7 | not judged |
| 213 | 927da7a | pass |
| 214 | 5c404e7 | not judged |
| 215 | 1935c86 | not judged |
| 216 | 3297a24 | not judged |
| 217 | 551c7da | not judged |

Of these fifty-eight runs the rule judged six. Run 180 began timing at a load of 8.0 and passed, with one figure too noisy to judge; its push changed files under `cmd/` and `internal/`, all of them the evaluation's, which the binary does not hold, so by the rule above it counts neither way. Run 186 began at a load of 3.0 and passed; its push changed a workflow and a document only, so it counts, and the count stood at 1. Run 196 began at a load of 9.0 and passed; its push added a package under `internal/` that the binary does not hold, so it counts neither way. Run 199 began at 11.8 and passed; its push changed documents only, against a base whose own push had changed the benchmark's scripts and no file the rule names, so it counts, and the count stands at 2. Run 205 began at 5.5 and passed; its push changed `internal/health` (HO-12a, whose code runs in a hook) and the release workflow, so it counts neither way, and it is that package's latency verdict. Run 213 began at 10.7 and passed; its push added a test file under `internal/`, so it counts neither way. The other fifty-two were too noisy to judge; those from run 191 on began at loads of 2.9 to 16.8. Three of them were the runs of a merge that changed what a hook runs, each behind a switch that is off: run 212 (DV-12b, load 5.0), run 214 (HO-12b, load 3.0) and run 216 (DV-13, load 12.8); what stands in for their verdict is the gate on the build machine, as each card's "As merged" says. The test jobs of runs 175 and 182 to 184 were cancelled by their own time limits, and those of runs 185 to 187 failed on go test's ten minutes for a package, which says nothing of the gate: its job ran and gave its artifact each time. The test jobs of runs 200 and 202 failed in the driver's tests (a fake host that gave no version), which says nothing of the gate either.

## What the counters cost

The counters release adds work to every hook: a record of what the run held back and delivered, a week of counts in the heartbeat, and a larger binary. Its cost was measured once on a quiet Linux machine (4 CPUs, a load of 3 falling to 2), the commit before any counters part against `main` with all four, in alternating runs, 400 samples a side for the common hooks. Medians, in milliseconds:

| Hook | Before | After |
| --- | --- | --- |
| The process's start, Claude Code's launch path | 10.6 | 11.3 |
| The process's start, Codex's launch path | 11.8 | 12.9 |
| Capture of a reply | 15.3 | 16.4 |
| Capture with a transcript to recover | 16.7 | 16.6 |
| A prompt after a brief | 16.9 | 16.4 |
| A prompt with pointers | 22.7 | 22.2 |
| A prompt with recall | 29.8 | 28.7 |
| A tool call, Claude Code | 14.5 | 14.8 |
| A tool call, Codex | 15.4 | 15.8 |
| A chat's start | 63.4 | 64.1 |
| A chat's start on the real install's shape | 98.2 | 99.8 |

So a hook costs about one millisecond more, most of it at the process's start, and nothing was seen in the slow tail: no sample on either side passed 250 ms, and the slowest after was 201 ms, a chat's start with 1.5 seconds to spend. The gate itself could not judge that run, nor CI's runs on the commits that merged the parts: its 95th percentiles moved both ways by more than this between runs of the same binary. The medians are steadier, which the gate's next rule should use.
