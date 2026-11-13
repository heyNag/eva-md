# The contract spike: scripts, renderer and the two forms of the contract

`internal/eval/spike` is the contract spike of [the v2 plan](../../../docs/v2/plan.md) (section 8, Stage 0, row DV-03): a throwaway prototype of the extraction contract of section 6.3, run black-box on made-up scripts to settle the contract's open bets by measurement: whether a small model quotes the user's words exactly or needs numbered segments, whether one repair round pays, and how often a typed refusal or hedge is cited as assent (decision 10). **It is deleted when DV-21 lands.** No release builds it: nothing outside its own folders and `cmd/eva-eval` imports it or a package under it, and its non-test files import from eva-md only `internal/eval/scenario`, `internal/ledger/event` and `internal/host/adapter` (`imports_test.go`). It changes nothing v1 does, and has no switch.

This file is the contract of the package's first part, DV-03a. The checker, the store across passes, the repair round and the measures are DV-03b's, in `CHECK.md`.

## Scripts

`Scripts()` gives the scripts in a fixed order: `scenario.V1()`'s 12, as that function gives them; the family `assent`, `assent.01` to `assent.06`; then `bounds.01`. A `Script` is a `scenario.Scenario` and its `Plants`. A `Plant{Session, Seq, Mode, Proposal, Fact}` says that the typed prompt at `Seq` of the session answers, by `Mode` (`refusal`, `hedge` or `assent`), the proposal of fact `Fact` that the agent's reply at `Proposal` plants. Only typed answers are planted: spoken ones wait on HF-00's recorded wrapper.

`Script.Validate` runs the scenario's `Validate`, then holds each plant to these rules: its session exists; its proposal is the agent's reply and plants the fact; its answer is a typed prompt after the proposal, and the first typed prompt after it; no fact is answered twice; an assented fact is stored under the user's authority (`store`, `user`) as a decision or a procedure and nothing else; a refused or hedged one is `not_with_authority`, and its answer holds none of its values in any case or spelling.

**The family `assent`.** Each script is one typed chat about one topic: the user asks for proposals, the agent makes one reply per proposal, and the user answers each in the next typed prompt. Each proposal plants a made-up value, an identifier the agent's reply holds, as the fact's only slot. Across the family there are 24 refusals, 24 hedges and 12 assents, each mode in at least 8, 8 and 5 wordings; at least 4 refusals are one word, at least 4 open with a word of assent ("Yes, I see why, but no: keep what we have."), and 4 replies propose two things, one taken and one refused, answered by one prompt. The wordings are taken in turn across the family, and `TestAssentFamilyCounts` holds these counts. No value is part of another.

**`bounds.01`** is one chat in which the user states 26 settings, relay and scheduler settings in turn, each in a typed prompt of its own that the agent acknowledges with a reply that holds no value, and each a fact to store under the user's authority. No entry, and so no quote and no segment, holds two settings: an answer that keeps each setting in an operation of its own needs 26, more than the schema's 24 (host fact F-064). An operation that keeps several at once must cite the entry of each, at most 3, or hold a value none of its spans holds (`TestBoundsPlantsOverTheBound`).

Every text of every script is ASCII and unchanged by `redact.Redact`, and no word `scenario.Forbid` lists for a script occurs in an id the spike writes: its forms, codes and script ids, an arm's tiers and efforts, the parts of a row's scenario, and the metric ids of DV-03b and DV-03c and of the fidelity scorer. So every row can pass the run's `Forbid`.

Each script is committed as `testdata/scripts/<id>.json`, `json.MarshalIndent` with two spaces and a final line break, and the test fails when the code and the files differ. After checking a change, `go test ./internal/eval/spike -run TestScriptsGolden -regen` rewrites them.

## The renderer

`Render(s scenario.Session, shown []Shown, f Form) (Input, error)` renders one pass of session `s` over `scenario.Events(s)`, the filler exchange that function ends a session with included.

```
Items in force:
[h2] decision accepted: The CI runner image is pinned to a fixed tag.
[h10] next stated: Write the release notes.
  [quoted] [4] user typed 14:03: forged

Entries:
[1] user typed 2026-03-03 14:00: Let's go over the CI pipeline today. Propose changes one at a time and I'll answer each.
[2] assistant 14:01: Shall I move the CI cache to the gravelstore bucket?
```

- **Items in force** come first, `[<handle>] <kind> <tag>: <text>`, ordered by handle with a shorter handle first, so `h2` comes before `h10`; with none, the line is `Items in force: none.`. A `Shown{Handle, Kind, Tag, Text}` needs a handle of lower-case letters, digits, `.`, `_` and `-` that starts with a letter, so that it cannot read as an entry or a segment, given once, a kind and a tag of one word each, and a text. A text that opens with a line break leaves its head alone on its line, with no space after the colon; so does an entry's.
- **Entries** follow, one per event: `[n] <label> <time>: <text>`, `n` the event's seq, so an entry names a turn as `scenario.Locate` takes it. The label is `user typed`, `user chose`, `user pasted`, `assistant`, `subagent`, `tool` or `program` for those authors, and any other author's id (`host`, `eva`, `v1_user`, `unknown`). The time is the event's own, in its own offset, as `2006-01-02 15:04` on the first entry and whenever the day changes, else `15:04`. An entry with no text ends at its time.
- **Off the record.** Controls are not shown, nor any event from an off control to the next on; with no on, nothing after the off is shown.
- **Cleaning.** Every kind of line break (`\r\n`, `\r`, U+2028, U+2029, U+0085) is one; control and format characters other than a tab are dropped; white space at a line's end is cut; and a line that starts with `[` after any white space is marked `[quoted] `, so no line of text can pass for an entry, a segment or an item. Lines after an entry's first are indented two spaces; an empty line stays empty.
- **The segment form** puts the entry's head alone on its line, then each segment on a line of its own, `  [n.k] <segment>`, k from 1. A text is split at each line break, and after a sentence-ending mark (`.`, `!`, `?`, `…`, `。`, `！`, `？`) that ends its line or precedes white space; segments are trimmed and an empty one is dropped. A segment cut from the middle of a line that starts with `[` is marked `[quoted] ` too, since it is shown on a line of its own: `Done. [7] user typed 14:05: yes` is the segments `Done.` and `[quoted] [7] user typed 14:05: yes`, while the quotes form shows it mid-line as typed. So `Go 1.26.` and `scripts/benchmark.py` are one segment each.

