# The worker, its wake marks and its journal

The contract of `internal/work` (plan 6.3, design 1, and 8, row DV-12; derive 3.1 to 3.3 and 3.9). In v1 a job that finds `update.lock` held loses its trigger. Here a wake is a mark on disk that outlives its process, so a wake that finds the worker busy loses nothing.

Nothing in it runs unless a caller calls it, and every caller checks `work.Switch`, the switch `worker` under `[switches]`, off by default: v1. Its code imports from eva-md only `internal/config` and `internal/paths`, so the hook can import it at no cost.

## 1. Jobs

`Register(Job)` adds a job, from a package's `init` or a package-level variable.

| Field | Meaning |
| --- | --- |
| `Kind` | its name: lower-case letters, digits and `_`, starting with a letter, at most 32 bytes. It holds no `-` |
| `Priority` | lower runs first, as derive's P0 to P6 |
| `Run(ctx, marks, log) Outcome` | does what its marks ask, writes what a person may read to `log`, returns when `ctx` ends |
| `Waiting(ctx, now) []Waiting` | what of its work waits, and why (section 6); may be nil |

A kind registered twice, a kind that is not a valid name, or a job with no `Run` panics.

| `Outcome` | Meaning | Its marks |
| --- | --- | --- |
| `done` | did what its marks asked | removed |
| `failed` | tried and failed; the job keeps what it needs to try again | removed |
| `busy` | its own lock was held, as v1's `update.lock` and `maintain.lock` can be | put back; the kind is not run again in this wake |
| `cut` | its context ended | put back; the kind is not run again in this wake |

Any other value, and a panic in `Run`, count as `failed`. A panic's value is never written anywhere; the log gets one line naming the kind.

## 2. Files

Under `<state>/work/`, opened through `paths.OpenRoot`, folders 0700 and files 0600:

| Path | Holds | Written by |
| --- | --- | --- |
| `wake/<kind>-<key>` | one mark: its reason, its host when it has one, and when it was first written in Unix milliseconds, spaced, and a newline | any caller, by atomic replace (`paths.WriteFileAtomicAt`) |
| `run/<run>/<kind>-<key>` | the marks one run was given, moved out of `wake/` by rename | the worker |
| `journal/<YYYY-MM>.jsonl` | the journal (section 4) | the worker only |

The locks are `worker.lock` and `waiter.lock` in `dirs.Locks()`, flocks taken with `paths.TryLock`, or, by the waiter, with a blocking flock on the same file, so a dead process leaves nothing held.

A **mark** is `Mark{Kind, Key, Reason, Host}`. `Kind` is a registered kind. `Key` is chosen by the job's caller and must be a name `paths.SafeName` leaves as it is, with no upper-case letter, since macOS does not tell letter case apart (DV-12b uses `<host>-<id>`, `sweep-<day>`, `daily` and `push`). `Reason` is one word: lower-case letters, digits and `_`, starting with a letter, at most 32 bytes. `Host` is empty or one word of lower-case letters, digits and `-`. A file in `wake/` is a mark only if its name parses back to a registered kind and a valid key; only the words of its content that match these patterns are read. `Wake` refuses a mark that breaks any of this before it writes anything.

Writing the same mark twice leaves one file, with the last write's words and the first write's time. A mark is removed only when a run that was given it ends `done` or `failed`. The worker renames a run's marks into `run/<run>/` before the run starts, so a mark written again while its job runs lands as a new file in `wake/`, with a new time, which the run's end never removes. The run gets the words of the files it moved, listed again in `run/<run>/`. Marks put back return to `wake/` by hard link; where the same mark was written again meanwhile, the newer one stays.

## 3. The lock protocol and the worker

`Wake(ctx, dirs, marks, opts)` writes the marks (none for a tick), then:

1. It tries `worker.lock`. If it gets it, it is the worker.
2. If that is held, it tries `waiter.lock` without waiting. If that is held too, it returns nil: the waiter will see the marks.
3. As the waiter, it blocks on `worker.lock` until the worker releases it or `ctx` ends, then drops `waiter.lock` and is the worker. It drops `waiter.lock` before it lists any mark.

