# HO-12c: one line, prepared outside the hook

**Plan:** section 6.5 (design 6), section 5.4 (rows "Memory stops working" and "Notices on a healthy install"), section 8 Stage 1 row HO-12, section 13.4 (the two files that misled a reader). **Design:** `hosts.md` sections 7.4 (conditions, relevance, selection), 7.6 (`notice.json`) and 7.7 (rows 2, 3, 6, 8, 9, 14 and 15); `ops.md` findings ops-1 and ops-5, and miss-2. **Part of:** HO-12: a, b, c (this). **Size:** M. **Reviewers:** two (it changes the hook). **Depends on:** HO-12b; SEAM-0b (this adds a handler); CT-1, which edits `resume.go`; EV-16's gate.

## Why

v1 works out its notice inside every chat-start hook (`QuickNotice`, called from `internal/hook/resume.go`) and shows the first problem in code order, so a trust flag for a host nobody uses hides everything else and shows at every Claude Code start for good. Failures are pooled across a host's events, and every line ends "run eva-md doctor".

## What to build

All with HO-12b's switch `health` on. Off, the hook calls `QuickNotice` as today and nothing below runs.

1. **Conditions**, `internal/health/conditions.go` (new): one evaluator over v1's evidence and HO-12b's `Stages`. Closed list, most severe first, a tie going to the earlier row:
   - high: `config.unknown_keys`; `wiki.unreadable`; `wiki.instance`; `trust.review` (the `needs-trust.json` flag with no Codex SessionStart heartbeat newer than it); `capture.silent{host}` (capture past its budget); `hook.failing{host,event}` for SessionStart and UserPromptSubmit; `update.signin{host}`; `push.diverged`;
   - medium: `hook.failing` for any other event (half or more of that event's last 4 or more runs in 7 days, never pooled); `update.failing{host}` (3 in a row); `update.quarantined{host}`; `wiki.schema_newer`; `recovery.drift{host}`; `commit.stale` (commit past its budget, or failing for 48 hours); `push.stale` (push past its budget).
   A condition about a host is relevant only while HO-12b counts the host as used; one not relevant is never shown. Each type has one line of at most 160 characters, starting `eva.md: `, saying what is wrong and the one step; no line says "run eva-md doctor".
2. **Preparing**: `health.Prepare(ctx, d, now) error` writes `<state>/health/notice.json` atomically (folder 0700, file 0600, through `paths.OpenRoot`): `{"v":1,"prepared":"<RFC 3339>","lines":{"<host id>":{"id":"<condition id>","text":"<line>"}}}`, for each host's chat start the most severe relevant condition, leaving out that host's own `capture.silent` and `trust.review`, which its running hook disproves. A lock in `d.Locks()` keeps two preparers apart. It reads local files and runs git locally; no host command, network or model. It also removes `needs-trust.json` once `trust.review` is over, and `commit/push-retry` once the hook keeps the new name (item 4).
3. **The command**: `eva-md doctor --notice` runs `Prepare`, prints nothing, and exits 0 whatever happens. The doctor's synopsis gains the flag, and README's doctor line gains words for it.
4. **The hook**: `internal/hook/notice.go` (new) registers an `atBriefed` handler that asks for the job `doctor --notice`, with `git` false and a new reason word `health` added to SEAM-0b's list in `handlers.go`, when `notice.json` is missing or older than 30 minutes (one stat). DV-12b's wake leaves this job alone, since it rewrites only `update` and `maintain`. In `resume.go`, the call to `QuickNotice` becomes, at the same moments, a read of this host's prepared line, shown when it was prepared in the last 2 hours and its id was not shown in the last 24 hours; showing it writes `<state>/health/shown.json` (`{"id","at"}`) after the output. In `maintain.go`, the push check's timer is named `commit/push-checked`, so no healthy state holds a file whose name says a retry is pending.

The MCP server keeps `QuickNotice` (HO-23 moves it).

## Owns and may touch

`internal/health/conditions.go`, `notice.go` and their tests (new); `internal/hook/notice.go` and `notice_test.go` (new); in `internal/hook`, the notice call in `resume.go`, the timer's name in `maintain.go` and the reason word in `handlers.go`; in `cmd/eva-md/commands.go`, the `--notice` flag and the doctor's synopsis; README's doctor line.

```paths
internal/health/conditions.go
internal/health/conditions_test.go
internal/health/notice.go
internal/health/notice_test.go
internal/hook/notice.go
internal/hook/notice_test.go
internal/hook/resume.go
internal/hook/maintain.go
internal/hook/handlers.go
internal/hook/handlers_test.go
internal/hook/hook.go
internal/hook/wake_test.go
cmd/eva-md/commands.go
cmd/eva-md/notice_test.go
README.md
```

## Must not touch

`internal/update`, `internal/mcp`, `internal/install`, `internal/work`, HO-12b's rows, what a hook stores in the inbox, any plugin byte, and anything with the switch off.

## Accepted when

- `scripts/linux-check.sh` passes; with the switch off no existing test changes and the hook's output is today's.
- **A replayed healthy month**: a test builds a month of a healthy install's evidence with an injected clock (both hosts' heartbeats, updates that succeed, a daily commit and push, Codex used on alternate weeks, a trust flag met the day after install, an index that covers part of the inbox), runs `Prepare` every 30 minutes of activity and a chat start on each host every working hour: no line is shown.
- The same month with one fault planted, for each condition type, shows that type's line at the next start on a host where it is relevant, and once a day after, not more.
- Rows 2, 3, 6, 8, 9 and 15 of hosts.md's section 7.7 pass on v1's evidence; row 15 shows the more severe line, then the other once it closes.
- A golden of every line; none over 160 characters or holding "run eva-md doctor".
- In the hook: with no `notice.json` nothing is shown and the job is asked for; a prepared line is shown as written; one prepared over 2 hours ago is not; a shown id is not shown again within 24 hours.
- In `internal/hook/notice_test.go`, a healthy fixture state built by running the hook and `Prepare` holds no file whose name has `needs`, `retry`, `fail`, `error`, `stale`, `pending` or `missing` in it.
- A marker planted in inbox entries appears in neither file this part writes.
- `scripts/linux-gate.sh` against the commit this part started from, switch off and on; report the figures.

