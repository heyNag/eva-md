# EV-17e: the 28 continuation tasks and their three fixture repositories

**Plan:** section 6.6 (28 tasks: 10 handoffs, 10 with a decision hidden in the first session, 8 control tasks); section 8, the Stage 1 row EV-17 and the Stage 0 row EV-03; section 8's rows added on 2026-11-10 (a voice hand-over joins at least two of EV-17's tasks if the second report counts 10% or more); section 14.2 items 2 and 3 (work outside git, and across repositories). **Design:** `docs/v2/working-papers/design/evaluate.md` section 6.3 (fixture repositories written in-house, the screen) and 7. **Size:** M. **Reviewers:** one, who also tries to solve each hidden-decision task without its fact. **Depends on:** [EV-17a](EV-17a.md), [EV-17b](EV-17b.md) (its `continuation check`) and EV-03. **Does not start** until EV-03 has said whether its tasks can show a difference (the pilot's threshold: an ideal record against memory off, under 30% fewer steps, means the tasks are rebuilt first) and the lead has written its findings into this card.

## Why

The instrument is only as good as its tasks. A task memory cannot help shows nothing; a task whose hidden test can be passed without the fact shows nothing either. The pilot tells which kinds of task move; this part writes the set the plan asks for, in the format of EV-17a, each checked offline before any session is spent on it.

## What to build

1. **Three fixture repositories** under `eval/continuation/testdata/repos/`: two in Go (a command-line tool and a library, so that they differ in shape) and one in Python, each a small working program of a few hundred lines with its own tests and no dependency beyond the language's standard test runner. Written in-house, so no model has seen their solutions. Every name, value and fact in them is made up. The Go repository's `go.mod` names no Go newer than eva-md's.
2. **28 tasks** under `eval/continuation/testdata/tasks/<id>/`, in EV-17a's format:
   - 10 `handoff`: the earlier chat stops part way (after the first source edit, after the first test result, or after the first edit that follows a failure), and `start/` holds the repositories as it left them. At least 4 are investigations across two repositories, with `workdir` `outside`, because that is how the real install is used.
   - 10 `hidden`: a decision, a convention or a failed approach exists only in the earlier chat, and a hidden test encodes it. At least 2 hide a stated fact about a name (`name` true). At least 2 have about twice as many unrelated turns as turns that matter.
   - 8 `control`: tasks memory cannot help, each with an earlier chat in the same repository about something else, from which the "no measurable harm" bar is read.
   - Every task has a `note` that a person could have written at the end of the earlier chat, holding what the new chat needs and nothing that names a hidden test.
3. **`eval/continuation/testdata/pack.json`**: the task ids, and for each the sha256 of its folder's files in a fixed order, so a change to a task fails until the pack is updated.
4. **A package `internal/eval/continuation/pack`** whose tests load the pack, validate every task, and run `task.Check` on each.
5. **What the pilot and the second report add.** The pilot's ten tasks may be among the 28, rewritten in this format, if they passed its screen. The pilot's throwaway code and tasks are then deleted: the paths EV-03's cards own, listed below as they stand in those cards' drafts (the lead corrects them to the cards as merged). If the second report counts a person's prompts inside a voice hand-over at 10% or more, at least two tasks carry one in their earlier chat; the lead writes that into this card when the report is read, and under 2% no voice case is added.

## Owns and may touch

New: the task set, its repositories and its pack, and the package that checks them. Deleted: the pilot's paths.

```paths
eval/continuation/testdata/**
internal/eval/continuation/pack/**
internal/eval/pilot/**
eval/pilot/**
cmd/eva-eval/pilot.go
cmd/eva-eval/pilot_test.go
cmd/eva-eval/pilot_run.go
cmd/eva-eval/pilot_run_test.go
```

## Must not touch

Everything else. `internal/eval/continuation/task` and the driver are read, never edited; if the format lacks something a task needs, stop and report it.

## Accepted when

- `scripts/linux-check.sh` passes.
- A test holds the counts of item 2: 10, 10 and 8; at least 4 handoffs with two repositories and `workdir` `outside`; at least 2 hidden-decision tasks with a fact about a name; every repository used by at least 4 tasks.
- `task.Check` passes for all 28 tasks in `go test`: every hidden test runs with `go` or `python3`, which the checks have everywhere, so no task is checked on some machines only. `eva-eval continuation check --tasks eval/continuation/testdata/tasks --repos eval/continuation/testdata/repos` gives the same.
- Changing one byte of a task fails the pack test until `pack.json` changes.
- In every file of the task set, `redact.Scan` finds nothing, and no word `TestThePublicRepoUsesNeutralWording` bans appears (that test reads no file under `eval/`, so the pack test applies its pattern, copied with a line naming it).
- No file of the pilot is left, and no Go file names `internal/eval/pilot`.
- No fact's value appears in any task's id, any test's id or any repository's name, by `task.Validate`; no value of one task is a value of another, by a test.
- Each hidden-decision task's `decoy/` is a plausible solution from the repository and the prompt alone, and fails exactly the tests that encode a fact (`task.Check`). The reviewer's own attempts at such a solution are listed in the review; one that passes sends the task back.
