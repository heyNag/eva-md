# SEAM-0f: hook tests that a slow machine cannot fail

**Plan:** section 9, "tests never ... depend on its speed", and `AGENTS.md`'s rule "Tests never depend on machine speed". **Follows:** [SEAM-0b](SEAM-0b.md), the lead's commit fe39012, and [LG-12](LG-12.md)'s notes, which owe this card. **Size:** S. **Reviewers:** one, who runs the tests under load. **Depends on:** nothing.

## Why

`TestLateRunRecordsItsTime` fails in `go test -race` on a busy machine with `open .../heartbeats/codex/Stop.json: no such file or directory` at `beat_test.go:92`. By reading: `beat_test.go:73` sets `held.Slack = 0`, so the late run's record, `lateBeat` (`internal/hook/resume.go:389`, called at `hook.go:153`), has 100 ms of real time (`resume.go:403`). Its write, `paths.WriteFileAtomicAt`, gives up once its context has ended (`internal/paths/paths.go:143`, `:172`), so past 100 ms no file is written. In this test `Slack` changes nothing else: `holdWaits` holds every wait of `Run`, and recovery is a stub.

Commit fe39012 mended two tests that took a free session lock to mean a capture `Run` gave up on had ended; that capture still writes its heartbeat after it lets the lock go. Three more tests go on while such a capture can still write.

## What to build

Test files only. Nothing `./cmd/eva-md` builds changes: no built line, no switch, nothing v1 does.

1. **`TestLateRunRecordsItsTime`** (`beat_test.go:60`). Delete `held.Slack = 0`, so the late record keeps `busySlack`, and say why in the comment. Make `held.elapsed` (`:75`) record each start it is given, under a mutex (the capture `Run` gave up on calls it too, from its own goroutine), and check each is `Run`'s start: not before a time taken before the goroutine calls `Run`, not after `nextWait` returns the capture's wait. Check the one run's `At` is not the fixed time `setup`'s `Now` gives: the late record reads the system clock. Keep `awaitNoCapture` before the heartbeat is read.
   Make the test hold what its comment says, that the capture `Run` gave up on writes no heartbeat of its own, with the two writes in a fixed order. Once the test ends the capture's time, the capture's next call of `env.Now` is for its own heartbeat (`hook.go:369`); `Run` makes none (`lateBeat` reads `time.Now`, and `launch` returns at once for a run with no turn). So `held.Now`, for a call made from inside `capture` after the test ends its time, waits until `Run` has returned, under `hangGuard`; every other call returns at once. Then a capture heartbeat that ignored its ended time writes after the late record, and the test sees two runs.
2. **Wait for the capture, not the lock**, with `awaitNoCapture` (`hook_test.go:809`), in `hook_test.go`:
   - `TestDeadlinesAndNoLateCapture` (`:646`): after `release()` at `:692`, before the next `invoke` at `:697`.
   - `TestSessionStartBudgetAndMarkerErrors` (`:842`): after `release()` at `:888`, before the records are read at `:889`, which a capture that ignored its ended time would write to only after the check.
   - `TestCancelledCallbacksCannotWriteLater` (`:1005`): after `<-returned` at `:1034`, which closes while the capture still runs, before the next `invoke` at `:1040`. In the `getenv` case that capture holds no lock to order it.

   Mend the comments at `:693` and `:1035`, which take the session lock for the end of the capture.
3. **Read and found sound; leave them.** The late-capture tests that already call `awaitNoCapture`: `handlers_test.go:373`, `maintain_test.go:273`, `headless_test.go:196`, `record_test.go:385`. `deferred_test.go:179`: `Slack` is 0, but its stub waits for its own context to end, so the outcome is fixed. The `Slack = 0` of `hook_test.go:661` and `:854` serves exact deadline checks, and the late record it shortens is never read; that of `:706` serves a bound from below, which a slow machine only passes more easily. `TestParallelFreshHooks` (`:1049`) rests on `busySlack`, as the rule allows. `nearDeadline` (`resume_marker_test.go:20`) keeps its deadline inside the reserve at every call. The real-exec tests of `spawn_test.go` and `trigger_flag_test.go` wait only with `hangGuard`. A builder who finds one more test of either fault stops and reports it.

Scratch changes made below to see a test fail are put back, never committed. No test bounds `lateBeat`'s own window once item 1 is done, and none can without a seam in the hook's code: that waits for a card that changes the hook.

## Owns and may touch

The two test files the fixes are in.

```paths
internal/hook/beat_test.go
internal/hook/hook_test.go
```

## Must not touch

Every non-test file, `internal/hook`'s included; the other test files of `internal/hook`; what the helpers of `hook_test.go` do; `busySlack` and `hangGuard`.

## Accepted when

- `scripts/linux-check.sh` passes.
- **The cause, shown.** With `resume.go:403`'s `100*time.Millisecond` made `0` in a scratch change, `TestLateRunRecordsItsTime` as on `main` fails at `beat_test.go:92` with the same message on any machine, and the rewritten test passes. The report gives both outputs.
- **Each fix guards what it says**, by scratch changes, each failing a test the report names with that test's own message, not its hang guard: `lateBeat`'s call removed from `Run`; `lateBeat` passing `false` for failed; `lateBeat` passing `env.Now()` for the time; `lateBeat` not setting `p.elsewhere`; `Run` passing `time.Now()` for `late.started`; and `context.WithoutCancel(ctx)` for the context of `capture`'s deferred `heartbeat` (`hook.go:369`) alone, which item 1's test must catch, as `TestRunOutOfTimeIsMarkedFailed` does. And `ctx = context.WithoutCancel(ctx)` as `capture`'s first line, on which each test of item 2 must fail every time (`TestSessionStartBudgetAndMarkerErrors` and the `getenv` case could miss it before).
- **Twenty runs, twice**, from the worktree's root, each with both `go test` steps passing; `-cpu=1` sets GOMAXPROCS to 1 for every test, and under load the benchmark's verdict says nothing (`AGENTS.md`, "Load"):
  ```sh
  scripts/linux-check.sh -count=20 -cpu=1 -timeout=6h ./internal/hook
  pids=; for i in $(seq "$(nproc)"); do sh -c 'while :; do :; done' & pids="$pids $!"; done
  scripts/linux-check.sh -count=20 -timeout=6h ./internal/hook; kill $pids
  ```
  They show nothing rests on speed; they are not a test. The reviewer runs the second.
- The diff adds no `time.Sleep`, no `time.After` but with `hangGuard`, and no `Slack = 0`.

## The lead's rulings of 2026-11-13

The card's checker left these to the lead.

- `-cpu=1` stands for the one processor the lead's note asked for, and the run under load keeps the default, which is how the failure was seen.
- The builder runs the first command once and the reviewer the second, whatever else the machine is doing: a busy machine is what the card is about. Neither waits for a quiet one.
- A test of `lateBeat`'s own window waits for a card that changes the hook's code; none owes it today.
