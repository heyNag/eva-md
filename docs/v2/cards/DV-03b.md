# DV-03b: the contract spike's checker, repair round and measures

**Plan:** section 8, Stage 0, row DV-03; section 6.3, design 3 and 4; decision 10, at its recommendation; row DV-21 (a whole entry however short); section 13.2. **Design:** `docs/v2/working-papers/design/derive.md` 5.4 to 5.8; `docs/v2/host-facts.md` F-064, F-065. **Size:** M. **Reviewers:** one. **Depends on:** [DV-03a](DV-03a.md). **Part of:** DV-03 (a, b, [c](DV-03c.md), [d](DV-03d.md)).

## Why

The spike's numbers are only as good as its checker, which applies the plan's rules by code alone: a quote is found word for word or not; who said a span comes from the record's author, not the model; a refused operation never takes another with it; one repair call may fix what is repairable. It counts what the row asks. It changes nothing v1 does: no switch.

## What to build

Files in `internal/eval/spike`, under DV-03a's import rule; pure, no clock.

1. **The checker.** `Check(in Input, shown []Shown, a Answer, f Form) Verdict`: each op accepted, with its tag, or refused with one code of `Codes`. Steps:
   - Shape: DV-03a's bounds; an `id` on exactly `item.supersede`, `item.retire`, `next.done` and `ask.answer`; `kind` and `admit` as DV-03a item 3 says: `bad_shape`. An answer over 24 ops, or an op breaking a bound, counts in `schema_breaks` (F-064).
   - Target: an `id` must be shown: `unknown_target`.
   - Spans: the entry must be shown (`bad_entry`). Quotes: found in the entry's `Text` once both sides collapse each run of white space to one space, nothing else, and 8 to 240 characters unless it is the whole text, so a one-word yes may be cited whole: else `quote_not_found`. Segments: it must exist (`bad_segment`); its text becomes the quote.
   - Tag, from the cited entry's author (`derive.md` 5.5): `typed` gives `stated`, except a span cited `as` `assent`, which gives `accepted` when its entry is the first typed prompt after the agent entry of one of the op's `proposal` spans, and nothing otherwise; `chosen` gives `accepted`; `tool` `observed`; `agent` and `subagent` `suggested`; any other `external`. An op takes its spans' highest tag. An `as` that does not fit the author (`said` and `assent` are a user's, `proposal` and `report` the agent's, `result` a tool's) counts `role_fixed`.
   - Acceptance: `decision`, `procedure`: `stated` or `accepted`; `constraint`, `rule`: `stated`; `fact`: `stated` or `observed`; `next.*`, `ask.*`: not `external`; a note: any. Superseding or retiring a `stated` or `accepted` item needs `stated`. Else `tag_too_low`.
   - Grounding, for an op tagged `stated` or `accepted`: each token of `text` holding a digit, `/`, `_`, a dot between letters, or in quotes or backticks, must be in a cited span: `ungrounded`.
2. **The store across passes.** `Store` applies accepted ops in order and mints handles `h1`, `h2`, …; `Shown()` gives the next pass its active items, next steps and open questions; `Items()` gives DV-03c each item's kind, state (active, retired or log), tag, text, the day of its newest cited entry, and its sources (session id, entry number).
3. **One repair round.** `RepairInput(first, answer []byte, v Verdict) []byte`: the first call's input byte for byte, a fixed boundary line, the model's own answer, then `#<index> <code> <place>` for each op refused with a repairable code, `place` a fixed word such as `ev.1.q` or `text`. `ApplyRepair` checks each replacement as in item 1; a skip, a second refusal or an index not sent is final.
4. **Measures.** `Tally`, counts over passes and scripts, by fixed id: `spans_cited` and `spans_missed` (each `_first` and `_final`), `ops_proposed`, `ops_accepted_first`, `ops_accepted_final`, `refused_<code>` (final), `answers_invalid`, `schema_breaks`, `nothing`, `role_fixed`, `repair_calls`, `repair_sent`, `repair_recovered`, `repair_input_bytes`, `repair_prefix_bytes`. For DV-03a's plants: `plants_refusal`, `plants_hedge`, `plants_assent`; `assent_cited_refusal` and `assent_cited_hedge` (`item.*` ops of kind decision or procedure with the fact's first slot in their text, citing a span in the plant's prompt, whatever its `as`); `accepted_from_refusal` and `accepted_from_hedge` (of those, accepted as `stated` or `accepted`); `assents_captured` (assent plants that became an accepted decision or procedure). `Ratios()` adds `quote_miss_first`, `quote_miss_final`, `repair_yield`, leaving out a ratio with no denominator.
5. **Canned answers** in `testdata/answers/`, by hand, both forms: a faithful answer to each pass of three v1 and two `assent` scripts, and the faults the tests need.
6. **`CHECK.md`**, this part's spec: the checker's steps, the repair input's layout, each measure, and that it is deleted when DV-21 lands.