## The lead's rulings of 2026-11-14

Written when HO-12b merged (its card's "As merged" says what it left for this part).

1. **A test file of the command is in the paths block**: `cmd/eva-md/notice_test.go` (new) holds the tests of `doctor --notice` (it prints nothing and exits 0 whatever happens, also with the switch off and with no config) and the one HO-12b could not write: with the switch on, the doctor prints the verdict first.
2. **HO-12b's `Stage` has three fields beyond its card's list**: `Unread`, `Notes` and `Off`. A stage that is `Off` (a host eva-md is not installed for, `updates = false`, `commits = false`) raises no condition: `capture.silent`, `commit.stale` and `push.stale` are raised only from a stage that is on and past its budget. A stage that could not be read (`Unread`) raises none either. Whether a host is used is `unusedHost` in `verdict.go`; this part is in the same package and calls it, and changes nothing in HO-12b's files.
3. **Each handler at a chat's launch is recovered in `handlers.go`** (DV-12b's card, "The SEAM-0b point"): a handler that panics starts nothing of its own and stops no other handler, and the hook still exits 0. This part adds that with its own handler, by test, with the switch off as well as on, since it is a guard and no behaviour a person sees.
4. **The documents** are the lead's at the merge: give one sentence a line for `docs/spec/health.md`'s "Since the split" (it has about 800 bytes of room, so few and short; say which of them could go to `docs/spec/hooks.md` or the spec that holds the chat-start notice instead) and for `docs/known-limitations.md`. README's doctor line is this part's own, in the paths block.
5. **The check** is the scoped one the order names, not the whole suite; the latency gate is run once, against origin/main, when the machine is quiet, with the switch off and with it on.

## The lead's rulings at the merge, 2026-11-14

1. **Three more files are in the paths block**, each for a reason the reviews gave: `internal/hook/hook.go` (the hook's output write says whether it reached the host, so a line that did not is not recorded as shown), `internal/hook/handlers_test.go` (its list of the package's handlers gains this part's two) and `internal/hook/wake_test.go` (the reason word `health` is the hooks' and not the worker's wake's, so `eva-md work --wake --reason health` stays refused and `wake.go` is unchanged).
2. **A line is shown when it was prepared in the last 24 hours, not 2** (item 4 and its acceptance line said 2). With 2 hours, a chat start more than two hours after the last one showed nothing and only asked, so a person whose starts are hours apart never saw a line. A line prepared before its condition closed can so be shown once; the same start asks for a fresh check (docs/known-limitations.md says so).
3. **The closed list stays closed**: an update held for a day by a failed MCP server has no condition of its own, and shows once three passes in a row have failed (`update.failing`); `status` and `doctor` show the hold at once.
4. **The hosts' own event names are no names of a fault.** `heartbeats/claude-code/PostToolUseFailure.json` and `StopFailure.json` are named for the host's events, which a healthy install fires whenever a tool call fails; plan section 13.4 is about files eva-md names for a state of its own (`needs-trust.json`, `push-retry`). The healthy fixture's word check passes over the hosts' event names under `heartbeats/`.
