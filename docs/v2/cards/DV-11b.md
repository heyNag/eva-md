# DV-11b: the Codex backend, its stream read against fixed lists

**Plan:** 6.3, design 8 (a tool call cannot pass unseen); section 8, Stage 1, row DV-11; 13.2 (failed and cut-off passes). **Design:** `derive.md` 3.6 and 8.1 (the Codex lines of "Changed"); `audit/update-model.md` 1.3, must-keeps 3, 4, 7 and 8, F3, F7, M1. **Part of:** DV-11, after [DV-11a](DV-11a.md), beside [DV-11c](DV-11c.md). **Size:** M. **Reviewers:** two: the contract, and whether any tool use can pass. **Depends on:** DV-11a, HO-14.

## Why

v1 reads Codex's `--json` stream live and kills a run at a tool item; that stays. But it calls an event type it does not know the chat's fault (`invalid_output`, `internal/update/codex.go`), runs `codex features list` before every attempt, and loses the tokens of a run it kills. This part is DV-11a's backend for Codex, with those three fixed.

## What to build

In `codex.go` and `codex_test.go` of `internal/derive/modelrun`, registered with DV-11a's `register` for `adapter.Codex`. The CLI's name comes from `adapter.ByID`.

1. **The run**, as v1's `runCodex`: the command of `codexArgs`, the `System` leading stdin as `codexStdin` puts it, the schema and the answer in a private folder made for each run, the answer read as `readOutput` reads it (no link followed, a regular file, at most 4 MiB, one JSON object). Efforts: v1's `codexEfforts`.
2. **The probe step**: `features list` once per DV-11a cache entry, through `Options.Env.Run` when set, else the sealed run in a private folder, under DV-11a's probe-step limit and its holdable timer. `Caps.Features` holds the names of v1's `codexOff` and `codexTools` the list gives, read as `codexDisable` reads it; a list that cannot be read gives `codexOff` and is not cached.
3. **The stream**, read as it arrives and stopped at the first line that decides:
   - known events: `thread.started`, `turn.started`, `turn.completed`, `turn.failed`, `error`, and `item.*`;
   - items allowed: `agent_message`, `reasoning`, `error`;
   - tool items, `tool_used`: `command_execution`, `file_change`, `mcp_tool_call`, `web_search`, `todo_list`;
   - any other item type (a missing type too), or any other event type: `host`;
   - a line that is not JSON but holds `{`, or is longer than 4 MiB: `host`; any other non-JSON line is skipped, with a signal.
   No message text is kept; error text is kept only to be classified.
4. **The class**, in this order: `cli_missing`; `tool_used`; `timeout`; `host` for item 3's cases; `ok` when the CLI exited 0, no turn failed, and the answer file holds one JSON object; `answer_invalid` when it exited 0 and no turn failed but the file holds none, and for v1's "incomplete response returned" (a filtered answer). Else, a failed turn, an `error` event or a non-zero exit, the error text is read through v1's wording for Codex, kept in this file with each term cited to `internal/update/classify.go`: `auth`, `rate_limit`, `host`, or `host` with `Refused` set when a refusal names the model or the effort. A required MCP server that failed is `host`, an optional one beside an answer a signal, and text nothing matches is `host`. One attempt: no retry, no effort ladder.
5. **Usage**: over every `turn.completed`, `In` = `input_tokens` less `cached_input_tokens` (never below 0), `CachedIn` = `cached_input_tokens`, `Out` = `output_tokens`; kept on a timeout.
6. **The model**: the stream names none, so `ModelAnswered` and `ModelsSeen` stay empty and DV-11a adds `model_unreported`.

Not here, each waiting on DV-02's recordings ([host-facts.md](../host-facts.md)): the fields a real failure carries (F-059), on which the wording kept from v1 is checked and the design's measure (no recorded outcome misclassified) taken; whether a run names the model that answered (F-060); usage on a run killed part way (F-061); that the sealed run has no tool and no global instructions (F-168); the effort names a real `codex` takes and how it refuses one (F-167). Whether `EVA_MD_INNER` reaches hooks (F-028) waits on the field report, and `--version`'s wording is HO-14's choice until HF-00 records it.

## Owns and may touch

The Codex backend and its tests.

```paths
internal/derive/modelrun/codex.go
internal/derive/modelrun/codex_test.go
```

## Must not touch

