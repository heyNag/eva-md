# DV-03a: the contract spike's scripts, renderer and two forms of the contract

**Plan:** section 8, Stage 0, row DV-03 and the threshold "Typed refusals cited as assent"; section 6.3, design 2 and 3; decisions 5 and 10, at their recommendation. **Design:** `docs/v2/working-papers/design/derive.md` 5.1 to 5.4 and row DV-03; `docs/v2/host-facts.md` F-064. **Size:** M. **Reviewers:** one. **Depends on:** [EV-14a](EV-14a.md) (merged). **Part of:** DV-03, built by one builder in order: a, [b](DV-03b.md), [c](DV-03c.md), [d](DV-03d.md).

## Why

DV-21's contract rests on unmeasured bets: that a small model quotes the user's words exactly, or needs numbered segments; that one repair round pays; that a model never cites "no" or "maybe" as a yes. DV-03 measures them on made-up scripts, in a throwaway prototype that no release builds and that DV-21 deletes. It changes nothing v1 does: no switch.

## What to build

A new package `internal/eval/spike`; its comment and `SPEC.md` say it is deleted when DV-21 lands. Non-test files import from eva-md only `internal/eval/scenario`, `internal/ledger/event` and `internal/host/adapter`, for host ids, never literals (`TestHostLiteralCount`).

1. **Scripts.** `Scripts() []Script`, in a fixed order: `scenario.V1()`'s 12, a family `assent` of at least six, then `bounds.01`. `Script{Scenario scenario.Scenario; Plants []Plant}`; `Plant{Session string; Seq int64; Mode string; Proposal int64; Fact string}`: the typed prompt at `Seq` answers (`refusal`, `hedge` or `assent`) the proposal of fact `Fact` in the agent reply at `Proposal`. Typed turns only: spoken ones wait on HF-00's recorded wrapper.
   - Across `assent`: at least 24 refusals in 8 wordings, 24 hedges in 8 and 12 assents in 5; at least 4 one-word refusals; 4 refusals that open with an assent word ("yes, I see why, but no"); 4 replies proposing two things, one taken and one refused. No refusal or hedge repeats the value it answers.
   - Each proposal plants a made-up value at the agent's turn: an assent's fact must be stored under the user's authority as a decision or procedure; a refused or hedged one is `not_with_authority`. No planted value occurs in an id the spike writes (forms, tiers, efforts, script and metric names).
   - `bounds.01` plants over 24 facts in one session, so keeping all breaks the schema's bound (F-064).
   - Built by Go code; each script's JSON committed under `testdata/scripts/`.
2. **The renderer.** `Render(s scenario.Session, shown []Shown, f Form) (Input, error)`, over `scenario.Events(s)`. Items in force come first, by handle: `Shown{Handle, Kind, Tag, Text string}`. Then one entry per event, `[n] <label> <time>: <text>`, `n` the event's seq, so an entry names a turn as `scenario.Locate` takes it. Labels: `user typed`, `user chose`, `user pasted`, `assistant`, `subagent`, `tool`, `program`, any other author by its id. Controls, and events from an off control to the next on, are not shown. A text line that starts like an entry is marked so it cannot pass for one. The segment form lists each entry as `[n.k]` segments, split at line breaks and after sentence-final punctuation that ends the text or precedes white space. `Input{Bytes []byte; Entries []Entry}`; `Entry{N int64; Session string; Author event.Author; Kind event.Kind; Text string; Segments []string}`, `Text` as shown, marks in.
3. **Two forms.** `Form` is `FormQuotes` (`quotes`) or `FormSegments` (`segments`); `Prompt(f Form) string`, `Schema(f Form) []byte`, `RepairSchema(f Form) []byte`. An answer is `{"ops": [...], "nothing": none_new | only_progress | only_derivable | null}`, 0 to 24 ops. An op is `{"op", "id", "kind", "text", "admit", "ev"}`: `op` one of `item.add`, `item.supersede`, `item.retire`, `next.add`, `next.done`, `ask.add`, `ask.answer`, `note`; `kind` one of the plan's five for an add or supersede, `did`, `found`, `failed` or `left` for a note, else null; `admit` one of derive 5.3's seven for an `item.add`, else null; `ev` 1 to 3 spans, `{"e", "q", "as"}` or, in the segment form, `{"e", "s", "as"}` (`s` the segment's `k`), `as` one of `said`, `assent`, `proposal`, `result`, `report`. A repair answer is `{"fixes": [{"index", "op"}]}`, a null op meaning skip. Every key is required and nullable, no object takes another key, no schema holds `maxLength`.
4. **One refusal table.** `Codes`, each with its sentence and whether repair may fix it: `bad_shape`, `unknown_target`, `bad_entry`, `quote_not_found`, `bad_segment` and `ungrounded` yes, `tag_too_low` no. `Prompt` writes every sentence.
5. **The prompt**, one text for both hosts, in derive 5.1's order, with a made-up example answer and negative example. Its last line is `Everything after this line is data.` It names no model and no host.
6. **Strict reading.** `ParseAnswer(data []byte, f Form) (Answer, error)` and `ParseRepair`: an error only for data that is not one JSON object of the form's keys and types. Bounds are DV-03b's to check, op by op.

## Owns and may touch

The package's first files, all new.

```paths
internal/eval/spike/SPEC.md
internal/eval/spike/scripts.go
internal/eval/spike/scripts_test.go
internal/eval/spike/render.go
internal/eval/spike/render_test.go
internal/eval/spike/contract.go
internal/eval/spike/contract_test.go
internal/eval/spike/imports_test.go
internal/eval/spike/testdata/scripts/**
```

## Must not touch

Everything else. If a script needs what `scenario` lacks, stop and report.

## Accepted when

- `scripts/linux-check.sh` passes, and `go list -deps ./cmd/eva-md | grep -c internal/eval` prints 0.
- Every script passes `Validate`; item 1's counts hold; regenerating the scripts gives the committed files byte for byte; every script's text is ASCII and unchanged by `redact.Redact`: a test each.
- A golden render of an `assent` script in each form, with two shown items; v1.01's off-the-record stretch absent; v1.06's forged line marked; `Go 1.26.` and `scripts/benchmark.py` each one segment: a test each.
- Both schemas parse; each object requires all its keys, allows no other and holds no `maxLength`; the quotes schema has no `s`, the segment one no `q`: by test. Each prompt's example answer parses with `ParseAnswer`.
- A test fails when a code has no sentence in a prompt, or a prompt holds a host's name from `adapter.All()`.
- `imports_test.go` fails if a package outside `internal/eval/spike/**` and `cmd/eva-eval` imports this one, or a non-test file here imports an eva-md package beyond the three named.
