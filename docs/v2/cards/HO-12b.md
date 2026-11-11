# HO-12b: each stage's lag beside its bar, and a verdict

**Plan:** section 6.5 (designs 6 and 7), section 5.4 and section 6.2 (the bars), section 8 Stage 1 row HO-12, sections 13.3 (points 4 and 7), 13.4, 14.3 (the count of waiting chats is a floor) and 14.5 (the search index). Decision 4 is taken at its recommendation, but the clock is HO-22's: until it lands, lags are read when `status` or `doctor` runs. **Design:** `hosts.md` sections 7.1 to 7.3 and 8; `docs/v2/working-papers/audit/ops.md` finding ops-1. **Part of:** HO-12: a, b (this), c. **Size:** M. **Reviewers:** one. **Depends on:** HO-12a; LG-10 (merged); CT-1 (its held-back counts and its week of runs) and CT-4 (the age of the oldest event not yet organized); DV-12a and DV-12b (`work.Status`, the reason work waits).

## Why

v1's health asks whether a job recorded a failure, never whether the work happened. The update backlog row is PASS at any age and reads only the 64 newest files of a folder; a daily commit last made three weeks ago reads PASS; thirteen rows about a host nobody uses say UNKNOWN for good; and "covers 88%" led an agent to build the index although search scans the rest.

## What to build

1. **The switch** `health.Switch = config.NewBoolSwitch("health", false)`, in `internal/health/switch.go` (new). Off is v1: every row, the output of `status` and `doctor` and their exit codes, byte for byte. All below is with it on.
2. **Stages**, in `internal/health/stages.go` (new): `type Stage struct { Name, Host string; Lag, Bar, Budget time.Duration; Since time.Time; Reason string; Waiting int }` and `Stages(ctx, d paths.Dirs, c config.Config, now time.Time) []Stage`, which part c reuses. A lag is measured from the oldest thing still waiting, never from a file's date:

| Stage | Lag | Bar | Budget |
| --- | --- | --- | --- |
| `activity`, each host | none: the newest chat in the host's folder, by names, types and modification times as `watchdog.go` reads them, over 14 days | | |
| `capture`, each host | now less the newest hook run, while host activity is newer than that run by more than the bar; else 0 | 5 min | 1 h |
| `organizing`, each host | now less the `ts` of the oldest entry past its session's watermark, in every session file of every project folder, by CT-4's function | 5 min | 24 h |
| `commit` | now less the oldest run-log record with `pages` > 0 after the newest commit run that committed or found nothing, while `wikigit.Unsaved` counts files; else 0 | 24 h on v1, which commits once a day; 2 s once the v2 store commits each pass (ST-05) | 48 h |
| `push` | now less the oldest commit run whose push has not succeeded since, while `Unsaved` counts commits; else 0 | 10 min | 72 h |

   `Reason` for organizing past its bar is the host's reason from `work.Status`, in its words. `Waiting` is the sessions waiting, or for commit the files written and not yet committed.
3. **Rows**: one for each stage but activity, at place 1300, in `status` and `doctor`, named `<host> capture lag` and so on: the lag beside the bar, "past its bar" when it is, the reason, and the count waiting. PASS within the budget, FAIL past it; UNKNOWN only when the evidence cannot be read, saying which.
4. **A host nobody uses**: no activity and no hook run in 14 days, and no install for it in the last day (its marketplace marker's modification time). Each of its rows is replaced by one PASS row, `<App>: not used in the last 14 days`.
5. **Counted losses**: events held back and events kept as a name only (CT-1's counts over 7 days, as a share of the runs CT-1 counts), and closed turns with a prompt and no stored reply (read through LG-10's `inboxv1` reader over the sessions changed in 7 days, as a share of closed turns). The bar of each is 0.5%. While a host's loss is past it, none of its capture rows (capture lag, capture freshness, hook failures, and CT-1's held-back row) reads PASS. Counts only.
6. **The search index row**, when it covers less than all: "; search scans the rest".
7. **The verdict**: `Report` gains `Verdict string` (JSON `verdict`, omitted when empty). With the switch on, `status` and `doctor` set one line: a counted loss past its bar first; else the stage furthest past its bar, by lag over bar, with its lag, its bar and its reason; else that every stage is within its bar. `runDoctor` prints it first when set.

## Owns and may touch

`internal/health/**` but `work.go` (DV-12b's); in `cmd/eva-md/commands.go`, only the printing of the verdict.

## Must not touch

`internal/hook`, `internal/update`, `internal/ledger`, `internal/work`, `internal/mcp`, `QuickNotice`, and the rows with the switch off.

## Accepted when

- `scripts/linux-check.sh` passes; with the switch off, SEAM-0c's goldens pass and no existing test changes.
- For each stage, a fixture whose file dates look fresh and whose oldest waiting thing is old gives the lag of the thing: an inbox file touched today whose oldest entry past its watermark is two days old reads two days, also in a folder's 65th-newest file.
- A last commit three weeks old with files waiting gives a FAIL commit row; files waiting under 48 hours, PASS with "past its bar".
- A host unused for 14 days has no row that says UNKNOWN; a day after its install it has its rows.
- A loss just past its bar leaves no capture row of that host PASS and leads the verdict; at the bar it does not.
- Verdict goldens: all within bars; organizing furthest with each of `work.Status`'s six reasons from a fake job; commit furthest.
- `status` writes nothing: the state folder and the wiki are the same before and after, by test. No host command or network call is made, and a marker planted in inbox entries appears in no row or verdict.
