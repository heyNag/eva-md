# ST-03c: the codec's protection reads a lock as v1 read it, behind white space, quotes and escapes

**Plan:** section 6.2 (a lock honoured in every form YAML allows; design 4), the Stage 1 row ST-03 ("every unreadable protection value protects"). **Follows:** [ST-03b](ST-03b.md), whose notes owe it before ST-05 reads a page through the codec. **Design:** `docs/spec/store-format.md` section 6. **Size:** S. **Reviewers:** two: the contract; protection. **Depends on:** ST-03b (merged).

## Why

ST-03b found nine forms v1 took for a lock or verified mark that `Protection` (`internal/store/codec/frontmatter.go`) reads as unprotected, listed in its `SPEC.md` section 12: `locked :true`, Unicode white space before a key or its colon, white space inside a key's quotes, a quote that never closes or runs past a colon. v1 cut a key line at its first colon and trimmed Go's white space and quotes (`VerifiedIn` in `internal/okfadapter/adapter.go`), or read a double-quoted key as JSON (behind `IsLocked`). ST-07 would migrate such a page unlocked. Nothing in the binary calls the codec: v1 is unchanged, no switch.

## What to build

1. **`Protection` reads more as a mark, never less.** In `frontmatter.go`:
   - A key line or opaque line, as its first loop reads them, protects as `locked` or `verified`, whatever its value, when the text before its first colon (inside quotes too) trims to that word, trimming at both ends, in any order, each rune `unicode.IsSpace` reports, U+FEFF, `"` and `'`; or when that text, trimmed of white space and U+FEFF, is one double-quoted string whose escapes, read as `yamlUnescape` reads them, give such a text (U+00A0, then `"lo\u0063ked": x`). Only the bare canonical line is still read by its value.
   - An entry whose key, as `readKey` decodes it, trims by the same runes to `locked` or `verified` protects as that key (`"locked\_": x`).
   - These protect as both, as the key `*` does: a key line or opaque line that, after white space and U+FEFF, starts with a quote not closed on the line (`"verified: me`, but not `genrat"d: x`); a double-quoted key holding an escape outside YAML 1.2's list (which holds `\_`, `\L`, `\P`), or a `\x`, `\u` or `\U` that is no character (a surrogate too).
   Only what `Protection` returns changes (an edit refuses a newly locked one). `ReadV1` still refuses the nine forms.
2. **`yamlUnescape`, checked.** It reads `\_`, `\L` and `\P` as U+00A0, U+2028 and U+2029, as YAML does, not as a space. Correct what says otherwise: the comment on `unquoteV1`, the row "an escape read as a space" in `TestReadV1Refusals`, section 12. `TestYAMLEscapes` holds each escape of YAML 1.2's list to its character.
3. **`TestNoLibraryName`** in `codec_test.go` checks the text of `ErrUnreadable` too, and fails when its list of errors differs from the exported `Err` variables `go/parser` finds in the package.
4. **The differences.** `TestV1Differences` in `v1_test.go` expects each of the nine refused and protected as its mark, the unclosed quote as both. `protectsLess` in `v1oracle/oracle_test.go` lists the five empty `verified` forms only.
5. **Owed by ST-03b:** `testdata/v1/differences/colon-end-comment.md` (`writer: Tide: # x`) and its row: refused, `ErrNotWritten`.
6. **`SPEC.md`:** item 1's rules in section 4, the new tests in section 9, five differences in section 12, within `TestSpec`'s 24 KB. Name the oracle's test without backticks: `TestSpec` reads only this folder's tests.

## Owns and may touch

`keyforms_test.go`, `colon-end-comment.md` and `v1oracle/protection_test.go` are new; in `v1.go`, comments only.

```paths
internal/store/codec/frontmatter.go
internal/store/codec/v1.go
internal/store/codec/SPEC.md
internal/store/codec/codec_test.go
internal/store/codec/v1_test.go
internal/store/codec/keyforms_test.go
internal/store/codec/testdata/v1/differences/colon-end-comment.md
internal/store/codec/v1oracle/oracle_test.go
internal/store/codec/v1oracle/protection_test.go
```

## Must not touch

Everything else. `internal/store/profile`, `conformance` and `migrate` read `Protection` and are not edited: if a test outside these paths fails, stop and report. `docs/spec/store-format.md`: a sentence its section 6 needs goes in the report. `internal/okfadapter`, `go.mod`, the corpus, the goldens. No file of the codec's own package names the library.

## Accepted when

- `scripts/linux-check.sh` passes.
- **A row for each case.** `TestProtectionKeyForms` in `keyforms_test.go` holds each of the nine forms; U+00A0, U+2003 and U+3000 before a key, before its colon, inside and around its quotes; each escape that decodes to white space (`\_`, `\L`, `\P`, `\N`, `\t`, `\u00a0`); a letter spelled by an escape after white space; an unclosed and a run-on quote; an unknown escape; a surrogate; each with its exact protection. Unprotected: `status: stable` with U+00A0 before the colon, `genrat"d: x`, `Locked: true`, `lockedness: x`, `# locked: true`, `title: locked: x`. Every positive row fails on the codec as merged before.
- **The property.** `TestProtectionCoversV1` in `v1oracle/protection_test.go`: 10,000 documents from a fixed seed, or `-seed`, logged. A key's line mixes white space before it, quotes (none, matched, mismatched, unclosed), white space and escapes inside them, letters spelled by escapes, the word or a near miss, white space and a quote before the colon, zero to two colons, a value (true, yes, quoted, false, a flow mapping, a list, empty, `~`, a comment, lines under it), placed anywhere among other keys, at times in a block indented whole. v1's reading is `okfadapter.IsLocked` and `IsVerified` of the page read from a wiki (`libraryRead`); the codec's is `codec.Read(b).Protection()`. No document v1 reads as locked is neither `Locked` nor `Unreadable`; likewise verified, but for a bare `verified` line in one of section 12's five empty forms. It fails unless v1 protects 2,000 documents by a line other than the bare one. On the codec as merged before it fails: the report gives two of its documents.
- `TestProtectionProperty` and `TestProtectionBeyondProfile` pass unchanged.
- `TestDifferencesAreDifferences` passes with five names in `protectsLess`.
- `TestNoLibraryName` fails on a new exported error that names the library.
- A reviewer's mutations (trim spaces and tabs only; cut at the first colon outside quotes only; no quote trim; no escape read; no both-ways rule) each fail a row.
- No built line is added to `./cmd/eva-md`: nothing there imports the codec.

## The lead's rulings of 2026-11-13

The card's checker left these to the lead.

- `ReadV1` keeps refusing `\_`, `\L` and `\P`; only the reason given for it is corrected. ST-03b's notes said the codec read them as a plain space: it reads them as YAML does.
- The both-ways rule stands for a line that starts with a quote not closed on it, and for a quoted key with an escape YAML does not have.
- The colon-end file stays in this card, though ST-04b carries the same file: whichever merges first writes it, and the other finds it there and adds only what is missing.
- The profile checker will report a carried mark in the new forms: that follows ST-01b's ruling.
- The floor of 2,000 protected documents in 10,000 is the drafter's figure: the builder reports the count the generator gives, and tunes the generator, not the floor, to reach it.
