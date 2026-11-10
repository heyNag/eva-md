# CT-3: capture checked against the host's own record (LG-02)

**Plan:** section 8, Stage 0, the counters release row and the list "What the counters release counts"; decision 17. **Part of:** the counters release, a v1 point release that changes none of v1's behaviour (2026.11.7's) and no plugin byte. **Depends on:** SEAM-0a, SEAM-0c, and EV-16's baseline, which must be taken before any hook changes.

Rules for every part of the counters release, beside those in the cards' README:

- **Content-free.** A counter may read stored text to count it. It never writes, prints, logs or returns any of that text, nor a path under a home folder, a project name, a branch name or a session id. Each part has a test that plants a made-up marker in every text a counter reads and finds it in no output and no file the part writes.
- **No behaviour change.** What v1 captures, organizes, recalls, commits and prints stays the same, except for the new fields, rows, flags and commands this card names. Every existing test passes unchanged.
- **No plugin byte changes** (SEAM-0d's hash test holds it), and nothing is added to a hook's declaration.
- **The hook stays as fast.** EV-16's gate judges it; report the alternating-run figures.

**Also depends on:** LG-10, whose reader this uses for v1's files. **Design:** `ledger.md` row LG-02 and section 11 form 3. **Size:** M. **Reviewers:** two (it reads the host's transcripts).

## Why

Nobody knows how much of a chat v1 captures. The field report could compare one session by hand and found the host's file with more prompts and replies than the inbox. The plan's bar is 99.5% of turns and 99% of tool calls, and its thresholds for a held spool wait on a measured rate.

## What to build

A read-only command, `eva-md audit capture [--session ID | --last N]`, registered through SEAM-0a's table, and a scorer in `internal/ledger/audit`. For each session it can pair with a host record it prints numbers only:

1. **Coverage:** prompts that reached the agent and are in the inbox; final replies; tool calls; each as found, missing and extra.
2. **Copies:** exact repeats by kind (prompt, reply, tool line), and how many pair a hook's line with a recovered one.
3. **Gaps by reason**, and turns that have a prompt and no stored reply, by cause (interrupted, failed, no reply in the payload, hook never ran).
4. **Prompts that arrive inside a voice hand-over**, counted by their prefix, never decoded here.
5. **Sessions by what v1's files show:** a subagent's id, a program's start on Claude Code, an end with no start, no turn at all.
6. **Recoveries with no host record,** split into those whose payload named none and those whose path was named and refused. This needs one content-free field in the recovery record that `internal/catchup` writes.
7. **Host sessions with no eva-md file,** counted by file name in the host's own store, with those left out by design (excluded, temporary, cloud and spare folders) counted apart as an upper bound.

It reads the host's transcripts under the same trust rules as v1's recovery, opens nothing outside the hosts' session folders, and writes nothing but its report to standard output.

## Owns and may touch

`cmd/eva-md/audit.go` (new), `internal/ledger/audit/**` (new), `internal/catchup` only for the one field of item 6, and their tests.

## Must not touch

`internal/hook/**`, `internal/inbox/**` beyond reading through LG-10's reader, any writer of the inbox.

## Accepted when

- `scripts/linux-check.sh` passes.
- On a made-up transcript and inbox with planted losses, extras and copies, for both hosts' formats, the report gives the exact counts.
- The planted-marker test finds no text, session id, path or project name in the output.
- A transcript the trust rules refuse is counted as refused and never opened, by test.
- Nothing off the record is read into any count that could reveal it: a paused range counts as paused, with no kinds.
