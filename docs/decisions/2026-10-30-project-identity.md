# Project identity

- **Status:** Reopened
- **Date:** 2026-10-30

## Context

Decision 10 of v1, from the list in `docs/decisions.md`. It has stood since that file's first commit, af3e066 of 2026-10-30. The reasons behind v1's decisions are in [Why a lighter option](2026-10-30-why-a-lighter-option.md); where v1's design refines this one, [Changes to decisions](2026-10-30-changes-to-decisions.md) says how.

## Decision

<!-- v1 docs/decisions.md 012 -->
10. **Project identity.**
    - A project is identified by the repo's remote URL, so all checkouts and worktrees share memory. Without a remote, the folder identifies it.
    - Chats outside any project go to a general area.

<!-- /v1 -->

## Consequences

v1 builds it as [Identity and git's environment](../spec/identity.md) says.

## Reopens

None.

## Reopened by

Row 20 of [the plan's section 7](../v2/plan.md#7-decisions-for-the-owner), taken at its recommendation on 2026-11-10. v1 does what this record says; the rows say what v2 changes, and v2 builds it behind switches that default to v1's behaviour.
