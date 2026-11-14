# EV-10e: git's background work off in every test rig that commits

**Plan:** section 8 Stage 1 row EV-10 (its last clause), section 9 ("Documents a team can work from"). **Design:** `evaluate.md` section 12.1, step "Guarantees first". **Part of:** EV-10: a, b, c, d, e (this). **Size:** S. **Reviewers:** one. **Depends on:** EV-10d, merged.

## Why

After a commit, a fetch or a merge, git may start its own maintenance in the background, a process that outlives the command that started it. In a test rig it races the removal of the test's temporary folder and the next command's locks, so a test can fail with no fault in what it tests. One failure of CI's macOS job on 2026-11-14 (run 178) was that, in a rig of the evaluation's, and the two evaluation rigs were mended then with `gc.auto=0` and `maintenance.auto=false`. EV-10d's survey found the same gap in almost every older rig: eighteen packages whose tests commit, clone, fetch or push give git a configuration that leaves both at git's defaults. A test may fail only when behaviour is wrong (`AGENTS.md`, "Tests never depend on machine speed").

## What to build

Test files only. No file of the program changes, and no existing test's meaning changes.

1. **The rigs.** In each of the eighteen packages below, the git configuration the tests' own helper writes (the file `GIT_CONFIG_GLOBAL` names, or the `-c` settings a helper gives its git) gains `gc.auto = 0` and `maintenance.auto = false`. Check each package against the source before changing it, and report one the survey got wrong. A configuration the program under test writes, such as the one setup gives a wiki, is not a rig's and is left alone; so is any test that holds what the program itself configures. `internal/release`'s rig is in a file that runs on macOS only: change it by reading, and show that it still compiles there (`GOOS=darwin go vet ./internal/release`).
2. **A check of the source**, in `internal/spec/gitrigs_test.go` (new): among the module's test files, one that writes a git configuration for its tests and belongs to a package whose tests commit, clone, fetch or push writes both settings, or is listed with its reason. Hold the narrowest rule that is true and that the source can show, as EV-10c's checks do, and seed its faults on made-up trees: a rig that commits and sets neither, one that sets one of the two, and a listed rig that sets both.
3. **In the report**: for each of the eighteen, the file changed and how its configuration is given; any rig found that the survey missed; and any test that asserts on a configuration's exact text, with what was done about it.

## Owns and may touch

The test files of the eighteen packages, and the new file of `internal/spec`.

```paths
cmd/eva-eval/*_test.go
cmd/eva-md/*_test.go
cmd/hostrec/*_test.go
internal/coderef/*_test.go
internal/eval/continuation/v1arm/*_test.go
internal/eval/guard/*_test.go
internal/eval/results/*_test.go
internal/health/*_test.go
internal/hook/*_test.go
internal/identity/*_test.go
internal/knowledgepage/*_test.go
internal/mcp/*_test.go
internal/release/*_test.go
internal/resume/*_test.go
internal/systempage/*_test.go
internal/update/*_test.go
internal/wikigit/*_test.go
internal/wikiinit/*_test.go
internal/spec/gitrigs_test.go
```

## Must not touch

Any file of the program, the evaluation's program code among it (`internal/eval/continuation/task/workspace.go` sets both already); `internal/testgitenv`; `internal/redact/baseline`; what an existing test asserts of the program; `scripts/` and `.github/`; the documents, which are the lead's at the merge. A rig whose helper is in a file that is no test file is reported, not changed: the lead widens the block.

## Accepted when

- The scoped check the order names passes on the eighteen packages and `internal/spec`, and `go test ./internal/spec` passes as root too.
- The check of the source passes, and each seeded fault fails it.
- No test added here sleeps or skips, and the pinned counts of sleeps and skips are unchanged.
- The report holds item 3's list.
