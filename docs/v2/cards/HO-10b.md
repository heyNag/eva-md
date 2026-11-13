# HO-10b: install and setup ask the adapter

**Plan:** section 6.5 (design 1, and "Kept"), section 8 Stage 1 row HO-10 and "What runs in parallel" (`internal/install` is the hosts track's, its hook list the lead's). **Design:** `docs/v2/working-papers/design/hosts.md` sections 3.1 and 3.4, and its row HO-10 in section 21. **Part of:** HO-10: a, then b (this). Health and the doctor's command file follow with HO-12. **Size:** S to M. **Reviewers:** one. **Depends on:** HO-10a merged; SEAM-0d, whose byte and upgrade tests are this part's guard. HO-11 rebuilds the same files and starts after this part merges.

## Why

`internal/install` and setup branch on `"claude"` and `"codex"` in about 25 lines, use the one string as the host's name, its command and the folder it is installed in, and match three host quirks in place. This part moves that knowledge into the adapter HO-10a built, so the install manifest (HO-11) starts from code that asks one place.

## What to build

1. **No literal left.** In the non-test Go files of `internal/install` and in `cmd/eva-md/setup.go` and `cmd/eva-md/setup_checks.go`, each comparison with, or literal of, a host's spelling becomes a read of the adapter: its `ID`, its `Names` (`App`, `CLI`, `InstallName`), or a field or function added to the adapter for what one host does differently (its manifest paths, its add, remove and validate arguments, whether it is asked one marketplace at a time, whether a changed plugin needs a review in the host). Each added field is documented in the adapter and set in the table for both hosts. Where v1 uses one string as both command and install name (as `native` in `host.go` runs the host by its install name), use the field that says what the string is for; the bytes are the same.
2. **One CLI lookup.** `cliPath` in `host.go` and the `exec.LookPath` calls on a host's command in install and setup go through the adapter's lookup, the one its probe uses, without running `--version`.
3. **The quirks move beside their fixtures.** Codex's wording for a missing marketplace folder (`folderMissing`), the keys a marketplace's root is read from (`inspect`) and the reading of the hooks row of `codex features list` (`install.go`) become adapter functions, tested on HO-10a's fixtures. Install keeps its exact rule: it refuses only on a `hooks` row whose last column is `false`. The doctor's rule differs (it passes only on `true`; HO-12 moves it), so the function returns enough for both, and a test pins inputs on which the two rules give different answers.
4. **The render.** `render` in `internal/install/templates.go` and `MCPManifest` in `internal/install/mcp.go` take the manifest paths, the skill's host and the `--host` value from the adapter. The entries of the hook list (event names, matchers, timeouts, async, Codex's context limit) are the lead's and stay as they are, byte for byte; only the comparisons around them change. A new test checks that the events `render` writes for each host equal the adapter's event list for that host.
5. **The count.** In `internal/host/adapter/testdata/host-literals.txt`, set `internal/install` to 0 and lower `cmd/eva-md` by the lines this part removes.

What stays exactly as it is: every byte of both plugin folders; the install record's bytes and its `host` values; `cli-paths.json`; the marketplace folders' names; every message, flag, accepted spelling, host command and argument; the order the hosts are handled in. This part changes nothing v1 does, so it needs no switch.

## Owns and may touch

The non-test Go files of `internal/install` (in `templates.go`, only what item 4 allows), new test files in `internal/install`, `cmd/eva-md/setup.go`, `cmd/eva-md/setup_checks.go`, and `internal/host/adapter/**`.

## Must not touch

Existing test files in `internal/install` and `cmd/eva-md`, which pass unchanged. `internal/install/templates/**` and `internal/install/testdata/**`. `internal/health/**`, `cmd/eva-md/commands.go`, `main.go` and `mcp.go` (HO-12's, or other tracks'). `internal/config/**`, including the hosts key in `answers.go`. `internal/hook/**`, `internal/update/**`.

## Accepted when

- `scripts/linux-check.sh` passes, and no existing test has changed. In particular SEAM-0d's plugin byte hashes, the upgrade from 2026.11.5, and the install, uninstall, rollback and setup tests pass as they are.
- The literal count reads 0 for `internal/install` and the lowered figure for `cmd/eva-md`.
- Each moved quirk's function passes on its fixture, and the inputs on which install's and the doctor's features rules differ are pinned by a test.
- The events test of item 4 passes, and fails when an event is removed from the adapter's list for one host.
- The diff of `templates.go` changes no line inside the event list's entries, which a reviewer checks.

## As merged

Merged from `pkg/ho-10b-r1` on 2026-11-12, after a review that accepted it with three notes on tests, a fix round that added them and changed no code, and a closing verification with no finding. The verifier ran 26 install, uninstall and setup scenarios with the branch's binary and `main`'s, in made-up homes with fake host CLIs, and found every line printed, every exit code, every argument passed to a host and every file written the same.

The lead's ruling: the comparison `h.ID == adapter.Codex` in `templates.go`, for the event list and the form of the hook command, stays until HO-11 rebuilds the hook list. `MCPManifest` and `render` keep v1's handling of a name that is no host.

The latency gate could not judge on the build machine, which was busy each time; the code a hook runs gained only lookups in the adapter. CI's macOS latency job on the merge is the run of record.
