# The model runner: its interface, typed outcomes, the probe and the sealed run

The contract of `internal/derive/modelrun` (plan 6.3, design 8; section 8, rows DV-11 and IF-01; derive 3.6, 8.1 and 10.1; audit/update-model must-keeps 1 to 10, F3, F7, M1 and M2). v1's runner (`internal/update/runner.go`) names failures from English phrases, blames the chat for what it can't name, never reads the CLI's version and keeps at most one model the host names. This one says what happened in fixed words, with the CLI's version and every model the host named. Of the host's own text it lets through only an answer that is one JSON object and the names a backend read as models, which the core checks by their shape alone (section 4).

Nothing calls it yet, and nothing outside the package imports it, so it is built into no binary and no hook runs it. Its code imports, from eva-md, only `internal/host/adapter`, and no module. It names no host: a host is an `adapter.ID`, and its CLI's name comes from `adapter.ByID`. A host's backend lives in a file of its own: `codex.go` (DV-11b) and `claude.go` (DV-11c).

## 1. The interface

```go
type Runner interface {
    Probe(ctx context.Context) (Caps, error)
    Run(ctx context.Context, c Call) (Outcome, error)
}
func New(id adapter.ID, o Options) (Runner, error)
type Options struct {
    CLI string      // the CLI's absolute path, as install recorded it
    Env adapter.Env // what probes run through
}
```

`New` gives an error for a host with no backend, or one the adapter lacks. `Probe` calls no model. `Run` makes one call. A failed run is an `Outcome` and a nil error; an error means the call could not run (section 2), or `ctx` ended, before or during the run. No error holds a path, an argument, the input or anything the CLI printed: the run folder is never named, and a CLI that could not start is named by its base name. The core enforces this for its backends too: of an error an attempt gives, `Run` passes on only the core's own (a CLI that could not start, or a run folder the child could not enter), without what a backend wrapped around it, and gives any other as one fixed error, `modelrun: the call could not run`.

## 2. A call, and what is refused

| Field | Meaning |
| --- | --- |
| `Task` | `extract`, `repair`, `review` or `livecheck` |
| `System` | the fixed instructions |
| `Input` | the input, on stdin only, never in an argument |
| `Schema` | the answer's JSON schema, a JSON object |
| `Model`, `Effort` | empty: no flag, the host's default. An effort is one of `Caps.Efforts` |
| `Deadline` | one deadline for all the call's attempts; 0 is 600 s |
| `RunDir` | an existing, empty folder, by absolute path: the child's working folder |
| `IgnoreUserConfig` | passed to the backend |

