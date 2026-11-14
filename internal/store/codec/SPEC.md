# The store codec

`internal/store/codec` reads and writes the bytes of one store document: an item, a workstream, a disagreement, a session file, a project record or a frozen system page, as [the store's written format](../../../docs/spec/store-format.md) describes them (cited below as "the format", by section).
It is the one reader and writer of frontmatter that the store core (ST-05), the migration (ST-07) and system pages (DV-35) build on. It keeps every line a person wrote, reads a lock in every form YAML allows as a lock, and writes only the format's written forms.

It works on bytes. It opens no file, takes no lock, has no clock and makes no network call. It holds no state between calls. The profile checker reads every document through it (section 10); nothing else calls it yet.

## 1. The model

A document is read as lines, each kept with its own end (LF, CR LF, or none on the last line).

- **Frontmatter.** Line 1 is `---`; the first later `---` line closes it. The lines between are, in order, **entries** and **opaque lines**. An entry is a key's line and the lines that belong to it. An opaque line belongs to no key: a blank line, a comment at the top level, a line before the first key, or a line in no form the reader knows. Every line keeps its bytes and its place.
- **Body.** The lines after the closing `---`: a **preamble**, then **sections**. A section starts at a line that begins `## ` outside fenced code and runs to the next. Its heading is the line's text after `## ` (a CR before the end is not part of it). A fence opens at a line of up to three spaces and then three or more backticks or tildes (backticks with no backtick after them) and closes at a line of up to three spaces, at least as many of the same character, then only spaces or tabs; an unclosed fence runs to the end. So `---` and `## ` inside fenced code are body text.

How a frontmatter line is read, with CR before the end ignored:

1. The **top level** is the indentation of the block's first line that is neither blank nor a comment. YAML allows a block indented as a whole.
2. A line at the top level that is `key:` with the key in `[A-Za-z_][A-Za-z0-9_-]*` and the colon followed by a space, a tab or the line's end starts an entry. It is **bare** (canonical) only when the top level is column 0. `title:{x: y}` is no such line: YAML reads the key `title:{x`.
3. A line at the top level that YAML reads as a mapping key in another form starts an entry too: a quoted key (`"locked"`, `'locked'`, with YAML's escapes, `"lock\x65d"`), spaces or tabs before the colon, an explicit `? key`, a tag or an anchor before the key (`!!str locked`, `&a locked`), an alias as the key (`*a`, its name in libyaml's alphabet `[A-Za-z0-9_-]`, so `*a: x` is the alias then the colon; read as the key `*`), a flow collection closed on the line as the key (`[locked]: x`, read as `*`), or the merge key `<<`. An explicit key whose node is not a scalar on its line (`? [locked]`, `?` alone) or is a compact mapping (`? note: x`, `? locked : false`) is read as the key `*`. No key starts with a space, a tab or U+FEFF.
4. A `: value` line at the top level after an explicit key with no value line yet, and after any lines that continue the key (`? a`, `  b`, `: v` is one entry), is that key's value line.
5. A line indented deeper than the top level, or one starting with `-`, belongs to the last entry above it, across any opaque lines between.
6. Anything else is opaque.

## 2. The API

```go
func Read(b []byte) *Doc                              // never fails, never panics
func ReadFrom(r io.Reader, max int64) (*Doc, error)   // reads at most max bytes and one
func Write(d *Doc) ([]byte, error)                    // fails only for a nil Doc

func (d *Doc) Unreadable() string                     // the reason, or ""
func (d *Doc) Protection() Protection                 // {Locked, Verified, Unreadable}
func (d *Doc) Value(key string) (any, error)

func (d *Doc) Set(key string, v any) error
func (d *Doc) SetBlock(key string, list any) error
func (d *Doc) Delete(key string) error

func (d *Doc) Preamble() string
func (d *Doc) SetPreamble(text string) error
func (d *Doc) Section(heading string) (string, error)
func (d *Doc) SetSection(heading, text string) error
func (d *Doc) RemoveSection(heading string) error

func UnreadableText(b []byte) string                  // section 3's reasons but the frontmatter's two
func (d *Doc) Entries() []Entry                       // every entry, in file order
func (d *Doc) Opaque() []int                          // the frontmatter's lines that belong to no key
func (d *Doc) Sections() []Section                    // every section, in order

func ReadV1(b []byte) (V1Page, error)                 // a v1 wiki's page, section 12
```

The last four are for a reader that checks a document rather than edits it, the profile checker (section 10). An `Entry` is a key as section 1 reads it (`*` and `<<` as section 4 names them); its `Line`, the opening `---` being line 1; whether it is `Written`, and then its `Value` as `Value` decodes it; its `Scalar`, the value on the key's line with spaces and tabs around it, its quotes and a comment after a plain value dropped; and its `Text`, what follows the colon, then each line under the key after an LF.
`Opaque` counts lines alike. A `Section` is a heading and the text `Section` gives for it. A repeated key or heading is listed each time. An unreadable document has no entries, opaque lines or sections. Each call decodes afresh.

Errors: `ErrProtected`, `ErrNoKey` (no such key or section), `ErrRepeated` (the key or section is there more than once), `ErrNotWritten` (a value in a form the codec does not decode, or an edit it cannot write in a form it reads back), `ErrTooLarge`, and from `ReadV1` alone `ErrUnreadable`. An edit that returns an error changes nothing. A refusal's text may add a reason after the sentinel; callers test with `errors.Is`.

The zero `Doc` reads as the empty document: unreadable, "no frontmatter on line 1".

## 3. Unreadable

`Unreadable()` is exactly the list of the format's section 6. The reason is the first of these texts that applies, the two line checks taken line by line:

| Reason | When |
| --- | --- |
| `a byte-order mark` | the bytes start with EF BB BF |
| `invalid UTF-8` | |
| `a NUL byte` | |
| `line <n> is over 64 KiB` | a line, CR included, over 65,536 bytes |
| `a conflict marker on line <n>` | a line, CR dropped, that is `<<<<<<<`, `=======`, `>>>>>>>` or `|||||||`, alone or followed by a space |
| `no frontmatter on line 1` | line 1 is not `---` (an empty document included) |
| `a frontmatter that does not close` | no later `---` line |

An unreadable document keeps its bytes: `Write` gives them back. It has no entries, no value (`Value` and `Section` return `ErrNotWritten`; `Preamble` is ""), and its protection is `{Unreadable: true}` and nothing else.

## 4. Protection

Read fail-closed, as the format's section 6 says. Over-protection is allowed; a misread that unlocks is not.

- **Locked** when there is a `locked` entry in any form but the bare key with one of the values `false`, `False` or `FALSE`, unquoted, after a space or a tab, alone on its line. Spaces and tabs around it and a comment after it (a space, then `#`) are allowed; no other white space is (a no-break space makes the value a string). Anything else locks: an empty value, `"false"`, `no`, `false#x`, a comment after a tab, a value on the next line, a list. Two `locked` entries lock, whatever their values.
- **Verified** when there is a `verified` entry in any form but the bare key with a value, after a space or a tab and with a comment dropped, of `[]`, `{}`, `""`, `null`, `~` or nothing, and no lines under it. A comment after `""` is not dropped, so it verifies.
- An entry read as the key `*` (an alias, a flow collection, or an explicit key that is no scalar or a compact mapping) or `<<` (a merge) is both locked and verified.
- A block whose first line that is neither blank nor a comment opens a flow mapping (`{`, after any spaces, tabs, U+FEFF, tag or anchor) is both locked and verified: its keys are on lines the codec does not read as entries.
- A block with a line that holds a CR not at its end, or NEL, LS or PS, is both locked and verified: a YAML reader may break the line there and find a key the codec reads as part of a value (`title: x<CR>locked: true`); the codec writes none of them raw (section 6).
- An opaque line that reads as a `locked`, `verified`, alias or merge key once its leading spaces, tabs and U+FEFF are dropped protects as that key, as does one that starts with `*` (`*k.x: true`): a line indented less than a block indented whole, or with a tab (which YAML refuses) or U+FEFF (which a reader of the frontmatter alone drops) before its key. The person wrote a mark.
- A key line or opaque line but the bare one protects as `locked` or `verified`, whatever its value, when the text before its first colon (inside quotes too) trims to that word by Unicode white space, U+FEFF, `"` and `'` at both ends (`locked:false`, `locked :true`, `'locked ': true`, `'verified: a': x`, U+00A0 before `locked`), or, white space trimmed, is one double-quoted string whose escapes give such a text (U+00A0, then `"lo\x63ked": x`); so does an entry whose key trims so (`"locked\_": x`). v1 read keys so.
- A key line or opaque line that, after white space and U+FEFF, starts with a quote not closed on it (`"verified: me`), or whose double-quoted key holds an escape YAML 1.2 lacks or a `\x`, `\u` or `\U` that is no character, is both: another reader may close the quote anywhere or read the escape as anything.
- An empty bare `verified` verifies when the next line not blank starts with a tab, which v1 read as a line under it.
- A key under another (`note:` then `  locked: true`) is not a top-level key and protects nothing; nor do `Locked`, `lockedness` or a comment that says `locked: true`.

The codec reports a verified mark and refuses nothing for it. What a verified mark forbids depends on who asks, so the store core enforces it (ST-05).

## 5. Values

`Value(key)` decodes one entry in a written form of the format's section 4, and never guesses:

- a bare `key: value` whose value is a plain scalar (a lower-case letter, then `a-z0-9_.-`, not one of `y n yes no on off true false null`): the string;
- a bare `key: value` whose value is JSON with one space after each `:` and `,` outside strings and no other space: the decoded value, numbers as `json.Number`;
- a bare `key:` then one or more lines of two spaces, `- ` and such a JSON object: a `[]any` of `map[string]any`.

Otherwise: `ErrNoKey` when no entry has the key, `ErrRepeated` when more than one does, `ErrNotWritten` for any other form (a quoted key, a comment after the value, a block scalar, a flow mapping that is not JSON, a value in single quotes, a list of strings in block form). Each call decodes afresh: the caller owns what it gets.

## 6. The written form

- A string in the plain alphabet that is not a YAML word is written plain: `state: active`.
- Anything else is marshalled by `encoding/json` with HTML escaping off (so `<`, `>` and `&` are kept; struct fields keep their order; map keys are sorted), then spaced: one space after each `:` and `,` outside strings. `json.RawMessage` is compacted first.
- Each character YAML does not allow in a document, which `encoding/json` writes as it is, is escaped as `\uXXXX`: DEL, the C1 controls U+0080 to U+009F (NEL among them, at which a YAML reader may break a line), U+FFFE and U+FFFF. Outside strings JSON holds none of them, and inside one the escape is the same value. JSON escapes the C0 controls, LS and PS itself.
- A string, at any depth or as a map key, that is not valid UTF-8 is refused with `ErrNotWritten`: `encoding/json` would write U+FFFD in its place, and `Value` would not give back what was set. A `json.RawMessage` that holds the escape `�` is refused with it.
- A top-level value that is not a string, a list or an object (a number, a bool, null, a nil slice) is refused with `ErrNotWritten`, as is a value `encoding/json` refuses.
- `SetBlock` writes `key:` and then `  - {…}`, one object a line; it takes any value that marshals to a JSON list of one or more objects, and refuses anything else.
- A line the codec writes ends in LF, in a CR LF document too: LF is the format's line end, so a written line is in a written form whatever its neighbours. The first edit of a hand-written CR LF file leaves its line ends mixed; the store core may write the file back in LF.
- An error's text never holds the caller's key or value, nor `encoding/json`'s error, which may quote them: a key or value may be text that must not reach a log.

So what the codec writes is in a written form by construction, and `Value` reads it back.

## 7. Edits

- **Keys.** `Set`, `SetBlock` and `Delete` take a key of `[a-z][a-z0-9_]*` that is not a YAML word. `Set` and `SetBlock` replace the entry for the key in whatever form it was written (quoted, explicit, a block scalar, a block list) with the written lines, where its first line was; every line of the old entry goes, and opaque lines between its lines stay. A key that is not there goes right after the last line of the last entry, before any trailing comments or blank lines; in a block with no entry, before the closing `---`. `Delete` removes every line of the entry.
- **Body.** `SetPreamble` replaces the preamble's lines; `SetSection` replaces the lines under the heading, or adds `## heading` LF and the text at the end; `RemoveSection` removes the heading line and the lines under it. A heading is matched on its exact text, as the profile checker matches it. `SetSection` refuses with `ErrNotWritten` when another heading shows as the same once spaces and tabs around it and a closing run of `#` are dropped (`## Evidence ` or `## Evidence ##` for `Evidence`), as it would add a second that looks the same, and a new heading not in that shown form. The caller's text is written as given. Where the document ended without a newline and something now follows, the line it ended on gets one; a caller's text that would be followed by more and does not end in a newline is refused.
- **Only what it names.** An edit changes only the lines of the entry or section it names. Every edit is checked by reading its result again before it is kept: the result must be readable, every other frontmatter line must read as before (the same bytes, the same kind of line), and the edited entry must read back as one entry in a written form, and the document must protect as before (but for `verified`, when the edit is of `verified`); a body edit must leave the frontmatter's lines as they were and read back as the preamble and sections it meant. When the check fails, the edit returns `ErrNotWritten` and changes nothing. That refuses, for example, a value whose line would be over 64 KiB, a text that holds a `## ` line, opens a fence or holds a conflict marker, a `Delete` after which a `: value` line would attach to an explicit key above it, and a `Delete` after which an opaque `{…}` line would become the block's first.
- **Refusals.** Every edit of a locked or unreadable document, and any `Set`, `SetBlock` or `Delete` of `locked`, returns `ErrProtected`. The engine never writes `locked` (the format's section 6). A repeated key or heading is `ErrRepeated`; deleting an absent key or removing an absent section is `ErrNoKey`. A frontmatter indented as a whole is refused with `ErrNotWritten`, as a line at column 0 would not join it. A key outside the grammar above is `ErrNotWritten`.
- **Values that run on, and anchors.** A YAML reader continues a quoted scalar or a flow collection onto the next lines, even at column 0, where the codec reads each line on its own. So every key edit of a frontmatter with an entry whose value opens a quoted scalar or a flow collection that does not close on the entry's lines (`note: "a` then `status: draft"`) is refused with `ErrNotWritten`: a new key could land inside the string, or words inside it become a key. A `Set` or `Delete` whose removed lines define an anchor (`&a` where a node starts) while another frontmatter line holds an alias (`*a`) is refused with `ErrNotWritten` too: a YAML reader refuses an alias whose anchor is gone. Either check may refuse more than it must, as an `&` or `*` in a quoted string counts; never less.

## 8. Bounded read

`ReadFrom(r, max)` reads at most `max` bytes and one. More than `max` is `ErrTooLarge`; a read error is returned as it came; otherwise it is `Read` of what it read. A `max` below zero is zero. The store core passes the class's cap from the format's section 2.

## 9. Invariants and the tests that hold them

| Invariant | Test |
| --- | --- |
| `Write(Read(b)) == b` for every hand edit of the corpus (`testdata/corpus`) | `TestRoundTripCorpus`, `TestCorpusIsListed` |
| `Write(Read(b)) == b` for any bytes; nothing panics; a refused edit changes nothing; an edit that succeeds reads back | `FuzzRoundTrip` (its seeds run in `go test`) |
| `locked: true  # x` is locked | `TestLockedWithAComment` |
| Each corpus case protects as listed | `TestCorpusProtection` |
| A generated value or key form the codec cannot read protects; no misread | `TestProtectionProperty`, `TestProtectionBeyondProfile` |
| `Protection()` equals the profile checker's `Report.Protected` on the fixture store, each corpus case in a copy of it, generated forms and section 10's cases | `TestProtectionAgreesWithProfile`, `TestProtectionProperty`, `TestProtectionBeyondProfile` |
| The read methods: each entry's line, form and value as `Value` gives it, the opaque lines, the sections, the text reasons | `TestEntries`, `TestEntriesAgreeWithValue`, `TestOpaque`, `TestSectionsInOrder`, `TestUnreadableText` |
| After one edit of a readable, unlocked document, every byte outside the edited entry or section is unchanged, and `Read` gives back what was set | `TestEditProperty`, `TestSetKeepsOtherLines` |
| Setting each key an item may hold leaves no `written-form` or `frontmatter` fault | `TestSetEachItemKey` |
| The written form; every character written is one YAML allows | `TestWrittenForm`, `TestEditProperty` |
| Values and their errors | `TestValue` |
| Each refusal of section 7, and the refused document still writes its input | `TestRefusals`, `TestOtherRefusals`, `TestCorpusIsListed` |
| No error text holds the caller's key or value | `TestRefusalsNameNoInput` |
| Sections and fences | `TestSections` |
| The unreadable reasons | `TestUnreadableReasons` |
| The bounded read | `TestReadFrom` |
| A key behind Unicode white space, quotes or escapes protects as its mark, an unclosed quote or unknown escape as both; no near miss protects | `TestProtectionKeyForms` |
| Each escape of YAML 1.2 reads as its character | `TestYAMLEscapes` |
| No error's text, and no line of the package, names the library v1 was built on; the test lists every exported error | `TestNoLibraryName` |
| This spec is at most 24 KB and names only tests that exist | `TestSpec` |

## 10. One reader

`internal/store/profile` checks a store against the format and reads every document through the codec: `Unreadable` and `UnreadableText` for what cannot be read, `Entries` and `Opaque` for each frontmatter line and each value's written form, `Protection` for its report's `Protected`, `Preamble` and `Sections` for the body. It has no reader of its own (its test TestReadsThroughTheCodec fails on one), so what the codec reads, the checker reports.
A file at a path the layout rejects (a fault of rule `path`, or of its class's rule such as `item-file`) is reported by that fault and not read, so `Report.Protected` does not list it, locked or not.

The tests that run the checker are in `protection_test.go` (package `codec_test`). They hold the checker's protection equal to the codec's: `TestProtectionAgreesWithProfile` on the fixture store and on each corpus case placed in a copy of it, and `TestProtectionProperty` and `TestProtectionBeyondProfile` at an item and at a session, a project record and a disagreement, where none may be carried, each listed by the hand-edit record and not. And what `Set` and `SetBlock` write passes its `written-form` and `frontmatter` checks (`TestSetEachItemKey`).

The checker's own reader, before it read through the codec, saw no protection where the codec saw one in nine cases, each a document of `TestProtectionBeyondProfile`, which fails when the checker is given that reader again: Unicode white space around a value (`locked: <no-break space>false`); `locked:false`, `verified:[]`; a block that is one flow mapping; a lone CR, NEL, LS or PS; an opaque line that reads as a protection key; `*k: true`, `*k.x: true`; `? note: x`, `? locked : false`; `[locked]: false`; a tab or U+FEFF before a key.

The checker reads sections as the codec does: a `## ` line inside fenced code starts none, and a `# ` line ends none (its test TestSectionsAsTheCodecReadsThem). It reads `title:{x: y}` as YAML does, the key `title:{x`.

## 11. Not here

- Files, locks, durability, compare-and-swap and the hand-edit record: the store core (ST-05).
- What a verified mark forbids, and who may write `verified`: the store core.
- Escaping model text before it is set: the caller's, through the escaper the store core applies.
- Merging two versions of a document: later, with sync.

## 12. v1 pages

`ReadV1(b) (V1Page, error)` reads a page of a v1 wiki through `Read`, as v1 read it, for the migration (ST-07) and frozen system pages (DV-35). `V1Page` names its fields as v1's page model did (`Title`, `Sources` and so on), with `Extra` (every other key but `resource`, `tags`, `governance` and `code_refs`, unused), `Body` (the bytes after the closing line), and `Doc`. Protection is `Doc.Protection()`; `Extra["locked"]` is never read as a lock.

It reads what v1 wrote (flow mappings with plain values, block lists of mappings, flow lists, JSON, plain scalars) as v1 did, white space trimmed and numbers as `float64`. Where v1 misread a hand edit it reads as YAML: a comment after a value is dropped, a single-quoted value unquoted, a comment line is no key, item or line of a value. It refuses what it would guess at, returning only the Doc: `ErrUnreadable` (section 3's reasons, which v1 read past), `ErrRepeated`, and `ErrNotWritten` where YAML reads otherwise or not at all: a block scalar; a tag, anchor or alias; a scalar below its key; a value that runs on, as a flow mapping cut by a comment; text after a quoted value, item or collection; a collection closed by the other bracket; a quote inside a plain value (`[it's, b]`); a comment after a plain value holding `: `; an escape YAML does not know, or `\_`, `\L`, `\P`, which YAML reads as U+00A0, U+2028 and U+2029 and v1 kept as written; a `#` straight after a colon; a list item with no space after its dash (`-x`), or only a comment; a key read as `*` or `<<`; a flow-mapping block; a line that is no key (`title:x`, a tab, less indented than the block); a key v1 cut otherwise (a tag or anchor before it, a plain key holding a colon or a quote or with Unicode white space at an end, `title:: x`, a quoted key with an escape, white space at an end inside its quotes, or a colon after `verified`); and text in `generated`, `verified` or `sources` that is no pair (`note #1: x`, `by:a`). An error names only keys v1 wrote.

Five differences protect less: an empty `verified` (`{}`, `{} # x`, `[] # x`, `~ # x`, or only a comment), which v1 read as verified, is no verified mark (the format's section 6); the oracle's `protectsLess` lists them. ReadV1 refuses the keys v1 cut otherwise, and `Protection` reads each as the mark v1 read (section 4), an unclosed quote as both; ST-07 decides what such a page keeps. TestProtectionCoversV1 in `v1oracle` reads 10,000 seeded pages of such key lines both ways: no page v1 read as locked or verified is neither that nor unreadable, but for a bare `verified` in an empty form.

| Invariant | Test |
| --- | --- |
| A fixture page of each type v1 writes reads as its golden | `TestV1Goldens` |
| Each form v1 read, in `forms/`, reads as its golden | `TestReadV1Forms` |
| Each difference, and the codec's answer | `TestV1Differences`, `TestReadV1Refusals` |
| No panic; a refusal holds only the Doc | `FuzzReadV1` |

`v1oracle`'s tests hold the goldens and differences to v1's own reader while it is in `go.mod`, with 10,000 seeded generated pages; ST-13 deletes it.
