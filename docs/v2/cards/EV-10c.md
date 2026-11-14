# EV-10c: tests for the must-keep items no test holds

**Plan:** section 8 Stage 1 row EV-10 (its last clause), decision 15, section 3.1 (what v1 must keep), section 9 ("Documents a team can work from"). **Design:** `evaluate.md` section 12.1, step "Guarantees first". **Part of:** EV-10: a, b, c (this). **Size:** M. **Reviewers:** one. **Depends on:** EV-10b, merged.

## Why

EV-10b found that of the audit's 291 must-keep items no test holds eleven, and ten are held in part (its card, "As merged", lists the ids; each row of a spec's `## Must keep` table ends with what is untested). They are the properties v2 changes code under most: that a hook reaches no network, model or git, that setup makes no model call, that one package alone reads the page library, that the latency targets are checked, and that the tests neither touch the real machine nor lean on its speed. A package that reworks the hooks or setup can lose any of them today and every check stays green. This package writes the missing tests. It changes nothing eva-md does.

## What to build

Tests only, in new test files. No file of the program changes, and no existing test changes. Where a property turns out not to hold as written, hold the narrowest true statement of it by a test, and report the rest: do not change code to make it hold.

1. **A hook reaches nothing outside** (CAP-K05, PRD-K01, BRF-K01, RCL-K01, STO-K14, and the hooks' half of SRC-K12), in `internal/hook/reach_test.go` (new):
   - **By what it runs.** For every hook event of both hosts that the package's fixtures drive, with a wiki, knowledge pages with their recall index, system pages, a search index, and the conditions that make an update, the daily maintain and a push retry due: `PATH` holds recording stand-ins for `git`, `ssh`, `curl`, `claude` and `codex`, and none of them is started by the hook. The one thing a hook starts is its background job through the package's spawn seam, after its output.
   - **By what it links.** No package the hook's package reaches, by imports followed through this module and its two dependencies, imports a package that can open a connection (`net`, `net/http`, `net/rpc`, `net/smtp`, `crypto/tls`, and any other the standard library offers for it; `net/url` and `net/netip` open none). Read the imports with `go/parser` or `go/build`, not by running a command.
   - **The search index.** A hook's output and what it stores are the same, byte for byte, with a valid index, a damaged one and none, and the index's files are unchanged after the run.
2. **One package reads each library** (OKF-K01), in `internal/spec` (new file): among the module's Go files that are not tests, only `internal/okfadapter` imports the page library and only `internal/config` imports the TOML library. If "nothing else parses or writes frontmatter" can be held by a static rule that is true today, hold it; else leave that half in the row as untested, in words.
3. **Setup makes no model call** (SET-K19), in `cmd/eva-md/setup_nomodel_test.go` (new): a whole guided setup against the fake host CLIs and the fake GitHub, as the package's existing setup tests run it, records every start of a host CLI with its arguments; none of them is a headless run of a prompt (the forms `internal/update` and `doctor --live` use), and the config it leaves has made none due.
4. **Time limits and speed** (SYS-K17, SET-K20, EVL-K08, and the speed half of PRD-K14, CUP-K16 and SRC-K19), in `internal/spec` (new file), by reading the module's source:
   - every package-level variable of the program whose type is `time.Duration` and whose name or comment makes it a limit, a wait or a timeout is raised by `internal/testlimits` or assigned in a `TestMain` of its package, or is on a short list in the test with the reason it needs no raising;
   - the count of `time.Sleep` calls and of `Skip` calls in test files is pinned for each package, so that a new one fails the test until the table is changed with a reason beside it. The counts may fall freely.
5. **A temporary home** (EVL-K07 and that half of PRD-K14), in `internal/spec` (same file as 4): every package whose tests start git, or name `HOME`, has a `TestMain` that calls `testgitenv.Clear()` first, or is on a short list with its reason; and no test file names a path under the real home (`~/.claude`, `~/.codex`, `~/.config/eva-md`, `~/.local/share/eva-md`) but through a home the test made.
6. **The benchmark and its targets** (CAP-K18, EVL-K09, EVL-K10, and the gate's half of EVL-K08):
   - `internal/spec/mustkeep_test.go` learns one more form of a Tests cell, `check:<path>`, for a check that is no Go test: the file must exist, and a workflow under `.github/workflows/` must run it. The latency rows then name `check:scripts/benchmark.py` and `check:scripts/bench/test_gate.py`.
   - a test in `internal/spec` reads `scripts/benchmark.py` and holds its targets to the four the specs state: capture and a prompt under 50 ms, the startup brief under 300 ms, SessionEnd under 200 ms; and that it checks what a hook wrote as well as how long it took.
7. **CI** (EVL-K13 and the token's half of EVL-K14), in `internal/release/ci_guard_test.go` (new), beside the tests that read the workflows: `ci.yml` grants `contents: read` and no job of it asks for more; its macOS job runs `go test ./...` and `go test -race ./...` with no `continue-on-error` and no filter; and the Linux job's steps run the same script a session runs.
8. **The tables.** Each row this package now holds names its tests, and loses the words "(untested: ...)" where nothing is left untested. MCP-K16 stays `none yet`: it is open with the owner. A row whose spec has no room for longer words gets a shorter Item cell, never a changed v1 block.

## Owns and may touch

New test files only, and the `## Must keep` tables.

```paths
internal/spec/**
internal/hook/reach_test.go
internal/release/ci_guard_test.go
cmd/eva-md/setup_nomodel_test.go
docs/spec/**
```

## Must not touch

Any file of the program; any existing test; the words inside a `<!-- v1 ... -->` block; a spec's `## Since the split`; `docs/v2/findings.md`; `scripts/` and `.github/` (a fault found there is reported, not mended).

## Accepted when

- The scoped check the order names passes, and `go test ./internal/spec ./internal/release` passes as root too (they read files only).
- Each new guard is shown to fail on a seeded fault, in a copy under `t.TempDir()` or by a table of made-up inputs: a hook that starts `git`; an import of `net/http` in a package the hook reaches; a second importer of the page library; a setup that starts a headless run; a limit no test raises; a new sleep in a test; a workflow that widens the token; a benchmark target changed to 60.
- No test added here sleeps, skips, or depends on the machine's speed, and each runs in a temporary home.
- The report gives, for each of the 21 ids, the tests that now hold it or the words that stay untested and why, and `go test -v ./internal/spec -run TestEachMustKeepItemSitsInASpecWithItsTests` prints the new counts.
