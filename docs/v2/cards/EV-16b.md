# EV-16b: the latency gate's rule, second round

**Plan:** section 6.6 (the latency instrument), section 8 Stage 0 row EV-16, section 9 ("One timed check is also a merge gate"). **Follows:** [EV-16](EV-16.md), merged, whose gate is advisory in CI. **Size:** M. **Reviewers:** two, one of whom measures the rule by simulation with a harness of their own. **Depends on:** nothing.

## Why

Two reviewers measured the merged gate on the samples of its own 21 proof runs. It never fails a quiet, unchanged binary, and it catches 20 ms added to capture. But on a quiet machine a rise of 20% in every hook is called "not judged" two times in three, because a figure whose rise is just inside its own noise stops the whole verdict; load that falls on one run of each of the new binary's two roles fails an unchanged binary 5 to 10 times in 100, because both controls pair whole roles; and two runs of a scenario lost to load fail the build. Every later package that touches a hook is judged by this rule, so it is put right before the gate is made blocking.

## What to build

No Go file changes. The rulings:

1. **Noise from runs, not roles.** `decide` takes each role's samples by run. A control is a split of the runs into two sides that each hold half of the old binary's runs and half of the new one's: 18 splits at the default two rounds, the two role splits among them. With more rounds, take a seeded sample of at most 36 splits that always holds the two role splits. A figure's noise comes from its controls' lower bounds, both ways, and is at least 1. Choose the statistic (the largest, or a high quantile) by the table of item 8, and say why in the README.
2. **Only the controls call the machine noisy.** A figure stops the whole verdict only when its noise is above 1.15 and it has at least 100 samples a side. A figure whose rise is above 1.15 but inside 1.15 times a noise of 1.15 or less is itself unsettled and stops nothing: another figure that failed still fails the gate.
3. **Enough samples for every gated figure.** In gate mode each gated figure collects at least 100 samples a side at the default rounds; raise the loops of the chat-start scenarios in gate mode only. A figure with fewer is listed as having too few samples: it neither fails the build nor stops the verdict. The plain benchmark's report and its sample counts do not change.
4. **A scenario lost to load is not a regression.** The new binary failing a scenario in at least three quarters of its runs more than the old one (3 of 4 at two rounds) fails the gate. A difference of half its runs (2 of 4) makes the verdict "not judged", with a line that says to run it again. Less decides nothing.
5. **The ceiling by an interval.** A figure passes its ceiling only when the lower bound of the interval of the new binary's p95 is above the ceiling and the old binary's p95 is not.
6. **A faster, exact bootstrap.** The p95 of a resample is an order statistic of the sorted samples, and its rank can be drawn directly (a Beta draw), so no resample is built. Keep the fixed seed, and the same bound for the same samples in any order. `python3 -I scripts/bench/test_gate.py` must take seconds, not a minute.
7. **Plumbing.** A hook that passes its time limit is killed with its whole process group, on Codex's launch path too, where the shell does not replace itself with the binary. `gate.json` keeps the samples by run and unrounded, so a verdict can be worked out again exactly. The comment above the roles and `docs/v2/baselines/README.md` state the rule as it now is.
8. **A simulation kept in the repository**, `scripts/bench/sim_gate.py`, run by hand with `python3 -I`: it feeds `decide` made-up samples with the shapes real hooks have (a long right tail, a few slow outliers, the gate's real sample counts) or the samples of a saved `gate.json`, under no load, light bursts, heavy bursts, whole runs loaded and a drift, and prints for each case the share of fail, pass and not judged. It is not a test: it takes minutes. The README holds its table for the rule as merged.

The earlier reviewers' harnesses and the 21 saved gates are on this machine, under the session's scratchpad in `ev16v/rule/`, `ev16v/plumbing/` and `ev16fix/gates/`; read them, and write your own.

## Owns and may touch

`scripts/bench/core.py`, `scripts/bench/test_gate.py`, `scripts/bench/sim_gate.py` (new), `scripts/benchmark.py` (gate mode only), the chat-start scenarios' loop counts in gate mode, `docs/v2/baselines/README.md`.

## Must not touch

Any Go file. `.github/workflows/**`. `scripts/linux-check.sh` and `scripts/linux-gate.sh`. The plain benchmark's report: the same keys, in the same order, with the same sample counts.

## Accepted when

- `scripts/linux-check.sh` passes, and `python3 -I scripts/bench/test_gate.py` passes in under 15 seconds on a quiet machine.
- By the simulation, at least 300 seeds a case, on samples with the gate's real counts: an unchanged binary fails at most 1 time in 100 under each load shape, with load on one run of each of the new binary's roles among them, and is "not judged" at most 5 times in 100 on a quiet machine; on a quiet machine a rise of 30% in every hook fails at least 85 times in 100, a rise of 20% at least 70, and 20 ms added to capture at least 95. The report gives the whole table, and says plainly where a figure is not reached and what was traded for what.
- The 21 saved gates, worked out again by the new rule from their samples by run: the unchanged ones never fail, and the one with 20 ms added to capture fails.
- Tests on made-up numbers, each able to fail, for: load on runs 2 and 4 only does not fail; a rise of 30% with a noise of about 1.04 fails though another figure is unsettled; two runs of a scenario lost on the new binary is "not judged" and three is "fail"; a figure at its ceiling by sampling alone does not fail; a figure with too few samples neither fails nor stops the verdict; a timed-out hook's process group is gone; a verdict worked out again from `gate.json` equals the one printed.
- A reviewer's own mutations of `core.py` and of the gate's part of `benchmark.py` that change a verdict are each noticed by a test.
