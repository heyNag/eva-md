# EV-17f: the release workflow reads each binary it built for a build tag

**Plan:** section 7, the lead's choice that an evaluation-only build tag comes "with tests that no release contains it"; section 8, the Stage 1 row EV-17. **Follows:** [EV-17d](EV-17d.md), whose notes owe this step. **Design:** `docs/v2/working-papers/design/evaluate.md` section 4.4. **Size:** S. It adds no built line to `./cmd/eva-md` and changes nothing v1 does, so no switch; a release that passes today passes with it. **Reviewers:** two: the contract, and one who tries to get a tagged binary past the step. **Depends on:** EV-17d, merged.

## Why

EV-17d's tests show that the release's build, as the workflow writes it, records no tag. The files a release publishes are never read. A step that reads them closes that. The verify job has no checkout and no setup-go, so the step goes in the build job, after Build, where go.mod's Go is set up. Build's `SHA256SUMS` and verify's `shasum -c` tie what the step read to what is published. No action and no secret is added.

## What to build

1. **The step.** In `.github/workflows/release.yml`, job `build`, after `- name: Build` and before `actions/upload-artifact`: a step named `Check no binary carries a build tag`, with the keys `name` and `run` only. For each file matching `eva-md-darwin-*` in `$RUNNER_TEMP/release` it fails, with one `::error::` line naming the file and what was found, when:
   - `go version -m` fails, or its output has no line `<tab>path<tab>github.com/heyNag/eva-md/cmd/eva-md`. Read that output into a variable first, so no pipe can hide a failure or a match;
   - a line of that output matches `^[[:space:]]+build[[:space:]]+[-]tags=`. Go writes this setting only when a build had tags, from the flag or from `GOFLAGS`. A release has none;
   - the file's bytes hold `/cmd/eva-md/evalbuild` or `EVA_MD_EVAL_ACTOR` (`grep -a -F -q`; grep's status 2, could not read, fails too). These are `evalMarks` in `cmd/eva-md/evalbuild_test.go`: the path go keeps for code from `evalbuild*.go`, which only the tag compiles, and the seam's variable, whose only other mention, `table.go`'s `supportFiles`, is read by tests alone, so the linker drops it.
   It also fails when no file matches. It uses nothing `/bin/bash` 3.2 lacks, since the test runs it on macOS too.
2. **What the step may not write**, since tests read the workflow: the tag's name; `GOFLAGS`; a `-tags` that `tagGaps` reads as a flag (write `[-]tags`); `go build`, `go install` or `go run`; `live-accuracy`, `eva-eval` or `internal/eval`; a second line that `TestReleaseWorkflowPublishesEveryAsset` matches (`for arch in`, `cp ... "$out/"`, `shasum ... > SHA256SUMS`, `wc -l < SHA256SUMS`); or a `${{ }}` expression, so the test runs what the runner runs.
3. **The test,** `internal/release/release_tagcheck_test.go`, in two parts:
   - **Where it stands.** A function over the workflow's text gives what is wrong. It passes today's file and fails each of these copies: the step gone or renamed; before Build or after the upload; in another job; given `if`, `continue-on-error` or `shell`; no `go version -m` in its run, or not naming each string of `evalMarks` as `cmd/eva-md/evalbuild_test.go` holds them (read from that file's text); `continue-on-error` on any job; a job's `if` not on one line; a job's `if` that names a status function (`success()`, `failure()`, `cancelled()`, `always()`) and does not have `!failure()` as one of its parts joined by `&&`, has `||` outside brackets, or names `always()`; release, attest or verify no longer needing build, directly or through the jobs they need; and the workflow's `defaults` no longer `shell: bash`, or the build job given its own. Call `workflowJobs` and `lines`; never change them.
   - **What it does.** The test takes the step's `run` block from the file and runs it as `shell: bash` does (`bash --noprofile --norc -eo pipefail`), with `RUNNER_TEMP` set and the test's own go first on `PATH`. It passes eva-md built from the repository's root with `-trimpath -mod=readonly -buildvcs=false -ldflags "-X main.version=2026.11.99"`, `CGO_ENABLED=0 GOOS=darwin`, for each arch of the Build step's `for arch in` line. It fails each of these: that build with `-tags evamd_eval`, as either file; and made-up binaries from a module in `t.TempDir()` with go.mod's module path and a `go` line no newer than go.mod's, built with `-tags evamd_testlimits`; with the tag set in `GOFLAGS`; with a `cmd/eva-md/evalbuild.go` that has no constraint; with the variable in `main.go` alone. Each is first shown to hold what it is meant to (`debug/buildinfo`, `bytes.Contains`), and an untagged one with neither mark passes. Also failed: a file that is not a Go binary; another package's path; no file.
   Builds and the run use their own `HOME`, `GOENV=off`, `GOFLAGS=-mod=readonly`, `GOPROXY=off`, `GOTOOLCHAIN=local`, Go's telemetry off and Go's own caches, found with `go env` before `HOME` moves, as `standInEnv` in `cmd/eva-md/release_test.go` does. Every command has a hang guard of five minutes (`scriptHangGuard` there).
4. **`docs/releasing.md`**, item 1 of "What the workflow builds and publishes": one sentence for the step. The report gives a clause for line 18 of `docs/verification.md`, which EV-10a holds, for the lead.

## Owns and may touch

The new step of the release workflow, the test file (new), and one sentence of `docs/releasing.md`.

```paths
.github/workflows/release.yml
internal/release/release_tagcheck_test.go
docs/releasing.md
```

## Must not touch

Every other line of `release.yml`; `cut-release.yml`; `scripts/**`; `cmd/eva-md/**`; `internal/release/release_workflow_test.go`; `docs/verification.md`, `AGENTS.md`, `README.md`. Never run the release workflow, Cut a release or `scripts/release.sh` but with `--dry-run` or `--plan`.

## Accepted when

- `scripts/linux-check.sh` passes, and so do `go test ./cmd/live-accuracy ./cmd/eva-eval ./internal/release` and `go test ./cmd/eva-md -run 'Design|Agents|Usage|Verification|Wording|Document|NoReleaseCarries|TagCheck|ReleaseBuildIsReadWhole'`.
- `git diff` of `release.yml` adds the one step and changes no other line.
- With `release.yml` as it is before this card, the new test fails.
- A reviewer's mutations are each caught by a test: the tags line dropped, one mark dropped, a loop over arm64 alone, `|| true` after a check, `continue-on-error: true` on the step, the step moved after the upload, `if: ${{ !failure() || failure() }}` on the verify job.

## The lead's rulings of 2026-11-13

The card's checker left these to the lead.

- The step stands in the build job, as carded: it reads the bytes Build sums, and the verify job's check of the sums ties them to what is published.
- HO-13b's card now says how its release job's condition must be written so that this card's check passes it.
