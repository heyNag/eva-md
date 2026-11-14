# EV-03b: the pilot's ten tasks, exported to the continuation format

**Plan:** section 8, Stage 0 row EV-03 as the lead's ruling of 2026-11-13 left it ("only the tasks and their export are thrown away"); decisions 16 and 28. **Design:** `docs/v2/working-papers/design/evaluate.md` 4.3 to 4.5 and row EV-03. **Size:** S. **Reviewers:** one. **Depends on:** EV-03a, EV-17a.

## Why

This card replaces one for a second driver, withdrawn by the lead's ruling: the pilot runs on EV-17's driver (`internal/eval/continuation/drive`) and its v1 condition. The pilot's tasks are nearly in that format already. Three fields of their `task.json` (`v`, `lang`, `minutes`) are ones the continuation's strict decoder refuses, and each task keeps its repositories in its own `repos/`, where the driver reads one shared `--repos` folder. This card writes the copy the driver reads. The committed tasks stay as they are, and `eva-eval pilot check` stays their gate. It is throwaway and in no release. eva-md is unchanged: no switch, and no line added to `./cmd/eva-md`.

Not here: the driver's guards before a live run (EV-17g); whether eva-md's hooks fired in the new chat (EV-17c); the report and the pilot's README (EV-03c), which gives `--session-timeout` the tasks' 20 minutes; the run-book is the lead's. The live run is the lead's alone, after the owner's leave and ceiling and decision 28's keys and price.

## What to build

1. **`eva-eval pilot export --out DIR [--tasks dir] [--task id]...`**, in `cmd/eva-eval/pilot_export.go`, added with `addPilot` from its `init`; the work in a package `internal/eval/pilot/export`, which takes the time limit as an argument. `--tasks` defaults to `eval/pilot/testdata/tasks`, as `pilot check`'s does. With `--task`, only the tasks named.
2. **The pilot's checker first.** Each task exported is read with `task.Load` and checked with `task.Check` of `internal/eval/pilot/task`, the command passing `pilotCheckLimit`, in a scratch folder made in `TMPDIR` and removed, errors ignored, as `pilot check` does. It is then converted and must pass the continuation's `Validate` (`internal/eval/continuation/task`, `Dir` empty). A task any of these refuses gets the line `pilot check` prints for it (`broke`), or the continuation's message, and the command exits 1; a task that cannot be checked at all exits 2, as in `pilot check`. A task not named by `--task` is neither checked nor written.
3. **What it writes**, only once every check has passed: `DIR/tasks/<id>/task.json`, a continuation `Task` holding every field the pilot's has but `v`, `lang` and `minutes`, with no `limit_s`; `DIR/tasks/<id>/hidden/`, `solution/` and, for a hidden decision, `decoy/`, copied byte for byte, and no `start/`; and `DIR/repos/<name>/` from each task's `repos/<name>/`. A file is executable exactly when its source is. DIR is left with mode 0700, so the second user the run-book runs the CLI as (EV-17g) cannot read it. Nothing else is written in DIR, two exports of one source give the same bytes, and a write that fails part way removes what it wrote and exits 2.
4. **Refused with exit 2, nothing written:** no `--out`; an argument; an unknown flag or `--task` id; a DIR that exists and is not an empty folder, is a link, or lies inside `--tasks`, links resolved; two exported tasks that name one repository (one `--repos` folder holds them all).
5. **Output** is ids and figures only: a line a task, `<id> <family> exported`, then `session-timeout <n>m`, the largest `minutes` exported. No value, repository name or path is in output or an error.
6. **Bounded.** It reads no host's or model's output: only the source folder, which `pilot check` admits (folders and plain files, no link). Files are copied by streaming.

When EV-17's own tasks land (EV-17e), the pilot's tasks, their checker and this export are deleted; EV-17e may first rewrite a pilot task into its own set. The continuation format, the driver, its v1 condition and EV-03c's report stay.

## Owns and may touch

