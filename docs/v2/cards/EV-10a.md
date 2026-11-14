# EV-10a: v1's design split into component specs, and dated decision records

**Plan:** section 8 Stage 1 row EV-10, decision 15, section 9 ("Documents a team can work from"). **Design:** `evaluate.md` section 12.1, shrunk by `critique-simplicity.md` (no guarantees as data, no mutation per item); `critique-delivery.md` issue 18 splits the row in two. **Size:** M. **Reviewers:** one. **Depends on:** the counters release merged (CT-1 to CT-4, with the three sentences for v1's documents), since it changes the documents this card freezes; EV-10b follows it.

## Why

`docs/design.md` (394 KB) and `docs/verification.md` (183 KB) are too big to load, and every behaviour change edits them. Decision 15 makes per-component specs and dated decision records the truth, and replaces the tests that police the old prose. Nothing v1 does changes; no switch.

## What to build

1. **Specs**, `docs/spec/<component>.md`. Every word of `docs/design.md` and `docs/verification.md` moves into exactly one block of one spec. A block opens with `<!-- v1 <source path> <ordinal> -->` (its order in the source, from 001) and ends at the next such comment or `<!-- /v1 -->`. Inside a block one may only rewrap lines, change heading levels and fix link targets. Start from design.md's "Components" table; a verification bullet joins the component of its packages; the rest goes into an overview spec. Each spec opens with a short new header (component, paths covered, "v1 as built, frozen on <date>") and ends with `## Since the split`, where later packages write behaviour changes. The system pages spec says there that no background step writes a system page; only `eva-md system write` does (`cmd/eva-md/system.go`).
2. **Limits.** Every file at most 24,576 bytes; no line over 400 characters except a table row, and none over 2,000. Text too big for one spec (the 34 KB heading "Setup and init of a new wiki", the 23 KB bullet "Health, setup and CLI") is split at a paragraph or sentence into blocks in two specs.
3. **`docs/spec/INDEX.md`**: each file in `docs/spec/` with its component, paths and status; it also lists `docs/spec/store-format.md` and `internal/ledger/SPEC.md` as v2 specs, unedited.
4. **Decision records**, `docs/decisions/<YYYY-MM-DD>-<slug>.md`, fields Status, Date, Context, Decision, Consequences, Reopens, Reopened by. Each of v1's 17 decisions becomes one, in blocks as in item 1, dated by the first commit that held it (`git log -- docs/decisions.md`). The rest of decisions.md, and design.md's "Changes to decisions", move whole into records. "Reopened by" names each row of plan section 7 whose last column names that decision, all taken at their recommendation on 2026-11-10. One new record, dated 2026-11-10, holds plan decision 15.
5. **The old files become short indexes.** `docs/design.md` and `docs/verification.md` say they are frozen and split, and list every heading (and for verification.md every bold bullet title) beside the file that holds it, so a comment citing `docs/design.md "Validation"` still leads there. `docs/decisions.md` lists every record and the fields.
6. **Checks**, in a new package `internal/spec` (a `doc.go` and tests):
   - No word changed: for each of the three sources, its blocks (in `docs/spec/` and `docs/decisions/`) in ordinal order, with comments, link targets and tokens made only of `#` removed and whitespace collapsed, hash (SHA-256) to the value the test holds, computed the same way from the file at the split's base commit.
   - Item 2's limits hold for `docs/spec/`, `docs/decisions/` and the three indexes.
   - Each index names every heading of its old file against a file that holds it; `INDEX.md` and `docs/decisions.md` list exactly the files present; every path they name exists.
   - Every record's name, fields and date agree. Every relative link outside code resolves.
7. **Prose tests replaced** (decision 15). Delete `TestVerificationCountsTheKeptLines` and `TestVerificationBulletsNameTheirPackages` from `cmd/eva-md/docs_test.go` (EV-10b brings back a cited-test check). `TestDesignsPassKeyExamples` (`internal/redact/passwords_test.go`) holds the keys it reads from design.md today and opens no file. `TestInstallScriptVersionPinThroughThePipe` (`cmd/eva-md/release_test.go`) drops its design.md check. `TestThePublicRepoUsesNeutralWording` (`cmd/eva-md/wording_test.go`) reads every `.md` under `docs/` at any depth, not only `docs/*.md`; `docs/v2` passes today. `TestLowestRowIsTheLowestVersionRecorded` (`internal/host/adapter/spec_test.go`) reads `docs/*.md` for the host versions eva-md was checked on: it reads `docs/spec/*.md` too, so no version it sees today leaves its sight.
8. **Pointers.** In `AGENTS.md`: the three "Read first" lines on design, decisions and verification, the phrase "docs/design.md still says what v1 does", and the rule "If a change alters behaviour, update docs/design.md ..." now name the specs, `## Since the split` and dated records. In `README.md`: the same three lines under "Documentation" and the `verification.md#live-accuracy-run` link.

## Owns and may touch

`docs/spec/**` (new files only), `docs/decisions/**` (new), `docs/design.md`, `docs/verification.md`, `docs/decisions.md`, `internal/spec/**` (new), `cmd/eva-md/docs_test.go`, `cmd/eva-md/wording_test.go`, the one check in `cmd/eva-md/release_test.go`, `TestDesignsPassKeyExamples`, `TestLowestRowIsTheLowestVersionRecorded`, and the lines of `AGENTS.md` and `README.md` named in item 8. `AGENTS.md` stays within 24,576 bytes.

```paths
docs/spec/**
docs/decisions/**
docs/design.md
docs/verification.md
docs/decisions.md
internal/spec/**
cmd/eva-md/docs_test.go
cmd/eva-md/wording_test.go
cmd/eva-md/release_test.go
internal/redact/passwords_test.go
internal/host/adapter/spec_test.go
AGENTS.md
README.md
```

## Must not touch

Any existing non-test Go file; `docs/spec/store-format.md`, `internal/ledger/SPEC.md`, `docs/known-limitations.md`, `docs/releasing.md`, `docs/v2/**`, `cmd/live-accuracy`; any workflow or script. While this builds, no other package edits the three old files; one merged after it writes into a spec's `## Since the split`.

## Accepted when

- `scripts/linux-check.sh` passes.
- Each check of item 6 fails on a fault seeded in a copy under `t.TempDir()`: a word changed, dropped or added; a file over a limit; a heading missing from an index; a bad record name; a broken link.
- Outside `internal/spec`, which reads them as indexes, no test opens the three old files (a reviewer greps), and `git diff --stat` shows no existing non-test Go file changed.
- The report gives the base commit, the three hashes, the command that recomputes them from `git show <base>:<file>`, and each spec with its size.

## As merged

Merged on 2026-11-14 (fc7bb4f, from `pkg/ev-10a-r1`, after one review, one round of fixes and a verification that accepted it). 40 specs, 24 records (23 dated 2026-10-30, and one of 2026-11-10 for the plan's decision 15), the three old files as indexes, and `internal/spec`, which is tests only.

- **How a change is written now.** No word inside a block changes: `internal/spec` holds each source's blocks to a hash of the source as it stood at 3319eef. What changed after the split goes under the spec's `## Since the split`, dated and named for its card, one sentence a line (a line holds 400 characters at most, a file 24,576 bytes). The lead wrote the first four the same day: EV-16e's in `performance.md`, ST-04b's in `live-accuracy.md`, EV-17f's in `checks.md` and HO-11's in `install.md`.
- AGENTS.md says where behaviour is written now, names `internal/spec`, and no longer says that two tests read `docs/design.md`; it has 4 bytes of room.
- `docs/v2/` holds about 275 links to the three old files. They resolve, since the files remain, but an anchor into one no longer lands on a heading. Left: `docs/v2/` is outside this card, and the indexes name where each heading went.
- The wording test needed no change (it reads every document under `docs/` at any depth); the host adapter's test reads the records too, since the table of host versions moved into one.
- Known and left: a few branches of `internal/spec`'s checks that no test makes fail (the card's five kinds of fault are all seeded and caught).
- `docs/spec/store-format.md` was not moved or condensed: ST-03c's two sentences are still owed there, with 11 bytes of room.