`Run` refuses, with an error and before any process starts (the probe's `--version` included), what v1's `check` and `checkRunDir` refuse: an empty or blank input; empty or blank instructions, or instructions that start with `-` or hold a NUL byte; a model name a CLI could read as another flag (it must match v1's `modelName`, `^[A-Za-z0-9][A-Za-z0-9._:/@\[\]-]{0,199}$`); a schema that is not one JSON object, by the rule an answer is held to (section 4.2), so one that gives a key twice is refused too; a negative deadline; a run folder that is relative, missing, not a folder or not empty. It also refuses a task not in the list above, and an effort not in the backend's list (v1's `highest` is not one). An effort is checked against the backend's own list, which `Caps.Efforts` copies, so no probe runs first. A run folder that is gone, or that the child can't enter, when the CLI starts, as one removed after the check, is an error too, never a missing CLI.

## 3. What a call comes to

| Field | Meaning |
| --- | --- |
| `Class` | one of derive 8.1's eight words: `ClassOK`, `ClassAuth`, `ClassRateLimit`, `ClassHost`, `ClassTimeout`, `ClassAnswerInvalid`, `ClassToolUsed`, `ClassCLIMissing` |
| `Answer` | one JSON object in UTF-8, trimmed of space; nil unless `ok` |
| `ModelAsked` | the call's `Model` |
| `ModelAnswered` | the model the host says answered; "" when it says none |
| `ModelsSeen` | every model the host named, sorted, each once, at most 64 (section 4.3) |
| `Effort` | the call's `Effort` |
| `Refused` | `model`, `effort` or ""; set only on `host` |
| `CLI`, `CLIVersion` | the CLI's path and the probe's version; both "" on `cli_missing` |
| `Billing` | `plan`: the host's own sign-in, the only billing so far |
| `Usage{In, CachedIn, Out int64}` | over every attempt: `In` the uncached input (as DV-13's `cost.Usage`), `CachedIn` the cached input, `Out` the output |
| `UsageSeen` | the host reported any usage |
| `Quota{Known, RemainingPct, ResetAt}` | unknown for now: `Known` is false |
| `Signals` | each a `Signal` constant of the package, never host text |
| `Attempts` | the CLI processes the call started; the probe's are not counted |

`CLI` is a path, and may hold the user's name: a caller that writes an `Outcome` where paths are not kept, such as the worker's journal, leaves it out.

## 4. What Run enforces on every backend

A backend reads one attempt into an internal `result`. `Run` builds the `Outcome` from it and from what the processes did, and nothing else of the host's output reaches the `Outcome`:

1. **The class**, in this order: a CLI that was not there, at the probe or at a start, is `cli_missing`; any sign of a tool in any attempt (a backend's tool sign, or its class `tool_used`) is `tool_used`, beside any answer and whatever the class read; a process whose time limit ended, or whose time ran out before it started, is `timeout`; else the backend's class, when it is `ok`, `auth`, `rate_limit`, `host` or `answer_invalid`. `cli_missing` and `timeout` come from the process alone: a backend that says either, or any other word, gives `host`.
2. **`ok` needs one JSON object.** An `ok` whose answer is not exactly one JSON object, space around it aside, or is not valid UTF-8 throughout (which `json.Valid` lets pass inside a string), is `answer_invalid`. So is an answer in which any object, at any depth, gives a key twice (which `json.Valid` lets pass too), since a reader that keeps the first value and one that keeps the last would take two answers from one outcome: keys are compared as `encoding/json` decodes them, escapes undone, and two that differ in letter case are two keys, as is the same key in two objects. The answer is read once, with `encoding/json`'s token reader, to that reader's own limit of 10,000 levels and no deeper, on a stack of the core's own and with no recursion, so its work and memory grow with its size alone and no answer can exhaust the stack. Each backend gives such an answer as `answer_invalid` already, on a clean run, as it does any answer that is not one JSON object. `Answer` is nil on every other class, whatever the backend kept: an answer printed before a timeout does not count (must-keep 8), nor one beside a tool. The answer kept is a copy.
3. **Models are the host's.** `ModelAnswered` and `ModelsSeen` hold only names the backend read from the host that match `modelName`; `ModelAnswered` is also in `ModelsSeen`. `ModelsSeen` holds each name once and at most 64 names, whatever a backend gave, since the core does not lean on a backend's own bound: `ModelAnswered` first, then the others in the order the attempts first gave them, until 64 are kept; the rest are dropped, and the 64 are then sorted. Repeats are found with a set, so the work grows with the names given, not with their square, after the CLI has exited, where no deadline stops it. Neither is ever filled from `ModelAsked`; the host may name that model, and then it is kept as the host's word. A call that started a CLI and has no `ModelAnswered` gets the signal `model_unreported`. `modelName` is the core's only check here, and it bounds a name's shape, not where the name came from: an event type, an item type, an e-mail address, a path, a branch or an id with no space in it has a model name's shape, and reaches these two fields when a backend reads it as a model. That a backend fills `models` and `answered` only from the host's model fields is each backend's own guarantee. Each backend's tests therefore plant markers of a model name's shape (such as `planted-marker-41`, an e-mail address, a path and a UUID) in an event type, an item type and an error text, and show that none reaches any field, these two included; this package's test, whose puppet reads event and item types as models, records the limit.
4. **Signals are constants.** A backend declares its signals when it registers; `Run` keeps, of what a backend gives, only those, each once, in the order first given, then adds its own. A backend may not declare the core's (`model_unreported`).
5. **`Refused`** is kept only on `host`, and only when it is `model` or `effort`.
6. **The version** is the probe's on every class but `cli_missing`.
7. **Usage** is summed over the attempts, each count of an attempt held between 0 and 2^40, and is kept on every class, a timeout included (M1): a killed run reports what it saw.

## 5. The probe

`Probe` finds the CLI through `adapter.Probe`, with an `Env` whose `LookPath` gives the CLI install recorded (`Options.CLI`) while it is the absolute path of an executable regular file of the CLI's name (v1's `cliFor`), and otherwise the `Env`'s own `LookPath`, or `exec.LookPath` when that is nil. The adapter resolves the CLI's folder's symlinks and reads `--version` through `Env.Run`, under its own 5 s limit, which this package can't raise. When `Env.Run` is nil the probe gives the adapter a sealed run (section 6) in a private folder made for it and removed after, with nothing on stdin, so the CLI is never started with more of the environment than a call gets. The adapter keeps the version by the CLI's path, size and modification time.

`Caps{Host, CLI, Version, Known, Untested, Efforts, Features}`: `Known` and `Untested` are the adapter's, `Efforts` a copy of the backend's list, lowest first, and `Features` what the backend's probe step found, of a feature name's shape (`^[a-z][a-z0-9_.-]{0,63}$`): the host's other text is dropped. The probe step runs once per cache entry: the host, the CLI's path, size, modification time and version. It runs under its own limit, 15 s (v1's for `codex features list`), through `Env.Run` when that is set, and otherwise as that sealed run. What it prints past 64 KiB, or a run that fails, is an error that holds no text. A step whose backend says its list may not be kept (`ok` false) is not cached, so the next probe runs it again. Probes of one entry that overlap run the step once: the later waits for the earlier. The cache is the process's, shared by every runner.

`Probe` returns an error only when `ctx` has ended. `Run` probes first, so a call whose CLI changed reads the new version and features. A call with no CLI is `cli_missing` with no attempt.

## 6. The sealed run

`process.go` is a port of v1's `internal/update/process.go` and keeps its must-keeps:

| Must-keep | Here | Held by |
| --- | --- | --- |
| 1. The input on stdin, never in an argument | the backend's attempt gets `run(ctx, args, stdin, out)`; `Run` passes `Input` only as stdin | `TestInputGoesOnStdinOnly` |
| 2. An empty run folder, checked; an allowlisted environment with `EVA_MD_INNER=1`, API keys dropped | `checkRunDir`; `childEnv` keeps v1's 21 names and every `LC_*`, and adds `TERM=dumb`, `EVA_MD_INNER=1` and the variable that turns one host's own memory off | `TestCallsThatCannotRunAreRefused`, `TestSpawnOfAMissingCLI`, `TestChildEnvironmentIsAnAllowlist`, `TestChildGetsOnlyTheClearedEnvironment`, `TestTheProbeStepWithNoEnvRunIsSealed`, `TestTheVersionWithNoEnvRunIsSealed` |
| 5. The whole process group killed on timeout or cancel, and again once the CLI exits; capped writers | `Setpgid`, `cmd.Cancel` and `killGroup`; stderr capped at 64 KiB | `TestTimeoutKillsTheChildAndItsGroup`, `TestCanceledRunIsAnErrorAndEndsTheChild`, `TestACleanExitThatLeavesAGrandchildIsNotFailed`, `TestOutputIsCapped` |
| 6. The exit status from the process state, not `Wait`'s error | `cmd.ProcessState.Success()` | `TestACleanExitThatLeavesAGrandchildIsNotFailed` |
| 9. One deadline for all attempts; a retry that runs out before its CLI starts is a timeout, not an error | `Run`'s `end`, which bounds every run of the call; `spawn` on a limit already ended | `TestAttemptsShareTheDeadline`, `TestEveryRunGetsWhatIsLeftOfTheDeadline`, `TestTimeoutBeforeTheCLIStartsIsATimeout`, `TestTimeoutEndsTheRunAtItsDeadline` |
| 10. The recorded absolute CLI path, its folder added to the end of the child's `PATH` | the probe's `LookPath`; `withDir` when the recorded CLI is the one used | `TestRecordedCLIPathIsPreferred`, `TestRecordedCLI`, `TestWithDirAddsTheCLIsFolderOnce` |

Must-keeps 3, 4, 7, 8 and 11 are the backends' (the tool flags and reading a tool sign, no transcript left, the Codex answer file, Claude's refused `--strict-mcp-config`); `Run`'s class order holds 3 and 8 for every backend whatever it reads.

The time limits are variables `TestMain` raises before any test runs: `waitDelay` (2 s: the wait for output pipes once the CLI has exited) and `probeStepTimeout` (15 s). Their timers, `runTimer` and `probeStepTimer`, are variables too, which the tests replace with timers a test holds and ends itself. A test that needs a short output wait gives it to one runner, `runner.waitDelay`, never to the package. Two more variables are seams for tests, which a test that replaces one restores: `probeCLI`, the adapter's probe, so a test can see the `Env` the adapter is given, and `stepWaits`, called when a probe starts to wait for a step another probe runs, so a test of the overlap needs no time window.

## 7. Backends

A backend registers from its own file, in an `init` or a package variable, with `register(id, backend)`. A host the adapter lacks, a second backend for a host, a backend with no `attempt`, a signal that is not a name of lower-case letters, digits and `_` (at most 64) or is the core's, or an effort a model name could not be, panics.

```go
type backend struct {
    efforts []string // the effort flag's values, lowest first
    signals []Signal // what its results may carry
    probe   func(ctx context.Context, run probeRun) (features []string, ok bool) // nil: none
    attempt func(ctx context.Context, a *attempt) (result, error)
}
type probeRun func(ctx context.Context, args ...string) ([]byte, error)
type attempt struct {
    call Call
    caps Caps
    n    int // 1, or 2 for the retry a first result asked for
    run  func(ctx context.Context, args []string, stdin []byte, out io.Writer) (exit, error)
}
type exit struct{ started, missing, failed, timedOut bool; stderr []byte }
type result struct {
    class     Class
    answer    []byte
    refused   string
    models    []string // every model the host named
    answered  string   // the one it says answered
    usage     Usage
    usageSeen bool
    toolSign  bool
    signals   []Signal
    again     bool // one more attempt, under the same deadline
}
```

`attempt.run` starts `caps.CLI` in the call's run folder, sealed (section 6), under what is left of the deadline: the whole deadline for the call's first run, and for every later one, in the same attempt or the next, what is left of it; a run with none left starts nothing and makes the call a `timeout`. So a backend that starts the CLI more than once in an attempt still ends by the call's deadline. Its `ctx` may be a child of the attempt's, so a backend can stop a run part way (DV-11b stops Codex at the first line that decides). `exit.stderr` holds the first 64 KiB of stderr, for the backend to classify; the `out` a backend gives must not block, and should be capped. An error from `attempt` ends the call. `Run` passes on only an error the core made (`run`'s: a CLI that could not start, or a run folder the child could not enter), found with `errors.As` and given without the backend's wrapping, and gives any other as `modelrun: the call could not run`, so a backend's own error, which may hold a path or the input, never reaches a caller.

A call makes at most two attempts. A result with `again` set gets a second, with `n` 2, unless the call's time has run out (then it is `timeout`), a tool was seen, the CLI was missing or the attempt timed out. The second attempt's result is the call's, whatever it asks. There is no effort ladder and no model fallback: a refused model or effort is `host` with `Refused` set, and the policy decides what to try next.

**The Codex backend** (`codex.go`) reads the stream of events as it arrives and stops the run at the first line that decides. Its signals are `codex_line_skipped`, for a line that is not JSON and holds no brace, which is skipped, and `codex_mcp_optional_failed`, for an optional MCP server that failed beside a valid answer. A tool item is `tool_used`, and so is a tool item's type at any depth of a line, in any letter case. These are `host`, since the reader cannot vouch for them: an event or item type it does not know, a line of JSON that is not an object, a key given twice, a second value on a line, a `type` key anywhere but on the event and its item or in another letter case, a key only a tool item carries outside one, and nesting past 10,000 levels, where the scan stops, so no line can exhaust the stack. An `error` event with a clean exit and no answer is named from its text, and is `host` when no term matches, never `answer_invalid`: a host's limit is not the chat's fault. A clean exit after which the reader has read no known event is `host` too, with an answer or without, since a reader that saw nothing can vouch for nothing; a failed exit with no event is still named from its error text, as a CLI that fails before its first event prints none. The model's own words never name a failure.

**The Claude Code backend** (`claude.go`) reads the single envelope `--output-format json` prints, field by field with `encoding/json`, whose limit of 10,000 levels bounds every value; stdout past 8 MiB is no envelope. Its signals are `claude_strict_mcp_refused`, for a refused `--strict-mcp-config`, retried once without it under the call's one deadline; `claude_several_models`, for a `modelUsage` that names two models or more; and `claude_model_unnamed`, for a `modelUsage` key that is no model name, an output count it cannot read, a `modelUsage` that is not an object, or one that names more than 64 models. Two things are tool signs, beside any answer and on a timeout too: `permission_denials`, under its key in any letter case, given once or twice, holding anything but `null` or an empty list; and a count of tools the API ran itself (`server_tool_use`, `web_search_requests` and `web_fetch_requests` in `usage`, `webSearchRequests`, `webFetchRequests` and `serverToolUse` in a model's entry of `modelUsage`, under their keys in any letter case) holding anything but `null`, 0 or an object of those. `ModelsSeen` is every key of `modelUsage` of a model name's shape; `ModelAnswered` the key with the most output tokens, the first by name on a tie, and none when that key is no model name or any key's output count cannot be read. These are `host`: anything but one JSON object whose type is `result`, and an envelope with a key given twice, in it, its `modelUsage` or an object of counts, a key it reads in another letter case, an `is_error` or `subtype` of another type, or a `modelUsage` that names more than 64 models; such an envelope keeps its tool signs, its usage and the models it names (none past 64), but not the model that answered. Usage is read from `usage` and from `modelUsage`, each under its key in any letter case; a key given twice is read twice and the larger reading kept, count by count. `In` is the input plus the cache writes, `CachedIn` the cache reads and `Out` the output; a `modelUsage` reading adds up its entries, and every sum is capped at 10^12. Each of the three is the larger of `modelUsage`'s sum and `usage`'s reading, so no spend either reports comes out lower, or as zero. A count that is negative, fractional, text, null or past 10^12 adds nothing, as does an entry of `modelUsage` that is not an object or a count under a key in another letter case; `UsageSeen` is true only when some count, a 0 included, was read from either. Output the reader cannot read as one envelope gives no tool sign either: the run is `host` and keeps no answer. Each string of `errors` is read as a line of its own. A field names the class first; error text names it only where no field decides, and stdout that holds a brace is never read as error text, so the model's own words never name a failure. A tool call claude itself runs with permission is seen only once the stream reader is built (host-facts F-113).

An error a backend returns is not passed on: `Run` gives the caller none of its text. The one error `Run` passes on unchanged is the core's own type, `runError`, whose text the core wrote. A backend lives in this package and could make one: it never does, and never wraps its own text, a path or anything the CLI printed in one.

## 8. The fake

`modelrun.Fake`, in `fake.go` and not a test file, so other packages' tests can use it (as v1's `update.Fake`). `Probe` gives a copy of `Caps`. `Run` records a copy of the call, its `Input` and `Schema` cloned, then answers with `Respond` when set, given a copy of its own, and otherwise with a copy of `Outcome` and `Err`. A done `ctx` gets its error, after the call is recorded. `Calls` gives copies of the calls so far, oldest first, each `Input` and `Schema` cloned again. Set the fields before the first call; then it is safe for concurrent use.

## 9. The rig, for the backends' tests

`fakecli_test.go` is v1's fake-CLI rig, ported for DV-11b and DV-11c to use unchanged; it holds no host's output, which each backend's tests write.

- `fakeScript`, written once by `TestMain` into `fakeBin` under each host's CLI name. In `$HOME/fake` (the dump) it records run n's arguments, stdin, environment and working folder, copies `output[.n]` to the file after `-o` and the file after `--output-schema` to `schema.n`, prints `stderr[.n]` and `stdout[.n]`, runs `extra`, and exits with `exit[.n]`. `--version` runs `version.sh`, then prints the file `version`, or `0.1.0 (fake)`. `features list` records `features.args`, runs `features.sh`, and prints `features` or exits 1.
- `isolate`, `fakeCLI`, `put`, `read`, `runs`, `argsOf`, `envOf`; `call(t, id)`, a call with made-up values and no effort; `answer`, `batchMarker`, `hostIDs` and `cliName`.
- `testEnv()`, an `adapter.Env` whose `Run` counts each command by its arguments (`count`, `total`) and runs `--version` with no limit, since the adapter's 5 s can't be raised here; every test sets `Options.Env` through it, or its own. `ownCLI(t, id)` gives a test a link of its own to the script, its path resolved, for a test that reads, or changes, a version, since the adapter and the probe cache by path.
- `holdTimeouts` and `holdProbeStep`, with `timeouts.next` and `expire`, hold the attempts' and the probe step's timers; `launch` and `await`; `startChild`, `waitFile`, `waitGone` and `hangGuard` (one minute), as in v1.

This package's own tests run a test backend, the puppet, which passes on every word it reads, through the unexported `newRunner`, never `register`, so none clashes with a host's backend.

## 10. Not here

v1's update put behind this runner with a switch (a later part, after DV-13 and the counters release); the quota, flags and efforts read from the CLI (host-facts F-058, F-057, F-112, F-167), on DV-02; the policy table of model, effort and deadline per task (DV-03, DV-13); the API-key backend (plan section 12); the breaker and the control rule that act on a class (DV-22).
