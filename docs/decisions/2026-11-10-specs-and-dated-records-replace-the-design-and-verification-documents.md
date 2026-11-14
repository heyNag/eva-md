# Specs and dated records replace the design and verification documents

- **Status:** Accepted
- **Date:** 2026-11-10

## Context

Decision 15 of [the v2 plan](../v2/plan.md#7-decisions-for-the-owner), taken at its recommendation on 2026-11-10. `docs/design.md` (426 KB) and `docs/verification.md` (208 KB) were too big for an agent to load, and every behaviour change edited them, so packages built side by side collided in them, and tests policed their prose.

## Decision

Per-component specs in [docs/spec/](../spec/INDEX.md) and dated decision records in this folder are the source of truth for eva.md's behaviour and its reasons.
`docs/design.md`, `docs/verification.md` and `docs/decisions.md` are frozen as short indexes that say where each heading went.
A change of behaviour is written under `## Since the split` in its component's spec, and a new decision is a new record.
The tests that read the old prose are replaced by `internal/spec`, which checks that the split changed no word, the size of every file, the indexes, the records and the links.

## Consequences

The split, EV-10a, moved every word of the three files into blocks that `internal/spec` holds to the files at the split's base. EV-10b adds to each spec the must-keep items it touches and the tests that hold them.

## Reopens

Two lines of `AGENTS.md`, as the last column of the plan's row 15 says: the one that made `docs/design.md` the source of truth for behaviour, and the rule that a change of behaviour updates it.

## Reopened by

None.