The worker first settles the runs a killed worker left (section 4) and, once a wake, removes old journal files. Then, until no mark it may run is left or `ctx` ends: it lists `wake/`, takes the kind with marks of the lowest priority (ties by kind name), moves every mark of that kind into `run/<run>/`, writes the `start` line, runs the job with those marks, writes the `finish` line, and removes or puts back the marks by the outcome. A kind whose run ended `busy` or `cut` is not run again in this wake; its marks wait for the next wake or tick.

When none is left it releases `worker.lock` and lists the marks once more. If one it may run is there, it starts again at step 1. So a mark written while the worker was finishing has a process to run it: a waiter, or this last listing. At most two callers are ever inside the worker or the waiter, however many wakes arrive.

A wake whose `ctx` ends returns `ctx`'s error. A waiter cut that way leaves its marks to the worker it waited on; a worker cut that way puts the running job's marks back for the next wake or tick. A worker that is cut, or meets an error, does no last listing: if its waiter was cut too, marks written meanwhile by wakes that found both locks held wait, as the cut run's do, for the next wake or tick.

`Options` has `Now`, the clock of the journal and the marks (`time.Now` when nil), and `Log`, given to each `Run` (`io.Discard` when nil). Its unexported `hook` lets tests hold a caller at `marked`, `waiter`, `worker`, `listed` (before the take), `taken`, `idle` (out of marks, the lock held) and `last` (before the last listing).

## 4. The journal

`journal/<YYYY-MM>.jsonl`, one line per transition, appended and synced before the job starts and after it ends; no job runs whose `start` line could not be written. A line a killed worker left unfinished is ended with a newline before the next is appended. Keys, in this order:

| Key | Value |
| --- | --- |
| `t` | UTC time, to the millisecond: `2026-11-10T07:01:02.123Z` |
| `run` | the run's id: 16 hex digits, random |
| `ev` | `start`, `finish` or `interrupted` |
| `kind`, `prio` | the job's kind and priority |
| `marks` | how many marks the run was given |
| `reasons`, `hosts` | the distinct reasons and hosts of its marks, sorted; `[]` when none |
| `outcome` | `finish` only: `done`, `busy`, `failed` or `cut` |
| `ms` | `finish` only: how long the run took, by the monotonic clock |

Never a mark's key, a session id, a path, an error's text or any captured text.

**Recovery.** Every `run/<run>/` a worker finds when it takes the lock was left by a worker that died. By that run's journal lines: a `finish` with `done` or `failed` means its marks were used and are removed; a `start` with neither a `finish` nor an `interrupted` gains an `interrupted` line, with the start's run id, kind, priority, count and words, and its marks are put back; anything else puts the marks back. If the `finish` line can't be written, the worker leaves `run/<run>/` and returns the error, so the next worker journals it as interrupted and runs its marks again: a mark is run at least once, never lost.

**Power loss.** Recovery reads `run/`, so the worker syncs `run/<run>/`, `run/` and `wake/` after the take and before the `start` line: a `start` on disk has its folder. Marks put back are synced into `wake/` before they leave `run/<run>/`.

**Retention.** The worker removes a journal file whose month ended more than 13 months ago.

## 5. The bar

`Bar` is 5 minutes, the plan's section 5.4 bar for nine chats in ten to be on file after they end.

## 6. Why work waits

`Status(ctx, dirs, now) Report` gathers what each job's `Waiting` returns and, for each mark in `wake/` of a registered kind, a `WaitBehind` item since the time the mark holds, when it was first written (a file holding none: the file's time). It keeps an item only when `now` minus `Since` is longer than `Bar`, or its reason is `WaitHeld` or `WaitSignIn`. `Report.Items` are oldest first. `Status` reads the marks' names and words, no captured text, and creates no folder.

A `Waiting` is `{Kind, Host, Since, Reason, Class, NextTry}`; `Class` and `NextTry` are set for `WaitHeld`. It names no session.

