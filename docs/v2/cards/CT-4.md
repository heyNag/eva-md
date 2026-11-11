# CT-4: a content-free report of how v1 has behaved (HO-02)

**Plan:** section 8, Stage 0, the counters release row and the list "What the counters release counts"; decision 17. **Part of:** the counters release, a v1 point release that changes none of v1's behaviour (2026.11.7's) and no plugin byte. **Depends on:** SEAM-0a, SEAM-0c, EV-16's baseline, and CT-1 and CT-2, whose records it reads. It is built while they are fixed, on the lead's local branch `lead/ct-all`, which joins CT-1, CT-2 and CT-3 as built; the lead moves it onto `main` once they are merged, before its review.

Rules for every part of the counters release, beside those in the cards' README:

- **Content-free.** A counter may read stored text to count it. It never writes, prints, logs or returns any of that text, nor a path under a home folder, a project name, a branch name or a session id. Each part has a test that plants a made-up marker in every text a counter reads and finds it in no output and no file the part writes.
- **No behaviour change.** What v1 captures, organizes, recalls, commits and prints stays the same, except for the new fields, rows, flags and commands this card names. Every existing test passes unchanged.
- **No plugin byte changes** (SEAM-0d's hash test holds it), and nothing is added to a hook's declaration.
- **The hook stays as fast.** EV-16's gate judges it; report the alternating-run figures.

**Design:** `hosts.md` row HO-02. **Size:** S to M. **Reviewers:** one.

## Why

The first field report was made by an agent writing its own scripts over eva-md's files, with the owner as the last check on privacy. The second report should be one command's output: stable, complete and safe by construction.

## What to build

A report mode on the doctor, `eva-md doctor --report`, that prints counts and durations and nothing else, in a stable format a later report can be compared with (JSON, with a version). It gathers:

1. **Operations:** the days the daily job ran in the last 14; commit and push outcomes by class; the trust flag's age; failures for each hook event; and the kind of notice a chat start would show now. v1 keeps no record of the notices it showed, and this package changes no writer, so notices by kind over days wait for a later package that counts them.
2. **What the other parts record:** CT-1's held and delivered counts and the week's durations, and CT-2's update figures, each through the function its package exports, never by reading its file a second way. The report does not run or read CT-3's capture audit, which keeps no file and reads the hosts' own records: the second field report runs `eva-md audit capture` beside it.
3. **Backlog:** the age of the oldest event not yet organized, read from the event and past v1's limit of the 64 newest files of a folder, and how many sessions wait. Export the function that gives them: HO-12b will call it.
4. **The record:** sessions, entries by kind, bytes; redaction markers by kind; and what 2026.11.7's redaction rules would catch in text stored under an older release, by class.
5. **The wiki:** pages by kind; runs of `eva-md system write` and edits under the wiki folder, counted from tool lines; code-status entries with no clone.

## Owns and may touch

`internal/health/**` (new files for the report), the doctor command's file for `--report`, and their tests.

## Must not touch

What `doctor` and `status` print without the new flag. Any writer.

## Accepted when

- The report says which eva-md wrote it (its version) and over which days it counted, so that two reports can be compared; and a report made on a home with no heartbeat week, no run log or no wiki prints zeros or leaves the part out by a stated rule, never an error.

- `scripts/linux-check.sh` passes with no existing test changed.
- On a fixture state folder the report prints exact counts, and the planted-marker test finds no text, path, session id, project name or branch name in it.
- The report's format has a version and a test that pins its keys.
- The redaction count reads stored text, prints counts by class only, and changes no stored byte, by test.
- `doctor --report` makes no network call and no model call, and repairs nothing.
