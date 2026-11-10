# Background update, first half: input, model choice, headless run

Audit of `internal/update/{input,runner,claude,codex,schema,tokens,context,process,paste,classify,baseline,live,fake}.go`, with the callers read as far as needed to make true claims (`pass.go`, `backlog.go`, `job.go`, `runlog.go`, `validate.go`, `knowledge.go`, `rules.go`, `internal/hook/trigger.go`, `internal/hook/hook.go`).

Everything below is read from code unless marked **inferred**. Paths are relative to `the repository`. No test, update, doctor or model call was run. One read-only check was made outside the repo: `claude --help` on the `claude` 2.1.321 that happens to be installed in this session, to see whether the flags the update passes exist. `codex` is not installed here, so nothing about Codex's real behaviour is verified.

---

## 1. How it works

### 1.1 Triggers (what starts a job)

Hooks never run the update; they spawn `eva-md update --session <host>:<id>` detached, after writing their own output.

| Trigger | Condition | Where |
|---|---|---|
| Chat end | `SessionEnd`, not a program-run chat | `internal/hook/hook.go:568-570` |
| Compaction | `PreCompact` | `internal/hook/hook.go:568-570` |
| Long chat | `Stop` with 60 KB or more past the watermark | `internal/hook/hook.go:582-584`, `internal/hook/trigger.go:19,98-104` |
| Prompt after a gap | prompt 30 min or more after the file last changed, 600 bytes or more pending | `internal/hook/hook.go:522-524`, `trigger.go:20-21,111-116` |
| Chat start catch-up | this chat's file idle 2 min with a backlog, then up to 5 of the project's idle sessions | `trigger.go:22-23,151-168`, `internal/hook/resume.go:155` |
| Day's sweep | first person's chat start of the day: 5 sessions idle 1 h or more, whole inbox | `internal/update/job.go:138-142,199-233` |
| Manual | `eva-md update --pending / --session / --foreground` | `cmd/eva-md/update.go` |

A chat that stays open, under 60 KB, with no 30-minute gap, reaches no page until it ends or compacts.

### 1.2 Data flow of one pass

1. **Open the backlog.** `openBacklog` reads the watermark (`inbox.Mark{Offset, More, Wait}`, `internal/inbox/inbox.go:443-447`), then **rescans the file from byte 0 to the watermark** (`scanPrefix`, `input.go:75-107`, called at `backlog.go:63`) to recover four things the watermark does not store: the line count, whether the chat was off the record there, whether the open turn used a web tool, and whether a typed or host prompt came earlier. It finds prompts by byte patterns that depend on `json.Marshal`'s field order (`typedMarker`, `hostMarker`, `toolMarker`, `input.go:38-49`). Then it decodes a 4 MiB window past the watermark, growing by doubling to 64 MiB only when a group runs past it (`backlog.go:16-19,109-114`).
2. **Group.** The next `(session, branch)` group is the run of entries sharing a branch until a user prompt on another branch (`backlog.go:134-148`). At the end of the file the last prompt with no reply is held back (`answered`, `backlog.go:182-195`).
3. **Build** (`input.go:266-319`), called twice per pass: once on an empty page to learn where the chunk ends (`pass.go:219`), once with the real page (`pass.go:191`).
   - Refuses a session of an unknown host (`input.go:339-340`).
   - Drops off-the-record ranges (`onRecord`, `input.go:349-360`).
   - Requires one prompt, one reply and 600 characters, unless the group is the rest of a cut group or ends with `session_end` (`substantial`, `input.go:374-392`).
   - Renders units: one inbox entry, or a run of tool entries ending at its first `Bash` (`units`, `input.go:398-409`). Tool runs are compressed to one line: edited files listed, commands with exit code, every other tool only counted, e.g. `used Read ×12` (`compress`, `input.go:531-596`).
   - Each unit is `[n] <speaker> <date time>: <text>`; the speaker comes from the entry's role field (`speaker`, `input.go:493-501`). Text is cleaned by `freetext.Shown` (redaction again, control characters, forged headers quoted), lines that look like entries get `[quoted]`, continuation lines are indented (`body`, `quote`, `input.go:653-671`).
   - A user entry is cut to 6 KB, any other to 8 KB, **keeping head and tail around `[… cut …]`** (`input.go:224-225,239,700-715`). The inbox itself keeps up to 200 KB per entry (`internal/inbox/inbox.go:24`).
   - The chunk takes the oldest units up to 60 KB of rendered bytes, at least one (`input.go:289-292`). The cut is by unit, not by turn.
   - Page context precedes the entries (`pageContext`, `input.go:747-781`): branch, last 5 commit subjects, **all** Now items with id, kind, tag and refs, **all** standing rules, up to 12 knowledge rows in 2,400 characters, the last 5 Timeline lines, and for a new thread the parent's Now items. Every line is squeezed to one line of at most 600 characters (`oneLine`, `input.go:946-948`). The page's summary and last-chat line are deliberately not sent (`input.go:263-265`).
   - Knowledge rows are chosen by `knowledgeLines` (`input.go:864-907`).
4. **Model choice** (`pass.go:201`, `context.go:138-150`): the newest entry in the group that names a model, else the session's model marker, else no `--model` flag. A name that fails `modelName` (`runner.go:251`) counts as unknown. There is no config key that overrides the model (`internal/config/config.go:51-75`).
5. **Effort** (`pass.go:527-537`): the config's per-host or per-`host/model` value, default `highest` (`config.go:187-197`), which resolves to the last entry of the host's table: Claude `max` (`claude.go:14`), Codex `ultra` (`codex.go:21`). Today's run log may lower the start (`startEffort`, `runlog.go:414-433`).
6. **Run** (`pass.go:541-565`, `runner.go:149-213`): in a fresh empty temp folder, with `Rules`, `Schema(page)`, the batch on stdin.
7. **Classify** the attempt (`claude.go:68-117`, `codex.go:254-283`, `classify.go:85-101`).
8. Hand a JSON object to the validator (second half, not audited here).

### 1.3 The headless commands

Claude (`claudeArgs`, `claude.go:29-42`):

```
claude -p --safe-mode --tools "" --strict-mcp-config --no-session-persistence --permission-prompts none
       [--model M] [--effort E] --system-prompt <Rules> --output-format json --json-schema <schema>
```

