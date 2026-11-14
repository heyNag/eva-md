# Why a lighter option

- **Status:** Accepted
- **Date:** 2026-10-30

## Context

A section of v1's `docs/decisions.md`, since its first commit, af3e066 of 2026-10-30: the lessons from heavier memory systems that v1's decisions rest on.

## Decision

<!-- v1 docs/decisions.md 002 -->
### Why a lighter option

Memory that runs a model on every write pays for it: a local model needs gigabytes of RAM and seconds per write, embedding servers and databases must keep running, and every moving part is another way to fail.

Lessons that carry over:

- Model work on the write path is the usual root cause of slow, fragile memory.
- Word-for-word writes with no model lose nothing: 72 of 72 records in testing.
- When the agent ran follow-up searches it found everything 10 times out of 10. A single automatic search was complete only 4 times out of 10.
- LLM extraction got speakers and dates wrong (an assistant suggestion stored as the user's decision; a weakened event date).

<!-- /v1 -->

## Consequences

The 17 decisions and [Ruled out](2026-10-30-ruled-out.md) follow from these lessons.

## Reopens

None.

## Reopened by

None.