DV-11a's files (the rig is used unchanged; a gap in it goes in the report), DV-11c's, `internal/update/**`, `internal/host/adapter/**`, `internal/host/hostfake/**`.

## Accepted when

- `scripts/linux-check.sh` passes, and the diff holds only the two files.
- v1's Codex tests pass, ported: `TestCodexArgumentsMatchTheDesign`, `TestCodexDisablesTheToolFeaturesCodexLists`, `TestCodexFeaturesListThatHangsFallsBack`, `TestCodexToolEventStopsTheRun`, and `TestCodexStreams` with these rows now `host`: an unknown item type, an item without a type, an unknown event type, an event run together with other text, a required MCP failure, a failed exit, model text naming failures. `TestCodexModelRejectionFallsBack` becomes: a refused model, and a refused effort, are one attempt, `host`, with `Refused` set. `TestCodexStaleAnswerIsNotReused` becomes two calls in a row: the first leaves an answer, the second writes none and is `answer_invalid`.
- Each tool item, beside a valid answer file, is `tool_used` and the run is stopped; an unknown item type and an unknown event type are each `host` and stopped; a test each.
- `turn.completed`, then a CLI that waits to be killed, and the test ends the run's time with the rig's held timer: `timeout`, with that usage.
- HO-14's `(*Fake).Run`, given the base name by a wrapper in `Options.Env.Run` that counts calls, answers `--version` and `features list`; the runs go to the rig's script through the test's own link. Two runs at one version read `features list` once; after `SetVersion` and `os.Chtimes` on the link, once more. At HO-14's default (0.167.0, above the adapter's newest row) `Caps.Untested` is true and `CLIVersion` is that version; at that row's highest (0.166.0) `Known` is true.
- An agent message saying "rate limit" or "not logged in" never classifies. A marker planted in a message, an item type, an event type and an error text reaches no field DV-11a's invariants forbid.
- Mutations (an unknown item allowed, an unknown event ignored, `features list` read per run, a link followed to the answer, cached tokens counted twice) are each caught.

## Read first, added by the lead on 2026-11-13

DV-11a's page, under "As merged", holds four rules for this package that its builder could not have known when this page was written. Read them before the code.

## As merged

Merged from `pkg/dv-11b-r1` on 2026-11-13, after two reviews, one fix round and a verification that accepted it. Both reviews found the same major fault: the scan of a line, which the builder added beyond the card, walked its JSON by recursion with no limit, so one line of brackets from the host could end the whole process and leave the run's private folder behind. The scan now stops at 10,000 levels, the limit of Go's own JSON reader, and such a line is `host`.

Accepted as built, beyond the card: the reader is stricter than v1's. A tool item's type at any depth of a line, in any letter case, is `tool_used`. These are `host`: a line of JSON that is not an object, a key given twice, a second value on a line, a `type` key anywhere but on the event and its item or in another letter case, and a key only a tool item carries outside one. `SPEC.md`, section 7, lists them with the backend's two signals.

The lead's rulings of 2026-11-13, on the two points the builder, a reviewer each and the verifier left open:

1. **An `error` event with a clean exit and no answer is named from its text**, and is `host` when no term matches, never `answer_invalid`. A host's limit is not the chat's fault, and the card's "else, a failed turn, an error event or a non-zero exit" could not be reached any other way. An error item, such as a reconnect notice, does not count as one.
2. **A run in which the reader saw no known event is never `ok`.** With a clean exit it is `host`, whatever its answer, and with no answer too, where it was `answer_invalid`: the chat is not blamed for a stream nobody could read. A failed exit is still named from its error text as before, since a CLI that fails before its first event, as when it is not signed in or refuses a flag, prints none. As merged, a run with no event was `ok` when the CLI exited 0 with a valid answer, its only trace the signal for a skipped line: that is v1's rule, but a reader that saw nothing can vouch for nothing, and a host that stopped printing events would pass every tool unseen. A real run that ends well always prints one. A second round on this card's two files built it the same day, from `pkg/dv-11b-r2`, with the one test the verifier found missing (a key given twice does not stop the scan, so a tool later on the line is still a tool used); its commit carries this card's line too, and a verifier took the rule out, saw five tests fail, and found no run with no known event that still comes out `ok`.

For whoever reads the design paper: `derive.md` 8.1 said an unknown Codex event is ignored unless it is an item. The card and the code make it `host`, and the paper's line now says so.
