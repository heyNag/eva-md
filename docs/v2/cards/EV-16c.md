# EV-16c: the benchmark's count of background starts allows for a hook that missed its limit

**Plan:** section 6.6 (the latency instrument), section 9 ("Tests never depend on machine speed" applies to the checks CI fails on). **Follows:** [EV-16b](EV-16b.md). **Size:** S. **Reviewers:** one. **Depends on:** EV-16b merged, since both change `scripts/benchmark.py`.

## Why

CI's Linux job failed once on a commit that changed only a card: the plain benchmark's padded-inbox scenario saw the hooks start the stand-in 61 times, not 62. v1's hook is built to do that: a capture that misses its time limit delivers nothing, the background job it asked for included, and the next trigger asks again (`internal/hook/hook.go`, "A capture that misses the deadline delivers nothing, its spawns included"). One stall of a third of a second on a shared runner is enough. The scenario asks for an exact count, so one slow hook, which the p95 figures are built to absorb, turns the whole job red, and the message does not say which start was missing or why.

## What to build

1. **The count allows for hooks the benchmark itself timed past their limit.** In the padded-inbox scenario, each Stop and SessionEnd that should start the stand-in is timed. A start may be missing only for a hook whose measured time reached its limit less the hook's own margin (290 ms for these events; take the figures from one place, with a comment that names the Go constants they mirror). So the count must be between the expected number less those hooks and the expected number, and maintain and the day's sweep are still seen once each. With no hook that slow, the check is exact, as it is today.
2. **The decision is a small function** in `scripts/bench/core.py`, given the expected number, the number seen and the hooks' times, so that it is tested on made-up numbers in `scripts/bench/test_gate.py`: none slow and one missing fails; one slow and one missing passes; one slow and two missing fails; more seen than expected fails.
3. **The message says what was seen**: how many starts of each kind (digits in a line replaced, so that the thirty ends are one kind), how many were expected, and how many hooks passed their limit. No path and no session id beyond the scenario's own made-up ones.
4. **A hook that gave no output because it passed its limit is not a fault of the code either.** The same scenario reads each pointer prompt's output as JSON and stops the whole benchmark on an empty one (CI's Linux job failed that way on 80e6aa2, with every test green). A hook that misses its limit prints nothing, by design. So an empty output is passed over, and counted, only when that hook's measured time reached its limit less the hook's margin; an empty output from a hook that was in time still fails, and so does more than one in twenty of a loop's hooks passing the limit, since that many is no stall. The benchmark prints, beside its report and not in it, how many hooks passed their limit in each loop that had one.
5. **The same for the first chat start's sweep**, which waits ten seconds for one line: if the chat start itself was timed past its limit, say so in the message. It still fails, since later steps need the sweep's day.
6. **Say it in `docs/verification.md`**, where the benchmark is described: one sentence on what the count allows and why.

## Owns and may touch

`scripts/benchmark.py` (the padded-inbox scenario's two checks of started jobs only), `scripts/bench/core.py` (one new function), `scripts/bench/test_gate.py`, one sentence of `docs/verification.md`.

```paths
scripts/benchmark.py
scripts/bench/core.py
scripts/bench/test_gate.py
docs/verification.md
```

## Must not touch

Any Go file: what the hook does when it misses its limit is v1's, and DV-12 changes it behind a switch. The gate's rule and its report. The plain benchmark's report: the same keys, in the same order, with the same sample counts. `.github/workflows/**`.

## Accepted when

- `scripts/linux-check.sh` passes, and `python3 -I scripts/bench/test_gate.py` passes with the four new cases, each able to fail.
- An empty output from a hook timed inside its limit fails the scenario; one from a hook timed past it does not; more than one in twenty past the limit fails: a case for each on made-up numbers.
- With the stand-in made to lose one start (a test-only way, such as a stand-in that drops its nth call) and no slow hook, the scenario fails, and its message names the kind that is one short.
- The report says, from the Go code, which time limit each of the two events has and what margin the hook takes off it, and shows that the Python figures match.
