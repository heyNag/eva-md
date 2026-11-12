# EV-17c: the v1 condition: memory built from the earlier chat, as v1 builds it

**Plan:** section 6.6 (the continuation instrument, "with eva-md"); section 8, the Stage 1 row EV-17 ("v1's row recorded"); section 7, the lead's choice of an evaluation-only build tag that lets a scripted session count as a person's chat. **Design:** `docs/v2/working-papers/design/evaluate.md` sections 4.4 (the replay driver and the one evaluation seam) and 4.5 (the `eva` arm); [host-facts.md](../host-facts.md) F-115 and F-163 (settled by EV-03) and F-018 (by the host sittings). **Size:** M. **Reviewers:** one. **Depends on:** [EV-17b](EV-17b.md); [EV-17d](EV-17d.md) for real runs, not for its tests.

## Why

Memory off and the ideal record need no eva-md. The third condition is what a person gets: eva-md set up as a first run sets it up, the earlier chat captured by eva-md's own hooks, v1's update run on it, and the new chat started with eva-md's plugin loaded. This part adds that condition to the driver, so that v1's row can be recorded once a live run is allowed. Nothing in eva-md changes, so there is no switch.

## What to build

A package `internal/eval/continuation/v1arm` that registers the condition `eva` with `drive.Register`, and `cmd/eva-eval/continuation_eva.go`, which imports it for its effect only. Neither spells a host's name: `TestHostLiteralCount` passes unchanged.

1. **The binary.** The driver's `--eva-md PATH` names the eva-md to measure; it is linked into the trial's `bin/` as `eva-md`. The `Arm`'s `Check` refuses, before any trial, one whose build information (`debug/buildinfo`) has no `evamd_eval` among the tags of its `-tags` setting (EV-17d), since a release build refuses setup off macOS and takes a headless chat for a program's. An option of the condition lets a test skip that check; the command never sets it.
2. **Set up as a first run.** In the trial's home, with the host's CLI on PATH: a bare repository `<trial>/remote.git` made with `git init --bare`, then `eva-md setup --dir <home>/wiki --remote <trial>/remote.git --no-push --yes --host <the adapter's InstallName>` (setup run with no terminal stops without both `--dir` and `--remote`). Setup runs before any hook, since v1 sets aside chats captured before its install (`Baseline`, `internal/update/baseline.go`). A failure stops the trial unjudged, `setup_failed`.
3. **The earlier chat, through the hooks.** The task's `history` is fed to `eva-md hook --host <id>`, one payload a call on stdin, in order: `SessionStart` with `source` `startup`, then for each user turn `UserPromptSubmit` with `prompt`, for each agent turn `Stop` with `last_assistant_message`, then `SessionEnd`. Each payload's `cwd` is the folder the new chat runs in. Field names are those of `payload` in `internal/hook/hook.go`. The session id is made up for each trial; no transcript is named; and `CLAUDE_CODE_ENTRYPOINT` is not in the hooks' environment, so v1 takes the chat for a person's.
4. **Memory brought up to date.** Then `eva-md update --session <host id>:<session id> --foreground` runs until its line (`update: ...`, `Summary.Line` in `internal/update/job.go`) does not say `stopped: busy`, which it says while an update the hooks queued holds the update lock. Only then does the new chat start. An exit other than 0, or a line that says `waiting`, stops the trial unjudged, `update_failed`.
5. **The new chat** is started with `Launch.Plugin` true, so the driver loads eva-md's plugin as setup installed it, with the host's own memory off (EV-17b, item 4). `Launch.Env` adds `EVA_MD_EVAL_ACTOR=person`, the seam of EV-17d, so the evaluation build takes the headless chat for a person's, as it would take the same chat typed.
6. **What memory cost.** The condition's `After` waits, as item 4 does, for any update the new chat's hooks started, then reads the run log of the trial's home (the records `internal/update/runlog.go` writes under the state folder's `updates/`: tokens and duration) and returns their sums: the update of item 4 and every later one.
7. **Nothing of the history in a row.** The history's text, statements and values reach eva-md and the host only; rows and the command's output hold ids and figures.

## Owns and may touch

All new: the package with its test data, the one import file and its test. The package's `TestMain` calls `testgitenv.Clear()`.

```paths
internal/eval/continuation/v1arm/**
cmd/eva-eval/continuation_eva.go
cmd/eva-eval/continuation_eva_test.go
```

## Must not touch

Everything else: eva-md's own code (`cmd/eva-md`, `internal/hook`, `internal/update`, `internal/install`), `internal/eval/continuation/task`, the driver, and `cmd/eva-eval/continuation.go`. If the driver lacks a seam the condition needs, stop and report it.

## Accepted when

- `scripts/linux-check.sh` passes, and `go list -deps ./cmd/eva-md` names no `internal/eval` package.
- With a fake eva-md (the test binary re-run, recording its arguments, the names of its variables, its folder and its stdin) and the driver's fake host, for each host: the steps run in the order bare repository, setup, the hooks, the update, the new chat; setup's arguments are exact; the payloads equal a golden file; the hooks' environment holds no `CLAUDE_CODE_ENTRYPOINT`; the chat's holds `EVA_MD_EVAL_ACTOR=person`.
- While the fake eva-md answers `stopped: busy`, the new chat does not start; once the test lets it answer otherwise, it does.
- A failing setup, a failing update and an update line that says `waiting` each leave the trial unjudged with their code and write no row.
- `eva-eval continuation run --condition eva` refuses an eva-md built without the tag (the test binary, or any Go binary without it, serves), and nothing runs.
- A hand-written run log of two records in the trial's home gives the trial `memory_tokens` and `memory_ms` equal to their sums, and they count toward `--max-tokens`: by test.
- No word of the history is in a row or the output, by a test with a planted word in every turn.
