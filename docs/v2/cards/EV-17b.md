# EV-17b: the continuation driver, with memory off and the ideal record

**Plan:** section 6.6; the Stage 1 row EV-17; decisions 16 and 28. **Design:** `working-papers/design/evaluate.md` sections 4.3 to 4.5, 5.3 and 9; [host-facts.md](../host-facts.md) F-062, F-067 to F-069, F-115, F-163, F-169. **Size:** M. **Reviewers:** one. **Depends on:** [EV-17a](EV-17a.md).

## Why

The driver runs a task's second chat on a host's CLI, grades it, supplies facts the chat missed, and writes the cost as rows. A fake CLI drives every test; the real one runs in a container (decision 28). eva-md is unchanged: no switch.

## What to build

`internal/eval/continuation/drive` and `eva-eval continuation`.

1. **A trial's folder.** Each trial gets a fresh `<work>/<run>/<task>/<condition>/t<n>/` with `home/`, `tmp/`, `bin/`, `workspace/` (from `task.Prepare`) and `grade/`. `--work`, links resolved, is absolute and outside the invoking home, `/tmp`, `/var/tmp` (the hooks skip them: `tempPath`, `internal/hook`), `--tasks`, `--repos` and `--repo`, or nothing starts.
2. **The host, by path.** `--host` is an id `internal/host/adapter` knows, `--cli` its CLI's path, linked into `bin/` under `Names.CLI`, its version read by `adapter.Probe` with a `LookPath` giving the link. It spells no host name (`TestHostLiteralCount` unchanged).
3. **The environment is built from nothing**: `HOME`, `PATH` (`bin/`, then the invoking PATH), `TMPDIR` (`tmp/`), `GIT_CONFIG_NOSYSTEM=1`, and only the variables `--pass-env` names. `home/.gitconfig` holds a made-up identity, `commit.gpgsign false` and `maintenance.auto false` (setup and the agent commit).
4. **A host table**, `hosts.go`, by adapter id: how to start a headless session (prompt, turn limit, machine-readable output) and continue it; how to turn the host's own memory off and load no plugin; how to load eva-md's plugin from the trial's home (by `--plugin-dir` on Claude Code, F-115; by managed hooks on Codex, F-163; never by writing a trust record); and a reader of its output: session id, agent events, prompt tokens (cached included), cached and output tokens, and the model that answered. Each entry names its host fact; EV-03 and DV-02 confirm them, changing only this file.
5. **Conditions.** `drive.Register(task.Condition, Arm)`. `Arm` has `Check func(Options) error` (`Options`: the run's flags), run before any trial, and `Prepare func(ctx, Trial) (Launch, error)`, run in a trial before its chat. `Trial` gives the task, condition, number, item 1's folders, host, environment and `Options`. `Launch` has `Args` and `Env`, added to the chat's, and `Plugin`, choosing item 4's plugin entry. An `Arm` may also have `After func(ctx, Trial) (Cost, error)`, run when the trial's chat has ended: `Cost{Tokens, Ms int64}` is what the condition spent outside the chat, added to the trial's `Outcome` and to the tokens `--max-tokens` counts. An error `*Unjudged{Code}` leaves the trial unjudged. This part registers `off` and `oracle`; any other is a usage error until registered.
6. **Corrections.** The chat starts with `task.FirstPrompt`, then `task.Grade`. All pass: solved. Otherwise, while rounds remain, the same session gets one prompt, of a fixed form, naming the failing tests and stating each fact they encode not yet supplied. Each fact so supplied is a correction; a round supplying none is a round only.
7. **Asked again.** `Options.Delivered func(Trial, fact string) (bool, error)`: whether the chat was given a fact before the round that supplied it. Nil for v1: nothing measured.
8. **Unjudged**, by a closed list of codes: `sign_in`, `rate_limited`, `timeout` (past `--session-timeout`; its process group killed), `other_model` (not `--model`), `setup_failed`, `update_failed`. Such a trial writes no row; the run counts each code.
9. **Ceilings.** `--max-sessions` and `--max-tokens` have no default. A session is a trial and its follow-ups; tokens are prompt plus output. The plan prints first, refused over `--max-sessions`; no trial starts once tokens reach `--max-tokens`, the rest counted not run. Without `--spend-model-quota` it prints the plan, starts nothing and exits 2.
10. **Rows** through `results.Store`, `Forbid` the union of `task.Forbid`; commit and dirty by `results.Source` on `--repo`; host, version and model from the session.
11. **The command**, `cmd/eva-eval/continuation.go`, with `register`: `eva-eval continuation run --tasks DIR --repos DIR --host ID --cli PATH --condition C --trials N --work DIR --out DIR --max-sessions N --max-tokens N [--model ID] [--session-timeout D] [--pass-env NAME]... [--eva-md PATH] [--repo DIR] [--spend-model-quota]`, and `eva-eval continuation check --tasks DIR --repos DIR` (`task.Check`, a line a task). Output holds ids and figures only.
12. **A fake host**, `drive/fakehost`, only for tests, which call `fakehost.Main()` first in `TestMain`. It re-runs the test binary, follows a script a `--pass-env` variable names, answers `--version`, starts and continues sessions for each host (shapes its own until EV-03 records the real ones), records argv, variable names, folder, prompts, lays `decoy/` over the workspace until the script's facts appear in a prompt and `solution/` after, and can stall until killed, fail sign-in or name another model.

## Owns and may touch

All new. `TestMain` calls `testgitenv.Clear()`.

```paths
internal/eval/continuation/drive/**
cmd/eva-eval/continuation.go
cmd/eva-eval/continuation_test.go
```

## Must not touch

Everything else, `cmd/eva-eval/main.go` among it.

## Accepted when

- `scripts/linux-check.sh` passes; `go list -deps ./cmd/eva-md` names no `internal/eval`.
- For each host, on the fake: a hidden-decision task, under `oracle`, gives `solved` 1, `first_try` 1, `corrections` 0; under `off`, `solved` 1, `first_try` 0, `corrections` 1, `rounds` 2; rows exact but for time.
- The fake's record shows no hidden file in the workspace at any prompt, and exactly item 3's variables; a passed value is in no row, output or log.
- Each `--work` of item 1 is refused, a link into `/tmp` among them, and nothing runs; nothing is written under `/tmp`.
- A stalled session, blocking until killed, is stopped by a `--session-timeout` given that run: `timeout`, no row. No sleep decides it.
- A plan over `--max-sessions` is refused; `--max-tokens` reached after one trial leaves the rest not run; without `--spend-model-quota` the fake records no start.
- An `Arm` whose `Check` fails starts no trial; one whose `Prepare` returns `*Unjudged` writes no row.
- `Delivered` nil gives no `asked_again` row; a stub gives it exact.
- Mutations (a correction per round, a hidden file in the workspace, a variable let through, an unjudged row, a ceiling off by one) are each caught.

## As merged

Merged from `pkg/ev-17b-r1` on 2026-11-13, after a review, one fix round and a verification that accepted it. The review's two major faults: a host's own failure (an overload, a CLI that a signal killed, output with no session) was graded as if the chat had answered, so an outage would have read as a condition doing worse; and no test could tell a killed process group from a killed leader. Both are mended, with a fake that starts a child of its own.

Accepted as built, beyond the card: each round's grading copy, which holds the hidden tests, is deleted before the next prompt; `--pass-env` never passes a variable that points a host or git at a configuration of its own (`ConfigEnv` in the host table, with git's and the XDG ones), nor `SSH_AUTH_SOCK` or `GNUPGHOME`; a passed value of 8 or more letters and digits is a forbidden word of the rows; the prompt follows `--`.

