# SEAM-0e: a git child that a signal ended says which signal

**Plan:** section 8, Stage 0, row SEAM-0 (guards); section 9 (CI's macOS job is the run of record, and a test fails only when behaviour is wrong). **Size:** S. **Reviewers:** one. **Depends on:** nothing. Item 2 changes the words of one error in the binary.

## Why

CI's macOS job failed once on a commit that changed only documents (run 107, 2026-11-11). `TestCacheConcurrentHooks` in `internal/identity` got `identity: git config exited with -1`, in the race step, and the next run of the same tree passed. Three investigators and a judge read the job's log and every place eva-md sends a signal. Their finding: a signal ended the `git config` child, or the `/bin/sh` wrapper the test puts in front of git, and nothing of eva-md's sent it. Which signal it was, and which of the two it ended, cannot be told: Go reports -1 for every signal, the wrapper replaces itself with git and keeps no status, and the job keeps nothing the runner knew. In use the same death costs one hook event, dropped with exit 0, as a hook that fails closed should.

This card makes a repeat explain itself. It changes no outcome: the test fails as it did, and the hook fails closed as it did.

## What to build

1. **The error keeps what ended git.** In `internal/identity/identity.go`, a git call whose `ExitCode()` is -1 gives an error that reads `git <arg> ended by <state>`, where the state is `ProcessState.String()` of the child (as `signal: killed`), in place of `git <arg> exited with -1`. Every other exit reads as today, and `notRepo` is decided as today. The state holds no path, argument or output. This is the one change to v1 on this card, and it has no switch: the lead rules it an error's wording, which no caller reads.
2. **The test's wrapper keeps git's status.** In `internal/identity/cache_test.go`, the git that `gitCounter` puts first on PATH runs the real git as a child, not with `exec`; when git's status is not 0 it appends one line to a file beside `runs` holding only that status (128 plus the signal's number, for a signal); and it exits with the same status. A helper prints those lines, and every test that fails on an identity error from this fixture prints them through it, so a failure shows whether git or the wrapper died. First check which tests of the package have identity end its own git while this wrapper is on PATH: a wrapper that is not replaced by git is what the kill would reach, so such a test keeps the `exec` form, by a parameter, and the report names it.
3. **The probe test ends only its own child.** `TestProbeEndsAtItsWaitDelay` in `internal/host/adapter/probe_test.go` ends a `sleep` by a pid it reads from a file. It was ruled out as the cause; it should still be unable to reach a stranger: before the kill, the cleanup checks that the pid's command is still that `sleep` (`ps -o command= -p <pid>`), and kills nothing otherwise.
4. **Words for the lead**, in the report, for `.github/workflows/ci.yml`, which is not the builder's: a step at the end of the macOS `test` job, with `continue-on-error: true` and `timeout-minutes: 2`, that runs on every run (`if: always()`), so that it is known to work before it is needed, and lists the names, sizes and dates of the crash reports under `~/Library/Logs/DiagnosticReports` and `/Library/Logs/DiagnosticReports`, never their contents; and a second, only after a failure, that prints the last lines of the system log that name `memorystatus`. Give the exact YAML, and say which of `go test ./internal/release`'s checks it must pass.

## Owns and may touch

```paths
internal/identity/identity.go
internal/identity/identity_test.go
internal/identity/cache_test.go
internal/identity/signal_test.go
internal/host/adapter/probe_test.go
```

A new test file in `internal/identity` is the builder's too.

## Must not touch

Everything else. `.github/workflows/**` is the lead's. No other error's words change, and no test is skipped, retried or given a sleep.

## Accepted when

- `scripts/linux-check.sh` passes, with no existing test of `internal/identity` changed but where it pinned the words `exited with -1`.
- A fake git that ends itself with KILL, with SEGV and with TERM on `config` makes `Resolve` return `General()` and an error that names the signal, three times of three; a fake git that exits 3 gives `exited with 3` as today. By test, with the fake on PATH in a temporary folder.
- With the wrapper of item 2 and a real git that a test ends by a signal from inside (a fake that kills itself), the status file holds one line with 128 plus the signal's number and the failure text of the helper shows it; a git that exits 0 adds no line. By test.
- The count `gitCounter` returns is what it was: every cache test passes unchanged.
- The probe test's cleanup, given a pid file that names a process that is not its `sleep` (the test's own pid), kills nothing. By test.
- The latency gate's comparison with the commit before, since `internal/identity` is run by every hook: `scripts/linux-gate.sh`, as the rules of every card say.
- No new module; only eva's name in any output.
