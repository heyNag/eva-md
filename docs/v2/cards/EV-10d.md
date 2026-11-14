# EV-10d: every test package that reaches git clears its environment first

**Plan:** section 8 Stage 1 row EV-10 (its last clause), section 9 ("Documents a team can work from"). **Design:** `evaluate.md` section 12.1, step "Guarantees first". **Part of:** EV-10: a, b, c, d (this). **Size:** S. **Reviewers:** one. **Depends on:** EV-10c, merged.

## Why

A cloud workspace carries git configuration and a commit identity in the environment, which `HOME` and `GIT_CONFIG_GLOBAL` do not shut out, so `AGENTS.md` has every package whose tests run git call `testgitenv.Clear()` first in its `TestMain`. EV-10c's check of the source found 19 packages whose tests link code that starts git and that have no such `TestMain`. It could only list them (`unclearedGit` in `internal/spec/home_test.go`), since it changed no existing test. Seven of them link `internal/eval/results/row.go`, which runs git with the environment as it finds it: a test of theirs can pass or fail by where it runs. EV-10c's verifier also left one gap in the reader of `check:<path>` cells.

## What to build

Test files only. No file of the program changes.

1. **Clear first.** Each of the 19 packages gets `testgitenv.Clear()` as the first statement of its `TestMain`: a new `main_test.go` in the 17 that have none, and the call added, before anything else, in the two that have one (`cmd/live-accuracy` and `internal/eval/pilot/task`). A new `TestMain` clears, runs the tests and exits with their code, and does nothing else.
2. **The list empties.** `unclearedGit` holds no package and the reason `throughOthers` goes; the list stays, empty, as the place for a later exception with its reason. The check's seeded faults still fail on made-up trees: a package that reaches git through another and does not clear is caught.
3. **Shown, not assumed.** Run the 19 packages' tests once with git configuration and an identity carried in the environment (`GIT_CONFIG_COUNT=1` with `GIT_CONFIG_KEY_0=commit.gpgsign` and `GIT_CONFIG_VALUE_0=true`, `GIT_AUTHOR_NAME`, `GIT_COMMITTER_EMAIL`), before the change and after it, and report which tests failed before and pass after. A test that still fails so is reported, not mended, unless the mend is the clearing itself. Add no test that starts the test binary again.
4. **The check-cell reader refuses what it cannot read.** In `internal/spec/checkcell_test.go`, a workflow that holds a form the line reader cannot read as GitHub does holds no check cell: a quoted key, a key with a space before its colon, an anchor, an alias or a merge key, a complex key, a flow mapping, or a tab in an indent, outside block scalars, as `unplainForms` of `internal/release/ci_guard_test.go` lists them. `runByWorkflow` passes such a workflow over, and the seeded-fault test of the cell seeds each form. Two test packages share no code, so the list is written twice, and a comment in each names the other.
5. **A survey for the lead, in the report only.** Every test rig that writes a git configuration for a temporary home (a file `GIT_CONFIG_GLOBAL` names, or `-c` settings on a helper's git): whether it turns git's background work off (`gc.auto=0` and `maintenance.auto=false`), and whether its tests commit or fetch. Change none of them: the lead decides from the list.

## Owns and may touch

The 19 `main_test.go` files, and the two files of `internal/spec` named below.

```paths
cmd/live-accuracy/main_test.go
internal/config/main_test.go
internal/eval/continuation/drive/fakehost/main_test.go
internal/eval/continuation/report/main_test.go
internal/eval/families/main_test.go
internal/eval/pilot/task/main_test.go
internal/eval/retrieval/main_test.go
internal/eval/retrieval/retrievaltest/main_test.go
internal/eval/scenario/main_test.go
internal/eval/score/main_test.go
internal/eval/spike/main_test.go
internal/eval/spike/arm/main_test.go
internal/eval/v1page/main_test.go
internal/ledger/audit/main_test.go
internal/ledger/inboxv1/main_test.go
internal/ledger/registry/main_test.go
internal/threadpage/main_test.go
internal/update/accuracy/live/main_test.go
internal/work/main_test.go
internal/spec/home_test.go
internal/spec/checkcell_test.go
```

## Must not touch

Any file of the program; any other existing test; `internal/testgitenv`; `internal/eval/results/row.go` (whether its git should take a cleaned environment is reported, not changed); `scripts/` and `.github/`; the documents, which are the lead's at the merge.

## Accepted when

- The scoped check the order names passes on the 19 packages and `internal/spec`, and `go test ./internal/spec` passes as root too.
- `unclearedGit` is empty and the check of the source passes; taking the call out of one new `TestMain`, or putting it after the tests run, fails it (shown in the report, not left in the tree).
- Each form of item 4 is seeded and caught, and `ci.yml`, as it is, still holds today's check cells.
- No test added here sleeps or skips, and the pinned counts of sleeps and skips are unchanged.
- The report gives item 3's two runs and item 5's survey.
