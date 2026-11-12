# EV-03b: the pilot's driver

**Plan:** section 8, Stage 0 row EV-03 and its "Needs from you"; decisions 16, 28. **Design:** `docs/v2/working-papers/design/evaluate.md` 4.3 to 4.5, 5.3, row EV-03; `docs/v2/host-facts.md`, the facts kept for EV-03. **Size:** M. **Reviewers:** two: the contract, and one who tries to start a session unasked, pass a ceiling or leak a key. **Depends on:** EV-03a, EV-12 (merged).

## Why

EV-03a's tasks run under three conditions on real hosts in containers billed to evaluation keys (decision 28); tests drive a fake CLI given by path. Throwaway: in no release, deleted when EV-17 lands. eva-md is unchanged: no switch.

## What to build

1. **`eva-eval pilot run --cli path ... --eva-md path --work dir --out dir --trials n --max-sessions n --max-tokens n [--tasks dir] [--task id ...] [--repo dir] [--session-timeout d] [--pass-env NAME ...] [--seed n] --spend-model-quota`**, in `cmd/eva-eval/pilot_run.go`, added to EV-03a's `pilot` map. A `--cli` is an absolute path named as a host's `Names.CLI`, its version read by `adapter.Probe` with an `Env.Run` the driver gives and bounds (tests cannot raise Probe's 5 seconds). Without `--spend-model-quota` it prints the plan (sessions, updates, ceilings) and exits 2, starting nothing, as live-accuracy does. So does a usage error: a ceiling missing, a relative path, an unknown CLI, a `--pass-env` with `=`, a `--work` not empty or, links resolved, in the invoking home, this checkout, the tasks, `/tmp` or `/var/tmp` (which `tempPath` in `internal/hook/hook.go` skips).
2. **A trial** gets a folder under `--work` with its own `HOME` (a made-up git identity), `TMPDIR` and copies of the repositories, each committed once. `off`: nothing added. `oracle`: the note, a blank line, the prompt. `eva`, v1 from `--eva-md`, a release-way build:
   - `eva-md init --remote <a local bare repository> --dir <folder> --no-push`; `eva-md install --host <InstallName>` (not setup: macOS only);
   - the config's `updates` set false by `config.Load` and `config.Save`, so no hook starts an update (`queueJob`, `internal/hook/trigger.go`);
   - the history fed to `eva-md hook --host <id>` from `workdir`, one `payload` of `internal/hook/hook.go` a call: `SessionStart`, `UserPromptSubmit` per user turn, `Stop` with `last_assistant_message` per agent turn, `SessionEnd`;
   - `updates` true, `eva-md update --session <id>:<session> --foreground` timed, `updates` false again.

   Session B is the host's headless command in `workdir`, bounded by `max_turns` and `--session-timeout` (else the task's `minutes`), past which its process group is killed and the trial unjudged. While `Grade` fails and rounds remain, the same session gets one prompt naming the failing tests and stating each fact they encode not yet supplied; each fact so supplied is a correction.
3. **`hosts.go`**, a table by adapter id, each entry naming its host fact: the headless command with a turn bound and machine-readable output (F-068), the permission it needs to edit and run tests, the resume (F-069), and a reader of the session id, tool calls, tokens and the model that answered. Usage is read as v1's update reads it (`claudeTokens`, `codexStream.usage` in `internal/update`); prompt tokens include cached ones.
4. **Ceilings.** A session is session B with its resumes. None starts past `--max-sessions`, nor once the tokens of sessions and updates reach `--max-tokens`. A stream with no usage stops the run, the rest unjudged.
5. **Keys.** The driver holds, reads and writes no secret. A child gets `HOME`, `TMPDIR`, `PATH` (the CLI's and `--eva-md`'s folders, then the invoking `PATH`), `GIT_CONFIG_NOSYSTEM=1` and only the variables `--pass-env` names. A key reaches the CLI from the container's environment or proxy.
6. **Rows** through `results.Store`, `Forbid` the union of `task.Forbid`: instrument `pilot`, scenario `<task>.<arm>.t<n>`, host and version, the model that answered when it is a results id, the seed; commit and dirty from `results.Source` on `--repo`. Metrics: `pilot.solved`, `pilot.first_try` (`ratio`); `.corrections`, `.agent_events` (tool calls), `.hook_seen` (session B's events that `inboxv1.Backend` reads in the trial's inbox; `eva` only), `.peeked` (tool calls naming the tasks folder) (`count`); `.prompt_tokens`, `.cached_tokens`, `.output_tokens`, `.update_tokens` (`tokens_in` plus `tokens_out` in the trial's `~/.local/state/eva-md/updates/`) (`tokens`); `.wall_ms`, `.update_ms` (`ms`); `pilot.unjudged.sign_in`, `.rate_limit`, `.timeout`, `.no_usage` (`count`). `eval/pilot/metrics.json` lists each, with paths `internal/eval/pilot` and `eval/pilot/testdata`.

## Owns and may touch

```paths
internal/eval/pilot/run/**
eval/pilot/metrics.json
cmd/eva-eval/pilot_run.go
cmd/eva-eval/pilot_run_test.go
```

## Must not touch

Everything else; `internal/eval/pilot/task`, `cmd/eva-eval/pilot.go` and `main.go` are read only. `eval/pilot/README.md` is EV-03c's; the lead fills `docs/v2/host-facts.md`.

## Accepted when

- `scripts/linux-check.sh` passes; `TestNoReleaseContainsIt` and `TestHostLiteralCount` pass unchanged; the package's `TestMain` calls `testgitenv.Clear()`.
- Without the flag, and on each usage error, it exits 2, and a CLI that marks a file when run marks nothing.
- The package's fake CLI re-runs the test binary, answers `--version`, writes each host's stream with made-up usage, and lays `solution/` when a script passed by `--pass-env` says. On it and a fake eva-md, two of EV-03a's tasks, three arms, two trials and both hosts give exactly the expected rows, that `results.Load` and the catalogue's `Check` accept. No two trials share a home, none is the test's; `off` and `oracle` homes hold nothing of eva-md; `eva`'s calls come in item 2's order; only `oracle`'s prompt has the note.
- Each ceiling stops at its exact count; a stalled fake and its children are killed at a `--session-timeout` given to that run, under a 30-second hang guard; a fake that passes after one correction gives `solved` 1, `first_try` 0, `corrections` 1.
- A passed variable's made-up value is in no row, output or file the driver writes.
- With eva-md built from this checkout with `-tags evamd_testlimits` (raised hook limits), `init` and each task's history fed for each host into a new home make `eva-md brief --cwd <workdir> --host <id> --as startup` print text of that history.
- Mutations (no flag check, a ceiling off by one, the note given to `eva`, a shared home, a key printed) fail tests.
