# LG-13b: the decoder held to the hosts' recorded payloads

**Plan:** section 8, Stage 1 row LG-13 ("every fixture payload decodes to its labelled events"; "no fixture hand-over yields a typed event"), the HF-00 row and "What the host sitting must settle"; section 13.3, point 6. **Design:** `docs/v2/working-papers/design/ledger.md`, row LG-13 (`pv_false_typed` is 0 on the fixtures; an unknown entrypoint yields `unknown` with its text). **Facts:** the core facts F-016 (Claude Code's half), F-142, F-143 and F-144, from Part 1 of the run-book; F-080 and F-145, which Part 1 records at no added minute; F-001, F-018, F-027, F-079 and F-141, by the second field report where Part 1's payloads do not show them (else Part 2: group A for F-079, C for F-027); F-017 (pastes), Part 2's group D. **Size:** M. **Reviewers:** two: the contract, and one who looks for a recorded payload filed under the wrong author. **Depends on:** LG-13a, and HF-00: Part 1's fixtures under `internal/hostfacts/testdata/<host>/<version>/`, the core facts' Results in `docs/v2/host-facts.md`, and a vocabulary that keeps, as host words, the voice wrapper's inner tags, its source value, its `user: ` and `assistant: ` line heads and line breaks, the cut mark, and Claude Code's entrypoint values on the record's rows.

## Why

LG-13a rests on v1's lists and on a reading of Codex's source. The sittings record what both hosts really send for a typed prompt, a host's own prompt, a program's run and a voice call, with every value made up. This part labels each recorded payload by what the run-book step that made it did, and holds the decoder to the labels, so the allowlist and its spec rest on recordings. Like LG-13a it has no caller and changes nothing v1 does.

## What to build

1. **Labels.** `internal/host/capture/testdata/labels/<host>/<version>.json`: for every payload of HF-00's fixtures, the events the step made, each with its kind and its true author (typed, spoken, host, program, subagent, front model), and for a hand-over each line's speaker. A label comes from the run-book's step, as `internal/hostfacts` holds the steps and the voice script; never from the decoder's output.
2. **The gate.** A test decodes every fixture payload with `capture.Decode` and compares it with its label. On Claude Code, `Env.Entrypoint` is the `entrypoint` the fixtures keep on that session's user rows, since the recorder keeps no value of a hook's environment. An event matches when its author is the label's, or is `unknown` for a reason LG-13a names (a Codex prompt outside a hand-over, a user line that repeats the request, a line the cut left short); any other difference fails, as do a payload with no label and a label with no payload. It computes `pv_false_typed`, the events filed `typed` whose label is not typed, and fails unless it is 0; it logs, as counts, the events filed `unknown`.
3. **What the recordings change.** Where a fixture shows a host wrapper, a source value or an entrypoint LG-13a's lists lack, add it to `lists.go` with the fact and the step that show it. Where a fact is refuted, take its fallback: F-079 refuted, Claude Code's prompts are `unknown`; F-016's Claude Code half refuted, Claude Code's prompts are `unknown` in the hook too, and typed waits for LG-21's counterpart row in the record (ledger 3.6); F-142 refuted, the request is `unknown`, never `spoken` or `typed` (decision 29). A change to the voice form follows F-141 and F-143.
4. **Voice.** The Part 1 call decodes as the run-book scripted it: two hand-overs, one made while the agent works, whose events carry that turn's id (F-145); the spoken correction, the spoken yes and the spoken pause phrase, each a line by `spoken`; the request by the author F-142 found; a task voice starts by itself, whose first prompt is a hand-over (F-144). No fixture hand-over yields `typed`.
5. **Pastes.** If group D's fixtures carry the mark (F-017), a prompt that is wholly a host-marked paste is `pasted` and a mixed one stays `typed`; until then this stays waiting, and the spec says so.
6. **The spec.** `SPEC.md`'s entries that wait for the sitting are replaced by each fact's Result and the test that holds it.

## Owns and may touch

The decoder's package, which LG-13a made, with its labels.

```paths
internal/host/capture/**
```

## Must not touch

`internal/hostfacts/**` and its fixtures, which are HF-00's: a fixture that looks wrong is reported to the lead, never edited. `docs/v2/host-facts.md`, whose Results the lead writes. `internal/hook`, `internal/catchup`, `internal/ledger`, `internal/host/adapter`.

## Accepted when

- `scripts/linux-check.sh` passes; the diff names only `internal/host/capture`; `go list -deps ./cmd/eva-md` does not name it.
- Every fixture payload of both hosts has a label and matches it by item 2, and `pv_false_typed` is 0.
- On Claude Code a payload with an entrypoint on no list decodes to `unknown` with its text; on Codex no prompt outside a hand-over is `typed`.
- The voice items of item 4 hold, each by its own test.
- For each fact this part leans on, `SPEC.md` names its Result and the test that holds it; a fact still unsettled is listed as waiting, with what it waits on.
- A test fails when a labels file holds anything but kinds, authors, speakers, fixture file names and positions: no payload's text.
- A reviewer's own mutations (a host's prompt labelled typed, a payload left unlabelled, an entry the fixtures need taken off `lists.go`, a line's speaker swapped) are each noticed by a test.