- All of these flags exist in `claude --help` of 2.1.321, and its `--effort` levels are `low, medium, high, xhigh, max`, the same as `claudeEfforts` (verified by reading the help text only).
- The answer is the envelope's `structured_output` (`claude.go:110-114`). A non-empty `permission_denials` makes the run `tool_used` (`claude.go:93-94,141-147`).
- Failures are named from `api_error_status` when present, else from error text (`claude.go:97-108,122-136`). The code says neither `api_error_status` nor `api_error` is documented (`claude.go:57-61`).

Codex (`codexArgs`, `codex.go:113-129`):

```
codex --no-daemon exec --ephemeral --json -s read-only --skip-git-repo-check -C <run dir>
      --disable <feature>... -c web_search=disabled -c tools.update_plan.enabled=false
      [-m M] [-c model_reasoning_effort=E] [--ignore-user-config] --output-schema <file> -o <file> -
```

- Codex has no flag for fixed instructions, so `Rules` are prepended to the batch on stdin (`codexStdin`, `codex.go:133-135`).
- Before **every attempt** it runs `codex features list` under a separate 15 s limit (`codex.go:45,64,80-104`) and disables the names it finds from a fixed five (`codex.go:25`) plus 25 more copied from Codex 0.166.0's source (`codex.go:27-41`).
- The `--json` stream is read live. Any item that is not `agent_message`, `reasoning` or `error` is a tool and kills the run (`codex.go:229-239`). **Any event type the reader does not know also kills the run, as `invalid_output`** (`codex.go:217-221`), as does a non-JSON line containing `{` (`codex.go:196-201`).
- The answer is the `-o` file, read with `O_NOFOLLOW`, regular file only, at most 4 MiB (`codex.go:287-301`).

Both (`process.go`):

- Environment cleared to an allowlist, plus `TERM=dumb`, `EVA_MD_INNER=1`, `CLAUDE_CODE_DISABLE_AUTO_MEMORY=1` (`process.go:45-67`). API keys and every `CLAUDE_CODE_*` are dropped.
- Own process group, whole group killed on timeout or cancel and again after exit (`process.go:137-139,155-156,166-172`).
- Recorded absolute CLI path preferred, its folder appended to `PATH` (`process.go:86-109,130-132`).

### 1.4 Retry ladder (`runner.go:174-212`)

One shared deadline for all attempts (`runner.go:168,208-211`).

1. Claude refused `--strict-mcp-config` → retry once without it.
2. Refusal that names the effort → one step down the table.
3. Any other model refusal with an effort flag → drop the effort flag.
4. Refusal with no effort flag → drop the model flag, run the host default, note `fallback from <model>`.

`Result.Model` is the model **requested**, never the model that answered (`runner.go:85,169,199`); the envelope's `modelUsage` keys are read only for token counts (`claude.go:152-160`).

### 1.5 Caps, budgets and time limits

| What | Value | Where |
|---|---|---|
| Chunk of entries per pass | 60 KB rendered | `input.go:223` |
| One user entry / any other entry | 6 KB / 8 KB, head + tail | `input.go:224-225` |
| Minimum group | 1 prompt, 1 reply, 600 chars | `input.go:226,374-392` |
| Commit subjects / Timeline lines shown | 5 / 5 | `input.go:227-228` |
| One line of page context | 600 chars | `input.go:229` |
| Knowledge rows shown | 12 rows, 2,400 chars | `input.go:230-231` |
| Now items, standing rules shown | no cap | `input.go:752-763` |
| Decode window | 4 MiB, up to 64 MiB | `backlog.go:17-18` |
| Longest inbox line decoded | 2 MiB | `input.go:34` |
| Run timeout | 180 s at lowest effort to 600 s at highest | `runner.go:62-65,339-345` |
| `codex features list` | 15 s, per attempt, outside the run timeout | `codex.go:45` |
| `git log` for commits | 2 s | `context.go:20` |
| Claude stdout / stderr / Codex line / `-o` file | 8 MiB / 64 KiB / 4 MiB / 4 MiB | `process.go:21-28` |
| Daily cap | 40 records with a call, per host | `config.go:34`, `runlog.go:329-337` |
| Output arrays | done 8, items ids+40, timeline 20, unknowns 5, rules 10, knowledge 10 (5 kept) | `schema.go:167-176`, `knowledge.go:17` |
| Session quarantine | 3 session failures in a row | `runlog.go:56,62` |
| Backoff | 5 min doubling to 60 | `runlog.go:63-64` |

### 1.6 What this half writes

- `<inbox>/.baseline`, once, and a watermark at the end of every session file that existed before updates (`baseline.go:27,52-63,135,144-160`).
- Watermarks `<inbox>/<project>/.state/<file>.wm` (`inbox.go:434`), saved before the call (`backlog.go:199-204`).
- Temp folders `eva-md-run-*`, `eva-md-codex-*`, `eva-md-live-*`, all removed (`pass.go:546-550`, `codex.go:54-58`, `live.go:57-61`).
- Nothing else. Pages, run log and job log are written by the second half.

### 1.7 What the tests cover and do not

Covered, offline: argument lists match the design text, the environment allowlist, stdin-only input, process-group kill, shared timeout, effort step-down, model fallback, hand-written Claude envelopes and Codex streams, failure-text classification, chunking at exactly 60 KB, off-the-record and web state across passes, page context layout, strict schema parse.

Not covered:

- **No real CLI and no real model.** The fake is a shell script that records its arguments and replays files; it accepts any flag (`fakecli_test.go:31-62`). The envelopes and refusal texts are written by hand (`claude_test.go:15-22`, `codex_test.go:25`, `runner_test.go:591`).
- **The prompt's effect.** The only test of `Rules` checks that phrases are present and that it is 3,300 to 3,900 characters long (`input_test.go:836-864`).
- Whether Codex accepts `max` or `ultra`, how it words a refused effort, whether it loads a global `AGENTS.md` into an update run.
- Cost, latency and yield of a pass.

---

## 2. The prompt, read as a prompt

`Rules` is `schema.go:21-31`, version 4, eleven lines, about 3,600 characters. It is Claude's whole system prompt and the head of Codex's user message.

