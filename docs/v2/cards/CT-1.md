# CT-1: what the hooks held back and what they delivered (LG-01, DL-02)

**Plan:** section 8, Stage 0, the counters release row and the list "What the counters release counts"; decision 17. **Part of:** the counters release, a v1 point release that changes none of v1's behaviour (2026.11.7's) and no plugin byte. **Depends on:** SEAM-0a, SEAM-0c, and EV-16's baseline, which must be taken before any hook changes.

Rules for every part of the counters release, beside those in the cards' README:

- **Content-free.** A counter may read stored text to count it. It never writes, prints, logs or returns any of that text, nor a path under a home folder, a project name, a branch name or a session id. Each part has a test that plants a made-up marker in every text a counter reads and finds it in no output and no file the part writes.
- **No behaviour change.** What v1 captures, organizes, recalls, commits and prints stays the same, except for the new fields, rows, flags and commands this card names. Every existing test passes unchanged.
- **No plugin byte changes** (SEAM-0d's hash test holds it), and nothing is added to a hook's declaration.
- **The hook stays as fast.** EV-16's gate judges it; report the alternating-run figures.

**Design:** `ledger.md` row LG-01 and section 11 form 1; `deliver.md` row DL-02. **Size:** S to M. **Reviewers:** two (it changes the hook).

## Why

v1 cannot say how often a hook kept only an event's name because its recovery pass was behind, how often a brief or a recalled statement was really delivered, or how long hooks take over a week. The field report had to leave all three as unknown, and three thresholds of the plan's section 8 wait on them.

## What to build

1. **Held back.** When a hook keeps only an event's name, or nothing, because the chat is paused or its recovery pass is behind, the run's record says which kind was held: a prompt, a reply, a tool line or a brief. Counts only.
2. **Delivered.** For each run that can add text to a chat, the run's record says, with no text: the kind of start or prompt, which routes added something (rules, constraints, state, exchanges, a recalled statement, pointers, the reminder), the size added in characters, whether the state shown was older than the newest captured exchange, whether an update was pending, whether a fallback was used, and whether the hook passed its deadline and delivered nothing.
3. **A week of durations.** 2026.11.7 keeps the last 20 runs of each event. Keep a week's distribution for each event instead, in a bounded file (a fixed number of buckets for each day, not a line for each run), without the hook doing more work than one small write it already does.
4. **Two rows** in `status` and the doctor, through SEAM-0c's registry: what was held back in the last 7 days, by kind; and what was delivered, by route, with the share of chat starts whose state was stale or pending.

## Owns and may touch

`internal/hook/**` (the run record and the points where a hold or a delivery is decided), `internal/health/**` (two rows, new files), and their tests.

## Must not touch

What a hook stores in the inbox or returns to the host. `internal/update/**`. `cmd/eva-md/**`. Any template.

## Accepted when

- `scripts/linux-check.sh` passes with no existing test changed.
- A test holds an event behind recovery and reads the kind held in the run's record, for each of the four kinds; a test for each route reads its delivery fields.
- The week's file stays under a stated size after a simulated month of runs, by test, with the clock injected.
- The planted-marker test finds no text in any record, row or file this part writes.
- Alternating runs of the old and the new binary on the benchmark show no gated metric worse than EV-16's rule allows; report the figures.
