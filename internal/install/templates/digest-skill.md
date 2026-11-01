---
name: eva-md-digest
description: Digest documentation spaces and repos into eva.md system pages, one page per system with sources, so later chats share the understanding
---

# Digest systems into eva.md

One page per system (a service, repo, platform or major documentation area): `systems/<slug>.md`, the slug 2 to 64 of a-z, 0-9 and -, starting with a letter.

1. First read the map, `{{bin}} show systems/map` (missing until the first page is written), and any page you will change, `{{bin}} show systems/<slug>`; where it prints `[REDACTED:...]`, restore the text from its source before writing. A page shown prints `eva.md: systems/<slug>: ...` lines on stderr saying how far the code it cites has moved; before changing a page, run `{{bin}} system status <slug>` and read again the paths that moved.
2. One system at a time: summarize, never paste whole pages; keep identifiers exact, in backticks; cite each source's URL, with its version when it has one (`?version=<n>`, or the URL, a space and `v<n>`); cite code as `<repo>@<commit>:<folder>` for each folder or file you read, at the commit you read it (`git rev-parse HEAD` in that clone), one commit per repo in a digest, folders over files, at most 16 a page, or `<repo>@<commit>` for a page about a whole repo; a page built only from documentation may set `stale_after: <YYYY-MM-DD>`, the day to review it by; each line under `## Decisions and constraints` names its source; put what no source says under `## Unverified`; link related systems as `[<title>](<slug>.md)` once their pages exist.
3. Write the page to a temp file, then run `{{bin}} system write <slug> < <file>`, with `--replace` for a page you read and merged. On a refusal, fix the lines it names and run it again.
4. End each reply with the systems done and those that remain, so the background update keeps them in the thread's next steps and a new chat continues the digest.

Never copy sensitive documents (HR, legal, health, credentials) into a system page; cite them by link and title only.

The page, at most 24 KB. Only `## Summary` is required; sections keep this order, `###` may go inside one, and there are no code fences or HTML comments:

```markdown
---
type: system
title: <at most 80 characters>
description: <one line, at most 160 characters>
status: stable
generated: { by: {{host}}/<your model id, or unknown> }
sources:
  - resource: "<source>"
repos: [<git remote or project key>]
aliases: [<other name>]
as_of: <YYYY-MM-DD the content reflects>
state: active
---

## Summary
<at most 800 characters>
## Responsibilities
## Interfaces
## Depends on
## Used by
## Decisions and constraints
## Open questions
## Unverified
```
