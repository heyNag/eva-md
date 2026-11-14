# Settled details

- **Status:** Reopened
- **Date:** 2026-10-30

## Context

Decision 16 of v1, from the list in `docs/decisions.md`. It has stood since that file's first commit, af3e066 of 2026-10-30. The reasons behind v1's decisions are in [Why a lighter option](2026-10-30-why-a-lighter-option.md); where v1's design refines this one, [Changes to decisions](2026-10-30-changes-to-decisions.md) says how.

## Decision

<!-- v1 docs/decisions.md 018 -->
16. **Settled details.**
    - **Day one:** resuming only, meaning capture, the background update, the Now page, the context at chat start and the health check. Knowledge pages and search come next.
    - **Coverage:** every project automatically, with an exclude list for sensitive folders.
    - **Parallel chats:** one thread per branch, plus a short project overview.
    - **What resuming shows:**
      - a summary of the last chat, what's done, next steps, and open decisions and questions
      - the current git branch and uncommitted changes, read live
      - open PRs and failing tests come later
    - **Tool activity:** the conversation plus a one-line record of the tool calls that change or read things, such as files changed, commands run and what was read or fetched, without their output.
    - **Visibility:** one line at the start of a chat, plus a notice only when something is wrong.
    - **Off switch:** saying "off the record" stops saving for that chat.
    - **Managed machines:** eva-md assumes plugins, hooks and background headless runs are allowed on a managed machine. The health check still reports managed settings that switch hooks off, where it can see them.
    - **Repos:** the platform repo (github.com/heyNag/eva-md) holds no personal data, so it can be public. Each wiki repo stays private, or internal once you agree, when the host's admins forbid private repos or the repo you give is internal; never public.
    - **Wiki location:** chosen by you during setup; there is no default. Any folder of its own, outside Documents, Desktop, Downloads and iCloud Drive, which macOS protects from background processes, and outside other cloud storage, such as OneDrive or Dropbox, which syncs into .git.

<!-- /v1 -->

## Consequences

v1 builds it as [Wiki layout and page formats](../spec/wiki-format.md), [Capture and the hook dispatcher](../spec/hooks.md) and [The health check](../spec/health.md) say.

## Reopens

None.

## Reopened by

Rows 2, 6 and 19 of [the plan's section 7](../v2/plan.md#7-decisions-for-the-owner), taken at their recommendation on 2026-11-10. v1 does what this record says; the rows say what v2 changes, and v2 builds it behind switches that default to v1's behaviour.
