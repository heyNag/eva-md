# Hosts in scope

- **Status:** Accepted
- **Date:** 2026-10-30

## Context

Decision 3 of v1, from the list in `docs/decisions.md`. It has stood since that file's first commit, af3e066 of 2026-10-30. The reasons behind v1's decisions are in [Why a lighter option](2026-10-30-why-a-lighter-option.md); where v1's design refines this one, [Changes to decisions](2026-10-30-changes-to-decisions.md) says how.

## Decision

<!-- v1 docs/decisions.md 005 -->
3. **Hosts in scope:**
   - Claude Code: the desktop app's Code tab and the CLI.
   - Codex: the ChatGPT app's Codex mode and the CLI.
   - Not in v1: Claude Desktop's Chat tab, regular ChatGPT chat, and Claude Code cloud sessions.
   - The Codex VS Code extension has no plugin support and would need manual setup.

<!-- /v1 -->

## Consequences

v1 builds it as [Packaging for Claude Code and Codex](../spec/packaging.md) and [Hosts: open questions and live checks](../spec/hosts.md) say.

## Reopens

None.

## Reopened by

None.
