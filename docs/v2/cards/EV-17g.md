# EV-17g: the continuation driver's guards before a live run

**Plan:** follows [EV-17b](EV-17b.md) and the lead's ruling of 2026-11-13 (the pilot runs on it); section 6.6; section 8, rows EV-17 and EV-03; decisions 16 and 28. **Design:** `working-papers/design/evaluate.md` 4.3, 4.5, 5.3 and 14 (fact 3); [host-facts.md](../host-facts.md) F-062. **Size:** M. **Reviewers:** two: the contract, and one who tries for a hidden file in a chat, a row for a trial that should be unjudged, a session past a ceiling, a leftover process. **Depends on:** [EV-17b](EV-17b.md).

## Why

The pilot's sessions are this driver's first real ones: a ceiling must hold, no chat may reach what grades it, a run records what it left unjudged, and grading leaves nothing running. eva-md is unchanged: no switch, no line in `./cmd/eva-md`. Elsewhere: EV-03b exports the pilot's tasks; EV-17c sets `memory_off`; EV-03c reads rows and records; the run-book is the lead's.

## What to build

1. **No usage.** `report` says whether usage was given: each final event its reader counts (Claude Code's `result`, each Codex `turn.completed`) has a `usage` object, or on Claude Code a `modelUsage` naming a model. An answered call without it, or with 0 prompt and output tokens, leaves its trial unjudged `no_usage` and stops the run: the rest not run, summary and record still written, `Run` errs, exit 2. The fake gains an answer with no usage; scripts giving 0 tokens give some. Output is read within `outputLimit` (256 MiB), lines too.
2. **Codes.** After `host_failed`, `Codes` gains `CodeNoUsage`, `CodeMemoryOff`, `CodeHiddenFound` (`no_usage`, `memory_off`, `hidden_found`). Only an `Arm` sets `memory_off`, and it stops the run as `no_usage` does: every further trial would spend a session to learn nothing.
3. **Memory off by variable too.** `hostEntry.OffEnv`, in the chat's environment under every condition, `Plugin` included: `CLAUDE_CODE_DISABLE_AUTO_MEMORY=1` on Claude Code, beside `--settings` (evaluate.md 4.5); none on Codex (F-170). `--pass-env` refuses a name it sets.
4. **One record a run.** Once its folder is made, a run appends at its end, however it ends, one JSON line, `drive.Record`, to `<out>/runs.jsonl` (`drive.RunsFile`), as `results.Store` appends a row. Fields, exactly: `v` (1), `run`, `instrument` (`continuation`), `host`, `host_versions` (sorted, unique), `condition`, `model` (asked, if any), `commit`, `dirty`, `tasks` (ids, run order), `trials`, `planned`, `judged`, `not_run`, `unjudged` (by code, zeros too), `no_model`, `tokens`, `rows`, `kept`, `ended` (`done`, `error`, or the code that stopped it: `no_usage`, `memory_off`, `hidden_found`). Strings are checked as `Append` checks a row's. `drive.LoadRuns(dir) ([]Record, error)` reads it, refusing an unknown field or code. A killed process writes none.
5. **A second attempt is kept apart.** A run is refused before its plan when `--out`'s rows hold one of instrument `continuation`, its host, its condition (the metric's third part) and a scenario `<task>.t<n>` of its tasks; when `runs.jsonl` holds a record of its host and condition naming one of them; or when either file exists and cannot be read. A second attempt takes a fresh `--out`, outside `--work`.
6. **What a chat can reach.**
   - `--work` is absent or empty.
   - A trial's folder is removed once its rows are written, it is unjudged or the run stops in it, folders made writable first (the grading copy's too); one that cannot be removed is tried again, while a job of the trial's may still hold a file in it, up to a limit tests set, and then stops the run. `--keep-trials` keeps them, as the plan line and record say.
   - After `task.Prepare` the driver lists the workspace; after each call, and each grading a prompt follows, it checks the trial's folder but `grade/`: by path, a workspace path a file of `hidden/<repo>/` would have under its repository that the start lacked; by content, a regular file (no link followed) holding a hidden file's bytes whole. Found: `hidden_found`, and the run stops: a hidden file in a chat's reach is a fault of the set-up, not of one trial. Right after `Prepare`, the task is at fault, and the run stops too. Files are streamed: memory is bounded by the largest hidden file.
   - The package comment says what it cannot guard: a chat run as the invoking user reads `--tasks`, `--repos` and `--out` by path, or copies a hidden test reworded; a process leaving its group outlives a call; code run in grading may write wherever that user can. So the run-book runs the CLI as a second user through `--cli`.
