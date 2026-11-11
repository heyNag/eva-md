# HO-10a: the host adapter and its table

**Plan:** section 6.5 (design 1), section 4.2, section 8 Stage 1 row HO-10 and "What runs in parallel" (`internal/host/adapter` is the hosts track's), section 11's first risk. **Design:** `docs/v2/working-papers/design/hosts.md` sections 3.1 to 3.4, contract H-D1 in section 17, and its row HO-10 in section 21. **Part of:** HO-10, built in parts: a (this), then b (install and setup). Health and the doctor's command file move to the adapter with HO-12, which rebuilds those files. **Size:** M. **Reviewers:** one.

## Why

v1 has two names for one host (`claude` in install and setup; `claude-code` in the hook and the update), host names as literals in about 30 files, no code that reads a host's version, and quirks matched by wording where they are used. One package that knows a host lets the others ask it, and makes a third host a table row and an adapter.

## What to build

A new package, `internal/host/adapter`. Where hosts.md says `internal/host`, read `internal/host/adapter` (the plan's section 8 ruling); `internal/host/capture` is the record track's and is not made here. No v1 file changes and nothing imports the package yet, so it changes nothing v1 does and needs no switch.

1. **One id per host.** `type ID string`, with `ClaudeCode ID = "claude-code"` and `Codex ID = "codex"`, the spellings `eva-md hook --host` and `eva-md mcp --host` accept today. `All()` gives the hosts in v1's order: Claude Code, then Codex.
2. **Names.** A `Names` value for each host with at least `App` (`Claude Code`, `Codex`), `CLI` (`claude`, `codex`, the command that is run) and `InstallName` (`claude`, `codex`: the spelling of install's, setup's and uninstall's `--host`, of the install record's `host` field, of the folder `<share>/marketplace/<name>` and of the keys of `cli-paths.json`). These spellings are stored on users' machines and never change; the adapter is the one place they are written. `ByID` and `ByInstallName` accept exactly the spellings v1 accepts in each place (`ByID("claude")` finds nothing).
3. **The spec table** (`spec.go`), data in the binary: rows for each host and version range, each with its lowest and highest version, the events v1 declares on that host (the ten and the seven that `internal/install/templates.go`'s `render` writes) and its quirks. The newest row's highest version is the tested maximum, the newest version eva-md was checked against: Claude Code 2.1.316 and Codex 0.166.0 (`docs/verification.md`, "Live checks"). Each row cites its source. Enter only what the repository records; values plan section 4.2 read from documentation and no real host has confirmed are left out.
4. **Quirks.** A `Quirk` has an id, a host, a version range, one line saying what it is, and a fixture under `testdata/quirks/` that shows it. Enter at least the three hosts.md section 3.1 names: Codex 0.166.0's line for a marketplace whose folder is missing (`folderMissing` in `internal/install/host.go`); the keys a marketplace's root is read from (`inspect`, same file); and the hooks row of `codex features list`, read by its first and last column (`internal/health/hosts.go` and `internal/install/install.go`). Fixtures are made up, in the form v1's tests fake. The matching code stays where it is; part b and HO-12 move it.
5. **The probe.** `Probe(ctx, id, env)` returns a `ProbeResult`: the CLI's absolute path, found on PATH with its folder's symlinks resolved exactly as `cliPath` in `internal/install/host.go` does; the version, the first `N.N.N` token of what `<cli> --version` prints, or empty; the row it falls in; `Known` (a row covers it); and `Untested` (above the tested maximum, when the newest row is used). Export `ConditionVersionUntested = "host.version_untested"` for HO-12. The probe runs from `/`, reads no settings file, calls no model and uses no network; PATH lookup and the command runner come in through `env`; its time limit is an unexported variable the package's own `TestMain` raises. Results are cached in memory by the CLI's path, size and modification time.
6. **The literal count.** A test parses every non-test Go file under `cmd/` and `internal/` outside `internal/host/` with `go/parser` and counts, per package, the lines holding a string literal whose whole value is `claude`, `claude-code`, `codex`, `Claude Code` or `Codex`. It compares each count with `testdata/host-literals.txt` (one line per package: its path and count), fails on any difference naming the package and both numbers, and logs each count and the total as `host_literal_lines`. A change that lowers a count lowers its line; no count may rise.

## Owns and may touch

`internal/host/adapter/**` (new).

## Must not touch

Every other path. No v1 file imports the new package in this part.

## Accepted when

- `scripts/linux-check.sh` passes, and no existing file has changed.
- By test, `ByID` and `ByInstallName` accept exactly v1's spellings and refuse each other's, and `All()` is in v1's order.
- A fake CLI, a script written in `t.TempDir()` that prints a made-up version, probed at three versions: below the lowest row gives `Known` false; inside a row gives that row; above the tested maximum gives the newest row and `Untested`. Output with no version gives an empty version and `Known` false. A command that never exits ends when the test cancels the context, under a hang guard of 30 seconds or more; no sleep decides an outcome.
- A second probe of an unchanged file runs no command; after `os.Chtimes` changes its modification time, the command runs again.
- A copy of the table with one quirk's fixture removed fails the quirk check; the real table passes it.
- The literal count passes on the tree; on a temporary tree, one added literal fails it with that package named, and a literal in a comment or a `_test.go` file is not counted.
- A third host registered only in a `_test.go` file is found by `All` and `ByID` and gets its spec row, with no other change.
- The package imports no new module, and only eva's name appears in its text.
