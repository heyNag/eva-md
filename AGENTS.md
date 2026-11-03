# eva-md: instructions for coding agents and contributors

eva-md is the platform for eva.md ("Eva dot M D"), a Markdown, git-based memory for Claude Code and Codex. Hooks capture chats into a redacted local inbox and give new chats a resume brief. The long-term memory lives in a separate private wiki repository that `eva-md setup` creates in a folder the user chooses. This repo is the platform only; it never contains wiki content.

Capture, resume and wiki updates are built: the background update job and its triggers, which summarize chats into thread pages, the daily signed commit and push, `doctor --live`, system pages and how far the code they cite has moved, standing rules and per-prompt pointers, knowledge pages the update promotes, with recall of matching knowledge on prompts, the project's constraints in the brief and a recall skill, a search index, and a read-only MCP server the plugins declare. Support for a wiki on a self-hosted git server, such as a self-hosted GitLab or a GitHub Enterprise server, is built too, and so are releases, the one-command install and upgrade from them, and uninstall. All of it is checked offline, with fake host CLIs and a fake GitHub. Releases are published, and the install command from one has set up a wiki on a self-hosted GitLab, stopping at its plugin step. The live checks on real hosts, a whole install, `eva-md upgrade` and the uninstall script from a real release, the first live updates and a day's run against that wiki are still pending.

## Read first

- [README.md](README.md): what eva-md does, setup, commands and privacy.
- [docs/design.md](docs/design.md): the source of truth for behaviour.
- [docs/decisions.md](docs/decisions.md): the agreed decisions. Don't reopen them without a strong reason.
- [docs/verification.md](docs/verification.md): how behaviour is checked, automatically and live.
- [docs/known-limitations.md](docs/known-limitations.md): what eva-md doesn't do, or doesn't do well: gaps still open, limits it keeps by design, and limits of the hosts and tools it runs on.
- [docs/releasing.md](docs/releasing.md): who cuts a release and how, the tag format, what the release workflow publishes, and how to verify or pull a release.

## Layout

