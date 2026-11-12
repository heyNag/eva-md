# SEAM-0b: the hook's handler list

**Plan:** section 8, Stage 0, row SEAM-0 ("a handler list for the hook"); section 8, "What runs in parallel" (which track owns each handler file of `internal/hook`); section 9 ("a new command, hook handler, status row, config key or benchmark scenario is a new file"). **Design:** `evaluate.md` section 12.3 rule 4; `critique-delivery.md` point 4. **Size:** S to M. **Reviewers:** two (it changes the hook). **Depends on:** SEAM-0a and SEAM-0c (merged); EV-16 (the gate, and its baseline taken before any hook change); CT-1 and CT-2, which change `hook.go`, `trigger.go` and `spawn.go` for counters and merge first.

## Why

Every background job a hook asks for is decided in the middle of `capture` in `internal/hook/hook.go`, or inside the brief in `resume.go`, and started by `launch` in `spawn.go`. DV-12 must route those jobs through its worker, and later packages add steps of their own. Without a list, each of them edits the 320-line `capture`. After this package a new step is a new file that registers a handler. Nothing the hook stores, prints or starts changes.

## What to build

1. **The list**, in `internal/hook/handlers.go` (new): a `handler` with `place int`, `name string`, `at point` and `run func(*turn)`, registered from its own file's `init` by `registerHandler`. Handlers run in the order of their places; two at one place panic, naming both, as `registerRows` in `internal/health/rows.go` does.
2. **Four points**, each where today's code makes the decision, so no ask moves to a different moment:
   - `atGuarded`: where `queueMaintain` is called today, after the guards and before the session lock.
   - `atStored`: just before each `return resumeContext(...)` in `capture`, after the event's line is stored. The prompt-gap ask moves here from before the prompt's append; nothing between the two places returns or reads what the ask reads.
   - `atBriefed`: in `brief` (`resume.go`), where `queueCatchUp` is called today, once `resume.Build` has returned.
   - `atLaunch`: in `Run`'s deferred function, after the output is written, once the asked jobs have been judged due (`withSweep`, then `due`, in the order asked, as today). Handlers here may change or remove jobs. Then each job left is started with `env.spawn` or `detach`.
3. **A `turn`** carries what handlers read: the `Env`, the host, the payload's event, session, source and whether it is headless, the project key, the session's inbox file and its stat from before the hook appended (`before`), the time, at `atBriefed` the session files the brief read and the sessions start-of-chat recovery appended to, and the jobs. A handler asks with `t.ask(job)`. It carries no prompt, reply or other captured text.
4. **A `job`** has `args []string` (the command line after the binary, exactly as spawned today), `reason string`, and `git bool`. `jobEnv` gives `gitEnv`'s variables to a job whose `git` is set, in place of today's test of `args[0] == "maintain"`; the maintain handler sets it on both of its jobs. `reason` is one word from a closed list held in `handlers.go`: `daily`, `push_retry`, `prompt_gap`, `stop_backlog`, `session_end`, `pre_compact`, `chat_start`. If CT-2 has given these triggers names on the command line, use the same words. Nothing starts with the reason in its arguments that today's spawn lacks.
5. **Today's asks become handlers**, each in the file that holds it now: `maintain.go` registers the daily maintain and the push retry at `atGuarded`; `trigger.go` registers the prompt gap, the stop backlog, and the session end and compaction asks at `atStored`, and the chat-start catch-up, with its sweep, at `atBriefed`. `queueMaintain`, `queueUpdate` and `queueCatchUp` keep their conditions; only where they are called from changes.

The per-event capture in `capture` (the switch on the event), the brief, the pointers and recall stay where they are. A later package that needs an output step adds a point to this list.

## Owns and may touch

`internal/hook/handlers.go` (new), `internal/hook/handlers_test.go` (new), and in `internal/hook` only these: `hook.go`, `resume.go` (the catch-up call), `trigger.go`, `maintain.go`, `spawn.go`.

## Must not touch

Every path outside `internal/hook`. `pointers.go` and `recall.go`. What a hook stores in the inbox, writes to a marker or a heartbeat, or returns to the host; which jobs it starts and when; the trigger thresholds; any plugin byte or hook declaration.

## Accepted when

- `scripts/linux-check.sh` passes, with no existing test changed.
- A handler registered in a test file at each point runs once per hook run at that point, sees the facts listed in item 3, and does not run in a run that a guard stops before its point (an excluded folder, a chat off the record for `atStored`), by test.
- For each of today's asks, the job reaching `env.spawn` has today's exact arguments and the reason named in item 4, by a test for each, the sweep both due and not due.
- An `atLaunch` handler in a test that rewrites a job's arguments is seen by `env.spawn`; one that removes every job starts nothing.
- `jobEnv` gives the git variables to both maintain jobs and to no update job, by test.
- Two handlers at one place panic with both names, by test.
- EV-16's comparison (`scripts/benchmark.py --compare OLD NEW`), the old binary built from `main` before this change, shows no gated metric worse than EV-16's rule allows. Report the figures.

## As merged

Merged from `pkg/seam-0b-r1b` on 2026-11-12, after two reviews, a fix round and a verification that accepted it with nothing open, having broken the code five ways and seen a test fail each time. What the code holds beyond the list above:

- **Reasons are the counters' words**, as CT-2 writes them: `stop_size` and `compact`, not `stop_backlog` and `pre_compact`.
- **The turn carries the capture's context** as well as the facts of item 3, and its time is set only from `atStored` on, so the clock is read no earlier than in v1.
- **`atLaunch` runs only in a run whose capture passed the guards and returned in time.** It runs even when no job is due. Before the capture fills it, a run's turn holds only the payload's host, event and session, so a test can see a run that a guard stopped; `launch` starts nothing for it.
- **Left as they are**, since removing them changes tests this card may not touch: the unused trailing parameters of `capture` and `resumeContext`, the test-only `detach` wrapper, and the `[]string` arm of `jobEnv`. A later card may remove them.

For [DV-12b](DV-12b.md): `launch` judges every job first, which takes the day's attempt file, then runs the `atLaunch` handlers, then starts the jobs, all under one recover. A panic in a later judgment, or in an `atLaunch` handler, so starts nothing while the day's maintain is already marked as attempted, and the day's commit and push wait until tomorrow. No `atLaunch` handler exists yet, so nothing changes today. DV-12b either recovers each `atLaunch` handler on its own and still starts the judged jobs, or takes the attempt file only once a job is started or handed to the worker.

The latency gate could not judge this package on the build machine, which was busy. CI's macOS latency job on the merge is the run of record.
