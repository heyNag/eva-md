# DV-11c: the Claude Code backend, classified from the envelope's fields

**Plan:** 6.3, design 8 (a tool call or another model cannot pass unseen); section 8, Stage 1, row DV-11; 13.1 (a run records the model asked for, not the one used). **Design:** `derive.md` 3.6, 8.1 (the Claude lines of "Changed") and 11 (row 2 and its fallback); `audit/update-model.md` 1.3, must-keeps 3, 4, 8, 9 and 11, F3, M1, M2. **Part of:** DV-11, after [DV-11a](DV-11a.md), beside [DV-11b](DV-11b.md). **Size:** S. **Reviewers:** two: the contract, and whether any tool use or another model can pass unrecorded. **Depends on:** DV-11a.

## Why

v1 names most of Claude Code's failures from error text and calls the rest the chat's fault (`invalid_output`, `internal/update/claude.go`); it keeps only the top model of `modelUsage`, beside the one it asked for; on a timeout it reads nothing, so the tokens spent are lost. The stream reader the design wants waits on a host fact not yet recorded ([host-facts.md](../host-facts.md) F-113). Until then the design's own fallback holds: the single envelope, `permission_denials`, and every key of `modelUsage` recorded. This part builds that.

## What to build

In `claude.go` and `claude_test.go` of `internal/derive/modelrun`, registered with DV-11a's `register` for `adapter.ClaudeCode`. The CLI's name comes from `adapter.ByID`.

1. **The run**, as v1's `runClaude` and `claudeArgs`: `--output-format json`, the `System` as `--system-prompt`, the input on stdin. Efforts: v1's `claudeEfforts`. The probe step adds nothing.
2. **`--strict-mcp-config` refused** (must-keep 11): retried once without it, under the call's one deadline, with a signal and `Attempts` 2. No other retry and no effort ladder.
3. **The class**, in this order:
   - no CLI: `cli_missing`; the run's time ended: `timeout`, the answer not counted (must-keep 8) but the usage of an envelope already printed kept;
   - no envelope (stdout past its cap, or not one JSON object of type `result`): a failed exit reads stderr and stdout through the wording of item 4; a clean exit, or no match, is `host`;
   - `permission_denials` anything but absent, `null` or an empty list: `tool_used`, beside any answer;
   - `subtype` `error_max_structured_output_retries`: `answer_invalid`;
   - `api_error_status` 401: `auth`; 429 or 529: `rate_limit`; 404: `host` with `Refused` "model"; any other 5xx: `host`; any other status goes on to the next line;
   - `is_error`, another `subtype`, or a failed exit: the wording of item 4 over `result` (only when `is_error`), `errors`, `api_error` and stderr; no match is `host`;
   - else `ok` when `structured_output` is one JSON object, `answer_invalid` when not.
4. **The wording**, used only where no field of item 3 decides: v1's terms for Claude Code, kept in this file with each term cited to `internal/update/classify.go`: `auth`, `rate_limit`, `host`, and a refusal that names the model or the effort, `host` with `Refused` set. The model's own words (`result` without `is_error`) never classify.
5. **Models**: `ModelsSeen` is every key of `modelUsage` that passes v1's `modelName`, sorted; `ModelAnswered` is the one with the most output tokens, the first by name on a tie, as `claudeModel` chooses. With two or more, a signal says so. `ModelAsked` is the call's and is never written into either.
6. **Usage**: from `modelUsage` where present, else `usage`: `In` = input tokens plus cache-creation tokens, `CachedIn` = cache-read tokens, `Out` = output tokens.

Not here, each waiting on DV-02's recordings: the stream reader (`--output-format stream-json`: the opening message's tools, model and key source checked, any tool-use block `tool_used`, the model read from the stream; F-113), so an allowed tool call is seen only through `permission_denials` until then (audit M2); the fields a real failure carries (F-059), on which the design's measure (no recorded outcome misclassified) is taken; the sealed call from a background job, `--bare` and `--safe-mode` (F-112, F-114); usage on a run killed part way (F-061).

## Owns and may touch

The Claude Code backend and its tests.

```paths
internal/derive/modelrun/claude.go
internal/derive/modelrun/claude_test.go
```

## Must not touch

DV-11a's files (the rig is used unchanged; a gap in it goes in the report), DV-11b's, `internal/update/**`, `internal/host/adapter/**`.

## Accepted when

- `scripts/linux-check.sh` passes, and the diff holds only the two files.
- v1's Claude tests pass, ported, each row with its new class: `TestClaudeArgumentsMatchTheDesign`, `TestClaudeEnvelopes` ("plain text after a clean exit", "nothing printed", "error during execution" and "failed exit with model text" become `host`), `TestClaudeManagedMCPConfigRetriesWithoutTheStrictFlag`, `TestClaudeAnswerBeforeTheTimeoutStillTimesOut`, and v1's `TestAttemptsShareTheTimeout` with the refused flag as its first attempt. `TestClaudeModelRejectionFallsBack`, `TestClaudeEffortRefusedAtEveryLevel`, `TestClaudeModelRejectedOnEveryAttempt` and the Claude rows of `TestRefusedEffortStepsDownToTheModelsTop` become: a refused model, and a refused effort, are one attempt, `host`, with `Refused` set. A capped envelope is `host`.
- A denial beside a valid answer, and a denial in a shape v1 has not seen, are `tool_used`: a test each.
- An envelope naming `claude-a` and `claude-b` gives both in `ModelsSeen`, the one with more output tokens as `ModelAnswered`, and the signal; an envelope with no `modelUsage` gives an empty `ModelAnswered` and `model_unreported` while `ModelAsked` is `claude-test-model`.
- An envelope printed, then a CLI that waits to be killed, and the test ends the run's time with the rig's held timer: `timeout`, no answer, that envelope's usage.
- A marker planted in `result`, `errors`, a denial's input and stderr reaches no field DV-11a's invariants forbid.
- Mutations (the denial check dropped, only the top model kept, the asked model copied, `result` read without `is_error`, the answer kept on a timeout) are each caught.
