# HF-00b: the recorder keeps the shape of a voice hand-over

**Plan:** section 8, Stage 0 row HF-00 (its fixtures) and "What the host sitting must settle" (the voice call); row LG-13; section 6.1, design 2; decisions 29 and 30. **Design:** [HF-00](HF-00.md) items 2, 5 and 6; `docs/v2/working-papers/field/note-evaluation.md`, "How Codex's voice mode hands work to the agent"; facts F-016, F-141 to F-147; run-book step X-44. **Size:** M. **Reviewers:** two: it decides what a real account's recording keeps. **Depends on:** HF-00, Part 1 merged. It merges before sitting 0: a sitting is not repeated. Nothing in eva-md changes; no switch. **Then:** LG-13b.

## Why

HF-00 keeps a row's free text as its length, a keyed hash and a leading tag's name, and its fixtures replace text by made-up text, so a recorded hand-over shows only that it was one. LG-13b holds the decoder to recorded hand-overs, and EV-14 and DV-03 build spoken scripts on them. They need which line is whose, where the request is, whether the host cut a field, whether a tail-flush source line is there. This keeps the smallest skeleton that shows it, and no character of anyone's words.

## What to build

1. **The form**, in `internal/hostfacts/voice.go`: a function that returns a string's skeleton, or none. A string is in form when, after white space (space, tab, CR, LF), it is exactly what Codex's source writes: `<realtime_delegation>\n`, an optional `  <source>S</source>\n`, `  <input>I</input>\n`, an optional `  <transcript_delta>T</transcript_delta>\n`, `</realtime_delegation>`, then white space only, with no `<` or `>` in S, I or T.
2. **The skeleton** holds only:
   - the white space before and after;
   - which optional lines are there;
   - S when it is `transcript_tail_flush`, else `other` and its byte length;
   - for I and T: the byte length and keyed hash of the field as written (escapes and cut mark included), and whether it begins or ends with the cut mark `…`;
   - for each line of T, in order, split on `\n` after a leading cut mark is set aside: its head when it is `user: ` or `assistant: `, else `other`; the byte length and keyed hash of the rest (the whole line when `other`).

   The keyed hash is HF-00's, so a request equal to a line's rest or to T, or a line repeated in another hand-over or row, shows as equal hashes.
3. **Where it is kept.** Wherever HF-00 keeps a string's keyed hash, beside a payload's string values or in a record row's snapshot, an in-form string also gets its skeleton under the key `voice`, on either host, at any field path. The payload stays whole; its skeleton is kept since `fixtures` runs without the key. A string out of form, or an object key, keeps nothing more than HF-00 keeps.
4. **Fixtures.** `hostrec fixtures` rebuilds a string with a skeleton from the skeleton alone, never from a payload's text: the same white space, lines, listed heads and source value, cut marks and line breaks, and made-up text of each part's byte length, lowercase ASCII letters and single spaces from HF-00's seeded generator. Equal hashes get equal text across the recording: a request equal to a line gets that line's text, one equal to T is written as the rebuilt T, and any other string with a part's hash gets that part's text. Out of form, HF-00's rule holds.
5. **The vocabulary.** `internal/hostfacts/vocabulary.json` holds, as host words from Codex's source approved by this card, adding any HF-00 has not: `realtime_delegation`, `source`, `input`, `transcript_delta`, `transcript_tail_flush`, `user: `, `assistant: ` and `…`.
6. **The repository's fixture test** (HF-00 item 6) also reads every committed fixture wrapper back to its skeleton and rebuilds its parts from the seed, a part equal to an earlier one counting as a repeat; it fails on a wrapper out of form, a head off the list, or any part the generator would not have made.

## Owns and may touch

`internal/hostfacts/voice.go` and `voice_test.go` (new), and the vocabulary. In HF-00's code, only the function that keeps a string's keyed hash, the one that replaces a string in fixtures, the repository's fixture test, and their tests. Those files are not named yet, so the block names the folders: this runs after HF-00 Part 1 merges, never beside its Part 2.

```paths
internal/hostfacts/**
cmd/hostrec/**
```

## Must not touch

In those folders, every other rule: the enum allowlist, the id and number rules, F-016's leading-tag rule, the name placeholders, `setup`, the arms, and every other command. Every other path: `internal/ledger/**` (read `inboxv1.Wrapper` only), `internal/host/capture`, `docs/v2/host-facts.md`, the run-book.

## Accepted when

- `scripts/linux-check.sh` passes; `go list -deps ./cmd/eva-md | grep -c hostfacts` prints 0.
- A table test gives the expected skeleton for wrappers with and without a source line, the tail-flush source, an unlisted source, a cut input, a cut transcript whose first line lost its head, an empty input, a request equal to a line and to T, an unlisted head and escapes; and none for a swapped tag order, an extra tag, text after the closing tag, a missing line break, other indentation, a bare `<`, no closing tag. A test holds the tag equal to `inboxv1.Wrapper`.
- Planted markers: `hostrec hook`, given a made-up Codex session whose payloads and rows (a `response_item` and an `event_msg` user message) hold wrappers with a different marker in the input, in each speaker's lines, in an unlisted head, in an unlisted source value, after a cut mark, and in out-of-form wrappers, one of them an object key, keeps each in-form skeleton on both sides with equal line hashes, and no marker appears in the recording outside the payload that carried it, nor in any fixture.
- From a made-up recording shaped as X-44 (hand-overs for lines 1, 2 while the agent works, 4, 5 with its correction, 6 after the pause phrase), plus a tail-flush row and an empty-input row that no payload matches, `fixtures` writes wrappers in form with the same lines, heads, source values, cut marks and byte lengths, keeping every equality of item 4; `check` passes. One line's recorded text put back fails `check` and the repository test.
- Every word a skeleton keeps is in `vocabulary.json`, by test.
- A reviewer's mutations (an unlisted head or source kept, an out-of-form wrapper given a skeleton, a line's text kept, equal hashes given different text, a length changed) are each caught.
- No new module; only eva's name; no sleep decides an outcome.
