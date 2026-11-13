# The hook payload decoder and the author allowlist

The contract of `internal/host/capture` (plan 6.1, design 2; 8, row LG-13; decision 29; 14.3. Ledger 3.4, 3.6 and 6.5). v1 files a voice hand-over, and any prompt it does not recognise, as the user's typed words, from three prefix lists that disagree. Here an author is set only on positive evidence, and a hand-over is decoded line by line.

Nothing calls it yet: v1 and the binary are unchanged and no switch is needed. LG-20 puts it in the hook, LG-21 sets a Codex prompt's author from the host's record, and LG-13b holds it to the sittings' recordings. Its code imports the standard library, `internal/host/adapter` and `internal/ledger/event`, and no `os`, `io/fs`, `net`, `os/exec`, `log` or `time`.

## 1. Decode

`Decode(host, raw, env) (Decoded, error)` is pure: it reads no file, clock, environment or network and writes no log. `Env.Entrypoint` is Claude Code's `CLAUDE_CODE_ENTRYPOINT`, which the caller reads.

It refuses a payload over 2 MiB (`MaxPayload`, v1's limit), and one that is not one JSON object with each key once and nothing after it, or whose fields it reads (`hook_event_name`, `session_id`, `turn_id`, `prompt_id`, `agent_id`, `prompt`, `source`, `last_assistant_message`, `compact_summary`) are not strings or null. Keys are matched exactly; a null is an absent field.

`Decoded` holds `Event`, `Session`, `TID` (`turn_id`, else `prompt_id`, as v1), `Agent` (`agent_id`), `Rule`, the word of the rule that decided, and `Events` in order. Each event has `TID`, `Agent` and `Src.Hook` set; `Seq`, `Turn` and `Src.Key` are the writer's, which also redacts. Every event passes `event.Validate` once given a seq and a key.

## 2. Events

Only an event a row of `adapter.Rows(host)` declares yields events; any other yields none, under `undeclared`. Every other event's rule is `event`, but a prompt's (section 3).

| Hook event | Events |
| --- | --- |
| UserPromptSubmit | sections 3 and 4 |
| Stop | a `reply` by `agent`, none when the message is empty or absent |
| PostCompact with a summary | a `summary` by `host` |
| PostToolUse, PostToolUseFailure | a `tool` by `tool`, with no text; its fields are LG-23's |
| any other the host declares | a `life` by `host`, its text v1's fixed name, marked `name`: `session_start`, `session_end`, `compact`, `model_switch`, `interrupt`, and `stop_failure` with v1's failure type. SessionStart adds `: ` and its source only for `startup`, `resume`, `clear`, `compact` and `fork` |

A prompt, reply, summary or tool with an agent id is by `subagent`.

## 3. A prompt's author

The rules are tried in this order; the first that holds decides.

| Order | Evidence | Author | Rule word | Fact, and its state |
| --- | --- | --- | --- | --- |
| 1 | empty, or only white space | no event | `empty` | none |
| 2 | the first word one of v1's builtin commands, in any case | no event | `builtin` | v1's list |
| 3 | an agent id | `subagent` | `agent_id` | v1's list (the hook's `synthetic`); on Codex from source: a subagent's hooks carry `agent_id`, a main thread's never (note-evaluation, question 6) |
| 4 | a source on the list | the list's author | `source` | F-016, v1's list |
| 4 | any other non-empty source | `unknown` | `source_unlisted` | F-016, v1's list |
| 5 | `<realtime_delegation>` after white space | section 4 | `voice`, or `wrapper` out of form | F-141, F-142, F-143, F-145, F-146; from source |
| 6 | a prefix on the list, after white space, in any case | the list's author | `prefix` | F-016, v1's list |
| 7 | any other leading tag: `<` and a letter, after white space | `unknown` | `tag` | F-016, v1's list |
| 8 | Claude Code, an entrypoint on the program list | `program` | `entrypoint_program` | F-079, v1's list |
| 8 | Claude Code, an entrypoint on the interactive list | `typed` | `entrypoint_interactive` | F-079, v1's list; F-016's Claude Code half, waiting for the sitting |
| 8 | Claude Code, any other entrypoint, or none | `unknown`, with its text | `entrypoint_unknown` | F-079, waiting for the sitting |
| 9 | any other host | `unknown` | `no_evidence` | F-121 and F-122, waiting for the sitting |

So `typed` needs all of: Claude Code, a non-empty prompt that is no builtin command, no agent id, no source, no hand-over, no listed prefix and no leading tag, and an entrypoint on the interactive list. `claude-desktop-3p` and `local-agent`, which v1's `headless_test.go` also takes for a person's chat, stay `unknown` until LG-13b has F-079.

On Codex the hook's payload says nothing of where a prompt came from, so no prompt is ever `typed` there (plan 6.1): it is `unknown` in the hook, and LG-21 sets its author from the host's record of what started the turn.