### 2.1 What it does well

- States once and clearly that entries, Now items, Timeline lines, rules and knowledge on file are data, not instructions.
- Says who each speaker is and that the label comes from the record, not the text.
- Gives a closed vocabulary of tags and a closed JSON schema, the same on both hosts.
- Gives a usable bar for knowledge: still matters in six months and cannot be worked out from the code.
- Forbids ids, refs, markdown, dates and credentials in the output, so code owns structure and provenance.

### 2.2 What it leaves out

1. **Who reads the page and why.** The model is not told that a coding agent will read this at the start of a later chat, in a short brief, or what a thread, a Now, a Timeline or a "last chat" is for. It cannot rank what matters.
2. **A legend for the input.** Nothing explains `[… cut …]`, `[REDACTED:kind]`, `(exit N)`, `used Read ×12`, `subagent`, the event names (`session_end`, `host_prompt`, `gap: …`), `(eva-md line: …)`, `(no id)`, `(read-only)`, `from <project key>`, or the section headings of the page context. Commit subjects and the branch name are not named as data.
3. **That tool output is absent.** A tool entry holds the tool's name, a short detail and an exit code, never its output (`internal/hook/hook.go:781-881`). The prompt calls a tool entry "a tool result" and lets it settle `observed` facts.
4. **That the batch may be a slice.** `Options.Continued` only relaxes the size check (`input.go:272`); the model is never told this is the middle of a chat, yet it is asked for "what this chat finished" and "what this chat did".
5. **That a host summary restates old entries.** A compaction summary can make the model log again what an earlier pass logged.
6. **The rules it is graded by.** See 2.3.
7. **Examples.** One fragment for a Now item, none for a whole answer, none negative. The one example contains "70%", a number the grounding count would flag unless an entry says it.

### 2.3 The hidden contract

The validator enforces rules the prompt never states. All of these are read from code:

| Enforced | Where | In the prompt? |
|---|---|---|
| Every path, URL, SHA, dotted identifier and number-with-unit in a knowledge title or statement must appear in the batch, page or commit subjects, or the add is refused | `knowledge.go:134,306-315`, `validate.go:889-910` | No |
| A knowledge statement tagged stated or accepted must have half its distinctive words in the user's own typed words | `knowledge.go:128,267`, `rules.go:278-281` | Only "in the user's words" |
| A standing rule must have every distinctive word in one prompt's own words | `rules.go:271-274` | "keep the user's words" |
| A supersede or drop may name only an id the input showed, not a read-only one | `knowledge.go:160-173` | No |
| A supersede needs a user prompt at or after the entry's time | `knowledge.go:176,212-214` | No |
| More than 3 lines with an uncited date reject the whole answer | `validate.go:65,922-923` | No |
| More than half the page's ids without an action reject the whole answer | `validate.go:924-925` | "give every Now item id one action" |
| At most 5 knowledge changes are kept | `knowledge.go:17,112-115` | Only in the schema description |

A refusal is counted in the run log and nothing else happens: the watermark moves on and the statement is gone. A whole-answer rejection is retried with the **same input and no hint of why** (`pass.go:400-403,601-608`).

### 2.4 Shape

- One call does six jobs: reconcile every Now item, write Timeline lines, write done lines, write a summary and a last-chat line, extract standing rules under a policy, and promote and reconcile knowledge under another policy.
- The schema makes every key required on every object (`schema.go:38-133`). Keeping an item costs a full eight-field object per item per pass. A page with 30 items makes the model write 30 `keep` objects before it adds anything.
- "rules" names three things: the prompt itself ("eva-md update rules"), the standing rules, and the JSON key.
- On Claude the text is a system prompt. On Codex it is the start of a user message under Codex's own agent prompt, with no boundary before the data. Nothing compares the two.
- The length is pinned by a test (`input_test.go:838`).

### 2.5 Judgement

**Inferred**, since no live run exists: a strong model will produce well-formed JSON from this prompt and get the easy cases right. It will also paraphrase, convert units, tidy paths and attribute an accepted proposal to the user, because nothing tells it not to, and each of those is silently refused. The yield of durable knowledge on real chats is likely to be low and nobody will see why. The fix is not a longer prompt; it is one contract shared by prompt and checker, and a way to tell the model what was refused.

---

## 3. What must be kept

Isolation of the run:

1. The batch goes on stdin, never in an argument (`runner.go:73`, `claude.go:25-28`, `codex.go:128`; `TestInputGoesOnStdinOnly`).
2. An empty run folder, checked (`runner.go:303-323`), and an allowlisted environment with `EVA_MD_INNER=1` as the recursion guard and API keys dropped (`process.go:45-67`).
3. Tool-free twice over: flags turn tools off, and any sign of a tool rejects the run (`claude.go:93-94`, `codex.go:229-239`). An unknown item type counts as a tool.
4. No host transcript is left: `--no-session-persistence`, `--ephemeral` (`claude.go:34`, `codex.go:114`).
5. Whole process group killed on timeout or cancel; capped writers so a child never blocks on a pipe (`process.go:137-139,155-156,176-190`).
6. Exit status from `ProcessState`, not `Wait`'s error, so an escaped grandchild holding a pipe does not turn success into failure (`process.go:152-157`).
7. The Codex answer file lives in a private folder per attempt and is read without following links, so no stale or planted answer is used (`codex.go:52-58,287-301`; `TestCodexStaleAnswerIsNotReused`).
8. An answer printed before the timeout does not count while the CLI is still running (`TestClaudeAnswerBeforeTheTimeoutStillTimesOut`).
9. Retries share one deadline; a retry that runs out before its CLI starts is a timeout, not an error (`runner.go:168,208-211`, `process.go:144-149`).
10. Recorded absolute CLI path, with its folder appended to `PATH` for a script CLI's interpreter (`process.go:86-109`).
11. Claude's refusal of `--strict-mcp-config` under a managed MCP config is retried once without it and noted (`runner.go:188-192`).

Provenance and privacy of the input:

