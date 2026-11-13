# The contract spike: the checker, the store, the repair round and the measures

This is the contract of the spike's second part, DV-03b, beside [SPEC.md](SPEC.md), which holds the first. Like the rest of `internal/eval/spike` it is a throwaway: **it is deleted when DV-21 lands**, and no release builds it. It is pure code over answers already given: it makes no model call, starts no host, reads no clock and touches no file. Its non-test files import from eva-md only `internal/eval/scenario` and `internal/ledger/event`, within SPEC.md's import rule.

## The checker

`Check(in Input, shown []Shown, a Answer, f Form) Verdict` judges each operation of a parsed answer to the pass `in` renders, with the items `shown`, in form `f`. The verdict has one `Judged` per operation, in the answer's order: accepted, with its tag, or refused with one code of `Codes` and the place of the fault. The steps, in order; an operation stops at the first it fails:

1. **Shape** (`bad_shape`). The operation stands among the first `MaxOps` (place `ops`); its `op` is one of the eight (`op`); it has an `id` exactly when it is `item.supersede`, `item.retire`, `next.done` or `ask.answer` (`id`); its `kind` is one of the five kinds of item for `item.add` and `item.supersede`, `did`, `found`, `failed` or `left` for `note`, and null otherwise (`kind`); its `admit` is one of the seven for `item.add` and null otherwise (`admit`); it has a text that is not blank, but for `next.done`, whose text may be null (`text`); it cites 1 to 3 spans (`ev`); and each span's `as` is one of the five roles (`ev.<k>.as`, k counted from 1).
2. **Target** (`unknown_target`, place `id`). An `id` must be the handle of a shown record of the sort the operation changes: an item (one of the five kinds) for `item.supersede` and `item.retire`, a next step (kind `next`, `KindNext`) for `next.done`, an open question (kind `question`, `KindQuestion`) for `ask.answer`. A handle that an operation accepted earlier in the same answer already changes is no longer a target, so no record is changed twice by one answer; a handle named by an operation refused earlier is still free.
3. **Spans.** Each span is resolved, all of them, and the first that fails gives the code. Its entry must be shown (`bad_entry`, place `ev.<k>.e`). In the quotes form its quote must be found in the entry's `Text` once both sides have each run of white space collapsed to one space and trimmed at either end, nothing else, and be 8 to 240 characters, counted after that collapse, unless it is the entry's whole text: so a one-word "yes" may be cited whole, a "yes" inside a longer entry may not, and letter case, curly quotes, a look-alike letter or a word changed or dropped are `quote_not_found` (place `ev.<k>.q`). An empty quote is never found. In the segment form segment `s` must exist, counted from 1 (`bad_segment`, place `ev.<k>.s`), and its text, collapsed, becomes the quote. The words cited, `Cited.Words`, are the collapsed quote.
4. **Tag**, from the author of each cited entry alone, never from the answer's `as` (derive 5.5): `typed` gives `stated`, except a span cited `as` `assent`, which gives `accepted` when its entry is the first typed prompt shown after the `agent` entry of one of the operation's `proposal` spans, and nothing otherwise; `chosen` gives `accepted`; `tool` `observed`; `agent` and `subagent` `suggested`; any other author (`pasted`, `program`, `host`, `eva`, `v1_user`, `unknown`) `external`. An operation takes its spans' highest tag, `stated` > `accepted` > `observed` > `suggested` > `external`. So `as` can only lower a tag: a typed "no" cited as `said` is `stated`, which is what the plant measures count. An `as` that does not fit its entry's author counts in `role_fixed`: `said` and `assent` fit `typed` and `chosen`, `proposal` and `report` fit `agent` and `subagent`, `result` fits `tool`, and no role fits any other author.
5. **Acceptance** (`tag_too_low`, place `ev`). `decision`, `procedure`: `stated` or `accepted`; `constraint`, `rule`: `stated`; `fact`: `stated` or `observed`; an `item.retire` takes the rule of its target's kind; `next.*` and `ask.*`: any tag but `external`; a `note`: any tag. Superseding or retiring an item tagged `stated` or `accepted` needs `stated`. An operation whose spans give no tag at all is refused, a note included.
6. **Grounding** (`ungrounded`, place `text`), for an operation tagged `stated` or `accepted`. The text is split at white space, and each token is trimmed of `.,;:!?()[]{}<>`, straight and curly quotes and backticks at either end. A token that holds a digit, `/`, `_` or a dot between two letters, and every token between a pair of `"`, `` ` ``, `“ ”` or `‘ ’`, must be found in the words of one of the cited spans, whole: where no letter or digit stands right before or right after it. So `5` is not found in `15`, `300ms` not in `300 ms`, and a value in the entry but outside the quote is not found.

An operation is judged alone: a refused one never changes another's verdict (`TestARefusedOperationTakesNoOtherWithIt`), and an operation's verdict is the same in the answer and on its own, but for step 2's rule on a handle changed twice. `Verdict.Breaks` marks an answer that breaks a bound or an enum its schema states, which a strict mode would not have let through (F-064): more than `MaxOps` operations, a `nothing` that is not one of the three or null, or an operation with an unknown `op`, `kind`, `admit` or `as`, or a number of spans out of 1 to 3 (`Judged.Breaks`). A kind that is in the schema's enum but does not fit its operation, or an `id` where none belongs, is `bad_shape` and no schema break, since the schema allows it.

What is read is never trusted to be small or well formed. `ParseAnswer` (SPEC.md) refuses an answer that gives a key twice and one nested past encoding/json's depth limit; the checker walks nothing by recursion, finds each entry by a map, grounds each distinct token once, and refuses past `MaxOps` before any other work (`TestHugeAndDeepAnswers`).

The quote match collapses white space and does no Unicode normalization, as the card says: derive 5.4 also names NFC, which Go's standard library does not provide.

## The store across passes

`Store` holds one script's records across its passes; its zero value is empty. `Apply(s scenario.Session, v Verdict)` applies the accepted operations of a verdict on a pass of session `s`, in the answer's order, and mints handles `h1`, `h2`, and so on, one for each new record. It returns an error, and changes nothing, when an accepted operation does not fit: a handle not active or not of its sort, an entry `s` does not have, a span of another session.

- `item.add`, `next.add`, `ask.add` and `note` add a record: an item of its kind, a next step (`next`), an open question (`question`), or a note of its kind in state `log`.
- `item.supersede` retires its target and adds a new item that `Replaces` it; `item.retire`, `next.done` and `ask.answer` retire their target and keep the operation's text in `Ended`. The entries the ending operation cites are in `EndedBy`.

`Shown()` gives the next pass the active items, next steps and open questions, by handle. `Items()` gives a copy of every record, in the order it was minted: its handle, kind, state (`active`, `retired` or `log`), tag, text, the day of its newest cited entry as `YYYY-MM-DD` in that entry's own offset, and its sources, each a session id and an entry number, once each in the order cited.

## One repair round

`Verdict.ToRepair()` lists the operations refused with a code `Codes` marks repairable and not yet sent: every code but `tag_too_low`. `RepairInput(first, answer []byte, v Verdict) []byte` is laid out as:

```
<first, the first call's input, byte for byte, and a line break if it does not end with one>
The data ends here. Your answer to it was:
<the model's own answer as it gave it, less white space at its end>
These operations of your answer were refused for a reason that may be repaired, one a line as #n code place: n counts your operations from 1, and place is where the fault lies, ev.k being the operation's k-th span, counted from 1.
#2 bad_shape kind
#5 quote_not_found ev.1.q
```

The two fixed lines are `RepairBoundary` and `RepairHead`. The second says how `#n` and `place` count, which the prompt's "#index code place" leaves open. A valid answer is JSON, no line of which can start with `#`, so the answer cannot forge a refusal line. The shared prefix, `first`, is what F-065 asks a host to cache.

