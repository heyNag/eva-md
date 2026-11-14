# okf at the core

- **Status:** Reopened
- **Date:** 2026-10-30

## Context

Decision 15 of v1, from the list in `docs/decisions.md`. It has stood since that file's first commit, af3e066 of 2026-10-30. The reasons behind v1's decisions are in [Why a lighter option](2026-10-30-why-a-lighter-option.md); where v1's design refines this one, [Changes to decisions](2026-10-30-changes-to-decisions.md) says how.

## Decision

<!-- v1 docs/decisions.md 017 -->
15. **okf at the core.**
    - eva-md is the orchestrator. okf (okf-memory/okf-agent-memory, MIT license) is the core memory engine, compiled in as a Go library at a pinned version.
    - okf owns the page format, safe writes, search, validation and setting up a new wiki.
    - eva-md owns the hooks, the inbox, the background update, resume, the daily commit, the plugins, the config and the health check.
    - One adapter in eva-md is the only code that calls okf. It holds the protections okf lacks today: no overwrite on create, one writer at a time, supersede, and the real host recorded as author. Each one is removed once okf ships it.
    - No issues or pull requests go to okf. Anything okf lacks is built on top of it inside eva-md, and removed once an okf release covers it.
    - okf is reviewed weekly. Upgrades happen only for security fixes or releases that cover a gap eva-md works around, and are tested on a branch before merging.
    - Not used: okf's registry, hub sync, vendor bundles, governance tiers, scorecards, its agent instruction block and its own MCP server.
    - Way out: the wiki stays in okf's markdown format, so the core can be swapped without touching memory.

<!-- /v1 -->

## Consequences

v1 builds it as [The okf adapter](../spec/okf-adapter.md) says.

## Reopens

None.

## Reopened by

Row 1 of [the plan's section 7](../v2/plan.md#7-decisions-for-the-owner), taken at its recommendation on 2026-11-10. v1 does what this record says; the rows say what v2 changes, and v2 builds it behind switches that default to v1's behaviour.