12. The speaker comes from entry fields, never from text; forged headers are quoted; continuation lines indented; the same cleaning as the brief (`input.go:493-501,653-671`, `internal/freetext`).
13. Redaction is applied again on the way to the model (`freetext.Clean` calls `redact.Redact`).
14. Off-the-record ranges never reach the model, and the state carries across passes (`input.go:349-360`, `backlog.go:225-246`).
15. Physical line numbers and a byte-offset watermark, so a ref names the exact inbox line (`input.go:153-169,481-489`).
16. The watermark moves exactly past what a pass covered; a pass always takes at least one entry; a prompt waits for its reply (`input.go:289,311-314`, `backlog.go:182-195`).
17. What a web fetch or search reported never becomes observed knowledge (`input.go:443-459`, `knowledge.go:72-80`). Keep the principle; section 4 says why the mechanism is too narrow.
18. A pasted span can only be closed by the tag that repeats its id (`paste.go:26-55`).
19. Read-only, hardened git: no fsmonitor, no hooks, no optional locks, no lazy fetch, 2 s limit (`context.go:117-132`; `TestCommitsOfFetchesNothing`).

Contract with the hosts:

20. One strict schema both hosts accept: every key required, `additionalProperties: false`, no `maxLength`; and a strict re-parse in code (`schema.go:38-133,261-307`).
21. The model never writes ids, dates, markdown or provenance stamps (`schema.go:31`).
22. Always the chat's host, on the subscription login.
23. Three kinds of consequence for a failure: hold, backoff, blame on the session (`runlog.go:55-59`). Keep the three; change which one is the default.

Operations:

24. History from before updates is never summarized by surprise (`baseline.go:44-63`).
25. A watermark that cannot be saved costs no model call (`backlog.go:197-204`).
26. Tests end time limits themselves and never depend on machine speed (`fakecli_test.go:69-94`).

---

## 4. Where it falls short

Ranked by consequence for the goal.

### F1. Nothing here has met a real model or been measured against one — evaluation, critical, known in headline only

- `docs/known-limitations.md:176` says no live update has run. What it does not say is how little the offline suite can tell.
- The accuracy suite's 12 scripts run against **canned outputs written by the developers** (`internal/update/accuracy/scripts.go:1-16`). They measure the validator, not the prompt.
- The prompt's only test is phrase presence and length (`input_test.go:836-864`).
- The fake CLI accepts any flag (`fakecli_test.go:31-62`); the envelopes are hand-written.
- No test or metric covers: promotion yield, refusal rate by validator rule, summary quality, tokens and seconds per pass, long chats that span chunks, the two hosts side by side, or a model upgrade.
- The run log already records tokens, duration and a validation report per pass (`runlog.go:68-93`), but nothing aggregates them.

Direction: make evaluation a product component before any rebuild. Capture real envelopes and streams per host version as fixtures. Build a live suite on real and seeded long chats that scores yield, precision, staleness, refusals by rule, cost and latency per prompt version, model and effort. Choose defaults from it and gate releases on it. **Add.**

### F2. The prompt and the checker are two different contracts, with no feedback and no second chance — memory-quality, high, verified rules, inferred impact

- Evidence: section 2.3.
- A refused statement is lost when the watermark moves. A rejected answer is retried blind; after three the session is quarantined, and a failed retry the next day **moves the watermark past the chunk** (`pass.go:601-608`).
- `docs/known-limitations.md` lists two of the rules (the half-words rule and the 5-change cap) but not the gap between prompt and checker, nor the lack of feedback.

Direction: one source of truth for the output contract. Have the model return evidence as verbatim quote spans (entry number plus exact substring), which code checks by exact match; that replaces word-overlap guessing. Add one bounded repair turn that returns the refusals in machine form. Never move past unsummarized content without a replayable record. **Rebuild** the contract.

### F3. An unknown host failure is blamed on the chat, and blame ends in skipping the chat — reliability-safety, high, verified

- The default class is `invalid_output` (`classify.go:100`).
- An unknown Codex event type is `invalid_output` (`codex.go:217-221`). So is a Claude envelope without `structured_output` (`claude.go:110-114`).
- `invalid_output`, `tool_used`, `timeout` and `model_rejected` all count toward the session's quarantine (`runlog.go:56`), and the quarantined retry's failure skips the chunk (`pass.go:604-606`).
- No hold or backoff applies to these classes, so a host release that adds one event type makes every Codex session burn three calls, get quarantined, and lose its chunk the next day.
- The update never reads the CLI's version (no `--version` call in non-test code); wording is pinned to "claude 2.1.312 and codex 0.166.0" (`classify.go:11`), and this machine already has 2.1.321.
- The docs list only the reworded-error case (`docs/known-limitations.md:185`).

Direction: blame content only on positive evidence, which is a completed call whose answer the checker refused. Everything unrecognised is a host-level "unverified" state with backoff and a circuit breaker per host and CLI version. Chunks that fail go to a dead-letter list that is replayed after a CLI or eva-md upgrade. **Rebuild** the failure semantics; keep the three consequences.

### F4. The model sees a lossy, context-free slice of the chat — memory-quality, high, verified

- Tool output is never in the input; a tool entry is name, detail, exit code (`hook.go:781-881`). Tools other than edits and commands are only counted (`input.go:584-592`).
- An entry over 6 KB or 8 KB loses its middle (`input.go:700-715`). A long spec typed by the user or a long final answer is the most likely place for a decision.
- A chunk is cut by bytes, between units, with no overlap (`input.go:289-292`). A prompt and its reply can land in different passes; the second pass cannot attribute the reply.
- The previous summary is withheld (`input.go:263-265`) and the new one replaces it (`internal/threadpage/apply.go:140-145`). The docs tie this to the date rule: "the model writes them afresh from the entries, and the date rule runs on them" (`docs/design.md:847`). I read that as: an old summary's dates are in no entry of the batch, so showing it would make the model copy dates the checker then refuses (**inferred** reason). The effect is verified: "where the thread stands" is rewritten each pass from at most 60 KB plus the Now items.
- Commit subjects and the head commit are read **when the job runs**, not when the chat happened (`context.go:83-108`, `pass.go:280`). A chat caught up days later is shown later commits, they ground code-like tokens (`validate.go:193`), and the knowledge page is stamped with that later head (`pass.go:190,410`). The inbox entry has no commit field (`inbox.go:28-45`).
- None of this is in `docs/known-limitations.md`.