7. **Go's telemetry off.** `task.GoTelemetryOff(home string) error`, in `grade.go`, writes `off` to `<home>/.config/go/telemetry/mode` (on macOS `Library/Application Support` for `.config`). `Grade` calls it on its home before any test, the driver on each trial's.
8. **The command's map.** `continuation`'s subcommands become a map a file adds to, as `pilot`'s is by `addPilot` (`cmd/eva-eval/pilot.go`), so EV-03c adds `report` from a file of its own.
9. **Limits other packages' tests raise.** `versionTimeout` and `waitDelay` become exported variables, so `v1arm`'s tests (EV-17c) can raise them.

## Owns and may touch

EV-17b's files and grading. After this merges, EV-17c adds `drive/plugin.go`, and EV-03c adds its command through item 8's map.

```paths
internal/eval/continuation/drive/**
internal/eval/continuation/task/grade.go
internal/eval/continuation/task/grade_test.go
internal/eval/continuation/task/main_test.go
cmd/eva-eval/continuation.go
cmd/eva-eval/continuation_test.go
```

## Must not touch

eva-md's code; the task format, rows and `Prepare`; `internal/eval/results`; `v1arm`; `cmd/eva-eval/main.go`.

## Accepted when

- `scripts/linux-check.sh` passes; `go list -deps ./cmd/eva-md` has no `internal/eval`; `TestHostLiteralCount` unchanged.
- On the fake, per host, each test failing without its item: an answer with no usage, and one of 0 tokens, give `no_usage`, no row, the rest not run, `ended` `no_usage`, exit 2.
- An `Arm` giving `memory_off` writes no row, is counted, and stops the run with `ended` `memory_off`; a file added to the map runs as a subcommand.
- Claude Code's chat holds the variable under `off`, `oracle` and `Plugin`; Codex's does not.
- Two conditions run in one `--out` give two records, exact but run ids; `LoadRuns` refuses an unknown field or code, and a partial line.
- A second run of a host, condition and task in that `--out` starts nothing, also when the first judged none; another condition runs; an `--out` inside `--work` is refused.
- A non-empty `--work` is refused; no trial folder is left, even one the fake made read-only; `--keep-trials` keeps all.
- A hidden file the fake puts in the workspace by path, in the home renamed, or by code a hidden test runs, gives `hidden_found`, no later prompt and no later trial; a start holding a hidden file's bytes stops the run before any chat.
- A stub test failing unless its home's mode file says `off` passes under `Grade`; after a grading that runs go, the home's telemetry folder holds `mode` alone; each trial home has the file.
- Mutations (no usage graded, a code uncounted, no check after grading, a trial folder kept, the duplicate key without the host, no mode file) each fail a test.

## The lead's rulings of 2026-11-13

The card's checker left these to the lead.

- **The telemetry test holds the cause, not the process**: the mode file is in place before any test, and go leaves nothing else in the home's telemetry folder. Go's telemetry process leaves the test's process group, and waiting for it would rest on time.
- **`memory_off` and `hidden_found` stop the run**, as `no_usage` does (items 2 and 6). Each means the set-up is wrong, and the pilot's sessions are too few to spend on finding that out one trial at a time.
- **A second user for the chat is the run-book's, with no seam in the driver.** In the container the driver runs as root with the tasks and the repositories readable by root alone; the path given as `--cli` is a wrapper that gives the trial's home and workspace to the chat's user and starts the real CLI as that user. The driver then grades and removes as root. The smoke run shows whether that holds.
- **The hidden check has no cap**: it reads every file of the trial's folder but the grading copy, and a cap would let a padded copy through.
- **If the card proves too large for one run**, item 6 becomes a second round after the rest has merged, and the builder says so and stops there.
- EV-17e keeps what this card cannot do: grading as another user, and the invoking build cache.
- Not in this card, and not owed: a check of a call's output for the tasks folder's name, and a flag to start at a later trial.