The package, and the command's file and test, all new.

```paths
internal/eval/pilot/export/**
internal/eval/pilot/task/grade.go
internal/eval/pilot/task/grade_test.go
cmd/eva-eval/pilot_export.go
cmd/eva-eval/pilot_export_test.go
```

## Must not touch

Everything else. Read only: `internal/eval/pilot/task` but `grade.go` and its test, for the telemetry ruling below alone; `eval/pilot/testdata`, `internal/eval/continuation/**`, `cmd/eva-eval/pilot.go` (`addPilot`, `broke`, `pilotCheckLimit`), `cmd/eva-eval/pilot_test.go` (`pilotHome`, `copyPilotTasks`, `replaceIn`), `main.go` and `main_test.go`. A committed task the continuation refuses is reported to the lead, never edited.

## Accepted when

- `scripts/linux-check.sh` passes; `TestNoReleaseContainsIt` and `TestHostLiteralCount` pass unchanged; `go list -deps ./cmd/eva-md` names no `internal/eval` package. A test that runs go calls `pilotHome` first; the export package's `TestMain`, if its tests run go or git, calls `testgitenv.Clear()` and gives go a home with telemetry off, as `internal/eval/pilot/task/main_test.go` does.
- The ten committed tasks export with exit 0. On the result the continuation's `LoadAll` takes all ten; `eva-eval continuation check --tasks DIR/tasks --repos DIR/repos` prints ten `ok` lines; each task's continuation `Forbid` equals the pilot's as a set; every field both formats have is equal.
- Every file under the exported overlays and `repos/` has its source's bytes and executable bit (a copy with one file made executable included), and none is added or missing; a second export gives the same bytes.
- DIR holds exactly item 3's paths, with mode 0700; HOME, the working folder and the source are unchanged after.
- `--task` gives only the tasks named and their repositories.
- Each of these exits as item 2 or 4 says and writes nothing in DIR: each refusal of item 4; two tasks sharing a repository (a copy of one task under a second id); a task `Load` refuses; one only `Check` refuses (a broken decoy); one only the continuation refuses (in a handoff task, a fact's value replaced, everywhere `task.json` holds it, by `continuation`, a word inside every continuation metric's name).
- Output and errors hold no word of the union of `Forbid`, by test.
- A reviewer's mutations (`minutes` kept, an overlay byte changed, a refused task written, the repository clash let through, DIR left readable) each fail a test.

## The lead's rulings of 2026-11-13

The card's checker left these to the lead.

- **The export runs the pilot's whole check**, hidden tests included: a task with a broken decoy is not exported. It is slow and it is right.
- **DIR is made with mode 0700**, since the run-book runs the chat as a second user who must not read it.
- **The pilot's own grading turns Go's telemetry off too.** This card also owns `internal/eval/pilot/task/grade.go` and its test, for that alone: before any test, grading writes `off` as Go's telemetry mode into the home it gives go, as EV-17g does for the continuation's, with a test that a stub failing without the file passes. Nothing else in the pilot's `task` package changes.
- If the continuation's check refuses one of the ten exported tasks, the builder stops and reports, as the card says; no task is edited to pass.

## As merged

Merged from `pkg/ev-03b-r1` on 2026-11-14, after a review, one fix round and a verification that accepted it. A restart of the machine cut the first builder off with its work committed; a second read that work against the card, found one gap (a link given with a slash after it got past the refusal of a link) and finished. The review's major fault: a second name for the tasks folder that is no link, as a spelling in another letter case is on macOS, let the export write into the tasks. The export now compares the folders themselves, not their names. On Linux no test can make such a second name, so that part of the fix is held by a case only CI's macOS job runs.

The continuation's check accepted all ten exported tasks as they are: none was edited.

Known and left: the test of the largest `minutes` would pass a rule that kept the first task's; and until EV-17g lands, the continuation's own grading, which this package's test runs through `continuation check`, still gives go a home with telemetry on, in a scratch folder outside any test's own.
