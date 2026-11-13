# ST-01b: the profile checker reads through the codec

**Plan:** section 6.2 and 6.3 (protection fails closed), the Stage 1 rows ST-01 and ST-03. **Follows:** [ST-01](ST-01.md) and [ST-03a](ST-03a.md), both merged. **Size:** M. **Reviewers:** two: one on the contract and whether each test can fail, one who looks for a document the checker still calls unprotected that a YAML reader would call locked or verified. **Depends on:** nothing.

## Why

The profile checker (`internal/store/profile`) has a reader of its own, written before the codec. The codec's review found forms of a lock that this reader does not see, and the codec was fixed; the checker was not, since its card was closed. `internal/store/codec/SPEC.md` section 10 lists where the two differ today: in each case the codec protects and the checker reads the document as unprotected. Nothing uses either yet. But the storage properties (ST-02), the migrator (ST-04) and the store core (ST-05) will lean on the checker's answer, and two readers of one format drift again. So there is to be one reader.

## What to build

1. **The checker reads every document through `internal/store/codec`**: what is unreadable, how a frontmatter line is read, protection, a key's written value, and body sections. Its own copies go: `unreadableReason`, `parseDoc`, `lenient`, `quoted`, `protection`, `yamlKey`, `readKey`, `plainKeyEnd`, `yamlUnescape`, `stripComment`, and its `section()`. No second reading of YAML stays in the package.
2. **What the codec lacks, it gains**, as read-only exported methods with a test each, and with no change to what it already does: for example the keys of the frontmatter in file order, whether a key's value is in the written form, and the headings of the body. Say each in `SPEC.md`. Add only what the checker needs.
3. **No import cycle.** The codec's tests run `profile.Check` today from inside the package. Move those tests to an external test package (`package codec_test`), or into the checker's tests, so that the checker may import the codec.
4. **Agreement is tested as equality.** `TestProtectionBeyondProfile` and the property test now hold that the checker and the codec give the same protection for every generated form and every case of section 10's list, both ways. Section 10 of the spec is rewritten: there is one reader, and the list of differences goes.
5. **The checker's messages stay as they are** for every fault the fixture store and the checker's own tests name: the same file, field and rule. Where a document that was read as unprotected is now protected, or was readable and is now unreadable, list each such change in the report with the input that shows it.
6. **The format's specification**, `docs/spec/store-format.md`: if any sentence of it describes the looser reading (a trimmed value, a bare key with no space after its colon, a section that ends at a `# ` line), bring it in line with the codec's spec, and say in the report which sentences changed. It stays within its size limit.

## Owns and may touch

`internal/store/profile/**`. In `internal/store/codec`: new read-only exported methods and their tests, the three test files that import the checker (`corpus_test.go`, `edit_test.go`, `protection_test.go`), and `SPEC.md`. `docs/spec/store-format.md`, for item 6 only. The fixture store only to add documents; none of its present files changes.

## Must not touch

What the codec's existing exported functions do, and what `Set`, `Delete`, `SetBlock` and `SetSection` write. Any file outside `internal/store/` and `docs/spec/store-format.md`. `AGENTS.md`, `README.md` and the cards.

## Accepted when

- `scripts/linux-check.sh` passes.
- `go list -deps ./internal/store/profile` names `internal/store/codec`, and a test fails if the checker's package holds a function that reads a frontmatter line itself (name the functions of item 1 in it).
- For each of the nine cases of the codec spec's section 10 as merged, a test shows the checker now protects, and fails when the checker is given its old reading.
- The property test compares the checker and the codec for equality of protection, on at least the forms it generates today.
- A `## ` line inside fenced code is not a section for the checker, and a `# ` line does not end one: a test for each.
- Every test of the checker that passed before passes, or the report says why its expectation was wrong and what YAML reads for its input.
- A reviewer's own mutations of the checker's use of the codec (protection ignored, an unreadable document passed, a section read from fenced code) are each noticed by a test.

## As merged

Merged from `pkg/st-01b-r1` on 2026-11-12, after two reviews, a fix round and a verification that accepted it after a full check. The lead's rulings: a document at a path the layout rejects stays unread, as in the old checker, and `internal/store/codec/SPEC.md` section 10 says so; the new fault on a session, a project record or a disagreement whose protection comes only from a line the codec reads as no key stays, one fault for each key. The spec's line on who calls the codec now names the checker; ST-03b, built beside this, changes the same file, and the lead joins the two at its merge.