## As merged

Merged from `pkg/ev-17g-r1` on 2026-11-14, after two reviews, one fix round and a verification that accepted it. A restart of the machine cut the first builder off with all nine items committed; a second read that work against the card, found a gap in item 6 and closed it. The reviews found three major faults, all mended: a call that printed an answer with no usage and was then ended by a signal went uncounted and did not stop the run; under `--keep-trials` a later chat could read the earlier trials' solved workspaces; and code one hidden test ran could rewrite a later hidden test in the shared grading copy, so a wrong answer could be judged solved.

Accepted as built, beyond the card:

- **The hidden check reads the whole `--work` folder**, the grading folder included, not the trial's alone: code a hidden test runs could otherwise leave a copy beside the trial for the next chat. No grading copy exists when the check runs.
- **A run that keeps no trials removes its whole folder under `--work`** when it ends.
- **Under `--keep-trials` each kept trial is closed to all but the driver's user** once it ends.
- **The hidden files are laid again before each test of a grading**, and the grading copy is the driver's alone.
- **Claude Code's tokens are the larger of what `usage` and `modelUsage` name**, as the model runner reads them (DV-11c).

The lead's rulings at the merge:

1. **A call whose spend cannot be counted does not stop the run**: a session that hit its time limit before the host printed any usage, or a host's failure with no final event. The run goes on and counts the trial unjudged; `--max-sessions` and the session's limit bound such calls, not `--max-tokens`. The hard bound on spend is a limit the owner sets on each key at its provider, and the run-book has the lead compare each run's counted tokens with the provider's own figure.
2. **The driver keeps each call's raw output when asked**, in a folder only its user reads, outside the work and results folders and never committed: without it a wrong reading of a real host (no usage, a rate limit read as a failure, no model) cannot be mended from the run. A short second round on this card's files adds the flag. Built and merged the same day from `pkg/ev-17g-r2`: `--trace DIR` writes each call's two streams to files of its own, `<run>/<task>/<condition>/t<n>/<k>-version` and `<k>-chat` with `.stdout` and `.stderr`, in a folder the driver's user alone reads; the plan line says a trace is kept and the run's record does not. Known and left: with a trace, a process the host started that left its group and still holds the error stream makes the call fail after the wait, with an error that names the output; without a trace that call succeeds. If the smoke run meets it, that run is made again without the flag and the lead reads why.
3. **A hang-up ends the run as an interrupt does.** `cmd/eva-eval/main.go` listened for an interrupt and a termination only, so a dropped connection killed the driver and left the chat running with no limit. The lead mended that file with this merge.

Known and left, for EV-17e: grading runs the chat's code as the driver's user, so that code can read what that user can; a copy of a hidden test that is compressed or reworded is not found by the check; and a process that leaves its group outlives its call. The run-book runs the chat as a second user and says what that does not cover.

Added by the lead on 2026-11-14: CI's macOS job failed twice on this package's tests, on two different tests, and passed between them. The second (run 178, `TestTraceFoldersRefused`) has a plain cause: the rig commits in a repository of its own, git then starts maintenance in the background, and a test that walked the rig met a file of git's that had gone by the time it was read. The rig's git configuration, and the evaluation command's, now turn git's background work off, as the release tests' already did. The first (run 176, `TestNoTrialFolderIsLeft`: a sign-in failure read as a host's failure in the first of two trials) was looked for in a third round on this card's files, merged from `pkg/ev-17g-r4` on 2026-11-14, and **not found**. Reading ruled out the driver, every time limit, the fake's read-only folder, the reader and anything left by the run before; the test passed 70 times here under the race detector on a busy machine. What is left is that the fake's first call ended before it printed: a fault of its own, a crash of the race-built fake at its start, or a signal. Nothing was guessed. The fake now records how each call ended, the failing test keeps a trace, and a wrong outcome prints both, so the next failure says which. If it comes back, read that output first; CI's macOS job may fail on it again until then, and a second run of the job is no fix.

