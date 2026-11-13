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

Each round runs every scenario on each binary twice, as four roles: `old`, `new`, `old_again` and `new_again`, old and new alternating, and every other round in reverse (`--rounds`, 2 by default, so eight runs, four of each binary). The gate keeps each role's samples by run. All the old binary's runs together are compared with all the new one's, by the lower bound of the bootstrap interval (95%, 2,000 draws, a fixed seed) of the ratio of the new binary's p95 to the old one's. The p95 of a resample is one of the sorted samples, and its rank is drawn directly, from the law of the 95th percentile of that many uniform draws, so no resample is built: the bound is the one resampling gives, the same for the same samples in any order, and quick to work out.

The controls are splits of the runs into two sides that each hold half of the old binary's runs and half of the new one's: 18 at two rounds, among them the two splits of whole roles, `old`+`new` against `old_again`+`new_again` and `old`+`new_again` against `old_again`+`new`. With more rounds the controls are a seeded sample of at most 36 splits that always holds those two. Each side holds as many runs of each binary, so only what the machine did between runs is left between the sides. Load that falls on some runs and not others puts as much between the sides of some split as between the two binaries: load on one run of `new` and one of `new_again` is seen by the 6 splits that put both on one side, though neither split of whole roles can see it. A figure's noise is the 0.89 quantile, by nearest rank, of its controls' lower bounds, both ways, and at least 1: the fourth largest of 36 at two rounds, the eighth largest of 72 at three.

