# DV-12a: the worker, its wake marks and its journal

**Plan:** section 8, Stage 1, row DV-12, and section 14.6 (the sentence its gate gained); section 6.3, design 1; section 15.2 ("DV-12 builds its own package at once"); section 9 (a spec beside a package that others build on). **Design:** `derive.md` sections 3.1 to 3.3, 3.9 and its row DV-12 in section 18; `update-write.md` finding F4 (a busy lock drops the trigger). **Size:** M. **Reviewers:** one. **Depends on:** SEAM-0c (merged), for the switch. **Part of:** DV-12, in two parts: this one first, then DV-12b, which gives it a caller.

## Why

In v1 a job that finds `update.lock` held exits and its trigger is lost (`internal/update/job.go`, `run`). The worker makes a wake a mark on disk that outlives its process, so a wake that finds the worker busy loses nothing: a hard gate. DV-13, DV-22, DV-24 and HO-22 build on this contract.

## What to build

A new package `internal/work`, with no caller yet. It must not import `internal/update`, `internal/hook`, `internal/health` or `cmd/...`, so that the hook can import it at no cost.

1. **The switch**: `var Switch = config.NewBoolSwitch("worker", false)`. Off is v1: nothing in this package runs unless a caller calls it, and DV-12b's callers check it.
2. **Jobs.** `Register(Job)`, where a `Job` has `Kind string`, `Priority int` (lower runs first, as derive's P0 to P6), `Run func(ctx context.Context, marks []Mark, log io.Writer) Outcome` and `Waiting func(ctx context.Context, now time.Time) []Waiting`. A kind registered twice panics. `Outcome` is `done`, `busy` (the job's own lock was held, as v1's update and maintain locks can be), `failed` or `cut` (its context ended).
3. **Wake marks.** A `Mark` has `Kind`, `Key` (a name `paths.SafeName` accepts, chosen by the job's caller) and `Reason` (one word). A mark is a file `<state>/work/wake/<kind>-<key>` holding its reason. Writing the same mark twice leaves one. A mark is removed only when a run that was given it ends `done` or `failed`. A mark written again while its job runs is never removed by that run's end: take marks out of `wake/` before the run (by rename, for example) so that a new one lands as a new file. The marks of a run that ends `busy` or `cut`, or that was killed, go back.
4. **One worker, one waiter.** `Wake(ctx, dirs, marks []Mark, o Options) error` writes the marks (none for a tick), then follows derive section 3.1, steps 2 to 5, with `worker.lock` and `waiter.lock` in `dirs.Locks()`: become the worker; or, if it is busy, the waiter, which blocks until the worker lock is free; or exit when both are held. The worker runs the registered kind with marks and the lowest priority, one job at a time, until none is left; a kind that ended `busy` is not run again in that worker's run. Before it exits it releases the lock and lists the marks once more, so a mark written while it was finishing is never left with no process to run it; marks of a kind that ended `busy` wait for the next wake or tick.
5. **The journal**: `<state>/work/journal/<YYYY-MM>.jsonl`, written only by the worker, one line per transition, synced before the job starts and after it ends. Keys: `t` (UTC, milliseconds), `run` (an id for one job's run), `ev` (`start`, `finish`, `interrupted`), `kind`, `prio`, `marks` (a count), `reasons` and `hosts` (the distinct words of its marks), `outcome`, `ms`. Never a session id, a path, an error's text or any captured text. A worker that finds a `start` with no `finish` writes `interrupted` for that run and puts its marks back. Files older than 13 months are removed by the worker.
6. **Why work waits**: `Status(ctx, dirs, now) Report`. A `Waiting` has `Kind`, `Host`, `Since time.Time`, `Reason`, `Class string` and `NextTry time.Time`. `Reason` is one of six constants, each with the plan's words for printing: `WaitRule` (waiting for the size or quiet rule), `WaitBehind` (behind a running job), `WaitBudget` (deferred by the day's budget), `WaitHeld` (held after a failure; `Class` and `NextTry` set), `WaitSignIn` (waiting for sign-in) and `WaitOff` (switched off, paused by the owner or excluded). The report holds what each job's `Waiting` returns and, for marks not yet run, `WaitBehind`. It keeps an item only when it has waited longer than `Bar` (5 minutes, the plan's section 5.4 bar for nine chats in ten) or is held or waiting for sign-in. It reads no captured text.
7. **The spec**, `internal/work/SPEC.md`, at most 12 KB: the files (folders 0700, files 0600, opened through `paths.OpenRoot`), the lock protocol, the journal's keys, the reasons, the bar, and each invariant with its test.

## Owns and may touch

`internal/work/**` (new).

## Must not touch

Every other path. `internal/update`, `internal/hook` and `cmd/eva-md` are read, not edited.

## Accepted when

- `scripts/linux-check.sh` passes.
- **No lost wake.** While a job is held on a channel the test releases, 50 wakes for 50 keys arrive from goroutines; once it is released every key reaches a `Run`, and a counter exported for tests never sees more than two callers inside the worker or the waiter.
- **Generated schedules.** At least 200 seeded schedules interleave wakes with job starts, job ends and the worker's last listing (held there by a test hook). Every wake reaches a `Run` after it was written, and at the end no mark is left and no caller is inside. `lost_wakes` is 0. No sleep or time window decides an outcome.
- A mark written again during its own job's run is run again afterwards, by test.
- A job ending `busy` keeps its marks and is run once in that worker's run, by a test that counts calls.
- A child process (the test binary run again) writes `start` and is killed with SIGKILL; the next worker writes `interrupted` with that run's id and kind, and its marks run again.
- A marker planted in mark keys and in a job's error appears in no journal line, by test.
- Each of the six reasons, and the bar, reach `Status` from a fake job, by test, with the clock injected.
