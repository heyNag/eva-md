# Reliability

- **Status:** Accepted
- **Date:** 2026-10-30

## Context

Decision 12 of v1, from the list in `docs/decisions.md`. It has stood since that file's first commit, af3e066 of 2026-10-30. The reasons behind v1's decisions are in [Why a lighter option](2026-10-30-why-a-lighter-option.md); where v1's design refines this one, [Changes to decisions](2026-10-30-changes-to-decisions.md) says how.

## Decision

<!-- v1 docs/decisions.md 014 -->
12. **Reliability.**
    - Hooks capture every turn, so capture never depends on the model remembering.
    - A checkpoint is saved before compaction, and the next chat catches up on anything a hook missed.
    - Hooks always finish fast and never block.
    - Unknowns are reported as unknown.
    - A health check confirms the hooks are installed, approved and firing. Codex needs re-approval after each update.
    - A resume test, plus an accuracy suite and a scope suite, check dates, speakers, corrections and project scope.

<!-- /v1 -->

## Consequences

v1 builds it as [Capture and the hook dispatcher](../spec/hooks.md), [Transcript recovery](../spec/catchup.md), [The health check](../spec/health.md) and [Background update: the offline checks](../spec/update-checks.md) say.

## Reopens

None.

## Reopened by

None.