Direction: make the unit of work a turn-structured episode. Cut at turn boundaries, carry the previous summary and the open turn forward, summarize oversized entries instead of cutting them, capture a bounded redacted digest of tool results and the head commit at capture time. **Rebuild** the input model.

### F5. Memory runs on the chat's model at the highest effort, one pass at a time — invisibility, high, verified

- Model: the chat's (`context.go:138-150`), no override key (`config.go:51-75`).
- Effort: default `highest`, i.e. Claude `max`, Codex `ultra` (`config.go:196`, `runner.go:327-330`).
- Up to 600 s per pass (`runner.go:62-65`), 40 passes per host per day (`config.go:34`), one job at a time under one lock, passes serial.
- The budget counts calls, not tokens (`runlog.go:329-337`), though tokens are already parsed (`claude.go:152-167`, `codex.go:243-249`).
- This is decision 5 and decision 17 (`docs/decisions.md:42,107`), so it is a choice, not an oversight. The cost to the goal is real: the user's subscription quota is spent at the top rate for a summarization job, it competes with the live chat's rate limit, and a backlog of ten chunks can take over an hour.

Direction: separate the memory model from the chat model. A per-host policy of model tier and effort chosen by measured accuracy per cost (F1), escalating only when the checker refuses. A token budget shown as one number in `status`. Passes for different sessions in parallel behind per-page locks. **Evolve**; reopen decisions 5 and 17 with data.

### F6. The knowledge the model may reconcile is picked by raw word counts — memory-quality and recall-quality, high, verified mechanism, inferred impact

- `knowledgeLines` scores each row by the **number** of its content tokens found anywhere in the batch (`input.go:869-892`). There is no rarity weight and no length normalization; a 60 KB batch holds most common words.
- It ranks rows of every project together (`input.go:875`); another project's rows take places and are read-only (`input.go:919-921`).
- At most 12 rows in 2,400 characters (`input.go:230-231,893-905`); in practice about 7 full-length rows.
- A supersede or drop may name only a shown id (`knowledge.go:160-164`).
- So once a wiki holds more than a handful of rows, the row a chat contradicts may not be shown; the old fact stays in force and is recalled, and the new one is added beside it unless it shares 60% of its tokens (`knowledge.go:274-292`).
- The recall path already has a rarity-weighted scorer (`internal/knowledgepage/recall.go:131`). The update uses a second tokenizer and a cruder score.

Direction: one retrieval component for both recall and reconciliation, scoped project first, with rarity weighting and code-path matches. Add a periodic consolidation pass per project that sees the whole knowledge set, so correctness does not depend on what one chunk happened to mention. **Rebuild.**

### F7. The headless adapter is a stack of version-pinned host workarounds — architecture, high, verified

- 25 Codex feature names copied from one version of Codex's source (`codex.go:27-41`), plus a `codex features list` parse by whitespace (`codex.go:88-93`) run before every attempt and never cached (`codex.go:64`).
- Failure classes from English phrases of two CLI versions (`classify.go:11-52`), with a broad `refusalTerms` list ("access", "invalid", "unknown", "issue with") that only needs "model" or "effort" on the same line (`classify.go:47-48,129-131`).
- Auth is checked first over all error text, with terms such as "log in", "sign in", "api key" (`classify.go:18-19,89-90`); a false match holds the host for the day.
- A retry ladder (section 1.4) to discover each day what a model accepts (`runner.go:187-199`, `runlog.go:414-433`).
- Undocumented envelope fields (`claude.go:57-61`).
- No runtime probe of the CLI version or of what it accepts.
- None of this compensates for okf; all of it compensates for the hosts.

Direction: a host driver per host with two calls. `Probe`, cached by CLI path and version, learns flags, effort levels and features once. `Run` returns a typed outcome from structured fields first. On Claude, read `--output-format stream-json` and check the init message's tool list, model and key source before trusting the run (**inferred** from the SDK message format; to be confirmed). Contract tests run against real CLIs. **Rebuild** the adapter; keep section 3's isolation rules unchanged.

### F8. One monolithic call with a schema that forces boilerplate — architecture, medium, verified

- Six jobs in one answer (section 2.4).
- Every Now id needs a full object every pass (`schema.go:56-73`), and missing more than half rejects everything (`validate.go:924-925`).
- The parser accepts answers shaped by rules versions 1 to 3 (`schema.go:245-272`), versions that never ran against a model.
- The prompt's length is a tested invariant (`input_test.go:838`).

Direction: stages. Extract facts with evidence from the episode; reconcile them against the page and the knowledge set as a separate step, deterministic where it can be. Send deltas only: an id not mentioned is kept. Drop the version-compat parser and the length pin. **Rebuild.**

### F9. The cursor is rebuilt by byte-pattern scans on every job — architecture, medium, verified

- The watermark stores `Offset, More, Wait` (`inbox.go:443-447`).
- Line count, off-the-record state, web-turn state and "a person typed here" are recovered by scanning the file from byte 0 (`input.go:75-107`, `backlog.go:63`), using byte strings that depend on struct field order (`input.go:38-49`).
- The same missing state is why a cut group forgets its branch (`docs/known-limitations.md:191`), why an opening reply needs a special mark (`input.go:448-452`), and why `Options.Continued` exists.
- `docs/known-limitations.md` lists the rescan's cost, not its cause.

Direction: a per-session cursor record written atomically with the watermark: offset, line, off-the-record, taint, typed, open group's branch, open turn. Then `scanPrefix`, the markers and three special cases go away. **Rebuild.**

### F10. English-only word matching and a closed set of hosts — reach, medium, verified basis, inferred impact

- Stopwords, contractions, month names and units are English (`tokens.go:34-47,75-77,116,231`). Tokens are runs of letters (`tokens.go:58`), so text without spaces between words, such as Chinese or Japanese, becomes one token per phrase and nearly every overlap test fails.
- The rule cue words in the prompt are English (`schema.go:27`).
- `Build` refuses any host but two (`input.go:339-340`). Host knowledge is scattered through the input builder: paste detection (`paste.go:18`), web tool names (`input.go:421-423`), edit tool names (`input.go:526`), the tool line format (`input.go:514-523`).
- Process control is POSIX only (`process.go:137,166-172`).
- Users who bill through an API key, Bedrock or Vertex cannot run the update at all (`process.go:53-67`; this part is in `docs/known-limitations.md`).
- Not in the docs: the language limit.

