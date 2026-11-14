# Verification (frozen and split)

This file said how eva.md v1 is checked, automatically and live. On 2026-11-14 it was frozen and split, word for word, into the specs in [spec/](spec/INDEX.md): each bullet of its list of what the suite covers joined the spec of its packages, and the rest went to the specs below.

Every heading this file had, beside the file that now holds it:

| Heading | Now in |
| --- | --- |
| Verification | [spec/checks.md](spec/checks.md) |
| Automated tests | [spec/checks.md](spec/checks.md) |
| Live checks | [spec/hosts.md](spec/hosts.md) |
| Live accuracy run | [spec/live-accuracy.md](spec/live-accuracy.md) |
| Performance | [spec/performance.md](spec/performance.md) |
| Not verified yet | [spec/hosts.md](spec/hosts.md) |

Every bold bullet title, in its order, beside the file that now holds it:

| Bullet | Now in |
| --- | --- |
| Redaction | [spec/redaction.md](spec/redaction.md) |
| Transcript recovery | [spec/catchup.md](spec/catchup.md) |
| Hooks | [spec/hooks.md](spec/hooks.md) |
| Resume briefs | [spec/brief.md](spec/brief.md) |
| Search | [spec/search.md](spec/search.md) |
| Search index | [spec/search-index.md](spec/search-index.md) |
| Removing captured chats | [spec/inbox.md](spec/inbox.md) |
| Show by section | [spec/search.md](spec/search.md) |
| Install | [spec/install.md](spec/install.md) |
| Releases, install, upgrade and uninstall | [spec/releases.md](spec/releases.md) |
| Identity cache | [spec/identity.md](spec/identity.md) |
| Config | [spec/setup-checks.md](spec/setup-checks.md) |
| Thread pages | [spec/wiki-format.md](spec/wiki-format.md) |
| Update input and validator | [spec/update-checks.md](spec/update-checks.md) |
| Accuracy suite | [spec/update-checks.md](spec/update-checks.md) |
| Headless runners | [spec/update-model.md](spec/update-model.md) |
| Live accuracy run | [spec/live-accuracy.md](spec/live-accuracy.md) |
| Update job | [spec/update-job.md](spec/update-job.md) |
| Update job, later fixes | [spec/update-job.md](spec/update-job.md) |
| Standing rules and pasted text | [spec/wiki-format.md](spec/wiki-format.md) |
| System pages | [spec/system-pages.md](spec/system-pages.md) |
| Prompt pointers | [spec/system-pages.md](spec/system-pages.md) |
| Knowledge pages | [spec/knowledge-pages.md](spec/knowledge-pages.md) |
| Prompt recall | [spec/knowledge-pages.md](spec/knowledge-pages.md) |
| Code refs and staleness | [spec/code-refs.md](spec/code-refs.md) |
| MCP server | [spec/mcp-server.md](spec/mcp-server.md) |
| Program-run chats, tool lines and hook failures | [spec/hooks.md](spec/hooks.md) |
| Daily commit, push and lint | [spec/commit.md](spec/commit.md) |
| Verified pages and okf's body rule | [spec/okf-adapter.md](spec/okf-adapter.md) |
| log.md | [spec/okf-adapter.md](spec/okf-adapter.md) |
| Recovery records' pruning | [spec/catchup.md](spec/catchup.md) |
| Git's environment, output caps and files started at once | [spec/identity.md](spec/identity.md) |
| okf's lock file | [spec/okf-adapter.md](spec/okf-adapter.md) |
| Health, setup and CLI | [spec/health.md](spec/health.md) |
| Field report | [spec/field-report.md](spec/field-report.md) |
| Setup's install checks, the live check's CLI, what uninstall finds unsaved, and the wiki's mode | [spec/install.md](spec/install.md) |
| Public wording | [spec/cli.md](spec/cli.md) |
| Docs that match the CLI and the tests | [spec/cli.md](spec/cli.md) |
| Only eva's name | [spec/cli.md](spec/cli.md) |
| Capture audit | [spec/capture-audit.md](spec/capture-audit.md) |
| Shapes audit | [spec/shapes-audit.md](spec/shapes-audit.md) |
| Host adapter | [spec/packaging.md](spec/packaging.md) |
| Releases | [spec/releases.md](spec/releases.md) |
| Claude Code CLI. | [spec/hosts.md](spec/hosts.md) |
| Codex CLI and cross-host. | [spec/hosts.md](spec/hosts.md) |
| Claude Code desktop app. | [spec/hosts.md](spec/hosts.md) |
| Install through the native CLIs. | [spec/hosts.md](spec/hosts.md) |
| Transcript recovery on real hosts. | [spec/hosts.md](spec/hosts.md) |
| Codex tool features. | [spec/hosts.md](spec/hosts.md) |
| Codex plugin inventory. | [spec/hosts.md](spec/hosts.md) |
| Releases on GitHub. | [spec/hosts.md](spec/hosts.md) |
| Releases cut from GitHub. | [spec/hosts.md](spec/hosts.md) |
| The install command from a release. | [spec/hosts.md](spec/hosts.md) |
| Claude Code marketplace removal. | [spec/hosts.md](spec/hosts.md) |
| Codex desktop app. | [spec/hosts.md](spec/hosts.md) |
| Real use. | [spec/hosts.md](spec/hosts.md) |
| Live updates. | [spec/hosts.md](spec/hosts.md) |
| The daily commit on real hosts. | [spec/hosts.md](spec/hosts.md) |
| A wiki on a self-hosted GitLab. | [spec/hosts.md](spec/hosts.md) |
| The digest and recall skills. | [spec/hosts.md](spec/hosts.md) |
| Knowledge pages and recall in real use. | [spec/hosts.md](spec/hosts.md) |
| Code refs in real use. | [spec/hosts.md](spec/hosts.md) |
| Host signals. | [spec/hosts.md](spec/hosts.md) |
| Removing captured chats on a real inbox. | [spec/hosts.md](spec/hosts.md) |
| Setup with real gh and glab. | [spec/hosts.md](spec/hosts.md) |
| A deleted install folder. | [spec/hosts.md](spec/hosts.md) |
| Install, upgrades and uninstall from a release. | [spec/hosts.md](spec/hosts.md) |
| A release cut from GitHub, off its usual path. | [spec/hosts.md](spec/hosts.md) |
| An upgrade over an installed plugin. | [spec/hosts.md](spec/hosts.md) |
| A burst of parallel tool calls. | [spec/hosts.md](spec/hosts.md) |
| Codex `mcp__` tool lines. | [spec/hosts.md](spec/hosts.md) |
| The MCP server on real hosts. | [spec/hosts.md](spec/hosts.md) |
