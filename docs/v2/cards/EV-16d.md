# EV-16d: the latency gate's rule, third round

**Plan:** section 6.6 (latency), section 8 Stage 0 row EV-16 ("a build that slows a hook fails"), section 9 ("One timed check is also a merge gate"), decision 27. **Follows:** [EV-16b](EV-16b.md), whose rule `docs/v2/baselines/README.md` holds. **Size:** M. **Reviewers:** two, one measuring the rule by simulation with their own harness. **Depends on:** EV-16b and [EV-16c](EV-16c.md) merged: the three change the same files, so they go in that order.

## Why

The second round's rule cannot yet block a merge. In its simulation a mild load (1.1 to 1.3 times) on three of the new binary's four runs fails an unchanged binary 2 to 4 times in 100, and one hook alone 30% slower is caught 36 to 81 times in 100. The lead ruled what a gate that blocks must show, which is the aims under "Accepted when", and where to start: three rounds, a quick-run check against the median of a binary's other runs, and a figure judged on its median when its 95th percentile cannot be judged. No Go file changes, nor anything v1 does: no switch.

## What to build

1. **Three rounds.** `ROUNDS` in `scripts/bench/core.py` is 3, and `scripts/benchmark.py --rounds` defaults to it (today a literal 2, read before core loads): each binary runs six times. The plain benchmark's report, keys, order and sample counts do not change.
2. **A rule that meets the aims, found by measuring.** Start from the second round's rule at three rounds and try these in order, keeping each only where item 5's tables show it pays: (a) the quick-run check compares a binary's quickest run with the median of its other runs, not with its next (at two rounds this brought the mild-load false fails to 1 in 100 at a cost in runs not judged, which three rounds give room for); (b) a figure whose 95th percentile is too noisy to judge is judged on its median, with a bound of its own, where today it leaves the gate not judged; (c) each round compared on its own and a figure's bound taken as the median over the rounds, only if it beats the pooled runs, since two runs a side gave the second round's first fixer intervals too wide to use. The noise, the samples a figure needs (`WHOLE`) and a gate run takes (`GATE_RUN_SAMPLES`), and what a round that lost a run counts for, are the builder's, chosen by the tables; the README says each and why, and what was tried and dropped. `decide` still takes any rounds from 1.
3. **One hook 30% slower fails**, every other figure level, whichever gated figure it is.
4. **The record.** `decide` returns, and `gate.json`'s decision keeps, `"rule": 3` and, for each figure, the bounds the verdict read (the median's too, where it was judged on it), which the printed lines give too. `redecide` works any gate.json out by this rule, and one this rule wrote to its printed verdict exactly.
5. **The simulation.** `scripts/bench/sim_gate.py` gains three rounds on both bases; load on one whole round, and mild load (1.1 to 1.3) on three of the new binary's runs, on all but one and on all; each gated figure alone 30% and 50% slower, and two and three times as slow. Its saved base takes the gate reports the lead's order names (none committed): a three-round gate takes twelve runs of two saved gates of an unchanged binary from the same machine, never one run twice, each gate first brought to one level so the gap between gates is not read as load. The two-round base stays.
6. **The README.** "The gate's rule" and "The rule measured" state the rule as built, its tables at three rounds beside the second round's, what it cannot tell, and the gate's hook calls at two and three rounds, counted, not timed. In "Passes toward blocking" only the sentence naming the rule the count is for changes: the count starts again; the table is the lead's.
7. **Words for the lead**, in the report: the comment above the `latency` job in `.github/workflows/ci.yml` (EV-11's while it builds: three rounds, the README's rule for blocking), its step's and job's `timeout-minutes` scaled by item 6's counts, and `docs/verification.md`'s two sentences on the gate ("twenty times in twenty", "each twice a round").

## Owns and may touch

In `core.py`, the rule as EV-16b left it: the comment above `ROLES`, its constants, `schedule`, `splits`, `aligned`, `run_levels`, `one_quick_run`, `control_bounds`, `decide`, `redecide`, `GATE_RUN_SAMPLES` and new functions of the rule. In `benchmark.py`, gate mode only: `compare` and `--rounds`. `test_gate.py`: an existing case changes only where it takes two rounds as the default or pins a part of the second rule this card replaces, each listed in the report with what now checks it. `sim_gate.py`. The README's two rule sections and item 6's sentence.

EV-16c adds one function to `core.py` and changes `padded_inbox`'s checks of started jobs; neither card touches what the other changes, and EV-16c merges first; each adds its own classes to `test_gate.py`.

```paths
scripts/bench/core.py
scripts/benchmark.py
scripts/bench/test_gate.py
scripts/bench/sim_gate.py
docs/v2/baselines/README.md
```

## Must not touch

Any Go file. `.github/workflows/**`, `scripts/linux-check.sh`, `scripts/linux-gate.sh`. Every scenario, `Run` but `loops`, `Home`, `run_hook`. `docs/v2/baselines/latency-macos.json`. `docs/verification.md`, `README.md`, `AGENTS.md`.

## Accepted when

- `scripts/linux-check.sh` passes, and the report says how long `python3 -I scripts/bench/test_gate.py` takes.
- Tests on made-up numbers, each able to fail: `--rounds` as parsed defaults to `ROUNDS`, which is 3; every figure 1.3 times slower in both new runs of one round does not fail, and in every new run fails; load of 1.2 on both new runs of one round and one of another does not fail; one figure alone 1.3 times slower fails; a gate on stub binaries at the default rounds keeps six runs a binary and `"rule": 3`, and `redecide` equals its printed verdict.
- By the simulation at three rounds, 300 seeds a case and 600 where the aim is 1 in 100. An unchanged binary fails at most 5 times in 600, on both bases, under no load, light and heavy bursts, whole runs, a drift, one run of each new role, one whole round, heavy load on three of the new binary's runs, and mild load on three of them. On a quiet machine: an unchanged binary is not judged at most 15 times in 100 on both bases; every hook 20% slower fails at least 70 times in 100, 30% at least 85, 20 ms added to capture at least 95; each gated figure alone 30% slower at least 70, and 50% slower at least 90 (on the saved base, those it judges); a larger rise, of one hook or every hook, misses where a smaller one failed in at most 1 seed in 100. Mild load on all the new binary's runs, and on all but one, is reported with no aim: runs alternate, so load on one binary's runs alone is not a shape a machine gives, and on all of them it cannot be told from a change. The report gives the whole table, with counts and intervals near an aim, and what is missed and why.
- The saved gates worked out again by this rule: the unchanged ones never fail; the proof's with 20 ms added to capture, where at hand, fails.
- A reviewer's mutations of the rule's functions and of `compare` that change a verdict are each noticed by a test.
