# EV-10b: each spec lists the must-keep items it touches and the tests that hold them

**Plan:** section 8 Stage 1 row EV-10 (its last clause), decision 15, section 3.1, section 9 ("Documents a team can work from"). **Design:** `evaluate.md` section 12.1, step "Guarantees first", shrunk by `critique-simplicity.md`: no records in JSON and no named mutation for each item, because the test suite already pins the invariants. **Size:** M. **Reviewers:** one. **Depends on:** EV-10a, merged.

## Why

The audit's register, `docs/v2/findings.md`, names 291 must-keep invariants in 17 lists, one for each part of v1 (the "Must keep" blocks). Each item cites code lines, not tests. A package that replaces or deletes v1 code must know which tests hold what it may not lose, and a deleted test that held an invariant must be noticed. EV-10a removed the old check that a cited test exists, and this package brings it back where it matters. It changes nothing v1 does, so it needs no switch.

## What to build

1. **Ids.** Each must-keep bullet of `docs/v2/findings.md` is known as `<prefix>-K<nn>`. The prefix is the one the finding ids of its part use: PRD, CAP, CUP, RED, BRF, RCL, SRC, PGS, SYS, UPM, UPW, STO, OKF, MCP, SET, OPS or EVL. `nn` is the item's place in its list, from 01. The ids are computed and never written into findings.md, which is not edited.
2. **A `## Must keep` section** in each spec of `docs/spec/` that EV-10a wrote, just before `## Since the split`. It holds the table `| Id | Item | Tests |`.
   - The Item cell is one short line in new words.
   - The Tests cell names each test as `<package path>:<TestName>` (for example `internal/hook:TestMalformedMissingAndPanicOutput`), comma-separated, or says `none yet`.
   - A spec lists every item its component touches. An item may sit in more than one spec, and every item sits in at least one.
3. **Finding the tests.** For each item, follow the code it cites to the tests that would fail if that property broke. Read the tests to find them; run no mutations. Where no test holds an item, write `none yet`. Do not write the missing test here.
4. **The check**, `internal/spec/mustkeep_test.go`. It parses the 17 lists of findings.md, and it fails on:
   - parsing fewer than 17 parts or 291 items;
   - an id that sits in no spec;
   - an id in a spec that findings.md does not have;
   - a malformed row;
   - a test that no `_test.go` file of the named package defines. Read the function names as the deleted `TestVerificationBulletsNameTheirPackages` did, or with `go/parser`.

   From then on, a package that deletes or renames a test that holds an item fails this check until it updates the table.
5. **Size.** If a table takes a spec over 24,576 bytes, move whole v1 blocks to a new spec file, and update `docs/spec/INDEX.md` and the index lines in `docs/design.md` or `docs/verification.md` to match. EV-10a's checks must still pass.

## Owns and may touch

The `## Must keep` section of every spec under `docs/spec/` except `store-format.md`. New spec files, and the matching lines of `docs/spec/INDEX.md`, `docs/design.md` and `docs/verification.md`, only as item 5 needs them. `internal/spec/mustkeep_test.go` (new).

```paths
docs/spec/**
docs/design.md
docs/verification.md
internal/spec/mustkeep_test.go
```

## Must not touch

The words inside any `<!-- v1 ... -->` block. `## Since the split` sections. `docs/v2/findings.md`. `docs/spec/store-format.md`, `internal/ledger/SPEC.md`. Any other Go file. If a test looks wrong or missing, say so in the report and do not fix it here.

## Accepted when

- `scripts/linux-check.sh` passes, with EV-10a's checks unchanged.
- The check places every one of the 291 ids, by test.
- Each fault of item 4 fails the check when seeded in a copy under `t.TempDir()`: an id removed from every spec, an unknown id, a test name that does not exist, a test named in the wrong package, and a findings copy cut to fewer items.
- The report gives, for each part, how many items a test holds and the ids marked `none yet`. The lead turns those into work for later cards.
