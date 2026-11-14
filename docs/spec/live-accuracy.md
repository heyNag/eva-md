# The live accuracy run

- **Component:** The live accuracy run (a developer's command)
- **Paths:** `cmd/live-accuracy`, `internal/update/accuracy/live`
- **Status:** v1 as built, frozen on 2026-11-14

The blocks below are v1's design and verification text, moved here word for word when the two documents were split; [INDEX.md](INDEX.md) lists every spec.

<!-- v1 docs/verification.md 018 -->
- **Live accuracy run** (`cmd/live-accuracy`, `internal/update/accuracy`, `internal/update/accuracy/live`, `internal/update`, offline).
  The 12 fact scripts moved to `internal/update/accuracy` with every turn, fact and pass as they were, and give the offline suite the same session files, batches, reports, pages and metrics.
  The run's loop, with the suite's own correct canned outputs as the model's answers on each host's chats, meets the target with nothing missed or refused; with the adversarial outputs only the metrics catch, it counts each content failure once, at its pass, and misses the target (`TestAccuracyLiveRun`).
  A refused answer leaves the page and its `as_of` as they were and the run goes on; a host failure, or a context that ends, stops it unjudged (`TestRunCountsRefusedAnswers`, `TestRunStops`); each enforceable obligation a pass breaks is tallied at its pass (`TestRunTalliesBrokenObligations`, `TestBroken`).
  A refused answer counts the same however it is refused: it is a content obligation missed, and the page, measured at its chat's time, misses what the chat should have done, as content whatever its kind, so the validator's refusal of an answer that gives most of the page's ids no action misses the
  target (`TestAccuracyLiveRefusals`, `TestARefusedAnswerCountsAgainstTheTarget`, `TestUnanswered`); after a refusal a script's later passes aren't held to its out-of-order guard (`TestARefusalLeavesTheGuardUnjudged`).
  Each of the validator's guarantees is reported on an update that breaks it (`TestGuarantees`), each fact obligation and input check on what breaks it (`TestObligations`), and each miss by kind, a planted fact counted once a kind, each refused answer as content, with the target's 2 content misses allowed (`TestMeasureMisses`, `TestTally`).
  The command refuses without `--host` or `--spend-model-quota` and on any usage error, an effort or model the runner would refuse among them, with no runner made and nothing written (`TestRefusals`, `TestCheckFlags`), and refuses a `--cli` that names no executable file, which the runner would pass
  over for the CLI on PATH (`TestABadCLIIsRefused`); a run asks for each of the 21 passes in order with the update's rules, the batch, the schema for its page and the flags' effort, model, CLI and Codex config switch, in an empty run folder in a temporary home it removes, prints each history, the
  counts against the target and the efforts the answers came at, and exits 1 when the run misses it (`TestRun`, `TestAsk`, `TestReport`); a host failure stops it, home removed (`TestAFailureStopsTheRun`), and so does a `--model` the host rejects, at the first answer, which came from the host's default
  model, whether the runner takes that answer or refuses it, the stop then naming the refusal (`TestARejectedModelStopsTheRun`); and through the real runner, a fake claude runs 21 times in those folders and nothing is written in the home folder (`TestTheRealRunner`).
  With `--record` and `--pages`, a run on a fake runner whose answers add items, change nothing, are refused by the runner or by the validator, at several efforts, writes a line for each of the 21 passes, holding nothing of the machine, and the 12 pages, each the page the report prints for its script
  when it prints one, and `--replay` of that recording, with no runner, prints the same report, exits the same and writes the same pages byte for byte, and without `--effort` says in its first line that the effort is as recorded (`TestRecordAndReplay`, `TestReplayMeetsTheTarget`, `TestRecordLine`); a
  recording of another host, model or format version, with a reason the update does not know, a key added, missing, given twice or of another type, null among them, or a pass missing or repeated, is refused before anything runs (`TestReplayRefusals`, `TestTheRefusalsRecordingReplays`), and so is one
  from a run that stopped part way, which keeps the passes up to the stop (`TestAStoppedRunsRecordingIsRefused`), a `--record` that exists and a `--pages` that is not new or empty (`TestRecordAndPagesRefusals`); a recording that cannot be made stops the run before anything else is made
  (`TestARecordingThatCannotBeMade`); a page that cannot be written stops the run before its next call and recording, and the last script's is said on stderr after the report (`TestAPageThatCannotBeWritten`); and a replay whose context has ended stops at its first pass (`TestAReplayStopsWithItsContext`).
  With `--trace`, a run on a fake runner writes `run.json`, a line for each of the 21 passes with what the runner reported and, for each answer the report counts refused, why, the validator's error among them, the raw output of every attempt byte for byte, and `end.json`, none of them holding a path,
  a temporary folder, a time of day or the machine's system, and with `--record` and `--pages` each of the three writes what it writes alone, with the report and progress of a run without them, each call's milliseconds within the run's wall time and at least the time a slow call was held, and every
  file and folder of the trace private to its owner, an empty folder that was there included (`TestTraceOfARun`); a run that stops on a rate limit, an answer from the host's default model, an interrupt, a request that cannot run or a raw file that cannot be written keeps its trace up to that pass and
  ends it with why, as a code (`TestTraceOfAStoppedRun`); a trace that cannot be made leaves nothing of the trace or the recording behind, and a pages folder that cannot be made after it takes the trace away again (`TestATraceThatCannotBeMade`); `run.json`'s commit and changes are the build's
  `vcs.revision` and `vcs.modified` (`TestBuildFrom`); through the real runner, a fake claude's version, tokens, attempts, refused effort, model and output reach it (`TestTraceThroughTheRealRunner`); `--timeout-refused` counts a timeout refused and goes on, keeping the timeout in the recording and the
  trace, and a replay with it prints the same report and pages, while without it the run and the replay stop there (`TestTimeoutRefused`, `TestRefusedWhy`); and a trace folder that holds a file, is a file, a link to nothing, the pages folder, a folder in it or the recording's folder, or is given with
  `--replay`, is refused before anything runs (`TestTraceRefusals`), and so is one in the git work tree the command is run from, named directly or through a link (`TestTraceInsideTheWorkTree`).
  The runner's tap, which only the trace sets, gets each attempt's stdout and stderr byte for byte, the attempt a refused effort steps down from and one that timed out among them, never codex features list, once for each attempt the Result counts, and leaves the Result as it is without it, a rate
  limit that only stderr names included (`TestTapGetsEachAttemptsOutput`, `TestTapGetsATimedOutAttempt`, `TestTapCallsMatchTheAttempts`, `TestTapLeavesTheStderrThatDecides`); no Go file of the module outside `cmd/live-accuracy` sets a tap (`TestOnlyTheLiveRunSetsATap`).
  No workflow or script builds it: each `go build` there builds `./cmd/eva-md` alone (`TestNoReleaseBuildsIt`), and only it imports the suite (`TestOnlyItImportsTheSuite`).

<!-- v1 docs/verification.md 048 -->
## Live accuracy run

The live accuracy run checks the Wiki updates stage's accuracy target on a real host (see [design.md](../design.md), "Testing").
It has not run yet.
It sends each pass of the accuracy suite's 12 fact scripts through the host's real update command, as the update job does, checks and applies the answer, and prints each history's metrics and misses, with the page of each history that missed something, then the counts against the target: every enforceable obligation met, no speaker or date error, and at most 2 content obligations missed.
It is a developer's command, which no release or install builds.
Run it from the repo root, one host at a time, on a Mac where that host is logged in:

```sh
go run ./cmd/live-accuracy --host claude-code --spend-model-quota
go run ./cmd/live-accuracy --host codex --spend-model-quota
```

Each run makes 21 model calls on the host's subscription, one a pass, each allowed up to 10 minutes.
Without `--spend-model-quota` it says so and calls nothing.
`--effort` sets the effort, `highest` unless given, as for the update, and the report counts the answers at each effort the host used, since the update steps down an effort a model refuses; `--model` sets a model, the host's default unless given, and a model the host rejects stops the run at its
first answer, which came from the host's default model instead; `--cli` names the CLI by its absolute path, the one on PATH unless given; and `--codex-ignore-user-config` runs Codex as `codex_ignore_user_config = true` runs the update.
An effort the host lacks, a model name a CLI flag can't take and a `--cli` that names no executable file are refused before anything runs.
It works in a temporary home of its own, which holds each call's empty run folder and is removed at the end, and reads and writes nothing of eva-md's: no config, state, inbox or wiki.
The host CLI runs with your usual login, as the update job's calls do.
An answer the update would refuse counts as a content obligation missed, and so does each fact its chat says or ends.
Progress goes to stderr and the report to stdout.
It exits 0 when the run meets the target, 1 when it misses it or stops, as on an auth failure, a rate limit, a timeout or a rejected model, which judge nothing, and 2 when it refuses to run, as on any usage error.
One run is one sample of a model's answers, and the rule and knowledge histories run offline only.

To keep a run's answers, give it `--record` with a file that does not exist yet, in a folder that does, and `--pages` with a folder that is new or empty:

```sh
mkdir -p ~/eva-live/run-1
go run ./cmd/live-accuracy --host claude-code --spend-model-quota --record ~/eva-live/run-1/answers.jsonl --pages ~/eva-live/run-1/pages
go run ./cmd/live-accuracy --host claude-code --replay ~/eva-live/run-1/answers.jsonl --pages ~/eva-live/run-1/pages-again
```

The recording is one JSON line a pass, written as the run goes: the host, the model asked for, the effort the answer came at, the script and pass, the answer's JSON or null, and the runner's reason, and nothing of the machine, the account or a path; it does hold the model's name and its answers to the suite's made-up chats.
`--replay` answers each pass from it, calls nothing and needs no `--spend-model-quota`; given the run's `--effort` and `--model`, it prints the report the run printed and exits as it did.
The recording keeps the effort each answer came at, not the one the run asked for, so a replay without `--effort` says the effort is `as recorded` in the report's first line.
It refuses, before anything runs and with exit 2, a recording of another host, model or format version, one with a reason the update does not know, a line with a key missing, added, given twice or of another type, null among them, and one that lacks a pass of the suite or holds one twice, as the recording of a run that stopped part way lacks the passes after the stop.
`--pages` writes each script's page after its last pass, as `v1.NN.md`, the pages the evaluation harness's `eva-eval fidelity --pages <dir> --out <dir>` scores into v1's fidelity rows; a replay writes the same pages byte for byte.
A page that cannot be written stops the run before its next call, and the run exits 1.

To keep what each pass cost and what the host printed, give it `--trace` with a folder that is new or empty, outside the checkout, and to have a pass that times out counted as a refused answer, where it would stop the run, `--timeout-refused`. This is a run of its own, so it gets a folder of its own:

```sh
mkdir -p ~/eva-live/run-2
go run -buildvcs=true ./cmd/live-accuracy --host claude-code --spend-model-quota --timeout-refused --record ~/eva-live/run-2/answers.jsonl --trace ~/eva-live/run-2/trace
go run ./cmd/live-accuracy --host claude-code --replay ~/eva-live/run-2/answers.jsonl --timeout-refused
```

Keep the recording, the pages and the trace outside the checkout, as in both examples, a folder a run: `go run` runs from it, and a file left in it is one `git add` from a commit and marks every later `-buildvcs=true` build as built from a tree with changes, so the next run's `run.json` says `dirty` on an unchanged tree.

The trace is written as the run goes, each line synced.
`run.json` holds the host, the effort and model asked for, the version of the CLI the run starts, as the host adapter reads it, eva-md's commit and whether its tree had changes, which a build records only with `-buildvcs=true` (`go run` leaves the commit empty without it), whether `--timeout-refused` was given, and the run's UTC date.
`passes.jsonl` has a line a pass the runner answered: the call's milliseconds, the tokens in, cached ones included, and out, the attempts, the effort the answer came at, the model the host says answered, the model note, the degraded note, the runner's reason, and why the run counts the answer refused, when it does: the runner's reason, `timeout` under `--timeout-refused`, or the validator's error.
`raw/` holds what the CLI printed on stdout and stderr on each attempt, as `SS-PP-AA.out` and `.err` for script, pass and attempt.
`end.json`, written last, also when the run stops or is interrupted, says whether the run met the target, missed it or stopped, why it stopped, as a code (the runner's reason, such as `rate_limit` or `timeout`, `model_fallback` for an answer from the host's default model in place of the `--model`
asked for, `interrupted`, or `error` for anything else, which stderr names), and how many passes it traced, refused and timed out.
The passes it traced are the passes the run counts, but for an answer from the host's default model: the run stops on it, and its line is still written, for its model note.
A run that stops keeps the trace of every pass up to and with the one that stopped it.
A trace that cannot be made leaves nothing of the trace or the recording behind.
A pages folder that cannot be made after it takes the trace away again, but leaves the recording, an empty file: remove it before the next run, which wants a file that does not exist yet.
Its JSON files hold no path, no time of day and nothing of the machine, but the trace holds model names, host, session and thread ids and the host's raw output, which can name a plan, a rate limit's reset time, the model's reasoning, and paths under your home folder, which name your account: hand it over privately, and never commit it as written.
Its folder, even one that was there, and every file in it are private to you.
`--trace` is refused, before anything runs and with exit 2, for a folder that holds anything, is or lies in the pages folder, holds the recording, or lies in the git work tree it is run from, and with `--replay`, which calls nothing.
With `--timeout-refused` the run prints the existing timeout line on stderr, counts the pass refused and goes on, and its report says `answers timed out: n` after `answers refused`; the recording and the trace keep the pass's `timeout`, and `--replay` with the flag counts a recorded timeout the same way, so it prints the run's report and writes its pages.
Without the flag a run, or a replay, stops on a timeout.

<!-- /v1 -->

## Must keep

The must-keep items of [findings.md](../v2/findings.md) this component touches, numbered from K01 in each list, and the tests that hold them.

| Id | Item | Tests |
| --- | --- | --- |
| EVL-K03 | Canned and live answers go through one loop over the production path | internal/update:TestAccuracyLiveRun, internal/update/accuracy/live:TestRunStops |
| EVL-K05 | A refused answer costs, and host failures or a model fallback stop the run unjudged | internal/update/accuracy/live:TestARefusedAnswerCountsAgainstTheTarget, internal/update/accuracy/live:TestARefusalLeavesTheGuardUnjudged, internal/update/accuracy/live:TestRunCountsRefusedAnswers, cmd/live-accuracy:TestARejectedModelStopsTheRun, cmd/live-accuracy:TestAFailureStopsTheRun |
| EVL-K06 | The live run needs its flags, states its calls first, uses a temporary home, and no release builds it | cmd/live-accuracy:TestRefusals, cmd/live-accuracy:TestNoReleaseBuildsIt, cmd/live-accuracy:TestOnlyItImportsTheSuite, cmd/live-accuracy:TestRun |

## Since the split

**2026-11-14, ST-04b: the recording goes with a pages folder that cannot be made.**
A pages folder that cannot be made takes the trace and the recording away too, with a trace or without one, so the next run can make the recording again (`TestATraceThatCannotBeMade`).
The blocks above still say that it leaves the recording, an empty file, to be removed before the next run: that is no longer so.
A run whose answers meet the target exits 0 with every page written, and 1 when only the last script's page cannot be written, its report printed and the page named on stderr (`TestTheLastPageOfARunThatMeetsTheTarget`).
