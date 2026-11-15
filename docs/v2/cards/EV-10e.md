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

## As merged

Merged on 2026-11-15 from `pkg/ev-10e-r1`, after one review (protection: two major, three minor, all mended, each with seeds that fail without the mend) and a verification that accepted it with one minor point, a seed the check lacked (a method of another package called straight on a literal), which the lead added in the merge with a second for a pointer to one; both fail when the reading they seed is taken out. Tests only: 34 test files of seventeen of the eighteen packages, and the new check.

**What the survey got wrong, and what it missed.**

- `cmd/eva-eval` set both settings already, so nothing changed there.
- Six rigs named a global config file they never wrote, and committed (`cmd/eva-md`, `internal/health`, `internal/hook`, `internal/mcp`, `internal/update`, `internal/resume`): each writes one now. Rigs the survey did not list: the account config of `cmd/hostrec`'s remote tests, the signing configs that `internal/coderef` and `internal/resume` write over the global file, and `internal/identity`'s benchmark repository.
- No test asserted on a configuration's exact text. `cmd/eva-md`'s `TestFakeMacStartsNoMaintenance` reads git's trace for the fake Mac's own config, which now turns collection off too, and passes unchanged.

**The check, as merged.** The first build read every string of a file, so a rig that stopped writing its config still passed where the file declared the settings in a constant; the review showed it by taking the write out of two rigs. It now reads what each write writes, through local names, the module's constants and package-level variables:

- every write of the global config turns both settings off, and a later value of a key replaces an earlier one, as in git;
- adding to the config, or `git config --global`, turns neither back on;
- each `Setenv` of `GIT_CONFIG_GLOBAL` is paired, in its function, with such a write;
- a git given settings with `-c` turns neither back on, and gives both itself where its package writes no global config with both;
- a commit, clone, fetch or push through a function of the module whose git reads no global config gives both with `-c`.

A package is in scope when a test of it reaches a git that names `commit`, `clone`, `fetch` or `push`, by its own helpers, a method, a function of the program, git found with `exec.LookPath`, or a `sh -c` script. Three files outside this card's paths are listed with their reasons, not changed: `internal/eval/continuation/task/main_test.go` and two of `internal/store/migrate`, where the program's own git gives both settings with `-c`.

**The lead's mends in program code**, in a commit of the lead's after the package's, since the review found two places a rig cannot reach. The host recorder (`cmd/hostrec`) gives every git an empty global config, so the commit of its scratch repository and its push ran with collection and maintenance on: each of its gits now gets both settings with `-c`. The continuation trial's home config (`internal/eval/continuation/drive/trial.go`) turned maintenance off and left collection on: it turns both off. No release builds either tool.

**Known and left.** The check reads names, not types, and its header lists what it holds and what it does not. A package whose tests only merge or pull is out of scope; none is known. A `-c` helper passes without the settings where its package writes a global config with both (`internal/resume/git_test.go`), since git reads that config too.

**For every builder from now on.** A test that gives git a configuration, in a package whose tests commit, sets `gc.auto = 0` and `maintenance.auto = false` in it; `go test ./internal/spec` fails when one does not, and its message names the list for an exception with its reason.
