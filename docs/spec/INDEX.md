# Specs

One spec a component of eva.md.
The v1 specs hold the text of v1's design and verification documents, split on 2026-11-14 at commit 3319eef, word for word: each block opens with a comment naming its source and its order there, and only the line breaks, the heading levels and the link targets inside a block differ from the source.
`internal/spec` checks that no word changed.
A spec's `## Since the split` says what later packages changed; [the decision records](../decisions.md) say why.

| File | Component | Paths | Status |
| --- | --- | --- | --- |
| [brief.md](brief.md) | Brief builder: the brief | `internal/resume` | v1 as built, frozen on 2026-11-14 |
| [capture-audit.md](capture-audit.md) | Capture audit | `internal/ledger/audit`, `cmd/eva-md/audit.go` | v1 as built, frozen on 2026-11-14 |
| [catchup.md](catchup.md) | Catch-up reader | `internal/catchup` | v1 as built, frozen on 2026-11-14 |
| [checks.md](checks.md) | The test suite and CI | `.github/workflows/ci.yml`, `internal/testgitenv`, `internal/testlimits` | v1 as built, frozen on 2026-11-14 |
| [cli.md](cli.md) | eva-md binary: the CLI | `cmd/eva-md` | v1 as built, frozen on 2026-11-14 |
| [code-refs.md](code-refs.md) | Code refs | `internal/coderef`, `cmd/eva-md/code.go` | v1 as built, frozen on 2026-11-14 |
| [commit.md](commit.md) | Maintain / committer | `internal/wikigit`, `internal/lint`, `cmd/eva-md/commit.go`, `cmd/eva-md/maintain.go` | v1 as built, frozen on 2026-11-14 |
| [field-report.md](field-report.md) | init / install / uninstall / doctor: the field report | `internal/health` | v1 as built, frozen on 2026-11-14 |
| [health.md](health.md) | init / install / uninstall / doctor: the doctor | `internal/health` | v1 as built, frozen on 2026-11-14 |
| [hooks.md](hooks.md) | Hook dispatcher | `internal/hook` | v1 as built, frozen on 2026-11-14 |
| [hosts.md](hosts.md) | The hosts: what is observed and what is not | `internal/host/adapter` | v1 as built, frozen on 2026-11-14 |
| [identity.md](identity.md) | Identity resolver | `internal/identity` | v1 as built, frozen on 2026-11-14 |
| [inbox.md](inbox.md) | Inbox store | `internal/inbox`, `cmd/eva-md/inbox.go` | v1 as built, frozen on 2026-11-14 |
| [install.md](install.md) | init / install / uninstall / doctor: install and uninstall | `internal/install`, `cmd/eva-md/uninstall.go` | v1 as built, frozen on 2026-11-14 |
| [knowledge-pages.md](knowledge-pages.md) | Knowledge pages | `internal/knowledgepage`, `internal/hook/recall.go` | v1 as built, frozen on 2026-11-14 |
| [live-accuracy.md](live-accuracy.md) | The live accuracy run (a developer's command) | `cmd/live-accuracy`, `internal/update/accuracy/live` | v1 as built, frozen on 2026-11-14 |
| [mcp-server.md](mcp-server.md) | eva-md binary: the MCP server | `internal/mcp`, `cmd/eva-md/mcp.go` | v1 as built, frozen on 2026-11-14 |
| [okf-adapter.md](okf-adapter.md) | Renderer + okf adapter: the adapter | `internal/okfadapter` | v1 as built, frozen on 2026-11-14 |
| [overview.md](overview.md) | eva-md as a whole: its summary, how it works and its parts | `cmd/eva-md`, `internal` | v1 as built, frozen on 2026-11-14 |
| [packaging.md](packaging.md) | Static plugins, and making a release | `internal/install`, `internal/host/adapter`, `scripts/release.sh`, `.github/workflows` | v1 as built, frozen on 2026-11-14 |
| [performance.md](performance.md) | Latency targets and the benchmark | `scripts/benchmark.py`, `scripts/bench` | v1 as built, frozen on 2026-11-14 |
| [recall.md](recall.md) | Hook dispatcher and brief builder: recall and the data fence | `internal/hook`, `internal/resume` | v1 as built, frozen on 2026-11-14 |
| [redaction.md](redaction.md) | Redactor | `internal/redact` | v1 as built, frozen on 2026-11-14 |
| [releases.md](releases.md) | Releases and upgrade | `internal/release`, `cmd/eva-md/upgrade.go`, `scripts/install.sh`, `scripts/uninstall.sh` | v1 as built, frozen on 2026-11-14 |
| [resume.md](resume.md) | Brief builder: the resume flows | `internal/resume`, `internal/hook/resume.go` | v1 as built, frozen on 2026-11-14 |
| [scope.md](scope.md) | eva-md as a whole: what v1 covers and what it risks | `docs/known-limitations.md` | v1 as built, frozen on 2026-11-14 |
| [search.md](search.md) | Search | `internal/resume/search.go`, `internal/resume/show.go` | v1 as built, frozen on 2026-11-14 |
| [search-index.md](search-index.md) | Search index | `internal/searchindex`, `cmd/eva-md/index.go` | v1 as built, frozen on 2026-11-14 |
| [setup.md](setup.md) | init / install / uninstall / doctor: setup and init | `internal/wikiinit`, `cmd/eva-md/setup.go`, `cmd/eva-md/setup_checks.go` | v1 as built, frozen on 2026-11-14 |
| [setup-2.md](setup-2.md) | init / install / uninstall / doctor: setup and init | `internal/wikiinit`, `cmd/eva-md/setup_repo.go`, `cmd/eva-md/setup_refusals.go` | v1 as built, frozen on 2026-11-14 |
| [setup-checks.md](setup-checks.md) | init / install / uninstall / doctor: their offline checks | `internal/health`, `internal/wikiinit`, `internal/config`, `cmd/eva-md` | v1 as built, frozen on 2026-11-14 |
| [shapes-audit.md](shapes-audit.md) | Shapes audit | `internal/ledger/shapes`, `cmd/eva-md/shapes.go` | v1 as built, frozen on 2026-11-14 |
| [system-pages.md](system-pages.md) | System pages | `internal/systempage`, `cmd/eva-md/system.go`, `internal/hook/pointers.go` | v1 as built, frozen on 2026-11-14 |
| [testing.md](testing.md) | The test suite | `cmd/eva-md`, `internal` | v1 as built, frozen on 2026-11-14 |
| [update-checks.md](update-checks.md) | Updater and validator: their offline checks | `internal/update`, `internal/update/accuracy` | v1 as built, frozen on 2026-11-14 |
| [update-job.md](update-job.md) | Updater: the job | `internal/update`, `cmd/eva-md/update.go` | v1 as built, frozen on 2026-11-14 |
| [update-model.md](update-model.md) | Updater: input, model selection and output | `internal/update` | v1 as built, frozen on 2026-11-14 |
| [update-triggers.md](update-triggers.md) | Updater: triggers and job control | `internal/update`, `internal/hook/trigger.go`, `internal/hook/spawn.go` | v1 as built, frozen on 2026-11-14 |
| [validator.md](validator.md) | Validator | `internal/update/validate.go` | v1 as built, frozen on 2026-11-14 |
| [wiki-format.md](wiki-format.md) | Renderer + okf adapter: the renderer, and the wiki's layout | `internal/threadpage`, `internal/wikiinit` | v1 as built, frozen on 2026-11-14 |
| [store-format.md](store-format.md) | The store's written format | `internal/store/profile`, `internal/store/codec` | v2 spec, not edited by the split |
| [internal/ledger/SPEC.md](../../internal/ledger/SPEC.md) | The record's event model and the one reader | `internal/ledger` | v2 spec, not edited by the split |
