# EV-16e: the benchmark's code-brief check allows for a brief whose git calls ran out of time

**Plan:** section 6.6 (the latency instrument), section 9 ("Tests never depend on machine speed" holds for the checks CI fails on). **Follows:** [EV-16c](EV-16c.md), whose notes left this owed. **Size:** S. **Reviewers:** one. **Depends on:** EV-16d merged, which changes the same files.

## Why

On a busy machine the padded inbox fails with "a startup brief did not say how far the code of 3 systems moved" while every test passes; four builders lost a run of the checks to it in a day. The brief answers well inside its 1500 ms, but v1 counts each pointer's code under limits of its own: one `git rev-list --count` a cited commit, at most 60 ms a call and 150 ms in all, and only while 250 ms remain before the deadline (`codeClock`, `CodeCall`, `CodeBudget` and `codeLeft` in `internal/resume/systems.go`; `gitReserve` in `internal/resume/git.go`). The first call that runs out is killed with its process group (`KillGroup` in `internal/coderef/git.go`, SIGKILL) and stops the rest, and those pointers show `(as of <day>)` (docs/design.md, "Code in the pointers"). Nothing the hook prints or stores says so. But eva-md's git calls keep the global git config, which the benchmark's home names (`GIT_CONFIG_GLOBAL` in `Home`, `scripts/bench/core.py`; `gitDropped` in `internal/identity/gitenv.go` drops `GIT_TRACE*` but not the config files), so git's own trace, turned on there, records each count: started, exited with its code, or killed. No Go file changes, nor anything v1 does: no switch, no line added to `./cmd/eva-md`.

## What to build

1. **Git's trace of each code brief.** In `code_scenarios`'s brief loop only, before each brief the benchmark writes the home's global git config with `trace2.eventTarget` set to a file of that brief's own under the home, and removes the config after the loop, so no other hook is traced. A count is a traced process whose `start` argv holds `rev-list`, `--count`, `--max-count=1001` and `--literal-pathspecs`, which only coderef's calls carry (the live git's `rev-list --count` calls in `internal/resume/git.go` do not); its outcome is the code of the `exit` event with its `sid`, or killed when it wrote none. If git writes no trace from that file, stop and report: a Go change is the lead's to card.
2. **The decision**, a function in `core.py` given one brief's three `System: Code A`, `B`, `C` pointers, each as noted (it holds ` (code moved `) or not, in that order, the counts' outcomes in the trace's order, and the brief's measured ms. A missing pointer fails. A brief with all three notes passes only when its trace holds exactly three counts, each exited 0, so a trace that stops working fails at once. A brief lacking notes is passed over, and counted, only when all hold: the notes it lacks are the last ones, since the clock stops every count after the first it cannot make; the first counts exited 0, one for each note shown; at most one count follows them, killed or exited 0 (`os/exec` reads a count that ended as its context did as out of time); and either that count was killed, which shows the cause was time, since coderef kills a call only when its context ends, or the brief's measured time reached `CODE_CALL_MS`, 60, the least a brief whose counts time cut short can take. Anything else fails, with a message giving the brief's number and ms, which pointers lacked notes and each count's outcome: no path, no session id.
3. **The share.** More than one in two of a loop's briefs passed over fails: a fault of the code that looks like time hits every brief, and the run must still show the counts working. The figure is a named constant beside `CODE_CALL_MS`.
4. **Said beside the report**, as `say_late` says it, when any brief was passed over: how many of how many, and how many of those had a count killed. The loop keeps its `say_late("code_brief", "SessionStart", briefs)`, which `MissedLimits` checks.
5. **One place for the figure.** `CODE_CALL_MS` in `core.py`, with a comment naming `CodeCall` in `internal/resume/systems.go`, which it mirrors.

The report's keys, order and sample counts do not change; a brief passed over keeps its time among `code_brief`'s samples.

## Owns and may touch

In `benchmark.py`, `code_scenarios`'s brief loop and its call in `padded_inbox`, which may pass the home. In `core.py`, the two constants and new functions beside `starts_allowed`. In `test_gate.py`, a new class. `docs/verification.md` is EV-10a's: the report gives the words for its sentence that "every pointer must say the code moved", and the lead writes them.

```paths
scripts/benchmark.py
scripts/bench/core.py
scripts/bench/test_gate.py
```

## Must not touch

Any Go file. Every other scenario and loop, `Home`, `run_hook`, the gate's rule and `compare`. `.github/workflows/**`, `scripts/linux-check.sh`, `docs/**`.

## Accepted when

- `scripts/linux-check.sh` passes, and `python3 -I scripts/bench/test_gate.py` passes with the new cases, each able to fail.
- Cases on made-up outcomes and times, one for each branch: three notes with three counts exited 0 passes, and with two counts, or one exited 128, fails; C lacking after two counts exited 0 and one killed passes at any ms; C lacking with no third count, or with its count exited 0, and all three lacking with no count, pass at 60 ms and fail at 59.9; B lacking with C noted fails; two counts after those of the notes shown fail, killed or not; a missing pointer fails; a loop with one in two passed over passes, and with one more fails.
- The trace reader, given made-up lines of the shape git writes (a count started and exited, one started and killed, a live git `rev-list --count`, a `status`), returns the two counts alone, in order, with their outcomes.
- A test reads `internal/resume/systems.go` and fails unless `CodeCall` is `CODE_CALL_MS` ms and no more than `CodeBudget`, as `test_the_figures_mirror_the_hooks_go_code` reads `hook.go`.
- A test reading `code_scenarios`, as `MissedLimits` reads prompt loops, fails if a brief's notes are judged other than by the decision, with that brief's elapsed.
- `GateLoops` passes unchanged: `code_scenarios` still times [10, 25].
- The report says a plain run on this checkout passed with the trace read, and gives `code_brief`'s median and p95 from four plain runs, two of the script as EV-16d left it and two of this card's, alternated, with the load: what the trace costs a brief, reported, not judged.

## The lead's rulings of 2026-11-13

The card's checker left these to the lead.

- The share stays one in two until the counts that busy builders' runs and CI's print have been read; the lead tightens it then.
- With the trace on, `code_brief` costs what the trace costs. The gate compares two binaries traced alike, so its verdict stands; the next refresh of the stored baseline says, in the baselines' README, that its `code_brief` is measured with the trace.
- Whether CI's macOS git reads the trace's target from the global config is seen on the merge: the card's own check fails loudly if it does not.
- A chat start in the brief loop that passes its limit and prints nothing is not this card's.