The lead's rulings of 2026-11-13:

1. **A call with no answer leaves its trial unjudged, and the run goes on.** As merged it stops the run. The closed list gains `host_failed` for a host's failure that is no sign-in, rate limit or timeout: an overload, an empty exit, a CLI a signal killed, no session to continue. The run counts it as it counts the others.
2. **A session that names no model is judged.** As merged, with `--model` given, it is `other_model` and unjudged, which would leave every trial of a host that reports no model without a row. `other_model` is a session that names another model. A session that names none is judged, its rows carry no model, and the run counts such sessions.
3. **Codex's managed hooks are written inside the trial's home** until the pilot settles F-163. It is no trust record, and nothing outside the trial is written.
4. The floor of 8 for a passed value, and the two refused variables, stand.

A second round on this card's files, merged the same day from `pkg/ev-17b-r2` after a verification that accepted it, built rulings 1 and 2 (so item 8's closed list now ends with `host_failed`), and with them: a test of a CLI that a signal killed after it had printed an answer; the check of a passed value against what a row will hold, made before the first trial and not at its rows; a relative path in a passed value resolved as the work folder's is; and tests that pass whatever `TMPDIR` is, since `go test ./cmd/eva-eval` with a `TMPDIR` under `/tmp` refuses its own work folder today.

Every entry of the host table is the fake's own shape until the pilot (EV-03) records the real one: the flags of a headless chat and of its continuation, `--` before the prompt, what counts as an answer, how a sign-in failure and a rate limit read, the two configuration variables, and whether Codex reports a model.

Owed elsewhere:

- **Grading can leak a hidden test** (EV-17a's `Grade`): it runs the chat's code beside the hidden files, at a known path inside the trial, so that code can copy them into the workspace before the next prompt, and it uses the invoking build cache. Before a real run, grading moves under another user or into the container, or the workspace is checked for hidden files before each prompt. The tasks folder is kept out of the container's reach. EV-17e's card takes this.
- **For EV-17c:** the fake host answers `--version` and headless chats only; a setup step that runs the CLI's plugin commands needs more, from HO-14's fake or an addition under `drive/**`.

**The pilot runs on this driver** (the lead's ruling of 2026-11-13, in the cards' index): the pilot's ten tasks are exported to this format (EV-03b), the driver gets its guards for a live run (EV-17g), and the v1 condition is EV-17c. So the pilot's sessions are this host table's first real ones, and what they show changes `hosts.go` alone.
