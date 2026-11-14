# Background update settings

- **Status:** Reopened
- **Date:** 2026-10-30

## Context

Decision 17 of v1, from the list in `docs/decisions.md`. It has stood since that file's first commit, af3e066 of 2026-10-30. The reasons behind v1's decisions are in [Why a lighter option](2026-10-30-why-a-lighter-option.md); where v1's design refines this one, [Changes to decisions](2026-10-30-changes-to-decisions.md) says how.

## Decision

<!-- v1 docs/decisions.md 019 -->
17. **Background update settings.**
    - Codex update runs use your normal Codex config (`~/.codex/config.toml`) by default, so its provider settings apply. Your MCP servers start on those runs too.
    - Effort is configurable per host and per model. The default is the highest effort the chat's model supports.
    - If Codex asks you to re-approve eva-md's hooks after an update, you approve them. A notice and the health check point you to Codex's hooks settings.

<!-- /v1 -->

## Consequences

v1 builds it as [Background update: input, model and output](../spec/update-model.md) says.

## Reopens

None.

## Reopened by

Row 5 of [the plan's section 7](../v2/plan.md#7-decisions-for-the-owner), taken at its recommendation on 2026-11-10. v1 does what this record says; the rows say what v2 changes, and v2 builds it behind switches that default to v1's behaviour.