Direction: evidence by exact quote (F2) works in any language. Move host knowledge behind the driver of F7. Let the driver say how the account pays instead of dropping every key. **Evolve.**

### F11. The line between trusted and untrusted text is too narrow — reliability-safety, medium, mostly known

- Only `WebFetch`, `WebSearch` and `web_search` taint a turn (`input.go:421-423`). A shell `curl`, `gh issue view`, an MCP fetcher or a file in a cloned repository does not, so what the assistant reports from them can be `observed` knowledge. The docs list the MCP case.
- Commit subjects enter the input without being named as data in the prompt (`input.go:751`, `schema.go:23`) and enter the grounding haystack (`validate.go:193`).
- The summary and last-chat line cite nothing and are checked only for dates (`validate.go:849-863`).
- Pasted text is the user's own on Codex, and on Claude the wrapper has not been seen in a real payload (`paste.go:5-18`; both in the docs).

Direction: taint by source class per turn (web, network command, MCP, file outside the repo), carried to every claim. Name every section of the input as data. Make the summary cite. **Evolve.**

### F12. Memory starts empty — invisibility, medium, known

- `Baseline` sets every existing session file's watermark at its end (`baseline.go:44-63,144-160`). The docs say there is no command to work through older chats.
- This protects against a surprise quota bill, which is right. But a new user's first weeks show no memory of months of work.

Direction: an opt-in, budgeted backfill, newest first per project, with a stated cost before it starts, running on the cheap tier of F5. **Add.**

---

## 5. Essential and accidental complexity

Essential: isolating a headless agent run (no tools, cleared environment, process group, stdin only), provenance from record fields, exact off-the-record and watermark handling, one strict schema for two hosts.

Accidental: version-pinned flags and feature lists; error-text classification; the daily effort ladder; prefix rescans with byte markers; web-turn and "opening reply" state invented to survive byte-based cuts; a second tokenizer for knowledge candidates; a parser that accepts answer shapes of versions that never ran; the previous summary withheld alongside a lexical date rule; a prompt whose size is a test.

## 6. Overall call

- **Keep** `process.go`, the environment allowlist, the run-folder check, the double tool-free check and the anti-forgery rendering. They are small, tested and right.
- **Rebuild** three things: the input model (episodes plus a real cursor), the output contract (prompt, schema, evidence and repair as one design), and the host adapter (a versioned driver with typed outcomes).
- **Evolve** the model policy.
- **Add** live evaluation first. No part of this half has met a real model, so the rebuilds should be sized by measured results, not by this reading alone.

## 7. Open questions

1. Does Codex accept `max` and `ultra` as `model_reasoning_effort`, and how does it word a refusal? The tests' wording is hand-written (`codex_test.go:25`).
2. Does a Codex update run load the user's global `AGENTS.md`, with and without `--ignore-user-config`? On Claude `--safe-mode` turns `CLAUDE.md` off; nothing in the code or docs covers Codex.
3. Does any Claude usage-limit message contain an auth term such as "/login"? Auth is checked before rate limit (`classify.go:89-92`), and a false auth holds the host for the day.
4. Does a real `UserPromptSubmit` payload still carry the `<pasted_content>` wrapper (`paste.go:9-11`)?
5. What does one pass cost in tokens and seconds at `max` on a 60 KB chunk, and does it finish inside 600 s?
6. Will the owner reopen decisions 5 and 17 (same model, highest effort)?
7. On a machine with only the Claude desktop app, is there a `claude` binary the detached job can start?
8. Would the hosts allow the memory step to run inside the host (a plugin subagent or hook-run prompt) instead of a detached CLI, and would that be less visible or more?

---

## Verification

A second reader tried to refute each finding against the code. Everything below was read in the repository at commit 51b241c. No test, update, doctor or model call was run. One check was made outside the repo: `claude --help` on the `claude` 2.1.322 installed in this session, which lists every flag `claudeArgs` passes and the effort levels `low, medium, high, xhigh, max`. `codex` is not installed here, so nothing about Codex's real behaviour is checked.

Verdicts: 6 confirmed, 6 partly, 0 refuted. Where the verdict is "partly", the core of the finding stands and the correction is stated.

| Finding | Verdict | Severity I would assign |
|---|---|---|
| update-model-1 | partly | critical |
| update-model-2 | partly | high |
| update-model-3 | confirmed | high |
| update-model-4 | confirmed | high |
| update-model-5 | confirmed | high |
| update-model-6 | confirmed | high |
| update-model-7 | partly | medium |
| update-model-8 | confirmed | medium |
| update-model-9 | partly | low |
| update-model-10 | partly | medium |
| update-model-11 | confirmed | medium |
| update-model-12 | partly | medium |

### update-model-1: partly, critical

- Confirmed: no live update has run (`docs/known-limitations.md:176`), the fake CLI accepts any flag (`internal/update/fakecli_test.go:31-62`), and the prompt's only test is phrases and length (`internal/update/input_test.go:836-864`).
- Correction: a live suite is not missing. It is built and has not been run. `cmd/live-accuracy` sends the 12 fact scripts, 21 passes, through a host's real update command (`internal/update/accuracy/live/live.go:68-128`, `docs/verification.md:82-88`), and `eva-md doctor --live` runs the exact command once on a fixture (`internal/update/live.go:45-75`). The report does not mention either. The comment the finding cites, `accuracy/scripts.go:1-16`, names the live command itself.
- What that harness does not cover is the real gap, and the docs already say most of it (`docs/known-limitations.md:177`): it builds batches with no rules and no knowledge (`accuracy/live/live.go:161-168`), so rule and knowledge yield are unmeasured; it has no long chat that spans chunks; it prints seconds per answer but scores neither tokens nor time; one run is one sample.
- So the action is "run it, then extend it", not "add". The severity stands: every impact in findings 2 to 11 is inferred until this runs.

### update-model-2: partly, high

