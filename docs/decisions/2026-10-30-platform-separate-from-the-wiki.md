# Platform separate from the wiki

- **Status:** Reopened
- **Date:** 2026-10-30

## Context

Decision 1 of v1, from the list in `docs/decisions.md`. It has stood since that file's first commit, af3e066 of 2026-10-30. The reasons behind v1's decisions are in [Why a lighter option](2026-10-30-why-a-lighter-option.md); where v1's design refines this one, [Changes to decisions](2026-10-30-changes-to-decisions.md) says how.

## Decision

<!-- v1 docs/decisions.md 003 -->
### Decisions

Some decisions cover parts that are not built yet. [design.md](../design.md) marks those parts as planned.

1. **Platform separate from the wiki.**
   - eva.md is only the platform: tools, plugins and schema.
   - The memory lives in a separate private git repo of markdown that the platform is pointed at by a per-machine config.
     On a host whose admins allow no private repos, or when the repo you give is internal, it may live in an internal repo instead, which everyone signed in to that GitLab, or every member of that GitHub enterprise, can read, but only after you agree to that (`EVA_MD_ALLOW_INTERNAL_REPO=true` agrees in
     advance, for any internal repo); a bare repo on the Mac itself, which only your account can read, works too.
   - For example, one Mac uses a private GitHub repo and another Mac a private repo on a self-hosted GitLab.
   - The same code runs on every machine; the memory of one wiki never mixes with another's.
   - You create the empty repo, or `eva-md setup` creates it for you; `init` adds the structure and the first commit.
   - The platform uses only the plain git command line, never GitHub or GitLab APIs.
     The one exception is `eva-md setup`: when the gh or glab CLI is installed and signed in, setup may run it to create the wiki's private repo, to check that a repo is private, and to read back a repo's address.
     The wiki, init's git steps, the hooks, the update job and the daily commit stay plain git.
     Separately, `eva-md upgrade` and the install and uninstall scripts download eva-md's own releases from github.com over HTTPS, with no API call and no sign-in.

<!-- /v1 -->

## Consequences

v1 builds it as [Setup and init of a new wiki, part 1](../spec/setup.md), [Setup and init of a new wiki, part 2](../spec/setup-2.md) and [The daily commit and push](../spec/commit.md) say.

## Reopens

None.

## Reopened by

Rows 2 and 21 of [the plan's section 7](../v2/plan.md#7-decisions-for-the-owner), taken at their recommendation on 2026-11-10. v1 does what this record says; the rows say what v2 changes, and v2 builds it behind switches that default to v1's behaviour.
