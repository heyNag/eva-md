# Organizing (the background update)

- **Status:** Reopened
- **Date:** 2026-10-30

## Context

Decision 5 of v1, from the list in `docs/decisions.md`. It has stood since that file's first commit, af3e066 of 2026-10-30. The reasons behind v1's decisions are in [Why a lighter option](2026-10-30-why-a-lighter-option.md); where v1's design refines this one, [Changes to decisions](2026-10-30-changes-to-decisions.md) says how.

## Decision

<!-- v1 docs/decisions.md 007 -->
5. **Organizing (the background update).**
   - After each chat, a detached job runs the same host and model that ran the chat in headless mode, with tools off.
   - It updates the project's Now page and promotes lasting facts into wiki pages.
   - It supersedes old facts and never deletes or rewrites a whole page.
   - Every page links to its source entries, and pages you lock are left alone.
   - Dates come from entry timestamps.
   - The bar for promotion: would it matter in six months, and can it not be worked out from the code?

<!-- /v1 -->

## Consequences

v1 builds it as [Background update: triggers and job control](../spec/update-triggers.md), [Background update: input, model and output](../spec/update-model.md) and [Validation](../spec/validator.md) say.

## Reopens

None.

## Reopened by

Rows 5 and 10 of [the plan's section 7](../v2/plan.md#7-decisions-for-the-owner), taken at their recommendation on 2026-11-10. v1 does what this record says; the rows say what v2 changes, and v2 builds it behind switches that default to v1's behaviour.
