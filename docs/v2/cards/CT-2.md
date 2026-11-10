# CT-2: what the update cost and why work waited (DV-01)

**Plan:** section 8, Stage 0, the counters release row and the list "What the counters release counts"; decision 17. **Part of:** the counters release, a v1 point release that changes none of v1's behaviour (2026.11.7's) and no plugin byte. **Depends on:** SEAM-0a, SEAM-0c, and EV-16's baseline, which must be taken before any hook changes.

Rules for every part of the counters release, beside those in the cards' README:

- **Content-free.** A counter may read stored text to count it. It never writes, prints, logs or returns any of that text, nor a path under a home folder, a project name, a branch name or a session id. Each part has a test that plants a made-up marker in every text a counter reads and finds it in no output and no file the part writes.
- **No behaviour change.** What v1 captures, organizes, recalls, commits and prints stays the same, except for the new fields, rows, flags and commands this card names. Every existing test passes unchanged.
- **No plugin byte changes** (SEAM-0d's hash test holds it), and nothing is added to a hook's declaration.
- **The hook stays as fast.** EV-16's gate judges it; report the alternating-run figures.

**Design:** `derive.md` row DV-01. **Size:** S to M. **Reviewers:** one.

## Why

The run log does not say which trigger started an update, which job left because another was running, why a chunk was skipped, or which model answered. So the long tail the field report measured (one chat in twenty on file later than 88 minutes) cannot be traced to a cause, and the counted week runs on a release that already has a daily sweep the first report's release lacked.

## What to build

1. **The trigger's name on each run.** The hook passes the name of what started the job (session end, compaction, a stop past the size limit, a prompt after a gap, the chat-start catch-up, the sweep, a manual run) as a flag on the command it spawns, and the run's record keeps it.
2. **Busy exits.** A job that leaves because another holds the lock is counted, with the session it was started for kept as a hash that cannot be turned back into the id.
3. **Skips with their reason**, and for an answer rejected whole, the rule that rejected it and where, which v1 drops today.
4. **The model a run reported using**, beside the one asked for, where the host's output gives it.
5. **A read-only report**, `eva-md status --updates`: outcomes and reasons; refusals by rule; tokens and duration (median, 95th percentile, maximum); the time from a chat's boundary to memory on file by trigger, with the sweep shown apart; busy exits; calls for each day against the cap. Numbers only.

## Owns and may touch

`internal/update/**` (the run log and the points that write it), `internal/hook/trigger.go` and `spawn.go` (the flag), `cmd/eva-md/update.go` (accepting the flag), the status command's file for `--updates`, and a new file in `internal/health` for the report.

## Must not touch

What a pass sends to a model, how it checks an answer, what it writes to the wiki, or when a trigger fires. The thresholds stay as they are.

## Accepted when

- `scripts/linux-check.sh` passes with no existing test changed.
- On a fixture run log the report prints exact counts, and a test scans its output for the planted marker and for any session id and finds none.
- Each trigger's name reaches the record, by a test for each trigger; an old binary's record with no trigger field still reads.
- A job that finds the lock held leaves one counted record and changes nothing else, by test.
- The flag the hook adds is one the installed stable binary of 2026.11.5 to 2026.11.7 would refuse, so say in the report how an upgrade in which an old hook spawns a new binary, or a new hook an old binary, behaves, and hold it with a test: neither may fail a hook or lose an update.