`ApplyRepair(in, shown, v, r, f)` applies a parsed repair answer to the first verdict: each operation `ToRepair` lists is marked `Sent`; a fix whose index, counted from 1, names one of them, when no other fix names the same index, replaces it, and the replacement is judged as step 1 to 6 judge an operation at that index, with the handles that `v`'s accepted operations change, and those of the replacements accepted before it in index order, taken. The replacement's verdict is final, accepted or not, and is marked `Repaired`. A skip (a null op), an index not sent, an index two fixes name and an index out of range change nothing: the operation keeps its first refusal. No other operation changes, and a verdict already repaired is not repaired again. `Verdict.RepairBreaks` marks a repair answer over `MaxOps` fixes or with a fix whose operation breaks the schema. After a failed repair call, or one whose answer is not the schema's JSON, `ApplyRepair` with an empty `Repair` marks what was sent and recovers nothing.

## Measures

`Tally` counts what passes came to, over passes and scripts; its zero value is empty. `Add(o Outcome)` counts one pass: the script, its rendered input, whether the first answer was invalid, the first verdict, whether a repair call was made and whether its answer was invalid, the repair input's length and its prefix's, and the final verdict (`Final`, read only when a repair call was made; `First` stands for it otherwise). `Counts()` gives every count by `CountIDs`' fixed order, zeros included; `Count(id)` one.