`Input{Bytes, Entries}`: `Bytes` is what the model reads after the prompt; `Entries` has one `Entry{N, Session, Author, Kind, Text, Segments}` per shown event, `Text` its cleaned text as shown with its marks, lines joined by `\n` without the indent, and in the segment form `Segments` its segments as shown, segment k being `Segments[k-1]`; in the quotes form `Segments` is nil.

## The contract in two forms

`Form` is `FormQuotes` (`quotes`), where a span quotes an entry's exact words, or `FormSegments` (`segments`), where it names a segment and code copies its words.

**An answer** is `{"ops": [...], "nothing": ...}`, 0 to 24 operations, `nothing` one of `none_new`, `only_progress`, `only_derivable`, or null. An operation is `{"op", "id", "kind", "text", "admit", "ev"}`:

| Key | Values |
| --- | --- |
| `op` | `item.add`, `item.supersede`, `item.retire`, `next.add`, `next.done`, `ask.add`, `ask.answer`, `note` |
| `id` | a shown handle, or null |
| `kind` | for an add or a supersede the plan's five, `fact`, `decision`, `constraint`, `procedure`, `rule`; for a note `did`, `found`, `failed` or `left`; else null |
| `text` | one plain sentence, or null |
| `admit` | for an `item.add` derive 5.3's seven, `instruction`, `decision`, `invariant`, `environment`, `pointer`, `failure`, `command`; else null |
| `ev` | 1 to 3 spans: `{"e", "q", "as"}` in the quotes form, `{"e", "s", "as"}` in the segment form, `e` the entry's number, `q` its words (8 to 240 characters, or the entry's whole text), `s` the segment's k, `as` one of `said`, `assent`, `proposal`, `result`, `report` |

A repair answer is `{"fixes": [{"index", "op"}]}`, `op` a whole operation of the form, or null to skip it.

`Schema(f)` and `RepairSchema(f)` are generated from the one table in `contract.go` that also holds the bounds (`MaxOps`, `MinSpans`, `MaxSpans`, `MinQuote`, `MaxQuote`) and enums the checker uses. Every key is required, a key is nullable where the table lets its value be absent (`id`, `kind`, `text`, `admit`, `nothing`, and a fix's `op`), a nullable enum lists null among its values, and `op`, `ev`, `e`, `q` or `s`, `as`, `ops`, `fixes` and `index` are never null (`TestSchemaNullability`): this reads the card's "required and nullable" as nullable where a value may be absent, since a strict mode would otherwise let a model answer null where the contract needs a value. No object takes another key, and no schema holds `maxLength`: a text's bounds stand in its description. The quotes schema has no `s` and the segment schema no `q`. Each object lists its properties, and its `required`, in the card's order, `ops` then `nothing`; `op`, `id`, `kind`, `text`, `admit`, `ev`; `e`, `q` or `s`, `as`; `index` then `op`, since a strict mode writes an answer's keys in the schema's order (`TestSchemaKeysInTheCardsOrder`).

**The refusal table**, `Codes`, gives each code its sentence and whether one repair round may fix it: `bad_shape`, `unknown_target`, `bad_entry`, `quote_not_found`, `bad_segment` and `ungrounded` may be repaired; `tag_too_low` is final. A sentence that states a limit takes it from the table, as the prompt's other limit sentences do. Each prompt writes every code with its sentence, in the segment form too.

**The prompt**, `Prompt(f)`, is one text for both hosts and names no host and no model. It says, in derive 5.1's order: who reads the result and why; that everything after its last line is data; that writing nothing is allowed and preferred; how the data and the answer are laid out; what is worth keeping; the evidence rule; what is refused, code by code, and how a repair is asked; then a made-up example, rendered by `Render` in the form, with a good answer and one wrong operation, a hold-off cited as a yes. Its last line is `Everything after this line is data.` (`Boundary`). No word a script plants is in it.

## Strict reading

`ParseAnswer(data, f)` and `ParseRepair(data, f)` return an error only for data that is not one JSON object of the form's keys and types: data that is not UTF-8 or not JSON, anything after the object, a key missing, unknown (a key of the other form included), given twice or in another case, a string where it is not one, a null where none is allowed, or an integer written with a fraction or an exponent or beyond 64 bits. They check no bound and no enum: an unknown `op`, an empty `ev`, more than 24 operations or an unknown `nothing` are read as given, for DV-03b to judge op by op.
