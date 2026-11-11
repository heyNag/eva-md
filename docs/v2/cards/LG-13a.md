# LG-13a: the hook payload decoder and the author allowlist, with no caller

**Plan:** section 6.1, design 2; section 8, row LG-13 and "What runs in parallel" (the record track's package); decision 29; section 14.3. **Design:** `docs/v2/working-papers/design/ledger.md` 3.4, 3.6, 6.5 (`DecodeHook`), row LG-13; `docs/v2/working-papers/field/note-evaluation.md`, "How Codex's voice mode hands work to the agent". **Facts:** from source, F-016 (Codex half), F-145, F-146 and the wrapper as Codex's source writes it; v1's lists for the rest. LG-13b checks them. **Size:** M. **Reviewers:** two: the contract, and one who tries to get a payload filed typed or spoken wrongly. **Depends on:** LG-10, HO-10a (merged). **Then:** LG-13b.

## Why

v1 files a voice hand-over, and any prompt it does not recognise, as the user's typed words, from three prefix lists that disagree. v2 sets an author only on positive evidence and decodes a hand-over line by line. LG-20 puts this in the hook; LG-21 sets a Codex prompt's author from the record.

## What to build

A package, `internal/host/capture`, on the standard library, `internal/host/adapter` and `internal/ledger/event`. Nothing imports it: v1 and the binary are unchanged, and no switch is needed.

1. **`Decode(host adapter.ID, raw []byte, env Env) (Decoded, error)`** in `decode.go`. Pure: no file, clock, environment, network or log. `Env{Entrypoint string}` is Claude Code's `CLAUDE_CODE_ENTRYPOINT`, read by the caller. It refuses a payload over 2 MiB (v1's `maxPayload`) or not JSON. `Decoded` holds `Event`, `Session`, `TID` (`turn_id`, else `prompt_id`, as v1), `Agent` (`agent_id`), `Rule`, and `Events []event.Event` in order, each with `TID`, `Agent` and `Src.Hook` set; `Seq`, `Turn` and `Src.Key` are left to the writer, which also redacts.
2. **Events.** Only events a row of `adapter.Rows(host)` declares; any other yields none. UserPromptSubmit: items 3 and 4. Stop: a `reply` by `agent`, none if empty. PostCompact with a summary: `summary` by `host`. PostToolUse and PostToolUseFailure: a `tool` by `tool`, no text (fields are LG-23's). Others: a `life` by `host`, text v1's fixed name (`eventName`, and `failureType` on StopFailure), marked `name`; SessionStart adds its source only from `startup`, `resume`, `clear`, `compact`, `fork`. A prompt, reply, summary or tool with an agent id is by `subagent`.
3. **A prompt's author, by allowlist**, rules tried in this order, each a word exported as a constant and set in `Rule`: empty, or one of v1's builtin commands: no event. An agent id: `subagent`. A listed `source`: the author the list gives it; any other non-empty source: `unknown`. The voice wrapper: item 4. A prefix on the one list: its author there (`host` or `program`; messages from another session and teammate messages are a program's, ledger 3.6). Any other leading tag (`<` then a letter, after white space): `unknown`. Then on Claude Code: an entrypoint on the program list (`sdk-cli`, `sdk-ts`, `sdk-py`) is `program`; on the interactive list (`cli`, `claude-desktop`, `claude-vscode`) `typed`; any other, or none, `unknown` with its text. `claude-desktop-3p` and `local-agent`, which v1's `headless_test.go` also takes for a person's chat, stay `unknown` until LG-13b has F-079. On Codex: `unknown`. The one list, `lists.go`, holds every source and prefix of v1's lists, each with its author and fact.
4. **The voice hand-over**, in `voice.go`. `VoiceWrapper = "<realtime_delegation>"`. A prompt that begins with it after white space and has exactly the form Codex's source writes, `<realtime_delegation>\n  <input>…</input>\n  <transcript_delta>user: …\nassistant: …</transcript_delta>\n</realtime_delegation>`, with an optional `  <source>…</source>` line first, the transcript optional, and `&amp;`, `&lt;`, `&gt;` escaped, yields: each transcript line in order, `user: ` by `spoken`, `assistant: ` by `front_model`, any other line `unknown`; then the request, by `front_model`, or by `host` under the source `transcript_tail_flush`. Texts are unescaped once. An empty request yields none; a request equal to the whole transcript (the host's stand-in for none) is `unknown`. A `user:` line equal to the request is `unknown`: the host adds the request as one. A first line the host's 4 KiB cut (`…`) left short is `unknown`. Any other form or source value: one prompt by `unknown`, marked `wrapper`. A hand-over never yields `typed`.
5. **`SPEC.md`** in the package: for each host and author, the evidence, its rule word, and the fact behind it with its state (from source, v1's list, or waiting for the sitting). `internal/ledger/SPEC.md`, at its size limit, points to LG-13.

## Owns and may touch

The new package (new), with its `testdata/`.

```paths
internal/host/capture/**
```

## Must not touch

Every other path; what its tests read stays as it is.

## Accepted when

- `scripts/linux-check.sh` passes; the diff names only `internal/host/capture`; `go list -deps ./cmd/eva-md` does not name it.
- Made-up payloads in `testdata/payloads/`, with a labels file, cover every rule word on both hosts, from v1's test cases (`hook_test.go`, `headless_test.go`, `wrapperPrompt` in `inboxv1_test.go`, the out-of-form wrappers of `ledger/audit/audit_test.go`), and hand-overs with a correction, a spoken yes, the pause phrase spoken, the tail flush, an empty request, escapes (`&amp;lt;` among them), a cut field, the request repeated and an unknown line. Each decodes to its labels; every event passes `Validate` once given a seq and a key.
- A seeded generated test over hosts, events, entrypoints, sources, agent ids and prompts finds `typed` only where every condition of item 3 holds, `spoken` only from a `user:` line of a hand-over of the form, and nothing of Codex `typed`.
- A test reads `hook.go` (`synthetic`, `builtinCommand`), `catchup/parser.go` (`syntheticText`, `hostWrapper`) and `ledger/audit/host.go` (`hostWrapper`) with `go/parser` and fails on a prefix or source on neither the one list nor the builtin commands (a later package that retires one of those lists removes its entry here, by its own card). Another holds `VoiceWrapper` equal to `inboxv1.Wrapper`, which the capture audit counts. Another fails when `SPEC.md` lacks a rule word or a fact `lists.go` cites.
- A test fails if a non-test file imports `os`, `io/fs`, `net`, `os/exec`, `log` or `time`.
- A reviewer's own mutations (the agent rule dropped, an unknown entrypoint typed, a Codex prompt typed, a repeated request spoken, a wrapper out of form decoded, the prefix rule moved after the entrypoint rule) are each noticed by a test.
- Only eva's name in it; no new module.