## Owns and may touch

New files only.

```paths
internal/eval/spike/CHECK.md
internal/eval/spike/check.go
internal/eval/spike/check_test.go
internal/eval/spike/store.go
internal/eval/spike/store_test.go
internal/eval/spike/repair.go
internal/eval/spike/repair_test.go
internal/eval/spike/measure.go
internal/eval/spike/measure_test.go
internal/eval/spike/testdata/answers/**
```

## Must not touch

Everything else, DV-03a's files included: if the contract must change, stop and report.

## Accepted when

- `scripts/linux-check.sh` passes, and `go list -deps ./cmd/eva-md | grep -c internal/eval` prints 0.
- Every faithful canned answer is accepted whole, in both forms.
- Each code of `Codes` is reached by a canned answer, and repair is offered exactly for the repairable codes: a test.
- By test: a quote off by a word, or in curly quotes, is `quote_not_found`; a doubled space is not. A whole-entry "yes" after the proposal, cited as `assent`, makes a decision `accepted`; the same yes as the second prompt after it is `tag_too_low`, and a "yes" quoted inside a longer entry `quote_not_found`. A decision citing a pasted entry is `tag_too_low`. A changed number is `ungrounded`, and a repair that fixes it is accepted.
- The repair input starts with the first input byte for byte; `repair_prefix_bytes` is its length: a test.
- Canned answers citing a refusal and a hedge as assent give exact plant counts, and a partial answer counts only its refused half: a test. A hand-built run of three passes gives exact figures for every measure.
- A reviewer's mutations (case folded in the quote match, the tag taken from `as`, the first-prompt rule dropped, repair offered for `tag_too_low`, a refused op taking its neighbour with it) are each caught by a test.

## As merged

Merged from `pkg/dv-03b-r1` on 2026-11-13, after a review, one fix round and a verification that accepted it. The review's two major faults: a number, version or path the model cut short was found inside the longer value the user gave, so a changed number passed as grounded; and an operation past the 24th went to a repair round that could never mend it, which would have pulled down the very measure that says whether a repair round pays. `CHECK.md` in the package is this part's spec.

Accepted as built, beyond the card: an operation needs text that is not blank; a target must be of its operation's sort, and one changed earlier in the same answer is unknown to later operations; an operation whose spans give no tag at all is refused; the repair input says its numbers count from 1; no repair is offered for a fault of the whole answer's shape.

The lead's rulings of 2026-11-13, built in a second round on this card's files and merged the same day from `pkg/dv-03b-r3`, after two fix rounds and a verification that accepted it (the first fix let a dotted version count as a plain number, so `1.2.3` was found in `1.2.3-beta`; a number is now digits with at most one point):

1. **A quote starts and ends at a word boundary.** As merged, `acceptable to ship` is found in `unacceptable to ship`. That changes no tag, but it counts as found a quote that says the opposite, on the measure that decides between quotes and segments.
2. **A number followed by a hyphen or a slash and a letter is that number.** As merged, `30` is not found in `a 30-second timeout`, which a faithful rewording writes all the time. A joiner still makes a value go on when a digit follows it, or when the token is a path or a name.
3. **A span that lies wholly in lines the renderer marks as quoted is not the user's own words**: it is tagged as what the user showed, not what the user said, so pasted text cannot ground a decision as stated (the design's 5.5; the known failure of v1's sixth script). A span that takes in both kinds of line stays the user's.
4. **The counts of schema breaks and of invalid answers are kept apart for first answers and for repair answers**, so DV-03c can read either.

Known and left: quotes are not normalized to NFC, which Go's standard library lacks; the prompt's own example of a wrong operation (a hold-off cited as a yes) is accepted by the checker, which does not judge meaning, and decision 10's plant measures count that mistake; `8080` is not found in `localhost:8080`, nor `80` in `80,443`, the cost of refusing a value cut short. From the second round: a hyphen and an apostrophe are word boundaries, so `blocking for the release` is found in `non-blocking for the release`; a two-part version before a suffix of letters, as `2.0` in `2.0-beta`, is found; and a decision that cites one of the user's own lines beside pasted ones is stated even when every value in it comes from the pasted lines, since the tag is the highest of an operation's spans and a value may be grounded in any of them. DV-03c reads its figures with these in mind, and the fidelity scorer's count of misattributed items is what catches the last. DV-21, which builds the real checker, takes all three up: a quote's edge inside a hyphenated word, and a value grounded only in what the user showed.
