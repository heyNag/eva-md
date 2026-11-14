# What we borrow

- **Status:** Accepted
- **Date:** 2026-10-30

## Context

A section of v1's `docs/decisions.md`, since its first commit, af3e066 of 2026-10-30: the ideas taken from the reference memory repos.

## Decision

<!-- v1 docs/decisions.md 021 -->
### What we borrow

| Source | Idea |
| --- | --- |
| okf-agent-memory | Page format, provenance the tool stamps itself (agents can't mark anything human-verified), reload from disk on every call, a cap on always-loaded text |
| okf-agent-memory, for system pages | `stale_after` as a page's review day, and the idea of `code_refs`, tied to `<repo>@<commit>:<path>` sources instead of bare paths, which okf's drift check would find missing in a wiki kept apart from the repos |
| okf-agent-memory's search | For ranking search hits: its BM25 inverse document frequency and its matching of a query word to the tokens it starts. Its search itself is not used (see design.md, "Changes to decisions", 15). |
| brain.md | Page shape: current truth on top, dated timeline below |
| iwe | In-process keyword and fuzzy search, a titles-only list at session start, near-duplicate check after writes. It shipped automatic capture and removed it, so the promotion bar matters. |
| tigerless agent-memory | Markdown as truth with a rebuildable search cache and a test proving it; hook habits (always exit 0, timeouts, recursion guard, per-session watermark) |
| basic-memory | Thin plugin shims, a session-start brief marked as data, the Codex compaction workaround |
| markdown-agent-memory | Provenance tags (stated, observed, inferred, suggested), dated strikethrough supersession |
| memsearch | Links from entries back to the source conversation; recall in a sub-agent |
| obsidian-second-brain | A per-prompt recall hook that stays silent on weak matches; validation after writes |

<!-- /v1 -->

## Consequences

[Scope, roadmap, risks and borrowed ideas](../spec/scope.md) holds the ideas v1 borrowed later.

## Reopens

None.

## Reopened by

None.
