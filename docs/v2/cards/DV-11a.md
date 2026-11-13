# DV-11a: the runner's interface, typed outcomes, the probe and the sealed run

**Plan:** 6.3, design 8; section 8, rows DV-11 and IF-01 (its interface and fake); section 9; 13.1, 13.2. **Design:** `derive.md` 3.6, 8.1, 10.1 and row DV-11; `audit/update-model.md` 3 (must-keeps 1 to 10), F3, F7, M1, M2. **Part of:** DV-11, first; then [DV-11b](DV-11b.md) (Codex) and [DV-11c](DV-11c.md) (Claude Code) side by side. **Size:** M. **Reviewers:** two (a run carries a person's chat). **Depends on:** HO-10a (merged).

## Why

v1's runner (`internal/update/runner.go`) names failures from English phrases, blames the chat for what it can't name, never reads the CLI's version and keeps at most one model the host names. v2's says what happened in fixed words, with the version and every model named. DV-22, DV-24, EV-20 build on it.

## What to build

A new package `internal/derive/modelrun` (organizing's, plan section 8), its contract in `SPEC.md`, no caller: its non-test files import from eva-md only `internal/host/adapter`. The binary is unchanged; no switch.

1. **The interface**, `runner.go`: `Runner{Probe(ctx) (Caps, error); Run(ctx, Call) (Outcome, error)}`; `New(id adapter.ID, o Options) (Runner, error)`, an error for a host without a backend; `Options{CLI string; Env adapter.Env}`: the recorded CLI, and what probes run through.
2. **`Call`**: `Task` (`extract`, `repair`, `review`, `livecheck`), `System`, `Input` (stdin only), `Schema`, `Model` and `Effort` (empty: no flag), `Deadline` (for all attempts; 0 is 600 s), `RunDir`, `IgnoreUserConfig`. `Run` refuses with an error, before any process starts, what v1's `check` and `checkRunDir` refuse, an effort not in `Caps.Efforts`, and another task.
3. **`Outcome`**: `Class`, derive 8.1's eight words as constants (`ClassOK`, `ClassAuth`, `ClassRateLimit`, `ClassHost`, `ClassTimeout`, `ClassAnswerInvalid`, `ClassToolUsed`, `ClassCLIMissing`); `Answer`; `ModelAsked`, `ModelAnswered`, `ModelsSeen`; `Effort`; `Refused` ("model", "effort" or ""); `CLI`, `CLIVersion`; `Billing` ("plan"); `Usage{In, CachedIn, Out int64}` (`In` uncached, as DV-13's `cost.Usage`) and `UsageSeen`; `Quota{Known; RemainingPct; ResetAt}`, unknown for now; `Signals`, each a constant of the package, never host text; `Attempts`. A failed run is an `Outcome`, nil error; an error: the call could not run, or `ctx` ended.
4. **What `Run` enforces on every backend**: `ok` only with no tool sign and an `Answer` that is one JSON object; a tool sign is `tool_used` beside any answer; `Answer` nil unless `ok`; `ModelAnswered` and `ModelsSeen` hold only names the host gave that pass v1's `modelName`, never a copy of `ModelAsked`, and no `ModelAnswered` adds the signal `model_unreported`; `CLIVersion` is the probe's on all but `cli_missing`; usage seen before a timeout is kept. Nothing else of the host's output reaches the `Outcome`.
5. **The probe**, `probe.go`: `adapter.Probe`, through an `Env` whose `LookPath` gives the recorded CLI while it is an executable of the CLI's name (v1's `cliFor`), else PATH's. `Caps{Host, CLI, Version, Known, Untested, Efforts, Features}`, cached by path, size, modification time and version; a backend's probe step runs once per entry, under its own 15 s limit (v1's for `codex features list`). No model call.
6. **The sealed run**, `process.go`: a port of `internal/update/process.go`, keeping must-keeps 1, 2, 5, 6, 9 and 10. Its limits and the probe step's are variables its `TestMain` raises; a test can hold either timer (v1's `runTimer`, `featuresTimer`).
7. **Backends** register from their files with `register(id adapter.ID, b backend)`; the `backend` type (efforts, a probe step, one attempt) is defined here and in `SPEC.md`. Here, tests reach a test backend through an unexported constructor, never `register`, so none clashes with b or c.
8. **`modelrun.Fake`**, in `fake.go`, not a test file (as `update.Fake`): records copies of calls, answers with `Respond` or `Outcome` and `Err`, gives `Caps` to `Probe`; safe for concurrent use.
9. **The rig**, `fakecli_test.go`: v1's `fakeScript` and helpers, ported for b and c to use unchanged. Tests always set `Options.Env` (`adapter.Probe`'s limit can't be raised here); one that reads a version probes its own link of the script (`adapter.Probe` caches by path).

Not here: v1's update put behind this runner with a switch (a later part, after DV-13 and the counters release); the quota and flags and efforts read from the CLI ([host-facts.md](../host-facts.md) F-058, F-057, F-112, F-167), on DV-02; the policy table (DV-03, DV-13); the API-key backend (plan section 12). Report the words for `AGENTS.md`'s list of v2 packages.

## Owns and may touch

All new: the core, rig and spec.

```paths
internal/derive/modelrun/SPEC.md
internal/derive/modelrun/runner.go
internal/derive/modelrun/runner_test.go
internal/derive/modelrun/probe.go
internal/derive/modelrun/probe_test.go
internal/derive/modelrun/process.go
internal/derive/modelrun/process_test.go
internal/derive/modelrun/fake.go
internal/derive/modelrun/fake_test.go
internal/derive/modelrun/fakecli_test.go
internal/derive/modelrun/imports_test.go
```

## Must not touch

`internal/update/**`, `internal/host/adapter/**` (its literal baseline too), `internal/host/hostfake/**`, `cmd/**`. No host name as a literal outside tests: use `adapter.ByID`.

## Accepted when

- `scripts/linux-check.sh` passes; no existing file changed.
- v1's `runner_test.go` tests of what is kept pass, ported to a test backend running the rig's script: environment, stdin, timeouts, the group killed, cancel, a missing CLI, the recorded path (after `filepath.EvalSymlinks`), refused calls, capped output. New: a child that exits 0 leaving a grandchild holding its output is not failed (a short output wait given to that run alone).
- Each rule of item 4 has a test; a marker planted in an event type, an item type and an error text reaches no field item 4 does not allow.
- A second `Probe` of an unchanged file runs no command (counted in `Env.Run`); after `os.Chtimes` it runs again and the next `CLIVersion` is new.
- `imports_test.go`: non-test files import from eva-md only `internal/host/adapter`; no non-test file outside the package imports it. `TestHostLiteralCount` passes, baseline unchanged.
- Each mutation is caught: a key added to the allowlist, input as an argument, the group not killed, `Wait`'s error as the exit, mtime out of the cache key, `ModelAsked` copied, a tool sign dropped beside a valid answer.