Added by the lead on 2026-11-14, a last round before the pilot (merged from `pkg/ev-17g-r6`): a condition can stop a run before a trial starts, and the run then ends `arm_stopped` with its counts adding up; what a condition spent in a trial that ends unjudged is counted toward the ceiling and in the record; `--sign-in` is an option of the driver; and with a trace each call leaves a third file, one line on how it ended. The report prints every ending the driver knows. **Owed before a full run of the pilot:** every chat's web tools turned off in the host table, since the pilot's hidden tests are in this public repository and a host's web search runs at its provider (the run-book's last section).

Added by the lead on 2026-11-14, the round that was owed (merged from `pkg/ev-17g-r7`, after a verification that accepted it; a restart of the machine cut the first verifier off): **every chat's web tools are off**, at the first call and each follow-up, under every condition, by flags the host table puts after a condition's own so that none can undo them. Claude Code: `--disallowedTools WebSearch WebFetch`. Codex: `-c web_search=disabled`, and `--disable` for each of five features the CLI's own list names (`standalone_web_search`, the three `browser_use` features and `computer_use`), as v1's update does it; the list is read once a trial, kept in the trace as `<k>-tools`, and a list that cannot be read stops the run before any chat. **A call whose output names a web tool's call** (Claude Code's `WebSearch` or `WebFetch` tool use, a server tool use `web_search` or `web_fetch`, or a count of them above 0 in the usage; Codex's `web_search` item) leaves its trial unjudged under a new code, `web_used`, which stops the run as `hidden_found` does, also when the session's limit cut the call. `LoadRuns` reads a record written before the code existed as counting it 0. The fake host refuses any chat whose arguments leave a web tool on, so every test on it fails if the table drops a flag.

Each flag is a guess: no host fact records one. The run-book's smoke step checks them on each host, and adds a probe of a started and a resumed session that asks for a search, since a chat that never wanted the web shows nothing either way. Known and left: no test holds that `web_used` wins over an output past its limit (both stop the run; the verifier's one surviving mutant). Under a subscription sign-in a host may offer the account's remote tools, which the table does not turn off; the run-book signs in with keys only.

With the same merge the fake host leaves through `syscall.Exit`: it is the test binary run again, and under the race detector `os.Exit` first sleeps a second in the race runtime, which the tests paid at each of the fake's hundreds of starts. CI's macOS job had failed twice on it (runs 185 and 186: this package past go test's ten minutes under the detector, 40 seconds without it).

Added by the lead on 2026-11-14, after CI's runs 200 and 202 failed on macOS under the race detector in this package, each in one subtest, with "the host's CLI gave no version" after a fifth of a second (run 176's one failure, a chat call ended by a signal, is now read as the same event). **The cause is not proven.** What is known: in runs 176 and 200 the system wrote one crash report for this package's test binary, and a passing run had none; the fake is that binary run again, hundreds of times a run; nothing in the fake, the rig or the driver sends a signal that leaves a report. The two readings left are a race runtime that aborts as the process starts, and a kill for code signing. No mend was made, and none is a retry.

What was merged instead makes the next failure say which: every test of the rig logs, when it fails, how each call of the CLI ended as the driver saw it and what the fake recorded of that process (the seam `Options.ended` covers the version call and the features list now), and CI's macOS job has a step that prints, of a test binary's crash report alone, the exception, the termination's namespace and the top functions of the thread that failed. If it is the race runtime, the mend is a fake that is not race-built, built once by the tests; if it is code signing, the link the rig makes to the test binary is where to look. Known and left: a run without a trace still drops the version call's stderr.