A change cancels out of the controls too, unless it is large. When the new binary is much slower, each side's p95 comes from its two runs of the new binary alone, so the controls measure two of its runs against two, on a quarter of the samples, and say the machine was noisier than it was: as merged, the gate caught a tripled hook less often than a rise of 20%. So when the binaries' typical runs (the lower median of each binary's runs' medians, which load on half of a binary's runs or fewer leaves on a run the load missed) differ by more than 1.15, the controls first bring the new binary's samples to 1.15 times the old one's, and work out the bounds on those. They bring it to 1.15 and not to 1: a gap between two unchanged binaries larger than that comes from runs that differ a lot, and it should still show in the controls.

Why the fourth largest bound: the largest of 36 is high whenever any one split is, and with it an unchanged binary was not judged 42 and 45 times in 100 on a quiet machine on the two bases of the simulation below; the fourth still sees load on one run of each of the new binary's roles, whose 6 splits are well above it.

A figure is judged only with at least 100 samples on each binary's side. In the gate's runs, and only there, every hook a scenario times fewer than 34 times a run is timed 34 times (the chat-start figures, `code_brief`, `code_prompt`, `capture_with_spawn`, `session_end` and `real_install_startup`), so that a binary's three runs still make 100 when one of its four lost the scenario; and one chat start is timed and thrown away before the first chat-start figure's, since a run's first chat start takes about twice as long as the rest and, among 100 samples, would sit at the p95. Those extra chat starts' files are removed before the chat starts with chats a program ran, which a brief reads beyond its 64 newest files: 34 of them, and 34 of those, would push the person's chat out of the 64, as the plain benchmark's 10 never do. The plain benchmark's report and its sample counts are as they were. A figure with fewer than 100 samples on a side is listed as having too few: it neither fails nor passes. When it is short only because a scenario failed in some runs (the scenario that gives it failed, and each run, given as many samples as its fullest, would have made 100), the gate is not judged, with a line that says to run it again; when it is short by design, as at `--rounds 1`, it is only listed. `gate.json` records the scenario each figure comes from, so a figure the new binary gave no samples is excused only when the new binary lost that figure's own scenario in every run.

A judged figure whose noise is above 1.15 is too noisy to judge: it cannot fail. Two or more such figures say the machine was too noisy, and stop the whole verdict. So does one of a binary's runs being more than 1.07 times as quick as its next, by the median over the judged figures (those too noisy to judge among them) of each run's median against that figure's typical run: load on all of a binary's runs but one is what no control can tell from a change, since every split has a loaded run of that binary on each side, but the one run the load missed shows in every figure. The gap is 1.07, not 1.15: with load of a little over 1.15 on three runs, the run it missed, slower than its fellows by chance, is often less than 1.15 times as quick as the next, while the pooled samples put figures past 1.15; on a quiet machine the 20 unchanged gates of the proof never had a run more than 1.043 times as quick as its next. Only the runs themselves call the machine noisy. Otherwise a figure fails when its lower bound is above 1.15 times its noise, or when it passes its ceiling: the lower bound of the interval of the new binary's p95 is above the ceiling and the old binary's p95 is not, so a figure at its ceiling by sampling alone does not fail. The ceiling is the target times `--ceiling-factor`: 2 on the CI machine, the default, and 1 on the reference Mac at a release; 0 turns the ceilings off. The controls have no ceiling: a fixed ceiling says where a binary sits, not how noisy the machine is. A figure whose lower bound is above 1.15 but not above 1.15 times its noise is unsettled. A figure too noisy to judge, or unsettled, stops nothing: another figure that failed still fails the gate, and without one the gate is not judged.

A scenario that fails in a run, as when load runs a hook past its time limit, leaves that run's samples of it out and counts against the binary that failed it. A hook still running after 4 seconds is killed with every process in its process group, on Codex's launch path too, where the shell runs the binary as its child; a process the hook puts in a session or group of its own on purpose, as eva-md's detached spawns and its git calls are, is not reached, and may run on after its hook was killed. The new binary failing a scenario in at least three quarters of its runs more than the old one (3 of 4 at two rounds) fails the gate, as a hook that hangs or fails would. Half of its runs more (2 of 4) makes the verdict `not judged`, with a line that says to run the gate again: load can lose a scenario that often. Fewer decides nothing. The verdict is the first of these that applies:

1. `could not run`, exit 2: a scenario failed in every run of both binaries, or the gate itself stopped on an error.
2. `fail`, exit 1: the new binary failed a scenario in three quarters of its runs more than the old one, or gave no samples of a figure the old one gave samples of, without losing that figure's scenario in every run.
3. `not judged`, exit 0: two or more judged figures were too noisy to judge, or one run of a binary was more than 1.07 times as quick as its next.
4. `fail`, exit 1: a figure failed.
5. `not judged`, exit 0: a figure was too noisy to judge or unsettled, a scenario was lost in half the new binary's runs more, or a figure had too few samples because a scenario failed in some runs.
6. `pass`, exit 0.

A figure the old binary gave no samples, as one added with the change, is listed as not compared. `gate.json` keeps every sample by role and run, unrounded, with the ceilings and the scenario of each figure, so that `redecide` in `scripts/bench/core.py` works the verdict out again exactly. The step is advisory until the count in "Passes toward blocking" below says it may block; then the lead removes `continue-on-error` from it.

This rule differs from the card's rulings in four places, each for a reviewer's or a verifier's measurement. A single figure too noisy to judge no longer stops the whole verdict, as ruling 2 had it: two do, or the quick run above, with a gap of 1.07 of its own (on the saved gates' samples one noisy figure stopped about one gate in six, and with it a real rise was caught too seldom). The controls bring a large change to 1.15 first, which ruling 1 did not say. A figure short of 100 only because a scenario failed in some runs leaves the gate not judged, where ruling 3 had it stop nothing. And the gate's runs time `code_prompt`, `capture_with_spawn` and `session_end` 34 times a run too, not only the chat starts ruling 3 named.

### The rule measured

