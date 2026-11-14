# eva.md v1 design (frozen and split)

This file was the design of eva.md v1.
On 2026-11-14 it was frozen and split, word for word, into one spec a component in [spec/](spec/INDEX.md), with its "Changes to decisions" moved into [a dated decision record](decisions/2026-10-30-changes-to-decisions.md).
The specs are the source of truth for behaviour: a later change is written under `## Since the split` in its spec, and a new decision is a new [dated record](decisions.md).

Every heading this file had, in its order, beside the file that now holds it, so a comment that cites `docs/design.md` and a heading still leads to the text:

| Heading | Now in |
| --- | --- |
| eva.md v1 design | [spec/overview.md](spec/overview.md) |
| Summary | [spec/overview.md](spec/overview.md) |
| How it works | [spec/overview.md](spec/overview.md) |
| Components | [spec/overview.md](spec/overview.md) |
| The okf adapter | [spec/okf-adapter.md](spec/okf-adapter.md) |
| okf calls per operation | [spec/okf-adapter.md](spec/okf-adapter.md) |
| Protections eva-md adds | [spec/okf-adapter.md](spec/okf-adapter.md) |
| Gaps covered in eva-md (watch okf for these) | [spec/okf-adapter.md](spec/okf-adapter.md) |
| Pin and upgrade policy | [spec/okf-adapter.md](spec/okf-adapter.md) |
| Wiki layout and page formats | [spec/wiki-format.md](spec/wiki-format.md) |
| Thread page | [spec/wiki-format.md](spec/wiki-format.md) |
| Project now.md | [spec/wiki-format.md](spec/wiki-format.md) |
| Rules page | [spec/wiki-format.md](spec/wiki-format.md) |
| System page | [spec/system-pages.md](spec/system-pages.md) |
| Code a system page cites | [spec/code-refs.md](spec/code-refs.md) |
| Knowledge page | [spec/knowledge-pages.md](spec/knowledge-pages.md) |
| Branches and threads | [spec/wiki-format.md](spec/wiki-format.md) |
| Schema versioning | [spec/wiki-format.md](spec/wiki-format.md) |
| The inbox | [spec/inbox.md](spec/inbox.md) |
| Flows | [spec/hooks.md](spec/hooks.md) |
| Capture (every hook event) | [spec/hooks.md](spec/hooks.md) |
| Resume: new chat or /clear | [spec/resume.md](spec/resume.md) |
| Resume: resumed or forked chat | [spec/resume.md](spec/resume.md) |
| Resume: first-prompt fallback | [spec/resume.md](spec/resume.md) |
| Resume: hash-triggered refresh | [spec/resume.md](spec/resume.md) |
| After compaction | [spec/resume.md](spec/resume.md) |
| Background update | [spec/update-job.md](spec/update-job.md) |
| Daily commit and push | [spec/commit.md](spec/commit.md) |
| Setup and init of a new wiki | [spec/setup.md](spec/setup.md) |
| Install and uninstall | [spec/install-design.md](spec/install-design.md) |
| The install scripts and upgrade | [spec/releases.md](spec/releases.md) |
| Health check | [spec/health.md](spec/health.md) |
| Field report | [spec/field-report.md](spec/field-report.md) |
| Off the record | [spec/hooks.md](spec/hooks.md) |
| Capture audit | [spec/capture-audit.md](spec/capture-audit.md) |
| Shapes audit | [spec/shapes-audit.md](spec/shapes-audit.md) |
| Background update details | [spec/update-triggers.md](spec/update-triggers.md) |
| Triggers | [spec/update-triggers.md](spec/update-triggers.md) |
| Job control | [spec/update-triggers.md](spec/update-triggers.md) |
| Input | [spec/update-model.md](spec/update-model.md) |
| Model selection | [spec/update-model.md](spec/update-model.md) |
| Output format | [spec/update-model.md](spec/update-model.md) |
| Validation | [spec/validator.md](spec/validator.md) |
| Failures, budget and quarantine | [spec/update-job.md](spec/update-job.md) |
| The run log | [spec/update-job.md](spec/update-job.md) |
| Recall and fencing | [spec/brief.md](spec/brief.md) |
| At session start | [spec/brief.md](spec/brief.md) |
| Per prompt | [spec/recall.md](spec/recall.md) |
| Search | [spec/search.md](spec/search.md) |
| MCP server | [spec/mcp-server.md](spec/mcp-server.md) |
| Search index | [spec/search-index.md](spec/search-index.md) |
| Fencing | [spec/recall.md](spec/recall.md) |
| Packaging for Claude Code and Codex | [spec/packaging.md](spec/packaging.md) |
| Claude Code | [spec/packaging.md](spec/packaging.md) |
| Codex | [spec/packaging.md](spec/packaging.md) |
| Shared | [spec/packaging.md](spec/packaging.md) |
| Making a release | [spec/packaging.md](spec/packaging.md) |
| Command-line surface | [spec/cli.md](spec/cli.md) |
| Reliability guarantees | [spec/checks.md](spec/checks.md) |
| Testing | [spec/testing.md](spec/testing.md) |
| v1 scope | [spec/scope.md](spec/scope.md) |
| In | [spec/scope.md](spec/scope.md) |
| Out | [spec/scope.md](spec/scope.md) |
| Roadmap | [spec/scope.md](spec/scope.md) |
| Changes to decisions | [decisions/2026-10-30-changes-to-decisions.md](decisions/2026-10-30-changes-to-decisions.md) |
| Risks | [spec/scope.md](spec/scope.md) |
| Ideas borrowed from ai-memory | [spec/scope.md](spec/scope.md) |
| Open host questions | [spec/hosts.md](spec/hosts.md) |
