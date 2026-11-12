# EV-03a: the pilot's ten tasks, their format and their offline check

**Plan:** section 8, Stage 0 row EV-03, and the Stage 1 row EV-17, which takes the pilot over; sections 6.6, 14.2 and 15.2. **Design:** `docs/v2/working-papers/design/evaluate.md` sections 5.1 and 6.3, and its row EV-03. **Size:** M. **Reviewers:** one. **Depends on:** LG-11 (merged). First of three parts: this, then [EV-03b](EV-03b.md) (the driver) and [EV-03c](EV-03c.md) (the report), which may run side by side.

## Why

The pilot tells, before decision 16's ceiling is set, what one agent session costs, how much it varies, and whether a task can show memory's effect at all. That needs tasks whose second chat depends on something only the first said, graded by a hidden test and never by a model, and checked offline first. The code is throwaway, under `eval/pilot` and `internal/eval/pilot` only, built into no release, and deleted when EV-17 lands; EV-17 rewrites the tasks it keeps in its own format. Nothing v1 does changes: no switch.

## What to build

1. **The format**, with EV-17's field names. A task is a folder `eval/pilot/testdata/tasks/<id>/` holding `task.json`:
   - `v` (1); `id`: `h01` to `h05` (handoffs) or `d01` to `d05` (hidden decisions); `family`, `handoff` or `hidden`; `lang`, `go` or `python`;
   - `repos`, one or two names; `workdir`, `repo` (the chats run in the one repository) or `outside` (in a folder that is no repository and holds each one under its name);
   - `history`, the first chat: turns `{by, text, plants}`, `by` `user` or `agent`, `plants` the facts a turn states;
   - `facts`, each `{id, statement, values, name}`: `statement` what a correction says, `values` the made-up words it turns on, `name` true for a stated fact about a name;
   - `prompt`, the second chat's first prompt; `note`, the ideal record a person would hand over;
   - `tests`, each `{id, repo, run, facts}`: `run` an argv, no shell, running one hidden test with `go` or `python3`;
   - `max_turns`, `max_rounds` (1 to 5) and `minutes`.

   Beside it: `repos/<name>/` (each repository as the first chat left it), `hidden/<name>/`, `solution/<name>/` and, for a hidden decision, `decoy/<name>/` (the work done without the fact), each laid over it.
2. **Package `internal/eval/pilot/task`**: `Task` (with `Dir`, its folder); `Load(dir) ([]Task, error)`, holding item 4's rules that run nothing; `Check(ctx, t, scratch, limit)`, the rules that run tests; `Forbid(t) []string`, every value and repository name, for `results.Store`; `Grade(ctx, t, workspace, scratch string, limit time.Duration) (failed []string, err error)`, which copies the repositories as `workdir` lays them out in `workspace` into `scratch`, lays `hidden/` over the copy, runs each test within `limit`, and returns the ids that did not exit 0; it never writes `workspace`. Tests run in an environment built from nothing: `PATH`, `HOME` and `TMPDIR` under `scratch`, `GOCACHE` as `go env GOCACHE` gives it, `GOPROXY=off`, `GOTOOLCHAIN=local`. A test out of time fails; a runner not installed is an error. Errors name the task and the rule, never a value.
3. **The ten tasks**, standard library only, every name and value made up, no repository over 300 lines. Five handoffs: the first chat did part of the work and said what was left; the prompt asks to carry on without saying what. Five hidden decisions: the first chat stated a decision, a convention or a failed approach the repository does not show, and a test encodes it. At least three in each language. At least one handoff is an investigation across two repositories with `workdir` `outside` (section 14.2). At least one hidden decision's fact is a name. No voice turn, no control task, no JavaScript: each waits for EV-17.
4. **The rules.** Every fact is planted by a turn and, in a hidden decision, encoded by a test. Values are matched as `results.Forbidden` matches words: the note holds one of every fact, the prompt none, and in a hidden decision no file under `repos/` any. A value or repository name has six letters and digits or more, is not hex digits only, and is in no task id, host id, arm (`off`, `eva`, `oracle`) or EV-03b metric, since `Forbid` matches inside any field of a row. The history holds 600 characters or more and ends with an agent turn, or v1's update waits on it (`planWait`, `internal/update/pass.go`). Overlay paths are relative, clean, no link, and stay in their repository. A Go repository's `go.mod` names a made-up module, no requirement and no Go newer than eva-md's. Each repository's own tests pass as it stands; every hidden test fails on it and passes with `solution/`; with `decoy/`, exactly the tests that encode a fact fail.
5. **`eva-eval pilot check [--tasks dir]`**, in `cmd/eva-eval/pilot.go`, added with `register`; `main.go` is not edited. The default is `eval/pilot/testdata/tasks`. One line a task: its id, family, and `ok` or the rule broken. Exit 0, 1 when a task fails, 2 for a usage error. `pilot.go` keeps a map of `pilot` subcommands that another file adds to from its `init`, so EV-03b adds `run` and EV-03c `report` without editing it.

## Owns and may touch

The package, the tasks, and the command's file and test, all new.

```paths
internal/eval/pilot/task/**
eval/pilot/testdata/**
cmd/eva-eval/pilot.go
cmd/eva-eval/pilot_test.go
```

## Must not touch

Everything else. `cmd/eva-eval/main.go`, `internal/eval/results` and `internal/redact` are read only. `eval/pilot/metrics.json` is EV-03b's, `eval/pilot/README.md` EV-03c's.

## Accepted when

- `scripts/linux-check.sh` passes, and `TestNoReleaseContainsIt` in `cmd/eva-eval` passes unchanged.
- `eva-eval pilot check` exits 0 on the committed tasks, by test.
- A test holds item 3's composition.
- For each rule of item 4, a test breaks it in a copy of a task and `Load` or `Check` refuses naming that rule; no message holds a value.
- `Grade` leaves the workspace's bytes unchanged, and a test that blocks until its context ends fails at a `limit` given to that one call.
- `redact.Scan` finds nothing in any file under `eval/pilot/`, and no file there matches the workplace pattern of `TestThePublicRepoUsesNeutralWording`, which reads nothing under `eval/`: a test of the package does both.
- `TestHostLiteralCount` passes unchanged.
- A reviewer's mutations (the prompt check dropped, a hidden test that passes on the start accepted, a decoy that fails a test encoding no fact accepted, a grade that lets go reach the network) each fail a test.
