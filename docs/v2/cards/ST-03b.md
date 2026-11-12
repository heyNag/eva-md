# ST-03b: v1's pages read through the codec, as v1 read them

**Plan:** decision 1, section 6.2 design 5, the Stage 1 row ST-03 (its clause "parse equality with okf on v1 pages"); ST-07 and DV-35 read v1's pages through the codec. **Design:** `docs/v2/working-papers/design/store.md` sections 6.1 and 6.4 and its row ST-03; `docs/v2/working-papers/audit/okf.md` sections 4.3, 5.3, 9 and 10. **Format:** `docs/spec/store-format.md` section 5, the frozen system page. **Size:** S to M. **Reviewers:** one. **Depends on:** ST-03a, merged. **Part of:** ST-03.

## Why

The migration (ST-07) must read every v1 page as v1 read it, and system pages, frozen and kept (DV-35), are v1 pages read through the same codec. v1 read them through the page library it was built on, which leaves eva-md at the cut-over (decision 1). This part reads that dialect with the codec, proves it agrees with the library on every page v1 writes, and keeps the proof in goldens once the library is gone.

## What to build

1. **`codec.ReadV1(b []byte) (codec.V1Page, error)`** in `internal/store/codec/v1.go`, on ST-03a's `Read`: no second line reader. `V1Page` holds `Doc *Doc` and the fields eva-md uses, named as the library's page model names them: `Type`, `Title`, `Description`, `Status`, `StaleAfter`, `Generated` (`By`, `At`), `Verified` (a list of `By`, `At`), `Sources` (a list of `ID`, `Resource`, `Title`, `Author`, `LastModified`, `UsageCount`, `Extra`), `Extra` for the other keys v1 writes or reads (`aliases`, `as_of`, `branch`, `head`, `kind`, `locked`, `model`, `model_note`, `parent`, `project`, `remote`, `repos`, `scope`, `state`, `writer`), and `Body`. It reads the forms the library writes: flow mappings with unquoted values (`generated: { by: x, at: "…" }`), block lists of mappings (`sources:` then `  - resource: …`), flow lists, JSON values and plain scalars. Protection stays `Doc.Protection()`, fail-closed; v1's own reading of a lock is never used.
2. **Differences on purpose.** Where the library misreads a hand edit (a comment after a value, a repeated key, a block scalar, a byte-order mark, a line over 64 KiB, a quoted `"false"` lock), the codec keeps its own reading. A table test names each difference and the codec's answer.
3. **Goldens.** The library's reading of each fixture page, as JSON in `internal/store/codec/testdata/v1/`, so the comparison still runs after the module leaves `go.mod` (ST-13 freezes them).
4. A section on v1 pages in `internal/store/codec/SPEC.md`.

## Owns and may touch

`internal/store/codec/v1.go` (new), `internal/store/codec/v1_test.go` (new), `internal/store/codec/testdata/v1/**` (new), and a new section of `internal/store/codec/SPEC.md`.

## Must not touch

Everything else, ST-03a's other files included; a fault found in them is reported, not fixed here. `internal/okfadapter`, `internal/threadpage`, `internal/knowledgepage`, `internal/systempage`, `internal/update`, `internal/wikiinit` and `go.mod` are read, not edited. The library is reached only through `internal/okfadapter`, as the project's rule requires: no file here imports it.

## Accepted when

- `scripts/linux-check.sh` passes.
- **Fixture pages.** Made-up pages in `testdata/v1/`, at least one of each type v1 writes (`thread`, `rules`, `now`, `knowledge`, `map`, `system`, and `guide`, setup's README), the thread page copied from `internal/okfadapter/testdata/thread.golden.md`. Each, read through `okfadapter` (`Init`, `Open` and `Read` in a temporary folder, as `internal/threadpage/okf_test.go` does) and through `ReadV1`, gives equal fields, and equals its golden.
- **Generated pages.** 10,000 pages from a seeded generator (the seed is logged) with the fields, `Extra` keys and value types that `internal/update/wiki.go`, `internal/wikiinit/init.go`, `internal/knowledgepage/page.go` and `internal/systempage/write.go` set, saved as `okfadapter.Serialize` writes them, with a `generated` line, and read by both: every field equal, `Body` byte for byte. Values include colons, `#`, quotes, brackets, leading spaces, non-ASCII text, `true`, `null`, numbers and dates.
- The difference table holds, and no fixture or generated page that v1 reads as locked (`okfadapter.IsLocked`) or verified (`okfadapter.IsVerified`) reads as unprotected through the codec.
- No error text names the library.

## As merged

Merged from `pkg/st-03b-r3` on 2026-11-12, after a review, three fix rounds and three verifications; the last read sixty pages of its own through v1's library and through `ReadV1` and accepted. The lead's rulings:

- **`verified: {}` keeps no verified mark**, as the format's section 6 says, and the spec's differences name it among those that protect less than v1.
- **Fourteen differences protect less than v1 did**, each named in the codec's spec, section 12, and in the oracle's list. Five are an empty `verified`. `ReadV1` refuses the other nine outright: they are keys the codec's own `Protection` does not read as a lock or a verified mark, such as `locked :true`, a no-break space before `locked`, or a quote that never closes. ST-07 decides what a migrated page with any of them keeps.
- **Three lines of the spec outside its new section are accepted**: `ReadV1` in section 2's list, `ErrUnreadable` in the sentence on errors, and the removal of section 11's line that left v1's pages to this card. The spec is 24,572 bytes, four under its limit: a new sentence in it needs others cut.
- **`internal/store/codec/v1oracle/oracle_test.go`** is accepted outside the card's list, since the codec's own files may not name the library.

For ST-03a's code, found here and owed as a card of its own before ST-05 reads a page through the codec: `Protection` misses a key that v1's reading took for a lock or a verified mark when Unicode white space stands before the key or before its colon, when the key is quoted with such a space inside or around the quotes, or when a quote on a `verified` line never closes or runs on. It should read a key that trims, by Unicode white space, to `locked` or `verified` as that mark. Its `yamlUnescape` also reads three escapes as a plain space where YAML reads another character, and `TestNoLibraryName` leaves `ErrUnreadable` out of its list.

Owed, small: no test covers the refusal of a plain value that ends in a colon with a comment after it (`writer: Tide: # x`); one difference file with its row closes it.
