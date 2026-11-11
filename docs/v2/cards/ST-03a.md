# ST-03a: the line-preserving, fail-closed codec

**Plan:** section 6.2 (its bars, designs 4 and 5), decision 1, the Stage 1 row ST-03, and section 9 (a package that defines a contract writes a spec; store protection gets two reviewers). **Design:** `docs/v2/working-papers/design/store.md` sections 5.7, 6.1 and 6.2 and its row ST-03; `docs/v2/working-papers/audit/okf.md` sections 5.3, 5.4 and 9. **Format:** `docs/spec/store-format.md` sections 4 and 6. **Size:** M. **Reviewers:** two (store protection). **Depends on:** ST-01 (merged). **Part of:** ST-03, with ST-03b after it.

## Why

v1 reads a page's frontmatter in six places, and the reading that guards writes takes `locked: true  # note` as unlocked, drops comments and loses a block scalar's text. The store core (ST-05), the migration (ST-07) and system pages (DV-35) need one reader and writer that keeps every line a person wrote and reads a lock in every form YAML allows as a lock. This part builds it, with no caller.

## What to build

A new package `internal/store/codec`. It works on bytes: it opens no file, takes no lock and has no clock. Nothing calls it and no v1 code changes, so no switch is needed.

1. **Read.** `codec.Read(b []byte) *codec.Doc` never fails and never panics. A `Doc` holds the frontmatter as ordered entries (a key's line and the lines under it) and runs of lines that belong to no key, then the body: a preamble, then sections, each starting at a `## ` line outside fenced code. `(*Doc).Unreadable() string` is the reason the file cannot be read, or "": exactly the list of the format's section 6.
2. **Protection.** `codec.Protection` has the fields of `profile.Protection` (`Locked`, `Verified`, `Unreadable`). `(*Doc).Protection()` reads them fail-closed, as the format's section 6 says: any `locked` value but a bare `false`, `False` or `FALSE` locks, a comment after it allowed; so does a repeated `locked`. A `locked` or `verified` key in any form but the bare `key:` protects whatever its value; an alias key, a merge key or a non-scalar explicit key protects both ways.
3. **Values.** `(*Doc).Value(key) (any, error)` decodes a key in a written form of section 4: a plain scalar as a string, JSON with numbers as `json.Number`, a block list as a list of objects. It returns `codec.ErrNoKey`, `codec.ErrRepeated` or `codec.ErrNotWritten` (a form it does not decode) and never guesses.
4. **Write.** `codec.Write(d *Doc) ([]byte, error)`. An unchanged `Doc` writes back its input byte for byte, unreadable input included. Edits: `Set(key, v)`, `SetBlock(key, list)` (the `key:` then `  - {…}` form), `Delete(key)`, and on the body `Preamble`, `SetPreamble`, `Section(heading)`, `SetSection` (added at the end when absent) and `RemoveSection`. An edit changes only the lines of what it names. A new key goes after the last entry. `Set` replaces an entry in whatever form it was written, and refuses a repeated key.
5. **The written form.** A string in the plain alphabet of section 4 that is not a YAML word is written plain. Anything else is marshalled by `encoding/json` with HTML escaping off (struct field order kept), then spaced with one space after each `:` and `,` outside strings. A top-level value that is not a string, list or object is refused.
6. **Refusals.** Every edit of a locked or unreadable `Doc`, and any `Set` or `Delete` of `locked`, returns `codec.ErrProtected` and changes nothing. What a verified mark forbids depends on who asks, so the store core enforces it (ST-05); the codec reports it.
7. **Bounded read.** `codec.ReadFrom(r io.Reader, max int64) (*Doc, error)` reads at most `max` bytes and one, refusing more with `codec.ErrTooLarge`; otherwise it equals `Read`.
8. **A spec**, `internal/store/codec/SPEC.md`, at most 24 KB: the API above, each invariant with the test that holds it, and where the codec and `internal/store/profile` must agree.

## Owns and may touch

`internal/store/codec/**` (new), except `internal/store/codec/v1.go`, `internal/store/codec/v1_test.go` and `internal/store/codec/testdata/v1/**`, which are ST-03b's.

## Must not touch

Everything else. `internal/store/profile`, the fixture store, `docs/spec/store-format.md`, `internal/okfadapter` and the v1 page packages are read, not edited. `go.mod` does not change.

## Accepted when

- `scripts/linux-check.sh` passes.
- `Write(Read(b)) == b` by test over a corpus of hand edits in `internal/store/codec/testdata/corpus/`: comment lines and trailing comments, block scalars, flow mappings, repeated keys, a quoted key, CRLF ends, no final newline, a 100 KiB line, a byte-order mark, conflict markers, an unclosed frontmatter, `---` and `## ` inside fenced code; and by `FuzzRoundTrip` over any bytes, whose seeds run in `go test`.
- `locked: true  # x` is locked. A property test over generated values and key forms finds no misread: a value the codec cannot read protects. On every document of `internal/store/testdata/fixture-store`, and on each protection case of the corpus placed in a copy of it, `Protection()` equals `profile.Check`'s `Report.Protected` for that path.
- After one edit to a readable, unlocked document, every byte outside the edited entry or section is unchanged and `Read` gives back the value set, by property test.
- Setting each key an item may hold, with generated values, on a copy of a fixture item leaves `profile.Check` with no `written-form` or `frontmatter` fault for it.
- Each refusal of step 6 has a test, and the refused `Doc` still writes its input.
- No error text names the library v1 was built on. `go test -run '^$' -fuzz FuzzRoundTrip -fuzztime 10m ./internal/store/codec` finds no panic; report the run (a report, not a gate).