- `cmd/eva-md`: the CLI. `main.go` dispatches commands, `commands.go` holds brief, show, search, install, doctor and status, `uninstall.go` uninstall, `upgrade.go` the upgrade, `setup.go` and `setup_repo.go` hold the guided setup, `setup_refusals.go` how it says why gh, glab or git refused, and the internal-repo question, `setup_checks.go` its "Checking this Mac" checklist, `update.go` the background update, `commit.go` the daily commit, `maintain.go` the daily job, `system.go` system page writes, `code.go` the code status of system pages, `knowledge.go` maintain's knowledge step, `index.go` the search index command, `mcp.go` the MCP mode, and `timelimits.go` the time limits tests raise, which `timelimits_raised.go` raises as a build with the `evamd_testlimits` tag, the tests' release stand-in, starts.
- `internal/`: one package per concern, such as `hook`, `inbox`, `catchup` (transcript recovery), `resume`, `redact`, `install` (the stable binary, the plugins, the install record and the link on PATH), `release` (eva-md upgrade's releases), `health`, `config`, `paths`, `identity`, `session`, `wikiinit`, `okfadapter`, `threadpage` (the thread and rules page model), `freetext` (the anti-forgery cleaning the brief and the update input share), `update` (wiki updates), `systempage` (system pages, the system map and the names index), `knowledgepage` (knowledge pages, the knowledge map, the recall index and recall's scoring), `coderef` (how far the code a system page cites has moved), `wikigit` (the git runner, the daily commit and the push), `lint` (the daily wiki lint), `mcp` (the read-only MCP server) and `testlimits` (the time limits tests raise).
- `scripts/benchmark.py`: the offline latency benchmark.
- Releases: `scripts/release.sh` (the maintainer's one command for a release), `.github/workflows/release.yml` (the workflow a pushed tag runs, which builds, verifies, attests and publishes) and `internal/release`, which finds, downloads and checks a release for `eva-md upgrade` and holds the tests that run the release script against a temporary origin and a fake gh.
- `scripts/install.sh` and `scripts/uninstall.sh`: the one-command install, upgrade and uninstall, published with each release. They run on macOS's `/bin/bash` 3.2, so they use no bash 4 features. `cmd/eva-md/release_test.go` builds a release stand-in and runs both scripts against it.
- The search index: `internal/searchindex` (its segment format, refresh, merge, catch-up and queries), `internal/inbox/walk.go` (the walker it reads session files with, and the trust check it shares with search), `internal/paths/lock.go` (the lock helper its writers take), `internal/resume/search_index.go` (how search reads it) and `cmd/eva-md/index.go` (`eva-md index` and maintain's index step).

## Rules

- **Go and dependencies.** Module `github.com/heyNag/eva-md`, `go 1.26.0`. There are only two dependencies: okf (`github.com/okf-memory/okf-agent-memory` v0.7.2), imported only by `internal/okfadapter`, and `github.com/BurntSushi/toml`, imported only by `internal/config`. Don't add others.
- **okf stays pinned and unpatched.** Use it as a library at the pinned version. Never patch or vendor-modify it; the one exception is a temporary `replace` to a local fork to apply a security fix, as docs/design.md's upgrade policy allows. Build what it lacks inside eva-md; docs/design.md lists the gaps.
- **Never change `internal/redact/baseline`.** It holds the frozen baseline redaction rules. `redact.Redact` removes the union of what the baseline rules and the current rules remove, so a change to the current rules can never keep a secret the baseline rules remove. Improve redaction in the current rules in `internal/redact`.
- **Hooks fail closed and stay fast.** A hook must exit successfully on any failure, never block the host, and make no network calls. Keep within the latency targets: capture p95 under 50 ms, startup brief p95 under 300 ms.
- **Tests never touch the real machine.** Use `t.TempDir()`. Set `HOME`, `GIT_CONFIG_GLOBAL` and `GIT_CONFIG_NOSYSTEM=1` in tests. Never read or write the real home folder, `~/.claude`, `~/.codex`, eva-md's real config or state, a real wiki, or the user's git config. The one exception is Go's own build and module caches, which `go test` itself uses: the release stand-in the end-to-end tests build shares them, in a home of its own with `GOENV=off`.
- **Tests never depend on machine speed.** CI runs on a slow macOS virtual machine, so a test may fail only when behaviour is wrong. Tests raise the production time limits once, in `TestMain`, through their variables (`internal/testlimits` raises the exported ones), and hook tests set `Slack`. A test of a time limit makes the slow side block until the test releases it or the limit's context ends, or ends the limit's time at an exact point, and bounds wall-clock time from above only with a hang guard of 30 seconds or more; a test that needs a short limit gives it to one value only, never by changing a package variable. No sleep or time window decides an outcome, or what a test can catch. Never skip or retry a test to get it to pass.
- **Don't run setup, init, install, uninstall, upgrade, the install or uninstall scripts, update, commit, maintain, system write or doctor --live against the real machine** unless the person you are working for asks. Setup, init, install, upgrade, the scripts and uninstall change host plugins, create and push a wiki, or remove an installation; update and doctor --live make real model calls on the user's subscription; update and system write change the real wiki, and commit and maintain commit and push it. Use `install --dry-run`, `uninstall` without `--apply`, `upgrade --check` and `update --dry-run --session <host>:<id>` to preview. When you are asked to change host plugins, use the hosts' native commands, preserve unrelated settings, and never grant Codex hook trust on the user's behalf.
- **Never print, log or commit secrets.** Test fixtures use made-up values. Don't log captured content; report counts instead.
- **Don't commit or push unless asked.** Never disable commit signing or switch git identity to make a command succeed.
- **Releases are cut only with `scripts/release.sh`.** Never create or push a release tag, or run the release script without `--dry-run`, unless the person you are working for asks for that release in so many words; show them the exact tag first, as `--dry-run` prints it.
- **The search index is a cache, never the truth.** It is built from the inbox and can be deleted at any time; search reads every line it proposes again from the inbox before showing it, and scans what it doesn't cover. Hooks never read or write it, and nothing said off the record may reach it.

## Before you finish a change

Run all of these from the repo root. Each must pass, and `gofmt -l` must print nothing.

```sh
gofmt -l cmd internal
go vet ./...
go test ./...
go test -race ./...
go build -mod=readonly -o bin/eva-md ./cmd/eva-md
python3 -I scripts/benchmark.py --binary bin/eva-md --cwd "$PWD"
```

The benchmark uses an isolated temporary home. `--cwd` must be a git repository outside temporary folders, such as this checkout. Its transcript-recovery figure is sensitive to machine load, so compare builds in alternating runs.

If a change alters behaviour, update docs/design.md and, where it applies, README.md (commands, flags, config keys, setup steps), docs/verification.md, docs/known-limitations.md and docs/releasing.md in the same change.
