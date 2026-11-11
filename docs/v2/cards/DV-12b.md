# DV-12b: v1's update and maintain under the worker, behind a switch

**Plan:** section 8, Stage 1, row DV-12 (and its dependency on SEAM-0 for "its command, hook handler and status row"); section 15.2; section 9 (DV-12 is a named exception to the freeze of v1's update); sections 13.3 point 4, 14.2 proposal B and 14.5 (why waiting work needs a reason). **Design:** `derive.md` sections 3.1 and 3.8 and its row DV-12 ("until the fast pass exists it runs v1's update and maintain as registered jobs"); `update-write.md` sections 1.2 and 3.4 and finding F4. **Size:** M. **Reviewers:** two (it changes the hook). **Depends on:** DV-12a and SEAM-0b; CT-1, CT-2 and CT-3 merged first (they change `internal/hook`, `internal/update`, `cmd/eva-md/update.go` and the usage line); EV-16's gate.

## Why

DV-12a builds the worker with no caller. This part puts v1's two background jobs under it, so a trigger that finds a job running is kept instead of dropped, and it makes `status` say why work waits. All of it sits behind the switch `worker` (`work.Switch`, default false). Off means v1 exactly: the hooks start `eva-md update` and `eva-md maintain` as today, and no row is added.

## What to build

1. **The command** `eva-md work`, in `cmd/eva-md/work.go` (new), registered in the command table at place 145, with these forms in its synopsis and one line in README's Commands list:
   - `eva-md work --wake --reason WORD [--session host:id ...] [--sweep=YYYY-MM-DD] [--maintain [--push-only]]`: one wake. `--session` and `--sweep` mark the update job, with keys `<host>-<id>` and `sweep-<day>`; `--maintain` marks the maintain job, key `daily`, or `push` with `--push-only`. `WORD` is one of SEAM-0b's reasons, or `tick`. It calls `work.Wake` and exits 0, also when the worker is busy.
   - `eva-md work --tick`: a wake with no mark, which runs marks a busy lock left. HO-22's clock will call it.
   - **A session's key, as the worker is merged.** The worker refuses a key that `paths.SafeName` would change or that holds an upper-case letter, and a mark carries its host in a field of its own (`Mark.Host`). A session id is the host's to choose, so the key is `<host>-<id>` only when that is a key the worker takes; for any other id it is `<host>-` and a name made from the id that is lower-case and the same every time, and the update job finds a mark's session by making the key of each session that has a backlog. A mark that matches no such session is done. `work.Wake` also refuses a kind that is not registered, so both jobs are registered before any wake.
   - `eva-md work --status`: what waits past its bar or is held, counted by reason, with the oldest age, and for a hold its class and next try. Counts only.
2. **Two jobs registered** with `work.Register` from the same file:
   - `update`, priority 3: runs `update.Job.CatchUp` for the sessions of its marks, with the sweep's day as `SweepDay`, the same `Runner`, clock, version and day's log as `runUpdate`, and `Manual` false. Marks of different reasons run as separate calls, so the trigger CT-2 records stays true. A summary that stopped `busy` (another holder of `update.lock`, such as a person's `--foreground` run) is the outcome `busy`.
   - `maintain`, priority 4: runs what `runMaintain` runs, with `--push-only` for a `push` mark, its lines to the day's log. Change `maintain.go` only so that the job learns that `maintain.lock` was held; what it prints and its exit codes stay.
   - Each job's `Waiting` gives, for each session with a backlog by its watermark, one reason: `WaitOff` when `updates = false` or the chat is off the record; `WaitSignIn` when the host is held for `auth`; `WaitHeld` for any other hold, a backoff or a quarantine, with v1's reason as the class and the time it ends as the next try; `WaitBudget` when today's calls reached `daily_cap`; otherwise `WaitRule`. Read hosts' states with `update.Recent` and `update.Host`, and add to `update.HostState` one exported field, the end of the host's backoff, since `backoffUntil` is not exported. List sessions within the update backlog row's bounds (`internal/health/status.go`: 64 newest files a folder, 4,096 entries in all).
3. **The wake in the hook**: `internal/hook/wake.go` (new) registers a handler at `atLaunch`. When `work.Switch.On` the config, it rewrites each due job whose `args[0]` is `update` or `maintain` into the `work --wake` form with the job's reason and the same sessions, sweep day and push flag, and sets `git` on it, since the worker may run maintain. Off, it changes nothing.
4. **The status row**: `internal/health/work.go` (new), a producer at place 1210, in `status` only, with a wiki and the switch on. It prints what `work --status` prints. It is PASS, and FAIL only when something waits for sign-in, as the updates row fails on `auth`.
5. **The usage line** gains `work`. `TestUsageLineIsUnchanged` (`cmd/eva-md/docs_test.go`) and `TestUsageListsEveryCommand` (`cmd/eva-md/main_test.go`) gain that one word and nothing else. These are the only existing tests that change.

## Owns and may touch

`cmd/eva-md/work.go` and `work_test.go` (new), `internal/hook/wake.go` and `wake_test.go` (new), `internal/health/work.go` and `work_test.go` (new), `internal/update/runlog.go` (the one field), `cmd/eva-md/maintain.go` (the busy signal), the two tests named in item 5, and the one line of `README.md`.

## Must not touch

`internal/work` (DV-12a's), `hook.go`, `trigger.go`, `spawn.go`, `uninstall.go`, `update.go`, any plugin byte, and what v1 does with the switch off.

## Accepted when

- `scripts/linux-check.sh` passes, with no existing test changed beyond item 5.
- With the switch off, or `[switches] worker = false`, the hook spawns today's arguments, and `status` and `doctor` print SEAM-0c's golden output, by test.
- With it on, each of today's asks reaches `env.spawn` as its `work --wake` form, with `git` set, by a test for each.
- A session whose id holds an upper-case letter, and one whose id holds a character `paths.SafeName` changes, are each woken and updated, and two ids that differ only in letter case get two marks: a test for each.
- With it on and a fake runner held on a channel, 50 wakes for 50 sessions during one update: after release every session's backlog has a run-log record, and the fake runner never ran two calls at once.
- A person's `--foreground` update holding `update.lock`: the worker's job ends `busy`, its marks stay, and `--tick` runs them afterwards.
- On fixture run logs and inboxes, `work --status` and the row give each of the six reasons, and the next try equals v1's backoff end.
- A marker planted in inbox entries and session ids appears in no line `work --status`, the row or the journal print or write.
- EV-16's comparison of the old and new binary, switch off, shows no gated metric worse. Report the figures.
