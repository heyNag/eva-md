# SEAM-0d: guards on the plugin's bytes and on the upgrade from 2026.11.5

**Plan:** section 8, Stage 0, row SEAM-0 (the hash test and the upgrade test), and section 6.5, design 2. **Size:** S to M. **Reviewers:** one.

## Why

Codex binds a plugin's approval to a hash of every file in it, so one changed byte in the plugin folder can cost every user a new hook review. Nothing in the tests says so today: a template edit passes CI. And the one real install runs release 2026.11.5 (tag `2026.11.5`, commit 4f5fa2c), so the upgrade from that layout is the one that must never break.

## What to build

1. **A hash test over the whole Codex plugin folder.** Render the Codex plugin as install does, with a fixed made-up binary path, into a temporary folder. Compare the name and SHA-256 of every file, and one hash over the sorted list, with a golden file under `internal/install/testdata/`. The golden holds the bytes `main` renders today. The failure message says in one sentence what a changed byte costs (a new Codex hook review for every install) and how to update the golden on purpose. Do the same for the Claude Code plugin folder in a second golden, with a message that says no review is bound there. Look first at the golden tests that exist (`claude-hooks.golden.json`, `codex-hooks.golden.json` and the two MCP goldens) and do not duplicate what they hold: the new test is over the whole folder, skills and icon included.
2. **A fixture of what 2026.11.5 installed.** Build the binary of tag `2026.11.5` in a scratch worktree, run its install into a temporary home with the fake host CLIs the tests already use (see `cmd/eva-md/release_test.go` and the install tests), and record what it left: the plugin folders, the install record, and every other file it wrote under the temporary home, with the binary itself replaced by a small placeholder and the temporary home's path replaced by a token. Commit the result under `internal/install/testdata/installed-2026.11.5/`, with a short `README` in it that gives the exact commands that made it, so it can be made again.
3. **An upgrade test from that fixture.** A test lays the fixture down in a `t.TempDir()` home (token replaced), runs the current install over it, and checks: the install succeeds; the install record names the new files; every plugin file 2026.11.5 wrote is either replaced by the current one or removed; nothing outside what the two installs own is created, changed or removed; and a second run changes nothing.

## Owns and may touch

New test files in `internal/install` and, if the upgrade test must run the binary, in `cmd/eva-md`; `internal/install/testdata/**`. Test helpers may be added in `_test.go` files.

## Must not touch

Any production (non-test) Go file, any template, any workflow or script, `AGENTS.md`, the docs. If the upgrade from 2026.11.5 turns out to be broken on `main`, do not fix it here: write the failing test, mark nothing as skipped, and report it.

## Accepted when

- `scripts/linux-check.sh` passes.
- The hash test fails when one byte of a rendered plugin file is changed (show it inside the test, on a copy in memory) and passes on `main`.
- The upgrade test passes on `main`, and fails when a file 2026.11.5 wrote is left behind (show it with a seeded leftover inside the test).
- The fixture holds no real path, name or secret: a search of it for the builder's home path and user name finds nothing.
- Nothing here touches the real machine: every home is a `t.TempDir()`, `GIT_CONFIG_GLOBAL` and `GIT_CONFIG_NOSYSTEM=1` are set, and a package whose tests run git calls `testgitenv.Clear()` in `TestMain`.