### The one list

`lists.go` holds every source and prefix of v1's lists, each once, with the author it gives, its fact and its state, and the file of v1 that lists it.

| Values | Author | Fact | State |
| --- | --- | --- | --- |
| sources `scheduled`, `scheduled_task`, `scheduler`, `cron`, `loop`, `background`, `system` | `host` | F-016 | v1's list |
| source `subagent` | `subagent` | F-016 | v1's list |
| source `agent` | `agent` | F-016 | v1's list |
| sources `other_session`, `session_message`, `automation` | `program` | F-016 | v1's list |
| prefixes `<teammate-message`, `[message from ` (a message from another session) | `program` | F-016 | v1's list |
| every other prefix: task notifications, system reminders, command banners and output, bash-mode input and output, interrupt notices, loop prompts, the host's own instructions and memory agent | `host` | F-016 | v1's list |
| entrypoints `sdk-cli`, `sdk-ts`, `sdk-py` | `program` | F-079 | v1's list |
| entrypoints `cli`, `claude-desktop`, `claude-vscode` | `typed` | F-079 | v1's list |

A test reads v1's lists in `internal/hook/hook.go` (`synthetic`, `builtinCommand`), `internal/catchup/parser.go` (`syntheticText`, `hostWrapper`) and `internal/ledger/audit/host.go` (`hostWrapper`) and fails on a value on neither this list nor the builtin commands. A later package that retires one of those lists removes its entry from that test, by its own card.

## 4. The voice hand-over

`VoiceWrapper` is `<realtime_delegation>`, equal to `inboxv1.Wrapper`, which the capture audit counts. A prompt that begins with it after white space is in form when it is exactly what Codex's source writes (note-evaluation, "How Codex's voice mode hands work to the agent", from source at 0.167.0):

```
<realtime_delegation>
  <source>transcript_tail_flush</source>
  <input>REQUEST</input>
  <transcript_delta>user: …
assistant: …</transcript_delta>
</realtime_delegation>
```

with only space, tab, CR and LF around it, the source line optional and only `transcript_tail_flush`, the transcript line optional and not empty, no `<` or `>` in a field, and each `&` beginning `&amp;`, `&lt;` or `&gt;`. A hand-over in form yields, under the rule `voice`:

| Part | Author | Fact |
| --- | --- | --- |
| each transcript line in order, `user: ` | `spoken`, the line after its head | F-143, waiting for the sitting |
| each transcript line in order, `assistant: ` | `front_model`, the line after its head | F-143, waiting for the sitting |
| any other line | `unknown`, the whole line | F-143 |
| a `user: ` line equal to the request, or, when the host cut the request, beginning with what is left of it | `unknown`, the whole line: the host adds the request as one | from source |
| the first line, when the transcript begins with the cut mark `…` | `unknown`, the whole line: the host's 4 KiB cut left it short | F-146, from source |
| then the request | `front_model`; `host` under the source `transcript_tail_flush` | F-142, waiting for the sitting |
| a request equal to the whole transcript | `unknown`: the host's stand-in for none | from source |
| an empty request, or an empty line | no event | |

Texts are unescaped once, so `&amp;lt;` is `&lt;`. Every part is a `prompt`, and every event carries the payload's turn id: a hand-over made while the agent works shares that turn's (F-145, from source). Any other form, or any other source value, is one `prompt` by `unknown`, its text the whole prompt, marked `wrapper`, under the rule `wrapper`. A hand-over never yields `typed`; a line yields `spoken` only from a `user: ` line of a hand-over in form. A pause phrase spoken is a `spoken` line like any other: what it does is LG-24's.

Where the request in the voice versions read is the front model's wording (F-142), it never carries the person's authority (decision 29); were F-142 refuted, only its label would change.

## 5. The facts

The facts of `docs/v2/host-facts.md` this package leans on, and where each stands. LG-13b replaces each waiting entry with its Result and the test that holds it.

| Fact | What | State |
| --- | --- | --- |
| F-016 | a prompt the host writes is marked as the host's: the sources and prefixes of the one list | v1's list; Claude Code's half waits for the sitting, Codex's half from source |
| F-079 | the entrypoint tells a person's chat from a program's | v1's list; waiting for the sitting |
| F-121 | Codex's record marks a typed prompt (LG-21's evidence) | waiting for the sitting |
| F-122 | that row is on disk when the prompt hook fires | waiting for the sitting |
| F-141 | a hand-over reaches the prompt hook as a wrapper | from source; waiting for the sitting |
| F-142 | the request is the front model's wording | waiting for the sitting |
| F-143 | each spoken line keeps its speaker | waiting for the sitting |
| F-145 | a hand-over while the agent works shares its turn's id | from source |
| F-146 | each field of a hand-over holds at most 4 KiB, cut with `…` | from source |