- Confirmed, unstated in the prompt and enforced: every code-like token and number with a unit in a knowledge title or statement must be in the batch, the Now items or the commit subjects (`knowledge.go:134,222`, `validate.go:889-910`); a supersede needs a cited user prompt at or after the entry's time, even for an observed fact (`knowledge.go:176,212-214`); only shown, not read-only, ids may change (`knowledge.go:160-173`).
- Confirmed: refusals are counted in the report and nothing reads them back to the model or to a person. No command reads `Record.Validation` (`runlog.go:93`; no use in `cmd/` or `internal/health`). A rejected answer is rerun with the same input (`pass.go:400-403,601-608`).
- Correction 1: two of the four examples are in the prompt. `schema.go:31` states the date rule ("Never add a date ... copy one only as a cited entry's text writes it") and `schema.go:26` states one action per id. Only the thresholds that reject the whole answer are hidden (`validate.go:922-925`).
- Correction 2: the half-words rule is hinted in the prompt ("in the user's words", `schema.go:28`) and listed in `docs/known-limitations.md:149`, with the accepted-in-a-few-words case.
- Correction 3: the grounding rule refuses only knowledge. For Now items, Timeline and Done lines it only counts misses (`validate.go:886-888`).
- One more hidden rule, which makes the impact worse for corrections: the grounding text holds the batch, the Now items and the commit subjects, not the knowledge rows the input showed (`validate.go:193-203`). A supersede that keeps a path, an identifier or a number from the statement it replaces is refused unless the batch repeats it (`knowledge.go:222`), and the same text sent as an add is refused as a near duplicate of the old row (`knowledge.go:132,274-292`). The stale statement then stays in force.

### update-model-3: confirmed, high

- `classify.go:100` returns `invalid_output` for any unrecognised failure text. `codex.go:217-221` does the same for an unknown event type, `codex.go:196-201` for a non-JSON line with a brace, and `claude.go:82-84,110-114` for a clean exit without an envelope or without `structured_output`.
- `runlog.go:56` counts `invalid_output`, `tool_used`, `timeout` and `model_rejected` toward the session. `runlog.go:57-58` puts none of them in the hold or backoff lists. `pass.go:604-606` moves the watermark past the chunk when the next-day retry fails.
- The only bound is the daily cap of 40 passes per host (`pass.go:322`). The doctor and a chat-start notice report 3 failures in a row (`docs/design.md:770,985`), which is a notice, not a stop.
- No non-test code under `internal/update` reads a CLI version.
- Part of this is documented as designed (`docs/design.md:982`) and the reworded-error case is listed (`docs/known-limitations.md:185`). The unknown-event case and the loss of the chunk are not.

### update-model-4: confirmed, high

- Every cited line holds: head and tail cut at 6 KB and 8 KB (`input.go:224-225,700-715`); the chunk cut by unit with no overlap (`input.go:287-292`); the summary absent from `pageContext` (`input.go:747-781`) and replaced on apply (`internal/threadpage/apply.go:138-145`); a tool entry is name, detail and exit code (`internal/hook/hook.go:781-881`), and `compress` only counts tools other than edits and commands (`input.go:584-592`); commits and head read when the job runs (`context.go:83-108`, `pass.go:278-282`).
- The loss is larger than stated. Capture stores only the final assistant message of a turn (`hook.go:85,528-534`, `docs/design.md:450`). What the assistant said between tool calls is in no entry.
- Scope correction: a cut inside a turn happens only when a backlog is larger than one chunk. The Stop trigger fires at a turn's end once 60 KB of raw lines are pending (`internal/hook/trigger.go:19,98-104`), and the rendered text is smaller than the raw lines, so a chat that is kept up to date is cut at turn ends. Catch-up of idle chats, the sweep, and jobs that found the lock busy produce the mid-turn cuts.
- "Context-free" is too strong: the model gets all Now items, the last 5 Timeline lines, the rules and up to 12 knowledge rows. What it lacks is the previous turn and the old summary.

### update-model-5: confirmed, high

- `context.go:138-150` takes the chat's model, and `config.go:49-75` has no model key. `config.go:187-197` defaults effort to `highest`, which `runner.go:327-330` resolves to the table's last entry, `max` on Claude and `ultra` on Codex. `runner.go:62-65` allows 600 s.
- It is a recorded choice (decisions 5 and 17) and a listed risk with its mitigations (`docs/design.md:1425`): effort is already configurable per host and per `host/model`. What has no lever is the model.
- Two details: the cap counts passes, not CLI runs, since `runlog.go:329-337` adds one per record with a call, and a pass can make several runs down the effort ladder. "Over an hour" for ten chunks needs every pass to run near the 600 s limit.
- The direction's parallel passes would add to the rate-limit competition the finding itself names.

### update-model-6: confirmed, high

- `input.go:864-907` scores a row by how many of its content tokens the batch holds. There is no rarity weight, no length normalization and no preference for the chat's project; ties go to the newest. A long, generic row therefore outranks a short, specific one. Rows of other projects are ranked with the rest and shown read-only (`input.go:875,919-921`).
- `internal/knowledgepage/recall.go:118-135` already weights by rarity, doubles title terms and scopes by project.
- `knowledge.go:160-164` ignores a change to an id the input did not show.
- The consequence is worse than "recalled beside its replacement". The near-duplicate check compares an add with every row that applies to the project, shown or not (`validate.go:216-219`, `knowledge.go:132,274-292`). When the contradicted row is not shown, a corrected statement that shares 60% of its tokens with it is refused, so the correction is lost and the stale row stays alone.
- The impact grows with the number of rows and is inferred; no test measures it.

### update-model-7: partly, medium

- The evidence holds: 25 names from Codex 0.166.0 (`codex.go:35-41`), `codex features list` before each attempt (`codex.go:64,80-104`), English phrases of two CLI versions (`classify.go:11-52`), undocumented envelope fields (`claude.go:57-61`), the retry ladder (`runner.go:187-199`). The installed `claude` here is already 2.1.322, ten patch releases past the wording the code names.
- Correction 1: "without notice" is too strong. Three failures in a row reach the doctor and a chat-start notice, and `doctor --live` exists for this. Unknown output fails closed. What is missing is any tie between a failure and a CLI version.
- Correction 2: `runner.go:127-140` (`hostCLI{efforts, run}`) is already a seam per host. Adding a probe and typed outcomes to it is an evolution, not a rebuild.
- Severity: the user-visible cost of a host change is finding 3's (blame and skipped chunks). Counted apart from that, this is a maintenance cost, medium.