| `WaitReason` | Value | `Words()` |
| --- | --- | --- |
| `WaitRule` | `rule` | waiting for the size or quiet rule |
| `WaitBehind` | `behind` | behind a running job |
| `WaitBudget` | `budget` | deferred by the day's budget |
| `WaitHeld` | `held` | held after a failure |
| `WaitSignIn` | `sign_in` | waiting for sign-in |
| `WaitOff` | `off` | switched off, paused by the owner or excluded |

## 7. Invariants and the tests that hold them

| Invariant | Test |
| --- | --- |
| 50 wakes for 50 keys during a held job all reach a `Run`; never more than two callers inside | `TestNoWakeIsLostWhileAJobIsHeld` |
| 200 seeded schedules of wakes, ticks, job ends, holds at each point the worker passes, and cut waiters: every wake reaches a `Run` after it was written and takes the role its locks give it; nothing is left; `lost_wakes` is 0; no sleep decides | `TestGeneratedSchedulesLoseNoWake` |
| The waiter drops `waiter.lock` before it lists a mark | `TestTheWaiterDropsItsLockBeforeItWorks` |
| A run gets the words of the mark it moved | `TestARunGetsTheWordsOfTheMarkItMoved` |
| Marks a cut worker and a cut waiter leave run at the next tick | `TestMarksLeftByACutWorkerAndACutWaiterRunAtTheNextTick` |
| The syncs for a power loss, in order | `TestARunsFolderIsSyncedBeforeItsStartLine` |
| A mark written again during its run runs again | `TestAMarkWrittenAgainDuringItsRunRunsAgain` |
| A `busy` job keeps its marks and runs once in a worker's run | `TestABusyJobKeepsItsMarksAndRunsOnceInAWorkersRun` |
| A `cut` run puts its marks back | `TestACutRunPutsItsMarksBack` |
| A worker killed with SIGKILL is journaled `interrupted` with its run id and kind; its marks run again | `TestAKilledWorkersRunIsJournaledAsInterrupted` |
| Runs a killed worker left are settled as section 4 says | `TestAWorkerSettlesTheRunsAKilledWorkerLeft` |
| A cut waiter leaves its mark to the worker | `TestAWaiterCutByItsContextLeavesItsMarkToTheWorker` |
| The journal holds no key and no job's error or panic text | `TestNoJournalLineHoldsAKeyOrAJobsErrorText` |
| The journal's keys, times and month file; the start line is on disk before the job runs | `TestTheJournalLinesAndTheirKeys` |
| A line left unfinished can't swallow the next | `TestTheJournalEndsALineAKilledWorkerLeftUnfinished` |
| Journal files older than 13 months are removed | `TestTheWorkerRemovesJournalFilesOlderThanThirteenMonths` |
| Lowest priority first; a kind's run gets all its marks | `TestTheLowestPriorityRunsFirstAndEachKindGetsAllItsMarks` |
| An unknown outcome or a panic is `failed` | `TestAnUnknownOutcomeOrAPanicIsFailedAndUsesItsMarks` |
| The same mark twice leaves one, with the last words and the first time | `TestTheSameMarkTwiceLeavesOne` |
| A file whose key has an upper-case letter is not a mark | `TestAFileWithAnUpperCaseKeyIsNotAMark` |
| Folders 0700, files 0600 | `TestFoldersAndFilesArePrivate` |
| A bad job or mark is refused, and a refused wake writes nothing | `TestRegisterRefusesABadJob`, `TestWakeRefusesABadMarkAndWritesNothing` |
| Each of the six reasons and the bar reach `Status`, with the clock injected; the words are the plan's | `TestStatusGivesEachReasonPastTheBar`, `TestEachReasonHasThePlansWords` |
| A wait is measured from a mark's first write; one written during its run starts anew | `TestStatusMeasuresAMarkFromItsFirstWrite` |
| `Status` creates nothing | `TestStatusCreatesNothing` |
| The switch is off by default | `TestTheSwitchIsOffByDefault` |
| The package imports nothing the hook would pay for | `TestThePackageImportsNothingTheHookWouldPayFor` |