`python3 -I scripts/bench/sim_gate.py` feeds the rule gates with the gate's own sample counts, under a load and with a change to the new binary, 300 seeds a case, and prints the share of fail, pass and not judged. It takes minutes, and is run by hand. It has two bases. The made-up one has the shapes the proof's samples have (a long right tail, a few slow outliers), and runs that differ on a quiet machine as the saved gates' runs do: by a factor each run shares with every figure (the standard deviation of its logarithm 0.025, the median over the saved gates, and itself differing from gate to gate) and by one of each figure's own (2.6% to 8.2%, the median over the saved gates), which differs from gate to gate (a lognormal factor of standard deviation 0.4), with a share of each run's samples in the tail that differs from run to run, so that its runs' p95s differ about as much as the saved gates' do. The saved base (`--gates`) takes the 18 of the proof's 20 gates of an unchanged binary that lost no run, deals one gate's eight runs to the four roles at random, and puts the load and the change on them; its chat-start figures had 10 samples a run then, so they are too few to judge there, and only the other figures are. The loads: none; light bursts (about one a run, a twentieth of a run long, 1.2 to 1.6 times slower); heavy bursts (three a run, a tenth of a run long, 1.5 to 3 times); whole runs (each run loaded or not on its own, a quarter of them, 1.2 to 1.8 times); a drift (the machine half as slow again by the end); one run of `new` and one of `new_again` loaded, 1.2 to 2 times; and three or all four of the new binary's runs loaded and none of the old one's, by one factor of 1.2 to 2, or, for three, a mild one of 1.1 to 1.3. `--rounds` takes the made-up base to more rounds.

For the rule as it now is, at two rounds and 300 seeds a case, 600 for an unchanged binary on a quiet machine on the made-up base (fail, pass, not judged, with the count where it is near an aim):

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

More rounds lower it, at a cost in time: each round is four more runs, so three rounds take 1.5 times as long as two, and four rounds twice as long. On the made-up base, on a quiet machine, an unchanged binary was not judged 57 times in 600 at three rounds (9.5%, 7.4% to 12.1%) and 35 times in 600 at four (5.8%, 4.2% to 8.0%), and failed once in 600 at each; a rise of 20% in every hook failed 93.7 and 95.3 times in 100 (91.7 at two); with whole runs loaded an unchanged binary failed 0 and 1 times in 300, and with one run of each of the new binary's roles loaded 1 and 0 times. So even four rounds do not bring it under 5 in 100 on this base. The saved gates' base cannot be taken past two rounds: each saved gate has eight runs.