### update-model-8: confirmed, medium

- `schema.go:56-73` requires all eight fields on every item operation. `schema.go:245-272` accepts answers without `rules` and `knowledge`; an answer is always parsed by the binary that sent the rules, so no run can produce those shapes. `input_test.go:838` pins the length to 3,300 to 3,900 characters.
- Nuance: the checker does not force a `keep` per id. An id with no action is carried as it was, and only more than half of the ids without one rejects the answer (`validate.go:915-927`). The boilerplate comes from the prompt and the schema.

### update-model-9: partly, low

- The facts hold: `inbox.go:443-447` stores `Offset, More, Wait`; `backlog.go:63` scans from byte 0 at every open; the markers depend on the entry's field order (`input.go:38-49`).
- Severity: low. The scan is one buffered read per pending file, small beside a model call. The markers are tested (`headless_test.go:208`), and `internal/resume` uses the same technique. The wrong-branch and opening-reply cases arise only at a cut inside a turn, which only multi-chunk backlogs produce (see finding 4).
- The direction is sound and cheap, and it should ride with the input rebuild rather than rank on its own.

### update-model-10: partly, medium

- Confirmed: stopwords, contractions, month names, units, negations and the all-projects phrases are English (`tokens.go:34-47,75-77,116,231`, `rules.go:40-41,303-304`). Tokens are runs of letters (`tokens.go:58`), so text without spaces gives one token per clause. `restsOn` then finds no shared word (`validate.go:395-407`), a stated claim becomes inferred, and `file` turns the decision into a question (`validate.go:454-460`).
- The failure is not only refusal. In other languages the negation, all-projects and date checks never fire, so they pass what they are meant to stop.
- Correction 1: "POSIX only" is not a shortfall of this code. eva-md runs only on macOS by design.
- Correction 2: "API-key users get no update at all" is too wide. The run drops keys given in the environment (`process.go:45-67`); a stored API-key login still pays, as `docs/known-limitations.md:195` says.
- The language limit of the update is not in the docs.

### update-model-11: confirmed, medium

- `input.go:416-423` taints three tool names. A `curl` or `gh` run through Bash, and any MCP tool, count as local, by the code's own comment.
- Commit subjects enter the input (`input.go:750-751`) and the grounding text (`validate.go:193`), and the rules name neither them nor the branch as data (`schema.go:23`). They come from `refs/heads/<branch>`, so they include commits other people wrote.
- `free` checks the summary and the last-chat line for dates only (`validate.go:849-863`).
- Precision: tool output never reaches the model. Outside text arrives through the assistant's final reply about it, and that reply counts as a report of a result whenever any tool ran before it in the turn (`input.go:300-305`).

### update-model-12: partly, medium

- Confirmed: `baseline.go:44-63,144-160` gives every session file the update has not touched a watermark at its end, once, and no command works through older chats (`docs/known-limitations.md:201`).
- Correction: this is not why a new install starts empty. A new install's inbox is empty and `Baseline` sets aside nothing there (`baseline.go:48-49`). Baseline only matters for chats already captured before the first update job, as after an upgrade from a build that captured without updating.
- A new install starts empty because eva-md never imports a chat from before its first hook (`internal/catchup/catchup.go:1-2`).
- So a backfill for new users needs an importer of the hosts' own transcripts, which is a larger piece of work than replaying set-aside files and belongs to capture as much as to the update.

### Problems the report missed

**M1. A timeout is retried unchanged, and then the chunk is skipped (reliability-safety, high).**

- `runner.go:193-211` steps the effort down only on `model_rejected`. A timeout ends the pass.
- `runlog.go:56` counts it toward the session, and `startEffort` (`runlog.go:414-435`) reads only successful runs, so the next try asks for the same effort, the same chunk and the same limit.
- `claude.go:72-73` returns before reading the envelope, and Codex reports usage only at `turn.completed`, so a killed run is logged with 0 tokens.
- If a pass at the default highest effort on a 60 KB chunk runs past 600 s on a real host, each chunk costs three tries and one retry, about 40 minutes of top-effort quota, and then `pass.go:604-606` skips it. Whether that happens is unknown (the report's open question 5), and nothing in the code adapts if it does.

**M2. On Claude, a tool call that ran and a different answering model both pass unseen (reliability-safety, medium).**

- `claude.go:93-94,141-147` reads only `permission_denials`, the calls that were refused. Nothing reads a tool call that was allowed. The report's keep-list (section 3, item 3) says any sign of a tool rejects the run; that is true on Codex, where the stream is read live (`codex.go:229-239`), and not on Claude.
- Where a managed MCP config makes the run go without `--strict-mcp-config` (`runner.go:188-192`), a tool that managed settings allow can run inside the update and the answer is still saved. `docs/known-limitations.md:193` says the tool can be called; it does not say the run is then accepted.
- `claude.go:152-160` reads the `modelUsage` keys for counts only. `runner.go:85,169` return the model that was asked for, and `pass.go:405-411` stamps pages and knowledge with it.

**M3. A decision the user makes through the host's own UI never reaches the model as the user's (memory-quality, high).**

- The hooks match no `AskUserQuestion` or `ExitPlanMode` on Claude and no `request_user_input` on Codex (`internal/install/templates.go:74-75,84`). A user entry comes only from `UserPromptSubmit` (`internal/hook/hook.go:514-531`), and transcript recovery drops `tool_result` rows (`internal/catchup/parser.go:271-272`).
- `validate.go:316-328` gives `stated` or `accepted` only on a cited user prompt, and the rules say the same (`schema.go:29`).
- So a plan the user approves in plan mode, or an option the user picks in a question dialog, leaves no user entry and no plan text. The model can report it only from the assistant's final reply, as suggested, and `file` puts it under Open questions; as knowledge, a decision or constraint is refused.
- Checked in eva-md's code. That the hosts send no `UserPromptSubmit` for an approval or a dialog answer is the hosts' behaviour and was not run here.
