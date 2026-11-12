# EV-17a: continuation tasks, their hidden tests, and the rows they give

**Plan:** section 6.6; section 8, the Stage 1 row EV-17. **Design:** `docs/v2/working-papers/design/evaluate.md` sections 4.5, 5.3, 6.3 and 7; `critique-delivery.md` section 11. **Size:** M. **Reviewers:** one. **Depends on:** EV-12 (merged). First of five parts: a, b, c in order; d alone; e after a, b and EV-03.

## Why

The continuation instrument asks whether a new chat finishes interrupted work, or keeps an earlier chat's decision, with less help when memory is on, graded by hidden tests, steps, tokens and corrections, never by a model. This part is the format, grader and rows the later parts agree on. It runs no host. Nothing in eva-md changes: no switch.

## What to build

A new package `internal/eval/continuation/task`. Its non-test code imports the standard library and `internal/eval/results` only.

1. **A task is a folder**: `task.json`, and overlays on fixture repositories in `start/<repo>/`, `hidden/<repo>/`, `solution/<repo>/` and, for a hidden-decision task, `decoy/<repo>/`. `Load(dir)` reads one, `LoadAll(dir)` a folder of them. `Task` holds:
   - `id`, a results id; `family`: `handoff`, `hidden` or `control`;
   - `repos`: one or two names, each a folder `<repos dir>/<name>/`; `workdir`: `repo` (the chat runs in the repository) or `outside` (in a folder, not a repository, holding each under its name);
   - `history`: the earlier chat, as turns `{by, text, plants}`, `by` being `user` or `agent`, `plants` the facts a turn states;
   - `facts`: `{id, statement, values, name}`: `statement` is what the driver says to supply it; `values` the made-up words it turns on; `name` true for a fact about a name;
   - `prompt`, the new chat's first prompt, and `note`, the ideal record a person would hand over;
   - `tests`: `{id, repo, run, facts, limit_s}`: `run` an argv, no shell, that runs one hidden test with its language's runner; `facts` those it encodes; `limit_s` its time limit (600 when absent);
   - `max_turns`; `max_rounds`, 1 to 5.
2. **`Validate`** names the first fault, never a value: an id that is not a results id; an unknown family or workdir; two repositories under `repo`; a fact no turn plants; in a `hidden` task a fact no test encodes; in a `control` task a test that encodes one; a value or repository name inside the task's or a test's id, or of under six letters and digits, or of hex digits only (`Forbid` matches inside any field, commit ids too); a history that does not end with an agent turn or holds under 600 characters (v1's update waits on less: `planWait`, `internal/update/pass.go`); an overlay path that is absolute, unclean, a link, or leaves its repository.
3. **Conditions.** `Condition` is `off`, `eva` or `oracle`. `FirstPrompt(t, c)` is the prompt alone under `off` and `eva`, and under `oracle` the note, a fixed separator, then the prompt.
4. **The workspace.** `Prepare(ctx, t, reposDir, dst)` copies each repository (to `dst` under `repo`, to `dst/<name>/` under `outside`), lays `start/` over it, and commits each once with a made-up identity and fixed dates, git's environment built, not inherited: an empty `GIT_CONFIG_GLOBAL` outside `dst`, `GIT_CONFIG_NOSYSTEM=1`, and none of `GIT_CONFIG_COUNT`, `GIT_CONFIG_PARAMETERS` or the author and committer variables, so two builds give the same commit ids. No hidden, solution or decoy file is ever in it.
5. **Grading.** `Grade(ctx, t, taskDir, workspace, scratch)` copies the workspace into `scratch`, lays `hidden/` over the copy, runs each test within its `limit_s`, and returns which pass (exit 0) and, for each that fails, the facts it encodes. It never writes the workspace or reaches the network (`GOPROXY=off`, `GOTOOLCHAIN=local`, no package installed). A test out of time fails; a runner that is not installed is an error.
6. **`Check(ctx, t, ...)`**: on the start state at least one test fails; with `solution/` every test passes; in a `hidden` task, with `decoy/`, exactly the tests that encode a fact fail.
7. **Rows.** `Outcome` holds one trial's figures: solved, first try (passed before any follow-up), corrections, rounds, agent events, prompt, cached and output tokens, wall milliseconds, and, if measured, asked-again and what the condition itself spent outside the chat (`memory_tokens`, `memory_ms`: v1's update calls a model the chat's stream never shows). `Rows(t, c, trial, o, base results.Row)` gives one row a figure: metric `continuation.<family>.<condition>.<measure>`, instrument `continuation`, scenario `<task id>.t<trial>`, seed the trial, and commit, dirty, host, version, model and time from `base`. Units: `ratio` (0 or 1) for `solved` and `first_try`, `count`, `tokens`, `ms`. No `asked_again`, `memory_tokens` or `memory_ms` row unless measured. `Forbid(t)` lists every value and repository name, for `results.Store`.
8. **The catalogue**, `eval/continuation/metrics.json`, every metric of item 7 with its unit and direction. Every metric covers `eval/continuation/testdata` and `internal/eval/continuation`; an `eva` metric also covers what a chat start, a prompt and an update run through in v1: `cmd/eva-md`, `internal/hook`, `internal/resume`, `internal/update`, `internal/threadpage`, `internal/freetext`, `internal/knowledgepage` and `internal/install`. No gates file and no baseline: both wait for v1's live row.

Test data: one small made-up Go repository, and a task of each family. `gofmt -l cmd internal` reads test data, so its Go files, overlays included, parse and are formatted; its `go.mod` names no Go newer than eva-md's. `TestMain` calls `testgitenv.Clear()`.

## Owns and may touch

Both new.

```paths
internal/eval/continuation/task/**
eval/continuation/metrics.json
```

## Must not touch

Everything else. `internal/eval/results` and `cmd/eva-eval/main.go` are read, never edited.

## Accepted when

- `scripts/linux-check.sh` passes; `go list -deps ./cmd/eva-md` names no `internal/eval` package.
- Each fault of item 2 has a test that fails without its check.
- `Prepare` twice, once with `GIT_CONFIG_COUNT` and an author name set, gives the same commit ids; no file of `hidden/`, `solution/` or `decoy/` is in the workspace, by a test that lists it.
- `Grade` on the three tasks gives exact pass and fail sets on the start, the solution and the decoy, and the workspace is unchanged; a stub test that blocks until its context ends, with `limit_s` 1, fails.
- `Rows` gives exact rows for a made-up outcome, and the catalogue's `Check` accepts every row it can give; a value in a task's id is refused by `Validate`, and in a row by the Store with `Forbid(t)`.
- `FirstPrompt` is exact under each condition.