What the gate cannot tell. Load on all four of the new binary's runs and none of the old one's looks to every control exactly like a change: it fails 95.7 and 96.7 times in 100, as a change would. Load on three of them is caught by the quick run left when it is heavy: from 1.2 to 2 it never failed in 300 seeds on either base, where without that check the rule failed it 93 times in 100 (on round one's made-up base, at 150 seeds). A mild load on three of them, 1.1 to 1.3, still fails 4.0 and 2.0 times in 100: in the 12 made-up gates it failed, the load was 1.11 to 1.23, the run it missed was, by chance, no more than 1.064 times as quick as its next, and one to three figures' pooled samples were past 1.15 with a noise of about 1. Such load falls on the new binary's runs and skips the old one's between them, which load that comes and goes over minutes rarely does. A regression whose cost differs from run to run makes its own figure's runs differ, so that figure is too noisy to judge: capture slower by a factor each run draws between 1.2 and 2 failed 58 times in 100 and was not judged the rest. A regression in three of the new binary's four runs looks like load on three of them, and is never judged. Under load the gate is mostly not judged, and a regression under load is too: under light bursts a rise of 30% in every hook failed 22.3 times in 100 and 20 ms added to capture 17.7, and with whole runs loaded 14.3 and 15.0, and the rest were not judged. So a `not judged` is not a pass: the gate is to be run again, and one that is not judged run after run on a quiet machine may be a regression of these kinds.

The rule as it was merged first, measured by its reviewers: on the proof's samples an unchanged binary failed 4.7, 9.3 and 5 times in 100 under light bursts, heavy bursts and whole runs loaded, a rise of 20% failed 33 times in 100 and one of 30% 43.5, and two runs of a scenario lost failed the build. The rule of the first round of this card (one noisy figure stopping the verdict, controls of the raw samples, 25 chat starts a run) was measured on a made-up base without the spread from gate to gate: every aim met there. On the saved gates' samples it was not judged on an unchanged binary 16 to 19 times in 100, and caught a rise of 20% 65 times in 100, one of 30% 67 times and 20 ms added to capture 84 to 87 times; and a larger rise was caught less often than a smaller one (a tripled hook not judged where a rise of 20% failed), since the change itself made the controls noisy and one noisy figure stopped the verdict. This round's rule as first reworked, with a quick run at 1.15, failed an unchanged binary 28 times in 100 under a load of 1.15 to 1.25 on three of the new binary's runs, on a verifier's base fitted to the saved gates (25 times in 100 when run again at 100 seeds); the gap of 1.07 is for that, and on that base it brings those fails to 3.7 in 100, at 300 seeds, while an unchanged binary on a quiet machine is not judged 18.3 times in 100 (15.8 at 600 seeds with the gap at 1.15): above 15 on that base.

The 21 gates of that proof, 20 of an unchanged binary and one with 20 ms added to capture, worked out again by this rule from their samples by run (`sim_gate.py --rework`): the unchanged ones passed 16 times and were not judged 4 times, and never failed; the one with 20 ms added failed, on 12 figures. Of the 4, `same-3` and `same-10` had one figure too noisy to judge (`transcript_capture`, `capture_with_spawn`) and `same-20` two (`transcript_capture` and `turn_claude_code_tool`); `same-19` and `same-20` each lost the scenario of the padded inbox in one run, which left 3 of its figures (`capture_with_spawn`, `code_prompt` and `session_end`, at 30 and 25 a run then) short of 100 a side, so the gate was not judged, to be run again. Their chat-start figures had 10 samples a run then: 5 figures were listed as having too few samples in each gate, and 8 in `same-19` and `same-20`. None of them had a run more than 1.043 times as quick as its next.

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

The second round's rule (EV-16b) is the gate's from the commit that merged it, and the count starts again at 0 with it; its runs are listed here as they come. The first rule's later runs, 79 to 109, are not listed, since the rule they were judged by is replaced: their reports are kept as those runs' `latency` artifacts, and what the newer rule makes of them is read when the third round (EV-16d) takes its saved base from them.

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

Run 110 is the second rule's first: every figure but one was too noisy to judge. The job had waited its five minutes, and the load was still 8.9 on 3 CPUs, down from 40 when the machine started.

Of these twenty-two runs the rule judged one, run 120, whose job began timing at a load of 2.9. Where the lead read the others (runs 119 to 131), most began at loads of 4.6 to 17 after the same wait, and runs 128 and 129 began at 2.6 and were still too noisy to judge. So on CI's runner the second rule seldom gives a verdict, and the count stands at 0: run 120's push changed files under `internal/`, though none the binary is built from, so by the rule above it counts neither way. Three of these runs changed what a hook runs: 119 (install and setup ask the host adapter), 121 (the hook's handler list) and 126 (redaction can run the current rules alone, behind a switch that is off). None was judged. What stands in for a verdict there is each run's `latency` artifact, which holds the new binary's own figures on macOS: every figure was inside its target in runs 119 to 124, and the ratios of new to old lay on both sides of 1. In run 125 one figure was outside its target: the prompt with pointers read 52.8 ms against 50, at a load of 13.8, on a commit that changed no package the binary holds, where the run before had read 35.0 ms. In runs 126 and 127 every figure was inside its target again, and for run 126's change the gate on a quiet Linux machine judged all 19 figures and passed. Runs 128 to 131 were of commits that changed documents only: in runs 129 and 130 one and two prompt figures read 51 to 54 ms against 50, as in run 125, and in runs 128 and 131 none did. The third round (EV-16d) takes up a rule that can judge on this runner.

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