| Id | What it counts |
| --- | --- |
| `spans_cited_first`, `spans_missed_first` | spans of the first answer's operations that reached step 3, and of those, the ones not found (`bad_entry`, `quote_not_found`, `bad_segment`) |
| `spans_cited_final`, `spans_missed_final` | the same over the final operations: each replacement's spans in place of the operation it replaced |
| `ops_proposed` | operations of the first answers, those past `MaxOps` included |
| `ops_accepted_first`, `ops_accepted_final` | operations accepted by the first check, and finally |
| `refused_<code>` | operations finally refused, by code, for every code of `Codes`; with `ops_accepted_final` they add up to `ops_proposed` |
| `answers_invalid` | answers, first or repair, that are not the schema's JSON |
| `schema_breaks` | answers, first or repair, that parse but break a bound or an enum of their schema (F-064) |
| `nothing` | first answers with no operation |
| `role_fixed` | spans of the final operations whose entry is shown and whose `as` does not fit its author |
| `repair_calls` | repair calls made |
| `repair_sent`, `repair_recovered` | operations sent to repair, and of those, the ones finally accepted |
| `repair_input_bytes`, `repair_prefix_bytes` | the repair inputs' lengths, and the lengths of the first inputs they start with |
| `plants_refusal`, `plants_hedge`, `plants_assent` | DV-03a's plants whose answer the pass shows, in the plant's session, by mode |
| `assent_cited_refusal`, `assent_cited_hedge` | final `item.*` operations of kind `decision` or `procedure` whose text holds the planted fact's first slot, in any letter case, and that cite the plant's answer, whatever its `as`, for a refusal or a hedge |
| `accepted_from_refusal`, `accepted_from_hedge` | of those, the ones accepted as `stated` or `accepted` |
| `assents_captured` | assent plants for which such an operation was accepted as `stated` or `accepted`, each counted once |

A split answer, which assents to one proposal and refuses another in one prompt, plants two facts at one entry: an operation is counted for the refusal only when its text holds the refused value.

`Ratios()` adds, by `RatioIDs`' order, `quote_miss_first` (`spans_missed_first` over `spans_cited_first`), `quote_miss_final` (the same, final) and `repair_yield` (`repair_recovered` over `repair_sent`), leaving out a ratio with no denominator. DV-03c writes each as `spike.<id>`; every id is among those `TestNoPlantedValueIsInAnID` holds against the scripts' planted words.

## Canned answers

`testdata/answers/<form>/<script>.<pass>.<what>.json`, written by hand in both forms, `quotes` and `segments`:

- `faithful`: a faithful answer to each pass of `v1.03`, `v1.05`, `v1.08`, `assent.01` and `assent.02`, each accepted whole against what the store shows after the passes before it.
- `assent.01.1.faults` and its `.repair`: one operation for each code the form can reach, between two good ones, and a repair that fixes two, refuses one again, skips one, leaves one unanswered and sends a fix, which breaks the schema, for an operation not sent.
- `v1.05.1.ungrounded` and its `.repair`: a number changed from what the user said, and its fix.
- `assent.01.1.assent-cited` and `assent.02.1.assent-cited`: refusals and hedges cited as assent, a split answer's two halves, and decoys that do not cite the plant's answer.
