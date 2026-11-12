# CT-3b: the capture audit pairs a session by its id too (LG-02)

**Plan:** section 8, Stage 0, the counters release row and "What the counters release counts"; section 13, the lines "Events held back; turn and tool coverage" and "No transcript in 545 of 580 recoveries". **Part of:** the counters release. **Follows:** [CT-3](CT-3.md), on whose fixed branch it is built. **Size:** S. **Reviewers:** two (it opens the hosts' own records): the contract, and privacy.

The rules for every part of the counters release hold (see [CT-3](CT-3.md)): content-free, no behaviour change, no plugin byte changes, the hook stays as fast.

## Why

`eva-md audit capture` compares what v1 stored with the host's own record of a session. As built it finds that record only through the session's recovery record, when a hook's payload named a transcript. On the one real install most recovery records name none, so the audit would cover a few sessions in a hundred, and none of the kind the plan most wants to know about: whether a person's long chats are captured whole. The host's file is there all the same, in the host's own store, which the audit already walks to count host sessions with no eva-md file.

## What to build

1. **A second way to pair.** When a session's recovery record names no transcript, or names one that is gone, look for the host's file by the session's id among the files the audit already lists in the host's store: a Claude Code session file is named for its id, and a Codex one holds the id in its name. Take it only when exactly one file matches.
2. **The same trust rules.** A file found by id is admitted by recovery's own path rules (`catchup.Admits`) and opened by recovery's own opener, with its check on Codex that the file says it is that session's. A file that fails either is counted as refused and never opened. Nothing outside the hosts' session folders is listed or opened, and no link is followed out of them.
3. **Say how each session was paired.** The totals give the sessions paired by the recovery record, paired by id, and not paired, with why not (no file by that id, more than one, refused, gone). Every coverage figure of the report is over the sessions it names as paired, both ways together, and the two ways are also given apart, so that a difference between them can be seen.
4. **A session's own line** says which way it was paired, in a fixed word.
5. **Off the record.** Everything the audit does for a session paired by its recovery record, it does for one paired by id: the pause ranges of the inbox and of the host's record are joined, and what lies inside them is counted in nothing.
6. **Bounded.** A session's host file is read once, under the audit's present limits on a row's and a file's size. Say in the report what the audit costs on a store of a few hundred sessions, by a test on made-up files, with no sleep and no clock deciding an outcome.

## Owns and may touch

`internal/ledger/audit/**`, `internal/catchup/record.go` and its test (read-only exports only), `cmd/eva-md/audit.go` and its test, and the sentences on the capture audit in `README.md`, `docs/design.md` and `docs/verification.md`.

```paths
internal/ledger/audit/**
internal/catchup/record.go
internal/catchup/record_test.go
cmd/eva-md/audit.go
cmd/eva-md/audit_test.go
README.md
docs/design.md
docs/verification.md
```

## Must not touch

`internal/catchup`, beyond calling what CT-3 exported and, if its opener cannot open a file that has no recovery record, one more read-only export in `internal/catchup/record.go` that opens a path recovery's rules admit, with the Codex check that the file is that session's, and does nothing else. What recovery itself reads: a hook still recovers only from a transcript its payload names. Any writer: the audit writes nothing but its report.

## Accepted when

- `scripts/linux-check.sh` passes.
- A session whose recovery record names no transcript, and whose host file is found by its id, is paired, counted as paired by id, and scored exactly as the same session paired by its recovery record is: one test builds both and compares the figures.
- For each host: no file by that id, and two files that match it, leave the session not paired, with the reason counted. A file with the right name outside the hosts' session folders, one reached through a link that leads out of them, and a Codex file that says it is another session's, are refused and never opened: a test for each, which fails if the file is opened.
- The planted-marker test covers a session paired by id: no text, session id, path or project name of it is in the output.
- A range said off the record in a session paired by id is counted as paused, with no kinds, whichever side shows the range.
- A reviewer's own mutations (the path check dropped, the identity check dropped, the first of two matches taken, a refused file opened) are each noticed by a test.
