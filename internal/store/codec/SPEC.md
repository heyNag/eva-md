# The store codec

`internal/store/codec` reads and writes the bytes of one store document: an item, a workstream, a disagreement, a session file, a project record or a frozen system page, as [the store's written format](../../../docs/spec/store-format.md) describes them (cited below as "the format", by section).
It is the one reader and writer of frontmatter that the store core (ST-05), the migration (ST-07) and system pages (DV-35) build on. It keeps every line a person wrote, reads a lock in every form YAML allows as a lock, and writes only the format's written forms.

It works on bytes. It opens no file, takes no lock, has no clock and makes no network call. It holds no state between calls. Nothing outside the package calls it yet.

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
```

The last four are for a reader that checks a document rather than edits it, the profile checker (section 10). An `Entry` is a key as section 1 reads it (`*` and `<<` as section 4 names them); its `Line`, the opening `---` being line 1; whether it is `Written`, and then its `Value` as `Value` decodes it; its `Scalar`, the value on the key's line with spaces and tabs around it, its quotes and a comment after a plain value dropped; and its `Text`, what follows the colon, then each line under the key after an LF.
`Opaque` counts lines alike. A `Section` is a heading and the text `Section` gives for it. A repeated key or heading is listed each time. An unreadable document has no entries, opaque lines or sections. Each call decodes afresh.

Errors: `ErrProtected`, `ErrNoKey` (no such key or section), `ErrRepeated` (the key or section is there more than once), `ErrNotWritten` (a value in a form the codec does not decode, or an edit it cannot write in a form it reads back), `ErrTooLarge`. An edit that returns an error changes nothing. A refusal's text may add a reason after the sentinel; callers test with `errors.Is`.

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

- **Locked** when there is a `locked` entry in any form but the bare key with one of the values `false`, `False` or `FALSE`, unquoted, after a space or a tab, alone on its line. Spaces and tabs around the value and a comment after it (a space, then `#`) are allowed; no other white space is, as YAML knows no other (a no-break space makes the value a string). Anything else locks: an empty value, a quoted `"false"`, `no`, `false#x`, a comment after a tab, `locked:false` (no key to YAML), a value on the next line, a list. Two `locked` entries lock, whatever their values.
- **Verified** when there is a `verified` entry in any form but the bare key with a value, after a space or a tab and with a comment dropped, of `[]`, `{}`, `""`, `null`, `~` or nothing, and no lines under it. A comment after `""` is not dropped, so it verifies.
- An entry read as the key `*` (an alias, a flow collection, or an explicit key that is no scalar or a compact mapping) or `<<` (a merge) is both locked and verified.
- A block whose first line that is neither blank nor a comment opens a flow mapping (`{`, after any spaces, tabs, U+FEFF, tag or anchor) is both locked and verified: its keys are on lines the codec does not read as entries.
- A block with a line that holds a CR not at its end, or NEL, LS or PS, is both locked and verified: a YAML reader may break the line there and find a key the codec reads as part of a value (`title: x<CR>locked: true`). What the codec writes holds none of them raw (section 6).
- An opaque line that reads as a `locked`, `verified`, alias or merge key once its leading spaces, tabs and U+FEFF are dropped protects as that key would in any form but the bare one; so does an opaque line that starts with `*` (`*k.x: true`, whose alias name another reader may take to the space). Such a line is one indented less than a block indented as a whole, or one with a tab before its key, which YAML refuses, or one with U+FEFF before it, which a reader given the frontmatter alone drops where its stream starts. The person wrote a mark.
- A key line or opaque line that is `locked:` or `verified:` with no space after the colon (`locked:false`, `verified:{locked: true}`) protects as that key, though YAML reads another key or none.
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
| `Write(Read(b)) == b` for every hand edit of the corpus: comment lines and trailing comments, block scalars, flow mappings, repeated keys, quoted keys, CR LF, no final newline, a 100 KiB line, a byte-order mark, conflict markers, an unclosed frontmatter, `---` and `## ` in fenced code, a quoted value that runs on | `TestRoundTripCorpus`, `TestCorpusIsListed` |
| `Write(Read(b)) == b` for any bytes; nothing panics; a refused edit changes nothing; an edit that succeeds reads back | `FuzzRoundTrip` (its seeds run in `go test`) |
| `locked: true  # x` is locked | `TestLockedWithAComment` |
| Each corpus case protects as listed | `TestCorpusProtection` |
| A generated value or key form the codec cannot read protects; no misread | `TestProtectionProperty`, `TestProtectionBeyondProfile` |
| `Protection()` equals the profile checker's `Report.Protected`, on every document of the fixture store, on each corpus case placed in a copy of it, on generated forms and on each case of section 10 | `TestProtectionAgreesWithProfile`, `TestProtectionProperty`, `TestProtectionBeyondProfile` |
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
| No error text, and no line of the package, names the library v1 was built on | `TestNoLibraryName` |
| This spec is at most 24 KB and names only tests that exist | `TestSpec` |

## 10. One reader

`internal/store/profile` checks a store against the format and reads every document through the codec: `Unreadable` and `UnreadableText` for what cannot be read, `Entries` and `Opaque` for each frontmatter line and each value's written form, `Protection` for its report's `Protected`, `Preamble` and `Sections` for the body. It has no reader of its own, and its test TestReadsThroughTheCodec fails on one. So the two cannot drift: what the codec reads, the checker reports.
A file at a path the layout rejects (a fault of rule `path`, or of its class's rule such as `item-file`) is reported by that fault and not read, so `Report.Protected` does not list it, locked or not.

The tests that run the checker are in `protection_test.go`, in package `codec_test`, as the checker imports the codec. They hold the checker's protection equal to the codec's: `TestProtectionAgreesWithProfile`, `TestProtectionProperty` and `TestProtectionBeyondProfile`. And what `Set` and `SetBlock` write passes its `written-form` and `frontmatter` checks (`TestSetEachItemKey`).

The checker's own reader, before it read through the codec, saw no protection where the codec saw one, in nine cases. `TestProtectionBeyondProfile` holds a document of each, and fails when the checker is given that reader again:

1. white space other than a space or a tab around a `locked` or `verified` value (it trimmed any Unicode space, so `locked: <no-break space>false` unlocked);
2. `locked:false` and `verified:[]`, with no space after the colon;
3. a block that is one flow mapping, `{type: fact, locked: true}`;
4. a frontmatter line that holds a lone CR, NEL, LS or PS;
5. an opaque line that reads as a protection key;
6. an alias with its colon right after its name, `*k: true`, or with a name past libyaml's alphabet, `*k.x: true`;
7. an explicit key whose node is a compact mapping, `? note: x` or `? locked : false`;
8. a flow collection as an implicit key, `[locked]: false`;
9. a line with a tab or U+FEFF before its key, `\tlocked: true`.

The checker reads sections as the codec does: a `## ` line inside fenced code starts none, and a `# ` line ends none (its test TestSectionsAsTheCodecReadsThem). It reads `title:{x: y}` as YAML does, the key `title:{x`.

## 11. Not here

- Files, locks, durability, compare-and-swap and the hand-edit record: the store core (ST-05).
- What a verified mark forbids, and who may write `verified`: the store core.
- v1's pages read through the codec, as v1 read them: ST-03b.
- Escaping model text before it is set: the caller's, through the escaper the store core applies.
- Merging two versions of a document: later, with sync.
